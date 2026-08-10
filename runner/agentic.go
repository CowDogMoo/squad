package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/agenticcli"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/skill"
	"github.com/cowdogmoo/squad/tools"
)

// invokeAgenticCLI runs the prompt through a locally installed agentic CLI
// (claude-code or agy) in non-interactive print mode. The CLI executes its
// own tool loop directly in the working directory with its own
// authentication, so squad's executor, tool registry, MCP plumbing, and API
// keys are all bypassed.
func invokeAgenticCLI(ctx context.Context, opts *RunOptions, provider, model, systemPrompt string, bundle *agent.Bundle, m *metrics.Metrics) (string, *metrics.Metrics, error) {
	defer m.Finish()

	if bundle.Environment != nil && bundle.Environment.Type != "" && bundle.Environment.Type != "local" {
		return "", m, fmt.Errorf("provider %q runs a CLI on the local host; environment type %q is not supported", provider, bundle.Environment.Type)
	}
	// comments_only and ascii_only are enforced inside squad's Edit tool,
	// which an external CLI never calls; running anyway would silently drop
	// the guarantee the manifest asked for.
	if bundle.CommentsOnly || bundle.ASCIIOnly {
		return "", m, fmt.Errorf("provider %q cannot enforce comments_only/ascii_only (squad's edit gates do not apply to an external CLI); use an API provider for this agent", provider)
	}
	if n := len(bundle.MCPServers) + len(opts.MCPServers); n > 0 {
		logging.WarnContext(ctx, "%d MCP server(s) configured but not passed to the %s CLI; configure them in the CLI itself", n, provider)
	}
	if opts.ResumeID != "" {
		logging.Warn("resume is not supported for agentic CLI providers; starting fresh")
	}
	if opts.Stream {
		logging.InfoContext(ctx, "--stream is not supported for agentic CLI providers; ignoring")
	}

	systemPrompt += skillFileFallback(bundle.SkillEntries)

	before := agenticWorktreeSnapshot(ctx, bundle.WorkDir)
	logging.InfoContext(ctx, "agentic CLI run started (provider=%s model=%s dir=%s)", provider, model, bundle.WorkDir)
	res, err := agenticcli.Run(ctx, agenticcli.Request{
		Provider:     provider,
		Model:        model,
		SystemPrompt: systemPrompt,
		UserPrompt:   bundle.User,
		WorkDir:      bundle.WorkDir,
		ReadOnly:     opts.Mode == "readonly",
	})
	m.AddTokens(res.InputTokens, res.OutputTokens)
	m.AddIterations(res.Turns)
	if err != nil {
		return "", m, fmt.Errorf("model call failed: %w", err)
	}
	if before != "" && agenticWorktreeSnapshot(ctx, bundle.WorkDir) != before {
		tools.MarkEditsApplied(ctx)
		logging.InfoContext(ctx, "working tree changed during %s run; marking edits applied", provider)
	}
	logging.InfoContext(ctx, "agentic CLI run finished in %s (turns=%d response-bytes=%d)",
		m.Duration().Round(time.Millisecond), res.Turns, len(res.Response))
	return res.Response, m, nil
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
