// Package dag implements the Build Systems à la Carte framework for DAG dependency evaluation.
// This file provides Argo Workflows integration adapters that bridge the abstract framework
// to Argo Workflows' concrete types.
package dag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/util/expr/argoexpr"
)

// NodeValue wraps an Argo node status as a Value in the Build Systems framework.
// This adapter allows workflow node outputs to be used with the generic Store and Scheduler.
type NodeValue struct {
	// NodeStatus is the underlying Argo workflow node status.
	NodeStatus *wfv1.NodeStatus
}

// Hash implements Value.Hash for NodeValue.
// The hash is computed from the node phase and outputs for change detection.
func (n NodeValue) Hash() Hash {
	if n.NodeStatus == nil {
		return ""
	}

	h := sha256.New()

	// Include phase in hash
	h.Write([]byte(string(n.NodeStatus.Phase)))

	// Include outputs in hash if present
	if n.NodeStatus.Outputs != nil {
		// Hash parameters
		for _, param := range n.NodeStatus.Outputs.Parameters {
			h.Write([]byte(param.Name))
			if param.Value != nil {
				h.Write([]byte(param.Value.String()))
			}
		}
		// Hash artifact names (not contents, just names)
		for _, art := range n.NodeStatus.Outputs.Artifacts {
			h.Write([]byte(art.Name))
		}
		// Hash result if present
		if n.NodeStatus.Outputs.Result != nil {
			h.Write([]byte(*n.NodeStatus.Outputs.Result))
		}
		// Hash exit code if present
		if n.NodeStatus.Outputs.ExitCode != nil {
			h.Write([]byte(*n.NodeStatus.Outputs.ExitCode))
		}
	}

	return Hash(hex.EncodeToString(h.Sum(nil)))
}

// Phase returns the node phase.
func (n NodeValue) Phase() wfv1.NodePhase {
	if n.NodeStatus == nil {
		return ""
	}
	return n.NodeStatus.Phase
}

// Outputs returns the node outputs.
func (n NodeValue) Outputs() *wfv1.Outputs {
	if n.NodeStatus == nil {
		return nil
	}
	return n.NodeStatus.Outputs
}

// IsSucceeded returns true if the node succeeded.
func (n NodeValue) IsSucceeded() bool {
	return n.NodeStatus != nil && n.NodeStatus.Phase == wfv1.NodeSucceeded
}

// IsFailed returns true if the node failed.
func (n NodeValue) IsFailed() bool {
	return n.NodeStatus != nil && n.NodeStatus.Phase == wfv1.NodeFailed
}

// IsError returns true if the node is in error state.
func (n NodeValue) IsError() bool {
	return n.NodeStatus != nil && n.NodeStatus.Phase == wfv1.NodeError
}

// IsSkipped returns true if the node was skipped.
func (n NodeValue) IsSkipped() bool {
	return n.NodeStatus != nil && n.NodeStatus.Phase == wfv1.NodeSkipped
}

// IsOmitted returns true if the node was omitted.
func (n NodeValue) IsOmitted() bool {
	return n.NodeStatus != nil && n.NodeStatus.Phase == wfv1.NodeOmitted
}

// IsDaemoned returns true if the node is daemoned and not pending.
func (n NodeValue) IsDaemoned() bool {
	return n.NodeStatus != nil && n.NodeStatus.IsDaemoned() && n.NodeStatus.Phase != wfv1.NodePending
}

// IsRunning returns true if the node is running.
func (n NodeValue) IsRunning() bool {
	return n.NodeStatus != nil && n.NodeStatus.Phase == wfv1.NodeRunning
}

// IsPending returns true if the node is pending.
func (n NodeValue) IsPending() bool {
	return n.NodeStatus == nil || n.NodeStatus.Phase == wfv1.NodePending
}

// IsFulfilled returns true if the node is in a terminal state.
func (n NodeValue) IsFulfilled() bool {
	return n.NodeStatus != nil && n.NodeStatus.Fulfilled()
}

// NewNodeValue creates a new NodeValue from an Argo NodeStatus.
func NewNodeValue(node *wfv1.NodeStatus) NodeValue {
	return NodeValue{NodeStatus: node}
}

// WorkflowStore adapts Argo's Workflow.Status.Nodes as a Store for the Build Systems framework.
// It provides access to workflow node states and values using task names as keys.
type WorkflowStore struct {
	// Nodes is the underlying Argo nodes map.
	Nodes wfv1.Nodes
	// BoundaryID is the node ID of the DAG boundary node.
	BoundaryID string
	// BoundaryName is the node name of the DAG boundary node.
	BoundaryName string
	// Workflow is the workflow containing the nodes.
	Workflow *wfv1.Workflow

	mu sync.RWMutex
	// info stores per-key build information (for rebuilders that need it)
	info map[Key]any
	// states caches the task states (derived from node phases)
	states map[Key]TaskState
}

// NewWorkflowStore creates a new WorkflowStore from a workflow and DAG context.
func NewWorkflowStore(wf *wfv1.Workflow, boundaryID, boundaryName string) *WorkflowStore {
	return &WorkflowStore{
		Nodes:        wf.Status.Nodes,
		BoundaryID:   boundaryID,
		BoundaryName: boundaryName,
		Workflow:     wf,
		info:         make(map[Key]any),
		states:       make(map[Key]TaskState),
	}
}

// taskNodeName computes the node name for a task (same as dagContext.taskNodeName).
func (s *WorkflowStore) taskNodeName(taskName string) string {
	return fmt.Sprintf("%s.%s", s.BoundaryName, taskName)
}

// taskNodeID computes the node ID for a task (same as dagContext.taskNodeID).
func (s *WorkflowStore) taskNodeID(taskName string) string {
	nodeName := s.taskNodeName(taskName)
	return s.Workflow.NodeID(nodeName)
}

// GetValue retrieves the stored value for a task key.
// Returns the NodeValue and true if found, or empty NodeValue and false if not.
func (s *WorkflowStore) GetValue(key Key) (NodeValue, bool) {
	nodeID := s.taskNodeID(key)
	node, err := s.Nodes.Get(nodeID)
	if err != nil {
		return NodeValue{}, false
	}
	return NodeValue{NodeStatus: node}, true
}

// SetValue stores a value for a task key.
// In practice, this updates the node status in the workflow.
func (s *WorkflowStore) SetValue(key Key, value NodeValue) {
	if value.NodeStatus == nil {
		return
	}
	nodeID := s.taskNodeID(key)
	s.Nodes.Set(context.Background(), nodeID, *value.NodeStatus)
}

// GetInfo retrieves build information for the entire store.
func (s *WorkflowStore) GetInfo() any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

// SetInfo stores build information for the entire store.
func (s *WorkflowStore) SetInfo(info any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if infoMap, ok := info.(map[Key]any); ok {
		s.info = infoMap
	}
}

// GetState returns the current execution state of a task.
// This maps Argo node phases to Build Systems TaskStates.
func (s *WorkflowStore) GetState(key Key) TaskState {
	nodeID := s.taskNodeID(key)
	node, err := s.Nodes.Get(nodeID)
	if err != nil {
		return TaskStatePending // Node doesn't exist, treat as pending
	}
	return PhaseToTaskState(node.Phase)
}

// SetState updates the execution state of a task.
// This is primarily used by the scheduler to track internal state;
// actual node phase updates happen through SetValue.
func (s *WorkflowStore) SetState(key Key, state TaskState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[key] = state
}

// ListKeys returns all task keys in the store.
// This returns task names derived from node names in the DAG.
func (s *WorkflowStore) ListKeys() []Key {
	keys := make([]Key, 0)
	prefix := s.BoundaryName + "."

	for _, node := range s.Nodes {
		// Check if this node belongs to our DAG (has our boundary as parent)
		if node.BoundaryID == s.BoundaryID && strings.HasPrefix(node.Name, prefix) {
			// Extract task name from node name
			taskName := strings.TrimPrefix(node.Name, prefix)
			// Skip expanded task names (contain parentheses)
			if !strings.Contains(taskName, "(") {
				keys = append(keys, taskName)
			}
		}
	}
	return keys
}

// GetNode returns the raw node status for a task.
func (s *WorkflowStore) GetNode(taskName string) *wfv1.NodeStatus {
	nodeID := s.taskNodeID(taskName)
	node, err := s.Nodes.Get(nodeID)
	if err != nil {
		return nil
	}
	return node
}

// PhaseToTaskState converts an Argo NodePhase to a Build Systems TaskState.
func PhaseToTaskState(phase wfv1.NodePhase) TaskState {
	switch phase {
	case wfv1.NodePending:
		return TaskStatePending
	case wfv1.NodeRunning:
		return TaskStateRunning
	case wfv1.NodeSucceeded:
		return TaskStateSucceeded
	case wfv1.NodeFailed:
		return TaskStateFailed
	case wfv1.NodeError:
		return TaskStateError
	case wfv1.NodeSkipped:
		return TaskStateSkipped
	case wfv1.NodeOmitted:
		return TaskStateOmitted
	default:
		return TaskStatePending
	}
}

// TaskStateToPhase converts a Build Systems TaskState to an Argo NodePhase.
func TaskStateToPhase(state TaskState) wfv1.NodePhase {
	switch state {
	case TaskStatePending:
		return wfv1.NodePending
	case TaskStateRunning:
		return wfv1.NodeRunning
	case TaskStateSucceeded:
		return wfv1.NodeSucceeded
	case TaskStateFailed:
		return wfv1.NodeFailed
	case TaskStateError:
		return wfv1.NodeError
	case TaskStateSkipped:
		return wfv1.NodeSkipped
	case TaskStateOmitted:
		return wfv1.NodeOmitted
	default:
		return wfv1.NodePending
	}
}

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
	taskNameRegex   = regexp.MustCompile(`([a-zA-Z0-9][-a-zA-Z0-9]*?\.[A-Z][a-zA-Z]+)|([a-zA-Z0-9][-a-zA-Z0-9]*)`)
	taskResultRegex = regexp.MustCompile(`([a-zA-Z0-9][-a-zA-Z0-9]*?\.[A-Z][a-zA-Z]+)`)
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

	// Resolve dependencies
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

// resolveDependencies computes and caches dependencies for a task.
// This mirrors common.GetTaskDependencies but is self-contained to avoid import cycles.
func (w *WorkflowTasks) resolveDependencies(_ context.Context, taskName string) {
	task, ok := w.taskMap[taskName]
	if !ok {
		return
	}

	depends := w.getTaskDependsLogic(task)
	matches := taskNameRegex.FindAllStringSubmatchIndex(depends, -1)

	type expansionMatch struct {
		taskName string
		start    int
		end      int
	}

	var expansionMatches []expansionMatch
	dependencySet := make(map[string]bool)

	for _, matchGroup := range matches {
		// Matched taskName.TaskResult
		if matchGroup[2] != -1 {
			match := depends[matchGroup[2]:matchGroup[3]]
			split := strings.Split(match, ".")
			dependencySet[split[0]] = true
		} else if matchGroup[4] != -1 {
			// Matched plain taskName
			match := depends[matchGroup[4]:matchGroup[5]]
			dependencySet[match] = true
			expansionMatches = append(expansionMatches, expansionMatch{
				taskName: match,
				start:    matchGroup[4],
				end:      matchGroup[5],
			})
		}
	}

	// Expand bare task names to full depends expressions
	if len(expansionMatches) > 0 {
		// Sort in descending order by start position to replace from end to start
		sort.Slice(expansionMatches, func(i, j int) bool {
			return expansionMatches[i].start > expansionMatches[j].start
		})
		for _, match := range expansionMatches {
			depTask := w.taskMap[match.taskName]
			depends = depends[:match.start] + w.expandDependency(match.taskName, depTask) + depends[match.end:]
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
	resultForTask := func(result string) string { return fmt.Sprintf("%s.%s", depName, result) }

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
	CurrentState TaskState
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
	Scheduler *SuspendingScheduler[Key, NodeValue]
	// Rebuilder is the phase-based rebuilder for the DAG.
	Rebuilder *PhaseBasedRebuilder[Key, NodeValue]
	// Workflow is the underlying workflow.
	Workflow *wfv1.Workflow
	// Template is the DAG template being evaluated.
	Template *wfv1.Template
}

// NewDAGEvaluator creates a new DAGEvaluator for a workflow and DAG template.
func NewDAGEvaluator(wf *wfv1.Workflow, tmpl *wfv1.Template, boundaryID, boundaryName string) *DAGEvaluator {
	store := NewWorkflowStore(wf, boundaryID, boundaryName)
	tasks := NewWorkflowTasks(tmpl.DAG.Tasks, store, wf)

	evaluator := &DAGEvaluator{
		Store:    store,
		Tasks:    tasks,
		Workflow: wf,
		Template: tmpl,
	}

	// Create phase-based rebuilder with depends logic evaluation
	rebuilder := NewPhaseBasedRebuilder[Key, NodeValue](
		func(key Key) TaskState {
			return store.GetState(key)
		},
		func(ctx context.Context, key Key, fetch Fetch[Key, NodeValue]) (bool, bool, error) {
			return evaluator.evaluateDependsLogic(ctx, key)
		},
	)

	// Create the scheduler
	scheduler := NewSuspendingScheduler[Key, NodeValue](
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
func (e *DAGEvaluator) EvaluateDAG(ctx context.Context, targets []Key) ([]EvaluationResult, error) {
	results := make([]EvaluationResult, 0, len(targets))

	for _, target := range targets {
		result := e.EvaluateTask(ctx, target)
		results = append(results, result)
	}

	return results, nil
}

// EvaluateTask evaluates a single task and returns its evaluation result.
func (e *DAGEvaluator) EvaluateTask(ctx context.Context, taskName Key) EvaluationResult {
	result := EvaluationResult{
		TaskName:     taskName,
		CurrentState: e.Store.GetState(taskName),
	}

	// Check if task already has a node
	node := e.Store.GetNode(taskName)
	if node != nil {
		// Task already exists
		if node.Fulfilled() {
			result.ShouldRun = false
			if node.Phase == wfv1.NodeOmitted {
				result.Skipped = true
				result.SkipReason = node.Message
			}
			return result
		}
		if node.Phase == wfv1.NodeRunning {
			result.ShouldRun = false // Already running
			return result
		}
	}

	// Evaluate depends logic
	shouldExecute, canProceed, err := e.evaluateDependsLogic(ctx, taskName)
	if err != nil {
		result.Error = err
		return result
	}

	if !canProceed {
		// Dependencies not ready
		result.Suspended = true
		deps, _ := e.Tasks.GetDependencies(ctx, taskName)
		for _, dep := range deps {
			depNode := e.Store.GetNode(dep)
			if depNode == nil || !depNode.Fulfilled() {
				result.WaitingOn = append(result.WaitingOn, dep)
			}
		}
		return result
	}

	if !shouldExecute {
		// Depends logic evaluated to false - task should be omitted
		result.Skipped = true
		result.SkipReason = "depends condition not met"
		return result
	}

	// Task should be executed
	result.ShouldRun = true
	return result
}

// evaluateDependsLogic evaluates the depends expression for a task.
// Returns:
//   - shouldExecute: true if the task should execute based on depends logic
//   - canProceed: true if all dependencies are in a terminal state
//   - err: error during evaluation
//
// This mirrors dagContext.evaluateDependsLogic from workflow/controller/dag.go.
func (e *DAGEvaluator) evaluateDependsLogic(ctx context.Context, taskName string) (bool, bool, error) {
	node := e.Store.GetNode(taskName)
	if node != nil && node.Fulfilled() {
		return true, true, nil
	}

	evalScope := make(map[string]TaskResult)

	deps, _ := e.Tasks.GetDependencies(ctx, taskName)
	for _, depName := range deps {
		depNode := e.Store.GetNode(depName)

		// If the dependency doesn't exist or isn't fulfilled, we can't proceed
		if depNode == nil || !depNode.Fulfilled() {
			return false, false, nil
		}

		// Check hooks completion (if workflow is available)
		if e.Workflow != nil && !checkAllHooksFulfilled(depNode, e.Workflow.Status.Nodes) {
			return false, false, nil
		}

		// Normalize task name for expression evaluation (replace - with _)
		evalTaskName := strings.ReplaceAll(depName, "-", "_")
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
		return true, true, nil
	}

	// Evaluate the expression
	execute, err := argoexpr.EvalBool(evalLogic, evalScope)
	if err != nil {
		return false, false, fmt.Errorf("unable to evaluate expression '%s': %w", evalLogic, err)
	}

	return execute, true, nil
}

// FindLeafTaskNames finds tasks that no other tasks depend on.
// These are the default target tasks when dag.targets is not specified.
func (e *DAGEvaluator) FindLeafTaskNames(ctx context.Context) []string {
	taskIsLeaf := make(map[string]bool)

	for _, task := range e.Tasks.Tasks {
		if _, ok := taskIsLeaf[task.Name]; !ok {
			taskIsLeaf[task.Name] = true
		}
		deps, _ := e.Tasks.GetDependencies(ctx, task.Name)
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
	ready := make([]string, 0)

	for _, taskName := range e.Tasks.TaskNames() {
		result := e.EvaluateTask(ctx, taskName)
		if result.ShouldRun && !result.Suspended && result.Error == nil {
			ready = append(ready, taskName)
		}
	}

	sort.Strings(ready)
	return ready
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
var _ Store[any, Key, NodeValue] = (*WorkflowStore)(nil)
var _ Tasks[Key, NodeValue] = (*WorkflowTasks)(nil)
var _ Value = NodeValue{}
