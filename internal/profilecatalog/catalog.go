package profilecatalog

import (
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func FromBundle(bundle profile.Bundle, indexedAt time.Time) store.ProfileCatalog {
	catalog := store.ProfileCatalog{
		ProfileID: bundle.ID,
		IndexedAt: indexedAt,
	}
	for _, service := range bundle.Services {
		catalog.Services = append(catalog.Services, store.CatalogService{
			ID:          service.ID,
			DisplayName: service.DisplayName,
			Kind:        service.Kind,
		})
	}
	for _, workflow := range bundle.Workflows {
		catalog.Workflows = append(catalog.Workflows, store.CatalogWorkflow{
			ID:          workflow.ID,
			DisplayName: workflow.DisplayName,
			Description: workflow.Description,
		})
	}
	for _, node := range bundle.InterfaceNodes {
		catalog.InterfaceNodes = append(catalog.InterfaceNodes, store.CatalogInterfaceNode{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
		})
	}
	for _, item := range bundle.APICases {
		catalog.APICases = append(catalog.APICases, store.CatalogAPICase{
			ID:          item.ID,
			DisplayName: item.DisplayName,
			NodeID:      item.NodeID,
		})
	}
	for _, template := range bundle.RequestTemplates {
		catalog.RequestTemplates = append(catalog.RequestTemplates, store.CatalogRequestTemplate{
			ID:           template.ID,
			DisplayName:  template.DisplayName,
			NodeID:       template.NodeID,
			Method:       template.Method,
			Path:         template.Path,
			TemplateJSON: template.TemplateJSON,
		})
	}
	for _, binding := range bundle.WorkflowBindings {
		catalog.WorkflowBindings = append(catalog.WorkflowBindings, store.CatalogWorkflowBinding{
			WorkflowID: binding.WorkflowID,
			StepID:     binding.StepID,
			NodeID:     binding.NodeID,
			CaseID:     binding.CaseID,
			Required:   binding.Required,
		})
	}
	for _, dependency := range bundle.CaseDependencies {
		catalog.CaseDependencies = append(catalog.CaseDependencies, store.CatalogCaseDependency{
			ID:           dependency.ID,
			CaseID:       dependency.CaseID,
			FixtureID:    dependency.FixtureID,
			MappingsJSON: dependency.MappingsJSON,
		})
	}
	for _, fixture := range bundle.Fixtures {
		catalog.Fixtures = append(catalog.Fixtures, store.CatalogFixture{
			ID:          fixture.ID,
			DisplayName: fixture.DisplayName,
			Kind:        fixture.Kind,
			DataJSON:    fixture.DataJSON,
		})
	}
	return catalog
}
