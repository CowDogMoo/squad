//go:build windows

package registry

import "syscall"

// detachAttr is a no-op on Windows — process group semantics differ.
// SIGTERM still works via Process.Signal for portability.
func detachAttr() *syscall.SysProcAttr { return nil }
