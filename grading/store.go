package grading

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Store persists grade results to disk.
type Store struct {
	path string
}

// GradeHistory holds all stored grades.
type GradeHistory struct {
	Grades []*GradeResult `json:"grades"`
}

// NewStore creates a store at the default location (~/.cache/squad/grades.json).
func NewStore() (*Store, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get cache directory: %w", err)
	}

	squadCache := filepath.Join(cacheDir, "squad")
	if err := os.MkdirAll(squadCache, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &Store{
		path: filepath.Join(squadCache, "grades.json"),
	}, nil
}

// NewStoreAt creates a store at a specific path.
func NewStoreAt(path string) *Store {
	return &Store{path: path}
}

// Save appends a grade result to the store.
func (s *Store) Save(result *GradeResult) error {
	history, err := s.load()
	if err != nil {
		history = &GradeHistory{}
	}

	history.Grades = append(history.Grades, result)

	return s.write(history)
}

// List returns grades for a specific agent, sorted by timestamp (newest first).
func (s *Store) List(agentName string, limit int) ([]*GradeResult, error) {
	history, err := s.load()
	if err != nil {
		return nil, err
	}

	var filtered []*GradeResult
	for _, g := range history.Grades {
		if agentName == "" || g.Agent == agentName {
			filtered = append(filtered, g)
		}
	}

	// Sort by timestamp descending
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered, nil
}

// ListAll returns all grades, sorted by timestamp (newest first).
func (s *Store) ListAll(limit int) ([]*GradeResult, error) {
	return s.List("", limit)
}

// Stats returns aggregate statistics for an agent.
func (s *Store) Stats(agentName string) (*AgentStats, error) {
	grades, err := s.List(agentName, 0)
	if err != nil {
		return nil, err
	}

	if len(grades) == 0 {
		return nil, fmt.Errorf("no grades found for agent %q", agentName)
	}

	stats := &AgentStats{
		Agent:       agentName,
		TotalRuns:   len(grades),
		GradeCounts: make(map[string]int),
	}

	var totalScore, totalReport, totalEfficiency float64
	for _, g := range grades {
		stats.GradeCounts[g.Grade]++
		totalScore += g.TotalScore
		totalReport += g.ReportQuality
		totalEfficiency += g.IterationEfficiency
	}

	n := float64(len(grades))
	stats.AvgScore = totalScore / n
	stats.AvgReportQuality = totalReport / n
	stats.AvgIterationEfficiency = totalEfficiency / n
	stats.LatestGrade = grades[0].Grade

	return stats, nil
}

// AgentStats provides aggregate statistics for an agent.
type AgentStats struct {
	Agent                  string         `json:"agent"`
	TotalRuns              int            `json:"total_runs"`
	LatestGrade            string         `json:"latest_grade"`
	AvgScore               float64        `json:"avg_score"`
	AvgReportQuality       float64        `json:"avg_report_quality"`
	AvgIterationEfficiency float64        `json:"avg_iteration_efficiency"`
	GradeCounts            map[string]int `json:"grade_counts"`
}

func (s *Store) load() (*GradeHistory, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return &GradeHistory{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read grades file: %w", err)
	}

	var history GradeHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("failed to parse grades file: %w", err)
	}

	return &history, nil
}

func (s *Store) write(history *GradeHistory) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal grades: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write grades file: %w", err)
	}

	return nil
}

// Path returns the store's file path.
func (s *Store) Path() string {
	return s.path
}
