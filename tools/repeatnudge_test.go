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

	t.Run("off by default: a detected loop ends the run", func(t *testing.T) {
		t.Parallel()
		_, done, ran, err := repeatLoop(t, 0, command, loopMaxRepeats, textResp("moved on"))
		if !errors.Is(err, ErrLoopDetected) || done {
			t.Fatalf("err=%v done=%v, want ErrLoopDetected", err, done)
		}
		if ran != loopMaxRepeats {
			t.Fatalf("ran %d times, want %d", ran, loopMaxRepeats)
		}
	})

	t.Run("a nudged model is told to move on and goes on", func(t *testing.T) {
		t.Parallel()
		llm, done, _, err := repeatLoop(t, 1, command, loopMaxRepeats, textResp("moved on"))
		if err != nil || !done {
			t.Fatalf("the loop must go on and finish, err=%v done=%v", err, done)
		}
		if !strings.Contains(lastRequestText(llm), repeatNudgeMessage) {
			t.Fatal("the model must see the nudge")
		}
	})
}
