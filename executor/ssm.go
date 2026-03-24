package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// SSMExecutor runs commands on an EC2 instance via AWS Systems Manager.
type SSMExecutor struct {
	instanceID string
	region     string
	profile    string
}

// NewSSMExecutor creates an SSM executor targeting the given EC2 instance.
func NewSSMExecutor(cfg *Config) (*SSMExecutor, error) {
	if _, err := exec.LookPath("aws"); err != nil {
		return nil, fmt.Errorf("aws CLI not found: %w", err)
	}

	instanceID := cfg.Options["instance_id"]
	if instanceID == "" {
		return nil, fmt.Errorf("ssm executor requires 'instance_id' option")
	}

	return &SSMExecutor{
		instanceID: instanceID,
		region:     cfg.Options["region"],
		profile:    cfg.Options["profile"],
	}, nil
}

// Execute runs a command on the EC2 instance via ssm send-command
// and waits for the result.
func (e *SSMExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	// Build the parameters as proper JSON so that multi-line commands
	// (containing newlines) are correctly encoded.  The previous approach
	// using fmt.Sprintf(`commands=[%q]`, command) relied on Go's %q verb
	// which produces Go-escaped strings — the AWS CLI shorthand parser
	// does not interpret \n as a newline, causing literal "backslash-n"
	// to appear in commands and corrupt filenames.
	params := struct {
		Commands []string `json:"commands"`
	}{
		Commands: []string{command},
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SSM parameters: %w", err)
	}

	sendArgs := e.baseArgs("ssm", "send-command",
		"--instance-ids", e.instanceID,
		"--document-name", "AWS-RunShellScript",
		"--parameters", string(paramsJSON),
		"--output", "json",
	)

	var sendOut bytes.Buffer
	sendCmd := exec.CommandContext(ctx, "aws", sendArgs...)
	sendCmd.Stdout = &sendOut
	sendCmd.Stderr = &sendOut
	if err := sendCmd.Run(); err != nil {
		return sendOut.Bytes(), fmt.Errorf("ssm send-command failed: %w", err)
	}

	// Extract the command ID from the response.
	var result struct {
		Command struct {
			CommandID string `json:"CommandId"`
		} `json:"Command"`
	}
	if err := json.Unmarshal(sendOut.Bytes(), &result); err != nil {
		return sendOut.Bytes(), fmt.Errorf("failed to parse send-command response: %w", err)
	}
	commandID := result.Command.CommandID
	if commandID == "" {
		return sendOut.Bytes(), fmt.Errorf("send-command returned empty CommandId")
	}

	// Wait for the command to complete and get output.
	waitArgs := e.baseArgs("ssm", "wait", "command-executed",
		"--command-id", commandID,
		"--instance-id", e.instanceID,
	)
	waitCmd := exec.CommandContext(ctx, "aws", waitArgs...)
	_ = waitCmd.Run() // best-effort wait; get-command-invocation will show status

	// Retrieve the output.
	getArgs := e.baseArgs("ssm", "get-command-invocation",
		"--command-id", commandID,
		"--instance-id", e.instanceID,
		"--output", "json",
	)

	var getOut bytes.Buffer
	getCmd := exec.CommandContext(ctx, "aws", getArgs...)
	getCmd.Stdout = &getOut
	getCmd.Stderr = &getOut
	if err := getCmd.Run(); err != nil {
		return getOut.Bytes(), fmt.Errorf("ssm get-command-invocation failed: %w", err)
	}

	var invocation struct {
		StandardOutputContent string `json:"StandardOutputContent"`
		StandardErrorContent  string `json:"StandardErrorContent"`
		Status                string `json:"Status"`
		ResponseCode          int    `json:"ResponseCode"`
	}
	if err := json.Unmarshal(getOut.Bytes(), &invocation); err != nil {
		return getOut.Bytes(), fmt.Errorf("failed to parse invocation response: %w", err)
	}

	output := invocation.StandardOutputContent
	if invocation.StandardErrorContent != "" {
		output += "\nSTDERR:\n" + invocation.StandardErrorContent
	}

	// Only treat SSM infrastructure failures as errors (e.g., delivery
	// timeout, agent not reachable).  A command that runs but exits
	// non-zero (Status "Failed", ResponseCode > 0) is a normal tool
	// result — return the output so the model can interpret it.
	switch invocation.Status {
	case "Success":
		return []byte(output), nil
	case "Failed":
		// Command executed but exited non-zero.  Append the exit code
		// so the model knows, but don't return an error.
		output += fmt.Sprintf("\n[exit code %d]", invocation.ResponseCode)
		return []byte(output), nil
	default:
		// InProgress, TimedOut, Cancelled, etc. — real infrastructure issues.
		return []byte(output), fmt.Errorf("command status: %s", invocation.Status)
	}
}

// Close is a no-op for SSM (stateless per-command).
func (e *SSMExecutor) Close() error { return nil }

// baseArgs prepends region and profile flags to AWS CLI arguments.
func (e *SSMExecutor) baseArgs(args ...string) []string {
	var base []string
	if e.region != "" {
		base = append(base, "--region", e.region)
	}
	if e.profile != "" {
		base = append(base, "--profile", e.profile)
	}
	return append(base, args...)
}
