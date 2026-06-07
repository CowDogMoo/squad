package skill

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiscoverCatalogPrecedence is the Phase 5 validation gate for shadowing.
// A skill named "echo" appears in all three scopes — the repo copy must win,
// global is shadowed, catalog is shadowed.
func TestDiscoverCatalogPrecedence(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, globalSkills, "echo", "Global echo.", "global")

	repoRoot := t.TempDir()
	repoSkills := RepoSkillsDir(repoRoot)
	if err := os.MkdirAll(repoSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, repoSkills, "echo", "Repo echo.", "repo")

	catalogDir := t.TempDir()
	writeSkill(t, catalogDir, "echo", "Catalog echo.", "catalog")

	cat, err := Discover(repoRoot, catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	visible := cat.Visible()
	if len(visible) != 1 {
		t.Fatalf("expected one winner, got %d (%+v)", len(visible), visible)
	}
	if visible[0].Scope != ScopeRepo || visible[0].Manifest.Description != "Repo echo." {
		t.Errorf("wrong winner: %+v", visible[0])
	}

	shadowedCount := 0
	for _, e := range cat.All() {
		if e.Shadowed {
			shadowedCount++
		}
	}
	if shadowedCount != 2 {
		t.Errorf("expected 2 shadowed (global + catalog), got %d", shadowedCount)
	}
}

func TestDiscoverGlobalBeatsCatalog(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, globalSkills, "echo", "Global echo.", "global")

	catalogDir := t.TempDir()
	writeSkill(t, catalogDir, "echo", "Catalog echo.", "catalog")

	cat, err := Discover("", catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	visible := cat.Visible()
	if len(visible) != 1 || visible[0].Scope != ScopeGlobal {
		t.Fatalf("global should beat catalog: %+v", visible)
	}
}

func TestDiscoverCatalogOnly(t *testing.T) {
	withGlobalSkillsDir(t)
	catalogDir := t.TempDir()
	writeSkill(t, catalogDir, "lone", "Lonely catalog skill.", "body")

	cat, err := Discover("", catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	visible := cat.Visible()
	if len(visible) != 1 || visible[0].Scope != ScopeCatalog {
		t.Fatalf("expected single catalog entry, got %+v", visible)
	}
	if !filepathContains(visible[0].Dir, catalogDir) {
		t.Errorf("entry dir %q should be inside catalog %q", visible[0].Dir, catalogDir)
	}
}

func TestParseScopeCatalog(t *testing.T) {
	if s, err := ParseScope("catalog"); err != nil || s != ScopeCatalog {
		t.Errorf("got s=%v err=%v", s, err)
	}
	if ScopeCatalog.String() != "catalog" {
		t.Errorf("scope.String() = %q", ScopeCatalog.String())
	}
}

func filepathContains(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "" && rel[0] != '.'
}
