# Build Systems à la Carte: DAG Evaluation Framework

This package implements the theoretical framework from the paper "Build Systems à la Carte" by Andrey Mokhov, Neil Mitchell, and Simon Peyton Jones. The implementation provides a principled approach to DAG dependency evaluation that can serve as the foundation for Argo Workflows' DAG controller.

## Table of Contents

1. [Overview](#overview)
2. [Core Abstractions](#core-abstractions)
3. [Type Definitions](#type-definitions)
4. [Suspending Scheduler Design](#suspending-scheduler-design)
5. [Rebuilder Strategies](#rebuilder-strategies)
6. [Integration with Argo Workflows](#integration-with-argo-workflows)
7. [Example Usage](#example-usage)

---

## Overview

### Build Systems à la Carte Framework

The paper identifies that all build systems can be decomposed into two orthogonal components:

1. **Scheduler**: Determines the order in which tasks are executed
2. **Rebuilder**: Determines whether a task needs to be re-executed

This decomposition allows us to reason about and implement build systems in a modular way.

### Key Schedulers

| Scheduler | Description | Argo Fit |
|-----------|-------------|----------|
| **Topological** | Pre-computes execution order from static dependencies | Not suitable - Argo has dynamic dependencies |
| **Restarting** | Starts tasks, restarts from beginning if dependency missing | Inefficient for workflows |
| **Suspending** | Suspends task execution when dependency isn't ready | **Ideal for Argo Workflows** |

### Why Suspending Scheduler?

The suspending scheduler is ideal for Argo Workflows because:

- **Dynamic Dependencies**: Dependencies can be computed based on runtime values
- **Efficient**: Tasks suspend rather than restart when dependencies are unavailable
- **Stateful**: Naturally maps to Kubernetes pod states (Pending, Running, Succeeded, Failed)
- **Composable**: Can be combined with different rebuilder strategies

---

## Core Abstractions

The framework is built around these core concepts:

```
┌──────────────────────────────────────────────────────────────────┐
│                         Build System                              │
├──────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐                    ┌─────────────────────┐  │
│  │    Scheduler    │                    │     Rebuilder       │  │
│  │                 │                    │                     │  │
│  │ - Topological   │                    │ - Dirty Bit         │  │
│  │ - Restarting    │                    │ - Verifying Traces  │  │
│  │ - Suspending ◄──┼────────────────────┼── Constructive      │  │
│  │                 │                    │                     │  │
│  └────────┬────────┘                    └──────────┬──────────┘  │
│           │                                        │             │
│           │         ┌──────────────┐              │             │
│           └────────►│    Store     │◄─────────────┘             │
│                     │              │                             │
│                     │ - Values     │                             │
│                     │ - Info       │                             │
│                     └──────┬───────┘                             │
│                            │                                     │
│                     ┌──────▼───────┐                             │
│                     │    Tasks     │                             │
│                     │              │                             │
│                     │ Key -> Value │                             │
│                     └──────────────┘                             │
└──────────────────────────────────────────────────────────────────┘
```

### Components

1. **Task[K, V]**: A computation that produces a value `V` for a key `K`, potentially depending on other tasks
2. **Store[I, K, V]**: A key-value store with additional build information `I`
3. **Scheduler**: Orchestrates task execution order
4. **Rebuilder**: Decides if a task needs to be rebuilt

---

## Type Definitions

### Core Types

```go
package dag

import (
    "context"
)

// Key uniquely identifies a task in the DAG.
// In Argo Workflows context, this maps to a task name or node ID.
type Key = string

// Hash represents a content hash for verifying task inputs/outputs.
type Hash = string

// TaskState represents the execution state of a task.
type TaskState int

const (
    TaskStatePending TaskState = iota
    TaskStateRunning
    TaskStateSucceeded
    TaskStateFailed
    TaskStateSkipped
    TaskStateOmitted
    TaskStateSuspended
)

// Value represents the output of a task execution.
// This is generic to support different value types.
type Value interface {
    // Hash returns a content hash of the value for change detection.
    Hash() Hash
}
```

### Task Definition

```go
// Fetch is a function that retrieves the value for a given key.
// It is the mechanism by which tasks declare their dependencies.
// In a suspending scheduler, Fetch may suspend the current task
// if the requested dependency is not yet available.
type Fetch[V Value] func(ctx context.Context, key Key) (V, error)

// Task represents a computation that, given a way to fetch dependencies,
// produces a value. This is the core abstraction from the paper.
//
// The Task signature captures the essence of dependency-based computation:
// - It receives a Fetch function to request dependencies
// - It produces a value V or an error
// - The Fetch calls implicitly declare the task's dependencies
//
// Example:
//
//     task := func(ctx context.Context, fetch Fetch[NodeOutput]) (NodeOutput, error) {
//         // Declare dependency on task-a
//         depA, err := fetch(ctx, "task-a")
//         if err != nil {
//             return nil, err // Suspends if not ready
//         }
//         // Use depA to compute result
//         return computeOutput(depA), nil
//     }
type Task[V Value] func(ctx context.Context, fetch Fetch[V]) (V, error)

// Tasks is a mapping from keys to their task definitions.
// This represents the build rules of the system.
type Tasks[V Value] func(key Key) Task[V]
```

### Store Abstraction

```go
// Info holds build-related metadata for the store.
// Different rebuilder strategies require different Info types:
// - Dirty bit: just a boolean per key
// - Verifying traces: input hashes
// - Constructive traces: full dependency traces
type Info interface{}

// Store provides persistent storage for task values and build information.
// It is parameterized by:
// - I: The type of build information (varies by rebuilder)
// - V: The type of values stored
type Store[I Info, V Value] interface {
    // GetValue retrieves the stored value for a key.
    // Returns nil if no value exists.
    GetValue(key Key) V

    // PutValue stores a value for a key.
    PutValue(key Key, value V)

    // GetInfo retrieves build information for a key.
    GetInfo(key Key) I

    // PutInfo stores build information for a key.
    PutInfo(key Key, info I)

    // GetState returns the current execution state of a task.
    GetState(key Key) TaskState

    // SetState updates the execution state of a task.
    SetState(key Key, state TaskState)

    // ListKeys returns all keys in the store.
    ListKeys() []Key
}
```

### Rebuilder Interface

```go
// Rebuilder determines whether a task needs to be rebuilt based on
// the current store state and the task definition.
//
// The paper identifies several rebuilder strategies:
// - Dirty Bit: Rebuild if any dependency has changed
// - Verifying Traces: Rebuild if input hashes differ from traces
// - Constructive Traces: Reuse outputs if inputs match previous build
//
// For Argo Workflows, we primarily use a variation of Dirty Bit
// that considers task phases and states.
type Rebuilder[I Info, V Value] interface {
    // ShouldRebuild determines if a task needs to be executed.
    //
    // Parameters:
    // - ctx: Context for the rebuild check
    // - key: The task key to check
    // - task: The task definition
    // - store: The current store state
    //
    // Returns:
    // - bool: true if the task should be rebuilt
    // - error: any error during the check
    ShouldRebuild(ctx context.Context, key Key, task Task[V], store Store[I, V]) (bool, error)

    // RecordBuild updates the build information after a successful build.
    RecordBuild(ctx context.Context, key Key, value V, store Store[I, V]) error
}
```

---

## Suspending Scheduler Design

The suspending scheduler is the core of this implementation. It handles tasks that may have dynamic dependencies that are discovered at runtime.

### Scheduler Interface

```go
// Scheduler orchestrates the execution of tasks.
// Different schedulers have different strategies for handling dependencies.
type Scheduler[I Info, V Value] interface {
    // Build executes the build for a given target key.
    Build(ctx context.Context, target Key, tasks Tasks[V], store Store[I, V]) error
}

// SuspendError is returned when a task needs to suspend because
// a dependency is not yet available.
type SuspendError struct {
    // WaitingFor is the key of the dependency that is not ready.
    WaitingFor Key
    // Reason provides additional context about why suspension occurred.
    Reason string
}

func (e *SuspendError) Error() string {
    return fmt.Sprintf("suspended waiting for %s: %s", e.WaitingFor, e.Reason)
}

// IsSuspendError checks if an error is a suspension.
func IsSuspendError(err error) (*SuspendError, bool) {
    var se *SuspendError
    if errors.As(err, &se) {
        return se, true
    }
    return nil, false
}
```

### Suspending Scheduler Implementation

```go
// SuspendingScheduler implements a scheduler that suspends task execution
// when dependencies are not yet available.
//
// Key properties:
// - Tasks are started and may suspend mid-execution
// - When a dependency is requested but not ready, the task suspends
// - The scheduler tracks which tasks are waiting for which dependencies
// - When a dependency completes, dependent tasks are resumed
type SuspendingScheduler[I Info, V Value] struct {
    rebuilder Rebuilder[I, V]
    
    // waiting tracks tasks that are suspended waiting for dependencies
    // maps: waiting task -> dependency it's waiting for
    waiting map[Key]Key
    
    // ready is a queue of tasks ready to execute
    ready []Key
    
    // mu protects concurrent access to scheduler state
    mu sync.RWMutex
}

// NewSuspendingScheduler creates a new suspending scheduler with the given rebuilder.
func NewSuspendingScheduler[I Info, V Value](rebuilder Rebuilder[I, V]) *SuspendingScheduler[I, V] {
    return &SuspendingScheduler[I, V]{
        rebuilder: rebuilder,
        waiting:   make(map[Key]Key),
        ready:     make([]Key, 0),
    }
}

// Build executes the build, handling suspensions appropriately.
func (s *SuspendingScheduler[I, V]) Build(
    ctx context.Context,
    target Key,
    tasks Tasks[V],
    store Store[I, V],
) error {
    // Create a suspending fetch function
    fetch := func(ctx context.Context, key Key) (V, error) {
        state := store.GetState(key)
        
        switch state {
        case TaskStateSucceeded:
            // Dependency is ready, return its value
            return store.GetValue(key), nil
            
        case TaskStateFailed:
            // Dependency failed, propagate error
            var zero V
            return zero, fmt.Errorf("dependency %s failed", key)
            
        case TaskStateRunning, TaskStatePending:
            // Dependency not ready, suspend
            var zero V
            return zero, &SuspendError{WaitingFor: key, Reason: "dependency not completed"}
            
        default:
            // Unknown state or not started, need to build
            var zero V
            return zero, &SuspendError{WaitingFor: key, Reason: "dependency not started"}
        }
    }
    
    // Check if target needs to be built
    task := tasks(target)
    if task == nil {
        return fmt.Errorf("no task defined for key: %s", target)
    }
    
    shouldRebuild, err := s.rebuilder.ShouldRebuild(ctx, target, task, store)
    if err != nil {
        return err
    }
    
    if !shouldRebuild {
        return nil // Already built and up-to-date
    }
    
    // Mark task as running
    store.SetState(target, TaskStateRunning)
    
    // Execute the task
    value, err := task(ctx, fetch)
    if err != nil {
        if suspendErr, ok := IsSuspendError(err); ok {
            // Task suspended, record the waiting relationship
            s.mu.Lock()
            s.waiting[target] = suspendErr.WaitingFor
            store.SetState(target, TaskStateSuspended)
            s.mu.Unlock()
            
            // Recursively build the dependency
            if buildErr := s.Build(ctx, suspendErr.WaitingFor, tasks, store); buildErr != nil {
                return buildErr
            }
            
            // Retry the original task after dependency completes
            return s.Build(ctx, target, tasks, store)
        }
        
        // Actual error, mark task as failed
        store.SetState(target, TaskStateFailed)
        return err
    }
    
    // Task completed successfully
    store.PutValue(target, value)
    store.SetState(target, TaskStateSucceeded)
    
    // Record build for rebuilder
    if err := s.rebuilder.RecordBuild(ctx, target, value, store); err != nil {
        return err
    }
    
    return nil
}
```

### Execution Flow Diagram

```
┌────────────────────────────────────────────────────────────────────────┐
│                    Suspending Scheduler Flow                            │
└────────────────────────────────────────────────────────────────────────┘

    ┌──────────┐
    │  Start   │
    │  Build   │
    └────┬─────┘
         │
         ▼
    ┌──────────────────┐
    │ Check if target  │
    │ needs rebuild    │
    └────────┬─────────┘
         │
    ┌────▼────┐    No
    │ Rebuild?├────────────────┐
    └────┬────┘                │
         │ Yes                  │
         ▼                      │
    ┌──────────────┐           │
    │ Mark Running │           │
    └──────┬───────┘           │
           │                    │
           ▼                    │
    ┌──────────────┐           │
    │ Execute Task │           │
    └──────┬───────┘           │
           │                    │
           ▼                    │
    ┌──────────────┐           │
    │ Fetch dep?   │           │
    └──────┬───────┘           │
           │                    │
    ┌──────▼──────┐            │
    │ Dep ready?  │            │
    └──────┬──────┘            │
           │                    │
     ┌─────┴─────┐             │
   Yes           No             │
     │           │              │
     ▼           ▼              │
┌────────┐  ┌────────────┐     │
│ Return │  │ Suspend    │     │
│ value  │  │ task       │     │
└────┬───┘  └─────┬──────┘     │
     │            │             │
     │            ▼             │
     │    ┌───────────────┐    │
     │    │ Build dep     │    │
     │    │ ─recursively─ │    │
     │    └───────┬───────┘    │
     │            │             │
     │            ▼             │
     │    ┌───────────────┐    │
     │    │ Retry task    │    │
     │    └───────┬───────┘    │
     │            │             │
     ▼            ▼             │
    ┌───────────────────┐      │
    │ Task completed    │      │
    └─────────┬─────────┘      │
              │                 │
              ▼                 │
    ┌──────────────────┐       │
    │ Store value      │       │
    │ Mark Succeeded   │       │
    └────────┬─────────┘       │
             │                  │
             ▼                  │
    ┌────────────────┐         │
    │ Record build   │         │
    └────────┬───────┘         │
             │                  │
             ▼                  ▼
         ┌───────┐         ┌───────┐
         │ Done  │         │ Done  │
         └───────┘         └───────┘
```

---

## Rebuilder Strategies

### Dirty Bit Rebuilder

The simplest rebuilder strategy. A task is rebuilt if any of its dependencies have changed since the last build.

```go
// DirtyBitInfo stores whether a task is dirty.
type DirtyBitInfo struct {
    Dirty bool
}

// DirtyBitRebuilder implements a simple dirty-bit based rebuilder.
type DirtyBitRebuilder[V Value] struct{}

func NewDirtyBitRebuilder[V Value]() *DirtyBitRebuilder[V] {
    return &DirtyBitRebuilder[V]{}
}

func (r *DirtyBitRebuilder[V]) ShouldRebuild(
    ctx context.Context,
    key Key,
    task Task[V],
    store Store[*DirtyBitInfo, V],
) (bool, error) {
    info := store.GetInfo(key)
    if info == nil {
        return true, nil // Never built
    }
    return info.Dirty, nil
}

func (r *DirtyBitRebuilder[V]) RecordBuild(
    ctx context.Context,
    key Key,
    value V,
    store Store[*DirtyBitInfo, V],
) error {
    store.PutInfo(key, &DirtyBitInfo{Dirty: false})
    return nil
}
```

### Phase-Based Rebuilder (Argo-Specific)

For Argo Workflows, we need a rebuilder that considers node phases and states.

```go
// PhaseInfo stores phase and output hash information.
type PhaseInfo struct {
    Phase      TaskState
    OutputHash Hash
    CompletedAt time.Time
}

// PhaseRebuilder determines rebuild needs based on workflow node phases.
// This is specifically designed for Argo Workflows integration.
type PhaseRebuilder[V Value] struct {
    // evaluateDependsLogic evaluates the depends expression for a task
    evaluateDependsLogic func(ctx context.Context, key Key, store Store[*PhaseInfo, V]) (execute bool, proceed bool, err error)
}

func NewPhaseRebuilder[V Value](
    evaluator func(ctx context.Context, key Key, store Store[*PhaseInfo, V]) (bool, bool, error),
) *PhaseRebuilder[V] {
    return &PhaseRebuilder[V]{
        evaluateDependsLogic: evaluator,
    }
}

func (r *PhaseRebuilder[V]) ShouldRebuild(
    ctx context.Context,
    key Key,
    task Task[V],
    store Store[*PhaseInfo, V],
) (bool, error) {
    state := store.GetState(key)
    
    switch state {
    case TaskStateSucceeded:
        return false, nil // Already completed successfully
    case TaskStateFailed:
        return false, nil // Failed, don't retry automatically
    case TaskStateSkipped, TaskStateOmitted:
        return false, nil // Intentionally not run
    case TaskStateRunning:
        return false, nil // Already running
    default:
        // Check depends logic
        execute, proceed, err := r.evaluateDependsLogic(ctx, key, store)
        if err != nil {
            return false, err
        }
        if !proceed {
            return false, nil // Dependencies not ready
        }
        return execute, nil // Execute if depends logic says so
    }
}

func (r *PhaseRebuilder[V]) RecordBuild(
    ctx context.Context,
    key Key,
    value V,
    store Store[*PhaseInfo, V],
) error {
    store.PutInfo(key, &PhaseInfo{
        Phase:       TaskStateSucceeded,
        OutputHash:  value.Hash(),
        CompletedAt: time.Now(),
    })
    return nil
}
```

---

## Integration with Argo Workflows

### Mapping Argo Concepts to Build System Abstractions

| Build System Concept | Argo Workflows Equivalent |
|---------------------|---------------------------|
| Key | Task name or Node ID |
| Value | NodeStatus with Outputs |
| Store | Workflow Status (Nodes map) |
| Task | DAGTask execution |
| Fetch | Dependency lookup from ancestor tasks |
| Scheduler | DAG controller reconciliation loop |
| Rebuilder | Phase-based decision logic |

### NodeOutput Value Implementation

```go
// NodeOutput wraps Argo's node status as a Value.
type NodeOutput struct {
    NodeID    string
    Phase     wfv1.NodePhase
    Outputs   *wfv1.Outputs
    Message   string
}

func (n *NodeOutput) Hash() Hash {
    // Create a hash from outputs for change detection
    if n.Outputs == nil {
        return ""
    }
    h := sha256.New()
    for _, param := range n.Outputs.Parameters {
        h.Write([]byte(param.Name))
        if param.Value != nil {
            h.Write([]byte(param.Value.String()))
        }
    }
    return hex.EncodeToString(h.Sum(nil))
}
```

### WorkflowStore Implementation

```go
// WorkflowStore adapts Argo's workflow status as a Store.
type WorkflowStore struct {
    wf     *wfv1.Workflow
    nodes  wfv1.Nodes
    info   map[Key]*PhaseInfo
    prefix string // Boundary name prefix for node names
}

func NewWorkflowStore(wf *wfv1.Workflow, boundaryName string) *WorkflowStore {
    return &WorkflowStore{
        wf:     wf,
        nodes:  wf.Status.Nodes,
        info:   make(map[Key]*PhaseInfo),
        prefix: boundaryName,
    }
}

func (s *WorkflowStore) GetValue(key Key) *NodeOutput {
    nodeName := s.taskNodeName(key)
    nodeID := s.wf.NodeID(nodeName)
    node, err := s.nodes.Get(nodeID)
    if err != nil {
        return nil
    }
    return &NodeOutput{
        NodeID:  node.ID,
        Phase:   node.Phase,
        Outputs: node.Outputs,
        Message: node.Message,
    }
}

func (s *WorkflowStore) GetState(key Key) TaskState {
    nodeName := s.taskNodeName(key)
    nodeID := s.wf.NodeID(nodeName)
    node, err := s.nodes.Get(nodeID)
    if err != nil {
        return TaskStatePending
    }
    return phaseToState(node.Phase)
}

func (s *WorkflowStore) taskNodeName(taskName string) string {
    return fmt.Sprintf("%s.%s", s.prefix, taskName)
}

func phaseToState(phase wfv1.NodePhase) TaskState {
    switch phase {
    case wfv1.NodePending:
        return TaskStatePending
    case wfv1.NodeRunning:
        return TaskStateRunning
    case wfv1.NodeSucceeded:
        return TaskStateSucceeded
    case wfv1.NodeFailed, wfv1.NodeError:
        return TaskStateFailed
    case wfv1.NodeSkipped:
        return TaskStateSkipped
    case wfv1.NodeOmitted:
        return TaskStateOmitted
    default:
        return TaskStatePending
    }
}
```

### Integration Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Workflow Controller                                   │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                      executeDAG Method                                 │  │
│  │                                                                        │  │
│  │   ┌──────────────┐     ┌──────────────────┐     ┌────────────────┐   │  │
│  │   │ Build Target │────►│ SuspendingSched  │────►│ PhaseRebuilder │   │  │
│  │   │ ─leaf tasks─ │     │                  │     │                │   │  │
│  │   └──────────────┘     └────────┬─────────┘     └────────────────┘   │  │
│  │                                  │                                    │  │
│  │                                  ▼                                    │  │
│  │                         ┌───────────────┐                            │  │
│  │                         │ WorkflowStore │                            │  │
│  │                         │               │                            │  │
│  │                         │ Nodes Map     │                            │  │
│  │                         └───────────────┘                            │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                    │                                        │
│                                    ▼                                        │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                     DAGTasks -> Build Tasks                            │  │
│  │                                                                        │  │
│  │    for each DAGTask:                                                  │  │
│  │      Tasks[task.Name] = func(ctx, fetch) {                            │  │
│  │          // Fetch dependencies declared in depends/dependencies       │  │
│  │          for dep in task.Dependencies:                                │  │
│  │              depOutput, err := fetch(ctx, dep)                        │  │
│  │              if suspend error: return suspend                         │  │
│  │                                                                        │  │
│  │          // Execute template                                          │  │
│  │          return executeTemplate(ctx, task)                            │  │
│  │      }                                                                │  │
│  │                                                                        │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Migration Strategy

The migration from the current implementation to the Build Systems à la Carte framework can be done incrementally:

1. **Phase 1: Core Abstractions**
   - Implement the core types (Store, Task, Rebuilder)
   - Unit test with mock implementations
   - No changes to existing controller

2. **Phase 2: Adapter Layer**
   - Create `WorkflowStore` adapter for existing workflow status
   - Create task adapters for DAGTask
   - Integration tests with real workflows

3. **Phase 3: Suspending Scheduler**
   - Implement the suspending scheduler
   - Test alongside existing implementation (feature flag)
   - Validate behavior matches current controller

4. **Phase 4: Integration**
   - Replace `executeDAGTask` recursive calls with scheduler.Build()
   - Replace `assessDAGPhase` with store state queries
   - Remove old implementation

---

## Example Usage

### Basic Build Example

```go
package main

import (
    "context"
    "fmt"
    
    "github.com/argoproj/argo-workflows/v3/pkg/dag"
)

// SimpleValue implements Value for demonstration
type SimpleValue struct {
    Data string
}

func (v *SimpleValue) Hash() dag.Hash {
    return dag.Hash(v.Data)
}

func main() {
    ctx := context.Background()
    
    // Define tasks
    tasks := func(key dag.Key) dag.Task[*SimpleValue] {
        switch key {
        case "A":
            return func(ctx context.Context, fetch dag.Fetch[*SimpleValue]) (*SimpleValue, error) {
                return &SimpleValue{Data: "result-A"}, nil
            }
        case "B":
            return func(ctx context.Context, fetch dag.Fetch[*SimpleValue]) (*SimpleValue, error) {
                // B depends on A
                a, err := fetch(ctx, "A")
                if err != nil {
                    return nil, err
                }
                return &SimpleValue{Data: "result-B from " + a.Data}, nil
            }
        case "C":
            return func(ctx context.Context, fetch dag.Fetch[*SimpleValue]) (*SimpleValue, error) {
                // C depends on A and B
                a, err := fetch(ctx, "A")
                if err != nil {
                    return nil, err
                }
                b, err := fetch(ctx, "B")
                if err != nil {
                    return nil, err
                }
                return &SimpleValue{Data: fmt.Sprintf("result-C from %s and %s", a.Data, b.Data)}, nil
            }
        default:
            return nil
        }
    }
    
    // Create store and scheduler
    store := dag.NewInMemoryStore[*dag.DirtyBitInfo, *SimpleValue]()
    rebuilder := dag.NewDirtyBitRebuilder[*SimpleValue]()
    scheduler := dag.NewSuspendingScheduler[*dag.DirtyBitInfo, *SimpleValue](rebuilder)
    
    // Build target C (will automatically build dependencies A and B)
    err := scheduler.Build(ctx, "C", tasks, store)
    if err != nil {
        panic(err)
    }
    
    // Retrieve result
    result := store.GetValue("C")
    fmt.Println(result.Data) // Output: result-C from result-A and result-B from result-A
}
```

### Argo Workflows Integration Example

```go
package controller

import (
    "context"
    
    wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
    "github.com/argoproj/argo-workflows/v3/pkg/dag"
)

func (woc *wfOperationCtx) executeDAGWithBuildSystem(
    ctx context.Context,
    nodeName string,
    tmpl *wfv1.Template,
) (*wfv1.NodeStatus, error) {
    
    // Create the workflow store adapter
    store := dag.NewWorkflowStore(woc.wf, nodeName)
    
    // Create the phase-based rebuilder with depends logic evaluation
    rebuilder := dag.NewPhaseRebuilder[*dag.NodeOutput](
        func(ctx context.Context, key dag.Key, s dag.Store[*dag.PhaseInfo, *dag.NodeOutput]) (bool, bool, error) {
            return woc.evaluateDependsLogic(ctx, key)
        },
    )
    
    // Create the suspending scheduler
    scheduler := dag.NewSuspendingScheduler[*dag.PhaseInfo, *dag.NodeOutput](rebuilder)
    
    // Convert DAG tasks to build tasks
    tasks := woc.dagTasksToTasks(ctx, tmpl.DAG.Tasks, store)
    
    // Identify target tasks (leaves or explicit targets)
    targets := woc.findTargetTasks(tmpl.DAG)
    
    // Build each target
    for _, target := range targets {
        if err := scheduler.Build(ctx, target, tasks, store); err != nil {
            if suspendErr, ok := dag.IsSuspendError(err); ok {
                // Task suspended waiting for dependency - this is expected
                // The workflow reconciliation loop will retry
                continue
            }
            return nil, err
        }
    }
    
    // Assess overall DAG phase from store
    return woc.assessDAGPhaseFromStore(ctx, nodeName, targets, store)
}

func (woc *wfOperationCtx) dagTasksToTasks(
    ctx context.Context,
    dagTasks []wfv1.DAGTask,
    store *dag.WorkflowStore,
) dag.Tasks[*dag.NodeOutput] {
    
    taskMap := make(map[dag.Key]*wfv1.DAGTask)
    for i := range dagTasks {
        taskMap[dagTasks[i].Name] = &dagTasks[i]
    }
    
    return func(key dag.Key) dag.Task[*dag.NodeOutput] {
        dagTask, ok := taskMap[key]
        if !ok {
            return nil
        }
        
        return func(ctx context.Context, fetch dag.Fetch[*dag.NodeOutput]) (*dag.NodeOutput, error) {
            // Fetch all dependencies
            deps := woc.getTaskDependencies(ctx, dagTask)
            for _, dep := range deps {
                _, err := fetch(ctx, dep)
                if err != nil {
                    return nil, err // Suspends if not ready
                }
            }
            
            // All dependencies ready, execute the task
            node, err := woc.executeDAGTaskTemplate(ctx, dagTask, store)
            if err != nil {
                return nil, err
            }
            
            return &dag.NodeOutput{
                NodeID:  node.ID,
                Phase:   node.Phase,
                Outputs: node.Outputs,
                Message: node.Message,
            }, nil
        }
    }
}
```

---

## File Structure

```
pkg/dag/
├── README.md           # This document
├── types.go            # Core type definitions (Key, Value, TaskState)
├── store.go            # Store interface and implementations
├── task.go             # Task and Fetch type definitions
├── scheduler.go        # Scheduler interface and SuspendingScheduler
├── rebuilder.go        # Rebuilder interface and implementations
├── errors.go           # Error types including SuspendError
├── argo_adapter.go     # Argo-specific adapters (WorkflowStore, NodeOutput)
└── *_test.go           # Test files
```

---

## References

1. Mokhov, A., Mitchell, N., & Peyton Jones, S. (2018). "Build Systems à la Carte". *Proceedings of the ACM on Programming Languages*, 2(ICFP), 1-29.
   - Paper: https://www.microsoft.com/en-us/research/publication/build-systems-a-la-carte/

2. Argo Workflows Documentation: https://argoproj.github.io/argo-workflows/

3. Current DAG Controller Implementation: [`workflow/controller/dag.go`](../../workflow/controller/dag.go)
