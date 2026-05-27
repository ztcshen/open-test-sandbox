package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-testbench/internal/store"
)

type traceCollector struct {
	GraphQLURL string
}

type TraceCollector = traceCollector

type traceTopologyCollectInput struct {
	run        store.Run
	caseID     string
	stepID     string
	requestID  string
	traceID    string
	endpoint   string
	rowID      string
	startedAt  time.Time
	finishedAt time.Time
}

func handleTraceTopologyCollect(w http.ResponseWriter, r *http.Request, runtime store.Store, collector traceCollector) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	response, err := CollectTraceTopologyPayload(r.Context(), runtime, collector, payload)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, response)
}

func CollectTraceTopologyPayload(ctx context.Context, runtime store.Store, collector TraceCollector, payload map[string]any) (map[string]any, error) {
	task := traceTopologyCollectTaskSeed(ctx, runtime, payload)
	started := time.Now().UTC()
	status := store.StatusPassed
	errText := ""
	summary := map[string]any{}
	defer func() {
		if task.RunID == "" {
			return
		}
		finished := time.Now().UTC()
		task.Kind = postProcessKindTraceTopology
		task.Status = status
		task.StartedAt = started
		task.FinishedAt = finished
		task.DurationMs = finished.Sub(started).Milliseconds()
		task.Error = errText
		task.SummaryJSON = compactJSON(summary)
		task.CreatedAt = finished
		recordPostProcessTask(ctx, runtime, task)
	}()
	row, topology, err := collectTraceTopology(ctx, runtime, collector, payload)
	if err != nil {
		status = store.StatusFailed
		errText = err.Error()
		summary["error"] = err.Error()
		return nil, err
	}
	task.WorkflowID = row.WorkflowID
	task.StepID = row.StepID
	task.CaseID = row.CaseID
	summary["traceId"] = row.TraceID
	summary["requestId"] = row.RequestID
	summary["topologyStatus"] = topology.Status
	summary["spanCount"] = topology.SpanCount
	return map[string]any{"ok": true, "traceTopology": traceTopologyPayload(row), topologyPayloadField: topology}, nil
}

func traceTopologyCollectTaskSeed(ctx context.Context, runtime store.Store, payload map[string]any) store.PostProcessTask {
	runID := strings.TrimSpace(valueString(payload["runId"]))
	if runtime == nil || runID == "" {
		return store.PostProcessTask{}
	}
	stepID := strings.TrimSpace(valueString(payload["stepId"]))
	task := store.PostProcessTask{
		ID:     runID + "." + safeRuntimeLogPathSegment(stepID) + "." + postProcessKindTraceTopology,
		RunID:  runID,
		StepID: stepID,
		CaseID: strings.TrimSpace(valueString(payload["caseId"])),
	}
	if run, err := runtime.GetRun(ctx, runID); err == nil {
		task.WorkflowID = run.WorkflowID
	}
	return task
}

func collectTraceTopology(ctx context.Context, runtime store.Store, collector traceCollector, payload map[string]any) (store.TraceTopology, traceTopology, error) {
	if strings.TrimSpace(collector.GraphQLURL) == "" {
		return store.TraceTopology{}, traceTopology{}, fmt.Errorf("trace provider GraphQL URL is not configured")
	}
	input, err := traceTopologyCollectInputFromPayload(ctx, runtime, payload)
	if err != nil {
		return store.TraceTopology{}, traceTopology{}, err
	}
	provider := graphQLTraceProvider{URL: collector.GraphQLURL}
	topology, err := queryTraceTopology(ctx, provider, input)
	if err != nil {
		return store.TraceTopology{}, traceTopology{}, err
	}
	if topology.SpanCount == 0 {
		return store.TraceTopology{}, traceTopology{}, fmt.Errorf("trace provider returned no queryable traces")
	}
	raw, err := json.Marshal(topology)
	if err != nil {
		return store.TraceTopology{}, traceTopology{}, err
	}
	row, err := saveCollectedTraceTopology(ctx, runtime, input, topology, raw)
	if err != nil {
		return store.TraceTopology{}, traceTopology{}, err
	}
	return row, topology, nil
}

func traceTopologyCollectInputFromPayload(ctx context.Context, runtime store.Store, payload map[string]any) (traceTopologyCollectInput, error) {
	if runtime == nil {
		return traceTopologyCollectInput{}, fmt.Errorf("runtime store is not configured")
	}
	runID := strings.TrimSpace(valueString(payload["runId"]))
	if runID == "" {
		return traceTopologyCollectInput{}, fmt.Errorf("runId is required")
	}
	run, err := runtime.GetRun(ctx, runID)
	if err != nil {
		return traceTopologyCollectInput{}, err
	}
	caseID, err := traceTopologyCollectCaseID(ctx, runtime, runID, payload)
	if err != nil {
		return traceTopologyCollectInput{}, err
	}
	input := traceTopologyCollectInput{
		run:       run,
		caseID:    caseID,
		stepID:    strings.TrimSpace(valueString(payload["stepId"])),
		requestID: strings.TrimSpace(valueString(payload["requestId"])),
		traceID:   strings.TrimSpace(valueString(payload["traceId"])),
		endpoint:  strings.TrimSpace(valueString(payload["endpoint"])),
		rowID:     strings.TrimSpace(valueString(payload["id"])),
	}
	if input.endpoint == "" && input.traceID == "" {
		return traceTopologyCollectInput{}, fmt.Errorf("endpoint is required")
	}
	input.startedAt = timeFromPayload(payload["startedAt"], run.StartedAt, run.CreatedAt)
	input.finishedAt = timeFromPayload(payload["finishedAt"], run.FinishedAt, run.UpdatedAt, run.CreatedAt)
	if input.finishedAt.Before(input.startedAt) {
		input.finishedAt = input.startedAt.Add(2 * time.Minute)
	}
	return input, nil
}

func traceTopologyCollectCaseID(ctx context.Context, runtime store.Store, runID string, payload map[string]any) (string, error) {
	caseID := strings.TrimSpace(valueString(payload["caseId"]))
	if caseID != "" {
		return caseID, nil
	}
	caseRuns, err := runtime.ListAPICaseRuns(ctx, runID)
	if err != nil {
		return "", err
	}
	if len(caseRuns) == 0 {
		return "", nil
	}
	return caseRuns[0].CaseID, nil
}

func queryTraceTopology(ctx context.Context, provider graphQLTraceProvider, input traceTopologyCollectInput) (traceTopology, error) {
	if input.traceID != "" {
		trace, err := provider.QueryTrace(ctx, input.traceID)
		if err != nil {
			return traceTopology{}, err
		}
		return buildTraceTopology(input.stepID, input.caseID, input.requestID, trace), nil
	}
	return queryBestTraceCandidateTopology(ctx, provider, input)
}

func queryBestTraceCandidateTopology(ctx context.Context, provider graphQLTraceProvider, input traceTopologyCollectInput) (traceTopology, error) {
	candidates, err := provider.FindCandidates(ctx, input.endpoint, input.startedAt.Add(-30*time.Second), input.finishedAt.Add(90*time.Second))
	if err != nil {
		return traceTopology{}, err
	}
	sortTraceCandidatesByRunWindow(candidates, input.startedAt, input.finishedAt)
	var topology traceTopology
	var lastErr error
	for _, candidate := range candidates {
		trace, err := provider.QueryTrace(ctx, candidate.TraceID)
		if err != nil {
			lastErr = err
			continue
		}
		candidateTopology := buildTraceTopology(input.stepID, input.caseID, input.requestID, trace)
		if len(candidateTopology.ConfirmedEdges) > len(topology.ConfirmedEdges) || topology.SpanCount == 0 {
			topology = candidateTopology
		}
	}
	if topology.SpanCount == 0 && lastErr != nil {
		return traceTopology{}, lastErr
	}
	return topology, nil
}

func saveCollectedTraceTopology(ctx context.Context, runtime store.Store, input traceTopologyCollectInput, topology traceTopology, raw []byte) (store.TraceTopology, error) {
	rowID := input.rowID
	if rowID == "" {
		rowID = traceTopologyRowID(input.run.ID, input.stepID, input.caseID, topology.TraceID, topology.RequestID)
	}
	return runtime.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            rowID,
		WorkflowRunID: input.run.ID,
		WorkflowID:    input.run.WorkflowID,
		StepID:        input.stepID,
		CaseID:        input.caseID,
		RequestID:     topology.RequestID,
		TraceID:       topology.TraceID,
		Status:        topology.Status,
		TopologyJSON:  string(raw),
		TextTopology:  topology.TextTopology,
		CreatedAt:     time.Now().UTC(),
	})
}

func traceTopologyRowID(runID string, stepID string, caseID string, traceID string, requestID string) string {
	identity := firstNonEmpty(traceID, requestID, caseID, "topology")
	return strings.Join([]string{
		safeRuntimeLogPathSegment(runID),
		safeRuntimeLogPathSegment(firstNonEmpty(stepID, caseID, "step")),
		safeRuntimeLogPathSegment(identity),
		postProcessKindTraceTopology,
	}, ".")
}

func sortTraceCandidatesByRunWindow(candidates []traceCandidate, startedAt, finishedAt time.Time) {
	if len(candidates) < 2 || startedAt.IsZero() || finishedAt.IsZero() {
		return
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return traceCandidateDistance(candidates[i], startedAt, finishedAt) < traceCandidateDistance(candidates[j], startedAt, finishedAt)
	})
}

func traceCandidateDistance(candidate traceCandidate, startedAt, finishedAt time.Time) time.Duration {
	start, ok := parseTraceCandidateStart(candidate.Start)
	if !ok {
		return 1<<63 - 1
	}
	if start.Before(startedAt) {
		return startedAt.Sub(start)
	}
	if start.After(finishedAt) {
		return start.Sub(finishedAt)
	}
	return 0
}

func parseTraceCandidateStart(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if millis, err := strconv.ParseInt(value, 10, 64); err == nil {
		switch {
		case millis > 1_000_000_000_000_000:
			return time.UnixMicro(millis).UTC(), true
		case millis > 1_000_000_000_000:
			return time.UnixMilli(millis).UTC(), true
		default:
			return time.Unix(millis, 0).UTC(), true
		}
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 1504"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func timeFromPayload(value any, defaultTimes ...time.Time) time.Time {
	if raw := strings.TrimSpace(valueString(value)); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return parsed
		}
	}
	for _, defaultValue := range defaultTimes {
		if !defaultValue.IsZero() {
			return defaultValue
		}
	}
	return time.Now().UTC()
}
