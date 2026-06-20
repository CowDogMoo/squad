package tools

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/session"
	"github.com/tmc/langchaingo/llms"
)

// sampleConversation returns a representative slice covering every message
// shape the tool loop produces: system, human, an assistant turn with a tool
// call plus trailing text, the tool result, and a final assistant answer.
func sampleConversation() []llms.MessageContent {
	return []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: "you are a helper"}}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "list the files"}}},
		{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{
			llms.ToolCall{ID: "call-1", Type: "function", FunctionCall: &llms.FunctionCall{Name: "Bash", Arguments: `{"command":"ls"}`}},
			llms.TextContent{Text: "running ls"},
		}},
		{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "call-1", Name: "Bash", Content: "a.go\nb.go"},
		}},
		{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextContent{Text: "there are two files"}}},
	}
}

func TestSaveLoadTranscriptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveTranscript(dir, sampleConversation()); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}

	got, err := LoadTranscript(dir)
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}

	// The system message is intentionally dropped; everything after it must
	// round-trip exactly so a stateless provider accepts the replayed history.
	want := sampleConversation()[1:]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestSaveTranscriptDropsSystemAndSkipsWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	systemOnly := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: "sys"}}},
	}
	if err := SaveTranscript(dir, systemOnly); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, TranscriptFile)); !os.IsNotExist(err) {
		t.Fatalf("expected no transcript file for system-only conversation, stat err=%v", err)
	}
}

func TestLoadTranscriptMissingFileIsNotError(t *testing.T) {
	got, err := LoadTranscript(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error for missing transcript, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil messages for missing transcript, got %#v", got)
	}
}

func TestEncodePartsUnwrapsCacheControl(t *testing.T) {
	wrapped := llms.WithCacheControl(llms.TextContent{Text: "cached"}, &llms.CacheControl{Type: "ephemeral"})
	parts := encodeParts([]llms.ContentPart{wrapped})
	if len(parts) != 1 || parts[0].Type != partKindText || parts[0].Text != "cached" {
		t.Fatalf("cache-control wrapper not unwrapped to text: %#v", parts)
	}
}

func TestSplicePriorMessagesOrdering(t *testing.T) {
	initial := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: "sys"}}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "new question"}}},
	}
	prior := []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "old question"}}},
		{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextContent{Text: "old answer"}}},
	}

	got := splicePriorMessages(initial, prior)

	wantRoles := []llms.ChatMessageType{
		llms.ChatMessageTypeSystem,
		llms.ChatMessageTypeHuman, // old question
		llms.ChatMessageTypeAI,    // old answer
		llms.ChatMessageTypeHuman, // new question (must stay last)
	}
	if len(got) != len(wantRoles) {
		t.Fatalf("spliced length = %d, want %d", len(got), len(wantRoles))
	}
	for i, role := range wantRoles {
		if got[i].Role != role {
			t.Fatalf("position %d role = %q, want %q", i, got[i].Role, role)
		}
	}
	last := got[len(got)-1]
	if parts := encodeParts(last.Parts); len(parts) != 1 || parts[0].Text != "new question" {
		t.Fatalf("new user prompt must remain the final message, got %#v", last)
	}
}

func TestSplicePriorMessagesEmptyPriorReturnsInitial(t *testing.T) {
	initial := []llms.MessageContent{{Role: llms.ChatMessageTypeHuman}}
	if got := splicePriorMessages(initial, nil); !reflect.DeepEqual(got, initial) {
		t.Fatalf("empty prior should return initial unchanged, got %#v", got)
	}
}

func TestPersistTranscriptWritesForActiveSession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	logger, err := session.New(dir, "", "agent", "anthropic", "claude", "prompt")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	defer func() { _ = logger.Close() }()

	ctx := session.WithLogger(context.Background(), logger)
	persistTranscript(ctx, sampleConversation())

	got, err := LoadTranscript(logger.Dir())
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	if len(got) != len(sampleConversation())-1 {
		t.Fatalf("persisted %d messages, want %d", len(got), len(sampleConversation())-1)
	}
}

func TestPersistTranscriptNoLoggerIsNoop(t *testing.T) {
	// Must not panic when no session logger is attached to the context.
	persistTranscript(context.Background(), sampleConversation())
}

func TestPersistTranscriptSwallowsWriteFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger, err := session.New(t.TempDir(), "", "agent", "anthropic", "claude", "prompt")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	defer func() { _ = logger.Close() }()
	// Removing the session dir makes the atomic write fail; persistTranscript
	// must log and return, never panic or propagate.
	if err := os.RemoveAll(logger.Dir()); err != nil {
		t.Fatalf("rm session dir: %v", err)
	}
	persistTranscript(session.WithLogger(context.Background(), logger), sampleConversation())
}

func TestSaveTranscriptEmptyDirErrors(t *testing.T) {
	t.Parallel()
	if err := SaveTranscript("", sampleConversation()); err == nil {
		t.Fatal("expected an error for an empty session dir")
	}
}

func TestLoadTranscriptCorruptFileErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, TranscriptFile), []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed corrupt transcript: %v", err)
	}
	if _, err := LoadTranscript(dir); err == nil {
		t.Fatal("expected a parse error for a corrupt transcript")
	}
}

func TestLoadTranscriptReadErrorOnDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A directory at the transcript path makes os.ReadFile fail with a
	// non-NotExist error, which must surface rather than be swallowed.
	if err := os.Mkdir(filepath.Join(dir, TranscriptFile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := LoadTranscript(dir); err == nil {
		t.Fatal("expected a read error when the transcript path is a directory")
	}
}

func TestSaveTranscriptSkipsMessagesWithNoEncodableParts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	msgs := []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: nil}, // nothing to encode -> skipped
		{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextContent{Text: "kept"}}},
	}
	if err := SaveTranscript(dir, msgs); err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}
	got, err := LoadTranscript(dir)
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the empty-parts message to be skipped, got %d messages", len(got))
	}
}

func TestSplicePriorMessagesEmptyInitialReturnsPrior(t *testing.T) {
	t.Parallel()
	prior := []llms.MessageContent{{Role: llms.ChatMessageTypeHuman}}
	if got := splicePriorMessages(nil, prior); !reflect.DeepEqual(got, prior) {
		t.Fatalf("empty initial should return prior unchanged, got %#v", got)
	}
}

// TestRunWithToolsReplaysAndPersists exercises the resume seam end-to-end with
// a fake LLM: prior messages are spliced in, and the conversation is persisted
// so a later --resume can reload it.
func TestRunWithToolsReplaysAndPersists(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	logger, err := session.New(dir, "", "agent", "anthropic", "claude", "prompt")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	defer func() { _ = logger.Close() }()
	ctx := session.WithLogger(context.Background(), logger)

	llm := &fakeLLM{responses: []*llms.ContentResponse{
		{Choices: []*llms.ContentChoice{{Content: "done"}}},
	}}
	prior := []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "earlier"}}},
		{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.TextContent{Text: "earlier answer"}}},
	}
	out, err := RunWithTools(ctx, llm, "sys", "new question", dir, RunWithToolsConfig{
		MaxIterations: 1,
		Executor:      &executor.LocalExecutor{WorkingDir: dir},
		PriorMessages: prior,
	})
	if err != nil {
		t.Fatalf("RunWithTools() error = %v", err)
	}
	if out != "done" {
		t.Fatalf("output = %q, want done", out)
	}

	saved, err := LoadTranscript(logger.Dir())
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	// Persisted = prior turns + the new user prompt (system is dropped).
	if len(saved) < len(prior)+1 {
		t.Fatalf("expected >= %d persisted messages, got %d", len(prior)+1, len(saved))
	}
}
