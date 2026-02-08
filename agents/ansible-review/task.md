Review and fix all Ansible code quality issues in this codebase.

Start by using Glob with '**/*.yml' and '**/*.yaml' to discover all YAML files.
Filter to Ansible-relevant files: playbooks, tasks, handlers, vars, defaults, meta, molecule.
Batch Read calls: read 4-6 files per iteration. Do NOT read one file per iteration.
Cross-reference between files for consistency issues.
Apply fixes via Edit tool, highest severity first.
Run 'ansible-lint .' after each batch of edits (if available).

ANALYSIS CHECKLIST (check each file for these):

- FQCN usage — all modules MUST use fully qualified names (ansible.builtin.x, not x:)
- Task names — every task MUST have a descriptive name: field
- Handlers — are notifies matched by handler names?
- Idempotency — command/shell tasks have creates/removes or changed_when?
  BUT: only for IDEMPOTENT commands. For state-changing commands (execute,
  run, mv), use changed_when: true, NOT changed_when: false.
  CRITICAL: NEVER remove changed_when entirely — ansible-lint REQUIRES it
  on ALL command/shell tasks. Fix: change 'false' to 'true', don't delete.
- Security — no_log on credential tasks? vault for secrets?
- Variables — role-prefixed names? defaults vs vars usage correct?

PRIORITY (mandatory order):

- Fix ALL CRITICAL (security, correctness) before ANY HIGH
- Fix ALL HIGH (idempotency, reliability) before ANY MEDIUM

CONSTRAINTS:

- No cosmetic changes (import ordering, whitespace, comment style)
- Skip fixes needing 50+ lines or new files
- Preserve backwards compatibility — no variable renames, no role restructuring
- NEVER change functions whose behavior is asserted by Molecule tests
- Every fix must be PROPORTIONAL — no theoretical improvements without real impact

ITERATION BUDGET — scales with codebase size (count after Glob):

- Small (≤20 files): 12 iterations max
- Medium (21-50 files): 20 iterations max
- Large (50+ files): 25 iterations max

Phase allocation:

- Phase 1 (1 iter): Glob to discover files, COUNT source files
- Phase 2 (varies): Read files with 4-6 parallel Reads per iteration
- Phase 3 (2-4 iter): ALL Edit calls batched (10 fixes = 10 Edit calls in ONE response)
- Phase 4 (1 iter): Verify + report in SAME response

Do NOT hardcode directory names like roles/, playbooks/ — use Glob output.
Do NOT try to read reference files — they are bundled in your context.
COVERAGE IS MANDATORY for small/medium. For large codebases, document sampling.

HARD REQUIREMENTS:

- Do NOT read one file per iteration — batch 4-6 Read calls per iteration
- Do NOT edit-wait-edit-wait — batch ALL edits into ONE iteration
- Do NOT re-read files after editing — trust Edit output
- STOP after verification — emit report in SAME response, NO more iterations
- To check ansible-lint: run 'ansible-lint --version'. If it prints a version, USE IT.
  WARNING messages about collection versions are NORMAL — they mean ansible-lint IS working.
  Only 'command not found' means it's not installed (then use syntax-check only).
- Every file touched must appear in the output report
