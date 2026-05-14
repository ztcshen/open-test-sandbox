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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"open-test-sandbox/internal/controlplane"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
)

func TestServerExposesProfileAPI(t *testing.T) {
	bundle := loadEmptyProfile(t)
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/profile")
	if err != nil {
		t.Fatalf("get profile api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("profile api status = %d", resp.StatusCode)
	}

	var payload struct {
		ID          string         `json:"id"`
		DisplayName string         `json:"displayName"`
		Counts      profile.Counts `json:"counts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode profile api: %v", err)
	}
	if payload.ID != "empty" || payload.DisplayName != "Empty Profile" || payload.Counts.Workflows != 0 {
		t.Fatalf("profile api payload = %#v", payload)
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
				CasePath:       "profiles/sample/cases/case.alpha.json",
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

	resp, err := http.Get(server.URL + "/api/profile/assets")
	if err != nil {
		t.Fatalf("get profile assets api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("profile assets api status = %d", resp.StatusCode)
	}

	var payload struct {
		Services       []profile.Service       `json:"services"`
		Workflows      []profile.Workflow      `json:"workflows"`
		InterfaceNodes []profile.InterfaceNode `json:"interfaceNodes"`
		APICases       []profile.APICase       `json:"apiCases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode profile assets api: %v", err)
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

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":"../../profiles/empty"}`, http.StatusOK)

	if payload["profileId"] != "empty" || payload["bundlePath"] != "../../profiles/empty" {
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
	index, err := s.GetProfileIndex(ctx, "empty")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if index.BundlePath != "../../profiles/empty" || !strings.HasPrefix(index.BundleDigest, "sha256:") || index.ImportedAt.IsZero() {
		t.Fatalf("profile index = %#v", index)
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
	catalogIndex := decodeJSONResponse(t, server.URL+"/api/profile/catalog-index", http.StatusOK)
	if catalogIndex["profileId"] != "sample" || catalogIndex["indexedAt"] == "" {
		t.Fatalf("catalog index identity = %#v", catalogIndex)
	}
	catalogCounts, ok := catalogIndex["counts"].(map[string]any)
	if !ok || catalogCounts["services"] != float64(1) || catalogCounts["workflows"] != float64(1) || catalogCounts["templates"] != float64(1) {
		t.Fatalf("catalog index counts = %#v", catalogIndex["counts"])
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

func TestServerRejectsProfileImportWithoutRuntimeStore(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":"../../profiles/empty"}`, http.StatusNotImplemented)

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
		{path: "/agent-test.html", want: "react-agent-test-root"},
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
		{path: "/assets/react/agentTest.js", want: "/api/agent-test"},
		{path: "/assets/react/apiCases.js", want: "/api/cases/capabilities"},
		{path: "/assets/react/caseRuns.js", want: "/api/case/incomplete-batches"},
		{path: "/assets/react/environmentNode.js", want: "/api/interface-nodes"},
		{path: "/assets/react/environmentNodes.js", want: "/api/dashboard"},
		{path: "/assets/react/evidenceViewer.js", want: "/api/case/evidence"},
		{path: "/assets/react/interfaceNode.js", want: "/api/interface-node"},
		{path: "/assets/react/interfaceNodes.js", want: "/api/interface-nodes"},
		{path: "/assets/react/replayEvidence.js", want: "/api/replay/evidence"},
		{path: "/assets/react/sandboxWorkbench.js", want: "/api/profile/import"},
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
			CreatedAt:    started.Add(time.Minute),
			UpdatedAt:    started.Add(time.Minute),
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
	empty := workflows[1].(map[string]any)
	if empty["id"] != "workflow.empty" || empty["runCount"] != float64(0) {
		t.Fatalf("empty workflow run state = %#v", empty)
	}
	if _, ok := empty["latestRun"]; ok {
		t.Fatalf("empty workflow should not expose latestRun: %#v", empty)
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
		WorkflowRuns []map[string]any `json:"workflowRuns"`
		ReplayRuns   []map[string]any `json:"replayRuns"`
		ProbeRuns    []map[string]any `json:"probeRuns"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode runs api: %v", err)
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

	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  `{"summary":{"expectedStepCount":2,"stepCount":2},"steps":[{"stepId":"step.alpha","ok":true},{"stepId":"step.beta","ok":false}]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	list := decodeJSONResponse(t, server.URL+"/api/runs", http.StatusOK)
	workflowRuns := list["workflowRuns"].([]any)
	if len(workflowRuns) != 1 || workflowRuns[0].(map[string]any)["id"] != "run.alpha" {
		t.Fatalf("workflow run list = %#v", list)
	}

	detail := decodeJSONResponse(t, server.URL+"/api/workflow-runs/run.alpha", http.StatusOK)
	if detail["ok"] != true {
		t.Fatalf("workflow run detail failed: %#v", detail)
	}
	if detail["traceTopologies"] == nil {
		t.Fatalf("workflow run detail should include topology array: %#v", detail)
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

	latest := decodeJSONResponse(t, server.URL+"/api/workflow-runs/latest-step?workflowId=workflow.alpha&stepId=step.beta", http.StatusOK)
	latestRun := latest["run"].(map[string]any)
	if latestRun["id"] != "run.alpha" {
		t.Fatalf("latest workflow step run = %#v", latest)
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
		"steps":[{"stepId":"step.alpha","ok":true}],
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
		"stepId":"step.alpha",
		"dryRun":true
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
	if result["ok"] != true || result["caseId"] != "case.alpha" || result["stepId"] != "step.alpha" {
		t.Fatalf("test kit run result = %#v", result)
	}

	runs := decodeJSONResponse(t, server.URL+"/api/runs", http.StatusOK)
	workflowRuns := runs["workflowRuns"].([]any)
	if len(workflowRuns) != 1 || workflowRuns[0].(map[string]any)["workflowId"] != "workflow.alpha" {
		t.Fatalf("test kit run should be indexed in store: %#v", runs)
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
		"caseIds":["case.alpha","case.beta"],
		"dryRun":true
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
	if !payload.OK || len(payload.Results) != 2 || payload.Summary.CaseCount != 2 || payload.Summary.Passed != 2 {
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
			{ID: "case.alpha", DisplayName: "Case Alpha", CasePath: "profiles/sample/cases/case.alpha.json"},
			{ID: "case.beta", DisplayName: "Case Beta", CasePath: "profiles/sample/cases/case.beta.json"},
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
	if item["id"] != "case.beta" || item["reason"] != "not-run" || !strings.Contains(item["suggestedCommand"].(string), "profiles/sample/cases/case.beta.json") {
		t.Fatalf("incomplete case item = %#v", item)
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
	if err := os.WriteFile(requestPath, []byte(`{"method":"POST","path":"/alpha","headers":{"Content-Type":"application/json"},"body":{"id":"item-001"}}`), 0o644); err != nil {
		t.Fatalf("write request evidence: %v", err)
	}
	responsePath := filepath.Join(evidenceDir, "response.json")
	if err := os.WriteFile(responsePath, []byte(`{"statusCode":200,"headers":{"Content-Type":"application/json"},"body":"{\"ok\":true}"}`), 0o644); err != nil {
		t.Fatalf("write response evidence: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "run.alpha.request",
		RunID:     "run.alpha",
		CaseRunID: "run.alpha.case",
		Kind:      "request",
		URI:       requestPath,
		MediaType: "application/json",
		Summary:   `{"method":"POST","path":"/alpha","hasBody":true}`,
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

	payload := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId=run.alpha", http.StatusOK)
	evidence := payload["evidence"].(map[string]any)
	summary := evidence["summary"].(map[string]any)
	request := evidence["request"].(map[string]any)
	response := evidence["response"].(map[string]any)
	assertions := evidence["assertions"].(map[string]any)
	if summary["case_id"] != "case.alpha" || request["method"] != "POST" || request["path"] != "/alpha" {
		t.Fatalf("case evidence request = %#v", payload)
	}
	requestBody := request["body"].(map[string]any)
	if requestBody["id"] != "item-001" {
		t.Fatalf("case evidence request body = %#v", request)
	}
	if response["http_code"] != float64(200) || assertions["status"] != "passed" {
		t.Fatalf("case evidence response/assertions = %#v", payload)
	}
	if response["body"] != `{"ok":true}` {
		t.Fatalf("case evidence response body = %#v", response)
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
				CasePath:       "profiles/sample/cases/case.alpha.json",
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
	if payload.Cases[0].CasePath != "profiles/sample/cases/case.alpha.json" || payload.Cases[0].BaseURL == "" || payload.Cases[0].EvidenceDir != ".runtime/cases" || payload.Cases[0].TimeoutSeconds != 30 || payload.Cases[0].DefaultOverrides["itemId"] != "item-001" {
		t.Fatalf("api case run config = %#v", payload.Cases[0])
	}
	if len(payload.Cases[0].Graph.Nodes) != 1 || payload.Cases[0].Graph.Nodes[0].ID != "service.alpha" || payload.Cases[0].Graph.Nodes[0].Role != "http" {
		t.Fatalf("api case graph = %#v", payload.Cases[0].Graph)
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
		DryRun    bool   `json:"dryRun"`
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
	if !payload.OK || payload.DryRun || payload.Report.CaseID != "case.alpha" || payload.Report.Status != store.StatusPassed || payload.ViewerURL == "" {
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

func loadEmptyProfile(t *testing.T) profile.Bundle {
	t.Helper()
	bundle, err := profile.Load(filepath.Join("..", "..", "profiles", "empty"))
	if err != nil {
		t.Fatalf("load empty profile: %v", err)
	}
	return bundle
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
	return profileDir
}

type failingListRunsStore struct {
	store.Store
}

func (failingListRunsStore) ListRuns(context.Context) ([]store.Run, error) {
	return nil, errors.New("list runs failed")
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(raw)
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
