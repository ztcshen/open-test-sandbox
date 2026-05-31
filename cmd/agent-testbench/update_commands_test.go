package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestUpdateCheckReportsRemoteRevisionWithoutMutatingCheckout(t *testing.T) {
	remoteRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"cmd/agent-testbench/main.go": "package main\nfunc main() {}\n",
		"go.mod":                      "module update-fixture\n",
	})
	checkout := cloneUpdateFixture(t, remoteRepo)
	localHead := strings.TrimSpace(runGit(t, checkout, "rev-parse", "HEAD"))
	pushUpdateFixtureCommit(t, remoteRepo, "main", "README.md", "# updated\n")

	out := runCLI(t, "update", "--repo", checkout, "--check", "--json")
	var report struct {
		OK              bool   `json:"ok"`
		CheckOnly       bool   `json:"checkOnly"`
		UpdateAvailable bool   `json:"updateAvailable"`
		Updated         bool   `json:"updated"`
		Remote          string `json:"remote"`
		Branch          string `json:"branch"`
		LocalRevision   string `json:"localRevision"`
		RemoteRevision  string `json:"remoteRevision"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode update check report: %v\n%s", err, out)
	}
	if !report.OK || !report.CheckOnly || !report.UpdateAvailable || report.Updated {
		t.Fatalf("update check report = %#v", report)
	}
	if report.Remote != "origin" || report.Branch != "main" || report.LocalRevision == report.RemoteRevision {
		t.Fatalf("unexpected update check target/revisions: %#v", report)
	}
	if head := strings.TrimSpace(runGit(t, checkout, "rev-parse", "HEAD")); head != localHead {
		t.Fatalf("check mode changed checkout head: got %s want %s", head, localHead)
	}
}

func TestUpdatePullsFastForwardAndRebuildsRuntimeBinary(t *testing.T) {
	remoteRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"cmd/agent-testbench/main.go": "package main\nfunc main() {}\n",
		"go.mod":                      "module update-fixture\n",
	})
	checkout := cloneUpdateFixture(t, remoteRepo)
	remoteHead := pushUpdateFixtureCommit(t, remoteRepo, "main", "README.md", "# updated\n")
	fakeGoEnv, callsPath := fakeUpdateGoCommand(t)

	out := runCLIWithEnv(t, fakeGoEnv, "update", "--repo", checkout, "--json")
	var report struct {
		OK              bool   `json:"ok"`
		UpdateAvailable bool   `json:"updateAvailable"`
		Updated         bool   `json:"updated"`
		RuntimePath     string `json:"runtimePath"`
		Steps           []struct {
			Name    string   `json:"name"`
			Command []string `json:"command"`
			OK      bool     `json:"ok"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode update report: %v\n%s", err, out)
	}
	if !report.OK || !report.UpdateAvailable || !report.Updated {
		t.Fatalf("update report = %#v", report)
	}
	if head := strings.TrimSpace(runGit(t, checkout, "rev-parse", "HEAD")); head != remoteHead {
		t.Fatalf("checkout head = %s, want remote head %s", head, remoteHead)
	}
	wantRuntime := filepath.Join(checkout, ".runtime", "bin", "agent-testbench")
	if report.RuntimePath != wantRuntime {
		t.Fatalf("runtime path = %q, want %q", report.RuntimePath, wantRuntime)
	}
	calls := readUpdateCalls(t, callsPath)
	wantBuild := "build -o " + wantRuntime + " ./cmd/agent-testbench"
	if !strings.Contains(calls, wantBuild) {
		t.Fatalf("go build command missing %q:\n%s", wantBuild, calls)
	}
}

func TestUpdateCheckDoesNotReportLocalAheadAsAvailable(t *testing.T) {
	remoteRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"cmd/agent-testbench/main.go": "package main\nfunc main() {}\n",
		"go.mod":                      "module update-fixture\n",
	})
	checkout := cloneUpdateFixture(t, remoteRepo)
	writeFile(t, filepath.Join(checkout, "LOCAL.md"), "# local only\n")
	runGit(t, checkout, "add", ".")
	runGit(t, checkout, "-c", "user.name=Open Test", "-c", "user.email=open-test@example.com", "commit", "-m", "local ahead")

	out := runCLI(t, "update", "--repo", checkout, "--check", "--json")
	var report struct {
		OK              bool `json:"ok"`
		CheckOnly       bool `json:"checkOnly"`
		UpdateAvailable bool `json:"updateAvailable"`
		Updated         bool `json:"updated"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode local-ahead update check report: %v\n%s", err, out)
	}
	if !report.OK || !report.CheckOnly || report.UpdateAvailable || report.Updated {
		t.Fatalf("local-ahead update check report = %#v", report)
	}
}

func TestUpdateReleaseLatestResolvesHighestRemoteTag(t *testing.T) {
	remoteRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"cmd/agent-testbench/main.go": "package main\nfunc main() {}\n",
		"go.mod":                      "module update-fixture\n",
	})
	checkout := cloneUpdateFixture(t, remoteRepo)
	runGit(t, checkout, "tag", "v0.3.0")
	runGit(t, checkout, "push", "origin", "v0.3.0")
	remoteHead := pushUpdateFixtureCommit(t, remoteRepo, "main", "README.md", "# updated\n")
	tagUpdateFixture(t, remoteRepo, "main", "v0.3.2")

	out := runCLI(t, "update", "--repo", checkout, "--release", "latest", "--check", "--json")
	var report struct {
		OK              bool   `json:"ok"`
		CheckOnly       bool   `json:"checkOnly"`
		UpdateAvailable bool   `json:"updateAvailable"`
		Release         string `json:"release"`
		Branch          string `json:"branch"`
		RemoteRevision  string `json:"remoteRevision"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode release update report: %v\n%s", err, out)
	}
	if !report.OK || !report.CheckOnly || !report.UpdateAvailable || report.Release != "v0.3.2" || report.Branch != "v0.3.2" {
		t.Fatalf("release update report = %#v", report)
	}
	if report.RemoteRevision != remoteHead {
		t.Fatalf("release remote revision = %s, want %s", report.RemoteRevision, remoteHead)
	}
}

func TestUpdateReleaseTagComparisonUsesVersionOrder(t *testing.T) {
	tags := []string{"v0.9.9", "v0.10.0-rc1", "v0.10.0", "legacy", "v0.10.0-rc.2", "v0.10.0-rc.10"}
	sort.SliceStable(tags, func(i int, j int) bool {
		return compareUpdateReleaseTags(tags[i], tags[j]) > 0
	})
	if strings.Join(tags, ",") != "v0.10.0,v0.10.0-rc.10,v0.10.0-rc.2,v0.10.0-rc1,v0.9.9,legacy" {
		t.Fatalf("unexpected release tag order: %v", tags)
	}
}

func TestUpdateChannelReleaseDefaultsToLatest(t *testing.T) {
	remoteRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"cmd/agent-testbench/main.go": "package main\nfunc main() {}\n",
		"go.mod":                      "module update-fixture\n",
	})
	checkout := cloneUpdateFixture(t, remoteRepo)
	remoteHead := pushUpdateFixtureCommit(t, remoteRepo, "main", "README.md", "# updated\n")
	tagUpdateFixture(t, remoteRepo, "main", "v0.4.0")

	out := runCLI(t, "update", "--repo", checkout, "--channel", "release", "--check", "--json")
	var report struct {
		OK             bool   `json:"ok"`
		Channel        string `json:"channel"`
		Release        string `json:"release"`
		Branch         string `json:"branch"`
		RemoteRevision string `json:"remoteRevision"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode release channel report: %v\n%s", err, out)
	}
	if !report.OK || report.Channel != "release" || report.Release != "v0.4.0" || report.Branch != "v0.4.0" || report.RemoteRevision != remoteHead {
		t.Fatalf("release channel report = %#v, remote head %s", report, remoteHead)
	}
}

func TestUpdateChannelMainDefaultsToMainBranch(t *testing.T) {
	remoteRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"cmd/agent-testbench/main.go": "package main\nfunc main() {}\n",
		"go.mod":                      "module update-fixture\n",
	})
	checkout := cloneUpdateFixture(t, remoteRepo)

	out := runCLI(t, "update", "--repo", checkout, "--channel", "main", "--check", "--json")
	var report struct {
		OK      bool   `json:"ok"`
		Channel string `json:"channel"`
		Branch  string `json:"branch"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode main channel report: %v\n%s", err, out)
	}
	if !report.OK || report.Channel != "main" || report.Branch != "main" {
		t.Fatalf("main channel report = %#v", report)
	}
}

func TestUpdateCheckTextShowsNextAction(t *testing.T) {
	remoteRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"cmd/agent-testbench/main.go": "package main\nfunc main() {}\n",
		"go.mod":                      "module update-fixture\n",
	})
	checkout := cloneUpdateFixture(t, remoteRepo)
	pushUpdateFixtureCommit(t, remoteRepo, "main", "README.md", "# updated\n")

	out := runCLI(t, "update", "--repo", checkout, "--check")
	if !strings.Contains(out, "Update Available: true") || !strings.Contains(out, "Next: agent-testbench update") {
		t.Fatalf("update check text should show next action:\n%s", out)
	}
}

func TestUpdateRejectsTrackedLocalChangesWithoutForce(t *testing.T) {
	remoteRepo := createBareGitRepoWithFiles(t, "main", map[string]string{
		"cmd/agent-testbench/main.go": "package main\nfunc main() {}\n",
		"go.mod":                      "module update-fixture\n",
	})
	checkout := cloneUpdateFixture(t, remoteRepo)
	writeFile(t, filepath.Join(checkout, "go.mod"), "module local-change\n")

	out := runCLIFails(t, "update", "--repo", checkout, "--json")
	if !strings.Contains(out, `"dirty": true`) || !strings.Contains(out, "tracked files have local changes") || !strings.Contains(out, "Next: commit or stash") {
		t.Fatalf("dirty checkout update output = %q", out)
	}
}

func tagUpdateFixture(t *testing.T, remoteRepo string, branch string, tag string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "remote-work")
	runGit(t, "", "clone", "--branch", branch, remoteRepo, work)
	runGit(t, work, "tag", tag)
	runGit(t, work, "push", "origin", tag)
}

func cloneUpdateFixture(t *testing.T, remoteRepo string) string {
	t.Helper()
	checkout := filepath.Join(t.TempDir(), "checkout")
	runGit(t, "", "clone", "--branch", "main", remoteRepo, checkout)
	return checkout
}

func pushUpdateFixtureCommit(t *testing.T, remoteRepo string, branch string, name string, body string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "remote-work")
	runGit(t, "", "clone", "--branch", branch, remoteRepo, work)
	writeFile(t, filepath.Join(work, name), body)
	runGit(t, work, "add", ".")
	runGit(t, work, "-c", "user.name=Open Test", "-c", "user.email=open-test@example.com", "commit", "-m", "update fixture")
	runGit(t, work, "push", "origin", branch)
	return strings.TrimSpace(runGit(t, work, "rev-parse", "HEAD"))
}

func fakeUpdateGoCommand(t *testing.T) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	callsPath := filepath.Join(dir, "go-calls.txt")
	goPath := filepath.Join(dir, "go")
	writeFile(t, goPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$GO_CALLS_FILE"
if [[ "$1" == "build" && "$2" == "-o" ]]; then
  mkdir -p "$(dirname "$3")"
  printf '#!/usr/bin/env sh\n' > "$3"
  chmod +x "$3"
fi
`)
	if err := os.Chmod(goPath, 0o755); err != nil {
		t.Fatalf("chmod fake go: %v", err)
	}
	return []string{
		"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GO_CALLS_FILE=" + callsPath,
	}, callsPath
}

func readUpdateCalls(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read calls %s: %v", path, err)
	}
	return string(raw)
}
