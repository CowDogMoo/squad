package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cowdogmoo/squad/metrics"
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
