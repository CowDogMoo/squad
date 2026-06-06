package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// MultiEditOperation describes a single find-and-replace within a file.
type MultiEditOperation struct {
	Old        string   `json:"old"`
	New        string   `json:"new"`
	ReplaceAll FlexBool `json:"replace_all"`
}

// MultiEditArgs are the arguments for the MultiEdit tool.
type MultiEditArgs struct {
	Path  string               `json:"path"`
	Edits []MultiEditOperation `json:"edits"`
}

// FailedEdit records an edit operation that could not be applied.
type FailedEdit struct {
	Index int    `json:"index"`
	Old   string `json:"old"`
	Error string `json:"error"`
}

func definitionMultiEdit() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name: "MultiEdit",
			Description: "Apply multiple find-and-replace operations to a single file in one call. " +
				"More efficient than calling Edit repeatedly. Edits are applied sequentially " +
				"so later edits see the result of earlier ones. Supports partial success.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Path to the file."},
					"edits": map[string]any{
						"type":        "array",
						"description": "List of edit operations to apply in order.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"old":         map[string]any{"type": "string", "description": "Text to find."},
								"new":         map[string]any{"type": "string", "description": "Replacement text."},
								"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences (default: false)."},
							},
							"required": []string{"old", "new"},
						},
					},
				},
				"required": []string{"path", "edits"},
			},
		},
	}
}

func multiEditTool(workingDir string) func(ctx context.Context, rawArgs []byte) (string, error) {
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var payload MultiEditArgs
		if err := json.Unmarshal(rawArgs, &payload); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		if len(payload.Edits) == 0 {
			return "", fmt.Errorf("edits array is empty")
		}
		path, err := ResolvePath(workingDir, payload.Path)
		if err != nil {
			return "", err
		}
		if ft := GetFileTracker(ctx); ft != nil {
			if err := ft.ValidateBeforeEdit(path); err != nil {
				return "", err
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		content := string(data)

		var applied int
		var failed []FailedEdit

		for i, edit := range payload.Edits {
			if edit.Old == "" {
				failed = append(failed, FailedEdit{
					Index: i,
					Old:   edit.Old,
					Error: "old string is empty",
				})
				continue
			}
			if !strings.Contains(content, edit.Old) {
				failed = append(failed, FailedEdit{
					Index: i,
					Old:   edit.Old,
					Error: "text not found",
				})
				continue
			}
			if IsCommentsOnlyMode(ctx) {
				if err := ValidateCommentsOnly(edit.Old, edit.New); err != nil {
					failed = append(failed, FailedEdit{
						Index: i,
						Old:   edit.Old,
						Error: err.Error(),
					})
					continue
				}
			}
			if bool(edit.ReplaceAll) {
				content = strings.ReplaceAll(content, edit.Old, edit.New)
			} else {
				content = strings.Replace(content, edit.Old, edit.New, 1)
			}
			applied++
		}

		if applied == 0 {
			return formatMultiEditResult(payload.Path, 0, failed), fmt.Errorf("all %d edits failed", len(failed))
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}

		return formatMultiEditResult(payload.Path, applied, failed), nil
	}
}

func formatMultiEditResult(path string, applied int, failed []FailedEdit) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "updated %s: %d edit(s) applied", filepath.ToSlash(path), applied)
	if len(failed) > 0 {
		fmt.Fprintf(&sb, ", %d failed", len(failed))
		for _, f := range failed {
			fmt.Fprintf(&sb, "\n  edit[%d]: %s (old=%q)", f.Index, f.Error, TruncateString(f.Old, 80))
		}
	}
	return sb.String()
}
