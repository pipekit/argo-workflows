// Package dag implements the Build Systems à la Carte framework for DAG dependency evaluation.
package dag

import (
	"context"
	"fmt"
	"sync"
)

// Scheduler orchestrates task execution in the Build Systems à la Carte framework.
// Different scheduler implementations provide different strategies for handling dependencies:
//   - Topological: Pre-computes execution order from static dependencies (not suitable for dynamic deps)
//   - Restarting: Starts tasks, restarts from beginning if dependency missing (inefficient)
//   - Suspending: Suspends task execution when dependency isn't ready (ideal for Argo)
//
// The Scheduler is one of two core abstractions in the framework, the other being the Rebuilder.
type Scheduler[K comparable, V Value] interface {
	// Build builds a target key and returns the result.
	// It orchestrates the execution of tasks, handling dependencies according to the
	// scheduler's strategy. For a suspending scheduler, this means:
	//   1. Check if target should be rebuilt
	//   2. If a dependency is missing, suspend and build it first
	//   3. Retry the original task once dependency is available
	//
	// Returns the built value or an error if the build fails.
	Build(ctx context.Context, target K) (V, error)
}

// Build is the main build function from the Build Systems à la Carte paper.
// It ties together the scheduler, rebuilder, tasks, and store to create
// a complete build system.
//
// This struct represents the composition: Build = Scheduler × Rebuilder × Tasks × Store
type Build[K comparable, V Value] struct {
	// Scheduler orchestrates task execution order.
	Scheduler Scheduler[K, V]
	// Rebuilder determines whether tasks need to be rebuilt.
	Rebuilder Rebuilder[K, V]
	// Tasks contains the task definitions.
	Tasks Tasks[K, V]
	// Store provides persistent storage for values and build information.
	Store Store[any, K, V]
}

// Run builds a target using the configured components.
func (b *Build[K, V]) Run(ctx context.Context, target K) (V, error) {
	return b.Scheduler.Build(ctx, target)
}

// BuildResult contains the result of a build operation.
// This is useful for batch operations and for Argo Workflows integration
// where we need to evaluate multiple tasks at once.
type BuildResult[K comparable, V Value] struct {
	// Key is the task key that was built (or attempted).
	Key K
	// Value is the result of the build (if successful).
	Value V
	// State is the final state of the task after the build attempt.
	State TaskState
	// Error contains any error that occurred during the build.
	Error error
	// Suspended indicates if the task is suspended waiting for dependencies.
	Suspended bool
	// WaitingOn contains the keys of dependencies this task is waiting for.
	// Only populated when Suspended is true.
	WaitingOn []K
}

// SuspendingScheduler implements the suspending scheduling strategy from the paper.
// Unlike topological schedulers (like Make) which compute all dependencies upfront,
// or restarting schedulers (like Excel) which restart tasks when dependencies are missing,
// the suspending scheduler (like Shake):
//   - Suspends a task when it tries to fetch a dependency that isn't ready
//   - Builds the dependency
//   - Resumes the original task
//
// This is ideal for Argo Workflows because:
//   - Dependencies can be computed based on runtime values (dynamic dependencies)
//   - Tasks suspend rather than restart when dependencies are unavailable (efficient)
//   - Naturally maps to Kubernetes pod states (Pending, Running, Succeeded, Failed)
type SuspendingScheduler[K comparable, V Value] struct {
	// Rebuilder determines whether tasks need to be rebuilt.
	Rebuilder Rebuilder[K, V]
	// Tasks contains the task definitions.
	Tasks Tasks[K, V]
	// Store provides persistent storage for values and build information.
	Store Store[any, K, V]

	// MaxDepth limits the recursion depth to prevent stack overflow.
	// A value of 0 means no limit.
	MaxDepth int

	// mu protects the scheduler's internal state.
	mu sync.RWMutex
	// building tracks which tasks are currently being built (for cycle detection).
	// A task is marked as building when Build is called and unmarked when it returns.
	building map[K]bool
	// waiting tracks suspended tasks and their pending dependencies.
	// Key: waiting task, Value: dependencies it's waiting for.
	waiting map[K][]K
}

// NewSuspendingScheduler creates a new SuspendingScheduler with the given components.
func NewSuspendingScheduler[K comparable, V Value](
	rebuilder Rebuilder[K, V],
	tasks Tasks[K, V],
	store Store[any, K, V],
) *SuspendingScheduler[K, V] {
	return &SuspendingScheduler[K, V]{
		Rebuilder: rebuilder,
		Tasks:     tasks,
		Store:     store,
		MaxDepth:  1000, // Default max depth
		building:  make(map[K]bool),
		waiting:   make(map[K][]K),
	}
}

// Build implements Scheduler.Build for the suspending scheduler.
// The algorithm:
//  1. Check for cycles (fail if target is already being built)
//  2. Check if target should be rebuilt (using Rebuilder)
//  3. If not, return cached value from Store
//  4. Get the task definition from Tasks
//  5. Create a Fetch function that:
//     a. Checks if dependency value exists in Store
//     b. If yes and state is Succeeded, return it
//     c. If no or not ready, return SuspendError
//  6. Run the task with the Fetch function
//  7. If SuspendError returned:
//     a. Recursively build the missing dependency
//     b. Retry the original task
//  8. Store the result and return
func (s *SuspendingScheduler[K, V]) Build(ctx context.Context, target K) (V, error) {
	return s.buildWithDepth(ctx, target, 0, nil)
}

// buildWithDepth is the internal build function with depth tracking for stack overflow prevention.
func (s *SuspendingScheduler[K, V]) buildWithDepth(ctx context.Context, target K, depth int, path []K) (V, error) {
	var zero V

	// Check context cancellation
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}

	// Check depth limit
	if s.MaxDepth > 0 && depth >= s.MaxDepth {
		return zero, fmt.Errorf("maximum build depth exceeded (%d): possible infinite recursion for target %v", s.MaxDepth, target)
	}

	// Cycle detection: check if we're already building this target
	s.mu.Lock()
	if s.building[target] {
		// Found a cycle - construct the cycle path for error message
		cyclePath := append(path, target)
		s.mu.Unlock()
		return zero, NewCycleError(keysToStrings(cyclePath))
	}
	// Mark as building
	s.building[target] = true
	s.mu.Unlock()

	// Ensure we unmark building on return
	defer func() {
		s.mu.Lock()
		delete(s.building, target)
		s.mu.Unlock()
	}()

	// Get current value from store (may be zero value if not present)
	currentValue, hasValue := s.Store.GetValue(target)

	// Create a fetch function for the task
	// This is the "suspending" part - if a dependency isn't ready, we return SuspendError
	fetch := func(depKey K) (V, error) {
		return s.fetchDependency(ctx, depKey)
	}

	// Check if we need to rebuild using the Rebuilder
	shouldRebuild, err := s.Rebuilder.ShouldRebuild(ctx, target, currentValue, fetch)
	if err != nil {
		return zero, fmt.Errorf("failed to check rebuild for %v: %w", target, err)
	}

	// If no rebuild needed, return the cached value
	if !shouldRebuild {
		if hasValue {
			return currentValue, nil
		}
		// No value and shouldn't rebuild - this shouldn't happen normally
		// but indicates the rebuilder thinks it's done with no cached result
		return zero, nil
	}

	// Get the task definition
	task, found := s.Tasks.GetTask(target)
	if !found {
		return zero, NewTaskNotFoundError(Key(keyToString(target)))
	}

	// Mark as running in the store
	s.Store.SetState(target, TaskStateRunning)

	// Execute the task with retry logic for suspensions
	return s.executeWithRetry(ctx, target, task, depth, path)
}

// fetchDependency attempts to fetch a dependency value.
// Returns SuspendError if the dependency is not available.
func (s *SuspendingScheduler[K, V]) fetchDependency(_ context.Context, depKey K) (V, error) {
	var zero V

	// Get the dependency's current state
	state := s.Store.GetState(depKey)

	switch state {
	case TaskStateSucceeded:
		// Dependency completed successfully, return its value
		value, found := s.Store.GetValue(depKey)
		if found {
			return value, nil
		}
		// Value should exist for succeeded state, but if not, suspend
		return zero, NewSuspendErrorWithReason(Key(keyToString(depKey)), "dependency succeeded but value not found")

	case TaskStateFailed, TaskStateError:
		// Dependency failed. In some systems (like Argo), we still need the value
		// to evaluate "on exit" or "continue on fail" logic.
		// If value exists, return it.
		value, found := s.Store.GetValue(depKey)
		if found {
			return value, nil
		}
		// Dependency failed, propagate the failure
		return zero, NewDependencyError(
			"", // Current task key is not known in this context
			Key(keyToString(depKey)),
			fmt.Errorf("dependency %v is in %s state", depKey, state),
		)

	case TaskStateSkipped:
		// Dependency was skipped, this might be OK depending on use case
		// For now, return suspend to let the scheduler decide
		return zero, NewSuspendErrorWithReason(Key(keyToString(depKey)), "dependency was skipped")

	case TaskStateOmitted:
		// Dependency was omitted, similar to skipped
		return zero, NewSuspendErrorWithReason(Key(keyToString(depKey)), "dependency was omitted")

	case TaskStateRunning:
		// Dependency is running, suspend and wait
		return zero, NewSuspendErrorWithReason(Key(keyToString(depKey)), "dependency is running")

	case TaskStatePending:
		// Dependency not started, need to build it
		return zero, NewSuspendErrorWithReason(Key(keyToString(depKey)), "dependency not started")

	default:
		// Unknown state, treat as pending
		return zero, NewSuspendError(Key(keyToString(depKey)))
	}
}

// executeWithRetry executes a task and handles suspensions by building dependencies and retrying.
func (s *SuspendingScheduler[K, V]) executeWithRetry(
	ctx context.Context,
	target K,
	task *Task[K, V],
	depth int,
	path []K,
) (V, error) {
	var zero V

	// Track dependencies we're waiting on for this execution
	var waitingOn []K

	// Maximum retries to prevent infinite loops (one retry per unique dependency)
	maxRetries := 100 // Reasonable limit for dependency depth

	for retry := 0; retry <= maxRetries; retry++ {
		// Check context
		if ctx.Err() != nil {
			s.Store.SetState(target, TaskStateFailed)
			return zero, ctx.Err()
		}

		// Create fetch function that tracks suspensions
		var suspendedDeps []K
		fetch := func(depKey K) (V, error) {
			value, err := s.fetchDependency(ctx, depKey)
			if err != nil {
				if se, ok := IsSuspendError(err); ok {
					suspendedDeps = append(suspendedDeps, depKey)
					// Convert Key back to K type
					return zero, NewSuspendError(se.WaitingFor)
				}
			}
			return value, err
		}

		// Execute the task
		value, err := task.Run(fetch)

		// Handle the result
		if err == nil {
			// Task completed successfully
			s.Store.SetValue(target, value)
			s.Store.SetState(target, TaskStateSucceeded)

			// Clear waiting state
			s.mu.Lock()
			delete(s.waiting, target)
			s.mu.Unlock()

			return value, nil
		}

		// Check if this is a suspension
		if se, ok := IsSuspendError(err); ok {
			// Check for self-suspension (waiting for self execution)
			if se.WaitingFor == Key(keyToString(target)) {
				// Record waiting state
				s.mu.Lock()
				s.waiting[target] = []K{target}
				s.mu.Unlock()
				// Propagate suspension
				return zero, err
			}

			// Task suspended waiting for a dependency
			waitingOn = suspendedDeps
			if len(waitingOn) == 0 {
				// Add the dependency from the suspend error
				// Try to cast directly if K is string-compatible (Argo case)
				if key, ok := any(se.WaitingFor).(K); ok {
					waitingOn = append(waitingOn, key)
				} else {
					for _, k := range s.Store.ListKeys() {
						if keyToString(k) == se.WaitingFor {
							waitingOn = append(waitingOn, k)
							break
						}
					}
				}
			}

			// Record waiting state
			s.mu.Lock()
			s.waiting[target] = waitingOn
			s.mu.Unlock()
			s.Store.SetState(target, TaskStatePending) // Keep as pending until deps ready

			// Build all waiting dependencies
			for _, depKey := range waitingOn {
				// Build the dependency with increased depth
				newPath := append(path, target)
				_, depErr := s.buildWithDepth(ctx, depKey, depth+1, newPath)
				if depErr != nil {
					// Check if it's a cycle - need to propagate cycle errors
					if _, isCycle := IsCycleError(depErr); isCycle {
						s.Store.SetState(target, TaskStateFailed)
						return zero, depErr
					}
					// Other errors from dependency build
					if _, isDep := IsDependencyError(depErr); isDep {
						s.Store.SetState(target, TaskStateFailed)
						return zero, NewDependencyError(Key(keyToString(target)), Key(keyToString(depKey)), depErr)
					}
					// Propagate other errors
					s.Store.SetState(target, TaskStateFailed)
					return zero, fmt.Errorf("failed to build dependency %v for %v: %w", depKey, target, depErr)
				}
			}

			// Dependencies built, retry the task
			s.Store.SetState(target, TaskStateRunning)
			continue
		}

		// Check if this is a dependency failure (propagated from fetch)
		if de, ok := IsDependencyError(err); ok {
			s.Store.SetState(target, TaskStateFailed)
			return zero, de
		}

		// Actual task error (not a suspension)
		s.Store.SetState(target, TaskStateFailed)
		return zero, err
	}

	// Exceeded retry limit
	s.Store.SetState(target, TaskStateFailed)
	return zero, fmt.Errorf("exceeded maximum retries (%d) for target %v, waiting on: %v", maxRetries, target, waitingOn)
}

// BatchBuild builds multiple targets, returning results for all of them.
// This is useful for Argo's DAG where we want to evaluate all tasks at once.
// The results are returned in the same order as the targets.
//
// Unlike Build, BatchBuild:
//   - Continues building remaining targets even if one fails
//   - Returns a result for each target (success, failure, or suspended)
//   - Is suitable for the Argo controller's reconciliation loop
func (s *SuspendingScheduler[K, V]) BatchBuild(ctx context.Context, targets []K) []BuildResult[K, V] {
	results := make([]BuildResult[K, V], len(targets))

	for i, target := range targets {
		result := BuildResult[K, V]{
			Key: target,
		}

		value, err := s.Build(ctx, target)

		if err != nil {
			// Check error type
			if se, ok := IsSuspendError(err); ok {
				result.Suspended = true
				// Get waiting on from our tracking
				s.mu.RLock()
				if waiting, exists := s.waiting[target]; exists {
					result.WaitingOn = waiting
				} else {
					// Add the key from suspend error
					// Try to cast directly if K is string-compatible (Argo case)
					if key, ok := any(se.WaitingFor).(K); ok {
						result.WaitingOn = append(result.WaitingOn, key)
					} else {
						for _, k := range s.Store.ListKeys() {
							if keyToString(k) == se.WaitingFor {
								result.WaitingOn = append(result.WaitingOn, k)
								break
							}
						}
					}
				}
				s.mu.RUnlock()
				result.State = TaskStatePending
			} else if _, ok := IsCycleError(err); ok {
				result.Error = err
				result.State = TaskStateError
			} else if _, ok := IsDependencyError(err); ok {
				result.Error = err
				result.State = TaskStateFailed
			} else {
				result.Error = err
				result.State = TaskStateFailed
			}
		} else {
			result.Value = value
			result.State = s.Store.GetState(target)
		}

		results[i] = result
	}

	return results
}

// GetWaitingTasks returns a map of tasks that are suspended waiting for dependencies.
// Key is the waiting task, value is the list of dependencies it's waiting for.
func (s *SuspendingScheduler[K, V]) GetWaitingTasks() map[K][]K {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[K][]K, len(s.waiting))
	for k, v := range s.waiting {
		// Copy the slice to prevent external modifications
		waitingOn := make([]K, len(v))
		copy(waitingOn, v)
		result[k] = waitingOn
	}
	return result
}

// IsBuilding returns true if the given target is currently being built.
// This is useful for external code to check if a task is already in progress.
func (s *SuspendingScheduler[K, V]) IsBuilding(target K) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.building[target]
}

// Reset clears all internal state (building and waiting maps).
// This should be called between independent build sessions if the scheduler is reused.
func (s *SuspendingScheduler[K, V]) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.building = make(map[K]bool)
	s.waiting = make(map[K][]K)
}

// keysToStrings converts a slice of comparable keys to a slice of Key strings for error reporting.
func keysToStrings[K comparable](keys []K) []Key {
	result := make([]Key, len(keys))
	for i, k := range keys {
		result[i] = Key(keyToString(k))
	}
	return result
}

// TopologicalScheduler implements a simple topological scheduling strategy.
// Unlike the SuspendingScheduler, it requires all dependencies to be known upfront
// and computes a static execution order.
//
// This is NOT suitable for dynamic dependencies but is useful for:
//   - Simple DAGs with static dependency structure
//   - Debugging and comparison with the suspending scheduler
//   - Scenarios where dependencies don't change at runtime
type TopologicalScheduler[K comparable, V Value] struct {
	// Rebuilder determines whether tasks need to be rebuilt.
	Rebuilder Rebuilder[K, V]
	// Tasks contains the task definitions.
	Tasks Tasks[K, V]
	// Store provides persistent storage for values and build information.
	Store Store[any, K, V]
}

// NewTopologicalScheduler creates a new TopologicalScheduler.
func NewTopologicalScheduler[K comparable, V Value](
	rebuilder Rebuilder[K, V],
	tasks Tasks[K, V],
	store Store[any, K, V],
) *TopologicalScheduler[K, V] {
	return &TopologicalScheduler[K, V]{
		Rebuilder: rebuilder,
		Tasks:     tasks,
		Store:     store,
	}
}

// Build implements Scheduler.Build for the topological scheduler.
// It computes the dependency order upfront and executes tasks in that order.
func (s *TopologicalScheduler[K, V]) Build(ctx context.Context, target K) (V, error) {
	var zero V

	// Compute topological order starting from target
	order, err := s.computeOrder(ctx, target)
	if err != nil {
		return zero, err
	}

	// Execute tasks in order
	for _, key := range order {
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}

		// Get current value
		currentValue, _ := s.Store.GetValue(key)

		// Create fetch function (all deps should be available at this point)
		fetch := func(depKey K) (V, error) {
			value, found := s.Store.GetValue(depKey)
			if !found {
				return zero, NewSuspendErrorWithReason(Key(keyToString(depKey)), "dependency not available in topological scheduler")
			}
			return value, nil
		}

		// Check if rebuild needed
		shouldRebuild, err := s.Rebuilder.ShouldRebuild(ctx, key, currentValue, fetch)
		if err != nil {
			return zero, err
		}

		if !shouldRebuild {
			continue
		}

		// Get task and execute
		task, found := s.Tasks.GetTask(key)
		if !found {
			return zero, NewTaskNotFoundError(Key(keyToString(key)))
		}

		s.Store.SetState(key, TaskStateRunning)

		value, err := task.Run(fetch)
		if err != nil {
			s.Store.SetState(key, TaskStateFailed)
			return zero, err
		}

		s.Store.SetValue(key, value)
		s.Store.SetState(key, TaskStateSucceeded)
	}

	// Return target value
	value, found := s.Store.GetValue(target)
	if !found {
		return zero, fmt.Errorf("target %v was not built", target)
	}
	return value, nil
}

// computeOrder computes a topological order for building the target.
func (s *TopologicalScheduler[K, V]) computeOrder(ctx context.Context, target K) ([]K, error) {
	visited := make(map[any]bool)
	visiting := make(map[any]bool)
	var order []K

	var visit func(K) error
	visit = func(key K) error {
		if visited[key] {
			return nil
		}
		if visiting[key] {
			return NewCycleError([]Key{Key(keyToString(key))})
		}

		visiting[key] = true

		// Get dependencies
		deps, err := s.Tasks.GetDependencies(ctx, key)
		if err != nil {
			if _, ok := IsTaskNotFoundError(err); ok {
				// No task for this key, might be an input
				visited[key] = true
				delete(visiting, key)
				return nil
			}
			return err
		}

		// Visit dependencies first
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}

		delete(visiting, key)
		visited[key] = true
		order = append(order, key)
		return nil
	}

	if err := visit(target); err != nil {
		return nil, err
	}

	return order, nil
}
