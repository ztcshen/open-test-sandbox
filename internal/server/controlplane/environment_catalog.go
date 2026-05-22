package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
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
		if err := validateEnvironmentVerificationWorkflow(env); err != nil {
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

func handleEnvironmentItem(w http.ResponseWriter, r *http.Request, runtime store.Store, bundle profile.Bundle, runner *apiCaseBatchRunner, collector traceCollector) {
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
	if action == "acceptance-runs" {
		handleEnvironmentAcceptanceRuns(w, r, runtime, bundle, runner, collector, id, parts)
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		env, ok := loadEnvironmentAPI(w, r, runtime, id)
		if !ok {
			return
		}
		componentGraph, graphOK := loadEnvironmentComponentGraphAPI(w, r, runtime, id)
		if !graphOK {
			return
		}
		writeJSON(w, map[string]any{"ok": true, "environment": environmentAPIPayload(env), "componentGraph": EnvironmentComponentGraphReadinessReport(env.ID, componentGraph)})
	case action == "bootstrap" && r.Method == http.MethodGet:
		env, ok := loadEnvironmentAPI(w, r, runtime, id)
		if !ok {
			return
		}
		componentGraph, graphOK := loadEnvironmentComponentGraphAPI(w, r, runtime, id)
		if !graphOK {
			return
		}
		plan := EnvironmentBootstrapPlan(env)
		componentReadiness := EnvironmentComponentGraphReadinessReport(env.ID, componentGraph)
		componentStartupPlan := EnvironmentComponentStartupPlanReport(env.ID, componentGraph)
		plan["componentGraph"] = componentReadiness
		plan["componentStartupPlan"] = componentStartupPlan
		if restorePlan, ok := plan["restore"].(map[string]any); ok {
			restorePlan["componentGraph"] = componentReadiness
			restorePlan["componentStartupPlan"] = componentStartupPlan
		}
		writeJSON(w, map[string]any{"ok": true, "environment": environmentAPIPayload(env), "plan": plan})
	case action == "verify" && r.Method == http.MethodPost:
		handleEnvironmentVerifyAPI(w, r, runtime, id)
	case action == "publish-verified" && r.Method == http.MethodPost:
		handleEnvironmentPublishVerifiedAPI(w, r, runtime, id)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleEnvironmentAcceptanceRuns(w http.ResponseWriter, r *http.Request, runtime store.Store, bundle profile.Bundle, runner *apiCaseBatchRunner, collector traceCollector, id string, parts []string) {
	switch {
	case len(parts) == 2 && r.Method == http.MethodPost:
		handleEnvironmentAcceptanceRunStart(w, r, runtime, bundle, runner, collector, id)
	case len(parts) == 3 && r.Method == http.MethodGet:
		handleEnvironmentAcceptanceRunReport(w, r, runtime, runner, id, strings.TrimSpace(parts[2]))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleEnvironmentAcceptanceRunStart(w http.ResponseWriter, r *http.Request, runtime store.Store, bundle profile.Bundle, runner *apiCaseBatchRunner, collector traceCollector, id string) {
	env, ok := loadEnvironmentAPI(w, r, runtime, id)
	if !ok {
		return
	}
	if err := validateEnvironmentVerificationWorkflow(env); err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	requestID := strings.TrimSpace(valueString(payload["requestId"]))
	if requestID == "" {
		requestID = "env-acceptance-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	request := apiCaseBatchRunRequest{
		RequestID:      requestID,
		EnvironmentID:  env.ID,
		WorkflowID:     env.VerificationWorkflowID,
		BaseURL:        strings.TrimSpace(valueString(payload["baseUrl"])),
		EvidenceDir:    strings.TrimSpace(valueString(payload["evidenceDir"])),
		TimeoutSeconds: intValue(payload["timeoutSeconds"]),
		Overrides:      mapValue(payload["overrides"]),
	}
	report, status, err := startAPICaseBatchRun(r.Context(), bundle, runtime, runner, request, collector)
	if err != nil {
		writeJSONStatus(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	report.ReportURL = "/api/environments/" + url.PathEscape(env.ID) + "/acceptance-runs/" + url.PathEscape(report.BatchRunID)
	writeJSONStatus(w, http.StatusAccepted, environmentAcceptanceRunPayload(env.ID, report))
}

func handleEnvironmentAcceptanceRunReport(w http.ResponseWriter, r *http.Request, runtime store.Store, runner *apiCaseBatchRunner, environmentID string, batchRunID string) {
	if report, ok := runner.get(batchRunID); ok && report.EnvironmentID == environmentID {
		writeJSON(w, environmentAcceptanceRunPayload(environmentID, report))
		return
	}
	report, ok := storedEnvironmentAcceptanceRunReport(r.Context(), runtime, environmentID, batchRunID)
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "environment acceptance run not found"})
		return
	}
	writeJSON(w, environmentAcceptanceRunPayload(environmentID, report))
}

func storedEnvironmentAcceptanceRunReport(ctx context.Context, runtime store.Store, environmentID string, batchRunID string) (apiCaseBatchRunReport, bool) {
	if runtime == nil || strings.TrimSpace(environmentID) == "" || strings.TrimSpace(batchRunID) == "" {
		return apiCaseBatchRunReport{}, false
	}
	run, err := runtime.GetRun(ctx, batchRunID)
	if err != nil {
		return apiCaseBatchRunReport{}, false
	}
	var report apiCaseBatchRunReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(run.SummaryJSON)), &report); err != nil {
		return apiCaseBatchRunReport{}, false
	}
	if report.BatchRunID == "" {
		report.BatchRunID = run.ID
	}
	if report.WorkflowID == "" {
		report.WorkflowID = run.WorkflowID
	}
	if report.EnvironmentID != environmentID {
		return apiCaseBatchRunReport{}, false
	}
	return report, true
}

func environmentAcceptanceRunPayload(environmentID string, report apiCaseBatchRunReport) map[string]any {
	raw := cloneJSONObject(report)
	raw["environmentId"] = environmentID
	raw["reportUrl"] = "/api/environments/" + url.PathEscape(environmentID) + "/acceptance-runs/" + url.PathEscape(report.BatchRunID)
	raw["ok"] = report.OK
	return raw
}

func finalizeEnvironmentAcceptanceRun(ctx context.Context, runtime store.Store, report apiCaseBatchRunReport) {
	if runtime == nil || strings.TrimSpace(report.EnvironmentID) == "" || strings.TrimSpace(report.BatchRunID) == "" || strings.TrimSpace(report.WorkflowID) == "" {
		return
	}
	env, err := runtime.GetEnvironment(ctx, report.EnvironmentID)
	if err != nil {
		return
	}
	if env.VerificationWorkflowID != report.WorkflowID {
		return
	}
	env.LastVerificationRunID = report.BatchRunID
	if report.Acceptance.OK {
		env.LastVerificationStatus = store.StatusPassed
		env.EvidenceComplete = true
		env.TopologyComplete = true
		env.Status = "verified-ready"
		env.LastVerifiedAt = time.Now().UTC()
	} else {
		env.LastVerificationStatus = store.StatusFailed
		env.EvidenceComplete = false
		env.TopologyComplete = false
		env.Status = "verification-recorded"
	}
	env.Verified = false
	env.UpdatedAt = time.Now().UTC()
	_, _ = runtime.UpsertEnvironment(ctx, env)
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

func loadEnvironmentComponentGraphAPI(w http.ResponseWriter, r *http.Request, runtime store.Store, id string) (store.EnvironmentComponentGraph, bool) {
	graph, err := runtime.GetEnvironmentComponentGraph(r.Context(), id)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return store.EnvironmentComponentGraph{}, false
	}
	return graph, true
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

func validateEnvironmentVerificationWorkflow(env store.Environment) error {
	if strings.TrimSpace(env.VerificationWorkflowID) == "" {
		return errors.New("verificationWorkflowId is required for environment acceptance")
	}
	return nil
}

func cloneJSONObject(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
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
	workspace := "$AGENT_TESTBENCH_WORKSPACE"
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
	composeFiles := environmentBootstrapComposeFiles(compose)
	startCommand := strings.TrimSpace(valueString(compose["startCommand"]))
	if composeFile != "" {
		resolvedComposeFiles := environmentBootstrapResolvedComposeFiles(workspace, composeFiles)
		composeFile = resolvedComposeFiles[0]
		return map[string]any{
			"action":       "docker-compose",
			"composeFile":  composeFile,
			"composeFiles": resolvedComposeFiles,
			"projectName":  valueString(compose["projectName"]),
			"envFiles":     stringSliceValue(compose["envFiles"]),
			"profiles":     stringSliceValue(compose["profiles"]),
			"services":     stringSliceValue(compose["services"]),
			"skipPull":     boolValue(compose["skipPull"]),
			"skipBuild":    boolValue(compose["skipBuild"]),
			"commands":     environmentBootstrapComposeCommands(compose, workspace, resolvedComposeFiles),
			"heavy":        true,
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

func environmentBootstrapComposeFiles(compose map[string]any) []string {
	files := stringSliceValue(compose["composeFiles"])
	if len(files) == 0 {
		if file := strings.TrimSpace(valueString(compose["composeFile"])); file != "" {
			files = []string{file}
		}
	}
	return files
}

func environmentBootstrapResolvedComposeFiles(workspace string, files []string) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		if !filepath.IsAbs(file) && !strings.HasPrefix(file, "$") {
			file = filepath.Join(workspace, file)
		}
		if strings.TrimSpace(file) != "" {
			out = append(out, file)
		}
	}
	return out
}

func environmentBootstrapComposeCommands(compose map[string]any, workspace string, composeFiles []string) [][]string {
	baseArgs := []string{}
	for _, composeFile := range composeFiles {
		baseArgs = append(baseArgs, "-f", composeFile)
	}
	if len(stringMapValue(compose["env"])) > 0 {
		baseArgs = append(baseArgs, "--env-file", filepath.Join(workspace, ".agent-testbench", "restore.env"))
	}
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

func stringMapValue(value any) map[string]string {
	out := map[string]string{}
	switch typed := value.(type) {
	case map[string]string:
		for key, value := range typed {
			if strings.TrimSpace(key) != "" {
				out[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	case map[string]any:
		for key, value := range typed {
			if strings.TrimSpace(key) != "" {
				out[strings.TrimSpace(key)] = strings.TrimSpace(valueString(value))
			}
		}
	}
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
