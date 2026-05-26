package store_test

import (
	"time"

	"agent-testbench/internal/store"
)

func contractProfileCatalog(started time.Time) store.ProfileCatalog {
	return store.ProfileCatalog{
		ProfileID:        contractProfileID,
		IndexedAt:        started.Add(3 * time.Minute),
		Services:         contractCatalogServices(),
		Workflows:        contractCatalogWorkflows(),
		InterfaceNodes:   contractCatalogInterfaceNodes(),
		APICases:         contractCatalogAPICases(),
		RequestTemplates: contractCatalogRequestTemplates(),
		WorkflowBindings: contractCatalogWorkflowBindings(),
		CaseDependencies: contractCatalogCaseDependencies(),
		Fixtures:         contractCatalogFixtures(),
	}
}

func contractCatalogServices() []store.CatalogService {
	return []store.CatalogService{{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http", SourcePath: "/tmp/source/service.alpha"}}
}

func contractCatalogWorkflows() []store.CatalogWorkflow {
	return []store.CatalogWorkflow{{ID: "workflow.alpha", DisplayName: "Workflow Alpha"}}
}

func contractCatalogInterfaceNodes() []store.CatalogInterfaceNode {
	return []store.CatalogInterfaceNode{{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"}}
}

func contractCatalogAPICases() []store.CatalogAPICase {
	return []store.CatalogAPICase{{
		ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", CasePath: "cases/case.alpha.json",
		SourceKind: "karate", SourcePath: "tests/api.feature", ExecutorID: "executor.karate",
		BaseURL: "http://127.0.0.1:18080", EvidenceDir: ".runtime/cases", TimeoutSeconds: 12,
		DefaultOverridesJSON: `{"itemId":"item-001"}`,
	}}
}

func contractCatalogRequestTemplates() []store.CatalogRequestTemplate {
	return []store.CatalogRequestTemplate{{ID: "template.alpha", DisplayName: "Template Alpha", NodeID: "node.alpha", TemplateJSON: `{"method":"GET"}`}}
}

func contractCatalogWorkflowBindings() []store.CatalogWorkflowBinding {
	return []store.CatalogWorkflowBinding{{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true}}
}

func contractCatalogCaseDependencies() []store.CatalogCaseDependency {
	return []store.CatalogCaseDependency{{ID: "dependency.alpha", CaseID: "case.alpha", FixtureID: "fixture.alpha", MappingsJSON: `[]`}}
}

func contractCatalogFixtures() []store.CatalogFixture {
	return []store.CatalogFixture{{ID: "fixture.alpha", DisplayName: "Fixture Alpha", Kind: "json", DataJSON: `{}`}}
}
