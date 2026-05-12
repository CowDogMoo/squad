package routine

import (
	"path/filepath"
	"testing"
)

func setupTempXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir) // belt + suspenders for tests that may resolve UserHomeDir
	return dir
}

func TestLoadRootsMissing(t *testing.T) {
	setupTempXDG(t)
	roots, err := LoadRoots()
	if err != nil {
		t.Fatalf("missing roots file should not error: %v", err)
	}
	if len(roots) != 0 {
		t.Errorf("expected empty, got %v", roots)
	}
}

func TestAddRootIdempotent(t *testing.T) {
	setupTempXDG(t)
	tmp := t.TempDir()

	abs, changed, err := AddRoot(tmp)
	if err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	if !changed {
		t.Error("expected first AddRoot to change registry")
	}
	if abs == "" {
		t.Error("expected abs path returned")
	}

	_, changed2, err := AddRoot(tmp)
	if err != nil {
		t.Fatalf("AddRoot 2: %v", err)
	}
	if changed2 {
		t.Error("expected second AddRoot to be a no-op")
	}

	roots, err := LoadRoots()
	if err != nil {
		t.Fatalf("LoadRoots: %v", err)
	}
	if len(roots) != 1 || roots[0] != abs {
		t.Errorf("expected [%s], got %v", abs, roots)
	}
}

func TestAddRootRejectsFile(t *testing.T) {
	setupTempXDG(t)
	file := filepath.Join(t.TempDir(), "notadir")
	if err := writeFile(file, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddRoot(file); err == nil {
		t.Error("expected error adding a file as a root")
	}
}

func TestRemoveRoot(t *testing.T) {
	setupTempXDG(t)
	tmp := t.TempDir()
	if _, _, err := AddRoot(tmp); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	changed, err := RemoveRoot(tmp)
	if err != nil {
		t.Fatalf("RemoveRoot: %v", err)
	}
	if !changed {
		t.Error("expected RemoveRoot to report change")
	}
	roots, _ := LoadRoots()
	if len(roots) != 0 {
		t.Errorf("expected empty after remove, got %v", roots)
	}
}

func TestContainingRoot(t *testing.T) {
	tmp := t.TempDir()
	deep := filepath.Join(tmp, "sub", "deep")
	if err := makeDir(deep); err != nil {
		t.Fatal(err)
	}
	got, err := ContainingRoot(deep, []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	if got != tmp {
		t.Errorf("got %q, want %q", got, tmp)
	}
	// Not contained.
	other := t.TempDir()
	got2, _ := ContainingRoot(other, []string{tmp})
	if got2 != "" {
		t.Errorf("expected empty, got %q", got2)
	}
}

func TestContainingRootLongestWins(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "inner")
	if err := makeDir(inner); err != nil {
		t.Fatal(err)
	}
	got, err := ContainingRoot(inner, []string{outer, inner})
	if err != nil {
		t.Fatal(err)
	}
	if got != inner {
		t.Errorf("longest-match: got %q, want %q", got, inner)
	}
}

func TestHasRepoRoutinesDir(t *testing.T) {
	repo := t.TempDir()
	if HasRepoRoutinesDir(repo) {
		t.Error("expected false before creating .squad/routines")
	}
	if err := makeDir(filepath.Join(repo, ".squad", "routines")); err != nil {
		t.Fatal(err)
	}
	if !HasRepoRoutinesDir(repo) {
		t.Error("expected true after creating .squad/routines")
	}
}

func TestGlobalDirs(t *testing.T) {
	setupTempXDG(t)
	dir, err := GlobalRoutinesDir()
	if err != nil {
		t.Fatalf("GlobalRoutinesDir: %v", err)
	}
	if !pathExists(dir) {
		t.Errorf("global routines dir not created: %s", dir)
	}
	stateDir, err := GlobalStateDir()
	if err != nil {
		t.Fatalf("GlobalStateDir: %v", err)
	}
	if !pathExists(stateDir) {
		t.Errorf("global state dir not created: %s", stateDir)
	}
}
