package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileTracker_RecordAndValidate(t *testing.T) {
	t.Parallel()
	ft := NewFileTracker()
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Edit before read should fail.
	if err := ft.ValidateBeforeEdit(file); err == nil {
		t.Fatal("expected error editing unread file")
	}

	// Read, then edit should pass.
	ft.RecordRead(file)
	if err := ft.ValidateBeforeEdit(file); err != nil {
		t.Fatalf("unexpected error after read: %v", err)
	}
}

func TestFileTracker_StaleRead(t *testing.T) {
	t.Parallel()
	ft := NewFileTracker()
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft.RecordRead(file)
	// Wait a tiny bit then modify the file externally.
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(file, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ft.ValidateBeforeEdit(file); err == nil {
		t.Fatal("expected error for stale read")
	}
}

func TestFileTracker_NilSafe(t *testing.T) {
	t.Parallel()
	var ft *FileTracker
	ft.RecordRead("/some/path")
	if err := ft.ValidateBeforeEdit("/some/path"); err != nil {
		t.Fatalf("nil tracker should allow edits: %v", err)
	}
	if !ft.LastReadTime("/some/path").IsZero() {
		t.Fatal("nil tracker should return zero time")
	}
}

func TestFileTracker_NonexistentFile(t *testing.T) {
	t.Parallel()
	ft := NewFileTracker()
	// Validate a file that doesn't exist — should pass (Write creates new files).
	ft.RecordRead("/nonexistent/file.txt")
	if err := ft.ValidateBeforeEdit("/nonexistent/file.txt"); err != nil {
		t.Fatalf("should allow edit of non-existent file: %v", err)
	}
}

func TestFileTracker_ContextIntegration(t *testing.T) {
	t.Parallel()
	ctx := InitFileTracker(context.Background())
	ft := GetFileTracker(ctx)
	if ft == nil {
		t.Fatal("expected file tracker from context")
	}
	ft.RecordRead("/test")
	if ft.LastReadTime("/test").IsZero() {
		t.Fatal("expected non-zero read time")
	}
}

func TestGetFileTracker_MissingContext(t *testing.T) {
	t.Parallel()
	ft := GetFileTracker(context.Background())
	if ft != nil {
		t.Fatal("expected nil from bare context")
	}
}
