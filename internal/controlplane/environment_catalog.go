package controlplane

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"open-test-sandbox/internal/store"
)

func handleEnvironmentCollection(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "runtime Store is not configured"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := runtime.ListEnvironments(r.Context())
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		includeAll := strings.EqualFold(r.URL.Query().Get("all"), "true") || r.URL.Query().Get("all") == "1"
		filtered := make([]store.Environment, 0, len(items))
		for _, item := range items {
			if includeAll || item.Verified {
				filtered = append(filtered, item)
			}
		}
		writeJSON(w, map[string]any{"ok": true, "count": len(filtered), "items": environmentAPIPayloads(filtered)})
	case http.MethodPost:
		payload, err := readJSONPayload(r)
		if err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		env, err := environmentFromAPIPayload(payload)
		if err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		env, err = runtime.UpsertEnvironment(r.Context(), env)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "environment": environmentAPIPayload(env)})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleEnvironmentItem(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "runtime Store is not configured"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/environments/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "environment id is required"})
		return
	}
	id := strings.TrimSpace(parts[0])
	action := ""
	if len(parts) > 1 {
		action = strings.TrimSpace(parts[1])
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		env, ok := loadEnvironmentAPI(w, r, runtime, id)
		if !ok {
			return
		}
		writeJSON(w, map[string]any{"ok": true, "environment": environmentAPIPayload(env)})
	case action == "bootstrap" && r.Method == http.MethodGet:
		env, ok := loadEnvironmentAPI(w, r, runtime, id)
		if !ok {
			return
		}
		writeJSON(w, map[string]any{"ok": true, "environment": environmentAPIPayload(env), "plan": environmentBootstrapPlan(env)})
	case action == "verify" && r.Method == http.MethodPost:
		handleEnvironmentVerifyAPI(w, r, runtime, id)
	case action == "publish-verified" && r.Method == http.MethodPost:
		handleEnvironmentPublishVerifiedAPI(w, r, runtime, id)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleEnvironmentVerifyAPI(w http.ResponseWriter, r *http.Request, runtime store.Store, id string) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	runID := strings.TrimSpace(firstNonEmpty(valueString(payload["runId"]), valueString(payload["run"])))
	status := strings.TrimSpace(valueString(payload["status"]))
	if runID == "" || status == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "runId and status are required"})
		return
	}
	env, ok := loadEnvironmentAPI(w, r, runtime, id)
	if !ok {
		return
	}
	env.LastVerificationRunID = runID
	env.LastVerificationStatus = status
	env.EvidenceComplete = boolValue(payload["evidenceComplete"])
	env.TopologyComplete = boolValue(payload["topologyComplete"])
	env.Verified = false
	env.Status = "verification-recorded"
	if env.LastVerificationStatus == store.StatusPassed && env.EvidenceComplete && env.TopologyComplete {
		env.Status = "verified-ready"
		env.LastVerifiedAt = time.Now().UTC()
	}
	env, err = runtime.UpsertEnvironment(r.Context(), env)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "environment": environmentAPIPayload(env)})
}

func handleEnvironmentPublishVerifiedAPI(w http.ResponseWriter, r *http.Request, runtime store.Store, id string) {
	env, ok := loadEnvironmentAPI(w, r, runtime, id)
	if !ok {
		return
	}
	if err := ValidateEnvironmentPublishable(r.Context(), runtime, env); err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	env.Verified = true
	env.Status = "verified"
	if env.LastVerifiedAt.IsZero() {
		env.LastVerifiedAt = time.Now().UTC()
	}
	env, err := runtime.UpsertEnvironment(r.Context(), env)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "environment": environmentAPIPayload(env)})
}

func loadEnvironmentAPI(w http.ResponseWriter, r *http.Request, runtime store.Store, id string) (store.Environment, bool) {
	env, err := runtime.GetEnvironment(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "environment not found"})
			return store.Environment{}, false
		}
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return store.Environment{}, false
	}
	return env, true
}

func environmentFromAPIPayload(payload map[string]any) (store.Environment, error) {
	id := strings.TrimSpace(valueString(payload["id"]))
	if id == "" {
		return store.Environment{}, errors.New("id is required")
	}
	return store.Environment{
		ID:                     id,
		DisplayName:            strings.TrimSpace(valueString(payload["displayName"])),
		Description:            strings.TrimSpace(valueString(payload["description"])),
		Status:                 firstNonEmpty(strings.TrimSpace(valueString(payload["status"])), "draft"),
		ServicesJSON:           compactJSON(defaultJSONArray(payload["services"])),
		ReposJSON:              compactJSON(defaultJSONObject(payload["repos"])),
		ComposeJSON:            compactJSON(defaultJSONObject(payload["compose"])),
		HealthChecksJSON:       compactJSON(defaultJSONArray(payload["healthChecks"])),
		VerificationWorkflowID: strings.TrimSpace(valueString(payload["verificationWorkflowId"])),
		SummaryJSON:            compactJSON(defaultJSONObject(payload["summary"])),
	}, nil
}

func environmentAPIPayloads(items []store.Environment) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, environmentAPIPayload(item))
	}
	return out
}

func environmentAPIPayload(env store.Environment) map[string]any {
	payload := map[string]any{
		"id":                     env.ID,
		"displayName":            env.DisplayName,
		"description":            env.Description,
		"status":                 env.Status,
		"verified":               env.Verified,
		"services":               jsonArray(env.ServicesJSON),
		"repos":                  jsonObject(env.ReposJSON),
		"compose":                jsonObject(env.ComposeJSON),
		"healthChecks":           jsonArray(env.HealthChecksJSON),
		"verificationWorkflowId": env.VerificationWorkflowID,
		"lastVerificationRunId":  env.LastVerificationRunID,
		"lastVerificationStatus": env.LastVerificationStatus,
		"evidenceComplete":       env.EvidenceComplete,
		"topologyComplete":       env.TopologyComplete,
		"summary":                jsonObject(env.SummaryJSON),
		"createdAt":              env.CreatedAt,
		"updatedAt":              env.UpdatedAt,
	}
	if !env.LastVerifiedAt.IsZero() {
		payload["lastVerifiedAt"] = env.LastVerifiedAt
	}
	return payload
}

func environmentBootstrapPlan(env store.Environment) map[string]any {
	return map[string]any{
		"repos":                jsonObject(env.ReposJSON),
		"compose":              jsonObject(env.ComposeJSON),
		"healthChecks":         jsonArray(env.HealthChecksJSON),
		"verificationWorkflow": env.VerificationWorkflowID,
	}
}

func defaultJSONObject(value any) any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func defaultJSONArray(value any) any {
	if value == nil {
		return []any{}
	}
	return value
}
