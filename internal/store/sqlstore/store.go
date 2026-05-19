package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"open-test-sandbox/internal/store"
)

type Store struct {
	db      *sql.DB
	dialect Dialect
}

func New(db *sql.DB, dialect Dialect) *Store {
	return &Store{db: db, dialect: dialect}
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) CreateRun(ctx context.Context, r store.Run) (store.Run, error) {
	now := utcNow()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = r.CreatedAt
	}
	query := fmt.Sprintf(`
insert into runs (id, profile_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at)
values (%s);`, s.bindVars(10))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.ProfileID, r.WorkflowID, r.Status, r.EvidenceRoot, r.SummaryJSON,
		encodeTime(r.StartedAt), encodeTime(r.FinishedAt), encodeTime(r.CreatedAt), encodeTime(r.UpdatedAt)); err != nil {
		return store.Run{}, fmt.Errorf("create run %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) GetRun(ctx context.Context, id string) (store.Run, error) {
	query := fmt.Sprintf(`
select id, profile_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at
from runs where id = %s;`, s.dialect.BindVar(1))
	r, err := scanRun(s.db.QueryRowContext(ctx, query, id))
	if err != nil {
		return store.Run{}, err
	}
	return r, nil
}

func (s *Store) ListRuns(ctx context.Context) ([]store.Run, error) {
	rows, err := s.db.QueryContext(ctx, `
select id, profile_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at
from runs order by created_at, id;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) RecordAPICaseRun(ctx context.Context, r store.APICaseRun) (store.APICaseRun, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	query := fmt.Sprintf(`
insert into api_case_runs (id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at)
values (%s);`, s.bindVars(9))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.RunID, r.CaseID, r.Status, r.RequestSummaryJSON, r.AssertionSummaryJSON,
		encodeTime(r.StartedAt), encodeTime(r.FinishedAt), encodeTime(r.CreatedAt)); err != nil {
		return store.APICaseRun{}, fmt.Errorf("record api case run %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) ListAPICaseRuns(ctx context.Context, runID string) ([]store.APICaseRun, error) {
	query := fmt.Sprintf(`
select id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at
from api_case_runs where run_id = %s order by created_at, id;`, s.dialect.BindVar(1))
	rows, err := s.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.APICaseRun
	for rows.Next() {
		r, err := scanAPICaseRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) RecordEvidence(ctx context.Context, r store.EvidenceRecord) (store.EvidenceRecord, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	query := fmt.Sprintf(`
insert into evidence_records (id, run_id, case_run_id, step_id, kind, uri, media_type, sha256, size_bytes, summary, category, visibility, labels_json, created_at)
values (%s);`, s.bindVars(14))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.RunID, r.CaseRunID, r.StepID, r.Kind, r.URI, r.MediaType, r.SHA256, r.SizeBytes, r.Summary,
		r.Category, r.Visibility, r.LabelsJSON, encodeTime(r.CreatedAt)); err != nil {
		return store.EvidenceRecord{}, fmt.Errorf("record evidence %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) ListEvidence(ctx context.Context, runID string) ([]store.EvidenceRecord, error) {
	query := fmt.Sprintf(`
select id, run_id, case_run_id, step_id, kind, uri, media_type, sha256, size_bytes, summary, category, visibility, labels_json, created_at
from evidence_records where run_id = %s order by created_at, id;`, s.dialect.BindVar(1))
	rows, err := s.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.EvidenceRecord
	for rows.Next() {
		r, err := scanEvidenceRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) SaveTraceTopology(ctx context.Context, r store.TraceTopology) (store.TraceTopology, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	query := fmt.Sprintf(`
insert into trace_topologies (id, workflow_run_id, workflow_id, step_id, case_id, request_id, trace_id, status, topology_json, text_topology, created_at)
values (%s);`, s.bindVars(11))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.WorkflowRunID, r.WorkflowID, r.StepID, r.CaseID, r.RequestID, r.TraceID, r.Status,
		r.TopologyJSON, r.TextTopology, encodeTime(r.CreatedAt)); err != nil {
		return store.TraceTopology{}, fmt.Errorf("save trace topology %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) ListTraceTopologies(ctx context.Context, workflowRunID string) ([]store.TraceTopology, error) {
	query := fmt.Sprintf(`
select id, workflow_run_id, workflow_id, step_id, case_id, request_id, trace_id, status, topology_json, text_topology, created_at
from trace_topologies where workflow_run_id = %s order by created_at, id;`, s.dialect.BindVar(1))
	rows, err := s.db.QueryContext(ctx, query, workflowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.TraceTopology
	for rows.Next() {
		r, err := scanTraceTopology(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) RecordPostProcessTask(ctx context.Context, r store.PostProcessTask) (store.PostProcessTask, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	query := fmt.Sprintf(`
insert into post_process_tasks (id, run_id, workflow_id, step_id, case_id, kind, status, started_at, finished_at, duration_ms, error, summary_json, created_at)
values (%s);`, s.bindVars(13))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.RunID, r.WorkflowID, r.StepID, r.CaseID, r.Kind, r.Status, encodeTime(r.StartedAt),
		encodeTime(r.FinishedAt), r.DurationMs, r.Error, r.SummaryJSON, encodeTime(r.CreatedAt)); err != nil {
		return store.PostProcessTask{}, fmt.Errorf("record post-process task %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) ListPostProcessTasks(ctx context.Context, runID string) ([]store.PostProcessTask, error) {
	query := fmt.Sprintf(`
select id, run_id, workflow_id, step_id, case_id, kind, status, started_at, finished_at, duration_ms, error, summary_json, created_at
from post_process_tasks where run_id = %s order by created_at, id;`, s.dialect.BindVar(1))
	rows, err := s.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.PostProcessTask
	for rows.Next() {
		r, err := scanPostProcessTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) UpsertBaselineGate(ctx context.Context, r store.BaselineGate) (store.BaselineGate, error) {
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = utcNow()
	}
	query := fmt.Sprintf(`
insert into baseline_gates (profile_id, subject_id, status, required, summary_json, checked_at, updated_at)
values (%s)
%s;`, s.bindVars(7), s.dialect.UpsertClause("profile_id, subject_id", []string{"status", "required", "summary_json", "checked_at", "updated_at"}))
	if _, err := s.db.ExecContext(ctx, query,
		r.ProfileID, r.SubjectID, r.Status, r.Required, r.SummaryJSON, encodeTime(r.CheckedAt), encodeTime(r.UpdatedAt)); err != nil {
		return store.BaselineGate{}, fmt.Errorf("upsert baseline gate %q/%q: %w", r.ProfileID, r.SubjectID, err)
	}
	return r, nil
}

func (s *Store) GetBaselineGate(ctx context.Context, profileID string, subjectID string) (store.BaselineGate, error) {
	query := fmt.Sprintf(`
select profile_id, subject_id, status, required, summary_json, checked_at, updated_at
from baseline_gates where profile_id = %s and subject_id = %s;`, s.dialect.BindVar(1), s.dialect.BindVar(2))
	r, err := scanBaselineGate(s.db.QueryRowContext(ctx, query, profileID, subjectID))
	if err != nil {
		return store.BaselineGate{}, err
	}
	return r, nil
}

func (s *Store) UpsertProfileIndex(ctx context.Context, r store.ProfileIndex) (store.ProfileIndex, error) {
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = utcNow()
	}
	query := fmt.Sprintf(`
insert into profile_indexes (profile_id, bundle_path, bundle_digest, summary_json, imported_at, updated_at)
values (%s)
%s;`, s.bindVars(6), s.dialect.UpsertClause("profile_id", []string{"bundle_path", "bundle_digest", "summary_json", "imported_at", "updated_at"}))
	if _, err := s.db.ExecContext(ctx, query,
		r.ProfileID, r.BundlePath, r.BundleDigest, r.SummaryJSON, encodeTime(r.ImportedAt), encodeTime(r.UpdatedAt)); err != nil {
		return store.ProfileIndex{}, fmt.Errorf("upsert profile index %q: %w", r.ProfileID, err)
	}
	return r, nil
}

func (s *Store) GetProfileIndex(ctx context.Context, profileID string) (store.ProfileIndex, error) {
	query := fmt.Sprintf(`
select profile_id, bundle_path, bundle_digest, summary_json, imported_at, updated_at
from profile_indexes where profile_id = %s;`, s.dialect.BindVar(1))
	r, err := scanProfileIndex(s.db.QueryRowContext(ctx, query, profileID))
	if err != nil {
		return store.ProfileIndex{}, err
	}
	return r, nil
}

func (s *Store) UpsertConfigVersion(ctx context.Context, r store.ConfigVersion) (store.ConfigVersion, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	if r.PublishedAt.IsZero() {
		r.PublishedAt = r.CreatedAt
	}
	if r.Active {
		query := fmt.Sprintf(`update config_versions set active = %s;`, s.dialect.BindVar(1))
		if _, err := s.db.ExecContext(ctx, query, false); err != nil {
			return store.ConfigVersion{}, fmt.Errorf("reset active config versions: %w", err)
		}
	}
	query := fmt.Sprintf(`
insert into config_versions (id, profile_id, source_path, bundle_digest, summary_json, active, published_at, created_at)
values (%s)
%s;`, s.bindVars(8), s.dialect.UpsertClause("id", []string{"profile_id", "source_path", "bundle_digest", "summary_json", "active", "published_at"}))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.ProfileID, r.SourcePath, r.BundleDigest, r.SummaryJSON, r.Active, encodeTime(r.PublishedAt), encodeTime(r.CreatedAt)); err != nil {
		return store.ConfigVersion{}, fmt.Errorf("upsert config version %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) GetActiveConfigVersion(ctx context.Context) (store.ConfigVersion, error) {
	query := fmt.Sprintf(`
select id, profile_id, source_path, bundle_digest, summary_json, active, published_at, created_at
from config_versions
where active = %s
order by published_at desc, id desc
limit 1;`, s.dialect.BindVar(1))
	r, err := scanConfigVersion(s.db.QueryRowContext(ctx, query, true))
	if err != nil {
		return store.ConfigVersion{}, err
	}
	return r, nil
}

func (s *Store) UpsertReadModel(ctx context.Context, r store.ReadModel) (store.ReadModel, error) {
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = utcNow()
	}
	if r.GeneratedAt.IsZero() {
		r.GeneratedAt = r.UpdatedAt
	}
	query := fmt.Sprintf(`
insert into config_read_model (profile_id, model_key, config_version_id, payload_json, generated_at, updated_at)
values (%s)
%s;`, s.bindVars(6), s.dialect.UpsertClause("profile_id, model_key", []string{"config_version_id", "payload_json", "generated_at", "updated_at"}))
	if _, err := s.db.ExecContext(ctx, query,
		r.ProfileID, r.Key, r.ConfigVersionID, r.PayloadJSON, encodeTime(r.GeneratedAt), encodeTime(r.UpdatedAt)); err != nil {
		return store.ReadModel{}, fmt.Errorf("upsert read model %q/%q: %w", r.ProfileID, r.Key, err)
	}
	return r, nil
}

func (s *Store) GetReadModel(ctx context.Context, profileID string, key string) (store.ReadModel, error) {
	query := fmt.Sprintf(`
select profile_id, model_key, config_version_id, payload_json, generated_at, updated_at
from config_read_model
where profile_id = %s and model_key = %s;`, s.dialect.BindVar(1), s.dialect.BindVar(2))
	r, err := scanReadModel(s.db.QueryRowContext(ctx, query, profileID, key))
	if err != nil {
		return store.ReadModel{}, err
	}
	return r, nil
}

func (s *Store) ReplaceProfileCatalog(ctx context.Context, catalog store.ProfileCatalog) error {
	if catalog.IndexedAt.IsZero() {
		catalog.IndexedAt = utcNow()
	}
	counts := catalogCounts(catalog)
	payload, err := json.Marshal(catalog)
	if err != nil {
		return fmt.Errorf("encode profile catalog %q: %w", catalog.ProfileID, err)
	}
	query := fmt.Sprintf(`
insert into profile_catalogs (
  profile_id, indexed_at, catalog_json, services, workflows, interface_nodes, api_cases,
  request_templates, workflow_bindings, case_dependencies, fixtures, templates, template_configs
)
values (%s)
%s;`, s.bindVars(13), s.dialect.UpsertClause("profile_id", []string{
		"indexed_at", "catalog_json", "services", "workflows", "interface_nodes", "api_cases",
		"request_templates", "workflow_bindings", "case_dependencies", "fixtures", "templates", "template_configs",
	}))
	if _, err := s.db.ExecContext(ctx, query,
		catalog.ProfileID, encodeTime(catalog.IndexedAt), string(payload), counts.Services, counts.Workflows, counts.InterfaceNodes,
		counts.APICases, counts.RequestTemplates, counts.WorkflowBindings, counts.CaseDependencies, counts.Fixtures, counts.Templates, counts.TemplateConfigs,
	); err != nil {
		return fmt.Errorf("replace profile catalog %q: %w", catalog.ProfileID, err)
	}
	return nil
}

func (s *Store) GetProfileCatalog(ctx context.Context) (store.ProfileCatalog, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `
select catalog_json
from profile_catalogs
order by indexed_at desc, profile_id desc
limit 1;`).Scan(&payload)
	if err != nil {
		if err == sql.ErrNoRows {
			return store.ProfileCatalog{}, store.ErrNotFound
		}
		return store.ProfileCatalog{}, err
	}
	var catalog store.ProfileCatalog
	if err := json.Unmarshal([]byte(payload), &catalog); err != nil {
		return store.ProfileCatalog{}, fmt.Errorf("decode profile catalog: %w", err)
	}
	return catalog, nil
}

func (s *Store) GetProfileCatalogIndex(ctx context.Context) (store.ProfileCatalogIndex, error) {
	row := s.db.QueryRowContext(ctx, `
select profile_id, indexed_at, services, workflows, interface_nodes, api_cases, request_templates,
  workflow_bindings, case_dependencies, fixtures, templates, template_configs
from profile_catalogs
order by indexed_at desc, profile_id desc
limit 1;`)
	index, err := scanProfileCatalogIndex(row)
	if err != nil {
		return store.ProfileCatalogIndex{}, err
	}
	return index, nil
}

func (s *Store) bindVars(count int) string {
	vars := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		vars = append(vars, s.dialect.BindVar(i))
	}
	return strings.Join(vars, ", ")
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRun(row scanner) (store.Run, error) {
	var r store.Run
	var startedAt, finishedAt, createdAt, updatedAt string
	if err := row.Scan(
		&r.ID, &r.ProfileID, &r.WorkflowID, &r.Status, &r.EvidenceRoot, &r.SummaryJSON,
		&startedAt, &finishedAt, &createdAt, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.Run{}, store.ErrNotFound
		}
		return store.Run{}, err
	}
	r.StartedAt = decodeTime(startedAt)
	r.FinishedAt = decodeTime(finishedAt)
	r.CreatedAt = decodeTime(createdAt)
	r.UpdatedAt = decodeTime(updatedAt)
	return r, nil
}

func scanAPICaseRun(row scanner) (store.APICaseRun, error) {
	var r store.APICaseRun
	var startedAt, finishedAt, createdAt string
	if err := row.Scan(
		&r.ID, &r.RunID, &r.CaseID, &r.Status, &r.RequestSummaryJSON, &r.AssertionSummaryJSON,
		&startedAt, &finishedAt, &createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.APICaseRun{}, store.ErrNotFound
		}
		return store.APICaseRun{}, err
	}
	r.StartedAt = decodeTime(startedAt)
	r.FinishedAt = decodeTime(finishedAt)
	r.CreatedAt = decodeTime(createdAt)
	return r, nil
}

func scanEvidenceRecord(row scanner) (store.EvidenceRecord, error) {
	var r store.EvidenceRecord
	var createdAt string
	if err := row.Scan(
		&r.ID, &r.RunID, &r.CaseRunID, &r.StepID, &r.Kind, &r.URI, &r.MediaType, &r.SHA256, &r.SizeBytes,
		&r.Summary, &r.Category, &r.Visibility, &r.LabelsJSON, &createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.EvidenceRecord{}, store.ErrNotFound
		}
		return store.EvidenceRecord{}, err
	}
	r.CreatedAt = decodeTime(createdAt)
	return r, nil
}

func scanTraceTopology(row scanner) (store.TraceTopology, error) {
	var r store.TraceTopology
	var createdAt string
	if err := row.Scan(
		&r.ID, &r.WorkflowRunID, &r.WorkflowID, &r.StepID, &r.CaseID, &r.RequestID, &r.TraceID,
		&r.Status, &r.TopologyJSON, &r.TextTopology, &createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.TraceTopology{}, store.ErrNotFound
		}
		return store.TraceTopology{}, err
	}
	r.CreatedAt = decodeTime(createdAt)
	return r, nil
}

func scanPostProcessTask(row scanner) (store.PostProcessTask, error) {
	var r store.PostProcessTask
	var startedAt, finishedAt, createdAt string
	if err := row.Scan(
		&r.ID, &r.RunID, &r.WorkflowID, &r.StepID, &r.CaseID, &r.Kind, &r.Status,
		&startedAt, &finishedAt, &r.DurationMs, &r.Error, &r.SummaryJSON, &createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.PostProcessTask{}, store.ErrNotFound
		}
		return store.PostProcessTask{}, err
	}
	r.StartedAt = decodeTime(startedAt)
	r.FinishedAt = decodeTime(finishedAt)
	r.CreatedAt = decodeTime(createdAt)
	return r, nil
}

func scanBaselineGate(row scanner) (store.BaselineGate, error) {
	var r store.BaselineGate
	var checkedAt, updatedAt string
	if err := row.Scan(
		&r.ProfileID, &r.SubjectID, &r.Status, &r.Required, &r.SummaryJSON, &checkedAt, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.BaselineGate{}, store.ErrNotFound
		}
		return store.BaselineGate{}, err
	}
	r.CheckedAt = decodeTime(checkedAt)
	r.UpdatedAt = decodeTime(updatedAt)
	return r, nil
}

func scanProfileIndex(row scanner) (store.ProfileIndex, error) {
	var r store.ProfileIndex
	var importedAt, updatedAt string
	if err := row.Scan(&r.ProfileID, &r.BundlePath, &r.BundleDigest, &r.SummaryJSON, &importedAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return store.ProfileIndex{}, store.ErrNotFound
		}
		return store.ProfileIndex{}, err
	}
	r.ImportedAt = decodeTime(importedAt)
	r.UpdatedAt = decodeTime(updatedAt)
	return r, nil
}

func scanConfigVersion(row scanner) (store.ConfigVersion, error) {
	var r store.ConfigVersion
	var publishedAt, createdAt string
	if err := row.Scan(&r.ID, &r.ProfileID, &r.SourcePath, &r.BundleDigest, &r.SummaryJSON, &r.Active, &publishedAt, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return store.ConfigVersion{}, store.ErrNotFound
		}
		return store.ConfigVersion{}, err
	}
	r.PublishedAt = decodeTime(publishedAt)
	r.CreatedAt = decodeTime(createdAt)
	return r, nil
}

func scanReadModel(row scanner) (store.ReadModel, error) {
	var r store.ReadModel
	var generatedAt, updatedAt string
	if err := row.Scan(&r.ProfileID, &r.Key, &r.ConfigVersionID, &r.PayloadJSON, &generatedAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return store.ReadModel{}, store.ErrNotFound
		}
		return store.ReadModel{}, err
	}
	r.GeneratedAt = decodeTime(generatedAt)
	r.UpdatedAt = decodeTime(updatedAt)
	return r, nil
}

func scanProfileCatalogIndex(row scanner) (store.ProfileCatalogIndex, error) {
	var r store.ProfileCatalogIndex
	var indexedAt string
	if err := row.Scan(
		&r.ProfileID, &indexedAt, &r.Counts.Services, &r.Counts.Workflows, &r.Counts.InterfaceNodes,
		&r.Counts.APICases, &r.Counts.RequestTemplates, &r.Counts.WorkflowBindings, &r.Counts.CaseDependencies,
		&r.Counts.Fixtures, &r.Counts.Templates, &r.Counts.TemplateConfigs,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.ProfileCatalogIndex{}, store.ErrNotFound
		}
		return store.ProfileCatalogIndex{}, err
	}
	r.IndexedAt = decodeTime(indexedAt)
	return r, nil
}

func catalogCounts(catalog store.ProfileCatalog) store.ProfileCatalogCounts {
	return store.ProfileCatalogCounts{
		Services:         len(catalog.Services),
		Workflows:        len(catalog.Workflows),
		InterfaceNodes:   len(catalog.InterfaceNodes),
		APICases:         len(catalog.APICases),
		RequestTemplates: len(catalog.RequestTemplates),
		WorkflowBindings: len(catalog.WorkflowBindings),
		CaseDependencies: len(catalog.CaseDependencies),
		Fixtures:         len(catalog.Fixtures),
		Templates:        len(catalog.Workflows) + len(catalog.RequestTemplates) + len(catalog.TemplateConfigs),
		TemplateConfigs:  len(catalog.Workflows) + len(catalog.RequestTemplates) + len(catalog.TemplateConfigs),
	}
}

func utcNow() time.Time {
	return time.Now().UTC()
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
