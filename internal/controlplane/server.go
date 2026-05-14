package controlplane

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"open-test-sandbox/internal/profile"
)

func New(bundle profile.Bundle) http.Handler {
	mux := http.NewServeMux()
	staticDir := findStaticDir()
	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, profilePayload{
			ID:          bundle.ID,
			DisplayName: bundle.DisplayName,
			Counts:      bundle.Counts(),
		})
	})
	mux.HandleFunc("/api/profile/assets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, profileAssetsPayload{
			Services:       nonNil(bundle.Services),
			Workflows:      nonNil(bundle.Workflows),
			InterfaceNodes: nonNil(bundle.InterfaceNodes),
			APICases:       nonNil(bundle.APICases),
		})
	})
	mux.HandleFunc("/api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, dashboardPayloadFromBundle(bundle))
	})
	mux.HandleFunc("/api/catalog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, catalogPayloadFromBundle(bundle))
	})
	mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, runsPayload{
			WorkflowRuns: []map[string]any{},
			ReplayRuns:   []map[string]any{},
			ProbeRuns:    []map[string]any{},
		})
	})
	mux.HandleFunc("/dashboard.html", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		serveStaticFile(w, r, staticDir, "dashboard.html")
	})
	mux.HandleFunc("/workflows.html", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		serveStaticFile(w, r, staticDir, "workflows.html")
	})
	mux.Handle("/assets/react/", http.StripPrefix("/assets/react/", http.FileServer(http.Dir(filepath.Join(staticDir, "assets", "react")))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard.html", http.StatusFound)
	})
	return mux
}

type profilePayload struct {
	ID          string         `json:"id"`
	DisplayName string         `json:"displayName"`
	Counts      profile.Counts `json:"counts"`
}

type profileAssetsPayload struct {
	Services       []profile.Service       `json:"services"`
	Workflows      []profile.Workflow      `json:"workflows"`
	InterfaceNodes []profile.InterfaceNode `json:"interfaceNodes"`
	APICases       []profile.APICase       `json:"apiCases"`
}

type dashboardPayload struct {
	Summary dashboardSummary `json:"summary"`
	Groups  []dashboardGroup `json:"groups"`
}

type dashboardSummary struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Missing   int `json:"missing"`
	Unhealthy int `json:"unhealthy"`
}

type dashboardGroup struct {
	ID          string          `json:"id"`
	DisplayName string          `json:"displayName"`
	Items       []dashboardItem `json:"items"`
}

type dashboardItem struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	State       string `json:"state"`
	Health      string `json:"health"`
	Kind        string `json:"kind,omitempty"`
	OK          bool   `json:"ok"`
	Branch      string `json:"branch,omitempty"`
	Profile     string `json:"profile,omitempty"`
}

type runsPayload struct {
	WorkflowRuns []map[string]any `json:"workflowRuns"`
	ReplayRuns   []map[string]any `json:"replayRuns"`
	ProbeRuns    []map[string]any `json:"probeRuns"`
}

type catalogPayload struct {
	SchemaVersion string            `json:"schemaVersion"`
	Source        map[string]string `json:"source"`
	Services      []catalogService  `json:"services"`
	Workflows     []catalogWorkflow `json:"workflows"`
	APICases      []catalogAPICase  `json:"apiCases"`
	Topology      catalogTopology   `json:"topology"`
}

type catalogService struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"displayName,omitempty"`
	Role         string   `json:"role,omitempty"`
	Port         int      `json:"port,omitempty"`
	Dependencies []string `json:"dependencies"`
}

type catalogWorkflow struct {
	ID           string                      `json:"id"`
	DisplayName  string                      `json:"displayName,omitempty"`
	Description  string                      `json:"description,omitempty"`
	Entrypoint   string                      `json:"entrypoint"`
	Steps        []catalogWorkflowStep       `json:"steps"`
	Presentation catalogWorkflowPresentation `json:"presentation"`
}

type catalogWorkflowStep struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	ServiceID   string `json:"serviceId,omitempty"`
	CaseID      string `json:"caseId,omitempty"`
	Action      string `json:"action,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type catalogWorkflowPresentation struct {
	Kind string `json:"kind"`
}

type catalogAPICase struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	NodeID      string `json:"nodeId,omitempty"`
}

type catalogTopology struct {
	Nodes []string      `json:"nodes"`
	Edges []catalogEdge `json:"edges"`
}

type catalogEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func serveStaticFile(w http.ResponseWriter, r *http.Request, staticDir string, name string) {
	path := filepath.Join(staticDir, name)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

func findStaticDir() string {
	candidates := []string{
		filepath.Join("control-plane", "static"),
		filepath.Join("..", "..", "control-plane", "static"),
	}
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; dir = filepath.Dir(dir) {
			candidates = append(candidates, filepath.Join(dir, "control-plane", "static"))
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return filepath.Join("control-plane", "static")
}

func dashboardPayloadFromBundle(bundle profile.Bundle) dashboardPayload {
	items := make([]dashboardItem, 0, len(bundle.Services))
	for _, service := range bundle.Services {
		items = append(items, dashboardItem{
			ID:          service.ID,
			DisplayName: service.DisplayName,
			State:       "missing",
			Health:      "unknown",
			Kind:        service.Kind,
			OK:          false,
			Branch:      bundle.ID,
			Profile:     bundle.ID,
		})
	}
	return dashboardPayload{
		Summary: dashboardSummary{
			Total:   len(items),
			Missing: len(items),
		},
		Groups: []dashboardGroup{{
			ID:          "business",
			DisplayName: "Services",
			Items:       items,
		}},
	}
}

func catalogPayloadFromBundle(bundle profile.Bundle) catalogPayload {
	services := make([]catalogService, 0, len(bundle.Services))
	nodes := make([]string, 0, len(bundle.Services))
	for _, service := range bundle.Services {
		nodes = append(nodes, service.ID)
		services = append(services, catalogService{
			ID:           service.ID,
			DisplayName:  service.DisplayName,
			Role:         firstNonEmpty(service.Kind, "service"),
			Dependencies: []string{},
		})
	}

	apiCases := make([]catalogAPICase, 0, len(bundle.APICases))
	for _, item := range bundle.APICases {
		apiCases = append(apiCases, catalogAPICase{
			ID:          item.ID,
			DisplayName: item.DisplayName,
			NodeID:      item.NodeID,
		})
	}

	return catalogPayload{
		SchemaVersion: "1",
		Source: map[string]string{
			"kind":        "profile",
			"id":          bundle.ID,
			"displayName": bundle.DisplayName,
		},
		Services:  services,
		Workflows: catalogWorkflows(bundle),
		APICases:  apiCases,
		Topology: catalogTopology{
			Nodes: nodes,
			Edges: []catalogEdge{},
		},
	}
}

func catalogWorkflows(bundle profile.Bundle) []catalogWorkflow {
	bindingsByWorkflow := make(map[string][]profile.WorkflowBinding)
	for _, binding := range bundle.WorkflowBindings {
		bindingsByWorkflow[binding.WorkflowID] = append(bindingsByWorkflow[binding.WorkflowID], binding)
	}
	for workflowID := range bindingsByWorkflow {
		sort.SliceStable(bindingsByWorkflow[workflowID], func(i int, j int) bool {
			return bindingsByWorkflow[workflowID][i].StepID < bindingsByWorkflow[workflowID][j].StepID
		})
	}

	nodeByID := make(map[string]profile.InterfaceNode, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		nodeByID[node.ID] = node
	}
	caseByID := make(map[string]profile.APICase, len(bundle.APICases))
	for _, item := range bundle.APICases {
		caseByID[item.ID] = item
	}

	workflows := make([]catalogWorkflow, 0, len(bundle.Workflows))
	for _, workflow := range bundle.Workflows {
		steps := make([]catalogWorkflowStep, 0, len(bindingsByWorkflow[workflow.ID]))
		for _, binding := range bindingsByWorkflow[workflow.ID] {
			node := nodeByID[binding.NodeID]
			item := caseByID[binding.CaseID]
			steps = append(steps, catalogWorkflowStep{
				ID:          firstNonEmpty(binding.StepID, binding.NodeID, binding.CaseID),
				DisplayName: firstNonEmpty(item.DisplayName, node.DisplayName, binding.StepID),
				ServiceID:   node.ServiceID,
				CaseID:      binding.CaseID,
				Action:      item.DisplayName,
				Required:    binding.Required,
			})
		}
		workflows = append(workflows, catalogWorkflow{
			ID:           workflow.ID,
			DisplayName:  workflow.DisplayName,
			Description:  workflow.Description,
			Entrypoint:   "/workflow-studio.html",
			Steps:        steps,
			Presentation: catalogWorkflowPresentation{Kind: workflowPresentationKind(steps)},
		})
	}
	return workflows
}

func workflowPresentationKind(steps []catalogWorkflowStep) string {
	if len(steps) == 0 {
		return "controlPlaneTool"
	}
	return "businessFlow"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
