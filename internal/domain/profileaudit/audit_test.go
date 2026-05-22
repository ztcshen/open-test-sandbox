package profileaudit_test

import (
	"strings"
	"testing"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/domain/profileaudit"
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

func TestRepairPlanBuildsActionableStepsFromAuditIssues(t *testing.T) {
	audit, err := profileaudit.Audit(t.Context(), profileaudit.Options{
		Bundle: profile.Bundle{
			ID:          "sample",
			DisplayName: "Sample Profile",
			Workflows:   []profile.Workflow{{ID: "workflow.alpha", DisplayName: "Workflow Alpha"}},
			InterfaceNodes: []profile.InterfaceNode{
				{ID: "node.alpha", DisplayName: "Node Alpha"},
			},
			APICases: []profile.APICase{
				{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.missing"},
			},
			CaseDependencies: []profile.CaseDependency{
				{ID: "dependency.alpha", CaseID: "case.alpha", FixtureID: ""},
			},
			WorkflowBindings: []profile.WorkflowBinding{
				{WorkflowID: "workflow.alpha", StepID: "", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
			},
			Fixtures: []profile.Fixture{
				{ID: "fixture.bad", Kind: "json", DataJSON: `{"broken":`},
			},
		},
	})
	if err != nil {
		t.Fatalf("audit profile: %v", err)
	}

	plan := profileaudit.RepairPlan(audit)

	if !plan.OK || plan.ProfileID != "sample" || plan.IssueCount != 4 || plan.ActionCount != 4 {
		t.Fatalf("repair plan summary = %#v", plan)
	}
	if plan.Counts.UpdateReferenceOrAddAsset != 1 || plan.Counts.FillRequiredField != 2 || plan.Counts.FixInvalidJSON != 1 {
		t.Fatalf("repair plan counts = %#v", plan.Counts)
	}
	wantTypes := []string{"update-reference-or-add-asset", "fill-required-field", "fill-required-field", "fix-invalid-json"}
	for i, want := range wantTypes {
		if plan.Actions[i].Type != want {
			t.Fatalf("action %d type = %q, want %q; actions=%#v", i, plan.Actions[i].Type, want, plan.Actions)
		}
	}
	first := plan.Actions[0]
	if first.IssueCode != "api-case-node-missing" || first.SubjectType != "apiCase" || first.SubjectID != "case.alpha" || first.Field != "nodeId" {
		t.Fatalf("first repair action = %#v", first)
	}
	if !strings.Contains(first.SuggestedChange, "Create the missing interface node") || !strings.Contains(first.SuggestedChange, "nodeId") {
		t.Fatalf("first suggested change = %q", first.SuggestedChange)
	}
	if len(first.Command) == 0 || strings.Join(first.Command, " ") != "profile audit --json" {
		t.Fatalf("first command = %#v", first.Command)
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
