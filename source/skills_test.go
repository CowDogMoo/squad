package source

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/skill"
)

// makeBareSkillRepo creates a *seed* directory with one SKILL.md inside
// <alias>/SKILL.md, initializes it as a git repo, commits, and returns a
// file:// URL plus the original seed path. The seed has its own commit
// history, so git clone copies the working tree without network access.
//
// We use a real `git init` here because go-git's clone path expects a
// remote with refs and packed objects; piping that through go-git would
// require more setup than the test gains.
func makeBareSkillRepo(t *testing.T, skillName, description string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH; skipping integration test")
	}
	seed := t.TempDir()
	dir := filepath.Join(seed, skillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + skillName + "\ndescription: " + description + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, skill.FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "-c", "user.name=t", "-c", "user.email=t@t", "add", "."},
		{"git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "seed"},
	} {
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir = seed
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", cmd, err, out)
		}
	}
	return "file://" + seed
}

func newSkillsCfg(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
	t.Setenv("HOME", tmp)
	return &config.Config{
		Skills: config.SkillsConfig{
			Repositories: map[string]string{},
			LocalPaths:   []string{},
		},
	}
}

func TestSkillsManager_AddRepositoryClonesAndDiscovers(t *testing.T) {
	cfg := newSkillsCfg(t)
	url := makeBareSkillRepo(t, "echo", "An echo skill.")

	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", url); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnsureRepositoriesCloned(); err != nil {
		t.Fatalf("clone: %v", err)
	}

	paths := mgr.CatalogPaths()
	if len(paths) != 1 {
		t.Fatalf("expected 1 catalog path, got %d (%v)", len(paths), paths)
	}
	cat, err := skill.Discover("", paths...)
	if err != nil {
		t.Fatal(err)
	}
	visible := cat.Visible()
	if len(visible) != 1 || visible[0].Name() != "echo" || visible[0].Scope != skill.ScopeCatalog {
		t.Fatalf("expected one catalog-scope echo, got %#v", visible)
	}
}

func TestSkillsManager_AddLocalPathAndDiscover(t *testing.T) {
	cfg := newSkillsCfg(t)
	localDir := t.TempDir()
	skillDir := filepath.Join(localDir, "note")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skill.FileName),
		[]byte("---\nname: note\ndescription: Local note.\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddLocalPath(localDir); err != nil {
		t.Fatal(err)
	}

	paths := mgr.CatalogPaths()
	cat, err := skill.Discover("", paths...)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Visible()) != 1 || cat.Visible()[0].Name() != "note" {
		t.Fatalf("expected note skill, got %v", cat.Visible())
	}
}

func TestSkillsManager_UpdateRefetchesNewContent(t *testing.T) {
	cfg := newSkillsCfg(t)
	url := makeBareSkillRepo(t, "echo", "v1 description.")

	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", url); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnsureRepositoriesCloned(); err != nil {
		t.Fatal(err)
	}

	// Mutate the seed repo: rewrite the description and commit. UpdateRepositories
	// should pull the new content into the cache.
	seed := strings.TrimPrefix(url, "file://")
	skillPath := filepath.Join(seed, "echo", skill.FileName)
	v2 := "---\nname: echo\ndescription: v2 description.\n---\nbody-v2\n"
	if err := os.WriteFile(skillPath, []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{
		{"git", "-c", "user.name=t", "-c", "user.email=t@t", "add", "."},
		{"git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "v2"},
	} {
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Dir = seed
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", cmd, err, out)
		}
	}

	if err := mgr.UpdateRepositories(); err != nil {
		t.Fatal(err)
	}
	cat, err := skill.Discover("", mgr.CatalogPaths()...)
	if err != nil {
		t.Fatal(err)
	}
	visible := cat.Visible()
	if len(visible) != 1 {
		t.Fatalf("expected 1 visible, got %d", len(visible))
	}
	if visible[0].Manifest.Description != "v2 description." {
		t.Errorf("description not refetched: %q", visible[0].Manifest.Description)
	}
}

func TestSkillsManager_RemoveSourceUnregisters(t *testing.T) {
	cfg := newSkillsCfg(t)
	url := makeBareSkillRepo(t, "echo", "ok.")

	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", url); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RemoveSource("team"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Skills.Repositories["team"]; ok {
		t.Error("team should be unregistered from config")
	}
}

func TestSkillsManager_RemoveLocalPathByPath(t *testing.T) {
	cfg := newSkillsCfg(t)
	cfg.Skills.LocalPaths = []string{"/tmp/skills-x"}
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.RemoveSource("/tmp/skills-x"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Skills.LocalPaths) != 0 {
		t.Errorf("local path not removed: %v", cfg.Skills.LocalPaths)
	}
}

func TestSkillsManager_AddRejectsDuplicate(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", "https://example.com/a.git"); err == nil {
		t.Error("expected error on duplicate same-URL registration")
	}
	if err := mgr.AddRepository("team", "https://example.com/b.git"); err == nil {
		t.Error("expected error on duplicate alias with different URL")
	}
}

func TestSkillsManager_AddLocalPathMissing(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddLocalPath("/this/does/not/exist"); err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestSkillsManager_AddLocalPathNotADirectory(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	file := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddLocalPath(file); err == nil {
		t.Fatal("expected error for non-directory")
	}
}

func TestSkillsManager_AddLocalPathDuplicatePath(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := mgr.AddLocalPath(tmp); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddLocalPath(tmp); err == nil {
		t.Fatal("expected duplicate-path error")
	}
}

func TestSkillsManager_AddRepositoryRejectsNonGitURL(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("local", "/not/a/url"); err == nil {
		t.Fatal("expected error for non-git URL")
	}
}

func TestSkillsManager_RemoveSourceNotFound(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.RemoveSource("ghost"); err == nil {
		t.Fatal("expected error removing ghost")
	}
}

func TestSkillsManager_AddRepoCreatesMapWhenNil(t *testing.T) {
	cfg := newSkillsCfg(t)
	cfg.Skills.Repositories = nil
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	if cfg.Skills.Repositories["team"] == "" {
		t.Errorf("expected team registered, got %v", cfg.Skills.Repositories)
	}
}

func TestSkillsManager_NewWithCacheDirOverride(t *testing.T) {
	cfg := newSkillsCfg(t)
	custom := t.TempDir()
	cfg.Skills.CacheDir = custom
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if mgr.gitOps == nil {
		t.Error("expected gitOps configured")
	}
}

func TestSkillsManager_UpdateBadURLReportsError(t *testing.T) {
	cfg := newSkillsCfg(t)
	cfg.Skills.Repositories = map[string]string{
		"bad": "https://example.invalid/nope.git",
	}
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpdateRepositories(); err == nil {
		t.Fatal("expected error from bad upstream")
	}
}

func TestSkillsManager_EnsureClonedBadURLErrors(t *testing.T) {
	cfg := newSkillsCfg(t)
	cfg.Skills.Repositories = map[string]string{
		"bad": "https://example.invalid/nope.git",
	}
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnsureRepositoriesCloned(); err == nil {
		t.Fatal("expected clone error from bad upstream")
	}
}

func TestSkillsManager_EnsureClonedSkipsAlreadyCached(t *testing.T) {
	cfg := newSkillsCfg(t)
	url := makeBareSkillRepo(t, "echo", "ok.")

	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", url); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnsureRepositoriesCloned(); err != nil {
		t.Fatal(err)
	}
	// Second call should hit the "already cached" path and return nil quickly.
	if err := mgr.EnsureRepositoriesCloned(); err != nil {
		t.Fatalf("expected no-op on re-clone, got %v", err)
	}
}

func TestSkillsManager_SaveConfigCreatesDirectory(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Drive saveConfig via AddRepository, then check that the config file exists.
	if err := mgr.AddRepository("team", "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mgr.configPath); err != nil {
		t.Errorf("expected config file written at %s, got %v", mgr.configPath, err)
	}
}

func TestSkillsManager_SaveConfigFailsWhenParentBlocked(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Block the parent directory by making it a regular file.
	parent := filepath.Dir(mgr.configPath)
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", "https://example.com/a.git"); err == nil {
		t.Fatal("expected save failure when parent dir is a file")
	}
}

// TestNewSkillsManager_FailsWithoutXDG covers the config.ConfigFile error
// path: with HOME and XDG_CONFIG_HOME both empty the config dir cannot be
// resolved.
func TestNewSkillsManager_FailsWithoutXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	cfg := &config.Config{
		Skills: config.SkillsConfig{
			Repositories: map[string]string{},
			LocalPaths:   []string{},
		},
	}
	if _, err := NewSkillsManager(cfg); err == nil {
		t.Fatal("expected NewSkillsManager error when XDG_CONFIG_HOME and HOME are empty")
	}
}

// TestNewSkillsManager_SkillsCacheDirError exercises the branch where
// config.ConfigFile succeeds (because XDG_CONFIG_HOME is set) but the cache
// dir cannot be resolved. We set XDG_CACHE_HOME and HOME to empty so
// SkillsCacheDir returns os.ErrNotExist.
func TestNewSkillsManager_SkillsCacheDirError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	cfg := &config.Config{
		Skills: config.SkillsConfig{
			Repositories: map[string]string{},
			LocalPaths:   []string{},
		},
	}
	if _, err := NewSkillsManager(cfg); err == nil {
		t.Fatal("expected NewSkillsManager error when SkillsCacheDir cannot be resolved")
	}
}

// TestSkillsManager_AddLocalPathAppendsToNilSlice drives the append-to-nil
// behavior in AddLocalPath where cfg.Skills.LocalPaths is nil. Go allows
// append on a nil slice, so this should succeed.
func TestSkillsManager_AddLocalPathAppendsToNilSlice(t *testing.T) {
	cfg := newSkillsCfg(t)
	cfg.Skills.LocalPaths = nil
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := mgr.AddLocalPath(dir); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Skills.LocalPaths) != 1 {
		t.Fatalf("expected one local path registered, got %v", cfg.Skills.LocalPaths)
	}
}

// TestSkillsManager_AddLocalPathDuplicateErrorMessage verifies the duplicate
// error surfaces the "already configured" string so users can recognize it.
func TestSkillsManager_AddLocalPathDuplicateErrorMessage(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := mgr.AddLocalPath(dir); err != nil {
		t.Fatal(err)
	}
	err = mgr.AddLocalPath(dir)
	if err == nil {
		t.Fatal("expected duplicate-path error")
	}
	if !strings.Contains(err.Error(), "already configured") {
		t.Fatalf("wrong error: %v", err)
	}
}
