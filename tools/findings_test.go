package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestFindingsStoreBasics(t *testing.T) {
	tests := []struct {
		name    string
		add     []Finding
		wantLen int
		checkFn func(t *testing.T, s *FindingsStore)
	}{
		{
			name:    "new store is empty",
			add:     nil,
			wantLen: 0,
		},
		{
			name: "add two findings",
			add: []Finding{
				{Title: "SQL Injection", Severity: "high", Description: "desc"},
				{Title: "XSS", Severity: "medium", Description: "desc2"},
			},
			wantLen: 2,
		},
		{
			name: "All returns a copy",
			add: []Finding{
				{Title: "A", Severity: "high", Description: "d1"},
				{Title: "B", Severity: "low", Description: "d2"},
			},
			wantLen: 2,
			checkFn: func(t *testing.T, s *FindingsStore) {
				all := s.All()
				all[0].Title = "mutated"
				if s.All()[0].Title != "A" {
					t.Error("All() should return a copy, not a reference")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewFindingsStore()
			for _, f := range tt.add {
				s.Add(f)
			}
			if s.Count() != tt.wantLen {
				t.Errorf("Count() = %d, want %d", s.Count(), tt.wantLen)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, s)
			}
		})
	}
}

func TestFindingsStoreFormatMarkdown(t *testing.T) {
	tests := []struct {
		name         string
		findings     []Finding
		wantContains []string
		checkOrder   func(t *testing.T, md string)
	}{
		{
			name:         "empty store",
			findings:     nil,
			wantContains: []string{"No findings"},
		},
		{
			name: "severity sorting and summary table",
			findings: []Finding{
				{Title: "Low Finding", Severity: "low", Description: "Minor issue."},
				{Title: "Critical Finding", Severity: "critical", Description: "Severe issue.", Evidence: "proof here"},
				{Title: "High Finding", Severity: "high", Description: "Important issue.", Category: "security"},
			},
			wantContains: []string{
				"## Findings",
				"| CRITICAL | 1 |",
				"proof here",
				"**Category:** security",
			},
			checkOrder: func(t *testing.T, md string) {
				critIdx := strings.Index(md, "Critical Finding")
				highIdx := strings.Index(md, "High Finding")
				lowIdx := strings.Index(md, "Low Finding")
				if critIdx == -1 || highIdx == -1 || lowIdx == -1 {
					t.Fatalf("missing finding in markdown output:\n%s", md)
				}
				if critIdx > highIdx || highIdx > lowIdx {
					t.Errorf("findings not sorted by severity: critical=%d high=%d low=%d", critIdx, highIdx, lowIdx)
				}
			},
		},
		{
			name: "agent attribution",
			findings: []Finding{
				{Title: "Open Port", Severity: "high", Description: "Port 22 open.", Agent: "net-scanner"},
			},
			wantContains: []string{"**Reported by:** net-scanner"},
		},
		{
			name: "same-severity alphabetical sorting",
			findings: []Finding{
				{Title: "Zulu Issue", Severity: "high", Description: "z"},
				{Title: "Alpha Issue", Severity: "high", Description: "a"},
				{Title: "Mike Issue", Severity: "high", Description: "m"},
			},
			checkOrder: func(t *testing.T, md string) {
				alphaIdx := strings.Index(md, "Alpha Issue")
				mikeIdx := strings.Index(md, "Mike Issue")
				zuluIdx := strings.Index(md, "Zulu Issue")
				if alphaIdx > mikeIdx || mikeIdx > zuluIdx {
					t.Errorf("same-severity findings not sorted alphabetically: alpha=%d mike=%d zulu=%d", alphaIdx, mikeIdx, zuluIdx)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewFindingsStore()
			for _, f := range tt.findings {
				s.Add(f)
			}
			md := s.FormatMarkdown()
			for _, want := range tt.wantContains {
				if !strings.Contains(md, want) {
					t.Errorf("FormatMarkdown() missing %q in:\n%s", want, md)
				}
			}
			if tt.checkOrder != nil {
				tt.checkOrder(t, md)
			}
		})
	}
}

func TestFindingsStoreFormatJSON(t *testing.T) {
	tests := []struct {
		name         string
		findings     []Finding
		wantContains []string
		checkFn      func(t *testing.T, jsonStr string)
	}{
		{
			name: "single finding",
			findings: []Finding{
				{Title: "XSS", Severity: "medium", Description: "Cross-site scripting"},
			},
			wantContains: []string{"XSS", "medium"},
			checkFn: func(t *testing.T, jsonStr string) {
				if !strings.HasPrefix(strings.TrimSpace(jsonStr), "[") {
					t.Errorf("FormatJSON() should return JSON array, got: %q", jsonStr[:min(50, len(jsonStr))])
				}
			},
		},
		{
			name:     "empty store",
			findings: nil,
			checkFn: func(t *testing.T, jsonStr string) {
				trimmed := strings.TrimSpace(jsonStr)
				if trimmed != "[]" && trimmed != "null" && trimmed != "" {
					t.Errorf("FormatJSON() for empty store = %q, want [] or null", trimmed)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewFindingsStore()
			for _, f := range tt.findings {
				s.Add(f)
			}
			jsonStr, err := s.FormatJSON()
			if err != nil {
				t.Fatalf("FormatJSON() returned error: %v", err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(jsonStr, want) {
					t.Errorf("FormatJSON() missing %q", want)
				}
			}
			if tt.checkFn != nil {
				tt.checkFn(t, jsonStr)
			}
		})
	}
}

func TestFindingsStoreConcurrentAdd(t *testing.T) {
	s := NewFindingsStore()
	const goroutines = 20
	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			s.Add(Finding{
				Title:       fmt.Sprintf("Finding-%d", n),
				Severity:    "info",
				Description: "concurrent",
			})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
	if s.Count() != goroutines {
		t.Errorf("Count() = %d, want %d", s.Count(), goroutines)
	}
	if len(s.All()) != goroutines {
		t.Errorf("All() len = %d, want %d", len(s.All()), goroutines)
	}
}

func TestReportFindingTool(t *testing.T) {
	tests := []struct {
		name           string
		agentName      string
		payload        map[string]string
		wantOutputHas  []string
		wantCount      int
		wantAgentField string
	}{
		{
			name:      "valid finding",
			agentName: "test-agent",
			payload: map[string]string{
				"title":       "Open Port",
				"severity":    "high",
				"category":    "network",
				"description": "Port 22 is open.",
				"evidence":    "nmap output: 22/tcp open ssh",
			},
			wantOutputHas:  []string{"Finding recorded", "total findings: 1"},
			wantCount:      1,
			wantAgentField: "test-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewFindingsStore()
			tool := reportFindingTool(store, tt.agentName)

			data, _ := json.Marshal(tt.payload)
			output, err := tool(context.Background(), data)
			if err != nil {
				t.Fatalf("reportFindingTool: %v", err)
			}
			for _, want := range tt.wantOutputHas {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q, got: %s", want, output)
				}
			}
			if store.Count() != tt.wantCount {
				t.Errorf("store.Count() = %d, want %d", store.Count(), tt.wantCount)
			}
			if tt.wantAgentField != "" {
				findings := store.All()
				if len(findings) > 0 && findings[0].Agent != tt.wantAgentField {
					t.Errorf("agent = %q, want %q", findings[0].Agent, tt.wantAgentField)
				}
			}
		})
	}
}

func TestReportFindingToolErrors(t *testing.T) {
	store := NewFindingsStore()
	tool := reportFindingTool(store, "test")

	mustJSON := func(v any) []byte {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		return data
	}

	tests := []struct {
		name         string
		payload      []byte
		wantContains string
	}{
		{"invalid json", []byte("{"), "invalid ReportFinding args"},
		{"missing title", mustJSON(map[string]string{"severity": "high", "description": "x"}), "title is required"},
		{"missing severity", mustJSON(map[string]string{"title": "x", "description": "x"}), "severity is required"},
		{"missing description", mustJSON(map[string]string{"title": "x", "severity": "high"}), "description is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool(context.Background(), tt.payload)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantContains)
			}
		})
	}
}
