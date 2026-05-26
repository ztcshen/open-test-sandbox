package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type environmentRestoreDockerReport struct {
	OK            bool                                  `json:"ok"`
	Action        string                                `json:"action"`
	ComposeFile   string                                `json:"composeFile,omitempty"`
	Workdir       string                                `json:"workdir,omitempty"`
	Generated     []environmentRestoreGeneratedFile     `json:"generatedFiles,omitempty"`
	AppliedAssets []environmentRestoreAppliedAsset      `json:"appliedAssets,omitempty"`
	Cleanup       environmentRestoreDockerCleanupReport `json:"cleanup,omitempty"`
	Commands      [][]string                            `json:"commands,omitempty"`
	Output        []string                              `json:"output,omitempty"`
	Error         string                                `json:"error,omitempty"`
	HealthChecks  []environmentRestoreHealthCheckReport `json:"healthChecks,omitempty"`
}

type environmentRestoreGeneratedFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	Action string `json:"action"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

type environmentRestoreDockerCleanupReport struct {
	Requested      bool       `json:"requested,omitempty"`
	Allowed        bool       `json:"allowed,omitempty"`
	IncludeImages  bool       `json:"includeImages,omitempty"`
	Action         string     `json:"action,omitempty"`
	BackupCommands [][]string `json:"backupCommands,omitempty"`
	Commands       [][]string `json:"commands,omitempty"`
	Output         []string   `json:"output,omitempty"`
	Error          string     `json:"error,omitempty"`
	Warning        string     `json:"warning,omitempty"`
}

type environmentRestoreHealthCheckReport struct {
	ID         string `json:"id,omitempty"`
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	Address    string `json:"address,omitempty"`
	Command    string `json:"command,omitempty"`
	Service    string `json:"service,omitempty"`
	Container  string `json:"container,omitempty"`
	OK         bool   `json:"ok"`
	StatusCode int    `json:"statusCode,omitempty"`
	State      string `json:"state,omitempty"`
	Health     string `json:"health,omitempty"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
}

func environmentRestoreDockerPlan(compose map[string]any, workspace string, cleanupOptions environmentRestoreDockerCleanupOptions) (environmentRestoreDockerReport, []string) {
	report := environmentRestoreDockerReport{OK: true, Workdir: workspace}
	composeFiles := environmentRestoreComposeFiles(compose)
	startCommand := strings.TrimSpace(valueString(compose["startCommand"]))
	if strings.TrimSpace(valueString(compose["composeFile"])) != "" {
		baseArgs := environmentRestorePlanComposeCommands(&report, compose, workspace, composeFiles, cleanupOptions)
		return report, baseArgs
	}
	if startCommand != "" {
		return environmentRestorePlanStartCommand(workspace, startCommand, cleanupOptions), nil
	}
	report.OK = false
	report.Action = "missing-docker-plan"
	report.Error = "composeFile or startCommand is required to restore Docker services"
	return report, nil
}

func environmentRestorePlanComposeCommands(report *environmentRestoreDockerReport, compose map[string]any, workspace string, composeFiles []string, cleanupOptions environmentRestoreDockerCleanupOptions) []string {
	report.Action = "plan-docker-compose"
	resolvedComposeFiles := environmentRestoreResolvedComposeFiles(workspace, composeFiles)
	report.ComposeFile = strings.Join(resolvedComposeFiles, ",")
	baseArgs := environmentRestoreComposeBaseArgs(compose, workspace, resolvedComposeFiles)
	services := stringSliceFromAny(compose["services"])
	report.Cleanup = environmentRestoreDockerCleanupPlan(baseArgs, cleanupOptions)
	imageServices, buildServices := environmentRestoreComposeCommandServices(compose, workspace, composeFiles, services)
	if !boolFromReportAny(compose["skipPull"]) && len(imageServices) > 0 {
		report.Commands = append(report.Commands, append(append([]string{"docker", "compose"}, baseArgs...), append([]string{"pull"}, imageServices...)...))
	}
	if !boolFromReportAny(compose["skipBuild"]) && len(buildServices) > 0 {
		report.Commands = append(report.Commands, append(append([]string{"docker", "compose"}, baseArgs...), append([]string{"build"}, buildServices...)...))
	}
	report.Commands = append(report.Commands, append(append([]string{"docker", "compose"}, baseArgs...), append([]string{"up", "-d"}, services...)...))
	return baseArgs
}

func environmentRestorePlanStartCommand(workspace string, startCommand string, cleanupOptions environmentRestoreDockerCleanupOptions) environmentRestoreDockerReport {
	report := environmentRestoreDockerReport{
		OK:       true,
		Workdir:  workspace,
		Action:   "plan-start-command",
		Commands: [][]string{{"/bin/sh", "-c", startCommand}},
	}
	if cleanupOptions.Requested {
		report.OK = false
		report.Cleanup = environmentRestoreDockerCleanupReport{
			Requested:     true,
			Allowed:       cleanupOptions.Allowed,
			IncludeImages: cleanupOptions.IncludeImages,
			Action:        "unsupported-cleanup",
			Error:         "Docker cleanup requires a recorded composeFile",
		}
		report.Error = report.Cleanup.Error
	}
	return report
}

func environmentRestoreCheckGeneratedFiles(report *environmentRestoreDockerReport, compose map[string]any, workspace string, execute bool) bool {
	report.Generated = prepareEnvironmentRestoreGeneratedFiles(compose, workspace, execute)
	for _, item := range report.Generated {
		if !item.OK {
			report.OK = false
			report.Action = "prepare-generated-files"
			report.Error = item.Error
			return false
		}
	}
	return true
}

func environmentRestorePrepareDockerExecution(report *environmentRestoreDockerReport, compose map[string]any, workspace string) bool {
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		report.OK = false
		report.Action = "prepare-workspace"
		report.Error = err.Error()
		return false
	}
	if !environmentRestoreCheckGeneratedFiles(report, compose, workspace, true) {
		return false
	}
	envFile, err := writeEnvironmentRestoreGeneratedEnvFile(workspace, compose)
	if err != nil {
		report.OK = false
		report.Action = "prepare-compose-env"
		report.Error = err.Error()
		return false
	}
	if envFile != "" {
		report.Output = append(report.Output, "generated compose env file: "+envFile)
	}
	return true
}

func environmentRestoreValidateComposeFiles(report *environmentRestoreDockerReport) bool {
	if report.ComposeFile == "" {
		return true
	}
	for _, composeFile := range strings.Split(report.ComposeFile, ",") {
		composeFile = strings.TrimSpace(composeFile)
		if composeFile == "" {
			continue
		}
		if stat, err := os.Stat(composeFile); err != nil {
			report.OK = false
			report.Action = "missing-compose-file"
			report.Error = fmt.Sprintf("compose file is required before Docker execution: %s", composeFile)
			return false
		} else if stat.IsDir() {
			report.OK = false
			report.Action = "invalid-compose-file"
			report.Error = fmt.Sprintf("compose file path is a directory: %s", composeFile)
			return false
		}
	}
	return true
}

func environmentRestoreRunCleanup(ctx context.Context, report *environmentRestoreDockerReport, workspace string) bool {
	if report.ComposeFile == "" || !report.Cleanup.Requested {
		return true
	}
	if !report.Cleanup.Allowed {
		report.OK = false
		report.Cleanup.Action = "cleanup-blocked"
		report.Cleanup.Error = "Docker cleanup requested during --execute; rerun with --allow-destructive-docker-cleanup after reviewing cleanup commands"
		report.Error = report.Cleanup.Error
		return false
	}
	report.Cleanup.Action = "run-cleanup"
	for _, command := range append(report.Cleanup.BackupCommands, report.Cleanup.Commands...) {
		output, errText := runRestoreCommand(ctx, workspace, command)
		if strings.TrimSpace(output) != "" {
			report.Cleanup.Output = append(report.Cleanup.Output, output)
		}
		if errText != "" {
			report.OK = false
			report.Cleanup.Error = errText
			report.Error = errText
			return false
		}
	}
	return true
}

func environmentRestoreMarkDockerExecuting(report *environmentRestoreDockerReport) {
	if report.Action == "plan-docker-compose" {
		report.Action = "run-docker-compose"
		return
	}
	report.Action = "run-start-command"
}

func environmentRestoreRunCommands(ctx context.Context, report *environmentRestoreDockerReport, workspace string) bool {
	for _, command := range report.Commands {
		output, errText := runRestoreCommand(ctx, workspace, command)
		if strings.TrimSpace(output) != "" {
			report.Output = append(report.Output, output)
		}
		if errText != "" {
			report.OK = false
			report.Error = errText
			return false
		}
	}
	return true
}

func environmentRestoreDockerCleanupPlan(baseArgs []string, options environmentRestoreDockerCleanupOptions) environmentRestoreDockerCleanupReport {
	if !options.Requested {
		return environmentRestoreDockerCleanupReport{}
	}
	cleanup := environmentRestoreDockerCleanupReport{
		Requested:     true,
		Allowed:       options.Allowed,
		IncludeImages: options.IncludeImages,
		Action:        "plan-cleanup",
		Warning:       "Review Docker cleanup commands before simulating a clean colleague machine; the sandbox SQL Store must remain outside these Docker target services.",
	}
	cleanup.BackupCommands = [][]string{
		append(append([]string{"docker", "compose"}, baseArgs...), "ps"),
		append(append([]string{"docker", "compose"}, baseArgs...), "images"),
		append(append([]string{"docker", "compose"}, baseArgs...), "config"),
	}
	down := append(append([]string{"docker", "compose"}, baseArgs...), "down", "--remove-orphans")
	if options.IncludeImages {
		down = append(down, "--rmi", "all")
	}
	cleanup.Commands = [][]string{down}
	return cleanup
}

func environmentRestoreComposeFiles(compose map[string]any) []string {
	files := stringSliceFromAny(compose["composeFiles"])
	if len(files) == 0 {
		if file := strings.TrimSpace(valueString(compose["composeFile"])); file != "" {
			files = []string{file}
		}
	}
	return files
}

func environmentRestoreResolvedComposeFiles(workspace string, files []string) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		if resolved := restoreWorkspacePath(workspace, file); strings.TrimSpace(resolved) != "" {
			out = append(out, resolved)
		}
	}
	return out
}

func prepareEnvironmentRestoreGeneratedFiles(compose map[string]any, workspace string, execute bool) []environmentRestoreGeneratedFile {
	files := stringMapFromAny(compose["generatedFiles"])
	if len(files) == 0 {
		return nil
	}
	paths := environmentRestoreGeneratedFilePaths(compose, files)
	out := make([]environmentRestoreGeneratedFile, 0, len(paths))
	for _, path := range paths {
		content := files[path]
		report := environmentRestoreGeneratedFile{
			Path:   restoreWorkspacePath(workspace, path),
			Bytes:  len(content),
			Action: "plan-write",
			OK:     true,
		}
		if ok, errText := environmentRestoreGeneratedFileTargetOK(path, workspace); !ok {
			report.OK = false
			report.Error = errText
			out = append(out, report)
			continue
		}
		if execute {
			report.Action = "write"
			if err := os.MkdirAll(filepath.Dir(report.Path), 0o755); err != nil {
				report.OK = false
				report.Error = err.Error()
			} else if err := os.WriteFile(report.Path, []byte(content), 0o644); err != nil {
				report.OK = false
				report.Error = err.Error()
			}
		}
		out = append(out, report)
	}
	return out
}

func environmentRestoreGeneratedFilePaths(compose map[string]any, files map[string]string) []string {
	paths := make([]string, 0, len(files))
	seen := map[string]bool{}
	for _, path := range stringSliceFromAny(compose["generatedFileOrder"]) {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "." || clean == "" || seen[clean] {
			continue
		}
		if _, exists := files[clean]; !exists {
			continue
		}
		paths = append(paths, clean)
		seen[clean] = true
	}
	remaining := make([]string, 0, len(files)-len(paths))
	for path := range files {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "." || clean == "" || seen[clean] {
			continue
		}
		remaining = append(remaining, clean)
	}
	sort.Strings(remaining)
	paths = append(paths, remaining...)
	return paths
}

func environmentRestoreGeneratedFileTargetOK(path string, workspace string) (bool, string) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return false, "generated file path is empty"
	}
	if filepath.IsAbs(raw) {
		return false, "generated file path must be relative to the restore workspace: " + raw
	}
	clean := filepath.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return false, "generated file path must stay inside the restore workspace: " + raw
	}
	target := restoreWorkspacePath(workspace, clean)
	rel, err := filepath.Rel(workspace, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, "generated file path must stay inside the restore workspace: " + raw
	}
	return true, ""
}

func environmentRestoreComposeBaseArgs(compose map[string]any, workspace string, composeFiles []string) []string {
	args := []string{}
	for _, composeFile := range composeFiles {
		args = append(args, "-f", composeFile)
	}
	if len(stringMapFromAny(compose["env"])) > 0 {
		args = append(args, "--env-file", environmentRestoreGeneratedEnvFilePath(workspace))
	}
	if projectName := strings.TrimSpace(valueString(compose["projectName"])); projectName != "" {
		args = append(args, "-p", projectName)
	}
	for _, envFile := range stringSliceFromAny(compose["envFiles"]) {
		args = append(args, "--env-file", restoreWorkspacePath(workspace, envFile))
	}
	for _, profile := range stringSliceFromAny(compose["profiles"]) {
		args = append(args, "--profile", profile)
	}
	return args
}

func environmentRestoreGeneratedEnvFilePath(workspace string) string {
	return filepath.Join(workspace, ".agent-testbench", "restore.env")
}

func environmentRestoreComposeCommandServices(compose map[string]any, workspace string, composeFiles []string, selected []string) ([]string, []string) {
	knownServices, buildServices := environmentRestoreComposeBuildServiceSet(compose, workspace, composeFiles)
	services := append([]string{}, selected...)
	if len(services) == 0 && len(knownServices) > 0 {
		services = make([]string, 0, len(knownServices))
		for service := range knownServices {
			services = append(services, service)
		}
		sort.Strings(services)
	}
	imageOut := []string{}
	buildOut := []string{}
	for _, service := range services {
		service = strings.TrimSpace(service)
		if service == "" {
			continue
		}
		if buildServices[service] {
			buildOut = append(buildOut, service)
			continue
		}
		imageOut = append(imageOut, service)
	}
	return imageOut, buildOut
}

func environmentRestoreComposeBuildServiceSet(compose map[string]any, workspace string, composeFiles []string) (map[string]bool, map[string]bool) {
	known := map[string]bool{}
	builds := map[string]bool{}
	generated := stringMapFromAny(compose["generatedFiles"])
	for _, file := range composeFiles {
		content := generated[filepath.Clean(file)]
		if content == "" {
			content = generated[file]
		}
		if content == "" {
			if raw, err := os.ReadFile(restoreWorkspacePath(workspace, file)); err == nil {
				content = string(raw)
			}
		}
		if content == "" {
			continue
		}
		fileKnown, fileBuilds := environmentRestoreComposeBuildServicesFromText(content)
		for service := range fileKnown {
			known[service] = true
		}
		for service := range fileBuilds {
			known[service] = true
			builds[service] = true
		}
	}
	return known, builds
}

func environmentRestoreComposeBuildServicesFromText(content string) (map[string]bool, map[string]bool) {
	known := map[string]bool{}
	builds := map[string]bool{}
	inServices := false
	servicesIndent := -1
	serviceIndent := -1
	currentService := ""
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := leadingSpaceCount(line)
		trimmed := strings.TrimSpace(line)
		if !inServices {
			if trimmed == "services:" {
				inServices = true
				servicesIndent = indent
			}
			continue
		}
		if indent <= servicesIndent {
			break
		}
		if strings.HasPrefix(trimmed, "-") {
			continue
		}
		if strings.HasSuffix(trimmed, ":") {
			key := strings.TrimSuffix(trimmed, ":")
			if serviceIndent < 0 || indent == serviceIndent {
				serviceIndent = indent
				currentService = strings.TrimSpace(key)
				if currentService != "" {
					known[currentService] = true
				}
				continue
			}
		}
		if currentService != "" && indent > serviceIndent && (trimmed == "build:" || strings.HasPrefix(trimmed, "build: ")) {
			builds[currentService] = true
		}
	}
	return known, builds
}

func writeEnvironmentRestoreGeneratedEnvFile(workspace string, compose map[string]any) (string, error) {
	values := stringMapFromAny(compose["env"])
	if len(values) == 0 {
		return "", nil
	}
	path := environmentRestoreGeneratedEnvFilePath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		value := strings.ReplaceAll(values[key], "$AGENT_TESTBENCH_WORKSPACE", workspace)
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(value)
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func stringMapFromAny(value any) map[string]string {
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

func stringSliceFromAny(value any) []string {
	values, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if strings.TrimSpace(item) != "" {
					out = append(out, strings.TrimSpace(item))
				}
			}
			return out
		}
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
