package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Discover scans a sessions root (e.g. ".squad/sessions") and returns
// a Tailer for each direct child directory. Children without a meta.json
// or events.jsonl are still returned — the Tailer will yield zero state
// until those files appear.
//
// Results are sorted by directory name descending, which (for the squad
// session ID format) puts the newest first.
func Discover(sessionsRoot string) ([]*Tailer, error) {
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions root %s: %w", sessionsRoot, err)
	}

	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirs = append(dirs, e.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))

	tailers := make([]*Tailer, 0, len(dirs))
	for _, name := range dirs {
		tailers = append(tailers, NewTailer(filepath.Join(sessionsRoot, name)))
	}
	return tailers, nil
}
