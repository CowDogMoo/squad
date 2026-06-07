package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/grading"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// withIsolatedCache redirects os.UserCacheDir() to a temp directory so the
// grade store doesn't pollute the developer's real ~/.cache.
func withIsolatedCache(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
}

// runGradeCmd parses flags + args against a fresh `grade` cobra command and
// returns the combined stdout/stderr.
func runGradeCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newGradeCmd()
	cmd.Flags().VisitAll(func(f *pflag.Flag) { _ = cmd.Flags().Set(f.Name, f.DefValue) })
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.ParseFlags(args); err != nil {
		return buf.String(), err
	}
	if err := cmd.RunE(cmd, cmd.Flags().Args()); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

// gradableReport is a minimal markdown report containing every section the
// grader looks for, so report-quality scoring is deterministic.
const gradableReport = `# Changes Summary

Fixed three bugs.

# Analysis Summary

Reviewed the code.

# Files Touched

- foo.go
- bar.go

# Validation

All tests pass.

# Issues Fixed

- bug: something
- bug: something else

# Issues Skipped

- nit: ignored

# Findings

- finding: x
`

func writeReport(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunGrade_fromFile_savesAndPrints(t *testing.T) {
	withIsolatedCache(t)
	report := writeReport(t, gradableReport)

	out, err := runGradeCmd(t,
		report,
		"--agent", "demo-review",
		"--iterations", "10",
		"--files", "15",
	)
	if err != nil {
		t.Fatalf("grade: %v", err)
	}
	if !strings.Contains(out, "Grade:") {
		t.Fatalf("expected grade header in output:\n%s", out)
	}

	// Verify persistence — a follow-up --history should see the entry.
	histOut, err := runGradeCmd(t, "--history", "--agent", "demo-review")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(histOut, "demo-review") {
		t.Fatalf("expected saved grade in history:\n%s", histOut)
	}
}

func TestRunGrade_noSaveSkipsPersistence(t *testing.T) {
	withIsolatedCache(t)
	report := writeReport(t, gradableReport)

	if _, err := runGradeCmd(t,
		report,
		"--agent", "ephemeral",
		"--iterations", "5",
		"--files", "10",
		"--save=false",
	); err != nil {
		t.Fatalf("grade: %v", err)
	}

	out, err := runGradeCmd(t, "--history", "--agent", "ephemeral")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, "No grades found") {
		t.Fatalf("expected no-history message when --save=false, got:\n%s", out)
	}
}

func TestRunGrade_jsonOutput(t *testing.T) {
	withIsolatedCache(t)
	report := writeReport(t, gradableReport)
	out, err := runGradeCmd(t,
		report,
		"--agent", "json-agent",
		"--iterations", "8",
		"--files", "12",
		"--json",
	)
	if err != nil {
		t.Fatalf("grade: %v", err)
	}
	var got grading.GradeResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("expected JSON output, parse error %v, output:\n%s", err, out)
	}
	if got.Agent != "json-agent" {
		t.Errorf("Agent = %q, want json-agent", got.Agent)
	}
}

func TestRunGrade_stdinInput(t *testing.T) {
	withIsolatedCache(t)

	// Swap os.Stdin for a pipe whose read side carries gradableReport.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString(gradableReport); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		if err := r.Close(); err != nil {
			t.Logf("close pipe: %v", err)
		}
	})

	out, err := runGradeCmd(t, "-",
		"--agent", "stdin-agent",
		"--iterations", "6",
		"--files", "10",
		"--save=false",
	)
	if err != nil {
		t.Fatalf("grade stdin: %v", err)
	}
	if !strings.Contains(out, "Grade:") {
		t.Fatalf("expected grade output from stdin path:\n%s", out)
	}
}

func TestRunGrade_missingArgsErrors(t *testing.T) {
	withIsolatedCache(t)
	cases := []struct {
		name string
		args []string
	}{
		{"no args, no flags", []string{}},
		{"file but no agent", []string{writeReport(t, gradableReport)}},
		{"stats without agent", []string{"--stats"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := runGradeCmd(t, tc.args...); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestRunGrade_unreadableFileSurfacesError(t *testing.T) {
	withIsolatedCache(t)
	_, err := runGradeCmd(t,
		filepath.Join(t.TempDir(), "does-not-exist.md"),
		"--agent", "demo",
		"--iterations", "5",
		"--files", "8",
	)
	if err == nil {
		t.Fatal("expected read error to surface")
	}
}

func TestDisplayHistory_emptyMessage(t *testing.T) {
	withIsolatedCache(t)
	out, err := runGradeCmd(t, "--history", "--agent", "nobody")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(out, "No grades found") {
		t.Fatalf("expected empty message, got:\n%s", out)
	}
}

func TestDisplayHistory_jsonOutput(t *testing.T) {
	withIsolatedCache(t)
	store, err := grading.NewStore()
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	parsed := grading.NewParser().Parse(gradableReport)
	res := grading.ComputeGrade(parsed, grading.GradeOptions{
		Agent: "hist-json", Iterations: 9, FileCount: 12,
	})
	if err := store.Save(res); err != nil {
		t.Fatalf("save: %v", err)
	}

	out, err := runGradeCmd(t, "--history", "--agent", "hist-json", "--json")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var arr []grading.GradeResult
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("expected JSON array, got error %v, output:\n%s", err, out)
	}
	if len(arr) != 1 || arr[0].Agent != "hist-json" {
		t.Fatalf("unexpected history payload: %+v", arr)
	}
}

func TestDisplayStats_textAndJSON(t *testing.T) {
	withIsolatedCache(t)
	store, err := grading.NewStore()
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	parser := grading.NewParser()
	for i := 0; i < 3; i++ {
		parsed := parser.Parse(gradableReport)
		if err := store.Save(grading.ComputeGrade(parsed, grading.GradeOptions{
			Agent: "stats-agent", Iterations: 8 + i, FileCount: 12,
		})); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	textOut, err := runGradeCmd(t, "--stats", "--agent", "stats-agent")
	if err != nil {
		t.Fatalf("stats text: %v", err)
	}
	for _, want := range []string{"Statistics for stats-agent", "Total Runs:", "Avg Score:"} {
		if !strings.Contains(textOut, want) {
			t.Errorf("missing %q in stats output:\n%s", want, textOut)
		}
	}

	jsonOut, err := runGradeCmd(t, "--stats", "--agent", "stats-agent", "--json")
	if err != nil {
		t.Fatalf("stats json: %v", err)
	}
	var stats grading.AgentStats
	if err := json.Unmarshal([]byte(jsonOut), &stats); err != nil {
		t.Fatalf("stats json parse: %v, output:\n%s", err, jsonOut)
	}
	if stats.TotalRuns != 3 {
		t.Errorf("TotalRuns = %d, want 3", stats.TotalRuns)
	}
}

func TestDisplayStats_unknownAgentErrors(t *testing.T) {
	withIsolatedCache(t)
	if _, err := runGradeCmd(t, "--stats", "--agent", "ghost"); err == nil {
		t.Fatal("expected stats for unknown agent to error")
	}
}

func TestWriteJSON_marshalErrorSurfaces(t *testing.T) {
	// channel values are unsupported by encoding/json so MarshalIndent errors.
	if err := writeJSON(&bytes.Buffer{}, make(chan int)); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestOutputJSON_writesIndented(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := outputJSON(cmd, map[string]int{"a": 1}); err != nil {
		t.Fatalf("outputJSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"a": 1`) {
		t.Fatalf("expected indented JSON, got:\n%s", buf.String())
	}
}
