package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/tmc/langchaingo/llms"
)

// repeatLoop drives toolLoop with n copies of one Bash call followed by the
// given replies, and returns the outcome plus how many times the call ran.
func repeatLoop(t *testing.T, repeatNudges int, command string, n int, after ...*llms.ContentResponse) (*scriptedLLM, bool, int, error) {
	t.Helper()
	dir := t.TempDir()
	handlers, _ := BuildHandlers(dir, nil, &executor.LocalExecutor{WorkingDir: dir})
	var responses []*llms.ContentResponse
	for i := 0; i < n; i++ {
		responses = append(responses, toolCallResp("c", "Bash", `{"command":"`+command+`"}`))
	}
	llm := &scriptedLLM{responses: append(responses, after...)}
	ctx := InitEditsOptional(context.Background())
	_, _, err, done := toolLoop(ctx, llm, buildInitialMessages("sys", "user", false), handlers, 50, RunWithToolsConfig{RepeatNudges: repeatNudges}, nil)
	ran := 0
	if data, readErr := os.ReadFile(filepath.Join(dir, "runs.txt")); readErr == nil {
		ran = strings.Count(string(data), "\n")
	}
	return llm, done, ran, err
}

// lastRequestText joins the text and tool output of the last request.
func lastRequestText(llm *scriptedLLM) string {
	var sb strings.Builder
	for _, msg := range llm.msgs[len(llm.msgs)-1] {
		sb.WriteString(messageText(msg))
		sb.WriteString("\n")
	}
	return sb.String()
}

// countingCommand appends a line per run and prints the count, so each run's
// output differs and only the same-call limit can stop it.
const countingCommand = `echo x >> runs.txt; wc -l < runs.txt`

func TestToolLoopRepeatNudgeOnTheSameCall(t *testing.T) {
	t.Parallel()

	t.Run("off by default: the limit ends the loop", func(t *testing.T) {
		t.Parallel()
		llm, done, ran, err := repeatLoop(t, 0, countingCommand, maxSameToolRepeat, textResp("moved on"))
		if done || err == nil {
			t.Fatalf("the loop must end at the limit, err=%v done=%v", err, done)
		}
		if ran != maxSameToolRepeat-1 || llm.calls != maxSameToolRepeat {
			t.Fatalf("ran %d times over %d model calls, want %d and %d", ran, llm.calls, maxSameToolRepeat-1, maxSameToolRepeat)
		}
	})

	t.Run("a nudged model is told the call did not run and goes on", func(t *testing.T) {
		t.Parallel()
		llm, done, ran, err := repeatLoop(t, 1, countingCommand, maxSameToolRepeat, textResp("moved on"))
		if err != nil || !done {
			t.Fatalf("the loop must go on and finish, err=%v done=%v", err, done)
		}
		if ran != maxSameToolRepeat-1 {
			t.Fatalf("the call ran %d times, want %d: the nudged call must not run", ran, maxSameToolRepeat-1)
		}
		if !strings.Contains(lastRequestText(llm), repeatNudgeMessage) {
			t.Fatal("the model must see the nudge as the call's result")
		}
	})

	t.Run("the nudges run out", func(t *testing.T) {
		t.Parallel()
		_, done, ran, err := repeatLoop(t, 1, countingCommand, 2*maxSameToolRepeat, textResp("moved on"))
		if done || err == nil {
			t.Fatalf("a second run of repeats must end the loop, err=%v done=%v", err, done)
		}
		if ran != 2*maxSameToolRepeat-2 {
			t.Fatalf("the call ran %d times, want %d", ran, 2*maxSameToolRepeat-2)
		}
	})
}

func TestToolLoopRepeatNudgeOnALoop(t *testing.T) {
	t.Parallel()
	// The same call with the same output: the loop detector trips at its
	// third repeat, well before the same-call limit.
	const command = `echo x >> runs.txt; echo same`
	tests := []struct {
		name     string
		nudges   int
		repeats  int
		after    []*llms.ContentResponse
		wantLoop bool   // the run ends with ErrLoopDetected
		wantRan  int    // times the command ran
		wantSeen string // text the last request must hold, if any
	}{
		{name: "off by default: a detected loop ends the run", repeats: loopMaxRepeats,
			wantLoop: true, wantRan: loopMaxRepeats},
		{name: "a nudged model is told to move on and goes on", nudges: 1, repeats: loopMaxRepeats,
			wantRan: loopMaxRepeats, wantSeen: loopNudgeMessage},
		{name: "the looping call is not run again after the nudge", nudges: 1, repeats: loopMaxRepeats + 1,
			wantRan: loopMaxRepeats, wantSeen: repeatNudgeMessage},
		{name: "resending the blocked call ends the loop", nudges: 2, repeats: 2 * loopMaxRepeats,
			wantLoop: true, wantRan: loopMaxRepeats},
		{name: "a different call lifts the block", nudges: 1, repeats: loopMaxRepeats,
			after: []*llms.ContentResponse{
				toolCallResp("d", "Bash", `{"command":"echo other"}`),
				toolCallResp("e", "Bash", `{"command":"`+command+`"}`),
			},
			wantRan: loopMaxRepeats + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			llm, done, ran, err := repeatLoop(t, tt.nudges, command, tt.repeats, append(tt.after, textResp("moved on"))...)
			checkLoopOutcome(t, llm, done, err, tt.wantLoop, tt.wantSeen)
			if ran != tt.wantRan {
				t.Fatalf("the call ran %d times, want %d", ran, tt.wantRan)
			}
		})
	}
}

// checkLoopOutcome checks how a repeat-nudge run ended and what the model saw.
func checkLoopOutcome(t *testing.T, llm *scriptedLLM, done bool, err error, wantLoop bool, wantSeen string) {
	t.Helper()
	if wantLoop {
		if !errors.Is(err, ErrLoopDetected) || done {
			t.Fatalf("err=%v done=%v, want ErrLoopDetected", err, done)
		}
		return
	}
	if err != nil || !done {
		t.Fatalf("the loop must go on and finish, err=%v done=%v", err, done)
	}
	if wantSeen != "" && !strings.Contains(lastRequestText(llm), wantSeen) {
		t.Fatalf("the last request must hold %q", wantSeen)
	}
}

func TestCallsKey(t *testing.T) {
	t.Parallel()
	call := func(name, args string) llms.ToolCall {
		return llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: name, Arguments: args}}
	}
	tests := []struct {
		name  string
		a, b  []llms.ToolCall
		equal bool
	}{
		{"same name and arguments", []llms.ToolCall{call("Bash", `{"command":"ls"}`)}, []llms.ToolCall{call("Bash", `{"command":"ls"}`)}, true},
		{"other arguments", []llms.ToolCall{call("Bash", `{"command":"ls"}`)}, []llms.ToolCall{call("Bash", `{"command":"pwd"}`)}, false},
		{"other tool", []llms.ToolCall{call("Bash", `{}`)}, []llms.ToolCall{call("Read", `{}`)}, false},
		{"one call of a batch differs", []llms.ToolCall{call("Bash", `a`), call("Bash", `b`)}, []llms.ToolCall{call("Bash", `a`), call("Bash", `c`)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := callsKey(tt.a) == callsKey(tt.b); got != tt.equal {
				t.Fatalf("callsKey equal = %v, want %v", got, tt.equal)
			}
		})
	}
	if callsKey([]llms.ToolCall{{}}) != "" {
		t.Fatal("a call without a function call must give an empty key")
	}
}
