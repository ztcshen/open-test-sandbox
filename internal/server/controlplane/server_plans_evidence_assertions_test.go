package controlplane_test

import (
	"context"
	"encoding/json"
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

type openAPIImportPlanPayload struct {
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
		} `json:"apiCases"`
		CaseFiles []struct {
			Path string          `json:"path"`
			Body json.RawMessage `json:"body"`
		} `json:"caseFiles"`
	} `json:"plan"`
}

type openAPIGenerationPlanPayload struct {
	OK         bool   `json:"ok"`
	Kind       string `json:"kind"`
	SourcePath string `json:"sourcePath"`
	Plan       struct {
		OK         bool `json:"ok"`
		Candidates []struct {
			Kind   string `json:"kind"`
			Field  string `json:"field"`
			CaseID string `json:"caseId"`
		} `json:"candidates"`
		APICases []struct {
			Status      string   `json:"status"`
			EvidenceDir string   `json:"evidenceDir"`
			Tags        []string `json:"tags"`
		} `json:"apiCases"`
		CaseFiles []struct {
			Path string          `json:"path"`
			Body json.RawMessage `json:"body"`
		} `json:"caseFiles"`
	} `json:"plan"`
}

func writeOpenAPIImportPlanSpec(t *testing.T) string {
	t.Helper()
	specPath := filepath.Join(t.TempDir(), "catalog-openapi.json")
	raw := `{"openapi":"3.0.3","info":{"title":"Catalog API"},"paths":{"/items":{"get":{"operationId":"listItems","summary":"List items","tags":["catalog"],"responses":{"200":{"description":"OK"}}},"post":{"operationId":"createItem","summary":"Create item","tags":["catalog","write"],"requestBody":{"content":{"application/json":{"example":{"id":"item-001","name":"Example Item"}}}},"responses":{"201":{"description":"Created"}}}}}}`
	if err := os.WriteFile(specPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write openapi spec: %v", err)
	}
	return specPath
}

func writeHTTPCapturePlanSpec(t *testing.T) string {
	t.Helper()
	capturePath := filepath.Join(t.TempDir(), "traffic.json")
	raw := `{"name":"Catalog Traffic","captures":[{"id":"createItem","name":"Create item from traffic","request":{"method":"POST","path":"/items","headers":{"Content-Type":"application/json"},"body":{"id":"item-001","name":"Example"}},"response":{"status":201,"body":{"id":"item-001"}}}]}`
	if err := os.WriteFile(capturePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	return capturePath
}

func writeOpenAPIGenerationPlanSpec(t *testing.T) string {
	t.Helper()
	specPath := filepath.Join(t.TempDir(), "catalog-openapi.json")
	raw := `{"openapi":"3.0.3","info":{"title":"Catalog API"},"paths":{"/items":{"post":{"operationId":"createItem","summary":"Create item","tags":["catalog"],"requestBody":{"content":{"application/json":{"schema":{"type":"object","required":["id"],"properties":{"id":{"type":"string","example":"item-001"},"name":{"type":"string","example":"Example Item"}}}}}},"responses":{"201":{"description":"Created"},"400":{"description":"Bad request"}}}}}}`
	if err := os.WriteFile(specPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write openapi spec: %v", err)
	}
	return specPath
}

func postPlanAPI(t *testing.T, serverURL string, path string, body string) []byte {
	t.Helper()
	resp, err := http.Post(serverURL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post plan api: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read plan api: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plan api status = %d body=%s", resp.StatusCode, raw)
	}
	return raw
}

func decodeOpenAPIImportPlan(t *testing.T, raw []byte) openAPIImportPlanPayload {
	t.Helper()
	var payload openAPIImportPlanPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode import plan: %v body=%s", err, raw)
	}
	return payload
}

func decodeOpenAPIGenerationPlan(t *testing.T, raw []byte) openAPIGenerationPlanPayload {
	t.Helper()
	var payload openAPIGenerationPlanPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode generation plan: %v body=%s", err, raw)
	}
	return payload
}

func assertOpenAPIImportPlan(t *testing.T, payload openAPIImportPlanPayload, specPath string) {
	t.Helper()
	if !payload.OK || payload.Kind != "openapi" || payload.SourcePath != specPath || payload.Plan.Service.ID != "service.catalog" || payload.Plan.Service.Status != "draft" {
		t.Fatalf("import plan summary = %#v", payload)
	}
	if len(payload.Plan.InterfaceNodes) != 2 || len(payload.Plan.APICases) != 2 || len(payload.Plan.CaseFiles) != 2 {
		t.Fatalf("import plan counts = nodes:%d cases:%d files:%d", len(payload.Plan.InterfaceNodes), len(payload.Plan.APICases), len(payload.Plan.CaseFiles))
	}
	node := payload.Plan.InterfaceNodes[0]
	if node.ID != "node.service.catalog.list-items" || node.Method != "GET" || node.Path != "/items" || node.Status != "draft" {
		t.Fatalf("first node = %#v", node)
	}
	apiCase := payload.Plan.APICases[1]
	if apiCase.ID != "case.service.catalog.create-item" || apiCase.CasePath != "api-cases/case.service.catalog.create-item.json" || apiCase.EvidenceDir != ".runtime/openapi" || strings.Join(apiCase.Tags, ",") != "openapi,catalog,write" {
		t.Fatalf("second case = %#v", apiCase)
	}
}

func assertHTTPCaptureImportPlan(t *testing.T, payload openAPIImportPlanPayload, capturePath string) {
	t.Helper()
	if !payload.OK || payload.Kind != "http-capture" || payload.SourcePath != capturePath || payload.Plan.Service.ID != "service.catalog" || payload.Plan.Service.Status != "draft" {
		t.Fatalf("capture plan summary = %#v", payload)
	}
	if len(payload.Plan.InterfaceNodes) != 1 || len(payload.Plan.APICases) != 1 || len(payload.Plan.CaseFiles) != 1 {
		t.Fatalf("capture plan counts = nodes:%d cases:%d files:%d", len(payload.Plan.InterfaceNodes), len(payload.Plan.APICases), len(payload.Plan.CaseFiles))
	}
	node := payload.Plan.InterfaceNodes[0]
	if node.ID != "node.service.catalog.create-item" || node.Method != "POST" || node.Path != "/items" {
		t.Fatalf("capture node = %#v", node)
	}
	apiCase := payload.Plan.APICases[0]
	if apiCase.ID != "case.service.catalog.create-item" || apiCase.CasePath != "api-cases/case.service.catalog.create-item.json" || apiCase.EvidenceDir != ".runtime/replay" || strings.Join(apiCase.Tags, ",") != "recorded,replay" {
		t.Fatalf("capture case = %#v", apiCase)
	}
}

func assertOpenAPIGenerationPlan(t *testing.T, payload openAPIGenerationPlanPayload, specPath string) {
	t.Helper()
	if !payload.OK || payload.Kind != "openapi" || payload.SourcePath != specPath || !payload.Plan.OK || len(payload.Plan.Candidates) != 1 || len(payload.Plan.APICases) != 1 || len(payload.Plan.CaseFiles) != 1 {
		t.Fatalf("generation plan summary = %#v", payload)
	}
	candidate := payload.Plan.Candidates[0]
	if candidate.Kind != "missing-required-field" || candidate.Field != "id" || candidate.CaseID != "case.service.catalog.create-item.missing-id" {
		t.Fatalf("generation candidate = %#v", candidate)
	}
	apiCase := payload.Plan.APICases[0]
	if apiCase.Status != "draft" || apiCase.EvidenceDir != ".runtime/generated" || strings.Join(apiCase.Tags, ",") != "generated,schema,negative,catalog" {
		t.Fatalf("generated api case = %#v", apiCase)
	}
}

func newEvidenceListServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := seedEvidenceListStore(t)
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	t.Cleanup(server.Close)
	return server
}

func seedEvidenceListStore(t *testing.T) store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	recordEvidenceListRun(t, ctx, s)
	recordEvidenceListCaseRun(t, ctx, s)
	recordEvidenceListAttachment(t, ctx, s)
	return s
}

func recordEvidenceListRun(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()
	if _, err := s.CreateRun(ctx, store.Run{ID: "run.alpha", ProfileID: "sample", WorkflowID: "workflow.alpha", Status: store.StatusPassed, EvidenceRoot: ".runtime/evidence/run.alpha", SummaryJSON: "{}"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
}

func recordEvidenceListCaseRun(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{ID: "run.alpha.case", RunID: "run.alpha", CaseID: "case.alpha", Status: store.StatusPassed, RequestSummaryJSON: `{"method":"GET","path":"/alpha"}`, AssertionSummaryJSON: `{"status":"passed"}`}); err != nil {
		t.Fatalf("record api case run: %v", err)
	}
}

func recordEvidenceListAttachment(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{ID: "run.alpha.response", RunID: "run.alpha", CaseRunID: "run.alpha.case", StepID: "step.alpha", Kind: "response", URI: ".runtime/evidence/run.alpha/response.json", MediaType: "application/json", SHA256: "sha256-alpha", SizeBytes: 42, Summary: `{"statusCode":200}`, Category: "http-response", Visibility: "public", LabelsJSON: `{"stepId":"step.alpha"}`}); err != nil {
		t.Fatalf("record evidence: %v", err)
	}
}

func assertEvidenceListPayload(t *testing.T, payload map[string]any) {
	t.Helper()
	if payload["ok"] != true {
		t.Fatalf("evidence list payload should expose ok envelope: %#v", payload)
	}
	run := onlyEvidenceListRun(t, payload)
	if run["id"] != "run.alpha" || run["profileId"] != "sample" || run["workflowId"] != "workflow.alpha" {
		t.Fatalf("evidence list run identity = %#v", run)
	}
	if run["apiCaseRunCount"] != float64(1) || run["evidenceCount"] != float64(1) {
		t.Fatalf("evidence list counts = %#v", run)
	}
	assertEvidenceListRecord(t, onlyEvidenceListRecord(t, run))
}

func onlyEvidenceListRun(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	runs := payload["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("evidence list runs = %#v", runs)
	}
	return runs[0].(map[string]any)
}

func onlyEvidenceListRecord(t *testing.T, run map[string]any) map[string]any {
	t.Helper()
	records := run["evidenceRecords"].([]any)
	if len(records) != 1 {
		t.Fatalf("evidence records = %#v", records)
	}
	return records[0].(map[string]any)
}

func assertEvidenceListRecord(t *testing.T, record map[string]any) {
	t.Helper()
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
