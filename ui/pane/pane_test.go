package pane

import "testing"

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
