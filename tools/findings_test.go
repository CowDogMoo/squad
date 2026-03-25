package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFindingsStore(t *testing.T) {
	t.Parallel()
	store := NewFindingsStore()

	if store.Count() != 0 {
		t.Fatalf("Count() = %d, want 0", store.Count())
	}

	store.Add(Finding{
		Title:       "SQL Injection",
		Severity:    "critical",
		Category:    "vulnerability",
		Description: "Unsanitized input in query builder.",
		Agent:       "security-audit",
	})

	store.Add(Finding{
		Title:       "Weak Permissions",
		Severity:    "medium",
		Description: "Config file readable by all users.",
		Agent:       "config-review",
	})

	if store.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", store.Count())
	}

	findings := store.All()
	if len(findings) != 2 {
		t.Fatalf("All() len = %d, want 2", len(findings))
	}
	if findings[0].Title != "SQL Injection" {
		t.Fatalf("findings[0].Title = %q", findings[0].Title)
	}
}

func TestFindingsStoreFormatMarkdown(t *testing.T) {
	t.Parallel()
	store := NewFindingsStore()

	store.Add(Finding{Title: "Low Finding", Severity: "low", Description: "Minor issue."})
	store.Add(Finding{Title: "Critical Finding", Severity: "critical", Description: "Severe issue.", Evidence: "proof here"})
	store.Add(Finding{Title: "High Finding", Severity: "high", Description: "Important issue.", Category: "security"})

	md := store.FormatMarkdown()

	// Should be sorted: critical first, then high, then low.
	critIdx := strings.Index(md, "Critical Finding")
	highIdx := strings.Index(md, "High Finding")
	lowIdx := strings.Index(md, "Low Finding")

	if critIdx > highIdx || highIdx > lowIdx {
		t.Fatalf("findings not sorted by severity: critical=%d high=%d low=%d", critIdx, highIdx, lowIdx)
	}

	if !strings.Contains(md, "## Findings") {
		t.Fatalf("missing Findings header")
	}
	if !strings.Contains(md, "| CRITICAL | 1 |") {
		t.Fatalf("missing summary table entry for CRITICAL")
	}
	if !strings.Contains(md, "proof here") {
		t.Fatalf("missing evidence")
	}
	if !strings.Contains(md, "**Category:** security") {
		t.Fatalf("missing category")
	}
}

func TestFindingsStoreFormatMarkdownEmpty(t *testing.T) {
	t.Parallel()
	store := NewFindingsStore()
	md := store.FormatMarkdown()
	if !strings.Contains(md, "No findings") {
		t.Fatalf("expected 'No findings' for empty store, got: %s", md)
	}
}

func TestFindingsStoreFormatJSON(t *testing.T) {
	t.Parallel()
	store := NewFindingsStore()
	store.Add(Finding{Title: "Test", Severity: "info", Description: "Test finding."})

	jsonStr, err := store.FormatJSON()
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}

	var findings []Finding
	if err := json.Unmarshal([]byte(jsonStr), &findings); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(findings))
	}
}

func TestReportFindingTool(t *testing.T) {
	t.Parallel()
	store := NewFindingsStore()
	tool := reportFindingTool(store, "test-agent")

	payload, _ := json.Marshal(map[string]string{
		"title":       "Open Port",
		"severity":    "high",
		"category":    "network",
		"description": "Port 22 is open.",
		"evidence":    "nmap output: 22/tcp open ssh",
	})

	output, err := tool(context.Background(), payload)
	if err != nil {
		t.Fatalf("reportFindingTool: %v", err)
	}

	if !strings.Contains(output, "Finding recorded") {
		t.Fatalf("unexpected output: %s", output)
	}
	if !strings.Contains(output, "total findings: 1") {
		t.Fatalf("unexpected count in output: %s", output)
	}

	findings := store.All()
	if len(findings) != 1 {
		t.Fatalf("store has %d findings, want 1", len(findings))
	}
	if findings[0].Agent != "test-agent" {
		t.Fatalf("agent = %q, want test-agent", findings[0].Agent)
	}
}

func TestReportFindingToolErrors(t *testing.T) {
	t.Parallel()
	store := NewFindingsStore()
	tool := reportFindingTool(store, "test")

	tests := []struct {
		name         string
		payload      []byte
		wantContains string
	}{
		{"invalid json", []byte("{"), "invalid ReportFinding args"},
		{"missing title", mustJSON(t, map[string]string{"severity": "high", "description": "x"}), "title is required"},
		{"missing severity", mustJSON(t, map[string]string{"title": "x", "description": "x"}), "severity is required"},
		{"missing description", mustJSON(t, map[string]string{"title": "x", "severity": "high"}), "description is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool(context.Background(), tt.payload)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantContains)
			}
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}
