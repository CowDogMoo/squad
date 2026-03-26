package executor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/cowdogmoo/squad/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// defaultSSMTimeout is the default command timeout in seconds (10 minutes).
// Tools like httpx, trufflehog, and gau can scan hundreds of subdomains and
// need significantly more than the default 100s SSM timeout.
const defaultSSMTimeout = 600

// ssmAPI abstracts the SSM SDK calls for testing.
type ssmAPI interface {
	SendCommand(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error)
	GetCommandInvocation(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error)
}

// SSMExecutor runs commands on an EC2 instance via AWS Systems Manager.
type SSMExecutor struct {
	instanceID string
	region     string
	workingDir string
	timeout    int32
	client     ssmAPI
}

// NewSSMExecutor creates an SSM executor targeting the given EC2 instance.
func NewSSMExecutor(cfg *Config, workingDir string) (*SSMExecutor, error) {
	return newSSMExecutor(cfg, workingDir, nil)
}

// newSSMExecutor is the internal constructor. Pass a non-nil ssmAPI to
// override the real client (for testing).
func newSSMExecutor(cfg *Config, workingDir string, client ssmAPI) (*SSMExecutor, error) {
	instanceID := cfg.Options["instance_id"]
	if instanceID == "" {
		return nil, fmt.Errorf("ssm executor requires 'instance_id' option")
	}

	timeout := int32(defaultSSMTimeout)
	if t := cfg.Options["timeout"]; t != "" {
		parsed, err := strconv.Atoi(t)
		if err != nil {
			return nil, fmt.Errorf("ssm option 'timeout' must be an integer (seconds): %w", err)
		}
		timeout = int32(parsed)
	}

	ex := &SSMExecutor{
		instanceID: instanceID,
		region:     cfg.Options["region"],
		workingDir: workingDir,
		timeout:    timeout,
		client:     client,
	}

	// Build a real SDK client when none was injected (production path).
	if client == nil {
		var opts []func(*awsconfig.LoadOptions) error
		if ex.region != "" {
			opts = append(opts, awsconfig.WithRegion(ex.region))
		}
		if profile := cfg.Options["profile"]; profile != "" {
			opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to load AWS config: %w", err)
		}
		ex.client = ssm.NewFromConfig(awsCfg)
	}

	return ex, nil
}

// Execute runs a command on the EC2 instance via SSM SendCommand
// and polls for the result.
func (e *SSMExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "executor.ssm",
		trace.WithAttributes(
			attribute.String("squad.executor.instance_id", e.instanceID),
			attribute.String("squad.executor.region", e.region),
			attribute.String("squad.executor.command", command),
		),
	)
	defer span.End()

	// Prepend a cd into the working directory when one is configured so
	// that commands run in the expected location on the remote host.
	if e.workingDir != "" {
		command = fmt.Sprintf("mkdir -p %s && cd %s && %s", e.workingDir, e.workingDir, command)
	}

	sendOut, err := e.client.SendCommand(ctx, &ssm.SendCommandInput{
		InstanceIds:  []string{e.instanceID},
		DocumentName: aws.String("AWS-RunShellScript"),
		Parameters: map[string][]string{
			"commands": {command},
		},
		TimeoutSeconds: aws.Int32(e.timeout),
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ssm SendCommand failed: %w", err)
	}

	commandID := aws.ToString(sendOut.Command.CommandId)
	if commandID == "" {
		return nil, fmt.Errorf("SendCommand returned empty CommandId")
	}

	// Poll for command completion. The SDK waiter can be flaky with long
	// timeouts, so we poll manually with exponential backoff.
	var invocation *ssm.GetCommandInvocationOutput
	pollInput := &ssm.GetCommandInvocationInput{
		CommandId:  aws.String(commandID),
		InstanceId: aws.String(e.instanceID),
	}

	deadline := time.Duration(e.timeout) * time.Second
	timer := time.NewTimer(deadline + 30*time.Second)
	defer timer.Stop()
	delay := 2 * time.Second

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, fmt.Errorf("timed out waiting for SSM command %s", commandID)
		default:
		}

		time.Sleep(delay)
		if delay < 10*time.Second {
			delay = delay * 3 / 2
		}

		invocation, err = e.client.GetCommandInvocation(ctx, pollInput)
		if err != nil {
			// InvocationDoesNotExist means the command hasn't been delivered yet.
			if strings.Contains(err.Error(), "InvocationDoesNotExist") {
				continue
			}
			return nil, fmt.Errorf("ssm GetCommandInvocation failed: %w", err)
		}

		switch invocation.Status {
		case ssmtypes.CommandInvocationStatusPending,
			ssmtypes.CommandInvocationStatusInProgress,
			ssmtypes.CommandInvocationStatusDelayed:
			continue
		default:
			// Terminal status — break out of the poll loop.
			goto done
		}
	}

done:
	output := aws.ToString(invocation.StandardOutputContent)
	if stderr := aws.ToString(invocation.StandardErrorContent); stderr != "" {
		output += "\nSTDERR:\n" + stderr
	}

	// Only treat SSM infrastructure failures as errors (e.g., delivery
	// timeout, agent not reachable).  A command that runs but exits
	// non-zero (Status "Failed", ResponseCode > 0) is a normal tool
	// result — return the output so the model can interpret it.
	span.SetAttributes(
		attribute.String("squad.executor.ssm.command_id", commandID),
		attribute.String("squad.executor.ssm.status", string(invocation.Status)),
		attribute.Int("squad.executor.exit_code", int(invocation.ResponseCode)),
	)

	switch invocation.Status {
	case ssmtypes.CommandInvocationStatusSuccess:
		return []byte(output), nil
	case ssmtypes.CommandInvocationStatusFailed:
		// Command executed but exited non-zero.
		output += fmt.Sprintf("\n[exit code %d]", invocation.ResponseCode)
		return []byte(output), nil
	default:
		// TimedOut, Cancelled, etc. — real infrastructure issues.
		err := fmt.Errorf("command status: %s", string(invocation.Status))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return []byte(output), err
	}
}

// Close is a no-op for SSM (stateless per-command).
func (e *SSMExecutor) Close() error { return nil }

// Type returns "ssm".
func (e *SSMExecutor) Type() string { return "ssm" }

// EnvironmentDescription returns a description of the SSM execution environment.
func (e *SSMExecutor) EnvironmentDescription() string {
	desc := fmt.Sprintf(
		"Commands execute on EC2 instance %s via AWS Systems Manager (SSM).",
		e.instanceID,
	)
	if e.region != "" {
		desc += fmt.Sprintf(" Region: %s.", e.region)
	}
	if e.workingDir != "" {
		desc += fmt.Sprintf(" Working directory: %s.", e.workingDir)
	}
	desc += " Tools like Bash run remotely on the EC2 host, not locally. " +
		"If tools are installed inside Docker containers on the host, " +
		"you must use 'docker exec <container> <command>' to reach them."
	return desc
}
