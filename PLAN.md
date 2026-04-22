# Composed Agents: Unified `squad run` Entry Point

## Goal

Unify `squad run` and `squad pipeline run` so users don't need to know
whether an agent is a single leaf or a composed pipeline. The agent's
`agent.yaml` declares its own topology.

```
squad run --agent go-security-audit "Audit this repo"   # works for both
```

## Design

### Manifest discrimination

If `agent.yaml` has `stages` → composed agent (pipeline orchestrator).
If `agent.yaml` has `entrypoint` → leaf agent (single model call).
These are mutually exclusive; validation enforces this.

**Leaf agent (unchanged):**

```yaml
name: go-security-resources
version: 0.1.0
models:
  - model: claude-sonnet-4-6
    provider: anthropic
entrypoint: system.md
wrapper: agent.md
```

**Composed agent (new):**

```yaml
name: go-security-audit
version: 0.1.0
description: Parallel security audit

stages:
  - name: audit
    agents:
      - go-security-injection
      - go-security-resources

gates:
  - after: audit
    command: "go build ./..."
    on_failure: stop
```

### No synthesis stage (for now)

A composed agent is purely an orchestrator — no entrypoint, no models,
no wrapper. If synthesis is needed, add a final stage with a dedicated
synthesis agent. This keeps the model simple and is additive later.

### Architecture

```
cmd/squad/run.go RunE
  ├─ findAgentDir() + LoadManifest()
  ├─ leaf?     → runner.ExecuteRun() [unchanged]
  └─ composed? → runComposedAgent()
                    ├─ validateComposedFlags()
                    ├─ manifestToPipeline()
                    ├─ pl.Runner.Run()
                    └─ outputReport()
```

### Package layout (no circular imports)

- `agent/composed.go` — ComposedStage, ComposedGate types + Manifest
  validation. No pipeline import.
- `runner/composed.go` — manifestToPipeline() conversion + FindAgentDir
  export. Imports both agent and pipeline.
- `cmd/squad/run.go` — fork point + runComposedAgent(). Reuses
  buildRunAgentFunc pattern from pipeline.go.

### Incompatible flags (composed agents)

These flags error with a clear message when used with composed agents:

| Flag | Why incompatible |
|------|-----------------|
| `--system` | No single system prompt |
| `--print-bundle` | No single bundle |
| `--bundle-out` | No single bundle |
| `--apply` | Pipeline manages output |
| `--apply-fallback` | Pipeline manages output |
| `--require-actionable` | Reports, not raw responses |
| `--stream` | Multiple agents, ambiguous |

Flags that DO work: `--model`/`--provider` (defaults for sub-agents
without their own), `--max-cost`, `--max-iterations`, `--out`, `--json`,
`--dry-run`, `--var`, `--working-dir`.

### --dry-run for composed agents

Validates pipeline structure, checks all sub-agents exist and parse,
prints stage topology. No API calls.

## Implementation

### Phase 1: Manifest extension (`agent/composed.go`) ✅

- [x] Define ComposedStage, ComposedGate, ComposedPreGate,
      ComposedPartition types
- [x] Add Stages, Gates, Description fields to Manifest
- [x] Add IsComposed() bool method
- [x] Add Validate() error method (stages XOR entrypoint)
- [x] Call Validate() from LoadManifest()
- [x] Tests in agent/composed_test.go

### Phase 2: Conversion + FindAgentDir export (`runner/composed.go`) ✅

- [x] Export FindAgentDir (rename from findAgentDir)
- [x] Add ManifestToPipeline() conversion function
- [x] Tests in runner/composed_test.go

### Phase 3: Fork in run command (`cmd/squad/run.go`) ✅

- [x] Load manifest before deciding path
- [x] If composed → runComposedAgent()
- [x] runComposedAgent(): validate flags, convert manifest, build
      runner, execute, output report
- [x] Reuse buildRunAgentFunc pattern from pipeline.go
- [x] Handle --dry-run for composed agents

### Phase 4: Tests + validation ✅

- [x] Unit tests for manifest validation
- [x] Unit tests for conversion
- [x] Unit tests for flag validation
- [x] Verify existing tests still pass

### Phase 5: Remove `squad pipeline run` ✅

- [x] Remove `newPipelineCmd()` and `newPipelineRunCmd()` — never released
- [x] Remove pipeline command registration from root.go
- [x] Keep reusable functions in pipeline.go (buildRunAgentFunc,
      buildComposedRunOpts, outputReport, flagOrViper, mergeVars)
- [x] Update pipeline_test.go for renamed functions
- [x] Update docs (pipelines.md, observability.md) to use `squad run --agent`

### What stays unchanged

- `pipeline/runner.go` — all orchestration logic untouched
- `pipeline/pipeline.go` — Pipeline type, validation, topo sort
- Leaf agent manifests — fully backwards compatible
- Agent resolution — source.Manager.FindAgent() unchanged
