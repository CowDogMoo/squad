# Composed Agents (Pipelines)

Composed agents run multiple sub-agents across stages with dependency
ordering, parallel execution, regression gates, and structured output.
A composed agent declares its pipeline topology in `agent.yaml` — users
run it with `squad run` like any other agent.

## Running Composed Agents

```bash
# Run a composed agent
squad run --agent security-audit "Assess the target system"

# Run with cost limit and output file
squad run --agent security-audit --max-cost 5.00 --out report.md

# Validate without running (shows stages and gates)
squad run --agent security-audit --dry-run

# Force JSON output
squad run --agent security-audit --json
```

## Composed Agent Manifest

A composed agent's `agent.yaml` uses `stages` instead of `entrypoint`.
Squad detects this automatically.

```yaml
name: security-audit
version: v1
description: Multi-stage security review

stages:
  - name: review
    agent: go-review

  # Parallel agents within a stage
  - name: analysis
    agents:
      - go-review
      - go-security-audit

  # Stage with dependencies, mode, and variables
  - name: testing
    agent: go-tests
    depends_on: [review]
    mode: edit
    vars:
      COVERAGE_TARGET: "85"

# Regression gates run shell commands after a stage completes
gates:
  - after: review
    command: "go build ./..."
    on_failure: revert   # revert | stop (default: stop)
  - after: testing
    command: "go test ./..."
    on_failure: stop
```

## Features

- **Dependency ordering**: Stages execute in topological order
- **Parallel agents**: Multiple agents in a stage run concurrently
- **Regression gates**: Shell commands validate state between stages
- **Gate actions**: `revert` undoes stage changes on failure; `stop` halts
  the pipeline
- **Cost budgeting**: `--max-cost` limits total spend across all agents
- **Structured output**: JSON or Markdown reports with per-stage results
- **Unified entry point**: `squad run --agent <name>` works for both leaf
  and composed agents

## Leaf vs Composed Manifests

| | Leaf Agent | Composed Agent |
|---|---|---|
| Has `entrypoint` | Yes | No |
| Has `stages` | No | Yes |
| Has `models` | Yes | No (sub-agents declare their own) |
| Run with | `squad run --agent` | `squad run --agent` |
