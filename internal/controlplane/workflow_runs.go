package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func handleRuns(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	payload := runsPayload{
		OK:           true,
		WorkflowRuns: []map[string]any{},
		ReplayRuns:   []map[string]any{},
		ProbeRuns:    []map[string]any{},
	}
	if runtime == nil {
		writeJSON(w, payload)
		return
	}
	runs, err := catalogRunHeaders(r.Context(), runtime)
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
	writeWorkflowRunPayload(w, r.Context(), runtime, run)
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
		if errors.Is(err, store.ErrNotFound) {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow run step not found"})
			return
		}
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeWorkflowStepRunPayload(w, r.Context(), runtime, run, stepID)
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
		if errors.Is(err, store.ErrNotFound) {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow run step not found"})
			return
		}
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeWorkflowStepRunPayload(w, r.Context(), runtime, run, stepID)
		return
	}
	runs, err := runtime.ListRuns(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var fallback *store.Run
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if run.WorkflowID != workflowID || !workflowRunSummaryContainsStep(run.SummaryJSON, stepID) {
			continue
		}
		if fallback == nil {
			candidate := run
			fallback = &candidate
		}
		if !workflowRunSummaryStepHasHTTPResult(run.SummaryJSON, stepID) {
			continue
		}
		writeWorkflowStepRunPayload(w, r.Context(), runtime, run, stepID)
		return
	}
	if fallback != nil {
		writeWorkflowStepRunPayload(w, r.Context(), runtime, *fallback, stepID)
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
	if err := recordWorkflowRunStepCases(r.Context(), runtime, run.ID, payload, now); err != nil {
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

func recordWorkflowRunStepCases(ctx context.Context, runtime store.Store, runID string, payload map[string]any, fallback time.Time) error {
	for index, raw := range workflowRunSteps(payload) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		caseID := strings.TrimSpace(valueString(step["caseId"]))
		if caseID == "" {
			continue
		}
		stepID := strings.TrimSpace(valueString(step["stepId"]))
		status := workflowStepCaseStatus(step)
		startedAt := timeFromPayload(step["startedAt"], fallback)
		finishedAt := timeFromPayload(step["finishedAt"], startedAt, fallback)
		_, err := runtime.RecordAPICaseRun(ctx, store.APICaseRun{
			ID:                   fmt.Sprintf("%s.case.%02d", runID, index+1),
			RunID:                runID,
			CaseID:               caseID,
			Status:               status,
			RequestSummaryJSON:   compactJSON(workflowStepRequestSummary(step, stepID, caseID)),
			AssertionSummaryJSON: compactJSON(workflowStepAssertionSummary(step, status)),
			StartedAt:            startedAt,
			FinishedAt:           finishedAt,
			CreatedAt:            fallback,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func copyWorkflowRunStepTraceTopologies(ctx context.Context, runtime store.Store, runID string, workflowID string, payload map[string]any, fallback time.Time) error {
	for _, raw := range workflowRunSteps(payload) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := copyWorkflowStepTraceTopologiesFromSources(ctx, runtime, runID, workflowID, step, fallback); err != nil {
			return err
		}
	}
	return nil
}

func copyWorkflowRunStepPostProcessTasks(ctx context.Context, runtime store.Store, runID string, workflowID string, payload map[string]any, fallback time.Time) error {
	for _, raw := range workflowRunSteps(payload) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := copyWorkflowStepPostProcessTasksFromSources(ctx, runtime, runID, workflowID, step, fallback); err != nil {
			return err
		}
	}
	return nil
}

func copyWorkflowStepTraceTopologiesFromSources(ctx context.Context, runtime store.Store, runID string, workflowID string, step map[string]any, fallback time.Time) ([]map[string]any, error) {
	copiedRows := []map[string]any{}
	if runtime == nil {
		return copiedRows, nil
	}
	stepID := strings.TrimSpace(valueString(step["stepId"]))
	caseID := strings.TrimSpace(valueString(step["caseId"]))
	for _, sourceRunID := range workflowStepSourceRunIDs(step) {
		if sourceRunID == "" || sourceRunID == runID {
			continue
		}
		rows, err := runtime.ListTraceTopologies(ctx, sourceRunID)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if !isSkyWalkingTraceTopology(row) {
				continue
			}
			if stepID != "" && row.StepID != "" && row.StepID != stepID {
				continue
			}
			if caseID != "" && row.CaseID != "" && row.CaseID != caseID {
				continue
			}
			copied := row
			copied.ID = copiedWorkflowTraceTopologyID(runID, stepID, row)
			copied.WorkflowRunID = runID
			copied.WorkflowID = firstNonEmpty(workflowID, row.WorkflowID)
			copied.StepID = firstNonEmpty(stepID, row.StepID)
			copied.CaseID = firstNonEmpty(caseID, row.CaseID)
			copied.CreatedAt = fallback
			if copied.CreatedAt.IsZero() {
				copied.CreatedAt = time.Now().UTC()
			}
			saved, err := runtime.SaveTraceTopology(ctx, copied)
			if err != nil {
				return nil, err
			}
			copiedRows = append(copiedRows, traceTopologyPayload(saved))
		}
	}
	return copiedRows, nil
}

func copyWorkflowStepPostProcessTasksFromSources(ctx context.Context, runtime store.Store, runID string, workflowID string, step map[string]any, fallback time.Time) ([]map[string]any, error) {
	copiedRows := []map[string]any{}
	if runtime == nil {
		return copiedRows, nil
	}
	stepID := strings.TrimSpace(valueString(step["stepId"]))
	caseID := strings.TrimSpace(valueString(step["caseId"]))
	for _, sourceRunID := range workflowStepSourceRunIDs(step) {
		if sourceRunID == "" || sourceRunID == runID {
			continue
		}
		rows, err := runtime.ListPostProcessTasks(ctx, sourceRunID)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if stepID != "" && row.StepID != "" && row.StepID != stepID {
				continue
			}
			if caseID != "" && row.CaseID != "" && row.CaseID != caseID {
				continue
			}
			copied := row
			copied.ID = copiedWorkflowPostProcessTaskID(runID, stepID, row)
			copied.RunID = runID
			copied.WorkflowID = firstNonEmpty(workflowID, row.WorkflowID)
			copied.StepID = firstNonEmpty(stepID, row.StepID)
			copied.CaseID = firstNonEmpty(caseID, row.CaseID)
			if copied.CreatedAt.IsZero() {
				copied.CreatedAt = fallback
			}
			if copied.CreatedAt.IsZero() {
				copied.CreatedAt = time.Now().UTC()
			}
			saved, err := runtime.RecordPostProcessTask(ctx, copied)
			if err != nil {
				return nil, err
			}
			copiedRows = append(copiedRows, postProcessTaskPayload(saved))
		}
	}
	return copiedRows, nil
}

func workflowStepSourceRunIDs(step map[string]any) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, candidate := range []string{
		valueString(step["runId"]),
		runIDFromCaseRunID(valueString(step["caseRunId"])),
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func runIDFromCaseRunID(caseRunID string) string {
	caseRunID = strings.TrimSpace(caseRunID)
	if strings.HasSuffix(caseRunID, ".case") {
		return strings.TrimSuffix(caseRunID, ".case")
	}
	if index := strings.Index(caseRunID, ".case."); index > 0 {
		return caseRunID[:index]
	}
	return ""
}

func copiedWorkflowTraceTopologyID(runID string, stepID string, row store.TraceTopology) string {
	suffix := firstNonEmpty(row.TraceID, row.RequestID, row.ID, "topology")
	return runID + "." + safeRuntimeLogPathSegment(stepID) + "." + safeRuntimeLogPathSegment(suffix) + "." + postProcessKindTraceTopology
}

func copiedWorkflowPostProcessTaskID(runID string, stepID string, row store.PostProcessTask) string {
	suffix := firstNonEmpty(row.Kind, row.ID, "post-process")
	return runID + "." + safeRuntimeLogPathSegment(stepID) + "." + safeRuntimeLogPathSegment(suffix)
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

func writeWorkflowRunPayload(w http.ResponseWriter, ctx context.Context, runtime store.Store, run store.Run) {
	summary, err := workflowRunSummary(run.SummaryJSON)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	stepTimeouts, workflowTimeoutMs, err := workflowTimeoutConfigFromStore(ctx, runtime, run.WorkflowID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	applyWorkflowRunTimeouts(summary, stepTimeouts, workflowTimeoutMs)
	topologies, err := workflowRunTraceTopologies(ctx, runtime, run.ID, "")
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":              true,
		"run":             workflowRunListItem(run),
		"summary":         summary,
		"traceTopologies": topologies,
	})
}

func writeWorkflowStepRunPayload(w http.ResponseWriter, ctx context.Context, runtime store.Store, run store.Run, stepID string) {
	summary, err := workflowRunSummary(run.SummaryJSON)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	stepTimeouts, workflowTimeoutMs, err := workflowTimeoutConfigFromStore(ctx, runtime, run.WorkflowID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	applyWorkflowRunTimeouts(summary, stepTimeouts, workflowTimeoutMs)
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
	topologies, err := workflowRunTraceTopologies(ctx, runtime, run.ID, stepID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(topologies) == 0 {
		topologies, err = copyWorkflowStepTraceTopologiesFromSources(ctx, runtime, run.ID, run.WorkflowID, step, time.Now().UTC())
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	enrichWorkflowStepLogs(ctx, runtime, run, step, topologies)
	tasks, err := workflowRunPostProcessTasks(ctx, runtime, run.ID, stepID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(tasks) == 0 {
		tasks, err = copyWorkflowStepPostProcessTasksFromSources(ctx, runtime, run.ID, run.WorkflowID, step, time.Now().UTC())
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	writeJSON(w, map[string]any{
		"ok":               true,
		"run":              workflowRunStepItem(run),
		"summary":          stepSummary,
		"traceTopologies":  topologies,
		"postProcessTasks": tasks,
	})
}

func applyWorkflowRunTimeouts(summary map[string]any, stepTimeouts map[string]int, workflowTimeoutMs int) {
	failed := false
	passed := 0
	for _, raw := range workflowRunSteps(summary) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		stepID := strings.TrimSpace(valueString(step["stepId"]))
		timeoutMs := stepTimeouts[stepID]
		evaluation := evaluateRuntimeTimeout(workflowStepElapsedMs(step), timeoutMs)
		applyTimeoutFailure(step, evaluation)
		if evaluation.Exceeded || valueString(step["status"]) == store.StatusFailed || step["stepOk"] == false || step["ok"] == false {
			failed = true
			continue
		}
		if valueString(step["status"]) == store.StatusPassed || step["stepOk"] == true || step["ok"] == true {
			passed++
		}
	}
	if workflowTimeoutMs > 0 {
		summary["timeoutMs"] = workflowTimeoutMs
		nested := mapFromAny(summary["summary"])
		if nested == nil {
			nested = map[string]any{}
		}
		nested["timeoutMs"] = workflowTimeoutMs
		summary["summary"] = nested
		if evaluation := evaluateRuntimeTimeout(workflowElapsedMs(summary), workflowTimeoutMs); evaluation.Exceeded {
			failed = true
			applyTimeoutFailure(summary, evaluation)
			nested = mapFromAny(summary["summary"])
			nested["failureKind"] = "timeout"
			nested["failureReason"] = evaluation.Reason
			summary["summary"] = nested
		}
	}
	if !failed {
		return
	}
	summary["status"] = store.StatusFailed
	summary["ok"] = false
	nested := mapFromAny(summary["summary"])
	if nested == nil {
		nested = map[string]any{}
	}
	nested["passed"] = passed
	if valueString(nested["failureReason"]) == "" {
		nested["failureReason"] = "one or more workflow steps failed"
	}
	summary["summary"] = nested
}

func workflowStepElapsedMs(step map[string]any) int64 {
	if elapsed := intFromAny(step["elapsedMs"]); elapsed > 0 {
		return int64(elapsed)
	}
	for _, key := range []string{"summary", "result", "details"} {
		value := mapFromAny(step[key])
		if elapsed := intFromAny(value["elapsedMs"]); elapsed > 0 {
			return int64(elapsed)
		}
		response := mapFromAny(value["response"])
		if elapsed := intFromAny(response["elapsedMs"]); elapsed > 0 {
			return int64(elapsed)
		}
	}
	return 0
}

func workflowElapsedMs(summary map[string]any) int64 {
	if elapsed := intFromAny(summary["elapsedMs"]); elapsed > 0 {
		return int64(elapsed)
	}
	nested := mapFromAny(summary["summary"])
	if elapsed := intFromAny(nested["elapsedMs"]); elapsed > 0 {
		return int64(elapsed)
	}
	total := int64(0)
	for _, raw := range workflowRunSteps(summary) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		total += workflowStepElapsedMs(step)
	}
	return total
}

func workflowRunTraceTopologies(ctx context.Context, runtime store.Store, runID string, stepID string) ([]map[string]any, error) {
	if runtime == nil {
		return []map[string]any{}, nil
	}
	rows, err := runtime.ListTraceTopologies(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, row := range rows {
		if !isSkyWalkingTraceTopology(row) {
			continue
		}
		if stepID != "" && row.StepID != stepID {
			continue
		}
		out = append(out, traceTopologyPayload(row))
	}
	return out, nil
}

func workflowRunPostProcessTasks(ctx context.Context, runtime store.Store, runID string, stepID string) ([]map[string]any, error) {
	if runtime == nil {
		return []map[string]any{}, nil
	}
	rows, err := runtime.ListPostProcessTasks(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, row := range rows {
		if stepID != "" && row.StepID != stepID {
			continue
		}
		out = append(out, postProcessTaskPayload(row))
	}
	return out, nil
}

func postProcessTaskPayload(row store.PostProcessTask) map[string]any {
	return map[string]any{
		"id":          row.ID,
		"runId":       row.RunID,
		"workflowId":  row.WorkflowID,
		"stepId":      row.StepID,
		"caseId":      row.CaseID,
		"kind":        row.Kind,
		"status":      row.Status,
		"startedAt":   row.StartedAt,
		"finishedAt":  row.FinishedAt,
		"durationMs":  row.DurationMs,
		"error":       row.Error,
		"summaryJson": row.SummaryJSON,
		"createdAt":   row.CreatedAt,
	}
}

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

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		out, _ := typed.Int64()
		return int(out)
	case string:
		out, _ := strconv.Atoi(strings.TrimSpace(typed))
		return out
	default:
		return 0
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
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
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
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}
