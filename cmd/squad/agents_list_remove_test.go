package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
	"github.com/spf13/cobra"
)

func TestAgentsList_emptyMessage(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	out, err := runAgentsSubcmd(t, cfg, agentsListCmd)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "No agents found") {
		t.Fatalf("expected empty message, got:\n%s", out)
	}
}

func TestAgentsList_rendersLocalPathAgent(t *testing.T) {
	cfg := newAgentsTestCfg(t)

	// Set up a local path containing one scaffolded-looking agent.
	agentsRoot := t.TempDir()
	agentDir := filepath.Join(agentsRoot, "demo-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo-agent\nversion: 1.2.3\ndescription: a demo agent\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Agents.LocalPaths = []string{agentsRoot}

	out, err := runAgentsSubcmd(t, cfg, agentsListCmd)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"NAME", "demo-agent", "1.2.3", "a demo agent"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestAgentsList_nilCfgErrors(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	if err := runAgentsList(cmd, nil); err == nil {
		t.Fatal("expected error when context lacks a config")
	}
}

func TestAgentsRemove_repositoryByName(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	cfg.Agents.Repositories["team"] = config.RepoSpec{URL: "https://example.com/agents.git"}

	if _, err := runAgentsSubcmd(t, cfg, agentsRemoveCmd, "team"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := cfg.Agents.Repositories["team"]; ok {
		t.Fatal("expected repository to be deleted from cfg")
	}
}

func TestAgentsRemove_unknownErrors(t *testing.T) {
	cfg := newAgentsTestCfg(t)
	_, err := runAgentsSubcmd(t, cfg, agentsRemoveCmd, "ghost")
	if err == nil {
		t.Fatal("expected error when removing an unknown source")
	}
}

func TestAgentsRemove_nilCfgErrors(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	if err := runAgentsRemove(cmd, []string{"x"}); err == nil {
		t.Fatal("expected error when context lacks a config")
	}
}
