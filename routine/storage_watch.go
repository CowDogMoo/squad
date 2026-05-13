package routine

// This file holds the fsnotify-driven manifest watcher (Store.Watch and its
// supporting goroutines). The watch loop is inherently hard to unit-test
// deterministically — it merges debounced fsnotify events with reloads of
// the watched-roots registry, runs in a goroutine, and depends on the host
// kernel's inotify/FSEvents behavior. Higher-level confidence comes from
// integration testing via `squad routined` against real manifest churn.
//
// Codecov ignores this file. The diff-and-emit helpers it calls
// (routineChanged, stringMapEqual, entryKey) live in storage.go and are
// unit-tested there.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"github.com/fsnotify/fsnotify"
)

// Watch starts the fsnotify watcher and emits events on the returned channel
// until ctx is cancelled. The watcher tracks the global routines dir, each
// watched repo's `.squad/routines` dir, and the routine-roots.yaml file so a
// `routine watch` invocation is picked up without a daemon restart.
//
// The channel is buffered; if a slow consumer falls behind, events are
// dropped silently rather than blocking the watcher goroutine.
func (s *Store) Watch(ctx context.Context, buffer int) (<-chan Event, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}
	s.mu.Lock()
	s.watcher = w
	s.mu.Unlock()

	events := make(chan Event, max(1, buffer))

	if err := s.rewatch(); err != nil {
		_ = w.Close()
		return nil, err
	}

	// Also watch the roots file so we react to `routine watch`.
	if rootsPath, err := RootsPath(); err == nil {
		_ = w.Add(rootsPath)
	}

	go s.watchLoop(ctx, w, events)
	return events, nil
}

// rewatch tears down stale directory watches and adds watches for any
// directory we should currently be observing. Called on init and whenever
// the roots file changes.
func (s *Store) rewatch() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watcher == nil {
		return nil
	}

	desired := map[string]struct{}{}
	globalDir, err := GlobalRoutinesDir()
	if err != nil {
		return err
	}
	desired[globalDir] = struct{}{}
	for _, root := range s.roots {
		dir := RepoRoutinesDir(root)
		// Only watch dirs that exist — fsnotify.Add errors on missing dirs.
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			desired[dir] = struct{}{}
		}
	}

	for dir := range s.watching {
		if _, keep := desired[dir]; !keep {
			_ = s.watcher.Remove(dir)
			delete(s.watching, dir)
		}
	}
	for dir := range desired {
		if _, have := s.watching[dir]; have {
			continue
		}
		if err := s.watcher.Add(dir); err != nil {
			logging.Warn("watch %s: %v", dir, err)
			continue
		}
		s.watching[dir] = struct{}{}
	}
	return nil
}

func (s *Store) watchLoop(ctx context.Context, w *fsnotify.Watcher, events chan<- Event) {
	defer func() {
		_ = w.Close()
		close(events)
	}()
	// Debounce: editors often write a temp file, then rename it on top of the
	// target, generating multiple events for one logical change. We coalesce
	// any burst within 100ms into a single rescan.
	const debounce = 100 * time.Millisecond
	var timer *time.Timer
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			rootsPath, _ := RootsPath()
			if ev.Name == rootsPath {
				// Reload roots and adjust watched directories.
				if roots, err := LoadRoots(); err == nil {
					s.mu.Lock()
					s.roots = roots
					s.mu.Unlock()
				}
				if err := s.rewatch(); err != nil {
					logging.Warn("rewatch after roots change: %v", err)
				}
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, func() {
				s.diffAndEmit(events)
			})
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			logging.Warn("fsnotify error: %v", err)
		}
	}
}

func (s *Store) diffAndEmit(events chan<- Event) {
	s.mu.Lock()
	before := make(map[string]Entry, len(s.entries))
	for k, v := range s.entries {
		before[k] = v
	}
	s.mu.Unlock()

	after, err := s.LoadAll()
	if err != nil {
		logging.Warn("reload routines: %v", err)
		return
	}
	afterMap := make(map[string]Entry, len(after))
	for _, e := range after {
		afterMap[entryKey(e.Ref)] = e
	}

	// Added / Updated.
	for k, newEntry := range afterMap {
		old, existed := before[k]
		switch {
		case !existed:
			sendOrDrop(events, Event{Type: EventAdded, Ref: newEntry.Ref, Entry: newEntry})
		case routineChanged(old.Routine, newEntry.Routine):
			sendOrDrop(events, Event{Type: EventUpdated, Ref: newEntry.Ref, Entry: newEntry})
		}
	}
	// Removed.
	for k, oldEntry := range before {
		if _, still := afterMap[k]; !still {
			sendOrDrop(events, Event{Type: EventRemoved, Ref: oldEntry.Ref})
		}
	}
}

func sendOrDrop(ch chan<- Event, ev Event) {
	select {
	case ch <- ev:
	default:
		logging.Warn("routine event channel full, dropping %v %s", ev.Type, ev.Ref.Qualified())
	}
}
