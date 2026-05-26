package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type environmentRestoreDockerCLIFixture struct {
	StorePath       string
	StoreDSN        string
	Workspace       string
	DockerEnv       []string
	DockerCallsPath string
}

func newEnvironmentRestoreDockerCLIFixture(t *testing.T) environmentRestoreDockerCLIFixture {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	workspace := filepath.Join(t.TempDir(), "workspace")
	fakeDockerEnv, dockerCallsPath := fakeDockerCommand(t)
	return environmentRestoreDockerCLIFixture{
		StorePath:       storePath,
		StoreDSN:        "sqlite://" + storePath,
		Workspace:       workspace,
		DockerEnv:       fakeDockerEnv,
		DockerCallsPath: dockerCallsPath,
	}
}

func (fixture environmentRestoreDockerCLIFixture) writeWorkspaceFile(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(fixture.Workspace, name)
	writeFile(t, path, content)
	return path
}

func installEnvironmentRestoreDockerTool(t *testing.T, dockerScript string) {
	t.Helper()
	fakeBin := t.TempDir()
	writeFile(t, filepath.Join(fakeBin, "git"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(fakeBin, "docker"), dockerScript)
	if err := os.Chmod(filepath.Join(fakeBin, "git"), 0o755); err != nil {
		t.Fatalf("chmod fake git: %v", err)
	}
	if err := os.Chmod(filepath.Join(fakeBin, "docker"), 0o755); err != nil {
		t.Fatalf("chmod fake docker: %v", err)
	}
	t.Setenv("PATH", fakeBin)
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

func commandSlicesContain(commands [][]string, part string) bool {
	for _, command := range commands {
		for _, item := range command {
			if item == part {
				return true
			}
		}
	}
	return false
}

func restoreCleanMachinePrereqOK(items []environmentRestoreCleanMachinePrerequisite, name string) bool {
	for _, item := range items {
		if item.Name == name && item.OK {
			return true
		}
	}
	return false
}

func restoreHealthChecksContain(items []any, kind string, service string, url string) bool {
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(valueString(item["kind"])) != kind {
			continue
		}
		if service != "" && strings.TrimSpace(valueString(item["service"])) != service {
			continue
		}
		if url != "" && strings.TrimSpace(valueString(item["url"])) != url {
			continue
		}
		return true
	}
	return false
}
