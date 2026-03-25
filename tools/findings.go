package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

// Finding represents a single structured finding reported by an agent.
type Finding struct {
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description"`
	Evidence    string `json:"evidence,omitempty"`
	Agent       string `json:"agent,omitempty"`
}

// FindingsStore collects findings from multiple agents in a thread-safe way.
type FindingsStore struct {
	mu       sync.RWMutex
	findings []Finding
}

// NewFindingsStore creates an empty findings store.
func NewFindingsStore() *FindingsStore {
	return &FindingsStore{}
}

// Add appends a finding to the store.
func (s *FindingsStore) Add(f Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.findings = append(s.findings, f)
}

// All returns a copy of all findings.
func (s *FindingsStore) All() []Finding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Finding, len(s.findings))
	copy(result, s.findings)
	return result
}

// Count returns the number of findings.
func (s *FindingsStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.findings)
}

// FormatMarkdown returns a markdown-formatted findings report.
func (s *FindingsStore) FormatMarkdown() string {
	findings := s.All()
	if len(findings) == 0 {
		return "No findings reported.\n"
	}

	// Sort by severity: critical > high > medium > low > info.
	severityOrder := map[string]int{
		"critical": 0,
		"high":     1,
		"medium":   2,
		"low":      3,
		"info":     4,
	}
	sort.Slice(findings, func(i, j int) bool {
		si := severityOrder[strings.ToLower(findings[i].Severity)]
		sj := severityOrder[strings.ToLower(findings[j].Severity)]
		if si != sj {
			return si < sj
		}
		return findings[i].Title < findings[j].Title
	})

	var sb strings.Builder
	sb.WriteString("## Findings\n\n")

	// Summary table.
	counts := map[string]int{}
	for _, f := range findings {
		counts[strings.ToLower(f.Severity)]++
	}
	sb.WriteString("| Severity | Count |\n|----------|-------|\n")
	for _, sev := range []string{"critical", "high", "medium", "low", "info"} {
		if c := counts[sev]; c > 0 {
			fmt.Fprintf(&sb, "| %s | %d |\n", strings.ToUpper(sev), c)
		}
	}
	sb.WriteString("\n")

	// Detail sections.
	for i, f := range findings {
		fmt.Fprintf(&sb, "### %d. [%s] %s\n\n", i+1, strings.ToUpper(f.Severity), f.Title)
		if f.Category != "" {
			fmt.Fprintf(&sb, "**Category:** %s\n\n", f.Category)
		}
		if f.Agent != "" {
			fmt.Fprintf(&sb, "**Reported by:** %s\n\n", f.Agent)
		}
		sb.WriteString(f.Description)
		sb.WriteString("\n")
		if f.Evidence != "" {
			sb.WriteString("\n**Evidence:**\n```\n")
			sb.WriteString(f.Evidence)
			sb.WriteString("\n```\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatJSON returns JSON-formatted findings.
func (s *FindingsStore) FormatJSON() (string, error) {
	findings := s.All()
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func definitionReportFinding() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "ReportFinding",
			Description: "Report a structured finding to the shared findings store. Findings are aggregated across all agents into the final pipeline report. Use this instead of just printing findings in your output.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":       map[string]any{"type": "string", "description": "Short title of the finding."},
					"severity":    map[string]any{"type": "string", "enum": []string{"critical", "high", "medium", "low", "info"}, "description": "Severity level."},
					"category":    map[string]any{"type": "string", "description": "Category (e.g., vulnerability, misconfiguration, information)."},
					"description": map[string]any{"type": "string", "description": "Detailed description of the finding."},
					"evidence":    map[string]any{"type": "string", "description": "Supporting evidence (command output, file contents, etc.)."},
				},
				"required": []string{"title", "severity", "description"},
			},
		},
	}
}

type reportFindingArgs struct {
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
}

func reportFindingTool(store *FindingsStore, agentName string) func(ctx context.Context, rawArgs []byte) (string, error) {
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var args reportFindingArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("invalid ReportFinding args: %w", err)
		}
		if args.Title == "" {
			return "", fmt.Errorf("title is required")
		}
		if args.Severity == "" {
			return "", fmt.Errorf("severity is required")
		}
		if args.Description == "" {
			return "", fmt.Errorf("description is required")
		}

		finding := Finding{
			Title:       args.Title,
			Severity:    args.Severity,
			Category:    args.Category,
			Description: args.Description,
			Evidence:    args.Evidence,
			Agent:       agentName,
		}
		store.Add(finding)
		logging.InfoContext(ctx, "finding reported: [%s] %s (agent=%s)", args.Severity, args.Title, agentName)
		return fmt.Sprintf("Finding recorded: [%s] %s (total findings: %d)", args.Severity, args.Title, store.Count()), nil
	}
}
