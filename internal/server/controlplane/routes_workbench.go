package controlplane

import "net/http"

func registerWorkbenchRoutes(mux *http.ServeMux, deps routeDeps) {
	runtime := deps.runtime
	profiles := deps.profiles
	collector := deps.collector
	caseBatchRunner := deps.caseBatchRunner
	storeInfo := deps.storeInfo
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, statePayloadFromBundle(profiles.Current()))
	})
	mux.HandleFunc("/api/store/current", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, storeCurrentPayload{OK: true, StoreInfo: storeInfo})
	})
	mux.HandleFunc("/api/sandbox/services", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleSandboxServiceRegistration(w, r, runtime)
	})
	mux.HandleFunc("/api/sandbox/interfaces", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
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
		if !requireMethod(w, r, http.MethodGet) {
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
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		payload, err := catalogPayloadFromBundleWithStore(r.Context(), profiles.Current(), runtime)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, payload)
	})
}
