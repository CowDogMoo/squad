Audit and fix all security vulnerabilities in this Go codebase.

Start by using Glob with '**/*.go' to discover all Go source files.
Read each file (skip _test.go and vendor/).
Read go.mod for dependency and Go version info.
Cross-reference between files for inconsistent security practices.
Apply fixes via Edit tool, highest severity first.
Run 'go build ./...' after each batch of edits.

IMPORTANT CONSTRAINTS (repeat from system prompt):

- SECURITY FOCUS ONLY — skip code quality, doc comments, naming style
- No new dependencies
- Skip fixes needing 50+ lines or new files
- Preserve backwards compatibility — no API surface changes
- NEVER change functions whose behavior is asserted by tests
- NEVER remove intentional panic() calls — they are precondition guards
- Every fix must be PROPORTIONAL — ask 'could an attacker exploit this?'
- Theoretical vulnerabilities in internal-only code are INFO, not fixes
- No false positives — every finding must reference actual code with file:line
- Include CWE IDs where applicable
- Read each file ONCE, catalog all findings, then fix — target ≤12 iterations
- Batch ALL edits for the same file in one iteration — do NOT make separate Edit calls
- ALWAYS pass glob='*.go' when using Grep — NEVER grep without a glob filter
- NEVER fall back to per-directory Grep calls — that wastes iterations
- If Grep fails with 'token too long', skip it and use your Read-phase notes
- STOP after go build + go test BOTH pass — emit report IMMEDIATELY in the SAME response
- Do NOT re-read files after verification — no nl, sed, cat, Bash, Grep after tests pass
- Every file touched must appear in the output report
