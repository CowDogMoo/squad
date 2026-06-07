package routine

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName("nightly"))
	now := time.Date(2026, 5, 12, 2, 0, 0, 0, time.UTC)
	in := &State{
		LastRun:        now,
		LastStatus:     StatusOK,
		LastSessionID:  "20260512T020000Z-abc12345",
		LastDurationMs: 12345,
	}
	if err := SaveState(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := LoadState(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !out.LastRun.Equal(in.LastRun) || out.LastStatus != in.LastStatus ||
		out.LastSessionID != in.LastSessionID || out.LastDurationMs != in.LastDurationMs {
		t.Errorf("mismatch: got=%+v want=%+v", out, in)
	}
}

func TestLoadStateMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName("never-fired"))
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if !s.LastRun.IsZero() {
		t.Errorf("expected zero state, got %+v", s)
	}
}

func TestSaveStateCreatesDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	nested := filepath.Join(dir, "missing", "deep")
	path := filepath.Join(nested, StateFileName("foo"))
	if err := SaveState(path, &State{LastStatus: StatusOK}); err != nil {
		t.Fatalf("save with missing dir: %v", err)
	}
	if _, err := LoadState(path); err != nil {
		t.Fatalf("load: %v", err)
	}
}

func TestSaveStateAtomicNoLeftover(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName("clean"))
	for i := 0; i < 3; i++ {
		if err := SaveState(path, &State{LastStatus: StatusOK, LastRun: time.Now()}); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no .tmp leftovers, found: %v", entries)
	}
}
