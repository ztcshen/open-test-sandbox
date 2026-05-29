package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/store"
)

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
	if !strings.Contains(cleanup.Warning, "SQL Store") {
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
		"--health-url", newHealthyTestURL(t),
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

	out := runCLIFails(t, "environment", "restore", "env.missing.compose", "--store", "sqlite://"+storePath, "--workspace", workspace, "--execute", "--json")
	if !strings.Contains(out, "missing-compose-file") || !strings.Contains(out, "missing-compose.yml") {
		t.Fatalf("missing compose restore output = %q", out)
	}
}

func TestEnvironmentRestoreRunsVerificationWorkflowAfterDockerHealth(t *testing.T) {
	fixture := newEnvironmentRestoreWorkflowRunFixture(t)
	fixture.registerEnvironment(t)

	report := decodeRestoreWorkflowReport(t, fixture.runRestore(t))
	assertRestoreWorkflowRunReport(t, report, fixture.outputDir)
	fixture.assertAcceptancePayload(t)
	fixture.assertPersistedVerification(t, report.Workflow.RunID)
}

type environmentRestoreWorkflowRunFixture struct {
	envID             string
	storePath         string
	workspace         string
	outputDir         string
	fakeDockerEnv     []string
	healthServer      *httptest.Server
	acceptanceServer  *httptest.Server
	acceptancePayload map[string]any
}

type restoreWorkflowRunReportForTest struct {
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

func newEnvironmentRestoreWorkflowRunFixture(t *testing.T) *environmentRestoreWorkflowRunFixture {
	t.Helper()

	fakeDockerEnv, _ := fakeDockerCommand(t)
	fixture := &environmentRestoreWorkflowRunFixture{
		envID:         "env.workflow.restore",
		storePath:     filepath.Join(t.TempDir(), "store.sqlite"),
		workspace:     filepath.Join(t.TempDir(), "workspace"),
		outputDir:     filepath.Join(t.TempDir(), "workflow-evidence"),
		fakeDockerEnv: fakeDockerEnv,
	}
	fixture.healthServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fixture.healthServer.Close)
	fixture.acceptanceServer = newRestoreWorkflowAcceptanceServer(t, fixture)
	t.Cleanup(fixture.acceptanceServer.Close)
	writeFile(t, filepath.Join(fixture.workspace, "compose.yml"), "services: {}\n")
	return fixture
}

func newRestoreWorkflowAcceptanceServer(t *testing.T, fixture *environmentRestoreWorkflowRunFixture) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments/"+fixture.envID+"/acceptance-runs":
			if err := json.NewDecoder(r.Body).Decode(&fixture.acceptancePayload); err != nil {
				t.Fatalf("decode acceptance payload: %v", err)
			}
			writeTestJSON(t, w, http.StatusAccepted, restoreWorkflowAcceptedPayload(fixture.envID))
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/"+fixture.envID+"/acceptance-runs/batch.env.restore.acceptance.001":
			writeTestJSON(t, w, http.StatusOK, restoreWorkflowPassedPayload(fixture.envID))
		default:
			t.Fatalf("unexpected acceptance request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

func restoreWorkflowAcceptedPayload(envID string) map[string]any {
	return map[string]any{
		"ok":            true,
		"environmentId": envID,
		"batchRunId":    "batch.env.restore.acceptance.001",
		"requestId":     "restore.env.workflow.restore",
		"workflowId":    "workflow.alpha",
		"status":        "running",
		"reportUrl":     "/api/environments/" + envID + "/acceptance-runs/batch.env.restore.acceptance.001",
	}
}

func restoreWorkflowPassedPayload(envID string) map[string]any {
	return map[string]any{
		"ok":            true,
		"environmentId": envID,
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
	}
}

func (fixture *environmentRestoreWorkflowRunFixture) registerEnvironment(t *testing.T) {
	t.Helper()
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+fixture.storePath,
		"--id", fixture.envID,
		"--compose-file", "compose.yml",
		"--health-url", fixture.healthServer.URL+"/ready",
		"--verification-workflow", "workflow.alpha",
	)
}

func (fixture *environmentRestoreWorkflowRunFixture) runRestore(t *testing.T) string {
	t.Helper()
	return runCLIWithEnv(t, fixture.fakeDockerEnv,
		"environment", "restore",
		"--store", "sqlite://"+fixture.storePath,
		"--workspace", fixture.workspace,
		"--execute",
		"--run-workflow",
		"--server-url", fixture.acceptanceServer.URL,
		"--base-url", "http://127.0.0.1:18080",
		"--workflow-output-dir", fixture.outputDir,
		"--json",
		fixture.envID,
	)
}

func decodeRestoreWorkflowReport(t *testing.T, out string) restoreWorkflowRunReportForTest {
	t.Helper()
	var report restoreWorkflowRunReportForTest
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode restore workflow json: %v\n%s", err, out)
	}
	return report
}

func assertRestoreWorkflowRunReport(t *testing.T, report restoreWorkflowRunReportForTest, outputDir string) {
	t.Helper()
	if !report.OK || !report.Executed || !report.Docker.OK || !report.Workflow.OK || report.Workflow.Action != "run-acceptance-workflow" || report.Workflow.WorkflowID != "workflow.alpha" || report.Workflow.RunID != "batch.env.restore.acceptance.001" {
		t.Fatalf("restore workflow report = %#v", report)
	}
	if report.Workflow.OutputDir != outputDir || report.Workflow.ReportURL == "" || !report.Workflow.Acceptance.OK || report.Workflow.Acceptance.TemplateID != "environment.workflow.skywalking.v1" || report.Workflow.Acceptance.ExpectedSteps != 10 || report.Workflow.Acceptance.CompletedSteps != 10 || report.Workflow.Acceptance.PassedSteps != 10 || report.Workflow.Acceptance.FailedSteps != 0 || report.Workflow.Acceptance.TopologyProvider != "skywalking" {
		t.Fatalf("restore workflow acceptance = %#v", report.Workflow)
	}
}

func (fixture *environmentRestoreWorkflowRunFixture) assertAcceptancePayload(t *testing.T) {
	t.Helper()
	if fixture.acceptancePayload["baseUrl"] != "http://127.0.0.1:18080" || fixture.acceptancePayload["evidenceDir"] != fixture.outputDir {
		t.Fatalf("restore acceptance payload = %#v", fixture.acceptancePayload)
	}
}

func (fixture *environmentRestoreWorkflowRunFixture) assertPersistedVerification(t *testing.T, runID string) {
	t.Helper()
	inspectOut := runCLI(t, "environment", "inspect", "--store", "sqlite://"+fixture.storePath, "--json", fixture.envID)
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
	if inspected.Environment.LastVerificationRunID != runID || inspected.Environment.LastVerificationStatus != store.StatusPassed || inspected.Environment.Status != "verification-recorded" || !inspected.Environment.EvidenceComplete || !inspected.Environment.TopologyComplete || inspected.Environment.Verified {
		t.Fatalf("restored environment status = %#v", inspected.Environment)
	}
}

func TestEnvironmentRestoreUsesNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "restore-active-pg")
	runEnvironmentRestoreUsesNamedActiveStore(t, "pg", "PostgreSQL")
}

func TestEnvironmentRestoreUsesNamedMySQLActiveStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "restore-active-mysql")
	runEnvironmentRestoreUsesNamedActiveStore(t, "mysql", "MySQL")
}

func runEnvironmentRestoreUsesNamedActiveStore(t *testing.T, suffixLabel string, label string) {
	t.Helper()
	workspace := filepath.Join(t.TempDir(), "workspace")
	outputDir := filepath.Join(t.TempDir(), "workflow-evidence")
	envID := uniqueTestID(t, "env.restore."+suffixLabel)
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
			t.Fatalf("unexpected active %s acceptance request: %s %s", label, r.Method, r.URL.Path)
		}
	}))
	defer acceptanceServer.Close()
	sourceCompose := filepath.Join(t.TempDir(), "compose.yml")
	writeFile(t, sourceCompose, "services:\n  app:\n    image: busybox\n")
	runCLI(t, "environment", "register",
		"--id", envID,
		"--compose-file", "compose.yml",
		"--health-url", healthServer.URL+"/ready",
		"--verification-workflow", "workflow.alpha",
	)
	runCLI(t, "environment", "startup-file", "put",
		"--file", "compose.yml="+sourceCompose,
		envID,
	)
	graphPath := filepath.Join(t.TempDir(), "graph.json")
	writeFile(t, graphPath, mustJSON(t, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "app", Kind: "app", Role: "business-service", ComposeService: "app", Required: true, HealthCheckJSON: `{"type":"url","url":"` + healthServer.URL + `/ready"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
		},
	}))
	runCLI(t, "environment", "components", "replace", "--file", graphPath, envID)

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
		t.Fatalf("decode active %s restore json: %v\n%s", label, err, out)
	}
	if !report.OK || !report.Executed || !report.Workflow.OK || report.Workflow.Action != "run-acceptance-workflow" || !report.Workflow.Acceptance.OK || report.Workflow.RunID == "" {
		t.Fatalf("active %s restore report = %#v", label, report)
	}
}
