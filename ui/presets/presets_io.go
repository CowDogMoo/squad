package presets

import (
	"fmt"
	"os"
)

// The atomic-write IO half lives here; codecov ignores this file because its
// only uncovered branches are os.* failure paths that need fault injection.

// atomicWriteData writes data to final via a random-named temp file in dir then
// os.Rename. The random name avoids the TOCTOU race a fixed "<final>.tmp" would
// invite (CWE-377); pattern is an os.CreateTemp name pattern.
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
