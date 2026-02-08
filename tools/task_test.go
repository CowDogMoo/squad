package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/metrics"
)

func TestTaskTool(t *testing.T) {
	dir := t.TempDir()
	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			if TaskDepth(ctx) != 1 {
				t.Fatalf("expected depth 1, got %d", TaskDepth(ctx))
			}
			if workingDir != filepath.Join(dir, "sub") {
				t.Fatalf("unexpected working dir: %s", workingDir)
			}
			return "response", nil, nil
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

func TestTaskToolErrors(t *testing.T) {
	dir := t.TempDir()
	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			return "", nil, fmt.Errorf("boom")
		},
	}
	tool := taskTool(cfg)

	longResponse := strings.Repeat("a", maxToolOutput+10)

	tests := []struct {
		name         string
		ctx          context.Context
		payload      []byte
		config       TaskConfig
		wantContains string
		wantOutput   string
	}{
		{
			name:         "invalid json",
			ctx:          context.Background(),
			payload:      []byte("{"),
			wantContains: "invalid Task args",
		},
		{
			name:         "missing agent",
			ctx:          context.Background(),
			payload:      []byte(`{"prompt":"hi"}`),
			wantContains: "agent is required",
		},
		{
			name:         "missing prompt",
			ctx:          context.Background(),
			payload:      []byte(`{"agent":"child"}`),
			wantContains: "prompt is required",
		},
		{
			name:         "depth exceeded",
			ctx:          WithTaskDepth(context.Background(), MaxTaskDepth),
			payload:      []byte(`{"agent":"child","prompt":"hi"}`),
			wantContains: "maximum task depth",
		},
		{
			name:         "invalid working dir",
			ctx:          context.Background(),
			payload:      []byte(`{"agent":"child","prompt":"hi","working_dir":"../oops"}`),
			wantContains: "invalid working_dir",
		},
		{
			name:         "child error",
			ctx:          context.Background(),
			payload:      []byte(`{"agent":"child","prompt":"hi"}`),
			wantContains: "child agent \"child\" failed",
		},
		{
			name:    "truncate response",
			ctx:     context.Background(),
			payload: []byte(`{"agent":"child","prompt":"hi"}`),
			config: TaskConfig{
				AgentsDir:  "agents",
				WorkingDir: dir,
				CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
					return longResponse, nil, nil
				},
			},
			wantOutput: "...output truncated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localTool := tool
			if tt.config.CallModel != nil {
				localTool = taskTool(tt.config)
			}
			output, err := localTool(tt.ctx, tt.payload)
			if tt.wantContains != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(output, tt.wantOutput) {
				t.Fatalf("output = %q, want containing %q", output, tt.wantOutput)
			}
		})
	}
}

func TestTaskToolDepthLimit(t *testing.T) {
	cfg := TaskConfig{WorkingDir: t.TempDir(), CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
		return "", nil, nil
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
