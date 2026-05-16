package controlplane

import (
	"testing"

	"open-test-sandbox/internal/store"
)

func TestTopologyEvidenceViewForCasePrefersStoredTraceRows(t *testing.T) {
	view := topologyEvidenceViewForCase(topologyEvidenceViewInput{
		RunID:  "run.alpha",
		CaseID: "case.alpha",
		Rows: []store.TraceTopology{
			{
				ID:            "topology.alpha",
				WorkflowRunID: "run.alpha",
				StepID:        "step.alpha",
				CaseID:        "case.alpha",
				RequestID:     "request.alpha",
				TraceID:       "trace.alpha",
				Status:        "complete",
				TopologyJSON:  `{"status":"complete","confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.entry","service.worker"]}`,
				TextTopology:  "service.entry -> service.worker",
			},
		},
		SavedSummary: map[string]any{
			"topology": map[string]any{"status": "stale"},
		},
	})

	if view["traceId"] != "trace.alpha" || view["requestId"] != "request.alpha" {
		t.Fatalf("topology identifiers = %#v", view)
	}
	edges := view["confirmedEdges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("confirmed edges = %#v", view)
	}
	if view["topologyJson"] != nil {
		t.Fatalf("topology view should not expose raw topology json: %#v", view)
	}
}

func TestTopologyEvidenceViewForCaseReturnsUnavailableView(t *testing.T) {
	view := topologyEvidenceViewForCase(topologyEvidenceViewInput{
		RunID:  "run.alpha",
		CaseID: "case.alpha",
	})

	if view["status"] != "unavailable" || view["runId"] != "run.alpha" || view["caseId"] != "case.alpha" {
		t.Fatalf("unavailable topology view = %#v", view)
	}
}
