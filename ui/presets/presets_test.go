package presets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmptyStore(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(s.All()) != 0 {
		t.Errorf("missing file should yield empty store, got %d presets", len(s.All()))
	}
}

func TestSetCreatesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(Preset{Name: "alpha", Agent: "go-review", MaxCost: 5}); err != nil {
		t.Fatal(err)
	}

	// Reload from disk and verify.
	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get("alpha")
	if !ok {
		t.Fatal("alpha missing after reload")
	}
	if got.Agent != "go-review" || got.MaxCost != 5 {
		t.Errorf("alpha: got %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be stamped on Set")
	}
}

func TestSetUpsertsByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	_ = s.Set(Preset{Name: "x", Agent: "old"})
	_ = s.Set(Preset{Name: "x", Agent: "new"})
	if len(s.All()) != 1 {
		t.Errorf("upsert should not duplicate, got %d", len(s.All()))
	}
	got, _ := s.Get("x")
	if got.Agent != "new" {
		t.Errorf("upsert should overwrite, got %s", got.Agent)
	}
}

func TestSetRejectsEmptyName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	if err := s.Set(Preset{Agent: "x"}); err == nil {
		t.Error("empty name should error")
	}
}

func TestRemoveReturnsFalseForUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	ok, err := s.Remove("nope")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("removing unknown should return false")
	}
}

func TestRemoveDeletes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	_ = s.Set(Preset{Name: "a", Agent: "x"})
	_ = s.Set(Preset{Name: "b", Agent: "y"})
	ok, err := s.Remove("a")
	if !ok || err != nil {
		t.Fatalf("remove a: ok=%v err=%v", ok, err)
	}
	if _, found := s.Get("a"); found {
		t.Error("removed preset should not be found")
	}
	if _, found := s.Get("b"); !found {
		t.Error("unrelated preset should remain")
	}
}

func TestAllSortsByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	for _, n := range []string{"charlie", "alpha", "bravo"} {
		_ = s.Set(Preset{Name: n, Agent: "x"})
	}
	got := s.All()
	want := []string{"alpha", "bravo", "charlie"}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("All()[%d] = %q, want %q", i, got[i].Name, w)
		}
	}
}

func TestDefaultPathHonorsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "squad", "presets.yaml")
	if p != want {
		t.Errorf("DefaultPath() = %q, want %q", p, want)
	}
}

func TestSaveAtomic(t *testing.T) {
	// Verify the tmp file is cleaned up after a successful save.
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	_ = s.Set(Preset{Name: "x", Agent: "y"})
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file should not linger after save, stat err = %v", err)
	}
}

func TestDefaultPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "squad", "presets.yaml")
	if p != want {
		t.Errorf("DefaultPath() = %q, want %q", p, want)
	}
}

func TestPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	if s.Path() != path {
		t.Errorf("Path() = %q, want %q", s.Path(), path)
	}
}

func TestNamesSorted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	for _, n := range []string{"charlie", "alpha", "bravo"} {
		_ = s.Set(Preset{Name: n, Agent: "x"})
	}
	got := s.Names()
	want := []string{"alpha", "bravo", "charlie"}
	if len(got) != len(want) {
		t.Fatalf("Names(): got %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("Names()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	if err := os.WriteFile(path, []byte("not: [valid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("malformed yaml should error")
	}
}

func TestSaveWriteFailsWhenParentIsReadOnly(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod ineffective as root")
	}
	// Create a store whose path lives inside a read-only dir so WriteFile fails.
	dir := t.TempDir()
	path := filepath.Join(dir, "presets.yaml")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Add a preset so save() is called.
	if err := s.Set(Preset{Name: "x", Agent: "y"}); err != nil {
		t.Fatal(err)
	}
	// Now make the dir read-only so the next write fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	// Mutate and try to save again — should fail.
	if err := s.Set(Preset{Name: "z", Agent: "w"}); err == nil {
		t.Error("expected error writing to read-only dir, got nil")
	}
}

func TestDefaultPathUsesHomeDir(t *testing.T) {
	// Clear XDG_CONFIG_HOME so DefaultPath falls back to UserHomeDir.
	t.Setenv("XDG_CONFIG_HOME", "")
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if p == "" {
		t.Error("DefaultPath returned empty string")
	}
}

func TestLoadUnreadableFileReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod ineffective as root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "presets.yaml")
	if err := os.WriteFile(path, []byte("presets: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unreadable file, got nil")
	}
}

func TestSaveRenameFailsWhenTargetIsDir(t *testing.T) {
	// Make the final path a directory so Rename fails.
	dir := t.TempDir()
	path := filepath.Join(dir, "presets.yaml")
	// Create a directory at the target path.
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	s, _ := Load(filepath.Join(dir, "other.yaml"))
	// Manually set path to the dir-blocked path.
	s.path = path
	err := s.Set(Preset{Name: "x", Agent: "y"})
	if err == nil {
		t.Fatal("expected error when rename target is a directory, got nil")
	}
}

func TestSet_EmptyNameReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	if err := s.Set(Preset{Name: "", Agent: "x"}); err == nil {
		t.Fatal("expected error for empty preset name")
	}
}

func TestRemove_NonexistentReturnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	ok, err := s.Remove("nope")
	if err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if ok {
		t.Error("Remove of missing preset should return false")
	}
}

func TestRemove_Existing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	if err := s.Set(Preset{Name: "a", Agent: "x"}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Remove("a")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !ok {
		t.Error("Remove existing should return true")
	}
	if _, ok := s.Get("a"); ok {
		t.Error("Get after Remove should return false")
	}
}

func TestSet_UpdatesExistingPreset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.yaml")
	s, _ := Load(path)
	if err := s.Set(Preset{Name: "p", Agent: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(Preset{Name: "p", Agent: "v2"}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("p")
	if !ok {
		t.Fatal("Get returned !ok")
	}
	if got.Agent != "v2" {
		t.Errorf("Agent after update = %q, want v2", got.Agent)
	}
	if len(s.All()) != 1 {
		t.Errorf("All() len = %d, want 1 (update should not append)", len(s.All()))
	}
}

func TestSaveCreatesMissingDir(t *testing.T) {
	// Use a nested path whose parent dir doesn't exist yet.
	path := filepath.Join(t.TempDir(), "deep", "nested", "presets.yaml")
	s, _ := Load(path)
	if err := s.Set(Preset{Name: "x", Agent: "y"}); err != nil {
		t.Fatalf("Set should create parent dirs, got %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("presets file should exist after Set, stat err = %v", err)
	}
}
