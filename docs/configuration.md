# Configuration

Configuration uses XDG paths with environment variable and CLI flag overrides.

## Precedence (highest to lowest)

1. CLI flags
2. Environment variables (`SQUAD_*`)
3. Configuration file
4. Built-in defaults

## File Locations

Squad searches for `config.yaml` in the following order (first match wins):

1. `--config <path>` flag — explicit path, highest priority
2. `$XDG_CONFIG_HOME/squad/config.yaml` — typically `~/.config/squad/config.yaml`
3. `~/.squad/config.yaml` — legacy fallback
4. Linux/BSD only: `$XDG_CONFIG_DIRS/squad/config.yaml` — typically `/etc/xdg/squad/config.yaml`
5. `./config.yaml` — current working directory

Run `squad config path` to see which file is active.

## Commands

```bash
# Initialize config file at the default XDG location
squad config init

# Show current configuration (all sources merged)
squad config show

# Show the path of the active config file
squad config path

# Set a value
squad config set provider.default openai
squad config set model.default gpt-4.1-mini

# Get a value
squad config get provider.default
```

## Token Resolution

The `provider.token` field (and only this field) supports dynamic value
resolution, so you never have to store secrets in plaintext YAML.

| Syntax | Behavior |
| -------------------------------- | --------------------------------------- |
| `$VAR` or `${VAR}` | Replaced by the environment variable |
| `$(command args)` | Replaced by the command's stdout output |
| `$$` | Literal `$` character |

Commands run via `sh -c` with a 10-second timeout. An unset variable or a
failed command is a hard error — squad will not start with an empty token.

```yaml
provider:
  # Read from environment variable
  token: $OPENAI_API_KEY

  # Read from a secrets manager at startup
  token: $(aws secretsmanager get-secret-value --secret-id my-key | jq -r .SecretString)

  # Short-lived OAuth token for Databricks
  token: $(databricks auth token --host https://<workspace-url> 2>/dev/null | jq -r .access_token)
```

## Configuration Reference

All keys can be set in the config file, overridden by the matching `SQUAD_*`
environment variable (replace `.` → `_`, uppercase), and further overridden by
the corresponding CLI flag.

### log

| Key | Type | Default | Description |
| ------------ | ------ | ------- | --------------------------------------------- |
| `log.level` | string | `info` | Minimum log level: `debug`, `info`, `warn`, `error` |
| `log.format` | string | `text` | Output format: `text`, `json`, `color` |

### provider

| Key | Type | Default | Description |
| ------------------------------------ | ------ | ------- | --------------------------------------------------------- |
| `provider.default` | string | `openai` | Provider name when none is specified on the CLI or in an agent manifest |
| `provider.token` | string | `""` | API key or bearer token; supports `$VAR` and `$(cmd)` resolution |
| `provider.base_url` | string | `""` | Override the provider's default API endpoint URL |
| `provider.organization` | string | `""` | OpenAI organization ID |
| `provider.api_version` | string | `""` | API version for Azure OpenAI and other versioned APIs |
| `provider.api_type` | string | `""` | API variant: `openai` or `azure` |
| `provider.openai_compat_max_tokens` | bool | `false` | Enforce `max_tokens` in requests for OpenAI-compatible endpoints that require it |
| `provider.num_ctx` | int | `32768` | Context window size for Ollama models |

### model

| Key | Type | Default | Description |
| ---------------------------- | ------- | ----------- | -------------------------------------------------------- |
| `model.default` | string | `""` | Model identifier — **no built-in default; must be set** |
| `model.temperature` | float | `0.2` | Sampling randomness: `0.0` = deterministic, `1.0` = creative |
| `model.max_tokens` | int | `1024` | Output token budget per request |
| `model.reasoning_prefixes` | []string | `["gpt-5"]` | Model name prefixes that receive extended reasoning token budgets |

### agents

| Key | Type | Default | Description |
| ------------------------- | ----------- | ------------------------------- | ------------------------------------------------ |
| `agents.cache_dir` | string | `~/.cache/squad/agents` | Directory where cloned agent git repos are cached |
| `agents.repositories` | map | `{official: <squad-agents>}` | Named git URLs to fetch agents from |
| `agents.local_paths` | []string | `[]` | Local directories searched for agents |

### otel

| Key | Type | Default | Description |
| --------------- | ------ | ------- | ------------------------------------------------------ |
| `otel.endpoint` | string | `""` | OTLP/HTTP endpoint (e.g., `localhost:4318`); empty disables telemetry |

### run

These options can be persisted in the config file under the `run:` key,
overridden by `SQUAD_RUN_*` environment variables, and further overridden
by the corresponding `squad run` flags.

> `run.dry_run` and `run.apply` are mutually exclusive.

| Key | Type | Default | Description |
| ----------------------------- | ------- | ------- | --------------------------------------------------------- |
| `run.agent` | string | `""` | Agent name to run |
| `run.agents_dir` | string | `""` | Agents directory override |
| `run.working_dir` | string | `""` | Working directory (defaults to current directory) |
| `run.system` | string | `""` | System prompt override |
| `run.out` | string | `""` | Write response to a file |
| `run.print` | bool | `true` | Print response to stdout |
| `run.bundle_out` | string | `""` | Write agent bundle to a file |
| `run.print_bundle` | bool | `false` | Print agent bundle to stdout |
| `run.dry_run` | bool | `false` | Build bundle without calling the model |
| `run.require_actionable` | bool | `true` | Require actionable output (diff/files/no changes) |
| `run.apply` | bool | `false` | Apply unified diff from response to the working directory |
| `run.apply_fallback` | bool | `false` | Fallback to `patch(1)` if `git apply` fails (may create `.rej`/`.orig` files) |
| `run.mode` | string | `""` | Agent mode override (e.g., `readonly`) |
| `run.max_iterations` | int | `100` | Max tool-calling iterations, clamped to 10–1000 |
| `run.max_cost` | float | `5.0` | Max cost budget in USD; `0` = unlimited |
| `run.stream` | bool | `false` | Stream model output tokens to stderr as they arrive |
| `run.max_concurrent_tasks` | int | `0` (→4) | Max concurrent background child tasks; `0` uses default of 4 |
| `run.resume` | string | `""` | Resume a prior session by ID (see `./.squad/sessions/`) |

## Environment Variables

All `SQUAD_*` variables follow the rule: config key with `.` replaced by `_`,
uppercased, prefixed with `SQUAD_`.

| Variable | Config Key | Description |
| ----------------------------------------------- | ----------------------------------- | --------------------------------------- |
| `SQUAD_LOG_LEVEL` | `log.level` | Log level |
| `SQUAD_LOG_FORMAT` | `log.format` | Log format |
| `SQUAD_PROVIDER_DEFAULT` | `provider.default` | Default provider |
| `SQUAD_PROVIDER_TOKEN` | `provider.token` | API key or bearer token |
| `SQUAD_PROVIDER_BASE_URL` | `provider.base_url` | Base URL override |
| `SQUAD_PROVIDER_ORGANIZATION` | `provider.organization` | OpenAI organization ID |
| `SQUAD_PROVIDER_API_VERSION` | `provider.api_version` | API version |
| `SQUAD_PROVIDER_API_TYPE` | `provider.api_type` | API type (`openai` or `azure`) |
| `SQUAD_PROVIDER_OPENAI_COMPAT_MAX_TOKENS` | `provider.openai_compat_max_tokens` | Enforce `max_tokens` for compat endpoints |
| `SQUAD_PROVIDER_NUM_CTX` | `provider.num_ctx` | Ollama context window size |
| `SQUAD_MODEL_DEFAULT` | `model.default` | Default model |
| `SQUAD_MODEL_TEMPERATURE` | `model.temperature` | Sampling temperature |
| `SQUAD_MODEL_MAX_TOKENS` | `model.max_tokens` | Output token budget |
| `SQUAD_MODEL_REASONING_PREFIXES` | `model.reasoning_prefixes` | Reasoning model name prefixes |
| `SQUAD_AGENTS_CACHE_DIR` | `agents.cache_dir` | Agent repo cache directory |
| `SQUAD_AGENTS_LOCAL_PATHS` | `agents.local_paths` | Local agent search paths |
| `SQUAD_OTEL_ENDPOINT` | `otel.endpoint` | OTLP endpoint |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `otel.endpoint` | Standard OTEL endpoint (alias) |
| `SQUAD_RUN_MAX_ITERATIONS` | `run.max_iterations` | Max tool-calling iterations |
| `SQUAD_RUN_MAX_COST` | `run.max_cost` | Max cost budget in USD |
| `SQUAD_RUN_STREAM` | `run.stream` | Stream output tokens |
| `SQUAD_RUN_REQUIRE_ACTIONABLE` | `run.require_actionable` | Require actionable output |
| `SQUAD_RUN_APPLY` | `run.apply` | Apply diff to working directory |
| `SQUAD_RUN_DRY_RUN` | `run.dry_run` | Build bundle without calling the model |
| `SQUAD_RUN_MAX_CONCURRENT_TASKS` | `run.max_concurrent_tasks` | Concurrent background task limit |

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
    private: git@github.com:myorg/private-agents.git
  local_paths:
    - ~/dev/my-agents

otel:
  endpoint: localhost:4318
```

Persisting common `run` options:

```yaml
run:
  max_iterations: 50
  max_cost: 2.0
  stream: true
  require_actionable: false
```

## Providers

Providers are OpenAI-compatible by default. Configure with flags, environment
variables, or config file.

### Provider Matrix

| Provider | Status | Base URL | API Key | Notes |
| ---------------------- | --------- | --------------------------------------------- | -------- | -------------------------------------------- |
| OpenAI | supported | `https://api.openai.com/v1` (default) | required | Supports `--api-type azure` for Azure OpenAI |
| OpenAI Responses | supported | `https://api.openai.com/v1` (default) | required | Uses the Responses API instead of Chat Completions |
| Anthropic | supported | `https://api.anthropic.com/v1` | required | Claude models via LangChainGo |
| Google AI | supported | Google AI endpoints | required | Gemini models via LangChainGo |
| Ollama | supported | `http://localhost:11434/v1` (default) | optional | Local models; use `--num-ctx` for context size |
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
squad run --agent go-review --provider gemini --model gemini-2.5-flash
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

Use `--num-ctx` (or `SQUAD_PROVIDER_NUM_CTX`) to control the context window size.
The default is 32768 tokens. Increase it for agents that process large files.

```bash
# Local Ollama
squad run --agent go-review \
  --provider ollama \
  --base-url http://localhost:11434/v1 \
  --model qwen2.5-coder:7b-instruct

# Local Ollama with larger context window
squad run --agent go-review \
  --provider ollama \
  --base-url http://localhost:11434/v1 \
  --model qwen2.5-coder:7b-instruct \
  --num-ctx 65536

# Remote Ollama (Kubernetes ingress)
squad run --agent go-review \
  --provider ollama \
  --base-url https://ollama.example.com/v1 \
  --model qwen2.5-coder:7b-instruct
```
