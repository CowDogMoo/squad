# MCP for Fire-and-Forget Agents

## Goal

Support scheduled, unattended squad agents whose only tools are remote
MCP servers — same shape as Anthropic's `trigger.json` scheduled tasks
(see `weekly-planner/trigger.json`: cron → prompt → Google Drive +
Calendar MCP → done).

Squad already has the pieces:

- Routines = scheduled execution. ✓
- MCP servers in manifest + tool registry injection. ✓
- Template substitution (`{{.Env}}`, `{{.Default}}`) for credentials. ✓

What's missing for `weekly-planner`-shape agents:

1. **Streamable HTTP transport** — Google's hosted MCP endpoints
   (`drivemcp.googleapis.com/mcp/v1`) speak the current spec, not
   legacy SSE. `mark3labs/mcp-go@v0.45.0` already implements it; we
   just don't wire it.
2. **Inline-prompt agents** — these agents are one file (a prompt + a
   list of MCP servers). Today, `agent.yaml` requires separate
   `entrypoint:` + `wrapper:` markdown files.
3. **No-working-dir mode** — these agents never touch a local
   filesystem. Today, squad always resolves `WorkingDir` and registers
   `Read/Write/Edit/Glob/Grep/Bash` whether the agent uses them or not.
4. **CLI tooling** — `squad mcp ls / probe / tools` to debug a server
   without doing a full agent run.

## Non-goals (deferred)

- Full interactive OAuth 2.0 flow with PKCE + token cache + browser
  redirect. Today the user can already wire credentials via
  `{{.Default "GCLOUD_TOKEN" "..."}}` template expansion or
  `$(gcloud auth print-access-token)` shell substitution at config
  load time. Native OAuth lands when that pattern starts hurting.
- Global `mcp_servers:` block in `~/.config/squad/config.yaml`. Useful
  but unrelated to the weekly-planner shape.
- Per-server tool allowlists / aliases.
- 1Password secret resolver.

## Design

### 1. Streamable HTTP transport

Add `transport: streamable_http` to `mcp.ServerConfig`. In
`mcp.createTransport`, route it through
`transport.NewStreamableHTTP(url, transport.WithHTTPHeaders(...))`
from `mark3labs/mcp-go`. Headers reuse the existing `headers:` field.

CLI flag spec gains a third form: `NAME:http:URL`.

### 2. Inline-prompt agents

Add `prompt:` to `agent.Manifest`. When set, the leaf-agent validator
allows `entrypoint:` and `wrapper:` to be empty and the bundle
assembler skips file loading, using the inline `prompt:` as the system
prompt verbatim.

```yaml
# agent.yaml
name: weekly-planner
version: 1
working_dir: none
models:
  - { provider: anthropic, model: claude-sonnet-4-6 }
prompt: |
  You are a weekly family planner assistant. ...
mcp_servers:
  - name: google-drive
    transport: streamable_http
    url: https://drivemcp.googleapis.com/mcp/v1
    headers:
      - "Authorization=Bearer {{.Default \"GCLOUD_TOKEN\" \"\"}}"
```

### 3. No-working-dir mode

Add `working_dir: none` (string) to `agent.Manifest`. When set:

- `prepareBundle` skips `resolveWorkingDir`; the agent's WorkDir is a
  per-run temp dir (so session logs still have a place to live).
- `tools.Register` skips registering Read/Write/Edit/Glob/Grep/Bash.
  Only MCP tools (plus Task) remain.
- `compactRepoSummary` is skipped (no repo to summarize).

### 4. `squad mcp` subcommand

- `squad mcp ls` — list servers declared in a given agent manifest
  (`--agent NAME`) or in `--mcp-server` flags.
- `squad mcp probe SPEC` — connect, run initialize, dump
  protocol version + server info.
- `squad mcp tools SPEC` — connect, list tools with their schemas.

`SPEC` accepts the same syntax as `--mcp-server`.

## Phasing

| Phase | Scope |
|---|---|
| 1 | streamable_http transport, inline `prompt:`, `working_dir: none`. |
| 2 | `squad mcp ls/probe/tools`. |
| 3 | (deferred) native OAuth, global config, tool filters. |

Phases 1 and 2 land in this branch together.
