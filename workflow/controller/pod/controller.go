// Package pod reconciles pods and takes care of gc events
package pod

import (
	"context"
	"time"

	wfclientset "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-workflows/v3/workflow/common"
	"github.com/argoproj/argo-workflows/v3/workflow/metrics"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
)

// PodController is a controller for pods
type PodController struct {
	wfclientset      wfclientset.Interface
	wfInformer       cache.SharedIndexInformer
	wfInformerSynced cache.InformerSynced
	workqueue        workqueue.TypedDelayingInterface[string]
	clock            clock.WithTickerAndDelayedExecution
	metrics          *metrics.Metrics
	podLister        corelisters.PodLister
	podListerSynced  cache.InformerSynced
}

// NewPodController creates a pod controller
// @param instanceID we need to care about how workflows is sharded, although  I doubt anyone is runnning argo this way.
func NewPodController(ctx context.Context, instanceID *string, wfClientSet wfclientset.Interface, wfInformer cache.SharedIndexInformer, podInformer coreinformers.PodInformer) *PodController {
	podController := &PodController{
		wfclientset:      wfClientSet,
		wfInformer:       wfInformer,
		wfInformerSynced: wfInformer.HasSynced,
		workqueue:        workqueue.NewTypedDelayingQueueWithConfig(workqueue.TypedDelayingQueueConfig[string]{Name: "orphaned_pods_workflows"}),
		podLister:        podInformer.Lister(),
		podListerSynced:  podInformer.Informer().HasSynced,
	}
	podController.wfInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			un, ok := obj.(*unstructured.Unstructured)
			return ok && common.IsDone(un)
		},
		Handler: cache.ResourceEventHandlerFuncs{
			DeleteFunc: func(obj interface{}) {
			},
		},
	})

	podInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			un, ok := obj.(*unstructured.Unstructured)
			labels := un.GetLabels()
			_ = labels
			return ok
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				// call add self.addFunc
			},
			UpdateFunc: func(old, new interface{}) {
				// call self.updateFunc
			},
			DeleteFunc: func(obj interface{}) {
				// call self.deleteFunc
			},
		},
	})
	return podController
}

// log something after calling this function maybe?
func startTerminating(old *v1.Pod, newPod *v1.Pod) bool {
	return old.DeletionTimestamp == nil && newPod.DeletionTimestamp != nil
}

func (p *PodController) addPod() {
}

func (p *PodController) updatePod(old *v1.Pod, newPod *v1.Pod) {
	if old.ResourceVersion == newPod.ResourceVersion {
		// Two different versions of the same pod will always have different RVs
		return
	}
}

func (p *PodController) deletePod() {
}

func (p *PodController) processNextWorkItem(ctx context.Context) bool {
	return false
}

func (p *PodController) worker(ctx context.Context) {
	for p.processNextWorkItem(ctx) {
	}
}

// Run runs the pod controller
func (p *PodController) Run(ctx context.Context, workers int) {
	if !cache.WaitForCacheSync(ctx.Done(), p.podListerSynced, p.wfInformerSynced) {
		return
	}
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, p.worker, time.Second)
	}
}
