package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

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
	writeFile(t, dockerPath, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOCKER_CALLS_FILE\"\nif [ \"$1\" = \"compose\" ] && [ \"$2\" = \"version\" ]; then\n  printf 'Docker Compose version v2.0.0\\n'\n  exit 0\nfi\nif [ \"$1\" = \"compose\" ]; then\n  prev=\"\"\n  service=\"\"\n  for arg in \"$@\"; do\n    if [ \"$prev\" = \"--format\" ] && [ \"$arg\" = \"json\" ]; then\n      service=\"__next__\"\n    elif [ \"$service\" = \"__next__\" ]; then\n      service=\"$arg\"\n    fi\n    prev=\"$arg\"\n  done\n  if [ -n \"$service\" ] && [ \"$service\" != \"__next__\" ]; then\n    printf '{\"Name\":\"%s\",\"Service\":\"%s\",\"State\":\"running\",\"Health\":\"healthy\"}\\n' \"$service\" \"$service\"\n  fi\nfi\n")
	if err := os.Chmod(dockerPath, 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}
	return []string{
		"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"DOCKER_CALLS_FILE=" + callsPath,
	}, callsPath
}

func newHealthyTestURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/health"
}

func fakeMySQLApplyCommandWithFirstFailure(t *testing.T) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	callsPath := filepath.Join(dir, "mysql-apply-calls.txt")
	statePath := filepath.Join(dir, "mysql-exec-attempts.txt")
	commandPath := filepath.Join(dir, "mysql-apply")
	writeFile(t, commandPath, `#!/usr/bin/env bash
set -euo pipefail
printf 'apply\n' >> "$MYSQL_APPLY_CALLS_FILE"
attempts=0
if [[ -f "$MYSQL_EXEC_ATTEMPTS_FILE" ]]; then
  attempts=$(cat "$MYSQL_EXEC_ATTEMPTS_FILE")
fi
attempts=$((attempts + 1))
printf '%s\n' "$attempts" > "$MYSQL_EXEC_ATTEMPTS_FILE"
if [[ "$attempts" -eq 1 ]]; then
  printf "mysql: [Warning] Using a password on the command line interface can be insecure.\nERROR 1045 (28000): Access denied for user 'root'@'localhost' (using password: YES)\n" >&2
  exit 1
fi
cat >/dev/null
`)
	if err := os.Chmod(commandPath, 0o755); err != nil {
		t.Fatalf("chmod fake mysql apply command: %v", err)
	}
	t.Setenv("MYSQL_APPLY_CALLS_FILE", callsPath)
	t.Setenv("MYSQL_EXEC_ATTEMPTS_FILE", statePath)
	return []string{commandPath}, callsPath
}

func installGitRemoteFixture(t *testing.T, binDir string, remoteURL string, fixtureRepo string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find git: %v", err)
	}
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
remote_url=%q
fixture_repo=%q
args=()
for arg in "$@"; do
  if [[ "$arg" == "$remote_url" ]]; then
    args+=("$fixture_repo")
  else
    args+=("$arg")
  fi
done
exec %q "${args[@]}"
`, remoteURL, fixtureRepo, realGit)
	gitPath := filepath.Join(binDir, "git")
	writeFile(t, gitPath, script)
	if err := os.Chmod(gitPath, 0o755); err != nil {
		t.Fatalf("chmod fake git: %v", err)
	}
}
