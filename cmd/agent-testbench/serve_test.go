package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/domain/profilecatalog"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServeHandlerUsesConfiguredStore(t *testing.T) {
	storePath := seedServeRunStore(t, "run.alpha")
	handler, cleanup := newServeStoreHandler(t, storePath)
	defer cleanup()

	requireServeRunsContains(t, handler, "run.alpha", "configured store")
}

func seedServeRunStore(t *testing.T, runID string) string {
	t.Helper()

	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:           runID,
		ProfileID:    "empty",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/" + runID,
		SummaryJSON:  `{"steps":[{"stepId":"step.alpha","ok":true}]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	return storePath
}

func newServeStoreHandler(t *testing.T, storePath string) (http.Handler, func()) {
	t.Helper()

	handler, cleanup, err := serveHandlerFromArgs([]string{
		"--store", "sqlite://" + storePath,
	})
	if err != nil {
		t.Fatalf("build serve handler: %v", err)
	}
	return handler, func() { _ = cleanup() }
}

func requireServeRunsContains(t *testing.T, handler http.Handler, runID string, label string) {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), runID) {
		t.Fatalf("serve handler did not use %s: %s", label, rec.Body.String())
	}
}

func TestServeHandlerRequiresActiveStore(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("AGENT_TESTBENCH_CONFIG_HOME", configHome)
	cwd := t.TempDir()
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir temp cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalCwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	_, _, err = serveHandlerFromArgs(nil)
	if err == nil {
		t.Fatal("serve handler should require an active Store")
	}
	if !errors.Is(err, errNoActiveStoreConfigured) {
		t.Fatalf("serve handler error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, "runtime", "store.sqlite")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("serve should not create an implicit sqlite store, stat err=%v", statErr)
	}
}

func TestServeHandlerAcceptsLocationAgnosticStoreFlag(t *testing.T) {
	storePath := seedServeRunStore(t, "run.store.flag")
	handler, cleanup := newServeStoreHandler(t, storePath)
	defer cleanup()

	requireServeRunsContains(t, handler, "run.store.flag", "--store")

	current := httptest.NewRecorder()
	handler.ServeHTTP(current, httptest.NewRequest(http.MethodGet, "/api/store/current", nil))
	if current.Code != http.StatusOK {
		t.Fatalf("store current status = %d body=%s", current.Code, current.Body.String())
	}
	var payload struct {
		OK         bool   `json:"ok"`
		Configured bool   `json:"configured"`
		Backend    string `json:"backend"`
		URL        string `json:"url"`
		Source     string `json:"source"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode store current payload: %v\n%s", err, current.Body.String())
	}
	if !payload.OK || !payload.Configured || payload.Backend != "sqlite" || payload.Source != "store-flag" || payload.URL != "sqlite://"+storePath {
		t.Fatalf("store current payload = %#v", payload)
	}
}

func TestServeHandlerCanBootFromPublishedStoreCatalogWithoutProfilePath(t *testing.T) {
	storePath, sourcePath := seedServePublishedStoreCatalog(t)
	handler, cleanup, err := serveHandlerFromArgs([]string{"--store", "sqlite://" + storePath})
	if err != nil {
		t.Fatalf("build serve handler from store catalog: %v", err)
	}
	defer cleanup()

	requireServeInterfaceNodesFromStoreCatalog(t, handler)
	requireServeDashboardUsesRuntimeSource(t, handler, sourcePath)
}

func seedServePublishedStoreCatalog(t *testing.T) (string, string) {
	t.Helper()

	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	sourcePath := filepath.Join(t.TempDir(), "sources", "service-alpha", "main-4e8d26674209")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "team-alpha",
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http", SourcePath: sourcePath},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "create", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	return storePath, sourcePath
}

func requireServeInterfaceNodesFromStoreCatalog(t *testing.T, handler http.Handler) {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/interface-nodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("interface nodes status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Source struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"source"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode interface nodes payload: %v\n%s", err, rec.Body.String())
	}
	if payload.Source.ID != "team-alpha" || payload.Source.Kind != "store" || len(payload.Items) != 1 || payload.Items[0].ID != "node.alpha" {
		t.Fatalf("serve handler did not use published catalog: %#v", payload)
	}
}

func requireServeDashboardUsesRuntimeSource(t *testing.T, handler http.Handler, sourcePath string) {
	t.Helper()

	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d body=%s", dashboard.Code, dashboard.Body.String())
	}
	if !strings.Contains(dashboard.Body.String(), sourcePath) || !strings.Contains(dashboard.Body.String(), "4e8d26674209") {
		t.Fatalf("dashboard did not use published runtime source: %s", dashboard.Body.String())
	}
}

func TestServeBundleUsesPublishedCatalogBeforeProfilePath(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	profileDir := filepath.Join(dir, "external-profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha","casePath":"runnable/case-alpha.json"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(profileDir, "runnable", "case-alpha.json"), `{"id":"case.alpha","request":{"method":"GET","path":"/v1/items"},"assertions":{"expectedStatusCodes":[200]}}`)

	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if _, err := publishProfileBundleToStore(ctx, s, profileDir, storePath, false, false); err != nil {
		t.Fatalf("publish profile: %v", err)
	}
	sourceBundle, err := profile.Load(profileDir)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	catalog := profilecatalog.FromBundle(sourceBundle, time.Now().UTC())
	catalog.APICases[0].CasePath = "store/case-alpha.json"
	if err := s.ReplaceProfileCatalog(ctx, catalog); err != nil {
		t.Fatalf("replace catalog: %v", err)
	}

	bundle, err := serveBundle(ctx, s)
	if err != nil {
		t.Fatalf("serve bundle: %v", err)
	}
	if len(bundle.APICases) != 1 || bundle.APICases[0].CasePath != "store/case-alpha.json" {
		t.Fatalf("serve bundle api cases = %#v", bundle.APICases)
	}
}

func TestServeHandlerPublishesProfilePathIntoStoreBeforeServing(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := filepath.Join(t.TempDir(), "external-profile")
	writeWorkflowProfile(t, profileDir)

	handler, cleanup, err := serveHandlerFromArgs([]string{
		"--profile", profileDir,
		"--store", "sqlite://" + storePath,
	})
	if err != nil {
		t.Fatalf("build serve handler with profile path: %v", err)
	}
	defer cleanup()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/interface-nodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("interface nodes status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Source struct {
			ID string `json:"id"`
		} `json:"source"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode interface nodes payload: %v\n%s", err, rec.Body.String())
	}
	if payload.Source.ID != "sample" || len(payload.Items) != 1 || payload.Items[0].ID != "node.alpha" {
		t.Fatalf("interface nodes payload = %#v", payload)
	}
	if got := sqliteScalar(t, storePath, "select value from kv where key = 'active_profile_id';"); got != "sample" {
		t.Fatalf("active profile id = %q", got)
	}
	if got := sqliteScalar(t, storePath, "select count(*) from config_read_model where profile_id = 'sample';"); got == "0" {
		t.Fatalf("expected serve --profile to publish read models")
	}
}

func TestServeHandlerPublishesInstalledProfileIDBeforeServing(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	profileHome := filepath.Join(t.TempDir(), "profile-home")
	sourceDir := filepath.Join(t.TempDir(), "external-profile")
	writeWorkflowProfile(t, sourceDir)
	runCLI(t, "profile", "install", "--from", sourceDir, "--profile-home", profileHome)

	handler, cleanup, err := serveHandlerFromArgs([]string{
		"--profile", "sample",
		"--profile-home", profileHome,
		"--store", "sqlite://" + storePath,
	})
	if err != nil {
		t.Fatalf("build serve handler with installed profile id: %v", err)
	}
	defer cleanup()

	profiles := httptest.NewRecorder()
	handler.ServeHTTP(profiles, httptest.NewRequest(http.MethodGet, "/api/profile/installed", nil))
	if profiles.Code != http.StatusOK || !strings.Contains(profiles.Body.String(), profileHome) {
		t.Fatalf("installed profiles response = %d %s", profiles.Code, profiles.Body.String())
	}
	if got := sqliteScalar(t, storePath, "select value from kv where key = 'active_profile_id';"); got != "sample" {
		t.Fatalf("active profile id = %q", got)
	}
}
