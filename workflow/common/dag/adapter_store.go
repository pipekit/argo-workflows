package dag

import (
	"context"
	"fmt"
	"strings"
	"sync"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	genericdag "github.com/argoproj/argo-workflows/v3/pkg/dag"
)

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
	info map[genericdag.Key]any
	// states caches the task states (derived from node phases)
	states map[genericdag.Key]genericdag.TaskState
}

// NewWorkflowStore creates a new WorkflowStore from a workflow and DAG context.
func NewWorkflowStore(wf *wfv1.Workflow, boundaryID, boundaryName string) *WorkflowStore {
	return &WorkflowStore{
		Nodes:        wf.Status.Nodes,
		BoundaryID:   boundaryID,
		BoundaryName: boundaryName,
		Workflow:     wf,
		info:         make(map[genericdag.Key]any),
		states:       make(map[genericdag.Key]genericdag.TaskState),
	}
}

// taskNodeName computes the node name for a task (same as dagContext.taskNodeName).
func (s *WorkflowStore) taskNodeName(taskName string) string {
	if strings.HasPrefix(taskName, "[") {
		return fmt.Sprintf("%s%s", s.BoundaryName, taskName)
	}
	return fmt.Sprintf("%s.%s", s.BoundaryName, taskName)
}

// taskNodeID computes the node ID for a task (same as dagContext.taskNodeID).
func (s *WorkflowStore) taskNodeID(taskName string) string {
	nodeName := s.taskNodeName(taskName)
	return s.Workflow.NodeID(nodeName)
}

// GetValue retrieves the stored value for a task key.
// Returns the NodeValue and true if found, or empty NodeValue and false if not.
func (s *WorkflowStore) GetValue(_ context.Context, key genericdag.Key) (NodeValue, bool) {
	nodeID := s.taskNodeID(key)
	node, err := s.Nodes.Get(nodeID)
	if err != nil {
		return NodeValue{}, false
	}
	return NodeValue{NodeStatus: node}, true
}

// SetValue stores a value for a task key.
// In practice, this updates the node status in the workflow.
func (s *WorkflowStore) SetValue(ctx context.Context, key genericdag.Key, value NodeValue) {
	if value.NodeStatus == nil {
		return
	}
	nodeID := s.taskNodeID(key)
	s.Nodes.Set(ctx, nodeID, *value.NodeStatus)
}

// GetInfo retrieves build information for the entire store.
func (s *WorkflowStore) GetInfo(_ context.Context) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

// SetInfo stores build information for the entire store.
func (s *WorkflowStore) SetInfo(_ context.Context, info any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if infoMap, ok := info.(map[genericdag.Key]any); ok {
		s.info = infoMap
	}
}

// GetState returns the current execution state of a task.
// This maps Argo node phases to Build Systems TaskStates.
func (s *WorkflowStore) GetState(_ context.Context, key genericdag.Key) genericdag.TaskState {
	nodeID := s.taskNodeID(key)
	node, err := s.Nodes.Get(nodeID)
	if err != nil {
		return genericdag.TaskStatePending // Node doesn't exist, treat as pending
	}
	return PhaseToTaskState(node.Phase)
}

// SetState updates the execution state of a task.
// This is primarily used by the scheduler to track internal state;
// actual node phase updates happen through SetValue.
func (s *WorkflowStore) SetState(_ context.Context, key genericdag.Key, state genericdag.TaskState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[key] = state
}

// ListKeys returns all task keys in the store.
// This returns task names derived from node names in the DAG.
func (s *WorkflowStore) ListKeys(_ context.Context) []genericdag.Key {
	keys := make([]genericdag.Key, 0)
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
func PhaseToTaskState(phase wfv1.NodePhase) genericdag.TaskState {
	switch phase {
	case wfv1.NodePending:
		return genericdag.TaskStatePending
	case wfv1.NodeRunning:
		return genericdag.TaskStateRunning
	case wfv1.NodeSucceeded:
		return genericdag.TaskStateSucceeded
	case wfv1.NodeFailed:
		return genericdag.TaskStateFailed
	case wfv1.NodeError:
		return genericdag.TaskStateError
	case wfv1.NodeSkipped:
		return genericdag.TaskStateSkipped
	case wfv1.NodeOmitted:
		return genericdag.TaskStateOmitted
	default:
		return genericdag.TaskStatePending
	}
}

// TaskStateToPhase converts a Build Systems TaskState to an Argo NodePhase.
func TaskStateToPhase(state genericdag.TaskState) wfv1.NodePhase {
	switch state {
	case genericdag.TaskStatePending:
		return wfv1.NodePending
	case genericdag.TaskStateRunning:
		return wfv1.NodeRunning
	case genericdag.TaskStateSucceeded:
		return wfv1.NodeSucceeded
	case genericdag.TaskStateFailed:
		return wfv1.NodeFailed
	case genericdag.TaskStateError:
		return wfv1.NodeError
	case genericdag.TaskStateSkipped:
		return wfv1.NodeSkipped
	case genericdag.TaskStateOmitted:
		return wfv1.NodeOmitted
	default:
		return wfv1.NodePending
	}
}
