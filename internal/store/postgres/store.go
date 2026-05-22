package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlstore"

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
	db, err := openDB(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := sqlstore.UpgradeSchema(ctx, db, sqlstore.PostgresDialect{}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("upgrade postgres store schema: %w", err)
	}
	return &Store{core: sqlstore.New(db, sqlstore.PostgresDialect{})}, nil
}

func openDB(ctx context.Context, cfg Config) (*sql.DB, error) {
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
	return db, nil
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

func (s *Store) ListLatestAPICaseRuns(ctx context.Context) ([]store.APICaseRun, error) {
	return s.core.ListLatestAPICaseRuns(ctx)
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

func (s *Store) ReplaceProfileCatalog(ctx context.Context, catalog store.ProfileCatalog) error {
	return s.core.ReplaceProfileCatalog(ctx, catalog)
}

func (s *Store) GetProfileCatalog(ctx context.Context) (store.ProfileCatalog, error) {
	return s.core.GetProfileCatalog(ctx)
}

func (s *Store) GetProfileCatalogIndex(ctx context.Context) (store.ProfileCatalogIndex, error) {
	return s.core.GetProfileCatalogIndex(ctx)
}

func (s *Store) UpsertEnvironment(ctx context.Context, e store.Environment) (store.Environment, error) {
	return s.core.UpsertEnvironment(ctx, e)
}

func (s *Store) GetEnvironment(ctx context.Context, id string) (store.Environment, error) {
	return s.core.GetEnvironment(ctx, id)
}

func (s *Store) ListEnvironments(ctx context.Context) ([]store.Environment, error) {
	return s.core.ListEnvironments(ctx)
}

func (s *Store) ReplaceEnvironmentComponentGraph(ctx context.Context, envID string, graph store.EnvironmentComponentGraph) error {
	return s.core.ReplaceEnvironmentComponentGraph(ctx, envID, graph)
}

func (s *Store) GetEnvironmentComponentGraph(ctx context.Context, envID string) (store.EnvironmentComponentGraph, error) {
	return s.core.GetEnvironmentComponentGraph(ctx, envID)
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
	db, err := openDB(ctx, cfg)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	defer db.Close()
	status, err := sqlstore.SchemaStatus(ctx, db, sqlstore.PostgresDialect{})
	if err != nil {
		return SchemaStatusResult{}, err
	}
	return SchemaStatusResult{URL: cfg.URL, CurrentVersion: status.CurrentVersion, TargetVersion: status.TargetVersion, AppliedCount: status.AppliedCount}, nil
}

func UpgradeSchema(ctx context.Context, cfg Config) (SchemaStatusResult, error) {
	db, err := openDB(ctx, cfg)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	defer db.Close()
	status, err := sqlstore.UpgradeSchema(ctx, db, sqlstore.PostgresDialect{})
	if err != nil {
		return SchemaStatusResult{}, err
	}
	return SchemaStatusResult{URL: cfg.URL, CurrentVersion: status.CurrentVersion, TargetVersion: status.TargetVersion, AppliedCount: status.AppliedCount}, nil
}
