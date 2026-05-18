package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/cowdogmoo/squad/csync"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/telemetry"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// taskDepthKey is the context key for tracking Task tool nesting depth.
type taskDepthKey struct{}

// MaxTaskDepth is the maximum nesting depth for Task tool invocations.
const MaxTaskDepth = 3

// DefaultConcurrentTasks is the default concurrent background task limit.
const DefaultConcurrentTasks = 4

// BackgroundTaskResult holds the result of a background task.
type BackgroundTaskResult struct {
	Output  string
	Metrics *metrics.Metrics
	Err     error
	Done    chan struct{}
}

// BackgroundTaskRegistry manages background task spawning and results.
type BackgroundTaskRegistry struct {
	tasks     *csync.Map[string, *BackgroundTaskResult]
	counter   uint64
	semaphore chan struct{}
}

// NewBackgroundTaskRegistry creates a new registry with the given concurrency limit.
// If concurrency is <= 0, DefaultConcurrentTasks is used.
func NewBackgroundTaskRegistry(concurrency int) *BackgroundTaskRegistry {
	if concurrency <= 0 {
		concurrency = DefaultConcurrentTasks
	}
	return &BackgroundTaskRegistry{
		tasks:     csync.NewMap[string, *BackgroundTaskResult](),
		semaphore: make(chan struct{}, concurrency),
	}
}

// SpawnTask runs a task in the background and returns its ID immediately.
func (r *BackgroundTaskRegistry) SpawnTask(ctx context.Context, cfg TaskConfig, args taskArgs, workDir string) string {
	id := fmt.Sprintf("bg-%d", atomic.AddUint64(&r.counter, 1))
	result := &BackgroundTaskResult{
		Done: make(chan struct{}),
	}

	r.tasks.Set(id, result)

	// Capture the parent span context for linking.
	parentSpanCtx := trace.SpanContextFromContext(ctx)

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				result.Err = fmt.Errorf("task panicked: %v", rec)
			}
			close(result.Done)
		}()

		// Acquire semaphore slot
		select {
		case r.semaphore <- struct{}{}:
			defer func() { <-r.semaphore }()
		case <-ctx.Done():
			result.Err = ctx.Err()
			return
		}

		depth := TaskDepth(ctx)
		childCtx := WithTaskDepth(ctx, depth+1)

		// Create a linked span for the background task (not a child span,
		// because the goroutine outlives the parent).
		var linkOpts []trace.SpanStartOption
		linkOpts = append(linkOpts,
			trace.WithAttributes(
				attribute.String("squad.task.id", id),
				attribute.String("squad.task.agent", args.Agent),
				attribute.Int("squad.task.depth", depth+1),
			),
			trace.WithNewRoot(),
		)
		if parentSpanCtx.IsValid() {
			linkOpts = append(linkOpts, trace.WithLinks(trace.Link{SpanContext: parentSpanCtx}))
		}
		var taskSpan trace.Span
		childCtx, taskSpan = telemetry.Tracer().Start(childCtx, "task.background", linkOpts...)
		defer func() {
			if result.Err != nil {
				taskSpan.RecordError(result.Err)
				taskSpan.SetStatus(codes.Error, result.Err.Error())
			}
			taskSpan.End()
		}()
		// Give child agents a prefixed logger so their output is distinguishable.
		parentLogger := logging.FromContext(ctx)
		childLogger := parentLogger.WithPrefix(fmt.Sprintf("[%s] ", id))
		childCtx = logging.WithLogger(childCtx, childLogger)

		budgetInfo := ""
		if cfg.ParentMetrics != nil && cfg.MaxCost > 0 {
			remaining := cfg.ParentMetrics.RemainingBudget()
			budgetInfo = fmt.Sprintf(" budget=$%.4f remaining", remaining)
		}
		logging.InfoContext(ctx, "Task tool: spawning background child agent=%s id=%s depth=%d%s", args.Agent, id, depth+1, budgetInfo)

		response, childMetrics, err := cfg.CallModel(childCtx, cfg.AgentsDir, args.Agent, args.Prompt, workDir, args.Mode)
		result.Output = response
		result.Metrics = childMetrics
		result.Err = err

		if len(result.Output) > maxToolOutput {
			result.Output = result.Output[:maxToolOutput] + "\n...output truncated"
		}

		metricsInfo := ""
		if childMetrics != nil {
			metricsInfo = fmt.Sprintf(" tokens=%d", childMetrics.TotalTokens())
		}
		logging.InfoContext(ctx, "Task tool: background child agent=%s id=%s completed (%d bytes%s)", args.Agent, id, len(result.Output), metricsInfo)
	}()

	return id
}

// GetResult returns the result for a task ID, blocking until complete if wait is true.
func (r *BackgroundTaskRegistry) GetResult(taskID string, wait bool) (*BackgroundTaskResult, bool) {
	result, ok := r.tasks.Get(taskID)
	if !ok {
		return nil, false
	}

	if wait {
		<-result.Done
	}

	return result, true
}

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
type CallModelFunc func(ctx context.Context, agentsDir, agentName, prompt, workingDir, mode string) (string, *metrics.Metrics, error)

// TaskConfig holds the configuration needed to spawn child agent runs
// from within the Task tool.
// ChildMaxCostFunc returns the dedicated cost cap for the named child agent.
// Returns 0 if no dedicated cap is configured (use remaining budget).
type ChildMaxCostFunc func(agentName string) float64

// ChildMaxIterFunc returns the iteration cap for the named child agent.
// Returns 0 if no dedicated cap is configured (inherit parent's cap).
type ChildMaxIterFunc func(agentName string) int

// TaskConfig holds the configuration needed to spawn child agent runs
// from within the Task tool. It is created once per parent run and shared
// across all Task tool invocations in that run.
type TaskConfig struct {
	AgentsDir     string
	WorkingDir    string
	MaxIterations int
	MaxCost       float64 // original budget ceiling from parent
	CallModel     CallModelFunc
	Registry      *BackgroundTaskRegistry
	ParentMetrics *metrics.Metrics // parent metrics for cost aggregation
	Findings      *FindingsStore   // shared findings store for ReportFinding tool
	AgentName     string           // current agent name (for finding attribution)
	ExtraTools    []Handler        // additional tools injected by MCP servers or other providers
	ChildMaxCost  ChildMaxCostFunc // per-child budget lookup (nil = use remaining budget)
	ChildMaxIter  ChildMaxIterFunc // per-child iteration cap (nil = inherit parent's cap)
	// RemoteOnly suppresses registration of local-filesystem tools
	// (Read/Write/Edit/MultiEdit/Glob/Grep/Bash/RepoMap/SystemInfo) for
	// agents whose entire toolset is remote MCP tools.
	RemoteOnly bool
}

type taskArgs struct {
	Agent      string `json:"agent"`
	Prompt     string `json:"prompt"`
	Mode       string `json:"mode,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
	Background bool   `json:"background,omitempty"`
}

func definitionTask() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "Task",
			Description: "Spawn a child agent run. The child inherits the current context and tools but runs with its own iteration budget. Use background=true for parallel execution, then call TaskResult to collect output.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent":       map[string]any{"type": "string", "description": "Agent name to run (e.g. go-tests)."},
					"prompt":      map[string]any{"type": "string", "description": "Prompt/instructions for the child agent."},
					"mode":        map[string]any{"type": "string", "description": "Optional agent mode override (e.g. readonly)."},
					"working_dir": map[string]any{"type": "string", "description": "Optional working directory override for the child."},
					"background":  map[string]any{"type": "boolean", "description": "If true, spawn task in background and return task ID immediately. Use TaskResult to collect output."},
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
			return "", fmt.Errorf("prompt is required — the prompt parameter was empty; build the full prompt string (context + constraints + file list) with all placeholders substituted, then retry the Task call")
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

		// Background task: spawn and return ID immediately
		if args.Background {
			if cfg.Registry == nil {
				return "", fmt.Errorf("background tasks not supported (registry not initialized)")
			}
			taskID := cfg.Registry.SpawnTask(ctx, cfg, args, workDir)
			return fmt.Sprintf("Task started in background. Task ID: %s\nUse TaskResult(task_id=\"%s\") to collect the output when ready.", taskID, taskID), nil
		}

		// Blocking task: wait for completion
		childCtx := WithTaskDepth(ctx, depth+1)
		logging.InfoContext(ctx, "Task tool: spawning child agent=%s depth=%d", args.Agent, depth+1)

		response, childMetrics, err := cfg.CallModel(childCtx, cfg.AgentsDir, args.Agent, args.Prompt, workDir, args.Mode)
		if err != nil {
			return "", fmt.Errorf("child agent %q failed: %w", args.Agent, err)
		}

		// Cap output to 64KB to avoid blowing up context.
		if len(response) > maxToolOutput {
			response = response[:maxToolOutput] + "\n...output truncated"
		}

		metricsInfo := ""
		if childMetrics != nil {
			metricsInfo = fmt.Sprintf(" tokens=%d", childMetrics.TotalTokens())
			if cfg.ParentMetrics != nil {
				cfg.ParentMetrics.AddChild(args.Agent, childMetrics)
			}
		}
		logging.InfoContext(ctx, "Task tool: child agent=%s completed (%d bytes%s)", args.Agent, len(response), metricsInfo)
		return response, nil
	}
}

func definitionTaskResult() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "TaskResult",
			Description: "Collect the result of a background task spawned with Task(background=true). Blocks until the task completes.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string", "description": "The task ID returned by Task(background=true)."},
				},
				"required": []string{"task_id"},
			},
		},
	}
}

type taskResultArgs struct {
	TaskID string `json:"task_id"`
}

func taskResultTool(cfg TaskConfig) func(ctx context.Context, rawArgs []byte) (string, error) {
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var args taskResultArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("invalid TaskResult args: %w", err)
		}
		if args.TaskID == "" {
			return "", fmt.Errorf("task_id is required")
		}

		if cfg.Registry == nil {
			return "", fmt.Errorf("background tasks not supported (registry not initialized)")
		}

		result, ok := cfg.Registry.GetResult(args.TaskID, true)
		if !ok {
			return "", fmt.Errorf("unknown task ID: %s", args.TaskID)
		}

		if result.Err != nil {
			return "", fmt.Errorf("task %s failed: %w", args.TaskID, result.Err)
		}

		metricsInfo := ""
		if result.Metrics != nil {
			metricsInfo = fmt.Sprintf(" (tokens=%d)", result.Metrics.TotalTokens())
			if cfg.ParentMetrics != nil {
				cfg.ParentMetrics.AddChild(args.TaskID, result.Metrics)
			}
		}
		logging.InfoContext(ctx, "TaskResult: collected result for %s (%d bytes%s)", args.TaskID, len(result.Output), metricsInfo)

		return result.Output, nil
	}
}
