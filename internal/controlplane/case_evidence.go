package controlplane

import (
	"errors"
	"net/http"
	"strings"

	"open-test-sandbox/internal/store"
)

func handleCaseEvidence(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
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
	records, err := runtime.ListEvidence(r.Context(), run.ID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, caseEvidencePayload(run, caseRuns[0], records))
}

func caseEvidencePayload(run store.Run, item store.APICaseRun, records []store.EvidenceRecord) map[string]any {
	request := jsonObject(item.RequestSummaryJSON)
	assertions := jsonObject(item.AssertionSummaryJSON)
	response := caseEvidenceResponse(records, item.ID)
	operation := caseRunOperation(request, item.CaseID)
	summary := map[string]any{
		"case_id":       item.CaseID,
		"operation":     operation,
		"evidence_path": run.EvidenceRoot,
		"status":        item.Status,
	}
	if code, ok := response["http_code"]; ok {
		summary["actual_http_code"] = code
	}
	if reason := caseRunFailureReason(assertions); reason != "" {
		summary["failure_reason"] = reason
	}
	assertions["passed"] = strings.EqualFold(valueString(assertions["status"]), store.StatusPassed)
	return map[string]any{
		"ok": true,
		"evidence": map[string]any{
			"summary":    summary,
			"request":    request,
			"response":   response,
			"assertions": assertions,
			"services":   []map[string]any{},
			"logs":       []map[string]any{},
			"fixture":    emptyFixtureEvidencePayload(),
			"topology":   map[string]any{},
		},
	}
}

func caseEvidenceResponse(records []store.EvidenceRecord, caseRunID string) map[string]any {
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
		response["evidence_uri"] = record.URI
		break
	}
	return response
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
