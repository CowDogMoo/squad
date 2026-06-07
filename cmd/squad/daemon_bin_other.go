//go:build !windows

package main

// preferDaemonBinary is a no-op on non-Windows platforms: launchd and systemd
// can launch the regular console binary without any flashing-window concerns
// that motivate the Windows GUI-subsystem split.
func preferDaemonBinary(exe string) string { return exe }
