package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store/sqlite"
)

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
