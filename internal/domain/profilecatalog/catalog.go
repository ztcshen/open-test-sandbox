package profilecatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	domaincatalog "agent-testbench/internal/domain/catalog"
	"agent-testbench/internal/domain/profile"
)

const ReadModelInterfaceNodes = "interface-nodes"

func FromBundle(bundle profile.Bundle, indexedAt time.Time) domaincatalog.ProfileCatalog {
	runtimeEnv := RuntimeEnvFromBundle(bundle)
	return domaincatalog.ProfileCatalog{
		ProfileID:        bundle.ID,
		IndexedAt:        indexedAt,
		Services:         catalogServicesFromProfile(bundle.Services, runtimeEnv),
		Workflows:        catalogWorkflowsFromProfile(bundle.Workflows),
		InterfaceNodes:   catalogInterfaceNodesFromProfile(bundle.InterfaceNodes),
		APICases:         catalogAPICasesFromProfile(bundle.APICases),
		RequestTemplates: catalogRequestTemplatesFromProfile(bundle.RequestTemplates),
		WorkflowBindings: catalogWorkflowBindingsFromProfile(bundle.WorkflowBindings),
		CaseDependencies: catalogCaseDependenciesFromProfile(bundle.CaseDependencies),
		TemplateConfigs:  catalogTemplateConfigsFromProfile(bundle.TemplateConfigs),
		Fixtures:         catalogFixturesFromProfile(bundle.Fixtures),
	}
}

func ToBundle(catalog domaincatalog.ProfileCatalog) profile.Bundle {
	return profile.Bundle{
		ID:               catalog.ProfileID,
		DisplayName:      catalog.ProfileID,
		Services:         profileServicesFromCatalog(catalog.Services),
		Workflows:        profileWorkflowsFromCatalog(catalog.Workflows),
		InterfaceNodes:   profileInterfaceNodesFromCatalog(catalog.InterfaceNodes),
		APICases:         profileAPICasesFromCatalog(catalog.APICases),
		RequestTemplates: profileRequestTemplatesFromCatalog(catalog.RequestTemplates),
		WorkflowBindings: profileWorkflowBindingsFromCatalog(catalog.WorkflowBindings),
		CaseDependencies: profileCaseDependenciesFromCatalog(catalog.CaseDependencies),
		Fixtures:         profileFixturesFromCatalog(catalog.Fixtures),
		TemplateConfigs:  profileTemplateConfigsFromCatalog(catalog.TemplateConfigs),
	}
}

func InterfaceNodesReadModel(catalog domaincatalog.ProfileCatalog, configVersionID string, generatedAt time.Time) (domaincatalog.ReadModel, error) {
	payload := interfaceNodesReadModelPayload{
		OK:           true,
		TemplateID:   "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Filters:      map[string]string{"serviceId": "", "operation": ""},
		Source:       map[string]string{"kind": "read-model", "id": catalog.ProfileID},
		Items:        interfaceNodeReadModelItems(catalog),
		Presentation: interfaceNodesPresentationForCatalog(catalog.TemplateConfigs),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return domaincatalog.ReadModel{}, err
	}
	return domaincatalog.ReadModel{
		ProfileID:       catalog.ProfileID,
		Key:             ReadModelInterfaceNodes,
		ConfigVersionID: configVersionID,
		PayloadJSON:     string(raw),
		GeneratedAt:     generatedAt,
		UpdatedAt:       generatedAt,
	}, nil
}

type interfaceNodesReadModelPayload struct {
	OK           bool                       `json:"ok"`
	TemplateID   string                     `json:"templateId"`
	Filters      map[string]string          `json:"filters"`
	Source       map[string]string          `json:"source"`
	Items        []interfaceNodeReadModel   `json:"items"`
	Presentation interfaceNodesPresentation `json:"presentation,omitempty"`
}

type interfaceNodesPresentation struct {
	Copy map[string]string `json:"copy,omitempty"`
}

type interfaceNodeReadModel struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"displayName"`
	ServiceID            string `json:"serviceId"`
	Operation            string `json:"operation"`
	Method               string `json:"method,omitempty"`
	Path                 string `json:"path,omitempty"`
	Href                 string `json:"href"`
	Status               string `json:"status"`
	AdmissionStatus      string `json:"admissionStatus"`
	ValidationStatus     string `json:"validationStatus"`
	ValidationIssueCount int    `json:"validationIssueCount"`
	RequiredCaseCount    int    `json:"requiredCaseCount"`
	PassedCaseCount      int    `json:"passedCaseCount"`
	TimeoutMs            int    `json:"timeoutMs,omitempty"`
	LatestRunID          string `json:"latestRunId,omitempty"`
	LatestElapsedMs      int64  `json:"latestElapsedMs,omitempty"`
	TotalElapsedMs       int64  `json:"totalElapsedMs,omitempty"`
}

func interfaceNodeReadModelItems(catalog domaincatalog.ProfileCatalog) []interfaceNodeReadModel {
	requiredByNode := map[string]int{}
	for _, item := range catalog.APICases {
		if item.NodeID != "" && item.RequiredForAdmission && activeStatus(item.Status) {
			requiredByNode[item.NodeID]++
		}
	}
	items := make([]interfaceNodeReadModel, 0, len(catalog.InterfaceNodes))
	for _, node := range catalog.InterfaceNodes {
		operation := firstNonEmpty(node.Operation, node.DisplayName, node.ID)
		items = append(items, interfaceNodeReadModel{
			ID:                   node.ID,
			DisplayName:          node.DisplayName,
			ServiceID:            node.ServiceID,
			Operation:            operation,
			Method:               node.Method,
			Path:                 node.Path,
			Href:                 "/interface-node.html?id=" + node.ID,
			Status:               firstNonEmpty(node.Status, "draft"),
			AdmissionStatus:      "pending",
			ValidationStatus:     "valid",
			ValidationIssueCount: 0,
			RequiredCaseCount:    requiredByNode[node.ID],
			PassedCaseCount:      0,
			TimeoutMs:            node.TimeoutMs,
		})
	}
	return items
}

func interfaceNodesPresentationForCatalog(configs []domaincatalog.TemplateConfig) interfaceNodesPresentation {
	copy := map[string]string{}
	for _, config := range configs {
		if !activeStatus(config.Status) || config.ScopeType != "interface-node-directory" {
			continue
		}
		if config.ScopeID != "" && config.ScopeID != "_default" {
			continue
		}
		mergeStringMap(copy, templateConfigCopy(config.ConfigJSON))
	}
	if len(copy) == 0 {
		return interfaceNodesPresentation{}
	}
	return interfaceNodesPresentation{Copy: copy}
}

func templateConfigCopy(raw string) map[string]string {
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return map[string]string{}
	}
	copyValue, _ := value["copy"].(map[string]any)
	out := map[string]string{}
	for key, item := range copyValue {
		if key == "" {
			continue
		}
		text := ""
		if s, ok := item.(string); ok {
			text = strings.TrimSpace(s)
		}
		if text != "" {
			out[key] = text
		}
	}
	return out
}

func mergeStringMap(target map[string]string, source map[string]string) {
	for key, value := range source {
		if key != "" && value != "" {
			target[key] = value
		}
	}
}

func activeStatus(status string) bool {
	return status == "" || status == "active"
}

func jsonStringMap(value map[string]any) string {
	if len(value) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func jsonMap(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}

// RuntimeEnvFromBundle merges process environment values with bundle-declared runtime env files.
func RuntimeEnvFromBundle(bundle profile.Bundle) map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	for _, path := range bundle.RuntimeEnvFiles {
		for key, value := range loadRuntimeEnvFile(resolveBundlePath(bundle.BaseDir, path)) {
			env[key] = value
		}
	}
	return env
}

func serviceSourcePath(env map[string]string, service profile.Service) string {
	return ServiceSourcePath(env, service)
}

// ServiceSourcePath resolves the configured checkout path for a profile service.
func ServiceSourcePath(env map[string]string, service profile.Service) string {
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
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func resolveBundlePath(baseDir string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) || baseDir == "" {
		return path
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
