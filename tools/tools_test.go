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
	var b FlexBool
	if err := json.Unmarshal([]byte("true"), &b); err != nil || !bool(b) {
		t.Fatalf("expected true, got %v err=%v", b, err)
	}
	if err := json.Unmarshal([]byte("\"yes\""), &b); err != nil || !bool(b) {
		t.Fatalf("expected true from string, got %v err=%v", b, err)
	}
	if err := json.Unmarshal([]byte("\"no\""), &b); err != nil || bool(b) {
		t.Fatalf("expected false from string, got %v err=%v", b, err)
	}
	if err := json.Unmarshal([]byte("123"), &b); err == nil {
		t.Fatalf("expected error for invalid type")
	}
}

func TestResolvePath(t *testing.T) {
	dir := t.TempDir()
	resolved, err := ResolvePath(dir, "child.txt")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if !strings.HasSuffix(resolved, filepath.Join(dir, "child.txt")) {
		t.Fatalf("unexpected resolved path: %s", resolved)
	}

	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	if _, err := ResolvePath(dir, outside); err == nil {
		t.Fatalf("expected error for outside path")
	}
}

func TestGlobMatcher(t *testing.T) {
	matcher, err := newGlobMatcher("**/*.go")
	if err != nil {
		t.Fatalf("newGlobMatcher: %v", err)
	}
	if !matcher.Match("cmd/main.go") {
		t.Fatalf("expected match")
	}
	if matcher.Match("cmd/main.txt") {
		t.Fatalf("expected no match")
	}
}

func TestLimitOutput(t *testing.T) {
	data := bytes.Repeat([]byte("a"), maxToolOutput+10)
	truncated := limitOutput(data)
	if len(truncated) <= maxToolOutput {
		t.Fatalf("expected truncation")
	}
	if !strings.Contains(string(truncated), "output truncated") {
		t.Fatalf("expected truncation marker")
	}
}

func TestTruncateString(t *testing.T) {
	if got := TruncateString("short", 10); got != "short" {
		t.Fatalf("unexpected truncate result: %s", got)
	}
	if got := TruncateString("longer string", 4); got != "long..." {
		t.Fatalf("unexpected truncate result: %s", got)
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
	dir := t.TempDir()
	bash := bashTool(dir)
	out, err := bash(context.Background(), []byte(`{"command":"printf 'hi'"}`))
	if err != nil {
		t.Fatalf("bashTool: %v", err)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("unexpected bash output: %q", out)
	}

	if _, err := bash(context.Background(), []byte(`{"command":""}`)); err == nil {
		t.Fatalf("expected error for empty command")
	}
}

func TestRepeatTracker(t *testing.T) {
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
