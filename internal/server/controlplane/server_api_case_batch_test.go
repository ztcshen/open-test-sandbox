package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerRunsAPICaseAndIndexesStoreRecords(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/items" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode target request: %v", err)
		}
		if request["id"] != "item-override" {
			t.Fatalf("target request overrides = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"created"}`))
	}))
	defer target.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	if err := os.WriteFile(casePath, []byte(`{
  "id": "case.alpha",
  "title": "Create Item",
  "request": {
    "method": "POST",
    "path": "/v1/items",
    "headers": {"Content-Type": "application/json"},
    "body": {"id": "item-001"}
  },
  "assertions": {
    "expectedStatusCodes": [200],
    "responseContains": ["created"]
  }
}`), 0o644); err != nil {
		t.Fatalf("write api case: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	body := `{"casePath":` + strconv.Quote(casePath) + `,"baseUrl":` + strconv.Quote(target.URL) + `,"evidenceDir":` + strconv.Quote(filepath.Join(dir, "evidence")) + `,"overrides":{"id":"item-override"}}`
	var payload struct {
		OK        bool   `json:"ok"`
		ViewerURL string `json:"viewerUrl"`
		Report    struct {
			RunID          string `json:"run_id"`
			CaseID         string `json:"case_id"`
			Status         string `json:"status"`
			Operation      string `json:"operation"`
			ActualHTTPCode int    `json:"actual_http_code"`
			ElapsedMs      int64  `json:"elapsed_ms"`
		} `json:"report"`
	}
	postJSONInto(t, server.URL+"/api/cases/run", body, http.StatusOK, &payload)
	if !payload.OK || payload.Report.CaseID != "case.alpha" || payload.Report.Status != store.StatusPassed || payload.ViewerURL == "" {
		t.Fatalf("api case run payload = %#v", payload)
	}
	if payload.Report.RunID == "" || payload.Report.ElapsedMs < 0 {
		t.Fatalf("api case run timing = %#v", payload.Report)
	}
	if payload.Report.Operation != "POST /v1/items" || payload.Report.ActualHTTPCode != 200 {
		t.Fatalf("api case run report details = %#v", payload.Report)
	}

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != payload.Report.RunID || runs[0].Status != store.StatusPassed {
		t.Fatalf("stored runs = %#v", runs)
	}
	caseRuns, err := s.ListAPICaseRuns(ctx, payload.Report.RunID)
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != "case.alpha" || !caseRuns[0].FinishedAt.After(caseRuns[0].StartedAt) {
		t.Fatalf("stored api case runs = %#v", caseRuns)
	}
	evidence, err := s.ListEvidence(ctx, payload.Report.RunID)
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidence) < 4 {
		t.Fatalf("stored evidence = %#v", evidence)
	}
}

func TestServerStartsAsyncAPICaseBatchRunForNodes(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/items":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode target request: %v", err)
			}
			if request["id"] != "item-override" {
				http.Error(w, "missing override", http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"created"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/items/item-override":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"found"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer target.Close()

	dir := t.TempDir()
	firstCasePath := filepath.Join(dir, "case-alpha.json")
	if err := os.WriteFile(firstCasePath, []byte(`{
  "id": "case.alpha",
  "title": "Create Item",
  "request": {
    "method": "POST",
    "path": "/v1/items",
    "headers": {"Content-Type": "application/json"},
    "body": {"id": "item-001"}
  },
  "assertions": {
    "expectedStatusCodes": [200],
    "responseContains": ["created"]
  }
}`), 0o644); err != nil {
		t.Fatalf("write first api case: %v", err)
	}
	secondCasePath := filepath.Join(dir, "case-beta.json")
	if err := os.WriteFile(secondCasePath, []byte(`{
  "id": "case.beta",
  "title": "Find Item",
  "request": {
    "method": "GET",
    "path": "/v1/items/item-override"
  },
  "assertions": {
    "expectedStatusCodes": [200],
    "responseContains": ["found"]
  }
}`), 0o644); err != nil {
		t.Fatalf("write second api case: %v", err)
	}

	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
			{ID: "node.beta", DisplayName: "Node Beta"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", CasePath: firstCasePath, BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence")},
			{ID: "case.beta", DisplayName: "Case Beta", NodeID: "node.beta", CasePath: secondCasePath, BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence")},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"change-001","nodeIds":["node.alpha","node.beta"],"overrides":{"id":"item-override"}}`
	var created struct {
		OK                  bool   `json:"ok"`
		BatchRunID          string `json:"batchRunId"`
		RequestID           string `json:"requestId"`
		Status              string `json:"status"`
		ReportURL           string `json:"reportUrl"`
		HTMLReportURL       string `json:"htmlReportUrl"`
		JUnitReportURL      string `json:"junitReportUrl"`
		ArtifactManifestURL string `json:"artifactManifestUrl"`
		Total               int    `json:"total"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", body, http.StatusAccepted, &created)
	if !created.OK || created.BatchRunID == "" || created.RequestID != "change-001" || created.ReportURL == "" || created.HTMLReportURL == "" || created.JUnitReportURL == "" || created.ArtifactManifestURL == "" || created.Total != 2 {
		t.Fatalf("api case batch run response = %#v", created)
	}

	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.Completed != 2 || report.Passed != 2 || report.Failed != 0 || len(report.Cases) != 2 {
		t.Fatalf("api case batch report = %#v", report)
	}
	if report.HTMLReportPath == "" || !strings.HasPrefix(report.HTMLReportPath, filepath.Join(dir, "evidence")) || report.HTMLReportURL != created.HTMLReportURL {
		t.Fatalf("api case batch html report fields = %#v", report)
	}
	if report.JUnitReportPath == "" || !strings.HasPrefix(report.JUnitReportPath, filepath.Join(dir, "evidence")) || report.JUnitReportURL != created.JUnitReportURL {
		t.Fatalf("api case batch junit report fields = %#v", report)
	}
	if report.ArtifactManifestPath == "" || !strings.HasPrefix(report.ArtifactManifestPath, filepath.Join(dir, "evidence")) || report.ArtifactManifestURL != created.ArtifactManifestURL {
		t.Fatalf("api case batch artifact manifest fields = %#v", report)
	}
	htmlResp, err := http.Get(server.URL + report.HTMLReportURL)
	if err != nil {
		t.Fatalf("get api case batch html report: %v", err)
	}
	defer htmlResp.Body.Close()
	if htmlResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(htmlResp.Body)
		t.Fatalf("api case batch html status = %d body=%s", htmlResp.StatusCode, raw)
	}
	htmlRaw, err := io.ReadAll(htmlResp.Body)
	if err != nil {
		t.Fatalf("read api case batch html report: %v", err)
	}
	html := string(htmlRaw)
	if !strings.Contains(html, "API Case Batch Report") || !strings.Contains(html, "change-001") || !strings.Contains(html, "case.alpha") || !strings.Contains(html, "case.beta") {
		t.Fatalf("api case batch html report = %s", html)
	}
	if _, err := os.Stat(report.HTMLReportPath); err != nil {
		t.Fatalf("stat api case batch html report: %v", err)
	}
	junitResp, err := http.Get(server.URL + report.JUnitReportURL)
	if err != nil {
		t.Fatalf("get api case batch junit report: %v", err)
	}
	defer junitResp.Body.Close()
	junitRaw, err := io.ReadAll(junitResp.Body)
	if err != nil {
		t.Fatalf("read api case batch junit report: %v", err)
	}
	for _, want := range []string{`<testsuite name="API Case Batch change-001" tests="2" failures="0"`, `name="case.alpha"`, `classname="node.alpha"`} {
		if !strings.Contains(string(junitRaw), want) {
			t.Fatalf("api case batch junit missing %q: %s", want, junitRaw)
		}
	}
	if _, err := os.Stat(report.JUnitReportPath); err != nil {
		t.Fatalf("stat api case batch junit report: %v", err)
	}
	manifestResp, err := http.Get(server.URL + report.ArtifactManifestURL)
	if err != nil {
		t.Fatalf("get api case batch artifact manifest: %v", err)
	}
	defer manifestResp.Body.Close()
	var manifest struct {
		BatchRunID string `json:"batchRunId"`
		Status     string `json:"status"`
		Artifacts  []struct {
			Kind      string `json:"kind"`
			CaseID    string `json:"caseId,omitempty"`
			URL       string `json:"url,omitempty"`
			Path      string `json:"path,omitempty"`
			MediaType string `json:"mediaType,omitempty"`
		} `json:"artifacts"`
	}
	if err := json.NewDecoder(manifestResp.Body).Decode(&manifest); err != nil {
		t.Fatalf("decode api case batch artifact manifest: %v", err)
	}
	if manifest.BatchRunID != created.BatchRunID || manifest.Status != store.StatusPassed {
		t.Fatalf("artifact manifest header = %#v", manifest)
	}
	artifactKeys := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		artifactKeys[artifact.Kind+"|"+artifact.CaseID] = true
		if artifact.Kind == "junit" && artifact.MediaType != "application/xml" {
			t.Fatalf("junit artifact = %#v", artifact)
		}
	}
	for _, want := range []string{"json|", "html|", "junit|", "case-detail|case.alpha", "case-evidence|case.alpha", "case-detail|case.beta", "case-evidence|case.beta"} {
		if !artifactKeys[want] {
			t.Fatalf("artifact manifest missing %q: %#v", want, manifest.Artifacts)
		}
	}
	if _, err := os.Stat(report.ArtifactManifestPath); err != nil {
		t.Fatalf("stat api case batch artifact manifest: %v", err)
	}
	for _, item := range report.Cases {
		if item.RunID == "" || item.CaseRunID != item.RunID+".case" || item.ViewerURL == "" || item.DetailURL == "" || item.Status != store.StatusPassed || item.ElapsedMs < 0 {
			t.Fatalf("api case batch case report = %#v", item)
		}
	}

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("stored runs = %#v", runs)
	}
	batchRun, err := s.GetRun(ctx, created.BatchRunID)
	if err != nil {
		t.Fatalf("get stored batch run: %v", err)
	}
	if batchRun.Status != store.StatusPassed || batchRun.ProfileID != "sample" || batchRun.EvidenceRoot != filepath.Dir(report.HTMLReportPath) {
		t.Fatalf("stored batch run = %#v", batchRun)
	}
	batchEvidence, err := s.ListEvidence(ctx, created.BatchRunID)
	if err != nil {
		t.Fatalf("list batch evidence: %v", err)
	}
	evidenceByKind := map[string]store.EvidenceRecord{}
	for _, row := range batchEvidence {
		evidenceByKind[row.Kind] = row
	}
	for kind, want := range map[string]string{
		"html":              report.HTMLReportPath,
		"junit":             report.JUnitReportPath,
		"artifact-manifest": report.ArtifactManifestPath,
		"failure-summary":   filepath.Join(filepath.Dir(report.HTMLReportPath), "failures.json"),
	} {
		row, ok := evidenceByKind[kind]
		if !ok {
			t.Fatalf("batch evidence missing %s: %#v", kind, batchEvidence)
		}
		if row.URI != want || row.RunID != created.BatchRunID || row.Category != "report" || row.Visibility != "public" {
			t.Fatalf("batch evidence %s = %#v", kind, row)
		}
	}
}

func TestServerStartsAsyncAPICaseBatchRunForAllNodeCases(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/node-cases/") {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()

	dir := t.TempDir()
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Create Item", Method: "GET", Path: "/v1/node-cases"},
		},
	}
	for i := 1; i <= 3; i++ {
		caseID := fmt.Sprintf("case.alpha.%02d", i)
		casePath := filepath.Join(dir, caseID+".json")
		if err := os.WriteFile(casePath, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": "Node Case",
  "request": {"method": "GET", "path": "/v1/node-cases/%02d"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, caseID, i)), 0o644); err != nil {
			t.Fatalf("write api case: %v", err)
		}
		bundle.APICases = append(bundle.APICases, profile.APICase{
			ID:          caseID,
			DisplayName: fmt.Sprintf("Node Case %02d", i),
			NodeID:      "node.alpha",
			Scenario:    fmt.Sprintf("scenario-%02d", i),
			CasePath:    casePath,
			BaseURL:     target.URL,
			EvidenceDir: filepath.Join(dir, "evidence"),
		})
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"node-all-001","nodeIds":["node.alpha"]}`
	var created struct {
		ReportURL      string `json:"reportUrl"`
		HTMLReportURL  string `json:"htmlReportUrl"`
		JUnitReportURL string `json:"junitReportUrl"`
		Total          int    `json:"total"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", body, http.StatusAccepted, &created)
	if created.Total != 3 || created.JUnitReportURL == "" {
		t.Fatalf("created batch = %#v", created)
	}

	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.Completed != 3 || report.Passed != 3 || len(report.Cases) != 3 {
		t.Fatalf("node batch report = %#v", report)
	}
	if len(report.Nodes) != 1 || report.Nodes[0].DisplayName != "Node Alpha" || report.Nodes[0].Operation != "Create Item" || report.Nodes[0].Method != "GET" || report.Nodes[0].Path != "/v1/node-cases" {
		t.Fatalf("node batch report nodes = %#v", report.Nodes)
	}
	if report.Cases[0].DisplayName != "Node Case 01" || report.Cases[0].Scenario != "scenario-01" || report.Cases[0].NodeDisplayName != "Node Alpha" || report.Cases[0].Operation != "Create Item" {
		t.Fatalf("node batch report case metadata = %#v", report.Cases[0])
	}
	htmlResp, err := http.Get(server.URL + created.HTMLReportURL)
	if err != nil {
		t.Fatalf("get node batch html report: %v", err)
	}
	defer htmlResp.Body.Close()
	htmlRaw, err := io.ReadAll(htmlResp.Body)
	if err != nil {
		t.Fatalf("read node batch html report: %v", err)
	}
	html := string(htmlRaw)
	for _, want := range []string{"Node Alpha", "Create Item", "GET", "/v1/node-cases", "Node Case 01", "scenario-01"} {
		if !strings.Contains(html, want) {
			t.Fatalf("node batch html missing %q: %s", want, html)
		}
	}
	junitResp, err := http.Get(server.URL + created.JUnitReportURL)
	if err != nil {
		t.Fatalf("get node batch junit report: %v", err)
	}
	defer junitResp.Body.Close()
	junitRaw, err := io.ReadAll(junitResp.Body)
	if err != nil {
		t.Fatalf("read node batch junit report: %v", err)
	}
	for _, want := range []string{`<testsuite name="API Case Batch node-all-001" tests="3" failures="0"`, `name="case.alpha.01"`, `classname="node.alpha"`} {
		if !strings.Contains(string(junitRaw), want) {
			t.Fatalf("node batch junit missing %q: %s", want, junitRaw)
		}
	}
}

func TestServerStartsAsyncAPICaseBatchRunForExactCaseIDs(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/exact/first", "/v1/exact/third":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/exact/second":
			t.Fatalf("unselected case should not be run")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer target.Close()

	dir := t.TempDir()
	casePath := func(id string, path string) string {
		t.Helper()
		out := filepath.Join(dir, id+".json")
		if err := os.WriteFile(out, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": %q,
  "request": {"method": "GET", "path": %q},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, id, id, path)), 0o644); err != nil {
			t.Fatalf("write api case %s: %v", id, err)
		}
		return out
	}
	bundle := profile.Bundle{
		ID:      "sample",
		BaseDir: dir,
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Exact"},
		},
		APICases: []profile.APICase{
			{ID: "case.first", DisplayName: "First Case", NodeID: "node.alpha", CasePath: casePath("case.first", "/v1/exact/first"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 1},
			{ID: "case.second", DisplayName: "Second Case", NodeID: "node.alpha", CasePath: casePath("case.second", "/v1/exact/second"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 2},
			{ID: "case.third", DisplayName: "Third Case", NodeID: "node.alpha", CasePath: casePath("case.third", "/v1/exact/third"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 3},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"exact-001","caseIds":["case.third","case.first"]}`
	var created struct {
		ReportURL string   `json:"reportUrl"`
		CaseIDs   []string `json:"caseIds"`
		Total     int      `json:"total"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", body, http.StatusAccepted, &created)
	if created.Total != 2 || strings.Join(created.CaseIDs, ",") != "case.third,case.first" {
		t.Fatalf("created exact batch = %#v", created)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.Completed != 2 || len(report.Cases) != 2 {
		t.Fatalf("exact batch report = %#v", report)
	}
	if report.Cases[0].CaseID != "case.third" || report.Cases[1].CaseID != "case.first" {
		t.Fatalf("exact case order = %#v", report.Cases)
	}
}

func TestServerExposesAPICaseBatchFailureSummary(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/failures/pass":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/failures/fail":
			http.Error(w, "not ok", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer target.Close()

	dir := t.TempDir()
	casePath := func(id string, path string) string {
		t.Helper()
		out := filepath.Join(dir, id+".json")
		if err := os.WriteFile(out, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": %q,
  "request": {"method": "GET", "path": %q},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, id, id, path)), 0o644); err != nil {
			t.Fatalf("write api case %s: %v", id, err)
		}
		return out
	}
	bundle := profile.Bundle{
		ID: "sample",
		FailureCategories: []profile.FailureCategoryRule{
			{
				Name: "Product errors",
				Matchers: profile.FailureCategoryMatchers{
					Statuses:          []string{store.StatusFailed},
					FailureCategories: []string{"assertion-mismatch"},
					MessageContains:   []string{"not expected"},
				},
			},
			{
				Name: "Later matching rule",
				Matchers: profile.FailureCategoryMatchers{
					Statuses:          []string{store.StatusFailed},
					FailureCategories: []string{"assertion-mismatch"},
				},
			},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Failure Summary"},
		},
		APICases: []profile.APICase{
			{ID: "case.pass", DisplayName: "Passing Case", NodeID: "node.alpha", CasePath: casePath("case.pass", "/v1/failures/pass"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence")},
			{ID: "case.fail", DisplayName: "Failing Case", NodeID: "node.alpha", CasePath: casePath("case.fail", "/v1/failures/fail"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence")},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	var created struct {
		ReportURL         string `json:"reportUrl"`
		FailureSummaryURL string `json:"failureSummaryUrl"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", `{"requestId":"failure-001","caseIds":["case.pass","case.fail"]}`, http.StatusAccepted, &created)
	if created.FailureSummaryURL == "" {
		t.Fatalf("failure summary url missing: %#v", created)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if report.OK || report.Status != store.StatusFailed || report.Failed != 1 {
		t.Fatalf("failed batch report = %#v", report)
	}

	summaryResp, err := http.Get(server.URL + created.FailureSummaryURL)
	if err != nil {
		t.Fatalf("get failure summary: %v", err)
	}
	defer summaryResp.Body.Close()
	var summary struct {
		OK         bool   `json:"ok"`
		BatchRunID string `json:"batchRunId"`
		RequestID  string `json:"requestId"`
		Failed     int    `json:"failed"`
		Failures   []struct {
			CaseID          string `json:"caseId"`
			CaseRunID       string `json:"caseRunId"`
			Status          string `json:"status"`
			FailureCategory string `json:"failureCategory"`
			DetailURL       string `json:"detailUrl"`
			EvidencePath    string `json:"evidencePath"`
			Error           string `json:"error"`
		} `json:"failures"`
	}
	if err := json.NewDecoder(summaryResp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode failure summary: %v", err)
	}
	if summary.OK || summary.RequestID != "failure-001" || summary.Failed != 1 || len(summary.Failures) != 1 {
		t.Fatalf("failure summary = %#v", summary)
	}
	failure := summary.Failures[0]
	if failure.CaseID != "case.fail" || failure.Status != store.StatusFailed || failure.FailureCategory != "Product errors" || failure.CaseRunID == "" || failure.DetailURL == "" || failure.EvidencePath == "" || failure.Error == "" {
		t.Fatalf("failure item = %#v", failure)
	}
}

func TestServerStartsAsyncAPICaseBatchRunForMaintainedSuiteRunStates(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/suite/variant", "/v1/suite/unrun":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/suite/passed":
			t.Fatalf("already passed case should not be rerun")
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer target.Close()

	dir := t.TempDir()
	casePath := func(id string, path string) string {
		t.Helper()
		out := filepath.Join(dir, id+".json")
		if err := os.WriteFile(out, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": %q,
  "request": {"method": "GET", "path": %q},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, id, id, path)), 0o644); err != nil {
			t.Fatalf("write api case %s: %v", id, err)
		}
		return out
	}
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Suite", Method: "GET", Path: "/v1/suite"},
		},
		APICases: []profile.APICase{
			{ID: "case.passed", DisplayName: "Passed Case", NodeID: "node.alpha", Tags: []string{"regression"}, Owner: "team-a", Priority: "p0", CasePath: casePath("case.passed", "/v1/suite/passed"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 1},
			{ID: "case.variant", DisplayName: "Variant Case", NodeID: "node.alpha", Tags: []string{"regression"}, Owner: "team-a", Priority: "p1", CasePath: casePath("case.variant", "/v1/suite/variant"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, Owner: "team-b", Priority: "p2", CasePath: casePath("case.unrun", "/v1/suite/unrun"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 3},
			{ID: "case.other", DisplayName: "Other Case", NodeID: "node.alpha", Tags: []string{"smoke"}, Owner: "team-a", Priority: "p2", CasePath: casePath("case.other", "/v1/suite/other"), BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), SortOrder: 4},
		},
	}
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	for _, item := range []struct {
		runID  string
		caseID string
		status string
		at     time.Time
	}{
		{runID: "run.passed.latest", caseID: "case.passed", status: store.StatusPassed, at: base},
		{runID: "run.variant.latest", caseID: "case.variant", status: store.StatusFailed, at: base.Add(time.Minute)},
	} {
		_, err = s.CreateRun(ctx, store.Run{ID: item.runID, ProfileID: "sample", WorkflowID: item.caseID, Status: item.status, StartedAt: item.at, FinishedAt: item.at.Add(time.Second), CreatedAt: item.at, UpdatedAt: item.at.Add(time.Second)})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{ID: item.runID + ".case", RunID: item.runID, CaseID: item.caseID, Status: item.status, StartedAt: item.at, FinishedAt: item.at.Add(time.Second), CreatedAt: item.at})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"suite-rerun-001","suite":{"tags":["regression"],"status":"active","runStates":["failed","not-run"]}}`
	var created struct {
		ReportURL string `json:"reportUrl"`
		Total     int    `json:"total"`
		Suite     struct {
			Tags      []string `json:"tags"`
			RunStates []string `json:"runStates"`
		} `json:"suite"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", body, http.StatusAccepted, &created)
	if created.Total != 2 || strings.Join(created.Suite.Tags, ",") != "regression" || strings.Join(created.Suite.RunStates, ",") != "failed,not-run" {
		t.Fatalf("suite batch response = %#v", created)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Completed != 2 || report.Passed != 2 || len(report.Cases) != 2 {
		t.Fatalf("suite batch report = %#v", report)
	}
	gotCases := []string{report.Cases[0].CaseID, report.Cases[1].CaseID}
	if strings.Join(gotCases, ",") != "case.variant,case.unrun" {
		t.Fatalf("suite rerun cases = %#v", gotCases)
	}
}

func TestServerStartsAsyncAPICaseBatchRunForWorkflow(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/v1/workflow-steps/") {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()

	dir := t.TempDir()
	bundle := profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.ten", DisplayName: "Ten Step Workflow"},
		},
	}
	for i := 1; i <= 10; i++ {
		stepID := fmt.Sprintf("step-%02d", i)
		nodeID := fmt.Sprintf("node.step.%02d", i)
		caseID := fmt.Sprintf("case.step.%02d", i)
		casePath := filepath.Join(dir, caseID+".json")
		if err := os.WriteFile(casePath, []byte(fmt.Sprintf(`{
  "id": %q,
  "title": "Workflow Step",
  "request": {"method": "GET", "path": "/v1/workflow-steps/%02d"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`, caseID, i)), 0o644); err != nil {
			t.Fatalf("write api case: %v", err)
		}
		bundle.InterfaceNodes = append(bundle.InterfaceNodes, profile.InterfaceNode{ID: nodeID, DisplayName: nodeID})
		bundle.APICases = append(bundle.APICases, profile.APICase{
			ID:          caseID,
			DisplayName: caseID,
			NodeID:      nodeID,
			CasePath:    casePath,
			BaseURL:     target.URL,
			EvidenceDir: filepath.Join(dir, "evidence"),
		})
		bundle.WorkflowBindings = append(bundle.WorkflowBindings, profile.WorkflowBinding{
			WorkflowID: "workflow.ten",
			StepID:     stepID,
			NodeID:     nodeID,
			CaseID:     caseID,
			Required:   true,
			SortOrder:  i,
		})
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	body := `{"requestId":"workflow-001","workflowId":"workflow.ten"}`
	var created struct {
		ReportURL  string `json:"reportUrl"`
		WorkflowID string `json:"workflowId"`
		Total      int    `json:"total"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", body, http.StatusAccepted, &created)
	if created.WorkflowID != "workflow.ten" || created.Total != 10 {
		t.Fatalf("workflow batch response = %#v", created)
	}

	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.WorkflowID != "workflow.ten" || report.Completed != 10 || report.Passed != 10 || len(report.Cases) != 10 {
		t.Fatalf("workflow batch report = %#v", report)
	}
	if report.Acceptance.TemplateID != "environment.workflow.skywalking.v1" || report.Acceptance.WorkflowID != "workflow.ten" || report.Acceptance.OK || report.Acceptance.ExpectedSteps != 10 || report.Acceptance.CompletedSteps != 10 || report.Acceptance.PassedSteps != 10 || report.Acceptance.TopologyProvider != "skywalking" {
		t.Fatalf("workflow acceptance report should require SkyWalking topology: %#v", report.Acceptance)
	}
	if len(report.Acceptance.Steps) != 10 || !report.Acceptance.Steps[0].EvidenceComplete || report.Acceptance.Steps[0].TopologyComplete {
		t.Fatalf("workflow acceptance steps = %#v", report.Acceptance.Steps)
	}
	if report.Cases[0].StepID != "step-01" || report.Cases[9].StepID != "step-10" {
		t.Fatalf("workflow step order = %#v", report.Cases)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 11 {
		t.Fatalf("stored runs = %#v", runs)
	}
	batchRun, err := s.GetRun(ctx, report.BatchRunID)
	if err != nil {
		t.Fatalf("get stored workflow batch run: %v", err)
	}
	if batchRun.Status != store.StatusPassed || batchRun.WorkflowID != "workflow.ten" {
		t.Fatalf("stored workflow batch run = %#v", batchRun)
	}
	var storedSummary struct {
		Summary struct {
			ExpectedStepCount int `json:"expectedStepCount"`
			StepCount         int `json:"stepCount"`
			Passed            int `json:"passed"`
			Failed            int `json:"failed"`
		} `json:"summary"`
		Steps []struct {
			StepID string `json:"stepId"`
			CaseID string `json:"caseId"`
			Status string `json:"status"`
		} `json:"steps"`
		Acceptance struct {
			OK               bool   `json:"ok"`
			TemplateID       string `json:"templateId"`
			TopologyProvider string `json:"topologyProvider"`
		} `json:"acceptance"`
	}
	if err := json.Unmarshal([]byte(batchRun.SummaryJSON), &storedSummary); err != nil {
		t.Fatalf("decode stored workflow batch summary: %v", err)
	}
	if storedSummary.Summary.ExpectedStepCount != 10 || storedSummary.Summary.StepCount != 10 || storedSummary.Summary.Passed != 10 || storedSummary.Summary.Failed != 0 || len(storedSummary.Steps) != 10 {
		t.Fatalf("stored workflow run summary counts = %#v", storedSummary)
	}
	if storedSummary.Steps[0].StepID != "step-01" || storedSummary.Steps[0].CaseID == "" || storedSummary.Steps[0].Status != store.StatusPassed {
		t.Fatalf("stored workflow run steps = %#v", storedSummary.Steps)
	}
	if storedSummary.Acceptance.OK || storedSummary.Acceptance.TemplateID != "environment.workflow.skywalking.v1" || storedSummary.Acceptance.TopologyProvider != "skywalking" {
		t.Fatalf("stored workflow acceptance summary = %#v", storedSummary.Acceptance)
	}
}

func TestServerRejectsAsyncAPICaseBatchWithoutNodes(t *testing.T) {
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, nil))
	defer server.Close()

	var payload map[string]any
	postJSONInto(t, server.URL+"/api/cases/batch-runs", `{"requestId":"change-001"}`, http.StatusBadRequest, &payload)
}
