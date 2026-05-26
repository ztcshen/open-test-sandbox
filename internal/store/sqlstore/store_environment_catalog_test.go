package sqlstore_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlstore"
)

func TestStoreEnvironmentCatalogUsesMySQLDialect(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, sqlstore.MySQLDialect{})
	verifiedAt := mysqlAcceptedEnvironmentVerifiedAt()

	env := assertMySQLAcceptedEnvironmentUpsert(t, ctx, s, state, verifiedAt)
	assertMySQLAcceptedEnvironmentLookup(t, ctx, s, state, env, verifiedAt)
	assertMySQLComponentGraphReplace(t, ctx, s, state, env)
	assertMySQLComponentGraphLookup(t, ctx, s, state, env)
}

func mysqlAcceptedEnvironmentVerifiedAt() time.Time {
	return time.Date(2026, 5, 19, 14, 0, 0, 0, time.UTC)
}

func mysqlAcceptedEnvironmentFixture(verifiedAt time.Time) store.Environment {
	return store.Environment{
		ID:                     "env.mysql.accepted",
		DisplayName:            "MySQL Accepted Environment",
		Description:            "Store-backed MySQL environment",
		Status:                 "verified",
		Verified:               true,
		ServicesJSON:           `[{"id":"service.alpha"}]`,
		ReposJSON:              `{"service.alpha":{"url":"https://example.com/service-alpha.git"}}`,
		ComposeJSON:            `{"composeFile":"docker-compose.yml"}`,
		HealthChecksJSON:       `[{"id":"alpha-health","url":"http://127.0.0.1:18080/health"}]`,
		VerificationWorkflowID: "workflow.core-10",
		LastVerificationRunID:  "run-001",
		LastVerificationStatus: store.StatusPassed,
		EvidenceComplete:       true,
		TopologyComplete:       true,
		LastVerifiedAt:         verifiedAt,
		SummaryJSON:            `{"accepted":true}`,
	}
}

func assertMySQLAcceptedEnvironmentUpsert(
	t *testing.T,
	ctx context.Context,
	s *sqlstore.Store,
	state *fakeSQLState,
	verifiedAt time.Time,
) store.Environment {
	t.Helper()

	env, err := s.UpsertEnvironment(ctx, mysqlAcceptedEnvironmentFixture(verifiedAt))
	if err != nil {
		t.Fatalf("upsert environment: %v", err)
	}
	exec := state.lastExec(t)
	assertSQLContains(t, exec.query, "environment query", "insert into environments", sqlValuesClause(sqlstore.MySQLDialect{}, 18))
	assertSQLContains(t, exec.query, "environment query", "on duplicate key update", "compose_json = values(compose_json)", "topology_complete = values(topology_complete)")
	assertSQLOmits(t, exec.query, "environment query", "$1")
	if env.CreatedAt.IsZero() || env.UpdatedAt.IsZero() || exec.args[0] != "env.mysql.accepted" || exec.args[13] != true {
		t.Fatalf("environment/args = %#v %#v", env, exec.args)
	}
	return env
}

func assertMySQLAcceptedEnvironmentLookup(
	t *testing.T,
	ctx context.Context,
	s *sqlstore.Store,
	state *fakeSQLState,
	env store.Environment,
	verifiedAt time.Time,
) {
	t.Helper()

	queueMySQLAcceptedEnvironmentRow(state, env, verifiedAt)
	loadedEnv, err := s.GetEnvironment(ctx, env.ID)
	if err != nil {
		t.Fatalf("get environment: %v", err)
	}
	if !loadedEnv.Verified || loadedEnv.LastVerificationStatus != store.StatusPassed || !loadedEnv.EvidenceComplete || !loadedEnv.TopologyComplete {
		t.Fatalf("loaded environment verification = %#v", loadedEnv)
	}
	query := state.lastQuery(t)
	assertSQLContains(t, query.query, "environment get query", "from environments where id = ?")
	if query.args[0] != env.ID {
		t.Fatalf("environment get query = %#v", query)
	}
}

func queueMySQLAcceptedEnvironmentRow(state *fakeSQLState, env store.Environment, verifiedAt time.Time) {
	state.queueRows(fakeRows{
		columns: []string{"id", "display_name", "description", "status", "verified", "services_json", "repos_json", "compose_json", "health_checks_json", "verification_workflow_id", "last_verification_run_id", "last_verification_status", "evidence_complete", "topology_complete", "last_verified_at", "summary_json", "created_at", "updated_at"},
		values: [][]driver.Value{{
			env.ID, env.DisplayName, env.Description, env.Status, env.Verified, env.ServicesJSON, env.ReposJSON, env.ComposeJSON,
			env.HealthChecksJSON, env.VerificationWorkflowID, env.LastVerificationRunID, env.LastVerificationStatus,
			env.EvidenceComplete, env.TopologyComplete, verifiedAt.Format(time.RFC3339Nano), env.SummaryJSON,
			env.CreatedAt.Format(time.RFC3339Nano), env.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
}

func mysqlComponentGraphFixture() store.EnvironmentComponentGraph {
	return store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "mysql", DisplayName: "MySQL", Kind: "middleware", Role: "database", ComposeService: "mysql", Image: "mysql:8", Required: true, RuntimeJSON: `{"ports":[3306]}`, HealthCheckJSON: `{"type":"tcp"}`, SummaryJSON: `{}`},
			{ComponentID: "service.alpha", DisplayName: "Service Alpha", Kind: "app", Role: "business-service", ComposeService: "service-alpha", Required: true, RuntimeJSON: `{}`, HealthCheckJSON: `{"type":"url"}`, SummaryJSON: `{}`},
		},
		Dependencies: []store.ComponentDependency{
			{ConsumerComponentID: "service.alpha", ProviderComponentID: "mysql", Phase: "startup", Capability: "sql", Required: true, ProfileJSON: `{"database":"alpha"}`},
		},
		Assets: []store.ComponentConfigAsset{
			{OwnerComponentID: "service.alpha", AssetID: "alpha.mysql.ddl", AssetKind: "mysql-ddl", TargetComponentID: "mysql", TargetPath: "compose/mysql/init/alpha.sql", ContentInline: "create table alpha_smoke (id bigint primary key);", SizeBytes: 48, ApplyOrder: 10, SummaryJSON: `{"ownedBy":"service.alpha"}`},
		},
	}
}

func assertMySQLComponentGraphReplace(
	t *testing.T,
	ctx context.Context,
	s *sqlstore.Store,
	state *fakeSQLState,
	env store.Environment,
) {
	t.Helper()

	if err := s.ReplaceEnvironmentComponentGraph(ctx, env.ID, mysqlComponentGraphFixture()); err != nil {
		t.Fatalf("replace environment component graph: %v", err)
	}
	joinedExecs := joinedSQLExecs(state.execsSnapshot())
	assertSQLContains(t, joinedExecs, "mysql component graph execs",
		"delete from component_config_assets where env_id = ?",
		"delete from component_dependencies where env_id = ?",
		"delete from environment_components where env_id = ?",
		"insert into environment_components",
		"values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"insert into component_dependencies",
		"values (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"insert into component_config_assets",
		"`sensitive`",
		"values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
	)
	assertSQLOmits(t, joinedExecs, "mysql component graph execs", "$1")
}

func joinedSQLExecs(execs []fakeSQLCall) string {
	joinedExecs := strings.Builder{}
	for _, exec := range execs {
		joinedExecs.WriteString(exec.query)
		joinedExecs.WriteByte('\n')
	}
	return joinedExecs.String()
}

func assertMySQLComponentGraphLookup(
	t *testing.T,
	ctx context.Context,
	s *sqlstore.Store,
	state *fakeSQLState,
	env store.Environment,
) {
	t.Helper()

	queueMySQLComponentRows(state, env)
	loadedGraph, err := s.GetEnvironmentComponentGraph(ctx, env.ID)
	if err != nil {
		t.Fatalf("get environment component graph: %v", err)
	}
	if len(loadedGraph.Components) != 1 || len(loadedGraph.Dependencies) != 1 || len(loadedGraph.Assets) != 1 {
		t.Fatalf("loaded component graph = %#v", loadedGraph)
	}
	if loadedGraph.Assets[0].TargetComponentID != "mysql" || loadedGraph.Dependencies[0].ProviderComponentID != "mysql" {
		t.Fatalf("loaded component graph mysql links = %#v %#v", loadedGraph.Dependencies, loadedGraph.Assets)
	}
}

func queueMySQLComponentRows(state *fakeSQLState, env store.Environment) {
	state.queueRows(fakeRows{
		columns: []string{"env_id", "component_id", "display_name", "kind", "role", "compose_service", "image", "required", "runtime_json", "healthcheck_json", "summary_json", "created_at", "updated_at"},
		values: [][]driver.Value{{
			env.ID, "mysql", "MySQL", "middleware", "database", "mysql", "mysql:8", true, `{"ports":[3306]}`, `{"type":"tcp"}`, `{}`, env.CreatedAt.Format(time.RFC3339Nano), env.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
	state.queueRows(fakeRows{
		columns: []string{"env_id", "consumer_component_id", "provider_component_id", "phase", "capability", "required", "profile_json", "created_at", "updated_at"},
		values: [][]driver.Value{{
			env.ID, "service.alpha", "mysql", "startup", "sql", true, `{"database":"alpha"}`, env.CreatedAt.Format(time.RFC3339Nano), env.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
	state.queueRows(fakeRows{
		columns: []string{"env_id", "owner_component_id", "asset_id", "asset_kind", "target_component_id", "target_path", "content_inline", "remote_ref_json", "sha256", "size_bytes", "apply_order", "sensitive", "summary_json", "created_at", "updated_at"},
		values: [][]driver.Value{{
			env.ID, "service.alpha", "alpha.mysql.ddl", "mysql-ddl", "mysql", "compose/mysql/init/alpha.sql", "create table alpha_smoke (id bigint primary key);", `{}`, "", int64(48), int64(10), false, `{"ownedBy":"service.alpha"}`, env.CreatedAt.Format(time.RFC3339Nano), env.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
}
