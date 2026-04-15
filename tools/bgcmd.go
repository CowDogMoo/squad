package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/cowdogmoo/squad/csync"
	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

const (
	// DefaultAutoBackgroundTimeout is the duration after which a bash command
	// is automatically moved to the background.
	DefaultAutoBackgroundTimeout = 120 * time.Second
)

// BgCommand represents a running background command.
type BgCommand struct {
	ID      string
	Command string
	Output  string
	Err     error
	Done    chan struct{}
}

// BgCommandRegistry manages background shell commands.
type BgCommandRegistry struct {
	commands *csync.Map[string, *BgCommand]
	counter  uint64
}

// NewBgCommandRegistry creates a new background command registry.
func NewBgCommandRegistry() *BgCommandRegistry {
	return &BgCommandRegistry{
		commands: csync.NewMap[string, *BgCommand](),
	}
}

// Spawn starts a command in the background and returns its ID.
func (r *BgCommandRegistry) Spawn(ctx context.Context, ex executor.Executor, command string) string {
	id := fmt.Sprintf("cmd-%d", atomic.AddUint64(&r.counter, 1))
	bg := &BgCommand{
		ID:      id,
		Command: command,
		Done:    make(chan struct{}),
	}

	r.commands.Set(id, bg)

	go func() {
		defer close(bg.Done)
		output, err := ex.Execute(ctx, command)
		bg.Output = string(limitOutput(output))
		bg.Err = err
	}()

	return id
}

// Get returns a background command by ID.
func (r *BgCommandRegistry) Get(id string) (*BgCommand, bool) {
	return r.commands.Get(id)
}

// IsDone returns true if the command has finished.
func (bg *BgCommand) IsDone() bool {
	select {
	case <-bg.Done:
		return true
	default:
		return false
	}
}

// bgCommandRegistryKeyType is the context key for the BgCommandRegistry.
type bgCommandRegistryKeyType struct{}

// InitBgCommandRegistry attaches a registry to the context.
func InitBgCommandRegistry(ctx context.Context) context.Context {
	return context.WithValue(ctx, bgCommandRegistryKeyType{}, NewBgCommandRegistry())
}

// GetBgCommandRegistry retrieves the registry from context.
func GetBgCommandRegistry(ctx context.Context) *BgCommandRegistry {
	if r, ok := ctx.Value(bgCommandRegistryKeyType{}).(*BgCommandRegistry); ok {
		return r
	}
	return nil
}

func definitionBashBg() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "BashBackground",
			Description: "Run a shell command in the background. Returns a command ID immediately. Use BashOutput to check status and collect output. Useful for long-running commands like tests, builds, or servers.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Shell command to run in the background."},
				},
				"required": []string{"command"},
			},
		},
	}
}

func definitionBashOutput() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "BashOutput",
			Description: "Check on a background command started with BashBackground. Returns the current output and whether the command is still running.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command_id": map[string]any{"type": "string", "description": "The command ID returned by BashBackground."},
					"wait":       map[string]any{"type": "boolean", "description": "If true, block until the command completes (default: false)."},
				},
				"required": []string{"command_id"},
			},
		},
	}
}

func bashBackgroundTool(ex executor.Executor) func(ctx context.Context, rawArgs []byte) (string, error) {
	type args struct {
		Command string `json:"command"`
	}
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var payload args
		if err := json.Unmarshal(rawArgs, &payload); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		command := trimCommand(payload.Command)
		if command == "" {
			return "", fmt.Errorf("command is required")
		}
		if blocked, reason := IsBlockedCommand(command); blocked {
			return "", fmt.Errorf("%s", reason)
		}

		registry := GetBgCommandRegistry(ctx)
		if registry == nil {
			return "", fmt.Errorf("background commands not available")
		}

		id := registry.Spawn(ctx, ex, command)
		logging.InfoContext(ctx, "  → BashBackground %s → %s", TruncateString(command, 120), id)
		return fmt.Sprintf("Command started in background. ID: %s\nUse BashOutput(command_id=%q) to check status and collect output.", id, id), nil
	}
}

func bashOutputTool() func(ctx context.Context, rawArgs []byte) (string, error) {
	type args struct {
		CommandID string   `json:"command_id"`
		Wait      FlexBool `json:"wait"`
	}
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var payload args
		if err := json.Unmarshal(rawArgs, &payload); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		if payload.CommandID == "" {
			return "", fmt.Errorf("command_id is required")
		}

		registry := GetBgCommandRegistry(ctx)
		if registry == nil {
			return "", fmt.Errorf("background commands not available")
		}

		bg, ok := registry.Get(payload.CommandID)
		if !ok {
			return "", fmt.Errorf("unknown command ID: %s", payload.CommandID)
		}

		if bool(payload.Wait) {
			<-bg.Done
		}

		if bg.IsDone() {
			status := "completed"
			if bg.Err != nil {
				status = fmt.Sprintf("failed: %v", bg.Err)
			}
			return TruncateToolOutputHeadTail(
				fmt.Sprintf("[%s] status: %s\n\n%s", bg.ID, status, bg.Output),
				maxToolResultBytes,
			), nil
		}

		return fmt.Sprintf("[%s] status: running (command: %s)\nOutput so far not available — use wait=true to block until completion.",
			bg.ID, TruncateString(bg.Command, 80)), nil
	}
}

func trimCommand(s string) string {
	// inline trim to avoid import cycle issues
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
