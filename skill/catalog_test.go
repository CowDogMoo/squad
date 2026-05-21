package skill

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeSkill creates <root>/<name>/SKILL.md with the given description+body.
// Returns the SKILL.md path so callers can mutate it for negative tests.
func writeSkill(t *testing.T, root, name, description, body string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// withGlobalSkillsDir points XDG_CONFIG_HOME at a temp dir so Discover's
// global scope is isolated from the developer's real ~/.config.
func withGlobalSkillsDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp) // belt-and-suspenders for the home fallback path
	return filepath.Join(tmp, "squad", "skills")
}

func TestDiscoverRepoAndGlobal(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, globalSkills, "alpha", "global alpha", "body")
	writeSkill(t, globalSkills, "beta", "global beta", "body")

	repoRoot := t.TempDir()
	repoSkills := RepoSkillsDir(repoRoot)
	if err := os.MkdirAll(repoSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, repoSkills, "gamma", "repo gamma", "body")

	cat, err := Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	visible := cat.Visible()
	names := entryNames(visible)
	sort.Strings(names)
	wantNames := []string{"alpha", "beta", "gamma"}
	if !equalSlices(names, wantNames) {
		t.Fatalf("visible = %v, want %v", names, wantNames)
	}

	for _, e := range visible {
		switch e.Name() {
		case "alpha", "beta":
			if e.Scope != ScopeGlobal {
				t.Errorf("%s scope = %v, want global", e.Name(), e.Scope)
			}
		case "gamma":
			if e.Scope != ScopeRepo {
				t.Errorf("%s scope = %v, want repo", e.Name(), e.Scope)
			}
		}
	}
}

func TestDiscoverRepoBeatsGlobal(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, globalSkills, "dup", "global dup", "global body")

	repoRoot := t.TempDir()
	if err := os.MkdirAll(RepoSkillsDir(repoRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, RepoSkillsDir(repoRoot), "dup", "repo dup", "repo body")

	cat, err := Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	visible := cat.Visible()
	if len(visible) != 1 || visible[0].Scope != ScopeRepo {
		t.Fatalf("expected single repo-scoped winner, got %#v", visible)
	}
	if visible[0].Manifest.Description != "repo dup" {
		t.Errorf("wrong winner: %q", visible[0].Manifest.Description)
	}

	all := cat.All()
	if len(all) != 2 {
		t.Fatalf("expected both entries retained in All(), got %d", len(all))
	}
	shadowed := 0
	for _, e := range all {
		if e.Shadowed {
			shadowed++
			if e.Scope != ScopeGlobal {
				t.Errorf("shadowed entry should be global, got %v", e.Scope)
			}
		}
	}
	if shadowed != 1 {
		t.Errorf("expected 1 shadowed entry, got %d", shadowed)
	}
}

func TestDiscoverSkipsBrokenSkill(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, globalSkills, "good", "ok", "body")

	// broken/SKILL.md: missing frontmatter
	brokenDir := filepath.Join(globalSkills, "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, FileName), []byte("no frontmatter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := Discover("")
	if err != nil {
		t.Fatal(err)
	}
	if names := entryNames(cat.Visible()); !equalSlices(names, []string{"good"}) {
		t.Errorf("visible = %v, want [good]", names)
	}
	errs := cat.LoadErrors()
	if len(errs) != 1 {
		t.Fatalf("expected 1 load error, got %d (%v)", len(errs), errs)
	}
	if !strings.Contains(errs[0].Path, "broken") {
		t.Errorf("error path = %q, want broken in it", errs[0].Path)
	}
}

func TestDiscoverRejectsNameMismatch(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	// Directory named "actual" but manifest declares "different".
	dir := filepath.Join(globalSkills, "actual")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: different\ndescription: x\n---\nhi\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := Discover("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Visible()) != 0 {
		t.Errorf("expected no visible entries, got %d", len(cat.Visible()))
	}
	if len(cat.LoadErrors()) != 1 {
		t.Errorf("expected 1 load error, got %d", len(cat.LoadErrors()))
	}
}

func TestDiscoverMissingDirsAreFine(t *testing.T) {
	withGlobalSkillsDir(t) // points env at a tmp dir with nothing in it
	cat, err := Discover("/nonexistent/path/to/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Visible()) != 0 || len(cat.LoadErrors()) != 0 {
		t.Errorf("expected empty catalog, got entries=%d errors=%d", len(cat.Visible()), len(cat.LoadErrors()))
	}
}

func TestCatalogFind(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, globalSkills, "alpha", "ok", "body")
	cat, err := Discover("")
	if err != nil {
		t.Fatal(err)
	}
	e, ok := cat.Find("alpha")
	if !ok {
		t.Fatal("alpha not found")
	}
	if e.Name() != "alpha" {
		t.Errorf("name = %q", e.Name())
	}
	if _, ok := cat.Find("nope"); ok {
		t.Error("expected miss for unknown name")
	}
}

func TestCatalogFilter(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, globalSkills, "alpha", "ok", "body")
	writeSkill(t, globalSkills, "beta", "ok", "body")

	repoRoot := t.TempDir()
	if err := os.MkdirAll(RepoSkillsDir(repoRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, RepoSkillsDir(repoRoot), "gamma", "ok", "body")
	cat, err := Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("scope filter", func(t *testing.T) {
		got := entryNames(cat.Filter(FilterOptions{Scopes: []Scope{ScopeRepo}}))
		if !equalSlices(got, []string{"gamma"}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("allow list", func(t *testing.T) {
		got := entryNames(cat.Filter(FilterOptions{Allow: []string{"alpha", "gamma"}}))
		sort.Strings(got)
		if !equalSlices(got, []string{"alpha", "gamma"}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("deny list", func(t *testing.T) {
		got := entryNames(cat.Filter(FilterOptions{Deny: []string{"beta"}}))
		sort.Strings(got)
		if !equalSlices(got, []string{"alpha", "gamma"}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("allow wins over deny", func(t *testing.T) {
		got := entryNames(cat.Filter(FilterOptions{Allow: []string{"alpha"}, Deny: []string{"alpha"}}))
		if !equalSlices(got, []string{"alpha"}) {
			t.Errorf("got %v, want [alpha] (allow overrides deny per plan)", got)
		}
	})

	t.Run("allow ignores scopes", func(t *testing.T) {
		got := entryNames(cat.Filter(FilterOptions{Allow: []string{"alpha"}, Scopes: []Scope{ScopeRepo}}))
		if !equalSlices(got, []string{"alpha"}) {
			t.Errorf("got %v, want [alpha] (allow overrides scopes)", got)
		}
	})
}

func TestParseScope(t *testing.T) {
	if s, err := ParseScope("repo"); err != nil || s != ScopeRepo {
		t.Errorf("repo: got %v err=%v", s, err)
	}
	if s, err := ParseScope("global"); err != nil || s != ScopeGlobal {
		t.Errorf("global: got %v err=%v", s, err)
	}
	if _, err := ParseScope("bogus"); err == nil {
		t.Error("expected error for bogus scope")
	}
}

func entryNames(es []Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
