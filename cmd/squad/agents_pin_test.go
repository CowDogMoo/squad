package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newAgentsTestCfg installs an isolated XDG layout so the source.Manager's
// config-save side effect can't bleed into the developer's real ~/.config.
func newAgentsTestCfg(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
	t.Setenv("HOME", tmp)
	return &config.Config{
		Agents: config.AgentsConfig{
			Repositories: map[string]config.RepoSpec{},
			LocalPaths:   nil,
		},
	}
}

// runAgentsSubcmd invokes a `squad agents <sub>` command against an injected
// config. Returns combined stdout/stderr plus the RunE error so callers can
// assert on either.
func runAgentsSubcmd(t *testing.T, cfg *config.Config, sub *cobra.Command, args ...string) (string, error) {
	t.Helper()
	// Cobra's command instances retain flag state across calls, so reset
	// every flag to its zero/default value before each invocation. This
	// mirrors what a fresh process invocation would see.
	sub.Flags().VisitAll(func(f *pflag.Flag) {
		_ = sub.Flags().Set(f.Name, f.DefValue)
	})

	sub.SetContext(withConfig(context.Background(), cfg))
	var buf bytes.Buffer
	sub.SetOut(&buf)
	sub.SetErr(&buf)
	if err := sub.ParseFlags(args); err != nil {
		return buf.String(), err
	}
	if err := sub.RunE(sub, sub.Flags().Args()); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

func TestAgentsAdd_withRefStoresPinnedSpec(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	if _, err := runAgentsSubcmd(t, cfg, agentsAddCmd,
		"official", "https://github.com/cowdogmoo/squad-agents.git", "--ref", "v0.4.2",
	); err != nil {
		t.Fatalf("add: %v", err)
	}
	got := cfg.Agents.Repositories["official"]
	if got.URL != "https://github.com/cowdogmoo/squad-agents.git" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Ref != "v0.4.2" {
		t.Errorf("Ref = %q, want v0.4.2", got.Ref)
	}
}

func TestAgentsAdd_refOnLocalPathRejected(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	dir := t.TempDir()
	_, err := runAgentsSubcmd(t, cfg, agentsAddCmd, dir, "--ref", "main")
	if err == nil {
		t.Fatal("expected --ref on local path to error")
	}
	if !strings.Contains(err.Error(), "--ref only applies to git repositories") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentsPin_setsAndUnsets(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	cfg.Agents.Repositories["official"] = config.RepoSpec{
		URL: "https://github.com/cowdogmoo/squad-agents.git",
	}
	if _, err := runAgentsSubcmd(t, cfg, agentsPinCmd, "official", "v0.5.0"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if got := cfg.Agents.Repositories["official"].Ref; got != "v0.5.0" {
		t.Fatalf("Ref = %q, want v0.5.0", got)
	}

	if _, err := runAgentsSubcmd(t, cfg, agentsPinCmd, "official", "--unset"); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if cfg.Agents.Repositories["official"].IsPinned() {
		t.Fatal("expected unpinned after --unset")
	}
}

func TestAgentsPin_unsetWithRefArgRejected(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	cfg.Agents.Repositories["official"] = config.RepoSpec{URL: "https://x.test/r.git"}
	_, err := runAgentsSubcmd(t, cfg, agentsPinCmd, "official", "v1", "--unset")
	if err == nil {
		t.Fatal("expected error when combining --unset with a ref")
	}
}

func TestAgentsPin_missingRepoErrors(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	_, err := runAgentsSubcmd(t, cfg, agentsPinCmd, "ghost", "v1.0.0")
	if err == nil {
		t.Fatal("expected error when pinning an unknown repository")
	}
}

func TestAgentsPin_missingRefArgRejected(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	cfg.Agents.Repositories["official"] = config.RepoSpec{URL: "https://x.test/r.git"}
	_, err := runAgentsSubcmd(t, cfg, agentsPinCmd, "official")
	if err == nil {
		t.Fatal("expected usage error when ref is missing and --unset is not set")
	}
}

func TestAgentsUpdate_skipsPinnedWithoutForce(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	cfg.Agents.Repositories["pinned"] = config.RepoSpec{
		URL: "https://example.invalid/never.git",
		Ref: "v1",
	}
	if _, err := runAgentsSubcmd(t, cfg, agentsUpdateCmd); err != nil {
		t.Fatalf("expected pinned repo to be skipped, got: %v", err)
	}
}

func TestAgentsUpdate_forceAttemptsPinned(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	cfg.Agents.Repositories["pinned"] = config.RepoSpec{
		URL: "https://example.invalid/never.git",
		Ref: "v1",
	}
	_, err := runAgentsSubcmd(t, cfg, agentsUpdateCmd, "--force")
	if err == nil {
		t.Fatal("expected --force to attempt the upstream and surface the failure")
	}
}

func TestAgentsAdd_singleArgUrlDerivesName(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	if _, err := runAgentsSubcmd(t, cfg, agentsAddCmd,
		"https://github.com/cowdogmoo/squad-agents.git",
	); err != nil {
		t.Fatalf("add: %v", err)
	}
	// guessRepoName strips .git and takes the trailing path component.
	if _, ok := cfg.Agents.Repositories["squad-agents"]; !ok {
		t.Fatalf("expected derived name 'squad-agents', got %v",
			cfg.Agents.Repositories)
	}
}

func TestAgentsAdd_twoArgNonURLRejected(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	_, err := runAgentsSubcmd(t, cfg, agentsAddCmd, "alias", "/not/a/url")
	if err == nil {
		t.Fatal("expected error: alias-form second arg must be a git URL")
	}
}

func TestAgentsAdd_localPathSucceeds(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	dir := t.TempDir()
	if _, err := runAgentsSubcmd(t, cfg, agentsAddCmd, dir); err != nil {
		t.Fatalf("add local path: %v", err)
	}
	if len(cfg.Agents.LocalPaths) != 1 || cfg.Agents.LocalPaths[0] != dir {
		t.Fatalf("LocalPaths = %v, want [%s]", cfg.Agents.LocalPaths, dir)
	}
}

func TestAgentsAdd_unpinnedGitSucceeds(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	if _, err := runAgentsSubcmd(t, cfg, agentsAddCmd,
		"team", "https://example.com/agents.git",
	); err != nil {
		t.Fatalf("add: %v", err)
	}
	got := cfg.Agents.Repositories["team"]
	if got.URL != "https://example.com/agents.git" || got.IsPinned() {
		t.Fatalf("got %+v", got)
	}
}

func TestAgentsAdd_duplicateErrors(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	cfg.Agents.Repositories["team"] = config.RepoSpec{
		URL: "https://example.com/agents.git",
	}
	_, err := runAgentsSubcmd(t, cfg, agentsAddCmd,
		"team", "https://example.com/agents.git",
	)
	if err == nil {
		t.Fatal("expected duplicate-add error")
	}
}

func TestAgentsSources_emptyMessage(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	out, err := runAgentsSubcmd(t, cfg, agentsSourcesCmd)
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	if !strings.Contains(out, "No sources configured") {
		t.Fatalf("expected empty-sources message, got:\n%s", out)
	}
}

func TestAgentsSources_localPathsOnly(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	cfg.Agents.LocalPaths = []string{"/tmp/local-agents"}
	out, err := runAgentsSubcmd(t, cfg, agentsSourcesCmd)
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	if !strings.Contains(out, "LOCAL PATHS") || !strings.Contains(out, "/tmp/local-agents") {
		t.Fatalf("expected LOCAL PATHS row, got:\n%s", out)
	}
	if strings.Contains(out, "REPOSITORIES") {
		t.Fatalf("REPOSITORIES section should be omitted, got:\n%s", out)
	}
}

func TestAgentsSources_rendersRefColumn(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	cfg.Agents.Repositories["pinned"] = config.RepoSpec{
		URL: "https://example.com/pinned.git",
		Ref: "v1.0.0",
	}
	cfg.Agents.Repositories["floating"] = config.RepoSpec{
		URL: "https://example.com/floating.git",
	}
	out, err := runAgentsSubcmd(t, cfg, agentsSourcesCmd)
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	if !strings.Contains(out, "REF") {
		t.Errorf("sources output should include REF column, got:\n%s", out)
	}
	if !strings.Contains(out, "v1.0.0") {
		t.Errorf("sources output should include the pinned ref, got:\n%s", out)
	}
	if !strings.Contains(out, "floating") {
		t.Errorf("sources output should include the floating row, got:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "floating") {
			if !strings.HasSuffix(strings.TrimRight(line, " \t"), "-") {
				t.Errorf("unpinned source should render '-' for ref column, got line: %q", line)
			}
		}
	}
}
