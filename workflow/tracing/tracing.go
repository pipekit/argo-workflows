package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/util/telemetry"
)

type nodeSpans struct {
	node       *trace.Span
	phase      *trace.Span
	phasePhase wfv1.NodePhase
	phaseMsg   string
}

type workflowSpans struct {
	workflow *trace.Span
	phase    *trace.Span
	nodes    map[string]nodeSpans
}

type Tracing struct {
	*telemetry.Tracing
	workflows map[string]workflowSpans
	// workflowPhaseSpans     map[string]*trace.Span
	// workflowNodeSpans      map[string]map[string]*trace.Span
	// workflowNodePhaseSpans map[string]map[string]*trace.Span
}

func New(ctx context.Context, serviceName string) (*Tracing, error) {
	tracing, err := telemetry.NewTracing(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	// err = m.Populate(ctx,
	// 	telemetry.AddVersion,
	// )
	// if err != nil {
	// 	return nil, err
	// }

	return &Tracing{
		Tracing:   tracing,
		workflows: make(map[string]workflowSpans),
		//		workflowPhaseSpans:     make(map[string]*trace.Span),
		//workflowNodeSpans:      make(map[string]map[string]*trace.Span),
		//workflowNodePhaseSpans: make(map[string]map[string]*trace.Span),
	}, nil

	// err = metrics.populate(ctx,
	// 	addIsLeader,
	// 	addPodPhaseGauge,
	// 	addPodPhaseCounter,
	// 	addPodMissingCounter,
	// 	addPodPendingCounter,
	// 	addWorkflowPhaseGauge,
	// 	addCronWfTriggerCounter,
	// 	addWorkflowPhaseCounter,
	// 	addWorkflowTemplateCounter,
	// 	addWorkflowTemplateHistogram,
	// 	addOperationDurationHistogram,
	// 	addErrorCounter,
	// 	addLogCounter,
	// 	addK8sRequests,
	// 	addWorkflowConditionGauge,
	// 	addWorkQueueMetrics,
	// )
	// if err != nil {
	// 	return nil, err
	// }

	// go metrics.customMetricsGC(ctx, config.TTL)

	// return tracing, nil
}

// type addMetric func(context.Context, *Metrics) error

// func (m *Metrics) populate(ctx context.Context, adders ...addMetric) error {
// 	for _, adder := range adders {
// 		if err := adder(ctx, m); err != nil {
// 			return err
// 		}
// 	}
// 	return nil
// }
