Add or improve docstrings on all public Python declarations in this codebase.

Start by using Glob with '**/*.py' to discover all Python source files.
Batch Read calls: read 4-6 files per iteration. Do NOT read one file per iteration.
Catalog every public declaration that is missing a docstring or has a deficient one.
Apply fixes via Edit tool, highest priority first.
Run 'python -m py_compile <files>' after each batch of edits.

IMPORTANT CONSTRAINTS (repeat from system prompt):

- ONLY modify docstrings — never change code logic, signatures, or imports
- Triple double quotes — always use """ for docstrings, never '''
- Start with summary line in imperative mood ('Return X' not 'Returns X')
- No blank line before docstring — docstring immediately follows def/class
- Complete sentences with proper punctuation
- Focus on WHAT, not HOW — no implementation details
- No redundant docstrings ('Process processes the data' = skip)
- SKIP trivial functions: close, get_value, simple wrappers — they need NO docstring
- Key test: 'Does this docstring tell the reader something the name does not?' If no → skip
- Proportional: one-line getter = one-line docstring, complex = multi-paragraph
- Public declarations only — skip private names (_foo)
- Match existing style (NumPy/Sphinx if present, else Google style)
- NEVER add -> None — it's always inferable

ITERATION BUDGET — scales with codebase size (count after Glob):

- Small (≤20 files): 12 iterations max
- Medium (21-50 files): 20 iterations max
- Large (50+ files): 25 iterations max

Phase allocation:

- Phase 1 (1 iter): Glob + Read reference in parallel, COUNT source files
- Phase 2 (varies): Read files with 4-6 parallel Reads per iteration
- Phase 3 (2-4 iter): ALL Edit calls batched (10 fixes = 10 Edit calls in ONE response)
- Phase 4 (1 iter): Verify + report in SAME response

Do NOT hardcode directory names like app/, src/, lib/ — use Glob output.
COVERAGE IS MANDATORY for small/medium. For large codebases, document sampling.

HARD REQUIREMENTS:

- Do NOT read one file per iteration — batch 4-6 Read calls per iteration
- Do NOT edit-wait-edit-wait — batch ALL edits into ONE iteration
- Do NOT re-read files after editing — trust Edit output
- STOP after verification — emit report in SAME response, NO more iterations
- If ruff not installed, proceed with py_compile only
- Every file touched must appear in the output report
