package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

// taskDepthKey is the context key for tracking Task tool nesting depth.
type taskDepthKey struct{}

// MaxTaskDepth is the maximum nesting depth for Task tool invocations.
const MaxTaskDepth = 3

// WithTaskDepth returns a new context with the given task depth.
func WithTaskDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, taskDepthKey{}, depth)
}

// TaskDepth returns the current task depth from the context.
func TaskDepth(ctx context.Context) int {
	if v, ok := ctx.Value(taskDepthKey{}).(int); ok {
		return v
	}
	return 0
}

// CallModelFunc is the function signature for invoking a model run from
// within the Task tool. It mirrors the core model invocation path.
type CallModelFunc func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, error)

// TaskConfig holds the configuration needed to spawn child agent runs
// from within the Task tool.
type TaskConfig struct {
	AgentsDir     string
	WorkingDir    string
	MaxIterations int
	CallModel     CallModelFunc
}

type taskArgs struct {
	Agent      string `json:"agent"`
	Prompt     string `json:"prompt"`
	Mode       string `json:"mode,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
}

func definitionTask() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "Task",
			Description: "Spawn a child agent run. The child inherits the current context and tools but runs with its own iteration budget.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent":       map[string]any{"type": "string", "description": "Agent name to run (e.g. go-tests)."},
					"prompt":      map[string]any{"type": "string", "description": "Prompt/instructions for the child agent."},
					"mode":        map[string]any{"type": "string", "description": "Optional agent mode override (e.g. readonly)."},
					"working_dir": map[string]any{"type": "string", "description": "Optional working directory override for the child."},
				},
				"required": []string{"agent", "prompt"},
			},
		},
	}
}

func taskTool(cfg TaskConfig) func(ctx context.Context, rawArgs []byte) (string, error) {
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var args taskArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("invalid Task args: %w", err)
		}
		if args.Agent == "" {
			return "", fmt.Errorf("agent is required")
		}
		if args.Prompt == "" {
			return "", fmt.Errorf("prompt is required")
		}

		depth := TaskDepth(ctx)
		if depth >= MaxTaskDepth {
			return "", fmt.Errorf("maximum task depth (%d) exceeded", MaxTaskDepth)
		}

		workDir := cfg.WorkingDir
		if args.WorkingDir != "" {
			resolved, err := ResolvePath(cfg.WorkingDir, args.WorkingDir)
			if err != nil {
				return "", fmt.Errorf("invalid working_dir: %w", err)
			}
			workDir = resolved
		}

		childCtx := WithTaskDepth(ctx, depth+1)
		logging.InfoContext(ctx, "Task tool: spawning child agent=%s depth=%d", args.Agent, depth+1)

		response, err := cfg.CallModel(childCtx, cfg.AgentsDir, args.Agent, args.Prompt, workDir, args.Mode)
		if err != nil {
			return "", fmt.Errorf("child agent %q failed: %w", args.Agent, err)
		}

		// Cap output to 64KB to avoid blowing up context.
		if len(response) > maxToolOutput {
			response = response[:maxToolOutput] + "\n...output truncated"
		}

		logging.InfoContext(ctx, "Task tool: child agent=%s completed (%d bytes)", args.Agent, len(response))
		return response, nil
	}
}
