package grading

import (
	"fmt"
	"time"
)

// GradeResult represents the computed grade for an agent run.
type GradeResult struct {
	// Identification
	Agent     string    `json:"agent"`
	Timestamp time.Time `json:"timestamp"`
	RunID     string    `json:"run_id,omitempty"`

	// Overall grade
	Grade      string  `json:"grade"`
	TotalScore float64 `json:"total_score"`

	// Component scores (0-100)
	ReportQuality       float64 `json:"report_quality"`
	IterationEfficiency float64 `json:"iteration_efficiency"`

	// Manual review indicators
	FindingQualityReview bool `json:"finding_quality_review"`
	SkipDisciplineReview bool `json:"skip_discipline_review"`

	// Metadata
	Iterations    int    `json:"iterations"`
	FileCount     int    `json:"file_count"`
	FilesTouched  int    `json:"files_touched"`
	IssuesFixed   int    `json:"issues_fixed"`
	IssuesSkipped int    `json:"issues_skipped"`
	Mode          string `json:"mode"`

	// Breakdown details
	MissingSections []string `json:"missing_sections,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}

// GradeOptions configures the grading calculation.

type GradeOptions struct {
	Agent      string
	Iterations int
	FileCount  int
	RunID      string
}

// iterationTargets defines iteration budgets by codebase size.
var iterationTargets = []struct {
	MaxFiles  int
	Target    int
	MaxAccept int
}{
	{20, 12, 18},     // Small
	{50, 25, 35},     // Medium
	{999999, 40, 60}, // Large
}

// ComputeGrade calculates the grade for a parsed agent output.
func ComputeGrade(parsed *ParsedOutput, opts GradeOptions) *GradeResult {
	result := &GradeResult{
		Agent:      opts.Agent,
		Timestamp:  time.Now(),
		RunID:      opts.RunID,
		Iterations: opts.Iterations,
		FileCount:  opts.FileCount,
		Mode:       parsed.Mode,

		FilesTouched:  len(parsed.FilesTouched),
		IssuesFixed:   len(parsed.IssuesFixed) + len(parsed.Findings),
		IssuesSkipped: len(parsed.IssuesSkipped),

		FindingQualityReview: true,
		SkipDisciplineReview: true,
	}

	// Calculate report quality (10% of grade)
	result.ReportQuality = calculateReportQuality(parsed, result)

	// Calculate iteration efficiency (15% of grade)
	result.IterationEfficiency = calculateIterationEfficiency(opts.Iterations, opts.FileCount, result)

	// Calculate total automated score (25% of full grade)
	// Report quality is 10% of grade, Iteration efficiency is 15%
	// Max automated points = 25, so we compute what fraction they earned
	automatedPoints := (result.ReportQuality/100)*10 + (result.IterationEfficiency/100)*15
	result.TotalScore = (automatedPoints / 25) * 100 // Scale to 0-100

	// Determine letter grade based on automated portion
	result.Grade = calculateLetterGrade(result.TotalScore)

	return result
}

func calculateReportQuality(parsed *ParsedOutput, result *GradeResult) float64 {
	required := RequiredSections(parsed.Mode)
	if len(required) == 0 {
		return 100
	}

	present := 0
	for _, section := range required {
		if isSectionPresent(parsed, section) {
			present++
		} else {
			result.MissingSections = append(result.MissingSections, section)
		}
	}

	return float64(present) / float64(len(required)) * 100
}

func isSectionPresent(parsed *ParsedOutput, section string) bool {
	switch section {
	case "changes_summary":
		return parsed.HasChangesSummary
	case "analysis_summary":
		return parsed.HasAnalysisSummary
	case "files_touched":
		return parsed.HasFilesTouched
	case "validation":
		return parsed.HasValidation
	case "issues_fixed":
		return parsed.HasIssuesFixed
	case "issues_skipped":
		return parsed.HasIssuesSkipped
	case "findings":
		return parsed.HasFindings
	default:
		return false
	}
}

func calculateIterationEfficiency(iterations, fileCount int, result *GradeResult) float64 {
	if iterations == 0 {
		result.Notes = append(result.Notes, "No iteration count provided")
		return 0
	}

	// Find the appropriate tier
	var target, maxAccept int
	for _, tier := range iterationTargets {
		if fileCount <= tier.MaxFiles {
			target = tier.Target
			maxAccept = tier.MaxAccept
			break
		}
	}

	result.Notes = append(result.Notes,
		fmt.Sprintf("Iteration target: %d (max acceptable: %d) for %d files", target, maxAccept, fileCount))

	// Perfect score if at or below target
	if iterations <= target {
		return 100
	}

	// Linear degradation from target to max acceptable
	if iterations <= maxAccept {
		// 100% at target, 60% at maxAccept
		ratio := float64(iterations-target) / float64(maxAccept-target)
		return 100 - (ratio * 40)
	}

	// Beyond max acceptable: further penalty
	// 60% at maxAccept, 0% at 2x maxAccept
	overageRatio := float64(iterations-maxAccept) / float64(maxAccept)
	score := 60 - (overageRatio * 60)
	if score < 0 {
		return 0
	}
	return score
}

func calculateLetterGrade(score float64) string {
	switch {
	case score >= 97:
		return "A+"
	case score >= 93:
		return "A"
	case score >= 90:
		return "A-"
	case score >= 87:
		return "B+"
	case score >= 83:
		return "B"
	case score >= 80:
		return "B-"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

// FormatResult returns a human-readable summary of the grade.
func FormatResult(r *GradeResult) string {
	s := fmt.Sprintf("Grade: %s (Automated Score: %.0f%%)\n", r.Grade, r.TotalScore)
	s += fmt.Sprintf("  Report Quality:       %.0f%%\n", r.ReportQuality)
	s += fmt.Sprintf("  Iteration Efficiency: %.0f%%\n", r.IterationEfficiency)
	s += "\n"

	if len(r.MissingSections) > 0 {
		s += fmt.Sprintf("  Missing sections: %v\n", r.MissingSections)
	}

	s += fmt.Sprintf("  Iterations: %d | Files: %d | Touched: %d | Fixed: %d | Skipped: %d\n",
		r.Iterations, r.FileCount, r.FilesTouched, r.IssuesFixed, r.IssuesSkipped)

	if r.FindingQualityReview || r.SkipDisciplineReview {
		s += "\n  ⚠ Manual review required:\n"
		if r.FindingQualityReview {
			s += "    - Finding Quality (50% of grade)\n"
		}
		if r.SkipDisciplineReview {
			s += "    - Skip Discipline (25% of grade)\n"
		}
	}

	for _, note := range r.Notes {
		s += fmt.Sprintf("  Note: %s\n", note)
	}

	return s
}
