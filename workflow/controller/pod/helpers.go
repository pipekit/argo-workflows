// Package pod implements pod life cycle management
package pod

import (
	"context"
	"fmt"
	"syscall"
	"time"

	apiv1 "k8s.io/api/core/v1"
	//	wfclientset "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	// "github.com/argoproj/argo-workflows/v3/util/diff"
	// "github.com/argoproj/argo-workflows/v3/workflow/common"
	// "github.com/argoproj/argo-workflows/v3/workflow/controller/indexes"
	// "github.com/argoproj/argo-workflows/v3/workflow/metrics"
	// "github.com/argoproj/argo-workflows/v3/workflow/util"
	// v1 "k8s.io/api/core/v1"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	// "k8s.io/apimachinery/pkg/selection"
	// "k8s.io/apimachinery/pkg/util/wait"
	// "k8s.io/client-go/kubernetes"
	// // coreinformers "k8s.io/client-go/informers/core/v1"
	// "k8s.io/apimachinery/pkg/runtime"
	// "k8s.io/apimachinery/pkg/watch"
	// // corelisters "k8s.io/client-go/listers/core/v1"
	"github.com/argoproj/argo-workflows/v3/workflow/signal"
	// "k8s.io/apimachinery/pkg/labels"
	// "k8s.io/client-go/tools/cache"
	// "k8s.io/client-go/util/workqueue"
	// "k8s.io/utils/clock"
)

// signalContainers signals all containers of a pod
func (c *Controller) signalContainers(ctx context.Context, namespace string, podName string, sig syscall.Signal) (time.Duration, error) {
	pod, err := c.GetPod(namespace, podName)
	if pod == nil || err != nil {
		return 0, err
	}

	for _, container := range pod.Status.ContainerStatuses {
		if container.State.Running == nil {
			continue
		}
		// problems are already logged at info level, so we just ignore errors here
		_ = signal.SignalContainer(ctx, c.restConfig, pod, container.Name, sig)
	}
	if pod.Spec.TerminationGracePeriodSeconds == nil {
		return 30 * time.Second, nil
	}
	return time.Duration(*pod.Spec.TerminationGracePeriodSeconds) * time.Second, nil
}

func (c *Controller) GetPod(namespace string, podName string) (*apiv1.Pod, error) {
	obj, exists, err := c.podInformer.GetStore().GetByKey(namespace + "/" + podName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	pod, ok := obj.(*apiv1.Pod)
	if !ok {
		return nil, fmt.Errorf("object is not a pod")
	}
	return pod, nil
}

// TODO - return []*apiv1.Pod instead, save on duplicating this
func (c *Controller) GetPodsByIndex(index, key string) ([]interface{}, error) {
	return c.podInformer.GetIndexer().ByIndex(index, key)
}

func (c *Controller) TerminateContainers(namespace, name string) {
	c.queuePodForCleanup(namespace, name, terminateContainers)
}

func (c *Controller) DeletePod(namespace, name string) {
	c.queuePodForCleanup(namespace, name, deletePod)
}
