package controlplane

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"agent-testbench/internal/store"
)

func handleRuns(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	payload, err := WorkflowRunsPayload(r.Context(), runtime)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, payload)
}

func WorkflowRunsPayload(ctx context.Context, runtime store.Store) (map[string]any, error) {
	payload := runsPayload{
		OK:           true,
		WorkflowRuns: []map[string]any{},
		ReplayRuns:   []map[string]any{},
		ProbeRuns:    []map[string]any{},
	}
	if runtime == nil {
		return map[string]any{"ok": payload.OK, "workflowRuns": payload.WorkflowRuns, "replayRuns": payload.ReplayRuns, "probeRuns": payload.ProbeRuns}, nil
	}
	runs, err := catalogRunHeaders(ctx, runtime)
	if err != nil {
		return nil, err
	}
	for i := len(runs) - 1; i >= 0; i-- {
		payload.WorkflowRuns = append(payload.WorkflowRuns, workflowRunListItem(runs[i]))
	}
	return map[string]any{"ok": payload.OK, "workflowRuns": payload.WorkflowRuns, "replayRuns": payload.ReplayRuns, "probeRuns": payload.ProbeRuns}, nil
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
	payload, err := WorkflowRunPayloadForRun(r.Context(), runtime, run)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, payload)
}

func WorkflowRunPayload(ctx context.Context, runtime store.Store, id string) (map[string]any, bool, error) {
	run, err := runtime.GetRun(ctx, strings.TrimSpace(id))
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	payload, err := WorkflowRunPayloadForRun(ctx, runtime, run)
	if err != nil {
		return nil, false, err
	}
	return payload, true, nil
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
	if fast, ok := runtime.(workflowStepRunStore); ok {
		run, err := fast.WorkflowStepRun(r.Context(), runID, stepID)
		writeWorkflowStepRunStoreResult(w, r, runtime, run, stepID, err, "workflow run step not found")
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
	writeWorkflowStepRunPayload(w, r.Context(), runtime, run, stepID)
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
	if fast, ok := runtime.(latestWorkflowStepRunStore); ok {
		run, err := fast.LatestWorkflowStepRun(r.Context(), workflowID, stepID, true)
		if errors.Is(err, store.ErrNotFound) {
			run, err = fast.LatestWorkflowStepRun(r.Context(), workflowID, stepID, false)
		}
		writeWorkflowStepRunStoreResult(w, r, runtime, run, stepID, err, "workflow run step not found")
		return
	}
	runs, err := runtime.ListRuns(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var defaultValue *store.Run
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if run.WorkflowID != workflowID || !workflowRunSummaryContainsStep(run.SummaryJSON, stepID) {
			continue
		}
		if defaultValue == nil {
			candidate := run
			defaultValue = &candidate
		}
		if !workflowRunSummaryStepHasHTTPResult(run.SummaryJSON, stepID) {
			continue
		}
		writeWorkflowStepRunPayload(w, r.Context(), runtime, run, stepID)
		return
	}
	if defaultValue != nil {
		writeWorkflowStepRunPayload(w, r.Context(), runtime, *defaultValue, stepID)
		return
	}
	writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow run step not found"})
}

type latestWorkflowStepRunStore interface {
	LatestWorkflowStepRun(context.Context, string, string, bool) (store.Run, error)
}

type workflowStepRunStore interface {
	WorkflowStepRun(context.Context, string, string) (store.Run, error)
}

func writeWorkflowStepRunStoreResult(w http.ResponseWriter, r *http.Request, runtime store.Store, run store.Run, stepID string, err error, notFoundMessage string) {
	if errors.Is(err, store.ErrNotFound) {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": notFoundMessage})
		return
	}
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeWorkflowStepRunPayload(w, r.Context(), runtime, run, stepID)
}
