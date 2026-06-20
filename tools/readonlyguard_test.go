package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/tmc/langchaingo/llms"
)

func TestReadOnlyModeContext(t *testing.T) {
	t.Parallel()
	if IsReadOnlyMode(context.Background()) {
		t.Fatal("fresh context should not be readonly")
	}
	if !IsReadOnlyMode(InitReadOnlyMode(context.Background())) {
		t.Fatal("InitReadOnlyMode should mark the context readonly")
	}
}

func TestIsMutatingTool(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Edit", "MultiEdit", "Write"} {
		if !IsMutatingTool(name) {
			t.Errorf("%s should be a mutating tool", name)
		}
	}
	for _, name := range []string{"Read", "Glob", "Grep", "Bash", "Skill"} {
		if IsMutatingTool(name) {
			t.Errorf("%s should not be a mutating tool", name)
		}
	}
}

// TestReadOnlyModeDeniesWriteWithoutTouchingFile is the regression test for the
// composed-agent bug: a run in readonly mode must not be able to modify the
// working tree, even when the model emits a Write call.
func TestReadOnlyModeDeniesWriteWithoutTouchingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")

	handlers, _ := BuildHandlers(dir, nil, &executor.LocalExecutor{WorkingDir: dir})
	ctx := InitReadOnlyMode(context.Background())

	resp := executeToolCall(ctx, llms.ToolCall{
		ID:           "1",
		FunctionCall: &llms.FunctionCall{Name: "Write", Arguments: `{"path":"out.txt","content":"should not be written"}`},
	}, handlers)

	if !strings.Contains(resp.Content, "readonly mode") {
		t.Fatalf("expected readonly denial, got: %s", resp.Content)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("readonly mode must not create the file; stat err=%v", err)
	}
}

// TestReadOnlyModeAllowsReadOnlyTool confirms the gate only blocks mutating
// tools — a Read in readonly mode still runs.
func TestReadOnlyModeAllowsReadOnlyTool(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "in.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	handlers, _ := BuildHandlers(dir, nil, &executor.LocalExecutor{WorkingDir: dir})
	ctx := InitReadOnlyMode(context.Background())

	resp := executeToolCall(ctx, llms.ToolCall{
		ID:           "1",
		FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"in.txt"}`},
	}, handlers)

	if strings.Contains(resp.Content, "readonly mode") {
		t.Fatalf("Read must not be denied in readonly mode, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "hello") {
		t.Fatalf("expected file contents in Read response, got: %s", resp.Content)
	}
}
