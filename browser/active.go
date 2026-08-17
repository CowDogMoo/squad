package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ActivePort returns the websocket URL for the remote debugging port
// of an active browser session for the given profile.
func ActivePort(name string) (string, error) {
	dir, err := ProfileDir(name)
	if err != nil {
		return "", err
	}

	portFile := filepath.Join(dir, "DevToolsActivePort")
	data, err := os.ReadFile(portFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("no active browser session found for profile %q (DevToolsActivePort not found)", name)
		}
		return "", fmt.Errorf("read DevToolsActivePort: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("invalid DevToolsActivePort format in profile %q", name)
	}

	port, err := strconv.Atoi(lines[0])
	if err != nil {
		return "", fmt.Errorf("invalid port %q in DevToolsActivePort: %w", lines[0], err)
	}

	path := lines[1]
	return fmt.Sprintf("ws://127.0.0.1:%d%s", port, path), nil
}
