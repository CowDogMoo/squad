# Goal

Switch the Ollama provider from the OpenAI-compatible endpoint (`/v1/chat/completions`)
to Ollama's native `/api/chat` endpoint so that:

1. The full agent bundle is processed (not silently truncated to 4096 tokens).
2. Tool definitions are visible to the model, enabling agentic tool-call loops
   (Glob, Read, Edit, Grep, Bash).
3. The context window size (`num_ctx`) is configurable per-request via `--num-ctx`.

## Status

- [x] Native Ollama client (`cmd/squad/ollama.go`) implementing `llms.Model`
- [x] `--num-ctx` flag (default 32768)
- [x] Taskfile updated to remove `/v1` from default `BASE_URL`
- [x] Tool calls confirmed working (PromptTokens: 8654, multi-iteration loops)
- [x] Model makes edits via Edit tool (`llama3.3:70b` confirmed working)

## Completed

All three goals are met:

1. **Full bundle processing** — `llama3.3:70b` processes the complete go-review
   agent bundle (system.md + agent.md + go-review-criteria.md + user prompt)
   without truncation. `prompt_eval_count` values confirm full context ingestion.

2. **Tool-call loops work** — `llama3.3:70b` successfully calls Read, Edit, and
   Bash in a single iteration, then returns a structured review in subsequent
   iterations. The agentic loop executes correctly.

3. **`num_ctx` configurable** — `--num-ctx 32768` passes through to the native
   `/api/chat` request and the model operates at full context width.

### Key fixes applied

- **`flexBool` type** (`cmd/squad/tools.go`): LLMs sometimes send boolean fields
  as strings (`"false"` instead of `false`). Added a custom JSON unmarshaler that
  accepts both, unblocking the Edit tool for `llama3.3:70b`.

- **Taskfile default model** updated from `qwen3-coder:30b` to `llama3.3:70b`.

### Model comparison

| Model | Reads files | Generates review | Calls Edit tool |
|-------|-------------|-----------------|-----------------|
| `qwen3-coder:30b` | Yes | Yes | No |
| `qwen3:32b` | Yes | Yes | No |
| `llama3.3:70b` | Yes | Yes | **Yes** |

### go-review agent verification

The go-review agent (`agents/go-review/`) was tested end-to-end with `llama3.3:70b`:

- Agent bundle builds correctly (system.md + agent.md + references)
- Output follows the structured format from `system.md` (Summary, Critical Issues,
  Improvements, Positive Observations, Recommendations)
- All 12 review categories from `go-review-criteria.md` are available to the model
- Severity classification (CRITICAL/HIGH/MEDIUM/LOW/INFO) is used in output
- Tool loop: Read → Edit → Bash executed in a single iteration
