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
- After build+test pass, emit report immediately; no post-fix exploration

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
may even pass tests by accident; e.g. a nil dereference panics before
the new code is reached, so `recover()` still catches something.

**Fix:** Make test-asserted behavior an absolute ban: "If a test asserts
the current behavior, the fix is FORBIDDEN." Distinguish between "never
add panic" and "never remove intentional panics."

### Post-fix wandering

After applying fixes and verifying build+test pass, the agent spends
5+ iterations re-reading files, running extra greps, or gathering details
for the skipped table; all information it already had from the analysis
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
   After each run, pass the agent output to `squad grade` to capture the
   automated scores, then complete the manual dimensions before recording
   the final grade. Look for patterns in what it gets wrong.

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

## Using `squad grade`

### What the command automates

`squad grade` scores two dimensions automatically:

- **Report Quality (10%)** checks for required sections using the report
  parser. The parser auto-detects the report mode from its content and applies
  the corresponding required-section list:
  - **Edit mode** (triggered when `## Changes Summary` or
    `## Issues Found and Fixed` is present): requires `## Changes Summary`,
    `## Issues Found and Fixed`, `## Files Touched`, `## Validation`.
  - **Readonly mode** (triggered when `## Analysis Summary` or `## Findings`
    is present, and neither edit trigger is): requires `## Analysis Summary`,
    `## Findings`.
  - If neither trigger is detected the parser defaults to edit mode
    requirements, which will produce a low Report Quality score. An agent
    report that uses non-standard headings will be evaluated against the wrong
    required sections and may score 0% on Report Quality even if all
    information is present.
- **Iteration Efficiency (15%)** compares `--iterations` against the
  target/max thresholds for the codebase size derived from `--files`.

The remaining 75% of the grade requires manual review:

- **Finding Quality (50%)** no tool can judge whether a fix prevents a real
  bug.
- **Skip Discipline (25%)** no tool can judge whether the agent respected
  test-asserted behavior.

### Flag reference

| Flag | Short | Type | Default | Purpose |
|------|-------|------|---------|---------|
| `--agent` | `-a` | string | — | Agent name; required for grading and `--stats`; optional for `--history` (omit to show all agents) |
| `--iterations` | `-i` | int | `0` | Iterations the agent used; required for Efficiency score — omitting silently scores Efficiency as 0% |
| `--files` | `-f` | int | `0` | Source file count; determines small/medium/large bucket — omitting defaults to the Small bucket (≤20 files) thresholds |
| `--run-id` | | string | — | Optional label for tracking a specific run in history |
| `--save` | | bool | `true` | Persist grade to local history store |
| `--history` | | bool | `false` | Print past grades for the agent instead of grading; takes precedence if combined with `--stats` |
| `--stats` | | bool | `false` | Print aggregate stats (mean, distribution) for the agent |
| `--limit` | | int | `10` | Number of history records to show |
| `--json` | | bool | `false` | Output machine-readable JSON (history and stats modes) |

### Examples

**Grading mode file input:**

```bash
squad grade output.md --agent go-review --iterations 15 --files 12
```

- `output.md` — the report the agent wrote; the parser scans it for required
  sections to compute the Report Quality score.
- `--agent go-review` — identifies which agent produced the run. Used to
  namespace the grade in the history store so `--history` and `--stats`
  queries stay agent-specific.
- `--iterations 15` — the number of tool-call iterations the agent consumed.
  Compared against the target/max thresholds for the codebase size bucket to
  compute the Iteration Efficiency score.
- `--files 12` — the number of source files in the codebase. With 12 files the
  run is classified as "small" (≤20 files), so the efficiency target is ≤12
  iterations and the max acceptable is 18.

**Use this form** as the default after any `squad run` that writes its output
to a file.

---

**Grading mode stdin:**

```bash
cat output.md | squad grade - --agent go-review --iterations 15 --files 12
```

The `-` positional argument tells the command to read from stdin instead of a
file. The flags mean exactly the same thing as the file-input form.

**Use this form** when you want to pipe a report directly into the grader
without writing an intermediate file, or in CI pipelines. Note: a pipeline of
the form `squad run ... | squad grade -` only works if `squad run` emits the
agent's markdown report to stdout — verify this before using it in CI.

---

**Grading with run tracking:**

```bash
squad grade output.md --agent go-review --iterations 15 --files 12 \
  --run-id "2026-04-30-main"
```

- `--run-id "2026-04-30-main"` an arbitrary label stored alongside the grade.
  No effect on scoring; exists solely to make history records identifiable when
  queried later.

**Use this form** when doing systematic prompt tuning across multiple runs with
different system prompts.

---

**Grading without saving to history:**

```bash
squad grade output.md --agent go-review --iterations 15 --files 12 \
  --save=false
```

- `--save=false` — overrides the default (`true`) and skips writing to the
  local grade store.

**Use this form** for exploratory or throwaway runs you do not want polluting
the stats baseline.

---

**View grade history (last 10 runs):**

```bash
squad grade --history --agent go-review
```

**Use this form** to review how an agent has been trending across recent runs,
especially after making a prompt change.

---

**View grade history — extended window:**

```bash
squad grade --history --agent go-review --limit 25
```

**Use this form** when you need a longer baseline to spot slow regressions.

---

**View grade history as JSON:**

```bash
squad grade --history --agent go-review --json
```

**Use this form** when piping history into another tool (`jq`, a spreadsheet
import, or a CI quality gate script).

---

**View aggregate stats:**

```bash
squad grade --stats --agent go-review
```

**Use this form** to get a single-number summary of agent quality across all
saved runs; useful at the end of a prompt-tuning sprint.

### Interpreting the output

**Grading mode** prints a header line with the letter grade and automated
score, then per-dimension percentages, run metadata, and a manual-review
warning:

```
Grade: B+ (Automated Score: 88%)
  Report Quality:       100%
  Iteration Efficiency: 72%

  Iterations: 15 | Files: 12 | Touched: 3 | Fixed: 2 | Skipped: 0

⚠ Manual review required:
    - Finding Quality (50% of grade)
    - Skip Discipline (25% of grade)
  Note: Iteration target: 12 (max acceptable: 18) for 12 files
```

**Critical**: the letter grade and "Automated Score" reflect only the
automated 25% of the rubric, scaled to 0–100 using the formula:

```
automated_points = (report_quality / 100) * 10
                 + (iteration_efficiency / 100) * 15
total_score      = (automated_points / 25) * 100
```

A displayed grade of A+ means the agent scored 100% on the two automated
dimensions only. The grade is provisional — it will change once Finding
Quality (50%) and Skip Discipline (25%) are scored manually.

**History mode** prints a table of past grades with timestamps, agent name,
letter grade, automated score, and iteration count, sorted newest-first.

**Stats mode** prints total runs, latest grade, average automated score,
average per-dimension scores, and grade distribution (A+/A/A-/B+/… counts).

### Notes

- The grading store is local (no remote sync); grades are written to
  `~/.cache/squad/grades.json`.
- Omitting `--iterations` defaults to `0`, which scores Efficiency as 0% and
  appends "No iteration count provided". Always pass `--iterations` explicitly.
- Omitting `--files` defaults to `0`, which silently falls into the Small
  bucket. If the codebase is medium or large this produces an artificially
  strict score — always pass `--files`.
- If both `--history` and `--stats` are passed, `--history` takes precedence
  and `--stats` is silently ignored.
