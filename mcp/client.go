package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcptypes "github.com/mark3labs/mcp-go/mcp"
)

// Client wraps an MCP server connection with its configuration.
type Client struct {
	name      string
	inner     mcpclient.MCPClient
	tools     []mcptypes.Tool
	connected bool
}

// Connect starts an MCP server connection and performs the protocol handshake.
// For stdio transport, it spawns a subprocess. For SSE transport, it connects
// to a running HTTP server. It then sends the initialize request and discovers
// available tools via tools/list.
func Connect(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("mcp server config missing name")
	}

	var inner mcpclient.MCPClient
	var err error

	switch cfg.TransportType() {
	case "stdio":
		if cfg.Command == "" {
			return nil, fmt.Errorf("mcp server %q missing command for stdio transport", cfg.Name)
		}
		inner, err = mcpclient.NewStdioMCPClient(cfg.Command, cfg.Env, cfg.Args...)
		if err != nil {
			return nil, fmt.Errorf("failed to start MCP server %q (%s): %w", cfg.Name, cfg.Command, err)
		}
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("mcp server %q missing url for sse transport", cfg.Name)
		}
		var opts []transport.ClientOption
		if len(cfg.Headers) > 0 {
			hdrs := make(map[string]string, len(cfg.Headers))
			for _, h := range cfg.Headers {
				if idx := strings.Index(h, "="); idx > 0 {
					hdrs[h[:idx]] = h[idx+1:]
				}
			}
			opts = append(opts, transport.WithHeaders(hdrs))
		}
		sseClient, sseErr := mcpclient.NewSSEMCPClient(cfg.URL, opts...)
		if sseErr != nil {
			return nil, fmt.Errorf("failed to connect to MCP server %q (%s): %w", cfg.Name, cfg.URL, sseErr)
		}
		// SSE transport requires an explicit Start() call (stdio auto-starts).
		if startErr := sseClient.Start(ctx); startErr != nil {
			_ = sseClient.Close()
			return nil, fmt.Errorf("failed to start MCP server %q (%s): %w", cfg.Name, cfg.URL, startErr)
		}
		inner = sseClient
	default:
		return nil, fmt.Errorf("mcp server %q has unsupported transport %q (want stdio or sse)", cfg.Name, cfg.Transport)
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
