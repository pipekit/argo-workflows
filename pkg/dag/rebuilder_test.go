// Package dag implements the Build Systems à la Carte framework for DAG dependency evaluation.
package dag

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper types for rebuilder testing ---

// testValueWithHash implements Value with custom hash logic.
type testValueWithHash struct {
	data string
	hash string
}

func (v testValueWithHash) Hash() Hash {
	if v.hash != "" {
		return Hash(v.hash)
	}
	return Hash(v.data)
}

func newTestValueWithHash(data, hash string) testValueWithHash {
	return testValueWithHash{data: data, hash: hash}
}

// --- Tests for AlwaysRebuildRebuilder ---

func TestAlwaysRebuildRebuilder(t *testing.T) {
	t.Run("always returns true", func(t *testing.T) {
		rebuilder := NewAlwaysRebuildRebuilder[Key, testValueWithHash]()
		ctx := context.Background()

		// Test with nil current value
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)

		// Test with existing value
		existingValue := newTestValueWithHash("data", "hash")
		shouldRebuild, err = rebuilder.ShouldRebuild(ctx, "A", existingValue, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})

	t.Run("ignores fetch function", func(t *testing.T) {
		rebuilder := NewAlwaysRebuildRebuilder[Key, testValueWithHash]()
		ctx := context.Background()

		// Fetch function that would error if called
		fetch := func(key Key) (testValueWithHash, error) {
			t.Fatal("fetch should not be called")
			return testValueWithHash{}, nil
		}

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, fetch)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})
}

// --- Tests for DirtyBitRebuilder ---

func TestDirtyBitRebuilder(t *testing.T) {
	t.Run("rebuilds when no value exists", func(t *testing.T) {
		getDirtyBit := func(key Key) bool {
			return false
		}
		rebuilder := NewDirtyBitRebuilder[Key, testValueWithHash](getDirtyBit)
		ctx := context.Background()

		// Zero value means no existing value
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})

	t.Run("rebuilds when dirty bit is set", func(t *testing.T) {
		dirtyBits := map[Key]bool{
			"A": true,
			"B": false,
		}
		getDirtyBit := func(key Key) bool {
			return dirtyBits[key]
		}
		rebuilder := NewDirtyBitRebuilder[Key, testValueWithHash](getDirtyBit)
		ctx := context.Background()

		existingValue := newTestValueWithHash("data", "hash")

		// A is dirty
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", existingValue, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)

		// B is not dirty
		shouldRebuild, err = rebuilder.ShouldRebuild(ctx, "B", existingValue, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("does not rebuild when value exists and not dirty", func(t *testing.T) {
		getDirtyBit := func(key Key) bool {
			return false
		}
		rebuilder := NewDirtyBitRebuilder[Key, testValueWithHash](getDirtyBit)
		ctx := context.Background()

		existingValue := newTestValueWithHash("data", "hash")
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", existingValue, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("handles nil getDirtyBit function", func(t *testing.T) {
		rebuilder := NewDirtyBitRebuilder[Key, testValueWithHash](nil)
		ctx := context.Background()

		existingValue := newTestValueWithHash("data", "hash")
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", existingValue, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild) // Should not rebuild since value exists and no dirty check
	})
}

// --- Tests for VerifyingTraceRebuilder ---

func TestVerifyingTraceRebuilder(t *testing.T) {
	t.Run("rebuilds when no value exists", func(t *testing.T) {
		getTrace := func(key Key) *Trace {
			return NewTrace()
		}
		getDependencies := func(key Key) []Key {
			return nil
		}
		rebuilder := NewVerifyingTraceRebuilder[Key, testValueWithHash](getTrace, getDependencies)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})

	t.Run("rebuilds when no trace exists", func(t *testing.T) {
		getTrace := func(key Key) *Trace {
			return nil
		}
		getDependencies := func(key Key) []Key {
			return nil
		}
		rebuilder := NewVerifyingTraceRebuilder[Key, testValueWithHash](getTrace, getDependencies)
		ctx := context.Background()

		existingValue := newTestValueWithHash("data", "hash")
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", existingValue, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})

	t.Run("rebuilds when dependency hash changed", func(t *testing.T) {
		traces := map[Key]*Trace{
			"B": {
				Dependencies: []TraceEntry{
					{Key: "A", Hash: "old-hash"},
				},
			},
		}
		getTrace := func(key Key) *Trace {
			return traces[key]
		}
		getDependencies := func(key Key) []Key {
			if key == "B" {
				return []Key{"A"}
			}
			return nil
		}

		rebuilder := NewVerifyingTraceRebuilder[Key, testValueWithHash](getTrace, getDependencies)
		ctx := context.Background()

		// Current value of A has a different hash
		fetch := func(key Key) (testValueWithHash, error) {
			return newTestValueWithHash("data-A", "new-hash"), nil
		}

		existingValue := newTestValueWithHash("data-B", "hash-B")
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "B", existingValue, fetch)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})

	t.Run("does not rebuild when all hashes match", func(t *testing.T) {
		traces := map[Key]*Trace{
			"B": {
				Dependencies: []TraceEntry{
					{Key: "A", Hash: "hash-A"},
				},
			},
		}
		getTrace := func(key Key) *Trace {
			return traces[key]
		}
		getDependencies := func(key Key) []Key {
			if key == "B" {
				return []Key{"A"}
			}
			return nil
		}

		rebuilder := NewVerifyingTraceRebuilder[Key, testValueWithHash](getTrace, getDependencies)
		ctx := context.Background()

		// Current value of A has the same hash
		fetch := func(key Key) (testValueWithHash, error) {
			return newTestValueWithHash("data-A", "hash-A"), nil
		}

		existingValue := newTestValueWithHash("data-B", "hash-B")
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "B", existingValue, fetch)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("rebuilds when dependency not in trace", func(t *testing.T) {
		traces := map[Key]*Trace{
			"B": {
				Dependencies: []TraceEntry{
					{Key: "A", Hash: "hash-A"},
				},
			},
		}
		getTrace := func(key Key) *Trace {
			return traces[key]
		}
		getDependencies := func(key Key) []Key {
			if key == "B" {
				return []Key{"A", "C"} // C is a new dependency
			}
			return nil
		}

		rebuilder := NewVerifyingTraceRebuilder[Key, testValueWithHash](getTrace, getDependencies)
		ctx := context.Background()

		fetch := func(key Key) (testValueWithHash, error) {
			if key == "A" {
				return newTestValueWithHash("data-A", "hash-A"), nil
			}
			return newTestValueWithHash("data-C", "hash-C"), nil
		}

		existingValue := newTestValueWithHash("data-B", "hash-B")
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "B", existingValue, fetch)
		require.NoError(t, err)
		assert.True(t, shouldRebuild) // C not in trace, so rebuild
	})

	t.Run("rebuilds when fetch fails", func(t *testing.T) {
		traces := map[Key]*Trace{
			"B": {
				Dependencies: []TraceEntry{
					{Key: "A", Hash: "hash-A"},
				},
			},
		}
		getTrace := func(key Key) *Trace {
			return traces[key]
		}
		getDependencies := func(key Key) []Key {
			if key == "B" {
				return []Key{"A"}
			}
			return nil
		}

		rebuilder := NewVerifyingTraceRebuilder[Key, testValueWithHash](getTrace, getDependencies)
		ctx := context.Background()

		// Fetch returns error
		fetch := func(key Key) (testValueWithHash, error) {
			return testValueWithHash{}, errors.New("fetch failed")
		}

		existingValue := newTestValueWithHash("data-B", "hash-B")
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "B", existingValue, fetch)
		require.NoError(t, err) // Error doesn't propagate, just triggers rebuild
		assert.True(t, shouldRebuild)
	})

	t.Run("handles nil getTrace function", func(t *testing.T) {
		rebuilder := NewVerifyingTraceRebuilder[Key, testValueWithHash](nil, nil)
		ctx := context.Background()

		existingValue := newTestValueWithHash("data", "hash")
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", existingValue, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})

	t.Run("handles nil getDependencies function", func(t *testing.T) {
		getTrace := func(key Key) *Trace {
			return NewTrace()
		}
		rebuilder := NewVerifyingTraceRebuilder[Key, testValueWithHash](getTrace, nil)
		ctx := context.Background()

		existingValue := newTestValueWithHash("data", "hash")
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", existingValue, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})
}

// --- Tests for CompositeRebuilder ---

func TestCompositeRebuilder(t *testing.T) {
	t.Run("returns true only when all rebuilders return true", func(t *testing.T) {
		trueRebuilder := NewAlwaysRebuildRebuilder[Key, testValueWithHash]()
		falseRebuilder := &testFalseRebuilder[Key, testValueWithHash]{}

		// All true
		composite1 := NewCompositeRebuilder(trueRebuilder, trueRebuilder)
		ctx := context.Background()
		shouldRebuild, err := composite1.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)

		// One false
		composite2 := NewCompositeRebuilder[Key, testValueWithHash](trueRebuilder, falseRebuilder)
		shouldRebuild, err = composite2.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)

		// All false
		composite3 := NewCompositeRebuilder[Key, testValueWithHash](falseRebuilder, falseRebuilder)
		shouldRebuild, err = composite3.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("short-circuits on first false", func(t *testing.T) {
		var called []string
		rebuilder1 := &testTrackingRebuilder[Key, testValueWithHash]{
			name:          "r1",
			result:        false,
			calledTracker: &called,
		}
		rebuilder2 := &testTrackingRebuilder[Key, testValueWithHash]{
			name:          "r2",
			result:        true,
			calledTracker: &called,
		}

		composite := NewCompositeRebuilder[Key, testValueWithHash](rebuilder1, rebuilder2)
		ctx := context.Background()
		shouldRebuild, err := composite.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
		assert.Equal(t, []string{"r1"}, called) // r2 not called
	})

	t.Run("propagates errors", func(t *testing.T) {
		errRebuilder := &testErrorRebuilder[Key, testValueWithHash]{
			err: errors.New("rebuilder error"),
		}
		composite := NewCompositeRebuilder[Key, testValueWithHash](errRebuilder)
		ctx := context.Background()
		_, err := composite.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rebuilder error")
	})

	t.Run("empty composite returns true", func(t *testing.T) {
		composite := NewCompositeRebuilder[Key, testValueWithHash]()
		ctx := context.Background()
		shouldRebuild, err := composite.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild) // No rebuilders to say false
	})
}

// --- Tests for AnyRebuilder ---

func TestAnyRebuilder(t *testing.T) {
	t.Run("returns true when any rebuilder returns true", func(t *testing.T) {
		trueRebuilder := NewAlwaysRebuildRebuilder[Key, testValueWithHash]()
		falseRebuilder := &testFalseRebuilder[Key, testValueWithHash]{}

		// All true
		any1 := NewAnyRebuilder(trueRebuilder, trueRebuilder)
		ctx := context.Background()
		shouldRebuild, err := any1.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)

		// One true
		any2 := NewAnyRebuilder[Key, testValueWithHash](falseRebuilder, trueRebuilder)
		shouldRebuild, err = any2.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)

		// All false
		any3 := NewAnyRebuilder[Key, testValueWithHash](falseRebuilder, falseRebuilder)
		shouldRebuild, err = any3.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("short-circuits on first true", func(t *testing.T) {
		var called []string
		rebuilder1 := &testTrackingRebuilder[Key, testValueWithHash]{
			name:          "r1",
			result:        true,
			calledTracker: &called,
		}
		rebuilder2 := &testTrackingRebuilder[Key, testValueWithHash]{
			name:          "r2",
			result:        false,
			calledTracker: &called,
		}

		any := NewAnyRebuilder[Key, testValueWithHash](rebuilder1, rebuilder2)
		ctx := context.Background()
		shouldRebuild, err := any.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
		assert.Equal(t, []string{"r1"}, called) // r2 not called
	})

	t.Run("propagates errors", func(t *testing.T) {
		errRebuilder := &testErrorRebuilder[Key, testValueWithHash]{
			err: errors.New("rebuilder error"),
		}
		any := NewAnyRebuilder[Key, testValueWithHash](errRebuilder)
		ctx := context.Background()
		_, err := any.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rebuilder error")
	})

	t.Run("empty any returns false", func(t *testing.T) {
		any := NewAnyRebuilder[Key, testValueWithHash]()
		ctx := context.Background()
		shouldRebuild, err := any.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild) // No rebuilders to say true
	})
}

// --- Tests for Trace ---

func TestTrace(t *testing.T) {
	t.Run("NewTrace creates empty trace", func(t *testing.T) {
		trace := NewTrace()
		assert.NotNil(t, trace)
		assert.Empty(t, trace.Dependencies)
	})

	t.Run("AddEntry adds dependency hash", func(t *testing.T) {
		trace := NewTrace()
		trace.AddEntry("A", "hash-A")
		trace.AddEntry("B", "hash-B")

		assert.Len(t, trace.Dependencies, 2)
		assert.Equal(t, TraceEntry{Key: "A", Hash: "hash-A"}, trace.Dependencies[0])
		assert.Equal(t, TraceEntry{Key: "B", Hash: "hash-B"}, trace.Dependencies[1])
	})

	t.Run("GetHash retrieves stored hash", func(t *testing.T) {
		trace := NewTrace()
		trace.AddEntry("A", "hash-A")
		trace.AddEntry("B", "hash-B")

		hash, found := trace.GetHash("A")
		assert.True(t, found)
		assert.Equal(t, Hash("hash-A"), hash)

		hash, found = trace.GetHash("B")
		assert.True(t, found)
		assert.Equal(t, Hash("hash-B"), hash)

		hash, found = trace.GetHash("C")
		assert.False(t, found)
		assert.Equal(t, Hash(""), hash)
	})
}

// --- Helper rebuilders for testing ---

// testFalseRebuilder always returns false.
type testFalseRebuilder[K comparable, V Value] struct{}

func (r *testFalseRebuilder[K, V]) ShouldRebuild(_ context.Context, _ K, _ V, _ Fetch[K, V]) (bool, error) {
	return false, nil
}

// testTrackingRebuilder tracks calls and returns a configured result.
type testTrackingRebuilder[K comparable, V Value] struct {
	name          string
	result        bool
	calledTracker *[]string
}

func (r *testTrackingRebuilder[K, V]) ShouldRebuild(_ context.Context, _ K, _ V, _ Fetch[K, V]) (bool, error) {
	*r.calledTracker = append(*r.calledTracker, r.name)
	return r.result, nil
}

// testErrorRebuilder always returns an error.
type testErrorRebuilder[K comparable, V Value] struct {
	err error
}

func (r *testErrorRebuilder[K, V]) ShouldRebuild(_ context.Context, _ K, _ V, _ Fetch[K, V]) (bool, error) {
	return false, r.err
}
