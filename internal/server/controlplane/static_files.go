package controlplane

import (
	"net/http"
	"os"
	"path/filepath"
)

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
