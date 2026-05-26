package controlplane_test

import (
	"net/http"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

func TestServerExposesCaseSuitePriorityBySignals(t *testing.T) {
	ctx, s := openCaseSuiteRouteStore(t)
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseSuiteRouteRuns(t, ctx, s, append(caseSuiteSignalRunHistory(base),
		caseSuiteRun("run.low.1", "case.low", store.StatusPassed, base.Add(3*time.Minute)),
	)...)

	server := serveCaseSuiteRouteBundle(t, caseSuiteSignalBundle([]profile.APICase{
		{ID: "case.impacted", DisplayName: "Impacted Case", NodeID: "node.create", CasePath: "cases/impacted.json", Tags: []string{"regression"}, Priority: "p1", Status: "active", SortOrder: 1},
		{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.search", CasePath: "cases/failed.json", Tags: []string{"regression"}, Priority: "p0", Status: "active", SortOrder: 2},
		{ID: "case.low", DisplayName: "Low Case", NodeID: "node.search", CasePath: "cases/low.json", Tags: []string{"regression"}, Priority: "p2", Status: "active", SortOrder: 3},
	}), s)

	payload := decodeJSONResponse(t, server.URL+"/api/case/suite-priority?signal=Create&tag=regression&status=active&limit=2&requestId=change-010&baseUrl=http://127.0.0.1:8080", http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("suite priority ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(3) || counts["selected"] != float64(2) || counts["skipped"] != float64(1) {
		t.Fatalf("suite priority counts = %#v", counts)
	}
	caseIDs := payload["caseIds"].([]any)
	if len(caseIDs) != 2 || caseIDs[0] != "case.impacted" || caseIDs[1] != "case.failed" {
		t.Fatalf("suite priority case ids = %#v", caseIDs)
	}
	selected := payload["selected"].([]any)
	first := selected[0].(map[string]any)
	if first["caseId"] != "case.impacted" || first["score"].(float64) <= 0 || len(first["reasons"].([]any)) == 0 {
		t.Fatalf("suite priority first = %#v", first)
	}
	batch := payload["batchRequest"].(map[string]any)
	if batch["requestId"] != "change-010" || batch["baseUrl"] != "http://127.0.0.1:8080" {
		t.Fatalf("suite priority batch = %#v", batch)
	}
}

func TestServerExposesCaseSuiteBriefForAgentTriage(t *testing.T) {
	ctx, s := openCaseSuiteRouteStore(t)
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseSuiteRouteRuns(t, ctx, s, caseSuiteSignalRunHistory(base)...)

	server := serveCaseSuiteRouteBundle(t, caseSuiteSignalBundle([]profile.APICase{
		{ID: "case.impacted", DisplayName: "Impacted Case", NodeID: "node.create", CasePath: "cases/impacted.json", Tags: []string{"regression"}, Priority: "p1", Status: "active", SortOrder: 1},
		{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.search", CasePath: "cases/failed.json", Tags: []string{"regression"}, Priority: "p0", Status: "active", SortOrder: 2},
		{ID: "case.blocked", DisplayName: "Blocked Case", NodeID: "node.search", Tags: []string{"regression"}, Priority: "p2", Status: "active", SortOrder: 3},
	}), s)

	payload := decodeJSONResponse(t, server.URL+"/api/case/suite-brief?signal=Create&tag=regression&status=active&limit=2&requestId=change-012&baseUrl=http://127.0.0.1:8080", http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("suite brief ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(3) || counts["ready"] != float64(2) || counts["blocked"] != float64(1) || counts["prioritySelected"] != float64(2) {
		t.Fatalf("suite brief counts = %#v", counts)
	}
	recommended := payload["recommended"].([]any)
	first := recommended[0].(map[string]any)
	if first["caseId"] != "case.impacted" || first["score"].(float64) <= 0 {
		t.Fatalf("suite brief first = %#v", first)
	}
	batch := payload["batchRequest"].(map[string]any)
	if batch["requestId"] != "change-012" || batch["baseUrl"] != "http://127.0.0.1:8080" {
		t.Fatalf("suite brief batch = %#v", batch)
	}
}

func caseSuiteSignalRunHistory(base time.Time) []caseSuiteRouteRun {
	return []caseSuiteRouteRun{
		caseSuiteRun("run.impacted.1", "case.impacted", store.StatusPassed, base),
		caseSuiteRun("run.impacted.2", "case.impacted", store.StatusFailed, base.Add(time.Minute)),
		caseSuiteRun("run.failed.1", "case.failed", store.StatusFailed, base.Add(2*time.Minute)),
	}
}
