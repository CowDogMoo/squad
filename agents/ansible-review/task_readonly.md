Analyze this codebase for Ansible code quality issues.

Use Glob with '**/*.yml' and '**/*.yaml' to discover all YAML files.
Filter to Ansible-relevant files: playbooks, tasks, handlers, vars, defaults, meta, molecule.
Batch Read calls: read 4-6 files per iteration. Do NOT read one file per iteration.
Cross-reference between files for consistency issues.
Produce a prioritized report of all findings.

ANALYSIS CHECKLIST (check each file for these):

- FQCN usage — all modules MUST use fully qualified names (MEDIUM if missing)
- Task names — every task MUST have a descriptive name: field (LOW if missing)
- Handlers — are notifies matched by handler names? (HIGH if orphaned)
- Idempotency — command/shell tasks have guards? (HIGH if not idempotent)
  For state-changing commands, suggest changed_when: true, NOT removal.
  ansible-lint REQUIRES changed_when on ALL command/shell tasks.
- Security — no_log on credentials? vault for secrets? (CRITICAL if exposed)
- Variables — role-prefixed? correct precedence usage? (MEDIUM if wrong)

SEVERITY GUIDE:

- CRITICAL: hardcoded secrets, missing no_log on credentials, command injection
- HIGH: non-idempotent tasks, orphaned handlers, missing check_mode support
- MEDIUM: missing FQCN, poor variable naming, missing argument_specs
- LOW: missing task names, inconsistent YAML style

No cosmetic findings — skip comment style, whitespace, import ordering.
No false positives — every finding must reference actual file and line.

ITERATION BUDGET — scales with codebase size (count after Glob):

- Small (≤20 files): 12 iterations max
- Medium (21-50 files): 20 iterations max
- Large (50+ files): 25 iterations max

EFFICIENCY (must follow):

- Batch reads: 4-6 files per iteration, not one file per iteration
- Do NOT hardcode directory names like roles/, playbooks/ — use Glob output
- Do NOT try to read reference files — they are bundled in your context
- Small/medium: read ALL files. Large: prioritize entry points + core roles
- Use ONE Grep/Glob on repo root — do NOT run per-directory searches
- After analysis is complete, emit report IMMEDIATELY — no re-reading files

Do NOT write or modify any files.
