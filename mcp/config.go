// Package mcp provides MCP (Model Context Protocol) client integration
// for squad agents. It connects to MCP servers, discovers their tools,
// and bridges them into squad's tool handler system.
package mcp

import "fmt"

// ServerConfig defines how to connect to an MCP server.
// Agents declare MCP dependencies in agent.yaml under mcp_servers.
type ServerConfig struct {
	// Name is a short identifier used to namespace tools (e.g., "burpsuite").
	// Tools are registered as "mcp__<name>__<tool_name>".
	Name string `yaml:"name"`

	// Transport selects the protocol: "stdio" (default) or "sse".
	// Stdio spawns a subprocess and communicates over stdin/stdout.
	// SSE connects to a running HTTP server using Server-Sent Events.
	Transport string `yaml:"transport,omitempty"`

	// Command is the executable to spawn for stdio transport.
	Command string `yaml:"command,omitempty"`

	// Args are command-line arguments passed to the command.
	Args []string `yaml:"args,omitempty"`

	// Env are additional environment variables set for the subprocess.
	// Format: KEY=VALUE strings.
	Env []string `yaml:"env,omitempty"`

	// URL is the endpoint for SSE transport (e.g., "http://localhost:9876").
	URL string `yaml:"url,omitempty"`

	// Headers are additional HTTP headers for SSE transport.
	// Format: KEY=VALUE strings.
	Headers []string `yaml:"headers,omitempty"`
}

// TransportType returns the effective transport, defaulting to "stdio".
func (c ServerConfig) TransportType() string {
	if c.Transport != "" {
		return c.Transport
	}
	return "stdio"
}

// ConnectString returns a human-readable description of the connection
// target, e.g. "stdio: /usr/bin/tool [--flag]" or "sse: http://localhost:9876".
func (c ServerConfig) ConnectString() string {
	switch c.TransportType() {
	case "sse":
		return "sse: " + c.URL
	default:
		if len(c.Args) == 0 {
			return "stdio: " + c.Command
		}
		return "stdio: " + c.Command + " " + fmt.Sprint(c.Args)
	}
}
