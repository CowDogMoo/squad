//go:build darwin || windows

package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

// tailFile streams path's contents to w. When follow is true, it keeps the
// file open and re-reads as new bytes arrive (poll-based, 200ms cadence).
// Returns when ctx is cancelled.
//
// Shared by the macOS and Windows installers, which both use file-based
// daemon logs. Linux journald uses a different path (journalctl exec).
func tailFile(ctx context.Context, w io.Writer, path string, follow bool) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("daemon log not found at %s — has the daemon run yet?", path)
		}
		return err
	}
	defer func() { _ = f.Close() }()

	br := bufio.NewReader(f)
	if _, err := io.Copy(w, br); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(200 * time.Millisecond):
		}
		n, err := io.Copy(w, br)
		if err != nil {
			return err
		}
		if n == 0 {
			// No new bytes; loop back to wait.
			continue
		}
	}
}
