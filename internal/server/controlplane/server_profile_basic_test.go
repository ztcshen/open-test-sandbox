package controlplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesProfileAPI(t *testing.T) {
	bundle := loadEmptyProfile(t)
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/template-packages/current")
	if err != nil {
		t.Fatalf("get template package current api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("template package current api status = %d", resp.StatusCode)
	}

	var payload struct {
		TemplatePackageID string         `json:"templatePackageId"`
		ID                string         `json:"id"`
		DisplayName       string         `json:"displayName"`
		Counts            profile.Counts `json:"counts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode template package current api: %v", err)
	}
	if payload.TemplatePackageID != "empty" || payload.ID != "empty" || payload.DisplayName != "Empty Profile" || payload.Counts.Workflows != 0 {
		t.Fatalf("template package current api payload = %#v", payload)
	}
}

func TestServerExposesExecutorPlanAPI(t *testing.T) {
	server := httptest.NewServer(controlplane.New(profile.Bundle{
		ID: "sample",
		Executors: []profile.ExecutorDescriptor{
			{ID: "executor.command", DisplayName: "No-op command", Kind: "custom-command", Command: "true", Status: "active"},
			{ID: "executor.pytest", DisplayName: "Pytest suite", Kind: "pytest", Status: "active"},
		},
	}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/executor/plan", http.StatusOK)
	if payload["ok"] != false || payload["profileId"] != "sample" {
		t.Fatalf("executor plan payload = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"] != float64(2) || counts["ready"] != float64(1) || counts["blocked"] != float64(1) {
		t.Fatalf("executor plan counts = %#v", counts)
	}
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("executor plan items = %#v", items)
	}
	first := items[0].(map[string]any)
	second := items[1].(map[string]any)
	if first["id"] != "executor.command" || first["ready"] != true || first["runMode"] != "dry-run" {
		t.Fatalf("ready executor item = %#v", first)
	}
	issues := second["issues"].([]any)
	if second["id"] != "executor.pytest" || second["ready"] != false || len(issues) != 1 || issues[0] != "missing-source-path" {
		t.Fatalf("blocked executor item = %#v", second)
	}
}

func TestServerExecutorPlanPrefersStoreCatalog(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "executor.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		APICases: []store.CatalogAPICase{
			{ID: "case.catalog", DisplayName: "Catalog Case", SourceKind: "karate", SourcePath: "tests/catalog.feature", ExecutorID: "executor.catalog", Status: "active", TimeoutSeconds: 9},
		},
	}); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{
		ID: "bundle-only",
		Executors: []profile.ExecutorDescriptor{
			{ID: "executor.bundle", Kind: "custom-command", Command: "true", Status: "active"},
		},
	}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/executor/plan", http.StatusOK)
	if payload["ok"] != true || payload["profileId"] != "current" {
		t.Fatalf("store executor plan payload = %#v", payload)
	}
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("store executor plan items = %#v", items)
	}
	item := items[0].(map[string]any)
	if item["id"] != "executor.catalog" || item["kind"] != "karate" || item["sourcePath"] != "tests/catalog.feature" || item["ready"] != true || item["timeoutSeconds"] != float64(9) {
		t.Fatalf("store executor plan item = %#v", item)
	}
}

func TestServerExposesCurrentStoreAPIWithMaskedURL(t *testing.T) {
	tests := []struct {
		name    string
		info    controlplane.StoreInfo
		wantURL string
	}{
		{
			name: "postgres",
			info: controlplane.StoreInfo{
				Configured: true,
				Name:       "team-verified",
				Backend:    "postgres",
				URL:        "postgres://tester:xxxxx@example.com:5432/team_verified?sslmode=require",
				Source:     "active-config",
			},
			wantURL: "postgres://tester:xxxxx@example.com:5432/team_verified?sslmode=require",
		},
		{
			name: "mysql",
			info: controlplane.StoreInfo{
				Configured: true,
				Name:       "team-mysql",
				Backend:    "mysql",
				URL:        "mysql://tester:xxxxx@example.com:3306/agent_testbench_team?tls=false",
				Source:     "active-config",
			},
			wantURL: "mysql://tester:xxxxx@example.com:3306/agent_testbench_team?tls=false",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{
				StoreInfo: tt.info,
			}))
			defer server.Close()

			payload := decodeJSONResponse(t, server.URL+"/api/store/current", http.StatusOK)
			if payload["ok"] != true || payload["configured"] != true {
				t.Fatalf("store current flags = %#v", payload)
			}
			if payload["name"] != tt.info.Name || payload["backend"] != tt.info.Backend || payload["source"] != "active-config" {
				t.Fatalf("store current metadata = %#v", payload)
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			if strings.Contains(string(raw), "secret") || payload["url"] != tt.wantURL {
				t.Fatalf("store current url was not masked: %s", raw)
			}
		})
	}
}
