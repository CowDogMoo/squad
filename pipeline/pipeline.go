// Package pipeline defines declarative multi-agent workflow configurations.
// Pipelines specify stages with dependency ordering, parallel execution,
// regression gates, and structured output contracts.
package pipeline

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Pipeline defines a multi-agent workflow with stages, gates, and output config.
type Pipeline struct {
	Name        string  `yaml:"name"`
	Version     string  `yaml:"version"`
	Description string  `yaml:"description,omitempty"`
	Stages      []Stage `yaml:"stages"`
	Gates       []Gate  `yaml:"gates,omitempty"`
	Output      *Output `yaml:"output,omitempty"`
}

// Stage is a unit of work in a pipeline. A stage runs one agent or
// multiple agents in parallel, and may depend on prior stages.
type Stage struct {
	Name      string            `yaml:"name"`
	Agent     string            `yaml:"agent,omitempty"`  // single agent
	Agents    []string          `yaml:"agents,omitempty"` // parallel agents
	DependsOn []string          `yaml:"depends_on,omitempty"`
	Mode      string            `yaml:"mode,omitempty"`      // edit | readonly
	Vars      map[string]string `yaml:"vars,omitempty"`      // stage-specific template vars
	Condition string            `yaml:"condition,omitempty"` // skip condition (evaluated by orchestrator)
	PreGates  []PreGate         `yaml:"pre_gates,omitempty"` // commands to run before agents, output injected into prompt
}

// AgentList returns all agents in the stage (normalizing single vs parallel).
func (s Stage) AgentList() []string {
	if s.Agent != "" {
		return []string{s.Agent}
	}
	return s.Agents
}

// Gate defines a regression check run between pipeline stages.
type Gate struct {
	After     string `yaml:"after"`                // stage name to run after
	Command   string `yaml:"command"`              // shell command to run
	OnFailure string `yaml:"on_failure,omitempty"` // revert | stop (default: stop)
}

// PreGate runs a command before an agent and injects its output into the prompt.
// This is useful for running static analysis tools (clippy, ruff, eslint) and
// feeding structured output to the agent so it doesn't rediscover known issues.
type PreGate struct {
	Command string `yaml:"command"`            // shell command to run
	Label   string `yaml:"label,omitempty"`    // label for the output section (default: command)
	OnError string `yaml:"on_error,omitempty"` // skip | continue | stop (default: continue)
}

// Output configures the pipeline's output format.
type Output struct {
	Format string `yaml:"format,omitempty"` // json | markdown (default: markdown)
}

// Load reads and parses a pipeline configuration file.
func Load(path string) (*Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read pipeline config: %w", err)
	}
	return Parse(data)
}

// Parse parses pipeline YAML bytes into a Pipeline.
func Parse(data []byte) (*Pipeline, error) {
	var p Pipeline
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse pipeline config: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks the pipeline for structural errors.
func (p *Pipeline) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("pipeline name is required")
	}
	if len(p.Stages) == 0 {
		return fmt.Errorf("pipeline must have at least one stage")
	}

	stageNames, err := p.validateStages()
	if err != nil {
		return err
	}

	if err := p.validateDependencies(stageNames); err != nil {
		return err
	}

	return p.validateGates(stageNames)
}

// validateStages checks each stage for name uniqueness and agent configuration.
func (p *Pipeline) validateStages() (map[string]bool, error) {
	stageNames := make(map[string]bool, len(p.Stages))
	for i, s := range p.Stages {
		if s.Name == "" {
			return nil, fmt.Errorf("stage %d: name is required", i)
		}
		if stageNames[s.Name] {
			return nil, fmt.Errorf("duplicate stage name: %q", s.Name)
		}
		stageNames[s.Name] = true

		if len(s.AgentList()) == 0 {
			return nil, fmt.Errorf("stage %q: must specify agent or agents", s.Name)
		}
		if s.Agent != "" && len(s.Agents) > 0 {
			return nil, fmt.Errorf("stage %q: cannot specify both agent and agents", s.Name)
		}
	}
	return stageNames, nil
}

// validateDependencies checks dependency references and detects cycles.
func (p *Pipeline) validateDependencies(stageNames map[string]bool) error {
	for _, s := range p.Stages {
		for _, dep := range s.DependsOn {
			if !stageNames[dep] {
				return fmt.Errorf("stage %q depends on unknown stage %q", s.Name, dep)
			}
			if dep == s.Name {
				return fmt.Errorf("stage %q depends on itself", s.Name)
			}
		}
	}
	return p.detectCycle()
}

// validateGates checks that gate references point to existing stages.
func (p *Pipeline) validateGates(stageNames map[string]bool) error {
	for _, g := range p.Gates {
		if !stageNames[g.After] {
			return fmt.Errorf("gate references unknown stage %q", g.After)
		}
		if g.Command == "" {
			return fmt.Errorf("gate after %q: command is required", g.After)
		}
	}
	return nil
}

// detectCycle checks for circular dependencies using DFS.
func (p *Pipeline) detectCycle() error {
	adj := make(map[string][]string, len(p.Stages))
	for _, s := range p.Stages {
		adj[s.Name] = s.DependsOn
	}

	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // fully processed
	)
	color := make(map[string]int, len(p.Stages))

	var visit func(string) error
	visit = func(name string) error {
		color[name] = gray
		for _, dep := range adj[name] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("cycle detected: %s -> %s", name, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[name] = black
		return nil
	}

	for _, s := range p.Stages {
		if color[s.Name] == white {
			if err := visit(s.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// TopologicalOrder returns stages in dependency-respecting execution order.
// Stages with no unmet dependencies are grouped into tiers that can run in parallel.
func (p *Pipeline) TopologicalOrder() [][]Stage {
	stageMap := make(map[string]Stage, len(p.Stages))
	inDegree := make(map[string]int, len(p.Stages))
	dependents := make(map[string][]string, len(p.Stages))

	for _, s := range p.Stages {
		stageMap[s.Name] = s
		inDegree[s.Name] = len(s.DependsOn)
		for _, dep := range s.DependsOn {
			dependents[dep] = append(dependents[dep], s.Name)
		}
	}

	var tiers [][]Stage
	for {
		var ready []Stage
		for name, deg := range inDegree {
			if deg == 0 {
				ready = append(ready, stageMap[name])
			}
		}
		if len(ready) == 0 {
			break
		}
		tiers = append(tiers, ready)
		for _, s := range ready {
			delete(inDegree, s.Name)
			for _, dep := range dependents[s.Name] {
				inDegree[dep]--
			}
		}
	}

	return tiers
}

// GatesAfter returns all gates configured to run after the named stage.
func (p *Pipeline) GatesAfter(stageName string) []Gate {
	var gates []Gate
	for _, g := range p.Gates {
		if g.After == stageName {
			gates = append(gates, g)
		}
	}
	return gates
}
