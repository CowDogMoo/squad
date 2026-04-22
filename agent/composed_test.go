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
	if !strings.Contains(err.Error(), "must specify agent or agents") {
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
}
