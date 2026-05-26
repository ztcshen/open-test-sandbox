package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"agent-testbench/internal/store"
)

func (s *Store) ReplaceProfileCatalog(ctx context.Context, catalog store.ProfileCatalog) error {
	if catalog.IndexedAt.IsZero() {
		catalog.IndexedAt = utcNow()
	}
	counts := catalogCounts(catalog)
	payload, err := json.Marshal(catalog)
	if err != nil {
		return fmt.Errorf("encode profile catalog %q: %w", catalog.ProfileID, err)
	}
	query := fmt.Sprintf(`
insert into profile_catalogs (
  profile_id, indexed_at, catalog_json, services, workflows, interface_nodes, api_cases,
  request_templates, workflow_bindings, case_dependencies, fixtures, templates, template_configs
)
values (%s)
%s;`, s.bindVars(13), s.dialect.UpsertClause("profile_id", []string{
		"indexed_at", "catalog_json", "services", "workflows", "interface_nodes", "api_cases",
		"request_templates", "workflow_bindings", "case_dependencies", "fixtures", "templates", "template_configs",
	}))
	if _, err := s.db.ExecContext(ctx, query,
		catalog.ProfileID, dbTimeArg(s.dialect, catalog.IndexedAt), string(payload), counts.Services, counts.Workflows, counts.InterfaceNodes,
		counts.APICases, counts.RequestTemplates, counts.WorkflowBindings, counts.CaseDependencies, counts.Fixtures, counts.Templates, counts.TemplateConfigs,
	); err != nil {
		return fmt.Errorf("replace profile catalog %q: %w", catalog.ProfileID, err)
	}
	return nil
}

func (s *Store) GetProfileCatalog(ctx context.Context) (store.ProfileCatalog, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `
select catalog_json
from profile_catalogs
order by indexed_at desc, profile_id desc
limit 1;`).Scan(&payload)
	if err != nil {
		if err == sql.ErrNoRows {
			return store.ProfileCatalog{}, store.ErrNotFound
		}
		return store.ProfileCatalog{}, err
	}
	var catalog store.ProfileCatalog
	if err := json.Unmarshal([]byte(payload), &catalog); err != nil {
		return store.ProfileCatalog{}, fmt.Errorf("decode profile catalog: %w", err)
	}
	return catalog, nil
}

func (s *Store) GetProfileCatalogIndex(ctx context.Context) (store.ProfileCatalogIndex, error) {
	row := s.db.QueryRowContext(ctx, `
select profile_id, indexed_at, services, workflows, interface_nodes, api_cases, request_templates,
  workflow_bindings, case_dependencies, fixtures, templates, template_configs
from profile_catalogs
order by indexed_at desc, profile_id desc
limit 1;`)
	index, err := scanProfileCatalogIndex(row)
	if err != nil {
		return store.ProfileCatalogIndex{}, err
	}
	return index, nil
}

func scanProfileCatalogIndex(row scanner) (store.ProfileCatalogIndex, error) {
	var r store.ProfileCatalogIndex
	var indexedAt any
	if err := row.Scan(
		&r.ProfileID, &indexedAt, &r.Counts.Services, &r.Counts.Workflows, &r.Counts.InterfaceNodes,
		&r.Counts.APICases, &r.Counts.RequestTemplates, &r.Counts.WorkflowBindings, &r.Counts.CaseDependencies,
		&r.Counts.Fixtures, &r.Counts.Templates, &r.Counts.TemplateConfigs,
	); err != nil {
		if err == sql.ErrNoRows {
			return store.ProfileCatalogIndex{}, store.ErrNotFound
		}
		return store.ProfileCatalogIndex{}, err
	}
	r.IndexedAt = decodeDBTime(indexedAt)
	return r, nil
}

func catalogCounts(catalog store.ProfileCatalog) store.ProfileCatalogCounts {
	return store.ProfileCatalogCounts{
		Services:         len(catalog.Services),
		Workflows:        len(catalog.Workflows),
		InterfaceNodes:   len(catalog.InterfaceNodes),
		APICases:         len(catalog.APICases),
		RequestTemplates: len(catalog.RequestTemplates),
		WorkflowBindings: len(catalog.WorkflowBindings),
		CaseDependencies: len(catalog.CaseDependencies),
		Fixtures:         len(catalog.Fixtures),
		Templates:        len(catalog.Workflows) + len(catalog.RequestTemplates) + len(catalog.TemplateConfigs),
		TemplateConfigs:  len(catalog.Workflows) + len(catalog.RequestTemplates) + len(catalog.TemplateConfigs),
	}
}
