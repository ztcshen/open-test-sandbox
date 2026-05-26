package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
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
