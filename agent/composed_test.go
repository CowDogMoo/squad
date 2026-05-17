package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsComposed_WithStages(t *testing.T) {
	m := &Manifest{
		Name: "composed",
		Stages: []ComposedStage{
			{Name: "audit", Agents: []string{"a1", "a2"}},
		},
	}
	if !m.IsComposed() {
		t.Fatal("expected IsComposed() == true for manifest with stages")
	}
}

func TestIsComposed_WithoutStages(t *testing.T) {
	m := &Manifest{
		Name:       "leaf",
		EntryPoint: "system.md",
		Wrapper:    "agent.md",
	}
	if m.IsComposed() {
		t.Fatal("expected IsComposed() == false for leaf manifest")
	}
}

func TestValidate_LeafInlinePromptValid(t *testing.T) {
	m := &Manifest{
		Name:   "weekly-planner",
		Prompt: "You are a weekly family planner...",
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("inline-prompt leaf should validate, got: %v", err)
	}
	if !m.IsInlinePrompt() {
		t.Fatal("expected IsInlinePrompt() == true")
	}
}

func TestValidate_LeafInlinePromptRejectsEntrypoint(t *testing.T) {
	m := &Manifest{
		Name:       "bad",
		Prompt:     "hi",
		EntryPoint: "system.md",
	}
	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "cannot set both prompt and entrypoint") {
		t.Fatalf("expected entrypoint conflict error, got: %v", err)
	}
}

func TestValidate_LeafInlinePromptRejectsWrapper(t *testing.T) {
	m := &Manifest{
		Name:    "bad",
		Prompt:  "hi",
		Wrapper: "agent.md",
	}
	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "cannot set both prompt and wrapper") {
		t.Fatalf("expected wrapper conflict error, got: %v", err)
	}
}

func TestValidate_LeafWorkingDirNone(t *testing.T) {
	m := &Manifest{
		Name:       "weekly-planner",
		Prompt:     "remote only",
		WorkingDir: "none",
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("working_dir: none should validate, got: %v", err)
	}
	if !m.IsRemoteOnly() {
		t.Fatal("expected IsRemoteOnly() == true")
	}
}

func TestValidate_LeafWorkingDirInvalid(t *testing.T) {
	m := &Manifest{
		Name:       "bad",
		Prompt:     "hi",
		WorkingDir: "/tmp/whatever",
	}
	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "working_dir must be empty or \"none\"") {
		t.Fatalf("expected working_dir error, got: %v", err)
	}
}

func TestValidate_ComposedValid(t *testing.T) {
	m := &Manifest{
		Name:    "composed",
		Version: "1.0",
		Stages: []ComposedStage{
			{Name: "analyze", Agents: []string{"a1", "a2"}},
			{Name: "fix", Agent: "fixer", DependsOn: []string{"analyze"}},
		},
		Gates: []ComposedGate{
			{After: "fix", Command: "go build ./..."},
		},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid composed manifest, got: %v", err)
	}
}

func TestValidate_ComposedRejectsEntrypoint(t *testing.T) {
	m := &Manifest{
		Name:       "bad",
		EntryPoint: "system.md",
		Stages:     []ComposedStage{{Name: "s1", Agent: "a1"}},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for composed manifest with entrypoint")
	}
	if !strings.Contains(err.Error(), "cannot have entrypoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedRejectsWrapper(t *testing.T) {
	m := &Manifest{
		Name:    "bad",
		Wrapper: "agent.md",
		Stages:  []ComposedStage{{Name: "s1", Agent: "a1"}},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for composed manifest with wrapper")
	}
	if !strings.Contains(err.Error(), "cannot have wrapper") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedRejectsModels(t *testing.T) {
	m := &Manifest{
		Name:   "bad",
		Models: []ModelPreference{{Model: "gpt-4", Provider: "openai"}},
		Stages: []ComposedStage{{Name: "s1", Agent: "a1"}},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for composed manifest with models")
	}
	if !strings.Contains(err.Error(), "cannot have models") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedRejectsTask(t *testing.T) {
	m := &Manifest{
		Name:   "bad",
		Task:   "task.md",
		Stages: []ComposedStage{{Name: "s1", Agent: "a1"}},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for composed manifest with task")
	}
	if !strings.Contains(err.Error(), "cannot have task") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedDuplicateStage(t *testing.T) {
	m := &Manifest{
		Name: "bad",
		Stages: []ComposedStage{
			{Name: "s1", Agent: "a1"},
			{Name: "s1", Agent: "a2"},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate stage names")
	}
	if !strings.Contains(err.Error(), "duplicate stage name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedNoAgents(t *testing.T) {
	m := &Manifest{
		Name:   "bad",
		Stages: []ComposedStage{{Name: "s1"}},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for stage with no agents")
	}
	if !strings.Contains(err.Error(), "must specify agent, agents, or inline entrypoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedBothAgentAndAgents(t *testing.T) {
	m := &Manifest{
		Name: "bad",
		Stages: []ComposedStage{
			{Name: "s1", Agent: "a1", Agents: []string{"a2"}},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for stage with both agent and agents")
	}
	if !strings.Contains(err.Error(), "cannot specify both") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedInlineValid(t *testing.T) {
	m := &Manifest{
		Name: "inline-ok",
		Stages: []ComposedStage{
			{Name: "s1", EntryPoint: "system.md", Wrapper: "agent.md"},
		},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedInlineMissingWrapper(t *testing.T) {
	m := &Manifest{
		Name: "inline-bad",
		Stages: []ComposedStage{
			{Name: "s1", EntryPoint: "system.md"},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for inline without wrapper")
	}
	if !strings.Contains(err.Error(), "requires wrapper") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedInlineWithExternalAgent(t *testing.T) {
	m := &Manifest{
		Name: "conflict",
		Stages: []ComposedStage{
			{Name: "s1", Agent: "a1", EntryPoint: "system.md", Wrapper: "agent.md"},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for inline with external agent")
	}
	if !strings.Contains(err.Error(), "cannot specify both agent/agents and inline entrypoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedInvalidSummarize(t *testing.T) {
	m := &Manifest{
		Name: "bad-summarize",
		Stages: []ComposedStage{
			{Name: "s1", Agent: "a1", Summarize: "invalid"},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for invalid summarize value")
	}
	if !strings.Contains(err.Error(), "summarize must be auto, always, or never") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedValidSummarize(t *testing.T) {
	for _, val := range []string{"auto", "always", "never", ""} {
		m := &Manifest{
			Name: "ok",
			Stages: []ComposedStage{
				{Name: "s1", Agent: "a1", Summarize: val},
			},
		}
		if err := m.Validate(); err != nil {
			t.Fatalf("summarize=%q: unexpected error: %v", val, err)
		}
	}
}

func TestValidate_ComposedUnknownDep(t *testing.T) {
	m := &Manifest{
		Name: "bad",
		Stages: []ComposedStage{
			{Name: "s1", Agent: "a1", DependsOn: []string{"nonexistent"}},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for unknown dependency")
	}
	if !strings.Contains(err.Error(), "unknown stage") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedSelfDep(t *testing.T) {
	m := &Manifest{
		Name: "bad",
		Stages: []ComposedStage{
			{Name: "s1", Agent: "a1", DependsOn: []string{"s1"}},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for self-dependency")
	}
	if !strings.Contains(err.Error(), "depends on itself") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedCycleDetection(t *testing.T) {
	m := &Manifest{
		Name: "bad",
		Stages: []ComposedStage{
			{Name: "a", Agent: "a1", DependsOn: []string{"b"}},
			{Name: "b", Agent: "a2", DependsOn: []string{"a"}},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for cycle")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedGateUnknownStage(t *testing.T) {
	m := &Manifest{
		Name:   "bad",
		Stages: []ComposedStage{{Name: "s1", Agent: "a1"}},
		Gates:  []ComposedGate{{After: "nonexistent", Command: "echo ok"}},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for gate referencing unknown stage")
	}
	if !strings.Contains(err.Error(), "unknown stage") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedGateNoCommand(t *testing.T) {
	m := &Manifest{
		Name:   "bad",
		Stages: []ComposedStage{{Name: "s1", Agent: "a1"}},
		Gates:  []ComposedGate{{After: "s1"}},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for gate with no command")
	}
	if !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedPartition(t *testing.T) {
	m := &Manifest{
		Name: "valid",
		Stages: []ComposedStage{
			{
				Name:  "s1",
				Agent: "a1",
				Partition: &ComposedPartition{
					By:              "files",
					Glob:            "**/*.go",
					MaxPerPartition: 20,
				},
			},
		},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidate_ComposedPartitionRequiresSingleAgent(t *testing.T) {
	m := &Manifest{
		Name: "bad",
		Stages: []ComposedStage{
			{
				Name:      "s1",
				Agents:    []string{"a1", "a2"},
				Partition: &ComposedPartition{By: "files", Glob: "*.go"},
			},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for partition with multiple agents")
	}
	if !strings.Contains(err.Error(), "partition requires a single agent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedPartitionInvalidBy(t *testing.T) {
	m := &Manifest{
		Name: "bad",
		Stages: []ComposedStage{
			{
				Name:      "s1",
				Agent:     "a1",
				Partition: &ComposedPartition{By: "lines", Glob: "*.go"},
			},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for invalid partition.by")
	}
	if !strings.Contains(err.Error(), "partition.by must be") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedPartitionMissingGlob(t *testing.T) {
	m := &Manifest{
		Name: "bad",
		Stages: []ComposedStage{
			{
				Name:      "s1",
				Agent:     "a1",
				Partition: &ComposedPartition{By: "files"},
			},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for missing partition.glob")
	}
	if !strings.Contains(err.Error(), "partition.glob is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ComposedStageNameRequired(t *testing.T) {
	m := &Manifest{
		Name: "bad",
		Stages: []ComposedStage{
			{Agent: "a1"},
		},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for stage without name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LeafValid(t *testing.T) {
	m := &Manifest{
		Name:       "leaf",
		Version:    "1.0",
		EntryPoint: "system.md",
		Wrapper:    "agent.md",
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid leaf manifest, got: %v", err)
	}
}

func TestValidate_LeafMissingEntrypoint(t *testing.T) {
	m := &Manifest{
		Name:    "bad",
		Wrapper: "agent.md",
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for leaf without entrypoint")
	}
	if !strings.Contains(err.Error(), "entrypoint is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LeafMissingWrapper(t *testing.T) {
	m := &Manifest{
		Name:       "bad",
		EntryPoint: "system.md",
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for leaf without wrapper")
	}
	if !strings.Contains(err.Error(), "wrapper is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MissingName(t *testing.T) {
	m := &Manifest{EntryPoint: "system.md", Wrapper: "agent.md"}
	err := m.Validate()
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadManifest_ComposedFromYAML(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "composed-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	yaml := `name: security-audit
version: "1.0"
description: Parallel security audit

stages:
  - name: scan
    agents:
      - injection-scanner
      - resource-scanner
  - name: fix
    agent: auto-fixer
    depends_on: [scan]
    mode: edit

gates:
  - after: fix
    command: "go build ./..."
    on_failure: stop
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	m, err := LoadManifest(agentDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	if !m.IsComposed() {
		t.Fatal("expected IsComposed() == true")
	}
	if m.Name != "security-audit" {
		t.Fatalf("expected name 'security-audit', got %q", m.Name)
	}
	if m.Description != "Parallel security audit" {
		t.Fatalf("expected description, got %q", m.Description)
	}
	if len(m.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(m.Stages))
	}
	if len(m.Gates) != 1 {
		t.Fatalf("expected 1 gate, got %d", len(m.Gates))
	}

	// Verify stage details.
	if m.Stages[0].Name != "scan" {
		t.Fatalf("expected stage 0 name 'scan', got %q", m.Stages[0].Name)
	}
	if len(m.Stages[0].Agents) != 2 {
		t.Fatalf("expected 2 agents in scan stage, got %d", len(m.Stages[0].Agents))
	}
	if m.Stages[1].Agent != "auto-fixer" {
		t.Fatalf("expected stage 1 agent 'auto-fixer', got %q", m.Stages[1].Agent)
	}
	if m.Stages[1].Mode != "edit" {
		t.Fatalf("expected stage 1 mode 'edit', got %q", m.Stages[1].Mode)
	}
}

func TestLoadManifest_ComposedInvalid(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "bad-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Composed agent with entrypoint — should fail validation.
	yaml := `name: bad
entrypoint: system.md
stages:
  - name: s1
    agent: a1
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadManifest(agentDir)
	if err == nil {
		t.Fatal("expected LoadManifest to fail for invalid composed manifest")
	}
	if !strings.Contains(err.Error(), "cannot have entrypoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestComposedStage_AgentList(t *testing.T) {
	single := ComposedStage{Agent: "a1"}
	if got := single.AgentList(); len(got) != 1 || got[0] != "a1" {
		t.Fatalf("single agent: expected [a1], got %v", got)
	}

	multi := ComposedStage{Agents: []string{"a1", "a2", "a3"}}
	if got := multi.AgentList(); len(got) != 3 {
		t.Fatalf("multi agent: expected 3, got %d", len(got))
	}

	// Inline stage uses the stage name as the agent name.
	inline := ComposedStage{Name: "my-inline", EntryPoint: "system.md"}
	if got := inline.AgentList(); len(got) != 1 || got[0] != "my-inline" {
		t.Fatalf("inline agent: expected [my-inline], got %v", got)
	}
}

func TestComposedStage_IsInline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stage ComposedStage
		want  bool
	}{
		{"not inline", ComposedStage{Agent: "a1"}, false},
		{"inline with entrypoint", ComposedStage{Name: "s1", EntryPoint: "system.md"}, true},
		{"empty entrypoint", ComposedStage{Name: "s1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.stage.IsInline(); got != tt.want {
				t.Fatalf("IsInline() = %v, want %v", got, tt.want)
			}
		})
	}
}
