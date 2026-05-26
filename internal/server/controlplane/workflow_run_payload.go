package controlplane

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"agent-testbench/internal/store"
)

func writeWorkflowRunPayload(w http.ResponseWriter, ctx context.Context, runtime store.Store, run store.Run) {
	payload, err := WorkflowRunPayloadForRun(ctx, runtime, run)
	if err != nil {
		if strings.HasPrefix(err.Error(), "invalid workflow summary JSON") {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, payload)
}

func WorkflowRunPayloadForRun(ctx context.Context, runtime store.Store, run store.Run) (map[string]any, error) {
	summary, err := workflowRunSummary(run.SummaryJSON)
	if err != nil {
		return nil, err
	}
	stepTimeouts, workflowTimeoutMs, err := workflowTimeoutConfigFromStore(ctx, runtime, run.WorkflowID)
	if err != nil {
		return nil, err
	}
	applyWorkflowRunTimeouts(summary, stepTimeouts, workflowTimeoutMs)
	topologies, err := workflowRunTraceTopologies(ctx, runtime, run.ID, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":              true,
		"run":             workflowRunListItem(run),
		"summary":         summary,
		"traceTopologies": topologies,
	}, nil
}

func writeWorkflowStepRunPayload(w http.ResponseWriter, ctx context.Context, runtime store.Store, run store.Run, stepID string) {
	payload, ok, err := WorkflowStepRunPayloadForRun(ctx, runtime, run, stepID)
	if err != nil {
		if strings.HasPrefix(err.Error(), "invalid workflow summary JSON") {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow run step not found"})
		return
	}
	writeJSON(w, payload)
}

func WorkflowStepRunPayload(ctx context.Context, runtime store.Store, runID string, stepID string) (map[string]any, bool, error) {
	run, err := runtime.GetRun(ctx, strings.TrimSpace(runID))
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return WorkflowStepRunPayloadForRun(ctx, runtime, run, stepID)
}

func LatestWorkflowStepRunPayload(ctx context.Context, runtime store.Store, workflowID string, stepID string) (map[string]any, bool, error) {
	workflowID = strings.TrimSpace(workflowID)
	stepID = strings.TrimSpace(stepID)
	if fast, ok := runtime.(latestWorkflowStepRunStore); ok {
		run, err := fast.LatestWorkflowStepRun(ctx, workflowID, stepID, true)
		if errors.Is(err, store.ErrNotFound) {
			run, err = fast.LatestWorkflowStepRun(ctx, workflowID, stepID, false)
		}
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return WorkflowStepRunPayloadForRun(ctx, runtime, run, stepID)
	}
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return nil, false, err
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
		return WorkflowStepRunPayloadForRun(ctx, runtime, run, stepID)
	}
	if defaultValue != nil {
		return WorkflowStepRunPayloadForRun(ctx, runtime, *defaultValue, stepID)
	}
	return nil, false, nil
}

func WorkflowStepRunPayloadForRun(ctx context.Context, runtime store.Store, run store.Run, stepID string) (map[string]any, bool, error) {
	summary, err := workflowRunSummary(run.SummaryJSON)
	if err != nil {
		return nil, false, err
	}
	stepTimeouts, workflowTimeoutMs, err := workflowTimeoutConfigFromStore(ctx, runtime, run.WorkflowID)
	if err != nil {
		return nil, false, err
	}
	applyWorkflowRunTimeouts(summary, stepTimeouts, workflowTimeoutMs)
	step, ok := workflowRunStep(summary, stepID)
	if !ok {
		return nil, false, nil
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
		return nil, false, err
	}
	if len(topologies) == 0 {
		topologies, err = copyWorkflowStepTraceTopologiesFromSources(ctx, runtime, run.ID, run.WorkflowID, step, time.Now().UTC())
		if err != nil {
			return nil, false, err
		}
	}
	enrichWorkflowStepLogs(ctx, runtime, run, step, topologies)
	tasks, err := workflowRunPostProcessTasks(ctx, runtime, run.ID, stepID)
	if err != nil {
		return nil, false, err
	}
	if len(tasks) == 0 {
		tasks, err = copyWorkflowStepPostProcessTasksFromSources(ctx, runtime, run.ID, run.WorkflowID, step, time.Now().UTC())
		if err != nil {
			return nil, false, err
		}
	}
	return map[string]any{
		"ok":               true,
		"run":              workflowRunStepItem(run),
		"summary":          stepSummary,
		"traceTopologies":  topologies,
		"postProcessTasks": tasks,
	}, true, nil
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
