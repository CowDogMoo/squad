package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestBackgroundTaskBasic(t *testing.T) {
	dir := t.TempDir()
	registry := NewBackgroundTaskRegistry(0)
	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		Registry:   registry,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			return "background result", nil, nil
		},
	}

	tool := taskTool(cfg)
	resultTool := taskResultTool(cfg)

	// Spawn background task
	payload, _ := json.Marshal(map[string]any{
		"agent":      "child",
		"prompt":     "do work",
		"background": true,
	})

	out, err := tool(context.Background(), payload)
	if err != nil {
		t.Fatalf("taskTool background: %v", err)
	}
	if !strings.Contains(out, "bg-1") {
		t.Fatalf("expected task ID in output, got: %s", out)
	}

	// Collect result
	resultPayload, _ := json.Marshal(map[string]string{
		"task_id": "bg-1",
	})

	result, err := resultTool(context.Background(), resultPayload)
	if err != nil {
		t.Fatalf("taskResultTool: %v", err)
	}
	if result != "background result" {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestBackgroundTaskSemaphore(t *testing.T) {
	dir := t.TempDir()
	registry := NewBackgroundTaskRegistry(0)

	started := make(chan struct{}, DefaultConcurrentTasks+1)
	block := make(chan struct{})

	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		Registry:   registry,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			started <- struct{}{}
			<-block
			return "done", nil, nil
		},
	}

	tool := taskTool(cfg)

	// Spawn DefaultConcurrentTasks + 1 tasks
	for i := 0; i < DefaultConcurrentTasks+1; i++ {
		payload, _ := json.Marshal(map[string]any{
			"agent":      "child",
			"prompt":     fmt.Sprintf("task %d", i),
			"background": true,
		})
		_, err := tool(context.Background(), payload)
		if err != nil {
			t.Fatalf("taskTool: %v", err)
		}
	}

	// Wait for DefaultConcurrentTasks to start
	for i := 0; i < DefaultConcurrentTasks; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("expected %d tasks to start, only %d did", DefaultConcurrentTasks, i)
		}
	}

	// Verify the 5th task hasn't started yet (semaphore blocking)
	select {
	case <-started:
		t.Fatalf("5th task should be blocked by semaphore")
	case <-time.After(50 * time.Millisecond):
		// Expected: 5th task is waiting
	}

	// Unblock one task
	block <- struct{}{}

	// Now the 5th task should start
	select {
	case <-started:
		// Expected
	case <-time.After(time.Second):
		t.Fatalf("5th task should have started after one completed")
	}

	// Unblock remaining tasks
	close(block)
}

func TestBackgroundTaskCancel(t *testing.T) {
	dir := t.TempDir()
	registry := NewBackgroundTaskRegistry(0)

	started := make(chan struct{})

	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		Registry:   registry,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			close(started)
			<-ctx.Done()
			return "", nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	tool := taskTool(cfg)
	payload, _ := json.Marshal(map[string]any{
		"agent":      "child",
		"prompt":     "do work",
		"background": true,
	})

	out, err := tool(ctx, payload)
	if err != nil {
		t.Fatalf("taskTool: %v", err)
	}
	if !strings.Contains(out, "bg-1") {
		t.Fatalf("expected task ID")
	}

	// Wait for task to start
	<-started

	// Cancel context
	cancel()

	// Collect result - should have context error
	result, ok := registry.GetResult("bg-1", true)
	if !ok {
		t.Fatalf("expected to find task result")
	}
	if result.Err == nil {
		t.Fatalf("expected error from cancelled task")
	}
}

func TestBackgroundTaskPanic(t *testing.T) {
	dir := t.TempDir()
	registry := NewBackgroundTaskRegistry(0)

	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		Registry:   registry,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			panic("test panic")
		},
	}

	tool := taskTool(cfg)
	payload, _ := json.Marshal(map[string]any{
		"agent":      "child",
		"prompt":     "do work",
		"background": true,
	})

	_, err := tool(context.Background(), payload)
	if err != nil {
		t.Fatalf("taskTool: %v", err)
	}

	resultTool := taskResultTool(cfg)
	resultPayload, _ := json.Marshal(map[string]string{
		"task_id": "bg-1",
	})

	_, err = resultTool(context.Background(), resultPayload)
	if err == nil {
		t.Fatalf("expected error from panicked task")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("expected panic error, got: %v", err)
	}
}

func TestTaskResultInvalidID(t *testing.T) {
	dir := t.TempDir()
	registry := NewBackgroundTaskRegistry(0)
	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		Registry:   registry,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			return "", nil, nil
		},
	}

	resultTool := taskResultTool(cfg)

	tests := []struct {
		name         string
		payload      []byte
		wantContains string
	}{
		{
			name:         "invalid json",
			payload:      []byte("{"),
			wantContains: "invalid TaskResult args",
		},
		{
			name:         "missing task_id",
			payload:      []byte(`{}`),
			wantContains: "task_id is required",
		},
		{
			name:         "unknown task_id",
			payload:      []byte(`{"task_id":"bg-999"}`),
			wantContains: "unknown task ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resultTool(context.Background(), tt.payload)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantContains)
			}
		})
	}
}

func TestBackgroundTaskNoRegistry(t *testing.T) {
	dir := t.TempDir()
	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		Registry:   nil, // No registry
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			return "", nil, nil
		},
	}

	tool := taskTool(cfg)
	payload, _ := json.Marshal(map[string]any{
		"agent":      "child",
		"prompt":     "do work",
		"background": true,
	})

	_, err := tool(context.Background(), payload)
	if err == nil {
		t.Fatalf("expected error when registry is nil")
	}
	if !strings.Contains(err.Error(), "registry not initialized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTaskResultNoRegistry(t *testing.T) {
	t.Parallel()
	cfg := TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: t.TempDir(),
		Registry:   nil,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			return "", nil, nil
		},
	}

	tool := taskResultTool(cfg)
	payload, _ := json.Marshal(map[string]string{"task_id": "bg-1"})

	_, err := tool(context.Background(), payload)
	if err == nil {
		t.Fatalf("expected error when registry is nil")
	}
	if !strings.Contains(err.Error(), "registry not initialized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTaskToolWithMetricsTracking(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parentMetrics := metrics.New("openai", "gpt-4o")
	childMetrics := metrics.New("anthropic", "claude-3")
	childMetrics.AddTokens(500, 250)

	cfg := TaskConfig{
		AgentsDir:     "agents",
		WorkingDir:    dir,
		ParentMetrics: parentMetrics,
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			return "child response", childMetrics, nil
		},
	}

	tool := taskTool(cfg)
	payload, _ := json.Marshal(map[string]string{
		"agent":  "sub-agent",
		"prompt": "analyze code",
	})

	out, err := tool(context.Background(), payload)
	if err != nil {
		t.Fatalf("taskTool: %v", err)
	}
	if out != "child response" {
		t.Fatalf("output = %q, want %q", out, "child response")
	}

	// Verify child metrics were added to parent
	if len(parentMetrics.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(parentMetrics.Children))
	}
	child := parentMetrics.Children[0]
	if child.Agent != "sub-agent" {
		t.Fatalf("child agent = %q, want sub-agent", child.Agent)
	}
	if child.InputTokens != 500 || child.OutputTokens != 250 {
		t.Fatalf("child tokens = %d/%d, want 500/250", child.InputTokens, child.OutputTokens)
	}
	if child.Model != "claude-3" || child.Provider != "anthropic" {
		t.Fatalf("child model/provider = %q/%q, want claude-3/anthropic", child.Model, child.Provider)
	}
}
