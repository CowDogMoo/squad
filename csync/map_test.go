package csync

import (
	"sync"
	"testing"
)

func TestMap_BasicOps(t *testing.T) {
	t.Parallel()
	m := NewMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	v, ok := m.Get("a")
	if !ok || v != 1 {
		t.Fatalf("expected (1, true), got (%d, %v)", v, ok)
	}

	if m.Len() != 2 {
		t.Fatalf("expected len 2, got %d", m.Len())
	}

	m.Del("a")
	_, ok = m.Get("a")
	if ok {
		t.Fatal("expected key to be deleted")
	}
}

func TestMap_GetOrSet(t *testing.T) {
	t.Parallel()
	m := NewMap[string, int]()

	v := m.GetOrSet("x", func() int { return 42 })
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}

	// Second call should return cached value.
	v = m.GetOrSet("x", func() int { return 99 })
	if v != 42 {
		t.Fatalf("expected 42 (cached), got %d", v)
	}
}

func TestMap_Copy(t *testing.T) {
	t.Parallel()
	m := NewMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)

	cp := m.Copy()
	if len(cp) != 2 || cp["a"] != 1 || cp["b"] != 2 {
		t.Fatalf("unexpected copy: %v", cp)
	}

	// Modifying copy should not affect original.
	cp["c"] = 3
	if m.Len() != 2 {
		t.Fatal("copy modification affected original")
	}
}

func TestMap_Range(t *testing.T) {
	t.Parallel()
	m := NewMap[string, int]()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)

	var count int
	m.Range(func(_ string, _ int) bool {
		count++
		return count < 2 // stop after 2
	})
	if count != 2 {
		t.Fatalf("expected range to stop after 2, got %d", count)
	}
}

func TestMap_Reset(t *testing.T) {
	t.Parallel()
	m := NewMap[string, int]()
	m.Set("a", 1)
	m.Reset()
	if m.Len() != 0 {
		t.Fatalf("expected empty after reset, got %d", m.Len())
	}
}

func TestMap_NewMapFrom(t *testing.T) {
	t.Parallel()
	src := map[string]int{"x": 10, "y": 20}
	m := NewMapFrom(src)
	if m.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", m.Len())
	}
	v, ok := m.Get("x")
	if !ok || v != 10 {
		t.Fatalf("expected (10, true), got (%d, %v)", v, ok)
	}
}

func TestMap_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	m := NewMap[int, int]()
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(key int) {
			defer wg.Done()
			m.Set(key, key*2)
			m.Get(key)
		}(i)
	}
	wg.Wait()
	if m.Len() != 100 {
		t.Fatalf("expected 100 entries, got %d", m.Len())
	}
}

func TestMap_GetOrSet_Concurrent(t *testing.T) {
	t.Parallel()
	m := NewMap[string, int]()
	var wg sync.WaitGroup
	var initCount int64
	var mu sync.Mutex

	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.GetOrSet("key", func() int {
				mu.Lock()
				initCount++
				mu.Unlock()
				return 42
			})
		}()
	}
	wg.Wait()

	v, ok := m.Get("key")
	if !ok || v != 42 {
		t.Fatalf("expected (42, true), got (%d, %v)", v, ok)
	}
}

func TestMap_GetOrSet_ExistingKey(t *testing.T) {
	t.Parallel()
	m := NewMap[string, int]()
	m.Set("preexisting", 99)

	// GetOrSet should return existing value without calling init.
	v := m.GetOrSet("preexisting", func() int {
		t.Fatal("init should not be called for existing key")
		return 0
	})
	if v != 99 {
		t.Fatalf("expected 99, got %d", v)
	}
}
