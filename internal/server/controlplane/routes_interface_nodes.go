package controlplane

import (
	"errors"
	"net/http"

	"agent-testbench/internal/store"
)

func registerInterfaceNodeRoutes(mux *http.ServeMux, deps routeDeps) {
	runtime := deps.runtime
	profiles := deps.profiles
	mux.HandleFunc("/api/interface-nodes", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
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
		if !requireMethod(w, r, http.MethodGet) {
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
		if !requireMethod(w, r, http.MethodGet) {
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
		if !requireMethod(w, r, http.MethodGet) {
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
}
