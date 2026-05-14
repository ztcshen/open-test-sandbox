package controlplane

import (
	"encoding/json"
	"html/template"
	"net/http"

	"open-test-sandbox/internal/profile"
)

func New(bundle profile.Bundle) http.Handler {
	mux := http.NewServeMux()
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
	mux.HandleFunc("/dashboard.html", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = dashboardTemplate.Execute(w, dashboardData{
			Bundle: bundle,
			Counts: bundle.Counts(),
		})
	})
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

type dashboardData struct {
	Bundle profile.Bundle
	Counts profile.Counts
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

var dashboardTemplate = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Open Test Sandbox</title>
  <style>
    :root {
      color-scheme: light;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #f7f8f5;
      color: #18211f;
    }
    body {
      margin: 0;
      min-height: 100vh;
      background: #f7f8f5;
    }
    main {
      max-width: 1040px;
      margin: 0 auto;
      padding: 40px 24px;
    }
    header {
      display: flex;
      align-items: end;
      justify-content: space-between;
      gap: 24px;
      border-bottom: 1px solid #d8ddd4;
      padding-bottom: 20px;
    }
    h1 {
      font-size: 30px;
      line-height: 1.15;
      margin: 0 0 8px;
      font-weight: 720;
      letter-spacing: 0;
    }
    p {
      margin: 0;
      color: #59635f;
    }
    dl {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
      gap: 1px;
      margin: 28px 0 0;
      background: #d8ddd4;
      border: 1px solid #d8ddd4;
    }
    div.metric {
      background: #ffffff;
      padding: 18px;
      min-height: 86px;
    }
    dt {
      color: #59635f;
      font-size: 13px;
      margin-bottom: 10px;
    }
    dd {
      margin: 0;
      font-size: 28px;
      font-weight: 700;
    }
    code {
      font: inherit;
      color: #315f64;
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>Open Test Sandbox</h1>
        <p><code>{{.Bundle.ID}}</code> · {{.Bundle.DisplayName}}</p>
      </div>
    </header>
    <dl aria-label="Profile summary">
      <div class="metric"><dt>Services</dt><dd>{{.Counts.Services}}</dd></div>
      <div class="metric"><dt>Workflows</dt><dd>{{.Counts.Workflows}}</dd></div>
      <div class="metric"><dt>Interface Nodes</dt><dd>{{.Counts.InterfaceNodes}}</dd></div>
      <div class="metric"><dt>API Cases</dt><dd>{{.Counts.APICases}}</dd></div>
      <div class="metric"><dt>Fixtures</dt><dd>{{.Counts.Fixtures}}</dd></div>
    </dl>
  </main>
</body>
</html>`))
