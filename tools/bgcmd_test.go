package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/executor"
)

func TestBgCommandRegistry_SpawnAndCollect(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: dir}
	registry := NewBgCommandRegistry()

	id := registry.Spawn(context.Background(), ex, "echo hello")
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	bg, ok := registry.Get(id)
	if !ok {
		t.Fatal("expected to find command")
	}

	// Wait for completion.
	<-bg.Done
	if !bg.IsDone() {
		t.Fatal("expected command to be done")
	}
	if bg.Err != nil {
		t.Fatalf("unexpected error: %v", bg.Err)
	}
	if !strings.Contains(bg.Output, "hello") {
		t.Fatalf("expected 'hello' in output, got: %q", bg.Output)
	}
}

func TestBgCommandRegistry_UnknownID(t *testing.T) {
	t.Parallel()
	registry := NewBgCommandRegistry()
	_, ok := registry.Get("nonexistent")
	if ok {
		t.Fatal("expected false for unknown ID")
	}
}

func TestBashBackgroundTool_Integration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: dir}
	ctx := InitBgCommandRegistry(context.Background())

	// Start background command.
	bgTool := bashBackgroundTool(ex)
	args, _ := json.Marshal(map[string]string{"command": "echo background_test"})
	result, err := bgTool(ctx, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Command started in background") {
		t.Fatalf("expected background message, got: %s", result)
	}

	// Extract the command ID.
	// Format: "Command started in background. ID: cmd-1\n..."
	parts := strings.Split(result, "ID: ")
	if len(parts) < 2 {
		t.Fatalf("could not extract command ID from: %s", result)
	}
	cmdID := strings.Split(parts[1], "\n")[0]

	// Wait a bit for the command to complete.
	time.Sleep(500 * time.Millisecond)

	// Collect output.
	outTool := bashOutputTool()
	outArgs, _ := json.Marshal(map[string]any{"command_id": cmdID, "wait": true})
	output, err := outTool(ctx, outArgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "completed") {
		t.Fatalf("expected completed status, got: %s", output)
	}
	if !strings.Contains(output, "background_test") {
		t.Fatalf("expected output content, got: %s", output)
	}
}

func TestBashBackgroundTool_BlockedCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: dir}
	ctx := InitBgCommandRegistry(context.Background())

	bgTool := bashBackgroundTool(ex)
	args, _ := json.Marshal(map[string]string{"command": "sudo rm -rf /"})
	_, err := bgTool(ctx, args)
	if err == nil {
		t.Fatal("expected error for blocked command")
	}
}

func TestBashBackgroundTool_NoRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: dir}

	bgTool := bashBackgroundTool(ex)
	args, _ := json.Marshal(map[string]string{"command": "echo test"})
	_, err := bgTool(context.Background(), args)
	if err == nil {
		t.Fatal("expected error when no registry in context")
	}
}

func TestBashOutputTool_UnknownID(t *testing.T) {
	t.Parallel()
	ctx := InitBgCommandRegistry(context.Background())
	outTool := bashOutputTool()
	args, _ := json.Marshal(map[string]string{"command_id": "nonexistent"})
	_, err := outTool(ctx, args)
	if err == nil {
		t.Fatal("expected error for unknown command ID")
	}
}

func TestBgCommandRegistryContext(t *testing.T) {
	t.Parallel()
	ctx := InitBgCommandRegistry(context.Background())
	r := GetBgCommandRegistry(ctx)
	if r == nil {
		t.Fatal("expected registry from context")
	}
	if GetBgCommandRegistry(context.Background()) != nil {
		t.Fatal("expected nil from bare context")
	}
}

func TestBashBackgroundTool_InvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: dir}
	ctx := InitBgCommandRegistry(context.Background())

	bgTool := bashBackgroundTool(ex)
	_, err := bgTool(ctx, []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBashBackgroundTool_EmptyCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: dir}
	ctx := InitBgCommandRegistry(context.Background())

	bgTool := bashBackgroundTool(ex)
	args, _ := json.Marshal(map[string]string{"command": "   "})
	_, err := bgTool(ctx, args)
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestBashOutputTool_InvalidJSON(t *testing.T) {
	t.Parallel()
	ctx := InitBgCommandRegistry(context.Background())
	outTool := bashOutputTool()
	_, err := outTool(ctx, []byte(`{bad`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBashOutputTool_EmptyCommandID(t *testing.T) {
	t.Parallel()
	ctx := InitBgCommandRegistry(context.Background())
	outTool := bashOutputTool()
	args, _ := json.Marshal(map[string]string{"command_id": ""})
	_, err := outTool(ctx, args)
	if err == nil {
		t.Fatal("expected error for empty command_id")
	}
}

func TestBashOutputTool_NoRegistry(t *testing.T) {
	t.Parallel()
	outTool := bashOutputTool()
	args, _ := json.Marshal(map[string]string{"command_id": "cmd-1"})
	_, err := outTool(context.Background(), args)
	if err == nil {
		t.Fatal("expected error when no registry in context")
	}
}

func TestBashOutputTool_FailedCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: dir}
	ctx := InitBgCommandRegistry(context.Background())
	registry := GetBgCommandRegistry(ctx)

	id := registry.Spawn(ctx, ex, "exit 1")
	bg, _ := registry.Get(id)
	<-bg.Done

	outTool := bashOutputTool()
	args, _ := json.Marshal(map[string]string{"command_id": id})
	result, err := outTool(ctx, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "failed") {
		t.Fatalf("expected 'failed' in result, got: %s", result)
	}
}

func TestBashOutputTool_StillRunning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: dir}
	ctx := InitBgCommandRegistry(context.Background())
	registry := GetBgCommandRegistry(ctx)

	// Start a long-running command.
	id := registry.Spawn(ctx, ex, "sleep 30")

	outTool := bashOutputTool()
	args, _ := json.Marshal(map[string]any{"command_id": id, "wait": false})
	result, err := outTool(ctx, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "running") {
		t.Fatalf("expected 'running' in result, got: %s", result)
	}
}

func TestBgCommand_IsDoneBeforeCompletion(t *testing.T) {
	t.Parallel()
	bg := &BgCommand{Done: make(chan struct{})}
	if bg.IsDone() {
		t.Fatal("expected IsDone=false before closing channel")
	}
	close(bg.Done)
	if !bg.IsDone() {
		t.Fatal("expected IsDone=true after closing channel")
	}
}

func TestTrimCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, want string
	}{
		{"  hello  ", "hello"},
		{"\t\ntest\r\n", "test"},
		{"", ""},
		{"nospace", "nospace"},
	}
	for _, tt := range tests {
		if got := trimCommand(tt.input); got != tt.want {
			t.Errorf("trimCommand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
