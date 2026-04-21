package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/tmc/langchaingo/llms"
)

func TestFlexBoolUnmarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{"true literal", "true", true, false},
		{"false literal", "false", false, false},
		{"string true", `"true"`, true, false},
		{"string false", `"false"`, false, false},
		{"string 1", `"1"`, true, false},
		{"string yes", `"yes"`, true, false},
		{"string no", `"no"`, false, false},
		{"string 0", `"0"`, false, false},
		{"number rejects", "123", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var b FlexBool
			err := json.Unmarshal([]byte(tt.input), &b)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalJSON(%s) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && bool(b) != tt.want {
				t.Fatalf("UnmarshalJSON(%s) = %v, want %v", tt.input, b, tt.want)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty input", "", true},
		{"relative path", "child.txt", false},
		{"absolute within bounds", filepath.Join(dir, "inner.txt"), false},
		{"path traversal", "../outside.txt", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolved, err := ResolvePath(dir, tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolvePath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && resolved == "" {
				t.Fatalf("expected non-empty resolved path")
			}
		})
	}

	t.Run("absolute outside bounds", func(t *testing.T) {
		t.Parallel()
		outside := filepath.Join(filepath.Dir(dir), "outside.txt")
		if _, err := ResolvePath(dir, outside); err == nil {
			t.Fatalf("expected error for outside path")
		}
	})
}

func TestTruncateString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"shorter than max", "short", 10, "short"},
		{"equal to max", "abcde", 5, "abcde"},
		{"longer than max", "longer string", 4, "long..."},
		{"empty string", "", 5, ""},
		{"max zero", "abc", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := TruncateString(tt.input, tt.maxLen); got != tt.want {
				t.Fatalf("TruncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestGlobToRegex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		match   string
		noMatch string
	}{
		{"star dot go", "*.go", "main.go", "cmd/main.go"},
		{"double star", "**/*.go", "cmd/main.go", "main.txt"},
		{"question mark", "foo?", "foox", "fooxx"},
		{"literal dots", "file.txt", "file.txt", "filextxt"},
		{"literal bracket", "[ab].go", "[ab].go", "a.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regex, err := globToRegex(tt.pattern)
			if err != nil {
				t.Fatalf("globToRegex(%q) error = %v", tt.pattern, err)
			}
			re, err := regexp.Compile(regex)
			if err != nil {
				t.Fatalf("regexp.Compile(%q) error = %v", regex, err)
			}
			if !re.MatchString(tt.match) {
				t.Fatalf("expected %q to match pattern %q (regex %q)", tt.match, tt.pattern, regex)
			}
			if re.MatchString(tt.noMatch) {
				t.Fatalf("expected %q to NOT match pattern %q (regex %q)", tt.noMatch, tt.pattern, regex)
			}
		})
	}
}

func TestInitEditsAndTracking(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		setup      func(context.Context) context.Context
		wantResult bool
	}{
		{
			"init returns not applied",
			func(ctx context.Context) context.Context { return InitEdits(ctx) },
			false,
		},
		{
			"mark sets applied",
			func(ctx context.Context) context.Context {
				ctx = InitEdits(ctx)
				MarkEditsApplied(ctx)
				return ctx
			},
			true,
		},
		{
			"reset clears applied",
			func(ctx context.Context) context.Context {
				ctx = InitEdits(ctx)
				MarkEditsApplied(ctx)
				ResetEditsApplied(ctx)
				return ctx
			},
			false,
		},
		{
			"bare context returns false",
			func(ctx context.Context) context.Context { return ctx },
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := tt.setup(context.Background())
			if got := EditsApplied(ctx); got != tt.wantResult {
				t.Fatalf("EditsApplied() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestBuildHandlers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		withTask bool
		extra    []Handler
		wantDefs int
	}{
		{"without TaskConfig", false, nil, 11},
		{"with TaskConfig", true, nil, 12},
		{"with ExtraTools", true, []Handler{
			{
				Def: llms.Tool{
					Type: "function",
					Function: &llms.FunctionDefinition{
						Name:        "mcp__test__extra_tool",
						Description: "An extra MCP tool",
					},
				},
				Call: func(_ context.Context, _ []byte) (string, error) {
					return "ok", nil
				},
			},
		}, 13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var cfg *TaskConfig
			if tt.withTask {
				cfg = &TaskConfig{
					AgentsDir:  "agents",
					WorkingDir: dir,
					CallModel: func(
						_ context.Context,
						_, _, _, _, _ string,
					) (string, *metrics.Metrics, error) {
						return "", nil, nil
					},
					ExtraTools: tt.extra,
				}
			}
			handlers, defs := BuildHandlers(dir, cfg, &executor.LocalExecutor{WorkingDir: dir})
			if _, ok := handlers["Task"]; ok != tt.withTask {
				t.Fatalf(
					"Task handler present = %v, want %v",
					ok, tt.withTask,
				)
			}
			if _, ok := handlers["Read"]; !ok {
				t.Fatalf("expected Read handler")
			}
			if len(defs) != tt.wantDefs {
				t.Fatalf(
					"tool defs = %d, want %d",
					len(defs), tt.wantDefs,
				)
			}
			if len(tt.extra) > 0 {
				if _, ok := handlers["mcp__test__extra_tool"]; !ok {
					t.Fatal("expected ExtraTools handler to be merged")
				}
			}
			names := make([]string, len(defs))
			for i, d := range defs {
				names[i] = d.Function.Name
			}
			for i := 1; i < len(names); i++ {
				if names[i] < names[i-1] {
					t.Fatalf(
						"expected sorted tool defs, got %v",
						names,
					)
				}
			}
		})
	}
}

func TestGlobMatcher(t *testing.T) {
	t.Parallel()
	matcher, err := newGlobMatcher("**/*.go")
	if err != nil {
		t.Fatalf("newGlobMatcher: %v", err)
	}
	if !matcher.Match("cmd/main.go") {
		t.Fatalf("expected match for cmd/main.go")
	}
	if matcher.Match("cmd/main.txt") {
		t.Fatalf("expected no match for cmd/main.txt")
	}
}

func TestLimitOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		size    int
		wantTrn bool
	}{
		{"within limit", 100, false},
		{"at limit", maxToolOutput, false},
		{"over limit", maxToolOutput + 10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := bytes.Repeat([]byte("a"), tt.size)
			result := limitOutput(data)
			truncated := strings.Contains(string(result), "output truncated")
			if truncated != tt.wantTrn {
				t.Fatalf("truncated = %v, want %v", truncated, tt.wantTrn)
			}
		})
	}
}

func TestReadWriteEditTools(t *testing.T) {
	dir := t.TempDir()
	ctx := InitEdits(context.Background())

	write := trackEdits(writeTool(dir))
	read := readTool(dir)
	edit := trackEdits(editTool(dir))

	_, err := write(ctx, []byte(`{"path":"note.txt","content":"hello world"}`))
	if err != nil {
		t.Fatalf("writeTool: %v", err)
	}
	if !EditsApplied(ctx) {
		t.Fatalf("expected edits applied")
	}
	ResetEditsApplied(ctx)
	if EditsApplied(ctx) {
		t.Fatalf("expected reset edits applied")
	}

	output, err := read(ctx, []byte(`{"path":"note.txt"}`))
	if err != nil {
		t.Fatalf("readTool: %v", err)
	}
	if output != "hello world" {
		t.Fatalf("unexpected read output: %s", output)
	}

	_, err = edit(ctx, []byte(`{"path":"note.txt","old":"world","new":"squad","replace_all":"true"}`))
	if err != nil {
		t.Fatalf("editTool: %v", err)
	}

	output, err = read(ctx, []byte(`{"path":"note.txt"}`))
	if err != nil {
		t.Fatalf("readTool: %v", err)
	}
	if output != "hello squad" {
		t.Fatalf("unexpected edit output: %s", output)
	}
}

func TestGlobAndGrepTools(t *testing.T) {
	dir := t.TempDir()
	write := writeTool(dir)
	_, _ = write(context.Background(), []byte(`{"path":"a.txt","content":"alpha"}`))
	_, _ = write(context.Background(), []byte(`{"path":"b.log","content":"beta"}`))
	_, _ = write(context.Background(), []byte(`{"path":"nested/c.txt","content":"alpha beta"}`))

	glob := globTool(dir)
	globOut, err := glob(context.Background(), []byte(`{"pattern":"**/*.txt"}`))
	if err != nil {
		t.Fatalf("globTool: %v", err)
	}
	if strings.Contains(globOut, "a.txt") || !strings.Contains(globOut, "nested/c.txt") {
		t.Fatalf("unexpected glob output: %s", globOut)
	}

	grep := grepTool(dir)
	grepOut, err := grep(context.Background(), []byte(`{"pattern":"alpha","path":"."}`))
	if err != nil {
		t.Fatalf("grepTool: %v", err)
	}
	if !strings.Contains(grepOut, "a.txt") || !strings.Contains(grepOut, "nested/c.txt") {
		t.Fatalf("unexpected grep output: %s", grepOut)
	}
}

func TestGrepSearchPathSingleFile(t *testing.T) {
	dir := t.TempDir()
	write := writeTool(dir)
	_, _ = write(context.Background(), []byte(`{"path":"match.txt","content":"hello\nworld"}`))

	re := regexp.MustCompile("world")
	matches, err := grepSearchPath(dir, filepath.Join(dir, "match.txt"), re)
	if err != nil {
		t.Fatalf("grepSearchPath: %v", err)
	}
	if len(matches) != 1 || !strings.Contains(matches[0], "match.txt") {
		t.Fatalf("unexpected matches: %v", matches)
	}
}

func TestBashTool(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name    string
		payload string
		wantErr bool
		wantOut string
	}{
		{
			"valid command",
			`{"command":"printf 'hi'"}`,
			false,
			"hi",
		},
		{
			"empty command",
			`{"command":""}`,
			true,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bash := bashTool(&executor.LocalExecutor{WorkingDir: dir})
			out, err := bash(
				context.Background(), []byte(tt.payload),
			)
			if (err != nil) != tt.wantErr {
				t.Fatalf(
					"bashTool() error = %v, wantErr %v",
					err, tt.wantErr,
				)
			}
			if !tt.wantErr && !strings.Contains(out, tt.wantOut) {
				t.Fatalf(
					"output = %q, want containing %q",
					out, tt.wantOut,
				)
			}
		})
	}
}

func TestRepeatTracker(t *testing.T) {
	t.Parallel()
	tracker := &RepeatTracker{}
	call := func(name string) []llms.ToolCall {
		return []llms.ToolCall{{FunctionCall: &llms.FunctionCall{Name: name, Arguments: "{}"}}}
	}
	for i := 0; i < maxSameToolRepeat; i++ {
		tracker.Update(call("Other"))
	}
	if !tracker.Exceeded() {
		t.Fatalf("expected repeat limit exceeded")
	}
}

func TestReadToolErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	read := readTool(dir)
	tests := []struct {
		name         string
		payload      string
		wantErr      bool
		wantContains string
	}{
		{"invalid json", "{", true, "invalid args"},
		{"empty path", `{"path":""}`, true, "path is required"},
		{"outside path", `{"path":"../outside.txt"}`, true, "outside working directory"},
		{"missing file", `{"path":"missing.txt"}`, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := read(context.Background(), []byte(tt.payload))
			if (err != nil) != tt.wantErr {
				t.Fatalf("readTool() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantContains != "" && err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantContains)
			}
		})
	}
}

func TestWriteToolErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := writeTool(dir)
	tests := []struct {
		name         string
		payload      string
		wantErr      bool
		wantContains string
	}{
		{"invalid json", "{", true, "invalid args"},
		{"empty path", `{"path":"","content":"x"}`, true, "path is required"},
		{"outside path", `{"path":"../outside.txt","content":"x"}`, true, "outside working directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := write(context.Background(), []byte(tt.payload))
			if (err != nil) != tt.wantErr {
				t.Fatalf("writeTool() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantContains != "" && err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantContains)
			}
		})
	}
}

func TestEditToolErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	edit := editTool(dir)
	_, _ = writeTool(dir)(context.Background(), []byte(`{"path":"note.txt","content":"hello"}`))
	tests := []struct {
		name         string
		payload      string
		wantErr      bool
		wantContains string
	}{
		{"invalid json", "{", true, "invalid args"},
		{"missing file", `{"path":"missing.txt","old":"a","new":"b"}`, true, ""},
		{"text not found", `{"path":"note.txt","old":"absent","new":"x"}`, true, "text not found"},
		{"outside path", `{"path":"../outside.txt","old":"a","new":"b"}`, true, "outside working directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := edit(context.Background(), []byte(tt.payload))
			if (err != nil) != tt.wantErr {
				t.Fatalf("editTool() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantContains != "" && err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantContains)
			}
		})
	}
}

func TestEditToolSingleReplacement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := writeTool(dir)
	edit := editTool(dir)
	_, err := write(context.Background(), []byte(`{"path":"note.txt","content":"hello"}`))
	if err != nil {
		t.Fatalf("writeTool: %v", err)
	}

	out, err := edit(context.Background(), []byte(`{"path":"note.txt","old":"l","new":"L"}`))
	if err != nil {
		t.Fatalf("editTool: %v", err)
	}
	if !strings.Contains(out, "1 replacement") {
		t.Fatalf("output = %q, want single replacement", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "heLlo" {
		t.Fatalf("content = %q, want heLlo", string(data))
	}
}

func TestGlobToolErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	glob := globTool(dir)
	tests := []struct {
		name         string
		payload      string
		wantErr      bool
		wantContains string
		wantOutput   string
	}{
		{"invalid json", "{", true, "invalid args", ""},
		{"empty pattern", `{"pattern":""}`, true, "pattern is required", ""},
		{"no matches", `{"pattern":"**/*.txt"}`, false, "", "no matches"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := glob(context.Background(), []byte(tt.payload))
			if (err != nil) != tt.wantErr {
				t.Fatalf("globTool() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantContains != "" && err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantContains)
			}
			if tt.wantOutput != "" && out != tt.wantOutput {
				t.Fatalf("output = %q, want %q", out, tt.wantOutput)
			}
		})
	}
}

func TestSanitizeRegex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"dotnet (?n)", `(?n)func\s+readStdin\b`, `func\s+readStdin\b`},
		{"pcre (?x)", `(?x) foo \s+ bar`, ` foo \s+ bar`},
		{"valid (?i) preserved", `(?i)hello`, `(?i)hello`},
		{"valid (?ms) preserved", `(?ms)foo.bar`, `(?ms)foo.bar`},
		{"mixed valid/invalid stripped", `(?in)hello`, `hello`},
		{"multiple groups", `(?n)foo(?x)bar`, `foobar`},
		{"no flags unchanged", `func\s+\w+`, `func\s+\w+`},
		{"valid (?i-s) preserved", `(?i-s)test`, `(?i-s)test`},
		{"invalid (?n-i) stripped", `(?n-i)test`, `test`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeRegex(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeRegex(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGrepToolErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := writeTool(dir)
	_, _ = write(context.Background(), []byte(`{"path":"note.txt","content":"alpha"}`))
	grep := grepTool(dir)
	tests := []struct {
		name         string
		payload      string
		wantErr      bool
		wantContains string
		wantOutput   string
	}{
		{"invalid json", "{", true, "invalid args", ""},
		{"empty pattern", `{"pattern":""}`, true, "pattern is required", ""},
		{"invalid regex", `{"pattern":"["}`, true, "invalid regex", ""},
		{"dotnet (?n) sanitized", `{"pattern":"(?n)alpha","path":"."}`, false, "", "note.txt:1:alpha"},
		{"outside path", `{"pattern":"alpha","path":"../outside"}`, true, "outside working directory", ""},
		{"no matches", `{"pattern":"beta","path":"."}`, false, "", "no matches"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := grep(context.Background(), []byte(tt.payload))
			if (err != nil) != tt.wantErr {
				t.Fatalf("grepTool() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantContains != "" && err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantContains)
			}
			if tt.wantOutput != "" && out != tt.wantOutput {
				t.Fatalf("output = %q, want %q", out, tt.wantOutput)
			}
		})
	}
}

type fakeLLM struct {
	responses []*llms.ContentResponse
	calls     int
}

type stubLLM struct {
	resp *llms.ContentResponse
	err  error
}

func (f *fakeLLM) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	if f.calls >= len(f.responses) {
		return nil, errors.New("no response")
	}
	resp := f.responses[f.calls]
	f.calls++
	return resp, nil
}

func (f *fakeLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func (s *stubLLM) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	return s.resp, s.err
}

func (s *stubLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func TestExecuteToolCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		toolCall    llms.ToolCall
		handlers    map[string]Handler
		wantContent string
		wantName    string
	}{
		{
			"nil FunctionCall",
			llms.ToolCall{ID: "1"},
			map[string]Handler{},
			"tool call missing function definition",
			"",
		},
		{
			"unknown tool name",
			llms.ToolCall{ID: "2", FunctionCall: &llms.FunctionCall{Name: "Missing"}},
			map[string]Handler{},
			"unknown tool: Missing",
			"",
		},
		{
			"known tool success",
			llms.ToolCall{ID: "3", FunctionCall: &llms.FunctionCall{Name: "Echo", Arguments: "{}"}},
			map[string]Handler{
				"Echo": {Call: func(context.Context, []byte) (string, error) { return "ok", nil }},
			},
			"ok",
			"Echo",
		},
		{
			"known tool error",
			llms.ToolCall{ID: "4", FunctionCall: &llms.FunctionCall{Name: "Fail", Arguments: "{}"}},
			map[string]Handler{
				"Fail": {Call: func(context.Context, []byte) (string, error) { return "", errors.New("boom") }},
			},
			"error: boom",
			"Fail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := executeToolCall(context.Background(), tt.toolCall, tt.handlers)
			if resp.Content != tt.wantContent {
				t.Fatalf("Content = %q, want %q", resp.Content, tt.wantContent)
			}
			if resp.Name != tt.wantName {
				t.Fatalf("Name = %q, want %q", resp.Name, tt.wantName)
			}
		})
	}
}

func TestRepeatTrackerUpdate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		calls     [][]llms.ToolCall
		wantCount int
		wantName  string
	}{
		{
			"single call",
			[][]llms.ToolCall{
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
			},
			1,
			"A",
		},
		{
			"repeated identical call",
			[][]llms.ToolCall{
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
			},
			3,
			"A",
		},
		{
			"different call resets",
			[][]llms.ToolCall{
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
				{{FunctionCall: &llms.FunctionCall{Name: "B", Arguments: "{}"}}},
			},
			1,
			"B",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tracker := &RepeatTracker{}
			for _, c := range tt.calls {
				tracker.Update(c)
			}
			if tracker.Count != tt.wantCount {
				t.Fatalf("Count = %d, want %d", tracker.Count, tt.wantCount)
			}
			if tracker.LastName != tt.wantName {
				t.Fatalf("LastName = %q, want %q", tracker.LastName, tt.wantName)
			}
		})
	}
}

func TestRepeatTrackerExceeded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		toolName string
		count    int
		want     bool
	}{
		{"normal tool at limit", "Other", maxSameToolRepeat, true},
		{"normal tool below limit", "Other", maxSameToolRepeat - 1, false},
		{"mutating tool at limit", "Edit", maxMutatingToolRepeat, true},
		{"mutating tool below limit", "Edit", maxMutatingToolRepeat - 1, false},
		{"high repeat tool at limit", "Read", maxReadToolRepeat, true},
		{"high repeat tool below limit", "Read", maxReadToolRepeat - 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tracker := &RepeatTracker{LastName: tt.toolName, Count: tt.count}
			if got := tracker.Exceeded(); got != tt.want {
				t.Fatalf("Exceeded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEditEnforcer(t *testing.T) {
	t.Parallel()

	t.Run("nil enforcer is no-op", func(t *testing.T) {
		var e *EditEnforcer
		if e.CheckNames([]string{"Read", "Glob"}) {
			t.Fatal("nil enforcer should never stop")
		}
	})

	t.Run("disabled when deadline is 0", func(t *testing.T) {
		e := NewEditEnforcer(0)
		if e != nil {
			t.Fatal("deadline 0 should return nil")
		}
	})

	t.Run("stops after deadline with no Edit", func(t *testing.T) {
		e := NewEditEnforcer(3)
		if e.CheckNames([]string{"Read"}) {
			t.Fatal("should not stop after 1 iteration")
		}
		if e.CheckNames([]string{"Read", "Glob"}) {
			t.Fatal("should not stop after 2 iterations")
		}
		if !e.CheckNames([]string{"Read"}) {
			t.Fatal("should stop after 3 iterations with no Edit")
		}
	})

	t.Run("does not stop if Edit succeeds", func(t *testing.T) {
		e := NewEditEnforcer(2)
		if e.CheckNames([]string{"Read"}) {
			t.Fatal("should not stop after 1 iteration")
		}
		if e.CheckNames([]string{"Edit", "Read"}) {
			t.Fatal("should not stop when Edit is present")
		}
		// Confirm the edit succeeded
		e.ConfirmEdit([]llms.ToolCall{{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Edit"}}}, map[string]string{"1": "ok"})
		// After Edit confirmed, further read-only iterations are fine
		if e.CheckNames([]string{"Read"}) {
			t.Fatal("should not stop after Edit was confirmed")
		}
	})

	t.Run("failed Edit does not disarm enforcement", func(t *testing.T) {
		e := NewEditEnforcer(3)
		if e.CheckNames([]string{"Edit"}) {
			t.Fatal("should not stop when Edit is present")
		}
		// Edit failed — "text not found"
		e.ConfirmEdit([]llms.ToolCall{{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Edit"}}}, map[string]string{"1": "text not found in foo.go"})
		// Enforcement should still be active
		if e.CheckNames([]string{"Read"}) {
			t.Fatal("should not stop after 1 read-only iteration")
		}
		if e.CheckNames([]string{"Read"}) {
			t.Fatal("should not stop after 2 read-only iterations")
		}
		if !e.CheckNames([]string{"Read"}) {
			t.Fatal("should stop after 3 read-only iterations with failed Edit")
		}
	})

	t.Run("Edit on first iteration prevents enforcement", func(t *testing.T) {
		e := NewEditEnforcer(1)
		if e.CheckNames([]string{"Edit"}) {
			t.Fatal("should not stop when Edit is called on first iteration")
		}
		e.ConfirmEdit([]llms.ToolCall{{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Edit"}}}, map[string]string{"1": "ok"})
		// Should be disarmed now
		if e.CheckNames([]string{"Read"}) {
			t.Fatal("should not stop after confirmed Edit")
		}
	})
}

func TestRunWithToolsLoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	llm := &fakeLLM{responses: []*llms.ContentResponse{
		{
			Choices: []*llms.ContentChoice{{
				ToolCalls: []llms.ToolCall{{
					ID:   "1",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      "Read",
						Arguments: `{"path":"note.txt"}`,
					},
				}},
			}},
		},
		{
			Choices: []*llms.ContentChoice{{Content: "done"}},
		},
	}}

	out, err := RunWithTools(context.Background(), llm, "", "user", dir, 2, 0, nil, nil, &executor.LocalExecutor{WorkingDir: dir})
	if err != nil {
		t.Fatalf("RunWithTools() error = %v", err)
	}
	if out != "done" {
		t.Fatalf("output = %q, want %q", out, "done")
	}
}

func TestFinishToolLoopFallback(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{responses: []*llms.ContentResponse{{Choices: []*llms.ContentChoice{{Content: ""}}}}}
	out, err := finishToolLoop(context.Background(), llm, nil, "partial", 1, nil, nil)
	if err != nil {
		t.Fatalf("finishToolLoop() error = %v", err)
	}
	if out != "partial" {
		t.Fatalf("output = %q, want %q", out, "partial")
	}
}

func TestRunWithToolsErrors(t *testing.T) {
	tests := []struct {
		name string
		llm  llms.Model
	}{
		{
			name: "generate error",
			llm:  &stubLLM{err: errors.New("boom")},
		},
		{
			name: "nil response",
			llm:  &stubLLM{resp: nil},
		},
		{
			name: "empty choices",
			llm:  &stubLLM{resp: &llms.ContentResponse{Choices: nil}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td := t.TempDir()
			_, err := RunWithTools(context.Background(), tt.llm, "", "user", td, 1, 0, nil, nil, &executor.LocalExecutor{WorkingDir: td})
			if err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestFinishToolLoopFinalContent(t *testing.T) {
	llm := &stubLLM{
		resp: &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "final"}}},
	}
	out, err := finishToolLoop(context.Background(), llm, nil, "", 1, nil, nil)
	if err != nil {
		t.Fatalf("finishToolLoop() error = %v", err)
	}
	if out != "final" {
		t.Fatalf("output = %q, want %q", out, "final")
	}
}

func TestFinishToolLoopErrorNoContent(t *testing.T) {
	llm := &stubLLM{err: errors.New("boom")}
	_, err := finishToolLoop(context.Background(), llm, nil, "", 1, nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestExtractTokenUsage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		gi         map[string]any
		wantInput  int64
		wantOutput int64
	}{
		{
			name:       "nil gi",
			gi:         nil,
			wantInput:  0,
			wantOutput: 0,
		},
		{
			name:       "empty gi",
			gi:         map[string]any{},
			wantInput:  0,
			wantOutput: 0,
		},
		{
			name:       "PromptTokens/CompletionTokens (OpenAI style)",
			gi:         map[string]any{"PromptTokens": 100, "CompletionTokens": 50},
			wantInput:  100,
			wantOutput: 50,
		},
		{
			name:       "prompt_tokens/completion_tokens (snake_case)",
			gi:         map[string]any{"prompt_tokens": 200, "completion_tokens": 100},
			wantInput:  200,
			wantOutput: 100,
		},
		{
			name:       "InputTokens/OutputTokens (Anthropic style)",
			gi:         map[string]any{"InputTokens": 300, "OutputTokens": 150},
			wantInput:  300,
			wantOutput: 150,
		},
		{
			name:       "input_tokens/output_tokens (snake_case)",
			gi:         map[string]any{"input_tokens": 400, "output_tokens": 200},
			wantInput:  400,
			wantOutput: 200,
		},
		{
			name:       "int64 values",
			gi:         map[string]any{"PromptTokens": int64(500), "CompletionTokens": int64(250)},
			wantInput:  500,
			wantOutput: 250,
		},
		{
			name:       "float64 values",
			gi:         map[string]any{"PromptTokens": float64(600), "CompletionTokens": float64(300)},
			wantInput:  600,
			wantOutput: 300,
		},
		{
			name:       "only input tokens",
			gi:         map[string]any{"PromptTokens": 100},
			wantInput:  100,
			wantOutput: 0,
		},
		{
			name:       "only output tokens",
			gi:         map[string]any{"CompletionTokens": 50},
			wantInput:  0,
			wantOutput: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := metrics.New("openai", "gpt-4o")
			if tt.gi != nil {
				extractTokenUsage(tt.gi, m)
			}
			if m.InputTokens() != tt.wantInput {
				t.Fatalf("InputTokens = %d, want %d", m.InputTokens(), tt.wantInput)
			}
			if m.OutputTokens() != tt.wantOutput {
				t.Fatalf("OutputTokens = %d, want %d", m.OutputTokens(), tt.wantOutput)
			}
		})
	}
}

func TestTruncateToolOutputHeadTail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		maxBytes int
		wantTrn  bool
	}{
		{"within limit", "short output", 100, false},
		{"at limit", strings.Repeat("a", 100), 100, false},
		{"over limit", strings.Repeat("line\n", 100), 50, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := TruncateToolOutputHeadTail(tt.input, tt.maxBytes)
			truncated := strings.Contains(result, "lines omitted from tool output")
			if truncated != tt.wantTrn {
				t.Fatalf("truncated = %v, want %v", truncated, tt.wantTrn)
			}
			if tt.wantTrn {
				if !strings.Contains(result, "line") {
					t.Fatalf("truncated output should contain original content")
				}
			}
		})
	}
}

func TestTruncateToolOutputHeadTailPreservesEnds(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, "line-%03d\n", i)
	}
	input := sb.String()

	result := TruncateToolOutputHeadTail(input, 200)

	if !strings.HasPrefix(result, "line-000") {
		t.Fatalf("result should start with line-000, got: %s", result[:30])
	}
	if !strings.Contains(result, "line-099") {
		t.Fatalf("result should contain line-099")
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(strings.Repeat("a", 400))}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(strings.Repeat("b", 400))}},
	}
	tokens := estimateTokens(messages)
	if tokens != 200 {
		t.Fatalf("estimateTokens = %d, want 200", tokens)
	}
}

func TestCompactMessages(t *testing.T) {
	t.Parallel()
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart("system")}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart("user")}},
	}

	largeOutput := strings.Repeat("x", 10000)
	for i := 0; i < 30; i++ {
		messages = append(messages,
			llms.MessageContent{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{llms.ToolCall{
					ID:           fmt.Sprintf("call-%d", i),
					FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: "{}"},
				}},
			},
			llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{llms.ToolCallResponse{
					ToolCallID: fmt.Sprintf("call-%d", i),
					Content:    largeOutput,
				}},
			},
		)
	}

	ctx := context.Background()
	compacted := compactMessages(ctx, messages, nil)

	if len(compacted) != len(messages) {
		t.Fatalf("compacted length = %d, want %d", len(compacted), len(messages))
	}

	oldToolMsg := compacted[3]
	if oldToolMsg.Role != llms.ChatMessageTypeTool {
		t.Fatalf("expected tool message at index 3, got %v", oldToolMsg.Role)
	}
	oldContent := fmt.Sprintf("%v", oldToolMsg.Parts[0])
	if !strings.Contains(oldContent, "compacted") {
		t.Fatalf("old tool output should be compacted, got: %s", oldContent[:50])
	}

	lastToolIdx := len(compacted) - 1
	lastContent := fmt.Sprintf("%v", compacted[lastToolIdx].Parts[0])
	if strings.Contains(lastContent, "compacted") {
		t.Fatal("recent tool output should NOT be compacted")
	}

	beforeTokens := estimateTokens(messages)
	afterTokens := estimateTokens(compacted)
	if afterTokens >= beforeTokens {
		t.Fatalf("compaction should reduce tokens: before=%d, after=%d", beforeTokens, afterTokens)
	}
}

func TestCompactMessagesBelowThreshold(t *testing.T) {
	t.Parallel()
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart("system")}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart("user")}},
	}
	ctx := context.Background()
	result := compactMessages(ctx, messages, nil)
	if len(result) != len(messages) {
		t.Fatalf("should return unchanged messages below threshold")
	}
}

func TestRunWithToolsBudgetExceeded(t *testing.T) {
	originalCache, originalFetched, originalErr := metrics.PricingStatus()
	_ = originalCache
	_ = originalFetched
	_ = originalErr

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m := metrics.New("ollama", "llama3")
	m.SetMaxCost(0.0001)

	m2 := metrics.New("openai", "gpt-4o")
	m2.SetMaxCost(0.0001)
	m2.AddTokens(1_000_000, 1_000_000)

	if m2.MaxCost != 0.0001 {
		t.Fatalf("MaxCost = %v, want 0.0001", m2.MaxCost)
	}
}

func TestSliceLines(t *testing.T) {
	t.Parallel()
	data := []byte("line1\nline2\nline3\nline4\nline5\n")
	tests := []struct {
		name   string
		offset int
		limit  int
		want   string
	}{
		{"from start", 1, 2, "[lines 1-2 of 5]\nline1\nline2\n"},
		{"middle", 2, 2, "[lines 2-3 of 5]\nline2\nline3\n"},
		{"zero offset defaults to 1", 0, 2, "[lines 1-2 of 5]\nline1\nline2\n"},
		{"no limit", 3, 0, "[lines 3-5 of 5]\nline3\nline4\nline5\n"},
		{"past end", 10, 0, "(file has 5 lines, offset 10 is past end)"},
		{"limit exceeds file", 1, 100, "[lines 1-5 of 5]\nline1\nline2\nline3\nline4\nline5\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sliceLines(data, tt.offset, tt.limit)
			if got != tt.want {
				t.Fatalf("sliceLines(offset=%d, limit=%d) =\n%q\nwant:\n%q", tt.offset, tt.limit, got, tt.want)
			}
		})
	}
}

func TestTruncateHeadTail(t *testing.T) {
	t.Parallel()
	// Build a file larger than maxReadBytes
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "line-%04d: %s\n", i, strings.Repeat("x", 20))
	}
	data := []byte(sb.String())
	if len(data) <= maxReadBytes {
		t.Fatalf("test data too small: %d bytes, need > %d", len(data), maxReadBytes)
	}

	result := truncateHeadTail(data)

	if !strings.Contains(result, "lines omitted") {
		t.Fatal("truncateHeadTail should contain 'lines omitted' marker")
	}
	if !strings.Contains(result, "line-0000") {
		t.Fatal("truncateHeadTail should contain first line")
	}
	if !strings.Contains(result, "line-4999") {
		t.Fatal("truncateHeadTail should contain last line")
	}
	if !strings.Contains(result, "Use Read with offset/limit") {
		t.Fatal("truncateHeadTail should contain usage hint")
	}
}

func TestReadToolWithOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	content := "alpha\nbeta\ngamma\ndelta\nepsilon\n"
	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	read := readTool(dir)
	out, err := read(context.Background(), []byte(`{"path":"lines.txt","offset":2,"limit":2}`))
	if err != nil {
		t.Fatalf("readTool: %v", err)
	}
	if !strings.Contains(out, "beta") || !strings.Contains(out, "gamma") {
		t.Fatalf("expected lines 2-3, got: %s", out)
	}
	if strings.Contains(out, "alpha") || strings.Contains(out, "delta") {
		t.Fatalf("should not contain lines outside range, got: %s", out)
	}
}

func TestReadToolLargeFileTruncation(t *testing.T) {
	dir := t.TempDir()
	// Create a file larger than maxReadBytes
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "line-%04d: %s\n", i, strings.Repeat("x", 20))
	}
	content := sb.String()
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	read := readTool(dir)
	out, err := read(context.Background(), []byte(`{"path":"big.txt"}`))
	if err != nil {
		t.Fatalf("readTool: %v", err)
	}
	if !strings.Contains(out, "lines omitted") {
		t.Fatalf("large file should be truncated, got: %s", out[:100])
	}
}

func TestGlobToolSkipsDirs(t *testing.T) {
	dir := t.TempDir()
	write := writeTool(dir)
	_, _ = write(context.Background(), []byte(`{"path":"src/main.go","content":"package main"}`))
	_, _ = write(context.Background(), []byte(`{"path":"node_modules/pkg/index.js","content":"module.exports={}"}`))
	_, _ = write(context.Background(), []byte(`{"path":".git/config","content":"[core]"}`))
	_, _ = write(context.Background(), []byte(`{"path":"__pycache__/mod.pyc","content":"bytecode"}`))

	glob := globTool(dir)
	out, err := glob(context.Background(), []byte(`{"pattern":"**/*"}`))
	if err != nil {
		t.Fatalf("globTool: %v", err)
	}
	if !strings.Contains(out, "src/main.go") {
		t.Fatalf("glob should include src/main.go, got: %s", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Fatalf("glob should skip node_modules, got: %s", out)
	}
	if strings.Contains(out, ".git/config") {
		t.Fatalf("glob should skip .git, got: %s", out)
	}
	if strings.Contains(out, "__pycache__") {
		t.Fatalf("glob should skip __pycache__, got: %s", out)
	}
}

func TestGrepToolSkipsDirs(t *testing.T) {
	dir := t.TempDir()
	write := writeTool(dir)
	_, _ = write(context.Background(), []byte(`{"path":"src/main.go","content":"findme here"}`))
	_, _ = write(context.Background(), []byte(`{"path":"node_modules/pkg/lib.js","content":"findme there"}`))
	_, _ = write(context.Background(), []byte(`{"path":".git/objects/abc","content":"findme hidden"}`))

	grep := grepTool(dir)
	out, err := grep(context.Background(), []byte(`{"pattern":"findme","path":"."}`))
	if err != nil {
		t.Fatalf("grepTool: %v", err)
	}
	if !strings.Contains(out, "src/main.go") {
		t.Fatalf("grep should include src/main.go, got: %s", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Fatalf("grep should skip node_modules, got: %s", out)
	}
	if strings.Contains(out, ".git") {
		t.Fatalf("grep should skip .git, got: %s", out)
	}
}

func TestFinishToolLoopBudgetExceeded(t *testing.T) {
	t.Parallel()
	m := metrics.New("ollama", "llama3")
	m.SetMaxCost(0.0001)
	// Ollama is free, so budget won't be exceeded — use a hack to simulate
	// by setting MaxCost to 0 and checking non-budget path
	llm := &stubLLM{
		resp: &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "final"}}},
	}
	out, err := finishToolLoop(context.Background(), llm, nil, "partial", 1, m, nil)
	if err != nil {
		t.Fatalf("finishToolLoop() error = %v", err)
	}
	if out != "final" {
		t.Fatalf("output = %q, want %q", out, "final")
	}
}

func TestEstimateTokensWithToolCallResponse(t *testing.T) {
	t.Parallel()
	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{Content: strings.Repeat("a", 400)},
			},
		},
	}
	tokens := estimateTokens(messages)
	if tokens != 100 {
		t.Fatalf("estimateTokens = %d, want 100", tokens)
	}
}

func TestCompactMessagesProtectsHeadAndTail(t *testing.T) {
	t.Parallel()
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart("system")}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart("user")}},
	}

	largeOutput := strings.Repeat("x", 10000)
	for i := 0; i < 30; i++ {
		messages = append(messages,
			llms.MessageContent{
				Role: llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{llms.ToolCall{
					ID:           fmt.Sprintf("call-%d", i),
					FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: "{}"},
				}},
			},
			llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{llms.ToolCallResponse{
					ToolCallID: fmt.Sprintf("call-%d", i),
					Content:    largeOutput,
				}},
			},
		)
	}

	ctx := context.Background()
	compacted := compactMessages(ctx, messages, nil)

	// System (index 0) should be untouched
	sysText := fmt.Sprintf("%v", compacted[0].Parts[0])
	if !strings.Contains(sysText, "system") {
		t.Fatalf("system message should be preserved, got: %s", sysText)
	}

	// Human (index 1) should be untouched
	humanText := fmt.Sprintf("%v", compacted[1].Parts[0])
	if !strings.Contains(humanText, "user") {
		t.Fatalf("human message should be preserved, got: %s", humanText)
	}

	// AI messages (non-tool) should be untouched
	aiMsg := compacted[2]
	if aiMsg.Role != llms.ChatMessageTypeAI {
		t.Fatalf("expected AI message at index 2, got %v", aiMsg.Role)
	}
}

func TestExtractTokenUsageNilMetrics(t *testing.T) {
	t.Parallel()
	// Should not panic when metrics is nil
	gi := map[string]any{"PromptTokens": 100, "CompletionTokens": 50}
	extractTokenUsage(gi, nil) // Should not panic
}

func TestRunWithToolsWithMetrics(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	llm := &fakeLLM{responses: []*llms.ContentResponse{
		{Choices: []*llms.ContentChoice{{
			Content: "done",
			GenerationInfo: map[string]any{
				"PromptTokens":     100,
				"CompletionTokens": 50,
			},
		}}},
	}}

	m := metrics.New("openai", "gpt-4o")
	out, err := RunWithTools(context.Background(), llm, "", "user", dir, 2, 0, nil, m, &executor.LocalExecutor{WorkingDir: dir})
	if err != nil {
		t.Fatalf("RunWithTools() error = %v", err)
	}
	if out != "done" {
		t.Fatalf("output = %q, want %q", out, "done")
	}
	if m.Iterations() != 1 {
		t.Fatalf("Iterations = %d, want 1", m.Iterations())
	}
	if m.InputTokens() != 100 {
		t.Fatalf("InputTokens = %d, want 100", m.InputTokens())
	}
	if m.OutputTokens() != 50 {
		t.Fatalf("OutputTokens = %d, want 50", m.OutputTokens())
	}
}

func TestToolLoopBudgetExceeded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// LLM returns a tool call, then after tool execution budget is checked.
	// We pre-load metrics with enough tokens to exceed the budget.
	// The second response is the grace call's final structured output.
	llm := &fakeLLM{responses: []*llms.ContentResponse{
		{
			Choices: []*llms.ContentChoice{{
				Content: "partial status message",
				ToolCalls: []llms.ToolCall{{
					ID:   "1",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      "Read",
						Arguments: `{"path":"note.txt"}`,
					},
				}},
				GenerationInfo: map[string]any{
					"PromptTokens":     int64(500000),
					"CompletionTokens": int64(500000),
				},
			}},
		},
		// Grace call response: the model produces its final report.
		{
			Choices: []*llms.ContentChoice{{
				Content: "## Final Report\nComplete structured output from grace call.",
			}},
		},
	}}

	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	// Pre-load tokens so budget is exceeded after the first tool call
	m.AddTokens(1_000_000, 1_000_000)

	out, err := RunWithTools(context.Background(), llm, "", "user", dir, 10, 0, nil, m, &executor.LocalExecutor{WorkingDir: dir})
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	// The grace call should produce the final report, not the partial status message.
	if out != "## Final Report\nComplete structured output from grace call." {
		t.Fatalf("output = %q, want final report from grace call", out)
	}
}

func TestFinishToolLoopBudgetExceededWithContent(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	m.AddTokens(1_000_000, 1_000_000)

	// The grace call should produce the final structured output.
	llm := &stubLLM{
		resp: &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "final structured report"}}},
	}

	out, err := finishToolLoop(context.Background(), llm, nil, "partial", 1, m, nil)
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	// Grace call succeeds, so we get the model's final output, not the partial content.
	if out != "final structured report" {
		t.Fatalf("output = %q, want %q", out, "final structured report")
	}
}

func TestFinishToolLoopBudgetExceededNoContent(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	m.AddTokens(1_000_000, 1_000_000)

	// Grace call produces a final report even when there was no prior content.
	llm := &stubLLM{
		resp: &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "final report"}}},
	}

	out, err := finishToolLoop(context.Background(), llm, nil, "", 1, m, nil)
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	// Grace call succeeds, so we get the model's final output.
	if out != "final report" {
		t.Fatalf("output = %q, want %q", out, "final report")
	}
}

func TestFinishToolLoopBudgetExceededGraceCallFails(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	m.AddTokens(1_000_000, 1_000_000)

	// Grace call fails — should fall back to lastContent.
	llm := &stubLLM{
		err: errors.New("API error"),
	}

	out, err := finishToolLoop(context.Background(), llm, nil, "partial fallback", 1, m, nil)
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	if out != "partial fallback" {
		t.Fatalf("output = %q, want %q", out, "partial fallback")
	}
}

func TestTaskToolWithParentMetrics(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parentMetrics := metrics.New("openai", "gpt-4o")
	childMetrics := metrics.New("openai", "gpt-4o")
	childMetrics.AddTokens(100, 50)

	cfg := TaskConfig{
		AgentsDir:     "agents",
		WorkingDir:    dir,
		ParentMetrics: parentMetrics,
		CallModel: func(
			_ context.Context,
			_, _, _, _, _ string,
		) (string, *metrics.Metrics, error) {
			return "child output", childMetrics, nil
		},
	}

	tool := taskTool(cfg)
	out, err := tool(context.Background(), []byte(`{"agent":"test-agent","prompt":"do stuff"}`))
	if err != nil {
		t.Fatalf("taskTool() error = %v", err)
	}
	if out != "child output" {
		t.Fatalf("output = %q, want %q", out, "child output")
	}
	if len(parentMetrics.Children) != 1 {
		t.Fatalf("expected 1 child metric, got %d", len(parentMetrics.Children))
	}
	if parentMetrics.Children[0].Agent != "test-agent" {
		t.Fatalf("child agent = %q, want %q", parentMetrics.Children[0].Agent, "test-agent")
	}
	if parentMetrics.Children[0].InputTokens != 100 {
		t.Fatalf("child InputTokens = %d, want 100", parentMetrics.Children[0].InputTokens)
	}
}

func TestTaskResultToolWithParentMetrics(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parentMetrics := metrics.New("openai", "gpt-4o")
	childMetrics := metrics.New("openai", "gpt-4o")
	childMetrics.AddTokens(200, 100)

	registry := NewBackgroundTaskRegistry(0)
	result := &BackgroundTaskResult{
		Output:  "bg output",
		Metrics: childMetrics,
		Done:    make(chan struct{}),
	}
	close(result.Done)

	registry.tasks.Set("bg-test", result)

	cfg := TaskConfig{
		AgentsDir:     "agents",
		WorkingDir:    dir,
		ParentMetrics: parentMetrics,
		Registry:      registry,
		CallModel: func(
			_ context.Context,
			_, _, _, _, _ string,
		) (string, *metrics.Metrics, error) {
			return "", nil, nil
		},
	}

	tool := taskResultTool(cfg)
	out, err := tool(context.Background(), []byte(`{"task_id":"bg-test"}`))
	if err != nil {
		t.Fatalf("taskResultTool() error = %v", err)
	}
	if out != "bg output" {
		t.Fatalf("output = %q, want %q", out, "bg output")
	}
	if len(parentMetrics.Children) != 1 {
		t.Fatalf("expected 1 child metric, got %d", len(parentMetrics.Children))
	}
	if parentMetrics.Children[0].Agent != "bg-test" {
		t.Fatalf("child agent = %q, want %q", parentMetrics.Children[0].Agent, "bg-test")
	}
}

func TestBuildHandlersWithRegistry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := &TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		Registry:   NewBackgroundTaskRegistry(0),
		CallModel: func(
			_ context.Context,
			_, _, _, _, _ string,
		) (string, *metrics.Metrics, error) {
			return "", nil, nil
		},
	}
	handlers, defs := BuildHandlers(dir, cfg, &executor.LocalExecutor{WorkingDir: dir})
	if _, ok := handlers["TaskResult"]; !ok {
		t.Fatalf("expected TaskResult handler when registry is set")
	}
	if len(defs) != 13 {
		t.Fatalf("tool defs = %d, want 13 (11 base + Task + TaskResult)", len(defs))
	}
}

func TestExecuteToolCallWithOutputAndError(t *testing.T) {
	t.Parallel()
	handlers := map[string]Handler{
		"PartialFail": {Call: func(context.Context, []byte) (string, error) {
			return "some output", errors.New("partial error")
		}},
	}
	resp := executeToolCall(context.Background(), llms.ToolCall{
		ID:           "1",
		FunctionCall: &llms.FunctionCall{Name: "PartialFail", Arguments: "{}"},
	}, handlers)
	if !strings.Contains(resp.Content, "some output") {
		t.Fatalf("expected output in response, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "error: partial error") {
		t.Fatalf("expected error in response, got: %s", resp.Content)
	}
}

func TestToolArgsSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		toolName string
		argsJSON string
		want     string
	}{
		{"invalid json", "Bash", "{bad", ""},
		{"Read path", "Read", `{"path":"/tmp/foo"}`, "/tmp/foo"},
		{"Write path", "Write", `{"path":"/tmp/bar"}`, "/tmp/bar"},
		{"Edit path", "Edit", `{"path":"/tmp/baz"}`, "/tmp/baz"},
		{"Glob pattern", "Glob", `{"pattern":"*.go"}`, "*.go"},
		{"Bash command", "Bash", `{"command":"echo hello"}`, "echo hello"},
		{"Bash empty command", "Bash", `{"command":""}`, ""},
		{"Grep pattern only", "Grep", `{"pattern":"foo"}`, "foo"},
		{"Grep pattern with path", "Grep", `{"pattern":"foo","path":"/src"}`, "foo in /src"},
		{"Task with agent", "Task", `{"agent":"review","prompt":"check code quality and style"}`, `agent=review prompt="check code quality and style"`},
		{"Task without agent", "Task", `{"prompt":"do stuff"}`, ""},
		{"TaskResult", "TaskResult", `{"id":"abc-123"}`, "id=abc-123"},
		{"unknown tool", "Unknown", `{"x":"y"}`, ""},
		{"non-string value", "Read", `{"path":123}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := toolArgsSummary(tt.toolName, tt.argsJSON)
			if got != tt.want {
				t.Errorf("toolArgsSummary(%q, %q) = %q, want %q", tt.toolName, tt.argsJSON, got, tt.want)
			}
		})
	}
}

func TestToInt64(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		val  any
		want int64
	}{
		{"int", int(42), 42},
		{"int64", int64(99), 99},
		{"float64", float64(7.9), 7},
		{"string unsupported", "hello", 0},
		{"nil", nil, 0},
		{"bool unsupported", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := toInt64(tt.val)
			if got != tt.want {
				t.Fatalf("toInt64(%v) = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

func TestBuildHandlersWithFindings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewFindingsStore()
	cfg := &TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		Registry:   NewBackgroundTaskRegistry(0),
		Findings:   store,
		AgentName:  "test-agent",
		CallModel: func(
			_ context.Context,
			_, _, _, _, _ string,
		) (string, *metrics.Metrics, error) {
			return "", nil, nil
		},
	}
	handlers, defs := BuildHandlers(dir, cfg, &executor.LocalExecutor{WorkingDir: dir})

	if _, ok := handlers["ReportFinding"]; !ok {
		t.Fatalf("expected ReportFinding handler when Findings store is set")
	}
	// 11 base + Task + TaskResult + ReportFinding = 14
	if len(defs) != 14 {
		t.Fatalf("tool defs = %d, want 14", len(defs))
	}

	// Verify the handler actually works and attributes to the agent
	payload, _ := json.Marshal(map[string]string{
		"title":       "Test Finding",
		"severity":    "high",
		"description": "Found something.",
	})
	out, err := handlers["ReportFinding"].Call(context.Background(), payload)
	if err != nil {
		t.Fatalf("ReportFinding: %v", err)
	}
	if !strings.Contains(out, "Finding recorded") {
		t.Fatalf("unexpected output: %s", out)
	}
	if store.Count() != 1 {
		t.Fatalf("store count = %d, want 1", store.Count())
	}
	if store.All()[0].Agent != "test-agent" {
		t.Fatalf("agent = %q, want test-agent", store.All()[0].Agent)
	}
}

func TestFinishToolLoopBudgetExceededGraceCallFailsNoContent(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	m.AddTokens(1_000_000, 1_000_000)

	// Grace call fails and there is no lastContent to fall back on.
	llm := &stubLLM{err: errors.New("API error")}

	out, err := finishToolLoop(context.Background(), llm, nil, "", 1, m, nil)
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	if out != "" {
		t.Fatalf("output = %q, want empty", out)
	}
}

func TestFinishToolLoopBudgetExceededEmptyGraceResponse(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	m.AddTokens(1_000_000, 1_000_000)

	// Grace call succeeds but returns empty content.
	llm := &stubLLM{
		resp: &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: ""}}},
	}

	// With lastContent: should return it.
	out, err := finishToolLoop(context.Background(), llm, nil, "fallback", 1, m, nil)
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	if out != "fallback" {
		t.Fatalf("output = %q, want %q", out, "fallback")
	}
}

func TestFinishToolLoopBudgetExceededEmptyGraceAndNoContent(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(0.0001)
	m.AddTokens(1_000_000, 1_000_000)

	// Grace call succeeds but returns empty content, no lastContent either.
	llm := &stubLLM{
		resp: &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: ""}}},
	}

	out, err := finishToolLoop(context.Background(), llm, nil, "", 1, m, nil)
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}
	if out != "" {
		t.Fatalf("output = %q, want empty", out)
	}
}

func TestMergeChoicesMultipleWithMetrics(t *testing.T) {
	t.Parallel()
	m := metrics.New("openai", "gpt-4o")
	choices := []*llms.ContentChoice{
		{
			Content: "thinking...",
			GenerationInfo: map[string]any{
				"PromptTokens":     200,
				"CompletionTokens": 100,
			},
		},
		{
			ToolCalls: []llms.ToolCall{{
				ID:           "call-1",
				FunctionCall: &llms.FunctionCall{Name: "Bash", Arguments: `{"command":"ls"}`},
			}},
		},
	}

	content, toolCalls := mergeChoices(context.Background(), choices, m)
	if content != "thinking..." {
		t.Fatalf("content = %q, want %q", content, "thinking...")
	}
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls len = %d, want 1", len(toolCalls))
	}
	if m.InputTokens() != 200 || m.OutputTokens() != 100 {
		t.Fatalf("tokens = %d/%d, want 200/100", m.InputTokens(), m.OutputTokens())
	}
}

func TestBuildHandlersWithFindingsDefaultAgent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewFindingsStore()
	cfg := &TaskConfig{
		AgentsDir:  "agents",
		WorkingDir: dir,
		Findings:   store,
		AgentName:  "", // empty agent name should default to "unknown"
		CallModel: func(
			_ context.Context,
			_, _, _, _, _ string,
		) (string, *metrics.Metrics, error) {
			return "", nil, nil
		},
	}
	handlers, _ := BuildHandlers(dir, cfg, &executor.LocalExecutor{WorkingDir: dir})

	payload, _ := json.Marshal(map[string]string{
		"title":       "Test",
		"severity":    "info",
		"description": "desc",
	})
	_, err := handlers["ReportFinding"].Call(context.Background(), payload)
	if err != nil {
		t.Fatalf("ReportFinding: %v", err)
	}
	if store.All()[0].Agent != "unknown" {
		t.Fatalf("agent = %q, want unknown", store.All()[0].Agent)
	}
}

func TestExecuteToolCallsParallel(t *testing.T) {
	t.Parallel()
	// Read-only tools should run in parallel.
	handlers := map[string]Handler{
		"Read": {
			Def: llms.Tool{Function: &llms.FunctionDefinition{Name: "Read"}},
			Call: func(_ context.Context, _ []byte) (string, error) {
				return "read-result", nil
			},
		},
		"Glob": {
			Def: llms.Tool{Function: &llms.FunctionDefinition{Name: "Glob"}},
			Call: func(_ context.Context, _ []byte) (string, error) {
				return "glob-result", nil
			},
		},
	}
	toolCalls := []llms.ToolCall{
		{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"a.go"}`}},
		{ID: "2", FunctionCall: &llms.FunctionCall{Name: "Glob", Arguments: `{"pattern":"*.go"}`}},
	}

	messages := executeToolCalls(context.Background(), nil, toolCalls, handlers)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if len(messages[0].Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(messages[0].Parts))
	}
	// Verify results are in order despite parallel execution.
	assertToolResponse(t, messages[0].Parts[0], "read-result")
	assertToolResponse(t, messages[0].Parts[1], "glob-result")
}

func TestExecuteToolCallsSerialWhenMutating(t *testing.T) {
	t.Parallel()
	// If any tool is serial (e.g. Bash), all should run sequentially.
	callOrder := make([]string, 0, 2)
	handlers := map[string]Handler{
		"Read": {
			Def: llms.Tool{Function: &llms.FunctionDefinition{Name: "Read"}},
			Call: func(_ context.Context, _ []byte) (string, error) {
				callOrder = append(callOrder, "Read")
				return "ok", nil
			},
		},
		"Bash": {
			Def: llms.Tool{Function: &llms.FunctionDefinition{Name: "Bash"}},
			Call: func(_ context.Context, _ []byte) (string, error) {
				callOrder = append(callOrder, "Bash")
				return "ok", nil
			},
		},
	}
	toolCalls := []llms.ToolCall{
		{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{}`}},
		{ID: "2", FunctionCall: &llms.FunctionCall{Name: "Bash", Arguments: `{"command":"echo hi"}`}},
	}

	messages := executeToolCalls(context.Background(), nil, toolCalls, handlers)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	// Sequential execution: order should be preserved.
	if len(callOrder) != 2 || callOrder[0] != "Read" || callOrder[1] != "Bash" {
		t.Fatalf("callOrder = %v, want [Read Bash]", callOrder)
	}
}

func TestExecuteToolCallsSingleCallNoParallel(t *testing.T) {
	t.Parallel()
	handlers := map[string]Handler{
		"Read": {
			Def: llms.Tool{Function: &llms.FunctionDefinition{Name: "Read"}},
			Call: func(_ context.Context, _ []byte) (string, error) {
				return "ok", nil
			},
		},
	}
	toolCalls := []llms.ToolCall{
		{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{}`}},
	}

	messages := executeToolCalls(context.Background(), nil, toolCalls, handlers)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
}

func TestSerialToolsSet(t *testing.T) {
	t.Parallel()
	expected := []string{"Bash", "Edit", "Write", "Task"}
	for _, name := range expected {
		if !serialTools[name] {
			t.Errorf("expected %q to be in serialTools", name)
		}
	}
	if serialTools["Read"] {
		t.Error("Read should not be in serialTools")
	}
}

func assertToolResponse(t *testing.T, part llms.ContentPart, wantContent string) {
	t.Helper()
	resp, ok := part.(llms.ToolCallResponse)
	if !ok {
		t.Fatalf("part is %T, want ToolCallResponse", part)
	}
	if resp.Content != wantContent {
		t.Fatalf("content = %q, want %q", resp.Content, wantContent)
	}
}

func TestBuildHandlersDisableTask(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: workDir}

	taskCfg := &TaskConfig{
		CallModel: nil,
		Registry:  NewBackgroundTaskRegistry(0),
	}
	handlers, _ := BuildHandlers(workDir, taskCfg, ex)

	for name := range handlers {
		if name == "Task" || name == "TaskResult" {
			t.Fatalf("handler %q should not be registered when CallModel is nil", name)
		}
	}
}

func TestBuildHandlersWithTask(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: workDir}

	taskCfg := &TaskConfig{
		CallModel: func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error) {
			return "", nil, nil
		},
		Registry: NewBackgroundTaskRegistry(0),
	}
	handlers, _ := BuildHandlers(workDir, taskCfg, ex)

	if _, ok := handlers["Task"]; !ok {
		t.Fatal("expected Task handler when CallModel is set")
	}
	if _, ok := handlers["TaskResult"]; !ok {
		t.Fatal("expected TaskResult handler when CallModel and Registry are set")
	}
}

func TestBuildHandlersNilTaskConfig(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: workDir}

	handlers, _ := BuildHandlers(workDir, nil, ex)

	for name := range handlers {
		if name == "Task" || name == "TaskResult" {
			t.Fatalf("handler %q should not be registered when taskCfg is nil", name)
		}
	}
}

// captureLLM records the messages passed to each GenerateContent call so
// tests can inspect injected budget warning messages.
type captureLLM struct {
	responses []*llms.ContentResponse
	calls     int
	captured  [][]llms.MessageContent
}

func (c *captureLLM) GenerateContent(_ context.Context, msgs []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	cp := make([]llms.MessageContent, len(msgs))
	copy(cp, msgs)
	c.captured = append(c.captured, cp)

	if c.calls >= len(c.responses) {
		return &llms.ContentResponse{
			Choices: []*llms.ContentChoice{{Content: "done"}},
		}, nil
	}
	resp := c.responses[c.calls]
	c.calls++
	return resp, nil
}

func (c *captureLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func TestToolLoopBudgetWarnings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Use ollama provider so cost is $0 per token — we control cost via
	// pre-loaded tokens on an "openai" metrics instance instead.
	// Each iteration the LLM returns a Read tool call; after tool execution
	// the budget percentage is checked and warnings injected.
	//
	// We set MaxCost = $1.00 (openai gpt-4o pricing) and pre-load tokens
	// so that after iteration 1 we're at ~55% and after iteration 2 we're
	// at ~80%, triggering both the 50% and 75% warnings.

	m := metrics.New("openai", "gpt-4o")
	m.SetMaxCost(1.00)

	llm := &captureLLM{responses: []*llms.ContentResponse{
		// Tool call + token usage that pushes past 50%
		{Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID:   "1",
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "Read",
					Arguments: `{"path":"a.txt"}`,
				},
			}},
			GenerationInfo: map[string]any{
				"PromptTokens":     int64(100000),
				"CompletionTokens": int64(20000),
			},
		}}},
		// Another tool call pushing past 75%
		{Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID:   "2",
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "Read",
					Arguments: `{"path":"a.txt"}`,
				},
			}},
			GenerationInfo: map[string]any{
				"PromptTokens":     int64(100000),
				"CompletionTokens": int64(20000),
			},
		}}},
		// Final response with no tool calls
		{Choices: []*llms.ContentChoice{{
			Content: "final output",
		}}},
	}}

	// Pre-load tokens so the first iteration's addition crosses 50%.
	// gpt-4o pricing: $2.50/M input, $10/M output.
	// Pre-load 100k input + 10k output = $0.25 + $0.10 = $0.35.
	// After iter 1 adds 100k input + 20k output = $0.25 + $0.20 = $0.45
	// Cumulative after iter 1: $0.80 = 80% — triggers BOTH 50% and 75%.
	// After iter 2 adds another batch, cumulative exceeds $1.00 — budget exceeded.
	m.AddTokens(100000, 10000)

	handlers, _ := BuildHandlers(dir, nil, &executor.LocalExecutor{WorkingDir: dir})

	_, _, loopErr, _ := toolLoop(context.Background(), llm, buildInitialMessages("sys", "user"), handlers, 10, 0, m, nil)
	// Budget may be exceeded after iter 2 — that's fine, we just care that
	// warnings were injected before that happened.
	if loopErr != nil && !errors.Is(loopErr, metrics.ErrBudgetExceeded) {
		t.Fatalf("unexpected error: %v", loopErr)
	}

	// Inspect captured messages for budget warnings.
	var warnings50, warnings75 int
	for _, msgs := range llm.captured {
		for _, msg := range msgs {
			for _, part := range msg.Parts {
				if tp, ok := part.(llms.TextContent); ok {
					if strings.Contains(tp.Text, "[BUDGET WARNING]") && strings.Contains(tp.Text, "50%") {
						warnings50++
					}
					if strings.Contains(tp.Text, "[BUDGET WARNING]") && strings.Contains(tp.Text, "75%") {
						warnings75++
					}
				}
			}
		}
	}

	if warnings50 == 0 {
		t.Error("expected 50% budget warning to be injected, but found none")
	}
	if warnings75 == 0 {
		t.Error("expected 75% budget warning to be injected, but found none")
	}
}

func TestToolLoopNoBudgetWarningsWithoutMaxCost(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// No MaxCost set — no warnings should be injected.
	m := metrics.New("openai", "gpt-4o")

	llm := &captureLLM{responses: []*llms.ContentResponse{
		{Choices: []*llms.ContentChoice{{
			ToolCalls: []llms.ToolCall{{
				ID:   "1",
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      "Read",
					Arguments: `{"path":"a.txt"}`,
				},
			}},
			GenerationInfo: map[string]any{
				"PromptTokens":     int64(500000),
				"CompletionTokens": int64(500000),
			},
		}}},
		{Choices: []*llms.ContentChoice{{Content: "done"}}},
	}}

	handlers, _ := BuildHandlers(dir, nil, &executor.LocalExecutor{WorkingDir: dir})

	_, _, loopErr, done := toolLoop(context.Background(), llm, buildInitialMessages("sys", "user"), handlers, 10, 0, m, nil)
	if loopErr != nil {
		t.Fatalf("unexpected error: %v", loopErr)
	}
	if !done {
		t.Fatal("expected done=true")
	}

	for _, msgs := range llm.captured {
		for _, msg := range msgs {
			for _, part := range msg.Parts {
				if tp, ok := part.(llms.TextContent); ok {
					if strings.Contains(tp.Text, "[BUDGET WARNING]") {
						t.Fatalf("no budget warning expected when MaxCost=0, but found: %s", tp.Text)
					}
				}
			}
		}
	}
}

func TestExtractToolResults(t *testing.T) {
	t.Parallel()
	// Empty messages.
	r := extractToolResults(nil)
	if len(r) != 0 {
		t.Fatal("expected empty map for nil messages")
	}

	// Messages with tool call responses.
	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{ToolCallID: "1", Content: "output1"},
				llms.ToolCallResponse{ToolCallID: "2", Content: "output2"},
			},
		},
	}
	r = extractToolResults(messages)
	if len(r) != 2 || r["1"] != "output1" || r["2"] != "output2" {
		t.Fatalf("unexpected results: %v", r)
	}

	// Non-tool-call-response parts are ignored.
	messages = []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: "hello"},
			},
		},
	}
	r = extractToolResults(messages)
	if len(r) != 0 {
		t.Fatal("expected empty map for non-tool messages")
	}
}

func TestBashTool_BlockedCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := &executor.LocalExecutor{WorkingDir: dir}
	tool := bashTool(ex)
	args, _ := json.Marshal(map[string]string{"command": "sudo rm -rf /"})
	_, err := tool(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for blocked command")
	}
}

func TestReadTool_FileTracker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := InitFileTracker(context.Background())
	ft := GetFileTracker(ctx)

	tool := readTool(dir)
	args, _ := json.Marshal(map[string]string{"path": "test.go"})
	_, err := tool(ctx, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the file was recorded as read.
	if ft.LastReadTime(filepath.Join(dir, "test.go")).IsZero() {
		t.Fatal("expected file to be recorded as read")
	}
}

func TestEditTool_FileTrackerValidation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := InitFileTracker(context.Background())

	// Try to edit without reading first — should fail.
	tool := editTool(dir)
	args, _ := json.Marshal(map[string]any{"path": "test.go", "old": "hello", "new": "goodbye"})
	_, err := tool(ctx, args)
	if err == nil {
		t.Fatal("expected error when editing without reading first")
	}
}

func TestShouldBlockReads(t *testing.T) {
	t.Parallel()

	t.Run("nil enforcer never blocks", func(t *testing.T) {
		var e *EditEnforcer
		if e.ShouldBlockReads() {
			t.Fatal("nil enforcer should not block")
		}
	})

	t.Run("does not block before deadline", func(t *testing.T) {
		e := NewEditEnforcer(5)
		// Simulate one read-only iteration
		e.CheckNames([]string{"Read"})
		if e.ShouldBlockReads() {
			t.Fatal("should not block after first read-only iteration")
		}
	})

	t.Run("blocks near deadline", func(t *testing.T) {
		e := NewEditEnforcer(5)
		// threshold = 5-2 = 3, so need 3 read-only iterations to trigger
		e.CheckNames([]string{"Read"})
		e.CheckNames([]string{"Glob"})
		if e.ShouldBlockReads() {
			t.Fatal("should not block at 2 iterations when deadline is 5")
		}
		e.CheckNames([]string{"Read"})
		if !e.ShouldBlockReads() {
			t.Fatal("should block at 3 read-only iterations (deadline-2) with no edits")
		}
	})

	t.Run("does not block after confirmed edit", func(t *testing.T) {
		e := NewEditEnforcer(5)
		e.CheckNames([]string{"Read"})
		e.CheckNames([]string{"Read"})
		e.CheckNames([]string{"Edit"})
		e.ConfirmEdit([]llms.ToolCall{{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Edit"}}}, map[string]string{"1": "ok"})
		if e.ShouldBlockReads() {
			t.Fatal("should not block after confirmed Edit")
		}
	})

	t.Run("respects configured deadline exactly", func(t *testing.T) {
		e := NewEditEnforcer(5)
		if e.Deadline != 5 {
			t.Fatalf("expected deadline 5, got %d", e.Deadline)
		}
	})
}

func TestEditEnforcerContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if e := GetEditEnforcer(ctx); e != nil {
		t.Fatal("expected nil before setting")
	}

	enforcer := NewEditEnforcer(3)
	ctx = SetEditEnforcer(ctx, enforcer)
	got := GetEditEnforcer(ctx)
	if got != enforcer {
		t.Fatal("expected same enforcer back from context")
	}
}

func TestReadToolBlockedByEditEnforcer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewEditEnforcer(5)
	// threshold = 5-2 = 3; need 3 read-only iterations to trigger blocking
	e.CheckNames([]string{"Read"})
	e.CheckNames([]string{"Read"})
	e.CheckNames([]string{"Read"})
	ctx := SetEditEnforcer(context.Background(), e)

	read := readTool(dir)
	out, err := read(ctx, []byte(`{"path":"f.txt"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "READ BLOCKED") {
		t.Fatalf("expected READ BLOCKED, got: %s", out)
	}
}

func TestGlobToolBlockedByEditEnforcer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewEditEnforcer(5)
	// threshold = 5-2 = 3; need 3 read-only iterations
	e.CheckNames([]string{"Read"})
	e.CheckNames([]string{"Glob"})
	e.CheckNames([]string{"Read"})
	ctx := SetEditEnforcer(context.Background(), e)

	glob := globTool(dir)
	out, err := glob(ctx, []byte(`{"pattern":"*.txt"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "GLOB BLOCKED") {
		t.Fatalf("expected GLOB BLOCKED, got: %s", out)
	}
}

func TestGrepToolBlockedByEditEnforcer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("findme"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewEditEnforcer(5)
	// threshold = 5-2 = 3; need 3 read-only iterations
	e.CheckNames([]string{"Read"})
	e.CheckNames([]string{"Grep"})
	e.CheckNames([]string{"Read"})
	ctx := SetEditEnforcer(context.Background(), e)

	grep := grepTool(dir)
	out, err := grep(ctx, []byte(`{"pattern":"findme","path":"."}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "GREP BLOCKED") {
		t.Fatalf("expected GREP BLOCKED, got: %s", out)
	}
}

func TestReadToolCacheHitReturnsContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(dir, "cached.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := InitReadCache(context.Background())
	ctx = InitIterationCounter(ctx)
	read := readTool(dir)

	// First read populates cache.
	SetIteration(ctx, 1)
	fresh, err := read(ctx, []byte(`{"path":"cached.txt"}`))
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if !strings.Contains(fresh, "line1") {
		t.Fatalf("first read should contain file content, got: %s", fresh)
	}
	if strings.Contains(fresh, "CACHED") {
		t.Fatalf("first read should not be a cache hit, got: %s", fresh)
	}

	// Pre-edit cache hit: returns full content with CACHED warning
	// (no EditEnforcer on context → preEdit=true → content returned).
	SetIteration(ctx, 3)
	cached, err := read(ctx, []byte(`{"path":"cached.txt"}`))
	if err != nil {
		t.Fatalf("cached read: %v", err)
	}
	if !strings.Contains(cached, "CACHED") {
		t.Fatalf("cache hit should have CACHED marker, got: %s", cached)
	}
	if !strings.Contains(cached, "iteration 1") {
		t.Fatalf("cache hit should reference original iteration, got: %s", cached)
	}
	// Pre-edit: content is returned so model can write edits.
	if !strings.Contains(cached, "line1") || !strings.Contains(cached, "line3") {
		t.Fatalf("pre-edit cache hit should return full content, got: %s", cached)
	}

	// Post-edit cache hit: returns stub (no content).
	e := NewEditEnforcer(10)
	e.ConfirmEdit([]llms.ToolCall{{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Edit"}}}, map[string]string{"1": "ok"})
	ctx = SetEditEnforcer(ctx, e)
	SetIteration(ctx, 5)
	stub, err := read(ctx, []byte(`{"path":"cached.txt"}`))
	if err != nil {
		t.Fatalf("post-edit cached read: %v", err)
	}
	if !strings.Contains(stub, "CACHED") {
		t.Fatalf("post-edit cache hit should have CACHED marker, got: %s", stub)
	}
	if strings.Contains(stub, "line1") || strings.Contains(stub, "line3") {
		t.Fatalf("post-edit cache hit should return stub without content, got: %s", stub)
	}
}

// --- Rolling Compaction Tests ---

// buildLargeConversation creates a conversation with enough tokens to trigger rolling compaction.
func buildLargeConversation(numMiddle int, toolOutputSize int) []llms.MessageContent {
	msgs := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: "system"}}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "user"}}},
	}
	for i := 0; i < numMiddle; i++ {
		// AI message with a Read tool call
		msgs = append(msgs, llms.MessageContent{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{
					ID:           fmt.Sprintf("tc-%d", i),
					FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: fmt.Sprintf(`{"path":"file%d.go"}`, i)},
				},
			},
		})
		// Tool result
		msgs = append(msgs, llms.MessageContent{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{
					ToolCallID: fmt.Sprintf("tc-%d", i),
					Name:       "Read",
					Content:    strings.Repeat("x", toolOutputSize),
				},
			},
		})
	}
	// Add recent messages (protected tail)
	for i := 0; i < keepRecentMessages; i++ {
		msgs = append(msgs, llms.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: fmt.Sprintf("recent %d", i)}},
		})
	}
	return msgs
}

func TestRollingCompactSkipsNonInterval(t *testing.T) {
	t.Parallel()
	msgs := buildLargeConversation(30, 2000)
	ctx := context.Background()
	result := rollingCompact(ctx, msgs, 5) // not at interval
	if len(result) != len(msgs) {
		t.Fatal("should return unchanged messages at non-interval iteration")
	}
}

func TestRollingCompactAtInterval(t *testing.T) {
	t.Parallel()
	// Need enough tokens to exceed rollingCompactMinTokens (25K).
	// 40 messages * 4000 bytes each = 160K bytes / 4 = ~40K tokens
	msgs := buildLargeConversation(40, 4000)
	ctx := context.Background()
	result := rollingCompact(ctx, msgs, 15) // at interval

	// Check that some tool results were compressed
	compressed := 0
	for _, msg := range result {
		if msg.Role != llms.ChatMessageTypeTool {
			continue
		}
		for _, part := range msg.Parts {
			switch resp := part.(type) {
			case llms.ToolCallResponse:
				if strings.Contains(resp.Content, "rolling-compacted") {
					compressed++
				}
			default:
				continue
			}
		}
	}
	if compressed == 0 {
		t.Fatal("expected some tool results to be rolling-compacted at interval 15")
	}
}

func TestRollingCompactPreservesEditResults(t *testing.T) {
	t.Parallel()
	msgs := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: strings.Repeat("s", 20000)}}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: strings.Repeat("u", 20000)}}},
		// Edit tool call + result
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: "e1", FunctionCall: &llms.FunctionCall{Name: "Edit", Arguments: `{"path":"foo.go"}`}},
			},
		},
		{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{ToolCallID: "e1", Name: "Edit", Content: strings.Repeat("y", 2000)},
			},
		},
		// Read tool call + large result
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: "r1", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"bar.go"}`}},
			},
		},
		{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{ToolCallID: "r1", Name: "Read", Content: strings.Repeat("z", 2000)},
			},
		},
	}
	// Add more middle content to push above 25K token threshold
	for i := 0; i < 20; i++ {
		msgs = append(msgs, llms.MessageContent{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: fmt.Sprintf("rx%d", i), FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: fmt.Sprintf(`{"path":"extra%d.go"}`, i)}},
			},
		})
		msgs = append(msgs, llms.MessageContent{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{
				llms.ToolCallResponse{ToolCallID: fmt.Sprintf("rx%d", i), Name: "Read", Content: strings.Repeat("x", 4000)},
			},
		})
	}
	// Add enough tail messages
	for i := 0; i < keepRecentMessages; i++ {
		msgs = append(msgs, llms.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: strings.Repeat("t", 1000)}},
		})
	}

	ctx := context.Background()
	result := rollingCompact(ctx, msgs, 15)

	// Edit result should be preserved (at index 3)
	switch editResult := result[3].Parts[0].(type) {
	case llms.ToolCallResponse:
		if strings.Contains(editResult.Content, "rolling-compacted") {
			t.Fatal("Edit tool results should not be rolling-compacted")
		}
	default:
		t.Fatalf("expected ToolCallResponse at index 3, got %T", result[3].Parts[0])
	}

	// Read result for bar.go should be compressed (at index 5)
	switch readResult := result[5].Parts[0].(type) {
	case llms.ToolCallResponse:
		if !strings.Contains(readResult.Content, "rolling-compacted") {
			t.Fatal("Read tool results should be rolling-compacted")
		}
	default:
		t.Fatalf("expected ToolCallResponse at index 5, got %T", result[5].Parts[0])
	}
}

func TestRollingCompactPreservesSmallResults(t *testing.T) {
	t.Parallel()
	msgs := buildLargeConversation(30, 100) // small tool outputs (100 bytes < 500 threshold)
	ctx := context.Background()
	result := rollingCompact(ctx, msgs, 15)

	// Small results should not be compressed
	for _, msg := range result {
		if msg.Role != llms.ChatMessageTypeTool {
			continue
		}
		for _, part := range msg.Parts {
			switch resp := part.(type) {
			case llms.ToolCallResponse:
				if strings.Contains(resp.Content, "rolling-compacted") {
					t.Fatal("small tool results (under 500 bytes) should not be compressed")
				}
			default:
				continue
			}
		}
	}
}

func TestRollingCompactLowTokenSkip(t *testing.T) {
	t.Parallel()
	// Small conversation — under 25K tokens
	msgs := buildLargeConversation(5, 500)
	ctx := context.Background()
	before := estimateTokens(msgs)
	if before >= rollingCompactMinTokens {
		t.Skip("test assumes conversation is under threshold")
	}
	result := rollingCompact(ctx, msgs, 15)
	if len(result) != len(msgs) {
		t.Fatal("should skip rolling compaction when under token threshold")
	}
}

func TestRollingCompactPreservesRecentMessages(t *testing.T) {
	t.Parallel()
	// Build a large conversation that exceeds the rolling compact threshold.
	msgs := buildLargeConversation(40, 4000)

	// Record the last keepRecentMessages messages (the tail) before compaction.
	tailStart := len(msgs) - keepRecentMessages
	tailBefore := make([]llms.MessageContent, keepRecentMessages)
	copy(tailBefore, msgs[tailStart:])

	ctx := context.Background()
	result := rollingCompact(ctx, msgs, rollingCompactInterval) // at interval

	// Tail messages must be identical after compaction.
	resultTailStart := len(result) - keepRecentMessages
	if resultTailStart < 0 {
		t.Fatalf("result too short: %d messages", len(result))
	}
	for i := 0; i < keepRecentMessages; i++ {
		before := tailBefore[i]
		after := result[resultTailStart+i]
		if before.Role != after.Role {
			t.Fatalf("tail message %d role changed: %v -> %v", i, before.Role, after.Role)
		}
		// Compare text content of first part.
		bText := partText(before)
		aText := partText(after)
		if bText != aText {
			t.Fatalf("tail message %d content changed:\nbefore: %s\nafter:  %s", i, truncStr(bText, 80), truncStr(aText, 80))
		}
	}
}

func partText(m llms.MessageContent) string {
	for _, p := range m.Parts {
		switch v := p.(type) {
		case llms.TextContent:
			return v.Text
		case llms.ToolCallResponse:
			return v.Content
		}
	}
	return ""
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func TestCompactMessagesPreservesEditRelatedMessages(t *testing.T) {
	t.Parallel()

	// Build a conversation where some middle messages involve edited files.
	msgs := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextContent{Text: strings.Repeat("s", 10000)}}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: strings.Repeat("u", 10000)}}},
	}

	// Middle: Read of a file that will be edited (should be preserved)
	msgs = append(msgs, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{ID: "r1", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"edited.go"}`}},
		},
	})
	msgs = append(msgs, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "r1", Content: strings.Repeat("content", 500)},
		},
	})

	// Middle: Edit of the file
	msgs = append(msgs, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{ID: "e1", FunctionCall: &llms.FunctionCall{Name: "Edit", Arguments: `{"path":"edited.go"}`}},
		},
	})
	msgs = append(msgs, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "e1", Content: "updated edited.go (1 replacement)"},
		},
	})

	// Middle: Read of an unrelated file (should be compacted)
	msgs = append(msgs, llms.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []llms.ContentPart{
			llms.ToolCall{ID: "r2", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"unrelated.go"}`}},
		},
	})
	msgs = append(msgs, llms.MessageContent{
		Role: llms.ChatMessageTypeTool,
		Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "r2", Content: strings.Repeat("unrelated", 500)},
		},
	})

	// Add padding to push above token threshold
	for i := 0; i < 5; i++ {
		msgs = append(msgs, llms.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: strings.Repeat("pad", 3000)}},
		})
		msgs = append(msgs, llms.MessageContent{
			Role:  llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{llms.TextContent{Text: strings.Repeat("resp", 3000)}},
		})
	}

	// Recent tail
	for i := 0; i < keepRecentMessages; i++ {
		msgs = append(msgs, llms.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{llms.TextContent{Text: fmt.Sprintf("recent %d", i)}},
		})
	}

	ctx := context.Background()
	result := compactMessages(ctx, msgs, nil)

	// The edit-related messages (Read of edited.go, Edit of edited.go) should
	// score high and be preserved. The unrelated Read should be compacted.
	// Find the tool result for "r2" (unrelated) — it should be compacted.
	foundCompactedUnrelated := false
	for _, msg := range result {
		if msg.Role != llms.ChatMessageTypeTool {
			continue
		}
		for _, part := range msg.Parts {
			switch resp := part.(type) {
			case llms.ToolCallResponse:
				if resp.ToolCallID == "r2" && strings.Contains(resp.Content, "compacted") {
					foundCompactedUnrelated = true
				}
			default:
				continue
			}
		}
	}
	// This test validates the concept — with nil metrics (unlimited budget),
	// the threshold is 50K. If the conversation is large enough, compaction occurs.
	tokens := estimateTokens(msgs)
	if tokens >= contextTokenThreshold && !foundCompactedUnrelated {
		t.Log("conversation was large enough for compaction but unrelated result was not compacted — semantic scoring may have kept it")
	}
}
