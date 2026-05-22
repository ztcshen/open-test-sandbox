package profilecatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

const ReadModelInterfaceNodes = "interface-nodes"

func FromBundle(bundle profile.Bundle, indexedAt time.Time) store.ProfileCatalog {
	catalog := store.ProfileCatalog{
		ProfileID: bundle.ID,
		IndexedAt: indexedAt,
	}
	runtimeEnv := runtimeEnvFromBundle(bundle)
	for _, service := range bundle.Services {
		catalog.Services = append(catalog.Services, store.CatalogService{
			ID:                  service.ID,
			DisplayName:         service.DisplayName,
			Kind:                service.Kind,
			AttachedTemplateIDs: service.AttachedTemplateIDs,
			GitURL:              service.GitURL,
			GitBranch:           service.GitBranch,
			RepoEnv:             service.RepoEnv,
			SourcePath:          serviceSourcePath(runtimeEnv, service),
			ContainerName:       service.ContainerName,
			Image:               service.Image,
			DockerService:       service.DockerService,
			ServicePort:         service.ServicePort,
			ManagementPort:      service.ManagementPort,
			MemoryMb:            service.MemoryMb,
			CPUMilli:            service.CPUMilli,
			StartupCommand:      service.StartupCommand,
			HealthURL:           service.HealthURL,
			LogPath:             service.LogPath,
			Status:              service.Status,
			SortOrder:           service.SortOrder,
		})
	}
	for _, workflow := range bundle.Workflows {
		catalog.Workflows = append(catalog.Workflows, store.CatalogWorkflow{
			ID:                workflow.ID,
			DisplayName:       workflow.DisplayName,
			Description:       workflow.Description,
			BaseStepTimeoutMs: workflow.BaseStepTimeoutMs,
			TimeoutOffsetMs:   workflow.TimeoutOffsetMs,
		})
	}
	for _, node := range bundle.InterfaceNodes {
		catalog.InterfaceNodes = append(catalog.InterfaceNodes, store.CatalogInterfaceNode{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
			Operation:   node.Operation,
			Method:      node.Method,
			Path:        node.Path,
			TemplateID:  node.TemplateID,
			Version:     node.Version,
			Status:      node.Status,
			Tags:        node.Tags,
			Description: node.Description,
			TimeoutMs:   node.TimeoutMs,
			SortOrder:   node.SortOrder,
			CreatedAt:   node.CreatedAt,
			UpdatedAt:   node.UpdatedAt,
		})
	}
	for _, item := range bundle.APICases {
		catalog.APICases = append(catalog.APICases, store.CatalogAPICase{
			ID:                   item.ID,
			DisplayName:          item.DisplayName,
			Description:          item.Description,
			NodeID:               item.NodeID,
			CaseType:             item.CaseType,
			Scenario:             item.Scenario,
			Tags:                 item.Tags,
			Priority:             item.Priority,
			Owner:                item.Owner,
			PayloadTemplateJSON:  item.PayloadTemplateJSON,
			RequestTemplateID:    item.RequestTemplateID,
			PatchJSON:            item.PatchJSON,
			RenderMode:           item.RenderMode,
			ExpectedJSON:         item.ExpectedJSON,
			RequiredForAdmission: item.RequiredForAdmission,
			Status:               item.Status,
			SortOrder:            item.SortOrder,
			CasePath:             item.CasePath,
			SourceKind:           item.SourceKind,
			SourcePath:           item.SourcePath,
			ExecutorID:           item.ExecutorID,
			BaseURL:              item.BaseURL,
			EvidenceDir:          item.EvidenceDir,
			TimeoutSeconds:       item.TimeoutSeconds,
			DefaultOverridesJSON: jsonStringMap(item.DefaultOverrides),
		})
	}
	for _, template := range bundle.RequestTemplates {
		catalog.RequestTemplates = append(catalog.RequestTemplates, store.CatalogRequestTemplate{
			ID:           template.ID,
			DisplayName:  template.DisplayName,
			NodeID:       template.NodeID,
			Method:       template.Method,
			Path:         template.Path,
			TemplateJSON: template.TemplateJSON,
		})
	}
	for _, binding := range bundle.WorkflowBindings {
		catalog.WorkflowBindings = append(catalog.WorkflowBindings, store.CatalogWorkflowBinding{
			WorkflowID: binding.WorkflowID,
			StepID:     binding.StepID,
			NodeID:     binding.NodeID,
			CaseID:     binding.CaseID,
			Required:   binding.Required,
			SortOrder:  binding.SortOrder,
		})
	}
	for _, dependency := range bundle.CaseDependencies {
		catalog.CaseDependencies = append(catalog.CaseDependencies, store.CatalogCaseDependency{
			ID:           dependency.ID,
			CaseID:       dependency.CaseID,
			FixtureID:    dependency.FixtureID,
			MappingsJSON: dependency.MappingsJSON,
		})
	}
	for _, config := range bundle.TemplateConfigs {
		catalog.TemplateConfigs = append(catalog.TemplateConfigs, store.CatalogTemplateConfig{
			ID:          config.ID,
			TemplateID:  config.TemplateID,
			NodeID:      config.NodeID,
			WorkflowID:  config.WorkflowID,
			ScopeType:   config.ScopeType,
			ScopeID:     config.ScopeID,
			Title:       config.Title,
			Description: config.Description,
			ConfigJSON:  config.ConfigJSON,
			Status:      config.Status,
			SortOrder:   config.SortOrder,
		})
	}
	for _, fixture := range bundle.Fixtures {
		catalog.Fixtures = append(catalog.Fixtures, store.CatalogFixture{
			ID:          fixture.ID,
			DisplayName: fixture.DisplayName,
			Kind:        fixture.Kind,
			DataJSON:    fixture.DataJSON,
		})
	}
	return catalog
}

func ToBundle(catalog store.ProfileCatalog) profile.Bundle {
	bundle := profile.Bundle{
		ID:          catalog.ProfileID,
		DisplayName: catalog.ProfileID,
	}
	for _, service := range catalog.Services {
		bundle.Services = append(bundle.Services, profile.Service{
			ID:                  service.ID,
			DisplayName:         service.DisplayName,
			Kind:                service.Kind,
			AttachedTemplateIDs: service.AttachedTemplateIDs,
			GitURL:              service.GitURL,
			GitBranch:           service.GitBranch,
			RepoEnv:             service.RepoEnv,
			SourcePath:          service.SourcePath,
			ContainerName:       service.ContainerName,
			Image:               service.Image,
			DockerService:       service.DockerService,
			ServicePort:         service.ServicePort,
			ManagementPort:      service.ManagementPort,
			MemoryMb:            service.MemoryMb,
			CPUMilli:            service.CPUMilli,
			StartupCommand:      service.StartupCommand,
			HealthURL:           service.HealthURL,
			LogPath:             service.LogPath,
			Status:              service.Status,
			SortOrder:           service.SortOrder,
		})
	}
	for _, workflow := range catalog.Workflows {
		bundle.Workflows = append(bundle.Workflows, profile.Workflow{
			ID:                workflow.ID,
			DisplayName:       workflow.DisplayName,
			Description:       workflow.Description,
			BaseStepTimeoutMs: workflow.BaseStepTimeoutMs,
			TimeoutOffsetMs:   workflow.TimeoutOffsetMs,
		})
	}
	for _, node := range catalog.InterfaceNodes {
		bundle.InterfaceNodes = append(bundle.InterfaceNodes, profile.InterfaceNode{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
			Operation:   node.Operation,
			Method:      node.Method,
			Path:        node.Path,
			TemplateID:  node.TemplateID,
			Version:     node.Version,
			Status:      node.Status,
			Tags:        node.Tags,
			Description: node.Description,
			TimeoutMs:   node.TimeoutMs,
			SortOrder:   node.SortOrder,
			CreatedAt:   node.CreatedAt,
			UpdatedAt:   node.UpdatedAt,
		})
	}
	for _, item := range catalog.APICases {
		bundle.APICases = append(bundle.APICases, profile.APICase{
			ID:                   item.ID,
			DisplayName:          item.DisplayName,
			Description:          item.Description,
			NodeID:               item.NodeID,
			CaseType:             item.CaseType,
			Scenario:             item.Scenario,
			Tags:                 item.Tags,
			Priority:             item.Priority,
			Owner:                item.Owner,
			PayloadTemplateJSON:  item.PayloadTemplateJSON,
			RequestTemplateID:    item.RequestTemplateID,
			PatchJSON:            item.PatchJSON,
			RenderMode:           item.RenderMode,
			ExpectedJSON:         item.ExpectedJSON,
			RequiredForAdmission: item.RequiredForAdmission,
			Status:               item.Status,
			SortOrder:            item.SortOrder,
			CasePath:             item.CasePath,
			SourceKind:           item.SourceKind,
			SourcePath:           item.SourcePath,
			ExecutorID:           item.ExecutorID,
			BaseURL:              item.BaseURL,
			EvidenceDir:          item.EvidenceDir,
			TimeoutSeconds:       item.TimeoutSeconds,
			DefaultOverrides:     jsonMap(item.DefaultOverridesJSON),
		})
	}
	for _, template := range catalog.RequestTemplates {
		bundle.RequestTemplates = append(bundle.RequestTemplates, profile.RequestTemplate{
			ID:           template.ID,
			DisplayName:  template.DisplayName,
			NodeID:       template.NodeID,
			Method:       template.Method,
			Path:         template.Path,
			TemplateJSON: template.TemplateJSON,
		})
	}
	for _, binding := range catalog.WorkflowBindings {
		bundle.WorkflowBindings = append(bundle.WorkflowBindings, profile.WorkflowBinding{
			WorkflowID: binding.WorkflowID,
			StepID:     binding.StepID,
			NodeID:     binding.NodeID,
			CaseID:     binding.CaseID,
			Required:   binding.Required,
			SortOrder:  binding.SortOrder,
		})
	}
	for _, dependency := range catalog.CaseDependencies {
		bundle.CaseDependencies = append(bundle.CaseDependencies, profile.CaseDependency{
			ID:           dependency.ID,
			CaseID:       dependency.CaseID,
			FixtureID:    dependency.FixtureID,
			MappingsJSON: dependency.MappingsJSON,
		})
	}
	for _, fixture := range catalog.Fixtures {
		bundle.Fixtures = append(bundle.Fixtures, profile.Fixture{
			ID:          fixture.ID,
			DisplayName: fixture.DisplayName,
			Kind:        fixture.Kind,
			DataJSON:    fixture.DataJSON,
		})
	}
	for _, config := range catalog.TemplateConfigs {
		bundle.TemplateConfigs = append(bundle.TemplateConfigs, profile.TemplateConfig{
			ID:          config.ID,
			TemplateID:  config.TemplateID,
			NodeID:      config.NodeID,
			WorkflowID:  config.WorkflowID,
			ScopeType:   config.ScopeType,
			ScopeID:     config.ScopeID,
			Title:       config.Title,
			Description: config.Description,
			ConfigJSON:  config.ConfigJSON,
			Status:      config.Status,
			SortOrder:   config.SortOrder,
		})
	}
	return bundle
}

func InterfaceNodesReadModel(catalog store.ProfileCatalog, configVersionID string, generatedAt time.Time) (store.ReadModel, error) {
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
		return store.ReadModel{}, err
	}
	return store.ReadModel{
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

func interfaceNodeReadModelItems(catalog store.ProfileCatalog) []interfaceNodeReadModel {
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

func interfaceNodesPresentationForCatalog(configs []store.CatalogTemplateConfig) interfaceNodesPresentation {
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

func runtimeEnvFromBundle(bundle profile.Bundle) map[string]string {
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
