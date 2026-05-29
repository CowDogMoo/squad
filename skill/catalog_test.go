package skill

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestScopeString(t *testing.T) {
	cases := []struct {
		in   Scope
		want string
	}{
		{ScopeRepo, "repo"},
		{ScopeGlobal, "global"},
		{ScopeCatalog, "catalog"},
		{Scope(99), "scope(99)"},
	}
	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("%d.String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadErrorErrorAndUnwrap(t *testing.T) {
	inner := errors.New("boom")
	le := LoadError{Path: "/tmp/x", Err: inner}
	if got := le.Error(); !strings.Contains(got, "/tmp/x") || !strings.Contains(got, "boom") {
		t.Errorf("Error() = %q, missing path or wrapped message", got)
	}
	if errors.Unwrap(le) != inner {
		t.Errorf("Unwrap() did not return the inner error")
	}
	if !errors.Is(le, inner) {
		t.Errorf("errors.Is should find the inner error via Unwrap")
	}
}

func TestEntryNameNilManifest(t *testing.T) {
	e := Entry{}
	if got := e.Name(); got != "" {
		t.Fatalf("expected empty name for nil manifest, got %q", got)
	}
}

func TestDiscoverEmptyCatalogDirIsSkipped(t *testing.T) {
	withGlobalSkillsDir(t)
	cat, err := Discover("", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.All()) != 0 {
		t.Errorf("expected no entries, got %v", cat.All())
	}
}

func TestDiscoverIgnoresNonDirItems(t *testing.T) {
	withGlobalSkillsDir(t)
	catalogDir := t.TempDir()
	// A regular file in the catalog root must be silently ignored.
	if err := os.WriteFile(filepath.Join(catalogDir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := Discover("", catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.All()) != 0 {
		t.Errorf("expected zero entries, got %v", cat.All())
	}
}

func TestDiscoverSubdirWithoutSkillMdIgnored(t *testing.T) {
	withGlobalSkillsDir(t)
	catalogDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(catalogDir, "no-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	cat, err := Discover("", catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.All()) != 0 {
		t.Errorf("expected zero entries (subdir has no SKILL.md), got %v", cat.All())
	}
}

func TestDiscoverCatalogDirIsItselfASkill(t *testing.T) {
	withGlobalSkillsDir(t)
	// Simulate a single-skill catalog repo (e.g. blader/humanizer): the
	// clone's root directory IS the skill — SKILL.md sits at the top
	// level, not in a named subdirectory.
	catalogDir := t.TempDir()
	manifest := "---\nname: humanizer\ndescription: rewrite AI-generated prose into something a human would actually say\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(catalogDir, FileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := Discover("", catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	visible := cat.Visible()
	if len(visible) != 1 || visible[0].Name() != "humanizer" {
		t.Fatalf("want one humanizer entry, got %v", entryNames(visible))
	}
	if visible[0].Dir != catalogDir {
		t.Errorf("Dir = %s, want %s", visible[0].Dir, catalogDir)
	}
	if visible[0].Scope != ScopeCatalog {
		t.Errorf("Scope = %v, want catalog", visible[0].Scope)
	}
}

func TestDiscoverCatalogDirSkillAndSubdirSkillsCoexist(t *testing.T) {
	withGlobalSkillsDir(t)
	catalogDir := t.TempDir()
	rootManifest := "---\nname: root-skill\ndescription: skill defined at the catalog root\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(catalogDir, FileName), []byte(rootManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, catalogDir, "nested", "skill in a subdirectory", "body")

	cat, err := Discover("", catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	names := entryNames(cat.Visible())
	sort.Strings(names)
	want := []string{"nested", "root-skill"}
	if !equalSlices(names, want) {
		t.Fatalf("visible = %v, want %v", names, want)
	}
}

func TestDiscoverCatalogDirSkillInvalidIsLoadError(t *testing.T) {
	withGlobalSkillsDir(t)
	catalogDir := t.TempDir()
	// Missing required `description` field — parse should fail and the
	// failure should be surfaced as a LoadError, not a fatal Discover error.
	if err := os.WriteFile(filepath.Join(catalogDir, FileName), []byte("---\nname: broken\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := Discover("", catalogDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Visible()) != 0 {
		t.Errorf("expected no visible entries, got %v", entryNames(cat.Visible()))
	}
	if len(cat.LoadErrors()) != 1 {
		t.Fatalf("expected 1 LoadError, got %d", len(cat.LoadErrors()))
	}
}

func TestDiscoverScanDirReadDirError(t *testing.T) {
	withGlobalSkillsDir(t)
	// Pointing repoRoot at a file (not a dir) → RepoSkillsDir(repoRoot) is
	// <file>/.squad/skills, which ReadDir will fail with not-a-directory.
	tmp := t.TempDir()
	notADir := filepath.Join(tmp, "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// repoRoot itself is the file, so .squad/skills underneath is invalid.
	_, err := Discover(notADir)
	if err == nil {
		t.Fatal("expected scan error when path traverses into a file")
	}
}

func TestCatalogFindSkipsShadowed(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, globalSkills, "dup", "global", "g body")

	repoRoot := t.TempDir()
	repoSkills := RepoSkillsDir(repoRoot)
	if err := os.MkdirAll(repoSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, repoSkills, "dup", "repo", "r body")

	cat, err := Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cat.Find("dup")
	if !ok || entry.Manifest.Description != "repo" {
		t.Fatalf("Find should return the repo-scope winner, got %#v", entry)
	}
}

func TestCatalogFilterSkipsShadowed(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	if err := os.MkdirAll(globalSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, globalSkills, "dup", "global", "g body")
	repoRoot := t.TempDir()
	repoSkills := RepoSkillsDir(repoRoot)
	if err := os.MkdirAll(repoSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, repoSkills, "dup", "repo", "r body")

	cat, err := Discover(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	got := cat.Filter(FilterOptions{})
	if len(got) != 1 || got[0].Manifest.Description != "repo" {
		t.Fatalf("Filter should yield only repo-scope dup, got %v", got)
	}
}

// TestDiscoverGlobalSkillsDirError covers the branch where GlobalSkillsDir()
// fails — set XDG_CONFIG_HOME and HOME both empty so ConfigFile returns
// os.ErrNotExist.
func TestDiscoverGlobalSkillsDirError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := Discover(""); err == nil {
		t.Fatal("expected Discover error when GlobalSkillsDir cannot be resolved")
	}
}

// TestGlobalSkillsDirError directly exercises the ConfigFile error branch
// inside GlobalSkillsDir.
func TestGlobalSkillsDirError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := GlobalSkillsDir(); err == nil {
		t.Fatal("expected GlobalSkillsDir error when XDG_CONFIG_HOME and HOME are empty")
	}
}

// TestDiscoverScanGlobalFails forces the scan-global branch to error by
// making the global skills directory unreadable (mode 0). MkdirAll inside
// GlobalSkillsDir succeeds because the directory already exists, but the
// subsequent ReadDir in scanDir returns a permission error that is NOT
// fs.ErrNotExist — exactly the branch we want to exercise.
func TestDiscoverScanGlobalFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics are unix-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)
	skills := filepath.Join(tmp, "squad", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	// Strip read permission so ReadDir errors with EACCES (not ErrNotExist).
	if err := os.Chmod(skills, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(skills, 0o755); err != nil {
			t.Logf("restore chmod failed: %v", err)
		}
	})
	if _, err := Discover(""); err == nil {
		t.Fatal("expected scan-global error when skills directory is unreadable")
	}
}

// TestDiscoverScanCatalogFails forces the catalog scan branch to error by
// passing a path that resolves to a non-directory.
func TestDiscoverScanCatalogFails(t *testing.T) {
	withGlobalSkillsDir(t)
	tmp := t.TempDir()
	notADir := filepath.Join(tmp, "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover("", notADir); err == nil {
		t.Fatal("expected scan-catalog error when path is a file")
	}
}

// TestResolveCollisionsCatalogBeforeRepo verifies the branch where a
// later-scanned higher-precedence entry (e.g. repo) supersedes an
// earlier-scanned lower-precedence entry (catalog). The previous test
// covered repo-before-global; this one walks the swap path explicitly.
func TestResolveCollisionsCatalogBeforeRepo(t *testing.T) {
	withGlobalSkillsDir(t)
	// Build a catalog dir with "dup"; build a repo with "dup".
	// Discover scans repo first, then global, then catalog — so to take
	// the swap-the-winner branch we need a LATER-scanned entry with a
	// LOWER scope number, i.e. we cannot via Discover. Instead we
	// construct the Catalog directly.
	c := &Catalog{
		entries: []Entry{
			{Manifest: &Manifest{Name: "dup"}, Scope: ScopeCatalog},
			{Manifest: &Manifest{Name: "dup"}, Scope: ScopeRepo},
		},
	}
	c.resolveCollisions()
	if !c.entries[0].Shadowed {
		t.Errorf("catalog entry should be shadowed by repo entry; got %v", c.entries)
	}
	if c.entries[1].Shadowed {
		t.Errorf("repo entry should be the winner, not shadowed; got %v", c.entries)
	}
}

// TestCatalogFind_SkipsShadowedAndReturnsFalseWhenAllShadowed exercises
// the `if e.Shadowed { continue }` branch followed by no match.
func TestCatalogFind_SkipsShadowedAndReturnsFalseWhenAllShadowed(t *testing.T) {
	c := &Catalog{
		entries: []Entry{
			{Manifest: &Manifest{Name: "alpha"}, Scope: ScopeCatalog, Shadowed: true},
		},
	}
	if _, ok := c.Find("alpha"); ok {
		t.Fatal("Find should skip a shadowed entry")
	}
}

func TestCatalogLoadErrorsCopy(t *testing.T) {
	globalSkills := withGlobalSkillsDir(t)
	brokenDir := filepath.Join(globalSkills, "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, FileName), []byte("not valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := Discover("")
	if err != nil {
		t.Fatal(err)
	}
	errs := cat.LoadErrors()
	if len(errs) == 0 {
		t.Fatal("expected at least one load error")
	}
	// Mutate the copy — the catalog's slice must not be affected.
	errs[0].Path = "mutated"
	if cat.LoadErrors()[0].Path == "mutated" {
		t.Error("LoadErrors should return a snapshot, mutation leaked")
	}
}

func TestFindRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// From a subdirectory we walk up to the dir containing .git. Resolve
	// symlinks on both sides because t.TempDir() lives under a symlinked
	// path (/var -> /private/var) on macOS.
	got, err := filepath.EvalSymlinks(FindRepoRoot(sub))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("FindRepoRoot(%q) = %q, want %q", sub, got, want)
	}

	// Outside any repo, the input is returned unchanged.
	noRepo := t.TempDir()
	if got := FindRepoRoot(noRepo); got != noRepo {
		t.Errorf("FindRepoRoot outside a repo = %q, want %q", got, noRepo)
	}
}
