package mcp

import (
	"context"
	"encoding/json"
	"testing"

	mcptypes "github.com/mark3labs/mcp-go/mcp"
)

func TestPrefixedName(t *testing.T) {
	tests := []struct {
		server, tool, want string
	}{
		{"burpsuite", "get_proxy_http_history", "mcp__burpsuite__get_proxy_http_history"},
		{"chrome-devtools", "navigate_page", "mcp__chrome-devtools__navigate_page"},
		{"s", "t", "mcp__s__t"},
	}
	for _, tt := range tests {
		got := PrefixedName(tt.server, tt.tool)
		if got != tt.want {
			t.Errorf("PrefixedName(%q, %q) = %q, want %q", tt.server, tt.tool, got, tt.want)
		}
	}
}

func TestConvertInputSchema(t *testing.T) {
	tool := mcptypes.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: mcptypes.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"count":  map[string]any{"type": "integer", "description": "Number of items"},
				"filter": map[string]any{"type": "string", "description": "Regex filter"},
			},
			Required: []string{"count"},
		},
	}

	schema := convertInputSchema(tool)

	// Round-trip through JSON to verify the schema structure.
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to marshal schema: %v", err)
	}
	var parsed struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(schemaJSON, &parsed); err != nil {
		t.Fatalf("failed to unmarshal schema: %v", err)
	}
	if parsed.Type != "object" {
		t.Errorf("schema type = %v, want object", parsed.Type)
	}
	if _, ok := parsed.Properties["count"]; !ok {
		t.Error("schema missing 'count' property")
	}
	if _, ok := parsed.Properties["filter"]; !ok {
		t.Error("schema missing 'filter' property")
	}
	if len(parsed.Required) != 1 || parsed.Required[0] != "count" {
		t.Errorf("schema required = %v, want [count]", parsed.Required)
	}
}

func TestConvertInputSchemaRaw(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`)
	tool := mcptypes.Tool{
		Name:           "raw_tool",
		RawInputSchema: raw,
	}

	schema := convertInputSchema(tool)

	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", schema["type"])
	}
}

func TestFormatCallResult(t *testing.T) {
	result := &mcptypes.CallToolResult{
		Content: []mcptypes.Content{
			mcptypes.TextContent{Type: "text", Text: "line one"},
			mcptypes.TextContent{Type: "text", Text: "line two"},
		},
	}

	got := formatCallResult(result)
	if got != "line one\nline two" {
		t.Errorf("formatCallResult = %q, want %q", got, "line one\nline two")
	}
}

func TestFormatCallResultNil(t *testing.T) {
	got := formatCallResult(nil)
	if got != "" {
		t.Errorf("formatCallResult(nil) = %q, want empty", got)
	}
}

func TestBuildHandlersEmpty(t *testing.T) {
	handlers := BuildHandlers(nil)
	if len(handlers) != 0 {
		t.Errorf("BuildHandlers(nil) returned %d handlers, want 0", len(handlers))
	}
}

// mockClient creates a Client with pre-populated tools for testing handler building.
func mockClient(name string, tools []mcptypes.Tool) *Client {
	return &Client{
		name:  name,
		tools: tools,
	}
}

func TestBuildHandlersNamespacing(t *testing.T) {
	tools := []mcptypes.Tool{
		{
			Name:        "get_history",
			Description: "Get proxy history",
			InputSchema: mcptypes.ToolInputSchema{
				Type: "object",
			},
		},
		{
			Name:        "send_request",
			Description: "Send HTTP request",
			InputSchema: mcptypes.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"url": map[string]any{"type": "string"},
				},
				Required: []string{"url"},
			},
		},
	}

	c := mockClient("burpsuite", tools)
	handlers := BuildHandlers([]*Client{c})

	if len(handlers) != 2 {
		t.Fatalf("BuildHandlers returned %d handlers, want 2", len(handlers))
	}

	names := make(map[string]bool)
	for _, h := range handlers {
		names[h.Def.Function.Name] = true
	}

	if !names["mcp__burpsuite__get_history"] {
		t.Error("missing handler mcp__burpsuite__get_history")
	}
	if !names["mcp__burpsuite__send_request"] {
		t.Error("missing handler mcp__burpsuite__send_request")
	}
}

func TestBuildHandlersCallReturnsErrorOnNilClient(t *testing.T) {
	// The handler wraps c.CallTool which will fail since inner is nil.
	// This tests that the handler gracefully handles the error path.
	c := mockClient("test", []mcptypes.Tool{
		{
			Name:        "fail_tool",
			Description: "Will fail",
			InputSchema: mcptypes.ToolInputSchema{Type: "object"},
		},
	})
	handlers := BuildHandlers([]*Client{c})
	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(handlers))
	}

	_, err := handlers[0].Call(context.Background(), []byte(`{}`))
	if err == nil {
		t.Error("expected error from handler with nil inner client, got nil")
	}
}
