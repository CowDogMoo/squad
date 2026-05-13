package routine

import "os"

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func makeDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func removeOSFile(path string) error {
	return os.Remove(path)
}
