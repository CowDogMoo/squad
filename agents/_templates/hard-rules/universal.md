# Universal Hard Rules

These rules apply to ALL review agents. They override everything else.

## Discovery & Analysis

1. **Discover code yourself.** Use Glob to find all relevant source files.
   Filter out test files, vendor directories, and cache folders. Read each
   file before analyzing it. Never guess at file contents.

2. **Batch file reads.** Read 4-6 files per iteration by batching Read calls.
   Do NOT read one file per iteration — that wastes your iteration budget.

3. **Cross-reference files.** Check that types, functions, and error handling
   are consistent across package/module boundaries — not just within single files.

## Change Discipline

4. **No cosmetic-only changes.** Skip doc comments, import ordering, naming
   style preferences, and whitespace adjustments. Every edit must fix a
   functional or best-practice violation. Doc comments are the #1 false
   positive — ban them explicitly.

5. **Changes must pass verification.** Run the appropriate verification
   command (build, lint, tests) after every batch of edits. If verification
   fails, fix the error or revert before continuing.

6. **Follow existing conventions.** Read surrounding code before editing.
   Match the existing style for error messages, variable naming, logging,
   and code organization. Check existing imports before adding new ones.

7. **Preserve backwards compatibility.** Do not rename exported/public
   functions, change function signatures, remove exported types, or alter
   the public API surface. If something is wrong but published, note it —
   do not change it.

8. **Test-asserted behavior is UNFIXABLE.** Before applying ANY fix, search
   for tests that reference the function or type you are changing. If a test
   asserts the current behavior, the fix is **FORBIDDEN**. Move it to the
   skipped table with reason "test asserts current behavior" and move on.
   You CANNOT edit test files, so you cannot change what the tests expect.

9. **Do no harm.** Every fix must be strictly better than the original code.
   If a fix changes control flow (adds `return`, changes branching), you
   must justify why the new behavior is correct. If the only available fix
   is a lateral move (equally imperfect), skip it.

10. **Proportionality.** Every fix must be proportional to the problem. A
    micro-optimization for a 3-element loop is over-engineering, not a fix.
    Before applying a change, ask: "Does this prevent a real bug, fix a
    meaningful inconsistency, or improve correctness under realistic
    conditions?" If the answer is "it's a theoretical improvement that adds
    complexity," skip it and move to higher-value findings.

## Efficiency

11. **Efficiency with iterations.** Read each file ONCE and take notes. Do
    not re-read files you have already analyzed. Batch your analysis of all
    files first, then apply fixes. If you need to verify an edit, read only
    the edited region, not the whole file again.

12. **No post-fix exploration.** Once all fixes are applied and verified,
    go directly to the report. Do NOT re-read files to gather details for
    the skipped-findings table — use the notes you already took during the
    Analyze phase. Do NOT run extra Grep scans for patterns you already
    checked.

13. **STOP after verification.** Once verification passes, emit the report
    IMMEDIATELY in the SAME response. Do NOT:
    - Re-read files after verification passes
    - Run extra Grep or Glob calls
    - Use Bash commands (cat, head, tail) to inspect files
    Every tool call after verification is wasted.
