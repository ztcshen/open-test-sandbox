package sqlstore

import (
	"fmt"
	"strings"
)

type schemaTableComment struct {
	Table   string
	Comment string
	Columns []schemaColumnComment
}

type schemaColumnComment struct {
	Name         string
	MySQLType    string
	MySQLDefault string
	Nullable     bool
	Comment      string
}

type schemaCommentMySQLTypes struct {
	v128     string
	v255     string
	intType  string
	text     string
	jsonType string
	timeType string
	boolType string
}

func schemaCommentSQL(d Dialect) []string {
	if d.Name() == "sqlite" {
		return nil
	}
	specs := schemaCommentSpecs()
	statements := []string{}
	for _, table := range specs {
		switch d.Name() {
		case "postgres":
			statements = append(statements, fmt.Sprintf("comment on table %s is %s;", d.QuoteIdent(table.Table), sqlLiteral(table.Comment)))
			for _, column := range table.Columns {
				statements = append(statements, fmt.Sprintf("comment on column %s.%s is %s;", d.QuoteIdent(table.Table), d.QuoteIdent(column.Name), sqlLiteral(column.Comment)))
			}
		case "mysql":
			statements = append(statements, fmt.Sprintf("alter table %s comment = %s;", d.QuoteIdent(table.Table), sqlLiteral(table.Comment)))
			for _, column := range table.Columns {
				nullable := " not null"
				if column.Nullable {
					nullable = ""
				}
				defaultClause := ""
				if column.MySQLDefault != "" {
					defaultClause = " default " + column.MySQLDefault
				}
				statements = append(statements, fmt.Sprintf("alter table %s modify column %s %s%s%s comment %s;", d.QuoteIdent(table.Table), d.QuoteIdent(column.Name), column.MySQLType, nullable, defaultClause, sqlLiteral(column.Comment)))
			}
		}
	}
	return statements
}

func schemaCommentSpecs() []schemaTableComment {
	types := schemaCommentMySQLTypes{
		v128:     "varchar(128)",
		v255:     mysqlVarchar255Type,
		intType:  "integer",
		text:     "mediumtext",
		jsonType: "json",
		timeType: "datetime(6)",
		boolType: "boolean",
	}
	specs := coreRunCommentSpecs(types)
	specs = append(specs, observabilityCommentSpecs(types)...)
	specs = append(specs, profileConfigCommentSpecs(types)...)
	return append(specs, environmentCatalogCommentSpecs(types)...)
}

func coreRunCommentSpecs(types schemaCommentMySQLTypes) []schemaTableComment {
	return []schemaTableComment{
		schemaVersionsCommentSpec(types),
		runsCommentSpec(types),
		apiCaseRunsCommentSpec(types),
		evidenceRecordsCommentSpec(types),
	}
}

func schemaVersionsCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "schema_versions",
		Comment: "Applied SQL Store schema versions.",
		Columns: []schemaColumnComment{
			{Name: "version", MySQLType: types.intType, Comment: "Monotonic SQL Store schema version."},
			{Name: "name", MySQLType: types.text, Comment: "Human-readable migration name."},
			{Name: "applied_at", MySQLType: types.timeType, Comment: "UTC time when the schema version was recorded."},
		},
	}
}

func runsCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "runs",
		Comment: "Workflow run records and their execution summary.",
		Columns: []schemaColumnComment{
			{Name: "id", MySQLType: types.v255, Comment: "Stable workflow run identifier."},
			{Name: "profile_id", MySQLType: types.v128, Comment: "Profile that supplied the workflow definition."},
			{Name: "environment_id", MySQLType: types.v128, MySQLDefault: "''", Comment: "Environment where the workflow run executed."},
			{Name: "workflow_id", MySQLType: types.v128, Comment: "Workflow definition executed by this run."},
			{Name: "status", MySQLType: types.v128, Comment: "Current workflow run status."},
			{Name: "evidence_root", MySQLType: types.text, Comment: "Root URI or path for evidence produced by the run."},
			{Name: "summary_json", MySQLType: types.jsonType, Comment: "Machine-readable run summary used by APIs and reports."},
			{Name: "started_at", MySQLType: types.timeType, Nullable: true, Comment: "UTC time when execution started."},
			{Name: "finished_at", MySQLType: types.timeType, Nullable: true, Comment: "UTC time when execution finished."},
			{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the run record was created."},
			{Name: "updated_at", MySQLType: types.timeType, Comment: "UTC time when the run record last changed."},
		},
	}
}

func apiCaseRunsCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "api_case_runs",
		Comment: "API case execution records belonging to workflow runs.",
		Columns: []schemaColumnComment{
			{Name: "id", MySQLType: types.v255, Comment: "Stable API case run identifier."},
			{Name: "run_id", MySQLType: types.v255, Comment: "Workflow run that owns this API case run."},
			{Name: "case_id", MySQLType: types.v128, Comment: "API case definition executed by this record."},
			{Name: "status", MySQLType: types.v128, Comment: "Current API case run status."},
			{Name: "request_summary_json", MySQLType: types.jsonType, Comment: "Rendered request metadata captured for the case run."},
			{Name: "assertion_summary_json", MySQLType: types.jsonType, Comment: "Assertion results captured for the case run."},
			{Name: "started_at", MySQLType: types.timeType, Nullable: true, Comment: "UTC time when the case run started."},
			{Name: "finished_at", MySQLType: types.timeType, Nullable: true, Comment: "UTC time when the case run finished."},
			{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the case run record was created."},
		},
	}
}

func evidenceRecordsCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "evidence_records",
		Comment: "Evidence artifacts indexed for workflow and API case runs.",
		Columns: []schemaColumnComment{
			{Name: "id", MySQLType: types.v255, Comment: "Stable evidence record identifier."},
			{Name: "run_id", MySQLType: types.v255, Comment: "Workflow run associated with the evidence."},
			{Name: "case_run_id", MySQLType: types.v255, Comment: "API case run associated with the evidence."},
			{Name: "step_id", MySQLType: types.v128, Comment: "Workflow step that produced the evidence."},
			{Name: "kind", MySQLType: types.v128, Comment: "Evidence kind such as request, response, log, or screenshot."},
			{Name: "uri", MySQLType: types.text, Comment: "URI or local path where the evidence artifact can be retrieved."},
			{Name: "media_type", MySQLType: types.text, Comment: "Content media type for the evidence artifact."},
			{Name: sha256ColumnName, MySQLType: types.text, Comment: "SHA-256 digest for integrity checks."},
			{Name: "size_bytes", MySQLType: types.intType, Comment: "Evidence artifact size in bytes."},
			{Name: "summary", MySQLType: types.text, Comment: "Short human-readable evidence summary."},
			{Name: "category", MySQLType: types.v128, Comment: "Evidence category used for grouping and filtering."},
			{Name: "visibility", MySQLType: types.v128, Comment: "Visibility classification for sharing or redaction."},
			{Name: "labels_json", MySQLType: types.jsonType, Comment: "Structured labels attached to the evidence record."},
			{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the evidence record was created."},
		},
	}
}

func observabilityCommentSpecs(types schemaCommentMySQLTypes) []schemaTableComment {
	return []schemaTableComment{
		{
			Table:   "trace_topologies",
			Comment: "Observed service topology snapshots linked to workflow evidence.",
			Columns: []schemaColumnComment{
				{Name: "id", MySQLType: types.v255, Comment: "Stable topology record identifier."},
				{Name: "workflow_run_id", MySQLType: types.v255, Comment: "Workflow run associated with the topology."},
				{Name: "workflow_id", MySQLType: types.v128, Comment: "Workflow definition associated with the topology."},
				{Name: "step_id", MySQLType: types.v128, Comment: "Workflow step that collected the topology."},
				{Name: "case_id", MySQLType: types.v128, Comment: "API case associated with the topology."},
				{Name: "request_id", MySQLType: types.v255, Comment: "Request identifier used to correlate trace data."},
				{Name: "trace_id", MySQLType: types.v255, Comment: "Trace identifier from the observability backend."},
				{Name: "status", MySQLType: types.v128, Comment: "Topology collection status."},
				{Name: "topology_json", MySQLType: types.jsonType, Comment: "Structured topology graph and diagnostic metadata."},
				{Name: "text_topology", MySQLType: types.text, Comment: "Human-readable topology rendering."},
				{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the topology record was created."},
			},
		},
		{
			Table:   "post_process_tasks",
			Comment: "Post-processing task records for evidence and verification workflows.",
			Columns: []schemaColumnComment{
				{Name: "id", MySQLType: types.v255, Comment: "Stable post-process task identifier."},
				{Name: "run_id", MySQLType: types.v255, Comment: "Workflow run associated with the task."},
				{Name: "workflow_id", MySQLType: types.v128, Comment: "Workflow definition associated with the task."},
				{Name: "step_id", MySQLType: types.v128, Comment: "Workflow step associated with the task."},
				{Name: "case_id", MySQLType: types.v128, Comment: "API case associated with the task."},
				{Name: "kind", MySQLType: types.v128, Comment: "Post-processing task kind."},
				{Name: "status", MySQLType: types.v128, Comment: "Current post-processing task status."},
				{Name: "started_at", MySQLType: types.timeType, Nullable: true, Comment: "UTC time when the task started."},
				{Name: "finished_at", MySQLType: types.timeType, Nullable: true, Comment: "UTC time when the task finished."},
				{Name: "duration_ms", MySQLType: types.intType, Comment: "Task duration in milliseconds."},
				{Name: "error", MySQLType: types.text, Comment: "Error text captured for failed tasks."},
				{Name: "summary_json", MySQLType: types.jsonType, Comment: "Structured task summary and outputs."},
				{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the task record was created."},
			},
		},
		{
			Table:   "baseline_gates",
			Comment: "Baseline gate status for profiles and verification subjects.",
			Columns: []schemaColumnComment{
				{Name: "profile_id", MySQLType: types.v255, Comment: "Profile that owns the baseline gate."},
				{Name: "subject_id", MySQLType: types.v128, Comment: "Gate subject within the profile."},
				{Name: "status", MySQLType: types.v128, Comment: "Current baseline gate status."},
				{Name: "required", MySQLType: types.boolType, Comment: "Whether this gate is required for acceptance."},
				{Name: "summary_json", MySQLType: types.jsonType, Comment: "Structured baseline gate summary."},
				{Name: "checked_at", MySQLType: types.timeType, Nullable: true, Comment: "UTC time when the gate was last checked."},
				{Name: "updated_at", MySQLType: types.timeType, Comment: "UTC time when the gate record last changed."},
			},
		},
	}
}

func profileConfigCommentSpecs(types schemaCommentMySQLTypes) []schemaTableComment {
	return []schemaTableComment{
		profileIndexesCommentSpec(types),
		configVersionsCommentSpec(types),
		configReadModelCommentSpec(types),
		profileCatalogsCommentSpec(types),
	}
}

func profileIndexesCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "profile_indexes",
		Comment: "Imported profile bundle indexes.",
		Columns: []schemaColumnComment{
			{Name: "profile_id", MySQLType: types.v255, Comment: "Profile identifier."},
			{Name: "bundle_path", MySQLType: types.text, Comment: "Path to the imported profile bundle."},
			{Name: "bundle_digest", MySQLType: types.text, Comment: "Digest of the imported profile bundle."},
			{Name: "summary_json", MySQLType: types.jsonType, Comment: "Structured profile index summary."},
			{Name: "imported_at", MySQLType: types.timeType, Nullable: true, Comment: "UTC time when the profile was imported."},
			{Name: "updated_at", MySQLType: types.timeType, Comment: "UTC time when the profile index last changed."},
		},
	}
}

func configVersionsCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "config_versions",
		Comment: "Published configuration versions derived from profile bundles.",
		Columns: []schemaColumnComment{
			{Name: "id", MySQLType: types.v255, Comment: "Stable configuration version identifier."},
			{Name: "profile_id", MySQLType: types.v255, Comment: "Profile that owns this configuration version."},
			{Name: "source_path", MySQLType: types.text, Comment: "Source path used to create the configuration version."},
			{Name: "bundle_digest", MySQLType: types.text, Comment: "Digest of the source bundle."},
			{Name: "summary_json", MySQLType: types.jsonType, Comment: "Structured configuration version summary."},
			{Name: "active", MySQLType: types.boolType, Comment: "Whether this version is active for its profile."},
			{Name: "published_at", MySQLType: types.timeType, Comment: "UTC time when the version was published."},
			{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the version record was created."},
		},
	}
}

func configReadModelCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "config_read_model",
		Comment: "Derived read models for active configuration versions.",
		Columns: []schemaColumnComment{
			{Name: "profile_id", MySQLType: types.v255, Comment: "Profile that owns the read model."},
			{Name: "model_key", MySQLType: types.v255, Comment: "Read model key within the profile."},
			{Name: "config_version_id", MySQLType: types.v255, Comment: "Configuration version used to generate the read model."},
			{Name: "payload_json", MySQLType: types.jsonType, Comment: "Structured read model payload."},
			{Name: "generated_at", MySQLType: types.timeType, Comment: "UTC time when the read model was generated."},
			{Name: "updated_at", MySQLType: types.timeType, Comment: "UTC time when the read model last changed."},
		},
	}
}

func profileCatalogsCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	catalogCountColumns := []string{
		"services",
		"workflows",
		"interface_nodes",
		"api_cases",
		"request_templates",
		"workflow_bindings",
		"case_dependencies",
		"fixtures",
		"templates",
		"template_configs",
	}
	columns := make([]schemaColumnComment, 0, 3+len(catalogCountColumns))
	columns = append(columns,
		schemaColumnComment{Name: "profile_id", MySQLType: types.v255, Comment: "Profile identifier for this catalog."},
		schemaColumnComment{Name: "indexed_at", MySQLType: types.timeType, Comment: "UTC time when the catalog was indexed."},
		schemaColumnComment{Name: "catalog_json", MySQLType: types.jsonType, Comment: "Full structured profile catalog payload."},
	)
	for _, name := range catalogCountColumns {
		label := strings.ReplaceAll(name, "_", " ")
		columns = append(columns, schemaColumnComment{
			Name:      name,
			MySQLType: types.intType,
			Comment:   fmt.Sprintf("Number of %s in the catalog.", label),
		})
	}
	return schemaTableComment{
		Table:   "profile_catalogs",
		Comment: "Searchable profile catalog snapshots.",
		Columns: columns,
	}
}

func environmentCatalogCommentSpecs(types schemaCommentMySQLTypes) []schemaTableComment {
	return []schemaTableComment{
		environmentsCommentSpec(types),
		environmentComponentsCommentSpec(types),
		componentDependenciesCommentSpec(types),
		componentConfigAssetsCommentSpec(types),
	}
}

func environmentsCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "environments",
		Comment: "Registered test environments and their verification state.",
		Columns: []schemaColumnComment{
			{Name: "id", MySQLType: types.v128, Comment: "Stable environment identifier."},
			{Name: "display_name", MySQLType: types.text, Comment: "Human-readable environment name."},
			{Name: "description", MySQLType: types.text, Comment: "Environment description for operators."},
			{Name: "status", MySQLType: types.v128, Comment: "Current environment status."},
			{Name: "verified", MySQLType: types.boolType, Comment: "Whether the environment passed verification."},
			{Name: "services_json", MySQLType: types.jsonType, Comment: "Structured service discovery configuration."},
			{Name: "repos_json", MySQLType: types.jsonType, Comment: "Structured repository checkout configuration."},
			{Name: "compose_json", MySQLType: types.jsonType, Comment: "Structured Docker or process restore configuration."},
			{Name: "health_checks_json", MySQLType: types.jsonType, Comment: "Structured health checks for the environment."},
			{Name: "verification_workflow_id", MySQLType: types.v128, Comment: "Workflow used to verify the environment."},
			{Name: "last_verification_run_id", MySQLType: types.v255, Comment: "Most recent verification run identifier."},
			{Name: "last_verification_status", MySQLType: types.v128, Comment: "Status from the most recent verification run."},
			{Name: "evidence_complete", MySQLType: types.boolType, Comment: "Whether required evidence is complete."},
			{Name: "topology_complete", MySQLType: types.boolType, Comment: "Whether required topology evidence is complete."},
			{Name: "last_verified_at", MySQLType: types.timeType, Nullable: true, Comment: "UTC time when the environment was last verified."},
			{Name: "summary_json", MySQLType: types.jsonType, Comment: "Structured environment summary."},
			{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the environment record was created."},
			{Name: "updated_at", MySQLType: types.timeType, Comment: "UTC time when the environment record last changed."},
		},
	}
}

func environmentComponentsCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "environment_components",
		Comment: "Runtime components that make up a registered environment.",
		Columns: []schemaColumnComment{
			{Name: "env_id", MySQLType: types.v128, Comment: "Environment that owns the component."},
			{Name: "component_id", MySQLType: types.v128, Comment: "Stable component identifier within the environment."},
			{Name: "display_name", MySQLType: types.text, Comment: "Human-readable component name."},
			{Name: "kind", MySQLType: types.v128, Comment: "Component kind such as app, middleware, or observability."},
			{Name: "role", MySQLType: types.v128, Comment: "Component role in the environment graph."},
			{Name: "compose_service", MySQLType: types.v128, Comment: "Docker Compose service name when applicable."},
			{Name: "image", MySQLType: types.text, Comment: "Container image or runtime artifact identifier."},
			{Name: "required", MySQLType: types.boolType, Comment: "Whether the component is required for restore or verification."},
			{Name: "runtime_json", MySQLType: types.jsonType, Comment: "Structured runtime facts for the component."},
			{Name: "healthcheck_json", MySQLType: types.jsonType, Comment: "Structured health check definition for the component."},
			{Name: "summary_json", MySQLType: types.jsonType, Comment: "Structured component summary."},
			{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the component record was created."},
			{Name: "updated_at", MySQLType: types.timeType, Comment: "UTC time when the component record last changed."},
		},
	}
}

func componentDependenciesCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "component_dependencies",
		Comment: "Directed dependencies between environment components.",
		Columns: []schemaColumnComment{
			{Name: "env_id", MySQLType: types.v128, Comment: "Environment that owns the dependency."},
			{Name: "consumer_component_id", MySQLType: types.v128, Comment: "Component that consumes the provider capability."},
			{Name: "provider_component_id", MySQLType: types.v128, Comment: "Component that provides the required capability."},
			{Name: "phase", MySQLType: types.v128, Comment: "Restore or verification phase for the dependency."},
			{Name: "capability", MySQLType: types.v128, Comment: "Capability required by the consumer component."},
			{Name: "required", MySQLType: types.boolType, Comment: "Whether the dependency is required."},
			{Name: "profile_json", MySQLType: types.jsonType, Comment: "Structured dependency profile metadata."},
			{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the dependency record was created."},
			{Name: "updated_at", MySQLType: types.timeType, Comment: "UTC time when the dependency record last changed."},
		},
	}
}

func componentConfigAssetsCommentSpec(types schemaCommentMySQLTypes) schemaTableComment {
	return schemaTableComment{
		Table:   "component_config_assets",
		Comment: "Configuration assets owned by environment components.",
		Columns: []schemaColumnComment{
			{Name: "env_id", MySQLType: types.v128, Comment: "Environment that owns the asset."},
			{Name: "owner_component_id", MySQLType: types.v128, Comment: "Component that owns the asset."},
			{Name: "asset_id", MySQLType: types.v128, Comment: "Stable asset identifier within the owner component."},
			{Name: "asset_kind", MySQLType: types.v128, Comment: "Asset kind such as SQL, config, or template."},
			{Name: "target_component_id", MySQLType: types.v128, Comment: "Component where the asset should be applied."},
			{Name: "target_path", MySQLType: types.text, Comment: "Target path when the asset is materialized as a file."},
			{Name: "content_inline", MySQLType: types.text, Comment: "Inline asset content when the asset is stored directly in the Store."},
			{Name: "remote_ref_json", MySQLType: types.jsonType, Comment: "Structured reference for assets stored outside the Store."},
			{Name: sha256ColumnName, MySQLType: types.text, Comment: "SHA-256 digest for asset integrity checks."},
			{Name: "size_bytes", MySQLType: types.intType, Comment: "Asset size in bytes."},
			{Name: "apply_order", MySQLType: types.intType, Comment: "Relative order for applying assets to the target component."},
			{Name: "sensitive", MySQLType: types.boolType, Comment: "Whether the asset content contains sensitive material."},
			{Name: "summary_json", MySQLType: types.jsonType, Comment: "Structured asset summary and metadata."},
			{Name: "created_at", MySQLType: types.timeType, Comment: "UTC time when the asset record was created."},
			{Name: "updated_at", MySQLType: types.timeType, Comment: "UTC time when the asset record last changed."},
		},
	}
}
