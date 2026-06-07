package grading

import (
	"strings"
	"testing"
)

func TestComputeGrade_PerfectRun(t *testing.T) {
	parsed := &ParsedOutput{
		HasChangesSummary: true,
		HasIssuesFixed:    true,
		HasFilesTouched:   true,
		HasValidation:     true,
		ValidationPassed:  true,
		Mode:              "edit",
		FilesTouched:      []string{"file1.go", "file2.go"},
		IssuesFixed:       []ParsedIssue{{Title: "Fix 1"}},
	}

	result := ComputeGrade(parsed, GradeOptions{
		Agent:      "go-review",
		Iterations: 10,
		FileCount:  15,
	})

	// With all sections present (100%) and iterations under target (100%)
	// Total automated score should be 100%
	if result.ReportQuality != 100 {
		t.Errorf("expected report quality 100, got %.0f", result.ReportQuality)
	}

	if result.IterationEfficiency != 100 {
		t.Errorf("expected iteration efficiency 100, got %.0f", result.IterationEfficiency)
	}

	if result.Grade != "A+" {
		t.Errorf("expected grade A+, got %s", result.Grade)
	}

	if result.Agent != "go-review" {
		t.Errorf("expected agent 'go-review', got %s", result.Agent)
	}
}

func TestComputeGrade_MissingSections(t *testing.T) {
	parsed := &ParsedOutput{
		HasChangesSummary: true,
		HasIssuesFixed:    true,
		// Missing: FilesTouched, Validation
		Mode: "edit",
	}

	result := ComputeGrade(parsed, GradeOptions{
		Agent:      "go-review",
		Iterations: 10,
		FileCount:  15,
	})

	// 2 of 4 sections present = 50% report quality
	if result.ReportQuality != 50 {
		t.Errorf("expected report quality 50, got %.0f", result.ReportQuality)
	}

	if len(result.MissingSections) != 2 {
		t.Errorf("expected 2 missing sections, got %d", len(result.MissingSections))
	}
}

func TestComputeGrade_IterationEfficiency(t *testing.T) {
	tests := []struct {
		name       string
		iterations int
		fileCount  int
		wantScore  float64
		wantRange  [2]float64 // min, max acceptable
	}{
		{"under target small", 10, 15, 100, [2]float64{100, 100}},
		{"at target small", 12, 15, 100, [2]float64{100, 100}},
		{"between target and max small", 15, 15, 0, [2]float64{60, 100}},
		{"at max small", 18, 15, 60, [2]float64{60, 60}},
		{"over max small", 24, 15, 0, [2]float64{0, 40}},

		{"under target medium", 20, 30, 100, [2]float64{100, 100}},
		{"at target medium", 25, 30, 100, [2]float64{100, 100}},
		{"at max medium", 35, 30, 60, [2]float64{60, 60}},

		{"under target large", 35, 60, 100, [2]float64{100, 100}},
		{"at target large", 40, 60, 100, [2]float64{100, 100}},
		{"at max large", 60, 60, 60, [2]float64{60, 60}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := &ParsedOutput{
				HasChangesSummary: true,
				HasIssuesFixed:    true,
				HasFilesTouched:   true,
				HasValidation:     true,
				Mode:              "edit",
			}

			result := ComputeGrade(parsed, GradeOptions{
				Agent:      "test",
				Iterations: tt.iterations,
				FileCount:  tt.fileCount,
			})

			if result.IterationEfficiency < tt.wantRange[0] || result.IterationEfficiency > tt.wantRange[1] {
				t.Errorf("iterations=%d, files=%d: got efficiency %.0f, want between %.0f and %.0f",
					tt.iterations, tt.fileCount, result.IterationEfficiency, tt.wantRange[0], tt.wantRange[1])
			}
		})
	}
}

func TestComputeGrade_ReadonlyMode(t *testing.T) {
	parsed := &ParsedOutput{
		HasAnalysisSummary: true,
		HasFindings:        true,
		Mode:               "readonly",
		Findings:           []ParsedIssue{{Title: "Finding 1"}, {Title: "Finding 2"}},
	}

	result := ComputeGrade(parsed, GradeOptions{
		Agent:      "go-review",
		Iterations: 8,
		FileCount:  10,
	})

	// All 2 required sections present
	if result.ReportQuality != 100 {
		t.Errorf("expected report quality 100, got %.0f", result.ReportQuality)
	}

	if result.IssuesFixed != 2 {
		t.Errorf("expected 2 issues (findings), got %d", result.IssuesFixed)
	}
}

func TestCalculateLetterGrade(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{100, "A+"},
		{97, "A+"},
		{95, "A"},
		{93, "A"},
		{91, "A-"},
		{90, "A-"},
		{88, "B+"},
		{85, "B"},
		{83, "B"},
		{81, "B-"},
		{75, "C"},
		{70, "C"},
		{65, "D"},
		{60, "D"},
		{55, "F"},
		{0, "F"},
	}

	for _, tt := range tests {
		got := calculateLetterGrade(tt.score)
		if got != tt.want {
			t.Errorf("calculateLetterGrade(%.0f) = %s, want %s", tt.score, got, tt.want)
		}
	}
}

func TestFormatResult(t *testing.T) {
	result := &GradeResult{
		Agent:                "go-review",
		Grade:                "A",
		TotalScore:           95,
		ReportQuality:        100,
		IterationEfficiency:  90,
		Iterations:           14,
		FileCount:            15,
		FilesTouched:         3,
		IssuesFixed:          2,
		IssuesSkipped:        1,
		Mode:                 "edit",
		FindingQualityReview: true,
		SkipDisciplineReview: true,
		Notes:                []string{"Iteration target: 12 (max acceptable: 18) for 15 files"},
	}

	output := FormatResult(result)

	if !strings.Contains(output, "Grade: A") {
		t.Error("output should contain grade")
	}

	if !strings.Contains(output, "Report Quality") {
		t.Error("output should contain report quality")
	}

	if !strings.Contains(output, "Manual review required") {
		t.Error("output should indicate manual review needed")
	}

	if !strings.Contains(output, "Finding Quality") {
		t.Error("output should mention finding quality review")
	}
}
