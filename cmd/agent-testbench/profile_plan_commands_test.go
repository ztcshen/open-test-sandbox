package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestProfileImportCommandCanAuditImportedProfile(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
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
}`)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+storePath, "--json", "--audit")

	var report struct {
		ProfileID string `json:"profileId"`
		Audit     *struct {
			OK         bool `json:"ok"`
			IssueCount int  `json:"issueCount"`
			Issues     []struct {
				Code      string `json:"code"`
				SubjectID string `json:"subjectId"`
			} `json:"issues"`
			Store *struct {
				ProfileIndexed bool `json:"profileIndexed"`
				DigestMatches  bool `json:"digestMatches"`
			} `json:"store,omitempty"`
		} `json:"audit,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode audited profile import json: %v\n%s", err, out)
	}
	if report.ProfileID != "sample" || report.Audit == nil {
		t.Fatalf("report missing audit = %#v", report)
	}
	if report.Audit.OK || report.Audit.IssueCount != 2 || len(report.Audit.Issues) != 2 {
		t.Fatalf("audit summary = %#v", report.Audit)
	}
	if report.Audit.Issues[0].Code != "api-case-node-missing" || report.Audit.Issues[1].Code != "case-dependency-fixture-missing" {
		t.Fatalf("audit issues = %#v", report.Audit.Issues)
	}
	if report.Audit.Store == nil || !report.Audit.Store.ProfileIndexed || !report.Audit.Store.DigestMatches {
		t.Fatalf("audit store = %#v", report.Audit.Store)
	}

	text := runCLI(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+storePath, "--audit")
	for _, want := range []string{"Imported profile: sample", "Audit OK: false", "Audit Issues: 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("audited text import output missing %q: %q", want, text)
		}
	}
}

func TestProfileImportCommandCanRequireCleanAuditBeforeWritingStore(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.missing"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLIFails(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+storePath, "--require-audit-ok")
	if !strings.Contains(out, "profile audit failed") || !strings.Contains(out, "api-case-node-missing") {
		t.Fatalf("strict profile import output = %q", out)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if _, err := s.GetProfileIndex(context.Background(), "sample"); err == nil {
		t.Fatalf("strict profile import wrote profile index")
	} else if err != store.ErrNotFound {
		t.Fatalf("get profile index after strict failure: %v", err)
	}
}

func TestProfileAuditCommandEmitsJSONWithStoreState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	alphaPath := filepath.Join(dir, "case.alpha.json")
	betaPath := filepath.Join(dir, "case.beta.json")
	writeAPICaseFile(t, alphaPath)
	writeFile(t, betaPath, `{
  "id": "case.beta",
  "title": "Read Item",
  "request": {"method": "GET", "path": "/v1/items/item-001"},
  "assertions": {"expectedStatusCodes": [200]}
}`)
	writeFile(t, filepath.Join(profileDir, "profile.json"), fmt.Sprintf(`{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha","casePath":%q},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.alpha","casePath":%q}
  ],
  "requestTemplates": [{"id":"template.alpha","nodeId":"node.alpha","method":"POST","path":"/v1/items"}],
  "caseDependencies": [{"id":"dependency.beta","caseId":"case.beta","fixtureId":"fixture.missing"}],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.beta","required":true}],
  "fixtures": []
}`, alphaPath, betaPath))

	storePath := filepath.Join(dir, "store.sqlite")
	runCLI(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+storePath)
	runCLI(t, "case", "run", "--case", alphaPath, "--base-url", server.URL, "--run-id", "run-alpha", "--store", "sqlite://"+storePath, "--profile", "sample")

	out := runCLI(t, "profile", "audit", "--profile", profileDir, "--offline-template-package", "--store", "sqlite://"+storePath, "--json")

	var report struct {
		OK         bool `json:"ok"`
		IssueCount int  `json:"issueCount"`
		Issues     []struct {
			Code      string `json:"code"`
			SubjectID string `json:"subjectId"`
		} `json:"issues"`
		Store *struct {
			ProfileIndexed bool `json:"profileIndexed"`
			DigestMatches  bool `json:"digestMatches"`
			APICases       []struct {
				CaseID       string `json:"caseId"`
				HasPassed    bool   `json:"hasPassed"`
				LatestStatus string `json:"latestStatus"`
			} `json:"apiCases"`
		} `json:"store,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit json: %v\n%s", err, out)
	}
	if report.OK || report.IssueCount != 1 || len(report.Issues) != 1 {
		t.Fatalf("audit report issues = %#v", report)
	}
	if report.Issues[0].Code != "case-dependency-fixture-missing" || report.Issues[0].SubjectID != "dependency.beta" {
		t.Fatalf("audit issue = %#v", report.Issues[0])
	}
	if report.Store == nil || !report.Store.ProfileIndexed || !report.Store.DigestMatches {
		t.Fatalf("audit store state = %#v", report.Store)
	}
	caseState := map[string]struct {
		HasPassed    bool
		LatestStatus string
	}{}
	for _, item := range report.Store.APICases {
		caseState[item.CaseID] = struct {
			HasPassed    bool
			LatestStatus string
		}{HasPassed: item.HasPassed, LatestStatus: item.LatestStatus}
	}
	if !caseState["case.alpha"].HasPassed || caseState["case.alpha"].LatestStatus != "passed" {
		t.Fatalf("case.alpha state = %#v", caseState["case.alpha"])
	}
	if caseState["case.beta"].HasPassed || caseState["case.beta"].LatestStatus != "" {
		t.Fatalf("case.beta state = %#v", caseState["case.beta"])
	}
}

func TestProfileAuditPlanCommandSuggestsRepairActions(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.missing"}],
  "requestTemplates": [],
  "caseDependencies": [{"id":"dependency.alpha","caseId":"case.alpha","fixtureId":""}],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"","nodeId":"node.alpha","caseId":"case.alpha","required":true}],
  "fixtures": [{"id":"fixture.bad","kind":"json","dataJson":"{\"broken\":"}]
}`)

	out := runCLI(t, "profile", "audit-plan", "--profile", profileDir, "--offline-template-package", "--json")
	var report struct {
		OK          bool   `json:"ok"`
		ProfileID   string `json:"profileId"`
		IssueCount  int    `json:"issueCount"`
		ActionCount int    `json:"actionCount"`
		Counts      struct {
			UpdateReferenceOrAddAsset int `json:"updateReferenceOrAddAsset"`
			FillRequiredField         int `json:"fillRequiredField"`
			FixInvalidJSON            int `json:"fixInvalidJson"`
		} `json:"counts"`
		Actions []struct {
			Type            string   `json:"type"`
			IssueCode       string   `json:"issueCode"`
			SubjectID       string   `json:"subjectId"`
			Field           string   `json:"field"`
			SuggestedChange string   `json:"suggestedChange"`
			Command         []string `json:"command"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit plan json: %v\n%s", err, out)
	}
	if !report.OK || report.ProfileID != "sample" || report.IssueCount != 4 || report.ActionCount != 4 {
		t.Fatalf("audit plan summary = %#v", report)
	}
	if report.Counts.UpdateReferenceOrAddAsset != 1 || report.Counts.FillRequiredField != 2 || report.Counts.FixInvalidJSON != 1 {
		t.Fatalf("audit plan counts = %#v", report.Counts)
	}
	if len(report.Actions) != 4 || report.Actions[0].Type != "update-reference-or-add-asset" || report.Actions[0].IssueCode != "api-case-node-missing" || report.Actions[0].SubjectID != "case.alpha" || report.Actions[0].Field != "nodeId" {
		t.Fatalf("audit plan actions = %#v", report.Actions)
	}
	if !strings.Contains(report.Actions[0].SuggestedChange, "Create the missing interface node") || strings.Join(report.Actions[0].Command, " ") != "profile audit --json" {
		t.Fatalf("audit plan first action = %#v", report.Actions[0])
	}

	textOut := runCLI(t, "profile", "audit-plan", "--profile", profileDir, "--offline-template-package")
	for _, want := range []string{"Profile Audit Repair Plan: sample", "Actions: 4", "update-reference-or-add-asset", "api-case-node-missing", "fix-invalid-json"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("audit plan text missing %q:\n%s", want, textOut)
		}
	}
}

const (
	catalogOpenAPIImportServiceID   = "service.catalog"
	catalogOpenAPIImportEvidenceDir = ".runtime/openapi"
)

func TestProfileImportPlanOpenAPICommand(t *testing.T) {
	fixture := writeCatalogOpenAPIImportPlanFixture(t)

	report := runCatalogOpenAPIImportPlanJSON(t, fixture)
	requireCatalogOpenAPIImportPlanSummary(t, fixture, report)
	requireCatalogOpenAPIImportRunnableCase(t, report.Plan.CaseFiles[1].Body)
	requireCatalogOpenAPIImportTextOutput(t, fixture)
	requireCatalogOpenAPIImportWrittenFiles(t, fixture)
}

type catalogOpenAPIImportPlanFixture struct {
	specPath string
}

func writeCatalogOpenAPIImportPlanFixture(t *testing.T) catalogOpenAPIImportPlanFixture {
	t.Helper()

	specPath := filepath.Join(t.TempDir(), "catalog-openapi.json")
	writeFile(t, specPath, `{
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
}`)

	return catalogOpenAPIImportPlanFixture{specPath: specPath}
}

type catalogOpenAPIImportPlanReport struct {
	Kind       string `json:"kind"`
	SourcePath string `json:"sourcePath"`
	Plan       struct {
		Service struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Status      string `json:"status"`
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
			Status      string   `json:"status"`
			EvidenceDir string   `json:"evidenceDir"`
			Tags        []string `json:"tags"`
		} `json:"apiCases"`
		CaseFiles []struct {
			Path string          `json:"path"`
			Body json.RawMessage `json:"body"`
		} `json:"caseFiles"`
		WrittenFiles []string `json:"writtenFiles"`
	} `json:"plan"`
}

func runCatalogOpenAPIImportPlanJSON(t *testing.T, fixture catalogOpenAPIImportPlanFixture) catalogOpenAPIImportPlanReport {
	t.Helper()

	out := runCLI(t, "profile", "import-plan", "openapi", "--from", fixture.specPath, "--service-id", catalogOpenAPIImportServiceID, "--evidence-dir", catalogOpenAPIImportEvidenceDir, "--json")
	var report catalogOpenAPIImportPlanReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile import plan json: %v\n%s", err, out)
	}
	return report
}

func requireCatalogOpenAPIImportPlanSummary(t *testing.T, fixture catalogOpenAPIImportPlanFixture, report catalogOpenAPIImportPlanReport) {
	t.Helper()

	if report.Kind != "openapi" || report.SourcePath != fixture.specPath || report.Plan.Service.ID != catalogOpenAPIImportServiceID || report.Plan.Service.Status != "draft" {
		t.Fatalf("import plan summary = %#v", report)
	}
	if len(report.Plan.InterfaceNodes) != 2 || len(report.Plan.APICases) != 2 || len(report.Plan.CaseFiles) != 2 {
		t.Fatalf("import plan counts = nodes:%d cases:%d files:%d", len(report.Plan.InterfaceNodes), len(report.Plan.APICases), len(report.Plan.CaseFiles))
	}
	if report.Plan.InterfaceNodes[0].ID != "node.service.catalog.list-items" || report.Plan.InterfaceNodes[0].Method != "GET" || report.Plan.InterfaceNodes[0].Path != "/items" || report.Plan.InterfaceNodes[0].Status != "draft" {
		t.Fatalf("first interface node = %#v", report.Plan.InterfaceNodes[0])
	}
	if report.Plan.APICases[1].ID != "case.service.catalog.create-item" || report.Plan.APICases[1].CasePath != "api-cases/case.service.catalog.create-item.json" || report.Plan.APICases[1].EvidenceDir != catalogOpenAPIImportEvidenceDir || strings.Join(report.Plan.APICases[1].Tags, ",") != "openapi,catalog,write" {
		t.Fatalf("second api case = %#v", report.Plan.APICases[1])
	}
}

type catalogOpenAPIImportRunnableCase struct {
	Request struct {
		Method string         `json:"method"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	} `json:"request"`
	Assertions struct {
		ExpectedStatusCodes []int `json:"expectedStatusCodes"`
	} `json:"assertions"`
}

func requireCatalogOpenAPIImportRunnableCase(t *testing.T, body json.RawMessage) {
	t.Helper()

	var runnable catalogOpenAPIImportRunnableCase
	if err := json.Unmarshal(body, &runnable); err != nil {
		t.Fatalf("decode generated case body: %v\n%s", err, string(body))
	}
	if runnable.Request.Method != "POST" || runnable.Request.Path != "/items" || runnable.Request.Body["id"] != "item-001" || len(runnable.Assertions.ExpectedStatusCodes) != 1 || runnable.Assertions.ExpectedStatusCodes[0] != 201 {
		t.Fatalf("generated runnable case = %#v", runnable)
	}
}

func requireCatalogOpenAPIImportTextOutput(t *testing.T, fixture catalogOpenAPIImportPlanFixture) {
	t.Helper()

	textOut := runCLI(t, "profile", "import-plan", "openapi", "--from", fixture.specPath, "--service-id", catalogOpenAPIImportServiceID)
	for _, want := range []string{"OpenAPI Import Plan", "Source: " + fixture.specPath, "Service: service.catalog", "Interface Nodes: 2", "API Cases: 2", "Case Files: 2"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("import plan text missing %q:\n%s", want, textOut)
		}
	}
}

func requireCatalogOpenAPIImportWrittenFiles(t *testing.T, fixture catalogOpenAPIImportPlanFixture) {
	t.Helper()

	outputDir := filepath.Join(t.TempDir(), "review-plan")
	textOut := runCLI(t, "profile", "import-plan", "openapi", "--from", fixture.specPath, "--service-id", catalogOpenAPIImportServiceID, "--evidence-dir", catalogOpenAPIImportEvidenceDir, "--output-dir", outputDir)
	if !strings.Contains(textOut, "Output Dir: "+outputDir) {
		t.Fatalf("import plan output-dir text = %q", textOut)
	}
	for _, path := range []string{
		"import-plan.json",
		filepath.Join("services", "service.catalog.json"),
		filepath.Join("interface-nodes", "node.service.catalog.list-items.json"),
		filepath.Join("request-templates", "template.service.catalog.create-item.json"),
		filepath.Join("cases", "case.service.catalog.create-item.json"),
		filepath.Join("api-cases", "case.service.catalog.create-item.json"),
	} {
		if _, err := os.Stat(filepath.Join(outputDir, path)); err != nil {
			t.Fatalf("expected import plan output %s: %v", path, err)
		}
	}
	var metadataCase struct {
		ID       string `json:"id"`
		CasePath string `json:"casePath"`
		Status   string `json:"status"`
	}
	readTestJSONFile(t, filepath.Join(outputDir, "cases", "case.service.catalog.create-item.json"), &metadataCase)
	if metadataCase.ID != "case.service.catalog.create-item" || metadataCase.CasePath != "api-cases/case.service.catalog.create-item.json" || metadataCase.Status != "draft" {
		t.Fatalf("written metadata case = %#v", metadataCase)
	}
	var runnable catalogOpenAPIImportRunnableCase
	readTestJSONFile(t, filepath.Join(outputDir, "api-cases", "case.service.catalog.create-item.json"), &runnable)
	if runnable.Request.Method != "POST" || runnable.Request.Path != "/items" || runnable.Request.Body["id"] != "item-001" {
		t.Fatalf("written runnable case = %#v", runnable)
	}
}

func TestProfileImportPlanHTTPCaptureCommand(t *testing.T) {
	capturePath := filepath.Join(t.TempDir(), "traffic.json")
	writeFile(t, capturePath, `{
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
}`)

	out := runCLI(t, "profile", "import-plan", "http-capture", "--from", capturePath, "--service-id", "service.catalog", "--evidence-dir", ".runtime/replay", "--json")
	var report struct {
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
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode http capture import plan json: %v\n%s", err, out)
	}
	if report.Kind != "http-capture" || report.SourcePath != capturePath || report.Plan.Service.ID != "service.catalog" || report.Plan.Service.Status != "draft" {
		t.Fatalf("http capture plan summary = %#v", report)
	}
	if len(report.Plan.InterfaceNodes) != 1 || len(report.Plan.APICases) != 1 || len(report.Plan.CaseFiles) != 1 {
		t.Fatalf("http capture plan counts = nodes:%d cases:%d files:%d", len(report.Plan.InterfaceNodes), len(report.Plan.APICases), len(report.Plan.CaseFiles))
	}
	if report.Plan.InterfaceNodes[0].ID != "node.service.catalog.create-item" || report.Plan.InterfaceNodes[0].Method != "POST" || report.Plan.InterfaceNodes[0].Path != "/items" {
		t.Fatalf("http capture node = %#v", report.Plan.InterfaceNodes[0])
	}
	if report.Plan.APICases[0].ID != "case.service.catalog.create-item" || report.Plan.APICases[0].CasePath != "api-cases/case.service.catalog.create-item.json" || report.Plan.APICases[0].EvidenceDir != ".runtime/replay" || strings.Join(report.Plan.APICases[0].Tags, ",") != "recorded,replay" {
		t.Fatalf("http capture case = %#v", report.Plan.APICases[0])
	}

	outputDir := filepath.Join(t.TempDir(), "capture-plan")
	textOut := runCLI(t, "profile", "import-plan", "http-capture", "--from", capturePath, "--service-id", "service.catalog", "--output-dir", outputDir)
	for _, want := range []string{"HTTP Capture Import Plan", "Source: " + capturePath, "Output Dir: " + outputDir, "API Cases: 1"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("http capture text missing %q:\n%s", want, textOut)
		}
	}
	for _, path := range []string{
		"import-plan.json",
		filepath.Join("services", "service.catalog.json"),
		filepath.Join("interface-nodes", "node.service.catalog.create-item.json"),
		filepath.Join("cases", "case.service.catalog.create-item.json"),
		filepath.Join("api-cases", "case.service.catalog.create-item.json"),
	} {
		if _, err := os.Stat(filepath.Join(outputDir, path)); err != nil {
			t.Fatalf("expected http capture output %s: %v", path, err)
		}
	}
}

func TestProfileGenerationPlanOpenAPICommand(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "catalog-openapi.json")
	writeFile(t, specPath, `{
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
}`)

	out := runCLI(t, "profile", "generation-plan", "openapi", "--from", specPath, "--service-id", "service.catalog", "--evidence-dir", ".runtime/generated", "--json")
	var report struct {
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
				ID       string   `json:"id"`
				Status   string   `json:"status"`
				CasePath string   `json:"casePath"`
				Tags     []string `json:"tags"`
			} `json:"apiCases"`
			CaseFiles []struct {
				Path string          `json:"path"`
				Body json.RawMessage `json:"body"`
			} `json:"caseFiles"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode generation plan json: %v\n%s", err, out)
	}
	if report.Kind != "openapi" || report.SourcePath != specPath || !report.Plan.OK || len(report.Plan.Candidates) != 1 || len(report.Plan.APICases) != 1 {
		t.Fatalf("generation plan summary = %#v", report)
	}
	if report.Plan.Candidates[0].Kind != "missing-required-field" || report.Plan.Candidates[0].Field != "id" || report.Plan.Candidates[0].CaseID != "case.service.catalog.create-item.missing-id" {
		t.Fatalf("generation candidate = %#v", report.Plan.Candidates[0])
	}
	if report.Plan.APICases[0].Status != "draft" || strings.Join(report.Plan.APICases[0].Tags, ",") != "generated,schema,negative,catalog" {
		t.Fatalf("generated api case = %#v", report.Plan.APICases[0])
	}

	outputDir := filepath.Join(t.TempDir(), "generation-plan")
	textOut := runCLI(t, "profile", "generation-plan", "openapi", "--from", specPath, "--service-id", "service.catalog", "--output-dir", outputDir)
	for _, want := range []string{"OpenAPI Generation Plan", "Source: " + specPath, "Candidates: 1", "Output Dir: " + outputDir} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("generation plan text missing %q:\n%s", want, textOut)
		}
	}
	for _, path := range []string{
		"generation-plan.json",
		filepath.Join("cases", "case.service.catalog.create-item.missing-id.json"),
		filepath.Join("api-cases", "case.service.catalog.create-item.missing-id.json"),
	} {
		if _, err := os.Stat(filepath.Join(outputDir, path)); err != nil {
			t.Fatalf("expected generation plan output %s: %v", path, err)
		}
	}
}
