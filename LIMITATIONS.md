# Squad Agent Limitations

## Parallel Sub-Agents

### Problem

The Task tool spawns child agents sequentially — the parent blocks on
each `CallModel` call until the child completes. Even when the LLM
requests multiple Task calls in a single response, the tool loop
processes them one at a time.

GO-TESTS-GOAL.md shows 4 parallel Task agents each writing tests for
a different package simultaneously. This is not possible today.

### What Exists

- `tools/task.go` — blocking Task tool with depth tracking (max 3)
- `tools/tools.go` — sequential tool loop processes one call at a time
- Agent prompts reference the Task tool but cannot exploit parallelism

### Solution

1. Add a `background` (bool) parameter to the Task tool definition.
2. When `background=true`, spawn the child in a goroutine and return
   a task ID immediately.
3. Add a `TaskResult` tool that blocks until a background task completes
   and returns its output.
4. Use a semaphore (channel of size N, start with N=4) to cap
   concurrent children.
5. The LLM can then call Task 4 times with `background=true`, do other
   work, then call TaskResult 4 times to collect results.

### Files to Change

- `tools/task.go` — add `background` arg parsing, goroutine spawning,
  task registry (map of ID to result channel), and `taskResultTool`
  handler
- `tools/tools.go` — register `TaskResult` in `BuildHandlers`
- Agent prompts — update Phase 3 to show the background/collect pattern
