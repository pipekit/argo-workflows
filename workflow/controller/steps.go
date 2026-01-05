package controller

import (
	"context"
	"fmt"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/workflow/templateresolution"
)

// StepAdapter is an adapter for wfv1.WorkflowStep to implement the Task interface.
type StepAdapter struct {
	step         *wfv1.WorkflowStep
	dependencies []string
	groupIndex   int
}

func (s *StepAdapter) GetName() string {
	return fmt.Sprintf("[%d].%s", s.groupIndex, s.step.Name)
}

func (s *StepAdapter) GetDisplayName() string {
	return s.step.Name
}

func (s *StepAdapter) GetTemplateReferenceHolder() wfv1.TemplateReferenceHolder {
	return s.step
}

func (s *StepAdapter) GetArguments() wfv1.Arguments {
	return s.step.Arguments
}

func (s *StepAdapter) GetWithItems() []wfv1.Item {
	return s.step.WithItems
}

func (s *StepAdapter) GetWithParam() string {
	return s.step.WithParam
}

func (s *StepAdapter) GetWithSequence() *wfv1.Sequence {
	return s.step.WithSequence
}

func (s *StepAdapter) GetWhen() string {
	return s.step.When
}

func (s *StepAdapter) GetDependencies() []string {
	return s.dependencies
}

func (s *StepAdapter) ContinuesOn(phase wfv1.NodePhase) bool {
	if s.step.ContinueOn != nil {
		if s.step.ContinueOn.Failed && phase == wfv1.NodeFailed {
			return true
		}
		if s.step.ContinueOn.Error && phase == wfv1.NodeError {
			return true
		}
	}
	return false
}

func (s *StepAdapter) ToDAGTask() *wfv1.DAGTask {
	return &wfv1.DAGTask{
		Name:         s.GetName(),
		Template:     s.step.Template,
		Arguments:    s.step.Arguments,
		WithItems:    s.step.WithItems,
		WithParam:    s.step.WithParam,
		WithSequence: s.step.WithSequence,
		When:         s.step.When,
		ContinueOn:   s.step.ContinueOn,
		OnExit:       s.step.OnExit,
		TemplateRef:  s.step.TemplateRef,
		Hooks:        s.step.Hooks,
		Dependencies: s.dependencies,
	}
}

func (woc *wfOperationCtx) executeSteps(ctx context.Context, nodeName string, tmplCtx *templateresolution.TemplateContext, templateScope string, tmpl *wfv1.Template, orgTmpl wfv1.TemplateReferenceHolder, opts *executeTemplateOpts) (*wfv1.NodeStatus, error) {
	node, err := woc.wf.GetNodeByName(nodeName)
	if err != nil {
		node = woc.initializeExecutableNode(ctx, nodeName, wfv1.NodeTypeSteps, templateScope, tmpl, orgTmpl, opts.boundaryID, wfv1.NodeRunning, opts.nodeFlag, true)
	}

	defer func() {
		nodePhase, err := woc.wf.Status.Nodes.GetPhase(node.ID)
		if err != nil {
			woc.log.WithField("nodeID", node.ID).WithFatal().Error(ctx, "was unable to obtain nodePhase for nodeID")
			panic(fmt.Sprintf("unable to obtain nodePhase for %s", node.ID))
		}
		if nodePhase.Fulfilled(node.TaskResultSynced) {
			woc.killDaemonedChildren(ctx, node.ID)
		}
	}()

	var tasks []Task
	var prevStepNames []string
	for i, stepGroup := range tmpl.Steps {
		var currentStepNames []string
		for _, step := range stepGroup.Steps {
			task := &StepAdapter{
				step:         &step,
				dependencies: prevStepNames,
				groupIndex:   i,
			}
			tasks = append(tasks, task)
			currentStepNames = append(currentStepNames, task.GetName())

			// create the node for the step group
			sgNodeName := fmt.Sprintf("%s[%d]", nodeName, i)
			if _, err := woc.wf.GetNodeByName(sgNodeName); err != nil {
				_ = woc.initializeNode(ctx, sgNodeName, wfv1.NodeTypeStepGroup, tmplCtx.GetTemplateScope(), &wfv1.WorkflowStep{}, node.ID, wfv1.NodeRunning, &wfv1.NodeFlag{}, true)
			}
		}
		prevStepNames = currentStepNames
	}

	engine := NewEngine(woc, nodeName, tmplCtx, tmpl, orgTmpl, node.ID, opts.onExitTemplate)
		err = engine.Execute(ctx, tasks)
		if err != nil {
			return woc.markNodeError(ctx, nodeName, err), nil
		}
	
		return woc.wf.GetNodeByName(nodeName)
	}