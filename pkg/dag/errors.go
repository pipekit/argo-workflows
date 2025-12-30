package dag

import (
	"errors"
	"fmt"
)

// SuspendError indicates a task needs to suspend waiting for a dependency.
// In the suspending scheduler, when a task attempts to fetch a dependency
// that is not yet available, it returns this error to signal suspension.
// The scheduler then handles building the dependency and retrying the task.
type SuspendError struct {
	// WaitingFor is the key of the dependency that is not ready.
	WaitingFor Key
	// Reason provides additional context about why suspension occurred.
	Reason string
}

// Error implements the error interface.
func (e *SuspendError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("suspended waiting for %s: %s", e.WaitingFor, e.Reason)
	}
	return fmt.Sprintf("suspended waiting for %s", e.WaitingFor)
}

// NewSuspendError creates a new SuspendError with the given dependency key.
func NewSuspendError(waitingFor Key) *SuspendError {
	return &SuspendError{WaitingFor: waitingFor}
}

// NewSuspendErrorWithReason creates a new SuspendError with a reason.
func NewSuspendErrorWithReason(waitingFor Key, reason string) *SuspendError {
	return &SuspendError{WaitingFor: waitingFor, Reason: reason}
}

// IsSuspendError checks if an error is a suspension.
// Returns the SuspendError and true if the error is a SuspendError,
// otherwise returns nil and false.
func IsSuspendError(err error) (*SuspendError, bool) {
	var se *SuspendError
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}

// DependencyError indicates an error related to task dependencies.
type DependencyError struct {
	// Key is the task that has the dependency issue.
	Key Key
	// DependencyKey is the dependency that caused the error.
	DependencyKey Key
	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *DependencyError) Error() string {
	return fmt.Sprintf("dependency error for task %s: dependency %s: %v",
		e.Key, e.DependencyKey, e.Err)
}

// Unwrap returns the underlying error.
func (e *DependencyError) Unwrap() error {
	return e.Err
}

// NewDependencyError creates a new DependencyError.
func NewDependencyError(key, dependencyKey Key, err error) *DependencyError {
	return &DependencyError{
		Key:           key,
		DependencyKey: dependencyKey,
		Err:           err,
	}
}

// IsDependencyError checks if an error is a DependencyError.
func IsDependencyError(err error) (*DependencyError, bool) {
	var de *DependencyError
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

// TaskNotFoundError indicates a task was not found in the task collection.
type TaskNotFoundError struct {
	// Key is the task key that was not found.
	Key Key
}

// Error implements the error interface.
func (e *TaskNotFoundError) Error() string {
	return fmt.Sprintf("task not found: %s", e.Key)
}

// NewTaskNotFoundError creates a new TaskNotFoundError.
func NewTaskNotFoundError(key Key) *TaskNotFoundError {
	return &TaskNotFoundError{Key: key}
}

// IsTaskNotFoundError checks if an error is a TaskNotFoundError.
func IsTaskNotFoundError(err error) (*TaskNotFoundError, bool) {
	var tnfe *TaskNotFoundError
	if errors.As(err, &tnfe) {
		return tnfe, true
	}
	return nil, false
}

// CycleError indicates a dependency cycle was detected.
type CycleError struct {
	// Path contains the keys in the cycle.
	Path []Key
}

// Error implements the error interface.
func (e *CycleError) Error() string {
	return fmt.Sprintf("dependency cycle detected: %v", e.Path)
}

// NewCycleError creates a new CycleError.
func NewCycleError(path []Key) *CycleError {
	return &CycleError{Path: path}
}

// IsCycleError checks if an error is a CycleError.
func IsCycleError(err error) (*CycleError, bool) {
	var ce *CycleError
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}
