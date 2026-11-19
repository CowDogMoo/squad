package agenticcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecodeLiveEventRetryFailure checks that a line whose message has an
// unexpected shape and whose other fields are also malformed is reported as
// undecodable rather than half-decoded.
func TestDecodeLiveEventRetryFailure(t *testing.T) {
	t.Parallel()
	if _, err := decodeLiveEvent([]byte(`{"type":"user","message":"text","session_id":5}`)); err == nil {
		t.Fatal("decodeLiveEvent accepted a numeric session_id")
	}
}

// TestStreamReaderBlocksAndBlankLines feeds a stream with a blank line, a
// thinking block and a user event that mixes text and tool_result blocks.
func TestStreamReaderBlocksAndBlankLines(t *testing.T) {
	t.Parallel()
	obs := &recordingObserver{}
	sr := &streamReader{obs: obs}
	input := strings.Join([]string{
		"",
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"thinking","thinking":"weigh the options"},{"type":"text","text":""},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
		"   ",
		`{"type":"user","message":{"content":[{"type":"text","text":"note"},{"type":"tool_result","tool_use_id":"t1","content":"a.txt"}]}}`,
		`{"type":"result","is_error":false,"num_turns":1,"result":"done","usage":{"input_tokens":1,"output_tokens":1}}`,
	}, "\n")
	if err := sr.read(strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	if sr.raw.Len() != 0 {
		t.Errorf("raw = %q, want blank lines ignored", sr.raw.String())
	}
	if len(obs.assistant) != 1 {
		t.Fatalf("assistant events = %d, want 1", len(obs.assistant))
	}
	msg := obs.assistant[0]
	if len(msg.Thinking) != 1 || msg.Thinking[0] != "weigh the options" || len(msg.Text) != 0 || len(msg.ToolUses) != 1 {
		t.Errorf("assistant message = %+v, want one thinking block, no empty text, one tool use", msg)
	}
	if len(obs.results) != 1 || obs.results[0].ToolUseID != "t1" || obs.results[0].Content != "a.txt" {
		t.Errorf("tool results = %+v, want only the tool_result block", obs.results)
	}
	if !sr.resultSeen || sr.res.Response != "done" {
		t.Errorf("result = %+v seen=%v", sr.res, sr.resultSeen)
	}
}

// TestRunStreamStartFailure checks that a claude binary that cannot be
// executed fails the run with the start error.
func TestRunStreamStartFailure(t *testing.T) {
	dir := t.TempDir()
	// Run's LookPath skips a file without exec permission, so pass the
	// path straight to runClaudeStream to reach cmd.Start.
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("not a program"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, _ := Lookup("claude-code")
	_, err := runClaudeStream(context.Background(), spec, path, Request{Provider: "claude-code", WorkDir: dir})
	if err == nil || !strings.Contains(err.Error(), "claude CLI failed") {
		t.Fatalf("err = %v, want a start failure", err)
	}
}

// TestRunStreamResultThenNonZeroExit keeps the result when the CLI exits
// non-zero after it has already printed a successful result event.
func TestRunStreamResultThenNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncat > /dev/null\n" +
		`printf '%s\n' '{"type":"result","is_error":false,"num_turns":2,"result":"finished","usage":{"input_tokens":4,"output_tokens":2}}'` + "\n" +
		"exit 3\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	res, err := Run(context.Background(), Request{Provider: "claude-code", UserPrompt: "x", WorkDir: dir})
	if err != nil {
		t.Fatalf("Run: %v, want the result kept despite the exit status", err)
	}
	if res.Response != "finished" || res.Turns != 2 || res.InputTokens != 4 {
		t.Errorf("result = %+v", res)
	}
}

// TestLiveDenialDefaultMessage checks that a deny with no message reaches the
// observer with the gate's default text.
func TestLiveDenialDefaultMessage(t *testing.T) {
	t.Parallel()
	obs := &recordingObserver{}
	s := &liveSession{
		ctx: context.Background(),
		canUseTool: func(context.Context, string, json.RawMessage, string) (Decision, error) {
			return Decision{}, nil
		},
		obs:   obs,
		stdin: nopWriteCloser{&bytes.Buffer{}},
	}
	line := `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Edit","input":{},"tool_use_id":"toolu_e"}}`
	if err := s.handle([]byte(line)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(obs.denied) != 1 || obs.denied[0] != "toolu_e: denied by squad sign-off gate" {
		t.Errorf("denied = %v", obs.denied)
	}
}
