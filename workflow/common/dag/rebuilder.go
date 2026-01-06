package dag

import (
	"context"

	genericdag "github.com/argoproj/argo-workflows/v3/pkg/dag"
)

// PhaseBasedRebuilder is an Argo-specific rebuilder for workflow nodes.
// It determines rebuild based on the task's current execution state (phase).
//
// Rebuild rules:
//   - Pending: rebuild (task not started)
//   - Running: do NOT rebuild (task already executing)
//   - Succeeded: do NOT rebuild (task completed successfully)
//   - Failed: do NOT rebuild (task failed, let retry policy handle)
//   - Skipped: do NOT rebuild (intentionally skipped)
//   - Omitted: do NOT rebuild (not meant to run)
//   - Error: do NOT rebuild (error state, needs intervention)
//
// This maps directly to Argo's node phases and is designed for use within
// the workflow controller's reconciliation loop.
type PhaseBasedRebuilder[K comparable, V genericdag.Value] struct {
	// getState returns the current task state for a key.
	getState func(ctx context.Context, key K) genericdag.TaskState
	// evaluateDepends evaluates the depends expression for conditional logic.
	// Returns:
	//   - shouldExecute: true if the task should execute based on depends logic
	//   - canProceed: true if all dependencies are in a terminal state
	//   - err: error during evaluation
	evaluateDepends func(ctx context.Context, key K, fetch genericdag.Fetch[K, V]) (shouldExecute bool, canProceed bool, err error)
}

// NewPhaseBasedRebuilder creates a new PhaseBasedRebuilder.
// Parameters:
//   - getState: function to get the current state of a task
//   - evaluateDepends: optional function to evaluate depends expressions
//     (for clauses like "A.Succeeded && B.Failed"). If nil, simple state check is used.
func NewPhaseBasedRebuilder[K comparable, V genericdag.Value](
	getState func(ctx context.Context, key K) genericdag.TaskState,
	evaluateDepends func(ctx context.Context, key K, fetch genericdag.Fetch[K, V]) (bool, bool, error),
) *PhaseBasedRebuilder[K, V] {
	return &PhaseBasedRebuilder[K, V]{
		getState:        getState,
		evaluateDepends: evaluateDepends,
	}
}

// ShouldRebuild implements genericdag.Rebuilder.ShouldRebuild.
// Returns true only if the task is in Pending state (or not yet created).
func (r *PhaseBasedRebuilder[K, V]) ShouldRebuild(
	ctx context.Context,
	key K,
	_ V,
	fetch genericdag.Fetch[K, V],
) (bool, error) {
	// Get current state
	if r.getState == nil {
		// No state function, assume pending
		return true, nil
	}
	state := r.getState(ctx, key)

	// Check based on state
	switch state {
	case genericdag.TaskStateSucceeded:
		// Already completed successfully, don't rebuild
		return false, nil

	case genericdag.TaskStateFailed:
		// Failed, let retry policy handle, don't automatically rebuild
		return false, nil

	case genericdag.TaskStateSkipped:
		// Intentionally skipped, don't rebuild
		return false, nil

	case genericdag.TaskStateOmitted:
		// Omitted from execution, don't rebuild
		return false, nil

	case genericdag.TaskStateError:
		// Error state, needs intervention, don't auto-rebuild
		return false, nil

	case genericdag.TaskStateRunning:
		// Already running, don't start another instance
		return false, nil

	case genericdag.TaskStatePending:
		// Task is pending - check depends logic if available
		if r.evaluateDepends != nil {
			shouldExecute, canProceed, err := r.evaluateDepends(ctx, key, fetch)
			if err != nil {
				return false, err
			}
			// Only proceed if can proceed AND should execute
			return shouldExecute && canProceed, nil
		}
		// No depends evaluator, rebuild by default
		return true, nil

	default:
		// Unknown state, treat as pending
		if r.evaluateDepends != nil {
			shouldExecute, canProceed, err := r.evaluateDepends(ctx, key, fetch)
			if err != nil {
				return false, err
			}
			return shouldExecute && canProceed, nil
		}
		return true, nil
	}
}

// StateBasedRebuilder is a simpler version of PhaseBasedRebuilder that only
// checks the task state without depends expression evaluation.
// This is useful when you just want to rebuild based on state alone.
type StateBasedRebuilder[K comparable, V genericdag.Value] struct {
	// getState returns the current task state for a key.
	getState func(ctx context.Context, key K) genericdag.TaskState
}

// NewStateBasedRebuilder creates a new StateBasedRebuilder.
func NewStateBasedRebuilder[K comparable, V genericdag.Value](getState func(ctx context.Context, key K) genericdag.TaskState) *StateBasedRebuilder[K, V] {
	return &StateBasedRebuilder[K, V]{
		getState: getState,
	}
}

// ShouldRebuild implements genericdag.Rebuilder.ShouldRebuild.
// Returns true only if the task is in Pending state.
func (r *StateBasedRebuilder[K, V]) ShouldRebuild(
	ctx context.Context,
	key K,
	_ V,
	_ genericdag.Fetch[K, V],
) (bool, error) {
	if r.getState == nil {
		return true, nil
	}
	state := r.getState(ctx, key)
	// Only rebuild if pending (not started)
	return state == genericdag.TaskStatePending, nil
}

var _ genericdag.Rebuilder[genericdag.Key, NodeValue] = (*PhaseBasedRebuilder[genericdag.Key, NodeValue])(nil)
var _ genericdag.Rebuilder[genericdag.Key, NodeValue] = (*StateBasedRebuilder[genericdag.Key, NodeValue])(nil)