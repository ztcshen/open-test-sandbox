package sqlstore

import (
	"context"
	"database/sql"
	"fmt"

	"agent-testbench/internal/store"
)

func (s *Store) UpsertBaselineGate(ctx context.Context, r store.BaselineGate) (store.BaselineGate, error) {
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = utcNow()
	}
	r.SummaryJSON = stringDefault(r.SummaryJSON, "{}")
	query := fmt.Sprintf(`
insert into baseline_gates (profile_id, subject_id, status, required, summary_json, checked_at, updated_at)
values (%s)
%s;`, s.bindVars(7), s.dialect.UpsertClause("profile_id, subject_id", []string{"status", "required", "summary_json", "checked_at", "updated_at"}))
	if _, err := s.db.ExecContext(ctx, query,
		r.ProfileID, r.SubjectID, r.Status, r.Required, r.SummaryJSON, dbTimeArg(s.dialect, r.CheckedAt), dbTimeArg(s.dialect, r.UpdatedAt)); err != nil {
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
	r.SummaryJSON = stringDefault(r.SummaryJSON, "{}")
	query := fmt.Sprintf(`
insert into profile_indexes (profile_id, bundle_path, bundle_digest, summary_json, imported_at, updated_at)
values (%s)
%s;`, s.bindVars(6), s.dialect.UpsertClause("profile_id", []string{"bundle_path", "bundle_digest", "summary_json", "imported_at", "updated_at"}))
	if _, err := s.db.ExecContext(ctx, query,
		r.ProfileID, r.BundlePath, r.BundleDigest, r.SummaryJSON, dbTimeArg(s.dialect, r.ImportedAt), dbTimeArg(s.dialect, r.UpdatedAt)); err != nil {
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
	r.SummaryJSON = stringDefault(r.SummaryJSON, "{}")
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
		r.ID, r.ProfileID, r.SourcePath, r.BundleDigest, r.SummaryJSON, r.Active, dbTimeArg(s.dialect, r.PublishedAt), dbTimeArg(s.dialect, r.CreatedAt)); err != nil {
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
	r.PayloadJSON = stringDefault(r.PayloadJSON, "{}")
	query := fmt.Sprintf(`
insert into config_read_model (profile_id, model_key, config_version_id, payload_json, generated_at, updated_at)
values (%s)
%s;`, s.bindVars(6), s.dialect.UpsertClause("profile_id, model_key", []string{"config_version_id", "payload_json", "generated_at", "updated_at"}))
	if _, err := s.db.ExecContext(ctx, query,
		r.ProfileID, r.Key, r.ConfigVersionID, r.PayloadJSON, dbTimeArg(s.dialect, r.GeneratedAt), dbTimeArg(s.dialect, r.UpdatedAt)); err != nil {
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

func scanBaselineGate(row scanner) (store.BaselineGate, error) {
	var r store.BaselineGate
	var checkedAt, updatedAt any
	if err := row.Scan(
		&r.ProfileID, &r.SubjectID, &r.Status, &r.Required, &r.SummaryJSON, &checkedAt, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.BaselineGate{}, store.ErrNotFound
		}
		return store.BaselineGate{}, err
	}
	r.SummaryJSON = normalizeJSONText(r.SummaryJSON)
	r.CheckedAt = decodeDBTime(checkedAt)
	r.UpdatedAt = decodeDBTime(updatedAt)
	return r, nil
}

func scanProfileIndex(row scanner) (store.ProfileIndex, error) {
	var r store.ProfileIndex
	var importedAt, updatedAt any
	if err := row.Scan(&r.ProfileID, &r.BundlePath, &r.BundleDigest, &r.SummaryJSON, &importedAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return store.ProfileIndex{}, store.ErrNotFound
		}
		return store.ProfileIndex{}, err
	}
	r.SummaryJSON = normalizeJSONText(r.SummaryJSON)
	r.ImportedAt = decodeDBTime(importedAt)
	r.UpdatedAt = decodeDBTime(updatedAt)
	return r, nil
}

func scanConfigVersion(row scanner) (store.ConfigVersion, error) {
	var r store.ConfigVersion
	var publishedAt, createdAt any
	if err := row.Scan(&r.ID, &r.ProfileID, &r.SourcePath, &r.BundleDigest, &r.SummaryJSON, &r.Active, &publishedAt, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return store.ConfigVersion{}, store.ErrNotFound
		}
		return store.ConfigVersion{}, err
	}
	r.SummaryJSON = normalizeJSONText(r.SummaryJSON)
	r.PublishedAt = decodeDBTime(publishedAt)
	r.CreatedAt = decodeDBTime(createdAt)
	return r, nil
}

func scanReadModel(row scanner) (store.ReadModel, error) {
	var r store.ReadModel
	var generatedAt, updatedAt any
	if err := row.Scan(&r.ProfileID, &r.Key, &r.ConfigVersionID, &r.PayloadJSON, &generatedAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return store.ReadModel{}, store.ErrNotFound
		}
		return store.ReadModel{}, err
	}
	r.PayloadJSON = normalizeJSONText(r.PayloadJSON)
	r.GeneratedAt = decodeDBTime(generatedAt)
	r.UpdatedAt = decodeDBTime(updatedAt)
	return r, nil
}
