package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func entry(name, dir string) Entry {
	return Entry{
		Manifest: &Manifest{Name: name, Description: "x"},
		Dir:      dir,
	}
}

func TestStackNilSafe(t *testing.T) {
	var s *Stack
	s.Push(entry("a", "/tmp/a"))
	if got := s.Len(); got != 0 {
		t.Errorf("nil stack Len = %d, want 0", got)
	}
	if _, ok := s.Top(); ok {
		t.Error("nil stack Top should be ok=false")
	}
	if s.Contains("/tmp/a") {
		t.Error("nil stack Contains should be false")
	}
	if _, ok := s.Resolve("anything"); ok {
		t.Error("nil stack Resolve should be ok=false")
	}
	if got := s.Entries(); got != nil {
		t.Errorf("nil stack Entries = %v, want nil", got)
	}
}

func TestStackPushTopLen(t *testing.T) {
	s := NewStack()
	s.Push(entry("a", "/tmp/a"))
	s.Push(entry("b", "/tmp/b"))
	if s.Len() != 2 {
		t.Errorf("len = %d, want 2", s.Len())
	}
	top, ok := s.Top()
	if !ok || top.Name() != "b" {
		t.Errorf("top = %v ok=%v, want b", top, ok)
	}
}

func TestStackPushIdempotent(t *testing.T) {
	s := NewStack()
	s.Push(entry("a", "/tmp/a"))
	s.Push(entry("a", "/tmp/a"))
	if s.Len() != 1 {
		t.Errorf("duplicate push grew stack to %d", s.Len())
	}
}

func TestStackContains(t *testing.T) {
	dir := t.TempDir()
	s := NewStack()
	s.Push(entry("a", dir))
	if !s.Contains(dir) {
		t.Error("dir itself should be contained")
	}
	if !s.Contains(filepath.Join(dir, "scripts", "x.sh")) {
		t.Error("nested path should be contained")
	}
	if s.Contains(t.TempDir()) {
		t.Error("unrelated dir should not be contained")
	}
}

func TestStackResolveAbsolute(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "references", "ok.md")

	s := NewStack()
	s.Push(entry("a", dir))

	abs, ok := s.Resolve(target)
	if !ok {
		t.Fatal("absolute path inside skill dir should resolve")
	}
	if abs != target {
		t.Errorf("abs = %q, want %q", abs, target)
	}

	if _, ok := s.Resolve("/etc/passwd"); ok {
		t.Error("path outside stack should not resolve")
	}
}

func TestStackResolveRelativeUsesTop(t *testing.T) {
	older := t.TempDir()
	newer := t.TempDir()

	s := NewStack()
	s.Push(entry("older", older))
	s.Push(entry("newer", newer))

	abs, ok := s.Resolve("scripts/echo.sh")
	if !ok {
		t.Fatal("relative path should resolve against top-of-stack")
	}
	if abs != filepath.Join(newer, "scripts", "echo.sh") {
		t.Errorf("abs = %q, want %q", abs, filepath.Join(newer, "scripts", "echo.sh"))
	}
}

func TestStackResolveRelativeFallsThroughStack(t *testing.T) {
	older := t.TempDir()
	newer := t.TempDir()
	if err := touch(filepath.Join(newer, "only-in-newer.txt")); err != nil {
		t.Fatal(err)
	}

	s := NewStack()
	s.Push(entry("older", older))
	s.Push(entry("newer", newer))

	abs, ok := s.Resolve("only-in-newer.txt")
	if !ok || abs != filepath.Join(newer, "only-in-newer.txt") {
		t.Errorf("got abs=%q ok=%v", abs, ok)
	}
}

func TestStackResolveTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	s := NewStack()
	s.Push(entry("a", dir))
	if _, ok := s.Resolve("../etc/passwd"); ok {
		t.Error("traversal out of skill dir should not resolve")
	}
	if _, ok := s.Resolve(""); ok {
		t.Error("empty input should not resolve")
	}
}

// touch creates an empty file at path so absolute-path resolution has
// something to refer to.
func touch(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return f.Close()
}
