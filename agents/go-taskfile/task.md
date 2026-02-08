Review and fix all Taskfile.yaml best-practice violations in this codebase.

Start by using Glob with '**/Taskfile.yaml' and '**/Taskfile.yml' to discover all Taskfiles.
Read each Taskfile to understand structure and conventions.
Cross-reference between files for include and variable consistency.
Apply fixes via Edit tool, highest severity first.
Run 'task --list' after each batch of edits.

IMPORTANT CONSTRAINTS (repeat from system prompt):

- No cosmetic changes (comment style, whitespace, task ordering)
- Skip fixes needing restructuring of 3+ tasks or new includes
- Preserve backwards compatibility — no task renames without noting breaking change
- NEVER add hardcoded secrets — use environment variables with no default
- Every fix must be PROPORTIONAL — no complex preconditions for trivial edge cases
- Path traversal validation: ONLY add for variables with NO default or unsafe defaults.
  Skip for variables with safe defaults like /tmp — local task runners don't need it
- Read each file ONCE, catalog all findings, then fix — target ≤10 iterations
- Use ONE Glob on repo root, not per-directory — minimize tool calls
- After task --list passes, emit report IMMEDIATELY — no post-fix exploration
- Every file touched must appear in the output report
