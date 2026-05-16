package routine

import (
	"path/filepath"
	"testing"
	"time"
)

func TestValidateID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id      string
		wantErr bool
	}{
		{"nightly-audit", false},
		{"a", false},
		{"ab", false},
		{"go-review-1", false},
		{"", true},
		{"Nightly", true},                // uppercase
		{"-leading", true},               // leading hyphen
		{"trailing-", true},              // trailing hyphen
		{"1leading-digit", true},         // starts with digit
		{"under_score", true},            // underscore
		{"dot.in.id", true},              // dot
		{"space id", true},               // space
		{string(make([]byte, 65)), true}, // too long (and invalid runes)
	}
	for _, tt := range tests {
		err := ValidateID(tt.id)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateID(%q) err=%v, wantErr=%v", tt.id, err, tt.wantErr)
		}
	}
	// Boundary: exactly 64 valid chars should pass.
	long := "a"
	for i := 0; i < 63; i++ {
		long += "b"
	}
	if err := ValidateID(long); err != nil {
		t.Errorf("ValidateID(64-char slug) unexpected error: %v", err)
	}
	// 65 chars should fail.
	if err := ValidateID(long + "c"); err == nil {
		t.Error("ValidateID(65-char slug) expected error, got nil")
	}
}

func TestValidateSchedule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sched   string
		wantErr bool
	}{
		{"0 2 * * *", false},
		{"*/5 * * * *", false},
		{"@daily", false},
		{"@hourly", false},
		{"@every 30m", false},
		{"@every 1h30m", false},
		{"", true},
		{"not a schedule", true},
		{"60 * * * *", true}, // out-of-range minute
		{"* * * * 8", true},  // out-of-range dow
		{"@every notaduration", true},
	}
	for _, tt := range tests {
		err := ValidateSchedule(tt.sched)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateSchedule(%q) err=%v, wantErr=%v", tt.sched, err, tt.wantErr)
		}
	}
}

func TestRoutineValidate(t *testing.T) {
	t.Parallel()
	base := Routine{ID: "ok", Agent: "go-review", Schedule: "@daily"}
	if err := base.Validate(); err != nil {
		t.Fatalf("baseline validate: %v", err)
	}

	cases := []struct {
		name string
		mut  func(r *Routine)
		want bool
	}{
		{"missing agent", func(r *Routine) { r.Agent = "" }, true},
		{"bad id", func(r *Routine) { r.ID = "BadID" }, true},
		{"bad schedule", func(r *Routine) { r.Schedule = "nope" }, true},
		{"bad catchup", func(r *Routine) { r.Catchup = "every-day" }, true},
		{"catchup skip ok", func(r *Routine) { r.Catchup = CatchupSkip }, false},
		{"catchup fire-once ok", func(r *Routine) { r.Catchup = CatchupFireOnce }, false},
		{"negative max_cost", func(r *Routine) { r.MaxCost = -1 }, true},
		{"negative max_iterations", func(r *Routine) { r.MaxIterations = -1 }, true},
		{"openai-compat missing base_url", func(r *Routine) { r.Provider = "openai-compat" }, true},
		{"openai-compat with base_url ok", func(r *Routine) {
			r.Provider = "openai-compat"
			r.BaseURL = "https://api.deepinfra.com/v1/openai"
		}, false},
	}
	for _, c := range cases {
		r := base
		c.mut(&r)
		err := r.Validate()
		if (err != nil) != c.want {
			t.Errorf("%s: err=%v, wantErr=%v", c.name, err, c.want)
		}
	}
}

func TestSaveLoadRoutineRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName("nightly-audit"))
	original := &Routine{
		ID:         "nightly-audit",
		Agent:      "go-review",
		Schedule:   "0 2 * * *",
		Prompt:     "audit recent changes",
		WorkingDir: "/tmp/repo",
		Vars:       map[string]string{"threshold": "high"},
		Enabled:    true,
		Catchup:    CatchupFireOnce,
		CreatedAt:  time.Date(2026, 5, 12, 18, 0, 0, 0, time.UTC),
	}
	if err := SaveRoutine(path, original); err != nil {
		t.Fatalf("SaveRoutine: %v", err)
	}
	got, err := LoadRoutine(path)
	if err != nil {
		t.Fatalf("LoadRoutine: %v", err)
	}
	if got.ID != original.ID || got.Agent != original.Agent || got.Schedule != original.Schedule {
		t.Errorf("round-trip mismatch: got=%+v want=%+v", got, original)
	}
	if got.Vars["threshold"] != "high" {
		t.Errorf("vars round-trip mismatch: %v", got.Vars)
	}
	if !got.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("created_at mismatch: got=%v want=%v", got.CreatedAt, original.CreatedAt)
	}
}

func TestSaveRoutineAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName("a-routine"))
	r := &Routine{ID: "a-routine", Agent: "go-review", Schedule: "@daily", Enabled: true}
	if err := SaveRoutine(path, r); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// Overwrite — also confirms no .tmp leftover.
	r.Prompt = "updated"
	if err := SaveRoutine(path, r); err != nil {
		t.Fatalf("second save: %v", err)
	}
	entries, err := filepathGlob(dir, "*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no .tmp files after save, found %v", entries)
	}
}

func TestIDFromFileName(t *testing.T) {
	t.Parallel()
	if id := IDFromFileName("foo.yaml"); id != "foo" {
		t.Errorf("got %q, want foo", id)
	}
	if id := IDFromFileName("Foo.yaml"); id != "" {
		t.Errorf("invalid id should yield empty, got %q", id)
	}
	if id := IDFromFileName("foo.txt"); id != "" {
		t.Errorf("non-yaml should yield empty, got %q", id)
	}
}

func TestNextFire(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 5, 12, 1, 0, 0, 0, time.UTC)
	next := NextFire("0 2 * * *", from)
	if next.IsZero() {
		t.Fatal("NextFire returned zero")
	}
	if next.Hour() != 2 || next.Minute() != 0 {
		t.Errorf("expected 02:00 fire, got %v", next)
	}
	if NextFire("garbage", from).IsZero() != true {
		t.Error("invalid schedule should yield zero time")
	}
}

// filepathGlob is a tiny wrapper so the import stays in the *_test.go.
func filepathGlob(dir, pattern string) ([]string, error) {
	return filepath.Glob(filepath.Join(dir, pattern))
}
