package main

import (
	"context"
	"encoding/json"
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
	fixture := writeProfileAuditStoreStateFixture(t)

	report := runProfileAuditJSON(t, fixture)
	requireProfileAuditIssues(t, report)
	requireProfileAuditStoreState(t, report)
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
      "get": {"operationId": "listItems", "summary": "List items", "tags": ["catalog"], "responses": {"200": {"description": "OK"}}},
      "post": {
        "operationId": "createItem",
        "summary": "Create item",
        "tags": ["catalog", "write"],
        "requestBody": {"content": {"application/json": {"example": {"id": "item-001", "name": "Example Item"}}}},
        "responses": {"201": {"description": "Created"}}
      }
    }
  }
}`)
	return catalogOpenAPIImportPlanFixture{specPath: specPath}
}

func runCatalogOpenAPIImportPlanJSON(t *testing.T, fixture catalogOpenAPIImportPlanFixture) catalogImportPlanReport {
	t.Helper()

	out := runCLI(t, "profile", "import-plan", "openapi", "--from", fixture.specPath, "--service-id", catalogProfilePlanServiceID, "--evidence-dir", catalogOpenAPIImportEvidenceDir, "--json")
	var report catalogImportPlanReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile import plan json: %v\n%s", err, out)
	}
	return report
}

func requireCatalogOpenAPIImportPlanSummary(t *testing.T, fixture catalogOpenAPIImportPlanFixture, report catalogImportPlanReport) {
	t.Helper()

	if report.Kind != "openapi" || report.SourcePath != fixture.specPath || report.Plan.Service.ID != catalogProfilePlanServiceID || report.Plan.Service.Status != "draft" {
		t.Fatalf("import plan summary = %#v", report)
	}
	if len(report.Plan.InterfaceNodes) != 2 || len(report.Plan.APICases) != 2 || len(report.Plan.CaseFiles) != 2 {
		t.Fatalf("import plan counts = nodes:%d cases:%d files:%d", len(report.Plan.InterfaceNodes), len(report.Plan.APICases), len(report.Plan.CaseFiles))
	}
	firstNode := report.Plan.InterfaceNodes[0]
	if firstNode.ID != "node.service.catalog.list-items" || firstNode.Method != "GET" || firstNode.Path != "/items" || firstNode.Status != "draft" {
		t.Fatalf("first interface node = %#v", firstNode)
	}
	secondCase := report.Plan.APICases[1]
	if secondCase.ID != "case.service.catalog.create-item" || secondCase.CasePath != "api-cases/case.service.catalog.create-item.json" || secondCase.EvidenceDir != catalogOpenAPIImportEvidenceDir || strings.Join(secondCase.Tags, ",") != "openapi,catalog,write" {
		t.Fatalf("second api case = %#v", secondCase)
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

	textOut := runCLI(t, "profile", "import-plan", "openapi", "--from", fixture.specPath, "--service-id", catalogProfilePlanServiceID)
	requireProfilePlanTextContains(t, "import plan text", textOut,
		"OpenAPI Import Plan",
		"Source: "+fixture.specPath,
		"Service: service.catalog",
		"Interface Nodes: 2",
		"API Cases: 2",
		"Case Files: 2",
	)
}

func requireCatalogOpenAPIImportWrittenFiles(t *testing.T, fixture catalogOpenAPIImportPlanFixture) {
	t.Helper()

	outputDir := filepath.Join(t.TempDir(), "review-plan")
	textOut := runCLI(t, "profile", "import-plan", "openapi", "--from", fixture.specPath, "--service-id", catalogProfilePlanServiceID, "--evidence-dir", catalogOpenAPIImportEvidenceDir, "--output-dir", outputDir)
	requireProfilePlanTextContains(t, "import plan output-dir text", textOut, "Output Dir: "+outputDir)
	requireProfilePlanOutputFiles(t, outputDir,
		"import-plan.json",
		filepath.Join("services", "service.catalog.json"),
		filepath.Join("interface-nodes", "node.service.catalog.list-items.json"),
		filepath.Join("request-templates", "template.service.catalog.create-item.json"),
		filepath.Join("cases", "case.service.catalog.create-item.json"),
		filepath.Join("api-cases", "case.service.catalog.create-item.json"),
	)

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
	fixture := writeCatalogHTTPCaptureImportPlanFixture(t)

	report := runCatalogHTTPCaptureImportPlanJSON(t, fixture)
	requireCatalogHTTPCaptureImportPlanSummary(t, fixture, report)
	requireCatalogHTTPCaptureImportPlanOutput(t, fixture)
}

func TestProfileGenerationPlanOpenAPICommand(t *testing.T) {
	fixture := writeCatalogOpenAPIGenerationPlanFixture(t)

	report := runCatalogOpenAPIGenerationPlanJSON(t, fixture)
	requireCatalogOpenAPIGenerationPlanSummary(t, fixture, report)
	requireCatalogOpenAPIGenerationPlanOutput(t, fixture)
}
