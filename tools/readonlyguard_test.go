package tools

import (
	"context"
	"fmt"
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

func TestInitToolLoopEnforcersReadOnly(t *testing.T) {
	t.Parallel()

	t.Run("readonly mode creates no enforcers", func(t *testing.T) {
		t.Parallel()
		ctx, ee, pe := initToolLoopEnforcers(InitReadOnlyMode(context.Background()), 4)
		if ee != nil || pe != nil {
			t.Fatalf("readonly mode must not create enforcers, got edit=%v phase=%v", ee, pe)
		}
		if GetEditEnforcer(ctx) != nil || GetPhaseEnforcer(ctx) != nil {
			t.Fatal("readonly mode must not attach enforcers to ctx")
		}
		if shouldBlockReadTools(ctx) {
			t.Fatal("readonly mode must never block reads")
		}
	})

	t.Run("edit-capable mode keeps both enforcers", func(t *testing.T) {
		t.Parallel()
		ctx, ee, pe := initToolLoopEnforcers(context.Background(), 4)
		if ee == nil || pe == nil {
			t.Fatalf("edit mode must create enforcers, got edit=%v phase=%v", ee, pe)
		}
		if GetEditEnforcer(ctx) != ee || GetPhaseEnforcer(ctx) != pe {
			t.Fatal("edit mode must attach enforcers to ctx")
		}
	})
}

// readOnlyLoopResponses scripts n iterations of read-like Bash calls that do
// not populate the read cache (so discovery progress never resets the enforcers) followed by a
// final answer.
func readOnlyLoopResponses(n int) []*llms.ContentResponse {
	var out []*llms.ContentResponse
	for i := 0; i < n; i++ {
		out = append(out, &llms.ContentResponse{Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID:   fmt.Sprintf("c%d", i),
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "Bash",
					Arguments: fmt.Sprintf(`{"command":"ls nomatch%d"}`, i),
				},
			}},
		}}})
	}
	return out
}

func runReadOnlyLoop(t *testing.T, ctx context.Context, iters, editDeadline int) (*captureLLM, error, bool) {
	t.Helper()
	dir := t.TempDir()
	handlers, _ := BuildHandlers(dir, nil, &executor.LocalExecutor{WorkingDir: dir})
	llm := &captureLLM{responses: readOnlyLoopResponses(iters)}
	_, _, err, done := toolLoop(ctx, llm, buildInitialMessages("sys", "user", false), handlers, iters+5, RunWithToolsConfig{EditDeadline: editDeadline}, nil)
	return llm, err, done
}

func capturedText(llm *captureLLM) string {
	var sb strings.Builder
	for _, msgs := range llm.captured {
		for _, msg := range msgs {
			for _, part := range msg.Parts {
				switch p := part.(type) {
				case llms.TextContent:
					sb.WriteString(p.Text)
					sb.WriteString("\n")
				case llms.ToolCallResponse:
					sb.WriteString(p.Content)
					sb.WriteString("\n")
				}
			}
		}
	}
	return sb.String()
}

// TestToolLoopReadOnlyNeverNudgedOrBlocked is the regression test for readonly
// agents stalling: they cannot edit, so many read-only iterations must produce
// no "start editing" nudge, no read blocking, and no edit-deadline stop.
func TestToolLoopReadOnlyNeverNudgedOrBlocked(t *testing.T) {
	t.Parallel()
	llm, err, done := runReadOnlyLoop(t, InitReadOnlyMode(context.Background()), 12, 4)
	if err != nil || !done {
		t.Fatalf("readonly loop must run to completion, err=%v done=%v", err, done)
	}
	text := capturedText(llm)
	for _, marker := range []string{"[PROGRESS CHECK]", "[URGENT", "[FINAL WARNING]", "READ BLOCKED"} {
		if strings.Contains(text, marker) {
			t.Fatalf("readonly agent must not see %q; messages:\n%s", marker, text)
		}
	}
}

// TestToolLoopEditModeStillNudgesAndBlocks pins the unchanged behavior for
// edit-capable agents, and proves the readonly test above can fail.
func TestToolLoopEditModeStillNudgesAndBlocks(t *testing.T) {
	t.Parallel()
	llm, _, _ := runReadOnlyLoop(t, context.Background(), 12, 0)
	text := capturedText(llm)
	if !strings.Contains(text, "[PROGRESS CHECK]") || !strings.Contains(text, "[URGENT") {
		t.Fatalf("edit-capable agent must still receive escalating nudges; messages:\n%s", text)
	}
	if !strings.Contains(text, "BASH READ BLOCKED") {
		t.Fatalf("edit-capable agent must still have reads blocked after ignored nudges; messages:\n%s", text)
	}
}
