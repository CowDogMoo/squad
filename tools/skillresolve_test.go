package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cowdogmoo/squad/skill"
)

// entryForDir builds a minimal catalog entry pointing at dir. Only Dir and a
// name are needed for path-resolution tests.
func entryForDir(name, dir string) skill.Entry {
	return skill.Entry{
		Manifest: &skill.Manifest{Name: name, Description: "test", Body: "test"},
		Dir:      dir,
	}
}

// TestResolvePathInRunRelativeReferenceUnderSkill is the regression guard for
// the progressive-disclosure read path. A skill body links its reference with
// a relative path (`references/foo.md`); when the agent Reads that path the
// resolver must reach the file inside the loaded skill directory even though
// the working directory has no such file. Before the existence-aware fallback,
// the working-dir anchor claimed the relative path unconditionally and the
// skill stack was never consulted, so the Read failed with not-found.
func TestResolvePathInRunRelativeReferenceUnderSkill(t *testing.T) {
	workingDir := t.TempDir()
	skillDir := t.TempDir()

	refRel := filepath.Join("references", "tell-categories.md")
	refAbs := filepath.Join(skillDir, refRel)
	if err := os.MkdirAll(filepath.Dir(refAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refAbs, []byte("corpus"), 0o644); err != nil {
		t.Fatal(err)
	}

	stack := skill.NewStack()
	stack.Push(entryForDir("detect-llm-tells", skillDir))
	ctx := WithSkillRuntime(context.Background(), &SkillRuntime{Stack: stack})

	got, err := resolvePathInRun(ctx, workingDir, refRel)
	if err != nil {
		t.Fatalf("resolvePathInRun returned error: %v", err)
	}
	if got != refAbs {
		t.Errorf("resolved %q, want the skill-dir reference %q", got, refAbs)
	}
}

// TestResolvePathInRunWorkingDirWins confirms the fallback does not change
// behavior when the relative path DOES exist under the working directory: the
// working-dir copy must still win over a same-named file inside a skill.
func TestResolvePathInRunWorkingDirWins(t *testing.T) {
	workingDir := t.TempDir()
	skillDir := t.TempDir()

	rel := "shared.md"
	wdAbs := filepath.Join(workingDir, rel)
	if err := os.WriteFile(wdAbs, []byte("working"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, rel), []byte("skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	stack := skill.NewStack()
	stack.Push(entryForDir("s", skillDir))
	ctx := WithSkillRuntime(context.Background(), &SkillRuntime{Stack: stack})

	got, err := resolvePathInRun(ctx, workingDir, rel)
	if err != nil {
		t.Fatalf("resolvePathInRun returned error: %v", err)
	}
	if got != wdAbs {
		t.Errorf("resolved %q, want the working-dir file %q", got, wdAbs)
	}
}

// TestResolvePathInRunMissingEverywhere confirms that when a relative path
// exists in neither the working dir nor any skill, the resolver still returns
// the working-dir-anchored path so the caller's not-found error matches the
// input the user supplied.
func TestResolvePathInRunMissingEverywhere(t *testing.T) {
	workingDir := t.TempDir()
	skillDir := t.TempDir()

	stack := skill.NewStack()
	stack.Push(entryForDir("s", skillDir))
	ctx := WithSkillRuntime(context.Background(), &SkillRuntime{Stack: stack})

	rel := "nope.md"
	got, err := resolvePathInRun(ctx, workingDir, rel)
	if err != nil {
		t.Fatalf("resolvePathInRun returned error: %v", err)
	}
	if want := filepath.Join(workingDir, rel); got != want {
		t.Errorf("resolved %q, want working-dir path %q", got, want)
	}
}
