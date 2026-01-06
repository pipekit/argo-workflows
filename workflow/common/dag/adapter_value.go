package dag

import (
	"crypto/sha256"
	"encoding/hex"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	genericdag "github.com/argoproj/argo-workflows/v3/pkg/dag"
)

// NodeValue wraps an Argo node status as a Value in the Build Systems framework.
// This adapter allows workflow node outputs to be used with the generic Store and Scheduler.
type NodeValue struct {
	// NodeStatus is the underlying Argo workflow node status.
	NodeStatus *wfv1.NodeStatus
}

// Hash implements Value.Hash for NodeValue.
// The hash is computed from the node phase and outputs for change detection.
func (n NodeValue) Hash() genericdag.Hash {
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

	return genericdag.Hash(hex.EncodeToString(h.Sum(nil)))
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
