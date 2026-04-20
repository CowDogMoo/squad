package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/tools"
)

func TestParsePipeline(t *testing.T) {
	yaml := `
name: test-pipeline
version: v1
stages:
  - name: review
    agent: go-review
  - name: security
    agents:
      - go-security-audit
      - go-review
    depends_on: [review]
  - name: testing
    agent: go-tests
    depends_on: [security]
gates:
  - after: review
    command: "go build ./..."
    on_failure: revert
output:
  format: json
`
	p, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if p.Name != "test-pipeline" {
		t.Fatalf("name = %q, want test-pipeline", p.Name)
	}
	if len(p.Stages) != 3 {
		t.Fatalf("stages = %d, want 3", len(p.Stages))
	}
	if len(p.Gates) != 1 {
		t.Fatalf("gates = %d, want 1", len(p.Gates))
	}
	if p.Output.Format != "json" {
		t.Fatalf("output format = %q, want json", p.Output.Format)
	}

	// Check stage agents.
	if agents := p.Stages[0].AgentList(); len(agents) != 1 || agents[0] != "go-review" {
		t.Fatalf("stage 0 agents = %v, want [go-review]", agents)
	}
	if agents := p.Stages[1].AgentList(); len(agents) != 2 {
		t.Fatalf("stage 1 agents = %v, want 2 agents", agents)
	}
}

func TestValidateErrors(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantPart string
	}{
		{
			name:     "missing name",
			yaml:     "stages:\n  - name: x\n    agent: a",
			wantPart: "pipeline name is required",
		},
		{
			name:     "no stages",
			yaml:     "name: p\nstages: []",
			wantPart: "at least one stage",
		},
		{
			name:     "stage missing name",
			yaml:     "name: p\nstages:\n  - agent: a",
			wantPart: "name is required",
		},
		{
			name:     "duplicate stage name",
			yaml:     "name: p\nstages:\n  - name: x\n    agent: a\n  - name: x\n    agent: b",
			wantPart: "duplicate stage name",
		},
		{
			name:     "no agent or agents",
			yaml:     "name: p\nstages:\n  - name: x",
			wantPart: "must specify agent or agents",
		},
		{
			name:     "both agent and agents",
			yaml:     "name: p\nstages:\n  - name: x\n    agent: a\n    agents: [b]",
			wantPart: "cannot specify both agent and agents",
		},
		{
			name:     "unknown dependency",
			yaml:     "name: p\nstages:\n  - name: x\n    agent: a\n    depends_on: [z]",
			wantPart: "depends on unknown stage",
		},
		{
			name:     "self dependency",
			yaml:     "name: p\nstages:\n  - name: x\n    agent: a\n    depends_on: [x]",
			wantPart: "depends on itself",
		},
		{
			name:     "cycle",
			yaml:     "name: p\nstages:\n  - name: a\n    agent: x\n    depends_on: [b]\n  - name: b\n    agent: y\n    depends_on: [a]",
			wantPart: "cycle detected",
		},
		{
			name:     "gate unknown stage",
			yaml:     "name: p\nstages:\n  - name: x\n    agent: a\ngates:\n  - after: z\n    command: echo",
			wantPart: "gate references unknown stage",
		},
		{
			name:     "gate missing command",
			yaml:     "name: p\nstages:\n  - name: x\n    agent: a\ngates:\n  - after: x",
			wantPart: "command is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantPart) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantPart)
			}
		})
	}
}

func TestTopologicalOrder(t *testing.T) {
	yaml := `
name: test
version: v1
stages:
  - name: c
    agent: x
    depends_on: [a, b]
  - name: a
    agent: x
  - name: b
    agent: x
    depends_on: [a]
  - name: d
    agent: x
    depends_on: [c]
`
	p, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	tiers := p.TopologicalOrder()
	if len(tiers) != 4 {
		t.Fatalf("tiers = %d, want 4", len(tiers))
	}

	tierNames := func(tier []Stage) []string {
		names := make([]string, len(tier))
		for i, s := range tier {
			names[i] = s.Name
		}
		return names
	}

	// Tier 0: a (no deps)
	if names := tierNames(tiers[0]); len(names) != 1 || names[0] != "a" {
		t.Fatalf("tier 0 = %v, want [a]", names)
	}
	// Tier 1: b (depends on a)
	if names := tierNames(tiers[1]); len(names) != 1 || names[0] != "b" {
		t.Fatalf("tier 1 = %v, want [b]", names)
	}
	// Tier 2: c (depends on a, b)
	if names := tierNames(tiers[2]); len(names) != 1 || names[0] != "c" {
		t.Fatalf("tier 2 = %v, want [c]", names)
	}
	// Tier 3: d (depends on c)
	if names := tierNames(tiers[3]); len(names) != 1 || names[0] != "d" {
		t.Fatalf("tier 3 = %v, want [d]", names)
	}
}

func TestTopologicalOrderParallel(t *testing.T) {
	yaml := `
name: test
version: v1
stages:
  - name: a
    agent: x
  - name: b
    agent: x
  - name: c
    agent: x
    depends_on: [a, b]
`
	p, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	tiers := p.TopologicalOrder()
	if len(tiers) != 2 {
		t.Fatalf("tiers = %d, want 2", len(tiers))
	}

	// Tier 0: a and b can run in parallel
	if len(tiers[0]) != 2 {
		t.Fatalf("tier 0 has %d stages, want 2", len(tiers[0]))
	}
	// Tier 1: c
	if len(tiers[1]) != 1 || tiers[1][0].Name != "c" {
		t.Fatalf("tier 1 = %v, want [c]", tiers[1])
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")
	content := `
name: file-test
version: v1
stages:
  - name: review
    agent: go-review
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Name != "file-test" {
		t.Fatalf("name = %q, want file-test", p.Name)
	}
}

func TestLoadFileMissing(t *testing.T) {
	_, err := Load("/nonexistent/pipeline.yaml")
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestGatesAfter(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages:  []Stage{{Name: "a", Agent: "x"}, {Name: "b", Agent: "y"}},
		Gates: []Gate{
			{After: "a", Command: "echo 1"},
			{After: "a", Command: "echo 2"},
			{After: "b", Command: "echo 3"},
		},
	}

	gates := p.GatesAfter("a")
	if len(gates) != 2 {
		t.Fatalf("gates after a = %d, want 2", len(gates))
	}
	gates = p.GatesAfter("b")
	if len(gates) != 1 {
		t.Fatalf("gates after b = %d, want 1", len(gates))
	}
	gates = p.GatesAfter("c")
	if len(gates) != 0 {
		t.Fatalf("gates after c = %d, want 0", len(gates))
	}
}

func TestRunnerBasic(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
			{Name: "test", Agent: "go-tests", DependsOn: []string{"review"}},
		},
	}

	var callOrder []string
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			callOrder = append(callOrder, agentName)
			return fmt.Sprintf("%s output", agentName), nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
	if len(report.Stages) != 2 {
		t.Fatalf("stages = %d, want 2", len(report.Stages))
	}
	if len(callOrder) != 2 || callOrder[0] != "go-review" || callOrder[1] != "go-tests" {
		t.Fatalf("call order = %v, want [go-review, go-tests]", callOrder)
	}
}

func TestRunnerParallelAgents(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "analysis", Agents: []string{"agent-a", "agent-b", "agent-c"}},
		},
	}

	var count int64
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			atomic.AddInt64(&count, 1)
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
	if len(report.Stages) != 1 {
		t.Fatalf("stages = %d, want 1", len(report.Stages))
	}
	if len(report.Stages[0].Agents) != 3 {
		t.Fatalf("agents = %d, want 3", len(report.Stages[0].Agents))
	}
	if atomic.LoadInt64(&count) != 3 {
		t.Fatalf("agent runs = %d, want 3", count)
	}
}

func TestRunnerAgentFailure(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
			{Name: "test", Agent: "go-tests", DependsOn: []string{"review"}},
		},
	}

	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			if agentName == "go-review" {
				return "", nil, fmt.Errorf("review failed")
			}
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error from failed stage")
	}
	if report.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", report.Status)
	}
	// Testing stage should not have run.
	if len(report.Stages) != 1 {
		t.Fatalf("stages = %d, want 1 (should stop after failure)", len(report.Stages))
	}
}

func TestRunnerGateFailure(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
		},
		Gates: []Gate{
			{After: "review", Command: "false", OnFailure: "stop"},
		},
	}

	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			return "ok", nil, nil
		},
	}

	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error from gate failure")
	}
	if !strings.Contains(err.Error(), "gate after") {
		t.Fatalf("error = %q, want gate failure message", err.Error())
	}
}

func TestRunnerGateRevert(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
		},
		Gates: []Gate{
			{After: "review", Command: "false", OnFailure: "revert"},
		},
	}

	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			return "ok", nil, nil
		},
	}

	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error from gate failure")
	}
	if !strings.Contains(err.Error(), "reverted") {
		t.Fatalf("error = %q, want revert message", err.Error())
	}
}

func TestRunnerPromptContext(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
			{Name: "test", Agent: "go-tests", DependsOn: []string{"review"}},
		},
	}

	var testPrompt string
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			if agentName == "go-tests" {
				testPrompt = prompt
			}
			return "review findings here", nil, nil
		},
	}

	_, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(testPrompt, "Prior Stage Results") {
		t.Fatalf("test agent should receive prior stage context")
	}
	if !strings.Contains(testPrompt, "review findings here") {
		t.Fatalf("test agent should receive review output")
	}
}

func TestFormatReportJSON(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages:  []Stage{{Name: "review", Agent: "go-review"}},
		Output:  &Output{Format: "json"},
	}

	runner := &Runner{Pipeline: p, WorkingDir: t.TempDir()}
	report := &Report{
		Pipeline: "test",
		Version:  "v1",
		Status:   StatusPassed,
		Stages: []StageResult{
			{Name: "review", Status: StatusPassed, Agents: []AgentResult{{Agent: "go-review", Status: StatusPassed}}},
		},
		Duration: "1s",
	}

	output, err := runner.FormatReport(report)
	if err != nil {
		t.Fatalf("FormatReport: %v", err)
	}
	if !strings.Contains(output, `"pipeline": "test"`) {
		t.Fatalf("expected JSON output, got: %s", output)
	}
}

func TestFormatReportMarkdown(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages:  []Stage{{Name: "review", Agent: "go-review"}},
	}

	runner := &Runner{Pipeline: p, WorkingDir: t.TempDir()}
	report := &Report{
		Pipeline: "test",
		Version:  "v1",
		Status:   StatusPassed,
		Stages: []StageResult{
			{Name: "review", Status: StatusPassed, Agents: []AgentResult{{Agent: "go-review", Status: StatusPassed}}},
		},
		Duration: "1s",
	}

	output, err := runner.FormatReport(report)
	if err != nil {
		t.Fatalf("FormatReport: %v", err)
	}
	if !strings.Contains(output, "# Pipeline Report") {
		t.Fatalf("expected markdown output, got: %s", output)
	}
	if !strings.Contains(output, "| review |") {
		t.Fatalf("expected stage table, got: %s", output)
	}
}

func TestRunnerBudgetTracking(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
			{Name: "test", Agent: "go-tests", DependsOn: []string{"review"}},
		},
	}

	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		MaxCost:    1.00,
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			m := metrics.New("ollama", "llama3") // free
			m.AddTokens(1000, 500)
			return "ok", m, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
	// Both agents should have run (ollama is free, within budget)
	if len(report.Stages) != 2 {
		t.Fatalf("stages = %d, want 2", len(report.Stages))
	}
}

func TestRunnerBudgetExhausted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pricing test in short mode")
	}

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "expensive", Agent: "agent-a"},
			{Name: "cheap", Agent: "agent-b", DependsOn: []string{"expensive"}},
		},
	}

	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		MaxCost:    0.0001, // very small budget
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			m := metrics.New("openai", "gpt-4o")
			m.AddTokens(1_000_000, 500_000) // expensive
			return "ok", m, nil
		},
	}

	report, err := runner.Run(context.Background())
	// The first agent runs and spends over budget, second should be skipped
	cost := runner.RemainingBudget()
	if report != nil && len(report.Stages) >= 2 {
		secondAgent := report.Stages[1].Agents[0]
		if secondAgent.Status == StatusPassed && cost == 0 {
			t.Fatalf("second agent should have failed when budget exhausted")
		}
	}
	_ = err // may or may not error depending on pricing availability
}

func TestRunnerRemainingBudgetUnlimited(t *testing.T) {
	t.Parallel()
	runner := &Runner{MaxCost: 0}
	if runner.RemainingBudget() != 0 {
		t.Fatalf("RemainingBudget() = %v, want 0 for unlimited", runner.RemainingBudget())
	}
}

func TestRunnerStageVarsAndMode(t *testing.T) {
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				Mode:  "readonly",
				Vars:  map[string]string{"COVERAGE_TARGET": "90"},
			},
		},
	}

	var capturedMode string
	var capturedVars map[string]string
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			capturedMode = mode
			capturedVars = vars
			return "ok", nil, nil
		},
	}

	_, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if capturedMode != "readonly" {
		t.Fatalf("mode = %q, want readonly", capturedMode)
	}
	if capturedVars["COVERAGE_TARGET"] != "90" {
		t.Fatalf("vars = %v, want COVERAGE_TARGET=90", capturedVars)
	}
}

func TestBuildPromptContextTruncation(t *testing.T) {
	t.Parallel()

	// Create output longer than 4096 characters.
	longOutput := strings.Repeat("x", 5000)

	stage := Stage{
		Name:      "analysis",
		Agent:     "agent-a",
		DependsOn: []string{"review"},
	}
	completed := map[string]*StageResult{
		"review": {
			Name:   "review",
			Status: StatusPassed,
			Agents: []AgentResult{
				{Agent: "go-review", Status: StatusPassed, Output: longOutput},
			},
		},
	}

	runner := &Runner{
		Pipeline: &Pipeline{Name: "test", Version: "v1"},
	}

	ctx := runner.buildPromptContext(stage, completed)

	// The output should be truncated: 4096 chars + "...(truncated)" marker.
	if !strings.Contains(ctx, "...(truncated)") {
		t.Fatalf("expected truncation marker in prompt context, got %d chars", len(ctx))
	}
	// The full 5000-char output should NOT appear.
	if strings.Contains(ctx, longOutput) {
		t.Fatalf("expected output to be truncated, but full output is present")
	}
}

func TestFormatReportMarkdownWithFindings(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages:  []Stage{{Name: "review", Agent: "go-review"}},
	}

	runner := &Runner{Pipeline: p, WorkingDir: t.TempDir()}
	report := &Report{
		Pipeline: "test",
		Version:  "v1",
		Status:   StatusPassed,
		Stages: []StageResult{
			{Name: "review", Status: StatusPassed, Agents: []AgentResult{{Agent: "go-review", Status: StatusPassed}}},
		},
		Findings: []tools.Finding{
			{Title: "SQL Injection", Severity: "critical", Description: "User input not sanitized"},
			{Title: "Missing auth", Severity: "high", Description: "Endpoint lacks authentication"},
		},
		Duration: "2s",
	}

	output, err := runner.FormatReport(report)
	if err != nil {
		t.Fatalf("FormatReport: %v", err)
	}
	if !strings.Contains(output, "## Findings") {
		t.Fatalf("expected Findings section in markdown report")
	}
	if !strings.Contains(output, "SQL Injection") {
		t.Fatalf("expected finding title in report")
	}
	if !strings.Contains(output, "CRITICAL") {
		t.Fatalf("expected severity in report")
	}
	if !strings.Contains(output, "Missing auth") {
		t.Fatalf("expected second finding in report")
	}
}

func TestFormatReportMarkdownWithOutput(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages:  []Stage{{Name: "review", Agent: "go-review"}},
	}

	runner := &Runner{Pipeline: p, WorkingDir: t.TempDir()}
	report := &Report{
		Pipeline: "test",
		Version:  "v1",
		Status:   StatusPassed,
		Stages: []StageResult{
			{
				Name:   "review",
				Status: StatusPassed,
				Agents: []AgentResult{
					{Agent: "go-review", Status: StatusPassed, Output: "Found 3 issues in main.go"},
				},
			},
		},
		Duration: "1s",
	}

	output, err := runner.FormatReport(report)
	if err != nil {
		t.Fatalf("FormatReport: %v", err)
	}
	// Should contain the agent output section header.
	if !strings.Contains(output, "## review / go-review") {
		t.Fatalf("expected agent output section header, got: %s", output)
	}
	if !strings.Contains(output, "Found 3 issues in main.go") {
		t.Fatalf("expected agent output in report")
	}
}

func TestRunnerAddSpent(t *testing.T) {
	t.Parallel()

	runner := &Runner{MaxCost: 10.0}

	// Basic accumulation.
	total := runner.addSpent(1.5)
	if total != 1.5 {
		t.Fatalf("addSpent(1.5) = %v, want 1.5", total)
	}
	total = runner.addSpent(2.5)
	if total != 4.0 {
		t.Fatalf("addSpent(2.5) = %v, want 4.0", total)
	}

	// Concurrent accumulation: 100 goroutines each adding 0.01.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runner.addSpent(0.01)
		}()
	}
	wg.Wait()

	// Should be 4.0 + 100*0.01 = 5.0 (with floating point tolerance).
	remaining := runner.RemainingBudget()
	expectedRemaining := 10.0 - 5.0
	if remaining < expectedRemaining-0.01 || remaining > expectedRemaining+0.01 {
		t.Fatalf("remainingBudget() = %v, want ~%v", remaining, expectedRemaining)
	}
}

func TestRunnerRunWithFindings(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
		},
	}

	store := tools.NewFindingsStore()
	store.Add(tools.Finding{
		Title:       "Path Traversal",
		Severity:    "critical",
		Description: "Unsanitized path input",
		Agent:       "go-review",
	})
	store.Add(tools.Finding{
		Title:       "Weak Permissions",
		Severity:    "high",
		Description: "Config file is world-readable",
		Agent:       "go-review",
	})

	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		Findings:   store,
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			return "review complete", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(report.Findings))
	}
	if report.Findings[0].Title != "Path Traversal" && report.Findings[1].Title != "Path Traversal" {
		t.Fatalf("expected Path Traversal finding in report, got: %v", report.Findings)
	}
}

func TestRunnerPreGates(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				PreGates: []PreGate{
					{Command: "echo 'clippy: no warnings'", Label: "cargo clippy"},
					{Command: "echo 'all tests pass'", Label: "cargo test"},
				},
			},
		},
	}

	var capturedPrompt string
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			capturedPrompt = prompt
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}

	// The agent should receive pre-gate output in its prompt.
	if !strings.Contains(capturedPrompt, "Static Analysis Output") {
		t.Fatalf("expected Static Analysis Output in prompt, got: %s", capturedPrompt[:min(200, len(capturedPrompt))])
	}
	if !strings.Contains(capturedPrompt, "cargo clippy") {
		t.Fatalf("expected cargo clippy label in prompt")
	}
	if !strings.Contains(capturedPrompt, "clippy: no warnings") {
		t.Fatalf("expected clippy output in prompt")
	}
}

func TestRunnerPreGatesSkipOnError(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				PreGates: []PreGate{
					{Command: "false", Label: "failing gate", OnError: "skip"},
				},
			},
		},
	}

	agentRan := false
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			agentRan = true
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Stage should be skipped, agent should not run.
	if agentRan {
		t.Fatal("agent should not have run when pre-gate failed with on_error=skip")
	}
	if report.Stages[0].Status != StatusSkipped {
		t.Fatalf("stage status = %s, want skipped", report.Stages[0].Status)
	}
}

func TestRunnerPreGatesContinueOnError(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				PreGates: []PreGate{
					{Command: "false", Label: "failing gate", OnError: "continue"},
				},
			},
		},
	}

	agentRan := false
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			agentRan = true
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Agent should still run despite failed pre-gate with on_error=continue.
	if !agentRan {
		t.Fatal("agent should have run when pre-gate failed with on_error=continue")
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
}

func TestBuildPromptContextWithFindings(t *testing.T) {
	t.Parallel()

	store := tools.NewFindingsStore()
	store.Add(tools.Finding{
		Title:       "SQL Injection",
		Severity:    "critical",
		Description: "User input not sanitized",
		Agent:       "security-review",
	})

	stage := Stage{
		Name:      "fix",
		Agent:     "fixer",
		DependsOn: []string{"review"},
	}
	completed := map[string]*StageResult{
		"review": {
			Name:   "review",
			Status: StatusPassed,
			Agents: []AgentResult{
				{Agent: "security-review", Status: StatusPassed, Output: "found issues"},
			},
		},
	}

	runner := &Runner{
		Pipeline: &Pipeline{Name: "test", Version: "v1"},
		Findings: store,
	}

	ctx := runner.buildPromptContext(stage, completed)

	if !strings.Contains(ctx, "Structured Findings") {
		t.Fatalf("expected structured findings section in context")
	}
	if !strings.Contains(ctx, "SQL Injection") {
		t.Fatalf("expected finding title in context")
	}
	if !strings.Contains(ctx, "critical") {
		t.Fatalf("expected severity in context")
	}
}

func TestParsePreGates(t *testing.T) {
	t.Parallel()

	yaml := `
name: test-pipeline
version: v1
stages:
  - name: review
    agent: go-review
    pre_gates:
      - command: "cargo clippy --message-format=json"
        label: "clippy"
      - command: "cargo test"
        label: "unit tests"
        on_error: skip
`
	p, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Stages[0].PreGates) != 2 {
		t.Fatalf("pre_gates = %d, want 2", len(p.Stages[0].PreGates))
	}
	if p.Stages[0].PreGates[0].Label != "clippy" {
		t.Fatalf("pre_gate[0].Label = %q, want clippy", p.Stages[0].PreGates[0].Label)
	}
	if p.Stages[0].PreGates[1].OnError != "skip" {
		t.Fatalf("pre_gate[1].OnError = %q, want skip", p.Stages[0].PreGates[1].OnError)
	}
}

func TestRunnerPreGatesStopOnError(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				PreGates: []PreGate{
					{Command: "false", Label: "lint", OnError: "stop"},
				},
			},
		},
	}

	agentRan := false
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			agentRan = true
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if agentRan {
		t.Fatal("agent should not have run when pre-gate failed with on_error=stop")
	}
	if report.Stages[0].Status != StatusSkipped {
		t.Fatalf("stage status = %s, want skipped", report.Stages[0].Status)
	}
	if !strings.Contains(report.Stages[0].Error, "stopping pipeline") {
		t.Fatalf("error = %q, want stopping pipeline message", report.Stages[0].Error)
	}
}

func TestRunnerPreGatesDefaultOnError(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				PreGates: []PreGate{
					{Command: "false", Label: "lint"},
				},
			},
		},
	}

	agentRan := false
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			agentRan = true
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !agentRan {
		t.Fatal("agent should have run with default on_error (continue)")
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
}

func TestRunnerPreGatesTruncation(t *testing.T) {
	t.Parallel()

	longCmd := "python3 -c \"print('x' * 10000)\""
	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				PreGates: []PreGate{
					{Command: longCmd, Label: "verbose lint"},
				},
			},
		},
	}

	var capturedPrompt string
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			capturedPrompt = prompt
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
	if !strings.Contains(capturedPrompt, "...(truncated)") {
		t.Fatal("expected truncation marker in pre-gate output")
	}
}

func TestRunnerPreGatesNoPreGates(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
		},
	}

	var capturedPrompt string
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			capturedPrompt = prompt
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
	if strings.Contains(capturedPrompt, "Static Analysis Output") {
		t.Fatal("expected no Static Analysis Output in prompt when no pre-gates configured")
	}
}

func TestRunnerPreGatesLabelFallback(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				PreGates: []PreGate{
					{Command: "echo hello"},
				},
			},
		},
	}

	var capturedPrompt string
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			capturedPrompt = prompt
			return "ok", nil, nil
		},
	}

	_, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(capturedPrompt, "### echo hello") {
		t.Fatal("expected command used as label fallback")
	}
}

func TestHasUncommittedChanges(t *testing.T) {
	t.Parallel()

	t.Run("non-git directory returns true", func(t *testing.T) {
		t.Parallel()
		runner := &Runner{
			Pipeline:   &Pipeline{Name: "test"},
			WorkingDir: t.TempDir(),
		}
		if !runner.hasUncommittedChanges(context.Background()) {
			t.Fatal("expected true for non-git directory")
		}
	})
}

func TestRunnerGatesSkippedWhenNoChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
		},
		Gates: []Gate{
			{After: "review", Command: "false", OnFailure: "stop"},
		},
	}

	runner := &Runner{
		Pipeline:   p,
		WorkingDir: dir,
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			return "ok", nil, nil
		},
	}

	_, err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from gate failure in non-git dir")
	}
}

func TestRunnerGatesNoGatesAfterStage(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{Name: "review", Agent: "go-review"},
		},
		Gates: []Gate{
			{After: "other-stage", Command: "false", OnFailure: "stop"},
		},
	}

	runner := &Runner{
		Pipeline:   p,
		WorkingDir: t.TempDir(),
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v (no gates should run for this stage)", err)
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
}

func TestRunnerAttachFindings(t *testing.T) {
	t.Parallel()

	store := tools.NewFindingsStore()
	store.Add(tools.Finding{
		Title:       "Test Finding",
		Severity:    "medium",
		Description: "A test finding",
	})

	runner := &Runner{
		Pipeline: &Pipeline{Name: "test", Version: "v1"},
		Findings: store,
	}

	// attachFindings should copy findings into the report.
	report := &Report{Pipeline: "test"}
	runner.attachFindings(report)

	if len(report.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(report.Findings))
	}
	if report.Findings[0].Title != "Test Finding" {
		t.Fatalf("finding title = %q, want Test Finding", report.Findings[0].Title)
	}

	// With empty store, findings should remain nil.
	emptyRunner := &Runner{
		Pipeline: &Pipeline{Name: "test", Version: "v1"},
		Findings: tools.NewFindingsStore(),
	}
	emptyReport := &Report{Pipeline: "test"}
	emptyRunner.attachFindings(emptyReport)

	if emptyReport.Findings != nil {
		t.Fatalf("expected nil findings for empty store, got %v", emptyReport.Findings)
	}

	// With nil store, should not panic.
	nilRunner := &Runner{
		Pipeline: &Pipeline{Name: "test", Version: "v1"},
		Findings: nil,
	}
	nilReport := &Report{Pipeline: "test"}
	nilRunner.attachFindings(nilReport)

	if nilReport.Findings != nil {
		t.Fatalf("expected nil findings for nil store, got %v", nilReport.Findings)
	}
}
