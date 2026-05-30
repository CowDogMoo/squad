package runner

import (
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/mcp"
)

func TestManifestToPipeline_Basic(t *testing.T) {
	m := &agent.Manifest{
		Name:        "security-audit",
		Version:     "1.0",
		Description: "Parallel security audit",
		Stages: []agent.ComposedStage{
			{
				Name:   "scan",
				Agents: []string{"injection-scanner", "resource-scanner"},
			},
			{
				Name:      "fix",
				Agent:     "auto-fixer",
				DependsOn: []string{"scan"},
				Mode:      "edit",
			},
		},
		Gates: []agent.ComposedGate{
			{After: "fix", Command: "go build ./...", OnFailure: "stop"},
		},
	}

	p, err := ManifestToPipeline(m)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	if p.Name != "security-audit" {
		t.Fatalf("expected name 'security-audit', got %q", p.Name)
	}
	if p.Version != "1.0" {
		t.Fatalf("expected version '1.0', got %q", p.Version)
	}
	if p.Description != "Parallel security audit" {
		t.Fatalf("expected description, got %q", p.Description)
	}
	if len(p.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(p.Stages))
	}
	if len(p.Gates) != 1 {
		t.Fatalf("expected 1 gate, got %d", len(p.Gates))
	}

	// Stage 0: scan
	if p.Stages[0].Name != "scan" {
		t.Fatalf("stage 0 name: expected 'scan', got %q", p.Stages[0].Name)
	}
	if len(p.Stages[0].Agents) != 2 {
		t.Fatalf("stage 0 agents: expected 2, got %d", len(p.Stages[0].Agents))
	}

	// Stage 1: fix
	if p.Stages[1].Agent != "auto-fixer" {
		t.Fatalf("stage 1 agent: expected 'auto-fixer', got %q", p.Stages[1].Agent)
	}
	if p.Stages[1].Mode != "edit" {
		t.Fatalf("stage 1 mode: expected 'edit', got %q", p.Stages[1].Mode)
	}
	if len(p.Stages[1].DependsOn) != 1 || p.Stages[1].DependsOn[0] != "scan" {
		t.Fatalf("stage 1 deps: expected [scan], got %v", p.Stages[1].DependsOn)
	}

	// Gate
	if p.Gates[0].After != "fix" {
		t.Fatalf("gate after: expected 'fix', got %q", p.Gates[0].After)
	}
	if p.Gates[0].Command != "go build ./..." {
		t.Fatalf("gate command: expected 'go build ./...', got %q", p.Gates[0].Command)
	}
}

func TestManifestToPipeline_WithPreGates(t *testing.T) {
	m := &agent.Manifest{
		Name: "with-pregates",
		Stages: []agent.ComposedStage{
			{
				Name:  "lint",
				Agent: "linter",
				PreGates: []agent.ComposedPreGate{
					{Command: "golangci-lint run", Label: "golangci-lint", OnError: "continue"},
				},
			},
		},
	}

	p, err := ManifestToPipeline(m)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	if len(p.Stages[0].PreGates) != 1 {
		t.Fatalf("expected 1 pre-gate, got %d", len(p.Stages[0].PreGates))
	}
	pg := p.Stages[0].PreGates[0]
	if pg.Command != "golangci-lint run" {
		t.Fatalf("pre-gate command: expected 'golangci-lint run', got %q", pg.Command)
	}
	if pg.Label != "golangci-lint" {
		t.Fatalf("pre-gate label: expected 'golangci-lint', got %q", pg.Label)
	}
	if pg.OnError != "continue" {
		t.Fatalf("pre-gate on_error: expected 'continue', got %q", pg.OnError)
	}
}

func TestManifestToPipeline_WithPartition(t *testing.T) {
	m := &agent.Manifest{
		Name: "with-partition",
		Stages: []agent.ComposedStage{
			{
				Name:  "review",
				Agent: "reviewer",
				Partition: &agent.ComposedPartition{
					By:              "files",
					Glob:            "**/*.go",
					MaxPerPartition: 20,
				},
			},
		},
	}

	p, err := ManifestToPipeline(m)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	part := p.Stages[0].Partition
	if part == nil {
		t.Fatal("expected partition, got nil")
	}
	if part.By != "files" {
		t.Fatalf("partition by: expected 'files', got %q", part.By)
	}
	if part.Glob != "**/*.go" {
		t.Fatalf("partition glob: expected '**/*.go', got %q", part.Glob)
	}
	if part.MaxPerPartition != 20 {
		t.Fatalf("partition max: expected 20, got %d", part.MaxPerPartition)
	}
}

func TestManifestToPipeline_WithVarsAndCost(t *testing.T) {
	m := &agent.Manifest{
		Name: "with-vars",
		Stages: []agent.ComposedStage{
			{
				Name:    "scan",
				Agent:   "scanner",
				Vars:    map[string]string{"SCOPE": "internal"},
				MaxCost: 3.50,
			},
		},
	}

	p, err := ManifestToPipeline(m)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	if p.Stages[0].Vars["SCOPE"] != "internal" {
		t.Fatalf("vars: expected SCOPE=internal, got %v", p.Stages[0].Vars)
	}
	if p.Stages[0].MaxCost != 3.50 {
		t.Fatalf("max_cost: expected 3.50, got %f", p.Stages[0].MaxCost)
	}
}

func TestManifestToPipeline_LeafManifestErrors(t *testing.T) {
	m := &agent.Manifest{
		Name:       "leaf",
		EntryPoint: "system.md",
		Wrapper:    "agent.md",
	}

	_, err := ManifestToPipeline(m)
	if err == nil {
		t.Fatal("expected error for leaf manifest")
	}
	if !strings.Contains(err.Error(), "not a composed agent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManifestToPipeline_TopologicalOrder(t *testing.T) {
	m := &agent.Manifest{
		Name: "topo-test",
		Stages: []agent.ComposedStage{
			{Name: "c", Agent: "a3", DependsOn: []string{"a", "b"}},
			{Name: "a", Agent: "a1"},
			{Name: "b", Agent: "a2", DependsOn: []string{"a"}},
		},
	}

	p, err := ManifestToPipeline(m)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	// Verify topological ordering works.
	tiers := p.TopologicalOrder()
	if len(tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %d", len(tiers))
	}
	if tiers[0][0].Name != "a" {
		t.Fatalf("tier 0: expected 'a', got %q", tiers[0][0].Name)
	}
	if tiers[1][0].Name != "b" {
		t.Fatalf("tier 1: expected 'b', got %q", tiers[1][0].Name)
	}
	if tiers[2][0].Name != "c" {
		t.Fatalf("tier 2: expected 'c', got %q", tiers[2][0].Name)
	}
}

func TestManifestToPipeline_InlineConfig(t *testing.T) {
	t.Parallel()

	m := &agent.Manifest{
		Name:    "inline-test",
		Version: "v1",
		Stages: []agent.ComposedStage{
			{
				Name:       "inlined",
				EntryPoint: "system.md",
				Wrapper:    "agent.md",
				Task:       "review",
				References: []string{"ref1.md"},
				Models: []agent.ModelPreference{
					{Model: "gpt-4", Provider: "openai"},
				},
			},
		},
	}

	p, err := ManifestToPipeline(m)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	if len(p.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(p.Stages))
	}
	s := p.Stages[0]
	if s.InlineConfig == nil {
		t.Fatal("expected InlineConfig to be set")
	}
	if s.InlineConfig.EntryPoint != "system.md" {
		t.Fatalf("expected entrypoint system.md, got %q", s.InlineConfig.EntryPoint)
	}
	if s.InlineConfig.Wrapper != "agent.md" {
		t.Fatalf("expected wrapper agent.md, got %q", s.InlineConfig.Wrapper)
	}
	if s.InlineConfig.Task != "review" {
		t.Fatalf("expected task review, got %q", s.InlineConfig.Task)
	}
	if len(s.InlineConfig.References) != 1 || s.InlineConfig.References[0] != "ref1.md" {
		t.Fatalf("expected references [ref1.md], got %v", s.InlineConfig.References)
	}
	if len(s.InlineConfig.Models) != 1 || s.InlineConfig.Models[0].Model != "gpt-4" {
		t.Fatalf("expected model gpt-4, got %v", s.InlineConfig.Models)
	}
}

func TestManifestToPipeline_StageMCPServers(t *testing.T) {
	t.Parallel()

	m := &agent.Manifest{
		Name: "stage-mcp",
		MCPServers: []mcp.ServerConfig{
			{Name: "manifest-level", Command: "echo"},
		},
		Stages: []agent.ComposedStage{
			{
				Name:  "parse",
				Agent: "parser",
				// Empty (non-nil) MCPServers should still copy through —
				// the cmd-layer treats nil as "inherit", []{} as "no MCP".
				MCPServers: []mcp.ServerConfig{},
			},
			{
				Name:  "shop",
				Agent: "shopper",
				MCPServers: []mcp.ServerConfig{
					{Name: "chrome", Command: "npx", Args: []string{"chrome-devtools-mcp@latest"}},
				},
			},
			{
				Name:  "inherit",
				Agent: "other",
				// No MCPServers field — stage should inherit from manifest.
			},
		},
	}

	p, err := ManifestToPipeline(m)
	if err != nil {
		t.Fatalf("ManifestToPipeline: %v", err)
	}

	parse := p.StageByName("parse")
	if parse == nil {
		t.Fatal("parse stage missing")
	}
	if parse.MCPServers == nil {
		t.Fatal("parse stage: expected non-nil empty slice, got nil")
	}
	if len(parse.MCPServers) != 0 {
		t.Fatalf("parse stage: expected 0 MCP servers, got %d", len(parse.MCPServers))
	}

	shop := p.StageByName("shop")
	if shop == nil {
		t.Fatal("shop stage missing")
	}
	if len(shop.MCPServers) != 1 || shop.MCPServers[0].Name != "chrome" {
		t.Fatalf("shop stage: expected chrome MCP server, got %+v", shop.MCPServers)
	}

	inherit := p.StageByName("inherit")
	if inherit == nil {
		t.Fatal("inherit stage missing")
	}
	if inherit.MCPServers != nil {
		t.Fatalf("inherit stage: expected nil MCPServers (signal to inherit), got %+v", inherit.MCPServers)
	}
}

func TestManifestToPipeline_ValidationFailure(t *testing.T) {
	t.Parallel()

	m := &agent.Manifest{
		Name:    "bad-pipeline",
		Version: "v1",
		Stages: []agent.ComposedStage{
			{Name: "s1", Agent: "a1", DependsOn: []string{"nonexistent"}},
		},
	}

	_, err := ManifestToPipeline(m)
	if err == nil {
		t.Fatal("expected validation error for unknown dependency")
	}
	if !strings.Contains(err.Error(), "pipeline validation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
