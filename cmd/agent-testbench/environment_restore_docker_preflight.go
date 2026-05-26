package main

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type environmentRestorePreflight struct {
	OK                 bool                              `json:"ok"`
	AssumeCleanDocker  bool                              `json:"assumeCleanDocker,omitempty"`
	Tools              []environmentRestorePreflightTool `json:"tools"`
	HeavySteps         []string                          `json:"heavySteps,omitempty"`
	ContainerConflicts []string                          `json:"containerConflicts,omitempty"`
	StartupAssets      []environmentRestoreStartupAsset  `json:"startupAssets,omitempty"`
	Notes              []string                          `json:"notes,omitempty"`
}

type environmentRestorePreflightTool struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	OK       bool   `json:"ok"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
}

func environmentRestoreContainerNameConflicts(compose map[string]any, workspace string) []string {
	wanted := environmentRestoreContainerNames(compose, workspace)
	if len(wanted) == 0 {
		return nil
	}
	path, err := exec.LookPath("docker")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "ps", "-a", "--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		return nil
	}
	existing := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			existing[name] = true
		}
	}
	conflicts := []string{}
	for _, name := range wanted {
		if existing[name] {
			conflicts = append(conflicts, name)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

func environmentRestoreContainerNames(compose map[string]any, workspace string) []string {
	byService := environmentRestoreContainerNameByService(compose, workspace)
	names := make([]string, 0, len(byService))
	for _, name := range byService {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func environmentRestoreContainerNameByService(compose map[string]any, workspace string) map[string]string {
	out := map[string]string{}
	addContent := func(content string) {
		for service, container := range parseComposeContainerNames(content) {
			out[service] = container
		}
	}
	for _, content := range stringMapFromAny(compose["generatedFiles"]) {
		addContent(content)
	}
	for _, file := range environmentRestoreComposeFiles(compose) {
		path := restoreWorkspacePath(workspace, file)
		raw, err := os.ReadFile(path)
		if err == nil {
			addContent(string(raw))
		}
	}
	return out
}

func parseComposeContainerNames(content string) map[string]string {
	out := map[string]string{}
	inServices := false
	currentService := ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inServices = trimmed == "services:"
			currentService = ""
			continue
		}
		if !inServices {
			continue
		}
		if indent == 2 && strings.HasSuffix(trimmed, ":") {
			currentService = strings.TrimSuffix(trimmed, ":")
			continue
		}
		if currentService == "" || !strings.HasPrefix(trimmed, "container_name:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "container_name:"))
		name = strings.Trim(name, `"'`)
		if name != "" {
			out[currentService] = name
		}
	}
	return out
}

func environmentRestorePreflightReport(packageSpec environmentRestorePackageSpec, specs []environmentRestoreRepoSpec, compose map[string]any, workspace string, cleanupOptions environmentRestoreDockerCleanupOptions, prepareReposOnly bool) environmentRestorePreflight {
	report := environmentRestorePreflight{
		OK:                true,
		AssumeCleanDocker: cleanupOptions.AssumeCleanDocker,
		Notes: []string{
			"Sandbox control-plane Store must already be reachable outside restored Docker target services.",
			"Heavy Docker image and container validation should be reviewed before deleting or rebuilding existing local Docker state.",
		},
	}
	if environmentRestorePreflightRequiresGit(packageSpec, specs) {
		report.Tools = append(report.Tools, environmentRestoreTool("git", true))
	}
	composeFile := strings.TrimSpace(valueString(compose["composeFile"]))
	startCommand := strings.TrimSpace(valueString(compose["startCommand"]))
	if composeFile != "" {
		environmentRestoreAddComposePreflight(&report, compose, specs, workspace, cleanupOptions, prepareReposOnly)
	} else if startCommand != "" {
		report.HeavySteps = append(report.HeavySteps, "start command may create local runtime processes or containers")
	}
	for _, tool := range report.Tools {
		if tool.Required && !tool.OK {
			report.OK = false
		}
	}
	return report
}

func environmentRestorePreflightRequiresGit(packageSpec environmentRestorePackageSpec, specs []environmentRestoreRepoSpec) bool {
	if strings.TrimSpace(packageSpec.URL) != "" || strings.TrimSpace(packageSpec.Ref) != "" {
		return true
	}
	for _, spec := range specs {
		if strings.TrimSpace(spec.URL) != "" || strings.TrimSpace(spec.Ref) != "" {
			return true
		}
	}
	return false
}

func environmentRestoreAddComposePreflight(report *environmentRestorePreflight, compose map[string]any, specs []environmentRestoreRepoSpec, workspace string, cleanupOptions environmentRestoreDockerCleanupOptions, prepareReposOnly bool) {
	report.Tools = append(report.Tools, environmentRestoreTool("docker", true))
	report.Tools = append(report.Tools, environmentRestoreCommandTool("docker compose", true, "docker", "compose", "version"))
	report.HeavySteps = append(report.HeavySteps, environmentRestoreComposeHeavySteps(compose, cleanupOptions)...)
	environmentRestoreCheckContainerConflicts(report, compose, workspace, cleanupOptions, prepareReposOnly)
	environmentRestoreCheckStartupAssets(report, compose, specs, workspace, cleanupOptions, prepareReposOnly)
	for _, file := range environmentRestoreComposeFiles(compose) {
		if resolved := restoreWorkspacePath(workspace, file); strings.TrimSpace(resolved) != "" {
			report.Notes = append(report.Notes, "compose file must exist before Docker execution: "+resolved)
		}
	}
}

func environmentRestoreComposeHeavySteps(compose map[string]any, cleanupOptions environmentRestoreDockerCleanupOptions) []string {
	steps := []string{}
	if !boolFromReportAny(compose["skipPull"]) {
		steps = append(steps, "docker compose pull may download images")
	}
	if !boolFromReportAny(compose["skipBuild"]) {
		steps = append(steps, "docker compose build may build images from local checkouts")
	}
	steps = append(steps, "docker compose up -d may create or replace containers")
	if cleanupOptions.Requested {
		steps = append(steps, "docker compose down may remove existing containers and orphan containers")
		if cleanupOptions.IncludeImages {
			steps = append(steps, "docker compose down --rmi all may remove local images")
		}
	}
	return steps
}

func environmentRestoreCheckContainerConflicts(report *environmentRestorePreflight, compose map[string]any, workspace string, cleanupOptions environmentRestoreDockerCleanupOptions, prepareReposOnly bool) {
	switch {
	case cleanupOptions.Requested:
		return
	case cleanupOptions.AssumeCleanDocker:
		report.Notes = append(report.Notes, "Clean-machine dry-run assumes target Docker containers do not exist on the colleague machine; current local container names are not treated as blockers.")
	case prepareReposOnly || cleanupOptions.UseExistingContainers:
		return
	default:
		report.ContainerConflicts = environmentRestoreContainerNameConflicts(compose, workspace)
		if len(report.ContainerConflicts) > 0 {
			report.OK = false
		}
	}
}

func environmentRestoreCheckStartupAssets(report *environmentRestorePreflight, compose map[string]any, specs []environmentRestoreRepoSpec, workspace string, cleanupOptions environmentRestoreDockerCleanupOptions, prepareReposOnly bool) {
	if prepareReposOnly || cleanupOptions.UseExistingContainers {
		return
	}
	report.StartupAssets = environmentRestoreStartupAssets(compose, specs, workspace)
	for _, asset := range report.StartupAssets {
		if !asset.OK {
			report.OK = false
		}
	}
}

func environmentRestoreTool(name string, required bool) environmentRestorePreflightTool {
	tool := environmentRestorePreflightTool{Name: name, Required: required}
	path, err := exec.LookPath(name)
	if err != nil {
		tool.OK = false
		tool.Error = err.Error()
		return tool
	}
	tool.OK = true
	tool.Path = path
	return tool
}

func environmentRestoreCommandTool(name string, required bool, command string, args ...string) environmentRestorePreflightTool {
	tool := environmentRestorePreflightTool{Name: name, Required: required}
	path, err := exec.LookPath(command)
	if err != nil {
		tool.OK = false
		tool.Error = err.Error()
		return tool
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tool.OK = false
		tool.Path = path
		tool.Error = strings.TrimSpace(string(out))
		if tool.Error == "" {
			tool.Error = err.Error()
		}
		return tool
	}
	tool.OK = true
	tool.Path = path
	return tool
}
