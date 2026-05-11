package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
		{"invalid value errors", "branch", "", "", "", true},
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

// initGitRepo creates a fresh git repo with one commit and returns its path.
func initGitRepo(t *testing.T) string {
	t.Helper()
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
