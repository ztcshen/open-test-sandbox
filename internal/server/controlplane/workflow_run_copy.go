package controlplane

import (
	"context"
	"strings"
	"time"

	"agent-testbench/internal/store"
)

func copyWorkflowRunStepEvidence(ctx context.Context, runtime store.Store, runID string, payload map[string]any, defaultValue time.Time) error {
	for index, raw := range workflowRunSteps(payload) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		_, caseID := workflowStepIDs(step)
		if caseID == "" {
			continue
		}
		caseRunID := caseRunRunID(runID, index)
		if _, err := copyWorkflowStepEvidenceFromSources(ctx, runtime, runID, caseRunID, step, defaultValue); err != nil {
			return err
		}
	}
	return nil
}

func copyWorkflowRunStepTraceTopologies(ctx context.Context, runtime store.Store, runID string, workflowID string, payload map[string]any, defaultValue time.Time) error {
	for _, raw := range workflowRunSteps(payload) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := copyWorkflowStepTraceTopologiesFromSources(ctx, runtime, runID, workflowID, step, defaultValue); err != nil {
			return err
		}
	}
	return nil
}

func copyWorkflowRunStepPostProcessTasks(ctx context.Context, runtime store.Store, runID string, workflowID string, payload map[string]any, defaultValue time.Time) error {
	for _, raw := range workflowRunSteps(payload) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := copyWorkflowStepPostProcessTasksFromSources(ctx, runtime, runID, workflowID, step, defaultValue); err != nil {
			return err
		}
	}
	return nil
}

func copyWorkflowStepEvidenceFromSources(ctx context.Context, runtime store.Store, runID string, caseRunID string, step map[string]any, defaultValue time.Time) ([]map[string]any, error) {
	copiedRows := []map[string]any{}
	if runtime == nil {
		return copiedRows, nil
	}
	for _, source := range workflowStepCopySources(runID, step) {
		rows, err := runtime.ListEvidence(ctx, source.runID)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if source.stepID != "" && row.StepID != "" && row.StepID != source.stepID {
				continue
			}
			labels := jsonObject(row.LabelsJSON)
			if source.caseID != "" && valueString(labels["caseId"]) != "" && valueString(labels["caseId"]) != source.caseID {
				continue
			}
			copied := row
			copied.ID = copiedWorkflowEvidenceID(runID, source.stepID, row)
			copied.RunID = runID
			copied.CaseRunID = caseRunID
			copied.StepID = firstNonEmpty(source.stepID, row.StepID)
			copied.CreatedAt = defaultValue
			labels["runId"] = runID
			labels["caseRunId"] = caseRunID
			if source.caseID != "" {
				labels["caseId"] = source.caseID
			}
			if copied.StepID != "" {
				labels["stepId"] = copied.StepID
			}
			copied.LabelsJSON = compactJSON(labels)
			if copied.CreatedAt.IsZero() {
				copied.CreatedAt = time.Now().UTC()
			}
			saved, err := runtime.RecordEvidence(ctx, copied)
			if err != nil {
				return nil, err
			}
			copiedRows = append(copiedRows, map[string]any{
				"id":        saved.ID,
				"runId":     saved.RunID,
				"caseRunId": saved.CaseRunID,
				"stepId":    saved.StepID,
				"kind":      saved.Kind,
				"uri":       saved.URI,
			})
		}
	}
	return copiedRows, nil
}

func copyWorkflowStepTraceTopologiesFromSources(ctx context.Context, runtime store.Store, runID string, workflowID string, step map[string]any, defaultValue time.Time) ([]map[string]any, error) {
	copiedRows := []map[string]any{}
	if runtime == nil {
		return copiedRows, nil
	}
	stepID, caseID := workflowStepIDs(step)
	if row, ok := workflowStepEmbeddedTraceTopology(step); ok && isSkyWalkingTraceTopology(row) {
		copied, err := saveCopiedWorkflowTraceTopology(ctx, runtime, row, runID, workflowID, stepID, caseID, defaultValue)
		if err != nil {
			return nil, err
		}
		copiedRows = append(copiedRows, copied)
	}
	for _, source := range workflowStepCopySources(runID, step) {
		rows, err := runtime.ListTraceTopologies(ctx, source.runID)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if !isSkyWalkingTraceTopology(row) {
				continue
			}
			if source.stepID != "" && row.StepID != "" && row.StepID != source.stepID {
				continue
			}
			if source.caseID != "" && row.CaseID != "" && row.CaseID != source.caseID {
				continue
			}
			copied, err := saveCopiedWorkflowTraceTopology(ctx, runtime, row, runID, workflowID, source.stepID, source.caseID, defaultValue)
			if err != nil {
				return nil, err
			}
			copiedRows = append(copiedRows, copied)
		}
	}
	return copiedRows, nil
}

func workflowStepEmbeddedTraceTopology(step map[string]any) (store.TraceTopology, bool) {
	row := mapFromAny(step["traceTopologyRow"])
	topology := mapFromAny(step["traceTopology"])
	if len(row) == 0 && len(topology) == 0 {
		return store.TraceTopology{}, false
	}
	topologyJSON := valueString(row["topologyJson"])
	if topologyJSON == "" && len(topology) > 0 {
		topologyJSON = compactJSON(topology)
	}
	if topologyJSON == "" {
		topologyJSON = "{}"
	}
	out := store.TraceTopology{
		ID:            valueString(row["id"]),
		WorkflowRunID: valueString(row["workflowRunId"]),
		WorkflowID:    valueString(row["workflowId"]),
		StepID:        firstNonEmpty(valueString(row["stepId"]), valueString(topology["stepId"])),
		CaseID:        firstNonEmpty(valueString(row["caseId"]), valueString(topology["caseId"])),
		RequestID:     firstNonEmpty(valueString(row["requestId"]), valueString(topology["requestId"])),
		TraceID:       firstNonEmpty(valueString(row["traceId"]), valueString(topology["traceId"])),
		Status:        firstNonEmpty(valueString(row["status"]), valueString(topology["status"])),
		TopologyJSON:  topologyJSON,
		TextTopology:  firstNonEmpty(valueString(row["textTopology"]), valueString(topology["textTopology"])),
		CreatedAt:     timeFromPayload(row["createdAt"]),
	}
	return out, true
}

func saveCopiedWorkflowTraceTopology(ctx context.Context, runtime store.Store, row store.TraceTopology, runID string, workflowID string, stepID string, caseID string, defaultValue time.Time) (map[string]any, error) {
	copied := row
	copied.ID = copiedWorkflowTraceTopologyID(runID, stepID, row)
	copied.WorkflowRunID = runID
	copied.WorkflowID = firstNonEmpty(workflowID, row.WorkflowID)
	copied.StepID = firstNonEmpty(stepID, row.StepID)
	copied.CaseID = firstNonEmpty(caseID, row.CaseID)
	copied.CreatedAt = defaultValue
	if copied.CreatedAt.IsZero() {
		copied.CreatedAt = time.Now().UTC()
	}
	saved, err := runtime.SaveTraceTopology(ctx, copied)
	if err != nil {
		return nil, err
	}
	return traceTopologyPayload(saved), nil
}

func copyWorkflowStepPostProcessTasksFromSources(ctx context.Context, runtime store.Store, runID string, workflowID string, step map[string]any, defaultValue time.Time) ([]map[string]any, error) {
	copiedRows := []map[string]any{}
	if runtime == nil {
		return copiedRows, nil
	}
	for _, source := range workflowStepCopySources(runID, step) {
		rows, err := runtime.ListPostProcessTasks(ctx, source.runID)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if source.stepID != "" && row.StepID != "" && row.StepID != source.stepID {
				continue
			}
			if source.caseID != "" && row.CaseID != "" && row.CaseID != source.caseID {
				continue
			}
			copied := row
			copied.ID = copiedWorkflowPostProcessTaskID(runID, source.stepID, row)
			copied.RunID = runID
			copied.WorkflowID = firstNonEmpty(workflowID, row.WorkflowID)
			copied.StepID = firstNonEmpty(source.stepID, row.StepID)
			copied.CaseID = firstNonEmpty(source.caseID, row.CaseID)
			if copied.CreatedAt.IsZero() {
				copied.CreatedAt = defaultValue
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

type workflowStepCopySource struct {
	runID  string
	stepID string
	caseID string
}

func workflowStepCopySources(runID string, step map[string]any) []workflowStepCopySource {
	stepID, caseID := workflowStepIDs(step)
	sources := []workflowStepCopySource{}
	for _, sourceRunID := range workflowStepSourceRunIDs(step) {
		if sourceRunID == "" || sourceRunID == runID {
			continue
		}
		sources = append(sources, workflowStepCopySource{
			runID:  sourceRunID,
			stepID: stepID,
			caseID: caseID,
		})
	}
	return sources
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

func workflowStepIDs(step map[string]any) (string, string) {
	return strings.TrimSpace(valueString(step["stepId"])), strings.TrimSpace(valueString(step["caseId"]))
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

func copiedWorkflowEvidenceID(runID string, stepID string, row store.EvidenceRecord) string {
	suffix := firstNonEmpty(row.Kind, row.ID, "evidence")
	return runID + "." + safeRuntimeLogPathSegment(stepID) + "." + safeRuntimeLogPathSegment(suffix)
}
