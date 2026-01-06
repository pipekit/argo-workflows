// Package dag implements the Build Systems à la Carte framework for DAG dependency evaluation.
package dag

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper types and functions for testing ---

// testValue implements Value for testing purposes.
type testValue struct {
	data string
}

func (v testValue) Hash() Hash {
	return Hash(v.data)
}

func newTestValue(data string) testValue {
	return testValue{data: data}
}

// createTestScheduler creates a scheduler with the given tasks and an AlwaysRebuildRebuilder.
func createTestScheduler(tasks []*Task[Key, testValue]) *SuspendingScheduler[Key, testValue] {
	mapTasks := NewMapTasks(tasks)
	store := NewMapStore[any, Key, testValue](nil)
	rebuilder := NewAlwaysRebuildRebuilder[Key, testValue]()
	return NewSuspendingScheduler(rebuilder, mapTasks, store)
}

// createTestSchedulerWithStore creates a scheduler with a specific store.
func createTestSchedulerWithStore(tasks []*Task[Key, testValue], store *MapStore[any, Key, testValue]) *SuspendingScheduler[Key, testValue] {
	mapTasks := NewMapTasks(tasks)
	rebuilder := NewAlwaysRebuildRebuilder[Key, testValue]()
	return NewSuspendingScheduler(rebuilder, mapTasks, store)
}

// --- Tests for SuspendingScheduler ---

func TestSuspendingScheduler_BasicBuild(t *testing.T) {
	t.Run("single task with no dependencies", func(t *testing.T) {
		// Create a single task that produces a value
		task := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-A"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{task})

		ctx := context.Background()
		result, err := scheduler.Build(ctx, "A")

		require.NoError(t, err)
		assert.Equal(t, "result-A", result.data)

		// Verify state is Succeeded
		state := scheduler.Store.GetState(ctx, "A")
		assert.Equal(t, TaskStateSucceeded, state)

		// Verify value is stored
		stored, ok := scheduler.Store.GetValue(ctx, "A")
		assert.True(t, ok)
		assert.Equal(t, "result-A", stored.data)
	})

	t.Run("task not found returns error", func(t *testing.T) {
		scheduler := createTestScheduler(nil)

		ctx := context.Background()
		_, err := scheduler.Build(ctx, "nonexistent")

		require.Error(t, err)
		tnfe, ok := IsTaskNotFoundError(err)
		require.True(t, ok, "expected TaskNotFoundError, got %T: %v", err, err)
		assert.Equal(t, "nonexistent", tnfe.Key)
	})

	t.Run("context cancellation stops build", func(t *testing.T) {
		// Create a task that blocks
		blockCh := make(chan struct{})
		task := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			<-blockCh
			return newTestValue("result-A"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{task})

		ctx, cancel := context.WithCancel(context.Background())

		// Start build in goroutine
		var buildErr error
		done := make(chan struct{})
		go func() {
			_, buildErr = scheduler.Build(ctx, "A")
			close(done)
		}()

		// Cancel context
		cancel()

		// Unblock the task
		close(blockCh)

		select {
		case <-done:
			require.Error(t, buildErr)
			assert.Equal(t, context.Canceled, buildErr)
		case <-time.After(time.Second):
			t.Fatal("build didn't complete after cancellation")
		}
	})
}

func TestSuspendingScheduler_LinearChain(t *testing.T) {
	t.Run("A -> B -> C chain builds in order", func(t *testing.T) {
		// Track execution order
		var executionOrder []string
		var mu sync.Mutex

		// Task A: no dependencies
		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "A")
			mu.Unlock()
			return newTestValue("result-A"), nil
		})

		// Task B: depends on A
		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			mu.Lock()
			executionOrder = append(executionOrder, "B")
			mu.Unlock()
			return newTestValue(valA.data + "-B"), nil
		})

		// Task C: depends on B
		taskC := NewTask("C", []Key{"B"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valB, err := fetch("B")
			if err != nil {
				return testValue{}, err
			}
			mu.Lock()
			executionOrder = append(executionOrder, "C")
			mu.Unlock()
			return newTestValue(valB.data + "-C"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB, taskC})

		ctx := context.Background()
		result, err := scheduler.Build(ctx, "C")

		require.NoError(t, err)
		assert.Equal(t, "result-A-B-C", result.data)

		// Verify execution order: A, then B, then C
		assert.Equal(t, []string{"A", "B", "C"}, executionOrder)

		// Verify all tasks succeeded
		assert.Equal(t, TaskStateSucceeded, scheduler.Store.GetState(ctx, "A"))
		assert.Equal(t, TaskStateSucceeded, scheduler.Store.GetState(ctx, "B"))
		assert.Equal(t, TaskStateSucceeded, scheduler.Store.GetState(ctx, "C"))
	})

	t.Run("building middle of chain fetches upstream", func(t *testing.T) {
		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-A"), nil
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue(valA.data + "-B"), nil
		})

		taskC := NewTask("C", []Key{"B"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valB, err := fetch("B")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue(valB.data + "-C"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB, taskC})

		ctx := context.Background()

		// Build B directly
		result, err := scheduler.Build(ctx, "B")

		require.NoError(t, err)
		assert.Equal(t, "result-A-B", result.data)

		// A should also be built
		assert.Equal(t, TaskStateSucceeded, scheduler.Store.GetState(ctx, "A"))
	})
}

func TestSuspendingScheduler_DiamondDependency(t *testing.T) {
	t.Run("diamond DAG: A -> B,C -> D", func(t *testing.T) {
		//     A
		//    / \
		//   B   C
		//    \ /
		//     D
		var executionCount int32

		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			atomic.AddInt32(&executionCount, 1)
			return newTestValue("A"), nil
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			atomic.AddInt32(&executionCount, 1)
			return newTestValue(valA.data + "-B"), nil
		})

		taskC := NewTask("C", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			atomic.AddInt32(&executionCount, 1)
			return newTestValue(valA.data + "-C"), nil
		})

		taskD := NewTask("D", []Key{"B", "C"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valB, err := fetch("B")
			if err != nil {
				return testValue{}, err
			}
			valC, err := fetch("C")
			if err != nil {
				return testValue{}, err
			}
			atomic.AddInt32(&executionCount, 1)
			return newTestValue(valB.data + "+" + valC.data + "-D"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB, taskC, taskD})

		ctx := context.Background()
		result, err := scheduler.Build(ctx, "D")

		require.NoError(t, err)

		// Result should combine both B and C
		assert.Contains(t, result.data, "A-B")
		assert.Contains(t, result.data, "A-C")
		assert.Contains(t, result.data, "-D")

		// Each task should be executed
		assert.Equal(t, int32(4), atomic.LoadInt32(&executionCount))

		// All tasks should be succeeded
		for _, key := range []Key{"A", "B", "C", "D"} {
			assert.Equal(t, TaskStateSucceeded, scheduler.Store.GetState(ctx, key), "task %s should be succeeded", key)
		}
	})
}

func TestSuspendingScheduler_ParallelTasks(t *testing.T) {
	t.Run("independent tasks can be built independently", func(t *testing.T) {
		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-A"), nil
		})

		taskB := NewTask("B", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-B"), nil
		})

		taskC := NewTask("C", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-C"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB, taskC})

		ctx := context.Background()

		// Build all independently
		resultA, err := scheduler.Build(ctx, "A")
		require.NoError(t, err)
		assert.Equal(t, "result-A", resultA.data)

		resultB, err := scheduler.Build(ctx, "B")
		require.NoError(t, err)
		assert.Equal(t, "result-B", resultB.data)

		resultC, err := scheduler.Build(ctx, "C")
		require.NoError(t, err)
		assert.Equal(t, "result-C", resultC.data)
	})

	t.Run("tasks with shared dependency", func(t *testing.T) {
		var aCount int32

		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			atomic.AddInt32(&aCount, 1)
			return newTestValue("result-A"), nil
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue(valA.data + "-B"), nil
		})

		taskC := NewTask("C", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue(valA.data + "-C"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB, taskC})

		ctx := context.Background()

		// Build B and C
		resultB, err := scheduler.Build(ctx, "B")
		require.NoError(t, err)
		assert.Equal(t, "result-A-B", resultB.data)

		// Reset scheduler for second build
		scheduler.Reset()

		resultC, err := scheduler.Build(ctx, "C")
		require.NoError(t, err)
		assert.Equal(t, "result-A-C", resultC.data)
	})
}

func TestSuspendingScheduler_CycleDetection(t *testing.T) {
	t.Run("direct cycle: A -> B -> A", func(t *testing.T) {
		taskA := NewTask("A", []Key{"B"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			_, err := fetch("B")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue("A"), nil
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			_, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue("B"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB})

		ctx := context.Background()
		_, err := scheduler.Build(ctx, "A")

		require.Error(t, err)
		ce, ok := IsCycleError(err)
		require.True(t, ok, "expected CycleError, got %T: %v", err, err)
		assert.NotEmpty(t, ce.Path)
	})

	t.Run("indirect cycle: A -> B -> C -> A", func(t *testing.T) {
		taskA := NewTask("A", []Key{"C"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			_, err := fetch("C")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue("A"), nil
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			_, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue("B"), nil
		})

		taskC := NewTask("C", []Key{"B"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			_, err := fetch("B")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue("C"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB, taskC})

		ctx := context.Background()
		_, err := scheduler.Build(ctx, "A")

		require.Error(t, err)
		ce, ok := IsCycleError(err)
		require.True(t, ok, "expected CycleError, got %T: %v", err, err)
		assert.NotEmpty(t, ce.Path)
	})

	t.Run("self cycle: A -> A", func(t *testing.T) {
		taskA := NewTask("A", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			_, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue("A"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA})

		ctx := context.Background()
		_, err := scheduler.Build(ctx, "A")

		require.Error(t, err)
		_, ok := IsCycleError(err)
		require.True(t, ok, "expected CycleError, got %T: %v", err, err)
	})
}

func TestSuspendingScheduler_DepthLimiting(t *testing.T) {
	t.Run("exceeds max depth", func(t *testing.T) {
		// Create a very deep chain
		const depth = 10
		tasks := make([]*Task[Key, testValue], depth)

		for i := 0; i < depth; i++ {
			idx := i
			var deps []Key
			if i > 0 {
				deps = []Key{Key(string(rune('A' + i - 1)))}
			}
			tasks[i] = NewTask(Key(string(rune('A'+i))), deps, func(fetch Fetch[Key, testValue]) (testValue, error) {
				if idx > 0 {
					depKey := Key(string(rune('A' + idx - 1)))
					_, err := fetch(depKey)
					if err != nil {
						return testValue{}, err
					}
				}
				return newTestValue(string(rune('A' + idx))), nil
			})
		}

		scheduler := createTestScheduler(tasks)
		scheduler.MaxDepth = 5 // Set a low max depth

		ctx := context.Background()
		_, err := scheduler.Build(ctx, Key(string(rune('A'+depth-1))))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "maximum build depth exceeded")
	})

	t.Run("within max depth succeeds", func(t *testing.T) {
		// Create a chain within limits
		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("A"), nil
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue(valA.data + "-B"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB})
		scheduler.MaxDepth = 10 // Sufficient depth

		ctx := context.Background()
		result, err := scheduler.Build(ctx, "B")

		require.NoError(t, err)
		assert.Equal(t, "A-B", result.data)
	})
}

func TestSuspendingScheduler_SuspendAndResume(t *testing.T) {
	t.Run("task suspends when dependency not ready", func(t *testing.T) {
		store := NewMapStore[any, Key, testValue](nil)

		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-A"), nil
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue(valA.data + "-B"), nil
		})

		scheduler := createTestSchedulerWithStore([]*Task[Key, testValue]{taskA, taskB}, store)

		ctx := context.Background()

		// Build B - should trigger A first due to suspension mechanism
		result, err := scheduler.Build(ctx, "B")

		require.NoError(t, err)
		assert.Equal(t, "result-A-B", result.data)

		// Both should be succeeded now
		assert.Equal(t, TaskStateSucceeded, store.GetState(ctx, "A"))
		assert.Equal(t, TaskStateSucceeded, store.GetState(ctx, "B"))
	})

	t.Run("multiple suspensions resolve correctly", func(t *testing.T) {
		var buildOrder []string
		var mu sync.Mutex

		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			mu.Lock()
			buildOrder = append(buildOrder, "A")
			mu.Unlock()
			return newTestValue("A"), nil
		})

		taskB := NewTask("B", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			mu.Lock()
			buildOrder = append(buildOrder, "B")
			mu.Unlock()
			return newTestValue("B"), nil
		})

		taskC := NewTask("C", []Key{"A", "B"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			valB, err := fetch("B")
			if err != nil {
				return testValue{}, err
			}
			mu.Lock()
			buildOrder = append(buildOrder, "C")
			mu.Unlock()
			return newTestValue(valA.data + "-" + valB.data + "-C"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB, taskC})

		ctx := context.Background()
		result, err := scheduler.Build(ctx, "C")

		require.NoError(t, err)
		assert.Contains(t, result.data, "A")
		assert.Contains(t, result.data, "B")
		assert.Contains(t, result.data, "C")

		// C should be built last
		assert.Equal(t, "C", buildOrder[len(buildOrder)-1])
	})
}

func TestSuspendingScheduler_ErrorPropagation(t *testing.T) {
	t.Run("task error is propagated", func(t *testing.T) {
		expectedErr := errors.New("task A failed")

		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return testValue{}, expectedErr
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA})

		ctx := context.Background()
		_, err := scheduler.Build(ctx, "A")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "task A failed")

		// State should be failed
		assert.Equal(t, TaskStateFailed, scheduler.Store.GetState(ctx, "A"))
	})

	t.Run("dependency error propagates to dependent", func(t *testing.T) {
		expectedErr := errors.New("task A failed")

		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return testValue{}, expectedErr
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			return newTestValue(valA.data + "-B"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB})

		ctx := context.Background()
		_, err := scheduler.Build(ctx, "B")

		require.Error(t, err)

		// Both should be in failed state
		assert.Equal(t, TaskStateFailed, scheduler.Store.GetState(ctx, "A"))
		assert.Equal(t, TaskStateFailed, scheduler.Store.GetState(ctx, "B"))
	})
}

func TestSuspendingScheduler_BatchBuild(t *testing.T) {
	t.Run("batch build multiple targets", func(t *testing.T) {
		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-A"), nil
		})

		taskB := NewTask("B", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-B"), nil
		})

		taskC := NewTask("C", []Key{"A", "B"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, _ := fetch("A")
			valB, _ := fetch("B")
			return newTestValue(valA.data + "-" + valB.data + "-C"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB, taskC})

		ctx := context.Background()
		results := scheduler.BatchBuild(ctx, []Key{"A", "B", "C"})

		require.Len(t, results, 3)

		// Check each result
		for _, r := range results {
			assert.NoError(t, r.Error)
			assert.Equal(t, TaskStateSucceeded, r.State)
			assert.False(t, r.Suspended)
		}
	})

	t.Run("batch build with failures continues", func(t *testing.T) {
		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-A"), nil
		})

		taskB := NewTask("B", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return testValue{}, errors.New("B failed")
		})

		taskC := NewTask("C", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-C"), nil
		})

		scheduler := createTestScheduler([]*Task[Key, testValue]{taskA, taskB, taskC})

		ctx := context.Background()
		results := scheduler.BatchBuild(ctx, []Key{"A", "B", "C"})

		require.Len(t, results, 3)

		// A should succeed
		assert.NoError(t, results[0].Error)
		assert.Equal(t, TaskStateSucceeded, results[0].State)

		// B should fail
		assert.Error(t, results[1].Error)
		assert.Equal(t, TaskStateFailed, results[1].State)

		// C should succeed (independent of B)
		assert.NoError(t, results[2].Error)
		assert.Equal(t, TaskStateSucceeded, results[2].State)
	})
}

func TestSuspendingScheduler_Reset(t *testing.T) {
	t.Run("reset clears building and waiting state", func(t *testing.T) {
		scheduler := createTestScheduler(nil)

		// Manually set some state
		scheduler.mu.Lock()
		scheduler.building["A"] = true
		scheduler.waiting["B"] = []Key{"A"}
		scheduler.mu.Unlock()

		// Reset
		scheduler.Reset()

		// Verify state is cleared
		scheduler.mu.RLock()
		assert.Empty(t, scheduler.building)
		assert.Empty(t, scheduler.waiting)
		scheduler.mu.RUnlock()
	})
}

func TestSuspendingScheduler_GetWaitingTasks(t *testing.T) {
	t.Run("returns waiting tasks", func(t *testing.T) {
		scheduler := createTestScheduler(nil)

		// Manually set waiting state
		scheduler.mu.Lock()
		scheduler.waiting["C"] = []Key{"A", "B"}
		scheduler.waiting["D"] = []Key{"C"}
		scheduler.mu.Unlock()

		waiting := scheduler.GetWaitingTasks()

		assert.Len(t, waiting, 2)
		assert.Equal(t, []Key{"A", "B"}, waiting["C"])
		assert.Equal(t, []Key{"C"}, waiting["D"])
	})

	t.Run("returns copy of waiting state", func(t *testing.T) {
		scheduler := createTestScheduler(nil)

		scheduler.mu.Lock()
		scheduler.waiting["A"] = []Key{"B"}
		scheduler.mu.Unlock()

		waiting := scheduler.GetWaitingTasks()

		// Modify the returned slice
		waiting["A"] = append(waiting["A"], "C")

		// Original should be unchanged
		scheduler.mu.RLock()
		assert.Equal(t, []Key{"B"}, scheduler.waiting["A"])
		scheduler.mu.RUnlock()
	})
}

func TestSuspendingScheduler_IsBuilding(t *testing.T) {
	t.Run("returns true when building", func(t *testing.T) {
		scheduler := createTestScheduler(nil)

		scheduler.mu.Lock()
		scheduler.building["A"] = true
		scheduler.mu.Unlock()

		assert.True(t, scheduler.IsBuilding("A"))
		assert.False(t, scheduler.IsBuilding("B"))
	})
}

// --- Tests for TopologicalScheduler ---

func TestTopologicalScheduler_BasicBuild(t *testing.T) {
	t.Run("linear chain builds in order", func(t *testing.T) {
		var executionOrder []string
		var mu sync.Mutex

		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "A")
			mu.Unlock()
			return newTestValue("A"), nil
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valA, err := fetch("A")
			if err != nil {
				return testValue{}, err
			}
			mu.Lock()
			executionOrder = append(executionOrder, "B")
			mu.Unlock()
			return newTestValue(valA.data + "-B"), nil
		})

		taskC := NewTask("C", []Key{"B"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			valB, err := fetch("B")
			if err != nil {
				return testValue{}, err
			}
			mu.Lock()
			executionOrder = append(executionOrder, "C")
			mu.Unlock()
			return newTestValue(valB.data + "-C"), nil
		})

		tasks := NewMapTasks([]*Task[Key, testValue]{taskA, taskB, taskC})
		store := NewMapStore[any, Key, testValue](nil)
		rebuilder := NewAlwaysRebuildRebuilder[Key, testValue]()
		scheduler := NewTopologicalScheduler(rebuilder, tasks, store)

		ctx := context.Background()
		result, err := scheduler.Build(ctx, "C")

		require.NoError(t, err)
		assert.Equal(t, "A-B-C", result.data)
		assert.Equal(t, []string{"A", "B", "C"}, executionOrder)
	})

	t.Run("cycle detection", func(t *testing.T) {
		taskA := NewTask("A", []Key{"B"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return testValue{}, nil
		})

		taskB := NewTask("B", []Key{"A"}, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return testValue{}, nil
		})

		tasks := NewMapTasks([]*Task[Key, testValue]{taskA, taskB})
		store := NewMapStore[any, Key, testValue](nil)
		rebuilder := NewAlwaysRebuildRebuilder[Key, testValue]()
		scheduler := NewTopologicalScheduler(rebuilder, tasks, store)

		ctx := context.Background()
		_, err := scheduler.Build(ctx, "A")

		require.Error(t, err)
		_, ok := IsCycleError(err)
		assert.True(t, ok, "expected CycleError")
	})
}

// --- Tests for Build struct ---

func TestBuild_Run(t *testing.T) {
	t.Run("build runs scheduler", func(t *testing.T) {
		taskA := NewTask("A", nil, func(fetch Fetch[Key, testValue]) (testValue, error) {
			return newTestValue("result-A"), nil
		})

		tasks := NewMapTasks([]*Task[Key, testValue]{taskA})
		store := NewMapStore[any, Key, testValue](nil)
		rebuilder := NewAlwaysRebuildRebuilder[Key, testValue]()
		scheduler := NewSuspendingScheduler(rebuilder, tasks, store)

		build := &Build[Key, testValue]{
			Scheduler: scheduler,
			Rebuilder: rebuilder,
			Tasks:     tasks,
			Store:     store,
		}

		ctx := context.Background()
		result, err := build.Run(ctx, "A")

		require.NoError(t, err)
		assert.Equal(t, "result-A", result.data)
	})
}

// --- Tests for BuildResult ---

func TestBuildResult(t *testing.T) {
	t.Run("result fields", func(t *testing.T) {
		result := BuildResult[Key, testValue]{
			Key:       "test",
			Value:     newTestValue("value"),
			State:     TaskStateSucceeded,
			Error:     nil,
			Suspended: false,
			WaitingOn: nil,
		}

		assert.Equal(t, "test", result.Key)
		assert.Equal(t, "value", result.Value.data)
		assert.Equal(t, TaskStateSucceeded, result.State)
		assert.NoError(t, result.Error)
		assert.False(t, result.Suspended)
		assert.Nil(t, result.WaitingOn)
	})
}
