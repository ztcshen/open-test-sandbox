package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesOpenAPIImportPlanAPI(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "catalog-openapi.json")
	if err := os.WriteFile(specPath, []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Catalog API"},
  "paths": {
    "/items": {
      "get": {
        "operationId": "listItems",
        "summary": "List items",
        "tags": ["catalog"],
        "responses": {"200": {"description": "OK"}}
      },
      "post": {
        "operationId": "createItem",
        "summary": "Create item",
        "tags": ["catalog", "write"],
        "requestBody": {
          "content": {
            "application/json": {
              "example": {"id": "item-001", "name": "Example Item"}
            }
          }
        },
        "responses": {"201": {"description": "Created"}}
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write openapi spec: %v", err)
	}
	server := httptest.NewServer(controlplane.New(profile.Bundle{ID: "sample"}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/template-packages/import-plan/openapi", "application/json", strings.NewReader(fmt.Sprintf(`{"sourcePath":%q,"serviceId":"service.catalog","evidenceDir":".runtime/openapi"}`, specPath)))
	if err != nil {
		t.Fatalf("post import plan: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read import plan: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import plan status = %d body=%s", resp.StatusCode, raw)
	}
	var payload struct {
		OK         bool   `json:"ok"`
		Kind       string `json:"kind"`
		SourcePath string `json:"sourcePath"`
		Plan       struct {
			Service struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"service"`
			InterfaceNodes []struct {
				ID     string `json:"id"`
				Method string `json:"method"`
				Path   string `json:"path"`
				Status string `json:"status"`
			} `json:"interfaceNodes"`
			APICases []struct {
				ID          string   `json:"id"`
				CasePath    string   `json:"casePath"`
				EvidenceDir string   `json:"evidenceDir"`
				Tags        []string `json:"tags"`
				Status      string   `json:"status"`
			} `json:"apiCases"`
			CaseFiles []struct {
				Path string          `json:"path"`
				Body json.RawMessage `json:"body"`
			} `json:"caseFiles"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode import plan: %v body=%s", err, raw)
	}
	if !payload.OK || payload.Kind != "openapi" || payload.SourcePath != specPath || payload.Plan.Service.ID != "service.catalog" || payload.Plan.Service.Status != "draft" {
		t.Fatalf("import plan summary = %#v", payload)
	}
	if len(payload.Plan.InterfaceNodes) != 2 || len(payload.Plan.APICases) != 2 || len(payload.Plan.CaseFiles) != 2 {
		t.Fatalf("import plan counts = nodes:%d cases:%d files:%d", len(payload.Plan.InterfaceNodes), len(payload.Plan.APICases), len(payload.Plan.CaseFiles))
	}
	if payload.Plan.InterfaceNodes[0].ID != "node.service.catalog.list-items" || payload.Plan.InterfaceNodes[0].Method != "GET" || payload.Plan.InterfaceNodes[0].Path != "/items" || payload.Plan.InterfaceNodes[0].Status != "draft" {
		t.Fatalf("first node = %#v", payload.Plan.InterfaceNodes[0])
	}
	if payload.Plan.APICases[1].ID != "case.service.catalog.create-item" || payload.Plan.APICases[1].CasePath != "api-cases/case.service.catalog.create-item.json" || payload.Plan.APICases[1].EvidenceDir != ".runtime/openapi" || strings.Join(payload.Plan.APICases[1].Tags, ",") != "openapi,catalog,write" {
		t.Fatalf("second case = %#v", payload.Plan.APICases[1])
	}
}

func TestServerExposesHTTPCaptureImportPlanAPI(t *testing.T) {
	capturePath := filepath.Join(t.TempDir(), "traffic.json")
	if err := os.WriteFile(capturePath, []byte(`{
  "name": "Catalog Traffic",
  "captures": [
    {
      "id": "createItem",
      "name": "Create item from traffic",
      "request": {
        "method": "POST",
        "path": "/items",
        "headers": {"Content-Type": "application/json"},
        "body": {"id": "item-001", "name": "Example"}
      },
      "response": {"status": 201, "body": {"id": "item-001"}}
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	server := httptest.NewServer(controlplane.New(profile.Bundle{ID: "sample"}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/template-packages/import-plan/http-capture", "application/json", strings.NewReader(fmt.Sprintf(`{"sourcePath":%q,"serviceId":"service.catalog","evidenceDir":".runtime/replay"}`, capturePath)))
	if err != nil {
		t.Fatalf("post capture plan: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read capture plan: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("capture plan status = %d body=%s", resp.StatusCode, raw)
	}
	var payload struct {
		OK         bool   `json:"ok"`
		Kind       string `json:"kind"`
		SourcePath string `json:"sourcePath"`
		Plan       struct {
			Service struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"service"`
			InterfaceNodes []struct {
				ID     string `json:"id"`
				Method string `json:"method"`
				Path   string `json:"path"`
			} `json:"interfaceNodes"`
			APICases []struct {
				ID          string   `json:"id"`
				CasePath    string   `json:"casePath"`
				EvidenceDir string   `json:"evidenceDir"`
				Tags        []string `json:"tags"`
			} `json:"apiCases"`
			CaseFiles []struct {
				Path string          `json:"path"`
				Body json.RawMessage `json:"body"`
			} `json:"caseFiles"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode capture plan: %v body=%s", err, raw)
	}
	if !payload.OK || payload.Kind != "http-capture" || payload.SourcePath != capturePath || payload.Plan.Service.ID != "service.catalog" || payload.Plan.Service.Status != "draft" {
		t.Fatalf("capture plan summary = %#v", payload)
	}
	if len(payload.Plan.InterfaceNodes) != 1 || len(payload.Plan.APICases) != 1 || len(payload.Plan.CaseFiles) != 1 {
		t.Fatalf("capture plan counts = nodes:%d cases:%d files:%d", len(payload.Plan.InterfaceNodes), len(payload.Plan.APICases), len(payload.Plan.CaseFiles))
	}
	if payload.Plan.InterfaceNodes[0].ID != "node.service.catalog.create-item" || payload.Plan.InterfaceNodes[0].Method != "POST" || payload.Plan.InterfaceNodes[0].Path != "/items" {
		t.Fatalf("capture node = %#v", payload.Plan.InterfaceNodes[0])
	}
	if payload.Plan.APICases[0].ID != "case.service.catalog.create-item" || payload.Plan.APICases[0].CasePath != "api-cases/case.service.catalog.create-item.json" || payload.Plan.APICases[0].EvidenceDir != ".runtime/replay" || strings.Join(payload.Plan.APICases[0].Tags, ",") != "recorded,replay" {
		t.Fatalf("capture case = %#v", payload.Plan.APICases[0])
	}
}

func TestServerExposesOpenAPIGenerationPlanAPI(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "catalog-openapi.json")
	if err := os.WriteFile(specPath, []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Catalog API"},
  "paths": {
    "/items": {
      "post": {
        "operationId": "createItem",
        "summary": "Create item",
        "tags": ["catalog"],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["id"],
                "properties": {
                  "id": {"type": "string", "example": "item-001"},
                  "name": {"type": "string", "example": "Example Item"}
                }
              }
            }
          }
        },
        "responses": {
          "201": {"description": "Created"},
          "400": {"description": "Bad request"}
        }
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write openapi spec: %v", err)
	}
	server := httptest.NewServer(controlplane.New(profile.Bundle{ID: "sample"}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/template-packages/generation-plan/openapi", "application/json", strings.NewReader(fmt.Sprintf(`{"sourcePath":%q,"serviceId":"service.catalog","evidenceDir":".runtime/generated"}`, specPath)))
	if err != nil {
		t.Fatalf("post generation plan: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read generation plan: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generation plan status = %d body=%s", resp.StatusCode, raw)
	}
	var payload struct {
		OK         bool   `json:"ok"`
		Kind       string `json:"kind"`
		SourcePath string `json:"sourcePath"`
		Plan       struct {
			OK         bool `json:"ok"`
			Candidates []struct {
				ID     string `json:"id"`
				Kind   string `json:"kind"`
				Field  string `json:"field"`
				CaseID string `json:"caseId"`
			} `json:"candidates"`
			APICases []struct {
				ID          string   `json:"id"`
				Status      string   `json:"status"`
				CasePath    string   `json:"casePath"`
				EvidenceDir string   `json:"evidenceDir"`
				Tags        []string `json:"tags"`
			} `json:"apiCases"`
			CaseFiles []struct {
				Path string          `json:"path"`
				Body json.RawMessage `json:"body"`
			} `json:"caseFiles"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode generation plan: %v body=%s", err, raw)
	}
	if !payload.OK || payload.Kind != "openapi" || payload.SourcePath != specPath || !payload.Plan.OK || len(payload.Plan.Candidates) != 1 || len(payload.Plan.APICases) != 1 || len(payload.Plan.CaseFiles) != 1 {
		t.Fatalf("generation plan summary = %#v", payload)
	}
	if payload.Plan.Candidates[0].Kind != "missing-required-field" || payload.Plan.Candidates[0].Field != "id" || payload.Plan.Candidates[0].CaseID != "case.service.catalog.create-item.missing-id" {
		t.Fatalf("generation candidate = %#v", payload.Plan.Candidates[0])
	}
	if payload.Plan.APICases[0].Status != "draft" || payload.Plan.APICases[0].EvidenceDir != ".runtime/generated" || strings.Join(payload.Plan.APICases[0].Tags, ",") != "generated,schema,negative,catalog" {
		t.Fatalf("generated api case = %#v", payload.Plan.APICases[0])
	}
}

func TestServerExposesEvidenceListAPI(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
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
		RequestSummaryJSON:   `{"method":"GET","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"passed"}`,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         "run.alpha.response",
		RunID:      "run.alpha",
		CaseRunID:  "run.alpha.case",
		StepID:     "step.alpha",
		Kind:       "response",
		URI:        ".runtime/evidence/run.alpha/response.json",
		MediaType:  "application/json",
		SHA256:     "sha256-alpha",
		SizeBytes:  42,
		Summary:    `{"statusCode":200}`,
		Category:   "http-response",
		Visibility: "public",
		LabelsJSON: `{"stepId":"step.alpha"}`,
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/evidence/list?run=run.alpha", http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("evidence list payload should expose ok envelope: %#v", payload)
	}
	runs := payload["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("evidence list runs = %#v", runs)
	}
	run := runs[0].(map[string]any)
	if run["id"] != "run.alpha" || run["profileId"] != "sample" || run["workflowId"] != "workflow.alpha" {
		t.Fatalf("evidence list run identity = %#v", run)
	}
	if run["apiCaseRunCount"] != float64(1) || run["evidenceCount"] != float64(1) {
		t.Fatalf("evidence list counts = %#v", run)
	}
	records := run["evidenceRecords"].([]any)
	if len(records) != 1 {
		t.Fatalf("evidence records = %#v", records)
	}
	record := records[0].(map[string]any)
	if _, ok := record["Kind"]; ok {
		t.Fatalf("evidence record should not leak Go field names: %#v", record)
	}
	if record["id"] != "run.alpha.response" || record["runId"] != "run.alpha" || record["caseRunId"] != "run.alpha.case" || record["stepId"] != "step.alpha" || record["kind"] != "response" {
		t.Fatalf("evidence record identity = %#v", record)
	}
	if record["mediaType"] != "application/json" || record["sha256"] != "sha256-alpha" || record["sizeBytes"] != float64(42) {
		t.Fatalf("evidence record metadata = %#v", record)
	}
	if record["category"] != "http-response" || record["visibility"] != "public" {
		t.Fatalf("evidence record attachment classification = %#v", record)
	}
	labels := record["labels"].(map[string]any)
	if labels["stepId"] != "step.alpha" {
		t.Fatalf("evidence record labels = %#v", labels)
	}
}

func TestServerExposesEvidenceImportAPI(t *testing.T) {
	ctx := context.Background()
	sourcePath := filepath.Join(t.TempDir(), "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/evidence/import", fmt.Sprintf(`{"sourcePath":%q,"profileId":"sample"}`, sourcePath), http.StatusOK)
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
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/baseline/gate", `{
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

	loaded := decodeJSONResponse(t, server.URL+"/api/baseline/gate?profileId=sample&subjectId=workflow.alpha", http.StatusOK)
	loadedGate := loaded["baselineGate"].(map[string]any)
	if loaded["ok"] != true || loadedGate["status"] != "passed" || loadedGate["required"] != true {
		t.Fatalf("baseline get payload = %#v", loaded)
	}

	missing := decodeJSONResponse(t, server.URL+"/api/baseline/gate?profileId=sample&subjectId=workflow.missing", http.StatusNotFound)
	if missing["ok"] != false || !strings.Contains(fmt.Sprint(missing["error"]), "baseline gate not found") {
		t.Fatalf("missing baseline payload = %#v", missing)
	}
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
