package sqlstore

import "fmt"

func CoreSchemaSQL(d Dialect) []string {
	text := "text"
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
);`, text, text, text, text, text, jsonType, timeType, timeType, timeType, timeType),
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
);`, text, text, text, text, jsonType, jsonType, timeType, timeType, timeType),
		`
create index if not exists idx_api_case_runs_run_id_created_at
  on api_case_runs(run_id, created_at, id);`,
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
);`, text, text, text, text, text, text, text, text, intType, text, text, text, jsonType, timeType),
		`
create index if not exists idx_evidence_records_run_id_created_at
  on evidence_records(run_id, created_at, id);`,
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
);`, text, text, text, text, text, text, text, text, jsonType, text, timeType),
		`
create index if not exists idx_trace_topologies_workflow_run_id_created_at
  on trace_topologies(workflow_run_id, created_at, id);`,
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
);`, text, text, text, text, text, text, text, timeType, timeType, intType, text, jsonType, timeType),
		`
create index if not exists idx_post_process_tasks_run_id_created_at
  on post_process_tasks(run_id, created_at, id);`,
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
);`, text, text, text, boolType, jsonType, timeType, timeType),
		fmt.Sprintf(`
create table if not exists profile_indexes (
  profile_id %s primary key,
  bundle_path %s not null,
  bundle_digest %s not null,
  summary_json %s not null,
  imported_at %s,
  updated_at %s not null
);`, text, text, text, jsonType, timeType, timeType),
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
);`, text, text, text, text, jsonType, boolType, timeType, timeType),
		`
create index if not exists idx_config_versions_active_published
  on config_versions(active, published_at, id);`,
		fmt.Sprintf(`
create table if not exists config_read_model (
  profile_id %s not null,
  model_key %s not null,
  config_version_id %s not null,
  payload_json %s not null,
  generated_at %s not null,
  updated_at %s not null,
  primary key (profile_id, model_key)
);`, text, text, text, jsonType, timeType, timeType),
	}
}
