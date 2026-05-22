package controlplane

import (
	"net/http"
	"strings"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/domain/workflowaudit"
	"open-test-sandbox/internal/store"
)

func handleWorkflowAudit(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflowId"))
	if workflowID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "workflowId is required"})
		return
	}
	if !bundleHasWorkflow(bundle, workflowID) {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow not found"})
		return
	}
	report, err := workflowaudit.Audit(r.Context(), workflowaudit.Options{
		Bundle:     bundle,
		WorkflowID: workflowID,
		Store:      runtime,
	})
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func bundleHasWorkflow(bundle profile.Bundle, workflowID string) bool {
	for _, workflow := range bundle.Workflows {
		if workflow.ID == workflowID {
			return true
		}
	}
	return false
}
