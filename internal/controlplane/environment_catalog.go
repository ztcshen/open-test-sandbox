package controlplane

import (
	"errors"
	"net/http"
	"path/filepath"
	"sort"
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
		writeJSON(w, map[string]any{"ok": true, "environment": environmentAPIPayload(env), "plan": EnvironmentBootstrapPlan(env)})
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

func EnvironmentBootstrapPlan(env store.Environment) map[string]any {
	workspace := "$OTS_WORKSPACE"
	repos := environmentBootstrapRepoPlan(env, workspace)
	compose := jsonObject(env.ComposeJSON)
	healthChecks := jsonArray(env.HealthChecksJSON)
	docker := environmentBootstrapDockerPlan(compose, workspace)
	return map[string]any{
		"repos":                jsonObject(env.ReposJSON),
		"compose":              jsonObject(env.ComposeJSON),
		"healthChecks":         jsonArray(env.HealthChecksJSON),
		"verificationWorkflow": env.VerificationWorkflowID,
		"workspace":            workspace,
		"steps":                environmentBootstrapSteps(repos, docker, healthChecks, env.VerificationWorkflowID),
		"restore": map[string]any{
			"repos":        repos,
			"docker":       docker,
			"healthChecks": environmentBootstrapHealthPlan(healthChecks),
			"workflow": map[string]any{
				"action":     "run-verification-workflow",
				"workflowId": env.VerificationWorkflowID,
			},
			"pauseBeforeHeavyValidation": true,
			"notes": []string{
				"API bootstrap returns a plan only; local CLI restore executes Git, Docker, health checks, and workflow runs.",
				"Sandbox control-plane Store must already be reachable outside the restored Docker target environment.",
			},
		},
	}
}

func environmentBootstrapRepoPlan(env store.Environment, workspace string) []map[string]any {
	repoMap := jsonObject(env.ReposJSON)
	services := jsonArray(env.ServicesJSON)
	specByID := map[string]map[string]string{}
	for id, raw := range repoMap {
		item := environmentPlanMap(raw)
		specByID[strings.TrimSpace(id)] = map[string]string{
			"id":       strings.TrimSpace(id),
			"url":      strings.TrimSpace(valueString(item["url"])),
			"branch":   strings.TrimSpace(valueString(item["branch"])),
			"ref":      strings.TrimSpace(valueString(item["ref"])),
			"checkout": strings.TrimSpace(valueString(item["checkout"])),
		}
	}
	for _, raw := range services {
		item := environmentPlanMap(raw)
		id := strings.TrimSpace(valueString(item["id"]))
		if id == "" {
			continue
		}
		spec := specByID[id]
		if spec == nil {
			spec = map[string]string{"id": id}
		}
		if value := strings.TrimSpace(valueString(item["repo"])); value != "" {
			spec["url"] = value
		}
		if value := strings.TrimSpace(valueString(item["branch"])); value != "" {
			spec["branch"] = value
		}
		if value := strings.TrimSpace(valueString(item["ref"])); value != "" {
			spec["ref"] = value
		}
		if value := strings.TrimSpace(valueString(item["checkout"])); value != "" {
			spec["checkout"] = value
		}
		specByID[id] = spec
	}
	ids := make([]string, 0, len(specByID))
	for id := range specByID {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		spec := specByID[id]
		checkout := strings.TrimSpace(spec["checkout"])
		if checkout == "" {
			checkout = filepath.Join(workspace, safePlanSegment(id))
		} else if !filepath.IsAbs(checkout) && !strings.HasPrefix(checkout, "$") {
			checkout = filepath.Join(workspace, checkout)
		}
		action := "use-existing-checkout"
		command := []string{}
		if strings.TrimSpace(spec["url"]) != "" {
			action = "clone-if-missing"
			command = []string{"git", "clone"}
			if strings.TrimSpace(spec["branch"]) != "" {
				command = append(command, "--branch", strings.TrimSpace(spec["branch"]))
			}
			command = append(command, strings.TrimSpace(spec["url"]), checkout)
			if strings.TrimSpace(spec["ref"]) != "" {
				command = append(command, "&&", "git", "-C", checkout, "checkout", "--detach", strings.TrimSpace(spec["ref"]))
			}
		}
		out = append(out, map[string]any{
			"serviceId": id,
			"url":       strings.TrimSpace(spec["url"]),
			"branch":    strings.TrimSpace(spec["branch"]),
			"ref":       strings.TrimSpace(spec["ref"]),
			"checkout":  checkout,
			"action":    action,
			"command":   command,
		})
	}
	return out
}

func environmentBootstrapDockerPlan(compose map[string]any, workspace string) map[string]any {
	composeFile := strings.TrimSpace(valueString(compose["composeFile"]))
	startCommand := strings.TrimSpace(valueString(compose["startCommand"]))
	if composeFile != "" {
		if !filepath.IsAbs(composeFile) && !strings.HasPrefix(composeFile, "$") {
			composeFile = filepath.Join(workspace, composeFile)
		}
		return map[string]any{
			"action":      "docker-compose",
			"composeFile": composeFile,
			"projectName": valueString(compose["projectName"]),
			"envFiles":    stringSliceValue(compose["envFiles"]),
			"profiles":    stringSliceValue(compose["profiles"]),
			"services":    stringSliceValue(compose["services"]),
			"skipPull":    boolValue(compose["skipPull"]),
			"skipBuild":   boolValue(compose["skipBuild"]),
			"commands":    environmentBootstrapComposeCommands(compose, workspace, composeFile),
			"heavy":       true,
		}
	}
	if startCommand != "" {
		return map[string]any{
			"action":   "start-command",
			"commands": [][]string{{"/bin/sh", "-c", startCommand}},
			"heavy":    true,
		}
	}
	return map[string]any{"action": "missing-docker-plan", "commands": [][]string{}, "heavy": false}
}

func environmentBootstrapComposeCommands(compose map[string]any, workspace string, composeFile string) [][]string {
	baseArgs := []string{"-f", composeFile}
	if projectName := strings.TrimSpace(valueString(compose["projectName"])); projectName != "" {
		baseArgs = append(baseArgs, "-p", projectName)
	}
	for _, envFile := range stringSliceValue(compose["envFiles"]) {
		if !filepath.IsAbs(envFile) && !strings.HasPrefix(envFile, "$") {
			envFile = filepath.Join(workspace, envFile)
		}
		baseArgs = append(baseArgs, "--env-file", envFile)
	}
	for _, profile := range stringSliceValue(compose["profiles"]) {
		baseArgs = append(baseArgs, "--profile", profile)
	}
	services := stringSliceValue(compose["services"])
	out := [][]string{}
	if !boolValue(compose["skipPull"]) {
		out = append(out, append(append([]string{"docker", "compose"}, baseArgs...), append([]string{"pull"}, services...)...))
	}
	if !boolValue(compose["skipBuild"]) {
		out = append(out, append(append([]string{"docker", "compose"}, baseArgs...), append([]string{"build"}, services...)...))
	}
	out = append(out, append(append([]string{"docker", "compose"}, baseArgs...), append([]string{"up", "-d"}, services...)...))
	return out
}

func stringSliceValue(value any) []string {
	if typed, ok := value.([]string); ok {
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	}
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if value := strings.TrimSpace(valueString(item)); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func environmentBootstrapHealthPlan(checks []any) []map[string]any {
	out := make([]map[string]any, 0, len(checks))
	for _, raw := range checks {
		item := environmentPlanMap(raw)
		kind := strings.TrimSpace(valueString(item["kind"]))
		if kind == "" && strings.TrimSpace(valueString(item["url"])) != "" {
			kind = "url"
		}
		if kind == "" {
			continue
		}
		plan := map[string]any{
			"id":     strings.TrimSpace(valueString(item["id"])),
			"kind":   kind,
			"expect": environmentBootstrapHealthExpectation(kind),
		}
		for _, field := range []string{"url", "address", "command", "service"} {
			if value := strings.TrimSpace(valueString(item[field])); value != "" {
				plan[field] = value
			}
		}
		if kind == "url" {
			plan["method"] = "GET"
		}
		out = append(out, plan)
	}
	return out
}

func environmentBootstrapHealthExpectation(kind string) string {
	switch kind {
	case "url":
		return "2xx"
	case "tcp":
		return "connect"
	case "command":
		return "exit 0"
	case "compose-service":
		return "running and healthy if health is reported"
	default:
		return "supported health check"
	}
}

func environmentBootstrapSteps(repos []map[string]any, docker map[string]any, healthChecks []any, workflowID string) []map[string]any {
	steps := make([]map[string]any, 0, len(repos)+3)
	for _, repo := range repos {
		steps = append(steps, map[string]any{
			"kind":      "repository",
			"serviceId": repo["serviceId"],
			"action":    repo["action"],
			"command":   repo["command"],
		})
	}
	steps = append(steps, map[string]any{
		"kind":     "docker",
		"action":   docker["action"],
		"commands": docker["commands"],
		"heavy":    docker["heavy"],
	})
	steps = append(steps, map[string]any{
		"kind":   "health-checks",
		"action": "wait",
		"checks": environmentBootstrapHealthPlan(healthChecks),
	})
	steps = append(steps, map[string]any{
		"kind":       "verification-workflow",
		"action":     "run",
		"workflowId": workflowID,
	})
	return steps
}

func safePlanSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "service"
	}
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-").Replace(value)
}

func environmentPlanMap(value any) map[string]any {
	item, _ := value.(map[string]any)
	if item == nil {
		return map[string]any{}
	}
	return item
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
