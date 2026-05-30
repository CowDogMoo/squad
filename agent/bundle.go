// Package agent assembles agent prompt bundles and configuration metadata.
package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/cowdogmoo/squad/browser"
	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/mcp"
	"github.com/cowdogmoo/squad/skill"
	"gopkg.in/yaml.v3"
)

// SkillOverrides supplies per-run adjustments to the agent's skill catalog.
// Each field is opt-in; an unset field defers to the agent.yaml Skills block
// or, if that is also unset, to the engine default.
type SkillOverrides struct {
	// Enabled, when non-nil, force-enables or disables the catalog for this
	// run, overriding the manifest.
	Enabled *bool
	// Allow, when non-empty, replaces the manifest's Allow list for this run.
	Allow []string
	// Deny, when non-empty, replaces the manifest's Deny list for this run.
	Deny []string
}

// BundleOptions carries optional inputs to BuildBundleWithOptions. A nil
// pointer is equivalent to a zero value — all fields are opt-in.
type BundleOptions struct {
	// SkillOverrides applies CLI-flag overrides to the skill catalog.
	SkillOverrides *SkillOverrides
	// CatalogPaths are additional directories to scan as catalog-scope
	// skills (lowest precedence). Typically supplied by the runner from
	// source.SkillsManager.CatalogPaths().
	CatalogPaths []string
}

// ModelPreference represents a ranked model recommendation for an agent.
// The first entry in a models list is the primary (preferred) model.
type ModelPreference struct {
	Model    string `yaml:"model"`              // model identifier (e.g. "claude-sonnet-4-6")
	Provider string `yaml:"provider"`           // squad provider name (e.g. "anthropic", "openai-compat")
	BaseURL  string `yaml:"base_url,omitempty"` // optional endpoint override; required when Provider is "openai-compat"
}

// Manifest represents the structure of an agent's manifest file.
// A manifest is either a leaf agent (has entrypoint/wrapper) or a composed
// agent (has stages). These are mutually exclusive; Validate() enforces this.
type Manifest struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description,omitempty"`

	// Leaf agent fields.
	Models     []ModelPreference `yaml:"models,omitempty"` // ranked model preferences
	EntryPoint string            `yaml:"entrypoint,omitempty"`
	Wrapper    string            `yaml:"wrapper,omitempty"`
	References []string          `yaml:"references,omitempty"`
	Task       string            `yaml:"task,omitempty"`
	// Prompt is an inline system prompt. When set, the agent does not
	// need entrypoint or wrapper files — the inline prompt becomes the
	// whole system message. Useful for short single-file agents like
	// scheduled remote-tool runners.
	Prompt string `yaml:"prompt,omitempty"`
	// WorkingDir controls whether the agent operates on a local
	// filesystem. Set to "none" for agents that only call remote MCP
	// tools (Google Calendar/Drive, etc.). When "none", local file
	// tools (Read/Write/Edit/Glob/Grep/Bash) are not registered and
	// the run executes in a per-run temp directory.
	WorkingDir string `yaml:"working_dir,omitempty"`

	// Composed agent fields.
	Stages []ComposedStage `yaml:"stages,omitempty"`
	Gates  []ComposedGate  `yaml:"gates,omitempty"`

	// Shared fields.
	Environment   *executor.Config   `yaml:"environment,omitempty"`
	DependsOn     []string           `yaml:"depends_on,omitempty"`
	Output        *OutputConfig      `yaml:"output,omitempty"`
	Budget        *BudgetConfig      `yaml:"budget,omitempty"`
	MCPServers    []mcp.ServerConfig `yaml:"mcp_servers,omitempty"`
	DisableTask   bool               `yaml:"disable_task,omitempty"`
	MaxIterations int                `yaml:"max_iterations,omitempty"` // iteration cap for this agent (0 = use CLI default)
	EditDeadline  int                `yaml:"edit_deadline,omitempty"`  // stop after N iterations with no Edit calls (0 = disabled)
	// Isolation declares the recommended isolation mode: "" (none) or
	// "worktree" to run inside a fresh git worktree on a new branch.
	// CLI --isolate and config run.isolation override this.
	Isolation string `yaml:"isolation,omitempty"`

	// Skills, when non-nil, configures the Agent Skills catalog injected
	// into this agent's system prompt at boot. nil means "use defaults"
	// (catalog enabled, both scopes, no allow/deny). See skill package
	// and PLAN.md for the progressive-disclosure model.
	Skills *SkillsConfig `yaml:"skills,omitempty"`
}

// SkillsConfig controls which Agent Skills are surfaced to this agent via
// the system-prompt catalog. Each field is opt-in; an unset field falls
// through to the engine default.
type SkillsConfig struct {
	// Enabled, when non-nil, force-enables or disables the skill catalog
	// for this agent. nil means "auto-enable when any skill is discovered".
	Enabled *bool `yaml:"enabled,omitempty"`
	// Scopes restricts catalog assembly to this set of skill scopes. Empty
	// means all scopes ("repo" + "global"). Values match skill.Scope.String().
	Scopes []string `yaml:"scopes,omitempty"`
	// Allow, when non-empty, is an exclusive allowlist of skill names. Wins
	// over Deny and Scopes per PLAN.md.
	Allow []string `yaml:"allow,omitempty"`
	// Deny removes skills by name from the catalog. Applied only when Allow
	// is empty.
	Deny []string `yaml:"deny,omitempty"`
}

// BudgetConfig provides static hints for cost estimation.
// These are used by --dry-run to estimate costs before running.
type BudgetConfig struct {
	// MaxTokens is the recommended output-token budget for this agent.
	// If zero, inferred from whether the agent dispatches child agents.
	MaxTokens int `yaml:"max_tokens,omitempty"`

	// EstimatedIterations is the expected number of model iterations.
	EstimatedIterations int `yaml:"estimated_iterations,omitempty"`

	// Children lists child agents this orchestrator dispatches via the Task tool.
	// Each entry can be a plain string (agent name) or a ChildBudget with max_cost.
	Children []ChildBudget `yaml:"children,omitempty"`

	// ScaleFactor describes what makes this agent's cost scale.
	// Currently supported: "files" (cost scales with source file count).
	ScaleFactor string `yaml:"scale_factor,omitempty"`

	// FilesPerIteration is how many files the agent typically processes
	// per model iteration, used when ScaleFactor is "files".
	FilesPerIteration int `yaml:"files_per_iteration,omitempty"`
}

// ChildBudget represents a child agent with an optional dedicated cost cap.
type ChildBudget struct {
	Name          string  `yaml:"name"`
	MaxCost       float64 `yaml:"max_cost,omitempty"`       // dedicated cost cap in USD (0 = use remaining budget)
	MaxIterations int     `yaml:"max_iterations,omitempty"` // iteration cap for child (0 = inherit parent's cap)
}

// PrimaryModel returns the first ranked ModelPreference, or the zero value
// if no models are declared.
func (m *Manifest) PrimaryModel() ModelPreference {
	if len(m.Models) == 0 {
		return ModelPreference{}
	}
	return m.Models[0]
}

// FindModel returns the ModelPreference matching the given provider and model,
// or nil if not found. An empty provider or model never matches.
func (b *Bundle) FindModel(provider, model string) *ModelPreference {
	if provider == "" || model == "" {
		return nil
	}
	for i := range b.Models {
		if b.Models[i].Provider == provider && b.Models[i].Model == model {
			return &b.Models[i]
		}
	}
	return nil
}

// ChildNames returns the list of child agent names.
func (b *BudgetConfig) ChildNames() []string {
	if b == nil {
		return nil
	}
	names := make([]string, len(b.Children))
	for i, c := range b.Children {
		names[i] = c.Name
	}
	return names
}

// ChildMaxCost returns the dedicated cost cap for the named child agent.
// Returns 0 if no dedicated cap is configured (use remaining budget).
func (b *BudgetConfig) ChildMaxCost(agentName string) float64 {
	if b == nil {
		return 0
	}
	for _, c := range b.Children {
		if c.Name == agentName {
			return c.MaxCost
		}
	}
	return 0
}

// ChildMaxIterations returns the iteration cap for the named child agent.
// Returns 0 if no cap is configured (inherit parent's cap).
func (b *BudgetConfig) ChildMaxIterations(agentName string) int {
	if b == nil {
		return 0
	}
	for _, c := range b.Children {
		if c.Name == agentName {
			return c.MaxIterations
		}
	}
	return 0
}

// OutputConfig specifies the structured output contract for an agent.
// When format is "json", the agent's system prompt is augmented with
// instructions to emit JSON matching the declared schema.
type OutputConfig struct {
	// Format is the output format: "json" or "markdown" (default: "markdown").
	Format string `yaml:"format,omitempty"`

	// Schema is a JSON Schema definition for the agent's output.
	Schema map[string]any `yaml:"schema,omitempty"`
}

// Bundle contains the assembled system, user, and combined prompt content for an agent run.
type Bundle struct {
	System        string // wrapper + system prompt + references + task
	User          string // user request (CLI prompt or default)
	Combined      []byte // concatenated for --print-bundle/--bundle-out
	WorkDir       string
	Model         string             // primary model override (first from models list or single model field)
	Provider      string             // primary provider override (first from models list or single provider field)
	BaseURL       string             // primary model's base URL override (for openai-compat)
	Models        []ModelPreference  // ranked model preferences from manifest
	Budget        *BudgetConfig      // budget configuration from manifest
	Environment   *executor.Config   // execution environment from agent manifest
	MCPServers    []mcp.ServerConfig // MCP server dependencies declared in agent.yaml
	DisableTask   bool               // when true, the Task tool is not registered for this agent
	MaxIterations int                // iteration cap from manifest (0 = use CLI default)
	EditDeadline  int                // stop after N iterations with no Edit calls (0 = disabled)
	// RemoteOnly mirrors Manifest.IsRemoteOnly. When true, local
	// filesystem tools are not registered and the runner skips
	// working-dir resolution / repo-summary injection.
	RemoteOnly bool
	// SkillEntries is the filtered set of skills surfaced in the system
	// prompt's "Available skills" block. The runner uses these to build
	// the Skill tool's runtime so the names the agent sees are exactly
	// the names it can load via Skill(name).
	SkillEntries []skill.Entry
}

// TemplateData holds the data passed to prompt templates.
// Templates can use {{.Mode}}, {{.Var "KEY"}}, or {{.Vars.KEY}}.
//
// AgentDir is the absolute path to the agent's directory on disk. Useful for
// MCP `command:` entries that point at scripts shipped alongside the agent —
// `{{.AgentDir}}/wrapper.sh` keeps the manifest portable across machines.
type TemplateData struct {
	Mode     string
	Vars     map[string]string
	AgentDir string
}

// Var returns the value of a template variable, or empty string if not set.
// Usage in templates: {{.Var "COVERAGE_TARGET"}}
func (td TemplateData) Var(key string) string {
	if td.Vars == nil {
		return ""
	}
	return td.Vars[key]
}

// Default returns the value of a variable, or a default if not set.
// Usage in templates: {{.Default "COVERAGE_TARGET" "75"}}
func (td TemplateData) Default(key, defaultVal string) string {
	if td.Vars == nil {
		return defaultVal
	}
	if v, ok := td.Vars[key]; ok && v != "" {
		return v
	}
	return defaultVal
}

// Env returns the value of an OS environment variable, or the first
// non-empty fallback when unset. Distinct from Var/Default which read
// from squad's --var / cfg.Vars map. Usage in templates:
//
//	env:
//	  - "GOOGLE_TOKEN={{.Env \"GOOGLE_TOKEN\"}}"
//	  - "API_BASE={{.Env \"API_BASE\" \"https://api.example.com\"}}"
//
// Squad's `--var KEY=VAL` flags shadow OS env when both name the same
// key — author your manifest to call `.Env` for ambient credentials
// (refresh tokens, machine-local secrets) and `.Default` / `.Var` for
// runtime knobs the caller is expected to set explicitly.
func (td TemplateData) Env(key string, fallbacks ...string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	for _, f := range fallbacks {
		if f != "" {
			return f
		}
	}
	return ""
}

// BrowserProfile returns the absolute path to the named browser profile
// dir, creating it lazily if missing. Usage in templates:
//
//	mcp_servers:
//	  - name: chrome
//	    command: npx
//	    args:
//	      - chrome-devtools-mcp@latest
//	      - --userDataDir={{.BrowserProfile "amazon"}}
//
// The agent.yaml stays portable across machines — each machine maintains
// its own logins in its own copy of the profile dir.
func (td TemplateData) BrowserProfile(name string) (string, error) {
	return browser.ProfileDir(name)
}

// makeIncludeFunc creates an include function that reads from the _templates directory.
// Templates can use {{include "hard-rules/universal.md"}} to include shared content.
// Uses os.OpenInRoot (Go 1.24+) for traversal-resistant file access.
func makeIncludeFunc(agentsDir string) func(string) (string, error) {
	templatesDir := filepath.Join(agentsDir, "_templates")
	return func(path string) (string, error) {
		f, err := os.OpenInRoot(templatesDir, path)
		if err != nil {
			return "", fmt.Errorf("failed to include template %s: %w", path, err)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil {
				logging.Warn("failed to close template %s: %v", path, cerr)
			}
		}()
		content, err := io.ReadAll(f)
		if err != nil {
			return "", fmt.Errorf("failed to read template %s: %w", path, err)
		}
		return strings.TrimSpace(string(content)), nil
	}
}

// resolveDisplayMode returns mode, defaulting to "edit" when empty.
func resolveDisplayMode(mode string) string {
	if mode == "" {
		return "edit"
	}
	return mode
}

// processTemplate executes a Go text/template with the given data.
// Templates can use {{if eq .Mode "edit"}}...{{end}} conditionals.
// Templates can use {{include "hard-rules/universal.md"}} to include shared content.
// Templates can use {{.Var "KEY"}} or {{.Default "KEY" "default"}} for custom variables.
// Templates can use {{now "Monday 2006-01-02"}} for the current local date/time
// formatted via Go's reference layout, or {{today}} as a shortcut for
// "Monday 2006-01-02".
func processTemplate(name, content, agentsDir string, data TemplateData) (string, error) {
	data.Mode = resolveDisplayMode(data.Mode)

	funcMap := template.FuncMap{
		"include": makeIncludeFunc(agentsDir),
		"now": func(layouts ...string) string {
			layout := time.RFC3339
			if len(layouts) > 0 && layouts[0] != "" {
				layout = layouts[0]
			}
			return time.Now().Format(layout)
		},
		"today": func() string {
			return time.Now().Format("Monday 2006-01-02")
		},
	}

	tmpl, err := template.New(name).Funcs(funcMap).Parse(content)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", name, err)
	}

	return buf.String(), nil
}

// readFileInRoot opens path relative to root using os.OpenInRoot (Go 1.24+)
// for traversal-resistant reads, then returns the file contents.
func readFileInRoot(root, path string) (data []byte, retErr error) {
	f, err := os.OpenInRoot(root, path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("failed to close %s: %w", path, cerr)
		}
	}()
	return io.ReadAll(f)
}

func loadReferences(agentPath string, refs []string) ([]string, error) {
	var result []string
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		refData, err := readFileInRoot(agentPath, ref)
		if err != nil {
			return nil, fmt.Errorf("failed to read reference %s: %w", ref, err)
		}
		result = append(result, fmt.Sprintf("## Reference: %s\n\n%s\n", ref, strings.TrimSpace(string(refData))))
	}
	return result, nil
}

// loadTask reads the task file if specified and returns its content.
func loadTask(agentPath, taskFile string) (string, error) {
	if taskFile == "" {
		return "", nil
	}
	taskData, err := readFileInRoot(agentPath, taskFile)
	if err != nil {
		return "", fmt.Errorf("failed to read task %s: %w", taskFile, err)
	}
	return strings.TrimSpace(string(taskData)), nil
}

// LoadManifest reads and parses the agent manifest from the given agent directory.
// It validates the manifest structure, ensuring composed and leaf fields are
// mutually exclusive.
func LoadManifest(agentPath string) (*Manifest, error) {
	manifestPath := filepath.Join(agentPath, "agent.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent manifest: %w", err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse agent manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid agent manifest: %w", err)
	}
	return &manifest, nil
}

// resolveEnvironmentTemplates processes template variables in environment options.
func resolveEnvironmentTemplates(env *executor.Config, data TemplateData) error {
	if env == nil || env.Options == nil {
		return nil
	}
	for key, val := range env.Options {
		tmpl, err := template.New(key).Parse(val)
		if err != nil {
			return fmt.Errorf("failed to parse environment option %s: %w", key, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return fmt.Errorf("failed to resolve environment option %s: %w", key, err)
		}
		env.Options[key] = buf.String()
	}
	return nil
}

// resolveMCPServerTemplates processes template variables in MCP server configurations.
// ApplyMCPOverride replaces a bundle's MCP server list with the supplied
// override, applying the same template expansion (Var/Env/BrowserProfile/AgentDir)
// that BuildBundle uses. Intended for stage-scoped MCP overrides in
// composed agents, where a stage's mcp_servers replaces the parent
// manifest's list for that stage's run only.
func ApplyMCPOverride(b *Bundle, override []mcp.ServerConfig, mode, agentDir string, vars map[string]string) error {
	if b == nil {
		return fmt.Errorf("ApplyMCPOverride: bundle is nil")
	}
	data := TemplateData{
		Mode:     mode,
		Vars:     vars,
		AgentDir: agentDir,
	}
	resolved, err := resolveMCPServerTemplates(override, data)
	if err != nil {
		return err
	}
	b.MCPServers = resolved
	return nil
}

func resolveMCPServerTemplates(servers []mcp.ServerConfig, data TemplateData) ([]mcp.ServerConfig, error) {
	resolveStr := func(name, val string) (string, error) {
		if val == "" || !strings.Contains(val, "{{") {
			return val, nil
		}
		tmpl, err := template.New(name).Parse(val)
		if err != nil {
			return "", fmt.Errorf("failed to parse MCP server template %s: %w", name, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return "", fmt.Errorf("failed to resolve MCP server template %s: %w", name, err)
		}
		return buf.String(), nil
	}

	resolved := make([]mcp.ServerConfig, len(servers))
	for i, srv := range servers {
		resolved[i] = srv
		var err error
		if resolved[i].Command, err = resolveStr(srv.Name+".command", srv.Command); err != nil {
			return nil, err
		}
		if resolved[i].URL, err = resolveStr(srv.Name+".url", srv.URL); err != nil {
			return nil, err
		}
		for j, arg := range srv.Args {
			if resolved[i].Args[j], err = resolveStr(fmt.Sprintf("%s.args[%d]", srv.Name, j), arg); err != nil {
				return nil, err
			}
		}
		for j, env := range srv.Env {
			if resolved[i].Env[j], err = resolveStr(fmt.Sprintf("%s.env[%d]", srv.Name, j), env); err != nil {
				return nil, err
			}
		}
		for j, hdr := range srv.Headers {
			if resolved[i].Headers[j], err = resolveStr(fmt.Sprintf("%s.headers[%d]", srv.Name, j), hdr); err != nil {
				return nil, err
			}
		}
	}
	return resolved, nil
}

// loadAndProcessPrompts loads system, wrapper, and task files, then processes them as templates.
func loadAndProcessPrompts(agentPath, agentsDir string, manifest *Manifest, data TemplateData) (system, wrapper, task string, err error) {
	systemData, err := readFileInRoot(agentPath, manifest.EntryPoint)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read system prompt: %w", err)
	}
	wrapperData, err := readFileInRoot(agentPath, manifest.Wrapper)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read agent wrapper: %w", err)
	}
	taskContent, err := loadTask(agentPath, manifest.Task)
	if err != nil {
		return "", "", "", err
	}

	system, err = processTemplate("system", string(systemData), agentsDir, data)
	if err != nil {
		return "", "", "", err
	}
	wrapper, err = processTemplate("wrapper", string(wrapperData), agentsDir, data)
	if err != nil {
		return "", "", "", err
	}
	if taskContent != "" {
		task, err = processTemplate("task", taskContent, agentsDir, data)
		if err != nil {
			return "", "", "", err
		}
	}
	return system, wrapper, task, nil
}

// toolEfficiencyPrompt is injected into all agent system prompts to encourage
// batching of tool calls and efficient file reading.
const toolEfficiencyPrompt = `## Tool Efficiency

When you need to perform multiple independent operations, invoke ALL relevant tools in a single response:
- Reading multiple files: call Read for ALL of them in one response, not one at a time.
- Making multiple edits: call Edit for ALL of them in one response.
- Multiple searches: call Grep for ALL patterns in one response.

Do NOT read files you have already read unless you need to verify edits you just made.
Do NOT spend more than a few iterations exploring before making your first edit.
Prefer using Read with offset/limit for large files instead of reading the entire file.
`

// compactRepoSummary generates a brief structural overview of the working directory.
// This is injected into the system prompt so agents don't waste iterations
// discovering the basic structure of the codebase.
// repoSummarySkipDirs are directories excluded from the repo summary walk.
var repoSummarySkipDirs = map[string]bool{
	".venv": true, "venv": true, "__pycache__": true, ".tox": true,
	"node_modules": true, ".git": true, ".mypy_cache": true,
	".pytest_cache": true, ".ruff_cache": true, ".eggs": true,
	"target": true, "build": true, "dist": true, "vendor": true,
}

type dirInfo struct {
	files int
	exts  map[string]int
}

// repoSummaryVisitor returns the WalkDirFunc used by compactRepoSummary.
func repoSummaryVisitor(workingDir string, dirs map[string]*dirInfo, totalFiles *int) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(workingDir, path)
		if rel == "." {
			rel = ""
		}
		if d.IsDir() {
			return repoSummaryVisitDir(rel, d)
		}
		return repoSummaryVisitFile(rel, d, dirs, totalFiles)
	}
}

func repoSummaryVisitDir(rel string, d fs.DirEntry) error {
	name := d.Name()
	if (strings.HasPrefix(name, ".") && name != ".") || repoSummarySkipDirs[name] {
		return filepath.SkipDir
	}
	depth := 0
	if rel != "" {
		depth = strings.Count(rel, string(filepath.Separator)) + 1
	}
	if depth > 3 {
		return filepath.SkipDir
	}
	return nil
}

func repoSummaryVisitFile(rel string, d fs.DirEntry, dirs map[string]*dirInfo, totalFiles *int) error {
	if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") {
		return nil
	}
	*totalFiles++
	dir := filepath.Dir(rel)
	if dirs[dir] == nil {
		dirs[dir] = &dirInfo{exts: make(map[string]int)}
	}
	dirs[dir].files++
	if ext := strings.ToLower(filepath.Ext(d.Name())); ext != "" {
		dirs[dir].exts[ext]++
	}
	return nil
}

func compactRepoSummary(workingDir string) string {
	dirs := make(map[string]*dirInfo)
	totalFiles := 0

	if err := filepath.WalkDir(workingDir, repoSummaryVisitor(workingDir, dirs, &totalFiles)); err != nil {
		logging.Warn("failed to walk working directory for repo summary: %v", err)
		return ""
	}

	if totalFiles == 0 {
		return ""
	}

	dirNames := make([]string, 0, len(dirs))
	for d := range dirs {
		dirNames = append(dirNames, d)
	}
	sort.Strings(dirNames)

	var sb strings.Builder
	sb.WriteString("## Repository Structure\n\n")
	fmt.Fprintf(&sb, "Working directory: %s (%d files)\n\n", workingDir, totalFiles)
	sb.WriteString("```\n")
	for _, dir := range dirNames {
		info := dirs[dir]
		display := dir
		if display == "." {
			display = "./"
		} else {
			display += "/"
		}
		topExts := topNExts(info.exts, 3)
		fmt.Fprintf(&sb, "%-40s %3d files  %s\n", display, info.files, topExts)
	}
	sb.WriteString("```\n")
	sb.WriteString("\nUse RepoMap for detailed module/dependency analysis. Use Read with offset/limit for large files.\n")
	return sb.String()
}

// topNExts returns the top N file extensions as a compact string.
func topNExts(exts map[string]int, n int) string {
	type extCount struct {
		ext   string
		count int
	}
	sorted := make([]extCount, 0, len(exts))
	for e, c := range exts {
		sorted = append(sorted, extCount{e, c})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	parts := make([]string, len(sorted))
	for i, ec := range sorted {
		parts[i] = fmt.Sprintf("%s(%d)", ec.ext, ec.count)
	}
	return strings.Join(parts, " ")
}

// resolveSkills discovers the catalog for workingDir, applies the agent's
// manifest config plus any CLI overrides, and returns both the filtered
// entries and the rendered Level-1 system-prompt block. The entries are the
// authoritative set surfaced to the agent — the same list is later handed to
// the Skill tool so the names the model sees are exactly the names it can
// load.
//
// Returns (nil, "", nil) when skills are disabled or no entries survive the
// filter, so callers can branch on either.
func resolveSkills(workingDir string, manifestCfg *SkillsConfig, overrides *SkillOverrides, catalogPaths []string) ([]skill.Entry, string, error) {
	enabled := true
	if manifestCfg != nil && manifestCfg.Enabled != nil {
		enabled = *manifestCfg.Enabled
	}
	if overrides != nil && overrides.Enabled != nil {
		enabled = *overrides.Enabled
	}
	if !enabled {
		return nil, "", nil
	}

	catalog, err := skill.Discover(skill.FindRepoRoot(workingDir), catalogPaths...)
	if err != nil {
		return nil, "", fmt.Errorf("discover skills: %w", err)
	}
	for _, le := range catalog.LoadErrors() {
		logging.Warn("skipping invalid skill %s: %v", le.Path, le.Err)
	}

	filterOpts := skill.FilterOptions{}
	if manifestCfg != nil {
		for _, s := range manifestCfg.Scopes {
			scope, err := skill.ParseScope(s)
			if err != nil {
				return nil, "", fmt.Errorf("agent skills config: %w", err)
			}
			filterOpts.Scopes = append(filterOpts.Scopes, scope)
		}
		filterOpts.Allow = manifestCfg.Allow
		filterOpts.Deny = manifestCfg.Deny
	}
	if overrides != nil {
		if len(overrides.Allow) > 0 {
			filterOpts.Allow = overrides.Allow
		}
		if len(overrides.Deny) > 0 {
			filterOpts.Deny = overrides.Deny
		}
	}

	entries := catalog.Filter(filterOpts)
	return entries, skill.RenderPromptBlock(entries), nil
}

// buildSystemMessage assembles the system prompt content from all bundle components.
func buildSystemMessage(manifest *Manifest, displayMode, workingDir, wrapperContent, systemContent, taskContent, skillBlock string, refs []string) bytes.Buffer {
	var sys bytes.Buffer
	sys.WriteString("# Squad Agent Bundle\n\n")
	fmt.Fprintf(&sys, "Agent: %s (%s)\n", manifest.Name, manifest.Version)
	fmt.Fprintf(&sys, "Mode: %s\n", displayMode)
	fmt.Fprintf(&sys, "Working Directory: %s\n\n", workingDir)
	sys.WriteString("## Agent Wrapper\n\n")
	sys.WriteString(wrapperContent)
	sys.WriteString("\n\n## System Prompt\n\n")
	sys.WriteString(systemContent)

	if len(refs) > 0 {
		sys.WriteString("\n\n## References\n\n")
		for _, ref := range refs {
			sys.WriteString(ref)
			sys.WriteString("\n")
		}
	}

	if taskContent != "" {
		sys.WriteString("\n\n## Task\n\n")
		sys.WriteString(taskContent)
		sys.WriteString("\n")
	}

	if manifest.Output != nil && manifest.Output.Format == "json" {
		sys.WriteString("\n\n## Output Contract\n\n")
		sys.WriteString("You MUST emit your final response as a single JSON object.\n")
		sys.WriteString("Do not wrap it in markdown code fences.\n")
		if len(manifest.Output.Schema) > 0 {
			schemaBytes, schemaErr := json.MarshalIndent(manifest.Output.Schema, "", "  ")
			if schemaErr == nil {
				sys.WriteString("\nYour output must conform to this JSON Schema:\n\n")
				sys.Write(schemaBytes)
				sys.WriteString("\n")
			}
		}
	}

	// Inject tool efficiency instructions into all agents.
	sys.WriteString("\n\n")
	sys.WriteString(toolEfficiencyPrompt)

	if skillBlock != "" {
		sys.WriteString("\n")
		sys.WriteString(skillBlock)
	}

	// Inject compact repo structure summary so agents don't waste
	// iterations discovering the basic layout.
	if repoSummary := compactRepoSummary(workingDir); repoSummary != "" {
		sys.WriteString("\n")
		sys.WriteString(repoSummary)
	}

	return sys
}

// BuildBundle assembles a [Bundle] from the named agent in agentsDir.
// It loads the manifest, processes system/wrapper/task templates with the
// given mode and vars, resolves references and MCP server configs, and
// constructs the combined system and user messages. prompt becomes the user
// message (defaulting to "Begin." when empty).
//
// Callers that need to overlay the agent.yaml skills config — e.g. CLI flags
// like --allow-skill / --skills-disabled — should use [BuildBundleWithOptions]
// instead.
func BuildBundle(agentsDir, agentName, prompt, workingDir, mode string, vars map[string]string) (*Bundle, error) {
	return BuildBundleWithOptions(agentsDir, agentName, prompt, workingDir, mode, vars, nil)
}

// BuildBundleWithOptions is the full-featured constructor that also accepts
// per-run overrides via opts. A nil opts is equivalent to [BuildBundle].
func BuildBundleWithOptions(agentsDir, agentName, prompt, workingDir, mode string, vars map[string]string, opts *BundleOptions) (*Bundle, error) {
	agentPath := filepath.Join(agentsDir, agentName)

	manifest, err := LoadManifest(agentPath)
	if err != nil {
		return nil, err
	}

	displayMode := resolveDisplayMode(mode)
	data := TemplateData{Mode: displayMode, Vars: vars, AgentDir: agentPath}

	var skillOverrides *SkillOverrides
	var catalogPaths []string
	if opts != nil {
		skillOverrides = opts.SkillOverrides
		catalogPaths = opts.CatalogPaths
	}
	skillEntries, skillBlock, err := resolveSkills(workingDir, manifest.Skills, skillOverrides, catalogPaths)
	if err != nil {
		return nil, err
	}

	var sys bytes.Buffer
	if manifest.IsInlinePrompt() {
		systemContent, perr := processTemplate("prompt", manifest.Prompt, agentsDir, data)
		if perr != nil {
			return nil, perr
		}
		sys = buildInlineSystemMessage(manifest, displayMode, workingDir, systemContent, skillBlock)
	} else {
		systemContent, wrapperContent, taskContent, lerr := loadAndProcessPrompts(agentPath, agentsDir, manifest, data)
		if lerr != nil {
			return nil, lerr
		}
		refs, rerr := loadReferences(agentPath, manifest.References)
		if rerr != nil {
			return nil, rerr
		}
		sys = buildSystemMessage(manifest, displayMode, workingDir, wrapperContent, systemContent, taskContent, skillBlock, refs)
	}

	userMessage := prompt
	if userMessage == "" {
		userMessage = "Begin."
	}

	var combined bytes.Buffer
	combined.Write(sys.Bytes())
	combined.WriteString("\n\n## User Message\n\n")
	combined.WriteString(userMessage)
	combined.WriteString("\n")

	if manifest.Environment != nil {
		if err := resolveEnvironmentTemplates(manifest.Environment, data); err != nil {
			return nil, err
		}
	}

	resolvedMCP, err := resolveMCPServerTemplates(manifest.MCPServers, data)
	if err != nil {
		return nil, err
	}

	primary := manifest.PrimaryModel()

	return &Bundle{
		System:        sys.String(),
		User:          userMessage,
		Combined:      combined.Bytes(),
		WorkDir:       workingDir,
		Model:         primary.Model,
		Provider:      primary.Provider,
		BaseURL:       primary.BaseURL,
		Models:        manifest.Models,
		Budget:        manifest.Budget,
		Environment:   manifest.Environment,
		MCPServers:    resolvedMCP,
		DisableTask:   manifest.DisableTask,
		MaxIterations: manifest.MaxIterations,
		EditDeadline:  manifest.EditDeadline,
		RemoteOnly:    manifest.IsRemoteOnly(),
		SkillEntries:  skillEntries,
	}, nil
}

// buildInlineSystemMessage assembles the system message for an inline-prompt
// agent. It skips wrapper/references/task sections and (for remote-only
// agents) the repo summary.
func buildInlineSystemMessage(manifest *Manifest, displayMode, workingDir, promptContent, skillBlock string) bytes.Buffer {
	var sys bytes.Buffer
	sys.WriteString("# Squad Agent Bundle\n\n")
	fmt.Fprintf(&sys, "Agent: %s (%s)\n", manifest.Name, manifest.Version)
	fmt.Fprintf(&sys, "Mode: %s\n", displayMode)
	if !manifest.IsRemoteOnly() {
		fmt.Fprintf(&sys, "Working Directory: %s\n", workingDir)
	}
	sys.WriteString("\n## System Prompt\n\n")
	sys.WriteString(promptContent)

	if manifest.Output != nil && manifest.Output.Format == "json" {
		sys.WriteString("\n\n## Output Contract\n\n")
		sys.WriteString("You MUST emit your final response as a single JSON object.\n")
		sys.WriteString("Do not wrap it in markdown code fences.\n")
		if len(manifest.Output.Schema) > 0 {
			schemaBytes, schemaErr := json.MarshalIndent(manifest.Output.Schema, "", "  ")
			if schemaErr == nil {
				sys.WriteString("\nYour output must conform to this JSON Schema:\n\n")
				sys.Write(schemaBytes)
				sys.WriteString("\n")
			}
		}
	}

	sys.WriteString("\n\n")
	sys.WriteString(toolEfficiencyPrompt)

	if skillBlock != "" {
		sys.WriteString("\n")
		sys.WriteString(skillBlock)
	}

	if !manifest.IsRemoteOnly() {
		if repoSummary := compactRepoSummary(workingDir); repoSummary != "" {
			sys.WriteString("\n")
			sys.WriteString(repoSummary)
		}
	}
	return sys
}

// InlineAgentConfig holds the configuration for an agent defined inline
// within a composed agent's stage.
type InlineAgentConfig struct {
	Name       string            // stage name, used as the agent identity
	EntryPoint string            // path to system prompt, relative to base dir
	Wrapper    string            // path to wrapper prompt, relative to base dir
	Task       string            // path to task prompt, relative to base dir
	Models     []ModelPreference // model preferences
	References []string          // paths to reference files, relative to base dir
}

// resolveInlinePromptDir returns the directory containing the inline agent's
// prompt files. It checks stages/<name>/ first (for stage-specific prompts),
// then falls back to baseDir.
func resolveInlinePromptDir(baseDir, name, entryPoint string) string {
	stageDir := filepath.Join(baseDir, "stages", name)
	if _, err := os.Stat(filepath.Join(stageDir, entryPoint)); err == nil {
		return stageDir
	}
	return baseDir
}

// BuildBundleInline assembles a bundle from inline agent config.
// baseDir is the composed agent's directory where prompt files live.
// Files are resolved with progressive lookup: stages/<name>/ first, then baseDir.
func BuildBundleInline(baseDir string, cfg *InlineAgentConfig, prompt, workingDir, mode string, vars map[string]string) (*Bundle, error) {
	// Resolve the prompt directory: check stages/<name>/ first, then baseDir.
	promptDir := resolveInlinePromptDir(baseDir, cfg.Name, cfg.EntryPoint)

	manifest := &Manifest{
		Name:       cfg.Name,
		Version:    "inline",
		EntryPoint: cfg.EntryPoint,
		Wrapper:    cfg.Wrapper,
		Task:       cfg.Task,
		Models:     cfg.Models,
		References: cfg.References,
	}

	displayMode := resolveDisplayMode(mode)

	data := TemplateData{Mode: displayMode, Vars: vars, AgentDir: promptDir}
	systemContent, wrapperContent, taskContent, err := loadAndProcessPrompts(promptDir, baseDir, manifest, data)
	if err != nil {
		return nil, err
	}

	// References are resolved from baseDir (shared) or promptDir (stage-specific).
	refs, err := loadReferences(promptDir, manifest.References)
	if err != nil {
		// Fall back to baseDir for shared references.
		refs, err = loadReferences(baseDir, manifest.References)
		if err != nil {
			return nil, err
		}
	}

	_, skillBlock, err := resolveSkills(workingDir, manifest.Skills, nil, nil)
	if err != nil {
		return nil, err
	}
	sys := buildSystemMessage(manifest, displayMode, workingDir, wrapperContent, systemContent, taskContent, skillBlock, refs)

	userMessage := prompt
	if userMessage == "" {
		userMessage = "Begin."
	}

	var combined bytes.Buffer
	combined.Write(sys.Bytes())
	combined.WriteString("\n\n## User Message\n\n")
	combined.WriteString(userMessage)
	combined.WriteString("\n")

	primary := manifest.PrimaryModel()

	return &Bundle{
		System:   sys.String(),
		User:     userMessage,
		Combined: combined.Bytes(),
		WorkDir:  workingDir,
		Model:    primary.Model,
		Provider: primary.Provider,
		BaseURL:  primary.BaseURL,
		Models:   manifest.Models,
	}, nil
}
