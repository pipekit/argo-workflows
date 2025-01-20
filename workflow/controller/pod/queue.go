// Package pod reconciles pods and takes care of gc events
package pod

import (
	"context"
	"os"
	"slices"
	"syscall"
	"time"

	errorsutil "github.com/argoproj/argo-workflows/v3/util/errors"
	"github.com/argoproj/argo-workflows/v3/workflow/common"
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	// //	wfclientset "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	// "github.com/argoproj/argo-workflows/v3/util/diff"
	// "github.com/argoproj/argo-workflows/v3/workflow/controller/indexes"
	// "github.com/argoproj/argo-workflows/v3/workflow/metrics"
	// "github.com/argoproj/argo-workflows/v3/workflow/util"
	// v1 "k8s.io/api/core/v1"
	// "k8s.io/apimachinery/pkg/selection"
	// "k8s.io/apimachinery/pkg/util/wait"
	// "k8s.io/client-go/kubernetes"
	// // coreinformers "k8s.io/client-go/informers/core/v1"
	// "k8s.io/apimachinery/pkg/runtime"
	// "k8s.io/apimachinery/pkg/watch"
	// // corelisters "k8s.io/client-go/listers/core/v1"
	// "k8s.io/apimachinery/pkg/labels"
	// "k8s.io/client-go/tools/cache"
	// "k8s.io/client-go/util/workqueue"
	// "k8s.io/utils/clock"
)

func (c *Controller) runPodCleanup(ctx context.Context) {
	for c.processNextPodCleanupItem(ctx) {
	}
}

func (c *Controller) getPodCleanupPatch(pod *apiv1.Pod, labelPodCompleted bool) ([]byte, error) {
	un := unstructured.Unstructured{}
	if labelPodCompleted {
		un.SetLabels(map[string]string{common.LabelKeyCompleted: "true"})
	}

	finalizerEnabled := os.Getenv(common.EnvVarPodStatusCaptureFinalizer) == "true"
	if finalizerEnabled && pod.Finalizers != nil {
		finalizers := slices.Clone(pod.Finalizers)
		finalizers = slices.DeleteFunc(finalizers,
			func(s string) bool { return s == common.FinalizerPodStatus })
		if len(finalizers) != len(pod.Finalizers) {
			un.SetFinalizers(finalizers)
			un.SetResourceVersion(pod.ObjectMeta.ResourceVersion)
		}
	}

	// if there was nothing to patch (no-op)
	if len(un.Object) == 0 {
		return nil, nil
	}

	return un.MarshalJSON()
}

func (c *Controller) patchPodForCleanup(ctx context.Context, pods typedv1.PodInterface, namespace, podName string, labelPodCompleted bool) error {
	pod, err := c.getPod(namespace, podName)
	// err is always nil in all kind of caches for now
	if err != nil {
		return err
	}
	// if pod is nil, it must have been deleted
	if pod == nil {
		return nil
	}

	patch, err := c.getPodCleanupPatch(pod, labelPodCompleted)
	if err != nil {
		return err
	}
	if patch == nil {
		return nil
	}

	_, err = pods.Patch(ctx, podName, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil && !apierr.IsNotFound(err) {
		return err
	}

	return nil
}

// all pods will ultimately be cleaned up by either deleting them, or labelling them
func (c *Controller) processNextPodCleanupItem(ctx context.Context) bool {
	key, quit := c.workqueue.Get()
	if quit {
		return false
	}

	defer func() {
		c.workqueue.Forget(key)
		c.workqueue.Done(key)
	}()

	namespace, podName, action := parsePodCleanupKey(key.(podCleanupKey))
	logCtx := log.WithFields(log.Fields{"key": key, "action": action})
	logCtx.Info("cleaning up pod")
	err := func() error {
		switch action {
		case terminateContainers:
			pod, err := c.getPod(namespace, podName)
			if err == nil && pod != nil && pod.Status.Phase == apiv1.PodPending {
				c.queuePodForCleanup(namespace, podName, deletePod)
			} else if terminationGracePeriod, err := c.SignalContainers(ctx, namespace, podName, syscall.SIGTERM); err != nil {
				return err
			} else if terminationGracePeriod > 0 {
				c.queuePodForCleanupAfter(namespace, podName, killContainers, terminationGracePeriod)
			}
		case killContainers:
			if _, err := c.SignalContainers(ctx, namespace, podName, syscall.SIGKILL); err != nil {
				return err
			}
		case labelPodCompleted:
			pods := c.kubeclientset.CoreV1().Pods(namespace)
			if err := c.patchPodForCleanup(ctx, pods, namespace, podName, true); err != nil {
				return err
			}
		case deletePod:
			pods := c.kubeclientset.CoreV1().Pods(namespace)
			if err := c.patchPodForCleanup(ctx, pods, namespace, podName, false); err != nil {
				return err
			}
			propagation := metav1.DeletePropagationBackground
			err := pods.Delete(ctx, podName, metav1.DeleteOptions{
				PropagationPolicy:  &propagation,
				GracePeriodSeconds: c.config.PodGCGracePeriodSeconds,
			})
			if err != nil && !apierr.IsNotFound(err) {
				return err
			}
		case removeFinalizer:
			pods := c.kubeclientset.CoreV1().Pods(namespace)
			if err := c.patchPodForCleanup(ctx, pods, namespace, podName, false); err != nil {
				return err
			}
		}
		return nil
	}()
	if err != nil {
		logCtx.WithError(err).Warn("failed to clean-up pod")
		if errorsutil.IsTransientErr(err) || apierr.IsConflict(err) {
			logCtx.WithError(err).Warn("failed to clean-up pod")
			c.workqueue.AddRateLimited(key)
		}
	}
	return true
}

func (c *Controller) queuePodForCleanup(namespace string, podName string, action podCleanupAction) {
	c.workqueue.AddRateLimited(newPodCleanupKey(namespace, podName, action))
}

func (c *Controller) queuePodForCleanupAfter(namespace string, podName string, action podCleanupAction, duration time.Duration) {
	c.workqueue.AddAfter(newPodCleanupKey(namespace, podName, action), duration)
}
