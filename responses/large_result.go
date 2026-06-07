package responses

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/tools"
	"github.com/tmc/langchaingo/llms"
)

// toolGetToolResult is the tool name registered to fetch a previously spilled
// large tool result by id. Kept in one place so callers can compare against it.
const toolGetToolResult = "get_tool_result"

// largeResultMaxChunk caps a single get_tool_result reply so a re-fetch can't
// itself flood the context. The model can page with offset/limit.
const largeResultMaxChunk = 16 * 1024

// registerLargeResultTool adds the get_tool_result tool to the handler map and
// the tool definition list. The tool is a no-op when no session logger is
// attached to ctx (results were never spilled, so there's nothing to fetch).
func registerLargeResultTool(ctx context.Context, handlers map[string]tools.Handler, defs *[]llms.Tool) {
	if handlers == nil || defs == nil {
		return
	}
	if _, exists := handlers[toolGetToolResult]; exists {
		return
	}
	def := llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name: toolGetToolResult,
			Description: "Fetch a previously elided tool result by id. " +
				"When a tool returns more bytes than fit inline you'll see a " +
				"`[result:<id> — N bytes elided ...]` placeholder; pass that " +
				"id here to read the full content. Use offset/limit (bytes) " +
				"to page through very large results without re-flooding context.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"result_id": map[string]any{"type": "string", "description": "The id from the placeholder, e.g. 'a1b2c3d4'."},
					"offset":    map[string]any{"type": "integer", "description": "Byte offset to start at (0 = beginning)."},
					"limit":     map[string]any{"type": "integer", "description": "Max bytes to return (default 16384, max 16384)."},
				},
				"required": []string{"result_id"},
			},
		},
	}
	handlers[toolGetToolResult] = tools.Handler{Def: def, Call: getToolResult}
	*defs = append(*defs, def)
}

func getToolResult(ctx context.Context, rawArgs []byte) (string, error) {
	var args struct {
		ResultID string `json:"result_id"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.ResultID == "" {
		return "", fmt.Errorf("result_id is required")
	}
	logger := session.FromContext(ctx)
	if logger == nil {
		return "", fmt.Errorf("no session log attached; cannot fetch result %s", args.ResultID)
	}
	limit := args.Limit
	if limit <= 0 || limit > largeResultMaxChunk {
		limit = largeResultMaxChunk
	}
	chunk, total, err := logger.ReadLargeResult(args.ResultID, args.Offset, limit)
	if err != nil {
		return "", err
	}
	end := args.Offset + len(chunk)
	suffix := ""
	if end < total {
		suffix = fmt.Sprintf("\n\n[%d more bytes available; call get_tool_result(result_id=%q, offset=%d) to continue.]", total-end, args.ResultID, end)
	}
	return chunk + suffix, nil
}
