Analyze this codebase for Python code quality issues.

Use Glob with '**/*.py' to discover all Python source files.
Batch Read calls: read 4-6 files per iteration. Do NOT read one file per iteration.
Cross-reference between files for consistency issues.
Produce a prioritized report of all findings.

ANALYSIS CHECKLIST (check each file for these):

- Method calls self.x() — is the method actually defined? (CRITICAL if not)
- Every name used — is it imported or defined? (CRITICAL if missing import)
- if/else branches — do they do different things or identical code? (HIGH if dead code)
- HTTP calls — inside context managers? (HIGH if httpx.get without Client())
- Public functions — return type annotations? (but NEVER add -> None, it's inferable)

SEVERITY GUIDE:

- CRITICAL: syntax errors, undefined methods, missing imports, bare except, mutable defaults
- HIGH: broad except, resource leaks, missing type annotations, dead code, fire-and-forget
- MEDIUM: print() instead of logging, legacy type syntax, inconsistent styles

No cosmetic findings — skip docstrings, import ordering, naming style.
No false positives — every finding must reference actual file and line.

ITERATION BUDGET — scales with codebase size (count after Glob):

- Small (≤20 files): 12 iterations max
- Medium (21-50 files): 20 iterations max
- Large (50+ files): 25 iterations max

EFFICIENCY (must follow):

- Batch reads: 6-10 files per iteration, not one file per iteration
- Do NOT hardcode directory names like app/, src/ — use Glob output
- Small/medium: read ALL files. Large: prioritize entry points + core modules
- Use ONE Grep/Glob on repo root — do NOT run per-directory searches
- After analysis is complete, emit report IMMEDIATELY — no re-reading files

Do NOT write or modify any files.
