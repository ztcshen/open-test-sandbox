package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
)

func TestEnvironmentRestorePreflightReportsMissingDockerComposePlugin(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	installEnvironmentRestoreDockerTool(t, "#!/bin/sh\nif [ \"$1\" = compose ] && [ \"$2\" = version ]; then exit 17; fi\nexit 0\n")
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

func TestEnvironmentRestoreExecutesDockerComposeWithoutRepository(t *testing.T) {
	fixture := newEnvironmentRestoreDockerCLIFixture(t)
	fixture.writeWorkspaceFile(t, "compose.yml", "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", fixture.StoreDSN,
		"--id", "env.docker.only",
		"--compose-file", "compose.yml",
		"--health-url", newHealthyTestURL(t),
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--json", "env.docker.only")
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
	fixture := newEnvironmentRestoreDockerCLIFixture(t)
	fixture.writeWorkspaceFile(t, "compose.yml", "services: {}\n")
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

	runCLI(t, "environment", "register",
		"--store", fixture.StoreDSN,
		"--id", "env.health.mixed",
		"--compose-file", "compose.yml",
		"--health-url", newHealthyTestURL(t),
		"--health-tcp", listener.Addr().String(),
		"--health-command", "test -f compose.yml",
		"--health-compose-service", "web",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--json", "env.health.mixed")
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
	fixture := newEnvironmentRestoreDockerCLIFixture(t)
	fixture.writeWorkspaceFile(t, "compose.yml", "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", fixture.StoreDSN,
		"--id", "env.health.fail",
		"--compose-file", "compose.yml",
		"--health-command", "echo nope && exit 7",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIFailsWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--health-timeout-seconds", "1", "--json", "env.health.fail")
	if !strings.Contains(out, `"kind": "command"`) || !strings.Contains(out, "exit status 7") {
		t.Fatalf("health failure output = %q", out)
	}
	inspectOut := runCLI(t, "environment", "inspect", "--store", fixture.StoreDSN, "--json", "env.health.fail")
	if !strings.Contains(inspectOut, `"phase": "health-check"`) {
		t.Fatalf("health failure should persist health-check phase: %s", inspectOut)
	}
}

func TestEnvironmentRestoreHonorsComposeOptionsFromStore(t *testing.T) {
	fixture := newEnvironmentRestoreDockerCLIFixture(t)
	fixture.writeWorkspaceFile(t, "compose.yml", "services: {}\n")
	fixture.writeWorkspaceFile(t, ".env.local", "MODE=local\n")
	runCLI(t, "environment", "register",
		"--store", fixture.StoreDSN,
		"--id", "env.compose.options",
		"--compose-file", "compose.yml",
		"--compose-project-name", "demo",
		"--compose-env-file", ".env.local",
		"--compose-profile", "api",
		"--compose-service", "web",
		"--compose-skip-pull",
		"--compose-skip-build",
		"--health-url", newHealthyTestURL(t),
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--json", "env.compose.options")
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
	foundComposeServiceHealth := false
	for _, check := range report.Docker.HealthChecks {
		if check.Kind == "compose-service" && check.Service == "web" && check.State == "running" && check.OK {
			foundComposeServiceHealth = true
		}
	}
	if !foundComposeServiceHealth {
		t.Fatalf("compose service readiness should be generated for requested service: %#v", report.Docker.HealthChecks)
	}
	want := "compose -f " + filepath.Join(fixture.Workspace, "compose.yml") + " -p demo --env-file " + filepath.Join(fixture.Workspace, ".env.local") + " --profile api up -d web"
	dockerCalls, err := os.ReadFile(fixture.DockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if strings.Contains(string(dockerCalls), " pull") || strings.Contains(string(dockerCalls), " build") || !strings.Contains(string(dockerCalls), want) {
		t.Fatalf("compose option docker calls want %q:\n%s", want, dockerCalls)
	}
	if !strings.Contains(string(dockerCalls), "compose -f "+filepath.Join(fixture.Workspace, "compose.yml")+" -p demo --env-file "+filepath.Join(fixture.Workspace, ".env.local")+" --profile api ps --format json web") {
		t.Fatalf("compose option docker calls should include service readiness check:\n%s", dockerCalls)
	}
}

func TestEnvironmentRestoreSupportsMultipleComposeFiles(t *testing.T) {
	fixture := newEnvironmentRestoreDockerCLIFixture(t)
	fixture.writeWorkspaceFile(t, "compose.base.yml", "services: {}\n")
	fixture.writeWorkspaceFile(t, "compose.apps.yml", "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", fixture.StoreDSN,
		"--id", "env.compose.multi",
		"--compose-file", "compose.base.yml",
		"--compose-file", "compose.apps.yml",
		"--compose-env", "SANDBOX_ROOT=$AGENT_TESTBENCH_WORKSPACE",
		"--compose-skip-pull",
		"--compose-skip-build",
		"--health-compose-service", "web",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--json", "env.compose.multi")
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
	dockerCalls, err := os.ReadFile(fixture.DockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	want := "compose -f " + filepath.Join(fixture.Workspace, "compose.base.yml") + " -f " + filepath.Join(fixture.Workspace, "compose.apps.yml") + " up -d"
	want = strings.Replace(want, " up -d", " --env-file "+filepath.Join(fixture.Workspace, ".agent-testbench", "restore.env")+" up -d", 1)
	if !strings.Contains(string(dockerCalls), want) {
		t.Fatalf("multi compose docker calls missing %q:\n%s", want, dockerCalls)
	}
	envFile, err := os.ReadFile(filepath.Join(fixture.Workspace, ".agent-testbench", "restore.env"))
	if err != nil || !strings.Contains(string(envFile), "SANDBOX_ROOT="+fixture.Workspace) {
		t.Fatalf("generated compose env file = %q err=%v", envFile, err)
	}
}

func TestEnvironmentRestoreDoesNotPullComposeBuildServices(t *testing.T) {
	fixture := newEnvironmentRestoreDockerCLIFixture(t)
	composeSource := filepath.Join(t.TempDir(), "compose.yml")
	writeFile(t, composeSource, `services:
  web:
    image: nginx:alpine
  llt:
    build:
      context: ${DOCKER_LLT_SIMULATOR_REPO}
    image: agent-testbench/llt-simulator:local
`)
	runCLI(t, "environment", "register",
		"--store", fixture.StoreDSN,
		"--id", "env.compose.build-filter",
		"--compose-file", "compose/docker-compose.yml",
		"--compose-generated-file", "compose/docker-compose.yml="+composeSource,
		"--compose-env", "DOCKER_LLT_SIMULATOR_REPO=$AGENT_TESTBENCH_WORKSPACE/agent-testbench-llt-simulator",
		"--compose-service", "web",
		"--compose-service", "llt",
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--json", "env.compose.build-filter")
	var report struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode build service restore json: %v\n%s", err, out)
	}
	if !report.OK {
		t.Fatalf("build service restore report = %#v\n%s", report, out)
	}
	dockerCalls, err := os.ReadFile(fixture.DockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	calls := string(dockerCalls)
	if !strings.Contains(calls, " pull web\n") || strings.Contains(calls, " pull web llt") || strings.Contains(calls, " pull llt") {
		t.Fatalf("pull should include image services only:\n%s", calls)
	}
	if !strings.Contains(calls, " build llt\n") || strings.Contains(calls, " build web") {
		t.Fatalf("build should include build services only:\n%s", calls)
	}
	if !strings.Contains(calls, " up -d web llt") {
		t.Fatalf("up should still include all requested services:\n%s", calls)
	}
}
