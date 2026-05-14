package controlplane

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func handleRuns(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	payload := runsPayload{
		WorkflowRuns: []map[string]any{},
		ReplayRuns:   []map[string]any{},
		ProbeRuns:    []map[string]any{},
	}
	if runtime == nil {
		writeJSON(w, payload)
		return
	}
	runs, err := runtime.ListRuns(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for i := len(runs) - 1; i >= 0; i-- {
		payload.WorkflowRuns = append(payload.WorkflowRuns, workflowRunListItem(runs[i]))
	}
	writeJSON(w, payload)
}

func handleWorkflowRun(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "runtime store is not configured"})
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/workflow-runs/"))
	if id == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "workflow run id is required"})
		return
	}
	run, err := runtime.GetRun(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow run not found"})
		return
	}
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeWorkflowRunPayload(w, run)
}

func handleWorkflowStepRun(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "runtime store is not configured"})
		return
	}
	runID := strings.TrimSpace(r.URL.Query().Get("runId"))
	stepID := strings.TrimSpace(r.URL.Query().Get("stepId"))
	if runID == "" || stepID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "runId and stepId are required"})
		return
	}
	run, err := runtime.GetRun(r.Context(), runID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow run not found"})
		return
	}
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeWorkflowStepRunPayload(w, run, stepID)
}

func handleLatestWorkflowStepRun(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "runtime store is not configured"})
		return
	}
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflowId"))
	stepID := strings.TrimSpace(r.URL.Query().Get("stepId"))
	if workflowID == "" || stepID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "workflowId and stepId are required"})
		return
	}
	runs, err := runtime.ListRuns(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if run.WorkflowID != workflowID || !workflowRunSummaryContainsStep(run.SummaryJSON, stepID) {
			continue
		}
		writeWorkflowStepRunPayload(w, run, stepID)
		return
	}
	writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow run step not found"})
}

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
		ID:           id,
		ProfileID:    bundle.ID,
		WorkflowID:   workflowID,
		Status:       status,
		EvidenceRoot: valueString(payload["evidenceRoot"]),
		SummaryJSON:  string(raw),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "workflowRunId": run.ID, "run": workflowRunListItem(run)})
}

func writeWorkflowRunPayload(w http.ResponseWriter, run store.Run) {
	summary, err := workflowRunSummary(run.SummaryJSON)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":              true,
		"run":             workflowRunListItem(run),
		"summary":         summary,
		"traceTopologies": []map[string]any{},
	})
}

func writeWorkflowStepRunPayload(w http.ResponseWriter, run store.Run, stepID string) {
	summary, err := workflowRunSummary(run.SummaryJSON)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	step, ok := workflowRunStep(summary, stepID)
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow run step not found"})
		return
	}
	stepSummary := map[string]any{"steps": []map[string]any{step}}
	if nested, ok := summary["summary"].(map[string]any); ok {
		for _, key := range []string{"expectedStepCount", "stepCount", "passed"} {
			if value, exists := nested[key]; exists {
				stepSummary[key] = value
			}
		}
	}
	writeJSON(w, map[string]any{
		"ok":              true,
		"run":             workflowRunListItem(run),
		"summary":         stepSummary,
		"traceTopologies": []map[string]any{},
	})
}

func workflowRunListItem(run store.Run) map[string]any {
	return map[string]any{
		"id":           run.ID,
		"profileId":    run.ProfileID,
		"workflowId":   run.WorkflowID,
		"status":       run.Status,
		"evidenceRoot": run.EvidenceRoot,
		"createdAt":    run.CreatedAt,
		"updatedAt":    run.UpdatedAt,
	}
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

func readJSONPayload(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = []byte("{}")
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func valueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}
