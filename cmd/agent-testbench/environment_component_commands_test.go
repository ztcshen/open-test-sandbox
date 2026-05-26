package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/store"
)

type environmentComponentReadinessFixture struct {
	storePath string
	graphPath string
	envID     string
}

func writeEnvironmentComponentReadinessFixture(t *testing.T, envID string, includeAsset bool) environmentComponentReadinessFixture {
	t.Helper()

	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	graphPath := filepath.Join(t.TempDir(), "graph.json")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", envID,
		"--start-command", "true",
		"--verification-workflow", "workflow.core-10",
	)
	graph := store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "db", Kind: "middleware", Role: "database", ComposeService: "db", Required: true, HealthCheckJSON: `{"type":"compose-service"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
			{ComponentID: "app", Kind: "app", Role: "business-service", ComposeService: "app", Required: true, HealthCheckJSON: `{"type":"url","url":"http://127.0.0.1:18080/health"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
		},
		Dependencies: []store.ComponentDependency{
			{ConsumerComponentID: "app", ProviderComponentID: "db", Phase: "startup", Capability: "sql", Required: true, ProfileJSON: `{}`},
		},
	}
	if includeAsset {
		graph.Assets = []store.ComponentConfigAsset{
			{OwnerComponentID: "app", AssetID: "app.schema", AssetKind: "mysql-ddl", TargetComponentID: "db", TargetPath: "compose/mysql/init/app.sql", ContentInline: "create database app;\n", ApplyOrder: 10, SummaryJSON: `{}`},
		}
	}
	writeFile(t, graphPath, mustJSON(t, graph))
	return environmentComponentReadinessFixture{storePath: storePath, graphPath: graphPath, envID: envID}
}

func TestEnvironmentComponentsReplaceRejectsBlockingDependencyCycle(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	graphPath := filepath.Join(t.TempDir(), "graph.json")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.component.replace-cycle",
		"--start-command", "true",
		"--verification-workflow", "workflow.core-10",
	)
	writeFile(t, graphPath, mustJSON(t, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "app.a", Kind: "app", Role: "business-service", ComposeService: "app-a", Required: true, HealthCheckJSON: `{"type":"url","url":"http://127.0.0.1:18081/health"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
			{ComponentID: "app.b", Kind: "app", Role: "business-service", ComposeService: "app-b", Required: true, HealthCheckJSON: `{"type":"url","url":"http://127.0.0.1:18082/health"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
		},
		Dependencies: []store.ComponentDependency{
			{ConsumerComponentID: "app.a", ProviderComponentID: "app.b", Phase: "startup", Capability: "http", Required: true, ProfileJSON: `{}`},
			{ConsumerComponentID: "app.b", ProviderComponentID: "app.a", Phase: "startup", Capability: "http", Required: true, ProfileJSON: `{}`},
		},
	}))
	out := runCLIFails(t, "environment", "components", "replace", "--store", "sqlite://"+storePath, "--file", graphPath, "env.component.replace-cycle")
	if !strings.Contains(out, "component graph restore readiness failed") || !strings.Contains(out, "cycle") || !strings.Contains(out, "app.a") || !strings.Contains(out, "app.b") {
		t.Fatalf("replace cycle failure output = %q", out)
	}
}

func TestEnvironmentComponentsReplaceRejectsInvalidComponentHealthCheck(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	graphPath := filepath.Join(t.TempDir(), "graph.json")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.component.replace-health",
		"--start-command", "true",
		"--verification-workflow", "workflow.core-10",
	)
	writeFile(t, graphPath, mustJSON(t, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "app", Kind: "app", Role: "business-service", Required: true, HealthCheckJSON: `{"kind":"url"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
		},
	}))
	out := runCLIFails(t, "environment", "components", "replace", "--store", "sqlite://"+storePath, "--file", graphPath, "env.component.replace-health")
	if !strings.Contains(out, "component graph restore readiness failed") || !strings.Contains(out, "url health check requires url") {
		t.Fatalf("replace invalid health failure output = %q", out)
	}
}

func TestEnvironmentComponentsReplaceRejectsRemoteComponentAssetWithoutURLPath(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	graphPath := filepath.Join(t.TempDir(), "graph.json")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.component.replace-remote-asset",
		"--start-command", "true",
		"--verification-workflow", "workflow.core-10",
	)
	writeFile(t, graphPath, mustJSON(t, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "app", Kind: "app", Role: "business-service", ComposeService: "app", Required: true, HealthCheckJSON: `{"type":"url","url":"http://127.0.0.1:18080/health"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
		},
		Assets: []store.ComponentConfigAsset{
			{OwnerComponentID: "app", AssetID: "app.remote-ddl", AssetKind: "mysql-ddl", TargetPath: "compose/mysql/init/app.sql", RemoteRefJSON: `{"path":"compose/mysql/init/app.sql"}`, SizeBytes: 48 * 1024, SummaryJSON: `{}`},
		},
	}))
	out := runCLIFails(t, "environment", "components", "replace", "--store", "sqlite://"+storePath, "--file", graphPath, "env.component.replace-remote-asset")
	if !strings.Contains(out, "component graph restore readiness failed") || !strings.Contains(out, "remote Git URL/path") {
		t.Fatalf("replace invalid remote asset output = %q", out)
	}
}

func TestEnvironmentComponentsInspectReportsRestoreReadiness(t *testing.T) {
	fixture := writeEnvironmentComponentReadinessFixture(t, "env.component.inspect-readiness", true)
	replaceOut := runCLI(t, "environment", "components", "replace", "--store", "sqlite://"+fixture.storePath, "--file", fixture.graphPath, "--json", fixture.envID)
	inspectOut := runCLI(t, "environment", "components", "inspect", "--store", "sqlite://"+fixture.storePath, "--json", fixture.envID)
	documentedReplaceOut := runCLI(t, "environment", "components", "replace", fixture.envID, "--store", "sqlite://"+fixture.storePath, "--file", fixture.graphPath, "--json")
	documentedInspectOut := runCLI(t, "environment", "components", "inspect", fixture.envID, "--store", "sqlite://"+fixture.storePath, "--json")
	for _, out := range []string{replaceOut, inspectOut, documentedReplaceOut, documentedInspectOut} {
		var payload struct {
			ComponentGraph struct {
				RestoreReadiness struct {
					OK                   bool     `json:"ok"`
					BlockingDependencies int      `json:"blockingDependencies"`
					Assets               int      `json:"assets"`
					BlockingOrder        []string `json:"blockingOrder"`
				} `json:"restoreReadiness"`
			} `json:"componentGraph"`
		}
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("decode components readiness json: %v\n%s", err, out)
		}
		readiness := payload.ComponentGraph.RestoreReadiness
		if !readiness.OK || readiness.BlockingDependencies != 1 || readiness.Assets != 1 || strings.Join(readiness.BlockingOrder, ",") != "db,app" {
			t.Fatalf("components readiness payload = %#v\n%s", readiness, out)
		}
	}
}

func TestEnvironmentInspectReportsComponentGraphReadiness(t *testing.T) {
	fixture := writeEnvironmentComponentReadinessFixture(t, "env.inspect.component-readiness", false)
	runCLI(t, "environment", "components", "replace", "--store", "sqlite://"+fixture.storePath, "--file", fixture.graphPath, fixture.envID)
	out := runCLI(t, "environment", "inspect", "--store", "sqlite://"+fixture.storePath, "--json", fixture.envID)
	var payload struct {
		ComponentGraph struct {
			OK                   bool     `json:"ok"`
			Components           int      `json:"components"`
			BlockingDependencies int      `json:"blockingDependencies"`
			BlockingOrder        []string `json:"blockingOrder"`
		} `json:"componentGraph"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode environment inspect component readiness json: %v\n%s", err, out)
	}
	if !payload.ComponentGraph.OK || payload.ComponentGraph.Components != 2 || payload.ComponentGraph.BlockingDependencies != 1 || strings.Join(payload.ComponentGraph.BlockingOrder, ",") != "db,app" {
		t.Fatalf("environment inspect component readiness = %#v", payload.ComponentGraph)
	}
}

func TestEnvironmentBootstrapReportsComponentGraphReadiness(t *testing.T) {
	storePath := seedEnvironmentBootstrapComponentReadiness(t)
	payload := runEnvironmentBootstrapComponentReadinessJSON(t, storePath)
	requireEnvironmentBootstrapComponentReadiness(t, payload)
}

type environmentBootstrapComponentReadinessPayload struct {
	Plan struct {
		ComponentGraph struct {
			OK                   bool     `json:"ok"`
			BlockingDependencies int      `json:"blockingDependencies"`
			BlockingOrder        []string `json:"blockingOrder"`
		} `json:"componentGraph"`
		ComponentStartupPlan struct {
			OK      bool `json:"ok"`
			Batches []struct {
				Components []struct {
					ComponentID string `json:"componentId"`
				} `json:"components"`
			} `json:"batches"`
			HealthGates []struct {
				ComponentID string `json:"componentId"`
			} `json:"healthGates"`
		} `json:"componentStartupPlan"`
		Restore struct {
			ComponentGraph struct {
				OK            bool     `json:"ok"`
				BlockingOrder []string `json:"blockingOrder"`
			} `json:"componentGraph"`
			ComponentStartupPlan struct {
				OK bool `json:"ok"`
			} `json:"componentStartupPlan"`
		} `json:"restore"`
	} `json:"plan"`
}

func seedEnvironmentBootstrapComponentReadiness(t *testing.T) string {
	t.Helper()

	fixture := writeEnvironmentComponentReadinessFixture(t, "env.component.bootstrap-readiness", false)
	runCLI(t, "environment", "components", "replace", "--store", "sqlite://"+fixture.storePath, "--file", fixture.graphPath, fixture.envID)
	return fixture.storePath
}

func runEnvironmentBootstrapComponentReadinessJSON(t *testing.T, storePath string) environmentBootstrapComponentReadinessPayload {
	t.Helper()

	out := runCLI(t, "environment", "bootstrap", "--store", "sqlite://"+storePath, "--json", "env.component.bootstrap-readiness")
	var payload environmentBootstrapComponentReadinessPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode bootstrap component readiness json: %v\n%s", err, out)
	}
	return payload
}

func requireEnvironmentBootstrapComponentReadiness(t *testing.T, payload environmentBootstrapComponentReadinessPayload) {
	t.Helper()

	if !payload.Plan.ComponentGraph.OK || payload.Plan.ComponentGraph.BlockingDependencies != 1 || strings.Join(payload.Plan.ComponentGraph.BlockingOrder, ",") != "db,app" {
		t.Fatalf("bootstrap component graph readiness = %#v", payload.Plan.ComponentGraph)
	}
	if !payload.Plan.Restore.ComponentGraph.OK || strings.Join(payload.Plan.Restore.ComponentGraph.BlockingOrder, ",") != "db,app" {
		t.Fatalf("bootstrap restore component graph readiness = %#v", payload.Plan.Restore.ComponentGraph)
	}
	if !payload.Plan.ComponentStartupPlan.OK || len(payload.Plan.ComponentStartupPlan.Batches) != 2 || payload.Plan.ComponentStartupPlan.Batches[0].Components[0].ComponentID != "db" || payload.Plan.ComponentStartupPlan.Batches[1].Components[0].ComponentID != "app" || len(payload.Plan.ComponentStartupPlan.HealthGates) != 2 {
		t.Fatalf("bootstrap component startup plan = %#v", payload.Plan.ComponentStartupPlan)
	}
	if !payload.Plan.Restore.ComponentStartupPlan.OK {
		t.Fatalf("bootstrap restore component startup plan = %#v", payload.Plan.Restore.ComponentStartupPlan)
	}
}

func TestEnvironmentStartupFilePutMergesGeneratedFilesWithoutReRegistering(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	sourceCompose := filepath.Join(t.TempDir(), "source-compose.yml")
	writeFile(t, sourceCompose, "services:\n  generated-service:\n    image: alpine:3.20\n")
	runCLI(t, "environment", "register",
		"--store", "sqlite://"+storePath,
		"--id", "env.startup.files",
		"--repo", "entry-gateway=https://example.com/team/entry-gateway.git",
		"--checkout", "entry-gateway=services/entry-gateway",
		"--compose-file", "compose/docker-compose.yml",
		"--health-url", newHealthyTestURL(t),
		"--verification-workflow", "workflow.core-10",
	)

	out := runCLI(t, "environment", "startup-file", "put",
		"--store", "sqlite://"+storePath,
		"--file", "compose/docker-compose.yml="+sourceCompose,
		"--json",
		"env.startup.files",
	)
	var payload struct {
		GeneratedFiles []struct {
			Path  string `json:"path"`
			Bytes int    `json:"bytes"`
		} `json:"generatedFiles"`
		Environment struct {
			Repos   map[string]any `json:"repos"`
			Compose struct {
				GeneratedFiles map[string]string `json:"generatedFiles"`
			} `json:"compose"`
			Summary struct {
				StartupFiles struct {
					Files []struct {
						Path string `json:"path"`
					} `json:"files"`
				} `json:"startupFiles"`
			} `json:"summary"`
		} `json:"environment"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode startup-file put json: %v\n%s", err, out)
	}
	if len(payload.GeneratedFiles) != 1 || payload.GeneratedFiles[0].Path != "compose/docker-compose.yml" || payload.GeneratedFiles[0].Bytes == 0 {
		t.Fatalf("startup-file payload = %#v", payload.GeneratedFiles)
	}
	if payload.Environment.Repos["entry-gateway"] == nil {
		t.Fatalf("startup-file put should preserve existing repositories: %#v", payload.Environment.Repos)
	}
	if !strings.Contains(payload.Environment.Compose.GeneratedFiles["compose/docker-compose.yml"], "generated-service") {
		t.Fatalf("generated file was not stored in compose metadata: %#v", payload.Environment.Compose.GeneratedFiles)
	}
	if len(payload.Environment.Summary.StartupFiles.Files) != 1 || payload.Environment.Summary.StartupFiles.Files[0].Path != "compose/docker-compose.yml" {
		t.Fatalf("startup-file summary = %#v", payload.Environment.Summary.StartupFiles)
	}
}
