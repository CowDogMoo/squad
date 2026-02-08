Review and fix all Cobra/Viper best-practice violations in this codebase.

Start by using Glob with '**/*.go' to discover all Go source files.
Read each file in cmd/ and internal/ that imports cobra or viper.
Cross-reference between files for integration issues.
Apply fixes via Edit tool, highest severity first.
Run 'go build ./...' after each batch of edits.

IMPORTANT CONSTRAINTS (repeat from system prompt):

- Only modify files in cmd/ and internal/
- No cosmetic changes (doc comments, import ordering, naming style)
- No new dependencies
- Skip fixes needing 50+ lines or new files
- Preserve backwards compatibility — no flag renames or removals
- Every file touched must appear in the output report
