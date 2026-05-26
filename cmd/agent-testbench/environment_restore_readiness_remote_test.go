package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
)

func TestEnvironmentRestoreClonesRemoteReposForVerifiedWorkflow(t *testing.T) {
	fixture := newEnvironmentRestoreRemoteRepoFixture(t)

	dryRun := fixture.runDryRun()
	fixture.assertDryRun(dryRun)
	fixture.assertDryRunDidNotCheckout()

	executed := fixture.runExecute()
	fixture.assertExecuted(executed)
	fixture.assertDockerComposeUpCalled()
	fixture.assertCheckoutRestored()

	inspected := fixture.inspectEnvironment()
	fixture.assertPersistedSummary(inspected, executed.RestoreID)
}

type environmentRestoreRemoteRepoFixture struct {
	t                *testing.T
	storePath        string
	workspace        string
	expectedCheckout string
	fakeDockerEnv    []string
	dockerCallsPath  string
}

func newEnvironmentRestoreRemoteRepoFixture(t *testing.T) environmentRestoreRemoteRepoFixture {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	remoteRepo := createBareGitRepo(t, "main")
	remoteURL := "https://example.test/entry-gateway.git"
	workspace := filepath.Join(t.TempDir(), "workspace")
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(healthServer.Close)
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	installGitRemoteFixture(t, filepath.Dir(dockerCallsPath), remoteURL, remoteRepo)

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.restore",
		"--repo", "entry-gateway="+remoteURL,
		"--branch", "entry-gateway=main",
		"--checkout", "entry-gateway=services/entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--start-command", "docker compose up -d",
		"--health-url", healthServer.URL+"/health",
		"--verification-workflow", "workflow.core-10",
	)
	sourceCompose := filepath.Join(t.TempDir(), "docker-compose.yml")
	writeFile(t, sourceCompose, "services: {}\n")
	runCLI(t, "environment", "startup-file", "put",
		"--store", "sqlite://"+storePath,
		"--file", "docker-compose.yml="+sourceCompose,
		"env.restore",
	)
	graphPath := filepath.Join(t.TempDir(), "graph.json")
	writeFile(t, graphPath, mustJSON(t, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			environmentRestoreReadinessAppComponent("entry-gateway", "entry-gateway", healthServer.URL+"/health"),
		},
	}))
	runCLI(t, "environment", "components", "replace", "--store", "sqlite://"+storePath, "--file", graphPath, "env.restore")

	return environmentRestoreRemoteRepoFixture{
		t:                t,
		storePath:        storePath,
		workspace:        workspace,
		expectedCheckout: filepath.Join(workspace, "services", "entry-gateway"),
		fakeDockerEnv:    fakeDockerEnv,
		dockerCallsPath:  dockerCallsPath,
	}
}

type environmentRestoreRemoteRepoDryRun struct {
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

type environmentRestoreRemoteRepoExecuted struct {
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

type environmentRestoreRemoteRepoInspection struct {
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

func (fixture environmentRestoreRemoteRepoFixture) runDryRun() environmentRestoreRemoteRepoDryRun {
	fixture.t.Helper()
	out := runCLIWithEnv(fixture.t, fixture.fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+fixture.storePath, "--workspace", fixture.workspace, "--json", "env.restore")
	var dryRun environmentRestoreRemoteRepoDryRun
	if err := json.Unmarshal([]byte(out), &dryRun); err != nil {
		fixture.t.Fatalf("decode restore dry-run json: %v\n%s", err, out)
	}
	return dryRun
}

func (fixture environmentRestoreRemoteRepoFixture) assertDryRun(dryRun environmentRestoreRemoteRepoDryRun) {
	fixture.t.Helper()
	if !dryRun.OK || dryRun.Executed || dryRun.VerificationWorkflow != "workflow.core-10" || len(dryRun.Repos) != 1 {
		fixture.t.Fatalf("restore dry-run report = %#v", dryRun)
	}
	if dryRun.Repos[0].ServiceID != "entry-gateway" || dryRun.Repos[0].Action != "clone" || dryRun.Repos[0].Checkout != fixture.expectedCheckout || strings.Join(dryRun.Repos[0].Command, " ") == "" {
		fixture.t.Fatalf("restore dry-run repo = %#v", dryRun.Repos[0])
	}
	if !dryRun.Docker.OK || dryRun.Docker.Action != "plan-docker-compose" || len(dryRun.Docker.Commands) == 0 || !commandSlicesContain(dryRun.Docker.Commands, "up") {
		fixture.t.Fatalf("restore dry-run docker plan = %#v", dryRun.Docker)
	}
	if !dryRun.Preflight.OK || !restorePreflightHasTool(dryRun.Preflight.Tools, "git", true) || !restorePreflightHasTool(dryRun.Preflight.Tools, "docker", true) || !restorePreflightHasTool(dryRun.Preflight.Tools, "docker compose", true) || len(dryRun.Preflight.HeavySteps) == 0 {
		fixture.t.Fatalf("restore dry-run preflight = %#v", dryRun.Preflight)
	}
	if !dryRun.Readiness.OK || !dryRun.Readiness.PauseBeforeHeavyValidation || !restoreReadinessHasItem(dryRun.Readiness.Items, "component-repositories", true, "will be cloned") || !restoreReadinessHasItem(dryRun.Readiness.Items, "compose-services-and-middleware", true, "including middleware") || !restoreReadinessHasItem(dryRun.Readiness.Items, "health-probes", true, "1 Store-backed") || !restoreReadinessHasItem(dryRun.Readiness.Items, "operator-pause", true, "pause before") {
		fixture.t.Fatalf("restore dry-run readiness = %#v", dryRun.Readiness)
	}
	if len(dryRun.NextActions) == 0 || !strings.Contains(strings.Join(dryRun.NextActions, "\n"), "workflow.core-10") {
		fixture.t.Fatalf("restore dry-run should anchor next actions to verification workflow: %#v", dryRun.NextActions)
	}
}

func (fixture environmentRestoreRemoteRepoFixture) assertDryRunDidNotCheckout() {
	fixture.t.Helper()
	if _, err := os.Stat(fixture.expectedCheckout); !os.IsNotExist(err) {
		fixture.t.Fatalf("dry-run should not create checkout, stat err=%v", err)
	}
}

func (fixture environmentRestoreRemoteRepoFixture) runExecute() environmentRestoreRemoteRepoExecuted {
	fixture.t.Helper()
	out := runCLIWithEnv(fixture.t, fixture.fakeDockerEnv, "environment", "restore", "--store", "sqlite://"+fixture.storePath, "--workspace", fixture.workspace, "--execute", "--json", "env.restore")
	var executed environmentRestoreRemoteRepoExecuted
	if err := json.Unmarshal([]byte(out), &executed); err != nil {
		fixture.t.Fatalf("decode restore execute json: %v\n%s", err, out)
	}
	return executed
}

func (fixture environmentRestoreRemoteRepoFixture) assertExecuted(executed environmentRestoreRemoteRepoExecuted) {
	fixture.t.Helper()
	if !executed.OK || !executed.Executed || len(executed.Repos) != 1 || executed.Repos[0].Action != "clone" || !executed.Repos[0].OK {
		fixture.t.Fatalf("restore execute report = %#v", executed)
	}
	if !executed.Docker.OK || len(executed.Docker.HealthChecks) != 1 || !executed.Docker.HealthChecks[0].OK {
		fixture.t.Fatalf("restore execute docker report = %#v", executed.Docker)
	}
}

func (fixture environmentRestoreRemoteRepoFixture) assertDockerComposeUpCalled() {
	fixture.t.Helper()
	dockerCalls, err := os.ReadFile(fixture.dockerCallsPath)
	if err != nil {
		fixture.t.Fatalf("read fake docker calls: %v", err)
	}
	composePath := filepath.Join(fixture.workspace, "docker-compose.yml")
	if want := "compose -f " + composePath + " up -d"; !strings.Contains(string(dockerCalls), want) {
		fixture.t.Fatalf("fake docker calls missing %q:\n%s", want, dockerCalls)
	}
}

func (fixture environmentRestoreRemoteRepoFixture) assertCheckoutRestored() {
	fixture.t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixture.expectedCheckout, "README.md"))
	if err != nil || !strings.Contains(string(raw), "restore fixture") {
		fixture.t.Fatalf("restored checkout missing fixture file raw=%q err=%v", raw, err)
	}
}

func (fixture environmentRestoreRemoteRepoFixture) inspectEnvironment() environmentRestoreRemoteRepoInspection {
	fixture.t.Helper()
	out := runCLI(fixture.t, "environment", "inspect", "--store", "sqlite://"+fixture.storePath, "--json", "env.restore")
	var inspected environmentRestoreRemoteRepoInspection
	if err := json.Unmarshal([]byte(out), &inspected); err != nil {
		fixture.t.Fatalf("decode restored environment inspect json: %v\n%s", err, out)
	}
	return inspected
}

func (fixture environmentRestoreRemoteRepoFixture) assertPersistedSummary(inspected environmentRestoreRemoteRepoInspection, restoreID string) {
	fixture.t.Helper()
	lastRestore := inspected.Environment.Summary.LastRestore
	if lastRestore.ID != restoreID || !lastRestore.OK || !lastRestore.Executed || lastRestore.Phase != "completed" || lastRestore.VerificationWorkflow != "workflow.core-10" || lastRestore.Docker.Action != "run-docker-compose" || !lastRestore.Docker.OK || lastRestore.Docker.HealthChecks != 1 || lastRestore.Docker.HealthPassed != 1 || len(lastRestore.Repositories) != 1 || lastRestore.Repositories[0].Action != "clone" || !lastRestore.Repositories[0].OK {
		fixture.t.Fatalf("persisted restore summary = %#v; executed restore id=%s", lastRestore, restoreID)
	}
	if !lastRestore.Readiness.OK || len(lastRestore.Readiness.FailedItems) != 0 {
		fixture.t.Fatalf("persisted readiness summary = %#v", lastRestore.Readiness)
	}
	attempts := inspected.Environment.Summary.RestoreAttempts
	if len(attempts) != 2 || attempts[0].ID == attempts[1].ID || attempts[1].ID != restoreID || attempts[1].Phase != "completed" {
		fixture.t.Fatalf("persisted restore attempts = %#v; executed restore id=%s", attempts, restoreID)
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

func TestEnvironmentRestoreRequiresRemoteGitSourcesForSQLOneClickEnvironment(t *testing.T) {
	for _, backend := range environmentRestoreReadinessProductStoreBackends() {
		t.Run(backend.name, func(t *testing.T) {
			env := newEnvironmentRestoreReadinessEnv(
				"env.remote.sources."+backend.name,
				`{"composeFile":"compose/docker-compose.yml","package":{"url":"/Users/zlh/codes/agent-testbench-validation","checkout":"."}}`,
				`[{"kind":"url","url":"http://127.0.0.1:28080/health"}]`,
			)
			env.ReposJSON = `{"llt":{"url":"/Users/zlh/codes/agent-testbench-llt-simulator","checkout":"llt"}}`
			report := buildEnvironmentRestoreSQLPolicyReport(t, backend, env)
			if report.OK || report.SourcePolicy.OK || !report.SourcePolicy.RemoteOnly || len(report.SourcePolicy.Violations) != 1 || report.Docker.Action != "skipped-due-to-source-policy" {
				t.Fatalf("%s remote source policy report = %#v", backend.name, report)
			}
			if !strings.Contains(report.SourcePolicy.Violations[0], "component llt") {
				t.Fatalf("%s source policy should only reject component repositories, got %#v", backend.name, report.SourcePolicy.Violations)
			}
			if !restoreTypedReadinessHasItem(report.Readiness.Items, "remote-git-sources", false, "remote Git URL") {
				t.Fatalf("%s readiness should include remote source violation: %#v", backend.name, report.Readiness.Items)
			}
		})
	}
}

func TestEnvironmentRestoreRequiresRemoteSourcesForSQLStoreBackends(t *testing.T) {
	for _, storeURL := range environmentRestoreReadinessProductStoreURLs() {
		if !environmentRestoreRequiresRemoteSources(storeURL) {
			t.Fatalf("SQL Store URL should require remote restore sources: %s", storeURL)
		}
	}
	for _, storeURL := range environmentRestoreReadinessCompatibilityStoreURLs() {
		if environmentRestoreRequiresRemoteSources(storeURL) {
			t.Fatalf("compatibility Store URL should not require SQL remote source policy: %s", storeURL)
		}
	}
}
