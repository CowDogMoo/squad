//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// preferDaemonBinary returns a sibling `squad-routined.exe` next to the
// running squad executable when it exists; otherwise the squad binary
// itself. The sibling is built with -ldflags "-H=windowsgui" so the OS
// service doesn't flash a console window each time Task Scheduler restarts
// it. Releases ship both binaries; users who `go install` only the main
// command still get a working (if briefly flashy) daemon.
func preferDaemonBinary(exe string) string {
	candidate := filepath.Join(filepath.Dir(exe), "squad-routined.exe")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return exe
}
