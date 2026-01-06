# Build Systems à la Carte: DAG Evaluation Framework

This package implements the theoretical framework from the paper "Build Systems à la Carte" by Andrey Mokhov, Neil Mitchell, and Simon Peyton Jones. It provides a principled approach to DAG dependency evaluation that can serve as the foundation for any build system or workflow engine.

## Overview

### Build Systems à la Carte Framework

The paper identifies that all build systems can be decomposed into two orthogonal components:

1. **Scheduler**: Determines the order in which tasks are executed
2. **Rebuilder**: Determines whether a task needs to be re-executed

This decomposition allows us to reason about and implement build systems in a modular way.

### Schedulers

| Scheduler | Description |
|-----------|-------------|
| **Topological** | Pre-computes execution order from static dependencies |
| **Suspending** | Suspends task execution when dependency isn't ready |

### Rebuilders

| Rebuilder | Description |
|-----------|-------------|
| **Dirty Bit** | Simple check if a dependency has changed |
| **Verifying Trace** | Compares dependency hashes against stored traces |
| **Always Rebuild** | Forces execution every time |

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
│  │ - Suspending ◄──┼────────────────────┼── Verifying Trace   │  │
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

1. **Task[K, V]**: A computation that produces a value `V` for a key `K`, potentially depending on other tasks.
2. **Store[I, K, V]**: A key-value store with additional build information `I`.
3. **Tasks[K, V]**: A collection of task definitions.
4. **Scheduler**: Orchestrates task execution order.
5. **Rebuilder**: Decides if a task needs to be rebuilt.

---

## Example Usage

```go
package main

import (
    "context"
    "fmt"
    
    "github.com/argoproj/argo-workflows/v3/pkg/dag"
)

func main() {
    ctx := context.Background()
    
    // Define tasks
    taskA := dag.NewTask("A", nil, func(fetch dag.Fetch[string, dag.StringValue]) (dag.StringValue, error) {
        return dag.NewStringValue("result-A"), nil
    })
    
    taskB := dag.NewTask("B", []string{"A"}, func(fetch dag.Fetch[string, dag.StringValue]) (dag.StringValue, error) {
        a, err := fetch("A")
        if err != nil {
            return "", err
        }
        return dag.NewStringValue("result-B from " + string(a)), nil
    })
    
    tasks := dag.NewMapTasks([]*dag.Task[string, dag.StringValue]{taskA, taskB})
    
    // Create store and scheduler
    store := dag.NewMapStore[any, string, dag.StringValue](nil)
    rebuilder := dag.NewAlwaysRebuildRebuilder[string, dag.StringValue]()
    scheduler := dag.NewSuspendingScheduler(rebuilder, tasks, store)
    
    // Build target B
    result, err := scheduler.Build(ctx, "B")
    if err != nil {
        panic(err)
    }
    
    fmt.Println(result) // Output: result-B from result-A
}
```

---

## References

1. Mokhov, A., Mitchell, N., & Peyton Jones, S. (2018). "Build Systems à la Carte". *Proceedings of the ACM on Programming Languages*, 2(ICFP), 1-29.
   - Paper: https://www.microsoft.com/en-us/research/publication/build-systems-a-la-carte/