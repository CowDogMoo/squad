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
| NVIDIA NIM | supported | `https://integrate.api.nvidia.com/v1` (default) | required | OpenAI-compatible; models at build.nvidia.com |
| Databricks AI Gateway | supported | `https://<id>.ai-gateway.cloud.databricks.com/mlflow/v1` | required | OpenAI-compatible; PAT (`dapi-` prefix) as Bearer token |

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

### NVIDIA NIM

```bash
# Using an NVIDIA API key from build.nvidia.com
squad run --agent go-review \
  --provider nvidia \
  --api-key $NVIDIA_API_KEY \
  --model meta/llama-3.1-8b-instruct

# Or via config / env vars
export SQUAD_PROVIDER_DEFAULT=nvidia
export SQUAD_PROVIDER_TOKEN=$NVIDIA_API_KEY
export SQUAD_MODEL_DEFAULT=meta/llama-3.1-8b-instruct
squad run --agent go-review
```

### Databricks AI Gateway

Databricks AI Gateway is an OpenAI-compatible proxy that routes requests to
foundation models hosted on Databricks. It is currently in **beta** (no charges
during beta; unavailable on GovCloud/Azure Government).

**Endpoint format:** The base URL uses the `ai-gateway` subdomain — not the
workspace URL. The path is always `/mlflow/v1` regardless of which model you
target; langchaingo appends `/chat/completions` automatically:

```
https://<id>.ai-gateway.cloud.databricks.com/mlflow/v1
```

**Authentication:** Send a Databricks PAT (`dapi-` prefix) or OAuth access token
via `--api-key` / `SQUAD_PROVIDER_TOKEN`. The token is forwarded as
`Authorization: Bearer <token>`. PATs can be scoped to specific API operations
for least-privilege access.

**Model field:** The `--model` value is the name of the deployed gateway endpoint
(e.g. `databricks-gpt-5-5-pro`), not a model family.

```bash
# CLI flags
squad run --agent go-review \
  --provider databricks \
  --base-url "https://<id>.ai-gateway.cloud.databricks.com/mlflow/v1" \
  --api-key "dapi-your-databricks-token" \
  --model databricks-gpt-5-5-pro

# Environment variables
export SQUAD_PROVIDER_DEFAULT=databricks
export SQUAD_PROVIDER_BASE_URL=https://<id>.ai-gateway.cloud.databricks.com/mlflow/v1
export SQUAD_PROVIDER_TOKEN=dapi-your-databricks-token
export SQUAD_MODEL_DEFAULT=databricks-gpt-5-5-pro
squad run --agent go-review
```

Config file with short-lived OAuth token via shell substitution:

```yaml
provider:
  default: databricks
  base_url: https://<id>.ai-gateway.cloud.databricks.com/mlflow/v1
  token: $(databricks auth token --host https://<workspace-url> 2>/dev/null | jq -r .access_token)
model:
  default: databricks-gpt-5-5-pro
```

Agent manifest override:

```yaml
models:
  - model: databricks-gpt-5-5-pro
    provider: databricks
```

Example endpoint names: `databricks-gpt-5-5-pro`, `databricks-claude-sonnet-4-5`,
`databricks-meta-llama-3-3-70b-instruct`.

**Rate limiting** is configured on the Databricks side per endpoint, per user,
and per group — not in squad.

**Claude Code integration:** Databricks AI Gateway natively supports Claude Code
as a coding agent, making it straightforward to route squad's LLM calls through
the gateway for enterprise governance.

**Known limitation:** The `usage_context` map (per-request cost attribution stored
as `request_tags` in `system.ai_gateway.usage`) requires `extra_body` support in
the OpenAI client, which langchaingo does not currently provide. This is a
candidate follow-on item.

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
