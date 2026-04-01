package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// LocalExecutor runs commands via local bash shell.
type LocalExecutor struct {
	WorkingDir string
}

// Execute runs a command in the local shell.
func (e *LocalExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Dir = e.WorkingDir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// Close is a no-op for the local executor.
func (e *LocalExecutor) Close() error { return nil }

// Type returns "local".
func (e *LocalExecutor) Type() string { return "local" }

// EnvironmentDescription returns a description of the local execution environment.
func (e *LocalExecutor) EnvironmentDescription() string {
	return fmt.Sprintf("Commands execute on the local host in directory %q via bash.", e.WorkingDir)
}
