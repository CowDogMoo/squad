package pipeline

import (
	"context"
	"testing"

	"github.com/cowdogmoo/squad/metrics"
)

func TestCountFiles(t *testing.T) {
	t.Parallel()
	parts := [][]string{{"a.go", "b.go"}, {"c.go"}, {}, {"d.go", "e.go", "f.go"}}
	if got, want := countFiles(parts), 6; got != want {
		t.Fatalf("countFiles = %d, want %d", got, want)
	}
}

func TestRunAgentsParallel(t *testing.T) {
	t.Parallel()

	r := &Runner{
		Pipeline: &Pipeline{Name: "test", Version: "v1"},
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			// Return a small distinctive string so we can spot wiring issues.
			return agentName + " ok", nil, nil
		},
	}

	agents := []string{"a1", "a2", "a3"}
	stage := Stage{Name: "stage"}
	got := r.runAgentsParallel(context.Background(), agents, stage, "ctx")

	if len(got) != len(agents) {
		t.Fatalf("results = %d, want %d", len(got), len(agents))
	}
	// Order must match input slice indices.
	for i, ar := range got {
		if ar.Agent != agents[i] {
			t.Fatalf("result[%d].Agent = %q, want %q", i, ar.Agent, agents[i])
		}
		if ar.Status != StatusPassed {
			t.Fatalf("result[%d].Status = %s, want passed", i, ar.Status)
		}
	}
}
