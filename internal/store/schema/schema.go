package schema

type Change struct {
	Version int
	Name    string
	SQL     string
}

const CurrentVersion = 17

func All() []Change {
	return []Change{
		{
			Version: 1,
			Name:    "create runtime store tables",
			SQL: `
create table if not exists runs (
  id text primary key,
  profile_id text not null,
  workflow_id text not null,
  status text not null,
  evidence_root text not null,
  summary_json text not null default '',
  started_at text,
  finished_at text,
  created_at text not null,
  updated_at text not null
);

create table if not exists api_case_runs (
  id text primary key,
  run_id text not null,
  case_id text not null,
  status text not null,
  request_summary_json text not null default '',
  assertion_summary_json text not null default '',
  started_at text,
  finished_at text,
  created_at text not null,
  foreign key (run_id) references runs(id) on delete cascade
);

create index if not exists idx_api_case_runs_run_id_created_at
  on api_case_runs(run_id, created_at, id);

create table if not exists evidence_records (
  id text primary key,
  run_id text not null,
  case_run_id text not null default '',
  kind text not null,
  uri text not null,
  media_type text not null default '',
  sha256 text not null default '',
  size_bytes integer not null default 0,
  summary text not null default '',
  created_at text not null,
  foreign key (run_id) references runs(id) on delete cascade
);

create index if not exists idx_evidence_records_run_id_created_at
  on evidence_records(run_id, created_at, id);

create table if not exists baseline_gates (
  profile_id text not null,
  subject_id text not null,
  status text not null,
  required integer not null,
  summary_json text not null default '',
  checked_at text,
  updated_at text not null,
  primary key (profile_id, subject_id)
);

create table if not exists profile_indexes (
  profile_id text primary key,
  bundle_path text not null,
  bundle_digest text not null,
  summary_json text not null default '',
  imported_at text,
  updated_at text not null
);`,
		},
		{
			Version: 2,
			Name:    "add template config catalog tables",
			SQL: `
create table if not exists kv (
  key text primary key,
  value text not null,
  updated_at text not null
);

create table if not exists template (
  id text primary key,
  name text not null default '',
  kind text not null default '',
  version text not null default '',
  parent_id text not null default '',
  path text not null default '',
  watermark text not null default '',
  status text not null default 'active',
  sort_order integer not null default 0
);

create index if not exists idx_template_parent_sort
  on template(parent_id, sort_order, id);

create table if not exists template_config (
  id text primary key,
  template_id text not null,
  node_id text not null default '',
  workflow_id text not null default '',
  scope_type text not null default '',
  scope_id text not null default '',
  title text not null default '',
  description text not null default '',
  config_json text not null default '{}',
  status text not null default 'active',
  sort_order integer not null default 0
);

create index if not exists idx_template_config_template_sort
  on template_config(template_id, sort_order, id);

create index if not exists idx_template_config_node
  on template_config(node_id, scope_type, sort_order, id);

create index if not exists idx_template_config_workflow
  on template_config(workflow_id, scope_type, sort_order, id);

create table if not exists node_config (
  id text primary key,
  display_name text not null default '',
  role text not null default '',
  attached_template_ids text not null default '[]',
  git_url text not null default '',
  git_branch text not null default '',
  repo_env text not null default '',
  container_name text not null default '',
  image text not null default '',
  docker_service text not null default '',
  service_port integer not null default 0,
  management_port integer not null default 0,
  memory_mb integer not null default 0,
  cpu_milli integer not null default 0,
  startup_command text not null default '',
  health_url text not null default '',
  log_path text not null default '',
  status text not null default 'active',
  sort_order integer not null default 0
);

create index if not exists idx_node_config_role_sort
  on node_config(role, sort_order, id);

create table if not exists workflow (
  id text primary key,
  name text not null default '',
  template_id text not null,
  template_config_id text not null,
  description text not null default '',
  status text not null default 'active',
  sort_order integer not null default 0
);

create index if not exists idx_workflow_template_sort
  on workflow(template_id, sort_order, id);

create table if not exists workflow_node (
  workflow_id text not null,
  node_id text not null,
  relation_type text not null default 'required',
  required integer not null default 1,
  sort_order integer not null default 0,
  primary key(workflow_id, node_id, relation_type)
);

create index if not exists idx_workflow_node_node
  on workflow_node(node_id, workflow_id);

create table if not exists interface_node (
  id text primary key,
  display_name text not null default '',
  service_id text not null default '',
  operation text not null default '',
  method text not null default '',
  path text not null default '',
  template_id text not null default '',
  version text not null default 'v1',
  status text not null default 'draft',
  tags_json text not null default '[]',
  description text not null default '',
  sort_order integer not null default 0,
  created_at text not null,
  updated_at text not null
);

create index if not exists idx_interface_node_service_operation
  on interface_node(service_id, operation, status);

create index if not exists idx_interface_node_template_sort
  on interface_node(template_id, sort_order, id);

create table if not exists interface_node_field (
  id text primary key,
  node_id text not null,
  direction text not null,
  field_path text not null,
  display_name text not null default '',
  data_type text not null default '',
  required integer not null default 0,
  bindable integer not null default 0,
  port_type text not null default 'DATA',
  status text not null default 'active',
  sort_order integer not null default 0
);

create index if not exists idx_interface_node_field_node_direction
  on interface_node_field(node_id, direction, sort_order, id);

create table if not exists interface_node_request_template (
  id text primary key,
  node_id text not null,
  name text not null default '',
  template_json text not null default '{}',
  version text not null default 'v1',
  status text not null default 'active',
  sort_order integer not null default 0,
  created_at text not null,
  updated_at text not null
);

create index if not exists idx_interface_node_request_template_node
  on interface_node_request_template(node_id, status, sort_order, id);

create table if not exists interface_node_case (
  id text primary key,
  node_id text not null,
  title text not null default '',
  case_type text not null,
  scenario text not null default '',
  payload_template_json text not null default '{}',
  request_template_id text not null default '',
  patch_json text not null default '[]',
  render_mode text not null default 'legacy_payload',
  expected_json text not null default '{}',
  required_for_admission integer not null default 1,
  status text not null default 'active',
  sort_order integer not null default 0,
  created_at text not null,
  updated_at text not null
);

create index if not exists idx_interface_node_case_node_type
  on interface_node_case(node_id, case_type, sort_order, id);

create table if not exists workflow_interface_node (
  workflow_id text not null,
  step_id text not null,
  node_id text not null,
  case_id text not null default '',
  required integer not null default 1,
  sort_order integer not null default 0,
  primary key(workflow_id, step_id)
);

create index if not exists idx_workflow_interface_node_node
  on workflow_interface_node(node_id, workflow_id);

create index if not exists idx_workflow_interface_node_case
  on workflow_interface_node(case_id, workflow_id);

create table if not exists fixture_profile (
  id text primary key,
  name text not null default '',
  source_type text not null default '',
  source_workflow_id text not null default '',
  source_until_step text not null default '',
  ttl_seconds integer not null default 0,
  status text not null default 'active',
  description text not null default '',
  sort_order integer not null default 0,
  created_at text not null,
  updated_at text not null
);

create index if not exists idx_fixture_profile_source
  on fixture_profile(source_type, source_workflow_id, source_until_step, status);

create table if not exists fixture_table_binding (
  id text primary key,
  profile_id text not null,
  schema_name text not null default '',
  table_name text not null default '',
  key_fields_json text not null default '[]',
  extract_sql text not null default '',
  apply_mode text not null default 'upsert',
  status text not null default 'active',
  sort_order integer not null default 0
);

create index if not exists idx_fixture_table_binding_profile
  on fixture_table_binding(profile_id, sort_order, id);

create table if not exists interface_node_case_dependency (
  id text primary key,
  case_id text not null,
  fixture_profile_id text not null,
  required integer not null default 1,
  mappings_json text not null default '[]',
  status text not null default 'active',
  sort_order integer not null default 0
);

create index if not exists idx_interface_node_case_dependency_case
  on interface_node_case_dependency(case_id, status, sort_order);

create index if not exists idx_interface_node_case_dependency_fixture
  on interface_node_case_dependency(fixture_profile_id, status);`,
		},
		{
			Version: 3,
			Name:    "add trace topology evidence",
			SQL: `
create table if not exists trace_topologies (
  id text primary key,
  workflow_run_id text not null,
  workflow_id text not null default '',
  step_id text not null default '',
  case_id text not null default '',
  request_id text not null default '',
  trace_id text not null default '',
  status text not null default 'unknown',
  topology_json text not null default '{}',
  text_topology text not null default '',
  created_at text not null,
  foreign key (workflow_run_id) references runs(id) on delete cascade
);

create index if not exists idx_trace_topologies_workflow_run
  on trace_topologies(workflow_run_id, created_at, id);

create index if not exists idx_trace_topologies_case
  on trace_topologies(workflow_run_id, case_id, step_id);`,
		},
		{
			Version: 4,
			Name:    "add execution budgets",
			SQL: `
alter table workflow add column base_step_timeout_ms integer not null default 0;
alter table workflow add column timeout_offset_ms integer not null default 0;
alter table interface_node add column timeout_ms integer not null default 0;`,
		},
		{
			Version: 5,
			Name:    "add post process task records",
			SQL: `
create table if not exists post_process_tasks (
  id text primary key,
  run_id text not null,
  workflow_id text not null default '',
  step_id text not null default '',
  case_id text not null default '',
  kind text not null,
  status text not null,
  started_at text,
  finished_at text,
  duration_ms integer not null default 0,
  error text not null default '',
  summary_json text not null default '{}',
  created_at text not null,
  foreign key (run_id) references runs(id) on delete cascade
);

create index if not exists idx_post_process_tasks_run_created
  on post_process_tasks(run_id, created_at, id);

create index if not exists idx_post_process_tasks_kind_status
  on post_process_tasks(kind, status, created_at);`,
		},
		{
			Version: 6,
			Name:    "add latest api case run lookup index",
			SQL: `
create index if not exists idx_api_case_runs_case_created
  on api_case_runs(case_id, created_at, id);`,
		},
		{
			Version: 7,
			Name:    "add service source path config",
			SQL: `
alter table node_config add column source_path text not null default '';`,
		},
		{
			Version: 8,
			Name:    "add config version catalog",
			SQL: `
create table if not exists config_versions (
  id text primary key,
  profile_id text not null,
  source_path text not null default '',
  bundle_digest text not null default '',
  summary_json text not null default '',
  active integer not null default 0,
  published_at text,
  created_at text not null
);

create index if not exists idx_config_versions_active
  on config_versions(active, published_at, id);

create index if not exists idx_config_versions_profile_published
  on config_versions(profile_id, published_at, id);`,
		},
		{
			Version: 9,
			Name:    "add configuration read models",
			SQL: `
create table if not exists config_read_model (
  profile_id text not null,
  model_key text not null,
  config_version_id text not null default '',
  payload_json text not null default '{}',
  generated_at text,
  updated_at text not null,
  primary key (profile_id, model_key)
);

create index if not exists idx_config_read_model_version
  on config_read_model(config_version_id, model_key);`,
		},
		{
			Version: 10,
			Name:    "add api case execution config",
			SQL: `
alter table interface_node_case add column case_path text not null default '';
alter table interface_node_case add column base_url text not null default '';
alter table interface_node_case add column evidence_dir text not null default '';
alter table interface_node_case add column timeout_seconds integer not null default 0;
alter table interface_node_case add column default_overrides_json text not null default '{}';`,
		},
		{
			Version: 11,
			Name:    "add api case maintenance metadata",
			SQL: `
alter table interface_node_case add column description text not null default '';
alter table interface_node_case add column tags_json text not null default '[]';
alter table interface_node_case add column priority text not null default '';
alter table interface_node_case add column owner text not null default '';

create index if not exists idx_interface_node_case_status_owner
  on interface_node_case(status, owner, priority, sort_order, id);`,
		},
		{
			Version: 12,
			Name:    "add evidence attachment metadata",
			SQL: `
alter table evidence_records add column category text not null default '';
alter table evidence_records add column visibility text not null default '';
alter table evidence_records add column labels_json text not null default '{}';

create index if not exists idx_evidence_records_category
  on evidence_records(category, visibility, created_at, id);`,
		},
		{
			Version: 13,
			Name:    "add api case external source refs",
			SQL: `
alter table interface_node_case add column source_kind text not null default '';
alter table interface_node_case add column source_path text not null default '';
alter table interface_node_case add column executor_id text not null default '';

create index if not exists idx_interface_node_case_executor
  on interface_node_case(executor_id, source_kind, status, sort_order, id);`,
		},
		{
			Version: 14,
			Name:    "add evidence step relation",
			SQL: `
alter table evidence_records add column step_id text not null default '';

create index if not exists idx_evidence_records_step
  on evidence_records(run_id, step_id, case_run_id, created_at, id);`,
		},
		{
			Version: 15,
			Name:    "add environment catalog",
			SQL: `
create table if not exists environments (
  id text primary key,
  display_name text not null default '',
  description text not null default '',
  status text not null default 'draft',
  verified integer not null default 0,
  services_json text not null default '[]',
  repos_json text not null default '{}',
  compose_json text not null default '{}',
  health_checks_json text not null default '[]',
  verification_workflow_id text not null default '',
  last_verification_run_id text not null default '',
  last_verification_status text not null default '',
  evidence_complete integer not null default 0,
  topology_complete integer not null default 0,
  last_verified_at text,
  summary_json text not null default '{}',
  created_at text not null,
  updated_at text not null
);

create index if not exists idx_environments_verified_status
  on environments(verified, status, updated_at, id);

create index if not exists idx_environments_verification
  on environments(verification_workflow_id, last_verification_status, updated_at, id);`,
		},
		{
			Version: 16,
			Name:    "add environment component assets",
			SQL: `
create table if not exists environment_components (
  env_id text not null,
  component_id text not null,
  display_name text not null default '',
  kind text not null default '',
  role text not null default '',
  compose_service text not null default '',
  image text not null default '',
  required integer not null default 1,
  runtime_json text not null default '{}',
  healthcheck_json text not null default '{}',
  summary_json text not null default '{}',
  created_at text not null,
  updated_at text not null,
  primary key (env_id, component_id),
  foreign key (env_id) references environments(id) on delete cascade
);

create index if not exists idx_environment_components_kind
  on environment_components(env_id, kind, role, component_id);

create table if not exists service_dependencies (
  env_id text not null,
  service_id text not null,
  dependency_component_id text not null,
  dependency_kind text not null default '',
  required integer not null default 1,
  profile_json text not null default '{}',
  created_at text not null,
  updated_at text not null,
  primary key (env_id, service_id, dependency_component_id, dependency_kind),
  foreign key (env_id, service_id) references environment_components(env_id, component_id) on delete cascade,
  foreign key (env_id, dependency_component_id) references environment_components(env_id, component_id) on delete cascade
);

create index if not exists idx_service_dependencies_component
  on service_dependencies(env_id, dependency_component_id, dependency_kind, service_id);

create table if not exists service_config_assets (
  env_id text not null,
  service_id text not null,
  asset_id text not null,
  asset_kind text not null default '',
  target_component_id text not null default '',
  target_path text not null default '',
  content_inline text not null default '',
  remote_ref_json text not null default '{}',
  sha256 text not null default '',
  size_bytes integer not null default 0,
  apply_order integer not null default 0,
  sensitive integer not null default 0,
  summary_json text not null default '{}',
  created_at text not null,
  updated_at text not null,
  primary key (env_id, service_id, asset_id),
  foreign key (env_id, service_id) references environment_components(env_id, component_id) on delete cascade,
  foreign key (env_id, target_component_id) references environment_components(env_id, component_id) on delete cascade
);

create index if not exists idx_service_config_assets_target
  on service_config_assets(env_id, target_component_id, asset_kind, apply_order, asset_id);

create index if not exists idx_service_config_assets_service_order
  on service_config_assets(env_id, service_id, apply_order, asset_id);`,
		},
		{
			Version: 17,
			Name:    "generalize environment component graph",
			SQL: `
create table if not exists component_dependencies (
  env_id text not null,
  consumer_component_id text not null,
  provider_component_id text not null,
  phase text not null default '',
  capability text not null default '',
  required integer not null default 1,
  profile_json text not null default '{}',
  created_at text not null,
  updated_at text not null,
  primary key (env_id, consumer_component_id, provider_component_id, phase, capability),
  foreign key (env_id, consumer_component_id) references environment_components(env_id, component_id) on delete cascade,
  foreign key (env_id, provider_component_id) references environment_components(env_id, component_id) on delete cascade
);

create index if not exists idx_component_dependencies_provider
  on component_dependencies(env_id, provider_component_id, phase, capability, consumer_component_id);

create index if not exists idx_component_dependencies_phase
  on component_dependencies(env_id, phase, capability, consumer_component_id, provider_component_id);

create table if not exists component_config_assets (
  env_id text not null,
  owner_component_id text not null,
  asset_id text not null,
  asset_kind text not null default '',
  target_component_id text not null default '',
  target_path text not null default '',
  content_inline text not null default '',
  remote_ref_json text not null default '{}',
  sha256 text not null default '',
  size_bytes integer not null default 0,
  apply_order integer not null default 0,
  sensitive integer not null default 0,
  summary_json text not null default '{}',
  created_at text not null,
  updated_at text not null,
  primary key (env_id, owner_component_id, asset_id),
  foreign key (env_id, owner_component_id) references environment_components(env_id, component_id) on delete cascade,
  foreign key (env_id, target_component_id) references environment_components(env_id, component_id) on delete cascade
);

create index if not exists idx_component_config_assets_target
  on component_config_assets(env_id, target_component_id, asset_kind, apply_order, asset_id);

create index if not exists idx_component_config_assets_owner_order
  on component_config_assets(env_id, owner_component_id, apply_order, asset_id);`,
		},
	}
}
