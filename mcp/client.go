package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/telemetry"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcptypes "github.com/mark3labs/mcp-go/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// closeTimeout is the maximum time to wait for an MCP server subprocess to
// exit during Close(). MCP servers that maintain long-lived connections
// (WebSocket, HTTP keep-alive) often ignore stdin EOF and hang indefinitely.
const closeTimeout = 5 * time.Second

// Client wraps an MCP server connection with its configuration.
type Client struct {
	name      string
	inner     mcpclient.MCPClient
	tools     []mcptypes.Tool
	connected bool
}

// createTransport starts the appropriate MCP transport.
// Supports stdio (subprocess), sse (legacy HTTP Server-Sent Events), and
// streamable_http (current MCP HTTP spec used by hosted endpoints).
func createTransport(ctx context.Context, cfg ServerConfig) (mcpclient.MCPClient, error) {
	switch cfg.TransportType() {
	case "stdio":
		if cfg.Command == "" {
			return nil, fmt.Errorf("mcp server %q missing command for stdio transport", cfg.Name)
		}
		inner, err := mcpclient.NewStdioMCPClient(cfg.Command, cfg.Env, cfg.Args...)
		if err != nil {
			return nil, fmt.Errorf("failed to start MCP server %q (%s): %w", cfg.Name, cfg.Command, err)
		}
		return inner, nil
	case "sse":
		return createSSETransport(ctx, cfg)
	case "streamable_http", "http":
		return createStreamableHTTPTransport(ctx, cfg)
	default:
		return nil, fmt.Errorf("mcp server %q has unsupported transport %q (want stdio, sse, or streamable_http)", cfg.Name, cfg.Transport)
	}
}

// createSSETransport creates and starts an SSE transport connection.
func createSSETransport(ctx context.Context, cfg ServerConfig) (mcpclient.MCPClient, error) {
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
	sseClient, err := mcpclient.NewSSEMCPClient(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MCP server %q (%s): %w", cfg.Name, cfg.URL, err)
	}
	if startErr := sseClient.Start(ctx); startErr != nil {
		if cerr := sseClient.Close(); cerr != nil {
			logging.Warn("MCP server %q close after start failure: %v", cfg.Name, cerr)
		}
		return nil, fmt.Errorf("failed to start MCP server %q (%s): %w", cfg.Name, cfg.URL, startErr)
	}
	return sseClient, nil
}

// createStreamableHTTPTransport creates and starts a Streamable HTTP transport
// connection. This is the current MCP HTTP spec; use it for hosted MCP
// endpoints (Google Drive, Google Calendar, etc.).
func createStreamableHTTPTransport(ctx context.Context, cfg ServerConfig) (mcpclient.MCPClient, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("mcp server %q missing url for streamable_http transport", cfg.Name)
	}
	var opts []transport.StreamableHTTPCOption
	if len(cfg.Headers) > 0 {
		hdrs := make(map[string]string, len(cfg.Headers))
		for _, h := range cfg.Headers {
			if idx := strings.Index(h, "="); idx > 0 {
				hdrs[h[:idx]] = h[idx+1:]
			}
		}
		opts = append(opts, transport.WithHTTPHeaders(hdrs))
	}
	httpClient, err := mcpclient.NewStreamableHttpClient(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MCP server %q (%s): %w", cfg.Name, cfg.URL, err)
	}
	if startErr := httpClient.Start(ctx); startErr != nil {
		if cerr := httpClient.Close(); cerr != nil {
			logging.Warn("MCP server %q close after start failure: %v", cfg.Name, cerr)
		}
		return nil, fmt.Errorf("failed to start MCP server %q (%s): %w", cfg.Name, cfg.URL, startErr)
	}
	return httpClient, nil
}

// closeOnError closes an MCP client and logs any close error.
func closeOnError(inner mcpclient.MCPClient, name, context string) {
	if cerr := inner.Close(); cerr != nil {
		logging.Warn("MCP server %q close after %s failure: %v", name, context, cerr)
	}
}

// handshake performs the MCP protocol initialization and tool discovery on
// an already-connected transport. On failure it closes inner and returns an error.
func handshake(ctx context.Context, name string, inner mcpclient.MCPClient) (*Client, error) {
	initReq := mcptypes.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcptypes.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcptypes.Implementation{
		Name:    "squad",
		Version: "0.1.0",
	}

	if _, err := inner.Initialize(ctx, initReq); err != nil {
		closeOnError(inner, name, "init")
		return nil, fmt.Errorf("MCP server %q initialization failed: %w", name, err)
	}

	toolsResult, err := inner.ListTools(ctx, mcptypes.ListToolsRequest{})
	if err != nil {
		closeOnError(inner, name, "tools/list")
		return nil, fmt.Errorf("MCP server %q tools/list failed: %w", name, err)
	}

	return &Client{
		name:      name,
		inner:     inner,
		tools:     toolsResult.Tools,
		connected: true,
	}, nil
}

// Connect starts an MCP server connection and performs the protocol handshake.
// For stdio transport, it spawns a subprocess. For SSE transport, it connects
// to a running HTTP server. It then sends the initialize request and discovers
// available tools via tools/list.
func Connect(ctx context.Context, cfg ServerConfig) (*Client, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "mcp.connect",
		trace.WithAttributes(
			attribute.String("mcp.server.name", cfg.Name),
			attribute.String("mcp.transport", cfg.TransportType()),
		),
	)
	defer span.End()

	if cfg.Name == "" {
		return nil, fmt.Errorf("mcp server config missing name")
	}

	inner, err := createTransport(ctx, cfg)
	if err != nil {
		return nil, err
	}

	c, err := handshake(ctx, cfg.Name, inner)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.Int("mcp.tools.count", len(c.tools)))
	return c, nil
}

// Name returns the server's configured name.
func (c *Client) Name() string { return c.name }

// Tools returns the tools discovered from this server.
func (c *Client) Tools() []mcptypes.Tool { return c.tools }

// CallTool invokes a tool on the MCP server by its original (un-prefixed) name.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*mcptypes.CallToolResult, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "mcp.call_tool",
		trace.WithAttributes(
			attribute.String("mcp.server.name", c.name),
			attribute.String("mcp.tool.name", name),
		),
	)
	defer span.End()

	if c.inner == nil {
		err := fmt.Errorf("MCP client %q is not connected", c.name)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	req := mcptypes.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	result, err := c.inner.CallTool(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return result, err
}

// Close shuts down the MCP server subprocess and releases resources.
// It applies a timeout because mcp-go's Close() blocks on cmd.Wait()
// after closing stdin — MCP servers with long-lived connections (e.g.
// WebSocket, HTTP) often don't exit on stdin EOF.
func (c *Client) Close() error {
	if c.inner == nil {
		return nil
	}
	c.connected = false

	done := make(chan error, 1)
	go func() {
		done <- c.inner.Close()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(closeTimeout):
		return fmt.Errorf("MCP server %q did not shut down within %s", c.name, closeTimeout)
	}
}
