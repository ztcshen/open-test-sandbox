package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-testbench/internal/store"
)

type environmentRestoreReadinessSQLBackend struct {
	name     string
	storeURL string
}

func environmentRestoreReadinessProductStoreBackends() []environmentRestoreReadinessSQLBackend {
	return []environmentRestoreReadinessSQLBackend{
		{name: "postgres", storeURL: "postgres://tester@127.0.0.1:5432/agent_testbench?sslmode=disable"},
		{name: "mysql", storeURL: "mysql://tester:secret@127.0.0.1:3306/agent_testbench?tls=false"},
	}
}

func environmentRestoreReadinessProductStoreURLs() []string {
	return []string{
		"postgres://tester@127.0.0.1:5432/agent_testbench?sslmode=disable",
		"postgresql://tester@127.0.0.1:5432/agent_testbench?sslmode=disable",
		"mysql://tester:secret@127.0.0.1:3306/agent_testbench?tls=false",
	}
}

func environmentRestoreReadinessCompatibilityStoreURLs() []string {
	return []string{"", "sqlite:///tmp/agent-testbench.sqlite", "file:///tmp/agent-testbench.sqlite"}
}

func newEnvironmentRestoreReadinessEnv(id string, composeJSON string, healthChecksJSON string) store.Environment {
	return store.Environment{
		ID:                     id,
		ComposeJSON:            composeJSON,
		HealthChecksJSON:       healthChecksJSON,
		VerificationWorkflowID: "workflow.core-10",
	}
}

func buildEnvironmentRestoreReadinessReport(t *testing.T, env store.Environment, workspace string, workflowOptions environmentRestoreWorkflowOptions, componentGraphs ...store.EnvironmentComponentGraph) environmentRestoreReport {
	t.Helper()
	return buildEnvironmentRestoreReadinessReportWithMode(t, env, workspace, false, false, false, workflowOptions, componentGraphs...)
}

func buildEnvironmentRestoreReadinessReportWithMode(t *testing.T, env store.Environment, workspace string, execute bool, pull bool, prepareReposOnly bool, workflowOptions environmentRestoreWorkflowOptions, componentGraphs ...store.EnvironmentComponentGraph) environmentRestoreReport {
	t.Helper()
	report, err := buildEnvironmentRestoreReport(context.Background(), env, workspace, execute, pull, prepareReposOnly, time.Second, workflowOptions, environmentRestoreDockerCleanupOptions{}, componentGraphs...)
	if err != nil {
		t.Fatalf("build %s restore readiness report: %v", env.ID, err)
	}
	return report
}

func buildEnvironmentRestoreSQLPolicyReport(t *testing.T, backend environmentRestoreReadinessSQLBackend, env store.Environment) environmentRestoreReport {
	t.Helper()
	workspace := filepath.Join(t.TempDir(), "workspace")
	return buildEnvironmentRestoreReadinessReport(t, env, workspace, environmentRestoreWorkflowOptions{StoreURL: backend.storeURL})
}

func buildEnvironmentRestoreSQLReadinessReport(t *testing.T, backend environmentRestoreReadinessSQLBackend, env store.Environment, componentGraphs ...store.EnvironmentComponentGraph) environmentRestoreReport {
	t.Helper()
	installEnvironmentRestoreReadinessFakeGitDocker(t)
	workspace := filepath.Join(t.TempDir(), "workspace")
	return buildEnvironmentRestoreReadinessReport(t, env, workspace, environmentRestoreWorkflowOptions{StoreURL: backend.storeURL}, componentGraphs...)
}

func installEnvironmentRestoreReadinessFakeGitDocker(t *testing.T) {
	t.Helper()
	fakeBin := t.TempDir()
	scripts := []struct {
		name string
		body string
	}{
		{name: "git", body: "#!/bin/sh\nexit 0\n"},
		{name: "docker", body: "#!/bin/sh\nif [ \"$1\" = compose ] && [ \"$2\" = version ]; then exit 0; fi\nexit 0\n"},
	}
	for _, script := range scripts {
		path := filepath.Join(fakeBin, script.name)
		writeFile(t, path, script.body)
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatalf("chmod fake %s: %v", script.name, err)
		}
	}
	t.Setenv("PATH", fakeBin)
}

func environmentRestoreReadinessURLHealth(url string) string {
	return `{"type":"url","url":"` + url + `"}`
}

func environmentRestoreReadinessComposeHealth(service string) string {
	if service == "" {
		return `{"type":"compose-service"}`
	}
	return `{"type":"compose-service","service":"` + service + `"}`
}

func environmentRestoreReadinessComponent(id string, kind string, role string, composeService string, healthCheckJSON string) store.EnvironmentComponent {
	return store.EnvironmentComponent{
		ComponentID:     id,
		Kind:            kind,
		Role:            role,
		ComposeService:  composeService,
		Required:        true,
		RuntimeJSON:     `{}`,
		HealthCheckJSON: healthCheckJSON,
		SummaryJSON:     `{}`,
	}
}

func environmentRestoreReadinessAppComponent(id string, composeService string, healthURL string) store.EnvironmentComponent {
	return environmentRestoreReadinessComponent(id, "app", "business-service", composeService, environmentRestoreReadinessURLHealth(healthURL))
}

func environmentRestoreReadinessInlineAsset(ownerID string, assetID string, kind string, targetID string, targetPath string, content string, applyOrder int) store.ComponentConfigAsset {
	return store.ComponentConfigAsset{
		OwnerComponentID:  ownerID,
		AssetID:           assetID,
		AssetKind:         kind,
		TargetComponentID: targetID,
		TargetPath:        targetPath,
		ContentInline:     content,
		SizeBytes:         int64(len(content)),
		ApplyOrder:        applyOrder,
		SummaryJSON:       `{}`,
	}
}

func environmentRestoreReadinessRemoteAsset(ownerID string, assetID string, kind string, targetPath string, remoteRefJSON string, applyOrder int) store.ComponentConfigAsset {
	return store.ComponentConfigAsset{
		OwnerComponentID: ownerID,
		AssetID:          assetID,
		AssetKind:        kind,
		TargetPath:       targetPath,
		RemoteRefJSON:    remoteRefJSON,
		SizeBytes:        48 * 1024,
		ApplyOrder:       applyOrder,
		SummaryJSON:      `{}`,
	}
}

func buildEnvironmentRestoreComponentReadinessReport(t *testing.T, id string, healthChecksJSON string, graph store.EnvironmentComponentGraph) environmentRestoreReport {
	t.Helper()
	workspace := filepath.Join(t.TempDir(), "workspace")
	env := newEnvironmentRestoreReadinessEnv(id, `{"startCommand":"true"}`, healthChecksJSON)
	return buildEnvironmentRestoreReadinessReport(t, env, workspace, environmentRestoreWorkflowOptions{}, graph)
}
