package runner

import (
	"fmt"

	"github.com/cowdogmoo/squad/agent"
	pl "github.com/cowdogmoo/squad/pipeline"
)

// ComposedFlags lists flags that are incompatible with composed agents.
// `require-actionable` is deliberately omitted: it is leaf-only and a no-op on
// the pipeline path, so it is tolerated rather than rejected.
var ComposedFlags = []string{
	"system",
	"print-bundle",
	"bundle-out",
	"apply",
	"apply-fallback",
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
			MCPServers:      s.MCPServers,
		}

		// Carry inline agent config through to the pipeline stage.
		// Use the stage name as the agent identifier so the pipeline
		// validator sees a valid agent reference.
		if s.IsInline() {
			ps.Agent = s.Name
			ps.InlineConfig = &pl.InlineConfig{
				EntryPoint: s.EntryPoint,
				Wrapper:    s.Wrapper,
				Task:       s.Task,
				References: s.References,
			}
			for _, model := range s.Models {
				ps.InlineConfig.Models = append(ps.InlineConfig.Models, pl.ModelPreference{
					Model:    model.Model,
					Provider: model.Provider,
				})
			}
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
