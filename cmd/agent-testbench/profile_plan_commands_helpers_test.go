package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	catalogProfilePlanServiceID         = "service.catalog"
	catalogOpenAPIImportEvidenceDir     = ".runtime/openapi"
	catalogHTTPCaptureEvidenceDir       = ".runtime/replay"
	catalogOpenAPIGenerationEvidenceDir = ".runtime/generated"
)

type profileAuditStoreStateFixture struct {
	profileDir string
	storeDSN   string
}

func writeProfileAuditStoreStateFixture(t *testing.T) profileAuditStoreStateFixture {
	t.Helper()

	serverURL := newProfileAuditCaseServer(t)
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	alphaPath := filepath.Join(dir, "case.alpha.json")
	betaPath := filepath.Join(dir, "case.beta.json")
	writeProfileAuditCaseFiles(t, alphaPath, betaPath)
	writeProfileAuditProfile(t, profileDir, alphaPath, betaPath)

	storeDSN := "sqlite://" + filepath.Join(dir, "store.sqlite")
	runCLI(t, "profile", "import", "--from", profileDir, "--store", storeDSN)
	runCLI(t, "case", "run", "--case", alphaPath, "--base-url", serverURL, "--run-id", "run-alpha", "--store", storeDSN, "--profile", "sample")
	return profileAuditStoreStateFixture{profileDir: profileDir, storeDSN: storeDSN}
}

func newProfileAuditCaseServer(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func writeProfileAuditCaseFiles(t *testing.T, alphaPath string, betaPath string) {
	t.Helper()

	writeAPICaseFile(t, alphaPath)
	writeFile(t, betaPath, `{
  "id": "case.beta",
  "title": "Read Item",
  "request": {"method": "GET", "path": "/v1/items/item-001"},
  "assertions": {"expectedStatusCodes": [200]}
}`)
}

func writeProfileAuditProfile(t *testing.T, profileDir string, alphaPath string, betaPath string) {
	t.Helper()

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
}

type profileAuditReport struct {
	OK         bool                `json:"ok"`
	IssueCount int                 `json:"issueCount"`
	Issues     []profileAuditIssue `json:"issues"`
	Store      *profileAuditStore  `json:"store,omitempty"`
}

type profileAuditIssue struct {
	Code      string `json:"code"`
	SubjectID string `json:"subjectId"`
}

type profileAuditStore struct {
	ProfileIndexed bool                        `json:"profileIndexed"`
	DigestMatches  bool                        `json:"digestMatches"`
	APICases       []profileAuditAPICaseStatus `json:"apiCases"`
}

type profileAuditAPICaseStatus struct {
	CaseID       string `json:"caseId"`
	HasPassed    bool   `json:"hasPassed"`
	LatestStatus string `json:"latestStatus"`
}

func runProfileAuditJSON(t *testing.T, fixture profileAuditStoreStateFixture) profileAuditReport {
	t.Helper()

	out := runCLI(t, "profile", "audit", "--profile", fixture.profileDir, "--offline-template-package", "--store", fixture.storeDSN, "--json")
	var report profileAuditReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit json: %v\n%s", err, out)
	}
	return report
}

func requireProfileAuditIssues(t *testing.T, report profileAuditReport) {
	t.Helper()

	if report.OK || report.IssueCount != 1 || len(report.Issues) != 1 {
		t.Fatalf("audit report issues = %#v", report)
	}
	if report.Issues[0].Code != "case-dependency-fixture-missing" || report.Issues[0].SubjectID != "dependency.beta" {
		t.Fatalf("audit issue = %#v", report.Issues[0])
	}
}

func requireProfileAuditStoreState(t *testing.T, report profileAuditReport) {
	t.Helper()

	if report.Store == nil || !report.Store.ProfileIndexed || !report.Store.DigestMatches {
		t.Fatalf("audit store state = %#v", report.Store)
	}
	caseState := map[string]profileAuditAPICaseStatus{}
	for _, item := range report.Store.APICases {
		caseState[item.CaseID] = item
	}
	if !caseState["case.alpha"].HasPassed || caseState["case.alpha"].LatestStatus != "passed" {
		t.Fatalf("case.alpha state = %#v", caseState["case.alpha"])
	}
	if caseState["case.beta"].HasPassed || caseState["case.beta"].LatestStatus != "" {
		t.Fatalf("case.beta state = %#v", caseState["case.beta"])
	}
}

type catalogImportPlanReport struct {
	Kind       string            `json:"kind"`
	SourcePath string            `json:"sourcePath"`
	Plan       catalogImportPlan `json:"plan"`
}

type catalogImportPlan struct {
	Service        catalogImportPlanService         `json:"service"`
	InterfaceNodes []catalogImportPlanInterfaceNode `json:"interfaceNodes"`
	APICases       []catalogImportPlanAPICase       `json:"apiCases"`
	CaseFiles      []catalogImportPlanCaseFile      `json:"caseFiles"`
	WrittenFiles   []string                         `json:"writtenFiles"`
}

type catalogImportPlanService struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
}

type catalogImportPlanInterfaceNode struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Status string `json:"status"`
}

type catalogImportPlanAPICase struct {
	ID          string   `json:"id"`
	CasePath    string   `json:"casePath"`
	Status      string   `json:"status"`
	EvidenceDir string   `json:"evidenceDir"`
	Tags        []string `json:"tags"`
}

type catalogImportPlanCaseFile struct {
	Path string          `json:"path"`
	Body json.RawMessage `json:"body"`
}

type catalogHTTPCaptureImportPlanFixture struct {
	capturePath string
}

func writeCatalogHTTPCaptureImportPlanFixture(t *testing.T) catalogHTTPCaptureImportPlanFixture {
	t.Helper()

	capturePath := filepath.Join(t.TempDir(), "traffic.json")
	writeFile(t, capturePath, `{
  "name": "Catalog Traffic",
  "captures": [{
    "id": "createItem",
    "name": "Create item from traffic",
    "request": {"method": "POST", "path": "/items", "headers": {"Content-Type": "application/json"}, "body": {"id": "item-001", "name": "Example"}},
    "response": {"status": 201, "body": {"id": "item-001"}}
  }]
}`)
	return catalogHTTPCaptureImportPlanFixture{capturePath: capturePath}
}

func runCatalogHTTPCaptureImportPlanJSON(t *testing.T, fixture catalogHTTPCaptureImportPlanFixture) catalogImportPlanReport {
	t.Helper()

	out := runCLI(t, "profile", "import-plan", "http-capture", "--from", fixture.capturePath, "--service-id", catalogProfilePlanServiceID, "--evidence-dir", catalogHTTPCaptureEvidenceDir, "--json")
	var report catalogImportPlanReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode http capture import plan json: %v\n%s", err, out)
	}
	return report
}

func requireCatalogHTTPCaptureImportPlanSummary(t *testing.T, fixture catalogHTTPCaptureImportPlanFixture, report catalogImportPlanReport) {
	t.Helper()

	if report.Kind != "http-capture" || report.SourcePath != fixture.capturePath || report.Plan.Service.ID != catalogProfilePlanServiceID || report.Plan.Service.Status != "draft" {
		t.Fatalf("http capture plan summary = %#v", report)
	}
	if len(report.Plan.InterfaceNodes) != 1 || len(report.Plan.APICases) != 1 || len(report.Plan.CaseFiles) != 1 {
		t.Fatalf("http capture plan counts = nodes:%d cases:%d files:%d", len(report.Plan.InterfaceNodes), len(report.Plan.APICases), len(report.Plan.CaseFiles))
	}
	node := report.Plan.InterfaceNodes[0]
	if node.ID != "node.service.catalog.create-item" || node.Method != "POST" || node.Path != "/items" {
		t.Fatalf("http capture node = %#v", node)
	}
	apiCase := report.Plan.APICases[0]
	if apiCase.ID != "case.service.catalog.create-item" || apiCase.CasePath != "api-cases/case.service.catalog.create-item.json" || apiCase.EvidenceDir != catalogHTTPCaptureEvidenceDir || strings.Join(apiCase.Tags, ",") != "recorded,replay" {
		t.Fatalf("http capture case = %#v", apiCase)
	}
}

func requireCatalogHTTPCaptureImportPlanOutput(t *testing.T, fixture catalogHTTPCaptureImportPlanFixture) {
	t.Helper()

	outputDir := filepath.Join(t.TempDir(), "capture-plan")
	textOut := runCLI(t, "profile", "import-plan", "http-capture", "--from", fixture.capturePath, "--service-id", catalogProfilePlanServiceID, "--output-dir", outputDir)
	requireProfilePlanTextContains(t, "http capture text", textOut,
		"HTTP Capture Import Plan",
		"Source: "+fixture.capturePath,
		"Output Dir: "+outputDir,
		"API Cases: 1",
	)
	requireProfilePlanOutputFiles(t, outputDir,
		"import-plan.json",
		filepath.Join("services", "service.catalog.json"),
		filepath.Join("interface-nodes", "node.service.catalog.create-item.json"),
		filepath.Join("cases", "case.service.catalog.create-item.json"),
		filepath.Join("api-cases", "case.service.catalog.create-item.json"),
	)
}

type catalogOpenAPIGenerationPlanFixture struct {
	specPath string
}

func writeCatalogOpenAPIGenerationPlanFixture(t *testing.T) catalogOpenAPIGenerationPlanFixture {
	t.Helper()

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
        "requestBody": {"content": {"application/json": {"schema": {
          "type": "object",
          "required": ["id"],
          "properties": {
            "id": {"type": "string", "example": "item-001"},
            "name": {"type": "string", "example": "Example Item"}
          }
        }}}},
        "responses": {"201": {"description": "Created"}, "400": {"description": "Bad request"}}
      }
    }
  }
}`)
	return catalogOpenAPIGenerationPlanFixture{specPath: specPath}
}

type catalogOpenAPIGenerationPlanReport struct {
	Kind       string                       `json:"kind"`
	SourcePath string                       `json:"sourcePath"`
	Plan       catalogOpenAPIGenerationPlan `json:"plan"`
}

type catalogOpenAPIGenerationPlan struct {
	OK         bool                                `json:"ok"`
	Candidates []catalogOpenAPIGenerationCandidate `json:"candidates"`
	APICases   []catalogOpenAPIGenerationAPICase   `json:"apiCases"`
	CaseFiles  []catalogImportPlanCaseFile         `json:"caseFiles"`
}

type catalogOpenAPIGenerationCandidate struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Field  string `json:"field"`
	CaseID string `json:"caseId"`
}

type catalogOpenAPIGenerationAPICase struct {
	ID       string   `json:"id"`
	Status   string   `json:"status"`
	CasePath string   `json:"casePath"`
	Tags     []string `json:"tags"`
}

func runCatalogOpenAPIGenerationPlanJSON(t *testing.T, fixture catalogOpenAPIGenerationPlanFixture) catalogOpenAPIGenerationPlanReport {
	t.Helper()

	out := runCLI(t, "profile", "generation-plan", "openapi", "--from", fixture.specPath, "--service-id", catalogProfilePlanServiceID, "--evidence-dir", catalogOpenAPIGenerationEvidenceDir, "--json")
	var report catalogOpenAPIGenerationPlanReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode generation plan json: %v\n%s", err, out)
	}
	return report
}

func requireCatalogOpenAPIGenerationPlanSummary(t *testing.T, fixture catalogOpenAPIGenerationPlanFixture, report catalogOpenAPIGenerationPlanReport) {
	t.Helper()

	if report.Kind != "openapi" || report.SourcePath != fixture.specPath || !report.Plan.OK || len(report.Plan.Candidates) != 1 || len(report.Plan.APICases) != 1 {
		t.Fatalf("generation plan summary = %#v", report)
	}
	candidate := report.Plan.Candidates[0]
	if candidate.Kind != "missing-required-field" || candidate.Field != "id" || candidate.CaseID != "case.service.catalog.create-item.missing-id" {
		t.Fatalf("generation candidate = %#v", candidate)
	}
	apiCase := report.Plan.APICases[0]
	if apiCase.Status != "draft" || strings.Join(apiCase.Tags, ",") != "generated,schema,negative,catalog" {
		t.Fatalf("generated api case = %#v", apiCase)
	}
}

func requireCatalogOpenAPIGenerationPlanOutput(t *testing.T, fixture catalogOpenAPIGenerationPlanFixture) {
	t.Helper()

	outputDir := filepath.Join(t.TempDir(), "generation-plan")
	textOut := runCLI(t, "profile", "generation-plan", "openapi", "--from", fixture.specPath, "--service-id", catalogProfilePlanServiceID, "--output-dir", outputDir)
	requireProfilePlanTextContains(t, "generation plan text", textOut,
		"OpenAPI Generation Plan",
		"Source: "+fixture.specPath,
		"Candidates: 1",
		"Output Dir: "+outputDir,
	)
	requireProfilePlanOutputFiles(t, outputDir,
		"generation-plan.json",
		filepath.Join("cases", "case.service.catalog.create-item.missing-id.json"),
		filepath.Join("api-cases", "case.service.catalog.create-item.missing-id.json"),
	)
}

func requireProfilePlanTextContains(t *testing.T, label string, text string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("%s missing %q:\n%s", label, want, text)
		}
	}
}

func requireProfilePlanOutputFiles(t *testing.T, outputDir string, paths ...string) {
	t.Helper()

	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(outputDir, path)); err != nil {
			t.Fatalf("expected profile plan output %s: %v", path, err)
		}
	}
}
