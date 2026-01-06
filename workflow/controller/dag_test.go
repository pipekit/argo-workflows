package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/workflow/common/dag"
	"github.com/argoproj/argo-workflows/v3/util/logging"
	"github.com/argoproj/argo-workflows/v3/workflow/common"
)

// TestDagXfail verifies a DAG can fail properly
func TestDagXfail(t *testing.T) {
	wf := wfv1.MustUnmarshalWorkflow("@testdata/dag_xfail.yaml")
	ctx := logging.TestContext(t.Context())
	woc := newWoc(ctx, *wf)
	woc.operate(ctx)
	makePodsPhase(ctx, woc, v1.PodFailed)
	woc.operate(ctx)
	assert.Equal(t, wfv1.WorkflowFailed, woc.wf.Status.Phase)
}

// TestDagRetrySucceeded verifies a DAG will be marked Succeeded if retry was successful
func TestDagRetrySucceeded(t *testing.T) {
	wf := wfv1.MustUnmarshalWorkflow("@testdata/dag_retry_succeeded.yaml")
	ctx := logging.TestContext(t.Context())
	woc := newWoc(ctx, *wf)
	woc.operate(ctx)
	assert.Equal(t, wfv1.WorkflowSucceeded, woc.wf.Status.Phase)
}

// TestDagRetryExhaustedXfail verifies we fail properly when we exhaust our retries
func TestDagRetryExhaustedXfail(t *testing.T) {
	wf := wfv1.MustUnmarshalWorkflow("@testdata/dag-exhausted-retries-xfail.yaml")
	ctx := logging.TestContext(t.Context())
	woc := newWoc(ctx, *wf)
	woc.operate(ctx)
	assert.Equal(t, wfv1.WorkflowFailed, woc.wf.Status.Phase)
}

// TestDagDisableFailFast test disable fail fast function
func TestDagDisableFailFast(t *testing.T) {
	wf := wfv1.MustUnmarshalWorkflow("@testdata/dag-disable-fail-fast.yaml")
	ctx := logging.TestContext(t.Context())
	woc := newWoc(ctx, *wf)
	woc.operate(ctx)
	assert.Equal(t, wfv1.WorkflowFailed, woc.wf.Status.Phase)
}

var dynamicSingleDag = `
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
 generateName: dag-diamond-
spec:
 entrypoint: diamond
 templates:
 - name: diamond
   dag:
     tasks:
     - name: A
       template: %s
       %s
     - name: TestSingle
       template: Succeeded
       depends: A.%s

 - name: Succeeded
   container:
     image: alpine:3.23
     command: [sh, -c, "exit 0"]

 - name: Failed
   container:
     image: alpine:3.23
     command: [sh, -c, "exit 1"]

 - name: Skipped
   container:
     image: alpine:3.23
     command: [sh, -c, "echo Hello"]
`

func TestSingleDependency(t *testing.T) {
	statusMap := map[string]v1.PodPhase{"Succeeded": v1.PodSucceeded, "Failed": v1.PodFailed}
	var closer context.CancelFunc
	var controller *WorkflowController
	for _, status := range []string{"Succeeded", "Failed", "Skipped"} {
		fmt.Printf("\n\n\nCurrent status %s\n\n\n", status)
		ctx := logging.TestContext(t.Context())
		closer, controller = newController(ctx)
		wfcset := controller.wfclientset.ArgoprojV1alpha1().Workflows("")

		// If the status is "skipped" skip the root node.
		var wfString string
		if status == "Skipped" {
			wfString = fmt.Sprintf(dynamicSingleDag, status, `when: "False == True"`, status)
		} else {
			wfString = fmt.Sprintf(dynamicSingleDag, status, "", status)
		}
		wf := wfv1.MustUnmarshalWorkflow(wfString)

		wf, err := wfcset.Create(ctx, wf, metav1.CreateOptions{})
		require.NoError(t, err)
		wf, err = wfcset.Get(ctx, wf.Name, metav1.GetOptions{})
		require.NoError(t, err)
		woc := newWorkflowOperationCtx(ctx, wf, controller)

		woc.operate(ctx)
		// Mark the status of the pod according to the test
		if _, ok := statusMap[status]; ok {
			makePodsPhase(ctx, woc, statusMap[status])
		} else {
			makePodsPhase(ctx, woc, v1.PodPending)
		}

		woc = newWorkflowOperationCtx(ctx, woc.wf, controller)
		woc.operate(ctx)
		found := false
		for _, node := range woc.wf.Status.Nodes {
			if strings.Contains(node.Name, "TestSingle") {
				found = true
				assert.Equal(t, wfv1.NodePending, node.Phase)
			}
		}
		assert.True(t, found)
		if closer != nil {
			closer()
		}
	}
}

var artifactResolutionWhenSkippedDAG = `
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: conditional-artifact-passing-
spec:
  entrypoint: artifact-example
  templates:
  - name: artifact-example
    dag:
      tasks:
      - name: generate-artifact
        template: whalesay
        when: "false"
      - name: consume-artifact
        dependencies: [generate-artifact]
        template: print-message
        when: "false"
        arguments:
          artifacts:
          - name: message
            from: "{{tasks.generate-artifact.outputs.artifacts.hello-art}}"
      - name: sequence-param
        template: print-message
        dependencies: [generate-artifact]
        when: "false"
        arguments:
          artifacts:
          - name: message
            from: "{{tasks.generate-artifact.outputs.artifacts.hello-art}}"
        withSequence:
          count: "5"

  - name: whalesay
    container:
      image: docker/whalesay:latest
      command: [sh, -c]
      args: ["sleep 1; cowsay hello world | tee /tmp/hello_world.txt"]
    outputs:
      artifacts:
      - name: hello-art
        path: /tmp/hello_world.txt

  - name: print-message
    inputs:
      artifacts:
      - name: message
        path: /tmp/message
    container:
      image: alpine:3.23
      command: [sh, -c]
      args: ["cat /tmp/message"]

`

// Tests ability to reference workflow parameters from within top level spec fields (e.g. spec.volumes)
func TestArtifactResolutionWhenSkippedDAG(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	cancel, controller := newController(ctx)
	defer cancel()
	wfcset := controller.wfclientset.ArgoprojV1alpha1().Workflows("")

	wf := wfv1.MustUnmarshalWorkflow(artifactResolutionWhenSkippedDAG)
	wf, err := wfcset.Create(ctx, wf, metav1.CreateOptions{})
	require.NoError(t, err)
	woc := newWorkflowOperationCtx(ctx, wf, controller)

	woc.operate(ctx)

	woc = newWorkflowOperationCtx(ctx, woc.wf, controller)
	woc.operate(ctx)
	assert.Equal(t, wfv1.WorkflowSucceeded, woc.wf.Status.Phase)
}

func TestExpandTaskWithParam(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	task := wfv1.DAGTask{
		Name:     "fanout-param",
		Template: "tmpl",
		Arguments: wfv1.Arguments{
			Parameters: []wfv1.Parameter{{
				Name:  "msg",
				Value: wfv1.AnyStringPtr("{{item}}"),
			}},
		},
		WithParam: `[1234, "foo\tbar", true, []]`,
	}
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "test-wf"},
	}
	woc := newWoc(ctx, *wf)
	tmpl := &wfv1.Template{
		DAG: &wfv1.DAGTemplate{
			Tasks: []wfv1.DAGTask{task},
		},
	}
	evaluator := dag.NewDAGEvaluator(wf, tmpl, "test", "test")

	expanded, err := evaluator.ExpandTask(ctx, task, map[string]string{}, woc)
	require.NoError(t, err)
	require.Len(t, expanded, 4)

	expectedExpandedTasks := []struct {
		Name      string
		Parameter string
	}{
		{
			Name:      "fanout-param(0:1234)",
			Parameter: "1234",
		},
		{
			Name:      "fanout-param(1:foo\tbar)",
			Parameter: "foo\tbar",
		},
		{
			Name:      "fanout-param(2:true)",
			Parameter: "true",
		},
		{
			Name:      "fanout-param(3:[])",
			Parameter: "[]",
		},
	}

	for i, expected := range expectedExpandedTasks {
		assert.Equal(t, expected.Name, expanded[i].Name)
		assert.Equal(t, "tmpl", expanded[i].Template)
		assert.Equal(t, expected.Parameter, expanded[i].Arguments.Parameters[0].Value.String())
	}
}

func TestEvaluateDependsLogic(t *testing.T) {
	testTasks := []wfv1.DAGTask{
		{
			Name: "A",
		},
		{
			Name:    "B",
			Depends: "A",
		},
		{
			Name:    "C", // This task should fail
			Depends: "A",
		},
		{
			Name:    "should-execute-1",
			Depends: "A && (C.Succeeded || C.Failed)",
		},
		{
			Name:    "should-execute-2",
			Depends: "B || C",
		},
		{
			Name:    "should-not-execute",
			Depends: "B && C",
		},
		{
			Name:    "should-execute-3",
			Depends: "should-execute-2 || should-not-execute",
		},
	}
	ctx := logging.TestContext(t.Context())
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "test-wf"},
		Status: wfv1.WorkflowStatus{
			Nodes: make(wfv1.Nodes),
		},
	}
	tmpl := &wfv1.Template{
		DAG: &wfv1.DAGTemplate{
			Tasks: testTasks,
		},
	}
	evaluator := dag.NewDAGEvaluator(wf, tmpl, "test", "test")

	// Task A is running
	nodeID := wf.NodeID("test.A")
	wf.Status.Nodes[nodeID] = wfv1.NodeStatus{Phase: wfv1.NodeRunning}

	// Task B should not proceed, task A is still running
	result := evaluator.EvaluateTask(ctx, "B")
	require.NoError(t, result.Error)
	assert.True(t, result.Suspended)
	assert.False(t, result.ShouldRun)

	// Task A succeeded
	wf.Status.Nodes[nodeID] = wfv1.NodeStatus{Phase: wfv1.NodeSucceeded}

	// Task B and C should proceed and execute
	result = evaluator.EvaluateTask(ctx, "B")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.True(t, result.ShouldRun)
	result = evaluator.EvaluateTask(ctx, "C")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.True(t, result.ShouldRun)
	// Other tasks should not
	result = evaluator.EvaluateTask(ctx, "should-execute-1")
	require.NoError(t, result.Error)
	assert.True(t, result.Suspended)
	assert.False(t, result.ShouldRun)

	// Tasks B succeeded, C failed
	wf.Status.Nodes[wf.NodeID("test.B")] = wfv1.NodeStatus{Phase: wfv1.NodeSucceeded}
	wf.Status.Nodes[wf.NodeID("test.C")] = wfv1.NodeStatus{Phase: wfv1.NodeFailed}

	// Tasks should-execute-1 and should-execute-2 should proceed and execute
	result = evaluator.EvaluateTask(ctx, "should-execute-1")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.True(t, result.ShouldRun)
	result = evaluator.EvaluateTask(ctx, "should-execute-2")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.True(t, result.ShouldRun)
	// Task should-not-execute should proceed, but not execute
	result = evaluator.EvaluateTask(ctx, "should-not-execute")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.False(t, result.ShouldRun)

	// Tasks should-execute-1 and should-execute-2 succeeded, should-not-execute skipped
	wf.Status.Nodes[wf.NodeID("test.should-execute-1")] = wfv1.NodeStatus{Phase: wfv1.NodeSucceeded}
	wf.Status.Nodes[wf.NodeID("test.should-execute-2")] = wfv1.NodeStatus{Phase: wfv1.NodeSucceeded}
	wf.Status.Nodes[wf.NodeID("test.should-not-execute")] = wfv1.NodeStatus{Phase: wfv1.NodeSkipped}

	// Tasks should-execute-3 should proceed and execute
	result = evaluator.EvaluateTask(ctx, "should-execute-3")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.True(t, result.ShouldRun)
}

func TestEvaluateAnyAllDependsLogic(t *testing.T) {
	testTasks := []wfv1.DAGTask{
		{
			Name: "A",
		},
		{
			Name: "A-1",
		},
		{
			Name: "A-2",
		},
		{
			Name:    "B",
			Depends: "A.AnySucceeded",
		},
		{
			Name: "B-1",
		},
		{
			Name: "B-2",
		},
		{
			Name:    "C",
			Depends: "B.AllFailed",
		},
	}

	ctx := logging.TestContext(t.Context())
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "test-wf"},
		Status: wfv1.WorkflowStatus{
			Nodes: make(wfv1.Nodes),
		},
	}
	tmpl := &wfv1.Template{
		DAG: &wfv1.DAGTemplate{
			Tasks: testTasks,
		},
	}
	evaluator := dag.NewDAGEvaluator(wf, tmpl, "test", "test")

	// Task A is still running, A-1 succeeded but A-2 failed
	wf.Status.Nodes[wf.NodeID("test.A")] = wfv1.NodeStatus{
		Phase:    wfv1.NodeRunning,
		Type:     wfv1.NodeTypeTaskGroup,
		Children: []string{wf.NodeID("test.A-1"), wf.NodeID("test.A-2")},
	}
	wf.Status.Nodes[wf.NodeID("test.A-1")] = wfv1.NodeStatus{Phase: wfv1.NodeRunning}
	wf.Status.Nodes[wf.NodeID("test.A-2")] = wfv1.NodeStatus{Phase: wfv1.NodeRunning}

	// Task B should not proceed as task A is still running
	result := evaluator.EvaluateTask(ctx, "B")
	require.NoError(t, result.Error)
	assert.True(t, result.Suspended)
	assert.False(t, result.ShouldRun)

	// Task A succeeded
	wf.Status.Nodes[wf.NodeID("test.A")] = wfv1.NodeStatus{
		Phase:    wfv1.NodeSucceeded,
		Type:     wfv1.NodeTypeTaskGroup,
		Children: []string{wf.NodeID("test.A-1"), wf.NodeID("test.A-2")},
	}

	// Task B should proceed, but not execute as none of the children have succeeded yet
	result = evaluator.EvaluateTask(ctx, "B")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.False(t, result.ShouldRun)

	// Task A-2 succeeded
	wf.Status.Nodes[wf.NodeID("test.A-2")] = wfv1.NodeStatus{Phase: wfv1.NodeSucceeded}

	// Task B should now proceed and execute
	result = evaluator.EvaluateTask(ctx, "B")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.True(t, result.ShouldRun)

	// Task B succeeds and B-1 fails
	wf.Status.Nodes[wf.NodeID("test.B")] = wfv1.NodeStatus{
		Phase:    wfv1.NodeSucceeded,
		Type:     wfv1.NodeTypeTaskGroup,
		Children: []string{wf.NodeID("test.B-1"), wf.NodeID("test.B-2")},
	}
	wf.Status.Nodes[wf.NodeID("test.B-1")] = wfv1.NodeStatus{Phase: wfv1.NodeFailed}

	// Task C should proceed, but not execute as not all of B's children have failed yet
	result = evaluator.EvaluateTask(ctx, "C")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.False(t, result.ShouldRun)

	wf.Status.Nodes[wf.NodeID("test.B-2")] = wfv1.NodeStatus{Phase: wfv1.NodeFailed}

	// Task C should now proceed and execute as all of B's children have failed
	result = evaluator.EvaluateTask(ctx, "C")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.True(t, result.ShouldRun)
}

func TestEvaluateDependsLogicWhenDaemonFailed(t *testing.T) {
	testTasks := []wfv1.DAGTask{
		{
			Name: "A",
		},
		{
			Name:    "B",
			Depends: "A",
		},
	}

	ctx := logging.TestContext(t.Context())
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "test-wf"},
		Status: wfv1.WorkflowStatus{
			Nodes: make(wfv1.Nodes),
		},
	}
	tmpl := &wfv1.Template{
		DAG: &wfv1.DAGTemplate{
			Tasks: testTasks,
		},
	}
	evaluator := dag.NewDAGEvaluator(wf, tmpl, "test", "test")

	// Task A is running
	daemon := true
	wf.Status.Nodes[wf.NodeID("test.A")] = wfv1.NodeStatus{Phase: wfv1.NodeRunning, Daemoned: &daemon}

	// Task B should proceed and execute
	result := evaluator.EvaluateTask(ctx, "B")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.True(t, result.ShouldRun)

	// Task B running
	wf.Status.Nodes[wf.NodeID("test.B")] = wfv1.NodeStatus{Phase: wfv1.NodeRunning}

	// Task A failed or error
	wf.Status.Nodes[wf.NodeID("test.A")] = wfv1.NodeStatus{Phase: wfv1.NodeFailed, Daemoned: &daemon}

	// Task B should proceed and execute
	result = evaluator.EvaluateTask(ctx, "B")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.False(t, result.ShouldRun)
}

func TestEvaluateDependsLogicWhenTaskOmitted(t *testing.T) {
	testTasks := []wfv1.DAGTask{
		{
			Name: "A",
		},
		{
			Name:    "B",
			Depends: "A.Omitted",
		},
	}

	ctx := logging.TestContext(t.Context())
	wf := &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "test-wf"},
		Status: wfv1.WorkflowStatus{
			Nodes: make(wfv1.Nodes),
		},
	}
	tmpl := &wfv1.Template{
		DAG: &wfv1.DAGTemplate{
			Tasks: testTasks,
		},
	}
	evaluator := dag.NewDAGEvaluator(wf, tmpl, "test", "test")

	// Task A is running
	wf.Status.Nodes[wf.NodeID("test.A")] = wfv1.NodeStatus{Phase: wfv1.NodeOmitted}

	// Task B should proceed and execute
	result := evaluator.EvaluateTask(ctx, "B")
	require.NoError(t, result.Error)
	assert.False(t, result.Suspended)
	assert.True(t, result.ShouldRun)
}

func TestAllEvaluateDependsLogic(t *testing.T) {
	statusMap := map[common.TaskResult]wfv1.NodePhase{
		common.TaskResultSucceeded: wfv1.NodeSucceeded,
		common.TaskResultFailed:    wfv1.NodeFailed,
		common.TaskResultSkipped:   wfv1.NodeSkipped,
		common.TaskResultOmitted:   wfv1.NodeOmitted,
	}
	for _, status := range []common.TaskResult{common.TaskResultSucceeded, common.TaskResultFailed, common.TaskResultSkipped, common.TaskResultOmitted} {
		testTasks := []wfv1.DAGTask{
			{
				Name: "same",
			},
			{
				Name:    "Run",
				Depends: fmt.Sprintf("same.%s", status),
			},
			{
				Name:    "NotRun",
				Depends: fmt.Sprintf("!same.%s", status),
			},
		}

		ctx := logging.TestContext(t.Context())
		wf := &wfv1.Workflow{
			ObjectMeta: metav1.ObjectMeta{Name: "test-wf"},
			Status: wfv1.WorkflowStatus{
				Nodes: make(wfv1.Nodes),
			},
		}
		tmpl := &wfv1.Template{
			DAG: &wfv1.DAGTemplate{
				Tasks: testTasks,
			},
		}
		evaluator := dag.NewDAGEvaluator(wf, tmpl, "test", "test")

		// Task A is running
		wf.Status.Nodes[wf.NodeID("test.same")] = wfv1.NodeStatus{Phase: statusMap[status]}

		result := evaluator.EvaluateTask(ctx, "Run")
		require.NoError(t, result.Error)
		assert.False(t, result.Suspended)
		assert.True(t, result.ShouldRun)
		result = evaluator.EvaluateTask(ctx, "NotRun")
		require.NoError(t, result.Error)
		assert.False(t, result.Suspended)
		assert.False(t, result.ShouldRun)
	}
}

func TestHTTPTmplDAG(t *testing.T) {
	wf := wfv1.MustUnmarshalWorkflow("@testdata/http-tmpl-dag.yaml")
	ctx := logging.TestContext(t.Context())
	woc := newWoc(ctx, *wf)
	woc.operate(ctx)
	makePodsPhase(ctx, woc, v1.PodSucceeded)
	woc.operate(ctx)
	assert.Equal(t, wfv1.WorkflowSucceeded, woc.wf.Status.Phase)
}
