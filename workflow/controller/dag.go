package controller

import (
	"context"
	"fmt"
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/workflow/templateresolution"
)

// DAGTaskAdapter is an adapter for wfv1.DAGTask to implement the Task interface.
type DAGTaskAdapter struct {
	task *wfv1.DAGTask
}

func (t *DAGTaskAdapter) GetName() string {
	return t.task.Name
}

func (t *DAGTaskAdapter) GetDisplayName() string {
	return t.task.Name
}

func (t *DAGTaskAdapter) GetTemplateReferenceHolder() wfv1.TemplateReferenceHolder {
	return t.task
}

func (t *DAGTaskAdapter) GetArguments() wfv1.Arguments {
	return t.task.Arguments
}

func (t *DAGTaskAdapter) GetWithItems() []wfv1.Item {
	return t.task.WithItems
}

func (t *DAGTaskAdapter) GetWithParam() string {
	return t.task.WithParam
}

func (t *DAGTaskAdapter) GetWithSequence() *wfv1.Sequence {
	return t.task.WithSequence
}

func (t *DAGTaskAdapter) GetWhen() string {
	return t.task.When
}

func (t *DAGTaskAdapter) GetDependencies() []string {
	return t.task.Dependencies
}

func (t *DAGTaskAdapter) ContinuesOn(phase wfv1.NodePhase) bool {
	return t.task.ContinuesOn(phase)
}

func (t *DAGTaskAdapter) ToDAGTask() *wfv1.DAGTask {
	return t.task
}


func (woc *wfOperationCtx) executeDAG(ctx context.Context, nodeName string, tmplCtx *templateresolution.TemplateContext, templateScope string, tmpl *wfv1.Template, orgTmpl wfv1.TemplateReferenceHolder, opts *executeTemplateOpts) (*wfv1.NodeStatus, error) {
	node, err := woc.wf.GetNodeByName(nodeName)
	if err != nil {
		node = woc.initializeExecutableNode(ctx, nodeName, wfv1.NodeTypeDAG, templateScope, tmpl, orgTmpl, opts.boundaryID, wfv1.NodeRunning, opts.nodeFlag, true)
	}

	defer func() {
		node, err := woc.wf.Status.Nodes.Get(node.ID)
		if err != nil {
			// CRITICAL ERROR IF THIS BRANCH IS REACHED -> PANIC
			panic(fmt.Sprintf("expected node for %s due to preceded initializeExecutableNode but couldn't find it", node.ID))
		}
		if node.Fulfilled() {
			woc.killDaemonedChildren(ctx, node.ID)
		}
	}()

	engine := NewEngine(woc, nodeName, tmplCtx, tmpl, orgTmpl, node.ID, opts.onExitTemplate)

	var tasks []Task
	for i := range tmpl.DAG.Tasks {
		tasks = append(tasks, &DAGTaskAdapter{task: &tmpl.DAG.Tasks[i]})
	}

	err = engine.Execute(ctx, tasks)
	if err != nil {
		return woc.markNodeError(ctx, nodeName, err), nil
	}

	// The engine will have updated the node status.
	// We just need to return the node.
	return woc.wf.GetNodeByName(nodeName)
}
