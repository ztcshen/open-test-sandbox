package controlplane

import (
	"path/filepath"
	"sort"
	"strings"

	"agent-testbench/internal/store"
)

const (
	environmentBootstrapKeyCommands     = "commands"
	environmentBootstrapKeyHealthChecks = "healthChecks"
	environmentBootstrapKeyHeavy        = "heavy"
)

func EnvironmentBootstrapPlan(env store.Environment) map[string]any {
	workspace := "$AGENT_TESTBENCH_WORKSPACE"
	repos := environmentBootstrapRepoPlan(env, workspace)
	compose := jsonObject(env.ComposeJSON)
	healthChecks := jsonArray(env.HealthChecksJSON)
	docker := environmentBootstrapDockerPlan(compose, workspace)
	return map[string]any{
		"repos":                             jsonObject(env.ReposJSON),
		"compose":                           jsonObject(env.ComposeJSON),
		environmentBootstrapKeyHealthChecks: jsonArray(env.HealthChecksJSON),
		"verificationWorkflow":              env.VerificationWorkflowID,
		"workspace":                         workspace,
		"steps":                             environmentBootstrapSteps(repos, docker, healthChecks, env.VerificationWorkflowID),
		"restore": map[string]any{
			"repos":                             repos,
			"docker":                            docker,
			environmentBootstrapKeyHealthChecks: environmentBootstrapHealthPlan(healthChecks),
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
			"action":                        "docker-compose",
			"composeFile":                   composeFile,
			"composeFiles":                  resolvedComposeFiles,
			"projectName":                   valueString(compose["projectName"]),
			"envFiles":                      stringSliceValue(compose["envFiles"]),
			"profiles":                      stringSliceValue(compose["profiles"]),
			"services":                      stringSliceValue(compose["services"]),
			"skipPull":                      boolValue(compose["skipPull"]),
			"skipBuild":                     boolValue(compose["skipBuild"]),
			environmentBootstrapKeyCommands: environmentBootstrapComposeCommands(compose, workspace, resolvedComposeFiles),
			environmentBootstrapKeyHeavy:    true,
		}
	}
	if startCommand != "" {
		return map[string]any{
			"action":                        "start-command",
			environmentBootstrapKeyCommands: [][]string{{"/bin/sh", "-c", startCommand}},
			environmentBootstrapKeyHeavy:    true,
		}
	}
	return map[string]any{
		"action":                        "missing-docker-plan",
		environmentBootstrapKeyCommands: [][]string{},
		environmentBootstrapKeyHeavy:    false,
	}
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
	add := func(key string, value any) {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = strings.TrimSpace(valueString(value))
		}
	}
	switch typed := value.(type) {
	case map[string]string:
		for key, value := range typed {
			add(key, value)
		}
	case map[string]any:
		for key, value := range typed {
			add(key, value)
		}
	}
	return out
}

func stringSliceValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return trimNonEmptyStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(valueString(item)); value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func trimNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
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
		"kind":                          "docker",
		"action":                        docker["action"],
		environmentBootstrapKeyCommands: docker[environmentBootstrapKeyCommands],
		environmentBootstrapKeyHeavy:    docker[environmentBootstrapKeyHeavy],
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
	item, ok := value.(map[string]any)
	if !ok || item == nil {
		return map[string]any{}
	}
	return item
}
