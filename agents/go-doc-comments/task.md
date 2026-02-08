Add or improve doc comments on all exported Go declarations in this codebase.

Start by using Glob with '**/*.go' to discover all Go source files.
Read each file (skip _test.go and vendor/).
Catalog every exported declaration that is missing a doc comment or has a deficient one.
Apply fixes via Edit tool, highest priority first.
Run 'go build ./...' after each batch of edits.

IMPORTANT CONSTRAINTS (repeat from system prompt):

- ONLY modify doc comments — never change code logic, signatures, or imports
- Start every comment with the declared name (godoc indexes by first word)
- No blank line between comment and declaration (godoc drops these)
- Complete sentences with proper punctuation
- Focus on WHAT, not HOW — no implementation details
- No redundant comments ('Process processes the data' = skip)
- SKIP trivial wrappers: Info/Warn/Debug/Error on loggers, simple setters, delegation functions
- Key test: 'Does this comment tell the reader something the name does not?' If no → skip
- Boolean functions use 'reports whether' (fix EXISTING 'returns true' comments too)
- Grep for 'returns true' in existing comments — these are format violations to fix
- Proportional: one-line getter = one-line comment, trivial = NO comment
- Exported declarations only — skip unexported names
- Read each file ONCE, catalog all findings, then fix — target ≤15 iterations
- Use ONE Grep/Glob on repo root, not per-directory — minimize tool calls
- After go build passes, emit report IMMEDIATELY — no post-fix exploration
- Every file touched must appear in the output report
