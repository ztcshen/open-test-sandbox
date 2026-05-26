package controlplane_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

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

	payload := decodeJSONResponse(t, server.URL+"/api/dashboard", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["total"] != float64(1) || summary["missing"] != float64(1) || summary["healthy"] != float64(0) || summary["unhealthy"] != float64(0) {
		t.Fatalf("dashboard summary = %#v", summary)
	}
	groups := payload["groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["id"] != "business" {
		t.Fatalf("dashboard groups = %#v", groups)
	}
	item := groups[0].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["id"] != "service.alpha" || item["name"] != "Service Alpha" || item["state"] != "missing" {
		t.Fatalf("dashboard item = %#v", item)
	}
	if item["branch"] != "sample" || item["profile"] != "sample" {
		t.Fatalf("dashboard item profile markers = %#v", item)
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
	installDashboardDockerPS(t, `{"Names":"sandbox-service-alpha","Image":"example/service-alpha:1","State":"running","Status":"Up 12 seconds","Ports":"0.0.0.0:18080->8080/tcp, 0.0.0.0:19090->9090/tcp"}`)
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/dashboard", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["total"] != float64(1) || summary["healthy"] != float64(0) || summary["missing"] != float64(0) || summary["unhealthy"] != float64(1) {
		t.Fatalf("dashboard summary = %#v", summary)
	}
	item := firstDashboardItem(payload)
	if item["id"] != "service-alpha" || item["ok"] != false || item["state"] != "running" || item["health"] != "unchecked" {
		t.Fatalf("dashboard item state = %#v", item)
	}
	if item["container"] != "sandbox-service-alpha" || item["image"] != "example/service-alpha:1" || item["port"] != float64(18080) || item["managementPort"] != float64(19090) {
		t.Fatalf("dashboard item runtime = %#v", item)
	}
}

func TestServerHydratesDashboardHealthFromHTTPCheck(t *testing.T) {
	target := newDashboardHealthServer(t, "/health", `{"ready":true}`)
	defer target.Close()
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service-alpha", DisplayName: "Service Alpha", Kind: "http", HealthURL: target.URL + "/health"},
		},
	}
	installDashboardDockerPS(t, `{"Names":"sandbox-service-alpha","Image":"example/service-alpha:1","State":"running","Status":"Up 12 seconds","Ports":"0.0.0.0:18080->8080/tcp, 0.0.0.0:19090->9090/tcp"}`)
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/dashboard", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["total"] != float64(1) || summary["healthy"] != float64(1) || summary["missing"] != float64(0) {
		t.Fatalf("dashboard summary = %#v", summary)
	}
	item := firstDashboardItem(payload)
	if item["id"] != "service-alpha" || item["ok"] != true || item["state"] != "running" || item["health"] != "healthy" {
		t.Fatalf("dashboard item state = %#v", item)
	}
}

func TestServerHydratesDashboardHealthFromEnvironmentComponentGraph(t *testing.T) {
	ctx := context.Background()
	s := openDashboardSQLiteStore(t, ctx)
	defer s.Close()
	target := newDashboardHealthServer(t, "/actuator/health", `{"status":"UP"}`)
	defer target.Close()
	seedDashboardComponentGraph(t, ctx, s, target.URL+"/actuator/health")
	installDashboardDockerPS(t, `{"Names":"sandbox-alpha","Image":"example/service-alpha:1","State":"running","Status":"Up 12 seconds","Ports":"0.0.0.0:18080->8080/tcp"}`)
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/dashboard", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["healthy"] != float64(1) || summary["unhealthy"] != float64(0) {
		t.Fatalf("dashboard summary = %#v", summary)
	}
	item := firstDashboardItem(payload)
	if item["id"] != "service-alpha" || item["ok"] != true || item["health"] != "healthy" {
		t.Fatalf("dashboard item should use component graph HTTP health: %#v", item)
	}
}

func TestServerUsesRuntimeCatalogForDashboardSnapshot(t *testing.T) {
	ctx := context.Background()
	s := openDashboardSQLiteStore(t, ctx)
	defer s.Close()
	now := time.Now().UTC()
	seedDashboardReadModel(t, ctx, s, runtimeDashboardCatalog(now), now)
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	requireRuntimeCatalogDashboardPayload(t, decodeJSONResponse(t, server.URL+"/api/dashboard", http.StatusOK))
}

func installDashboardDockerPS(t *testing.T, rows ...string) {
	t.Helper()
	fakeBin := t.TempDir()
	docker := filepath.Join(fakeBin, "docker")
	script := "#!/bin/sh\ncat <<'JSON'\n" + strings.Join(rows, "\n") + "\nJSON\n"
	if err := os.WriteFile(docker, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func newDashboardHealthServer(t *testing.T, path string, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Fatalf("unexpected health path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
}

func openDashboardSQLiteStore(t *testing.T, ctx context.Context) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	return s
}

func firstDashboardItem(payload map[string]any) map[string]any {
	return payload["groups"].([]any)[0].(map[string]any)["items"].([]any)[0].(map[string]any)
}

func seedDashboardComponentGraph(t *testing.T, ctx context.Context, s store.Store, healthURL string) {
	t.Helper()
	now := time.Now().UTC()
	catalog := store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Services:  []store.CatalogService{{ID: "service-alpha", DisplayName: "Service Alpha", Kind: "app", ContainerName: "sandbox-alpha", Status: "active"}},
	}
	seedDashboardReadModel(t, ctx, s, catalog, now)
	if _, err := s.UpsertEnvironment(ctx, store.Environment{ID: "env.sample", DisplayName: "Sample Environment", Status: "draft", VerificationWorkflowID: "workflow.sample"}); err != nil {
		t.Fatalf("upsert environment: %v", err)
	}
	graph := store.EnvironmentComponentGraph{Components: []store.EnvironmentComponent{{
		ComponentID:     "service-alpha",
		Kind:            "app",
		Role:            "business-service",
		ComposeService:  "service-alpha",
		Required:        true,
		HealthCheckJSON: fmt.Sprintf(`{"kind":"url","url":%q}`, healthURL),
	}}}
	if err := s.ReplaceEnvironmentComponentGraph(ctx, "env.sample", graph); err != nil {
		t.Fatalf("replace component graph: %v", err)
	}
}

func seedDashboardReadModel(t *testing.T, ctx context.Context, s store.Store, catalog store.ProfileCatalog, now time.Time) {
	t.Helper()
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
}

func runtimeDashboardCatalog(now time.Time) store.ProfileCatalog {
	return store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Alpha Service", Kind: "app", ContainerName: "sandbox-alpha", Image: "example/alpha:1", ServicePort: 18080, ManagementPort: 19090, SourcePath: "/tmp/alpha", GitBranch: "main", Status: "active", SortOrder: 1},
			{ID: "service.beta", DisplayName: "Beta Service", Kind: "app", ContainerName: "sandbox-beta", Image: "example/beta:1", ServicePort: 18081, ManagementPort: 19091, SourcePath: "/tmp/runtime/service/beta-4e8d26674209", Status: "active", SortOrder: 2},
			{ID: "service.retired", DisplayName: "Retired Service", Kind: "app", ContainerName: "sandbox-retired", Image: "example/retired:1", ServicePort: 18082, ManagementPort: 19092, Status: "inactive", SortOrder: 3},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{ID: "cfg.environment.default", TemplateID: "TPL-ENVIRONMENT-NODE-LIST-V1", ScopeType: "environment", ScopeID: "_default", Title: "Default environment presentation", ConfigJSON: `{"copy":{"listTitle":"Configured environments","detailTitle":"Configured service detail","runtimeTitle":"Configured runtime","connectionTitle":"Configured connection","openServicePort":"Open configured service"}}`, Status: "active"},
			{ID: "cfg.environment.service.alpha", TemplateID: "TPL-ENVIRONMENT-NODE-DETAIL-V1", NodeID: "service.alpha", ScopeType: "environment-node", ScopeID: "service.alpha", Title: "Alpha environment presentation", ConfigJSON: `{"copy":{"detailTitle":"Alpha configured detail","runtimeTitle":"Alpha runtime"}}`, Status: "active"},
		},
	}
}

func requireRuntimeCatalogDashboardPayload(t *testing.T, payload map[string]any) {
	t.Helper()
	if payload["ok"] != true {
		t.Fatalf("dashboard envelope = %#v", payload)
	}
	source := payload["source"].(map[string]any)
	if source["kind"] != "read-model" || source["id"] != "sample" {
		t.Fatalf("dashboard source = %#v", source)
	}
	copy := payload["presentation"].(map[string]any)["copy"].(map[string]any)
	if copy["listTitle"] != "Configured environments" || copy["connectionTitle"] != "Configured connection" {
		t.Fatalf("dashboard presentation copy = %#v", copy)
	}
	items := payload["groups"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("dashboard should hide inactive services, items = %#v", items)
	}
	requireRuntimeCatalogDashboardItem(t, items[0].(map[string]any))
	requireRuntimeCatalogDashboardRuntime(t, payload["serviceRuntime"].([]any))
}

func requireRuntimeCatalogDashboardItem(t *testing.T, item map[string]any) {
	t.Helper()
	if item["id"] != "service.alpha" || item["container"] != "sandbox-alpha" || item["port"] != float64(18080) || item["managementPort"] != float64(19090) {
		t.Fatalf("dashboard item = %#v", item)
	}
	copy := item["presentation"].(map[string]any)["copy"].(map[string]any)
	if copy["detailTitle"] != "Alpha configured detail" || copy["runtimeTitle"] != "Alpha runtime" || copy["openServicePort"] != "Open configured service" {
		t.Fatalf("dashboard item presentation = %#v", copy)
	}
}

func requireRuntimeCatalogDashboardRuntime(t *testing.T, runtimes []any) {
	t.Helper()
	runtimeByID := map[string]map[string]any{}
	for _, raw := range runtimes {
		runtime := raw.(map[string]any)
		runtimeByID[fmt.Sprint(runtime["serviceId"])] = runtime
	}
	if runtimeByID["service.alpha"]["branchName"] != "main" || runtimeByID["service.alpha"]["sourcePath"] != "/tmp/alpha" {
		t.Fatalf("dashboard alpha runtime = %#v", runtimeByID["service.alpha"])
	}
	betaRuntime := runtimeByID["service.beta"]
	if betaRuntime["branchName"] != "beta" || betaRuntime["commitId"] != "4e8d26674209" || betaRuntime["sourcePath"] != "/tmp/runtime/service/beta-4e8d26674209" {
		t.Fatalf("dashboard beta runtime = %#v", betaRuntime)
	}
	if _, ok := runtimeByID["service.retired"]; ok {
		t.Fatalf("dashboard runtime should hide inactive services: %#v", runtimeByID["service.retired"])
	}
}
