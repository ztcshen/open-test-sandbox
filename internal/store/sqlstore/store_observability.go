package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"agent-testbench/internal/store"
)

func (s *Store) RecordEvidence(ctx context.Context, r store.EvidenceRecord) (store.EvidenceRecord, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	if strings.TrimSpace(r.LabelsJSON) == "" {
		r.LabelsJSON = "{}"
	}
	query := fmt.Sprintf(`
insert into evidence_records (id, run_id, case_run_id, step_id, kind, uri, media_type, sha256, size_bytes, summary, category, visibility, labels_json, created_at)
values (%s);`, s.bindVars(14))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.RunID, r.CaseRunID, r.StepID, r.Kind, r.URI, r.MediaType, r.SHA256, r.SizeBytes, r.Summary,
		r.Category, r.Visibility, r.LabelsJSON, dbTimeArg(s.dialect, r.CreatedAt)); err != nil {
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
	r.TopologyJSON = stringDefault(r.TopologyJSON, "{}")
	query := fmt.Sprintf(`
insert into trace_topologies (id, workflow_run_id, workflow_id, step_id, case_id, request_id, trace_id, status, topology_json, text_topology, created_at)
values (%s)
%s;`, s.bindVars(11), s.dialect.UpsertClause("id", []string{
		"workflow_run_id", "workflow_id", "step_id", "case_id", "request_id", "trace_id", "status", "topology_json", "text_topology", "created_at",
	}))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.WorkflowRunID, r.WorkflowID, r.StepID, r.CaseID, r.RequestID, r.TraceID, r.Status,
		r.TopologyJSON, r.TextTopology, dbTimeArg(s.dialect, r.CreatedAt)); err != nil {
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
	if r.StartedAt.IsZero() {
		r.StartedAt = r.CreatedAt
	}
	if r.FinishedAt.IsZero() && r.Status != store.StatusRunning {
		r.FinishedAt = r.StartedAt
	}
	if r.DurationMs == 0 && !r.StartedAt.IsZero() && !r.FinishedAt.IsZero() {
		r.DurationMs = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
	}
	r.SummaryJSON = stringDefault(r.SummaryJSON, "{}")
	query := fmt.Sprintf(`
insert into post_process_tasks (id, run_id, workflow_id, step_id, case_id, kind, status, started_at, finished_at, duration_ms, error, summary_json, created_at)
values (%s)
%s;`, s.bindVars(13), s.dialect.UpsertClause("id", []string{
		"run_id", "workflow_id", "step_id", "case_id", "kind", "status", "started_at", "finished_at", "duration_ms", "error", "summary_json", "created_at",
	}))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.RunID, r.WorkflowID, r.StepID, r.CaseID, r.Kind, r.Status, dbTimeArg(s.dialect, r.StartedAt),
		dbTimeArg(s.dialect, r.FinishedAt), r.DurationMs, r.Error, r.SummaryJSON, dbTimeArg(s.dialect, r.CreatedAt)); err != nil {
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

func scanEvidenceRecord(row scanner) (store.EvidenceRecord, error) {
	var r store.EvidenceRecord
	var createdAt any
	if err := row.Scan(
		&r.ID, &r.RunID, &r.CaseRunID, &r.StepID, &r.Kind, &r.URI, &r.MediaType, &r.SHA256, &r.SizeBytes,
		&r.Summary, &r.Category, &r.Visibility, &r.LabelsJSON, &createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.EvidenceRecord{}, store.ErrNotFound
		}
		return store.EvidenceRecord{}, err
	}
	r.LabelsJSON = normalizeJSONText(r.LabelsJSON)
	r.CreatedAt = decodeDBTime(createdAt)
	return r, nil
}

func scanTraceTopology(row scanner) (store.TraceTopology, error) {
	var r store.TraceTopology
	var createdAt any
	if err := row.Scan(
		&r.ID, &r.WorkflowRunID, &r.WorkflowID, &r.StepID, &r.CaseID, &r.RequestID, &r.TraceID,
		&r.Status, &r.TopologyJSON, &r.TextTopology, &createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.TraceTopology{}, store.ErrNotFound
		}
		return store.TraceTopology{}, err
	}
	r.TopologyJSON = normalizeJSONText(r.TopologyJSON)
	r.CreatedAt = decodeDBTime(createdAt)
	return r, nil
}

func scanPostProcessTask(row scanner) (store.PostProcessTask, error) {
	var r store.PostProcessTask
	var startedAt, finishedAt, createdAt any
	if err := row.Scan(
		&r.ID, &r.RunID, &r.WorkflowID, &r.StepID, &r.CaseID, &r.Kind, &r.Status,
		&startedAt, &finishedAt, &r.DurationMs, &r.Error, &r.SummaryJSON, &createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.PostProcessTask{}, store.ErrNotFound
		}
		return store.PostProcessTask{}, err
	}
	r.SummaryJSON = normalizeJSONText(r.SummaryJSON)
	r.StartedAt = decodeDBTime(startedAt)
	r.FinishedAt = decodeDBTime(finishedAt)
	r.CreatedAt = decodeDBTime(createdAt)
	return r, nil
}
