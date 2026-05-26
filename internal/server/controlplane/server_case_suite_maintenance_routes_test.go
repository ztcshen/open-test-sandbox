package controlplane_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

func TestServerExposesCaseSuiteCoverageByMaintenanceFilters(t *testing.T) {
	ctx, s := openCaseSuiteRouteStore(t)
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseSuiteRouteRuns(t, ctx, s,
		caseSuiteWorkflowRun("run.default.old", "case.default", store.StatusFailed, base).withAssertionSummary(`{"status":"failed","errorCount":1}`),
		caseSuiteWorkflowRun("run.default.latest", "case.default", store.StatusPassed, base.Add(time.Minute)).withAssertionSummary(`{"status":"passed","errorCount":1}`),
		caseSuiteWorkflowRun("run.variant.latest", "case.variant", store.StatusFailed, base.Add(2*time.Minute)).withAssertionSummary(`{"status":"failed","errorCount":1}`),
	)

	bundle := caseSuiteAlphaBundle([]profile.APICase{
		{ID: "case.default", DisplayName: "Default Case", NodeID: "node.alpha", Tags: []string{"regression", "smoke"}, Priority: "p0", Owner: "team-a", SortOrder: 1},
		{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 2},
		{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p2", Owner: "team-b", SortOrder: 3},
		{ID: "case.other", DisplayName: "Other Case", NodeID: "node.alpha", Tags: []string{"smoke"}, Priority: "p2", Owner: "team-c", SortOrder: 4},
	})
	server := serveCaseSuiteRouteBundle(t, bundle, s)

	endpoint := server.URL + "/api/case/suite-coverage?tag=regression&status=active"
	payload := decodeJSONResponse(t, endpoint, http.StatusOK)
	if payload["ok"] != false {
		t.Fatalf("suite coverage ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(3) || counts["passed"] != float64(1) || counts["failed"] != float64(1) || counts["notRun"] != float64(1) {
		t.Fatalf("suite coverage counts = %#v", counts)
	}
	byCase := caseSuiteItemsByCase(t, payload)
	if len(byCase) != 3 {
		t.Fatalf("suite coverage items = %#v", payload["items"])
	}
	if byCase["case.default"]["latestStatus"] != store.StatusPassed || byCase["case.default"]["latestRunId"] != "run.default.latest" || byCase["case.default"]["hasPassed"] != true {
		t.Fatalf("default coverage = %#v", byCase["case.default"])
	}
	if byCase["case.variant"]["latestStatus"] != store.StatusFailed || byCase["case.variant"]["caseRunId"] != "run.variant.latest.case" || byCase["case.variant"]["detailUrl"] != "/api/case-run/evidence?caseRunId="+url.QueryEscape("run.variant.latest.case") {
		t.Fatalf("variant coverage = %#v", byCase["case.variant"])
	}
	if byCase["case.unrun"]["latestStatus"] != "not-run" || byCase["case.unrun"]["reason"] != "no run recorded in Store" {
		t.Fatalf("unrun coverage = %#v", byCase["case.unrun"])
	}
	if _, ok := byCase["case.other"]; ok {
		t.Fatalf("suite coverage should not include non-matching case: %#v", byCase["case.other"])
	}
}

func TestServerExposesCaseSuiteInspectionByMaintenanceFilters(t *testing.T) {
	s := caseSuiteStoreWithLatestMaintenanceRuns(t)
	bundle := caseSuiteAlphaBundle(
		[]profile.APICase{
			{ID: "case.default", DisplayName: "Default Case", NodeID: "node.alpha", CasePath: "cases/default.json", Tags: []string{"regression", "smoke"}, Priority: "p0", Owner: "team-a", SortOrder: 1},
			{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p2", Owner: "team-b", SortOrder: 3},
			{ID: "case.other", DisplayName: "Other Case", NodeID: "node.alpha", Tags: []string{"smoke"}, Priority: "p2", Owner: "team-c", SortOrder: 4},
		},
		profile.TemplateConfig{ID: "config.case.variant", ScopeType: "case", ScopeID: "case.variant", Status: "active", ConfigJSON: `{"caseId":"case.variant","caseExecution":{"method":"GET","nodeId":"node.alpha","path":"/alpha"}}`},
	)
	server := serveCaseSuiteRouteBundle(t, bundle, s)

	payload := decodeJSONResponse(t, server.URL+"/api/case/suite-inspection?tag=regression&status=active", http.StatusOK)
	if payload["ok"] != false {
		t.Fatalf("suite inspection ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(3) || counts["ready"] != float64(2) || counts["blocked"] != float64(1) || counts["failed"] != float64(1) || counts["notRun"] != float64(1) {
		t.Fatalf("suite inspection counts = %#v", counts)
	}
	byCase := caseSuiteItemsByCase(t, payload)
	if byCase["case.default"]["ready"] != true || byCase["case.default"]["hasRunnableFile"] != true || byCase["case.default"]["latestStatus"] != store.StatusPassed {
		t.Fatalf("default inspection = %#v", byCase["case.default"])
	}
	if byCase["case.variant"]["ready"] != true || byCase["case.variant"]["hasExecutionConfig"] != true || byCase["case.variant"]["suggestedAction"] != "rerun" {
		t.Fatalf("variant inspection = %#v", byCase["case.variant"])
	}
	if byCase["case.unrun"]["ready"] != false || byCase["case.unrun"]["suggestedAction"] != "add-runnable-source" {
		t.Fatalf("unrun inspection = %#v", byCase["case.unrun"])
	}
}

func TestServerExposesCaseSuitePlanByMaintenanceFilters(t *testing.T) {
	s := caseSuiteStoreWithLatestMaintenanceRuns(t)
	bundle := caseSuiteAlphaBundle([]profile.APICase{
		{ID: "case.default", DisplayName: "Default Case", NodeID: "node.alpha", CasePath: "cases/default.json", Tags: []string{"regression", "smoke"}, Priority: "p0", Owner: "team-a", SortOrder: 1},
		{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", CasePath: "cases/variant.json", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 2},
		{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p2", Owner: "team-b", SortOrder: 3},
	})
	server := serveCaseSuiteRouteBundle(t, bundle, s)

	endpoint := server.URL + "/api/case/suite-plan?tag=regression&status=active&action=run&action=rerun&requestId=change-001&baseUrl=http://127.0.0.1:8080&timeoutSeconds=9"
	payload := decodeJSONResponse(t, endpoint, http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("suite plan ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(3) || counts["selected"] != float64(1) || counts["blocked"] != float64(1) || counts["skipped"] != float64(1) {
		t.Fatalf("suite plan counts = %#v", counts)
	}
	batch := payload["batchRequest"].(map[string]any)
	caseIDs := batch["caseIds"].([]any)
	if len(caseIDs) != 1 || caseIDs[0] != "case.variant" || batch["requestId"] != "change-001" || batch["timeoutSeconds"] != float64(9) {
		t.Fatalf("suite plan batch request = %#v", batch)
	}
}

func TestServerExposesCaseSuiteStabilityByMaintenanceFilters(t *testing.T) {
	ctx, s := openCaseSuiteRouteStore(t)
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseSuiteRouteRuns(t, ctx, s,
		caseSuiteWorkflowRun("run.variant.1", "case.variant", store.StatusPassed, base),
		caseSuiteWorkflowRun("run.variant.2", "case.variant", store.StatusFailed, base.Add(time.Minute)),
		caseSuiteWorkflowRun("run.variant.3", "case.variant", store.StatusPassed, base.Add(2*time.Minute)),
		caseSuiteWorkflowRun("run.default.1", "case.default", store.StatusPassed, base.Add(3*time.Minute)),
		caseSuiteWorkflowRun("run.default.2", "case.default", store.StatusPassed, base.Add(4*time.Minute)),
	)

	bundle := caseSuiteAlphaBundle([]profile.APICase{
		{ID: "case.default", DisplayName: "Default Case", NodeID: "node.alpha", CasePath: "cases/default.json", Tags: []string{"regression"}, Priority: "p0", Owner: "team-a", SortOrder: 1},
		{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", CasePath: "cases/variant.json", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 2},
		{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p2", Owner: "team-b", SortOrder: 3},
	})
	server := serveCaseSuiteRouteBundle(t, bundle, s)

	payload := decodeJSONResponse(t, server.URL+"/api/case/suite-stability?tag=regression&status=active&limit=3", http.StatusOK)
	if payload["ok"] != false {
		t.Fatalf("suite stability ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(3) || counts["unstable"] != float64(1) || counts["stable"] != float64(1) || counts["notRun"] != float64(1) {
		t.Fatalf("suite stability counts = %#v", counts)
	}
	byCase := caseSuiteItemsByCase(t, payload)
	if byCase["case.variant"]["unstable"] != true || byCase["case.variant"]["transitions"] != float64(2) || byCase["case.variant"]["latestStatus"] != store.StatusPassed {
		t.Fatalf("variant stability = %#v", byCase["case.variant"])
	}
	recent := byCase["case.variant"]["recent"].([]any)
	if len(recent) != 3 || recent[0].(map[string]any)["runId"] != "run.variant.3" {
		t.Fatalf("variant recent = %#v", recent)
	}
	if byCase["case.unrun"]["latestStatus"] != "not-run" || byCase["case.unrun"]["reason"] != "no run recorded in Store" {
		t.Fatalf("unrun stability = %#v", byCase["case.unrun"])
	}
}

func caseSuiteStoreWithLatestMaintenanceRuns(t *testing.T) store.Store {
	t.Helper()

	ctx, s := openCaseSuiteRouteStore(t)
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseSuiteRouteRuns(t, ctx, s,
		caseSuiteWorkflowRun("run.default.latest", "case.default", store.StatusPassed, base),
		caseSuiteWorkflowRun("run.variant.latest", "case.variant", store.StatusFailed, base.Add(time.Minute)),
	)
	return s
}
