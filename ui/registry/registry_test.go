package registry

import (
	"testing"
	"time"
)

func TestLaunchEmptyArgs(t *testing.T) {
	r := New()
	if _, err := r.Launch("", nil); err == nil {
		t.Error("expected error for empty args")
	}
}

func TestLaunchAndReap(t *testing.T) {
	r := New()
	// Use /bin/true (or fallback to /usr/bin/true) — exits 0 immediately.
	lr, err := r.Launch(t.TempDir(), []string{"/usr/bin/true"})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if lr.Status != StatusRunning {
		t.Errorf("status: got %v, want StatusRunning", lr.Status)
	}
	// Wait for the reaper to run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := r.Get(lr.ID)
		if got.Status != StatusRunning {
			if got.Status != StatusExited {
				t.Errorf("status after exit: got %v, want StatusExited", got.Status)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("subprocess never exited (or reap goroutine never ran)")
}

func TestLaunchFailsForBadBinary(t *testing.T) {
	r := New()
	if _, err := r.Launch(t.TempDir(), []string{"/definitely/not/a/binary"}); err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestAllSortsNewestFirst(t *testing.T) {
	r := New()
	first, err := r.Launch(t.TempDir(), []string{"/usr/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	second, err := r.Launch(t.TempDir(), []string{"/usr/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	all := r.All()
	if len(all) != 2 {
		t.Fatalf("want 2 launches, got %d", len(all))
	}
	if all[0].ID != second.ID || all[1].ID != first.ID {
		t.Errorf("order: got [%s, %s], want [%s, %s]",
			all[0].ID, all[1].ID, second.ID, first.ID)
	}
}

func TestStopUnknownID(t *testing.T) {
	r := New()
	if err := r.Stop("L9999"); err == nil {
		t.Error("expected error stopping unknown launch")
	}
}

func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		StatusStarting: "starting",
		StatusRunning:  "running",
		StatusExited:   "exited",
		StatusFailed:   "failed",
		Status(99):     "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}
