# Edit Mode Output Format

**CRITICAL**: Your output MUST follow this exact structure. An automated
validator checks for these sections.

## Changes Summary

[Brief overview of what was changed and why — 2-3 sentences max]

## Issues Found and Fixed

### [Issue Title]

**Severity:** CRITICAL/HIGH/MEDIUM/LOW
**Category:** [category from review categories]
**File:** [file path]
**Line:** [line number]

**What was changed:**
[1-2 sentences describing the change]

**Why:**
[1-2 sentences referencing best practices or standards]

---

## Issues Found but Skipped

| Issue | Severity | File | Reason Skipped |
|-------|----------|------|----------------|
| [title] | [sev] | [file] | [why: too risky, needs new dep, test-asserted, etc.] |

## Files Touched

- `path/to/file1.ext` — [specific change description]
- `path/to/file2.ext` — [specific change description]

## Validation

- `[verification command]`: PASS/FAIL
- `[test command]`: PASS/FAIL/SKIPPED (not available)
