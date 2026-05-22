package casesuite

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	"agent-testbench/internal/domain/profile"
)

type QualityCounts struct {
	Nodes                  int `json:"nodes"`
	NodesWithoutCases      int `json:"nodesWithoutCases"`
	Cases                  int `json:"cases"`
	CompleteCases          int `json:"completeCases"`
	IncompleteCases        int `json:"incompleteCases"`
	MissingDescription     int `json:"missingDescription"`
	MissingTags            int `json:"missingTags"`
	MissingPriority        int `json:"missingPriority"`
	MissingOwner           int `json:"missingOwner"`
	MissingRunnable        int `json:"missingRunnable"`
	MissingExecution       int `json:"missingExecution"`
	Inactive               int `json:"inactive"`
	NonExecutableLifecycle int `json:"nonExecutableLifecycle"`
	InvalidStatus          int `json:"invalidStatus"`
}

type QualityCase struct {
	CaseID             string   `json:"caseId"`
	Title              string   `json:"title"`
	NodeID             string   `json:"nodeId,omitempty"`
	NodeName           string   `json:"nodeName,omitempty"`
	Status             string   `json:"status"`
	Lifecycle          string   `json:"lifecycle"`
	Tags               []string `json:"tags,omitempty"`
	Priority           string   `json:"priority,omitempty"`
	Owner              string   `json:"owner,omitempty"`
	HasDescription     bool     `json:"hasDescription"`
	HasRunnableFile    bool     `json:"hasRunnableFile"`
	HasExecutionConfig bool     `json:"hasExecutionConfig"`
	Complete           bool     `json:"complete"`
	Issues             []string `json:"issues,omitempty"`
}

type QualityNode struct {
	NodeID      string   `json:"nodeId"`
	DisplayName string   `json:"displayName,omitempty"`
	ServiceID   string   `json:"serviceId,omitempty"`
	Operation   string   `json:"operation,omitempty"`
	Method      string   `json:"method,omitempty"`
	Path        string   `json:"path,omitempty"`
	CaseCount   int      `json:"caseCount"`
	Issues      []string `json:"issues,omitempty"`
}

type QualityReport struct {
	OK          bool          `json:"ok"`
	ProfileID   string        `json:"profileId"`
	GeneratedAt string        `json:"generatedAt"`
	Filters     Filter        `json:"filters"`
	Counts      QualityCounts `json:"counts"`
	Cases       []QualityCase `json:"cases"`
	Nodes       []QualityNode `json:"nodes"`
	Warnings    []string      `json:"warnings,omitempty"`
}

type QualityPlanCounts struct {
	Total            int `json:"total"`
	DraftCase        int `json:"draftCase"`
	CompleteMetadata int `json:"completeMetadata"`
	AddRunnable      int `json:"addRunnable"`
	AddExecution     int `json:"addExecution"`
	ReviewLifecycle  int `json:"reviewLifecycle"`
}

type QualityPlanAction struct {
	Type            string   `json:"type"`
	NodeID          string   `json:"nodeId,omitempty"`
	NodeName        string   `json:"nodeName,omitempty"`
	CaseID          string   `json:"caseId,omitempty"`
	CaseTitle       string   `json:"caseTitle,omitempty"`
	SuggestedCaseID string   `json:"suggestedCaseId,omitempty"`
	Fields          []string `json:"fields,omitempty"`
	Issues          []string `json:"issues,omitempty"`
	Command         []string `json:"command,omitempty"`
	Reason          string   `json:"reason,omitempty"`
}

type QualityPlanReport struct {
	OK          bool                `json:"ok"`
	ProfileID   string              `json:"profileId"`
	GeneratedAt string              `json:"generatedAt"`
	Filters     Filter              `json:"filters"`
	Counts      QualityPlanCounts   `json:"counts"`
	Actions     []QualityPlanAction `json:"actions"`
	Quality     QualityReport       `json:"quality"`
	Warnings    []string            `json:"warnings,omitempty"`
}

func Quality(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, cases []profile.APICase) (QualityReport, error) {
	filter = NormalizeFilter(filter)
	configs := ExecutionConfigSet(ctx, bundle, runtime)
	executorRefs := executorReferenceSet(bundle)
	nodesByID := map[string]profile.InterfaceNode{}
	for _, node := range bundle.InterfaceNodes {
		nodesByID[node.ID] = node
	}
	casesByNode := map[string]int{}
	for _, item := range cases {
		if strings.TrimSpace(item.NodeID) != "" {
			casesByNode[item.NodeID]++
		}
	}
	report := QualityReport{
		OK:          true,
		ProfileID:   bundle.ID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Filters:     filter,
		Cases:       []QualityCase{},
		Nodes:       []QualityNode{},
	}
	for _, item := range cases {
		node := nodesByID[item.NodeID]
		row := QualityCase{
			CaseID:             item.ID,
			Title:              firstNonEmpty(item.DisplayName, item.ID),
			NodeID:             item.NodeID,
			NodeName:           firstNonEmpty(node.DisplayName, item.NodeID),
			Status:             CaseStatus(item),
			Lifecycle:          CaseStatus(item),
			Tags:               append([]string(nil), item.Tags...),
			Priority:           item.Priority,
			Owner:              item.Owner,
			HasDescription:     strings.TrimSpace(item.Description) != "",
			HasRunnableFile:    hasRunnableSource(item),
			HasExecutionConfig: configs[item.ID] || hasUsableExecutorReference(item, executorRefs),
		}
		if hasExternalSource(item) && !hasUsableExecutorReference(item, executorRefs) {
			row.Issues = append(row.Issues, "missing-executor")
		}
		if row.Lifecycle == CaseLifecycleInvalid {
			row.Issues = append(row.Issues, "invalid-status")
			report.Counts.InvalidStatus++
		}
		if !IsExecutableCaseLifecycle(row.Lifecycle) {
			row.Issues = append(row.Issues, "inactive")
			row.Issues = append(row.Issues, "non-executable-lifecycle")
			report.Counts.Inactive++
			report.Counts.NonExecutableLifecycle++
		}
		if !row.HasDescription {
			row.Issues = append(row.Issues, "missing-description")
			report.Counts.MissingDescription++
		}
		if len(row.Tags) == 0 {
			row.Issues = append(row.Issues, "missing-tags")
			report.Counts.MissingTags++
		}
		if strings.TrimSpace(row.Priority) == "" {
			row.Issues = append(row.Issues, "missing-priority")
			report.Counts.MissingPriority++
		}
		if strings.TrimSpace(row.Owner) == "" {
			row.Issues = append(row.Issues, "missing-owner")
			report.Counts.MissingOwner++
		}
		if !row.HasRunnableFile {
			row.Issues = append(row.Issues, "missing-runnable-source")
			report.Counts.MissingRunnable++
		}
		if !row.HasExecutionConfig {
			row.Issues = append(row.Issues, "missing-execution-config")
			report.Counts.MissingExecution++
		}
		row.Complete = len(row.Issues) == 0
		if row.Complete {
			report.Counts.CompleteCases++
		} else {
			report.Counts.IncompleteCases++
		}
		report.Cases = append(report.Cases, row)
	}
	for _, node := range bundle.InterfaceNodes {
		if !qualityNodeMatchesFilter(node, filter) {
			continue
		}
		report.Counts.Nodes++
		caseCount := casesByNode[node.ID]
		if caseCount > 0 {
			continue
		}
		report.Counts.NodesWithoutCases++
		report.Nodes = append(report.Nodes, QualityNode{
			NodeID:      node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
			Operation:   node.Operation,
			Method:      node.Method,
			Path:        node.Path,
			CaseCount:   0,
			Issues:      []string{"no-maintained-cases"},
		})
	}
	report.Counts.Cases = len(report.Cases)
	report.OK = report.Counts.IncompleteCases == 0 && report.Counts.NodesWithoutCases == 0
	if report.Counts.Cases == 0 {
		report.Warnings = append(report.Warnings, "no cases matched selector")
	}
	return report, nil
}

func QualityPlan(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, cases []profile.APICase) (QualityPlanReport, error) {
	quality, err := Quality(ctx, bundle, runtime, filter, cases)
	if err != nil {
		return QualityPlanReport{}, err
	}
	report := QualityPlanReport{
		OK:          true,
		ProfileID:   bundle.ID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Filters:     quality.Filters,
		Actions:     []QualityPlanAction{},
		Quality:     quality,
		Warnings:    append([]string(nil), quality.Warnings...),
	}
	for _, node := range quality.Nodes {
		action := QualityPlanAction{
			Type:            "draft-case",
			NodeID:          node.NodeID,
			NodeName:        node.DisplayName,
			SuggestedCaseID: suggestedCaseID(node.NodeID),
			Issues:          append([]string(nil), node.Issues...),
			Reason:          "interface node has no maintained cases",
		}
		action.Command = []string{"interface-node", "case", "draft", "--node", node.NodeID, "--case-id", action.SuggestedCaseID, "--title", firstNonEmpty(node.DisplayName, node.NodeID) + " Default Case"}
		report.Actions = append(report.Actions, action)
		report.Counts.DraftCase++
	}
	for _, item := range quality.Cases {
		if item.Complete {
			continue
		}
		if lifecycleIssues := caseLifecycleIssues(item.Issues); len(lifecycleIssues) > 0 {
			report.Actions = append(report.Actions, QualityPlanAction{
				Type:      "review-case-lifecycle",
				CaseID:    item.CaseID,
				CaseTitle: item.Title,
				NodeID:    item.NodeID,
				Issues:    lifecycleIssues,
				Reason:    "case lifecycle status is not executable",
			})
			report.Counts.ReviewLifecycle++
		}
		fields := missingMetadataFields(item)
		if len(fields) > 0 {
			report.Actions = append(report.Actions, QualityPlanAction{
				Type:      "complete-case-metadata",
				CaseID:    item.CaseID,
				CaseTitle: item.Title,
				NodeID:    item.NodeID,
				Fields:    fields,
				Issues:    metadataIssues(item.Issues),
				Reason:    "case metadata is incomplete",
			})
			report.Counts.CompleteMetadata++
		}
		if !item.HasRunnableFile {
			report.Actions = append(report.Actions, QualityPlanAction{
				Type:      "add-runnable-source",
				CaseID:    item.CaseID,
				CaseTitle: item.Title,
				NodeID:    item.NodeID,
				Issues:    []string{"missing-runnable-source"},
				Reason:    "case has no runnable API case file",
			})
			report.Counts.AddRunnable++
		}
		if !item.HasExecutionConfig {
			report.Actions = append(report.Actions, QualityPlanAction{
				Type:      "add-execution-config",
				CaseID:    item.CaseID,
				CaseTitle: item.Title,
				NodeID:    item.NodeID,
				Issues:    []string{"missing-execution-config"},
				Reason:    "case has no execution config",
			})
			report.Counts.AddExecution++
		}
	}
	sort.SliceStable(report.Actions, func(i, j int) bool {
		if actionRank(report.Actions[i].Type) != actionRank(report.Actions[j].Type) {
			return actionRank(report.Actions[i].Type) < actionRank(report.Actions[j].Type)
		}
		if report.Actions[i].NodeID != report.Actions[j].NodeID {
			return report.Actions[i].NodeID < report.Actions[j].NodeID
		}
		return report.Actions[i].CaseID < report.Actions[j].CaseID
	})
	report.Counts.Total = len(report.Actions)
	return report, nil
}

func qualityNodeMatchesFilter(node profile.InterfaceNode, filter Filter) bool {
	if filter.NodeID != "" && node.ID != filter.NodeID {
		return false
	}
	return MatchesText(filter.Filter, node.ID, node.DisplayName, node.ServiceID, node.Operation, node.Method, node.Path, node.Description, strings.Join(node.Tags, " "))
}

func missingMetadataFields(item QualityCase) []string {
	fields := []string{}
	if !item.HasDescription {
		fields = append(fields, "description")
	}
	if len(item.Tags) == 0 {
		fields = append(fields, "tags")
	}
	if strings.TrimSpace(item.Priority) == "" {
		fields = append(fields, "priority")
	}
	if strings.TrimSpace(item.Owner) == "" {
		fields = append(fields, "owner")
	}
	return fields
}

func metadataIssues(issues []string) []string {
	out := []string{}
	for _, issue := range issues {
		switch issue {
		case "missing-description", "missing-tags", "missing-priority", "missing-owner":
			out = append(out, issue)
		}
	}
	return out
}

func caseLifecycleIssues(issues []string) []string {
	out := []string{}
	for _, issue := range issues {
		switch issue {
		case "invalid-status", "non-executable-lifecycle":
			out = append(out, issue)
		}
	}
	return out
}

func hasRunnableSource(item profile.APICase) bool {
	return strings.TrimSpace(item.CasePath) != "" || hasExternalSource(item)
}

func hasExternalSource(item profile.APICase) bool {
	return strings.TrimSpace(item.SourcePath) != "" || strings.TrimSpace(item.SourceKind) != "" || strings.TrimSpace(item.ExecutorID) != ""
}

func executorReferenceSet(bundle profile.Bundle) map[string]profile.ExecutorDescriptor {
	refs := map[string]profile.ExecutorDescriptor{}
	for _, item := range bundle.Executors {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		refs[id] = item
	}
	return refs
}

func hasUsableExecutorReference(item profile.APICase, refs map[string]profile.ExecutorDescriptor) bool {
	executorID := strings.TrimSpace(item.ExecutorID)
	if executorID == "" || strings.TrimSpace(item.SourcePath) == "" {
		return false
	}
	executor, ok := refs[executorID]
	if !ok {
		return false
	}
	status := strings.TrimSpace(strings.ToLower(executor.Status))
	if status != "" && status != "active" {
		return false
	}
	sourceKind := strings.TrimSpace(strings.ToLower(item.SourceKind))
	executorKind := strings.TrimSpace(strings.ToLower(executor.Kind))
	return sourceKind == "" || executorKind == "" || sourceKind == executorKind
}

func suggestedCaseID(nodeID string) string {
	return "case." + slugValue(nodeID) + ".default"
}

func slugValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "case"
	}
	return out
}

func actionRank(actionType string) int {
	switch actionType {
	case "draft-case":
		return 0
	case "complete-case-metadata":
		return 1
	case "review-case-lifecycle":
		return 2
	case "add-runnable-source":
		return 3
	case "add-execution-config":
		return 4
	default:
		return 99
	}
}
