package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/domain/profilecatalog"
	"agent-testbench/internal/runner/executor"
	"agent-testbench/internal/store"
)

func New(bundle profile.Bundle) http.Handler {
	return NewWithStore(bundle, nil)
}

func NewWithStore(bundle profile.Bundle, runtime store.Store) http.Handler {
	return NewWithOptions(bundle, Options{Runtime: runtime})
}

type Options struct {
	Runtime         store.Store
	TraceGraphQLURL string
	ProfileHome     string
	StoreInfo       StoreInfo
}

type StoreInfo struct {
	Configured bool   `json:"configured"`
	Name       string `json:"name,omitempty"`
	Backend    string `json:"backend,omitempty"`
	URL        string `json:"url,omitempty"`
	Source     string `json:"source,omitempty"`
}

type storeCurrentPayload struct {
	OK bool `json:"ok"`
	StoreInfo
}

func NewWithOptions(bundle profile.Bundle, options Options) http.Handler {
	mux := http.NewServeMux()
	staticDir := findStaticDir()
	profiles := newProfileState(bundle)
	runtime := options.Runtime
	collector := traceCollector{GraphQLURL: options.TraceGraphQLURL}
	caseBatchRunner := newAPICaseBatchRunner()
	mux.HandleFunc("/api/template-packages/import-plan/openapi", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleOpenAPIImportPlan(w, r)
	})
	mux.HandleFunc("/api/template-packages/import-plan/http-capture", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleHTTPCaptureImportPlan(w, r)
	})
	mux.HandleFunc("/api/template-packages/generation-plan/openapi", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleOpenAPIGenerationPlan(w, r)
	})
	mux.HandleFunc("/api/template-packages/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileImport(w, r, runtime, profiles.Replace, options.ProfileHome)
	})
	mux.HandleFunc("/api/template-packages/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileVerify(w, r, runtime, profiles.Replace, options.ProfileHome)
	})
	mux.HandleFunc("/api/template-packages/audit-plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileAuditPlan(w, r, runtime, options.ProfileHome)
	})
	mux.HandleFunc("/api/template-packages/install", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileInstall(w, r, options.ProfileHome)
	})
	mux.HandleFunc("/api/template-packages/installed", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleInstalledProfiles(w, r, options.ProfileHome)
	})
	mux.HandleFunc("/api/template-packages/catalog-index", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileCatalogIndex(w, r, runtime)
	})
	mux.HandleFunc("/api/template-packages/current", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeProfileSummary(w, profiles.Current())
	})
	mux.HandleFunc("/api/template-packages/assets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeProfileAssets(w, profiles.Current())
	})
	mux.HandleFunc("/api/profile/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileImport(w, r, runtime, profiles.Replace, options.ProfileHome)
	})
	mux.HandleFunc("/api/profile/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileVerify(w, r, runtime, profiles.Replace, options.ProfileHome)
	})
	mux.HandleFunc("/api/profile/audit-plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileAuditPlan(w, r, runtime, options.ProfileHome)
	})
	mux.HandleFunc("/api/profile/install", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileInstall(w, r, options.ProfileHome)
	})
	mux.HandleFunc("/api/profile/installed", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleInstalledProfiles(w, r, options.ProfileHome)
	})
	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeProfileSummary(w, profiles.Current())
	})
	mux.HandleFunc("/api/profile/assets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeProfileAssets(w, profiles.Current())
	})
	mux.HandleFunc("/api/profile/catalog-index", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleProfileCatalogIndex(w, r, runtime)
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, statePayloadFromBundle(profiles.Current()))
	})
	mux.HandleFunc("/api/store/current", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, storeCurrentPayload{OK: true, StoreInfo: options.StoreInfo})
	})
	mux.HandleFunc("/api/sandbox/services", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleSandboxServiceRegistration(w, r, runtime)
	})
	mux.HandleFunc("/api/sandbox/interfaces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleSandboxInterfaceRegistration(w, r, runtime)
	})
	mux.HandleFunc("/api/environments/", func(w http.ResponseWriter, r *http.Request) {
		handleEnvironmentItem(w, r, runtime, profiles.Current(), caseBatchRunner, collector)
	})
	mux.HandleFunc("/api/environments", func(w http.ResponseWriter, r *http.Request) {
		handleEnvironmentCollection(w, r, runtime)
	})
	mux.HandleFunc("/api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		payload, err := dashboardPayloadFromBundleWithStore(r.Context(), profiles.Current(), runtime)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, payload)
	})
	mux.HandleFunc("/api/catalog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		payload, err := catalogPayloadFromBundleWithStore(r.Context(), profiles.Current(), runtime)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, payload)
	})
	mux.HandleFunc("/api/workflow-audit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleWorkflowAudit(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/workflow-plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleWorkflowPlan(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/workflows", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleWorkflowDiscovery(w, r, profiles.Current(), runtime)
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
		handleSaveWorkflowRun(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/workflow-runs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleWorkflowRun(w, r, runtime)
	})
	mux.HandleFunc("/api/trace-topology/collect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleTraceTopologyCollect(w, r, runtime, collector)
	})
	mux.HandleFunc("/api/agent-test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleAgentTestWorkbench(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/executor/plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		report, err := executor.PlanWithStore(r.Context(), profiles.Current(), runtime)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, report)
	})
	mux.HandleFunc("/api/evidence/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleEvidenceList(w, r, runtime)
	})
	mux.HandleFunc("/api/evidence/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleEvidenceImport(w, r, runtime, profiles.Current())
	})
	mux.HandleFunc("/api/baseline/gate", func(w http.ResponseWriter, r *http.Request) {
		handleBaselineGate(w, r, runtime, profiles.Current())
	})
	mux.HandleFunc("/api/template/render", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleTemplateRender(w, r, profiles.Current())
	})
	mux.HandleFunc("/api/case/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseRuns(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/evidence", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseEvidence(w, r, runtime)
	})
	mux.HandleFunc("/api/case-run/evidence", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseRunEvidence(w, r, runtime)
	})
	mux.HandleFunc("/api/case/timing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseTiming(w, r, runtime)
	})
	mux.HandleFunc("/api/post-process-tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handlePostProcessTasks(w, r, runtime)
	})
	mux.HandleFunc("/api/case/incomplete-batches", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseIncompleteBatches(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-coverage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuiteCoverage(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-inspection", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuiteInspection(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuitePlan(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-stability", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuiteStability(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-priority", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuitePriority(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-brief", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuiteBrief(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-quality", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuiteQuality(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-quality-plan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuiteQualityPlan(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-impact", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuiteImpact(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-impact-runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleCaseSuiteImpactRun(w, r, profiles.Current(), runtime, caseBatchRunner, collector)
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
		payload, err := apiCaseCapabilitiesFromBundleWithStore(r.Context(), profiles.Current(), runtime)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, payload)
	})
	mux.HandleFunc("/api/cases/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleAPICaseRun(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/cases/batch-runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleAPICaseBatchRunStart(w, r, profiles.Current(), runtime, caseBatchRunner, collector)
	})
	mux.HandleFunc("/api/cases/batch-runs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleAPICaseBatchRunReport(w, r, caseBatchRunner)
	})
	mux.HandleFunc("/api/test-kit/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleTestKitRun(w, r, profiles.Current(), runtime, collector)
	})
	mux.HandleFunc("/api/test-kit/run-batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handleTestKitRunBatch(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/interface-nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		filter := r.URL.Query().Get("filter")
		if runtime != nil {
			catalog, err := runtime.GetProfileCatalog(r.Context())
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if err == nil && len(catalog.InterfaceNodes) > 0 {
				payload, err := interfaceNodesPayloadFromStore(r.Context(), catalog, runtime, r.URL.Query().Get("serviceId"), r.URL.Query().Get("operation"))
				if err != nil {
					writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
					return
				}
				applyInterfaceNodeSearchFilter(&payload, filter)
				writeJSON(w, payload)
				return
			}
		}
		payload := interfaceNodesPayloadFromBundle(profiles.Current(), r.URL.Query().Get("serviceId"), r.URL.Query().Get("operation"))
		applyInterfaceNodeSearchFilter(&payload, filter)
		writeJSON(w, payload)
	})
	mux.HandleFunc("/api/interface-node/coverage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		payload, err := interfaceNodeCoveragePayloadFromBundleWithStore(r.Context(), profiles.Current(), r.URL.Query().Get("workflow"), runtime)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, payload)
	})
	mux.HandleFunc("/api/interface-node/coverage-gaps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		payload, err := interfaceNodeCoverageGapsPayloadFromBundleWithStore(r.Context(), profiles.Current(), r.URL.Query().Get("workflow"), runtime)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, payload)
	})
	mux.HandleFunc("/api/interface-node", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		payload, ok, err := interfaceNodeDetailPayloadFromBundleWithStore(r.Context(), profiles.Current(), r.URL.Query().Get("id"), runtime, interfaceNodeRunContextFromQuery(r.URL.Query()))
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
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

type profileState struct {
	mu     sync.RWMutex
	bundle profile.Bundle
}

func newProfileState(bundle profile.Bundle) *profileState {
	return &profileState{bundle: bundle}
}

func (s *profileState) Current() profile.Bundle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bundle
}

func (s *profileState) Replace(bundle profile.Bundle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bundle = bundle
}

var staticFileNames = []string{
	"index.html",
	"agent-run.html",
	"api-cases.html",
	"case-runs.html",
	"evidence-viewer.html",
	"trace-topology.html",
	"replay-evidence.html",
	"trace-call.html",
	"trace-evidence.html",
	"workflow-blueprint-demo.html",
	"workflow-blueprint-new.html",
	"interface-nodes.html",
	"interface-node.html",
	"interface-node-history.html",
	"interface-node-fields.html",
	"environment-nodes.html",
	"environment-node.html",
	"service-inventory.html",
	"workflow-run.html",
	"workflow-detail.html",
	"workflow-step.html",
	"styles.css",
}

type profilePayload struct {
	TemplatePackageID string         `json:"templatePackageId"`
	ID                string         `json:"id"`
	DisplayName       string         `json:"displayName"`
	Counts            profile.Counts `json:"counts"`
}

type profileAssetsPayload struct {
	TemplatePackageID string                  `json:"templatePackageId"`
	Services          []profile.Service       `json:"services"`
	Workflows         []profile.Workflow      `json:"workflows"`
	InterfaceNodes    []profile.InterfaceNode `json:"interfaceNodes"`
	APICases          []profile.APICase       `json:"apiCases"`
}

func writeProfileSummary(w http.ResponseWriter, bundle profile.Bundle) {
	writeJSON(w, profilePayload{
		TemplatePackageID: bundle.ID,
		ID:                bundle.ID,
		DisplayName:       bundle.DisplayName,
		Counts:            bundle.Counts(),
	})
}

func writeProfileAssets(w http.ResponseWriter, bundle profile.Bundle) {
	writeJSON(w, profileAssetsPayload{
		TemplatePackageID: bundle.ID,
		Services:          nonNil(bundle.Services),
		Workflows:         nonNil(bundle.Workflows),
		InterfaceNodes:    nonNil(bundle.InterfaceNodes),
		APICases:          nonNil(bundle.APICases),
	})
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
	OK             bool                  `json:"ok"`
	Source         map[string]string     `json:"source,omitempty"`
	Summary        dashboardSummary      `json:"summary"`
	Groups         []dashboardGroup      `json:"groups"`
	ServiceRuntime []serviceRuntime      `json:"serviceRuntime"`
	Presentation   dashboardPresentation `json:"presentation,omitempty"`
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
	ID             string                `json:"id"`
	Name           string                `json:"name,omitempty"`
	DisplayName    string                `json:"displayName,omitempty"`
	State          string                `json:"state"`
	Health         string                `json:"health"`
	Kind           string                `json:"kind,omitempty"`
	OK             bool                  `json:"ok"`
	Branch         string                `json:"branch,omitempty"`
	Profile        string                `json:"profile,omitempty"`
	Container      string                `json:"container,omitempty"`
	Image          string                `json:"image,omitempty"`
	Port           int                   `json:"port,omitempty"`
	ManagementPort int                   `json:"managementPort,omitempty"`
	Message        string                `json:"message,omitempty"`
	Presentation   dashboardPresentation `json:"presentation,omitempty"`
}

type dashboardPresentation struct {
	Copy map[string]string `json:"copy,omitempty"`
}

type runsPayload struct {
	OK           bool             `json:"ok"`
	WorkflowRuns []map[string]any `json:"workflowRuns"`
	ReplayRuns   []map[string]any `json:"replayRuns"`
	ProbeRuns    []map[string]any `json:"probeRuns"`
}

type interfaceNodesPayload struct {
	OK           bool                      `json:"ok"`
	TemplateID   string                    `json:"templateId"`
	Filters      map[string]string         `json:"filters"`
	Source       map[string]string         `json:"source"`
	Items        []interfaceNodeItem       `json:"items"`
	Presentation interfaceNodePresentation `json:"presentation,omitempty"`
}

type interfaceNodeItem struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"displayName,omitempty"`
	ServiceID            string `json:"serviceId,omitempty"`
	Operation            string `json:"operation,omitempty"`
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

type interfaceNodeDetailPayload struct {
	OK               bool                       `json:"ok"`
	TemplateID       string                     `json:"templateId"`
	Source           map[string]string          `json:"source"`
	Context          map[string]string          `json:"context,omitempty"`
	Error            string                     `json:"error,omitempty"`
	Requested        string                     `json:"requested,omitempty"`
	Available        []interfaceNodeItem        `json:"available,omitempty"`
	Attention        map[string]any             `json:"attention,omitempty"`
	Node             interfaceNodeDetail        `json:"node,omitempty"`
	Admission        interfaceNodeAdmission     `json:"admission,omitempty"`
	RequestTemplates []interfaceRequestTemplate `json:"requestTemplates"`
	Cases            []interfaceCase            `json:"cases"`
	Fields           interfaceNodeFields        `json:"fields"`
	History          map[string]any             `json:"history"`
	Runs             []map[string]any           `json:"runs"`
	Presentation     interfaceNodePresentation  `json:"presentation,omitempty"`
}

type interfaceNodePresentation struct {
	Copy map[string]string `json:"copy,omitempty"`
}

type interfaceNodeDetail struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName,omitempty"`
	ServiceID   string   `json:"serviceId,omitempty"`
	Operation   string   `json:"operation,omitempty"`
	Method      string   `json:"method,omitempty"`
	Path        string   `json:"path,omitempty"`
	TimeoutMs   int      `json:"timeoutMs,omitempty"`
	TemplateID  string   `json:"templateId,omitempty"`
	Version     string   `json:"version,omitempty"`
	Status      string   `json:"status,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Description string   `json:"description,omitempty"`
	SortOrder   int      `json:"sortOrder,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
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
	Blocked              bool             `json:"blocked"`
	BlockedReason        string           `json:"blockedReason"`
	Scenario             string           `json:"scenario"`
	PayloadTemplateJSON  string           `json:"payloadTemplateJson,omitempty"`
	RequestTemplateID    string           `json:"requestTemplateId"`
	PatchJSON            string           `json:"patchJson,omitempty"`
	RenderMode           string           `json:"renderMode,omitempty"`
	ExpectedJSON         string           `json:"expectedJson,omitempty"`
	Status               string           `json:"status,omitempty"`
	SortOrder            int              `json:"sortOrder,omitempty"`
	RequiredForAdmission bool             `json:"requiredForAdmission"`
	Dependencies         []map[string]any `json:"dependencies"`
	LatestRun            map[string]any   `json:"latestRun,omitempty"`
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

type catalogPayload struct {
	SchemaVersion string               `json:"schemaVersion"`
	OK            bool                 `json:"ok"`
	GeneratedAt   time.Time            `json:"generatedAt"`
	Navigation    map[string]any       `json:"navigation"`
	Warnings      []string             `json:"warnings"`
	Source        map[string]string    `json:"source"`
	Presentation  *catalogPresentation `json:"presentation,omitempty"`
	Services      []catalogService     `json:"services"`
	Workflows     []catalogWorkflow    `json:"workflows"`
	APICases      []catalogAPICase     `json:"apiCases"`
	Topology      catalogTopology      `json:"topology"`
}

type catalogPresentation struct {
	WorkflowFinder *catalogWorkflowFinderConfig `json:"workflowFinder,omitempty"`
}

type catalogWorkflowFinderConfig struct {
	TargetStepCount      int    `json:"targetStepCount,omitempty"`
	TargetInterfaceCount int    `json:"targetInterfaceCount,omitempty"`
	TargetLabel          string `json:"targetLabel,omitempty"`
}

type catalogService struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"displayName,omitempty"`
	Role         string   `json:"role,omitempty"`
	Port         int      `json:"port,omitempty"`
	Dependencies []string `json:"dependencies"`
}

type catalogWorkflow struct {
	ID                string                       `json:"id"`
	DisplayName       string                       `json:"displayName,omitempty"`
	Description       string                       `json:"description,omitempty"`
	Entrypoint        string                       `json:"entrypoint"`
	BaseStepTimeoutMs int                          `json:"baseStepTimeoutMs"`
	TimeoutOffsetMs   int                          `json:"timeoutOffsetMs"`
	TimeoutMs         int                          `json:"timeoutMs"`
	Graph             catalogTopology              `json:"graph,omitempty"`
	Observability     catalogWorkflowObservability `json:"observability,omitempty"`
	Steps             []catalogWorkflowStep        `json:"steps"`
	StepCount         int                          `json:"stepCount,omitempty"`
	CaseCount         int                          `json:"caseCount,omitempty"`
	ServiceCount      int                          `json:"serviceCount,omitempty"`
	Presentation      catalogWorkflowPresentation  `json:"presentation"`
	RunCount          int                          `json:"runCount"`
	LatestRun         map[string]any               `json:"latestRun,omitempty"`
}

type catalogWorkflowStep struct {
	ID                 string                  `json:"id"`
	DisplayName        string                  `json:"displayName,omitempty"`
	ServiceID          string                  `json:"serviceId,omitempty"`
	CaseID             string                  `json:"caseId,omitempty"`
	Action             string                  `json:"action,omitempty"`
	Required           bool                    `json:"required,omitempty"`
	Executable         bool                    `json:"executable"`
	EvidenceKinds      []string                `json:"evidenceKinds,omitempty"`
	RelatedMockTargets []string                `json:"relatedMockTargets,omitempty"`
	Inputs             []map[string]any        `json:"inputs,omitempty"`
	Exports            []map[string]any        `json:"exports,omitempty"`
	TimeoutMs          int                     `json:"timeoutMs,omitempty"`
	Presentation       catalogStepPresentation `json:"presentation,omitempty"`
}

type catalogStepPresentation struct {
	Copy map[string]string `json:"copy,omitempty"`
}

type catalogWorkflowPresentation struct {
	Kind     string                 `json:"kind,omitempty"`
	Template string                 `json:"template,omitempty"`
	Title    string                 `json:"title,omitempty"`
	Copy     map[string]string      `json:"copy,omitempty"`
	Stages   []catalogWorkflowStage `json:"stages,omitempty"`
}

type catalogWorkflowObservability struct {
	Panels []catalogWorkflowPanel `json:"panels,omitempty"`
}

type catalogWorkflowPanel struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
	Scope string `json:"scope,omitempty"`
}

type catalogWorkflowStage struct {
	ID      string                     `json:"id"`
	Title   string                     `json:"title,omitempty"`
	Summary string                     `json:"summary,omitempty"`
	Steps   []catalogWorkflowStageStep `json:"steps,omitempty"`
}

type catalogWorkflowStageStep struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	CaseID string `json:"caseId,omitempty"`
}

type catalogAPICase struct {
	ID               string         `json:"id"`
	DisplayName      string         `json:"displayName,omitempty"`
	NodeID           string         `json:"nodeId,omitempty"`
	CasePath         string         `json:"casePath,omitempty"`
	SourceKind       string         `json:"sourceKind,omitempty"`
	SourcePath       string         `json:"sourcePath,omitempty"`
	ExecutorID       string         `json:"executorId,omitempty"`
	BaseURL          string         `json:"baseUrl,omitempty"`
	EvidenceDir      string         `json:"evidenceDir,omitempty"`
	TimeoutSeconds   int            `json:"timeoutSeconds,omitempty"`
	DefaultOverrides map[string]any `json:"defaultOverrides,omitempty"`
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

const ReadModelDashboard = "dashboard"

func DashboardReadModel(catalog store.ProfileCatalog, configVersionID string, generatedAt time.Time) (store.ReadModel, error) {
	payload := dashboardPayloadFromCatalog(catalog)
	payload.Source = map[string]string{"kind": "read-model", "id": catalog.ProfileID}
	raw, err := json.Marshal(payload)
	if err != nil {
		return store.ReadModel{}, err
	}
	return store.ReadModel{
		ProfileID:       catalog.ProfileID,
		Key:             ReadModelDashboard,
		ConfigVersionID: configVersionID,
		PayloadJSON:     string(raw),
		GeneratedAt:     generatedAt,
		UpdatedAt:       generatedAt,
	}, nil
}

func dashboardPayloadFromBundleWithStore(ctx context.Context, bundle profile.Bundle, runtime store.Store) (dashboardPayload, error) {
	if runtime == nil {
		return dashboardPayloadFromBundle(ctx, bundle), nil
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return dashboardPayload{}, err
	}
	if err == nil && len(catalog.Services) > 0 {
		payload, ok, err := dashboardPayloadFromReadModel(ctx, runtime, catalog.ProfileID)
		if err != nil {
			return dashboardPayload{}, err
		}
		if !ok {
			payload = dashboardPayloadFromCatalog(catalog)
		}
		hydrateDashboardRuntime(ctx, runtime, &payload, catalog)
		return payload, nil
	}
	return dashboardPayloadFromBundle(ctx, bundle), nil
}

func dashboardPayloadFromReadModel(ctx context.Context, runtime store.Store, profileID string) (dashboardPayload, bool, error) {
	model, err := runtime.GetReadModel(ctx, profileID, ReadModelDashboard)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return dashboardPayload{}, false, nil
		}
		return dashboardPayload{}, false, err
	}
	var payload dashboardPayload
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		return dashboardPayload{}, false, err
	}
	payload.Source = map[string]string{"kind": "read-model", "id": profileID}
	return payload, true, nil
}

func dashboardPayloadFromCatalog(catalog store.ProfileCatalog) dashboardPayload {
	services := activeCatalogServices(catalog.Services)
	items := make([]dashboardItem, 0, len(services))
	serviceRuntimes := make([]serviceRuntime, 0, len(services))
	for _, service := range services {
		state := "missing"
		health := "unknown"
		healthy := false
		if strings.EqualFold(service.Kind, "external") {
			state = "external"
			health = "external"
			healthy = true
		}
		runtime := serviceRuntimeFromCatalogService(service, state, health, healthy)
		if runtime.ServiceID != "" {
			serviceRuntimes = append(serviceRuntimes, runtime)
		}
		items = append(items, dashboardItemFromCatalogService(catalog, service, runtime, state, health, healthy))
	}
	return dashboardPayload{
		OK:             true,
		Source:         map[string]string{"kind": "store", "id": catalog.ProfileID},
		Summary:        dashboardSummaryForItems(items),
		Groups:         []dashboardGroup{{ID: "business", Label: "Services", DisplayName: "Services", Items: items}},
		ServiceRuntime: serviceRuntimes,
		Presentation:   dashboardPresentationForCatalog(catalog.TemplateConfigs, ""),
	}
}

func hydrateDashboardRuntime(ctx context.Context, runtime store.Store, payload *dashboardPayload, catalog store.ProfileCatalog) {
	services := activeCatalogServices(catalog.Services)
	filterDashboardPayloadServices(payload, services)
	dockerRuntimes := dockerRuntimeByCatalogService(ctx, services)
	componentHealthURLByService := dashboardComponentHealthURLByService(ctx, runtime, services)
	runtimeByService := map[string]serviceRuntime{}
	for _, runtime := range payload.ServiceRuntime {
		runtimeByService[runtime.ServiceID] = runtime
	}
	for _, service := range services {
		runtime := runtimeByService[service.ID]
		if observed, ok := dockerRuntimes[service.ID]; ok {
			runtime = mergeRuntime(runtime, observed)
		}
		runtime = applyHTTPServiceHealth(ctx, runtime, firstNonEmpty(componentHealthURLByService[service.ID], service.HealthURL))
		runtimeByService[service.ID] = runtime
	}
	payload.ServiceRuntime = make([]serviceRuntime, 0, len(services))
	for groupIndex := range payload.Groups {
		for itemIndex := range payload.Groups[groupIndex].Items {
			item := &payload.Groups[groupIndex].Items[itemIndex]
			service := catalogServiceByID(services, item.ID)
			runtime := runtimeByService[item.ID]
			if runtime.ServiceID == "" {
				continue
			}
			state := runtime.State
			health := runtime.Health
			healthy := runtime.OK
			*item = dashboardItemFromCatalogService(catalog, service, runtime, state, health, healthy)
		}
	}
	for _, service := range services {
		if runtime := runtimeByService[service.ID]; runtime.ServiceID != "" {
			payload.ServiceRuntime = append(payload.ServiceRuntime, runtime)
		}
	}
	allItems := []dashboardItem{}
	for _, group := range payload.Groups {
		allItems = append(allItems, group.Items...)
	}
	payload.Summary = dashboardSummaryForItems(allItems)
}

func dashboardComponentHealthURLByService(ctx context.Context, runtime store.Store, services []store.CatalogService) map[string]string {
	if runtime == nil || len(services) == 0 {
		return nil
	}
	serviceIDs := make(map[string]bool, len(services))
	for _, service := range services {
		serviceIDs[strings.TrimSpace(service.ID)] = true
	}
	envs, err := runtime.ListEnvironments(ctx)
	if err != nil {
		return nil
	}
	bestScore := 0
	best := map[string]string{}
	for _, env := range envs {
		graph, err := runtime.GetEnvironmentComponentGraph(ctx, env.ID)
		if err != nil {
			continue
		}
		score := 0
		urls := map[string]string{}
		for _, component := range graph.Components {
			id := strings.TrimSpace(component.ComponentID)
			if !serviceIDs[id] {
				continue
			}
			score++
			check, errText := normalizeEnvironmentComponentHealthCheck(component)
			if errText != "" {
				continue
			}
			if strings.TrimSpace(valueString(check["kind"])) == "url" {
				urls[id] = strings.TrimSpace(valueString(check["url"]))
			}
		}
		if score > bestScore {
			bestScore = score
			best = urls
		}
	}
	if bestScore == 0 {
		return nil
	}
	return best
}

func activeCatalogServices(services []store.CatalogService) []store.CatalogService {
	out := make([]store.CatalogService, 0, len(services))
	for _, service := range services {
		if catalogServiceActive(service) {
			out = append(out, service)
		}
	}
	return out
}

func catalogServiceActive(service store.CatalogService) bool {
	status := strings.TrimSpace(service.Status)
	return status == "" || strings.EqualFold(status, "active")
}

func filterDashboardPayloadServices(payload *dashboardPayload, services []store.CatalogService) {
	activeByID := make(map[string]bool, len(services))
	for _, service := range services {
		activeByID[service.ID] = true
	}
	for groupIndex := range payload.Groups {
		items := payload.Groups[groupIndex].Items[:0]
		for _, item := range payload.Groups[groupIndex].Items {
			if activeByID[item.ID] {
				items = append(items, item)
			}
		}
		payload.Groups[groupIndex].Items = items
	}
	runtimes := payload.ServiceRuntime[:0]
	for _, runtime := range payload.ServiceRuntime {
		if activeByID[runtime.ServiceID] {
			runtimes = append(runtimes, runtime)
		}
	}
	payload.ServiceRuntime = runtimes
}

func dashboardItemFromCatalogService(catalog store.ProfileCatalog, service store.CatalogService, runtime serviceRuntime, state string, health string, healthy bool) dashboardItem {
	return dashboardItem{
		ID:             service.ID,
		Name:           firstNonEmpty(service.DisplayName, service.ID),
		DisplayName:    service.DisplayName,
		State:          firstNonEmpty(state, "missing"),
		Health:         firstNonEmpty(health, "unknown"),
		Kind:           service.Kind,
		OK:             healthy,
		Branch:         catalog.ProfileID,
		Profile:        catalog.ProfileID,
		Container:      firstNonEmpty(runtime.Container, service.ContainerName),
		Image:          firstNonEmpty(runtime.Image, service.Image),
		Port:           firstPositiveInt(runtime.Port, service.ServicePort),
		ManagementPort: firstPositiveInt(runtime.ManagementPort, service.ManagementPort),
		Message:        runtime.Message,
		Presentation:   dashboardPresentationForCatalog(catalog.TemplateConfigs, service.ID),
	}
}

func serviceRuntimeFromCatalogService(service store.CatalogService, state string, health string, ok bool) serviceRuntime {
	branchName, commitID := sourceSnapshotRevision(service.SourcePath)
	return serviceRuntime{
		ServiceID:      service.ID,
		NodeRole:       service.Kind,
		Container:      service.ContainerName,
		Image:          service.Image,
		SourcePath:     service.SourcePath,
		BranchName:     firstNonEmpty(service.GitBranch, branchName),
		CommitID:       commitID,
		State:          firstNonEmpty(state, "missing"),
		Health:         firstNonEmpty(health, "unknown"),
		OK:             ok,
		Port:           service.ServicePort,
		ManagementPort: service.ManagementPort,
	}
}

func dashboardSummaryForItems(items []dashboardItem) dashboardSummary {
	healthy, missing, unhealthy := 0, 0, 0
	for _, item := range items {
		if item.OK {
			healthy++
			continue
		}
		if item.State == "missing" {
			missing++
			continue
		}
		unhealthy++
	}
	return dashboardSummary{Total: len(items), Healthy: healthy, Missing: missing, Unhealthy: unhealthy}
}

func dashboardPresentationForCatalog(configs []store.CatalogTemplateConfig, serviceID string) dashboardPresentation {
	copy := map[string]string{}
	for _, config := range configs {
		if !visibleTemplateConfigStatus(config.Status) {
			continue
		}
		configCopy := stringMapFromAny(jsonObject(config.ConfigJSON)["copy"])
		if len(configCopy) == 0 {
			continue
		}
		switch {
		case config.ScopeType == "environment":
			mergeStringMap(copy, configCopy)
		case config.ScopeType == "environment-node" && config.NodeID == "" && (config.ScopeID == "" || config.ScopeID == "_default"):
			mergeStringMap(copy, configCopy)
		case config.ScopeType == "environment-node" && serviceID != "" && (config.NodeID == serviceID || config.ScopeID == serviceID):
			mergeStringMap(copy, configCopy)
		}
	}
	if len(copy) == 0 {
		return dashboardPresentation{}
	}
	return dashboardPresentation{Copy: copy}
}

func catalogServiceByID(services []store.CatalogService, id string) store.CatalogService {
	for _, service := range services {
		if service.ID == id {
			return service
		}
	}
	return store.CatalogService{ID: id}
}

func dashboardPayloadFromBundle(ctx context.Context, bundle profile.Bundle) dashboardPayload {
	dockerRuntimes := dockerRuntimeByService(ctx, bundle.Services)
	configuredRuntimes := configuredRuntimeByService(ctx, bundle)
	items := make([]dashboardItem, 0, len(bundle.Services))
	serviceRuntimes := make([]serviceRuntime, 0, len(bundle.Services))
	for _, service := range bundle.Services {
		runtime := configuredRuntimes[service.ID]
		dockerRuntime, ok := dockerRuntimes[service.ID]
		state := "missing"
		health := "unknown"
		healthy := false
		if ok {
			runtime = mergeRuntime(runtime, dockerRuntime)
			state = dockerRuntime.State
			health = dockerRuntime.Health
			healthy = dockerRuntime.OK
		} else if strings.EqualFold(service.Kind, "external") {
			runtime = serviceRuntime{
				ServiceID: service.ID,
				NodeRole:  service.Kind,
				State:     "external",
				Health:    "external",
				OK:        true,
			}
			runtime = applyHTTPServiceHealth(ctx, runtime, service.HealthURL)
			state = "external"
			health = runtime.Health
			healthy = runtime.OK
		}
		if runtime.ServiceID != "" {
			serviceRuntimes = append(serviceRuntimes, runtime)
		}
		items = append(items, dashboardItem{
			ID:             service.ID,
			Name:           firstNonEmpty(service.DisplayName, service.ID),
			DisplayName:    service.DisplayName,
			State:          state,
			Health:         health,
			Kind:           service.Kind,
			OK:             healthy,
			Branch:         bundle.ID,
			Profile:        bundle.ID,
			Container:      firstNonEmpty(runtime.Container, service.ContainerName),
			Image:          firstNonEmpty(runtime.Image, service.Image),
			Port:           firstPositiveInt(runtime.Port, service.ServicePort),
			ManagementPort: firstPositiveInt(runtime.ManagementPort, service.ManagementPort),
			Message:        runtime.Message,
		})
	}
	healthy, missing, unhealthy := 0, 0, 0
	for _, item := range items {
		if item.OK {
			healthy++
			continue
		}
		if item.State == "missing" {
			missing++
			continue
		}
		unhealthy++
	}
	return dashboardPayload{
		OK:     true,
		Source: map[string]string{"kind": "profile", "id": bundle.ID},
		Summary: dashboardSummary{
			Total:     len(items),
			Healthy:   healthy,
			Missing:   missing,
			Unhealthy: unhealthy,
		},
		Groups: []dashboardGroup{{
			ID:          "business",
			Label:       "Services",
			DisplayName: "Services",
			Items:       items,
		}},
		ServiceRuntime: serviceRuntimes,
	}
}

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
	targets := []string{service.ContainerName, service.DockerService, service.ID}
	for _, container := range containers {
		for _, name := range strings.Split(container.Names, ",") {
			name = strings.TrimSpace(name)
			for _, target := range targets {
				target = strings.TrimSpace(target)
				if target == "" {
					continue
				}
				if name == target || name == service.ID || strings.HasSuffix(name, "-"+target) || strings.HasSuffix(name, "_"+target) {
					return container, true
				}
			}
		}
	}
	return dockerContainerRow{}, false
}

func matchCatalogServiceContainer(service store.CatalogService, containers []dockerContainerRow) (dockerContainerRow, bool) {
	targets := []string{service.ContainerName, service.DockerService, service.ID}
	for _, container := range containers {
		for _, name := range strings.Split(container.Names, ",") {
			name = strings.TrimSpace(name)
			for _, target := range targets {
				target = strings.TrimSpace(target)
				if target == "" {
					continue
				}
				if name == target || name == service.ID || strings.HasSuffix(name, "-"+target) || strings.HasSuffix(name, "_"+target) {
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
			ID:               item.ID,
			DisplayName:      item.DisplayName,
			NodeID:           item.NodeID,
			CasePath:         item.CasePath,
			SourceKind:       item.SourceKind,
			SourcePath:       item.SourcePath,
			ExecutorID:       item.ExecutorID,
			BaseURL:          item.BaseURL,
			EvidenceDir:      item.EvidenceDir,
			TimeoutSeconds:   item.TimeoutSeconds,
			DefaultOverrides: item.DefaultOverrides,
		})
	}

	return catalogPayload{
		SchemaVersion: "1",
		OK:            true,
		GeneratedAt:   time.Now().UTC(),
		Navigation:    map[string]any{},
		Warnings:      []string{},
		Source: map[string]string{
			"kind":        "profile",
			"id":          bundle.ID,
			"displayName": bundle.DisplayName,
		},
		Services:     services,
		Presentation: catalogPresentationFromProfileConfigs(bundle.TemplateConfigs),
		Workflows:    catalogWorkflows(bundle),
		APICases:     apiCases,
		Topology: catalogTopology{
			Nodes: nodes,
			Edges: []catalogEdge{},
		},
	}
}

func catalogPresentationFromProfileConfigs(configs []profile.TemplateConfig) *catalogPresentation {
	var presentation catalogPresentation
	for _, config := range configs {
		if !visibleTemplateConfigStatus(config.Status) || config.ScopeType != "workflow-directory" {
			continue
		}
		if config.ScopeID != "" && config.ScopeID != "_default" {
			continue
		}
		mergeCatalogWorkflowFinder(&presentation, jsonObject(config.ConfigJSON))
	}
	if presentation.WorkflowFinder == nil {
		return nil
	}
	return &presentation
}

func mergeCatalogWorkflowFinder(presentation *catalogPresentation, config map[string]any) {
	rawFinder := config["workflowFinder"]
	if rawFinder == nil {
		rawFinder = config["targetWorkflow"]
	}
	finderConfig, ok := rawFinder.(map[string]any)
	if !ok {
		return
	}
	if presentation.WorkflowFinder == nil {
		presentation.WorkflowFinder = &catalogWorkflowFinderConfig{}
	}
	if value := intValue(finderConfig["targetStepCount"]); value > 0 {
		presentation.WorkflowFinder.TargetStepCount = value
	}
	if value := intValue(finderConfig["targetInterfaceCount"]); value > 0 {
		presentation.WorkflowFinder.TargetInterfaceCount = value
	}
	if value := strings.TrimSpace(valueString(finderConfig["targetLabel"])); value != "" {
		presentation.WorkflowFinder.TargetLabel = value
	}
	if presentation.WorkflowFinder.TargetStepCount == 0 && presentation.WorkflowFinder.TargetInterfaceCount == 0 && presentation.WorkflowFinder.TargetLabel == "" {
		presentation.WorkflowFinder = nil
	}
}

func interfaceNodesPayloadFromBundle(bundle profile.Bundle, serviceID string, operation string) interfaceNodesPayload {
	items := make([]interfaceNodeItem, 0, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		if serviceID != "" && node.ServiceID != serviceID {
			continue
		}
		nodeOperation := firstNonEmpty(node.Operation, node.DisplayName, node.ID)
		if operation != "" && nodeOperation != operation {
			continue
		}
		items = append(items, interfaceNodeItem{
			ID:               node.ID,
			DisplayName:      node.DisplayName,
			ServiceID:        node.ServiceID,
			Operation:        nodeOperation,
			Href:             "/interface-node.html?id=" + node.ID,
			Status:           "pending",
			AdmissionStatus:  "pending",
			ValidationStatus: "valid",
			TimeoutMs:        node.TimeoutMs,
		})
	}
	return interfaceNodesPayload{
		OK:         true,
		TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Filters:    map[string]string{"serviceId": serviceID, "operation": operation},
		Source:     map[string]string{"kind": "profile", "id": bundle.ID},
		Items:      items,
	}
}

func interfaceNodesPayloadFromStore(ctx context.Context, catalog store.ProfileCatalog, runtime store.Store, serviceID string, operation string) (interfaceNodesPayload, error) {
	payload, ok, err := interfaceNodesPayloadFromReadModel(ctx, runtime, catalog.ProfileID, serviceID, operation)
	if err != nil {
		return interfaceNodesPayload{}, err
	}
	if !ok {
		payload = interfaceNodesBasePayloadFromCatalog(catalog, serviceID, operation)
	}
	if err := hydrateInterfaceNodesPayload(ctx, catalog, runtime, &payload); err != nil {
		return interfaceNodesPayload{}, err
	}
	if len(payload.Presentation.Copy) == 0 {
		payload.Presentation = interfaceNodeDirectoryPresentationForCatalog(catalog.TemplateConfigs)
	}
	return payload, nil
}

func interfaceNodesPayloadFromReadModel(ctx context.Context, runtime store.Store, profileID string, serviceID string, operation string) (interfaceNodesPayload, bool, error) {
	model, err := runtime.GetReadModel(ctx, profileID, profilecatalog.ReadModelInterfaceNodes)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return interfaceNodesPayload{}, false, nil
		}
		return interfaceNodesPayload{}, false, err
	}
	var payload interfaceNodesPayload
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		return interfaceNodesPayload{}, false, err
	}
	payload.Filters = map[string]string{"serviceId": serviceID, "operation": operation}
	payload.Source = map[string]string{"kind": "read-model", "id": profileID}
	if serviceID == "" && operation == "" {
		return payload, true, nil
	}
	filtered := payload.Items[:0]
	for _, item := range payload.Items {
		if serviceID != "" && item.ServiceID != serviceID {
			continue
		}
		if operation != "" && item.Operation != operation {
			continue
		}
		filtered = append(filtered, item)
	}
	payload.Items = filtered
	return payload, true, nil
}

func applyInterfaceNodeSearchFilter(payload *interfaceNodesPayload, filter string) {
	filter = strings.TrimSpace(filter)
	if payload.Filters == nil {
		payload.Filters = map[string]string{}
	}
	payload.Filters["filter"] = filter
	if filter == "" {
		return
	}
	filtered := payload.Items[:0]
	for _, item := range payload.Items {
		if matchesControlplaneDiscoveryFilter(filter, item.ID, item.DisplayName, item.Operation, item.Method, item.Path, item.ServiceID) {
			filtered = append(filtered, item)
		}
	}
	payload.Items = filtered
}

func interfaceNodesBasePayloadFromCatalog(catalog store.ProfileCatalog, serviceID string, operation string) interfaceNodesPayload {
	items := make([]interfaceNodeItem, 0, len(catalog.InterfaceNodes))
	for _, node := range catalog.InterfaceNodes {
		if serviceID != "" && node.ServiceID != serviceID {
			continue
		}
		nodeOperation := firstNonEmpty(node.Operation, node.DisplayName, node.ID)
		if operation != "" && nodeOperation != operation {
			continue
		}
		cases := catalogCasesForNode(catalog.APICases, node.ID)
		required := 0
		for _, item := range cases {
			if item.RequiredForAdmission {
				required++
			}
		}
		items = append(items, interfaceNodeItem{
			ID:                   node.ID,
			DisplayName:          node.DisplayName,
			ServiceID:            node.ServiceID,
			Operation:            nodeOperation,
			Method:               node.Method,
			Path:                 node.Path,
			Href:                 "/interface-node.html?id=" + node.ID,
			Status:               firstNonEmpty(node.Status, "draft"),
			AdmissionStatus:      "pending",
			ValidationStatus:     "valid",
			ValidationIssueCount: 0,
			RequiredCaseCount:    required,
			PassedCaseCount:      0,
			TimeoutMs:            node.TimeoutMs,
		})
	}
	return interfaceNodesPayload{
		OK:           true,
		TemplateID:   "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Filters:      map[string]string{"serviceId": serviceID, "operation": operation},
		Source:       map[string]string{"kind": "store", "id": catalog.ProfileID},
		Items:        items,
		Presentation: interfaceNodeDirectoryPresentationForCatalog(catalog.TemplateConfigs),
	}
}

func hydrateInterfaceNodesPayload(ctx context.Context, catalog store.ProfileCatalog, runtime store.Store, payload *interfaceNodesPayload) error {
	latest, err := preferredCaseStates(ctx, catalog, runtime)
	if err != nil {
		return err
	}
	for index := range payload.Items {
		cases := catalogCasesForNode(catalog.APICases, payload.Items[index].ID)
		passed, failed, missing := 0, 0, 0
		required := 0
		latestRunID := ""
		latestElapsedMs := int64(0)
		latestObservedAt := time.Time{}
		latestRequiredRunID := ""
		latestRequiredElapsedMs := int64(0)
		latestRequiredObservedAt := time.Time{}
		totalElapsedMs := int64(0)
		for _, item := range cases {
			state := latest[item.ID]
			if state.RunID != "" && (latestRunID == "" || state.ObservedAt.After(latestObservedAt)) {
				latestRunID = state.RunID
				latestElapsedMs = state.ElapsedMs
				latestObservedAt = state.ObservedAt
			}
			if !item.RequiredForAdmission {
				continue
			}
			required++
			if state.RunID != "" && (latestRequiredRunID == "" || state.ObservedAt.After(latestRequiredObservedAt)) {
				latestRequiredRunID = state.RunID
				latestRequiredElapsedMs = state.ElapsedMs
				latestRequiredObservedAt = state.ObservedAt
			}
			totalElapsedMs += state.ElapsedMs
			switch state.Status {
			case store.StatusPassed:
				passed++
			case store.StatusFailed:
				failed++
			default:
				missing++
			}
		}
		admission := "pending"
		if required > 0 && passed == required {
			admission = store.StatusPassed
		} else if failed > 0 {
			admission = store.StatusFailed
		} else if missing == 0 && required == 0 {
			admission = "pending"
		}
		payload.Items[index].AdmissionStatus = admission
		payload.Items[index].RequiredCaseCount = required
		payload.Items[index].PassedCaseCount = passed
		payload.Items[index].LatestRunID = firstNonEmpty(latestRequiredRunID, latestRunID)
		payload.Items[index].LatestElapsedMs = firstPositiveInt64(latestRequiredElapsedMs, latestElapsedMs)
		payload.Items[index].TotalElapsedMs = firstPositiveInt64(totalElapsedMs, payload.Items[index].LatestElapsedMs)
	}
	return nil
}

func preferredCaseStates(ctx context.Context, catalog store.ProfileCatalog, runtime store.Store) (map[string]latestCaseState, error) {
	timeoutByCase := interfaceCaseTimeoutsByID(catalog)
	if fast, ok := runtime.(interfaceNodeCaseRunRecordStore); ok {
		caseIDs := make([]string, 0, len(catalog.APICases))
		for _, item := range catalog.APICases {
			if item.ID != "" && activeCatalogStatus(item.Status) {
				caseIDs = append(caseIDs, item.ID)
			}
		}
		records, err := fast.ListAPICaseRunRecordsForCaseIDs(ctx, caseIDs)
		if err != nil {
			return nil, err
		}
		out := map[string]latestCaseState{}
		selectedPassed := map[string]bool{}
		for _, record := range records {
			item := record.CaseRun
			if item.CaseID == "" || selectedPassed[item.CaseID] {
				continue
			}
			state := evaluateLatestCaseStateTimeout(latestCaseStateFromRun(item), timeoutByCase[item.CaseID])
			if _, exists := out[item.CaseID]; !exists {
				out[item.CaseID] = state
			}
			if state.Status == store.StatusPassed {
				out[item.CaseID] = state
				selectedPassed[item.CaseID] = true
			}
		}
		return out, nil
	}
	states, err := latestCaseStates(ctx, runtime)
	if err != nil {
		return nil, err
	}
	for caseID, state := range states {
		states[caseID] = evaluateLatestCaseStateTimeout(state, timeoutByCase[caseID])
	}
	return states, nil
}

type latestCaseState struct {
	Status     string
	RunID      string
	ElapsedMs  int64
	ObservedAt time.Time
}

func latestCaseStatuses(ctx context.Context, runtime store.Store) (map[string]string, error) {
	states, err := latestCaseStates(ctx, runtime)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for caseID, state := range states {
		out[caseID] = state.Status
	}
	return out, nil
}

func latestCaseStates(ctx context.Context, runtime store.Store) (map[string]latestCaseState, error) {
	out := map[string]latestCaseState{}
	if runtime == nil {
		return out, nil
	}
	if fast, ok := runtime.(latestAPICaseRunStore); ok {
		caseRuns, err := fast.ListLatestAPICaseRuns(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range caseRuns {
			if item.CaseID == "" {
				continue
			}
			if _, exists := out[item.CaseID]; !exists {
				out[item.CaseID] = latestCaseStateFromRun(item)
			}
		}
		return out, nil
	}
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	for i := len(runs) - 1; i >= 0; i-- {
		caseRuns, err := runtime.ListAPICaseRuns(ctx, runs[i].ID)
		if err != nil {
			return nil, err
		}
		for j := len(caseRuns) - 1; j >= 0; j-- {
			item := caseRuns[j]
			if item.CaseID == "" {
				continue
			}
			if _, exists := out[item.CaseID]; !exists {
				out[item.CaseID] = latestCaseStateFromRun(item)
			}
		}
	}
	return out, nil
}

func latestCaseStateFromRun(item store.APICaseRun) latestCaseState {
	observedAt := item.FinishedAt
	if observedAt.IsZero() {
		observedAt = item.StartedAt
	}
	if observedAt.IsZero() {
		observedAt = item.CreatedAt
	}
	return latestCaseState{
		Status:     item.Status,
		RunID:      item.RunID,
		ElapsedMs:  elapsedMilliseconds(item.StartedAt, item.FinishedAt),
		ObservedAt: observedAt,
	}
}

func evaluateLatestCaseStateTimeout(state latestCaseState, timeoutMs int) latestCaseState {
	if evaluateRuntimeTimeout(state.ElapsedMs, timeoutMs).Exceeded {
		state.Status = store.StatusFailed
	}
	return state
}

type latestAPICaseRunStore interface {
	ListLatestAPICaseRuns(context.Context) ([]store.APICaseRun, error)
}

func catalogCasesForNode(items []store.CatalogAPICase, nodeID string) []store.CatalogAPICase {
	cases := make([]store.CatalogAPICase, 0)
	for _, item := range items {
		if item.NodeID == nodeID && activeCatalogStatus(item.Status) {
			cases = append(cases, item)
		}
	}
	return cases
}

func interfaceNodeDetailPayloadFromBundle(bundle profile.Bundle, id string) (interfaceNodeDetailPayload, bool) {
	for _, node := range bundle.InterfaceNodes {
		if node.ID == id {
			return interfaceNodeDetailPayloadForNode(bundle, node), true
		}
	}
	return interfaceNodeDetailPayload{
		OK:         false,
		TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Source:     map[string]string{"kind": "profile", "id": bundle.ID},
		Error:      "interface node not found",
		Requested:  id,
		Available:  interfaceNodesPayloadFromBundle(bundle, "", "").Items,
		Cases:      []interfaceCase{},
		Fields:     emptyInterfaceNodeFields(),
		History:    emptyInterfaceNodeHistory(),
		Runs:       []map[string]any{},
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
			ID:               item.ID,
			Title:            firstNonEmpty(item.DisplayName, item.ID),
			Operation:        firstNonEmpty(node.DisplayName, item.NodeID),
			CasePath:         item.CasePath,
			SourceKind:       item.SourceKind,
			SourcePath:       item.SourcePath,
			ExecutorID:       item.ExecutorID,
			BaseURL:          item.BaseURL,
			EvidenceDir:      item.EvidenceDir,
			TimeoutSeconds:   item.TimeoutSeconds,
			DefaultOverrides: item.DefaultOverrides,
			Workflow:         map[string]string{},
			Graph:            graph,
		})
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
			ID:               item.ID,
			Title:            firstNonEmpty(item.DisplayName, item.ID),
			Operation:        firstNonEmpty(node.DisplayName, item.NodeID),
			CasePath:         item.CasePath,
			SourceKind:       item.SourceKind,
			SourcePath:       item.SourcePath,
			ExecutorID:       item.ExecutorID,
			BaseURL:          item.BaseURL,
			EvidenceDir:      item.EvidenceDir,
			TimeoutSeconds:   item.TimeoutSeconds,
			DefaultOverrides: jsonObject(item.DefaultOverridesJSON),
			Workflow:         map[string]string{},
			Graph:            graph,
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
		OK:         true,
		TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Source:     map[string]string{"kind": "profile", "id": bundle.ID},
		Node: interfaceNodeDetail{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
			Operation:   firstNonEmpty(node.DisplayName, node.ID),
			Method:      method,
			Path:        path,
			TimeoutMs:   node.TimeoutMs,
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

func interfaceNodeDetailPayloadFromCatalog(catalog store.ProfileCatalog, id string) (interfaceNodeDetailPayload, bool) {
	var node store.CatalogInterfaceNode
	found := false
	for _, item := range catalog.InterfaceNodes {
		if item.ID == id {
			node = item
			found = true
			break
		}
	}
	if !found {
		return interfaceNodeDetailPayload{}, false
	}
	cases := casesForCatalogNode(catalog, id)
	return interfaceNodeDetailPayload{
		OK:         true,
		TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Source:     map[string]string{"kind": "store", "id": catalog.ProfileID},
		Requested:  id,
		Node: interfaceNodeDetail{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
			Operation:   firstNonEmpty(node.Operation, node.DisplayName, node.ID),
			Method:      node.Method,
			Path:        node.Path,
			TimeoutMs:   node.TimeoutMs,
			TemplateID:  node.TemplateID,
			Version:     node.Version,
			Status:      node.Status,
			Tags:        node.Tags,
			Description: node.Description,
			SortOrder:   node.SortOrder,
			CreatedAt:   node.CreatedAt,
			UpdatedAt:   node.UpdatedAt,
		},
		Admission: interfaceNodeAdmission{
			Status:            "pending",
			RequiredCaseCount: requiredInterfaceCaseCount(cases),
			PassedCaseCount:   0,
			Blockers:          []map[string]any{},
		},
		RequestTemplates: requestTemplatesForCatalogNode(catalog.RequestTemplates, id),
		Cases:            cases,
		Fields:           fieldsForCatalogNode(catalog.InterfaceFields, id),
		History:          emptyInterfaceNodeHistory(),
		Runs:             []map[string]any{},
		Presentation:     interfaceNodePresentationForCatalog(catalog.TemplateConfigs, node),
	}, true
}

func interfaceNodePresentationForCatalog(configs []store.CatalogTemplateConfig, node store.CatalogInterfaceNode) interfaceNodePresentation {
	copy := map[string]string{}
	for _, config := range configs {
		if !visibleTemplateConfigStatus(config.Status) || config.ScopeType != "interface-node" {
			continue
		}
		configCopy := stringMapFromAny(jsonObject(config.ConfigJSON)["copy"])
		if len(configCopy) == 0 {
			continue
		}
		switch {
		case config.NodeID == "" && (config.ScopeID == "" || config.ScopeID == "_default"):
			mergeStringMap(copy, configCopy)
		case config.NodeID == node.ID || config.ScopeID == node.ID:
			mergeStringMap(copy, configCopy)
		}
	}
	if len(copy) == 0 {
		return interfaceNodePresentation{}
	}
	return interfaceNodePresentation{Copy: copy}
}

func interfaceNodeDirectoryPresentationForCatalog(configs []store.CatalogTemplateConfig) interfaceNodePresentation {
	copy := map[string]string{}
	for _, config := range configs {
		if !visibleTemplateConfigStatus(config.Status) || config.ScopeType != "interface-node-directory" {
			continue
		}
		if config.ScopeID != "" && config.ScopeID != "_default" {
			continue
		}
		configCopy := stringMapFromAny(jsonObject(config.ConfigJSON)["copy"])
		if len(configCopy) == 0 {
			continue
		}
		mergeStringMap(copy, configCopy)
	}
	if len(copy) == 0 {
		return interfaceNodePresentation{}
	}
	return interfaceNodePresentation{Copy: copy}
}

func mergeStringMap(target map[string]string, source map[string]string) {
	for key, value := range source {
		if key != "" && value != "" {
			target[key] = value
		}
	}
}

func requestTemplatesForCatalogNode(items []store.CatalogRequestTemplate, nodeID string) []interfaceRequestTemplate {
	templates := make([]interfaceRequestTemplate, 0)
	for _, item := range items {
		if item.NodeID != nodeID || !activeCatalogStatus(item.Status) {
			continue
		}
		templates = append(templates, interfaceRequestTemplate{
			ID:           item.ID,
			Name:         item.DisplayName,
			Version:      item.Version,
			Status:       firstNonEmpty(item.Status, "active"),
			Method:       item.Method,
			Path:         item.Path,
			TemplateJSON: item.TemplateJSON,
		})
	}
	sort.SliceStable(templates, func(i int, j int) bool { return templates[i].ID < templates[j].ID })
	return templates
}

func casesForCatalogNode(catalog store.ProfileCatalog, nodeID string) []interfaceCase {
	dependenciesByCase := make(map[string][]map[string]any)
	fixtureByID := make(map[string]store.CatalogFixture)
	for _, fixture := range catalog.Fixtures {
		fixtureByID[fixture.ID] = fixture
	}
	for _, dependency := range catalog.CaseDependencies {
		if !activeCatalogStatus(dependency.Status) {
			continue
		}
		fixture := fixtureByID[dependency.FixtureID]
		dependenciesByCase[dependency.CaseID] = append(dependenciesByCase[dependency.CaseID], map[string]any{
			"id":               dependency.ID,
			"fixtureProfileId": dependency.FixtureID,
			"profile": map[string]any{
				"id":   fixture.ID,
				"name": fixture.DisplayName,
				"kind": fixture.Kind,
			},
			"required":     dependency.Required,
			"mappingsJson": dependency.MappingsJSON,
		})
	}
	cases := make([]interfaceCase, 0)
	for _, item := range catalog.APICases {
		if item.NodeID != nodeID || !activeCatalogStatus(item.Status) {
			continue
		}
		cases = append(cases, interfaceCase{
			ID:                   item.ID,
			Title:                item.DisplayName,
			CaseType:             firstNonEmpty(item.CaseType, "api"),
			Scenario:             item.Scenario,
			PayloadTemplateJSON:  item.PayloadTemplateJSON,
			RequestTemplateID:    item.RequestTemplateID,
			PatchJSON:            item.PatchJSON,
			RenderMode:           item.RenderMode,
			ExpectedJSON:         item.ExpectedJSON,
			Status:               item.Status,
			SortOrder:            item.SortOrder,
			RequiredForAdmission: item.RequiredForAdmission,
			Dependencies:         nonNil(dependenciesByCase[item.ID]),
		})
	}
	return cases
}

func fieldsForCatalogNode(items []store.CatalogInterfaceNodeField, nodeID string) interfaceNodeFields {
	fields := emptyInterfaceNodeFields()
	for _, item := range items {
		if item.NodeID != nodeID || !activeCatalogStatus(item.Status) {
			continue
		}
		row := map[string]any{
			"id":          item.ID,
			"fieldPath":   item.FieldPath,
			"displayName": item.DisplayName,
			"dataType":    item.DataType,
			"required":    item.Required,
			"bindable":    item.Bindable,
			"portType":    item.PortType,
		}
		switch item.Direction {
		case "response":
			fields.Response = append(fields.Response, row)
		default:
			fields.Request = append(fields.Request, row)
		}
	}
	return fields
}

func requiredInterfaceCaseCount(items []interfaceCase) int {
	count := 0
	for _, item := range items {
		if item.RequiredForAdmission {
			count++
		}
	}
	return count
}

func activeCatalogStatus(status string) bool {
	status = strings.TrimSpace(strings.ToLower(status))
	return status == "" || status == "active"
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
				TimeoutMs:   node.TimeoutMs,
			})
		}
		workflows = append(workflows, catalogWorkflow{
			ID:                workflow.ID,
			DisplayName:       workflow.DisplayName,
			Description:       workflow.Description,
			Entrypoint:        "/workflow-studio.html",
			BaseStepTimeoutMs: workflow.BaseStepTimeoutMs,
			TimeoutOffsetMs:   workflow.TimeoutOffsetMs,
			TimeoutMs:         workflowBudgetMs(workflow.BaseStepTimeoutMs, workflow.TimeoutOffsetMs, steps),
			Steps:             steps,
			Presentation:      catalogWorkflowPresentation{Kind: workflowPresentationKind(steps)},
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
