package csync

import (
	"maps"
	"sync"
)

// Map is a generic thread-safe map. All operations are guarded by a RWMutex.
type Map[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// NewMap creates an empty Map.
func NewMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{m: make(map[K]V)}
}

// NewMapFrom creates a Map pre-populated with entries from src.
func NewMapFrom[K comparable, V any](src map[K]V) *Map[K, V] {
	return &Map[K, V]{m: maps.Clone(src)}
}

// Get returns the value for key and whether it was found.
func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.m[key]
	return v, ok
}

// Set stores a key-value pair.
func (m *Map[K, V]) Set(key K, val V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[key] = val
}

// Del removes a key.
func (m *Map[K, V]) Del(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.m, key)
}

// Len returns the number of entries.
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.m)
}

// GetOrSet returns the existing value for key if present.
// Otherwise it calls init(), stores and returns the result.
func (m *Map[K, V]) GetOrSet(key K, init func() V) V {
	m.mu.RLock()
	v, ok := m.m[key]
	m.mu.RUnlock()
	if ok {
		return v
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check after acquiring write lock.
	if v, ok := m.m[key]; ok {
		return v
	}
	v = init()
	m.m[key] = v
	return v
}

// Copy returns a shallow clone of the underlying map.
func (m *Map[K, V]) Copy() map[K]V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return maps.Clone(m.m)
}

// Range calls fn for each entry. If fn returns false, iteration stops.
// Operates on a snapshot for safety.
func (m *Map[K, V]) Range(fn func(K, V) bool) {
	snapshot := m.Copy()
	for k, v := range snapshot {
		if !fn(k, v) {
			return
		}
	}
}

// Reset clears all entries.
func (m *Map[K, V]) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m = make(map[K]V)
}
