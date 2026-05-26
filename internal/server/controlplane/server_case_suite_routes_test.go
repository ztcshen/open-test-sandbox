package controlplane_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesInterfaceNodeCoverage(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
			{ID: "case.beta", DisplayName: "Case Beta"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
			{WorkflowID: "workflow.alpha", StepID: "step.beta", CaseID: "case.beta", Required: true},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
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
		resp, err := http.Get(server.URL + item.path)
		if err != nil {
			t.Fatalf("get %s: %v", item.path, err)
		}
		var payload map[string]any
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", item.path, err)
		}
		if resp.StatusCode != http.StatusOK || payload["ok"] != true || payload[item.key] == nil {
			t.Fatalf("%s payload = %#v status=%d", item.path, payload, resp.StatusCode)
		}
	}
}

func TestServerExposesIncompleteAPICasesFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  "{}",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.alpha.case",
		RunID:                "run.alpha",
		CaseID:               "case.alpha",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"passed","errorCount":0}`,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}

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

func TestServerExposesCaseSuiteCoverageByMaintenanceFilters(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	for _, item := range []struct {
		runID  string
		caseID string
		status string
		at     time.Time
	}{
		{runID: "run.default.old", caseID: "case.default", status: store.StatusFailed, at: base},
		{runID: "run.default.latest", caseID: "case.default", status: store.StatusPassed, at: base.Add(time.Minute)},
		{runID: "run.variant.latest", caseID: "case.variant", status: store.StatusFailed, at: base.Add(2 * time.Minute)},
	} {
		_, err = s.CreateRun(ctx, store.Run{
			ID:         item.runID,
			ProfileID:  "sample",
			WorkflowID: item.caseID,
			Status:     item.status,
			StartedAt:  item.at,
			FinishedAt: item.at.Add(time.Second),
			CreatedAt:  item.at,
			UpdatedAt:  item.at.Add(time.Second),
		})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
			ID:                   item.runID + ".case",
			RunID:                item.runID,
			CaseID:               item.caseID,
			Status:               item.status,
			AssertionSummaryJSON: `{"status":"` + item.status + `","errorCount":1}`,
			StartedAt:            item.at,
			FinishedAt:           item.at.Add(time.Second),
			CreatedAt:            item.at,
		})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}

	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.default", DisplayName: "Default Case", NodeID: "node.alpha", Tags: []string{"regression", "smoke"}, Priority: "p0", Owner: "team-a", SortOrder: 1},
			{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p2", Owner: "team-b", SortOrder: 3},
			{ID: "case.other", DisplayName: "Other Case", NodeID: "node.alpha", Tags: []string{"smoke"}, Priority: "p2", Owner: "team-c", SortOrder: 4},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	endpoint := server.URL + "/api/case/suite-coverage?tag=regression&status=active"
	payload := decodeJSONResponse(t, endpoint, http.StatusOK)
	if payload["ok"] != false {
		t.Fatalf("suite coverage ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(3) || counts["passed"] != float64(1) || counts["failed"] != float64(1) || counts["notRun"] != float64(1) {
		t.Fatalf("suite coverage counts = %#v", counts)
	}
	items := payload["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("suite coverage items = %#v", items)
	}
	byCase := map[string]map[string]any{}
	for _, raw := range items {
		item := raw.(map[string]any)
		byCase[item["caseId"].(string)] = item
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
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	for _, item := range []struct {
		runID  string
		caseID string
		status string
		at     time.Time
	}{
		{runID: "run.default.latest", caseID: "case.default", status: store.StatusPassed, at: base},
		{runID: "run.variant.latest", caseID: "case.variant", status: store.StatusFailed, at: base.Add(time.Minute)},
	} {
		_, err := s.CreateRun(ctx, store.Run{
			ID:         item.runID,
			ProfileID:  "sample",
			WorkflowID: item.caseID,
			Status:     item.status,
			StartedAt:  item.at,
			FinishedAt: item.at.Add(time.Second),
			CreatedAt:  item.at,
			UpdatedAt:  item.at.Add(time.Second),
		})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
			ID:         item.runID + ".case",
			RunID:      item.runID,
			CaseID:     item.caseID,
			Status:     item.status,
			StartedAt:  item.at,
			FinishedAt: item.at.Add(time.Second),
			CreatedAt:  item.at,
		})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}

	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.default", DisplayName: "Default Case", NodeID: "node.alpha", CasePath: "cases/default.json", Tags: []string{"regression", "smoke"}, Priority: "p0", Owner: "team-a", SortOrder: 1},
			{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p2", Owner: "team-b", SortOrder: 3},
			{ID: "case.other", DisplayName: "Other Case", NodeID: "node.alpha", Tags: []string{"smoke"}, Priority: "p2", Owner: "team-c", SortOrder: 4},
		},
		TemplateConfigs: []profile.TemplateConfig{
			{ID: "config.case.variant", ScopeType: "case", ScopeID: "case.variant", Status: "active", ConfigJSON: `{"caseId":"case.variant","caseExecution":{"method":"GET","nodeId":"node.alpha","path":"/alpha"}}`},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/suite-inspection?tag=regression&status=active", http.StatusOK)
	if payload["ok"] != false {
		t.Fatalf("suite inspection ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(3) || counts["ready"] != float64(2) || counts["blocked"] != float64(1) || counts["failed"] != float64(1) || counts["notRun"] != float64(1) {
		t.Fatalf("suite inspection counts = %#v", counts)
	}
	items := payload["items"].([]any)
	byCase := map[string]map[string]any{}
	for _, raw := range items {
		item := raw.(map[string]any)
		byCase[item["caseId"].(string)] = item
	}
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
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	for _, item := range []struct {
		runID  string
		caseID string
		status string
		at     time.Time
	}{
		{runID: "run.default.latest", caseID: "case.default", status: store.StatusPassed, at: base},
		{runID: "run.variant.latest", caseID: "case.variant", status: store.StatusFailed, at: base.Add(time.Minute)},
	} {
		_, err := s.CreateRun(ctx, store.Run{
			ID:         item.runID,
			ProfileID:  "sample",
			WorkflowID: item.caseID,
			Status:     item.status,
			StartedAt:  item.at,
			FinishedAt: item.at.Add(time.Second),
			CreatedAt:  item.at,
			UpdatedAt:  item.at.Add(time.Second),
		})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
			ID:         item.runID + ".case",
			RunID:      item.runID,
			CaseID:     item.caseID,
			Status:     item.status,
			StartedAt:  item.at,
			FinishedAt: item.at.Add(time.Second),
			CreatedAt:  item.at,
		})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}

	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.default", DisplayName: "Default Case", NodeID: "node.alpha", CasePath: "cases/default.json", Tags: []string{"regression", "smoke"}, Priority: "p0", Owner: "team-a", SortOrder: 1},
			{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", CasePath: "cases/variant.json", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p2", Owner: "team-b", SortOrder: 3},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

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
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	for _, item := range []struct {
		runID  string
		caseID string
		status string
		at     time.Time
	}{
		{runID: "run.variant.1", caseID: "case.variant", status: store.StatusPassed, at: base},
		{runID: "run.variant.2", caseID: "case.variant", status: store.StatusFailed, at: base.Add(time.Minute)},
		{runID: "run.variant.3", caseID: "case.variant", status: store.StatusPassed, at: base.Add(2 * time.Minute)},
		{runID: "run.default.1", caseID: "case.default", status: store.StatusPassed, at: base.Add(3 * time.Minute)},
		{runID: "run.default.2", caseID: "case.default", status: store.StatusPassed, at: base.Add(4 * time.Minute)},
	} {
		_, err := s.CreateRun(ctx, store.Run{
			ID:         item.runID,
			ProfileID:  "sample",
			WorkflowID: item.caseID,
			Status:     item.status,
			StartedAt:  item.at,
			FinishedAt: item.at.Add(time.Second),
			CreatedAt:  item.at,
			UpdatedAt:  item.at.Add(time.Second),
		})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
			ID:         item.runID + ".case",
			RunID:      item.runID,
			CaseID:     item.caseID,
			Status:     item.status,
			StartedAt:  item.at,
			FinishedAt: item.at.Add(time.Second),
			CreatedAt:  item.at,
		})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}

	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.default", DisplayName: "Default Case", NodeID: "node.alpha", CasePath: "cases/default.json", Tags: []string{"regression"}, Priority: "p0", Owner: "team-a", SortOrder: 1},
			{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", CasePath: "cases/variant.json", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p2", Owner: "team-b", SortOrder: 3},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/suite-stability?tag=regression&status=active&limit=3", http.StatusOK)
	if payload["ok"] != false {
		t.Fatalf("suite stability ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(3) || counts["unstable"] != float64(1) || counts["stable"] != float64(1) || counts["notRun"] != float64(1) {
		t.Fatalf("suite stability counts = %#v", counts)
	}
	items := payload["items"].([]any)
	byCase := map[string]map[string]any{}
	for _, raw := range items {
		item := raw.(map[string]any)
		byCase[item["caseId"].(string)] = item
	}
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

func TestServerExposesCaseSuitePriorityBySignals(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	for _, item := range []struct {
		runID  string
		caseID string
		status string
		at     time.Time
	}{
		{runID: "run.impacted.1", caseID: "case.impacted", status: store.StatusPassed, at: base},
		{runID: "run.impacted.2", caseID: "case.impacted", status: store.StatusFailed, at: base.Add(time.Minute)},
		{runID: "run.failed.1", caseID: "case.failed", status: store.StatusFailed, at: base.Add(2 * time.Minute)},
		{runID: "run.low.1", caseID: "case.low", status: store.StatusPassed, at: base.Add(3 * time.Minute)},
	} {
		_, err := s.CreateRun(ctx, store.Run{ID: item.runID, ProfileID: "sample", Status: item.status, StartedAt: item.at, FinishedAt: item.at.Add(time.Second), CreatedAt: item.at, UpdatedAt: item.at.Add(time.Second)})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{ID: item.runID + ".case", RunID: item.runID, CaseID: item.caseID, Status: item.status, StartedAt: item.at, FinishedAt: item.at.Add(time.Second), CreatedAt: item.at})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.create", DisplayName: "Create Item", Operation: "Create", Path: "/v1/items"},
			{ID: "node.search", DisplayName: "Search Item", Operation: "Search", Path: "/v1/items/search"},
		},
		APICases: []profile.APICase{
			{ID: "case.impacted", DisplayName: "Impacted Case", NodeID: "node.create", CasePath: "cases/impacted.json", Tags: []string{"regression"}, Priority: "p1", Status: "active", SortOrder: 1},
			{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.search", CasePath: "cases/failed.json", Tags: []string{"regression"}, Priority: "p0", Status: "active", SortOrder: 2},
			{ID: "case.low", DisplayName: "Low Case", NodeID: "node.search", CasePath: "cases/low.json", Tags: []string{"regression"}, Priority: "p2", Status: "active", SortOrder: 3},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

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
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	for _, item := range []struct {
		runID  string
		caseID string
		status string
		at     time.Time
	}{
		{runID: "run.impacted.1", caseID: "case.impacted", status: store.StatusPassed, at: base},
		{runID: "run.impacted.2", caseID: "case.impacted", status: store.StatusFailed, at: base.Add(time.Minute)},
		{runID: "run.failed.1", caseID: "case.failed", status: store.StatusFailed, at: base.Add(2 * time.Minute)},
	} {
		_, err := s.CreateRun(ctx, store.Run{ID: item.runID, ProfileID: "sample", Status: item.status, StartedAt: item.at, FinishedAt: item.at.Add(time.Second), CreatedAt: item.at, UpdatedAt: item.at.Add(time.Second)})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{ID: item.runID + ".case", RunID: item.runID, CaseID: item.caseID, Status: item.status, StartedAt: item.at, FinishedAt: item.at.Add(time.Second), CreatedAt: item.at})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.create", DisplayName: "Create Item", Operation: "Create", Path: "/v1/items"},
			{ID: "node.search", DisplayName: "Search Item", Operation: "Search", Path: "/v1/items/search"},
		},
		APICases: []profile.APICase{
			{ID: "case.impacted", DisplayName: "Impacted Case", NodeID: "node.create", CasePath: "cases/impacted.json", Tags: []string{"regression"}, Priority: "p1", Status: "active", SortOrder: 1},
			{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.search", CasePath: "cases/failed.json", Tags: []string{"regression"}, Priority: "p0", Status: "active", SortOrder: 2},
			{ID: "case.blocked", DisplayName: "Blocked Case", NodeID: "node.search", Tags: []string{"regression"}, Priority: "p2", Status: "active", SortOrder: 3},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

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

func TestServerExposesCaseSuiteQuality(t *testing.T) {
	bundle := profile.Bundle{
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
	server := httptest.NewServer(controlplane.NewWithStore(bundle, nil))
	defer server.Close()

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
	bundle := profile.Bundle{
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
	server := httptest.NewServer(controlplane.NewWithStore(bundle, nil))
	defer server.Close()

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

func TestServerExposesCaseSuiteImpactPlan(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.create", DisplayName: "Create Item", ServiceID: "service.alpha", Operation: "Create", Method: "POST", Path: "/v1/items"},
			{ID: "node.other", DisplayName: "Other", ServiceID: "service.beta", Operation: "Other", Method: "GET", Path: "/v1/other"},
		},
		Workflows: []profile.Workflow{
			{ID: "workflow.item", DisplayName: "Item Flow"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.item", StepID: "create", NodeID: "node.create", CaseID: "case.create", SortOrder: 1},
		},
		APICases: []profile.APICase{
			{ID: "case.create", DisplayName: "Create default", NodeID: "node.create", CasePath: "cases/create.json", Tags: []string{"regression"}, Status: "active", SortOrder: 1},
			{ID: "case.other", DisplayName: "Other default", NodeID: "node.other", CasePath: "cases/other.json", Tags: []string{"regression"}, Status: "active", SortOrder: 2},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	endpoint := server.URL + "/api/case/suite-impact?signal=/v1/items&status=active&action=run&requestId=change-001&baseUrl=http://127.0.0.1:8080"
	payload := decodeJSONResponse(t, endpoint, http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("suite impact ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["signals"] != float64(1) || counts["nodes"] != float64(1) || counts["workflows"] != float64(1) || counts["cases"] != float64(1) || counts["selected"] != float64(1) {
		t.Fatalf("suite impact counts = %#v", counts)
	}
	batch := payload["batchRequest"].(map[string]any)
	caseIDs := batch["caseIds"].([]any)
	if len(caseIDs) != 1 || caseIDs[0] != "case.create" || batch["requestId"] != "change-001" || batch["baseUrl"] != "http://127.0.0.1:8080" {
		t.Fatalf("suite impact batch request = %#v", batch)
	}
	cases := payload["cases"].([]any)
	if len(cases) != 1 {
		t.Fatalf("suite impact cases = %#v", cases)
	}
	impacted := cases[0].(map[string]any)
	if impacted["caseId"] != "case.create" || len(impacted["reasons"].([]any)) == 0 {
		t.Fatalf("suite impact case = %#v", impacted)
	}
}

func TestServerStartsCaseSuiteImpactBatchRun(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/items" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Sandbox-Trace-Endpoint", "/v1/env-acceptance")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()
	dir := t.TempDir()
	casePath := filepath.Join(dir, "case-create.json")
	if err := os.WriteFile(casePath, []byte(`{
  "id": "case.create",
  "title": "Create default",
  "request": {"method": "GET", "path": "/v1/items"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`), 0o644); err != nil {
		t.Fatalf("write case: %v", err)
	}
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.create", DisplayName: "Create Item", ServiceID: "service.alpha", Operation: "Create", Method: "GET", Path: "/v1/items"},
		},
		APICases: []profile.APICase{
			{ID: "case.create", DisplayName: "Create default", NodeID: "node.create", CasePath: casePath, BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), Tags: []string{"regression"}, Status: "active", SortOrder: 1},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"change-004","signals":["/v1/items"],"status":"active","actions":["run"],"baseUrl":"` + target.URL + `"}`
	resp, err := http.Post(server.URL+"/api/case/suite-impact-runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post suite impact run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("suite impact run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		OK         bool   `json:"ok"`
		BatchRunID string `json:"batchRunId"`
		ReportURL  string `json:"reportUrl"`
		Impact     struct {
			BatchRequest struct {
				CaseIDs []string `json:"caseIds"`
			} `json:"batchRequest"`
		} `json:"impact"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode suite impact run: %v", err)
	}
	if !created.OK || created.BatchRunID == "" || created.ReportURL == "" || strings.Join(created.Impact.BatchRequest.CaseIDs, ",") != "case.create" {
		t.Fatalf("suite impact run response = %#v", created)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.Passed != 1 || report.Failed != 0 || len(report.Cases) != 1 {
		t.Fatalf("suite impact batch report = %#v", report)
	}
}
