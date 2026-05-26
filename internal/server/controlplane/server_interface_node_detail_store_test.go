package controlplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerUsesRuntimeCatalogForInterfaceNodeDetails(t *testing.T) {
	ctx := context.Background()
	s := openInterfaceNodeDetailStore(t, ctx)
	defer s.Close()
	seedInterfaceNodeDetailCatalog(t, ctx, s)
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := getInterfaceNodeDetail(t, server.URL, "interface.alpha")
	requireInterfaceNodeDetailEnvelope(t, payload)
	requireInterfaceNodeDetailNode(t, payload)
	requireInterfaceNodeDetailRelatedAssets(t, payload)
	requireInterfaceNodeDetailCases(t, payload.Cases)
	requireInterfaceNodeDetailPresentation(t, payload)
}

type interfaceNodeDetailPayload struct {
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

func openInterfaceNodeDetailStore(t *testing.T, ctx context.Context) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s
}

func seedInterfaceNodeDetailCatalog(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()
	if err := s.ReplaceProfileCatalog(ctx, interfaceNodeDetailCatalog(time.Now().UTC())); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
}

func interfaceNodeDetailCatalog(now time.Time) store.ProfileCatalog {
	return store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Services:  []store.CatalogService{{ID: "entry-service", DisplayName: "Entry", Kind: "app"}},
		InterfaceNodes: []store.CatalogInterfaceNode{{
			ID: "interface.alpha", DisplayName: "Alpha", ServiceID: "entry-service", Operation: "alpha.create",
			Method: "POST", Path: "/alpha", TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1", Version: "v1",
			Status: "draft", Tags: []string{"baseline", "alpha"}, Description: "Alpha interface node", SortOrder: 7,
			CreatedAt: "2026-05-12 12:54:33", UpdatedAt: "2026-05-12 12:55:33",
		}},
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
		TemplateConfigs: interfaceNodeDetailTemplateConfigs(),
		CaseDependencies: []store.CatalogCaseDependency{
			{ID: "dep.alpha", CaseID: "case.alpha.failure", FixtureID: "fixture.alpha", Required: true, MappingsJSON: `[{"from":"$.id","to":"$.name"}]`, Status: "active", SortOrder: 1},
		},
		Fixtures: []store.CatalogFixture{
			{ID: "fixture.alpha", DisplayName: "Alpha fixture", Kind: "sql", DataJSON: "fixture data"},
		},
	}
}

func interfaceNodeDetailTemplateConfigs() []store.CatalogTemplateConfig {
	return []store.CatalogTemplateConfig{
		{ID: "cfg.interface-node.default", TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1", ScopeType: "interface-node", ScopeID: "_default", Title: "Default interface node presentation", ConfigJSON: `{"copy":{"casesTitle":"Default cases","runAllButton":"Run all default cases","emptyCases":"No configured cases.","historyTitle":"Configured history"}}`, Status: "active"},
		{ID: "cfg.interface.alpha", TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1", NodeID: "interface.alpha", ScopeType: "interface-node", ScopeID: "interface.alpha", Title: "Alpha interface node", ConfigJSON: `{"copy":{"casesTitle":"Configured cases","runAllButton":"Run configured cases","emptyCases":"No configured cases."}}`, Status: "active"},
	}
}

func getInterfaceNodeDetail(t *testing.T, serverURL string, id string) interfaceNodeDetailPayload {
	t.Helper()
	resp, err := http.Get(serverURL + "/api/interface-node?id=" + id)
	if err != nil {
		t.Fatalf("get interface node: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var payload interfaceNodeDetailPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload
}

func requireInterfaceNodeDetailEnvelope(t *testing.T, payload interfaceNodeDetailPayload) {
	t.Helper()
	if !payload.OK || payload.TemplateID != "TPL-INTERFACE-NODE-CASE-LIST-V1" || payload.Source["kind"] != "store" {
		t.Fatalf("interface detail envelope = %#v", payload)
	}
}

func requireInterfaceNodeDetailNode(t *testing.T, payload interfaceNodeDetailPayload) {
	t.Helper()
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
}

func requireInterfaceNodeDetailRelatedAssets(t *testing.T, payload interfaceNodeDetailPayload) {
	t.Helper()
	if len(payload.RequestTemplates) != 1 || payload.RequestTemplates[0]["id"] != "tpl.alpha" {
		t.Fatalf("request templates = %#v", payload.RequestTemplates)
	}
	if len(payload.Fields.Request) != 1 || payload.Fields.Request[0]["fieldPath"] != "$.name" {
		t.Fatalf("request fields = %#v", payload.Fields.Request)
	}
}

func requireInterfaceNodeDetailCases(t *testing.T, cases []map[string]any) {
	t.Helper()
	if len(cases) != 2 || cases[0]["caseType"] != "failure" || cases[0]["requiredForAdmission"] != true || cases[0]["requestTemplateId"] != "tpl.alpha" {
		t.Fatalf("cases = %#v", cases)
	}
	successCase := cases[1]
	for _, key := range []string{"blocked", "blockedReason", "scenario", "requestTemplateId"} {
		if _, ok := successCase[key]; !ok {
			t.Fatalf("case should expose stable key %q: %#v", key, successCase)
		}
	}
	if successCase["blocked"] != false || successCase["blockedReason"] != "" || successCase["scenario"] != "" || successCase["requestTemplateId"] != "" {
		t.Fatalf("case empty contract fields = %#v", successCase)
	}
}

func requireInterfaceNodeDetailPresentation(t *testing.T, payload interfaceNodeDetailPayload) {
	t.Helper()
	copy := payload.Presentation.Copy
	if copy.CasesTitle != "Configured cases" || copy.RunAllButton != "Run configured cases" || copy.EmptyCases != "No configured cases." || copy.HistoryTitle != "Configured history" {
		t.Fatalf("interface node presentation copy = %#v", copy)
	}
}
