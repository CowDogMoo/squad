package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadReferencesSharedSymlink verifies a reference that is a symlink into
// a shared top-level skills/ directory resolves via the agentsDir retry, and
// that a leading SKILL.md frontmatter block is stripped from the injected
// content.
func TestLoadReferencesSharedSymlink(t *testing.T) {
	t.Parallel()

	agentsDir := t.TempDir()
	skillDir := filepath.Join(agentsDir, "skills", "foo-guide")
	agentDir := filepath.Join(agentsDir, "my-agent")
	refsDir := filepath.Join(agentDir, "references")
	for _, d := range []string{skillDir, refsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	skillMD := "---\nname: foo-guide\ndescription: Guide for foo\n---\n# Foo Guide\n\nAlways foo before bar.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "skills", "foo-guide", "SKILL.md"),
		filepath.Join(refsDir, "foo-guide.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	refs, err := loadReferences(agentDir, agentsDir, []string{"references/foo-guide.md"})
	if err != nil {
		t.Fatalf("loadReferences: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 reference, got %d", len(refs))
	}
	if !strings.Contains(refs[0], "Always foo before bar.") {
		t.Errorf("reference content missing body: %q", refs[0])
	}
	if strings.Contains(refs[0], "name: foo-guide") {
		t.Errorf("frontmatter not stripped from injected reference: %q", refs[0])
	}
	if !strings.Contains(refs[0], "## Reference: references/foo-guide.md") {
		t.Errorf("reference header missing: %q", refs[0])
	}
}

// TestLoadReferencesLocalFile verifies plain in-agent references keep working
// and pass through untouched when they carry no frontmatter.
func TestLoadReferencesLocalFile(t *testing.T) {
	t.Parallel()

	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, "my-agent")
	refsDir := filepath.Join(agentDir, "references")
	if err := os.MkdirAll(refsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(refsDir, "criteria.md"), []byte("# Criteria\n\nBe correct.\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	refs, err := loadReferences(agentDir, agentsDir, []string{"references/criteria.md"})
	if err != nil {
		t.Fatalf("loadReferences: %v", err)
	}
	if len(refs) != 1 || !strings.Contains(refs[0], "Be correct.") {
		t.Fatalf("unexpected refs: %v", refs)
	}
}

// TestLoadReferencesMissing verifies a missing reference still errors even
// with the agentsDir retry in place.
func TestLoadReferencesMissing(t *testing.T) {
	t.Parallel()

	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, "my-agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := loadReferences(agentDir, agentsDir, []string{"references/nope.md"}); err == nil {
		t.Fatal("expected error for missing reference, got nil")
	}
}
