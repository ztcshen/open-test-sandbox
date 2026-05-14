package controlplane_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"open-test-sandbox/internal/controlplane"
	"open-test-sandbox/internal/profile"
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
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
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
			Items []struct {
				ID      string `json:"id"`
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
	if len(payload.Groups[0].Items) != 1 || payload.Groups[0].Items[0].ID != "service.alpha" || payload.Groups[0].Items[0].State != "missing" {
		t.Fatalf("dashboard items = %#v", payload.Groups[0].Items)
	}
	if payload.Groups[0].Items[0].Branch != "sample" || payload.Groups[0].Items[0].Profile != "sample" {
		t.Fatalf("dashboard item profile markers = %#v", payload.Groups[0].Items[0])
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

func loadEmptyProfile(t *testing.T) profile.Bundle {
	t.Helper()
	bundle, err := profile.Load(filepath.Join("..", "..", "profiles", "empty"))
	if err != nil {
		t.Fatalf("load empty profile: %v", err)
	}
	return bundle
}
