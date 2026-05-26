package sqlstore

import (
	"context"
	"database/sql"
	"fmt"

	"agent-testbench/internal/store"
)

func (s *Store) RecordAPICaseRun(ctx context.Context, r store.APICaseRun) (store.APICaseRun, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = utcNow()
	}
	r.RequestSummaryJSON = stringDefault(r.RequestSummaryJSON, "{}")
	r.AssertionSummaryJSON = stringDefault(r.AssertionSummaryJSON, "{}")
	query := fmt.Sprintf(`
insert into api_case_runs (id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at)
values (%s);`, s.bindVars(9))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.RunID, r.CaseID, r.Status, r.RequestSummaryJSON, r.AssertionSummaryJSON,
		dbTimeArg(s.dialect, r.StartedAt), dbTimeArg(s.dialect, r.FinishedAt), dbTimeArg(s.dialect, r.CreatedAt)); err != nil {
		return store.APICaseRun{}, fmt.Errorf("record api case run %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) ListAPICaseRuns(ctx context.Context, runID string) ([]store.APICaseRun, error) {
	query := fmt.Sprintf(`
select id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at
from api_case_runs where run_id = %s order by created_at, id;`, s.dialect.BindVar(1))
	return s.queryAPICaseRuns(ctx, query, runID)
}

func (s *Store) ListLatestAPICaseRuns(ctx context.Context) ([]store.APICaseRun, error) {
	return s.queryAPICaseRuns(ctx, `
select id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at
from (
  select id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at,
    row_number() over (partition by case_id order by created_at desc, id desc) as rn
  from api_case_runs
  where case_id <> ''
) latest
where rn = 1
order by created_at, id;`)
}

func (s *Store) queryAPICaseRuns(ctx context.Context, query string, args ...any) ([]store.APICaseRun, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
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

func scanAPICaseRun(row scanner) (store.APICaseRun, error) {
	var r store.APICaseRun
	var startedAt, finishedAt, createdAt any
	if err := row.Scan(
		&r.ID, &r.RunID, &r.CaseID, &r.Status, &r.RequestSummaryJSON, &r.AssertionSummaryJSON,
		&startedAt, &finishedAt, &createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.APICaseRun{}, store.ErrNotFound
		}
		return store.APICaseRun{}, err
	}
	r.RequestSummaryJSON = normalizeJSONText(r.RequestSummaryJSON)
	r.AssertionSummaryJSON = normalizeJSONText(r.AssertionSummaryJSON)
	r.StartedAt = decodeDBTime(startedAt)
	r.FinishedAt = decodeDBTime(finishedAt)
	r.CreatedAt = decodeDBTime(createdAt)
	return r, nil
}
