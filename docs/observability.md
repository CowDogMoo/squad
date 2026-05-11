# Observability

## Quick debug checklist

Start here when something goes wrong:

| Symptom | First step |
|---|---|
| Run failed or behaved unexpectedly | `cat .squad/sessions/<id>/events.jsonl` |
| Need to see what the model is doing in real time | `squad run --agent go-review --stream` |
| Logs too noisy | `squad run --log-level warn ...` |
| Need full debug output | `squad run --log-level debug ...` or `-v` |
| Need traces in Jaeger or Grafana Tempo | `squad run --otel-endpoint localhost:4318 ...` |
| Want to check cost before running | `squad run --dry-run ...` |
| Output quality dropped vs. last time | `squad grade output.md --agent go-review ...` |

---

## Session logs

Every run automatically writes an append-only log to `.squad/sessions/` in the working directory. No configuration is needed — it is always on.

```
.squad/sessions/
└── 20260507T143022Z-a1b2c3d4/
    ├── events.jsonl   # one JSON line per event, append-only
    ├── meta.json      # session metadata, rewritten on each update
    └── results/       # large tool outputs (> 8 KiB) spilled to disk
        └── b3f1e2c9.txt
```

The session ID embeds a timestamp so `ls -t .squad/sessions/` gives you the most recent run first.

### events.jsonl

Each line is a JSON object with a timestamp, an event type, and a payload:

```jsonl
{"ts":"2026-05-07T14:30:22Z","type":"run_start","payload":{"agent":"go-review","model":"gpt-4o","provider":"openai"}}
{"ts":"2026-05-07T14:30:23Z","type":"prompt","payload":{"content":"Review the authentication changes in this PR..."}}
{"ts":"2026-05-07T14:30:25Z","type":"tool_call","payload":{"name":"read_file","input":{"path":"auth/middleware.go"}}}
{"ts":"2026-05-07T14:30:25Z","type":"tool_result","payload":{"tool_call_id":"tc_01","content":"package auth\n..."}}
{"ts":"2026-05-07T14:30:48Z","type":"iteration","payload":{"iteration":1}}
{"ts":"2026-05-07T14:31:10Z","type":"run_end","payload":{"status":"completed"}}
```

Event types: `run_start`, `resume`, `prompt`, `response`, `tool_call`, `tool_result`, `large_result`, `iteration`, `error`, `run_end`.

When a tool result exceeds 8 KiB it is written to `results/<id>.txt` and the inline event payload becomes a placeholder. The model can re-fetch it via the `get_tool_result` tool. This keeps `events.jsonl` readable even after runs that read large files.

### meta.json

`meta.json` is rewritten after every iteration and holds cumulative metrics for the session:

```json
{
  "session_id": "20260507T143022Z-a1b2c3d4",
  "created": "2026-05-07T14:30:22Z",
  "updated": "2026-05-07T14:31:10Z",
  "agent": "go-review",
  "provider": "openai",
  "model": "gpt-4o",
  "working_dir": "/home/user/myproject",
  "prompt": "Review the authentication changes in this PR...",
  "last_response_id": "resp_abc123",
  "status": "completed",
  "input_tokens": 12480,
  "output_tokens": 3210,
  "cost": 0.0621,
  "iterations": 8
}
```

Terminal statuses are `completed`, `error`, and `budget_exceeded`. A status of `running` means the session is active or was interrupted.

The `last_response_id` field powers `--resume`: it lets squad chain a new request onto the same conversation via the OpenAI Responses API, without re-sending the full transcript.

---

## Structured logging

Squad logs to stderr. The default format is plain text at info level.

```bash
# More verbose
squad run --agent go-review -v
squad run --agent go-review --log-level debug

# Less verbose
squad run --agent go-review -q
squad run --agent go-review --log-level warn

# Machine-readable (useful when piping to jq or a log aggregator)
squad run --agent go-review --log-format json

# ANSI color output
squad run --agent go-review --log-format color
```

These can be set in the config file instead of passing them every time:

```yaml
log:
  level: debug     # debug, info, warn, error
  format: color    # text, json, color
quiet: false
verbose: false
```

`--quiet` suppresses everything except errors. `--verbose` is shorthand for `--log-level debug`. They cannot be combined.

---

## Streaming output

Stream model output tokens to stderr as they arrive:

```bash
squad run --agent go-review --stream
```

Tokens appear on stderr so they do not interfere with the final output on stdout.

---

## OpenTelemetry tracing

Export traces to any OTLP-compatible backend:

```bash
# Via CLI flag
squad run --agent go-review --otel-endpoint localhost:4318

# Via environment variable
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318
squad run --agent go-review
```

When no endpoint is configured, a no-op tracer runs with zero overhead.

### Config file

```yaml
otel:
  endpoint: localhost:4318
```

### What gets traced

Spans are created for each layer of execution. A typical run produces a tree like this:

```
agent.run (go-review)
├── model.invoke
├── tool.call (read_file)
├── model.invoke
├── tool.call (bash)
│   └── executor.local
├── model.invoke
└── tool.call (mcp/filesystem/read)
    └── mcp.call (filesystem)
```

Pipeline runs add a layer above this:

```
pipeline.run (security-audit)
├── stage.run (scan)
│   └── agent.run (go-security-audit)
│       └── ...
└── stage.run (fix)
    └── agent.run (go-refactor)
        └── ...
```

Errors are recorded on the relevant span so you can filter to failed spans in Jaeger or Tempo.

### TLS and plaintext

Squad auto-detects whether to use TLS based on the endpoint:

- `localhost` or `127.0.0.1` → plaintext (no TLS)
- `http://` prefix → plaintext
- `https://` prefix → TLS
- Anything else → TLS

Override with `OTEL_EXPORTER_OTLP_INSECURE=true` to force plaintext, or `=false` to force TLS regardless of the endpoint format.

### Running Jaeger locally

```bash
docker run --rm -p 16686:16686 -p 4318:4318 jaegertracing/all-in-one
squad run --agent go-review --otel-endpoint localhost:4318
# open http://localhost:16686
```

---

## Cost budgeting

Stop a run when it reaches a spend limit:

```bash
# Single agent with a $2 budget
squad run --agent go-review --max-cost 2.00

# Composed agent with a $10 total budget
squad run --agent security-audit --max-cost 10.00
```

Budget enforcement is a hard stop. When the limit is reached the run exits with status `budget_exceeded` and the session log reflects it in `meta.json`.

Pricing is fetched from the LiteLLM community database at startup. Local Ollama models always show $0.00.

### Cost hints in agent.yaml

Agents can declare budget hints that inform cost estimates before a run:

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

`scale_factor: files` tells the estimator that cost grows with the number of files passed to the agent. `files_per_iteration` is how many files are processed each iteration on average. `children` lists the child agents a composed agent will invoke, so their budgets are factored in.

### Dry run

Use `--dry-run` to validate configuration and see what would run, without calling the model:

```bash
squad run --agent security-audit --dry-run
```

For a composed agent this prints the resolved pipeline structure:

```
Composed agent "security-audit" (v1.2.0) validated: 3 stages

Tier 1:
  Stage "scan" [mode=read]: go-security-audit
  Stage "lint" [mode=read]: go-review
Tier 2:
  Stage "fix" [mode=edit]: go-refactor
    depends_on: scan, lint

Gates:
  after "fix": go test ./... (on_failure=revert)
```

Stages in the same tier run in parallel. Stages with `depends_on` wait for their dependencies.

---

## Grading

Grade agent output against a quality rubric:

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

### How scoring works

The automated score covers 25% of the total grade. The other 75% requires manual review.

| Component | Weight | What it measures |
|---|---|---|
| Report quality | 10% | Required sections present in the output |
| Iteration efficiency | 15% | How close to the target iteration count for the codebase size |
| Finding quality | 50% | Manual: are the findings accurate and useful? |
| Skip discipline | 25% | Manual: did the agent skip appropriate issues? |

Iteration targets scale with codebase size: 12 iterations for ≤20 files, 25 for ≤50 files, 40 for larger codebases. Scores degrade linearly once you go over the target, hitting 0 at double the maximum acceptable count.

### Grade output

```
Grade: B (Automated Score: 83%)
  Report Quality:       100%
  Iteration Efficiency: 72%

  Iterations: 14 | Files: 22 | Touched: 18 | Fixed: 7 | Skipped: 2

  ⚠ Manual review required:
    - Finding Quality (50% of grade)
    - Skip Discipline (25% of grade)
  Note: Iteration target: 12 (max acceptable: 18) for 22 files
```

Letter grades run from F through A+, with A+ at ≥97%, A at ≥93%, down through B, C, D, and F below 60%.

### Regression gates in pipelines

Pipelines can run shell commands after a stage and halt or revert if they fail. This is how you prevent a fix stage from breaking tests:

```yaml
gates:
  - after: fix
    command: go test ./...
    on_failure: revert   # or "stop" or "continue"
```

`revert` runs `git checkout .` and marks the stage as reverted. `stop` halts the pipeline immediately. `continue` logs the failure but keeps going. Gates are skipped when no files were changed by the stage.

Combining grading history with gates lets you track quality trends across runs: grade the output after each run, use `--stats` to spot drift, and tighten iteration targets in `budget.estimated_iterations` when you see consistent overuse.
