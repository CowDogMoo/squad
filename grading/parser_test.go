package grading

import (
	"testing"
)

func TestParser_EditModeOutput(t *testing.T) {
	input := `## Changes Summary

Fixed 3 issues related to error handling and logging consistency.

## Issues Found and Fixed

### Missing error check on file close

**Severity:** HIGH
**Category:** error-handling
**File:** cmd/app/main.go
**Line:** 45

**What was changed:**
Added deferred error check for file.Close().

**Why:**
Unchecked close errors can hide data loss.

---

### Inconsistent logging package

**Severity:** MEDIUM
**Category:** consistency
**File:** pkg/util/helper.go
**Line:** 12

**What was changed:**
Changed log.Printf to logging.Info.

**Why:**
Codebase uses custom logging package.

---

## Issues Found but Skipped

| Issue | Severity | File | Reason Skipped |
|-------|----------|------|----------------|
| Potential nil pointer | LOW | pkg/api/handler.go | Test asserts current behavior |
| Magic number | INFO | pkg/config/config.go | Too minor to fix |

## Files Touched

- ` + "`cmd/app/main.go`" + ` — Added error check for file close
- ` + "`pkg/util/helper.go`" + ` — Switched to logging package

## Validation

- Build: PASS
- Tests: PASS
`

	parser := NewParser()
	result := parser.Parse(input)

	t.Run("mode", func(t *testing.T) {
		assertEqual(t, "edit", result.Mode)
	})

	t.Run("sections_detected", func(t *testing.T) {
		assertTrue(t, result.HasChangesSummary, "HasChangesSummary")
		assertTrue(t, result.HasIssuesFixed, "HasIssuesFixed")
		assertTrue(t, result.HasIssuesSkipped, "HasIssuesSkipped")
		assertTrue(t, result.HasFilesTouched, "HasFilesTouched")
		assertTrue(t, result.HasValidation, "HasValidation")
		assertTrue(t, result.ValidationPassed, "ValidationPassed")
	})

	t.Run("counts", func(t *testing.T) {
		assertLen(t, result.FilesTouched, 2, "FilesTouched")
		assertLen(t, result.IssuesFixed, 2, "IssuesFixed")
		assertLen(t, result.IssuesSkipped, 2, "IssuesSkipped")
	})

	t.Run("first_issue_details", func(t *testing.T) {
		if len(result.IssuesFixed) == 0 {
			t.Skip("no issues to check")
		}
		issue := result.IssuesFixed[0]
		assertEqual(t, "HIGH", issue.Severity)
		assertEqual(t, "cmd/app/main.go", issue.File)
	})

	t.Run("skipped_issue_details", func(t *testing.T) {
		if len(result.IssuesSkipped) == 0 {
			t.Skip("no skipped issues to check")
		}
		skip := result.IssuesSkipped[0]
		assertEqual(t, "Potential nil pointer", skip.Title)
		assertEqual(t, "Test asserts current behavior", skip.Reason)
	})
}

func assertEqual(t *testing.T, expected, actual string) {
	t.Helper()
	if expected != actual {
		t.Errorf("expected %q, got %q", expected, actual)
	}
}

func assertTrue(t *testing.T, value bool, name string) {
	t.Helper()
	if !value {
		t.Errorf("expected %s to be true", name)
	}
}

func assertLen[T any](t *testing.T, slice []T, expected int, name string) {
	t.Helper()
	if len(slice) != expected {
		t.Errorf("expected %s to have %d items, got %d", name, expected, len(slice))
	}
}

func TestParser_ReadonlyModeOutput(t *testing.T) {
	input := `## Analysis Summary

**Files analyzed:** 15
**Total findings:** 8
**By severity:** CRITICAL: 1, HIGH: 2, MEDIUM: 3, LOW: 1, INFO: 1

## Findings

### SQL injection vulnerability

**Severity:** CRITICAL
**Category:** security
**File:** pkg/db/query.go
**Line:** 78

**What is wrong:**
User input is concatenated directly into SQL query.

**Suggested fix:**
Use parameterized queries with ? placeholders.

---

### Missing input validation

**Severity:** HIGH
**Category:** security
**File:** pkg/api/handler.go
**Line:** 23

**What is wrong:**
Request body is used without validation.

**Suggested fix:**
Add validation middleware.

---

## Priority Order

Findings ranked by impact (fix in this order):

1. **SQL injection vulnerability** — CRITICAL, pkg/db/query.go
2. **Missing input validation** — HIGH, pkg/api/handler.go

## Recommendations

Focus on the CRITICAL SQL injection issue first. Consider adding a security linter to CI.
`

	parser := NewParser()
	result := parser.Parse(input)

	if result.Mode != "readonly" {
		t.Errorf("expected mode 'readonly', got %q", result.Mode)
	}

	if !result.HasAnalysisSummary {
		t.Error("expected HasAnalysisSummary to be true")
	}

	if !result.HasFindings {
		t.Error("expected HasFindings to be true")
	}

	if len(result.Findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(result.Findings))
	}

	if len(result.Findings) > 0 {
		finding := result.Findings[0]
		if finding.Severity != "CRITICAL" {
			t.Errorf("expected severity CRITICAL, got %q", finding.Severity)
		}
	}
}

func TestParser_MinimalOutput(t *testing.T) {
	input := `## Changes Summary

No issues found.

## Files Touched

(none)

## Validation

- Build: PASS
`

	parser := NewParser()
	result := parser.Parse(input)

	if result.Mode != "edit" {
		t.Errorf("expected mode 'edit', got %q", result.Mode)
	}

	if !result.HasChangesSummary {
		t.Error("expected HasChangesSummary to be true")
	}

	if len(result.FilesTouched) != 0 {
		t.Errorf("expected 0 files touched, got %d", len(result.FilesTouched))
	}
}

func TestRequiredSections(t *testing.T) {
	editSections := RequiredSections("edit")
	if len(editSections) != 4 {
		t.Errorf("expected 4 required sections for edit mode, got %d", len(editSections))
	}

	readonlySections := RequiredSections("readonly")
	if len(readonlySections) != 2 {
		t.Errorf("expected 2 required sections for readonly mode, got %d", len(readonlySections))
	}
}
