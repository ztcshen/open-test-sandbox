package controlplane

import (
	"net/http"
	"os"
	"strings"

	profilegenerateopenapi "open-test-sandbox/internal/domain/profilegenerate/openapi"
	profileimporthttpcapture "open-test-sandbox/internal/domain/profileimport/httpcapture"
	profileimportopenapi "open-test-sandbox/internal/domain/profileimport/openapi"
)

func handleOpenAPIImportPlan(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	sourcePath := strings.TrimSpace(valueString(payload["sourcePath"]))
	if sourcePath == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "sourcePath is required"})
		return
	}
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	plan, err := profileimportopenapi.Plan(raw, profileimportopenapi.Options{
		ServiceID:   valueString(payload["serviceId"]),
		EvidenceDir: valueString(payload["evidenceDir"]),
	})
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"kind":       "openapi",
		"sourcePath": sourcePath,
		"plan":       plan,
	})
}

func handleOpenAPIGenerationPlan(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	sourcePath := strings.TrimSpace(valueString(payload["sourcePath"]))
	if sourcePath == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "sourcePath is required"})
		return
	}
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	plan, err := profilegenerateopenapi.Plan(raw, profilegenerateopenapi.Options{
		ServiceID:   valueString(payload["serviceId"]),
		EvidenceDir: valueString(payload["evidenceDir"]),
	})
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"kind":       "openapi",
		"sourcePath": sourcePath,
		"plan":       plan,
	})
}

func handleHTTPCaptureImportPlan(w http.ResponseWriter, r *http.Request) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	sourcePath := strings.TrimSpace(valueString(payload["sourcePath"]))
	if sourcePath == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "sourcePath is required"})
		return
	}
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	plan, err := profileimporthttpcapture.Plan(raw, profileimporthttpcapture.Options{
		ServiceID:   valueString(payload["serviceId"]),
		EvidenceDir: valueString(payload["evidenceDir"]),
	})
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"kind":       "http-capture",
		"sourcePath": sourcePath,
		"plan":       plan,
	})
}
