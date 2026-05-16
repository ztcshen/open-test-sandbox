package controlplane

import "open-test-sandbox/internal/store"

type topologyEvidenceViewInput struct {
	RunID        string
	CaseID       string
	SavedSummary map[string]any
	Rows         []store.TraceTopology
}

func traceTopologyPayload(row store.TraceTopology) map[string]any {
	return map[string]any{
		"id":            row.ID,
		"workflowRunId": row.WorkflowRunID,
		"workflowId":    row.WorkflowID,
		"stepId":        row.StepID,
		"caseId":        row.CaseID,
		"requestId":     row.RequestID,
		"traceId":       row.TraceID,
		"status":        row.Status,
		"topologyJson":  row.TopologyJSON,
		"textTopology":  row.TextTopology,
		"createdAt":     row.CreatedAt,
	}
}

func topologyEvidenceViewForCase(input topologyEvidenceViewInput) map[string]any {
	if topology := storedTraceTopologyEvidence(input.CaseID, input.Rows); len(topology) > 0 {
		return topology
	}
	if topology := mapFromAny(input.SavedSummary["traceTopology"]); len(topology) > 0 {
		return topology
	}
	if topology := mapFromAny(input.SavedSummary["topology"]); len(topology) > 0 {
		return topology
	}
	return unavailableTraceTopologyEvidence(input.RunID, input.CaseID)
}

func storedTraceTopologyEvidence(caseID string, rows []store.TraceTopology) map[string]any {
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if row.CaseID != caseID {
			continue
		}
		return traceTopologyEvidencePayload(row)
	}
	return map[string]any{}
}

func traceTopologyEvidencePayload(row store.TraceTopology) map[string]any {
	topology := jsonObject(row.TopologyJSON)
	if len(topology) == 0 {
		topology = map[string]any{}
	}
	for key, value := range traceTopologyPayload(row) {
		if key == "topologyJson" {
			continue
		}
		if _, exists := topology[key]; !exists {
			topology[key] = value
		}
	}
	return topology
}

func unavailableTraceTopologyEvidence(runID string, caseID string) map[string]any {
	return map[string]any{
		"status":          "unavailable",
		"caseId":          caseID,
		"runId":           runID,
		"observedNodes":   []string{},
		"confirmedEdges":  []map[string]any{},
		"externalExits":   []map[string]any{},
		"unresolvedExits": []map[string]any{},
		"warnings":        []string{"Trace topology was not captured for this case run."},
		"textTopology":    "Trace topology unavailable: no captured spans",
	}
}
