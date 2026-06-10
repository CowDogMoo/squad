//go:build darwin || windows

package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise the unexported tailFile helper on platforms that
// implement file-based daemon logs (macOS and Windows). They are build-gated
// to those OSes to match the implementation file's build tags.

func TestTailFile_NotExist(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "does-not-exist.log")
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tailFile(ctx, &buf, missing, false); err == nil {
		t.Fatal("expected error for missing file, got nil")
	} else if got := err.Error(); !strings.Contains(got, "daemon log not found") {
		t.Fatalf("error = %q, want to contain %q", got, "daemon log not found")
	}
}

func TestTailFile_OpenError(t *testing.T) {
	// Exercises the non-ErrNotExist open error branch by pointing at a path
	// inside a file (not a directory).
	t.Parallel()
	dir := t.TempDir()
	// Create a file where a directory should be.
	blockFile := filepath.Join(dir, "notadir")
	if err := os.WriteFile(blockFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Path inside a file — Open will fail with a non-ErrNotExist error.
	badPath := filepath.Join(blockFile, "daemon.log")
	var buf bytes.Buffer
	ctx := context.Background()
	err := tailFile(ctx, &buf, badPath, false)
	if err == nil {
		t.Fatal("expected error for unreadable path, got nil")
	}
	// Should NOT be the "daemon log not found" message.
	if strings.Contains(err.Error(), "daemon log not found") {
		t.Errorf("expected non-ErrNotExist error, got: %v", err)
	}
}

func TestTailFile_Follow_ContextCancel(t *testing.T) {
	// Exercises the follow=true ctx.Done() branch.
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "daemon.log")
	if err := os.WriteFile(p, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		cancel()
	}()
	if err := tailFile(ctx, &buf, p, true); err != nil {
		t.Fatalf("tailFile(follow=true) returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "line1") {
		t.Errorf("output %q missing 'line1'", buf.String())
	}
}

func TestTailFile_Copy_NoFollow(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	p := filepath.Join(d, "daemon.log")
	if err := os.WriteFile(p, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write temp log: %v", err)
	}
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tailFile(ctx, &buf, p, false); err != nil {
		t.Fatalf("tailFile returned error: %v", err)
	}
	if got, want := buf.String(), "hello\nworld\n"; got != want {
		t.Fatalf("copied contents = %q, want %q", got, want)
	}
}
