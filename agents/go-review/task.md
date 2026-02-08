{{if eq .Mode "edit"}}
Review and fix all Go code quality issues in this codebase.

Start by using Glob with '**/*.go' to discover all Go source files.
Read each file (skip _test.go and vendor/).
Cross-reference between files for consistency issues.
Apply fixes via Edit tool, highest severity first.
Run 'go build ./...' after each batch of edits.

IMPORTANT CONSTRAINTS (repeat from system prompt):

- No cosmetic changes (doc comments, import ordering, naming style)
- No new dependencies
- Skip fixes needing 50+ lines or new files
- Preserve backwards compatibility — no API surface changes
- NEVER change functions whose behavior is asserted by tests (especially panics with wantPanic/recover)
- NEVER remove intentional panic() calls — they are precondition guards, not bugs
- Every fix must be PROPORTIONAL — no micro-optimizations for small loops
- Flag inconsistent imports (e.g. 'log' when codebase uses custom logging)
- Read each file ONCE, catalog all findings, then fix — target ≤12 iterations
- Use ONE Grep/Glob on repo root, not per-directory — minimize tool calls
- After go build + go test pass, emit report IMMEDIATELY — no post-fix exploration
- Every file touched must appear in the output report
{{end}}
{{if eq .Mode "readonly"}}
Analyze this codebase for Go code quality issues.

Use Glob with '**/*.go' to discover all Go source files.
Read each file (skip _test.go and vendor/).
Cross-reference between files for consistency issues.
Produce a prioritized report of all findings.

Do NOT write or modify any files.
{{end}}
