package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"open-test-sandbox/internal/store"
)

const currentSandboxStoreID = "current"

var (
	errSandboxStoreRequired       = errors.New("store runtime is required")
	errSandboxInvalidRegistration = errors.New("invalid sandbox registration")
)

type SandboxServiceRegistrationRequest = sandboxServiceRegistrationRequest
type SandboxServiceRegistrationResponse = sandboxServiceRegistrationResponse
type SandboxInterfaceRegistrationRequest = sandboxInterfaceRegistrationRequest
type SandboxInterfaceRegistrationResponse = sandboxInterfaceRegistrationResponse
type SandboxInterfaceCase = sandboxInterfaceCase

type sandboxServiceRegistrationRequest struct {
	ID                  string   `json:"id"`
	DisplayName         string   `json:"displayName"`
	Kind                string   `json:"kind"`
	AttachedTemplateIDs []string `json:"attachedTemplateIds"`
	GitURL              string   `json:"gitUrl"`
	GitBranch           string   `json:"gitBranch"`
	RepoEnv             string   `json:"repoEnv"`
	SourcePath          string   `json:"sourcePath"`
	ContainerName       string   `json:"containerName"`
	Image               string   `json:"image"`
	DockerService       string   `json:"dockerService"`
	ServicePort         int      `json:"servicePort"`
	ManagementPort      int      `json:"managementPort"`
	MemoryMb            int      `json:"memoryMb"`
	CPUMilli            int      `json:"cpuMilli"`
	StartupCommand      string   `json:"startupCommand"`
	HealthURL           string   `json:"healthUrl"`
	LogPath             string   `json:"logPath"`
	Status              string   `json:"status"`
	SortOrder           int      `json:"sortOrder"`
}

type sandboxServiceRegistrationResponse struct {
	OK      bool                           `json:"ok"`
	StoreID string                         `json:"storeId"`
	Service sandboxServiceRegistrationView `json:"service"`
	Counts  map[string]int                 `json:"counts"`
}

type sandboxInterfaceRegistrationRequest struct {
	ID              string                 `json:"id"`
	DisplayName     string                 `json:"displayName"`
	ServiceID       string                 `json:"serviceId"`
	Operation       string                 `json:"operation"`
	Method          string                 `json:"method"`
	Path            string                 `json:"path"`
	TemplateID      string                 `json:"templateId"`
	Version         string                 `json:"version"`
	Status          string                 `json:"status"`
	Tags            []string               `json:"tags"`
	Description     string                 `json:"description"`
	TimeoutMs       int                    `json:"timeoutMs"`
	SortOrder       int                    `json:"sortOrder"`
	RequestTemplate sandboxRequestTemplate `json:"requestTemplate"`
	Case            sandboxInterfaceCase   `json:"case"`
	CaseExecution   map[string]any         `json:"caseExecution"`
}

type sandboxRequestTemplate struct {
	ID           string         `json:"id"`
	DisplayName  string         `json:"displayName"`
	TemplateJSON map[string]any `json:"templateJson"`
	Version      string         `json:"version"`
	Status       string         `json:"status"`
	SortOrder    int            `json:"sortOrder"`
}

type sandboxInterfaceCase struct {
	ID                   string         `json:"id"`
	DisplayName          string         `json:"displayName"`
	Description          string         `json:"description"`
	CaseType             string         `json:"caseType"`
	Scenario             string         `json:"scenario"`
	Tags                 []string       `json:"tags"`
	Priority             string         `json:"priority"`
	Owner                string         `json:"owner"`
	PayloadTemplateJSON  map[string]any `json:"payloadTemplateJson"`
	RequestTemplateID    string         `json:"requestTemplateId"`
	PatchJSON            map[string]any `json:"patchJson"`
	RenderMode           string         `json:"renderMode"`
	ExpectedJSON         map[string]any `json:"expectedJson"`
	RequiredForAdmission bool           `json:"requiredForAdmission"`
	Status               string         `json:"status"`
	SortOrder            int            `json:"sortOrder"`
	TimeoutSeconds       int            `json:"timeoutSeconds"`
}

type sandboxInterfaceRegistrationResponse struct {
	OK        bool                             `json:"ok"`
	StoreID   string                           `json:"storeId"`
	Interface sandboxInterfaceRegistrationView `json:"interface"`
	Counts    map[string]int                   `json:"counts"`
}

type sandboxInterfaceRegistrationView struct {
	ID        string `json:"id"`
	ServiceID string `json:"serviceId"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	CaseID    string `json:"caseId"`
	Status    string `json:"status"`
}

type sandboxServiceRegistrationView struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	Kind           string `json:"kind"`
	ServicePort    int    `json:"servicePort,omitempty"`
	ManagementPort int    `json:"managementPort,omitempty"`
	HealthURL      string `json:"healthUrl,omitempty"`
	Status         string `json:"status"`
}

func handleSandboxServiceRegistration(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	var request sandboxServiceRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "decode sandbox service request: " + err.Error()})
		return
	}
	response, err := RegisterSandboxService(r.Context(), runtime, request)
	if err != nil {
		writeSandboxRegistrationError(w, err)
		return
	}
	writeJSON(w, response)
}

func RegisterSandboxService(ctx context.Context, runtime store.Store, request SandboxServiceRegistrationRequest) (SandboxServiceRegistrationResponse, error) {
	if runtime == nil {
		return SandboxServiceRegistrationResponse{}, errSandboxStoreRequired
	}
	service, err := catalogServiceFromRegistration(request)
	if err != nil {
		return SandboxServiceRegistrationResponse{}, fmt.Errorf("%w: %w", errSandboxInvalidRegistration, err)
	}

	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return SandboxServiceRegistrationResponse{}, err
		}
		catalog = store.ProfileCatalog{ProfileID: currentSandboxStoreID}
	}
	if strings.TrimSpace(catalog.ProfileID) == "" {
		catalog.ProfileID = currentSandboxStoreID
	}
	catalog.IndexedAt = time.Now().UTC()
	catalog.Services = upsertCatalogService(catalog.Services, service)
	if err := runtime.ReplaceProfileCatalog(ctx, catalog); err != nil {
		return SandboxServiceRegistrationResponse{}, err
	}
	return SandboxServiceRegistrationResponse{
		OK:      true,
		StoreID: catalog.ProfileID,
		Service: sandboxServiceView(service),
		Counts:  map[string]int{"services": len(catalog.Services)},
	}, nil
}

func handleSandboxInterfaceRegistration(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	var request sandboxInterfaceRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "decode sandbox interface request: " + err.Error()})
		return
	}
	response, err := RegisterSandboxInterface(r.Context(), runtime, request)
	if err != nil {
		writeSandboxRegistrationError(w, err)
		return
	}
	writeJSON(w, response)
}

func RegisterSandboxInterface(ctx context.Context, runtime store.Store, request SandboxInterfaceRegistrationRequest) (SandboxInterfaceRegistrationResponse, error) {
	if runtime == nil {
		return SandboxInterfaceRegistrationResponse{}, errSandboxStoreRequired
	}
	node, template, apiCase, config, err := catalogInterfacePartsFromRegistration(request)
	if err != nil {
		return SandboxInterfaceRegistrationResponse{}, fmt.Errorf("%w: %w", errSandboxInvalidRegistration, err)
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return SandboxInterfaceRegistrationResponse{}, err
		}
		catalog = store.ProfileCatalog{ProfileID: currentSandboxStoreID}
	}
	if strings.TrimSpace(catalog.ProfileID) == "" {
		catalog.ProfileID = currentSandboxStoreID
	}
	if !catalogHasService(catalog.Services, node.ServiceID) {
		return SandboxInterfaceRegistrationResponse{}, fmt.Errorf("%w: entry service is not registered: %s", errSandboxInvalidRegistration, node.ServiceID)
	}
	catalog.IndexedAt = time.Now().UTC()
	catalog.InterfaceNodes = upsertCatalogInterfaceNode(catalog.InterfaceNodes, node)
	catalog.RequestTemplates = upsertCatalogRequestTemplate(catalog.RequestTemplates, template)
	catalog.APICases = upsertCatalogAPICase(catalog.APICases, apiCase)
	catalog.TemplateConfigs = upsertCatalogTemplateConfig(catalog.TemplateConfigs, config)
	if err := runtime.ReplaceProfileCatalog(ctx, catalog); err != nil {
		return SandboxInterfaceRegistrationResponse{}, err
	}
	return SandboxInterfaceRegistrationResponse{
		OK:        true,
		StoreID:   catalog.ProfileID,
		Interface: sandboxInterfaceView(node, apiCase),
		Counts: map[string]int{
			"interfaceNodes":   len(catalog.InterfaceNodes),
			"requestTemplates": len(catalog.RequestTemplates),
			"apiCases":         len(catalog.APICases),
			"templateConfigs":  len(catalog.TemplateConfigs),
		},
	}, nil
}

func writeSandboxRegistrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSandboxStoreRequired):
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": err.Error()})
	case errors.Is(err, errSandboxInvalidRegistration):
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": strings.TrimPrefix(err.Error(), errSandboxInvalidRegistration.Error()+": ")})
	default:
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
	}
}

func catalogServiceFromRegistration(request sandboxServiceRegistrationRequest) (store.CatalogService, error) {
	id := strings.TrimSpace(request.ID)
	if id == "" {
		return store.CatalogService{}, errors.New("service id is required")
	}
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = "active"
	}
	kind := strings.TrimSpace(request.Kind)
	if kind == "" {
		kind = "http"
	}
	displayName := strings.TrimSpace(request.DisplayName)
	if displayName == "" {
		displayName = id
	}
	return store.CatalogService{
		ID:                  id,
		DisplayName:         displayName,
		Kind:                kind,
		AttachedTemplateIDs: request.AttachedTemplateIDs,
		GitURL:              strings.TrimSpace(request.GitURL),
		GitBranch:           strings.TrimSpace(request.GitBranch),
		RepoEnv:             strings.TrimSpace(request.RepoEnv),
		SourcePath:          strings.TrimSpace(request.SourcePath),
		ContainerName:       strings.TrimSpace(request.ContainerName),
		Image:               strings.TrimSpace(request.Image),
		DockerService:       strings.TrimSpace(request.DockerService),
		ServicePort:         request.ServicePort,
		ManagementPort:      request.ManagementPort,
		MemoryMb:            request.MemoryMb,
		CPUMilli:            request.CPUMilli,
		StartupCommand:      strings.TrimSpace(request.StartupCommand),
		HealthURL:           strings.TrimSpace(request.HealthURL),
		LogPath:             strings.TrimSpace(request.LogPath),
		Status:              status,
		SortOrder:           request.SortOrder,
	}, nil
}

func catalogInterfacePartsFromRegistration(request sandboxInterfaceRegistrationRequest) (store.CatalogInterfaceNode, store.CatalogRequestTemplate, store.CatalogAPICase, store.CatalogTemplateConfig, error) {
	id := strings.TrimSpace(request.ID)
	if id == "" {
		return store.CatalogInterfaceNode{}, store.CatalogRequestTemplate{}, store.CatalogAPICase{}, store.CatalogTemplateConfig{}, errors.New("interface id is required")
	}
	serviceID := strings.TrimSpace(request.ServiceID)
	if serviceID == "" {
		return store.CatalogInterfaceNode{}, store.CatalogRequestTemplate{}, store.CatalogAPICase{}, store.CatalogTemplateConfig{}, errors.New("serviceId is required")
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = "GET"
	}
	path := strings.TrimSpace(request.Path)
	if path == "" {
		return store.CatalogInterfaceNode{}, store.CatalogRequestTemplate{}, store.CatalogAPICase{}, store.CatalogTemplateConfig{}, errors.New("path is required")
	}
	status := strings.TrimSpace(request.Status)
	if status == "" {
		status = "active"
	}
	displayName := strings.TrimSpace(request.DisplayName)
	if displayName == "" {
		displayName = id
	}
	templateID := firstNonEmpty(strings.TrimSpace(request.RequestTemplate.ID), strings.TrimSpace(request.TemplateID), id+".template")
	caseID := strings.TrimSpace(request.Case.ID)
	if caseID == "" {
		caseID = id + ".default"
	}
	caseStatus := strings.TrimSpace(request.Case.Status)
	if caseStatus == "" {
		caseStatus = "active"
	}
	configJSON := compactJSON(map[string]any{
		"caseId":        caseID,
		"caseExecution": request.CaseExecution,
	})
	node := store.CatalogInterfaceNode{
		ID:          id,
		DisplayName: displayName,
		ServiceID:   serviceID,
		Operation:   strings.TrimSpace(request.Operation),
		Method:      method,
		Path:        path,
		TemplateID:  templateID,
		Version:     strings.TrimSpace(request.Version),
		Status:      status,
		Tags:        request.Tags,
		Description: strings.TrimSpace(request.Description),
		TimeoutMs:   request.TimeoutMs,
		SortOrder:   request.SortOrder,
	}
	template := store.CatalogRequestTemplate{
		ID:           templateID,
		DisplayName:  firstNonEmpty(strings.TrimSpace(request.RequestTemplate.DisplayName), displayName+" Template"),
		NodeID:       id,
		Method:       method,
		Path:         path,
		TemplateJSON: compactJSON(request.RequestTemplate.TemplateJSON),
		Version:      strings.TrimSpace(request.RequestTemplate.Version),
		Status:       firstNonEmpty(strings.TrimSpace(request.RequestTemplate.Status), "active"),
		SortOrder:    request.RequestTemplate.SortOrder,
	}
	apiCase := store.CatalogAPICase{
		ID:                   caseID,
		DisplayName:          firstNonEmpty(strings.TrimSpace(request.Case.DisplayName), caseID),
		Description:          strings.TrimSpace(request.Case.Description),
		NodeID:               id,
		CaseType:             strings.TrimSpace(request.Case.CaseType),
		Scenario:             strings.TrimSpace(request.Case.Scenario),
		Tags:                 request.Case.Tags,
		Priority:             strings.TrimSpace(request.Case.Priority),
		Owner:                strings.TrimSpace(request.Case.Owner),
		PayloadTemplateJSON:  compactJSON(request.Case.PayloadTemplateJSON),
		RequestTemplateID:    firstNonEmpty(strings.TrimSpace(request.Case.RequestTemplateID), templateID),
		PatchJSON:            compactJSON(request.Case.PatchJSON),
		RenderMode:           strings.TrimSpace(request.Case.RenderMode),
		ExpectedJSON:         compactJSON(request.Case.ExpectedJSON),
		RequiredForAdmission: request.Case.RequiredForAdmission,
		Status:               caseStatus,
		SortOrder:            request.Case.SortOrder,
		TimeoutSeconds:       request.Case.TimeoutSeconds,
	}
	config := store.CatalogTemplateConfig{
		ID:         "config." + caseID,
		TemplateID: templateID,
		NodeID:     id,
		ScopeType:  "case",
		ScopeID:    caseID,
		Title:      "Execution " + caseID,
		Status:     "active",
		ConfigJSON: configJSON,
	}
	return node, template, apiCase, config, nil
}

func upsertCatalogService(services []store.CatalogService, service store.CatalogService) []store.CatalogService {
	for index := range services {
		if services[index].ID == service.ID {
			services[index] = service
			return services
		}
	}
	return append(services, service)
}

func catalogHasService(services []store.CatalogService, serviceID string) bool {
	for _, service := range services {
		if service.ID == serviceID {
			return true
		}
	}
	return false
}

func upsertCatalogInterfaceNode(nodes []store.CatalogInterfaceNode, node store.CatalogInterfaceNode) []store.CatalogInterfaceNode {
	for index := range nodes {
		if nodes[index].ID == node.ID {
			nodes[index] = node
			return nodes
		}
	}
	return append(nodes, node)
}

func upsertCatalogRequestTemplate(templates []store.CatalogRequestTemplate, template store.CatalogRequestTemplate) []store.CatalogRequestTemplate {
	for index := range templates {
		if templates[index].ID == template.ID {
			templates[index] = template
			return templates
		}
	}
	return append(templates, template)
}

func upsertCatalogAPICase(cases []store.CatalogAPICase, apiCase store.CatalogAPICase) []store.CatalogAPICase {
	for index := range cases {
		if cases[index].ID == apiCase.ID {
			cases[index] = apiCase
			return cases
		}
	}
	return append(cases, apiCase)
}

func upsertCatalogTemplateConfig(configs []store.CatalogTemplateConfig, config store.CatalogTemplateConfig) []store.CatalogTemplateConfig {
	for index := range configs {
		if configs[index].ID == config.ID {
			configs[index] = config
			return configs
		}
	}
	return append(configs, config)
}

func sandboxServiceView(service store.CatalogService) sandboxServiceRegistrationView {
	return sandboxServiceRegistrationView{
		ID:             service.ID,
		DisplayName:    service.DisplayName,
		Kind:           service.Kind,
		ServicePort:    service.ServicePort,
		ManagementPort: service.ManagementPort,
		HealthURL:      service.HealthURL,
		Status:         service.Status,
	}
}

func sandboxInterfaceView(node store.CatalogInterfaceNode, apiCase store.CatalogAPICase) sandboxInterfaceRegistrationView {
	return sandboxInterfaceRegistrationView{
		ID:        node.ID,
		ServiceID: node.ServiceID,
		Method:    node.Method,
		Path:      node.Path,
		CaseID:    apiCase.ID,
		Status:    node.Status,
	}
}
