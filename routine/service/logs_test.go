//go:build darwin || windows

package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestTailFileNonFollowReadsExistingContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "routined.log")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	if err := tailFile(context.Background(), buf, path, false); err != nil {
		t.Fatalf("tailFile: %v", err)
	}
	if buf.String() != "alpha\nbeta\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestTailFileMissingExplainsToUser(t *testing.T) {
	t.Parallel()
	err := tailFile(context.Background(), &bytes.Buffer{}, filepath.Join(t.TempDir(), "nope.log"), false)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !contains(err.Error(), "daemon log not found") {
		t.Errorf("error should mention missing log, got %q", err)
	}
}

func TestTailFileFollowStreamsAppends(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "live.log")
	if err := os.WriteFile(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	buf := &concurrentBuffer{}
	tailDone := make(chan struct{})
	go func() {
		_ = tailFile(ctx, buf, path, true)
		close(tailDone)
	}()

	// Give the initial read time to land.
	time.Sleep(50 * time.Millisecond)

	// Append a line and wait for the poll loop (200ms cadence) to pick it up.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("second\n")
	_ = f.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if contains(buf.String(), "second") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !contains(buf.String(), "first") || !contains(buf.String(), "second") {
		t.Errorf("expected both lines, got %q", buf.String())
	}
	cancel()
	select {
	case <-tailDone:
	case <-time.After(2 * time.Second):
		t.Fatal("tailFile did not return after ctx cancel")
	}
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}

// concurrentBuffer wraps bytes.Buffer with a mutex so the tail goroutine and
// the test goroutine can safely write/read concurrently.
type concurrentBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *concurrentBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *concurrentBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}
