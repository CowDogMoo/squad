package tools

import "errors"

// Sentinel errors for common agent failure modes.
// Use errors.Is() to check for these in callers.
var (
	// ErrToolExecutionFailed indicates a tool handler returned an error.
	ErrToolExecutionFailed = errors.New("tool execution failed")

	// ErrIterationLimitReached indicates the agent hit its max iteration count.
	ErrIterationLimitReached = errors.New("iteration limit reached")

	// ErrLoopDetected indicates the agent was stuck repeating identical tool calls.
	ErrLoopDetected = errors.New("loop detected: agent repeating identical tool calls")

	// ErrEditDeadlineReached indicates the agent failed to make edits within the deadline.
	ErrEditDeadlineReached = errors.New("edit deadline reached without edits")

	// ErrBlockedCommand indicates a dangerous command was rejected by the safety filter.
	ErrBlockedCommand = errors.New("blocked command")

	// ErrFileNotRead indicates an edit was attempted on a file that hasn't been read.
	ErrFileNotRead = errors.New("file not read before edit")

	// ErrStaleRead indicates a file was modified after the agent last read it.
	ErrStaleRead = errors.New("file modified since last read")
)
