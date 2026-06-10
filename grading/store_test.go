package grading

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SaveAndList(t *testing.T) {
	// Create temp file for test store
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "grades.json")
	store := NewStoreAt(storePath)

	// Save a grade
	result := &GradeResult{
		Agent:               "go-review",
		Timestamp:           time.Now(),
		Grade:               "A",
		TotalScore:          95,
		ReportQuality:       100,
		IterationEfficiency: 90,
		Iterations:          12,
		FileCount:           15,
	}

	if err := store.Save(result); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		t.Error("store file was not created")
	}

	// List grades
	grades, err := store.List("go-review", 10)
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}

	if len(grades) != 1 {
		t.Errorf("expected 1 grade, got %d", len(grades))
	}

	if grades[0].Grade != "A" {
		t.Errorf("expected grade A, got %s", grades[0].Grade)
	}
}

func TestStore_ListFiltersAgent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreAt(filepath.Join(tmpDir, "grades.json"))

	// Save grades for different agents
	agents := []string{"go-review", "python-review", "go-review"}
	for i, agent := range agents {
		_ = store.Save(&GradeResult{
			Agent:     agent,
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
			Grade:     "A",
		})
	}

	// List only go-review
	grades, err := store.List("go-review", 10)
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}

	if len(grades) != 2 {
		t.Errorf("expected 2 go-review grades, got %d", len(grades))
	}

	// List all
	allGrades, err := store.ListAll(10)
	if err != nil {
		t.Fatalf("failed to list all: %v", err)
	}

	if len(allGrades) != 3 {
		t.Errorf("expected 3 total grades, got %d", len(allGrades))
	}
}

func TestStore_ListSortsByTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreAt(filepath.Join(tmpDir, "grades.json"))

	// Save grades with different timestamps (older first)
	for i := 0; i < 3; i++ {
		_ = store.Save(&GradeResult{
			Agent:     "go-review",
			Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
			RunID:     string(rune('A' + i)),
		})
	}

	grades, _ := store.List("", 10)

	// Should be sorted newest first
	for i := 1; i < len(grades); i++ {
		if grades[i].Timestamp.After(grades[i-1].Timestamp) {
			t.Error("grades should be sorted by timestamp descending")
		}
	}
}

func TestStore_ListWithLimit(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreAt(filepath.Join(tmpDir, "grades.json"))

	// Save 5 grades
	for i := 0; i < 5; i++ {
		_ = store.Save(&GradeResult{
			Agent:     "go-review",
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	// List with limit 2
	grades, _ := store.List("", 2)
	if len(grades) != 2 {
		t.Errorf("expected 2 grades with limit, got %d", len(grades))
	}
}

func TestStore_Stats(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreAt(filepath.Join(tmpDir, "grades.json"))

	// Save grades with different scores
	testGrades := []struct {
		grade      string
		score      float64
		report     float64
		efficiency float64
	}{
		{"A+", 100, 100, 100},
		{"A", 95, 100, 90},
		{"B+", 88, 75, 100},
	}

	for i, tg := range testGrades {
		_ = store.Save(&GradeResult{
			Agent:               "go-review",
			Timestamp:           time.Now().Add(time.Duration(i) * time.Minute),
			Grade:               tg.grade,
			TotalScore:          tg.score,
			ReportQuality:       tg.report,
			IterationEfficiency: tg.efficiency,
		})
	}

	stats, err := store.Stats("go-review")
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.TotalRuns != 3 {
		t.Errorf("expected 3 total runs, got %d", stats.TotalRuns)
	}

	if stats.LatestGrade != "B+" {
		t.Errorf("expected latest grade B+, got %s", stats.LatestGrade)
	}

	expectedAvg := (100.0 + 95.0 + 88.0) / 3.0
	if stats.AvgScore < expectedAvg-0.1 || stats.AvgScore > expectedAvg+0.1 {
		t.Errorf("expected avg score %.1f, got %.1f", expectedAvg, stats.AvgScore)
	}

	if stats.GradeCounts["A+"] != 1 {
		t.Errorf("expected 1 A+ grade, got %d", stats.GradeCounts["A+"])
	}
}

func TestStore_StatsNoGrades(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreAt(filepath.Join(tmpDir, "grades.json"))

	_, err := store.Stats("nonexistent")
	if err == nil {
		t.Error("expected error for agent with no grades")
	}
}

func TestStore_Path(t *testing.T) {
	store := NewStoreAt("/tmp/test.json")
	if store.Path() != "/tmp/test.json" {
		t.Errorf("expected path /tmp/test.json, got %s", store.Path())
	}
}

func TestStore_LoadCorruptJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "grades.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStoreAt(path)
	_, err := store.List("", 10)
	if err == nil {
		t.Error("expected error loading corrupt JSON")
	}
}

func TestStore_WriteAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "grades.json"))
	now := time.Now().UTC().Truncate(time.Second)
	r := &GradeResult{
		Agent:               "test-agent",
		Timestamp:           now,
		Grade:               "B",
		TotalScore:          80,
		ReportQuality:       75,
		IterationEfficiency: 85,
		RunID:               "run-001",
	}
	if err := store.Save(r); err != nil {
		t.Fatalf("Save: %v", err)
	}
	store2 := NewStoreAt(filepath.Join(dir, "grades.json"))
	grades, err := store2.List("test-agent", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(grades) != 1 {
		t.Fatalf("expected 1 grade, got %d", len(grades))
	}
	if grades[0].Grade != "B" {
		t.Errorf("grade: got %q want %q", grades[0].Grade, "B")
	}
	if grades[0].RunID != "run-001" {
		t.Errorf("RunID: got %q want %q", grades[0].RunID, "run-001")
	}
}

func TestStore_LoadMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "nonexistent.json"))
	grades, err := store.List("", 0)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(grades) != 0 {
		t.Errorf("expected empty list, got %d", len(grades))
	}
}

func TestStore_WriteFailsOnUnwritablePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Use a directory as the path — os.WriteFile will fail.
	store := NewStoreAt(dir)
	err := store.Save(&GradeResult{Agent: "x", Timestamp: time.Now()})
	if err == nil {
		t.Error("expected error writing to a directory path")
	}
}

func TestNewStore_DefaultPath(t *testing.T) {
	// NewStore uses os.UserCacheDir — verify it returns a non-nil store.
	store, err := NewStore()
	if err != nil {
		t.Skipf("UserCacheDir unavailable: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.Path() == "" {
		t.Error("expected non-empty path")
	}
}
