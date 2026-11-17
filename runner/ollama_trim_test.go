package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/metrics"
)

// probeMessage is the part of an Ollama chat message the fake model reads.
type probeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const (
	probeTarget       = "target.go"
	probeTargetMarker = "// TARGET-FILE-MARKER"
	probeMaxFillers   = 30
)

// trimProbeModel is a fake Ollama server playing an agent that reads
// target.go, keeps reading other files until squad has trimmed that first
// output out of the request, and then reads target.go again.
type trimProbeModel struct {
	// trimmed reports whether the request no longer carries the first Read
	// of the target whole.
	trimmed func(msgs []probeMessage) bool
	// chatter is the text the model sends along with every tool call.
	chatter string

	mu      sync.Mutex
	calls   int
	fillers int
	asked   bool   // the second Read of the target was requested
	done    bool   // its output was seen
	reread  string // that output, as the request delivered it
}

func (m *trimProbeModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []probeMessage `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	switch {
	case m.calls == 1:
		m.readFile(w, probeTarget)
	case m.asked:
		if !m.done {
			m.done = true
			m.reread = lastToolOutput(req.Messages)
		}
		m.finish(w, "done")
	case m.trimmed(req.Messages):
		m.asked = true
		m.readFile(w, probeTarget)
	case m.fillers < probeMaxFillers:
		m.fillers++
		m.readFile(w, fmt.Sprintf("filler-%02d.txt", m.fillers))
	default:
		m.finish(w, "gave up")
	}
}

func (m *trimProbeModel) readFile(w http.ResponseWriter, path string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model": "qwen3",
		"message": map[string]any{
			"role":    "assistant",
			"content": m.chatter,
			"tool_calls": []map[string]any{{
				"id":       fmt.Sprintf("call-%d", m.calls),
				"function": map[string]any{"name": "Read", "arguments": map[string]any{"path": path}},
			}},
		},
		"done": true, "done_reason": "stop", "prompt_eval_count": 10, "eval_count": 10,
	})
}

func (m *trimProbeModel) finish(w http.ResponseWriter, text string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model":   "qwen3",
		"message": map[string]any{"role": "assistant", "content": text},
		"done":    true, "done_reason": "stop", "prompt_eval_count": 10, "eval_count": 5,
	})
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

// TestOllamaTrimmedReadIsServedAgain is the regression test for the read
// cache not knowing what the Ollama client trims. An agent reads a file, the
// client later trims that output out of the request to fit num_ctx, and the
// agent reads the file again: it must get the content, where it used to get
// "[CACHE HIT ... Content was already provided]". One case per trimming step.
func TestOllamaTrimmedReadIsServedAgain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		numCtx      int
		targetBytes int
		fillerBytes int
		chatter     int
		trimmed     func(msgs []probeMessage) bool
	}{
		{
			name:        "older output replaced by a placeholder",
			numCtx:      32768,
			targetBytes: 3_000,
			fillerBytes: 16_000,
			trimmed: func(msgs []probeMessage) bool {
				return strings.HasPrefix(firstToolOutput(msgs), "[older tool output removed by squad")
			},
		},
		{
			// The model's own text piles up, so replacing tool outputs
			// stops being enough and whole turns go.
			name:        "oldest turns removed",
			numCtx:      32768,
			targetBytes: 3_000,
			fillerBytes: 1_000,
			chatter:     6_000,
			trimmed: func(msgs []probeMessage) bool {
				for _, m := range msgs {
					if m.Role == "user" && strings.HasPrefix(m.Content, "[squad removed the") {
						return true
					}
				}
				return false
			},
		},
		{
			// The file alone is bigger than the window.
			name:        "latest output cut in the middle",
			numCtx:      16384,
			targetBytes: 40_000,
			trimmed: func(msgs []probeMessage) bool {
				return strings.Contains(lastToolOutput(msgs), "bytes removed by squad to fit num_ctx")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			write := func(name, content string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write(probeTarget, probeTargetMarker+"\n"+probeLines(tt.targetBytes))
			for i := 1; i <= probeMaxFillers; i++ {
				write(fmt.Sprintf("filler-%02d.txt", i), fmt.Sprintf("filler %d\n", i)+probeLines(tt.fillerBytes))
			}

			model := &trimProbeModel{trimmed: tt.trimmed, chatter: strings.Repeat("y", tt.chatter)}
			srv := httptest.NewServer(model)
			defer srv.Close()

			opts := &RunOptions{Provider: "ollama", BaseURL: srv.URL, NumCtx: tt.numCtx, MaxIterations: probeMaxFillers + 10}
			bundle := &agent.Bundle{System: "You fix Go code.", User: "Fix the bug in target.go.", WorkDir: dir}
			response, err := callLangChainLLM(
				context.Background(),
				opts,
				"ollama",
				"qwen3",
				bundle.System,
				bundle,
				0.2,
				1024,
				opts.MaxIterations,
				nil,
				&executor.LocalExecutor{WorkingDir: dir},
				metrics.New("ollama", "qwen3"),
			)
			if err != nil {
				t.Fatalf("callLangChainLLM() error = %v", err)
			}

			model.mu.Lock()
			defer model.mu.Unlock()
			if !model.done {
				t.Fatalf("the first Read of %s was never trimmed out of a request (%d requests, %d filler reads, response %q)",
					probeTarget, model.calls, model.fillers, response)
			}
			if !strings.Contains(model.reread, probeTargetMarker) {
				t.Fatalf("re-reading %s after its output was trimmed must return the file, got: %.300s", probeTarget, model.reread)
			}
			if response != "done" {
				t.Fatalf("response = %q, want done", response)
			}
		})
	}
}
