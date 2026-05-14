package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/migrations"
)

type Config struct {
	Path    string
	BaseDir string
}

func (c Config) Resolve() Config {
	if c.Path != "" {
		return c
	}
	baseDir := c.BaseDir
	if baseDir == "" {
		baseDir = "."
	}
	c.Path = filepath.Join(baseDir, "runtime", "store.sqlite")
	return c
}

func ConfigFromURL(storeURL string) Config {
	if storeURL == "" {
		return Config{}.Resolve()
	}
	for _, prefix := range []string{"sqlite://", "file:"} {
		if strings.HasPrefix(storeURL, prefix) {
			return Config{Path: strings.TrimPrefix(storeURL, prefix)}.Resolve()
		}
	}
	return Config{Path: storeURL}.Resolve()
}

func ParseConfigFromURL(storeURL string) (Config, error) {
	if storeURL == "" {
		return ConfigFromURL(storeURL), nil
	}
	if isUnsupportedBackendURL(storeURL) {
		return Config{}, fmt.Errorf("unsupported store backend %q; supported forms are local paths, sqlite://PATH, and file:PATH", backendScheme(storeURL))
	}
	return ConfigFromURL(storeURL), nil
}

func isUnsupportedBackendURL(storeURL string) bool {
	scheme := backendScheme(storeURL)
	if scheme == "" {
		return false
	}
	return scheme != "sqlite" && scheme != "file"
}

func backendScheme(storeURL string) string {
	match := regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*):`).FindStringSubmatch(storeURL)
	if len(match) != 2 {
		return ""
	}
	return strings.ToLower(match[1])
}

type Store struct {
	path string
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	s, err := openRaw(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

type MigrationStatusResult struct {
	Path           string
	CurrentVersion int
	TargetVersion  int
	AppliedCount   int
}

func (r MigrationStatusResult) HasPending() bool {
	return r.CurrentVersion < r.TargetVersion
}

func MigrationStatus(ctx context.Context, cfg Config) (MigrationStatusResult, error) {
	s, err := openRaw(ctx, cfg)
	if err != nil {
		return MigrationStatusResult{}, err
	}
	defer s.Close()
	return s.migrationStatus(ctx, 0)
}

func Migrate(ctx context.Context, cfg Config) (MigrationStatusResult, error) {
	s, err := openRaw(ctx, cfg)
	if err != nil {
		return MigrationStatusResult{}, err
	}
	defer s.Close()
	return s.migrate(ctx)
}

func openRaw(ctx context.Context, cfg Config) (*Store, error) {
	cfg = cfg.Resolve()
	if cfg.Path == "" {
		return nil, errors.New("sqlite store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite store directory: %w", err)
	}

	s := &Store{path: cfg.Path}
	if err := s.configure(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) configure(ctx context.Context) error {
	return s.exec(ctx, `
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA journal_mode = WAL;`)
}

func (s *Store) migrate(ctx context.Context) (MigrationStatusResult, error) {
	if err := s.ensureMigrationTable(ctx); err != nil {
		return MigrationStatusResult{}, err
	}
	current, err := s.currentMigrationVersion(ctx)
	if err != nil {
		return MigrationStatusResult{}, err
	}

	applied := 0
	for _, migration := range migrations.All() {
		if migration.Version <= current {
			continue
		}
		statement := fmt.Sprintf(`
begin;
%s
insert into schema_migrations (version, name, applied_at)
values (%d, %s, %s);
commit;`, migration.SQL, migration.Version, sqlString(migration.Name), sqlString(encodeTime(utcNow())))
		if err := s.exec(ctx, statement); err != nil {
			return MigrationStatusResult{}, fmt.Errorf("apply migration %d %q: %w", migration.Version, migration.Name, err)
		}
		applied++
	}
	return s.migrationStatus(ctx, applied)
}

func (s *Store) migrationStatus(ctx context.Context, applied int) (MigrationStatusResult, error) {
	current, err := s.currentMigrationVersion(ctx)
	if err != nil {
		return MigrationStatusResult{}, err
	}
	return MigrationStatusResult{
		Path:           s.path,
		CurrentVersion: current,
		TargetVersion:  migrations.CurrentVersion,
		AppliedCount:   applied,
	}, nil
}

func (s *Store) ensureMigrationTable(ctx context.Context) error {
	return s.exec(ctx, `
create table if not exists schema_migrations (
  version integer primary key,
  name text not null,
  applied_at text not null
);`)
}

func (s *Store) currentMigrationVersion(ctx context.Context) (int, error) {
	var tableRows []struct {
		Count int `json:"count"`
	}
	if err := s.query(ctx, `
select count(*) as count from sqlite_master
where type = 'table' and name = 'schema_migrations';`, &tableRows); err != nil {
		return 0, err
	}
	if len(tableRows) == 0 || tableRows[0].Count == 0 {
		return 0, nil
	}

	var versionRows []struct {
		Version int `json:"version"`
	}
	if err := s.query(ctx, `select coalesce(max(version), 0) as version from schema_migrations;`, &versionRows); err != nil {
		return 0, err
	}
	if len(versionRows) == 0 {
		return 0, nil
	}
	return versionRows[0].Version, nil
}

func (s *Store) CreateRun(ctx context.Context, r store.Run) (store.Run, error) {
	now := utcNow()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = r.CreatedAt
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into runs (id, profile_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at)
values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s);`,
		sqlString(r.ID), sqlString(r.ProfileID), sqlString(r.WorkflowID), sqlString(r.Status), sqlString(r.EvidenceRoot),
		sqlString(r.SummaryJSON), sqlString(encodeTime(r.StartedAt)), sqlString(encodeTime(r.FinishedAt)),
		sqlString(encodeTime(r.CreatedAt)), sqlString(encodeTime(r.UpdatedAt)))); err != nil {
		return store.Run{}, fmt.Errorf("create run %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) GetRun(ctx context.Context, id string) (store.Run, error) {
	var rows []runRow
	if err := s.query(ctx, fmt.Sprintf(`
select id, profile_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at
from runs where id = %s;`, sqlString(id)), &rows); err != nil {
		return store.Run{}, err
	}
	if len(rows) == 0 {
		return store.Run{}, store.ErrNotFound
	}
	return rows[0].toStore(), nil
}

func (s *Store) ListRuns(ctx context.Context) ([]store.Run, error) {
	var rows []runRow
	if err := s.query(ctx, `
select id, profile_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at
from runs order by created_at, id;`, &rows); err != nil {
		return nil, err
	}
	out := make([]store.Run, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *Store) RecordAPICaseRun(ctx context.Context, r store.APICaseRun) (store.APICaseRun, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into api_case_runs (id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at)
values (%s, %s, %s, %s, %s, %s, %s, %s, %s);`,
		sqlString(r.ID), sqlString(r.RunID), sqlString(r.CaseID), sqlString(r.Status), sqlString(r.RequestSummaryJSON),
		sqlString(r.AssertionSummaryJSON), sqlString(encodeTime(r.StartedAt)), sqlString(encodeTime(r.FinishedAt)),
		sqlString(encodeTime(r.CreatedAt)))); err != nil {
		return store.APICaseRun{}, fmt.Errorf("record api case run %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) ListAPICaseRuns(ctx context.Context, runID string) ([]store.APICaseRun, error) {
	var rows []apiCaseRunRow
	if err := s.query(ctx, fmt.Sprintf(`
select id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at
from api_case_runs where run_id = %s order by created_at, id;`, sqlString(runID)), &rows); err != nil {
		return nil, err
	}
	out := make([]store.APICaseRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *Store) RecordEvidence(ctx context.Context, r store.EvidenceRecord) (store.EvidenceRecord, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into evidence_records (id, run_id, case_run_id, kind, uri, media_type, sha256, size_bytes, summary, created_at)
values (%s, %s, %s, %s, %s, %s, %s, %d, %s, %s);`,
		sqlString(r.ID), sqlString(r.RunID), sqlString(r.CaseRunID), sqlString(r.Kind), sqlString(r.URI),
		sqlString(r.MediaType), sqlString(r.SHA256), r.SizeBytes, sqlString(r.Summary), sqlString(encodeTime(r.CreatedAt)))); err != nil {
		return store.EvidenceRecord{}, fmt.Errorf("record evidence %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) ListEvidence(ctx context.Context, runID string) ([]store.EvidenceRecord, error) {
	var rows []evidenceRecordRow
	if err := s.query(ctx, fmt.Sprintf(`
select id, run_id, case_run_id, kind, uri, media_type, sha256, size_bytes, summary, created_at
from evidence_records where run_id = %s order by created_at, id;`, sqlString(runID)), &rows); err != nil {
		return nil, err
	}
	out := make([]store.EvidenceRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *Store) UpsertBaselineGate(ctx context.Context, g store.BaselineGate) (store.BaselineGate, error) {
	if g.UpdatedAt.IsZero() {
		g.UpdatedAt = utcNow()
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into baseline_gates (profile_id, subject_id, status, required, summary_json, checked_at, updated_at)
values (%s, %s, %s, %d, %s, %s, %s)
on conflict(profile_id, subject_id) do update set
  status = excluded.status,
  required = excluded.required,
  summary_json = excluded.summary_json,
  checked_at = excluded.checked_at,
  updated_at = excluded.updated_at;`,
		sqlString(g.ProfileID), sqlString(g.SubjectID), sqlString(g.Status), boolInt(g.Required), sqlString(g.SummaryJSON),
		sqlString(encodeTime(g.CheckedAt)), sqlString(encodeTime(g.UpdatedAt)))); err != nil {
		return store.BaselineGate{}, fmt.Errorf("upsert baseline gate %q/%q: %w", g.ProfileID, g.SubjectID, err)
	}
	return g, nil
}

func (s *Store) GetBaselineGate(ctx context.Context, profileID, subjectID string) (store.BaselineGate, error) {
	var rows []baselineGateRow
	if err := s.query(ctx, fmt.Sprintf(`
select profile_id, subject_id, status, required, summary_json, checked_at, updated_at
from baseline_gates where profile_id = %s and subject_id = %s;`, sqlString(profileID), sqlString(subjectID)), &rows); err != nil {
		return store.BaselineGate{}, err
	}
	if len(rows) == 0 {
		return store.BaselineGate{}, store.ErrNotFound
	}
	return rows[0].toStore(), nil
}

func (s *Store) UpsertProfileIndex(ctx context.Context, p store.ProfileIndex) (store.ProfileIndex, error) {
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = utcNow()
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into profile_indexes (profile_id, bundle_path, bundle_digest, summary_json, imported_at, updated_at)
values (%s, %s, %s, %s, %s, %s)
on conflict(profile_id) do update set
  bundle_path = excluded.bundle_path,
  bundle_digest = excluded.bundle_digest,
  summary_json = excluded.summary_json,
  imported_at = excluded.imported_at,
  updated_at = excluded.updated_at;`,
		sqlString(p.ProfileID), sqlString(p.BundlePath), sqlString(p.BundleDigest), sqlString(p.SummaryJSON),
		sqlString(encodeTime(p.ImportedAt)), sqlString(encodeTime(p.UpdatedAt)))); err != nil {
		return store.ProfileIndex{}, fmt.Errorf("upsert profile index %q: %w", p.ProfileID, err)
	}
	return p, nil
}

func (s *Store) GetProfileIndex(ctx context.Context, profileID string) (store.ProfileIndex, error) {
	var rows []profileIndexRow
	if err := s.query(ctx, fmt.Sprintf(`
select profile_id, bundle_path, bundle_digest, summary_json, imported_at, updated_at
from profile_indexes where profile_id = %s;`, sqlString(profileID)), &rows); err != nil {
		return store.ProfileIndex{}, err
	}
	if len(rows) == 0 {
		return store.ProfileIndex{}, store.ErrNotFound
	}
	return rows[0].toStore(), nil
}

func (s *Store) exec(ctx context.Context, statement string) error {
	out, err := sqliteCommand(ctx, false, s.path, statement)
	if err != nil {
		return fmt.Errorf("run sqlite statement: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *Store) query(ctx context.Context, statement string, target any) error {
	out, err := sqliteCommand(ctx, true, s.path, statement)
	if err != nil {
		return fmt.Errorf("run sqlite query: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		out = []byte("[]")
	}
	if err := json.Unmarshal(out, target); err != nil {
		return fmt.Errorf("decode sqlite query result: %w", err)
	}
	return nil
}

func sqliteCommand(ctx context.Context, jsonOutput bool, path string, statement string) ([]byte, error) {
	args := []string{"-cmd", ".timeout 5000"}
	if jsonOutput {
		args = append(args, "-json")
	}
	args = append(args, path, "PRAGMA foreign_keys = ON;\n"+statement)
	cmd := exec.CommandContext(ctx, "sqlite3", args...)
	return cmd.CombinedOutput()
}

type runRow struct {
	ID           string `json:"id"`
	ProfileID    string `json:"profile_id"`
	WorkflowID   string `json:"workflow_id"`
	Status       string `json:"status"`
	EvidenceRoot string `json:"evidence_root"`
	SummaryJSON  string `json:"summary_json"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (r runRow) toStore() store.Run {
	return store.Run{
		ID:           r.ID,
		ProfileID:    r.ProfileID,
		WorkflowID:   r.WorkflowID,
		Status:       r.Status,
		EvidenceRoot: r.EvidenceRoot,
		SummaryJSON:  r.SummaryJSON,
		StartedAt:    decodeTime(r.StartedAt),
		FinishedAt:   decodeTime(r.FinishedAt),
		CreatedAt:    decodeTime(r.CreatedAt),
		UpdatedAt:    decodeTime(r.UpdatedAt),
	}
}

type apiCaseRunRow struct {
	ID                   string `json:"id"`
	RunID                string `json:"run_id"`
	CaseID               string `json:"case_id"`
	Status               string `json:"status"`
	RequestSummaryJSON   string `json:"request_summary_json"`
	AssertionSummaryJSON string `json:"assertion_summary_json"`
	StartedAt            string `json:"started_at"`
	FinishedAt           string `json:"finished_at"`
	CreatedAt            string `json:"created_at"`
}

func (r apiCaseRunRow) toStore() store.APICaseRun {
	return store.APICaseRun{
		ID:                   r.ID,
		RunID:                r.RunID,
		CaseID:               r.CaseID,
		Status:               r.Status,
		RequestSummaryJSON:   r.RequestSummaryJSON,
		AssertionSummaryJSON: r.AssertionSummaryJSON,
		StartedAt:            decodeTime(r.StartedAt),
		FinishedAt:           decodeTime(r.FinishedAt),
		CreatedAt:            decodeTime(r.CreatedAt),
	}
}

type evidenceRecordRow struct {
	ID        string `json:"id"`
	RunID     string `json:"run_id"`
	CaseRunID string `json:"case_run_id"`
	Kind      string `json:"kind"`
	URI       string `json:"uri"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"created_at"`
}

func (r evidenceRecordRow) toStore() store.EvidenceRecord {
	return store.EvidenceRecord{
		ID:        r.ID,
		RunID:     r.RunID,
		CaseRunID: r.CaseRunID,
		Kind:      r.Kind,
		URI:       r.URI,
		MediaType: r.MediaType,
		SHA256:    r.SHA256,
		SizeBytes: r.SizeBytes,
		Summary:   r.Summary,
		CreatedAt: decodeTime(r.CreatedAt),
	}
}

type baselineGateRow struct {
	ProfileID   string `json:"profile_id"`
	SubjectID   string `json:"subject_id"`
	Status      string `json:"status"`
	Required    int    `json:"required"`
	SummaryJSON string `json:"summary_json"`
	CheckedAt   string `json:"checked_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (r baselineGateRow) toStore() store.BaselineGate {
	return store.BaselineGate{
		ProfileID:   r.ProfileID,
		SubjectID:   r.SubjectID,
		Status:      r.Status,
		Required:    r.Required != 0,
		SummaryJSON: r.SummaryJSON,
		CheckedAt:   decodeTime(r.CheckedAt),
		UpdatedAt:   decodeTime(r.UpdatedAt),
	}
}

type profileIndexRow struct {
	ProfileID    string `json:"profile_id"`
	BundlePath   string `json:"bundle_path"`
	BundleDigest string `json:"bundle_digest"`
	SummaryJSON  string `json:"summary_json"`
	ImportedAt   string `json:"imported_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (r profileIndexRow) toStore() store.ProfileIndex {
	return store.ProfileIndex{
		ProfileID:    r.ProfileID,
		BundlePath:   r.BundlePath,
		BundleDigest: r.BundleDigest,
		SummaryJSON:  r.SummaryJSON,
		ImportedAt:   decodeTime(r.ImportedAt),
		UpdatedAt:    decodeTime(r.UpdatedAt),
	}
}

func sqlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func encodeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func decodeTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func utcNow() time.Time {
	return time.Now().UTC()
}
