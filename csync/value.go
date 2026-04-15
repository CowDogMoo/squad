// Package csync provides generic thread-safe containers.
package csync

import "sync"

// Value is a generic thread-safe scalar wrapper. Use it for values that can
// be read and written concurrently by multiple goroutines.
//
// For pointer, slice, or map types, use Map or Slice instead — those types
// have reference semantics that a simple mutex guard cannot make safe.
type Value[T any] struct {
	mu  sync.RWMutex
	val T
}

// NewValue creates a Value with the given initial value.
func NewValue[T any](initial T) *Value[T] {
	return &Value[T]{val: initial}
}

// Get returns the current value under a read lock.
func (v *Value[T]) Get() T {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.val
}

// Set updates the value under a write lock.
func (v *Value[T]) Set(val T) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.val = val
}

// Update atomically applies fn to the current value and stores the result.
func (v *Value[T]) Update(fn func(T) T) T {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.val = fn(v.val)
	return v.val
}
