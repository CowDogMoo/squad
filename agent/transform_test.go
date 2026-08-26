package agent

import (
	"strings"
	"testing"
)

func TestBuildTransformBundle(t *testing.T) {
	t.Parallel()
	bundle := BuildTransformBundle("some piped diff", "/work")

	if bundle.User != "some piped diff" {
		t.Errorf("User = %q, want the prompt", bundle.User)
	}
	if bundle.WorkDir != "/work" {
		t.Errorf("WorkDir = %q, want /work", bundle.WorkDir)
	}
	if !bundle.DisableTask {
		t.Error("DisableTask = false, want true")
	}
	if !bundle.RemoteOnly {
		t.Error("RemoteOnly = false, want true")
	}
	for _, section := range []string{
		"# Squad Agent Bundle",
		"Agent: transform (built-in)",
		"Mode: readonly",
		"## Agent Wrapper",
		"## System Prompt",
		"System Override",
		"## Task",
		"NO INPUT",
	} {
		if !strings.Contains(bundle.System, section) {
			t.Errorf("System missing %q", section)
		}
	}
	combined := string(bundle.Combined)
	if !strings.Contains(combined, "## User Message\n\nsome piped diff") {
		t.Errorf("Combined missing user message, got:\n%s", combined)
	}
}

func TestBuildTransformBundle_EmptyPrompt(t *testing.T) {
	t.Parallel()
	bundle := BuildTransformBundle("", "/work")
	if bundle.User != "Begin." {
		t.Errorf("User = %q, want the Begin. placeholder", bundle.User)
	}
}
