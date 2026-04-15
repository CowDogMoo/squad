package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTestFile: %v", err)
	}
	return file
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readTestFile: %v", err)
	}
	return string(data)
}

func TestMultiEdit_BasicReplacements(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := writeTestFile(t, dir, "test.go", "foo bar baz\nfoo qux\n")

	tool := multiEditTool(dir)
	args := MultiEditArgs{
		Path: "test.go",
		Edits: []MultiEditOperation{
			{Old: "foo bar", New: "hello"},
			{Old: "foo qux", New: "world"},
		},
	}
	raw, _ := json.Marshal(args)
	result, err := tool(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "2 edit(s) applied") {
		t.Fatalf("expected 2 edits applied, got: %s", result)
	}
	if got := readTestFile(t, file); got != "hello baz\nworld\n" {
		t.Fatalf("unexpected file content: %q", got)
	}
}

func TestMultiEdit_PartialFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := writeTestFile(t, dir, "test.txt", "alpha beta gamma")

	tool := multiEditTool(dir)
	args := MultiEditArgs{
		Path: "test.txt",
		Edits: []MultiEditOperation{
			{Old: "alpha", New: "ALPHA"},
			{Old: "nonexistent", New: "X"},
			{Old: "gamma", New: "GAMMA"},
		},
	}
	raw, _ := json.Marshal(args)
	result, err := tool(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error on partial success: %v", err)
	}
	if !strings.Contains(result, "2 edit(s) applied") {
		t.Fatalf("expected 2 applied, got: %s", result)
	}
	if !strings.Contains(result, "1 failed") {
		t.Fatalf("expected 1 failed, got: %s", result)
	}
	if got := readTestFile(t, file); got != "ALPHA beta GAMMA" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestMultiEdit_AllFail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := writeTestFile(t, dir, "test.txt", "hello world")

	tool := multiEditTool(dir)
	args := MultiEditArgs{
		Path: "test.txt",
		Edits: []MultiEditOperation{
			{Old: "xxx", New: "Y"},
			{Old: "yyy", New: "Z"},
		},
	}
	raw, _ := json.Marshal(args)
	_, err := tool(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error when all edits fail")
	}
	if got := readTestFile(t, file); got != "hello world" {
		t.Fatalf("file should not be modified when all edits fail: %q", got)
	}
}

func TestMultiEdit_ReplaceAll(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := writeTestFile(t, dir, "test.txt", "aaa bbb aaa ccc aaa")

	tool := multiEditTool(dir)
	args := MultiEditArgs{
		Path: "test.txt",
		Edits: []MultiEditOperation{
			{Old: "aaa", New: "XXX", ReplaceAll: true},
		},
	}
	raw, _ := json.Marshal(args)
	result, err := tool(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "1 edit(s) applied") {
		t.Fatalf("expected 1 applied, got: %s", result)
	}
	if got := readTestFile(t, file); got != "XXX bbb XXX ccc XXX" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestMultiEdit_SequentialEdits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := writeTestFile(t, dir, "test.txt", "foo")

	tool := multiEditTool(dir)
	args := MultiEditArgs{
		Path: "test.txt",
		Edits: []MultiEditOperation{
			{Old: "foo", New: "bar"},
			{Old: "bar", New: "baz"},
		},
	}
	raw, _ := json.Marshal(args)
	result, err := tool(context.Background(), raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "2 edit(s) applied") {
		t.Fatalf("expected 2 applied, got: %s", result)
	}
	if got := readTestFile(t, file); got != "baz" {
		t.Fatalf("expected sequential application, got: %q", got)
	}
}

func TestMultiEdit_EmptyEdits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tool := multiEditTool(dir)
	args := MultiEditArgs{Path: "test.txt", Edits: []MultiEditOperation{}}
	raw, _ := json.Marshal(args)
	_, err := tool(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error for empty edits")
	}
}

func TestMultiEdit_EmptyOldString(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "test.txt", "hello")

	tool := multiEditTool(dir)
	args := MultiEditArgs{
		Path: "test.txt",
		Edits: []MultiEditOperation{
			{Old: "", New: "X"},
		},
	}
	raw, _ := json.Marshal(args)
	_, err := tool(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error when old string is empty")
	}
}

func TestMultiEdit_PathTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tool := multiEditTool(dir)
	args := MultiEditArgs{
		Path:  "../etc/passwd",
		Edits: []MultiEditOperation{{Old: "root", New: "hacked"}},
	}
	raw, _ := json.Marshal(args)
	_, err := tool(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestMultiEdit_InvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tool := multiEditTool(dir)
	_, err := tool(context.Background(), []byte(`{bad`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMultiEdit_NonexistentFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tool := multiEditTool(dir)
	args := MultiEditArgs{
		Path:  "nonexistent.go",
		Edits: []MultiEditOperation{{Old: "x", New: "y"}},
	}
	raw, _ := json.Marshal(args)
	_, err := tool(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestMultiEdit_FileTrackerValidation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "test.go", "hello world")

	ctx := InitFileTracker(context.Background())
	// Don't record a read — edit should fail validation.
	tool := multiEditTool(dir)
	args := MultiEditArgs{
		Path:  "test.go",
		Edits: []MultiEditOperation{{Old: "hello", New: "goodbye"}},
	}
	raw, _ := json.Marshal(args)
	_, err := tool(ctx, raw)
	if err == nil {
		t.Fatal("expected error when file not read before edit")
	}
}
