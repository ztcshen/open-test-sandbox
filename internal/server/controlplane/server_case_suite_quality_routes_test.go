package controlplane_test

import (
	"net/http"
	"testing"

	"agent-testbench/internal/domain/profile"
)

func TestServerExposesCaseSuiteQuality(t *testing.T) {
	server := serveCaseSuiteRouteBundle(t, caseSuiteQualityBundle(), nil)

	payload := decodeJSONResponse(t, server.URL+"/api/case/suite-quality?status=active", http.StatusOK)
	if payload["ok"] != false {
		t.Fatalf("suite quality ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["nodes"] != float64(2) || counts["nodesWithoutCases"] != float64(1) || counts["cases"] != float64(2) || counts["incompleteCases"] != float64(1) {
		t.Fatalf("suite quality counts = %#v", counts)
	}
	nodes := payload["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["nodeId"] != "node.empty" {
		t.Fatalf("suite quality nodes = %#v", nodes)
	}
	cases := payload["cases"].([]any)
	if len(cases) != 2 || cases[1].(map[string]any)["caseId"] != "case.gaps" {
		t.Fatalf("suite quality cases = %#v", cases)
	}
}

func TestServerExposesCaseSuiteQualityPlan(t *testing.T) {
	server := serveCaseSuiteRouteBundle(t, caseSuiteQualityBundle(), nil)

	payload := decodeJSONResponse(t, server.URL+"/api/case/suite-quality-plan?status=active", http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("suite quality plan ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(4) || counts["draftCase"] != float64(1) || counts["completeMetadata"] != float64(1) {
		t.Fatalf("suite quality plan counts = %#v", counts)
	}
	actions := payload["actions"].([]any)
	first := actions[0].(map[string]any)
	if first["type"] != "draft-case" || first["nodeId"] != "node.empty" || first["suggestedCaseId"] != "case.node-empty.default" {
		t.Fatalf("suite quality plan first = %#v", first)
	}
}

func caseSuiteQualityBundle() profile.Bundle {
	return profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
			{ID: "node.empty", DisplayName: "Node Empty"},
		},
		APICases: []profile.APICase{
			{ID: "case.complete", DisplayName: "Complete Case", Description: "Ready.", NodeID: "node.alpha", CasePath: "cases/complete.json", Tags: []string{"regression"}, Priority: "p0", Owner: "team-a", Status: "active"},
			{ID: "case.gaps", DisplayName: "Gap Case", NodeID: "node.alpha", Status: "active"},
		},
		TemplateConfigs: []profile.TemplateConfig{
			{ID: "cfg.case.complete", ScopeType: "case", ScopeID: "case.complete", Status: "active", ConfigJSON: `{"caseId":"case.complete","caseExecution":{"method":"GET","path":"/items"}}`},
		},
	}
}
