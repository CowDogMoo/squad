package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/skill"
)

// fakeSkillEntry builds a skill.Entry pointing at dir, with a body the test
// can identify in the Skill tool's return value.
func fakeSkillEntry(name, dir string) skill.Entry {
	return skill.Entry{
		Manifest: &skill.Manifest{
			Name:        name,
			Description: name + " skill description.",
			Body:        "BODY-OF-" + name,
		},
		Scope: skill.ScopeRepo,
		Dir:   dir,
	}
}

func TestSkillTool_LoadsBodyAndPushes(t *testing.T) {
	dir := t.TempDir()
	entry := fakeSkillEntry("alpha", dir)
	stack := skill.NewStack()
	pushed := []skill.Entry{}
	rt := &SkillRuntime{
		Entries: []skill.Entry{entry},
		Stack:   stack,
		OnLoad:  func(e skill.Entry) { pushed = append(pushed, e) },
	}

	call := skillTool(rt)
	body, err := call(context.Background(), []byte(`{"name":"alpha"}`))
	if err != nil {
		t.Fatal(err)
	}
	if body != "BODY-OF-alpha" {
		t.Errorf("body = %q, want BODY-OF-alpha", body)
	}
	if stack.Len() != 1 {
		t.Errorf("stack should have one entry, got %d", stack.Len())
	}
	if len(pushed) != 1 || pushed[0].Name() != "alpha" {
		t.Errorf("OnLoad callback not invoked correctly: %+v", pushed)
	}
}

func TestSkillTool_UnknownName(t *testing.T) {
	rt := &SkillRuntime{
		Entries: []skill.Entry{fakeSkillEntry("alpha", t.TempDir())},
		Stack:   skill.NewStack(),
	}
	_, err := skillTool(rt)(context.Background(), []byte(`{"name":"missing"}`))
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestSkillTool_NoCatalogRegistered(t *testing.T) {
	_, err := skillTool(nil)(context.Background(), []byte(`{"name":"x"}`))
	if err == nil {
		t.Fatal("expected error when runtime is nil")
	}
}

func TestSkillTool_MissingName(t *testing.T) {
	rt := &SkillRuntime{Entries: []skill.Entry{fakeSkillEntry("a", t.TempDir())}}
	_, err := skillTool(rt)(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestSkillRuntime_HasCatalog(t *testing.T) {
	var r *SkillRuntime
	if r.HasCatalog() {
		t.Error("nil runtime should not report a catalog")
	}
	r = &SkillRuntime{}
	if r.HasCatalog() {
		t.Error("empty entries should not report a catalog")
	}
	r.Entries = []skill.Entry{fakeSkillEntry("a", "/tmp")}
	if !r.HasCatalog() {
		t.Error("with entries, HasCatalog should be true")
	}
}

func TestReadTool_StackRelaxation(t *testing.T) {
	// Two separate directories: the agent's working dir and a skill dir.
	// Read of a file inside the skill dir is blocked by the strict anchor
	// but should succeed once the skill is on the stack.
	workingDir := t.TempDir()
	skillDir := t.TempDir()
	refPath := filepath.Join(skillDir, "references", "notes.md")
	if err := os.MkdirAll(filepath.Dir(refPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refPath, []byte("REF-CONTENT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	read := readTool(workingDir)
	args, _ := json.Marshal(map[string]string{"path": refPath})

	// 1. Without the skill on the stack, the read must fail with the
	//    "outside working directory" anchor error.
	ctx := WithSkillRuntime(context.Background(), &SkillRuntime{Stack: skill.NewStack()})
	ctx = InitReadCache(ctx)
	ctx = InitFileTracker(ctx)
	if _, err := read(ctx, args); err == nil {
		t.Fatal("expected anchor error before skill is on stack")
	}

	// 2. Push the skill and retry — should now succeed.
	rt := GetSkillRuntime(ctx)
	rt.Stack.Push(fakeSkillEntry("alpha", skillDir))
	out, err := read(ctx, args)
	if err != nil {
		t.Fatalf("read after push: %v", err)
	}
	if !strings.Contains(out, "REF-CONTENT") {
		t.Errorf("expected REF-CONTENT in output, got: %q", out)
	}
}

func TestReadTool_StackDoesNotAllowArbitraryPaths(t *testing.T) {
	workingDir := t.TempDir()
	skillDir := t.TempDir()
	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := &SkillRuntime{
		Entries: []skill.Entry{fakeSkillEntry("alpha", skillDir)},
		Stack:   skill.NewStack(),
	}
	rt.Stack.Push(rt.Entries[0])
	ctx := WithSkillRuntime(context.Background(), rt)
	ctx = InitReadCache(ctx)
	ctx = InitFileTracker(ctx)

	args, _ := json.Marshal(map[string]string{"path": outsidePath})
	if _, err := readTool(workingDir)(ctx, args); err == nil {
		t.Fatal("stack relaxation should not grant access to arbitrary outside paths")
	}
}

// pushedSkillCtx returns a context carrying a SkillRuntime whose stack already
// contains skillDir, plus the read cache / file tracker the file tools expect.
func pushedSkillCtx(skillDir string) context.Context {
	rt := &SkillRuntime{
		Entries: []skill.Entry{fakeSkillEntry("alpha", skillDir)},
		Stack:   skill.NewStack(),
	}
	rt.Stack.Push(rt.Entries[0])
	ctx := WithSkillRuntime(context.Background(), rt)
	ctx = InitReadCache(ctx)
	ctx = InitFileTracker(ctx)
	return ctx
}

// TestWriteEditStayAnchoredWithSkillLoaded is the load-bearing security
// assertion: even when a skill is on the stack (which relaxes Read/Grep into
// the skill dir), Write/Edit/MultiEdit must remain anchored to the working
// dir and refuse to touch the skill dir. A regression that swapped these tools
// to resolvePathInRun would let an agent write arbitrary files through a
// loaded skill — this test fails loudly if that happens.
func TestWriteEditStayAnchoredWithSkillLoaded(t *testing.T) {
	workingDir := t.TempDir()
	skillDir := t.TempDir()

	// Seed a real file in the skill dir so Edit/MultiEdit would otherwise
	// succeed if the anchor were relaxed — the rejection must come from the
	// path check, not from a missing file.
	target := filepath.Join(skillDir, "victim.txt")
	if err := os.WriteFile(target, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := pushedSkillCtx(skillDir)

	writeArgs, _ := json.Marshal(map[string]string{"path": target, "content": "x"})
	if _, err := writeTool(workingDir)(ctx, writeArgs); err == nil {
		t.Error("Write into a loaded skill dir must be rejected")
	}

	editArgs, _ := json.Marshal(map[string]string{"path": target, "old": "hello", "new": "bye"})
	if _, err := editTool(workingDir)(ctx, editArgs); err == nil {
		t.Error("Edit into a loaded skill dir must be rejected")
	}

	multiArgs, _ := json.Marshal(map[string]any{
		"path":  target,
		"edits": []map[string]any{{"old": "hello", "new": "bye"}},
	})
	if _, err := multiEditTool(workingDir)(ctx, multiArgs); err == nil {
		t.Error("MultiEdit into a loaded skill dir must be rejected")
	}

	// The skill-dir file must be untouched by any of the above.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Errorf("skill-dir file was modified: %q", got)
	}
}

// TestGrepTool_StackRelaxation mirrors the Read relaxation: Grep cannot reach
// the skill dir before the skill is pushed, but can after — and still refuses
// paths outside every active skill dir.
func TestGrepTool_StackRelaxation(t *testing.T) {
	workingDir := t.TempDir()
	skillDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillDir, "ref.md"), []byte("MATCHME here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	grep := grepTool(workingDir)
	args, _ := json.Marshal(map[string]string{"pattern": "MATCHME", "path": skillDir})

	// Before push: the skill dir is outside the working dir, so grep is rejected.
	emptyCtx := WithSkillRuntime(context.Background(), &SkillRuntime{Stack: skill.NewStack()})
	if _, err := grep(emptyCtx, args); err == nil {
		t.Fatal("expected anchor error before skill is on stack")
	}

	// After push: grep reaches the skill dir.
	ctx := pushedSkillCtx(skillDir)
	out, err := grep(ctx, args)
	if err != nil {
		t.Fatalf("grep after push: %v", err)
	}
	if !strings.Contains(out, "MATCHME") {
		t.Errorf("expected match in output, got %q", out)
	}

	// An unrelated outside dir is still rejected even with a skill loaded.
	outside := t.TempDir()
	outArgs, _ := json.Marshal(map[string]string{"pattern": "MATCHME", "path": outside})
	if _, err := grep(ctx, outArgs); err == nil {
		t.Error("grep of an unrelated outside dir must be rejected")
	}
}

// TestResolvePathInRun_RejectsTraversalWithStack proves the relaxed resolver
// rejects traversal and outside-absolute inputs even when the stack is
// populated — the asymmetry only widens access to files genuinely inside a
// skill dir.
func TestResolvePathInRun_RejectsTraversalWithStack(t *testing.T) {
	workingDir := t.TempDir()
	skillDir := t.TempDir()
	ctx := pushedSkillCtx(skillDir)

	if _, err := resolvePathInRun(ctx, workingDir, "../../etc/passwd"); err == nil {
		t.Error("relative traversal must be rejected with a populated stack")
	}
	if _, err := resolvePathInRun(ctx, workingDir, "/etc/passwd"); err == nil {
		t.Error("outside-absolute path must be rejected with a populated stack")
	}
	// A path genuinely inside the skill dir resolves.
	inside := filepath.Join(skillDir, "ok.md")
	if _, err := resolvePathInRun(ctx, workingDir, inside); err != nil {
		t.Errorf("path inside skill dir should resolve: %v", err)
	}
}

// TestReadTool_RejectsSymlinkEscape covers the defense-in-depth check: a
// symlink inside the skill dir that points outside it must not become a read
// primitive for arbitrary files.
func TestReadTool_RejectsSymlinkEscape(t *testing.T) {
	workingDir := t.TempDir()
	skillDir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skillDir, "link")); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	ctx := pushedSkillCtx(skillDir)
	escapePath := filepath.Join(skillDir, "link", "secret.txt") // lexically inside skillDir
	args, _ := json.Marshal(map[string]string{"path": escapePath})
	if _, err := readTool(workingDir)(ctx, args); err == nil {
		t.Fatal("read through a skill-dir symlink that escapes the dir must be rejected")
	}
}

func TestBashTool_SkillDirEnv(t *testing.T) {
	skillDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(skillDir, "scripts", "echo.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hello-from-$SQUAD_SKILL_DIR\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	rt := &SkillRuntime{
		Entries: []skill.Entry{fakeSkillEntry("alpha", skillDir)},
		Stack:   skill.NewStack(),
	}
	rt.Stack.Push(rt.Entries[0])
	ctx := WithSkillRuntime(context.Background(), rt)

	ex := &executor.LocalExecutor{WorkingDir: t.TempDir()}
	defer func() { _ = ex.Close() }()

	args, _ := json.Marshal(map[string]string{"command": "bash \"$SQUAD_SKILL_DIR/scripts/echo.sh\""})
	out, err := bashTool(ex)(ctx, args)
	if err != nil {
		t.Fatalf("bash: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "hello-from-"+skillDir) {
		t.Errorf("script output missing skill dir: %q", out)
	}
}

func TestBashTool_NoSkillEnvWhenStackEmpty(t *testing.T) {
	rt := &SkillRuntime{Stack: skill.NewStack()}
	ctx := WithSkillRuntime(context.Background(), rt)

	ex := &executor.LocalExecutor{WorkingDir: t.TempDir()}
	defer func() { _ = ex.Close() }()

	args, _ := json.Marshal(map[string]string{"command": "echo \"SQUAD_SKILL_DIR=[$SQUAD_SKILL_DIR]\""})
	out, err := bashTool(ex)(ctx, args)
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if !strings.Contains(out, "SQUAD_SKILL_DIR=[]") {
		t.Errorf("expected empty env, got: %q", out)
	}
}

func TestBuildHandlers_SkillToolGatedByCatalog(t *testing.T) {
	wd := t.TempDir()
	// Empty runtime — no Skill tool should be registered.
	handlers, _ := buildHandlersWithSkill(wd, nil, &executor.LocalExecutor{WorkingDir: wd}, nil, nil)
	if _, ok := handlers["Skill"]; ok {
		t.Error("Skill tool should not appear when runtime is nil")
	}

	// Empty Entries — also no Skill tool.
	handlers, _ = buildHandlersWithSkill(wd, nil, &executor.LocalExecutor{WorkingDir: wd}, &SkillRuntime{}, nil)
	if _, ok := handlers["Skill"]; ok {
		t.Error("Skill tool should not appear when entries are empty")
	}

	// With at least one entry the tool appears.
	rt := &SkillRuntime{Entries: []skill.Entry{fakeSkillEntry("alpha", wd)}}
	handlers, _ = buildHandlersWithSkill(wd, nil, &executor.LocalExecutor{WorkingDir: wd}, rt, nil)
	if _, ok := handlers["Skill"]; !ok {
		t.Error("Skill tool should be registered when catalog has entries")
	}
}

func TestShellSingleQuote(t *testing.T) {
	cases := map[string]string{
		"/tmp/skill":             "'/tmp/skill'",
		"path with spaces":       "'path with spaces'",
		"o'malley":               `'o'\''malley'`,
		"":                       "''",
		"semicolon;injection":    "'semicolon;injection'",
		"dollar$sign":            "'dollar$sign'",
		"backtick`evil`":         "'backtick`evil`'",
		`'leading and trailing'`: `''\''leading and trailing'\'''`,
	}
	for in, want := range cases {
		if got := shellSingleQuote(in); got != want {
			t.Errorf("shellSingleQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSkillRuntime_FindNilReceiver(t *testing.T) {
	var r *SkillRuntime
	if _, ok := r.find("anything"); ok {
		t.Error("nil receiver find should return !ok")
	}
}

func TestSkillTool_InvalidJSONArgs(t *testing.T) {
	rt := &SkillRuntime{Entries: []skill.Entry{
		{Manifest: &skill.Manifest{Name: "x", Body: "body"}, Dir: t.TempDir()},
	}}
	if _, err := skillTool(rt)(context.Background(), []byte("not json")); err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestSkillRuntime_AllowsToolNoStack(t *testing.T) {
	var r *SkillRuntime
	if !r.AllowsTool("Read") {
		t.Error("nil runtime should permit any tool")
	}
	r = &SkillRuntime{}
	if !r.AllowsTool("Read") {
		t.Error("runtime with nil stack should permit any tool")
	}
}

func TestSkillRuntime_AllowsToolEnforcesAllowList(t *testing.T) {
	stack := skill.NewStack()
	stack.Push(skill.Entry{
		Manifest: &skill.Manifest{
			Name:         "restricted",
			Description:  "x",
			AllowedTools: skill.AllowedTools{"Read", "Bash"},
		},
		Dir: t.TempDir(),
	})
	r := &SkillRuntime{Stack: stack}
	if !r.AllowsTool("Read") {
		t.Error("Read should be allowed")
	}
	if r.AllowsTool("WebFetch") {
		t.Error("WebFetch should be denied")
	}
	if !r.AllowsTool("Skill") {
		t.Error("Skill loader must always be allowed even under restriction")
	}
}

func TestSkillRuntime_AllowsToolIntersectsStack(t *testing.T) {
	stack := skill.NewStack()
	stack.Push(skill.Entry{
		Manifest: &skill.Manifest{
			Name:         "a",
			Description:  "x",
			AllowedTools: skill.AllowedTools{"Read", "Bash", "WebFetch"},
		},
		Dir: t.TempDir(),
	})
	stack.Push(skill.Entry{
		Manifest: &skill.Manifest{
			Name:         "b",
			Description:  "x",
			AllowedTools: skill.AllowedTools{"Read"},
		},
		Dir: t.TempDir(),
	})
	r := &SkillRuntime{Stack: stack}
	if !r.AllowsTool("Read") {
		t.Error("Read should pass both skills")
	}
	if r.AllowsTool("Bash") {
		t.Error("Bash allowed by 'a' but denied by 'b' — intersection should deny")
	}
}

func TestSkillRuntime_AllowsToolSkipsUnrestrictedSkill(t *testing.T) {
	stack := skill.NewStack()
	stack.Push(skill.Entry{
		Manifest: &skill.Manifest{Name: "unrestricted", Description: "x"},
		Dir:      t.TempDir(),
	})
	r := &SkillRuntime{Stack: stack}
	if !r.AllowsTool("AnythingGoes") {
		t.Error("skill without allowed-tools should impose no restriction")
	}
}

// TestSkillRuntime_AllowsTool_RealManifestRoundTrip wires ParseManifest into
// SkillRuntime to cover the seam unit tests miss. Every hand-built Manifest
// test bypasses the parser, so a parser bug (e.g. a comma sticking to a tool
// name) can silently deny every Read call a skill authorized — exactly the
// regression that surfaced when the official squad-skills catalog landed.
// The fixture mirrors the real syntax authors use: kebab-case name, comma
// separators, mixed spacing, Claude-Code-style parens on one entry.
func TestSkillRuntime_AllowsTool_RealManifestRoundTrip(t *testing.T) {
	const fixture = `---
name: doc-comments-discovery-and-fix-loop
description: Use when scrubbing doc comments from a Go codebase.
allowed-tools: Read, Glob, Edit, Bash(go:*)
---

# Doc Comments

body content
`
	m, err := skill.ParseManifest([]byte(fixture))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	stack := skill.NewStack()
	stack.Push(skill.Entry{Manifest: m, Dir: t.TempDir()})
	r := &SkillRuntime{Stack: stack}

	for _, name := range []string{"Read", "Glob", "Edit", "Bash"} {
		if !r.AllowsTool(name) {
			t.Errorf("declared tool %q denied — parser/runtime seam broke", name)
		}
	}
	for _, name := range []string{"Write", "WebFetch", "Grep"} {
		if r.AllowsTool(name) {
			t.Errorf("undeclared tool %q permitted — restriction not honored", name)
		}
	}
	if !r.AllowsTool("Skill") {
		t.Error("Skill loader must remain callable so the agent can stack more skills")
	}
}

func TestSkillTool_RejectsOversizedBody(t *testing.T) {
	dir := t.TempDir()
	entry := skill.Entry{
		Manifest: &skill.Manifest{
			Name:        "huge",
			Description: "x",
			Body:        strings.Repeat("x", skill.MaxBodyBytes+1),
		},
		Dir: dir,
	}
	rt := &SkillRuntime{Entries: []skill.Entry{entry}, Stack: skill.NewStack()}
	if _, err := skillTool(rt)(context.Background(), []byte(`{"name":"huge"}`)); err == nil {
		t.Fatal("expected oversize-body rejection")
	}
}
