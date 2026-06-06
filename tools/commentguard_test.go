package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateCommentsOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		old     string
		new     string
		wantErr bool
		wantSub string
	}{
		{
			name: "delete entire single-line comment",
			old: `// Set foo to bar.
foo = bar`,
			new:     `foo = bar`,
			wantErr: false,
		},
		{
			name: "delete one line of a doc-comment block",
			old: `// Foo does the thing.
// Step 1: prepare.
func Foo() {}`,
			new: `// Foo does the thing.
func Foo() {}`,
			wantErr: false,
		},
		{
			name: "trim blank-line gap between doc comment and decl",
			old: `// Foo does the thing.

func Foo() {}`,
			new: `// Foo does the thing.
func Foo() {}`,
			wantErr: false,
		},
		{
			name: "add new function is rejected",
			old: `package x

func Foo() {}`,
			new: `package x

func Foo() {}

func Bar() { return }`,
			wantErr: true,
			wantSub: "added",
		},
		{
			name: "duplicate package decl is rejected",
			old: `package tools

const x = 1`,
			new: `package tools

package tools

const x = 1`,
			wantErr: true,
			wantSub: "added",
		},
		{
			name:    "modify code (rename identifier) is rejected",
			old:     `foo := 1`,
			new:     `bar := 1`,
			wantErr: true,
		},
		{
			name:    "no-op edit",
			old:     `func Foo() {}`,
			new:     `func Foo() {}`,
			wantErr: false,
		},
		{
			name:    "delete trailing line comment is allowed",
			old:     `x := 1 // counter`,
			new:     `x := 1`,
			wantErr: false,
		},
		{
			name: "python hash comment delete is allowed",
			old: `# noisy
x = 1`,
			new:     `x = 1`,
			wantErr: false,
		},
		{
			name: "SQL dash comment delete is allowed",
			old: `-- get all
SELECT * FROM t`,
			new:     `SELECT * FROM t`,
			wantErr: false,
		},
		{
			name: "block comment line delete is allowed",
			old: `/* note */
SELECT * FROM t`,
			new:     `SELECT * FROM t`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCommentsOnly(tt.old, tt.new)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantSub != "" && err != nil && !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error %q missing substring %q", err.Error(), tt.wantSub)
			}
		})
	}
}

func TestIsCommentsOnlyMode_defaultFalse(t *testing.T) {
	t.Parallel()
	if IsCommentsOnlyMode(context.Background()) {
		t.Fatal("default ctx should not be in comments-only mode")
	}
}

func TestInitCommentsOnlyMode(t *testing.T) {
	t.Parallel()
	ctx := InitCommentsOnlyMode(context.Background())
	if !IsCommentsOnlyMode(ctx) {
		t.Fatal("InitCommentsOnlyMode should set the flag")
	}
}

// editPayload is the minimal JSON shape the Edit tool accepts. Declared
// once so the integration tests below don't repeat the inline struct.
type editPayload struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

func TestEditTool_commentsOnlyMode_rejectsCodeChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const src = "package x\n\nfunc Foo() {}\n"
	writeTestFile(t, dir, "main.go", src)

	tool := editTool(dir)
	ctx := InitCommentsOnlyMode(context.Background())
	raw, _ := json.Marshal(editPayload{
		Path: "main.go",
		Old:  "func Foo() {}",
		New:  "func Foo() {}\n\nfunc Bar() { return }",
	})

	_, err := tool(ctx, raw)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "comments-only") {
		t.Fatalf("expected comments-only rejection, got: %v", err)
	}
	if got := readTestFile(t, dir+"/main.go"); got != src {
		t.Fatalf("file should be unchanged on rejection; got: %q", got)
	}
}

func TestEditTool_commentsOnlyMode_allowsCommentDelete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "// Set foo to bar.\nfoo := bar\n")

	tool := editTool(dir)
	ctx := InitCommentsOnlyMode(context.Background())
	raw, _ := json.Marshal(editPayload{
		Path: "main.go",
		Old:  "// Set foo to bar.\nfoo := bar\n",
		New:  "foo := bar\n",
	})

	if _, err := tool(ctx, raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readTestFile(t, dir+"/main.go"); got != "foo := bar\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestEditTool_commentsOnlyMode_offByDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "main.go", "func Foo() {}\n")

	tool := editTool(dir)
	raw, _ := json.Marshal(editPayload{
		Path: "main.go",
		Old:  "func Foo() {}",
		New:  "func Bar() {}",
	})

	if _, err := tool(context.Background(), raw); err != nil {
		t.Fatalf("default mode should allow code change: %v", err)
	}
	if got := readTestFile(t, dir+"/main.go"); got != "func Bar() {}\n" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestMultiEditTool_commentsOnlyMode_rejectsCodeChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const src = "package x\n\nfunc Foo() {}\n"
	writeTestFile(t, dir, "main.go", src)

	tool := multiEditTool(dir)
	ctx := InitCommentsOnlyMode(context.Background())
	raw, _ := json.Marshal(MultiEditArgs{
		Path: "main.go",
		Edits: []MultiEditOperation{
			{Old: "func Foo() {}", New: "func Foo() {}\n\nfunc Bar() {}"},
		},
	})

	_, err := tool(ctx, raw)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := readTestFile(t, dir+"/main.go"); got != src {
		t.Fatalf("file should be unchanged on rejection; got: %q", got)
	}
}

// TestEditTool_commentsOnlyMode_rejectsHallucinatedFunctionInsertion replays
// the exact Edit payload the gpt-4.1-mini run made against ui/pane/pane.go —
// it grafts an isTautologicalComment function and a call site into the
// middle of ClassifyKind, framed as comment cleanup. Without the guardrail
// this Edit succeeded and broke the build with `undefined: strings`. The
// guardrail must reject it and leave the file byte-identical.
func TestEditTool_commentsOnlyMode_rejectsHallucinatedFunctionInsertion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	original := `package pane

import "strings"

type Kind int

const KindPrompt Kind = 0

func ClassifyKind(raw string) (Kind, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return KindPrompt, ""
	}
	switch trimmed[0] {
	case '/':
		return KindPrompt, trimmed[1:]
	}
	return KindPrompt, trimmed
}
`
	writeTestFile(t, dir, "pane.go", original)

	oldStr := `	if trimmed == "" {
		return KindPrompt, ""
	}
	switch trimmed[0] {`
	newStr := `	if trimmed == "" {
		return KindPrompt, ""
	}
	// Delete tautological comments like "// Prompt the user."
	if isTautologicalComment(trimmed) {
		return KindPrompt, ""
	}
	switch trimmed[0] {`

	tool := editTool(dir)
	ctx := InitCommentsOnlyMode(context.Background())
	raw, _ := json.Marshal(editPayload{Path: "pane.go", Old: oldStr, New: newStr})

	_, err := tool(ctx, raw)
	if err == nil {
		t.Fatal("guardrail failed: hallucinated Edit was accepted")
	}
	if !strings.Contains(err.Error(), "comments-only edit rejected") {
		t.Fatalf("rejected for wrong reason: %v", err)
	}
	if got := readTestFile(t, dir+"/pane.go"); got != original {
		t.Fatalf("file was mutated despite rejection:\nwant: %q\ngot:  %q", original, got)
	}
}

// TestEditTool_commentsOnlyMode_rejectsDuplicatePackageBlock replays the
// duplicate-package-decl injection the same run made against tools/retry.go.
// Without the guardrail the file got two `package tools` declarations and
// failed to compile.
func TestEditTool_commentsOnlyMode_rejectsDuplicatePackageBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	original := `package tools

import "time"

const (
	DefaultMaxRetries = 3
	retryBaseDelay    = 2 * time.Second
)
`
	writeTestFile(t, dir, "retry.go", original)

	oldStr := `const (
	DefaultMaxRetries = 3
	retryBaseDelay    = 2 * time.Second
)`
	newStr := `const (
	DefaultMaxRetries = 3
	retryBaseDelay    = 2 * time.Second
)

package tools

import "time"

const (
	DefaultMaxRetries = 3
	retryBaseDelay    = 2 * time.Second
)`

	tool := editTool(dir)
	ctx := InitCommentsOnlyMode(context.Background())
	raw, _ := json.Marshal(editPayload{Path: "retry.go", Old: oldStr, New: newStr})

	_, err := tool(ctx, raw)
	if err == nil {
		t.Fatal("guardrail failed: duplicate package decl Edit was accepted")
	}
	if !strings.Contains(err.Error(), "comments-only edit rejected") {
		t.Fatalf("rejected for wrong reason: %v", err)
	}
	if got := readTestFile(t, dir+"/retry.go"); got != original {
		t.Fatal("file mutated despite rejection")
	}
}
