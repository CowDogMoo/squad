// Package session provides an append-only event log that records every
// prompt, response, tool call, and tool result for a run. The log is the
// source of truth and lets a run be resumed via the OpenAI Responses API's
// PreviousResponseID. Large tool results are spilled to disk under
// results/<id>.txt and replaced inline with a placeholder the model can
// re-fetch via the get_tool_result tool.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cowdogmoo/squad/logging"
)

// SessionsRoot is the directory under workingDir where sessions are stored.
const SessionsRoot = ".squad/sessions"

// LargeResultThreshold is the byte size above which a tool result is spilled
// to results/<id>.txt and replaced inline with a placeholder.
const LargeResultThreshold = 8 * 1024

// Statuses returned by Logger.Finish.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusError     = "error"
	StatusBudget    = "budget_exceeded"
)

// Event types written to events.jsonl.
const (
	EventRunStart        = "run_start"
	EventResume          = "resume"
	EventPrompt          = "prompt"
	EventResponse        = "response"
	EventToolCall        = "tool_call"
	EventToolResult      = "tool_result"
	EventLargeResult     = "large_result"
	EventIteration       = "iteration"
	EventError           = "error"
	EventRunEnd          = "run_end"
	EventSkillLoaded     = "skill_loaded"
	EventConfirmResolved = "confirm_resolved"
)

// Meta holds the per-session metadata kept in meta.json. It is rewritten on
// every update so a `--resume` can read it without scanning the event log.
type Meta struct {
	SessionID      string    `json:"session_id"`
	Created        time.Time `json:"created"`
	Updated        time.Time `json:"updated"`
	Agent          string    `json:"agent"`
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	WorkingDir     string    `json:"working_dir"`
	Prompt         string    `json:"prompt"`
	LastResponseID string    `json:"last_response_id,omitempty"`
	// RoutineID identifies the routine that produced this session (qualified
	// form: "global:nightly" / "repo:audit"). Empty for non-routine runs.
	RoutineID    string  `json:"routine_id,omitempty"`
	Status       string  `json:"status"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Cost         float64 `json:"cost"`
	Iterations   int     `json:"iterations"`
}

// Event is one append-only record written to events.jsonl.
type Event struct {
	Ts      time.Time       `json:"ts"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Logger writes events.jsonl and rewrites meta.json on update. Methods are
// safe to call concurrently.
type Logger struct {
	dir       string
	resultDir string
	events    *os.File
	mu        sync.Mutex
	meta      Meta
}

// New creates a fresh session under <workingDir>/.squad/sessions/<id>/ and
// returns a Logger ready to receive events. The caller should defer Close.
func New(workingDir, agent, provider, model, prompt string) (*Logger, error) {
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(workingDir, SessionsRoot, id)
	resultDir := filepath.Join(dir, "results")
	if err := os.MkdirAll(resultDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir session: %w", err)
	}

	now := time.Now().UTC()
	l := &Logger{
		dir:       dir,
		resultDir: resultDir,
		meta: Meta{
			SessionID:  id,
			Created:    now,
			Updated:    now,
			Agent:      agent,
			Provider:   provider,
			Model:      model,
			WorkingDir: workingDir,
			Prompt:     prompt,
			Status:     StatusRunning,
		},
	}
	if err := l.openEventsFile(); err != nil {
		return nil, err
	}
	if err := l.writeMeta(); err != nil {
		return nil, err
	}
	return l, nil
}

// Open loads an existing session for resume. The Logger appends to the same
// events.jsonl and rewrites meta.json in place.
func Open(workingDir, sessionID string) (*Logger, error) {
	dir := filepath.Join(workingDir, SessionsRoot, sessionID)
	resultDir := filepath.Join(dir, "results")
	if err := os.MkdirAll(resultDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir results: %w", err)
	}

	metaBytes, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("read meta: %w", err)
	}
	var meta Meta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("parse meta: %w", err)
	}
	meta.Updated = time.Now().UTC()
	meta.Status = StatusRunning

	l := &Logger{dir: dir, resultDir: resultDir, meta: meta}
	if err := l.openEventsFile(); err != nil {
		return nil, err
	}
	if err := l.writeMeta(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Logger) openEventsFile() error {
	f, err := os.OpenFile(filepath.Join(l.dir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open events.jsonl: %w", err)
	}
	l.events = f
	return nil
}

// SessionID returns the session identifier.
func (l *Logger) SessionID() string { return l.meta.SessionID }

// Dir returns the session directory.
func (l *Logger) Dir() string { return l.dir }

// LastResponseID returns the most recent OpenAI response id chained for this
// session, or "" if no response has been recorded yet.
func (l *Logger) LastResponseID() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.meta.LastResponseID
}

// Append records an event to events.jsonl. payload may be nil. Safe to
// call on a nil Logger (no-op).
func (l *Logger) Append(eventType string, payload any) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.appendLocked(eventType, payload)
}

func (l *Logger) appendLocked(eventType string, payload any) error {
	if l.events == nil {
		return nil
	}
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		raw = b
	}
	ev := Event{Ts: time.Now().UTC(), Type: eventType, Payload: raw}
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if _, err := l.events.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	return nil
}

// SetLastResponseID records the most recent OpenAI response id and persists
// it to meta.json so a resume can pick up the chain. Safe on nil Logger.
func (l *Logger) SetLastResponseID(id string) {
	if l == nil || id == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.meta.LastResponseID = id
	l.meta.Updated = time.Now().UTC()
	if err := l.writeMeta(); err != nil {
		logging.Warn("write meta: %v", err)
	}
}

// SetRoutineID stamps the meta.json with the qualified routine id that owns
// this session. Called by the routines daemon's fire handler before invoking
// the agent so `squad routine history` can filter by exact provenance.
func (l *Logger) SetRoutineID(id string) {
	if l == nil || id == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.meta.RoutineID = id
	l.meta.Updated = time.Now().UTC()
	if err := l.writeMeta(); err != nil {
		logging.Warn("write meta: %v", err)
	}
}

// UpdateMetrics overwrites the cumulative token + cost fields in meta.json.
// Pass the current totals from *metrics.Metrics; this is not additive.
func (l *Logger) UpdateMetrics(inputTokens, outputTokens int64, cost float64, iterations int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.meta.InputTokens = inputTokens
	l.meta.OutputTokens = outputTokens
	l.meta.Cost = cost
	l.meta.Iterations = iterations
	l.meta.Updated = time.Now().UTC()
	if err := l.writeMeta(); err != nil {
		logging.Warn("write meta: %v", err)
	}
}

// StoreLargeResult writes the full content to results/<id>.txt and returns
// the result id. Callers replace the inline tool output with a placeholder
// referencing this id, and the model can re-fetch via get_tool_result.
func (l *Logger) StoreLargeResult(content string) (string, error) {
	if l == nil {
		return "", fmt.Errorf("nil logger")
	}
	id, err := newShortID()
	if err != nil {
		return "", err
	}
	path := filepath.Join(l.resultDir, id+".txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write large result: %w", err)
	}
	return id, nil
}

// ReadLargeResult returns a slice of a previously stored large result. If
// limit <= 0 the whole content from offset is returned. The total byte
// length is also returned so the caller can advise the model on remaining
// content.
func (l *Logger) ReadLargeResult(id string, offset, limit int) (string, int, error) {
	if l == nil {
		return "", 0, fmt.Errorf("nil logger")
	}
	path := filepath.Join(l.resultDir, id+".txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read large result: %w", err)
	}
	total := len(data)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		return "", total, nil
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return string(data[offset:end]), total, nil
}

// Finish records the terminal status (run_end event + meta.json status).
func (l *Logger) Finish(status string, errMsg string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	payload := map[string]string{"status": status}
	if errMsg != "" {
		payload["error"] = errMsg
	}
	if err := l.appendLocked(EventRunEnd, payload); err != nil {
		logging.Warn("append run_end event: %v", err)
	}
	l.meta.Status = status
	l.meta.Updated = time.Now().UTC()
	if err := l.writeMeta(); err != nil {
		logging.Warn("write meta: %v", err)
	}
}

// Close flushes the events file. Call after Finish.
func (l *Logger) Close() error {
	if l == nil || l.events == nil {
		return nil
	}
	err := l.events.Close()
	l.events = nil
	return err
}

func (l *Logger) writeMeta() error {
	final := filepath.Join(l.dir, "meta.json")
	b, err := json.MarshalIndent(l.meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	// Create the temp file with a random name (CWE-377): a fixed
	// "meta.json.tmp" path is predictable and races with anything else in the
	// session dir. os.CreateTemp opens it exclusively.
	tmpFile, err := os.CreateTemp(l.dir, ".meta-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp meta: %w", err)
	}
	tmp := tmpFile.Name()
	if _, werr := tmpFile.Write(b); werr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write meta: %w", werr)
	}
	if cerr := tmpFile.Close(); cerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close meta: %w", cerr)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod meta: %w", err)
	}
	return os.Rename(tmp, final)
}

func newSessionID() (string, error) {
	short, err := newShortID()
	if err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + short, nil
}

func newShortID() (string, error) {
	var b [4]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

type ctxKey struct{}

// WithLogger attaches a Logger to ctx so deeply nested calls (executor,
// responses loop, tool handlers) can record events without plumbing.
func WithLogger(ctx context.Context, l *Logger) context.Context {
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the Logger attached to ctx, or nil if none.
func FromContext(ctx context.Context) *Logger {
	if ctx == nil {
		return nil
	}
	l, _ := ctx.Value(ctxKey{}).(*Logger)
	return l
}
