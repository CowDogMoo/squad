package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cowdogmoo/squad/agent"
)

func TestBatchFiles(t *testing.T) {
	t.Parallel()
	files := []string{"a", "b", "c", "d", "e"}
	tests := []struct {
		size int
		want int // number of shards
	}{
		{1, 5},
		{2, 3}, // [a,b][c,d][e]
		{5, 1},
		{10, 1},
		{0, 5}, // size < 1 coerced to 1
	}
	for _, tt := range tests {
		got := batchFiles(files, tt.size)
		if len(got) != tt.want {
			t.Errorf("batchFiles(size=%d) = %d shards, want %d", tt.size, len(got), tt.want)
		}
	}
	// Every file appears exactly once across shards.
	shards := batchFiles(files, 2)
	var flat []string
	for _, s := range shards {
		flat = append(flat, s...)
	}
	if strings.Join(flat, ",") != "a,b,c,d,e" {
		t.Errorf("batchFiles lost/reordered files: %v", flat)
	}
}

func TestGroupByPackage(t *testing.T) {
	t.Parallel()
	// Two files in agent/, one each in runner/ and the repo root: three
	// packages → three shards, each holding exactly its directory's files.
	files := []string{"agent/bundle.go", "agent/composed.go", "main.go", "runner/run.go"}
	got := groupByPackage(files)
	want := [][]string{
		{"main.go"}, // dir "." sorts before "agent" and "runner"
		{"agent/bundle.go", "agent/composed.go"},
		{"runner/run.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("groupByPackage = %d shards, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Errorf("shard %d = %v, want %v", i, got[i], want[i])
		}
	}

	// shardFiles dispatches on ShardBy: "package" groups by directory, the
	// default path batches by count.
	pkgShards := shardFiles(files, &agent.ExecutionConfig{ShardBy: "package"})
	if len(pkgShards) != 3 {
		t.Errorf("shardFiles(package) = %d shards, want 3", len(pkgShards))
	}
	fileShards := shardFiles(files, &agent.ExecutionConfig{ShardBy: "file", ShardBatch: 1})
	if len(fileShards) != 4 {
		t.Errorf("shardFiles(file, batch=1) = %d shards, want 4", len(fileShards))
	}
}

func TestGlobShardFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	must := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("README.md", "x")
	must("docs/a.md", "x")
	must("docs/b.txt", "x")
	must("node_modules/pkg/c.md", "x") // skipped dir
	must("vendor/d.md", "x")           // skipped dir
	must("generated.md", "x")          // excluded by pattern

	got, err := globShardFiles(dir, []string{"**/*.md", "**/*.txt"}, []string{"generated.md"})
	if err != nil {
		t.Fatalf("globShardFiles: %v", err)
	}
	want := []string{"README.md", "docs/a.md", "docs/b.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("globShardFiles = %v, want %v", got, want)
	}
}

func TestValidateShard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		report  string
		edits   bool
		wantErr bool
	}{
		{"edits applied passes", "Files touched: a.md", true, false},
		{"claim without edit is fabrication", "Files touched: a.md", false, true},
		{"no claim and no edit passes", "Files touched: none\nNo changes", false, false},
		{"clean report passes", "No tells found.", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateShard(tt.report, tt.edits)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateShard() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSummarizeAndMerge(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{Agent: "degpt", Mode: "edit"}
	outcomes := []shardOutcome{
		{Files: []string{"a.md"}, EditsApplied: true, Report: "rewrote a"},
		{Files: []string{"b.md"}, EditsApplied: false, Report: "clean"},
		{Files: []string{"c.md"}, Error: "boom", Report: "x"},
	}
	res := summarize(opts, outcomes)
	if res.Summary.Files != 3 || res.Summary.Edited != 1 || res.Summary.Clean != 1 || res.Summary.Failed != 1 {
		t.Fatalf("summary = %+v", res.Summary)
	}

	jsonOut := mergeShards(res, "json")
	for _, want := range []string{`"agent": "degpt"`, `"edited": 1`, `"failed": 1`, `a.md`} {
		if !strings.Contains(jsonOut, want) {
			t.Errorf("json merge missing %q in:\n%s", want, jsonOut)
		}
	}

	noneOut := mergeShards(res, "none")
	if !strings.Contains(noneOut, "Files touched: a.md") {
		t.Errorf("none merge missing touched line:\n%s", noneOut)
	}

	// A none merge with nothing edited reports "Files touched: none".
	clean := summarize(opts, []shardOutcome{{Files: []string{"x.md"}, Report: "clean"}})
	if got := mergeShards(clean, "none"); !strings.Contains(got, "Files touched: none") {
		t.Errorf("expected 'Files touched: none', got:\n%s", got)
	}
}

// ollamaShardServer stands up a minimal Ollama-compatible chat endpoint that
// replies to every request with the same assistant content, so a sharded run
// can drive InvokeModel once per shard without a real backend.
func ollamaShardServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	body := fmt.Sprintf(`{"model":"mistral","message":{"role":"assistant","content":%q},"done":true}`, content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeShardedAgent writes a sharded ("shard_by: file") agent manifest globbing
// **/*.md and returns the agents dir. merge/onErr/maxParallel are caller-set so
// tests can exercise the json/none merges and the stop/continue error policies.
func writeShardedAgent(t *testing.T, name, merge, onErr string, maxParallel int) string {
	t.Helper()
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, name)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := fmt.Sprintf(`name: %s
version: 0.0.0
entrypoint: system.md
wrapper: wrapper.md
ascii_only: true
execution:
  shard_by: file
  glob:
    - "**/*.md"
  shard_batch: 1
  merge: %s
  max_parallel: %d
  on_shard_error: %s
`, name, merge, maxParallel, onErr)
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(agentDir, rel), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("agent.yaml", manifest)
	write("system.md", "System prompt.")
	write("wrapper.md", "Wrapper prompt.")
	return agentsDir
}

// writeWorkFiles creates named files under a fresh temp dir used as the sharded
// run's working directory, and returns that directory.
func writeWorkFiles(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("content"), 0o644); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	return dir
}

func buildShardBundle(t *testing.T, opts *RunOptions, workingDir string) *agent.Bundle {
	t.Helper()
	bundle, err := agent.BuildBundleWithOptions(opts.AgentsDir, opts.Agent, "do work", workingDir, opts.Mode, opts.Vars, &agent.BundleOptions{})
	if err != nil {
		t.Fatalf("BuildBundleWithOptions: %v", err)
	}
	if !bundle.Sharded() {
		t.Fatal("expected sharded bundle")
	}
	return bundle
}

func TestRunSharded_EndToEnd(t *testing.T) {
	srv := ollamaShardServer(t, "ok")
	agentsDir := writeShardedAgent(t, "degpt", "json", "continue", 2)
	workingDir := writeWorkFiles(t, "a.md", "b.md")
	opts := &RunOptions{
		Agent:           "degpt",
		AgentsDir:       agentsDir,
		Provider:        "ollama",
		Model:           "mistral",
		BaseURL:         srv.URL,
		MaxIterations:   1,
		Mode:            "edit",
		ConfigAvailable: true,
	}
	bundle := buildShardBundle(t, opts, workingDir)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	resp, m, err := runSharded(context.Background(), cmd, opts, bundle, "do work", workingDir)
	if err != nil {
		t.Fatalf("runSharded: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil metrics")
	}
	var res shardedResult
	if jerr := json.Unmarshal([]byte(resp), &res); jerr != nil {
		t.Fatalf("merged output not valid json: %v\n%s", jerr, resp)
	}
	if res.Summary.Files != 2 || res.Summary.Shards != 2 || res.Summary.Clean != 2 || res.Summary.Failed != 0 {
		t.Fatalf("summary = %+v", res.Summary)
	}
}

func TestExecuteRun_Sharded(t *testing.T) {
	// Full ExecuteRun path for a sharded agent: exercises invokeAndHandle's
	// sharded branch (runSharded + writeResponse of the merged report) and
	// initRunContext's ascii-only activation.
	srv := ollamaShardServer(t, "ok")
	agentsDir := writeShardedAgent(t, "degpt", "json", "continue", 1)
	workingDir := writeWorkFiles(t, "a.md")
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	opts := &RunOptions{
		Agent:           "degpt",
		AgentsDir:       agentsDir,
		WorkingDir:      workingDir,
		Provider:        "ollama",
		Model:           "mistral",
		BaseURL:         srv.URL,
		MaxIterations:   1,
		Mode:            "edit",
		ConfigAvailable: true,
	}
	if err := ExecuteRun(cmd, []string{"do work"}, opts); err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
	if !strings.Contains(out.String(), `"agent": "degpt"`) {
		t.Fatalf("expected merged shard report in output, got:\n%s", out.String())
	}
}

func TestRunSharded_NoMatchingFiles(t *testing.T) {
	srv := ollamaShardServer(t, "ok")
	agentsDir := writeShardedAgent(t, "degpt", "json", "continue", 1)
	workingDir := writeWorkFiles(t, "ignored.txt") // no .md files to glob
	opts := &RunOptions{
		Agent:           "degpt",
		AgentsDir:       agentsDir,
		Provider:        "ollama",
		Model:           "mistral",
		BaseURL:         srv.URL,
		MaxIterations:   1,
		Mode:            "edit",
		ConfigAvailable: true,
	}
	bundle := buildShardBundle(t, opts, workingDir)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	resp, _, err := runSharded(context.Background(), cmd, opts, bundle, "do work", workingDir)
	if err != nil {
		t.Fatalf("runSharded with no files should be a clean outcome, got %v", err)
	}
	var res shardedResult
	if jerr := json.Unmarshal([]byte(resp), &res); jerr != nil {
		t.Fatalf("merged output not valid json: %v\n%s", jerr, resp)
	}
	if res.Summary.Shards != 0 || res.Summary.Files != 0 {
		t.Fatalf("expected empty summary, got %+v", res.Summary)
	}
}

func TestRunSharded_ShardFailureStops(t *testing.T) {
	// Every shard gets a fabricated "files touched" report with no real edit;
	// with require-actionable on, the first shard fails validation and (stop
	// policy) cancels the rest, so the second shard records a skip.
	srv := ollamaShardServer(t, "Files touched: a.md")
	agentsDir := writeShardedAgent(t, "degpt", "json", "stop", 1)
	workingDir := writeWorkFiles(t, "a.md", "b.md")
	opts := &RunOptions{
		Agent:             "degpt",
		AgentsDir:         agentsDir,
		Provider:          "ollama",
		Model:             "mistral",
		BaseURL:           srv.URL,
		MaxIterations:     1,
		Mode:              "edit",
		ConfigAvailable:   true,
		RequireActionable: true,
	}
	bundle := buildShardBundle(t, opts, workingDir)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	resp, _, err := runSharded(context.Background(), cmd, opts, bundle, "do work", workingDir)
	if err == nil {
		t.Fatal("expected an aggregate shard failure")
	}
	if !strings.Contains(err.Error(), "shards failed") {
		t.Fatalf("error = %v, want it to mention failed shards", err)
	}
	var res shardedResult
	if jerr := json.Unmarshal([]byte(resp), &res); jerr != nil {
		t.Fatalf("merged output not valid json: %v\n%s", jerr, resp)
	}
	if res.Summary.Failed != 2 {
		t.Fatalf("expected both shards failed (one validation, one skipped), got %+v", res.Summary)
	}
	if !strings.Contains(resp, "skipped") {
		t.Errorf("expected a skipped shard in merged output:\n%s", resp)
	}
}
