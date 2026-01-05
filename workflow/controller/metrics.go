package controller

import (
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/workflow/common"
)

func (woc *wfOperationCtx) prepareDefaultMetricScope() (map[string]string, map[string]func() float64) {
	durationCPU := fmt.Sprintf("%s.%s", common.LocalVarResourcesDuration, v1.ResourceCPU)
	durationMem := fmt.Sprintf("%s.%s", common.LocalVarResourcesDuration, v1.ResourceMemory)

	localScope := woc.globalParams.DeepCopy()
	localScope[common.LocalVarDuration] = "0"
	localScope[common.LocalVarStatus] = string(wfv1.NodePending)
	localScope[durationCPU] = "0"
	localScope[durationMem] = "0"

	var realTimeScope = map[string]func() float64{
		common.GlobalVarWorkflowDuration: func() float64 {
			if woc.wf.Status.Phase.Completed() {
				return woc.wf.Status.FinishedAt.Time.Sub(woc.wf.Status.StartedAt.Time).Seconds()
			}
			return time.Since(woc.wf.Status.StartedAt.Time).Seconds()
		},
	}

	return localScope, realTimeScope
}

func (woc *wfOperationCtx) prepareMetricScope(node *wfv1.NodeStatus) (map[string]string, map[string]func() float64) {
	localScope, realTimeScope := woc.prepareDefaultMetricScope()
	if node.Fulfilled() {
		localScope[common.LocalVarDuration] = fmt.Sprintf("%f", node.FinishedAt.Sub(node.StartedAt.Time).Seconds())
		realTimeScope[common.LocalVarDuration] = func() float64 {
			return node.FinishedAt.Sub(node.StartedAt.Time).Seconds()
		}
	} else {
		localScope[common.LocalVarDuration] = fmt.Sprintf("%f", time.Since(node.StartedAt.Time).Seconds())
		realTimeScope[common.LocalVarDuration] = func() float64 {
			return time.Since(node.StartedAt.Time).Seconds()
		}
	}

	if len(node.Children) != 0 {
		localScope[common.LocalVarRetries] = strconv.Itoa(len(node.Children) - 1)
	}

	if node.Phase != "" {
		localScope[common.LocalVarStatus] = string(node.Phase)
	}

	if node.Inputs != nil {
		for _, param := range node.Inputs.Parameters {
			key := fmt.Sprintf("inputs.parameters.%s", param.Name)
			if param.Value == nil {
				localScope[key] = ""
			} else {
				localScope[key] = param.Value.String()
			}
		}
	}

	if node.Outputs != nil {
		if node.Outputs.Result != nil {
			localScope["outputs.result"] = *node.Outputs.Result
		}
		if node.Outputs.ExitCode != nil {
			localScope[common.LocalVarExitCode] = *node.Outputs.ExitCode
		}
		for _, param := range node.Outputs.Parameters {
			key := fmt.Sprintf("outputs.parameters.%s", param.Name)
			if param.Value == nil {
				localScope[key] = ""
			} else {
				localScope[key] = param.Value.String()
			}
		}
	}

	if node.ResourcesDuration != nil {
		for name, duration := range node.ResourcesDuration {
			localScope[fmt.Sprintf("%s.%s", common.LocalVarResourcesDuration, name)] = fmt.Sprint(duration.Duration().Seconds())
		}
	}

	return localScope, realTimeScope
}
