package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

type profileExportCommandReport struct {
	OK        bool   `json:"ok"`
	ProfileID string `json:"profileId"`
	Output    string `json:"output"`
	Counts    struct {
		Services        int `json:"services"`
		Workflows       int `json:"workflows"`
		InterfaceNodes  int `json:"interfaceNodes"`
		APICases        int `json:"apiCases"`
		TemplateConfigs int `json:"templateConfigs"`
	} `json:"counts"`
}

func TestProfileExportWritesActiveStoreCatalogAsProfileBundle(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	seedProfileExportCatalog(t, storePath)

	outputDir := filepath.Join(t.TempDir(), "exported-profile")
	report := runProfileExportJSON(t, storePath, outputDir)
	requireProfileExportReport(t, report, outputDir)
	requireExportedProfileBundle(t, outputDir)
}

func seedProfileExportCatalog(t *testing.T, storePath string) {
	t.Helper()

	ctx := context.Background()
	runtime, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	now := time.Now().UTC()
	if err := runtime.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "profile.export",
		IndexedAt: now,
		Services: []store.CatalogService{{
			ID:          "service.alpha",
			DisplayName: "Service Alpha",
			ServicePort: 18080,
		}},
		Workflows: []store.CatalogWorkflow{{
			ID:          "workflow.alpha",
			DisplayName: "Workflow Alpha",
		}},
		InterfaceNodes: []store.CatalogInterfaceNode{{
			ID:          "node.alpha",
			DisplayName: "Node Alpha",
			ServiceID:   "service.alpha",
			Method:      "GET",
			Path:        "/v1/items",
		}},
		APICases: []store.CatalogAPICase{{
			ID:          "case.alpha",
			DisplayName: "Case Alpha",
			NodeID:      "node.alpha",
			Status:      "active",
		}},
		TemplateConfigs: []store.CatalogTemplateConfig{{
			ID:         "cfg.case.alpha",
			TemplateID: "case-execution",
			NodeID:     "node.alpha",
			ScopeType:  "case",
			ScopeID:    "case.alpha",
			ConfigJSON: `{"caseId":"case.alpha","caseExecution":{"method":"GET","nodeId":"node.alpha","path":"/v1/items","query":{"id":"item-001"},"expectedHttpCodes":[200]}}`,
			Status:     "active",
		}},
	}); err != nil {
		t.Fatalf("seed profile catalog: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

func runProfileExportJSON(t *testing.T, storePath string, outputDir string) profileExportCommandReport {
	t.Helper()

	out := runCLI(t, "profile", "export", "--store", "sqlite://"+storePath, "--output", outputDir, "--json")
	var report profileExportCommandReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode export report: %v\n%s", err, out)
	}
	return report
}

func requireProfileExportReport(t *testing.T, report profileExportCommandReport, outputDir string) {
	t.Helper()

	if !report.OK || report.ProfileID != "profile.export" || report.Output != outputDir || report.Counts.TemplateConfigs != 2 {
		t.Fatalf("export report = %#v", report)
	}
}

func requireExportedProfileBundle(t *testing.T, outputDir string) {
	t.Helper()

	bundle, err := profile.Load(outputDir)
	if err != nil {
		t.Fatalf("load exported profile: %v", err)
	}
	if bundle.ID != "profile.export" || len(bundle.Services) != 1 || len(bundle.APICases) != 1 || len(bundle.TemplateConfigs) != 2 {
		t.Fatalf("exported bundle = %#v", bundle)
	}
	configs := caseExecutionConfigIDs(bundle.TemplateConfigs)
	if configs["case.alpha"] != "cfg.case.alpha" || !strings.Contains(bundle.TemplateConfigs[1].ConfigJSON+bundle.TemplateConfigs[0].ConfigJSON, `"query":{"id":"item-001"}`) {
		t.Fatalf("exported template configs lost case query: %#v", bundle.TemplateConfigs)
	}
}
