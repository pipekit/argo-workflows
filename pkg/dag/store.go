package dag

import (
	"sync"
)

// Store provides persistent storage for task values and build information.
// It is parameterized by:
//   - I: The type of build information (varies by rebuilder strategy)
//   - K: The type of key (must be comparable)
//   - V: The type of values stored
//
// Different rebuilder strategies require different Info types:
//   - Dirty bit: just a boolean per key
//   - Verifying traces: input hashes
//   - Constructive traces: full dependency traces
type Store[I any, K comparable, V any] interface {
	// GetValue retrieves the stored value for a key.
	// Returns the value and true if found, or the zero value and false if not found.
	GetValue(key K) (V, bool)

	// SetValue stores a value for a key.
	SetValue(key K, value V)

	// GetInfo retrieves build information for the entire store.
	// This is used by rebuilders to track build metadata.
	GetInfo() I

	// SetInfo stores build information for the entire store.
	SetInfo(info I)

	// GetState returns the current execution state of a task.
	GetState(key K) TaskState

	// SetState updates the execution state of a task.
	SetState(key K, state TaskState)

	// ListKeys returns all keys in the store.
	ListKeys() []K
}

// MapStore is a simple in-memory implementation of Store using maps.
// This is the basic store implementation suitable for testing and
// simple use cases. It is thread-safe.
type MapStore[I any, K comparable, V any] struct {
	mu     sync.RWMutex
	values map[K]V
	states map[K]TaskState
	info   I
}

// NewMapStore creates a new MapStore with the given initial info.
func NewMapStore[I any, K comparable, V any](initialInfo I) *MapStore[I, K, V] {
	return &MapStore[I, K, V]{
		values: make(map[K]V),
		states: make(map[K]TaskState),
		info:   initialInfo,
	}
}

// GetValue retrieves the stored value for a key.
func (s *MapStore[I, K, V]) GetValue(key K) (V, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[key]
	return v, ok
}

// SetValue stores a value for a key.
func (s *MapStore[I, K, V]) SetValue(key K, value V) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

// GetInfo retrieves build information for the store.
func (s *MapStore[I, K, V]) GetInfo() I {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

// SetInfo stores build information for the store.
func (s *MapStore[I, K, V]) SetInfo(info I) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info = info
}

// GetState returns the current execution state of a task.
func (s *MapStore[I, K, V]) GetState(key K) TaskState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[key]
	if !ok {
		return TaskStatePending
	}
	return state
}

// SetState updates the execution state of a task.
func (s *MapStore[I, K, V]) SetState(key K, state TaskState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[key] = state
}

// ListKeys returns all keys in the store.
// This includes keys that have values, states, or both.
func (s *MapStore[I, K, V]) ListKeys() []K {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Use a map to deduplicate keys from values and states
	keySet := make(map[K]struct{})
	for k := range s.values {
		keySet[k] = struct{}{}
	}
	for k := range s.states {
		keySet[k] = struct{}{}
	}

	keys := make([]K, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	return keys
}

// HasValue returns true if the store has a value for the given key.
func (s *MapStore[I, K, V]) HasValue(key K) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.values[key]
	return ok
}

// DeleteValue removes the value for a key from the store.
func (s *MapStore[I, K, V]) DeleteValue(key K) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
}

// DeleteState removes the state for a key from the store.
func (s *MapStore[I, K, V]) DeleteState(key K) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, key)
}

// Clear removes all values and states from the store, but keeps the info.
func (s *MapStore[I, K, V]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = make(map[K]V)
	s.states = make(map[K]TaskState)
}

// KeyInfo holds per-key build information for use with KeyInfoStore.
// This is used when rebuilders need to track information per key
// rather than globally.
type KeyInfo[I any] struct {
	Info I
}

// KeyInfoStore extends MapStore to support per-key info storage.
// This is useful for rebuilder strategies that need to track
// information (like dirty bits or hashes) per key.
type KeyInfoStore[I any, K comparable, V any] struct {
	*MapStore[map[K]I, K, V]
}

// NewKeyInfoStore creates a new KeyInfoStore.
func NewKeyInfoStore[I any, K comparable, V any]() *KeyInfoStore[I, K, V] {
	return &KeyInfoStore[I, K, V]{
		MapStore: NewMapStore[map[K]I, K, V](make(map[K]I)),
	}
}

// GetKeyInfo retrieves build information for a specific key.
func (s *KeyInfoStore[I, K, V]) GetKeyInfo(key K) (I, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info, ok := s.info[key]
	return info, ok
}

// SetKeyInfo stores build information for a specific key.
func (s *KeyInfoStore[I, K, V]) SetKeyInfo(key K, info I) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info[key] = info
}

// DeleteKeyInfo removes build information for a specific key.
func (s *KeyInfoStore[I, K, V]) DeleteKeyInfo(key K) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.info, key)
}
