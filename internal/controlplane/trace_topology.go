package controlplane

import (
	"fmt"
	"sort"
	"strings"
)

type traceData struct {
	Spans []traceSpan `json:"spans"`
}

type traceSpan struct {
	TraceID             string     `json:"traceId"`
	SegmentID           string     `json:"segmentId"`
	SpanID              int        `json:"spanId"`
	ParentSpanID        int        `json:"parentSpanId"`
	Refs                []traceRef `json:"refs"`
	ServiceCode         string     `json:"serviceCode"`
	ServiceInstanceName string     `json:"serviceInstanceName"`
	StartTime           int64      `json:"startTime"`
	EndTime             int64      `json:"endTime"`
	EndpointName        string     `json:"endpointName"`
	Type                string     `json:"type"`
	Peer                string     `json:"peer"`
	Component           string     `json:"component"`
	IsError             bool       `json:"isError"`
	Layer               string     `json:"layer"`
}

type traceRef struct {
	TraceID         string `json:"traceId"`
	ParentSegmentID string `json:"parentSegmentId"`
	ParentSpanID    int    `json:"parentSpanId"`
	Type            string `json:"type"`
}

type traceTopology struct {
	Status          string         `json:"status"`
	StepID          string         `json:"stepId,omitempty"`
	CaseID          string         `json:"caseId,omitempty"`
	RequestID       string         `json:"requestId,omitempty"`
	TraceID         string         `json:"traceId,omitempty"`
	SpanCount       int            `json:"spanCount"`
	ConfirmedEdges  []topologyEdge `json:"confirmedEdges"`
	ExternalExits   []topologyExit `json:"externalExits"`
	UnresolvedExits []topologyExit `json:"unresolvedExits"`
	ObservedNodes   []string       `json:"observedNodes"`
	Warnings        []string       `json:"warnings,omitempty"`
	TextTopology    string         `json:"textTopology"`
}

type topologyEdge struct {
	Source          string `json:"source"`
	Target          string `json:"target"`
	SourceComponent string `json:"sourceComponent,omitempty"`
	TargetComponent string `json:"targetComponent,omitempty"`
	SourceEndpoint  string `json:"sourceEndpoint,omitempty"`
	TargetEndpoint  string `json:"targetEndpoint,omitempty"`
}

type topologyExit struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Component string `json:"component,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	IsError   bool   `json:"isError,omitempty"`
}

func buildTraceTopology(stepID, caseID, requestID string, trace traceData) traceTopology {
	topology := traceTopology{
		Status:          "unavailable",
		StepID:          strings.TrimSpace(stepID),
		CaseID:          strings.TrimSpace(caseID),
		RequestID:       strings.TrimSpace(requestID),
		SpanCount:       len(trace.Spans),
		ConfirmedEdges:  []topologyEdge{},
		ExternalExits:   []topologyExit{},
		UnresolvedExits: []topologyExit{},
		ObservedNodes:   []string{},
	}
	if len(trace.Spans) == 0 {
		topology.Warnings = []string{"Trace provider returned no spans for this step."}
		topology.TextTopology = "Trace topology unavailable: no spans"
		return topology
	}
	topology.Status = "partial"
	topology.TraceID = firstTraceID(trace.Spans)
	topology.ObservedNodes = observedTraceNodes(trace.Spans)

	spanByKey := map[string]traceSpan{}
	for _, span := range trace.Spans {
		spanByKey[traceSpanKey(span.SegmentID, span.SpanID)] = span
	}
	childRefs := map[string]bool{}
	edgeSeen := map[string]bool{}
	for _, child := range trace.Spans {
		for _, ref := range child.Refs {
			parent, ok := spanByKey[traceSpanKey(ref.ParentSegmentID, ref.ParentSpanID)]
			if !ok || strings.TrimSpace(parent.ServiceCode) == "" || strings.TrimSpace(child.ServiceCode) == "" {
				continue
			}
			childRefs[traceSpanKey(ref.ParentSegmentID, ref.ParentSpanID)] = true
			edge := topologyEdge{
				Source:          parent.ServiceCode,
				Target:          child.ServiceCode,
				SourceComponent: parent.Component,
				TargetComponent: child.Component,
				SourceEndpoint:  parent.EndpointName,
				TargetEndpoint:  child.EndpointName,
			}
			key := strings.Join([]string{edge.Source, edge.Target, edge.SourceEndpoint, edge.TargetEndpoint}, "\x00")
			if edgeSeen[key] {
				continue
			}
			edgeSeen[key] = true
			topology.ConfirmedEdges = append(topology.ConfirmedEdges, edge)
		}
	}

	exitSeen := map[string]bool{}
	for _, span := range trace.Spans {
		if !strings.EqualFold(span.Type, "Exit") || strings.TrimSpace(span.Peer) == "" {
			continue
		}
		if childRefs[traceSpanKey(span.SegmentID, span.SpanID)] {
			continue
		}
		exit := topologyExit{
			Source:    span.ServiceCode,
			Target:    span.Peer,
			Component: span.Component,
			Endpoint:  span.EndpointName,
			IsError:   span.IsError,
		}
		key := strings.Join([]string{exit.Source, exit.Target, exit.Component, exit.Endpoint}, "\x00")
		if exitSeen[key] {
			continue
		}
		exitSeen[key] = true
		if isUnresolvedTraceExit(exit) {
			topology.UnresolvedExits = append(topology.UnresolvedExits, exit)
		} else {
			topology.ExternalExits = append(topology.ExternalExits, exit)
		}
	}
	sortTraceTopology(topology.ConfirmedEdges, topology.ExternalExits, topology.UnresolvedExits)
	if len(topology.ConfirmedEdges) > 0 && len(topology.UnresolvedExits) == 0 {
		topology.Status = "complete"
	}
	topology.TextTopology = renderTraceTextTopology(topology)
	return topology
}

func firstTraceID(spans []traceSpan) string {
	for _, span := range spans {
		if strings.TrimSpace(span.TraceID) != "" {
			return strings.TrimSpace(span.TraceID)
		}
		for _, ref := range span.Refs {
			if strings.TrimSpace(ref.TraceID) != "" {
				return strings.TrimSpace(ref.TraceID)
			}
		}
	}
	return ""
}

func observedTraceNodes(spans []traceSpan) []string {
	seen := map[string]bool{}
	nodes := []string{}
	for _, span := range spans {
		service := strings.TrimSpace(span.ServiceCode)
		if service == "" || seen[service] {
			continue
		}
		seen[service] = true
		nodes = append(nodes, service)
	}
	sort.Strings(nodes)
	return nodes
}

func traceSpanKey(segmentID string, spanID int) string {
	return fmt.Sprintf("%s#%d", segmentID, spanID)
}

func isUnresolvedTraceExit(exit topologyExit) bool {
	target := strings.TrimSpace(exit.Target)
	if target == "" {
		return false
	}
	if exit.IsError {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(exit.Component), "Dubbo") && strings.HasSuffix(target, ":0")
}

func sortTraceTopology(edges []topologyEdge, exitGroups ...[]topologyExit) {
	sort.Slice(edges, func(i, j int) bool {
		left := edges[i].Source + edges[i].Target + edges[i].SourceEndpoint + edges[i].TargetEndpoint
		right := edges[j].Source + edges[j].Target + edges[j].SourceEndpoint + edges[j].TargetEndpoint
		return left < right
	})
	for _, exits := range exitGroups {
		sort.Slice(exits, func(i, j int) bool {
			left := exits[i].Source + exits[i].Target + exits[i].Endpoint
			right := exits[j].Source + exits[j].Target + exits[j].Endpoint
			return left < right
		})
	}
}

func renderTraceTextTopology(topology traceTopology) string {
	lines := []string{}
	if topology.RequestID != "" || topology.TraceID != "" {
		parts := []string{}
		if topology.RequestID != "" {
			parts = append(parts, "requestId="+topology.RequestID)
		}
		if topology.TraceID != "" {
			parts = append(parts, "traceId="+topology.TraceID)
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	if len(topology.ConfirmedEdges) == 0 {
		lines = append(lines, "Observed nodes:")
		for _, node := range topology.ObservedNodes {
			lines = append(lines, "  - "+node)
		}
	} else {
		lines = append(lines, "Confirmed edges:")
		for _, edge := range topology.ConfirmedEdges {
			label := componentPair(edge.SourceComponent, edge.TargetComponent)
			if label != "" {
				label = " [" + label + "]"
			}
			lines = append(lines, fmt.Sprintf("  - %s -> %s%s", edge.Source, edge.Target, label))
		}
	}
	if len(topology.ExternalExits) > 0 {
		lines = append(lines, "Client-only external exits:")
		for _, exit := range topology.ExternalExits {
			lines = append(lines, renderTraceExitLine(exit))
		}
	}
	if len(topology.UnresolvedExits) > 0 {
		lines = append(lines, "Unresolved exits:")
		for _, exit := range topology.UnresolvedExits {
			lines = append(lines, renderTraceExitLine(exit))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderTraceExitLine(exit topologyExit) string {
	label := strings.TrimSpace(exit.Component)
	parts := []string{}
	if label != "" {
		parts = append(parts, label)
	}
	if exit.IsError {
		parts = append(parts, "error")
	}
	if len(parts) > 0 {
		label = " [" + strings.Join(parts, ", ") + "]"
	}
	return fmt.Sprintf("  - %s -> %s%s", exit.Source, exit.Target, label)
}

func componentPair(source, target string) string {
	source = strings.TrimSpace(source)
	target = strings.TrimSpace(target)
	if source == "" && target == "" {
		return ""
	}
	if source == "" {
		return target
	}
	if target == "" {
		return source
	}
	return source + " -> " + target
}
