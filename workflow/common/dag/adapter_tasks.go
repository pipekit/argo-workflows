package dag

import (
	"context"
	"sort"
	"strings"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	genericdag "github.com/argoproj/argo-workflows/v3/pkg/dag"
)

// WorkflowTasks adapts Argo's DAG tasks as a Tasks collection for the Build Systems framework.
// It provides task lookup and dependency resolution using the same logic as the DAG controller.
type WorkflowTasks struct {
	// Tasks is the slice of tasks from the template.
	Tasks []Task
	// Store provides access to current node states for dependency resolution.
	Store *WorkflowStore
	// Workflow is the workflow for checking hooks completion.
	Workflow *wfv1.Workflow

	// taskMap provides fast lookup of tasks by name.
	taskMap map[string]Task
	// dependencies stores pre-computed dependencies for each task.
	dependencies map[string][]string
	// dependsLogic stores pre-computed resolved depends expressions.
	dependsLogic map[string]string
}

// NewWorkflowTasks creates a new WorkflowTasks adapter.
// It pre-computes all dependencies and logic expressions.
func NewWorkflowTasks(tasks []Task, store *WorkflowStore, wf *wfv1.Workflow) *WorkflowTasks {
	taskMap := make(map[string]Task, len(tasks))
	for i := range tasks {
		taskMap[tasks[i].GetName()] = tasks[i]
	}

	wt := &WorkflowTasks{
		Tasks:        tasks,
		Store:        store,
		Workflow:     wf,
		taskMap:      taskMap,
		dependencies: make(map[string][]string, len(tasks)),
		dependsLogic: make(map[string]string, len(tasks)),
	}

	// Pre-compute dependencies and logic for all tasks
	taskProvider := func(name string) Task { return taskMap[name] }
	
	for _, task := range tasks {
		name := task.GetName()
		initialLogic := getTaskDependsLogic(task, taskProvider)
		deps, normalizedLogic := resolveDependencies(initialLogic, taskProvider)
		
		wt.dependencies[name] = deps
		wt.dependsLogic[name] = normalizedLogic
	}

	return wt
}

// GetTask retrieves the task definition for a given key (task name).
func (w *WorkflowTasks) GetTask(key genericdag.Key) (*genericdag.Task[genericdag.Key, NodeValue], bool) {
	dagTask, ok := w.taskMap[key]
	if !ok {
		return nil, false
	}

	// Get static dependencies (already computed)
	deps := w.dependencies[key]

	// Create a Task that wraps the DAG task
	task := &genericdag.Task[genericdag.Key, NodeValue]{
		Key:          key,
		Dependencies: deps,
		Run: func(fetch genericdag.Fetch[genericdag.Key, NodeValue]) (NodeValue, error) {
			// This Run function is used by the scheduler to execute the task.
			// For Argo Workflows, actual execution happens elsewhere (in the controller).
			// Here we just check if dependencies are ready and return the current value.
			for _, dep := range deps {
				_, err := fetch(dep)
				if err != nil {
					return NodeValue{}, err
				}
			}
			// Return current value from store
			if val, ok := w.Store.GetValue(context.TODO(), key); ok {
				return val, nil
			}
			// No value yet - this would trigger actual execution in the controller
			return NodeValue{}, genericdag.NewSuspendErrorWithReason(key, "task not yet executed")
		},
	}

	// Store reference to original DAG task for later use
	_ = dagTask

	return task, true
}

// GetDependencies returns the dependencies for a task.
func (w *WorkflowTasks) GetDependencies(_ context.Context, key genericdag.Key) ([]genericdag.Key, error) {
	if deps, ok := w.dependencies[key]; ok {
		return deps, nil
	}
	// If task is not found, return empty (or error if preferred, but keeping consistent with map behavior)
	return nil, nil
}

// GetDependsLogic returns the resolved depends expression for a task.
func (w *WorkflowTasks) GetDependsLogic(_ context.Context, taskName string) string {
	return w.dependsLogic[taskName]
}

// getTaskDependsLogic returns the depends expression for a task.
// If 'depends' is empty, it constructs one from the 'dependencies' list.
func getTaskDependsLogic(task Task, taskProvider func(string) Task) string {
	if task.GetDepends() != "" {
		return task.GetDepends()
	}

	// For backwards compatibility, convert dependencies list to depends expression
	var depExpressions []string
	for _, dep := range task.GetDependencies() {
		depTask := taskProvider(dep)
		depExpressions = append(depExpressions, expandDependency(dep, depTask))
	}
	return strings.Join(depExpressions, " && ")
}

// GetOriginalTask returns the original DAG task by name.
func (w *WorkflowTasks) GetOriginalTask(taskName string) Task {
	return w.taskMap[taskName]
}

// TaskNames returns all task names.
func (w *WorkflowTasks) TaskNames() []string {
	names := make([]string, 0, len(w.taskMap))
	for name := range w.taskMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetTaskFinishedAtTime returns the finished time of a task (for ancestry ordering).
func (w *WorkflowTasks) GetTaskFinishedAtTime(taskName string) time.Time {
	node := w.Store.GetNode(taskName)
	if node == nil {
		return time.Time{}
	}
	if !node.FinishedAt.IsZero() {
		return node.FinishedAt.Time
	}
	return node.StartedAt.Time
}