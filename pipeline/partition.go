package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
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

	if !doublestar.ValidatePattern(p.Glob) {
		return nil, fmt.Errorf("invalid partition glob %q", p.Glob)
	}

	matches, err := collectMatches(workDir, p.Glob)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}

	return splitIntoPartitions(matches, maxPer), nil
}

// collectMatches walks workDir and returns relative paths matching the pattern.
func collectMatches(workDir string, pattern string) ([]string, error) {
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
		matched, _ := doublestar.Match(pattern, rel)
		if matched {
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
