# go-doc-comments

Autonomous Go documentation agent that discovers exported declarations, adds
or improves doc comments following the official Go Doc Comments specification,
and verifies compilation.

## Overview

This agent specializes in creating high-quality Go documentation comments
that follow:

- Official [Go Doc Comments specification](https://go.dev/doc/comment)
- Go community conventions and idioms
- Modern doc comment features (Go 1.19+)
- 80-character line length limit

## Modes

### Normal (edit) mode

Discovers all Go source files, catalogs missing or deficient doc comments on
exported declarations, applies fixes, verifies compilation, and reports results.

```bash
task run:go-doc-comments
task run:go-doc-comments TARGET_REPO=/path/to/repo
```

### Readonly (analysis) mode

Produces a prioritized report of doc comment gaps without modifying any files.

```bash
task run:go-doc-comments-analyze
task run:go-doc-comments-analyze TARGET_REPO=/path/to/repo
```

## What Gets Documented

The agent documents all exported (capitalized) declarations:

- **Packages** — "Package [name]" format, one per package
- **Functions** — start with function name, describe behavior
- **Types** — "A [Type] represents..." or "[Type] is..."
- **Methods** — start with method name, describe behavior
- **Constants/Variables** — purpose and usage
- **Boolean functions** — "reports whether" pattern
- **Error variables** — when returned and how to check
- **Concurrency safety** — thread safety when non-obvious
- **Cleanup requirements** — resource release needs

## What Gets Skipped

- Unexported (lowercase) declarations
- Code logic, signatures, or behavior (only comments are modified)
- Test files and vendor directories
- Trivial exports where a comment would just restate the signature
- Generated code files
- Interface method declarations (interface doc covers these)

## Hard Rules

Key constraints that prevent common failure modes:

1. **Only modify doc comments** — never change code logic
2. **No blank line between comment and declaration** — godoc drops these
3. **Start with the declared name** — godoc indexes by first word
4. **Complete sentences** — fragments are not doc comments
5. **No redundant comments** — "Process processes the data" = skip
6. **Proportional** — match comment length to declaration complexity
7. **Focus on WHAT, not HOW** — no implementation details
8. **Boolean functions use "reports whether"** — not "returns true if"

## Output Format

### Normal mode

- Changes Summary
- Doc Comments Added (with file, line, category, comment, reason)
- Doc Comments Improved (with before/after)
- Declarations Skipped (table with reasons)
- Files Touched
- Validation (`go build ./...`)

### Readonly mode

- Analysis Summary (files analyzed, finding counts by severity)
- Findings (with severity, category, file, line, suggested comment)
- Priority Order (ranked by impact)
- Recommendations

## Reference

See [references/go-documentation-standards.md](references/go-documentation-standards.md)
for the complete knowledge base covering syntax by declaration type, modern
doc comment features, common mistakes, and quality checklist.

## Related Agents

- **go-review** — review Go code for best practices
- **go-cobra** — fix Cobra/Viper CLI violations
- **go-tests** — increase test coverage
