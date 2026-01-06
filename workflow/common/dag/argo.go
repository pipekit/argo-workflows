package dag

import (
	"context"
	"fmt"
	"sort"
	"strings"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	genericdag "github.com/argoproj/argo-workflows/v3/pkg/dag"
	"github.com/argoproj/argo-workflows/v3/util/expr/argoexpr"
)

// TaskState re-exports genericdag.TaskState for convenience.
type TaskState = genericdag.TaskState

// Key re-exports genericdag.Key for convenience.
type Key = genericdag.Key

// Value re-exports genericdag.Value for convenience.
type Value = genericdag.Value

const (
	TaskStatePending   = genericdag.TaskStatePending
	TaskStateRunning   = genericdag.TaskStateRunning
	TaskStateSucceeded = genericdag.TaskStateSucceeded
	TaskStateFailed    = genericdag.TaskStateFailed
	TaskStateSkipped   = genericdag.TaskStateSkipped
	TaskStateOmitted   = genericdag.TaskStateOmitted
	TaskStateError     = genericdag.TaskStateError
)

// TaskResult represents the result state of a dependency task.
type TaskResult struct {
	Succeeded    bool `json:"Succeeded"`
	Failed       bool `json:"Failed"`
	Errored      bool `json:"Errored"`
	Skipped      bool `json:"Skipped"`
	Omitted      bool `json:"Omitted"`
	Daemoned     bool `json:"Daemoned"`
	AnySucceeded bool `json:"AnySucceeded"`
	AllFailed    bool `json:"AllFailed"`
}

// EvaluationResult contains the evaluation result for a single task.
// This is the output of the DAGEvaluator for integration with the workflow controller.
type EvaluationResult struct {
	// TaskName is the name of the evaluated task.
	TaskName string
	// ShouldRun indicates if this task should be executed.
	ShouldRun bool
	// Suspended indicates if this task is waiting for dependencies.
	Suspended bool
	// WaitingOn contains task names this task is waiting for.
	WaitingOn []string
	// Skipped indicates if this task should be skipped.
	Skipped bool
	// SkipReason explains why the task was skipped.
	SkipReason string
	// Error contains any error from evaluation.
	Error error
	// CurrentState is the task's current state.
	CurrentState genericdag.TaskState
}

// DAGEvaluator provides a high-level API for evaluating DAG workflows.
// This is the main entry point for integration with the workflow controller.
// It wraps the SuspendingScheduler with Argo-specific adapters.
type DAGEvaluator struct {
	// Store is the workflow store adapter.
	Store *WorkflowStore
	// Tasks is the workflow tasks adapter.
	Tasks *WorkflowTasks
	// Scheduler is the suspending scheduler for the DAG.
	Scheduler *genericdag.SuspendingScheduler[genericdag.Key, NodeValue]
	// Rebuilder is the phase-based rebuilder for the DAG.
	Rebuilder *PhaseBasedRebuilder[genericdag.Key, NodeValue]
	// Workflow is the underlying workflow.
	Workflow *wfv1.Workflow
	// Template is the DAG template being evaluated.
	Template *wfv1.Template
}

// NewDAGEvaluator creates a new DAGEvaluator for a workflow and DAG template.
func NewDAGEvaluator(wf *wfv1.Workflow, tmpl *wfv1.Template, boundaryID, boundaryName string) *DAGEvaluator {
	store := NewWorkflowStore(wf, boundaryID, boundaryName)

	dagTasks := make([]Task, len(tmpl.DAG.Tasks))
	for i := range tmpl.DAG.Tasks {
		dagTasks[i] = &DAGTask{DAGTask: &tmpl.DAG.Tasks[i]}
	}
	tasks := NewWorkflowTasks(dagTasks, store, wf)

	evaluator := &DAGEvaluator{
		Store:    store,
		Tasks:    tasks,
		Workflow: wf,
		Template: tmpl,
	}

	// Create phase-based rebuilder with depends logic evaluation
	rebuilder := NewPhaseBasedRebuilder[genericdag.Key, NodeValue](
		func(ctx context.Context, key genericdag.Key) genericdag.TaskState {
			return store.GetState(ctx, key)
		},
		func(ctx context.Context, key genericdag.Key, fetch genericdag.Fetch[genericdag.Key, NodeValue]) (bool, bool, error) {
			should, err := evaluator.evaluateDependsLogic(ctx, key)
			if err != nil {
				return false, false, err
			}
			return should, true, nil
		},
	)

	// Create the scheduler
	scheduler := genericdag.NewSuspendingScheduler[genericdag.Key, NodeValue](
		rebuilder,
		tasks,
		store,
	)

	evaluator.Rebuilder = rebuilder
	evaluator.Scheduler = scheduler

	return evaluator
}

// EvaluateDAG evaluates all target tasks and returns which tasks should be executed.
// This maps to the existing dagContext.evaluateDependsLogic but uses the
// Build Systems à la Carte framework.
func (e *DAGEvaluator) EvaluateDAG(ctx context.Context, targets []genericdag.Key) ([]EvaluationResult, error) {
	results := make([]EvaluationResult, 0, len(targets))

	// Batch build all targets
	buildResults := e.Scheduler.BatchBuild(ctx, targets)

	for i, buildResult := range buildResults {
		// Map BuildResult to EvaluationResult
		result := EvaluationResult{
			TaskName:     targets[i],
			CurrentState: buildResult.State,
		}

		if buildResult.Value.NodeStatus != nil {
			// Task already has a value (node exists)
			if buildResult.Value.IsOmitted() {
				result.Skipped = true
				result.SkipReason = buildResult.Value.NodeStatus.Message
			}
			// Already completed or running
			results = append(results, result)
			continue
		}

		if buildResult.Suspended {
			// Suspended - check if waiting on self (should run) or others
			waitingForSelf := false
			for _, waitKey := range buildResult.WaitingOn {
				if waitKey == targets[i] {
					waitingForSelf = true
					break
				}
			}

			if waitingForSelf {
				result.ShouldRun = true
			} else {
				result.Suspended = true
				result.WaitingOn = buildResult.WaitingOn
			}
		} else if buildResult.Error != nil {
			result.Error = buildResult.Error
		} else {
			// No error, no value, not suspended -> Skipped/Omitted by logic
			result.Skipped = true
			result.SkipReason = "depends condition not met"
		}

		results = append(results, result)
	}

	return results, nil
}

// EvaluateTask evaluates a single task and returns its evaluation result.
func (e *DAGEvaluator) EvaluateTask(ctx context.Context, taskName string) EvaluationResult {
	results, _ := e.EvaluateDAG(ctx, []genericdag.Key{taskName})
	if len(results) > 0 {
		return results[0]
	}
	return EvaluationResult{TaskName: taskName, Error: fmt.Errorf("evaluation failed")}
}

// evaluateDependsLogic evaluates the depends expression for a task.
// Returns:
//   - shouldExecute: true if the task should execute based on depends logic
//   - err: error if dependencies are not fulfilled (SuspendError) or evaluation fails
func (e *DAGEvaluator) evaluateDependsLogic(ctx context.Context, taskName string) (bool, error) {
	node := e.Store.GetNode(taskName)
	if node != nil && node.Fulfilled() {
		return true, nil
	}

	evalScope := make(map[string]TaskResult)

	deps, _ := e.Tasks.GetDependencies(ctx, taskName)
	for _, depName := range deps {
		depNode := e.Store.GetNode(depName)

		if depNode == nil {
			return false, genericdag.NewSuspendErrorWithReason(depName, "dependency does not exist")
		}
		if depNode.IsDaemoned() {
			// Daemoned nodes are considered fulfilled for dependency purposes if they are not pending
			// But wait, IsDaemoned() check in NodeValue logic includes IsDaemoned && !Pending
			// proceed to logic evaluation
		} else if !depNode.Fulfilled() {
			return false, genericdag.NewSuspendErrorWithReason(depName, "dependency not fulfilled")
		}

		// Check hooks completion (if workflow is available)
		if e.Workflow != nil && !checkAllHooksFulfilled(depNode, e.Workflow.Status.Nodes) {
			return false, genericdag.NewSuspendErrorWithReason(depName, "dependency hooks not fulfilled")
		}

		// Normalize task name for expression evaluation
		evalTaskName := normalizeTaskName(depName)
		if _, ok := evalScope[evalTaskName]; ok {
			continue
		}

		// Compute AnySucceeded and AllFailed for task groups
		anySucceeded := false
		allFailed := false

		if depNode.Type == wfv1.NodeTypeTaskGroup {
			allFailed = len(depNode.Children) > 0
			for _, childID := range depNode.Children {
				childPhase, err := e.Workflow.Status.Nodes.GetPhase(childID)
				if err != nil {
					allFailed = false
					continue
				}
				anySucceeded = anySucceeded || *childPhase == wfv1.NodeSucceeded
				allFailed = allFailed && *childPhase == wfv1.NodeFailed
			}
		}

		evalScope[evalTaskName] = TaskResult{
			Succeeded:    depNode.Phase == wfv1.NodeSucceeded,
			Failed:       depNode.Phase == wfv1.NodeFailed,
			Errored:      depNode.Phase == wfv1.NodeError,
			Skipped:      depNode.Phase == wfv1.NodeSkipped,
			Omitted:      depNode.Phase == wfv1.NodeOmitted,
			Daemoned:     depNode.IsDaemoned() && depNode.Phase != wfv1.NodePending,
			AnySucceeded: anySucceeded,
			AllFailed:    allFailed,
		}
	}

	// Get and normalize the depends logic expression
	dependsLogic := e.Tasks.GetDependsLogic(ctx, taskName)
	evalLogic := strings.ReplaceAll(dependsLogic, "-", "_")

	if evalLogic == "" {
		// No depends expression means always execute (if no dependencies or all are satisfied)
		return true, nil
	}

	// Evaluate the expression
	result, err := argoexpr.EvalBool(evalLogic, evalScope)
	if err != nil {
		return false, fmt.Errorf("unable to evaluate expression '%s': %s", evalLogic, err)
	}
	return result, nil
}

// FindLeafTaskNames finds tasks that no other tasks depend on.
// These are the default target tasks when dag.targets is not specified.
func (e *DAGEvaluator) FindLeafTaskNames(ctx context.Context) []string {
	taskIsLeaf := make(map[string]bool)

	for _, task := range e.Tasks.Tasks {
		if _, ok := taskIsLeaf[task.GetName()]; !ok {
			taskIsLeaf[task.GetName()] = true
		}
		deps, _ := e.Tasks.GetDependencies(ctx, task.GetName())
		for _, dep := range deps {
			taskIsLeaf[dep] = false
		}
	}

	leafTasks := make([]string, 0)
	for taskName, isLeaf := range taskIsLeaf {
		if isLeaf {
			leafTasks = append(leafTasks, taskName)
		}
	}

	sort.Strings(leafTasks) // Execute in predictable order
	return leafTasks
}

// GetTargetTasks returns the target tasks for the DAG.
// If explicit targets are specified, those are returned.
// Otherwise, leaf tasks (tasks with no dependents) are returned.
func (e *DAGEvaluator) GetTargetTasks(ctx context.Context) []string {
	if e.Template.DAG.Target != "" {
		return strings.Split(e.Template.DAG.Target, " ")
	}
	return e.FindLeafTaskNames(ctx)
}

// checkAllHooksFulfilled checks if all lifecycle hooks for a node are fulfilled.
// This mirrors common.CheckAllHooksFullfilled from workflow/common/util.go.
func checkAllHooksFulfilled(node *wfv1.NodeStatus, nodes wfv1.Nodes) bool {
	if node == nil {
		return true
	}

	// Check child nodes that are hooked
	for _, childID := range node.Children {
		childNode, ok := nodes[childID]
		if !ok {
			continue
		}
		// Check if this child is a hook node and if so, whether it's fulfilled
		if childNode.NodeFlag != nil && childNode.NodeFlag.Hooked && !childNode.Fulfilled() {
			return false
		}
	}

	return true
}

// EvaluateAll evaluates all tasks in the DAG and returns a map of results.
// This is useful for getting a complete picture of the DAG state.
func (e *DAGEvaluator) EvaluateAll(ctx context.Context) map[string]EvaluationResult {
	results := make(map[string]EvaluationResult)

	for _, taskName := range e.Tasks.TaskNames() {
		results[taskName] = e.EvaluateTask(ctx, taskName)
	}

	return results
}

// GetReadyTasks returns all tasks that are ready to execute.
// A task is ready if:
//   - It hasn't started yet
//   - All its dependencies are fulfilled
//   - Its depends expression evaluates to true
func (e *DAGEvaluator) GetReadyTasks(ctx context.Context) []string {
	var readyTasks []string
	for _, task := range e.Tasks.Tasks {
		// Only consider tasks that haven't started yet
		if e.Store.GetNode(task.GetName()) != nil {
			continue
		}

		result := e.EvaluateTask(ctx, task.GetName())
		if result.Error == nil && result.ShouldRun && !result.Suspended {
			readyTasks = append(readyTasks, task.GetName())
		}
	}
	return readyTasks
}

// GetWaitingTasks returns all tasks that are waiting for dependencies.
func (e *DAGEvaluator) GetWaitingTasks(ctx context.Context) map[string][]string {
	waiting := make(map[string][]string)

	for _, taskName := range e.Tasks.TaskNames() {
		result := e.EvaluateTask(ctx, taskName)
		if result.Suspended && len(result.WaitingOn) > 0 {
			waiting[taskName] = result.WaitingOn
		}
	}

	return waiting
}

// Ensure adapters implement the required interfaces
var _ genericdag.Store[any, genericdag.Key, NodeValue] = (*WorkflowStore)(nil)
var _ genericdag.Tasks[genericdag.Key, NodeValue] = (*WorkflowTasks)(nil)
var _ genericdag.Value = NodeValue{}
