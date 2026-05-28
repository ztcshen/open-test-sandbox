package casesuite

import (
	"context"
	"strings"
	"testing"
	"time"

	domaincatalog "agent-testbench/internal/domain/catalog"
	"agent-testbench/internal/domain/execution"
	"agent-testbench/internal/domain/profile"
)

type priorityScenario struct {
	bundle  profile.Bundle
	records []execution.APICaseRunRecord
}

func writePriorityScenario(t *testing.T, includeLowCase bool) priorityScenario {
	t.Helper()
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	apiCases := []profile.APICase{
		{ID: "case.impacted", DisplayName: "Impacted Case", NodeID: "node.alpha", CasePath: "cases/impacted.json", Tags: []string{"regression"}, Priority: "p1", SortOrder: 1},
		{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.beta", CasePath: "cases/failed.json", Tags: []string{"regression"}, Priority: "p0", SortOrder: 2},
		{ID: "case.blocked", DisplayName: "Blocked Case", NodeID: "node.beta", Tags: []string{"regression"}, Priority: "p0", SortOrder: 3},
	}
	records := []execution.APICaseRunRecord{
		record("run.impacted.1", "case.impacted", execution.StatusPassed, base),
		record("run.impacted.2", "case.impacted", execution.StatusFailed, base.Add(time.Minute)),
		record("run.impacted.3", "case.impacted", execution.StatusPassed, base.Add(2*time.Minute)),
		record("run.failed.1", "case.failed", execution.StatusFailed, base.Add(3*time.Minute)),
	}
	if includeLowCase {
		apiCases = append(apiCases, profile.APICase{ID: "case.low", DisplayName: "Low Case", NodeID: "node.beta", CasePath: "cases/low.json", Tags: []string{"regression"}, Priority: "p2", SortOrder: 4})
		records = append(records, record("run.low.1", "case.low", execution.StatusPassed, base.Add(4*time.Minute)))
	}
	return priorityScenario{
		bundle: profile.Bundle{
			ID: "sample",
			InterfaceNodes: []profile.InterfaceNode{
				{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Create", Path: "/v1/items"},
				{ID: "node.beta", DisplayName: "Node Beta", Operation: "Search", Path: "/v1/items/search"},
			},
			APICases: apiCases,
		},
		records: records,
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

type recordStore struct {
	records []execution.APICaseRunRecord
	catalog *domaincatalog.ProfileCatalog
}

func (s recordStore) ListAPICaseRunRecordsForCaseIDs(context.Context, []string) ([]execution.APICaseRunRecord, error) {
	return s.records, nil
}

func (s recordStore) ListRuns(context.Context) ([]execution.Run, error) {
	return nil, nil
}

func (s recordStore) ListAPICaseRuns(context.Context, string) ([]execution.APICaseRun, error) {
	return nil, nil
}

func (s recordStore) GetProfileCatalog(context.Context) (domaincatalog.ProfileCatalog, error) {
	if s.catalog == nil {
		return domaincatalog.ProfileCatalog{}, nil
	}
	return *s.catalog, nil
}

func record(runID string, caseID string, status string, at time.Time) execution.APICaseRunRecord {
	return execution.APICaseRunRecord{
		Run: execution.Run{
			ID:        runID,
			ProfileID: "sample",
			Status:    status,
			CreatedAt: at,
			UpdatedAt: at.Add(time.Second),
		},
		CaseRun: execution.APICaseRun{
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
