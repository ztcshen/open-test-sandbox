package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

func handleSaveWorkflowRun(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "runtime store is not configured"})
		return
	}
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	workflowID := strings.TrimSpace(valueString(payload["workflowId"]))
	if workflowID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "workflowId is required"})
		return
	}
	if _, ok := payload["steps"]; !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "steps are required"})
		return
	}
	status := strings.TrimSpace(valueString(payload["status"]))
	if status == "" {
		status = workflowRunStatus(payload["ok"])
		payload["status"] = status
	}
	now := time.Now().UTC()
	id := workflowRunID(now)
	payload["workflowRunId"] = id
	raw, err := json.Marshal(payload)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid workflow run payload"})
		return
	}
	run, err := runtime.CreateRun(r.Context(), store.Run{
		ID:            id,
		ProfileID:     bundle.ID,
		EnvironmentID: valueString(payload["environmentId"]),
		WorkflowID:    workflowID,
		Status:        status,
		EvidenceRoot:  valueString(payload["evidenceRoot"]),
		SummaryJSON:   string(raw),
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := recordWorkflowRunStepCases(r.Context(), runtime, run.ID, payload, now); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := copyWorkflowRunStepEvidence(r.Context(), runtime, run.ID, payload, now); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := copyWorkflowRunStepTraceTopologies(r.Context(), runtime, run.ID, workflowID, payload, now); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := copyWorkflowRunStepPostProcessTasks(r.Context(), runtime, run.ID, workflowID, payload, now); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "workflowRunId": run.ID, "run": workflowRunListItem(run)})
}

func recordWorkflowRunStepCases(ctx context.Context, runtime store.Store, runID string, payload map[string]any, defaultValue time.Time) error {
	for index, raw := range workflowRunSteps(payload) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		stepID, caseID := workflowStepIDs(step)
		if caseID == "" {
			continue
		}
		status := workflowStepCaseStatus(step)
		startedAt := timeFromPayload(step["startedAt"], defaultValue)
		finishedAt := timeFromPayload(step["finishedAt"], startedAt, defaultValue)
		_, err := runtime.RecordAPICaseRun(ctx, store.APICaseRun{
			ID:                   caseRunRunID(runID, index),
			RunID:                runID,
			CaseID:               caseID,
			Status:               status,
			RequestSummaryJSON:   compactJSON(workflowStepRequestSummary(step, stepID, caseID)),
			AssertionSummaryJSON: compactJSON(workflowStepAssertionSummary(step, status)),
			StartedAt:            startedAt,
			FinishedAt:           finishedAt,
			CreatedAt:            defaultValue,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func caseRunRunID(runID string, index int) string {
	return fmt.Sprintf("%s.case.%02d", runID, index+1)
}

func workflowStepCaseStatus(step map[string]any) string {
	if status := strings.TrimSpace(valueString(step["status"])); status != "" {
		return status
	}
	if step["stepOk"] != nil {
		return workflowRunStatus(step["stepOk"])
	}
	return workflowRunStatus(step["ok"])
}

func workflowStepRequestSummary(step map[string]any, stepID string, caseID string) map[string]any {
	for _, key := range []string{"request", "details", "result"} {
		value := mapFromAny(step[key])
		if key == "details" || key == "result" {
			value = mapFromAny(value["request"])
		}
		if len(value) > 0 {
			value["stepId"] = stepID
			value["caseId"] = caseID
			return value
		}
	}
	summary := mapFromAny(step["summary"])
	out := map[string]any{"stepId": stepID, "caseId": caseID}
	for _, key := range []string{"requestId", "httpCode", "targetBaseUrl"} {
		if value, ok := summary[key]; ok {
			out[key] = value
		}
	}
	return out
}

func workflowStepAssertionSummary(step map[string]any, status string) map[string]any {
	summary := mapFromAny(step["summary"])
	out := map[string]any{
		"status": status,
		"passed": status == store.StatusPassed,
	}
	for _, key := range []string{"failureReason", "httpCode", "requestId"} {
		if value, ok := summary[key]; ok {
			out[key] = value
		}
	}
	return out
}

func workflowRunStatus(value any) string {
	ok, isBool := value.(bool)
	if !isBool {
		return store.StatusRunning
	}
	if ok {
		return store.StatusPassed
	}
	return store.StatusFailed
}

func workflowRunID(now time.Time) string {
	return "run." + now.Format("20060102T150405.000000000Z")
}
