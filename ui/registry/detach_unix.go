//go:build !windows

package registry

import "syscall"

// detachAttr returns SysProcAttr that puts the child in its own process
// group so SIGINT/SIGQUIT delivered to the TUI's controlling terminal
// doesn't propagate to the subprocess. The TUI signals children
// explicitly via Stop().
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
