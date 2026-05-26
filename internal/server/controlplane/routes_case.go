package controlplane

import "net/http"

func registerCaseRoutes(mux *http.ServeMux, deps routeDeps) {
	runtime := deps.runtime
	profiles := deps.profiles
	collector := deps.collector
	caseBatchRunner := deps.caseBatchRunner
	mux.HandleFunc("/api/case/runs", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseRuns(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/evidence", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseEvidence(w, r, runtime)
	})
	mux.HandleFunc("/api/case-run/evidence", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseRunEvidence(w, r, runtime)
	})
	mux.HandleFunc("/api/case/timing", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseTiming(w, r, runtime)
	})
	mux.HandleFunc("/api/post-process-tasks", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handlePostProcessTasks(w, r, runtime)
	})
	mux.HandleFunc("/api/case/incomplete-batches", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseIncompleteBatches(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-coverage", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseSuiteCoverage(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-inspection", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseSuiteInspection(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-plan", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseSuitePlan(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-stability", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseSuiteStability(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-priority", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseSuitePriority(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-brief", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseSuiteBrief(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-quality", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseSuiteQuality(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-quality-plan", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseSuiteQualityPlan(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-impact", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleCaseSuiteImpact(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/case/suite-impact-runs", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleCaseSuiteImpactRun(w, r, profiles.Current(), runtime, caseBatchRunner, collector)
	})
	mux.HandleFunc("/api/replay/evidence", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleReplayEvidence(w, r)
	})
	mux.HandleFunc("/api/cases/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
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
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleAPICaseRun(w, r, profiles.Current(), runtime)
	})
	mux.HandleFunc("/api/cases/batch-runs", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleAPICaseBatchRunStart(w, r, profiles.Current(), runtime, caseBatchRunner, collector)
	})
	mux.HandleFunc("/api/cases/batch-runs/", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleAPICaseBatchRunReport(w, r, caseBatchRunner)
	})
	mux.HandleFunc("/api/test-kit/run", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleTestKitRun(w, r, profiles.Current(), runtime, collector)
	})
	mux.HandleFunc("/api/test-kit/run-batch", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleTestKitRunBatch(w, r, profiles.Current(), runtime)
	})
}
