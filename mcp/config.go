// Package mcp provides MCP (Model Context Protocol) client integration
// for squad agents. It connects to MCP servers, discovers their tools,
// and bridges them into squad's tool handler system.
package mcp

// ServerConfig defines how to connect to an MCP server.
// Agents declare MCP dependencies in agent.yaml under mcp_servers.
type ServerConfig struct {
	// Name is a short identifier used to namespace tools (e.g., "burpsuite").
	// Tools are registered as "mcp__<name>__<tool_name>".
	Name string `yaml:"name"`

	// Command is the executable to spawn for stdio transport.
	Command string `yaml:"command"`

	// Args are command-line arguments passed to the command.
	Args []string `yaml:"args,omitempty"`

	// Env are additional environment variables set for the subprocess.
	// Format: KEY=VALUE strings.
	Env []string `yaml:"env,omitempty"`
}
