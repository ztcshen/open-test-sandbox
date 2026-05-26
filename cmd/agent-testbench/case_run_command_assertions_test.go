package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

type caseRunDryRunPlan struct {
	OK      bool   `json:"ok"`
	DryRun  bool   `json:"dryRun"`
	RunID   string `json:"runId"`
	CaseID  string `json:"caseId"`
	Request struct {
		Method   string   `json:"method"`
		Path     string   `json:"path"`
		URL      string   `json:"url"`
		BodyKeys []string `json:"bodyKeys"`
	} `json:"request"`
	Assertions struct {
		ExpectedStatusCodes []int `json:"expectedStatusCodes"`
	} `json:"assertions"`
	Effects struct {
		HTTPRequest         bool   `json:"httpRequest"`
		WritesEvidence      bool   `json:"writesEvidence"`
		WritesStore         bool   `json:"writesStore"`
		PlannedEvidencePath string `json:"plannedEvidencePath"`
	} `json:"effects"`
}

func decodeCaseRunDryRunPlan(t *testing.T, raw string) caseRunDryRunPlan {
	t.Helper()
	var plan caseRunDryRunPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatalf("decode dry-run json: %v\n%s", err, raw)
	}
	return plan
}

func assertCaseRunDryRunPlan(t *testing.T, plan caseRunDryRunPlan, serverURL string, evidenceDir string) {
	t.Helper()
	if !plan.OK || !plan.DryRun || plan.RunID != "case-run-dry" || plan.CaseID != "case.alpha" {
		t.Fatalf("dry-run plan identity = %#v", plan)
	}
	if plan.Request.Method != "POST" || plan.Request.Path != "/v1/items" || plan.Request.URL != serverURL+"/v1/items" {
		t.Fatalf("dry-run request plan = %#v", plan.Request)
	}
	if len(plan.Request.BodyKeys) != 1 || plan.Request.BodyKeys[0] != "id" {
		t.Fatalf("dry-run body keys = %#v", plan.Request.BodyKeys)
	}
	if len(plan.Assertions.ExpectedStatusCodes) != 1 || plan.Assertions.ExpectedStatusCodes[0] != http.StatusOK {
		t.Fatalf("dry-run assertions = %#v", plan.Assertions)
	}
	if plan.Effects.HTTPRequest || plan.Effects.WritesEvidence || plan.Effects.WritesStore || plan.Effects.PlannedEvidencePath != filepath.Join(evidenceDir, "case-run-dry") {
		t.Fatalf("dry-run effects = %#v", plan.Effects)
	}
}

func runStoredFileCase(t *testing.T, runID string) (string, string) {
	t.Helper()
	fixture := newCaseRunFileFixture(t, false)
	runCLI(t, "case", "run", "--case", fixture.casePath, "--base-url", fixture.serverURL, "--run-id", runID, "--evidence-dir", fixture.evidenceDir, "--store", "sqlite://"+fixture.storePath, "--profile", "sample")
	return fixture.storePath, fixture.evidenceDir
}

func openCaseRunSQLiteStore(t *testing.T, storePath string) store.Store {
	t.Helper()
	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s
}

func assertIndexedParentRun(t *testing.T, s store.Store, runID string, evidenceRoot string) {
	t.Helper()
	run, err := s.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.ProfileID != "sample" || run.Status != "passed" || run.EvidenceRoot != evidenceRoot {
		t.Fatalf("run = %#v", run)
	}
	if !run.FinishedAt.After(run.StartedAt) {
		t.Fatalf("run timing was not indexed: %#v", run)
	}
	var runSummary struct {
		RunID  string `json:"runId"`
		CaseID string `json:"caseId"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(run.SummaryJSON), &runSummary); err != nil {
		t.Fatalf("decode run summary: %v", err)
	}
	if runSummary.RunID != runID || runSummary.Status != "passed" {
		t.Fatalf("run summary = %#v", runSummary)
	}
}

func assertIndexedAPICaseRun(t *testing.T, s store.Store, runID string, caseID string) store.APICaseRun {
	t.Helper()
	caseRuns, err := s.ListAPICaseRuns(context.Background(), runID)
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != caseID || caseRuns[0].Status != "passed" {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	if !caseRuns[0].FinishedAt.After(caseRuns[0].StartedAt) {
		t.Fatalf("case run timing was not indexed: %#v", caseRuns[0])
	}
	return caseRuns[0]
}

func assertIndexedCaseRunSummaries(t *testing.T, item store.APICaseRun) {
	t.Helper()
	var requestSummary struct {
		Method  string `json:"method"`
		Path    string `json:"path"`
		HasBody bool   `json:"hasBody"`
	}
	if err := json.Unmarshal([]byte(item.RequestSummaryJSON), &requestSummary); err != nil {
		t.Fatalf("decode request summary: %v", err)
	}
	if requestSummary.Method != "POST" || requestSummary.Path != "/v1/items" || !requestSummary.HasBody {
		t.Fatalf("request summary = %#v", requestSummary)
	}
	var assertionSummary struct {
		Status     string `json:"status"`
		ErrorCount int    `json:"errorCount"`
	}
	if err := json.Unmarshal([]byte(item.AssertionSummaryJSON), &assertionSummary); err != nil {
		t.Fatalf("decode assertion summary: %v", err)
	}
	if assertionSummary.Status != "passed" || assertionSummary.ErrorCount != 0 {
		t.Fatalf("assertion summary = %#v", assertionSummary)
	}
}

func assertIndexedResponseEvidence(t *testing.T, s store.Store, runID string, wantRecords int) {
	t.Helper()
	records, err := s.ListEvidence(context.Background(), runID)
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(records) != wantRecords {
		t.Fatalf("evidence records = %#v", records)
	}
	var responseSummary string
	for _, record := range records {
		if record.Kind == "response" {
			responseSummary = record.Summary
		}
	}
	if responseSummary == "" {
		t.Fatalf("response evidence missing from %#v", records)
	}
	var response struct {
		StatusCode int `json:"statusCode"`
		BodyBytes  int `json:"bodyBytes"`
	}
	if err := json.Unmarshal([]byte(responseSummary), &response); err != nil {
		t.Fatalf("decode response evidence summary: %v", err)
	}
	if response.StatusCode != http.StatusOK || response.BodyBytes == 0 {
		t.Fatalf("response evidence summary = %#v", response)
	}
}

type caseGateCLIReport struct {
	OK     bool   `json:"ok"`
	RunID  string `json:"runId"`
	Counts struct {
		Total            int `json:"total"`
		Passed           int `json:"passed"`
		Failed           int `json:"failed"`
		EvidenceComplete int `json:"evidenceComplete"`
	} `json:"counts"`
	Gates struct {
		HasCaseRuns      bool `json:"hasCaseRuns"`
		NoFailures       bool `json:"noFailures"`
		EvidenceComplete bool `json:"evidenceComplete"`
	} `json:"gates"`
	FailedCaseRuns []struct {
		ID     string `json:"id"`
		CaseID string `json:"caseId"`
		Status string `json:"status"`
	} `json:"failedCaseRuns"`
	NextActions []string `json:"nextActions"`
}

func seedCaseGateFailureStore(t *testing.T, dir string) (string, string) {
	t.Helper()
	ctx := context.Background()
	storePath := filepath.Join(dir, "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	started := time.Date(2026, 5, 24, 9, 0, 0, 0, time.UTC)
	runID := "run.case-gate"
	if _, err := s.CreateRun(ctx, store.Run{
		ID:           runID,
		ProfileID:    "sample",
		Status:       store.StatusFailed,
		EvidenceRoot: filepath.Join(dir, "evidence", runID),
		StartedAt:    started,
		FinishedAt:   started.Add(time.Second),
		CreatedAt:    started,
		UpdatedAt:    started.Add(time.Second),
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	recordCaseGateAPICaseRuns(t, ctx, s, runID, dir, started)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	return storePath, runID
}

func recordCaseGateAPICaseRuns(t *testing.T, ctx context.Context, s store.Store, runID string, dir string, started time.Time) {
	t.Helper()
	for _, item := range []struct {
		id     string
		caseID string
		status string
	}{
		{id: runID + ".passed", caseID: "case.passed", status: store.StatusPassed},
		{id: runID + ".failed", caseID: "case.failed", status: store.StatusFailed},
	} {
		recordCaseGateAPICaseRunWithEvidence(t, ctx, s, runID, dir, started, item.id, item.caseID, item.status)
	}
}

func recordCaseGateAPICaseRunWithEvidence(t *testing.T, ctx context.Context, s store.Store, runID string, dir string, started time.Time, id string, caseID string, status string) {
	t.Helper()
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   id,
		RunID:                runID,
		CaseID:               caseID,
		Status:               status,
		RequestSummaryJSON:   `{"method":"GET","path":"/gate"}`,
		AssertionSummaryJSON: `{"status":"` + status + `"}`,
		StartedAt:            started,
		FinishedAt:           started.Add(time.Second),
		CreatedAt:            started,
	}); err != nil {
		t.Fatalf("record case run %s: %v", id, err)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        id + ".summary",
		RunID:     runID,
		CaseRunID: id,
		Kind:      "summary",
		URI:       filepath.Join(dir, "evidence", runID, id, "summary.json"),
		MediaType: "application/json",
		Summary:   `{"kind":"summary"}`,
		CreatedAt: started,
	}); err != nil {
		t.Fatalf("record evidence %s: %v", id, err)
	}
}

func decodeCaseGateCLIReport(t *testing.T, raw string) caseGateCLIReport {
	t.Helper()
	var report caseGateCLIReport
	if err := json.Unmarshal([]byte(extractJSONObject(t, raw)), &report); err != nil {
		t.Fatalf("decode gate json: %v\n%s", err, raw)
	}
	return report
}

func assertCaseGateFailureReport(t *testing.T, report caseGateCLIReport, runID string) {
	t.Helper()
	if report.OK || report.RunID != runID || report.Counts.Total != 2 || report.Counts.Passed != 1 || report.Counts.Failed != 1 || report.Counts.EvidenceComplete != 2 {
		t.Fatalf("gate report counts = %#v", report)
	}
	if !report.Gates.HasCaseRuns || report.Gates.NoFailures || !report.Gates.EvidenceComplete {
		t.Fatalf("gate booleans = %#v", report.Gates)
	}
	if len(report.FailedCaseRuns) != 1 || report.FailedCaseRuns[0].ID != runID+".failed" || report.FailedCaseRuns[0].CaseID != "case.failed" {
		t.Fatalf("failed case runs = %#v", report.FailedCaseRuns)
	}
	if !strings.Contains(strings.Join(report.NextActions, "\n"), "agent-testbench case diagnose --case-run "+runID+".failed") {
		t.Fatalf("gate next actions = %#v", report.NextActions)
	}
}

func seedCatalogCaseStore(t *testing.T, storePath string) {
	t.Helper()
	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(context.Background(), store.ProfileCatalog{
		ProfileID: "sample",
		APICases: []store.CatalogAPICase{{
			ID:          "case.catalog",
			DisplayName: "Catalog Case",
			NodeID:      "node.alpha",
		}},
		TemplateConfigs: []store.CatalogTemplateConfig{{
			ID:         "cfg.case.catalog",
			ScopeType:  "api-case",
			ScopeID:    "case.catalog",
			ConfigJSON: `{"caseId":"case.catalog","caseExecution":{"method":"POST","nodeId":"node.alpha","path":"/v1/catalog","body":{"id":"{{ override:id }}"},"expectedHttpCodes":[201]}}`,
			Status:     "active",
		}},
	}); err != nil {
		t.Fatalf("replace catalog: %v", err)
	}
}

func assertCatalogCaseRunPayload(t *testing.T, raw string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode case-id run json: %v\n%s", err, raw)
	}
	if payload["runId"] != "catalog-run-001" || payload["caseRunId"] != "catalog-run-001.case" || payload["caseId"] != "case.catalog" || payload["status"] != "passed" {
		t.Fatalf("case-id run payload = %#v", payload)
	}
}

func assertCatalogCaseRunStore(t *testing.T, storePath string, evidenceDir string) {
	t.Helper()
	s := openCaseRunSQLiteStore(t, storePath)
	defer s.Close()
	run, err := s.GetRun(context.Background(), "catalog-run-001")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.ProfileID != "sample" || run.Status != "passed" || run.EvidenceRoot != filepath.Join(evidenceDir, "catalog-run-001") {
		t.Fatalf("run = %#v", run)
	}
	assertIndexedAPICaseRun(t, s, "catalog-run-001", "case.catalog")
	records, err := s.ListEvidence(context.Background(), "catalog-run-001")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("evidence records = %#v", records)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "catalog-run-001", "request.json")); err != nil {
		t.Fatalf("request evidence missing: %v", err)
	}
}
