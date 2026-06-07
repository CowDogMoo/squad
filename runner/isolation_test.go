package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveIsolationMode(t *testing.T) {
	tests := []struct {
		name     string
		cli      string
		manifest string
		config   string
		want     IsolationMode
		wantErr  bool
	}{
		{"all empty -> none", "", "", "", IsolationNone, false},
		{"cli wins over manifest", "none", "worktree", "", IsolationNone, false},
		{"manifest wins over config", "", "worktree", "none", IsolationWorktree, false},
		{"config used when others empty", "", "", "worktree", IsolationWorktree, false},
		{"case insensitive", "WORKTREE", "", "", IsolationWorktree, false},
		{"trims whitespace", "  worktree ", "", "", IsolationWorktree, false},
		{"branch accepted", "branch", "", "", IsolationBranch, false},
		{"commit accepted", "commit", "", "", IsolationCommit, false},
		{"staged accepted", "staged", "", "", IsolationStaged, false},
		{"unstaged accepted", "unstaged", "", "", IsolationUnstaged, false},
		{"none accepted", "none", "", "", IsolationNone, false},
		{"invalid value errors", "garbage", "", "", "", true},
		{"invalid manifest errors", "", "garbage", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveIsolationMode(tc.cli, tc.manifest, tc.config)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrepareIsolationNonGitFallsBack(t *testing.T) {
	scrubGitEnv(t)
	dir := t.TempDir()
	iso, err := PrepareIsolation(context.Background(), dir, IsolationWorktree, "agent")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if iso.Mode != IsolationNone {
		t.Errorf("non-git dir should downgrade to IsolationNone, got %q", iso.Mode)
	}
	if iso.Effective != dir {
		t.Errorf("Effective = %q, want %q", iso.Effective, dir)
	}
}

func TestPrepareIsolationNoneIsNoop(t *testing.T) {
	dir := t.TempDir()
	iso, err := PrepareIsolation(context.Background(), dir, IsolationNone, "agent")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if iso.Effective != dir || iso.Branch != "" {
		t.Errorf("expected no-op isolation, got effective=%q branch=%q", iso.Effective, iso.Branch)
	}
	kept, _ := iso.Teardown(context.Background())
	if kept {
		t.Error("Teardown for IsolationNone should report kept=false")
	}
}

func TestWorktreeRoundtripPrunesWhenEmpty(t *testing.T) {
	dir := initGitRepo(t)
	iso, err := PrepareIsolation(context.Background(), dir, IsolationWorktree, "go-review")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if iso.Mode != IsolationWorktree {
		t.Fatalf("expected IsolationWorktree, got %q", iso.Mode)
	}
	if iso.Effective == dir {
		t.Fatalf("Effective should differ from original, both = %q", dir)
	}
	if _, statErr := os.Stat(iso.Effective); statErr != nil {
		t.Fatalf("worktree path missing: %v", statErr)
	}

	kept, _ := iso.Teardown(context.Background())
	if kept {
		t.Errorf("expected prune (no changes), but Teardown reported kept=true")
	}
	if _, statErr := os.Stat(iso.Effective); !os.IsNotExist(statErr) {
		t.Errorf("expected worktree dir to be removed, stat err = %v", statErr)
	}
}

func TestWorktreeRoundtripRetainsWhenChanged(t *testing.T) {
	dir := initGitRepo(t)
	iso, err := PrepareIsolation(context.Background(), dir, IsolationWorktree, "go-tests")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}

	if writeErr := os.WriteFile(filepath.Join(iso.Effective, "new.txt"), []byte("hi"), 0o644); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}

	kept, path := iso.Teardown(context.Background())
	if !kept {
		t.Error("expected retention when worktree has changes")
	}
	if path != iso.Effective {
		t.Errorf("kept path = %q, want %q", path, iso.Effective)
	}
	if _, statErr := os.Stat(iso.Effective); statErr != nil {
		t.Errorf("retained worktree should still exist: %v", statErr)
	}
}

func TestPrepareIsolationUnstagedNoop(t *testing.T) {
	dir := initGitRepo(t)
	// Add a dirty change to verify it is left untouched.
	writeFile(t, dir, "dirty.txt", "untouched")

	iso, err := PrepareIsolation(context.Background(), dir, IsolationUnstaged, "agent")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if iso.Mode != IsolationUnstaged || iso.Effective != dir {
		t.Fatalf("unstaged should run in place, got mode=%q effective=%q", iso.Mode, iso.Effective)
	}
	if branchName := currentBranch(t, dir); branchName != "main" {
		t.Errorf("unstaged should not switch branch, on %q", branchName)
	}
	if commits := commitCount(t, dir); commits != 1 {
		t.Errorf("unstaged should not create commits, got %d", commits)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dirty.txt")); statErr != nil {
		t.Errorf("dirty file should still exist: %v", statErr)
	}
	iso.Teardown(context.Background())
}

func TestPrepareIsolationBranchCheckoutsNewBranch(t *testing.T) {
	dir := initGitRepo(t)
	// A dirty file should carry over to the new branch (per spec).
	writeFile(t, dir, "carry.txt", "dirty content")

	iso, err := PrepareIsolation(context.Background(), dir, IsolationBranch, "go-review")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if iso.Effective != dir {
		t.Fatalf("branch mode runs in place, got effective=%q", iso.Effective)
	}
	if !strings.HasPrefix(iso.Branch, "squad-go-review-") {
		t.Errorf("branch name = %q, want prefix squad-go-review-", iso.Branch)
	}
	if branchName := currentBranch(t, dir); branchName != iso.Branch {
		t.Errorf("expected to be on %q, got %q", iso.Branch, branchName)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "carry.txt")); err != nil || string(data) != "dirty content" {
		t.Errorf("dirty file should carry over: err=%v data=%q", err, string(data))
	}
	if commits := commitCount(t, dir); commits != 1 {
		t.Errorf("branch mode should not create commits, got %d", commits)
	}
	iso.Teardown(context.Background())
}

func TestPrepareIsolationBranchErrorsOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := PrepareIsolation(context.Background(), dir, IsolationBranch, "agent"); err == nil {
		t.Fatal("expected error when not in git repo")
	}
}

func TestPrepareIsolationCommitSnapshotsDirty(t *testing.T) {
	dir := initGitRepo(t)
	writeFile(t, dir, "feature.txt", "new code")

	iso, err := PrepareIsolation(context.Background(), dir, IsolationCommit, "go-review")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if commits := commitCount(t, dir); commits != 2 {
		t.Fatalf("commit mode should add 1 commit, got total %d", commits)
	}
	if msg := lastCommitMessage(t, dir); !strings.Contains(msg, "squad: snapshot before go-review") {
		t.Errorf("commit message = %q, want contains squad: snapshot before go-review", msg)
	}
	if dirty, _ := workingTreeDirty(context.Background(), dir); dirty {
		t.Errorf("working tree should be clean after commit snapshot")
	}
	iso.Teardown(context.Background())
}

func TestPrepareIsolationCommitNoopWhenClean(t *testing.T) {
	dir := initGitRepo(t)
	iso, err := PrepareIsolation(context.Background(), dir, IsolationCommit, "agent")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if commits := commitCount(t, dir); commits != 1 {
		t.Errorf("clean tree should not get an extra commit, got %d", commits)
	}
	iso.Teardown(context.Background())
}

func TestPrepareIsolationStagedCommitsIndexAndRestoresUnstaged(t *testing.T) {
	dir := initGitRepo(t)
	// Create two tracked files committed already.
	writeFile(t, dir, "a.txt", "a-original")
	writeFile(t, dir, "b.txt", "b-original")
	runInDir(t, dir, "git", "add", "a.txt", "b.txt")
	runInDir(t, dir, "git", "commit", "-m", "seed two files")

	// Modify a.txt and stage it; modify b.txt but leave it unstaged.
	writeFile(t, dir, "a.txt", "a-staged-change")
	runInDir(t, dir, "git", "add", "a.txt")
	writeFile(t, dir, "b.txt", "b-unstaged-change")

	commitsBefore := commitCount(t, dir)
	iso, err := PrepareIsolation(context.Background(), dir, IsolationStaged, "go-review")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if got := commitCount(t, dir); got != commitsBefore+1 {
		t.Fatalf("expected 1 new commit, got %d -> %d", commitsBefore, got)
	}
	if msg := lastCommitMessage(t, dir); !strings.Contains(msg, "squad: staged snapshot before go-review") {
		t.Errorf("commit message = %q", msg)
	}
	// After prepare: a.txt is committed, b.txt unstaged change should be stashed.
	if data, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(data) != "b-original" {
		t.Errorf("after prepare, b.txt should be at HEAD (stashed), got %q", string(data))
	}
	if iso.stashRef == "" {
		t.Fatal("expected stashRef to be set when unstaged changes exist")
	}

	iso.Teardown(context.Background())

	if data, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(data) != "b-unstaged-change" {
		t.Errorf("after teardown, b.txt unstaged change should be restored, got %q", string(data))
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(data) != "a-staged-change" {
		t.Errorf("a.txt should still hold the staged change, got %q", string(data))
	}
	if n := stashCount(t, dir); n != 0 {
		t.Errorf("stash should be empty after teardown, got %d entries", n)
	}
}

func TestPrepareIsolationStagedNoopWhenIndexEmpty(t *testing.T) {
	dir := initGitRepo(t)
	writeFile(t, dir, "untracked.txt", "x") // unstaged only

	commitsBefore := commitCount(t, dir)
	iso, err := PrepareIsolation(context.Background(), dir, IsolationStaged, "agent")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if got := commitCount(t, dir); got != commitsBefore {
		t.Errorf("staged mode with empty index should not commit, got %d -> %d", commitsBefore, got)
	}
	if iso.stashRef != "" {
		t.Errorf("no stash should be created when index is empty, got %q", iso.stashRef)
	}
	iso.Teardown(context.Background())
}

func TestPrepareIsolationStagedNoUnstagedChanges(t *testing.T) {
	dir := initGitRepo(t)
	writeFile(t, dir, "feature.txt", "new")
	runInDir(t, dir, "git", "add", "feature.txt")

	iso, err := PrepareIsolation(context.Background(), dir, IsolationStaged, "agent")
	if err != nil {
		t.Fatalf("PrepareIsolation: %v", err)
	}
	if iso.stashRef != "" {
		t.Errorf("nothing unstaged so no stash expected, got %q", iso.stashRef)
	}
	if n := stashCount(t, dir); n != 0 {
		t.Errorf("stash count should be 0, got %d", n)
	}
	iso.Teardown(context.Background())
}

// --- test helpers ---

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func runInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func commitCount(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("git", "rev-list", "--count", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	n := 0
	for _, r := range strings.TrimSpace(string(out)) {
		n = n*10 + int(r-'0')
	}
	return n
}

func lastCommitMessage(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "log", "-1", "--pretty=%s")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func stashCount(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("git", "stash", "list")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("stash list: %v", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// scrubGitEnv unsets GIT_* environment variables for the duration of the
// test. When the test suite runs inside an outer git operation (notably
// `pre-commit run` during `git commit`), git sets GIT_INDEX_FILE / GIT_DIR /
// etc. to point at the parent repo. Child git invocations inside these tests
// inherit those values and fail with confusing errors like
// `.git/index: index file open failed: Not a directory`.
func scrubGitEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"GIT_INDEX_FILE",
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_PREFIX",
		"GIT_OBJECT_DIRECTORY",
		"GIT_COMMON_DIR",
	}
	saved := make(map[string]string, len(vars))
	for _, k := range vars {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = v
			_ = os.Unsetenv(k)
		}
	}
	t.Cleanup(func() {
		for k, v := range saved {
			_ = os.Setenv(k, v)
		}
	})
}

// initGitRepo creates a fresh git repo with one commit and returns its path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	scrubGitEnv(t)
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-b", "main")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	return dir
}

func TestSanitizeForBranch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"my-agent", "my-agent"},
		{"My Agent", "my-agent"},
		{"hello world", "hello-world"},
		{"---leading", "leading"},
		{"trailing---", "trailing"},
		{"UPPER_CASE", "upper_case"},
		{"special!@#chars", "special---chars"},
		{"", ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := sanitizeForBranch(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeForBranch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewIsolationID(t *testing.T) {
	t.Parallel()
	id := newIsolationID("my-agent")
	if id == "" {
		t.Fatal("newIsolationID returned empty string")
	}
	if !strings.HasPrefix(id, "my-agent-") {
		t.Errorf("newIsolationID(%q) = %q, want prefix %q", "my-agent", id, "my-agent-")
	}
	// Empty agent name falls back to "agent"
	id2 := newIsolationID("!@#")
	if !strings.HasPrefix(id2, "agent-") {
		t.Errorf("newIsolationID with empty slug = %q, want prefix %q", id2, "agent-")
	}
}

func TestGitRepoRoot(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)
	root, err := gitRepoRoot(context.Background(), dir)
	if err != nil {
		t.Fatalf("gitRepoRoot() error: %v", err)
	}
	if root == "" {
		t.Error("gitRepoRoot() returned empty string")
	}
}

func TestGitRepoRoot_NotGit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := gitRepoRoot(context.Background(), dir)
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}
