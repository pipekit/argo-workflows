package dag

import (
	"context"
)

// Fetch is a function that retrieves the value for a given key.
// It is the mechanism by which tasks declare their dependencies.
// In a suspending scheduler, Fetch may return a SuspendError
// if the requested dependency is not yet available.
//
// The fetch function is provided to tasks by the scheduler and
// handles the dependency resolution logic.
//
// Example usage within a task:
//
//	func(ctx context.Context, fetch Fetch[string, MyValue]) (MyValue, error) {
//	    // Fetch a dependency - may suspend if not ready
//	    depValue, err := fetch("dependency-key")
//	    if err != nil {
//	        return nil, err // Propagate error or suspension
//	    }
//	    // Use depValue to compute result
//	    return computeResult(depValue), nil
//	}
type Fetch[K comparable, V any] func(key K) (V, error)

// Task represents a computation that produces a value.
// This is the core abstraction from the Build Systems à la Carte paper.
//
// A Task contains:
//   - Key: The unique identifier for this task
//   - Dependencies: Static dependencies known before execution (may be empty for dynamic deps)
//   - Run: The computation function that produces the value
//
// The Run function receives a Fetch function to request dependencies.
// The Fetch calls implicitly declare the task's dependencies. For tasks
// with dynamic dependencies (determined at runtime), Dependencies may be
// empty and all dependencies are discovered through Fetch calls.
//
// Example:
//
//	task := &Task[string, *NodeOutput]{
//	    Key: "process-data",
//	    Dependencies: []string{"load-data"},  // Static dependency
//	    Run: func(fetch Fetch[string, *NodeOutput]) (*NodeOutput, error) {
//	        // Fetch the static dependency
//	        data, err := fetch("load-data")
//	        if err != nil {
//	            return nil, err
//	        }
//	        // Process and return result
//	        return processData(data), nil
//	    },
//	}
type Task[K comparable, V any] struct {
	// Key is the unique identifier for this task.
	Key K

	// Dependencies contains the static dependencies of this task.
	// These are dependencies known before task execution.
	// For tasks with dynamic dependencies, this may be empty.
	Dependencies []K

	// Run is the computation function that produces the task's value.
	// It receives a Fetch function to retrieve dependency values.
	// If a dependency is not available, Fetch returns a SuspendError.
	Run func(fetch Fetch[K, V]) (V, error)
}

// NewTask creates a new Task with the given parameters.
func NewTask[K comparable, V any](key K, deps []K, run func(fetch Fetch[K, V]) (V, error)) *Task[K, V] {
	return &Task[K, V]{
		Key:          key,
		Dependencies: deps,
		Run:          run,
	}
}

// Tasks is a collection of tasks that can be queried by key.
// This represents the "task description" in the Build Systems à la Carte framework.
//
// The interface supports both static and dynamic dependency discovery:
//   - GetTask: Retrieves the task definition for a key
//   - GetDependencies: Returns dependencies, potentially computing them dynamically
type Tasks[K comparable, V any] interface {
	// GetTask retrieves the task for a given key.
	// Returns the task and true if found, or nil and false if not found.
	GetTask(key K) (*Task[K, V], bool)

	// GetDependencies returns the dependencies for a task.
	// For static dependencies, this returns the task's Dependencies field.
	// For dynamic dependencies, this may compute dependencies based on
	// the current state or context.
	//
	// The context can be used for cancellation and deadline propagation.
	GetDependencies(ctx context.Context, key K) ([]K, error)
}

// MapTasks is a simple Tasks implementation backed by a map.
// This is suitable for static task collections where all tasks
// are known ahead of time.
type MapTasks[K comparable, V any] struct {
	tasks map[K]*Task[K, V]
}

// NewMapTasks creates a new MapTasks from a slice of tasks.
func NewMapTasks[K comparable, V any](taskList []*Task[K, V]) *MapTasks[K, V] {
	tasks := make(map[K]*Task[K, V], len(taskList))
	for _, t := range taskList {
		tasks[t.Key] = t
	}
	return &MapTasks[K, V]{tasks: tasks}
}

// GetTask retrieves the task for a given key.
func (m *MapTasks[K, V]) GetTask(key K) (*Task[K, V], bool) {
	t, ok := m.tasks[key]
	return t, ok
}

// GetDependencies returns the static dependencies for a task.
func (m *MapTasks[K, V]) GetDependencies(_ context.Context, key K) ([]K, error) {
	t, ok := m.tasks[key]
	if !ok {
		return nil, NewTaskNotFoundError(Key(keyToString(key)))
	}
	return t.Dependencies, nil
}

// AddTask adds a task to the collection.
func (m *MapTasks[K, V]) AddTask(task *Task[K, V]) {
	m.tasks[task.Key] = task
}

// RemoveTask removes a task from the collection.
func (m *MapTasks[K, V]) RemoveTask(key K) {
	delete(m.tasks, key)
}

// Keys returns all task keys in the collection.
func (m *MapTasks[K, V]) Keys() []K {
	keys := make([]K, 0, len(m.tasks))
	for k := range m.tasks {
		keys = append(keys, k)
	}
	return keys
}

// keyToString is a helper to convert any comparable key to a string for errors.
// This uses a type switch to handle common cases.
func keyToString[K comparable](key K) string {
	// Key is a type alias for string, so we only need to check for string
	if v, ok := any(key).(string); ok {
		return v
	}
	return ""
}

// FuncTasks wraps a function as a Tasks implementation.
// This is useful when tasks are generated dynamically or
// when integrating with existing code that uses a function-based approach.
type FuncTasks[K comparable, V any] struct {
	// GetTaskFunc retrieves a task by key.
	GetTaskFunc func(key K) (*Task[K, V], bool)
	// GetDependenciesFunc retrieves dependencies for a task.
	// If nil, dependencies are retrieved from the task's Dependencies field.
	GetDependenciesFunc func(ctx context.Context, key K) ([]K, error)
}

// GetTask retrieves the task for a given key.
func (f *FuncTasks[K, V]) GetTask(key K) (*Task[K, V], bool) {
	if f.GetTaskFunc == nil {
		return nil, false
	}
	return f.GetTaskFunc(key)
}

// GetDependencies returns the dependencies for a task.
func (f *FuncTasks[K, V]) GetDependencies(ctx context.Context, key K) ([]K, error) {
	if f.GetDependenciesFunc != nil {
		return f.GetDependenciesFunc(ctx, key)
	}
	// Fall back to static dependencies from task
	t, ok := f.GetTaskFunc(key)
	if !ok {
		return nil, NewTaskNotFoundError(Key(keyToString(key)))
	}
	return t.Dependencies, nil
}
