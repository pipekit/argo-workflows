package dag

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericdag "github.com/argoproj/argo-workflows/v3/pkg/dag"
)

// --- Helper types for rebuilder testing ---

// testValueWithHash implements Value with custom hash logic.
type testValueWithHash struct {
	data string
	hash string
}

func (v testValueWithHash) Hash() genericdag.Hash {
	if v.hash != "" {
		return genericdag.Hash(v.hash)
	}
	return genericdag.Hash(v.data)
}

func newTestValueWithHash(data, hash string) testValueWithHash {
	return testValueWithHash{data: data, hash: hash}
}

// --- Tests for PhaseBasedRebuilder ---

func TestPhaseBasedRebuilder(t *testing.T) {
	t.Run("does not rebuild when succeeded", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStateSucceeded
		}
		rebuilder := NewPhaseBasedRebuilder[genericdag.Key, testValueWithHash](getState, nil)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("does not rebuild when running", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStateRunning
		}
		rebuilder := NewPhaseBasedRebuilder[genericdag.Key, testValueWithHash](getState, nil)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("does not rebuild when failed", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStateFailed
		}
		rebuilder := NewPhaseBasedRebuilder[genericdag.Key, testValueWithHash](getState, nil)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("does not rebuild when skipped", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStateSkipped
		}
		rebuilder := NewPhaseBasedRebuilder[genericdag.Key, testValueWithHash](getState, nil)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("does not rebuild when omitted", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStateOmitted
		}
		rebuilder := NewPhaseBasedRebuilder[genericdag.Key, testValueWithHash](getState, nil)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("does not rebuild when error state", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStateError
		}
		rebuilder := NewPhaseBasedRebuilder[genericdag.Key, testValueWithHash](getState, nil)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("rebuilds when pending without depends logic", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStatePending
		}
		rebuilder := NewPhaseBasedRebuilder[genericdag.Key, testValueWithHash](getState, nil)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})

	t.Run("uses depends logic when provided", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStatePending
		}
		evaluateDepends := func(ctx context.Context, key genericdag.Key, fetch genericdag.Fetch[genericdag.Key, testValueWithHash]) (bool, bool, error) {
			// Return shouldExecute=true, canProceed=true
			if key == "A" {
				return true, true, nil
			}
			// Return shouldExecute=false, canProceed=true for B
			return false, true, nil
		}
		rebuilder := NewPhaseBasedRebuilder(getState, evaluateDepends)
		ctx := context.Background()

		// A should rebuild (shouldExecute=true && canProceed=true)
		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)

		// B should not rebuild (shouldExecute=false)
		shouldRebuild, err = rebuilder.ShouldRebuild(ctx, "B", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("does not rebuild when cannot proceed", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStatePending
		}
		evaluateDepends := func(ctx context.Context, key genericdag.Key, fetch genericdag.Fetch[genericdag.Key, testValueWithHash]) (bool, bool, error) {
			// shouldExecute=true but canProceed=false (deps not ready)
			return true, false, nil
		}
		rebuilder := NewPhaseBasedRebuilder(getState, evaluateDepends)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.False(t, shouldRebuild)
	})

	t.Run("propagates depends evaluation error", func(t *testing.T) {
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return genericdag.TaskStatePending
		}
		evaluateDepends := func(ctx context.Context, key genericdag.Key, fetch genericdag.Fetch[genericdag.Key, testValueWithHash]) (bool, bool, error) {
			return false, false, errors.New("evaluation failed")
		}
		rebuilder := NewPhaseBasedRebuilder(getState, evaluateDepends)
		ctx := context.Background()

		_, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "evaluation failed")
	})

	t.Run("handles nil getState function", func(t *testing.T) {
		rebuilder := NewPhaseBasedRebuilder[genericdag.Key, testValueWithHash](nil, nil)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild) // No state function means assume pending
	})
}

// --- Tests for StateBasedRebuilder ---

func TestStateBasedRebuilder(t *testing.T) {
	t.Run("rebuilds only when pending", func(t *testing.T) {
		states := map[genericdag.Key]genericdag.TaskState{
			"pending":   genericdag.TaskStatePending,
			"running":   genericdag.TaskStateRunning,
			"succeeded": genericdag.TaskStateSucceeded,
			"failed":    genericdag.TaskStateFailed,
			"skipped":   genericdag.TaskStateSkipped,
			"omitted":   genericdag.TaskStateOmitted,
			"error":     genericdag.TaskStateError,
		}
		getState := func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return states[key]
		}
		rebuilder := NewStateBasedRebuilder[genericdag.Key, testValueWithHash](getState)
		ctx := context.Background()

		testCases := []struct {
			key           genericdag.Key
			shouldRebuild bool
		}{
			{"pending", true},
			{"running", false},
			{"succeeded", false},
			{"failed", false},
			{"skipped", false},
			{"omitted", false},
			{"error", false},
		}

		for _, tc := range testCases {
			t.Run(string(tc.key), func(t *testing.T) {
				shouldRebuild, err := rebuilder.ShouldRebuild(ctx, tc.key, testValueWithHash{}, nil)
				require.NoError(t, err)
				assert.Equal(t, tc.shouldRebuild, shouldRebuild)
			})
		}
	})

	t.Run("handles nil getState function", func(t *testing.T) {
		rebuilder := NewStateBasedRebuilder[genericdag.Key, testValueWithHash](nil)
		ctx := context.Background()

		shouldRebuild, err := rebuilder.ShouldRebuild(ctx, "A", testValueWithHash{}, nil)
		require.NoError(t, err)
		assert.True(t, shouldRebuild)
	})
}
