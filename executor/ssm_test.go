package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestNewSSMExecutor_MissingInstanceID(t *testing.T) {
	t.Parallel()
	_, err := newSSMExecutor(&Config{Options: map[string]string{}}, &fakeCmdRunner{})
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
	}, &fakeCmdRunner{})
	if err != nil {
		t.Fatalf("newSSMExecutor: %v", err)
	}
	if ex.instanceID != "i-abc123" {
		t.Fatalf("instanceID = %q, want i-abc123", ex.instanceID)
	}
	if ex.region != "us-east-1" {
		t.Fatalf("region = %q, want us-east-1", ex.region)
	}
	if ex.profile != "dev" {
		t.Fatalf("profile = %q, want dev", ex.profile)
	}
}

func TestSSMExecutor_BaseArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		executor *SSMExecutor
		input    []string
		want     []string
	}{
		{
			name:     "no region or profile",
			executor: &SSMExecutor{instanceID: "i-abc"},
			input:    []string{"ssm", "send-command"},
			want:     []string{"ssm", "send-command"},
		},
		{
			name:     "with region",
			executor: &SSMExecutor{instanceID: "i-abc", region: "us-west-2"},
			input:    []string{"ssm", "send-command"},
			want:     []string{"--region", "us-west-2", "ssm", "send-command"},
		},
		{
			name:     "with profile",
			executor: &SSMExecutor{instanceID: "i-abc", profile: "prod"},
			input:    []string{"ssm", "send-command"},
			want:     []string{"--profile", "prod", "ssm", "send-command"},
		},
		{
			name:     "with both",
			executor: &SSMExecutor{instanceID: "i-abc", region: "eu-west-1", profile: "staging"},
			input:    []string{"ssm", "send-command"},
			want:     []string{"--region", "eu-west-1", "--profile", "staging", "ssm", "send-command"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.executor.baseArgs(tt.input...)
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("baseArgs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSSMExecutor_Close(t *testing.T) {
	t.Parallel()
	ex := &SSMExecutor{instanceID: "i-abc"}
	if err := ex.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// makeSendCommandResponse creates a fake SSM send-command JSON response.
func makeSendCommandResponse(commandID string) []byte {
	resp := struct {
		Command struct {
			CommandID string `json:"CommandId"`
		} `json:"Command"`
	}{}
	resp.Command.CommandID = commandID
	data, _ := json.Marshal(resp)
	return data
}

// makeInvocationResponse creates a fake SSM get-command-invocation JSON response.
func makeInvocationResponse(stdout, stderr, status string) []byte {
	resp := struct {
		StandardOutputContent string `json:"StandardOutputContent"`
		StandardErrorContent  string `json:"StandardErrorContent"`
		Status                string `json:"Status"`
	}{
		StandardOutputContent: stdout,
		StandardErrorContent:  stderr,
		Status:                status,
	}
	data, _ := json.Marshal(resp)
	return data
}

func TestSSMExecutor_Execute_Success(t *testing.T) {
	t.Parallel()

	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: makeSendCommandResponse("cmd-123")}, // send-command
			{Output: nil}, // wait
			{Output: makeInvocationResponse("hello\n", "", "Success")}, // get-command-invocation
		},
	}

	ex := &SSMExecutor{
		instanceID: "i-abc123",
		region:     "us-east-1",
		runner:     runner,
	}

	out, err := ex.Execute(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Fatalf("output = %q, want 'hello'", string(out))
	}

	// Verify 3 AWS CLI calls were made.
	if len(runner.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(runner.calls))
	}
}

func TestSSMExecutor_Execute_WithStderr(t *testing.T) {
	t.Parallel()

	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: makeSendCommandResponse("cmd-123")},
			{Output: nil},
			{Output: makeInvocationResponse("out", "warn", "Success")},
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", runner: runner}

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

	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: []byte("error"), Err: fmt.Errorf("aws error")},
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", runner: runner}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "send-command failed") {
		t.Fatalf("expected send-command error, got: %v", err)
	}
}

func TestSSMExecutor_Execute_InvalidSendResponse(t *testing.T) {
	t.Parallel()

	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: []byte("not json")},
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", runner: runner}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "parse send-command response") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestSSMExecutor_Execute_EmptyCommandID(t *testing.T) {
	t.Parallel()

	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: makeSendCommandResponse("")},
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", runner: runner}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "empty CommandId") {
		t.Fatalf("expected empty CommandId error, got: %v", err)
	}
}

func TestSSMExecutor_Execute_GetInvocationFails(t *testing.T) {
	t.Parallel()

	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: makeSendCommandResponse("cmd-123")},
			{Output: nil}, // wait
			{Output: []byte("err"), Err: fmt.Errorf("get failed")}, // get-command-invocation
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", runner: runner}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "get-command-invocation failed") {
		t.Fatalf("expected get-command-invocation error, got: %v", err)
	}
}

func TestSSMExecutor_Execute_InvalidInvocationResponse(t *testing.T) {
	t.Parallel()

	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: makeSendCommandResponse("cmd-123")},
			{Output: nil},
			{Output: []byte("not json")},
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", runner: runner}

	_, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "parse invocation response") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestSSMExecutor_Execute_CommandFailedStatus(t *testing.T) {
	t.Parallel()

	runner := &fakeCmdRunner{
		results: []fakeResult{
			{Output: makeSendCommandResponse("cmd-123")},
			{Output: nil},
			{Output: makeInvocationResponse("partial output", "", "Failed")},
		},
	}

	ex := &SSMExecutor{instanceID: "i-abc123", runner: runner}

	out, err := ex.Execute(context.Background(), "cmd")
	if err == nil || !strings.Contains(err.Error(), "Failed") {
		t.Fatalf("expected status error, got: %v", err)
	}
	if !strings.Contains(string(out), "partial output") {
		t.Fatalf("expected partial output, got: %q", string(out))
	}
}
