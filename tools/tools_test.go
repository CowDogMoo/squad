package tools

import (
	"bytes"
	"context"
	"encoding/json"
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
