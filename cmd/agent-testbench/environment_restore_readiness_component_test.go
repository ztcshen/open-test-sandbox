package main

import (
	"strings"
	"testing"

	"agent-testbench/internal/store"
)

func TestEnvironmentRestoreReportsComponentGraphReadiness(t *testing.T) {
	report := buildEnvironmentRestoreComponentReadinessReport(t, "env.component.graph", `[{"kind":"url","url":"http://127.0.0.1:18080/health"}]`, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			environmentRestoreReadinessComponent("mysql", "middleware", "database", "mysql", environmentRestoreReadinessComposeHealth("mysql")),
			environmentRestoreReadinessAppComponent("service.alpha", "service-alpha", "http://127.0.0.1:18080/service-alpha/health"),
		},
		Dependencies: []store.ComponentDependency{
			{
				ConsumerComponentID: "service.alpha",
				ProviderComponentID: "mysql",
				Phase:               "startup",
				Capability:          "sql",
				Required:            true,
				ProfileJSON:         `{}`,
			},
		},
		Assets: []store.ComponentConfigAsset{
			environmentRestoreReadinessInlineAsset("service.alpha", "service.alpha.mysql.ddl", "mysql-ddl", "mysql", "compose/mysql/init/service-alpha.sql", "create table service_alpha_smoke (id bigint primary key);", 0),
		},
	})
	if !report.ComponentGraph.Configured || !report.ComponentGraph.OK || report.ComponentGraph.Components != 2 || report.ComponentGraph.BlockingDependencies != 1 || report.ComponentGraph.Assets != 1 || report.ComponentGraph.MissingHealthChecks != 0 {
		t.Fatalf("component graph report = %#v", report.ComponentGraph)
	}
	if strings.Join(report.ComponentGraph.BlockingOrder, ",") != "mysql,service.alpha" {
		t.Fatalf("blocking dependency order = %#v", report.ComponentGraph.BlockingOrder)
	}
	if !report.ComponentStartupPlan.OK || len(report.ComponentStartupPlan.Batches) != 2 || len(report.ComponentStartupPlan.HealthGates) != 2 {
		t.Fatalf("component startup plan = %#v", report.ComponentStartupPlan)
	}
	if got := report.ComponentStartupPlan.Batches[0].Components[0].ComponentID + "," + report.ComponentStartupPlan.Batches[1].Components[0].ComponentID; got != "mysql,service.alpha" {
		t.Fatalf("component startup batches = %s plan=%#v", got, report.ComponentStartupPlan)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "component-graph", true, "2 component(s)") {
		t.Fatalf("readiness should include component graph item: %#v", report.Readiness.Items)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "component-startup-plan", true, "2 startup batch") {
		t.Fatalf("readiness should include component startup plan item: %#v", report.Readiness.Items)
	}
}

func TestEnvironmentRestoreRejectsRequiredComposeServiceGaps(t *testing.T) {
	compose := `{"composeFile":"compose/docker-compose.yml","composeFiles":["compose/docker-compose.yml"],"services":["mysql","service-alpha"],"generatedFiles":{"compose/docker-compose.yml":"services:\n  mysql:\n    image: mysql:8\n  service-alpha:\n    image: alpine:3.20\n"}}`
	env := newEnvironmentRestoreReadinessEnv("env.component.compose-gap", compose, `[]`)
	report := buildEnvironmentRestoreReadinessReport(t, env, t.TempDir(), environmentRestoreWorkflowOptions{}, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			environmentRestoreReadinessComponent("mysql", "middleware", "database", "mysql", environmentRestoreReadinessComposeHealth("mysql")),
			environmentRestoreReadinessAppComponent("service.alpha", "service-alpha", "http://127.0.0.1:18080/service-alpha/health"),
			environmentRestoreReadinessAppComponent("service.beta", "service-beta", "http://127.0.0.1:18081/service-beta/health"),
		},
	})
	if report.OK || report.Readiness.OK {
		t.Fatalf("required compose service gap should fail readiness: %#v", report.Readiness)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "compose-services-and-middleware", false, "service-beta") {
		t.Fatalf("readiness should name the missing required compose service: %#v", report.Readiness.Items)
	}
}

func TestEnvironmentRestoreRequiresComponentGraphForSQLOneClick(t *testing.T) {
	for _, backend := range environmentRestoreReadinessProductStoreBackends() {
		t.Run(backend.name, func(t *testing.T) {
			env := newEnvironmentRestoreReadinessEnv(
				"env."+backend.name+".component.required",
				`{"startCommand":"true"}`,
				`[{"kind":"url","url":"http://127.0.0.1:18080/health"}]`,
			)
			report := buildEnvironmentRestoreSQLPolicyReport(t, backend, env)
			if report.OK || report.Readiness.OK || report.ComponentGraph.Configured {
				t.Fatalf("%s restore without component graph should fail readiness: %#v", backend.name, report)
			}
			if !restoreTypedReadinessHasItem(report.Readiness.Items, "component-graph", false, "requires a Store component graph") {
				t.Fatalf("%s readiness should require component graph: %#v", backend.name, report.Readiness.Items)
			}
		})
	}
}

func TestEnvironmentRestoreRejectsBlockingComponentDependencyCycle(t *testing.T) {
	report := buildEnvironmentRestoreComponentReadinessReport(t, "env.component.cycle", `[]`, environmentRestoreReadinessTwoAppGraph("startup"))
	if report.OK || report.ComponentGraph.OK || len(report.ComponentGraph.BlockingCycles) == 0 {
		t.Fatalf("blocking dependency cycle should fail restore graph: %#v", report.ComponentGraph)
	}
	if !strings.Contains(report.ComponentGraph.Error, "cycle") || !strings.Contains(report.ComponentGraph.Error, "app.a") || !strings.Contains(report.ComponentGraph.Error, "app.b") {
		t.Fatalf("cycle error should name the component path: %q", report.ComponentGraph.Error)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "component-graph", false, "cycle") {
		t.Fatalf("readiness should include component cycle failure: %#v", report.Readiness.Items)
	}
}

func TestEnvironmentRestoreAllowsRuntimeComponentDependencyCycle(t *testing.T) {
	report := buildEnvironmentRestoreComponentReadinessReport(t, "env.component.runtime-cycle", `[]`, environmentRestoreReadinessTwoAppGraph("runtime"))
	if !report.OK || !report.ComponentGraph.OK || report.ComponentGraph.BlockingDependencies != 0 || report.ComponentGraph.RuntimeDependencies != 2 || len(report.ComponentGraph.BlockingCycles) != 0 {
		t.Fatalf("runtime dependency cycle should be allowed by blocking graph gate: %#v", report.ComponentGraph)
	}
	if strings.Join(report.ComponentGraph.BlockingOrder, ",") != "app.a,app.b" {
		t.Fatalf("runtime-only graph should have stable component order: %#v", report.ComponentGraph.BlockingOrder)
	}
}

func TestEnvironmentRestoreUsesComponentHealthChecksForReadiness(t *testing.T) {
	report := buildEnvironmentRestoreComponentReadinessReport(t, "env.component.health", `[]`, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			environmentRestoreReadinessAppComponent("app", "app", "http://127.0.0.1:18080/actuator/health"),
		},
	})
	if !report.OK || len(report.HealthChecks) != 1 {
		t.Fatalf("component health checks should be restore probes: report=%#v health=%#v", report, report.HealthChecks)
	}
	check, ok := report.HealthChecks[0].(map[string]any)
	if !ok || valueString(check["kind"]) != "url" || valueString(check["service"]) != "app" || valueString(check["url"]) != "http://127.0.0.1:18080/actuator/health" || valueString(check["componentId"]) != "app" {
		t.Fatalf("component health check was not normalized: %#v", report.HealthChecks)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "health-probes", true, "1 Store-backed health probe") {
		t.Fatalf("readiness should count component health probes: %#v", report.Readiness.Items)
	}
}

func TestEnvironmentRestoreRequiresURLHealthForBusinessComponents(t *testing.T) {
	report := buildEnvironmentRestoreComponentReadinessReport(t, "env.component.business-health", `[]`, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			environmentRestoreReadinessComponent("mysql", "middleware", "database", "mysql", environmentRestoreReadinessComposeHealth("")),
			environmentRestoreReadinessComponent("app", "app", "business-service", "app", environmentRestoreReadinessComposeHealth("")),
		},
	})
	if report.OK || report.ComponentGraph.OK || report.ComponentGraph.MissingHealthChecks != 1 {
		t.Fatalf("business service compose-only health should fail readiness: %#v", report.ComponentGraph)
	}
	if !strings.Contains(report.ComponentGraph.Error, "app: business-service health check requires url") {
		t.Fatalf("business health error should require url: %q", report.ComponentGraph.Error)
	}
}

func TestEnvironmentRestoreRejectsInvalidComponentHealthCheck(t *testing.T) {
	report := buildEnvironmentRestoreComponentReadinessReport(t, "env.component.invalid-health", `[]`, store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			environmentRestoreReadinessComponent("app", "app", "business-service", "", `{"kind":"url"}`),
		},
	})
	if report.OK || report.ComponentGraph.OK || report.ComponentGraph.MissingHealthChecks != 1 {
		t.Fatalf("component graph should reject invalid health check: %#v", report.ComponentGraph)
	}
	if !restoreTypedReadinessHasItem(report.Readiness.Items, "component-graph", false, "url health check requires url") {
		t.Fatalf("readiness should include invalid component health detail: %#v", report.Readiness.Items)
	}
}

func environmentRestoreReadinessTwoAppGraph(phase string) store.EnvironmentComponentGraph {
	return store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			environmentRestoreReadinessAppComponent("app.a", "app-a", "http://127.0.0.1:18080/app-a/health"),
			environmentRestoreReadinessAppComponent("app.b", "app-b", "http://127.0.0.1:18080/app-b/health"),
		},
		Dependencies: []store.ComponentDependency{
			{ConsumerComponentID: "app.a", ProviderComponentID: "app.b", Phase: phase, Capability: "http", Required: true, ProfileJSON: `{}`},
			{ConsumerComponentID: "app.b", ProviderComponentID: "app.a", Phase: phase, Capability: "http", Required: true, ProfileJSON: `{}`},
		},
	}
}
