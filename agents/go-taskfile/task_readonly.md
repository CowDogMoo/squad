Analyze this codebase for Taskfile.yaml best-practice violations.

Use Glob with '**/Taskfile.yaml' and '**/Taskfile.yml' to discover all Taskfiles.
Read each file to understand structure and conventions.
Cross-reference between files for include and variable consistency.
Produce a prioritized report of all findings.

No cosmetic findings — skip comment style, whitespace, task ordering.
No false positives — every finding must reference actual file and line.
Flag security issues (hardcoded secrets, unvalidated input) as CRITICAL.
Path traversal: ONLY flag for variables with NO default or unsafe defaults.
Skip for variables with safe defaults like /tmp — proportionality applies.
Read each file ONCE — target ≤10 iterations.
After analysis, emit report IMMEDIATELY — no re-reading files.

Do NOT write or modify any files.
