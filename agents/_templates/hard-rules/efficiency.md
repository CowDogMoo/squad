# Efficiency Rules

Maximize output quality while minimizing iteration count.

## Iteration Budget

Budget allocation scales with codebase size:

| Codebase Size | File Count | Max Iterations |
|---------------|------------|----------------|
| Small         | ≤20 files  | 12 iterations  |
| Medium        | 21-50 files| 20 iterations  |
| Large         | 50+ files  | 25 iterations  |

## Phase Budget Allocation

- **Phase 1 (Discover):** 1 iteration — Glob + reference reads
- **Phase 2 (Analyze):** varies by size — read ALL files, run static analysis
- **Phase 3 (Fix):** 2-4 iterations — ALL fixes batched
- **Phase 4 (Report):** 1 iteration — verify + report in SAME response

## Read Strategy by Size

- **Small (≤20):** Read ALL files in 2-3 iterations (6-10 files per iteration)
- **Medium (21-50):** Read ALL files in 4-5 iterations
- **Large (50+):** Prioritize entry points and core modules. Sample remaining
  files. Document what was skipped and why.

## Batching Rules

1. **Batch file reads.** Read 4-6 files per Read call. Never read one file
   per iteration.

2. **Batch edits by file.** Make ALL edits to a single file in ONE iteration.
   If a file needs 5 fixes, make 5 Edit calls in the same response.

3. **Efficient tool calls.** Use one Grep/Glob call on the repo root instead
   of N calls per-directory. Search the whole tree in one shot.

## Anti-Patterns to Avoid

- Reading one file per iteration
- Re-reading files after editing (trust the Edit tool's output)
- Making one edit per iteration instead of batching
- Running extra tool calls after verification passes
- Retrying failed tools instead of moving on

## Coverage vs Efficiency Balance

**Coverage is mandatory** for small/medium codebases. Do NOT skip files to
save iterations — that defeats the purpose of the review. For large codebases,
document what was sampled vs skipped.

## Wind-Down Protocol

When approaching your iteration limit:

1. Stop applying new fixes immediately
2. Run final verification
3. Produce the structured report
4. Populate skipped-findings from notes taken during analysis

A partial report with accurate results is infinitely better than no report at all.
