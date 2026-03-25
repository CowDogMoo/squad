package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cowdogmoo/squad/tools"
	mcptypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/tmc/langchaingo/llms"
)

// maxMCPToolResult caps MCP tool output to avoid blowing up context.
const maxMCPToolResult = 32 * 1024

// ToolPrefix is the namespace prefix for MCP tools: mcp__<server>__<tool>.
const ToolPrefix = "mcp__"

// PrefixedName returns the squad-namespaced tool name.
func PrefixedName(serverName, toolName string) string {
	return ToolPrefix + serverName + "__" + toolName
}

// BuildHandlers converts all tools from a set of MCP clients into
// squad tool handlers. Each tool is namespaced as mcp__<server>__<tool>.
func BuildHandlers(clients []*Client) []tools.Handler {
	var handlers []tools.Handler
	for _, c := range clients {
		for _, t := range c.Tools() {
			handlers = append(handlers, buildHandler(c, t))
		}
	}
	return handlers
}

// buildHandler creates a single squad Handler for one MCP tool.
func buildHandler(c *Client, t mcptypes.Tool) tools.Handler {
	prefixed := PrefixedName(c.Name(), t.Name)

	// Convert the MCP tool's input schema to the langchaingo parameter format.
	params := convertInputSchema(t)

	def := llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        prefixed,
			Description: t.Description,
			Parameters:  params,
		},
	}

	originalName := t.Name
	call := func(ctx context.Context, rawArgs []byte) (string, error) {
		var args map[string]any
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return "", fmt.Errorf("invalid MCP tool args: %w", err)
			}
		}

		result, err := c.CallTool(ctx, originalName, args)
		if err != nil {
			return "", fmt.Errorf("MCP tool %s failed: %w", prefixed, err)
		}

		output := formatCallResult(result)

		// Truncate large results to stay within context budget.
		if len(output) > maxMCPToolResult {
			output = output[:maxMCPToolResult] + "\n...output truncated"
		}

		if result.IsError {
			return "", fmt.Errorf("%s", output)
		}
		return output, nil
	}

	return tools.Handler{Def: def, Call: call}
}

// convertInputSchema converts an MCP tool's input schema to the
// map[string]any format expected by langchaingo's FunctionDefinition.Parameters.
func convertInputSchema(t mcptypes.Tool) map[string]any {
	// If there's a RawInputSchema, use it directly.
	if t.RawInputSchema != nil {
		var schema map[string]any
		if err := json.Unmarshal(t.RawInputSchema, &schema); err == nil {
			return schema
		}
	}

	// Build from the structured InputSchema.
	schema := map[string]any{
		"type": "object",
	}
	if t.InputSchema.Properties != nil {
		schema["properties"] = t.InputSchema.Properties
	}
	if len(t.InputSchema.Required) > 0 {
		schema["required"] = t.InputSchema.Required
	}
	if t.InputSchema.AdditionalProperties != nil {
		schema["additionalProperties"] = t.InputSchema.AdditionalProperties
	}
	return schema
}

// formatCallResult extracts text from a CallToolResult.
func formatCallResult(result *mcptypes.CallToolResult) string {
	if result == nil {
		return ""
	}

	var parts []string
	for _, content := range result.Content {
		switch c := content.(type) {
		case mcptypes.TextContent:
			parts = append(parts, c.Text)
		default:
			// For non-text content (images, audio, etc.), include a placeholder.
			data, err := json.Marshal(c)
			if err == nil {
				parts = append(parts, string(data))
			}
		}
	}
	return strings.Join(parts, "\n")
}
