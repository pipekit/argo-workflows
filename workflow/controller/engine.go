package controller

import (
	"context"
	"fmt"
	"time"
	"strings"

	"github.com/Knetic/govaluate"
	"github.com/argoproj/argo-workflows/v3/errors"
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/util/logging"
	"github.com/argoproj/argo-workflows/v3/workflow/common/dag"
	"github.com/argoproj/argo-workflows/v3/workflow/templateresolution"
)

// Task is an interface that abstracts the properties of a task that are common to both DAG tasks and steps.
type Task interface {
	GetName() string
	GetDisplayName() string
	GetTemplateReferenceHolder() wfv1.TemplateReferenceHolder
	GetArguments() wfv1.Arguments
	GetWithItems() []wfv1.Item
	GetWithParam() string
	GetWithSequence() *wfv1.Sequence
	GetWhen() string
	GetDependencies() []string
	ContinuesOn(phase wfv1.NodePhase) bool
	ToDAGTask() *wfv1.DAGTask
}

// Engine is the new generic engine for executing tasks in a DAG or steps.
type Engine struct {
	woc            *wfOperationCtx
	evaluator      *dag.DAGEvaluator
	tmplCtx        *templateresolution.TemplateContext
	boundaryID     string
	nodeName       string
	tmpl           *wfv1.Template
	orgTmpl        wfv1.TemplateReferenceHolder
	onExitTemplate bool
	log            logging.Logger
}

// NewEngine creates a new Engine.
func NewEngine(woc *wfOperationCtx, nodeName string, tmplCtx *templateresolution.TemplateContext, tmpl *wfv1.Template, orgTmpl wfv1.TemplateReferenceHolder, boundaryID string, onExitTemplate bool) *Engine {
	return &Engine{
		woc:            woc,
		nodeName:       nodeName,
		tmplCtx:        tmplCtx,
		tmpl:           tmpl,
		orgTmpl:        orgTmpl,
		boundaryID:     boundaryID,
		onExitTemplate: onExitTemplate,
		log:            woc.log,
	}
}

// Execute executes a list of tasks.
func (e *Engine) Execute(ctx context.Context, tasks []Task) error {
	taskMap := make(map[string]*wfv1.DAGTask)
	for _, task := range tasks {
		// This is a hack for now. The DAGEvaluator expects DAGTasks.
		// We will need to create a more generic way to represent tasks.
		taskMap[task.GetName()] = task.ToDAGTask()
	}
	dagTmpl := &wfv1.Template{
		DAG: &wfv1.DAGTemplate{},
	}
	for _, task := range taskMap {
		dagTmpl.DAG.Tasks = append(dagTmpl.DAG.Tasks, *task)
	}
	e.evaluator = dag.NewDAGEvaluator(e.woc.wf, dagTmpl, e.boundaryID, e.nodeName)

	targetTasks := e.evaluator.GetTargetTasks(ctx)
	e.log.Info(ctx, fmt.Sprintf("Engine.Execute: targetTasks=%v", targetTasks))

		for _, task := range tasks {
			taskNode := e.getTaskNode(ctx, task.GetName())
			if taskNode != nil && taskNode.IsDaemoned() {
				e.log.Info(ctx, fmt.Sprintf("Engine.Execute: executing daemoned task %s", task.GetName()))
				e.executeTask(ctx, task)
			}
		}
	
		onExitCompleted := true
		prefix := "tasks"
		if e.tmpl.GetType() == wfv1.TemplateTypeSteps {
			prefix = "steps"
		}
		for _, task := range tasks {
			taskName := task.GetName()
			taskNode := e.getTaskNode(ctx, taskName)
			if taskNode != nil {
				scope, err := e.buildLocalScopeFromTask(ctx, task)
				if err != nil {
					e.woc.markNodeError(ctx, e.nodeName, err)
					return err
				}
				scope.addParamToScope(fmt.Sprintf("%s.%s.status", prefix, task.GetDisplayName()), string(taskNode.Phase))
				hookCompleted, err := e.woc.executeTmplLifeCycleHook(ctx, scope, task.ToDAGTask().Hooks, taskNode, e.boundaryID, e.tmplCtx, prefix+"."+task.GetDisplayName())
				if err != nil {
					e.woc.markNodeError(ctx, e.nodeName, err)
					return err
				}
				if !hookCompleted {
					onExitCompleted = false
					continue
				}
				if taskNode.Fulfilled() {
					if taskNode.Completed() {
						hasOnExitNode, onExitNode, err := e.runOnExitNode(ctx, task.ToDAGTask().GetExitHook(e.woc.execWf.Spec.Arguments), taskNode, e.boundaryID, e.tmplCtx, prefix+"."+task.GetDisplayName(), scope)
						if err != nil {
							return err
						}
						if hasOnExitNode && (onExitNode == nil || !onExitNode.Fulfilled()) {
							onExitCompleted = false
						}
					}
				}
			}
		}
	
		readyTasks := e.evaluator.GetReadyTasks(ctx)
		e.log.Info(ctx, fmt.Sprintf("Engine.Execute: readyTasks=%v", readyTasks))
		for _, taskName := range readyTasks {
			task := e.getTaskByName(tasks, taskName)
			e.executeTask(ctx, task)
		}
	
		dagPhase, err := e.assessDAGPhase(ctx, tasks, e.woc.GetShutdownStrategy().Enabled() && onExitCompleted)
	if err != nil {
		return err
	}
			
				switch dagPhase {
				case wfv1.NodeRunning:
					return nil
				case wfv1.NodeError, wfv1.NodeFailed:
					err = e.updateOutboundNodesForTargetTasks(ctx, targetTasks)
					if err != nil {
						return err
					}
					_ = e.woc.markNodePhase(ctx, e.nodeName, dagPhase)
					return nil
				}
			
				err = e.setDAGOutputs(ctx)
				if err != nil {
					return err
				}
			
				err = e.updateOutboundNodesForTargetTasks(ctx, targetTasks)
				if err != nil {
					return err
				}
				_ = e.woc.markNodePhase(ctx, e.nodeName, wfv1.NodeSucceeded)
				return nil
			}
			
func (e *Engine) runOnExitNode(ctx context.Context, exitHook *wfv1.LifecycleHook, taskNode *wfv1.NodeStatus, boundaryID string, tmplCtx *templateresolution.TemplateContext, scopePrefix string, scope *wfScope) (bool, *wfv1.NodeStatus, error) {
	e.log.Info(ctx, fmt.Sprintf("runOnExitNode check: taskNode=%s exitHook=%v", taskNode.Name, exitHook != nil))
	if exitHook == nil {
		return false, nil, nil
	}
	onExitNodeName := fmt.Sprintf("%s.onExit", taskNode.Name)
	onExitNode, err := e.woc.wf.GetNodeByName(onExitNodeName)
	if err != nil {
		e.log.Info(ctx, fmt.Sprintf("Running OnExit node for %s", taskNode.Name))
		onExitNode, err = e.woc.executeTemplate(ctx, onExitNodeName, toTemplateReferenceHolder(exitHook), tmplCtx, exitHook.Arguments, &executeTemplateOpts{
			boundaryID:     boundaryID,
			onExitTemplate: true,
			nodeFlag:       &wfv1.NodeFlag{Hooked: true},
			scope:          scope,
			scopePrefix:    scopePrefix,
		})
		if err != nil {
			return true, nil, err
		}
		if onExitNode != nil {
			e.woc.addChildNode(ctx, taskNode.Name, onExitNode.Name)
		}
	}
	return true, onExitNode, nil
}

func toTemplateReferenceHolder(lifecycleHook *wfv1.LifecycleHook) wfv1.TemplateReferenceHolder {
	return &wfv1.WorkflowStep{
		Template:    lifecycleHook.Template,
		TemplateRef: lifecycleHook.TemplateRef,
	}
}
func (e *Engine) executeTask(ctx context.Context, task Task) (*wfv1.NodeStatus, error) {
	taskName := task.GetName()
	taskNodeName := e.taskNodeName(taskName)

	taskNode := e.getTaskNode(ctx, taskName)
	prefix := "tasks"
	if e.tmpl.GetType() == wfv1.TemplateTypeSteps {
		prefix = "steps"
	}
	if taskNode != nil && (taskNode.Fulfilled() || taskNode.Phase == wfv1.NodeRunning) {
		// build a local scope for the task
		scope, err := e.buildLocalScopeFromTask(ctx, task)
		if err != nil {
			return e.woc.markNodeError(ctx, taskNodeName, err), err
		}
		scope.addParamToScope(fmt.Sprintf("%s.%s.status", prefix, task.GetDisplayName()), string(taskNode.Phase))
		hookCompleted, err := e.woc.executeTmplLifeCycleHook(ctx, scope, task.ToDAGTask().Hooks, taskNode, e.boundaryID, e.tmplCtx, prefix+"."+task.GetDisplayName())
		if err != nil {
			e.woc.markNodeError(ctx, taskNodeName, err)
		}
		if !hookCompleted {
			return taskNode, nil
		}
	}

	if taskNode != nil && taskNode.Fulfilled() {
		e.log.WithFields(logging.Fields{"task": taskName, "node": taskNodeName}).Debug(ctx, "task already fulfilled")
		return taskNode, nil
	}

	// build a local scope for the task
	scope, err := e.buildLocalScopeFromTask(ctx, task)
	if err != nil {
		return e.woc.markNodeError(ctx, taskNodeName, err), err
	}

	// Check the task's when clause to decide if it should execute
	proceed, err := shouldExecute(task.GetWhen())
	if err != nil {
		e.woc.initializeNode(ctx, taskNodeName, wfv1.NodeTypeSkipped, e.tmplCtx.GetTemplateScope(), e.orgTmpl, e.boundaryID, wfv1.NodeError, &wfv1.NodeFlag{}, true, err.Error())
		e.woc.addChildNode(ctx, e.nodeName, taskNodeName)
		e.woc.markNodeError(ctx, taskNodeName, err)
		return e.woc.markNodeError(ctx, e.nodeName, err), err
	}
	if !proceed {
		if _, err := e.woc.wf.GetNodeByName(taskNodeName); err != nil {
			skipReason := fmt.Sprintf("when '%s' evaluated false", task.GetWhen())
			e.log.WithFields(logging.Fields{"childNodeName": taskNodeName, "skipReason": skipReason}).Info(ctx, "Skipping")
			e.woc.initializeNode(ctx, taskNodeName, wfv1.NodeTypeSkipped, e.tmplCtx.GetTemplateScope(), e.orgTmpl, e.boundaryID, wfv1.NodeSkipped, &wfv1.NodeFlag{}, true, skipReason)
			e.woc.addChildNode(ctx, e.nodeName, taskNodeName)
		}
		return nil, nil
	}

	// Expand withItems if necessary
	if task.GetWithItems() != nil || task.GetWithParam() != "" || task.GetWithSequence() != nil {
		expandedTasks, err := e.evaluator.ExpandTask(ctx, *task.ToDAGTask(), scope.getParameters(), e.woc)
		if err != nil {
			return e.woc.markNodeError(ctx, taskNodeName, err), err
		}
		var expandedNodes []*wfv1.NodeStatus
		for _, expandedTask := range expandedTasks {
			dagTask := expandedTask
			node, err := e.executeTask(ctx, &DAGTaskAdapter{task: &dagTask})
			if err != nil {
				return e.woc.markNodeError(ctx, taskNodeName, err), err
			}
			expandedNodes = append(expandedNodes, node)
		}
		// create a task group node
		tgNode := e.woc.initializeNode(ctx, taskNodeName, wfv1.NodeTypeTaskGroup, e.tmplCtx.GetTemplateScope(), e.orgTmpl, e.boundaryID, wfv1.NodeRunning, &wfv1.NodeFlag{}, true)
		for _, expandedNode := range expandedNodes {
			e.woc.addChildNode(ctx, tgNode.Name, expandedNode.Name)
		}
		return tgNode, nil
	}

	childNode, err := e.woc.executeTemplate(ctx, taskNodeName, task.GetTemplateReferenceHolder(), e.tmplCtx, task.GetArguments(), &executeTemplateOpts{boundaryID: e.boundaryID, onExitTemplate: e.onExitTemplate})
	if err != nil {
		return e.woc.markNodeError(ctx, e.nodeName, fmt.Errorf("task %s errored: %w", taskName, err)), err
	}
	if childNode != nil {
		e.woc.addChildNode(ctx, e.nodeName, childNode.Name)
	}
	return childNode, nil
}

func (e *Engine) getTaskByName(tasks []Task, name string) Task {
	for _, task := range tasks {
		if task.GetName() == name {
			return task
		}
	}
	return nil
}

// taskNodeName formulates the nodeName for a dag task
func (e *Engine) taskNodeName(taskName string) string {
	if strings.HasPrefix(taskName, "[") {
		return fmt.Sprintf("%s%s", e.nodeName, taskName)
	}
	return fmt.Sprintf("%s.%s", e.nodeName, taskName)
}

// taskNodeID formulates the node ID for a dag task
func (e *Engine) taskNodeID(taskName string) string {
	nodeName := e.taskNodeName(taskName)
	return e.woc.wf.NodeID(nodeName)
}

// getTaskNode returns the node status of a task.
func (e *Engine) getTaskNode(ctx context.Context, taskName string) *wfv1.NodeStatus {
	nodeID := e.taskNodeID(taskName)
	node, err := e.woc.wf.Status.Nodes.Get(nodeID)
	if err != nil {
		e.log.WithFields(logging.Fields{"nodeID": nodeID, "taskName": taskName}).Warn(ctx, "was unable to obtain the node")
		return nil
	}
	return node
}

// assessDAGPhase assesses the overall DAG status
func (e *Engine) assessDAGPhase(ctx context.Context, tasks []Task, isShutdown bool) (wfv1.NodePhase, error) {
	if isShutdown {
		return wfv1.NodeFailed, nil
	}

	results := e.evaluator.EvaluateAll(ctx)
	var phase wfv1.NodePhase = wfv1.NodeSucceeded
	for _, result := range results {
		if result.CurrentState == dag.TaskStateRunning || result.CurrentState == dag.TaskStatePending {
			phase = wfv1.NodeRunning
			break
		}
		if result.CurrentState == dag.TaskStateFailed || result.CurrentState == dag.TaskStateError {
			task := e.getTaskByName(tasks, result.TaskName)
			if !task.ContinuesOn(dag.TaskStateToPhase(result.CurrentState)) {
				phase = dag.TaskStateToPhase(result.CurrentState)
				if e.tmpl.DAG != nil && (e.tmpl.DAG.FailFast == nil || *e.tmpl.DAG.FailFast) {
					break
				}
			}
		}
	}

	return phase, nil
}

func (e *Engine) GetTaskFinishedAtTime(ctx context.Context, taskName string) time.Time {
	node := e.getTaskNode(ctx, taskName)
	if node == nil {
		return time.Time{}} // zero time
	if !node.FinishedAt.IsZero() {
		return node.FinishedAt.Time
	}
	return node.StartedAt.Time
}

// buildLocalScopeFromTask builds a local scope for a task.
func (e *Engine) buildLocalScopeFromTask(ctx context.Context, task Task) (*wfScope, error) {
	scope := createScope(e.tmpl)
	// Add outputs of dependencies to the scope
	for _, depName := range task.GetDependencies() {
		depNode := e.getTaskNode(ctx, depName)
		if depNode == nil {
			return nil, fmt.Errorf("dependency %s of task %s not found", depName, task.GetName())
		}
		prefix := fmt.Sprintf("tasks.%s", depName)
		e.woc.buildLocalScope(scope, prefix, depNode)
	}
	return scope, nil
}

// setDAGOutputs sets the outputs of the DAG.
func (e *Engine) setDAGOutputs(ctx context.Context) error {
	node, err := e.woc.wf.GetNodeByName(e.nodeName)
	if err != nil {
		return err
	}
	// Create a new scope for the DAG outputs
	scope := createScope(e.tmpl)
	var tasks []wfv1.DAGTask
	if e.tmpl.DAG != nil {
		tasks = e.tmpl.DAG.Tasks
	} else if e.tmpl.Steps != nil {
		for _, stepGroup := range e.tmpl.Steps {
			for _, step := range stepGroup.Steps {
				tasks = append(tasks, wfv1.DAGTask{Name: step.Name})
			}
		}
	}

	for _, task := range tasks {
		taskNode := e.getTaskNode(ctx, task.Name)
		if taskNode != nil {
			prefix := fmt.Sprintf("tasks.%s", task.Name)
			e.woc.buildLocalScope(scope, prefix, taskNode)
		}
	}

	outputs, err := e.woc.getTemplateOutputsFromScope(ctx, e.tmpl, scope)
	if err != nil {
		return err
	}
	if outputs != nil {
		node.Outputs = outputs
		e.woc.addOutputsToGlobalScope(ctx, node.Outputs)
		e.woc.wf.Status.Nodes.Set(ctx, node.ID, *node)
	}
	return nil
}

// updateOutboundNodesForTargetTasks sets the outbound nodes for the target tasks.
func (e *Engine) updateOutboundNodesForTargetTasks(ctx context.Context, targetTasks []string) error {
	outbound := make([]string, 0)
	for _, taskName := range targetTasks {
		taskNode := e.getTaskNode(ctx, taskName)
		if taskNode != nil {
			outbound = append(outbound, e.woc.getOutboundNodes(ctx, taskNode.ID)...)
		}
	}
	node, err := e.woc.wf.GetNodeByName(e.nodeName)
	if err != nil {
		return err
	}
	node.OutboundNodes = outbound
	e.woc.wf.Status.Nodes.Set(ctx, node.ID, *node)
	return nil
}

// shouldExecute evaluates a already substituted when expression to decide whether or not a step should execute
func shouldExecute(when string) (bool, error) {
	if when == "" {
		return true, nil
	}
	expression, err := govaluate.NewEvaluableExpression(when)
	if err != nil {
		if strings.Contains(err.Error(), "Invalid token") {
			return false, errors.Errorf(errors.CodeBadRequest, "Invalid 'when' expression '%s': %v (hint: try wrapping the affected expression in quotes)", when, err)
		}
		return false, errors.Errorf(errors.CodeBadRequest, "Invalid 'when' expression '%s': %v", when, err)
	}
	// The following loop converts govaluate variables (which we don't use), into strings. This
	// allows us to have expressions like: "foo != bar" without requiring foo and bar to be quoted.
	tokens := expression.Tokens()
	for i, tok := range tokens {
		switch tok.Kind {
		case govaluate.VARIABLE:
			tok.Kind = govaluate.STRING
		default:
			continue
		}
		tokens[i] = tok
	}
	expression, err = govaluate.NewEvaluableExpressionFromTokens(tokens)
	if err != nil {
		return false, errors.InternalWrapErrorf(err, "Failed to parse 'when' expression '%s': %v", when, err)
	}
	result, err := expression.Evaluate(nil)
	if err != nil {
		return false, errors.InternalWrapErrorf(err, "Failed to evaluate 'when' expresion '%s': %v", when, err)
	}
	boolRes, ok := result.(bool)
	if !ok {
		return false, errors.Errorf(errors.CodeBadRequest, "Expected boolean evaluation for '%s'. Got %v", when, result)
	}
	return boolRes, nil
}
