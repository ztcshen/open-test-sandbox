package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentRestoreCanPrepareRepositoriesBeforeDocker(t *testing.T) {
	fixture := newEnvironmentRestoreDockerCLIFixture(t)
	remoteRepo := createBareGitRepo(t, "main")
	fixture.writeWorkspaceFile(t, "compose.yml", "services: {}\n")
	runCLI(t, "environment", "register",
		"--store", fixture.StoreDSN,
		"--id", "env.prepare.repos",
		"--service", "entry-gateway",
		"--repo", "entry-gateway="+remoteRepo,
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "compose.yml",
		"--health-url", newHealthyTestURL(t),
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--prepare-repos-only", "--json", "env.prepare.repos")
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
	if _, err := os.Stat(filepath.Join(fixture.Workspace, "entry-gateway", ".git")); err != nil {
		t.Fatalf("repository was not cloned before Docker: %v", err)
	}
	dockerCalls, err := os.ReadFile(fixture.DockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if strings.Contains(string(dockerCalls), " compose ") {
		t.Fatalf("prepare repos should not invoke Docker Compose:\n%s", dockerCalls)
	}
}

func TestEnvironmentRestoreCanPreparePackageRepositoryBeforeDocker(t *testing.T) {
	fixture := newEnvironmentRestoreDockerCLIFixture(t)
	packageRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"compose/docker-compose.yml": "services: {}\n",
		"README.md":                  "# environment package\n",
	})
	if err := os.MkdirAll(fixture.Workspace, 0o755); err != nil {
		t.Fatalf("create empty workspace: %v", err)
	}
	runCLI(t, "environment", "register",
		"--store", fixture.StoreDSN,
		"--id", "env.package.prepare",
		"--package-repo", packageRepo,
		"--package-branch", "main",
		"--compose-file", "compose/docker-compose.yml",
		"--health-url", newHealthyTestURL(t),
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--prepare-repos-only", "--json", "env.package.prepare")
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
	if !report.OK || !report.Package.Configured || report.Package.Action != "clone" || !report.Package.OK || report.Package.Checkout != fixture.Workspace || len(report.Repos) != 0 || !report.Docker.OK || report.Docker.Action != "skipped-after-repository-preparation" || !report.Readiness.OK {
		t.Fatalf("package prepare report = %#v", report)
	}
	if !restoreReadinessHasItem(report.Readiness.Items, "environment-package", true, "environment package") {
		t.Fatalf("readiness should include package gate: %#v", report.Readiness.Items)
	}
	if raw, err := os.ReadFile(filepath.Join(fixture.Workspace, "compose", "docker-compose.yml")); err != nil || !strings.Contains(string(raw), "services") {
		t.Fatalf("package compose file missing raw=%q err=%v", raw, err)
	}
	dockerCalls, err := os.ReadFile(fixture.DockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if strings.Contains(string(dockerCalls), " compose ") {
		t.Fatalf("prepare package should not invoke Docker Compose:\n%s", dockerCalls)
	}
}

func TestEnvironmentRestoreWritesStoreGeneratedComposeFileBeforeDocker(t *testing.T) {
	fixture, sourceCompose, generatedPath := newEnvironmentRestoreGeneratedComposeFixture(t)
	registerEnvironmentRestoreGeneratedCompose(t, fixture, "env.generated.compose", sourceCompose,
		"--compose-skip-pull",
		"--compose-skip-build",
		"--health-url", newHealthyTestURL(t),
	)

	dryRunOut := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--json", "env.generated.compose")
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
	if !dryRun.OK || len(dryRun.Docker.Generated) != 1 || dryRun.Docker.Generated[0].Action != "plan-write" || dryRun.Docker.Generated[0].Path != generatedPath || !dryRun.Docker.Generated[0].OK {
		t.Fatalf("generated compose dry-run = %#v", dryRun)
	}
	if _, err := os.Stat(generatedPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write generated compose file, stat err=%v", err)
	}

	executeOut := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--json", "env.generated.compose")
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
	dockerCalls, err := os.ReadFile(fixture.DockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if !strings.Contains(string(dockerCalls), "compose -f "+generatedPath+" up -d") {
		t.Fatalf("fake docker calls should use generated compose file:\n%s", dockerCalls)
	}
}

func TestEnvironmentRestorePrepareReposOnlyWritesStoreGeneratedComposeFile(t *testing.T) {
	fixture, sourceCompose, generatedPath := newEnvironmentRestoreGeneratedComposeFixture(t)
	registerEnvironmentRestoreGeneratedCompose(t, fixture, "env.generated.prepare", sourceCompose,
		"--health-url", newHealthyTestURL(t),
	)

	out := runCLIWithEnv(t, fixture.DockerEnv, "environment", "restore", "--store", fixture.StoreDSN, "--workspace", fixture.Workspace, "--execute", "--prepare-repos-only", "--json", "env.generated.prepare")
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
	if !report.OK || report.Docker.Action != "skipped-after-repository-preparation" || len(report.Docker.Generated) != 1 || report.Docker.Generated[0].Action != "write" || report.Docker.Generated[0].Path != generatedPath || !report.Docker.Generated[0].OK {
		t.Fatalf("generated prepare-only report = %#v", report)
	}
	if raw, err := os.ReadFile(generatedPath); err != nil || !strings.Contains(string(raw), "generated-service") {
		t.Fatalf("generated compose file raw=%q err=%v", raw, err)
	}
	dockerCalls, err := os.ReadFile(fixture.DockerCallsPath)
	if err != nil {
		t.Fatalf("read fake docker calls: %v", err)
	}
	if strings.Contains(string(dockerCalls), " compose ") {
		t.Fatalf("prepare-only should not invoke Docker Compose:\n%s", dockerCalls)
	}
}

func newEnvironmentRestoreGeneratedComposeFixture(t *testing.T) (environmentRestoreDockerCLIFixture, string, string) {
	t.Helper()
	fixture := newEnvironmentRestoreDockerCLIFixture(t)
	sourceCompose := filepath.Join(t.TempDir(), "source-compose.yml")
	writeFile(t, sourceCompose, "services:\n  generated-service:\n    image: alpine:3.20\n")
	return fixture, sourceCompose, filepath.Join(fixture.Workspace, "compose", "docker-compose.yml")
}

func registerEnvironmentRestoreGeneratedCompose(t *testing.T, fixture environmentRestoreDockerCLIFixture, id string, sourceCompose string, args ...string) {
	t.Helper()
	registerArgs := []string{
		"environment", "register",
		"--store", fixture.StoreDSN,
		"--id", id,
		"--compose-file", "compose/docker-compose.yml",
		"--compose-generated-file", "compose/docker-compose.yml=" + sourceCompose,
	}
	registerArgs = append(registerArgs, args...)
	registerArgs = append(registerArgs, "--verification-workflow", "workflow.core-10")
	runCLI(t, registerArgs...)
}
