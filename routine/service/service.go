// Package service installs and manages the per-user OS service that runs
// `squad routined`. macOS uses a launchd LaunchAgent, Linux a systemd --user
// unit, Windows a Task Scheduler per-user task. The Service interface hides
// these platform-specific shapes from the CLI.
package service

import (
	"context"
	"errors"
	"io"
)

// State describes whether the service is installed and currently running.
type State int

const (
	// StateNotInstalled means no service artifact exists for the current user.
	StateNotInstalled State = iota
	// StateInstalledStopped means the service is installed but not running.
	StateInstalledStopped
	// StateInstalledRunning means the service is installed and the daemon is
	// believed to be running. "Believed" because not every platform exposes a
	// reliable per-process probe; treat this as best-effort.
	StateInstalledRunning
)

// String renders a state value for log lines and the `routine doctor` output.
func (s State) String() string {
	switch s {
	case StateInstalledRunning:
		return "installed (running)"
	case StateInstalledStopped:
		return "installed (stopped)"
	case StateNotInstalled:
		return "not installed"
	default:
		return "unknown"
	}
}

// Status is the snapshot returned by Service.Status. Paths are filled in
// regardless of state so `routine doctor` can show users where things would
// live, even pre-install.
type Status struct {
	State        State
	ServicePath  string // launchd plist / systemd unit / Task XML
	LogPath      string // platform-conventional daemon log location
	DaemonBinary string // path the service points at (squad routined)
}

// InstallOptions captures per-install knobs that vary by routine set.
// Today only WakeSystem is here; future expansions (per-OS environment
// overrides, IPC binding paths) would go alongside it.
type InstallOptions struct {
	// WakeSystem requests that the OS wake the machine from sleep to keep the
	// daemon supervised. Honored on macOS (launchd WakeSystem) and Windows
	// (Task Scheduler WakeToRun). No effect on Linux user services.
	//
	// IMPORTANT: this affects the *daemon* artifact, not individual fires.
	// It ensures the daemon is awake/ready, but does not guarantee that a
	// routine scheduled for 3 AM fires at 3 AM on a closed laptop — that
	// would require per-routine OS-level calendar triggers, which v1 does
	// not generate.
	WakeSystem bool
}

// Service installs, removes, and reports on the per-user OS service.
type Service interface {
	// Install writes the service artifact and starts the daemon. Idempotent —
	// calling it on an already-installed service performs the equivalent of
	// repair (rewrite + reload).
	Install(daemonBinary string, opts InstallOptions) error
	// Uninstall stops the daemon and removes the service artifact. Returns
	// nil when nothing is installed.
	Uninstall() error
	// Status reports the current state and paths. Never returns an error for
	// the not-installed case — that's reflected via State.
	Status() (Status, error)
	// TailLogs writes daemon logs to w. When follow is true, the call blocks
	// until ctx is cancelled, streaming new lines as they arrive. On Linux
	// this shells out to journalctl; on macOS/Windows it reads the log file
	// directly.
	TailLogs(ctx context.Context, w io.Writer, follow bool) error
}

// ErrUnsupported is returned by platforms without a service implementation
// (currently only platforms outside darwin/linux/windows). All three
// supported platforms return a real Service from New().
var ErrUnsupported = errors.New("service install not supported on this platform")
