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

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/mcp"
	"gopkg.in/yaml.v3"
)

// ModelPreference represents a ranked model recommendation for an agent.
// The first entry in a models list is the primary (preferred) model.
type ModelPreference struct {
	Model    string `yaml:"model"`
	Provider string `yaml:"provider"`
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
	Models        []ModelPreference  // ranked model preferences from manifest
	Budget        *BudgetConfig      // budget configuration from manifest
	Environment   *executor.Config   // execution environment from agent manifest
	MCPServers    []mcp.ServerConfig // MCP server dependencies declared in agent.yaml
	DisableTask   bool               // when true, the Task tool is not registered for this agent
	MaxIterations int                // iteration cap from manifest (0 = use CLI default)
	EditDeadline  int                // stop after N iterations with no Edit calls (0 = disabled)
}

// TemplateData holds the data passed to prompt templates.
// Templates can use {{.Mode}}, {{.Var "KEY"}}, or {{.Vars.KEY}}.
type TemplateData struct {
	Mode string
	Vars map[string]string
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

// processTemplate executes a Go text/template with the given data.
// Templates can use {{if eq .Mode "edit"}}...{{end}} conditionals.
// Templates can use {{include "hard-rules/universal.md"}} to include shared content.
// Templates can use {{.Var "KEY"}} or {{.Default "KEY" "default"}} for custom variables.
func processTemplate(name, content, agentsDir string, data TemplateData) (string, error) {
	if data.Mode == "" {
		data.Mode = "edit"
	}

	funcMap := template.FuncMap{
		"include": makeIncludeFunc(agentsDir),
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

// loadReferences reads all reference files and returns formatted content.
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

// BuildBundle assembles the agent bundle from manifest, system prompt, wrapper, references, and task.
// The task instructions are included in the system bundle. The CLI prompt becomes the user message.
// If no CLI prompt is provided, a default user message is used.
// The vars parameter allows passing custom template variables (e.g., COVERAGE_TARGET=85).
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
	if dir == "." {
		dir = "."
	}
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

// buildSystemMessage assembles the system prompt content from all bundle components.
func buildSystemMessage(manifest *Manifest, displayMode, workingDir, wrapperContent, systemContent, taskContent string, refs []string) bytes.Buffer {
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

	// Inject compact repo structure summary so agents don't waste
	// iterations discovering the basic layout.
	if repoSummary := compactRepoSummary(workingDir); repoSummary != "" {
		sys.WriteString("\n")
		sys.WriteString(repoSummary)
	}

	return sys
}

func BuildBundle(agentsDir, agentName, prompt, workingDir, mode string, vars map[string]string) (*Bundle, error) {
	agentPath := filepath.Join(agentsDir, agentName)

	manifest, err := LoadManifest(agentPath)
	if err != nil {
		return nil, err
	}

	displayMode := mode
	if displayMode == "" {
		displayMode = "edit"
	}

	data := TemplateData{Mode: mode, Vars: vars}
	systemContent, wrapperContent, taskContent, err := loadAndProcessPrompts(agentPath, agentsDir, manifest, data)
	if err != nil {
		return nil, err
	}

	refs, err := loadReferences(agentPath, manifest.References)
	if err != nil {
		return nil, err
	}

	sys := buildSystemMessage(manifest, displayMode, workingDir, wrapperContent, systemContent, taskContent, refs)

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

	var primaryModel, primaryProvider string
	if len(manifest.Models) > 0 {
		primaryModel = manifest.Models[0].Model
		primaryProvider = manifest.Models[0].Provider
	}

	return &Bundle{
		System:        sys.String(),
		User:          userMessage,
		Combined:      combined.Bytes(),
		WorkDir:       workingDir,
		Model:         primaryModel,
		Provider:      primaryProvider,
		Models:        manifest.Models,
		Budget:        manifest.Budget,
		Environment:   manifest.Environment,
		MCPServers:    resolvedMCP,
		DisableTask:   manifest.DisableTask,
		MaxIterations: manifest.MaxIterations,
		EditDeadline:  manifest.EditDeadline,
	}, nil
}
