package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"agent-testbench/internal/domain/redaction"
	"agent-testbench/internal/store"
)

var ErrCaseEvidenceNotFound = errors.New("case evidence not found")

func handleCaseEvidence(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	if caseRunID := strings.TrimSpace(r.URL.Query().Get("caseRunId")); caseRunID != "" {
		writeCaseEvidenceForCaseRunID(w, r, runtime, caseRunID)
		return
	}
	runID := strings.TrimSpace(r.URL.Query().Get("runId"))
	if runID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "runId is required"})
		return
	}
	run, err := runtime.GetRun(r.Context(), runID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	caseRuns, err := runtime.ListAPICaseRuns(r.Context(), run.ID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(caseRuns) == 0 {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	selected, ok := selectCaseEvidenceRun(caseRuns, "", r.URL.Query().Get("caseId"), r.URL.Query().Get("stepId"), run.SummaryJSON)
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	writeCaseEvidencePayload(w, r, runtime, run, selected, caseRuns)
}

func handleCaseRunEvidence(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	caseRunID := strings.TrimSpace(r.URL.Query().Get("caseRunId"))
	if caseRunID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "caseRunId is required"})
		return
	}
	writeCaseEvidenceForCaseRunID(w, r, runtime, caseRunID)
}

func writeCaseEvidenceForCaseRunID(w http.ResponseWriter, r *http.Request, runtime store.Store, caseRunID string) {
	payload, ok, err := CaseEvidencePayloadForCaseRunID(r.Context(), runtime, caseRunID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	writeJSON(w, payload)
}

func writeCaseEvidencePayload(w http.ResponseWriter, r *http.Request, runtime store.Store, run store.Run, selected store.APICaseRun, caseRuns []store.APICaseRun) {
	records, err := runtime.ListEvidence(r.Context(), run.ID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	catalog, catalogErr := runtime.GetProfileCatalog(r.Context())
	if catalogErr != nil {
		catalog = store.ProfileCatalog{}
	}
	topologies, err := runtime.ListTraceTopologies(r.Context(), run.ID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, caseEvidencePayload(run, selected, caseRuns, records, catalog, topologies))
}

func CaseEvidencePayloadForCaseRunID(ctx context.Context, runtime store.Store, caseRunID string) (map[string]any, bool, error) {
	run, selected, caseRuns, ok, err := findCaseEvidenceRunByCaseRunID(ctx, runtime, caseRunID)
	if err != nil || !ok {
		return nil, ok, err
	}
	return CaseEvidencePayloadForRun(ctx, runtime, run, selected, caseRuns)
}

func CaseEvidencePayloadForRunID(ctx context.Context, runtime store.Store, runID string, caseID string, stepID string) (map[string]any, bool, error) {
	run, err := runtime.GetRun(ctx, strings.TrimSpace(runID))
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
	if err != nil {
		return nil, false, err
	}
	selected, ok := selectCaseEvidenceRun(caseRuns, "", caseID, stepID, run.SummaryJSON)
	if !ok {
		return nil, false, nil
	}
	return CaseEvidencePayloadForRun(ctx, runtime, run, selected, caseRuns)
}

func CaseEvidencePayloadForRun(ctx context.Context, runtime store.Store, run store.Run, selected store.APICaseRun, caseRuns []store.APICaseRun) (map[string]any, bool, error) {
	records, err := runtime.ListEvidence(ctx, run.ID)
	if err != nil {
		return nil, false, err
	}
	catalog, catalogErr := runtime.GetProfileCatalog(ctx)
	if catalogErr != nil {
		catalog = store.ProfileCatalog{}
	}
	topologies, err := runtime.ListTraceTopologies(ctx, run.ID)
	if err != nil {
		return nil, false, err
	}
	return caseEvidencePayload(run, selected, caseRuns, records, catalog, topologies), true, nil
}

func findCaseEvidenceRunByCaseRunID(ctx context.Context, runtime store.Store, caseRunID string) (store.Run, store.APICaseRun, []store.APICaseRun, bool, error) {
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return store.Run{}, store.APICaseRun{}, nil, false, err
	}
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
		if err != nil {
			return store.Run{}, store.APICaseRun{}, nil, false, err
		}
		selected, ok := selectCaseEvidenceRun(caseRuns, caseRunID, "", "", run.SummaryJSON)
		if ok {
			return run, selected, caseRuns, true, nil
		}
	}
	return store.Run{}, store.APICaseRun{}, nil, false, nil
}

func selectCaseEvidenceRun(caseRuns []store.APICaseRun, caseRunID string, caseID string, stepID string, summaryJSON string) (store.APICaseRun, bool) {
	caseRunID = strings.TrimSpace(caseRunID)
	if caseRunID != "" {
		for _, item := range caseRuns {
			if item.ID == caseRunID {
				return item, true
			}
		}
		return store.APICaseRun{}, false
	}
	caseID = strings.TrimSpace(caseID)
	stepID = strings.TrimSpace(stepID)
	if caseID == "" && stepID != "" {
		caseID = caseIDForWorkflowStep(summaryJSON, stepID)
	}
	if caseID == "" {
		return caseRuns[0], true
	}
	for _, item := range caseRuns {
		if item.CaseID == caseID {
			return item, true
		}
	}
	return store.APICaseRun{}, false
}

func caseIDForWorkflowStep(summaryJSON string, stepID string) string {
	summary := jsonObject(summaryJSON)
	for _, raw := range workflowRunSteps(summary) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(valueString(step["stepId"])) == stepID {
			return strings.TrimSpace(valueString(step["caseId"]))
		}
	}
	return ""
}

func caseEvidencePayload(run store.Run, item store.APICaseRun, caseRuns []store.APICaseRun, records []store.EvidenceRecord, catalog store.ProfileCatalog, topologies []store.TraceTopology) map[string]any {
	request := caseEvidenceRequest(records, item.ID, run.EvidenceRoot, jsonObject(item.RequestSummaryJSON))
	assertions := jsonObject(item.AssertionSummaryJSON)
	response := caseEvidenceResponse(records, item.ID, run.EvidenceRoot)
	saved := jsonObject(run.SummaryJSON)
	trace := mapFromAny(saved["trace"])
	fixture := caseEvidenceSeedData(item.CaseID, catalog, run, caseRuns)
	logs := caseEvidenceLogs(records, item, run.EvidenceRoot)
	topology := topologyEvidenceViewForCase(topologyEvidenceViewInput{
		RunID:        run.ID,
		CaseID:       item.CaseID,
		SavedSummary: saved,
		Rows:         topologies,
	})
	operation := caseRunOperation(request, item.CaseID)
	summary := map[string]any{
		"case_id":       item.CaseID,
		"case_run_id":   item.ID,
		"run_id":        run.ID,
		"workflow_id":   run.WorkflowID,
		"operation":     operation,
		"evidence_path": run.EvidenceRoot,
		"status":        item.Status,
	}
	if stepID := firstNonEmpty(valueString(request["stepId"]), valueString(jsonObject(item.RequestSummaryJSON)["stepId"])); stepID != "" {
		summary["step_id"] = stepID
	}
	if code, ok := response["http_code"]; ok {
		summary["actual_http_code"] = code
	}
	if reason := caseRunFailureReason(assertions); reason != "" {
		summary["failure_reason"] = reason
	}
	assertions["passed"] = strings.EqualFold(valueString(assertions["status"]), store.StatusPassed)
	evidence := map[string]any{
		"ok": true,
		"evidence": map[string]any{
			"summary":                     summary,
			"trace":                       trace,
			"request":                     request,
			"response":                    response,
			apiCaseEvidenceKindAssertions: assertions,
			"services":                    []map[string]any{},
			"logs":                        logs,
			"mysql":                       map[string]any{"ok": true, "queries": []map[string]any{}},
			"fixture":                     fixture,
			topologyPayloadField:          topology,
		},
	}
	evidence["evidence"] = redaction.Value(evidence["evidence"])
	return evidence
}

func caseEvidenceLogs(records []store.EvidenceRecord, item store.APICaseRun, evidenceRoot string) []map[string]any {
	request := jsonObject(item.RequestSummaryJSON)
	stepID := strings.TrimSpace(valueString(request["stepId"]))
	out := []map[string]any{}
	for _, record := range records {
		if !caseEvidenceLogRecordMatches(record, item, stepID) {
			continue
		}
		summary := jsonObject(record.Summary)
		payload := map[string]any{
			"id":        record.ID,
			"kind":      record.Kind,
			"uri":       record.URI,
			"mediaType": record.MediaType,
			"summary":   summary,
		}
		if attachment := evidenceAttachmentMetadata(record, evidenceRoot); len(attachment) > 0 {
			payload["attachment"] = attachment
		}
		if !record.CreatedAt.IsZero() {
			payload["createdAt"] = record.CreatedAt
		}
		if body, ok := evidenceRecordObject(record, evidenceRoot); ok {
			if systems := listFromAny(body["systems"]); len(systems) > 0 {
				payload["systems"] = systems
			} else if lines := listFromAny(body["lines"]); len(lines) > 0 {
				payload["lines"] = lines
			} else {
				payload["body"] = body
			}
		}
		out = append(out, payload)
	}
	return out
}

func caseEvidenceLogRecordMatches(record store.EvidenceRecord, item store.APICaseRun, stepID string) bool {
	switch strings.ToLower(strings.TrimSpace(record.Kind)) {
	case workflowStepRuntimeLogsKind, "logs", "log", "runtime-log", "runtime-logs", "elk_logs":
	default:
		return false
	}
	if record.CaseRunID == item.ID || record.CaseRunID == item.CaseID {
		return true
	}
	if stepID != "" && (record.CaseRunID == stepID || evidenceRecordStepID(record) == stepID) {
		return true
	}
	summary := jsonObject(record.Summary)
	if valueString(summary["caseId"]) == item.CaseID {
		return true
	}
	return stepID != "" && valueString(summary["stepId"]) == stepID
}

func mapFromAny(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case nil:
		return map[string]any{}
	default:
		return map[string]any{}
	}
}

func caseEvidenceRequest(records []store.EvidenceRecord, caseRunID string, evidenceRoot string, defaultValue map[string]any) map[string]any {
	request := copyMap(defaultValue)
	for _, record := range records {
		if record.CaseRunID != caseRunID || record.Kind != "request" {
			continue
		}
		if body, ok := evidenceRecordObject(record, evidenceRoot); ok {
			for key, value := range body {
				request[key] = value
			}
		}
		request["evidence_uri"] = record.URI
		if attachment := evidenceAttachmentMetadata(record, evidenceRoot); len(attachment) > 0 {
			request["attachment"] = attachment
		}
		break
	}
	return request
}

func caseEvidenceResponse(records []store.EvidenceRecord, caseRunID string, evidenceRoot string) map[string]any {
	response := map[string]any{}
	for _, record := range records {
		if record.CaseRunID != caseRunID || record.Kind != "response" {
			continue
		}
		summary := jsonObject(record.Summary)
		if code, ok := summary["statusCode"]; ok {
			response["http_code"] = code
		}
		if bytes, ok := summary["bodyBytes"]; ok {
			response["body_bytes"] = bytes
		}
		if body, ok := evidenceRecordObject(record, evidenceRoot); ok {
			if code, ok := body["statusCode"]; ok {
				response["http_code"] = code
			}
			if headers, ok := body["headers"]; ok {
				response["headers"] = headers
			}
			if responseBody, ok := body["body"]; ok {
				response["body"] = responseBody
			}
		}
		response["evidence_uri"] = record.URI
		if attachment := evidenceAttachmentMetadata(record, evidenceRoot); len(attachment) > 0 {
			response["attachment"] = attachment
		}
		break
	}
	return response
}

func evidenceAttachmentMetadata(record store.EvidenceRecord, evidenceRoot string) map[string]any {
	out := map[string]any{
		"id":        record.ID,
		"runId":     record.RunID,
		"caseRunId": record.CaseRunID,
		"kind":      record.Kind,
		"uri":       record.URI,
	}
	if stepID := evidenceRecordStepID(record); stepID != "" {
		out["stepId"] = stepID
	}
	if strings.TrimSpace(record.MediaType) != "" {
		out["mediaType"] = record.MediaType
	}
	if strings.TrimSpace(record.SHA256) != "" {
		out["sha256"] = record.SHA256
	}
	if record.SizeBytes > 0 {
		out["sizeBytes"] = record.SizeBytes
	}
	if strings.TrimSpace(record.Category) != "" {
		out["category"] = record.Category
	}
	if strings.TrimSpace(record.Visibility) != "" {
		out["visibility"] = record.Visibility
	}
	labels := jsonObject(record.LabelsJSON)
	if len(labels) > 0 {
		out["labels"] = labels
	}
	if lifecycle := evidenceRecordLifecycle(record, evidenceRoot); len(lifecycle) > 0 {
		out["lifecycle"] = lifecycle
	}
	return out
}

func evidenceRecordLifecycle(record store.EvidenceRecord, evidenceRoot string) map[string]any {
	uri := strings.TrimSpace(record.URI)
	if uri == "" || !evidenceURIIsLocalFile(uri) {
		return nil
	}
	path := evidenceResolvedLocalFilePath(uri, evidenceRoot)
	out := map[string]any{
		"kind": "local-file",
		"path": path,
	}
	if _, err := os.Stat(path); err == nil {
		out[evidenceLifecycleAvailable] = true
		out["state"] = evidenceLifecycleAvailable
		return out
	} else if errors.Is(err, os.ErrNotExist) {
		out[evidenceLifecycleAvailable] = false
		out["state"] = "missing"
		out["nextAction"] = "Rerun the case with --evidence-dir pointing to a durable directory, or copy/export local Evidence before temporary files are cleaned up."
		return out
	} else {
		out[evidenceLifecycleAvailable] = false
		out["state"] = "unreadable"
		out["error"] = err.Error()
		out["nextAction"] = "Check local file permissions, then rerun the case with --evidence-dir pointing to a durable directory if the file cannot be recovered."
		return out
	}
}

const evidenceLifecycleAvailable = "available"

func evidenceURIIsLocalFile(uri string) bool {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return false
	}
	if strings.HasPrefix(uri, "file://") {
		return true
	}
	return !strings.Contains(uri, "://")
}

func evidenceLocalFilePath(uri string) string {
	return strings.TrimPrefix(strings.TrimSpace(uri), "file://")
}

func evidenceResolvedLocalFilePath(uri string, evidenceRoot string) string {
	path := filepath.Clean(evidenceLocalFilePath(uri))
	if filepath.IsAbs(path) {
		return path
	}
	root := strings.TrimSpace(evidenceRoot)
	if root == "" {
		return path
	}
	root = filepath.Clean(evidenceLocalFilePath(root))
	if root == "." || root == "" {
		return path
	}
	if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return path
	}
	return filepath.Join(root, path)
}

func evidenceRecordObject(record store.EvidenceRecord, evidenceRoot string) (map[string]any, bool) {
	path := record.URI
	if evidenceURIIsLocalFile(path) {
		path = evidenceResolvedLocalFilePath(path, evidenceRoot)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, false
	}
	return body, true
}

func copyMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func emptyFixtureEvidencePayload() map[string]any {
	return map[string]any{
		"status":    "empty",
		"applyRuns": []map[string]any{},
		"summary": map[string]any{
			"applyCount":   0,
			"restoreCount": 0,
			"failedCount":  0,
		},
	}
}
