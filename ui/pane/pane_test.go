package pane

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestClassifyKind(t *testing.T) {
	cases := []struct {
		raw     string
		want    Kind
		payload string
	}{
		{"", KindPrompt, ""},
		{"   ", KindPrompt, ""},
		{"hello world", KindPrompt, "hello world"},
		{"  hello world  ", KindPrompt, "hello world"},
		{"/new agent", KindCommand, "new agent"},
		{"  /preset save  ", KindCommand, "preset save"},
		{"/", KindCommand, ""},
		{"!ls -la", KindShell, "ls -la"},
		{"! git status", KindShell, "git status"},
		{"@README.md", KindFile, "README.md"},
		{"@ tools/edit.go", KindFile, "tools/edit.go"},
		{"//not-a-command", KindCommand, "/not-a-command"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			gotKind, gotPayload := ClassifyKind(tc.raw)
			if gotKind != tc.want {
				t.Errorf("kind: got %v, want %v", gotKind, tc.want)
			}
			if gotPayload != tc.payload {
				t.Errorf("payload: got %q, want %q", gotPayload, tc.payload)
			}
		})
	}
}

func TestKindString(t *testing.T) {
	cases := map[Kind]string{
		KindPrompt:  "prompt",
		KindCommand: "command",
		KindShell:   "shell",
		KindFile:    "file",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestAsSubmitted(t *testing.T) {
	// Happy path unwrap
	var msg tea.Msg = Submitted{Kind: KindCommand, Text: "preset save"}
	s, ok := AsSubmitted(msg)
	if !ok {
		t.Fatal("AsSubmitted should return ok=true for Submitted msg")
	}
	if s.Kind != KindCommand || s.Text != "preset save" {
		t.Fatalf("AsSubmitted returned %+v", s)
	}
	// Non-matching type
	if _, ok := AsSubmitted(tea.Msg("not-submitted")); ok {
		t.Fatal("AsSubmitted should return ok=false for other msg types")
	}
}

func TestAsLaunchRequest(t *testing.T) {
	var msg tea.Msg = LaunchRequest{Agent: "go-tests", WorkingDir: ".", Prompt: "Begin."}
	r, ok := AsLaunchRequest(msg)
	if !ok {
		t.Fatal("AsLaunchRequest should return ok=true for LaunchRequest msg")
	}
	if r.Agent != "go-tests" || r.Prompt != "Begin." {
		t.Fatalf("AsLaunchRequest returned %+v", r)
	}
	if _, ok := AsLaunchRequest(tea.Msg("other")); ok {
		t.Fatal("AsLaunchRequest should return ok=false for other msg types")
	}
}

func TestTrimHelpers(t *testing.T) {
	cases := []struct {
		in   string
		left string
		both string
	}{
		{"", "", ""},
		{"  \t\n", "", ""},
		{"  a ", "a ", "a"},
		{"a\n\r\t ", "a\n\r\t ", "a"},
		{"  abc  ", "abc  ", "abc"},
	}
	for _, c := range cases {
		if got := trimLeftSpace(c.in); got != c.left {
			t.Errorf("trimLeftSpace(%q) = %q, want %q", c.in, got, c.left)
		}
		if got := trimSpace(c.in); got != c.both {
			t.Errorf("trimSpace(%q) = %q, want %q", c.in, got, c.both)
		}
	}
}
