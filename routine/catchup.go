package routine

import (
	"context"
	"time"

	"github.com/cowdogmoo/squad/logging"
)

// MissedFire describes a routine whose schedule fired one or more times while
// the daemon was not running.
type MissedFire struct {
	Ref    Ref
	Last   time.Time // last recorded run; may be zero
	Missed time.Time // most recent missed fire time
}

// FindMissedFires inspects every entry's state file and returns the set of
// routines that have at least one missed scheduled fire since their LastRun
// (or since CreatedAt, if no run is recorded yet but the routine has existed
// long enough to fire). Disabled routines and ones with Catchup == skip are
// excluded.
//
// `now` is taken as a parameter so tests can pin time deterministically.
func FindMissedFires(store *Store, now time.Time) []MissedFire {
	out := []MissedFire{}
	for _, entry := range store.Entries() {
		if !entry.Routine.Enabled {
			continue
		}
		if entry.Routine.EffectiveCatchup() == CatchupSkip {
			continue
		}
		st, err := LoadState(entry.StatePath)
		if err != nil {
			logging.Warn("load state %s: %v", entry.Ref.Qualified(), err)
			continue
		}
		// Anchor for "what fires have happened since": last run, else
		// routine creation, else now (in which case nothing was missed).
		anchor := st.LastRun
		if anchor.IsZero() {
			anchor = entry.Routine.CreatedAt
		}
		if anchor.IsZero() {
			continue
		}
		next := NextFire(entry.Routine.Schedule, anchor)
		if next.IsZero() || !next.Before(now) {
			continue // nothing fired after anchor and before now
		}
		// Find the most recent missed fire — walk forward from `next` while
		// the candidate is before now. The loop is bounded by the schedule
		// frequency vs. (now - anchor); in practice this is at most a few
		// iterations for any reasonable gap.
		latest := next
		for {
			candidate := NextFire(entry.Routine.Schedule, latest)
			if candidate.IsZero() || !candidate.Before(now) {
				break
			}
			latest = candidate
		}
		out = append(out, MissedFire{Ref: entry.Ref, Last: st.LastRun, Missed: latest})
	}
	return out
}

// QueueCatchups asynchronously fires every missed routine through scheduler.
// Each fire happens in its own goroutine so the daemon's Start() doesn't
// block on long-running runs. Caller is responsible for any wait/cancellation
// via ctx.
func QueueCatchups(ctx context.Context, sched *Scheduler, misses []MissedFire) {
	for _, m := range misses {
		miss := m
		go func() {
			logging.Info("catchup: firing %s (missed at %s)", miss.Ref.Qualified(), miss.Missed.Format(time.RFC3339))
			if err := sched.RunNow(ctx, miss.Ref); err != nil {
				logging.Warn("catchup fire %s: %v", miss.Ref.Qualified(), err)
			}
		}()
	}
}
