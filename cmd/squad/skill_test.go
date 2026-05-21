package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/skill"
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
