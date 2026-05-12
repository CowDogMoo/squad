package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverMissingRoot(t *testing.T) {
	tailers, err := Discover("/does/not/exist/squad/sessions")
	if err != nil {
		t.Errorf("missing root should not error, got %v", err)
	}
	if tailers != nil {
		t.Errorf("missing root should return nil, got %d tailers", len(tailers))
	}
}

func TestDiscoverSortsNewestFirst(t *testing.T) {
	root := t.TempDir()
	// Squad session IDs are timestamped — string-descending puts newest first.
	for _, name := range []string{
		"20260101T000000Z-aaaa",
		"20260301T000000Z-cccc",
		"20260201T000000Z-bbbb",
	} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Also drop a non-directory entry to verify it's skipped.
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	tailers, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tailers) != 3 {
		t.Fatalf("want 3 tailers, got %d", len(tailers))
	}
	want := []string{
		filepath.Join(root, "20260301T000000Z-cccc"),
		filepath.Join(root, "20260201T000000Z-bbbb"),
		filepath.Join(root, "20260101T000000Z-aaaa"),
	}
	for i, w := range want {
		if tailers[i].Dir() != w {
			t.Errorf("tailers[%d].Dir() = %q, want %q", i, tailers[i].Dir(), w)
		}
	}
}
