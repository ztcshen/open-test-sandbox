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

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/schema"
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
	if sqliteStoreDisabled() {
		return nil, errors.New("SQLite Store is disabled by AGENT_TESTBENCH_DISABLE_SQLITE_STORE; use a PostgreSQL or MySQL Store for this run")
	}
	s, err := openRaw(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := s.upgradeSchema(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

type SchemaStatusResult struct {
	Path           string
	CurrentVersion int
	TargetVersion  int
	AppliedCount   int
}

func (r SchemaStatusResult) HasPending() bool {
	return r.CurrentVersion < r.TargetVersion
}

func SchemaStatus(ctx context.Context, cfg Config) (SchemaStatusResult, error) {
	if sqliteStoreDisabled() {
		return SchemaStatusResult{}, errors.New("SQLite Store is disabled by AGENT_TESTBENCH_DISABLE_SQLITE_STORE; use a PostgreSQL or MySQL Store for this run")
	}
	s, err := openRaw(ctx, cfg)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	defer s.Close()
	return s.schemaStatus(ctx, 0)
}

func UpgradeSchema(ctx context.Context, cfg Config) (SchemaStatusResult, error) {
	if sqliteStoreDisabled() {
		return SchemaStatusResult{}, errors.New("SQLite Store is disabled by AGENT_TESTBENCH_DISABLE_SQLITE_STORE; use a PostgreSQL or MySQL Store for this run")
	}
	s, err := openRaw(ctx, cfg)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	defer s.Close()
	return s.upgradeSchema(ctx)
}

func sqliteStoreDisabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("AGENT_TESTBENCH_DISABLE_SQLITE_STORE")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
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

func (s *Store) upgradeSchema(ctx context.Context) (SchemaStatusResult, error) {
	if err := s.ensureSchemaVersionTable(ctx); err != nil {
		return SchemaStatusResult{}, err
	}
	current, err := s.currentSchemaVersion(ctx)
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
commit;`, change.SQL, change.Version, sqlString(change.Name), sqlString(encodeTime(utcNow())))
		if err := s.exec(ctx, statement); err != nil {
			return SchemaStatusResult{}, fmt.Errorf("apply schema change %d %q: %w", change.Version, change.Name, err)
		}
		applied++
	}
	return s.schemaStatus(ctx, applied)
}

func (s *Store) schemaStatus(ctx context.Context, applied int) (SchemaStatusResult, error) {
	current, err := s.currentSchemaVersion(ctx)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	return SchemaStatusResult{
		Path:           s.path,
		CurrentVersion: current,
		TargetVersion:  schema.CurrentVersion,
		AppliedCount:   applied,
	}, nil
}

func (s *Store) ensureSchemaVersionTable(ctx context.Context) error {
	return s.exec(ctx, `
create table if not exists schema_versions (
  version integer primary key,
  name text not null,
  applied_at text not null
);`)
}

func (s *Store) currentSchemaVersion(ctx context.Context) (int, error) {
	var tableRows []struct {
		Count int `json:"count"`
	}
	if err := s.query(ctx, `
select count(*) as count from sqlite_master
where type = 'table' and name = 'schema_versions';`, &tableRows); err != nil {
		return 0, err
	}
	if len(tableRows) == 0 || tableRows[0].Count == 0 {
		return 0, nil
	}

	var versionRows []struct {
		Version int `json:"version"`
	}
	if err := s.query(ctx, `select coalesce(max(version), 0) as version from schema_versions;`, &versionRows); err != nil {
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
insert into runs (id, profile_id, environment_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at)
values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s);`,
		sqlString(r.ID), sqlString(r.ProfileID), sqlString(r.EnvironmentID), sqlString(r.WorkflowID), sqlString(r.Status), sqlString(r.EvidenceRoot),
		sqlString(r.SummaryJSON), sqlString(encodeTime(r.StartedAt)), sqlString(encodeTime(r.FinishedAt)),
		sqlString(encodeTime(r.CreatedAt)), sqlString(encodeTime(r.UpdatedAt)))); err != nil {
		return store.Run{}, fmt.Errorf("create run %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) GetRun(ctx context.Context, id string) (store.Run, error) {
	var rows []runRow
	if err := s.query(ctx, fmt.Sprintf(`
select id, profile_id, environment_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at
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
select id, profile_id, environment_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at
from runs order by created_at, id;`, &rows); err != nil {
		return nil, err
	}
	out := make([]store.Run, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *Store) WorkflowStepRun(ctx context.Context, runID string, stepID string) (store.Run, error) {
	var rows []runRow
	if err := s.query(ctx, fmt.Sprintf(`
select r.id, r.profile_id, r.environment_id, r.workflow_id, r.status, r.evidence_root,
  json_object(
    'summary', coalesce(json_extract(r.summary_json, '$.summary'), json('{}')),
    'steps', json_array(json(step.value))
  ) as summary_json,
  r.started_at, r.finished_at, r.created_at, r.updated_at
from runs r, json_each(r.summary_json, '$.steps') as step
where r.id = %s
  and json_valid(r.summary_json)
  and json_extract(step.value, '$.stepId') = %s
limit 1;`, sqlString(runID), sqlString(stepID)), &rows); err != nil {
		return store.Run{}, err
	}
	if len(rows) == 0 {
		return store.Run{}, store.ErrNotFound
	}
	return rows[0].toStore(), nil
}

func (s *Store) LatestWorkflowStepRun(ctx context.Context, workflowID string, stepID string, requireHTTPResult bool) (store.Run, error) {
	httpFilter := ""
	if requireHTTPResult {
		httpFilter = `
  and (
    coalesce(json_extract(step.value, '$.result.response.statusCode'), 0) > 0
    or coalesce(json_extract(step.value, '$.summary.httpCode'), 0) > 0
  )`
	}
	var rows []runRow
	if err := s.query(ctx, fmt.Sprintf(`
select r.id, r.profile_id, r.environment_id, r.workflow_id, r.status, r.evidence_root,
  json_object(
    'summary', coalesce(json_extract(r.summary_json, '$.summary'), json('{}')),
    'steps', json_array(json(step.value))
  ) as summary_json,
  r.started_at, r.finished_at, r.created_at, r.updated_at
from runs r, json_each(r.summary_json, '$.steps') as step
where r.workflow_id = %s
  and json_valid(r.summary_json)
  and json_extract(step.value, '$.stepId') = %s%s
order by
  case
    when coalesce(json_extract(r.summary_json, '$.kind'), '') <> 'apiCase'
      and coalesce(
        json_extract(r.summary_json, '$.summary.expectedStepCount'),
        json_extract(r.summary_json, '$.summary.stepCount'),
        json_extract(r.summary_json, '$.stepCount'),
        json_array_length(r.summary_json, '$.steps'),
        0
      ) > 1
    then 0 else 1
  end,
  r.created_at desc, r.id desc
limit 1;`, sqlString(workflowID), sqlString(stepID), httpFilter), &rows); err != nil {
		return store.Run{}, err
	}
	if len(rows) == 0 {
		return store.Run{}, store.ErrNotFound
	}
	return rows[0].toStore(), nil
}

func (s *Store) ListRunHeaders(ctx context.Context) ([]store.Run, error) {
	var rows []runRow
	if err := s.query(ctx, `
select id, profile_id, environment_id, workflow_id, status, evidence_root,
  case
    when json_valid(summary_json) then json_object(
      'kind', json_extract(summary_json, '$.kind'),
      'summary', json_object(
        'caseId', json_extract(summary_json, '$.summary.caseId'),
        'expectedStepCount', json_extract(summary_json, '$.summary.expectedStepCount'),
        'stepCount', coalesce(
          json_extract(summary_json, '$.summary.stepCount'),
          json_extract(summary_json, '$.stepCount'),
          json_array_length(summary_json, '$.steps'),
          0
        )
      )
    )
    else '{}'
  end as summary_json,
  started_at, finished_at, created_at, updated_at
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

func (s *Store) ListLatestAPICaseRuns(ctx context.Context) ([]store.APICaseRun, error) {
	var rows []apiCaseRunRow
	if err := s.query(ctx, `
select id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at
from (
  select id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at,
    row_number() over (partition by case_id order by created_at desc, id desc) as row_number
  from api_case_runs
  where case_id <> ''
)
where row_number = 1
order by created_at, id;`, &rows); err != nil {
		return nil, err
	}
	out := make([]store.APICaseRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *Store) ListAPICaseRunRecordsForCaseIDs(ctx context.Context, caseIDs []string) ([]store.APICaseRunRecord, error) {
	if len(caseIDs) == 0 {
		return []store.APICaseRunRecord{}, nil
	}
	values := make([]string, 0, len(caseIDs))
	for _, id := range caseIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		values = append(values, sqlString(id))
	}
	if len(values) == 0 {
		return []store.APICaseRunRecord{}, nil
	}
	var rows []apiCaseRunRecordRow
	if err := s.query(ctx, fmt.Sprintf(`
select
  r.id as run_id,
  r.profile_id as run_profile_id,
  r.workflow_id as run_workflow_id,
  r.status as run_status,
  r.evidence_root as run_evidence_root,
  case
    when json_valid(r.summary_json) then json_object(
      'kind', json_extract(r.summary_json, '$.kind'),
      'summary', json_object(
        'caseId', json_extract(r.summary_json, '$.summary.caseId'),
        'expectedStepCount', json_extract(r.summary_json, '$.summary.expectedStepCount'),
        'stepCount', coalesce(
          json_extract(r.summary_json, '$.summary.stepCount'),
          json_extract(r.summary_json, '$.stepCount'),
          json_array_length(r.summary_json, '$.steps'),
          0
        )
      )
    )
    else '{}'
  end as run_summary_json,
  r.started_at as run_started_at,
  r.finished_at as run_finished_at,
  r.created_at as run_created_at,
  r.updated_at as run_updated_at,
  acr.id as case_run_id,
  acr.run_id as case_run_run_id,
  acr.case_id as case_run_case_id,
  acr.status as case_run_status,
  acr.request_summary_json as case_run_request_summary_json,
  acr.assertion_summary_json as case_run_assertion_summary_json,
  acr.started_at as case_run_started_at,
  acr.finished_at as case_run_finished_at,
  acr.created_at as case_run_created_at
from api_case_runs acr
join runs r on r.id = acr.run_id
where acr.case_id in (%s)
order by acr.created_at desc, acr.id desc;`, strings.Join(values, ",")), &rows); err != nil {
		return nil, err
	}
	out := make([]store.APICaseRunRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *Store) RecordEvidence(ctx context.Context, r store.EvidenceRecord) (store.EvidenceRecord, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	if strings.TrimSpace(r.LabelsJSON) == "" {
		r.LabelsJSON = "{}"
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into evidence_records (id, run_id, case_run_id, step_id, kind, uri, media_type, sha256, size_bytes, summary, category, visibility, labels_json, created_at)
values (%s, %s, %s, %s, %s, %s, %s, %s, %d, %s, %s, %s, %s, %s);`,
		sqlString(r.ID), sqlString(r.RunID), sqlString(r.CaseRunID), sqlString(r.StepID), sqlString(r.Kind), sqlString(r.URI),
		sqlString(r.MediaType), sqlString(r.SHA256), r.SizeBytes, sqlString(r.Summary), sqlString(r.Category),
		sqlString(r.Visibility), sqlString(r.LabelsJSON), sqlString(encodeTime(r.CreatedAt)))); err != nil {
		return store.EvidenceRecord{}, fmt.Errorf("record evidence %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) ListEvidence(ctx context.Context, runID string) ([]store.EvidenceRecord, error) {
	var rows []evidenceRecordRow
	if err := s.query(ctx, fmt.Sprintf(`
select id, run_id, case_run_id, step_id, kind, uri, media_type, sha256, size_bytes, summary, category, visibility, labels_json, created_at
from evidence_records where run_id = %s order by created_at, id;`, sqlString(runID)), &rows); err != nil {
		return nil, err
	}
	out := make([]store.EvidenceRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *Store) SaveTraceTopology(ctx context.Context, t store.TraceTopology) (store.TraceTopology, error) {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = utcNow()
	}
	if strings.TrimSpace(t.ID) == "" {
		t.ID = "trace-topology." + strings.ReplaceAll(t.CreatedAt.Format("20060102T150405.000000000Z"), ":", "")
	}
	if strings.TrimSpace(t.Status) == "" {
		t.Status = "unknown"
	}
	if strings.TrimSpace(t.TopologyJSON) == "" {
		t.TopologyJSON = "{}"
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into trace_topologies (id, workflow_run_id, workflow_id, step_id, case_id, request_id, trace_id, status, topology_json, text_topology, created_at)
values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
on conflict(id) do update set
  workflow_run_id = excluded.workflow_run_id,
  workflow_id = excluded.workflow_id,
  step_id = excluded.step_id,
  case_id = excluded.case_id,
  request_id = excluded.request_id,
  trace_id = excluded.trace_id,
  status = excluded.status,
  topology_json = excluded.topology_json,
  text_topology = excluded.text_topology,
  created_at = excluded.created_at;`,
		sqlString(t.ID), sqlString(t.WorkflowRunID), sqlString(t.WorkflowID), sqlString(t.StepID), sqlString(t.CaseID),
		sqlString(t.RequestID), sqlString(t.TraceID), sqlString(t.Status), sqlString(t.TopologyJSON), sqlString(t.TextTopology),
		sqlString(encodeTime(t.CreatedAt)))); err != nil {
		return store.TraceTopology{}, fmt.Errorf("save trace topology %q: %w", t.ID, err)
	}
	return t, nil
}

func (s *Store) ListTraceTopologies(ctx context.Context, workflowRunID string) ([]store.TraceTopology, error) {
	var rows []traceTopologyRow
	if err := s.query(ctx, fmt.Sprintf(`
select id, workflow_run_id, workflow_id, step_id, case_id, request_id, trace_id, status, topology_json, text_topology, created_at
from trace_topologies where workflow_run_id = %s order by created_at, id;`, sqlString(workflowRunID)), &rows); err != nil {
		return nil, err
	}
	out := make([]store.TraceTopology, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *Store) RecordPostProcessTask(ctx context.Context, t store.PostProcessTask) (store.PostProcessTask, error) {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = utcNow()
	}
	if t.StartedAt.IsZero() {
		t.StartedAt = t.CreatedAt
	}
	if t.FinishedAt.IsZero() && t.Status != store.StatusRunning {
		t.FinishedAt = t.StartedAt
	}
	if t.DurationMs == 0 && !t.StartedAt.IsZero() && !t.FinishedAt.IsZero() {
		t.DurationMs = t.FinishedAt.Sub(t.StartedAt).Milliseconds()
		if t.DurationMs < 0 {
			t.DurationMs = 0
		}
	}
	if strings.TrimSpace(t.ID) == "" {
		t.ID = "post-process." + strings.ReplaceAll(t.CreatedAt.Format("20060102T150405.000000000Z"), ":", "")
	}
	if strings.TrimSpace(t.Status) == "" {
		t.Status = store.StatusPassed
	}
	if strings.TrimSpace(t.SummaryJSON) == "" {
		t.SummaryJSON = "{}"
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into post_process_tasks (id, run_id, workflow_id, step_id, case_id, kind, status, started_at, finished_at, duration_ms, error, summary_json, created_at)
values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %d, %s, %s, %s)
on conflict(id) do update set
  run_id = excluded.run_id,
  workflow_id = excluded.workflow_id,
  step_id = excluded.step_id,
  case_id = excluded.case_id,
  kind = excluded.kind,
  status = excluded.status,
  started_at = excluded.started_at,
  finished_at = excluded.finished_at,
  duration_ms = excluded.duration_ms,
  error = excluded.error,
  summary_json = excluded.summary_json,
  created_at = excluded.created_at;`,
		sqlString(t.ID), sqlString(t.RunID), sqlString(t.WorkflowID), sqlString(t.StepID), sqlString(t.CaseID),
		sqlString(t.Kind), sqlString(t.Status), sqlString(encodeTime(t.StartedAt)), sqlString(encodeTime(t.FinishedAt)),
		t.DurationMs, sqlString(t.Error), sqlString(t.SummaryJSON), sqlString(encodeTime(t.CreatedAt)))); err != nil {
		return store.PostProcessTask{}, fmt.Errorf("record post process task %q: %w", t.ID, err)
	}
	return t, nil
}

func (s *Store) ListPostProcessTasks(ctx context.Context, runID string) ([]store.PostProcessTask, error) {
	var rows []postProcessTaskRow
	if err := s.query(ctx, fmt.Sprintf(`
select id, run_id, workflow_id, step_id, case_id, kind, status, started_at, finished_at, duration_ms, error, summary_json, created_at
from post_process_tasks where run_id = %s order by created_at, id;`, sqlString(runID)), &rows); err != nil {
		return nil, err
	}
	out := make([]store.PostProcessTask, 0, len(rows))
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

func (s *Store) UpsertConfigVersion(ctx context.Context, v store.ConfigVersion) (store.ConfigVersion, error) {
	if v.CreatedAt.IsZero() {
		v.CreatedAt = utcNow()
	}
	if v.PublishedAt.IsZero() {
		v.PublishedAt = v.CreatedAt
	}
	active := 0
	if v.Active {
		active = 1
	}
	statements := []string{}
	if v.Active {
		statements = append(statements, "update config_versions set active = 0;")
	}
	statements = append(statements, fmt.Sprintf(`
insert into config_versions (id, profile_id, source_path, bundle_digest, summary_json, active, published_at, created_at)
values (%s, %s, %s, %s, %s, %d, %s, %s)
on conflict(id) do update set
  profile_id = excluded.profile_id,
  source_path = excluded.source_path,
  bundle_digest = excluded.bundle_digest,
  summary_json = excluded.summary_json,
  active = excluded.active,
  published_at = excluded.published_at;`,
		sqlString(v.ID), sqlString(v.ProfileID), sqlString(v.SourcePath), sqlString(v.BundleDigest), sqlString(v.SummaryJSON),
		active, sqlString(encodeTime(v.PublishedAt)), sqlString(encodeTime(v.CreatedAt))))
	if err := s.exec(ctx, strings.Join(statements, "\n")); err != nil {
		return store.ConfigVersion{}, fmt.Errorf("upsert config version %q: %w", v.ID, err)
	}
	return v, nil
}

func (s *Store) GetActiveConfigVersion(ctx context.Context) (store.ConfigVersion, error) {
	var rows []configVersionRow
	if err := s.query(ctx, `
select id, profile_id, source_path, bundle_digest, summary_json, active, published_at, created_at
from config_versions
where active = 1
order by published_at desc, id desc
limit 1;`, &rows); err != nil {
		return store.ConfigVersion{}, err
	}
	if len(rows) == 0 {
		return store.ConfigVersion{}, store.ErrNotFound
	}
	return rows[0].toStore(), nil
}

func (s *Store) UpsertReadModel(ctx context.Context, m store.ReadModel) (store.ReadModel, error) {
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = utcNow()
	}
	if m.GeneratedAt.IsZero() {
		m.GeneratedAt = m.UpdatedAt
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into config_read_model (profile_id, model_key, config_version_id, payload_json, generated_at, updated_at)
values (%s, %s, %s, %s, %s, %s)
on conflict(profile_id, model_key) do update set
  config_version_id = excluded.config_version_id,
  payload_json = excluded.payload_json,
  generated_at = excluded.generated_at,
  updated_at = excluded.updated_at;`,
		sqlString(m.ProfileID), sqlString(m.Key), sqlString(m.ConfigVersionID), sqlString(m.PayloadJSON),
		sqlString(encodeTime(m.GeneratedAt)), sqlString(encodeTime(m.UpdatedAt)))); err != nil {
		return store.ReadModel{}, fmt.Errorf("upsert read model %q/%q: %w", m.ProfileID, m.Key, err)
	}
	return m, nil
}

func (s *Store) GetReadModel(ctx context.Context, profileID string, key string) (store.ReadModel, error) {
	var rows []readModelRow
	if err := s.query(ctx, fmt.Sprintf(`
select profile_id, model_key, config_version_id, payload_json, generated_at, updated_at
from config_read_model
where profile_id = %s and model_key = %s;`, sqlString(profileID), sqlString(key)), &rows); err != nil {
		return store.ReadModel{}, err
	}
	if len(rows) == 0 {
		return store.ReadModel{}, store.ErrNotFound
	}
	return rows[0].toStore(), nil
}

func (s *Store) ReplaceProfileCatalog(ctx context.Context, catalog store.ProfileCatalog) error {
	indexedAt := encodeTime(catalog.IndexedAt)
	if indexedAt == "" {
		indexedAt = encodeTime(utcNow())
	}
	statements := []string{
		"delete from interface_node_case_dependency;",
		"delete from fixture_profile;",
		"delete from workflow_interface_node;",
		"delete from interface_node_case;",
		"delete from interface_node_request_template;",
		"delete from interface_node_field;",
		"delete from interface_node;",
		"delete from workflow_node;",
		"delete from workflow;",
		"delete from node_config;",
		"delete from template_config;",
		"delete from template;",
		"delete from kv;",
		fmt.Sprintf(`insert into kv (key, value, updated_at) values ('active_profile_id', %s, %s);`, sqlString(catalog.ProfileID), sqlString(indexedAt)),
	}
	for index, service := range catalog.Services {
		statements = append(statements, fmt.Sprintf(`
insert into node_config (id, display_name, role, attached_template_ids, git_url, git_branch, repo_env, source_path, container_name, image, docker_service, service_port, management_port, memory_mb, cpu_milli, startup_command, health_url, log_path, status, sort_order)
values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %d, %d, %d, %d, %s, %s, %s, %s, %d);`,
			sqlString(service.ID), sqlString(service.DisplayName), sqlString(service.Kind), sqlString(jsonString(service.AttachedTemplateIDs, "[]")),
			sqlString(service.GitURL), sqlString(service.GitBranch), sqlString(service.RepoEnv), sqlString(service.SourcePath), sqlString(service.ContainerName),
			sqlString(service.Image), sqlString(service.DockerService), service.ServicePort, service.ManagementPort, service.MemoryMb, service.CPUMilli,
			sqlString(service.StartupCommand), sqlString(service.HealthURL), sqlString(service.LogPath), sqlString(stringDefault(service.Status, "active")),
			firstNonZero(service.SortOrder, index)))
	}
	for index, workflow := range catalog.Workflows {
		templateID := "workflow/" + workflow.ID
		configID := templateID + "/config"
		statements = append(statements, fmt.Sprintf(`
insert into template (id, name, kind, status, sort_order)
values (%s, %s, 'workflow', 'active', %d);`, sqlString(templateID), sqlString(workflow.DisplayName), index))
		statements = append(statements, fmt.Sprintf(`
insert into template_config (id, template_id, workflow_id, title, description, config_json, status, sort_order)
values (%s, %s, %s, %s, %s, '{}', 'active', %d);`, sqlString(configID), sqlString(templateID), sqlString(workflow.ID), sqlString(workflow.DisplayName), sqlString(workflow.Description), index))
		statements = append(statements, fmt.Sprintf(`
insert into workflow (id, name, template_id, template_config_id, description, status, sort_order, base_step_timeout_ms, timeout_offset_ms)
values (%s, %s, %s, %s, %s, 'active', %d, %d, %d);`, sqlString(workflow.ID), sqlString(workflow.DisplayName), sqlString(templateID), sqlString(configID), sqlString(workflow.Description), index, workflow.BaseStepTimeoutMs, workflow.TimeoutOffsetMs))
	}
	for index, node := range catalog.InterfaceNodes {
		tagsJSON := jsonString(node.Tags, "[]")
		createdAt := stringDefault(node.CreatedAt, indexedAt)
		updatedAt := stringDefault(node.UpdatedAt, indexedAt)
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node (id, display_name, service_id, operation, method, path, template_id, version, status, tags_json, description, timeout_ms, sort_order, created_at, updated_at)
	values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %d, %d, %s, %s);`,
			sqlString(node.ID), sqlString(node.DisplayName), sqlString(node.ServiceID), sqlString(node.Operation), sqlString(node.Method), sqlString(node.Path),
			sqlString(node.TemplateID), sqlString(stringDefault(node.Version, "v1")), sqlString(stringDefault(node.Status, "active")), sqlString(tagsJSON),
			sqlString(node.Description), node.TimeoutMs, firstNonZero(node.SortOrder, index), sqlString(createdAt), sqlString(updatedAt)))
	}
	for index, field := range catalog.InterfaceFields {
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node_field (id, node_id, direction, field_path, display_name, data_type, required, bindable, port_type, status, sort_order)
	values (%s, %s, %s, %s, %s, %s, %d, %d, %s, %s, %d);`,
			sqlString(field.ID), sqlString(field.NodeID), sqlString(field.Direction), sqlString(field.FieldPath), sqlString(field.DisplayName), sqlString(field.DataType),
			boolInt(field.Required), boolInt(field.Bindable), sqlString(stringDefault(field.PortType, "DATA")), sqlString(stringDefault(field.Status, "active")), firstNonZero(field.SortOrder, index)))
	}
	for index, template := range catalog.RequestTemplates {
		templateID := "request/" + template.ID
		configID := templateID + "/config"
		statements = append(statements, fmt.Sprintf(`
insert into template (id, name, kind, status, sort_order)
values (%s, %s, 'request', 'active', %d);`, sqlString(templateID), sqlString(template.DisplayName), index))
		statements = append(statements, fmt.Sprintf(`
insert into template_config (id, template_id, node_id, scope_type, scope_id, title, config_json, status, sort_order)
values (%s, %s, %s, 'interface_node', %s, %s, %s, 'active', %d);`, sqlString(configID), sqlString(templateID), sqlString(template.NodeID), sqlString(template.NodeID), sqlString(template.DisplayName), sqlString(stringDefault(template.TemplateJSON, "{}")), index))
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node_request_template (id, node_id, name, template_json, status, sort_order, created_at, updated_at)
	values (%s, %s, %s, %s, %s, %d, %s, %s);`,
			sqlString(template.ID), sqlString(template.NodeID), sqlString(template.DisplayName), sqlString(stringDefault(template.TemplateJSON, "{}")),
			sqlString(stringDefault(template.Status, "active")), firstNonZero(template.SortOrder, index), sqlString(indexedAt), sqlString(indexedAt)))
	}
	for index, item := range catalog.APICases {
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node_case (id, node_id, title, description, case_type, scenario, tags_json, priority, owner, payload_template_json, request_template_id, patch_json, render_mode, expected_json, required_for_admission, status, sort_order, created_at, updated_at, case_path, source_kind, source_path, executor_id, base_url, evidence_dir, timeout_seconds, default_overrides_json)
	values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %d, %s, %d, %s, %s, %s, %s, %s, %s, %s, %s, %d, %s);`,
			sqlString(item.ID), sqlString(item.NodeID), sqlString(item.DisplayName), sqlString(item.Description), sqlString(stringDefault(item.CaseType, "api")), sqlString(item.Scenario),
			sqlString(jsonString(item.Tags, "[]")), sqlString(item.Priority), sqlString(item.Owner),
			sqlString(stringDefault(item.PayloadTemplateJSON, "{}")), sqlString(item.RequestTemplateID), sqlString(stringDefault(item.PatchJSON, "[]")),
			sqlString(stringDefault(item.RenderMode, "legacy_payload")), sqlString(stringDefault(item.ExpectedJSON, "{}")), boolInt(item.RequiredForAdmission),
			sqlString(stringDefault(item.Status, "active")), firstNonZero(item.SortOrder, index), sqlString(indexedAt), sqlString(indexedAt),
			sqlString(item.CasePath), sqlString(item.SourceKind), sqlString(item.SourcePath), sqlString(item.ExecutorID),
			sqlString(item.BaseURL), sqlString(item.EvidenceDir), item.TimeoutSeconds, sqlString(stringDefault(item.DefaultOverridesJSON, "{}"))))
	}
	for index, binding := range catalog.WorkflowBindings {
		statements = append(statements, fmt.Sprintf(`
insert into workflow_interface_node (workflow_id, step_id, node_id, case_id, required, sort_order)
values (%s, %s, %s, %s, %d, %d);`, sqlString(binding.WorkflowID), sqlString(binding.StepID), sqlString(binding.NodeID), sqlString(binding.CaseID), boolInt(binding.Required), firstNonZero(binding.SortOrder, index)))
		if binding.NodeID != "" {
			statements = append(statements, fmt.Sprintf(`
insert into workflow_node (workflow_id, node_id, required, sort_order)
values (%s, %s, %d, %d)
on conflict(workflow_id, node_id, relation_type) do nothing;`, sqlString(binding.WorkflowID), sqlString(binding.NodeID), boolInt(binding.Required), firstNonZero(binding.SortOrder, index)))
		}
	}
	for index, fixture := range catalog.Fixtures {
		statements = append(statements, fmt.Sprintf(`
insert into fixture_profile (id, name, source_type, source_workflow_id, source_until_step, ttl_seconds, status, description, sort_order, created_at, updated_at)
values (%s, %s, %s, %s, %s, %d, %s, %s, %d, %s, %s);`,
			sqlString(fixture.ID), sqlString(fixture.DisplayName), sqlString(fixture.Kind), sqlString(fixture.SourceWorkflowID),
			sqlString(fixture.SourceUntilStep), fixture.TTLSeconds, sqlString(stringDefault(fixture.Status, "active")),
			sqlString(fixture.DataJSON), firstNonZero(fixture.SortOrder, index), sqlString(indexedAt), sqlString(indexedAt)))
	}
	for index, dependency := range catalog.CaseDependencies {
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node_case_dependency (id, case_id, fixture_profile_id, required, mappings_json, status, sort_order)
	values (%s, %s, %s, %d, %s, %s, %d);`,
			sqlString(dependency.ID), sqlString(dependency.CaseID), sqlString(dependency.FixtureID), boolInt(dependency.Required),
			sqlString(stringDefault(dependency.MappingsJSON, "[]")), sqlString(stringDefault(dependency.Status, "active")), firstNonZero(dependency.SortOrder, index)))
	}
	for index, config := range catalog.TemplateConfigs {
		if strings.TrimSpace(config.ID) == "" {
			continue
		}
		templateID := stringDefault(config.TemplateID, "template-config/"+config.ID)
		statements = append(statements, fmt.Sprintf(`
insert into template (id, name, kind, status, sort_order)
values (%s, %s, %s, 'active', %d)
on conflict(id) do nothing;`, sqlString(templateID), sqlString(stringDefault(config.Title, templateID)), sqlString(stringDefault(config.ScopeType, "config")), firstNonZero(config.SortOrder, index)))
		statements = append(statements, fmt.Sprintf(`
insert or replace into template_config (id, template_id, node_id, workflow_id, scope_type, scope_id, title, description, config_json, status, sort_order)
values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %d);`,
			sqlString(config.ID), sqlString(templateID), sqlString(config.NodeID), sqlString(config.WorkflowID), sqlString(config.ScopeType),
			sqlString(config.ScopeID), sqlString(config.Title), sqlString(config.Description), sqlString(stringDefault(config.ConfigJSON, "{}")),
			sqlString(stringDefault(config.Status, "active")), firstNonZero(config.SortOrder, index)))
	}
	if err := s.exec(ctx, "begin;\n"+strings.Join(statements, "\n")+"\ncommit;"); err != nil {
		return fmt.Errorf("replace profile catalog index %q: %w", catalog.ProfileID, err)
	}
	return nil
}

func (s *Store) GetProfileCatalog(ctx context.Context) (store.ProfileCatalog, error) {
	index, err := s.GetProfileCatalogIndex(ctx)
	if err != nil {
		return store.ProfileCatalog{}, err
	}
	catalog := store.ProfileCatalog{
		ProfileID: index.ProfileID,
		IndexedAt: index.IndexedAt,
	}

	var services []catalogServiceRow
	if err := s.query(ctx, `select id, display_name, role, attached_template_ids, git_url, git_branch, repo_env, source_path, container_name, image, docker_service, service_port, management_port, memory_mb, cpu_milli, startup_command, health_url, log_path, status, sort_order from node_config order by sort_order, id;`, &services); err != nil {
		return store.ProfileCatalog{}, err
	}
	for _, row := range services {
		catalog.Services = append(catalog.Services, store.CatalogService{
			ID: row.ID, DisplayName: row.DisplayName, Kind: row.Role, AttachedTemplateIDs: stringSliceFromJSON(row.AttachedTemplateIDs),
			GitURL: row.GitURL, GitBranch: row.GitBranch, RepoEnv: row.RepoEnv, SourcePath: row.SourcePath, ContainerName: row.ContainerName,
			Image: row.Image, DockerService: row.DockerService, ServicePort: row.ServicePort, ManagementPort: row.ManagementPort,
			MemoryMb: row.MemoryMb, CPUMilli: row.CPUMilli, StartupCommand: row.StartupCommand, HealthURL: row.HealthURL,
			LogPath: row.LogPath, Status: row.Status, SortOrder: row.SortOrder,
		})
	}

	var workflows []catalogWorkflowRow
	if err := s.query(ctx, `select id, name, description, base_step_timeout_ms, timeout_offset_ms from workflow order by sort_order, id;`, &workflows); err != nil {
		return store.ProfileCatalog{}, err
	}
	for _, row := range workflows {
		catalog.Workflows = append(catalog.Workflows, store.CatalogWorkflow{ID: row.ID, DisplayName: row.Name, Description: row.Description, BaseStepTimeoutMs: row.BaseStepTimeoutMs, TimeoutOffsetMs: row.TimeoutOffsetMs})
	}

	var nodes []catalogInterfaceNodeRow
	if err := s.query(ctx, `select id, display_name, service_id, operation, method, path, template_id, version, status, tags_json, description, timeout_ms, sort_order, created_at, updated_at from interface_node order by sort_order, id;`, &nodes); err != nil {
		return store.ProfileCatalog{}, err
	}
	for _, row := range nodes {
		catalog.InterfaceNodes = append(catalog.InterfaceNodes, store.CatalogInterfaceNode{
			ID: row.ID, DisplayName: row.DisplayName, ServiceID: row.ServiceID, Operation: row.Operation,
			Method: row.Method, Path: row.Path, TemplateID: row.TemplateID, Version: row.Version, Status: row.Status,
			Tags: stringSliceFromJSON(row.TagsJSON), Description: row.Description, SortOrder: row.SortOrder,
			TimeoutMs: row.TimeoutMs, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}

	var fields []catalogInterfaceNodeFieldRow
	if err := s.query(ctx, `select id, node_id, direction, field_path, display_name, data_type, required, bindable, port_type, status, sort_order from interface_node_field order by node_id, direction, sort_order, id;`, &fields); err != nil {
		return store.ProfileCatalog{}, err
	}
	for _, row := range fields {
		catalog.InterfaceFields = append(catalog.InterfaceFields, store.CatalogInterfaceNodeField{
			ID: row.ID, NodeID: row.NodeID, Direction: row.Direction, FieldPath: row.FieldPath, DisplayName: row.DisplayName,
			DataType: row.DataType, Required: row.Required != 0, Bindable: row.Bindable != 0, PortType: row.PortType,
			Status: row.Status, SortOrder: row.SortOrder,
		})
	}

	var templates []catalogRequestTemplateRow
	if err := s.query(ctx, `select id, node_id, name, template_json, version, status, sort_order from interface_node_request_template order by node_id, sort_order, id;`, &templates); err != nil {
		return store.ProfileCatalog{}, err
	}
	nodeByID := map[string]store.CatalogInterfaceNode{}
	for _, node := range catalog.InterfaceNodes {
		nodeByID[node.ID] = node
	}
	for _, row := range templates {
		node := nodeByID[row.NodeID]
		catalog.RequestTemplates = append(catalog.RequestTemplates, store.CatalogRequestTemplate{
			ID: row.ID, DisplayName: row.Name, NodeID: row.NodeID, Method: node.Method, Path: node.Path,
			TemplateJSON: row.TemplateJSON, Version: row.Version, Status: row.Status, SortOrder: row.SortOrder,
		})
	}

	var cases []catalogAPICaseRow
	if err := s.query(ctx, `select id, node_id, title, description, case_type, scenario, tags_json, priority, owner, payload_template_json, request_template_id, patch_json, render_mode, expected_json, required_for_admission, status, sort_order, case_path, source_kind, source_path, executor_id, base_url, evidence_dir, timeout_seconds, default_overrides_json from interface_node_case order by node_id, sort_order, id;`, &cases); err != nil {
		return store.ProfileCatalog{}, err
	}
	for _, row := range cases {
		catalog.APICases = append(catalog.APICases, store.CatalogAPICase{
			ID: row.ID, DisplayName: row.Title, Description: row.Description, NodeID: row.NodeID, CaseType: row.CaseType, Scenario: row.Scenario,
			Tags: stringSliceFromJSON(row.TagsJSON), Priority: row.Priority, Owner: row.Owner,
			PayloadTemplateJSON: row.PayloadTemplateJSON, RequestTemplateID: row.RequestTemplateID, PatchJSON: row.PatchJSON,
			RenderMode: row.RenderMode, ExpectedJSON: row.ExpectedJSON, RequiredForAdmission: row.RequiredForAdmission != 0,
			Status: row.Status, SortOrder: row.SortOrder, CasePath: row.CasePath, SourceKind: row.SourceKind, SourcePath: row.SourcePath,
			ExecutorID: row.ExecutorID, BaseURL: row.BaseURL, EvidenceDir: row.EvidenceDir, TimeoutSeconds: row.TimeoutSeconds,
			DefaultOverridesJSON: row.DefaultOverridesJSON,
		})
	}

	var dependencies []catalogCaseDependencyRow
	if err := s.query(ctx, `select id, case_id, fixture_profile_id, required, mappings_json, status, sort_order from interface_node_case_dependency order by case_id, sort_order, id;`, &dependencies); err != nil {
		return store.ProfileCatalog{}, err
	}
	for _, row := range dependencies {
		catalog.CaseDependencies = append(catalog.CaseDependencies, store.CatalogCaseDependency{
			ID: row.ID, CaseID: row.CaseID, FixtureID: row.FixtureProfileID, Required: row.Required != 0,
			MappingsJSON: row.MappingsJSON, Status: row.Status, SortOrder: row.SortOrder,
		})
	}

	var fixtures []catalogFixtureRow
	if err := s.query(ctx, `select id, name, source_type, source_workflow_id, source_until_step, ttl_seconds, status, description, sort_order from fixture_profile order by sort_order, id;`, &fixtures); err != nil {
		return store.ProfileCatalog{}, err
	}
	for _, row := range fixtures {
		catalog.Fixtures = append(catalog.Fixtures, store.CatalogFixture{
			ID: row.ID, DisplayName: row.Name, Kind: row.SourceType, DataJSON: row.Description,
			SourceWorkflowID: row.SourceWorkflowID, SourceUntilStep: row.SourceUntilStep, TTLSeconds: row.TTLSeconds,
			Status: row.Status, SortOrder: row.SortOrder,
		})
	}

	var bindings []catalogWorkflowBindingRow
	if err := s.query(ctx, `select workflow_id, step_id, node_id, case_id, required, sort_order from workflow_interface_node order by workflow_id, sort_order, step_id;`, &bindings); err != nil {
		return store.ProfileCatalog{}, err
	}
	for _, row := range bindings {
		catalog.WorkflowBindings = append(catalog.WorkflowBindings, store.CatalogWorkflowBinding{
			WorkflowID: row.WorkflowID, StepID: row.StepID, NodeID: row.NodeID, CaseID: row.CaseID, Required: row.Required != 0,
			SortOrder: row.SortOrder,
		})
	}

	var configs []catalogTemplateConfigRow
	if err := s.query(ctx, `select id, template_id, node_id, workflow_id, scope_type, scope_id, title, description, config_json, status, sort_order from template_config order by workflow_id, scope_type, sort_order, id;`, &configs); err != nil {
		return store.ProfileCatalog{}, err
	}
	for _, row := range configs {
		catalog.TemplateConfigs = append(catalog.TemplateConfigs, store.CatalogTemplateConfig{
			ID: row.ID, TemplateID: row.TemplateID, NodeID: row.NodeID, WorkflowID: row.WorkflowID, ScopeType: row.ScopeType,
			ScopeID: row.ScopeID, Title: row.Title, Description: row.Description, ConfigJSON: row.ConfigJSON,
			Status: row.Status, SortOrder: row.SortOrder,
		})
	}

	return catalog, nil
}

func (s *Store) GetProfileCatalogIndex(ctx context.Context) (store.ProfileCatalogIndex, error) {
	var rows []profileCatalogIndexRow
	if err := s.query(ctx, `
select
  coalesce((select value from kv where key = 'active_profile_id'), '') as profile_id,
  coalesce((select updated_at from kv where key = 'active_profile_id'), '') as indexed_at,
  (select count(*) from node_config) as services,
  (select count(*) from workflow) as workflows,
  (select count(*) from interface_node) as interface_nodes,
  (select count(*) from interface_node_case) as api_cases,
  (select count(*) from interface_node_request_template) as request_templates,
  (select count(*) from workflow_interface_node) as workflow_bindings,
  (select count(*) from interface_node_case_dependency) as case_dependencies,
  (select count(*) from fixture_profile) as fixtures,
  (select count(*) from template) as templates,
  (select count(*) from template_config) as template_configs;`, &rows); err != nil {
		return store.ProfileCatalogIndex{}, err
	}
	if len(rows) == 0 || rows[0].ProfileID == "" {
		return store.ProfileCatalogIndex{}, store.ErrNotFound
	}
	return rows[0].toStore(), nil
}

func (s *Store) UpsertEnvironment(ctx context.Context, e store.Environment) (store.Environment, error) {
	if err := store.ValidateEnvironmentDefinitionSize(e); err != nil {
		return store.Environment{}, err
	}
	now := utcNow()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = now
	}
	if err := s.exec(ctx, fmt.Sprintf(`
insert into environments (
  id, display_name, description, status, verified, services_json, repos_json, compose_json,
  health_checks_json, verification_workflow_id, last_verification_run_id, last_verification_status,
  evidence_complete, topology_complete, last_verified_at, summary_json, created_at, updated_at
)
values (%s, %s, %s, %s, %d, %s, %s, %s, %s, %s, %s, %s, %d, %d, %s, %s, %s, %s)
on conflict(id) do update set
  display_name = excluded.display_name,
  description = excluded.description,
  status = excluded.status,
  verified = excluded.verified,
  services_json = excluded.services_json,
  repos_json = excluded.repos_json,
  compose_json = excluded.compose_json,
  health_checks_json = excluded.health_checks_json,
  verification_workflow_id = excluded.verification_workflow_id,
  last_verification_run_id = excluded.last_verification_run_id,
  last_verification_status = excluded.last_verification_status,
  evidence_complete = excluded.evidence_complete,
  topology_complete = excluded.topology_complete,
  last_verified_at = excluded.last_verified_at,
  summary_json = excluded.summary_json,
  updated_at = excluded.updated_at;`,
		sqlString(e.ID), sqlString(e.DisplayName), sqlString(e.Description), sqlString(stringDefault(e.Status, "draft")),
		boolInt(e.Verified), sqlString(stringDefault(e.ServicesJSON, "[]")), sqlString(stringDefault(e.ReposJSON, "{}")),
		sqlString(stringDefault(e.ComposeJSON, "{}")), sqlString(stringDefault(e.HealthChecksJSON, "[]")), sqlString(e.VerificationWorkflowID),
		sqlString(e.LastVerificationRunID), sqlString(e.LastVerificationStatus), boolInt(e.EvidenceComplete), boolInt(e.TopologyComplete),
		sqlString(encodeTime(e.LastVerifiedAt)), sqlString(stringDefault(e.SummaryJSON, "{}")), sqlString(encodeTime(e.CreatedAt)), sqlString(encodeTime(e.UpdatedAt)))); err != nil {
		return store.Environment{}, fmt.Errorf("upsert environment %q: %w", e.ID, err)
	}
	return e, nil
}

func (s *Store) GetEnvironment(ctx context.Context, id string) (store.Environment, error) {
	var rows []environmentRow
	if err := s.query(ctx, fmt.Sprintf(`
select id, display_name, description, status, verified, services_json, repos_json, compose_json,
  health_checks_json, verification_workflow_id, last_verification_run_id, last_verification_status,
  evidence_complete, topology_complete, last_verified_at, summary_json, created_at, updated_at
from environments where id = %s;`, sqlString(id)), &rows); err != nil {
		return store.Environment{}, err
	}
	if len(rows) == 0 {
		return store.Environment{}, store.ErrNotFound
	}
	return rows[0].toStore(), nil
}

func (s *Store) ListEnvironments(ctx context.Context) ([]store.Environment, error) {
	var rows []environmentRow
	if err := s.query(ctx, `
select id, display_name, description, status, verified, services_json, repos_json, compose_json,
  health_checks_json, verification_workflow_id, last_verification_run_id, last_verification_status,
  evidence_complete, topology_complete, last_verified_at, summary_json, created_at, updated_at
from environments order by verified desc, updated_at desc, id;`, &rows); err != nil {
		return nil, err
	}
	out := make([]store.Environment, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStore())
	}
	return out, nil
}

func (s *Store) ReplaceEnvironmentComponentGraph(ctx context.Context, envID string, graph store.EnvironmentComponentGraph) error {
	if err := store.ValidateEnvironmentComponentGraph(envID, graph); err != nil {
		return err
	}
	now := utcNow()
	statements := []string{
		fmt.Sprintf("delete from component_config_assets where env_id = %s;", sqlString(envID)),
		fmt.Sprintf("delete from component_dependencies where env_id = %s;", sqlString(envID)),
		fmt.Sprintf("delete from environment_components where env_id = %s;", sqlString(envID)),
	}
	for _, component := range graph.Components {
		if component.CreatedAt.IsZero() {
			component.CreatedAt = now
		}
		if component.UpdatedAt.IsZero() {
			component.UpdatedAt = now
		}
		statements = append(statements, fmt.Sprintf(`
insert into environment_components (
  env_id, component_id, display_name, kind, role, compose_service, image, required,
  runtime_json, healthcheck_json, summary_json, created_at, updated_at
) values (%s, %s, %s, %s, %s, %s, %s, %d, %s, %s, %s, %s, %s);`,
			sqlString(envID), sqlString(component.ComponentID), sqlString(component.DisplayName), sqlString(component.Kind),
			sqlString(component.Role), sqlString(component.ComposeService), sqlString(component.Image), boolInt(component.Required),
			sqlString(stringDefault(component.RuntimeJSON, "{}")), sqlString(stringDefault(component.HealthCheckJSON, "{}")),
			sqlString(stringDefault(component.SummaryJSON, "{}")), sqlString(encodeTime(component.CreatedAt)), sqlString(encodeTime(component.UpdatedAt))))
	}
	for _, dep := range graph.Dependencies {
		if dep.CreatedAt.IsZero() {
			dep.CreatedAt = now
		}
		if dep.UpdatedAt.IsZero() {
			dep.UpdatedAt = now
		}
		statements = append(statements, fmt.Sprintf(`
insert into component_dependencies (
  env_id, consumer_component_id, provider_component_id, phase, capability, required,
  profile_json, created_at, updated_at
) values (%s, %s, %s, %s, %s, %d, %s, %s, %s);`,
			sqlString(envID), sqlString(dep.ConsumerComponentID), sqlString(dep.ProviderComponentID),
			sqlString(dep.Phase), sqlString(dep.Capability), boolInt(dep.Required),
			sqlString(stringDefault(dep.ProfileJSON, "{}")), sqlString(encodeTime(dep.CreatedAt)), sqlString(encodeTime(dep.UpdatedAt))))
	}
	for _, asset := range graph.Assets {
		if asset.CreatedAt.IsZero() {
			asset.CreatedAt = now
		}
		if asset.UpdatedAt.IsZero() {
			asset.UpdatedAt = now
		}
		if strings.TrimSpace(asset.TargetComponentID) == "" {
			asset.TargetComponentID = asset.OwnerComponentID
		}
		statements = append(statements, fmt.Sprintf(`
insert into component_config_assets (
  env_id, owner_component_id, asset_id, asset_kind, target_component_id, target_path,
  content_inline, remote_ref_json, sha256, size_bytes, apply_order, sensitive,
  summary_json, created_at, updated_at
) values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %d, %d, %d, %s, %s, %s);`,
			sqlString(envID), sqlString(asset.OwnerComponentID), sqlString(asset.AssetID), sqlString(asset.AssetKind),
			sqlString(asset.TargetComponentID), sqlString(asset.TargetPath), sqlString(asset.ContentInline),
			sqlString(stringDefault(asset.RemoteRefJSON, "{}")), sqlString(asset.SHA256), asset.SizeBytes,
			asset.ApplyOrder, boolInt(asset.Sensitive), sqlString(stringDefault(asset.SummaryJSON, "{}")),
			sqlString(encodeTime(asset.CreatedAt)), sqlString(encodeTime(asset.UpdatedAt))))
	}
	return s.exec(ctx, "begin;\n"+strings.Join(statements, "\n")+"\ncommit;")
}

func (s *Store) GetEnvironmentComponentGraph(ctx context.Context, envID string) (store.EnvironmentComponentGraph, error) {
	var componentRows []environmentComponentRow
	if err := s.query(ctx, fmt.Sprintf(`
select env_id, component_id, display_name, kind, role, compose_service, image, required,
  runtime_json, healthcheck_json, summary_json, created_at, updated_at
from environment_components
where env_id = %s
order by component_id;`, sqlString(envID)), &componentRows); err != nil {
		return store.EnvironmentComponentGraph{}, err
	}
	var dependencyRows []componentDependencyRow
	if err := s.query(ctx, fmt.Sprintf(`
select env_id, consumer_component_id, provider_component_id, phase, capability, required,
  profile_json, created_at, updated_at
from component_dependencies
where env_id = %s
order by consumer_component_id, provider_component_id, phase, capability;`, sqlString(envID)), &dependencyRows); err != nil {
		return store.EnvironmentComponentGraph{}, err
	}
	var assetRows []componentConfigAssetRow
	if err := s.query(ctx, fmt.Sprintf(`
select env_id, owner_component_id, asset_id, asset_kind, target_component_id, target_path,
  content_inline, remote_ref_json, sha256, size_bytes, apply_order, sensitive,
  summary_json, created_at, updated_at
from component_config_assets
where env_id = %s
order by owner_component_id, apply_order, asset_id;`, sqlString(envID)), &assetRows); err != nil {
		return store.EnvironmentComponentGraph{}, err
	}
	graph := store.EnvironmentComponentGraph{
		Components:   make([]store.EnvironmentComponent, 0, len(componentRows)),
		Dependencies: make([]store.ComponentDependency, 0, len(dependencyRows)),
		Assets:       make([]store.ComponentConfigAsset, 0, len(assetRows)),
	}
	for _, row := range componentRows {
		graph.Components = append(graph.Components, row.toStore())
	}
	for _, row := range dependencyRows {
		graph.Dependencies = append(graph.Dependencies, row.toStore())
	}
	for _, row := range assetRows {
		graph.Assets = append(graph.Assets, row.toStore())
	}
	return graph, nil
}

func (s *Store) exec(ctx context.Context, statement string) error {
	out, err := sqliteCommand(ctx, false, s.path, statement)
	if err != nil {
		return fmt.Errorf("run sqlite statement: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func stringDefault(value string, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func jsonString(value any, defaultValue string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return defaultValue
	}
	return string(raw)
}

func stringSliceFromJSON(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	return out
}

func firstNonZero(value int, defaultValue int) int {
	if value != 0 {
		return value
	}
	return defaultValue
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
	ID            string `json:"id"`
	ProfileID     string `json:"profile_id"`
	EnvironmentID string `json:"environment_id"`
	WorkflowID    string `json:"workflow_id"`
	Status        string `json:"status"`
	EvidenceRoot  string `json:"evidence_root"`
	SummaryJSON   string `json:"summary_json"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type catalogServiceRow struct {
	ID                  string `json:"id"`
	DisplayName         string `json:"display_name"`
	Role                string `json:"role"`
	AttachedTemplateIDs string `json:"attached_template_ids"`
	GitURL              string `json:"git_url"`
	GitBranch           string `json:"git_branch"`
	RepoEnv             string `json:"repo_env"`
	SourcePath          string `json:"source_path"`
	ContainerName       string `json:"container_name"`
	Image               string `json:"image"`
	DockerService       string `json:"docker_service"`
	ServicePort         int    `json:"service_port"`
	ManagementPort      int    `json:"management_port"`
	MemoryMb            int    `json:"memory_mb"`
	CPUMilli            int    `json:"cpu_milli"`
	StartupCommand      string `json:"startup_command"`
	HealthURL           string `json:"health_url"`
	LogPath             string `json:"log_path"`
	Status              string `json:"status"`
	SortOrder           int    `json:"sort_order"`
}

type catalogWorkflowRow struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	BaseStepTimeoutMs int    `json:"base_step_timeout_ms"`
	TimeoutOffsetMs   int    `json:"timeout_offset_ms"`
}

type catalogInterfaceNodeRow struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	ServiceID   string `json:"service_id"`
	Operation   string `json:"operation"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	TemplateID  string `json:"template_id"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	TagsJSON    string `json:"tags_json"`
	Description string `json:"description"`
	TimeoutMs   int    `json:"timeout_ms"`
	SortOrder   int    `json:"sort_order"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type catalogInterfaceNodeFieldRow struct {
	ID          string `json:"id"`
	NodeID      string `json:"node_id"`
	Direction   string `json:"direction"`
	FieldPath   string `json:"field_path"`
	DisplayName string `json:"display_name"`
	DataType    string `json:"data_type"`
	Required    int    `json:"required"`
	Bindable    int    `json:"bindable"`
	PortType    string `json:"port_type"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sort_order"`
}

type catalogRequestTemplateRow struct {
	ID           string `json:"id"`
	NodeID       string `json:"node_id"`
	Name         string `json:"name"`
	TemplateJSON string `json:"template_json"`
	Version      string `json:"version"`
	Status       string `json:"status"`
	SortOrder    int    `json:"sort_order"`
}

type catalogAPICaseRow struct {
	ID                   string `json:"id"`
	NodeID               string `json:"node_id"`
	Title                string `json:"title"`
	Description          string `json:"description"`
	CaseType             string `json:"case_type"`
	Scenario             string `json:"scenario"`
	TagsJSON             string `json:"tags_json"`
	Priority             string `json:"priority"`
	Owner                string `json:"owner"`
	PayloadTemplateJSON  string `json:"payload_template_json"`
	RequestTemplateID    string `json:"request_template_id"`
	PatchJSON            string `json:"patch_json"`
	RenderMode           string `json:"render_mode"`
	ExpectedJSON         string `json:"expected_json"`
	RequiredForAdmission int    `json:"required_for_admission"`
	Status               string `json:"status"`
	SortOrder            int    `json:"sort_order"`
	CasePath             string `json:"case_path"`
	SourceKind           string `json:"source_kind"`
	SourcePath           string `json:"source_path"`
	ExecutorID           string `json:"executor_id"`
	BaseURL              string `json:"base_url"`
	EvidenceDir          string `json:"evidence_dir"`
	TimeoutSeconds       int    `json:"timeout_seconds"`
	DefaultOverridesJSON string `json:"default_overrides_json"`
}

type catalogCaseDependencyRow struct {
	ID               string `json:"id"`
	CaseID           string `json:"case_id"`
	FixtureProfileID string `json:"fixture_profile_id"`
	Required         int    `json:"required"`
	MappingsJSON     string `json:"mappings_json"`
	Status           string `json:"status"`
	SortOrder        int    `json:"sort_order"`
}

type catalogFixtureRow struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	SourceType       string `json:"source_type"`
	SourceWorkflowID string `json:"source_workflow_id"`
	SourceUntilStep  string `json:"source_until_step"`
	TTLSeconds       int    `json:"ttl_seconds"`
	Status           string `json:"status"`
	Description      string `json:"description"`
	SortOrder        int    `json:"sort_order"`
}

type catalogWorkflowBindingRow struct {
	WorkflowID string `json:"workflow_id"`
	StepID     string `json:"step_id"`
	NodeID     string `json:"node_id"`
	CaseID     string `json:"case_id"`
	Required   int    `json:"required"`
	SortOrder  int    `json:"sort_order"`
}

type catalogTemplateConfigRow struct {
	ID          string `json:"id"`
	TemplateID  string `json:"template_id"`
	NodeID      string `json:"node_id"`
	WorkflowID  string `json:"workflow_id"`
	ScopeType   string `json:"scope_type"`
	ScopeID     string `json:"scope_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ConfigJSON  string `json:"config_json"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sort_order"`
}

func (r runRow) toStore() store.Run {
	return store.Run{
		ID:            r.ID,
		ProfileID:     r.ProfileID,
		EnvironmentID: r.EnvironmentID,
		WorkflowID:    r.WorkflowID,
		Status:        r.Status,
		EvidenceRoot:  r.EvidenceRoot,
		SummaryJSON:   r.SummaryJSON,
		StartedAt:     decodeTime(r.StartedAt),
		FinishedAt:    decodeTime(r.FinishedAt),
		CreatedAt:     decodeTime(r.CreatedAt),
		UpdatedAt:     decodeTime(r.UpdatedAt),
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

type apiCaseRunRecordRow struct {
	RunID           string `json:"run_id"`
	RunProfileID    string `json:"run_profile_id"`
	RunWorkflowID   string `json:"run_workflow_id"`
	RunStatus       string `json:"run_status"`
	RunEvidenceRoot string `json:"run_evidence_root"`
	RunSummaryJSON  string `json:"run_summary_json"`
	RunStartedAt    string `json:"run_started_at"`
	RunFinishedAt   string `json:"run_finished_at"`
	RunCreatedAt    string `json:"run_created_at"`
	RunUpdatedAt    string `json:"run_updated_at"`

	CaseRunID                   string `json:"case_run_id"`
	CaseRunRunID                string `json:"case_run_run_id"`
	CaseRunCaseID               string `json:"case_run_case_id"`
	CaseRunStatus               string `json:"case_run_status"`
	CaseRunRequestSummaryJSON   string `json:"case_run_request_summary_json"`
	CaseRunAssertionSummaryJSON string `json:"case_run_assertion_summary_json"`
	CaseRunStartedAt            string `json:"case_run_started_at"`
	CaseRunFinishedAt           string `json:"case_run_finished_at"`
	CaseRunCreatedAt            string `json:"case_run_created_at"`
}

func (r apiCaseRunRecordRow) toStore() store.APICaseRunRecord {
	return store.APICaseRunRecord{
		Run: store.Run{
			ID:           r.RunID,
			ProfileID:    r.RunProfileID,
			WorkflowID:   r.RunWorkflowID,
			Status:       r.RunStatus,
			EvidenceRoot: r.RunEvidenceRoot,
			SummaryJSON:  r.RunSummaryJSON,
			StartedAt:    decodeTime(r.RunStartedAt),
			FinishedAt:   decodeTime(r.RunFinishedAt),
			CreatedAt:    decodeTime(r.RunCreatedAt),
			UpdatedAt:    decodeTime(r.RunUpdatedAt),
		},
		CaseRun: store.APICaseRun{
			ID:                   r.CaseRunID,
			RunID:                r.CaseRunRunID,
			CaseID:               r.CaseRunCaseID,
			Status:               r.CaseRunStatus,
			RequestSummaryJSON:   r.CaseRunRequestSummaryJSON,
			AssertionSummaryJSON: r.CaseRunAssertionSummaryJSON,
			StartedAt:            decodeTime(r.CaseRunStartedAt),
			FinishedAt:           decodeTime(r.CaseRunFinishedAt),
			CreatedAt:            decodeTime(r.CaseRunCreatedAt),
		},
	}
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
	ID         string `json:"id"`
	RunID      string `json:"run_id"`
	CaseRunID  string `json:"case_run_id"`
	StepID     string `json:"step_id"`
	Kind       string `json:"kind"`
	URI        string `json:"uri"`
	MediaType  string `json:"media_type"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"size_bytes"`
	Summary    string `json:"summary"`
	Category   string `json:"category"`
	Visibility string `json:"visibility"`
	LabelsJSON string `json:"labels_json"`
	CreatedAt  string `json:"created_at"`
}

func (r evidenceRecordRow) toStore() store.EvidenceRecord {
	return store.EvidenceRecord{
		ID:         r.ID,
		RunID:      r.RunID,
		CaseRunID:  r.CaseRunID,
		StepID:     r.StepID,
		Kind:       r.Kind,
		URI:        r.URI,
		MediaType:  r.MediaType,
		SHA256:     r.SHA256,
		SizeBytes:  r.SizeBytes,
		Summary:    r.Summary,
		Category:   r.Category,
		Visibility: r.Visibility,
		LabelsJSON: r.LabelsJSON,
		CreatedAt:  decodeTime(r.CreatedAt),
	}
}

type traceTopologyRow struct {
	ID            string `json:"id"`
	WorkflowRunID string `json:"workflow_run_id"`
	WorkflowID    string `json:"workflow_id"`
	StepID        string `json:"step_id"`
	CaseID        string `json:"case_id"`
	RequestID     string `json:"request_id"`
	TraceID       string `json:"trace_id"`
	Status        string `json:"status"`
	TopologyJSON  string `json:"topology_json"`
	TextTopology  string `json:"text_topology"`
	CreatedAt     string `json:"created_at"`
}

func (r traceTopologyRow) toStore() store.TraceTopology {
	return store.TraceTopology{
		ID:            r.ID,
		WorkflowRunID: r.WorkflowRunID,
		WorkflowID:    r.WorkflowID,
		StepID:        r.StepID,
		CaseID:        r.CaseID,
		RequestID:     r.RequestID,
		TraceID:       r.TraceID,
		Status:        r.Status,
		TopologyJSON:  r.TopologyJSON,
		TextTopology:  r.TextTopology,
		CreatedAt:     decodeTime(r.CreatedAt),
	}
}

type postProcessTaskRow struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	WorkflowID  string `json:"workflow_id"`
	StepID      string `json:"step_id"`
	CaseID      string `json:"case_id"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at"`
	DurationMs  int64  `json:"duration_ms"`
	Error       string `json:"error"`
	SummaryJSON string `json:"summary_json"`
	CreatedAt   string `json:"created_at"`
}

func (r postProcessTaskRow) toStore() store.PostProcessTask {
	return store.PostProcessTask{
		ID:          r.ID,
		RunID:       r.RunID,
		WorkflowID:  r.WorkflowID,
		StepID:      r.StepID,
		CaseID:      r.CaseID,
		Kind:        r.Kind,
		Status:      r.Status,
		StartedAt:   decodeTime(r.StartedAt),
		FinishedAt:  decodeTime(r.FinishedAt),
		DurationMs:  r.DurationMs,
		Error:       r.Error,
		SummaryJSON: r.SummaryJSON,
		CreatedAt:   decodeTime(r.CreatedAt),
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

type configVersionRow struct {
	ID           string `json:"id"`
	ProfileID    string `json:"profile_id"`
	SourcePath   string `json:"source_path"`
	BundleDigest string `json:"bundle_digest"`
	SummaryJSON  string `json:"summary_json"`
	Active       int    `json:"active"`
	PublishedAt  string `json:"published_at"`
	CreatedAt    string `json:"created_at"`
}

func (r configVersionRow) toStore() store.ConfigVersion {
	return store.ConfigVersion{
		ID:           r.ID,
		ProfileID:    r.ProfileID,
		SourcePath:   r.SourcePath,
		BundleDigest: r.BundleDigest,
		SummaryJSON:  r.SummaryJSON,
		Active:       r.Active != 0,
		PublishedAt:  decodeTime(r.PublishedAt),
		CreatedAt:    decodeTime(r.CreatedAt),
	}
}

type readModelRow struct {
	ProfileID       string `json:"profile_id"`
	Key             string `json:"model_key"`
	ConfigVersionID string `json:"config_version_id"`
	PayloadJSON     string `json:"payload_json"`
	GeneratedAt     string `json:"generated_at"`
	UpdatedAt       string `json:"updated_at"`
}

func (r readModelRow) toStore() store.ReadModel {
	return store.ReadModel{
		ProfileID:       r.ProfileID,
		Key:             r.Key,
		ConfigVersionID: r.ConfigVersionID,
		PayloadJSON:     r.PayloadJSON,
		GeneratedAt:     decodeTime(r.GeneratedAt),
		UpdatedAt:       decodeTime(r.UpdatedAt),
	}
}

type profileCatalogIndexRow struct {
	ProfileID        string `json:"profile_id"`
	IndexedAt        string `json:"indexed_at"`
	Services         int    `json:"services"`
	Workflows        int    `json:"workflows"`
	InterfaceNodes   int    `json:"interface_nodes"`
	APICases         int    `json:"api_cases"`
	RequestTemplates int    `json:"request_templates"`
	WorkflowBindings int    `json:"workflow_bindings"`
	CaseDependencies int    `json:"case_dependencies"`
	Fixtures         int    `json:"fixtures"`
	Templates        int    `json:"templates"`
	TemplateConfigs  int    `json:"template_configs"`
}

func (r profileCatalogIndexRow) toStore() store.ProfileCatalogIndex {
	return store.ProfileCatalogIndex{
		ProfileID: r.ProfileID,
		IndexedAt: decodeTime(r.IndexedAt),
		Counts: store.ProfileCatalogCounts{
			Services:         r.Services,
			Workflows:        r.Workflows,
			InterfaceNodes:   r.InterfaceNodes,
			APICases:         r.APICases,
			RequestTemplates: r.RequestTemplates,
			WorkflowBindings: r.WorkflowBindings,
			CaseDependencies: r.CaseDependencies,
			Fixtures:         r.Fixtures,
			Templates:        r.Templates,
			TemplateConfigs:  r.TemplateConfigs,
		},
	}
}

type environmentRow struct {
	ID                     string `json:"id"`
	DisplayName            string `json:"display_name"`
	Description            string `json:"description"`
	Status                 string `json:"status"`
	Verified               int    `json:"verified"`
	ServicesJSON           string `json:"services_json"`
	ReposJSON              string `json:"repos_json"`
	ComposeJSON            string `json:"compose_json"`
	HealthChecksJSON       string `json:"health_checks_json"`
	VerificationWorkflowID string `json:"verification_workflow_id"`
	LastVerificationRunID  string `json:"last_verification_run_id"`
	LastVerificationStatus string `json:"last_verification_status"`
	EvidenceComplete       int    `json:"evidence_complete"`
	TopologyComplete       int    `json:"topology_complete"`
	LastVerifiedAt         string `json:"last_verified_at"`
	SummaryJSON            string `json:"summary_json"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
}

type environmentComponentRow struct {
	EnvID           string `json:"env_id"`
	ComponentID     string `json:"component_id"`
	DisplayName     string `json:"display_name"`
	Kind            string `json:"kind"`
	Role            string `json:"role"`
	ComposeService  string `json:"compose_service"`
	Image           string `json:"image"`
	Required        int    `json:"required"`
	RuntimeJSON     string `json:"runtime_json"`
	HealthCheckJSON string `json:"healthcheck_json"`
	SummaryJSON     string `json:"summary_json"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func (r environmentComponentRow) toStore() store.EnvironmentComponent {
	return store.EnvironmentComponent{
		EnvID:           r.EnvID,
		ComponentID:     r.ComponentID,
		DisplayName:     r.DisplayName,
		Kind:            r.Kind,
		Role:            r.Role,
		ComposeService:  r.ComposeService,
		Image:           r.Image,
		Required:        r.Required != 0,
		RuntimeJSON:     normalizeJSONText(r.RuntimeJSON),
		HealthCheckJSON: normalizeJSONText(r.HealthCheckJSON),
		SummaryJSON:     normalizeJSONText(r.SummaryJSON),
		CreatedAt:       decodeTime(r.CreatedAt),
		UpdatedAt:       decodeTime(r.UpdatedAt),
	}
}

type componentDependencyRow struct {
	EnvID               string `json:"env_id"`
	ConsumerComponentID string `json:"consumer_component_id"`
	ProviderComponentID string `json:"provider_component_id"`
	Phase               string `json:"phase"`
	Capability          string `json:"capability"`
	Required            int    `json:"required"`
	ProfileJSON         string `json:"profile_json"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

func (r componentDependencyRow) toStore() store.ComponentDependency {
	return store.ComponentDependency{
		EnvID:               r.EnvID,
		ConsumerComponentID: r.ConsumerComponentID,
		ProviderComponentID: r.ProviderComponentID,
		Phase:               r.Phase,
		Capability:          r.Capability,
		Required:            r.Required != 0,
		ProfileJSON:         normalizeJSONText(r.ProfileJSON),
		CreatedAt:           decodeTime(r.CreatedAt),
		UpdatedAt:           decodeTime(r.UpdatedAt),
	}
}

type componentConfigAssetRow struct {
	EnvID             string `json:"env_id"`
	OwnerComponentID  string `json:"owner_component_id"`
	AssetID           string `json:"asset_id"`
	AssetKind         string `json:"asset_kind"`
	TargetComponentID string `json:"target_component_id"`
	TargetPath        string `json:"target_path"`
	ContentInline     string `json:"content_inline"`
	RemoteRefJSON     string `json:"remote_ref_json"`
	SHA256            string `json:"sha256"`
	SizeBytes         int64  `json:"size_bytes"`
	ApplyOrder        int    `json:"apply_order"`
	Sensitive         int    `json:"sensitive"`
	SummaryJSON       string `json:"summary_json"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

func (r componentConfigAssetRow) toStore() store.ComponentConfigAsset {
	return store.ComponentConfigAsset{
		EnvID:             r.EnvID,
		OwnerComponentID:  r.OwnerComponentID,
		AssetID:           r.AssetID,
		AssetKind:         r.AssetKind,
		TargetComponentID: r.TargetComponentID,
		TargetPath:        r.TargetPath,
		ContentInline:     r.ContentInline,
		RemoteRefJSON:     normalizeJSONText(r.RemoteRefJSON),
		SHA256:            r.SHA256,
		SizeBytes:         r.SizeBytes,
		ApplyOrder:        r.ApplyOrder,
		Sensitive:         r.Sensitive != 0,
		SummaryJSON:       normalizeJSONText(r.SummaryJSON),
		CreatedAt:         decodeTime(r.CreatedAt),
		UpdatedAt:         decodeTime(r.UpdatedAt),
	}
}

func (r environmentRow) toStore() store.Environment {
	return store.Environment{
		ID:                     r.ID,
		DisplayName:            r.DisplayName,
		Description:            r.Description,
		Status:                 r.Status,
		Verified:               r.Verified != 0,
		ServicesJSON:           r.ServicesJSON,
		ReposJSON:              r.ReposJSON,
		ComposeJSON:            r.ComposeJSON,
		HealthChecksJSON:       r.HealthChecksJSON,
		VerificationWorkflowID: r.VerificationWorkflowID,
		LastVerificationRunID:  r.LastVerificationRunID,
		LastVerificationStatus: r.LastVerificationStatus,
		EvidenceComplete:       r.EvidenceComplete != 0,
		TopologyComplete:       r.TopologyComplete != 0,
		LastVerifiedAt:         decodeTime(r.LastVerifiedAt),
		SummaryJSON:            r.SummaryJSON,
		CreatedAt:              decodeTime(r.CreatedAt),
		UpdatedAt:              decodeTime(r.UpdatedAt),
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

func normalizeJSONText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return value
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return value
	}
	return string(encoded)
}

func utcNow() time.Time {
	return time.Now().UTC()
}
