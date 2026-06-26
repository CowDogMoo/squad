# Design: Sharded (map/reduce) agent execution

Status: **DRAFT for review** — no code yet.
Author: (pairing session)
Date: 2026-06-25

## 1. Problem

squad runs each leaf agent as a **single linear tool loop with one ever-growing
context window**. Every file the agent reads accumulates in the same context.
For agents whose unit of work is **independent per file** — `degpt`,
`go-scrub-comments`, `rust-scrub-comments` — this is the wrong shape:

- Context bloats as the agent reads all N files → weak/local models loop,
  slow down, or overflow the window (observed: gpt-oss-120b looped to 23
  iterations / 6 min and covered 5/32 files; glm-4.7-flash overflowed a 32K
  slot outright).
- The agent prompt is forced to babysit the harness's job
  ("read 4-6 files per iteration", "do NOT re-Glob", "STOP after report") —
  brittle instructions that the model ignores under load.

The work is embarrassingly parallel: scanning `a.md` has nothing to do with
scanning `b.md`. squad should **partition the input and run the agent per
shard with isolated context**, then merge. The model should never have to
reason about context budget.

## 2. Goal & non-goals

**Goal:** an opt-in, manifest-declared execution mode that shards an agent's
input (by file, batched), runs the existing single-agent loop once per shard
with fresh context, and merges the per-shard outputs into one result —
preserving edit mode, readonly mode, the fabrication guard, and metrics.

**Non-goals:**

- Not the default. Whole-repo-context agents (cross-file refactors, dependency
  tracing, `go-review`) must be unaffected. Sharding is opt-in per agent.
- Not a replacement for pipelines (which compose *different* agents). This
  shards *one* agent over *data*.
- Sharding is by file or by package (directory). `shard_by: package` groups a
  glob's matches by parent directory so every package runs as one shard with
  full intra-package context — the unit for guaranteed whole-repo coverage
  (see NEWBIE.md). Finer units (function) are out of scope; the schema leaves
  room to extend.

## 3. Manifest schema

New optional `execution:` block on the leaf-agent manifest
(`agent.Manifest`, `agent/bundle.go`):

```yaml
execution:
  shard_by: file          # "" (default, no sharding) | "file" | "package"
  glob:                   # patterns squad globs to build the work-list.
    - "**/*.md"           #   Required when shard_by is "file" or "package".
    - "**/*.txt"
  exclude:                # optional ignore globs (node_modules, vendor, .git…)
    - "**/node_modules/**"
  shard_batch: 4          # files per shard (default 1 = strict per-file)
  merge: reports          # "reports" (concat per-shard text) | "none"
  max_parallel: 1         # v1 default 1 (sequential); >1 enables concurrency (v2)
  on_shard_error: continue # "continue" (aggregate failures) | "stop"
```

```go
// agent/bundle.go
type ExecutionConfig struct {
    ShardBy      string   `yaml:"shard_by,omitempty"`      // "" | "file" | "package"
    Glob         []string `yaml:"glob,omitempty"`
    Exclude      []string `yaml:"exclude,omitempty"`
    ShardBatch   int      `yaml:"shard_batch,omitempty"`   // default 1
    Merge        string   `yaml:"merge,omitempty"`         // default "reports"
    MaxParallel  int      `yaml:"max_parallel,omitempty"`  // default 1
    OnShardError string   `yaml:"on_shard_error,omitempty"`// default "continue"
}
// Manifest gains:  Execution *ExecutionConfig `yaml:"execution,omitempty"`
```

Validation at bundle-load: `shard_by: file`/`package` requires non-empty
`glob`; `merge` ∈ {reports,none}; `shard_batch` ≥ 1; `max_parallel` ≥ 1.
`shard_by: package` groups the globbed files by parent directory (one shard
per directory); `shard_batch` does not apply.

## 4. Execution model

In `ExecuteRun` (`runner/run.go`), after `prepareBundle` and before the single
`InvokeModel` call, branch on `bundle.Manifest.Execution.ShardBy`:

```
if shardBy == "file":
    files   := globWorkList(workingDir, exec.Glob, exec.Exclude)   // squad owns discovery
    shards  := batch(files, exec.ShardBatch)
    results := runShards(ctx, opts, bundle, shards)                // each → InvokeModel, isolated ctx
    response, m := merge(results, exec.Merge)
    handleResponse(merged response)                                // one validate/apply/write
else:
    (today's single InvokeModel path, unchanged)
```

Per-shard run (`runShard`):

- Clones `opts` with the shard's file list injected. The shard files are
  surfaced to the agent via the prompt (a `{{.ShardFiles}}` template var and/or
  an injected "Files assigned to you:" block), so the prompt no longer globs.
- **Fresh context per shard**: a new `InvokeModel` call gets its own message
  history and `tools.ResetEditsApplied(ctx)`. This is the core win — each run
  sees only its 1–N files.
- Returns `(response string, *metrics.Metrics, error)` — `InvokeModel`'s
  existing signature; no new primitive needed.

`InvokeModel` is reused as-is. Sharding is orchestration *around* it.

## 5. Merge semantics

- `merge: reports` — concatenate per-shard response text under a synthesized
  header per shard (`## <file(s)>`), then a roll-up summary line
  (`N files scanned, M edited, K clean`). Re-emit the required literal markers
  once at the top (`Files touched: …` aggregated across shards, or
  `Files touched: none` + `No changes` when all clean).
- `merge: none` — emit shards verbatim, separated.

Edits are applied **by each shard's own loop** against its own files (edit mode
uses the Edit tool directly; no unified-diff reassembly needed). Merge is about
the *report*, not the edits.

## 6. Guard & budget interactions

- **Fabrication guard** (`validateActionableChanges`): runs **per shard**
  (inside each shard's handleResponse-equivalent), where the claim↔tree check is
  cleanest (a shard claims edits to its own file). The merged report is not
  re-checked for fabrication (it's an aggregate). `tools.EditsApplied` is
  per-shard ctx. This actually *strengthens* the guard — fabrication is caught
  at file granularity instead of being masked by other files' real edits.
- **Iteration budget** (`scale_factor: files`, `MaxIterations`,
  `IterationFactor`): becomes **per-shard** — each shard gets the budget for
  *its* file count (batch size), so a 1-file shard gets a small cap and can't
  loop for 23 iterations. This makes the existing `files_per_iteration` budget
  finally meaningful. Total run budget = sum of shard budgets, logged.
- **EditDeadline / CommentsOnly**: apply per shard unchanged.
- **Metrics**: aggregate tokens/cost/iterations across shards into one
  `*metrics.Metrics` (sum), so `printMetrics` and history reporting are
  unchanged downstream.

## 7. Agent (degpt) changes once sharding lands

The prompt sheds all context-management scaffolding:

- Delete the Phase-1 "Glob all four patterns / FROZEN set / do NOT re-Glob"
  machinery and the "read 4-6 files per iteration / READ-ONCE / phase boundary"
  rules — squad now hands the agent its files.
- System prompt becomes: "Score the prose paragraphs in the file(s) provided.
  Flag/rewrite per the rules. Report." No discovery, no budget babysitting.
- `budget.scale_factor: files` stays (now drives per-shard caps).
- Same `detect-llm-tells` skill, same edit/readonly gating.

`go-scrub-comments` / `rust-scrub-comments` adopt the same `execution:` block
(glob `**/*.go` / `**/*.rs`).

## 8. Parallelism

- **v1: `max_parallel: 1` (sequential).** Lands the context-isolation win with
  zero concurrency risk. Each shard is small → fast even serially.
- **v2: `max_parallel: N`.** Bounded worker pool over shards, mirroring the
  tier-parallel pattern already in `pipeline/pipeline.go`. Edit-mode shards
  touch disjoint files (no write conflicts by construction, since shards
  partition the file set). Metrics/merge collected after the barrier.

## 9. Failure handling

- `on_shard_error: continue` (default): a shard that errors (model error,
  budget exceeded, guard rejection) is recorded as a failed shard in the merged
  report; other shards proceed. Exit non-zero iff ≥1 shard failed *and* none
  succeeded, OR a stricter policy is chosen.
- `on_shard_error: stop`: first failure aborts remaining shards.
- A shard whose fabrication guard fires fails *that shard only* — the offending
  file's report is dropped, not the whole run.

## 10. Edge cases

- **0 files matched**: emit a clean no-findings report (`Files touched: none` /
  `No changes`), exit 0. Do not error.
- **1 file / 1 shard**: behaves like a normal single run (no overhead beyond a
  glob).
- **Working tree dirty before run**: per-shard guard only attributes a shard's
  own claimed files; pre-existing dirt elsewhere is ignored (same as today).
- **`working_dir: none` agents**: incompatible with `shard_by: file`; reject at
  load.

## 11. Testing

- Unit: `globWorkList` (include/exclude), `batch` (batching + remainder),
  `merge` (marker re-emission, roll-up counts), schema validation.
- Integration (table-driven, fake `InvokeModel`): N shards → merged report;
  one shard fabricates → that shard fails, others pass; 0-file → clean report;
  edit-mode shards apply to disjoint files.
- End-to-end smoke: degpt over a 3-file fixture (1 slop, 2 clean) on local
  gpt-oss → exactly the slop file edited, fast, exit 0, report lists all 3.

## 12. Backward compatibility

- No `execution:` block → today's exact behavior. Zero risk to existing agents.
- New manifest field is optional and omit-empty.
- The CLI gains no required flags; an optional `--no-shard` escape hatch can
  force the single-loop path for debugging.

## 13. Open questions

1. **Discovery ownership**: squad globs (proposed). Confirm no agent needs to
   influence the work-list dynamically (e.g., skip files by content) — if so,
   add an optional pre-filter hook later.
2. **Merge for edit mode**: is a concatenated report enough, or do we want a
   structured roll-up (JSON summary + per-file sections)? Proposed: text concat
   + roll-up header for v1.
3. **Budget accounting**: sum per-shard, or impose a global ceiling that halts
   remaining shards when hit? Proposed: per-shard caps + logged total; optional
   global ceiling later.
4. **Where the shard loop lives**: `ExecuteRun` (proposed) vs a new
   `runner/shard.go` orchestrator called by `ExecuteRun`. Proposed: new
   `runner/shard.go`, keep `ExecuteRun` thin.

```
