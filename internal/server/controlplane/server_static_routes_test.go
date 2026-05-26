package controlplane_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
)

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
	for _, want := range []string{"AgentTestBench", "react-dashboard-root", "/assets/react/dashboard.js"} {
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
