package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
)

func TestEnvironmentRestoreBlocksDockerWhenContainerNamesAlreadyExist(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	installEnvironmentRestoreDockerTool(t, "#!/bin/sh\nif [ \"$1\" = compose ] && [ \"$2\" = version ]; then exit 0; fi\nif [ \"$1\" = ps ]; then printf 'sandbox-mysql\\n'; exit 0; fi\nexit 0\n")

	report, err := buildEnvironmentRestoreReport(context.Background(), store.Environment{
		ID:                     "env.container.conflict",
		ComposeJSON:            `{"composeFile":"compose.yml","generatedFiles":{"compose.yml":"services:\n  mysql:\n    image: mysql:8\n    container_name: sandbox-mysql\n"}}`,
		HealthChecksJSON:       `[{"kind":"url","url":"http://127.0.0.1:18080/health"}]`,
		VerificationWorkflowID: "workflow.core-10",
	}, workspace, false, false, false, time.Second, environmentRestoreWorkflowOptions{}, environmentRestoreDockerCleanupOptions{})
	if err != nil {
		t.Fatalf("build restore container conflict report: %v", err)
	}
	if report.OK || report.Preflight.OK || len(report.Preflight.ContainerConflicts) != 1 || report.Preflight.ContainerConflicts[0] != "sandbox-mysql" {
		t.Fatalf("container conflict report = %#v", report)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "docker-container-conflicts", false, "sandbox-mysql") {
		t.Fatalf("readiness should include container conflict: %#v", report.Readiness.Items)
	}
}

func TestEnvironmentRestoreAssumeCleanDockerIgnoresLocalContainerConflicts(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	installEnvironmentRestoreDockerTool(t, "#!/bin/sh\nif [ \"$1\" = compose ] && [ \"$2\" = version ]; then exit 0; fi\nif [ \"$1\" = ps ]; then printf 'sandbox-mysql\\n'; exit 0; fi\nexit 0\n")

	report, err := buildEnvironmentRestoreReport(context.Background(), store.Environment{
		ID:                     "env.clean-machine",
		ComposeJSON:            `{"composeFile":"compose.yml","generatedFiles":{"compose.yml":"services:\n  mysql:\n    image: mysql:8\n    container_name: sandbox-mysql\n"}}`,
		HealthChecksJSON:       `[{"kind":"url","url":"http://127.0.0.1:18080/health"}]`,
		VerificationWorkflowID: "workflow.core-10",
	}, workspace, false, false, false, time.Second, environmentRestoreWorkflowOptions{}, environmentRestoreDockerCleanupOptions{AssumeCleanDocker: true}, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "mysql", Kind: "middleware", Role: "database", ComposeService: "mysql", Required: true, HealthCheckJSON: `{"kind":"compose-service","service":"mysql"}`},
			{ComponentID: "gateway", Kind: "app", Role: "business-service", ComposeService: "gateway", Required: true, HealthCheckJSON: `{"kind":"url","url":"http://127.0.0.1:18080/health"}`},
		},
		Dependencies: []store.ComponentDependency{
			{ConsumerComponentID: "gateway", ProviderComponentID: "mysql", Required: true},
		},
		Assets: []store.ComponentConfigAsset{
			{OwnerComponentID: "mysql", AssetID: "mysql.schema", AssetKind: "mysql-ddl", TargetPath: "mysql/init/schema.sql", ContentInline: "create table demo(id bigint);"},
		},
	})
	if err != nil {
		t.Fatalf("build clean-machine restore report: %v", err)
	}
	if !report.OK || !report.Preflight.OK || !report.Preflight.AssumeCleanDocker || len(report.Preflight.ContainerConflicts) != 0 || report.Docker.Action != "plan-docker-compose" {
		t.Fatalf("clean-machine report should not be blocked by local containers: %#v", report)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "docker-container-conflicts", true, "clean-machine dry-run") {
		t.Fatalf("readiness should document clean-machine assumption: %#v", report.Readiness.Items)
	}
	if report.Readiness.Action != "ready-for-clean-machine-execute" || !strings.Contains(report.Readiness.NextStep, "--execute") {
		t.Fatalf("clean-machine readiness should point to execute: %#v", report.Readiness)
	}
	if len(report.NextActions) == 0 || !strings.Contains(report.NextActions[0], "colleague machine") {
		t.Fatalf("clean-machine next actions should point to colleague machine: %#v", report.NextActions)
	}
	if !report.CleanMachine.Ready || strings.Join(report.CleanMachine.ExecuteCommand, " ") != "agent-testbench environment restore env.clean-machine --store STORE_NAME_OR_SQL_DSN --workspace "+workspace+" --execute --json" {
		t.Fatalf("clean-machine execute command = %#v", report.CleanMachine)
	}
	if strings.Join(report.CleanMachine.PrepareCommand, " ") != "agent-testbench environment restore env.clean-machine --store STORE_NAME_OR_SQL_DSN --workspace "+workspace+" --execute --prepare-repos-only --json" {
		t.Fatalf("clean-machine prepare command = %#v", report.CleanMachine)
	}
	if !restoreCleanMachinePrereqOK(report.CleanMachine.Prerequisites, "tool:docker") || !restoreCleanMachinePrereqOK(report.CleanMachine.Prerequisites, "docker-start-plan") {
		t.Fatalf("clean-machine prerequisites = %#v", report.CleanMachine.Prerequisites)
	}
	if report.CleanMachine.Summary.Components != 2 || report.CleanMachine.Summary.StartupBatches != 2 || report.CleanMachine.Summary.HealthGates != 2 {
		t.Fatalf("clean-machine component summary = %#v", report.CleanMachine.Summary)
	}
	if report.CleanMachine.Summary.InlineAssetBytes == 0 || report.CleanMachine.Summary.GraphMetadataLimitBytes != store.ComponentGraphMaxBytes || report.CleanMachine.Summary.DockerImagesStored || report.CleanMachine.Summary.LargeBinariesStored {
		t.Fatalf("clean-machine storage summary = %#v", report.CleanMachine.Summary)
	}
}

func TestEnvironmentRestoreEffectiveHealthChecksUseStartedComposeServices(t *testing.T) {
	checks := []any{
		map[string]any{"id": "llt-url", "kind": "url", "url": "http://127.0.0.1:28080/health"},
	}
	compose := map[string]any{"services": []any{"app", "db"}}
	graph := store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "app", ComposeService: "app", HealthCheckJSON: `{"type":"compose-service","service":"app"}`},
			{ComponentID: "demo", ComposeService: "demo", HealthCheckJSON: `{"type":"compose-service","service":"demo"}`},
			{ComponentID: "db", ComposeService: "db", HealthCheckJSON: `{"type":"compose-service","service":"db"}`},
		},
	}
	effective := environmentRestoreEffectiveHealthChecks(checks, compose, graph)
	if !restoreHealthChecksContain(effective, "url", "", "http://127.0.0.1:28080/health") {
		t.Fatalf("explicit URL health check missing: %#v", effective)
	}
	if !restoreHealthChecksContain(effective, "compose-service", "app", "") || !restoreHealthChecksContain(effective, "compose-service", "db", "") {
		t.Fatalf("started service health checks missing: %#v", effective)
	}
	if restoreHealthChecksContain(effective, "compose-service", "demo", "") {
		t.Fatalf("unstarted component health check should be excluded: %#v", effective)
	}
}

func TestEnvironmentRestoreEffectiveHealthChecksCoverBusinessURLService(t *testing.T) {
	compose := map[string]any{"services": []any{"app"}}
	graph := store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{
				ComponentID:     "app",
				Kind:            "app",
				Role:            "business-service",
				ComposeService:  "app",
				HealthCheckJSON: `{"type":"url","url":"http://127.0.0.1:18080/actuator/health"}`,
			},
		},
	}
	effective := environmentRestoreEffectiveHealthChecks(nil, compose, graph)
	if !restoreHealthChecksContain(effective, "url", "app", "http://127.0.0.1:18080/actuator/health") {
		t.Fatalf("business URL health check missing service binding: %#v", effective)
	}
	if restoreHealthChecksContain(effective, "compose-service", "app", "") {
		t.Fatalf("business service with URL health should not add compose-only health: %#v", effective)
	}
}

func TestEnvironmentRestoreCanAdoptExistingContainers(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	installEnvironmentRestoreDockerTool(t, "#!/bin/sh\nif [ \"$1\" = compose ] && [ \"$2\" = version ]; then exit 0; fi\nif [ \"$1\" = ps ]; then printf 'sandbox-mysql\\n'; exit 0; fi\nif [ \"$1\" = inspect ]; then printf 'running healthy\\n'; exit 0; fi\nexit 0\n")

	report, err := buildEnvironmentRestoreReport(context.Background(), store.Environment{
		ID:                     "env.adopt.container",
		ComposeJSON:            `{"composeFile":"compose.yml","services":["mysql"],"generatedFiles":{"compose.yml":"services:\n  mysql:\n    image: mysql:8\n    container_name: sandbox-mysql\n"}}`,
		HealthChecksJSON:       `[]`,
		VerificationWorkflowID: "workflow.core-10",
	}, workspace, true, false, false, time.Second, environmentRestoreWorkflowOptions{}, environmentRestoreDockerCleanupOptions{
		UseExistingContainers: true,
	})
	if err != nil {
		t.Fatalf("build restore adopt existing container report: %v", err)
	}
	if !report.OK || !report.Preflight.OK || report.Docker.Action != "use-existing-containers" || len(report.Docker.Commands) != 0 || len(report.Docker.HealthChecks) != 1 || !report.Docker.HealthChecks[0].OK || report.Docker.HealthChecks[0].Container != "sandbox-mysql" {
		t.Fatalf("adopt existing container report = %#v", report)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "docker-container-conflicts", true, "explicitly adopted") {
		t.Fatalf("readiness should acknowledge explicit adoption: %#v", report.Readiness.Items)
	}
	if _, err := os.Stat(filepath.Join(workspace, "compose.yml")); err != nil {
		t.Fatalf("adopt existing containers should write Store startup file: %v", err)
	}
}
