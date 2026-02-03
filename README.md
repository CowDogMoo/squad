# squad

[![codecov](https://codecov.io/gh/CowDogMoo/squad/graph/badge.svg?token=O74GTQA4J7)](https://codecov.io/gh/CowDogMoo/squad)

Model-agnostic agent CLI built on LangChainGo.

## Install

Build from source:

```bash
go build ./cmd/squad
```

## Quick start

Run an agent:

```bash
./squad run --agent go-cobra "add a new cobra command"
```

## Providers

Providers are OpenAI-compatible by default. Configure with flags, env vars, or config:

- base_url
- api_key
- model

### Provider matrix

| Provider | Status | API surface | Base URL | API key | Notes |
| --- | --- | --- | --- | --- | --- |
| openai | supported | OpenAI-compatible | `https://api.openai.com/v1` (default) | required | Supports `--api-type azure` + `--api-version` for Azure OpenAI. |
| ollama | supported | OpenAI-compatible | `http://localhost:11434/v1` (default) | optional | Uses `max_tokens` for compatibility; supply any non-empty key if required. |
| anthropic | planned | — | — | — | Not yet implemented in this CLI. |
| gemini | planned | — | — | — | Not yet implemented in this CLI. |

### OpenAI (default)

```bash
./squad run --agent go-cobra --provider openai --model gpt-4.1-mini
```

### Ollama (OpenAI-compatible)

Ollama is supported via the OpenAI-compatible API surface.

Local Ollama:

```bash
./squad run --agent go-cobra \
  --provider ollama \
  --base-url http://localhost:11434/v1 \
  --model qwen2.5-coder:7b-instruct
```

Kubernetes ingress example:

```bash
./squad run --agent go-cobra \
  --provider ollama \
  --base-url https://ollama.techvomit.xyz/v1 \
  --model qwen2.5-coder:7b-instruct
```

Notes:

- Ollama does not require an API key; if your client requires one, use any non-empty string (e.g., `--api-key ollama`).
- For OpenAI-compatible endpoints, `max_tokens` is used for Ollama; OpenAI defaults to `max_completion_tokens` unless you set `--openai-compat-max-tokens`.

## Configuration

Initialize a config file:

```bash
./squad config init
```

Set defaults:

```bash
./squad config set provider.default ollama
./squad config set provider.base_url https://ollama.techvomit.xyz/v1
./squad config set model.default qwen2.5-coder:7b-instruct
```
