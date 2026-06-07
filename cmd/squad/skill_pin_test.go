package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
)

func TestSkillAdd_withRefStoresPinnedSpec(t *testing.T) {
	cfg := newSkillTestCfg(t)
	url := seedSkillRepo(t, "echo", "An echo skill.")
	if _, err := runSkillSubcmd(t, cfg, newSkillAddCmd, "team", url, "--ref", "v1.2.0"); err != nil {
		t.Fatalf("add: %v", err)
	}
	got := cfg.Skills.Repositories["team"]
	if got.URL != url {
		t.Errorf("URL = %q, want %q", got.URL, url)
	}
	if got.Ref != "v1.2.0" {
		t.Errorf("Ref = %q, want v1.2.0", got.Ref)
	}
}

func TestSkillAdd_refOnLocalPathRejected(t *testing.T) {
	cfg := newSkillTestCfg(t)
	dir := t.TempDir()
	_, err := runSkillSubcmd(t, cfg, newSkillAddCmd, dir, "--ref", "main")
	if err == nil {
		t.Fatal("expected --ref on local path to error")
	}
	if !strings.Contains(err.Error(), "--ref only applies to git repositories") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSkillPin_setsAndUnsets(t *testing.T) {
	cfg := newSkillTestCfg(t)
	cfg.Skills.Repositories["team"] = config.RepoSpec{URL: "https://example.com/skills.git"}
	if _, err := runSkillSubcmd(t, cfg, newSkillPinCmd, "team", "v0.3.0"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if got := cfg.Skills.Repositories["team"].Ref; got != "v0.3.0" {
		t.Fatalf("Ref = %q, want v0.3.0", got)
	}

	if _, err := runSkillSubcmd(t, cfg, newSkillPinCmd, "team", "--unset"); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if cfg.Skills.Repositories["team"].IsPinned() {
		t.Fatal("expected unpinned after --unset")
	}
}

func TestSkillPin_unsetWithRefArgRejected(t *testing.T) {
	cfg := newSkillTestCfg(t)
	cfg.Skills.Repositories["team"] = config.RepoSpec{URL: "https://x.test/skills.git"}
	_, err := runSkillSubcmd(t, cfg, newSkillPinCmd, "team", "v1", "--unset")
	if err == nil {
		t.Fatal("expected error when combining --unset with a ref")
	}
}

func TestSkillPin_missingRefArgRejected(t *testing.T) {
	cfg := newSkillTestCfg(t)
	cfg.Skills.Repositories["team"] = config.RepoSpec{URL: "https://x.test/skills.git"}
	_, err := runSkillSubcmd(t, cfg, newSkillPinCmd, "team")
	if err == nil {
		t.Fatal("expected usage error when ref is missing and --unset is not set")
	}
}

func TestSkillPin_missingCatalogErrors(t *testing.T) {
	cfg := newSkillTestCfg(t)
	_, err := runSkillSubcmd(t, cfg, newSkillPinCmd, "ghost", "v1")
	if err == nil {
		t.Fatal("expected error when pinning a catalog that does not exist")
	}
}

func TestSkillUpdate_skipsPinnedWithoutForce(t *testing.T) {
	cfg := newSkillTestCfg(t)
	cfg.Skills.Repositories["pinned"] = config.RepoSpec{
		URL: "https://example.invalid/never.git",
		Ref: "v1",
	}
	if _, err := runSkillSubcmd(t, cfg, newSkillUpdateCmd); err != nil {
		t.Fatalf("expected pinned catalog to be skipped, got: %v", err)
	}
}

func TestSkillUpdate_forceAttemptsPinned(t *testing.T) {
	cfg := newSkillTestCfg(t)
	cfg.Skills.Repositories["pinned"] = config.RepoSpec{
		URL: "https://example.invalid/never.git",
		Ref: "v1",
	}
	_, err := runSkillSubcmd(t, cfg, newSkillUpdateCmd, "--force")
	if err == nil {
		t.Fatal("expected --force to attempt the upstream and surface the failure")
	}
}

func TestSkillUpdate_unpinnedFailureSurfaced(t *testing.T) {
	cfg := newSkillTestCfg(t)
	cfg.Skills.Repositories["bad"] = config.RepoSpec{
		URL: "https://example.invalid/no-such-repo.git",
	}
	if _, err := runSkillSubcmd(t, cfg, newSkillUpdateCmd); err == nil {
		t.Fatal("expected error from unpinned bad URL")
	}
}

func TestSkillAdd_bogusURLWarnsButRegisters(t *testing.T) {
	cfg := newSkillTestCfg(t)
	// A URL that fails to clone still registers in config; the warning
	// goes to stderr and the command exits 0.
	out, err := runSkillSubcmd(t, cfg, newSkillAddCmd, "team",
		"https://example.invalid/no-such-repo.git")
	if err != nil {
		t.Fatalf("add should succeed even when clone fails, got: %v", err)
	}
	if _, ok := cfg.Skills.Repositories["team"]; !ok {
		t.Fatal("team should be registered despite clone failure")
	}
	if !strings.Contains(out, "warning") || !strings.Contains(out, "initial clone failed") {
		t.Fatalf("expected clone-failure warning, got:\n%s", out)
	}
}

func TestSkillAdd_localPathDuplicateRejected(t *testing.T) {
	cfg := newSkillTestCfg(t)
	dir := t.TempDir()
	if _, err := runSkillSubcmd(t, cfg, newSkillAddCmd, dir); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := runSkillSubcmd(t, cfg, newSkillAddCmd, dir); err == nil {
		t.Fatal("expected duplicate local-path error")
	}
}

func TestSkillPin_nilCfgErrors(t *testing.T) {
	// Build the pin command without seeding cfg into the context so the
	// RunE closure's cfg-nil guard executes.
	cmd := newSkillPinCmd()
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{"any", "v1"}); err == nil {
		t.Fatal("expected error when context lacks a config")
	}
}

func TestSkillPin_managerConstructionErrorSurfaced(t *testing.T) {
	// Force source.NewSkillsManager to fail by pointing XDG_CONFIG_HOME at
	// a regular file so config.ConfigFile's MkdirAll trips ENOTDIR.
	// Exercises the NewSkillsManager-error branch of the pin RunE.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocker)
	t.Setenv("XDG_CACHE_HOME", blocker)
	t.Setenv("HOME", blocker)

	cfg := &config.Config{
		Skills: config.SkillsConfig{
			Repositories: map[string]config.RepoSpec{"x": {URL: "https://example.com/r.git"}},
		},
	}
	cmd := newSkillPinCmd()
	cmd.SetContext(withConfig(context.Background(), cfg))
	if err := cmd.RunE(cmd, []string{"x", "v1"}); err == nil {
		t.Fatal("expected NewSkillsManager construction failure to surface as error")
	}
}

func TestSkillSources_rendersRefColumn(t *testing.T) {
	cfg := newSkillTestCfg(t)
	cfg.Skills.Repositories["pinned"] = config.RepoSpec{
		URL: "https://example.com/pinned.git",
		Ref: "v1.0.0",
	}
	cfg.Skills.Repositories["floating"] = config.RepoSpec{
		URL: "https://example.com/floating.git",
	}
	out, err := runSkillSubcmd(t, cfg, newSkillSourcesCmd)
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	if !strings.Contains(out, "REF") {
		t.Errorf("expected REF column in output:\n%s", out)
	}
	if !strings.Contains(out, "v1.0.0") {
		t.Errorf("expected pinned ref v1.0.0 in output:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "floating") {
			if !strings.HasSuffix(strings.TrimRight(line, " \t"), "-") {
				t.Errorf("unpinned catalog should render '-' for ref: %q", line)
			}
		}
	}
}
