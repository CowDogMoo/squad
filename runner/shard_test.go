package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
}
