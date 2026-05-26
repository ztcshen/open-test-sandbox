package controlplane

import (
	"net/http"

	"agent-testbench/internal/runner/executor"
)

func registerWorkflowRoutes(mux *http.ServeMux, deps routeDeps) {
	runtime := deps.runtime
	profiles := deps.profiles
	collector := deps.collector
	mux.HandleFunc("/api/workflow-audit", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleWorkflowAudit(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/workflow-plan", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleWorkflowPlan(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/workflows", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleWorkflowDiscovery(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleRuns(w, r, runtime)
	})
	mux.HandleFunc("/api/workflow-runs/latest-step", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleLatestWorkflowStepRun(w, r, runtime)
	})
	mux.HandleFunc("/api/workflow-runs/step", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleWorkflowStepRun(w, r, runtime)
	})
	mux.HandleFunc("/api/workflow-runs", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleSaveWorkflowRun(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/workflow-runs/", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleWorkflowRun(w, r, runtime)
	})
	mux.HandleFunc("/api/trace-topology/collect", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleTraceTopologyCollect(w, r, runtime, collector)
	})
	mux.HandleFunc("/api/agent-test", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleAgentTestWorkbench(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/executor/plan", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
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
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleEvidenceList(w, r, runtime)
	})
	mux.HandleFunc("/api/evidence/import", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleEvidenceImport(w, r, runtime, profiles.Current())
	})
	mux.HandleFunc("/api/baseline/gate", func(w http.ResponseWriter, r *http.Request) {
		handleBaselineGate(w, r, runtime, profiles.Current())
	})
	mux.HandleFunc("/api/template/render", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleTemplateRender(w, r, profiles.Current())
	})
}
