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
