// Package dag implements the Build Systems à la Carte framework for DAG dependency evaluation.
package dag

import (
	"context"
)

// Rebuilder determines whether a task needs to be rebuilt based on the current store state.
// In the Build Systems à la Carte paper, this is parameterized by:
//   - ir: "information about the key" - persistent build info
//   - k: key type
//   - v: value type
//
// The rebuilder is one of the two core abstractions in the framework:
//   - Scheduler: determines the order in which tasks are executed
//   - Rebuilder: determines whether a task needs to be re-executed
//
// Different rebuilder strategies offer different trade-offs:
//   - DirtyBitRebuilder: Simple, but may over-rebuild
//   - VerifyingTraceRebuilder: More precise, but requires hash computation
//   - AlwaysRebuildRebuilder: Always rebuilds, useful for testing
type Rebuilder[K comparable, V Value] interface {
	// ShouldRebuild determines if a task should be rebuilt.
	// Parameters:
	//   - ctx: context for cancellation and timeouts
	//   - key: the task to check
	//   - currentValue: the current stored value (if any)
	//   - fetch: a function to get dependency values
	// Returns:
	//   - rebuild: true if the task should be rebuilt
	//   - err: error if check failed
	ShouldRebuild(ctx context.Context, key K, currentValue V, fetch Fetch[K, V]) (bool, error)
}

// TraceEntry records the hash of a dependency value at the time of a build.
// This is used by VerifyingTraceRebuilder to track which dependency values
// were used to produce a task's output.
type TraceEntry struct {
	// Key is the dependency key.
	Key Key
	// Hash is the hash of the dependency value.
	Hash Hash
}

// Trace records the dependency hashes used in a build.
// When checking if a rebuild is needed, we compare current dependency hashes
// against the stored trace.
type Trace struct {
	// Dependencies contains the hashes of all dependencies at build time.
	Dependencies []TraceEntry
}

// NewTrace creates a new empty trace.
func NewTrace() *Trace {
	return &Trace{
		Dependencies: make([]TraceEntry, 0),
	}
}

// AddEntry adds a dependency hash entry to the trace.
func (t *Trace) AddEntry(key Key, hash Hash) {
	t.Dependencies = append(t.Dependencies, TraceEntry{Key: key, Hash: hash})
}

// GetHash returns the stored hash for a dependency key, or empty string if not found.
func (t *Trace) GetHash(key Key) (Hash, bool) {
	for _, entry := range t.Dependencies {
		if entry.Key == key {
			return entry.Hash, true
		}
	}
	return "", false
}

// DirtyBitInfo stores whether a task is dirty (needs rebuild).
// This is the simplest form of build information.
type DirtyBitInfo struct {
	// Dirty indicates the task needs to be rebuilt.
	Dirty bool
}

// DirtyBitRebuilder implements the simplest rebuilder strategy.
// It rebuilds a task if:
//   - No value exists in the store (never built)
//   - Task state is not Succeeded
//   - The dirty bit is set (dependency has been modified)
//
// This rebuilder is simple but may cause unnecessary rebuilds since it
// doesn't track which specific dependency values were used.
type DirtyBitRebuilder[K comparable, V Value] struct {
	// getDirtyBit retrieves the dirty bit for a key from the store info.
	getDirtyBit func(key K) bool
}

// NewDirtyBitRebuilder creates a new DirtyBitRebuilder.
// The getDirtyBit function should return true if the key is marked dirty.
func NewDirtyBitRebuilder[K comparable, V Value](getDirtyBit func(key K) bool) *DirtyBitRebuilder[K, V] {
	return &DirtyBitRebuilder[K, V]{
		getDirtyBit: getDirtyBit,
	}
}

// ShouldRebuild implements Rebuilder.ShouldRebuild.
// Returns true if:
//   - The current value is nil (never built)
//   - The dirty bit is set for this key
func (r *DirtyBitRebuilder[K, V]) ShouldRebuild(
	_ context.Context,
	key K,
	currentValue V,
	_ Fetch[K, V],
) (bool, error) {
	// Check if value exists - if not, must rebuild
	var zero V
	if any(currentValue) == any(zero) {
		return true, nil
	}

	// Check dirty bit
	if r.getDirtyBit != nil && r.getDirtyBit(key) {
		return true, nil
	}

	return false, nil
}

// VerifyingTraceRebuilder implements a more sophisticated rebuilder strategy.
// It tracks which dependency values (by hash) were used to produce each task's output.
// On rebuild check, it:
//  1. Retrieves the stored trace of dependency hashes
//  2. Fetches current dependency values
//  3. Compares current hashes with stored trace
//  4. Only rebuilds if any hash differs
//
// This is more precise than DirtyBitRebuilder because it only rebuilds when
// the actual dependency values have changed, not just when any modification occurred.
type VerifyingTraceRebuilder[K comparable, V Value] struct {
	// getTrace retrieves the trace for a key.
	getTrace func(key K) *Trace
	// getDependencies returns the dependencies for a key.
	getDependencies func(key K) []K
}

// NewVerifyingTraceRebuilder creates a new VerifyingTraceRebuilder.
// Parameters:
//   - getTrace: function to retrieve the build trace for a key
//   - getDependencies: function to get the dependencies for a key
func NewVerifyingTraceRebuilder[K comparable, V Value](
	getTrace func(key K) *Trace,
	getDependencies func(key K) []K,
) *VerifyingTraceRebuilder[K, V] {
	return &VerifyingTraceRebuilder[K, V]{
		getTrace:        getTrace,
		getDependencies: getDependencies,
	}
}

// ShouldRebuild implements Rebuilder.ShouldRebuild.
// Returns true if:
//   - The current value is nil (never built)
//   - No trace exists for the key
//   - Any dependency hash differs from the stored trace
func (r *VerifyingTraceRebuilder[K, V]) ShouldRebuild(
	_ context.Context,
	key K,
	currentValue V,
	fetch Fetch[K, V],
) (bool, error) {
	// Check if value exists - if not, must rebuild
	var zero V
	if any(currentValue) == any(zero) {
		return true, nil
	}

	// Get the stored trace
	if r.getTrace == nil {
		return true, nil
	}
	trace := r.getTrace(key)
	if trace == nil {
		return true, nil
	}

	// Get dependencies
	if r.getDependencies == nil {
		// No way to check dependencies, assume rebuild needed
		return true, nil
	}
	deps := r.getDependencies(key)

	// Verify each dependency hash matches the trace
	for _, depKey := range deps {
		// Fetch current dependency value
		depValue, err := fetch(depKey)
		if err != nil {
			// If we can't fetch, assume rebuild needed
			// (could be a suspend error which means dep not ready)
			return true, nil
		}

		// Get current hash
		currentHash := depValue.Hash()

		// Get stored hash from trace
		storedHash, found := trace.GetHash(Key(keyToString(depKey)))
		if !found {
			// Dependency not in trace, must rebuild
			return true, nil
		}

		// Compare hashes
		if currentHash != storedHash {
			return true, nil
		}
	}

	// All dependency hashes match, no rebuild needed
	return false, nil
}

// AlwaysRebuildRebuilder always returns true for ShouldRebuild.
// This is useful for:
//   - Testing and debugging
//   - Scenarios where caching is not desired
//   - Ensuring fresh builds every time
type AlwaysRebuildRebuilder[K comparable, V Value] struct{}

// NewAlwaysRebuildRebuilder creates a new AlwaysRebuildRebuilder.
func NewAlwaysRebuildRebuilder[K comparable, V Value]() *AlwaysRebuildRebuilder[K, V] {
	return &AlwaysRebuildRebuilder[K, V]{}
}

// ShouldRebuild implements Rebuilder.ShouldRebuild.
// Always returns true.
func (r *AlwaysRebuildRebuilder[K, V]) ShouldRebuild(
	_ context.Context,
	_ K,
	_ V,
	_ Fetch[K, V],
) (bool, error) {
	return true, nil
}

// CompositeRebuilder combines multiple rebuilders with logical AND semantics.

type CompositeRebuilder[K comparable, V Value] struct {
	rebuilders []Rebuilder[K, V]
}

// NewCompositeRebuilder creates a new CompositeRebuilder with the given rebuilders.
func NewCompositeRebuilder[K comparable, V Value](rebuilders ...Rebuilder[K, V]) *CompositeRebuilder[K, V] {
	return &CompositeRebuilder[K, V]{
		rebuilders: rebuilders,
	}
}

// ShouldRebuild implements Rebuilder.ShouldRebuild.
// Returns true only if ALL component rebuilders return true.
func (r *CompositeRebuilder[K, V]) ShouldRebuild(
	ctx context.Context,
	key K,
	currentValue V,
	fetch Fetch[K, V],
) (bool, error) {
	for _, rebuilder := range r.rebuilders {
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, key, currentValue, fetch)
		if err != nil {
			return false, err
		}
		if !shouldRebuild {
			return false, nil
		}
	}
	return true, nil
}

// AnyRebuilder combines multiple rebuilders with logical OR semantics.
// A task is rebuilt if ANY component rebuilder says it should be rebuilt.
type AnyRebuilder[K comparable, V Value] struct {
	rebuilders []Rebuilder[K, V]
}

// NewAnyRebuilder creates a new AnyRebuilder with the given rebuilders.
func NewAnyRebuilder[K comparable, V Value](rebuilders ...Rebuilder[K, V]) *AnyRebuilder[K, V] {
	return &AnyRebuilder[K, V]{
		rebuilders: rebuilders,
	}
}

// ShouldRebuild implements Rebuilder.ShouldRebuild.
// Returns true if ANY component rebuilder returns true.
func (r *AnyRebuilder[K, V]) ShouldRebuild(
	ctx context.Context,
	key K,
	currentValue V,
	fetch Fetch[K, V],
) (bool, error) {
	for _, rebuilder := range r.rebuilders {
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, key, currentValue, fetch)
		if err != nil {
			return false, err
		}
		if shouldRebuild {
			return true, nil
		}
	}
	return false, nil
}
