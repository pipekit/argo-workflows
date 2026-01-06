// Package dag implements the Build Systems à la Carte framework for DAG dependency evaluation.
// This provides a principled approach to task scheduling and execution based on the paper
// "Build Systems à la Carte" by Mokhov, Mitchell, and Peyton Jones.
//
// The framework decomposes build systems into two orthogonal components:
//   - Scheduler: Determines the order in which tasks are executed
//   - Rebuilder: Determines whether a task needs to be re-executed
//
// This implementation uses a suspending scheduler which handles dynamic dependencies
// efficiently by suspending task execution when dependencies are not yet available.
package dag

// Key uniquely identifies a task in the DAG.
// In Argo Workflows context, this maps to a task name or node ID.
type Key = string

// Hash represents a content hash for verifying task inputs/outputs.
// Used for change detection by rebuilders.
type Hash = string

// TaskState represents the execution state of a task.
type TaskState int

const (
	// TaskStatePending indicates the task has not started yet.
	TaskStatePending TaskState = iota
	// TaskStateRunning indicates the task is currently executing.
	TaskStateRunning
	// TaskStateSucceeded indicates the task completed successfully.
	TaskStateSucceeded
	// TaskStateFailed indicates the task failed during execution.
	TaskStateFailed
	// TaskStateSkipped indicates the task was intentionally skipped.
	TaskStateSkipped
	// TaskStateOmitted indicates the task was omitted from execution.
	TaskStateOmitted
	// TaskStateError indicates an error occurred with the task.
	TaskStateError
)

// String returns the string representation of the TaskState.
func (s TaskState) String() string {
	switch s {
	case TaskStatePending:
		return "Pending"
	case TaskStateRunning:
		return "Running"
	case TaskStateSucceeded:
		return "Succeeded"
	case TaskStateFailed:
		return "Failed"
	case TaskStateSkipped:
		return "Skipped"
	case TaskStateOmitted:
		return "Omitted"
	case TaskStateError:
		return "Error"
	default:
		return "Unknown"
	}
}

// IsComplete returns true if the task is in a terminal state.
func (s TaskState) IsComplete() bool {
	switch s {
	case TaskStateSucceeded, TaskStateFailed, TaskStateSkipped, TaskStateOmitted, TaskStateError:
		return true
	default:
		return false
	}
}

// IsSuccessful returns true if the task completed successfully.
func (s TaskState) IsSuccessful() bool {
	return s == TaskStateSucceeded
}

// Value represents the output of a task execution.
// Types implementing Value can be stored in a Store and used
// for change detection through their Hash method.
type Value interface {
	// Hash returns a content hash of the value for change detection.
	// The hash should be deterministic and reflect the meaningful
	// content of the value.
	Hash() Hash
}

// StringValue is a simple Value implementation for string data.
// This is useful for testing and simple use cases.
type StringValue string

// Hash returns the string itself as its hash.
func (v StringValue) Hash() Hash {
	return Hash(v)
}

// NewStringValue creates a new StringValue.
func NewStringValue(s string) StringValue {
	return StringValue(s)
}
