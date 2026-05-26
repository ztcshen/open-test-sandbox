package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		"--health-url", newHealthyTestURL(t),
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
		"--health-url", newHealthyTestURL(t),
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
	fixture := newEnvironmentRestoreRefFixture(t, "env.restore.ref")
	workspace := fixture.workspaceWithCompose(t)
	fakeDockerEnv, _ := fakeDockerCommand(t)
	fixture.registerRefEnvironment(t)

	out := fixture.restoreRefEnvironment(t, fakeDockerEnv, workspace, "--execute")
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
	fixture := newEnvironmentRestoreRefFixture(t, "env.restore.existing.ref")
	fixture.pushCommitAfterTag(t, "second")
	workspace := fixture.workspaceWithCompose(t)
	checkout := fixture.cloneMain(t, workspace)
	fakeDockerEnv, _ := fakeDockerCommand(t)
	fixture.registerRefEnvironment(t)

	out := fixture.restoreRefEnvironment(t, fakeDockerEnv, workspace, "--execute")
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
	fixture := newEnvironmentRestoreRefFixture(t, "env.restore.existing.ref.detach")
	workspace := fixture.workspaceWithCompose(t)
	checkout := fixture.cloneMain(t, workspace)
	fakeDockerEnv, _ := fakeDockerCommand(t)
	fixture.registerRefEnvironment(t)

	out := fixture.restoreRefEnvironment(t, fakeDockerEnv, workspace, "--execute")
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

type environmentRestoreRefFixture struct {
	envID      string
	storePath  string
	remoteRepo string
	work       string
}

func newEnvironmentRestoreRefFixture(t *testing.T, envID string) environmentRestoreRefFixture {
	t.Helper()
	remoteRepo := createBareGitRepo(t, "main")
	work := filepath.Join(filepath.Dir(remoteRepo), "work")
	runGit(t, work, "tag", "v1")
	runGit(t, work, "push", "origin", "v1")
	return environmentRestoreRefFixture{
		envID:      envID,
		storePath:  filepath.Join(t.TempDir(), "store.sqlite"),
		remoteRepo: remoteRepo,
		work:       work,
	}
}

func (fixture environmentRestoreRefFixture) pushCommitAfterTag(t *testing.T, message string) {
	t.Helper()
	writeFile(t, filepath.Join(fixture.work, "README.md"), "# restore fixture\n\nupdated\n")
	runGit(t, fixture.work, "add", "README.md")
	runGit(t, fixture.work, "-c", "user.name=Open Test", "-c", "user.email=open-test@example.com", "commit", "-m", message)
	runGit(t, fixture.work, "push", "origin", "main")
}

func (fixture environmentRestoreRefFixture) workspaceWithCompose(t *testing.T) string {
	t.Helper()
	workspace := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")
	return workspace
}

func (fixture environmentRestoreRefFixture) cloneMain(t *testing.T, workspace string) string {
	t.Helper()
	checkout := filepath.Join(workspace, "entry-gateway")
	runGit(t, "", "clone", "--branch", "main", fixture.remoteRepo, checkout)
	return checkout
}

func (fixture environmentRestoreRefFixture) registerRefEnvironment(t *testing.T) {
	t.Helper()
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+fixture.storePath,
		"--id", fixture.envID,
		"--repo", "entry-gateway="+fixture.remoteRepo,
		"--branch", "entry-gateway=main",
		"--repo-ref", "entry-gateway=v1",
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--health-url", newHealthyTestURL(t),
		"--verification-workflow", "workflow.core-10",
	)
}

func (fixture environmentRestoreRefFixture) restoreRefEnvironment(t *testing.T, env []string, workspace string, extraArgs ...string) string {
	t.Helper()
	args := append([]string{
		"environment", "restore",
		"--store", "sqlite://" + fixture.storePath,
		"--workspace", workspace,
	}, extraArgs...)
	args = append(args, "--json", fixture.envID)
	return runCLIWithEnv(t, env, args...)
}

func TestEnvironmentRestorePreflightRequiresGitForExistingCheckoutRef(t *testing.T) {
	fixture := newEnvironmentRestoreRefFixture(t, "env.restore.preflight.existing.ref")
	workspace := fixture.workspaceWithCompose(t)
	fakeDockerEnv, _ := fakeDockerCommand(t)
	fixture.cloneMain(t, workspace)
	fixture.registerRefEnvironment(t)

	out := fixture.restoreRefEnvironment(t, fakeDockerEnv, workspace)
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
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	writeFile(t, filepath.Join(checkout, "README.md"), "# existing checkout\n")
	writeFile(t, filepath.Join(workspace, "docker-compose.yml"), "services: {}\n")

	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.existing.checkout",
		"--service", "entry-gateway",
		"--checkout", "entry-gateway=entry-gateway",
		"--compose-file", "docker-compose.yml",
		"--health-url", healthServer.URL+"/health",
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
