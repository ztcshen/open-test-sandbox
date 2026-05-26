package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInterfaceNodeCaseReportRunsAllCasesByTargetName(t *testing.T) {
	server := newInterfaceNodeReportTargetServer()
	defer server.Close()
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store", "sqlite://"+storePath)
	nodeID := requireDiscoveredInterfaceNode(t, "sqlite://"+storePath, "Result Lookup", "node.alpha", "interface-node discover")

	outputDir := filepath.Join(t.TempDir(), "report")
	report := runInterfaceNodeCaseReportCommand(t, nodeID, "sqlite://"+storePath, server.URL, outputDir, "report")
	requireInterfaceNodeCaseReport(t, report, "node.alpha", "report")
	requireInterfaceNodeReportBodyPreviewsRedacted(t, report, "report")
	if _, err := os.Stat(filepath.Join(outputDir, "report.json")); err != nil {
		t.Fatalf("json report missing: %v", err)
	}
	htmlPath := filepath.Join(outputDir, "report.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("html report missing: %v", err)
	}
	for _, want := range []string{"Result Lookup", "Case Alpha Default", "Case Alpha Variant", "passed", "caseRunId"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("html report missing %q:\n%s", want, html)
		}
	}
	for _, leaked := range []string{"report-secret", "variant-secret"} {
		if strings.Contains(string(html), leaked) {
			t.Fatalf("html report leaked %q:\n%s", leaked, html)
		}
	}
	if report.ReportURL != htmlPath {
		t.Fatalf("report url = %q want %q", report.ReportURL, htmlPath)
	}
	requireNoRuntimeSQLite(t, outputDir, "report should use selected Store")
}

func TestCaseExecutionAndInterfaceReportUseNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-case-exec-pg")
	runCaseExecutionAndInterfaceReportUseNamedActiveStore(t, "pg", "PostgreSQL")
}

func TestCaseExecutionAndInterfaceReportUseNamedMySQLActiveStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "daily-case-exec-mysql")
	runCaseExecutionAndInterfaceReportUseNamedActiveStore(t, "mysql", "MySQL")
}

func runCaseExecutionAndInterfaceReportUseNamedActiveStore(t *testing.T, runLabel string, label string) {
	t.Helper()
	server := newInterfaceNodeReportTargetServer()
	defer server.Close()

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	dir := t.TempDir()
	runFileBackedCaseThroughActiveStore(t, label, runLabel, suffix, dir, server.URL)

	profileDir := writeInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)
	runCatalogCaseThroughActiveStore(t, label, runLabel, suffix, dir, server.URL)
	nodeID := requireDiscoveredInterfaceNode(t, "", "Result Lookup", "node.alpha", label+" interface-node discover")
	outputDir := filepath.Join(t.TempDir(), runLabel+"-interface-report")
	report := runInterfaceNodeCaseReportCommand(t, nodeID, "", server.URL, outputDir, label+" interface-node report")
	requireInterfaceNodeCaseReport(t, report, "node.alpha", label+" interface-node report")
	requireInterfaceNodeReportBodyPreviewsRedacted(t, report, label+" interface-node report")
	requireNoRuntimeSQLite(t, outputDir, label+" interface-node report should use active Store")
}

type interfaceNodeCaseReportForTest struct {
	OK        bool   `json:"ok"`
	NodeID    string `json:"nodeId"`
	ReportURL string `json:"reportUrl"`
	Counts    struct {
		Total          int `json:"total"`
		Passed         int `json:"passed"`
		Failed         int `json:"failed"`
		DerivedConfigs int `json:"derivedConfigs"`
	} `json:"counts"`
	Results []struct {
		RunID       string `json:"runId"`
		CaseRunID   string `json:"caseRunId"`
		DetailURL   string `json:"detailUrl"`
		BodyPreview string `json:"bodyPreview"`
	} `json:"results"`
}

func newInterfaceNodeReportTargetServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/items":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"created"}`)
		case "/lookup":
			writeInterfaceNodeLookupResponse(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func writeInterfaceNodeLookupResponse(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("mode") {
	case "bad":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"status":"rejected","password":"variant-secret"}`)
	default:
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"accepted","token":"report-secret"}`)
	}
}

func runFileBackedCaseThroughActiveStore(t *testing.T, label, runLabel, suffix, dir, serverURL string) {
	t.Helper()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	fileRunID := runLabel + "-file-case-run-" + suffix
	fileEvidenceDir := filepath.Join(dir, "file-evidence")
	fileOut := runCLI(t,
		"case", "run",
		"--case", casePath,
		"--base-url", serverURL,
		"--run-id", fileRunID,
		"--evidence-dir", fileEvidenceDir,
		"--profile", "sample",
	)
	if !strings.Contains(fileOut, "Case Run: "+fileRunID) || !strings.Contains(fileOut, "Status: passed") {
		t.Fatalf("%s file case run via active SQL Store = %q", label, fileOut)
	}
	caseRunsOut := runCLI(t, "case", "runs", "--run", fileRunID, "--json")
	if !strings.Contains(caseRunsOut, fileRunID) || !strings.Contains(caseRunsOut, "case.alpha") {
		t.Fatalf("%s case runs via active SQL Store = %s", label, caseRunsOut)
	}
	fileEvidenceOut := runCLI(t, "case", "evidence", "--run", fileRunID, "--case-id", "case.alpha", "--json")
	for _, want := range []string{fileRunID, "case.alpha", "request", "response"} {
		if !strings.Contains(fileEvidenceOut, want) {
			t.Fatalf("%s file case evidence via active SQL Store missing %q:\n%s", label, want, fileEvidenceOut)
		}
	}
	evidenceListOut := runCLI(t, "evidence", "list", "--run", fileRunID, "--json")
	if !strings.Contains(evidenceListOut, fileRunID) || !strings.Contains(evidenceListOut, "response") {
		t.Fatalf("%s evidence list via active SQL Store = %s", label, evidenceListOut)
	}
}

func runCatalogCaseThroughActiveStore(t *testing.T, label, runLabel, suffix, dir, serverURL string) {
	t.Helper()
	catalogRunID := runLabel + "-catalog-case-run-" + suffix
	catalogOut := runCLI(t,
		"case", "run",
		"--case-id", "case.alpha.default",
		"--base-url", serverURL,
		"--run-id", catalogRunID,
		"--evidence-dir", filepath.Join(dir, "catalog-evidence"),
		"--profile", "sample",
		"--json",
	)
	var catalogRun struct {
		RunID  string `json:"runId"`
		CaseID string `json:"caseId"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(catalogOut), &catalogRun); err != nil {
		t.Fatalf("decode %s catalog case run json: %v\n%s", label, err, catalogOut)
	}
	if catalogRun.RunID != catalogRunID || catalogRun.CaseID != "case.alpha.default" || catalogRun.Status != "passed" {
		t.Fatalf("%s catalog case run = %#v", label, catalogRun)
	}
	catalogEvidenceOut := runCLI(t, "case", "evidence", "--run", catalogRunID, "--case-id", "case.alpha.default", "--json")
	for _, want := range []string{catalogRunID, "case.alpha.default", "request", "response"} {
		if !strings.Contains(catalogEvidenceOut, want) {
			t.Fatalf("%s catalog case evidence via active SQL Store missing %q:\n%s", label, want, catalogEvidenceOut)
		}
	}
}

func requireDiscoveredInterfaceNode(t *testing.T, storeURL, filter, wantID, label string) string {
	t.Helper()
	args := []string{"interface-node", "discover", "--filter", filter, "--json"}
	if storeURL != "" {
		args = append([]string{"interface-node", "discover", "--store", storeURL}, "--filter", filter, "--json")
	}
	listOut := runCLI(t, args...)
	var listReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listOut), &listReport); err != nil {
		t.Fatalf("decode %s json: %v\n%s", label, err, listOut)
	}
	if len(listReport.Items) != 1 || listReport.Items[0].ID != wantID {
		t.Fatalf("%s = %#v", label, listReport.Items)
	}
	return listReport.Items[0].ID
}

func runInterfaceNodeCaseReportCommand(t *testing.T, nodeID, storeURL, baseURL, outputDir, label string) interfaceNodeCaseReportForTest {
	t.Helper()
	args := []string{"interface-node", "case", "report", "--node", nodeID}
	if storeURL != "" {
		args = append(args, "--store", storeURL)
	}
	args = append(args,
		"--base-url", baseURL,
		"--output-dir", outputDir,
		"--timeout-seconds", "1",
		"--json",
	)
	reportOut := runCLI(t, args...)
	var report interfaceNodeCaseReportForTest
	if err := json.Unmarshal([]byte(reportOut), &report); err != nil {
		t.Fatalf("decode %s json: %v\n%s", label, err, reportOut)
	}
	return report
}

func requireInterfaceNodeCaseReport(t *testing.T, report interfaceNodeCaseReportForTest, wantNodeID, label string) {
	t.Helper()
	if !report.OK || report.NodeID != wantNodeID || report.Counts.Total != 2 || report.Counts.Passed != 2 || report.Counts.Failed != 0 || report.Counts.DerivedConfigs != 1 {
		t.Fatalf("%s = %#v", label, report)
	}
	if len(report.Results) != 2 || report.Results[0].RunID == "" || report.Results[0].CaseRunID != report.Results[0].RunID+".case" || report.Results[0].DetailURL == "" {
		t.Fatalf("%s handles = %#v", label, report.Results)
	}
}

func requireInterfaceNodeReportBodyPreviewsRedacted(t *testing.T, report interfaceNodeCaseReportForTest, label string) {
	t.Helper()
	for _, item := range report.Results {
		if strings.Contains(item.BodyPreview, "report-secret") || strings.Contains(item.BodyPreview, "variant-secret") {
			t.Fatalf("%s body preview leaked sensitive value: %#v", label, item)
		}
		if !strings.Contains(item.BodyPreview, "[REDACTED]") {
			t.Fatalf("%s body preview was not redacted: %#v", label, item)
		}
	}
}

func requireNoRuntimeSQLite(t *testing.T, outputDir string, label string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(outputDir, "runtime.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s without creating runtime.sqlite, stat err=%v", label, err)
	}
}
