package skill

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateHappyPath(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "ok")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Header\nLink to [refs](references/notes.md).\n"
	content := "---\nname: ok\ndescription: A fine skill.\n---\n" + body
	if err := os.WriteFile(filepath.Join(skillDir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Validate(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if r.HasErrors() {
		t.Errorf("unexpected errors: %v", r.Errors())
	}
	if len(r.Warnings()) != 0 {
		t.Errorf("unexpected warnings: %v", r.Warnings())
	}
}

func TestValidateMissingSkillMd(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "empty")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := Validate(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasErrors() {
		t.Fatal("expected an error")
	}
	if !strings.Contains(r.Errors()[0].Message, "missing SKILL.md") {
		t.Errorf("wrong message: %q", r.Errors()[0].Message)
	}
}

func TestValidateNameMismatch(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "actual")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: declared\ndescription: x\n---\nhi\n"
	if err := os.WriteFile(filepath.Join(skillDir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Validate(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasErrors() {
		t.Fatal("expected an error")
	}
	found := false
	for _, f := range r.Errors() {
		if strings.Contains(f.Message, "does not match") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected name-mismatch error, got %v", r.Errors())
	}
}

func TestValidatePathTraversalLink(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "trav")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "See [external](../../etc/passwd) for details.\n"
	content := "---\nname: trav\ndescription: x\n---\n" + body
	if err := os.WriteFile(filepath.Join(skillDir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Validate(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasErrors() {
		t.Fatalf("expected error, got %v", r.Findings)
	}
}

func TestValidateReservedSubstringWarning(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "claude-api")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: claude-api\ndescription: ok\n---\nhi\n"
	if err := os.WriteFile(filepath.Join(skillDir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Validate(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if r.HasErrors() {
		t.Errorf("reserved substring should warn, not error: %v", r.Errors())
	}
	warningFound := false
	for _, w := range r.Warnings() {
		if strings.Contains(w.Message, "reserved substring") {
			warningFound = true
		}
	}
	if !warningFound {
		t.Errorf("expected reserved-substring warning, got: %v", r.Warnings())
	}
}

func TestValidateBodyWarning(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "big")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("x", WarnBodyBytes+1)
	content := "---\nname: big\ndescription: ok\n---\n" + body
	if err := os.WriteFile(filepath.Join(skillDir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Validate(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if r.HasErrors() {
		t.Errorf("unexpected error: %v", r.Errors())
	}
	if len(r.Warnings()) == 0 {
		t.Errorf("expected a warning, got %v", r.Findings)
	}
}

func TestValidateScriptShebangOk(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "scr")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, FileName), []byte("---\nname: scr\ndescription: x\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "do.sh"), []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Validate(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings()) != 0 {
		t.Errorf("unexpected warnings: %v", r.Warnings())
	}
}

func TestValidateScriptMissingInvoker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission check is unix-only")
	}
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "scr")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, FileName), []byte("---\nname: scr\ndescription: x\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-executable, no shebang.
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "do.py"), []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Validate(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings()) == 0 {
		t.Errorf("expected warning about invoker")
	}
}

func TestIsPathTraversal(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"references/notes.md", false},
		{"./notes.md", false},
		{"#anchor-only", false},
		{"https://example.com", false},
		{"/etc/passwd", false},
		{"../outside", true},
		{"a/../../../etc/passwd", true},
	}
	for _, tc := range cases {
		if got := isPathTraversal(tc.in); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.in, got, tc.want)
		}
	}
}
