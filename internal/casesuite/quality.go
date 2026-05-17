package casesuite

import (
	"context"
	"strings"
	"time"

	"open-test-sandbox/internal/profile"
)

type QualityCounts struct {
	Nodes              int `json:"nodes"`
	NodesWithoutCases  int `json:"nodesWithoutCases"`
	Cases              int `json:"cases"`
	CompleteCases      int `json:"completeCases"`
	IncompleteCases    int `json:"incompleteCases"`
	MissingDescription int `json:"missingDescription"`
	MissingTags        int `json:"missingTags"`
	MissingPriority    int `json:"missingPriority"`
	MissingOwner       int `json:"missingOwner"`
	MissingRunnable    int `json:"missingRunnable"`
	MissingExecution   int `json:"missingExecution"`
	Inactive           int `json:"inactive"`
}

type QualityCase struct {
	CaseID             string   `json:"caseId"`
	Title              string   `json:"title"`
	NodeID             string   `json:"nodeId,omitempty"`
	NodeName           string   `json:"nodeName,omitempty"`
	Status             string   `json:"status"`
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

func Quality(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, cases []profile.APICase) (QualityReport, error) {
	filter = NormalizeFilter(filter)
	configs := ExecutionConfigSet(ctx, bundle, runtime)
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
			Tags:               append([]string(nil), item.Tags...),
			Priority:           item.Priority,
			Owner:              item.Owner,
			HasDescription:     strings.TrimSpace(item.Description) != "",
			HasRunnableFile:    strings.TrimSpace(item.CasePath) != "",
			HasExecutionConfig: configs[item.ID],
		}
		if !strings.EqualFold(row.Status, "active") {
			row.Issues = append(row.Issues, "inactive")
			report.Counts.Inactive++
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

func qualityNodeMatchesFilter(node profile.InterfaceNode, filter Filter) bool {
	if filter.NodeID != "" && node.ID != filter.NodeID {
		return false
	}
	return MatchesText(filter.Filter, node.ID, node.DisplayName, node.ServiceID, node.Operation, node.Method, node.Path, node.Description, strings.Join(node.Tags, " "))
}
