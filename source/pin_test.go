package source

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
)

// newAgentsCfg returns a config with an isolated XDG layout so manager
// tests don't touch the developer's real ~/.config when saveConfig fires.
func newAgentsCfg(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
	t.Setenv("HOME", tmp)
	return &config.Config{
		Agents: config.AgentsConfig{
			Repositories: map[string]config.RepoSpec{},
			LocalPaths:   []string{},
		},
	}
}

func TestManager_PinRepository_setsAndUpdatesRef(t *testing.T) {
	cfg := newAgentsCfg(t)
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}

	const url = "https://example.com/org/agents.git"
	if err := mgr.AddRepository("official", url, ""); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}
	if spec := cfg.Agents.Repositories["official"]; spec.IsPinned() {
		t.Fatalf("freshly-added repo should not be pinned, got %+v", spec)
	}

	if err := mgr.PinRepository("official", "v1.0.0"); err != nil {
		t.Fatalf("PinRepository: %v", err)
	}
	if got := cfg.Agents.Repositories["official"]; got.Ref != "v1.0.0" || !got.IsPinned() {
		t.Fatalf("after pin, spec = %+v", got)
	}

	// Pinning again replaces the ref rather than appending.
	if err := mgr.PinRepository("official", "v1.1.0"); err != nil {
		t.Fatalf("PinRepository (replace): %v", err)
	}
	if got := cfg.Agents.Repositories["official"].Ref; got != "v1.1.0" {
		t.Fatalf("replaced ref = %q, want v1.1.0", got)
	}

	// Empty ref unpins.
	if err := mgr.PinRepository("official", ""); err != nil {
		t.Fatalf("PinRepository (unset): %v", err)
	}
	if got := cfg.Agents.Repositories["official"]; got.IsPinned() {
		t.Fatalf("after unset, spec should be unpinned, got %+v", got)
	}
}

func TestManager_PinRepository_missingRepoErrors(t *testing.T) {
	cfg := newAgentsCfg(t)
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = mgr.PinRepository("ghost", "v1.0.0")
	if err == nil {
		t.Fatal("expected error for unknown repo")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error %q should mention the repo name", err.Error())
	}
}

func TestManager_AddRepository_withRefStoresRef(t *testing.T) {
	cfg := newAgentsCfg(t)
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const url = "https://example.com/org/agents.git"
	if err := mgr.AddRepository("official", url, "v0.4.2"); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}
	got := cfg.Agents.Repositories["official"]
	if got.URL != url || got.Ref != "v0.4.2" {
		t.Fatalf("stored spec = %+v", got)
	}
}

func TestManager_UpdateRepositories_skipsPinnedWithoutForce(t *testing.T) {
	cfg := newAgentsCfg(t)
	cfg.Agents.Repositories = map[string]config.RepoSpec{
		// A bogus URL would normally fail the update; if the entry is
		// pinned and force is false we expect it to be skipped instead.
		"pinned": {URL: "https://example.invalid/never-resolved.git", Ref: "v1"},
	}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpdateRepositories(false); err != nil {
		t.Fatalf("expected pinned repo to be skipped, got: %v", err)
	}
}

func TestManager_UpdateRepositories_forceAttemptsPinned(t *testing.T) {
	cfg := newAgentsCfg(t)
	cfg.Agents.Repositories = map[string]config.RepoSpec{
		"pinned": {URL: "https://example.invalid/never-resolved.git", Ref: "v1"},
	}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpdateRepositories(true); err == nil {
		t.Fatal("expected --force to attempt the update and report the upstream failure")
	}
}

func TestSkillsManager_PinRepository_setsAndUnsets(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", "https://example.com/skills.git", ""); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}
	if err := mgr.PinRepository("team", "v0.2.0"); err != nil {
		t.Fatalf("PinRepository: %v", err)
	}
	if got := cfg.Skills.Repositories["team"].Ref; got != "v0.2.0" {
		t.Fatalf("Ref = %q", got)
	}
	if err := mgr.PinRepository("team", ""); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if cfg.Skills.Repositories["team"].IsPinned() {
		t.Fatal("expected unpinned after empty ref")
	}
}

func TestSkillsManager_PinRepository_missingErrors(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.PinRepository("ghost", "v1"); err == nil {
		t.Fatal("expected error for unknown catalog")
	}
}

func TestSkillsManager_AddRepository_withRefStoresRef(t *testing.T) {
	cfg := newSkillsCfg(t)
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddRepository("team", "https://example.com/skills.git", "v1.2.0"); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}
	if got := cfg.Skills.Repositories["team"].Ref; got != "v1.2.0" {
		t.Fatalf("Ref = %q", got)
	}
}

func TestSkillsManager_UpdateRepositories_skipsPinned(t *testing.T) {
	cfg := newSkillsCfg(t)
	cfg.Skills.Repositories = map[string]config.RepoSpec{
		"pinned": {URL: "https://example.invalid/never.git", Ref: "v1"},
	}
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpdateRepositories(false); err != nil {
		t.Fatalf("pinned catalog should be skipped, got %v", err)
	}
}

func TestSkillsManager_UpdateRepositories_forceTriesPinned(t *testing.T) {
	cfg := newSkillsCfg(t)
	cfg.Skills.Repositories = map[string]config.RepoSpec{
		"pinned": {URL: "https://example.invalid/never.git", Ref: "v1"},
	}
	mgr, err := NewSkillsManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.UpdateRepositories(true); err == nil {
		t.Fatal("expected --force to attempt the upstream and report the failure")
	}
}
