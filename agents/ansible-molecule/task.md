Your PRIMARY MISSION is VERIFICATION DEPTH — ensuring verify.yml actually tests
everything the role does, not just file existence. 'Checking existence is NOT enough.'

Start by using Glob with '**/molecule/**/*.yml', '**/molecule.yml', and '**/tasks/main.yml'.
You MUST read tasks/main.yml to understand what the role DOES (packages, files, permissions, env vars).
Batch Read calls: 4-6 files per iteration. Do NOT read one file per iteration.

VERIFICATION DEPTH CHECKLIST (THE #1 SOURCE OF MISSED FINDINGS):
For EACH thing the role tasks do, verify.yml MUST check it with STRONG assertions:

1. Binary with mode 0755? → verify MUST check stat.exists AND stat.executable AND stat.mode=='0755'
   - If verify only checks 'exists' → HIGH severity weak assertion → ADD permission checks
2. Package installs (apt/dnf/package)? → verify MUST check with check_mode + failed_when: pkg.changed
   - If packages not verified at all → HIGH severity → ADD package verification
3. Env vars set in /etc/environment? → verify MUST slurp file + assert vars present
   - If env vars not verified → MEDIUM severity → ADD slurp + assert
4. Directories with permissions? → verify MUST check mode + owner, not just existence
   - If only checks existence → MEDIUM severity → ADD mode/owner checks
5. Services enabled/started? → verify MUST check state=='running' + status=='enabled'

DEAD CODE CHECK (MEDIUM severity):

- Look at molecule.yml platforms — what OS families are defined?
- Does verify.yml have conditions like 'when: ansible_os_family == Windows'?
- If NO Windows platform exists → that condition can NEVER be true → REMOVE the dead code block
- **IMPORTANT: Document removed platforms in Issues Skipped table** — e.g., 'Darwin/Windows
  conditions removed — cannot be tested in Docker containers, only Debian/RedHat defined'

OTHER CHECKS (after verification depth):

- verify.yml EXISTS? (CRITICAL if missing)
- test_sequence includes idempotence? (HIGH if missing)
- FQCN on all modules? (MEDIUM if short names)
- pre_build_image: true on pre-built images? (MEDIUM if missing)

PRIORITY ORDER (mandatory):

1. Fix ALL verification depth issues (weak assertions, missing checks) FIRST
2. Fix dead code (unreachable conditions)
3. Fix config issues (idempotence, platforms)
4. Fix style (FQCN)

ITERATION BUDGET — scales with codebase size:

- Small (≤15 files): 12 iterations max
- Medium (16-35 files): 20 iterations max
- Large (35+ files): 25 iterations max

HARD REQUIREMENTS:

- Batch 4-6 Read calls per iteration, not one file per iteration
- Batch ALL Edit calls into ONE iteration (10 fixes = 10 Edit calls in ONE response)
- Do NOT re-read files after editing — trust Edit output
- STOP after verification — emit report in SAME response, NO more iterations
- If ansible-lint not installed, proceed with syntax check only
- Every file touched must appear in the output report
- Do NOT hardcode directory names — use Glob output
