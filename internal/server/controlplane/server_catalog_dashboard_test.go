package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

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
