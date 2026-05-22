package controlplane_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"open-test-sandbox/internal/controlplane"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/profilehome"
	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
)

func TestServerExposesProfileAPI(t *testing.T) {
	bundle := loadEmptyProfile(t)
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/template-packages/current")
	if err != nil {
		t.Fatalf("get template package current api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("template package current api status = %d", resp.StatusCode)
	}

	var payload struct {
		TemplatePackageID string         `json:"templatePackageId"`
		ID                string         `json:"id"`
		DisplayName       string         `json:"displayName"`
		Counts            profile.Counts `json:"counts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode template package current api: %v", err)
	}
	if payload.TemplatePackageID != "empty" || payload.ID != "empty" || payload.DisplayName != "Empty Profile" || payload.Counts.Workflows != 0 {
		t.Fatalf("template package current api payload = %#v", payload)
	}
}

func TestServerExposesExecutorPlanAPI(t *testing.T) {
	server := httptest.NewServer(controlplane.New(profile.Bundle{
		ID: "sample",
		Executors: []profile.ExecutorDescriptor{
			{ID: "executor.command", DisplayName: "No-op command", Kind: "custom-command", Command: "true", Status: "active"},
			{ID: "executor.pytest", DisplayName: "Pytest suite", Kind: "pytest", Status: "active"},
		},
	}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/executor/plan", http.StatusOK)
	if payload["ok"] != false || payload["profileId"] != "sample" {
		t.Fatalf("executor plan payload = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(2) || counts["ready"] != float64(1) || counts["blocked"] != float64(1) {
		t.Fatalf("executor plan counts = %#v", counts)
	}
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("executor plan items = %#v", items)
	}
	first := items[0].(map[string]any)
	second := items[1].(map[string]any)
	if first["id"] != "executor.command" || first["ready"] != true || first["runMode"] != "dry-run" {
		t.Fatalf("ready executor item = %#v", first)
	}
	issues := second["issues"].([]any)
	if second["id"] != "executor.pytest" || second["ready"] != false || len(issues) != 1 || issues[0] != "missing-source-path" {
		t.Fatalf("blocked executor item = %#v", second)
	}
}

func TestServerExecutorPlanPrefersStoreCatalog(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "executor.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		APICases: []store.CatalogAPICase{
			{ID: "case.catalog", DisplayName: "Catalog Case", SourceKind: "karate", SourcePath: "tests/catalog.feature", ExecutorID: "executor.catalog", Status: "active", TimeoutSeconds: 9},
		},
	}); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{
		ID: "bundle-only",
		Executors: []profile.ExecutorDescriptor{
			{ID: "executor.bundle", Kind: "custom-command", Command: "true", Status: "active"},
		},
	}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/executor/plan", http.StatusOK)
	if payload["ok"] != true || payload["profileId"] != "current" {
		t.Fatalf("store executor plan payload = %#v", payload)
	}
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("store executor plan items = %#v", items)
	}
	item := items[0].(map[string]any)
	if item["id"] != "executor.catalog" || item["kind"] != "karate" || item["sourcePath"] != "tests/catalog.feature" || item["ready"] != true || item["timeoutSeconds"] != float64(9) {
		t.Fatalf("store executor plan item = %#v", item)
	}
}

func TestServerExposesCurrentStoreAPIWithMaskedURL(t *testing.T) {
	tests := []struct {
		name    string
		info    controlplane.StoreInfo
		wantURL string
	}{
		{
			name: "postgres",
			info: controlplane.StoreInfo{
				Configured: true,
				Name:       "team-verified",
				Backend:    "postgres",
				URL:        "postgres://tester:xxxxx@example.com:5432/team_verified?sslmode=require",
				Source:     "active-config",
			},
			wantURL: "postgres://tester:xxxxx@example.com:5432/team_verified?sslmode=require",
		},
		{
			name: "mysql",
			info: controlplane.StoreInfo{
				Configured: true,
				Name:       "team-mysql",
				Backend:    "mysql",
				URL:        "mysql://tester:xxxxx@example.com:3306/otsandbox_team?tls=false",
				Source:     "active-config",
			},
			wantURL: "mysql://tester:xxxxx@example.com:3306/otsandbox_team?tls=false",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{
				StoreInfo: tt.info,
			}))
			defer server.Close()

			payload := decodeJSONResponse(t, server.URL+"/api/store/current", http.StatusOK)
			if payload["ok"] != true || payload["configured"] != true {
				t.Fatalf("store current flags = %#v", payload)
			}
			if payload["name"] != tt.info.Name || payload["backend"] != tt.info.Backend || payload["source"] != "active-config" {
				t.Fatalf("store current metadata = %#v", payload)
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			if strings.Contains(string(raw), "secret") || payload["url"] != tt.wantURL {
				t.Fatalf("store current url was not masked: %s", raw)
			}
		})
	}
}

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

func TestServerExposesProfileAssetLists(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{
				ID:             "case.alpha",
				DisplayName:    "Case Alpha",
				NodeID:         "node.alpha",
				CasePath:       "cases/case.alpha.json",
				SourceKind:     "karate",
				SourcePath:     "tests/api.feature",
				ExecutorID:     "executor.karate",
				BaseURL:        "http://127.0.0.1:18080",
				EvidenceDir:    ".runtime/cases",
				TimeoutSeconds: 30,
				DefaultOverrides: map[string]any{
					"itemId": "item-001",
				},
			},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/template-packages/assets")
	if err != nil {
		t.Fatalf("get template package assets api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("template package assets api status = %d", resp.StatusCode)
	}

	var payload struct {
		TemplatePackageID string                  `json:"templatePackageId"`
		Services          []profile.Service       `json:"services"`
		Workflows         []profile.Workflow      `json:"workflows"`
		InterfaceNodes    []profile.InterfaceNode `json:"interfaceNodes"`
		APICases          []profile.APICase       `json:"apiCases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode template package assets api: %v", err)
	}
	if payload.TemplatePackageID != "sample" {
		t.Fatalf("template package assets identity = %#v", payload)
	}
	if len(payload.Services) != 1 || payload.Services[0].ID != "service.alpha" {
		t.Fatalf("services payload = %#v", payload.Services)
	}
	if len(payload.Workflows) != 1 || payload.Workflows[0].ID != "workflow.alpha" {
		t.Fatalf("workflows payload = %#v", payload.Workflows)
	}
	if len(payload.InterfaceNodes) != 1 || payload.InterfaceNodes[0].ServiceID != "service.alpha" {
		t.Fatalf("interface nodes payload = %#v", payload.InterfaceNodes)
	}
	if len(payload.APICases) != 1 || payload.APICases[0].NodeID != "node.alpha" {
		t.Fatalf("api cases payload = %#v", payload.APICases)
	}
}

func TestServerImportsProfileBundleIntoRuntimeStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()
	profileDir := writeEmptyProfileBundle(t)

	payload := postJSONResponse(t, server.URL+"/api/profile/import", fmt.Sprintf(`{"path":%q}`, profileDir), http.StatusOK)

	if payload["profileId"] != "empty" || payload["bundlePath"] != profileDir {
		t.Fatalf("import payload identity = %#v", payload)
	}
	if digest, ok := payload["bundleDigest"].(string); !ok || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("import payload digest = %#v", payload["bundleDigest"])
	}
	if payload["importedAt"] == "" {
		t.Fatalf("import payload importedAt = %#v", payload)
	}
	counts, ok := payload["counts"].(map[string]any)
	if !ok || counts["services"] != float64(0) || counts["apiCases"] != float64(0) {
		t.Fatalf("import payload counts = %#v", payload["counts"])
	}
	indexedStore, ok := payload["store"].(map[string]any)
	if !ok || indexedStore["profileId"] != "empty" {
		t.Fatalf("import payload store = %#v", payload["store"])
	}
	readModels, ok := payload["readModels"].([]any)
	if !ok || fmt.Sprint(readModels) != "[interface-nodes catalog dashboard]" {
		t.Fatalf("import payload read models = %#v", payload["readModels"])
	}
	index, err := s.GetProfileIndex(ctx, "empty")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if index.BundlePath != profileDir || !strings.HasPrefix(index.BundleDigest, "sha256:") || index.ImportedAt.IsZero() {
		t.Fatalf("profile index = %#v", index)
	}
}

func TestServerTemplatePackageAliasesImportIntoRuntimeStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()
	profileDir := writeEmptyProfileBundle(t)

	payload := postJSONResponse(t, server.URL+"/api/template-packages/import", fmt.Sprintf(`{"templatePackagePath":%q}`, profileDir), http.StatusOK)

	if payload["templatePackageId"] != "empty" || payload["templatePackagePath"] != profileDir {
		t.Fatalf("template package import payload identity = %#v", payload)
	}
	if payload["profileId"] != "empty" || payload["bundlePath"] != profileDir {
		t.Fatalf("legacy profile import fields should remain available: %#v", payload)
	}
	catalogIndex := decodeJSONResponse(t, server.URL+"/api/template-packages/catalog-index", http.StatusOK)
	if catalogIndex["profileId"] != "empty" || catalogIndex["indexedAt"] == "" {
		t.Fatalf("template package catalog index = %#v", catalogIndex)
	}
	verify := postJSONResponse(t, server.URL+"/api/template-packages/verify", fmt.Sprintf(`{"templatePackagePath":%q}`, profileDir), http.StatusOK)
	if verify["templatePackageId"] != "empty" || verify["ok"] != true {
		t.Fatalf("template package verify payload = %#v", verify)
	}
	repairPlan := postJSONResponse(t, server.URL+"/api/template-packages/audit-plan", fmt.Sprintf(`{"templatePackagePath":%q}`, profileDir), http.StatusOK)
	if repairPlan["profileId"] != "empty" {
		t.Fatalf("template package audit plan payload = %#v", repairPlan)
	}
}

func TestServerTemplatePackageInstallAcceptsTemplatePackagePath(t *testing.T) {
	profileHome := t.TempDir()
	sourceDir := writeEmptyProfileBundle(t)
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{ProfileHome: profileHome}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/template-packages/install", `{"templatePackagePath":`+mustJSON(t, sourceDir)+`}`, http.StatusOK)
	if payload["templatePackageId"] != "empty" || payload["id"] != "empty" {
		t.Fatalf("template package install payload = %#v", payload)
	}
	list := decodeJSONResponse(t, server.URL+"/api/template-packages/installed", http.StatusOK)
	if list["templatePackageHome"] != profileHome {
		t.Fatalf("template package home = %#v", list)
	}
	items := list["templatePackages"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["templatePackageId"] != "empty" {
		t.Fatalf("template package list = %#v", list)
	}
}

func TestServerProfileImportSwitchesActiveProfile(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sandbox.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeWorkbenchSampleProfile(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	initial := decodeJSONResponse(t, server.URL+"/api/profile", http.StatusOK)
	if initial["id"] != "empty" {
		t.Fatalf("initial profile = %#v", initial)
	}

	postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`+mustJSON(t, profileDir)+`}`, http.StatusOK)

	active := decodeJSONResponse(t, server.URL+"/api/profile", http.StatusOK)
	if active["id"] != "sample" || active["displayName"] != "Sample Profile" {
		t.Fatalf("active profile after import = %#v", active)
	}
	catalog := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	services, ok := catalog["services"].([]any)
	if !ok || len(services) != 1 {
		t.Fatalf("catalog after import = %#v", catalog)
	}
	service, ok := services[0].(map[string]any)
	if !ok || service["id"] != "service.alpha" {
		t.Fatalf("catalog service after import = %#v", services)
	}
	nodes := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	items, ok := nodes["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("interface nodes after import = %#v", nodes)
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["id"] != "node.alpha" {
		t.Fatalf("interface node after import = %#v", items)
	}
	nodeDetail := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	nodeSource, ok := nodeDetail["source"].(map[string]any)
	if !ok || nodeSource["kind"] != "read-model" || nodeSource["id"] != "sample" {
		t.Fatalf("interface node detail source = %#v", nodeDetail["source"])
	}
	nodeCases, ok := nodeDetail["cases"].([]any)
	if !ok || len(nodeCases) != 1 {
		t.Fatalf("interface node detail cases = %#v", nodeDetail["cases"])
	}
	coverage := decodeJSONResponse(t, server.URL+"/api/interface-node/coverage?workflow=workflow.alpha", http.StatusOK)
	coverageSource, ok := coverage["source"].(map[string]any)
	if !ok || coverageSource["kind"] != "read-model" || coverageSource["id"] != "sample" {
		t.Fatalf("interface node coverage source = %#v", coverage["source"])
	}
	catalogIndex := decodeJSONResponse(t, server.URL+"/api/profile/catalog-index", http.StatusOK)
	if catalogIndex["profileId"] != "sample" || catalogIndex["indexedAt"] == "" {
		t.Fatalf("catalog index identity = %#v", catalogIndex)
	}
	catalogCounts, ok := catalogIndex["counts"].(map[string]any)
	if !ok || catalogCounts["services"] != float64(1) || catalogCounts["workflows"] != float64(1) || catalogCounts["templates"] != float64(1) {
		t.Fatalf("catalog index counts = %#v", catalogIndex["counts"])
	}
	configVersion, ok := catalogIndex["configVersion"].(map[string]any)
	if !ok || configVersion["profileId"] != "sample" || configVersion["bundleDigest"] == "" || configVersion["active"] != true {
		t.Fatalf("catalog index config version = %#v", catalogIndex["configVersion"])
	}
	if got := sqliteCountRows(t, dbPath, "config_read_model"); got != 6 {
		t.Fatalf("config_read_model count = %d, want 6", got)
	}
	if dashboardModel, err := s.GetReadModel(ctx, "sample", controlplane.ReadModelDashboard); err != nil {
		t.Fatalf("get dashboard read model: %v", err)
	} else if !strings.Contains(dashboardModel.PayloadJSON, `"source"`) || !strings.Contains(dashboardModel.PayloadJSON, `"groups"`) {
		t.Fatalf("dashboard read model payload = %s", dashboardModel.PayloadJSON)
	}
	for table, want := range map[string]int{
		"template":                1,
		"template_config":         1,
		"node_config":             1,
		"workflow":                1,
		"interface_node":          1,
		"interface_node_case":     1,
		"workflow_interface_node": 1,
	} {
		if got := sqliteCountRows(t, dbPath, table); got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
}

func TestServerRegistersServiceIntoSandboxStoreWithoutProfileImport(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sandbox.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/sandbox/services", `{
		"id":"service.gateway",
		"displayName":"Gateway",
		"kind":"http",
		"servicePort":18181,
		"healthUrl":"http://127.0.0.1:18181/health",
		"status":"active"
	}`, http.StatusOK)

	if payload["ok"] != true {
		t.Fatalf("service registration payload = %#v", payload)
	}
	service, ok := payload["service"].(map[string]any)
	if !ok || service["id"] != "service.gateway" || service["servicePort"] != float64(18181) {
		t.Fatalf("registered service payload = %#v", payload["service"])
	}
	if payload["storeId"] != "current" {
		t.Fatalf("store identity should be current, got %#v", payload["storeId"])
	}

	catalog, err := s.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get store catalog: %v", err)
	}
	if catalog.ProfileID != "current" || len(catalog.Services) != 1 {
		t.Fatalf("catalog after service registration = %#v", catalog)
	}
	if catalog.Services[0].ID != "service.gateway" || catalog.Services[0].HealthURL != "http://127.0.0.1:18181/health" {
		t.Fatalf("registered catalog service = %#v", catalog.Services[0])
	}

	catalogPayload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	services, ok := catalogPayload["services"].([]any)
	if !ok || len(services) != 1 {
		t.Fatalf("catalog payload services = %#v", catalogPayload["services"])
	}
	first, ok := services[0].(map[string]any)
	if !ok || first["id"] != "service.gateway" {
		t.Fatalf("catalog payload first service = %#v", services[0])
	}
}

func TestServerRegistersInterfaceIntoSandboxStoreWithoutProfileImport(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sandbox.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		Services: []store.CatalogService{
			{ID: "service.gateway", DisplayName: "Gateway", Kind: "http", ServicePort: 18181, Status: "active"},
		},
	}); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/sandbox/interfaces", `{
		"id":"interface.create",
		"displayName":"Create API",
		"serviceId":"service.gateway",
		"operation":"item.create",
		"method":"POST",
		"path":"/v1/items",
		"timeoutMs":1200,
		"requestTemplate":{"id":"template.create","templateJson":{"body":{"name":"{{override:name|demo}}"}}},
		"case":{
			"id":"case.create.default",
			"displayName":"Create default",
			"caseType":"success",
			"requiredForAdmission":true,
			"expectedJson":{"ok":true},
			"timeoutSeconds":5
		},
		"caseExecution":{
			"method":"POST",
			"path":"/v1/items",
			"body":{"name":"{{override:name|demo}}"},
			"expectedHttpCodes":[200]
		}
	}`, http.StatusOK)

	if payload["ok"] != true || payload["storeId"] != "current" {
		t.Fatalf("interface registration payload = %#v", payload)
	}
	view := payload["interface"].(map[string]any)
	if view["id"] != "interface.create" || view["serviceId"] != "service.gateway" || view["caseId"] != "case.create.default" {
		t.Fatalf("interface registration view = %#v", view)
	}

	catalog, err := s.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get catalog: %v", err)
	}
	if len(catalog.Services) != 1 || catalog.Services[0].ID != "service.gateway" {
		t.Fatalf("service registration should be preserved: %#v", catalog.Services)
	}
	if len(catalog.InterfaceNodes) != 1 || catalog.InterfaceNodes[0].ID != "interface.create" || catalog.InterfaceNodes[0].ServiceID != "service.gateway" {
		t.Fatalf("registered interface node = %#v", catalog.InterfaceNodes)
	}
	if len(catalog.RequestTemplates) != 1 || catalog.RequestTemplates[0].ID != "template.create" || catalog.RequestTemplates[0].NodeID != "interface.create" {
		t.Fatalf("registered request templates = %#v", catalog.RequestTemplates)
	}
	if len(catalog.APICases) != 1 || catalog.APICases[0].ID != "case.create.default" || catalog.APICases[0].NodeID != "interface.create" || !catalog.APICases[0].RequiredForAdmission {
		t.Fatalf("registered api cases = %#v", catalog.APICases)
	}
	executionConfigFound := false
	for _, config := range catalog.TemplateConfigs {
		if config.WorkflowID == "" && config.ScopeType == "case" && config.ScopeID == "case.create.default" && strings.Contains(config.ConfigJSON, `"caseExecution"`) {
			executionConfigFound = true
		}
	}
	if !executionConfigFound {
		t.Fatalf("registered execution config = %#v", catalog.TemplateConfigs)
	}

	catalogPayload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	cases, ok := catalogPayload["apiCases"].([]any)
	if !ok || len(cases) != 1 || cases[0].(map[string]any)["id"] != "case.create.default" {
		t.Fatalf("catalog payload cases = %#v", catalogPayload["apiCases"])
	}
	detail := decodeJSONResponse(t, server.URL+"/api/interface-node?id=interface.create", http.StatusOK)
	if detail["ok"] != true || detail["requested"] != "interface.create" {
		t.Fatalf("interface detail payload = %#v", detail)
	}
}

func TestServerManagesVerifiedEnvironmentCatalogFromStore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sandbox.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	missingWorkflow := postJSONResponse(t, server.URL+"/api/environments", `{"id":"env.no-workflow"}`, http.StatusBadRequest)
	if !strings.Contains(fmt.Sprint(missingWorkflow["error"]), "verificationWorkflowId") {
		t.Fatalf("register without verification workflow should be denied: %#v", missingWorkflow)
	}

	registered := postJSONResponse(t, server.URL+"/api/environments", `{
  "id": "env.team.api",
  "displayName": "Team API Environment",
  "description": "Accepted local Docker environment",
  "services": [{"id":"entry-gateway","repo":"../entry-gateway"}],
  "repos": {"entry-gateway":{"url":"../entry-gateway","branch":"main","checkout":"/tmp/entry-gateway"}},
  "compose": {"composeFile":"docker-compose.yml","startCommand":"docker compose up -d"},
  "healthChecks": [{"id":"retail-health","url":"http://127.0.0.1:18080/health"}],
  "verificationWorkflowId": "workflow.core-10"
}`, http.StatusOK)
	env := registered["environment"].(map[string]any)
	if env["id"] != "env.team.api" || env["status"] != "draft" || env["verified"] != false || env["verificationWorkflowId"] != "workflow.core-10" {
		t.Fatalf("registered environment = %#v", env)
	}

	discover := decodeJSONResponse(t, server.URL+"/api/environments", http.StatusOK)
	if discover["count"] != float64(0) {
		t.Fatalf("unverified environment should stay out of default API discovery: %#v", discover)
	}
	discoverAll := decodeJSONResponse(t, server.URL+"/api/environments?all=true", http.StatusOK)
	if discoverAll["count"] != float64(1) {
		t.Fatalf("all API discovery = %#v", discoverAll)
	}

	denied := postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(denied["error"]), "not publishable") {
		t.Fatalf("publish denied payload = %#v", denied)
	}

	verified := postJSONResponse(t, server.URL+"/api/environments/env.team.api/verify", `{
  "runId": "run.core-10",
  "status": "passed",
  "evidenceComplete": true,
  "topologyComplete": true
}`, http.StatusOK)
	verifiedEnv := verified["environment"].(map[string]any)
	if verifiedEnv["status"] != "verified-ready" || verifiedEnv["lastVerificationRunId"] != "run.core-10" || verifiedEnv["evidenceComplete"] != true || verifiedEnv["topologyComplete"] != true {
		t.Fatalf("verified environment = %#v", verifiedEnv)
	}

	now := time.Now().UTC()
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.core-10",
		ProfileID:  "sample",
		WorkflowID: "workflow.core-10",
		Status:     store.StatusPassed,
		StartedAt:  now.Add(-time.Second),
		FinishedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed verification run: %v", err)
	}
	stillDenied := postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(stillDenied["error"]), "no indexed Evidence") {
		t.Fatalf("publish without verification artifacts should be denied: %#v", stillDenied)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         "run.core-10.summary",
		RunID:      "run.core-10",
		Kind:       "summary",
		URI:        "store://verification/run.core-10/summary.json",
		MediaType:  "application/json",
		SHA256:     "verification-summary-sha256",
		SizeBytes:  2,
		Summary:    `{"status":"passed"}`,
		Category:   "verification",
		Visibility: "internal",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("seed verification Evidence: %v", err)
	}
	noTopology := postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(noTopology["error"]), "no complete SkyWalking topology") {
		t.Fatalf("publish without topology should be denied: %#v", noTopology)
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "run.core-10.topology.skywalking",
		WorkflowRunID: "run.core-10",
		WorkflowID:    "workflow.core-10",
		StepID:        "step.core-10",
		CaseID:        "case.core-10",
		RequestID:     "request.core-10",
		TraceID:       "trace.core-10",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","traceId":"trace.core-10","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("seed verification topology: %v", err)
	}
	stillDenied = postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(stillDenied["error"]), "acceptance report") {
		t.Fatalf("publish without workflow acceptance report should be denied: %#v", stillDenied)
	}
	acceptedRunID := "run.core-10.accepted"
	verified = postJSONResponse(t, server.URL+"/api/environments/env.team.api/verify", `{
  "runId": "run.core-10.accepted",
  "status": "passed",
  "evidenceComplete": true,
  "topologyComplete": true
}`, http.StatusOK)
	verifiedEnv = verified["environment"].(map[string]any)
	if verifiedEnv["lastVerificationRunId"] != acceptedRunID {
		t.Fatalf("accepted verification environment = %#v", verifiedEnv)
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         acceptedRunID,
		ProfileID:  "sample",
		WorkflowID: "workflow.core-10",
		Status:     store.StatusPassed,
		SummaryJSON: `{"acceptance":{"templateId":"environment.workflow.skywalking.v1","ok":true,"workflowId":"workflow.core-10",
"expectedSteps":1,"completedSteps":1,"passedSteps":1,"failedSteps":0,"topologyProvider":"skywalking",
"steps":[{"stepId":"step.core-10","caseId":"case.core-10","status":"passed","elapsedMs":12,"evidenceComplete":true,"topologyComplete":true}]}}`,
		StartedAt:  now.Add(-time.Second),
		FinishedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed accepted verification run summary: %v", err)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         acceptedRunID + ".summary",
		RunID:      acceptedRunID,
		Kind:       "summary",
		URI:        "store://verification/" + acceptedRunID + "/summary.json",
		MediaType:  "application/json",
		SHA256:     "verification-summary-sha256",
		SizeBytes:  2,
		Summary:    `{"status":"passed"}`,
		Category:   "verification",
		Visibility: "internal",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("seed accepted verification Evidence: %v", err)
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            acceptedRunID + ".topology.skywalking",
		WorkflowRunID: acceptedRunID,
		WorkflowID:    "workflow.core-10",
		StepID:        "step.core-10",
		CaseID:        "case.core-10",
		RequestID:     "request.core-10",
		TraceID:       "trace.core-10",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","traceId":"trace.core-10","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("seed accepted verification topology: %v", err)
	}

	published := postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusOK)
	publishedEnv := published["environment"].(map[string]any)
	if publishedEnv["status"] != "verified" || publishedEnv["verified"] != true {
		t.Fatalf("published environment = %#v", publishedEnv)
	}
	discoverVerified := decodeJSONResponse(t, server.URL+"/api/environments", http.StatusOK)
	if discoverVerified["count"] != float64(1) {
		t.Fatalf("verified API discovery = %#v", discoverVerified)
	}

	bootstrap := decodeJSONResponse(t, server.URL+"/api/environments/env.team.api/bootstrap", http.StatusOK)
	plan := bootstrap["plan"].(map[string]any)
	if plan["verificationWorkflow"] != "workflow.core-10" || len(plan["healthChecks"].([]any)) != 1 {
		t.Fatalf("bootstrap plan = %#v", plan)
	}
	restorePlan := plan["restore"].(map[string]any)
	dockerPlan := restorePlan["docker"].(map[string]any)
	if restorePlan["pauseBeforeHeavyValidation"] != true || dockerPlan["action"] != "docker-compose" || len(dockerPlan["commands"].([]any)) != 3 {
		t.Fatalf("bootstrap restore plan = %#v", restorePlan)
	}
	steps := plan["steps"].([]any)
	if len(steps) != 4 || steps[0].(map[string]any)["kind"] != "repository" || steps[1].(map[string]any)["kind"] != "docker" || steps[3].(map[string]any)["workflowId"] != "workflow.core-10" {
		t.Fatalf("bootstrap executable steps = %#v", steps)
	}

	registeredOptions := postJSONResponse(t, server.URL+"/api/environments", `{
  "id": "env.compose.options.api",
  "compose": {"composeFile":"compose.yml","projectName":"demo","envFiles":[".env.local"],"profiles":["api"],"services":["web"],"skipPull":true,"skipBuild":true},
  "verificationWorkflowId": "workflow.core-10"
}`, http.StatusOK)
	if registeredOptions["ok"] != true {
		t.Fatalf("register compose options environment = %#v", registeredOptions)
	}
	optionsBootstrap := decodeJSONResponse(t, server.URL+"/api/environments/env.compose.options.api/bootstrap", http.StatusOK)
	optionsDocker := optionsBootstrap["plan"].(map[string]any)["restore"].(map[string]any)["docker"].(map[string]any)
	if optionsDocker["projectName"] != "demo" || optionsDocker["skipPull"] != true || optionsDocker["skipBuild"] != true || len(optionsDocker["commands"].([]any)) != 1 {
		t.Fatalf("compose options bootstrap docker plan = %#v", optionsDocker)
	}
}

func TestServerEnvironmentAPIReportsComponentGraphReadiness(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sandbox.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	registered := postJSONResponse(t, server.URL+"/api/environments", `{
  "id": "env.component.api",
  "compose": {"startCommand":"true"},
  "verificationWorkflowId": "workflow.core-10"
}`, http.StatusOK)
	if registered["ok"] != true {
		t.Fatalf("register environment = %#v", registered)
	}
	if err := s.ReplaceEnvironmentComponentGraph(ctx, "env.component.api", store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "db", Kind: "middleware", Role: "database", ComposeService: "db", Required: true, HealthCheckJSON: `{"type":"compose-service"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
			{ComponentID: "app", Kind: "app", Role: "business-service", ComposeService: "app", Required: true, HealthCheckJSON: `{"type":"url","url":"http://127.0.0.1:18080/health"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
		},
		Dependencies: []store.ComponentDependency{
			{ConsumerComponentID: "app", ProviderComponentID: "db", Phase: "startup", Capability: "sql", Required: true, ProfileJSON: `{}`},
		},
		Assets: []store.ComponentConfigAsset{
			{OwnerComponentID: "app", AssetID: "app.schema", AssetKind: "mysql-ddl", TargetComponentID: "db", TargetPath: "compose/mysql/init/app.sql", ContentInline: "create database app;\n", ApplyOrder: 10, SummaryJSON: `{}`},
		},
	}); err != nil {
		t.Fatalf("replace component graph: %v", err)
	}

	inspect := decodeJSONResponse(t, server.URL+"/api/environments/env.component.api", http.StatusOK)
	componentGraph := inspect["componentGraph"].(map[string]any)
	if componentGraph["ok"] != true || componentGraph["components"] != float64(2) || componentGraph["blockingDependencies"] != float64(1) || strings.Join(jsonStringSlice(componentGraph["blockingOrder"]), ",") != "db,app" {
		t.Fatalf("inspect component graph readiness = %#v", componentGraph)
	}

	bootstrap := decodeJSONResponse(t, server.URL+"/api/environments/env.component.api/bootstrap", http.StatusOK)
	plan := bootstrap["plan"].(map[string]any)
	planGraph := plan["componentGraph"].(map[string]any)
	restoreGraph := plan["restore"].(map[string]any)["componentGraph"].(map[string]any)
	startupPlan := plan["componentStartupPlan"].(map[string]any)
	restoreStartupPlan := plan["restore"].(map[string]any)["componentStartupPlan"].(map[string]any)
	if planGraph["ok"] != true || strings.Join(jsonStringSlice(planGraph["blockingOrder"]), ",") != "db,app" {
		t.Fatalf("bootstrap component graph readiness = %#v", planGraph)
	}
	if restoreGraph["ok"] != true || strings.Join(jsonStringSlice(restoreGraph["blockingOrder"]), ",") != "db,app" {
		t.Fatalf("bootstrap restore component graph readiness = %#v", restoreGraph)
	}
	batches := startupPlan["batches"].([]any)
	firstBatch := batches[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	secondBatch := batches[1].(map[string]any)["components"].([]any)[0].(map[string]any)
	if startupPlan["ok"] != true || len(batches) != 2 || firstBatch["componentId"] != "db" || secondBatch["componentId"] != "app" || len(startupPlan["healthGates"].([]any)) != 2 {
		t.Fatalf("bootstrap component startup plan = %#v", startupPlan)
	}
	if restoreStartupPlan["ok"] != true {
		t.Fatalf("bootstrap restore component startup plan = %#v", restoreStartupPlan)
	}
}

func TestServerListsInstalledProfilesFromProfileHome(t *testing.T) {
	profileHome := t.TempDir()
	installedDir := filepath.Join(profileHome, "sample")
	writeWorkbenchSampleProfileAt(t, installedDir)
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{ProfileHome: profileHome}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/profile/installed", http.StatusOK)

	if payload["profileHome"] != profileHome {
		t.Fatalf("installed profile home = %#v", payload)
	}
	items, ok := payload["profiles"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("installed profiles = %#v", payload["profiles"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("installed profile item = %#v", items[0])
	}
	counts, ok := item["counts"].(map[string]any)
	if item["id"] != "sample" || item["displayName"] != "Sample Profile" || item["path"] != installedDir || !ok || counts["workflows"] != float64(1) || counts["apiCases"] != float64(1) {
		t.Fatalf("installed profile item = %#v", item)
	}
	if digest, ok := item["bundleDigest"].(string); !ok || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("installed profile digest = %#v", item["bundleDigest"])
	}
}

func TestServerListsInvalidInstalledProfileWithoutFailing(t *testing.T) {
	profileHome := t.TempDir()
	brokenDir := filepath.Join(profileHome, "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("create broken profile dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "profile.json"), []byte(`{"id":`), 0o644); err != nil {
		t.Fatalf("write broken profile: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{ProfileHome: profileHome}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/profile/installed", http.StatusOK)

	items, ok := payload["profiles"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("installed profiles = %#v", payload["profiles"])
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["id"] != "broken" || item["path"] != brokenDir || item["valid"] != false || item["error"] == "" {
		t.Fatalf("invalid installed profile item = %#v", items[0])
	}
}

func TestServerInstallsProfileBundleIntoProfileHome(t *testing.T) {
	sourceDir := writeWorkbenchSampleProfile(t)
	profileHome := t.TempDir()
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{ProfileHome: profileHome}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/install", `{"path":`+mustJSON(t, sourceDir)+`}`, http.StatusOK)

	targetPath := filepath.Join(profileHome, "sample")
	if payload["id"] != "sample" || payload["targetPath"] != targetPath {
		t.Fatalf("install payload = %#v", payload)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "profile.json")); err != nil {
		t.Fatalf("installed manifest missing: %v", err)
	}
	list := decodeJSONResponse(t, server.URL+"/api/profile/installed", http.StatusOK)
	items, ok := list["profiles"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("installed profiles after install = %#v", list)
	}
}

func TestServerImportsPackedProfileArchiveIntoRuntimeStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	sourceDir := writeWorkbenchSampleProfile(t)
	archivePath := filepath.Join(t.TempDir(), "sample-profile.tgz")
	if _, err := profilehome.Pack(sourceDir, "", archivePath, false); err != nil {
		t.Fatalf("pack sample profile: %v", err)
	}
	profileHome := t.TempDir()
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{Runtime: s, ProfileHome: profileHome}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`+mustJSON(t, archivePath)+`}`, http.StatusOK)

	targetPath := filepath.Join(profileHome, "sample")
	if payload["profileId"] != "sample" || payload["bundlePath"] != targetPath {
		t.Fatalf("archive import payload = %#v", payload)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "profile.json")); err != nil {
		t.Fatalf("installed archive profile missing: %v", err)
	}
	index, err := s.GetProfileIndex(ctx, "sample")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if index.BundlePath != targetPath || !strings.HasPrefix(index.BundleDigest, "sha256:") {
		t.Fatalf("archive profile index = %#v", index)
	}
	active := decodeJSONResponse(t, server.URL+"/api/profile", http.StatusOK)
	if active["id"] != "sample" {
		t.Fatalf("active profile after archive import = %#v", active)
	}
}

func TestServerVerifiesPackedProfileArchiveIntoRuntimeStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	sourceDir := writeWorkbenchSampleProfile(t)
	archivePath := filepath.Join(t.TempDir(), "sample-profile.tgz")
	if _, err := profilehome.Pack(sourceDir, "", archivePath, false); err != nil {
		t.Fatalf("pack sample profile: %v", err)
	}
	profileHome := t.TempDir()
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{Runtime: s, ProfileHome: profileHome}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, archivePath)+`}`, http.StatusOK)

	targetPath := filepath.Join(profileHome, "sample")
	if payload["ok"] != true || payload["profileId"] != "sample" {
		t.Fatalf("archive verify payload = %#v", payload)
	}
	publish, ok := payload["publish"].(map[string]any)
	if !ok || publish["bundlePath"] != targetPath {
		t.Fatalf("archive verify publish = %#v", payload["publish"])
	}
	index, err := s.GetProfileIndex(ctx, "sample")
	if err != nil {
		t.Fatalf("get verified archive profile index: %v", err)
	}
	if index.BundlePath != targetPath || !strings.HasPrefix(index.BundleDigest, "sha256:") {
		t.Fatalf("verified archive profile index = %#v", index)
	}
}

func TestServerCanVerifyInstalledProfileByID(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileHome := t.TempDir()
	installedDir := filepath.Join(profileHome, "sample")
	writeWorkbenchSampleProfileAt(t, installedDir)
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{Runtime: s, ProfileHome: profileHome}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":"sample"}`, http.StatusOK)

	if payload["ok"] != true || payload["profileId"] != "sample" {
		t.Fatalf("verify installed profile payload = %#v", payload)
	}
	publish, ok := payload["publish"].(map[string]any)
	if !ok || publish["bundlePath"] != installedDir {
		t.Fatalf("verify installed profile publish = %#v", payload["publish"])
	}
	active := decodeJSONResponse(t, server.URL+"/api/profile", http.StatusOK)
	if active["id"] != "sample" {
		t.Fatalf("active profile after installed verify = %#v", active)
	}
}

func TestServerImportsProfileBundleWithAudit(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeAuditSampleProfile(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`+mustJSON(t, profileDir)+`,"audit":true}`, http.StatusOK)

	audit, ok := payload["audit"].(map[string]any)
	if !ok {
		t.Fatalf("missing audit in import payload = %#v", payload)
	}
	if audit["ok"] != false || audit["issueCount"] != float64(2) {
		t.Fatalf("audit summary = %#v", audit)
	}
	auditStore, ok := audit["store"].(map[string]any)
	if !ok || auditStore["profileIndexed"] != true || auditStore["digestMatches"] != true {
		t.Fatalf("audit store = %#v", audit["store"])
	}
}

func TestServerCanRequireCleanProfileAuditBeforeImport(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeAuditSampleProfile(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`+mustJSON(t, profileDir)+`,"requireAuditOk":true}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "profile audit failed") {
		t.Fatalf("strict import payload = %#v", payload)
	}
	if _, err := s.GetProfileIndex(ctx, "sample"); err == nil {
		t.Fatalf("strict import wrote profile index")
	} else if err != store.ErrNotFound {
		t.Fatalf("get profile index after strict import: %v", err)
	}
}

func TestServerExposesProfileAuditRepairPlan(t *testing.T) {
	profileDir := writeAuditSampleProfile(t)
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/audit-plan", `{"path":`+mustJSON(t, profileDir)+`}`, http.StatusOK)

	if payload["ok"] != true || payload["profileId"] != "sample" || payload["actionCount"] != float64(2) {
		t.Fatalf("profile audit plan payload = %#v", payload)
	}
	counts, ok := payload["counts"].(map[string]any)
	if !ok || counts["updateReferenceOrAddAsset"] != float64(2) {
		t.Fatalf("profile audit plan counts = %#v", payload["counts"])
	}
	actions, ok := payload["actions"].([]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("profile audit plan actions = %#v", payload["actions"])
	}
	first := actions[0].(map[string]any)
	if first["type"] != "update-reference-or-add-asset" || first["issueCode"] != "api-case-node-missing" || first["subjectId"] != "case.alpha" {
		t.Fatalf("profile audit plan first action = %#v", first)
	}

	missing := postJSONResponse(t, server.URL+"/api/profile/audit-plan", `{}`, http.StatusBadRequest)
	if missing["ok"] != false || !strings.Contains(fmt.Sprint(missing["error"]), "path is required") {
		t.Fatalf("profile audit plan missing path = %#v", missing)
	}
}

func TestServerVerifiesProfileBundleBeforeActivation(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeEmptyProfileBundle(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`}`, http.StatusOK)

	if payload["ok"] != true || payload["profileId"] != "empty" {
		t.Fatalf("profile verify payload = %#v", payload)
	}
	audit, ok := payload["audit"].(map[string]any)
	if !ok || audit["ok"] != true || audit["issueCount"] != float64(0) {
		t.Fatalf("profile verify audit = %#v", payload["audit"])
	}
	publish, ok := payload["publish"].(map[string]any)
	if !ok || publish["profileId"] != "empty" || publish["configVersion"] == nil {
		t.Fatalf("profile verify publish = %#v", payload["publish"])
	}
	checks, ok := payload["checks"].([]any)
	if !ok || len(checks) < 6 {
		t.Fatalf("profile verify checks = %#v", payload["checks"])
	}
	for _, raw := range checks {
		check, ok := raw.(map[string]any)
		if !ok || check["ok"] != true || check["detail"] == "" {
			t.Fatalf("profile verify check = %#v", raw)
		}
	}
	index, err := s.GetProfileIndex(ctx, "empty")
	if err != nil {
		t.Fatalf("get verified profile index: %v", err)
	}
	if index.BundleDigest == "" {
		t.Fatalf("verified profile index = %#v", index)
	}
	model, err := s.GetReadModel(ctx, "empty", controlplane.ReadModelDashboard)
	if err != nil {
		t.Fatalf("get verified dashboard read model: %v", err)
	}
	if model.ConfigVersionID == "" {
		t.Fatalf("verified dashboard read model = %#v", model)
	}
}

func TestServerProfileVerifyCanRequirePassedAPICaseRuns(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeWorkbenchSampleProfile(t)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
		UpdatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
	}); err != nil {
		t.Fatalf("create api case run parent: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "case-run.alpha",
		RunID:      "run.alpha",
		CaseID:     "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
	}); err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`,"requireCaseRuns":true}`, http.StatusOK)

	if payload["ok"] != true {
		t.Fatalf("profile verify runtime payload = %#v", payload)
	}
	checks, ok := payload["checks"].([]any)
	if !ok || !hasJSONCheck(checks, "api-case-run:case.alpha") {
		t.Fatalf("profile verify runtime checks = %#v", payload["checks"])
	}
}

func TestServerProfileVerifyFailureIncludesDiagnosticReport(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeWorkbenchSampleProfile(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`,"requireCaseRuns":true}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "profile verification failed") {
		t.Fatalf("profile verify failure payload = %#v", payload)
	}
	summary, ok := payload["summary"].(map[string]any)
	if !ok || summary["firstFailed"] != "api-case-run:case.alpha" || summary["failedChecks"] != float64(1) {
		t.Fatalf("profile verify failure summary = %#v", payload["summary"])
	}
	checks, ok := payload["checks"].([]any)
	if !ok || !hasJSONFailedCheck(checks, "api-case-run:case.alpha") {
		t.Fatalf("profile verify failure checks = %#v", payload["checks"])
	}
}

func TestServerProfileVerifyCanRequirePassedWorkflowRuns(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeWorkbenchSampleProfile(t)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.workflow.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T02:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T02:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T02:00:01Z"),
		UpdatedAt:  mustParseTime(t, "2026-05-14T02:00:01Z"),
	}); err != nil {
		t.Fatalf("create workflow run: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`,"requireWorkflowRuns":true}`, http.StatusOK)

	if payload["ok"] != true {
		t.Fatalf("profile verify workflow payload = %#v", payload)
	}
	checks, ok := payload["checks"].([]any)
	if !ok || !hasJSONCheck(checks, "workflow-run:workflow.alpha") {
		t.Fatalf("profile verify workflow checks = %#v", payload["checks"])
	}
	summary, ok := payload["summary"].(map[string]any)
	if !ok {
		t.Fatalf("profile verify workflow summary missing: %#v", payload)
	}
	if summary["totalChecks"] != float64(len(checks)) || summary["passedChecks"] != float64(len(checks)) || summary["failedChecks"] != float64(0) {
		t.Fatalf("profile verify workflow summary counts = %#v checks=%d", summary, len(checks))
	}
	if summary["requiredWorkflowRuns"] != true || summary["requiredCaseRuns"] != false {
		t.Fatalf("profile verify workflow summary gates = %#v", summary)
	}
}

func TestServerProfileVerifyStopsBeforePublishWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeAuditSampleProfile(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "profile audit failed") {
		t.Fatalf("profile verify failure payload = %#v", payload)
	}
	if _, err := s.GetProfileIndex(ctx, "sample"); err == nil {
		t.Fatalf("profile verify wrote profile index after audit failure")
	} else if err != store.ErrNotFound {
		t.Fatalf("get profile index after verify failure: %v", err)
	}
}

func TestServerRejectsProfileImportWithoutRuntimeStore(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":"/tmp/external-profile"}`, http.StatusNotImplemented)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "runtime store") {
		t.Fatalf("missing store payload = %#v", payload)
	}
}

func TestServerRejectsCatalogIndexWithoutRuntimeStore(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/profile/catalog-index", http.StatusNotImplemented)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "runtime store") {
		t.Fatalf("missing store catalog index payload = %#v", payload)
	}
}

func TestServerRejectsProfileImportNonPost(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/profile/import")
	if err != nil {
		t.Fatalf("get profile import: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("profile import get status = %d", resp.StatusCode)
	}
}

func TestServerRejectsProfileImportInvalidJSON(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "invalid json") {
		t.Fatalf("invalid json payload = %#v", payload)
	}
}

func TestServerRejectsProfileImportWithoutPath(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"audit":true}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "path is required") {
		t.Fatalf("missing path payload = %#v", payload)
	}
}

func TestServerRejectsProfileImportForInvalidProfile(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), []byte(`{"id":"","displayName":"Broken Profile"}`), 0o644); err != nil {
		t.Fatalf("write invalid profile: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`+mustJSON(t, dir)+`}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "load profile") {
		t.Fatalf("invalid profile payload = %#v", payload)
	}
}

func TestServerExposesEmptyProfileAssetLists(t *testing.T) {
	server := httptest.NewServer(controlplane.New(profile.Bundle{
		ID:          "empty",
		DisplayName: "Empty Profile",
	}))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/profile/assets")
	if err != nil {
		t.Fatalf("get profile assets api: %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Services       []profile.Service       `json:"services"`
		Workflows      []profile.Workflow      `json:"workflows"`
		InterfaceNodes []profile.InterfaceNode `json:"interfaceNodes"`
		APICases       []profile.APICase       `json:"apiCases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode profile assets api: %v", err)
	}
	if payload.Services == nil || payload.Workflows == nil || payload.InterfaceNodes == nil || payload.APICases == nil {
		t.Fatalf("empty asset lists should encode as arrays: %#v", payload)
	}
}

func TestServerRendersDashboardForEmptyProfile(t *testing.T) {
	bundle := loadEmptyProfile(t)
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/dashboard.html")
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	body := string(raw)
	for _, want := range []string{"Open Test Sandbox", "react-dashboard-root", "/assets/react/dashboard.js"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q: %s", want, body)
		}
	}
}

func TestServerRendersWorkbenchIndexAtRoot(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("get index: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	body := string(raw)
	for _, want := range []string{"react-sandbox-workbench-root", "/assets/react/sandboxWorkbench.js"} {
		if !strings.Contains(body, want) {
			t.Fatalf("index missing %q: %s", want, body)
		}
	}
}

func TestServerRendersWorkflowCatalogPage(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/workflows.html")
	if err != nil {
		t.Fatalf("get workflows page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("workflows status = %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read workflows page: %v", err)
	}
	body := string(raw)
	for _, want := range []string{"Workflow Catalog", "react-workflows-root", "/assets/react/workflows.js"} {
		if !strings.Contains(body, want) {
			t.Fatalf("workflows page missing %q: %s", want, body)
		}
	}
}

func TestServerServesReferenceStaticPagesAndAssets(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	for _, item := range []struct {
		path string
		want string
	}{
		{path: "/index.html", want: "react-sandbox-workbench-root"},
		{path: "/agent-run.html", want: "react-agent-run-root"},
		{path: "/api-cases.html", want: "react-api-cases-root"},
		{path: "/case-runs.html", want: "react-case-runs-root"},
		{path: "/evidence-viewer.html", want: "react-evidence-viewer-root"},
		{path: "/trace-topology.html", want: "react-trace-topology-root"},
		{path: "/replay-evidence.html", want: "react-replay-evidence-root"},
		{path: "/trace-call.html", want: "react-control-plane-root"},
		{path: "/trace-evidence.html", want: "react-control-plane-root"},
		{path: "/workflow-blueprint-demo.html", want: "react-workflow-blueprint-demo-root"},
		{path: "/workflow-blueprint-new.html", want: "react-workflow-blueprint-demo-root"},
		{path: "/interface-nodes.html", want: "react-interface-nodes-root"},
		{path: "/interface-node.html", want: "react-interface-node-root"},
		{path: "/interface-node-history.html", want: "react-interface-node-root"},
		{path: "/interface-node-fields.html", want: "react-interface-node-root"},
		{path: "/environment-nodes.html", want: "react-environment-nodes-root"},
		{path: "/environment-node.html", want: "react-environment-node-root"},
		{path: "/service-inventory.html", want: "react-service-inventory-root"},
		{path: "/workflow-run.html", want: "react-workflow-run-root"},
		{path: "/workflow-detail.html", want: "react-workflow-detail-root"},
		{path: "/workflow-step.html", want: "react-workflow-step-root"},
		{path: "/styles.css", want: "body"},
		{path: "/assets/react/controlPlane.css", want: "react-control-plane"},
		{path: "/assets/react/controlPlane.js", want: "Trace Evidence"},
		{path: "/assets/react/agentRun.js", want: "/api/agent-test"},
		{path: "/assets/react/apiCases.js", want: "/api/cases/capabilities"},
		{path: "/assets/react/caseRuns.js", want: "/api/case/incomplete-batches"},
		{path: "/assets/react/environmentNode.js", want: "/api/interface-nodes"},
		{path: "/assets/react/environmentNodes.js", want: "/api/dashboard"},
		{path: "/assets/react/evidenceViewer.js", want: "/api/case/evidence"},
		{path: "/assets/react/interfaceNode.js", want: "/api/interface-node"},
		{path: "/assets/react/interfaceNodes.js", want: "/api/interface-nodes"},
		{path: "/assets/react/replayEvidence.js", want: "/api/replay/evidence"},
		{path: "/assets/react/sandboxWorkbench.js", want: "/api/template-packages/import"},
		{path: "/assets/react/serviceInventory.js", want: "/api/catalog"},
		{path: "/assets/react/traceTopology.js", want: "/api/workflow-runs/"},
		{path: "/assets/react/workflowDetail.js", want: "/api/catalog"},
		{path: "/assets/react/workflowRun.js", want: "/api/runs"},
		{path: "/assets/react/workflowStep.js", want: "/api/dashboard"},
		{path: "/assets/react/workflowBlueprintDemo.css", want: "blueprint-demo-shell"},
		{path: "/assets/react/workflowBlueprintDemo.js", want: "workflow-blueprint-demo/v1"},
	} {
		resp, err := http.Get(server.URL + item.path)
		if err != nil {
			t.Fatalf("get %s: %v", item.path, err)
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", item.path, readErr)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", item.path, resp.StatusCode)
		}
		if strings.HasSuffix(item.path, ".css") && !strings.Contains(resp.Header.Get("Content-Type"), "text/css") {
			t.Fatalf("%s content-type = %q", item.path, resp.Header.Get("Content-Type"))
		}
		if !strings.Contains(string(raw), item.want) {
			t.Fatalf("%s missing %q", item.path, item.want)
		}
	}

	resp, err := http.Get(server.URL + "/agent-test.html")
	if err != nil {
		t.Fatalf("get removed agent test page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("removed agent test page status = %d", resp.StatusCode)
	}
}

func TestServerExposesStateForWorkbenchIndex(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/state")
	if err != nil {
		t.Fatalf("get state api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state status = %d", resp.StatusCode)
	}

	var payload struct {
		Services []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode state api: %v", err)
	}
	if len(payload.Services) != 1 || payload.Services[0].ID != "service.alpha" || payload.Services[0].Name != "Service Alpha" {
		t.Fatalf("state services = %#v", payload.Services)
	}
	if payload.Services[0].Status != "missing" {
		t.Fatalf("state service status = %#v", payload.Services[0])
	}
}

func TestServerExposesInterfaceNodesForService(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
			{ID: "node.beta", DisplayName: "Node Beta", ServiceID: "service.beta"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/interface-nodes?serviceId=service.alpha")
	if err != nil {
		t.Fatalf("get interface nodes api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("interface nodes status = %d", resp.StatusCode)
	}

	var payload struct {
		Source struct {
			Kind string `json:"kind"`
		} `json:"source"`
		Items []struct {
			ID                string `json:"id"`
			DisplayName       string `json:"displayName"`
			ServiceID         string `json:"serviceId"`
			Href              string `json:"href"`
			AdmissionStatus   string `json:"admissionStatus"`
			ValidationStatus  string `json:"validationStatus"`
			RequiredCaseCount int    `json:"requiredCaseCount"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode interface nodes api: %v", err)
	}
	if payload.Source.Kind != "profile" {
		t.Fatalf("interface node source = %#v", payload.Source)
	}
	if len(payload.Items) != 1 || payload.Items[0].ID != "node.alpha" || payload.Items[0].ServiceID != "service.alpha" {
		t.Fatalf("interface node items = %#v", payload.Items)
	}
	if payload.Items[0].Href == "" || payload.Items[0].AdmissionStatus != "pending" || payload.Items[0].ValidationStatus != "valid" || payload.Items[0].RequiredCaseCount != 0 {
		t.Fatalf("interface node link/status = %#v", payload.Items[0])
	}
}

func TestServerFiltersInterfaceNodesBySearchText(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Create item"},
			{ID: "node.beta", DisplayName: "Node Beta", ServiceID: "service.beta", Operation: "Delete item"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-nodes?filter=delete", http.StatusOK)
	filters := payload["filters"].(map[string]any)
	if filters["filter"] != "delete" {
		t.Fatalf("interface node filters = %#v", filters)
	}
	items := payload["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != "node.beta" {
		t.Fatalf("filtered interface nodes = %#v", payload)
	}
}

func TestServerExposesInterfaceNodesFromLatestCaseRunsWithoutFullRunScan(t *testing.T) {
	runtime := latestCaseRunCatalogStore{
		catalog: store.ProfileCatalog{
			ProfileID: "sample",
			InterfaceNodes: []store.CatalogInterfaceNode{
				{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Method: "POST", Path: "/alpha", Status: "active"},
			},
			APICases: []store.CatalogAPICase{
				{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
			},
			TemplateConfigs: []store.CatalogTemplateConfig{
				{
					ID:         "cfg.interface-directory.default",
					TemplateID: "TPL-INTERFACE-NODE-DIRECTORY-V1",
					ScopeType:  "interface-node-directory",
					ScopeID:    "_default",
					ConfigJSON: `{"copy":{"directoryTitle":"Configured interface directory","latestElapsedLabel":"Configured latest","totalElapsedLabel":"Configured total"}}`,
					Status:     "active",
				},
			},
		},
		latest: []store.APICaseRun{
			{
				ID:         "run.alpha.case",
				RunID:      "run.alpha",
				CaseID:     "case.alpha",
				Status:     store.StatusPassed,
				StartedAt:  time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
				FinishedAt: time.Date(2026, 5, 15, 10, 0, 0, 240*int(time.Millisecond), time.UTC),
			},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, runtime))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("interface node items = %#v", items)
	}
	item := items[0].(map[string]any)
	if item["admissionStatus"] != store.StatusPassed || item["passedCaseCount"] != float64(1) || item["latestRunId"] != "run.alpha" {
		t.Fatalf("interface node latest state = %#v", item)
	}
	if item["latestElapsedMs"] != float64(240) || item["totalElapsedMs"] != float64(240) {
		t.Fatalf("interface node elapsed state = %#v", item)
	}
	presentation := payload["presentation"].(map[string]any)
	copy := presentation["copy"].(map[string]any)
	if copy["directoryTitle"] != "Configured interface directory" || copy["totalElapsedLabel"] != "Configured total" {
		t.Fatalf("interface node directory presentation = %#v", presentation)
	}
}

func TestServerHydratesInterfaceNodeCoverageFromLatestCaseRunsWithoutFullRunScan(t *testing.T) {
	catalog := store.ProfileCatalog{
		ProfileID: "sample",
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
		},
	}
	models, err := controlplane.InterfaceNodeCoverageReadModels(catalog, "config.sample.001", time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build coverage read models: %v", err)
	}
	readModels := map[string]store.ReadModel{}
	for _, model := range models {
		readModels[model.Key] = model
	}
	runtime := latestCaseRunCatalogStore{
		catalog:    catalog,
		readModels: readModels,
		latest: []store.APICaseRun{
			{ID: "run.alpha.case", RunID: "run.alpha", CaseID: "case.alpha", Status: store.StatusPassed},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, runtime))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node/coverage?workflow=workflow.alpha", http.StatusOK)
	source := payload["source"].(map[string]any)
	if source["kind"] != "read-model" {
		t.Fatalf("coverage source = %#v", source)
	}
	rows := payload["rows"].([]any)
	row := rows[0].(map[string]any)
	if row["admissionStatus"] != store.StatusPassed || row["passedCaseCount"] != float64(1) || row["latestRunId"] != "run.alpha" {
		t.Fatalf("coverage row latest state = %#v", row)
	}
	summary := payload["summary"].(map[string]any)
	if summary["passedNodes"] != float64(1) || summary["pendingNodes"] != float64(0) || summary["failedNodes"] != float64(0) {
		t.Fatalf("coverage summary latest state = %#v", summary)
	}
}

func TestServerExposesInterfaceNodeDetail(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
		RequestTemplates: []profile.RequestTemplate{
			{ID: "template.alpha", DisplayName: "Template Alpha", NodeID: "node.alpha", Method: "POST", Path: "/alpha", TemplateJSON: "{}"},
		},
		CaseDependencies: []profile.CaseDependency{
			{ID: "dependency.alpha", CaseID: "case.alpha", FixtureID: "fixture.alpha", MappingsJSON: "[]"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/interface-node?id=node.alpha")
	if err != nil {
		t.Fatalf("get interface node api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("interface node status = %d", resp.StatusCode)
	}

	var payload struct {
		Node struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			ServiceID   string `json:"serviceId"`
			Method      string `json:"method"`
			Path        string `json:"path"`
		} `json:"node"`
		Admission struct {
			Status            string `json:"status"`
			RequiredCaseCount int    `json:"requiredCaseCount"`
			PassedCaseCount   int    `json:"passedCaseCount"`
		} `json:"admission"`
		RequestTemplates []map[string]any `json:"requestTemplates"`
		Cases            []map[string]any `json:"cases"`
		Fields           struct {
			Request  []map[string]any `json:"request"`
			Response []map[string]any `json:"response"`
		} `json:"fields"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode interface node api: %v", err)
	}
	if payload.Node.ID != "node.alpha" || payload.Node.ServiceID != "service.alpha" {
		t.Fatalf("interface node detail = %#v", payload.Node)
	}
	if payload.Node.Method != "POST" || payload.Node.Path != "/alpha" {
		t.Fatalf("interface node operation = %#v", payload.Node)
	}
	if payload.Admission.Status != "pending" || payload.Admission.RequiredCaseCount != 0 || payload.Admission.PassedCaseCount != 0 {
		t.Fatalf("interface node admission = %#v", payload.Admission)
	}
	if len(payload.RequestTemplates) != 1 || payload.RequestTemplates[0]["id"] != "template.alpha" {
		t.Fatalf("interface node templates = %#v", payload.RequestTemplates)
	}
	if len(payload.Cases) != 1 || payload.Cases[0]["id"] != "case.alpha" {
		t.Fatalf("interface node cases = %#v", payload.Cases)
	}
	if payload.Cases == nil || payload.Fields.Request == nil || payload.Fields.Response == nil {
		t.Fatalf("interface node empty arrays = %#v", payload)
	}
}

func TestServerExposesInterfaceNodeRunHistoryFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		run     store.Run
		caseRun store.APICaseRun
	}{
		{
			run: store.Run{
				ID:           "run.alpha",
				ProfileID:    "sample",
				WorkflowID:   "workflow.alpha",
				Status:       store.StatusPassed,
				EvidenceRoot: ".runtime/evidence/run.alpha",
				CreatedAt:    started,
			},
			caseRun: store.APICaseRun{
				ID:                   "run.alpha.case",
				RunID:                "run.alpha",
				CaseID:               "case.alpha",
				Status:               store.StatusPassed,
				RequestSummaryJSON:   `{"method":"GET","path":"/alpha"}`,
				AssertionSummaryJSON: `{"status":"passed"}`,
				StartedAt:            started,
				FinishedAt:           started.Add(150 * time.Millisecond),
				CreatedAt:            started,
			},
		},
		{
			run: store.Run{
				ID:           "run.beta",
				ProfileID:    "sample",
				WorkflowID:   "workflow.alpha",
				Status:       store.StatusFailed,
				EvidenceRoot: ".runtime/evidence/run.beta",
				CreatedAt:    started.Add(time.Minute),
			},
			caseRun: store.APICaseRun{
				ID:                   "run.beta.case",
				RunID:                "run.beta",
				CaseID:               "case.beta",
				Status:               store.StatusFailed,
				RequestSummaryJSON:   `{"method":"POST","path":"/beta"}`,
				AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
				StartedAt:            started.Add(time.Minute),
				FinishedAt:           started.Add(time.Minute + 250*time.Millisecond),
				CreatedAt:            started.Add(time.Minute),
			},
		},
	} {
		if _, err := s.CreateRun(ctx, item.run); err != nil {
			t.Fatalf("create run %s: %v", item.run.ID, err)
		}
		if _, err := s.RecordAPICaseRun(ctx, item.caseRun); err != nil {
			t.Fatalf("record case run %s: %v", item.caseRun.ID, err)
		}
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.alpha",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.alpha",
		CaseID:        "case.alpha",
		RequestID:     "request.alpha",
		TraceID:       "trace.alpha",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","requestId":"request.alpha","traceId":"trace.alpha","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     started.Add(time.Second),
	}); err != nil {
		t.Fatalf("save trace topology: %v", err)
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
			{ID: "case.beta", DisplayName: "Case Beta", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	history := payload["history"].(map[string]any)
	if history["latestRunId"] != "run.beta" || history["runCount"] != float64(2) || history["passCount"] != float64(1) || history["failCount"] != float64(1) {
		t.Fatalf("interface node history = %#v", history)
	}
	if history["latestFailureReason"] != "assertion errors: 1" || history["totalElapsedMs"] != float64(400) {
		t.Fatalf("interface node history details = %#v", history)
	}
	cases := payload["cases"].([]any)
	if len(cases) != 2 {
		t.Fatalf("interface node cases = %#v", cases)
	}
	latest := cases[1].(map[string]any)["latestRun"].(map[string]any)
	if latest["runId"] != "run.beta" || latest["caseId"] != "case.beta" || latest["status"] != store.StatusFailed || latest["elapsedMs"] != float64(250) {
		t.Fatalf("case latest run = %#v", latest)
	}
	runs := payload["runs"].([]any)
	if len(runs) != 2 || runs[0].(map[string]any)["runId"] != "run.beta" {
		t.Fatalf("interface node runs = %#v", runs)
	}
}

func TestServerScopesInterfaceNodeRunsToWorkflowStepContext(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		run     store.Run
		caseRun store.APICaseRun
	}{
		{
			run: store.Run{
				ID:         "run.alpha",
				ProfileID:  "sample",
				WorkflowID: "workflow.alpha",
				Status:     store.StatusPassed,
				SummaryJSON: `{"steps":[
					{"stepId":"step.alpha","caseId":"case.alpha"},
					{"stepId":"step.beta","caseId":"case.beta"}
				]}`,
				CreatedAt: started,
			},
			caseRun: store.APICaseRun{
				ID:                   "run.alpha.case",
				RunID:                "run.alpha",
				CaseID:               "case.alpha",
				Status:               store.StatusPassed,
				RequestSummaryJSON:   `{"stepId":"step.alpha","method":"POST","path":"/alpha","requestId":"request.alpha"}`,
				AssertionSummaryJSON: `{"status":"passed"}`,
				StartedAt:            started,
				FinishedAt:           started.Add(150 * time.Millisecond),
				CreatedAt:            started,
			},
		},
		{
			run: store.Run{
				ID:          "run.beta",
				ProfileID:   "sample",
				WorkflowID:  "case.alpha.standalone",
				Status:      store.StatusFailed,
				SummaryJSON: `{}`,
				CreatedAt:   started.Add(time.Minute),
			},
			caseRun: store.APICaseRun{
				ID:                   "run.beta.case",
				RunID:                "run.beta",
				CaseID:               "case.alpha",
				Status:               store.StatusFailed,
				RequestSummaryJSON:   `{"method":"POST","path":"/alpha","requestId":"request.beta"}`,
				AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
				StartedAt:            started.Add(time.Minute),
				FinishedAt:           started.Add(time.Minute + 250*time.Millisecond),
				CreatedAt:            started.Add(time.Minute),
			},
		},
	} {
		if _, err := s.CreateRun(ctx, item.run); err != nil {
			t.Fatalf("create run %s: %v", item.run.ID, err)
		}
		if _, err := s.RecordAPICaseRun(ctx, item.caseRun); err != nil {
			t.Fatalf("record case run %s: %v", item.caseRun.ID, err)
		}
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.alpha",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.alpha",
		CaseID:        "case.alpha",
		RequestID:     "request.alpha",
		TraceID:       "trace.alpha",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","requestId":"request.alpha","traceId":"trace.alpha","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     started.Add(time.Second),
	}); err != nil {
		t.Fatalf("save trace topology: %v", err)
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	global := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	globalCase := global["cases"].([]any)[0].(map[string]any)
	if globalCase["latestRun"].(map[string]any)["runId"] != "run.alpha" {
		t.Fatalf("global interface node should prefer latest passing cache: %#v", globalCase)
	}

	scoped := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha&flowId=workflow.alpha&runId=run.alpha&stepId=step.alpha", http.StatusOK)
	context := scoped["context"].(map[string]any)
	if context["flowId"] != "workflow.alpha" || context["workflowId"] != "workflow.alpha" || context["runId"] != "run.alpha" || context["stepId"] != "step.alpha" {
		t.Fatalf("interface node context = %#v", context)
	}
	scopedCase := scoped["cases"].([]any)[0].(map[string]any)
	latest := scopedCase["latestRun"].(map[string]any)
	if latest["runId"] != "run.alpha" || latest["caseRunId"] != "run.alpha.case" || latest["elapsedMs"] != float64(150) {
		t.Fatalf("scoped interface node latest run = %#v", latest)
	}
	topology := latest["topology"].(map[string]any)
	if topology["traceId"] != "trace.alpha" || topology["requestId"] != "request.alpha" || topology["status"] != "complete" || topology["provider"] != "skywalking" {
		t.Fatalf("scoped interface node topology = %#v", topology)
	}
	request := latest["requestSummary"].(map[string]any)
	if request["requestId"] != "request.alpha" || request["stepId"] != "step.alpha" {
		t.Fatalf("scoped request summary = %#v", request)
	}
	runs := scoped["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["runId"] != "run.alpha" {
		t.Fatalf("scoped interface node runs = %#v", runs)
	}

}

func TestServerEvaluatesInterfaceNodeRunTimeoutFromCatalog(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: started,
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Method: "POST", Path: "/alpha", Status: "active", TimeoutMs: 100},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		CreatedAt:  started,
		UpdatedAt:  started.Add(150 * time.Millisecond),
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
		AssertionSummaryJSON: `{"status":"passed"}`,
		StartedAt:            started,
		FinishedAt:           started.Add(150 * time.Millisecond),
		CreatedAt:            started,
	})
	if err != nil {
		t.Fatalf("record case run: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	list := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("interface node list = %#v", list)
	}
	node := items[0].(map[string]any)
	if node["admissionStatus"] != store.StatusFailed || node["latestElapsedMs"] != float64(150) || node["timeoutMs"] != float64(100) {
		t.Fatalf("interface node timeout state = %#v", node)
	}

	detail := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	cases := detail["cases"].([]any)
	latest := cases[0].(map[string]any)["latestRun"].(map[string]any)
	if latest["status"] != store.StatusFailed || latest["timeoutExceeded"] != true || latest["timeoutMs"] != float64(100) || latest["failureKind"] != "timeout" {
		t.Fatalf("interface node latest run timeout = %#v", latest)
	}
	if !strings.Contains(latest["failureReason"].(string), "exceeded timeout") {
		t.Fatalf("interface node timeout reason = %#v", latest)
	}
	admission := detail["admission"].(map[string]any)
	if admission["status"] != store.StatusFailed || admission["passedCaseCount"] != float64(0) {
		t.Fatalf("interface node admission timeout = %#v", admission)
	}
}

func TestServerExposesInterfaceNodeRunsWithoutFullRunScan(t *testing.T) {
	runtime := interfaceNodeCaseRunCatalogStore{
		catalog: store.ProfileCatalog{
			ProfileID: "sample",
			InterfaceNodes: []store.CatalogInterfaceNode{
				{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Method: "POST", Path: "/alpha", Status: "active"},
			},
			APICases: []store.CatalogAPICase{
				{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
			},
		},
		records: []store.APICaseRunRecord{{
			Run: store.Run{
				ID:           "run.alpha",
				ProfileID:    "sample",
				WorkflowID:   "workflow.alpha",
				Status:       store.StatusPassed,
				EvidenceRoot: ".runtime/evidence/run.alpha",
				CreatedAt:    time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
				UpdatedAt:    time.Date(2026, 5, 15, 9, 0, 1, 0, time.UTC),
			},
			CaseRun: store.APICaseRun{
				ID:                   "run.alpha.case",
				RunID:                "run.alpha",
				CaseID:               "case.alpha",
				Status:               store.StatusPassed,
				RequestSummaryJSON:   `{"method":"POST","path":"/alpha"}`,
				AssertionSummaryJSON: `{"status":"passed"}`,
				StartedAt:            time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
				FinishedAt:           time.Date(2026, 5, 15, 9, 0, 0, 150*int(time.Millisecond), time.UTC),
				CreatedAt:            time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
			},
		}},
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, runtime))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	history := payload["history"].(map[string]any)
	if history["latestRunId"] != "run.alpha" || history["runCount"] != float64(1) {
		t.Fatalf("interface node history = %#v", history)
	}
	cases := payload["cases"].([]any)
	latest := cases[0].(map[string]any)["latestRun"].(map[string]any)
	if latest["runId"] != "run.alpha" || latest["caseRunId"] != "run.alpha.case" || latest["status"] != store.StatusPassed {
		t.Fatalf("interface node latest run = %#v", latest)
	}
}

func TestServerExposesCatalogForReactShell(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha", Description: "Sample workflow"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/catalog")
	if err != nil {
		t.Fatalf("get catalog api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("catalog status = %d", resp.StatusCode)
	}

	var payload struct {
		SchemaVersion string `json:"schemaVersion"`
		Source        struct {
			Kind string `json:"kind"`
		} `json:"source"`
		Services []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Role        string `json:"role"`
		} `json:"services"`
		Workflows []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Entrypoint  string `json:"entrypoint"`
			Steps       []struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
				ServiceID   string `json:"serviceId"`
				CaseID      string `json:"caseId"`
			} `json:"steps"`
			Presentation struct {
				Kind string `json:"kind"`
			} `json:"presentation"`
		} `json:"workflows"`
		Topology struct {
			Nodes []string `json:"nodes"`
		} `json:"topology"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode catalog api: %v", err)
	}
	if payload.SchemaVersion != "1" || payload.Source.Kind != "profile" {
		t.Fatalf("catalog metadata = %#v", payload)
	}
	if len(payload.Services) != 1 || payload.Services[0].ID != "service.alpha" || payload.Services[0].Role != "http" {
		t.Fatalf("catalog services = %#v", payload.Services)
	}
	if len(payload.Workflows) != 1 || payload.Workflows[0].Entrypoint != "/workflow-studio.html" {
		t.Fatalf("catalog workflows = %#v", payload.Workflows)
	}
	if len(payload.Workflows[0].Steps) != 1 || payload.Workflows[0].Steps[0].ServiceID != "service.alpha" || payload.Workflows[0].Steps[0].CaseID != "case.alpha" {
		t.Fatalf("catalog workflow steps = %#v", payload.Workflows[0].Steps)
	}
	if payload.Workflows[0].Presentation.Kind != "businessFlow" {
		t.Fatalf("catalog workflow presentation = %#v", payload.Workflows[0].Presentation)
	}
	if len(payload.Topology.Nodes) != 1 || payload.Topology.Nodes[0] != "service.alpha" {
		t.Fatalf("catalog topology = %#v", payload.Topology)
	}
}

func TestServerExposesCatalogWorkflowFinderFromProfileConfig(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		TemplateConfigs: []profile.TemplateConfig{
			{
				ID:         "cfg.workflow-directory.default",
				TemplateID: "TPL-WORKFLOW-DIRECTORY-V1",
				ScopeType:  "workflow-directory",
				ScopeID:    "_default",
				ConfigJSON: `{"workflowFinder":{"targetStepCount":4,"targetInterfaceCount":4,"targetLabel":"Configured target"}}`,
				Status:     "active",
			},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	presentation, ok := payload["presentation"].(map[string]any)
	if !ok {
		t.Fatalf("catalog presentation missing = %#v", payload)
	}
	finder, ok := presentation["workflowFinder"].(map[string]any)
	if !ok {
		t.Fatalf("workflow finder presentation missing = %#v", presentation)
	}
	if finder["targetStepCount"] != float64(4) || finder["targetInterfaceCount"] != float64(4) || finder["targetLabel"] != "Configured target" {
		t.Fatalf("workflow finder presentation = %#v", finder)
	}
}

func TestServerExposesCatalogWorkflowFinderFromStoreConfig(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.workflow-directory.default",
				TemplateID: "TPL-WORKFLOW-DIRECTORY-V1",
				ScopeType:  "workflow-directory",
				ScopeID:    "_default",
				ConfigJSON: `{"workflowFinder":{"targetStepCount":5,"targetInterfaceCount":5,"targetLabel":"Catalog target"}}`,
				Status:     "active",
			},
		},
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	presentation, ok := payload["presentation"].(map[string]any)
	if !ok {
		t.Fatalf("catalog presentation missing = %#v", payload)
	}
	finder, ok := presentation["workflowFinder"].(map[string]any)
	if !ok {
		t.Fatalf("workflow finder presentation missing = %#v", presentation)
	}
	if finder["targetStepCount"] != float64(5) || finder["targetInterfaceCount"] != float64(5) || finder["targetLabel"] != "Catalog target" {
		t.Fatalf("workflow finder presentation = %#v", finder)
	}
}

func TestServerExposesCatalogWorkflowRunsFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	for _, item := range []store.Run{
		{
			ID:           "run.alpha",
			ProfileID:    "sample",
			WorkflowID:   "workflow.alpha",
			Status:       store.StatusPassed,
			EvidenceRoot: ".runtime/evidence/run.alpha",
			CreatedAt:    started,
			UpdatedAt:    started,
		},
		{
			ID:           "run.beta",
			ProfileID:    "sample",
			WorkflowID:   "workflow.alpha",
			Status:       store.StatusFailed,
			EvidenceRoot: ".runtime/evidence/run.beta",
			SummaryJSON:  `{"steps":[{"stepId":"step.alpha"},{"stepId":"step.beta"}]}`,
			CreatedAt:    started.Add(time.Minute),
			UpdatedAt:    started.Add(time.Minute),
		},
		{
			ID:          "run.gamma",
			ProfileID:   "sample",
			WorkflowID:  "workflow.alpha",
			Status:      store.StatusPassed,
			SummaryJSON: `{"kind":"apiCase","summary":{"caseId":"case.alpha","stepId":"step.alpha"},"steps":[{"stepId":"step.alpha","caseId":"case.alpha"}]}`,
			CreatedAt:   started.Add(2 * time.Minute),
			UpdatedAt:   started.Add(2 * time.Minute),
		},
	} {
		if _, err := s.CreateRun(ctx, item); err != nil {
			t.Fatalf("create run %s: %v", item.ID, err)
		}
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
			{ID: "workflow.empty", DisplayName: "Workflow Empty"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	workflows := payload["workflows"].([]any)
	alpha := workflows[0].(map[string]any)
	if alpha["id"] != "workflow.alpha" || alpha["runCount"] != float64(2) {
		t.Fatalf("workflow run count = %#v", alpha)
	}
	latest := alpha["latestRun"].(map[string]any)
	if latest["id"] != "run.beta" || latest["status"] != store.StatusFailed || latest["workflowId"] != "workflow.alpha" {
		t.Fatalf("workflow latest run = %#v", latest)
	}
	if latest["summaryJson"] != nil || latest["stepCount"] != float64(2) {
		t.Fatalf("catalog latest run should be lightweight but keep stepCount: %#v", latest)
	}
	if _, ok := latest["summary"]; ok {
		t.Fatalf("catalog latest run must not inline full summary: %#v", latest)
	}
	empty := workflows[1].(map[string]any)
	if empty["id"] != "workflow.empty" || empty["runCount"] != float64(0) {
		t.Fatalf("empty workflow run state = %#v", empty)
	}
	if _, ok := empty["latestRun"]; ok {
		t.Fatalf("empty workflow should not expose latestRun: %#v", empty)
	}
}

func TestServerCatalogWorkflowLatestRunPrefersCompleteWorkflowRun(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	workflow := store.CatalogWorkflow{ID: "workflow.alpha", DisplayName: "Workflow Alpha"}
	bindings := make([]store.CatalogWorkflowBinding, 0, 10)
	for i := 1; i <= 10; i++ {
		stepID := fmt.Sprintf("step.%02d", i)
		bindings = append(bindings, store.CatalogWorkflowBinding{
			WorkflowID: workflow.ID,
			StepID:     stepID,
			CaseID:     "case." + stepID,
			NodeID:     "node." + stepID,
			Required:   true,
			SortOrder:  i,
		})
	}
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID:        "sample",
		Workflows:        []store.CatalogWorkflow{workflow},
		WorkflowBindings: bindings,
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	started := time.Date(2026, 5, 19, 9, 0, 0, 0, time.UTC)
	for _, item := range []store.Run{
		{
			ID:          "run.complete",
			ProfileID:   "sample",
			WorkflowID:  workflow.ID,
			Status:      store.StatusPassed,
			SummaryJSON: `{"summary":{"expectedStepCount":10,"stepCount":10,"passed":10},"steps":[{},{},{},{},{},{},{},{},{},{}]}`,
			CreatedAt:   started,
			UpdatedAt:   started,
		},
		{
			ID:          "run.partial",
			ProfileID:   "sample",
			WorkflowID:  workflow.ID,
			Status:      store.StatusFailed,
			SummaryJSON: `{"summary":{"expectedStepCount":10,"stepCount":7,"passed":6},"steps":[{},{},{},{},{},{},{}]}`,
			CreatedAt:   started.Add(time.Minute),
			UpdatedAt:   started.Add(time.Minute),
		},
	} {
		if _, err := s.CreateRun(ctx, item); err != nil {
			t.Fatalf("create run %s: %v", item.ID, err)
		}
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	workflows := payload["workflows"].([]any)
	alpha := workflows[0].(map[string]any)
	if alpha["runCount"] != float64(2) {
		t.Fatalf("workflow run count = %#v", alpha)
	}
	latest := alpha["latestRun"].(map[string]any)
	if latest["id"] != "run.complete" || latest["status"] != store.StatusPassed || latest["stepCount"] != float64(10) {
		t.Fatalf("catalog should expose latest complete workflow run: %#v", latest)
	}
}

func TestServerExposesDashboardSnapshotForReactShell(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/dashboard")
	if err != nil {
		t.Fatalf("get dashboard api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard api status = %d", resp.StatusCode)
	}

	var payload struct {
		Summary struct {
			Total     int `json:"total"`
			Missing   int `json:"missing"`
			Healthy   int `json:"healthy"`
			Unhealthy int `json:"unhealthy"`
		} `json:"summary"`
		Groups []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Items []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				State   string `json:"state"`
				Health  string `json:"health"`
				Kind    string `json:"kind"`
				OK      bool   `json:"ok"`
				Branch  string `json:"branch"`
				Profile string `json:"profile"`
			} `json:"items"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode dashboard api: %v", err)
	}
	if payload.Summary.Total != 1 || payload.Summary.Missing != 1 || payload.Summary.Healthy != 0 || payload.Summary.Unhealthy != 0 {
		t.Fatalf("dashboard summary = %#v", payload.Summary)
	}
	if len(payload.Groups) != 1 || payload.Groups[0].ID != "business" {
		t.Fatalf("dashboard groups = %#v", payload.Groups)
	}
	if payload.Groups[0].Label != "Services" {
		t.Fatalf("dashboard group label = %#v", payload.Groups[0])
	}
	if len(payload.Groups[0].Items) != 1 || payload.Groups[0].Items[0].ID != "service.alpha" || payload.Groups[0].Items[0].Name != "Service Alpha" || payload.Groups[0].Items[0].State != "missing" {
		t.Fatalf("dashboard items = %#v", payload.Groups[0].Items)
	}
	if payload.Groups[0].Items[0].Branch != "sample" || payload.Groups[0].Items[0].Profile != "sample" {
		t.Fatalf("dashboard item profile markers = %#v", payload.Groups[0].Items[0])
	}
}

func TestServerHydratesDashboardSnapshotFromDockerRuntime(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service-alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
	}
	fakeBin := t.TempDir()
	docker := filepath.Join(fakeBin, "docker")
	if err := os.WriteFile(docker, []byte(`#!/bin/sh
cat <<'JSON'
{"Names":"sandbox-service-alpha","Image":"example/service-alpha:1","State":"running","Status":"Up 12 seconds","Ports":"0.0.0.0:18080->8080/tcp, 0.0.0.0:19090->9090/tcp"}
JSON
`), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/dashboard")
	if err != nil {
		t.Fatalf("get dashboard api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard api status = %d", resp.StatusCode)
	}

	var payload struct {
		Summary struct {
			Total     int `json:"total"`
			Healthy   int `json:"healthy"`
			Missing   int `json:"missing"`
			Unhealthy int `json:"unhealthy"`
		} `json:"summary"`
		Groups []struct {
			Items []struct {
				ID             string `json:"id"`
				Container      string `json:"container"`
				Image          string `json:"image"`
				State          string `json:"state"`
				Health         string `json:"health"`
				Port           int    `json:"port"`
				ManagementPort int    `json:"managementPort"`
				OK             bool   `json:"ok"`
			} `json:"items"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode dashboard api: %v", err)
	}
	if payload.Summary.Total != 1 || payload.Summary.Healthy != 0 || payload.Summary.Missing != 0 || payload.Summary.Unhealthy != 1 {
		t.Fatalf("dashboard summary = %#v", payload.Summary)
	}
	item := payload.Groups[0].Items[0]
	if item.ID != "service-alpha" || item.OK || item.State != "running" || item.Health != "unchecked" {
		t.Fatalf("dashboard item state = %#v", item)
	}
	if item.Container != "sandbox-service-alpha" || item.Image != "example/service-alpha:1" {
		t.Fatalf("dashboard item runtime identity = %#v", item)
	}
	if item.Port != 18080 || item.ManagementPort != 19090 {
		t.Fatalf("dashboard item ports = %#v", item)
	}
}

func TestServerHydratesDashboardHealthFromHTTPCheck(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected health path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ready":true}`))
	}))
	defer target.Close()

	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service-alpha", DisplayName: "Service Alpha", Kind: "http", HealthURL: target.URL + "/health"},
		},
	}
	fakeBin := t.TempDir()
	docker := filepath.Join(fakeBin, "docker")
	if err := os.WriteFile(docker, []byte(`#!/bin/sh
cat <<'JSON'
{"Names":"sandbox-service-alpha","Image":"example/service-alpha:1","State":"running","Status":"Up 12 seconds","Ports":"0.0.0.0:18080->8080/tcp, 0.0.0.0:19090->9090/tcp"}
JSON
`), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/dashboard", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["total"] != float64(1) || summary["healthy"] != float64(1) || summary["missing"] != float64(0) {
		t.Fatalf("dashboard summary = %#v", summary)
	}
	groups := payload["groups"].([]any)
	item := groups[0].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["id"] != "service-alpha" || item["ok"] != true || item["state"] != "running" || item["health"] != "healthy" {
		t.Fatalf("dashboard item state = %#v", item)
	}
}

func TestServerHydratesDashboardHealthFromEnvironmentComponentGraph(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/actuator/health" {
			t.Fatalf("unexpected health path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP"}`))
	}))
	defer target.Close()

	now := time.Now().UTC()
	catalog := store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Services: []store.CatalogService{
			{ID: "service-alpha", DisplayName: "Service Alpha", Kind: "app", ContainerName: "sandbox-alpha", Status: "active"},
		},
	}
	if err := s.ReplaceProfileCatalog(ctx, catalog); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	readModel, err := controlplane.DashboardReadModel(catalog, "config.sample.001", now)
	if err != nil {
		t.Fatalf("build dashboard read model: %v", err)
	}
	if _, err := s.UpsertReadModel(ctx, readModel); err != nil {
		t.Fatalf("upsert dashboard read model: %v", err)
	}
	if _, err := s.UpsertEnvironment(ctx, store.Environment{
		ID:                     "env.sample",
		DisplayName:            "Sample Environment",
		Status:                 "draft",
		VerificationWorkflowID: "workflow.sample",
	}); err != nil {
		t.Fatalf("upsert environment: %v", err)
	}
	if err := s.ReplaceEnvironmentComponentGraph(ctx, "env.sample", store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{
				ComponentID:     "service-alpha",
				Kind:            "app",
				Role:            "business-service",
				ComposeService:  "service-alpha",
				Required:        true,
				HealthCheckJSON: fmt.Sprintf(`{"kind":"url","url":%q}`, target.URL+"/actuator/health"),
			},
		},
	}); err != nil {
		t.Fatalf("replace component graph: %v", err)
	}
	fakeBin := t.TempDir()
	docker := filepath.Join(fakeBin, "docker")
	if err := os.WriteFile(docker, []byte(`#!/bin/sh
cat <<'JSON'
{"Names":"sandbox-alpha","Image":"example/service-alpha:1","State":"running","Status":"Up 12 seconds","Ports":"0.0.0.0:18080->8080/tcp"}
JSON
`), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/dashboard", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["healthy"] != float64(1) || summary["unhealthy"] != float64(0) {
		t.Fatalf("dashboard summary = %#v", summary)
	}
	item := payload["groups"].([]any)[0].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["id"] != "service-alpha" || item["ok"] != true || item["health"] != "healthy" {
		t.Fatalf("dashboard item should use component graph HTTP health: %#v", item)
	}
}

func TestServerUsesRuntimeCatalogForDashboardSnapshot(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	now := time.Now().UTC()
	catalog := store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Services: []store.CatalogService{
			{
				ID: "service.alpha", DisplayName: "Alpha Service", Kind: "app", ContainerName: "sandbox-alpha",
				Image: "example/alpha:1", ServicePort: 18080, ManagementPort: 19090, SourcePath: "/tmp/alpha", GitBranch: "main", Status: "active", SortOrder: 1,
			},
			{
				ID: "service.beta", DisplayName: "Beta Service", Kind: "app", ContainerName: "sandbox-beta",
				Image: "example/beta:1", ServicePort: 18081, ManagementPort: 19091, SourcePath: "/tmp/runtime/service/beta-4e8d26674209", Status: "active", SortOrder: 2,
			},
			{
				ID: "service.retired", DisplayName: "Retired Service", Kind: "app", ContainerName: "sandbox-retired",
				Image: "example/retired:1", ServicePort: 18082, ManagementPort: 19092, Status: "inactive", SortOrder: 3,
			},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.environment.default",
				TemplateID: "TPL-ENVIRONMENT-NODE-LIST-V1",
				ScopeType:  "environment",
				ScopeID:    "_default",
				Title:      "Default environment presentation",
				ConfigJSON: `{"copy":{"listTitle":"Configured environments","detailTitle":"Configured service detail","runtimeTitle":"Configured runtime","connectionTitle":"Configured connection","openServicePort":"Open configured service"}}`,
				Status:     "active",
			},
			{
				ID:         "cfg.environment.service.alpha",
				TemplateID: "TPL-ENVIRONMENT-NODE-DETAIL-V1",
				NodeID:     "service.alpha",
				ScopeType:  "environment-node",
				ScopeID:    "service.alpha",
				Title:      "Alpha environment presentation",
				ConfigJSON: `{"copy":{"detailTitle":"Alpha configured detail","runtimeTitle":"Alpha runtime"}}`,
				Status:     "active",
			},
		},
	}
	if err := s.ReplaceProfileCatalog(ctx, catalog); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	readModel, err := controlplane.DashboardReadModel(catalog, "config.sample.001", now)
	if err != nil {
		t.Fatalf("build dashboard read model: %v", err)
	}
	if _, err := s.UpsertReadModel(ctx, readModel); err != nil {
		t.Fatalf("upsert dashboard read model: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/dashboard", http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("dashboard envelope = %#v", payload)
	}
	source := payload["source"].(map[string]any)
	if source["kind"] != "read-model" || source["id"] != "sample" {
		t.Fatalf("dashboard source = %#v", source)
	}
	presentation := payload["presentation"].(map[string]any)
	copy := presentation["copy"].(map[string]any)
	if copy["listTitle"] != "Configured environments" || copy["connectionTitle"] != "Configured connection" {
		t.Fatalf("dashboard presentation copy = %#v", copy)
	}
	groups := payload["groups"].([]any)
	items := groups[0].(map[string]any)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("dashboard should hide inactive services, items = %#v", items)
	}
	item := items[0].(map[string]any)
	if item["id"] != "service.alpha" || item["container"] != "sandbox-alpha" || item["port"] != float64(18080) || item["managementPort"] != float64(19090) {
		t.Fatalf("dashboard item = %#v", item)
	}
	itemCopy := item["presentation"].(map[string]any)["copy"].(map[string]any)
	if itemCopy["detailTitle"] != "Alpha configured detail" || itemCopy["runtimeTitle"] != "Alpha runtime" || itemCopy["openServicePort"] != "Open configured service" {
		t.Fatalf("dashboard item presentation = %#v", itemCopy)
	}
	runtimes := payload["serviceRuntime"].([]any)
	runtimeByID := map[string]map[string]any{}
	for _, raw := range runtimes {
		runtime := raw.(map[string]any)
		runtimeByID[fmt.Sprint(runtime["serviceId"])] = runtime
	}
	alphaRuntime := runtimeByID["service.alpha"]
	if alphaRuntime["branchName"] != "main" || alphaRuntime["sourcePath"] != "/tmp/alpha" {
		t.Fatalf("dashboard alpha runtime = %#v", alphaRuntime)
	}
	betaRuntime := runtimeByID["service.beta"]
	if betaRuntime["branchName"] != "beta" || betaRuntime["commitId"] != "4e8d26674209" || betaRuntime["sourcePath"] != "/tmp/runtime/service/beta-4e8d26674209" {
		t.Fatalf("dashboard beta runtime = %#v", betaRuntime)
	}
	if _, ok := runtimeByID["service.retired"]; ok {
		t.Fatalf("dashboard runtime should hide inactive services: %#v", runtimeByID["service.retired"])
	}
}

func TestServerUsesRuntimeCatalogForInterfaceNodeDetails(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Services: []store.CatalogService{
			{ID: "entry-service", DisplayName: "Entry", Kind: "app"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{
				ID: "interface.alpha", DisplayName: "Alpha", ServiceID: "entry-service", Operation: "alpha.create",
				Method: "POST", Path: "/alpha", TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1", Version: "v1",
				Status: "draft", Tags: []string{"baseline", "alpha"}, Description: "Alpha interface node", SortOrder: 7,
				CreatedAt: "2026-05-12 12:54:33", UpdatedAt: "2026-05-12 12:55:33",
			},
		},
		RequestTemplates: []store.CatalogRequestTemplate{
			{ID: "tpl.alpha", DisplayName: "Alpha template", NodeID: "interface.alpha", Method: "POST", Path: "/alpha", TemplateJSON: `{"name":"default"}`, Version: "v1", Status: "active", SortOrder: 1},
		},
		InterfaceFields: []store.CatalogInterfaceNodeField{
			{ID: "field.alpha.name", NodeID: "interface.alpha", Direction: "request", FieldPath: "$.name", DisplayName: "name", DataType: "string", Required: true, Bindable: true, PortType: "DATA", Status: "active", SortOrder: 1},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha.failure", DisplayName: "Alpha failure", NodeID: "interface.alpha", CaseType: "failure", Scenario: "required field", RequestTemplateID: "tpl.alpha", PatchJSON: `[{"op":"remove","path":"$.name"}]`, RenderMode: "template_patch", ExpectedJSON: `{"expectedHttpCodes":[400]}`, RequiredForAdmission: true, Status: "active", SortOrder: 1},
			{ID: "case.alpha.success", DisplayName: "Alpha success", NodeID: "interface.alpha", CaseType: "success", PayloadTemplateJSON: `{}`, PatchJSON: `[]`, ExpectedJSON: `{}`, RequiredForAdmission: false, Status: "active", SortOrder: 2},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.interface-node.default",
				TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
				ScopeType:  "interface-node",
				ScopeID:    "_default",
				Title:      "Default interface node presentation",
				ConfigJSON: `{"copy":{"casesTitle":"Default cases","runAllButton":"Run all default cases","emptyCases":"No configured cases.","historyTitle":"Configured history"}}`,
				Status:     "active",
			},
			{
				ID:         "cfg.interface.alpha",
				TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
				NodeID:     "interface.alpha",
				ScopeType:  "interface-node",
				ScopeID:    "interface.alpha",
				Title:      "Alpha interface node",
				ConfigJSON: `{"copy":{"casesTitle":"Configured cases","runAllButton":"Run configured cases","emptyCases":"No configured cases."}}`,
				Status:     "active",
			},
		},
		CaseDependencies: []store.CatalogCaseDependency{
			{ID: "dep.alpha", CaseID: "case.alpha.failure", FixtureID: "fixture.alpha", Required: true, MappingsJSON: `[{"from":"$.id","to":"$.name"}]`, Status: "active", SortOrder: 1},
		},
		Fixtures: []store.CatalogFixture{
			{ID: "fixture.alpha", DisplayName: "Alpha fixture", Kind: "sql", DataJSON: "fixture data"},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/interface-node?id=interface.alpha")
	if err != nil {
		t.Fatalf("get interface node: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var payload struct {
		OK         bool              `json:"ok"`
		TemplateID string            `json:"templateId"`
		Source     map[string]string `json:"source"`
		Node       struct {
			ID          string   `json:"id"`
			Method      string   `json:"method"`
			Path        string   `json:"path"`
			TemplateID  string   `json:"templateId"`
			Version     string   `json:"version"`
			Status      string   `json:"status"`
			Tags        []string `json:"tags"`
			Description string   `json:"description"`
			SortOrder   int      `json:"sortOrder"`
			CreatedAt   string   `json:"createdAt"`
			UpdatedAt   string   `json:"updatedAt"`
		} `json:"node"`
		RequestTemplates []map[string]any `json:"requestTemplates"`
		Cases            []map[string]any `json:"cases"`
		Fields           struct {
			Request []map[string]any `json:"request"`
		} `json:"fields"`
		Presentation struct {
			Copy struct {
				CasesTitle   string `json:"casesTitle"`
				RunAllButton string `json:"runAllButton"`
				EmptyCases   string `json:"emptyCases"`
				HistoryTitle string `json:"historyTitle"`
			} `json:"copy"`
		} `json:"presentation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !payload.OK || payload.TemplateID != "TPL-INTERFACE-NODE-CASE-LIST-V1" || payload.Source["kind"] != "store" {
		t.Fatalf("interface detail envelope = %#v", payload)
	}
	if payload.Node.ID != "interface.alpha" || payload.Node.Method != "POST" || payload.Node.Path != "/alpha" {
		t.Fatalf("node payload = %#v", payload.Node)
	}
	if payload.Node.TemplateID != "TPL-INTERFACE-NODE-CASE-LIST-V1" || payload.Node.Version != "v1" || payload.Node.Status != "draft" {
		t.Fatalf("node metadata = %#v", payload.Node)
	}
	if len(payload.Node.Tags) != 2 || payload.Node.Tags[0] != "baseline" || payload.Node.Description != "Alpha interface node" || payload.Node.SortOrder != 7 {
		t.Fatalf("node catalog metadata = %#v", payload.Node)
	}
	if payload.Node.CreatedAt != "2026-05-12 12:54:33" || payload.Node.UpdatedAt != "2026-05-12 12:55:33" {
		t.Fatalf("node timestamps = %#v", payload.Node)
	}
	if len(payload.RequestTemplates) != 1 || payload.RequestTemplates[0]["id"] != "tpl.alpha" {
		t.Fatalf("request templates = %#v", payload.RequestTemplates)
	}
	if len(payload.Fields.Request) != 1 || payload.Fields.Request[0]["fieldPath"] != "$.name" {
		t.Fatalf("request fields = %#v", payload.Fields.Request)
	}
	if len(payload.Cases) != 2 || payload.Cases[0]["caseType"] != "failure" || payload.Cases[0]["requiredForAdmission"] != true || payload.Cases[0]["requestTemplateId"] != "tpl.alpha" {
		t.Fatalf("cases = %#v", payload.Cases)
	}
	successCase := payload.Cases[1]
	for _, key := range []string{"blocked", "blockedReason", "scenario", "requestTemplateId"} {
		if _, ok := successCase[key]; !ok {
			t.Fatalf("case should expose stable key %q: %#v", key, successCase)
		}
	}
	if successCase["blocked"] != false || successCase["blockedReason"] != "" || successCase["scenario"] != "" || successCase["requestTemplateId"] != "" {
		t.Fatalf("case empty contract fields = %#v", successCase)
	}
	if payload.Presentation.Copy.CasesTitle != "Configured cases" ||
		payload.Presentation.Copy.RunAllButton != "Run configured cases" ||
		payload.Presentation.Copy.EmptyCases != "No configured cases." ||
		payload.Presentation.Copy.HistoryTitle != "Configured history" {
		t.Fatalf("interface node presentation copy = %#v", payload.Presentation.Copy)
	}
}

func TestServerUsesRuntimeCatalogForWorkflowDirectory(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	services := []store.CatalogService{
		{ID: "entry-service", DisplayName: "Entry", Kind: "app"},
		{ID: "worker-service", DisplayName: "Worker", Kind: "app"},
	}
	stepServices := []string{"entry-service", "worker-service", "entry-service"}
	nodes := make([]store.CatalogInterfaceNode, 0, len(stepServices))
	cases := make([]store.CatalogAPICase, 0, len(stepServices))
	bindings := make([]store.CatalogWorkflowBinding, 0, len(stepServices))
	for i, serviceID := range stepServices {
		sortOrder := i + 1
		nodeID := fmt.Sprintf("interface.step.%02d", sortOrder)
		caseID := fmt.Sprintf("case.step.%02d", sortOrder)
		nodes = append(nodes, store.CatalogInterfaceNode{
			ID:          nodeID,
			DisplayName: fmt.Sprintf("Step %02d Interface", sortOrder),
			ServiceID:   serviceID,
			Operation:   fmt.Sprintf("step.%02d", sortOrder),
			Method:      "POST",
			Path:        fmt.Sprintf("/steps/%02d", sortOrder),
			Status:      "active",
			TimeoutMs:   sortOrder * 100,
			SortOrder:   sortOrder,
		})
		cases = append(cases, store.CatalogAPICase{
			ID:                   caseID,
			DisplayName:          fmt.Sprintf("Step %02d Case", sortOrder),
			NodeID:               nodeID,
			CaseType:             "happy_path",
			RequiredForAdmission: true,
			Status:               "active",
			SortOrder:            sortOrder,
		})
		bindings = append(bindings, store.CatalogWorkflowBinding{
			WorkflowID: "workflow.primary",
			StepID:     fmt.Sprintf("step-%02d", sortOrder),
			NodeID:     nodeID,
			CaseID:     caseID,
			Required:   true,
			SortOrder:  sortOrder,
		})
	}

	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		Services:  services,
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.primary", DisplayName: "Primary Workflow", Description: "Runs the primary workflow.", BaseStepTimeoutMs: 300, TimeoutOffsetMs: 50},
		},
		InterfaceNodes:   nodes,
		APICases:         cases,
		WorkflowBindings: bindings,
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:          "cfg.workflow.primary",
				TemplateID:  "TPL-WORKFLOW-LONG-CHAIN-V1",
				WorkflowID:  "workflow.primary",
				ScopeType:   "workflow",
				ScopeID:     "workflow.primary",
				Title:       "Primary Workflow",
				Description: "Runs the primary workflow from runtime template configuration.",
				ConfigJSON:  `{"copy":{"runButton":"Run configured flow","coverageTitle":"Configured coverage","coverageEmpty":"No configured mappings."}}`,
				Status:      "needs-business-input",
				SortOrder:   1,
			},
			{
				ID:         "cfg.workflow-step.default",
				TemplateID: "TPL-WORKFLOW-STEP-V1",
				WorkflowID: "workflow.primary",
				ScopeType:  "step",
				ScopeID:    "_default",
				Title:      "Default workflow step presentation",
				ConfigJSON: `{"copy":{"topologyTitle":"Configured topology","requestTitle":"Configured request","responseTitle":"Configured response","logsTitle":"Configured logs","emptyRun":"No configured step run."}}`,
				Status:     "active",
				SortOrder:  0,
			},
			{
				ID:         "cfg.step.one",
				TemplateID: "TPL-WORKFLOW-STEP-V1",
				WorkflowID: "workflow.primary",
				NodeID:     "entry-service",
				ScopeType:  "step",
				ScopeID:    "step-01",
				Title:      "Entry step",
				ConfigJSON: `{"serviceId":"worker-service","evidenceKinds":["request","response","logs"],"relatedMockTargets":["mock-a"],"inputs":[{"name":"order_id","source":"previous","required":false}],"exports":[{"name":"request_id","from":"response","path":"request_id"}],"copy":{"topologyTitle":"Entry topology"}}`,
				Status:     "active",
				SortOrder:  1,
			},
			{
				ID:         "cfg.step.two",
				TemplateID: "TPL-WORKFLOW-STEP-V1",
				WorkflowID: "workflow.primary",
				NodeID:     "entry-service",
				ScopeType:  "step",
				ScopeID:    "step-02",
				Title:      "Worker step",
				Status:     "active",
				SortOrder:  2,
			},
			{
				ID:         "cfg.edge.entry.worker",
				TemplateID: "TPL-ENVIRONMENT-OVERVIEW-V1",
				ScopeType:  "topology-edge",
				ScopeID:    "entry-service->worker-service",
				ConfigJSON: `{"from":"entry-service","to":"worker-service"}`,
				Status:     "active",
				SortOrder:  1,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:          "run.primary",
		ProfileID:   "sample",
		WorkflowID:  "workflow.primary",
		Status:      store.StatusPassed,
		SummaryJSON: `{"status":"passed","steps":[{"stepId":"step-01","elapsedMs":123},{"stepId":"step-02","elapsedMs":456}],"summary":{"stepCount":2,"elapsedMs":579}}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Workflows: []profile.Workflow{
			{ID: "workflow.profile", DisplayName: "Profile Workflow"},
		},
	}, s))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/catalog")
	if err != nil {
		t.Fatalf("get catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("catalog status = %d", resp.StatusCode)
	}

	var payload struct {
		OK          bool              `json:"ok"`
		GeneratedAt string            `json:"generatedAt"`
		Navigation  map[string]any    `json:"navigation"`
		Warnings    []string          `json:"warnings"`
		Source      map[string]string `json:"source"`
		Services    []struct {
			ID string `json:"id"`
		} `json:"services"`
		Workflows []struct {
			ID                string `json:"id"`
			DisplayName       string `json:"displayName"`
			Description       string `json:"description"`
			Entrypoint        string `json:"entrypoint"`
			StepCount         int    `json:"stepCount"`
			CaseCount         int    `json:"caseCount"`
			ServiceCount      int    `json:"serviceCount"`
			BaseStepTimeoutMs int    `json:"baseStepTimeoutMs"`
			TimeoutOffsetMs   int    `json:"timeoutOffsetMs"`
			TimeoutMs         int    `json:"timeoutMs"`
			Graph             struct {
				Nodes []string `json:"nodes"`
				Edges []struct {
					From string `json:"from"`
					To   string `json:"to"`
				} `json:"edges"`
			} `json:"graph"`
			Observability struct {
				Panels []struct {
					ID string `json:"id"`
				} `json:"panels"`
			} `json:"observability"`
			Presentation struct {
				Template string `json:"template"`
				Title    string `json:"title"`
				Copy     struct {
					RunButton     string `json:"runButton"`
					CoverageTitle string `json:"coverageTitle"`
					CoverageEmpty string `json:"coverageEmpty"`
				} `json:"copy"`
				Stages []struct {
					ID    string `json:"id"`
					Steps []struct {
						ID    string `json:"id"`
						Title string `json:"title"`
					} `json:"steps"`
				} `json:"stages"`
			} `json:"presentation"`
			RunCount  int `json:"runCount"`
			LatestRun struct {
				ID      string `json:"id"`
				Summary struct {
					Steps []struct {
						StepID    string `json:"stepId"`
						ElapsedMs int    `json:"elapsedMs"`
					} `json:"steps"`
				} `json:"summary"`
			} `json:"latestRun"`
			Steps []struct {
				ID                 string           `json:"id"`
				DisplayName        string           `json:"displayName"`
				ServiceID          string           `json:"serviceId"`
				CaseID             string           `json:"caseId"`
				Required           bool             `json:"required"`
				Executable         bool             `json:"executable"`
				EvidenceKinds      []string         `json:"evidenceKinds"`
				RelatedMockTargets []string         `json:"relatedMockTargets"`
				Inputs             []map[string]any `json:"inputs"`
				Exports            []map[string]any `json:"exports"`
				TimeoutMs          int              `json:"timeoutMs"`
				Presentation       struct {
					Copy struct {
						TopologyTitle string `json:"topologyTitle"`
						RequestTitle  string `json:"requestTitle"`
						ResponseTitle string `json:"responseTitle"`
						LogsTitle     string `json:"logsTitle"`
						EmptyRun      string `json:"emptyRun"`
					} `json:"copy"`
				} `json:"presentation"`
			} `json:"steps"`
		} `json:"workflows"`
		APICases []struct {
			ID     string `json:"id"`
			NodeID string `json:"nodeId"`
		} `json:"apiCases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if !payload.OK || payload.GeneratedAt == "" || payload.Navigation == nil || payload.Warnings == nil {
		t.Fatalf("catalog envelope = %#v", payload)
	}
	if payload.Source["kind"] != "store" || payload.Source["id"] != "sample" {
		t.Fatalf("catalog source = %#v", payload.Source)
	}
	if len(payload.Services) != len(services) || len(payload.APICases) != len(cases) {
		t.Fatalf("catalog inventory counts services=%d cases=%d", len(payload.Services), len(payload.APICases))
	}
	if len(payload.Workflows) != 1 {
		t.Fatalf("workflow count = %d, payload=%#v", len(payload.Workflows), payload.Workflows)
	}
	workflow := payload.Workflows[0]
	if workflow.ID != "workflow.primary" {
		t.Fatalf("workflow id = %q", workflow.ID)
	}
	if workflow.DisplayName != "Primary Workflow" || workflow.Description == "" {
		t.Fatalf("workflow metadata = %#v", workflow)
	}
	if workflow.StepCount != len(bindings) || workflow.CaseCount != len(bindings) || workflow.ServiceCount != 2 {
		t.Fatalf("workflow summary counts = %#v", workflow)
	}
	if workflow.BaseStepTimeoutMs != 300 || workflow.TimeoutOffsetMs != 50 || workflow.TimeoutMs != 650 {
		t.Fatalf("workflow timeout budget = %#v", workflow)
	}
	if workflow.Entrypoint != "/workflow-detail.html?id=workflow.primary" {
		t.Fatalf("workflow entrypoint = %q", workflow.Entrypoint)
	}
	if len(workflow.Graph.Nodes) != 2 || len(workflow.Graph.Edges) != 1 || workflow.Graph.Edges[0].From != "entry-service" || workflow.Graph.Edges[0].To != "worker-service" {
		t.Fatalf("workflow graph = %#v", workflow.Graph)
	}
	if len(workflow.Observability.Panels) != 5 || workflow.Observability.Panels[0].ID != "workflowGraph" {
		t.Fatalf("workflow observability = %#v", workflow.Observability)
	}
	if workflow.Presentation.Template != "workflowStudio" || workflow.Presentation.Title != "Primary Workflow" || len(workflow.Presentation.Stages) != 1 || workflow.Presentation.Stages[0].Steps[0].Title != "Entry step" {
		t.Fatalf("workflow presentation = %#v", workflow.Presentation)
	}
	if workflow.Presentation.Copy.RunButton != "Run configured flow" || workflow.Presentation.Copy.CoverageTitle != "Configured coverage" || workflow.Presentation.Copy.CoverageEmpty != "No configured mappings." {
		t.Fatalf("workflow presentation copy = %#v", workflow.Presentation.Copy)
	}
	if workflow.RunCount != 1 || workflow.LatestRun.ID != "run.primary" || len(workflow.LatestRun.Summary.Steps) != 0 {
		t.Fatalf("workflow run state = %#v", workflow)
	}
	if len(workflow.Steps) != len(bindings) {
		t.Fatalf("workflow step count = %d", len(workflow.Steps))
	}
	for i, step := range workflow.Steps {
		wantStep := fmt.Sprintf("step-%02d", i+1)
		wantCase := fmt.Sprintf("case.step.%02d", i+1)
		wantService := stepServices[i]
		if i == 0 {
			wantService = "worker-service"
		}
		if i == 1 {
			wantService = "entry-service"
		}
		if step.ID != wantStep || step.CaseID != wantCase || step.ServiceID != wantService || !step.Required {
			t.Fatalf("step %d = %#v", i, step)
		}
		if step.TimeoutMs != (i+1)*100 {
			t.Fatalf("step timeout %d = %#v", i, step)
		}
		if i == 0 && step.DisplayName != "Entry step" {
			t.Fatalf("step template title = %#v", step)
		}
		if i == 0 {
			if !step.Executable || len(step.EvidenceKinds) != 3 || step.EvidenceKinds[0] != "request" || len(step.RelatedMockTargets) != 1 {
				t.Fatalf("step runtime metadata = %#v", step)
			}
			if len(step.Inputs) != 1 || step.Inputs[0]["name"] != "order_id" || len(step.Exports) != 1 || step.Exports[0]["name"] != "request_id" {
				t.Fatalf("step inputs/exports = %#v", step)
			}
			if step.Presentation.Copy.TopologyTitle != "Entry topology" ||
				step.Presentation.Copy.RequestTitle != "Configured request" ||
				step.Presentation.Copy.ResponseTitle != "Configured response" ||
				step.Presentation.Copy.LogsTitle != "Configured logs" ||
				step.Presentation.Copy.EmptyRun != "No configured step run." {
				t.Fatalf("step presentation copy = %#v", step.Presentation.Copy)
			}
		}
	}
}

func TestServerPersistsBatchCaseRunsForInterfaceNodeGreenState(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "interface.alpha", DisplayName: "Alpha", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha.one", DisplayName: "Alpha one", NodeID: "interface.alpha", CaseType: "success", RequiredForAdmission: true, Status: "active"},
			{ID: "case.alpha.two", DisplayName: "Alpha two", NodeID: "interface.alpha", CaseType: "success", RequiredForAdmission: true, Status: "active"},
			{ID: "case.alpha.optional", DisplayName: "Alpha optional", NodeID: "interface.alpha", CaseType: "success", RequiredForAdmission: false, Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{ID: "cfg.one", ScopeType: "step", ScopeID: "one", Status: "active", ConfigJSON: `{"caseId":"case.alpha.one","caseExecution":{"method":"GET","nodeId":"service.alpha","path":"/ok","expectedHttpCodes":[200]}}`},
			{ID: "cfg.two", ScopeType: "step", ScopeID: "two", Status: "active", ConfigJSON: `{"caseId":"case.alpha.two","caseExecution":{"method":"GET","nodeId":"service.alpha","path":"/ok","expectedHttpCodes":[200]}}`},
			{ID: "cfg.optional", ScopeType: "step", ScopeID: "optional", Status: "active", ConfigJSON: `{"caseId":"case.alpha.optional","caseExecution":{"method":"GET","nodeId":"service.alpha","path":"/ok","expectedHttpCodes":[200]}}`},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run-batch", "application/json", strings.NewReader(fmt.Sprintf(`{"caseIds":["case.alpha.one","case.alpha.two","case.alpha.optional"],"baseUrl":%q}`, target.URL)))
	if err != nil {
		t.Fatalf("post batch: %v", err)
	}
	var batchPayload struct {
		Results []struct {
			RunID     string `json:"runId"`
			CaseRunID string `json:"caseRunId"`
			DetailURL string `json:"detailUrl"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batchPayload); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close batch response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch status = %d", resp.StatusCode)
	}
	if len(batchPayload.Results) != 3 {
		t.Fatalf("batch results = %#v", batchPayload.Results)
	}
	for _, item := range batchPayload.Results {
		if item.RunID == "" || item.CaseRunID != item.RunID+".case" || item.DetailURL != "/api/case-run/evidence?caseRunId="+url.QueryEscape(item.CaseRunID) {
			t.Fatalf("batch case evidence handles = %#v", item)
		}
	}
	detail := decodeJSONResponse(t, server.URL+batchPayload.Results[0].DetailURL, http.StatusOK)
	if detail["ok"] != true {
		t.Fatalf("batch case detail lookup = %#v", detail)
	}

	resp, err = http.Get(server.URL + "/api/interface-node?id=interface.alpha")
	if err != nil {
		t.Fatalf("get interface node: %v", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Admission struct {
			Status          string `json:"status"`
			PassedCaseCount int    `json:"passedCaseCount"`
		} `json:"admission"`
		Cases []struct {
			ID        string         `json:"id"`
			LatestRun map[string]any `json:"latestRun"`
		} `json:"cases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode interface node: %v", err)
	}
	if payload.Admission.Status != store.StatusPassed || payload.Admission.PassedCaseCount != 2 {
		t.Fatalf("admission = %#v", payload.Admission)
	}
	for _, item := range payload.Cases {
		if item.LatestRun["status"] != store.StatusPassed {
			t.Fatalf("case %s latest run = %#v", item.ID, item.LatestRun)
		}
	}

	list := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	if list["ok"] != true || list["templateId"] != "TPL-INTERFACE-NODE-CASE-LIST-V1" {
		t.Fatalf("interface node list envelope = %#v", list)
	}
	filters := list["filters"].(map[string]any)
	if filters["serviceId"] != "" || filters["operation"] != "" {
		t.Fatalf("interface node list filters = %#v", filters)
	}
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("interface node list = %#v", list)
	}
	row := items[0].(map[string]any)
	if row["status"] != "active" || row["admissionStatus"] != store.StatusPassed || row["passedCaseCount"] != float64(2) {
		t.Fatalf("interface node list row = %#v", row)
	}
	if row["operation"] != "Alpha" || row["latestRunId"] == "" {
		t.Fatalf("interface node list row = %#v", row)
	}
}

func TestServerUsesLatestPassingCacheForDirectInterfaceAdmission(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "interface.alpha", DisplayName: "Alpha", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Alpha", NodeID: "interface.alpha", CaseType: "success", RequiredForAdmission: true, Status: "active"},
			{ID: "case.alpha.optional", DisplayName: "Alpha optional", NodeID: "interface.alpha", CaseType: "failure", RequiredForAdmission: false, Status: "active"},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	for _, run := range []store.Run{
		{ID: "run.pass", ProfileID: "sample", WorkflowID: "workflow.alpha", Status: store.StatusPassed, StartedAt: now, FinishedAt: now.Add(100 * time.Millisecond), CreatedAt: now, UpdatedAt: now},
		{ID: "run.fail", ProfileID: "sample", WorkflowID: "case.alpha", Status: store.StatusFailed, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute + 200*time.Millisecond), CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)},
	} {
		if _, err := s.CreateRun(ctx, run); err != nil {
			t.Fatalf("create run %s: %v", run.ID, err)
		}
	}
	for _, item := range []store.APICaseRun{
		{ID: "run.pass.case", RunID: "run.pass", CaseID: "case.alpha", Status: store.StatusPassed, AssertionSummaryJSON: `{"status":"passed"}`, StartedAt: now, FinishedAt: now.Add(100 * time.Millisecond), CreatedAt: now},
		{ID: "run.fail.case", RunID: "run.fail", CaseID: "case.alpha", Status: store.StatusFailed, AssertionSummaryJSON: `{"status":"failed"}`, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute + 200*time.Millisecond), CreatedAt: now.Add(time.Minute)},
		{ID: "run.fail.optional", RunID: "run.fail", CaseID: "case.alpha.optional", Status: store.StatusFailed, AssertionSummaryJSON: `{"status":"failed"}`, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute + 300*time.Millisecond), CreatedAt: now.Add(time.Minute)},
	} {
		if _, err := s.RecordAPICaseRun(ctx, item); err != nil {
			t.Fatalf("record case run %s: %v", item.ID, err)
		}
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	list := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	row := list["items"].([]any)[0].(map[string]any)
	if row["admissionStatus"] != store.StatusPassed || row["latestRunId"] != "run.pass" || row["latestElapsedMs"] != float64(100) {
		t.Fatalf("interface list should prefer cached pass: %#v", row)
	}
	detail := decodeJSONResponse(t, server.URL+"/api/interface-node?id=interface.alpha", http.StatusOK)
	admission := detail["admission"].(map[string]any)
	if admission["status"] != store.StatusPassed || admission["latestRunId"] != "run.pass" {
		t.Fatalf("interface detail should prefer cached pass: %#v", admission)
	}
}

func TestServerExplainsInterfaceAdmissionBlockersFromStoreRuns(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "interface.blocked", DisplayName: "Blocked", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.passed", DisplayName: "Passed case", NodeID: "interface.blocked", CaseType: "success", RequiredForAdmission: true, Status: "active", SortOrder: 1},
			{ID: "case.failed", DisplayName: "Failed case", NodeID: "interface.blocked", CaseType: "success", RequiredForAdmission: true, Status: "active", SortOrder: 2},
			{ID: "case.missing", DisplayName: "Missing case", NodeID: "interface.blocked", CaseType: "failure", RequiredForAdmission: true, Status: "active", SortOrder: 3},
			{ID: "case.optional", DisplayName: "Optional case", NodeID: "interface.blocked", CaseType: "failure", RequiredForAdmission: false, Status: "active", SortOrder: 4},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{ID: "run.blocked", ProfileID: "sample", WorkflowID: "workflow.blocked", Status: store.StatusFailed, SummaryJSON: `{}`})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	for _, item := range []store.APICaseRun{
		{ID: "run.blocked.case.passed", RunID: "run.blocked", CaseID: "case.passed", Status: store.StatusPassed, AssertionSummaryJSON: `{"status":"passed"}`},
		{ID: "run.blocked.case.failed", RunID: "run.blocked", CaseID: "case.failed", Status: store.StatusFailed, AssertionSummaryJSON: `{"status":"failed","errorCount":1,"failureKind":"assertion"}`},
		{ID: "run.blocked.case.optional", RunID: "run.blocked", CaseID: "case.optional", Status: store.StatusFailed, AssertionSummaryJSON: `{"status":"failed","errorCount":1}`},
	} {
		if _, err := s.RecordAPICaseRun(ctx, item); err != nil {
			t.Fatalf("record api case run: %v", err)
		}
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node?id=interface.blocked", http.StatusOK)
	admission := payload["admission"].(map[string]any)
	if admission["status"] != store.StatusFailed || admission["requiredCaseCount"] != float64(3) || admission["passedCaseCount"] != float64(1) {
		t.Fatalf("admission = %#v", admission)
	}
	blockers := admission["blockers"].([]any)
	if len(blockers) != 2 {
		t.Fatalf("blockers = %#v", admission)
	}
	failed := blockers[0].(map[string]any)
	missing := blockers[1].(map[string]any)
	if failed["caseId"] != "case.failed" || failed["status"] != store.StatusFailed || failed["runId"] != "run.blocked" || failed["failureReason"] != "assertion errors: 1" || failed["failureKind"] != "assertion" || failed["evidenceHref"] != "/evidence-viewer.html?caseRun=run.blocked&caseId=case.failed" {
		t.Fatalf("failed blocker = %#v", failed)
	}
	if missing["caseId"] != "case.missing" || missing["status"] != "missing_run" || missing["failureReason"] != "required case has no run" {
		t.Fatalf("missing blocker = %#v", missing)
	}
	attention := payload["attention"].(map[string]any)
	if attention["status"] != store.StatusFailed || attention["blockerCount"] != float64(2) {
		t.Fatalf("attention = %#v", attention)
	}
}

func TestServerDoesNotServeLegacyTopLevelScripts(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	for _, path := range []string{"/dashboard.js", "/workflows.js"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestServerExposesEmptyRunListsForReactShell(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/runs")
	if err != nil {
		t.Fatalf("get runs api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("runs status = %d", resp.StatusCode)
	}

	var payload struct {
		OK           bool             `json:"ok"`
		WorkflowRuns []map[string]any `json:"workflowRuns"`
		ReplayRuns   []map[string]any `json:"replayRuns"`
		ProbeRuns    []map[string]any `json:"probeRuns"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode runs api: %v", err)
	}
	if !payload.OK {
		t.Fatalf("runs should expose ok envelope: %#v", payload)
	}
	if payload.WorkflowRuns == nil || payload.ReplayRuns == nil || payload.ProbeRuns == nil {
		t.Fatalf("runs should encode empty arrays: %#v", payload)
	}
}

func TestServerExposesWorkflowRunContracts(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  `{"summary":{"expectedStepCount":2,"stepCount":2},"steps":[{"stepId":"step.alpha","ok":true},{"stepId":"step.beta","ok":false,"summary":{"httpCode":200},"result":{"response":{"statusCode":200}}}]}`,
		CreatedAt:    started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:          "run.beta",
		ProfileID:   "sample",
		WorkflowID:  "workflow.alpha",
		Status:      store.StatusPassed,
		SummaryJSON: `{"kind":"apiCase","summary":{"httpCode":200},"steps":[{"stepId":"step.beta","caseId":"case.beta","ok":true,"summary":{"httpCode":200},"result":{"response":{"statusCode":200,"body":"{}"}}}]}`,
		CreatedAt:   started.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create incomplete run: %v", err)
	}
	_, err = s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.alpha",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.beta",
		CaseID:        "case.beta",
		RequestID:     "request.beta",
		TraceID:       "trace.beta",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"service.alpha","target":"service.beta"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.alpha","service.beta"]}`,
		TextTopology:  "service.alpha -> service.beta",
	})
	if err != nil {
		t.Fatalf("save topology: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "runtime-logs.json")
	if err := os.WriteFile(logPath, []byte(`{"systems":[{"name":"worker","found":true,"coreLogs":["request.beta handled"]}]}`), 0o644); err != nil {
		t.Fatalf("write runtime logs: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "runtime.logs.step.beta",
		RunID:     "run.alpha",
		CaseRunID: "step.beta",
		Kind:      "runtime_logs",
		URI:       logPath,
		MediaType: "application/json",
		Summary:   `{"stepId":"step.beta"}`,
		CreatedAt: started,
	})
	if err != nil {
		t.Fatalf("record runtime logs: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	list := decodeJSONResponse(t, server.URL+"/api/runs", http.StatusOK)
	workflowRuns := list["workflowRuns"].([]any)
	if len(workflowRuns) != 2 || workflowRuns[0].(map[string]any)["id"] != "run.beta" || workflowRuns[1].(map[string]any)["id"] != "run.alpha" {
		t.Fatalf("workflow run list = %#v", list)
	}
	if list["ok"] != true {
		t.Fatalf("workflow run list should expose ok envelope: %#v", list)
	}
	firstRun := workflowRuns[1].(map[string]any)
	if firstRun["summaryJson"] == "" || firstRun["stepCount"] != float64(2) {
		t.Fatalf("workflow run list summary fields = %#v", firstRun)
	}

	detail := decodeJSONResponse(t, server.URL+"/api/workflow-runs/run.alpha", http.StatusOK)
	if detail["ok"] != true {
		t.Fatalf("workflow run detail failed: %#v", detail)
	}
	traceTopologies := detail["traceTopologies"].([]any)
	if len(traceTopologies) != 1 {
		t.Fatalf("workflow run detail should include topology array: %#v", detail)
	}
	if traceTopologies[0].(map[string]any)["traceId"] != "trace.beta" {
		t.Fatalf("workflow run topology row = %#v", traceTopologies[0])
	}
	summary := detail["summary"].(map[string]any)
	if len(summary["steps"].([]any)) != 2 {
		t.Fatalf("workflow run detail summary = %#v", summary)
	}

	step := decodeJSONResponse(t, server.URL+"/api/workflow-runs/step?runId=run.alpha&stepId=step.beta", http.StatusOK)
	stepSummary := step["summary"].(map[string]any)
	steps := stepSummary["steps"].([]any)
	if len(steps) != 1 || steps[0].(map[string]any)["stepId"] != "step.beta" {
		t.Fatalf("workflow run step payload = %#v", step)
	}
	if strings.Contains(mustJSON(t, step), "step.alpha") {
		t.Fatalf("workflow run step leaked other steps: %#v", step)
	}
	stepTopologies := step["traceTopologies"].([]any)
	if len(stepTopologies) != 1 || stepTopologies[0].(map[string]any)["stepId"] != "step.beta" {
		t.Fatalf("workflow run step topology payload = %#v", step)
	}

	latest := decodeJSONResponse(t, server.URL+"/api/workflow-runs/latest-step?workflowId=workflow.alpha&stepId=step.beta", http.StatusOK)
	latestRun := latest["run"].(map[string]any)
	if latestRun["id"] != "run.alpha" {
		t.Fatalf("latest workflow step should prefer full workflow cache over newer single-step runs: %#v", latest)
	}
	latestStep := latest["summary"].(map[string]any)["steps"].([]any)[0].(map[string]any)
	trace := latestStep["trace"].(map[string]any)
	systems := trace["systems"].([]any)
	if len(systems) != 1 || systems[0].(map[string]any)["name"] != "worker" {
		t.Fatalf("latest workflow step should use cached runtime logs: %#v", latest)
	}
}

func TestServerCopiesStepTraceTopologyIntoSavedWorkflowRun(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 18, 7, 0, 0, 0, time.UTC)
	_, err = s.CreateRun(ctx, store.Run{
		ID:          "run.single.apply",
		ProfileID:   "sample",
		WorkflowID:  "workflow.alpha",
		Status:      store.StatusPassed,
		SummaryJSON: `{"kind":"apiCase","caseId":"case.apply","stepId":"apply","ok":true}`,
		CreatedAt:   started,
		UpdatedAt:   started,
	})
	if err != nil {
		t.Fatalf("create source run: %v", err)
	}
	_, err = s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.single.apply",
		WorkflowRunID: "run.single.apply",
		WorkflowID:    "workflow.alpha",
		StepID:        "apply",
		CaseID:        "case.apply",
		RequestID:     "request.apply",
		TraceID:       "trace.apply",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"entry-service","target":"worker-service"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["entry-service","worker-service"]}`,
		TextTopology:  "entry-service -> worker-service",
		CreatedAt:     started,
	})
	if err != nil {
		t.Fatalf("save source topology: %v", err)
	}
	_, err = s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:          "task.single.apply.topology",
		RunID:       "run.single.apply",
		WorkflowID:  "workflow.alpha",
		StepID:      "apply",
		CaseID:      "case.apply",
		Kind:        "trace_topology_collect",
		Status:      store.StatusPassed,
		StartedAt:   started,
		FinishedAt:  started.Add(100 * time.Millisecond),
		DurationMs:  100,
		SummaryJSON: `{"traceId":"trace.apply","topologyStatus":"complete"}`,
		CreatedAt:   started,
	})
	if err != nil {
		t.Fatalf("record source post-process task: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/workflow-runs", "application/json", strings.NewReader(`{
		"workflowId":"workflow.alpha",
		"ok":true,
		"steps":[
			{"stepId":"apply","caseId":"case.apply","runId":"run.single.apply","caseRunId":"run.single.apply.case","ok":true,"status":"passed"}
		]
	}`))
	if err != nil {
		t.Fatalf("post workflow run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("workflow run status = %d body=%s", resp.StatusCode, raw)
	}

	latest := decodeJSONResponse(t, server.URL+"/api/workflow-runs/latest-step?workflowId=workflow.alpha&stepId=apply", http.StatusOK)
	topologies := latest["traceTopologies"].([]any)
	if len(topologies) != 1 {
		t.Fatalf("latest saved workflow step should include copied topology: %#v", latest)
	}
	topology := topologies[0].(map[string]any)
	if topology["runId"] == "run.single.apply" || topology["stepId"] != "apply" || topology["traceId"] != "trace.apply" {
		t.Fatalf("copied topology should belong to saved workflow run and keep step evidence: %#v", topology)
	}
	runID := topology["workflowRunId"].(string)
	tasks := decodeJSONResponse(t, server.URL+"/api/post-process-tasks?runId="+url.QueryEscape(runID)+"&stepId=apply&kind=trace_topology_collect", http.StatusOK)
	taskRows := tasks["tasks"].([]any)
	counts := tasks["counts"].(map[string]any)
	if len(taskRows) != 1 || counts["passed"] != float64(1) || taskRows[0].(map[string]any)["runId"] != runID {
		t.Fatalf("latest saved workflow run should include copied topology task: %#v", tasks)
	}
}

func TestServerBackfillsSavedWorkflowStepTopologyAfterAsyncCollection(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 18, 7, 0, 0, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:          "run.single.apply",
		ProfileID:   "sample",
		WorkflowID:  "workflow.alpha",
		Status:      store.StatusPassed,
		SummaryJSON: `{"kind":"apiCase","caseId":"case.apply","stepId":"apply","ok":true}`,
		CreatedAt:   started,
		UpdatedAt:   started,
	}); err != nil {
		t.Fatalf("create source run: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/workflow-runs", "application/json", strings.NewReader(`{
		"workflowId":"workflow.alpha",
		"ok":true,
		"steps":[
			{"stepId":"apply","caseId":"case.apply","runId":"run.single.apply","caseRunId":"run.single.apply.case","ok":true,"status":"passed"}
		]
	}`))
	if err != nil {
		t.Fatalf("post workflow run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("workflow run status = %d body=%s", resp.StatusCode, raw)
	}

	_, err = s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.single.apply",
		WorkflowRunID: "run.single.apply",
		WorkflowID:    "workflow.alpha",
		StepID:        "apply",
		CaseID:        "case.apply",
		RequestID:     "request.apply",
		TraceID:       "trace.apply",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"entry-service","target":"worker-service"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["entry-service","worker-service"]}`,
		TextTopology:  "entry-service -> worker-service",
		CreatedAt:     started.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("save late source topology: %v", err)
	}

	latest := decodeJSONResponse(t, server.URL+"/api/workflow-runs/latest-step?workflowId=workflow.alpha&stepId=apply", http.StatusOK)
	topologies := latest["traceTopologies"].([]any)
	if len(topologies) != 1 || topologies[0].(map[string]any)["traceId"] != "trace.apply" {
		t.Fatalf("latest saved workflow step should backfill late topology: %#v", latest)
	}
}

func TestServerEvaluatesWorkflowStepTimeoutFromCatalog(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: started,
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha", BaseStepTimeoutMs: 500, TimeoutOffsetMs: 0},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Method: "POST", Path: "/alpha", Status: "active", TimeoutMs: 100},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true, SortOrder: 1},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		SummaryJSON: `{
			"status":"passed",
			"summary":{"expectedStepCount":1,"stepCount":1,"passed":1,"elapsedMs":150},
			"steps":[{"stepId":"step.alpha","caseId":"case.alpha","ok":true,"stepOk":true,"status":"passed","elapsedMs":150}]
		}`,
		CreatedAt: started,
		UpdatedAt: started.Add(150 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	detail := decodeJSONResponse(t, server.URL+"/api/workflow-runs/run.alpha", http.StatusOK)
	summary := detail["summary"].(map[string]any)
	steps := summary["steps"].([]any)
	step := steps[0].(map[string]any)
	if step["status"] != store.StatusFailed || step["stepOk"] != false || step["timeoutExceeded"] != true || step["timeoutMs"] != float64(100) {
		t.Fatalf("workflow run detail step timeout = %#v", step)
	}
	if summary["status"] != store.StatusFailed || summary["ok"] != false {
		t.Fatalf("workflow run summary timeout status = %#v", summary)
	}

	stepPayload := decodeJSONResponse(t, server.URL+"/api/workflow-runs/step?runId=run.alpha&stepId=step.alpha", http.StatusOK)
	stepSummary := stepPayload["summary"].(map[string]any)
	scopedStep := stepSummary["steps"].([]any)[0].(map[string]any)
	if scopedStep["status"] != store.StatusFailed || scopedStep["timeoutExceeded"] != true || !strings.Contains(scopedStep["failureReason"].(string), "exceeded timeout") {
		t.Fatalf("workflow run step timeout = %#v", scopedStep)
	}
}

func TestServerSavesWorkflowRunToStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/workflow-runs", "application/json", strings.NewReader(`{
		"workflowId":"workflow.alpha",
		"status":"passed",
		"ok":true,
		"steps":[{"stepId":"step.alpha","caseId":"case.alpha","ok":true,"summary":{"requestId":"request.alpha","httpCode":200}}],
		"summary":{"expectedStepCount":1,"stepCount":1}
	}`))
	if err != nil {
		t.Fatalf("post workflow run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("save workflow run status = %d body=%s", resp.StatusCode, raw)
	}
	var saved struct {
		OK            bool   `json:"ok"`
		WorkflowRunID string `json:"workflowRunId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		t.Fatalf("decode saved workflow run: %v", err)
	}
	if !saved.OK || saved.WorkflowRunID == "" {
		t.Fatalf("saved workflow run = %#v", saved)
	}

	loaded := decodeJSONResponse(t, server.URL+"/api/workflow-runs/"+saved.WorkflowRunID, http.StatusOK)
	run := loaded["run"].(map[string]any)
	if run["workflowId"] != "workflow.alpha" || run["status"] != "passed" {
		t.Fatalf("loaded saved workflow run = %#v", loaded)
	}
	if run["evidenceRoot"] != "" {
		t.Fatalf("empty evidence root should stay empty: %#v", run)
	}

	caseRuns, err := s.ListAPICaseRuns(ctx, saved.WorkflowRunID)
	if err != nil {
		t.Fatalf("list case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != "case.alpha" || caseRuns[0].Status != store.StatusPassed {
		t.Fatalf("saved workflow case runs = %#v", caseRuns)
	}
	evidence := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId="+saved.WorkflowRunID, http.StatusOK)
	evidenceBody := evidence["evidence"].(map[string]any)
	summary := evidenceBody["summary"].(map[string]any)
	if summary["case_id"] != "case.alpha" || summary["status"] != store.StatusPassed {
		t.Fatalf("saved workflow evidence = %#v", evidence)
	}
}

func TestServerExposesWorkflowAuditWithoutStore(t *testing.T) {
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
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

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
	bundle := profile.Bundle{
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
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

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
	bundle := profile.Bundle{
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
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

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
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
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
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "bundle-profile"}, s))
	defer server.Close()

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
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

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
		_, err = s.CreateRun(ctx, store.Run{
			ID:          item.id,
			ProfileID:   "sample",
			WorkflowID:  "workflow.alpha",
			Status:      item.status,
			SummaryJSON: "{}",
			CreatedAt:   item.createdAt,
			UpdatedAt:   item.createdAt,
		})
		if err != nil {
			t.Fatalf("create run %s: %v", item.id, err)
		}
		for _, caseRun := range item.caseRuns {
			caseRun.RunID = item.id
			_, err = s.RecordAPICaseRun(ctx, caseRun)
			if err != nil {
				t.Fatalf("record api case run %s: %v", caseRun.ID, err)
			}
		}
	}

	bundle := profile.Bundle{
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
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

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
	server := httptest.NewServer(controlplane.New(profile.Bundle{ID: "sample"}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit", http.StatusBadRequest)
	if payload["ok"] != false || !strings.Contains(payload["error"].(string), "workflowId") {
		t.Fatalf("workflow audit missing id payload = %#v", payload)
	}
}

func TestServerReturnsNotFoundForUnknownWorkflowAudit(t *testing.T) {
	server := httptest.NewServer(controlplane.New(profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
	}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.missing", http.StatusNotFound)
	if payload["ok"] != false || !strings.Contains(payload["error"].(string), "workflow not found") {
		t.Fatalf("workflow audit missing workflow payload = %#v", payload)
	}
}

func TestServerReturnsInternalErrorForWorkflowAuditStoreFailure(t *testing.T) {
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
	}, failingListRunsStore{}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.alpha", http.StatusInternalServerError)
	if payload["ok"] != false || !strings.Contains(payload["error"].(string), "list runs failed") {
		t.Fatalf("workflow audit store failure payload = %#v", payload)
	}
}

func TestServerExposesTestKitRunContracts(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run", "application/json", strings.NewReader(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
			"stepId":"step.alpha"
		}`))
	if err != nil {
		t.Fatalf("post test kit run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("test kit run status = %d body=%s", resp.StatusCode, raw)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode test kit run: %v", err)
	}
	if result["ok"] != false || result["caseId"] != "case.alpha" || result["stepId"] != "step.alpha" {
		t.Fatalf("test kit run result = %#v", result)
	}

	runs := decodeJSONResponse(t, server.URL+"/api/runs", http.StatusOK)
	workflowRuns := runs["workflowRuns"].([]any)
	if len(workflowRuns) != 1 || workflowRuns[0].(map[string]any)["workflowId"] != "workflow.alpha" {
		t.Fatalf("test kit run should be indexed in store: %#v", runs)
	}
}

func TestServerExecutesTestKitRunFromRuntimeConfig(t *testing.T) {
	ctx := context.Background()
	var received struct {
		Method string
		Path   string
		Header string
		Body   map[string]any
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Method = r.Method
		received.Path = r.URL.String()
		received.Header = r.Header.Get("X-Case")
		if err := json.NewDecoder(r.Body).Decode(&received.Body); err != nil {
			t.Fatalf("decode target body: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"request_id":"req-001"}`))
	}))
	defer target.Close()

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "app"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.alpha",
				TemplateID: "template.case.alpha",
				NodeID:     "node.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Title:      "Case Alpha Runtime",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"POST",
						"nodeId":"service.alpha",
						"path":"/callback",
						"query":{"mode":"{{override:mode|default}}"},
						"headers":{"X-Case":"{{override:header|defaultValue}}"},
						"body":{"id":"{{override:id|default-id}}","serial":"{{serial:TST}}"},
						"expectedHttpCodes":[200],
						"requireRequestId":true,
						"traceCorrelatorFields":["request_id"]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run", "application/json", strings.NewReader(fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
			"stepId":"step.alpha",
			"baseUrl":%q,
		"overrides":{"id":"runtime-id","mode":"live","header":"selected"},
		"timeoutSeconds":5
	}`, target.URL)))
	if err != nil {
		t.Fatalf("post test kit run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("test kit run status = %d body=%s", resp.StatusCode, raw)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode test kit result: %v", err)
	}
	if result["ok"] != true || result["status"] != store.StatusPassed {
		t.Fatalf("test kit run result = %#v", result)
	}
	if received.Method != http.MethodPost || received.Path != "/callback?mode=live" || received.Header != "selected" || received.Body["id"] != "runtime-id" {
		t.Fatalf("target received = %#v", received)
	}

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].WorkflowID != "workflow.alpha" {
		t.Fatalf("runs = %#v", runs)
	}
	caseRuns, err := s.ListAPICaseRuns(ctx, runs[0].ID)
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].Status != store.StatusPassed {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	if !caseRuns[0].FinishedAt.After(caseRuns[0].StartedAt) || caseRuns[0].FinishedAt.Sub(caseRuns[0].StartedAt) < 10*time.Millisecond {
		t.Fatalf("case run timing = %#v", caseRuns[0])
	}
	var requestSummary map[string]any
	if err := json.Unmarshal([]byte(caseRuns[0].RequestSummaryJSON), &requestSummary); err != nil {
		t.Fatalf("decode request summary: %v", err)
	}
	if requestSummary["method"] != http.MethodPost || requestSummary["fullUrl"] == "" || requestSummary["stepId"] != "step.alpha" {
		t.Fatalf("request summary = %#v", requestSummary)
	}
	records, err := s.ListEvidence(ctx, runs[0].ID)
	if err != nil {
		t.Fatalf("list test-kit evidence: %v", err)
	}
	if len(records) < 3 {
		t.Fatalf("test-kit run should persist request/response/assertion Evidence records, got %#v", records)
	}
	evidence := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId="+runs[0].ID+"&caseId=case.alpha&stepId=step.alpha", http.StatusOK)
	body := evidence["evidence"].(map[string]any)
	requestEvidence := body["request"].(map[string]any)
	responseEvidence := body["response"].(map[string]any)
	if requestEvidence["evidence_uri"] == "" || responseEvidence["evidence_uri"] == "" || responseEvidence["http_code"] != float64(200) {
		t.Fatalf("test-kit Evidence payload = %#v", evidence)
	}
}

func TestServerTestKitRunHonorsExpectedResponseContains(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result_status":"F"}`))
	}))
	defer target.Close()

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.alpha",
				TemplateID: "template.case.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"GET",
						"nodeId":"node.alpha",
						"path":"/result",
						"expectedHttpCodes":[200],
						"expectedResponseContains":["\"result_status\":\"S\""]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run", "application/json", strings.NewReader(fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
		"stepId":"step.alpha",
		"baseUrl":%q
	}`, target.URL)))
	if err != nil {
		t.Fatalf("post test kit run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("test kit run status = %d body=%s", resp.StatusCode, raw)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode test kit result: %v", err)
	}
	if result["ok"] != false || !strings.Contains(fmt.Sprint(result["error"]), "response body missing") {
		t.Fatalf("test kit result = %#v", result)
	}
}

func TestServerExecutesTestKitRunFromStoreRegisteredServicePort(t *testing.T) {
	ctx := context.Background()
	var receivedPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"request_id":"req-store-port"}`))
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}
	targetPort, err := strconv.Atoi(targetURL.Port())
	if err != nil {
		t.Fatalf("parse target port: %v", err)
	}

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{ID: "service.gateway", DisplayName: "Gateway", Kind: "http", ServicePort: targetPort, Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.gateway", DisplayName: "Gateway Case", NodeID: "node.gateway", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.gateway",
				TemplateID: "template.case.gateway",
				NodeID:     "node.gateway",
				WorkflowID: "workflow.gateway",
				ScopeType:  "step",
				ScopeID:    "step.gateway",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.gateway",
					"caseExecution":{
						"method":"GET",
						"nodeId":"service.gateway",
						"path":"/ready",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "current"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run", "application/json", strings.NewReader(`{
		"caseId":"case.gateway",
		"workflowId":"workflow.gateway",
		"stepId":"step.gateway",
		"timeoutSeconds":5
	}`))
	if err != nil {
		t.Fatalf("post test kit run: %v", err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode test kit result: %v", err)
	}
	if resp.StatusCode != http.StatusOK || result["ok"] != true {
		t.Fatalf("test kit run status=%d result=%#v", resp.StatusCode, result)
	}
	if receivedPath != "/ready" {
		t.Fatalf("target received path = %q", receivedPath)
	}
}

func TestServerPrefersStoreRegisteredServicePortOverBundleServicePort(t *testing.T) {
	ctx := context.Background()
	var receivedPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"request_id":"req-store-first"}`))
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}
	targetPort, err := strconv.Atoi(targetURL.Port())
	if err != nil {
		t.Fatalf("parse target port: %v", err)
	}
	staleBundleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "stale bundle service", http.StatusTeapot)
	}))
	defer staleBundleServer.Close()
	staleURL, err := url.Parse(staleBundleServer.URL)
	if err != nil {
		t.Fatalf("parse stale url: %v", err)
	}
	stalePort, err := strconv.Atoi(staleURL.Port())
	if err != nil {
		t.Fatalf("parse stale port: %v", err)
	}

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{ID: "service.gateway", DisplayName: "Gateway", Kind: "http", ServicePort: targetPort, Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.gateway", DisplayName: "Gateway Case", NodeID: "node.gateway", Status: "active"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.gateway", ServiceID: "service.gateway", Method: "GET", Path: "/ready", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.gateway",
				TemplateID: "template.case.gateway",
				NodeID:     "node.gateway",
				WorkflowID: "workflow.gateway",
				ScopeType:  "step",
				ScopeID:    "step.gateway",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.gateway",
					"caseExecution":{
						"method":"GET",
						"nodeId":"service.gateway",
						"path":"/ready",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	bundle := profile.Bundle{
		ID: "current",
		Services: []profile.Service{
			{ID: "service.gateway", DisplayName: "Stale Gateway", Kind: "http", ServicePort: stalePort, Status: "active"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run", "application/json", strings.NewReader(`{
		"caseId":"case.gateway",
		"workflowId":"workflow.gateway",
		"stepId":"step.gateway",
		"timeoutSeconds":5
	}`))
	if err != nil {
		t.Fatalf("post test kit run: %v", err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode test kit result: %v", err)
	}
	if resp.StatusCode != http.StatusOK || result["ok"] != true {
		t.Fatalf("test kit run status=%d result=%#v", resp.StatusCode, result)
	}
	if receivedPath != "/ready" {
		t.Fatalf("Store service port should win over stale bundle service, target received path = %q", receivedPath)
	}
}

func TestServerCatalogUsesStoreCatalogOverStaleReadModel(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	stale := store.ProfileCatalog{
		ProfileID: "current",
		Services:  []store.CatalogService{{ID: "service.gateway", Kind: "http"}},
		Workflows: []store.CatalogWorkflow{{ID: "workflow.core", DisplayName: "Core"}},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.query", ServiceID: "service.gateway", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.old", NodeID: "node.query", Status: "active"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.core", StepID: "query", NodeID: "node.query", CaseID: "case.old", Required: true, SortOrder: 1},
		},
	}
	readModel, err := controlplane.CatalogReadModel(stale, "config.stale", time.Now().UTC())
	if err != nil {
		t.Fatalf("build stale catalog read model: %v", err)
	}
	if _, err := s.UpsertReadModel(ctx, readModel); err != nil {
		t.Fatalf("upsert stale catalog read model: %v", err)
	}

	current := stale
	current.APICases = []store.CatalogAPICase{{ID: "case.current", NodeID: "node.query", Status: "active"}}
	current.WorkflowBindings = []store.CatalogWorkflowBinding{
		{WorkflowID: "workflow.core", StepID: "query", NodeID: "node.query", CaseID: "case.current", Required: true, SortOrder: 1},
	}
	current.TemplateConfigs = []store.CatalogTemplateConfig{
		{
			ID:         "cfg.workflow.core.query",
			WorkflowID: "workflow.core",
			ScopeType:  "step",
			ScopeID:    "query",
			Status:     "active",
			ConfigJSON: `{"caseId":"case.old","caseExecution":{"method":"GET","nodeId":"service.gateway","path":"/query"}}`,
		},
	}
	if err := s.ReplaceProfileCatalog(ctx, current); err != nil {
		t.Fatalf("replace current catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "current"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	source := payload["source"].(map[string]any)
	if source["kind"] != "store" {
		t.Fatalf("catalog should use store source over stale read model: %#v", source)
	}
	workflows := payload["workflows"].([]any)
	steps := workflows[0].(map[string]any)["steps"].([]any)
	if steps[0].(map[string]any)["caseId"] != "case.current" {
		t.Fatalf("catalog step should use current store binding: %#v", steps[0])
	}
}

func TestServerCollectsTraceTopologyForSingleTestKitRun(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "request.alpha")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["GET:/callback"],"duration":80,"start":"2026-05-15 0830","isError":false,"traceIds":["trace.alpha"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			if payload.Variables["traceId"] != "trace.alpha" {
				t.Fatalf("trace id variable = %#v", payload.Variables)
			}
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.alpha","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/callback","type":"Entry","component":"Tomcat"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.alpha",
				TemplateID: "template.case.alpha",
				NodeID:     "node.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Title:      "Case Alpha Runtime",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"GET",
						"nodeId":"service.alpha",
						"path":"/callback",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{
		Runtime:         s,
		TraceGraphQLURL: provider.URL,
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run", "application/json", strings.NewReader(fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
			"stepId":"step.alpha",
			"baseUrl":%q,
		"timeoutSeconds":5
	}`, target.URL)))
	if err != nil {
		t.Fatalf("post test kit run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("test kit run status = %d body=%s", resp.StatusCode, raw)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode test kit result: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("test kit run result = %#v", result)
	}

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %#v", runs)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		topologies, err := s.ListTraceTopologies(ctx, runs[0].ID)
		if err != nil {
			t.Fatalf("list trace topologies: %v", err)
		}
		if len(topologies) == 1 && topologies[0].CaseID == "case.alpha" && topologies[0].StepID == "step.alpha" && topologies[0].RequestID == "request.alpha" {
			tasks, err := s.ListPostProcessTasks(ctx, runs[0].ID)
			if err != nil {
				t.Fatalf("list post process tasks: %v", err)
			}
			if len(tasks) != 1 || tasks[0].Kind != "trace_topology_collect" || tasks[0].Status != store.StatusPassed || tasks[0].DurationMs < 0 {
				t.Fatalf("trace post process tasks = %#v", tasks)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("stored trace topology was not collected asynchronously")
}

func TestServerReturnsTraceTopologyForWorkflowStepTestKitRun(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "request.alpha")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["GET:/callback"],"duration":80,"start":"2026-05-15 0830","isError":false,"traceIds":["trace.alpha"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.alpha","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/callback","type":"Entry","component":"Tomcat"},{"traceId":"trace.alpha","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.alpha","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"GET:/callback","type":"Entry","component":"Server"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.alpha",
				TemplateID: "template.case.alpha",
				NodeID:     "node.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Title:      "Case Alpha Runtime",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"GET",
						"nodeId":"service.alpha",
						"path":"/callback",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{
		Runtime:         s,
		TraceGraphQLURL: provider.URL,
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run", "application/json", strings.NewReader(fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
		"stepId":"step.alpha",
		"baseUrl":%q,
		"timeoutSeconds":5
	}`, target.URL)))
	if err != nil {
		t.Fatalf("post test kit run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("test kit run status = %d body=%s", resp.StatusCode, raw)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode test kit result: %v", err)
	}
	topology := result["traceTopology"].(map[string]any)
	if topology["provider"] != "skywalking" || topology["status"] != "complete" || topology["traceId"] != "trace.alpha" {
		t.Fatalf("trace topology should be returned inline: %#v", topology)
	}
	if edges := topology["confirmedEdges"].([]any); len(edges) != 1 {
		t.Fatalf("trace topology edges = %#v", edges)
	}
}

func TestServerRecordsSkippedTraceTopologyTaskWhenTraceProviderMissing(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "store.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Request-Id", "request.alpha")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.alpha",
				TemplateID: "template.case.alpha",
				NodeID:     "node.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Title:      "Case Alpha Runtime",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"GET",
						"nodeId":"service.alpha",
						"path":"/callback",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{Runtime: s}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run", "application/json", strings.NewReader(fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
		"stepId":"step.alpha",
		"baseUrl":%q,
		"timeoutSeconds":5
	}`, target.URL)))
	if err != nil {
		t.Fatalf("post test kit run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("test kit run status = %d body=%s", resp.StatusCode, raw)
	}

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %#v", runs)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := s.ListPostProcessTasks(ctx, runs[0].ID)
		if err != nil {
			t.Fatalf("list post process tasks: %v", err)
		}
		if len(tasks) == 1 {
			if tasks[0].Kind != "trace_topology_collect" || tasks[0].Status != store.StatusSkipped || tasks[0].StepID != "step.alpha" {
				t.Fatalf("trace task should record skipped collection: %#v", tasks)
			}
			if !strings.Contains(tasks[0].Error, "TraceGraphQLURL") {
				t.Fatalf("trace skipped task should explain missing provider config: %#v", tasks[0])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("missing trace topology skipped task")
}

func TestServerExposesPostProcessTasks(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "store.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	base := time.Date(2026, 5, 17, 2, 3, 4, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.tasks",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  base,
		FinishedAt: base.Add(time.Second),
		CreatedAt:  base,
		UpdatedAt:  base.Add(time.Second),
	}); err != nil {
		t.Fatalf("create task run: %v", err)
	}
	if _, err := s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:         "task.trace",
		RunID:      "run.tasks",
		WorkflowID: "workflow.alpha",
		StepID:     "step-a",
		CaseID:     "case.alpha",
		Kind:       "trace_topology_collect",
		Status:     store.StatusPassed,
		StartedAt:  base.Add(10 * time.Millisecond),
		FinishedAt: base.Add(135 * time.Millisecond),
		CreatedAt:  base.Add(10 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record task: %v", err)
	}
	if _, err := s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:          "task.logs",
		RunID:       "run.tasks",
		WorkflowID:  "workflow.alpha",
		StepID:      "step-b",
		CaseID:      "case.beta",
		Kind:        "runtime_log_collect",
		Status:      store.StatusFailed,
		StartedAt:   base.Add(200 * time.Millisecond),
		FinishedAt:  base.Add(500 * time.Millisecond),
		Error:       "log source missing",
		SummaryJSON: `{"source":"runtime-log"}`,
		CreatedAt:   base.Add(200 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record failed task: %v", err)
	}
	if _, err := s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:          "task.trace.skip",
		RunID:       "run.tasks",
		WorkflowID:  "workflow.alpha",
		StepID:      "step-c",
		CaseID:      "case.gamma",
		Kind:        "trace_topology_collect",
		Status:      store.StatusSkipped,
		StartedAt:   base.Add(600 * time.Millisecond),
		FinishedAt:  base.Add(600 * time.Millisecond),
		SummaryJSON: `{"reason":"SkyWalking provider unavailable"}`,
		CreatedAt:   base.Add(600 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record skipped task: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{Runtime: s}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/post-process-tasks?runId=run.tasks&stepId=step-a&kind=trace_topology_collect", http.StatusOK)
	if payload["ok"] != true || payload["runId"] != "run.tasks" {
		t.Fatalf("post process task payload = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"].(float64) != 1 || counts["passed"].(float64) != 1 || counts["durationMs"].(float64) != 125 {
		t.Fatalf("post process task counts = %#v", counts)
	}
	tasks := payload["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("post process tasks = %#v", tasks)
	}
	task := tasks[0].(map[string]any)
	if task["id"] != "task.trace" || task["kind"] != "trace_topology_collect" || task["stepId"] != "step-a" {
		t.Fatalf("post process task = %#v", task)
	}
	if task["outcome"] != "success" || task["reason"] != "completed" || task["displayStatus"] != "passed: completed" {
		t.Fatalf("post process task readable status = %#v", task)
	}

	all := decodeJSONResponse(t, server.URL+"/api/post-process-tasks?runId=run.tasks", http.StatusOK)
	allTasks := all["tasks"].([]any)
	if len(allTasks) != 3 {
		t.Fatalf("all post process tasks = %#v", allTasks)
	}
	byID := map[string]map[string]any{}
	for _, raw := range allTasks {
		task := raw.(map[string]any)
		byID[task["id"].(string)] = task
	}
	if byID["task.logs"]["outcome"] != "failed" || byID["task.logs"]["reason"] != "log source missing" || byID["task.logs"]["displayStatus"] != "failed: log source missing" {
		t.Fatalf("failed task readable status = %#v", byID["task.logs"])
	}
	if byID["task.trace.skip"]["outcome"] != "skipped" || byID["task.trace.skip"]["reason"] != "SkyWalking provider unavailable" || byID["task.trace.skip"]["displayStatus"] != "skipped: SkyWalking provider unavailable" {
		t.Fatalf("skipped task readable status = %#v", byID["task.trace.skip"])
	}

	missing := decodeJSONResponse(t, server.URL+"/api/post-process-tasks", http.StatusBadRequest)
	if missing["ok"] != false || !strings.Contains(missing["error"].(string), "runId") {
		t.Fatalf("missing runId response = %#v", missing)
	}
}

func TestServerExposesTestKitBatchContract(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
			{ID: "case.beta", DisplayName: "Case Beta", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run-batch", "application/json", strings.NewReader(`{
			"caseIds":["case.alpha","case.beta"]
		}`))
	if err != nil {
		t.Fatalf("post test kit batch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("test kit batch status = %d body=%s", resp.StatusCode, raw)
	}
	var payload struct {
		OK      bool             `json:"ok"`
		Results []map[string]any `json:"results"`
		Summary struct {
			CaseCount int `json:"caseCount"`
			Passed    int `json:"passed"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode test kit batch: %v", err)
	}
	if payload.OK || len(payload.Results) != 2 || payload.Summary.CaseCount != 2 || payload.Summary.Passed != 0 {
		t.Fatalf("test kit batch payload = %#v", payload)
	}
}

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

func TestServerExposesCaseRunsFromStore(t *testing.T) {
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
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "run.alpha.response",
		RunID:     "run.alpha",
		CaseRunID: "run.alpha.case",
		Kind:      "response",
		URI:       ".runtime/evidence/run.alpha/response.json",
		MediaType: "application/json",
		Summary:   `{"statusCode":200}`,
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/runs", http.StatusOK)
	caseRuns := payload["caseRuns"].([]any)
	if len(caseRuns) != 1 {
		t.Fatalf("case runs = %#v", payload)
	}
	item := caseRuns[0].(map[string]any)
	if item["runId"] != "run.alpha" || item["caseId"] != "case.alpha" || item["status"] != "passed" {
		t.Fatalf("case run item = %#v", item)
	}
	if item["operation"] != "POST /alpha" || item["evidencePath"] != ".runtime/evidence/run.alpha" || item["evidenceCount"] != float64(1) {
		t.Fatalf("case run details = %#v", item)
	}
}

func TestServerExposesCaseRunFailureCategoriesFromProfile(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.failed",
		ProfileID:    "sample",
		Status:       store.StatusFailed,
		EvidenceRoot: ".runtime/evidence/run.failed",
		SummaryJSON:  "{}",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.failed.case",
		RunID:                "run.failed",
		CaseID:               "case.failed",
		Status:               store.StatusFailed,
		RequestSummaryJSON:   `{"method":"GET","path":"/failed"}`,
		AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		FailureCategories: []profile.FailureCategoryRule{
			{
				Name: "Product errors",
				Matchers: profile.FailureCategoryMatchers{
					Statuses:          []string{store.StatusFailed},
					FailureCategories: []string{"assertion-mismatch"},
					MessageContains:   []string{"assertion errors"},
				},
			},
			{
				Name: "Later matching rule",
				Matchers: profile.FailureCategoryMatchers{
					Statuses: []string{store.StatusFailed},
				},
			},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/runs", http.StatusOK)
	caseRuns := payload["caseRuns"].([]any)
	if len(caseRuns) != 1 {
		t.Fatalf("case runs = %#v", payload)
	}
	item := caseRuns[0].(map[string]any)
	if item["failureCategory"] != "Product errors" || item["defaultFailureCategory"] != "assertion-mismatch" {
		t.Fatalf("case run failure category = %#v", item)
	}
	categories := payload["failureCategories"].([]any)
	if len(categories) != 2 || categories[0].(map[string]any)["name"] != "Product errors" {
		t.Fatalf("failure category rules = %#v", payload["failureCategories"])
	}
}

func TestServerExposesCaseEvidenceFromStore(t *testing.T) {
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
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha","hasBody":true}`,
		AssertionSummaryJSON: `{"status":"passed","errorCount":0}`,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	evidenceDir := t.TempDir()
	requestPath := filepath.Join(evidenceDir, "request.json")
	if err := os.WriteFile(requestPath, []byte(`{"method":"POST","path":"/alpha?token=query-secret","headers":{"Content-Type":"application/json","Authorization":"Bearer request-secret"},"body":{"id":"item-001","token":"request-token"}}`), 0o644); err != nil {
		t.Fatalf("write request evidence: %v", err)
	}
	responsePath := filepath.Join(evidenceDir, "response.json")
	if err := os.WriteFile(responsePath, []byte(`{"statusCode":200,"headers":{"Content-Type":"application/json","Set-Cookie":"session=response-cookie"},"body":"{\"ok\":true,\"password\":\"response-secret\"}"}`), 0o644); err != nil {
		t.Fatalf("write response evidence: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         "run.alpha.request",
		RunID:      "run.alpha",
		CaseRunID:  "run.alpha.case",
		StepID:     "step.alpha",
		Kind:       "request",
		URI:        requestPath,
		MediaType:  "application/json",
		SHA256:     "sha256-request",
		SizeBytes:  128,
		Summary:    `{"method":"POST","path":"/alpha","hasBody":true}`,
		Category:   "http-exchange",
		Visibility: "public",
		LabelsJSON: `{"kind":"request","owner":"qa"}`,
	})
	if err != nil {
		t.Fatalf("record request evidence: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "run.alpha.response",
		RunID:     "run.alpha",
		CaseRunID: "run.alpha.case",
		Kind:      "response",
		URI:       responsePath,
		MediaType: "application/json",
		Summary:   `{"statusCode":200,"bodyBytes":19}`,
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId=run.alpha&caseId=case.alpha", http.StatusOK)
	evidence := payload["evidence"].(map[string]any)
	summary := evidence["summary"].(map[string]any)
	request := evidence["request"].(map[string]any)
	response := evidence["response"].(map[string]any)
	assertions := evidence["assertions"].(map[string]any)
	if summary["case_id"] != "case.alpha" || request["method"] != "POST" || request["path"] != "/alpha?token=%5BREDACTED%5D" {
		t.Fatalf("case evidence request = %#v", payload)
	}
	requestBody := request["body"].(map[string]any)
	if requestBody["id"] != "item-001" {
		t.Fatalf("case evidence request body = %#v", request)
	}
	redactedRequest, _ := json.Marshal(request)
	for _, leaked := range []string{"query-secret", "request-secret", "request-token"} {
		if strings.Contains(string(redactedRequest), leaked) {
			t.Fatalf("case evidence request leaked %q: %s", leaked, redactedRequest)
		}
	}
	if !strings.Contains(string(redactedRequest), "[REDACTED]") {
		t.Fatalf("case evidence request was not redacted: %s", redactedRequest)
	}
	if response["http_code"] != float64(200) || assertions["status"] != "passed" {
		t.Fatalf("case evidence response/assertions = %#v", payload)
	}
	redactedResponse, _ := json.Marshal(response)
	for _, leaked := range []string{"response-secret", "response-cookie"} {
		if strings.Contains(string(redactedResponse), leaked) {
			t.Fatalf("case evidence response leaked %q: %s", leaked, redactedResponse)
		}
	}
	if !strings.Contains(fmt.Sprint(response["body"]), "[REDACTED]") {
		t.Fatalf("case evidence response body = %#v", response)
	}
	attachment := request["attachment"].(map[string]any)
	labels := attachment["labels"].(map[string]any)
	if attachment["category"] != "http-exchange" || attachment["visibility"] != "public" || labels["owner"] != "qa" {
		t.Fatalf("case evidence attachment metadata = %#v", request)
	}
	if attachment["id"] != "run.alpha.request" || attachment["runId"] != "run.alpha" || attachment["caseRunId"] != "run.alpha.case" || attachment["stepId"] != "step.alpha" || attachment["kind"] != "request" {
		t.Fatalf("case evidence attachment identity = %#v", attachment)
	}
	if attachment["uri"] != requestPath || attachment["mediaType"] != "application/json" || attachment["sha256"] != "sha256-request" || attachment["sizeBytes"] != float64(128) {
		t.Fatalf("case evidence attachment file metadata = %#v", attachment)
	}
}

func TestServerExposesFailedCaseRunEvidenceByCaseRunID(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC)
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusFailed,
		EvidenceRoot: filepath.Join(t.TempDir(), "evidence"),
		SummaryJSON:  `{"steps":[{"stepId":"step.alpha","caseId":"case.alpha"}]}`,
		CreatedAt:    started,
		UpdatedAt:    started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "case-run.alpha",
		RunID:                "run.alpha",
		CaseID:               "case.alpha",
		Status:               store.StatusFailed,
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha","stepId":"step.alpha"}`,
		AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
		StartedAt:            started,
		FinishedAt:           started.Add(300 * time.Millisecond),
		CreatedAt:            started,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	evidenceDir := t.TempDir()
	logPath := filepath.Join(evidenceDir, "runtime-logs.json")
	if err := os.WriteFile(logPath, []byte(`{"systems":[{"id":"service.alpha","name":"Service Alpha","found":true,"coreLogs":["request.alpha failed in worker"]}]}`), 0o644); err != nil {
		t.Fatalf("write runtime logs: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "run.alpha.logs",
		RunID:     "run.alpha",
		CaseRunID: "case-run.alpha",
		Kind:      "runtime_logs",
		URI:       logPath,
		MediaType: "application/json",
		Summary:   `{"caseId":"case.alpha","stepId":"step.alpha","systems":1}`,
		CreatedAt: started,
	})
	if err != nil {
		t.Fatalf("record runtime logs: %v", err)
	}
	_, err = s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.alpha",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.alpha",
		CaseID:        "case.alpha",
		RequestID:     "request.alpha",
		TraceID:       "trace.alpha",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
	})
	if err != nil {
		t.Fatalf("save topology: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case-run/evidence?caseRunId=case-run.alpha", http.StatusOK)
	evidence := payload["evidence"].(map[string]any)
	summary := evidence["summary"].(map[string]any)
	if summary["case_run_id"] != "case-run.alpha" || summary["run_id"] != "run.alpha" || summary["status"] != store.StatusFailed {
		t.Fatalf("failed case evidence summary = %#v", summary)
	}
	topology := evidence["topology"].(map[string]any)
	if topology["traceId"] != "trace.alpha" || len(topology["confirmedEdges"].([]any)) != 1 {
		t.Fatalf("failed case evidence topology = %#v", topology)
	}
	logs := evidence["logs"].([]any)
	if len(logs) != 1 {
		t.Fatalf("failed case evidence logs = %#v", evidence)
	}
	log := logs[0].(map[string]any)
	systems := log["systems"].([]any)
	if log["kind"] != "runtime_logs" || len(systems) != 1 || systems[0].(map[string]any)["found"] != true {
		t.Fatalf("failed case evidence log details = %#v", logs)
	}
}

func TestServerExposesCaseEvidenceDependenciesWithoutInventingTopology(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.alpha", DisplayName: "Alpha workflow"},
		},
		Services: []store.CatalogService{
			{ID: "service.one", DisplayName: "One", Kind: "app"},
			{ID: "service.two", DisplayName: "Two", Kind: "app"},
			{ID: "service.three", DisplayName: "Three", Kind: "app"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.one", DisplayName: "Node One", ServiceID: "service.one", Status: "active", SortOrder: 1},
			{ID: "node.two", DisplayName: "Node Two", ServiceID: "service.two", Status: "active", SortOrder: 2},
			{ID: "node.three", DisplayName: "Node Three", ServiceID: "service.three", Status: "active", SortOrder: 3},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.step.one", DisplayName: "Step One", NodeID: "node.one", RequiredForAdmission: true, Status: "active", SortOrder: 1},
			{ID: "case.step.two.config", DisplayName: "Step Two", NodeID: "node.two", RequiredForAdmission: true, Status: "active", SortOrder: 2},
			{ID: "case.step.three", DisplayName: "Step Three", NodeID: "node.three", RequiredForAdmission: true, Status: "active", SortOrder: 3},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.one", NodeID: "node.one", CaseID: "case.step.one", Required: true, SortOrder: 1},
			{WorkflowID: "workflow.alpha", StepID: "step.two", NodeID: "node.two", CaseID: "case.step.two.config", Required: true, SortOrder: 2},
			{WorkflowID: "workflow.alpha", StepID: "step.three", NodeID: "node.three", CaseID: "case.step.three", Required: true, SortOrder: 3},
		},
		Fixtures: []store.CatalogFixture{
			{ID: "fixture.after.second", DisplayName: "After second", Kind: "workflow_prefix", SourceWorkflowID: "workflow.alpha", SourceUntilStep: "step.two", Status: "active", SortOrder: 1},
		},
		CaseDependencies: []store.CatalogCaseDependency{
			{ID: "dependency.step.three", CaseID: "case.step.three", FixtureID: "fixture.after.second", Required: true, MappingsJSON: `[{"from":"$.exports.item_id","to":"$.request.item_id"}]`, Status: "active", SortOrder: 1},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:        "run.alpha",
		ProfileID: "sample",
		Status:    store.StatusPassed,
		SummaryJSON: `{"steps":[
			{"stepId":"step.one","caseId":"case.step.one","ok":true},
			{"stepId":"step.two","caseId":"case.step.two.runtime","ok":true},
			{"stepId":"step.three","caseId":"case.step.three","ok":true}
		]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	for _, item := range []store.APICaseRun{
		{ID: "run.alpha.case.one", RunID: "run.alpha", CaseID: "case.step.one", Status: store.StatusPassed, RequestSummaryJSON: `{"method":"POST","path":"/one"}`, AssertionSummaryJSON: `{"status":"passed"}`},
		{ID: "run.alpha.case.two", RunID: "run.alpha", CaseID: "case.step.two.runtime", Status: store.StatusPassed, RequestSummaryJSON: `{"method":"POST","path":"/two"}`, AssertionSummaryJSON: `{"status":"passed"}`},
		{ID: "run.alpha.case.three", RunID: "run.alpha", CaseID: "case.step.three", Status: store.StatusPassed, RequestSummaryJSON: `{"method":"POST","path":"/three"}`, AssertionSummaryJSON: `{"status":"passed"}`},
	} {
		if _, err := s.RecordAPICaseRun(ctx, item); err != nil {
			t.Fatalf("record api case run: %v", err)
		}
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId=run.alpha&caseId=case.step.three", http.StatusOK)
	evidence := payload["evidence"].(map[string]any)
	fixture := evidence["fixture"].(map[string]any)
	if fixture["status"] != "configured" {
		t.Fatalf("fixture status = %#v", fixture)
	}
	dependencies := fixture["dependencies"].([]any)
	if len(dependencies) != 1 {
		t.Fatalf("dependencies = %#v", fixture)
	}
	dependency := dependencies[0].(map[string]any)
	if dependency["fixtureProfileId"] != "fixture.after.second" {
		t.Fatalf("dependency = %#v", dependency)
	}
	upstreamSteps := fixture["upstreamSteps"].([]any)
	if len(upstreamSteps) != 2 {
		t.Fatalf("upstream steps = %#v", fixture)
	}
	applyRuns := fixture["applyRuns"].([]any)
	if len(applyRuns) != 2 {
		t.Fatalf("apply runs = %#v", fixture)
	}
	firstApply := applyRuns[0].(map[string]any)
	if firstApply["caseId"] != "case.step.one" || firstApply["runId"] != "run.alpha" || firstApply["status"] != "applied" {
		t.Fatalf("first apply run = %#v", firstApply)
	}
	secondApply := applyRuns[1].(map[string]any)
	if secondApply["caseId"] != "case.step.two.runtime" || secondApply["stepId"] != "step.two" {
		t.Fatalf("second apply run = %#v", secondApply)
	}
	fixtureSummary := fixture["summary"].(map[string]any)
	if fixtureSummary["applyCount"] != float64(2) || fixtureSummary["failedCount"] != float64(0) {
		t.Fatalf("fixture summary = %#v", fixtureSummary)
	}
	topology := evidence["topology"].(map[string]any)
	if topology["status"] != "unavailable" {
		t.Fatalf("topology = %#v", topology)
	}
	edges := topology["confirmedEdges"].([]any)
	if len(edges) != 0 {
		t.Fatalf("workflow order must not be exposed as confirmed topology edges: %#v", topology)
	}
}

func TestServerSelectsCaseEvidenceWithinWorkflowRun(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		SummaryJSON: `{"steps":[
			{"stepId":"step.one","caseId":"case.one"},
			{"stepId":"step.two","caseId":"case.two"}
		]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	for _, item := range []store.APICaseRun{
		{ID: "run.alpha.case.01", RunID: "run.alpha", CaseID: "case.one", Status: store.StatusPassed, AssertionSummaryJSON: `{"status":"passed"}`},
		{ID: "run.alpha.case.02", RunID: "run.alpha", CaseID: "case.two", Status: store.StatusPassed, AssertionSummaryJSON: `{"status":"passed"}`},
	} {
		if _, err := s.RecordAPICaseRun(ctx, item); err != nil {
			t.Fatalf("record case run: %v", err)
		}
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.two",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.two",
		CaseID:        "case.two",
		RequestID:     "request.two",
		TraceID:       "trace.two",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"service.one","target":"service.two"}],"externalExits":[],"unresolvedExits":[]}`,
	}); err != nil {
		t.Fatalf("save trace topology: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId=run.alpha&caseId=case.two", http.StatusOK)
	evidence := payload["evidence"].(map[string]any)
	summary := evidence["summary"].(map[string]any)
	if summary["case_id"] != "case.two" {
		t.Fatalf("selected evidence summary = %#v", payload)
	}
	topology := evidence["topology"].(map[string]any)
	if topology["traceId"] != "trace.two" || len(topology["confirmedEdges"].([]any)) != 1 {
		t.Fatalf("selected evidence topology = %#v", topology)
	}

	byStep := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId=run.alpha&stepId=step.two", http.StatusOK)
	byStepEvidence := byStep["evidence"].(map[string]any)
	byStepSummary := byStepEvidence["summary"].(map[string]any)
	if byStepSummary["case_id"] != "case.two" {
		t.Fatalf("step-selected evidence summary = %#v", byStep)
	}
}

func TestServerExposesCaseTimingFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		runID    string
		caseID   string
		duration time.Duration
	}{
		{runID: "run.fast", caseID: "case.fast", duration: 150 * time.Millisecond},
		{runID: "run.slow", caseID: "case.slow", duration: 1250 * time.Millisecond},
	} {
		_, err = s.CreateRun(ctx, store.Run{
			ID:           item.runID,
			ProfileID:    "sample",
			Status:       store.StatusPassed,
			EvidenceRoot: ".runtime/evidence/" + item.runID,
			SummaryJSON:  "{}",
			StartedAt:    started,
			FinishedAt:   started.Add(item.duration),
			CreatedAt:    started,
			UpdatedAt:    started.Add(item.duration),
		})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
			ID:                   item.runID + ".case",
			RunID:                item.runID,
			CaseID:               item.caseID,
			Status:               store.StatusPassed,
			RequestSummaryJSON:   `{"method":"GET","path":"/timing"}`,
			AssertionSummaryJSON: `{"status":"passed","errorCount":0}`,
			StartedAt:            started,
			FinishedAt:           started.Add(item.duration),
			CreatedAt:            started,
		})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/timing?kind=case", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["caseRunCount"] != float64(2) || summary["durationMeasuredCount"] != float64(2) || summary["maxDurationMs"] != float64(1250) {
		t.Fatalf("case timing summary = %#v", summary)
	}
	slowest := summary["slowestRows"].(map[string]any)["caseRun"].(map[string]any)
	if slowest["id"] != "run.slow.case" || slowest["caseId"] != "case.slow" || slowest["durationMs"] != float64(1250) {
		t.Fatalf("slowest timing row = %#v", slowest)
	}
}

func TestServerExposesEmptyAgentTestWorkbench(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/agent-test")
	if err != nil {
		t.Fatalf("get agent test api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent test status = %d", resp.StatusCode)
	}

	var payload struct {
		Summary struct {
			CapabilityCount int `json:"capabilityCount"`
			ProfileCount    int `json:"profileCount"`
			RunCount        int `json:"runCount"`
		} `json:"summary"`
		Capabilities      []map[string]any `json:"capabilities"`
		Profiles          []map[string]any `json:"profiles"`
		AgentRuns         []map[string]any `json:"agentRuns"`
		ConfigEvents      []map[string]any `json:"configEvents"`
		EscalationEvents  []map[string]any `json:"escalationEvents"`
		AcceptanceReports []map[string]any `json:"acceptanceReports"`
		Warnings          []string         `json:"warnings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode agent test api: %v", err)
	}
	if payload.Summary.CapabilityCount != 0 || payload.Summary.ProfileCount != 0 || payload.Summary.RunCount != 0 {
		t.Fatalf("agent test summary = %#v", payload.Summary)
	}
	if payload.Capabilities == nil || payload.Profiles == nil || payload.AgentRuns == nil || payload.ConfigEvents == nil || payload.EscalationEvents == nil || payload.AcceptanceReports == nil || payload.Warnings == nil {
		t.Fatalf("agent test empty arrays = %#v", payload)
	}
}

func TestServerExposesAgentTestRunsFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	first := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	for _, item := range []store.Run{
		{
			ID:           "run.alpha",
			ProfileID:    "sample",
			WorkflowID:   "workflow.alpha",
			Status:       store.StatusPassed,
			EvidenceRoot: ".runtime/evidence/run.alpha",
			SummaryJSON:  `{"diagnosisIndex":{"nextStep":"inspect evidence"}}`,
			StartedAt:    first,
			FinishedAt:   first.Add(time.Second),
			CreatedAt:    first,
		},
		{
			ID:           "run.beta",
			ProfileID:    "sample",
			WorkflowID:   "workflow.beta",
			Status:       store.StatusFailed,
			EvidenceRoot: ".runtime/evidence/run.beta",
			SummaryJSON:  `{"diagnosisIndex":{"failureKind":"dependency_missing","nextStep":"add fixture data"}}`,
			StartedAt:    first.Add(time.Minute),
			FinishedAt:   first.Add(time.Minute + time.Second),
			CreatedAt:    first.Add(time.Minute),
		},
	} {
		if _, err := s.CreateRun(ctx, item); err != nil {
			t.Fatalf("create run %s: %v", item.ID, err)
		}
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
			{ID: "workflow.beta", DisplayName: "Workflow Beta"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/agent-test", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["runCount"] != float64(2) || summary["latestFailureKind"] != "dependency_missing" {
		t.Fatalf("agent test summary = %#v", summary)
	}
	statusCounts := summary["statusCounts"].(map[string]any)
	if statusCounts[store.StatusPassed] != float64(1) || statusCounts[store.StatusFailed] != float64(1) {
		t.Fatalf("agent test status counts = %#v", statusCounts)
	}
	runs := payload["agentRuns"].([]any)
	if len(runs) != 2 {
		t.Fatalf("agent runs = %#v", runs)
	}
	latest := runs[0].(map[string]any)
	if latest["runId"] != "run.beta" || latest["profileId"] != "sample" || latest["workflowId"] != "workflow.beta" || latest["failureKind"] != "dependency_missing" {
		t.Fatalf("latest agent run = %#v", latest)
	}
	diagnosis := latest["diagnosis"].(map[string]any)
	if diagnosis["nextStep"] != "add fixture data" {
		t.Fatalf("latest diagnosis = %#v", diagnosis)
	}
	profiles := payload["profiles"].([]any)
	if len(profiles) != 1 || profiles[0].(map[string]any)["id"] != "sample" || profiles[0].(map[string]any)["stepCount"] != float64(2) {
		t.Fatalf("agent test profiles = %#v", profiles)
	}
	capabilities := payload["capabilities"].([]any)
	if len(capabilities) == 0 {
		t.Fatalf("agent test capabilities = %#v", capabilities)
	}
}

func TestServerExposesProfileAgentValidationConfig(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		AgentTestProfiles: []profile.AgentTestProfile{
			{
				ID:    "baseline",
				Title: "Baseline Chain",
				Steps: []profile.AgentTestStep{
					{Type: "workflow", ID: "workflow.baseline"},
				},
				Probes: []profile.AgentTestProbe{
					{Name: "row_count", Query: "select count(*) from records"},
				},
				EvidencePolicy: map[string]bool{"collectTrace": true, "collectLogs": true},
				ConfigPolicy: profile.AgentConfigPolicy{
					AllowedChanges: []profile.ConfigChange{{Kind: "env", Key: "SANDBOX_FLAG"}},
				},
				RequiredConfig: []profile.RequiredConfig{
					{Kind: "setting", Key: "feature.flag", SuggestedValue: "enabled", Reason: "exercise config application"},
				},
			},
		},
		ConfigAuthoring: profile.ConfigAuthoring{
			SchemaVersion:               "1",
			Role:                        "configuration-subagent",
			Summary:                     "Concrete template configuration is authored by a dedicated subagent.",
			GuidePath:                   "template-config/SKILL.md",
			AllowedWritePaths:           []string{"template-config/"},
			AllowedReadPaths:            []string{"template-config/SKILL.md"},
			MainAgentResponsibilities:   []string{"maintain tools"},
			SubagentResponsibilities:    []string{"author configuration", "report friction"},
			HandoffRequiredFields:       []string{"changedFiles", "friction"},
			FrictionCategories:          []string{"missing-model-capability"},
			RequiresDedicatedSubagent:   true,
			ProhibitsMainAgentAuthoring: true,
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, nil))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/agent-test", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["profileCount"] != float64(1) || summary["authoringContractCount"] != float64(1) {
		t.Fatalf("agent validation summary = %#v", summary)
	}
	profiles := payload["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("agent validation profiles = %#v", profiles)
	}
	agentProfile := profiles[0].(map[string]any)
	if agentProfile["id"] != "baseline" || agentProfile["stepCount"] != float64(1) || agentProfile["probeCount"] != float64(1) {
		t.Fatalf("agent validation profile = %#v", agentProfile)
	}
	if len(agentProfile["allowedChanges"].([]any)) != 1 || len(agentProfile["requiredConfig"].([]any)) != 1 {
		t.Fatalf("agent validation profile config = %#v", agentProfile)
	}
	authoring := payload["configAuthoring"].(map[string]any)
	if authoring["role"] != "configuration-subagent" || authoring["requiresDedicatedSubagent"] != true || authoring["prohibitsMainAgentAuthoring"] != true {
		t.Fatalf("config authoring = %#v", authoring)
	}
}

func TestServerExposesAPICaseCapabilities(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{
				ID:             "case.alpha",
				DisplayName:    "Case Alpha",
				NodeID:         "node.alpha",
				CasePath:       "cases/case.alpha.json",
				SourceKind:     "karate",
				SourcePath:     "tests/api.feature",
				ExecutorID:     "executor.karate",
				BaseURL:        "http://127.0.0.1:18080",
				EvidenceDir:    ".runtime/cases",
				TimeoutSeconds: 30,
				DefaultOverrides: map[string]any{
					"itemId": "item-001",
				},
			},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/cases/capabilities")
	if err != nil {
		t.Fatalf("get api case capabilities: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("api case capabilities status = %d", resp.StatusCode)
	}

	var payload struct {
		Cases []struct {
			ID               string         `json:"id"`
			Title            string         `json:"title"`
			Operation        string         `json:"operation"`
			CasePath         string         `json:"casePath"`
			SourceKind       string         `json:"sourceKind"`
			SourcePath       string         `json:"sourcePath"`
			ExecutorID       string         `json:"executorId"`
			BaseURL          string         `json:"baseUrl"`
			EvidenceDir      string         `json:"evidenceDir"`
			TimeoutSeconds   int            `json:"timeoutSeconds"`
			DefaultOverrides map[string]any `json:"defaultOverrides"`
			Graph            struct {
				Nodes []struct {
					ID          string `json:"id"`
					DisplayName string `json:"displayName"`
					Role        string `json:"role"`
				} `json:"nodes"`
			} `json:"graph"`
		} `json:"cases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode api case capabilities: %v", err)
	}
	if len(payload.Cases) != 1 || payload.Cases[0].ID != "case.alpha" || payload.Cases[0].Operation != "Node Alpha" {
		t.Fatalf("api case capabilities = %#v", payload.Cases)
	}
	if payload.Cases[0].CasePath != "cases/case.alpha.json" || payload.Cases[0].SourceKind != "karate" || payload.Cases[0].SourcePath != "tests/api.feature" || payload.Cases[0].ExecutorID != "executor.karate" || payload.Cases[0].BaseURL == "" || payload.Cases[0].EvidenceDir != ".runtime/cases" || payload.Cases[0].TimeoutSeconds != 30 || payload.Cases[0].DefaultOverrides["itemId"] != "item-001" {
		t.Fatalf("api case run config = %#v", payload.Cases[0])
	}
	if len(payload.Cases[0].Graph.Nodes) != 1 || payload.Cases[0].Graph.Nodes[0].ID != "service.alpha" || payload.Cases[0].Graph.Nodes[0].Role != "http" {
		t.Fatalf("api case graph = %#v", payload.Cases[0].Graph)
	}
}

func TestServerExposesAPICaseCapabilitiesFromStoreCatalog(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC),
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{
				ID:                   "case.alpha",
				DisplayName:          "Case Alpha",
				NodeID:               "node.alpha",
				CasePath:             "cases/case.alpha.json",
				SourceKind:           "karate",
				SourcePath:           "tests/api.feature",
				ExecutorID:           "executor.karate",
				BaseURL:              "http://127.0.0.1:18080",
				EvidenceDir:          ".runtime/cases",
				TimeoutSeconds:       30,
				DefaultOverridesJSON: `{"itemId":"item-001"}`,
				RequiredForAdmission: true,
				Status:               "active",
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "empty", DisplayName: "Empty Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/cases/capabilities", http.StatusOK)
	cases := payload["cases"].([]any)
	if len(cases) != 1 {
		t.Fatalf("api case capabilities from store = %#v", payload)
	}
	item := cases[0].(map[string]any)
	if item["id"] != "case.alpha" || item["casePath"] != "cases/case.alpha.json" || item["sourceKind"] != "karate" || item["sourcePath"] != "tests/api.feature" || item["executorId"] != "executor.karate" || item["baseUrl"] != "http://127.0.0.1:18080" || item["evidenceDir"] != ".runtime/cases" || item["timeoutSeconds"] != float64(30) {
		t.Fatalf("api case store run config = %#v", item)
	}
	overrides := item["defaultOverrides"].(map[string]any)
	if overrides["itemId"] != "item-001" {
		t.Fatalf("api case store default overrides = %#v", overrides)
	}
	graph := item["graph"].(map[string]any)
	nodes := graph["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["id"] != "service.alpha" {
		t.Fatalf("api case store graph = %#v", graph)
	}
}

func TestServerExposesAPICaseCapabilityRunsFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusFailed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		CreatedAt:    started,
		UpdatedAt:    started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.alpha.case",
		RunID:                "run.alpha",
		CaseID:               "case.alpha",
		Status:               store.StatusFailed,
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
		StartedAt:            started,
		FinishedAt:           started.Add(200 * time.Millisecond),
		CreatedAt:            started,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
			{ID: "case.empty", DisplayName: "Case Empty", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/cases/capabilities", http.StatusOK)
	cases := payload["cases"].([]any)
	alpha := cases[0].(map[string]any)
	if alpha["id"] != "case.alpha" || alpha["runCount"] != float64(1) {
		t.Fatalf("api case run count = %#v", alpha)
	}
	latest := alpha["latestRun"].(map[string]any)
	if latest["runId"] != "run.alpha" || latest["status"] != store.StatusFailed || latest["failureReason"] != "assertion errors: 1" {
		t.Fatalf("api case latest run = %#v", latest)
	}
	empty := cases[1].(map[string]any)
	if empty["id"] != "case.empty" || empty["runCount"] != float64(0) {
		t.Fatalf("empty api case run state = %#v", empty)
	}
	if _, ok := empty["latestRun"]; ok {
		t.Fatalf("empty api case should not expose latestRun: %#v", empty)
	}
}

func TestServerRunsAPICaseAndIndexesStoreRecords(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/items" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode target request: %v", err)
		}
		if request["id"] != "item-override" {
			t.Fatalf("target request overrides = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"created"}`))
	}))
	defer target.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	if err := os.WriteFile(casePath, []byte(`{
  "id": "case.alpha",
  "title": "Create Item",
  "request": {
    "method": "POST",
    "path": "/v1/items",
    "headers": {"Content-Type": "application/json"},
    "body": {"id": "item-001"}
  },
  "assertions": {
    "expectedStatusCodes": [200],
    "responseContains": ["created"]
  }
}`), 0o644); err != nil {
		t.Fatalf("write api case: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	body := `{"casePath":` + strconv.Quote(casePath) + `,"baseUrl":` + strconv.Quote(target.URL) + `,"evidenceDir":` + strconv.Quote(filepath.Join(dir, "evidence")) + `,"overrides":{"id":"item-override"}}`
	resp, err := http.Post(server.URL+"/api/cases/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post api case run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case run status = %d body=%s", resp.StatusCode, raw)
	}
	var payload struct {
		OK        bool   `json:"ok"`
		ViewerURL string `json:"viewerUrl"`
		Report    struct {
			RunID          string `json:"run_id"`
			CaseID         string `json:"case_id"`
			Status         string `json:"status"`
			Operation      string `json:"operation"`
			ActualHTTPCode int    `json:"actual_http_code"`
			ElapsedMs      int64  `json:"elapsed_ms"`
		} `json:"report"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode api case run response: %v", err)
	}
	if !payload.OK || payload.Report.CaseID != "case.alpha" || payload.Report.Status != store.StatusPassed || payload.ViewerURL == "" {
		t.Fatalf("api case run payload = %#v", payload)
	}
	if payload.Report.RunID == "" || payload.Report.ElapsedMs < 0 {
		t.Fatalf("api case run timing = %#v", payload.Report)
	}
	if payload.Report.Operation != "POST /v1/items" || payload.Report.ActualHTTPCode != 200 {
		t.Fatalf("api case run report details = %#v", payload.Report)
	}

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != payload.Report.RunID || runs[0].Status != store.StatusPassed {
		t.Fatalf("stored runs = %#v", runs)
	}
	caseRuns, err := s.ListAPICaseRuns(ctx, payload.Report.RunID)
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != "case.alpha" || !caseRuns[0].FinishedAt.After(caseRuns[0].StartedAt) {
		t.Fatalf("stored api case runs = %#v", caseRuns)
	}
	evidence, err := s.ListEvidence(ctx, payload.Report.RunID)
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidence) < 4 {
		t.Fatalf("stored evidence = %#v", evidence)
	}
}

func TestServerStartsAsyncAPICaseBatchRunForNodes(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/items":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode target request: %v", err)
			}
			if request["id"] != "item-override" {
				http.Error(w, "missing override", http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"created"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/items/item-override":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"found"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer target.Close()

	dir := t.TempDir()
	firstCasePath := filepath.Join(dir, "case-alpha.json")
	if err := os.WriteFile(firstCasePath, []byte(`{
  "id": "case.alpha",
  "title": "Create Item",
  "request": {
    "method": "POST",
    "path": "/v1/items",
    "headers": {"Content-Type": "application/json"},
    "body": {"id": "item-001"}
  },
  "assertions": {
    "expectedStatusCodes": [200],
    "responseContains": ["created"]
  }
}`), 0o644); err != nil {
		t.Fatalf("write first api case: %v", err)
	}
	secondCasePath := filepath.Join(dir, "case-beta.json")
	if err := os.WriteFile(secondCasePath, []byte(`{
  "id": "case.beta",
  "title": "Find Item",
  "request": {
    "method": "GET",
    "path": "/v1/items/item-override"
  },
  "assertions": {
    "expectedStatusCodes": [200],
    "responseContains": ["found"]
  }
}`), 0o644); err != nil {
		t.Fatalf("write second api case: %v", err)
	}

	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
			{ID: "node.beta", DisplayName: "Node Beta"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", CasePath: firstCasePath, BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence")},
			{ID: "case.beta", DisplayName: "Case Beta", NodeID: "node.beta", CasePath: secondCasePath, BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence")},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"change-001","nodeIds":["node.alpha","node.beta"],"overrides":{"id":"item-override"}}`
	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		OK                  bool   `json:"ok"`
		BatchRunID          string `json:"batchRunId"`
		RequestID           string `json:"requestId"`
		Status              string `json:"status"`
		ReportURL           string `json:"reportUrl"`
		HTMLReportURL       string `json:"htmlReportUrl"`
		JUnitReportURL      string `json:"junitReportUrl"`
		ArtifactManifestURL string `json:"artifactManifestUrl"`
		Total               int    `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode api case batch run response: %v", err)
	}
	if !created.OK || created.BatchRunID == "" || created.RequestID != "change-001" || created.ReportURL == "" || created.HTMLReportURL == "" || created.JUnitReportURL == "" || created.ArtifactManifestURL == "" || created.Total != 2 {
		t.Fatalf("api case batch run response = %#v", created)
	}

	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.Completed != 2 || report.Passed != 2 || report.Failed != 0 || len(report.Cases) != 2 {
		t.Fatalf("api case batch report = %#v", report)
	}
	if report.HTMLReportPath == "" || !strings.HasPrefix(report.HTMLReportPath, filepath.Join(dir, "evidence")) || report.HTMLReportURL != created.HTMLReportURL {
		t.Fatalf("api case batch html report fields = %#v", report)
	}
	if report.JUnitReportPath == "" || !strings.HasPrefix(report.JUnitReportPath, filepath.Join(dir, "evidence")) || report.JUnitReportURL != created.JUnitReportURL {
		t.Fatalf("api case batch junit report fields = %#v", report)
	}
	if report.ArtifactManifestPath == "" || !strings.HasPrefix(report.ArtifactManifestPath, filepath.Join(dir, "evidence")) || report.ArtifactManifestURL != created.ArtifactManifestURL {
		t.Fatalf("api case batch artifact manifest fields = %#v", report)
	}
	htmlResp, err := http.Get(server.URL + report.HTMLReportURL)
	if err != nil {
		t.Fatalf("get api case batch html report: %v", err)
	}
	defer htmlResp.Body.Close()
	if htmlResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(htmlResp.Body)
		t.Fatalf("api case batch html status = %d body=%s", htmlResp.StatusCode, raw)
	}
	htmlRaw, err := io.ReadAll(htmlResp.Body)
	if err != nil {
		t.Fatalf("read api case batch html report: %v", err)
	}
	html := string(htmlRaw)
	if !strings.Contains(html, "API Case Batch Report") || !strings.Contains(html, "change-001") || !strings.Contains(html, "case.alpha") || !strings.Contains(html, "case.beta") {
		t.Fatalf("api case batch html report = %s", html)
	}
	if _, err := os.Stat(report.HTMLReportPath); err != nil {
		t.Fatalf("stat api case batch html report: %v", err)
	}
	junitResp, err := http.Get(server.URL + report.JUnitReportURL)
	if err != nil {
		t.Fatalf("get api case batch junit report: %v", err)
	}
	defer junitResp.Body.Close()
	junitRaw, err := io.ReadAll(junitResp.Body)
	if err != nil {
		t.Fatalf("read api case batch junit report: %v", err)
	}
	for _, want := range []string{`<testsuite name="API Case Batch change-001" tests="2" failures="0"`, `name="case.alpha"`, `classname="node.alpha"`} {
		if !strings.Contains(string(junitRaw), want) {
			t.Fatalf("api case batch junit missing %q: %s", want, junitRaw)
		}
	}
	if _, err := os.Stat(report.JUnitReportPath); err != nil {
		t.Fatalf("stat api case batch junit report: %v", err)
	}
	manifestResp, err := http.Get(server.URL + report.ArtifactManifestURL)
	if err != nil {
		t.Fatalf("get api case batch artifact manifest: %v", err)
	}
	defer manifestResp.Body.Close()
	var manifest struct {
		BatchRunID string `json:"batchRunId"`
		Status     string `json:"status"`
		Artifacts  []struct {
			Kind      string `json:"kind"`
			CaseID    string `json:"caseId,omitempty"`
			URL       string `json:"url,omitempty"`
			Path      string `json:"path,omitempty"`
			MediaType string `json:"mediaType,omitempty"`
		} `json:"artifacts"`
	}
	if err := json.NewDecoder(manifestResp.Body).Decode(&manifest); err != nil {
		t.Fatalf("decode api case batch artifact manifest: %v", err)
	}
	if manifest.BatchRunID != created.BatchRunID || manifest.Status != store.StatusPassed {
		t.Fatalf("artifact manifest header = %#v", manifest)
	}
	artifactKeys := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		artifactKeys[artifact.Kind+"|"+artifact.CaseID] = true
		if artifact.Kind == "junit" && artifact.MediaType != "application/xml" {
			t.Fatalf("junit artifact = %#v", artifact)
		}
	}
	for _, want := range []string{"json|", "html|", "junit|", "case-detail|case.alpha", "case-evidence|case.alpha", "case-detail|case.beta", "case-evidence|case.beta"} {
		if !artifactKeys[want] {
			t.Fatalf("artifact manifest missing %q: %#v", want, manifest.Artifacts)
		}
	}
	if _, err := os.Stat(report.ArtifactManifestPath); err != nil {
		t.Fatalf("stat api case batch artifact manifest: %v", err)
	}
	for _, item := range report.Cases {
		if item.RunID == "" || item.CaseRunID != item.RunID+".case" || item.ViewerURL == "" || item.DetailURL == "" || item.Status != store.StatusPassed || item.ElapsedMs < 0 {
			t.Fatalf("api case batch case report = %#v", item)
		}
	}

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("stored runs = %#v", runs)
	}
	batchRun, err := s.GetRun(ctx, created.BatchRunID)
	if err != nil {
		t.Fatalf("get stored batch run: %v", err)
	}
	if batchRun.Status != store.StatusPassed || batchRun.ProfileID != "sample" || batchRun.EvidenceRoot != filepath.Dir(report.HTMLReportPath) {
		t.Fatalf("stored batch run = %#v", batchRun)
	}
	batchEvidence, err := s.ListEvidence(ctx, created.BatchRunID)
	if err != nil {
		t.Fatalf("list batch evidence: %v", err)
	}
	evidenceByKind := map[string]store.EvidenceRecord{}
	for _, row := range batchEvidence {
		evidenceByKind[row.Kind] = row
	}
	for kind, want := range map[string]string{
		"html":              report.HTMLReportPath,
		"junit":             report.JUnitReportPath,
		"artifact-manifest": report.ArtifactManifestPath,
		"failure-summary":   filepath.Join(filepath.Dir(report.HTMLReportPath), "failures.json"),
	} {
		row, ok := evidenceByKind[kind]
		if !ok {
			t.Fatalf("batch evidence missing %s: %#v", kind, batchEvidence)
		}
		if row.URI != want || row.RunID != created.BatchRunID || row.Category != "report" || row.Visibility != "public" {
			t.Fatalf("batch evidence %s = %#v", kind, row)
		}
	}
}

func TestServerStartsAsyncAPICaseBatchRunForAllNodeCases(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/node-cases/") {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()

	dir := t.TempDir()
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Create Item", Method: "GET", Path: "/v1/node-cases"},
		},
	}
	for i := 1; i <= 3; i++ {
		caseID := fmt.Sprintf("case.alpha.%02d", i)
		casePath := filepath.Join(dir, caseID+".json")
		if err := os.WriteFile(casePath, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": "Node Case",
  "request": {"method": "GET", "path": "/v1/node-cases/%02d"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, caseID, i)), 0o644); err != nil {
			t.Fatalf("write api case: %v", err)
		}
		bundle.APICases = append(bundle.APICases, profile.APICase{
			ID:          caseID,
			DisplayName: fmt.Sprintf("Node Case %02d", i),
			NodeID:      "node.alpha",
			Scenario:    fmt.Sprintf("scenario-%02d", i),
			CasePath:    casePath,
			BaseURL:     target.URL,
			EvidenceDir: filepath.Join(dir, "evidence"),
		})
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"node-all-001","nodeIds":["node.alpha"]}`
	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ReportURL      string `json:"reportUrl"`
		HTMLReportURL  string `json:"htmlReportUrl"`
		JUnitReportURL string `json:"junitReportUrl"`
		Total          int    `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if created.Total != 3 || created.JUnitReportURL == "" {
		t.Fatalf("created batch = %#v", created)
	}

	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.Completed != 3 || report.Passed != 3 || len(report.Cases) != 3 {
		t.Fatalf("node batch report = %#v", report)
	}
	if len(report.Nodes) != 1 || report.Nodes[0].DisplayName != "Node Alpha" || report.Nodes[0].Operation != "Create Item" || report.Nodes[0].Method != "GET" || report.Nodes[0].Path != "/v1/node-cases" {
		t.Fatalf("node batch report nodes = %#v", report.Nodes)
	}
	if report.Cases[0].DisplayName != "Node Case 01" || report.Cases[0].Scenario != "scenario-01" || report.Cases[0].NodeDisplayName != "Node Alpha" || report.Cases[0].Operation != "Create Item" {
		t.Fatalf("node batch report case metadata = %#v", report.Cases[0])
	}
	htmlResp, err := http.Get(server.URL + created.HTMLReportURL)
	if err != nil {
		t.Fatalf("get node batch html report: %v", err)
	}
	defer htmlResp.Body.Close()
	htmlRaw, err := io.ReadAll(htmlResp.Body)
	if err != nil {
		t.Fatalf("read node batch html report: %v", err)
	}
	html := string(htmlRaw)
	for _, want := range []string{"Node Alpha", "Create Item", "GET", "/v1/node-cases", "Node Case 01", "scenario-01"} {
		if !strings.Contains(html, want) {
			t.Fatalf("node batch html missing %q: %s", want, html)
		}
	}
	junitResp, err := http.Get(server.URL + created.JUnitReportURL)
	if err != nil {
		t.Fatalf("get node batch junit report: %v", err)
	}
	defer junitResp.Body.Close()
	junitRaw, err := io.ReadAll(junitResp.Body)
	if err != nil {
		t.Fatalf("read node batch junit report: %v", err)
	}
	for _, want := range []string{`<testsuite name="API Case Batch node-all-001" tests="3" failures="0"`, `name="case.alpha.01"`, `classname="node.alpha"`} {
		if !strings.Contains(string(junitRaw), want) {
			t.Fatalf("node batch junit missing %q: %s", want, junitRaw)
		}
	}
}

func TestServerStartsAsyncAPICaseBatchRunForExactCaseIDs(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/exact/first", "/v1/exact/third":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/exact/second":
			t.Fatalf("unselected case should not be run")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer target.Close()

	dir := t.TempDir()
	casePath := func(id string, path string) string {
		t.Helper()
		out := filepath.Join(dir, id+".json")
		if err := os.WriteFile(out, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": %q,
  "request": {"method": "GET", "path": %q},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, id, id, path)), 0o644); err != nil {
			t.Fatalf("write api case %s: %v", id, err)
		}
		return out
	}
	bundle := profile.Bundle{
		ID:      "sample",
		BaseDir: dir,
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Exact"},
		},
		APICases: []profile.APICase{
			{ID: "case.first", DisplayName: "First Case", NodeID: "node.alpha", CasePath: casePath("case.first", "/v1/exact/first"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 1},
			{ID: "case.second", DisplayName: "Second Case", NodeID: "node.alpha", CasePath: casePath("case.second", "/v1/exact/second"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 2},
			{ID: "case.third", DisplayName: "Third Case", NodeID: "node.alpha", CasePath: casePath("case.third", "/v1/exact/third"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 3},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"exact-001","caseIds":["case.third","case.first"]}`
	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ReportURL string   `json:"reportUrl"`
		CaseIDs   []string `json:"caseIds"`
		Total     int      `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if created.Total != 2 || strings.Join(created.CaseIDs, ",") != "case.third,case.first" {
		t.Fatalf("created exact batch = %#v", created)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.Completed != 2 || len(report.Cases) != 2 {
		t.Fatalf("exact batch report = %#v", report)
	}
	if report.Cases[0].CaseID != "case.third" || report.Cases[1].CaseID != "case.first" {
		t.Fatalf("exact case order = %#v", report.Cases)
	}
}

func TestServerExposesAPICaseBatchFailureSummary(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/failures/pass":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/failures/fail":
			http.Error(w, "not ok", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer target.Close()

	dir := t.TempDir()
	casePath := func(id string, path string) string {
		t.Helper()
		out := filepath.Join(dir, id+".json")
		if err := os.WriteFile(out, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": %q,
  "request": {"method": "GET", "path": %q},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, id, id, path)), 0o644); err != nil {
			t.Fatalf("write api case %s: %v", id, err)
		}
		return out
	}
	bundle := profile.Bundle{
		ID: "sample",
		FailureCategories: []profile.FailureCategoryRule{
			{
				Name: "Product errors",
				Matchers: profile.FailureCategoryMatchers{
					Statuses:          []string{store.StatusFailed},
					FailureCategories: []string{"assertion-mismatch"},
					MessageContains:   []string{"not expected"},
				},
			},
			{
				Name: "Later matching rule",
				Matchers: profile.FailureCategoryMatchers{
					Statuses:          []string{store.StatusFailed},
					FailureCategories: []string{"assertion-mismatch"},
				},
			},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Failure Summary"},
		},
		APICases: []profile.APICase{
			{ID: "case.pass", DisplayName: "Passing Case", NodeID: "node.alpha", CasePath: casePath("case.pass", "/v1/failures/pass"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence")},
			{ID: "case.fail", DisplayName: "Failing Case", NodeID: "node.alpha", CasePath: casePath("case.fail", "/v1/failures/fail"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence")},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(`{"requestId":"failure-001","caseIds":["case.pass","case.fail"]}`))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ReportURL         string `json:"reportUrl"`
		FailureSummaryURL string `json:"failureSummaryUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if created.FailureSummaryURL == "" {
		t.Fatalf("failure summary url missing: %#v", created)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if report.OK || report.Status != store.StatusFailed || report.Failed != 1 {
		t.Fatalf("failed batch report = %#v", report)
	}

	summaryResp, err := http.Get(server.URL + created.FailureSummaryURL)
	if err != nil {
		t.Fatalf("get failure summary: %v", err)
	}
	defer summaryResp.Body.Close()
	var summary struct {
		OK         bool   `json:"ok"`
		BatchRunID string `json:"batchRunId"`
		RequestID  string `json:"requestId"`
		Failed     int    `json:"failed"`
		Failures   []struct {
			CaseID          string `json:"caseId"`
			CaseRunID       string `json:"caseRunId"`
			Status          string `json:"status"`
			FailureCategory string `json:"failureCategory"`
			DetailURL       string `json:"detailUrl"`
			EvidencePath    string `json:"evidencePath"`
			Error           string `json:"error"`
		} `json:"failures"`
	}
	if err := json.NewDecoder(summaryResp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode failure summary: %v", err)
	}
	if summary.OK || summary.RequestID != "failure-001" || summary.Failed != 1 || len(summary.Failures) != 1 {
		t.Fatalf("failure summary = %#v", summary)
	}
	failure := summary.Failures[0]
	if failure.CaseID != "case.fail" || failure.Status != store.StatusFailed || failure.FailureCategory != "Product errors" || failure.CaseRunID == "" || failure.DetailURL == "" || failure.EvidencePath == "" || failure.Error == "" {
		t.Fatalf("failure item = %#v", failure)
	}
}

func TestServerStartsAsyncAPICaseBatchRunForMaintainedSuiteRunStates(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/suite/variant", "/v1/suite/unrun":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/suite/passed":
			t.Fatalf("already passed case should not be rerun")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer target.Close()

	dir := t.TempDir()
	casePath := func(id string, path string) string {
		t.Helper()
		out := filepath.Join(dir, id+".json")
		if err := os.WriteFile(out, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": %q,
  "request": {"method": "GET", "path": %q},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, id, id, path)), 0o644); err != nil {
			t.Fatalf("write api case %s: %v", id, err)
		}
		return out
	}
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Suite", Method: "GET", Path: "/v1/suite"},
		},
		APICases: []profile.APICase{
			{ID: "case.passed", DisplayName: "Passed Case", NodeID: "node.alpha", Tags: []string{"regression"}, Owner: "team-a", Priority: "p0", CasePath: casePath("case.passed", "/v1/suite/passed"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 1},
			{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", Tags: []string{"regression"}, Owner: "team-a", Priority: "p1", CasePath: casePath("case.variant", "/v1/suite/variant"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Owner: "team-b", Priority: "p2", CasePath: casePath("case.unrun", "/v1/suite/unrun"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 3},
			{ID: "case.other", DisplayName: "Other Case", NodeID: "node.alpha", Tags: []string{"smoke"}, Owner: "team-a", Priority: "p2", CasePath: casePath("case.other", "/v1/suite/other"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 4},
		},
	}
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	for _, item := range []struct {
		runID  string
		caseID string
		status string
		at     time.Time
	}{
		{runID: "run.passed.latest", caseID: "case.passed", status: store.StatusPassed, at: base},
		{runID: "run.variant.latest", caseID: "case.variant", status: store.StatusFailed, at: base.Add(time.Minute)},
	} {
		_, err = s.CreateRun(ctx, store.Run{ID: item.runID, ProfileID: "sample", WorkflowID: item.caseID, Status: item.status, StartedAt: item.at, FinishedAt: item.at.Add(time.Second), CreatedAt: item.at, UpdatedAt: item.at.Add(time.Second)})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{ID: item.runID + ".case", RunID: item.runID, CaseID: item.caseID, Status: item.status, StartedAt: item.at, FinishedAt: item.at.Add(time.Second), CreatedAt: item.at})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"suite-rerun-001","suite":{"tags":["regression"],"status":"active","runStates":["failed","not-run"]}}`
	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post suite batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("suite batch status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ReportURL string `json:"reportUrl"`
		Total     int    `json:"total"`
		Suite     struct {
			Tags      []string `json:"tags"`
			RunStates []string `json:"runStates"`
		} `json:"suite"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode suite batch response: %v", err)
	}
	if created.Total != 2 || strings.Join(created.Suite.Tags, ",") != "regression" || strings.Join(created.Suite.RunStates, ",") != "failed,not-run" {
		t.Fatalf("suite batch response = %#v", created)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Completed != 2 || report.Passed != 2 || len(report.Cases) != 2 {
		t.Fatalf("suite batch report = %#v", report)
	}
	gotCases := []string{report.Cases[0].CaseID, report.Cases[1].CaseID}
	if strings.Join(gotCases, ",") != "case.variant,case.unrun" {
		t.Fatalf("suite rerun cases = %#v", gotCases)
	}
}

func TestServerStartsAsyncAPICaseBatchRunForWorkflow(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/workflow-steps/") {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()

	dir := t.TempDir()
	bundle := profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.ten", DisplayName: "Ten Step Workflow"},
		},
	}
	for i := 1; i <= 10; i++ {
		stepID := fmt.Sprintf("step-%02d", i)
		nodeID := fmt.Sprintf("node.step.%02d", i)
		caseID := fmt.Sprintf("case.step.%02d", i)
		casePath := filepath.Join(dir, caseID+".json")
		if err := os.WriteFile(casePath, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": "Workflow Step",
  "request": {"method": "GET", "path": "/v1/workflow-steps/%02d"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, caseID, i)), 0o644); err != nil {
			t.Fatalf("write api case: %v", err)
		}
		bundle.InterfaceNodes = append(bundle.InterfaceNodes, profile.InterfaceNode{ID: nodeID, DisplayName: nodeID})
		bundle.APICases = append(bundle.APICases, profile.APICase{
			ID:          caseID,
			DisplayName: caseID,
			NodeID:      nodeID,
			CasePath:    casePath,
			BaseURL:     target.URL,
			EvidenceDir: filepath.Join(dir, "evidence"),
		})
		bundle.WorkflowBindings = append(bundle.WorkflowBindings, profile.WorkflowBinding{
			WorkflowID: "workflow.ten",
			StepID:     stepID,
			NodeID:     nodeID,
			CaseID:     caseID,
			Required:   true,
			SortOrder:  i,
		})
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"workflow-001","workflowId":"workflow.ten"}`
	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ReportURL  string `json:"reportUrl"`
		WorkflowID string `json:"workflowId"`
		Total      int    `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if created.WorkflowID != "workflow.ten" || created.Total != 10 {
		t.Fatalf("workflow batch response = %#v", created)
	}

	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.WorkflowID != "workflow.ten" || report.Completed != 10 || report.Passed != 10 || len(report.Cases) != 10 {
		t.Fatalf("workflow batch report = %#v", report)
	}
	if report.Acceptance.TemplateID != "environment.workflow.skywalking.v1" || report.Acceptance.WorkflowID != "workflow.ten" || report.Acceptance.OK || report.Acceptance.ExpectedSteps != 10 || report.Acceptance.CompletedSteps != 10 || report.Acceptance.PassedSteps != 10 || report.Acceptance.TopologyProvider != "skywalking" {
		t.Fatalf("workflow acceptance report should require SkyWalking topology: %#v", report.Acceptance)
	}
	if len(report.Acceptance.Steps) != 10 || !report.Acceptance.Steps[0].EvidenceComplete || report.Acceptance.Steps[0].TopologyComplete {
		t.Fatalf("workflow acceptance steps = %#v", report.Acceptance.Steps)
	}
	if report.Cases[0].StepID != "step-01" || report.Cases[9].StepID != "step-10" {
		t.Fatalf("workflow step order = %#v", report.Cases)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 11 {
		t.Fatalf("stored runs = %#v", runs)
	}
	batchRun, err := s.GetRun(ctx, report.BatchRunID)
	if err != nil {
		t.Fatalf("get stored workflow batch run: %v", err)
	}
	if batchRun.Status != store.StatusPassed || batchRun.WorkflowID != "workflow.ten" {
		t.Fatalf("stored workflow batch run = %#v", batchRun)
	}
	var storedSummary struct {
		Summary struct {
			ExpectedStepCount int `json:"expectedStepCount"`
			StepCount         int `json:"stepCount"`
			Passed            int `json:"passed"`
			Failed            int `json:"failed"`
		} `json:"summary"`
		Steps []struct {
			StepID string `json:"stepId"`
			CaseID string `json:"caseId"`
			Status string `json:"status"`
		} `json:"steps"`
		Acceptance struct {
			OK               bool   `json:"ok"`
			TemplateID       string `json:"templateId"`
			TopologyProvider string `json:"topologyProvider"`
		} `json:"acceptance"`
	}
	if err := json.Unmarshal([]byte(batchRun.SummaryJSON), &storedSummary); err != nil {
		t.Fatalf("decode stored workflow batch summary: %v", err)
	}
	if storedSummary.Summary.ExpectedStepCount != 10 || storedSummary.Summary.StepCount != 10 || storedSummary.Summary.Passed != 10 || storedSummary.Summary.Failed != 0 || len(storedSummary.Steps) != 10 {
		t.Fatalf("stored workflow run summary counts = %#v", storedSummary)
	}
	if storedSummary.Steps[0].StepID != "step-01" || storedSummary.Steps[0].CaseID == "" || storedSummary.Steps[0].Status != store.StatusPassed {
		t.Fatalf("stored workflow run steps = %#v", storedSummary.Steps)
	}
	if storedSummary.Acceptance.OK || storedSummary.Acceptance.TemplateID != "environment.workflow.skywalking.v1" || storedSummary.Acceptance.TopologyProvider != "skywalking" {
		t.Fatalf("stored workflow acceptance summary = %#v", storedSummary.Acceptance)
	}
}

func TestServerAsyncWorkflowAcceptancePassesWithSkyWalkingTopology(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "request.acceptance")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["GET:/v1/acceptance"],"duration":80,"start":"2026-05-20 0320","isError":false,"traceIds":["trace.acceptance"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.acceptance","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/v1/acceptance","type":"Entry","component":"Tomcat"},{"traceId":"trace.acceptance","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.acceptance","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"GET:/v1/acceptance","type":"Entry","component":"Server"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case-acceptance.json")
	if err := os.WriteFile(casePath, []byte(`{
  "id": "case.acceptance",
  "title": "Acceptance Step",
  "request": {"method": "GET", "path": "/v1/acceptance"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`), 0o644); err != nil {
		t.Fatalf("write api case: %v", err)
	}
	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.acceptance", DisplayName: "Acceptance Workflow"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.acceptance", DisplayName: "Acceptance Node"}},
		APICases: []profile.APICase{{
			ID:          "case.acceptance",
			DisplayName: "Acceptance Step",
			NodeID:      "node.acceptance",
			CasePath:    casePath,
			BaseURL:     target.URL,
			EvidenceDir: filepath.Join(dir, "evidence"),
		}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.acceptance",
			StepID:     "step.acceptance",
			NodeID:     "node.acceptance",
			CaseID:     "case.acceptance",
			Required:   true,
			SortOrder:  1,
		}},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{
		Runtime:         s,
		TraceGraphQLURL: provider.URL,
	}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(`{"requestId":"workflow-acceptance-001","workflowId":"workflow.acceptance"}`))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ReportURL string `json:"reportUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.Acceptance.OK || len(report.Acceptance.Steps) != 1 || !report.Acceptance.Steps[0].EvidenceComplete || !report.Acceptance.Steps[0].TopologyComplete {
		t.Fatalf("workflow acceptance with SkyWalking = %#v", report.Acceptance)
	}
}

func TestServerRunsWorkflowCaseWithStoreExecutionConfigWithoutCaseFile(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/store-execution" {
			t.Fatalf("unexpected execution path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	now := time.Now().UTC()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Workflows: []store.CatalogWorkflow{{ID: "workflow.store-execution", DisplayName: "Store Execution Workflow"}},
		APICases:  []store.CatalogAPICase{{ID: "case.store-execution", DisplayName: "Store Execution Case", NodeID: "node.store-execution", Status: "active"}},
		Services:  []store.CatalogService{{ID: "service.store-execution", Kind: "app", ServicePort: 18080, Status: "active"}},
		InterfaceNodes: []store.CatalogInterfaceNode{{
			ID: "node.store-execution", ServiceID: "service.store-execution", Method: "POST", Path: "/store-execution", Status: "active",
		}},
		WorkflowBindings: []store.CatalogWorkflowBinding{{
			WorkflowID: "workflow.store-execution", StepID: "store-step", NodeID: "node.store-execution", CaseID: "case.store-execution", Required: true, SortOrder: 1,
		}},
		TemplateConfigs: []store.CatalogTemplateConfig{{
			ID: "cfg.case.store-execution", TemplateID: "TPL-CASE-EXECUTION-V1", ScopeType: "api-case", ScopeID: "case.store-execution", Status: "active",
			ConfigJSON: `{"caseId":"case.store-execution","caseExecution":{"method":"POST","nodeId":"node.store-execution","path":"/store-execution","body":{"hello":"store"},"expectedHttpCodes":[200],"headers":{"Content-Type":"application/json"}}}`,
		}},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.store-execution"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.store-execution", Method: "POST", Path: "/store-execution"}},
		APICases:       []profile.APICase{{ID: "case.store-execution", NodeID: "node.store-execution"}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.store-execution", StepID: "store-step", NodeID: "node.store-execution", CaseID: "case.store-execution", Required: true, SortOrder: 1,
		}},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(fmt.Sprintf(`{"requestId":"store-execution-001","workflowId":"workflow.store-execution","baseUrl":%q}`, target.URL)))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ReportURL string `json:"reportUrl"`
		Total     int    `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if created.Total != 1 {
		t.Fatalf("store execution workflow should plan one case, got %d", created.Total)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if report.Total != 1 || report.Passed != 1 {
		t.Fatalf("store execution workflow report = %#v", report)
	}
}

func TestServerBatchRunHonorsStoreExecutionExpectedResponseContains(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result_status":"F"}`))
	}))
	defer target.Close()

	now := time.Now().UTC()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Workflows: []store.CatalogWorkflow{{ID: "workflow.store-execution", DisplayName: "Store Execution Workflow"}},
		APICases:  []store.CatalogAPICase{{ID: "case.store-execution", DisplayName: "Store Execution Case", NodeID: "node.store-execution", Status: "active"}},
		InterfaceNodes: []store.CatalogInterfaceNode{{
			ID: "node.store-execution", Method: "GET", Path: "/store-execution", Status: "active",
		}},
		WorkflowBindings: []store.CatalogWorkflowBinding{{
			WorkflowID: "workflow.store-execution", StepID: "store-step", NodeID: "node.store-execution", CaseID: "case.store-execution", Required: true, SortOrder: 1,
		}},
		TemplateConfigs: []store.CatalogTemplateConfig{{
			ID: "cfg.case.store-execution", TemplateID: "TPL-CASE-EXECUTION-V1", ScopeType: "api-case", ScopeID: "case.store-execution", Status: "active",
			ConfigJSON: `{"caseId":"case.store-execution","caseExecution":{"method":"GET","nodeId":"node.store-execution","path":"/store-execution","expectedHttpCodes":[200],"expectedResponseContains":["\"result_status\":\"S\""]}}`,
		}},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.store-execution"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.store-execution", Method: "GET", Path: "/store-execution"}},
		APICases:       []profile.APICase{{ID: "case.store-execution", NodeID: "node.store-execution"}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.store-execution", StepID: "store-step", NodeID: "node.store-execution", CaseID: "case.store-execution", Required: true, SortOrder: 1,
		}},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(fmt.Sprintf(`{"requestId":"store-execution-assertion-001","workflowId":"workflow.store-execution","baseUrl":%q}`, target.URL)))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ReportURL string `json:"reportUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if report.OK || report.Status != store.StatusFailed || report.Passed != 0 || report.Failed != 1 || !strings.Contains(report.Cases[0].Error, "response did not contain") {
		t.Fatalf("store execution assertion report = %#v", report)
	}
}

func TestServerBatchRunAppliesWorkflowStepExportsAsOverrides(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	var appliedAmount string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/trial":
			_, _ = w.Write([]byte(`{"total_amount":500000}`))
		case "/apply":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode apply request: %v", err)
			}
			appliedAmount = strings.TrimSpace(fmt.Sprint(request["requested_amount"]))
			if appliedAmount != "500000" {
				_, _ = w.Write([]byte(`{"result_status":"F"}`))
				return
			}
			_, _ = w.Write([]byte(`{"result_status":"S"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer target.Close()

	now := time.Now().UTC()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Workflows: []store.CatalogWorkflow{{ID: "workflow.exports", DisplayName: "Export Workflow"}},
		APICases: []store.CatalogAPICase{
			{ID: "case.trial", DisplayName: "Trial", NodeID: "node.trial", Status: "active"},
			{ID: "case.apply", DisplayName: "Apply", NodeID: "node.apply", Status: "active"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.trial", Method: "GET", Path: "/trial", Status: "active"},
			{ID: "node.apply", Method: "POST", Path: "/apply", Status: "active"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.exports", StepID: "trial", NodeID: "node.trial", CaseID: "case.trial", Required: true, SortOrder: 1},
			{WorkflowID: "workflow.exports", StepID: "apply", NodeID: "node.apply", CaseID: "case.apply", Required: true, SortOrder: 2},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID: "cfg.trial", TemplateID: "TPL-CASE-EXECUTION-V1", WorkflowID: "workflow.exports", ScopeType: "step", ScopeID: "trial", Status: "active",
				ConfigJSON: `{"caseId":"case.trial","caseExecution":{"method":"GET","nodeId":"node.trial","path":"/trial","expectedHttpCodes":[200]},"exports":[{"from":"responseBody","name":"requested_amount","path":"total_amount"}]}`,
			},
			{
				ID: "cfg.apply", TemplateID: "TPL-CASE-EXECUTION-V1", WorkflowID: "workflow.exports", ScopeType: "step", ScopeID: "apply", Status: "active",
				ConfigJSON: `{"caseId":"case.apply","caseExecution":{"method":"POST","nodeId":"node.apply","path":"/apply","body":{"requested_amount":"{{override:requested_amount|}}"},"expectedHttpCodes":[200],"expectedResponseContains":["\"result_status\":\"S\""]}}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.exports"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.trial", Method: "GET", Path: "/trial"}, {ID: "node.apply", Method: "POST", Path: "/apply"}},
		APICases:       []profile.APICase{{ID: "case.trial", NodeID: "node.trial"}, {ID: "case.apply", NodeID: "node.apply"}},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.exports", StepID: "trial", NodeID: "node.trial", CaseID: "case.trial", Required: true, SortOrder: 1},
			{WorkflowID: "workflow.exports", StepID: "apply", NodeID: "node.apply", CaseID: "case.apply", Required: true, SortOrder: 2},
		},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s}))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(fmt.Sprintf(`{"requestId":"workflow-exports-001","workflowId":"workflow.exports","baseUrl":%q}`, target.URL)))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
	var created struct {
		ReportURL string `json:"reportUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Passed != 2 || appliedAmount != "500000" {
		t.Fatalf("workflow export report = %#v appliedAmount=%q", report, appliedAmount)
	}
}

func TestServerStartsEnvironmentAcceptanceRunWithHealthSummary(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "request.env.acceptance")
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"ready":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["GET:/v1/env-acceptance"],"duration":80,"start":"2026-05-20 0320","isError":false,"traceIds":["trace.env.acceptance"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.env.acceptance","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/v1/env-acceptance","type":"Entry","component":"Tomcat"},{"traceId":"trace.env.acceptance","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.env.acceptance","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"GET:/v1/env-acceptance","type":"Entry","component":"Server"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case-env-acceptance.json")
	if err := os.WriteFile(casePath, []byte(`{
  "id": "case.env.acceptance",
  "title": "Environment Acceptance Step",
  "request": {"method": "GET", "path": "/v1/env-acceptance"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`), 0o644); err != nil {
		t.Fatalf("write api case: %v", err)
	}
	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.env.acceptance"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.env.acceptance"}},
		APICases: []profile.APICase{{
			ID:          "case.env.acceptance",
			NodeID:      "node.env.acceptance",
			CasePath:    casePath,
			BaseURL:     target.URL,
			EvidenceDir: filepath.Join(dir, "evidence"),
		}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.env.acceptance",
			StepID:     "step.env.acceptance",
			NodeID:     "node.env.acceptance",
			CaseID:     "case.env.acceptance",
			Required:   true,
			SortOrder:  1,
		}},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s, TraceGraphQLURL: provider.URL}))
	defer server.Close()

	registered := postJSONResponse(t, server.URL+"/api/environments", fmt.Sprintf(`{
  "id": "env.acceptance",
  "verificationWorkflowId": "workflow.env.acceptance"
}`), http.StatusOK)
	if registered["ok"] != true {
		t.Fatalf("register environment = %#v", registered)
	}
	if err := s.ReplaceEnvironmentComponentGraph(ctx, "env.acceptance", store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{
				ComponentID:     "service.gateway",
				Kind:            "app",
				Role:            "business-service",
				ComposeService:  "service-gateway",
				Required:        true,
				HealthCheckJSON: fmt.Sprintf(`{"kind":"url","url":%q}`, target.URL+"/health"),
			},
		},
	}); err != nil {
		t.Fatalf("replace component graph: %v", err)
	}

	started := postJSONResponse(t, server.URL+"/api/environments/env.acceptance/acceptance-runs", `{"requestId":"env-acceptance-001"}`, http.StatusAccepted)
	reportURL := fmt.Sprint(started["reportUrl"])
	if started["environmentId"] != "env.acceptance" || started["workflowId"] != "workflow.env.acceptance" || reportURL == "" {
		t.Fatalf("environment acceptance start = %#v", started)
	}
	report := waitAPICaseBatchReport(t, server.URL+reportURL)
	if !report.Acceptance.OK || report.Acceptance.HealthSummary.Total != 1 || report.Acceptance.HealthSummary.Passed != 1 || len(report.Acceptance.NodeHealth) != 1 || !report.Acceptance.NodeHealth[0].OK {
		t.Fatalf("environment acceptance health summary = %#v", report.Acceptance)
	}
	env, err := s.GetEnvironment(ctx, "env.acceptance")
	if err != nil {
		t.Fatalf("get environment after acceptance: %v", err)
	}
	if env.Status != "verified-ready" || env.LastVerificationRunID != report.BatchRunID || env.LastVerificationStatus != store.StatusPassed || !env.EvidenceComplete || !env.TopologyComplete {
		t.Fatalf("environment after acceptance = %#v", env)
	}
	batchRun, err := s.GetRun(ctx, report.BatchRunID)
	if err != nil {
		t.Fatalf("get batch run after acceptance: %v", err)
	}
	if batchRun.EnvironmentID != "env.acceptance" {
		t.Fatalf("batch run environment id = %#v", batchRun)
	}
	topologies, err := s.ListTraceTopologies(ctx, report.BatchRunID)
	if err != nil {
		t.Fatalf("list batch topology: %v", err)
	}
	if len(topologies) != 1 || topologies[0].WorkflowRunID != report.BatchRunID || topologies[0].StepID != "step.env.acceptance" {
		t.Fatalf("batch topology copies = %#v", topologies)
	}

	restarted := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s}))
	defer restarted.Close()
	persisted := decodeJSONResponse(t, restarted.URL+reportURL, http.StatusOK)
	if persisted["environmentId"] != "env.acceptance" || persisted["batchRunId"] != report.BatchRunID {
		t.Fatalf("persisted environment acceptance report = %#v", persisted)
	}
}

type apiCaseBatchReportForTest struct {
	OK                   bool   `json:"ok"`
	BatchRunID           string `json:"batchRunId"`
	Status               string `json:"status"`
	WorkflowID           string `json:"workflowId"`
	HTMLReportPath       string `json:"htmlReportPath"`
	HTMLReportURL        string `json:"htmlReportUrl"`
	JUnitReportPath      string `json:"junitReportPath"`
	JUnitReportURL       string `json:"junitReportUrl"`
	ArtifactManifestPath string `json:"artifactManifestPath"`
	ArtifactManifestURL  string `json:"artifactManifestUrl"`
	Completed            int    `json:"completed"`
	Total                int    `json:"total"`
	Passed               int    `json:"passed"`
	Failed               int    `json:"failed"`
	Acceptance           struct {
		OK               bool   `json:"ok"`
		TemplateID       string `json:"templateId"`
		WorkflowID       string `json:"workflowId"`
		ExpectedSteps    int    `json:"expectedSteps"`
		CompletedSteps   int    `json:"completedSteps"`
		PassedSteps      int    `json:"passedSteps"`
		TopologyProvider string `json:"topologyProvider"`
		HealthSummary    struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"healthSummary"`
		NodeHealth []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			URL    string `json:"url"`
			OK     bool   `json:"ok"`
			Status string `json:"status"`
		} `json:"nodeHealth"`
		Steps []struct {
			StepID           string `json:"stepId"`
			CaseID           string `json:"caseId"`
			Status           string `json:"status"`
			ElapsedMs        int64  `json:"elapsedMs"`
			EvidenceComplete bool   `json:"evidenceComplete"`
			TopologyComplete bool   `json:"topologyComplete"`
		} `json:"steps"`
	} `json:"acceptance"`
	Nodes []struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Operation   string `json:"operation"`
		Method      string `json:"method"`
		Path        string `json:"path"`
	} `json:"nodes"`
	Cases []struct {
		CaseID          string `json:"caseId"`
		DisplayName     string `json:"displayName"`
		Scenario        string `json:"scenario"`
		NodeID          string `json:"nodeId"`
		NodeDisplayName string `json:"nodeDisplayName"`
		Operation       string `json:"operation"`
		Method          string `json:"method"`
		Path            string `json:"path"`
		StepID          string `json:"stepId"`
		CaseRunID       string `json:"caseRunId"`
		Status          string `json:"status"`
		RunID           string `json:"runId"`
		ViewerURL       string `json:"viewerUrl"`
		DetailURL       string `json:"detailUrl"`
		ElapsedMs       int64  `json:"elapsedMs"`
		Error           string `json:"error"`
	} `json:"cases"`
}

func waitAPICaseBatchReport(t *testing.T, reportURL string) apiCaseBatchReportForTest {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(reportURL)
		if err != nil {
			t.Fatalf("get batch report: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("batch report status = %d body=%s", resp.StatusCode, raw)
		}
		var report apiCaseBatchReportForTest
		if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
			resp.Body.Close()
			t.Fatalf("decode batch report: %v", err)
		}
		resp.Body.Close()
		if report.Status != store.StatusRunning || time.Now().After(deadline) {
			return report
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServerRejectsAsyncAPICaseBatchWithoutNodes(t *testing.T) {
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, nil))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/cases/batch-runs", "application/json", strings.NewReader(`{"requestId":"change-001"}`))
	if err != nil {
		t.Fatalf("post api case batch run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("api case batch run status = %d body=%s", resp.StatusCode, raw)
	}
}

func loadEmptyProfile(t *testing.T) profile.Bundle {
	t.Helper()
	return profile.EmptyBundle()
}

func writeEmptyProfileBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir empty profile: %v", err)
	}
	raw := `{
  "id": "empty",
  "displayName": "Empty Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write empty profile: %v", err)
	}
	return dir
}

func createLegacyRuntimeDB(t *testing.T, path string) {
	t.Helper()
	statement := `
create table workflow_runs (
  id integer primary key,
  workflow_id text not null,
  status text not null,
  summary_json text not null default '',
  created_at text not null
);
create table interface_node_case_run (
  id integer primary key,
  node_id text not null,
  case_id text not null,
  run_id text not null,
  status text not null,
  failure_kind text not null default '',
  failure_reason text not null default '',
  evidence_path text not null default '',
  elapsed_ms integer not null default 0,
  summary_json text not null default '',
  created_at text not null
);
insert into workflow_runs(id, workflow_id, status, summary_json, created_at)
values (7, 'workflow.alpha', 'passed', '{"steps":1}', '2026-05-14T01:02:03Z');
insert into interface_node_case_run(id, node_id, case_id, run_id, status, evidence_path, summary_json, created_at)
values (11, 'node.alpha', 'case.alpha', 'case-run-parent', 'failed', '.runtime/cases/case-run-parent', '{"failure":"expected"}', '2026-05-14T01:03:03Z');
`
	cmd := exec.Command("sqlite3", path, statement)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create legacy db: %v\n%s", err, out)
	}
}

func decodeJSONResponse(t *testing.T, url string, wantStatus int) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s status = %d body=%s", url, resp.StatusCode, raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode %s: %v body=%s", url, err, raw)
	}
	return payload
}

func postJSONResponse(t *testing.T, url string, body string, wantStatus int) map[string]any {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s status = %d body=%s", url, resp.StatusCode, raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode %s: %v body=%s", url, err, raw)
	}
	return payload
}

func writeAuditSampleProfile(t *testing.T) string {
	t.Helper()
	profileDir := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("create profile dir: %v", err)
	}
	raw := `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}],
  "requestTemplates": [],
  "caseDependencies": [{"id":"dependency.alpha","caseId":"case.alpha","fixtureId":"fixture.missing"}],
  "workflowBindings": [],
  "fixtures": []
}`
	if err := os.WriteFile(filepath.Join(profileDir, "profile.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write audit sample profile: %v", err)
	}
	return profileDir
}

func writeWorkbenchSampleProfile(t *testing.T) string {
	t.Helper()
	profileDir := filepath.Join(t.TempDir(), "profile")
	writeWorkbenchSampleProfileAt(t, profileDir)
	return profileDir
}

func writeWorkbenchSampleProfileAt(t *testing.T, profileDir string) {
	t.Helper()
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("create profile dir: %v", err)
	}
	raw := `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha","kind":"http"}],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha","serviceId":"service.alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.alpha","nodeId":"node.alpha","caseId":"case.alpha","required":true}],
  "fixtures": []
}`
	if err := os.WriteFile(filepath.Join(profileDir, "profile.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write workbench sample profile: %v", err)
	}
}

type failingListRunsStore struct {
	store.Store
}

func (failingListRunsStore) ListRuns(context.Context) ([]store.Run, error) {
	return nil, errors.New("list runs failed")
}

type latestCaseRunCatalogStore struct {
	store.Store
	catalog    store.ProfileCatalog
	latest     []store.APICaseRun
	readModels map[string]store.ReadModel
}

func (s latestCaseRunCatalogStore) GetProfileCatalog(context.Context) (store.ProfileCatalog, error) {
	return s.catalog, nil
}

func (s latestCaseRunCatalogStore) GetReadModel(_ context.Context, _ string, key string) (store.ReadModel, error) {
	if s.readModels != nil {
		if model, ok := s.readModels[key]; ok {
			return model, nil
		}
	}
	return store.ReadModel{}, store.ErrNotFound
}

func (s latestCaseRunCatalogStore) ListRuns(context.Context) ([]store.Run, error) {
	return nil, errors.New("full run scan should not be used")
}

func (s latestCaseRunCatalogStore) ListLatestAPICaseRuns(context.Context) ([]store.APICaseRun, error) {
	return s.latest, nil
}

type interfaceNodeCaseRunCatalogStore struct {
	store.Store
	catalog store.ProfileCatalog
	records []store.APICaseRunRecord
}

func (s interfaceNodeCaseRunCatalogStore) GetProfileCatalog(context.Context) (store.ProfileCatalog, error) {
	return s.catalog, nil
}

func (s interfaceNodeCaseRunCatalogStore) GetReadModel(context.Context, string, string) (store.ReadModel, error) {
	return store.ReadModel{}, store.ErrNotFound
}

func (s interfaceNodeCaseRunCatalogStore) ListRuns(context.Context) ([]store.Run, error) {
	return nil, errors.New("full run scan should not be used")
}

func (s interfaceNodeCaseRunCatalogStore) ListAPICaseRunRecordsForCaseIDs(context.Context, []string) ([]store.APICaseRunRecord, error) {
	return s.records, nil
}

func (s interfaceNodeCaseRunCatalogStore) ListTraceTopologies(context.Context, string) ([]store.TraceTopology, error) {
	return []store.TraceTopology{}, nil
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(raw)
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

func hasJSONCheck(items []any, name string) bool {
	for _, item := range items {
		check, ok := item.(map[string]any)
		if ok && check["name"] == name && check["ok"] == true {
			return true
		}
	}
	return false
}

func hasJSONFailedCheck(items []any, name string) bool {
	for _, item := range items {
		check, ok := item.(map[string]any)
		if ok && check["name"] == name && check["ok"] == false {
			return true
		}
	}
	return false
}

func jsonStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func sqliteCountRows(t *testing.T, dbPath string, table string) int {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, "select count(*) from "+table+";").CombinedOutput()
	if err != nil {
		t.Fatalf("count %s: %v: %s", table, err, out)
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("parse %s count %q: %v", table, out, err)
	}
	return value
}
