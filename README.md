# squad

**Model-agnostic agent CLI built on LangChainGo.**

[![License](https://img.shields.io/github/license/CowDogMoo/squad?label=License&style=flat&color=blue&logo=github)](https://github.com/CowDogMoo/squad/blob/main/LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/CowDogMoo/squad?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/CowDogMoo/squad?label=Release&logo=github)](https://github.com/CowDogMoo/squad/releases)
[![🚨 Semgrep](https://github.com/CowDogMoo/squad/actions/workflows/semgrep.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/semgrep.yaml)
[![Pre-Commit](https://github.com/CowDogMoo/squad/actions/workflows/pre-commit.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/pre-commit.yaml)
[![Renovate](https://github.com/CowDogMoo/squad/actions/workflows/renovate.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/renovate.yaml)
[![codecov](https://codecov.io/github/CowDogMoo/squad/graph/badge.svg?token=O74GTQA4J7)](https://codecov.io/github/CowDogMoo/squad)

## Install

```bash
go install github.com/cowdogmoo/squad/cmd/squad@latest
```

Or build from source:

```bash
go build ./cmd/squad
```

## Quick Start

```bash
# Add the official agents repository
squad agents add official https://github.com/cowdogmoo/squad-agents.git

# List available agents
squad agents list

# Run an agent
squad run --agent go-review
```

## Agents

- **[Official Agents](https://github.com/CowDogMoo/squad-agents)** - Production-ready
  agents for Go, Python, and Ansible

Each agent operates in two modes:

- **Fix mode** (default): Autonomously applies fixes
- **Analyze mode** (`--mode readonly`): Reports issues without modifying files

### Agent Sources

Agents are loaded from multiple sources in priority order:

1. **Local `./agents/` directory** - For development
2. **Configured local paths** - Custom agent directories
3. **Git repositories** - Remote agent collections
4. **XDG config directories** - User agents (`~/.config/squad/agents`)

```bash
# List available agents
squad agents list

# Add a git repository
squad agents add https://github.com/user/agents.git
squad agents add myrepo https://github.com/user/agents.git

# Add a local path
squad agents add /path/to/agents

# List configured sources
squad agents sources

# Update git repositories
squad agents update

# Remove a source
squad agents remove myrepo
```

### Creating Agents

```bash
# Create a new agent from templates
squad init agent my-review --lang go

# Create from an existing agent
squad init agent my-review --from go-review

# Test your agent
squad run --agent my-review --print
```

See [docs/creating-agents.md](docs/creating-agents.md) for the full guide.

### Parallel Agent Execution

Agents can spawn child agents using the `Task` tool. For parallel execution,
use `background=true` to spawn multiple agents concurrently, then collect
results with `TaskResult`.

```text
# Inside an agent prompt, spawn two agents in parallel:
Task(agent="go-review", background=true, prompt="fix code quality issues")
Task(agent="go-security-audit", background=true, prompt="fix security issues")

# Collect results:
TaskResult(task_id="bg-1")
TaskResult(task_id="bg-2")
```

**Features:**

- **Concurrent execution**: Up to 4 background tasks run simultaneously
- **Automatic semaphore**: Prevents resource exhaustion
- **Depth limiting**: Maximum 3 levels of nested agent calls
- **Panic recovery**: Background tasks recover gracefully from panics

**Example orchestration prompt:**

```bash
squad run --agent go-cobra \
  --model gpt-4o \
  --require-actionable=false \
  "Run go-review and go-security-audit IN PARALLEL using background=true.
   Spawn both with Task(background=true), then collect with TaskResult.
   Report what was fixed."
```

This spawns both agents concurrently, reducing total wall time compared to
sequential execution.

## Pipelines

Declarative multi-agent pipelines run multiple agents across stages with
dependency ordering, parallel execution, regression gates, and structured
output.

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

### Pipeline YAML Format

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

**Features:**

- **Dependency ordering**: Stages execute in topological order
- **Parallel agents**: Multiple agents in a stage run concurrently
- **Regression gates**: Shell commands validate state between stages
- **Gate actions**: `revert` undoes stage changes on failure; `stop` halts
  the pipeline
- **Cost budgeting**: `--max-cost` limits total spend across all agents
- **Structured output**: JSON or Markdown reports with per-stage results

### Scaffold a Pipeline

```bash
squad init pipeline my-pipeline
```

## Streaming Output

Stream model output tokens to stderr as they arrive:

```bash
squad run --agent go-review --stream
```

This is useful for watching agent progress in real time. Tokens appear on
stderr so they don't interfere with the final output on stdout.

## MCP Servers

Agents can connect to [Model Context Protocol](https://modelcontextprotocol.io/)
servers for additional tool access (databases, APIs, monitoring systems).

### CLI Flags

```bash
# Stdio transport: NAME:COMMAND[:ARG1,ARG2,...]
squad run --agent go-review \
  --mcp-server mytools:npx:@myorg/mcp-server

# SSE transport: NAME:sse:URL
squad run --agent go-review \
  --mcp-server grafana:sse:https://grafana.example.com/mcp
```

### Agent Manifest

Declare MCP servers in `agent.yaml` so they're always available:

```yaml
mcp_servers:
  - name: grafana
    transport: sse
    url: https://grafana.example.com/mcp
  - name: mytools
    command: npx
    args: ["@myorg/mcp-server"]
```

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

## Execution Backends

Agents can run commands in different execution environments. The backend is
declared in the agent's `agent.yaml` manifest.

| Backend  | Description                      | Key Options                          |
| -------- | -------------------------------- | ------------------------------------ |
| `local`  | Default local shell              | _(none)_                             |
| `docker` | Docker container                 | `image`, `volumes`, `env`, `shell`   |
| `ssm`    | AWS Systems Manager (EC2)        | `instance_id`, `region`, `profile`   |
| `kubectl`| Kubernetes pod                   | `pod`, `namespace`, `container`      |

### Example: Docker Backend

```yaml
# agent.yaml
environment:
  type: docker
  options:
    image: golang:1.23
    volumes: ".:/workspace"
    working_dir: /workspace
    shell: /bin/bash
```

### Example: Kubernetes Backend

```yaml
# agent.yaml
environment:
  type: kubectl
  options:
    pod: build-pod
    namespace: ci
    container: golang
    shell: /bin/bash
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

## Providers

Providers are OpenAI-compatible by default. Configure with flags, environment
variables, or config file.

### Provider Matrix

| Provider  | Status    | Base URL                              | API Key  | Notes                                        |
| --------- | --------- | ------------------------------------- | -------- | -------------------------------------------- |
| openai    | supported | `https://api.openai.com/v1` (default) | required | Supports `--api-type azure` for Azure OpenAI |
| ollama    | supported | `http://localhost:11434/v1` (default) | optional | Uses `max_tokens` for compatibility          |
| anthropic | planned   | —                                     | —        | Not yet implemented                          |
| gemini    | planned   | —                                     | —        | Not yet implemented                          |

### OpenAI

```bash
squad run --agent go-review --provider openai --model gpt-4.1-mini
```

### Ollama

```bash
# Local Ollama
squad run --agent go-review \
  --provider ollama \
  --base-url http://localhost:11434/v1 \
  --model qwen2.5-coder:7b-instruct

# Remote Ollama (Kubernetes ingress)
squad run --agent go-review \
  --provider ollama \
  --base-url https://ollama.example.com/v1 \
  --model qwen2.5-coder:7b-instruct
```

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

## Taskfile Tasks

The project includes [Taskfile](https://taskfile.dev) tasks for running agents
with common configurations.

```bash
# Run an agent in fix mode
task run:go-review WORKING_DIR=/path/to/project

# Run in analyze mode (append -analyze)
task run:go-review-analyze WORKING_DIR=/path/to/project

# Run coverage agent with target
task run:go-tests COVERAGE_TARGET=90 WORKING_DIR=/path/to/project
```

### Task Variables

| Variable          | Default              | Description                     |
| ----------------- | -------------------- | ------------------------------- |
| `PROVIDER`        | `ollama`             | LLM provider                    |
| `MODEL`           | `qwen3-coder:30b`    | Model name                      |
| `API_KEY`         | —                    | API key for the provider        |
| `BASE_URL`        | _(provider default)_ | Custom API endpoint             |
| `WORKING_DIR`     | `.`                  | Target codebase directory       |
| `COVERAGE_TARGET` | `75`                 | Target coverage % (test agents) |

## Configuration

Configuration uses XDG paths with environment variable and CLI flag overrides.

### Precedence (highest to lowest)

1. CLI flags
2. Environment variables (`SQUAD_*`)
3. Configuration file
4. Built-in defaults

### Commands

```bash
# Initialize config file
squad config init

# Show current configuration
squad config show

# Set a value
squad config set provider.default openai
squad config set model.default gpt-4.1-mini

# Get a value
squad config get provider.default
```

### Environment Variables

| Variable                           | Description                          |
| ---------------------------------- | ------------------------------------ |
| `SQUAD_PROVIDER_DEFAULT`           | Default provider                     |
| `SQUAD_PROVIDER_TOKEN`             | API key                              |
| `SQUAD_PROVIDER_BASE_URL`          | Base URL override                    |
| `SQUAD_MODEL_DEFAULT`              | Default model                        |
| `SQUAD_LOG_LEVEL`                  | Log level (debug, info, warn, error) |
| `SQUAD_LOG_FORMAT`                 | Log format (text, json, color)       |
| `SQUAD_OTEL_ENDPOINT`              | OpenTelemetry OTLP endpoint          |
| `OTEL_EXPORTER_OTLP_ENDPOINT`     | Standard OTEL endpoint (also works)  |

### Example Config

`~/.config/squad/config.yaml`:

```yaml
log:
  level: info
  format: color

provider:
  default: ollama
  base_url: http://localhost:11434/v1

model:
  default: qwen2.5-coder:7b-instruct
  temperature: 0.3

agents:
  repositories:
    official: https://github.com/cowdogmoo/squad-agents.git
  local_paths:
    - ~/dev/my-agents

otel:
  endpoint: localhost:4318
```
