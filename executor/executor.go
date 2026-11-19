// Package executor provides pluggable command execution backends.
// Agents declare their execution environment in agent.yaml, and the
// appropriate executor routes Bash tool commands to local shell,
// Docker containers, AWS SSM sessions, or Kubernetes pods.
package executor

import "context"

// Executor runs shell commands in an execution environment.
type Executor interface {
	// Execute runs a shell command and returns combined stdout+stderr.
	Execute(ctx context.Context, command string) ([]byte, error)

	// Close releases any resources held by the executor
	// (e.g., stops a long-lived Docker container).
	Close() error

	// Type returns the executor backend name (e.g., "local", "docker", "ssm", "kubectl").
	Type() string

	// EnvironmentDescription returns a human-readable summary of the execution
	// environment, suitable for injection into agent system prompts so the model
	// knows where its tool commands will run.
	EnvironmentDescription() string
}

// commandFailedStatus is the span status description for a failed executor
// call. The backend's error can quote the command, so its text reaches the
// span only under SQUAD_OTEL_EXPORT_COMMANDS=1 (see
// telemetry.RecordContentError).
const commandFailedStatus = "executor command failed"
