Analyze this Go codebase for security vulnerabilities.

Use Glob with '**/*.go' to discover all Go source files.
Read each file (skip _test.go and vendor/).
Read go.mod for dependency and Go version info.
Cross-reference between files for inconsistent security practices.
Produce a prioritized report of all security findings.

SECURITY FOCUS ONLY — skip code quality, doc comments, naming style.
No false positives — every finding must reference actual code.
Include CWE IDs where applicable.
Read each file ONCE, batch 4-6 Read calls per iteration — target ≤12 iterations.
ALWAYS pass glob='*.go' when using Grep — NEVER grep without a glob filter.
NEVER fall back to per-directory Grep calls — that wastes iterations.
After analysis, emit report IMMEDIATELY — no re-reading files.

Do NOT write or modify any files.
