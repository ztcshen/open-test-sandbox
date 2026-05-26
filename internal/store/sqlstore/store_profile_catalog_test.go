package sqlstore_test

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlstore"
)

func TestStoreReplacesAndReadsProfileCatalogSnapshotThroughDatabaseSQL(t *testing.T) {
	exerciseStoreReplacesAndReadsProfileCatalogSnapshot(t, profileCatalogDialectExpectation{
		dialect:         sqlstore.PostgresDialect{},
		upsertFragments: []string{"on conflict(profile_id) do update"},
	})
}

func TestStoreReplacesProfileCatalogSnapshotUsesMySQLDialect(t *testing.T) {
	exerciseStoreReplacesAndReadsProfileCatalogSnapshot(t, profileCatalogDialectExpectation{
		dialect: sqlstore.MySQLDialect{},
		reject:  "$1",
		upsertFragments: []string{
			"on duplicate key update",
			"catalog_json = values(catalog_json)",
			"template_configs = values(template_configs)",
		},
		requireGeneratedCounts: true,
	})
}

type profileCatalogDialectExpectation struct {
	dialect                sqlstore.Dialect
	reject                 string
	upsertFragments        []string
	requireGeneratedCounts bool
}

func exerciseStoreReplacesAndReadsProfileCatalogSnapshot(t *testing.T, tt profileCatalogDialectExpectation) {
	t.Helper()

	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, tt.dialect)
	indexedAt := time.Date(2026, 5, 19, 13, 0, 0, 0, time.UTC)
	catalog := sampleProfileCatalog(indexedAt)

	if err := s.ReplaceProfileCatalog(ctx, catalog); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	exec := state.lastExec(t)
	assertSQLContains(t, exec.query, "profile catalog query", "insert into profile_catalogs", sqlValuesClause(tt.dialect, 13))
	assertSQLContains(t, exec.query, "profile catalog query", tt.upsertFragments...)
	assertSQLOmits(t, exec.query, "profile catalog query", tt.reject)
	if exec.args[0] != "profile.alpha" || exec.args[2] == "" {
		t.Fatalf("profile catalog args = %#v", exec.args)
	}
	if tt.requireGeneratedCounts && (exec.args[11] == 0 || exec.args[12] == 0) {
		t.Fatalf("profile catalog generated-count args = %#v", exec.args)
	}

	queueProfileCatalogIndexRow(state, indexedAt)
	index, err := s.GetProfileCatalogIndex(ctx)
	if err != nil {
		t.Fatalf("get profile catalog index: %v", err)
	}
	if index.ProfileID != "profile.alpha" || index.Counts.Services != 1 || index.Counts.Templates != 2 || index.Counts.TemplateConfigs != 1 {
		t.Fatalf("profile catalog index = %#v", index)
	}
	query := state.lastQuery(t)
	assertSQLContains(t, query.query, "profile catalog index query", "from profile_catalogs")

	state.queueRows(fakeRows{
		columns: []string{"catalog_json"},
		values:  [][]driver.Value{{exec.args[2]}},
	})
	loaded, err := s.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get profile catalog: %v", err)
	}
	if loaded.ProfileID != "profile.alpha" || !loaded.IndexedAt.Equal(indexedAt) {
		t.Fatalf("loaded profile catalog identity = %#v", loaded)
	}
	if len(loaded.Services) != 1 || loaded.Services[0].SourcePath != "/tmp/source/service.alpha" {
		t.Fatalf("loaded profile catalog services = %#v", loaded.Services)
	}
	if len(loaded.APICases) != 1 || loaded.APICases[0].CasePath != "cases/case.alpha.json" {
		t.Fatalf("loaded profile catalog cases = %#v", loaded.APICases)
	}
	query = state.lastQuery(t)
	assertSQLContains(t, query.query, "profile catalog get query", "select catalog_json", "from profile_catalogs")
}

func sampleProfileCatalog(indexedAt time.Time) store.ProfileCatalog {
	return store.ProfileCatalog{
		ProfileID: "profile.alpha",
		IndexedAt: indexedAt,
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http", SourcePath: "/tmp/source/service.alpha"},
		},
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", CasePath: "cases/case.alpha.json"},
		},
		RequestTemplates: []store.CatalogRequestTemplate{
			{ID: "template.alpha", DisplayName: "Template Alpha", NodeID: "node.alpha", TemplateJSON: `{"method":"GET"}`},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
		},
		CaseDependencies: []store.CatalogCaseDependency{
			{ID: "dependency.alpha", CaseID: "case.alpha", FixtureID: "fixture.alpha", MappingsJSON: `[]`},
		},
		Fixtures: []store.CatalogFixture{
			{ID: "fixture.alpha", DisplayName: "Fixture Alpha", Kind: "json", DataJSON: `{}`},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{ID: "template-config.alpha", TemplateID: "template.alpha", ScopeType: "interface_node", ConfigJSON: `{}`},
		},
	}
}

func queueProfileCatalogIndexRow(state *fakeSQLState, indexedAt time.Time) {
	state.queueRows(fakeRows{
		columns: []string{"profile_id", "indexed_at", "services", "workflows", "interface_nodes", "api_cases", "request_templates", "workflow_bindings", "case_dependencies", "fixtures", "templates", "template_configs"},
		values: [][]driver.Value{{
			"profile.alpha", indexedAt.Format(time.RFC3339Nano), int64(1), int64(1), int64(1), int64(1), int64(1), int64(1), int64(1), int64(1), int64(2), int64(1),
		}},
	})
}
