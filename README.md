<div align="center">

<img src="docs/images/logos/squad-app-icon-transparent.png" alt="squad logo" width="320"/>

# `squad`

[![License](https://img.shields.io/github/license/CowDogMoo/squad?label=License&style=flat&color=blue&logo=github)](https://github.com/CowDogMoo/squad/blob/main/LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/CowDogMoo/squad?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/CowDogMoo/squad?label=Release&logo=github)](https://github.com/CowDogMoo/squad/releases)
[![codecov](https://codecov.io/github/CowDogMoo/squad/graph/badge.svg?token=O74GTQA4J7)](https://codecov.io/github/CowDogMoo/squad)
<br />
[![Semgrep](https://github.com/CowDogMoo/squad/actions/workflows/semgrep.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/semgrep.yaml)
[![Pre-Commit](https://github.com/CowDogMoo/squad/actions/workflows/pre-commit.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/pre-commit.yaml)
[![Renovate](https://github.com/CowDogMoo/squad/actions/workflows/renovate.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/renovate.yaml)

<h4><code>squad</code> is an open-source framework for building, sharing, and running AI agents from the command line.</h4>

</div>

---

## Overview

Inspired by [Daniel Miessler's Fabric](https://github.com/danielmiessler/fabric).
Define an agent in markdown and YAML, point it at any LLM, and turn it
loose on a codebase.

- Works with OpenAI, Anthropic, Google AI, Ollama, and any OpenAI-compatible endpoint (NVIDIA NIM, Databricks, DeepInfra, Together AI, vLLM, LM Studio, and more)
- Agents are just files: markdown prompts + YAML manifest, checked into git
- Built-in tools: Read, Write, Edit, Glob, Grep, Bash, plus any MCP server
- Multi-agent pipelines with dependency ordering, parallel stages, and regression gates
- Runs locally, in Docker, on Kubernetes, or over AWS SSM
- Scheduled unattended runs via OS-native daemons (launchd, systemd, Task Scheduler)

**Built for:**

- Security teams running code review, audits, and recon
- Platform engineers enforcing standards across repos
- Developers building review and refactoring workflows

## Prerequisites

| Requirement    | Version | Notes                                   |
| -------------- | ------- | --------------------------------------- |
| **Go**         | 1.24+   | Required for `go install`               |
| **LLM access** | -       | OpenAI, Anthropic, Google AI, or Ollama |

## Installation

```bash
go install github.com/cowdogmoo/squad/cmd/squad@latest
```

### Build from Source

```bash
git clone https://github.com/cowdogmoo/squad.git
cd squad
go build -o squad ./cmd/squad
```

### Verification

```bash
squad version
```

## Quick Start

```bash
# Initialize configuration
squad config init

# Add the official agents repository
squad agents add official https://github.com/cowdogmoo/squad-agents.git

# List available agents
squad agents list

# Run an agent against your codebase
squad run --agent go-review --provider openai --model gpt-4.1-mini

# Run with local Ollama — no API key required
squad run --agent go-review --provider ollama --model qwen2.5-coder:7b-instruct

# Run a composed (multi-agent) pipeline
squad run --agent security-audit "Review this codebase"

# Resume a prior run after a crash, ctrl-c, or budget stop
squad run --agent go-review --resume 20260429T150220Z-a1b2c3d4 \
    "continue where you left off; finish the remaining files"
```

## Usage

### Key Run Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--agent` | Agent name to run | required |
| `--provider` | LLM provider | config default |
| `--model` | Model identifier | config default |
| `--working-dir` | Target directory | current dir |
| `--max-cost` | USD budget cap | `5.00` |
| `--max-iterations` | Max tool-call loops | `100` |
| `--apply` | Apply suggested diffs via git apply | `false` |
| `--dry-run` | Estimate cost without running | `false` |
| `--stream` | Stream tokens to stderr in real time | `false` |
| `--resume` | Resume a session by ID | — |
| `--out` | Write response to file | — |
| `--var KEY=VALUE` | Template variable (repeatable) | — |
| `--mcp-server` | MCP server spec (repeatable) | — |
| `--isolate` | Isolation mode (e.g. `worktree`) | — |

### Sessions

Every run writes an append-only event log to `./.squad/sessions/<id>/`:

| File               | Contents                                               |
| ------------------ | ------------------------------------------------------ |
| `meta.json`        | Run options, last response id, cost, status            |
| `events.jsonl`     | One line per prompt, response, tool call, tool result  |
| `results/<id>.txt` | Full bytes of any tool result that exceeded 8 KiB      |

`--resume <id>` reopens that session and chains the next request to the
prior OpenAI Responses API id, so the model picks up server-side state
without re-sending the transcript. When a tool result is too large to
inline, the model sees a `[result:<id> — N bytes elided …]` placeholder
and can fetch the full bytes via the `get_tool_result` tool.

### Creating Your Own Agent

```bash
# Scaffold from a built-in template
squad init agent my-review --lang go

# Edit the prompts
$EDITOR agents/my-review/system.md

# Test it
squad run --agent my-review --print
```

Agent directory structure:

```
my-review/
├── agent.yaml      # Manifest (model preferences, tools, budget, pipeline)
├── system.md       # Agent identity, rules, and workflow
├── agent.md        # Execution wrapper
├── task.md         # Default task instructions
└── references/     # Optional knowledge-base documents
```

## Multi-Agent Pipelines

Define pipelines declaratively in `agent.yaml` using `stages` and `gates`:

```yaml
name: security-audit
stages:
  - name: scan
    agents: [go-review, dependency-check]  # run in parallel
  - name: report
    agent: summarize
    depends_on: [scan]
gates:
  - name: tests-pass
    command: go test ./...
    on_error: halt
```

```bash
squad run --agent security-audit "Audit all changes since last release"
```

Stages can also partition work automatically across files:

```yaml
stages:
  - name: review-files
    agent: go-review
    partition:
      by: glob
      glob: "**/*.go"
      max_per_partition: 10
```

## Scheduled Runs (Routines)

Routines enable unattended agent runs via OS-native schedulers (macOS launchd,
Linux systemd, Windows Task Scheduler).

```bash
# Create a nightly audit routine (installs daemon automatically)
squad routine create nightly-audit

# List all routines and their next fire time
squad routine list

# Fire a routine immediately to test it
squad routine run-now nightly-audit

# Tail the daemon log
squad routine logs --follow

# Check daemon health
squad routine doctor
```

Per-repo routine manifest (`.squad/routines/nightly-audit.yaml`):

```yaml
id: nightly-audit
agent: go-security-audit
schedule: "0 2 * * *"           # standard cron or @daily / @every 30m
prompt: "Audit changes merged in the last 24 hours"
provider: anthropic
model: claude-sonnet-4-6
max_cost: 5.00
max_iterations: 30
enabled: true
```

Checked into git, per-repo routines give the whole team the same automated
reviews. Global routines live in `~/.config/squad/routines/` for
personal cross-repo automations.

| Routine Command | Description |
|----------------|-------------|
| `routine create <id>` | Create and install a routine |
| `routine list` | List all routines with status |
| `routine show <id>` | Full details and next fire time |
| `routine run-now <id>` | Fire immediately |
| `routine enable/disable <id>` | Toggle a routine |
| `routine history <id>` | List past sessions |
| `routine logs [--follow]` | Tail daemon output |
| `routine doctor` | Health check |
| `routine repair` | Reinstall the OS service |

## Configuration

Config is loaded from (highest to lowest precedence):
CLI flags → `SQUAD_*` environment variables → config file → built-in defaults.

```bash
squad config init    # Create config at the default location
squad config show    # View the merged effective config
squad config path    # Show the active config file path
squad config set provider.default anthropic
squad config get model.default
```

**Default path:** `~/.config/squad/config.yaml`

### Config File Reference

```yaml
log:
  level: info              # debug | info | warn | error
  format: text             # text | json | color

provider:
  default: openai          # openai | anthropic | google | ollama | openai-compat
  token: $OPENAI_API_KEY   # supports $VAR and $(command) for dynamic resolution
  base_url: ""             # override provider endpoint (OpenAI-compatible APIs)
  organization: ""         # OpenAI organization ID
  api_version: ""          # for Azure OpenAI

model:
  default: gpt-4.1-mini
  temperature: 0.2
  max_tokens: 1024

agents:
  repositories:
    official: https://github.com/cowdogmoo/squad-agents.git
  local_paths: []          # additional local agent search directories

run:
  max_iterations: 100
  max_cost: 5.0            # USD
  stream: false
  apply: false
  require_actionable: true

otel:
  endpoint: ""             # OTLP endpoint; empty disables tracing
```

### Environment Variables

All config keys are available as `SQUAD_*` environment variables
(replace `.` with `_`, uppercase):

```bash
SQUAD_PROVIDER_DEFAULT=anthropic
SQUAD_PROVIDER_TOKEN=$ANTHROPIC_API_KEY
SQUAD_MODEL_DEFAULT=claude-sonnet-4-6
SQUAD_RUN_MAX_COST=10.0
SQUAD_LOG_LEVEL=debug
```

### OpenAI-Compatible Endpoints

```yaml
provider:
  default: openai-compat
  base_url: https://api.together.xyz/v1
  token: $TOGETHER_API_KEY
```

Works with NVIDIA NIM, Databricks, Together AI, vLLM, LM Studio, and any
OpenAI-compatible API.

## Execution Backends

Run agents in isolated or remote environments by adding an `environment` block
to `agent.yaml`:

### Docker

```yaml
environment:
  type: docker
  options:
    image: golang:1.24
    volumes: ".:/workspace"
    working_dir: /workspace
```

### Kubernetes

```yaml
environment:
  type: kubectl
  options:
    namespace: default
    image: golang:1.24
    resources:
      requests:
        memory: "512Mi"
```

### AWS SSM

```yaml
environment:
  type: ssm
  options:
    instance_id: i-1234567890abcdef0
    region: us-east-1
```

## Features

### Core Capabilities

| Feature | Description |
|---------|-------------|
| **Agent Execution** | Run agents with Read, Write, Edit, Glob, Grep, Bash tools |
| **Multi-provider** | OpenAI, Anthropic, Google AI, Ollama, any OpenAI-compatible endpoint |
| **Streaming Output** | Real-time token streaming to stderr |
| **Fix + Analyze Modes** | Agents can apply fixes or report only |
| **Agent Scaffolding** | `squad init agent` from templates or existing agents |
| **Agent Repositories** | Git-based agent sharing and discovery |

### Orchestration

| Feature | Description |
|---------|-------------|
| **Declarative Pipelines** | Multi-stage YAML pipelines with dependency ordering |
| **Parallel Execution** | Multiple agents in a stage run concurrently |
| **Regression Gates** | Shell commands validate state between stages |
| **Background Tasks** | Spawn child agents with `Task(background=true)` |
| **Cost Budgeting** | Per-run and per-stage USD caps with dry-run estimation |

### Infrastructure

| Feature | Description |
|---------|-------------|
| **Execution Backends** | Local, Docker, Kubernetes, AWS SSM |
| **MCP Integration** | Connect to any Model Context Protocol server |
| **OpenTelemetry** | OTLP tracing for agent runs and tool calls |
| **Agent Grading** | Grade outputs against quality rubrics |
| **XDG Configuration** | Standard config paths with env var overrides |
| **Routines** | OS-native daemon for scheduled unattended agent runs |

## Documentation

### Getting Started

- **[Agent and Skill Concepts](docs/agents-and-skills.md)** - Taxonomy guide: agents, skills, Task tool, and pipelines and when to reach for each
- **[Configuration](docs/configuration.md)** - Providers, environment variables, and config file reference
- **[Creating Agents](docs/creating-agents.md)** - Build your own agents from scratch or from templates
- **[Prompt Engineering Basics](docs/prompt-engineering-basics.md)** - LLM fundamentals, context windows, and how to write effective agent prompts

### Guides

- **[Pipelines](docs/pipelines.md)** - Multi-agent orchestration with stages, gates, and cost budgets
- **[Execution Backends](docs/execution-backends.md)** - Run agents in Docker, Kubernetes, or AWS SSM
- **[MCP Servers](docs/mcp-servers.md)** - Connect agents to external tools via Model Context Protocol
- **[Observability](docs/observability.md)** - Streaming output, OpenTelemetry tracing, cost budgeting, and grading
- **[Routines](docs/routines.md)** - Scheduled unattended runs via OS-native daemons
- **[Skills](docs/skills.md)** - On-demand capabilities a running agent loads — Anthropic Agent Skills format

### Agents

- **[Official Agents](https://github.com/CowDogMoo/squad-agents)** - Production-ready agents for Go, Python, Ansible, and Molecule
- **[Agent Quality Guide](docs/agent-quality.md)** - Tuning methodology and grading rubric

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes and add tests
4. Run the test suite: `go test ./...`
5. Commit your changes: `git commit -m 'Add my feature'`
6. Push to the branch: `git push origin feature/my-feature`
7. Open a Pull Request

### Development Setup

```bash
git clone https://github.com/cowdogmoo/squad.git
cd squad
go mod download
go build ./...
go test ./...
```

## Built With

- [LangChainGo](https://github.com/tmc/langchaingo)
- [Cobra](https://github.com/spf13/cobra)
- [Viper](https://github.com/spf13/viper)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- [go-git](https://github.com/go-git/go-git)

## License

[MIT License](./LICENSE)
