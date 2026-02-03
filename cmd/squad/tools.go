package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

const maxToolIterations = 100
const maxToolOutput = 64 * 1024
const maxSameToolRepeat = 10

type toolHandler struct {
	def  llms.Tool
	call func(ctx context.Context, rawArgs []byte) (string, error)
}

// toolRepeatTracker detects when the model is stuck calling the same
// tool repeatedly (e.g. Bash in a loop).
type toolRepeatTracker struct {
	lastName string
	count    int
}

func (t *toolRepeatTracker) update(calls []llms.ToolCall) {
	name := ""
	if len(calls) == 1 && calls[0].FunctionCall != nil {
		name = calls[0].FunctionCall.Name
	}
	if name != "" && name == t.lastName {
		t.count++
	} else {
		t.count = 1
		t.lastName = name
	}
}

func (t *toolRepeatTracker) exceeded() bool {
	return t.count >= maxSameToolRepeat
}

func runWithTools(ctx context.Context, llm llms.Model, systemPrompt, userPrompt, workingDir string, callOpts ...llms.CallOption) (string, error) {
	handlers, toolDefs := buildToolHandlers(workingDir)
	callOpts = append(callOpts, llms.WithTools(toolDefs))

	messages := buildInitialMessages(systemPrompt, userPrompt)
	lastContent, messages, loopErr, done := toolLoop(ctx, llm, messages, handlers, callOpts)
	if done {
		return lastContent, nil
	}
	if loopErr != nil {
		return lastContent, loopErr
	}

	return finishToolLoop(ctx, llm, messages, lastContent, callOpts)
}

func toolLoop(ctx context.Context, llm llms.Model, messages []llms.MessageContent, handlers map[string]toolHandler, callOpts []llms.CallOption) (string, []llms.MessageContent, error, bool) {
	var lastContent string
	var repeat toolRepeatTracker
	for i := 0; i < maxToolIterations; i++ {
		logging.InfoContext(ctx, "model iteration %d/%d", i+1, maxToolIterations)
		iterStart := time.Now()
		response, err := llm.GenerateContent(ctx, messages, callOpts...)
		iterDuration := time.Since(iterStart)
		if err != nil {
			logging.InfoContext(ctx, "model call failed in %s: %v", iterDuration.Round(time.Millisecond), err)
			return lastContent, messages, fmt.Errorf("GenerateContent failed: %w", err), false
		}
		if response == nil || len(response.Choices) == 0 {
			logging.InfoContext(ctx, "model returned empty response in %s", iterDuration.Round(time.Millisecond))
			return lastContent, messages, fmt.Errorf("model returned empty response"), false
		}

		choice := response.Choices[0]
		if gi := choice.GenerationInfo; gi != nil {
			logging.DebugContext(ctx, "generation info: %v", gi)
		}
		if choice.Content != "" {
			lastContent = choice.Content
		}
		if len(choice.ToolCalls) == 0 {
			logging.InfoContext(ctx, "model returned final response in %s (no tool calls)", iterDuration.Round(time.Millisecond))
			return choice.Content, messages, nil, true
		}
		logging.DebugContext(ctx, "model responded in %s with %d tool call(s)", iterDuration.Round(time.Millisecond), len(choice.ToolCalls))

		repeat.update(choice.ToolCalls)
		if repeat.exceeded() {
			logging.InfoContext(ctx, "model called %s %d times in a row, breaking tool loop", repeat.lastName, repeat.count)
			break
		}

		messages = appendToolCallMessage(messages, choice.ToolCalls, ctx)
		messages = executeToolCalls(ctx, messages, choice.ToolCalls, handlers)
	}
	return lastContent, messages, nil, false
}

func finishToolLoop(ctx context.Context, llm llms.Model, messages []llms.MessageContent, lastContent string, callOpts []llms.CallOption) (string, error) {
	// Hit the iteration or repetition limit. Make one final call with
	// tool_choice="none" to force the model to produce a text summary.
	logging.InfoContext(ctx, "tool loop ended, requesting final response with tool_choice=none")
	finalOpts := make([]llms.CallOption, len(callOpts), len(callOpts)+1)
	copy(finalOpts, callOpts)
	finalOpts = append(finalOpts, llms.WithToolChoice("none"))

	response, err := llm.GenerateContent(ctx, messages, finalOpts...)
	if err == nil && response != nil && len(response.Choices) > 0 && response.Choices[0].Content != "" {
		logging.InfoContext(ctx, "final call produced response (%d bytes)", len(response.Choices[0].Content))
		return response.Choices[0].Content, nil
	}

	// Fall back to any text content accumulated during the loop.
	if lastContent != "" {
		logging.InfoContext(ctx, "returning last partial content (%d bytes)", len(lastContent))
		return lastContent, nil
	}

	return "", fmt.Errorf("tool loop ended after %d iterations with no usable response", maxToolIterations)
}

func buildInitialMessages(systemPrompt, userPrompt string) []llms.MessageContent {
	messages := []llms.MessageContent{}
	if systemPrompt != "" {
		messages = append(messages, llms.MessageContent{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextPart(systemPrompt)},
		})
	}
	messages = append(messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart(userPrompt)},
	})
	return messages
}

func appendToolCallMessage(messages []llms.MessageContent, toolCalls []llms.ToolCall, ctx context.Context) []llms.MessageContent {
	toolNames := make([]string, 0, len(toolCalls))
	toolCallParts := make([]llms.ContentPart, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		if toolCall.FunctionCall != nil && toolCall.FunctionCall.Name != "" {
			toolNames = append(toolNames, toolCall.FunctionCall.Name)
		}
		toolCallParts = append(toolCallParts, llms.ToolCall{
			ID:           toolCall.ID,
			Type:         toolCall.Type,
			FunctionCall: toolCall.FunctionCall,
		})
	}
	if len(toolNames) > 0 {
		logging.InfoContext(ctx, "tool calls requested: %s", strings.Join(toolNames, ", "))
	}
	return append(messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: toolCallParts,
	})
}

func executeToolCalls(ctx context.Context, messages []llms.MessageContent, toolCalls []llms.ToolCall, handlers map[string]toolHandler) []llms.MessageContent {
	for _, toolCall := range toolCalls {
		toolResponse := executeToolCall(ctx, toolCall, handlers)
		messages = append(messages, llms.MessageContent{
			Role:  llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{toolResponse},
		})
	}
	return messages
}

func executeToolCall(ctx context.Context, toolCall llms.ToolCall, handlers map[string]toolHandler) llms.ToolCallResponse {
	toolResponse := llms.ToolCallResponse{
		ToolCallID: toolCall.ID,
	}
	if toolCall.FunctionCall == nil {
		toolResponse.Content = "tool call missing function definition"
		return toolResponse
	}

	handler, ok := handlers[toolCall.FunctionCall.Name]
	if !ok {
		toolResponse.Content = fmt.Sprintf("unknown tool: %s", toolCall.FunctionCall.Name)
		logging.DebugContext(ctx, "unknown tool requested: %s", toolCall.FunctionCall.Name)
		return toolResponse
	}

	toolResponse.Name = toolCall.FunctionCall.Name
	logging.DebugContext(ctx, "tool %s args: %s", toolCall.FunctionCall.Name, truncateString(toolCall.FunctionCall.Arguments, 200))
	toolStart := time.Now()
	output, err := handler.call(ctx, []byte(toolCall.FunctionCall.Arguments))
	toolDuration := time.Since(toolStart)
	if err != nil {
		toolResponse.Content = fmt.Sprintf("error: %v", err)
		logging.DebugContext(ctx, "tool %s failed in %s: %v", toolCall.FunctionCall.Name, toolDuration.Round(time.Millisecond), err)
	} else {
		toolResponse.Content = output
		logging.DebugContext(ctx, "tool %s completed in %s (output-bytes=%d)", toolCall.FunctionCall.Name, toolDuration.Round(time.Millisecond), len(output))
	}
	return toolResponse
}

func buildToolHandlers(workingDir string) (map[string]toolHandler, []llms.Tool) {
	handlers := map[string]toolHandler{}

	add := func(handler toolHandler) {
		name := handler.def.Function.Name
		handlers[name] = handler
	}

	add(toolHandler{def: toolDefinitionRead(), call: readTool(workingDir)})
	add(toolHandler{def: toolDefinitionWrite(), call: writeTool(workingDir)})
	add(toolHandler{def: toolDefinitionEdit(), call: editTool(workingDir)})
	add(toolHandler{def: toolDefinitionGlob(), call: globTool(workingDir)})
	add(toolHandler{def: toolDefinitionGrep(), call: grepTool(workingDir)})
	add(toolHandler{def: toolDefinitionBash(), call: bashTool(workingDir)})

	toolDefs := make([]llms.Tool, 0, len(handlers))
	for _, handler := range handlers {
		toolDefs = append(toolDefs, handler.def)
	}
	sort.Slice(toolDefs, func(i, j int) bool {
		return toolDefs[i].Function.Name < toolDefs[j].Function.Name
	})

	return handlers, toolDefs
}

func toolDefinitionRead() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "Read",
			Description: "Read a text file and return its contents.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Path to the file."},
				},
				"required": []string{"path"},
			},
		},
	}
}

func toolDefinitionWrite() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "Write",
			Description: "Write content to a file, creating directories as needed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Path to the file."},
					"content": map[string]any{"type": "string", "description": "File contents."},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

func toolDefinitionEdit() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "Edit",
			Description: "Replace text in a file.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "Path to the file."},
					"old":         map[string]any{"type": "string", "description": "Text to replace."},
					"new":         map[string]any{"type": "string", "description": "Replacement text."},
					"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences."},
				},
				"required": []string{"path", "old", "new"},
			},
		},
	}
}

func toolDefinitionGlob() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "Glob",
			Description: "Find files matching a glob pattern (supports **).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Glob pattern (e.g. **/*.go)."},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

func toolDefinitionGrep() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "Grep",
			Description: "Search for a regex pattern in files under a path.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Regex pattern to search for."},
					"path":    map[string]any{"type": "string", "description": "File or directory path. Default: ."},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

func toolDefinitionBash() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "Bash",
			Description: "Run a shell command in the working directory.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Shell command to run."},
				},
				"required": []string{"command"},
			},
		},
	}
}

func readTool(workingDir string) func(ctx context.Context, rawArgs []byte) (string, error) {
	type args struct {
		Path string `json:"path"`
	}
	return func(_ context.Context, rawArgs []byte) (string, error) {
		var payload args
		if err := json.Unmarshal(rawArgs, &payload); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		path, err := resolvePath(workingDir, payload.Path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

func writeTool(workingDir string) func(ctx context.Context, rawArgs []byte) (string, error) {
	type args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	return func(_ context.Context, rawArgs []byte) (string, error) {
		var payload args
		if err := json.Unmarshal(rawArgs, &payload); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		path, err := resolvePath(workingDir, payload.Path)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(payload.Content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %s (%d bytes)", filepath.ToSlash(payload.Path), len(payload.Content)), nil
	}
}

func editTool(workingDir string) func(ctx context.Context, rawArgs []byte) (string, error) {
	type args struct {
		Path       string   `json:"path"`
		Old        string   `json:"old"`
		New        string   `json:"new"`
		ReplaceAll flexBool `json:"replace_all"`
	}
	return func(_ context.Context, rawArgs []byte) (string, error) {
		var payload args
		if err := json.Unmarshal(rawArgs, &payload); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		path, err := resolvePath(workingDir, payload.Path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		content := string(data)
		if !strings.Contains(content, payload.Old) {
			return "", fmt.Errorf("text not found in %s", payload.Path)
		}
		var updated string
		replaced := 1
		if bool(payload.ReplaceAll) {
			replaced = strings.Count(content, payload.Old)
			updated = strings.ReplaceAll(content, payload.Old, payload.New)
		} else {
			updated = strings.Replace(content, payload.Old, payload.New, 1)
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("updated %s (%d replacement(s))", filepath.ToSlash(payload.Path), replaced), nil
	}
}

func globTool(workingDir string) func(ctx context.Context, rawArgs []byte) (string, error) {
	type args struct {
		Pattern string `json:"pattern"`
	}
	return func(_ context.Context, rawArgs []byte) (string, error) {
		var payload args
		if err := json.Unmarshal(rawArgs, &payload); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		pattern := strings.TrimSpace(payload.Pattern)
		if pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}
		matcher, err := newGlobMatcher(pattern)
		if err != nil {
			return "", err
		}
		var matches []string
		err = filepath.WalkDir(workingDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(workingDir, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if matcher.Match(rel) {
				matches = append(matches, rel)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		sort.Strings(matches)
		if len(matches) == 0 {
			return "no matches", nil
		}
		return strings.Join(matches, "\n"), nil
	}
}

func grepTool(workingDir string) func(ctx context.Context, rawArgs []byte) (string, error) {
	type args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	return func(_ context.Context, rawArgs []byte) (string, error) {
		var payload args
		if err := json.Unmarshal(rawArgs, &payload); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		pattern := strings.TrimSpace(payload.Pattern)
		if pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("invalid regex: %w", err)
		}
		searchPath := payload.Path
		if strings.TrimSpace(searchPath) == "" {
			searchPath = "."
		}
		resolved, err := resolvePath(workingDir, searchPath)
		if err != nil {
			return "", err
		}

		matches, err := grepSearchPath(workingDir, resolved, re)
		if err != nil {
			return "", err
		}

		if len(matches) == 0 {
			return "no matches", nil
		}
		return strings.Join(matches, "\n"), nil
	}
}

func grepSearchPath(workingDir, resolved string, re *regexp.Regexp) ([]string, error) {
	var matches []string
	visit := grepVisitFile(workingDir, re, &matches)

	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		if err := filepath.Walk(resolved, visit); err != nil {
			return nil, err
		}
	} else {
		if err := visit(resolved, info, nil); err != nil {
			return nil, err
		}
	}
	return matches, nil
}

func grepVisitFile(workingDir string, re *regexp.Regexp, matches *[]string) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		rel, err := filepath.Rel(workingDir, path)
		if err != nil {
			rel = path
		}
		scanner := bufio.NewScanner(file)
		lineNum := 1
		for scanner.Scan() {
			line := scanner.Text()
			if re.MatchString(line) {
				*matches = append(*matches, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(rel), lineNum, line))
			}
			lineNum++
		}
		return nil
	}
}

func bashTool(workingDir string) func(ctx context.Context, rawArgs []byte) (string, error) {
	type args struct {
		Command string `json:"command"`
	}
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var payload args
		if err := json.Unmarshal(rawArgs, &payload); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		command := strings.TrimSpace(payload.Command)
		if command == "" {
			return "", fmt.Errorf("command is required")
		}
		cmd := exec.CommandContext(ctx, "bash", "-lc", command)
		cmd.Dir = workingDir
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			output := limitOutput(buf.Bytes())
			return string(output), fmt.Errorf("command failed: %w", err)
		}
		return string(limitOutput(buf.Bytes())), nil
	}
}

// flexBool unmarshals both JSON booleans and string representations
// ("true"/"false") that LLMs sometimes produce for boolean fields.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	// Try bool first.
	var v bool
	if err := json.Unmarshal(data, &v); err == nil {
		*b = flexBool(v)
		return nil
	}
	// Fall back to string.
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("replace_all must be bool or string, got %s", string(data))
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		*b = true
	default:
		*b = false
	}
	return nil
}

func resolvePath(workingDir, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("path is required")
	}
	var joined string
	if filepath.IsAbs(input) {
		joined = filepath.Clean(input)
	} else {
		joined = filepath.Join(workingDir, input)
	}
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	wd, err := filepath.Abs(workingDir)
	if err != nil {
		return "", err
	}
	if abs != wd && !strings.HasPrefix(abs, wd+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside working directory", input)
	}
	return abs, nil
}

func limitOutput(data []byte) []byte {
	if len(data) <= maxToolOutput {
		return data
	}
	head := data[:maxToolOutput]
	return append(head, []byte("\n...output truncated\n")...)
}

type globMatcher struct {
	re *regexp.Regexp
}

func newGlobMatcher(pattern string) (*globMatcher, error) {
	normalized := filepath.ToSlash(pattern)
	regex, err := globToRegex(normalized)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(regex)
	if err != nil {
		return nil, err
	}
	return &globMatcher{re: re}, nil
}

func (g *globMatcher) Match(path string) bool {
	return g.re.MatchString(path)
}

func globToRegex(pattern string) (string, error) {
	var buf strings.Builder
	buf.WriteString("^")
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if ch == '*' {
			if i+1 < len(runes) && runes[i+1] == '*' {
				buf.WriteString(".*")
				i++
			} else {
				buf.WriteString(`[^/]*`)
			}
			continue
		}
		if ch == '?' {
			buf.WriteString(".")
			continue
		}
		if strings.ContainsRune(`.+()|[]{}^$\\`, ch) {
			buf.WriteString(`\`)
		}
		buf.WriteRune(ch)
	}
	buf.WriteString("$")
	return buf.String(), nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
