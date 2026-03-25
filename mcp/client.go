package mcp

import (
	"context"
	"fmt"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptypes "github.com/mark3labs/mcp-go/mcp"
)

// Client wraps an MCP server connection with its configuration.
type Client struct {
	name      string
	inner     mcpclient.MCPClient
	tools     []mcptypes.Tool
	connected bool
}

// Connect starts an MCP server subprocess and performs the protocol handshake.
// It spawns the command from cfg, sends the initialize request, and
// discovers available tools via tools/list.
func Connect(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("mcp server config missing name")
	}
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp server %q missing command", cfg.Name)
	}

	inner, err := mcpclient.NewStdioMCPClient(cfg.Command, cfg.Env, cfg.Args...)
	if err != nil {
		return nil, fmt.Errorf("failed to start MCP server %q (%s): %w", cfg.Name, cfg.Command, err)
	}

	c := &Client{
		name:  cfg.Name,
		inner: inner,
	}

	// Protocol handshake.
	initReq := mcptypes.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcptypes.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcptypes.Implementation{
		Name:    "squad",
		Version: "0.1.0",
	}

	if _, err := inner.Initialize(ctx, initReq); err != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("MCP server %q initialization failed: %w", cfg.Name, err)
	}

	// Discover tools.
	toolsResult, err := inner.ListTools(ctx, mcptypes.ListToolsRequest{})
	if err != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("MCP server %q tools/list failed: %w", cfg.Name, err)
	}

	c.tools = toolsResult.Tools
	c.connected = true
	return c, nil
}

// Name returns the server's configured name.
func (c *Client) Name() string { return c.name }

// Tools returns the tools discovered from this server.
func (c *Client) Tools() []mcptypes.Tool { return c.tools }

// CallTool invokes a tool on the MCP server by its original (un-prefixed) name.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*mcptypes.CallToolResult, error) {
	if c.inner == nil {
		return nil, fmt.Errorf("MCP client %q is not connected", c.name)
	}
	req := mcptypes.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return c.inner.CallTool(ctx, req)
}

// Close shuts down the MCP server subprocess and releases resources.
func (c *Client) Close() error {
	if c.inner == nil {
		return nil
	}
	c.connected = false
	return c.inner.Close()
}
