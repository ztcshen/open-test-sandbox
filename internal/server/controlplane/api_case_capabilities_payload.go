package controlplane

import (
	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

type apiCaseCapabilitiesPayload struct {
	OK    bool                `json:"ok"`
	Cases []apiCaseCapability `json:"cases"`
	Graph map[string][]string `json:"graph,omitempty"`
}

type apiCaseCapability struct {
	ID               string              `json:"id"`
	Title            string              `json:"title,omitempty"`
	Operation        string              `json:"operation,omitempty"`
	CasePath         string              `json:"casePath,omitempty"`
	SourceKind       string              `json:"sourceKind,omitempty"`
	SourcePath       string              `json:"sourcePath,omitempty"`
	ExecutorID       string              `json:"executorId,omitempty"`
	BaseURL          string              `json:"baseUrl,omitempty"`
	EvidenceDir      string              `json:"evidenceDir,omitempty"`
	TimeoutSeconds   int                 `json:"timeoutSeconds,omitempty"`
	DefaultOverrides map[string]any      `json:"defaultOverrides,omitempty"`
	Workflow         map[string]string   `json:"workflow,omitempty"`
	Graph            apiCaseServiceGraph `json:"graph"`
	RunCount         int                 `json:"runCount"`
	LatestRun        map[string]any      `json:"latestRun,omitempty"`
}

type apiCaseServiceGraph struct {
	Nodes []apiCaseServiceNode `json:"nodes"`
	Edges []catalogEdge        `json:"edges"`
}

type apiCaseServiceNode struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Role        string `json:"role,omitempty"`
	Href        string `json:"href,omitempty"`
}

func apiCaseCapabilitiesFromBundle(bundle profile.Bundle) apiCaseCapabilitiesPayload {
	nodeByID := make(map[string]profile.InterfaceNode)
	for _, node := range bundle.InterfaceNodes {
		nodeByID[node.ID] = node
	}
	serviceByID := make(map[string]profile.Service)
	for _, service := range bundle.Services {
		serviceByID[service.ID] = service
	}

	cases := make([]apiCaseCapability, 0, len(bundle.APICases))
	for _, item := range bundle.APICases {
		node := nodeByID[item.NodeID]
		service := serviceByID[node.ServiceID]
		cases = append(cases, newAPICaseCapability(item.ID, item.DisplayName, item.NodeID, node.DisplayName, node.ServiceID, service.DisplayName, service.Kind, item.CasePath, item.SourceKind, item.SourcePath, item.ExecutorID, item.BaseURL, item.EvidenceDir, item.TimeoutSeconds, item.DefaultOverrides))
	}
	return apiCaseCapabilitiesPayload{
		OK:    true,
		Cases: cases,
		Graph: map[string][]string{},
	}
}

func apiCaseCapabilitiesFromCatalog(catalog store.ProfileCatalog) apiCaseCapabilitiesPayload {
	nodeByID := make(map[string]store.CatalogInterfaceNode)
	for _, node := range catalog.InterfaceNodes {
		nodeByID[node.ID] = node
	}
	serviceByID := make(map[string]store.CatalogService)
	for _, service := range catalog.Services {
		serviceByID[service.ID] = service
	}
	cases := make([]apiCaseCapability, 0, len(catalog.APICases))
	for _, item := range catalog.APICases {
		node := nodeByID[item.NodeID]
		service := serviceByID[node.ServiceID]
		cases = append(cases, newAPICaseCapability(item.ID, item.DisplayName, item.NodeID, node.DisplayName, node.ServiceID, service.DisplayName, service.Kind, item.CasePath, item.SourceKind, item.SourcePath, item.ExecutorID, item.BaseURL, item.EvidenceDir, item.TimeoutSeconds, jsonObject(item.DefaultOverridesJSON)))
	}
	return apiCaseCapabilitiesPayload{
		OK:    true,
		Cases: cases,
		Graph: map[string][]string{},
	}
}

func newAPICaseCapability(id string, displayName string, nodeID string, nodeDisplayName string, serviceID string, serviceName string, serviceKind string, casePath string, sourceKind string, sourcePath string, executorID string, baseURL string, evidenceDir string, timeoutSeconds int, defaultOverrides map[string]any) apiCaseCapability {
	return apiCaseCapability{
		ID:               id,
		Title:            firstNonEmpty(displayName, id),
		Operation:        firstNonEmpty(nodeDisplayName, nodeID),
		CasePath:         casePath,
		SourceKind:       sourceKind,
		SourcePath:       sourcePath,
		ExecutorID:       executorID,
		BaseURL:          baseURL,
		EvidenceDir:      evidenceDir,
		TimeoutSeconds:   timeoutSeconds,
		DefaultOverrides: defaultOverrides,
		Workflow:         map[string]string{},
		Graph:            apiCaseGraphForService(serviceID, serviceName, serviceKind),
	}
}

func apiCaseGraphForService(serviceID string, serviceName string, serviceKind string) apiCaseServiceGraph {
	graph := apiCaseServiceGraph{Nodes: []apiCaseServiceNode{}, Edges: []catalogEdge{}}
	if serviceID == "" {
		return graph
	}
	graph.Nodes = append(graph.Nodes, apiCaseServiceNode{
		ID:          serviceID,
		DisplayName: firstNonEmpty(serviceName, serviceID),
		Role:        firstNonEmpty(serviceKind, "service"),
		Href:        "/environment-node.html?id=" + serviceID,
	})
	return graph
}
