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
	handlers, _ := buildHandlersWithSkill(wd, nil, &executor.LocalExecutor{WorkingDir: wd}, nil)
	if _, ok := handlers["Skill"]; ok {
		t.Error("Skill tool should not appear when runtime is nil")
	}

	// Empty Entries — also no Skill tool.
	handlers, _ = buildHandlersWithSkill(wd, nil, &executor.LocalExecutor{WorkingDir: wd}, &SkillRuntime{})
	if _, ok := handlers["Skill"]; ok {
		t.Error("Skill tool should not appear when entries are empty")
	}

	// With at least one entry the tool appears.
	rt := &SkillRuntime{Entries: []skill.Entry{fakeSkillEntry("alpha", wd)}}
	handlers, _ = buildHandlersWithSkill(wd, nil, &executor.LocalExecutor{WorkingDir: wd}, rt)
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
