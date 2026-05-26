package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"agent-testbench/internal/store"
)

func (s *Store) UpsertEnvironment(ctx context.Context, e store.Environment) (store.Environment, error) {
	var err error
	e, err = store.PrepareEnvironmentForUpsert(e, utcNow())
	if err != nil {
		return store.Environment{}, err
	}
	query := fmt.Sprintf(`
insert into environments (
  id, display_name, description, status, verified, services_json, repos_json, compose_json,
  health_checks_json, verification_workflow_id, last_verification_run_id, last_verification_status,
  evidence_complete, topology_complete, last_verified_at, summary_json, created_at, updated_at
)
values (%s)
%s;`, s.bindVars(18), s.dialect.UpsertClause("id", []string{
		"display_name", "description", "status", "verified", "services_json", "repos_json", "compose_json",
		"health_checks_json", "verification_workflow_id", "last_verification_run_id", "last_verification_status",
		"evidence_complete", "topology_complete", "last_verified_at", "summary_json", "updated_at",
	}))
	if _, err := s.db.ExecContext(ctx, query,
		e.ID, e.DisplayName, e.Description, e.Status, e.Verified, stringDefault(e.ServicesJSON, "[]"),
		stringDefault(e.ReposJSON, "{}"), stringDefault(e.ComposeJSON, "{}"), stringDefault(e.HealthChecksJSON, "[]"),
		e.VerificationWorkflowID, e.LastVerificationRunID, e.LastVerificationStatus, e.EvidenceComplete, e.TopologyComplete,
		dbTimeArg(s.dialect, e.LastVerifiedAt), stringDefault(e.SummaryJSON, "{}"), dbTimeArg(s.dialect, e.CreatedAt), dbTimeArg(s.dialect, e.UpdatedAt),
	); err != nil {
		return store.Environment{}, fmt.Errorf("upsert environment %q: %w", e.ID, err)
	}
	return e, nil
}

func (s *Store) GetEnvironment(ctx context.Context, id string) (store.Environment, error) {
	query := fmt.Sprintf(`
select id, display_name, description, status, verified, services_json, repos_json, compose_json,
  health_checks_json, verification_workflow_id, last_verification_run_id, last_verification_status,
  evidence_complete, topology_complete, last_verified_at, summary_json, created_at, updated_at
from environments where id = %s;`, s.dialect.BindVar(1))
	return scanEnvironment(s.db.QueryRowContext(ctx, query, id))
}

func (s *Store) ListEnvironments(ctx context.Context) ([]store.Environment, error) {
	rows, err := s.db.QueryContext(ctx, `
select id, display_name, description, status, verified, services_json, repos_json, compose_json,
  health_checks_json, verification_workflow_id, last_verification_run_id, last_verification_status,
  evidence_complete, topology_complete, last_verified_at, summary_json, created_at, updated_at
from environments order by verified desc, updated_at desc, id;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Environment
	for rows.Next() {
		item, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ReplaceEnvironmentComponentGraph(ctx context.Context, envID string, graph store.EnvironmentComponentGraph) error {
	if err := store.ValidateEnvironmentComponentGraph(envID, graph); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"component_config_assets", "component_dependencies", "environment_components"} {
		query := fmt.Sprintf(`delete from %s where env_id = %s;`, table, s.dialect.BindVar(1))
		if _, err := tx.ExecContext(ctx, query, envID); err != nil {
			return fmt.Errorf("clear %s for environment %q: %w", table, envID, err)
		}
	}
	now := utcNow()
	for _, component := range graph.Components {
		if component.CreatedAt.IsZero() {
			component.CreatedAt = now
		}
		if component.UpdatedAt.IsZero() {
			component.UpdatedAt = now
		}
		query := fmt.Sprintf(`
insert into environment_components (
  env_id, component_id, display_name, kind, role, compose_service, image, required,
  runtime_json, healthcheck_json, summary_json, created_at, updated_at
) values (%s);`, s.bindVars(13))
		if _, err := tx.ExecContext(ctx, query,
			envID, component.ComponentID, component.DisplayName, component.Kind, component.Role, component.ComposeService,
			component.Image, component.Required, stringDefault(component.RuntimeJSON, "{}"), stringDefault(component.HealthCheckJSON, "{}"),
			stringDefault(component.SummaryJSON, "{}"), dbTimeArg(s.dialect, component.CreatedAt), dbTimeArg(s.dialect, component.UpdatedAt),
		); err != nil {
			return fmt.Errorf("insert environment component %q: %w", component.ComponentID, err)
		}
	}
	for _, dep := range graph.Dependencies {
		if dep.CreatedAt.IsZero() {
			dep.CreatedAt = now
		}
		if dep.UpdatedAt.IsZero() {
			dep.UpdatedAt = now
		}
		query := fmt.Sprintf(`
insert into component_dependencies (
  env_id, consumer_component_id, provider_component_id, phase, capability, required,
  profile_json, created_at, updated_at
) values (%s);`, s.bindVars(9))
		if _, err := tx.ExecContext(ctx, query,
			envID, dep.ConsumerComponentID, dep.ProviderComponentID, dep.Phase, dep.Capability, dep.Required,
			stringDefault(dep.ProfileJSON, "{}"), dbTimeArg(s.dialect, dep.CreatedAt), dbTimeArg(s.dialect, dep.UpdatedAt),
		); err != nil {
			return fmt.Errorf("insert component dependency %q -> %q: %w", dep.ConsumerComponentID, dep.ProviderComponentID, err)
		}
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
		query := fmt.Sprintf(`
insert into component_config_assets (
  env_id, owner_component_id, asset_id, asset_kind, target_component_id, target_path,
  content_inline, remote_ref_json, sha256, size_bytes, apply_order, %s,
  summary_json, created_at, updated_at
) values (%s);`, s.dialect.QuoteIdent("sensitive"), s.bindVars(15))
		if _, err := tx.ExecContext(ctx, query,
			envID, asset.OwnerComponentID, asset.AssetID, asset.AssetKind, asset.TargetComponentID, asset.TargetPath,
			asset.ContentInline, stringDefault(asset.RemoteRefJSON, "{}"), asset.SHA256, asset.SizeBytes, asset.ApplyOrder, asset.Sensitive,
			stringDefault(asset.SummaryJSON, "{}"), dbTimeArg(s.dialect, asset.CreatedAt), dbTimeArg(s.dialect, asset.UpdatedAt),
		); err != nil {
			return fmt.Errorf("insert component config asset %q: %w", asset.AssetID, err)
		}
	}
	return tx.Commit()
}

func (s *Store) GetEnvironmentComponentGraph(ctx context.Context, envID string) (store.EnvironmentComponentGraph, error) {
	graph := store.EnvironmentComponentGraph{
		Components:   []store.EnvironmentComponent{},
		Dependencies: []store.ComponentDependency{},
		Assets:       []store.ComponentConfigAsset{},
	}
	componentRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
select env_id, component_id, display_name, kind, role, compose_service, image, required,
  runtime_json, healthcheck_json, summary_json, created_at, updated_at
from environment_components
where env_id = %s
order by component_id;`, s.dialect.BindVar(1)), envID)
	if err != nil {
		return store.EnvironmentComponentGraph{}, err
	}
	defer componentRows.Close()
	for componentRows.Next() {
		item, err := scanEnvironmentComponent(componentRows)
		if err != nil {
			return store.EnvironmentComponentGraph{}, err
		}
		graph.Components = append(graph.Components, item)
	}
	if err := componentRows.Err(); err != nil {
		return store.EnvironmentComponentGraph{}, err
	}
	depRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
select env_id, consumer_component_id, provider_component_id, phase, capability, required,
  profile_json, created_at, updated_at
from component_dependencies
where env_id = %s
order by consumer_component_id, provider_component_id, phase, capability;`, s.dialect.BindVar(1)), envID)
	if err != nil {
		return store.EnvironmentComponentGraph{}, err
	}
	defer depRows.Close()
	for depRows.Next() {
		item, err := scanComponentDependency(depRows)
		if err != nil {
			return store.EnvironmentComponentGraph{}, err
		}
		graph.Dependencies = append(graph.Dependencies, item)
	}
	if err := depRows.Err(); err != nil {
		return store.EnvironmentComponentGraph{}, err
	}
	assetRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
select env_id, owner_component_id, asset_id, asset_kind, target_component_id, target_path,
  content_inline, remote_ref_json, sha256, size_bytes, apply_order, %s,
  summary_json, created_at, updated_at
from component_config_assets
where env_id = %s
order by owner_component_id, apply_order, asset_id;`, s.dialect.QuoteIdent("sensitive"), s.dialect.BindVar(1)), envID)
	if err != nil {
		return store.EnvironmentComponentGraph{}, err
	}
	defer assetRows.Close()
	for assetRows.Next() {
		item, err := scanComponentConfigAsset(assetRows)
		if err != nil {
			return store.EnvironmentComponentGraph{}, err
		}
		graph.Assets = append(graph.Assets, item)
	}
	return graph, assetRows.Err()
}

func scanEnvironment(row scanner) (store.Environment, error) {
	var e store.Environment
	var lastVerifiedAt, createdAt, updatedAt any
	if err := row.Scan(
		&e.ID, &e.DisplayName, &e.Description, &e.Status, &e.Verified, &e.ServicesJSON, &e.ReposJSON, &e.ComposeJSON,
		&e.HealthChecksJSON, &e.VerificationWorkflowID, &e.LastVerificationRunID, &e.LastVerificationStatus,
		&e.EvidenceComplete, &e.TopologyComplete, &lastVerifiedAt, &e.SummaryJSON, &createdAt, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.Environment{}, store.ErrNotFound
		}
		return store.Environment{}, err
	}
	e.ServicesJSON = normalizeJSONText(e.ServicesJSON)
	e.ReposJSON = normalizeJSONText(e.ReposJSON)
	e.ComposeJSON = normalizeJSONText(e.ComposeJSON)
	e.HealthChecksJSON = normalizeJSONText(e.HealthChecksJSON)
	e.SummaryJSON = normalizeJSONText(e.SummaryJSON)
	e.LastVerifiedAt = decodeDBTime(lastVerifiedAt)
	e.CreatedAt = decodeDBTime(createdAt)
	e.UpdatedAt = decodeDBTime(updatedAt)
	return e, nil
}

func scanEnvironmentComponent(row scanner) (store.EnvironmentComponent, error) {
	var item store.EnvironmentComponent
	var createdAt, updatedAt any
	if err := row.Scan(
		&item.EnvID, &item.ComponentID, &item.DisplayName, &item.Kind, &item.Role, &item.ComposeService,
		&item.Image, &item.Required, &item.RuntimeJSON, &item.HealthCheckJSON, &item.SummaryJSON,
		&createdAt, &updatedAt,
	); err != nil {
		return store.EnvironmentComponent{}, err
	}
	item.RuntimeJSON = normalizeJSONText(item.RuntimeJSON)
	item.HealthCheckJSON = normalizeJSONText(item.HealthCheckJSON)
	item.SummaryJSON = normalizeJSONText(item.SummaryJSON)
	item.CreatedAt = decodeDBTime(createdAt)
	item.UpdatedAt = decodeDBTime(updatedAt)
	return item, nil
}

func scanComponentDependency(row scanner) (store.ComponentDependency, error) {
	var item store.ComponentDependency
	var createdAt, updatedAt any
	if err := row.Scan(
		&item.EnvID, &item.ConsumerComponentID, &item.ProviderComponentID, &item.Phase, &item.Capability,
		&item.Required, &item.ProfileJSON, &createdAt, &updatedAt,
	); err != nil {
		return store.ComponentDependency{}, err
	}
	item.ProfileJSON = normalizeJSONText(item.ProfileJSON)
	item.CreatedAt = decodeDBTime(createdAt)
	item.UpdatedAt = decodeDBTime(updatedAt)
	return item, nil
}

func scanComponentConfigAsset(row scanner) (store.ComponentConfigAsset, error) {
	var item store.ComponentConfigAsset
	var createdAt, updatedAt any
	if err := row.Scan(
		&item.EnvID, &item.OwnerComponentID, &item.AssetID, &item.AssetKind, &item.TargetComponentID,
		&item.TargetPath, &item.ContentInline, &item.RemoteRefJSON, &item.SHA256, &item.SizeBytes,
		&item.ApplyOrder, &item.Sensitive, &item.SummaryJSON, &createdAt, &updatedAt,
	); err != nil {
		return store.ComponentConfigAsset{}, err
	}
	item.RemoteRefJSON = normalizeJSONText(item.RemoteRefJSON)
	item.SummaryJSON = normalizeJSONText(item.SummaryJSON)
	item.CreatedAt = decodeDBTime(createdAt)
	item.UpdatedAt = decodeDBTime(updatedAt)
	return item, nil
}
