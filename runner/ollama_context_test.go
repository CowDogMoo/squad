package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/ollama"
)

// probeMessage is the part of an Ollama chat message the fake model reads.
type probeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// probeRequest is one request the fake Ollama received.
type probeRequest struct {
	Messages []probeMessage
	// Tokens is the size of the prompt by the fake tokenizer.
	Tokens     int
	NumCtx     int
	NumPredict int
}

// fakeOllamaAgent is a fake Ollama server that plays an agent. script picks
// each reply from the request: a file to read, or the final answer. Its
// tokenizer yields one token per bytesPerToken bytes of message content, tool
// calls, and tool definitions, and like Ollama it reports that count as
// prompt_eval_count, capped at num_ctx because an over-long prompt is cut to
// fit.
type fakeOllamaAgent struct {
	bytesPerToken int
	// script returns the path to read next, or "" and the final answer.
	script func(msgs []probeMessage) (path, answer string)

	mu       sync.Mutex
	requests []probeRequest
}

func (f *fakeOllamaAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			probeMessage
			ToolCalls json.RawMessage `json:"tool_calls"`
		} `json:"messages"`
		Tools   json.RawMessage `json:"tools"`
		Options struct {
			NumCtx     int `json:"num_ctx"`
			NumPredict int `json:"num_predict"`
		} `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	size := len(req.Tools)
	msgs := make([]probeMessage, len(req.Messages))
	for i, m := range req.Messages {
		size += len(m.Content) + len(m.ToolCalls)
		msgs[i] = m.probeMessage
	}
	tokens := (size + f.bytesPerToken - 1) / f.bytesPerToken

	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, probeRequest{Messages: msgs, Tokens: tokens, NumCtx: req.Options.NumCtx, NumPredict: req.Options.NumPredict})

	message := map[string]any{"role": "assistant", "content": ""}
	if path, answer := f.script(msgs); path != "" {
		message["tool_calls"] = []map[string]any{{
			"id":       fmt.Sprintf("call-%d", len(f.requests)),
			"function": map[string]any{"name": "Read", "arguments": map[string]any{"path": path}},
		}}
	} else {
		message["content"] = answer
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model": "qwen3", "message": message,
		"done": true, "done_reason": "stop",
		"prompt_eval_count": min(tokens, req.Options.NumCtx), "eval_count": 10,
	})
}

func (f *fakeOllamaAgent) received() []probeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]probeRequest(nil), f.requests...)
}

// runOllamaAgent runs an agent against model through InvokeModel, the path
// every agent run takes: the real tool loop and Ollama client, the full tool
// set with the Task tool, and the max tokens the runner itself settles on.
func runOllamaAgent(ctx context.Context, t *testing.T, model *fakeOllamaAgent, dir string, numCtx, maxIter int, bundle *agent.Bundle) string {
	t.Helper()
	srv := httptest.NewServer(model)
	defer srv.Close()
	opts := &RunOptions{
		Provider: "ollama", Model: "qwen3", BaseURL: srv.URL, NumCtx: numCtx,
		MaxIterations: maxIter, MaxIterationsExplicit: true,
	}
	bundle.WorkDir = dir
	response, _, err := InvokeModel(ctx, opts, bundle)
	if err != nil {
		t.Fatalf("InvokeModel() error = %v", err)
	}
	return response
}

func firstToolOutput(msgs []probeMessage) string {
	for _, m := range msgs {
		if m.Role == "tool" {
			return m.Content
		}
	}
	return ""
}

func lastToolOutput(msgs []probeMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "tool" {
			return msgs[i].Content
		}
	}
	return ""
}

// probeLines returns about size bytes of text in 250-byte lines.
func probeLines(size int) string {
	line := strings.Repeat("x", 249) + "\n"
	return strings.Repeat(line, max(1, size/len(line)))
}

func writeProbeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestOllamaCompactedReadIsServedAgain is the regression test for the read
// cache not knowing what was cut from an Ollama request to fit num_ctx. It
// runs the real tool loop, Read tool, and Ollama client against a fake Ollama
// server: the agent reads a.go, keeps reading until that output has been
// compacted out of the request, and reads a.go again. It must get the file,
// where it used to get "[CACHE HIT ... Content was already provided]" and,
// after three of those, "READ LOOP DETECTED". One case per compaction step.
func TestOllamaCompactedReadIsServedAgain(t *testing.T) {
	t.Parallel()
	const (
		target     = "a.go"
		marker     = "// A-GO-FILE-MARKER"
		maxFillers = 30
	)
	tests := []struct {
		name        string
		numCtx      int
		targetBytes int
		// compacted reports whether the request no longer carries the first
		// Read of the target whole.
		compacted func(msgs []probeMessage) bool
	}{
		{
			name:        "older output replaced",
			numCtx:      32768,
			targetBytes: 3_000,
			compacted: func(msgs []probeMessage) bool {
				return strings.HasPrefix(firstToolOutput(msgs), "[tool output compacted")
			},
		},
		{
			// The file alone is bigger than the window.
			name:        "latest output shortened",
			numCtx:      8192,
			targetBytes: 30_000,
			compacted: func(msgs []probeMessage) bool {
				return strings.Contains(lastToolOutput(msgs), "lines omitted from tool output")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeProbeFile(t, dir, target, marker+"\n"+probeLines(tt.targetBytes))
			for i := 1; i <= maxFillers; i++ {
				writeProbeFile(t, dir, fmt.Sprintf("filler-%02d.txt", i), fmt.Sprintf("filler %d\n", i)+probeLines(16_000))
			}

			var (
				calls, fillers int
				asked          bool
				reread         string
			)
			model := &fakeOllamaAgent{bytesPerToken: 4}
			model.script = func(msgs []probeMessage) (string, string) {
				calls++
				switch {
				case calls == 1:
					return target, ""
				case asked:
					if reread == "" {
						reread = lastToolOutput(msgs)
					}
					return "", "done"
				case tt.compacted(msgs):
					asked = true
					return target, ""
				case fillers < maxFillers:
					fillers++
					return fmt.Sprintf("filler-%02d.txt", fillers), ""
				default:
					return "", "gave up"
				}
			}

			response := runOllamaAgent(context.Background(), t, model, dir, tt.numCtx, maxFillers+10,
				&agent.Bundle{System: "You fix Go code.", User: "Fix the bug in a.go."})

			if !asked {
				t.Fatalf("the first Read of %s was never compacted out of a request (%d requests, %d filler reads, response %q)",
					target, calls, fillers, response)
			}
			if !strings.Contains(reread, marker) {
				t.Fatalf("re-reading %s after its output was compacted must return the file, got: %.300s", target, reread)
			}
			for _, stub := range []string{"CACHE HIT", "READ LOOP DETECTED"} {
				if strings.Contains(reread, stub) {
					t.Fatalf("re-reading %s after its output was compacted returned a %s answer: %.300s", target, stub, reread)
				}
			}
			if response != "done" {
				t.Fatalf("response = %q, want done", response)
			}
		})
	}
}

// denseOutput returns size bytes of the tool output a web-app mapper
// collects: compact JSON with short keys, hex IDs, and digit runs. Tokenizers
// split such text into far more tokens per byte than prose.
func denseOutput(turn, size int) string {
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, `{"id":"%08x","ts":17317%05d,"ok":true,"n":[%d,%d,%d]},`, turn*7919+i, i, i, i+1, i+2)
	}
	return b.String()[:size]
}

// TestOllamaConversationStaysInsideWindow is the regression test for the
// context balloon: a mapper's conversation grew far past num_ctx, so Ollama
// cut the prompt from the front and the model lost its instructions. Through
// the real tool loop and Ollama client, an agent collects 40 token-dense
// outputs, 240K tokens in all by a tokenizer that finds one every 2 bytes,
// denser than squad assumes until it learns otherwise from prompt_eval_count.
// Every request must leave the model its whole prompt plus room for the reply
// inside num_ctx, start with the system prompt and the task, and still carry
// the recent outputs that fit.
func TestOllamaConversationStaysInsideWindow(t *testing.T) {
	t.Parallel()
	const (
		numCtx      = 65536
		turns       = 40
		outputBytes = 12_000
		system      = "SYSTEM: you are the webapp-map agent. Never lose these instructions."
		task        = "TASK: map every endpoint of the target."
	)
	dir := t.TempDir()
	for i := 1; i <= turns; i++ {
		writeProbeFile(t, dir, fmt.Sprintf("page-%02d.json", i), denseOutput(i, outputBytes))
	}

	reads := 0
	model := &fakeOllamaAgent{bytesPerToken: 2}
	model.script = func([]probeMessage) (string, string) {
		if reads == turns {
			return "", "mapped"
		}
		reads++
		return fmt.Sprintf("page-%02d.json", reads), ""
	}

	response := runOllamaAgent(context.Background(), t, model, dir, numCtx, turns+5, &agent.Bundle{System: system, User: task})
	if response != "mapped" {
		t.Fatalf("response = %q, want mapped", response)
	}

	requests := model.received()
	if len(requests) != turns+1 {
		t.Fatalf("requests = %d, want %d", len(requests), turns+1)
	}
	compacted := false
	for i, req := range requests {
		if req.NumCtx != numCtx || req.NumPredict <= 0 {
			t.Fatalf("request %d has num_ctx=%d num_predict=%d", i+1, req.NumCtx, req.NumPredict)
		}
		if req.Tokens+req.NumPredict > req.NumCtx {
			t.Fatalf("request %d holds %d tokens: with %d for the reply it overflows num_ctx=%d", i+1, req.Tokens, req.NumPredict, req.NumCtx)
		}
		if got := req.Messages[0]; got.Role != "system" || got.Content != system {
			t.Fatalf("request %d does not start with the whole system prompt: %.120q", i+1, got.Content)
		}
		if got := req.Messages[1]; got.Role != "user" || got.Content != task {
			t.Fatalf("request %d lost the task: %.120q", i+1, got.Content)
		}
		whole := 0
		for _, m := range req.Messages {
			switch {
			case m.Role != "tool":
			case strings.HasPrefix(m.Content, "[tool output compacted"):
				compacted = true
			case len(m.Content) >= outputBytes:
				whole++
			}
		}
		if want := min(i, 3); whole < want {
			t.Fatalf("request %d carries %d whole tool outputs, want at least the %d newest", i+1, whole, want)
		}
	}
	if !compacted {
		t.Fatal("no request was compacted, so the test did not reach the window")
	}
}

// TestOllamaDefaultWindowKeepsReadsThatFit is the regression test for the
// reply taking half of the default window. The runner never asks for fewer
// than 16384 reply tokens, and squad set all of them aside, so at
// num_ctx=32768 an agent with a 27 KB prompt had no room left: every file it
// read reached the model cut down to about 2 KB, and every request warned
// that the window was too small. With that prompt and three 20 KB reads, the
// newest read must arrive whole at the default window and all three at
// 65536, with nothing shortened and no warning.
func TestOllamaDefaultWindowKeepsReadsThatFit(t *testing.T) {
	t.Parallel()
	const (
		reads     = 3
		readBytes = 20_000
	)
	tests := []struct {
		name      string
		numCtx    int
		wantWhole int
	}{
		{"default window", 32768, 1},
		{"larger window", 65536, reads},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for i := 1; i <= reads; i++ {
				writeProbeFile(t, dir, fmt.Sprintf("file-%d.go", i), probeLines(readBytes))
			}
			system := "You review Go code.\n" + strings.Repeat("Check every exported function against the review checklist. ", 450)
			if len(system) < 26_000 || len(system) > 28_000 {
				t.Fatalf("the system prompt is %d bytes, want about 27 KB", len(system))
			}

			done := 0
			model := &fakeOllamaAgent{bytesPerToken: 4}
			model.script = func([]probeMessage) (string, string) {
				if done == reads {
					return "", "reviewed"
				}
				done++
				return fmt.Sprintf("file-%d.go", done), ""
			}
			var logs bytes.Buffer
			ctx := logging.WithLogger(context.Background(), &logging.CustomLogger{LogLevel: slog.LevelInfo, ConsoleWriter: &logs})

			response := runOllamaAgent(ctx, t, model, dir, tt.numCtx, reads+5, &agent.Bundle{System: system, User: "Review the three files."})
			if response != "reviewed" {
				t.Fatalf("response = %q, want reviewed", response)
			}

			requests := model.received()
			if len(requests) != reads+1 {
				t.Fatalf("requests = %d, want %d", len(requests), reads+1)
			}
			whole := 0
			for i, req := range requests[1:] {
				if got := req.Messages[0]; got.Role != "system" || got.Content != system {
					t.Fatalf("request %d does not start with the whole system prompt", i+2)
				}
				if got := lastToolOutput(req.Messages); len(got) < readBytes {
					t.Fatalf("request %d carries the newest read as %d bytes, want all %d", i+2, len(got), readBytes)
				}
				whole = 0
				for _, m := range req.Messages {
					switch {
					case m.Role != "tool":
					case len(m.Content) >= readBytes:
						whole++
					case !strings.HasPrefix(m.Content, "[tool output compacted"):
						t.Fatalf("request %d carries a read that is neither whole nor replaced: %.120q", i+2, m.Content)
					}
				}
			}
			if whole < tt.wantWhole {
				t.Fatalf("the last request carries %d whole reads, want at least %d", whole, tt.wantWhole)
			}
			if strings.Contains(logs.String(), "raise the model's context window") {
				t.Fatalf("squad warned that the window is too small:\n%s", logs.String())
			}
		})
	}
}

// TestOllamaEmptyTurnErrorNamesNumCtx is the regression test for the
// empty-turn error sending users to --max-tokens. The runner raises max
// tokens to at least 16384 before the Ollama client sees it, so at the
// default window the reply budget is its share of num_ctx whatever
// --max-tokens says: a user who raised --max-tokens as the error advised got
// the same failure with the same num_predict. Through InvokeModel, a model
// whose replies run into the budget without answering must produce an error
// that points at --num-ctx and not at --max-tokens.
func TestOllamaEmptyTurnErrorNamesNumCtx(t *testing.T) {
	t.Parallel()
	for _, maxTokens := range []int{1024, 8192} {
		t.Run(fmt.Sprintf("max tokens %d", maxTokens), func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				requests++
				mu.Unlock()
				_, _ = w.Write([]byte(`{"model":"qwen3","message":{"role":"assistant","content":"","thinking":"still reasoning"},"done":true,"done_reason":"length","prompt_eval_count":10,"eval_count":4096}`))
			}))
			defer srv.Close()

			opts := &RunOptions{
				Provider: "ollama", Model: "qwen3", BaseURL: srv.URL, NumCtx: 32768, MaxTokens: maxTokens,
				MaxIterations: 5, MaxIterationsExplicit: true,
			}
			bundle := &agent.Bundle{System: "You map web applications.", User: "Map the target.", WorkDir: t.TempDir()}
			_, _, err := InvokeModel(context.Background(), opts, bundle)
			if !errors.Is(err, ollama.ErrEmptyTurn) {
				t.Fatalf("InvokeModel() error = %v, want ollama.ErrEmptyTurn", err)
			}
			for _, want := range []string{"num_predict=4096", "raise --num-ctx"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q must mention %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "--max-tokens") {
				t.Fatalf("error %q must not point at --max-tokens: raising it does not change the reply budget here", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if requests != 2 {
				t.Fatalf("requests = %d, want the first attempt and one retry", requests)
			}
		})
	}
}
