Analyze this codebase for missing or deficient Python docstrings.

Use Glob with '**/*.py' to discover all Python source files.
Batch Read calls: read 4-6 files per iteration. Do NOT read one file per iteration.
Catalog every public declaration that needs a docstring.
Produce a prioritized report of all findings.

ANALYSIS CHECKLIST (check each file for these):

- Public functions/methods — do they have docstrings? (HIGH if missing on complex)
- Public classes — do they have docstrings with Attributes section? (HIGH if missing)
- Modules — do they have module docstrings? (MEDIUM if missing)
- Existing docstrings — are they complete sentences? (MEDIUM if fragments)
- Existing docstrings — do they use imperative mood? (LOW if not)
- Complex functions — do they have Args/Returns/Raises? (LOW if missing)

SEVERITY GUIDE:

- HIGH: missing docstring on complex public function/class
- MEDIUM: missing docstring on simpler exports, format violations
- LOW: improvement opportunities (missing Args/Returns, could use examples)

WHAT TO SKIP:

- Private names (_foo) — don't need docstrings
- Trivial functions (close, get_value, wrappers) — name is the doc
- -> None return annotations — always inferable, NEVER report as missing

No cosmetic findings — skip import ordering, naming style.
No false positives — every finding must reference actual file and line.

ITERATION BUDGET — scales with codebase size (count after Glob):

- Small (≤20 files): 12 iterations max
- Medium (21-50 files): 20 iterations max
- Large (50+ files): 25 iterations max

EFFICIENCY (must follow):

- Batch reads: 4-6 files per iteration, not one file per iteration
- Do NOT hardcode directory names like app/, src/ — use Glob output
- Small/medium: read ALL files. Large: prioritize entry points + core modules
- Use ONE Grep/Glob on repo root — do NOT run per-directory searches
- After analysis is complete, emit report IMMEDIATELY — no re-reading files

Do NOT write or modify any files.
