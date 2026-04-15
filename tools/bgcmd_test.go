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
