package casesuite

import (
	"context"
	"sort"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
)

type ImpactOptions struct {
	Signals []string    `json:"signals,omitempty"`
	Plan    PlanOptions `json:"plan,omitempty"`
}

type ImpactCounts struct {
	Signals   int `json:"signals"`
	Nodes     int `json:"nodes"`
	Workflows int `json:"workflows"`
	Cases     int `json:"cases"`
	Selected  int `json:"selected"`
	Blocked   int `json:"blocked"`
}

type ImpactNode struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName,omitempty"`
	ServiceID   string   `json:"serviceId,omitempty"`
	Operation   string   `json:"operation,omitempty"`
	Method      string   `json:"method,omitempty"`
	Path        string   `json:"path,omitempty"`
	Reasons     []string `json:"reasons,omitempty"`
}

type ImpactWorkflow struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName,omitempty"`
	Reasons     []string `json:"reasons,omitempty"`
}

type ImpactCase struct {
	InspectionItem
	Reasons []string `json:"reasons,omitempty"`
}

type ImpactReport struct {
	OK           bool             `json:"ok"`
	ProfileID    string           `json:"profileId"`
	GeneratedAt  string           `json:"generatedAt"`
	Filters      Filter           `json:"filters"`
	Signals      []string         `json:"signals,omitempty"`
	Counts       ImpactCounts     `json:"counts"`
	Nodes        []ImpactNode     `json:"nodes,omitempty"`
	Workflows    []ImpactWorkflow `json:"workflows,omitempty"`
	Cases        []ImpactCase     `json:"cases,omitempty"`
	Plan         PlanReport       `json:"plan"`
	BatchRequest BatchRequest     `json:"batchRequest"`
	Warnings     []string         `json:"warnings,omitempty"`
}

func Impact(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, options ImpactOptions) (ImpactReport, error) {
	filter = NormalizeFilter(filter)
	signals := NormalizeStringList(options.Signals)
	impact := collectImpact(bundle, signals)
	cases := impactCases(bundle, filter, impact.caseReasons)
	plan, err := Plan(ctx, bundle, runtime, filter, cases, options.Plan)
	if err != nil {
		return ImpactReport{}, err
	}
	report := ImpactReport{
		OK:           plan.OK && len(signals) > 0,
		ProfileID:    bundle.ID,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Filters:      filter,
		Signals:      signals,
		Plan:         plan,
		BatchRequest: plan.BatchRequest,
		Warnings:     append([]string(nil), plan.Warnings...),
	}
	if len(signals) == 0 {
		report.Warnings = append(report.Warnings, "no impact signals provided")
	}
	report.Nodes = impactNodes(bundle, impact.nodeReasons)
	report.Workflows = impactWorkflows(bundle, impact.workflowReasons)
	report.Cases = impactCaseRows(plan, impact.caseReasons)
	report.Counts = ImpactCounts{
		Signals:   len(signals),
		Nodes:     len(report.Nodes),
		Workflows: len(report.Workflows),
		Cases:     len(report.Cases),
		Selected:  plan.Counts.Selected,
		Blocked:   plan.Counts.Blocked,
	}
	return report, nil
}

type impactSelection struct {
	nodeReasons     map[string][]string
	workflowReasons map[string][]string
	caseReasons     map[string][]string
}

func collectImpact(bundle profile.Bundle, signals []string) impactSelection {
	out := impactSelection{
		nodeReasons:     map[string][]string{},
		workflowReasons: map[string][]string{},
		caseReasons:     map[string][]string{},
	}
	for _, signal := range signals {
		for _, node := range bundle.InterfaceNodes {
			if MatchesText(signal, node.ID, node.DisplayName, node.ServiceID, node.Operation, node.Method, node.Path, node.Description, strings.Join(node.Tags, " ")) {
				out.nodeReasons[node.ID] = appendReason(out.nodeReasons[node.ID], "matched node signal: "+signal)
			}
		}
		for _, workflow := range bundle.Workflows {
			if MatchesText(signal, workflow.ID, workflow.DisplayName, workflow.Description) {
				out.workflowReasons[workflow.ID] = appendReason(out.workflowReasons[workflow.ID], "matched workflow signal: "+signal)
			}
		}
		for _, item := range bundle.APICases {
			if MatchesText(signal, item.ID, item.DisplayName, item.Description, item.Scenario, item.Owner, item.Priority, strings.Join(item.Tags, " "), item.NodeID) {
				out.caseReasons[item.ID] = appendReason(out.caseReasons[item.ID], "matched case signal: "+signal)
			}
		}
	}
	for _, binding := range bundle.WorkflowBindings {
		if len(out.nodeReasons[binding.NodeID]) > 0 {
			out.workflowReasons[binding.WorkflowID] = appendReason(out.workflowReasons[binding.WorkflowID], "contains impacted node: "+binding.NodeID)
		}
		if len(out.workflowReasons[binding.WorkflowID]) > 0 {
			if strings.TrimSpace(binding.NodeID) != "" {
				out.nodeReasons[binding.NodeID] = appendReason(out.nodeReasons[binding.NodeID], "bound to impacted workflow: "+binding.WorkflowID)
			}
			if strings.TrimSpace(binding.CaseID) != "" {
				out.caseReasons[binding.CaseID] = appendReason(out.caseReasons[binding.CaseID], "bound to impacted workflow: "+binding.WorkflowID)
			}
		}
	}
	for _, item := range bundle.APICases {
		if len(out.nodeReasons[item.NodeID]) > 0 {
			out.caseReasons[item.ID] = appendReason(out.caseReasons[item.ID], "attached to impacted node: "+item.NodeID)
		}
	}
	return out
}

func impactCases(bundle profile.Bundle, filter Filter, reasons map[string][]string) []profile.APICase {
	cases := SelectCases(bundle, filter)
	out := make([]profile.APICase, 0, len(cases))
	for _, item := range cases {
		if len(reasons[item.ID]) > 0 {
			out = append(out, item)
		}
	}
	return out
}

func impactNodes(bundle profile.Bundle, reasons map[string][]string) []ImpactNode {
	out := make([]ImpactNode, 0, len(reasons))
	for _, node := range bundle.InterfaceNodes {
		if len(reasons[node.ID]) == 0 {
			continue
		}
		out = append(out, ImpactNode{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
			Operation:   node.Operation,
			Method:      node.Method,
			Path:        node.Path,
			Reasons:     append([]string(nil), reasons[node.ID]...),
		})
	}
	return out
}

func impactWorkflows(bundle profile.Bundle, reasons map[string][]string) []ImpactWorkflow {
	out := make([]ImpactWorkflow, 0, len(reasons))
	for _, workflow := range bundle.Workflows {
		if len(reasons[workflow.ID]) == 0 {
			continue
		}
		out = append(out, ImpactWorkflow{
			ID:          workflow.ID,
			DisplayName: workflow.DisplayName,
			Reasons:     append([]string(nil), reasons[workflow.ID]...),
		})
	}
	return out
}

func impactCaseRows(plan PlanReport, reasons map[string][]string) []ImpactCase {
	rows := make([]ImpactCase, 0, len(plan.Selected)+len(plan.Blocked)+len(plan.Skipped))
	for _, group := range [][]InspectionItem{plan.Selected, plan.Blocked, plan.Skipped} {
		for _, item := range group {
			rows = append(rows, ImpactCase{
				InspectionItem: item,
				Reasons:        append([]string(nil), reasons[item.CaseID]...),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].NodeID != rows[j].NodeID {
			return rows[i].NodeID < rows[j].NodeID
		}
		if rows[i].CaseID != rows[j].CaseID {
			return rows[i].CaseID < rows[j].CaseID
		}
		return rows[i].Title < rows[j].Title
	})
	return rows
}

func appendReason(existing []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return existing
	}
	for _, item := range existing {
		if item == reason {
			return existing
		}
	}
	return append(existing, reason)
}
