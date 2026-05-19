package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/schema"
	"open-test-sandbox/internal/store/sqlstore"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Config struct {
	URL        string
	DriverName string
}

type Store struct {
	core *sqlstore.Store
}

func ParseConfigFromURL(storeURL string) (Config, error) {
	storeURL = strings.TrimSpace(storeURL)
	if storeURL == "" {
		return Config{}, errors.New("postgres store url is required")
	}
	parsed, err := url.Parse(storeURL)
	if err != nil {
		return Config{}, err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "postgres", "postgresql":
		return Config{URL: storeURL, DriverName: sqlstore.PostgresDialect{}.DriverName()}, nil
	default:
		return Config{}, fmt.Errorf("unsupported postgres store backend %q", parsed.Scheme)
	}
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	driverName := strings.TrimSpace(cfg.DriverName)
	if driverName == "" {
		driverName = sqlstore.PostgresDialect{}.DriverName()
	}
	db, err := sql.Open(driverName, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("open postgres store: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres store: %w", err)
	}
	return &Store{core: sqlstore.New(db, sqlstore.PostgresDialect{})}, nil
}

func (s *Store) Close() error {
	if s == nil || s.core == nil {
		return nil
	}
	return s.core.Close()
}

func (s *Store) CreateRun(ctx context.Context, r store.Run) (store.Run, error) {
	return s.core.CreateRun(ctx, r)
}

func (s *Store) GetRun(ctx context.Context, id string) (store.Run, error) {
	return s.core.GetRun(ctx, id)
}

func (s *Store) ListRuns(ctx context.Context) ([]store.Run, error) {
	return s.core.ListRuns(ctx)
}

func (s *Store) RecordAPICaseRun(ctx context.Context, r store.APICaseRun) (store.APICaseRun, error) {
	return s.core.RecordAPICaseRun(ctx, r)
}

func (s *Store) ListAPICaseRuns(ctx context.Context, runID string) ([]store.APICaseRun, error) {
	return s.core.ListAPICaseRuns(ctx, runID)
}

func (s *Store) RecordEvidence(ctx context.Context, r store.EvidenceRecord) (store.EvidenceRecord, error) {
	return s.core.RecordEvidence(ctx, r)
}

func (s *Store) ListEvidence(ctx context.Context, runID string) ([]store.EvidenceRecord, error) {
	return s.core.ListEvidence(ctx, runID)
}

func (s *Store) SaveTraceTopology(ctx context.Context, r store.TraceTopology) (store.TraceTopology, error) {
	return s.core.SaveTraceTopology(ctx, r)
}

func (s *Store) ListTraceTopologies(ctx context.Context, workflowRunID string) ([]store.TraceTopology, error) {
	return s.core.ListTraceTopologies(ctx, workflowRunID)
}

func (s *Store) RecordPostProcessTask(ctx context.Context, r store.PostProcessTask) (store.PostProcessTask, error) {
	return s.core.RecordPostProcessTask(ctx, r)
}

func (s *Store) ListPostProcessTasks(ctx context.Context, runID string) ([]store.PostProcessTask, error) {
	return s.core.ListPostProcessTasks(ctx, runID)
}

func (s *Store) UpsertBaselineGate(ctx context.Context, r store.BaselineGate) (store.BaselineGate, error) {
	return s.core.UpsertBaselineGate(ctx, r)
}

func (s *Store) GetBaselineGate(ctx context.Context, profileID string, subjectID string) (store.BaselineGate, error) {
	return s.core.GetBaselineGate(ctx, profileID, subjectID)
}

func (s *Store) UpsertProfileIndex(ctx context.Context, r store.ProfileIndex) (store.ProfileIndex, error) {
	return s.core.UpsertProfileIndex(ctx, r)
}

func (s *Store) GetProfileIndex(ctx context.Context, profileID string) (store.ProfileIndex, error) {
	return s.core.GetProfileIndex(ctx, profileID)
}

func (s *Store) UpsertConfigVersion(ctx context.Context, r store.ConfigVersion) (store.ConfigVersion, error) {
	return s.core.UpsertConfigVersion(ctx, r)
}

func (s *Store) GetActiveConfigVersion(ctx context.Context) (store.ConfigVersion, error) {
	return s.core.GetActiveConfigVersion(ctx)
}

func (s *Store) UpsertReadModel(ctx context.Context, r store.ReadModel) (store.ReadModel, error) {
	return s.core.UpsertReadModel(ctx, r)
}

func (s *Store) GetReadModel(ctx context.Context, profileID string, key string) (store.ReadModel, error) {
	return s.core.GetReadModel(ctx, profileID, key)
}

func (s *Store) ReplaceProfileCatalog(context.Context, store.ProfileCatalog) error {
	return errPostgresStoreNotImplemented()
}

func (s *Store) GetProfileCatalog(context.Context) (store.ProfileCatalog, error) {
	return store.ProfileCatalog{}, errPostgresStoreNotImplemented()
}

func (s *Store) GetProfileCatalogIndex(context.Context) (store.ProfileCatalogIndex, error) {
	return store.ProfileCatalogIndex{}, errPostgresStoreNotImplemented()
}

func errPostgresStoreNotImplemented() error {
	return errors.New("postgres store contract is not implemented yet")
}

type SchemaStatusResult struct {
	URL            string
	CurrentVersion int
	TargetVersion  int
	AppliedCount   int
}

func (r SchemaStatusResult) HasPending() bool {
	return r.CurrentVersion < r.TargetVersion
}

func SchemaStatus(ctx context.Context, cfg Config) (SchemaStatusResult, error) {
	current, err := currentSchemaVersion(ctx, cfg.URL)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	return SchemaStatusResult{URL: cfg.URL, CurrentVersion: current, TargetVersion: schema.CurrentVersion}, nil
}

func UpgradeSchema(ctx context.Context, cfg Config) (SchemaStatusResult, error) {
	if err := ensureSchemaVersionTable(ctx, cfg.URL); err != nil {
		return SchemaStatusResult{}, err
	}
	current, err := currentSchemaVersion(ctx, cfg.URL)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	applied := 0
	for _, change := range schema.All() {
		if change.Version <= current {
			continue
		}
		statement := fmt.Sprintf(`
begin;
%s
insert into schema_versions (version, name, applied_at)
values (%d, %s, %s);
commit;`, change.SQL, change.Version, pgString(change.Name), pgString(time.Now().UTC().Format(time.RFC3339Nano)))
		if err := psqlExec(ctx, cfg.URL, statement); err != nil {
			return SchemaStatusResult{}, fmt.Errorf("apply schema change %d %q: %w", change.Version, change.Name, err)
		}
		applied++
	}
	status, err := SchemaStatus(ctx, cfg)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	status.AppliedCount = applied
	return status, nil
}

func ensureSchemaVersionTable(ctx context.Context, dsn string) error {
	return psqlExec(ctx, dsn, `
create table if not exists schema_versions (
  version integer primary key,
  name text not null,
  applied_at timestamptz not null
);`)
}

func currentSchemaVersion(ctx context.Context, dsn string) (int, error) {
	var tableRows []struct {
		Count int `json:"count"`
	}
	if err := psqlQuery(ctx, dsn, `
select case when exists (
  select 1 from information_schema.tables
  where table_schema = current_schema() and table_name = 'schema_versions'
) then 1 else 0 end as count`, &tableRows); err != nil {
		return 0, err
	}
	if len(tableRows) == 0 || tableRows[0].Count == 0 {
		return 0, nil
	}
	var versionRows []struct {
		Version int `json:"version"`
	}
	if err := psqlQuery(ctx, dsn, `select coalesce(max(version), 0) as version from schema_versions`, &versionRows); err != nil {
		return 0, err
	}
	if len(versionRows) == 0 {
		return 0, nil
	}
	return versionRows[0].Version, nil
}

func psqlExec(ctx context.Context, dsn string, statement string) error {
	cmd := exec.CommandContext(ctx, "psql", dsn, "-X", "-q", "-v", "ON_ERROR_STOP=1", "-c", statement)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run psql statement: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func psqlQuery(ctx context.Context, dsn string, statement string, target any) error {
	wrapped := fmt.Sprintf(`copy (select coalesce(json_agg(row_to_json(q)), '[]'::json) from (%s) q) to stdout;`, strings.TrimSuffix(strings.TrimSpace(statement), ";"))
	cmd := exec.CommandContext(ctx, "psql", dsn, "-X", "-q", "-t", "-A", "-v", "ON_ERROR_STOP=1", "-c", wrapped)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run psql query: %w: %s", err, strings.TrimSpace(string(out)))
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		raw = "[]"
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode psql query result: %w", err)
	}
	return nil
}

func pgString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
