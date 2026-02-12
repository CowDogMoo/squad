# squad

[![codecov](https://codecov.io/gh/CowDogMoo/squad/graph/badge.svg?token=O74GTQA4J7)](https://codecov.io/gh/CowDogMoo/squad)

Model-agnostic agent CLI built on LangChainGo.

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

## Providers

Providers are OpenAI-compatible by default. Configure with flags, environment
variables, or config file.

### Provider Matrix

| Provider | Status | Base URL | API Key | Notes |
| --- | --- | --- | --- | --- |
| openai | supported | `https://api.openai.com/v1` (default) | required | Supports `--api-type azure` for Azure OpenAI |
| ollama | supported | `http://localhost:11434/v1` (default) | optional | Uses `max_tokens` for compatibility |
| anthropic | planned | — | — | Not yet implemented |
| gemini | planned | — | — | Not yet implemented |

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

| Variable | Default | Description |
|----------|---------|-------------|
| `PROVIDER` | `ollama` | LLM provider |
| `MODEL` | `qwen3-coder:30b` | Model name |
| `API_KEY` | — | API key for the provider |
| `BASE_URL` | *(provider default)* | Custom API endpoint |
| `WORKING_DIR` | `.` | Target codebase directory |
| `COVERAGE_TARGET` | `75` | Target coverage % (test agents) |

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

| Variable | Description |
|----------|-------------|
| `SQUAD_PROVIDER_DEFAULT` | Default provider |
| `SQUAD_PROVIDER_TOKEN` | API key |
| `SQUAD_PROVIDER_BASE_URL` | Base URL override |
| `SQUAD_MODEL_DEFAULT` | Default model |
| `SQUAD_LOG_LEVEL` | Log level (debug, info, warn, error) |
| `SQUAD_LOG_FORMAT` | Log format (text, json, color) |

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
```
