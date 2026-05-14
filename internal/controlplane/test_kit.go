package controlplane

import (
	"encoding/json"
	"net/http"
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func handleTestKitRun(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	result, status := testKitCaseResult(bundle, payload)
	if status == http.StatusOK {
		if err := recordTestKitRun(r, bundle, runtime, payload, result); err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	writeJSONStatus(w, status, result)
}

func handleTestKitRunBatch(w http.ResponseWriter, r *http.Request, bundle profile.Bundle) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	caseIDs := testKitCaseIDs(payload["caseIds"])
	if len(caseIDs) == 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "caseIds are required"})
		return
	}

	results := make([]map[string]any, 0, len(caseIDs))
	passed := 0
	started := time.Now()
	for _, caseID := range caseIDs {
		itemPayload := map[string]any{
			"caseId": caseID,
			"dryRun": boolValue(payload["dryRun"]),
		}
		result, _ := testKitCaseResult(bundle, itemPayload)
		if result["ok"] == true {
			passed++
		}
		results = append(results, result)
	}
	writeJSON(w, map[string]any{
		"ok":        passed == len(results),
		"results":   results,
		"elapsedMs": time.Since(started).Milliseconds(),
		"summary": map[string]any{
			"caseCount": len(results),
			"passed":    passed,
			"failed":    len(results) - passed,
		},
	})
}

func testKitCaseResult(bundle profile.Bundle, payload map[string]any) (map[string]any, int) {
	started := time.Now()
	caseID := valueString(payload["caseId"])
	if caseID == "" {
		return map[string]any{"ok": false, "error": "caseId is required", "code": http.StatusBadRequest}, http.StatusBadRequest
	}
	item, ok := findAPICase(bundle.APICases, caseID)
	if !ok {
		return map[string]any{
			"ok":     false,
			"caseId": caseID,
			"status": store.StatusFailed,
			"error":  "api case not found",
			"code":   http.StatusNotFound,
		}, http.StatusNotFound
	}

	dryRun := boolValue(payload["dryRun"])
	runOK := dryRun
	status := store.StatusPassed
	failureReason := ""
	if !runOK {
		status = store.StatusFailed
		failureReason = "api case execution adapter is not configured"
	}
	stepID := valueString(payload["stepId"])
	result := map[string]any{
		"ok":        runOK,
		"caseId":    item.ID,
		"title":     firstNonEmpty(item.DisplayName, item.ID),
		"stepId":    stepID,
		"status":    status,
		"dryRun":    dryRun,
		"elapsedMs": time.Since(started).Milliseconds(),
		"summary": map[string]any{
			"caseId":        item.ID,
			"stepId":        stepID,
			"dryRun":        dryRun,
			"failureReason": failureReason,
		},
		"result": map[string]any{
			"request":  map[string]any{"caseId": item.ID},
			"response": map[string]any{"body": "{}"},
		},
	}
	if failureReason != "" {
		result["error"] = failureReason
	}
	return result, http.StatusOK
}

func recordTestKitRun(r *http.Request, bundle profile.Bundle, runtime store.Store, payload map[string]any, result map[string]any) error {
	if runtime == nil {
		return nil
	}
	status := store.StatusFailed
	if result["ok"] == true {
		status = store.StatusPassed
	}
	workflowID := firstNonEmpty(valueString(payload["workflowId"]), valueString(result["caseId"]))
	summary := map[string]any{
		"summary": result["summary"],
		"steps":   []map[string]any{result},
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = runtime.CreateRun(r.Context(), store.Run{
		ID:           workflowRunID(now),
		ProfileID:    bundle.ID,
		WorkflowID:   workflowID,
		Status:       status,
		EvidenceRoot: "",
		SummaryJSON:  string(raw),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	return err
}

func findAPICase(items []profile.APICase, id string) (profile.APICase, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return profile.APICase{}, false
}

func testKitCaseIDs(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if id := valueString(item); id != "" {
				out = append(out, id)
			}
		}
		return out
	case []string:
		return typed
	default:
		return nil
	}
}

func boolValue(value any) bool {
	typed, _ := value.(bool)
	return typed
}
