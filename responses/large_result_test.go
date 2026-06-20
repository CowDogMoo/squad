package responses

import (
	"context"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/tools"
	"github.com/tmc/langchaingo/llms"
)

func TestRegisterLargeResultToolIsIdempotent(t *testing.T) {
	handlers := map[string]tools.Handler{}
	defs := []llms.Tool{}
	registerLargeResultTool(context.Background(), handlers, &defs)
	registerLargeResultTool(context.Background(), handlers, &defs)
	if _, ok := handlers[toolGetToolResult]; !ok {
		t.Fatalf("get_tool_result handler not registered")
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool def, got %d", len(defs))
	}
}

func TestGetToolResultPagesContent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	wd := t.TempDir()
	l, err := session.New(wd, "", "agent", "openai", "gpt-5", "")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	content := strings.Repeat("xY", 20000) // 40000 bytes
	id, err := l.StoreLargeResult(content)
	if err != nil {
		t.Fatalf("StoreLargeResult: %v", err)
	}

	ctx := session.WithLogger(context.Background(), l)

	// First page: no offset, default chunk.
	out, err := getToolResult(ctx, []byte(`{"result_id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("getToolResult: %v", err)
	}
	if !strings.HasPrefix(out, content[:100]) {
		t.Fatalf("first page does not start with stored content")
	}
	if !strings.Contains(out, "more bytes available") {
		t.Fatalf("expected pagination hint, got: %s", out[len(out)-200:])
	}

	// Tail page: read everything from the middle.
	out2, err := getToolResult(ctx, []byte(`{"result_id":"`+id+`","offset":30000,"limit":16384}`))
	if err != nil {
		t.Fatalf("getToolResult page: %v", err)
	}
	if !strings.HasPrefix(out2, content[30000:30100]) {
		t.Fatalf("paged result does not start with content[30000:]")
	}
}

func TestGetToolResultRequiresLogger(t *testing.T) {
	_, err := getToolResult(context.Background(), []byte(`{"result_id":"abc"}`))
	if err == nil {
		t.Fatalf("expected error when no logger attached")
	}
}

func TestGetToolResultRejectsEmptyID(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	wd := t.TempDir()
	l, _ := session.New(wd, "", "a", "p", "m", "")
	t.Cleanup(func() { _ = l.Close() })
	ctx := session.WithLogger(context.Background(), l)

	if _, err := getToolResult(ctx, []byte(`{"result_id":""}`)); err == nil {
		t.Fatalf("expected error for empty result_id")
	}
	if _, err := getToolResult(ctx, []byte(`not json`)); err == nil {
		t.Fatalf("expected error for invalid json")
	}
}

func TestRegisterLargeResultToolNoopOnNilContainers(t *testing.T) {
	registerLargeResultTool(context.Background(), nil, nil) // no panic

	defs := []llms.Tool{}
	registerLargeResultTool(context.Background(), nil, &defs)
	if len(defs) != 0 {
		t.Fatalf("expected no defs when handlers is nil")
	}
}

func TestGetToolResultPropagatesUnknownID(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	wd := t.TempDir()
	l, _ := session.New(wd, "", "a", "p", "m", "")
	t.Cleanup(func() { _ = l.Close() })
	ctx := session.WithLogger(context.Background(), l)

	if _, err := getToolResult(ctx, []byte(`{"result_id":"deadbeef"}`)); err == nil {
		t.Fatalf("expected error for unknown id")
	}
}
