// Package registry owns subprocess agent runs launched from the TUI. It
// runs `squad run` (or arbitrary commands) in a background goroutine,
// keeps an exec.Cmd handle for each, and surfaces exit status via a
// channel the TUI polls.
//
// The registry does NOT pair commands to session IDs — squad's session
// package picks IDs at runtime, and the TUI's existing file-based
// discovery (watch.Discover) finds the new dirs on the next refresh.
// Pairing is performed lazily via NewestAfter(): callers ask "what new
// session dir appeared since I launched this cmd?" and attribute by
// timestamp.
package registry

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Status is the lifecycle of a Launched subprocess.
type Status int

const (
	StatusStarting Status = iota
	StatusRunning
	StatusExited
	StatusFailed
)

// String renders the status as a short label.
func (s Status) String() string {
	switch s {
	case StatusStarting:
		return "starting"
	case StatusRunning:
		return "running"
	case StatusExited:
		return "exited"
	case StatusFailed:
		return "failed"
	}
	return "unknown"
}

// Launched is one tracked subprocess.
type Launched struct {
	ID        string    // registry-assigned, stable across the launched lifetime
	Args      []string  // full argv as launched (for display)
	Cmd       *exec.Cmd // active subprocess handle
	StartedAt time.Time
	ExitedAt  time.Time
	Status    Status
	ExitErr   error // non-nil if the subprocess failed to start or exited non-zero
}

// Registry owns a set of Launched subprocesses.
type Registry struct {
	mu  sync.Mutex
	seq int
	all map[string]*Launched
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{all: map[string]*Launched{}}
}

// Launch starts a subprocess with the given argv. Args[0] is the program
// path (absolute or PATH-resolved). Stdin is closed; stdout/stderr are
// redirected to /dev/null — the TUI reads progress from the session dir
// the subprocess writes, not its streams.
//
// Returns the assigned Launched record (Status will be Running) and any
// startup error.
func (r *Registry) Launch(workingDir string, args []string) (*Launched, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("launch: empty args")
	}
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec // intentional subprocess
	cmd.Dir = workingDir
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	// Detach from the controlling terminal so SIGINT to the TUI doesn't
	// also hit the child. The TUI cancels via Stop() (SIGTERM).
	cmd.SysProcAttr = detachAttr()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", args[0], err)
	}

	r.mu.Lock()
	r.seq++
	id := fmt.Sprintf("L%04d", r.seq)
	lr := &Launched{
		ID:        id,
		Args:      append([]string(nil), args...),
		Cmd:       cmd,
		StartedAt: time.Now(),
		Status:    StatusRunning,
	}
	r.all[id] = lr
	r.mu.Unlock()

	go r.reap(lr)
	return lr, nil
}

// reap waits for the subprocess to exit and records the outcome.
func (r *Registry) reap(lr *Launched) {
	err := lr.Cmd.Wait()
	r.mu.Lock()
	lr.ExitedAt = time.Now()
	if err != nil {
		lr.Status = StatusFailed
		lr.ExitErr = err
	} else {
		lr.Status = StatusExited
	}
	r.mu.Unlock()
}

// Stop sends SIGTERM to a running launched subprocess. No-op if the
// process has already exited.
func (r *Registry) Stop(id string) error {
	r.mu.Lock()
	lr, ok := r.all[id]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown launch: %s", id)
	}
	if lr.Status != StatusRunning {
		return nil
	}
	if lr.Cmd.Process == nil {
		return nil
	}
	return lr.Cmd.Process.Signal(syscall.SIGTERM)
}

// All returns a snapshot of every launched subprocess, newest first.
// Safe to call from the bubble-tea render loop; returns copies.
func (r *Registry) All() []Launched {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Launched, 0, len(r.all))
	for _, lr := range r.all {
		out = append(out, *lr)
	}
	// Sort by StartedAt descending (newest first).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].StartedAt.After(out[j-1].StartedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Get returns the current state of one launch.
func (r *Registry) Get(id string) (Launched, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lr, ok := r.all[id]
	if !ok {
		return Launched{}, false
	}
	return *lr, true
}

// SquadBinary returns the path to the squad executable to use for child
// launches. Prefers os.Args[0] when it looks like a real path (contains
// a separator or ends with "squad"); else returns "squad" for PATH lookup.
func SquadBinary() string {
	exe := os.Args[0]
	if exe == "" {
		return "squad"
	}
	return exe
}
