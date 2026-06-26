package agent

import (
	"fmt"

	"github.com/cowdogmoo/squad/mcp"
)

// ComposedStage defines a unit of work in a composed agent.
// It mirrors pipeline.Stage but lives in the agent package to avoid
// circular imports between agent and pipeline.
//
// A stage can either reference external agents (agent/agents fields) or define
// an inline agent (entrypoint/wrapper/models fields). These are mutually exclusive.
type ComposedStage struct {
	Name            string             `yaml:"name"`
	Agent           string             `yaml:"agent,omitempty"`
	Agents          []string           `yaml:"agents,omitempty"`
	DependsOn       []string           `yaml:"depends_on,omitempty"`
	Mode            string             `yaml:"mode,omitempty"`
	Vars            map[string]string  `yaml:"vars,omitempty"`
	Condition       string             `yaml:"condition,omitempty"`
	PreGates        []ComposedPreGate  `yaml:"pre_gates,omitempty"`
	MaxCost         float64            `yaml:"max_cost,omitempty"`
	Partition       *ComposedPartition `yaml:"partition,omitempty"`
	Summarize       string             `yaml:"summarize,omitempty"`
	SummarizePrompt string             `yaml:"summarize_prompt,omitempty"`

	// Inline agent fields — define the agent directly in the stage.
	// When set, agent/agents must be empty.
	EntryPoint string            `yaml:"entrypoint,omitempty"`
	Wrapper    string            `yaml:"wrapper,omitempty"`
	Task       string            `yaml:"task,omitempty"`
	Models     []ModelPreference `yaml:"models,omitempty"`
	References []string          `yaml:"references,omitempty"`

	// MCPServers, when non-empty, replaces the parent manifest's
	// MCP server list for this stage only. Used to scope tool surface
	// per stage (e.g. stage 1 has no MCP, stage 2 has chrome+gdrive).
	// Empty means "inherit from the parent manifest".
	MCPServers []mcp.ServerConfig `yaml:"mcp_servers,omitempty"`
}

// IsInline returns true if the stage defines an inline agent.
func (s ComposedStage) IsInline() bool {
	return s.EntryPoint != ""
}

// AgentList returns all agents in the stage (normalizing single vs parallel).
// For inline stages, the stage name is used as the agent name.
func (s ComposedStage) AgentList() []string {
	if s.IsInline() {
		return []string{s.Name}
	}
	if s.Agent != "" {
		return []string{s.Agent}
	}
	return s.Agents
}

// ComposedGate defines a regression check between composed agent stages.
type ComposedGate struct {
	After     string `yaml:"after"`
	Command   string `yaml:"command"`
	OnFailure string `yaml:"on_failure,omitempty"`
}

// ComposedPreGate runs a command before an agent and injects output into the prompt.
type ComposedPreGate struct {
	Command string `yaml:"command"`
	Label   string `yaml:"label,omitempty"`
	OnError string `yaml:"on_error,omitempty"`
}

// ComposedPartition configures automatic work splitting for a composed stage.
type ComposedPartition struct {
	By              string `yaml:"by"`
	Glob            string `yaml:"glob"`
	MaxPerPartition int    `yaml:"max_per_partition"`
}

// IsComposed returns true if the manifest declares a composed agent
// (has stages rather than an entrypoint).
func (m *Manifest) IsComposed() bool {
	return len(m.Stages) > 0
}

// IsInlinePrompt returns true if the manifest uses the inline `prompt:`
// form instead of separate entrypoint+wrapper files.
func (m *Manifest) IsInlinePrompt() bool {
	return m.Prompt != ""
}

// IsRemoteOnly returns true when the agent has declared it does not
// touch the local filesystem (working_dir: none). Local file tools
// (Read/Write/Edit/Glob/Grep/Bash) are not registered for such agents.
func (m *Manifest) IsRemoteOnly() bool {
	return m.WorkingDir == "none"
}

// Validate checks the manifest for structural errors.
// Composed manifests (with stages) must not have entrypoint/wrapper.
// Leaf manifests (with entrypoint) must not have stages.
func (m *Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("manifest name is required")
	}

	if err := m.Requires.Validate(m.Name); err != nil {
		return err
	}

	if err := m.Execution.Validate(); err != nil {
		return err
	}

	if m.IsComposed() {
		return m.validateComposed()
	}
	return m.validateLeaf()
}

func (m *Manifest) validateComposed() error {
	if m.EntryPoint != "" {
		return fmt.Errorf("composed agent %q: cannot have entrypoint (has stages)", m.Name)
	}
	if m.Wrapper != "" {
		return fmt.Errorf("composed agent %q: cannot have wrapper (has stages)", m.Name)
	}
	if len(m.Models) > 0 {
		return fmt.Errorf("composed agent %q: cannot have models (sub-agents declare their own)", m.Name)
	}
	if m.Task != "" {
		return fmt.Errorf("composed agent %q: cannot have task (has stages)", m.Name)
	}

	stageNames, err := m.validateComposedStages()
	if err != nil {
		return err
	}
	if err := m.validateComposedDeps(stageNames); err != nil {
		return err
	}
	return m.validateComposedGates(stageNames)
}

func (m *Manifest) validateComposedStages() (map[string]bool, error) {
	stageNames := make(map[string]bool, len(m.Stages))
	for i, s := range m.Stages {
		if s.Name == "" {
			return nil, fmt.Errorf("composed agent %q: stage %d: name is required", m.Name, i)
		}
		if stageNames[s.Name] {
			return nil, fmt.Errorf("composed agent %q: duplicate stage name: %q", m.Name, s.Name)
		}
		stageNames[s.Name] = true

		if err := m.validateStageAgents(s); err != nil {
			return nil, err
		}
		if err := m.validateStagePartition(s); err != nil {
			return nil, err
		}
		if s.Summarize != "" && s.Summarize != "auto" && s.Summarize != "always" && s.Summarize != "never" {
			return nil, fmt.Errorf("composed agent %q: stage %q: summarize must be auto, always, or never", m.Name, s.Name)
		}
	}
	return stageNames, nil
}

func (m *Manifest) validateStageAgents(s ComposedStage) error {
	hasExternal := s.Agent != "" || len(s.Agents) > 0
	hasInline := s.IsInline()

	if !hasExternal && !hasInline {
		return fmt.Errorf("composed agent %q: stage %q: must specify agent, agents, or inline entrypoint", m.Name, s.Name)
	}
	if hasExternal && hasInline {
		return fmt.Errorf("composed agent %q: stage %q: cannot specify both agent/agents and inline entrypoint", m.Name, s.Name)
	}
	if s.Agent != "" && len(s.Agents) > 0 {
		return fmt.Errorf("composed agent %q: stage %q: cannot specify both agent and agents", m.Name, s.Name)
	}
	if hasInline && s.Wrapper == "" {
		return fmt.Errorf("composed agent %q: stage %q: inline agent requires wrapper", m.Name, s.Name)
	}
	return nil
}

func (m *Manifest) validateStagePartition(s ComposedStage) error {
	if s.Partition == nil {
		return nil
	}
	if s.Agent == "" {
		return fmt.Errorf("composed agent %q: stage %q: partition requires a single agent", m.Name, s.Name)
	}
	if s.Partition.By != "files" {
		return fmt.Errorf("composed agent %q: stage %q: partition.by must be \"files\"", m.Name, s.Name)
	}
	if s.Partition.Glob == "" {
		return fmt.Errorf("composed agent %q: stage %q: partition.glob is required", m.Name, s.Name)
	}
	return nil
}

func (m *Manifest) validateComposedDeps(stageNames map[string]bool) error {
	for _, s := range m.Stages {
		for _, dep := range s.DependsOn {
			if !stageNames[dep] {
				return fmt.Errorf("composed agent %q: stage %q depends on unknown stage %q", m.Name, s.Name, dep)
			}
			if dep == s.Name {
				return fmt.Errorf("composed agent %q: stage %q depends on itself", m.Name, s.Name)
			}
		}
	}
	return m.detectComposedCycle()
}

func (m *Manifest) detectComposedCycle() error {
	adj := make(map[string][]string, len(m.Stages))
	for _, s := range m.Stages {
		adj[s.Name] = s.DependsOn
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(m.Stages))

	var visit func(string) error
	visit = func(name string) error {
		color[name] = gray
		for _, dep := range adj[name] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("composed agent %q: cycle detected: %s -> %s", m.Name, name, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[name] = black
		return nil
	}

	for _, s := range m.Stages {
		if color[s.Name] == white {
			if err := visit(s.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manifest) validateComposedGates(stageNames map[string]bool) error {
	for _, g := range m.Gates {
		if !stageNames[g.After] {
			return fmt.Errorf("composed agent %q: gate references unknown stage %q", m.Name, g.After)
		}
		if g.Command == "" {
			return fmt.Errorf("composed agent %q: gate after %q: command is required", m.Name, g.After)
		}
	}
	return nil
}

func (m *Manifest) validateLeaf() error {
	// Inline-prompt agents: a single `prompt:` field replaces the
	// entrypoint+wrapper file pair. Either form is valid; mixing is not.
	if m.Prompt != "" {
		if m.EntryPoint != "" {
			return fmt.Errorf("agent %q: cannot set both prompt and entrypoint", m.Name)
		}
		if m.Wrapper != "" {
			return fmt.Errorf("agent %q: cannot set both prompt and wrapper", m.Name)
		}
		if m.WorkingDir != "" && m.WorkingDir != "none" {
			return fmt.Errorf("agent %q: working_dir must be empty or \"none\" (got %q)", m.Name, m.WorkingDir)
		}
		return nil
	}
	if m.EntryPoint == "" {
		return fmt.Errorf("agent %q: entrypoint is required", m.Name)
	}
	if m.Wrapper == "" {
		return fmt.Errorf("agent %q: wrapper is required", m.Name)
	}
	if m.WorkingDir != "" && m.WorkingDir != "none" {
		return fmt.Errorf("agent %q: working_dir must be empty or \"none\" (got %q)", m.Name, m.WorkingDir)
	}
	return nil
}
