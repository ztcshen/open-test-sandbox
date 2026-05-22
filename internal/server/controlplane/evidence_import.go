package controlplane

import (
	"encoding/json"
	"net/http"
	"strings"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/runner/evidence"
	"agent-testbench/internal/store"
)

type EvidenceImportRequest struct {
	SourcePath string `json:"sourcePath"`
	ProfileID  string `json:"profileId"`
}

type EvidenceImportPayload struct {
	OK              bool   `json:"ok"`
	SourcePath      string `json:"sourcePath"`
	ProfileID       string `json:"profileId"`
	RunCount        int    `json:"runCount"`
	APICaseRunCount int    `json:"apiCaseRunCount"`
	EvidenceCount   int    `json:"evidenceCount"`
}

func handleEvidenceImport(w http.ResponseWriter, r *http.Request, runtime store.Store, bundle profile.Bundle) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "runtime store is not configured"})
		return
	}
	var req EvidenceImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	profileID := strings.TrimSpace(req.ProfileID)
	if profileID == "" {
		profileID = strings.TrimSpace(bundle.ID)
	}
	result, err := evidence.ImportLegacyRuntime(r.Context(), evidence.ImportOptions{
		SourcePath: req.SourcePath,
		ProfileID:  profileID,
		Store:      runtime,
	})
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, EvidenceImportPayload{
		OK:              true,
		SourcePath:      req.SourcePath,
		ProfileID:       profileID,
		RunCount:        result.RunCount,
		APICaseRunCount: result.APICaseRunCount,
		EvidenceCount:   result.EvidenceCount,
	})
}
