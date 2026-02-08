Review and fix all Python code quality issues in this codebase.

Start by using Glob with '**/*.py' to discover all Python source files.
Batch Read calls: read 4-6 files per iteration. Do NOT read one file per iteration.
Cross-reference between files for consistency issues.
Apply fixes via Edit tool, highest severity first.
Run 'ruff check .' after edits. If ruff is NOT installed, use py_compile only.

ANALYSIS CHECKLIST (check each file for these):

- Method calls self.x() — is the method actually defined? (CRITICAL if not)
- Every name used — is it imported or defined? (CRITICAL if missing import)
- if/else branches — do they do different things or identical code? (HIGH if dead code)
- HTTP calls — inside context managers? (HIGH if httpx.get without Client())
- Public functions — return type annotations? (but NEVER add -> None, it's inferable)

PRIORITY (mandatory order):

- Fix ALL CRITICAL before ANY HIGH. Fix ALL HIGH before ANY MEDIUM.
- Do NOT skip CRITICAL/HIGH to fix easier MEDIUM issues.

CONSTRAINTS:

- No cosmetic changes (docstrings, import ordering, naming style)
- No new dependencies
- Skip fixes needing 50+ lines or new files
- Preserve backwards compatibility — no API surface changes
- NEVER change functions whose behavior is asserted by tests (pytest.raises)
- NEVER remove intentional raise statements — they are precondition guards
- Every fix must be PROPORTIONAL — no micro-optimizations for small loops
- NEVER change identifier/correlation ID assignments (job_id=x.project may be intentional)
- When fixing UnboundLocalError, use None as fallback, NOT another variable's value
- Flag inconsistent imports (e.g. print() when codebase uses logging)
- NEVER add -> None return annotations — they are always inferable

ITERATION BUDGET — scales with codebase size (count after Glob):

- Small (≤20 files): 12 iterations max
- Medium (21-50 files): 20 iterations max
- Large (50+ files): 25 iterations max

Phase allocation:

- Phase 1 (1 iter): Glob + Read reference in parallel, COUNT source files
- Phase 2 (varies): Read files with 6-10 parallel Reads per iteration
  - Small: 2-3 iters to read ALL
  - Medium: 4-5 iters to read ALL
  - Large: prioritize entry points + core modules
- Phase 3 (2-4 iter): ALL Edit calls batched (10 fixes = 10 Edit calls in ONE response)
- Phase 4 (1 iter): Verify + report in SAME response

Do NOT hardcode directory names like app/, src/, lib/ — use Glob output.
COVERAGE IS MANDATORY for small/medium. For large codebases, document sampling.

HARD REQUIREMENTS:

- Do NOT read one file per iteration — batch 6-10 Read calls per iteration
- Do NOT edit-wait-edit-wait — batch ALL edits into ONE iteration
- Do NOT re-read files after editing — trust Edit output
- STOP after verification — emit report in SAME response, NO more iterations
- If ruff not installed, proceed with py_compile only
- Every file touched must appear in the output report
