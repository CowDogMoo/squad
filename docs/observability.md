# Observability

## Streaming Output

Stream model output tokens to stderr as they arrive:

```bash
squad run --agent go-review --stream
```

Tokens appear on stderr so they don't interfere with the final output on stdout.

## OpenTelemetry Tracing

Export traces from agent runs to any OTLP-compatible backend:

```bash
# Via CLI flag
squad run --agent go-review --otel-endpoint localhost:4318

# Via environment variable
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318
squad run --agent go-review
```

Traces cover agent execution, tool calls, model invocations, pipeline stages,
and MCP interactions.

### Config File

```yaml
otel:
  endpoint: localhost:4318
```

## Cost Budgeting

Limit spend with `--max-cost` (in USD). The agent stops when the budget is
exhausted.

```bash
# Single agent with $2 budget
squad run --agent go-review --max-cost 2.00

# Pipeline with $10 total budget
squad pipeline run audit.yaml --max-cost 10.00
```

Agents can declare cost estimation hints in `agent.yaml`:

```yaml
budget:
  max_tokens: 4000
  estimated_iterations: 12
  scale_factor: files
  files_per_iteration: 4
  children:
    - go-review
    - go-security-audit
```

Use `--dry-run` to see cost estimates before running.

## Grading

Grade agent outputs against a quality rubric:

```bash
# Grade from a file
squad grade output.md --agent go-review --iterations 15 --files 12

# Grade from stdin
cat output.md | squad grade - --agent go-review --iterations 15

# View grade history
squad grade --history --agent go-review

# View aggregate stats
squad grade --stats --agent go-review
```
