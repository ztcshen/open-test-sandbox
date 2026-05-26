package sqlite

import (
	"context"
	"fmt"
	"strings"

	"agent-testbench/internal/store"
)

func (s *Store) ReplaceProfileCatalog(ctx context.Context, catalog store.ProfileCatalog) error {
	indexedAt := replaceProfileCatalogIndexedAt(catalog)
	statements := replaceProfileCatalogStatements(catalog, indexedAt)
	if err := s.exec(ctx, "begin;\n"+strings.Join(statements, "\n")+"\ncommit;"); err != nil {
		return fmt.Errorf("replace profile catalog index %q: %w", catalog.ProfileID, err)
	}
	return nil
}

func replaceProfileCatalogIndexedAt(catalog store.ProfileCatalog) string {
	if indexedAt := encodeTime(catalog.IndexedAt); indexedAt != "" {
		return indexedAt
	}
	return encodeTime(utcNow())
}

func replaceProfileCatalogStatements(catalog store.ProfileCatalog, indexedAt string) []string {
	statements := replaceProfileCatalogResetStatements(catalog.ProfileID, indexedAt)
	statements = append(statements, replaceCatalogServiceStatements(catalog.Services)...)
	statements = append(statements, replaceCatalogWorkflowStatements(catalog.Workflows)...)
	statements = append(statements, replaceCatalogInterfaceNodeStatements(catalog.InterfaceNodes, indexedAt)...)
	statements = append(statements, replaceCatalogInterfaceFieldStatements(catalog.InterfaceFields)...)
	statements = append(statements, replaceCatalogRequestTemplateStatements(catalog.RequestTemplates, indexedAt)...)
	statements = append(statements, replaceCatalogAPICaseStatements(catalog.APICases, indexedAt)...)
	statements = append(statements, replaceCatalogWorkflowBindingStatements(catalog.WorkflowBindings)...)
	statements = append(statements, replaceCatalogFixtureStatements(catalog.Fixtures, indexedAt)...)
	statements = append(statements, replaceCatalogCaseDependencyStatements(catalog.CaseDependencies)...)
	statements = append(statements, replaceCatalogTemplateConfigStatements(catalog.TemplateConfigs)...)
	return statements
}

func replaceProfileCatalogResetStatements(profileID, indexedAt string) []string {
	return []string{
		"delete from interface_node_case_dependency;",
		"delete from fixture_profile;",
		"delete from workflow_interface_node;",
		"delete from interface_node_case;",
		"delete from interface_node_request_template;",
		"delete from interface_node_field;",
		"delete from interface_node;",
		"delete from workflow_node;",
		"delete from workflow;",
		"delete from node_config;",
		"delete from template_config;",
		"delete from template;",
		"delete from kv;",
		fmt.Sprintf(`insert into kv (key, value, updated_at) values ('active_profile_id', %s, %s);`, sqlString(profileID), sqlString(indexedAt)),
	}
}

func replaceCatalogServiceStatements(services []store.CatalogService) []string {
	statements := make([]string, 0, len(services))
	for index, service := range services {
		statements = append(statements, fmt.Sprintf(`
insert into node_config (id, display_name, role, attached_template_ids, git_url, git_branch, repo_env, source_path, container_name, image, docker_service, service_port, management_port, memory_mb, cpu_milli, startup_command, health_url, log_path, status, sort_order)
values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %d, %d, %d, %d, %s, %s, %s, %s, %d);`,
			sqlString(service.ID), sqlString(service.DisplayName), sqlString(service.Kind), sqlString(jsonString(service.AttachedTemplateIDs, "[]")),
			sqlString(service.GitURL), sqlString(service.GitBranch), sqlString(service.RepoEnv), sqlString(service.SourcePath), sqlString(service.ContainerName),
			sqlString(service.Image), sqlString(service.DockerService), service.ServicePort, service.ManagementPort, service.MemoryMb, service.CPUMilli,
			sqlString(service.StartupCommand), sqlString(service.HealthURL), sqlString(service.LogPath), sqlString(stringDefault(service.Status, "active")),
			firstNonZero(service.SortOrder, index)))
	}
	return statements
}

func replaceCatalogWorkflowStatements(workflows []store.CatalogWorkflow) []string {
	statements := make([]string, 0, len(workflows)*3)
	for index, workflow := range workflows {
		templateID := "workflow/" + workflow.ID
		configID := templateID + "/config"
		statements = append(statements, fmt.Sprintf(`
insert into template (id, name, kind, status, sort_order)
values (%s, %s, 'workflow', 'active', %d);`, sqlString(templateID), sqlString(workflow.DisplayName), index))
		statements = append(statements, fmt.Sprintf(`
insert into template_config (id, template_id, workflow_id, title, description, config_json, status, sort_order)
values (%s, %s, %s, %s, %s, '{}', 'active', %d);`, sqlString(configID), sqlString(templateID), sqlString(workflow.ID), sqlString(workflow.DisplayName), sqlString(workflow.Description), index))
		statements = append(statements, fmt.Sprintf(`
insert into workflow (id, name, template_id, template_config_id, description, status, sort_order, base_step_timeout_ms, timeout_offset_ms)
values (%s, %s, %s, %s, %s, 'active', %d, %d, %d);`, sqlString(workflow.ID), sqlString(workflow.DisplayName), sqlString(templateID), sqlString(configID), sqlString(workflow.Description), index, workflow.BaseStepTimeoutMs, workflow.TimeoutOffsetMs))
	}
	return statements
}

func replaceCatalogInterfaceNodeStatements(nodes []store.CatalogInterfaceNode, indexedAt string) []string {
	statements := make([]string, 0, len(nodes))
	for index, node := range nodes {
		tagsJSON := jsonString(node.Tags, "[]")
		createdAt := stringDefault(node.CreatedAt, indexedAt)
		updatedAt := stringDefault(node.UpdatedAt, indexedAt)
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node (id, display_name, service_id, operation, method, path, template_id, version, status, tags_json, description, timeout_ms, sort_order, created_at, updated_at)
	values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %d, %d, %s, %s);`,
			sqlString(node.ID), sqlString(node.DisplayName), sqlString(node.ServiceID), sqlString(node.Operation), sqlString(node.Method), sqlString(node.Path),
			sqlString(node.TemplateID), sqlString(stringDefault(node.Version, "v1")), sqlString(stringDefault(node.Status, "active")), sqlString(tagsJSON),
			sqlString(node.Description), node.TimeoutMs, firstNonZero(node.SortOrder, index), sqlString(createdAt), sqlString(updatedAt)))
	}
	return statements
}

func replaceCatalogInterfaceFieldStatements(fields []store.CatalogInterfaceNodeField) []string {
	statements := make([]string, 0, len(fields))
	for index, field := range fields {
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node_field (id, node_id, direction, field_path, display_name, data_type, required, bindable, port_type, status, sort_order)
	values (%s, %s, %s, %s, %s, %s, %d, %d, %s, %s, %d);`,
			sqlString(field.ID), sqlString(field.NodeID), sqlString(field.Direction), sqlString(field.FieldPath), sqlString(field.DisplayName), sqlString(field.DataType),
			boolInt(field.Required), boolInt(field.Bindable), sqlString(stringDefault(field.PortType, "DATA")), sqlString(stringDefault(field.Status, "active")), firstNonZero(field.SortOrder, index)))
	}
	return statements
}

func replaceCatalogRequestTemplateStatements(templates []store.CatalogRequestTemplate, indexedAt string) []string {
	statements := make([]string, 0, len(templates)*3)
	for index, template := range templates {
		templateID := "request/" + template.ID
		configID := templateID + "/config"
		statements = append(statements, fmt.Sprintf(`
insert into template (id, name, kind, status, sort_order)
values (%s, %s, 'request', 'active', %d);`, sqlString(templateID), sqlString(template.DisplayName), index))
		statements = append(statements, fmt.Sprintf(`
insert into template_config (id, template_id, node_id, scope_type, scope_id, title, config_json, status, sort_order)
values (%s, %s, %s, 'interface_node', %s, %s, %s, 'active', %d);`, sqlString(configID), sqlString(templateID), sqlString(template.NodeID), sqlString(template.NodeID), sqlString(template.DisplayName), sqlString(stringDefault(template.TemplateJSON, "{}")), index))
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node_request_template (id, node_id, name, template_json, status, sort_order, created_at, updated_at)
	values (%s, %s, %s, %s, %s, %d, %s, %s);`,
			sqlString(template.ID), sqlString(template.NodeID), sqlString(template.DisplayName), sqlString(stringDefault(template.TemplateJSON, "{}")),
			sqlString(stringDefault(template.Status, "active")), firstNonZero(template.SortOrder, index), sqlString(indexedAt), sqlString(indexedAt)))
	}
	return statements
}

func replaceCatalogAPICaseStatements(cases []store.CatalogAPICase, indexedAt string) []string {
	statements := make([]string, 0, len(cases))
	for index, item := range cases {
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node_case (id, node_id, title, description, case_type, scenario, tags_json, priority, owner, payload_template_json, request_template_id, patch_json, render_mode, expected_json, required_for_admission, status, sort_order, created_at, updated_at, case_path, source_kind, source_path, executor_id, base_url, evidence_dir, timeout_seconds, default_overrides_json)
	values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %d, %s, %d, %s, %s, %s, %s, %s, %s, %s, %s, %d, %s);`,
			sqlString(item.ID), sqlString(item.NodeID), sqlString(item.DisplayName), sqlString(item.Description), sqlString(stringDefault(item.CaseType, "api")), sqlString(item.Scenario),
			sqlString(jsonString(item.Tags, "[]")), sqlString(item.Priority), sqlString(item.Owner),
			sqlString(stringDefault(item.PayloadTemplateJSON, "{}")), sqlString(item.RequestTemplateID), sqlString(stringDefault(item.PatchJSON, "[]")),
			sqlString(stringDefault(item.RenderMode, "legacy_payload")), sqlString(stringDefault(item.ExpectedJSON, "{}")), boolInt(item.RequiredForAdmission),
			sqlString(stringDefault(item.Status, "active")), firstNonZero(item.SortOrder, index), sqlString(indexedAt), sqlString(indexedAt),
			sqlString(item.CasePath), sqlString(item.SourceKind), sqlString(item.SourcePath), sqlString(item.ExecutorID),
			sqlString(item.BaseURL), sqlString(item.EvidenceDir), item.TimeoutSeconds, sqlString(stringDefault(item.DefaultOverridesJSON, "{}"))))
	}
	return statements
}

func replaceCatalogWorkflowBindingStatements(bindings []store.CatalogWorkflowBinding) []string {
	statements := make([]string, 0, len(bindings)*2)
	for index, binding := range bindings {
		statements = append(statements, fmt.Sprintf(`
insert into workflow_interface_node (workflow_id, step_id, node_id, case_id, required, sort_order)
values (%s, %s, %s, %s, %d, %d);`, sqlString(binding.WorkflowID), sqlString(binding.StepID), sqlString(binding.NodeID), sqlString(binding.CaseID), boolInt(binding.Required), firstNonZero(binding.SortOrder, index)))
		if binding.NodeID != "" {
			statements = append(statements, fmt.Sprintf(`
insert into workflow_node (workflow_id, node_id, required, sort_order)
values (%s, %s, %d, %d)
on conflict(workflow_id, node_id, relation_type) do nothing;`, sqlString(binding.WorkflowID), sqlString(binding.NodeID), boolInt(binding.Required), firstNonZero(binding.SortOrder, index)))
		}
	}
	return statements
}

func replaceCatalogFixtureStatements(fixtures []store.CatalogFixture, indexedAt string) []string {
	statements := make([]string, 0, len(fixtures))
	for index, fixture := range fixtures {
		statements = append(statements, fmt.Sprintf(`
insert into fixture_profile (id, name, source_type, source_workflow_id, source_until_step, ttl_seconds, status, description, sort_order, created_at, updated_at)
values (%s, %s, %s, %s, %s, %d, %s, %s, %d, %s, %s);`,
			sqlString(fixture.ID), sqlString(fixture.DisplayName), sqlString(fixture.Kind), sqlString(fixture.SourceWorkflowID),
			sqlString(fixture.SourceUntilStep), fixture.TTLSeconds, sqlString(stringDefault(fixture.Status, "active")),
			sqlString(fixture.DataJSON), firstNonZero(fixture.SortOrder, index), sqlString(indexedAt), sqlString(indexedAt)))
	}
	return statements
}

func replaceCatalogCaseDependencyStatements(dependencies []store.CatalogCaseDependency) []string {
	statements := make([]string, 0, len(dependencies))
	for index, dependency := range dependencies {
		statements = append(statements, fmt.Sprintf(`
	insert into interface_node_case_dependency (id, case_id, fixture_profile_id, required, mappings_json, status, sort_order)
	values (%s, %s, %s, %d, %s, %s, %d);`,
			sqlString(dependency.ID), sqlString(dependency.CaseID), sqlString(dependency.FixtureID), boolInt(dependency.Required),
			sqlString(stringDefault(dependency.MappingsJSON, "[]")), sqlString(stringDefault(dependency.Status, "active")), firstNonZero(dependency.SortOrder, index)))
	}
	return statements
}

func replaceCatalogTemplateConfigStatements(configs []store.CatalogTemplateConfig) []string {
	statements := make([]string, 0, len(configs)*2)
	for index, config := range configs {
		if strings.TrimSpace(config.ID) == "" {
			continue
		}
		templateID := stringDefault(config.TemplateID, "template-config/"+config.ID)
		statements = append(statements, fmt.Sprintf(`
insert into template (id, name, kind, status, sort_order)
values (%s, %s, %s, 'active', %d)
on conflict(id) do nothing;`, sqlString(templateID), sqlString(stringDefault(config.Title, templateID)), sqlString(stringDefault(config.ScopeType, "config")), firstNonZero(config.SortOrder, index)))
		statements = append(statements, fmt.Sprintf(`
insert or replace into template_config (id, template_id, node_id, workflow_id, scope_type, scope_id, title, description, config_json, status, sort_order)
values (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %d);`,
			sqlString(config.ID), sqlString(templateID), sqlString(config.NodeID), sqlString(config.WorkflowID), sqlString(config.ScopeType),
			sqlString(config.ScopeID), sqlString(config.Title), sqlString(config.Description), sqlString(stringDefault(config.ConfigJSON, "{}")),
			sqlString(stringDefault(config.Status, "active")), firstNonZero(config.SortOrder, index)))
	}
	return statements
}
