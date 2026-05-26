package controlplane_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

func TestServerExposesWorkflowAuditWithoutStore(t *testing.T) {
	server := newWorkflowRoutesProfileServer(t, workflowAuditSingleStepProfile())

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.alpha", http.StatusOK)
	if payload["ok"] != true || payload["profileId"] != "sample" || payload["workflowId"] != "workflow.alpha" {
		t.Fatalf("workflow audit identity = %#v", payload)
	}
	if payload["bindingCount"] != float64(1) || payload["issueCount"] != float64(0) {
		t.Fatalf("workflow audit counts = %#v", payload)
	}
	if _, ok := payload["store"]; ok {
		t.Fatalf("workflow audit without store should not include store report: %#v", payload)
	}
	bindings := payload["bindings"].([]any)
	if len(bindings) != 1 || bindings[0].(map[string]any)["caseId"] != "case.alpha" {
		t.Fatalf("workflow audit bindings = %#v", payload)
	}
}

func TestServerExposesWorkflowPlanAPI(t *testing.T) {
	server := newWorkflowRoutesProfileServer(t, workflowPlanProfile())

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-plan?workflowId=workflow.alpha", http.StatusOK)
	if payload["ok"] != true || payload["workflowId"] != "workflow.alpha" {
		t.Fatalf("workflow plan summary = %#v", payload)
	}
	workflow := payload["workflow"].(map[string]any)
	if workflow["id"] != "workflow.alpha" || workflow["displayName"] != "Workflow Alpha" {
		t.Fatalf("workflow plan workflow = %#v", workflow)
	}
	counts := payload["counts"].(map[string]any)
	if counts["steps"] != float64(1) || counts["requiredSteps"] != float64(1) {
		t.Fatalf("workflow plan counts = %#v", counts)
	}
	steps := payload["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("workflow plan steps = %#v", payload)
	}
	step := steps[0].(map[string]any)
	if step["stepId"] != "step.one" || step["nodeId"] != "node.alpha" || step["caseId"] != "case.alpha" || step["required"] != true {
		t.Fatalf("workflow plan step = %#v", step)
	}
	if node := step["node"].(map[string]any); node["displayName"] != "Node Alpha" {
		t.Fatalf("workflow plan step node = %#v", node)
	}
	if item := step["case"].(map[string]any); item["displayName"] != "Case Alpha" {
		t.Fatalf("workflow plan step case = %#v", item)
	}
}

func TestServerExposesWorkflowDiscoveryAPI(t *testing.T) {
	server := newWorkflowRoutesProfileServer(t, workflowDiscoveryProfile())

	payload := decodeJSONResponse(t, server.URL+"/api/workflows?filter=smoke", http.StatusOK)
	if payload["ok"] != true || payload["profileId"] != "sample" || payload["count"] != float64(1) {
		t.Fatalf("workflow discovery summary = %#v", payload)
	}
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("workflow discovery items = %#v", payload)
	}
	item := items[0].(map[string]any)
	if item["id"] != "workflow.alpha" || item["displayName"] != "Workflow Alpha" || item["stepCount"] != float64(2) {
		t.Fatalf("workflow discovery item = %#v", item)
	}
}

func TestServerExposesWorkflowDiscoveryAPIFromStore(t *testing.T) {
	ctx := context.Background()
	s := openWorkflowRoutesSQLiteStore(t, ctx)
	replaceWorkflowDiscoveryStoreCatalog(t, ctx, s)
	server := newWorkflowRoutesStoreServer(t, profile.Bundle{ID: "bundle-profile"}, s)

	payload := decodeJSONResponse(t, server.URL+"/api/workflows?filter=store", http.StatusOK)
	if payload["ok"] != true || payload["profileId"] != "store-profile" || payload["count"] != float64(1) {
		t.Fatalf("workflow discovery store summary = %#v", payload)
	}
	source := payload["source"].(map[string]any)
	if source["kind"] != "store" {
		t.Fatalf("workflow discovery source = %#v", source)
	}
	item := payload["items"].([]any)[0].(map[string]any)
	if item["id"] != "workflow.store" || item["stepCount"] != float64(1) {
		t.Fatalf("workflow discovery store item = %#v", item)
	}
}

func TestServerExposesWorkflowAuditStoreState(t *testing.T) {
	ctx := context.Background()
	s := openWorkflowRoutesSQLiteStore(t, ctx)
	recordWorkflowAuditStoreRuns(t, ctx, s)
	server := newWorkflowRoutesStoreServer(t, workflowAuditStoreStateProfile(), s)

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.alpha", http.StatusOK)
	storeReport := payload["store"].(map[string]any)
	latestRun := storeReport["latestRun"].(map[string]any)
	if latestRun["id"] != "run.beta" || latestRun["status"] != store.StatusFailed {
		t.Fatalf("workflow audit latest run = %#v", storeReport)
	}
	bindingCases := storeReport["bindingCases"].([]any)
	if len(bindingCases) != 2 {
		t.Fatalf("workflow audit binding cases = %#v", storeReport)
	}
	alpha := bindingCases[0].(map[string]any)
	if alpha["caseId"] != "case.alpha" || alpha["latestStatus"] != store.StatusFailed || alpha["latestRunId"] != "run.beta" || alpha["hasPassed"] != true {
		t.Fatalf("workflow audit alpha case state = %#v", alpha)
	}
	beta := bindingCases[1].(map[string]any)
	if beta["caseId"] != "case.beta" || beta["latestStatus"] != store.StatusPassed || beta["latestRunId"] != "run.beta" || beta["required"] != false {
		t.Fatalf("workflow audit beta case state = %#v", beta)
	}
}

func TestServerRejectsWorkflowAuditWithoutWorkflowID(t *testing.T) {
	server := newWorkflowRoutesProfileServer(t, profile.Bundle{ID: "sample"})

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit", http.StatusBadRequest)
	if payload["ok"] != false || !strings.Contains(payload["error"].(string), "workflowId") {
		t.Fatalf("workflow audit missing id payload = %#v", payload)
	}
}

func TestServerReturnsNotFoundForUnknownWorkflowAudit(t *testing.T) {
	server := newWorkflowRoutesProfileServer(t, profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
	})

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.missing", http.StatusNotFound)
	if payload["ok"] != false || !strings.Contains(payload["error"].(string), "workflow not found") {
		t.Fatalf("workflow audit missing workflow payload = %#v", payload)
	}
}

func TestServerReturnsInternalErrorForWorkflowAuditStoreFailure(t *testing.T) {
	server := newWorkflowRoutesStoreServer(t, profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
	}, failingListRunsStore{})

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.alpha", http.StatusInternalServerError)
	if payload["ok"] != false || !strings.Contains(payload["error"].(string), "list runs failed") {
		t.Fatalf("workflow audit store failure payload = %#v", payload)
	}
}

func workflowAuditSingleStepProfile() profile.Bundle {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
	}
	bundle.Workflows = append(bundle.Workflows, profile.Workflow{ID: "workflow.alpha", DisplayName: "Workflow Alpha"})
	bundle.InterfaceNodes = append(bundle.InterfaceNodes, profile.InterfaceNode{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"})
	bundle.APICases = append(bundle.APICases, profile.APICase{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"})
	bundle.WorkflowBindings = append(bundle.WorkflowBindings, profile.WorkflowBinding{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true})
	return bundle
}

func workflowPlanProfile() profile.Bundle {
	return profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.one", NodeID: "node.alpha", CaseID: "case.alpha", Required: true, SortOrder: 1},
		},
	}
}

func workflowDiscoveryProfile() profile.Bundle {
	return profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha", Description: "Primary smoke path"},
			{ID: "workflow.beta", DisplayName: "Workflow Beta"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.one", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
			{WorkflowID: "workflow.alpha", StepID: "step.two", NodeID: "node.beta", CaseID: "case.beta", Required: true},
		},
	}
}

func replaceWorkflowDiscoveryStoreCatalog(t *testing.T, ctx context.Context, s interface {
	ReplaceProfileCatalog(context.Context, store.ProfileCatalog) error
}) {
	t.Helper()

	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "store-profile",
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.store", DisplayName: "Store Workflow", Description: "Store smoke path"},
			{ID: "workflow.other", DisplayName: "Other Workflow"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.store", StepID: "step.one", NodeID: "node.store", CaseID: "case.store", Required: true},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
}

func recordWorkflowAuditStoreRuns(t *testing.T, ctx context.Context, s interface {
	CreateRun(context.Context, store.Run) (store.Run, error)
	RecordAPICaseRun(context.Context, store.APICaseRun) (store.APICaseRun, error)
}) {
	t.Helper()

	older := time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	for _, item := range []struct {
		id        string
		status    string
		createdAt time.Time
		caseRuns  []store.APICaseRun
	}{
		{
			id:        "run.alpha",
			status:    store.StatusPassed,
			createdAt: older,
			caseRuns: []store.APICaseRun{
				{ID: "run.alpha.case.alpha", CaseID: "case.alpha", Status: store.StatusPassed, CreatedAt: older},
			},
		},
		{
			id:        "run.beta",
			status:    store.StatusFailed,
			createdAt: newer,
			caseRuns: []store.APICaseRun{
				{ID: "run.beta.case.alpha", CaseID: "case.alpha", Status: store.StatusFailed, CreatedAt: newer},
				{ID: "run.beta.case.beta", CaseID: "case.beta", Status: store.StatusPassed, CreatedAt: newer},
			},
		},
	} {
		if _, err := s.CreateRun(ctx, store.Run{
			ID:          item.id,
			ProfileID:   "sample",
			WorkflowID:  "workflow.alpha",
			Status:      item.status,
			SummaryJSON: "{}",
			CreatedAt:   item.createdAt,
			UpdatedAt:   item.createdAt,
		}); err != nil {
			t.Fatalf("create run %s: %v", item.id, err)
		}
		for _, caseRun := range item.caseRuns {
			caseRun.RunID = item.id
			if _, err := s.RecordAPICaseRun(ctx, caseRun); err != nil {
				t.Fatalf("record api case run %s: %v", caseRun.ID, err)
			}
		}
	}
}

func workflowAuditStoreStateProfile() profile.Bundle {
	return profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
			{ID: "node.beta", DisplayName: "Node Beta"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", NodeID: "node.alpha"},
			{ID: "case.beta", NodeID: "node.beta"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
			{WorkflowID: "workflow.alpha", StepID: "step.beta", NodeID: "node.beta", CaseID: "case.beta", Required: false},
		},
	}
}
