package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// fakeSSMClient implements ssmAPI for testing.
type fakeSSMClient struct {
	sendCommandFn        func(ctx context.Context, params *ssm.SendCommandInput) (*ssm.SendCommandOutput, error)
	getCommandInvocation func(ctx context.Context, params *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error)
	// Track calls for assertions.
	sendCalls []ssm.SendCommandInput
}

func (f *fakeSSMClient) SendCommand(ctx context.Context, params *ssm.SendCommandInput, _ ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
	f.sendCalls = append(f.sendCalls, *params)
	if f.sendCommandFn != nil {
		return f.sendCommandFn(ctx, params)
	}
	return &ssm.SendCommandOutput{
		Command: &ssmtypes.Command{CommandId: aws.String("cmd-fake")},
	}, nil
}

func (f *fakeSSMClient) GetCommandInvocation(ctx context.Context, params *ssm.GetCommandInvocationInput, _ ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
	if f.getCommandInvocation != nil {
		return f.getCommandInvocation(ctx, params)
	}
	return &ssm.GetCommandInvocationOutput{
		Status:                ssmtypes.CommandInvocationStatusSuccess,
		StandardOutputContent: aws.String("ok"),
	}, nil
}

func TestNewSSMExecutor_MissingInstanceID(t *testing.T) {
	t.Parallel()
	_, err := newSSMExecutor(&Config{Options: map[string]string{}}, "", &fakeSSMClient{})
	if err == nil || !strings.Contains(err.Error(), "instance_id") {
		t.Fatalf("expected instance_id error, got: %v", err)
	}
}

func TestNewSSMExecutor_Success(t *testing.T) {
	t.Parallel()
	ex, err := newSSMExecutor(&Config{
		Options: map[string]string{
			"instance_id": "i-abc123",
			"region":      "us-east-1",
			"profile":     "dev",
		},
	}, "/work", &fakeSSMClient{})
	if err != nil {
		t.Fatalf("newSSMExecutor: %v", err)
	}
	if ex.instanceID != "i-abc123" {
		t.Fatalf("instanceID = %q, want i-abc123", ex.instanceID)
	}
	if ex.region != "us-east-1" {
		t.Fatalf("region = %q, want us-east-1", ex.region)
	}
	if ex.workingDir != "/work" {
		t.Fatalf("workingDir = %q, want /work", ex.workingDir)
	}
	if ex.timeout != defaultSSMTimeout {
		t.Fatalf("timeout = %d, want %d", ex.timeout, int32(defaultSSMTimeout))
	}
}

func TestNewSSMExecutor_CustomTimeout(t *testing.T) {
	t.Parallel()
	ex, err := newSSMExecutor(&Config{
		Options: map[string]string{
			"instance_id": "i-abc123",
			"timeout":     "900",
		},
	}, "", &fakeSSMClient{})
	if err != nil {
		t.Fatalf("newSSMExecutor: %v", err)
	}
	if ex.timeout != 900 {
		t.Fatalf("timeout = %d, want 900", ex.timeout)
	}
}

func TestNewSSMExecutor_InvalidTimeout(t *testing.T) {
	t.Parallel()
	_, err := newSSMExecutor(&Config{
		Options: map[string]string{
			"instance_id": "i-abc123",
			"timeout":     "not-a-number",
		},
	}, "", &fakeSSMClient{})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestSSMExecutor_Close(t *testing.T) {
	t.Parallel()
	ex := &SSMExecutor{instanceID: "i-abc"}
	if err := ex.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSSMExecutor_Execute_Success(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("hello\n"),
			}, nil
		},
	}

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		region:     "us-east-1",
		timeout:    defaultSSMTimeout,
		client:     client,
	}

	out, err := ex.Execute(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Fatalf("output = %q, want 'hello'", string(out))
	}

	// Verify SendCommand was called with correct timeout.
	if len(client.sendCalls) != 1 {
		t.Fatalf("expected 1 SendCommand call, got %d", len(client.sendCalls))
	}
	if ts := client.sendCalls[0].TimeoutSeconds; ts == nil || *ts != defaultSSMTimeout {
		t.Fatalf("TimeoutSeconds = %v, want %d", ts, defaultSSMTimeout)
	}
}

func TestSSMExecutor_Execute_WithWorkingDir(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("ok"),
			}, nil
		},
	}

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		workingDir: "/opt/app",
		timeout:    defaultSSMTimeout,
		client:     client,
	}

	_, err := ex.Execute(context.Background(), "ls")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Verify the command was wrapped with cd.
	if len(client.sendCalls) != 1 {
		t.Fatalf("expected 1 SendCommand call, got %d", len(client.sendCalls))
	}
	cmds := client.sendCalls[0].Parameters["commands"]
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	wantCmd := "mkdir -p /opt/app && cd /opt/app && ls"
	if cmds[0] != wantCmd {
		t.Fatalf("command = %q, want %q", cmds[0], wantCmd)
	}
}

func TestSSMExecutor_Execute_WithStderr(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("out"),
				StandardErrorContent:  aws.String("warn"),
			}, nil
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", timeout: defaultSSMTimeout, client: client}

	out, err := ex.Execute(context.Background(), "cmd")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "out") || !strings.Contains(string(out), "warn") {
		t.Fatalf("output = %q, want both stdout and stderr", string(out))
	}
}

func TestSSMExecutor_Execute_SendCommandFails(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		sendCommandFn: func(_ context.Context, _ *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", timeout: defaultSSMTimeout, client: client}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "SendCommand failed") {
		t.Fatalf("expected SendCommand error, got: %v", err)
	}
}

func TestSSMExecutor_Execute_EmptyCommandID(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		sendCommandFn: func(_ context.Context, _ *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("")},
			}, nil
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", timeout: defaultSSMTimeout, client: client}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "empty CommandId") {
		t.Fatalf("expected empty CommandId error, got: %v", err)
	}
}

func TestSSMExecutor_Execute_GetInvocationFails(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			return nil, fmt.Errorf("get failed")
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", timeout: defaultSSMTimeout, client: client}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "GetCommandInvocation failed") {
		t.Fatalf("expected GetCommandInvocation error, got: %v", err)
	}
}

func TestSSMExecutor_Execute_CommandFailedStatus(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusFailed,
				StandardOutputContent: aws.String("partial output"),
				ResponseCode:          1,
			}, nil
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", timeout: defaultSSMTimeout, client: client}

	out, err := ex.Execute(context.Background(), "cmd")
	// "Failed" status means the command ran but exited non-zero — this is
	// treated as a normal tool result (no error), with an exit code appended.
	if err != nil {
		t.Fatalf("expected no error for Failed status, got: %v", err)
	}
	if !strings.Contains(string(out), "partial output") {
		t.Fatalf("expected partial output, got: %q", string(out))
	}
	if !strings.Contains(string(out), "[exit code") {
		t.Fatalf("expected exit code annotation, got: %q", string(out))
	}
}

func TestSSMExecutor_Execute_TimedOutStatus(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusTimedOut,
				StandardOutputContent: aws.String("partial"),
			}, nil
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", timeout: defaultSSMTimeout, client: client}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "TimedOut") {
		t.Fatalf("expected TimedOut error, got: %v", err)
	}
}

func TestSSMExecutor_EnvironmentDescription(t *testing.T) {
	t.Parallel()
	ex := &SSMExecutor{instanceID: "i-abc123", region: "us-east-1", workingDir: "/tmp/work"}
	desc := ex.EnvironmentDescription()
	if !strings.Contains(desc, "i-abc123") {
		t.Fatalf("expected instance ID in description, got: %q", desc)
	}
	if !strings.Contains(desc, "us-east-1") {
		t.Fatalf("expected region in description, got: %q", desc)
	}
	if !strings.Contains(desc, "/tmp/work") {
		t.Fatalf("expected working dir in description, got: %q", desc)
	}
}

func TestSSMExecutor_Type(t *testing.T) {
	t.Parallel()
	ex := &SSMExecutor{}
	if got := ex.Type(); got != "ssm" {
		t.Fatalf("Type() = %q, want 'ssm'", got)
	}
}

func TestSSMExecutor_EnvironmentDescriptionNoRegion(t *testing.T) {
	t.Parallel()
	ex := &SSMExecutor{instanceID: "i-abc123", workingDir: "/work"}
	desc := ex.EnvironmentDescription()
	if !strings.Contains(desc, "i-abc123") {
		t.Fatalf("expected instance ID in description, got: %q", desc)
	}
	if strings.Contains(desc, "Region:") {
		t.Fatalf("expected no region in description, got: %q", desc)
	}
	if !strings.Contains(desc, "/work") {
		t.Fatalf("expected working dir in description, got: %q", desc)
	}
}

func TestSSMExecutor_EnvironmentDescriptionNoWorkDir(t *testing.T) {
	t.Parallel()
	ex := &SSMExecutor{instanceID: "i-abc123", region: "eu-west-1"}
	desc := ex.EnvironmentDescription()
	if !strings.Contains(desc, "i-abc123") {
		t.Fatalf("expected instance ID in description, got: %q", desc)
	}
	if !strings.Contains(desc, "eu-west-1") {
		t.Fatalf("expected region in description, got: %q", desc)
	}
	if strings.Contains(desc, "Working directory:") {
		t.Fatalf("expected no working dir in description, got: %q", desc)
	}
}

func TestSSMExecutor_Execute_InvocationNotExistThenSuccess(t *testing.T) {
	t.Parallel()

	callCount := 0
	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			callCount++
			if callCount == 1 {
				return nil, &ssmtypes.InvocationDoesNotExist{Message: aws.String("command not delivered yet")}
			}
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("done"),
			}, nil
		},
	}

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		timeout:    defaultSSMTimeout,
		client:     client,
	}

	out, err := ex.Execute(context.Background(), "echo done")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "done") {
		t.Fatalf("output = %q, want 'done'", string(out))
	}
	if callCount < 2 {
		t.Fatalf("expected at least 2 GetCommandInvocation calls, got %d", callCount)
	}
}

func TestSSMExecutor_Execute_PendingThenSuccess(t *testing.T) {
	t.Parallel()

	callCount := 0
	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			callCount++
			if callCount <= 2 {
				return &ssm.GetCommandInvocationOutput{
					Status: ssmtypes.CommandInvocationStatusPending,
				}, nil
			}
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("done"),
			}, nil
		},
	}

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		timeout:    defaultSSMTimeout,
		client:     client,
	}

	out, err := ex.Execute(context.Background(), "echo done")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "done") {
		t.Fatalf("output = %q, want 'done'", string(out))
	}
	if callCount < 3 {
		t.Fatalf("expected at least 3 GetCommandInvocation calls (2 Pending + 1 Success), got %d", callCount)
	}
}

func TestSSMExecutor_Execute_InProgressThenSuccess(t *testing.T) {
	t.Parallel()

	callCount := 0
	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			callCount++
			if callCount == 1 {
				return &ssm.GetCommandInvocationOutput{
					Status: ssmtypes.CommandInvocationStatusInProgress,
				}, nil
			}
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("result"),
			}, nil
		},
	}

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		timeout:    defaultSSMTimeout,
		client:     client,
	}

	out, err := ex.Execute(context.Background(), "ls")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "result") {
		t.Fatalf("output = %q, want 'result'", string(out))
	}
}

func TestSSMExecutor_Execute_DelayedThenSuccess(t *testing.T) {
	t.Parallel()

	callCount := 0
	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			callCount++
			if callCount == 1 {
				return &ssm.GetCommandInvocationOutput{
					Status: ssmtypes.CommandInvocationStatusDelayed,
				}, nil
			}
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("ok"),
			}, nil
		},
	}

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		timeout:    defaultSSMTimeout,
		client:     client,
	}

	out, err := ex.Execute(context.Background(), "echo ok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("output = %q, want 'ok'", string(out))
	}
}

func TestSSMExecutor_Execute_ContextCancelled(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status: ssmtypes.CommandInvocationStatusInProgress,
			}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the polling loop sees it.
	cancel()

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		timeout:    defaultSSMTimeout,
		client:     client,
	}

	_, err := ex.Execute(ctx, "long-running")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestIsShellSafePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"/opt/app", true},
		{"relative/path", true},
		{"with-dash_and.dot", true},
		{"alpha123", true},
		{"has space", false},
		{"semi;colon", false},
		{"dollar$sign", false},
		{"back\\slash", false},
		{"pipe|char", false},
		{"quote'mark", false},
	}
	for _, c := range cases {
		if got := isShellSafePath(c.in); got != c.want {
			t.Errorf("isShellSafePath(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestShellSingleQuote(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", "''"},
		{"plain", "'plain'"},
		{"with space", "'with space'"},
		{"a'b", `'a'\''b'`},
		{"a'b'c", `'a'\''b'\''c'`},
		{"$dangerous;`stuff`", "'$dangerous;`stuff`'"},
	}
	for _, c := range cases {
		if got := shellSingleQuote(c.in); got != c.want {
			t.Errorf("shellSingleQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSSMExecutor_Execute_QuotesUnsafeWorkingDir asserts that a workingDir
// containing shell metacharacters is wrapped in single quotes before being
// composed into the remote command, blocking CWE-77 command injection.
func TestSSMExecutor_Execute_QuotesUnsafeWorkingDir(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		sendCommandFn: func(_ context.Context, params *ssm.SendCommandInput) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{Command: &ssmtypes.Command{CommandId: aws.String("cid")}}, nil
		},
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("ok"),
			}, nil
		},
	}

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		workingDir: "/opt/app; rm -rf /",
		timeout:    defaultSSMTimeout,
		client:     client,
	}

	if _, err := ex.Execute(context.Background(), "ls"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(client.sendCalls) != 1 {
		t.Fatalf("expected 1 SendCommand call, got %d", len(client.sendCalls))
	}
	got := client.sendCalls[0].Parameters["commands"][0]
	want := "mkdir -p '/opt/app; rm -rf /' && cd '/opt/app; rm -rf /' && ls"
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestSSMExecutor_Execute_CancelledStatus(t *testing.T) {
	t.Parallel()

	client := &fakeSSMClient{
		getCommandInvocation: func(_ context.Context, _ *ssm.GetCommandInvocationInput) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusCancelled,
				StandardOutputContent: aws.String("partial"),
			}, nil
		},
	}

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		timeout:    defaultSSMTimeout,
		client:     client,
	}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "Cancelled") {
		t.Fatalf("expected Cancelled error, got: %v", err)
	}
}
