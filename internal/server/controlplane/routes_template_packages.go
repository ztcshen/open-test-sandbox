package controlplane

import "net/http"

func registerTemplatePackageRoutes(mux *http.ServeMux, deps routeDeps) {
	runtime := deps.runtime
	profiles := deps.profiles
	profileHome := deps.profileHome
	mux.HandleFunc("/api/template-packages/import-plan/openapi", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleOpenAPIImportPlan(w, r)
	})
	mux.HandleFunc("/api/template-packages/import-plan/http-capture", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleHTTPCaptureImportPlan(w, r)
	})
	mux.HandleFunc("/api/template-packages/generation-plan/openapi", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleOpenAPIGenerationPlan(w, r)
	})
	mux.HandleFunc("/api/template-packages/import", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleProfileImport(w, r, runtime, profiles.Replace, profileHome)
	})
	mux.HandleFunc("/api/template-packages/verify", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleProfileVerify(w, r, runtime, profiles.Replace, profileHome)
	})
	mux.HandleFunc("/api/template-packages/audit-plan", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleProfileAuditPlan(w, r, runtime, profileHome)
	})
	mux.HandleFunc("/api/template-packages/install", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleProfileInstall(w, r, profileHome)
	})
	mux.HandleFunc("/api/template-packages/installed", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleInstalledProfiles(w, r, profileHome)
	})
	mux.HandleFunc("/api/template-packages/catalog-index", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleProfileCatalogIndex(w, r, runtime)
	})
	mux.HandleFunc("/api/template-packages/current", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeProfileSummary(w, profiles.Current())
	})
	mux.HandleFunc("/api/template-packages/assets", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeProfileAssets(w, profiles.Current())
	})
	mux.HandleFunc("/api/profile/import", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleProfileImport(w, r, runtime, profiles.Replace, profileHome)
	})
	mux.HandleFunc("/api/profile/verify", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleProfileVerify(w, r, runtime, profiles.Replace, profileHome)
	})
	mux.HandleFunc("/api/profile/audit-plan", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleProfileAuditPlan(w, r, runtime, profileHome)
	})
	mux.HandleFunc("/api/profile/install", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		handleProfileInstall(w, r, profileHome)
	})
	mux.HandleFunc("/api/profile/installed", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleInstalledProfiles(w, r, profileHome)
	})
	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeProfileSummary(w, profiles.Current())
	})
	mux.HandleFunc("/api/profile/assets", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeProfileAssets(w, profiles.Current())
	})
	mux.HandleFunc("/api/profile/catalog-index", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		handleProfileCatalogIndex(w, r, runtime)
	})
}
