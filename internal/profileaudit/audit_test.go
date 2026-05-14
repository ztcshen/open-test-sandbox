package profileaudit_test

import (
	"strings"
	"testing"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/profileaudit"
)

func TestAuditReportsBrokenProfileReferences(t *testing.T) {
	report, err := profileaudit.Audit(t.Context(), profileaudit.Options{
		Bundle: profile.Bundle{
			ID:          "sample",
			DisplayName: "Sample Profile",
			Workflows: []profile.Workflow{
				{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
			},
			InterfaceNodes: []profile.InterfaceNode{
				{ID: "node.alpha", DisplayName: "Node Alpha"},
			},
			APICases: []profile.APICase{
				{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.missing"},
				{ID: "case.beta", DisplayName: "Case Beta", NodeID: "node.alpha"},
			},
			RequestTemplates: []profile.RequestTemplate{
				{ID: "template.alpha", NodeID: "node.missing", Method: "POST", Path: "/v1/items"},
			},
			Fixtures: []profile.Fixture{
				{ID: "fixture.alpha", Kind: "json", DataJSON: `{"id":"item-001"}`},
			},
			CaseDependencies: []profile.CaseDependency{
				{ID: "dependency.alpha", CaseID: "case.missing", FixtureID: "fixture.alpha"},
				{ID: "dependency.beta", CaseID: "case.beta", FixtureID: "fixture.missing"},
			},
			WorkflowBindings: []profile.WorkflowBinding{
				{WorkflowID: "workflow.missing", StepID: "step.one", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
				{WorkflowID: "workflow.alpha", StepID: "step.two", NodeID: "node.missing", CaseID: "case.missing", Required: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("audit profile: %v", err)
	}
	if report.OK {
		t.Fatalf("report should not be ok: %#v", report)
	}
	for _, want := range []string{
		"api-case-node-missing:apiCase:case.alpha:nodeId",
		"request-template-node-missing:requestTemplate:template.alpha:nodeId",
		"case-dependency-case-missing:caseDependency:dependency.alpha:caseId",
		"case-dependency-fixture-missing:caseDependency:dependency.beta:fixtureId",
		"workflow-binding-workflow-missing:workflowBinding:workflow.missing/step.one:workflowId",
		"workflow-binding-node-missing:workflowBinding:workflow.alpha/step.two:nodeId",
		"workflow-binding-case-missing:workflowBinding:workflow.alpha/step.two:caseId",
	} {
		if !hasIssue(report, want) {
			t.Fatalf("missing issue %q in %#v", want, report.Issues)
		}
	}
}

func hasIssue(report profileaudit.Report, key string) bool {
	for _, issue := range report.Issues {
		got := strings.Join([]string{issue.Code, issue.SubjectType, issue.SubjectID, issue.Field}, ":")
		if got == key {
			return true
		}
	}
	return false
}
