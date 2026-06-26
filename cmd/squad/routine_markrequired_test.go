package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestMustMarkRequiredSuccess(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().String("agent", "", "")
	cmd.Flags().String("schedule", "", "")

	mustMarkRequired(cmd, "agent", "schedule")

	for _, name := range []string{"agent", "schedule"} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("flag %q missing", name)
		}
		if _, ok := flag.Annotations[cobra.BashCompOneRequiredFlag]; !ok {
			t.Fatalf("flag %q not marked required", name)
		}
	}
}

func TestMustMarkRequiredPanicsOnUnknownFlag(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unknown flag name")
		}
	}()

	mustMarkRequired(&cobra.Command{Use: "x"}, "nope")
}

func TestNewRoutineCreateCmdMarksRequired(t *testing.T) {
	cmd := newRoutineCreateCmd()
	for _, name := range []string{"agent", "schedule"} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("flag %q missing", name)
		}
		if _, ok := flag.Annotations[cobra.BashCompOneRequiredFlag]; !ok {
			t.Fatalf("flag %q not marked required", name)
		}
	}
}
