package agenticcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordingObserver captures every Observer call in order.
type recordingObserver struct {
	sessionID, model string
	thinking         []int64
	assistant        []AssistantMessage
	results          []ToolResult
	denied           []string
	finished         int
}

func (o *recordingObserver) SessionStarted(id, model string) { o.sessionID, o.model = id, model }
func (o *recordingObserver) ThinkingTokens(n int64)          { o.thinking = append(o.thinking, n) }
func (o *recordingObserver) Assistant(m AssistantMessage)    { o.assistant = append(o.assistant, m) }
func (o *recordingObserver) ToolResults(r []ToolResult)      { o.results = append(o.results, r...) }
func (o *recordingObserver) ToolDenied(id, msg string)       { o.denied = append(o.denied, id+": "+msg) }
func (o *recordingObserver) Finished(time.Time)              { o.finished++ }

const printStreamFixture = "claude_print_stream.jsonl"

func readPrintStream(t *testing.T) *streamReader {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", printStreamFixture))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	sr := &streamReader{obs: &recordingObserver{}}
	if err := sr.read(f); err != nil {
		t.Fatalf("read: %v", err)
	}
	return sr
}

// TestStreamReaderResultMatchesEnvelope checks that the result read from the
// stream's terminal event is what the json envelope used to give callers.
func TestStreamReaderResultMatchesEnvelope(t *testing.T) {
	t.Parallel()
	sr := readPrintStream(t)
	if !sr.resultSeen || sr.resErr != nil {
		t.Fatalf("resultSeen=%v resErr=%v", sr.resultSeen, sr.resErr)
	}
	want := Result{
		Response:           "Notes hold alpha and beta; /missing does not exist.",
		InputTokens:        7,
		CacheReadTokens:    60309,
		CacheWriteTokens:   0,
		CacheWrite1hTokens: 7591,
		OutputTokens:       224,
		Turns:              3,
	}
	if sr.res != want {
		t.Errorf("result = %+v, want %+v", sr.res, want)
	}
	if sr.raw.Len() != 0 {
		t.Errorf("raw = %q, want no undecodable lines", sr.raw.String())
	}
}

func TestStreamReaderFeedsObserver(t *testing.T) {
	t.Parallel()
	sr := readPrintStream(t)
	obs := sr.obs.(*recordingObserver)
	if obs.sessionID != "11111111-2222-4333-8444-555555555555" || obs.model != "claude-haiku-5-5" {
		t.Errorf("session = %q model = %q", obs.sessionID, obs.model)
	}
	if len(obs.thinking) != 2 || obs.thinking[1] != 40 {
		t.Errorf("thinking = %v, want [12 40]", obs.thinking)
	}
	if len(obs.assistant) != 5 {
		t.Fatalf("assistant events = %d, want 5", len(obs.assistant))
	}
	first := obs.assistant[0]
	if first.ID != "msg_A" || len(first.Text) != 1 || !strings.HasPrefix(first.Text[0], "I'll read") {
		t.Errorf("first assistant = %+v", first)
	}
	if first.Usage.CacheReadInputTokens != 11615 || first.Time.IsZero() {
		t.Errorf("first usage/time = %+v %v", first.Usage, first.Time)
	}
	tool := obs.assistant[1].ToolUses
	if len(tool) != 1 || tool[0].ID != "toolu_cat" || tool[0].Name != "Bash" || !strings.Contains(string(tool[0].Input), "cat notes.txt") {
		t.Errorf("tool use = %+v", tool)
	}
	if last := obs.assistant[4]; last.StopReason != "end_turn" {
		t.Errorf("last stop reason = %q", last.StopReason)
	}
	if len(obs.results) != 3 {
		t.Fatalf("tool results = %d, want 3", len(obs.results))
	}
	if r := obs.results[0]; r.ToolUseID != "toolu_cat" || r.IsError || r.Content != "alpha\nbeta" || r.PermissionDecision != "accept" {
		t.Errorf("cat result = %+v", r)
	}
	if r := obs.results[1]; !r.IsError || r.Content != "Exit code 1\nls: /missing: No such file or directory" {
		t.Errorf("block-list result = %+v", r)
	}
	if r := obs.results[2]; r.PermissionDecision != "reject" {
		t.Errorf("denied result = %+v", r)
	}
	if obs.finished != 1 {
		t.Errorf("finished = %d, want 1", obs.finished)
	}
}

func TestStreamReaderWithoutObserver(t *testing.T) {
	t.Parallel()
	f, err := os.Open(filepath.Join("testdata", printStreamFixture))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sr := &streamReader{}
	if err := sr.read(f); err != nil || !sr.resultSeen {
		t.Fatalf("read err=%v resultSeen=%v", err, sr.resultSeen)
	}
}

func TestStreamReaderKeepsOnlyNonEventLinesRaw(t *testing.T) {
	t.Parallel()
	sr := &streamReader{}
	input := "plain text line\n" +
		`{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"assistant prose"}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"partial outp` + "\n" +
		`{"type":"result","is_error":true,"result":"boom"}`
	if err := sr.read(strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	if sr.raw.String() != "plain text line\n" {
		t.Errorf("raw = %q, want only the non-event line", sr.raw.String())
	}
	if !sr.resultSeen || sr.resErr == nil || !strings.Contains(sr.resErr.Error(), "boom") {
		t.Errorf("error result not surfaced: seen=%v err=%v", sr.resultSeen, sr.resErr)
	}

	bad := &streamReader{}
	if err := bad.read(strings.NewReader(`{"type":"result","num_turns":"three","result":"final prose"}`)); err != nil {
		t.Fatal(err)
	}
	if bad.raw.Len() != 0 || !bad.resultSeen || bad.resErr == nil ||
		!strings.HasPrefix(bad.resErr.Error(), "undecodable result event") || strings.Contains(bad.resErr.Error(), "final prose") {
		t.Errorf("undecodable result: raw=%q seen=%v err=%v", bad.raw.String(), bad.resultSeen, bad.resErr)
	}
}

func TestDecodeLiveEventStringContent(t *testing.T) {
	t.Parallel()
	ev, err := decodeLiveEvent([]byte(`{"type":"user","session_id":"s","message":{"role":"user","content":"hi"}}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.Type != "user" || ev.SessionID != "s" || ev.Message != nil {
		t.Errorf("event = %+v", ev)
	}
	if _, err := decodeLiveEvent([]byte(`not json`)); err == nil {
		t.Error("want an error for a non-JSON line")
	}
}

func TestToolResultText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, raw, want string
	}{
		{"empty", ``, ""},
		{"string", `"plain"`, "plain"},
		{"blocks", `[{"type":"text","text":"a"},{"type":"image"},{"type":"text","text":"b"}]`, "a\nb"},
		{"other", `{"x":1}`, `{"x":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolResultText([]byte(tt.raw)); got != tt.want {
				t.Errorf("toolResultText(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestEventTime(t *testing.T) {
	t.Parallel()
	got := eventTime("2026-11-18T15:51:33.874Z")
	if got.UnixMilli() != time.Date(2026, 11, 18, 15, 51, 33, 874e6, time.UTC).UnixMilli() {
		t.Errorf("eventTime = %v", got)
	}
	if eventTime("garbage").IsZero() || eventTime("").IsZero() {
		t.Error("eventTime must fall back to now")
	}
}

// TestRunStreamsToObserver runs a fake claude that prints the stream fixture
// and checks Run returns the result and feeds the observer.
func TestRunStreamsToObserver(t *testing.T) {
	dir := t.TempDir()
	fixture, err := filepath.Abs(filepath.Join("testdata", printStreamFixture))
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		`printf '%s\n' "$@" > "` + filepath.Join(dir, "args.txt") + `"` + "\n" +
		"cat > /dev/null\n" +
		`cat "` + fixture + `"` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	obs := &recordingObserver{}
	res, err := Run(context.Background(), Request{Provider: "claude-code", UserPrompt: "x", WorkDir: dir, Observer: obs})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Turns != 3 || res.OutputTokens != 224 || !strings.HasPrefix(res.Response, "Notes hold") {
		t.Errorf("result = %+v", res)
	}
	if len(obs.results) != 3 || obs.finished != 1 {
		t.Errorf("observer saw %d results, finished=%d", len(obs.results), obs.finished)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stream-json", "--verbose"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
}

// TestRunStreamErrorResult keeps the is_error handling of the json envelope.
func TestRunStreamErrorResult(t *testing.T) {
	dir := t.TempDir()
	writeFakeCLI(t, dir, "claude",
		`{"type":"system","subtype":"init","session_id":"s"}
{"type":"result","is_error":true,"num_turns":1,"result":"Credit balance is too low","usage":{"input_tokens":3,"output_tokens":1}}`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	res, err := Run(context.Background(), Request{Provider: "claude-code", UserPrompt: "x", WorkDir: dir})
	if err == nil || !strings.Contains(err.Error(), "claude run failed: Credit balance is too low") {
		t.Fatalf("err = %v", err)
	}
	if res.Turns != 1 || res.InputTokens != 3 {
		t.Errorf("result = %+v, want usage kept on error", res)
	}
}

// TestRunStreamFailureOmitsStreamContent checks a failed run's error carries
// the CLI's stderr, its non-event stdout lines and its error result, but none
// of the stream's assistant text, tool input or tool output.
func TestRunStreamFailureOmitsStreamContent(t *testing.T) {
	events := `{"type":"system","subtype":"init","session_id":"s"}
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Reading TEXT-fixture"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"cat INPUT-fixture"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"cat: OUTPUT-fixture: denied"}]}}
plain diagnostic line
`
	tests := []struct {
		name    string
		stdout  string
		wantErr string
		want    []string
	}{
		{
			name:    "no result event",
			stdout:  events,
			wantErr: "claude CLI failed: exit status 1",
			want:    []string{"stderr: auth expired", "stdout: plain diagnostic line"},
		},
		{
			name:    "error result",
			stdout:  events + `{"type":"result","is_error":true,"num_turns":1,"result":"Credit balance is too low","usage":{"input_tokens":3}}` + "\n",
			wantErr: "claude run failed: Credit balance is too low",
		},
		{
			name:    "killed mid-event",
			stdout:  events + `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"more OUTPUT-fixture bytes`,
			wantErr: "claude CLI failed: exit status 1",
			want:    []string{"stderr: auth expired", "stdout: plain diagnostic line"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "stdout.jsonl")
			if err := os.WriteFile(out, []byte(tt.stdout), 0o600); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\ncat > /dev/null\ncat \"" + out + "\"\necho 'auth expired' >&2\nexit 1\n"
			if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			obs := &recordingObserver{}
			_, err := Run(context.Background(), Request{Provider: "claude-code", UserPrompt: "x", WorkDir: dir, Observer: obs})
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want prefix %q", err, tt.wantErr)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %q, want it to contain %q", err, want)
				}
			}
			for _, leak := range []string{"TEXT-fixture", "INPUT-fixture", "OUTPUT-fixture"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("err = %q, leaks %q", err, leak)
				}
			}
			if len(obs.assistant) != 1 || len(obs.results) != 1 {
				t.Errorf("observer saw %d assistant and %d result events, want 1 and 1", len(obs.assistant), len(obs.results))
			}
		})
	}
}

// TestLiveDenialReachesObserver checks a gate denial on the live path is
// reported to the observer with the tool_use id and the deny message, and
// that the control request itself is not treated as stream activity.
func TestLiveDenialReachesObserver(t *testing.T) {
	t.Parallel()
	obs := &recordingObserver{}
	s := &liveSession{
		ctx: context.Background(),
		canUseTool: func(context.Context, string, json.RawMessage, string) (Decision, error) {
			return Decision{Message: "write your plan first"}, nil
		},
		obs:   obs,
		stdin: nopWriteCloser{&bytes.Buffer{}},
	}
	line := `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"a"},"tool_use_id":"toolu_w"}}`
	if err := s.handle([]byte(line)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(obs.denied) != 1 || obs.denied[0] != "toolu_w: write your plan first" {
		t.Errorf("denied = %v", obs.denied)
	}

	// An allowed call reports nothing.
	s.canUseTool = func(context.Context, string, json.RawMessage, string) (Decision, error) {
		return Decision{Allow: true}, nil
	}
	if err := s.handle([]byte(strings.Replace(line, "r1", "r2", 1))); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(obs.denied) != 1 {
		t.Errorf("allowed call reported as denied: %v", obs.denied)
	}

	// Stream activity on the live path reaches the observer too.
	if err := s.handle([]byte(`{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"plan"}]}}`)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(obs.assistant) != 1 || obs.assistant[0].Text[0] != "plan" {
		t.Errorf("assistant = %+v", obs.assistant)
	}
}
