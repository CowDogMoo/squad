package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/pipeline"
	"github.com/cowdogmoo/squad/tools"
)

// shardSkipDirs are directories never globbed when building a shard work-list.
// Mirrors pipeline's partition skip set.
var shardSkipDirs = map[string]bool{
	".git": true, ".venv": true, "venv": true, "node_modules": true,
	"vendor": true, "__pycache__": true, ".tox": true, ".mypy_cache": true,
	".pytest_cache": true, ".ruff_cache": true, "target": true,
	"build": true, "dist": true, ".claude": true,
}

// shardOutcome is one shard's result in the merged report.
type shardOutcome struct {
	Files        []string `json:"files"`
	EditsApplied bool     `json:"edits_applied"`
	Error        string   `json:"error,omitempty"`
	Report       string   `json:"report"`
}

// shardSummary is the roll-up across all shards.
type shardSummary struct {
	Files  int `json:"files"`
	Shards int `json:"shards"`
	Edited int `json:"edited"`
	Clean  int `json:"clean"`
	Failed int `json:"failed"`
}

// shardedResult is the structured merge of a sharded run.
type shardedResult struct {
	Agent   string         `json:"agent"`
	Mode    string         `json:"mode"`
	Summary shardSummary   `json:"summary"`
	Shards  []shardOutcome `json:"shards"`
}

// runSharded executes a leaf agent that declared an execution.shard_by block.
// It globs the work-list, splits it into shards of exec.ShardBatch files, and
// runs the normal single-agent loop once per shard with FRESH context — so no
// single run accumulates the whole repo. Edits are applied live per shard by
// the agent's own Edit tool calls; this function aggregates metrics and merges
// the per-shard reports. Shards run through a bounded worker pool sized by
// exec.MaxParallel (1 = sequential).
func runSharded(ctx context.Context, cmd *cobra.Command, opts *RunOptions, bundle *agent.Bundle, prompt, workingDir string) (string, *metrics.Metrics, error) {
	exec := bundle.Execution
	parent := metrics.New(opts.Provider, opts.Model)

	files, err := globShardFiles(workingDir, exec.Glob, exec.Exclude)
	if err != nil {
		return "", parent, fmt.Errorf("shard globbing failed: %w", err)
	}
	shards := batchFiles(files, exec.ShardBatch)
	logging.InfoContext(ctx, "sharded run: %d files → %d shards (batch=%d)", len(files), len(shards), exec.ShardBatch)

	if len(shards) == 0 {
		// No matching files is a correct, clean outcome.
		res := shardedResult{Agent: opts.Agent, Mode: opts.Mode, Summary: shardSummary{}}
		return mergeShards(res, exec.Merge), parent, nil
	}

	bundleOpts := &agent.BundleOptions{
		SkillOverrides: opts.SkillOverrides,
		CatalogPaths:   resolveSkillCatalogPaths(opts.Config),
	}

	// Bounded worker pool. MaxParallel == 1 is effectively sequential. Each
	// shard gets its OWN edit-tracker (tools.InitEdits per shard) so concurrent
	// shards don't race on the shared tracker; partitions are disjoint files,
	// so concurrent edits never touch the same file.
	results := make([]shardOutcome, len(shards))
	childMetrics := make([]*metrics.Metrics, len(shards))
	sem := make(chan struct{}, exec.MaxParallel)
	var wg sync.WaitGroup
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stopOnce sync.Once
	stop := func() {
		if exec.OnShardError == "stop" {
			stopOnce.Do(cancel)
		}
	}
	logging.InfoContext(ctx, "running %d shards (max_parallel=%d)", len(shards), exec.MaxParallel)

	for i, shard := range shards {
		wg.Add(1)
		go func(idx int, files []string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if runCtx.Err() != nil {
				results[idx] = shardOutcome{Files: files, Error: "skipped: an earlier shard failed (on_shard_error=stop)"}
				return
			}

			oc, sm := runOneShard(runCtx, opts, bundleOpts, files, idx, len(shards), prompt, workingDir)
			results[idx] = oc
			childMetrics[idx] = sm
			if oc.Error != "" {
				logging.InfoContext(ctx, "  ✗ shard %d/%d (%s): %s", idx+1, len(shards), strings.Join(files, ", "), oc.Error)
				stop()
			}
		}(i, shard)
	}
	wg.Wait()

	// Aggregate in deterministic order: metrics rolled up as children, the
	// first error preserved for the run's exit status.
	outcomes := make([]shardOutcome, len(shards))
	var firstErr error
	for i := range shards {
		if childMetrics[i] != nil {
			parent.AddChild(fmt.Sprintf("shard-%02d", i+1), childMetrics[i])
		}
		outcomes[i] = results[i]
		if outcomes[i].Error != "" && firstErr == nil {
			firstErr = fmt.Errorf("shard %d (%s): %s", i+1, strings.Join(outcomes[i].Files, ", "), outcomes[i].Error)
		}
	}

	res := summarize(opts, outcomes)
	merged := mergeShards(res, exec.Merge)

	// Aggregate failures fail the run honestly (non-zero exit), even in
	// "continue" mode — continue only means other shards still ran.
	if res.Summary.Failed > 0 {
		return merged, parent, fmt.Errorf("%d of %d shards failed: %w", res.Summary.Failed, len(shards), firstErr)
	}
	return merged, parent, nil
}

// runOneShard builds the shard's bundle, runs the single-agent loop in an
// isolated edit-tracker context, and classifies the outcome. Returning the
// outcome + metrics keeps the goroutine in runSharded small.
func runOneShard(runCtx context.Context, opts *RunOptions, bundleOpts *agent.BundleOptions, files []string, idx, total int, prompt, workingDir string) (shardOutcome, *metrics.Metrics) {
	shardPrompt := pipeline.FormatPartitionPrompt(files, idx+1, total) + "\n\n" + prompt
	shardBundle, buildErr := agent.BuildBundleWithOptions(opts.AgentsDir, opts.Agent, shardPrompt, workingDir, opts.Mode, opts.Vars, bundleOpts)
	if buildErr != nil {
		return shardOutcome{Files: files, Error: buildErr.Error()}, nil
	}

	shardCtx := tools.InitEdits(runCtx) // isolated edit-tracker per shard
	resp, sm, runErr := InvokeModel(shardCtx, opts, shardBundle)
	editsApplied := tools.EditsApplied(shardCtx)
	oc := shardOutcome{Files: files, EditsApplied: editsApplied, Report: resp}
	switch {
	case runErr != nil:
		oc.Error = runErr.Error()
	case opts.RequireActionable:
		if vErr := validateShard(resp, editsApplied); vErr != nil {
			oc.Error = vErr.Error()
		}
	}
	return oc, sm
}

// validateShard is the per-shard fabrication check. The edit-tracker (reset
// before each shard) is authoritative ground truth, so it is reliable across
// sequential shards in a way the whole-tree git check is not: a shard that
// claims specific files were touched but recorded no edit is a fabrication.
func validateShard(report string, editsApplied bool) error {
	if editsApplied {
		return nil
	}
	if claimsSpecificFilesTouched(report) {
		return fmt.Errorf("report claims files touched but no edit was applied in this shard")
	}
	return nil
}

// globShardFiles builds the shard work-list: the union of include globs
// (relative to workingDir), minus excludes and skip dirs, sorted and deduped.
func globShardFiles(workingDir string, include, exclude []string) ([]string, error) {
	fsys := os.DirFS(workingDir)
	seen := map[string]bool{}
	var out []string
	for _, pattern := range include {
		matches, err := doublestar.Glob(fsys, pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid glob %q: %w", pattern, err)
		}
		for _, rel := range matches {
			if seen[rel] || skipPath(rel) || matchesAny(rel, exclude) {
				continue
			}
			info, statErr := fs.Stat(fsys, rel)
			if statErr != nil || info.IsDir() {
				continue
			}
			seen[rel] = true
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

// skipPath reports whether any path segment is a skipped directory. rel comes
// from an io/fs glob, so it is always "/"-separated regardless of OS.
func skipPath(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if shardSkipDirs[seg] {
			return true
		}
	}
	return false
}

// matchesAny reports whether rel matches any of the exclude globs.
func matchesAny(rel string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := doublestar.Match(p, rel); ok {
			return true
		}
	}
	return false
}

// batchFiles splits files into shards of at most size entries (size >= 1).
func batchFiles(files []string, size int) [][]string {
	if size < 1 {
		size = 1
	}
	var shards [][]string
	for i := 0; i < len(files); i += size {
		end := i + size
		if end > len(files) {
			end = len(files)
		}
		shards = append(shards, files[i:end])
	}
	return shards
}

// summarize rolls per-shard outcomes into a shardedResult.
func summarize(opts *RunOptions, outcomes []shardOutcome) shardedResult {
	res := shardedResult{Agent: opts.Agent, Mode: opts.Mode, Shards: outcomes}
	res.Summary.Shards = len(outcomes)
	for _, oc := range outcomes {
		res.Summary.Files += len(oc.Files)
		switch {
		case oc.Error != "":
			res.Summary.Failed++
		case oc.EditsApplied:
			res.Summary.Edited++
		default:
			res.Summary.Clean++
		}
	}
	return res
}

// mergeShards renders the merged output. "json" emits the structured aggregate;
// "none" concatenates the per-shard reports verbatim under file headers.
func mergeShards(res shardedResult, mode string) string {
	if mode == "none" {
		var sb strings.Builder
		for _, oc := range res.Shards {
			fmt.Fprintf(&sb, "## %s\n\n%s\n\n---\n\n", strings.Join(oc.Files, ", "), oc.Report)
		}
		fmt.Fprintf(&sb, "Files touched: %s\n", touchedList(res))
		return sb.String()
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\":\"merge failed: %v\"}", err)
	}
	return string(b)
}

// touchedList returns a human "files touched" line for the "none" merge.
func touchedList(res shardedResult) string {
	var touched []string
	for _, oc := range res.Shards {
		if oc.EditsApplied {
			touched = append(touched, oc.Files...)
		}
	}
	if len(touched) == 0 {
		return "none"
	}
	return strings.Join(touched, ", ")
}
