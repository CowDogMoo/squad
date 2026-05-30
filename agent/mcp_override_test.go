package agent

import (
	"testing"

	"github.com/cowdogmoo/squad/mcp"
)

func TestApplyMCPOverride_ReplacesAndExpandsTemplates(t *testing.T) {
	t.Parallel()

	b := &Bundle{
		MCPServers: []mcp.ServerConfig{
			{Name: "old", Command: "/usr/bin/old"},
		},
	}

	override := []mcp.ServerConfig{
		{
			Name:    "chrome",
			Command: "npx",
			Args:    []string{`{{.AgentDir}}/runner.sh`, `--mode={{.Mode}}`, `--profile={{.Var "PROFILE"}}`},
		},
	}

	err := ApplyMCPOverride(b, override, "edit", "/agents/grocery-runner", map[string]string{"PROFILE": "amazon"})
	if err != nil {
		t.Fatalf("ApplyMCPOverride: %v", err)
	}

	if len(b.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server after override, got %d", len(b.MCPServers))
	}
	got := b.MCPServers[0]
	if got.Name != "chrome" {
		t.Fatalf("name: expected chrome, got %q", got.Name)
	}
	wantArgs := []string{"/agents/grocery-runner/runner.sh", "--mode=edit", "--profile=amazon"}
	if len(got.Args) != len(wantArgs) {
		t.Fatalf("args length: expected %d, got %d (%v)", len(wantArgs), len(got.Args), got.Args)
	}
	for i, want := range wantArgs {
		if got.Args[i] != want {
			t.Fatalf("args[%d]: expected %q, got %q", i, want, got.Args[i])
		}
	}
}

func TestApplyMCPOverride_EmptySliceClearsServers(t *testing.T) {
	t.Parallel()

	b := &Bundle{
		MCPServers: []mcp.ServerConfig{
			{Name: "chrome", Command: "npx"},
			{Name: "gdrive", Command: "drive-mcp"},
		},
	}

	if err := ApplyMCPOverride(b, []mcp.ServerConfig{}, "edit", "/x", nil); err != nil {
		t.Fatalf("ApplyMCPOverride: %v", err)
	}
	if len(b.MCPServers) != 0 {
		t.Fatalf("expected MCPServers cleared, got %+v", b.MCPServers)
	}
}

func TestApplyMCPOverride_NilBundleReturnsError(t *testing.T) {
	t.Parallel()
	if err := ApplyMCPOverride(nil, nil, "", "", nil); err == nil {
		t.Fatal("expected error for nil bundle")
	}
}

func TestApplyMCPOverride_BadTemplateReturnsError(t *testing.T) {
	t.Parallel()

	b := &Bundle{}
	override := []mcp.ServerConfig{
		{Name: "broken", Command: "{{.NoSuchField}}"},
	}
	if err := ApplyMCPOverride(b, override, "", "", nil); err == nil {
		t.Fatal("expected template-execution error, got nil")
	}
}
