package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionPrintsShellScripts(t *testing.T) {
	bash := runCLI(t, "completion", "bash")
	if !strings.Contains(bash, "complete -W") || !strings.Contains(bash, "agent-testbench") {
		t.Fatalf("bash completion script = %q", bash)
	}
	zsh := runCLI(t, "completion", "zsh")
	if !strings.Contains(zsh, "#compdef agent-testbench") || !strings.Contains(zsh, "_arguments") {
		t.Fatalf("zsh completion script = %q", zsh)
	}
}

func TestLogsListAndTailRuntimeLogs(t *testing.T) {
	repo := t.TempDir()
	logDir := filepath.Join(repo, ".runtime", "logs")
	writeFile(t, filepath.Join(logDir, "agent-testbench.log"), "one\ntwo\nthree\n")

	out := runCLIWithEnv(t, []string{"AGENT_TESTBENCH_REPO=" + repo}, "logs", "--json")
	var report struct {
		OK   bool `json:"ok"`
		Logs []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"logs"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode logs report: %v\n%s", err, out)
	}
	if !report.OK || len(report.Logs) != 1 || report.Logs[0].Name != "agent-testbench" {
		t.Fatalf("logs report = %#v", report)
	}

	tail := runCLIWithEnv(t, []string{"AGENT_TESTBENCH_REPO=" + repo}, "logs", "agent-testbench", "-n", "2")
	if strings.Contains(tail, "one") || !strings.Contains(tail, "two") || !strings.Contains(tail, "three") {
		t.Fatalf("logs tail output = %q", tail)
	}
}

func TestConfigShowAndPath(t *testing.T) {
	configHome := t.TempDir()
	runCLIWithEnv(t, []string{"AGENT_TESTBENCH_CONFIG_HOME=" + configHome}, "store", "config", "set", "local", "--url", "sqlite://"+filepath.Join(configHome, "local.sqlite"))
	runCLIWithEnv(t, []string{"AGENT_TESTBENCH_CONFIG_HOME=" + configHome}, "store", "use", "local")

	pathOut := runCLIWithEnv(t, []string{"AGENT_TESTBENCH_CONFIG_HOME=" + configHome}, "config", "path")
	if !strings.Contains(pathOut, filepath.Join(configHome, "store-config.json")) {
		t.Fatalf("config path output = %q", pathOut)
	}
	showOut := runCLIWithEnv(t, []string{"AGENT_TESTBENCH_CONFIG_HOME=" + configHome}, "config", "show", "--json")
	if !strings.Contains(showOut, `"active": "local"`) || !strings.Contains(showOut, `"backend": "sqlite"`) {
		t.Fatalf("config show output = %s", showOut)
	}
}

func TestMainHelpIncludesP2OperatorCommands(t *testing.T) {
	out := runCLI(t)
	for _, want := range []string{"agent-testbench setup", "agent-testbench completion", "agent-testbench logs", "agent-testbench config show"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestLogsRejectsPathTraversal(t *testing.T) {
	repo := t.TempDir()
	if out := runCLIFailsWithEnv(t, []string{"AGENT_TESTBENCH_REPO=" + repo}, "logs", "../secret"); !strings.Contains(out, "log name must not contain path separators") {
		t.Fatalf("path traversal error = %q", out)
	}
}

func TestConfigEditRequiresEditor(t *testing.T) {
	configHome := t.TempDir()
	out := runCLIFailsWithEnv(t, []string{"AGENT_TESTBENCH_CONFIG_HOME=" + configHome, "EDITOR="}, "config", "edit")
	if !strings.Contains(out, "EDITOR") {
		t.Fatalf("config edit should explain EDITOR requirement: %q", out)
	}
}

func TestLogsJSONTail(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".runtime", "logs", "restore.log"), "a\nb\nc\n")

	out := runCLIWithEnv(t, []string{"AGENT_TESTBENCH_REPO=" + repo}, "logs", "restore", "-n", "1", "--json")
	var report struct {
		OK    bool     `json:"ok"`
		Name  string   `json:"name"`
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode logs tail json: %v\n%s", err, out)
	}
	if !report.OK || report.Name != "restore" || len(report.Lines) != 1 || report.Lines[0] != "c" {
		t.Fatalf("logs tail json = %#v", report)
	}
}

func TestConfigEditRunsEditor(t *testing.T) {
	configHome := t.TempDir()
	dir := t.TempDir()
	editorPath := filepath.Join(dir, "editor")
	callsPath := filepath.Join(dir, "editor-calls.txt")
	writeFile(t, editorPath, "#!/bin/sh\nprintf '%s\\n' \"$1\" > \"$EDITOR_CALLS\"\n")
	if err := os.Chmod(editorPath, 0o755); err != nil {
		t.Fatalf("chmod editor: %v", err)
	}

	runCLIWithEnv(t, []string{"AGENT_TESTBENCH_CONFIG_HOME=" + configHome, "EDITOR=" + editorPath, "EDITOR_CALLS=" + callsPath}, "config", "edit")
	raw, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatalf("read editor calls: %v", err)
	}
	if !strings.Contains(string(raw), filepath.Join(configHome, "store-config.json")) {
		t.Fatalf("editor called with %q", raw)
	}
}
