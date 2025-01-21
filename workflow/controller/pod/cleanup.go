package pod

import (
	"slices"
	"time"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-workflows/v3/workflow/common"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	log "github.com/sirupsen/logrus"
)

// // undo this and move back to main controller execWf
// func QueuePodsForCleanup(workflow, execwf) {
// 	log.Warnf("queuePodsForCleanup")

// 	delay := c.config.GetPodGCDeleteDelayDuration()
// 	podGC := execworkflow.Spec.PodGC
// 	podGCDelay, err := podGC.GetDeleteDelayDuration()
// 	if err != nil {
// 		log.WithError(err).Warn("failed to parse podGC.deleteDelayDuration")
// 	} else if podGCDelay >= 0 {
// 		delay = podGCDelay
// 	}
// 	strategy := podGC.GetStrategy()
// 	selector, _ := podGC.GetLabelSelector()
// 	workflowPhase := woc.wf.Status.Phase
// 	objs, _ := c.podInformer.GetIndexer().ByIndex(indexes.WorkflowIndex, woc.wf.Namespace+"/"+woc.wf.Name)
// 	for _, obj := range objs {
// 		pod := obj.(*apiv1.Pod)
// 		if _, ok := pod.Labels[common.LabelKeyComponent]; ok { // for these types we don't want to do PodGC
// 			continue
// 		}
// 		nodeID := woc.nodeID(pod)
// 		nodePhase, err := woc.wf.Status.Nodes.GetPhase(nodeID)
// 		if err != nil {
// 			woc.log.Errorf("pod cleanup: was unable to obtain node for %s", nodeID)
// 			continue
// 		}
// 		woc.log.Infof("pod cleanup: pod %s is fulfilled %t", pod.Name, nodePhase.Fulfilled())
// 		if !nodePhase.Fulfilled() {
// 			continue
// 		}
// 		PodCleanupAction(selector, pod, strategy, workflowPhase, delay)
// 	}
// }

func (c *Controller) EnactAnyPodCleanup(
	selector labels.Selector,
	pod *apiv1.Pod,
	strategy wfv1.PodGCStrategy,
	workflowPhase wfv1.WorkflowPhase,
	delay time.Duration,
) {
	action := determinePodCleanupAction(selector, pod.Labels, strategy, workflowPhase, pod.Status.Phase, pod.Finalizers)
	log.Infof("pod cleanup: pod %s is action %s", pod.Name, action)
	switch action {
	case deletePod:
		c.queuePodForCleanupAfter(pod.Namespace, pod.Name, action, delay)
	default:
		c.queuePodForCleanup(pod.Namespace, pod.Name, action)
	}

}

func determinePodCleanupAction(
	selector labels.Selector,
	podLabels map[string]string,
	strategy wfv1.PodGCStrategy,
	workflowPhase wfv1.WorkflowPhase,
	podPhase apiv1.PodPhase,
	finalizers []string,
) podCleanupAction {
	switch {
	case !selector.Matches(labels.Set(podLabels)): // if the pod will never be deleted, label it now
		return labelPodCompleted
	case strategy == wfv1.PodGCOnPodNone:
		return labelPodCompleted
	case strategy == wfv1.PodGCOnWorkflowCompletion && workflowPhase.Completed():
		return deletePod
	case strategy == wfv1.PodGCOnWorkflowSuccess && workflowPhase == wfv1.WorkflowSucceeded:
		return deletePod
	case strategy == wfv1.PodGCOnPodCompletion:
		return deletePod
	case strategy == wfv1.PodGCOnPodSuccess && podPhase == apiv1.PodSucceeded:
		return deletePod
	case strategy == wfv1.PodGCOnPodSuccess && podPhase == apiv1.PodFailed:
		return labelPodCompleted
	case workflowPhase.Completed():
		return labelPodCompleted
	case hasOurFinalizer(finalizers):
		return removeFinalizer
	}
	return ""
}

func hasOurFinalizer(finalizers []string) bool {
	if finalizers != nil {
		return slices.Contains(finalizers, common.FinalizerPodStatus)
	}
	return false
}
