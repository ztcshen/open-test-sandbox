package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type caseSuiteVariantReport struct {
	OK     bool `json:"ok"`
	Counts struct {
		Total          int `json:"total"`
		Passed         int `json:"passed"`
		DerivedConfigs int `json:"derivedConfigs"`
	} `json:"counts"`
	Results []struct {
		CaseID   string `json:"caseId"`
		Priority string `json:"priority"`
		Owner    string `json:"owner"`
		HTTPCode int    `json:"httpCode"`
	} `json:"results"`
}

type caseSuiteNamedActiveReport struct {
	OK     bool `json:"ok"`
	Counts struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
	} `json:"counts"`
	Results []struct {
		CaseID    string `json:"caseId"`
		CaseRunID string `json:"caseRunId"`
		DetailURL string `json:"detailUrl"`
	} `json:"results"`
}

type caseSuiteNamedActiveCoverage struct {
	OK     bool `json:"ok"`
	Counts struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
		NotRun int `json:"notRun"`
	} `json:"counts"`
}

type caseSuiteNamedActivePriority struct {
	OK      bool     `json:"ok"`
	CaseIDs []string `json:"caseIds"`
	Counts  struct {
		Selected int `json:"selected"`
		Blocked  int `json:"blocked"`
	} `json:"counts"`
	BatchRequest struct {
		RequestID string   `json:"requestId"`
		CaseIDs   []string `json:"caseIds"`
		BaseURL   string   `json:"baseUrl"`
	} `json:"batchRequest"`
}

type caseSuiteNamedActiveBrief struct {
	OK     bool `json:"ok"`
	Counts struct {
		Ready            int `json:"ready"`
		Blocked          int `json:"blocked"`
		PrioritySelected int `json:"prioritySelected"`
	} `json:"counts"`
	Recommended []struct {
		CaseID string `json:"caseId"`
	} `json:"recommended"`
}

func decodeCaseSuiteVariantReport(t *testing.T, label string, raw string) caseSuiteVariantReport {
	t.Helper()
	var report caseSuiteVariantReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("decode %s variant suite report json: %v\n%s", label, err, raw)
	}
	return report
}

func requireCaseSuiteVariantReport(t *testing.T, label string, report caseSuiteVariantReport, wantCaseID string) {
	t.Helper()
	if !report.OK || report.Counts.Total != 1 || report.Counts.Passed != 1 || report.Counts.DerivedConfigs != 1 {
		t.Fatalf("%s variant suite report = %#v", label, report)
	}
	if len(report.Results) != 1 || report.Results[0].CaseID != wantCaseID || report.Results[0].HTTPCode != http.StatusBadRequest {
		t.Fatalf("%s variant suite result = %#v", label, report.Results)
	}
}

func TestCaseSuiteReportRunsCasesByMaintenanceFilters(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-suite-report-pg")
	runCaseSuiteReportRunsCasesByMaintenanceFilters(t, storeRef, "PostgreSQL")
}

func TestCaseSuiteReportUsesNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-case-suite-report-mysql")
	runCaseSuiteReportRunsCasesByMaintenanceFilters(t, storeRef, "MySQL")
}

func runCaseSuiteReportRunsCasesByMaintenanceFilters(t *testing.T, _ string, label string) {
	t.Helper()
	serverURL := newCaseSuiteStatusServer(t)
	fixture := writeUniqueInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", fixture.profileDir)

	outputDir := filepath.Join(t.TempDir(), "suite-report")
	report := runCaseSuiteReportJSON(t, label, serverURL, outputDir)
	requireCaseSuiteReportJSON(t, label, report, fixture)
	requireCaseSuiteReportFiles(t, label, report, fixture, outputDir)
	requireCaseSuiteVariantReportRun(t, label, serverURL, fixture)
}

type caseSuiteReportCommandOutput struct {
	OK             bool   `json:"ok"`
	JUnitReportURL string `json:"junitReportUrl"`
	Filters        struct {
		Tags  []string `json:"tags"`
		Owner string   `json:"owner"`
	} `json:"filters"`
	Counts struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
	} `json:"counts"`
	Results []struct {
		CaseID    string   `json:"caseId"`
		Title     string   `json:"title"`
		NodeID    string   `json:"nodeId"`
		Tags      []string `json:"tags"`
		Priority  string   `json:"priority"`
		Owner     string   `json:"owner"`
		Status    string   `json:"status"`
		CaseRunID string   `json:"caseRunId"`
		DetailURL string   `json:"detailUrl"`
	} `json:"results"`
}

func runCaseSuiteReportJSON(t *testing.T, label string, serverURL string, outputDir string) caseSuiteReportCommandOutput {
	t.Helper()
	out := runCLI(t, "case", "suite", "report", "--tag", "smoke", "--owner", "team-a", "--base-url", serverURL, "--output-dir", outputDir, "--json")
	var report caseSuiteReportCommandOutput
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s suite report json: %v\n%s", label, err, out)
	}
	return report
}

func requireCaseSuiteReportJSON(t *testing.T, label string, report caseSuiteReportCommandOutput, fixture interfaceNodeBatchReportFixture) {
	t.Helper()
	if !report.OK || report.Counts.Total != 1 || report.Counts.Passed != 1 || report.Counts.Failed != 0 {
		t.Fatalf("%s suite report = %#v", label, report)
	}
	if strings.Join(report.Filters.Tags, ",") != "smoke" || report.Filters.Owner != "team-a" {
		t.Fatalf("%s suite filters = %#v", label, report.Filters)
	}
	if len(report.Results) != 1 {
		t.Fatalf("%s suite results = %#v", label, report.Results)
	}
	item := report.Results[0]
	if item.CaseID != fixture.defaultCaseID || item.NodeID != fixture.nodeAlphaID || item.Priority != "p0" || item.Owner != "team-a" || item.CaseRunID == "" || item.DetailURL == "" {
		t.Fatalf("%s suite result item = %#v", label, item)
	}
	if strings.Join(item.Tags, ",") != "smoke,regression" {
		t.Fatalf("%s suite result tags = %#v", label, item.Tags)
	}
}

func requireCaseSuiteReportFiles(t *testing.T, label string, report caseSuiteReportCommandOutput, fixture interfaceNodeBatchReportFixture, outputDir string) {
	t.Helper()
	html := readCaseSuiteReportFile(t, label, filepath.Join(outputDir, "report.html"), "suite html report")
	for _, want := range []string{"Case Suite Report", "Case Alpha Default", "team-a", "smoke", "p0", "caseRunId"} {
		if !strings.Contains(html, want) {
			t.Fatalf("%s suite html missing %q:\n%s", label, want, html)
		}
	}
	if strings.Contains(html, "Case Alpha Variant") {
		t.Fatalf("%s suite html should not include unselected case:\n%s", label, html)
	}
	junitPath := filepath.Join(outputDir, "report.junit.xml")
	junit := readCaseSuiteReportFile(t, label, junitPath, "suite junit report")
	if report.JUnitReportURL != junitPath {
		t.Fatalf("%s junit report url = %q want %q", label, report.JUnitReportURL, junitPath)
	}
	for _, want := range []string{`<testsuite name="Case Suite Report" tests="1" failures="0"`, `name="` + fixture.defaultCaseID + `"`, `classname="` + fixture.nodeAlphaID + `"`} {
		if !strings.Contains(junit, want) {
			t.Fatalf("%s suite junit missing %q:\n%s", label, want, junit)
		}
	}
}

func readCaseSuiteReportFile(t *testing.T, label string, path string, reportName string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s %s missing: %v", label, reportName, err)
	}
	return string(raw)
}

func requireCaseSuiteVariantReportRun(t *testing.T, label string, serverURL string, fixture interfaceNodeBatchReportFixture) {
	t.Helper()
	variantOut := runCLI(t,
		"case", "suite", "report",
		"--tag", "negative",
		"--base-url", serverURL,
		"--output-dir", filepath.Join(t.TempDir(), "variant-suite-report"),
		"--json",
	)
	variantReport := decodeCaseSuiteVariantReport(t, label, variantOut)
	requireCaseSuiteVariantReport(t, label, variantReport, fixture.variantCaseID)
}

func TestCaseSuiteCommandsUseNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-suite-pg")
	runCaseSuiteCommandsUseNamedActiveStore(t, "pg", "PostgreSQL")
}

func TestCaseSuiteCommandsUseNamedMySQLActiveStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "daily-suite-mysql")
	runCaseSuiteCommandsUseNamedActiveStore(t, "mysql", "MySQL")
}

func runCaseSuiteCommandsUseNamedActiveStore(t *testing.T, runLabel string, label string) {
	t.Helper()
	serverURL := newCaseSuiteStatusServer(t)
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	requireCaseSuiteNamedActiveReport(t, runLabel, label, serverURL)
	requireCaseSuiteNamedActiveVariant(t, runLabel, label, serverURL)
	requireCaseSuiteNamedActiveCoverage(t, label)
	requireCaseSuiteNamedActivePriority(t, runLabel, label, serverURL)
	requireCaseSuiteNamedActiveBrief(t, label, serverURL)
}

func requireCaseSuiteNamedActiveReport(t *testing.T, runLabel string, label string, serverURL string) {
	t.Helper()

	out := runCLI(t,
		"case", "suite", "report",
		"--tag", "smoke",
		"--owner", "team-a",
		"--base-url", serverURL,
		"--output-dir", filepath.Join(t.TempDir(), runLabel+"-suite-report"),
		"--json",
	)
	var report caseSuiteNamedActiveReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s suite report json: %v\n%s", label, err, out)
	}
	if !report.OK || report.Counts.Total != 1 || report.Counts.Passed != 1 || report.Counts.Failed != 0 || len(report.Results) != 1 {
		t.Fatalf("%s suite report = %#v", label, report)
	}
	if report.Results[0].CaseID != "case.alpha.default" || report.Results[0].CaseRunID == "" || report.Results[0].DetailURL == "" {
		t.Fatalf("%s suite report result = %#v", label, report.Results[0])
	}
}

func requireCaseSuiteNamedActiveVariant(t *testing.T, runLabel string, label string, serverURL string) {
	t.Helper()

	variantOut := runCLI(t,
		"case", "suite", "report",
		"--tag", "negative",
		"--base-url", serverURL,
		"--output-dir", filepath.Join(t.TempDir(), runLabel+"-variant-suite-report"),
		"--json",
	)
	variantReport := decodeCaseSuiteVariantReport(t, label, variantOut)
	requireCaseSuiteVariantReport(t, label, variantReport, "case.alpha.variant")
}

func requireCaseSuiteNamedActiveCoverage(t *testing.T, label string) {
	t.Helper()

	coverageOut := runCLI(t, "case", "suite", "coverage", "--status", "active", "--json")
	var coverage caseSuiteNamedActiveCoverage
	if err := json.Unmarshal([]byte(coverageOut), &coverage); err != nil {
		t.Fatalf("decode %s suite coverage json: %v\n%s", label, err, coverageOut)
	}
	if !coverage.OK || coverage.Counts.Total != 2 || coverage.Counts.Passed != 2 || coverage.Counts.Failed != 0 || coverage.Counts.NotRun != 0 {
		t.Fatalf("%s suite coverage = %#v", label, coverage)
	}
}

func requireCaseSuiteNamedActivePriority(t *testing.T, runLabel string, label string, serverURL string) {
	t.Helper()

	priorityOut := runCLI(t,
		"case", "suite", "priority",
		"--signal", "Alpha",
		"--limit", "2",
		"--request-id", runLabel+"-change-001",
		"--base-url", serverURL,
		"--json",
	)
	var priority caseSuiteNamedActivePriority
	if err := json.Unmarshal([]byte(priorityOut), &priority); err != nil {
		t.Fatalf("decode %s suite priority json: %v\n%s", label, err, priorityOut)
	}
	if !priority.OK || priority.Counts.Selected != 2 || priority.Counts.Blocked != 0 || priority.BatchRequest.RequestID != runLabel+"-change-001" || priority.BatchRequest.BaseURL != serverURL {
		t.Fatalf("%s suite priority = %#v", label, priority)
	}
	if strings.Join(priority.BatchRequest.CaseIDs, ",") != strings.Join(priority.CaseIDs, ",") || len(priority.CaseIDs) != 2 {
		t.Fatalf("%s suite priority case ids = %#v batch=%#v", label, priority.CaseIDs, priority.BatchRequest.CaseIDs)
	}
}

func requireCaseSuiteNamedActiveBrief(t *testing.T, label string, serverURL string) {
	t.Helper()

	briefOut := runCLI(t, "case", "suite", "brief", "--signal", "Alpha", "--limit", "2", "--base-url", serverURL, "--json")
	var brief caseSuiteNamedActiveBrief
	if err := json.Unmarshal([]byte(briefOut), &brief); err != nil {
		t.Fatalf("decode %s suite brief json: %v\n%s", label, err, briefOut)
	}
	if !brief.OK || brief.Counts.Ready != 2 || brief.Counts.Blocked != 0 || brief.Counts.PrioritySelected != 2 || len(brief.Recommended) != 2 {
		t.Fatalf("%s suite brief = %#v", label, brief)
	}
}
