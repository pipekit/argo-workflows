package dag

import wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

// Task represents a task in a workflow (DAG or Steps) that has a name and dependency information.
type Task interface {
	GetName() string
	GetDepends() string
	GetDependencies() []string
	GetContinueOn() *wfv1.ContinueOn
}

// DAGTask adapts wfv1.DAGTask to the Task interface.
type DAGTask struct {
	*wfv1.DAGTask
}

func (t *DAGTask) GetName() string {
	return t.Name
}

func (t *DAGTask) GetDepends() string {
	return t.Depends
}

func (t *DAGTask) GetDependencies() []string {
	return t.Dependencies
}

func (t *DAGTask) GetContinueOn() *wfv1.ContinueOn {
	return t.ContinueOn
}
