# Configuration

Configuration uses XDG paths with environment variable and CLI flag overrides.

## Precedence (highest to lowest)

1. CLI flags
2. Environment variables (`SQUAD_*`)
3. Configuration file
4. Built-in defaults

## Commands

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

## Environment Variables

| Variable                       | Description                          |
| ------------------------------ | ------------------------------------ |
| `SQUAD_PROVIDER_DEFAULT`       | Default provider                     |
| `SQUAD_PROVIDER_TOKEN`         | API key                              |
| `SQUAD_PROVIDER_BASE_URL`      | Base URL override                    |
| `SQUAD_MODEL_DEFAULT`          | Default model                        |
| `SQUAD_LOG_LEVEL`              | Log level (debug, info, warn, error) |
| `SQUAD_LOG_FORMAT`             | Log format (text, json, color)       |
| `SQUAD_OTEL_ENDPOINT`          | OpenTelemetry OTLP endpoint          |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Standard OTEL endpoint (also works)  |

## Example Config

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

## Providers

Providers are OpenAI-compatible by default. Configure with flags, environment
variables, or config file.

### Provider Matrix

| Provider   | Status    | Base URL                              | API Key  | Notes                                        |
| ---------- | --------- | ------------------------------------- | -------- | -------------------------------------------- |
| OpenAI     | supported | `https://api.openai.com/v1` (default) | required | Supports `--api-type azure` for Azure OpenAI |
| Anthropic  | supported | `https://api.anthropic.com/v1`        | required | Claude models via LangChainGo                |
| Google AI  | supported | Google AI endpoints                   | required | Gemini models via LangChainGo                |
| Ollama     | supported | `http://localhost:11434/v1` (default) | optional | Local models, uses `max_tokens` compat       |

### OpenAI

```bash
squad run --agent go-review --provider openai --model gpt-4.1-mini
```

### Anthropic

```bash
squad run --agent go-review --provider anthropic --model claude-sonnet-4-20250514
```

### Google AI

```bash
squad run --agent go-review --provider googleai --model gemini-2.5-flash
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
