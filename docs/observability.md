# Observability

## Quick debug checklist

Start here when something goes wrong:

| Symptom | First step |
|---|---|
| Run failed or behaved unexpectedly | `squad watch --once <id>`, or read `events.jsonl` in the session directory (`squad sessions open <id>`) |
| Need to see what the agent is doing in real time | `squad watch` in a second terminal, or `squad run --agent go-review --stream` |
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

Each line is a JSON object with a timestamp, an event type, the trace and span
ids of the span that was active when it was written, and a payload:

```jsonl
{"ts":"2026-11-18T16:03:55Z","type":"run_start","trace_id":"6f48ef07fae64f7ad4f7a483a10f534c","span_id":"c1d375427ca8ed38","payload":{"agent":"probe","mode":"readonly","model":"qwen3.8:27b","provider":"ollama","system_bytes":25603}}
{"ts":"2026-11-18T16:05:44Z","type":"iteration","trace_id":"6f48ef07fae64f7ad4f7a483a10f534c","span_id":"bee6730caaa1660b","payload":{"index":1,"tool_calls":2}}
{"ts":"2026-11-18T16:05:44Z","type":"response","trace_id":"6f48ef07fae64f7ad4f7a483a10f534c","span_id":"bee6730caaa1660b","payload":{"iteration":1,"text":"I'll run the first two commands together since they're independent.","finish_reason":"stop","input_tokens":7669,"output_tokens":73,"tool_calls":2,"duration_ms":109055}}
{"ts":"2026-11-18T16:05:44Z","type":"tool_call","trace_id":"6f48ef07fae64f7ad4f7a483a10f534c","span_id":"f4d0a7a7fd85e9f5","payload":{"call_id":"call_38amfbhv","iteration":1,"name":"Bash","args":"{\"command\":\"cat notes.txt\"}"}}
{"ts":"2026-11-18T16:05:44Z","type":"tool_result","trace_id":"6f48ef07fae64f7ad4f7a483a10f534c","span_id":"f4d0a7a7fd85e9f5","payload":{"call_id":"call_38amfbhv","iteration":1,"name":"Bash","bytes":174,"duration_ms":32,"is_error":false}}
{"ts":"2026-11-18T16:06:49Z","type":"tool_denied","trace_id":"6f48ef07fae64f7ad4f7a483a10f534c","span_id":"88a891aa00e442ac","payload":{"call_id":"call_t8tjrnom","iteration":2,"name":"Write","reason":"readonly","message":"error: Write is not permitted in readonly mode (this run is analysis-only and must not modify files)"}}
{"ts":"2026-11-18T16:07:30Z","type":"run_end","trace_id":"6f48ef07fae64f7ad4f7a483a10f534c","payload":{"status":"completed"}}
```

Event types: `run_start`, `resume`, `prompt`, `iteration`, `response`, `tool_call`, `tool_result`, `tool_denied`, `large_result`, `error`, `skill_loaded`, `confirm_resolved`, `sign_off_resolved`, `permission_decision`, `run_end`.

The per-turn and per-tool events have the same shape on every provider path:
squad's own tool loop (`anthropic`, `openai`, `openai-compat`, `gemini`,
`ollama`), the OpenAI Responses API path, and `claude-code`, which squad reads
from the CLI's stream-json output.

| Event | Written | Payload |
|---|---|---|
| `iteration` | Once per model turn | `index` (1-based), `tool_calls` (calls the turn requested) |
| `response` | Once per model turn | `iteration`, `text` (the assistant's text), `thinking` and `thinking_bytes` (reasoning text, when the provider returns it), `thinking_tokens` (claude-code's reasoning estimate), `finish_reason`, `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `tool_calls`, `duration_ms` |
| `tool_call` | When a tool call starts | `call_id`, `name`, `args` (the raw argument JSON), `iteration` |
| `tool_result` | When a tool call finishes | `call_id`, `name`, `bytes` (output size), `duration_ms`, `is_error`, `error`, `iteration` |
| `tool_denied` | When a tool call is refused before it runs | `call_id`, `name`, `reason` (`readonly`, `sign_off`, `skill_allowed_tools`, or `permission` for a refusal the claude CLI reported), `message` (what the model was told), `iteration` |
| `error` | When a model call or the agentic CLI fails | `phase` (`generate`, `final`, `cli`), `iteration`, `error` |

Events from a claude-code subagent (a `Task` call inside the CLI) also carry
`parent_tool_use_id`, the id of the tool call that started the subagent.
Events from a child agent started by squad's `Task` tool share the parent's
`events.jsonl` and carry `agent`, the child agent's name, and
`parent_tool_use_id`, the id of the `Task` call (unless they already name a
claude-code subagent's call).
Responses API events keep their own extra fields (`label`, `id`, `status`,
`sent_bytes`). The `antigravity` provider prints only a final JSON envelope, so
its sessions hold `run_start`, `run_end` and, on failure, `error`.

`events.jsonl` stays on your machine and is written with mode `0600`. It keeps
full assistant text and full tool arguments (each value is capped at 64 KiB,
and reasoning text at 16 KiB), so treat it like the run's transcript.

`trace_id` and `span_id` join each event to its span in an exported trace (see
[OpenTelemetry tracing](#opentelemetry-tracing)): the `iteration` and
`response` events carry the turn span, and tool events carry the tool span.
Events written outside any span, such as `run_end`, carry only the run's trace
id. squad assigns the ids even when no trace exporter is configured.

When a tool result exceeds 8 KiB on the OpenAI Responses path it is written to `results/<id>.txt` and the result sent to the model becomes a placeholder. The model can re-fetch it via the `get_tool_result` tool.

### squad watch

`squad watch` prints a session's events as one line each and keeps following
the session until the run ends:

```bash
squad watch                   # latest session for this repository
squad watch 20261118T160355Z-ca6214b3
squad watch --once <id>       # print what is recorded so far and exit
squad watch ~/.local/state/squad/sessions/<id>   # a session directory
```

```text
session 20261118T162403Z-9bf04fdd · agent=probe provider=claude-code model=haiku
trace_id 952638b4393c6dd0e2aacfd9c83fe1db
claude_session_id 57acaab9-8a20-4f36-84e1-5d68e779deba
09:24:03  run_start     started · agent=probe model=haiku
09:24:06  iteration     iter 1 · 2 tool calls
09:24:06  response      response (iter 1) · +2 in / +4 out · I'll check the contents of `notes.txt` and whether the local endpoint on port 9 responds.
09:24:06  tool_call     Bash  {"command":"cat notes.txt","description":"Print contents of notes.txt"}
09:24:06  tool_call     Bash  {"command":"curl -s -m 2 -H \"Authorization: Bearer <token>\" http://…
09:24:06  tool_result   result · Bash · 10 B · 1092ms
09:24:06  tool_result   result · Bash · 11 B · 789ms
09:24:08  iteration     iter 2 · 1 tool calls
09:24:08  response      response (iter 2) · +2 in / +4 out · I'll list `/definitely-missing-dir` to confirm that a nonexistent path produces an error.
09:24:08  tool_call     Bash  {"command":"ls /definitely-missing-dir","description":"List a nonexistent direc…
09:24:08  tool_result   result · Bash · 66 B · 34ms · error: Exit code 1 ls: /definitely-missing-dir: No such file or di…
09:24:09  iteration     iter 3 · 0 tool calls
09:24:09  response      response (iter 3) · +2 in / +8 out · `notes.txt` contains "alpha" and "beta", the probe to 127.0.0.1:9 returned nothing so the fallback …
09:24:09  run_end       ended · completed
status=completed · 3 turns · 3 tool calls (1 failed, 0 denied) · 0 errors
```

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
  "trace_id": "6f48ef07fae64f7ad4f7a483a10f534c",
  "span_id": "c1d375427ca8ed38",
  "status": "completed",
  "input_tokens": 12480,
  "output_tokens": 3210,
  "cost": 0.0621,
  "iterations": 8
}
```

`trace_id` and `span_id` name the run's root `squad.run` span. A
`claude-code` run also records `claude_session_id`, the CLI's own session id,
which names the CLI's transcript under `~/.claude/projects/`. A `claude-code`
agent started by a `Task` call does not record its session id there.

Terminal statuses are `completed`, `error`, and `budget_exceeded`. A status of `running` means the session is active or was interrupted.

The `last_response_id` field powers `--resume` on the OpenAI Responses API path: it lets squad chain a new request onto the same conversation server-side, without re-sending the full transcript.

Every other provider (Anthropic, openai-compat, gemini, ollama) has a **stateless** API — there is no server-side conversation to chain onto. For those, `--resume` instead replays a locally persisted transcript. Each run on that path writes the full conversation to `transcript.json` alongside `meta.json` and `events.jsonl`; a resume loads it and prepends the prior turns to the new prompt. (`events.jsonl` is a lossy audit log and is **not** used for replay — only `transcript.json` is.)

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

When no endpoint is configured, nothing is exported. squad still assigns trace
and span ids, without recording spans, so `events.jsonl` and `meta.json` can
name them.

### Config file

```yaml
otel:
  endpoint: localhost:4318
```

### What gets traced

A run is one trace. On squad's own tool loop a run looks like this:

```
squad.run (squad.agent, squad.session.id)
└── agent.invoke
    └── llm.call (run token totals and cost)
        └── tool.loop
            ├── chat qwen3.8:27b (turn 1)
            │   ├── llm.generate
            │   ├── tool.Bash
            │   └── tool.Write (squad.tool.denied=true)
            ├── chat qwen3.8:27b (turn 2)
            │   └── llm.generate
            └── ...
```

Each model turn is a `chat <model>` span that parents the model call and the
tool calls that turn requested. A `claude-code` run has the same turns and tool
spans directly under `agent.invoke`, built from the CLI's stream-json output.
Their start and end times come from the CLI's event timestamps. A subagent the
CLI starts with its `Task` tool gets its own turns, nested under that `Task`
tool span. The OpenAI Responses API path has tool spans under
`responses.tool_loop` but no turn spans.

Pipeline runs add a layer above `agent.invoke`:

```
pipeline.run (security-audit)
├── pipeline.agent (scan)
│   └── agent.invoke
│       └── ...
└── pipeline.agent (fix)
    └── agent.invoke
        └── ...
```

Errors are recorded on the relevant span so you can filter to failed spans in Jaeger or Tempo.

### Span attributes

Spans carry names, timing, sizes, token counts and errors, but no assistant
text and no tool output, and by default no tool arguments (the one opt-in is
described under shell command attributes below). `events.jsonl` keeps all of
them in full locally:

| Span | Attributes |
|---|---|
| `squad.run` | `squad.agent`, `squad.session.id`, `gen_ai.provider.name`, `gen_ai.request.model` |
| `agent.invoke` | `gen_ai.provider.name`, `gen_ai.system`, `gen_ai.request.model`, `squad.agent`. For `claude-code` and `antigravity` also the run's `gen_ai.usage.*` totals, and for `claude-code` `squad.claude.session_id` |
| `llm.call` | Request parameters and the run's `gen_ai.usage.*` totals (API providers) |
| `chat <model>` | `gen_ai.operation.name=chat`, `gen_ai.provider.name`, `gen_ai.system`, `gen_ai.request.model`, `squad.turn.index`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.usage.cache_read.input_tokens`, `gen_ai.usage.cache_creation.input_tokens`, `gen_ai.response.finish_reasons`, `squad.turn.tool_calls`, `squad.turn.text_bytes`, `squad.turn.thinking_bytes`, `squad.turn.thinking_tokens` |
| `tool.<Name>` | `squad.tool.name`, `squad.tool.call_id`, `squad.tool.args_bytes`, `squad.tool.output_bytes`, `squad.tool.denied`, `squad.tool.denied_reason`, error status; Bash also `squad.tool.command` (see below) |

A tool call that failed has an error status. When the error is only the
tool's own output, as with `claude-code` tool results, the status reads
`tool returned an error` and the output itself is only in `events.jsonl`. A
failed `claude-code` run puts the CLI's error result on `agent.invoke`, or,
when there is none, its stderr and any stdout lines that are not JSON. No
stream-json event reaches the error, not even one cut off because the CLI was
killed while writing it.

`gen_ai.provider.name` and `gen_ai.system` both hold squad's provider name
(`ollama`, `claude-code`, ...). `gen_ai.system` is kept for backends that still
read the older attribute.

On `claude-code` turns the token counts are the ones the CLI streams with each
message, which it reports at the start of the message. `output_tokens` can
therefore be lower than the turn's final count; the run totals on
`agent.invoke` are exact.

### Shell command attributes

Bash tool spans carry `squad.tool.command` and `squad.tool.command_length`, and
Docker, kubectl and SSM executor spans carry `squad.executor.command` and
`squad.executor.command_length`. Commands often contain credentials, so by
default the `command` attribute holds only the executable name: the basename
of the command's first whitespace-separated word, when that word is a plain
name or path made of letters, digits and `. _ + - /`. Any other first word,
such as a `VAR=value` assignment, a quoted word, a `$(...)` substitution or a
`#` comment, is exported as `complex`. For example,
`/usr/bin/curl -H 'Authorization: Bearer ...' https://x | jq .token` is
exported as `curl`. The `command_length` attribute is the full command's
length in bytes.

To export the full command text instead, set `SQUAD_OTEL_EXPORT_COMMANDS=1`
(any other value leaves the default on).
On that path squad applies a small, best-effort redaction before export:
Bearer values, JWTs, `user:pass@` in URLs, and values of password, token,
secret and api_key style keys written as `key=value` or `key: value`. It has
no tool-specific rules and will miss credentials in other forms, such as
`-p <password>` flags, so only turn it on when the trace backend is trusted
with the commands your agents run. The exported text is truncated to 1000
bytes.

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

Prompt tokens that go through a provider's prompt cache are billed at their own rates, and the budget counts them that way. A run's cost is the sum of four components:

| Component   | Tokens                                   | Rate                                                                                       |
| ----------- | ---------------------------------------- | ------------------------------------------------------------------------------------------ |
| input       | Prompt tokens that did not use the cache | The model's input price                                                                    |
| cache read  | Prompt tokens served from the cache      | The model's cache-read price (on Claude, 0.1x the input price for most models)             |
| cache write | Prompt tokens written to the cache       | The model's cache-write price (on Claude, 1.25x the input price, or 2x for a 1-hour cache) |
| output      | Generated tokens                         | The model's output price                                                                   |

Cache prices come from the same LiteLLM entry as the input and output prices. When the entry has none and the provider is `anthropic` or `claude-code`, squad applies the multipliers from [Anthropic's pricing page](https://platform.claude.com/docs/en/about-claude/pricing#prompt-caching). On any other provider, a model without cache prices has its cached tokens charged at the input price, whatever the model is called.

The `claude-code`, `anthropic`, `openai`, and `openai-responses` providers report cache counts, as does an `openai-compat` endpoint that returns them. When a run used the cache, the metrics summary and the run logs name each component, and `meta.json` records `cache_read_tokens` and `cache_write_tokens` next to `input_tokens`, which stays the count of every prompt token.

The `gemini` provider is not covered yet: squad does not read the 32-bit token counts its client reports, so a Gemini model's usage is recorded as zero tokens and no cost, and does not count toward `--max-cost`.

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
