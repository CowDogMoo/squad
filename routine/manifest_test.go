package routine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/routine"
)

func TestValidateID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{"empty", "", "id is required"},
		{"too-long", strings.Repeat("a", 65), "exceeds 64"},
		{"invalid-chars", "Bad_Upper", "invalid"},
		{"ends-with-hyphen", "abc-", "invalid"},
		{"valid", "abc-123", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := routine.ValidateID(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSchedule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{"empty", "", "schedule is required"},
		{"invalid", "not-a-cron", "invalid schedule"},
		{"valid-predef", "@daily", ""},
		{"valid-interval", "*/5 * * * *", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := routine.ValidateSchedule(tt.in)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestNextFire_InvalidReturnsZero(t *testing.T) {
	t.Parallel()
	if got := routine.NextFire("invalid", time.Now()); !got.IsZero() {
		t.Fatalf("NextFire with invalid schedule = %v, want zero time", got)
	}
}

func TestRoutine_Validate(t *testing.T) {
	t.Parallel()
	base := routine.Routine{ID: "id", Agent: "agent", Schedule: "@daily"}

	// Good baseline
	if err := base.Validate(); err != nil {
		t.Fatalf("baseline validate: %v", err)
	}
	// Missing agent
	r := base
	r.Agent = ""
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "agent is required") {
		t.Fatalf("expected agent required error, got %v", err)
	}
	// Bad catchup
	r = base
	r.Catchup = "bogus"
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "invalid catchup") {
		t.Fatalf("expected invalid catchup error, got %v", err)
	}
	// Negative cost
	r = base
	r.MaxCost = -1
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "max_cost must not be negative") {
		t.Fatalf("expected negative cost error, got %v", err)
	}
	// Negative iterations
	r = base
	r.MaxIterations = -1
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "max_iterations must not be negative") {
		t.Fatalf("expected negative iterations error, got %v", err)
	}
	// Provider constraint
	r = base
	r.Provider = "openai-compat"
	r.BaseURL = ""
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "requires base_url") {
		t.Fatalf("expected base_url required error, got %v", err)
	}
}

func TestRoutine_EffectiveCatchup(t *testing.T) {
	t.Parallel()
	r := &routine.Routine{}
	if got := r.EffectiveCatchup(); got != routine.DefaultCatchup {
		t.Fatalf("EffectiveCatchup default = %q, want %q", got, routine.DefaultCatchup)
	}
	r.Catchup = routine.CatchupSkip
	if got := r.EffectiveCatchup(); got != routine.CatchupSkip {
		t.Fatalf("EffectiveCatchup set = %q, want %q", got, routine.CatchupSkip)
	}
}

func TestLoadRoutine_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "job.yaml")
	content := "id: job\nagent: x\nschedule: \"@daily\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := routine.LoadRoutine(path)
	if err != nil {
		t.Fatalf("LoadRoutine: %v", err)
	}
	if r.ID != "job" || r.Agent != "x" {
		t.Fatalf("unexpected routine fields: %+v", r)
	}
}

func TestLoadRoutine_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("not: [yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("id: x\nschedule: \"@daily\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"missing", filepath.Join(dir, "missing.yaml"), "read routine"},
		{"parse", bad, "parse routine"},
		{"validate", invalid, "validate routine"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := routine.LoadRoutine(tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestIDFromFileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"round-trip", routine.FileName("abc-1"), "abc-1"},
		{"non-yaml", "not-yaml.txt", ""},
		{"invalid-id", "Bad_", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routine.IDFromFileName(tt.in); got != tt.want {
				t.Fatalf("IDFromFileName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
