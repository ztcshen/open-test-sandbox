package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	CurrentSchemaVersion = 5
	CoreSchemaName       = "create shared sql store schema"
)

type SchemaStatusResult struct {
	CurrentVersion int
	TargetVersion  int
	AppliedCount   int
}

func (r SchemaStatusResult) HasPending() bool {
	return r.CurrentVersion < r.TargetVersion
}

func SchemaStatus(ctx context.Context, db *sql.DB, d Dialect) (SchemaStatusResult, error) {
	current, err := currentSchemaVersion(ctx, db, d)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	return SchemaStatusResult{CurrentVersion: current, TargetVersion: CurrentSchemaVersion}, nil
}

func UpgradeSchema(ctx context.Context, db *sql.DB, d Dialect) (SchemaStatusResult, error) {
	current, err := currentSchemaVersion(ctx, db, d)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	applied := 0
	if current >= CurrentSchemaVersion {
		return SchemaStatusResult{CurrentVersion: current, TargetVersion: CurrentSchemaVersion}, nil
	}
	for _, statement := range CoreSchemaSQL(d) {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			if isIdempotentSchemaReplayError(d, statement, err) {
				continue
			}
			return SchemaStatusResult{}, fmt.Errorf("apply shared sql store schema: %w", err)
		}
	}
	for _, statement := range incrementalSchemaSQL(d, current) {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return SchemaStatusResult{}, fmt.Errorf("apply shared sql store migration: %w", err)
		}
	}
	query := fmt.Sprintf(`
insert into schema_versions (version, name, applied_at)
values (%s)
%s;`, bindVars(d, 3), d.UpsertClause("version", []string{"name", "applied_at"}))
	if _, err := db.ExecContext(ctx, query, CurrentSchemaVersion, CoreSchemaName, time.Now().UTC()); err != nil {
		return SchemaStatusResult{}, fmt.Errorf("record shared sql store schema version: %w", err)
	}
	applied = 1
	status, err := SchemaStatus(ctx, db, d)
	if err != nil {
		return SchemaStatusResult{}, err
	}
	status.AppliedCount = applied
	return status, nil
}

func currentSchemaVersion(ctx context.Context, db *sql.DB, d Dialect) (int, error) {
	var exists int
	if err := db.QueryRowContext(ctx, d.TableExistsSQL("schema_versions")).Scan(&exists); err != nil {
		return 0, fmt.Errorf("check schema_versions table: %w", err)
	}
	if exists == 0 {
		return 0, nil
	}
	var version int
	if err := db.QueryRowContext(ctx, `select coalesce(max(version), 0) as version from schema_versions;`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read shared sql store schema version: %w", err)
	}
	return version, nil
}

func CoreSchemaSQL(d Dialect) []string {
	text := d.TextType()
	keyText := d.KeyTextType()
	runIDText := runIdentifierTextType(d)
	intType := "integer"
	timeType := d.TimeType()
	jsonType := d.JSONType()
	boolType := d.BoolType()
	return []string{
		fmt.Sprintf(`
create table if not exists schema_versions (
  version integer primary key,
  name %s not null,
  applied_at %s not null
);`, text, timeType),
		fmt.Sprintf(`
create table if not exists runs (
  id %s primary key,
  profile_id %s not null,
  workflow_id %s not null,
  status %s not null,
  evidence_root %s not null,
  summary_json %s not null,
  started_at %s,
  finished_at %s,
  created_at %s not null,
  updated_at %s not null
);`, runIDText, keyText, keyText, keyText, text, jsonType, timeType, timeType, timeType, timeType),
		fmt.Sprintf(`
create table if not exists api_case_runs (
  id %s primary key,
  run_id %s not null,
  case_id %s not null,
  status %s not null,
  request_summary_json %s not null,
  assertion_summary_json %s not null,
  started_at %s,
  finished_at %s,
  created_at %s not null
);`, runIDText, runIDText, keyText, keyText, jsonType, jsonType, timeType, timeType, timeType),
		d.CreateIndexSQL("idx_api_case_runs_run_id_created_at", "api_case_runs", []string{"run_id", "created_at", "id"}),
		fmt.Sprintf(`
create table if not exists evidence_records (
  id %s primary key,
  run_id %s not null,
  case_run_id %s not null,
  step_id %s not null,
  kind %s not null,
  uri %s not null,
  media_type %s not null,
  sha256 %s not null,
  size_bytes %s not null,
  summary %s not null,
  category %s not null,
  visibility %s not null,
  labels_json %s not null,
  created_at %s not null
);`, runIDText, runIDText, runIDText, keyText, keyText, text, text, text, intType, text, keyText, keyText, jsonType, timeType),
		d.CreateIndexSQL("idx_evidence_records_run_id_created_at", "evidence_records", []string{"run_id", "created_at", "id"}),
		fmt.Sprintf(`
create table if not exists trace_topologies (
  id %s primary key,
  workflow_run_id %s not null,
  workflow_id %s not null,
  step_id %s not null,
  case_id %s not null,
  request_id %s not null,
  trace_id %s not null,
  status %s not null,
  topology_json %s not null,
  text_topology %s not null,
  created_at %s not null
);`, runIDText, runIDText, keyText, keyText, keyText, runIDText, runIDText, keyText, jsonType, text, timeType),
		d.CreateIndexSQL("idx_trace_topologies_workflow_run_id_created_at", "trace_topologies", []string{"workflow_run_id", "created_at", "id"}),
		fmt.Sprintf(`
create table if not exists post_process_tasks (
  id %s primary key,
  run_id %s not null,
  workflow_id %s not null,
  step_id %s not null,
  case_id %s not null,
  kind %s not null,
  status %s not null,
  started_at %s,
  finished_at %s,
  duration_ms %s not null,
  error %s not null,
  summary_json %s not null,
  created_at %s not null
);`, runIDText, runIDText, keyText, keyText, keyText, keyText, keyText, timeType, timeType, intType, text, jsonType, timeType),
		d.CreateIndexSQL("idx_post_process_tasks_run_id_created_at", "post_process_tasks", []string{"run_id", "created_at", "id"}),
		fmt.Sprintf(`
create table if not exists baseline_gates (
  profile_id %s not null,
  subject_id %s not null,
  status %s not null,
  required %s not null,
  summary_json %s not null,
  checked_at %s,
  updated_at %s not null,
  primary key (profile_id, subject_id)
);`, keyText, keyText, keyText, boolType, jsonType, timeType, timeType),
		fmt.Sprintf(`
create table if not exists profile_indexes (
  profile_id %s primary key,
  bundle_path %s not null,
  bundle_digest %s not null,
  summary_json %s not null,
  imported_at %s,
  updated_at %s not null
);`, keyText, text, text, jsonType, timeType, timeType),
		fmt.Sprintf(`
create table if not exists config_versions (
  id %s primary key,
  profile_id %s not null,
  source_path %s not null,
  bundle_digest %s not null,
  summary_json %s not null,
  active %s not null,
  published_at %s not null,
  created_at %s not null
);`, keyText, keyText, text, text, jsonType, boolType, timeType, timeType),
		d.CreateIndexSQL("idx_config_versions_active_published", "config_versions", []string{"active", "published_at", "id"}),
		fmt.Sprintf(`
create table if not exists config_read_model (
  profile_id %s not null,
  model_key %s not null,
  config_version_id %s not null,
  payload_json %s not null,
  generated_at %s not null,
  updated_at %s not null,
  primary key (profile_id, model_key)
);`, keyText, keyText, keyText, jsonType, timeType, timeType),
		fmt.Sprintf(`
create table if not exists profile_catalogs (
  profile_id %s primary key,
  indexed_at %s not null,
  catalog_json %s not null,
  services %s not null,
  workflows %s not null,
  interface_nodes %s not null,
  api_cases %s not null,
  request_templates %s not null,
  workflow_bindings %s not null,
  case_dependencies %s not null,
  fixtures %s not null,
  templates %s not null,
  template_configs %s not null
);`, keyText, timeType, jsonType, intType, intType, intType, intType, intType, intType, intType, intType, intType, intType),
		fmt.Sprintf(`
create table if not exists environments (
  id %s primary key,
  display_name %s not null,
  description %s not null,
  status %s not null,
  verified %s not null,
  services_json %s not null,
  repos_json %s not null,
  compose_json %s not null,
  health_checks_json %s not null,
  verification_workflow_id %s not null,
  last_verification_run_id %s not null,
  last_verification_status %s not null,
  evidence_complete %s not null,
  topology_complete %s not null,
  last_verified_at %s,
  summary_json %s not null,
  created_at %s not null,
  updated_at %s not null
);`, keyText, text, text, keyText, boolType, jsonType, jsonType, jsonType, jsonType, keyText, runIDText, keyText, boolType, boolType, timeType, jsonType, timeType, timeType),
		d.CreateIndexSQL("idx_environments_verified_status", "environments", []string{"verified", "status", "updated_at", "id"}),
		d.CreateIndexSQL("idx_environments_verification", "environments", []string{"verification_workflow_id", "last_verification_status", "updated_at", "id"}),
		fmt.Sprintf(`
create table if not exists environment_components (
  env_id %s not null,
  component_id %s not null,
  display_name %s not null,
  kind %s not null,
  role %s not null,
  compose_service %s not null,
  image %s not null,
  required %s not null,
  runtime_json %s not null,
  healthcheck_json %s not null,
  summary_json %s not null,
  created_at %s not null,
  updated_at %s not null,
  primary key (env_id, component_id),
  foreign key (env_id) references environments(id) on delete cascade
);`, keyText, keyText, text, keyText, keyText, keyText, text, boolType, jsonType, jsonType, jsonType, timeType, timeType),
		d.CreateIndexSQL("idx_environment_components_kind", "environment_components", []string{"env_id", "kind", "role", "component_id"}),
		fmt.Sprintf(`
create table if not exists service_dependencies (
  env_id %s not null,
  service_id %s not null,
  dependency_component_id %s not null,
  dependency_kind %s not null,
  required %s not null,
  profile_json %s not null,
  created_at %s not null,
  updated_at %s not null,
  primary key (env_id, service_id, dependency_component_id, dependency_kind),
  foreign key (env_id, service_id) references environment_components(env_id, component_id) on delete cascade,
  foreign key (env_id, dependency_component_id) references environment_components(env_id, component_id) on delete cascade
);`, keyText, keyText, keyText, keyText, boolType, jsonType, timeType, timeType),
		d.CreateIndexSQL("idx_service_dependencies_component", "service_dependencies", []string{"env_id", "dependency_component_id", "dependency_kind", "service_id"}),
		fmt.Sprintf(`
create table if not exists service_config_assets (
  env_id %s not null,
  service_id %s not null,
  asset_id %s not null,
  asset_kind %s not null,
  target_component_id %s not null,
  target_path %s not null,
  content_inline %s not null,
  remote_ref_json %s not null,
  sha256 %s not null,
  size_bytes %s not null,
  apply_order %s not null,
  %s %s not null,
  summary_json %s not null,
  created_at %s not null,
  updated_at %s not null,
  primary key (env_id, service_id, asset_id),
  foreign key (env_id, service_id) references environment_components(env_id, component_id) on delete cascade,
  foreign key (env_id, target_component_id) references environment_components(env_id, component_id) on delete cascade
);`, keyText, keyText, keyText, keyText, keyText, text, text, jsonType, text, intType, intType, d.QuoteIdent("sensitive"), boolType, jsonType, timeType, timeType),
		d.CreateIndexSQL("idx_service_config_assets_target", "service_config_assets", []string{"env_id", "target_component_id", "asset_kind", "apply_order", "asset_id"}),
		d.CreateIndexSQL("idx_service_config_assets_service_order", "service_config_assets", []string{"env_id", "service_id", "apply_order", "asset_id"}),
		fmt.Sprintf(`
create table if not exists component_dependencies (
  env_id %s not null,
  consumer_component_id %s not null,
  provider_component_id %s not null,
  phase %s not null,
  capability %s not null,
  required %s not null,
  profile_json %s not null,
  created_at %s not null,
  updated_at %s not null,
  primary key (env_id, consumer_component_id, provider_component_id, phase, capability),
  foreign key (env_id, consumer_component_id) references environment_components(env_id, component_id) on delete cascade,
  foreign key (env_id, provider_component_id) references environment_components(env_id, component_id) on delete cascade
);`, keyText, keyText, keyText, keyText, keyText, boolType, jsonType, timeType, timeType),
		d.CreateIndexSQL("idx_component_dependencies_provider", "component_dependencies", []string{"env_id", "provider_component_id", "phase", "capability", "consumer_component_id"}),
		d.CreateIndexSQL("idx_component_dependencies_phase", "component_dependencies", []string{"env_id", "phase", "capability", "consumer_component_id", "provider_component_id"}),
		fmt.Sprintf(`
create table if not exists component_config_assets (
  env_id %s not null,
  owner_component_id %s not null,
  asset_id %s not null,
  asset_kind %s not null,
  target_component_id %s not null,
  target_path %s not null,
  content_inline %s not null,
  remote_ref_json %s not null,
  sha256 %s not null,
  size_bytes %s not null,
  apply_order %s not null,
  %s %s not null,
  summary_json %s not null,
  created_at %s not null,
  updated_at %s not null,
  primary key (env_id, owner_component_id, asset_id),
  foreign key (env_id, owner_component_id) references environment_components(env_id, component_id) on delete cascade,
  foreign key (env_id, target_component_id) references environment_components(env_id, component_id) on delete cascade
);`, keyText, keyText, keyText, keyText, keyText, text, text, jsonType, text, intType, intType, d.QuoteIdent("sensitive"), boolType, jsonType, timeType, timeType),
		d.CreateIndexSQL("idx_component_config_assets_target", "component_config_assets", []string{"env_id", "target_component_id", "asset_kind", "apply_order", "asset_id"}),
		d.CreateIndexSQL("idx_component_config_assets_owner_order", "component_config_assets", []string{"env_id", "owner_component_id", "apply_order", "asset_id"}),
	}
}

func runIdentifierTextType(d Dialect) string {
	if d.Name() == "mysql" {
		return "varchar(255)"
	}
	return d.KeyTextType()
}

func incrementalSchemaSQL(d Dialect, current int) []string {
	if d.Name() != "mysql" || current == 0 || current >= 5 {
		return nil
	}
	return []string{
		"alter table `runs` modify column `id` varchar(255) not null;",
		"alter table `api_case_runs` modify column `id` varchar(255) not null, modify column `run_id` varchar(255) not null;",
		"alter table `evidence_records` modify column `id` varchar(255) not null, modify column `run_id` varchar(255) not null, modify column `case_run_id` varchar(255) not null;",
		"alter table `trace_topologies` modify column `id` varchar(255) not null, modify column `workflow_run_id` varchar(255) not null, modify column `request_id` varchar(255) not null, modify column `trace_id` varchar(255) not null;",
		"alter table `post_process_tasks` modify column `id` varchar(255) not null, modify column `run_id` varchar(255) not null;",
		"alter table `environments` modify column `last_verification_run_id` varchar(255) not null;",
	}
}

func isIdempotentSchemaReplayError(d Dialect, statement string, err error) bool {
	if err == nil || d.Name() != "mysql" {
		return false
	}
	normalizedStatement := strings.ToLower(strings.TrimSpace(statement))
	if !strings.HasPrefix(normalizedStatement, "create index ") {
		return false
	}
	normalizedError := strings.ToLower(err.Error())
	return strings.Contains(normalizedError, "duplicate key name") || strings.Contains(normalizedError, "error 1061")
}

func bindVars(d Dialect, count int) string {
	var out []string
	for i := 1; i <= count; i++ {
		out = append(out, d.BindVar(i))
	}
	return strings.Join(out, ", ")
}
