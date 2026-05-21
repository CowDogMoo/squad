package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// confirmInvoke is a thin helper that calls confirmTool with the given
// runtime and args. Returns the tool's resolution + error.
func confirmInvoke(t *testing.T, rt *ConfirmRuntime, args confirmArgs) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return confirmTool(rt)(context.Background(), raw)
}

func TestConfirm_RequiresSummary(t *testing.T) {
	if _, err := confirmInvoke(t, nil, confirmArgs{}); err == nil {
		t.Fatal("expected error when summary is empty")
	}
}

func TestConfirm_RejectsEmptyOption(t *testing.T) {
	_, err := confirmInvoke(t, &ConfirmRuntime{AutoConfirm: AutoConfirmYes},
		confirmArgs{Summary: "go?", Options: []string{"yes", ""}})
	if err == nil {
		t.Fatal("expected error for empty option string")
	}
}

func TestConfirm_AutoConfirmYes(t *testing.T) {
	out, err := confirmInvoke(t, &ConfirmRuntime{AutoConfirm: AutoConfirmYes},
		confirmArgs{Summary: "go?"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "yes" {
		t.Errorf("got %q want yes", out)
	}
}

func TestConfirm_AutoConfirmYesCustomOptions(t *testing.T) {
	out, err := confirmInvoke(t, &ConfirmRuntime{AutoConfirm: AutoConfirmYes},
		confirmArgs{Summary: "promote?", Options: []string{"promote", "skip"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "promote" {
		t.Errorf("got %q want promote", out)
	}
}

func TestConfirm_AutoConfirmNo(t *testing.T) {
	out, err := confirmInvoke(t, &ConfirmRuntime{AutoConfirm: AutoConfirmNo},
		confirmArgs{Summary: "go?"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "no" {
		t.Errorf("got %q want no", out)
	}
}

func TestConfirm_AutoConfirmNoRequiresTwoOptions(t *testing.T) {
	_, err := confirmInvoke(t, &ConfirmRuntime{AutoConfirm: AutoConfirmNo},
		confirmArgs{Summary: "go?", Options: []string{"only-one"}})
	if err == nil {
		t.Fatal("expected error when --auto-confirm=no but only one option")
	}
}

func TestConfirm_AutoConfirmAbort(t *testing.T) {
	_, err := confirmInvoke(t, &ConfirmRuntime{AutoConfirm: AutoConfirmAbort},
		confirmArgs{Summary: "go?"})
	if err == nil {
		t.Fatal("expected error from auto-confirm=abort")
	}
	if !strings.Contains(err.Error(), "aborting") {
		t.Errorf("error message should mention abort: %v", err)
	}
}

func TestConfirm_DefaultUnsetIsAbort(t *testing.T) {
	_, err := confirmInvoke(t, &ConfirmRuntime{}, confirmArgs{Summary: "go?"})
	if err == nil {
		t.Fatal("expected error when no auto-confirm policy is set")
	}
}

func TestConfirm_NilRuntimeIsAbort(t *testing.T) {
	_, err := confirmInvoke(t, nil, confirmArgs{Summary: "go?"})
	if err == nil {
		t.Fatal("expected error when runtime is nil")
	}
}

func TestConfirm_TTYYes(t *testing.T) {
	out := &bytes.Buffer{}
	rt := &ConfirmRuntime{
		In:    strings.NewReader("yes\n"),
		Out:   out,
		IsTTY: func() bool { return true },
	}
	resolution, err := confirmInvoke(t, rt, confirmArgs{Summary: "Proceed?"})
	if err != nil {
		t.Fatal(err)
	}
	if resolution != "yes" {
		t.Errorf("resolution = %q want yes", resolution)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "Proceed?") {
		t.Errorf("summary not printed: %q", rendered)
	}
	if !strings.Contains(rendered, "[1] yes") || !strings.Contains(rendered, "[2] no") {
		t.Errorf("options not rendered: %q", rendered)
	}
}

func TestConfirm_TTYNumericIndex(t *testing.T) {
	rt := &ConfirmRuntime{
		In:    strings.NewReader("2\n"),
		Out:   &bytes.Buffer{},
		IsTTY: func() bool { return true },
	}
	out, err := confirmInvoke(t, rt, confirmArgs{Summary: "?", Options: []string{"go", "halt"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "halt" {
		t.Errorf("got %q want halt", out)
	}
}

func TestConfirm_TTYUniquePrefix(t *testing.T) {
	rt := &ConfirmRuntime{
		In:    strings.NewReader("h\n"),
		Out:   &bytes.Buffer{},
		IsTTY: func() bool { return true },
	}
	out, err := confirmInvoke(t, rt, confirmArgs{Summary: "?", Options: []string{"go", "halt"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "halt" {
		t.Errorf("got %q want halt", out)
	}
}

func TestConfirm_TTYAmbiguousPrefix(t *testing.T) {
	rt := &ConfirmRuntime{
		In:    strings.NewReader("ha\n"),
		Out:   &bytes.Buffer{},
		IsTTY: func() bool { return true },
	}
	_, err := confirmInvoke(t, rt, confirmArgs{Summary: "?", Options: []string{"halt", "halt-now"}})
	if err == nil {
		t.Fatal("ambiguous prefix should error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestConfirm_TTYUnknownAnswer(t *testing.T) {
	rt := &ConfirmRuntime{
		In:    strings.NewReader("maybe\n"),
		Out:   &bytes.Buffer{},
		IsTTY: func() bool { return true },
	}
	_, err := confirmInvoke(t, rt, confirmArgs{Summary: "?"})
	if err == nil {
		t.Fatal("unknown answer should error")
	}
}

func TestConfirm_TTYEmptyResponse(t *testing.T) {
	rt := &ConfirmRuntime{
		In:    strings.NewReader("\n"),
		Out:   &bytes.Buffer{},
		IsTTY: func() bool { return true },
	}
	_, err := confirmInvoke(t, rt, confirmArgs{Summary: "?"})
	if err == nil {
		t.Fatal("empty response should error")
	}
}

func TestConfirm_AutoConfirmInvalidMode(t *testing.T) {
	_, err := confirmInvoke(t, &ConfirmRuntime{AutoConfirm: AutoConfirmMode("bogus")},
		confirmArgs{Summary: "?"})
	if err == nil {
		t.Fatal("invalid mode should error")
	}
}

func TestAutoConfirmModeIsValid(t *testing.T) {
	for _, m := range []AutoConfirmMode{AutoConfirmUnset, AutoConfirmYes, AutoConfirmNo, AutoConfirmAbort} {
		if !m.IsValid() {
			t.Errorf("%q should be valid", m)
		}
	}
	if AutoConfirmMode("bogus").IsValid() {
		t.Error("bogus should be invalid")
	}
}

func TestParsePositiveInt(t *testing.T) {
	cases := []struct {
		in       string
		expected int
		ok       bool
	}{
		{"1", 1, true},
		{"42", 42, true},
		{"0", 0, true},
		{"", 0, false},
		{"-1", 0, false},
		{"1a", 0, false},
	}
	for _, c := range cases {
		got, ok := parsePositiveInt(c.in)
		if got != c.expected || ok != c.ok {
			t.Errorf("parsePositiveInt(%q) = (%d,%v) want (%d,%v)", c.in, got, ok, c.expected, c.ok)
		}
	}
}

func TestBuildHandlers_ConfirmRegistrationGated(t *testing.T) {
	wd := t.TempDir()
	// nil runtime → no Confirm tool
	handlers, _ := buildHandlersWithSkill(wd, nil, nil, nil, nil)
	if _, ok := handlers["Confirm"]; ok {
		t.Error("Confirm should not register when runtime is nil")
	}
	// Non-nil runtime → registered
	handlers, _ = buildHandlersWithSkill(wd, nil, nil, nil, &ConfirmRuntime{AutoConfirm: AutoConfirmAbort})
	if _, ok := handlers["Confirm"]; !ok {
		t.Error("Confirm should register when runtime is present")
	}
}
