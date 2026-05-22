package controlplane

import (
	"errors"
	"net/http"
	"strings"
)

func ReplayEvidencePayload(traceID string) (map[string]any, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil, errors.New("traceId is required")
	}
	return map[string]any{
		"ok": true,
		"run": map[string]any{
			"traceId":     traceID,
			"httpStatus":  "",
			"summaryJson": "{}",
		},
		"evidence": map[string]any{
			"traceId": traceID,
			"request": map[string]any{
				"method":      "",
				"targetUrl":   "",
				"bodySummary": "",
			},
			"response": map[string]any{
				"httpStatus":  "",
				"bodySummary": "",
			},
			"systems": []map[string]any{},
		},
	}, nil
}

func handleReplayEvidence(w http.ResponseWriter, r *http.Request) {
	payload, err := ReplayEvidencePayload(r.URL.Query().Get("traceId"))
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, payload)
}
