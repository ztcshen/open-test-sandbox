package controlplane

import (
	"net/http"
	"path/filepath"
)

func registerStaticRoutes(mux *http.ServeMux, deps routeDeps) {
	staticDir := deps.staticDir
	mux.HandleFunc("/dashboard.html", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		serveStaticFile(w, r, staticDir, "dashboard.html")
	})
	mux.HandleFunc("/workflows.html", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		serveStaticFile(w, r, staticDir, "workflows.html")
	})
	for _, name := range staticFileNames {
		name := name
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, r *http.Request) {
			if !requireMethod(w, r, http.MethodGet) {
				return
			}
			serveStaticFile(w, r, staticDir, name)
		})
	}
	mux.Handle("/assets/react/", http.StripPrefix("/assets/react/", http.FileServer(http.Dir(filepath.Join(staticDir, "assets", "react")))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveStaticFile(w, r, staticDir, "index.html")
	})
}
