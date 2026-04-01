# MCP Servers

Agents can connect to [Model Context Protocol](https://modelcontextprotocol.io/)
servers for additional tool access (databases, APIs, monitoring systems).

## CLI Flags

```bash
# Stdio transport: NAME:COMMAND[:ARG1,ARG2,...]
squad run --agent go-review \
  --mcp-server mytools:npx:@myorg/mcp-server

# SSE transport: NAME:sse:URL
squad run --agent go-review \
  --mcp-server grafana:sse:https://grafana.example.com/mcp
```

## Agent Manifest

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
