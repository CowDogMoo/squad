package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/skill"
	"github.com/spf13/cobra"
)

// runSkillCmd builds the skill subtree and executes it with args. It returns
// the combined stdout+stderr output of the command and any error.
func runSkillCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newSkillCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return buf.String(), err
}

// makeSkill writes a SKILL.md under <root>/<name>/SKILL.md and returns the
// containing directory.
func makeSkill(t *testing.T, root, name, description, body string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, skill.FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSkillListEmpty(t *testing.T) {
	setupXDG(t)
	repoRoot := t.TempDir()
	out, err := runSkillCmd(t, "list", "--repo", repoRoot)
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no skills found") {
		t.Errorf("expected empty marker, got:\n%s", out)
	}
}

func TestSkillListMixedScopes(t *testing.T) {
	xdg := setupXDG(t)
	globalSkillsDir := filepath.Join(xdg, ".config", "squad", "skills")
	if err := os.MkdirAll(globalSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeSkill(t, globalSkillsDir, "alpha", "Global alpha description.", "alpha body")

	repoRoot := t.TempDir()
	repoSkillsDir := skill.RepoSkillsDir(repoRoot)
	if err := os.MkdirAll(repoSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeSkill(t, repoSkillsDir, "beta", "Repo beta description.", "beta body")

	out, err := runSkillCmd(t, "list", "--repo", repoRoot)
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "global") {
		t.Errorf("missing alpha/global in:\n%s", out)
	}
	if !strings.Contains(out, "beta") || !strings.Contains(out, "repo") {
		t.Errorf("missing beta/repo in:\n%s", out)
	}
}

func TestSkillListShadowing(t *testing.T) {
	xdg := setupXDG(t)
	globalSkillsDir := filepath.Join(xdg, ".config", "squad", "skills")
	if err := os.MkdirAll(globalSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeSkill(t, globalSkillsDir, "dup", "Global wins description.", "global")

	repoRoot := t.TempDir()
	repoSkillsDir := skill.RepoSkillsDir(repoRoot)
	if err := os.MkdirAll(repoSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeSkill(t, repoSkillsDir, "dup", "Repo wins description.", "repo")

	out, err := runSkillCmd(t, "list", "--repo", repoRoot)
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Repo wins") {
		t.Errorf("expected repo skill to win, output:\n%s", out)
	}
	if strings.Contains(out, "Global wins") {
		t.Errorf("global skill should be shadowed, output:\n%s", out)
	}

	outAll, err := runSkillCmd(t, "list", "--repo", repoRoot, "--all")
	if err != nil {
		t.Fatalf("list --all: %v\n%s", err, outAll)
	}
	if !strings.Contains(outAll, "(shadowed)") {
		t.Errorf("expected (shadowed) marker with --all, got:\n%s", outAll)
	}
	if !strings.Contains(outAll, "Global wins") {
		t.Errorf("expected shadowed global to appear with --all, got:\n%s", outAll)
	}
}

func TestSkillListSkipsBrokenSkill(t *testing.T) {
	xdg := setupXDG(t)
	globalSkillsDir := filepath.Join(xdg, ".config", "squad", "skills")
	if err := os.MkdirAll(globalSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeSkill(t, globalSkillsDir, "good", "Fine description.", "ok")

	// "broken" missing frontmatter
	brokenDir := filepath.Join(globalSkillsDir, "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, skill.FileName), []byte("no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runSkillCmd(t, "list", "--repo", t.TempDir())
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "good") {
		t.Errorf("expected good skill in output:\n%s", out)
	}
	if strings.Contains(out, "broken") {
		t.Errorf("broken skill should not appear in visible list:\n%s", out)
	}
}

func TestSkillShow(t *testing.T) {
	xdg := setupXDG(t)
	globalSkillsDir := filepath.Join(xdg, ".config", "squad", "skills")
	if err := os.MkdirAll(globalSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeSkill(t, globalSkillsDir, "alpha", "An alpha skill.", "# Body\nWith content.")

	out, err := runSkillCmd(t, "show", "alpha", "--repo", t.TempDir())
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	for _, want := range []string{"alpha", "An alpha skill", "# Body", "With content"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in show output:\n%s", want, out)
		}
	}
}

func TestSkillShowNotFound(t *testing.T) {
	setupXDG(t)
	out, err := runSkillCmd(t, "show", "nonexistent", "--repo", t.TempDir())
	if err == nil {
		t.Fatalf("expected error, got output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestSkillValidateOK(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "ok")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: ok\ndescription: A fine skill.\n---\n# Body\n"
	if err := os.WriteFile(filepath.Join(skillDir, skill.FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runSkillCmd(t, "validate", skillDir)
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("expected OK, got:\n%s", out)
	}
}

func TestSkillNew_RepoScope(t *testing.T) {
	setupXDG(t)
	repoRoot := t.TempDir()
	out, err := runSkillCmd(t, "new", "demo", "--repo", repoRoot)
	if err != nil {
		t.Fatalf("new: %v\n%s", err, out)
	}
	path := filepath.Join(skill.RepoSkillsDir(repoRoot), "demo", skill.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: demo") {
		t.Errorf("scaffold missing name: %s", data)
	}
	if !strings.Contains(string(data), "When to use this skill") {
		t.Errorf("scaffold missing starter section: %s", data)
	}
}

func TestSkillNew_GlobalScope(t *testing.T) {
	xdg := setupXDG(t)
	out, err := runSkillCmd(t, "new", "demo", "--global")
	if err != nil {
		t.Fatalf("new --global: %v\n%s", err, out)
	}
	path := filepath.Join(xdg, ".config", "squad", "skills", "demo", skill.FileName)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("global scaffold not created at %s: %v", path, err)
	}
}

func TestSkillNew_RejectsExisting(t *testing.T) {
	setupXDG(t)
	repoRoot := t.TempDir()
	if _, err := runSkillCmd(t, "new", "demo", "--repo", repoRoot); err != nil {
		t.Fatal(err)
	}
	out, err := runSkillCmd(t, "new", "demo", "--repo", repoRoot)
	if err == nil {
		t.Fatalf("expected error on duplicate, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestSkillNew_InvalidName(t *testing.T) {
	setupXDG(t)
	out, err := runSkillCmd(t, "new", "BadName", "--repo", t.TempDir())
	if err == nil {
		t.Fatalf("expected validation error, got:\n%s", out)
	}
}

func TestSkillNew_CustomDescription(t *testing.T) {
	setupXDG(t)
	repoRoot := t.TempDir()
	custom := "Does the thing."
	if _, err := runSkillCmd(t, "new", "demo", "--repo", repoRoot, "--description", custom); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skill.RepoSkillsDir(repoRoot), "demo", skill.FileName)
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), custom) {
		t.Errorf("custom description not embedded: %s", data)
	}
}

func TestSkillNew_GlobalAndRepoMutuallyExclusive(t *testing.T) {
	setupXDG(t)
	if _, err := runSkillCmd(t, "new", "demo", "--global", "--repo", t.TempDir()); err == nil {
		t.Fatal("expected mutually-exclusive error")
	}
}

// TestSkillNew_RoundTripsThroughValidate ensures every scaffolded skill
// passes the spec validator. Catches drift between starter content and
// SKILL.md rules.
func TestSkillNew_RoundTripsThroughValidate(t *testing.T) {
	setupXDG(t)
	repoRoot := t.TempDir()
	if _, err := runSkillCmd(t, "new", "demo", "--repo", repoRoot); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(skill.RepoSkillsDir(repoRoot), "demo")
	out, err := runSkillCmd(t, "validate", skillDir)
	if err != nil {
		t.Fatalf("scaffold did not pass validate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("validate did not report OK:\n%s", out)
	}
}

// newSkillTestCfg installs an isolated XDG layout so the SkillsManager's
// config-save side effect can't bleed into the developer's real ~/.config.
func newSkillTestCfg(t *testing.T) *config.Config {
	t.Helper()
	setupXDG(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), ".cache"))
	return &config.Config{
		Skills: config.SkillsConfig{
			Repositories: map[string]string{},
			LocalPaths:   nil,
		},
	}
}

// seedSkillRepo creates a tiny git repo containing <name>/SKILL.md and
// returns a file:// URL that go-git can clone offline.
func seedSkillRepo(t *testing.T, name, description string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not on PATH")
	}
	seed := t.TempDir()
	dir := filepath.Join(seed, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, skill.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "-c", "user.name=t", "-c", "user.email=t@t", "add", "."},
		{"git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "seed"},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = seed
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	return "file://" + seed
}

// runSkillSubcmd executes a single skill subcommand with config injected.
// Args are passed to RunE directly to avoid cobra's flag-parsing intercept
// (which can swallow stdout for commands invoked without a root parent).
func runSkillSubcmd(t *testing.T, cfg *config.Config, build func() *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd := build()
	cmd.SetContext(withConfig(context.Background(), cfg))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.ParseFlags(args); err != nil {
		return buf.String(), err
	}
	if err := cmd.RunE(cmd, cmd.Flags().Args()); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

func TestSkillAddLocalPath(t *testing.T) {
	cfg := newSkillTestCfg(t)
	dir := t.TempDir()
	if _, err := runSkillSubcmd(t, cfg, newSkillAddCmd, "local", dir); err != nil {
		t.Fatalf("add local: %v", err)
	}
	if len(cfg.Skills.LocalPaths) != 1 {
		t.Fatalf("expected 1 local path, got %v", cfg.Skills.LocalPaths)
	}
}

func TestSkillAddRepository(t *testing.T) {
	cfg := newSkillTestCfg(t)
	url := seedSkillRepo(t, "echo", "An echo skill.")
	if _, err := runSkillSubcmd(t, cfg, newSkillAddCmd, "team", url); err != nil {
		t.Fatalf("add repo: %v", err)
	}
	if cfg.Skills.Repositories["team"] != url {
		t.Fatalf("repo not registered: %v", cfg.Skills.Repositories)
	}
}

func TestSkillAddMissingConfig(t *testing.T) {
	cmd := newSkillAddCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"team", "/tmp/x"})
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when config missing")
	}
}

func TestSkillRemoveRepository(t *testing.T) {
	cfg := newSkillTestCfg(t)
	url := seedSkillRepo(t, "echo", "ok.")
	if _, err := runSkillSubcmd(t, cfg, newSkillAddCmd, "team", url); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := runSkillSubcmd(t, cfg, newSkillRemoveCmd, "team"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := cfg.Skills.Repositories["team"]; ok {
		t.Fatalf("team should be removed, got %v", cfg.Skills.Repositories)
	}
}

func TestSkillRemoveUnknown(t *testing.T) {
	cfg := newSkillTestCfg(t)
	if _, err := runSkillSubcmd(t, cfg, newSkillRemoveCmd, "ghost"); err == nil {
		t.Fatal("expected error removing unknown source")
	}
}

func TestSkillRemoveMissingConfig(t *testing.T) {
	cmd := newSkillRemoveCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"x"})
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when config missing")
	}
}

func TestSkillUpdateEmpty(t *testing.T) {
	cfg := newSkillTestCfg(t)
	if _, err := runSkillSubcmd(t, cfg, newSkillUpdateCmd); err != nil {
		t.Fatalf("update with no repos should be a no-op: %v", err)
	}
}

func TestSkillUpdateWithRepo(t *testing.T) {
	cfg := newSkillTestCfg(t)
	url := seedSkillRepo(t, "echo", "ok.")
	if _, err := runSkillSubcmd(t, cfg, newSkillAddCmd, "team", url); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := runSkillSubcmd(t, cfg, newSkillUpdateCmd); err != nil {
		t.Fatalf("update: %v", err)
	}
}

func TestSkillUpdateMissingConfig(t *testing.T) {
	cmd := newSkillUpdateCmd()
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when config missing")
	}
}

func TestSkillSourcesEmpty(t *testing.T) {
	cfg := newSkillTestCfg(t)
	out, err := runSkillSubcmd(t, cfg, newSkillSourcesCmd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No sources configured") {
		t.Fatalf("expected empty-state hint, got: %q", out)
	}
}

func TestSkillSourcesPopulated(t *testing.T) {
	cfg := newSkillTestCfg(t)
	cfg.Skills.Repositories["team"] = "https://example.com/a.git"
	cfg.Skills.LocalPaths = []string{"/tmp/skills"}
	out, err := runSkillSubcmd(t, cfg, newSkillSourcesCmd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "REPOSITORIES") || !strings.Contains(out, "team") {
		t.Fatalf("expected repo section, got: %q", out)
	}
	if !strings.Contains(out, "LOCAL PATHS") || !strings.Contains(out, "/tmp/skills") {
		t.Fatalf("expected local-path section, got: %q", out)
	}
}

func TestSkillSourcesMissingConfig(t *testing.T) {
	cmd := newSkillSourcesCmd()
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when config missing")
	}
}

func TestResolveSkillRepoRootOverride(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveSkillRepoRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if want != "" && gotResolved != want {
		t.Fatalf("override = %q, want %q", gotResolved, want)
	}
}

func TestResolveSkillRepoRootDefault(t *testing.T) {
	got, err := resolveSkillRepoRoot("")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("expected non-empty default")
	}
}

func TestResolveNewSkillDirRepo(t *testing.T) {
	repo := t.TempDir()
	dir, scope, err := resolveNewSkillDir("alpha", false, repo)
	if err != nil {
		t.Fatal(err)
	}
	if scope != "repo" {
		t.Fatalf("scope = %q, want repo", scope)
	}
	if !strings.HasSuffix(dir, filepath.Join(".squad", "skills", "alpha")) {
		t.Fatalf("dir = %q does not end with .squad/skills/alpha", dir)
	}
}

func TestResolveNewSkillDirGlobal(t *testing.T) {
	setupXDG(t)
	dir, scope, err := resolveNewSkillDir("alpha", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if scope != "global" {
		t.Fatalf("scope = %q, want global", scope)
	}
	if !strings.HasSuffix(dir, filepath.Join("squad", "skills", "alpha")) {
		t.Fatalf("dir = %q does not end with squad/skills/alpha", dir)
	}
}

func TestResolveNewSkillDirCWD(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir, scope, err := resolveNewSkillDir("alpha", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if scope != "repo" {
		t.Fatalf("scope = %q, want repo", scope)
	}
	wdResolved, _ := filepath.EvalSymlinks(wd)
	dirResolved, _ := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	if wdResolved != "" && dirResolved != "" && dirResolved != wdResolved {
		t.Fatalf("dir = %q should be rooted at cwd %q", dir, wd)
	}
}

func TestSkillCatalogPathsNilConfig(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	if got := skillCatalogPaths(cmd); got != nil {
		t.Fatalf("expected nil when config missing, got %v", got)
	}
}

func TestSkillCatalogPathsLocal(t *testing.T) {
	cfg := newSkillTestCfg(t)
	localDir := t.TempDir()
	cfg.Skills.LocalPaths = []string{localDir}
	cmd := &cobra.Command{}
	cmd.SetContext(withConfig(context.Background(), cfg))
	paths := skillCatalogPaths(cmd)
	if len(paths) != 1 || paths[0] != localDir {
		t.Fatalf("paths = %v, want [%q]", paths, localDir)
	}
}

func TestStarterSkillBodyContainsName(t *testing.T) {
	if !strings.Contains(starterSkillBody("grocery"), "# grocery") {
		t.Fatal("body missing skill-name header")
	}
}

func TestSkillCmdTree(t *testing.T) {
	cmd := newSkillCmd()
	want := []string{"list", "show", "validate", "add", "remove", "update", "sources", "new"}
	got := make(map[string]bool, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		got[strings.Fields(c.Use)[0]] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing subcommand %q (have %v)", name, got)
		}
	}
}

func TestSkillValidateFails(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "broken")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Missing description in frontmatter.
	content := "---\nname: broken\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, skill.FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runSkillCmd(t, "validate", skillDir)
	if err == nil {
		t.Fatalf("expected error, got output:\n%s", out)
	}
	if !strings.Contains(out, "error") {
		t.Errorf("expected error in output:\n%s", out)
	}
}
