package csync

import (
	"sync"
	"testing"
)

func TestValue_GetSet(t *testing.T) {
	t.Parallel()
	v := NewValue(42)
	if got := v.Get(); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	v.Set(100)
	if got := v.Get(); got != 100 {
		t.Fatalf("expected 100, got %d", got)
	}
}

func TestValue_Update(t *testing.T) {
	t.Parallel()
	v := NewValue(10)
	result := v.Update(func(n int) int { return n * 2 })
	if result != 20 {
		t.Fatalf("expected 20, got %d", result)
	}
	if got := v.Get(); got != 20 {
		t.Fatalf("expected 20 after update, got %d", got)
	}
}

func TestValue_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	v := NewValue(0)
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v.Update(func(n int) int { return n + 1 })
		}()
	}
	wg.Wait()
	if got := v.Get(); got != 100 {
		t.Fatalf("expected 100, got %d", got)
	}
}

func TestValue_StringType(t *testing.T) {
	t.Parallel()
	v := NewValue("hello")
	if got := v.Get(); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
	v.Set("world")
	if got := v.Get(); got != "world" {
		t.Fatalf("expected 'world', got %q", got)
	}
}

func TestValue_ZeroValue(t *testing.T) {
	t.Parallel()
	v := NewValue(0)
	if got := v.Get(); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}
