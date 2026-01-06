# Argo Workflows DAG Evaluation

This package integrates the generic DAG evaluation framework from `pkg/dag` with Argo Workflows. It provides adapters for Argo's `Workflow` and `NodeStatus` objects, allowing the workflow controller to use the "Build Systems à la Carte" framework for DAG execution.

## Integration with Argo Workflows

### Mapping Argo Concepts to Build System Abstractions

| Build System Concept | Argo Workflows Equivalent |
|---------------------|---------------------------|
| Key | Task name or Node ID |
| Value | `NodeValue` (wraps NodeStatus and Outputs) |
| Store | `WorkflowStore` (adapts Workflow Status Nodes map) |
| Task | `WorkflowTasks` (adapts DAGTask definitions) |
| Scheduler | Suspending Scheduler from `pkg/dag` |
| Rebuilder | `PhaseBasedRebuilder` (Argo-specific logic) |

### Core Components

#### [NodeValue](./adapter_value.go)
Wraps Argo's node status as a `genericdag.Value`. It implements hashing for outputs to support change detection if needed.

#### [WorkflowStore](./adapter_store.go)
Adapts Argo's workflow status as a `genericdag.Store`. It maps task names to node IDs and handles the hierarchy within a DAG template.

#### [WorkflowTasks](./adapter_tasks.go)
Converts Argo's `DAGTask` definitions into `genericdag.Task` functions. It handles both static and dynamic dependencies (e.g., discovered via `depends` logic).

#### [PhaseBasedRebuilder](./rebuilder.go)
Argo-specific rebuilder that determines whether a task should execute based on its current phase (Pending, Running, Succeeded, etc.) and `depends` expression evaluation.

## Usage Example

```go
// Create the evaluator for a DAG template
evaluator := NewDAGEvaluator(wf, tmpl, boundaryID, boundaryName)

// Evaluate all leaf tasks (or explicit targets)
targets := evaluator.GetTargetTasks(ctx)
results, err := evaluator.EvaluateDAG(ctx, targets)

// Process results
for _, result := range results {
    if result.ShouldRun {
        // Execute task...
    }
}
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Workflow Controller                                   │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                      DAGEvaluator                                      │  │
│  │                                                                        │  │
│  │   ┌──────────────┐     ┌──────────────────┐     ┌────────────────┐   │  │
│  │   │ Targets      │────►│ SuspendingSched  │────►│ PhaseRebuilder │   │  │
│  │   │ (Leaf tasks) │     │ (from pkg/dag)   │     │ (Argo-specific)│   │  │
│  │   └──────────────┘     └────────┬─────────┘     └────────────────┘   │  │
│  │                                  │                                    │  │
│  │                                  ▼                                    │  │
│  │                         ┌───────────────┐                            │  │
│  │                         │ WorkflowStore │                            │  │
│  │                         │ (Argo Adapter)│                            │  │
│  │                         └───────────────┘                            │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```
