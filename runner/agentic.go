package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/activity"
	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/agenticcli"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/mcp"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/skill"
	"github.com/cowdogmoo/squad/tools"
)

// invokeAgenticCLI runs the prompt through a locally installed agentic CLI
// (claude-code or antigravity) in non-interactive print mode. The CLI executes its
// own tool loop directly in the working directory with its own
// authentication, so squad's executor, tool registry, MCP plumbing, and API
// keys are all bypassed. For claude-code squad still sets the limits: the
// tool set, the turn and spend caps, and the MCP servers (applyClaudeLimits).
func invokeAgenticCLI(ctx context.Context, opts *RunOptions, provider, model, systemPrompt string, bundle *agent.Bundle, m *metrics.Metrics) (string, *metrics.Metrics, error) {
	defer m.Finish()

	mcpServers, err := agenticCLIPreflight(ctx, opts, provider, bundle)
	if err != nil {
		return "", m, err
	}

	systemPrompt += skillFileFallback(bundle.SkillEntries)

	before := agenticWorktreeSnapshot(ctx, bundle.WorkDir)
	logging.InfoContext(ctx, "agentic CLI run started (provider=%s model=%s dir=%s)", provider, model, bundle.WorkDir)
	req := agenticcli.Request{
		Provider:     provider,
		Model:        model,
		SystemPrompt: systemPrompt,
		UserPrompt:   bundle.User,
		WorkDir:      bundle.WorkDir,
		ReadOnly:     agent.ModeRestrictsEdits(opts.Mode),
	}
	if provider == "claude-code" {
		cleanup, err := applyClaudeLimits(ctx, &req, opts, bundle, model, mcpServers)
		defer cleanup()
		if err != nil {
			return "", m, err
		}
	}
	res, gate, err := runAgenticCLI(ctx, req)
	m.AddUsage(metrics.Usage{
		InputTokens:        res.InputTokens,
		CacheReadTokens:    res.CacheReadTokens,
		CacheWriteTokens:   res.CacheWriteTokens,
		CacheWrite1hTokens: res.CacheWrite1hTokens,
		OutputTokens:       res.OutputTokens,
	})
	m.AddIterations(res.Turns)
	if gate != nil && gate.wasRejected() {
		// The rejection interrupt cuts the CLI off mid-turn, so its result
		// envelope is an empty error; report the rejection itself, matching
		// the native ProposePlan outcome.
		return "", m, errors.New("sign-off: plan rejected by the user; no changes were made")
	}
	if err != nil {
		activity.Error(ctx, "cli", err)
		return "", m, agenticCLIError(err)
	}
	if before != "" && agenticWorktreeSnapshot(ctx, bundle.WorkDir) != before {
		tools.MarkEditsApplied(ctx)
		logging.InfoContext(ctx, "working tree changed during %s run; marking edits applied", provider)
	}
	logging.InfoContext(ctx, "agentic CLI run finished in %s (turns=%d response-bytes=%d)",
		m.Duration().Round(time.Millisecond), res.Turns, len(res.Response))
	logging.InfoContext(ctx, "agentic CLI usage: %s", m.UsageLogLine())
	return res.Response, m, nil
}

// agenticCLIPreflight rejects manifest guarantees an external CLI cannot
// keep, logs the options it ignores, and returns the MCP servers for the run
// (none for a transform, whose untrusted input gets no tools of any kind).
func agenticCLIPreflight(ctx context.Context, opts *RunOptions, provider string, bundle *agent.Bundle) ([]mcp.ServerConfig, error) {
	if bundle.Environment != nil && bundle.Environment.Type != "" && bundle.Environment.Type != "local" {
		return nil, fmt.Errorf("provider %q runs a CLI on the local host; environment type %q is not supported", provider, bundle.Environment.Type)
	}
	// comments_only and ascii_only are enforced inside squad's Edit tool,
	// which an external CLI never calls; running anyway would silently drop
	// the guarantee the manifest asked for.
	if bundle.CommentsOnly || bundle.ASCIIOnly {
		return nil, fmt.Errorf("provider %q cannot enforce comments_only/ascii_only (squad's edit gates do not apply to an external CLI); use an API provider for this agent", provider)
	}
	if opts.ResumeID != "" {
		logging.Warn("resume is not supported for agentic CLI providers; starting fresh")
	}
	if opts.Stream {
		logging.InfoContext(ctx, "--stream is not supported for agentic CLI providers; ignoring")
	}
	if bundle.Transform {
		return nil, nil
	}
	servers := append(append([]mcp.ServerConfig{}, bundle.MCPServers...), opts.MCPServers...)
	if len(servers) > 0 && provider != "claude-code" {
		logging.WarnContext(ctx, "%d MCP server(s) configured but not passed to the %s CLI; configure them in the CLI itself", len(servers), provider)
	}
	return servers, nil
}

// agenticCLIError wraps a failed CLI run. A claude-code run stopped by its
// spend cap is the same outcome as squad's own loop reaching max_cost, so it
// also wraps metrics.ErrBudgetExceeded and is reported as a budget stop.
func agenticCLIError(err error) error {
	if errors.Is(err, agenticcli.ErrMaxBudget) {
		return fmt.Errorf("model call failed: %w: %w", metrics.ErrBudgetExceeded, err)
	}
	return fmt.Errorf("model call failed: %w", err)
}

// applyClaudeLimits bounds a claude-code run the way squad's own loop bounds
// an API-provider run: the built-in tools squad would register for the
// bundle (none for a transform), a turn cap from the resolved iteration
// budget, a spend cap from max_cost, and the manifest's MCP servers passed
// through a private config file. A server that cannot be expressed as CLI
// config fails the run here, before anything is spent. The returned cleanup
// removes the config file and is always safe to call.
func applyClaudeLimits(ctx context.Context, req *agenticcli.Request, opts *RunOptions, bundle *agent.Bundle, model string, servers []mcp.ServerConfig) (func(), error) {
	req.LimitTools = true
	req.Tools = claudeTools(bundle, req.ReadOnly)
	req.MaxTurns = resolveIterationBudget(opts, bundle, model)
	if opts.MaxCost > 0 {
		req.MaxBudgetUSD = opts.MaxCost
	}
	logging.InfoContext(ctx, "claude-code limits: tools=[%s] max-turns=%d max-budget-usd=%g mcp-servers=%d",
		strings.Join(req.Tools, ","), req.MaxTurns, req.MaxBudgetUSD, len(servers))
	if len(servers) == 0 {
		return func() {}, nil
	}
	path, names, err := writeClaudeMCPConfig(servers)
	if err != nil {
		return func() {}, err
	}
	req.MCPConfigFile = path
	req.RequiredMCPServers = names
	return func() {
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			logging.Warn("failed to remove claude MCP config %s: %v", path, rmErr)
		}
	}, nil
}

// claudeTools maps the tools squad's own loop registers for bundle (see
// tools.BuildHandlers) onto Claude Code built-in tool names. Squad's
// background-shell, SystemInfo and RepoMap tools are covered by Bash;
// squad's Skill tool becomes a Read of the SKILL.md (skillFileFallback). A
// transform gets no tools, and readonly drops the edit tools.
func claudeTools(bundle *agent.Bundle, readOnly bool) []string {
	tools := []string{}
	if bundle.Transform {
		return tools
	}
	switch {
	case !bundle.RemoteOnly:
		tools = append(tools, "Bash", "Edit", "Glob", "Grep", "Read", "Write")
	case len(bundle.SkillEntries) > 0:
		tools = append(tools, "Read")
	}
	if !bundle.DisableTask {
		tools = append(tools, "Task")
	}
	if readOnly {
		tools = slices.DeleteFunc(tools, func(t string) bool { return t == "Edit" || t == "Write" })
	}
	return tools
}

// claudeMCPServer is one entry of the claude CLI's mcpServers config.
type claudeMCPServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// claudeMCPConfig translates squad MCP server configs into the claude CLI's
// {"mcpServers": {...}} document and returns the server names. Any server it
// cannot translate is an error: running without it would leave the agent
// short of the tools its prompt depends on.
func claudeMCPConfig(servers []mcp.ServerConfig) ([]byte, []string, error) {
	entries := make(map[string]claudeMCPServer, len(servers))
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		entry, err := claudeMCPEntry(s)
		if err != nil {
			return nil, nil, fmt.Errorf("MCP server %q cannot be passed to the claude CLI: %w", s.Name, err)
		}
		if _, dup := entries[s.Name]; dup {
			return nil, nil, fmt.Errorf("MCP server %q is configured twice", s.Name)
		}
		entries[s.Name] = entry
		names = append(names, s.Name)
	}
	doc, err := json.Marshal(map[string]any{"mcpServers": entries})
	if err != nil {
		return nil, nil, fmt.Errorf("encode claude MCP config: %w", err)
	}
	return doc, names, nil
}

func claudeMCPEntry(s mcp.ServerConfig) (claudeMCPServer, error) {
	if strings.TrimSpace(s.Name) == "" {
		return claudeMCPServer{}, errors.New("the server has no name")
	}
	switch t := s.TransportType(); t {
	case "stdio":
		if strings.TrimSpace(s.Command) == "" {
			return claudeMCPServer{}, errors.New("stdio transport needs a command")
		}
		env, err := keyValues(s.Env, "env")
		if err != nil {
			return claudeMCPServer{}, err
		}
		return claudeMCPServer{Type: "stdio", Command: s.Command, Args: s.Args, Env: env}, nil
	case "sse", "streamable_http", "http":
		if strings.TrimSpace(s.URL) == "" {
			return claudeMCPServer{}, fmt.Errorf("%s transport needs a url", t)
		}
		headers, err := keyValues(s.Headers, "header")
		if err != nil {
			return claudeMCPServer{}, err
		}
		typ := "http"
		if t == "sse" {
			typ = "sse"
		}
		return claudeMCPServer{Type: typ, URL: s.URL, Headers: headers}, nil
	default:
		return claudeMCPServer{}, fmt.Errorf("unsupported transport %q", t)
	}
}

// keyValues parses KEY=VALUE strings into a map.
func keyValues(pairs []string, what string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, kv := range pairs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("%s entry %q is not KEY=VALUE", what, kv)
		}
		out[k] = v
	}
	return out, nil
}

// writeClaudeMCPConfig writes the translated config to a private temp file
// (mode 0600) and returns its path and the server names.
func writeClaudeMCPConfig(servers []mcp.ServerConfig) (string, []string, error) {
	doc, names, err := claudeMCPConfig(servers)
	if err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp("", "squad-claude-mcp-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create claude MCP config: %w", err)
	}
	path := f.Name()
	_, werr := f.Write(doc)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("write claude MCP config: %w", err)
	}
	return path, names, nil
}

// skillFileFallback rewrites squad's Skill-tool affordance for external CLIs:
// the system prompt's "Available skills" block tells the model to call
// Skill(name), which doesn't exist in a CLI session, so append an override
// pointing at each SKILL.md to read directly instead.
func skillFileFallback(entries []skill.Entry) string {
	if len(entries) == 0 {
		return ""
	}
	sorted := make([]skill.Entry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name() < sorted[j].Name() })

	var sb strings.Builder
	sb.WriteString("\n\n## Skill loading override\n\n")
	sb.WriteString("The `Skill` tool is NOT available in this session. To load a skill listed above, read its SKILL.md file directly:\n\n")
	for _, e := range sorted {
		fmt.Fprintf(&sb, "- %s: %s\n", e.Name(), filepath.Join(e.Dir, skill.FileName))
	}
	return sb.String()
}

// agenticWorktreeSnapshot fingerprints the git working tree (porcelain status
// plus tracked-file diff) so edits made out-of-band by an agentic CLI can be
// detected afterwards. Returns "" when the tree can't be fingerprinted (not a
// git repo, or git failed) — callers treat that as "detection unavailable".
func agenticWorktreeSnapshot(ctx context.Context, dir string) string {
	if !isGitRepo(ctx, dir) {
		return ""
	}
	h := sha256.New()
	for _, args := range [][]string{{"status", "--porcelain"}, {"diff", "HEAD"}} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		h.Write(out)
	}
	return hex.EncodeToString(h.Sum(nil))
}
