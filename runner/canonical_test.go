package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveGitToplevelInRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	gitInit(t, repo)
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	// From a nested subdir, the canonical path is the repo toplevel, not the
	// subdir. Resolve symlinks on both sides (macOS /var -> /private/var).
	got := ResolveGitToplevel(sub)
	want := evalSymlinks(t, repo)
	if evalSymlinks(t, got) != want {
		t.Fatalf("ResolveGitToplevel(%q)=%q, want toplevel %q", sub, got, want)
	}
}

func TestResolveGitToplevelNonRepoFallsBackToDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	// Not a git repo: ResolveGitToplevel must fall back to the input dir
	// rather than aborting.
	if got := ResolveGitToplevel(dir); got != dir {
		t.Fatalf("ResolveGitToplevel(%q)=%q, want fallback to input dir", dir, got)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func evalSymlinks(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return resolved
}
