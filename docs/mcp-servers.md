# MCP Servers

Agents can connect to [Model Context Protocol](https://modelcontextprotocol.io/)
servers for additional tool access: databases, APIs, monitoring systems, and
more. Squad discovers tools from each server on connect (via MCP's
`ListTools` handshake) and makes them available to the agent automatically.

Declare servers in `agent.yaml` for any server your agent depends on. Squad
loads those servers automatically on every run, so no flags are needed at the
command line. Use `--mcp-server` flags to attach additional servers for one-off
runs or experimentation without modifying the manifest. CLI flags are appended
to the manifest list, never replacing it.

At startup, Squad connects to every declared server in order. If any server
fails, the run is aborted immediately with an error. No tools from any server
are made available until all connections succeed.

## Adding Servers at Runtime

The `--mcp-server` flag can be repeated to attach multiple servers. CLI servers
are merged with any servers already declared in the agent's `agent.yaml`, so
they supplement rather than replace manifest declarations.

```bash
# Stdio transport: NAME:COMMAND[:ARG1,ARG2,...]
# Spawns a subprocess and communicates over stdin/stdout.
squad run --agent go-review \
  --mcp-server mytools:npx:@myorg/mcp-server

# Stdio with multiple args, separate with commas in the third segment
squad run --agent go-review \
  --mcp-server mytools:npx:-y,@myorg/mcp-server,--port,8080

# SSE transport: NAME:sse:FULL_URL
# Connects to a running HTTP server using Server-Sent Events.
squad run --agent go-review \
  --mcp-server grafana:sse:https://grafana.example.com/mcp

# Multiple servers in one command
squad run --agent go-review \
  --mcp-server db:npx:@myorg/db-mcp \
  --mcp-server grafana:sse:https://grafana.example.com/mcp
```

## Transports

Every `--mcp-server` value follows the format `NAME:TRANSPORT_OR_COMMAND[:ARGS]`.
The second segment determines the transport:

### Stdio (subprocess)

```
NAME:COMMAND[:ARG1,ARG2,...]
```

Squad spawns `COMMAND` as a subprocess and communicates with it over
stdin/stdout. `npx` in examples throughout this page is just the command being
run. It is not a keyword. Any executable works: `python`, `./my-binary`,
`node`, etc. The process is started and managed by Squad for the duration of
the run.

### SSE (HTTP Server-Sent Events)

```
NAME:sse:URL
```

`sse` is a **literal keyword** (case-insensitive) that switches to SSE mode.
Squad connects to an **already-running** HTTP server at `URL` using
[Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events).
Squad does not spawn the server. It must be running before `squad run` is
called.

### Comparison

| | Stdio | SSE |
|---|---|---|
| Second segment | Any executable (`npx`, `python`, `./bin`) | Literal keyword `sse` |
| Third segment | Comma-separated CLI args | Full HTTP URL |
| Server lifecycle | Squad spawns and manages it | Must already be running |
| Extra config (yaml) | `env:` (subprocess env vars) | `headers:` (HTTP headers) |

## Tool Naming

Tools from MCP servers are registered as `mcp__<name>__<tool>`, where `<name>`
is the server name you assigned. For example, a server named `grafana` that
exposes a `query_dashboard` tool is available to the agent as
`mcp__grafana__query_dashboard`.

## Agent Manifest

Declare MCP servers in `agent.yaml` so they're always available without
passing CLI flags on every invocation. The fields map directly to the two
transports described above.

```yaml
mcp_servers:
  - name: grafana
    transport: sse
    url: https://grafana.example.com/mcp
    headers: ["Authorization=Bearer {{.Env \"GRAFANA_TOKEN\"}}"]   # HTTP header for SSE auth
  - name: mytools
    command: npx
    args: ["@myorg/mcp-server"]
    env: ["API_KEY={{.Env \"MYTOOLS_API_KEY\"}}", "LOG_LEVEL=debug"]   # subprocess env vars
```

## Credentials

Most MCP servers require authentication. How you pass credentials depends on
the transport.

**SSE servers** authenticate via HTTP headers. Use the `headers` field with an
`Authorization` header:

```yaml
mcp_servers:
  - name: grafana
    transport: sse
    url: https://grafana.example.com/mcp
    headers: ["Authorization=Bearer {{.Env \"GRAFANA_TOKEN\"}}"]
```

**Stdio servers** receive credentials as environment variables passed to the
subprocess. Use the `env` field:

```yaml
mcp_servers:
  - name: mytools
    command: npx
    args: ["@myorg/mcp-server"]
    env: ["API_KEY={{.Env \"MYTOOLS_API_KEY\"}}"]
```

Never hardcode credentials directly in `agent.yaml`. The file typically lives
in source control, so hardcoded secrets will leak. Instead, use `{{.Env
"VAR_NAME"}}` to read the value from the environment at runtime, or use
`.Default` to provide a fallback for local development:

```yaml
headers: ['Authorization=Bearer {{.Default "GRAFANA_TOKEN" "dev-token-local"}}']
```

Set the real credential in your shell environment, CI secrets store, or a
`.env` file that is excluded from source control. Squad reads the variable at
the start of each run and injects it into the server configuration before
connecting.

## Template Variables

Any string field in an `mcp_servers` entry (`command`, `url`, `args`, `env`,
`headers`) supports Go `text/template` expressions. This is especially useful
for injecting secrets or environment-specific URLs without hardcoding them.

Use `.Default` to fall back to a hard-coded value when an environment variable
is absent:

```yaml
mcp_servers:
  - name: grafana
    transport: sse
    url: '{{.Default "MCP_SERVER_URL" "http://localhost:8080"}}'
    headers: ['Authorization=Bearer {{.Default "MCP_TOKEN" ""}}']
  - name: mytools
    command: '{{.Default "MCP_COMMAND" "npx"}}'
    args: ['{{.Default "MCP_PACKAGE" "@myorg/mcp-server"}}']
    env: ['API_KEY={{.Default "API_KEY" ""}}']
```

## Connection Failures

If any declared MCP server fails to connect (whether the subprocess can't be
spawned or the SSE endpoint is unreachable), Squad aborts the run immediately
with an error. There is no silent degradation or partial tool availability.
Check server names, commands, and URLs before running if you see connection
errors.
