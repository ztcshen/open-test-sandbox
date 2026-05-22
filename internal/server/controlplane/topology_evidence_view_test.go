package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
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
				TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.entry","service.worker"]}`,
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

func TestTopologyEvidenceViewForCaseIgnoresRowsWithoutSkyWalkingProvider(t *testing.T) {
	view := topologyEvidenceViewForCase(topologyEvidenceViewInput{
		RunID:  "run.alpha",
		CaseID: "case.alpha",
		Rows: []store.TraceTopology{
			{
				ID:            "topology.legacy",
				WorkflowRunID: "run.alpha",
				StepID:        "step.alpha",
				CaseID:        "case.alpha",
				RequestID:     "request.alpha",
				TraceID:       "trace.alpha",
				Status:        "complete",
				TopologyJSON:  `{"status":"complete","confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
				TextTopology:  "service.entry -> service.worker",
			},
		},
	})

	if view["status"] != "unavailable" {
		t.Fatalf("legacy topology row should not be trusted: %#v", view)
	}
}

func TestTopologyEvidenceViewForCaseIgnoresSavedSummaryWithoutSkyWalkingProvider(t *testing.T) {
	view := topologyEvidenceViewForCase(topologyEvidenceViewInput{
		RunID:  "run.alpha",
		CaseID: "case.alpha",
		SavedSummary: map[string]any{
			"topology": map[string]any{
				"status":         "complete",
				"confirmedEdges": []any{map[string]any{"source": "service.entry", "target": "service.worker"}},
			},
		},
	})

	if view["status"] != "unavailable" {
		t.Fatalf("saved summary without SkyWalking provider should not be trusted: %#v", view)
	}
}

func TestTopologyEvidenceViewForCaseAcceptsSavedSkyWalkingSummary(t *testing.T) {
	view := topologyEvidenceViewForCase(topologyEvidenceViewInput{
		RunID:  "run.alpha",
		CaseID: "case.alpha",
		SavedSummary: map[string]any{
			"traceTopology": map[string]any{
				"provider":       "skywalking",
				"status":         "complete",
				"traceId":        "trace.alpha",
				"confirmedEdges": []any{map[string]any{"source": "service.entry", "target": "service.worker"}},
			},
		},
	})

	if view["status"] != "complete" || view["traceId"] != "trace.alpha" {
		t.Fatalf("saved SkyWalking summary should be trusted: %#v", view)
	}
}

func TestWorkflowRunTraceTopologiesExposeOnlySkyWalkingRows(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: t.TempDir() + "/sandbox.sqlite"})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	now := time.Date(2026, 5, 18, 8, 0, 0, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.legacy",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.alpha",
		CaseID:        "case.alpha",
		RequestID:     "request.alpha",
		TraceID:       "trace.alpha",
		Status:        "complete",
		TopologyJSON:  `{"status":"complete","confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("save legacy topology: %v", err)
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.skywalking",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.alpha",
		CaseID:        "case.alpha",
		RequestID:     "request.beta",
		TraceID:       "trace.beta",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
		CreatedAt:     now.Add(time.Second),
	}); err != nil {
		t.Fatalf("save skywalking topology: %v", err)
	}

	rows, err := workflowRunTraceTopologies(ctx, s, "run.alpha", "step.alpha")
	if err != nil {
		t.Fatalf("workflow topologies: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "topology.skywalking" || rows[0]["provider"] != "skywalking" {
		t.Fatalf("workflow topology rows = %#v", rows)
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
	warnings := view["warnings"].([]string)
	if len(warnings) == 0 || !strings.Contains(warnings[0], "SkyWalking") {
		t.Fatalf("unavailable topology warning should name SkyWalking: %#v", warnings)
	}
}
