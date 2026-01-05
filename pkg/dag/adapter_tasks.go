package dag

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

// TaskResult represents the result state of a dependency task.
// This mirrors the TaskResults struct in workflow/controller/dag.go.
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

// WorkflowTasks adapts Argo's DAG tasks as a Tasks collection for the Build Systems framework.
// It provides task lookup and dependency resolution using the same logic as the DAG controller.
type WorkflowTasks struct {
	// Tasks is the slice of DAG tasks from the template.
	Tasks []wfv1.DAGTask
	// Store provides access to current node states for dependency resolution.
	Store *WorkflowStore
	// Workflow is the workflow for checking hooks completion.
	Workflow *wfv1.Workflow

	mu sync.RWMutex
	// taskMap provides fast lookup of tasks by name.
	taskMap map[string]*wfv1.DAGTask
	// dependencies caches computed dependencies for each task.
	dependencies map[string][]string
	// dependsLogic caches resolved depends expressions.
	dependsLogic map[string]string
}

// NewWorkflowTasks creates a new WorkflowTasks adapter.
func NewWorkflowTasks(tasks []wfv1.DAGTask, store *WorkflowStore, wf *wfv1.Workflow) *WorkflowTasks {
	taskMap := make(map[string]*wfv1.DAGTask, len(tasks))
	for i := range tasks {
		taskMap[tasks[i].Name] = &tasks[i]
	}
	return &WorkflowTasks{
		Tasks:        tasks,
		Store:        store,
		Workflow:     wf,
		taskMap:      taskMap,
		dependencies: make(map[string][]string),
		dependsLogic: make(map[string]string),
	}
}

// GetTask retrieves the task definition for a given key (task name).
func (w *WorkflowTasks) GetTask(key Key) (*Task[Key, NodeValue], bool) {
	dagTask, ok := w.taskMap[key]
	if !ok {
		return nil, false
	}

	// Get static dependencies
	deps, _ := w.GetDependencies(context.Background(), key)

	// Create a Task that wraps the DAG task
	task := &Task[Key, NodeValue]{
		Key:          key,
		Dependencies: deps,
		Run: func(fetch Fetch[Key, NodeValue]) (NodeValue, error) {
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
			if val, ok := w.Store.GetValue(key); ok {
				return val, nil
			}
			// No value yet - this would trigger actual execution in the controller
			return NodeValue{}, NewSuspendErrorWithReason(key, "task not yet executed")
		},
	}

	// Store reference to original DAG task for later use
	_ = dagTask

	return task, true
}

// regex patterns for dependency parsing (same as in workflow/common/ancestry.go)
var (
	taskNameRegex   = regexp.MustCompile(`([a-zA-Z0-9\[\]\.\-_]+?\.[A-Z][a-zA-Z]+)|([a-zA-Z0-9\[\]\.\-_]+)`)
	taskResultRegex = regexp.MustCompile(`([a-zA-Z0-9\[\]\.\-_]+?\.[A-Z][a-zA-Z]+)`)
)

// GetDependencies returns the dependencies for a task.
// This parses the depends expression to extract dependency task names.
func (w *WorkflowTasks) GetDependencies(ctx context.Context, key Key) ([]Key, error) {
	w.mu.RLock()
	if deps, ok := w.dependencies[key]; ok {
		w.mu.RUnlock()
		return deps, nil
	}
	w.mu.RUnlock()

	w.resolveDependencies(ctx, key)

	w.mu.RLock()
	deps := w.dependencies[key]
	w.mu.RUnlock()

	return deps, nil
}

// GetDependsLogic returns the resolved depends expression for a task.
func (w *WorkflowTasks) GetDependsLogic(ctx context.Context, taskName string) string {
	w.mu.RLock()
	if logic, ok := w.dependsLogic[taskName]; ok {
		w.mu.RUnlock()
		return logic
	}
	w.mu.RUnlock()

	w.resolveDependencies(ctx, taskName)

	w.mu.RLock()
	logic := w.dependsLogic[taskName]
	w.mu.RUnlock()

	return logic
}

// normalizeTaskName normalizes a task name to be a valid identifier in expressions.
func normalizeTaskName(name string) string {
	r := strings.NewReplacer("-", "_", ".", "_", "[", "_", "]", "_")
	return r.Replace(name)
}

// resolveDependencies computes and caches dependencies for a task.
// This mirrors common.GetTaskDependencies but is self-contained to avoid import cycles.
func (w *WorkflowTasks) resolveDependencies(_ context.Context, taskName string) {
	task, ok := w.taskMap[taskName]
	if !ok {
		return
	}

	depends := w.getTaskDependsLogic(task)
	matches := taskNameRegex.FindAllStringSubmatchIndex(depends, -1)

	type replacement struct {
		start int
		end   int
		text  string
	}

	var replacements []replacement
	dependencySet := make(map[string]bool)

	for _, matchGroup := range matches {
		// Matched taskName.TaskResult
		if matchGroup[2] != -1 {
			match := depends[matchGroup[2]:matchGroup[3]]
			// Split by the LAST dot to separate task name from result
			lastDot := strings.LastIndex(match, ".")
			if lastDot != -1 {
				taskName := match[:lastDot]
				result := match[lastDot+1:]
				dependencySet[taskName] = true

				normalized := normalizeTaskName(taskName)
				if normalized != taskName {
					replacements = append(replacements, replacement{
						start: matchGroup[2],
						end:   matchGroup[3],
						text:  fmt.Sprintf("%s.%s", normalized, result),
					})
				}
			}
		} else if matchGroup[4] != -1 {
			// Matched plain taskName
			match := depends[matchGroup[4]:matchGroup[5]]
			dependencySet[match] = true

			depTask := w.taskMap[match]
			replacements = append(replacements, replacement{
				start: matchGroup[4],
				end:   matchGroup[5],
				text:  w.expandDependency(match, depTask),
			})
		}
	}

	// Apply replacements
	if len(replacements) > 0 {
		// Sort in descending order by start position to replace from end to start
		sort.Slice(replacements, func(i, j int) bool {
			return replacements[i].start > replacements[j].start
		})
		for _, r := range replacements {
			depends = depends[:r.start] + r.text + depends[r.end:]
		}
	}

	// Convert set to slice
	deps := make([]string, 0, len(dependencySet))
	for dep := range dependencySet {
		deps = append(deps, dep)
	}

	w.mu.Lock()
	w.dependencies[taskName] = deps
	w.dependsLogic[taskName] = depends
	w.mu.Unlock()
}

// getTaskDependsLogic returns the depends expression for a task.
func (w *WorkflowTasks) getTaskDependsLogic(task *wfv1.DAGTask) string {
	if task.Depends != "" {
		return task.Depends
	}

	// For backwards compatibility, convert dependencies list to depends expression
	var depExpressions []string
	for _, dep := range task.Dependencies {
		depTask := w.taskMap[dep]
		depExpressions = append(depExpressions, w.expandDependency(dep, depTask))
	}
	return strings.Join(depExpressions, " && ")
}

// expandDependency expands a bare task name to a full depends expression.
func (w *WorkflowTasks) expandDependency(depName string, depTask *wfv1.DAGTask) string {
	normalizedName := normalizeTaskName(depName)
	resultForTask := func(result string) string { return fmt.Sprintf("%s.%s", normalizedName, result) }

	taskDepends := []string{
		resultForTask("Succeeded"),
		resultForTask("Skipped"),
		resultForTask("Daemoned"),
	}

	if depTask != nil && depTask.ContinueOn != nil {
		if depTask.ContinueOn.Error {
			taskDepends = append(taskDepends, resultForTask("Errored"))
		}
		if depTask.ContinueOn.Failed {
			taskDepends = append(taskDepends, resultForTask("Failed"))
		}
	}

	return "(" + strings.Join(taskDepends, " || ") + ")"
}

// GetDAGTask returns the original DAG task by name.
func (w *WorkflowTasks) GetDAGTask(taskName string) *wfv1.DAGTask {
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
