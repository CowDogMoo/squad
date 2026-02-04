package tools

import (
	"context"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

func TestBuildHandlersIncludesTask(t *testing.T) {
	cfg := &TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: t.TempDir(),
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, error) {
			return "", nil
		},
	}

	handlers, defs := BuildHandlers(cfg.WorkingDir, cfg)
	if _, ok := handlers["Task"]; !ok {
		t.Fatalf("expected Task handler")
	}
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Function.Name)
	}
	if len(names) == 0 {
		t.Fatalf("expected tool defs")
	}

	// Ensure handler map contains basic tool as well.
	if _, ok := handlers["Read"]; !ok {
		t.Fatalf("expected Read handler")
	}

	_ = llms.Tool{}
}
