package tracing

import (
	"context"
	"errors"
	"strings"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

func debugTrace(traceType string, span *trace.Span) {
	log.WithField("trace", (*span).SpanContext().SpanID()).Info(traceType)
}

func (trc *Tracing) createWorkflow(id string) (workflowSpans, error) {
	if _, ok := trc.workflows[id]; ok {
		return trc.workflows[id], errors.New("found an existing trace for a starting workflow")
	}
	trc.workflows[id] = workflowSpans{
		nodes: make(map[string]nodeSpans),
	}
	return trc.workflows[id], nil
}

func (trc *Tracing) expectWorkflow(id string) (workflowSpans, error) {
	wf, ok := trc.workflows[id]
	if !ok {
		wf, _ := trc.createWorkflow(id)
		return wf, errors.New("no existing trace for a running workflow")
	}
	return wf, nil
}

func (wfs *workflowSpans) createNode(id string) (nodeSpans, error) {
	if _, ok := wfs.nodes[id]; ok {
		return wfs.nodes[id], errors.New("found an existing trace for a starting node")
	}
	return wfs.nodes[id], nil
}

func (wfs *workflowSpans) expectNode(id string) (nodeSpans, error) {
	node, ok := wfs.nodes[id]
	if !ok {
		node, _ := wfs.createNode(id)
		return node, errors.New("no existing trace for a running node")
	}
	return node, nil
}

func (trc *Tracing) updateWorkflow(id string, spans workflowSpans) {
	trc.workflows[id] = spans
}

func (wfs *workflowSpans) updateNode(id string, spans nodeSpans) {
	wfs.nodes[id] = spans
}

func (trc *Tracing) StartWorkflow(ctx context.Context, id string) context.Context {
	log.Info("Trace StartWorkflow")
	spans, err := trc.createWorkflow(id)
	if err != nil {
		log.WithField("workflow", id).Error(err)
		return ctx
	}
	var ts trace.TraceState

	if ts, err = ts.Insert("workflow", id); err != nil {
		log.Info("Trace StartWorkflow failed")
		return ctx
	}
	ctx = trace.ContextWithRemoteSpanContext(ctx, trace.SpanContext{}.WithTraceState(ts))
	ctx, span := trc.Tracer.Start(ctx, "workflow", trace.WithAttributes(attribute.String("workflow", string(id))), trace.WithSpanKind(trace.SpanKindConsumer))

	debugTrace("start", &span)
	spans.workflow = &span
	trc.updateWorkflow(id, spans)
	return ctx
}

func (trc *Tracing) ChangeWorkflowPhase(ctx context.Context, id string, phase wfv1.WorkflowPhase) context.Context {
	log.Info("Trace CWP")
	wf, err := trc.expectWorkflow(id)
	if err != nil {
		log.WithField("workflow", id).Error(err)
		return ctx
	}
	if wf.phase != nil {
		(*wf.phase).End()
		debugTrace("end wf phase", wf.phase)
	}
	ctx, newSpan := trc.Tracer.Start(ctx, "workflow-phase", trace.WithAttributes(attribute.String("phase", string(phase))))
	debugTrace("start", &newSpan)
	wf.phase = &newSpan
	trc.updateWorkflow(id, wf)
	return ctx
}

func (wfs *workflowSpans) endNodes() {
	for _, node := range wfs.nodes {
		node.endNode(wfv1.NodeSkipped)
	}
}

func (trc *Tracing) EndWorkflow(ctx context.Context, id string, phase wfv1.WorkflowPhase) context.Context {
	log.Info("Trace EndWorkflow")
	wf, err := trc.expectWorkflow(id)
	if err != nil {
		log.WithField("workflow", id).Error(err)
		return ctx
	}
	wf.endNodes()
	if wf.phase != nil {
		(*wf.phase).End()
		debugTrace("end wf phase", wf.phase)
	} else {
		log.Errorf("Unexpectedly didn't find a phase span for ending workflow %s", id)
	}
	if wf.workflow != nil {
		switch phase {
		case wfv1.WorkflowPending, wfv1.WorkflowRunning, wfv1.WorkflowUnknown:
			// Unexpected end
			(*wf.workflow).SetStatus(codes.Error, `Unexpected phase`)
		case wfv1.WorkflowSucceeded:
			(*wf.workflow).SetStatus(codes.Ok, ``)
		case wfv1.WorkflowFailed:
			(*wf.workflow).SetStatus(codes.Error, `Failed`)
		case wfv1.WorkflowError:
			(*wf.workflow).SetStatus(codes.Error, `Error`)
		}
		(*wf.workflow).End()
		debugTrace("end wf", wf.workflow)
		delete(trc.workflows, id)
	} else {
		log.Errorf("Unexpectedly didn't find a workflow span for ending workflow %s", id)
	}
	return ctx
}

func (trc *Tracing) RecoverWorkflowContext(ctx context.Context, id string) context.Context {
	log.Info("Trace RWC")
	if span, ok := trc.workflows[id]; ok {
		return trace.ContextWithSpan(ctx, *span.workflow)
	}
	return ctx
}

func (trc *Tracing) StartNode(ctx context.Context, wfId string, nodeId string, phase wfv1.NodePhase, message string) {
	log.Info("Trace Start Node")
	wf, err := trc.expectWorkflow(wfId)
	if err != nil {
		log.WithFields(log.Fields{"workflow": wfId, "node": nodeId}).Error(err)
		return
	}
	node, err := wf.createNode(nodeId)
	if err != nil {
		log.WithFields(log.Fields{"workflow": wfId, "node": nodeId}).Error(err)
		return
	}
	_, span := trc.Tracer.Start(ctx, "node", trace.WithAttributes(attribute.String("node", string(nodeId))), trace.WithSpanKind(trace.SpanKindProducer))
	debugTrace("start", &span)
	node.node = &span
	wf.updateNode(nodeId, node)
	trc.ChangeNodePhase(wfId, nodeId, phase, message)
}

func phaseMessage(phase wfv1.NodePhase, message string) string {
	switch phase {
	case wfv1.NodePending:
		splitReason := strings.Split(message, `:`)
		if splitReason[0] == "PodInitializing" {
			return ""
		}
		return splitReason[0]
	default:
		return ""
	}
}

func (trc *Tracing) ChangeNodePhase(wfId string, nodeId string, phase wfv1.NodePhase, message string) {
	log.Info("Trace CNP")
	wf, err := trc.expectWorkflow(wfId)
	if err != nil {
		log.WithFields(log.Fields{"workflow": wfId, "node": nodeId}).Error(err)
		return
	}
	node, err := wf.expectNode(nodeId)
	if err != nil {
		log.WithFields(log.Fields{"workflow": wfId, "node": nodeId}).Error(err)
		return
	}
	attribs := []attribute.KeyValue{attribute.String("node", string(nodeId)), attribute.String("phase", string(phase))}
	shortMsg := phaseMessage(phase, message)
	if shortMsg != "" {
		attribs = append(attribs, attribute.String("message", shortMsg))
	}
	log.Infof("Trace CNP message >%s< and short >%s<", message, shortMsg)
	if node.phasePhase == phase && node.phaseMsg == shortMsg {
		log.Info("Trace CNP no change")
		return
	}
	node.endPhase()
	ctx := trace.ContextWithSpan(context.Background(), *node.node)
	node.phasePhase = phase
	node.phaseMsg = shortMsg
	if phase.Fulfilled() {
		trc.EndNode(wfId, nodeId, phase)
	} else {
		_, span := trc.Tracer.Start(ctx, "node-phase", trace.WithAttributes(attribs...))
		debugTrace("start", &span)
		node.phase = &span
	}
	wf.updateNode(nodeId, node)
}

func (node *nodeSpans) endPhase() {
	if node.phase != nil {
		(*node.phase).End()
		debugTrace("end node phase", node.phase)
		node.phase = nil
	}
}

func (node *nodeSpans) endNode(phase wfv1.NodePhase) bool {
	node.endPhase()
	if node.node != nil {
		switch phase {
		case wfv1.NodePending, wfv1.NodeRunning, wfv1.NodeSkipped, wfv1.NodeOmitted:
			(*node.node).SetStatus(codes.Error, `Unexpected phase`)
		case wfv1.NodeSucceeded:
			(*node.node).SetStatus(codes.Ok, ``)
		case wfv1.NodeFailed:
			(*node.node).SetStatus(codes.Error, `Failed`)
		case wfv1.NodeError:
			(*node.node).SetStatus(codes.Error, `Error`)
		}
		(*node.node).End()
		debugTrace("end node", node.node)
		node.node = nil
		return true
	}
	return false
}

func (trc *Tracing) EndNode(wfId string, nodeId string, phase wfv1.NodePhase) {
	log.Info("Trace End Node")
	wf, err := trc.expectWorkflow(wfId)
	if err != nil {
		log.WithFields(log.Fields{"workflow": wfId, "node": nodeId}).Error(err)
		return
	}
	node, err := wf.expectNode(nodeId)
	if err != nil {
		log.WithFields(log.Fields{"workflow": wfId, "node": nodeId}).Error(err)
		return
	}
	if node.endNode(phase) {
		wf.updateNode(nodeId, node)
	}
	// May happen on controller restart
	//	log.WithFields(log.Fields{"workflow": wfId, "node": nodeId}).Error("Unexpectedly couldn't find a trace for node which is ending")
}
