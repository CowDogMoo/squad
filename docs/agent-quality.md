# Agent Quality Rubric

Scoring criteria for evaluating Squad agent runs. Use this rubric when
tuning prompts, comparing models, or validating new agents.

The `squad grade` command automates Report Quality and Iteration Efficiency
scoring. Finding Quality and Skip Discipline require manual review.

## Grading Scale

| Grade | Meaning |
|-------|---------|
| A+    | Correct findings, no false positives, efficient iterations, clean report |
| A     | Correct findings, no false positives, minor iteration overhead |
| A-    | Correct findings, no false positives, moderate iteration overhead |
| B+    | Mostly correct, one low-impact false positive or significant overhead |
| B     | One over-engineered or low-value fix applied, otherwise correct |
| B-    | Over-engineered fixes or missed high-value findings |
| C     | Multiple false positives, missed real issues, or wasted iterations |
| D     | Harmful changes (broke tests, introduced dead code, violated skip rules) |
| F     | Left codebase in a broken state or ignored hard rules entirely |

## Evaluation Dimensions

### 1. Finding Quality (50% of grade)

The most important dimension. Did the agent find real issues?

**A-tier:**

- Found genuine bugs, correctness issues, or meaningful inconsistencies
- Every applied fix prevents a real problem under realistic conditions
- No over-engineered fixes (micro-optimizations for trivial cases)

**B-tier:**

- Found real issues but also applied low-value fixes
- Applied a technically correct change that adds complexity without
  meaningful benefit (e.g. `strings.Builder` for a 3-element loop)

**C-tier or below:**

- Missed obvious issues (e.g. wrong logging package) while fixing trivial ones
- Applied changes that violate skip rules (test-asserted behavior, intentional panics)
- Introduced dead code or changes that pass tests by accident

**Key test:** For each fix, ask: "Does this prevent a real bug, fix a
meaningful inconsistency, or improve correctness under realistic
conditions?" If the answer is "it's a theoretical improvement that adds
complexity," it's over-engineering.

### 2. Skip Discipline (25% of grade)

Did the agent correctly leave things alone?

**A-tier:**

- Test-asserted behavior left untouched
- Intentional panics (precondition guards) left untouched
- Acceptable `_ =` patterns (logging writes, body closes) left untouched
- Callback contracts respected (`return nil` in `filepath.WalkFunc`)

**Automatic downgrade to D:**

- Changed a function whose behavior is asserted by tests
- Removed an intentional panic that has a `wantPanic`/`recover()` test
- Applied a fix that passes tests by accident (different panic at a
  different line, dead code that's never reached)

**Common traps to watch for:**

- `viperFromContext`-style panics: exist to crash loudly when a
  precondition is violated; tests assert them; removing them hides bugs
- `_ = fmt.Fprintln(...)` in logging code: you can't log a logging failure
- `return nil` in walk callbacks: means "continue," not "ignore error"

### 3. Iteration Efficiency (15% of grade)

Did the agent use its iteration budget well?

**Targets by codebase size:**

| Codebase size | Target iterations | Max acceptable |
|---------------|-------------------|----------------|
| Small (<=20 files) | <=12 | 18 |
| Medium (21-50 files) | <=25 | 35 |
| Large (50+ files) | <=40 | 60 |

**Efficient patterns:**

- Read each file once during analysis, catalog all findings, then fix
- One Grep/Glob on repo root instead of N calls per-directory
- Verify edits by reading only the changed lines, not the whole file
- After build+test pass, emit report immediately -- no post-fix exploration

**Wasteful patterns (deduct points for each):**

- Re-reading files already analyzed
- Per-directory Grep calls (8 calls for 8 dirs instead of 1 on root)
- Running `go build` multiple times when one would suffice
- Re-reading files to populate the skipped-findings table
- Post-fix exploratory reads or greps after verification passes

### 4. Report Quality (10% of grade)

Did the agent produce a clean, accurate report?

**A-tier:**

- All required sections present (Summary, Fixed, Skipped, Files Touched,
  Validation)
- Every file touched is listed with a description
- Skipped findings include the reason
- Build and test results are accurate

**Deductions:**

- Missing required sections
- Files touched but not listed in report
- Inaccurate build/test results
- Skipped table says "None" when there were clearly skippable findings
  (minor deduction -- acceptable but not ideal)

## Failure Modes Reference

Lessons learned from prompt tuning the `go-review` agent. These apply
to any code review or fix agent.

### Over-engineering (most common)

The agent finds a technically applicable rule (e.g. "use strings.Builder
in loops") and applies it without considering whether the improvement
matters. A `strings.Builder` for a 3-element loop adds complexity for
zero real-world benefit.

**Fix:** Add a proportionality rule: "Every fix must be proportional to
the problem. A micro-optimization for a small loop is over-engineering."
Qualify pattern rules with thresholds (e.g. "hot loops with dozens+
iterations").

### Missed consistency violations

The agent finds micro-optimizations but misses that a file imports `log`
when the entire codebase uses a custom `logging` package. Consistency
violations are real issues; micro-optimizations usually aren't.

**Fix:** Explicitly call out consistency violations in the WHAT TO FIX
list. Strengthen the "follow conventions" rule to name specific patterns
(e.g. "flag files that import a different logging package").

### Changing test-asserted behavior

The agent sees a `panic()` call, knows "panic is bad," and replaces it
with a warning + fallback. But the panic is intentional (a precondition
guard) and a test asserts it with `wantPanic: true`. The replacement
may even pass tests by accident -- e.g. a nil dereference panics before
the new code is reached, so `recover()` still catches something.

**Fix:** Make test-asserted behavior an absolute ban: "If a test asserts
the current behavior, the fix is FORBIDDEN." Distinguish between "never
add panic" and "never remove intentional panics."

### Post-fix wandering

After applying fixes and verifying build+test pass, the agent spends
5+ iterations re-reading files, running extra greps, or gathering details
for the skipped table -- all information it already had from the analysis
phase.

**Fix:** Add explicit workflow guidance: "After verification passes, emit
the report IMMEDIATELY. Use your analysis-phase notes for the skipped
table. Every tool call after verification is wasted."

### Inefficient tool calls

The agent runs 8 Grep calls (one per directory) instead of 1 on the repo
root. Or runs `go build` after each individual edit instead of after a
batch.

**Fix:** Add a rule: "Use one Grep/Glob on the repo root, not N per
directory. Batch related checks into single iterations."

## Applying This Rubric to New Agents

When creating a new agent (e.g. `go-tests`, `go-cobra`, `security-review`):

1. **Start with the hard rules from `go-review/system.md`** as a template.
   Rules 11 (test-asserted behavior), 15 (intentional panics), 16 (do no
   harm), 18 (proportionality), and 19-21 (efficiency) are universal.

2. **Run the agent 3+ times** on the same codebase and grade each run.
   Look for patterns in what it gets wrong.

3. **The first failure mode you see will repeat.** Fix it in the prompt
   immediately. Over-engineering and test-asserted-behavior violations
   are the two most common across all agents.

4. **Reinforce critical rules in the user prompt.** Models weight user
   messages higher than system messages. Repeat the top 3-5 constraints
   from the system prompt in the Taskfile user prompt.

5. **Qualify absolute rules with thresholds.** "Use strings.Builder in
   loops" becomes "use strings.Builder in hot loops (dozens+ iterations)."
   "Handle all errors" becomes "handle errors that can cause incorrect
   behavior; leave `_ =` on logging writes alone."

6. **Grade against this rubric after every prompt change.** If the grade
   doesn't improve, the change didn't help -- revert it.
