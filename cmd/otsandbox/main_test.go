package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"open-test-sandbox/internal/apicase"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/profilecatalog"
	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/postgres"
	"open-test-sandbox/internal/store/schema"
	"open-test-sandbox/internal/store/sqlite"
	"open-test-sandbox/internal/store/sqlstore"
)

func TestTopLevelHelpShowsStoreFlagNotLegacyStoreURL(t *testing.T) {
	out := runCLI(t)
	if !strings.Contains(out, "--store NAME_OR_DSN") {
		t.Fatalf("top-level help should show Store-first flag, got %q", out)
	}
	for _, want := range []string{"--clean-docker-state", "--clean-docker-images", "--allow-destructive-docker-cleanup"} {
		if !strings.Contains(out, want) {
			t.Fatalf("top-level help missing restore cleanup flag %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "--store-url PATH") {
		t.Fatalf("top-level help should not promote deprecated store-url path flag:\n%s", out)
	}
}

func TestStoreUpgradeAndStatusCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")

	initial := runCLI(t, "store", "status", "--store", "sqlite://"+dbPath)
	if !strings.Contains(initial, "Version: 0") || !strings.Contains(initial, fmt.Sprintf("Pending: %d", schema.CurrentVersion)) {
		t.Fatalf("initial status output = %q", initial)
	}

	upgraded := runCLI(t, "store", "upgrade", "--store", "sqlite://"+dbPath)
	if !strings.Contains(upgraded, fmt.Sprintf("Upgraded store schema to version %d", schema.CurrentVersion)) {
		t.Fatalf("upgrade output = %q", upgraded)
	}

	current := runCLI(t, "store", "status", "--store", "sqlite://"+dbPath)
	if !strings.Contains(current, fmt.Sprintf("Version: %d", schema.CurrentVersion)) || !strings.Contains(current, "Pending: 0") {
		t.Fatalf("current status output = %q", current)
	}
}

func TestStoreDDLCommandPrintsPostgreSQLSchema(t *testing.T) {
	out := runStoreCommand(t, "ddl", "--backend", "postgres")
	if !strings.Contains(out, "create table if not exists schema_versions") {
		t.Fatalf("postgres ddl should include schema_versions table:\n%s", out)
	}
	if !strings.Contains(out, "jsonb") {
		t.Fatalf("postgres ddl should use PostgreSQL jsonb columns:\n%s", out)
	}
}

func TestStoreConfigCommandsManageActivePostgresStore(t *testing.T) {
	configHome := t.TempDir()
	env := []string{"OTSANDBOX_CONFIG_HOME=" + configHome}
	dsn := "postgres://user:secret@example.com:5432/otsandbox_local?sslmode=disable"

	setOut := runCLIWithEnv(t, env, "store", "config", "set", "local-personal", "--url", dsn)
	if !strings.Contains(setOut, "Configured store: local-personal") || !strings.Contains(setOut, "Backend: postgres") {
		t.Fatalf("store config set output = %q", setOut)
	}

	listOut := runCLIWithEnv(t, env, "store", "config", "list")
	if !strings.Contains(listOut, "local-personal") || !strings.Contains(listOut, "postgres://user:xxxxx@example.com:5432/otsandbox_local?sslmode=disable") {
		t.Fatalf("store config list output = %q", listOut)
	}
	listJSONOut := runCLIWithEnv(t, env, "store", "config", "list", "--json")
	if strings.Contains(listJSONOut, "secret") || !strings.Contains(listJSONOut, "postgres://user:xxxxx@example.com:5432/otsandbox_local?sslmode=disable") {
		t.Fatalf("store config list json should mask credentials = %q", listJSONOut)
	}

	useOut := runCLIWithEnv(t, env, "store", "use", "local-personal")
	if !strings.Contains(useOut, "Active store: local-personal") {
		t.Fatalf("store use output = %q", useOut)
	}

	currentOut := runCLIWithEnv(t, env, "store", "current", "--json")
	var current struct {
		OK      bool   `json:"ok"`
		Name    string `json:"name"`
		Backend string `json:"backend"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal([]byte(currentOut), &current); err != nil {
		t.Fatalf("decode current store: %v\n%s", err, currentOut)
	}
	if !current.OK || current.Name != "local-personal" || current.Backend != "postgres" || current.URL != "postgres://user:xxxxx@example.com:5432/otsandbox_local?sslmode=disable" {
		t.Fatalf("current store = %#v", current)
	}
	if strings.Contains(currentOut, "secret") {
		t.Fatalf("store current json should mask credentials = %q", currentOut)
	}
}

func TestStoreStatusAndUpgradeRequireActiveStore(t *testing.T) {
	env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}
	for _, command := range []string{"status", "upgrade"} {
		out := runCLIFailsWithEnv(t, env, "store", command)
		if !strings.Contains(out, "no active store configured") || !strings.Contains(out, "store config set NAME --url postgres://") {
			t.Fatalf("store %s should guide active PostgreSQL Store setup, got %q", command, out)
		}
	}
}

func TestStoreStatusSupportsPostgresURLs(t *testing.T) {
	withPostgresSchemaStatus(t, func(_ context.Context, cfg postgres.Config) (postgres.SchemaStatusResult, error) {
		return postgres.SchemaStatusResult{URL: cfg.URL, CurrentVersion: 0, TargetVersion: sqlstore.CurrentSchemaVersion}, nil
	})

	out := runStoreCommand(t, "status", "--store-url", "postgres://localhost/open_test_sandbox")
	if !strings.Contains(out, "Store: postgres") || !strings.Contains(out, "Version: 0") || !strings.Contains(out, fmt.Sprintf("Pending: %d", sqlstore.CurrentSchemaVersion)) {
		t.Fatalf("postgres status output = %q", out)
	}
}

func TestStoreStatusCanUseNamedPostgresStore(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("OTSANDBOX_CONFIG_HOME", configHome)
	withPostgresSchemaStatus(t, func(_ context.Context, cfg postgres.Config) (postgres.SchemaStatusResult, error) {
		return postgres.SchemaStatusResult{URL: cfg.URL, CurrentVersion: 0, TargetVersion: sqlstore.CurrentSchemaVersion}, nil
	})
	runStoreCommand(t, "config", "set", "team-verified", "--url", "postgres://user:secret@example.com:5432/team_verified?sslmode=disable")

	out := runStoreCommand(t, "status", "--store", "team-verified")
	if !strings.Contains(out, "Store: postgres") || !strings.Contains(out, "team_verified") || strings.Contains(out, "secret") {
		t.Fatalf("named postgres status output = %q", out)
	}
}

func TestStoreReferenceResolutionKeepsLocalAndRemotePostgresCommandShape(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("OTSANDBOX_CONFIG_HOME", configHome)
	cfg := storeConfigFile{Stores: map[string]storeConfigEntry{}}
	local, err := newStoreConfigEntry("local-personal", "postgres://tester:secret@localhost:5432/local_personal?sslmode=disable")
	if err != nil {
		t.Fatalf("local config entry: %v", err)
	}
	remote, err := newStoreConfigEntry("team-verified", "postgres://tester:secret@pg.example.com:5432/team_verified?sslmode=require")
	if err != nil {
		t.Fatalf("remote config entry: %v", err)
	}
	cfg.Stores[local.Name] = local
	cfg.Stores[remote.Name] = remote
	cfg.Active = local.Name
	if err := saveStoreConfig(cfg); err != nil {
		t.Fatalf("save store config: %v", err)
	}

	localURL, err := resolveStoreReference("local-personal", "")
	if err != nil {
		t.Fatalf("resolve local store: %v", err)
	}
	remoteURL, err := resolveStoreReference("team-verified", "")
	if err != nil {
		t.Fatalf("resolve remote store: %v", err)
	}
	activeURL, err := resolveStoreReference("", "")
	if err != nil {
		t.Fatalf("resolve active store: %v", err)
	}
	if localURL != local.URL || remoteURL != remote.URL || activeURL != local.URL {
		t.Fatalf("resolved urls local=%q remote=%q active=%q", localURL, remoteURL, activeURL)
	}
}

func TestLegacyStoreURLPathIsExplicitSQLiteCompatibility(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	resolved, err := resolveRequiredStoreReference("", storePath)
	if err != nil {
		t.Fatalf("resolve legacy store url path: %v", err)
	}
	if resolved != "sqlite://"+storePath {
		t.Fatalf("legacy store url path = %q want sqlite://%s", resolved, storePath)
	}
}

func TestDailyStoreReferenceRejectsLegacySQLiteStoreURL(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	for _, legacyStoreURL := range []string{storePath, "sqlite://" + storePath} {
		_, err := resolveRequiredDailyStoreReference("", legacyStoreURL)
		if err == nil {
			t.Fatalf("daily Store reference should reject legacy SQLite store URL %q", legacyStoreURL)
		}
		for _, want := range []string{"--store-url", "daily commands require PostgreSQL Store", "SQLite", "postgres://"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("daily Store reference error missing %q: %v", want, err)
			}
		}
	}
}

func TestDailyStoreReferenceRejectsNamedSQLiteConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OTSANDBOX_CONFIG_HOME", filepath.Join(dir, "config"))
	if err := saveStoreConfig(storeConfigFile{
		Stores: map[string]storeConfigEntry{
			"legacy-local": {Name: "legacy-local", URL: "sqlite://" + filepath.Join(dir, "store.sqlite"), Backend: "sqlite"},
		},
	}); err != nil {
		t.Fatalf("save store config: %v", err)
	}

	_, err := resolveRequiredDailyStoreReference("legacy-local", "")
	if err == nil {
		t.Fatal("daily Store reference should reject named SQLite config")
	}
	for _, want := range []string{`Store config "legacy-local"`, "daily commands require PostgreSQL Store", "SQLite", "postgres://"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("daily Store reference error missing %q: %v", want, err)
		}
	}
}

func TestDailyStoreReferenceKeepsDirectSQLiteStoreFlagAsExplicitCompatibility(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	for _, tc := range []struct {
		storeRef string
		want     string
	}{
		{storeRef: "sqlite://" + storePath, want: "sqlite://" + storePath},
		{storeRef: "file://" + storePath, want: "file://" + storePath},
	} {
		resolved, err := resolveRequiredDailyStoreReference(tc.storeRef, "")
		if err != nil {
			t.Fatalf("daily Store reference should keep explicit SQLite compatibility store flag %q: %v", tc.storeRef, err)
		}
		if resolved != tc.want {
			t.Fatalf("direct SQLite compatibility store flag = %q want %q", resolved, tc.want)
		}
	}
}

func TestEnvironmentCommandsRejectActiveSQLiteStore(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "register",
			args: []string{"environment", "register", "--id", "env.legacy"},
		},
		{
			name: "discover",
			args: []string{"environment", "discover", "--json"},
		},
		{
			name: "inspect",
			args: []string{"environment", "inspect", "env.legacy"},
		},
		{
			name: "bootstrap",
			args: []string{"environment", "bootstrap", "env.legacy"},
		},
		{
			name: "verify",
			args: []string{"environment", "verify", "--run", "run.legacy", "--status", "passed", "env.legacy"},
		},
		{
			name: "publish verified",
			args: []string{"environment", "publish-verified", "env.legacy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("OTSANDBOX_CONFIG_HOME", filepath.Join(dir, "config"))
			if err := saveStoreConfig(storeConfigFile{
				Active: "legacy-local",
				Stores: map[string]storeConfigEntry{
					"legacy-local": {Name: "legacy-local", URL: "sqlite://" + filepath.Join(dir, "store.sqlite"), Backend: "sqlite"},
				},
			}); err != nil {
				t.Fatalf("save store config: %v", err)
			}

			out := runCLIFails(t, tt.args...)
			for _, want := range []string{"daily commands require PostgreSQL Store", "SQLite", "postgres://"} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s output missing %q: %q", tt.name, want, out)
				}
			}
		})
	}
}

func TestEnvironmentRegisterRequiresVerificationWorkflow(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	out := runCLIFails(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.no-workflow",
		"--repo", "entry-gateway=https://example.com/team/entry-gateway.git",
	)
	if !strings.Contains(out, "--verification-workflow") {
		t.Fatalf("register without verification workflow output = %q", out)
	}
}

func TestEnvironmentRegisterRejectsOversizedDefinitionMetadata(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	large := strings.Repeat("x", store.EnvironmentDefinitionMaxBytes)
	out := runCLIFails(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.too-large",
		"--description", large,
		"--verification-workflow", "workflow.core-10",
	)
	if !strings.Contains(out, "maximum is 65536 bytes") || !strings.Contains(out, "not code, images, logs, evidence payloads, or large files") {
		t.Fatalf("oversized environment metadata output = %q", out)
	}
}

func TestEnvironmentCommandsGateVerifiedDiscovery(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	storeRef := "sqlite://" + storePath

	registerOut := runCLI(t, "environment", "register",
		"--store", storeRef,
		"--id", "env.team.verified",
		"--display-name", "Team Verified Environment",
		"--description", "Accepted local Docker environment",
		"--service", "entry-gateway",
		"--repo", "entry-gateway=../entry-gateway",
		"--branch", "entry-gateway=main",
		"--repo-ref", "entry-gateway=v1.2.3",
		"--checkout", "entry-gateway=/tmp/entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--start-command", "docker compose up -d",
		"--health-url", "http://127.0.0.1:18080/health",
		"--verification-workflow", "workflow.core-10",
		"--json",
	)
	var registered struct {
		OK          bool `json:"ok"`
		Environment struct {
			ID                     string           `json:"id"`
			Status                 string           `json:"status"`
			Verified               bool             `json:"verified"`
			VerificationWorkflowID string           `json:"verificationWorkflowId"`
			Services               []map[string]any `json:"services"`
			Repos                  map[string]any   `json:"repos"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(registerOut), &registered); err != nil {
		t.Fatalf("decode environment register json: %v\n%s", err, registerOut)
	}
	if !registered.OK || registered.Environment.ID != "env.team.verified" || registered.Environment.Status != "draft" || registered.Environment.Verified {
		t.Fatalf("registered environment = %#v", registered.Environment)
	}
	if registered.Environment.VerificationWorkflowID != "workflow.core-10" || len(registered.Environment.Services) != 1 || registered.Environment.Repos["entry-gateway"] == nil {
		t.Fatalf("registered environment catalog fields = %#v", registered.Environment)
	}

	discoverOut := runCLI(t, "environment", "discover", "--store", storeRef, "--json")
	var discovered struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(discoverOut), &discovered); err != nil {
		t.Fatalf("decode discover json: %v\n%s", err, discoverOut)
	}
	if discovered.Count != 0 {
		t.Fatalf("unverified environment should stay out of default discovery: %#v", discovered)
	}

	discoverAllOut := runCLI(t, "environment", "discover", "--store", storeRef, "--all", "--json")
	var discoveredAll struct {
		Count int `json:"count"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(discoverAllOut), &discoveredAll); err != nil {
		t.Fatalf("decode discover all json: %v\n%s", err, discoverAllOut)
	}
	if discoveredAll.Count != 1 || discoveredAll.Items[0].ID != "env.team.verified" {
		t.Fatalf("discover all = %#v", discoveredAll)
	}

	publishDenied := runCLIFails(t, "environment", "publish-verified", "--store", storeRef, "env.team.verified")
	if !strings.Contains(publishDenied, "not publishable") {
		t.Fatalf("publish should require complete verification evidence: %q", publishDenied)
	}

	verifyOut := runCLI(t, "environment", "verify",
		"--store", storeRef,
		"--run", "run.core-10",
		"--status", "passed",
		"--evidence-complete",
		"--topology-complete",
		"--json",
		"env.team.verified",
	)
	var verified struct {
		Environment struct {
			Status                 string `json:"status"`
			LastVerificationRunID  string `json:"lastVerificationRunId"`
			LastVerificationStatus string `json:"lastVerificationStatus"`
			EvidenceComplete       bool   `json:"evidenceComplete"`
			TopologyComplete       bool   `json:"topologyComplete"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(verifyOut), &verified); err != nil {
		t.Fatalf("decode verify json: %v\n%s", err, verifyOut)
	}
	if verified.Environment.Status != "verified-ready" || verified.Environment.LastVerificationRunID != "run.core-10" || verified.Environment.LastVerificationStatus != "passed" || !verified.Environment.EvidenceComplete || !verified.Environment.TopologyComplete {
		t.Fatalf("verified environment = %#v", verified.Environment)
	}

	missingArtifacts := runCLIFails(t, "environment", "publish-verified", "--store", storeRef, "env.team.verified")
	if !strings.Contains(missingArtifacts, "was not found in Store") {
		t.Fatalf("publish should require indexed verification artifacts: %q", missingArtifacts)
	}
	seedEnvironmentVerificationArtifacts(t, storeRef, "run.core-10")

	publishOut := runCLI(t, "environment", "publish-verified", "--store", storeRef, "--json", "env.team.verified")
	var published struct {
		Environment struct {
			Status   string `json:"status"`
			Verified bool   `json:"verified"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(publishOut), &published); err != nil {
		t.Fatalf("decode publish json: %v\n%s", err, publishOut)
	}
	if published.Environment.Status != "verified" || !published.Environment.Verified {
		t.Fatalf("published environment = %#v", published.Environment)
	}

	discoverVerifiedOut := runCLI(t, "environment", "discover", "--store", storeRef, "--json")
	var discoveredVerified struct {
		Count int `json:"count"`
		Items []struct {
			ID       string `json:"id"`
			Verified bool   `json:"verified"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(discoverVerifiedOut), &discoveredVerified); err != nil {
		t.Fatalf("decode verified discover json: %v\n%s", err, discoverVerifiedOut)
	}
	if discoveredVerified.Count != 1 || discoveredVerified.Items[0].ID != "env.team.verified" || !discoveredVerified.Items[0].Verified {
		t.Fatalf("verified discovery = %#v", discoveredVerified)
	}

	bootstrapOut := runCLI(t, "environment", "bootstrap", "--store", storeRef, "--json", "env.team.verified")
	var bootstrap struct {
		Plan struct {
			VerificationWorkflow string         `json:"verificationWorkflow"`
			Repos                map[string]any `json:"repos"`
			HealthChecks         []any          `json:"healthChecks"`
			Restore              struct {
				PauseBeforeHeavyValidation bool `json:"pauseBeforeHeavyValidation"`
				Docker                     struct {
					Action   string     `json:"action"`
					Commands [][]string `json:"commands"`
				} `json:"docker"`
			} `json:"restore"`
			Steps []struct {
				Kind string `json:"kind"`
			} `json:"steps"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(bootstrapOut), &bootstrap); err != nil {
		t.Fatalf("decode bootstrap json: %v\n%s", err, bootstrapOut)
	}
	if bootstrap.Plan.VerificationWorkflow != "workflow.core-10" || bootstrap.Plan.Repos["entry-gateway"] == nil || len(bootstrap.Plan.HealthChecks) != 1 {
		t.Fatalf("bootstrap plan = %#v", bootstrap.Plan)
	}
	if repo, ok := bootstrap.Plan.Repos["entry-gateway"].(map[string]any); !ok || repo["ref"] != "v1.2.3" {
		t.Fatalf("bootstrap repo ref = %#v", bootstrap.Plan.Repos["entry-gateway"])
	}
	if !bootstrap.Plan.Restore.PauseBeforeHeavyValidation || bootstrap.Plan.Restore.Docker.Action != "docker-compose" || len(bootstrap.Plan.Restore.Docker.Commands) != 3 {
		t.Fatalf("bootstrap restore plan = %#v", bootstrap.Plan.Restore)
	}
	if len(bootstrap.Plan.Steps) != 4 || bootstrap.Plan.Steps[0].Kind != "repository" || bootstrap.Plan.Steps[1].Kind != "docker" {
		t.Fatalf("bootstrap executable steps = %#v", bootstrap.Plan.Steps)
	}
}

func TestWorkflowAcceptanceCLIStartsAndReadsAsyncReport(t *testing.T) {
	var startPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/cases/batch-runs":
			if err := json.NewDecoder(r.Body).Decode(&startPayload); err != nil {
				t.Fatalf("decode start payload: %v", err)
			}
			writeTestJSON(t, w, http.StatusAccepted, map[string]any{
				"ok":         true,
				"batchRunId": "batch.acceptance.001",
				"requestId":  "acceptance-001",
				"workflowId": "workflow.core-10",
				"status":     "running",
				"total":      10,
				"reportUrl":  "/api/cases/batch-runs/batch.acceptance.001",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/cases/batch-runs/batch.acceptance.001":
			writeTestJSON(t, w, http.StatusOK, map[string]any{
				"ok":         true,
				"batchRunId": "batch.acceptance.001",
				"workflowId": "workflow.core-10",
				"status":     "passed",
				"total":      10,
				"acceptance": map[string]any{
					"ok":               true,
					"templateId":       "environment.workflow.skywalking.v1",
					"workflowId":       "workflow.core-10",
					"expectedSteps":    10,
					"completedSteps":   10,
					"passedSteps":      10,
					"failedSteps":      0,
					"topologyProvider": "skywalking",
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	startOut := runCLI(t, "workflow", "acceptance", "start",
		"--server-url", server.URL,
		"--workflow", "workflow.core-10",
		"--request-id", "acceptance-001",
		"--base-url", "http://127.0.0.1:18080",
		"--timeout-seconds", "30",
		"--json",
	)
	var started struct {
		OK         bool   `json:"ok"`
		BatchRunID string `json:"batchRunId"`
		WorkflowID string `json:"workflowId"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal([]byte(startOut), &started); err != nil {
		t.Fatalf("decode workflow acceptance start: %v\n%s", err, startOut)
	}
	if !started.OK || started.BatchRunID != "batch.acceptance.001" || started.WorkflowID != "workflow.core-10" || started.Status != "running" {
		t.Fatalf("workflow acceptance start = %#v", started)
	}
	if startPayload["workflowId"] != "workflow.core-10" || startPayload["requestId"] != "acceptance-001" || startPayload["baseUrl"] != "http://127.0.0.1:18080" || startPayload["timeoutSeconds"] != float64(30) {
		t.Fatalf("workflow acceptance start payload = %#v", startPayload)
	}

	reportOut := runCLI(t, "workflow", "acceptance", "report",
		"--server-url", server.URL,
		"--run", "batch.acceptance.001",
		"--json",
	)
	var report struct {
		Acceptance struct {
			OK               bool   `json:"ok"`
			TemplateID       string `json:"templateId"`
			TopologyProvider string `json:"topologyProvider"`
		} `json:"acceptance"`
	}
	if err := json.Unmarshal([]byte(reportOut), &report); err != nil {
		t.Fatalf("decode workflow acceptance report: %v\n%s", err, reportOut)
	}
	if !report.Acceptance.OK || report.Acceptance.TemplateID != "environment.workflow.skywalking.v1" || report.Acceptance.TopologyProvider != "skywalking" {
		t.Fatalf("workflow acceptance report = %#v", report.Acceptance)
	}
}

func TestEnvironmentAcceptanceCLIStartsAndReadsAsyncReport(t *testing.T) {
	var startPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments/env.team/acceptance-runs":
			if err := json.NewDecoder(r.Body).Decode(&startPayload); err != nil {
				t.Fatalf("decode environment start payload: %v", err)
			}
			writeTestJSON(t, w, http.StatusAccepted, map[string]any{
				"ok":            true,
				"environmentId": "env.team",
				"batchRunId":    "batch.env.acceptance.001",
				"requestId":     "env-acceptance-001",
				"workflowId":    "workflow.core-10",
				"status":        "running",
				"reportUrl":     "/api/environments/env.team/acceptance-runs/batch.env.acceptance.001",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/env.team/acceptance-runs/batch.env.acceptance.001":
			writeTestJSON(t, w, http.StatusOK, map[string]any{
				"ok":            true,
				"environmentId": "env.team",
				"batchRunId":    "batch.env.acceptance.001",
				"workflowId":    "workflow.core-10",
				"status":        "passed",
				"acceptance": map[string]any{
					"ok":               true,
					"templateId":       "environment.workflow.skywalking.v1",
					"workflowId":       "workflow.core-10",
					"topologyProvider": "skywalking",
					"healthSummary":    map[string]any{"total": 1, "passed": 1, "failed": 0},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	startOut := runCLI(t, "environment", "acceptance", "start",
		"--server-url", server.URL,
		"--request-id", "env-acceptance-001",
		"--base-url", "http://127.0.0.1:18080",
		"--json",
		"env.team",
	)
	var started struct {
		OK            bool   `json:"ok"`
		EnvironmentID string `json:"environmentId"`
		BatchRunID    string `json:"batchRunId"`
		WorkflowID    string `json:"workflowId"`
	}
	if err := json.Unmarshal([]byte(startOut), &started); err != nil {
		t.Fatalf("decode environment acceptance start: %v\n%s", err, startOut)
	}
	if !started.OK || started.EnvironmentID != "env.team" || started.BatchRunID != "batch.env.acceptance.001" || started.WorkflowID != "workflow.core-10" {
		t.Fatalf("environment acceptance start = %#v", started)
	}
	if startPayload["requestId"] != "env-acceptance-001" || startPayload["baseUrl"] != "http://127.0.0.1:18080" {
		t.Fatalf("environment acceptance start payload = %#v", startPayload)
	}

	reportOut := runCLI(t, "environment", "acceptance", "report",
		"--server-url", server.URL,
		"--run", "batch.env.acceptance.001",
		"--json",
		"env.team",
	)
	var report struct {
		Acceptance struct {
			OK            bool `json:"ok"`
			HealthSummary struct {
				Total  int `json:"total"`
				Passed int `json:"passed"`
			} `json:"healthSummary"`
		} `json:"acceptance"`
	}
	if err := json.Unmarshal([]byte(reportOut), &report); err != nil {
		t.Fatalf("decode environment acceptance report: %v\n%s", err, reportOut)
	}
	if !report.Acceptance.OK || report.Acceptance.HealthSummary.Total != 1 || report.Acceptance.HealthSummary.Passed != 1 {
		t.Fatalf("environment acceptance report = %#v", report.Acceptance)
	}
}

func TestEnvironmentCommandsUseNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-environment-pg")
	runID := "run.core-10." + time.Now().UTC().Format("20060102150405.000000000")

	registerOut := runCLI(t, "environment", "register",
		"--id", "env.team.pg",
		"--display-name", "Team PostgreSQL Environment",
		"--description", "Accepted local Docker environment",
		"--service", "entry-gateway",
		"--repo", "entry-gateway=../entry-gateway",
		"--branch", "entry-gateway=main",
		"--checkout", "entry-gateway=/tmp/entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--start-command", "docker compose up -d",
		"--health-url", "http://127.0.0.1:18080/health",
		"--verification-workflow", "workflow.core-10",
		"--json",
	)
	var registered struct {
		OK          bool `json:"ok"`
		Environment struct {
			ID                     string         `json:"id"`
			Status                 string         `json:"status"`
			Verified               bool           `json:"verified"`
			VerificationWorkflowID string         `json:"verificationWorkflowId"`
			Repos                  map[string]any `json:"repos"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(registerOut), &registered); err != nil {
		t.Fatalf("decode environment register json: %v\n%s", err, registerOut)
	}
	if !registered.OK || registered.Environment.ID != "env.team.pg" || registered.Environment.Status != "draft" || registered.Environment.Verified {
		t.Fatalf("registered PostgreSQL environment = %#v", registered.Environment)
	}
	if registered.Environment.VerificationWorkflowID != "workflow.core-10" || registered.Environment.Repos["entry-gateway"] == nil {
		t.Fatalf("registered PostgreSQL environment catalog fields = %#v", registered.Environment)
	}

	discoverOut := runCLI(t, "environment", "discover", "--json")
	var discovered struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(discoverOut), &discovered); err != nil {
		t.Fatalf("decode discover json: %v\n%s", err, discoverOut)
	}
	if discovered.Count != 0 {
		t.Fatalf("unverified PostgreSQL environment should stay out of default discovery: %#v", discovered)
	}

	publishDenied := runCLIFails(t, "environment", "publish-verified", "env.team.pg")
	if !strings.Contains(publishDenied, "not publishable") {
		t.Fatalf("publish should require complete verification evidence: %q", publishDenied)
	}

	verifyOut := runCLI(t, "environment", "verify",
		"--run", runID,
		"--status", "passed",
		"--evidence-complete",
		"--topology-complete",
		"--json",
		"env.team.pg",
	)
	var verified struct {
		Environment struct {
			Status                 string `json:"status"`
			LastVerificationRunID  string `json:"lastVerificationRunId"`
			LastVerificationStatus string `json:"lastVerificationStatus"`
			EvidenceComplete       bool   `json:"evidenceComplete"`
			TopologyComplete       bool   `json:"topologyComplete"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(verifyOut), &verified); err != nil {
		t.Fatalf("decode verify json: %v\n%s", err, verifyOut)
	}
	if verified.Environment.Status != "verified-ready" || verified.Environment.LastVerificationRunID != runID || verified.Environment.LastVerificationStatus != "passed" || !verified.Environment.EvidenceComplete || !verified.Environment.TopologyComplete {
		t.Fatalf("verified PostgreSQL environment = %#v", verified.Environment)
	}

	missingArtifacts := runCLIFails(t, "environment", "publish-verified", "env.team.pg")
	if !strings.Contains(missingArtifacts, "was not found in Store") {
		t.Fatalf("publish should require indexed PostgreSQL verification artifacts: %q", missingArtifacts)
	}
	seedEnvironmentVerificationArtifacts(t, storeRef, runID)

	publishOut := runCLI(t, "environment", "publish-verified", "--json", "env.team.pg")
	var published struct {
		Environment struct {
			Status   string `json:"status"`
			Verified bool   `json:"verified"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(publishOut), &published); err != nil {
		t.Fatalf("decode publish json: %v\n%s", err, publishOut)
	}
	if published.Environment.Status != "verified" || !published.Environment.Verified {
		t.Fatalf("published PostgreSQL environment = %#v", published.Environment)
	}

	discoverVerifiedOut := runCLI(t, "environment", "discover", "--json")
	var discoveredVerified struct {
		Count int `json:"count"`
		Items []struct {
			ID       string `json:"id"`
			Verified bool   `json:"verified"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(discoverVerifiedOut), &discoveredVerified); err != nil {
		t.Fatalf("decode verified discover json: %v\n%s", err, discoverVerifiedOut)
	}
	if discoveredVerified.Count != 1 || discoveredVerified.Items[0].ID != "env.team.pg" || !discoveredVerified.Items[0].Verified {
		t.Fatalf("verified PostgreSQL discovery = %#v", discoveredVerified)
	}

	bootstrapOut := runCLI(t, "environment", "bootstrap", "--json", "env.team.pg")
	var bootstrap struct {
		Plan struct {
			VerificationWorkflow string         `json:"verificationWorkflow"`
			Repos                map[string]any `json:"repos"`
			HealthChecks         []any          `json:"healthChecks"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(bootstrapOut), &bootstrap); err != nil {
		t.Fatalf("decode bootstrap json: %v\n%s", err, bootstrapOut)
	}
	if bootstrap.Plan.VerificationWorkflow != "workflow.core-10" || bootstrap.Plan.Repos["entry-gateway"] == nil || len(bootstrap.Plan.HealthChecks) != 1 {
		t.Fatalf("PostgreSQL bootstrap plan = %#v", bootstrap.Plan)
	}
}

func TestEnvironmentRestoreClonesRemoteReposForVerifiedWorkflow(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	remoteRepo := createBareGitRepo(t, "main")
	workspace := filepath.Join(t.TempDir(), "workspace")
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.restore",
		"--repo", "entry-gateway="+remoteRepo,
		"--branch", "entry-gateway=main",
		"--checkout", "entry-gateway=services/entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--start-command", "docker compose up -d",
		"--health-url", healthServer.URL+"/health",
		"--verification-workflow", "workflow.core-10",
	)
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")

	dryRunOut := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--json", "env.restore")
	var dryRun struct {
		OK                   bool   `json:"ok"`
		Executed             bool   `json:"executed"`
		VerificationWorkflow string `json:"verificationWorkflow"`
		Repos                []struct {
			ServiceID string   `json:"serviceId"`
			Action    string   `json:"action"`
			Checkout  string   `json:"checkout"`
			Command   []string `json:"command"`
		} `json:"repos"`
		Docker struct {
			OK       bool       `json:"ok"`
			Action   string     `json:"action"`
			Commands [][]string `json:"commands"`
		} `json:"docker"`
		Preflight struct {
			OK    bool `json:"ok"`
			Tools []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
				OK       bool   `json:"ok"`
			} `json:"tools"`
			HeavySteps []string `json:"heavySteps"`
		} `json:"preflight"`
		Readiness struct {
			OK                         bool `json:"ok"`
			PauseBeforeHeavyValidation bool `json:"pauseBeforeHeavyValidation"`
			Items                      []struct {
				Name   string `json:"name"`
				OK     bool   `json:"ok"`
				Detail string `json:"detail"`
			} `json:"items"`
		} `json:"readiness"`
		NextActions []string `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(dryRunOut), &dryRun); err != nil {
		t.Fatalf("decode restore dry-run json: %v\n%s", err, dryRunOut)
	}
	expectedCheckout := filepath.Join(workspace, "services", "entry-gateway")
	if !dryRun.OK || dryRun.Executed || dryRun.VerificationWorkflow != "workflow.core-10" || len(dryRun.Repos) != 1 {
		t.Fatalf("restore dry-run report = %#v", dryRun)
	}
	if dryRun.Repos[0].ServiceID != "entry-gateway" || dryRun.Repos[0].Action != "clone" || dryRun.Repos[0].Checkout != expectedCheckout || strings.Join(dryRun.Repos[0].Command, " ") == "" {
		t.Fatalf("restore dry-run repo = %#v", dryRun.Repos[0])
	}
	if !dryRun.Docker.OK || dryRun.Docker.Action != "plan-docker-compose" || len(dryRun.Docker.Commands) != 3 {
		t.Fatalf("restore dry-run docker plan = %#v", dryRun.Docker)
	}
	if !dryRun.Preflight.OK || !restorePreflightHasTool(dryRun.Preflight.Tools, "git", true) || !restorePreflightHasTool(dryRun.Preflight.Tools, "docker", true) || !restorePreflightHasTool(dryRun.Preflight.Tools, "docker compose", true) || len(dryRun.Preflight.HeavySteps) == 0 {
		t.Fatalf("restore dry-run preflight = %#v", dryRun.Preflight)
	}
	if !dryRun.Readiness.OK || !dryRun.Readiness.PauseBeforeHeavyValidation || !restoreReadinessHasItem(dryRun.Readiness.Items, "service-repositories", true, "will be cloned") || !restoreReadinessHasItem(dryRun.Readiness.Items, "compose-services-and-middleware", true, "including middleware") || !restoreReadinessHasItem(dryRun.Readiness.Items, "health-probes", true, "1 Store-backed") || !restoreReadinessHasItem(dryRun.Readiness.Items, "operator-pause", true, "pause before") {
		t.Fatalf("restore dry-run readiness = %#v", dryRun.Readiness)
	}
	if len(dryRun.NextActions) == 0 || !strings.Contains(strings.Join(dryRun.NextActions, "\n"), "workflow.core-10") {
		t.Fatalf("restore dry-run should anchor next actions to verification workflow: %#v", dryRun.NextActions)
	}
	if _, err := os.Stat(expectedCheckout); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create checkout, stat err=%v", err)
	}

	executeOut := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.restore")
	var executed struct {
		OK        bool   `json:"ok"`
		RestoreID string `json:"restoreId"`
		Executed  bool   `json:"executed"`
		Repos     []struct {
			Action string `json:"action"`
			OK     bool   `json:"ok"`
		} `json:"repos"`
		Docker struct {
			OK           bool `json:"ok"`
			HealthChecks []struct {
				URL string `json:"url"`
				OK  bool   `json:"ok"`
			} `json:"healthChecks"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(executeOut), &executed); err != nil {
		t.Fatalf("decode restore execute json: %v\n%s", err, executeOut)
	}
	if !executed.OK || !executed.Executed || len(executed.Repos) != 1 || executed.Repos[0].Action != "clone" || !executed.Repos[0].OK {
		t.Fatalf("restore execute report = %#v", executed)
	}
	if !executed.Docker.OK || len(executed.Docker.HealthChecks) != 1 || !executed.Docker.HealthChecks[0].OK {
		t.Fatalf("restore execute docker report = %#v", executed.Docker)
	}
	dockerCalls, err := os.ReadFile(dockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	composePath := filepath.Join(workspace, "docker-compose.yml")
	for _, want := range []string{
		"compose -f " + composePath + " pull",
		"compose -f " + composePath + " build",
		"compose -f " + composePath + " up -d",
	} {
		if !strings.Contains(string(dockerCalls), want) {
			t.Fatalf("fake docker calls missing %q:\n%s", want, dockerCalls)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(expectedCheckout, "README.md")); err != nil || !strings.Contains(string(raw), "restore fixture") {
		t.Fatalf("restored checkout missing fixture file raw=%q err=%v", raw, err)
	}
	inspectOut := runCLI(t, "environment", "inspect", "--store", "sqlite://"+storePath, "--json", "env.restore")
	var inspected struct {
		Environment struct {
			Summary struct {
				LastRestore struct {
					ID                   string `json:"id"`
					OK                   bool   `json:"ok"`
					Executed             bool   `json:"executed"`
					Phase                string `json:"phase"`
					VerificationWorkflow string `json:"verificationWorkflow"`
					Docker               struct {
						Action       string `json:"action"`
						OK           bool   `json:"ok"`
						HealthChecks int    `json:"healthChecks"`
						HealthPassed int    `json:"healthPassed"`
					} `json:"docker"`
					Repositories []struct {
						ServiceID string `json:"serviceId"`
						Action    string `json:"action"`
						OK        bool   `json:"ok"`
					} `json:"repositories"`
					Readiness struct {
						OK          bool `json:"ok"`
						FailedItems []struct {
							Name string `json:"name"`
						} `json:"failedItems"`
					} `json:"readiness"`
				} `json:"lastRestore"`
				RestoreAttempts []struct {
					ID    string `json:"id"`
					Phase string `json:"phase"`
				} `json:"restoreAttempts"`
			} `json:"summary"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(inspectOut), &inspected); err != nil {
		t.Fatalf("decode restored environment inspect json: %v\n%s", err, inspectOut)
	}
	lastRestore := inspected.Environment.Summary.LastRestore
	if lastRestore.ID != executed.RestoreID || !lastRestore.OK || !lastRestore.Executed || lastRestore.Phase != "completed" || lastRestore.VerificationWorkflow != "workflow.core-10" || lastRestore.Docker.Action != "run-docker-compose" || !lastRestore.Docker.OK || lastRestore.Docker.HealthChecks != 1 || lastRestore.Docker.HealthPassed != 1 || len(lastRestore.Repositories) != 1 || lastRestore.Repositories[0].Action != "clone" || !lastRestore.Repositories[0].OK {
		t.Fatalf("persisted restore summary = %#v; executed restore id=%s", lastRestore, executed.RestoreID)
	}
	if !lastRestore.Readiness.OK || len(lastRestore.Readiness.FailedItems) != 0 {
		t.Fatalf("persisted readiness summary = %#v", lastRestore.Readiness)
	}
	attempts := inspected.Environment.Summary.RestoreAttempts
	if len(attempts) != 2 || attempts[0].ID == attempts[1].ID || attempts[1].ID != executed.RestoreID || attempts[1].Phase != "completed" {
		t.Fatalf("persisted restore attempts = %#v; executed restore id=%s", attempts, executed.RestoreID)
	}
}

func TestEnvironmentRestorePreflightReportsMissingGitForMissingCheckout(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeBin := t.TempDir()
	writeFile(t, filepath.Join(fakeBin, "docker"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(fakeBin, "docker"), 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin)
	report, err := buildEnvironmentRestoreReport(context.Background(), store.Environment{
		ID:                     "env.preflight",
		ReposJSON:              `{"entry-gateway":{"url":"https://example.com/team/entry-gateway.git","checkout":"entry-gateway"}}`,
		ComposeJSON:            `{"composeFile":"docker-compose.yml"}`,
		HealthChecksJSON:       `[]`,
		VerificationWorkflowID: "workflow.core-10",
	}, workspace, false, false, false, time.Second, environmentRestoreWorkflowOptions{}, environmentRestoreDockerCleanupOptions{})
	if err != nil {
		t.Fatalf("build restore preflight report: %v", err)
	}
	if report.OK || report.Preflight.OK || !restoreTypedPreflightHasTool(report.Preflight.Tools, "git", false) || !restoreTypedPreflightHasTool(report.Preflight.Tools, "docker", true) {
		t.Fatalf("missing git preflight report = %#v", report.Preflight)
	}
}

func TestEnvironmentRestoreRequiresRemoteGitSourcesForPostgreSQLOneClickEnvironment(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	report, err := buildEnvironmentRestoreReport(context.Background(), store.Environment{
		ID:                     "env.remote.sources",
		ReposJSON:              `{"llt":{"url":"/Users/zlh/codes/open-test-sandbox-llt-simulator","checkout":"llt"}}`,
		ComposeJSON:            `{"composeFile":"compose/docker-compose.yml","package":{"url":"/Users/zlh/codes/open-test-sandbox-validation","checkout":"."}}`,
		HealthChecksJSON:       `[{"kind":"url","url":"http://127.0.0.1:28080/health"}]`,
		VerificationWorkflowID: "workflow.core-10",
	}, workspace, false, false, false, time.Second, environmentRestoreWorkflowOptions{
		StoreURL: "postgres://tester@127.0.0.1:5432/otsandbox?sslmode=disable",
	}, environmentRestoreDockerCleanupOptions{})
	if err != nil {
		t.Fatalf("build restore remote source policy report: %v", err)
	}
	if report.OK || report.SourcePolicy.OK || !report.SourcePolicy.RemoteOnly || len(report.SourcePolicy.Violations) != 1 || report.Docker.Action != "skipped-due-to-source-policy" {
		t.Fatalf("remote source policy report = %#v", report)
	}
	if !strings.Contains(report.SourcePolicy.Violations[0], "service llt") {
		t.Fatalf("source policy should only reject service repositories, got %#v", report.SourcePolicy.Violations)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "remote-git-sources", false, "remote Git URL") {
		t.Fatalf("readiness should include remote source violation: %#v", report.Readiness.Items)
	}
}

func TestEnvironmentRestorePostgreSQLUsesStoreGeneratedStartupFiles(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeBin := t.TempDir()
	writeFile(t, filepath.Join(fakeBin, "git"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(fakeBin, "docker"), "#!/bin/sh\nif [ \"$1\" = compose ] && [ \"$2\" = version ]; then exit 0; fi\nexit 0\n")
	if err := os.Chmod(filepath.Join(fakeBin, "git"), 0o755); err != nil {
		t.Fatalf("chmod fake git: %v", err)
	}
	if err := os.Chmod(filepath.Join(fakeBin, "docker"), 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin)

	report, err := buildEnvironmentRestoreReport(context.Background(), store.Environment{
		ID:                     "env.pg.generated",
		ReposJSON:              `{"llt":{"url":"git@github.com:ztcshen/open-test-sandbox-llt-simulator.git","checkout":"llt"}}`,
		ComposeJSON:            `{"composeFile":"compose/docker-compose.yml","composeFiles":["compose/docker-compose.yml"],"generatedFiles":{"compose/docker-compose.yml":"services:\n  llt:\n    image: alpine:3.20\n"},"package":{"url":"/Users/zlh/codes/open-test-sandbox-validation","checkout":"."}}`,
		HealthChecksJSON:       `[{"kind":"url","url":"http://127.0.0.1:28080/health"}]`,
		VerificationWorkflowID: "workflow.core-10",
	}, workspace, false, false, false, time.Second, environmentRestoreWorkflowOptions{
		StoreURL: "postgres://tester@127.0.0.1:5432/otsandbox?sslmode=disable",
	}, environmentRestoreDockerCleanupOptions{})
	if err != nil {
		t.Fatalf("build restore PostgreSQL generated startup report: %v", err)
	}
	if !report.SourcePolicy.OK || !report.SourcePolicy.RemoteOnly || report.Package.Action != "ignored-for-postgresql-store-restore" || report.Docker.Action != "plan-docker-compose" {
		t.Fatalf("PostgreSQL generated startup report = %#v", report)
	}
	if len(report.Docker.Generated) != 1 || report.Docker.Generated[0].Action != "plan-write" || !report.Docker.Generated[0].OK {
		t.Fatalf("generated startup file report = %#v", report.Docker.Generated)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "store-startup-files", true, "generated from Store metadata") {
		t.Fatalf("readiness should accept Store generated startup files: %#v", report.Readiness.Items)
	}
}

func TestEnvironmentRestorePostgreSQLRejectsLocalStartupFilesWithoutStoreGeneratedContent(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeBin := t.TempDir()
	writeFile(t, filepath.Join(fakeBin, "git"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(fakeBin, "docker"), "#!/bin/sh\nif [ \"$1\" = compose ] && [ \"$2\" = version ]; then exit 0; fi\nexit 0\n")
	if err := os.Chmod(filepath.Join(fakeBin, "git"), 0o755); err != nil {
		t.Fatalf("chmod fake git: %v", err)
	}
	if err := os.Chmod(filepath.Join(fakeBin, "docker"), 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin)

	report, err := buildEnvironmentRestoreReport(context.Background(), store.Environment{
		ID:                     "env.pg.local.compose",
		ReposJSON:              `{"llt":{"url":"git@github.com:ztcshen/open-test-sandbox-llt-simulator.git","checkout":"llt"}}`,
		ComposeJSON:            `{"composeFile":"compose/docker-compose.yml","composeFiles":["compose/docker-compose.yml"],"package":{"url":"/Users/zlh/codes/open-test-sandbox-validation","checkout":"."}}`,
		HealthChecksJSON:       `[{"kind":"url","url":"http://127.0.0.1:28080/health"}]`,
		VerificationWorkflowID: "workflow.core-10",
	}, workspace, false, false, false, time.Second, environmentRestoreWorkflowOptions{
		StoreURL: "postgres://tester@127.0.0.1:5432/otsandbox?sslmode=disable",
	}, environmentRestoreDockerCleanupOptions{})
	if err != nil {
		t.Fatalf("build restore PostgreSQL local startup report: %v", err)
	}
	if !report.SourcePolicy.OK || report.Package.Action != "ignored-for-postgresql-store-restore" {
		t.Fatalf("PostgreSQL local startup pre-readiness report = %#v", report)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "store-startup-files", false, "missing generatedFiles") {
		t.Fatalf("readiness should reject local startup files without Store content: %#v", report.Readiness.Items)
	}
}

func TestEnvironmentRestorePreflightReportsMissingDockerComposePlugin(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeBin := t.TempDir()
	writeFile(t, filepath.Join(fakeBin, "git"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(fakeBin, "docker"), "#!/bin/sh\nif [ \"$1\" = compose ] && [ \"$2\" = version ]; then exit 17; fi\nexit 0\n")
	if err := os.Chmod(filepath.Join(fakeBin, "git"), 0o755); err != nil {
		t.Fatalf("chmod fake git: %v", err)
	}
	if err := os.Chmod(filepath.Join(fakeBin, "docker"), 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin)
	report, err := buildEnvironmentRestoreReport(context.Background(), store.Environment{
		ID:                     "env.preflight.compose",
		ReposJSON:              `{}`,
		ComposeJSON:            `{"composeFile":"docker-compose.yml"}`,
		HealthChecksJSON:       `[]`,
		VerificationWorkflowID: "workflow.core-10",
	}, workspace, false, false, false, time.Second, environmentRestoreWorkflowOptions{}, environmentRestoreDockerCleanupOptions{})
	if err != nil {
		t.Fatalf("build restore preflight report: %v", err)
	}
	if report.OK || report.Preflight.OK || !restoreTypedPreflightHasTool(report.Preflight.Tools, "docker", true) || !restoreTypedPreflightHasTool(report.Preflight.Tools, "docker compose", false) {
		t.Fatalf("missing docker compose preflight report = %#v", report.Preflight)
	}
}

func restoreTypedReadinessHasItem(items []environmentRestoreReadinessItem, name string, ok bool, detailContains string) bool {
	for _, item := range items {
		if item.Name != name || item.OK != ok {
			continue
		}
		if detailContains == "" || strings.Contains(item.Detail, detailContains) {
			return true
		}
	}
	return false
}

func restorePreflightHasTool(tools []struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	OK       bool   `json:"ok"`
}, name string, ok bool) bool {
	for _, tool := range tools {
		if tool.Name == name && tool.Required && tool.OK == ok {
			return true
		}
	}
	return false
}

func restoreTypedPreflightHasTool(tools []environmentRestorePreflightTool, name string, ok bool) bool {
	for _, tool := range tools {
		if tool.Name == name && tool.Required && tool.OK == ok {
			return true
		}
	}
	return false
}

func restoreReadinessHasItem(items []struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}, name string, ok bool, detailContains string) bool {
	for _, item := range items {
		if item.Name != name || item.OK != ok {
			continue
		}
		if detailContains == "" || strings.Contains(item.Detail, detailContains) {
			return true
		}
	}
	return false
}

func TestEnvironmentRestoreExecutesDockerComposeWithoutRepository(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, _ := fakeDockerCommand(t)
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.docker.only",
		"--compose-file", "compose.yml",
		"--health-url", healthServer.URL+"/ready",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.docker.only")
	var report struct {
		OK     bool  `json:"ok"`
		Repos  []any `json:"repos"`
		Docker struct {
			OK           bool   `json:"ok"`
			Action       string `json:"action"`
			HealthChecks []struct {
				OK bool `json:"ok"`
			} `json:"healthChecks"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode docker-only restore json: %v\n%s", err, out)
	}
	if !report.OK || len(report.Repos) != 0 || !report.Docker.OK || report.Docker.Action != "run-docker-compose" || len(report.Docker.HealthChecks) != 1 || !report.Docker.HealthChecks[0].OK {
		t.Fatalf("docker-only restore report = %#v", report)
	}
}

func TestEnvironmentRestoreRunsMixedHealthProbes(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, _ := fakeDockerCommand(t)
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp health: %v", err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.health.mixed",
		"--compose-file", "compose.yml",
		"--health-url", healthServer.URL+"/ready",
		"--health-tcp", listener.Addr().String(),
		"--health-command", "test -f compose.yml",
		"--health-compose-service", "web",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.health.mixed")
	var report struct {
		OK     bool `json:"ok"`
		Docker struct {
			HealthChecks []struct {
				Kind    string `json:"kind"`
				OK      bool   `json:"ok"`
				State   string `json:"state"`
				Health  string `json:"health"`
				Service string `json:"service"`
			} `json:"healthChecks"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode mixed health restore json: %v\n%s", err, out)
	}
	if !report.OK || len(report.Docker.HealthChecks) != 4 {
		t.Fatalf("mixed health report = %#v", report)
	}
	seen := map[string]bool{}
	for _, check := range report.Docker.HealthChecks {
		if !check.OK {
			t.Fatalf("mixed health check failed: %#v", check)
		}
		seen[check.Kind] = true
		if check.Kind == "compose-service" && (check.Service != "web" || check.State != "running" || check.Health != "healthy") {
			t.Fatalf("compose service health = %#v", check)
		}
	}
	for _, kind := range []string{"url", "tcp", "command", "compose-service"} {
		if !seen[kind] {
			t.Fatalf("missing health kind %s in %#v", kind, report.Docker.HealthChecks)
		}
	}
}

func TestEnvironmentRestoreFailsWhenHealthProbeFails(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, _ := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.health.fail",
		"--compose-file", "compose.yml",
		"--health-command", "echo nope && exit 7",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIFailsWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--health-timeout-seconds", "1", "--json", "env.health.fail")
	if !strings.Contains(out, `"kind": "command"`) || !strings.Contains(out, "exit status 7") {
		t.Fatalf("health failure output = %q", out)
	}
	inspectOut := runCLI(t, "environment", "inspect", "--store", "sqlite://"+storePath, "--json", "env.health.fail")
	if !strings.Contains(inspectOut, `"phase": "health-check"`) {
		t.Fatalf("health failure should persist health-check phase: %s", inspectOut)
	}
}

func TestEnvironmentRestoreHonorsComposeOptionsFromStore(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")
	writeFile(t, filepath.Join(workspace, ".env.local"), "MODE=local\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.compose.options",
		"--compose-file", "compose.yml",
		"--compose-project-name", "demo",
		"--compose-env-file", ".env.local",
		"--compose-profile", "api",
		"--compose-service", "web",
		"--compose-skip-pull",
		"--compose-skip-build",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.compose.options")
	var report struct {
		OK      bool `json:"ok"`
		Compose struct {
			ProjectName string   `json:"projectName"`
			EnvFiles    []string `json:"envFiles"`
			Profiles    []string `json:"profiles"`
			Services    []string `json:"services"`
			SkipPull    bool     `json:"skipPull"`
			SkipBuild   bool     `json:"skipBuild"`
		} `json:"compose"`
		Docker struct {
			Commands     [][]string `json:"commands"`
			HealthChecks []struct {
				Kind    string `json:"kind"`
				Service string `json:"service"`
				State   string `json:"state"`
				OK      bool   `json:"ok"`
			} `json:"healthChecks"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode compose options restore json: %v\n%s", err, out)
	}
	if !report.OK || report.Compose.ProjectName != "demo" || len(report.Compose.EnvFiles) != 1 || len(report.Compose.Profiles) != 1 || len(report.Compose.Services) != 1 || !report.Compose.SkipPull || !report.Compose.SkipBuild {
		t.Fatalf("compose options report = %#v", report)
	}
	if len(report.Docker.Commands) != 1 {
		t.Fatalf("compose options should only run up command, got %#v", report.Docker.Commands)
	}
	if len(report.Docker.HealthChecks) != 1 || report.Docker.HealthChecks[0].Kind != "compose-service" || report.Docker.HealthChecks[0].Service != "web" || report.Docker.HealthChecks[0].State != "running" || !report.Docker.HealthChecks[0].OK {
		t.Fatalf("compose service readiness should be generated for requested service: %#v", report.Docker.HealthChecks)
	}
	want := "compose -f " + filepath.Join(workspace, "compose.yml") + " -p demo --env-file " + filepath.Join(workspace, ".env.local") + " --profile api up -d web"
	dockerCalls, err := os.ReadFile(dockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if strings.Contains(string(dockerCalls), " pull") || strings.Contains(string(dockerCalls), " build") || !strings.Contains(string(dockerCalls), want) {
		t.Fatalf("compose option docker calls want %q:\n%s", want, dockerCalls)
	}
	if !strings.Contains(string(dockerCalls), "compose -f "+filepath.Join(workspace, "compose.yml")+" -p demo --env-file "+filepath.Join(workspace, ".env.local")+" --profile api ps --format json web") {
		t.Fatalf("compose option docker calls should include service readiness check:\n%s", dockerCalls)
	}
}

func TestEnvironmentRestoreSupportsMultipleComposeFiles(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "compose.base.yml"), "services: {}\n")
	writeFile(t, filepath.Join(workspace, "compose.apps.yml"), "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.compose.multi",
		"--compose-file", "compose.base.yml",
		"--compose-file", "compose.apps.yml",
		"--compose-env", "SANDBOX_ROOT=$OTS_WORKSPACE",
		"--compose-skip-pull",
		"--compose-skip-build",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.compose.multi")
	var report struct {
		OK      bool `json:"ok"`
		Compose struct {
			ComposeFile  string   `json:"composeFile"`
			ComposeFiles []string `json:"composeFiles"`
		} `json:"compose"`
		Docker struct {
			ComposeFile string     `json:"composeFile"`
			Commands    [][]string `json:"commands"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode multi compose restore json: %v\n%s", err, out)
	}
	if !report.OK || report.Compose.ComposeFile != "compose.base.yml" || len(report.Compose.ComposeFiles) != 2 || !strings.Contains(report.Docker.ComposeFile, "compose.base.yml") || !strings.Contains(report.Docker.ComposeFile, "compose.apps.yml") {
		t.Fatalf("multi compose report = %#v", report)
	}
	dockerCalls, err := os.ReadFile(dockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	want := "compose -f " + filepath.Join(workspace, "compose.base.yml") + " -f " + filepath.Join(workspace, "compose.apps.yml") + " up -d"
	want = strings.Replace(want, " up -d", " --env-file "+filepath.Join(workspace, ".otsandbox", "restore.env")+" up -d", 1)
	if !strings.Contains(string(dockerCalls), want) {
		t.Fatalf("multi compose docker calls missing %q:\n%s", want, dockerCalls)
	}
	envFile, err := os.ReadFile(filepath.Join(workspace, ".otsandbox", "restore.env"))
	if err != nil || !strings.Contains(string(envFile), "SANDBOX_ROOT="+workspace) {
		t.Fatalf("generated compose env file = %q err=%v", envFile, err)
	}
}

func TestEnvironmentRestoreCanPrepareRepositoriesBeforeDocker(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	remoteRepo := createBareGitRepo(t, "main")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.prepare.repos",
		"--service", "entry-gateway",
		"--repo", "entry-gateway="+remoteRepo,
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "compose.yml",
		"--health-url", healthServer.URL+"/ready",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--prepare-repos-only", "--json", "env.prepare.repos")
	var report struct {
		OK       bool `json:"ok"`
		Executed bool `json:"executed"`
		Repos    []struct {
			ServiceID string `json:"serviceId"`
			Action    string `json:"action"`
			OK        bool   `json:"ok"`
		} `json:"repos"`
		Docker struct {
			OK     bool   `json:"ok"`
			Action string `json:"action"`
		} `json:"docker"`
		Readiness struct {
			OK bool `json:"ok"`
		} `json:"readiness"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode prepare repos restore json: %v\n%s", err, out)
	}
	if !report.OK || !report.Executed || len(report.Repos) != 1 || report.Repos[0].Action != "clone" || !report.Repos[0].OK || !report.Docker.OK || report.Docker.Action != "skipped-after-repository-preparation" || !report.Readiness.OK {
		t.Fatalf("prepare repos report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(workspace, "entry-gateway", ".git")); err != nil {
		t.Fatalf("repository was not cloned before Docker: %v", err)
	}
	dockerCalls, err := os.ReadFile(dockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if strings.Contains(string(dockerCalls), " compose ") {
		t.Fatalf("prepare repos should not invoke Docker Compose:\n%s", dockerCalls)
	}
}

func TestEnvironmentRestoreCanPreparePackageRepositoryBeforeDocker(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	packageRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"compose/docker-compose.yml": "services: {}\n",
		"README.md":                  "# environment package\n",
	})
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create empty workspace: %v", err)
	}
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.package.prepare",
		"--package-repo", packageRepo,
		"--package-branch", "main",
		"--compose-file", "compose/docker-compose.yml",
		"--health-url", healthServer.URL+"/ready",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--prepare-repos-only", "--json", "env.package.prepare")
	var report struct {
		OK      bool `json:"ok"`
		Package struct {
			Configured bool   `json:"configured"`
			Action     string `json:"action"`
			OK         bool   `json:"ok"`
			Checkout   string `json:"checkout"`
		} `json:"package"`
		Repos  []any `json:"repos"`
		Docker struct {
			OK     bool   `json:"ok"`
			Action string `json:"action"`
		} `json:"docker"`
		Readiness struct {
			OK    bool `json:"ok"`
			Items []struct {
				Name   string `json:"name"`
				OK     bool   `json:"ok"`
				Detail string `json:"detail"`
			} `json:"items"`
		} `json:"readiness"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode package prepare restore json: %v\n%s", err, out)
	}
	if !report.OK || !report.Package.Configured || report.Package.Action != "clone" || !report.Package.OK || report.Package.Checkout != workspace || len(report.Repos) != 0 || !report.Docker.OK || report.Docker.Action != "skipped-after-repository-preparation" || !report.Readiness.OK {
		t.Fatalf("package prepare report = %#v", report)
	}
	if !restoreReadinessHasItem(report.Readiness.Items, "environment-package", true, "environment package") {
		t.Fatalf("readiness should include package gate: %#v", report.Readiness.Items)
	}
	if raw, err := os.ReadFile(filepath.Join(workspace, "compose", "docker-compose.yml")); err != nil || !strings.Contains(string(raw), "services") {
		t.Fatalf("package compose file missing raw=%q err=%v", raw, err)
	}
	dockerCalls, err := os.ReadFile(dockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if strings.Contains(string(dockerCalls), " compose ") {
		t.Fatalf("prepare package should not invoke Docker Compose:\n%s", dockerCalls)
	}
}

func TestEnvironmentRestoreWritesStoreGeneratedComposeFileBeforeDocker(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	sourceCompose := filepath.Join(t.TempDir(), "source-compose.yml")
	writeFile(t, sourceCompose, "services:\n  generated-service:\n    image: alpine:3.20\n")
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.generated.compose",
		"--compose-file", "compose/docker-compose.yml",
		"--compose-generated-file", "compose/docker-compose.yml="+sourceCompose,
		"--compose-skip-pull",
		"--compose-skip-build",
		"--health-url", healthServer.URL+"/ready",
		"--verification-workflow", "workflow.core-10",
	)

	dryRunOut := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--json", "env.generated.compose")
	var dryRun struct {
		OK     bool `json:"ok"`
		Docker struct {
			Generated []struct {
				Path   string `json:"path"`
				Action string `json:"action"`
				OK     bool   `json:"ok"`
			} `json:"generatedFiles"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(dryRunOut), &dryRun); err != nil {
		t.Fatalf("decode generated compose dry-run json: %v\n%s", err, dryRunOut)
	}
	generatedPath := filepath.Join(workspace, "compose", "docker-compose.yml")
	if !dryRun.OK || len(dryRun.Docker.Generated) != 1 || dryRun.Docker.Generated[0].Action != "plan-write" || dryRun.Docker.Generated[0].Path != generatedPath || !dryRun.Docker.Generated[0].OK {
		t.Fatalf("generated compose dry-run = %#v", dryRun)
	}
	if _, err := os.Stat(generatedPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write generated compose file, stat err=%v", err)
	}

	executeOut := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.generated.compose")
	var executed struct {
		OK     bool `json:"ok"`
		Docker struct {
			Action    string `json:"action"`
			Generated []struct {
				Path   string `json:"path"`
				Action string `json:"action"`
				OK     bool   `json:"ok"`
			} `json:"generatedFiles"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(executeOut), &executed); err != nil {
		t.Fatalf("decode generated compose execute json: %v\n%s", err, executeOut)
	}
	if !executed.OK || executed.Docker.Action != "run-docker-compose" || len(executed.Docker.Generated) != 1 || executed.Docker.Generated[0].Action != "write" || !executed.Docker.Generated[0].OK {
		t.Fatalf("generated compose execute = %#v", executed)
	}
	if raw, err := os.ReadFile(generatedPath); err != nil || !strings.Contains(string(raw), "generated-service") {
		t.Fatalf("generated compose file raw=%q err=%v", raw, err)
	}
	dockerCalls, err := os.ReadFile(dockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if !strings.Contains(string(dockerCalls), "compose -f "+generatedPath+" up -d") {
		t.Fatalf("fake docker calls should use generated compose file:\n%s", dockerCalls)
	}
}

func TestEnvironmentRestorePrepareReposOnlyWritesStoreGeneratedComposeFile(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	sourceCompose := filepath.Join(t.TempDir(), "source-compose.yml")
	writeFile(t, sourceCompose, "services:\n  generated-service:\n    image: alpine:3.20\n")
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.generated.prepare",
		"--compose-file", "compose/docker-compose.yml",
		"--compose-generated-file", "compose/docker-compose.yml="+sourceCompose,
		"--health-url", healthServer.URL+"/ready",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--prepare-repos-only", "--json", "env.generated.prepare")
	var report struct {
		OK     bool `json:"ok"`
		Docker struct {
			Action    string `json:"action"`
			Generated []struct {
				Path   string `json:"path"`
				Action string `json:"action"`
				OK     bool   `json:"ok"`
			} `json:"generatedFiles"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode generated prepare-only restore json: %v\n%s", err, out)
	}
	generatedPath := filepath.Join(workspace, "compose", "docker-compose.yml")
	if !report.OK || report.Docker.Action != "skipped-after-repository-preparation" || len(report.Docker.Generated) != 1 || report.Docker.Generated[0].Action != "write" || report.Docker.Generated[0].Path != generatedPath || !report.Docker.Generated[0].OK {
		t.Fatalf("generated prepare-only report = %#v", report)
	}
	if raw, err := os.ReadFile(generatedPath); err != nil || !strings.Contains(string(raw), "generated-service") {
		t.Fatalf("generated compose file raw=%q err=%v", raw, err)
	}
	dockerCalls, err := os.ReadFile(dockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if strings.Contains(string(dockerCalls), " compose ") {
		t.Fatalf("prepare-only should not invoke Docker Compose:\n%s", dockerCalls)
	}
}

func TestEnvironmentRestoreBlocksDockerWhenContainerNamesAlreadyExist(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeBin := t.TempDir()
	writeFile(t, filepath.Join(fakeBin, "git"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(fakeBin, "docker"), "#!/bin/sh\nif [ \"$1\" = compose ] && [ \"$2\" = version ]; then exit 0; fi\nif [ \"$1\" = ps ]; then printf 'sandbox-mysql\\n'; exit 0; fi\nexit 0\n")
	if err := os.Chmod(filepath.Join(fakeBin, "git"), 0o755); err != nil {
		t.Fatalf("chmod fake git: %v", err)
	}
	if err := os.Chmod(filepath.Join(fakeBin, "docker"), 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin)

	report, err := buildEnvironmentRestoreReport(context.Background(), store.Environment{
		ID:                     "env.container.conflict",
		ComposeJSON:            `{"composeFile":"compose.yml","generatedFiles":{"compose.yml":"services:\n  mysql:\n    image: mysql:8\n    container_name: sandbox-mysql\n"}}`,
		HealthChecksJSON:       `[{"kind":"url","url":"http://127.0.0.1:18080/health"}]`,
		VerificationWorkflowID: "workflow.core-10",
	}, workspace, false, false, false, time.Second, environmentRestoreWorkflowOptions{}, environmentRestoreDockerCleanupOptions{})
	if err != nil {
		t.Fatalf("build restore container conflict report: %v", err)
	}
	if report.OK || report.Preflight.OK || len(report.Preflight.ContainerConflicts) != 1 || report.Preflight.ContainerConflicts[0] != "sandbox-mysql" {
		t.Fatalf("container conflict report = %#v", report)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "docker-container-conflicts", false, "sandbox-mysql") {
		t.Fatalf("readiness should include container conflict: %#v", report.Readiness.Items)
	}
}

func TestEnvironmentStartupFilePutMergesGeneratedFilesWithoutReRegistering(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	sourceCompose := filepath.Join(t.TempDir(), "source-compose.yml")
	writeFile(t, sourceCompose, "services:\n  generated-service:\n    image: alpine:3.20\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.startup.files",
		"--repo", "entry-gateway=https://example.com/team/entry-gateway.git",
		"--checkout", "entry-gateway=services/entry-gateway",
		"--compose-file", "compose/docker-compose.yml",
		"--health-url", "http://127.0.0.1:18080/health",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLI(t, "environment", "startup-file", "put",
		"--store", "sqlite://"+storePath,
		"--file", "compose/docker-compose.yml="+sourceCompose,
		"--json",
		"env.startup.files",
	)
	var payload struct {
		GeneratedFiles []struct {
			Path  string `json:"path"`
			Bytes int    `json:"bytes"`
		} `json:"generatedFiles"`
		Environment struct {
			Repos   map[string]any `json:"repos"`
			Compose struct {
				GeneratedFiles map[string]string `json:"generatedFiles"`
			} `json:"compose"`
			Summary struct {
				StartupFiles struct {
					Files []struct {
						Path string `json:"path"`
					} `json:"files"`
				} `json:"startupFiles"`
			} `json:"summary"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode startup-file put json: %v\n%s", err, out)
	}
	if len(payload.GeneratedFiles) != 1 || payload.GeneratedFiles[0].Path != "compose/docker-compose.yml" || payload.GeneratedFiles[0].Bytes == 0 {
		t.Fatalf("startup-file payload = %#v", payload.GeneratedFiles)
	}
	if payload.Environment.Repos["entry-gateway"] == nil {
		t.Fatalf("startup-file put should preserve existing repositories: %#v", payload.Environment.Repos)
	}
	if !strings.Contains(payload.Environment.Compose.GeneratedFiles["compose/docker-compose.yml"], "generated-service") {
		t.Fatalf("generated file was not stored in compose metadata: %#v", payload.Environment.Compose.GeneratedFiles)
	}
	if len(payload.Environment.Summary.StartupFiles.Files) != 1 || payload.Environment.Summary.StartupFiles.Files[0].Path != "compose/docker-compose.yml" {
		t.Fatalf("startup-file summary = %#v", payload.Environment.Summary.StartupFiles)
	}
}

func TestEnvironmentRestorePlansDockerCleanupWithoutExecuting(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, _ := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.cleanup.plan",
		"--compose-file", "compose.yml",
		"--compose-project-name", "demo",
		"--compose-service", "web",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--clean-docker-state", "--clean-docker-images", "--json", "env.cleanup.plan")
	var report struct {
		OK     bool `json:"ok"`
		Docker struct {
			Cleanup struct {
				Requested      bool       `json:"requested"`
				Allowed        bool       `json:"allowed"`
				IncludeImages  bool       `json:"includeImages"`
				Action         string     `json:"action"`
				BackupCommands [][]string `json:"backupCommands"`
				Commands       [][]string `json:"commands"`
				Warning        string     `json:"warning"`
			} `json:"cleanup"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode cleanup dry-run json: %v\n%s", err, out)
	}
	cleanup := report.Docker.Cleanup
	if !report.OK || !cleanup.Requested || cleanup.Allowed || !cleanup.IncludeImages || cleanup.Action != "plan-cleanup" || len(cleanup.BackupCommands) != 3 || len(cleanup.Commands) != 1 {
		t.Fatalf("cleanup dry-run report = %#v", report.Docker.Cleanup)
	}
	command := strings.Join(cleanup.Commands[0], " ")
	if !strings.Contains(command, "compose -f "+filepath.Join(workspace, "compose.yml")+" -p demo down --remove-orphans --rmi all") {
		t.Fatalf("cleanup command = %#v", cleanup.Commands[0])
	}
	allCommands := strings.Join(append(cleanup.BackupCommands[0], cleanup.Commands[0]...), " ")
	if strings.Contains(allCommands, "--volumes") || strings.Contains(allCommands, "system prune") {
		t.Fatalf("cleanup should stay scoped to compose project: %q", allCommands)
	}
	if !strings.Contains(cleanup.Warning, "PostgreSQL Store") {
		t.Fatalf("cleanup warning should mention Store boundary: %q", cleanup.Warning)
	}
}

func TestEnvironmentRestoreBlocksDockerCleanupWithoutExplicitAllow(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.cleanup.block",
		"--compose-file", "compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIFailsWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--clean-docker-state", "--json", "env.cleanup.block")
	if !strings.Contains(out, "cleanup-blocked") || !strings.Contains(out, "--allow-destructive-docker-cleanup") {
		t.Fatalf("cleanup block output = %q", out)
	}
	if raw, err := os.ReadFile(dockerCallsPath); err == nil {
		calls := string(raw)
		for _, forbidden := range []string{" down ", " pull", " build", " up -d"} {
			if strings.Contains(calls, forbidden) {
				t.Fatalf("blocked cleanup should not run docker command %q:\n%s", forbidden, calls)
			}
		}
	}
	inspectOut := runCLI(t, "environment", "inspect", "--store", "sqlite://"+storePath, "--json", "env.cleanup.block")
	var inspected struct {
		Environment struct {
			Summary struct {
				LastRestore struct {
					OK     bool   `json:"ok"`
					Phase  string `json:"phase"`
					Docker struct {
						Action  string `json:"action"`
						OK      bool   `json:"ok"`
						Cleanup struct {
							Requested bool   `json:"requested"`
							Action    string `json:"action"`
							Error     string `json:"error"`
						} `json:"cleanup"`
					} `json:"docker"`
				} `json:"lastRestore"`
				RestoreAttempts []struct {
					Phase string `json:"phase"`
				} `json:"restoreAttempts"`
			} `json:"summary"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(inspectOut), &inspected); err != nil {
		t.Fatalf("decode cleanup block inspect json: %v\n%s", err, inspectOut)
	}
	lastRestore := inspected.Environment.Summary.LastRestore
	if lastRestore.OK || lastRestore.Phase != "docker" || lastRestore.Docker.OK || lastRestore.Docker.Action != "plan-docker-compose" || !lastRestore.Docker.Cleanup.Requested || lastRestore.Docker.Cleanup.Action != "cleanup-blocked" || !strings.Contains(lastRestore.Docker.Cleanup.Error, "--allow-destructive-docker-cleanup") {
		t.Fatalf("persisted blocked cleanup summary = %#v", lastRestore)
	}
	if len(inspected.Environment.Summary.RestoreAttempts) != 1 || inspected.Environment.Summary.RestoreAttempts[0].Phase != "docker" {
		t.Fatalf("persisted blocked cleanup attempts = %#v", inspected.Environment.Summary.RestoreAttempts)
	}
}

func TestEnvironmentRestoreRunsAllowedDockerCleanupBeforeStartup(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.cleanup.execute",
		"--compose-file", "compose.yml",
		"--compose-skip-pull",
		"--compose-skip-build",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--clean-docker-state", "--clean-docker-images", "--allow-destructive-docker-cleanup", "--json", "env.cleanup.execute")
	var report struct {
		OK     bool `json:"ok"`
		Docker struct {
			Cleanup struct {
				Action string `json:"action"`
			} `json:"cleanup"`
		} `json:"docker"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode cleanup execute json: %v\n%s", err, out)
	}
	if !report.OK || report.Docker.Cleanup.Action != "run-cleanup" {
		t.Fatalf("cleanup execute report = %#v", report)
	}
	raw, err := os.ReadFile(dockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"compose -f " + filepath.Join(workspace, "compose.yml") + " ps", "compose -f " + filepath.Join(workspace, "compose.yml") + " images", "compose -f " + filepath.Join(workspace, "compose.yml") + " config", "compose -f " + filepath.Join(workspace, "compose.yml") + " down --remove-orphans --rmi all", "compose -f " + filepath.Join(workspace, "compose.yml") + " up -d"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("cleanup docker calls missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "--volumes") || strings.Contains(joined, "system prune") {
		t.Fatalf("cleanup should not remove volumes or run global prune:\n%s", joined)
	}
	order := []string{" ps", " images", " config", " down --remove-orphans --rmi all", " up -d"}
	last := -1
	for _, marker := range order {
		index := strings.Index(joined, marker)
		if index <= last {
			t.Fatalf("cleanup order marker %q out of order in:\n%s", marker, joined)
		}
		last = index
	}
}

func TestEnvironmentRestoreFailsBeforeDockerWhenComposeFileIsMissing(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.missing.compose",
		"--compose-file", "missing-compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIFails(t, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.missing.compose")
	if !strings.Contains(out, "missing-compose-file") || !strings.Contains(out, "missing-compose.yml") {
		t.Fatalf("missing compose restore output = %q", out)
	}
}

func TestEnvironmentRestoreRunsVerificationWorkflowAfterDockerHealth(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	outputDir := filepath.Join(t.TempDir(), "workflow-evidence")
	fakeDockerEnv, _ := fakeDockerCommand(t)
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	var acceptancePayload map[string]any
	acceptanceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments/env.workflow.restore/acceptance-runs":
			if err := json.NewDecoder(r.Body).Decode(&acceptancePayload); err != nil {
				t.Fatalf("decode acceptance payload: %v", err)
			}
			writeTestJSON(t, w, http.StatusAccepted, map[string]any{
				"ok":            true,
				"environmentId": "env.workflow.restore",
				"batchRunId":    "batch.env.restore.acceptance.001",
				"requestId":     "restore.env.workflow.restore",
				"workflowId":    "workflow.alpha",
				"status":        "running",
				"reportUrl":     "/api/environments/env.workflow.restore/acceptance-runs/batch.env.restore.acceptance.001",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/env.workflow.restore/acceptance-runs/batch.env.restore.acceptance.001":
			writeTestJSON(t, w, http.StatusOK, map[string]any{
				"ok":            true,
				"environmentId": "env.workflow.restore",
				"batchRunId":    "batch.env.restore.acceptance.001",
				"workflowId":    "workflow.alpha",
				"status":        "passed",
				"acceptance": map[string]any{
					"ok":               true,
					"templateId":       "environment.workflow.skywalking.v1",
					"workflowId":       "workflow.alpha",
					"expectedSteps":    10,
					"completedSteps":   10,
					"passedSteps":      10,
					"failedSteps":      0,
					"topologyProvider": "skywalking",
				},
			})
		default:
			t.Fatalf("unexpected acceptance request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer acceptanceServer.Close()
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.workflow.restore",
		"--compose-file", "compose.yml",
		"--health-url", healthServer.URL+"/ready",
		"--verification-workflow", "workflow.alpha",
	)

	out := runCLIWithEnv(t, fakeDockerEnv,
		"environment", "restore",
		"--store", "sqlite://"+storePath,
		"--workspace", workspace,
		"--execute",
		"--run-workflow",
		"--server-url", acceptanceServer.URL,
		"--base-url", "http://127.0.0.1:18080",
		"--workflow-output-dir", outputDir,
		"--json",
		"env.workflow.restore",
	)
	var report struct {
		OK       bool `json:"ok"`
		Executed bool `json:"executed"`
		Docker   struct {
			OK bool `json:"ok"`
		} `json:"docker"`
		Workflow struct {
			OK         bool   `json:"ok"`
			Action     string `json:"action"`
			WorkflowID string `json:"workflowId"`
			RunID      string `json:"runId"`
			OutputDir  string `json:"outputDir"`
			ReportURL  string `json:"reportUrl"`
			Acceptance struct {
				OK               bool   `json:"ok"`
				TemplateID       string `json:"templateId"`
				ExpectedSteps    int    `json:"expectedSteps"`
				CompletedSteps   int    `json:"completedSteps"`
				PassedSteps      int    `json:"passedSteps"`
				FailedSteps      int    `json:"failedSteps"`
				TopologyProvider string `json:"topologyProvider"`
			} `json:"acceptance"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode restore workflow json: %v\n%s", err, out)
	}
	if !report.OK || !report.Executed || !report.Docker.OK || !report.Workflow.OK || report.Workflow.Action != "run-acceptance-workflow" || report.Workflow.WorkflowID != "workflow.alpha" || report.Workflow.RunID != "batch.env.restore.acceptance.001" {
		t.Fatalf("restore workflow report = %#v", report)
	}
	if report.Workflow.OutputDir != outputDir || report.Workflow.ReportURL == "" || !report.Workflow.Acceptance.OK || report.Workflow.Acceptance.TemplateID != "environment.workflow.skywalking.v1" || report.Workflow.Acceptance.ExpectedSteps != 10 || report.Workflow.Acceptance.CompletedSteps != 10 || report.Workflow.Acceptance.PassedSteps != 10 || report.Workflow.Acceptance.FailedSteps != 0 || report.Workflow.Acceptance.TopologyProvider != "skywalking" {
		t.Fatalf("restore workflow acceptance = %#v", report.Workflow)
	}
	if acceptancePayload["baseUrl"] != "http://127.0.0.1:18080" || acceptancePayload["evidenceDir"] != outputDir {
		t.Fatalf("restore acceptance payload = %#v", acceptancePayload)
	}
	inspectOut := runCLI(t, "environment", "inspect", "--store", "sqlite://"+storePath, "--json", "env.workflow.restore")
	var inspected struct {
		Environment struct {
			Status                 string `json:"status"`
			LastVerificationRunID  string `json:"lastVerificationRunId"`
			LastVerificationStatus string `json:"lastVerificationStatus"`
			EvidenceComplete       bool   `json:"evidenceComplete"`
			TopologyComplete       bool   `json:"topologyComplete"`
			Verified               bool   `json:"verified"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(inspectOut), &inspected); err != nil {
		t.Fatalf("decode restored environment inspect json: %v\n%s", err, inspectOut)
	}
	if inspected.Environment.LastVerificationRunID != report.Workflow.RunID || inspected.Environment.LastVerificationStatus != store.StatusPassed || inspected.Environment.Status != "verification-recorded" || !inspected.Environment.EvidenceComplete || !inspected.Environment.TopologyComplete || inspected.Environment.Verified {
		t.Fatalf("restored environment status = %#v", inspected.Environment)
	}
}

func TestEnvironmentRestoreUsesNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "restore-active-pg")
	workspace := filepath.Join(t.TempDir(), "workspace")
	outputDir := filepath.Join(t.TempDir(), "workflow-evidence")
	envID := uniqueTestID(t, "env.restore.pg")
	fakeDockerEnv, _ := fakeDockerCommand(t)
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	acceptanceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments/"+envID+"/acceptance-runs":
			writeTestJSON(t, w, http.StatusAccepted, map[string]any{
				"ok":            true,
				"environmentId": envID,
				"batchRunId":    "batch." + envID + ".acceptance.001",
				"workflowId":    "workflow.alpha",
				"status":        "running",
				"reportUrl":     "/api/environments/" + envID + "/acceptance-runs/batch." + envID + ".acceptance.001",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/"+envID+"/acceptance-runs/batch."+envID+".acceptance.001":
			writeTestJSON(t, w, http.StatusOK, map[string]any{
				"ok":            true,
				"environmentId": envID,
				"batchRunId":    "batch." + envID + ".acceptance.001",
				"workflowId":    "workflow.alpha",
				"status":        "passed",
				"acceptance": map[string]any{
					"ok":               true,
					"templateId":       "environment.workflow.skywalking.v1",
					"workflowId":       "workflow.alpha",
					"expectedSteps":    10,
					"completedSteps":   10,
					"passedSteps":      10,
					"failedSteps":      0,
					"topologyProvider": "skywalking",
				},
			})
		default:
			t.Fatalf("unexpected active PG acceptance request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer acceptanceServer.Close()
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")
	runCLI(t, "environment", "register",
		"--id", envID,
		"--compose-file", "compose.yml",
		"--health-url", healthServer.URL+"/ready",
		"--verification-workflow", "workflow.alpha",
	)

	out := runCLIWithEnv(t, fakeDockerEnv,
		"environment", "restore",
		envID,
		"--workspace", workspace,
		"--execute",
		"--run-workflow",
		"--server-url", acceptanceServer.URL,
		"--base-url", "http://127.0.0.1:18080",
		"--workflow-output-dir", outputDir,
		"--json",
	)
	var report struct {
		OK       bool `json:"ok"`
		Executed bool `json:"executed"`
		Workflow struct {
			OK         bool   `json:"ok"`
			Action     string `json:"action"`
			RunID      string `json:"runId"`
			Acceptance struct {
				OK bool `json:"ok"`
			} `json:"acceptance"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode active PostgreSQL restore json: %v\n%s", err, out)
	}
	if !report.OK || !report.Executed || !report.Workflow.OK || report.Workflow.Action != "run-acceptance-workflow" || !report.Workflow.Acceptance.OK || report.Workflow.RunID == "" {
		t.Fatalf("active PostgreSQL restore report = %#v", report)
	}
}

func TestEnvironmentRestorePullsExistingCheckoutWhenRequested(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	remoteRepo := createBareGitRepo(t, "main")
	workspace := filepath.Join(t.TempDir(), "workspace")
	checkout := filepath.Join(workspace, "entry-gateway")
	runGit(t, "", "clone", "--branch", "main", remoteRepo, checkout)
	fakeDockerEnv, _ := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.restore.pull",
		"--repo", "entry-gateway="+remoteRepo,
		"--branch", "entry-gateway=main",
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--pull", "--json", "env.restore.pull")
	var report struct {
		OK    bool `json:"ok"`
		Repos []struct {
			Action  string   `json:"action"`
			Exists  bool     `json:"exists"`
			Command []string `json:"command"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode restore pull json: %v\n%s", err, out)
	}
	if !report.OK || len(report.Repos) != 1 || !report.Repos[0].Exists || report.Repos[0].Action != "pull-existing-checkout" {
		t.Fatalf("restore pull report = %#v", report)
	}
	if strings.Join(report.Repos[0].Command, " ") != "git -C "+checkout+" pull --ff-only" {
		t.Fatalf("restore pull command = %#v", report.Repos[0].Command)
	}
}

func TestEnvironmentRestoreRejectsExistingCheckoutWithDifferentOrigin(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	remoteRepo := createBareGitRepo(t, "main")
	otherRepo := createBareGitRepo(t, "main")
	workspace := filepath.Join(t.TempDir(), "workspace")
	checkout := filepath.Join(workspace, "entry-gateway")
	runGit(t, "", "clone", "--branch", "main", otherRepo, checkout)
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.restore.origin",
		"--repo", "entry-gateway="+remoteRepo,
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIFails(t, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--json", "env.restore.origin")
	if !strings.Contains(out, "invalid-existing-checkout") || !strings.Contains(out, "origin mismatch") {
		t.Fatalf("origin mismatch restore output = %q", out)
	}
}

func TestEnvironmentRestoreStopsBeforeDockerWhenRepositoryPrecheckFails(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.restore.repo.precheck",
		"--repo", "entry-gateway="+filepath.Join(t.TempDir(), "missing.git"),
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIFailsWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.restore.repo.precheck")
	if !strings.Contains(out, "skipped-due-to-repository-error") || !strings.Contains(out, "repository") {
		t.Fatalf("repo precheck restore output = %q", out)
	}
	if raw, err := os.ReadFile(dockerCallsPath); err == nil {
		calls := string(raw)
		for _, forbidden := range []string{" pull", " build", " up -d", " down "} {
			if strings.Contains(calls, forbidden) {
				t.Fatalf("repo precheck failure should not run docker command %q:\n%s", forbidden, calls)
			}
		}
	}
	inspectOut := runCLI(t, "environment", "inspect", "--store", "sqlite://"+storePath, "--json", "env.restore.repo.precheck")
	if !strings.Contains(inspectOut, `"phase": "repository"`) {
		t.Fatalf("repo precheck failure should persist repository phase: %s", inspectOut)
	}
}

func TestEnvironmentRestoreChecksOutRequestedRefAfterClone(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	remoteRepo := createBareGitRepo(t, "main")
	work := filepath.Join(filepath.Dir(remoteRepo), "work")
	runGit(t, work, "tag", "v1")
	runGit(t, work, "push", "origin", "v1")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, _ := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.restore.ref",
		"--repo", "entry-gateway="+remoteRepo,
		"--branch", "entry-gateway=main",
		"--repo-ref", "entry-gateway=v1",
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.restore.ref")
	var report struct {
		OK    bool `json:"ok"`
		Repos []struct {
			Ref string `json:"ref"`
			OK  bool   `json:"ok"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode ref restore json: %v\n%s", err, out)
	}
	if !report.OK || len(report.Repos) != 1 || report.Repos[0].Ref != "v1" || !report.Repos[0].OK {
		t.Fatalf("ref restore report = %#v", report)
	}
	head := strings.TrimSpace(runGit(t, filepath.Join(workspace, "entry-gateway"), "rev-parse", "--abbrev-ref", "HEAD"))
	if head != "HEAD" {
		t.Fatalf("expected detached checkout at ref, got %q", head)
	}
}

func TestEnvironmentRestoreChecksOutRequestedRefForExistingCheckout(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	remoteRepo := createBareGitRepo(t, "main")
	work := filepath.Join(filepath.Dir(remoteRepo), "work")
	runGit(t, work, "tag", "v1")
	runGit(t, work, "push", "origin", "v1")
	writeFile(t, filepath.Join(work, "README.md"), "# restore fixture\n\nupdated\n")
	runGit(t, work, "add", "README.md")
	runGit(t, work, "-c", "user.name=Open Test", "-c", "user.email=open-test@example.com", "commit", "-m", "second")
	runGit(t, work, "push", "origin", "main")

	workspace := filepath.Join(t.TempDir(), "workspace")
	checkout := filepath.Join(workspace, "entry-gateway")
	runGit(t, "", "clone", "--branch", "main", remoteRepo, checkout)
	fakeDockerEnv, _ := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.restore.existing.ref",
		"--repo", "entry-gateway="+remoteRepo,
		"--branch", "entry-gateway=main",
		"--repo-ref", "entry-gateway=v1",
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.restore.existing.ref")
	var report struct {
		OK    bool `json:"ok"`
		Repos []struct {
			Action  string   `json:"action"`
			Exists  bool     `json:"exists"`
			Ref     string   `json:"ref"`
			Command []string `json:"command"`
			OK      bool     `json:"ok"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode existing ref restore json: %v\n%s", err, out)
	}
	if !report.OK || len(report.Repos) != 1 || !report.Repos[0].OK || !report.Repos[0].Exists || report.Repos[0].Action != "checkout-existing-ref" || report.Repos[0].Ref != "v1" {
		t.Fatalf("existing ref restore report = %#v", report)
	}
	command := strings.Join(report.Repos[0].Command, " ")
	for _, want := range []string{"git -C " + checkout + " fetch --tags origin", "git -C " + checkout + " checkout --detach v1"} {
		if !strings.Contains(command, want) {
			t.Fatalf("existing ref command missing %q: %#v", want, report.Repos[0].Command)
		}
	}
	head := strings.TrimSpace(runGit(t, checkout, "rev-parse", "--abbrev-ref", "HEAD"))
	if head != "HEAD" {
		t.Fatalf("expected detached checkout at ref, got %q", head)
	}
	tagCommit := strings.TrimSpace(runGit(t, checkout, "rev-parse", "v1^{commit}"))
	headCommit := strings.TrimSpace(runGit(t, checkout, "rev-parse", "HEAD"))
	if headCommit != tagCommit {
		t.Fatalf("expected checkout at v1, head=%s tag=%s", headCommit, tagCommit)
	}
}

func TestEnvironmentRestoreDetachesExistingCheckoutAlreadyAtRef(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	remoteRepo := createBareGitRepo(t, "main")
	work := filepath.Join(filepath.Dir(remoteRepo), "work")
	runGit(t, work, "tag", "v1")
	runGit(t, work, "push", "origin", "v1")
	workspace := filepath.Join(t.TempDir(), "workspace")
	checkout := filepath.Join(workspace, "entry-gateway")
	runGit(t, "", "clone", "--branch", "main", remoteRepo, checkout)
	fakeDockerEnv, _ := fakeDockerCommand(t)
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.restore.existing.ref.detach",
		"--repo", "entry-gateway="+remoteRepo,
		"--branch", "entry-gateway=main",
		"--repo-ref", "entry-gateway=v1",
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.restore.existing.ref.detach")
	var report struct {
		OK    bool `json:"ok"`
		Repos []struct {
			Action string `json:"action"`
			OK     bool   `json:"ok"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode existing same ref restore json: %v\n%s", err, out)
	}
	if !report.OK || len(report.Repos) != 1 || !report.Repos[0].OK || report.Repos[0].Action != "checkout-existing-ref" {
		t.Fatalf("existing same ref restore report = %#v", report)
	}
	head := strings.TrimSpace(runGit(t, checkout, "rev-parse", "--abbrev-ref", "HEAD"))
	if head != "HEAD" {
		t.Fatalf("expected detached checkout at ref, got %q", head)
	}
}

func TestEnvironmentRestorePreflightRequiresGitForExistingCheckoutRef(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	remoteRepo := createBareGitRepo(t, "main")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, _ := fakeDockerCommand(t)
	checkout := filepath.Join(workspace, "entry-gateway")
	runGit(t, "", "clone", "--branch", "main", remoteRepo, checkout)
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.restore.preflight.existing.ref",
		"--repo", "entry-gateway="+remoteRepo,
		"--repo-ref", "entry-gateway=v1",
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--json", "env.restore.preflight.existing.ref")
	var report struct {
		Preflight struct {
			Tools []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
				OK       bool   `json:"ok"`
			} `json:"tools"`
		} `json:"preflight"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode existing ref preflight json: %v\n%s", err, out)
	}
	if !restorePreflightHasTool(report.Preflight.Tools, "git", true) {
		t.Fatalf("existing ref preflight tools = %#v", report.Preflight.Tools)
	}
}

func TestEnvironmentRestoreAcceptsExistingCheckoutWithoutRepoURL(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	checkout := filepath.Join(workspace, "entry-gateway")
	fakeDockerEnv, _ := fakeDockerCommand(t)
	writeFile(t, filepath.Join(checkout, "README.md"), "# existing checkout\n")
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.existing.checkout",
		"--service", "entry-gateway",
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json", "env.existing.checkout")
	var report struct {
		OK    bool `json:"ok"`
		Repos []struct {
			ServiceID string `json:"serviceId"`
			Action    string `json:"action"`
			Exists    bool   `json:"exists"`
			OK        bool   `json:"ok"`
		} `json:"repos"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode existing checkout restore json: %v\n%s", err, out)
	}
	if !report.OK || len(report.Repos) != 1 || report.Repos[0].ServiceID != "entry-gateway" || report.Repos[0].Action != "use-existing-checkout" || !report.Repos[0].Exists || !report.Repos[0].OK {
		t.Fatalf("existing checkout restore report = %#v", report)
	}
}

func TestSandboxStartCommandRunsStartupCommandsFromStore(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	startedPath := filepath.Join(dir, "started.txt")
	platformStartedPath := filepath.Join(dir, "platform-started.txt")
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sandbox",
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{
				ID:             "entry-service",
				DisplayName:    "Entry Service",
				Kind:           "app",
				StartupCommand: fmt.Sprintf("printf entry-service > %q", startedPath),
				Status:         "active",
			},
			{
				ID:             "platform-service",
				DisplayName:    "Platform Service",
				Kind:           "platform",
				StartupCommand: fmt.Sprintf("printf platform-service > %q", platformStartedPath),
				Status:         "active",
			},
			{
				ID:          "documented-service",
				DisplayName: "Documented Service",
				Kind:        "external",
				Status:      "active",
			},
		},
	}); err != nil {
		t.Fatalf("replace catalog: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t, "sandbox", "start", "--store", "sqlite://"+storePath, "--json")

	var report struct {
		OK       bool `json:"ok"`
		Services []struct {
			ID       string `json:"id"`
			ExitCode int    `json:"exitCode"`
			Skipped  bool   `json:"skipped"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode sandbox start report: %v\n%s", err, out)
	}
	if !report.OK || len(report.Services) != 3 {
		t.Fatalf("sandbox start report = %#v", report)
	}
	byID := map[string]int{}
	skippedByID := map[string]bool{}
	for _, service := range report.Services {
		byID[service.ID] = service.ExitCode
		skippedByID[service.ID] = service.Skipped
	}
	if byID["entry-service"] != 0 || skippedByID["entry-service"] {
		t.Fatalf("entry-service result exit=%d skipped=%t", byID["entry-service"], skippedByID["entry-service"])
	}
	if byID["platform-service"] != 0 || skippedByID["platform-service"] {
		t.Fatalf("platform-service result exit=%d skipped=%t", byID["platform-service"], skippedByID["platform-service"])
	}
	if !skippedByID["documented-service"] {
		t.Fatalf("documented-service should be skipped without a startup command")
	}
	started, err := os.ReadFile(startedPath)
	if err != nil {
		t.Fatalf("read startup side effect: %v", err)
	}
	if string(started) != "entry-service" {
		t.Fatalf("startup command wrote %q", started)
	}
	platformStarted, err := os.ReadFile(platformStartedPath)
	if err != nil {
		t.Fatalf("read platform startup side effect: %v", err)
	}
	if string(platformStarted) != "platform-service" {
		t.Fatalf("platform startup command wrote %q", platformStarted)
	}
}

func TestSandboxStartUsesNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-sandbox-start-pg")
	dir := t.TempDir()
	startedPath := filepath.Join(dir, "started-pg.txt")
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	serviceID := "entry-service-pg-" + suffix

	ctx := context.Background()
	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open active PostgreSQL Store: %v", err)
	}
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sandbox-pg-" + suffix,
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{
				ID:             serviceID,
				DisplayName:    "Entry Service PG",
				Kind:           "app",
				StartupCommand: fmt.Sprintf("printf %s > %q", serviceID, startedPath),
				Status:         "active",
			},
		},
	}); err != nil {
		_ = s.Close()
		t.Fatalf("replace PostgreSQL catalog: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close PostgreSQL Store: %v", err)
	}

	out := runCLI(t, "sandbox", "start", "--service", serviceID, "--json")
	var report struct {
		OK       bool `json:"ok"`
		Services []struct {
			ID       string `json:"id"`
			ExitCode int    `json:"exitCode"`
			Skipped  bool   `json:"skipped"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode PostgreSQL sandbox start report: %v\n%s", err, out)
	}
	if !report.OK || len(report.Services) != 1 || report.Services[0].ID != serviceID || report.Services[0].ExitCode != 0 || report.Services[0].Skipped {
		t.Fatalf("PostgreSQL sandbox start report = %#v", report)
	}
	started, err := os.ReadFile(startedPath)
	if err != nil {
		t.Fatalf("read PostgreSQL startup side effect: %v", err)
	}
	if string(started) != serviceID {
		t.Fatalf("PostgreSQL startup command wrote %q want %q", started, serviceID)
	}
}

func TestSandboxRegisterCommandsWriteStoreCatalog(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")

	serviceOut := runCLI(t, "sandbox", "service", "register",
		"--store", "sqlite://"+storePath,
		"--id", "service.gateway",
		"--display-name", "Gateway",
		"--kind", "http",
		"--service-port", "18080",
		"--health-url", "http://127.0.0.1:18080/health",
	)
	if !strings.Contains(serviceOut, "Registered service: service.gateway") {
		t.Fatalf("service register output = %q", serviceOut)
	}

	interfaceOut := runCLI(t, "sandbox", "interface", "register",
		"--store", "sqlite://"+storePath,
		"--id", "node.create-order",
		"--service-id", "service.gateway",
		"--method", "POST",
		"--path", "/orders",
		"--case-id", "case.create-order",
		"--case-title", "Create order",
		"--required-for-admission",
	)
	if !strings.Contains(interfaceOut, "Registered interface: node.create-order") || !strings.Contains(interfaceOut, "Case: case.create-order") {
		t.Fatalf("interface register output = %q", interfaceOut)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	catalog, err := s.GetProfileCatalog(context.Background())
	if err != nil {
		t.Fatalf("get catalog: %v", err)
	}
	if catalog.ProfileID != "current" || len(catalog.Services) != 1 || catalog.Services[0].ID != "service.gateway" {
		t.Fatalf("catalog services = %#v", catalog)
	}
	if len(catalog.InterfaceNodes) != 1 || catalog.InterfaceNodes[0].ID != "node.create-order" || catalog.InterfaceNodes[0].ServiceID != "service.gateway" {
		t.Fatalf("catalog interface nodes = %#v", catalog.InterfaceNodes)
	}
	if len(catalog.RequestTemplates) != 1 || catalog.RequestTemplates[0].NodeID != "node.create-order" {
		t.Fatalf("catalog request templates = %#v", catalog.RequestTemplates)
	}
	if len(catalog.APICases) != 1 || catalog.APICases[0].ID != "case.create-order" || !catalog.APICases[0].RequiredForAdmission {
		t.Fatalf("catalog api cases = %#v", catalog.APICases)
	}
}

func TestSandboxRegisterCommandsUseNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-sandbox-register-pg")
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	serviceID := "service.gateway.pg." + suffix
	nodeID := "node.create-order.pg." + suffix
	caseID := "case.create-order.pg." + suffix

	serviceOut := runCLI(t, "sandbox", "service", "register",
		"--id", serviceID,
		"--display-name", "Gateway PG",
		"--kind", "http",
		"--service-port", "18080",
		"--health-url", "http://127.0.0.1:18080/health",
	)
	if !strings.Contains(serviceOut, "Registered service: "+serviceID) {
		t.Fatalf("PostgreSQL service register output = %q", serviceOut)
	}

	interfaceOut := runCLI(t, "sandbox", "interface", "register",
		"--id", nodeID,
		"--service-id", serviceID,
		"--method", "POST",
		"--path", "/orders",
		"--case-id", caseID,
		"--case-title", "Create order",
		"--required-for-admission",
	)
	if !strings.Contains(interfaceOut, "Registered interface: "+nodeID) || !strings.Contains(interfaceOut, "Case: "+caseID) {
		t.Fatalf("PostgreSQL interface register output = %q", interfaceOut)
	}

	s, err := openStore(context.Background(), storeRef)
	if err != nil {
		t.Fatalf("open PostgreSQL Store: %v", err)
	}
	defer s.Close()
	catalog, err := s.GetProfileCatalog(context.Background())
	if err != nil {
		t.Fatalf("get PostgreSQL catalog: %v", err)
	}
	serviceFound := false
	for _, service := range catalog.Services {
		if service.ID == serviceID {
			serviceFound = true
			break
		}
	}
	if !serviceFound {
		t.Fatalf("PostgreSQL catalog services = %#v", catalog.Services)
	}
	nodeFound := false
	for _, node := range catalog.InterfaceNodes {
		if node.ID == nodeID && node.ServiceID == serviceID {
			nodeFound = true
			break
		}
	}
	if !nodeFound {
		t.Fatalf("PostgreSQL catalog interface nodes = %#v", catalog.InterfaceNodes)
	}
	templateFound := false
	for _, template := range catalog.RequestTemplates {
		if template.NodeID == nodeID {
			templateFound = true
			break
		}
	}
	if !templateFound {
		t.Fatalf("PostgreSQL catalog request templates = %#v", catalog.RequestTemplates)
	}
	caseFound := false
	for _, apiCase := range catalog.APICases {
		if apiCase.ID == caseID && apiCase.RequiredForAdmission {
			caseFound = true
			break
		}
	}
	if !caseFound {
		t.Fatalf("PostgreSQL catalog api cases = %#v", catalog.APICases)
	}
}

func TestProfileInitCommandWritesExternalBundle(t *testing.T) {
	profileDir := filepath.Join(t.TempDir(), "external-profile")

	out := runCLI(t, "profile", "init", "--output", profileDir, "--id", "sample", "--display-name", "Sample Profile")
	if !strings.Contains(out, "Initialized external profile bundle: sample") || !strings.Contains(out, profileDir) {
		t.Fatalf("profile init output = %q", out)
	}
	for _, path := range []string{
		"profile.json",
		"README.md",
		".gitignore",
		"services",
		"workflows",
		"interface-nodes",
		"cases",
		"request-templates",
		"case-dependencies",
		"workflow-bindings",
		"fixtures",
	} {
		if _, err := os.Stat(filepath.Join(profileDir, path)); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	ignore, err := os.ReadFile(filepath.Join(profileDir, ".gitignore"))
	if err != nil {
		t.Fatalf("read generated ignore file: %v", err)
	}
	for _, want := range []string{".runtime/", "*.sqlite", "*.log"} {
		if !strings.Contains(string(ignore), want) {
			t.Fatalf("generated ignore file missing %q:\n%s", want, ignore)
		}
	}
	readme, err := os.ReadFile(filepath.Join(profileDir, "README.md"))
	if err != nil {
		t.Fatalf("read generated readme: %v", err)
	}
	if !strings.Contains(string(readme), "--store local-personal") || strings.Contains(string(readme), "--store-url .runtime/store.sqlite") {
		t.Fatalf("generated readme should use Store-first commands:\n%s", readme)
	}

	inspect := runCLI(t, "profile", "inspect", "--profile", profileDir)
	if !strings.Contains(inspect, "Profile: sample") || !strings.Contains(inspect, "Display Name: Sample Profile") {
		t.Fatalf("inspect generated profile = %q", inspect)
	}
}

func TestTemplatePackageCommandAliasesProfileLifecycle(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-template-package")
	writeWorkflowProfile(t, sourceDir)
	profileHome := filepath.Join(t.TempDir(), "template-package-home")

	install := runCLI(t, "template-package", "install", "--from", sourceDir, "--profile-home", profileHome)
	if !strings.Contains(install, "sample") || !strings.Contains(install, filepath.Join(profileHome, "sample")) {
		t.Fatalf("template-package install output = %q", install)
	}

	inspect := runCLI(t, "template-package", "inspect", "--template-package", "sample", "--profile-home", profileHome)
	if !strings.Contains(inspect, "sample") || !strings.Contains(inspect, "Workflows: 1") {
		t.Fatalf("template-package inspect output = %q", inspect)
	}

	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	verify := runCLI(t, "template-packages", "verify", "--template-package", "sample", "--profile-home", profileHome, "--store", "sqlite://"+dbPath)
	if !strings.Contains(verify, "OK: true") {
		t.Fatalf("template-packages verify output = %q", verify)
	}
}

func TestTemplatePackageCatalogIndexCommandReadsStoreCatalog(t *testing.T) {
	profileDir := filepath.Join(t.TempDir(), "template-package")
	writeWorkflowProfile(t, profileDir)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "template-package", "import", "--from", profileDir, "--store", "sqlite://"+storePath)

	out := runCLI(t, "template-package", "catalog-index", "--store", "sqlite://"+storePath, "--json")

	var report struct {
		ProfileID string `json:"profileId"`
		Counts    struct {
			Services       int `json:"services"`
			Workflows      int `json:"workflows"`
			InterfaceNodes int `json:"interfaceNodes"`
			APICases       int `json:"apiCases"`
		} `json:"counts"`
		ConfigVersion *struct {
			ProfileID string `json:"profileId"`
			Active    bool   `json:"active"`
		} `json:"configVersion"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode catalog-index json: %v\n%s", err, out)
	}
	if report.ProfileID != "sample" || report.Counts.Services != 0 || report.Counts.Workflows != 1 || report.Counts.InterfaceNodes != 1 || report.Counts.APICases != 1 {
		t.Fatalf("catalog-index report = %#v", report)
	}
	if report.ConfigVersion == nil || report.ConfigVersion.ProfileID != "sample" || !report.ConfigVersion.Active {
		t.Fatalf("catalog-index config version = %#v", report.ConfigVersion)
	}
}

func TestCaseRunsCommandListsStoredCaseRuns(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-runs-pg")
	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	runID := uniqueTestID(t, "run.case-runs")
	caseRunID := runID + ".case"
	started := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:           runID,
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: "/tmp/evidence/" + runID,
		StartedAt:    started,
		FinishedAt:   started.Add(time.Second),
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   caseRunID,
		RunID:                runID,
		CaseID:               "case.alpha",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"passed"}`,
		StartedAt:            started,
		FinishedAt:           started.Add(250 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record case run: %v", err)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        runID + ".evidence",
		RunID:     runID,
		CaseRunID: caseRunID,
		Kind:      "http-response",
		URI:       "/tmp/evidence/" + runID + "/response.json",
	}); err != nil {
		t.Fatalf("record evidence: %v", err)
	}

	out := runCLI(t, "case", "runs", "--run", runID, "--json")

	var report struct {
		OK       bool `json:"ok"`
		CaseRuns []struct {
			ID            string `json:"id"`
			RunID         string `json:"runId"`
			CaseID        string `json:"caseId"`
			Status        string `json:"status"`
			Operation     string `json:"operation"`
			EvidenceCount int    `json:"evidenceCount"`
			EvidencePath  string `json:"evidencePath"`
		} `json:"caseRuns"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode case runs json: %v\n%s", err, out)
	}
	if !report.OK || len(report.CaseRuns) != 1 {
		t.Fatalf("case runs report = %#v", report)
	}
	item := report.CaseRuns[0]
	if item.ID != caseRunID || item.RunID != runID || item.CaseID != "case.alpha" || item.Status != store.StatusPassed || item.Operation != "POST /alpha" || item.EvidenceCount != 1 || item.EvidencePath != "/tmp/evidence/"+runID {
		t.Fatalf("case run item = %#v", item)
	}
}

func TestCaseEvidenceCommandReadsCaseRunEvidence(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-evidence-pg")
	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	runID := uniqueTestID(t, "run.case-evidence")
	caseRunID := runID + ".case"
	if _, err := s.CreateRun(ctx, store.Run{
		ID:           runID,
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: "/tmp/evidence/" + runID,
		SummaryJSON:  "{}",
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   caseRunID,
		RunID:                runID,
		CaseID:               "case.alpha",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"GET","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"passed"}`,
	}); err != nil {
		t.Fatalf("record case run: %v", err)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        runID + ".response",
		RunID:     runID,
		CaseRunID: caseRunID,
		Kind:      "response",
		URI:       "/tmp/evidence/" + runID + "/response.json",
		MediaType: "application/json",
		Summary:   `{"statusCode":200}`,
	}); err != nil {
		t.Fatalf("record evidence: %v", err)
	}

	out := runCLI(t, "case", "evidence", "--case-run", caseRunID, "--json")

	var payload struct {
		OK       bool `json:"ok"`
		Evidence struct {
			Summary  map[string]any `json:"summary"`
			Request  map[string]any `json:"request"`
			Response map[string]any `json:"response"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode case evidence json: %v\n%s", err, out)
	}
	if !payload.OK || payload.Evidence.Summary["case_run_id"] != caseRunID || payload.Evidence.Summary["operation"] != "GET /alpha" {
		t.Fatalf("case evidence summary = %#v", payload.Evidence.Summary)
	}
	if payload.Evidence.Response["http_code"] != float64(200) || payload.Evidence.Response["evidence_uri"] != "/tmp/evidence/"+runID+"/response.json" {
		t.Fatalf("case evidence response = %#v", payload.Evidence.Response)
	}
}

func TestCaseTimingCommandSummarizesStoredCaseRuns(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-timing-pg")
	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	fastRunID := uniqueTestID(t, "run.fast")
	slowRunID := uniqueTestID(t, "run.slow")
	fastCaseID := uniqueTestID(t, "case.fast")
	slowCaseID := uniqueTestID(t, "case.slow")
	for _, item := range []struct {
		runID    string
		caseID   string
		duration time.Duration
	}{
		{runID: fastRunID, caseID: fastCaseID, duration: 200 * time.Millisecond},
		{runID: slowRunID, caseID: slowCaseID, duration: 1250 * time.Millisecond},
	} {
		started := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
		if _, err := s.CreateRun(ctx, store.Run{
			ID:         item.runID,
			ProfileID:  "sample",
			Status:     store.StatusPassed,
			StartedAt:  started,
			FinishedAt: started.Add(item.duration),
			CreatedAt:  started,
			UpdatedAt:  started.Add(item.duration),
		}); err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
			ID:         item.runID + ".case",
			RunID:      item.runID,
			CaseID:     item.caseID,
			Status:     store.StatusPassed,
			StartedAt:  started,
			FinishedAt: started.Add(item.duration),
			CreatedAt:  started,
		}); err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t, "case", "timing", "--kind", "case", "--json")

	var payload struct {
		OK      bool `json:"ok"`
		Summary struct {
			CaseRunCount          int            `json:"caseRunCount"`
			DurationMeasuredCount int            `json:"durationMeasuredCount"`
			MaxDurationMs         int            `json:"maxDurationMs"`
			SlowestRows           map[string]any `json:"slowestRows"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode case timing json: %v\n%s", err, out)
	}
	if !payload.OK || payload.Summary.CaseRunCount != 2 || payload.Summary.DurationMeasuredCount != 2 || payload.Summary.MaxDurationMs != 1250 {
		t.Fatalf("case timing summary = %#v", payload.Summary)
	}
	slowest := payload.Summary.SlowestRows["caseRun"].(map[string]any)
	if slowest["id"] != slowRunID+".case" || slowest["caseId"] != slowCaseID || slowest["durationMs"] != float64(1250) {
		t.Fatalf("case timing slowest = %#v", slowest)
	}
}

func TestCaseQueryCommandsAcceptStoreFlag(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	started := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:           "run-store-flag",
		ProfileID:    "sample",
		Status:       store.StatusPassed,
		EvidenceRoot: "/tmp/evidence/run-store-flag",
		StartedAt:    started,
		FinishedAt:   started.Add(time.Second),
		CreatedAt:    started,
		UpdatedAt:    started.Add(time.Second),
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "case-run-store-flag",
		RunID:                "run-store-flag",
		CaseID:               "case.alpha",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"GET","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"passed"}`,
		StartedAt:            started,
		FinishedAt:           started.Add(500 * time.Millisecond),
		CreatedAt:            started,
	}); err != nil {
		t.Fatalf("record case run: %v", err)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "response-store-flag",
		RunID:     "run-store-flag",
		CaseRunID: "case-run-store-flag",
		Kind:      "response",
		URI:       "/tmp/evidence/run-store-flag/response.json",
		MediaType: "application/json",
		Summary:   `{"statusCode":200}`,
	}); err != nil {
		t.Fatalf("record evidence: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	storeRef := "sqlite://" + storePath
	runsOut := runCLI(t, "case", "runs", "--store", storeRef, "--json")
	if !strings.Contains(runsOut, "case-run-store-flag") {
		t.Fatalf("case runs output = %q", runsOut)
	}
	evidenceOut := runCLI(t, "case", "evidence", "--store", storeRef, "--case-run", "case-run-store-flag", "--json")
	if !strings.Contains(evidenceOut, "case-run-store-flag") || !strings.Contains(evidenceOut, "/alpha") {
		t.Fatalf("case evidence output = %q", evidenceOut)
	}
	timingOut := runCLI(t, "case", "timing", "--store", storeRef, "--kind", "case", "--json")
	if !strings.Contains(timingOut, `"maxDurationMs": 500`) {
		t.Fatalf("case timing output = %q", timingOut)
	}
}

func TestCaseReadCommandsRejectActiveSQLiteStore(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "runs",
			args: []string{"case", "runs", "--json"},
		},
		{
			name: "evidence",
			args: []string{"case", "evidence", "--case-run", "case-run-legacy", "--json"},
		},
		{
			name: "timing",
			args: []string{"case", "timing", "--kind", "case", "--json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("OTSANDBOX_CONFIG_HOME", filepath.Join(dir, "config"))
			if err := saveStoreConfig(storeConfigFile{
				Active: "legacy-local",
				Stores: map[string]storeConfigEntry{
					"legacy-local": {Name: "legacy-local", URL: "sqlite://" + filepath.Join(dir, "store.sqlite"), Backend: "sqlite"},
				},
			}); err != nil {
				t.Fatalf("save store config: %v", err)
			}

			out := runCLIFails(t, tt.args...)
			for _, want := range []string{"daily commands require PostgreSQL Store", "SQLite", "postgres://"} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s output missing %q: %q", tt.name, want, out)
				}
			}
		})
	}
}

func TestProfileInitCommandRejectsCoreProfilesPath(t *testing.T) {
	out := runCLIFails(t, "profile", "init", "--output", "profiles/sample")
	if !strings.Contains(out, "outside this core repository") {
		t.Fatalf("profile init rejection output = %q", out)
	}
}

func TestProfileInstallCommandCopiesBundleIntoProfileHome(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	for _, path := range []string{
		filepath.Join(".runtime", "store.sqlite"),
		filepath.Join(".runtime", "evidence", "run.json"),
		filepath.Join(".git", "config"),
		"debug.log",
		"local.sqlite",
	} {
		fullPath := filepath.Join(sourceDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create generated state parent %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte("generated"), 0o644); err != nil {
			t.Fatalf("write generated state %s: %v", path, err)
		}
	}
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "install", "--from", sourceDir, "--profile-home", profileHome)
	if !strings.Contains(out, "Installed profile: sample") || !strings.Contains(out, filepath.Join(profileHome, "sample")) {
		t.Fatalf("profile install output = %q", out)
	}
	for _, path := range []string{"profile.json", filepath.Join("workflows", "workflow.json"), filepath.Join("cases", "case.json")} {
		if _, err := os.Stat(filepath.Join(profileHome, "sample", path)); err != nil {
			t.Fatalf("expected installed path %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(".runtime", "store.sqlite"),
		filepath.Join(".runtime", "evidence", "run.json"),
		filepath.Join(".git", "config"),
		"debug.log",
		"local.sqlite",
	} {
		if _, err := os.Stat(filepath.Join(profileHome, "sample", path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("generated state should not be installed at %s: %v", path, err)
		}
	}

	inspect := runCLI(t, "profile", "inspect", "--profile", "sample", "--profile-home", profileHome)
	if !strings.Contains(inspect, "Profile: sample") || !strings.Contains(inspect, "Workflows: 1") {
		t.Fatalf("inspect installed profile = %q", inspect)
	}

	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	verify := runCLI(t, "profile", "verify", "--profile", "sample", "--profile-home", profileHome, "--store", "sqlite://"+dbPath)
	if !strings.Contains(verify, "Profile Verification: sample") || !strings.Contains(verify, "OK: true") {
		t.Fatalf("verify installed profile = %q", verify)
	}
}

func TestProfilePackCommandWritesCleanArchive(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	for _, path := range []string{
		filepath.Join(".runtime", "store.sqlite"),
		"debug.log",
		"local.sqlite",
	} {
		fullPath := filepath.Join(sourceDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create generated state parent %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte("generated"), 0o644); err != nil {
			t.Fatalf("write generated state %s: %v", path, err)
		}
	}
	outputPath := filepath.Join(t.TempDir(), "sample-profile.tar.gz")

	out := runCLI(t, "profile", "pack", "--profile", sourceDir, "--output", outputPath, "--json")

	var report struct {
		ID           string `json:"id"`
		SourcePath   string `json:"sourcePath"`
		OutputPath   string `json:"outputPath"`
		BundleDigest string `json:"bundleDigest"`
		FileCount    int    `json:"fileCount"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile pack report: %v\n%s", err, out)
	}
	if report.ID != "sample" || report.SourcePath != sourceDir || report.OutputPath != outputPath || report.FileCount == 0 || !strings.HasPrefix(report.BundleDigest, "sha256:") {
		t.Fatalf("profile pack report = %#v", report)
	}
	entries := readTarGZEntries(t, outputPath)
	for _, want := range []string{"sample/profile.json", "sample/workflows/workflow.json", "sample/cases/case.json"} {
		if !containsString(entries, want) {
			t.Fatalf("profile archive missing %s: %#v", want, entries)
		}
	}
	for _, unwanted := range []string{"sample/.runtime/store.sqlite", "sample/debug.log", "sample/local.sqlite"} {
		if containsString(entries, unwanted) {
			t.Fatalf("profile archive included generated state %s: %#v", unwanted, entries)
		}
	}
}

func TestProfilePackCommandResolvesInstalledProfileID(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	profileHome := filepath.Join(t.TempDir(), "profile-home")
	runCLI(t, "profile", "install", "--from", sourceDir, "--profile-home", profileHome)
	outputPath := filepath.Join(t.TempDir(), "installed-profile.tar.gz")

	out := runCLI(t, "profile", "pack", "--profile", "sample", "--profile-home", profileHome, "--output", outputPath)

	if !strings.Contains(out, "Packed profile: sample") || !strings.Contains(out, outputPath) {
		t.Fatalf("profile pack installed output = %q", out)
	}
	if !containsString(readTarGZEntries(t, outputPath), "sample/profile.json") {
		t.Fatalf("installed profile archive missing manifest")
	}
}

func TestProfileInstallCommandAcceptsPackedArchive(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	archivePath := filepath.Join(t.TempDir(), "sample-profile.tar.gz")
	runCLI(t, "profile", "pack", "--profile", sourceDir, "--output", archivePath)
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "install", "--from", archivePath, "--profile-home", profileHome, "--json")

	var report struct {
		ID         string `json:"id"`
		SourcePath string `json:"sourcePath"`
		TargetPath string `json:"targetPath"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode archive install report: %v\n%s", err, out)
	}

	if report.ID != "sample" || report.SourcePath != archivePath || report.TargetPath != filepath.Join(profileHome, "sample") {
		t.Fatalf("profile install archive report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(profileHome, "sample", "profile.json")); err != nil {
		t.Fatalf("installed archive manifest missing: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	verify := runCLI(t, "profile", "verify", "--profile", "sample", "--profile-home", profileHome, "--store", "sqlite://"+dbPath)
	if !strings.Contains(verify, "Profile Verification: sample") || !strings.Contains(verify, "OK: true") {
		t.Fatalf("verify installed archive profile = %q", verify)
	}
}

func TestProfileInstallCommandRejectsUnsafeArchivePath(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "unsafe.tar.gz")
	writeTarGZEntries(t, archivePath, map[string]string{
		"sample/profile.json": `{"id":"sample","displayName":"Sample Profile"}`,
		"../escaped.txt":      "nope",
	})
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLIFails(t, "profile", "install", "--from", archivePath, "--profile-home", profileHome)

	if !strings.Contains(out, "escapes profile root") {
		t.Fatalf("unsafe archive output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(profileHome), "escaped.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe archive wrote escaped path: %v", err)
	}
}

func TestProfileListCommandReportsInstalledBundles(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	profileHome := filepath.Join(t.TempDir(), "profile-home")
	runCLI(t, "profile", "install", "--from", sourceDir, "--profile-home", profileHome)

	out := runCLI(t, "profile", "list", "--profile-home", profileHome, "--json")
	var report struct {
		ProfileHome string `json:"profileHome"`
		Profiles    []struct {
			ID           string `json:"id"`
			DisplayName  string `json:"displayName"`
			Path         string `json:"path"`
			BundleDigest string `json:"bundleDigest"`
			Counts       struct {
				Workflows int `json:"workflows"`
				APICases  int `json:"apiCases"`
			} `json:"counts"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile list: %v\n%s", err, out)
	}
	if report.ProfileHome != profileHome || len(report.Profiles) != 1 {
		t.Fatalf("profile list identity = %#v", report)
	}
	item := report.Profiles[0]
	if item.ID != "sample" || item.DisplayName != "Sample Profile" || item.Path != filepath.Join(profileHome, "sample") || !strings.HasPrefix(item.BundleDigest, "sha256:") {
		t.Fatalf("profile list item = %#v", item)
	}
	if item.Counts.Workflows != 1 || item.Counts.APICases != 1 {
		t.Fatalf("profile list counts = %#v", item.Counts)
	}
}

func TestProfileListCommandReportsInvalidInstalledBundle(t *testing.T) {
	profileHome := filepath.Join(t.TempDir(), "profile-home")
	brokenDir := filepath.Join(profileHome, "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("create broken profile dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "profile.json"), []byte(`{"id":`), 0o644); err != nil {
		t.Fatalf("write broken profile: %v", err)
	}

	out := runCLI(t, "profile", "list", "--profile-home", profileHome, "--json")
	var report struct {
		Profiles []struct {
			ID    string `json:"id"`
			Path  string `json:"path"`
			Valid bool   `json:"valid"`
			Error string `json:"error"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode invalid profile list report: %v\n%s", err, out)
	}
	if len(report.Profiles) != 1 || report.Profiles[0].ID != "broken" || report.Profiles[0].Path != brokenDir || report.Profiles[0].Valid || report.Profiles[0].Error == "" {
		t.Fatalf("invalid profile list report = %#v", report)
	}
}

func TestProfileInspectCommand(t *testing.T) {
	profileDir := writeEmptyProfileBundle(t)
	out := runCLI(t, "profile", "inspect", "--profile", profileDir)
	for _, want := range []string{"Profile: empty", "Display Name: Empty Profile", "Workflows: 0", "API Cases: 0", "Request Templates: 0", "Case Dependencies: 0", "Workflow Bindings: 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile inspect output missing %q: %q", want, out)
		}
	}
}

func TestProfileAuditCommandAcceptsPackedArchive(t *testing.T) {
	profileDir := writeEmptyProfileBundle(t)
	archivePath := filepath.Join(t.TempDir(), "empty-profile.tgz")
	runCLI(t, "profile", "pack", "--profile", profileDir, "--output", archivePath)
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "audit", "--profile", archivePath, "--offline-template-package", "--profile-home", profileHome, "--json")

	var report struct {
		ProfileID string `json:"profileId"`
		OK        bool   `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit archive report: %v\n%s", err, out)
	}
	targetPath := filepath.Join(profileHome, "empty")
	if report.ProfileID != "empty" || !report.OK {
		t.Fatalf("profile audit archive report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "profile.json")); err != nil {
		t.Fatalf("installed audit archive manifest missing: %v", err)
	}
}

func TestProfileVerifyCommandAuditsPublishesAndChecksReadModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)

	out := runCLI(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--json")

	var report struct {
		OK    bool `json:"ok"`
		Audit struct {
			OK         bool `json:"ok"`
			IssueCount int  `json:"issueCount"`
		} `json:"audit"`
		Publish struct {
			ProfileID  string   `json:"profileId"`
			ReadModels []string `json:"readModels"`
		} `json:"publish"`
		Checks []struct {
			Name   string `json:"name"`
			OK     bool   `json:"ok"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile verify report: %v\n%s", err, out)
	}
	if !report.OK || !report.Audit.OK || report.Audit.IssueCount != 0 || report.Publish.ProfileID != "empty" {
		t.Fatalf("profile verify report = %#v", report)
	}
	if strings.Join(report.Publish.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("profile verify read models = %#v", report.Publish.ReadModels)
	}
	if len(report.Checks) < 5 {
		t.Fatalf("profile verify checks = %#v", report.Checks)
	}
	for _, check := range report.Checks {
		if !check.OK || check.Detail == "" {
			t.Fatalf("profile verify check = %#v", check)
		}
	}
	if got := sqliteScalar(t, dbPath, "select value from kv where key = 'active_profile_id';"); got != "empty" {
		t.Fatalf("active profile id after verify = %q", got)
	}
}

func TestProfileVerifyCommandAcceptsPackedArchive(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)
	archivePath := filepath.Join(t.TempDir(), "empty-profile.tgz")
	runCLI(t, "profile", "pack", "--profile", profileDir, "--output", archivePath)
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "verify", "--profile", archivePath, "--profile-home", profileHome, "--store", "sqlite://"+dbPath, "--json")

	var report struct {
		OK      bool `json:"ok"`
		Publish struct {
			ProfileID  string `json:"profileId"`
			BundlePath string `json:"bundlePath"`
		} `json:"publish"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile verify archive report: %v\n%s", err, out)
	}
	targetPath := filepath.Join(profileHome, "empty")
	if !report.OK || report.Publish.ProfileID != "empty" || report.Publish.BundlePath != targetPath {
		t.Fatalf("profile verify archive report = %#v", report)
	}
	if got := sqliteScalar(t, dbPath, "select bundle_path from profile_indexes where profile_id = 'empty';"); got != targetPath {
		t.Fatalf("archive verify profile index path = %q, want %q", got, targetPath)
	}
}

func TestProfileVerifyCommandStopsBeforePublishWhenAuditFails(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.missing"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+storePath)
	if !strings.Contains(out, "profile audit failed") || !strings.Contains(out, "api-case-node-missing") {
		t.Fatalf("profile verify failure output = %q", out)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if _, err := s.GetProfileIndex(context.Background(), "sample"); err == nil {
		t.Fatalf("profile verify wrote profile index after audit failure")
	} else if err != store.ErrNotFound {
		t.Fatalf("get profile index after verify failure: %v", err)
	}
}

func TestProfileVerifyCommandCanRequirePassedAPICaseRuns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeInterfaceNodeCaseProfile(t)
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run-alpha",
		ProfileID:  "sample",
		WorkflowID: "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
		UpdatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
	}); err != nil {
		t.Fatalf("create alpha run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "case-run-alpha",
		RunID:      "run-alpha",
		CaseID:     "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
	}); err != nil {
		t.Fatalf("record alpha case run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	missing := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-case-runs")
	if !strings.Contains(missing, "api-case-run:case.beta") || !strings.Contains(missing, "no passed run") {
		t.Fatalf("missing case run verify output = %q", missing)
	}
	missingJSON := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-case-runs", "--json")
	for _, want := range []string{`"ok": false`, `"firstFailed": "api-case-run:case.beta"`, `"name": "api-case-run:case.beta"`} {
		if !strings.Contains(missingJSON, want) {
			t.Fatalf("missing case run json output does not contain %q:\n%s", want, missingJSON)
		}
	}

	s, err = sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run-beta",
		ProfileID:  "sample",
		WorkflowID: "case.beta",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:01:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:01:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:01:01Z"),
		UpdatedAt:  mustParseTime(t, "2026-05-14T01:01:01Z"),
	}); err != nil {
		t.Fatalf("create beta run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "case-run-beta",
		RunID:      "run-beta",
		CaseID:     "case.beta",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:01:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:01:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:01:01Z"),
	}); err != nil {
		t.Fatalf("record beta case run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}

	out := runCLI(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-case-runs", "--json")
	var report struct {
		OK      bool `json:"ok"`
		Summary struct {
			TotalChecks          int  `json:"totalChecks"`
			PassedChecks         int  `json:"passedChecks"`
			FailedChecks         int  `json:"failedChecks"`
			RequiredCaseRuns     bool `json:"requiredCaseRuns"`
			RequiredWorkflowRuns bool `json:"requiredWorkflowRuns"`
		} `json:"summary"`
		Checks []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile verify runtime report: %v\n%s", err, out)
	}
	if !report.OK || !hasProfileVerifyCheck(report.Checks, "api-case-run:case.alpha") || !hasProfileVerifyCheck(report.Checks, "api-case-run:case.beta") {
		t.Fatalf("profile verify runtime report = %#v", report)
	}
}

func TestProfileVerifyCommandCanRequirePassedWorkflowRuns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := filepath.Join(t.TempDir(), "profile")
	writeWorkflowProfile(t, profileDir)

	missing := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-workflow-runs")
	if !strings.Contains(missing, "workflow-run:workflow.alpha") || !strings.Contains(missing, "no passed run") {
		t.Fatalf("missing workflow run verify output = %q", missing)
	}

	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.workflow.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T02:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T02:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T02:00:01Z"),
		UpdatedAt:  mustParseTime(t, "2026-05-14T02:00:01Z"),
	}); err != nil {
		t.Fatalf("create workflow run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-workflow-runs", "--json")
	var report struct {
		OK      bool `json:"ok"`
		Summary struct {
			TotalChecks          int  `json:"totalChecks"`
			PassedChecks         int  `json:"passedChecks"`
			FailedChecks         int  `json:"failedChecks"`
			RequiredCaseRuns     bool `json:"requiredCaseRuns"`
			RequiredWorkflowRuns bool `json:"requiredWorkflowRuns"`
		} `json:"summary"`
		Checks []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile verify workflow report: %v\n%s", err, out)
	}
	if !report.OK || !hasProfileVerifyCheck(report.Checks, "workflow-run:workflow.alpha") {
		t.Fatalf("profile verify workflow report = %#v", report)
	}
	if report.Summary.TotalChecks != len(report.Checks) || report.Summary.PassedChecks != len(report.Checks) || report.Summary.FailedChecks != 0 {
		t.Fatalf("profile verify summary counts = %#v checks=%d", report.Summary, len(report.Checks))
	}
	if !report.Summary.RequiredWorkflowRuns || report.Summary.RequiredCaseRuns {
		t.Fatalf("profile verify summary gates = %#v", report.Summary)
	}
}

func TestProfileImportAndVerifyUseNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-profile-pg")

	importDir := writeEmptyProfileBundle(t)
	importOut := runCLI(t, "profile", "import", "--from", importDir, "--json")
	var importReport struct {
		ProfileID  string   `json:"profileId"`
		BundlePath string   `json:"bundlePath"`
		ReadModels []string `json:"readModels"`
	}
	if err := json.Unmarshal([]byte(importOut), &importReport); err != nil {
		t.Fatalf("decode PostgreSQL profile import json: %v\n%s", err, importOut)
	}
	if importReport.ProfileID != "empty" || importReport.BundlePath != importDir || strings.Join(importReport.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("PostgreSQL profile import report = %#v", importReport)
	}

	ctx := context.Background()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open PostgreSQL profile Store: %v", err)
	}
	index, err := runtime.GetProfileIndex(ctx, "empty")
	if err != nil {
		t.Fatalf("get PostgreSQL profile index: %v", err)
	}
	if index.BundlePath != importDir || !strings.HasPrefix(index.BundleDigest, "sha256:") {
		t.Fatalf("PostgreSQL profile index = %#v", index)
	}
	catalogIndex, err := runtime.GetProfileCatalogIndex(ctx)
	if err != nil {
		t.Fatalf("get PostgreSQL profile catalog index: %v", err)
	}
	if catalogIndex.ProfileID != "empty" {
		t.Fatalf("PostgreSQL profile catalog index = %#v", catalogIndex)
	}

	verifyDir := writeInterfaceNodeCaseProfile(t)
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	base := mustParseTime(t, "2026-05-18T03:00:00Z")
	recordCaseRunForCoverage(t, ctx, runtime, "run.pg.alpha."+suffix, "case.alpha", store.StatusPassed, base)
	recordCaseRunForCoverage(t, ctx, runtime, "run.pg.beta."+suffix, "case.beta", store.StatusPassed, base.Add(time.Minute))
	if err := runtime.Close(); err != nil {
		t.Fatalf("close PostgreSQL profile Store: %v", err)
	}

	verifyOut := runCLI(t, "profile", "verify", "--profile", verifyDir, "--require-case-runs", "--json")
	var verifyReport struct {
		OK      bool `json:"ok"`
		Publish struct {
			ProfileID  string   `json:"profileId"`
			ReadModels []string `json:"readModels"`
		} `json:"publish"`
		Summary struct {
			RequiredCaseRuns bool `json:"requiredCaseRuns"`
			FailedChecks     int  `json:"failedChecks"`
		} `json:"summary"`
		Checks []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(verifyOut), &verifyReport); err != nil {
		t.Fatalf("decode PostgreSQL profile verify json: %v\n%s", err, verifyOut)
	}
	if !verifyReport.OK || verifyReport.Publish.ProfileID != "sample" || strings.Join(verifyReport.Publish.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("PostgreSQL profile verify report = %#v", verifyReport)
	}
	if !verifyReport.Summary.RequiredCaseRuns || verifyReport.Summary.FailedChecks != 0 {
		t.Fatalf("PostgreSQL profile verify summary = %#v", verifyReport.Summary)
	}
	if !hasProfileVerifyCheck(verifyReport.Checks, "api-case-run:case.alpha") || !hasProfileVerifyCheck(verifyReport.Checks, "api-case-run:case.beta") {
		t.Fatalf("PostgreSQL profile verify checks = %#v", verifyReport.Checks)
	}

	runtime, err = openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("reopen PostgreSQL profile Store: %v", err)
	}
	defer runtime.Close()
	verifiedIndex, err := runtime.GetProfileIndex(ctx, "sample")
	if err != nil {
		t.Fatalf("get verified PostgreSQL profile index: %v", err)
	}
	if verifiedIndex.BundlePath != verifyDir || !strings.HasPrefix(verifiedIndex.BundleDigest, "sha256:") {
		t.Fatalf("verified PostgreSQL profile index = %#v", verifiedIndex)
	}
	verifiedCatalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get verified PostgreSQL profile catalog: %v", err)
	}
	if verifiedCatalog.ProfileID != "sample" || len(verifiedCatalog.APICases) != 2 {
		t.Fatalf("verified PostgreSQL profile catalog = %#v", verifiedCatalog)
	}
}

func TestProfileImportCommandIndexesBundleInStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)

	out := runCLI(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+dbPath)
	if !strings.Contains(out, "Imported profile: empty") {
		t.Fatalf("profile import output = %q", out)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	index, err := s.GetProfileIndex(context.Background(), "empty")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if index.BundlePath == "" || !strings.HasPrefix(index.BundleDigest, "sha256:") {
		t.Fatalf("profile index = %#v", index)
	}
	if got := sqliteScalar(t, dbPath, "select value from kv where key = 'active_profile_id';"); got != "empty" {
		t.Fatalf("active profile catalog index = %q", got)
	}
}

func TestProfileImportCommandAcceptsPackedArchive(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)
	archivePath := filepath.Join(t.TempDir(), "empty-profile.tar.gz")
	runCLI(t, "profile", "pack", "--profile", profileDir, "--output", archivePath)
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "import", "--from", archivePath, "--profile-home", profileHome, "--store", "sqlite://"+dbPath, "--json")

	var report struct {
		ProfileID  string `json:"profileId"`
		BundlePath string `json:"bundlePath"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile import archive report: %v\n%s", err, out)
	}
	targetPath := filepath.Join(profileHome, "empty")
	if report.ProfileID != "empty" || report.BundlePath != targetPath {
		t.Fatalf("profile import archive report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "profile.json")); err != nil {
		t.Fatalf("installed archive manifest missing: %v", err)
	}
	if got := sqliteScalar(t, dbPath, "select source_path from config_versions where active = 1;"); got != targetPath {
		t.Fatalf("archive import config source path = %q, want %q", got, targetPath)
	}
}

func TestConfigPublishCommandIndexesBundleInStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)

	out := runCLI(t, "config", "publish", "--from", profileDir, "--store", "sqlite://"+dbPath, "--json")

	var report struct {
		ProfileID     string   `json:"profileId"`
		BundleDigest  string   `json:"bundleDigest"`
		ReadModels    []string `json:"readModels"`
		ConfigVersion struct {
			ID           string `json:"id"`
			ProfileID    string `json:"profileId"`
			BundleDigest string `json:"bundleDigest"`
			Active       bool   `json:"active"`
		} `json:"configVersion"`
		CatalogIndex struct {
			ProfileID string `json:"profileId"`
		} `json:"catalogIndex"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode config publish report: %v\n%s", err, out)
	}
	if report.ProfileID != "empty" || report.CatalogIndex.ProfileID != "empty" || !strings.HasPrefix(report.BundleDigest, "sha256:") {
		t.Fatalf("config publish report = %#v", report)
	}
	if report.ConfigVersion.ID == "" || report.ConfigVersion.ProfileID != "empty" || report.ConfigVersion.BundleDigest != report.BundleDigest || !report.ConfigVersion.Active {
		t.Fatalf("config version = %#v", report.ConfigVersion)
	}
	if strings.Join(report.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("config publish read models = %#v", report.ReadModels)
	}
	if got := sqliteScalar(t, dbPath, "select value from kv where key = 'active_profile_id';"); got != "empty" {
		t.Fatalf("active config profile = %q", got)
	}
	if got := sqliteScalar(t, dbPath, "select bundle_digest from config_versions where active = 1;"); got != report.BundleDigest {
		t.Fatalf("active config digest = %q, want %q", got, report.BundleDigest)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'empty' and model_key = 'interface-nodes';"); got != report.ConfigVersion.ID {
		t.Fatalf("interface nodes read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'empty' and model_key = 'catalog';"); got != report.ConfigVersion.ID {
		t.Fatalf("catalog read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
}

func TestConfigPublishCommandMaterializesInterfaceNodeDetailReadModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeInterfaceNodeCaseProfile(t)

	out := runCLI(t, "config", "publish", "--from", profileDir, "--store", "sqlite://"+dbPath, "--json")

	var report struct {
		ConfigVersion struct {
			ID string `json:"id"`
		} `json:"configVersion"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode config publish report: %v\n%s", err, out)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'sample' and model_key = 'interface-node:node.alpha';"); got != report.ConfigVersion.ID {
		t.Fatalf("interface node detail read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
	if got := sqliteScalar(t, dbPath, "select json_extract(payload_json, '$.source.kind') from config_read_model where profile_id = 'sample' and model_key = 'interface-node:node.alpha';"); got != "read-model" {
		t.Fatalf("interface node detail source kind = %q", got)
	}
}

func TestConfigPublishCommandMaterializesInterfaceNodeCoverageReadModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeInterfaceNodeCoverageProfile(t)

	out := runCLI(t, "config", "publish", "--from", profileDir, "--store", "sqlite://"+dbPath, "--json")

	var report struct {
		ConfigVersion struct {
			ID string `json:"id"`
		} `json:"configVersion"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode config publish report: %v\n%s", err, out)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'sample' and model_key = 'interface-node-coverage:workflow.alpha';"); got != report.ConfigVersion.ID {
		t.Fatalf("interface node coverage read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
	if got := sqliteScalar(t, dbPath, "select json_extract(payload_json, '$.source.kind') from config_read_model where profile_id = 'sample' and model_key = 'interface-node-coverage-gaps:workflow.alpha';"); got != "read-model" {
		t.Fatalf("interface node coverage gaps source kind = %q", got)
	}
}

func TestInterfaceNodeCoverageCommandCanEmitJSON(t *testing.T) {
	profileDir := writeInterfaceNodeCoverageProfile(t)
	configureNamedPostgreSQLActiveStore(t, "daily-interface-coverage-pg")
	runCLI(t, "config", "publish", "--from", profileDir)

	out := runCLI(t, "interface-node", "coverage", "--workflow", "workflow.alpha", "--json")

	var report struct {
		OK      bool `json:"ok"`
		Summary struct {
			TotalSteps  int `json:"totalSteps"`
			MappedSteps int `json:"mappedSteps"`
		} `json:"summary"`
		Rows []struct {
			WorkflowID string `json:"workflowId"`
			StepID     string `json:"stepId"`
			NodeID     string `json:"nodeId"`
			Mapped     bool   `json:"mapped"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode interface-node coverage json: %v\n%s", err, out)
	}
	if !report.OK || report.Summary.TotalSteps != 1 || report.Summary.MappedSteps != 1 {
		t.Fatalf("coverage summary = %#v", report.Summary)
	}
	if len(report.Rows) != 1 || report.Rows[0].WorkflowID != "workflow.alpha" || report.Rows[0].StepID != "step.alpha" || report.Rows[0].NodeID != "node.alpha" || !report.Rows[0].Mapped {
		t.Fatalf("coverage rows = %#v", report.Rows)
	}
}

func TestInterfaceNodeCoverageGapsCommandCanEmitJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.missing","nodeId":"node.missing","caseId":"case.missing","required":true}],
  "fixtures": []
}`)
	configureNamedPostgreSQLActiveStore(t, "daily-interface-coverage-gaps-pg")
	runCLI(t, "config", "publish", "--from", dir)

	out := runCLI(t, "interface-node", "coverage-gaps", "--workflow", "workflow.alpha", "--json")

	var report struct {
		OK      bool `json:"ok"`
		Summary struct {
			TotalSteps int `json:"totalSteps"`
			GapCount   int `json:"gapCount"`
		} `json:"summary"`
		Gaps []struct {
			StepID string `json:"stepId"`
			NodeID string `json:"nodeId"`
			Mapped bool   `json:"mapped"`
		} `json:"gaps"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode interface-node coverage gaps json: %v\n%s", err, out)
	}
	if !report.OK || report.Summary.TotalSteps != 1 || report.Summary.GapCount != 1 {
		t.Fatalf("coverage gaps summary = %#v", report.Summary)
	}
	if len(report.Gaps) != 1 || report.Gaps[0].StepID != "step.missing" || report.Gaps[0].Mapped {
		t.Fatalf("coverage gaps = %#v", report.Gaps)
	}
}

func TestProfileImportCommandCanEmitJSONReport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)

	out := runCLI(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+dbPath, "--json")

	var report struct {
		ProfileID    string `json:"profileId"`
		BundlePath   string `json:"bundlePath"`
		BundleDigest string `json:"bundleDigest"`
		Counts       struct {
			Services         int `json:"services"`
			Workflows        int `json:"workflows"`
			InterfaceNodes   int `json:"interfaceNodes"`
			APICases         int `json:"apiCases"`
			RequestTemplates int `json:"requestTemplates"`
			CaseDependencies int `json:"caseDependencies"`
			WorkflowBindings int `json:"workflowBindings"`
			Fixtures         int `json:"fixtures"`
		} `json:"counts"`
		CatalogIndex struct {
			ProfileID   string `json:"profileId"`
			IndexedAt   string `json:"indexedAt"`
			StoreCounts struct {
				Services        int `json:"services"`
				Workflows       int `json:"workflows"`
				Templates       int `json:"templates"`
				TemplateConfigs int `json:"templateConfigs"`
			} `json:"counts"`
		} `json:"catalogIndex"`
		StorePath  string   `json:"storePath"`
		ImportedAt string   `json:"importedAt"`
		ReadModels []string `json:"readModels"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile import json: %v\n%s", err, out)
	}
	if report.ProfileID != "empty" || report.BundlePath != profileDir {
		t.Fatalf("report profile/path = %#v", report)
	}
	if !strings.HasPrefix(report.BundleDigest, "sha256:") || report.StorePath != "sqlite://"+dbPath || report.ImportedAt == "" {
		t.Fatalf("report digest/store/import time = %#v", report)
	}
	if report.Counts.Services != 0 || report.Counts.APICases != 0 || report.Counts.WorkflowBindings != 0 {
		t.Fatalf("report counts = %#v", report.Counts)
	}
	if report.CatalogIndex.ProfileID != "empty" || report.CatalogIndex.IndexedAt == "" {
		t.Fatalf("report catalog index identity = %#v", report.CatalogIndex)
	}
	if report.CatalogIndex.StoreCounts.Services != 0 || report.CatalogIndex.StoreCounts.Templates != 0 || report.CatalogIndex.StoreCounts.TemplateConfigs != 0 {
		t.Fatalf("report catalog index counts = %#v", report.CatalogIndex.StoreCounts)
	}
	if strings.Join(report.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("profile import read models = %#v", report.ReadModels)
	}
}

func TestInterfaceNodeCaseAuditReportsMissingExecutionConfigs(t *testing.T) {
	dir := writeInterfaceNodeCaseProfile(t)

	out := runCLI(t, "interface-node", "case", "audit", "--profile", dir, "--node", "node.alpha", "--json")

	var report struct {
		OK     bool   `json:"ok"`
		NodeID string `json:"nodeId"`
		Counts struct {
			Cases      int `json:"cases"`
			Configured int `json:"configured"`
			Missing    int `json:"missing"`
		} `json:"counts"`
		Missing []struct {
			CaseID string `json:"caseId"`
		} `json:"missing"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode interface node case audit json: %v\n%s", err, out)
	}
	if report.OK || report.NodeID != "node.alpha" || report.Counts.Cases != 2 || report.Counts.Configured != 1 || report.Counts.Missing != 1 {
		t.Fatalf("audit report = %#v", report)
	}
	if len(report.Missing) != 1 || report.Missing[0].CaseID != "case.beta" {
		t.Fatalf("missing cases = %#v", report.Missing)
	}
}

func TestInterfaceNodeCaseApplyMergesExecutionConfigsIntoProfileCatalog(t *testing.T) {
	dir := writeInterfaceNodeCaseProfile(t)
	requestPath := filepath.Join(t.TempDir(), "case-config.json")
	writeFile(t, requestPath, `{
  "templateConfigs": [
    {
      "id": "cfg.case.beta",
      "templateId": "case-execution",
      "nodeId": "node.alpha",
      "scopeType": "case",
      "scopeId": "case.beta",
      "title": "Case Beta execution",
      "status": "active",
      "sortOrder": 2,
      "config": {
        "caseId": "case.beta",
        "caseExecution": {
          "method": "GET",
          "nodeId": "service.alpha",
          "path": "/beta",
          "expectedHttpCodes": [200]
        }
      }
    }
  ]
}`)

	out := runCLI(t, "interface-node", "case", "apply", "--profile", dir, "--file", requestPath, "--json")

	var result struct {
		Applied int    `json:"applied"`
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode interface node case apply json: %v\n%s", err, out)
	}
	if result.Applied != 1 || result.Profile != dir {
		t.Fatalf("apply result = %#v", result)
	}
	audit := runCLI(t, "interface-node", "case", "audit", "--profile", dir, "--node", "node.alpha", "--json")
	var auditReport struct {
		OK     bool `json:"ok"`
		Counts struct {
			Missing int `json:"missing"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(audit), &auditReport); err != nil {
		t.Fatalf("decode audit after apply: %v\n%s", err, audit)
	}
	if !auditReport.OK || auditReport.Counts.Missing != 0 {
		t.Fatalf("audit after apply = %s", audit)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var catalog struct {
		TemplateConfigs []struct {
			ConfigJSON string `json:"configJson"`
		} `json:"templateConfigs"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("decode catalog after apply: %v\n%s", err, raw)
	}
	hasBeta := false
	for _, item := range catalog.TemplateConfigs {
		var config struct {
			CaseID string `json:"caseId"`
		}
		if err := json.Unmarshal([]byte(item.ConfigJSON), &config); err != nil {
			t.Fatalf("decode template config after apply: %v\n%s", err, item.ConfigJSON)
		}
		hasBeta = hasBeta || config.CaseID == "case.beta"
	}
	if !hasBeta || strings.Contains(string(raw), "store.sqlite") {
		t.Fatalf("catalog after apply = %s", raw)
	}
}

func TestInterfaceNodeCaseDraftAndApplyCreatesRunnableMaintainedCase(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha","method":"POST","path":"/v1/items","sortOrder":7}],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	bundlePath := filepath.Join(t.TempDir(), "case-draft.json")

	out := runCLI(t,
		"interface-node", "case", "draft",
		"--profile", dir,
		"--node", "node.alpha",
		"--case-id", "case.generated",
		"--title", "Generated Case",
		"--tag", "regression",
		"--tag", "smoke",
		"--priority", "p1",
		"--owner", "team-a",
		"--output", bundlePath,
		"--json",
	)
	var draft struct {
		OK             bool   `json:"ok"`
		CaseID         string `json:"caseId"`
		NodeID         string `json:"nodeId"`
		BundlePath     string `json:"bundlePath"`
		CasePath       string `json:"casePath"`
		TemplateConfig struct {
			ConfigJSON string `json:"configJson"`
		} `json:"templateConfig"`
		CaseFile struct {
			Path string       `json:"path"`
			Case apicase.Case `json:"case"`
		} `json:"caseFile"`
	}
	if err := json.Unmarshal([]byte(out), &draft); err != nil {
		t.Fatalf("decode case draft json: %v\n%s", err, out)
	}
	if !draft.OK || draft.CaseID != "case.generated" || draft.NodeID != "node.alpha" || draft.BundlePath != bundlePath || draft.CasePath != "api-cases/case.generated.json" {
		t.Fatalf("case draft = %#v", draft)
	}
	if draft.CaseFile.Path != draft.CasePath || draft.CaseFile.Case.Request.Method != "POST" || draft.CaseFile.Case.Request.Path != "/v1/items" {
		t.Fatalf("case draft file = %#v", draft.CaseFile)
	}
	if !strings.Contains(draft.TemplateConfig.ConfigJSON, `"caseId":"case.generated"`) || !strings.Contains(draft.TemplateConfig.ConfigJSON, `"expectedHttpCodes":[200]`) {
		t.Fatalf("case draft config json = %s", draft.TemplateConfig.ConfigJSON)
	}
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("draft bundle missing: %v", err)
	}

	applyOut := runCLI(t, "interface-node", "case", "apply", "--profile", dir, "--file", bundlePath, "--json")
	var applied struct {
		Applied int `json:"applied"`
		Cases   int `json:"cases"`
		Files   int `json:"files"`
	}
	if err := json.Unmarshal([]byte(applyOut), &applied); err != nil {
		t.Fatalf("decode apply draft json: %v\n%s", err, applyOut)
	}
	if applied.Applied != 1 || applied.Cases != 1 || applied.Files != 1 {
		t.Fatalf("apply draft result = %#v", applied)
	}
	if _, err := os.Stat(filepath.Join(dir, "api-cases", "case.generated.json")); err != nil {
		t.Fatalf("applied runnable case file missing: %v", err)
	}
	loaded, err := profile.Load(dir)
	if err != nil {
		t.Fatalf("load applied profile: %v", err)
	}
	if len(loaded.APICases) != 1 || loaded.APICases[0].ID != "case.generated" || loaded.APICases[0].CasePath != "api-cases/case.generated.json" || loaded.APICases[0].Owner != "team-a" {
		t.Fatalf("loaded applied cases = %#v", loaded.APICases)
	}
	audit := runCLI(t, "interface-node", "case", "audit", "--profile", dir, "--node", "node.alpha", "--json")
	var auditReport struct {
		OK     bool `json:"ok"`
		Counts struct {
			Cases      int `json:"cases"`
			Configured int `json:"configured"`
			Missing    int `json:"missing"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(audit), &auditReport); err != nil {
		t.Fatalf("decode audit after draft apply: %v\n%s", err, audit)
	}
	if !auditReport.OK || auditReport.Counts.Cases != 1 || auditReport.Counts.Configured != 1 || auditReport.Counts.Missing != 0 {
		t.Fatalf("audit after draft apply = %#v", auditReport)
	}
}

func TestProfileImportCommandCanAuditImportedProfile(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
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
}`)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+storePath, "--json", "--audit")

	var report struct {
		ProfileID string `json:"profileId"`
		Audit     *struct {
			OK         bool `json:"ok"`
			IssueCount int  `json:"issueCount"`
			Issues     []struct {
				Code      string `json:"code"`
				SubjectID string `json:"subjectId"`
			} `json:"issues"`
			Store *struct {
				ProfileIndexed bool `json:"profileIndexed"`
				DigestMatches  bool `json:"digestMatches"`
			} `json:"store,omitempty"`
		} `json:"audit,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode audited profile import json: %v\n%s", err, out)
	}
	if report.ProfileID != "sample" || report.Audit == nil {
		t.Fatalf("report missing audit = %#v", report)
	}
	if report.Audit.OK || report.Audit.IssueCount != 2 || len(report.Audit.Issues) != 2 {
		t.Fatalf("audit summary = %#v", report.Audit)
	}
	if report.Audit.Issues[0].Code != "api-case-node-missing" || report.Audit.Issues[1].Code != "case-dependency-fixture-missing" {
		t.Fatalf("audit issues = %#v", report.Audit.Issues)
	}
	if report.Audit.Store == nil || !report.Audit.Store.ProfileIndexed || !report.Audit.Store.DigestMatches {
		t.Fatalf("audit store = %#v", report.Audit.Store)
	}

	text := runCLI(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+storePath, "--audit")
	for _, want := range []string{"Imported profile: sample", "Audit OK: false", "Audit Issues: 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("audited text import output missing %q: %q", want, text)
		}
	}
}

func TestProfileImportCommandCanRequireCleanAuditBeforeWritingStore(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.missing"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLIFails(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+storePath, "--require-audit-ok")
	if !strings.Contains(out, "profile audit failed") || !strings.Contains(out, "api-case-node-missing") {
		t.Fatalf("strict profile import output = %q", out)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if _, err := s.GetProfileIndex(context.Background(), "sample"); err == nil {
		t.Fatalf("strict profile import wrote profile index")
	} else if err != store.ErrNotFound {
		t.Fatalf("get profile index after strict failure: %v", err)
	}
}

func TestProfileAuditCommandEmitsJSONWithStoreState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	alphaPath := filepath.Join(dir, "case.alpha.json")
	betaPath := filepath.Join(dir, "case.beta.json")
	writeAPICaseFile(t, alphaPath)
	writeFile(t, betaPath, `{
  "id": "case.beta",
  "title": "Read Item",
  "request": {"method": "GET", "path": "/v1/items/item-001"},
  "assertions": {"expectedStatusCodes": [200]}
}`)
	writeFile(t, filepath.Join(profileDir, "profile.json"), fmt.Sprintf(`{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha","casePath":%q},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.alpha","casePath":%q}
  ],
  "requestTemplates": [{"id":"template.alpha","nodeId":"node.alpha","method":"POST","path":"/v1/items"}],
  "caseDependencies": [{"id":"dependency.beta","caseId":"case.beta","fixtureId":"fixture.missing"}],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.beta","required":true}],
  "fixtures": []
}`, alphaPath, betaPath))

	storePath := filepath.Join(dir, "store.sqlite")
	runCLI(t, "profile", "import", "--from", profileDir, "--store", "sqlite://"+storePath)
	runCLI(t, "case", "run", "--case", alphaPath, "--base-url", server.URL, "--run-id", "run-alpha", "--store", "sqlite://"+storePath, "--profile", "sample")

	out := runCLI(t, "profile", "audit", "--profile", profileDir, "--offline-template-package", "--store", "sqlite://"+storePath, "--json")

	var report struct {
		OK         bool `json:"ok"`
		IssueCount int  `json:"issueCount"`
		Issues     []struct {
			Code      string `json:"code"`
			SubjectID string `json:"subjectId"`
		} `json:"issues"`
		Store *struct {
			ProfileIndexed bool `json:"profileIndexed"`
			DigestMatches  bool `json:"digestMatches"`
			APICases       []struct {
				CaseID       string `json:"caseId"`
				HasPassed    bool   `json:"hasPassed"`
				LatestStatus string `json:"latestStatus"`
			} `json:"apiCases"`
		} `json:"store,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit json: %v\n%s", err, out)
	}
	if report.OK || report.IssueCount != 1 || len(report.Issues) != 1 {
		t.Fatalf("audit report issues = %#v", report)
	}
	if report.Issues[0].Code != "case-dependency-fixture-missing" || report.Issues[0].SubjectID != "dependency.beta" {
		t.Fatalf("audit issue = %#v", report.Issues[0])
	}
	if report.Store == nil || !report.Store.ProfileIndexed || !report.Store.DigestMatches {
		t.Fatalf("audit store state = %#v", report.Store)
	}
	caseState := map[string]struct {
		HasPassed    bool
		LatestStatus string
	}{}
	for _, item := range report.Store.APICases {
		caseState[item.CaseID] = struct {
			HasPassed    bool
			LatestStatus string
		}{HasPassed: item.HasPassed, LatestStatus: item.LatestStatus}
	}
	if !caseState["case.alpha"].HasPassed || caseState["case.alpha"].LatestStatus != "passed" {
		t.Fatalf("case.alpha state = %#v", caseState["case.alpha"])
	}
	if caseState["case.beta"].HasPassed || caseState["case.beta"].LatestStatus != "" {
		t.Fatalf("case.beta state = %#v", caseState["case.beta"])
	}
}

func TestProfileAuditPlanCommandSuggestsRepairActions(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.missing"}],
  "requestTemplates": [],
  "caseDependencies": [{"id":"dependency.alpha","caseId":"case.alpha","fixtureId":""}],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"","nodeId":"node.alpha","caseId":"case.alpha","required":true}],
  "fixtures": [{"id":"fixture.bad","kind":"json","dataJson":"{\"broken\":"}]
}`)

	out := runCLI(t, "profile", "audit-plan", "--profile", profileDir, "--offline-template-package", "--json")
	var report struct {
		OK          bool   `json:"ok"`
		ProfileID   string `json:"profileId"`
		IssueCount  int    `json:"issueCount"`
		ActionCount int    `json:"actionCount"`
		Counts      struct {
			UpdateReferenceOrAddAsset int `json:"updateReferenceOrAddAsset"`
			FillRequiredField         int `json:"fillRequiredField"`
			FixInvalidJSON            int `json:"fixInvalidJson"`
		} `json:"counts"`
		Actions []struct {
			Type            string   `json:"type"`
			IssueCode       string   `json:"issueCode"`
			SubjectID       string   `json:"subjectId"`
			Field           string   `json:"field"`
			SuggestedChange string   `json:"suggestedChange"`
			Command         []string `json:"command"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit plan json: %v\n%s", err, out)
	}
	if !report.OK || report.ProfileID != "sample" || report.IssueCount != 4 || report.ActionCount != 4 {
		t.Fatalf("audit plan summary = %#v", report)
	}
	if report.Counts.UpdateReferenceOrAddAsset != 1 || report.Counts.FillRequiredField != 2 || report.Counts.FixInvalidJSON != 1 {
		t.Fatalf("audit plan counts = %#v", report.Counts)
	}
	if len(report.Actions) != 4 || report.Actions[0].Type != "update-reference-or-add-asset" || report.Actions[0].IssueCode != "api-case-node-missing" || report.Actions[0].SubjectID != "case.alpha" || report.Actions[0].Field != "nodeId" {
		t.Fatalf("audit plan actions = %#v", report.Actions)
	}
	if !strings.Contains(report.Actions[0].SuggestedChange, "Create the missing interface node") || strings.Join(report.Actions[0].Command, " ") != "profile audit --json" {
		t.Fatalf("audit plan first action = %#v", report.Actions[0])
	}

	textOut := runCLI(t, "profile", "audit-plan", "--profile", profileDir, "--offline-template-package")
	for _, want := range []string{"Profile Audit Repair Plan: sample", "Actions: 4", "update-reference-or-add-asset", "api-case-node-missing", "fix-invalid-json"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("audit plan text missing %q:\n%s", want, textOut)
		}
	}
}

func TestProfileImportPlanOpenAPICommand(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "catalog-openapi.json")
	writeFile(t, specPath, `{
  "openapi": "3.0.3",
  "info": {"title": "Catalog API"},
  "paths": {
    "/items": {
      "get": {
        "operationId": "listItems",
        "summary": "List items",
        "tags": ["catalog"],
        "responses": {"200": {"description": "OK"}}
      },
      "post": {
        "operationId": "createItem",
        "summary": "Create item",
        "tags": ["catalog", "write"],
        "requestBody": {
          "content": {
            "application/json": {
              "example": {"id": "item-001", "name": "Example Item"}
            }
          }
        },
        "responses": {"201": {"description": "Created"}}
      }
    }
  }
}`)

	out := runCLI(t, "profile", "import-plan", "openapi", "--from", specPath, "--service-id", "service.catalog", "--evidence-dir", ".runtime/openapi", "--json")
	var report struct {
		Kind       string `json:"kind"`
		SourcePath string `json:"sourcePath"`
		Plan       struct {
			Service struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
				Status      string `json:"status"`
			} `json:"service"`
			InterfaceNodes []struct {
				ID     string `json:"id"`
				Method string `json:"method"`
				Path   string `json:"path"`
				Status string `json:"status"`
			} `json:"interfaceNodes"`
			APICases []struct {
				ID          string   `json:"id"`
				CasePath    string   `json:"casePath"`
				Status      string   `json:"status"`
				EvidenceDir string   `json:"evidenceDir"`
				Tags        []string `json:"tags"`
			} `json:"apiCases"`
			CaseFiles []struct {
				Path string          `json:"path"`
				Body json.RawMessage `json:"body"`
			} `json:"caseFiles"`
			WrittenFiles []string `json:"writtenFiles"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile import plan json: %v\n%s", err, out)
	}
	if report.Kind != "openapi" || report.SourcePath != specPath || report.Plan.Service.ID != "service.catalog" || report.Plan.Service.Status != "draft" {
		t.Fatalf("import plan summary = %#v", report)
	}
	if len(report.Plan.InterfaceNodes) != 2 || len(report.Plan.APICases) != 2 || len(report.Plan.CaseFiles) != 2 {
		t.Fatalf("import plan counts = nodes:%d cases:%d files:%d", len(report.Plan.InterfaceNodes), len(report.Plan.APICases), len(report.Plan.CaseFiles))
	}
	if report.Plan.InterfaceNodes[0].ID != "node.service.catalog.list-items" || report.Plan.InterfaceNodes[0].Method != "GET" || report.Plan.InterfaceNodes[0].Path != "/items" || report.Plan.InterfaceNodes[0].Status != "draft" {
		t.Fatalf("first interface node = %#v", report.Plan.InterfaceNodes[0])
	}
	if report.Plan.APICases[1].ID != "case.service.catalog.create-item" || report.Plan.APICases[1].CasePath != "api-cases/case.service.catalog.create-item.json" || report.Plan.APICases[1].EvidenceDir != ".runtime/openapi" || strings.Join(report.Plan.APICases[1].Tags, ",") != "openapi,catalog,write" {
		t.Fatalf("second api case = %#v", report.Plan.APICases[1])
	}
	var runnable struct {
		Request struct {
			Method string         `json:"method"`
			Path   string         `json:"path"`
			Body   map[string]any `json:"body"`
		} `json:"request"`
		Assertions struct {
			ExpectedStatusCodes []int `json:"expectedStatusCodes"`
		} `json:"assertions"`
	}
	if err := json.Unmarshal(report.Plan.CaseFiles[1].Body, &runnable); err != nil {
		t.Fatalf("decode generated case body: %v\n%s", err, string(report.Plan.CaseFiles[1].Body))
	}
	if runnable.Request.Method != "POST" || runnable.Request.Path != "/items" || runnable.Request.Body["id"] != "item-001" || len(runnable.Assertions.ExpectedStatusCodes) != 1 || runnable.Assertions.ExpectedStatusCodes[0] != 201 {
		t.Fatalf("generated runnable case = %#v", runnable)
	}

	textOut := runCLI(t, "profile", "import-plan", "openapi", "--from", specPath, "--service-id", "service.catalog")
	for _, want := range []string{"OpenAPI Import Plan", "Source: " + specPath, "Service: service.catalog", "Interface Nodes: 2", "API Cases: 2", "Case Files: 2"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("import plan text missing %q:\n%s", want, textOut)
		}
	}

	outputDir := filepath.Join(t.TempDir(), "review-plan")
	textOut = runCLI(t, "profile", "import-plan", "openapi", "--from", specPath, "--service-id", "service.catalog", "--evidence-dir", ".runtime/openapi", "--output-dir", outputDir)
	if !strings.Contains(textOut, "Output Dir: "+outputDir) {
		t.Fatalf("import plan output-dir text = %q", textOut)
	}
	for _, path := range []string{
		"import-plan.json",
		filepath.Join("services", "service.catalog.json"),
		filepath.Join("interface-nodes", "node.service.catalog.list-items.json"),
		filepath.Join("request-templates", "template.service.catalog.create-item.json"),
		filepath.Join("cases", "case.service.catalog.create-item.json"),
		filepath.Join("api-cases", "case.service.catalog.create-item.json"),
	} {
		if _, err := os.Stat(filepath.Join(outputDir, path)); err != nil {
			t.Fatalf("expected import plan output %s: %v", path, err)
		}
	}
	var metadataCase struct {
		ID       string `json:"id"`
		CasePath string `json:"casePath"`
		Status   string `json:"status"`
	}
	readTestJSONFile(t, filepath.Join(outputDir, "cases", "case.service.catalog.create-item.json"), &metadataCase)
	if metadataCase.ID != "case.service.catalog.create-item" || metadataCase.CasePath != "api-cases/case.service.catalog.create-item.json" || metadataCase.Status != "draft" {
		t.Fatalf("written metadata case = %#v", metadataCase)
	}
	readTestJSONFile(t, filepath.Join(outputDir, "api-cases", "case.service.catalog.create-item.json"), &runnable)
	if runnable.Request.Method != "POST" || runnable.Request.Path != "/items" || runnable.Request.Body["id"] != "item-001" {
		t.Fatalf("written runnable case = %#v", runnable)
	}
}

func TestProfileImportPlanHTTPCaptureCommand(t *testing.T) {
	capturePath := filepath.Join(t.TempDir(), "traffic.json")
	writeFile(t, capturePath, `{
  "name": "Catalog Traffic",
  "captures": [
    {
      "id": "createItem",
      "name": "Create item from traffic",
      "request": {
        "method": "POST",
        "path": "/items",
        "headers": {"Content-Type": "application/json"},
        "body": {"id": "item-001", "name": "Example"}
      },
      "response": {"status": 201, "body": {"id": "item-001"}}
    }
  ]
}`)

	out := runCLI(t, "profile", "import-plan", "http-capture", "--from", capturePath, "--service-id", "service.catalog", "--evidence-dir", ".runtime/replay", "--json")
	var report struct {
		Kind       string `json:"kind"`
		SourcePath string `json:"sourcePath"`
		Plan       struct {
			Service struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"service"`
			InterfaceNodes []struct {
				ID     string `json:"id"`
				Method string `json:"method"`
				Path   string `json:"path"`
			} `json:"interfaceNodes"`
			APICases []struct {
				ID          string   `json:"id"`
				CasePath    string   `json:"casePath"`
				EvidenceDir string   `json:"evidenceDir"`
				Tags        []string `json:"tags"`
			} `json:"apiCases"`
			CaseFiles []struct {
				Path string          `json:"path"`
				Body json.RawMessage `json:"body"`
			} `json:"caseFiles"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode http capture import plan json: %v\n%s", err, out)
	}
	if report.Kind != "http-capture" || report.SourcePath != capturePath || report.Plan.Service.ID != "service.catalog" || report.Plan.Service.Status != "draft" {
		t.Fatalf("http capture plan summary = %#v", report)
	}
	if len(report.Plan.InterfaceNodes) != 1 || len(report.Plan.APICases) != 1 || len(report.Plan.CaseFiles) != 1 {
		t.Fatalf("http capture plan counts = nodes:%d cases:%d files:%d", len(report.Plan.InterfaceNodes), len(report.Plan.APICases), len(report.Plan.CaseFiles))
	}
	if report.Plan.InterfaceNodes[0].ID != "node.service.catalog.create-item" || report.Plan.InterfaceNodes[0].Method != "POST" || report.Plan.InterfaceNodes[0].Path != "/items" {
		t.Fatalf("http capture node = %#v", report.Plan.InterfaceNodes[0])
	}
	if report.Plan.APICases[0].ID != "case.service.catalog.create-item" || report.Plan.APICases[0].CasePath != "api-cases/case.service.catalog.create-item.json" || report.Plan.APICases[0].EvidenceDir != ".runtime/replay" || strings.Join(report.Plan.APICases[0].Tags, ",") != "recorded,replay" {
		t.Fatalf("http capture case = %#v", report.Plan.APICases[0])
	}

	outputDir := filepath.Join(t.TempDir(), "capture-plan")
	textOut := runCLI(t, "profile", "import-plan", "http-capture", "--from", capturePath, "--service-id", "service.catalog", "--output-dir", outputDir)
	for _, want := range []string{"HTTP Capture Import Plan", "Source: " + capturePath, "Output Dir: " + outputDir, "API Cases: 1"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("http capture text missing %q:\n%s", want, textOut)
		}
	}
	for _, path := range []string{
		"import-plan.json",
		filepath.Join("services", "service.catalog.json"),
		filepath.Join("interface-nodes", "node.service.catalog.create-item.json"),
		filepath.Join("cases", "case.service.catalog.create-item.json"),
		filepath.Join("api-cases", "case.service.catalog.create-item.json"),
	} {
		if _, err := os.Stat(filepath.Join(outputDir, path)); err != nil {
			t.Fatalf("expected http capture output %s: %v", path, err)
		}
	}
}

func TestProfileGenerationPlanOpenAPICommand(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "catalog-openapi.json")
	writeFile(t, specPath, `{
  "openapi": "3.0.3",
  "info": {"title": "Catalog API"},
  "paths": {
    "/items": {
      "post": {
        "operationId": "createItem",
        "summary": "Create item",
        "tags": ["catalog"],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["id"],
                "properties": {
                  "id": {"type": "string", "example": "item-001"},
                  "name": {"type": "string", "example": "Example Item"}
                }
              }
            }
          }
        },
        "responses": {
          "201": {"description": "Created"},
          "400": {"description": "Bad request"}
        }
      }
    }
  }
}`)

	out := runCLI(t, "profile", "generation-plan", "openapi", "--from", specPath, "--service-id", "service.catalog", "--evidence-dir", ".runtime/generated", "--json")
	var report struct {
		Kind       string `json:"kind"`
		SourcePath string `json:"sourcePath"`
		Plan       struct {
			OK         bool `json:"ok"`
			Candidates []struct {
				ID     string `json:"id"`
				Kind   string `json:"kind"`
				Field  string `json:"field"`
				CaseID string `json:"caseId"`
			} `json:"candidates"`
			APICases []struct {
				ID       string   `json:"id"`
				Status   string   `json:"status"`
				CasePath string   `json:"casePath"`
				Tags     []string `json:"tags"`
			} `json:"apiCases"`
			CaseFiles []struct {
				Path string          `json:"path"`
				Body json.RawMessage `json:"body"`
			} `json:"caseFiles"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode generation plan json: %v\n%s", err, out)
	}
	if report.Kind != "openapi" || report.SourcePath != specPath || !report.Plan.OK || len(report.Plan.Candidates) != 1 || len(report.Plan.APICases) != 1 {
		t.Fatalf("generation plan summary = %#v", report)
	}
	if report.Plan.Candidates[0].Kind != "missing-required-field" || report.Plan.Candidates[0].Field != "id" || report.Plan.Candidates[0].CaseID != "case.service.catalog.create-item.missing-id" {
		t.Fatalf("generation candidate = %#v", report.Plan.Candidates[0])
	}
	if report.Plan.APICases[0].Status != "draft" || strings.Join(report.Plan.APICases[0].Tags, ",") != "generated,schema,negative,catalog" {
		t.Fatalf("generated api case = %#v", report.Plan.APICases[0])
	}

	outputDir := filepath.Join(t.TempDir(), "generation-plan")
	textOut := runCLI(t, "profile", "generation-plan", "openapi", "--from", specPath, "--service-id", "service.catalog", "--output-dir", outputDir)
	for _, want := range []string{"OpenAPI Generation Plan", "Source: " + specPath, "Candidates: 1", "Output Dir: " + outputDir} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("generation plan text missing %q:\n%s", want, textOut)
		}
	}
	for _, path := range []string{
		"generation-plan.json",
		filepath.Join("cases", "case.service.catalog.create-item.missing-id.json"),
		filepath.Join("api-cases", "case.service.catalog.create-item.missing-id.json"),
	} {
		if _, err := os.Stat(filepath.Join(outputDir, path)); err != nil {
			t.Fatalf("expected generation plan output %s: %v", path, err)
		}
	}
}

func TestExecutorPlanCommandReportsProfileDescriptors(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-executor-plan-pg")
	s, err := openStore(context.Background(), storeRef)
	if err != nil {
		t.Fatalf("open executor store: %v", err)
	}
	if err := s.ReplaceProfileCatalog(context.Background(), store.ProfileCatalog{
		ProfileID: "current",
		APICases: []store.CatalogAPICase{
			{ID: "case.catalog", DisplayName: "Catalog Case", SourceKind: "pytest", SourcePath: "tests/catalog_test.py", ExecutorID: "executor.catalog", Status: "active", TimeoutSeconds: 11},
			{ID: "case.blocked", DisplayName: "Blocked Case", SourceKind: "pytest", ExecutorID: "executor.blocked", Status: "active"},
		},
	}); err != nil {
		t.Fatalf("seed executor store: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close executor store: %v", err)
	}

	out := runCLI(t, "executor", "plan", "--json")
	var report struct {
		OK        bool   `json:"ok"`
		ProfileID string `json:"profileId"`
		Counts    struct {
			Total   int `json:"total"`
			Ready   int `json:"ready"`
			Blocked int `json:"blocked"`
		} `json:"counts"`
		Items []struct {
			ID             string   `json:"id"`
			Kind           string   `json:"kind"`
			SourcePath     string   `json:"sourcePath"`
			Ready          bool     `json:"ready"`
			RunMode        string   `json:"runMode"`
			TimeoutSeconds int      `json:"timeoutSeconds"`
			Issues         []string `json:"issues"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode executor plan json: %v\n%s", err, out)
	}
	if report.OK || report.ProfileID != "current" || report.Counts.Total != 2 || report.Counts.Ready != 1 || report.Counts.Blocked != 1 {
		t.Fatalf("executor plan summary = %#v", report)
	}
	itemsByID := map[string]struct {
		ID             string   `json:"id"`
		Kind           string   `json:"kind"`
		SourcePath     string   `json:"sourcePath"`
		Ready          bool     `json:"ready"`
		RunMode        string   `json:"runMode"`
		TimeoutSeconds int      `json:"timeoutSeconds"`
		Issues         []string `json:"issues"`
	}{}
	for _, item := range report.Items {
		itemsByID[item.ID] = item
	}
	blocked := itemsByID["executor.blocked"]
	if blocked.ID == "" || blocked.Ready || !containsString(blocked.Issues, "missing-source-path") {
		t.Fatalf("blocked executor item = %#v", blocked)
	}
	ready := itemsByID["executor.catalog"]
	if ready.ID == "" || ready.Kind != "pytest" || ready.SourcePath != "tests/catalog_test.py" || !ready.Ready || ready.RunMode != "dry-run" || ready.TimeoutSeconds != 11 {
		t.Fatalf("ready executor item = %#v", ready)
	}

	textOut := runCLI(t, "executor", "plan")
	for _, want := range []string{"Executor Plan", "Profile: current", "Ready: 1", "Blocked: 1", "missing-source-path"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("executor plan text missing %q:\n%s", want, textOut)
		}
	}
}

func TestBaselineGateCommandsSetAndGetState(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-baseline-pg")
	subjectID := uniqueTestID(t, "workflow.alpha")

	out := runCLI(t, "baseline", "set", "--profile", "sample", "--subject", subjectID, "--status", "passed", "--required")
	if !strings.Contains(out, "Baseline Gate: sample "+subjectID) || !strings.Contains(out, "Status: passed") {
		t.Fatalf("baseline set output = %q", out)
	}

	out = runCLI(t, "baseline", "get", "--profile", "sample", "--subject", subjectID)
	for _, want := range []string{"Baseline Gate: sample " + subjectID, "Status: passed", "Required: true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("baseline get output missing %q: %q", want, out)
		}
	}
}

func TestBaselineGetCommandRejectsMissingGate(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-baseline-missing-pg")
	subjectID := uniqueTestID(t, "workflow.missing")

	out := runCLIFails(t, "baseline", "get", "--profile", "sample", "--subject", subjectID)
	if !strings.Contains(out, "baseline gate not found") || !strings.Contains(out, "sample "+subjectID) {
		t.Fatalf("missing baseline gate output = %q", out)
	}
}

func TestWorkflowPlanCommandPrintsBoundSteps(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)
	configureNamedPostgreSQLActiveStore(t, "daily-workflow-plan-pg")
	runCLI(t, "config", "publish", "--from", dir)

	out := runCLI(t, "workflow", "plan", "--workflow", "workflow.alpha")

	for _, want := range []string{
		"Workflow: workflow.alpha",
		"Step: step.one",
		"Node: node.alpha",
		"Case: case.alpha",
		"Required: true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workflow plan output missing %q: %q", want, out)
		}
	}
}

func TestWorkflowPlanCommandCanEmitJSONFromStore(t *testing.T) {
	profileDir := t.TempDir()
	writeWorkflowProfile(t, profileDir)
	configureNamedPostgreSQLActiveStore(t, "daily-workflow-plan-json-pg")
	runCLI(t, "config", "publish", "--from", profileDir)

	out := runCLI(t, "workflow", "plan", "--workflow", "workflow.alpha", "--json")

	var payload struct {
		OK         bool   `json:"ok"`
		ProfileID  string `json:"profileId"`
		WorkflowID string `json:"workflowId"`
		Counts     struct {
			Steps         int `json:"steps"`
			RequiredSteps int `json:"requiredSteps"`
		} `json:"counts"`
		Steps []struct {
			StepID string `json:"stepId"`
			NodeID string `json:"nodeId"`
			CaseID string `json:"caseId"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode workflow plan json: %v\n%s", err, out)
	}
	if !payload.OK || payload.ProfileID != "sample" || payload.WorkflowID != "workflow.alpha" || payload.Counts.Steps != 1 || payload.Counts.RequiredSteps != 1 {
		t.Fatalf("workflow plan json summary = %#v", payload)
	}
	if len(payload.Steps) != 1 || payload.Steps[0].StepID != "step.one" || payload.Steps[0].NodeID != "node.alpha" || payload.Steps[0].CaseID != "case.alpha" {
		t.Fatalf("workflow plan json steps = %#v", payload.Steps)
	}
}

func TestWorkflowPlanCommandRejectsMissingWorkflow(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)
	configureNamedPostgreSQLActiveStore(t, "daily-workflow-plan-missing-pg")
	runCLI(t, "config", "publish", "--from", dir)

	out := runCLIFails(t, "workflow", "plan", "--workflow", "workflow.missing")
	if !strings.Contains(out, "workflow not found") || !strings.Contains(out, "workflow.missing") {
		t.Fatalf("missing workflow output = %q", out)
	}
}

func TestWorkflowRunCommandsReadStoredRuns(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-workflow-runs-pg")
	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	runID := uniqueTestID(t, "run.workflow")
	workflowID := uniqueTestID(t, "workflow.alpha")
	started := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         runID,
		ProfileID:  "sample",
		WorkflowID: workflowID,
		Status:     store.StatusPassed,
		SummaryJSON: `{
			"summary":{"stepCount":1,"passed":1},
			"steps":[{"stepId":"step.one","caseId":"case.alpha","status":"passed"}]
		}`,
		StartedAt:  started,
		FinishedAt: started.Add(time.Second),
		CreatedAt:  started,
		UpdatedAt:  started.Add(time.Second),
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}

	listOut := runCLI(t, "workflow", "runs", "--json")
	var list struct {
		OK           bool `json:"ok"`
		WorkflowRuns []struct {
			ID         string `json:"id"`
			WorkflowID string `json:"workflowId"`
			Status     string `json:"status"`
			StepCount  int    `json:"stepCount"`
		} `json:"workflowRuns"`
	}
	if err := json.Unmarshal([]byte(listOut), &list); err != nil {
		t.Fatalf("decode workflow runs json: %v\n%s", err, listOut)
	}
	foundRun := false
	for _, item := range list.WorkflowRuns {
		if item.ID == runID && item.StepCount == 1 {
			foundRun = true
			break
		}
	}
	if !list.OK || !foundRun {
		t.Fatalf("workflow runs = %#v", list)
	}

	detailOut := runCLI(t, "workflow", "run", "--run", runID, "--json")
	var detail struct {
		OK      bool           `json:"ok"`
		Run     map[string]any `json:"run"`
		Summary struct {
			Steps []map[string]any `json:"steps"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(detailOut), &detail); err != nil {
		t.Fatalf("decode workflow run json: %v\n%s", err, detailOut)
	}
	if !detail.OK || detail.Run["id"] != runID || len(detail.Summary.Steps) != 1 || detail.Summary.Steps[0]["stepId"] != "step.one" {
		t.Fatalf("workflow run detail = %#v", detail)
	}

	stepOut := runCLI(t, "workflow", "step", "--run", runID, "--step", "step.one", "--json")
	var stepDetail struct {
		OK      bool           `json:"ok"`
		Run     map[string]any `json:"run"`
		Summary struct {
			Steps []map[string]any `json:"steps"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stepOut), &stepDetail); err != nil {
		t.Fatalf("decode workflow step json: %v\n%s", err, stepOut)
	}
	if !stepDetail.OK || stepDetail.Run["id"] != runID || len(stepDetail.Summary.Steps) != 1 || stepDetail.Summary.Steps[0]["stepId"] != "step.one" {
		t.Fatalf("workflow step detail = %#v", stepDetail)
	}

	latestOut := runCLI(t, "workflow", "latest-step", "--workflow", workflowID, "--step", "step.one", "--json")
	var latestDetail struct {
		OK      bool           `json:"ok"`
		Run     map[string]any `json:"run"`
		Summary struct {
			Steps []map[string]any `json:"steps"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(latestOut), &latestDetail); err != nil {
		t.Fatalf("decode latest workflow step json: %v\n%s", err, latestOut)
	}
	if !latestDetail.OK || latestDetail.Run["id"] != runID || len(latestDetail.Summary.Steps) != 1 || latestDetail.Summary.Steps[0]["caseId"] != "case.alpha" {
		t.Fatalf("latest workflow step detail = %#v", latestDetail)
	}

	if out := runCLI(t, "workflow", "runs", "--store", storeRef, "--json"); !strings.Contains(out, runID) {
		t.Fatalf("workflow runs --store output = %q", out)
	}
	if out := runCLI(t, "workflow", "run", "--store", storeRef, "--run", runID, "--json"); !strings.Contains(out, "step.one") {
		t.Fatalf("workflow run --store output = %q", out)
	}
	if out := runCLI(t, "workflow", "step", "--store", storeRef, "--run", runID, "--step", "step.one", "--json"); !strings.Contains(out, "case.alpha") {
		t.Fatalf("workflow step --store output = %q", out)
	}
	if out := runCLI(t, "workflow", "latest-step", "--store", storeRef, "--workflow", workflowID, "--step", "step.one", "--json"); !strings.Contains(out, runID) {
		t.Fatalf("workflow latest-step --store output = %q", out)
	}
}

func TestWorkflowRunReadCommandsRejectActiveSQLiteStore(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "runs",
			args: []string{"workflow", "runs", "--json"},
		},
		{
			name: "run",
			args: []string{"workflow", "run", "--run", "run.legacy", "--json"},
		},
		{
			name: "step",
			args: []string{"workflow", "step", "--run", "run.legacy", "--step", "step.legacy", "--json"},
		},
		{
			name: "latest step",
			args: []string{"workflow", "latest-step", "--workflow", "workflow.legacy", "--step", "step.legacy", "--json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("OTSANDBOX_CONFIG_HOME", filepath.Join(dir, "config"))
			if err := saveStoreConfig(storeConfigFile{
				Active: "legacy-local",
				Stores: map[string]storeConfigEntry{
					"legacy-local": {Name: "legacy-local", URL: "sqlite://" + filepath.Join(dir, "store.sqlite"), Backend: "sqlite"},
				},
			}); err != nil {
				t.Fatalf("save store config: %v", err)
			}

			out := runCLIFails(t, tt.args...)
			for _, want := range []string{"daily commands require PostgreSQL Store", "SQLite", "postgres://"} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s output missing %q: %q", tt.name, want, out)
				}
			}
		})
	}
}

func TestTraceTopologyCollectCommandPersistsTopology(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	startedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.trace",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  startedAt,
		FinishedAt: startedAt.Add(3 * time.Second),
		CreatedAt:  startedAt,
		UpdatedAt:  startedAt.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["POST:/alpha"],"duration":120,"start":"2026-05-18 1000","isError":false,"traceIds":["trace.alpha"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.alpha","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/alpha","type":"Entry","component":"Tomcat"},{"traceId":"trace.alpha","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.alpha","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"POST:/alpha","type":"Entry","component":"Server"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	out := runCLI(t, "trace", "topology", "collect",
		"--store", "sqlite://"+storePath,
		"--trace-graphql-url", provider.URL,
		"--run", "run.trace",
		"--step", "step.alpha",
		"--case", "case.alpha",
		"--request", "request.alpha",
		"--endpoint", "/alpha",
		"--started-at", startedAt.Format(time.RFC3339Nano),
		"--json",
	)

	var payload struct {
		OK            bool `json:"ok"`
		TraceTopology struct {
			WorkflowRunID string `json:"workflowRunId"`
			TraceID       string `json:"traceId"`
			Status        string `json:"status"`
		} `json:"traceTopology"`
		Topology struct {
			SpanCount      int `json:"spanCount"`
			ConfirmedEdges []struct {
				Source string `json:"source"`
				Target string `json:"target"`
			} `json:"confirmedEdges"`
		} `json:"topology"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode trace topology collect json: %v\n%s", err, out)
	}
	if !payload.OK || payload.TraceTopology.WorkflowRunID != "run.trace" || payload.TraceTopology.TraceID != "trace.alpha" || payload.TraceTopology.Status != "complete" {
		t.Fatalf("trace topology collect payload = %#v", payload)
	}
	if payload.Topology.SpanCount != 2 || len(payload.Topology.ConfirmedEdges) != 1 || payload.Topology.ConfirmedEdges[0].Source != "service.entry" || payload.Topology.ConfirmedEdges[0].Target != "service.worker" {
		t.Fatalf("trace topology = %#v", payload.Topology)
	}
	s, err = sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s.Close()
	rows, err := s.ListTraceTopologies(ctx, "run.trace")
	if err != nil {
		t.Fatalf("list trace topologies: %v", err)
	}
	if len(rows) != 1 || rows[0].StepID != "step.alpha" || rows[0].CaseID != "case.alpha" || rows[0].RequestID != "request.alpha" {
		t.Fatalf("stored topologies = %#v", rows)
	}
}

func TestReplayEvidenceCommandEmitsShellPayload(t *testing.T) {
	out := runCLI(t, "replay", "evidence", "--trace-id", "TRACE-1", "--json")

	var payload struct {
		OK  bool `json:"ok"`
		Run struct {
			TraceID string `json:"traceId"`
		} `json:"run"`
		Evidence struct {
			TraceID string `json:"traceId"`
			Systems []any  `json:"systems"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode replay evidence json: %v\n%s", err, out)
	}
	if !payload.OK || payload.Run.TraceID != "TRACE-1" || payload.Evidence.TraceID != "TRACE-1" || len(payload.Evidence.Systems) != 0 {
		t.Fatalf("replay evidence payload = %#v", payload)
	}
}

func TestWorkflowAuditCommandEmitsJSONWithScopedStoreState(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-workflow-audit-json-pg")
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.missing"}
  ],
  "requestTemplates": [{"id":"template.alpha","nodeId":"node.alpha","method":"POST","path":"/v1/items"}],
  "caseDependencies": [{"id":"dependency.beta","caseId":"case.beta","fixtureId":"fixture.missing"}],
	"workflowBindings": [
    {"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.alpha","required":true},
    {"workflowId":"workflow.alpha","stepId":"step.two","nodeId":"node.alpha","caseId":"case.beta","required":true}
  ],
  "fixtures": []
}`)
	runCLI(t, "config", "publish", "--from", profileDir)
	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	firstRunID := uniqueTestID(t, "run.workflow.001")
	secondRunID := uniqueTestID(t, "run.workflow.002")
	started := time.Now().UTC().Add(-10 * time.Second)
	finished := started.Add(2 * time.Second)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         firstRunID,
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusFailed,
		StartedAt:  started,
		FinishedAt: finished,
		CreatedAt:  started,
		UpdatedAt:  finished,
	}); err != nil {
		t.Fatalf("create first workflow run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         firstRunID + ".case.alpha",
		RunID:      firstRunID,
		CaseID:     "case.alpha",
		Status:     store.StatusFailed,
		StartedAt:  started,
		FinishedAt: finished,
		CreatedAt:  started,
	}); err != nil {
		t.Fatalf("record first case run: %v", err)
	}
	laterStarted := started.Add(10 * time.Second)
	laterFinished := laterStarted.Add(3 * time.Second)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         secondRunID,
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  laterStarted,
		FinishedAt: laterFinished,
		CreatedAt:  laterStarted,
		UpdatedAt:  laterFinished,
	}); err != nil {
		t.Fatalf("create second workflow run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         secondRunID + ".case.alpha",
		RunID:      secondRunID,
		CaseID:     "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  laterStarted,
		FinishedAt: laterFinished,
		CreatedAt:  laterStarted,
	}); err != nil {
		t.Fatalf("record second case run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t, "workflow", "audit", "--workflow", "workflow.alpha", "--json")

	var report struct {
		OK         bool   `json:"ok"`
		WorkflowID string `json:"workflowId"`
		IssueCount int    `json:"issueCount"`
		Issues     []struct {
			Code      string `json:"code"`
			SubjectID string `json:"subjectId"`
		} `json:"issues"`
		Store *struct {
			LatestRun *struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"latestRun"`
			BindingCases []struct {
				StepID       string `json:"stepId"`
				CaseID       string `json:"caseId"`
				HasPassed    bool   `json:"hasPassed"`
				LatestStatus string `json:"latestStatus"`
				LatestRunID  string `json:"latestRunId"`
			} `json:"bindingCases"`
		} `json:"store"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode workflow audit json: %v\n%s", err, out)
	}
	if report.OK || report.WorkflowID != "workflow.alpha" || report.IssueCount != 2 {
		t.Fatalf("workflow audit summary = %#v", report)
	}
	if len(report.Issues) != 2 || report.Issues[0].Code != "api-case-node-missing" || report.Issues[1].Code != "case-dependency-fixture-missing" {
		t.Fatalf("workflow audit issues = %#v", report.Issues)
	}
	if report.Store == nil || report.Store.LatestRun == nil || report.Store.LatestRun.ID != secondRunID || report.Store.LatestRun.Status != store.StatusPassed {
		t.Fatalf("latest workflow run = %#v", report.Store)
	}
	caseState := map[string]struct {
		HasPassed    bool
		LatestStatus string
		LatestRunID  string
	}{}
	for _, item := range report.Store.BindingCases {
		caseState[item.CaseID] = struct {
			HasPassed    bool
			LatestStatus string
			LatestRunID  string
		}{HasPassed: item.HasPassed, LatestStatus: item.LatestStatus, LatestRunID: item.LatestRunID}
	}
	if !caseState["case.alpha"].HasPassed || caseState["case.alpha"].LatestStatus != store.StatusPassed || caseState["case.alpha"].LatestRunID != secondRunID {
		t.Fatalf("case.alpha workflow state = %#v", caseState["case.alpha"])
	}
	if caseState["case.beta"].HasPassed || caseState["case.beta"].LatestStatus != "" || caseState["case.beta"].LatestRunID != "" {
		t.Fatalf("case.beta workflow state = %#v", caseState["case.beta"])
	}
}

func TestWorkflowAuditCommandPrintsTextSummary(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-workflow-audit-text-pg")
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)
	runCLI(t, "config", "publish", "--from", dir)

	out := runCLI(t, "workflow", "audit", "--workflow", "workflow.alpha")

	for _, want := range []string{
		"Workflow Audit: workflow.alpha",
		"OK: true",
		"Issues: 0",
		"Bindings: 1",
		"Binding: step.one Node: node.alpha Case: case.alpha Required: true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workflow audit output missing %q: %q", want, out)
		}
	}
}

func TestWorkflowAuditAllowsExplicitOfflineTemplatePackage(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)

	out := runCLI(t, "workflow", "audit", "--profile", dir, "--offline-template-package", "--workflow", "workflow.alpha", "--json")
	var report struct {
		OK         bool   `json:"ok"`
		WorkflowID string `json:"workflowId"`
		IssueCount int    `json:"issueCount"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode offline workflow audit json: %v\n%s", err, out)
	}
	if !report.OK || report.WorkflowID != "workflow.alpha" || report.IssueCount != 0 {
		t.Fatalf("offline workflow audit report = %#v", report)
	}
}

func TestTemplateRenderCommandPrintsRequestPreview(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-template-render-pg")
	dir := t.TempDir()
	writeTemplateProfile(t, dir)
	runCLI(t, "config", "publish", "--from", dir)

	out := runCLI(t, "template", "render", "--template", "template.create", "--fixture", "fixture.item")

	var rendered struct {
		Method string         `json:"method"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &rendered); err != nil {
		t.Fatalf("decode template render output: %v\n%s", err, out)
	}
	if rendered.Method != "POST" || rendered.Path != "/v1/items/item-001" {
		t.Fatalf("rendered request identity = %#v", rendered)
	}
	if rendered.Body["id"] != "item-001" || rendered.Body["quantity"].(float64) != 3 {
		t.Fatalf("rendered request body = %#v", rendered.Body)
	}

	ctx := context.Background()
	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open template store: %v", err)
	}
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.store", Method: "PATCH", Path: "/v1/items/{{.itemId}}", Status: "active"},
		},
		RequestTemplates: []store.CatalogRequestTemplate{
			{
				ID:           "template.store",
				NodeID:       "node.store",
				Method:       "PATCH",
				Path:         "/v1/items/{{.itemId}}",
				TemplateJSON: `{"id":"{{.itemId}}","enabled":{{.enabled}}}`,
			},
		},
		Fixtures: []store.CatalogFixture{
			{
				ID:       "fixture.store",
				Kind:     "json",
				DataJSON: `{"itemId":"item-002","enabled":true}`,
			},
		},
	}); err != nil {
		t.Fatalf("seed template store: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close template store: %v", err)
	}

	storeOut := runCLI(t, "template", "render", "--template", "template.store", "--fixture", "fixture.store")
	var storeRendered struct {
		Method string         `json:"method"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	}
	if err := json.Unmarshal([]byte(storeOut), &storeRendered); err != nil {
		t.Fatalf("decode store template render output: %v\n%s", err, storeOut)
	}
	if storeRendered.Method != "PATCH" || storeRendered.Path != "/v1/items/item-002" || storeRendered.Body["enabled"] != true {
		t.Fatalf("store rendered request = %#v", storeRendered)
	}
}

func TestEvidenceImportCommandIndexesLegacyRuntime(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--store", "sqlite://"+storePath)
	if !strings.Contains(out, "Imported evidence index") || !strings.Contains(out, "Runs: 2") || !strings.Contains(out, "API Case Runs: 1") {
		t.Fatalf("evidence import output = %q", out)
	}
}

func TestEvidenceImportCommandCanEmitJSONReport(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--store", "sqlite://"+storePath, "--json")

	var report struct {
		SourcePath      string `json:"sourcePath"`
		StorePath       string `json:"storePath"`
		ProfileID       string `json:"profileId"`
		RunCount        int    `json:"runCount"`
		APICaseRunCount int    `json:"apiCaseRunCount"`
		EvidenceCount   int    `json:"evidenceCount"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode evidence import json report: %v\n%s", err, out)
	}
	if report.SourcePath != sourcePath || report.StorePath != "sqlite://"+storePath || report.ProfileID != "sample" {
		t.Fatalf("report paths/profile = %#v", report)
	}
	if report.RunCount != 2 || report.APICaseRunCount != 1 || report.EvidenceCount != 1 {
		t.Fatalf("report counts = %#v", report)
	}
}

func TestEvidenceImportUsesNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-evidence-import-pg")
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	suffix := time.Now().UTC().UnixNano()
	workflowLegacyID := suffix
	caseLegacyID := suffix + 1
	parentRunID := fmt.Sprintf("case-run-parent-pg-%d", suffix)
	createLegacyRuntimeDBWithIDs(t, sourcePath, workflowLegacyID, caseLegacyID, parentRunID)

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--json")
	var report struct {
		SourcePath      string `json:"sourcePath"`
		ProfileID       string `json:"profileId"`
		RunCount        int    `json:"runCount"`
		APICaseRunCount int    `json:"apiCaseRunCount"`
		EvidenceCount   int    `json:"evidenceCount"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode PostgreSQL evidence import json: %v\n%s", err, out)
	}
	if report.SourcePath != sourcePath || report.ProfileID != "sample" || report.RunCount != 2 || report.APICaseRunCount != 1 || report.EvidenceCount != 1 {
		t.Fatalf("PostgreSQL evidence import report = %#v", report)
	}

	ctx := context.Background()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open PostgreSQL evidence import Store: %v", err)
	}
	defer runtime.Close()
	workflowRunID := fmt.Sprintf("legacy-workflow-%d", workflowLegacyID)
	workflowRun, err := runtime.GetRun(ctx, workflowRunID)
	if err != nil {
		t.Fatalf("get imported PostgreSQL workflow run: %v", err)
	}
	if workflowRun.ProfileID != "sample" || workflowRun.WorkflowID != "workflow.alpha" || workflowRun.Status != store.StatusPassed {
		t.Fatalf("imported PostgreSQL workflow run = %#v", workflowRun)
	}
	parentRun, err := runtime.GetRun(ctx, parentRunID)
	if err != nil {
		t.Fatalf("get imported PostgreSQL parent run: %v", err)
	}
	if parentRun.ProfileID != "sample" || parentRun.Status != store.StatusFailed {
		t.Fatalf("imported PostgreSQL parent run = %#v", parentRun)
	}
	caseRuns, err := runtime.ListAPICaseRuns(ctx, parentRunID)
	if err != nil {
		t.Fatalf("list imported PostgreSQL case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].ID != fmt.Sprintf("legacy-case-run-%d", caseLegacyID) || caseRuns[0].CaseID != "case.alpha" || caseRuns[0].Status != store.StatusFailed {
		t.Fatalf("imported PostgreSQL case runs = %#v", caseRuns)
	}
	records, err := runtime.ListEvidence(ctx, parentRunID)
	if err != nil {
		t.Fatalf("list imported PostgreSQL Evidence: %v", err)
	}
	if len(records) != 1 || records[0].ID != fmt.Sprintf("legacy-evidence-%d", caseLegacyID) || records[0].Kind != "case-run" {
		t.Fatalf("imported PostgreSQL Evidence = %#v", records)
	}

	listOut := runCLI(t, "evidence", "list", "--run", parentRunID, "--json")
	var evidenceReport struct {
		Runs []struct {
			ID              string `json:"id"`
			APICaseRunCount int    `json:"apiCaseRunCount"`
			EvidenceCount   int    `json:"evidenceCount"`
			EvidenceRecords []struct {
				ID        string `json:"id"`
				RunID     string `json:"runId"`
				CaseRunID string `json:"caseRunId"`
				Kind      string `json:"kind"`
				URI       string `json:"uri"`
			} `json:"evidenceRecords"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(listOut), &evidenceReport); err != nil {
		t.Fatalf("decode imported PostgreSQL evidence list json: %v\n%s", err, listOut)
	}
	if len(evidenceReport.Runs) != 1 || evidenceReport.Runs[0].ID != parentRunID || evidenceReport.Runs[0].APICaseRunCount != 1 || evidenceReport.Runs[0].EvidenceCount != 1 {
		t.Fatalf("imported PostgreSQL evidence list = %#v", evidenceReport.Runs)
	}
	if len(evidenceReport.Runs[0].EvidenceRecords) != 1 {
		t.Fatalf("imported PostgreSQL evidence list records = %#v", evidenceReport.Runs[0].EvidenceRecords)
	}
	record := evidenceReport.Runs[0].EvidenceRecords[0]
	if record.ID != fmt.Sprintf("legacy-evidence-%d", caseLegacyID) || record.RunID != parentRunID || record.CaseRunID != fmt.Sprintf("legacy-case-run-%d", caseLegacyID) || record.Kind != "case-run" || record.URI != ".runtime/cases/"+parentRunID {
		t.Fatalf("imported PostgreSQL evidence list record = %#v", record)
	}
}

func TestEvidenceListCommandPrintsStoreRecords(t *testing.T) {
	runID := uniqueTestID(t, "case-run-004")
	createStoredCaseRun(t, runID)

	out := runCLI(t, "evidence", "list", "--run", runID)

	for _, want := range []string{"Run: " + runID, "Case Run: " + runID + ".case", "Case: case.alpha", "Evidence: response"} {
		if !strings.Contains(out, want) {
			t.Fatalf("evidence list output missing %q: %q", want, out)
		}
	}
}

func TestEvidenceListCommandCanEmitJSON(t *testing.T) {
	runID := uniqueTestID(t, "case-run-005")
	createStoredCaseRun(t, runID)

	out := runCLI(t, "evidence", "list", "--run", runID, "--json")

	var report struct {
		Runs []struct {
			ID              string `json:"id"`
			APICaseRunCount int    `json:"apiCaseRunCount"`
			EvidenceCount   int    `json:"evidenceCount"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode evidence list json: %v\n%s", err, out)
	}
	if len(report.Runs) != 1 || report.Runs[0].ID != runID {
		t.Fatalf("json runs = %#v", report.Runs)
	}
	if report.Runs[0].APICaseRunCount != 1 || report.Runs[0].EvidenceCount != 5 {
		t.Fatalf("json run counts = %#v", report.Runs[0])
	}
}

func TestEvidenceListCommandRejectsMissingRun(t *testing.T) {
	runID := uniqueTestID(t, "case-run-006")
	createStoredCaseRun(t, runID)

	out := runCLIFails(t, "evidence", "list", "--run", "case-run-missing")
	if !strings.Contains(out, "run not found") || !strings.Contains(out, "case-run-missing") {
		t.Fatalf("missing run output = %q", out)
	}
}

func TestEvidenceReadCommandsRejectActiveSQLiteStore(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "list",
			args: []string{"evidence", "list", "--json"},
		},
		{
			name: "tasks",
			args: []string{"evidence", "tasks", "--run", "run.legacy", "--json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("OTSANDBOX_CONFIG_HOME", filepath.Join(dir, "config"))
			if err := saveStoreConfig(storeConfigFile{
				Active: "legacy-local",
				Stores: map[string]storeConfigEntry{
					"legacy-local": {Name: "legacy-local", URL: "sqlite://" + filepath.Join(dir, "store.sqlite"), Backend: "sqlite"},
				},
			}); err != nil {
				t.Fatalf("save store config: %v", err)
			}

			out := runCLIFails(t, tt.args...)
			for _, want := range []string{"daily commands require PostgreSQL Store", "SQLite", "postgres://"} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s output missing %q: %q", tt.name, want, out)
				}
			}
		})
	}
}

func TestEvidenceTasksCommandListsPostProcessTasks(t *testing.T) {
	storePath := createPostProcessTaskStore(t)

	out := runCLI(t,
		"evidence", "tasks",
		"--store", "sqlite://"+storePath,
		"--run", "run.tasks",
		"--step", "step-a",
		"--kind", "trace_topology_collect",
		"--json",
	)
	var report struct {
		RunID  string `json:"runId"`
		Counts struct {
			Total      int   `json:"total"`
			Passed     int   `json:"passed"`
			Failed     int   `json:"failed"`
			Running    int   `json:"running"`
			DurationMs int64 `json:"durationMs"`
		} `json:"counts"`
		Tasks []struct {
			ID            string `json:"id"`
			RunID         string `json:"runId"`
			StepID        string `json:"stepId"`
			Kind          string `json:"kind"`
			Status        string `json:"status"`
			Outcome       string `json:"outcome"`
			Reason        string `json:"reason"`
			DisplayStatus string `json:"displayStatus"`
			DurationMs    int64  `json:"durationMs"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode evidence tasks json: %v\n%s", err, out)
	}
	if report.RunID != "run.tasks" || report.Counts.Total != 1 || report.Counts.Passed != 1 || report.Counts.DurationMs != 125 {
		t.Fatalf("evidence tasks report = %#v", report)
	}
	if len(report.Tasks) != 1 || report.Tasks[0].ID != "task.trace" || report.Tasks[0].StepID != "step-a" || report.Tasks[0].Kind != "trace_topology_collect" {
		t.Fatalf("evidence tasks = %#v", report.Tasks)
	}
	if report.Tasks[0].Outcome != "success" || report.Tasks[0].Reason != "completed" || report.Tasks[0].DisplayStatus != "passed: completed" {
		t.Fatalf("evidence task readable status = %#v", report.Tasks[0])
	}

	textOut := runCLI(t, "evidence", "tasks", "--store", "sqlite://"+storePath, "--run", "run.tasks", "--status", "failed")
	for _, want := range []string{"Post Process Tasks: run.tasks", "task.logs", "runtime_log_collect", "300 ms", "log source missing"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("evidence tasks text missing %q:\n%s", want, textOut)
		}
	}
	skippedOut := runCLI(t, "evidence", "tasks", "--store", "sqlite://"+storePath, "--run", "run.tasks", "--status", "skipped")
	for _, want := range []string{"task.trace.skip", "skipped: SkyWalking provider unavailable"} {
		if !strings.Contains(skippedOut, want) {
			t.Fatalf("evidence skipped task text missing %q:\n%s", want, skippedOut)
		}
	}

	storeRef := "sqlite://" + storePath
	listOut := runCLI(t, "evidence", "list", "--store", storeRef, "--json")
	if !strings.Contains(listOut, "run.tasks") {
		t.Fatalf("evidence list --store output = %q", listOut)
	}
	tasksOut := runCLI(t, "evidence", "tasks", "--store", storeRef, "--run", "run.tasks", "--json")
	if !strings.Contains(tasksOut, "task.trace") || !strings.Contains(tasksOut, "task.logs") {
		t.Fatalf("evidence tasks --store output = %q", tasksOut)
	}
}

func TestCaseRunCommandWritesEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	evidenceDir := filepath.Join(dir, "evidence")
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-001", "--evidence-dir", evidenceDir, "--store", "sqlite://"+storePath)
	if !strings.Contains(out, "Case Run: case-run-001") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("case run output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "case-run-001", "summary.json")); err != nil {
		t.Fatalf("summary evidence missing: %v", err)
	}
}

func TestCaseRunCommandRequiresActiveStoreBeforeFileExecution(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	configHome := filepath.Join(dir, "config")

	out := runCLIFailsWithEnv(t, []string{"OTSANDBOX_CONFIG_HOME=" + configHome}, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-no-store")
	if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
		t.Fatalf("case run without store output = %q", out)
	}
	if called {
		t.Fatal("case run executed request before resolving active Store")
	}
}

func TestCaseRunCommandRejectsActiveSQLiteStoreBeforeFileExecution(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	t.Setenv("OTSANDBOX_CONFIG_HOME", filepath.Join(dir, "config"))
	storePath := filepath.Join(dir, "store.sqlite")
	if err := saveStoreConfig(storeConfigFile{
		Active: "legacy-local",
		Stores: map[string]storeConfigEntry{
			"legacy-local": {Name: "legacy-local", URL: "sqlite://" + storePath, Backend: "sqlite"},
		},
	}); err != nil {
		t.Fatalf("save store config: %v", err)
	}

	out := runCLIFails(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-active-sqlite")
	for _, want := range []string{"daily commands require PostgreSQL Store", "SQLite", "postgres://"} {
		if !strings.Contains(out, want) {
			t.Fatalf("case run with active SQLite store output missing %q: %q", want, out)
		}
	}
	if called {
		t.Fatal("case run executed request before rejecting active SQLite Store")
	}
}

func TestCaseRunCommandExecutesHTTPCase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["id"] != "item-override" {
			t.Fatalf("request overrides = %#v", request)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	evidenceDir := filepath.Join(dir, "evidence")
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-002", "--evidence-dir", evidenceDir, "--override", "id=item-override", "--store", "sqlite://"+storePath)
	if !strings.Contains(out, "Case Run: case-run-002") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("case run output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "case-run-002", "response.json")); err != nil {
		t.Fatalf("response evidence missing: %v", err)
	}
}

func TestCaseRunCommandIndexesStoreRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	storePath := filepath.Join(dir, "store.sqlite")
	evidenceDir := filepath.Join(dir, "evidence")

	runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-003", "--evidence-dir", evidenceDir, "--store", "sqlite://"+storePath, "--profile", "sample")

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	run, err := s.GetRun(context.Background(), "case-run-003")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.ProfileID != "sample" || run.Status != "passed" {
		t.Fatalf("run = %#v", run)
	}
	if !run.FinishedAt.After(run.StartedAt) {
		t.Fatalf("run timing was not indexed: %#v", run)
	}
	var runSummary struct {
		RunID  string `json:"runId"`
		CaseID string `json:"caseId"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(run.SummaryJSON), &runSummary); err != nil {
		t.Fatalf("decode run summary: %v", err)
	}
	if runSummary.RunID != "case-run-003" || runSummary.CaseID != "case.alpha" || runSummary.Status != "passed" {
		t.Fatalf("run summary = %#v", runSummary)
	}
	caseRuns, err := s.ListAPICaseRuns(context.Background(), "case-run-003")
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != "case.alpha" {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	if !caseRuns[0].FinishedAt.After(caseRuns[0].StartedAt) {
		t.Fatalf("case run timing was not indexed: %#v", caseRuns[0])
	}
	var requestSummary struct {
		Method  string `json:"method"`
		Path    string `json:"path"`
		HasBody bool   `json:"hasBody"`
	}
	if err := json.Unmarshal([]byte(caseRuns[0].RequestSummaryJSON), &requestSummary); err != nil {
		t.Fatalf("decode request summary: %v", err)
	}
	if requestSummary.Method != "POST" || requestSummary.Path != "/v1/items" || !requestSummary.HasBody {
		t.Fatalf("request summary = %#v", requestSummary)
	}
	var assertionSummary struct {
		Status     string `json:"status"`
		ErrorCount int    `json:"errorCount"`
	}
	if err := json.Unmarshal([]byte(caseRuns[0].AssertionSummaryJSON), &assertionSummary); err != nil {
		t.Fatalf("decode assertion summary: %v", err)
	}
	if assertionSummary.Status != "passed" || assertionSummary.ErrorCount != 0 {
		t.Fatalf("assertion summary = %#v", assertionSummary)
	}
	records, err := s.ListEvidence(context.Background(), "case-run-003")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("evidence records = %#v", records)
	}
	var responseSummary string
	for _, record := range records {
		if record.Kind == "response" {
			responseSummary = record.Summary
		}
	}
	var response struct {
		StatusCode int `json:"statusCode"`
		BodyBytes  int `json:"bodyBytes"`
	}
	if err := json.Unmarshal([]byte(responseSummary), &response); err != nil {
		t.Fatalf("decode response evidence summary: %v", err)
	}
	if response.StatusCode != http.StatusOK || response.BodyBytes == 0 {
		t.Fatalf("response evidence summary = %#v", response)
	}
}

func TestCaseRunCommandIndexesStoreRecordsWithStoreFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	storePath := filepath.Join(dir, "store.sqlite")
	evidenceDir := filepath.Join(dir, "evidence")

	runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-store-flag", "--evidence-dir", evidenceDir, "--store", "sqlite://"+storePath, "--profile", "sample")

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	run, err := s.GetRun(context.Background(), "case-run-store-flag")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.ProfileID != "sample" || run.Status != "passed" {
		t.Fatalf("run = %#v", run)
	}
}

func TestCaseRunCommandExecutesStoreCatalogCaseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/catalog" {
			t.Fatalf("request path = %s", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["id"] != "item-override" {
			t.Fatalf("request overrides = %#v", request)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	evidenceDir := filepath.Join(dir, "evidence")
	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.ReplaceProfileCatalog(context.Background(), store.ProfileCatalog{
		ProfileID: "sample",
		APICases: []store.CatalogAPICase{{
			ID:          "case.catalog",
			DisplayName: "Catalog Case",
			NodeID:      "node.alpha",
		}},
		TemplateConfigs: []store.CatalogTemplateConfig{{
			ID:         "cfg.case.catalog",
			ScopeType:  "api-case",
			ScopeID:    "case.catalog",
			ConfigJSON: `{"caseId":"case.catalog","caseExecution":{"method":"POST","nodeId":"node.alpha","path":"/v1/catalog","body":{"id":"{{ override:id }}"},"expectedHttpCodes":[201]}}`,
			Status:     "active",
		}},
	}); err != nil {
		t.Fatalf("replace catalog: %v", err)
	}
	s.Close()

	out := runCLI(t, "case", "run", "--case-id", "case.catalog", "--base-url", server.URL, "--run-id", "catalog-run-001", "--evidence-dir", evidenceDir, "--store", "sqlite://"+storePath, "--profile", "sample", "--override", "id=item-override", "--json")
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode case-id run json: %v\n%s", err, out)
	}
	if payload["runId"] != "catalog-run-001" || payload["caseRunId"] != "catalog-run-001.case" || payload["caseId"] != "case.catalog" || payload["status"] != "passed" {
		t.Fatalf("case-id run payload = %#v", payload)
	}

	s, err = sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s.Close()
	run, err := s.GetRun(context.Background(), "catalog-run-001")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.ProfileID != "sample" || run.Status != "passed" || run.EvidenceRoot != filepath.Join(evidenceDir, "catalog-run-001") {
		t.Fatalf("run = %#v", run)
	}
	caseRuns, err := s.ListAPICaseRuns(context.Background(), "catalog-run-001")
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != "case.catalog" || caseRuns[0].Status != "passed" {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	records, err := s.ListEvidence(context.Background(), "catalog-run-001")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("evidence records = %#v", records)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "catalog-run-001", "request.json")); err != nil {
		t.Fatalf("request evidence missing: %v", err)
	}
}

func TestInterfaceNodeCaseReportRunsAllCasesByTargetName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "bad":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"status":"rejected","password":"variant-secret"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"accepted","token":"report-secret"}`)
		}
	}))
	defer server.Close()
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store", "sqlite://"+storePath)
	listOut := runCLI(t, "interface-node", "discover", "--store", "sqlite://"+storePath, "--filter", "Result Lookup", "--json")
	var listReport struct {
		Items []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listOut), &listReport); err != nil {
		t.Fatalf("decode interface-node discover json: %v\n%s", err, listOut)
	}
	if len(listReport.Items) != 1 || listReport.Items[0].ID != "node.alpha" {
		t.Fatalf("interface-node discover = %#v", listReport.Items)
	}

	outputDir := filepath.Join(t.TempDir(), "report")
	out := runCLI(t,
		"interface-node", "case", "report",
		"--node", listReport.Items[0].ID,
		"--store", "sqlite://"+storePath,
		"--base-url", server.URL,
		"--output-dir", outputDir,
		"--timeout-seconds", "1",
		"--json",
	)

	var report struct {
		OK        bool   `json:"ok"`
		NodeID    string `json:"nodeId"`
		ReportURL string `json:"reportUrl"`
		Counts    struct {
			Total          int `json:"total"`
			Passed         int `json:"passed"`
			Failed         int `json:"failed"`
			DerivedConfigs int `json:"derivedConfigs"`
		} `json:"counts"`
		Results []struct {
			RunID       string `json:"runId"`
			CaseRunID   string `json:"caseRunId"`
			DetailURL   string `json:"detailUrl"`
			BodyPreview string `json:"bodyPreview"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode report json: %v\n%s", err, out)
	}
	if !report.OK || report.NodeID != "node.alpha" || report.Counts.Total != 2 || report.Counts.Passed != 2 || report.Counts.Failed != 0 || report.Counts.DerivedConfigs != 1 {
		t.Fatalf("report = %#v", report)
	}
	if len(report.Results) != 2 || report.Results[0].RunID == "" || report.Results[0].CaseRunID != report.Results[0].RunID+".case" || report.Results[0].DetailURL == "" {
		t.Fatalf("report evidence handles = %#v", report.Results)
	}
	for _, item := range report.Results {
		if strings.Contains(item.BodyPreview, "report-secret") || strings.Contains(item.BodyPreview, "variant-secret") {
			t.Fatalf("report body preview leaked sensitive value: %#v", item)
		}
		if !strings.Contains(item.BodyPreview, "[REDACTED]") {
			t.Fatalf("report body preview was not redacted: %#v", item)
		}
	}
	if _, err := os.Stat(filepath.Join(outputDir, "report.json")); err != nil {
		t.Fatalf("json report missing: %v", err)
	}
	htmlPath := filepath.Join(outputDir, "report.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("html report missing: %v", err)
	}
	for _, want := range []string{"Result Lookup", "Case Alpha Default", "Case Alpha Variant", "passed", "caseRunId"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("html report missing %q:\n%s", want, html)
		}
	}
	for _, leaked := range []string{"report-secret", "variant-secret"} {
		if strings.Contains(string(html), leaked) {
			t.Fatalf("html report leaked %q:\n%s", leaked, html)
		}
	}
	if report.ReportURL != htmlPath {
		t.Fatalf("report url = %q want %q", report.ReportURL, htmlPath)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "runtime.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("report should use selected Store without creating runtime.sqlite, stat err=%v", err)
	}
}

func TestCaseExecutionAndInterfaceReportUseNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-case-exec-pg")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/items":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"created"}`)
		case "/lookup":
			switch r.URL.Query().Get("mode") {
			case "bad":
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, `{"status":"rejected","password":"variant-secret"}`)
			default:
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{"status":"accepted","token":"report-secret"}`)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	fileRunID := "pg-file-case-run-" + suffix
	fileEvidenceDir := filepath.Join(dir, "file-evidence")
	fileOut := runCLI(t,
		"case", "run",
		"--case", casePath,
		"--base-url", server.URL,
		"--run-id", fileRunID,
		"--evidence-dir", fileEvidenceDir,
		"--profile", "sample",
	)
	if !strings.Contains(fileOut, "Case Run: "+fileRunID) || !strings.Contains(fileOut, "Status: passed") {
		t.Fatalf("file case run via active PostgreSQL Store = %q", fileOut)
	}
	caseRunsOut := runCLI(t, "case", "runs", "--run", fileRunID, "--json")
	if !strings.Contains(caseRunsOut, fileRunID) || !strings.Contains(caseRunsOut, "case.alpha") {
		t.Fatalf("case runs via active PostgreSQL Store = %s", caseRunsOut)
	}
	fileEvidenceOut := runCLI(t, "case", "evidence", "--run", fileRunID, "--case-id", "case.alpha", "--json")
	for _, want := range []string{fileRunID, "case.alpha", "request", "response"} {
		if !strings.Contains(fileEvidenceOut, want) {
			t.Fatalf("file case evidence via active PostgreSQL Store missing %q:\n%s", want, fileEvidenceOut)
		}
	}
	evidenceListOut := runCLI(t, "evidence", "list", "--run", fileRunID, "--json")
	if !strings.Contains(evidenceListOut, fileRunID) || !strings.Contains(evidenceListOut, "response") {
		t.Fatalf("evidence list via active PostgreSQL Store = %s", evidenceListOut)
	}

	profileDir := writeInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)
	catalogRunID := "pg-catalog-case-run-" + suffix
	catalogOut := runCLI(t,
		"case", "run",
		"--case-id", "case.alpha.default",
		"--base-url", server.URL,
		"--run-id", catalogRunID,
		"--evidence-dir", filepath.Join(dir, "catalog-evidence"),
		"--profile", "sample",
		"--json",
	)
	var catalogRun struct {
		RunID  string `json:"runId"`
		CaseID string `json:"caseId"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(catalogOut), &catalogRun); err != nil {
		t.Fatalf("decode PostgreSQL catalog case run json: %v\n%s", err, catalogOut)
	}
	if catalogRun.RunID != catalogRunID || catalogRun.CaseID != "case.alpha.default" || catalogRun.Status != "passed" {
		t.Fatalf("PostgreSQL catalog case run = %#v", catalogRun)
	}
	catalogEvidenceOut := runCLI(t, "case", "evidence", "--run", catalogRunID, "--case-id", "case.alpha.default", "--json")
	for _, want := range []string{catalogRunID, "case.alpha.default", "request", "response"} {
		if !strings.Contains(catalogEvidenceOut, want) {
			t.Fatalf("catalog case evidence via active PostgreSQL Store missing %q:\n%s", want, catalogEvidenceOut)
		}
	}

	listOut := runCLI(t, "interface-node", "discover", "--filter", "Result Lookup", "--json")
	var listReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listOut), &listReport); err != nil {
		t.Fatalf("decode PostgreSQL interface-node discover json: %v\n%s", err, listOut)
	}
	if len(listReport.Items) != 1 || listReport.Items[0].ID != "node.alpha" {
		t.Fatalf("PostgreSQL interface-node discover = %#v", listReport.Items)
	}

	outputDir := filepath.Join(t.TempDir(), "pg-interface-report")
	reportOut := runCLI(t,
		"interface-node", "case", "report",
		"--node", listReport.Items[0].ID,
		"--base-url", server.URL,
		"--output-dir", outputDir,
		"--timeout-seconds", "1",
		"--json",
	)
	var report struct {
		OK     bool   `json:"ok"`
		NodeID string `json:"nodeId"`
		Counts struct {
			Total          int `json:"total"`
			Passed         int `json:"passed"`
			Failed         int `json:"failed"`
			DerivedConfigs int `json:"derivedConfigs"`
		} `json:"counts"`
		Results []struct {
			RunID       string `json:"runId"`
			CaseRunID   string `json:"caseRunId"`
			DetailURL   string `json:"detailUrl"`
			BodyPreview string `json:"bodyPreview"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(reportOut), &report); err != nil {
		t.Fatalf("decode PostgreSQL interface-node report json: %v\n%s", err, reportOut)
	}
	if !report.OK || report.NodeID != "node.alpha" || report.Counts.Total != 2 || report.Counts.Passed != 2 || report.Counts.Failed != 0 || report.Counts.DerivedConfigs != 1 {
		t.Fatalf("PostgreSQL interface-node report = %#v", report)
	}
	if len(report.Results) != 2 || report.Results[0].RunID == "" || report.Results[0].CaseRunID != report.Results[0].RunID+".case" || report.Results[0].DetailURL == "" {
		t.Fatalf("PostgreSQL interface-node report handles = %#v", report.Results)
	}
	for _, item := range report.Results {
		if strings.Contains(item.BodyPreview, "report-secret") || strings.Contains(item.BodyPreview, "variant-secret") {
			t.Fatalf("PostgreSQL interface-node report body preview leaked sensitive value: %#v", item)
		}
		if !strings.Contains(item.BodyPreview, "[REDACTED]") {
			t.Fatalf("PostgreSQL interface-node report body preview was not redacted: %#v", item)
		}
	}
	if _, err := os.Stat(filepath.Join(outputDir, "runtime.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PostgreSQL interface-node report should use active Store without creating runtime.sqlite, stat err=%v", err)
	}
}

func TestDailyReportExecutionsUseSelectedStoreWithoutSQLiteDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lookup":
			if r.URL.Query().Get("mode") == "bad" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, `{"status":"rejected"}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
		case "/first":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"item_id":"item-001"}`)
		case "/second":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	sourceStore, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "store.sqlite")})
	if err != nil {
		t.Fatalf("open selected store before disabling sqlite: %v", err)
	}
	defer sourceStore.Close()
	t.Setenv("OTSANDBOX_DISABLE_SQLITE_STORE", "1")

	interfaceBundle, err := profile.Load(writeInterfaceNodeBatchReportProfile(t))
	if err != nil {
		t.Fatalf("load interface bundle: %v", err)
	}
	node, err := findInterfaceNodeByID(interfaceBundle.InterfaceNodes, "node.alpha")
	if err != nil {
		t.Fatalf("find node: %v", err)
	}
	cases := interfaceNodeReportCases(interfaceBundle.APICases, node.ID)
	derived := deriveInterfaceNodeCaseConfigs(interfaceBundle, node, cases)
	interfaceBundle.TemplateConfigs = mergeTemplateConfigs(interfaceBundle.TemplateConfigs, derived)
	interfaceDir := filepath.Join(t.TempDir(), "interface-report")
	interfaceReport, err := executeInterfaceNodeCaseReport(ctx, interfaceBundle, node, cases, derived, sourceStore, "selected-store", server.URL, interfaceDir, 1)
	if err != nil {
		t.Fatalf("execute interface report with selected store: %v", err)
	}
	if !interfaceReport.OK || interfaceReport.Counts.Total != 2 || interfaceReport.Counts.Passed != 2 {
		t.Fatalf("interface report = %#v", interfaceReport)
	}
	if _, err := os.Stat(filepath.Join(interfaceDir, "runtime.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interface report created runtime.sqlite, stat err=%v", err)
	}

	workflowBundle, err := profile.Load(writeWorkflowBatchReportProfile(t))
	if err != nil {
		t.Fatalf("load workflow bundle: %v", err)
	}
	workflowDir := filepath.Join(t.TempDir(), "workflow-report")
	workflowReport, err := executeWorkflowCaseReport(ctx, workflowBundle, sourceStore, "workflow.alpha", workflowDir, server.URL)
	if err != nil {
		t.Fatalf("execute workflow report with selected store: %v", err)
	}
	if !workflowReport.OK || workflowReport.Counts.Total != 2 || workflowReport.Counts.Passed != 2 || workflowReport.RunID == "" {
		t.Fatalf("workflow report = %#v", workflowReport)
	}
	if _, err := os.Stat(filepath.Join(workflowDir, "runtime.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workflow report created runtime.sqlite, stat err=%v", err)
	}

	suiteDir := filepath.Join(t.TempDir(), "suite-report")
	suiteReport, err := executeCaseSuiteReport(ctx, interfaceBundle, cases, derived, sourceStore, "selected-store", caseListFilter{}, server.URL, suiteDir, 1)
	if err != nil {
		t.Fatalf("execute suite report with selected store: %v", err)
	}
	if !suiteReport.OK || suiteReport.Counts.Total != 2 || suiteReport.Counts.Passed != 2 {
		t.Fatalf("suite report = %#v", suiteReport)
	}
	if _, err := os.Stat(filepath.Join(suiteDir, "runtime.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("suite report created runtime.sqlite, stat err=%v", err)
	}
}

func TestInterfaceNodeCaseReportRequiresStoreBeforeProfileLoad(t *testing.T) {
	env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}
	out := runCLIFailsWithEnv(t, env,
		"interface-node", "case", "report",
		"--node", "node.alpha",
		"--profile", filepath.Join(t.TempDir(), "missing-profile"),
		"--json",
	)
	if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
		t.Fatalf("interface-node case report output = %q", out)
	}
	if strings.Contains(out, "missing-profile") {
		t.Fatalf("interface-node case report loaded profile before Store binding: %q", out)
	}
}

func TestWorkflowReportRequiresStoreBeforeProfileLoad(t *testing.T) {
	env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}
	out := runCLIFailsWithEnv(t, env,
		"workflow", "report",
		"--workflow", "workflow.alpha",
		"--profile", filepath.Join(t.TempDir(), "missing-profile"),
		"--json",
	)
	if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
		t.Fatalf("workflow report output = %q", out)
	}
	if strings.Contains(out, "missing-profile") {
		t.Fatalf("workflow report loaded profile before Store binding: %q", out)
	}
}

func TestCaseSuiteReportRequiresStoreBeforeProfileLoad(t *testing.T) {
	env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}
	out := runCLIFailsWithEnv(t, env,
		"case", "suite", "report",
		"--profile", filepath.Join(t.TempDir(), "missing-profile"),
		"--json",
	)
	if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
		t.Fatalf("case suite report output = %q", out)
	}
	if strings.Contains(out, "missing-profile") {
		t.Fatalf("case suite report loaded profile before Store binding: %q", out)
	}
}

func TestDailyPlanningCommandsRequireStoreBeforeProfileLoad(t *testing.T) {
	missingProfile := filepath.Join(t.TempDir(), "missing-profile")
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "interface-node coverage",
			args: []string{"interface-node", "coverage", "--profile", missingProfile, "--json"},
		},
		{
			name: "interface-node coverage-gaps",
			args: []string{"interface-node", "coverage-gaps", "--profile", missingProfile, "--json"},
		},
		{
			name: "workflow plan",
			args: []string{"workflow", "plan", "--workflow", "workflow.alpha", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite stability",
			args: []string{"case", "suite", "stability", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite coverage",
			args: []string{"case", "suite", "coverage", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite priority",
			args: []string{"case", "suite", "priority", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite brief",
			args: []string{"case", "suite", "brief", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite quality",
			args: []string{"case", "suite", "quality", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite quality-plan",
			args: []string{"case", "suite", "quality-plan", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite quality-report",
			args: []string{"case", "suite", "quality-report", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite inspect",
			args: []string{"case", "suite", "inspect", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite plan",
			args: []string{"case", "suite", "plan", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite impact",
			args: []string{"case", "suite", "impact", "--profile", missingProfile, "--json"},
		},
		{
			name: "case suite impact-report",
			args: []string{"case", "suite", "impact-report", "--profile", missingProfile, "--json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}
			out := runCLIFailsWithEnv(t, env, tt.args...)
			if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
				t.Fatalf("%s output = %q", tt.name, out)
			}
			if strings.Contains(out, "missing-profile") {
				t.Fatalf("%s loaded profile before Store binding: %q", tt.name, out)
			}
		})
	}
}

func TestExecutorAndTemplateCommandsRequireStoreBeforeProfileLoad(t *testing.T) {
	missingProfile := filepath.Join(t.TempDir(), "missing-profile")
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "executor plan",
			args: []string{"executor", "plan", "--profile", missingProfile, "--json"},
		},
		{
			name: "template render",
			args: []string{"template", "render", "--profile", missingProfile, "--template", "template.create"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}
			out := runCLIFailsWithEnv(t, env, tt.args...)
			if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
				t.Fatalf("%s output = %q", tt.name, out)
			}
			if strings.Contains(out, "missing-profile") {
				t.Fatalf("%s loaded profile before Store binding: %q", tt.name, out)
			}
		})
	}
}

func TestAuditCommandsRequireExplicitStoreOrOfflineReviewBeforeProfileLoad(t *testing.T) {
	missingProfile := filepath.Join(t.TempDir(), "missing-profile")
	tests := []struct {
		name       string
		args       []string
		wantPieces []string
	}{
		{
			name:       "workflow audit",
			args:       []string{"workflow", "audit", "--profile", missingProfile, "--workflow", "workflow.alpha", "--json"},
			wantPieces: []string{"--offline-template-package", "--store"},
		},
		{
			name:       "profile audit",
			args:       []string{"profile", "audit", "--profile", missingProfile, "--json"},
			wantPieces: []string{"--offline-template-package"},
		},
		{
			name:       "profile audit-plan",
			args:       []string{"profile", "audit-plan", "--profile", missingProfile, "--json"},
			wantPieces: []string{"--offline-template-package"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}
			out := runCLIFailsWithEnv(t, env, tt.args...)
			for _, want := range tt.wantPieces {
				if !strings.Contains(out, want) {
					t.Fatalf("%s output missing %q: %q", tt.name, want, out)
				}
			}
			if strings.Contains(out, "missing-profile") {
				t.Fatalf("%s loaded profile before Store binding: %q", tt.name, out)
			}
		})
	}
}

func TestCaseDiscoverFiltersByMaintenanceMetadata(t *testing.T) {
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	configureNamedPostgreSQLActiveStore(t, "daily-case-discover-pg")
	runCLI(t, "config", "publish", "--from", profileDir)

	out := runCLI(t,
		"case", "discover",
		"--tag", "smoke",
		"--status", "active",
		"--owner", "team-a",
		"--json",
	)

	var report struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
		Items []struct {
			ID          string   `json:"id"`
			DisplayName string   `json:"displayName"`
			NodeID      string   `json:"nodeId"`
			Tags        []string `json:"tags"`
			Priority    string   `json:"priority"`
			Owner       string   `json:"owner"`
			Description string   `json:"description"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode case discover json: %v\n%s", err, out)
	}
	if !report.OK || report.Count != 1 || len(report.Items) != 1 {
		t.Fatalf("case discover report = %#v", report)
	}
	item := report.Items[0]
	if item.ID != "case.alpha.default" || item.NodeID != "node.alpha" || item.Priority != "p0" || item.Owner != "team-a" {
		t.Fatalf("case discover item = %#v", item)
	}
	if strings.Join(item.Tags, ",") != "smoke,regression" || item.Description == "" {
		t.Fatalf("case discover metadata = %#v", item)
	}

	filtered := runCLI(t, "case", "discover", "--filter", "variant", "--json")
	var filteredReport struct {
		Items []struct {
			ID    string   `json:"id"`
			Tags  []string `json:"tags"`
			Owner string   `json:"owner"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(filtered), &filteredReport); err != nil {
		t.Fatalf("decode filtered case discover json: %v\n%s", err, filtered)
	}
	if len(filteredReport.Items) != 1 || filteredReport.Items[0].ID != "case.alpha.variant" || filteredReport.Items[0].Owner != "team-b" {
		t.Fatalf("filtered case discover = %#v", filteredReport.Items)
	}
}

func TestCaseDiscoverRequiresStoreUnlessOfflineTemplatePackage(t *testing.T) {
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}

	missingStore := runCLIFailsWithEnv(t, env, "case", "discover", "--profile", profileDir, "--json")
	if !strings.Contains(missingStore, "--offline-template-package") || !strings.Contains(missingStore, "--store") {
		t.Fatalf("case discover package-only output = %q", missingStore)
	}

	out := runCLIWithEnv(t, env, "case", "discover", "--profile", profileDir, "--offline-template-package", "--filter", "variant", "--json")
	var report struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode offline case discover json: %v\n%s", err, out)
	}
	if len(report.Items) != 1 || report.Items[0].ID != "case.alpha.variant" {
		t.Fatalf("offline case discover = %#v", report.Items)
	}
}

func TestDiscoverCommandsAcceptStoreFlagAsLocationAgnosticStoreSelector(t *testing.T) {
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store", "sqlite://"+storePath)
	storeRef := "sqlite://" + storePath

	caseOut := runCLI(t, "case", "discover", "--store", storeRef, "--filter", "variant", "--json")
	var caseReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(caseOut), &caseReport); err != nil {
		t.Fatalf("decode case discover json: %v\n%s", err, caseOut)
	}
	if len(caseReport.Items) != 1 || caseReport.Items[0].ID != "case.alpha.variant" {
		t.Fatalf("case discover via --store = %#v", caseReport.Items)
	}

	nodeOut := runCLI(t, "interface-node", "discover", "--store", storeRef, "--filter", "Result Lookup", "--json")
	var nodeReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(nodeOut), &nodeReport); err != nil {
		t.Fatalf("decode interface-node discover json: %v\n%s", err, nodeOut)
	}
	if len(nodeReport.Items) != 1 || nodeReport.Items[0].ID != "node.alpha" {
		t.Fatalf("interface-node discover via --store = %#v", nodeReport.Items)
	}

	workflowProfileDir := writeWorkflowBatchReportProfile(t)
	workflowStorePath := filepath.Join(t.TempDir(), "workflow-store.sqlite")
	runCLI(t, "config", "publish", "--from", workflowProfileDir, "--store", "sqlite://"+workflowStorePath)
	workflowOut := runCLI(t, "workflow", "discover", "--store", "sqlite://"+workflowStorePath, "--filter", "Workflow Alpha", "--json")
	var workflowReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(workflowOut), &workflowReport); err != nil {
		t.Fatalf("decode workflow discover json: %v\n%s", err, workflowOut)
	}
	if len(workflowReport.Items) != 1 || workflowReport.Items[0].ID != "workflow.alpha" {
		t.Fatalf("workflow discover via --store = %#v", workflowReport.Items)
	}
}

func TestDiscoverCommandsRejectActiveSQLiteStore(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "case discover", args: []string{"case", "discover", "--json"}},
		{name: "interface-node discover", args: []string{"interface-node", "discover", "--json"}},
		{name: "workflow discover", args: []string{"workflow", "discover", "--json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("OTSANDBOX_CONFIG_HOME", filepath.Join(dir, "config"))
			if err := saveStoreConfig(storeConfigFile{
				Active: "legacy-local",
				Stores: map[string]storeConfigEntry{
					"legacy-local": {Name: "legacy-local", URL: "sqlite://" + filepath.Join(dir, "store.sqlite"), Backend: "sqlite"},
				},
			}); err != nil {
				t.Fatalf("save store config: %v", err)
			}

			out := runCLIFails(t, tt.args...)
			for _, want := range []string{"daily commands require PostgreSQL Store", "SQLite", "postgres://"} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s output missing %q: %q", tt.name, want, out)
				}
			}
		})
	}
}

func TestDiscoverCommandsUseNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-pg")

	profileDir := writeInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	caseOut := runCLI(t, "case", "discover", "--filter", "variant", "--json")
	var caseReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(caseOut), &caseReport); err != nil {
		t.Fatalf("decode case discover json: %v\n%s", err, caseOut)
	}
	if len(caseReport.Items) != 1 || caseReport.Items[0].ID != "case.alpha.variant" {
		t.Fatalf("case discover via active PostgreSQL Store = %#v", caseReport.Items)
	}

	nodeOut := runCLI(t, "interface-node", "discover", "--filter", "Result Lookup", "--json")
	var nodeReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(nodeOut), &nodeReport); err != nil {
		t.Fatalf("decode interface-node discover json: %v\n%s", err, nodeOut)
	}
	if len(nodeReport.Items) != 1 || nodeReport.Items[0].ID != "node.alpha" {
		t.Fatalf("interface-node discover via active PostgreSQL Store = %#v", nodeReport.Items)
	}
}

func TestDailyWorkflowCommandsUseNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-workflow-pg")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/first":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"item_id":"item-001"}`)
		case "/second":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"ok"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(payload.Query, "queryTrace") {
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
		_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.pg.daily","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/first","type":"Entry","component":"Tomcat"},{"traceId":"trace.pg.daily","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.pg.daily","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"GET:/first","type":"Entry","component":"Server"}]}}}`))
	}))
	defer provider.Close()

	profileDir := writeWorkflowBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)
	workflowOut := runCLI(t, "workflow", "discover", "--filter", "Workflow Alpha", "--json")
	var workflowList struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(workflowOut), &workflowList); err != nil {
		t.Fatalf("decode workflow discover json: %v\n%s", err, workflowOut)
	}
	if len(workflowList.Items) != 1 || workflowList.Items[0].ID != "workflow.alpha" {
		t.Fatalf("workflow discover via active PostgreSQL Store = %#v", workflowList.Items)
	}

	planOut := runCLI(t, "workflow", "plan", "--workflow", "workflow.alpha", "--json")
	var plan struct {
		Counts struct {
			Steps int `json:"steps"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(planOut), &plan); err != nil {
		t.Fatalf("decode workflow plan json: %v\n%s", err, planOut)
	}
	if plan.Counts.Steps != 2 {
		t.Fatalf("workflow plan via active PostgreSQL Store = %#v", plan)
	}

	runCLI(t, "baseline", "set", "--profile", "sample", "--subject", "workflow.alpha", "--status", "passed", "--required")
	baselineOut := runCLI(t, "baseline", "get", "--profile", "sample", "--subject", "workflow.alpha")
	if !strings.Contains(baselineOut, "Status: passed") || !strings.Contains(baselineOut, "Required: true") {
		t.Fatalf("baseline get via active PostgreSQL Store = %q", baselineOut)
	}

	reportOut := runCLI(t,
		"workflow", "report",
		"--workflow", "workflow.alpha",
		"--base-url", server.URL,
		"--output-dir", filepath.Join(t.TempDir(), "workflow-report"),
		"--json",
	)
	var report struct {
		OK     bool   `json:"ok"`
		RunID  string `json:"runId"`
		Counts struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(reportOut), &report); err != nil {
		t.Fatalf("decode workflow report json: %v\n%s", err, reportOut)
	}
	if !report.OK || report.RunID == "" || report.Counts.Total != 2 || report.Counts.Passed != 2 || report.Counts.Failed != 0 {
		t.Fatalf("workflow report via active PostgreSQL Store = %#v", report)
	}

	caseRunsOut := runCLI(t, "case", "runs", "--run", report.RunID, "--json")
	if !strings.Contains(caseRunsOut, "case.first") || !strings.Contains(caseRunsOut, "case.second") {
		t.Fatalf("case runs via active PostgreSQL Store = %s", caseRunsOut)
	}
	traceOut := runCLI(t, "trace", "topology", "collect",
		"--trace-graphql-url", provider.URL,
		"--run", report.RunID,
		"--step", "first",
		"--case", "case.first",
		"--request", "request.pg.daily",
		"--trace-id", "trace.pg.daily",
		"--json",
	)
	if !strings.Contains(traceOut, `"provider":"skywalking"`) || !strings.Contains(traceOut, `"status":"complete"`) || !strings.Contains(traceOut, "trace.pg.daily") {
		t.Fatalf("trace topology via active PostgreSQL Store = %s", traceOut)
	}
	evidenceOut := runCLI(t, "case", "evidence", "--run", report.RunID, "--case-id", "case.first", "--step-id", "first", "--json")
	if !strings.Contains(evidenceOut, `"provider":"skywalking"`) || !strings.Contains(evidenceOut, "trace.pg.daily") {
		t.Fatalf("case evidence via active PostgreSQL Store = %s", evidenceOut)
	}
}

func TestInterfaceNodeDiscoverRequiresStoreUnlessOfflineTemplatePackage(t *testing.T) {
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}

	missingStore := runCLIFailsWithEnv(t, env, "interface-node", "discover", "--profile", profileDir, "--json")
	if !strings.Contains(missingStore, "--offline-template-package") || !strings.Contains(missingStore, "--store") {
		t.Fatalf("interface-node discover package-only output = %q", missingStore)
	}

	out := runCLIWithEnv(t, env, "interface-node", "discover", "--profile", profileDir, "--offline-template-package", "--filter", "Result Lookup", "--json")
	var report struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode offline interface-node discover json: %v\n%s", err, out)
	}
	if len(report.Items) != 1 || report.Items[0].ID != "node.alpha" {
		t.Fatalf("offline interface-node discover = %#v", report.Items)
	}
}

func TestWorkflowDiscoverRequiresStoreUnlessOfflineTemplatePackage(t *testing.T) {
	profileDir := writeWorkflowBatchReportProfile(t)
	env := []string{"OTSANDBOX_CONFIG_HOME=" + t.TempDir()}

	missingStore := runCLIFailsWithEnv(t, env, "workflow", "discover", "--profile", profileDir, "--json")
	if !strings.Contains(missingStore, "--offline-template-package") || !strings.Contains(missingStore, "--store") {
		t.Fatalf("workflow discover package-only output = %q", missingStore)
	}

	out := runCLIWithEnv(t, env, "workflow", "discover", "--profile", profileDir, "--offline-template-package", "--filter", "Workflow Alpha", "--json")
	var report struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode offline workflow discover json: %v\n%s", err, out)
	}
	if len(report.Items) != 1 || report.Items[0].ID != "workflow.alpha" {
		t.Fatalf("offline workflow discover = %#v", report.Items)
	}
}

func TestCaseSuiteReportRunsCasesByMaintenanceFilters(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-case-suite-report-pg")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "bad":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"status":"rejected"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
		}
	}))
	defer server.Close()
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	outputDir := filepath.Join(t.TempDir(), "suite-report")
	out := runCLI(t,
		"case", "suite", "report",
		"--tag", "smoke",
		"--owner", "team-a",
		"--base-url", server.URL,
		"--output-dir", outputDir,
		"--json",
	)

	var report struct {
		OK             bool   `json:"ok"`
		JUnitReportURL string `json:"junitReportUrl"`
		Filters        struct {
			Tags  []string `json:"tags"`
			Owner string   `json:"owner"`
		} `json:"filters"`
		Counts struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"counts"`
		Results []struct {
			CaseID    string   `json:"caseId"`
			Title     string   `json:"title"`
			NodeID    string   `json:"nodeId"`
			Tags      []string `json:"tags"`
			Priority  string   `json:"priority"`
			Owner     string   `json:"owner"`
			Status    string   `json:"status"`
			CaseRunID string   `json:"caseRunId"`
			DetailURL string   `json:"detailUrl"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite report json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Total != 1 || report.Counts.Passed != 1 || report.Counts.Failed != 0 {
		t.Fatalf("suite report = %#v", report)
	}
	if strings.Join(report.Filters.Tags, ",") != "smoke" || report.Filters.Owner != "team-a" {
		t.Fatalf("suite filters = %#v", report.Filters)
	}
	if len(report.Results) != 1 {
		t.Fatalf("suite results = %#v", report.Results)
	}
	item := report.Results[0]
	if item.CaseID != "case.alpha.default" || item.NodeID != "node.alpha" || item.Priority != "p0" || item.Owner != "team-a" || item.CaseRunID == "" || item.DetailURL == "" {
		t.Fatalf("suite result item = %#v", item)
	}
	if strings.Join(item.Tags, ",") != "smoke,regression" {
		t.Fatalf("suite result tags = %#v", item.Tags)
	}
	html, err := os.ReadFile(filepath.Join(outputDir, "report.html"))
	if err != nil {
		t.Fatalf("suite html report missing: %v", err)
	}
	for _, want := range []string{"Case Suite Report", "Case Alpha Default", "team-a", "smoke", "p0", "caseRunId"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("suite html missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(string(html), "Case Alpha Variant") {
		t.Fatalf("suite html should not include unselected case:\n%s", html)
	}
	junitPath := filepath.Join(outputDir, "report.junit.xml")
	junitRaw, err := os.ReadFile(junitPath)
	if err != nil {
		t.Fatalf("suite junit report missing: %v", err)
	}
	if report.JUnitReportURL != junitPath {
		t.Fatalf("junit report url = %q want %q", report.JUnitReportURL, junitPath)
	}
	for _, want := range []string{`<testsuite name="Case Suite Report" tests="1" failures="0"`, `name="case.alpha.default"`, `classname="node.alpha"`} {
		if !strings.Contains(string(junitRaw), want) {
			t.Fatalf("suite junit missing %q:\n%s", want, junitRaw)
		}
	}

	variantOut := runCLI(t,
		"case", "suite", "report",
		"--tag", "negative",
		"--base-url", server.URL,
		"--output-dir", filepath.Join(t.TempDir(), "variant-suite-report"),
		"--json",
	)
	var variantReport struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total          int `json:"total"`
			Passed         int `json:"passed"`
			DerivedConfigs int `json:"derivedConfigs"`
		} `json:"counts"`
		Results []struct {
			CaseID   string `json:"caseId"`
			Priority string `json:"priority"`
			Owner    string `json:"owner"`
			HTTPCode int    `json:"httpCode"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(variantOut), &variantReport); err != nil {
		t.Fatalf("decode variant suite report json: %v\n%s", err, variantOut)
	}
	if !variantReport.OK || variantReport.Counts.Total != 1 || variantReport.Counts.Passed != 1 || variantReport.Counts.DerivedConfigs != 1 {
		t.Fatalf("variant suite report = %#v", variantReport)
	}
	if len(variantReport.Results) != 1 || variantReport.Results[0].CaseID != "case.alpha.variant" || variantReport.Results[0].HTTPCode != http.StatusBadRequest {
		t.Fatalf("variant suite result = %#v", variantReport.Results)
	}
}

func TestCaseSuiteCommandsUseNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-suite-pg")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "bad":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"status":"rejected"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
		}
	}))
	defer server.Close()
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	reportOut := runCLI(t,
		"case", "suite", "report",
		"--tag", "smoke",
		"--owner", "team-a",
		"--base-url", server.URL,
		"--output-dir", filepath.Join(t.TempDir(), "pg-suite-report"),
		"--json",
	)
	var suiteReport struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"counts"`
		Results []struct {
			CaseID    string `json:"caseId"`
			CaseRunID string `json:"caseRunId"`
			DetailURL string `json:"detailUrl"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(reportOut), &suiteReport); err != nil {
		t.Fatalf("decode PostgreSQL suite report json: %v\n%s", err, reportOut)
	}
	if !suiteReport.OK || suiteReport.Counts.Total != 1 || suiteReport.Counts.Passed != 1 || suiteReport.Counts.Failed != 0 || len(suiteReport.Results) != 1 {
		t.Fatalf("PostgreSQL suite report = %#v", suiteReport)
	}
	if suiteReport.Results[0].CaseID != "case.alpha.default" || suiteReport.Results[0].CaseRunID == "" || suiteReport.Results[0].DetailURL == "" {
		t.Fatalf("PostgreSQL suite report result = %#v", suiteReport.Results[0])
	}

	variantOut := runCLI(t,
		"case", "suite", "report",
		"--tag", "negative",
		"--base-url", server.URL,
		"--output-dir", filepath.Join(t.TempDir(), "pg-variant-suite-report"),
		"--json",
	)
	var variantReport struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total          int `json:"total"`
			Passed         int `json:"passed"`
			DerivedConfigs int `json:"derivedConfigs"`
		} `json:"counts"`
		Results []struct {
			CaseID   string `json:"caseId"`
			HTTPCode int    `json:"httpCode"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(variantOut), &variantReport); err != nil {
		t.Fatalf("decode PostgreSQL variant suite report json: %v\n%s", err, variantOut)
	}
	if !variantReport.OK || variantReport.Counts.Total != 1 || variantReport.Counts.Passed != 1 || variantReport.Counts.DerivedConfigs != 1 {
		t.Fatalf("PostgreSQL variant suite report = %#v", variantReport)
	}
	if len(variantReport.Results) != 1 || variantReport.Results[0].CaseID != "case.alpha.variant" || variantReport.Results[0].HTTPCode != http.StatusBadRequest {
		t.Fatalf("PostgreSQL variant suite result = %#v", variantReport.Results)
	}

	coverageOut := runCLI(t, "case", "suite", "coverage", "--status", "active", "--json")
	var coverage struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
			NotRun int `json:"notRun"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(coverageOut), &coverage); err != nil {
		t.Fatalf("decode PostgreSQL suite coverage json: %v\n%s", err, coverageOut)
	}
	if !coverage.OK || coverage.Counts.Total != 2 || coverage.Counts.Passed != 2 || coverage.Counts.Failed != 0 || coverage.Counts.NotRun != 0 {
		t.Fatalf("PostgreSQL suite coverage = %#v", coverage)
	}

	priorityOut := runCLI(t,
		"case", "suite", "priority",
		"--signal", "Alpha",
		"--limit", "2",
		"--request-id", "pg-change-001",
		"--base-url", server.URL,
		"--json",
	)
	var priority struct {
		OK      bool     `json:"ok"`
		CaseIDs []string `json:"caseIds"`
		Counts  struct {
			Selected int `json:"selected"`
			Blocked  int `json:"blocked"`
		} `json:"counts"`
		BatchRequest struct {
			RequestID string   `json:"requestId"`
			CaseIDs   []string `json:"caseIds"`
			BaseURL   string   `json:"baseUrl"`
		} `json:"batchRequest"`
	}
	if err := json.Unmarshal([]byte(priorityOut), &priority); err != nil {
		t.Fatalf("decode PostgreSQL suite priority json: %v\n%s", err, priorityOut)
	}
	if !priority.OK || priority.Counts.Selected != 2 || priority.Counts.Blocked != 0 || priority.BatchRequest.RequestID != "pg-change-001" || priority.BatchRequest.BaseURL != server.URL {
		t.Fatalf("PostgreSQL suite priority = %#v", priority)
	}
	if strings.Join(priority.BatchRequest.CaseIDs, ",") != strings.Join(priority.CaseIDs, ",") || len(priority.CaseIDs) != 2 {
		t.Fatalf("PostgreSQL suite priority case ids = %#v batch=%#v", priority.CaseIDs, priority.BatchRequest.CaseIDs)
	}

	briefOut := runCLI(t, "case", "suite", "brief", "--signal", "Alpha", "--limit", "2", "--base-url", server.URL, "--json")
	var brief struct {
		OK     bool `json:"ok"`
		Counts struct {
			Ready            int `json:"ready"`
			Blocked          int `json:"blocked"`
			PrioritySelected int `json:"prioritySelected"`
		} `json:"counts"`
		Recommended []struct {
			CaseID string `json:"caseId"`
		} `json:"recommended"`
	}
	if err := json.Unmarshal([]byte(briefOut), &brief); err != nil {
		t.Fatalf("decode PostgreSQL suite brief json: %v\n%s", err, briefOut)
	}
	if !brief.OK || brief.Counts.Ready != 2 || brief.Counts.Blocked != 0 || brief.Counts.PrioritySelected != 2 || len(brief.Recommended) != 2 {
		t.Fatalf("PostgreSQL suite brief = %#v", brief)
	}
}

func TestCaseSuiteCoverageReportsLatestRunStatusByMaintenanceFilters(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-suite-coverage-pg")
	profileDir := writeCaseSuiteCoverageProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := time.Now().UTC()
	oldDefaultRunID := uniqueTestID(t, "run.default.old")
	latestDefaultRunID := uniqueTestID(t, "run.default.latest")
	latestVariantRunID := uniqueTestID(t, "run.variant.latest")
	recordCaseRunForCoverage(t, ctx, s, oldDefaultRunID, "case.default", store.StatusFailed, base.Add(-2*time.Minute))
	recordCaseRunForCoverage(t, ctx, s, latestDefaultRunID, "case.default", store.StatusPassed, base.Add(-time.Minute))
	recordCaseRunForCoverage(t, ctx, s, latestVariantRunID, "case.variant", store.StatusFailed, base)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "coverage",
		"--profile", profileDir,
		"--tag", "regression",
		"--status", "active",
		"--json",
	)

	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
			NotRun int `json:"notRun"`
		} `json:"counts"`
		Items []struct {
			CaseID       string `json:"caseId"`
			Title        string `json:"title"`
			NodeID       string `json:"nodeId"`
			LatestStatus string `json:"latestStatus"`
			LatestRunID  string `json:"latestRunId"`
			CaseRunID    string `json:"caseRunId"`
			DetailURL    string `json:"detailUrl"`
			HasPassed    bool   `json:"hasPassed"`
			Reason       string `json:"reason"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite coverage json: %v\n%s", err, out)
	}
	if report.OK || report.Counts.Total != 3 || report.Counts.Passed != 1 || report.Counts.Failed != 1 || report.Counts.NotRun != 1 {
		t.Fatalf("suite coverage report = %#v", report)
	}
	byCase := map[string]struct {
		LatestStatus string
		LatestRunID  string
		CaseRunID    string
		DetailURL    string
		HasPassed    bool
		Reason       string
	}{}
	for _, item := range report.Items {
		byCase[item.CaseID] = struct {
			LatestStatus string
			LatestRunID  string
			CaseRunID    string
			DetailURL    string
			HasPassed    bool
			Reason       string
		}{item.LatestStatus, item.LatestRunID, item.CaseRunID, item.DetailURL, item.HasPassed, item.Reason}
	}
	if byCase["case.default"].LatestStatus != store.StatusPassed || byCase["case.default"].LatestRunID != latestDefaultRunID || !byCase["case.default"].HasPassed {
		t.Fatalf("default coverage = %#v", byCase["case.default"])
	}
	if byCase["case.variant"].LatestStatus != store.StatusFailed || byCase["case.variant"].CaseRunID != latestVariantRunID+".case" || byCase["case.variant"].DetailURL == "" || byCase["case.variant"].HasPassed {
		t.Fatalf("variant coverage = %#v", byCase["case.variant"])
	}
	if byCase["case.unrun"].LatestStatus != "not-run" || byCase["case.unrun"].Reason != "no run recorded in Store" {
		t.Fatalf("unrun coverage = %#v", byCase["case.unrun"])
	}

	textOut := runCLI(t, "case", "suite", "coverage", "--profile", profileDir, "--tag", "regression")
	for _, want := range []string{"Case Suite Coverage", "Total: 3 Passed: 1 Failed: 1 Not Run: 1", "case.variant", latestVariantRunID + ".case"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("coverage text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteInspectReportsReadinessByMaintenanceFilters(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-suite-inspect-pg")
	profileDir := writeCaseSuiteCoverageProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := time.Now().UTC()
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.default.latest"), "case.default", store.StatusPassed, base.Add(-time.Minute))
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.variant.latest"), "case.variant", store.StatusFailed, base)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "inspect",
		"--profile", profileDir,
		"--tag", "regression",
		"--status", "active",
		"--json",
	)

	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total   int `json:"total"`
			Ready   int `json:"ready"`
			Blocked int `json:"blocked"`
			Failed  int `json:"failed"`
			NotRun  int `json:"notRun"`
		} `json:"counts"`
		Items []struct {
			CaseID             string   `json:"caseId"`
			Ready              bool     `json:"ready"`
			HasRunnableFile    bool     `json:"hasRunnableFile"`
			HasExecutionConfig bool     `json:"hasExecutionConfig"`
			LatestStatus       string   `json:"latestStatus"`
			Issues             []string `json:"issues"`
			SuggestedAction    string   `json:"suggestedAction"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite inspection json: %v\n%s", err, out)
	}
	if report.OK || report.Counts.Total != 3 || report.Counts.Ready != 2 || report.Counts.Blocked != 1 || report.Counts.Failed != 1 || report.Counts.NotRun != 1 {
		t.Fatalf("suite inspection report = %#v", report)
	}
	byCase := map[string]struct {
		Ready              bool
		HasRunnableFile    bool
		HasExecutionConfig bool
		LatestStatus       string
		Issues             []string
		SuggestedAction    string
	}{}
	for _, item := range report.Items {
		byCase[item.CaseID] = struct {
			Ready              bool
			HasRunnableFile    bool
			HasExecutionConfig bool
			LatestStatus       string
			Issues             []string
			SuggestedAction    string
		}{item.Ready, item.HasRunnableFile, item.HasExecutionConfig, item.LatestStatus, item.Issues, item.SuggestedAction}
	}
	if !byCase["case.default"].Ready || !byCase["case.default"].HasRunnableFile || byCase["case.default"].LatestStatus != store.StatusPassed {
		t.Fatalf("default inspection = %#v", byCase["case.default"])
	}
	if !byCase["case.variant"].Ready || !byCase["case.variant"].HasExecutionConfig || byCase["case.variant"].SuggestedAction != "rerun" {
		t.Fatalf("variant inspection = %#v", byCase["case.variant"])
	}
	if byCase["case.unrun"].Ready || byCase["case.unrun"].SuggestedAction != "add-runnable-source" || len(byCase["case.unrun"].Issues) == 0 {
		t.Fatalf("unrun inspection = %#v", byCase["case.unrun"])
	}

	textOut := runCLI(t, "case", "suite", "inspect", "--profile", profileDir, "--tag", "regression")
	for _, want := range []string{"Case Suite Inspection", "Total: 3 Ready: 2 Blocked: 1", "case.unrun", "add-runnable-source"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("inspection text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuitePlanBuildsExecutableBatchRequest(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-suite-plan-pg")
	profileDir := writeCaseSuiteCoverageProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := time.Now().UTC()
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.default.latest"), "case.default", store.StatusPassed, base.Add(-time.Minute))
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.variant.latest"), "case.variant", store.StatusFailed, base)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "plan",
		"--profile", profileDir,
		"--tag", "regression",
		"--status", "active",
		"--action", "run",
		"--action", "rerun",
		"--request-id", "change-001",
		"--base-url", "http://127.0.0.1:8080",
		"--evidence-dir", ".runtime/evidence",
		"--timeout-seconds", "7",
		"--json",
	)

	var report struct {
		OK      bool     `json:"ok"`
		CaseIDs []string `json:"caseIds"`
		Counts  struct {
			Total    int `json:"total"`
			Ready    int `json:"ready"`
			Blocked  int `json:"blocked"`
			Selected int `json:"selected"`
			Skipped  int `json:"skipped"`
		} `json:"counts"`
		BatchRequest struct {
			RequestID      string   `json:"requestId"`
			CaseIDs        []string `json:"caseIds"`
			BaseURL        string   `json:"baseUrl"`
			EvidenceDir    string   `json:"evidenceDir"`
			TimeoutSeconds int      `json:"timeoutSeconds"`
		} `json:"batchRequest"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite plan json: %v\n%s", err, out)
	}
	if !report.OK || strings.Join(report.CaseIDs, ",") != "case.variant" || report.Counts.Total != 3 || report.Counts.Ready != 2 || report.Counts.Blocked != 1 || report.Counts.Selected != 1 || report.Counts.Skipped != 1 {
		t.Fatalf("suite plan report = %#v", report)
	}
	if report.BatchRequest.RequestID != "change-001" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.variant" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" || report.BatchRequest.EvidenceDir != ".runtime/evidence" || report.BatchRequest.TimeoutSeconds != 7 {
		t.Fatalf("batch request = %#v", report.BatchRequest)
	}

	textOut := runCLI(t, "case", "suite", "plan", "--profile", profileDir, "--tag", "regression", "--action", "rerun")
	for _, want := range []string{"Case Suite Plan", "Selected: 1", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("plan text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteStabilityReportsTransitions(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-suite-stability-pg")
	profileDir := writeCaseSuiteCoverageProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := time.Now().UTC()
	variantRun1ID := uniqueTestID(t, "run.variant.1")
	variantRun2ID := uniqueTestID(t, "run.variant.2")
	variantRun3ID := uniqueTestID(t, "run.variant.3")
	recordCaseRunForCoverage(t, ctx, s, variantRun1ID, "case.variant", store.StatusPassed, base.Add(-3*time.Minute))
	recordCaseRunForCoverage(t, ctx, s, variantRun2ID, "case.variant", store.StatusFailed, base.Add(-2*time.Minute))
	recordCaseRunForCoverage(t, ctx, s, variantRun3ID, "case.variant", store.StatusPassed, base.Add(-time.Minute))
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.default.1"), "case.default", store.StatusPassed, base)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "stability",
		"--profile", profileDir,
		"--tag", "regression",
		"--status", "active",
		"--limit", "3",
		"--json",
	)
	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total    int `json:"total"`
			Stable   int `json:"stable"`
			Unstable int `json:"unstable"`
			NotRun   int `json:"notRun"`
		} `json:"counts"`
		Items []struct {
			CaseID       string `json:"caseId"`
			LatestStatus string `json:"latestStatus"`
			Transitions  int    `json:"transitions"`
			Unstable     bool   `json:"unstable"`
			Recent       []struct {
				RunID string `json:"runId"`
			} `json:"recent"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite stability json: %v\n%s", err, out)
	}
	if report.OK || report.Counts.Total != 3 || report.Counts.Unstable != 1 || report.Counts.Stable != 1 || report.Counts.NotRun != 1 {
		t.Fatalf("suite stability report = %#v", report)
	}
	byCase := map[string]struct {
		LatestStatus string
		Transitions  int
		Unstable     bool
		Recent       []struct {
			RunID string `json:"runId"`
		}
	}{}
	for _, item := range report.Items {
		byCase[item.CaseID] = struct {
			LatestStatus string
			Transitions  int
			Unstable     bool
			Recent       []struct {
				RunID string `json:"runId"`
			}
		}{item.LatestStatus, item.Transitions, item.Unstable, item.Recent}
	}
	if !byCase["case.variant"].Unstable || byCase["case.variant"].Transitions != 2 || byCase["case.variant"].LatestStatus != store.StatusPassed || byCase["case.variant"].Recent[0].RunID != variantRun3ID {
		t.Fatalf("variant stability = %#v", byCase["case.variant"])
	}

	textOut := runCLI(t, "case", "suite", "stability", "--profile", profileDir, "--tag", "regression", "--limit", "3")
	for _, want := range []string{"Case Suite Stability", "Unstable: 1", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("stability text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuitePriorityBuildsRankedBatchRequest(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-suite-priority-pg")
	profileDir := writeCaseSuiteCoverageProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := time.Now().UTC()
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.default.1"), "case.default", store.StatusPassed, base.Add(-2*time.Minute))
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.variant.1"), "case.variant", store.StatusPassed, base.Add(-time.Minute))
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.variant.2"), "case.variant", store.StatusFailed, base)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "priority",
		"--profile", profileDir,
		"--tag", "regression",
		"--status", "active",
		"--signal", "Variant",
		"--limit", "2",
		"--request-id", "change-011",
		"--base-url", "http://127.0.0.1:8080",
		"--json",
	)
	var report struct {
		OK      bool     `json:"ok"`
		CaseIDs []string `json:"caseIds"`
		Counts  struct {
			Total    int `json:"total"`
			Selected int `json:"selected"`
			Skipped  int `json:"skipped"`
			Blocked  int `json:"blocked"`
		} `json:"counts"`
		Selected []struct {
			CaseID  string   `json:"caseId"`
			Score   int      `json:"score"`
			Reasons []string `json:"reasons"`
		} `json:"selected"`
		BatchRequest struct {
			RequestID string   `json:"requestId"`
			CaseIDs   []string `json:"caseIds"`
			BaseURL   string   `json:"baseUrl"`
		} `json:"batchRequest"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite priority json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Total != 3 || report.Counts.Selected != 2 || report.Counts.Blocked != 1 || strings.Join(report.CaseIDs, ",") != "case.variant,case.default" {
		t.Fatalf("suite priority report = %#v", report)
	}
	if report.Selected[0].CaseID != "case.variant" || report.Selected[0].Score <= report.Selected[1].Score || len(report.Selected[0].Reasons) == 0 {
		t.Fatalf("suite priority selected = %#v", report.Selected)
	}
	if report.BatchRequest.RequestID != "change-011" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.variant,case.default" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("suite priority batch = %#v", report.BatchRequest)
	}

	textOut := runCLI(t, "case", "suite", "priority", "--profile", profileDir, "--tag", "regression", "--signal", "Variant", "--limit", "1")
	for _, want := range []string{"Case Suite Priority", "Selected: 1", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("priority text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteBriefSummarizesMaintainedSuiteForAgents(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-suite-brief-pg")
	profileDir := writeCaseSuiteCoverageProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := time.Now().UTC()
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.default.1"), "case.default", store.StatusPassed, base.Add(-2*time.Minute))
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.variant.1"), "case.variant", store.StatusPassed, base.Add(-time.Minute))
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.variant.2"), "case.variant", store.StatusFailed, base)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "brief",
		"--profile", profileDir,
		"--tag", "regression",
		"--status", "active",
		"--signal", "Variant",
		"--limit", "2",
		"--request-id", "change-012",
		"--base-url", "http://127.0.0.1:8080",
		"--json",
	)
	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total            int `json:"total"`
			Ready            int `json:"ready"`
			Blocked          int `json:"blocked"`
			Failed           int `json:"failed"`
			PrioritySelected int `json:"prioritySelected"`
		} `json:"counts"`
		Recommended []struct {
			CaseID string `json:"caseId"`
			Score  int    `json:"score"`
		} `json:"recommended"`
		BatchRequest struct {
			RequestID string   `json:"requestId"`
			CaseIDs   []string `json:"caseIds"`
			BaseURL   string   `json:"baseUrl"`
		} `json:"batchRequest"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite brief json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Total != 3 || report.Counts.Ready != 2 || report.Counts.Blocked != 1 || report.Counts.Failed != 1 || report.Counts.PrioritySelected != 2 {
		t.Fatalf("suite brief report = %#v", report)
	}
	if len(report.Recommended) != 2 || report.Recommended[0].CaseID != "case.variant" || report.Recommended[0].Score <= report.Recommended[1].Score {
		t.Fatalf("suite brief recommended = %#v", report.Recommended)
	}
	if report.BatchRequest.RequestID != "change-012" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.variant,case.default" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("suite brief batch = %#v", report.BatchRequest)
	}

	textOut := runCLI(t, "case", "suite", "brief", "--profile", profileDir, "--tag", "regression", "--signal", "Variant")
	for _, want := range []string{"Case Suite Brief", "Ready: 2", "Recommended: 2", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("brief text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteQualityAuditsMaintainedCaseMetadata(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-case-suite-quality-pg")
	profileDir := writeCaseSuiteQualityProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	out := runCLI(t,
		"case", "suite", "quality",
		"--profile", profileDir,
		"--status", "active",
		"--json",
	)
	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Nodes             int `json:"nodes"`
			NodesWithoutCases int `json:"nodesWithoutCases"`
			Cases             int `json:"cases"`
			CompleteCases     int `json:"completeCases"`
			IncompleteCases   int `json:"incompleteCases"`
			MissingOwner      int `json:"missingOwner"`
			MissingRunnable   int `json:"missingRunnable"`
			MissingExecution  int `json:"missingExecution"`
		} `json:"counts"`
		Cases []struct {
			CaseID   string   `json:"caseId"`
			Complete bool     `json:"complete"`
			Issues   []string `json:"issues"`
		} `json:"cases"`
		Nodes []struct {
			NodeID string   `json:"nodeId"`
			Issues []string `json:"issues"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite quality json: %v\n%s", err, out)
	}
	if report.OK || report.Counts.Nodes != 2 || report.Counts.NodesWithoutCases != 1 || report.Counts.Cases != 2 || report.Counts.CompleteCases != 1 || report.Counts.IncompleteCases != 1 {
		t.Fatalf("suite quality report = %#v", report)
	}
	if report.Counts.MissingOwner != 1 || report.Counts.MissingRunnable != 1 || report.Counts.MissingExecution != 1 {
		t.Fatalf("suite quality gaps = %#v", report.Counts)
	}
	if len(report.Nodes) != 1 || report.Nodes[0].NodeID != "node.empty" {
		t.Fatalf("suite quality nodes = %#v", report.Nodes)
	}
	textOut := runCLI(t, "case", "suite", "quality", "--profile", profileDir, "--status", "active")
	for _, want := range []string{"Case Suite Quality", "Incomplete: 1", "node.empty", "case.gaps"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("quality text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteQualityPlanSuggestsAuthoringActions(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-case-suite-quality-plan-pg")
	profileDir := writeCaseSuiteQualityProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	out := runCLI(t,
		"case", "suite", "quality-plan",
		"--profile", profileDir,
		"--status", "active",
		"--json",
	)
	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total            int `json:"total"`
			DraftCase        int `json:"draftCase"`
			CompleteMetadata int `json:"completeMetadata"`
			AddRunnable      int `json:"addRunnable"`
			AddExecution     int `json:"addExecution"`
		} `json:"counts"`
		Actions []struct {
			Type            string   `json:"type"`
			NodeID          string   `json:"nodeId"`
			CaseID          string   `json:"caseId"`
			SuggestedCaseID string   `json:"suggestedCaseId"`
			Fields          []string `json:"fields"`
			Command         []string `json:"command"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite quality plan json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Total != 4 || report.Counts.DraftCase != 1 || report.Counts.CompleteMetadata != 1 || report.Counts.AddRunnable != 1 || report.Counts.AddExecution != 1 {
		t.Fatalf("suite quality plan report = %#v", report)
	}
	if len(report.Actions) != 4 || report.Actions[0].Type != "draft-case" || report.Actions[0].NodeID != "node.empty" || report.Actions[0].SuggestedCaseID != "case.node-empty.default" {
		t.Fatalf("suite quality plan actions = %#v", report.Actions)
	}
	textOut := runCLI(t, "case", "suite", "quality-plan", "--profile", profileDir, "--status", "active")
	for _, want := range []string{"Case Suite Quality Plan", "Draft Case: 1", "case.node-empty.default", "case.gaps"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("quality plan text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteQualityReportWritesJSONAndHTML(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-case-suite-quality-report-pg")
	profileDir := writeCaseSuiteQualityProfile(t)
	outputDir := filepath.Join(t.TempDir(), "quality-report")
	runCLI(t, "config", "publish", "--from", profileDir)

	out := runCLI(t,
		"case", "suite", "quality-report",
		"--profile", profileDir,
		"--status", "active",
		"--output-dir", outputDir,
		"--json",
	)
	var report struct {
		OK            bool   `json:"ok"`
		ProfileID     string `json:"profileId"`
		ReportURL     string `json:"reportUrl"`
		JSONReportURL string `json:"jsonReportUrl"`
		QualityPlan   struct {
			Counts struct {
				Total            int `json:"total"`
				DraftCase        int `json:"draftCase"`
				CompleteMetadata int `json:"completeMetadata"`
				AddRunnable      int `json:"addRunnable"`
				AddExecution     int `json:"addExecution"`
			} `json:"counts"`
			Actions []struct {
				Type            string `json:"type"`
				CaseID          string `json:"caseId"`
				SuggestedCaseID string `json:"suggestedCaseId"`
			} `json:"actions"`
		} `json:"qualityPlan"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite quality report json: %v\n%s", err, out)
	}
	if !report.OK || report.ProfileID != "sample" || report.QualityPlan.Counts.Total != 4 || report.QualityPlan.Counts.DraftCase != 1 || report.QualityPlan.Counts.CompleteMetadata != 1 || report.QualityPlan.Counts.AddRunnable != 1 || report.QualityPlan.Counts.AddExecution != 1 {
		t.Fatalf("suite quality report = %#v", report)
	}
	if report.ReportURL != filepath.Join(outputDir, "report.html") || report.JSONReportURL != filepath.Join(outputDir, "report.json") {
		t.Fatalf("suite quality report paths = %#v", report)
	}
	jsonReportRaw, err := os.ReadFile(filepath.Join(outputDir, "report.json"))
	if err != nil {
		t.Fatalf("read quality json report: %v", err)
	}
	htmlReportRaw, err := os.ReadFile(filepath.Join(outputDir, "report.html"))
	if err != nil {
		t.Fatalf("read quality html report: %v", err)
	}
	jsonReport := string(jsonReportRaw)
	htmlReport := string(htmlReportRaw)
	for _, want := range []string{"Case Suite Quality Report", "case.node-empty.default", "case.gaps", "complete-case-metadata", "add-execution-config"} {
		if !strings.Contains(htmlReport, want) {
			t.Fatalf("quality html missing %q:\n%s", want, htmlReport)
		}
	}
	if !strings.Contains(jsonReport, `"qualityPlan"`) || !strings.Contains(jsonReport, `"case.node-empty.default"`) {
		t.Fatalf("quality json report missing expected content:\n%s", jsonReport)
	}

	textOut := runCLI(t, "case", "suite", "quality-report", "--profile", profileDir, "--status", "active", "--output-dir", filepath.Join(t.TempDir(), "text-quality-report"))
	for _, want := range []string{"Case Suite Quality Report", "Total Actions: 4", "Report:"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("quality report text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteImpactBuildsExecutableBatchRequest(t *testing.T) {
	ctx := context.Background()
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-suite-impact-pg")
	profileDir := writeCaseSuiteCoverageProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := time.Now().UTC()
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.default.latest"), "case.default", store.StatusPassed, base.Add(-time.Minute))
	recordCaseRunForCoverage(t, ctx, s, uniqueTestID(t, "run.variant.latest"), "case.variant", store.StatusFailed, base)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "impact",
		"--profile", profileDir,
		"--signal", "/alpha",
		"--status", "active",
		"--action", "run",
		"--action", "rerun",
		"--request-id", "change-002",
		"--base-url", "http://127.0.0.1:8080",
		"--json",
	)

	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Signals  int `json:"signals"`
			Nodes    int `json:"nodes"`
			Cases    int `json:"cases"`
			Selected int `json:"selected"`
			Blocked  int `json:"blocked"`
		} `json:"counts"`
		BatchRequest struct {
			RequestID string   `json:"requestId"`
			CaseIDs   []string `json:"caseIds"`
			BaseURL   string   `json:"baseUrl"`
		} `json:"batchRequest"`
		Cases []struct {
			CaseID  string   `json:"caseId"`
			Reasons []string `json:"reasons"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite impact json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Signals != 1 || report.Counts.Nodes != 1 || report.Counts.Cases != 3 || report.Counts.Selected != 1 || report.Counts.Blocked != 1 {
		t.Fatalf("suite impact report = %#v", report)
	}
	if report.BatchRequest.RequestID != "change-002" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.variant" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("impact batch request = %#v", report.BatchRequest)
	}
	if len(report.Cases) != 3 || len(report.Cases[0].Reasons) == 0 {
		t.Fatalf("impact cases = %#v", report.Cases)
	}

	textOut := runCLI(t, "case", "suite", "impact", "--profile", profileDir, "--signal", "/alpha", "--action", "rerun")
	for _, want := range []string{"Case Suite Impact", "Selected: 1", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("impact text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteImpactReportRunsImpactedCases(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-case-suite-impact-report-pg")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lookup" || r.URL.Query().Get("mode") != "ok" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
	}))
	defer server.Close()
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	outputDir := filepath.Join(t.TempDir(), "impact-report")
	out := runCLI(t,
		"case", "suite", "impact-report",
		"--profile", profileDir,
		"--signal", "/lookup",
		"--tag", "smoke",
		"--status", "active",
		"--action", "run",
		"--request-id", "change-003",
		"--base-url", server.URL,
		"--output-dir", outputDir,
		"--json",
	)

	var report struct {
		OK     bool `json:"ok"`
		Impact struct {
			BatchRequest struct {
				RequestID string   `json:"requestId"`
				CaseIDs   []string `json:"caseIds"`
			} `json:"batchRequest"`
		} `json:"impact"`
		Report struct {
			OK        bool   `json:"ok"`
			ReportURL string `json:"reportUrl"`
			Counts    struct {
				Total  int `json:"total"`
				Passed int `json:"passed"`
				Failed int `json:"failed"`
			} `json:"counts"`
			Results []struct {
				CaseID    string `json:"caseId"`
				CaseRunID string `json:"caseRunId"`
				Status    string `json:"status"`
			} `json:"results"`
		} `json:"report"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode impact report json: %v\n%s", err, out)
	}
	if !report.OK || report.Impact.BatchRequest.RequestID != "change-003" || strings.Join(report.Impact.BatchRequest.CaseIDs, ",") != "case.alpha.default" {
		t.Fatalf("impact report selection = %#v", report)
	}
	if !report.Report.OK || report.Report.Counts.Total != 1 || report.Report.Counts.Passed != 1 || report.Report.Counts.Failed != 0 || len(report.Report.Results) != 1 {
		t.Fatalf("impact execution report = %#v", report.Report)
	}
	if report.Report.Results[0].CaseID != "case.alpha.default" || report.Report.Results[0].CaseRunID == "" || report.Report.Results[0].Status != store.StatusPassed {
		t.Fatalf("impact execution item = %#v", report.Report.Results[0])
	}
	if _, err := os.Stat(filepath.Join(outputDir, "report.html")); err != nil {
		t.Fatalf("impact report html missing: %v", err)
	}
}

func TestWorkflowReportWritesReportWhenStepFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/first":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"item_id":"item-001"}`)
		case "/second":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"status":"failed"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	profileDir := writeWorkflowBatchReportProfile(t)
	configureNamedPostgreSQLActiveStore(t, "daily-workflow-report-fail-pg")
	runCLI(t, "config", "publish", "--from", profileDir)
	listOut := runCLI(t, "workflow", "discover", "--filter", "Workflow Alpha", "--json")
	var listReport struct {
		Items []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listOut), &listReport); err != nil {
		t.Fatalf("decode workflow discover json: %v\n%s", err, listOut)
	}
	if len(listReport.Items) != 1 || listReport.Items[0].ID != "workflow.alpha" {
		t.Fatalf("workflow discover = %#v", listReport.Items)
	}

	outputDir := filepath.Join(t.TempDir(), "workflow-report")
	out := runCLI(t,
		"workflow", "report",
		"--workflow", listReport.Items[0].ID,
		"--base-url", server.URL,
		"--output-dir", outputDir,
		"--json",
	)

	var report struct {
		OK        bool   `json:"ok"`
		RunID     string `json:"runId"`
		ReportURL string `json:"reportUrl"`
		Counts    struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"counts"`
		Steps []struct {
			RunID     string `json:"runId"`
			CaseRunID string `json:"caseRunId"`
			DetailURL string `json:"detailUrl"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode workflow report json: %v\n%s", err, out)
	}
	if report.OK || report.RunID == "" || report.Counts.Total != 2 || report.Counts.Passed != 1 || report.Counts.Failed != 1 {
		t.Fatalf("workflow report = %#v", report)
	}
	if len(report.Steps) != 2 || report.Steps[1].RunID == "" || report.Steps[1].CaseRunID != report.Steps[1].RunID+".case" || report.Steps[1].DetailURL == "" {
		t.Fatalf("workflow report evidence handles = %#v", report.Steps)
	}
	htmlPath := filepath.Join(outputDir, "report.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("html report missing: %v", err)
	}
	for _, want := range []string{"Workflow Alpha", "First Step", "Second Step", "failed", "caseRunId"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("workflow html missing %q:\n%s", want, html)
		}
	}
	if report.ReportURL != htmlPath {
		t.Fatalf("report url = %q want %q", report.ReportURL, htmlPath)
	}
}

func TestCaseIncompleteBatchesCommandReportsNotRunCases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	alphaPath := filepath.Join(dir, "case.alpha.json")
	betaPath := filepath.Join(dir, "case.beta.json")
	alphaCaseID := uniqueTestID(t, "case.alpha")
	betaCaseID := uniqueTestID(t, "case.beta")
	runID := uniqueTestID(t, "run-alpha")
	writeFile(t, alphaPath, fmt.Sprintf(`{
  "id": %q,
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
}`, alphaCaseID))
	writeFile(t, betaPath, fmt.Sprintf(`{
  "id": %q,
  "title": "Read Item",
  "request": {"method": "GET", "path": "/v1/items/item-001"},
  "assertions": {"expectedStatusCodes": [200]}
}`, betaCaseID))
	writeFile(t, filepath.Join(profileDir, "profile.json"), fmt.Sprintf(`{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [
    {"id":%q,"displayName":"Case Alpha","casePath":%q},
    {"id":%q,"displayName":"Case Beta","casePath":%q}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`, alphaCaseID, alphaPath, betaCaseID, betaPath))

	configureNamedPostgreSQLActiveStore(t, "daily-incomplete-batches-pg")
	runCLI(t, "case", "run", "--case", alphaPath, "--base-url", server.URL, "--run-id", runID, "--profile", "sample")

	out := runCLI(t, "case", "incomplete-batches", "--profile", profileDir)
	for _, want := range []string{"Incomplete API Cases: 1", betaCaseID, "not-run", betaPath} {
		if !strings.Contains(out, want) {
			t.Fatalf("incomplete case output missing %q: %q", want, out)
		}
	}

	jsonOut := runCLI(t, "case", "incomplete-batches", "--profile", profileDir, "--json")
	var report struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
		Items []struct {
			ID      string `json:"id"`
			Reason  string `json:"reason"`
			Command string `json:"suggestedCommand"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("decode incomplete cases report: %v\n%s", err, jsonOut)
	}
	if !report.OK || report.Count != 1 || len(report.Items) != 1 {
		t.Fatalf("incomplete cases report = %#v", report)
	}
	if report.Items[0].ID != betaCaseID || report.Items[0].Reason != "not-run" {
		t.Fatalf("incomplete case item = %#v", report.Items[0])
	}
	if !strings.Contains(report.Items[0].Command, betaPath) {
		t.Fatalf("suggested command = %q", report.Items[0].Command)
	}

	ctx := context.Background()
	storeOnlyPath := filepath.Join(dir, "store-only.sqlite")
	storeOnly, err := sqlite.Open(ctx, sqlite.Config{Path: storeOnlyPath})
	if err != nil {
		t.Fatalf("open store-only catalog: %v", err)
	}
	if err := storeOnly.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		APICases: []store.CatalogAPICase{
			{ID: "case.store.passed", DisplayName: "Passed Store Case", Status: "active"},
			{ID: "case.store.pending", DisplayName: "Pending Store Case", CasePath: betaPath, Status: "active"},
		},
	}); err != nil {
		t.Fatalf("seed store-only catalog: %v", err)
	}
	if _, err := storeOnly.CreateRun(ctx, store.Run{ID: "run.store.passed", ProfileID: "current", WorkflowID: "case.store.passed", Status: store.StatusPassed}); err != nil {
		t.Fatalf("create store-only run: %v", err)
	}
	if _, err := storeOnly.RecordAPICaseRun(ctx, store.APICaseRun{ID: "run.store.passed.case", RunID: "run.store.passed", CaseID: "case.store.passed", Status: store.StatusPassed}); err != nil {
		t.Fatalf("record store-only case run: %v", err)
	}
	if err := storeOnly.Close(); err != nil {
		t.Fatalf("close store-only catalog: %v", err)
	}

	storeOnlyOut := runCLI(t, "case", "incomplete-batches", "--store", "sqlite://"+storeOnlyPath, "--json")
	var storeOnlyReport struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
		Items []struct {
			ID      string `json:"id"`
			Reason  string `json:"reason"`
			Source  string `json:"source"`
			Command string `json:"suggestedCommand"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(storeOnlyOut), &storeOnlyReport); err != nil {
		t.Fatalf("decode store-only incomplete cases report: %v\n%s", err, storeOnlyOut)
	}
	if !storeOnlyReport.OK || storeOnlyReport.Count != 1 || len(storeOnlyReport.Items) != 1 {
		t.Fatalf("store-only incomplete report = %#v", storeOnlyReport)
	}
	if storeOnlyReport.Items[0].ID != "case.store.pending" || storeOnlyReport.Items[0].Reason != "not-run" || storeOnlyReport.Items[0].Source != "profile:current" {
		t.Fatalf("store-only incomplete item = %#v", storeOnlyReport.Items[0])
	}
	if !strings.Contains(storeOnlyReport.Items[0].Command, betaPath) {
		t.Fatalf("store-only suggested command = %q", storeOnlyReport.Items[0].Command)
	}
}

func TestServeHandlerUsesConfiguredStore(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "empty",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  `{"steps":[{"stepId":"step.alpha","ok":true}]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	handler, cleanup, err := serveHandlerFromArgs([]string{
		"--store", "sqlite://" + storePath,
	})
	if err != nil {
		t.Fatalf("build serve handler: %v", err)
	}
	defer cleanup()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "run.alpha") {
		t.Fatalf("serve handler did not use configured store: %s", rec.Body.String())
	}
}

func TestServeHandlerRequiresActiveStore(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("OTSANDBOX_CONFIG_HOME", configHome)
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
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.store.flag",
		ProfileID:    "empty",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.store.flag",
		SummaryJSON:  `{"steps":[{"stepId":"step.alpha","ok":true}]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	handler, cleanup, err := serveHandlerFromArgs([]string{
		"--store", "sqlite://" + storePath,
	})
	if err != nil {
		t.Fatalf("build serve handler: %v", err)
	}
	defer cleanup()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "run.store.flag") {
		t.Fatalf("serve handler did not use --store: %s", rec.Body.String())
	}

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

	handler, cleanup, err := serveHandlerFromArgs([]string{"--store", "sqlite://" + storePath})
	if err != nil {
		t.Fatalf("build serve handler from store catalog: %v", err)
	}
	defer cleanup()

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

func TestServeAndEvidenceTasksUseNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-serve-pg")
	runID := "run.tasks.pg." + time.Now().UTC().Format("20060102150405.000000000")
	ctx := context.Background()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open PostgreSQL task store: %v", err)
	}
	seedPostProcessTaskFixture(t, ctx, runtime, runID, runID+".")
	if err := runtime.Close(); err != nil {
		t.Fatalf("close PostgreSQL task store: %v", err)
	}

	profileDir := writeInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	listOut := runCLI(t, "evidence", "list", "--run", runID, "--json")
	var evidenceReport struct {
		Runs []struct {
			ID            string `json:"id"`
			EvidenceCount int    `json:"evidenceCount"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(listOut), &evidenceReport); err != nil {
		t.Fatalf("decode PostgreSQL evidence list json: %v\n%s", err, listOut)
	}
	if len(evidenceReport.Runs) != 1 || evidenceReport.Runs[0].ID != runID || evidenceReport.Runs[0].EvidenceCount != 1 {
		t.Fatalf("PostgreSQL evidence list report = %#v", evidenceReport.Runs)
	}

	tasksOut := runCLI(t,
		"evidence", "tasks",
		"--run", runID,
		"--step", "step-a",
		"--kind", "trace_topology_collect",
		"--json",
	)
	var tasksReport struct {
		RunID  string `json:"runId"`
		Counts struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
		} `json:"counts"`
		Tasks []struct {
			ID            string `json:"id"`
			StepID        string `json:"stepId"`
			DisplayStatus string `json:"displayStatus"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(tasksOut), &tasksReport); err != nil {
		t.Fatalf("decode PostgreSQL evidence tasks json: %v\n%s", err, tasksOut)
	}
	if tasksReport.RunID != runID || tasksReport.Counts.Total != 1 || tasksReport.Counts.Passed != 1 || len(tasksReport.Tasks) != 1 {
		t.Fatalf("PostgreSQL evidence tasks report = %#v", tasksReport)
	}
	if !strings.Contains(tasksReport.Tasks[0].ID, "task.trace") || tasksReport.Tasks[0].StepID != "step-a" || tasksReport.Tasks[0].DisplayStatus != "passed: completed" {
		t.Fatalf("PostgreSQL evidence task = %#v", tasksReport.Tasks[0])
	}

	handler, cleanup, err := serveHandlerFromArgs(nil)
	if err != nil {
		t.Fatalf("build serve handler from active PostgreSQL Store: %v", err)
	}
	defer cleanup()

	current := httptest.NewRecorder()
	handler.ServeHTTP(current, httptest.NewRequest(http.MethodGet, "/api/store/current", nil))
	if current.Code != http.StatusOK {
		t.Fatalf("store current status = %d body=%s", current.Code, current.Body.String())
	}
	var storePayload struct {
		OK         bool   `json:"ok"`
		Configured bool   `json:"configured"`
		Name       string `json:"name"`
		Backend    string `json:"backend"`
		Source     string `json:"source"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &storePayload); err != nil {
		t.Fatalf("decode PostgreSQL store current payload: %v\n%s", err, current.Body.String())
	}
	if !storePayload.OK || !storePayload.Configured || storePayload.Name != "daily-serve-pg" || storePayload.Backend != "postgres" || storePayload.Source != "active-config" {
		t.Fatalf("PostgreSQL store current payload = %#v", storePayload)
	}

	runs := httptest.NewRecorder()
	handler.ServeHTTP(runs, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if runs.Code != http.StatusOK || !strings.Contains(runs.Body.String(), runID) {
		t.Fatalf("serve runs via active PostgreSQL Store = %d %s", runs.Code, runs.Body.String())
	}

	nodes := httptest.NewRecorder()
	handler.ServeHTTP(nodes, httptest.NewRequest(http.MethodGet, "/api/interface-nodes", nil))
	if nodes.Code != http.StatusOK {
		t.Fatalf("interface nodes status = %d body=%s", nodes.Code, nodes.Body.String())
	}
	var nodesPayload struct {
		Source struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"source"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(nodes.Body.Bytes(), &nodesPayload); err != nil {
		t.Fatalf("decode PostgreSQL interface nodes payload: %v\n%s", err, nodes.Body.String())
	}
	if nodesPayload.Source.ID != "sample" || nodesPayload.Source.Kind != "store" || len(nodesPayload.Items) != 1 || nodesPayload.Items[0].ID != "node.alpha" {
		t.Fatalf("PostgreSQL serve catalog payload = %#v", nodesPayload)
	}

	apiImportDir := writeEmptyProfileBundle(t)
	importRec := httptest.NewRecorder()
	handler.ServeHTTP(importRec, httptest.NewRequest(http.MethodPost, "/api/profile/import", strings.NewReader(`{"path":`+mustJSON(t, apiImportDir)+`}`)))
	if importRec.Code != http.StatusOK {
		t.Fatalf("profile import status = %d body=%s", importRec.Code, importRec.Body.String())
	}
	var importPayload struct {
		ProfileID  string   `json:"profileId"`
		BundlePath string   `json:"bundlePath"`
		ReadModels []string `json:"readModels"`
	}
	if err := json.Unmarshal(importRec.Body.Bytes(), &importPayload); err != nil {
		t.Fatalf("decode PostgreSQL serve profile import payload: %v\n%s", err, importRec.Body.String())
	}
	if importPayload.ProfileID != "empty" || importPayload.BundlePath != apiImportDir || strings.Join(importPayload.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("PostgreSQL serve profile import payload = %#v", importPayload)
	}

	apiVerifyDir := writeInterfaceNodeCaseProfile(t)
	verifyRec := httptest.NewRecorder()
	handler.ServeHTTP(verifyRec, httptest.NewRequest(http.MethodPost, "/api/profile/verify", strings.NewReader(`{"path":`+mustJSON(t, apiVerifyDir)+`}`)))
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("profile verify status = %d body=%s", verifyRec.Code, verifyRec.Body.String())
	}
	var verifyPayload struct {
		OK        bool   `json:"ok"`
		ProfileID string `json:"profileId"`
		Publish   struct {
			ProfileID  string   `json:"profileId"`
			BundlePath string   `json:"bundlePath"`
			ReadModels []string `json:"readModels"`
		} `json:"publish"`
		Summary struct {
			FailedChecks int `json:"failedChecks"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(verifyRec.Body.Bytes(), &verifyPayload); err != nil {
		t.Fatalf("decode PostgreSQL serve profile verify payload: %v\n%s", err, verifyRec.Body.String())
	}
	if !verifyPayload.OK || verifyPayload.ProfileID != "sample" || verifyPayload.Publish.ProfileID != "sample" || verifyPayload.Publish.BundlePath != apiVerifyDir || strings.Join(verifyPayload.Publish.ReadModels, ",") != "interface-nodes,catalog,dashboard" || verifyPayload.Summary.FailedChecks != 0 {
		t.Fatalf("PostgreSQL serve profile verify payload = %#v", verifyPayload)
	}

	runtime, err = openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("reopen PostgreSQL serve profile Store: %v", err)
	}
	defer runtime.Close()
	verifiedIndex, err := runtime.GetProfileIndex(ctx, "sample")
	if err != nil {
		t.Fatalf("get PostgreSQL serve profile index: %v", err)
	}
	if verifiedIndex.BundlePath != apiVerifyDir || !strings.HasPrefix(verifiedIndex.BundleDigest, "sha256:") {
		t.Fatalf("PostgreSQL serve profile index = %#v", verifiedIndex)
	}
	verifiedCatalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get PostgreSQL serve profile catalog: %v", err)
	}
	if verifiedCatalog.ProfileID != "sample" || len(verifiedCatalog.APICases) != 2 {
		t.Fatalf("PostgreSQL serve profile catalog = %#v", verifiedCatalog)
	}

	apiLegacyPath := filepath.Join(t.TempDir(), "legacy-api.sqlite")
	apiLegacySuffix := time.Now().UTC().UnixNano()
	apiLegacyWorkflowID := apiLegacySuffix
	apiLegacyCaseID := apiLegacySuffix + 1
	apiLegacyParentRunID := fmt.Sprintf("case-run-parent-api-pg-%d", apiLegacySuffix)
	createLegacyRuntimeDBWithIDs(t, apiLegacyPath, apiLegacyWorkflowID, apiLegacyCaseID, apiLegacyParentRunID)
	importEvidenceRec := httptest.NewRecorder()
	handler.ServeHTTP(importEvidenceRec, httptest.NewRequest(http.MethodPost, "/api/evidence/import", strings.NewReader(`{"sourcePath":`+mustJSON(t, apiLegacyPath)+`,"profileId":"sample"}`)))
	if importEvidenceRec.Code != http.StatusOK {
		t.Fatalf("evidence import status = %d body=%s", importEvidenceRec.Code, importEvidenceRec.Body.String())
	}
	var importEvidencePayload struct {
		OK              bool   `json:"ok"`
		SourcePath      string `json:"sourcePath"`
		ProfileID       string `json:"profileId"`
		RunCount        int    `json:"runCount"`
		APICaseRunCount int    `json:"apiCaseRunCount"`
		EvidenceCount   int    `json:"evidenceCount"`
	}
	if err := json.Unmarshal(importEvidenceRec.Body.Bytes(), &importEvidencePayload); err != nil {
		t.Fatalf("decode PostgreSQL serve evidence import payload: %v\n%s", err, importEvidenceRec.Body.String())
	}
	if !importEvidencePayload.OK || importEvidencePayload.SourcePath != apiLegacyPath || importEvidencePayload.ProfileID != "sample" || importEvidencePayload.RunCount != 2 || importEvidencePayload.APICaseRunCount != 1 || importEvidencePayload.EvidenceCount != 1 {
		t.Fatalf("PostgreSQL serve evidence import payload = %#v", importEvidencePayload)
	}
	evidenceListRec := httptest.NewRecorder()
	handler.ServeHTTP(evidenceListRec, httptest.NewRequest(http.MethodGet, "/api/evidence/list?run="+apiLegacyParentRunID, nil))
	if evidenceListRec.Code != http.StatusOK {
		t.Fatalf("evidence list status = %d body=%s", evidenceListRec.Code, evidenceListRec.Body.String())
	}
	var importedEvidencePayload struct {
		Runs []struct {
			ID              string `json:"id"`
			APICaseRunCount int    `json:"apiCaseRunCount"`
			EvidenceCount   int    `json:"evidenceCount"`
			EvidenceRecords []struct {
				ID        string `json:"id"`
				CaseRunID string `json:"caseRunId"`
				Kind      string `json:"kind"`
				URI       string `json:"uri"`
			} `json:"evidenceRecords"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(evidenceListRec.Body.Bytes(), &importedEvidencePayload); err != nil {
		t.Fatalf("decode PostgreSQL serve evidence list payload: %v\n%s", err, evidenceListRec.Body.String())
	}
	if len(importedEvidencePayload.Runs) != 1 || importedEvidencePayload.Runs[0].ID != apiLegacyParentRunID || importedEvidencePayload.Runs[0].APICaseRunCount != 1 || importedEvidencePayload.Runs[0].EvidenceCount != 1 || len(importedEvidencePayload.Runs[0].EvidenceRecords) != 1 {
		t.Fatalf("PostgreSQL serve evidence list payload = %#v", importedEvidencePayload.Runs)
	}
	importedRecord := importedEvidencePayload.Runs[0].EvidenceRecords[0]
	if importedRecord.ID != fmt.Sprintf("legacy-evidence-%d", apiLegacyCaseID) || importedRecord.CaseRunID != fmt.Sprintf("legacy-case-run-%d", apiLegacyCaseID) || importedRecord.Kind != "case-run" || importedRecord.URI != ".runtime/cases/"+apiLegacyParentRunID {
		t.Fatalf("PostgreSQL serve evidence list record = %#v", importedRecord)
	}
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(raw)
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

func configureNamedPostgreSQLActiveStore(t *testing.T, name string) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("OTSANDBOX_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set OTSANDBOX_TEST_PG_DSN to run named PostgreSQL daily path coverage")
	}
	t.Setenv("OTSANDBOX_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	runCLI(t, "store", "config", "set", name, "--url", dsn)
	runCLI(t, "store", "use", name)
	runCLI(t, "store", "upgrade")
	return dsn
}

func uniqueTestID(t *testing.T, prefix string) string {
	t.Helper()
	slug := strings.ToLower(t.Name())
	slug = strings.NewReplacer("/", "-", "_", "-", " ", "-").Replace(slug)
	return fmt.Sprintf("%s.%s.%d", prefix, slug, time.Now().UTC().UnixNano())
}

func seedEnvironmentVerificationArtifacts(t *testing.T, storeRef string, runID string) {
	t.Helper()
	ctx := context.Background()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open verification artifact Store: %v", err)
	}
	defer runtime.Close()
	now := time.Now().UTC()
	if _, err := runtime.CreateRun(ctx, store.Run{
		ID:         runID,
		ProfileID:  "sample",
		WorkflowID: "workflow.core-10",
		Status:     store.StatusPassed,
		SummaryJSON: `{"acceptance":{"templateId":"environment.workflow.skywalking.v1","ok":true,"workflowId":"workflow.core-10",
"expectedSteps":1,"completedSteps":1,"passedSteps":1,"failedSteps":0,"topologyProvider":"skywalking",
"steps":[{"stepId":"step.core-10","caseId":"case.core-10","status":"passed","elapsedMs":12,"evidenceComplete":true,"topologyComplete":true}]}}`,
		StartedAt:  now.Add(-time.Second),
		FinishedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed verification run: %v", err)
	}
	if _, err := runtime.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         runID + ".summary",
		RunID:      runID,
		Kind:       "summary",
		URI:        "store://verification/" + runID + "/summary.json",
		MediaType:  "application/json",
		SHA256:     "verification-summary-sha256",
		SizeBytes:  2,
		Summary:    `{"status":"passed"}`,
		Category:   "verification",
		Visibility: "internal",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("seed verification Evidence: %v", err)
	}
	if _, err := runtime.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            runID + ".topology.skywalking",
		WorkflowRunID: runID,
		WorkflowID:    "workflow.core-10",
		StepID:        "step.core-10",
		CaseID:        "case.core-10",
		RequestID:     "request.core-10",
		TraceID:       "trace.core-10",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","traceId":"trace.core-10","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("seed verification topology: %v", err)
	}
}

func createBareGitRepo(t *testing.T, branch string) string {
	return createBareGitRepoWithFiles(t, branch, map[string]string{
		"README.md": "# restore fixture\n",
	})
}

func createBareGitRepoWithFiles(t *testing.T, branch string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	work := filepath.Join(dir, "work")
	runGit(t, "", "init", "--bare", remote)
	runGit(t, "", "init", "-b", branch, work)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writeFile(t, filepath.Join(work, name), files[name])
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "-c", "user.name=Open Test", "-c", "user.email=open-test@example.com", "commit", "-m", "initial")
	runGit(t, work, "remote", "add", "origin", remote)
	runGit(t, work, "push", "origin", branch)
	return remote
}

func runGit(t *testing.T, workdir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if strings.TrimSpace(workdir) != "" {
		cmd.Dir = workdir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func fakeDockerCommand(t *testing.T) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	callsPath := filepath.Join(dir, "docker-calls.txt")
	dockerPath := filepath.Join(dir, "docker")
	writeFile(t, dockerPath, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOCKER_CALLS_FILE\"\nif [ \"$1\" = \"compose\" ]; then\n  prev=\"\"\n  service=\"\"\n  for arg in \"$@\"; do\n    if [ \"$prev\" = \"--format\" ] && [ \"$arg\" = \"json\" ]; then\n      service=\"__next__\"\n    elif [ \"$service\" = \"__next__\" ]; then\n      service=\"$arg\"\n    fi\n    prev=\"$arg\"\n  done\n  if [ -n \"$service\" ] && [ \"$service\" != \"__next__\" ]; then\n    printf '{\"Name\":\"%s\",\"Service\":\"%s\",\"State\":\"running\",\"Health\":\"healthy\"}\\n' \"$service\" \"$service\"\n  fi\nfi\n")
	if err := os.Chmod(dockerPath, 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}
	return []string{
		"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"DOCKER_CALLS_FILE=" + callsPath,
	}, callsPath
}

func runCLIWithEnv(t *testing.T, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func runCLIFailsWithEnv(t *testing.T, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("go run . %s unexpectedly succeeded:\n%s", strings.Join(args, " "), out)
	}
	return string(out)
}

func runStoreCommand(t *testing.T, args ...string) string {
	t.Helper()
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = writePipe
	runErr := runStore(context.Background(), args)
	if closeErr := writePipe.Close(); closeErr != nil {
		t.Fatalf("close stdout pipe: %v", closeErr)
	}
	os.Stdout = originalStdout
	out, readErr := io.ReadAll(readPipe)
	if readErr != nil {
		t.Fatalf("read stdout pipe: %v", readErr)
	}
	if runErr != nil {
		t.Fatalf("store %s failed: %v\n%s", strings.Join(args, " "), runErr, out)
	}
	return string(out)
}

func withPostgresSchemaStatus(t *testing.T, fn func(context.Context, postgres.Config) (postgres.SchemaStatusResult, error)) {
	t.Helper()
	original := postgresSchemaStatus
	postgresSchemaStatus = fn
	t.Cleanup(func() {
		postgresSchemaStatus = original
	})
}

func sqliteScalar(t *testing.T, dbPath string, statement string) string {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, statement).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite scalar failed: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func hasProfileVerifyCheck(checks []struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
}, name string) bool {
	for _, check := range checks {
		if check.Name == name && check.OK {
			return true
		}
	}
	return false
}

func runCLIFails(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("go run . %s unexpectedly succeeded:\n%s", strings.Join(args, " "), out)
	}
	return string(out)
}

func readTarGZEntries(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive %s: %v", path, err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("open gzip %s: %v", path, err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var entries []string
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read archive %s: %v", path, err)
		}
		entries = append(entries, header.Name)
	}
	return entries
}

func writeTarGZEntries(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive %s: %v", path, err)
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	defer gz.Close()
	writer := tar.NewWriter(gz)
	defer writer.Close()
	for name, body := range entries {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(body)),
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("write archive header %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatalf("write archive entry %s: %v", name, err)
		}
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func createStoredCaseRun(t *testing.T, runID string) {
	t.Helper()
	configureNamedPostgreSQLActiveStore(t, "daily-evidence-list-pg")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	evidenceDir := filepath.Join(dir, "evidence")

	runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", runID, "--evidence-dir", evidenceDir, "--profile", "sample")
}

func createPostProcessTaskStore(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open post process task store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close post process task store: %v", err)
		}
	})
	seedPostProcessTaskFixture(t, ctx, s, "run.tasks", "")
	return storePath
}

func seedPostProcessTaskFixture(t *testing.T, ctx context.Context, s store.Store, runID string, idPrefix string) {
	t.Helper()
	base := time.Date(2026, 5, 17, 1, 2, 3, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         runID,
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  base,
		FinishedAt: base.Add(time.Second),
		CreatedAt:  base,
		UpdatedAt:  base.Add(time.Second),
	}); err != nil {
		t.Fatalf("create task run: %v", err)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         idPrefix + "evidence.response",
		RunID:      runID,
		CaseRunID:  runID + ".case",
		StepID:     "step-a",
		Kind:       "response",
		URI:        "store://evidence/" + runID + "/response.json",
		MediaType:  "application/json",
		SHA256:     "response-sha256",
		SizeBytes:  2,
		Summary:    `{"statusCode":200}`,
		Category:   "http",
		Visibility: "internal",
		CreatedAt:  base.Add(5 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record task Evidence: %v", err)
	}
	records := []store.PostProcessTask{
		{
			ID:         idPrefix + "task.trace",
			RunID:      runID,
			WorkflowID: "workflow.alpha",
			StepID:     "step-a",
			CaseID:     "case.alpha",
			Kind:       "trace_topology_collect",
			Status:     store.StatusPassed,
			StartedAt:  base.Add(10 * time.Millisecond),
			FinishedAt: base.Add(135 * time.Millisecond),
			CreatedAt:  base.Add(10 * time.Millisecond),
		},
		{
			ID:          idPrefix + "task.logs",
			RunID:       runID,
			WorkflowID:  "workflow.alpha",
			StepID:      "step-b",
			CaseID:      "case.beta",
			Kind:        "runtime_log_collect",
			Status:      store.StatusFailed,
			StartedAt:   base.Add(200 * time.Millisecond),
			FinishedAt:  base.Add(500 * time.Millisecond),
			Error:       "log source missing",
			SummaryJSON: `{"source":"runtime-log"}`,
			CreatedAt:   base.Add(200 * time.Millisecond),
		},
		{
			ID:          idPrefix + "task.trace.skip",
			RunID:       runID,
			WorkflowID:  "workflow.alpha",
			StepID:      "step-c",
			CaseID:      "case.gamma",
			Kind:        "trace_topology_collect",
			Status:      store.StatusSkipped,
			StartedAt:   base.Add(600 * time.Millisecond),
			FinishedAt:  base.Add(600 * time.Millisecond),
			SummaryJSON: `{"reason":"SkyWalking provider unavailable"}`,
			CreatedAt:   base.Add(600 * time.Millisecond),
		},
	}
	for _, record := range records {
		if _, err := s.RecordPostProcessTask(ctx, record); err != nil {
			t.Fatalf("record post process task %s: %v", record.ID, err)
		}
	}
}

func writeAPICaseFile(t *testing.T, path string) {
	t.Helper()
	raw := []byte(`{
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
}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write api case: %v", err)
	}
}

func writeEmptyProfileBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "empty",
  "displayName": "Empty Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	return dir
}

func writeWorkflowProfile(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "workflows", "workflow.json"), `{"id":"workflow.alpha","displayName":"Workflow Alpha"}`)
	writeFile(t, filepath.Join(dir, "interface-nodes", "node.json"), `{"id":"node.alpha","displayName":"Node Alpha"}`)
	writeFile(t, filepath.Join(dir, "cases", "case.json"), `{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}`)
	writeFile(t, filepath.Join(dir, "workflow-bindings", "binding.json"), `{"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.alpha","required":true}`)
}

func writeTemplateProfile(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "request-templates", "template.json"), `{
  "id": "template.create",
  "method": "POST",
  "path": "/v1/items/{{.itemId}}",
  "templateJson": "{\"id\":\"{{.itemId}}\",\"quantity\":{{.quantity}}}"
}`)
	writeFile(t, filepath.Join(dir, "fixtures", "fixture.json"), `{
  "id": "fixture.item",
  "kind": "json",
  "dataJson": "{\"itemId\":\"item-001\",\"quantity\":3}"
}`)
}

func writeInterfaceNodeCaseProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.alpha"}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "schemaVersion": "1",
  "templateConfigs": [
    {
      "id": "cfg.case.alpha",
      "templateId": "case-execution",
      "nodeId": "node.alpha",
      "scopeType": "case",
      "scopeId": "case.alpha",
      "title": "Case Alpha execution",
      "status": "active",
      "sortOrder": 1,
      "configJson": "{\"caseId\":\"case.alpha\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"service.alpha\",\"path\":\"/alpha\",\"expectedHttpCodes\":[200]}}"
    }
  ]
}`)
	return dir
}

func writeInterfaceNodeCoverageProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha","serviceId":"service.alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.alpha","nodeId":"node.alpha","caseId":"case.alpha","required":true}],
  "fixtures": []
}`)
	return dir
}

func writeInterfaceNodeBatchReportProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Result Lookup","serviceId":"service.alpha","operation":"Result Lookup","method":"GET","path":"/lookup"}],
  "apiCases": [
    {"id":"case.alpha.default","displayName":"Case Alpha Default","nodeId":"node.alpha","payloadTemplateJson":"{\"mode\":\"ok\"}","expectedJson":"{\"expectedHttpCodes\":[200]}","sortOrder":1,"tags":["smoke","regression"],"priority":"p0","owner":"team-a","description":"Default maintained smoke case."},
    {"id":"case.alpha.variant","displayName":"Case Alpha Variant","nodeId":"node.alpha","payloadTemplateJson":"{\"mode\":\"bad\"}","expectedJson":"{\"expectedHttpCodes\":[400]}","sortOrder":2,"tags":["negative"],"priority":"p1","owner":"team-b","description":"Negative maintained variant."}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "schemaVersion": "1",
  "templateConfigs": [
    {
      "id": "cfg.case.alpha.default",
      "templateId": "case-execution",
      "nodeId": "node.alpha",
      "scopeType": "case",
      "scopeId": "case.alpha.default",
      "title": "Case Alpha Default execution",
      "status": "active",
      "sortOrder": 1,
      "configJson": "{\"caseId\":\"case.alpha.default\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"service.alpha\",\"path\":\"/lookup\",\"query\":{\"mode\":\"ok\"},\"expectedHttpCodes\":[200]}}"
    }
  ]
}`)
	return dir
}

func writeCaseSuiteCoverageProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha","serviceId":"service.alpha","operation":"Alpha","method":"GET","path":"/alpha"}],
  "apiCases": [
    {"id":"case.default","displayName":"Default Case","nodeId":"node.alpha","sortOrder":1,"tags":["regression","smoke"],"priority":"p0","owner":"team-a","description":"Default maintained case.","casePath":"cases/default.json"},
    {"id":"case.variant","displayName":"Variant Case","nodeId":"node.alpha","sortOrder":2,"tags":["regression"],"priority":"p1","owner":"team-a","description":"Variant maintained case."},
    {"id":"case.unrun","displayName":"Unrun Case","nodeId":"node.alpha","sortOrder":3,"tags":["regression"],"priority":"p2","owner":"team-b","description":"Unrun maintained case."}
  ],
  "requestTemplates": [],
  "templateConfigs": [
    {
      "id": "config.case.variant",
      "scopeType": "case",
      "scopeId": "case.variant",
      "status": "active",
      "configJson": "{\"caseId\":\"case.variant\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"node.alpha\",\"path\":\"/alpha\",\"expectedHttpCodes\":[200]}}"
    }
  ],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	return dir
}

func writeCaseSuiteQualityProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [],
  "interfaceNodes": [
    {"id":"node.alpha","displayName":"Node Alpha","serviceId":"service.alpha","operation":"Alpha","method":"GET","path":"/alpha"},
    {"id":"node.empty","displayName":"Node Empty","serviceId":"service.alpha","operation":"Empty","method":"GET","path":"/empty"}
  ],
  "apiCases": [
    {"id":"case.complete","displayName":"Complete Case","description":"Ready maintained case.","nodeId":"node.alpha","sortOrder":1,"tags":["regression"],"priority":"p0","owner":"team-a","casePath":"cases/complete.json"},
    {"id":"case.gaps","displayName":"Gap Case","nodeId":"node.alpha","sortOrder":2}
  ],
  "requestTemplates": [],
  "templateConfigs": [
    {
      "id": "config.case.complete",
      "scopeType": "case",
      "scopeId": "case.complete",
      "status": "active",
      "configJson": "{\"caseId\":\"case.complete\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"node.alpha\",\"path\":\"/alpha\",\"expectedHttpCodes\":[200]}}"
    }
  ],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	return dir
}

func recordCaseRunForCoverage(t *testing.T, ctx context.Context, s store.Store, runID string, caseID string, status string, at time.Time) {
	t.Helper()
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         runID,
		ProfileID:  "sample",
		WorkflowID: caseID,
		Status:     status,
		StartedAt:  at,
		FinishedAt: at.Add(time.Second),
		CreatedAt:  at,
		UpdatedAt:  at.Add(time.Second),
	}); err != nil {
		t.Fatalf("create coverage run %s: %v", runID, err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         runID + ".case",
		RunID:      runID,
		CaseID:     caseID,
		Status:     status,
		StartedAt:  at,
		FinishedAt: at.Add(time.Second),
		CreatedAt:  at,
	}); err != nil {
		t.Fatalf("record coverage case run %s: %v", runID, err)
	}
}

func writeWorkflowBatchReportProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha","baseStepTimeoutMs":1000}],
  "interfaceNodes": [
    {"id":"node.first","displayName":"First Node","serviceId":"service.alpha","method":"GET","path":"/first"},
    {"id":"node.second","displayName":"Second Node","serviceId":"service.alpha","method":"GET","path":"/second"}
  ],
  "apiCases": [
    {"id":"case.first","displayName":"First Step Case","nodeId":"node.first","sortOrder":1},
    {"id":"case.second","displayName":"Second Step Case","nodeId":"node.second","sortOrder":2}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [
    {"workflowId":"workflow.alpha","stepId":"first","nodeId":"node.first","caseId":"case.first","required":true,"sortOrder":1},
    {"workflowId":"workflow.alpha","stepId":"second","nodeId":"node.second","caseId":"case.second","required":true,"sortOrder":2}
  ],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "schemaVersion": "1",
  "templateConfigs": [
    {
      "id": "cfg.step.first",
      "templateId": "case-execution",
      "workflowId": "workflow.alpha",
      "nodeId": "service.alpha",
      "scopeType": "step",
      "scopeId": "first",
      "title": "First Step",
      "status": "active",
      "sortOrder": 1,
      "configJson": "{\"caseId\":\"case.first\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"service.alpha\",\"path\":\"/first\",\"expectedHttpCodes\":[200]},\"exports\":[{\"name\":\"item_id\",\"from\":\"responseBody\",\"path\":\"item_id\"}]}"
    },
    {
      "id": "cfg.step.second",
      "templateId": "case-execution",
      "workflowId": "workflow.alpha",
      "nodeId": "service.alpha",
      "scopeType": "step",
      "scopeId": "second",
      "title": "Second Step",
      "status": "active",
      "sortOrder": 2,
      "configJson": "{\"caseId\":\"case.second\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"service.alpha\",\"path\":\"/second\",\"expectedHttpCodes\":[200]},\"inputs\":[{\"name\":\"item_id\",\"source\":\"previous\"}]}"
    }
  ]
}`)
	return dir
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read json file %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode json file %s: %v\n%s", path, err, raw)
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

func createLegacyRuntimeDB(t *testing.T, path string) {
	t.Helper()
	createLegacyRuntimeDBWithIDs(t, path, 7, 11, "case-run-parent")
}

func createLegacyRuntimeDBWithIDs(t *testing.T, path string, workflowLegacyID int64, caseLegacyID int64, parentRunID string) {
	t.Helper()
	statement := fmt.Sprintf(`
create table workflow_runs (
  id integer primary key,
  workflow_id text not null,
  status text not null,
  summary_json text not null default '',
  created_at text not null
);
create table interface_node_case_run (
  id integer primary key,
  node_id text not null,
  case_id text not null,
  run_id text not null,
  status text not null,
  failure_kind text not null default '',
  failure_reason text not null default '',
  evidence_path text not null default '',
  elapsed_ms integer not null default 0,
  summary_json text not null default '',
  created_at text not null
);
insert into workflow_runs(id, workflow_id, status, summary_json, created_at)
values (%d, 'workflow.alpha', 'passed', '{"steps":1}', '2026-05-14T01:02:03Z');
insert into interface_node_case_run(id, node_id, case_id, run_id, status, evidence_path, summary_json, created_at)
values (%d, 'node.alpha', 'case.alpha', '%s', 'failed', '.runtime/cases/%s', '{"failure":"expected"}', '2026-05-14T01:03:03Z');
`, workflowLegacyID, caseLegacyID, parentRunID, parentRunID)
	cmd := exec.Command("sqlite3", path, statement)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create legacy db: %v\n%s", err, out)
	}
}
