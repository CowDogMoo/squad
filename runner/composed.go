package runner

import (
	"fmt"

	"github.com/cowdogmoo/squad/agent"
	pl "github.com/cowdogmoo/squad/pipeline"
)

// ComposedFlags lists flags that are incompatible with composed agents.
// Used by the run command to validate flag usage.
var ComposedFlags = []string{
	"system",
	"print-bundle",
	"bundle-out",
	"apply",
	"apply-fallback",
	"require-actionable",
	"stream",
}

// ManifestToPipeline converts a composed agent manifest into a Pipeline.
// The manifest's stages and gates are mapped to their pipeline equivalents.
func ManifestToPipeline(m *agent.Manifest) (*pl.Pipeline, error) {
	if !m.IsComposed() {
		return nil, fmt.Errorf("manifest %q is not a composed agent", m.Name)
	}

	p := &pl.Pipeline{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
	}

	for _, s := range m.Stages {
		ps := pl.Stage{
			Name:            s.Name,
			Agent:           s.Agent,
			Agents:          s.Agents,
			DependsOn:       s.DependsOn,
			Mode:            s.Mode,
			Vars:            s.Vars,
			Condition:       s.Condition,
			MaxCost:         s.MaxCost,
			Summarize:       s.Summarize,
			SummarizePrompt: s.SummarizePrompt,
		}

		for _, pg := range s.PreGates {
			ps.PreGates = append(ps.PreGates, pl.PreGate{
				Command: pg.Command,
				Label:   pg.Label,
				OnError: pg.OnError,
			})
		}

		if s.Partition != nil {
			ps.Partition = &pl.Partition{
				By:              s.Partition.By,
				Glob:            s.Partition.Glob,
				MaxPerPartition: s.Partition.MaxPerPartition,
			}
		}

		p.Stages = append(p.Stages, ps)
	}

	for _, g := range m.Gates {
		p.Gates = append(p.Gates, pl.Gate{
			After:     g.After,
			Command:   g.Command,
			OnFailure: g.OnFailure,
		})
	}

	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("composed agent %q: pipeline validation failed: %w", m.Name, err)
	}

	return p, nil
}
