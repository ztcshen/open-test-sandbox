package controlplane_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
)

type caseSuiteRouteRun struct {
	runID                string
	caseID               string
	status               string
	at                   time.Time
	workflowID           string
	assertionSummaryJSON string
}

func caseSuiteRun(runID, caseID, status string, at time.Time) caseSuiteRouteRun {
	return caseSuiteRouteRun{runID: runID, caseID: caseID, status: status, at: at}
}

func caseSuiteWorkflowRun(runID, caseID, status string, at time.Time) caseSuiteRouteRun {
	run := caseSuiteRun(runID, caseID, status, at)
	run.workflowID = caseID
	return run
}

func (r caseSuiteRouteRun) withAssertionSummary(summary string) caseSuiteRouteRun {
	r.assertionSummaryJSON = summary
	return r
}

func openCaseSuiteRouteStore(t *testing.T) (context.Context, store.Store) {
	t.Helper()

	ctx := context.Background()
	return ctx, openTestKitSQLiteStore(t, ctx, "sandbox.sqlite")
}

func recordCaseSuiteRouteRuns(t *testing.T, ctx context.Context, s store.Store, runs ...caseSuiteRouteRun) {
	t.Helper()

	for _, item := range runs {
		_, err := s.CreateRun(ctx, store.Run{
			ID:         item.runID,
			ProfileID:  "sample",
			WorkflowID: item.workflowID,
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
			AssertionSummaryJSON: item.assertionSummaryJSON,
			StartedAt:            item.at,
			FinishedAt:           item.at.Add(time.Second),
			CreatedAt:            item.at,
		})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}
}

func serveCaseSuiteRouteBundle(t *testing.T, bundle profile.Bundle, s store.Store) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	t.Cleanup(server.Close)
	return server
}

func caseSuiteAlphaBundle(cases []profile.APICase, configs ...profile.TemplateConfig) profile.Bundle {
	return profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Alpha"},
		},
		APICases:        cases,
		TemplateConfigs: configs,
	}
}

func caseSuiteSignalBundle(cases []profile.APICase) profile.Bundle {
	return profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.create", DisplayName: "Create Item", Operation: "Create", Path: "/v1/items"},
			{ID: "node.search", DisplayName: "Search Item", Operation: "Search", Path: "/v1/items/search"},
		},
		APICases: cases,
	}
}

func caseSuiteItemsByCase(t *testing.T, payload map[string]any) map[string]map[string]any {
	t.Helper()

	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("suite payload items = %#v", payload["items"])
	}
	byCase := make(map[string]map[string]any, len(items))
	for _, raw := range items {
		item := raw.(map[string]any)
		byCase[item["caseId"].(string)] = item
	}
	return byCase
}
