package telemetry

import (
	"strings"
	"testing"
)

func TestToolArgsAttributesDefaultIsSizeOnly(t *testing.T) {
	t.Setenv(ExportCommandsEnv, "")
	got := attrMap(ToolArgsAttributes(42, "curl -H 'Authorization: Bearer "+fakeSecret+"' https://lab.test"))
	if len(got) != 1 || got["squad.tool.args_bytes"].AsInt64() != 42 {
		t.Fatalf("attributes = %v, want only squad.tool.args_bytes=42", got)
	}
}

func TestToolArgsAttributesOptIn(t *testing.T) {
	t.Setenv(ExportCommandsEnv, "1")
	got := attrMap(ToolArgsAttributes(10, "curl -H 'Authorization: Bearer "+fakeSecret+"' https://lab.test"))
	args := got["squad.tool.args"].AsString()
	if strings.Contains(args, fakeSecret) || !strings.Contains(args, RedactedMarker) {
		t.Errorf("squad.tool.args = %q, want the bearer value redacted", args)
	}
	if _, ok := attrMap(ToolArgsAttributes(10, ""))["squad.tool.args"]; ok {
		t.Error("an empty summary must not add squad.tool.args")
	}
	long := attrMap(ToolArgsAttributes(5000, strings.Repeat("a", 5000)))["squad.tool.args"].AsString()
	if len(long) > maxExportedCommandBytes+len("...") {
		t.Errorf("squad.tool.args is %d bytes, want <= %d", len(long), maxExportedCommandBytes+3)
	}
}

func TestTextAttributes(t *testing.T) {
	text := "I'll log in with password=" + fakeSecret + " and check the token."
	t.Setenv(ExportTextEnv, "")
	got := attrMap(TextAttributes(text))
	if len(got) != 1 || got["squad.turn.text_bytes"].AsInt64() != int64(len(text)) {
		t.Fatalf("default attributes = %v, want only squad.turn.text_bytes", got)
	}

	// The commands switch alone does not export text.
	t.Setenv(ExportCommandsEnv, "1")
	if _, ok := attrMap(TextAttributes(text))["squad.turn.text"]; ok {
		t.Error("SQUAD_OTEL_EXPORT_COMMANDS must not export assistant text")
	}

	t.Setenv(ExportTextEnv, "1")
	got = attrMap(TextAttributes(text))
	exported := got["squad.turn.text"].AsString()
	if strings.Contains(exported, fakeSecret) || !strings.Contains(exported, "password="+RedactedMarker) {
		t.Errorf("squad.turn.text = %q, want the password redacted", exported)
	}
	if _, ok := attrMap(TextAttributes(""))["squad.turn.text"]; ok {
		t.Error("empty text must not add squad.turn.text")
	}
	long := attrMap(TextAttributes(strings.Repeat("b", 3000)))["squad.turn.text"].AsString()
	if len(long) > maxExportedTextBytes+len("...") {
		t.Errorf("squad.turn.text is %d bytes, want <= %d", len(long), maxExportedTextBytes+3)
	}
}
