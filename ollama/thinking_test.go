package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// fakeOllama replays scripted /api/chat replies in order and records each
// request body, raw and decoded.
type fakeOllama struct {
	mu      sync.Mutex
	replies []fakeReply
	raw     []string
	reqs    []chatRequest
}

type fakeReply struct {
	status int
	body   string
}

func newFakeOllama(t *testing.T, replies ...fakeReply) (*fakeOllama, *httptest.Server) {
	t.Helper()
	f := &fakeOllama{replies: replies}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		_ = json.Unmarshal(body, &req)
		f.mu.Lock()
		n := len(f.reqs)
		f.raw = append(f.raw, string(body))
		f.reqs = append(f.reqs, req)
		f.mu.Unlock()
		if n >= len(f.replies) {
			http.Error(w, "unexpected extra request", http.StatusInternalServerError)
			return
		}
		reply := f.replies[n]
		if reply.status != 0 {
			w.WriteHeader(reply.status)
		}
		_, _ = io.WriteString(w, reply.body)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeOllama) requests() ([]chatRequest, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chatRequest(nil), f.reqs...), append([]string(nil), f.raw...)
}

// Reply shapes captured from qwen3 on Ollama 0.40.16.
const (
	thinkingOnlyReply = `{"model":"qwen3","message":{"role":"assistant","content":"","thinking":"Let me reason about which tool to call first..."},"done":true,"done_reason":"length","prompt_eval_count":48000,"eval_count":1024}`
	thinkingToolReply = `{"model":"qwen3","message":{"role":"assistant","content":"","thinking":"I should look up the weather.","tool_calls":[{"id":"call_6e1h11pf","function":{"index":0,"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":true,"done_reason":"stop","prompt_eval_count":40,"eval_count":66}`
	toolOnlyReply     = `{"model":"qwen3","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_7kj0xuay","function":{"index":0,"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":true,"done_reason":"stop","prompt_eval_count":40,"eval_count":26}`
)

func weatherOpts() []llms.CallOption {
	return []llms.CallOption{
		llms.WithMaxTokens(1024),
		llms.WithTools([]llms.Tool{{Type: "function", Function: &llms.FunctionDefinition{
			Name: "get_weather", Description: "Get current weather for a city",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
		}}}),
	}
}

func userMsg(text string) []llms.MessageContent {
	return []llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(text)}}}
}

// TestThinkSentExplicitly pins the think field squad sends. Omitting it lets a
// thinking model such as qwen3 think by default, so the default is an
// explicit false.
func TestThinkSentExplicitly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts []Option
		want bool
	}{
		{"default sends false", nil, false},
		{"off", []Option{WithThink(false)}, false},
		{"on", []Option{WithThink(true)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, srv := newFakeOllama(t, fakeReply{body: toolOnlyReply})
			llm := New(srv.URL, "qwen3", 8192, tt.opts...)
			if _, err := llm.GenerateContent(context.Background(), userMsg("weather?"), weatherOpts()...); err != nil {
				t.Fatalf("GenerateContent() error = %v", err)
			}
			_, raw := f.requests()
			var sent map[string]any
			if err := json.Unmarshal([]byte(raw[0]), &sent); err != nil {
				t.Fatalf("request is not JSON: %v", err)
			}
			got, present := sent["think"]
			if !present || got != tt.want {
				t.Fatalf("request think = %v (present=%v), want an explicit %v", got, present, tt.want)
			}
		})
	}
}

// TestReplyBudgetSentAsNumPredict pins the reply budget squad asks Ollama
// for: an eighth of num_ctx, a quarter with thinking on so the reasoning
// cannot use the whole reply, and never more than the caller's max tokens.
// 16384 is the least the runner ever passes.
func TestReplyBudgetSentAsNumPredict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		think     bool
		numCtx    int
		maxTokens int
		want      any
	}{
		{"an eighth of the default window", false, 32768, 16384, float64(4096)},
		{"a quarter with thinking on", true, 32768, 16384, float64(8192)},
		{"an eighth of a larger window", false, 65536, 16384, float64(8192)},
		{"a quarter of a larger window with thinking on", true, 65536, 16384, float64(16384)},
		{"never more than the caller's max tokens", false, 65536, 1024, float64(1024)},
		{"never more than the caller's max tokens with thinking on", true, 65536, 1024, float64(1024)},
		{"the share when the caller sets no max tokens", false, 32768, 0, float64(4096)},
		{"the caller's max tokens when there is no window", false, 0, 1024, float64(1024)},
		{"nothing when there is neither", false, 0, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, srv := newFakeOllama(t, fakeReply{body: toolOnlyReply})
			var opts []llms.CallOption
			if tt.maxTokens > 0 {
				opts = append(opts, llms.WithMaxTokens(tt.maxTokens))
			}
			llm := New(srv.URL, "qwen3", tt.numCtx, WithThink(tt.think))
			if _, err := llm.GenerateContent(context.Background(), userMsg("weather?"), opts...); err != nil {
				t.Fatalf("GenerateContent() error = %v", err)
			}
			reqs, _ := f.requests()
			if got := reqs[0].Options["num_predict"]; got != tt.want {
				t.Fatalf("num_predict = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestThinkingRaisesRequestTimeout checks that a client with thinking on
// waits longer for each reply, by at least the factor its reply budget is
// larger, and that a client with thinking off keeps the five-minute limit.
func TestThinkingRaisesRequestTimeout(t *testing.T) {
	t.Parallel()
	off := New("http://x", "qwen3", 65536)
	on := New("http://x", "qwen3", 65536, WithThink(true))
	if got := off.httpClient.Timeout; got != 5*time.Minute {
		t.Fatalf("thinking-off timeout = %s, want 5m0s", got)
	}
	larger := on.replyBudget(0) / off.replyBudget(0)
	if larger < 2 {
		t.Fatalf("the thinking reply budget is %d times the plain one; the test expects it to be larger", larger)
	}
	if want := time.Duration(larger) * off.httpClient.Timeout; on.httpClient.Timeout < want {
		t.Fatalf("thinking-on timeout = %s, want at least %s for a reply budget %d times larger",
			on.httpClient.Timeout, want, larger)
	}
}

// TestRequestTimeoutBoundsTheReply checks that the client's own timeout is
// the one that ends a request whose reply does not arrive.
func TestRequestTimeoutBoundsTheReply(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	llm := New(srv.URL, "qwen3", 65536)
	llm.httpClient.Timeout = 50 * time.Millisecond
	_, err := llm.GenerateContent(context.Background(), userMsg("weather?"), weatherOpts()...)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want a timeout", err)
	}
}

// TestThinkingOnlyTurnIsRetried is the regression test for qwen3's empty
// final message: content empty, no tool calls, only thinking. Squad must
// retry with thinking off and a nudge instead of ending the run.
func TestThinkingOnlyTurnIsRetried(t *testing.T) {
	t.Parallel()
	f, srv := newFakeOllama(t, fakeReply{body: thinkingOnlyReply}, fakeReply{body: toolOnlyReply})
	llm := New(srv.URL, "qwen3", 65536, WithThink(true))
	resp, err := llm.GenerateContent(context.Background(), userMsg("weather?"), weatherOpts()...)
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	if len(resp.Choices[0].ToolCalls) != 1 || resp.Choices[0].ToolCalls[0].FunctionCall.Name != "get_weather" {
		t.Fatalf("retry's tool call must be returned, got %+v", resp.Choices[0])
	}
	reqs, _ := f.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2 (original + one retry)", len(reqs))
	}
	if reqs[0].Think != true {
		t.Fatalf("first request think = %v, want true", reqs[0].Think)
	}
	if reqs[1].Think != false {
		t.Fatalf("retry think = %v, want false", reqs[1].Think)
	}
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	if last.Role != "user" || last.Content != emptyTurnNudge {
		t.Fatalf("retry must end with the nudge, got %+v", last)
	}
	if len(reqs[1].Tools) != 1 {
		t.Fatal("retry must keep the tools")
	}
}

// TestEmptyTurnTwiceFails checks that a second empty turn is an error naming
// the cause, never a silent empty final answer.
func TestEmptyTurnTwiceFails(t *testing.T) {
	t.Parallel()
	_, srv := newFakeOllama(t, fakeReply{body: thinkingOnlyReply}, fakeReply{body: thinkingOnlyReply})
	llm := New(srv.URL, "qwen3", 65536, WithThink(true))
	_, err := llm.GenerateContent(context.Background(), userMsg("weather?"), weatherOpts()...)
	if !errors.Is(err, ErrEmptyTurn) {
		t.Fatalf("error = %v, want ErrEmptyTurn", err)
	}
	for _, want := range []string{"qwen3", "done_reason=length", "num_predict=1024", "--max-tokens", "--num-ctx"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q must mention %q", err, want)
		}
	}
}

// TestEmptyTurnWithoutThinking covers a non-thinking model's empty reply: it
// gets the same retry, so the run never ends on an empty answer.
func TestEmptyTurnWithoutThinking(t *testing.T) {
	t.Parallel()
	empty := `{"model":"llama3","message":{"role":"assistant","content":"  \n"},"done":true,"done_reason":"stop"}`
	final := `{"model":"llama3","message":{"role":"assistant","content":"done"},"done":true,"done_reason":"stop"}`
	f, srv := newFakeOllama(t, fakeReply{body: empty}, fakeReply{body: final})
	resp, err := New(srv.URL, "llama3", 8192).GenerateContent(context.Background(), userMsg("hi"))
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	if resp.Choices[0].Content != "done" {
		t.Fatalf("content = %q, want done", resp.Choices[0].Content)
	}
	if reqs, _ := f.requests(); len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
}

// TestToolCallsKeptAlongsideThinking checks a thinking turn that also calls a
// tool: the call, its server ID, and the thinking all survive, with no retry.
func TestToolCallsKeptAlongsideThinking(t *testing.T) {
	t.Parallel()
	f, srv := newFakeOllama(t, fakeReply{body: thinkingToolReply})
	resp, err := New(srv.URL, "qwen3", 8192, WithThink(true)).GenerateContent(context.Background(), userMsg("weather?"), weatherOpts()...)
	if err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	choice := resp.Choices[0]
	if len(choice.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(choice.ToolCalls))
	}
	tc := choice.ToolCalls[0]
	if tc.ID != "call_6e1h11pf" || tc.FunctionCall.Name != "get_weather" || tc.FunctionCall.Arguments != `{"city":"Paris"}` {
		t.Fatalf("tool call = %+v / %+v", tc, tc.FunctionCall)
	}
	if choice.ReasoningContent != "I should look up the weather." {
		t.Fatalf("ReasoningContent = %q", choice.ReasoningContent)
	}
	if choice.StopReason != "stop" {
		t.Fatalf("StopReason = %q, want stop", choice.StopReason)
	}
	if reqs, _ := f.requests(); len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
}

// TestStreamKeepsThinkingAndToolCalls checks the streaming path: thinking
// chunks are not streamed as text, and tool calls are kept.
func TestStreamKeepsThinkingAndToolCalls(t *testing.T) {
	t.Parallel()
	ndjson := strings.Join([]string{
		`{"message":{"role":"assistant","content":"","thinking":"hmm "},"done":false}`,
		`{"message":{"role":"assistant","content":"","thinking":"ok"},"done":false}`,
		`{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c1","function":{"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":false}`,
		`{"message":{"role":"assistant","content":"Checking."},"done":false}`,
		`{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":7}`,
	}, "\n")
	var streamed []string
	full, err := New("http://x", "qwen3", 8192).readStream(context.Background(), strings.NewReader(ndjson), func(_ context.Context, b []byte) error {
		streamed = append(streamed, string(b))
		return nil
	})
	if err != nil {
		t.Fatalf("readStream() error = %v", err)
	}
	if full.Message.Thinking != "hmm ok" {
		t.Fatalf("thinking = %q, want %q", full.Message.Thinking, "hmm ok")
	}
	if len(full.Message.ToolCalls) != 1 || full.Message.ToolCalls[0].ID != "c1" {
		t.Fatalf("tool calls = %+v", full.Message.ToolCalls)
	}
	if fmt.Sprint(streamed) != "[Checking.]" {
		t.Fatalf("streamed = %v, want only the text", streamed)
	}
	if full.DoneReason != "stop" || full.EvalCount != 7 {
		t.Fatalf("done fields = %q/%d", full.DoneReason, full.EvalCount)
	}
}

// TestThinkRejectedByModelFails covers think=true on a model that cannot
// think: Ollama's 400 reaches the caller as is, naming the cause, and squad
// does not send the request again.
func TestThinkRejectedByModelFails(t *testing.T) {
	t.Parallel()
	f, srv := newFakeOllama(t,
		fakeReply{status: http.StatusBadRequest, body: `{"error":"\"llama3\" does not support thinking"}`},
	)
	_, err := New(srv.URL, "llama3", 8192, WithThink(true)).GenerateContent(context.Background(), userMsg("hi"))
	if err == nil {
		t.Fatal("GenerateContent() must fail when the model rejects think")
	}
	if !strings.Contains(err.Error(), "does not support thinking") {
		t.Fatalf("error %q must carry Ollama's explanation", err)
	}
	if reqs, _ := f.requests(); len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1 (no resend)", len(reqs))
	}
}

// TestToolResultCarriesToolName checks tool results name their tool, which
// Ollama's chat templates use to pair results with calls.
func TestToolResultCarriesToolName(t *testing.T) {
	t.Parallel()
	out, err := convertMessages([]llms.MessageContent{{
		Role:  llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{llms.ToolCallResponse{ToolCallID: "c1", Name: "get_weather", Content: "sunny"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ToolName != "get_weather" {
		t.Fatalf("tool_name = %q, want get_weather", out[0].ToolName)
	}
}
