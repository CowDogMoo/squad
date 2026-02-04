package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

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
		wantDefs int
	}{
		{"without TaskConfig", false, 6},
		{"with TaskConfig", true, 7},
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
					) (string, error) {
						return "", nil
					},
				}
			}
			handlers, defs := BuildHandlers(dir, cfg)
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
			bash := bashTool(dir)
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
		{"high repeat tool at limit", "Read", MaxToolIterations, true},
		{"high repeat tool below limit", "Read", MaxToolIterations - 1, false},
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

	out, err := RunWithTools(context.Background(), llm, "", "user", dir, 2, nil)
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
	out, err := finishToolLoop(context.Background(), llm, nil, "partial", 1, nil)
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
			_, err := RunWithTools(context.Background(), tt.llm, "", "user", t.TempDir(), 1, nil)
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
	out, err := finishToolLoop(context.Background(), llm, nil, "", 1, nil)
	if err != nil {
		t.Fatalf("finishToolLoop() error = %v", err)
	}
	if out != "final" {
		t.Fatalf("output = %q, want %q", out, "final")
	}
}

func TestFinishToolLoopErrorNoContent(t *testing.T) {
	llm := &stubLLM{err: errors.New("boom")}
	_, err := finishToolLoop(context.Background(), llm, nil, "", 1, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}
