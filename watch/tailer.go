package watch

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/session"
)

// Tailer reads one session directory incrementally. Refresh() picks up any
// new bytes appended to events.jsonl and re-parses meta.json when its mtime
// changes. The tailer is safe to call from a single goroutine (typically
// the bubble-tea Update loop); it is not goroutine-safe.
type Tailer struct {
	dir         string
	eventCap    int
	eventOffset int64     // byte offset into events.jsonl reached so far
	metaModTime time.Time // last mtime observed on meta.json
	state       State
}

// NewTailer returns a Tailer for the given .squad/sessions/<id>/ dir.
// The directory does not need to exist yet — Refresh() returns the zero
// State until the session is created on disk.
func NewTailer(dir string) *Tailer {
	return &Tailer{
		dir:      dir,
		eventCap: DefaultEventCap,
	}
}

// SetEventCap overrides the retention limit. 0 disables capping (use with
// caution on long-running sessions).
func (t *Tailer) SetEventCap(n int) {
	t.eventCap = n
}

// Dir returns the watched directory.
func (t *Tailer) Dir() string { return t.dir }

// SessionID returns the session identifier. Prefers the value parsed from
// meta.json; falls back to the directory basename, which is the same value
// for squad's standard layout.
func (t *Tailer) SessionID() string {
	if id := t.state.Meta.SessionID; id != "" {
		return id
	}
	return filepath.Base(t.dir)
}

// State returns the current snapshot. Callers should not mutate the
// returned struct; treat it as a value.
func (t *Tailer) State() State { return t.state }

// Refresh reads any new content from disk and returns the latest State.
// Missing files are treated as "not yet written" — no error, zero state
// fields. Other I/O errors are returned.
func (t *Tailer) Refresh() (State, error) {
	if err := t.refreshMeta(); err != nil {
		return t.state, err
	}
	if err := t.refreshEvents(); err != nil {
		return t.state, err
	}
	return t.state, nil
}

// refreshMeta re-reads meta.json if its mtime is newer than the last read.
func (t *Tailer) refreshMeta() error {
	path := filepath.Join(t.dir, "meta.json")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat meta.json: %w", err)
	}
	if !info.ModTime().After(t.metaModTime) {
		return nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read meta.json: %w", err)
	}
	var meta session.Meta
	if err := json.Unmarshal(body, &meta); err != nil {
		// meta.json may be mid-rewrite; ignore parse errors transiently —
		// the next refresh will see a consistent snapshot.
		return nil
	}
	t.state.Meta = meta
	t.metaModTime = info.ModTime()
	return nil
}

// refreshEvents reads any new bytes from events.jsonl starting at the
// tracked offset. Partially-written lines at the end are deferred until
// the next call (we stop at the last complete newline).
func (t *Tailer) refreshEvents() error {
	path := filepath.Join(t.dir, "events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open events.jsonl: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Seek(t.eventOffset, io.SeekStart); err != nil {
		return fmt.Errorf("seek events.jsonl: %w", err)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<16), 1<<22) // up to 4MB per line for large payloads
	var bytesRead int64
	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1 // +1 for the newline scanner stripped
		var ev session.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			// Skip malformed lines but keep advancing the offset so we
			// don't re-read them forever.
			continue
		}
		t.applyEvent(ev)
	}
	if err := scanner.Err(); err != nil {
		// Token-too-long is the most common scanner.Err — we've already
		// advanced past the lines we could read. Surface other errors.
		if !strings.Contains(err.Error(), "token too long") {
			return fmt.Errorf("scan events.jsonl: %w", err)
		}
	}
	t.eventOffset += bytesRead
	return nil
}

// applyEvent updates State with one event: appends to the tail, bumps the
// relevant counter, and records LastTool/LastError/LastEventAt.
func (t *Tailer) applyEvent(ev session.Event) {
	t.state.LastEventAt = ev.Ts
	switch ev.Type {
	case session.EventIteration:
		t.state.Counts.Iterations++
	case session.EventToolCall:
		t.state.Counts.ToolCalls++
		if p := decodePayload(ev.Payload); p != nil {
			t.state.LastTool = str(p, "name")
		}
	case session.EventResponse:
		t.state.Counts.Responses++
	case session.EventError:
		t.state.Counts.Errors++
		if p := decodePayload(ev.Payload); p != nil {
			if msg := str(p, "error"); msg != "" {
				t.state.LastError = msg
			} else if msg := str(p, "message"); msg != "" {
				t.state.LastError = msg
			}
		}
	case session.EventLargeResult:
		t.state.Counts.LargeResults++
	case session.EventRunEnd:
		if p := decodePayload(ev.Payload); p != nil {
			if msg := str(p, "error"); msg != "" {
				t.state.LastError = msg
			}
		}
	}
	line := EventLine{
		Ts:      ev.Ts,
		Type:    ev.Type,
		Summary: summarize(ev),
	}
	t.state.Events = append(t.state.Events, line)
	if t.eventCap > 0 && len(t.state.Events) > t.eventCap {
		drop := len(t.state.Events) - t.eventCap
		t.state.Events = t.state.Events[drop:]
	}
}

// Reset clears all state and offsets. The next Refresh re-reads from the
// beginning of the session.
func (t *Tailer) Reset() {
	t.eventOffset = 0
	t.metaModTime = time.Time{}
	t.state = State{}
}
