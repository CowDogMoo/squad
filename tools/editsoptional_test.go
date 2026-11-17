package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/tmc/langchaingo/llms"
)

func TestEditsOptionalContext(t *testing.T) {
	t.Parallel()
	bg := context.Background()
	if EditsOptional(bg) || !EditsExpected(bg) {
		t.Fatal("a plain context must expect edits")
	}
	if !EditsOptional(InitEditsOptional(bg)) || EditsExpected(InitEditsOptional(bg)) {
		t.Fatal("InitEditsOptional must mark edits optional and not expected")
	}
	if EditsExpected(InitReadOnlyMode(bg)) {
		t.Fatal("readonly mode must not expect edits")
	}
}

func TestInitToolLoopEnforcersEditsOptional(t *testing.T) {
	t.Parallel()
	ctx, ee, pe := initToolLoopEnforcers(InitEditsOptional(context.Background()), 4)
	if ee != nil || pe != nil {
		t.Fatalf("edits-optional run must not create enforcers, got edit=%v phase=%v", ee, pe)
	}
	if GetEditEnforcer(ctx) != nil || GetPhaseEnforcer(ctx) != nil {
		t.Fatal("edits-optional run must not attach enforcers to ctx")
	}
}

// TestToolLoopEditsOptionalNeverNudgedOrBlocked is the regression test for a
// probing agent (such as a web-app mapper) run with
// --require-actionable=false: it only reads, so many read-only iterations
// must bring no "start editing" nudge, no read blocking, and no edit-deadline
// stop. TestToolLoopEditModeStillNudgesAndBlocks proves the same script trips
// all three when edits are required.
func TestToolLoopEditsOptionalNeverNudgedOrBlocked(t *testing.T) {
	t.Parallel()
	llm, err, done := runReadOnlyLoop(t, InitEditsOptional(context.Background()), 12, 4)
	if errors.Is(err, ErrEditDeadlineReached) {
		t.Fatal("edits-optional run must not stop at the edit deadline")
	}
	if err != nil || !done {
		t.Fatalf("edits-optional loop must run to completion, err=%v done=%v", err, done)
	}
	text := capturedText(llm)
	for _, marker := range []string{"[PROGRESS CHECK]", "[URGENT", "[FINAL WARNING]", "READ BLOCKED", "[EDIT DEADLINE REACHED]"} {
		if strings.Contains(text, marker) {
			t.Fatalf("edits-optional agent must not see %q; messages:\n%s", marker, text)
		}
	}
}

// TestToolLoopEditsOptionalIgnoresInheritedEnforcers covers a Task child of an
// edit-capable parent inside an edits-optional run: enforcers the parent
// already tripped must not block the child's reads.
func TestToolLoopEditsOptionalIgnoresInheritedEnforcers(t *testing.T) {
	t.Parallel()
	for name, parentCtx := range inheritedEnforcerContexts(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handlers := seededReadHandlers(t)
			llm := &captureLLM{responses: []*llms.ContentResponse{
				{Choices: []*llms.ContentChoice{{ToolCalls: []llms.ToolCall{readCall}}}},
				{Choices: []*llms.ContentChoice{{ToolCalls: []llms.ToolCall{lsCall}}}},
			}}
			_, _, err, done := toolLoop(InitEditsOptional(parentCtx), llm, buildInitialMessages("sys", "user", false), handlers, 5, RunWithToolsConfig{}, nil)
			if err != nil || !done {
				t.Fatalf("loop must run to completion, err=%v done=%v", err, done)
			}
			text := capturedText(llm)
			if strings.Contains(text, "READ BLOCKED") {
				t.Fatalf("reads must not be blocked by an inherited enforcer; messages:\n%s", text)
			}
			if !strings.Contains(text, "file body") {
				t.Fatalf("Read must return the file; messages:\n%s", text)
			}
		})
	}
}

// TestEditsOptionalStillAllowsEdits checks that optional is not readonly: the
// edit tools still run.
func TestEditsOptionalStillAllowsEdits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	handlers, _ := BuildHandlers(dir, nil, &executor.LocalExecutor{WorkingDir: dir})
	ctx := InitEditsOptional(InitEdits(context.Background()))
	call := llms.ToolCall{ID: "w", Type: "function", FunctionCall: &llms.FunctionCall{Name: "Write", Arguments: `{"path":"out.txt","content":"data"}`}}
	resp := executeToolCall(ctx, call, handlers)
	if strings.HasPrefix(resp.Content, "error:") {
		t.Fatalf("Write must succeed in an edits-optional run, got %q", resp.Content)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || string(got) != "data" {
		t.Fatalf("Write must create the file, got %q err=%v", got, err)
	}
}

func TestBudgetWarningMessageNoEdits(t *testing.T) {
	t.Parallel()
	if msg := budgetWarningMessage(25, 1, true); strings.Contains(msg, "Do not read more files") {
		t.Fatalf("no-edit 25%% warning must carry no edit pressure, got %q", msg)
	}
	if msg := budgetWarningMessage(25, 1, false); msg == budgetWarningMessage(25, 1, true) {
		t.Fatal("edit-capable 25% warning must differ from the no-edit one")
	}
}
