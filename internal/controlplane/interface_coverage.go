package controlplane

import (
	"sort"

	"open-test-sandbox/internal/profile"
)

func interfaceNodeCoveragePayloadFromBundle(bundle profile.Bundle, workflowID string) map[string]any {
	rows := interfaceNodeCoverageRows(bundle, workflowID)
	return map[string]any{
		"ok":         true,
		"templateId": "TPL-INTERFACE-NODE-COVERAGE-V1",
		"workflowId": workflowID,
		"summary":    interfaceNodeCoverageSummary(rows),
		"rows":       rows,
		"source":     map[string]string{"kind": "profile", "id": bundle.ID},
	}
}

func interfaceNodeCoverageGapsPayloadFromBundle(bundle profile.Bundle, workflowID string) map[string]any {
	rows := interfaceNodeCoverageRows(bundle, workflowID)
	gaps := make([]map[string]any, 0)
	for _, row := range rows {
		if mapped, _ := row["mapped"].(bool); !mapped {
			gaps = append(gaps, row)
		}
	}
	return map[string]any{
		"ok":         true,
		"templateId": "TPL-INTERFACE-NODE-COVERAGE-GAP-V1",
		"workflowId": workflowID,
		"summary": map[string]any{
			"totalSteps": len(rows),
			"gapCount":   len(gaps),
		},
		"gaps":   gaps,
		"source": map[string]string{"kind": "profile", "id": bundle.ID},
	}
}

func interfaceNodeCoverageRows(bundle profile.Bundle, workflowID string) []map[string]any {
	nodeByID := make(map[string]profile.InterfaceNode, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		nodeByID[node.ID] = node
	}
	caseByID := make(map[string]profile.APICase, len(bundle.APICases))
	for _, item := range bundle.APICases {
		caseByID[item.ID] = item
	}

	rows := make([]map[string]any, 0)
	for _, binding := range bundle.WorkflowBindings {
		if workflowID != "" && binding.WorkflowID != workflowID {
			continue
		}
		node, mapped := nodeByID[binding.NodeID]
		item := caseByID[binding.CaseID]
		row := map[string]any{
			"workflowId":      binding.WorkflowID,
			"stepId":          binding.StepID,
			"caseId":          binding.CaseID,
			"caseDisplayName": item.DisplayName,
			"required":        binding.Required,
			"mapped":          mapped,
			"admissionStatus": "pending",
		}
		if mapped {
			row["nodeId"] = node.ID
			row["nodeDisplayName"] = node.DisplayName
			row["serviceId"] = node.ServiceID
			row["href"] = "/interface-node.html?id=" + node.ID
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i int, j int) bool {
		left := valueString(rows[i]["workflowId"]) + "\x00" + valueString(rows[i]["stepId"])
		right := valueString(rows[j]["workflowId"]) + "\x00" + valueString(rows[j]["stepId"])
		return left < right
	})
	return rows
}

func interfaceNodeCoverageSummary(rows []map[string]any) map[string]any {
	mapped := 0
	pending := 0
	for _, row := range rows {
		if ok, _ := row["mapped"].(bool); ok {
			mapped++
		}
		if valueString(row["admissionStatus"]) == "pending" {
			pending++
		}
	}
	return map[string]any{
		"totalSteps":    len(rows),
		"mappedSteps":   mapped,
		"unmappedSteps": len(rows) - mapped,
		"passedNodes":   0,
		"failedNodes":   0,
		"pendingNodes":  pending,
	}
}
