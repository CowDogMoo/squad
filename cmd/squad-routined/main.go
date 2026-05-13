// Command squad-routined is a thin alternative entrypoint to the squad
// routines daemon.
//
// On Windows it is built with `-ldflags "-H=windowsgui"` so Task Scheduler
// does not flash a console window each time it restarts the daemon. On
// macOS and Linux the equivalent functionality is reached via
// `squad routined`, and this binary is mostly a convenience for
// scripted installs that want a single-purpose executable.
//
// Both this binary and the `squad routined` subcommand call into
// `routine/daemon.Run` with the same config-loading behaviour, so the
// runtime behaviour is identical.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/routine/daemon"
)

func main() {
	os.Exit(run())
}

// run encapsulates the daemon lifecycle so deferred cancel() calls fire
// before the process exits. main() does the os.Exit dance so go-critic's
// exitAfterDefer rule doesn't trip on a defer that would never run.
func run() int {
	var (
		logFile       = flag.String("log-file", "", "Append daemon stdout/stderr to this file")
		maxConcurrent = flag.Uint("max-concurrent", 2, "Maximum concurrent routine fires across the daemon")
		fireTimeout   = flag.Duration("fire-timeout", 0, "Optional per-fire wall-clock timeout (0 = unlimited)")
	)
	flag.Parse()

	if err := daemon.RedirectStdio(*logFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	cfg, _, err := config.Load()
	if err != nil {
		logging.Warn("failed to load config, using defaults: %v", err)
		cfg = config.Defaults()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := daemon.Run(ctx, cfg, daemon.Options{
		MaxConcurrent: *maxConcurrent,
		FireTimeout:   *fireTimeout,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		// Give launchd/Task Scheduler a moment to log before exit, but don't
		// hang if stderr is already closed.
		time.Sleep(100 * time.Millisecond)
		return 1
	}
	return 0
}
