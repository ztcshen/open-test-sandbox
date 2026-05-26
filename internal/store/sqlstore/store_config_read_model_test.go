package sqlstore_test

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlstore"
)

func TestStoreUpsertsConfigIndexAndReadModelsThroughDatabaseSQL(t *testing.T) {
	exerciseStoreUpsertsConfigIndexAndReadModels(t, configReadModelDialectExpectation{
		dialect:                     sqlstore.PostgresDialect{},
		profileIndexUpsertFragments: []string{"on conflict(profile_id) do update"},
		configVersionUpsertFragments: []string{
			"on conflict(id) do update",
		},
		readModelUpsertFragments: []string{"on conflict(profile_id, model_key) do update"},
	})
}

func TestStoreUpsertsConfigIndexAndReadModelsUseMySQLDialect(t *testing.T) {
	exerciseStoreUpsertsConfigIndexAndReadModels(t, configReadModelDialectExpectation{
		dialect: sqlstore.MySQLDialect{},
		reject:  "$1",
		profileIndexUpsertFragments: []string{
			"on duplicate key update",
			"bundle_path = values(bundle_path)",
		},
		configVersionUpsertFragments: []string{
			"on duplicate key update",
			"summary_json = values(summary_json)",
		},
		readModelUpsertFragments: []string{
			"on duplicate key update",
			"payload_json = values(payload_json)",
		},
	})
}

type configReadModelDialectExpectation struct {
	dialect                      sqlstore.Dialect
	reject                       string
	profileIndexUpsertFragments  []string
	configVersionUpsertFragments []string
	readModelUpsertFragments     []string
}

func exerciseStoreUpsertsConfigIndexAndReadModels(t *testing.T, tt configReadModelDialectExpectation) {
	t.Helper()

	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, tt.dialect)
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	profileIndex, err := s.UpsertProfileIndex(ctx, store.ProfileIndex{
		ProfileID:    "profile.alpha",
		BundlePath:   "stores/profile.alpha",
		BundleDigest: "sha256:profile",
		SummaryJSON:  `{"services":2}`,
		ImportedAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert profile index: %v", err)
	}
	exec := state.lastExec(t)
	assertSQLContains(t, exec.query, "profile index query", "insert into profile_indexes", sqlValuesClause(tt.dialect, 6))
	assertSQLContains(t, exec.query, "profile index query", tt.profileIndexUpsertFragments...)
	assertSQLOmits(t, exec.query, "profile index query", tt.reject)
	if profileIndex.UpdatedAt.IsZero() || exec.args[0] != "profile.alpha" || exec.args[3] != `{"services":2}` {
		t.Fatalf("profile index/args = %#v %#v", profileIndex, exec.args)
	}

	queueProfileIndexRow(state, now, profileIndex.UpdatedAt)
	loadedIndex, err := s.GetProfileIndex(ctx, "profile.alpha")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if loadedIndex.ProfileID != "profile.alpha" || loadedIndex.BundleDigest != "sha256:profile" || !loadedIndex.ImportedAt.Equal(now) {
		t.Fatalf("loaded profile index = %#v", loadedIndex)
	}
	query := state.lastQuery(t)
	assertSQLContains(t, query.query, "profile index get query", "from profile_indexes where profile_id = "+tt.dialect.BindVar(1))
	if query.args[0] != "profile.alpha" {
		t.Fatalf("profile index get query = %#v", query)
	}

	configVersion, err := s.UpsertConfigVersion(ctx, store.ConfigVersion{
		ID:           "config-001",
		ProfileID:    "profile.alpha",
		SourcePath:   "stores/profile.alpha/catalog.json",
		BundleDigest: "sha256:config",
		SummaryJSON:  `{"cases":5}`,
		Active:       true,
		PublishedAt:  now.Add(1 * time.Minute),
	})
	if err != nil {
		t.Fatalf("upsert config version: %v", err)
	}
	if configVersion.CreatedAt.IsZero() {
		t.Fatalf("config version created timestamp = %#v", configVersion)
	}
	execs := state.lastExecs(t, 2)
	assertSQLContains(t, execs[0].query, "active config reset query", "update config_versions set active = "+tt.dialect.BindVar(1))
	assertSQLOmits(t, execs[0].query, "active config reset query", tt.reject)
	if execs[0].args[0] != false {
		t.Fatalf("active config reset query = %#v", execs[0])
	}
	assertSQLContains(t, execs[1].query, "config version insert query", "insert into config_versions", sqlValuesClause(tt.dialect, 8))
	assertSQLContains(t, execs[1].query, "config version insert query", tt.configVersionUpsertFragments...)
	assertSQLOmits(t, execs[1].query, "config version insert query", tt.reject)
	if execs[1].args[0] != "config-001" || execs[1].args[5] != true {
		t.Fatalf("config version upsert args/query = %#v %s", execs[1].args, execs[1].query)
	}

	queueConfigVersionRow(state, configVersion)
	active, err := s.GetActiveConfigVersion(ctx)
	if err != nil {
		t.Fatalf("get active config version: %v", err)
	}
	if active.ID != "config-001" || !active.Active || active.BundleDigest != "sha256:config" {
		t.Fatalf("active config version = %#v", active)
	}
	query = state.lastQuery(t)
	assertSQLContains(t, query.query, "active config query", "from config_versions", "where active = "+tt.dialect.BindVar(1))
	if query.args[0] != true {
		t.Fatalf("active config query = %#v", query)
	}

	readModel, err := s.UpsertReadModel(ctx, store.ReadModel{
		ProfileID:       "profile.alpha",
		Key:             "workflow-discovery",
		ConfigVersionID: "config-001",
		PayloadJSON:     `{"workflows":[{"id":"workflow.alpha"}]}`,
		GeneratedAt:     now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("upsert read model: %v", err)
	}
	exec = state.lastExec(t)
	assertSQLContains(t, exec.query, "read model query", "insert into config_read_model", sqlValuesClause(tt.dialect, 6))
	assertSQLContains(t, exec.query, "read model query", tt.readModelUpsertFragments...)
	assertSQLOmits(t, exec.query, "read model query", tt.reject)
	if exec.args[1] != "workflow-discovery" {
		t.Fatalf("read model upsert args/query = %#v %s", exec.args, exec.query)
	}
	if readModel.UpdatedAt.IsZero() {
		t.Fatalf("read model updated timestamp = %#v", readModel)
	}

	queueReadModelRow(state, readModel)
	loadedReadModel, err := s.GetReadModel(ctx, "profile.alpha", "workflow-discovery")
	if err != nil {
		t.Fatalf("get read model: %v", err)
	}
	if loadedReadModel.ProfileID != "profile.alpha" || loadedReadModel.Key != "workflow-discovery" || loadedReadModel.ConfigVersionID != "config-001" {
		t.Fatalf("loaded read model = %#v", loadedReadModel)
	}
	query = state.lastQuery(t)
	assertSQLContains(t, query.query, "read model get query", "from config_read_model", "where profile_id = "+tt.dialect.BindVar(1)+" and model_key = "+tt.dialect.BindVar(2))
	if query.args[0] != "profile.alpha" || query.args[1] != "workflow-discovery" {
		t.Fatalf("read model get query = %#v", query)
	}
}

func queueProfileIndexRow(state *fakeSQLState, importedAt, updatedAt time.Time) {
	state.queueRows(fakeRows{
		columns: []string{"profile_id", "bundle_path", "bundle_digest", "summary_json", "imported_at", "updated_at"},
		values: [][]driver.Value{{
			"profile.alpha", "stores/profile.alpha", "sha256:profile", `{"services":2}`,
			importedAt.Format(time.RFC3339Nano), updatedAt.Format(time.RFC3339Nano),
		}},
	})
}

func queueConfigVersionRow(state *fakeSQLState, version store.ConfigVersion) {
	state.queueRows(fakeRows{
		columns: []string{"id", "profile_id", "source_path", "bundle_digest", "summary_json", "active", "published_at", "created_at"},
		values: [][]driver.Value{{
			"config-001", "profile.alpha", "stores/profile.alpha/catalog.json", "sha256:config", `{"cases":5}`,
			true, version.PublishedAt.Format(time.RFC3339Nano), version.CreatedAt.Format(time.RFC3339Nano),
		}},
	})
}

func queueReadModelRow(state *fakeSQLState, readModel store.ReadModel) {
	state.queueRows(fakeRows{
		columns: []string{"profile_id", "model_key", "config_version_id", "payload_json", "generated_at", "updated_at"},
		values: [][]driver.Value{{
			"profile.alpha", "workflow-discovery", "config-001", `{"workflows":[{"id":"workflow.alpha"}]}`,
			readModel.GeneratedAt.Format(time.RFC3339Nano), readModel.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
}
