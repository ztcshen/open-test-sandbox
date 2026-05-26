package controlplane_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
)

func TestServerExposesInterfaceNodeCoverage(t *testing.T) {
	server := httptest.NewServer(controlplane.New(interfaceNodeCoverageProfile()))
	defer server.Close()

	coverage := decodeJSONResponse(t, server.URL+"/api/interface-node/coverage?workflow=workflow.alpha", http.StatusOK)
	summary := coverage["summary"].(map[string]any)
	if summary["totalSteps"] != float64(2) || summary["mappedSteps"] != float64(1) || summary["unmappedSteps"] != float64(1) {
		t.Fatalf("coverage summary = %#v", summary)
	}
	rows := coverage["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("coverage rows = %#v", coverage)
	}
	mapped := rows[0].(map[string]any)
	if mapped["stepId"] != "step.alpha" || mapped["nodeId"] != "node.alpha" || mapped["href"] != "/interface-node.html?id=node.alpha" {
		t.Fatalf("mapped coverage row = %#v", mapped)
	}

	gaps := decodeJSONResponse(t, server.URL+"/api/interface-node/coverage-gaps?workflow=workflow.alpha", http.StatusOK)
	gapSummary := gaps["summary"].(map[string]any)
	if gapSummary["gapCount"] != float64(1) {
		t.Fatalf("coverage gaps = %#v", gaps)
	}
}

func interfaceNodeCoverageProfile() profile.Bundle {
	bundle := profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}
	bundle.Workflows = append(bundle.Workflows, profile.Workflow{ID: "workflow.alpha", DisplayName: "Workflow Alpha"})
	bundle.InterfaceNodes = append(bundle.InterfaceNodes, profile.InterfaceNode{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"})
	bundle.APICases = append(bundle.APICases,
		profile.APICase{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		profile.APICase{ID: "case.beta", DisplayName: "Case Beta"},
	)
	bundle.WorkflowBindings = append(bundle.WorkflowBindings,
		profile.WorkflowBinding{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
		profile.WorkflowBinding{WorkflowID: "workflow.alpha", StepID: "step.beta", CaseID: "case.beta", Required: true},
	)
	return bundle
}

func TestServerExposesReplayEvidenceContract(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/replay/evidence?traceId=TRACE-1", http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("replay evidence payload = %#v", payload)
	}
	run := payload["run"].(map[string]any)
	evidence := payload["evidence"].(map[string]any)
	if run["traceId"] != "TRACE-1" || evidence["traceId"] != "TRACE-1" {
		t.Fatalf("replay evidence trace = %#v", payload)
	}
	if evidence["systems"] == nil {
		t.Fatalf("replay evidence should expose systems array: %#v", payload)
	}
}

func TestServerExposesEmptyWorkbenchAuxiliaryAPIs(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	for _, item := range []struct {
		path string
		key  string
	}{
		{path: "/api/agent-test", key: "summary"},
		{path: "/api/case/runs", key: "caseRuns"},
		{path: "/api/case/timing", key: "summary"},
		{path: "/api/case/incomplete-batches", key: "items"},
	} {
		payload := decodeJSONResponse(t, server.URL+item.path, http.StatusOK)
		if payload["ok"] != true || payload[item.key] == nil {
			t.Fatalf("%s payload = %#v", item.path, payload)
		}
	}
}

func TestServerExposesIncompleteAPICasesFromStore(t *testing.T) {
	ctx := context.Background()
	s := openTestKitSQLiteStore(t, ctx, "sandbox.sqlite")
	recordCompletedAPICaseForIncompleteBatch(t, ctx, s)

	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", CasePath: "cases/case.alpha.json"},
			{ID: "case.beta", DisplayName: "Case Beta", CasePath: "cases/case.beta.json"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/incomplete-batches", http.StatusOK)
	if payload["ok"] != true || payload["count"] != float64(1) {
		t.Fatalf("incomplete cases payload = %#v", payload)
	}
	items := payload["items"].([]any)
	item := items[0].(map[string]any)
	if item["id"] != "case.beta" || item["reason"] != "not-run" || !strings.Contains(item["suggestedCommand"].(string), "cases/case.beta.json") {
		t.Fatalf("incomplete case item = %#v", item)
	}
}

func recordCompletedAPICaseForIncompleteBatch(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()

	run := store.Run{ID: "run.alpha"}
	run.ProfileID = "sample"
	run.Status = store.StatusPassed
	run.EvidenceRoot = ".runtime/evidence/run.alpha"
	run.SummaryJSON = "{}"
	if _, err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	caseRun := store.APICaseRun{ID: "run.alpha.case", RunID: run.ID}
	caseRun.CaseID = "case.alpha"
	caseRun.Status = store.StatusPassed
	caseRun.RequestSummaryJSON = `{"method":"POST","path":"/alpha"}`
	caseRun.AssertionSummaryJSON = `{"status":"passed","errorCount":0}`
	if _, err := s.RecordAPICaseRun(ctx, caseRun); err != nil {
		t.Fatalf("record api case run: %v", err)
	}
}
