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

// IsolationMode controls how the agent's run interacts with the working tree
// and git history before, during, and after the run.
type IsolationMode string

const (
	// IsolationNone runs the agent in the working directory directly with no
	// git operations.
	IsolationNone IsolationMode = "none"
	// IsolationWorktree runs the agent in a fresh git worktree on a new
	// branch derived from the agent name and a timestamp.
	IsolationWorktree IsolationMode = "worktree"
	// IsolationBranch checks out a new branch `squad-<id>` from the current
	// state (including uncommitted changes) before the run. The branch is
	// left checked out afterwards.
	IsolationBranch IsolationMode = "branch"
	// IsolationCommit commits everything in the working tree as
	// `squad: snapshot before <agent>` before the run. No-op when the tree
	// is clean.
	IsolationCommit IsolationMode = "commit"
	// IsolationStaged stashes unstaged changes, commits the index as
	// `squad: staged snapshot before <agent>`, then restores the unstaged
	// changes on completion. No-op when nothing is staged.
	IsolationStaged IsolationMode = "staged"
	// IsolationUnstaged runs the agent against the working tree as-is. No
	// git operations occur before or after the run. Distinct from
	// IsolationNone only as user intent.
	IsolationUnstaged IsolationMode = "unstaged"
)

// allIsolationModes lists every valid mode string accepted by
// ResolveIsolationMode. Kept in sync with the constants above.
var allIsolationModes = []IsolationMode{
	IsolationNone,
	IsolationWorktree,
	IsolationBranch,
	IsolationCommit,
	IsolationStaged,
	IsolationUnstaged,
}

// ResolveIsolationMode picks an effective IsolationMode using precedence:
// CLI flag > agent manifest > config default > IsolationNone.
// Empty strings at any layer mean "not specified, fall through".
func ResolveIsolationMode(cliFlag, manifestVal, configVal string) (IsolationMode, error) {
	for _, raw := range []string{cliFlag, manifestVal, configVal} {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		for _, m := range allIsolationModes {
			if IsolationMode(v) == m {
				return m, nil
			}
		}
		return "", fmt.Errorf("invalid isolation mode %q (want: none, worktree, branch, commit, staged, unstaged)", raw)
	}
	return IsolationNone, nil
}

// Isolation describes the resolved working environment for a run.
// When Mode is IsolationNone, Effective == Original and Teardown is a no-op.
type Isolation struct {
	Mode      IsolationMode
	Original  string // user-facing working directory (cwd or --working-dir)
	Effective string // path the agent actually operates in
	Branch    string // branch name created by branch/worktree modes; empty otherwise

	// stashRef is the stash reference (e.g. "stash@{0}") created by
	// IsolationStaged when there were unstaged changes to set aside; empty
	// when no stash was needed.
	stashRef  string
	cleanedUp bool
}

// PrepareIsolation sets up the run environment for the requested mode.
//
// IsolationWorktree creates `.squad/worktrees/<agent>-<stamp>` on a new branch
// `squad/<agent>-<stamp>` rooted at HEAD of the original repo. If the original
// directory is not a git repository, it logs a warning and downgrades to
// IsolationNone instead of failing the run.
//
// IsolationBranch, IsolationCommit, and IsolationStaged perform their git
// operations in the original directory and require it to be a git repo;
// otherwise an error is returned.
//
// IsolationNone and IsolationUnstaged perform no git operations.
func PrepareIsolation(ctx context.Context, originalDir string, mode IsolationMode, agentName string) (*Isolation, error) {
	iso := &Isolation{Mode: mode, Original: originalDir, Effective: originalDir}

	switch mode {
	case IsolationNone, IsolationUnstaged:
		return iso, nil
	case IsolationWorktree:
		return prepareWorktree(ctx, iso, agentName)
	case IsolationBranch:
		return prepareBranch(ctx, iso, agentName)
	case IsolationCommit:
		return prepareCommit(ctx, iso, agentName)
	case IsolationStaged:
		return prepareStaged(ctx, iso, agentName)
	default:
		return nil, fmt.Errorf("isolation: unknown mode %q", mode)
	}
}

// prepareWorktree creates a git worktree for the agent run. When the working
// directory is not a git repository it logs a warning and downgrades to
// IsolationNone rather than failing.
func prepareWorktree(ctx context.Context, iso *Isolation, agentName string) (*Isolation, error) {
	if !isGitRepo(ctx, iso.Original) {
		logging.Warn("isolation=worktree requested but %s is not a git repository — running in place", iso.Original)
		iso.Mode = IsolationNone
		return iso, nil
	}

	repoRoot, err := gitRepoRoot(ctx, iso.Original)
	if err != nil {
		return nil, fmt.Errorf("isolation: cannot resolve git repo root: %w", err)
	}

	id := newIsolationID(agentName)
	branch := fmt.Sprintf("squad/%s", id)
	worktreesRoot := filepath.Join(repoRoot, ".squad", "worktrees")
	if err := os.MkdirAll(worktreesRoot, 0o755); err != nil {
		return nil, fmt.Errorf("isolation: cannot create worktrees dir: %w", err)
	}
	worktreePath := filepath.Join(worktreesRoot, id)

	if out, err := runGit(ctx, iso.Original, "worktree", "add", "-b", branch, worktreePath, "HEAD"); err != nil {
		return nil, fmt.Errorf("isolation: git worktree add failed: %w (output: %s)", err, out)
	}

	iso.Effective = worktreePath
	iso.Branch = branch
	logging.Info("isolation: worktree %s created on branch %s", worktreePath, branch)
	return iso, nil
}

// prepareBranch checks out a new branch `squad-<id>` in the original
// directory, carrying any uncommitted changes to the new branch.
func prepareBranch(ctx context.Context, iso *Isolation, agentName string) (*Isolation, error) {
	if err := requireGitRepo(ctx, iso.Original, IsolationBranch); err != nil {
		return nil, err
	}
	id := newIsolationID(agentName)
	branch := fmt.Sprintf("squad-%s", id)
	if out, err := runGit(ctx, iso.Original, "checkout", "-b", branch); err != nil {
		return nil, fmt.Errorf("isolation: git checkout -b %s failed: %w (output: %s)", branch, err, out)
	}
	iso.Branch = branch
	logging.Info("isolation: checked out branch %s (existing changes carried over)", branch)
	return iso, nil
}

// prepareCommit stages and commits all working-tree changes as a snapshot
// commit before the run. It is a no-op when the working tree is already clean.
func prepareCommit(ctx context.Context, iso *Isolation, agentName string) (*Isolation, error) {
	if err := requireGitRepo(ctx, iso.Original, IsolationCommit); err != nil {
		return nil, err
	}
	dirty, err := workingTreeDirty(ctx, iso.Original)
	if err != nil {
		return nil, fmt.Errorf("isolation: cannot check working tree status: %w", err)
	}
	if !dirty {
		logging.Info("isolation=commit: working tree clean — no snapshot needed")
		return iso, nil
	}
	if out, err := runGit(ctx, iso.Original, "add", "-A"); err != nil {
		return nil, fmt.Errorf("isolation: git add -A failed: %w (output: %s)", err, out)
	}
	msg := fmt.Sprintf("squad: snapshot before %s", agentName)
	if out, err := runGit(ctx, iso.Original, "commit", "-m", msg); err != nil {
		return nil, fmt.Errorf("isolation: git commit failed: %w (output: %s)", err, out)
	}
	logging.Info("isolation=commit: snapshotted working tree")
	return iso, nil
}

// prepareStaged stashes any unstaged changes (preserving the index via
// --keep-index), commits the staged snapshot, and records the stash ref for
// restoration in [Isolation.Teardown]. It is a no-op when nothing is staged.
func prepareStaged(ctx context.Context, iso *Isolation, agentName string) (*Isolation, error) {
	if err := requireGitRepo(ctx, iso.Original, IsolationStaged); err != nil {
		return nil, err
	}
	staged, err := hasStagedChanges(ctx, iso.Original)
	if err != nil {
		return nil, fmt.Errorf("isolation: cannot inspect staged changes: %w", err)
	}
	if !staged {
		logging.Info("isolation=staged: index empty — no snapshot needed")
		return iso, nil
	}

	hadUnstaged, err := hasUnstagedChanges(ctx, iso.Original)
	if err != nil {
		return nil, fmt.Errorf("isolation: cannot inspect unstaged changes: %w", err)
	}
	if hadUnstaged {
		// --keep-index ensures the staged snapshot stays in the index so we
		// can commit it after stashing the rest of the working tree.
		if out, err := runGit(ctx, iso.Original, "stash", "push", "--keep-index", "-u", "-m", "squad: unstaged snapshot"); err != nil {
			return nil, fmt.Errorf("isolation: git stash push failed: %w (output: %s)", err, out)
		}
		iso.stashRef = "stash@{0}"
	}

	msg := fmt.Sprintf("squad: staged snapshot before %s", agentName)
	if out, err := runGit(ctx, iso.Original, "commit", "-m", msg); err != nil {
		// Roll back the stash so the user's unstaged changes aren't lost when
		// the commit unexpectedly fails (e.g. pre-commit hook rejection).
		if iso.stashRef != "" {
			if popOut, popErr := runGit(ctx, iso.Original, "stash", "pop"); popErr != nil {
				logging.Warn("isolation: failed to pop stash after commit error: %v (output: %s)", popErr, popOut)
			}
		}
		return nil, fmt.Errorf("isolation: git commit failed: %w (output: %s)", err, out)
	}
	logging.Info("isolation=staged: committed staged snapshot (unstaged stashed=%v)", iso.stashRef != "")
	return iso, nil
}

// Teardown applies the cleanup policy for the resolved mode:
//   - worktree: prune the worktree when it has no changes; keep it otherwise.
//   - staged: restore the previously stashed unstaged changes (always, even
//     on agent failure, so the user's working tree isn't silently lost).
//   - none, branch, commit, unstaged: no-op.
//
// Idempotent. Returns whether the worktree was kept and its filesystem path
// (worktree mode only).
func (i *Isolation) Teardown(ctx context.Context) (kept bool, path string) {
	if i == nil || i.cleanedUp {
		return false, ""
	}
	i.cleanedUp = true

	switch i.Mode {
	case IsolationWorktree:
		return i.teardownWorktree(ctx)
	case IsolationStaged:
		i.teardownStaged(ctx)
	}
	return false, ""
}

// teardownWorktree removes the worktree and its branch when the agent
// produced no changes; otherwise it retains both for the caller to review.
func (i *Isolation) teardownWorktree(ctx context.Context) (kept bool, path string) {
	if worktreeHasChanges(ctx, i.Effective) {
		logging.Info("isolation: worktree retained at %s (branch %s) — agent produced changes", i.Effective, i.Branch)
		return true, i.Effective
	}
	if out, err := runGit(ctx, i.Original, "worktree", "remove", "--force", i.Effective); err != nil {
		logging.Warn("isolation: failed to remove empty worktree %s: %v (output: %s)", i.Effective, err, out)
		return true, i.Effective
	}
	if out, err := runGit(ctx, i.Original, "branch", "-D", i.Branch); err != nil {
		logging.Warn("isolation: failed to delete empty branch %s: %v (output: %s)", i.Branch, err, out)
	}
	logging.Info("isolation: worktree %s pruned (no changes)", i.Effective)
	return false, ""
}

// teardownStaged pops the stash created by prepareStaged, restoring unstaged
// changes to the working tree. It runs even when the agent failed so the
// user's in-progress work is never silently discarded.
func (i *Isolation) teardownStaged(ctx context.Context) {
	if i.stashRef == "" {
		return
	}
	// Pop even on agent failure: the stash holds the user's unstaged work
	// and must be restored before we hand the working tree back.
	if out, err := runGit(ctx, i.Original, "stash", "pop"); err != nil {
		logging.Warn("isolation=staged: failed to restore unstaged changes from %s: %v (output: %s)", i.stashRef, err, out)
		return
	}
	logging.Info("isolation=staged: restored unstaged changes from stash")
}

// newIsolationID builds the per-run identifier used in branch names, worktree
// paths, and snapshot messages. Format: `<slug>-<UTC stamp>`.
func newIsolationID(agentName string) string {
	stamp := time.Now().UTC().Format("20060102-150405")
	slug := sanitizeForBranch(agentName)
	if slug == "" {
		slug = "agent"
	}
	return fmt.Sprintf("%s-%s", slug, stamp)
}

// requireGitRepo returns an error when dir is not inside a git repository,
// providing a user-friendly message that names the failing isolation mode.
func requireGitRepo(ctx context.Context, dir string, mode IsolationMode) error {
	if !isGitRepo(ctx, dir) {
		return fmt.Errorf("isolation=%s requires a git repository (run from inside one): %s", mode, dir)
	}
	return nil
}

// isGitRepo reports whether dir is inside a git working tree.
func isGitRepo(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// gitRepoRoot returns the absolute path of the top-level git repository
// containing dir.
func gitRepoRoot(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// worktreeHasChanges reports whether dir has any uncommitted changes
// (tracked or untracked). On git command failure it returns true to avoid
// discarding a worktree that may contain agent output.
func worktreeHasChanges(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) != ""
}

// workingTreeDirty reports whether the working tree in dir has any uncommitted
// changes according to git status --porcelain.
func workingTreeDirty(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// hasStagedChanges reports whether the index in dir differs from HEAD.
func hasStagedChanges(ctx context.Context, dir string) (bool, error) {
	// `git diff --cached --quiet` exits 0 when index matches HEAD, 1 when not.
	cmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--quiet")
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

// hasUnstagedChanges reports whether dir has tracked modifications or untracked
// files that would be picked up by `git stash push -u`.
func hasUnstagedChanges(ctx context.Context, dir string) (bool, error) {
	// Tracked-file changes:
	tracked := exec.CommandContext(ctx, "git", "diff", "--quiet")
	tracked.Dir = dir
	if err := tracked.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil
		}
		return false, err
	}
	// Untracked files (matters because `stash push -u` will pick them up):
	untracked := exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard")
	untracked.Dir = dir
	out, err := untracked.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// runGit runs a git command in dir, capturing combined output for inclusion
// in error messages.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// sanitizeForBranch converts s to a string safe for use in a git branch name:
// letters and digits are kept, uppercase is lowercased, hyphens and underscores
// are kept, and all other characters are replaced with hyphens. Leading and
// trailing hyphens are stripped.
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
