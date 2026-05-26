package sqlstore

import (
	"context"
	"database/sql"
	"fmt"

	"agent-testbench/internal/store"
)

func (s *Store) CreateRun(ctx context.Context, r store.Run) (store.Run, error) {
	now := utcNow()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = r.CreatedAt
	}
	r.SummaryJSON = stringDefault(r.SummaryJSON, "{}")
	query := fmt.Sprintf(`
insert into runs (id, profile_id, environment_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at)
values (%s);`, s.bindVars(11))
	if _, err := s.db.ExecContext(ctx, query,
		r.ID, r.ProfileID, r.EnvironmentID, r.WorkflowID, r.Status, r.EvidenceRoot, r.SummaryJSON,
		dbTimeArg(s.dialect, r.StartedAt), dbTimeArg(s.dialect, r.FinishedAt), dbTimeArg(s.dialect, r.CreatedAt), dbTimeArg(s.dialect, r.UpdatedAt)); err != nil {
		return store.Run{}, fmt.Errorf("create run %q: %w", r.ID, err)
	}
	return r, nil
}

func (s *Store) GetRun(ctx context.Context, id string) (store.Run, error) {
	query := fmt.Sprintf(`
	select id, profile_id, environment_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at
from runs where id = %s;`, s.dialect.BindVar(1))
	r, err := scanRun(s.db.QueryRowContext(ctx, query, id))
	if err != nil {
		return store.Run{}, err
	}
	return r, nil
}

func (s *Store) ListRuns(ctx context.Context) ([]store.Run, error) {
	rows, err := s.db.QueryContext(ctx, `
select id, profile_id, environment_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at
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

func scanRun(row scanner) (store.Run, error) {
	var r store.Run
	var startedAt, finishedAt, createdAt, updatedAt any
	if err := row.Scan(
		&r.ID, &r.ProfileID, &r.EnvironmentID, &r.WorkflowID, &r.Status, &r.EvidenceRoot, &r.SummaryJSON,
		&startedAt, &finishedAt, &createdAt, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.Run{}, store.ErrNotFound
		}
		return store.Run{}, err
	}
	r.SummaryJSON = normalizeJSONText(r.SummaryJSON)
	r.StartedAt = decodeDBTime(startedAt)
	r.FinishedAt = decodeDBTime(finishedAt)
	r.CreatedAt = decodeDBTime(createdAt)
	r.UpdatedAt = decodeDBTime(updatedAt)
	return r, nil
}
