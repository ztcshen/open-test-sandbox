package controlplane_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesOpenAPIImportPlanAPI(t *testing.T) {
	specPath := writeOpenAPIImportPlanSpec(t)
	server := httptest.NewServer(controlplane.New(profile.Bundle{ID: "sample"}))
	defer server.Close()

	raw := postPlanAPI(t, server.URL, "/api/template-packages/import-plan/openapi", fmt.Sprintf(`{"sourcePath":%q,"serviceId":"service.catalog","evidenceDir":".runtime/openapi"}`, specPath))
	assertOpenAPIImportPlan(t, decodeOpenAPIImportPlan(t, raw), specPath)
}

func TestServerExposesHTTPCaptureImportPlanAPI(t *testing.T) {
	capturePath := writeHTTPCapturePlanSpec(t)
	server := httptest.NewServer(controlplane.New(profile.Bundle{ID: "sample"}))
	defer server.Close()

	raw := postPlanAPI(t, server.URL, "/api/template-packages/import-plan/http-capture", fmt.Sprintf(`{"sourcePath":%q,"serviceId":"service.catalog","evidenceDir":".runtime/replay"}`, capturePath))
	assertHTTPCaptureImportPlan(t, decodeOpenAPIImportPlan(t, raw), capturePath)
}

func TestServerExposesOpenAPIGenerationPlanAPI(t *testing.T) {
	specPath := writeOpenAPIGenerationPlanSpec(t)
	server := httptest.NewServer(controlplane.New(profile.Bundle{ID: "sample"}))
	defer server.Close()

	raw := postPlanAPI(t, server.URL, "/api/template-packages/generation-plan/openapi", fmt.Sprintf(`{"sourcePath":%q,"serviceId":"service.catalog","evidenceDir":".runtime/generated"}`, specPath))
	assertOpenAPIGenerationPlan(t, decodeOpenAPIGenerationPlan(t, raw), specPath)
}

func TestServerExposesEvidenceListAPI(t *testing.T) {
	server := newEvidenceListServer(t)
	payload := decodeJSONResponse(t, server.URL+"/api/evidence/list?run=run.alpha", http.StatusOK)
	assertEvidenceListPayload(t, payload)
}

func TestServerExposesEvidenceImportAPI(t *testing.T) {
	ctx, s, serverURL := newPlansEvidenceStoreServer(t)
	sourcePath := filepath.Join(t.TempDir(), "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)

	payload := postJSONResponse(t, serverURL+"/api/evidence/import", fmt.Sprintf(`{"sourcePath":%q,"profileId":"sample"}`, sourcePath), http.StatusOK)
	if payload["ok"] != true || payload["sourcePath"] != sourcePath || payload["profileId"] != "sample" {
		t.Fatalf("evidence import payload = %#v", payload)
	}
	if payload["runCount"] != float64(2) || payload["apiCaseRunCount"] != float64(1) || payload["evidenceCount"] != float64(1) {
		t.Fatalf("evidence import counts = %#v", payload)
	}

	run, err := s.GetRun(ctx, "legacy-workflow-7")
	if err != nil {
		t.Fatalf("get imported workflow run: %v", err)
	}
	if run.ProfileID != "sample" || run.WorkflowID != "workflow.alpha" || run.Status != store.StatusPassed {
		t.Fatalf("imported workflow run = %#v", run)
	}
	records, err := s.ListEvidence(ctx, "case-run-parent")
	if err != nil {
		t.Fatalf("list imported evidence: %v", err)
	}
	if len(records) != 1 || records[0].URI != ".runtime/cases/case-run-parent" {
		t.Fatalf("imported evidence = %#v", records)
	}
}

func TestServerExposesBaselineGateAPI(t *testing.T) {
	_, _, serverURL := newPlansEvidenceStoreServer(t)
	payload := postJSONResponse(t, serverURL+"/api/baseline/gate", `{
		"profileId":"sample",
		"subjectId":"workflow.alpha",
		"status":"passed",
		"required":true,
		"summaryJson":"{\"source\":\"api\"}"
	}`, http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("baseline set payload should expose ok envelope: %#v", payload)
	}
	gate := payload["baselineGate"].(map[string]any)
	if gate["profileId"] != "sample" || gate["subjectId"] != "workflow.alpha" || gate["status"] != "passed" || gate["required"] != true {
		t.Fatalf("baseline set gate = %#v", gate)
	}
	if gate["summaryJson"] != `{"source":"api"}` {
		t.Fatalf("baseline set summary = %#v", gate)
	}

	loaded := decodeJSONResponse(t, serverURL+"/api/baseline/gate?profileId=sample&subjectId=workflow.alpha", http.StatusOK)
	loadedGate := loaded["baselineGate"].(map[string]any)
	if loaded["ok"] != true || loadedGate["status"] != "passed" || loadedGate["required"] != true {
		t.Fatalf("baseline get payload = %#v", loaded)
	}

	missing := decodeJSONResponse(t, serverURL+"/api/baseline/gate?profileId=sample&subjectId=workflow.missing", http.StatusNotFound)
	if missing["ok"] != false || !strings.Contains(fmt.Sprint(missing["error"]), "baseline gate not found") {
		t.Fatalf("missing baseline payload = %#v", missing)
	}
}

func newPlansEvidenceStoreServer(t *testing.T) (context.Context, store.Store, string) {
	t.Helper()

	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	t.Cleanup(server.Close)
	t.Cleanup(func() { _ = s.Close() })
	return ctx, s, server.URL
}

func TestServerExposesTemplateRenderAPI(t *testing.T) {
	server := httptest.NewServer(controlplane.New(profile.Bundle{
		ID: "sample",
		RequestTemplates: []profile.RequestTemplate{
			{
				ID:           "template.create",
				Method:       "POST",
				Path:         "/v1/items/{{.itemId}}",
				TemplateJSON: `{"id":"{{.itemId}}","quantity":{{.quantity}}}`,
			},
		},
		Fixtures: []profile.Fixture{
			{
				ID:       "fixture.item",
				DataJSON: `{"itemId":"item-001","quantity":3}`,
			},
		},
	}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/template/render", `{"templateId":"template.create","fixtureId":"fixture.item"}`, http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("template render payload should expose ok envelope: %#v", payload)
	}
	rendered := payload["request"].(map[string]any)
	body := rendered["body"].(map[string]any)
	if rendered["method"] != "POST" || rendered["path"] != "/v1/items/item-001" {
		t.Fatalf("rendered request identity = %#v", rendered)
	}
	if body["id"] != "item-001" || body["quantity"] != float64(3) {
		t.Fatalf("rendered request body = %#v", body)
	}
}
