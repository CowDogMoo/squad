package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cowdogmoo/squad/csync"
)

// fileTrackerKeyType is the context key for the FileTracker.
type fileTrackerKeyType struct{}

// FileTracker records when files were read by the agent so that edit tools
// can verify the agent has seen the current content before modifying it.
type FileTracker struct {
	reads *csync.Map[string, time.Time]
}

// NewFileTracker creates an empty file tracker.
func NewFileTracker() *FileTracker {
	return &FileTracker{reads: csync.NewMap[string, time.Time]()}
}

// InitFileTracker attaches a FileTracker to the context.
func InitFileTracker(ctx context.Context) context.Context {
	return context.WithValue(ctx, fileTrackerKeyType{}, NewFileTracker())
}

// GetFileTracker retrieves the FileTracker from context, or nil if not set.
func GetFileTracker(ctx context.Context) *FileTracker {
	if ft, ok := ctx.Value(fileTrackerKeyType{}).(*FileTracker); ok {
		return ft
	}
	return nil
}

// RecordRead records that a file was read at the current time.
func (ft *FileTracker) RecordRead(path string) {
	if ft == nil {
		return
	}
	ft.reads.Set(path, time.Now())
}

// LastReadTime returns when a file was last read, or zero time if never read.
func (ft *FileTracker) LastReadTime(path string) time.Time {
	if ft == nil {
		return time.Time{}
	}
	v, _ := ft.reads.Get(path)
	return v
}

// ValidateBeforeEdit checks that the file has been read and hasn't been
// modified since the last read. Returns nil if safe to edit, or an error
// describing the problem.
func (ft *FileTracker) ValidateBeforeEdit(path string) error {
	if ft == nil {
		return nil // tracker not enabled, allow edit
	}
	readTime := ft.LastReadTime(path)
	if readTime.IsZero() {
		return fmt.Errorf("%w: %q has not been read yet — read it first before editing", ErrFileNotRead, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil // file may not exist yet (Write creates), allow
	}
	if info.ModTime().After(readTime) {
		return fmt.Errorf("%w: %q was modified after your last read — re-read it before editing", ErrStaleRead, path)
	}
	return nil
}
