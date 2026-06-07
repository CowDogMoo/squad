package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAgentManifest(t *testing.T, agentsDir, agentName, content string) {
	t.Helper()
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Write minimal required files
	for _, f := range []string{"system.md", "agent.md"} {
		if err := os.WriteFile(filepath.Join(agentDir, f), []byte("placeholder"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", f, err)
		}
	}
}

func TestEstimateCostLeafAgent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeAgentManifest(t, dir, "go-review", `
name: go-review
version: "1.0"
entrypoint: system.md
wrapper: agent.md
budget:
  estimated_iterations: 30
`)

	node, err := EstimateCost(dir, "go-review", "anthropic", "claude-opus-4-6")
	if err != nil {
		t.Fatalf("EstimateCost: %v", err)
	}
	if node.Agent != "go-review" {
		t.Fatalf("Agent = %s, want go-review", node.Agent)
	}
	if node.EstimatedIters != 30 {
		t.Fatalf("EstimatedIters = %d, want 30", node.EstimatedIters)
	}
	if len(node.Children) != 0 {
		t.Fatalf("Children = %d, want 0", len(node.Children))
	}
}

func TestEstimateCostOrchestrator(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	writeAgentManifest(t, dir, "go-pipeline", `
name: go-pipeline
version: "1.0"
entrypoint: system.md
wrapper: agent.md
budget:
  estimated_iterations: 10
  children:
    - name: go-review
    - name: go-tests
`)
	writeAgentManifest(t, dir, "go-review", `
name: go-review
version: "1.0"
entrypoint: system.md
wrapper: agent.md
budget:
  estimated_iterations: 30
`)
	writeAgentManifest(t, dir, "go-tests", `
name: go-tests
version: "1.0"
entrypoint: system.md
wrapper: agent.md
budget:
  estimated_iterations: 40
`)

	root, err := EstimateCost(dir, "go-pipeline", "anthropic", "claude-opus-4-6")
	if err != nil {
		t.Fatalf("EstimateCost: %v", err)
	}
	if root.Agent != "go-pipeline" {
		t.Fatalf("Agent = %s, want go-pipeline", root.Agent)
	}
	if len(root.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(root.Children))
	}
	if root.Children[0].Agent != "go-review" {
		t.Fatalf("Children[0] = %s, want go-review", root.Children[0].Agent)
	}
	if root.Children[1].Agent != "go-tests" {
		t.Fatalf("Children[1] = %s, want go-tests", root.Children[1].Agent)
	}

	// Total tree cost should be > root cost alone
	if root.TotalTreeCost() <= root.TotalCost {
		t.Fatalf("TotalTreeCost (%f) should exceed root cost (%f)", root.TotalTreeCost(), root.TotalCost)
	}
}

func TestEstimateCostMissingChild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	writeAgentManifest(t, dir, "orchestrator", `
name: orchestrator
version: "1.0"
entrypoint: system.md
wrapper: agent.md
budget:
  estimated_iterations: 10
  children:
    - name: missing-agent
`)

	root, err := EstimateCost(dir, "orchestrator", "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("EstimateCost: %v", err)
	}
	// Should still have a child with default estimates
	if len(root.Children) != 1 {
		t.Fatalf("Children = %d, want 1", len(root.Children))
	}
	if root.Children[0].Agent != "missing-agent" {
		t.Fatalf("Children[0] = %s, want missing-agent", root.Children[0].Agent)
	}
}

func TestEstimateCostDepthLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for i := 0; i < 7; i++ {
		name := agentNameForDepth(i)
		child := ""
		if i < 6 {
			child = "    - name: " + agentNameForDepth(i+1)
		}
		manifest := "name: " + name + "\nversion: '1.0'\nentrypoint: system.md\nwrapper: agent.md\n"
		if child != "" {
			manifest += "budget:\n  children:\n" + child + "\n"
		}
		writeAgentManifest(t, dir, name, manifest)
	}

	_, err := EstimateCost(dir, "agent-0", "openai", "gpt-4o")
	if err == nil || !strings.Contains(err.Error(), "too deep") {
		t.Fatalf("expected depth error, got: %v", err)
	}
}

func agentNameForDepth(i int) string {
	return "agent-" + string(rune('0'+i))
}

func TestFormatEstimate(t *testing.T) {
	t.Parallel()
	node := &EstimateNode{
		Agent:          "go-pipeline",
		EstimatedIters: 10,
		TotalCost:      0.50,
		Children: []*EstimateNode{
			{Agent: "go-review", EstimatedIters: 30, TotalCost: 5.00},
		},
	}

	output := FormatEstimate(node, "anthropic", "claude-opus-4-6")
	if !strings.Contains(output, "go-pipeline") {
		t.Fatalf("output missing go-pipeline: %s", output)
	}
	if !strings.Contains(output, "go-review") {
		t.Fatalf("output missing go-review: %s", output)
	}
	if !strings.Contains(output, "Estimated total") {
		t.Fatalf("output missing total: %s", output)
	}
}

func TestTotalTreeCost(t *testing.T) {
	t.Parallel()
	root := &EstimateNode{
		TotalCost: 1.0,
		Children: []*EstimateNode{
			{TotalCost: 2.0, Children: []*EstimateNode{
				{TotalCost: 3.0},
			}},
			{TotalCost: 4.0},
		},
	}
	got := root.TotalTreeCost()
	want := 10.0
	if got != want {
		t.Fatalf("TotalTreeCost() = %f, want %f", got, want)
	}
}
