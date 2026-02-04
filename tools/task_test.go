package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestTaskTool(t *testing.T) {
	dir := t.TempDir()
	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, error) {
			if TaskDepth(ctx) != 1 {
				t.Fatalf("expected depth 1, got %d", TaskDepth(ctx))
			}
			if workingDir != filepath.Join(dir, "sub") {
				t.Fatalf("unexpected working dir: %s", workingDir)
			}
			return "response", nil
		},
	}

	tool := taskTool(cfg)
	payload, _ := json.Marshal(map[string]string{
		"agent":       "child",
		"prompt":      "do work",
		"working_dir": "sub",
	})

	out, err := tool(context.Background(), payload)
	if err != nil {
		t.Fatalf("taskTool: %v", err)
	}
	if out != "response" {
		t.Fatalf("unexpected response: %s", out)
	}
}

func TestTaskToolDepthLimit(t *testing.T) {
	cfg := TaskConfig{WorkingDir: t.TempDir(), CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, error) {
		return "", nil
	}}
	tool := taskTool(cfg)
	payload, _ := json.Marshal(map[string]string{
		"agent":  "child",
		"prompt": "do work",
	})

	ctx := WithTaskDepth(context.Background(), MaxTaskDepth)
	if _, err := tool(ctx, payload); err == nil {
		t.Fatalf("expected depth limit error")
	}
}
