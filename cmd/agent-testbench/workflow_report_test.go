package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type workflowReportCommandReport struct {
	OK        bool   `json:"ok"`
	RunID     string `json:"runId"`
	ReportURL string `json:"reportUrl"`
	Counts    struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
	} `json:"counts"`
	Steps []struct {
		RunID     string `json:"runId"`
		CaseRunID string `json:"caseRunId"`
		DetailURL string `json:"detailUrl"`
	} `json:"steps"`
}

func TestWorkflowReportWritesReportWhenStepFails(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-workflow-report-fail-pg")
	runWorkflowReportWritesReportWhenStepFails(t, storeRef, "PostgreSQL")
}

func TestWorkflowReportUsesNamedMySQLActiveStoreWhenStepFails(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-workflow-report-fail-mysql")
	runWorkflowReportWritesReportWhenStepFails(t, storeRef, "MySQL")
}

func runWorkflowReportWritesReportWhenStepFails(t *testing.T, _ string, label string) {
	t.Helper()
	serverURL := newFailingWorkflowReportServer(t)
	fixture := writeUniqueWorkflowBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", fixture.profileDir)
	workflowID := discoverWorkflowReportID(t, label, fixture.workflowID)

	outputDir := filepath.Join(t.TempDir(), "workflow-report")
	report := runWorkflowReportJSON(t, label, workflowID, serverURL, outputDir)
	requireFailedWorkflowReport(t, label, report)
	requireWorkflowReportHTML(t, label, report, fixture, outputDir)
}

func newFailingWorkflowReportServer(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/first":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"item_id":"item-001"}`)
		case "/second":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"status":"failed"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func discoverWorkflowReportID(t *testing.T, label string, workflowID string) string {
	t.Helper()

	listOut := runCLI(t, "workflow", "discover", "--filter", workflowID, "--json")
	var listReport struct {
		Items []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listOut), &listReport); err != nil {
		t.Fatalf("decode %s workflow discover json: %v\n%s", label, err, listOut)
	}
	if len(listReport.Items) != 1 || listReport.Items[0].ID != workflowID {
		t.Fatalf("%s workflow discover = %#v", label, listReport.Items)
	}
	return listReport.Items[0].ID
}

func runWorkflowReportJSON(t *testing.T, label string, workflowID string, serverURL string, outputDir string) workflowReportCommandReport {
	t.Helper()

	out := runCLI(t,
		"workflow", "report",
		"--workflow", workflowID,
		"--base-url", serverURL,
		"--output-dir", outputDir,
		"--json",
	)

	var report workflowReportCommandReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s workflow report json: %v\n%s", label, err, out)
	}
	return report
}

func requireFailedWorkflowReport(t *testing.T, label string, report workflowReportCommandReport) {
	t.Helper()

	if report.OK || report.RunID == "" || report.Counts.Total != 2 || report.Counts.Passed != 1 || report.Counts.Failed != 1 {
		t.Fatalf("%s workflow report = %#v", label, report)
	}
	if len(report.Steps) != 2 || report.Steps[1].RunID == "" || report.Steps[1].CaseRunID != report.Steps[1].RunID+".case" || report.Steps[1].DetailURL == "" {
		t.Fatalf("%s workflow report evidence handles = %#v", label, report.Steps)
	}
}

func requireWorkflowReportHTML(t *testing.T, label string, report workflowReportCommandReport, fixture workflowBatchReportFixture, outputDir string) {
	t.Helper()

	htmlPath := filepath.Join(outputDir, "report.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("%s html report missing: %v", label, err)
	}
	for _, want := range []string{fixture.workflowName, "First Step", "Second Step", "failed", "caseRunId"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("%s workflow html missing %q:\n%s", label, want, html)
		}
	}
	if report.ReportURL != htmlPath {
		t.Fatalf("%s report url = %q want %q", label, report.ReportURL, htmlPath)
	}
}
