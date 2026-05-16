package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"open-test-sandbox/internal/store"
)

type traceCollector struct {
	GraphQLURL string
}

func handleTraceTopologyCollect(w http.ResponseWriter, r *http.Request, runtime store.Store, collector traceCollector) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	row, topology, err := collectTraceTopology(r.Context(), runtime, collector, payload)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "traceTopology": traceTopologyPayload(row), "topology": topology})
}

func collectTraceTopology(ctx context.Context, runtime store.Store, collector traceCollector, payload map[string]any) (store.TraceTopology, traceTopology, error) {
	if runtime == nil {
		return store.TraceTopology{}, traceTopology{}, fmt.Errorf("runtime store is not configured")
	}
	if strings.TrimSpace(collector.GraphQLURL) == "" {
		return store.TraceTopology{}, traceTopology{}, fmt.Errorf("trace provider GraphQL URL is not configured")
	}
	runID := strings.TrimSpace(valueString(payload["runId"]))
	if runID == "" {
		return store.TraceTopology{}, traceTopology{}, fmt.Errorf("runId is required")
	}
	run, err := runtime.GetRun(ctx, runID)
	if err != nil {
		return store.TraceTopology{}, traceTopology{}, err
	}
	caseID := strings.TrimSpace(valueString(payload["caseId"]))
	if caseID == "" {
		caseRuns, err := runtime.ListAPICaseRuns(ctx, runID)
		if err != nil {
			return store.TraceTopology{}, traceTopology{}, err
		}
		if len(caseRuns) > 0 {
			caseID = caseRuns[0].CaseID
		}
	}
	stepID := strings.TrimSpace(valueString(payload["stepId"]))
	requestID := strings.TrimSpace(valueString(payload["requestId"]))
	traceID := strings.TrimSpace(valueString(payload["traceId"]))
	endpoint := strings.TrimSpace(valueString(payload["endpoint"]))
	if endpoint == "" && traceID == "" {
		return store.TraceTopology{}, traceTopology{}, fmt.Errorf("endpoint is required")
	}
	startedAt := timeFromPayload(payload["startedAt"], run.StartedAt, run.CreatedAt).Add(-30 * time.Second)
	finishedAt := timeFromPayload(payload["finishedAt"], run.FinishedAt, run.UpdatedAt, run.CreatedAt).Add(90 * time.Second)
	if finishedAt.Before(startedAt) {
		finishedAt = startedAt.Add(2 * time.Minute)
	}
	provider := graphQLTraceProvider{URL: collector.GraphQLURL}
	var topology traceTopology
	if traceID != "" {
		trace, err := provider.QueryTrace(ctx, traceID)
		if err != nil {
			return store.TraceTopology{}, traceTopology{}, err
		}
		topology = buildTraceTopology(stepID, caseID, requestID, trace)
	} else {
		candidates, err := provider.FindCandidates(ctx, endpoint, startedAt, finishedAt)
		if err != nil {
			return store.TraceTopology{}, traceTopology{}, err
		}
		var lastErr error
		for _, candidate := range candidates {
			trace, err := provider.QueryTrace(ctx, candidate.TraceID)
			if err != nil {
				lastErr = err
				continue
			}
			candidateTopology := buildTraceTopology(stepID, caseID, requestID, trace)
			if len(candidateTopology.ConfirmedEdges) > len(topology.ConfirmedEdges) || topology.SpanCount == 0 {
				topology = candidateTopology
			}
		}
		if topology.SpanCount == 0 && lastErr != nil {
			return store.TraceTopology{}, traceTopology{}, lastErr
		}
	}
	if topology.SpanCount == 0 {
		return store.TraceTopology{}, traceTopology{}, fmt.Errorf("trace provider returned no queryable traces")
	}
	raw, err := json.Marshal(topology)
	if err != nil {
		return store.TraceTopology{}, traceTopology{}, err
	}
	row, err := runtime.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            strings.TrimSpace(valueString(payload["id"])),
		WorkflowRunID: run.ID,
		WorkflowID:    run.WorkflowID,
		StepID:        stepID,
		CaseID:        caseID,
		RequestID:     topology.RequestID,
		TraceID:       topology.TraceID,
		Status:        topology.Status,
		TopologyJSON:  string(raw),
		TextTopology:  topology.TextTopology,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		return store.TraceTopology{}, traceTopology{}, err
	}
	return row, topology, nil
}

func timeFromPayload(value any, fallbacks ...time.Time) time.Time {
	if raw := strings.TrimSpace(valueString(value)); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return parsed
		}
	}
	for _, fallback := range fallbacks {
		if !fallback.IsZero() {
			return fallback
		}
	}
	return time.Now().UTC()
}
