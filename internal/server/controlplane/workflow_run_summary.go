package controlplane

import (
	"encoding/json"
	"fmt"
	"strings"

	"agent-testbench/internal/store"
)

func workflowRunListItem(run store.Run) map[string]any {
	stepCount := workflowRunStepCount(run.SummaryJSON)
	return map[string]any{
		"id":           run.ID,
		"profileId":    run.ProfileID,
		"workflowId":   run.WorkflowID,
		"status":       run.Status,
		"evidenceRoot": run.EvidenceRoot,
		"summaryJson":  run.SummaryJSON,
		"stepCount":    stepCount,
		"createdAt":    run.CreatedAt,
		"updatedAt":    run.UpdatedAt,
	}
}

func workflowRunCatalogItem(run store.Run) map[string]any {
	item := workflowRunStepItem(run)
	item["startedAt"] = run.StartedAt
	item["finishedAt"] = run.FinishedAt
	return item
}

func workflowRunStepItem(run store.Run) map[string]any {
	item := workflowRunListItem(run)
	delete(item, "summaryJson")
	return item
}

func workflowRunStepCount(raw string) int {
	summary, err := workflowRunSummary(raw)
	if err != nil {
		return 0
	}
	if value := intFromAny(summary["stepCount"]); value > 0 {
		return value
	}
	if nested := mapFromAny(summary["summary"]); len(nested) > 0 {
		if value := intFromAny(nested["stepCount"]); value > 0 {
			return value
		}
		if value := intFromAny(nested["expectedStepCount"]); value > 0 {
			return value
		}
	}
	return len(workflowRunSteps(summary))
}

func workflowRunSummary(raw string) (map[string]any, error) {
	var summary map[string]any
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		return nil, fmt.Errorf("invalid workflow summary JSON: %w", err)
	}
	if summary["steps"] == nil {
		summary["steps"] = []map[string]any{}
	}
	return summary, nil
}

func workflowRunSummaryContainsStep(raw string, stepID string) bool {
	summary, err := workflowRunSummary(raw)
	if err != nil {
		return false
	}
	_, ok := workflowRunStep(summary, stepID)
	return ok
}

func workflowRunSummaryStepHasHTTPResult(raw string, stepID string) bool {
	summary, err := workflowRunSummary(raw)
	if err != nil {
		return false
	}
	step, ok := workflowRunStep(summary, stepID)
	if !ok {
		return false
	}
	result := mapFromAny(step["result"])
	response := mapFromAny(result["response"])
	if intValue(response["statusCode"]) > 0 {
		return true
	}
	stepSummary := mapFromAny(step["summary"])
	return intValue(stepSummary["httpCode"]) > 0
}

func workflowRunStep(summary map[string]any, stepID string) (map[string]any, bool) {
	for _, raw := range workflowRunSteps(summary) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(valueString(step["stepId"])) == stepID {
			return step, true
		}
	}
	return nil, false
}

func workflowRunSteps(summary map[string]any) []any {
	switch steps := summary["steps"].(type) {
	case []any:
		return steps
	case []map[string]any:
		out := make([]any, 0, len(steps))
		for _, step := range steps {
			out = append(out, step)
		}
		return out
	default:
		return nil
	}
}
