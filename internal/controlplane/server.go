package controlplane

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func New(bundle profile.Bundle) http.Handler {
	return NewWithStore(bundle, nil)
}

func NewWithStore(bundle profile.Bundle, runtime store.Store) http.Handler {
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
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, statePayloadFromBundle(bundle))
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
		handleRuns(w, r, runtime)
	})
	mux.HandleFunc("/api/workflow-runs/latest-step", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleLatestWorkflowStepRun(w, r, runtime)
	})
	mux.HandleFunc("/api/workflow-runs/step", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleWorkflowStepRun(w, r, runtime)
	})
	mux.HandleFunc("/api/workflow-runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleSaveWorkflowRun(w, r, bundle, runtime)
	})
	mux.HandleFunc("/api/workflow-runs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleWorkflowRun(w, r, runtime)
	})
	mux.HandleFunc("/api/agent-test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{
			"ok": true,
			"summary": map[string]any{
				"capabilityCount":         0,
				"profileCount":            0,
				"runCount":                0,
				"configEventCount":        0,
				"escalationEventCount":    0,
				"latestAcceptanceVerdict": "",
				"latestFailureKind":       "no active failure",
				"failureKinds":            map[string]int{},
			},
			"capabilities":      []map[string]any{},
			"profiles":          []map[string]any{},
			"agentRuns":         []map[string]any{},
			"configEvents":      []map[string]any{},
			"escalationEvents":  []map[string]any{},
			"acceptanceReports": []map[string]any{},
			"warnings":          []string{},
		})
	})
	mux.HandleFunc("/api/case/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseRuns(w, r, runtime)
	})
	mux.HandleFunc("/api/case/evidence", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseEvidence(w, r, runtime)
	})
	mux.HandleFunc("/api/case/timing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseTiming(w, r, runtime)
	})
	mux.HandleFunc("/api/case/incomplete-batches", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{
			"ok":       true,
			"dryRun":   true,
			"count":    0,
			"items":    []map[string]any{},
			"warnings": []string{},
		})
	})
	mux.HandleFunc("/api/replay/evidence", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleReplayEvidence(w, r)
	})
	mux.HandleFunc("/api/cases/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, apiCaseCapabilitiesFromBundle(bundle))
	})
	mux.HandleFunc("/api/cases/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleAPICaseRun(w, r, bundle, runtime)
	})
	mux.HandleFunc("/api/test-kit/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleTestKitRun(w, r, bundle, runtime)
	})
	mux.HandleFunc("/api/test-kit/run-batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleTestKitRunBatch(w, r, bundle)
	})
	mux.HandleFunc("/api/interface-nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, interfaceNodesPayloadFromBundle(bundle, r.URL.Query().Get("serviceId")))
	})
	mux.HandleFunc("/api/interface-node/coverage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, interfaceNodeCoveragePayloadFromBundle(bundle, r.URL.Query().Get("workflow")))
	})
	mux.HandleFunc("/api/interface-node/coverage-gaps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, interfaceNodeCoverageGapsPayloadFromBundle(bundle, r.URL.Query().Get("workflow")))
	})
	mux.HandleFunc("/api/interface-node", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		payload, ok := interfaceNodeDetailPayloadFromBundle(bundle, r.URL.Query().Get("id"))
		if !ok {
			writeJSONStatus(w, http.StatusNotFound, payload)
			return
		}
		writeJSON(w, payload)
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
	for _, name := range staticFileNames {
		name := name
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			serveStaticFile(w, r, staticDir, name)
		})
	}
	mux.Handle("/assets/react/", http.StripPrefix("/assets/react/", http.FileServer(http.Dir(filepath.Join(staticDir, "assets", "react")))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveStaticFile(w, r, staticDir, "index.html")
	})
	return mux
}

var staticFileNames = []string{
	"index.html",
	"app.js",
	"dashboard.js",
	"workflows.js",
	"agent-test.html",
	"agent-test.js",
	"agent-run.html",
	"agent-run.js",
	"api-cases.html",
	"api-cases.js",
	"case-runs.html",
	"case-runs.js",
	"evidence-viewer.html",
	"evidence-viewer.js",
	"trace-topology.html",
	"trace-topology.js",
	"replay-evidence.html",
	"replay-evidence.js",
	"trace-call.html",
	"trace-evidence.html",
	"workflow-blueprint-demo.html",
	"workflow-blueprint-new.html",
	"interface-nodes.html",
	"interface-nodes.js",
	"interface-node.html",
	"interface-node.js",
	"interface-node-history.html",
	"interface-node-fields.html",
	"environment-nodes.html",
	"environment-nodes.js",
	"environment-node.html",
	"environment-node.js",
	"service-inventory.html",
	"service-inventory.js",
	"workflow-run.html",
	"workflow-run.js",
	"workflow-detail.html",
	"workflow-detail.js",
	"workflow-step.html",
	"workflow-step.js",
	"topology-renderer.js",
	"interface-run-template.js",
	"styles.css",
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

type statePayload struct {
	Services []stateService `json:"services"`
}

type stateService struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Status string `json:"status"`
	Exists bool   `json:"exists"`
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
	Label       string          `json:"label"`
	DisplayName string          `json:"displayName"`
	Items       []dashboardItem `json:"items"`
}

type dashboardItem struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
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

type interfaceNodesPayload struct {
	Source map[string]string   `json:"source"`
	Items  []interfaceNodeItem `json:"items"`
}

type interfaceNodeItem struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"displayName,omitempty"`
	ServiceID            string `json:"serviceId,omitempty"`
	Href                 string `json:"href"`
	AdmissionStatus      string `json:"admissionStatus"`
	ValidationStatus     string `json:"validationStatus"`
	ValidationIssueCount int    `json:"validationIssueCount"`
	RequiredCaseCount    int    `json:"requiredCaseCount"`
	PassedCaseCount      int    `json:"passedCaseCount"`
}

type interfaceNodeDetailPayload struct {
	OK               bool                       `json:"ok,omitempty"`
	Error            string                     `json:"error,omitempty"`
	Requested        string                     `json:"requested,omitempty"`
	Available        []interfaceNodeItem        `json:"available,omitempty"`
	Node             interfaceNodeDetail        `json:"node,omitempty"`
	Admission        interfaceNodeAdmission     `json:"admission,omitempty"`
	RequestTemplates []interfaceRequestTemplate `json:"requestTemplates,omitempty"`
	Cases            []interfaceCase            `json:"cases"`
	Fields           interfaceNodeFields        `json:"fields"`
	History          map[string]any             `json:"history"`
	Runs             []map[string]any           `json:"runs"`
}

type interfaceNodeDetail struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	ServiceID   string `json:"serviceId,omitempty"`
	Operation   string `json:"operation,omitempty"`
	Method      string `json:"method,omitempty"`
	Path        string `json:"path,omitempty"`
}

type interfaceNodeAdmission struct {
	Status            string           `json:"status"`
	RequiredCaseCount int              `json:"requiredCaseCount"`
	PassedCaseCount   int              `json:"passedCaseCount"`
	LatestRunID       string           `json:"latestRunId,omitempty"`
	Blockers          []map[string]any `json:"blockers"`
}

type interfaceRequestTemplate struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	Version      string `json:"version,omitempty"`
	Status       string `json:"status,omitempty"`
	Method       string `json:"method,omitempty"`
	Path         string `json:"path,omitempty"`
	TemplateJSON string `json:"templateJson,omitempty"`
}

type interfaceCase struct {
	ID                   string           `json:"id"`
	Title                string           `json:"title,omitempty"`
	CaseType             string           `json:"caseType"`
	RequiredForAdmission bool             `json:"requiredForAdmission"`
	Dependencies         []map[string]any `json:"dependencies"`
}

type interfaceNodeFields struct {
	Request  []map[string]any `json:"request"`
	Response []map[string]any `json:"response"`
}

type apiCaseCapabilitiesPayload struct {
	OK    bool                `json:"ok"`
	Cases []apiCaseCapability `json:"cases"`
	Graph map[string][]string `json:"graph,omitempty"`
}

type apiCaseCapability struct {
	ID        string              `json:"id"`
	Title     string              `json:"title,omitempty"`
	Operation string              `json:"operation,omitempty"`
	Workflow  map[string]string   `json:"workflow,omitempty"`
	Graph     apiCaseServiceGraph `json:"graph"`
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
	writeJSONStatus(w, http.StatusOK, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
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
			Name:        firstNonEmpty(service.DisplayName, service.ID),
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
			Label:       "Services",
			DisplayName: "Services",
			Items:       items,
		}},
	}
}

func statePayloadFromBundle(bundle profile.Bundle) statePayload {
	services := make([]stateService, 0, len(bundle.Services))
	for _, service := range bundle.Services {
		services = append(services, stateService{
			ID:     service.ID,
			Name:   firstNonEmpty(service.DisplayName, service.ID),
			Kind:   service.Kind,
			Status: "missing",
			Exists: false,
		})
	}
	return statePayload{Services: services}
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

func interfaceNodesPayloadFromBundle(bundle profile.Bundle, serviceID string) interfaceNodesPayload {
	items := make([]interfaceNodeItem, 0, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		if serviceID != "" && node.ServiceID != serviceID {
			continue
		}
		items = append(items, interfaceNodeItem{
			ID:               node.ID,
			DisplayName:      node.DisplayName,
			ServiceID:        node.ServiceID,
			Href:             "/interface-node.html?id=" + node.ID,
			AdmissionStatus:  "pending",
			ValidationStatus: "valid",
		})
	}
	return interfaceNodesPayload{
		Source: map[string]string{"kind": "profile", "id": bundle.ID},
		Items:  items,
	}
}

func interfaceNodeDetailPayloadFromBundle(bundle profile.Bundle, id string) (interfaceNodeDetailPayload, bool) {
	for _, node := range bundle.InterfaceNodes {
		if node.ID == id {
			return interfaceNodeDetailPayloadForNode(bundle, node), true
		}
	}
	return interfaceNodeDetailPayload{
		OK:        false,
		Error:     "interface node not found",
		Requested: id,
		Available: interfaceNodesPayloadFromBundle(bundle, "").Items,
		Cases:     []interfaceCase{},
		Fields:    emptyInterfaceNodeFields(),
		History:   emptyInterfaceNodeHistory(),
		Runs:      []map[string]any{},
	}, false
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
		graph := apiCaseServiceGraph{Nodes: []apiCaseServiceNode{}, Edges: []catalogEdge{}}
		if node.ServiceID != "" {
			graph.Nodes = append(graph.Nodes, apiCaseServiceNode{
				ID:          node.ServiceID,
				DisplayName: firstNonEmpty(service.DisplayName, node.ServiceID),
				Role:        firstNonEmpty(service.Kind, "service"),
				Href:        "/environment-node.html?id=" + node.ServiceID,
			})
		}
		cases = append(cases, apiCaseCapability{
			ID:        item.ID,
			Title:     firstNonEmpty(item.DisplayName, item.ID),
			Operation: firstNonEmpty(node.DisplayName, item.NodeID),
			Workflow:  map[string]string{},
			Graph:     graph,
		})
	}
	return apiCaseCapabilitiesPayload{
		OK:    true,
		Cases: cases,
		Graph: map[string][]string{},
	}
}

func interfaceNodeDetailPayloadForNode(bundle profile.Bundle, node profile.InterfaceNode) interfaceNodeDetailPayload {
	templates := requestTemplatesForNode(bundle.RequestTemplates, node.ID)
	cases := casesForNode(bundle.APICases, bundle.CaseDependencies, node.ID)
	method, path := "", ""
	if len(templates) > 0 {
		method = templates[0].Method
		path = templates[0].Path
	}
	return interfaceNodeDetailPayload{
		Node: interfaceNodeDetail{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
			Operation:   firstNonEmpty(node.DisplayName, node.ID),
			Method:      method,
			Path:        path,
		},
		Admission: interfaceNodeAdmission{
			Status:            "pending",
			RequiredCaseCount: 0,
			PassedCaseCount:   0,
			Blockers:          []map[string]any{},
		},
		RequestTemplates: templates,
		Cases:            cases,
		Fields:           emptyInterfaceNodeFields(),
		History:          emptyInterfaceNodeHistory(),
		Runs:             []map[string]any{},
	}
}

func requestTemplatesForNode(items []profile.RequestTemplate, nodeID string) []interfaceRequestTemplate {
	templates := make([]interfaceRequestTemplate, 0)
	for _, item := range items {
		if item.NodeID != nodeID {
			continue
		}
		templates = append(templates, interfaceRequestTemplate{
			ID:           item.ID,
			Name:         item.DisplayName,
			Status:       "active",
			Method:       item.Method,
			Path:         item.Path,
			TemplateJSON: item.TemplateJSON,
		})
	}
	return templates
}

func casesForNode(items []profile.APICase, dependencies []profile.CaseDependency, nodeID string) []interfaceCase {
	dependenciesByCase := make(map[string][]map[string]any)
	for _, dependency := range dependencies {
		dependenciesByCase[dependency.CaseID] = append(dependenciesByCase[dependency.CaseID], map[string]any{
			"id":               dependency.ID,
			"fixtureProfileId": dependency.FixtureID,
			"mappingsJson":     dependency.MappingsJSON,
		})
	}
	cases := make([]interfaceCase, 0)
	for _, item := range items {
		if item.NodeID != nodeID {
			continue
		}
		cases = append(cases, interfaceCase{
			ID:                   item.ID,
			Title:                item.DisplayName,
			CaseType:             "success",
			RequiredForAdmission: false,
			Dependencies:         nonNil(dependenciesByCase[item.ID]),
		})
	}
	return cases
}

func emptyInterfaceNodeFields() interfaceNodeFields {
	return interfaceNodeFields{
		Request:  []map[string]any{},
		Response: []map[string]any{},
	}
}

func emptyInterfaceNodeHistory() map[string]any {
	return map[string]any{
		"latestRunId":         "",
		"passCount":           0,
		"failCount":           0,
		"runCount":            0,
		"latestFailureReason": "",
		"totalElapsedMs":      0,
		"perCase":             []map[string]any{},
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
