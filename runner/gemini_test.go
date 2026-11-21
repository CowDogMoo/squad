package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/tools"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
)

type geminiTestTransport func(*http.Request) (*http.Response, error)

func (f geminiTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Read one byte at a time to exercise JSON boundaries and escaped model text,
// and to prevent HTTP read-ahead from hiding a missed trailing usage chunk.
type geminiTestBody struct {
	io.Reader
	closed bool
}

func (b *geminiTestBody) Read(p []byte) (int, error) {
	return b.Reader.Read(p[:min(len(p), 1)])
}

func (b *geminiTestBody) Close() error { b.closed = true; return nil }

func geminiTestModel(t *testing.T, replies ...string) *geminiLLM {
	t.Helper()
	var bodies []*geminiTestBody
	transport := geminiTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("key") != "test-key" {
			t.Error("Google client did not preserve API key authentication")
		}
		if len(replies) == 0 {
			t.Fatal("unexpected Gemini request")
		}
		body := &geminiTestBody{Reader: strings.NewReader(replies[0])}
		replies = replies[1:]
		bodies = append(bodies, body)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body, Request: req}, nil
	})
	client, err := googleai.New(context.Background(), googleai.WithAPIKey("test-key"),
		googleai.WithHTTPClient(&http.Client{Transport: geminiUsageTransport{base: transport}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		for _, b := range bodies {
			if !b.closed {
				t.Error("Gemini response body was not closed")
			}
		}
	})
	return &geminiLLM{GoogleAI: client}
}

func geminiChunk(t *testing.T, text string, usage *geminiUsage) string {
	t.Helper()
	chunk := map[string]any{"candidates": []any{map[string]any{
		"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}},
		"finishReason": "STOP",
	}}}
	if usage != nil {
		chunk["usageMetadata"] = usage
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGeminiRecordsFinalBilledUsage(t *testing.T) {
	for _, trailing := range []bool{false, true} {
		name := "final content chunk"
		if trailing {
			name = "trailing usage after empty candidate"
		}
		t.Run(name, func(t *testing.T) {
			first := geminiChunk(t, `answer {"nested": "}\\"}`, &geminiUsage{Prompt: 1000, Candidates: 1, Total: 1001, Cached: 800})
			final := geminiChunk(t, " done", &geminiUsage{Prompt: 1200, Candidates: 100, Total: 2000, Cached: 900})
			reply := "[" + first + "," + final + "]"
			if trailing {
				reply = "[" + first + `,{"candidates":[{"finishReason":"STOP"}]},{"usageMetadata":{"promptTokenCount":1200,"candidatesTokenCount":100,"totalTokenCount":2000,"cachedContentTokenCount":900}}]`
			}
			model := geminiTestModel(t, reply)
			m := metrics.New("gemini", "gemini-2.5-flash")
			var streamed strings.Builder
			out, err := tools.RunWithTools(context.Background(), model, "system", "user", t.TempDir(), tools.RunWithToolsConfig{
				Provider: "gemini", Model: "gemini-2.5-flash", Metrics: m, MaxIterations: 1,
			}, llms.WithStreamingFunc(func(_ context.Context, p []byte) error { streamed.Write(p); return nil }))
			if err != nil {
				t.Fatal(err)
			}
			if out != streamed.String() || !strings.Contains(out, "answer") {
				t.Fatalf("content/stream changed: output=%q stream=%q", out, streamed.String())
			}
			want := metrics.Usage{InputTokens: 300, CacheReadTokens: 900, OutputTokens: 800}
			if got := m.Usage(); got != want {
				t.Fatalf("recorded usage = %+v, want %+v (100 visible + 700 thinking output)", got, want)
			}
			prices := metrics.Pricing{InputPerMillion: 10, CacheReadPerMillion: 1, OutputPerMillion: 40}
			cost := prices.Cost(m.Usage())
			if math.Abs(cost.Input-0.003) > 1e-12 || math.Abs(cost.CacheRead-0.0009) > 1e-12 ||
				math.Abs(cost.Output-0.032) > 1e-12 || math.Abs(cost.Total()-0.0359) > 1e-12 {
				t.Fatalf("billed cost = %+v, want 0.003 input + 0.0009 cache + 0.032 output", cost)
			}
		})
	}
}

func TestGeminiUsageAcrossCallModesAndTurns(t *testing.T) {
	for _, mode := range []string{"single", "single stream", "chat", "Call"} {
		t.Run(mode, func(t *testing.T) {
			final := geminiChunk(t, "answer", &geminiUsage{Prompt: 1000, Candidates: 50, Total: 1550, Cached: 800})
			second := geminiChunk(t, "next", &geminiUsage{Prompt: 100, Candidates: 20, Total: 120})
			if strings.Contains(mode, "stream") || mode == "chat" {
				first := geminiChunk(t, "", &geminiUsage{Prompt: 1000, Total: 1000})
				final, second = "["+first+","+final+"]", "["+second+"]"
			}
			model := geminiTestModel(t, final, second)
			if mode == "Call" {
				// Call must dispatch through the wrapper too.
				if out, err := model.Call(context.Background(), "user"); err != nil || out != "answer" {
					t.Fatalf("Call = %q, %v", out, err)
				}
				return
			}
			messages := []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "user")}
			if mode == "chat" {
				messages = append([]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeSystem, "system")}, messages...)
			}
			var opts []llms.CallOption
			if strings.Contains(mode, "stream") {
				opts = append(opts, llms.WithStreamingFunc(func(context.Context, []byte) error { return nil }))
			}
			for i, want := range []struct{ prompt, output, cached int64 }{{1000, 550, 800}, {100, 20, 0}} {
				resp, err := model.GenerateContent(context.Background(), messages, opts...)
				if err != nil {
					t.Fatal(err)
				}
				gi := resp.Choices[0].GenerationInfo
				if gi["PromptTokens"] != want.prompt || gi["CompletionTokens"] != want.output || gi["CachedTokens"] != want.cached {
					t.Fatalf("turn %d usage = %v, want %+v", i, gi, want)
				}
			}
		})
	}
}

func TestGeminiStreamingCallbackError(t *testing.T) {
	reply := "[" + geminiChunk(t, "answer", &geminiUsage{Prompt: 100, Total: 120}) + "]"
	model := geminiTestModel(t, reply)
	want := errors.New("stop streaming")
	_, err := model.GenerateContent(context.Background(), []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "user")},
		llms.WithStreamingFunc(func(context.Context, []byte) error { return want }))
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want callback error", err)
	}
}

func TestGeminiPreservesToolCalls(t *testing.T) {
	reply := `[{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"Read","args":{"path":"notes.txt"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":320}}]`
	model := geminiTestModel(t, reply)
	resp, err := model.GenerateContent(context.Background(), []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "user")},
		llms.WithStreamingFunc(func(context.Context, []byte) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	ch := resp.Choices[0]
	if len(ch.ToolCalls) != 1 || ch.ToolCalls[0].FunctionCall.Name != "Read" || ch.ToolCalls[0].FunctionCall.Arguments != `{"path":"notes.txt"}` {
		t.Fatalf("tool calls changed: %+v", ch.ToolCalls)
	}
	if ch.GenerationInfo["CompletionTokens"] != int64(220) {
		t.Fatalf("tool response billed output = %v, want 220", ch.GenerationInfo["CompletionTokens"])
	}
}
