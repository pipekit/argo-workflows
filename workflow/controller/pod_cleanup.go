package controller

import (
	"slices"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-workflows/v3/workflow/common"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/workflow/controller/indexes"
)

func (woc *wfOperationCtx) queuePodsForCleanup() {
	delay := woc.controller.Config.GetPodGCDeleteDelayDuration()
	podGC := woc.execWf.Spec.PodGC
	podGCDelay, err := podGC.GetDeleteDelayDuration()
	if err != nil {
		woc.log.WithError(err).Warn("failed to parse podGC.deleteDelayDuration")
	} else if podGCDelay >= 0 {
		delay = podGCDelay
	}
	strategy := podGC.GetStrategy()
	selector, _ := podGC.GetLabelSelector()
	workflowPhase := woc.wf.Status.Phase
	objs, _ := woc.controller.PodInformer./*GetIndexer().ByIndex(indexes.WorkflowIndex, woc.wf.Namespace+"/"+woc.wf.Name)*/ // Add a function to podInformer to get by index
	for _, obj := range objs {
		pod := obj.(*apiv1.Pod)
		if _, ok := pod.Labels[common.LabelKeyComponent]; ok { // for these types we don't want to do PodGC
			continue
		}
		nodeID := woc.nodeID(pod)
		nodePhase, err := woc.wf.Status.Nodes.GetPhase(nodeID)
		if err != nil {
			woc.log.Errorf("was unable to obtain node for %s", nodeID)
			continue
		}
		if !nodePhase.Fulfilled() {
			continue
		}
		woc.controller.PodController.EnactAnyPodCleanup(selector, pod, strategy, workflowPhase, delay)
	}
}
