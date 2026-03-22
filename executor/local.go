package executor

import (
	"bytes"
	"context"
	"os/exec"
)

// LocalExecutor runs commands via local bash shell.
// This is the default executor when no environment is configured.
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
