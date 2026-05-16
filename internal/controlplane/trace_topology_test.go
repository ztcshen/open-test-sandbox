package controlplane

import "testing"

func TestBuildTraceTopologyUsesSpanRefsForDirection(t *testing.T) {
	topology := buildTraceTopology("step.alpha", "case.alpha", "request.alpha", traceData{
		Spans: []traceSpan{
			{TraceID: "trace.alpha", SegmentID: "segment.entry", SpanID: 0, ServiceCode: "service.entry", EndpointName: "/entry", Component: "HttpClient"},
			{TraceID: "trace.alpha", SegmentID: "segment.worker", SpanID: 0, ServiceCode: "service.worker", EndpointName: "POST:/entry", Component: "Server", Refs: []traceRef{{TraceID: "trace.alpha", ParentSegmentID: "segment.entry", ParentSpanID: 0}}},
		},
	})

	if topology.Status != "complete" || topology.SpanCount != 2 {
		t.Fatalf("topology summary = %#v", topology)
	}
	if len(topology.ConfirmedEdges) != 1 {
		t.Fatalf("confirmed edges = %#v", topology.ConfirmedEdges)
	}
	edge := topology.ConfirmedEdges[0]
	if edge.Source != "service.entry" || edge.Target != "service.worker" {
		t.Fatalf("edge direction = %#v", edge)
	}
}

func TestBuildTraceTopologyDoesNotInventEdgesWithoutRefs(t *testing.T) {
	topology := buildTraceTopology("step.alpha", "case.alpha", "request.alpha", traceData{
		Spans: []traceSpan{
			{TraceID: "trace.alpha", SegmentID: "segment.entry", SpanID: 0, ServiceCode: "service.entry"},
			{TraceID: "trace.alpha", SegmentID: "segment.worker", SpanID: 0, ServiceCode: "service.worker"},
		},
	})

	if len(topology.ConfirmedEdges) != 0 {
		t.Fatalf("confirmed edges should only come from span refs: %#v", topology.ConfirmedEdges)
	}
	if topology.Status != "partial" {
		t.Fatalf("topology status = %q", topology.Status)
	}
}

func TestBuildTraceTopologyClassifiesErrorExitAsUnresolved(t *testing.T) {
	topology := buildTraceTopology("step.alpha", "case.alpha", "request.alpha", traceData{
		Spans: []traceSpan{
			{TraceID: "trace.alpha", SegmentID: "segment.entry", SpanID: 0, ServiceCode: "service.entry", EndpointName: "/entry", Component: "HttpClient"},
			{TraceID: "trace.alpha", SegmentID: "segment.worker", SpanID: 0, ServiceCode: "service.worker", EndpointName: "POST:/entry", Component: "Server", Refs: []traceRef{{TraceID: "trace.alpha", ParentSegmentID: "segment.entry", ParentSpanID: 0}}},
			{TraceID: "trace.alpha", SegmentID: "segment.worker", SpanID: 1, ParentSpanID: 0, ServiceCode: "service.worker", EndpointName: "call/downstream", Type: "Exit", Peer: "downstream:0", Component: "RPC", IsError: true},
		},
	})

	if topology.Status != "partial" {
		t.Fatalf("topology with unresolved exit should be partial: %#v", topology)
	}
	if len(topology.UnresolvedExits) != 1 || topology.UnresolvedExits[0].Target != "downstream:0" {
		t.Fatalf("unresolved exits = %#v", topology.UnresolvedExits)
	}
}
