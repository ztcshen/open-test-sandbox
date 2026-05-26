package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

type serviceRuntime struct {
	ServiceID      string `json:"serviceId"`
	NodeRole       string `json:"nodeRole,omitempty"`
	Container      string `json:"container,omitempty"`
	Image          string `json:"image,omitempty"`
	SourcePath     string `json:"sourcePath,omitempty"`
	BranchName     string `json:"branchName,omitempty"`
	CommitID       string `json:"commitId,omitempty"`
	State          string `json:"state"`
	Health         string `json:"health"`
	OK             bool   `json:"ok"`
	Port           int    `json:"port,omitempty"`
	ManagementPort int    `json:"managementPort,omitempty"`
	Message        string `json:"message,omitempty"`
}

type dockerContainerRow struct {
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
	Ports  string `json:"Ports"`
}

func dockerRuntimeByService(ctx context.Context, services []profile.Service) map[string]serviceRuntime {
	containers, err := listDockerContainers(ctx)
	if err != nil {
		return map[string]serviceRuntime{}
	}
	out := make(map[string]serviceRuntime)
	for _, service := range services {
		container, ok := matchServiceContainer(service, containers)
		if !ok {
			continue
		}
		state := strings.ToLower(strings.TrimSpace(container.State))
		if state == "" {
			state = "unknown"
		}
		health := dockerHealth(container.Status, state)
		port, managementPort := dockerPublishedPorts(container.Ports)
		runtime := serviceRuntime{
			ServiceID:      service.ID,
			NodeRole:       service.Kind,
			Container:      container.Names,
			Image:          firstNonEmpty(container.Image, service.Image),
			State:          state,
			Health:         health,
			OK:             false,
			Port:           firstPositiveInt(port, service.ServicePort),
			ManagementPort: firstPositiveInt(managementPort, service.ManagementPort),
			Message:        container.Status,
		}
		runtime = applyHTTPServiceHealth(ctx, runtime, service.HealthURL)
		out[service.ID] = runtime
	}
	return out
}

func dockerRuntimeByCatalogService(ctx context.Context, services []store.CatalogService) map[string]serviceRuntime {
	containers, err := listDockerContainers(ctx)
	if err != nil {
		return map[string]serviceRuntime{}
	}
	out := make(map[string]serviceRuntime)
	for _, service := range services {
		container, ok := matchCatalogServiceContainer(service, containers)
		if !ok {
			continue
		}
		configured := serviceRuntimeFromCatalogService(service, "", "", false)
		state := strings.ToLower(strings.TrimSpace(container.State))
		if state == "" {
			state = "unknown"
		}
		health := dockerHealth(container.Status, state)
		port, managementPort := dockerPublishedPorts(container.Ports)
		runtime := serviceRuntime{
			ServiceID:      service.ID,
			NodeRole:       service.Kind,
			Container:      container.Names,
			Image:          firstNonEmpty(container.Image, service.Image),
			SourcePath:     configured.SourcePath,
			BranchName:     configured.BranchName,
			CommitID:       configured.CommitID,
			State:          state,
			Health:         health,
			OK:             false,
			Port:           firstPositiveInt(port, service.ServicePort),
			ManagementPort: firstPositiveInt(managementPort, service.ManagementPort),
			Message:        container.Status,
		}
		out[service.ID] = runtime
	}
	return out
}

func configuredRuntimeByService(ctx context.Context, bundle profile.Bundle) map[string]serviceRuntime {
	env := runtimeEnv(bundle)
	out := make(map[string]serviceRuntime, len(bundle.Services))
	for _, service := range bundle.Services {
		sourcePath := serviceSourcePath(env, service)
		branchName, commitID := sourcePathRevision(ctx, sourcePath)
		if branchName == "" {
			branchName = strings.TrimSpace(service.GitBranch)
		}
		out[service.ID] = serviceRuntime{
			ServiceID:      service.ID,
			NodeRole:       service.Kind,
			Container:      service.ContainerName,
			Image:          service.Image,
			SourcePath:     sourcePath,
			BranchName:     branchName,
			CommitID:       commitID,
			State:          "missing",
			Health:         "unknown",
			Port:           service.ServicePort,
			ManagementPort: service.ManagementPort,
		}
	}
	return out
}

func mergeRuntime(configured serviceRuntime, observed serviceRuntime) serviceRuntime {
	if configured.ServiceID == "" {
		return observed
	}
	configured.Container = firstNonEmpty(observed.Container, configured.Container)
	configured.Image = firstNonEmpty(observed.Image, configured.Image)
	configured.SourcePath = firstNonEmpty(observed.SourcePath, configured.SourcePath)
	configured.BranchName = firstNonEmpty(observed.BranchName, configured.BranchName)
	configured.CommitID = firstNonEmpty(observed.CommitID, configured.CommitID)
	configured.State = firstNonEmpty(observed.State, configured.State)
	configured.Health = firstNonEmpty(observed.Health, configured.Health)
	configured.OK = observed.OK
	configured.Port = firstPositiveInt(observed.Port, configured.Port)
	configured.ManagementPort = firstPositiveInt(observed.ManagementPort, configured.ManagementPort)
	configured.Message = firstNonEmpty(observed.Message, configured.Message)
	return configured
}

func runtimeEnv(bundle profile.Bundle) map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	for _, path := range bundle.RuntimeEnvFiles {
		for key, value := range loadRuntimeEnvFile(resolveProfilePath(bundle.BaseDir, path)) {
			env[key] = value
		}
	}
	return env
}

func loadRuntimeEnvFile(path string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func resolveProfilePath(baseDir string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) || baseDir == "" {
		return path
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func serviceSourcePath(env map[string]string, service profile.Service) string {
	if value := strings.TrimSpace(service.SourcePath); value != "" {
		return value
	}
	repoEnv := strings.TrimSpace(service.RepoEnv)
	if repoEnv == "" {
		return ""
	}
	if value := strings.TrimSpace(env["DOCKER_"+repoEnv]); value != "" {
		return value
	}
	return strings.TrimSpace(env[repoEnv])
}

func listDockerContainers(ctx context.Context) ([]dockerContainerRow, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{json .}}")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	rows := []dockerContainerRow{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row dockerContainerRow
		if err := json.Unmarshal([]byte(line), &row); err == nil && row.Names != "" {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func sourcePathRevision(ctx context.Context, sourcePath string) (string, string) {
	if sourcePath == "" {
		return "", ""
	}
	if _, err := os.Stat(filepath.Join(sourcePath, ".git")); err == nil {
		return gitWorktreeRevision(ctx, sourcePath)
	}
	return sourceSnapshotRevision(sourcePath)
}

func gitWorktreeRevision(ctx context.Context, sourcePath string) (string, string) {
	branch := strings.TrimSpace(commandOutput(ctx, 800*time.Millisecond, "git", "-C", sourcePath, "rev-parse", "--abbrev-ref", "HEAD"))
	commit := strings.TrimSpace(commandOutput(ctx, 800*time.Millisecond, "git", "-C", sourcePath, "rev-parse", "--short=12", "HEAD"))
	if branch == "HEAD" {
		branch = ""
	}
	return branch, commit
}

func sourceSnapshotRevision(sourcePath string) (string, string) {
	name := filepath.Base(sourcePath)
	idx := strings.LastIndex(name, "-")
	if idx <= 0 || idx == len(name)-1 {
		return "", ""
	}
	branch := strings.TrimSpace(name[:idx])
	commit := strings.TrimSpace(name[idx+1:])
	if !regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`).MatchString(commit) {
		return "", ""
	}
	return branch, commit
}

func commandOutput(ctx context.Context, timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func matchServiceContainer(service profile.Service, containers []dockerContainerRow) (dockerContainerRow, bool) {
	return matchContainerByTargets(containers, service.ID, service.ContainerName, service.DockerService, service.ID)
}

func matchCatalogServiceContainer(service store.CatalogService, containers []dockerContainerRow) (dockerContainerRow, bool) {
	return matchContainerByTargets(containers, service.ID, service.ContainerName, service.DockerService, service.ID)
}

func matchContainerByTargets(containers []dockerContainerRow, serviceID string, targets ...string) (dockerContainerRow, bool) {
	for _, container := range containers {
		for _, name := range strings.Split(container.Names, ",") {
			name = strings.TrimSpace(name)
			for _, target := range targets {
				target = strings.TrimSpace(target)
				if target == "" {
					continue
				}
				if name == target || name == serviceID || strings.HasSuffix(name, "-"+target) || strings.HasSuffix(name, "_"+target) {
					return container, true
				}
			}
		}
	}
	return dockerContainerRow{}, false
}

func dockerHealth(status string, state string) string {
	status = strings.ToLower(status)
	switch {
	case strings.Contains(status, "(healthy)"):
		return "healthy"
	case strings.Contains(status, "(unhealthy)"):
		return "unhealthy"
	case state == "running":
		return "unchecked"
	case state == "":
		return "unknown"
	default:
		return state
	}
}

func applyHTTPServiceHealth(ctx context.Context, runtime serviceRuntime, rawURL string) serviceRuntime {
	url := serviceHTTPHealthURL(rawURL, runtime)
	if strings.TrimSpace(url) == "" {
		if runtime.State == "running" || runtime.State == "external" {
			runtime.Health = "unchecked"
			runtime.OK = false
			runtime.Message = firstNonEmpty(runtime.Message, "HTTP health check is not configured")
		}
		return runtime
	}
	if runtime.State != "running" && runtime.State != "external" {
		runtime.OK = false
		return runtime
	}
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		runtime.Health = "unhealthy"
		runtime.OK = false
		runtime.Message = err.Error()
		return runtime
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		runtime.Health = "unhealthy"
		runtime.OK = false
		runtime.Message = err.Error()
		return runtime
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		runtime.Health = "healthy"
		runtime.OK = true
		runtime.Message = firstNonEmpty(runtime.Message, "HTTP health check passed: "+url)
		return runtime
	}
	runtime.Health = "unhealthy"
	runtime.OK = false
	runtime.Message = "HTTP health check returned " + strconv.Itoa(resp.StatusCode) + ": " + url
	return runtime
}

func serviceHTTPHealthURL(rawURL string, runtime serviceRuntime) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "/") {
		port := firstPositiveInt(runtime.ManagementPort, runtime.Port)
		if port <= 0 {
			return ""
		}
		return "http://127.0.0.1:" + strconv.Itoa(port) + rawURL
	}
	return rawURL
}

func dockerPublishedPorts(raw string) (int, int) {
	matches := regexp.MustCompile(`(?:0\.0\.0\.0|127\.0\.0\.1|\[::\]|::):(\d+)->`).FindAllStringSubmatch(raw, -1)
	ports := make([]int, 0, len(matches))
	for _, match := range matches {
		port, err := strconv.Atoi(match[1])
		if err == nil && port > 0 {
			ports = append(ports, port)
		}
	}
	if len(ports) == 0 {
		return 0, 0
	}
	if len(ports) == 1 {
		return ports[0], 0
	}
	return ports[0], ports[1]
}
