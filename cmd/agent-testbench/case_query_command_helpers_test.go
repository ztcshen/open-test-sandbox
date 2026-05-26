package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

type caseRunsCommandFixture struct {
	runID        string
	caseRunID    string
	caseID       string
	evidencePath string
}

type caseEvidenceCommandFixture struct {
	runID       string
	caseRunID   string
	evidenceURI string
}

type caseTimingCommandFixture struct {
	slowRunID     string
	slowCaseID    string
	maxDurationMs int
}

type caseQueryTimedCase struct {
	runID    string
	caseID   string
	duration time.Duration
}

type caseRunsCommandReport struct {
	OK       bool                    `json:"ok"`
	CaseRuns []caseRunsCommandResult `json:"caseRuns"`
}

type caseRunsCommandResult struct {
	ID            string `json:"id"`
	RunID         string `json:"runId"`
	CaseID        string `json:"caseId"`
	Status        string `json:"status"`
	Operation     string `json:"operation"`
	EvidenceCount int    `json:"evidenceCount"`
	EvidencePath  string `json:"evidencePath"`
}

type caseEvidenceCommandPayload struct {
	OK       bool `json:"ok"`
	Evidence struct {
		Summary  map[string]any `json:"summary"`
		Request  map[string]any `json:"request"`
		Response map[string]any `json:"response"`
	} `json:"evidence"`
}

type caseTimingCommandPayload struct {
	OK      bool `json:"ok"`
	Summary struct {
		CaseRunCount          int            `json:"caseRunCount"`
		DurationMeasuredCount int            `json:"durationMeasuredCount"`
		MaxDurationMs         int            `json:"maxDurationMs"`
		SlowestRows           map[string]any `json:"slowestRows"`
	} `json:"summary"`
}

func openCaseQueryStore(t *testing.T, storeRef string, label string) (context.Context, store.Store) {
	t.Helper()
	ctx := context.Background()
	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open %s store: %v", label, err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return ctx, s
}

func recordCaseQueryRun(t *testing.T, ctx context.Context, s store.Store, label string, run store.Run) {
	t.Helper()
	if _, err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("create %s run %s: %v", label, run.ID, err)
	}
}

func recordCaseQueryAPICaseRun(t *testing.T, ctx context.Context, s store.Store, label string, caseRun store.APICaseRun) {
	t.Helper()
	if _, err := s.RecordAPICaseRun(ctx, caseRun); err != nil {
		t.Fatalf("record %s case run %s: %v", label, caseRun.ID, err)
	}
}

func recordCaseQueryEvidence(t *testing.T, ctx context.Context, s store.Store, label string, evidence store.EvidenceRecord) {
	t.Helper()
	if _, err := s.RecordEvidence(ctx, evidence); err != nil {
		t.Fatalf("record %s evidence %s: %v", label, evidence.ID, err)
	}
}

func seedCaseRunsCommandFixture(t *testing.T, storeRef string, label string) caseRunsCommandFixture {
	t.Helper()
	ctx, s := openCaseQueryStore(t, storeRef, label)
	runID := uniqueTestID(t, "run.case-runs")
	started := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	fixture := caseRunsCommandFixture{
		runID:        runID,
		caseRunID:    runID + ".case",
		caseID:       uniqueTestID(t, "case.alpha"),
		evidencePath: "/tmp/evidence/" + runID,
	}
	recordCaseQueryRun(t, ctx, s, label, store.Run{
		ID:           fixture.runID,
		ProfileID:    uniqueTestID(t, "profile.case-runs"),
		WorkflowID:   uniqueTestID(t, "workflow.case-runs"),
		Status:       store.StatusPassed,
		EvidenceRoot: fixture.evidencePath,
		StartedAt:    started,
		FinishedAt:   started.Add(time.Second),
	})
	recordCaseQueryAPICaseRun(t, ctx, s, label, store.APICaseRun{
		ID:                   fixture.caseRunID,
		RunID:                fixture.runID,
		CaseID:               fixture.caseID,
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"passed"}`,
		StartedAt:            started,
		FinishedAt:           started.Add(250 * time.Millisecond),
	})
	recordCaseQueryEvidence(t, ctx, s, label, store.EvidenceRecord{
		ID:        fixture.runID + ".evidence",
		RunID:     fixture.runID,
		CaseRunID: fixture.caseRunID,
		Kind:      "http-response",
		URI:       fixture.evidencePath + "/response.json",
	})
	return fixture
}

func assertCaseRunsReport(t *testing.T, label string, out string, expected caseRunsCommandFixture) {
	t.Helper()
	var report caseRunsCommandReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s case runs json: %v\n%s", label, err, out)
	}
	if !report.OK || len(report.CaseRuns) != 1 {
		t.Fatalf("%s case runs report = %#v", label, report)
	}
	item := report.CaseRuns[0]
	if item.ID != expected.caseRunID || item.RunID != expected.runID || item.CaseID != expected.caseID || item.Status != store.StatusPassed || item.Operation != "POST /alpha" || item.EvidenceCount != 1 || item.EvidencePath != expected.evidencePath {
		t.Fatalf("%s case run item = %#v", label, item)
	}
}

func seedCaseEvidenceCommandFixture(t *testing.T, storeRef string, label string) caseEvidenceCommandFixture {
	t.Helper()
	ctx, s := openCaseQueryStore(t, storeRef, label)
	runID := uniqueTestID(t, "run.case-evidence")
	fixture := caseEvidenceCommandFixture{
		runID:       runID,
		caseRunID:   runID + ".case",
		evidenceURI: "/tmp/evidence/" + runID + "/response.json",
	}
	recordCaseQueryRun(t, ctx, s, label, store.Run{
		ID:           fixture.runID,
		ProfileID:    uniqueTestID(t, "profile.case-evidence"),
		WorkflowID:   uniqueTestID(t, "workflow.case-evidence"),
		Status:       store.StatusPassed,
		EvidenceRoot: "/tmp/evidence/" + fixture.runID,
		SummaryJSON:  "{}",
	})
	recordCaseQueryAPICaseRun(t, ctx, s, label, store.APICaseRun{
		ID:                   fixture.caseRunID,
		RunID:                fixture.runID,
		CaseID:               uniqueTestID(t, "case.alpha"),
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"GET","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"passed"}`,
	})
	recordCaseQueryEvidence(t, ctx, s, label, store.EvidenceRecord{
		ID:        fixture.runID + ".response",
		RunID:     fixture.runID,
		CaseRunID: fixture.caseRunID,
		Kind:      "response",
		URI:       fixture.evidenceURI,
		MediaType: "application/json",
		Summary:   `{"statusCode":200}`,
	})
	return fixture
}

func assertCaseEvidencePayload(t *testing.T, label string, out string, expected caseEvidenceCommandFixture) {
	t.Helper()
	var payload caseEvidenceCommandPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode %s case evidence json: %v\n%s", label, err, out)
	}
	if !payload.OK || payload.Evidence.Summary["case_run_id"] != expected.caseRunID || payload.Evidence.Summary["operation"] != "GET /alpha" {
		t.Fatalf("%s case evidence summary = %#v", label, payload.Evidence.Summary)
	}
	if payload.Evidence.Response["http_code"] != float64(200) || payload.Evidence.Response["evidence_uri"] != expected.evidenceURI {
		t.Fatalf("%s case evidence response = %#v", label, payload.Evidence.Response)
	}
}

func seedCaseTimingCommandFixture(t *testing.T, storeRef string, label string) caseTimingCommandFixture {
	t.Helper()
	ctx, s := openCaseQueryStore(t, storeRef, label)
	fastRunID := uniqueTestID(t, "run.fast")
	slowDuration := 36 * time.Hour
	fixture := caseTimingCommandFixture{
		slowRunID:     uniqueTestID(t, "run.slow"),
		slowCaseID:    uniqueTestID(t, "case.slow"),
		maxDurationMs: int(slowDuration.Milliseconds()),
	}
	base := time.Now().UTC()
	for _, item := range []caseQueryTimedCase{
		{runID: fastRunID, caseID: uniqueTestID(t, "case.fast"), duration: 200 * time.Millisecond},
		{runID: fixture.slowRunID, caseID: fixture.slowCaseID, duration: slowDuration},
	} {
		recordCaseQueryTimedCaseRun(t, ctx, s, label, item, base)
	}
	return fixture
}

func recordCaseQueryTimedCaseRun(t *testing.T, ctx context.Context, s store.Store, label string, item caseQueryTimedCase, started time.Time) {
	t.Helper()
	recordCaseQueryRun(t, ctx, s, label, store.Run{
		ID:         item.runID,
		ProfileID:  "sample",
		Status:     store.StatusPassed,
		StartedAt:  started,
		FinishedAt: started.Add(item.duration),
		CreatedAt:  started,
		UpdatedAt:  started.Add(item.duration),
	})
	recordCaseQueryAPICaseRun(t, ctx, s, label, store.APICaseRun{
		ID:         item.runID + ".case",
		RunID:      item.runID,
		CaseID:     item.caseID,
		Status:     store.StatusPassed,
		StartedAt:  started,
		FinishedAt: started.Add(item.duration),
		CreatedAt:  started,
	})
}

func assertCaseTimingPayload(t *testing.T, label string, out string, expected caseTimingCommandFixture) {
	t.Helper()
	var payload caseTimingCommandPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode %s case timing json: %v\n%s", label, err, out)
	}
	if !payload.OK || payload.Summary.CaseRunCount < 2 || payload.Summary.DurationMeasuredCount < 2 || payload.Summary.MaxDurationMs < expected.maxDurationMs {
		t.Fatalf("%s case timing summary = %#v", label, payload.Summary)
	}
	slowest := payload.Summary.SlowestRows["caseRun"].(map[string]any)
	if slowest["id"] != expected.slowRunID+".case" || slowest["caseId"] != expected.slowCaseID || slowest["durationMs"] != float64(expected.maxDurationMs) {
		t.Fatalf("%s case timing slowest = %#v", label, slowest)
	}
}

func createCaseQueryStoreFlagStore(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()
	seedCaseQueryStoreFlagFixture(t, ctx, s)
	return "sqlite://" + storePath
}

func seedCaseQueryStoreFlagFixture(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()
	started := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	recordCaseQueryRun(t, ctx, s, "store flag", store.Run{
		ID:           "run-store-flag",
		ProfileID:    "sample",
		Status:       store.StatusPassed,
		EvidenceRoot: "/tmp/evidence/run-store-flag",
		StartedAt:    started,
		FinishedAt:   started.Add(time.Second),
		CreatedAt:    started,
		UpdatedAt:    started.Add(time.Second),
	})
	recordCaseQueryAPICaseRun(t, ctx, s, "store flag", store.APICaseRun{
		ID:                   "case-run-store-flag",
		RunID:                "run-store-flag",
		CaseID:               "case.alpha",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"GET","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"passed"}`,
		StartedAt:            started,
		FinishedAt:           started.Add(500 * time.Millisecond),
		CreatedAt:            started,
	})
	recordCaseQueryEvidence(t, ctx, s, "store flag", store.EvidenceRecord{
		ID:        "response-store-flag",
		RunID:     "run-store-flag",
		CaseRunID: "case-run-store-flag",
		Kind:      "response",
		URI:       "/tmp/evidence/run-store-flag/response.json",
		MediaType: "application/json",
		Summary:   `{"statusCode":200}`,
	})
}

func assertCaseQueryStoreFlagCommands(t *testing.T, storeRef string) {
	t.Helper()
	runsOut := runCLI(t, "case", "runs", "--store", storeRef, "--json")
	if !strings.Contains(runsOut, "case-run-store-flag") {
		t.Fatalf("case runs output = %q", runsOut)
	}
	evidenceOut := runCLI(t, "case", "evidence", "--store", storeRef, "--case-run", "case-run-store-flag", "--json")
	if !strings.Contains(evidenceOut, "case-run-store-flag") || !strings.Contains(evidenceOut, "/alpha") {
		t.Fatalf("case evidence output = %q", evidenceOut)
	}
	timingOut := runCLI(t, "case", "timing", "--store", storeRef, "--kind", "case", "--json")
	if !strings.Contains(timingOut, `"maxDurationMs": 500`) {
		t.Fatalf("case timing output = %q", timingOut)
	}
}
