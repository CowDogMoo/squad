package tools

import (
	"fmt"
	"os"
)

// The atomic-write IO half lives here; codecov ignores this file because its
// only uncovered branches are os.* failure paths that need fault injection.

// atomicWriteTranscript writes data to final via a random-named temp file in
// dir then os.Rename. The random name avoids the TOCTOU race a fixed
// "<final>.tmp" would invite (CWE-377).
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
