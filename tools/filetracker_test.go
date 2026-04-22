package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileTrackerNilSafe(t *testing.T) {
	var ft *FileTracker

	tests := []struct {
		name    string
		checkFn func(t *testing.T)
	}{
		{
			name: "RecordRead does not panic",
			checkFn: func(t *testing.T) {
				ft.RecordRead("/some/path")
			},
		},
		{
			name: "LastReadTime returns zero",
			checkFn: func(t *testing.T) {
				if !ft.LastReadTime("/some/path").IsZero() {
					t.Error("nil FileTracker.LastReadTime should return zero time")
				}
			},
		},
		{
			name: "ValidateBeforeEdit returns nil",
			checkFn: func(t *testing.T) {
				if err := ft.ValidateBeforeEdit("/some/path"); err != nil {
					t.Errorf("nil FileTracker.ValidateBeforeEdit should return nil, got %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.checkFn(t)
		})
	}
}

func TestFileTrackerRecordAndLastReadTime(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		record   bool
		wantZero bool
	}{
		{
			name:     "zero before recording",
			path:     "/some/file.go",
			record:   false,
			wantZero: true,
		},
		{
			name:     "non-zero after recording",
			path:     "/some/file.go",
			record:   true,
			wantZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ft := NewFileTracker()
			if tt.record {
				before := time.Now()
				ft.RecordRead(tt.path)
				after := time.Now()
				got := ft.LastReadTime(tt.path)
				if got.Before(before) || got.After(after) {
					t.Errorf("LastReadTime %v not in range [%v, %v]", got, before, after)
				}
			} else if !ft.LastReadTime(tt.path).IsZero() {
				t.Error("LastReadTime should be zero before recording")
			}
		})
	}
}

func TestInitAndGetFileTracker(t *testing.T) {
	tests := []struct {
		name    string
		init    bool
		wantNil bool
	}{
		{
			name:    "nil before init",
			init:    false,
			wantNil: true,
		},
		{
			name:    "non-nil after init",
			init:    true,
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.init {
				ctx = InitFileTracker(ctx)
			}
			ft := GetFileTracker(ctx)
			if tt.wantNil && ft != nil {
				t.Error("GetFileTracker should return nil")
			}
			if !tt.wantNil && ft == nil {
				t.Error("GetFileTracker should return non-nil")
			}
		})
	}
}

func TestValidateBeforeEdit(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, ft *FileTracker) string // returns file path
		wantErr error
	}{
		{
			name: "error for unread file",
			setup: func(t *testing.T, ft *FileTracker) string {
				dir := t.TempDir()
				path := filepath.Join(dir, "test.go")
				if err := os.WriteFile(path, []byte("package main"), 0644); err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				return path
			},
			wantErr: ErrFileNotRead,
		},
		{
			name: "success after read",
			setup: func(t *testing.T, ft *FileTracker) string {
				dir := t.TempDir()
				path := filepath.Join(dir, "test.go")
				if err := os.WriteFile(path, []byte("package main"), 0644); err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				ft.RecordRead(path)
				return path
			},
			wantErr: nil,
		},
		{
			name: "stale read after modification",
			setup: func(t *testing.T, ft *FileTracker) string {
				dir := t.TempDir()
				path := filepath.Join(dir, "test.go")
				if err := os.WriteFile(path, []byte("package main"), 0644); err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				ft.RecordRead(path)
				time.Sleep(10 * time.Millisecond)
				if err := os.WriteFile(path, []byte("package main\n// modified"), 0644); err != nil {
					t.Fatalf("failed to modify temp file: %v", err)
				}
				return path
			},
			wantErr: ErrStaleRead,
		},
		{
			name: "non-existent file allowed after read",
			setup: func(t *testing.T, ft *FileTracker) string {
				path := "/nonexistent/path/file.go"
				ft.RecordRead(path)
				return path
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ft := NewFileTracker()
			path := tt.setup(t, ft)
			err := ft.ValidateBeforeEdit(path)

			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("expected nil error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected %v, got: %v", tt.wantErr, err)
				}
			}
		})
	}
}
