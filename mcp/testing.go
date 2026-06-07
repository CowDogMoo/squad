package mcp

import (
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptypes "github.com/mark3labs/mcp-go/mcp"
)

// NewTestClient creates a Client for testing purposes.
// If inner is nil, only validation and cleanup paths work.
// If inner is provided, CallTool and Close will delegate to it.
func NewTestClient(name string, tools []mcptypes.Tool, inner ...mcpclient.MCPClient) *Client {
	c := &Client{
		name:  name,
		tools: tools,
	}
	if len(inner) > 0 {
		c.inner = inner[0]
		c.connected = true
	}
	return c
}
