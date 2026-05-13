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
