package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/skill"
	"github.com/cowdogmoo/squad/tools"
	openai "github.com/openai/openai-go/v3"
	oairesponses "github.com/openai/openai-go/v3/responses"
	"github.com/tmc/langchaingo/llms"
)

func TestIsReasoningModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{"gpt-5", "gpt-5", true},
		{"gpt-5.2-codex", "gpt-5.2-codex", true},
		{"gpt-5-mini", "gpt-5-mini", true},
		{"gpt-4o", "gpt-4o", false},
		{"claude-3", "claude-3", false},
		{"empty", "", false},
		{"case insensitive", "GPT-5", true},
		{"whitespace", " gpt-5 ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsReasoningModel(tt.model, []string{"gpt-5"}); got != tt.want {
				t.Fatalf("IsReasoningModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestUseResponsesAPI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		model    string
		want     bool
	}{
		{"openai-responses with any model", "openai-responses", "gpt-4o", true},
		{"openai with gpt-5", "openai", "gpt-5", true},
		{"openai with gpt-4", "openai", "gpt-4o", false},
		{"empty provider with gpt-5", "", "gpt-5", false},
		{"ollama with gpt-5", "ollama", "gpt-5", false},
		{"anthropic", "anthropic", "claude-3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := UseResponsesAPI(tt.provider, tt.model, []string{"gpt-5"}); got != tt.want {
				t.Fatalf("UseResponsesAPI(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestConvertTools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   []llms.Tool
		wantLen int
	}{
		{"nil input", nil, 0},
		{"empty input", []llms.Tool{}, 0},
		{
			"valid tool",
			[]llms.Tool{
				{Function: &llms.FunctionDefinition{Name: "Echo", Description: "desc", Parameters: map[string]any{"type": "object"}}},
			},
			1,
		},
		{
			"tool with nil function",
			[]llms.Tool{
				{},
				{Function: &llms.FunctionDefinition{Name: "Echo", Description: "desc", Parameters: "bad"}},
			},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ConvertTools(tt.input)
			if len(got) != tt.wantLen {
				t.Fatalf("ConvertTools() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestExtractFunctionCalls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		resp    *oairesponses.Response
		wantLen int
	}{
		{"nil response", nil, 0},
		{"empty output", &oairesponses.Response{Output: nil}, 0},
		{
			"mixed output items",
			&oairesponses.Response{
				Output: []oairesponses.ResponseOutputItemUnion{
					{Type: "message", ID: "msg"},
					{Type: "function_call", ID: "1", CallID: "call-1", Name: "Echo", Arguments: "{}"},
					{Type: "message", ID: "msg2"},
					{Type: "function_call", ID: "2", CallID: "call-2", Name: "Read", Arguments: `{"path":"x"}`},
				},
			},
			2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractFunctionCalls(tt.resp)
			if len(got) != tt.wantLen {
				t.Fatalf("ExtractFunctionCalls() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestCheckRepeat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		iterations int
		wantLast   bool
	}{
		{"below threshold", 5, false},
		{"at threshold minus one", 9, false},
		{"at threshold", 10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var repeat tools.RepeatTracker
			var exceeded bool
			for i := 0; i < tt.iterations; i++ {
				exceeded = checkRepeat(ctx, &repeat, []FunctionCall{{Name: "Tool", Arguments: "{}"}})
			}
			if exceeded != tt.wantLast {
				t.Fatalf("checkRepeat after %d iterations = %v, want %v", tt.iterations, exceeded, tt.wantLast)
			}
		})
	}
}

func TestConfigApplyOptionals(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		wantTemp      bool
		wantMaxTokens bool
	}{
		{
			"non-reasoning with temperature",
			Config{Model: "gpt-4o", Temperature: 0.7, ReasoningPrefixes: []string{"gpt-5"}},
			true,
			false,
		},
		{
			"reasoning model skips temperature",
			Config{Model: "gpt-5-turbo", Temperature: 0.5, ReasoningPrefixes: []string{"gpt-5"}},
			false,
			true,
		},
		{
			"explicit max tokens",
			Config{Model: "gpt-5", MaxTokens: 2048, ReasoningPrefixes: []string{"gpt-5"}},
			false,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var params oairesponses.ResponseNewParams
			tt.config.applyOptionals(&params)
			var emptyTemp oairesponses.ResponseNewParams
			hasTemp := !reflect.DeepEqual(params.Temperature, emptyTemp.Temperature)
			if hasTemp != tt.wantTemp {
				t.Fatalf("temperature set = %v, want %v", hasTemp, tt.wantTemp)
			}
			hasMax := !reflect.DeepEqual(params.MaxOutputTokens, emptyTemp.MaxOutputTokens)
			if hasMax != tt.wantMaxTokens {
				t.Fatalf("max tokens set = %v, want %v", hasMax, tt.wantMaxTokens)
			}
		})
	}
}

func TestExecuteAndBuildOutputs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	handlers := map[string]tools.Handler{
		"Success": {
			Call: func(context.Context, []byte) (string, error) { return "ok", nil },
		},
		"Fail": {
			Call: func(context.Context, []byte) (string, error) { return "", errors.New("boom") },
		},
	}
	tests := []struct {
		name       string
		calls      []FunctionCall
		wantOutput string
	}{
		{
			"missing tool",
			[]FunctionCall{{Name: "Missing", CallID: "call-miss", Arguments: "{}"}},
			"unknown tool: Missing",
		},
		{
			"success",
			[]FunctionCall{{Name: "Success", CallID: "call-ok", Arguments: "{}"}},
			"ok",
		},
		{
			"failure",
			[]FunctionCall{{Name: "Fail", CallID: "call-fail", Arguments: "{}"}},
			"error: boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			outputs := executeAndBuildOutputs(ctx, tt.calls, handlers)
			if len(outputs) != 1 {
				t.Fatalf("expected 1 output, got %d", len(outputs))
			}
			got := outputs[0].OfFunctionCallOutput
			if got == nil {
				t.Fatalf("expected function call output")
			}
			if !reflect.DeepEqual(got.Output.OfString, openai.String(tt.wantOutput)) {
				t.Fatalf("output = %v, want %q", got.Output.OfString, tt.wantOutput)
			}
		})
	}
}

func TestExecuteAndBuildOutputsDeniedByAllowedTools(t *testing.T) {
	t.Parallel()
	// Active skill on the stack restricts the agent to Read only. Any other
	// tool call must short-circuit with the structured denial message and
	// must NOT invoke the handler.
	stack := skill.NewStack()
	stack.Push(skill.Entry{
		Manifest: &skill.Manifest{
			Name:         "restricted",
			Description:  "x",
			AllowedTools: skill.AllowedTools{"Read"},
		},
		Dir: t.TempDir(),
	})
	ctx := tools.WithSkillRuntime(context.Background(), &tools.SkillRuntime{Stack: stack})
	var bashInvocations atomic.Int32
	handlers := map[string]tools.Handler{
		"Read": {Call: func(context.Context, []byte) (string, error) { return "read-ok", nil }},
		"Bash": {Call: func(context.Context, []byte) (string, error) {
			bashInvocations.Add(1)
			return "should-not-run", nil
		}},
	}
	calls := []FunctionCall{
		{Name: "Bash", CallID: "call-denied", Arguments: "{}"},
		{Name: "Read", CallID: "call-allowed", Arguments: "{}"},
	}
	outputs := executeAndBuildOutputs(ctx, calls, handlers)
	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outputs))
	}
	deniedOut := outputs[0].OfFunctionCallOutput
	if deniedOut == nil || deniedOut.CallID != "call-denied" {
		t.Fatalf("first output should mirror the denied call, got %+v", deniedOut)
	}
	wantDenied := `error: tool "Bash" is not permitted by the active skill's allowed-tools`
	if !reflect.DeepEqual(deniedOut.Output.OfString, openai.String(wantDenied)) {
		t.Fatalf("denial output = %v, want %q", deniedOut.Output.OfString, wantDenied)
	}
	if got := bashInvocations.Load(); got != 0 {
		t.Fatalf("Bash handler was invoked %d times; denial must short-circuit", got)
	}
	allowedOut := outputs[1].OfFunctionCallOutput
	if allowedOut == nil || allowedOut.CallID != "call-allowed" {
		t.Fatalf("second output should mirror the allowed call, got %+v", allowedOut)
	}
	if !reflect.DeepEqual(allowedOut.Output.OfString, openai.String("read-ok")) {
		t.Fatalf("allowed output = %v, want %q", allowedOut.Output.OfString, "read-ok")
	}
}

func TestLogOutputItems(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		resp  *oairesponses.Response
		label string
	}{
		{"nil response", nil, "nil"},
		{
			"with output",
			&oairesponses.Response{
				ID:     "resp",
				Status: "completed",
				Usage: oairesponses.ResponseUsage{
					InputTokens:  2,
					OutputTokens: 3,
				},
				Output: []oairesponses.ResponseOutputItemUnion{
					{Type: "message", ID: "msg"},
				},
			},
			"with-output",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// logOutputItems only logs; verify no panic.
			logOutputItems(
				context.Background(), tt.resp, tt.label,
			)
		})
	}
}

func TestRunWithToolsNoToolCalls(t *testing.T) {
	t.Parallel()
	payload := map[string]any{
		"id":                  "resp-1",
		"object":              "response",
		"created_at":          0,
		"model":               "gpt-4o",
		"parallel_tool_calls": false,
		"temperature":         0,
		"tool_choice":         "auto",
		"tools":               []any{},
		"top_p":               1,
		"error": map[string]any{
			"code":    "server_error",
			"message": "",
		},
		"incomplete_details": map[string]any{"reason": ""},
		"instructions":       "system",
		"metadata":           map[string]any{},
		"output": []map[string]any{
			{
				"id":     "msg-1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]any{
					{"type": "output_text", "text": "hello"},
				},
			},
		},
	}

	reqErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			reqErr <- errors.New("unexpected path")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		reqErr <- json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	td := t.TempDir()
	resp, err := RunWithTools(
		context.Background(),
		"key",
		server.URL,
		"gpt-4o",
		"system",
		"user",
		td,
		"",
		"",
		0.4,
		0,
		1,
		0,
		nil,
		nil,
		nil,
		&executor.LocalExecutor{WorkingDir: td},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunWithTools() error = %v", err)
	}
	if err := <-reqErr; err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp != "hello" {
		t.Fatalf("response = %q, want hello", resp)
	}
}

func TestRequestFinalMissingID(t *testing.T) {
	t.Parallel()
	client := openai.NewClient()
	cfg := &Config{Model: "gpt-4o"}
	if _, err := requestFinal(context.Background(), client, "", "sys", cfg, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunWithToolsExhaustedWithPendingCalls(t *testing.T) {
	t.Parallel()

	// Scenario: maxIterations=1.
	// Call 1 (initial): returns a function_call.
	// Call 2 (tool output follow-up): returns another function_call
	//   → loop exits at iteration budget with pending calls.
	// Call 3 (resolvePendingCalls): dummy outputs + tool_choice=none → text.
	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		var payload map[string]any
		switch count {
		case 1:
			// Initial request → function_call
			payload = map[string]any{
				"id": "resp-1", "object": "response", "created_at": 0,
				"model": "gpt-4o", "parallel_tool_calls": false,
				"temperature": 0, "tool_choice": "auto", "tools": []any{},
				"top_p":              1,
				"error":              map[string]any{"code": "server_error", "message": ""},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"output": []map[string]any{
					{
						"id": "fc-1", "type": "function_call",
						"call_id": "call-1", "name": "Echo", "arguments": `{"msg":"hi"}`,
					},
				},
			}
		case 2:
			// After tool output → another function_call (budget exhausted)
			payload = map[string]any{
				"id": "resp-2", "object": "response", "created_at": 0,
				"model": "gpt-4o", "parallel_tool_calls": false,
				"temperature": 0, "tool_choice": "auto", "tools": []any{},
				"top_p":              1,
				"error":              map[string]any{"code": "server_error", "message": ""},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"output": []map[string]any{
					{
						"id": "fc-2", "type": "function_call",
						"call_id": "call-2", "name": "Echo", "arguments": `{"msg":"again"}`,
					},
				},
			}
		default:
			// resolvePendingCalls → text response
			payload = map[string]any{
				"id": "resp-3", "object": "response", "created_at": 0,
				"model": "gpt-4o", "parallel_tool_calls": false,
				"temperature": 0, "tool_choice": "auto", "tools": []any{},
				"top_p":              1,
				"error":              map[string]any{"code": "server_error", "message": ""},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"output": []map[string]any{
					{
						"id": "msg-1", "type": "message", "role": "assistant",
						"status": "completed",
						"content": []map[string]any{
							{"type": "output_text", "text": "resolved"},
						},
					},
				},
			}
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	td2 := t.TempDir()
	resp, err := RunWithTools(
		context.Background(),
		"key",
		server.URL,
		"gpt-4o",
		"system",
		"user",
		td2,
		"",
		"",
		0.4,
		0,
		1, // maxIterations=1 → loop exits with pending calls
		0,
		nil,
		&tools.TaskConfig{},
		nil,
		&executor.LocalExecutor{WorkingDir: td2},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunWithTools() error = %v", err)
	}
	if resp != "resolved" {
		t.Fatalf("response = %q, want %q", resp, "resolved")
	}
	finalCount := atomic.LoadInt32(&callCount)
	if finalCount != 3 {
		t.Fatalf("expected 3 API calls (initial + follow-up + resolve), got %d", finalCount)
	}
}

func TestRunWithToolsFollowUp(t *testing.T) {
	t.Parallel()
	reqErr := make(chan error, 2)
	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		var payload map[string]any
		switch count {
		case 1:
			payload = map[string]any{
				"id":                  "resp-1",
				"object":              "response",
				"created_at":          0,
				"model":               "gpt-4o",
				"parallel_tool_calls": false,
				"temperature":         0,
				"tool_choice":         "auto",
				"tools":               []any{},
				"top_p":               1,
				"error": map[string]any{
					"code":    "server_error",
					"message": "",
				},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"output": []map[string]any{
					{
						"id":        "call-1",
						"type":      "function_call",
						"call_id":   "call-1",
						"name":      "MissingTool",
						"arguments": "{}",
					},
				},
			}
		default:
			payload = map[string]any{
				"id":                  "resp-2",
				"object":              "response",
				"created_at":          0,
				"model":               "gpt-4o",
				"parallel_tool_calls": false,
				"temperature":         0,
				"tool_choice":         "auto",
				"tools":               []any{},
				"top_p":               1,
				"error": map[string]any{
					"code":    "server_error",
					"message": "",
				},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"output": []map[string]any{
					{
						"id":     "msg-1",
						"type":   "message",
						"role":   "assistant",
						"status": "completed",
						"content": []map[string]any{
							{"type": "output_text", "text": "follow-up"},
						},
					},
				},
			}
		}
		reqErr <- json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	td3 := t.TempDir()
	resp, err := RunWithTools(
		context.Background(),
		"key",
		server.URL,
		"gpt-4o",
		"system",
		"user",
		td3,
		"",
		"",
		0.4,
		0,
		2,
		0,
		nil,
		nil,
		nil,
		&executor.LocalExecutor{WorkingDir: td3},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunWithTools() error = %v", err)
	}
	if resp != "follow-up" {
		t.Fatalf("response = %q, want follow-up", resp)
	}
	for i := 0; i < 2; i++ {
		if err := <-reqErr; err != nil {
			t.Fatalf("handler error: %v", err)
		}
	}
}

func TestRequestFinal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		payload     map[string]any
		wantErr     bool
		wantText    string
		wantErrPart string
	}{
		{
			name: "success",
			payload: map[string]any{
				"id":                  "resp-final",
				"object":              "response",
				"created_at":          0,
				"model":               "gpt-4o",
				"parallel_tool_calls": false,
				"temperature":         0,
				"tool_choice":         "none",
				"tools":               []any{},
				"top_p":               1,
				"error": map[string]any{
					"code":    "server_error",
					"message": "",
				},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"output": []map[string]any{
					{
						"id":     "msg-1",
						"type":   "message",
						"role":   "assistant",
						"status": "completed",
						"content": []map[string]any{
							{"type": "output_text", "text": "final"},
						},
					},
				},
			},
			wantText: "final",
		},
		{
			name: "empty output",
			payload: map[string]any{
				"id":                  "resp-final",
				"object":              "response",
				"created_at":          0,
				"model":               "gpt-4o",
				"parallel_tool_calls": false,
				"temperature":         0,
				"tool_choice":         "none",
				"tools":               []any{},
				"top_p":               1,
				"error": map[string]any{
					"code":    "server_error",
					"message": "",
				},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"output":             []map[string]any{},
			},
			wantErr:     true,
			wantErrPart: "empty text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.payload)
			}))
			defer server.Close()

			client := newClient("key", server.URL, "org")
			cfg := &Config{Model: "gpt-4o"}
			text, err := requestFinal(
				context.Background(),
				client,
				"resp-1",
				"system",
				cfg,
				nil,
			)
			if (err != nil) != tt.wantErr {
				t.Fatalf("requestFinal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErrPart != "" && err != nil && !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantErrPart)
			}
			if !tt.wantErr && text != tt.wantText {
				t.Fatalf("text = %q, want %q", text, tt.wantText)
			}
		})
	}
}

func TestRunWithToolsErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) *httptest.Server
	}{
		{
			name: "initial request error",
			setup: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("boom"))
				}))
			},
		},
		{
			name: "resolve pending error",
			setup: func(t *testing.T) *httptest.Server {
				callCount := int32(0)
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					count := atomic.AddInt32(&callCount, 1)
					if count == 3 {
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = w.Write([]byte("boom"))
						return
					}
					w.Header().Set("Content-Type", "application/json")
					payload := map[string]any{
						"id":                  fmt.Sprintf("resp-%d", count),
						"object":              "response",
						"created_at":          0,
						"model":               "gpt-4o",
						"parallel_tool_calls": false,
						"temperature":         0,
						"tool_choice":         "auto",
						"tools":               []any{},
						"top_p":               1,
						"error":               map[string]any{"code": "server_error", "message": ""},
						"incomplete_details":  map[string]any{"reason": ""},
						"instructions":        "system",
						"metadata":            map[string]any{},
						"output": []map[string]any{
							{
								"id":        "fc-1",
								"type":      "function_call",
								"call_id":   "call-1",
								"name":      "Missing",
								"arguments": "{}",
							},
						},
					}
					_ = json.NewEncoder(w).Encode(payload)
				}))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setup(t)
			defer server.Close()

			td := t.TempDir()
			_, err := RunWithTools(
				context.Background(),
				"key",
				server.URL,
				"gpt-4o",
				"system",
				"user",
				td,
				"",
				"",
				0.2,
				0,
				1,
				0,
				nil,
				&tools.TaskConfig{},
				nil,
				&executor.LocalExecutor{WorkingDir: td},
				nil,
				nil,
			)
			if err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestRequestFinalBudgetExceeded(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	m.AddTokens(1_000_000, 1_000_000)

	client := openai.NewClient()
	cfg := &Config{Model: "gpt-4o"}
	_, err := requestFinal(context.Background(), client, "resp-1", "sys", cfg, m)
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
}

func TestRunWithToolsBudgetExceededDuringLoop(t *testing.T) {
	t.Parallel()

	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		var payload map[string]any
		switch count {
		case 1:
			// Initial request → function_call
			payload = map[string]any{
				"id": "resp-1", "object": "response", "created_at": 0,
				"model": "gpt-4o", "parallel_tool_calls": false,
				"temperature": 0, "tool_choice": "auto", "tools": []any{},
				"top_p":              1,
				"error":              map[string]any{"code": "server_error", "message": ""},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"output": []map[string]any{
					{
						"id": "fc-1", "type": "function_call",
						"call_id": "call-1", "name": "Echo", "arguments": `{"msg":"hi"}`,
					},
				},
			}
		default:
			// Follow-up returns text (budget check happens after this)
			payload = map[string]any{
				"id": "resp-2", "object": "response", "created_at": 0,
				"model": "gpt-4o", "parallel_tool_calls": false,
				"temperature": 0, "tool_choice": "auto", "tools": []any{},
				"top_p":              1,
				"error":              map[string]any{"code": "server_error", "message": ""},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"usage":              map[string]any{"input_tokens": 500000, "output_tokens": 500000},
				"output": []map[string]any{
					{
						"id": "fc-2", "type": "function_call",
						"call_id": "call-2", "name": "Echo", "arguments": `{"msg":"again"}`,
					},
				},
			}
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	m.AddTokens(1_000_000, 1_000_000) // pre-exceed budget

	td4 := t.TempDir()
	text, err := RunWithTools(
		context.Background(),
		"key",
		server.URL,
		"gpt-4o",
		"system",
		"user",
		td4,
		"",
		"",
		0.4,
		0,
		10,
		0,
		nil,
		nil,
		m,
		&executor.LocalExecutor{WorkingDir: td4},
		nil,
		nil,
	)
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	// Should still return partial text
	_ = text
}

func TestRunWithToolsInputTokenCapReached(t *testing.T) {
	t.Parallel()

	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		var payload map[string]any
		switch count {
		case 1:
			payload = map[string]any{
				"id": "resp-1", "object": "response", "created_at": 0,
				"model": "gpt-4o", "parallel_tool_calls": false,
				"temperature": 0, "tool_choice": "auto", "tools": []any{},
				"top_p":              1,
				"error":              map[string]any{"code": "server_error", "message": ""},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"output": []map[string]any{
					{
						"id": "fc-1", "type": "function_call",
						"call_id": "call-1", "name": "Echo", "arguments": `{"msg":"hi"}`,
					},
				},
			}
		default:
			payload = map[string]any{
				"id": "resp-2", "object": "response", "created_at": 0,
				"model": "gpt-4o", "parallel_tool_calls": false,
				"temperature": 0, "tool_choice": "auto", "tools": []any{},
				"top_p":              1,
				"error":              map[string]any{"code": "server_error", "message": ""},
				"incomplete_details": map[string]any{"reason": ""},
				"instructions":       "system",
				"metadata":           map[string]any{},
				"usage":              map[string]any{"input_tokens": MaxResponsesInputTokens + 1, "output_tokens": 1000},
				"output": []map[string]any{
					{
						"id": "fc-2", "type": "function_call",
						"call_id": "call-2", "name": "Echo", "arguments": `{"msg":"again"}`,
					},
				},
			}
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	td := t.TempDir()
	_, err := RunWithTools(
		context.Background(),
		"key",
		server.URL,
		"gpt-4o",
		"system",
		"user",
		td,
		"",
		"",
		0.4,
		0,
		10,
		0,
		nil,
		nil,
		nil,
		&executor.LocalExecutor{WorkingDir: td},
		nil,
		nil,
	)
	if !errors.Is(err, ErrInputTokenCapReached) {
		t.Fatalf("expected ErrInputTokenCapReached, got: %v", err)
	}
}

func TestExecuteAndBuildOutputsWithResultAndError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	handlers := map[string]tools.Handler{
		"PartialFail": {
			Call: func(context.Context, []byte) (string, error) {
				return "partial output", fmt.Errorf("something failed")
			},
		},
	}
	calls := []FunctionCall{{Name: "PartialFail", CallID: "call-pf", Arguments: "{}"}}
	outputs := executeAndBuildOutputs(ctx, calls, handlers)
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}
	got := outputs[0].OfFunctionCallOutput
	if got == nil {
		t.Fatalf("expected function call output")
	}
	wantOutput := "partial output\n\nerror: something failed"
	if !reflect.DeepEqual(got.Output.OfString, openai.String(wantOutput)) {
		t.Fatalf("output = %v, want %q", got.Output.OfString, wantOutput)
	}
}

func TestTrackResponseMetricsNilMetrics(t *testing.T) {
	t.Parallel()
	// Should not panic when metrics is nil
	trackResponseMetrics(nil, nil) // nil response and nil metrics
}

func TestTrackResponseMetricsNilResponse(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")
	trackResponseMetrics(nil, m)
	// Should not increment or add tokens when response is nil
	if m.Iterations() != 0 {
		t.Fatalf("Iterations = %d, want 0", m.Iterations())
	}
}

func TestTrackResponseMetricsWithUsage(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")

	// Create a mock response with usage data
	resp := &oairesponses.Response{
		Usage: oairesponses.ResponseUsage{
			InputTokens:  1000,
			OutputTokens: 500,
		},
	}

	trackResponseMetrics(resp, m)

	if m.Iterations() != 1 {
		t.Fatalf("Iterations = %d, want 1", m.Iterations())
	}
	if m.InputTokens() != 1000 {
		t.Fatalf("InputTokens = %d, want 1000", m.InputTokens())
	}
	if m.OutputTokens() != 500 {
		t.Fatalf("OutputTokens = %d, want 500", m.OutputTokens())
	}
}

func TestTrackResponseMetricsAccumulates(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")

	resp1 := &oairesponses.Response{
		Usage: oairesponses.ResponseUsage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}
	resp2 := &oairesponses.Response{
		Usage: oairesponses.ResponseUsage{
			InputTokens:  200,
			OutputTokens: 100,
		},
	}

	trackResponseMetrics(resp1, m)
	trackResponseMetrics(resp2, m)

	if m.Iterations() != 2 {
		t.Fatalf("Iterations = %d, want 2", m.Iterations())
	}
	if m.InputTokens() != 300 {
		t.Fatalf("InputTokens = %d, want 300", m.InputTokens())
	}
	if m.OutputTokens() != 150 {
		t.Fatalf("OutputTokens = %d, want 150", m.OutputTokens())
	}
}

// TestRunWithToolsRegistersSkillAndConfirm regression-tests the
// Responses API path's tool registration. Before the fix this file
// landed in, the Responses path called tools.BuildHandlers (which omits
// the Skill and Confirm tools) regardless of whether the runner had
// constructed runtimes for them. The fix routes through
// tools.BuildHandlersWithSkill and forwards the runtimes — verified
// here by capturing the outgoing request and asserting that the tool
// names appear (when runtimes are non-nil) or are absent (when nil).
func TestRunWithToolsRegistersSkillAndConfirm(t *testing.T) {
	t.Parallel()

	// Minimal valid response payload: a single message, no tool calls.
	payload := map[string]any{
		"id":                  "resp-skill",
		"object":              "response",
		"created_at":          0,
		"model":               "gpt-4o",
		"parallel_tool_calls": false,
		"temperature":         0,
		"tool_choice":         "auto",
		"tools":               []any{},
		"top_p":               1,
		"error":               map[string]any{"code": "", "message": ""},
		"incomplete_details":  map[string]any{"reason": ""},
		"instructions":        "system",
		"metadata":            map[string]any{},
		"output": []map[string]any{
			{
				"id":     "msg-1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]any{
					{"type": "output_text", "text": "ok"},
				},
			},
		},
	}

	type capture struct {
		Tools []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tools"`
	}

	runAndCapture := func(t *testing.T, skillRT *tools.SkillRuntime, confirmRT *tools.ConfirmRuntime) capture {
		t.Helper()
		var cap capture
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &cap)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(payload)
		}))
		defer server.Close()

		td := t.TempDir()
		_, err := RunWithTools(
			context.Background(),
			"key",
			server.URL,
			"gpt-4o",
			"system",
			"user",
			td,
			"",
			"",
			0.4,
			0,
			1,
			0,
			nil,
			nil,
			nil,
			&executor.LocalExecutor{WorkingDir: td},
			skillRT,
			confirmRT,
		)
		if err != nil {
			t.Fatalf("RunWithTools() error = %v", err)
		}
		return cap
	}

	hasTool := func(c capture, name string) bool {
		for _, tdef := range c.Tools {
			if tdef.Name == name {
				return true
			}
		}
		return false
	}

	t.Run("both nil → neither tool registered", func(t *testing.T) {
		t.Parallel()
		got := runAndCapture(t, nil, nil)
		if hasTool(got, "Skill") {
			t.Errorf("Skill tool was registered with nil runtime; tools=%+v", got.Tools)
		}
		if hasTool(got, "Confirm") {
			t.Errorf("Confirm tool was registered with nil runtime; tools=%+v", got.Tools)
		}
	})

	t.Run("populated runtimes → both tools registered", func(t *testing.T) {
		t.Parallel()
		skillRT := &tools.SkillRuntime{
			Entries: []skill.Entry{{
				Manifest: &skill.Manifest{
					Name:        "demo-skill",
					Description: "regression-test fixture",
				},
				Scope: skill.ScopeRepo,
			}},
			Stack: skill.NewStack(),
		}
		confirmRT := &tools.ConfirmRuntime{}

		got := runAndCapture(t, skillRT, confirmRT)
		if !hasTool(got, "Skill") {
			t.Errorf("Skill tool MISSING from Responses API request; tools=%+v", got.Tools)
		}
		if !hasTool(got, "Confirm") {
			t.Errorf("Confirm tool MISSING from Responses API request; tools=%+v", got.Tools)
		}
	})
}

func TestLogEvent_NilLogger(t *testing.T) {
	t.Parallel()
	// logEvent with nil logger should not panic.
	logEvent(nil, "test_event", map[string]any{"key": "val"})
}

func TestLogEvent_WithLogger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l, err := session.New(dir, "test-agent", "openai", "gpt-4o", "test prompt")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	defer func() {
		if err := l.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	// Should not panic or error.
	logEvent(l, session.EventRunStart, map[string]any{"agent": "test"})
}

func TestFormatLargeResultPlaceholderContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		resultID   string
		toolName   string
		totalBytes int
		wantSubs   []string
	}{
		{
			name:       "basic",
			resultID:   "res-123",
			toolName:   "Bash",
			totalBytes: 4096,
			wantSubs:   []string{"res-123", "4096", "Bash"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLargeResultPlaceholder(tt.resultID, tt.toolName, tt.totalBytes)
			for _, sub := range tt.wantSubs {
				if !strings.Contains(got, sub) {
					t.Errorf("formatLargeResultPlaceholder() = %q, missing %q", got, sub)
				}
			}
		})
	}
}
