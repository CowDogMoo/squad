package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
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

func TestConvertInputSchemaAdditionalProperties(t *testing.T) {
	boolTrue := true
	tool := mcptypes.Tool{
		Name: "test_tool",
		InputSchema: mcptypes.ToolInputSchema{
			Type:                 "object",
			AdditionalProperties: &boolTrue,
		},
	}
	schema := convertInputSchema(tool)
	if schema["additionalProperties"] == nil {
		t.Error("expected additionalProperties in schema")
	}
}

func TestConvertInputSchemaInvalidRaw(t *testing.T) {
	// Invalid JSON in RawInputSchema should fall through to structured schema
	tool := mcptypes.Tool{
		Name:           "raw_invalid",
		RawInputSchema: json.RawMessage(`not json`),
		InputSchema: mcptypes.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"fallback": map[string]any{"type": "string"},
			},
		},
	}
	schema := convertInputSchema(tool)
	wantSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"fallback": map[string]any{"type": "string"},
		},
	}
	if !reflect.DeepEqual(schema, wantSchema) {
		t.Errorf("schema = %v, want %v", schema, wantSchema)
	}
}

func TestFormatCallResult(t *testing.T) {
	tests := []struct {
		name   string
		result *mcptypes.CallToolResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "",
		},
		{
			name:   "empty content",
			result: &mcptypes.CallToolResult{Content: []mcptypes.Content{}},
			want:   "",
		},
		{
			name: "multiple text parts",
			result: &mcptypes.CallToolResult{
				Content: []mcptypes.Content{
					mcptypes.TextContent{Type: "text", Text: "line one"},
					mcptypes.TextContent{Type: "text", Text: "line two"},
				},
			},
			want: "line one\nline two",
		},
		{
			name: "non-text content included",
			result: &mcptypes.CallToolResult{
				Content: []mcptypes.Content{
					mcptypes.TextContent{Type: "text", Text: "text part"},
					mcptypes.ImageContent{Type: "image", Data: "base64data", MIMEType: "image/png"},
				},
			},
			want: "base64data", // verify non-text content is serialized
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCallResult(tt.result)
			if tt.want == "" {
				if got != "" {
					t.Errorf("formatCallResult() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("formatCallResult() = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestBuildHandlersEmpty(t *testing.T) {
	handlers := BuildHandlers(nil)
	if len(handlers) != 0 {
		t.Errorf("BuildHandlers(nil) returned %d handlers, want 0", len(handlers))
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

	c := NewTestClient("burpsuite", tools)
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

func TestBuildHandlersMultipleClients(t *testing.T) {
	c1 := NewTestClient("server1", []mcptypes.Tool{
		{Name: "tool_a", InputSchema: mcptypes.ToolInputSchema{Type: "object"}},
	})
	c2 := NewTestClient("server2", []mcptypes.Tool{
		{Name: "tool_b", InputSchema: mcptypes.ToolInputSchema{Type: "object"}},
		{Name: "tool_c", InputSchema: mcptypes.ToolInputSchema{Type: "object"}},
	})

	handlers := BuildHandlers([]*Client{c1, c2})
	if len(handlers) != 3 {
		t.Fatalf("expected 3 handlers, got %d", len(handlers))
	}

	names := make(map[string]bool)
	for _, h := range handlers {
		names[h.Def.Function.Name] = true
	}
	for _, want := range []string{"mcp__server1__tool_a", "mcp__server2__tool_b", "mcp__server2__tool_c"} {
		if !names[want] {
			t.Errorf("missing handler %s", want)
		}
	}
}

func TestBuildHandlerDescription(t *testing.T) {
	c := NewTestClient("myserver", []mcptypes.Tool{
		{
			Name:        "my_tool",
			Description: "Does something useful",
			InputSchema: mcptypes.ToolInputSchema{Type: "object"},
		},
	})
	handlers := BuildHandlers([]*Client{c})
	if len(handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(handlers))
	}
	if handlers[0].Def.Function.Description != "Does something useful" {
		t.Errorf("description = %q, want 'Does something useful'", handlers[0].Def.Function.Description)
	}
	if handlers[0].Def.Type != "function" {
		t.Errorf("type = %q, want 'function'", handlers[0].Def.Type)
	}
}

func TestBuildHandlerCall(t *testing.T) {
	tests := []struct {
		name    string
		mock    *mockMCPClient
		args    []byte
		want    string
		wantErr string
	}{
		{
			name: "success",
			mock: &mockMCPClient{
				callResult: &mcptypes.CallToolResult{
					Content: []mcptypes.Content{
						mcptypes.TextContent{Type: "text", Text: "success output"},
					},
				},
			},
			args: []byte(`{"key":"value"}`),
			want: "success output",
		},
		{
			name: "error result",
			mock: &mockMCPClient{
				callResult: &mcptypes.CallToolResult{
					IsError: true,
					Content: []mcptypes.Content{
						mcptypes.TextContent{Type: "text", Text: "tool error message"},
					},
				},
			},
			wantErr: "tool error message",
		},
		{
			name:    "invalid JSON",
			args:    []byte(`not json`),
			wantErr: "invalid MCP tool args",
		},
		{
			name:    "nil inner client",
			wantErr: "MCP tool",
		},
		{
			name: "truncation",
			mock: &mockMCPClient{
				callResult: &mcptypes.CallToolResult{
					Content: []mcptypes.Content{
						mcptypes.TextContent{Type: "text", Text: strings.Repeat("x", maxMCPToolResult+100)},
					},
				},
			},
			want: "...output truncated",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c *Client
			if tt.mock != nil {
				c = NewTestClient("test", []mcptypes.Tool{
					{Name: "tool", InputSchema: mcptypes.ToolInputSchema{Type: "object"}},
				}, tt.mock)
			} else {
				c = NewTestClient("test", []mcptypes.Tool{
					{Name: "tool", InputSchema: mcptypes.ToolInputSchema{Type: "object"}},
				})
			}

			handlers := BuildHandlers([]*Client{c})
			if len(handlers) != 1 {
				t.Fatalf("expected 1 handler, got %d", len(handlers))
			}

			got, err := handlers[0].Call(context.Background(), tt.args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("output = %q, want to contain %q", got, tt.want)
			}
		})
	}
}
