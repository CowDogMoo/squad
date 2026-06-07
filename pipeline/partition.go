package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// defaultMaxPerPartition is the default number of files per partition.
	defaultMaxPerPartition = 25

	// maxPartitions caps the number of parallel agent instances to
	// prevent runaway resource consumption.
	maxPartitions = 10
)

// defaultSkipDirs mirrors the skip set from tools — directories that
// should never be globbed during partitioning.
var defaultSkipDirs = map[string]bool{
	".venv":         true,
	"venv":          true,
	"__pycache__":   true,
	".tox":          true,
	"node_modules":  true,
	".git":          true,
	".mypy_cache":   true,
	".pytest_cache": true,
	".ruff_cache":   true,
	".eggs":         true,
	"target":        true,
	"build":         true,
	"dist":          true,
	"vendor":        true,
}

// ExpandPartition globs the working directory using the partition config
// and splits matching files into groups. Each group becomes one agent
// instance's file list.
func ExpandPartition(workDir string, p *Partition) ([][]string, error) {
	if p == nil || p.Glob == "" {
		return nil, fmt.Errorf("partition glob is required")
	}

	maxPer := p.MaxPerPartition
	if maxPer <= 0 {
		maxPer = defaultMaxPerPartition
	}

	matcher, err := partitionGlobToRegex(p.Glob)
	if err != nil {
		return nil, fmt.Errorf("invalid partition glob %q: %w", p.Glob, err)
	}

	matches, err := collectMatches(workDir, matcher)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}

	return splitIntoPartitions(matches, maxPer), nil
}

// collectMatches walks workDir and returns relative paths matching the regex.
func collectMatches(workDir string, matcher *regexp.Regexp) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if defaultSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(workDir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if matcher.MatchString(rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("partition glob walk failed: %w", err)
	}
	return matches, nil
}

// splitIntoPartitions divides matches into groups of maxPer, capping at maxPartitions.
func splitIntoPartitions(matches []string, maxPer int) [][]string {
	var partitions [][]string
	for i := 0; i < len(matches); i += maxPer {
		end := i + maxPer
		if end > len(matches) {
			end = len(matches)
		}
		partitions = append(partitions, matches[i:end])
	}

	if len(partitions) > maxPartitions {
		merged := partitions[maxPartitions-1]
		for _, extra := range partitions[maxPartitions:] {
			merged = append(merged, extra...)
		}
		partitions = partitions[:maxPartitions]
		partitions[maxPartitions-1] = merged
	}

	return partitions
}

// FormatPartitionPrompt creates the prompt prefix that tells an agent
// which files to focus on within its partition.
func FormatPartitionPrompt(files []string, partIdx, totalParts int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Partition Assignment (%d of %d)\n\n", partIdx, totalParts)
	sb.WriteString("You are reviewing a subset of the codebase. Focus ONLY on these files:\n\n")
	for _, f := range files {
		fmt.Fprintf(&sb, "- %s\n", f)
	}
	sb.WriteString("\nDo NOT read or analyze files outside this list.\n")
	sb.WriteString("Other partitions are handling the remaining files in parallel.\n")
	return sb.String()
}

// partitionGlobToRegex converts a glob pattern to a compiled regex.
// Supports ** (any path, including zero segments) and * (any non-slash segment).
// The pattern **/ matches zero or more directories (including the root level).
func partitionGlobToRegex(pattern string) (*regexp.Regexp, error) {
	normalized := filepath.ToSlash(pattern)
	var buf strings.Builder
	buf.WriteString("^")
	runes := []rune(normalized)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if ch == '*' {
			if i+1 < len(runes) && runes[i+1] == '*' {
				// **/ matches zero or more directory segments
				if i+2 < len(runes) && runes[i+2] == '/' {
					buf.WriteString("(.*/)?")
					i += 2 // skip ** and /
				} else {
					buf.WriteString(".*")
					i++ // skip second *
				}
			} else {
				buf.WriteString(`[^/]*`)
			}
			continue
		}
		if ch == '?' {
			buf.WriteString(".")
			continue
		}
		if strings.ContainsRune(`.+()|[]{}^$\\`, ch) {
			buf.WriteString(`\`)
		}
		buf.WriteRune(ch)
	}
	buf.WriteString("$")
	return regexp.Compile(buf.String())
}
