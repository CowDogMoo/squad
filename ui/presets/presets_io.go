package presets

import (
	"fmt"
	"os"
)

// This file holds the atomic-write IO half of presets persistence: temp file +
// rename. It is isolated here (and ignored by codecov) because every uncovered
// branch is an os.CreateTemp / Write / Chmod / Rename error path that would
// need fault injection on the os package to exercise. The marshal half stays
// in presets.go and the happy-path round-trip is covered by presets_test.go.

// atomicWriteData writes data to final via a randomly-named temp file in dir
// followed by os.Rename, so a concurrent reader never sees a partial file. The
// random temp name (CWE-377) avoids the symlink/TOCTOU race a fixed
// "<final>.tmp" path would invite. pattern is an os.CreateTemp name pattern.
func atomicWriteData(dir, final, pattern string, data []byte, perm os.FileMode) error {
	tmpFile, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := tmpFile.Name()
	if _, werr := tmpFile.Write(data); werr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write temp: %w", werr)
	}
	if cerr := tmpFile.Close(); cerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temp: %w", cerr)
	}
	if err := os.Chmod(tmp, perm); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}
