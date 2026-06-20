package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/cowdogmoo/squad/config"
)

// List returns a deduplicated, reverse-chronological list of sessions for a
// repository. When repoDir is empty every session is returned.
//
// meta.json is the source of truth: it carries the live status, so scanning it
// keeps List correct even when the append-only index.jsonl is missing or stale
// (the index records status only at creation time). The index is consulted only
// as a recovery source for sessions whose meta.json can no longer be read.
func List(repoDir string) ([]IndexEntry, error) {
	sessionMap := make(map[string]IndexEntry)

	// 1. Authoritative: scan the global XDG sessions/<id>/meta.json files. The
	//    flat directory mixes every repo's sessions, so filter by canonical
	//    repo path.
	if stateDir := config.StateDir(); stateDir != "" {
		scanSessionMetas(filepath.Join(stateDir, "sessions"), repoDir, "", sessionMap)
	}

	// 2. Recovery: fold in index.jsonl entries we did not find on disk. This
	//    makes the index a rebuildable cache rather than the source of truth.
	if stateDir := config.StateDir(); stateDir != "" {
		indexFile := filepath.Join(stateDir, "index.jsonl")
		if f, err := os.Open(indexFile); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				var entry IndexEntry
				if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
					continue
				}
				if repoDir != "" && entry.CanonicalRepoPath != repoDir {
					continue
				}
				if _, exists := sessionMap[entry.SessionID]; !exists {
					sessionMap[entry.SessionID] = entry
				}
			}
			_ = f.Close()
		}
	}

	// 3. Legacy in-tree .squad/sessions/ for this repo. The directory itself
	//    scopes these to repoDir, so do not filter by canonical path (sessions
	//    written before this change predate the CanonicalRepoPath field);
	//    backfill it instead.
	if repoDir != "" {
		scanSessionMetas(filepath.Join(repoDir, SessionsRoot), "", repoDir, sessionMap)
	}

	results := make([]IndexEntry, 0, len(sessionMap))
	for _, entry := range sessionMap {
		results = append(results, entry)
	}

	// Reverse-chronological (newest first).
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp > results[j].Timestamp
	})

	return results, nil
}

// scanSessionMetas reads every <dir>/<id>/meta.json under dir and records an
// entry for each session. When repoFilter is non-empty only sessions whose
// CanonicalRepoPath matches it are kept. When an entry has no CanonicalRepoPath
// (sessions predating the field) defaultRepo is used in its place. Existing map
// entries win, so callers can layer authoritative sources first.
func scanSessionMetas(dir, repoFilter, defaultRepo string, sessionMap map[string]IndexEntry) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if _, exists := sessionMap[id]; exists {
			continue
		}
		metaData, err := os.ReadFile(filepath.Join(dir, id, "meta.json"))
		if err != nil {
			continue
		}
		var meta Meta
		if err := json.Unmarshal(metaData, &meta); err != nil {
			continue
		}
		if repoFilter != "" && meta.CanonicalRepoPath != repoFilter {
			continue
		}
		canonical := meta.CanonicalRepoPath
		if canonical == "" {
			canonical = defaultRepo
		}
		sessionMap[id] = IndexEntry{
			SessionID:         meta.SessionID,
			CanonicalRepoPath: canonical,
			WorktreePath:      meta.WorktreePath,
			Timestamp:         meta.Created.Format("20060102T150405Z"),
			Status:            meta.Status,
		}
	}
}
