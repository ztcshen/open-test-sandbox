package profileaudit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

type Options struct {
	Bundle     profile.Bundle
	BundlePath string
	Store      store.Store
}

type Report struct {
	OK          bool         `json:"ok"`
	ProfileID   string       `json:"profileId"`
	DisplayName string       `json:"displayName"`
	Counts      AssetCounts  `json:"counts"`
	IssueCount  int          `json:"issueCount"`
	Issues      []Issue      `json:"issues"`
	Store       *StoreReport `json:"store,omitempty"`
}

type AssetCounts struct {
	Services         int `json:"services"`
	Workflows        int `json:"workflows"`
	InterfaceNodes   int `json:"interfaceNodes"`
	APICases         int `json:"apiCases"`
	RequestTemplates int `json:"requestTemplates"`
	CaseDependencies int `json:"caseDependencies"`
	WorkflowBindings int `json:"workflowBindings"`
	Fixtures         int `json:"fixtures"`
}

type Issue struct {
	Severity    string `json:"severity"`
	Code        string `json:"code"`
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
	Field       string `json:"field"`
	Message     string `json:"message"`
}

type StoreReport struct {
	ProfileIndexed bool              `json:"profileIndexed"`
	BundleDigest   string            `json:"bundleDigest,omitempty"`
	IndexedDigest  string            `json:"indexedDigest,omitempty"`
	DigestMatches  bool              `json:"digestMatches"`
	APICases       []APICaseRunState `json:"apiCases"`
}

type RepairPlanReport struct {
	OK          bool             `json:"ok"`
	ProfileID   string           `json:"profileId"`
	DisplayName string           `json:"displayName"`
	IssueCount  int              `json:"issueCount"`
	ActionCount int              `json:"actionCount"`
	Counts      RepairPlanCounts `json:"counts"`
	Actions     []RepairAction   `json:"actions"`
	Audit       Report           `json:"audit"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type RepairPlanCounts struct {
	Total                     int `json:"total"`
	UpdateReferenceOrAddAsset int `json:"updateReferenceOrAddAsset"`
	FillRequiredField         int `json:"fillRequiredField"`
	FixInvalidJSON            int `json:"fixInvalidJson"`
	RenameDuplicateID         int `json:"renameDuplicateId"`
	Review                    int `json:"review"`
}

type RepairAction struct {
	Type            string   `json:"type"`
	IssueCode       string   `json:"issueCode"`
	Severity        string   `json:"severity"`
	SubjectType     string   `json:"subjectType"`
	SubjectID       string   `json:"subjectId"`
	Field           string   `json:"field"`
	Message         string   `json:"message"`
	SuggestedChange string   `json:"suggestedChange"`
	Command         []string `json:"command,omitempty"`
}

type APICaseRunState struct {
	CaseID       string `json:"caseId"`
	HasPassed    bool   `json:"hasPassed"`
	LatestStatus string `json:"latestStatus,omitempty"`
}

func FailureSummary(report Report) string {
	if report.OK {
		return "ok"
	}
	if len(report.Issues) == 0 {
		return "profile audit failed"
	}
	first := report.Issues[0]
	return first.Code + " " + first.SubjectType + " " + first.SubjectID + ": " + first.Message
}

func Audit(ctx context.Context, options Options) (Report, error) {
	report := Report{
		OK:          true,
		ProfileID:   options.Bundle.ID,
		DisplayName: options.Bundle.DisplayName,
		Counts:      counts(options.Bundle),
		Issues:      []Issue{},
	}

	auditor := referenceAuditor{
		workflows:        idSetFrom(options.Bundle.Workflows, func(item profile.Workflow) string { return item.ID }),
		nodes:            idSetFrom(options.Bundle.InterfaceNodes, func(item profile.InterfaceNode) string { return item.ID }),
		apiCases:         idSetFrom(options.Bundle.APICases, func(item profile.APICase) string { return item.ID }),
		requestTemplates: idSetFrom(options.Bundle.RequestTemplates, func(item profile.RequestTemplate) string { return item.ID }),
		fixtures:         idSetFrom(options.Bundle.Fixtures, func(item profile.Fixture) string { return item.ID }),
	}
	report.Issues = append(report.Issues, auditor.issues(options.Bundle)...)
	if options.Store != nil {
		storeReport, err := auditStore(ctx, options.Bundle, options.BundlePath, options.Store)
		if err != nil {
			return Report{}, err
		}
		report.Store = &storeReport
	}
	report.IssueCount = len(report.Issues)
	report.OK = report.IssueCount == 0
	return report, nil
}

func RepairPlan(audit Report) RepairPlanReport {
	report := RepairPlanReport{
		OK:          true,
		ProfileID:   audit.ProfileID,
		DisplayName: audit.DisplayName,
		IssueCount:  audit.IssueCount,
		Actions:     []RepairAction{},
		Audit:       audit,
	}
	for _, item := range audit.Issues {
		action := repairActionForIssue(item)
		report.Actions = append(report.Actions, action)
		report.Counts.Total++
		switch action.Type {
		case "update-reference-or-add-asset":
			report.Counts.UpdateReferenceOrAddAsset++
		case "fill-required-field":
			report.Counts.FillRequiredField++
		case "fix-invalid-json":
			report.Counts.FixInvalidJSON++
		case "rename-duplicate-id":
			report.Counts.RenameDuplicateID++
		default:
			report.Counts.Review++
		}
	}
	report.ActionCount = len(report.Actions)
	if audit.OK {
		report.Warnings = append(report.Warnings, "profile audit is already clean")
	}
	return report
}

func repairActionForIssue(item Issue) RepairAction {
	actionType := "review"
	suggested := "Review " + item.SubjectType + " " + item.SubjectID + " field " + item.Field + " and update the external profile bundle."
	switch {
	case strings.HasSuffix(item.Code, "-missing"):
		actionType = "update-reference-or-add-asset"
		suggested = missingReferenceSuggestion(item)
	case strings.HasSuffix(item.Code, "-required"):
		actionType = "fill-required-field"
		suggested = "Set " + item.SubjectType + " " + item.SubjectID + " field " + item.Field + " in the external profile bundle."
	case strings.HasSuffix(item.Code, "-id-duplicate"):
		actionType = "rename-duplicate-id"
		suggested = "Rename duplicate " + item.SubjectType + " id " + item.SubjectID + " so it is unique within the profile section."
	case item.Code == "fixture-data-json-invalid":
		actionType = "fix-invalid-json"
		suggested = "Replace fixture " + item.SubjectID + " field " + item.Field + " with valid JSON."
	}
	return RepairAction{
		Type:            actionType,
		IssueCode:       item.Code,
		Severity:        item.Severity,
		SubjectType:     item.SubjectType,
		SubjectID:       item.SubjectID,
		Field:           item.Field,
		Message:         item.Message,
		SuggestedChange: suggested,
		Command:         []string{"profile", "audit", "--json"},
	}
}

func missingReferenceSuggestion(item Issue) string {
	target := referenceTargetName(item)
	if target == "" {
		target = "asset"
	}
	return "Create the missing " + target + " or update " + item.SubjectType + " " + item.SubjectID + " field " + item.Field + " to an existing id."
}

func referenceTargetName(item Issue) string {
	switch item.Field {
	case "nodeId":
		return "interface node"
	case "caseId":
		return "API case"
	case "fixtureId":
		return "fixture"
	case "workflowId":
		return "workflow"
	default:
		return strings.TrimSuffix(strings.TrimPrefix(item.Code, item.SubjectType+"-"), "-missing")
	}
}

type referenceAuditor struct {
	workflows        map[string]bool
	nodes            map[string]bool
	apiCases         map[string]bool
	requestTemplates map[string]bool
	fixtures         map[string]bool
}

func (a referenceAuditor) issues(bundle profile.Bundle) []Issue {
	var issues []Issue
	issues = append(issues, duplicateIDIssues("workflow", bundle.Workflows, func(item profile.Workflow) string { return item.ID })...)
	issues = append(issues, duplicateIDIssues("apiCase", bundle.APICases, func(item profile.APICase) string { return item.ID })...)
	issues = append(issues, duplicateIDIssues("requestTemplate", bundle.RequestTemplates, func(item profile.RequestTemplate) string { return item.ID })...)
	issues = append(issues, duplicateIDIssues("fixture", bundle.Fixtures, func(item profile.Fixture) string { return item.ID })...)

	for _, item := range bundle.APICases {
		if strings.TrimSpace(item.NodeID) != "" && !a.nodes[item.NodeID] {
			issues = append(issues, issue("api-case-node-missing", "apiCase", subjectID(item.ID), "nodeId", "API Case references a missing interface node"))
		}
	}
	for _, item := range bundle.RequestTemplates {
		if strings.TrimSpace(item.NodeID) != "" && !a.nodes[item.NodeID] {
			issues = append(issues, issue("request-template-node-missing", "requestTemplate", subjectID(item.ID), "nodeId", "Request template references a missing interface node"))
		}
	}
	for _, item := range bundle.CaseDependencies {
		if strings.TrimSpace(item.CaseID) == "" {
			issues = append(issues, issue("case-dependency-case-required", "caseDependency", subjectID(item.ID), "caseId", "Case dependency must reference an API Case"))
		} else if !a.apiCases[item.CaseID] {
			issues = append(issues, issue("case-dependency-case-missing", "caseDependency", subjectID(item.ID), "caseId", "Case dependency references a missing API Case"))
		}
		if strings.TrimSpace(item.FixtureID) == "" {
			issues = append(issues, issue("case-dependency-fixture-required", "caseDependency", subjectID(item.ID), "fixtureId", "Case dependency must reference a fixture"))
		} else if !a.fixtures[item.FixtureID] {
			issues = append(issues, issue("case-dependency-fixture-missing", "caseDependency", subjectID(item.ID), "fixtureId", "Case dependency references a missing fixture"))
		}
	}
	for _, item := range bundle.WorkflowBindings {
		subject := workflowBindingSubject(item)
		if strings.TrimSpace(item.WorkflowID) == "" {
			issues = append(issues, issue("workflow-binding-workflow-required", "workflowBinding", subject, "workflowId", "Workflow binding must reference a workflow"))
		} else if !a.workflows[item.WorkflowID] {
			issues = append(issues, issue("workflow-binding-workflow-missing", "workflowBinding", subject, "workflowId", "Workflow binding references a missing workflow"))
		}
		if strings.TrimSpace(item.StepID) == "" {
			issues = append(issues, issue("workflow-binding-step-required", "workflowBinding", subject, "stepId", "Workflow binding must include a step id"))
		}
		if strings.TrimSpace(item.NodeID) != "" && !a.nodes[item.NodeID] {
			issues = append(issues, issue("workflow-binding-node-missing", "workflowBinding", subject, "nodeId", "Workflow binding references a missing interface node"))
		}
		if strings.TrimSpace(item.CaseID) != "" && !a.apiCases[item.CaseID] {
			issues = append(issues, issue("workflow-binding-case-missing", "workflowBinding", subject, "caseId", "Workflow binding references a missing API Case"))
		}
	}
	for _, item := range bundle.Fixtures {
		if strings.EqualFold(strings.TrimSpace(item.Kind), "json") && strings.TrimSpace(item.DataJSON) != "" && !json.Valid([]byte(item.DataJSON)) {
			issues = append(issues, issue("fixture-data-json-invalid", "fixture", subjectID(item.ID), "dataJson", "Fixture dataJson must be valid JSON"))
		}
	}
	return issues
}

func auditStore(ctx context.Context, bundle profile.Bundle, bundlePath string, s store.Store) (StoreReport, error) {
	report := StoreReport{APICases: make([]APICaseRunState, 0, len(bundle.APICases))}
	if strings.TrimSpace(bundlePath) != "" {
		digest, err := profile.BundleDigest(bundlePath)
		if err != nil {
			return StoreReport{}, err
		}
		report.BundleDigest = digest
	}
	index, err := s.GetProfileIndex(ctx, bundle.ID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return StoreReport{}, err
		}
	} else {
		report.ProfileIndexed = true
		report.IndexedDigest = index.BundleDigest
		report.DigestMatches = report.BundleDigest != "" && report.BundleDigest == index.BundleDigest
	}

	hasPassed, latest, err := apiCaseRunStatusByCase(ctx, bundle.ID, s)
	if err != nil {
		return StoreReport{}, err
	}
	for _, item := range bundle.APICases {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		report.APICases = append(report.APICases, APICaseRunState{
			CaseID:       item.ID,
			HasPassed:    hasPassed[item.ID],
			LatestStatus: latest[item.ID],
		})
	}
	return report, nil
}

func apiCaseRunStatusByCase(ctx context.Context, profileID string, s store.Store) (map[string]bool, map[string]string, error) {
	runs, err := s.ListRuns(ctx)
	if err != nil {
		return nil, nil, err
	}
	passed := map[string]bool{}
	latest := map[string]string{}
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].ProfileID != profileID {
			continue
		}
		caseRuns, err := s.ListAPICaseRuns(ctx, runs[i].ID)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range caseRuns {
			if latest[item.CaseID] == "" {
				latest[item.CaseID] = item.Status
			}
			if strings.EqualFold(item.Status, store.StatusPassed) {
				passed[item.CaseID] = true
			}
		}
	}
	return passed, latest, nil
}

func counts(bundle profile.Bundle) AssetCounts {
	raw := bundle.Counts()
	return AssetCounts{
		Services:         raw.Services,
		Workflows:        raw.Workflows,
		InterfaceNodes:   raw.InterfaceNodes,
		APICases:         raw.APICases,
		RequestTemplates: raw.RequestTemplates,
		CaseDependencies: raw.CaseDependencies,
		WorkflowBindings: raw.WorkflowBindings,
		Fixtures:         raw.Fixtures,
	}
}

func idSetFrom[T any](items []T, id func(T) string) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		value := strings.TrimSpace(id(item))
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func duplicateIDIssues[T any](subjectType string, items []T, id func(T) string) []Issue {
	seen := map[string]bool{}
	var issues []Issue
	for _, item := range items {
		value := strings.TrimSpace(id(item))
		if value == "" {
			issues = append(issues, issue(subjectType+"-id-required", subjectType, "(missing)", "id", "Asset id is required"))
			continue
		}
		if seen[value] {
			issues = append(issues, issue(subjectType+"-id-duplicate", subjectType, value, "id", "Asset id must be unique within this profile section"))
		}
		seen[value] = true
	}
	return issues
}

func issue(code string, subjectType string, subjectID string, field string, message string) Issue {
	return Issue{
		Severity:    "error",
		Code:        code,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Field:       field,
		Message:     message,
	}
}

func subjectID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(missing)"
	}
	return value
}

func workflowBindingSubject(item profile.WorkflowBinding) string {
	workflowID := subjectID(item.WorkflowID)
	stepID := subjectID(item.StepID)
	return workflowID + "/" + stepID
}
