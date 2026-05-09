package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/logging"
)

// IsolationMode controls whether an agent runs against the user's working
// tree directly or inside a dedicated git worktree on a fresh branch.
type IsolationMode string

const (
	// IsolationNone runs the agent in the working directory directly.
	IsolationNone IsolationMode = "none"
	// IsolationWorktree runs the agent in a fresh git worktree on a new
	// branch derived from the agent name and a timestamp.
	IsolationWorktree IsolationMode = "worktree"
)

// ResolveIsolationMode picks an effective IsolationMode using precedence:
// CLI flag > agent manifest > config default > IsolationNone.
// Empty strings at any layer mean "not specified, fall through".
func ResolveIsolationMode(cliFlag, manifestVal, configVal string) (IsolationMode, error) {
	for _, raw := range []string{cliFlag, manifestVal, configVal} {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		switch IsolationMode(v) {
		case IsolationNone, IsolationWorktree:
			return IsolationMode(v), nil
		default:
			return "", fmt.Errorf("invalid isolation mode %q (want: none, worktree)", raw)
		}
	}
	return IsolationNone, nil
}

// Isolation describes the resolved working environment for a run.
// When Mode is IsolationNone, Effective == Original and Teardown is a no-op.
type Isolation struct {
	Mode      IsolationMode
	Original  string // user-facing working directory (cwd or --working-dir)
	Effective string // path the agent actually operates in
	Branch    string // worktree branch name; empty when Mode is none
	cleanedUp bool
}

// PrepareIsolation sets up the run environment for the requested mode.
// For IsolationWorktree it creates `.squad/worktrees/<agent>-<stamp>` on a
// new branch `squad/<agent>-<stamp>` rooted at HEAD of the original repo.
// If the original directory is not a git repository, it logs a warning and
// downgrades to IsolationNone instead of failing the run.
func PrepareIsolation(ctx context.Context, originalDir string, mode IsolationMode, agentName string) (*Isolation, error) {
	iso := &Isolation{Mode: mode, Original: originalDir, Effective: originalDir}
	if mode != IsolationWorktree {
		return iso, nil
	}

	if !isGitRepo(ctx, originalDir) {
		logging.Warn("isolation=worktree requested but %s is not a git repository — running in place", originalDir)
		iso.Mode = IsolationNone
		return iso, nil
	}

	repoRoot, err := gitRepoRoot(ctx, originalDir)
	if err != nil {
		return nil, fmt.Errorf("isolation: cannot resolve git repo root: %w", err)
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	slug := sanitizeForBranch(agentName)
	if slug == "" {
		slug = "agent"
	}
	branch := fmt.Sprintf("squad/%s-%s", slug, stamp)
	worktreesRoot := filepath.Join(repoRoot, ".squad", "worktrees")
	if err := os.MkdirAll(worktreesRoot, 0o755); err != nil {
		return nil, fmt.Errorf("isolation: cannot create worktrees dir: %w", err)
	}
	worktreePath := filepath.Join(worktreesRoot, fmt.Sprintf("%s-%s", slug, stamp))

	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, worktreePath, "HEAD")
	cmd.Dir = originalDir
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("isolation: git worktree add failed: %w (output: %s)", err, strings.TrimSpace(out.String()))
	}

	iso.Effective = worktreePath
	iso.Branch = branch
	logging.Info("isolation: worktree %s created on branch %s", worktreePath, branch)
	return iso, nil
}

// Teardown applies the cleanup policy: prune the worktree when it has no
// changes; keep it (with branch) when the agent produced changes so the
// user can review, merge, or discard manually. Idempotent.
// Returns whether the worktree was kept and its filesystem path.
func (i *Isolation) Teardown(ctx context.Context) (kept bool, path string) {
	if i == nil || i.Mode != IsolationWorktree || i.cleanedUp {
		return false, ""
	}
	i.cleanedUp = true

	if worktreeHasChanges(ctx, i.Effective) {
		logging.Info("isolation: worktree retained at %s (branch %s) — agent produced changes", i.Effective, i.Branch)
		return true, i.Effective
	}

	rm := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", i.Effective)
	rm.Dir = i.Original
	if err := rm.Run(); err != nil {
		logging.Warn("isolation: failed to remove empty worktree %s: %v", i.Effective, err)
		return true, i.Effective
	}
	br := exec.CommandContext(ctx, "git", "branch", "-D", i.Branch)
	br.Dir = i.Original
	if err := br.Run(); err != nil {
		logging.Warn("isolation: failed to delete empty branch %s: %v", i.Branch, err)
	}
	logging.Info("isolation: worktree %s pruned (no changes)", i.Effective)
	return false, ""
}

func isGitRepo(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	return cmd.Run() == nil
}

func gitRepoRoot(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func worktreeHasChanges(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) != ""
}

func sanitizeForBranch(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			sb.WriteRune(r + 32)
		case r == '-' || r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteByte('-')
		}
	}
	return strings.Trim(sb.String(), "-")
}
