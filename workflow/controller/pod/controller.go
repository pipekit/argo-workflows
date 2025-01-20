// Package pod reconciles pods and takes care of gc events
package pod

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	//	wfclientset "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
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
)

type podEventCallback func(pod *apiv1.Pod) error

// Controller is a controller for pods
type Controller struct {
	config // Copy of controller config in here probably makes sense - for instanceId, GC delays, namespace instead of passing them in
	//	wfclientset      wfclientset.Interface
	kubeclientset    kubernetes.Interface
	wfInformer       cache.SharedIndexInformer
	wfInformerSynced cache.InformerSynced
	workqueue        workqueue.RateLimitingInterface
	clock            clock.WithTickerAndDelayedExecution
	//	metrics         *metrics.Metrics
	// podLister       corelisters.PodLister
	// podListerSynced cache.InformerSynced
	podInformer cache.SharedIndexInformer
	callBack    podEventCallback
}

func newWorkflowPodWatch(ctx context.Context, clientSet kubernetes.Interface, instanceID, namespace *string) *cache.ListWatch {
	c := clientSet.CoreV1().Pods(*namespace)
	// completed=false
	labelSelector := labels.NewSelector().
		Add(*workflowReq).
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
func NewController(ctx context.Context, instanceID *string, namespace string, clientSet kubernetes.Interface, wfInformer cache.SharedIndexInformer /* podInformer coreinformers.PodInformer,  */, metrics *metrics.Metrics, callback podEventCallback) *Controller {
	podController := &Controller{
		//		wfclientset:      wfClientSet,
		wfInformer:       wfInformer,
		wfInformerSynced: wfInformer.HasSynced,
		// workqueue:        workqueue.NewTypedDelayingQueueWithConfig(workqueue.TypedDelayingQueueConfig[string]{Name: "orphaned_pods_workflows"}),
		workqueue:   metrics.RateLimiterWithBusyWorkers(ctx, workqueue.DefaultControllerRateLimiter(), "pod_cleanup_queue"),
		podInformer: newInformer(ctx, clientSet, instanceID, &namespace),
		// podLister: podInformer.Lister(),
		// podListerSynced: podInformer.Informer().HasSynced,
	}
	podController.podInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod, err := podFromObj(obj)
				if err != nil {
					log.WithError(err).Error("object from informer wasn't a pod")
					return
				}
				err = callback(pod)
				if err != nil {
					log.WithError(err).Warn("could not enqueue workflow from pod label on add")
					return
				}
				podController.addPod(pod)
			},
			UpdateFunc: func(old, newVal interface{}) {
				key, err := cache.MetaNamespaceKeyFunc(newVal)
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
				err = callback(newPod)
				if err != nil {
					log.WithField("key", key).WithError(err).Warn("could not enqueue workflow from pod label on add")
					return
				}
				podController.updatePod(oldPod, newPod)
			},
			DeleteFunc: func(obj interface{}) {
				// IndexerInformer uses a delta queue, therefore for deletes we have to use this
				// key function.

				// Enqueue the workflow for deleted pod
				pod, err := podFromObj(obj)
				if err != nil {
					log.WithError(err).Error("object from informer wasn't a pod")
					return
				}
				err = callback(pod)
				if err != nil {
					log.WithError(err).Warn("could not enqueue workflow from pod label on delete")
					return
				}
				podController.deletePod(pod)
			},
		},
	)
	// podController.wfInformer.AddEventHandler(cache.FilteringResourceEventHandler{
	// 	FilterFunc: func(obj interface{}) bool {
	// 		un, ok := obj.(*unstructured.Unstructured)
	// 		return ok && common.IsDone(un)
	// 	},
	// 	Handler: cache.ResourceEventHandlerFuncs{}
	// 		DeleteFunc: func(obj interface{}) {
	// 		},
	// 	},
	// })

	// podInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
	// 	FilterFunc: func(obj interface{}) bool {
	// 		un, ok := obj.(*unstructured.Unstructured)
	// 		labels := un.GetLabels()
	// 		_ = labels
	// 		return ok
	// 	},
	// 	Handler: cache.ResourceEventHandlerFuncs{
	// 		AddFunc: func(obj interface{}) {
	// 			// call add self.addFunc
	// 		},
	// 		UpdateFunc: func(old, new interface{}) {
	// 			// call self.updateFunc
	// 		},
	// 		DeleteFunc: func(obj interface{}) {
	// 			// call self.deleteFunc
	// 		},
	// 	},
	// })
	return podController
}

// log something after calling this function maybe?
func startTerminating(old *v1.Pod, newPod *v1.Pod) bool {
	return old.DeletionTimestamp == nil && newPod.DeletionTimestamp != nil
}

func (c *Controller) addPod(pod *v1.Pod) {
}

func (c *Controller) updatePod(old *v1.Pod, newPod *v1.Pod) {
	// This is only called for actual updates, where there are "significant changes"
}

func (c *Controller) deletePod(pod *v1.Pod) {
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

func (c *Controller) GetPodPhaseMetrics() map[string]int64 {
	result := make(map[string]int64, 0)
	if c.podInformer != nil {
		for _, phase := range []apiv1.PodPhase{apiv1.PodRunning, apiv1.PodPending} {
			objs, err := c.podInformer.GetIndexer().IndexKeys(indexes.PodPhaseIndex, string(phase))
			if err != nil {
				log.WithError(err).Errorf("failed to  list pods in phase %s", phase)
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
