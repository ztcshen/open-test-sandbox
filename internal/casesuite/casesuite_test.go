package casesuite

import (
	"context"
	"testing"
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func TestSelectCasesFiltersByMaintenanceMetadata(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Alpha Default", NodeID: "node.alpha", Tags: []string{"regression", "smoke"}, Priority: "p0", Owner: "team-a", SortOrder: 2},
			{ID: "case.beta", DisplayName: "Beta Variant", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 1},
			{ID: "case.gamma", DisplayName: "Gamma Other", NodeID: "node.beta", Tags: []string{"smoke"}, Priority: "p2", Owner: "team-b", Status: "paused", SortOrder: 3},
		},
	}

	cases := SelectCases(bundle, Filter{Tags: []string{"regression"}, Owner: "team-a", Status: "active"})
	if len(cases) != 2 || cases[0].ID != "case.beta" || cases[1].ID != "case.alpha" {
		t.Fatalf("selected cases = %#v", cases)
	}

	filtered := SelectCases(bundle, Filter{Filter: "variant", Tags: []string{"regression"}, Status: "active"})
	if len(filtered) != 1 || filtered[0].ID != "case.beta" {
		t.Fatalf("filtered cases = %#v", filtered)
	}
}

func TestCoverageReportsLatestStatusAndHasPassed(t *testing.T) {
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.passed", DisplayName: "Passed Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 1},
			{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 3},
		},
	}
	records := []store.APICaseRunRecord{
		record("run.failed.old", "case.failed", store.StatusPassed, base),
		record("run.passed.latest", "case.passed", store.StatusPassed, base.Add(time.Minute)),
		record("run.failed.latest", "case.failed", store.StatusFailed, base.Add(2*time.Minute)),
	}
	cases := SelectCases(bundle, Filter{Tags: []string{"regression"}, Status: "active"})

	report, err := Coverage(context.Background(), bundle, recordStore{records: records}, Filter{Tags: []string{"regression"}, Status: "active"}, cases)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if report.OK || report.Counts.Total != 3 || report.Counts.Passed != 1 || report.Counts.Failed != 1 || report.Counts.NotRun != 1 {
		t.Fatalf("coverage report = %#v", report)
	}
	byCase := map[string]Item{}
	for _, item := range report.Items {
		byCase[item.CaseID] = item
	}
	if !byCase["case.failed"].HasPassed || byCase["case.failed"].LatestStatus != store.StatusFailed || byCase["case.failed"].LatestRunID != "run.failed.latest" {
		t.Fatalf("failed case item = %#v", byCase["case.failed"])
	}
	if byCase["case.unrun"].LatestStatus != "not-run" || byCase["case.unrun"].Reason != "no run recorded in Store" {
		t.Fatalf("unrun case item = %#v", byCase["case.unrun"])
	}
}

func TestNormalizeRunStateAliases(t *testing.T) {
	for input, want := range map[string]string{
		"fail":      store.StatusFailed,
		"failed":    store.StatusFailed,
		"PASS":      store.StatusPassed,
		"never-run": "not-run",
		"missing":   "not-run",
	} {
		if got := NormalizeRunState(input); got != want {
			t.Fatalf("NormalizeRunState(%q) = %q, want %q", input, got, want)
		}
	}
}

type recordStore struct {
	store.Store
	records []store.APICaseRunRecord
}

func (s recordStore) ListAPICaseRunRecordsForCaseIDs(context.Context, []string) ([]store.APICaseRunRecord, error) {
	return s.records, nil
}

func record(runID string, caseID string, status string, at time.Time) store.APICaseRunRecord {
	return store.APICaseRunRecord{
		Run: store.Run{
			ID:        runID,
			ProfileID: "sample",
			Status:    status,
			CreatedAt: at,
			UpdatedAt: at.Add(time.Second),
		},
		CaseRun: store.APICaseRun{
			ID:         runID + ".case",
			RunID:      runID,
			CaseID:     caseID,
			Status:     status,
			StartedAt:  at,
			FinishedAt: at.Add(time.Second),
			CreatedAt:  at,
		},
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}
