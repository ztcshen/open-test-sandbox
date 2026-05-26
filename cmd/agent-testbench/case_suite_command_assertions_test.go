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

	"agent-testbench/internal/store"
)

type caseSuiteQualityReportOutput struct {
	OK            bool   `json:"ok"`
	ProfileID     string `json:"profileId"`
	ReportURL     string `json:"reportUrl"`
	JSONReportURL string `json:"jsonReportUrl"`
	QualityPlan   struct {
		Counts struct {
			Total            int `json:"total"`
			DraftCase        int `json:"draftCase"`
			CompleteMetadata int `json:"completeMetadata"`
			AddRunnable      int `json:"addRunnable"`
			AddExecution     int `json:"addExecution"`
		} `json:"counts"`
		Actions []struct {
			Type            string `json:"type"`
			CaseID          string `json:"caseId"`
			SuggestedCaseID string `json:"suggestedCaseId"`
		} `json:"actions"`
	} `json:"qualityPlan"`
}

func runCaseSuiteQualityReportJSON(t *testing.T, label string, profileDir string, outputDir string) caseSuiteQualityReportOutput {
	t.Helper()
	out := runCLI(t, "case", "suite", "quality-report", "--profile", profileDir, "--status", "active", "--output-dir", outputDir, "--json")
	var report caseSuiteQualityReportOutput
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s suite quality report json: %v\n%s", label, err, out)
	}
	return report
}

func requireCaseSuiteQualityReportJSON(t *testing.T, label string, report caseSuiteQualityReportOutput, fixture caseSuiteQualityFixture, outputDir string) {
	t.Helper()
	counts := report.QualityPlan.Counts
	if !report.OK || report.ProfileID != fixture.profileID || counts.Total != 4 || counts.DraftCase != 1 || counts.CompleteMetadata != 1 || counts.AddRunnable != 1 || counts.AddExecution != 1 {
		t.Fatalf("%s suite quality report = %#v", label, report)
	}
	if report.ReportURL != filepath.Join(outputDir, "report.html") || report.JSONReportURL != filepath.Join(outputDir, "report.json") {
		t.Fatalf("%s suite quality report paths = %#v", label, report)
	}
}

func requireCaseSuiteQualityReportFiles(t *testing.T, label string, outputDir string, fixture caseSuiteQualityFixture) {
	t.Helper()
	jsonReportRaw, err := os.ReadFile(filepath.Join(outputDir, "report.json"))
	if err != nil {
		t.Fatalf("read %s quality json report: %v", label, err)
	}
	htmlReportRaw, err := os.ReadFile(filepath.Join(outputDir, "report.html"))
	if err != nil {
		t.Fatalf("read %s quality html report: %v", label, err)
	}
	jsonReport := string(jsonReportRaw)
	htmlReport := string(htmlReportRaw)
	for _, want := range []string{"Case Suite Quality Report", fixture.suggestedEmptyCaseID, fixture.gapsCaseID, "complete-case-metadata", "add-execution-config"} {
		if !strings.Contains(htmlReport, want) {
			t.Fatalf("%s quality html missing %q:\n%s", label, want, htmlReport)
		}
	}
	if !strings.Contains(jsonReport, `"qualityPlan"`) || !strings.Contains(jsonReport, fixture.suggestedEmptyCaseID) {
		t.Fatalf("%s quality json report missing expected content:\n%s", label, jsonReport)
	}
}

func requireCaseSuiteQualityReportText(t *testing.T, label string, profileDir string) {
	t.Helper()
	outputDir := filepath.Join(t.TempDir(), "text-quality-report")
	textOut := runCLI(t, "case", "suite", "quality-report", "--profile", profileDir, "--status", "active", "--output-dir", outputDir)
	for _, want := range []string{"Case Suite Quality Report", "Total Actions: 4", "Report:"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("%s quality report text missing %q:\n%s", label, want, textOut)
		}
	}
}

type caseSuiteImpactReportOutput struct {
	OK     bool `json:"ok"`
	Impact struct {
		BatchRequest struct {
			RequestID string   `json:"requestId"`
			CaseIDs   []string `json:"caseIds"`
		} `json:"batchRequest"`
	} `json:"impact"`
	Report struct {
		OK        bool   `json:"ok"`
		ReportURL string `json:"reportUrl"`
		Counts    struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"counts"`
		Results []struct {
			CaseID    string `json:"caseId"`
			CaseRunID string `json:"caseRunId"`
			Status    string `json:"status"`
		} `json:"results"`
	} `json:"report"`
}

func newCaseSuiteImpactReportServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lookup" || r.URL.Query().Get("mode") != "ok" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func runCaseSuiteImpactReportJSON(t *testing.T, label string, fixture interfaceNodeBatchReportFixture, runLabel string, serverURL string, outputDir string) caseSuiteImpactReportOutput {
	t.Helper()
	out := runCLI(t,
		"case", "suite", "impact-report",
		"--profile", fixture.profileDir,
		"--signal", "/lookup",
		"--tag", "smoke",
		"--status", "active",
		"--action", "run",
		"--request-id", runLabel+"-change-003",
		"--base-url", serverURL,
		"--output-dir", outputDir,
		"--json",
	)
	var report caseSuiteImpactReportOutput
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s impact report json: %v\n%s", label, err, out)
	}
	return report
}

func requireCaseSuiteImpactReport(t *testing.T, label string, report caseSuiteImpactReportOutput, fixture interfaceNodeBatchReportFixture, runLabel string, outputDir string) {
	t.Helper()
	if !report.OK || report.Impact.BatchRequest.RequestID != runLabel+"-change-003" || strings.Join(report.Impact.BatchRequest.CaseIDs, ",") != fixture.defaultCaseID {
		t.Fatalf("%s impact report selection = %#v", label, report)
	}
	if !report.Report.OK || report.Report.Counts.Total != 1 || report.Report.Counts.Passed != 1 || report.Report.Counts.Failed != 0 || len(report.Report.Results) != 1 {
		t.Fatalf("%s impact execution report = %#v", label, report.Report)
	}
	result := report.Report.Results[0]
	if result.CaseID != fixture.defaultCaseID || result.CaseRunID == "" || result.Status != store.StatusPassed {
		t.Fatalf("%s impact execution item = %#v", label, result)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "report.html")); err != nil {
		t.Fatalf("%s impact report html missing: %v", label, err)
	}
}
