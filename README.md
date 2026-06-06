<div align="center">

<img src="docs/images/logos/squad-app-icon-transparent.png" alt="squad logo" width="320"/>

# `squad`

[![License](https://img.shields.io/github/license/CowDogMoo/squad?label=License&style=flat&color=blue&logo=github)](https://github.com/CowDogMoo/squad/blob/main/LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/CowDogMoo/squad?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/CowDogMoo/squad?label=Release&logo=github)](https://github.com/CowDogMoo/squad/releases)
[![codecov](https://codecov.io/github/CowDogMoo/squad/graph/badge.svg?token=O74GTQA4J7)](https://codecov.io/github/CowDogMoo/squad)
<br />
[![Tests](https://github.com/CowDogMoo/squad/actions/workflows/tests.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/tests.yaml)
[![Pre-Commit](https://github.com/CowDogMoo/squad/actions/workflows/pre-commit.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/pre-commit.yaml)
[![Semgrep](https://github.com/CowDogMoo/squad/actions/workflows/semgrep.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/semgrep.yaml)
[![Renovate](https://github.com/CowDogMoo/squad/actions/workflows/renovate.yaml/badge.svg)](https://github.com/CowDogMoo/squad/actions/workflows/renovate.yaml)

</div>

---

`squad` is a framework for building, sharing, and running unattended AI agents from the command line. Define an agent as plain markdown prompts and a YAML manifest, point it at any LLM provider, and let it work through a codebase using a fixed tool surface (Read, Write, Edit, Glob, Grep, Bash, plus any MCP server).

Agents run in a deterministic tool loop with per-run cost caps, structured event logs, and session resume. Multi-agent **pipelines** compose stages with dependency ordering and regression gates. **Routines** schedule unattended runs through OS-native daemons. Provider support includes OpenAI, Anthropic, Google AI, Ollama, and any OpenAI-compatible endpoint (NVIDIA NIM, Databricks, vLLM, LM Studio, Together AI). Execution can target the local machine, a Docker container, a Kubernetes pod, or an EC2 instance over AWS SSM.

## Table of Contents

- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [CLI Reference](#cli-reference)
- [Agents](#agents)
- [Multi-Agent Pipelines](#multi-agent-pipelines)
- [Routines](#routines)
- [Sessions & Resume](#sessions--resume)
- [Execution Backends](#execution-backends)
- [MCP Servers](#mcp-servers)
- [Skills](#skills)
- [Browser Profiles](#browser-profiles)
- [Configuration](#configuration)
- [Repository Layout](#repository-layout)
- [Development](#development)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [License](#license)
- [Security](#security)

## Architecture

`squad` is a single Go binary built around a deterministic tool-calling loop. The CLI assembles an **agent bundle** (system prompt + tool definitions + budget config) from on-disk markdown and YAML, then drives the model through tool calls until it returns a final response or a budget/iteration cap fires.

```text
                        ┌──────────────────────────────────────────────┐
                        │                squad (CLI)                   │
                        │   run · agents · routine · pipeline · ...    │
                        └──────────────┬───────────────────────────────┘
                                       │
                        ┌──────────────▼───────────────┐
                        │     Agent Bundle (agent/)    │
                        │  system.md + agent.md +      │
                        │  agent.yaml + skills + refs  │
                        └──────────────┬───────────────┘
                                       │
                        ┌──────────────▼───────────────┐
                        │   Tool Loop (tools/)         │
                        │ Read · Edit · Bash · Glob ·  │
                        │ Grep · Task · MCP · Skill    │
                        └──┬───────────┬────────────┬──┘
                           │           │            │
            ┌──────────────▼─┐  ┌──────▼──────┐ ┌───▼─────────────┐
            │  LLM Providers  │  │  Executor   │ │  MCP Servers    │
            │ openai          │  │ local ·     │ │ stdio · sse ·   │
            │ anthropic       │  │ docker ·    │ │ streamable_http │
            │ gemini · ollama │  │ kubectl ·   │ │                 │
            │ openai-compat   │  │ ssm         │ │                 │
            └─────────────────┘  └─────────────┘ └─────────────────┘
```

| Package      | Purpose                                                                    |
| ------------ | -------------------------------------------------------------------------- |
| `cmd/squad`  | Cobra CLI: subcommands for `run`, `agents`, `routine`, `pipeline`, etc.    |
| `agent`      | Manifest parsing, bundle assembly, model preferences, skill catalog        |
| `runner`     | Per-run context (edits, deadlines, modes), model invocation, response apply |
| `tools`      | Tool definitions and the iteration loop (Read/Write/Edit/Glob/Grep/Bash/…) |
| `pipeline`   | Multi-stage agent composition with parallel stages and regression gates    |
| `skill`      | On-demand Anthropic-format Skills: progressive-disclosure tool extensions  |
| `mcp`        | Model Context Protocol client + handler registration                       |
| `executor`   | Run shell commands locally, in Docker, on K8s, or over AWS SSM             |
| `routine`    | OS-native scheduled runs (launchd, systemd, Windows Task Scheduler)        |
| `responses`  | OpenAI Responses API path (server-side state, large-result offload)        |
| `metrics`    | Token + cost accounting, per-run budget enforcement                        |
| `grading`    | Rubric-based scoring of agent runs                                         |
| `ui`         | Bubble Tea TUI for interactive launch and monitoring                       |
| `source`     | Skill discovery from local paths and git-hosted agent repos                |
| `session`    | Append-only event log per run; powers `--resume`                           |

## Quick Start

### Prerequisites

| Requirement    | Version | Notes                                                                |
| -------------- | ------- | -------------------------------------------------------------------- |
| **Go**         | 1.26+   | Required for `go install`                                            |
| **LLM access** | -       | OpenAI, Anthropic, Google AI, Ollama, or any OpenAI-compatible endpoint |
| **Git**        | -       | Required for agent-repository fetching and worktree isolation         |

### Install

```bash
# Install latest release
go install github.com/cowdogmoo/squad/cmd/squad@latest

# Or build from source
git clone https://github.com/cowdogmoo/squad.git && cd squad
go build -o squad ./cmd/squad

# Verify
squad version
```

### Configure

```bash
# Initialize config at ~/.config/squad/config.yaml
squad config init

# Set a default provider and model
squad config set provider.default anthropic
squad config set model.default claude-sonnet-4-6

# Show the merged effective config
squad config show
```

API keys can come from environment (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GOOGLE_AI_API_KEY`) or from `$(command)` substitution in `config.yaml` for secret managers.

### First Run

```bash
# Pull the official agents repo
squad agents add official https://github.com/cowdogmoo/squad-agents.git
squad agents list

# Run an agent against the current directory
squad run --agent go-review --provider openai --model gpt-4.1-mini

# Run with local Ollama (no API key required)
squad run --agent go-review --provider ollama --model qwen2.5-coder:7b-instruct

# Estimate cost without calling the model
squad run --agent go-review --dry-run

# Resume a prior run after a crash, ctrl-c, or budget stop
squad run --agent go-review --resume 20260429T150220Z-a1b2c3d4
```

## CLI Reference

`squad` is a unified CLI with the following subcommands:

| Subcommand   | Purpose                                                                  |
| ------------ | ------------------------------------------------------------------------ |
| `run`        | Run a single agent or composed pipeline                                  |
| `agents`     | Add, remove, and list agent sources (local paths and git repos)          |
| `init`       | Scaffold a new agent, config, or routine from templates                  |
| `config`     | Initialize, inspect, and edit the squad config                           |
| `routine`    | Manage scheduled, unattended agent runs                                  |
| `skill`      | Inspect, validate, and manage Agent Skills                               |
| `mcp`        | Inspect and debug MCP servers                                            |
| `grade`      | Grade an agent run output against a rubric                               |
| `browser`    | Manage named browser profiles for agents that drive Chrome               |
| `ui`         | Interactive TUI for launching and monitoring runs                        |
| `completion` | Generate shell completion scripts                                        |
| `version`    | Print the active binary version                                          |

Run `squad <subcommand> --help` for full flag documentation.

### Global Flags

| Flag                | Description                                              | Default                          |
| ------------------- | -------------------------------------------------------- | -------------------------------- |
| `-c, --config`      | Config file path                                         | `~/.config/squad/config.yaml`    |
| `--log-level`       | `debug`, `info`, `warn`, `error`                         | `info`                           |
| `--log-format`      | `text`, `json`, `color`                                  | `text`                           |
| `--otel-endpoint`   | OpenTelemetry OTLP endpoint (e.g. `localhost:4318`)      | disabled                         |
| `-q, --quiet`       | Suppress non-error output                                | `false`                          |
| `-v, --verbose`     | Show debug output                                        | `false`                          |

### `squad run` — Key Flags

| Flag                  | Description                                                       | Default       |
| --------------------- | ----------------------------------------------------------------- | ------------- |
| `--agent`             | Agent name (required)                                             | —             |
| `--provider`          | LLM provider (`openai`, `anthropic`, `gemini`, `ollama`, …)       | config        |
| `--model`             | Model identifier                                                  | config        |
| `--working-dir`       | Target directory the agent operates on                            | current dir   |
| `--max-cost`          | USD budget cap (0 = unlimited)                                    | `5.00`        |
| `--max-iterations`    | Max tool-calling iterations (10-1000)                             | `100`         |
| `--mode`              | Override agent's default mode (e.g. `readonly`)                   | manifest      |
| `--apply`             | Apply unified diff from response to working directory             | `false`       |
| `--dry-run`           | Build bundle and exit without calling the model                   | `false`       |
| `--resume`            | Resume a prior session by ID                                      | —             |
| `--isolate`           | Isolation mode: `worktree`, `branch`, `commit`, `staged`, `none`  | manifest      |
| `--stream`            | Stream tokens to stderr in real time                              | `false`       |
| `--out`               | Write final response to file                                      | —             |
| `--var KEY=VALUE`     | Template variable (repeatable)                                    | —             |
| `--mcp-server`        | MCP server spec (repeatable)                                      | —             |
| `--allow-skill`       | Restrict skills to this allowlist (repeatable)                    | manifest      |
| `--auto-confirm`      | How `Confirm` tool resolves in non-TTY runs (`yes`/`no`/`abort`)  | `abort`       |

## Agents

An agent is a directory of plain files checked into git. The CLI loads it, assembles the bundle, and drives the tool loop.

```text
my-review/
├── agent.yaml      # Manifest: model preferences, tools, budget, pipeline
├── system.md       # Agent identity, rules, and workflow
├── agent.md        # Execution wrapper
├── task.md         # Default task instructions
└── references/     # Optional knowledge-base documents
```

### Scaffold

```bash
# Create a new agent from a built-in template
squad init agent my-review --lang go

# Edit the system prompt
$EDITOR agents/my-review/system.md

# Test it
squad run --agent my-review --print
```

### Sources

Agents can come from local directories or git-hosted repositories:

```bash
squad agents add official https://github.com/cowdogmoo/squad-agents.git
squad agents add team git@github.com:internal/agents.git
squad agents add scratch ./local-agents/

# Pin a source to a specific commit, tag, or branch so unattended runs
# always resolve the same content.
squad agents add official https://github.com/cowdogmoo/squad-agents.git --ref v0.4.2
squad agents pin official v0.5.0
squad agents pin official --unset             # back to tracking the default branch

squad agents list
squad agents update                            # pulls unpinned sources; skips pins
squad agents update --force                    # re-resolves pinned refs too
squad agents remove team
```

The [official agents repo](https://github.com/CowDogMoo/squad-agents) ships production-tuned agents for Go review, Python review, comment scrubbing, security audit, Ansible playbook validation, and more.

Full agent-authoring guide: [docs/creating-agents.md](docs/creating-agents.md). For the underlying taxonomy (agents, skills, the `Task` tool, and pipelines, and when to reach for each), see [docs/agents-and-skills.md](docs/agents-and-skills.md). First time writing prompts? Start with [docs/prompt-engineering-basics.md](docs/prompt-engineering-basics.md).

## Multi-Agent Pipelines

Define pipelines declaratively in `agent.yaml` with `stages` and `gates`:

```yaml
name: security-audit
stages:
  - name: scan
    agents: [go-review, dependency-check]   # run in parallel
  - name: report
    agent: summarize
    depends_on: [scan]
gates:
  - after: report
    command: go test ./...
    on_failure: stop   # revert | stop
```

```bash
squad run --agent security-audit "Audit all changes since last release"
```

Stages can partition work automatically across files:

```yaml
stages:
  - name: review-files
    agent: go-review
    partition:
      by: files
      glob: "**/*.go"
      max_per_partition: 10
```

Full pipeline reference: [docs/pipelines.md](docs/pipelines.md).

## Routines

Routines run agents unattended on a schedule via OS-native daemons (launchd on macOS, systemd on Linux, Task Scheduler on Windows).

```bash
# Create and install a nightly audit routine
squad routine create nightly-audit

# Per-repo manifest at .squad/routines/nightly-audit.yaml
cat > .squad/routines/nightly-audit.yaml <<'EOF'
id: nightly-audit
agent: go-security-audit
schedule: "0 2 * * *"           # cron, or @daily / @every 30m
prompt: "Audit changes merged in the last 24 hours"
provider: anthropic
model: claude-sonnet-4-6
max_cost: 5.00
max_iterations: 30
enabled: true
EOF

# Monitor
squad routine list
squad routine logs --follow
squad routine doctor
```

| Command                       | Description                              |
| ----------------------------- | ---------------------------------------- |
| `routine create <id>`         | Create and install a routine             |
| `routine list`                | List all routines with status            |
| `routine show <id>`           | Full details and next fire time          |
| `routine run-now <id>`        | Fire immediately                         |
| `routine enable/disable <id>` | Toggle a routine                         |
| `routine history <id>`        | List past sessions                       |
| `routine logs [--follow]`     | Tail daemon output                       |
| `routine doctor`              | Health check                             |
| `routine repair`              | Reinstall the OS service                 |

Per-repo routines (checked into git) give the whole team identical automation. Global routines live in `~/.config/squad/routines/`. Full reference: [docs/routines.md](docs/routines.md).

## Sessions & Resume

Every run writes an append-only event log to `./.squad/sessions/<id>/`:

| File               | Contents                                              |
| ------------------ | ----------------------------------------------------- |
| `meta.json`        | Run options, last response id, cost, status          |
| `events.jsonl`     | One line per prompt, response, tool call, tool result |
| `results/<id>.txt` | Full bytes of any tool result that exceeded 8 KiB    |

`--resume <id>` reopens that session and chains the next request to the prior OpenAI Responses API id, so the model picks up server-side state without re-sending the transcript. When a tool result is too large to inline, the model sees a `[result:<id> — N bytes elided …]` placeholder and can fetch the full bytes via the `get_tool_result` tool.

Streaming output, OpenTelemetry tracing, cost budgeting, and the grading rubric are documented in [docs/observability.md](docs/observability.md).

## Execution Backends

Run agents in isolated or remote environments by adding an `environment` block to `agent.yaml`.

### Local (default)

No `environment` block needed — the agent runs in the current shell.

### Docker

```yaml
environment:
  type: docker
  options:
    image: golang:1.26
    volumes: ".:/workspace"
    working_dir: /workspace
```

### Kubernetes

```yaml
environment:
  type: kubectl
  options:
    namespace: default
    image: golang:1.26
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

Full reference: [docs/execution-backends.md](docs/execution-backends.md).

## MCP Servers

Agents can call any [Model Context Protocol](https://modelcontextprotocol.io/) server as a tool source — stdio, SSE, or Streamable HTTP.

```yaml
# agent.yaml
mcp_servers:
  - name: postgres
    transport: stdio
    command: mcp-server-postgres
    args: ["$DATABASE_URL"]
  - name: linear
    transport: streamable_http
    url: https://mcp.linear.app/sse
```

Or pass at run time:

```bash
squad run --agent my-agent \
  --mcp-server "postgres:mcp-server-postgres:$DATABASE_URL" \
  --mcp-server "linear:http:https://mcp.linear.app/sse"

# Inspect tools a server exposes
squad mcp inspect postgres
```

Full reference: [docs/mcp-servers.md](docs/mcp-servers.md).

## Skills

Skills are single-directory capabilities a running agent loads on demand. They follow [Anthropic's open Agent Skills standard](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview) — the same format Claude Code, Codex CLI, and ChatGPT consume — so a skill checked into your repo runs everywhere without conversion.

```text
skills/
└── comment-scrub-playbook/
    ├── SKILL.md             # required: identity, allowed_tools, instructions
    └── references/          # optional: knowledge-base documents the skill cites
```

Squad surfaces discovered skills in the agent's system prompt as an "Available skills" catalog. The agent loads one with `Skill("name")`, the skill's body and references are injected into context, and `Read`/`Bash` access expands to include the skill's directory for the rest of the run.

```bash
# Inspect a skill's metadata
squad skill show comment-scrub-playbook

# Validate every skill in a directory
squad skill validate ./skills

# List skills the active config can discover
squad skill list
```

Per-agent control lives under `agent.yaml`'s `skills:` block (allow/deny lists, scope filters); CLI flags `--allow-skill`/`--deny-skill`/`--skills-disabled` override per run.

Full reference: [docs/skills.md](docs/skills.md).

## Browser Profiles

Agents that drive Chrome via [chrome-devtools-mcp](https://github.com/ChromeDevTools/chrome-devtools-mcp) reuse named, persistent profiles so cookies, logins, and session state survive across runs.

```bash
squad browser open amazon https://amazon.com   # create + open profile for interactive setup
squad browser list                              # show profiles + paths
squad browser path amazon                       # print the profile's userDataDir
```

In `agent.yaml`, refer to the profile path with the `BrowserProfile` template helper:

```yaml
mcp_servers:
  - name: chrome
    command: npx
    args: [chrome-devtools-mcp@latest, --userDataDir={{.BrowserProfile "amazon"}}]
```

Full reference: [docs/browser-profiles.md](docs/browser-profiles.md).

## Configuration

Config is loaded from (highest to lowest precedence):
**CLI flags → `SQUAD_*` env vars → config file → built-in defaults.**

Default path: `~/.config/squad/config.yaml`.

### Config File

```yaml
log:
  level: info               # debug | info | warn | error
  format: text              # text | json | color

provider:
  default: openai           # openai | anthropic | google | ollama | openai-compat
  token: $OPENAI_API_KEY    # supports $VAR and $(command) for dynamic resolution
  base_url: ""              # override provider endpoint (OpenAI-compatible APIs)
  organization: ""          # OpenAI organization ID
  api_version: ""           # for Azure OpenAI

model:
  default: gpt-4.1-mini
  temperature: 0.2
  max_tokens: 1024

agents:
  repositories:
    official: https://github.com/cowdogmoo/squad-agents.git
  local_paths: []           # additional local agent search directories

run:
  max_iterations: 100
  max_cost: 5.0             # USD
  stream: false
  apply: false
  require_actionable: true

otel:
  endpoint: ""              # OTLP endpoint; empty disables tracing
```

### Environment Variables

All config keys are available as `SQUAD_*` env vars (replace `.` with `_`, uppercase).

**LLM Providers** (at least one required):

| Variable             | Description                                       |
| -------------------- | ------------------------------------------------- |
| `OPENAI_API_KEY`     | OpenAI API key                                    |
| `ANTHROPIC_API_KEY`  | Anthropic API key                                 |
| `GOOGLE_AI_API_KEY`  | Google AI Studio key                              |
| `OLLAMA_BASE_URL`    | Local Ollama server URL (default `http://localhost:11434`) |

**Squad Defaults** (override config-file values):

| Variable                  | Description                                  |
| ------------------------- | -------------------------------------------- |
| `SQUAD_PROVIDER_DEFAULT`  | Default LLM provider                         |
| `SQUAD_PROVIDER_TOKEN`    | API key for the default provider             |
| `SQUAD_MODEL_DEFAULT`     | Default model identifier                     |
| `SQUAD_RUN_MAX_COST`      | Default USD budget cap                       |
| `SQUAD_RUN_MAX_ITERATIONS`| Default max tool-call iterations             |
| `SQUAD_LOG_LEVEL`         | Log level                                    |
| `SQUAD_LOG_FORMAT`        | Log format                                   |
| `SQUAD_OTEL_ENDPOINT`     | OTLP endpoint for telemetry                  |

### OpenAI-Compatible Endpoints

```yaml
provider:
  default: openai-compat
  base_url: https://api.together.xyz/v1
  token: $TOGETHER_API_KEY
```

Works with NVIDIA NIM, Databricks, Together AI, vLLM, LM Studio, and any OpenAI-compatible API.

Full reference: [docs/configuration.md](docs/configuration.md).

## Repository Layout

```text
cmd/squad/                # Cobra entry point
cmd/squad-routined/       # Routine daemon binary

agent/                    # Manifest parsing, bundle assembly
runner/                   # Run execution, model invocation
tools/                    # Tool definitions + the iteration loop
pipeline/                 # Multi-stage agent composition
skill/                    # Agent Skills (Anthropic format)
mcp/                      # Model Context Protocol client
executor/                 # Local/Docker/K8s/SSM execution backends
routine/                  # Scheduled unattended runs
responses/                # OpenAI Responses API path
metrics/                  # Token + cost accounting
grading/                  # Run grading and rubrics
ui/                       # Interactive TUI

config/                   # Config loading and validation
source/                   # Agent + skill discovery
session/                  # Per-run event log
templates/                # Built-in agent scaffolds
docs/                     # User documentation (linked below)
```

## Development

### Prerequisites

- Go 1.26+
- [pre-commit](https://pre-commit.com/) (recommended)
- [golangci-lint](https://golangci-lint.run/) (CI uses v2.11.4)

### Build & Test

```bash
git clone https://github.com/cowdogmoo/squad.git && cd squad

go mod download
go build ./cmd/squad      # build the binary
go test ./...              # run the full test suite
go vet ./...
golangci-lint run --timeout=5m
```

### Pre-Commit Hooks

Pre-commit hooks cover gofmt, goimports, gocyclo, golangci-lint, gocritic, go-build, go-mod-tidy, govulncheck, yamllint, codespell, markdownlint, actionlint, and the GoReleaser config check. Install once and they run on every commit:

```bash
pre-commit install
```

CI rejects any commit that fails the hooks.

## Documentation

### Getting Started

- **[Agent and Skill Concepts](docs/agents-and-skills.md)** — taxonomy: agents, skills, Task tool, pipelines, and when to reach for each
- **[Configuration](docs/configuration.md)** — providers, environment variables, full config-file reference
- **[Creating Agents](docs/creating-agents.md)** — build your own agents from scratch or from templates
- **[Prompt Engineering Basics](docs/prompt-engineering-basics.md)** — LLM fundamentals, context windows, and writing effective agent prompts

### Guides

- **[Pipelines](docs/pipelines.md)** — multi-agent orchestration with stages, gates, and cost budgets
- **[Execution Backends](docs/execution-backends.md)** — run agents in Docker, Kubernetes, or AWS SSM
- **[MCP Servers](docs/mcp-servers.md)** — connect agents to external tools via Model Context Protocol
- **[Observability](docs/observability.md)** — streaming output, OpenTelemetry tracing, cost budgeting, and grading
- **[Routines](docs/routines.md)** — scheduled unattended runs via OS-native daemons
- **[Skills](docs/skills.md)** — on-demand capabilities a running agent loads (Anthropic Agent Skills format)
- **[Browser Profiles](docs/browser-profiles.md)** — named Chrome profiles for agents that drive a browser

### Engineering

- **[Agent Quality Guide](docs/agent-quality.md)** — tuning methodology and grading rubric
- **[Agents Engineering Pipeline](docs/agents-engineering-pipeline-basics.md)** — agent-engineering CI/CD with squad

### Agents

- **[Official Agents](https://github.com/CowDogMoo/squad-agents)** — production-ready agents for Go, Python, Ansible, and Molecule

## Contributing

Contributions are welcome. The contributor flow:

1. Fork and create a feature branch off `main`.
2. Install pre-commit hooks: `pre-commit install`.
3. Make changes and add tests (`go test ./...`).
4. Ensure `golangci-lint run` and `go vet ./...` pass.
5. Open a PR. CI will reject commits that fail any hook.

For agent contributions, the [official agents repo](https://github.com/CowDogMoo/squad-agents) is the home — open PRs there.

Inspired by [Daniel Miessler's Fabric](https://github.com/danielmiessler/fabric).

### Built With

- [LangChainGo](https://github.com/tmc/langchaingo) — LLM provider abstraction
- [Cobra](https://github.com/spf13/cobra) — CLI framework
- [Viper](https://github.com/spf13/viper) — config loading
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI
- [go-git](https://github.com/go-git/go-git) — agent-repo fetching

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.

## Security

Please do not file public issues for security vulnerabilities. See [SECURITY.md](SECURITY.md) for the private reporting channel and the Semgrep + govulncheck pipeline. For operational guidance on running squad safely in production (cost caps, isolation, secrets, agent-yaml review), see [Running Squad Safely](docs/configuration.md#running-squad-safely) in the configuration docs.
