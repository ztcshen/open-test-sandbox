package controlplane

import (
	"net/http"
	"strings"
)

func handleReplayEvidence(w http.ResponseWriter, r *http.Request) {
	traceID := strings.TrimSpace(r.URL.Query().Get("traceId"))
	if traceID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "traceId is required"})
		return
	}
	writeJSON(w, map[string]any{
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
	})
}
