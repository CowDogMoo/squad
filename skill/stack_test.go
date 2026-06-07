package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	if !s.Push(entry("a", "/tmp/a")) {
		t.Fatal("first push should succeed")
	}
	if !s.Push(entry("a", "/tmp/a")) {
		t.Error("duplicate push should report success (already on stack)")
	}
	if s.Len() != 1 {
		t.Errorf("duplicate push grew stack to %d", s.Len())
	}
}

func TestStackPushReturnsFalseForInvalidEntry(t *testing.T) {
	// Dir anchors Read/Bash scope expansion at runtime, so an entry with
	// no Dir is unusable and gets dropped. Push must report that drop so
	// tests don't quietly proceed against an empty stack — the exact bug
	// that masked the allowed-tools denial test before this contract.
	s := NewStack()
	if s.Push(Entry{Manifest: &Manifest{Name: "no-dir"}}) {
		t.Error("Push of entry with empty Dir should return false")
	}
	if s.Len() != 0 {
		t.Errorf("stack should remain empty, got len=%d", s.Len())
	}

	var nilStack *Stack
	if nilStack.Push(entry("a", "/tmp/a")) {
		t.Error("Push on nil receiver should return false")
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

func TestStackEntries(t *testing.T) {
	s := NewStack()
	if got := s.Entries(); len(got) != 0 {
		t.Errorf("empty stack should produce empty slice, got %v", got)
	}
	dir := t.TempDir()
	e := Entry{Manifest: &Manifest{Name: "x"}, Dir: dir}
	s.Push(e)
	got := s.Entries()
	if len(got) != 1 || got[0].Dir != dir {
		t.Errorf("expected one entry with dir %q, got %v", dir, got)
	}
	// Returned slice must be a snapshot, not the internal slice.
	got[0].Dir = "mutated"
	if s.Entries()[0].Dir != dir {
		t.Errorf("Entries should return a snapshot, mutation leaked")
	}
}

func TestStackEntriesNil(t *testing.T) {
	var s *Stack
	if got := s.Entries(); got != nil {
		t.Errorf("nil stack Entries should be nil, got %v", got)
	}
}

func TestStackResolveEmptyInput(t *testing.T) {
	s := NewStack()
	if _, ok := s.Resolve(""); ok {
		t.Error("empty input should not resolve")
	}
	if _, ok := s.Resolve("   "); ok {
		t.Error("whitespace input should not resolve")
	}
}

func TestStackResolveNilStack(t *testing.T) {
	var s *Stack
	if _, ok := s.Resolve("anything"); ok {
		t.Error("nil stack should not resolve")
	}
}

func TestStackResolveEmptyStack(t *testing.T) {
	s := NewStack()
	if _, ok := s.Resolve("anything"); ok {
		t.Error("empty stack should not resolve")
	}
}

// TestStackConcurrent exercises the documented "safe for concurrent use"
// guarantee. Run with -race to catch a regression that drops the lock; the
// stack is read from background task workers while the dispatch goroutine
// pushes new skills.
func TestStackConcurrent(t *testing.T) {
	s := NewStack()
	dirs := []string{t.TempDir(), t.TempDir(), t.TempDir()}

	var wg sync.WaitGroup
	for i, d := range dirs {
		wg.Add(1)
		go func(name, dir string) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Push(entry(name, dir))
			}
		}(fmt.Sprintf("s%d", i), d)
	}
	for range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = s.Len()
				_, _ = s.Top()
				_ = s.Entries()
				_ = s.Contains(dirs[0])
				_, _ = s.Resolve("scripts/x.sh")
			}
		}()
	}
	wg.Wait()

	if s.Len() != len(dirs) {
		t.Errorf("after concurrent idempotent pushes, len = %d, want %d", s.Len(), len(dirs))
	}
}

func TestVerifyNoSymlinkEscape(t *testing.T) {
	skillDir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skillDir, "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	inside := filepath.Join(skillDir, "real.txt")
	if err := os.WriteFile(inside, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStack()
	s.Push(entry("a", skillDir))

	if err := s.VerifyNoSymlinkEscape(filepath.Join(skillDir, "link", "secret.txt")); err == nil {
		t.Error("symlink escape should be rejected")
	}
	if err := s.VerifyNoSymlinkEscape(inside); err != nil {
		t.Errorf("real file inside skill dir should pass: %v", err)
	}
	// A non-existent path is allowed through (the file op surfaces its own error).
	if err := s.VerifyNoSymlinkEscape(filepath.Join(skillDir, "does-not-exist.txt")); err != nil {
		t.Errorf("non-existent path should be allowed through: %v", err)
	}
	// Nil receiver is a no-op.
	var nilStack *Stack
	if err := nilStack.VerifyNoSymlinkEscape(inside); err != nil {
		t.Errorf("nil stack should be a no-op: %v", err)
	}
}

func TestIsWithinRel(t *testing.T) {
	if !isWithin("/a/b/c", "/a/b") {
		t.Error("/a/b/c should be within /a/b")
	}
	if !isWithin("/a/b", "/a/b") {
		t.Error("same path should be within itself")
	}
	if isWithin("/x", "/a/b") {
		t.Error("/x should not be within /a/b")
	}
	if isWithin("/a/b/../c", "/a/b") {
		t.Error("/a/b/../c (= /a/c) should not be within /a/b")
	}
}
