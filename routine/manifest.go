// Package routine implements scheduled, unattended agent runs.
//
// A Routine is a saved agent invocation (agent + prompt + flags + working dir)
// paired with a cron-like schedule. The squad daemon (squad routined) registers
// each routine with a gocron scheduler and fires it as its schedule comes due.
//
// Routines live in two scopes:
//
//   - Global: $XDG_CONFIG_HOME/squad/routines/<id>.yaml (user-level, cross-repo)
//   - Per-repo: <repo>/.squad/routines/<id>.yaml (checked into git)
//
// Manifests are user-authored / checked into git. Daemon-written status
// (last_run, last_status, last_session_id) lives in a sibling state file owned
// by the daemon so manifests stay clean for version control.
package routine

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// CatchupPolicy controls what the daemon does on startup when a routine has
// missed one or more scheduled fires while the daemon was not running.
type CatchupPolicy string

const (
	// CatchupFireOnce queues exactly one immediate fire when any miss is
	// detected since the last recorded run. Matches systemd Persistent=true.
	CatchupFireOnce CatchupPolicy = "fire-once"
	// CatchupSkip ignores missed fires and waits for the next scheduled time.
	CatchupSkip CatchupPolicy = "skip"
)

// DefaultCatchup is the catch-up policy applied when a routine omits the field.
const DefaultCatchup = CatchupFireOnce

// Routine is the user-authored, on-disk definition of a scheduled agent run.
// It carries no daemon-mutable state — that lives in the sibling state file
// managed by [State].
type Routine struct {
	// ID is a user-supplied slug, unique within its scope.
	ID string `yaml:"id"`
	// Agent is the name of the agent to invoke (resolved like `squad run --agent`).
	Agent string `yaml:"agent"`
	// Schedule is either a standard 5-field cron expression / robfig predefined
	// (e.g. "@daily") or an "@every <duration>" form.
	Schedule string `yaml:"schedule"`
	// Prompt is the optional user prompt passed to the agent.
	Prompt string `yaml:"prompt,omitempty"`
	// WorkingDir is the directory the agent operates in. Required for global
	// routines; per-repo routines default to the containing repo root.
	WorkingDir string `yaml:"working_dir,omitempty"`
	// Provider overrides the default provider for this routine.
	Provider string `yaml:"provider,omitempty"`
	// Model overrides the default model for this routine.
	Model string `yaml:"model,omitempty"`
	// MaxCost caps the total USD spend for one fire (0 = inherit default).
	MaxCost float64 `yaml:"max_cost,omitempty"`
	// MaxIterations caps the per-fire tool-call iterations (0 = inherit default).
	MaxIterations int `yaml:"max_iterations,omitempty"`
	// Vars are passed to the agent prompt template (--var KEY=VALUE).
	Vars map[string]string `yaml:"vars,omitempty"`
	// Enabled gates the routine. Disabled routines load but never fire.
	Enabled bool `yaml:"enabled"`
	// WakeSystem opts in to waking the machine from sleep to fire (macOS/Windows
	// only; Linux user services cannot wake the system).
	WakeSystem bool `yaml:"wake_system,omitempty"`
	// Catchup controls missed-fire behavior when the daemon starts after a gap.
	Catchup CatchupPolicy `yaml:"catchup,omitempty"`
	// CreatedAt records when the routine was first created (informational).
	CreatedAt time.Time `yaml:"created_at,omitempty"`
}

// slugPattern enforces a lowercase-slug ID: must start with a letter, may
// contain letters / digits / single hyphens, must not end with a hyphen,
// length 1-64.
var slugPattern = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,62}[a-z0-9])?$`)

// scheduleParser is the robfig cron parser used to validate Schedule strings.
// It accepts standard 5-field expressions plus predefined descriptors and the
// "@every <duration>" form, which is the same surface gocron's CronJob accepts.
var scheduleParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ValidateID reports whether s is a valid routine ID slug.
func ValidateID(s string) error {
	if s == "" {
		return errors.New("id is required")
	}
	if len(s) > 64 {
		return fmt.Errorf("id %q exceeds 64 characters", s)
	}
	if !slugPattern.MatchString(s) {
		return fmt.Errorf("id %q is invalid: must be lowercase letters/digits/hyphens, start with a letter, not end with a hyphen", s)
	}
	return nil
}

// ValidateSchedule parses s as a cron expression or "@every" duration and
// returns an error if it cannot be scheduled.
func ValidateSchedule(s string) error {
	if s == "" {
		return errors.New("schedule is required")
	}
	if _, err := scheduleParser.Parse(s); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", s, err)
	}
	return nil
}

// NextFire returns the next time the schedule would fire after `from`.
// Returns zero time if the schedule cannot be parsed (caller should have
// validated first).
func NextFire(schedule string, from time.Time) time.Time {
	sched, err := scheduleParser.Parse(schedule)
	if err != nil {
		return time.Time{}
	}
	return sched.Next(from)
}

// Validate checks every field on the routine. It does not touch the filesystem
// or verify the agent exists — that is the responsibility of higher-level
// validation in storage.go (which has the scope/working-dir context).
func (r *Routine) Validate() error {
	if err := ValidateID(r.ID); err != nil {
		return err
	}
	if r.Agent == "" {
		return errors.New("agent is required")
	}
	if err := ValidateSchedule(r.Schedule); err != nil {
		return err
	}
	switch r.Catchup {
	case "", CatchupFireOnce, CatchupSkip:
	default:
		return fmt.Errorf("invalid catchup %q (must be %q or %q)", r.Catchup, CatchupFireOnce, CatchupSkip)
	}
	if r.MaxCost < 0 {
		return fmt.Errorf("max_cost must not be negative")
	}
	if r.MaxIterations < 0 {
		return fmt.Errorf("max_iterations must not be negative")
	}
	return nil
}

// EffectiveCatchup returns the catchup policy with the default substituted in
// when the routine left it blank.
func (r *Routine) EffectiveCatchup() CatchupPolicy {
	if r.Catchup == "" {
		return DefaultCatchup
	}
	return r.Catchup
}

// LoadRoutine reads and parses a routine YAML file at path. It does not apply
// scope-specific defaults (e.g. working_dir = repo root) — that happens in
// storage.go where the scope is known.
func LoadRoutine(path string) (*Routine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read routine %s: %w", path, err)
	}
	r := &Routine{}
	if err := yaml.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("parse routine %s: %w", path, err)
	}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("validate routine %s: %w", path, err)
	}
	return r, nil
}

// FileName returns the standard manifest filename for a routine id.
func FileName(id string) string {
	return id + ".yaml"
}

// IDFromFileName returns the routine id encoded in a manifest filename, or
// the empty string if the filename does not match the routine convention.
func IDFromFileName(name string) string {
	if !strings.HasSuffix(name, ".yaml") {
		return ""
	}
	id := strings.TrimSuffix(name, ".yaml")
	if ValidateID(id) != nil {
		return ""
	}
	return id
}
