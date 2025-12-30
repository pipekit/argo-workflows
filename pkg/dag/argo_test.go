// Package dag implements the Build Systems à la Carte framework for DAG dependency evaluation.
package dag

import (
	"context"
	"testing"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/util/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// testCtx returns a context with a test logger for Argo workflow operations.
func testCtx() context.Context {
	return logging.TestContext(context.Background())
}

// --- Helper functions for creating test workflows ---

func newTestWorkflow(name string) *wfv1.Workflow {
	return &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Status: wfv1.WorkflowStatus{
			Nodes: wfv1.Nodes{},
		},
	}
}

func addNodeToWorkflow(wf *wfv1.Workflow, name, boundaryID string, phase wfv1.NodePhase) *wfv1.NodeStatus {
	nodeID := wf.NodeID(name)
	node := wfv1.NodeStatus{
		ID:         nodeID,
		Name:       name,
		Phase:      phase,
		BoundaryID: boundaryID,
		Type:       wfv1.NodeTypePod,
	}
	wf.Status.Nodes.Set(testCtx(), nodeID, node)
	return &node
}

func createDAGTemplate(tasks []wfv1.DAGTask) *wfv1.Template {
	return &wfv1.Template{
		Name: "dag-template",
		DAG: &wfv1.DAGTemplate{
			Tasks: tasks,
		},
	}
}

// --- Tests for NodeValue ---

func TestNodeValue_Hash(t *testing.T) {
	t.Run("nil node returns empty hash", func(t *testing.T) {
		nv := NodeValue{NodeStatus: nil}
		assert.Equal(t, Hash(""), nv.Hash())
	})

	t.Run("hash includes phase", func(t *testing.T) {
		nv1 := NewNodeValue(&wfv1.NodeStatus{Phase: wfv1.NodeSucceeded})
		nv2 := NewNodeValue(&wfv1.NodeStatus{Phase: wfv1.NodeFailed})

		// Different phases should produce different hashes
		assert.NotEqual(t, nv1.Hash(), nv2.Hash())
	})

	t.Run("hash includes outputs", func(t *testing.T) {
		result1 := "result1"
		result2 := "result2"

		nv1 := NewNodeValue(&wfv1.NodeStatus{
			Phase: wfv1.NodeSucceeded,
			Outputs: &wfv1.Outputs{
				Result: &result1,
			},
		})

		nv2 := NewNodeValue(&wfv1.NodeStatus{
			Phase: wfv1.NodeSucceeded,
			Outputs: &wfv1.Outputs{
				Result: &result2,
			},
		})

		assert.NotEqual(t, nv1.Hash(), nv2.Hash())
	})

	t.Run("hash includes parameters", func(t *testing.T) {
		nv1 := NewNodeValue(&wfv1.NodeStatus{
			Phase: wfv1.NodeSucceeded,
			Outputs: &wfv1.Outputs{
				Parameters: []wfv1.Parameter{
					{Name: "param1", Value: wfv1.AnyStringPtr("value1")},
				},
			},
		})

		nv2 := NewNodeValue(&wfv1.NodeStatus{
			Phase: wfv1.NodeSucceeded,
			Outputs: &wfv1.Outputs{
				Parameters: []wfv1.Parameter{
					{Name: "param1", Value: wfv1.AnyStringPtr("value2")},
				},
			},
		})

		assert.NotEqual(t, nv1.Hash(), nv2.Hash())
	})

	t.Run("hash is deterministic", func(t *testing.T) {
		result := "test-result"
		node := &wfv1.NodeStatus{
			Phase: wfv1.NodeSucceeded,
			Outputs: &wfv1.Outputs{
				Result: &result,
				Parameters: []wfv1.Parameter{
					{Name: "param1", Value: wfv1.AnyStringPtr("value1")},
				},
			},
		}

		nv := NewNodeValue(node)
		hash1 := nv.Hash()
		hash2 := nv.Hash()

		assert.Equal(t, hash1, hash2)
	})
}

func TestNodeValue_Phase(t *testing.T) {
	t.Run("returns empty phase for nil node", func(t *testing.T) {
		nv := NodeValue{NodeStatus: nil}
		assert.Equal(t, wfv1.NodePhase(""), nv.Phase())
	})

	t.Run("returns correct phase", func(t *testing.T) {
		nv := NewNodeValue(&wfv1.NodeStatus{Phase: wfv1.NodeSucceeded})
		assert.Equal(t, wfv1.NodeSucceeded, nv.Phase())
	})
}

func TestNodeValue_StateChecks(t *testing.T) {
	testCases := []struct {
		name        string
		phase       wfv1.NodePhase
		isSucceeded bool
		isFailed    bool
		isError     bool
		isSkipped   bool
		isOmitted   bool
		isRunning   bool
		isPending   bool
		isFulfilled bool
	}{
		{"Succeeded", wfv1.NodeSucceeded, true, false, false, false, false, false, false, true},
		{"Failed", wfv1.NodeFailed, false, true, false, false, false, false, false, true},
		{"Error", wfv1.NodeError, false, false, true, false, false, false, false, true},
		{"Skipped", wfv1.NodeSkipped, false, false, false, true, false, false, false, true},
		{"Omitted", wfv1.NodeOmitted, false, false, false, false, true, false, false, true},
		{"Running", wfv1.NodeRunning, false, false, false, false, false, true, false, false},
		{"Pending", wfv1.NodePending, false, false, false, false, false, false, true, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nv := NewNodeValue(&wfv1.NodeStatus{Phase: tc.phase})

			assert.Equal(t, tc.isSucceeded, nv.IsSucceeded())
			assert.Equal(t, tc.isFailed, nv.IsFailed())
			assert.Equal(t, tc.isError, nv.IsError())
			assert.Equal(t, tc.isSkipped, nv.IsSkipped())
			assert.Equal(t, tc.isOmitted, nv.IsOmitted())
			assert.Equal(t, tc.isRunning, nv.IsRunning())
			assert.Equal(t, tc.isPending, nv.IsPending())
			assert.Equal(t, tc.isFulfilled, nv.IsFulfilled())
		})
	}

	t.Run("nil node is pending", func(t *testing.T) {
		nv := NodeValue{NodeStatus: nil}
		assert.True(t, nv.IsPending())
		assert.False(t, nv.IsSucceeded())
	})
}

// --- Tests for PhaseToTaskState and TaskStateToPhase ---

func TestPhaseToTaskState(t *testing.T) {
	testCases := []struct {
		phase wfv1.NodePhase
		state TaskState
	}{
		{wfv1.NodePending, TaskStatePending},
		{wfv1.NodeRunning, TaskStateRunning},
		{wfv1.NodeSucceeded, TaskStateSucceeded},
		{wfv1.NodeFailed, TaskStateFailed},
		{wfv1.NodeError, TaskStateError},
		{wfv1.NodeSkipped, TaskStateSkipped},
		{wfv1.NodeOmitted, TaskStateOmitted},
		{"unknown", TaskStatePending}, // Unknown phases default to pending
	}

	for _, tc := range testCases {
		t.Run(string(tc.phase), func(t *testing.T) {
			state := PhaseToTaskState(tc.phase)
			assert.Equal(t, tc.state, state)
		})
	}
}

func TestTaskStateToPhase(t *testing.T) {
	testCases := []struct {
		state TaskState
		phase wfv1.NodePhase
	}{
		{TaskStatePending, wfv1.NodePending},
		{TaskStateRunning, wfv1.NodeRunning},
		{TaskStateSucceeded, wfv1.NodeSucceeded},
		{TaskStateFailed, wfv1.NodeFailed},
		{TaskStateError, wfv1.NodeError},
		{TaskStateSkipped, wfv1.NodeSkipped},
		{TaskStateOmitted, wfv1.NodeOmitted},
		{TaskState(999), wfv1.NodePending}, // Unknown states default to pending
	}

	for _, tc := range testCases {
		t.Run(tc.state.String(), func(t *testing.T) {
			phase := TaskStateToPhase(tc.state)
			assert.Equal(t, tc.phase, phase)
		})
	}
}

// --- Tests for WorkflowStore ---

func TestWorkflowStore_NewWorkflowStore(t *testing.T) {
	t.Run("creates store with workflow context", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "boundary-id", "boundary-name")

		assert.NotNil(t, store)
		assert.Equal(t, "boundary-id", store.BoundaryID)
		assert.Equal(t, "boundary-name", store.BoundaryName)
	})
}

func TestWorkflowStore_GetSetValue(t *testing.T) {
	t.Run("get value from node added to workflow", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")

		// Add a node through the helper which uses testCtx()
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeSucceeded)

		// Retrieve via GetValue
		value, ok := store.GetValue("taskA")
		assert.True(t, ok)
		assert.Equal(t, wfv1.NodeSucceeded, value.Phase())
	})

	t.Run("get nonexistent node returns false", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")

		_, ok := store.GetValue("nonexistent")
		assert.False(t, ok)
	})
}

func TestWorkflowStore_GetState(t *testing.T) {
	t.Run("returns state from node phase", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")

		// Add node directly to workflow
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeSucceeded)

		state := store.GetState("taskA")
		assert.Equal(t, TaskStateSucceeded, state)
	})

	t.Run("returns pending for nonexistent node", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")

		state := store.GetState("nonexistent")
		assert.Equal(t, TaskStatePending, state)
	})
}

func TestWorkflowStore_ListKeys(t *testing.T) {
	t.Run("lists task names from nodes", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		boundaryID := wf.NodeID("dag")
		store := NewWorkflowStore(wf, boundaryID, "dag")

		// Add nodes with the boundary
		node1 := addNodeToWorkflow(wf, "dag.taskA", boundaryID, wfv1.NodeSucceeded)
		node2 := addNodeToWorkflow(wf, "dag.taskB", boundaryID, wfv1.NodeRunning)

		// Update nodes with correct boundary
		node1.BoundaryID = boundaryID
		node2.BoundaryID = boundaryID
		wf.Status.Nodes.Set(testCtx(), node1.ID, *node1)
		wf.Status.Nodes.Set(testCtx(), node2.ID, *node2)

		keys := store.ListKeys()

		assert.Contains(t, keys, Key("taskA"))
		assert.Contains(t, keys, Key("taskB"))
	})

	t.Run("filters out expanded task names", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		boundaryID := wf.NodeID("dag")
		store := NewWorkflowStore(wf, boundaryID, "dag")

		// Regular task
		node1 := addNodeToWorkflow(wf, "dag.taskA", boundaryID, wfv1.NodeSucceeded)
		node1.BoundaryID = boundaryID
		wf.Status.Nodes.Set(testCtx(), node1.ID, *node1)

		// Expanded task (contains parentheses)
		node2 := addNodeToWorkflow(wf, "dag.taskA(0)", boundaryID, wfv1.NodeSucceeded)
		node2.BoundaryID = boundaryID
		wf.Status.Nodes.Set(testCtx(), node2.ID, *node2)

		keys := store.ListKeys()

		assert.Contains(t, keys, Key("taskA"))
		// Should not contain expanded task
		for _, key := range keys {
			assert.NotContains(t, string(key), "(")
		}
	})
}

func TestWorkflowStore_GetNode(t *testing.T) {
	t.Run("returns node for task", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")

		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeSucceeded)

		node := store.GetNode("taskA")
		require.NotNil(t, node)
		assert.Equal(t, wfv1.NodeSucceeded, node.Phase)
	})

	t.Run("returns nil for nonexistent task", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")

		node := store.GetNode("nonexistent")
		assert.Nil(t, node)
	})
}

// --- Tests for WorkflowTasks ---

func TestWorkflowTasks_NewWorkflowTasks(t *testing.T) {
	t.Run("creates tasks adapter", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA"},
		}

		tasks := NewWorkflowTasks(dagTasks, store, wf)

		assert.NotNil(t, tasks)
		assert.Len(t, tasks.Tasks, 2)
	})
}

func TestWorkflowTasks_GetTask(t *testing.T) {
	t.Run("returns task for valid key", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskA"},
		}
		tasks := NewWorkflowTasks(dagTasks, store, wf)

		task, ok := tasks.GetTask("taskA")
		assert.True(t, ok)
		assert.Equal(t, Key("taskA"), task.Key)
	})

	t.Run("returns false for nonexistent key", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		tasks := NewWorkflowTasks(nil, store, wf)

		_, ok := tasks.GetTask("nonexistent")
		assert.False(t, ok)
	})
}

func TestWorkflowTasks_GetDependencies(t *testing.T) {
	t.Run("parses depends expression", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
			{Name: "taskC", Depends: "taskA && taskB"},
		}
		tasks := NewWorkflowTasks(dagTasks, store, wf)

		ctx := context.Background()
		deps, err := tasks.GetDependencies(ctx, "taskC")

		require.NoError(t, err)
		assert.Len(t, deps, 2)
		assert.Contains(t, deps, Key("taskA"))
		assert.Contains(t, deps, Key("taskB"))
	})

	t.Run("parses complex depends expression", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
			{Name: "taskC", Depends: "taskA.Succeeded && taskB.Failed"},
		}
		tasks := NewWorkflowTasks(dagTasks, store, wf)

		ctx := context.Background()
		deps, err := tasks.GetDependencies(ctx, "taskC")

		require.NoError(t, err)
		assert.Contains(t, deps, Key("taskA"))
		assert.Contains(t, deps, Key("taskB"))
	})

	t.Run("handles legacy dependencies field", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
			{Name: "taskC", Dependencies: []string{"taskA", "taskB"}},
		}
		tasks := NewWorkflowTasks(dagTasks, store, wf)

		ctx := context.Background()
		deps, err := tasks.GetDependencies(ctx, "taskC")

		require.NoError(t, err)
		assert.Len(t, deps, 2)
		assert.Contains(t, deps, Key("taskA"))
		assert.Contains(t, deps, Key("taskB"))
	})

	t.Run("returns empty for task with no dependencies", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskA"},
		}
		tasks := NewWorkflowTasks(dagTasks, store, wf)

		ctx := context.Background()
		deps, err := tasks.GetDependencies(ctx, "taskA")

		require.NoError(t, err)
		assert.Empty(t, deps)
	})
}

func TestWorkflowTasks_GetDependsLogic(t *testing.T) {
	t.Run("returns expanded depends expression", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA"},
		}
		tasks := NewWorkflowTasks(dagTasks, store, wf)

		ctx := context.Background()
		logic := tasks.GetDependsLogic(ctx, "taskB")

		// Should be expanded to include .Succeeded, .Skipped, .Daemoned
		assert.Contains(t, logic, "taskA.Succeeded")
	})

	t.Run("preserves explicit expressions", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA.Failed || taskA.Succeeded"},
		}
		tasks := NewWorkflowTasks(dagTasks, store, wf)

		ctx := context.Background()
		logic := tasks.GetDependsLogic(ctx, "taskB")

		assert.Contains(t, logic, "taskA.Failed")
		assert.Contains(t, logic, "taskA.Succeeded")
	})
}

func TestWorkflowTasks_TaskNames(t *testing.T) {
	t.Run("returns sorted task names", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskC"},
			{Name: "taskA"},
			{Name: "taskB"},
		}
		tasks := NewWorkflowTasks(dagTasks, store, wf)

		names := tasks.TaskNames()

		assert.Equal(t, []string{"taskA", "taskB", "taskC"}, names)
	})
}

func TestWorkflowTasks_GetDAGTask(t *testing.T) {
	t.Run("returns DAG task", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		dagTasks := []wfv1.DAGTask{
			{Name: "taskA", Template: "template-a"},
		}
		tasks := NewWorkflowTasks(dagTasks, store, wf)

		dagTask := tasks.GetDAGTask("taskA")
		require.NotNil(t, dagTask)
		assert.Equal(t, "template-a", dagTask.Template)
	})

	t.Run("returns nil for nonexistent task", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		store := NewWorkflowStore(wf, "", "dag")
		tasks := NewWorkflowTasks(nil, store, wf)

		dagTask := tasks.GetDAGTask("nonexistent")
		assert.Nil(t, dagTask)
	})
}

// --- Tests for DAGEvaluator ---

func TestDAGEvaluator_NewDAGEvaluator(t *testing.T) {
	t.Run("creates evaluator", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
		})

		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		assert.NotNil(t, evaluator)
		assert.NotNil(t, evaluator.Store)
		assert.NotNil(t, evaluator.Tasks)
		assert.NotNil(t, evaluator.Scheduler)
		assert.NotNil(t, evaluator.Rebuilder)
	})
}

func TestDAGEvaluator_EvaluateTask(t *testing.T) {
	t.Run("pending task with no dependencies should run", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		result := evaluator.EvaluateTask(ctx, "taskA")

		assert.Equal(t, "taskA", result.TaskName)
		assert.True(t, result.ShouldRun)
		assert.False(t, result.Suspended)
		assert.NoError(t, result.Error)
	})

	t.Run("succeeded task should not run", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeSucceeded)

		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		result := evaluator.EvaluateTask(ctx, "taskA")

		assert.False(t, result.ShouldRun)
	})

	t.Run("running task should not run again", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeRunning)

		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		result := evaluator.EvaluateTask(ctx, "taskA")

		assert.False(t, result.ShouldRun)
	})

	t.Run("task with unfulfilled dependencies is suspended", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		result := evaluator.EvaluateTask(ctx, "taskB")

		assert.False(t, result.ShouldRun)
		assert.True(t, result.Suspended)
		assert.Contains(t, result.WaitingOn, "taskA")
	})

	t.Run("task with fulfilled dependencies should run", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeSucceeded)

		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		result := evaluator.EvaluateTask(ctx, "taskB")

		assert.True(t, result.ShouldRun)
		assert.False(t, result.Suspended)
	})

	t.Run("task omitted when depends condition not met", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeFailed)

		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA.Succeeded"}, // taskA failed, not succeeded
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		result := evaluator.EvaluateTask(ctx, "taskB")

		assert.False(t, result.ShouldRun)
		assert.True(t, result.Skipped)
	})
}

func TestDAGEvaluator_EvaluateDAG(t *testing.T) {
	t.Run("evaluates multiple targets", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		results, err := evaluator.EvaluateDAG(ctx, []Key{"taskA", "taskB"})

		require.NoError(t, err)
		assert.Len(t, results, 2)
	})
}

func TestDAGEvaluator_DiamondDAG(t *testing.T) {
	t.Run("evaluates diamond DAG", func(t *testing.T) {
		//     A
		//    / \
		//   B   C
		//    \ /
		//     D
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "A"},
			{Name: "B", Depends: "A"},
			{Name: "C", Depends: "A"},
			{Name: "D", Depends: "B && C"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()

		// Initially, only A should be ready to run
		result := evaluator.EvaluateTask(ctx, "A")
		assert.True(t, result.ShouldRun)

		result = evaluator.EvaluateTask(ctx, "B")
		assert.True(t, result.Suspended)

		result = evaluator.EvaluateTask(ctx, "C")
		assert.True(t, result.Suspended)

		result = evaluator.EvaluateTask(ctx, "D")
		assert.True(t, result.Suspended)

		// After A succeeds
		addNodeToWorkflow(wf, "dag.A", "", wfv1.NodeSucceeded)
		evaluator = NewDAGEvaluator(wf, tmpl, "", "dag")

		result = evaluator.EvaluateTask(ctx, "B")
		assert.True(t, result.ShouldRun)

		result = evaluator.EvaluateTask(ctx, "C")
		assert.True(t, result.ShouldRun)

		result = evaluator.EvaluateTask(ctx, "D")
		assert.True(t, result.Suspended)

		// After B and C succeed
		addNodeToWorkflow(wf, "dag.B", "", wfv1.NodeSucceeded)
		addNodeToWorkflow(wf, "dag.C", "", wfv1.NodeSucceeded)
		evaluator = NewDAGEvaluator(wf, tmpl, "", "dag")

		result = evaluator.EvaluateTask(ctx, "D")
		assert.True(t, result.ShouldRun)
	})
}

func TestDAGEvaluator_FindLeafTaskNames(t *testing.T) {
	t.Run("finds leaf tasks", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA"},
			{Name: "taskC", Depends: "taskA"},
			{Name: "taskD", Depends: "taskB && taskC"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		leafTasks := evaluator.FindLeafTaskNames(ctx)

		assert.Len(t, leafTasks, 1)
		assert.Equal(t, "taskD", leafTasks[0])
	})

	t.Run("multiple leaf tasks", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA"},
			{Name: "taskC", Depends: "taskA"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		leafTasks := evaluator.FindLeafTaskNames(ctx)

		assert.Len(t, leafTasks, 2)
		assert.Contains(t, leafTasks, "taskB")
		assert.Contains(t, leafTasks, "taskC")
	})

	t.Run("all tasks are leaves when no dependencies", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
			{Name: "taskC"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		leafTasks := evaluator.FindLeafTaskNames(ctx)

		assert.Len(t, leafTasks, 3)
	})
}

func TestDAGEvaluator_GetTargetTasks(t *testing.T) {
	t.Run("returns explicit targets", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
			{Name: "taskC"},
		})
		tmpl.DAG.Target = "taskA taskB"
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		targets := evaluator.GetTargetTasks(ctx)

		assert.Equal(t, []string{"taskA", "taskB"}, targets)
	})

	t.Run("returns leaf tasks when no explicit targets", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		targets := evaluator.GetTargetTasks(ctx)

		assert.Equal(t, []string{"taskB"}, targets)
	})
}

func TestDAGEvaluator_EvaluateAll(t *testing.T) {
	t.Run("evaluates all tasks", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
			{Name: "taskC"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		results := evaluator.EvaluateAll(ctx)

		assert.Len(t, results, 3)
		assert.Contains(t, results, "taskA")
		assert.Contains(t, results, "taskB")
		assert.Contains(t, results, "taskC")
	})
}

func TestDAGEvaluator_GetReadyTasks(t *testing.T) {
	t.Run("returns tasks ready to execute", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeSucceeded)

		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA"},
			{Name: "taskC"}, // No dependencies
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		ready := evaluator.GetReadyTasks(ctx)

		// taskA is already done, taskB is ready (A succeeded), taskC is ready (no deps)
		assert.Contains(t, ready, "taskB")
		assert.Contains(t, ready, "taskC")
	})
}

func TestDAGEvaluator_GetWaitingTasks(t *testing.T) {
	t.Run("returns waiting tasks", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB", Depends: "taskA"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		waiting := evaluator.GetWaitingTasks(ctx)

		assert.Contains(t, waiting, "taskB")
		assert.Contains(t, waiting["taskB"], "taskA")
	})
}

// --- Tests for depends expression evaluation ---

func TestDAGEvaluator_ComplexDependsExpressions(t *testing.T) {
	t.Run("OR expression with one succeeded", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeSucceeded)
		addNodeToWorkflow(wf, "dag.taskB", "", wfv1.NodeFailed)

		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
			{Name: "taskC", Depends: "taskA.Succeeded || taskB.Succeeded"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		result := evaluator.EvaluateTask(ctx, "taskC")

		assert.True(t, result.ShouldRun)
	})

	t.Run("AND expression with both conditions met", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeSucceeded)
		addNodeToWorkflow(wf, "dag.taskB", "", wfv1.NodeFailed)

		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
			{Name: "taskC", Depends: "taskA.Succeeded && taskB.Failed"},
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		result := evaluator.EvaluateTask(ctx, "taskC")

		assert.True(t, result.ShouldRun)
	})

	t.Run("AND expression with one condition not met", func(t *testing.T) {
		wf := newTestWorkflow("test-wf")
		addNodeToWorkflow(wf, "dag.taskA", "", wfv1.NodeSucceeded)
		addNodeToWorkflow(wf, "dag.taskB", "", wfv1.NodeSucceeded)

		tmpl := createDAGTemplate([]wfv1.DAGTask{
			{Name: "taskA"},
			{Name: "taskB"},
			{Name: "taskC", Depends: "taskA.Succeeded && taskB.Failed"}, // B didn't fail
		})
		evaluator := NewDAGEvaluator(wf, tmpl, "", "dag")

		ctx := context.Background()
		result := evaluator.EvaluateTask(ctx, "taskC")

		assert.False(t, result.ShouldRun)
		assert.True(t, result.Skipped)
	})
}

// --- Tests for TaskResult ---

func TestTaskResult(t *testing.T) {
	t.Run("task result fields", func(t *testing.T) {
		tr := TaskResult{
			Succeeded:    true,
			Failed:       false,
			Errored:      false,
			Skipped:      false,
			Omitted:      false,
			Daemoned:     false,
			AnySucceeded: true,
			AllFailed:    false,
		}

		assert.True(t, tr.Succeeded)
		assert.False(t, tr.Failed)
		assert.True(t, tr.AnySucceeded)
	})
}
