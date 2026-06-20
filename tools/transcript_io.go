package tools

import (
	"fmt"
	"os"
)

// This file holds the atomic-write IO half of transcript persistence: temp
// file + rename. It is isolated here (and ignored by codecov) because every
// uncovered branch is an os.CreateTemp / Write / Chmod / Rename error path that
// would need fault injection on the os package to exercise. The encode/marshal
// half stays in transcript.go and the happy-path round-trip is covered by
// transcript_test.go.

// atomicWriteTranscript writes data to final via a randomly-named temp file in
// dir followed by os.Rename, so a resuming reader never loads a partial
// transcript. The random temp name (CWE-377) avoids the symlink/TOCTOU race a
// fixed "<final>.tmp" path would invite.
func atomicWriteTranscript(dir, final string, data []byte) error {
	tmpFile, err := os.CreateTemp(dir, ".transcript-*.json.tmp")
	if err != nil {
		return fmt.Errorf("transcript: create temp: %w", err)
	}
	tmp := tmpFile.Name()
	if _, werr := tmpFile.Write(data); werr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("transcript: write: %w", werr)
	}
	if cerr := tmpFile.Close(); cerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("transcript: close: %w", cerr)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("transcript: chmod: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("transcript: rename: %w", err)
	}
	return nil
}
