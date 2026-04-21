package tools

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

var update = flag.Bool("update", false, "update golden files")

// goldenTest compares actual output against a golden file. If -update is set,
// it writes the actual output as the new golden file.
func goldenTest(t *testing.T, name, actual string) {
	t.Helper()
	goldenPath := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden file %s not found (run with -update to create): %v", goldenPath, err)
	}
	if strings.TrimRight(string(expected), "\n") != strings.TrimRight(actual, "\n") {
		t.Fatalf("golden mismatch for %s:\n--- expected ---\n%s\n--- actual ---\n%s",
			goldenPath, string(expected), actual)
	}
}

func TestGolden_MultiEditResult(t *testing.T) {
	t.Parallel()
	result := formatMultiEditResult("src/main.go", 3, []FailedEdit{
		{Index: 2, Old: "nonexistent text", Error: "text not found"},
		{Index: 5, Old: "", Error: "old string is empty"},
	})
	goldenTest(t, "multiedit_result", result)
}

func TestGolden_TruncateToLines(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		sb.WriteString("line ")
		sb.WriteString(string(rune('0' + i%10)))
		sb.WriteString("\n")
	}
	result := TruncateToLines(sb.String(), 5, 3)
	goldenTest(t, "truncate_to_lines", result)
}

func TestGolden_CompactionSummary(t *testing.T) {
	t.Parallel()
	messages := buildCompactionTestMessages()
	result := CompactionSummary(messages, nil)
	goldenTest(t, "compaction_summary", result)
}

func buildCompactionTestMessages() []llms.MessageContent {
	return []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: "1", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"main.go"}`}},
				llms.ToolCall{ID: "2", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"config.go"}`}},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: "3", FunctionCall: &llms.FunctionCall{Name: "Grep", Arguments: `{"pattern":"TODO"}`}},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: "4", FunctionCall: &llms.FunctionCall{Name: "Edit", Arguments: `{"path":"main.go","old":"foo","new":"bar"}`}},
				llms.ToolCall{ID: "5", FunctionCall: &llms.FunctionCall{Name: "Edit", Arguments: `{"path":"main.go","old":"baz","new":"qux"}`}},
			},
		},
		{
			Role: llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{
				llms.ToolCall{ID: "6", FunctionCall: &llms.FunctionCall{Name: "Bash", Arguments: `{"command":"go test ./..."}`}},
			},
		},
	}
}

func TestGolden_CommandSafety(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	testCmds := []string{
		"ls -la",
		"git status",
		"sudo rm -rf /",
		"go test ./...",
		"nmap 192.168.1.0/24",
		"echo hello",
		"pip install requests",
	}
	for _, cmd := range testCmds {
		safe := IsSafeCommand(cmd)
		blocked, reason := IsBlockedCommand(cmd)
		sb.WriteString(cmd)
		sb.WriteString(" → safe=")
		if safe {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
		sb.WriteString(" blocked=")
		if blocked {
			sb.WriteString("true (")
			sb.WriteString(reason)
			sb.WriteString(")")
		} else {
			sb.WriteString("false")
		}
		sb.WriteString("\n")
	}
	goldenTest(t, "cmd_safety", sb.String())
}
