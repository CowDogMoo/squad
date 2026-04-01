# Pipelines

Declarative multi-agent pipelines run multiple agents across stages with
dependency ordering, parallel execution, regression gates, and structured
output.

## Running Pipelines

```bash
# Run a pipeline
squad pipeline run security-audit.yaml "Assess the target system"

# Run with cost limit and output file
squad pipeline run recon.yaml --max-cost 5.00 --out report.md

# Validate without running
squad pipeline run recon.yaml --dry-run

# Force JSON output
squad pipeline run recon.yaml --json
```

## Pipeline YAML Format

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

# Output format for the pipeline report
output:
  format: json  # json | markdown (default: markdown)
```

## Features

- **Dependency ordering**: Stages execute in topological order
- **Parallel agents**: Multiple agents in a stage run concurrently
- **Regression gates**: Shell commands validate state between stages
- **Gate actions**: `revert` undoes stage changes on failure; `stop` halts
  the pipeline
- **Cost budgeting**: `--max-cost` limits total spend across all agents
- **Structured output**: JSON or Markdown reports with per-stage results

## Scaffold a Pipeline

```bash
squad init pipeline my-pipeline
```
