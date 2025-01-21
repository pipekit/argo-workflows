// Package pod reconciles pods and takes care of gc events
package pod

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"

	//	wfclientset "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	argoConfig "github.com/argoproj/argo-workflows/v3/config"
	"github.com/argoproj/argo-workflows/v3/util/diff"
	"github.com/argoproj/argo-workflows/v3/workflow/common"
	"github.com/argoproj/argo-workflows/v3/workflow/controller/indexes"
	"github.com/argoproj/argo-workflows/v3/workflow/metrics"
	"github.com/argoproj/argo-workflows/v3/workflow/util"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	// coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	// corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
)

const (
	podResyncPeriod = 30 * time.Minute
)

var (
	incompleteReq, _ = labels.NewRequirement(common.LabelKeyCompleted, selection.Equals, []string{"false"})
	workflowReq, _   = labels.NewRequirement(common.LabelKeyWorkflow, selection.Exists, nil)
	keyFunc          = cache.DeletionHandlingMetaNamespaceKeyFunc
)

type podEventCallback func(pod *apiv1.Pod) error

// Controller is a controller for pods
type Controller struct {
	config           argoConfig.Controller
	kubeclientset    kubernetes.Interface
	wfInformer       cache.SharedIndexInformer
	wfInformerSynced cache.InformerSynced
	workqueue        workqueue.RateLimitingInterface
	clock            clock.WithTickerAndDelayedExecution
	podListerSynced  cache.InformerSynced
	podInformer      cache.SharedIndexInformer
	callBack         podEventCallback
	log              *logrus.Logger
	restConfig       *rest.Config
}

func newWorkflowPodWatch(ctx context.Context, clientSet kubernetes.Interface, instanceID, namespace *string) *cache.ListWatch {
	c := clientSet.CoreV1().Pods(*namespace)
	// completed=false
	labelSelector := labels.NewSelector().
		Add(*workflowReq).
		// not sure if we should do this
		Add(*incompleteReq).
		Add(util.InstanceIDRequirement(*instanceID))

	listFunc := func(options metav1.ListOptions) (runtime.Object, error) {
		options.LabelSelector = labelSelector.String()
		return c.List(ctx, options)
	}
	watchFunc := func(options metav1.ListOptions) (watch.Interface, error) {
		options.Watch = true
		options.LabelSelector = labelSelector.String()
		return c.Watch(ctx, options)
	}
	return &cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}

func newInformer(ctx context.Context, clientSet kubernetes.Interface, instanceID, namespace *string) cache.SharedIndexInformer {
	source := newWorkflowPodWatch(ctx, clientSet, instanceID, namespace)
	informer := cache.NewSharedIndexInformer(source, &apiv1.Pod{}, podResyncPeriod, cache.Indexers{
		indexes.WorkflowIndex: indexes.MetaWorkflowIndexFunc,
		indexes.NodeIDIndex:   indexes.MetaNodeIDIndexFunc,
		indexes.PodPhaseIndex: indexes.PodPhaseIndexFunc,
	})
	//nolint:errcheck // the error only happens if the informer was stopped, and it hasn't even started (https://github.com/kubernetes/client-go/blob/46588f2726fa3e25b1704d6418190f424f95a990/tools/cache/shared_informer.go#L580)
	return informer
}

// NewController creates a pod controller
func NewController(ctx context.Context, restConfig *rest.Config, instanceID *string, namespace string, clientSet kubernetes.Interface, wfInformer cache.SharedIndexInformer /* podInformer coreinformers.PodInformer,  */, metrics *metrics.Metrics, callback podEventCallback) *Controller {
	log := logrus.New()
	podController := &Controller{
		wfInformer:       wfInformer,
		wfInformerSynced: wfInformer.HasSynced,
		workqueue:        metrics.RateLimiterWithBusyWorkers(ctx, workqueue.DefaultControllerRateLimiter(), "pod_cleanup_queue"),
		podInformer:      newInformer(ctx, clientSet, instanceID, &namespace),
		log:              log,
		callBack:         callback,
		restConfig:       restConfig,
	}
	podController.podInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod, err := podFromObj(obj)
				if err != nil {
					log.WithError(err).Error("object from informer wasn't a pod")
					return
				}
				podController.addPod(pod)
			},
			UpdateFunc: func(old, newVal interface{}) {
				key, err := keyFunc(newVal)
				if err != nil {
					return
				}
				oldPod, newPod := old.(*apiv1.Pod), newVal.(*apiv1.Pod)
				if oldPod.ResourceVersion == newPod.ResourceVersion {
					return
				}
				if !significantPodChange(oldPod, newPod) {
					log.WithField("key", key).Info("insignificant pod change")
					diff.LogChanges(oldPod, newPod)
					return
				}
				podController.updatePod(oldPod, newPod)
			},
			DeleteFunc: func(obj interface{}) {
				// IndexerInformer uses a delta queue, therefore for deletes we have to use this
				// key function.
				podController.deletePod(obj)
			},
		},
	)
	return podController
}

// log something after calling this function maybe?
func startTerminating(old *v1.Pod, newPod *v1.Pod) bool {
	return old.DeletionTimestamp == nil && newPod.DeletionTimestamp != nil
}

func (c *Controller) addPod(pod *v1.Pod) {
	key, err := keyFunc(pod)
	if err != nil {
		return
	}
	err = c.callBack(pod)
	if err != nil {
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) updatePod(old *v1.Pod, newPod *v1.Pod) {
	// This is only called for actual updates, where there are "significant changes"
	key, err := keyFunc(newPod)
	if err != nil {
		return
	}
	if startTerminating(old, newPod) {
		c.log.Infof("termination event detected for pod %s", old.Name)
	}
	err = c.callBack(newPod)
	if err != nil {
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) deletePod(obj interface{}) {
	pod, ok := obj.(*apiv1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		pod, ok = tombstone.Obj.(*apiv1.Pod)
		if !ok {
			return
		}
	}
	key, err := keyFunc(pod)
	if err != nil {
		return
	}
	c.workqueue.Add(key)
}

// Run runs the pod controller
func (c *Controller) Run(ctx context.Context, workers int) {
	if !cache.WaitForCacheSync(ctx.Done(), c.podListerSynced, c.wfInformerSynced) {
		return
	}
	defer c.workqueue.ShutDown()
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runPodCleanup, time.Second)
	}
}

// GetPodPhaseMetrics obtains pod metrics
func (c *Controller) GetPodPhaseMetrics() map[string]int64 {
	result := make(map[string]int64, 0)
	if c.podInformer != nil {
		for _, phase := range []apiv1.PodPhase{apiv1.PodRunning, apiv1.PodPending} {
			objs, err := c.podInformer.GetIndexer().IndexKeys(indexes.PodPhaseIndex, string(phase))
			if err != nil {
				c.log.WithError(err).Errorf("failed to  list pods in phase %s", phase)
			} else {
				result[string(phase)] = int64(len(objs))
			}
		}
	}
	return result
}

func podFromObj(obj interface{}) (*apiv1.Pod, error) {
	pod, ok := obj.(*apiv1.Pod)
	if !ok {
		return nil, fmt.Errorf("Object is not a pod")
	}
	return pod, nil
}

// GetPod checks the informer cache to see if a pod exists
func (c *Controller) GetPod(key cache.ExplicitKey) (*apiv1.Pod, bool, error) {
	obj, exists, err := c.podInformer.GetStore().Get(key)
	if err != nil {
		return nil, exists, fmt.Errorf("failed to get pod from informer store: %w", err)
	}
	if exists {
		existing, ok := obj.(*apiv1.Pod)
		if ok {
			return existing, exists, nil
		}
		return nil, exists, errors.New("failed to convert object into pod")
	}
	return nil, false, nil
}
