package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaseDiscoverFiltersByMaintenanceMetadata(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-discover-pg")
	runCaseDiscoverFiltersByMaintenanceMetadata(t, storeRef, "PostgreSQL")
}

func TestCaseDiscoverUsesNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-case-discover-mysql")
	runCaseDiscoverFiltersByMaintenanceMetadata(t, storeRef, "MySQL")
}

func runCaseDiscoverFiltersByMaintenanceMetadata(t *testing.T, _ string, label string) {
	t.Helper()
	fixture := writeUniqueInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", fixture.profileDir)

	out := runCLI(t,
		"case", "discover",
		"--tag", "smoke",
		"--status", "active",
		"--owner", "team-a",
		"--json",
	)

	var report struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
		Items []struct {
			ID          string   `json:"id"`
			DisplayName string   `json:"displayName"`
			NodeID      string   `json:"nodeId"`
			Tags        []string `json:"tags"`
			Priority    string   `json:"priority"`
			Owner       string   `json:"owner"`
			Description string   `json:"description"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s case discover json: %v\n%s", label, err, out)
	}
	if !report.OK || report.Count != 1 || len(report.Items) != 1 {
		t.Fatalf("%s case discover report = %#v", label, report)
	}
	item := report.Items[0]
	if item.ID != fixture.defaultCaseID || item.NodeID != fixture.nodeAlphaID || item.Priority != "p0" || item.Owner != "team-a" {
		t.Fatalf("%s case discover item = %#v", label, item)
	}
	if strings.Join(item.Tags, ",") != "smoke,regression" || item.Description == "" {
		t.Fatalf("%s case discover metadata = %#v", label, item)
	}

	filtered := runCLI(t, "case", "discover", "--filter", "variant", "--json")
	var filteredReport struct {
		Items []struct {
			ID    string   `json:"id"`
			Tags  []string `json:"tags"`
			Owner string   `json:"owner"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(filtered), &filteredReport); err != nil {
		t.Fatalf("decode %s filtered case discover json: %v\n%s", label, err, filtered)
	}
	if len(filteredReport.Items) != 1 || filteredReport.Items[0].ID != fixture.variantCaseID || filteredReport.Items[0].Owner != "team-b" {
		t.Fatalf("%s filtered case discover = %#v", label, filteredReport.Items)
	}
}

func TestCaseDiscoverRequiresStoreUnlessOfflineTemplatePackage(t *testing.T) {
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	env := []string{"AGENT_TESTBENCH_CONFIG_HOME=" + t.TempDir()}

	missingStore := runCLIFailsWithEnv(t, env, "case", "discover", "--profile", profileDir, "--json")
	if !strings.Contains(missingStore, "--offline-template-package") || !strings.Contains(missingStore, "--store") {
		t.Fatalf("case discover package-only output = %q", missingStore)
	}

	out := runCLIWithEnv(t, env, "case", "discover", "--profile", profileDir, "--offline-template-package", "--filter", "variant", "--json")
	var report struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode offline case discover json: %v\n%s", err, out)
	}
	if len(report.Items) != 1 || report.Items[0].ID != "case.alpha.variant" {
		t.Fatalf("offline case discover = %#v", report.Items)
	}
}

func TestDiscoverCommandsAcceptStoreFlagAsLocationAgnosticStoreSelector(t *testing.T) {
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store", "sqlite://"+storePath)
	storeRef := "sqlite://" + storePath

	caseOut := runCLI(t, "case", "discover", "--store", storeRef, "--filter", "variant", "--json")
	var caseReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(caseOut), &caseReport); err != nil {
		t.Fatalf("decode case discover json: %v\n%s", err, caseOut)
	}
	if len(caseReport.Items) != 1 || caseReport.Items[0].ID != "case.alpha.variant" {
		t.Fatalf("case discover via --store = %#v", caseReport.Items)
	}

	nodeOut := runCLI(t, "interface-node", "discover", "--store", storeRef, "--filter", "Result Lookup", "--json")
	var nodeReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(nodeOut), &nodeReport); err != nil {
		t.Fatalf("decode interface-node discover json: %v\n%s", err, nodeOut)
	}
	if len(nodeReport.Items) != 1 || nodeReport.Items[0].ID != "node.alpha" {
		t.Fatalf("interface-node discover via --store = %#v", nodeReport.Items)
	}

	workflowProfileDir := writeWorkflowBatchReportProfile(t)
	workflowStorePath := filepath.Join(t.TempDir(), "workflow-store.sqlite")
	runCLI(t, "config", "publish", "--from", workflowProfileDir, "--store", "sqlite://"+workflowStorePath)
	workflowOut := runCLI(t, "workflow", "discover", "--store", "sqlite://"+workflowStorePath, "--filter", "Workflow Alpha", "--json")
	var workflowReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(workflowOut), &workflowReport); err != nil {
		t.Fatalf("decode workflow discover json: %v\n%s", err, workflowOut)
	}
	if len(workflowReport.Items) != 1 || workflowReport.Items[0].ID != "workflow.alpha" {
		t.Fatalf("workflow discover via --store = %#v", workflowReport.Items)
	}
}

func TestDiscoverCommandsUseNamedSQLiteActiveStore(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "case discover", args: []string{"case", "discover", "--json"}},
		{name: "interface-node discover", args: []string{"interface-node", "discover", "--json"}},
		{name: "workflow discover", args: []string{"workflow", "discover", "--json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configureNamedSQLiteActiveStore(t, "daily-discover-sqlite-"+strings.ReplaceAll(tt.name, " ", "-"))
			out := runCLI(t, tt.args...)
			if !strings.Contains(out, `"ok": true`) {
				t.Fatalf("%s SQLite output = %q", tt.name, out)
			}
		})
	}
}

func TestDiscoverCommandsUseNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-pg")
	runDiscoverCommandsUseNamedActiveStore(t, "PostgreSQL")
}

func TestDiscoverCommandsUseNamedMySQLActiveStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "daily-mysql")
	runDiscoverCommandsUseNamedActiveStore(t, "MySQL")
}

func runDiscoverCommandsUseNamedActiveStore(t *testing.T, label string) {
	t.Helper()
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	caseOut := runCLI(t, "case", "discover", "--filter", "variant", "--json")
	var caseReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(caseOut), &caseReport); err != nil {
		t.Fatalf("decode %s case discover json: %v\n%s", label, err, caseOut)
	}
	if len(caseReport.Items) != 1 || caseReport.Items[0].ID != "case.alpha.variant" {
		t.Fatalf("%s case discover via active SQL Store = %#v", label, caseReport.Items)
	}

	nodeOut := runCLI(t, "interface-node", "discover", "--filter", "Result Lookup", "--json")
	var nodeReport struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(nodeOut), &nodeReport); err != nil {
		t.Fatalf("decode %s interface-node discover json: %v\n%s", label, err, nodeOut)
	}
	if len(nodeReport.Items) != 1 || nodeReport.Items[0].ID != "node.alpha" {
		t.Fatalf("%s interface-node discover via active SQL Store = %#v", label, nodeReport.Items)
	}
}

func TestDailyWorkflowCommandsUseNamedPostgreSQLActiveStore(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-workflow-pg")
	runDailyWorkflowCommandsUseNamedActiveStore(t, "pg", "PostgreSQL")
}

func TestDailyWorkflowCommandsUseNamedMySQLActiveStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "daily-workflow-mysql")
	runDailyWorkflowCommandsUseNamedActiveStore(t, "mysql", "MySQL")
}

func runDailyWorkflowCommandsUseNamedActiveStore(t *testing.T, runLabel string, label string) {
	t.Helper()
	traceID := "trace." + runLabel + ".daily"
	requestID := "request." + runLabel + ".daily"
	server := newDailyWorkflowTargetServer()
	defer server.Close()
	provider := newDailyWorkflowTraceProvider(t, traceID)
	defer provider.Close()

	profileDir := writeWorkflowBatchReportProfile(t)
	runCLI(t, "config", "publish", "--from", profileDir)

	requireDailyWorkflowDiscover(t, label)
	requireDailyWorkflowPlan(t, label)
	requireDailyWorkflowBaseline(t, label)
	report := requireDailyWorkflowReport(t, label, server.URL)
	requireDailyWorkflowCaseRuns(t, label, report.RunID)
	requireDailyWorkflowTraceTopology(t, label, report.RunID, requestID, traceID, provider.URL)
	requireDailyWorkflowEvidence(t, label, report.RunID, traceID)
}

func newDailyWorkflowTargetServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/first":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"item_id":"item-001"}`)
		case "/second":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"ok"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newDailyWorkflowTraceProvider(t *testing.T, traceID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(payload.Query, "queryTrace") {
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
		_, _ = fmt.Fprintf(w, `{"data":{"queryTrace":{"spans":[{"traceId":%q,"segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/first","type":"Entry","component":"Tomcat"},{"traceId":%q,"segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":%q,"parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"GET:/first","type":"Entry","component":"Server"}]}}}`, traceID, traceID, traceID)
	}))
}

func requireDailyWorkflowDiscover(t *testing.T, label string) {
	t.Helper()
	workflowOut := runCLI(t, "workflow", "discover", "--filter", "Workflow Alpha", "--json")
	var workflowList struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(workflowOut), &workflowList); err != nil {
		t.Fatalf("decode %s workflow discover json: %v\n%s", label, err, workflowOut)
	}
	if len(workflowList.Items) != 1 || workflowList.Items[0].ID != "workflow.alpha" {
		t.Fatalf("%s workflow discover via active SQL Store = %#v", label, workflowList.Items)
	}
}

func requireDailyWorkflowPlan(t *testing.T, label string) {
	t.Helper()
	planOut := runCLI(t, "workflow", "plan", "--workflow", "workflow.alpha", "--json")
	var plan struct {
		Counts struct {
			Steps int `json:"steps"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(planOut), &plan); err != nil {
		t.Fatalf("decode %s workflow plan json: %v\n%s", label, err, planOut)
	}
	if plan.Counts.Steps != 2 {
		t.Fatalf("%s workflow plan via active SQL Store = %#v", label, plan)
	}
}

func requireDailyWorkflowBaseline(t *testing.T, label string) {
	t.Helper()
	runCLI(t, "baseline", "set", "--profile", "sample", "--subject", "workflow.alpha", "--status", "passed", "--required")
	baselineOut := runCLI(t, "baseline", "get", "--profile", "sample", "--subject", "workflow.alpha")
	if !strings.Contains(baselineOut, "Status: passed") || !strings.Contains(baselineOut, "Required: true") {
		t.Fatalf("%s baseline get via active SQL Store = %q", label, baselineOut)
	}
}

type dailyWorkflowReportForTest struct {
	OK     bool   `json:"ok"`
	RunID  string `json:"runId"`
	Counts struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
	} `json:"counts"`
}

func requireDailyWorkflowReport(t *testing.T, label string, serverURL string) dailyWorkflowReportForTest {
	t.Helper()
	reportOut := runCLI(t,
		"workflow", "report",
		"--workflow", "workflow.alpha",
		"--base-url", serverURL,
		"--output-dir", filepath.Join(t.TempDir(), "workflow-report"),
		"--json",
	)
	var report dailyWorkflowReportForTest
	if err := json.Unmarshal([]byte(reportOut), &report); err != nil {
		t.Fatalf("decode %s workflow report json: %v\n%s", label, err, reportOut)
	}
	if !report.OK || report.RunID == "" || report.Counts.Total != 2 || report.Counts.Passed != 2 || report.Counts.Failed != 0 {
		t.Fatalf("%s workflow report via active SQL Store = %#v", label, report)
	}
	return report
}

func requireDailyWorkflowCaseRuns(t *testing.T, label string, runID string) {
	t.Helper()
	caseRunsOut := runCLI(t, "case", "runs", "--run", runID, "--json")
	if !strings.Contains(caseRunsOut, "case.first") || !strings.Contains(caseRunsOut, "case.second") {
		t.Fatalf("%s case runs via active SQL Store = %s", label, caseRunsOut)
	}
}

func requireDailyWorkflowTraceTopology(t *testing.T, label string, runID string, requestID string, traceID string, providerURL string) {
	t.Helper()
	traceOut := runCLI(t, "trace", "topology", "collect",
		"--trace-graphql-url", providerURL,
		"--run", runID,
		"--step", "first",
		"--case", "case.first",
		"--request", requestID,
		"--trace-id", traceID,
		"--json",
	)
	if !strings.Contains(traceOut, `"provider":"skywalking"`) && !strings.Contains(traceOut, `"provider": "skywalking"`) || !strings.Contains(traceOut, `"status":"complete"`) && !strings.Contains(traceOut, `"status": "complete"`) || !strings.Contains(traceOut, traceID) {
		t.Fatalf("%s trace topology via active SQL Store = %s", label, traceOut)
	}
}

func requireDailyWorkflowEvidence(t *testing.T, label string, runID string, traceID string) {
	t.Helper()
	evidenceOut := runCLI(t, "case", "evidence", "--run", runID, "--case-id", "case.first", "--step-id", "first", "--json")
	if !strings.Contains(evidenceOut, `"provider":"skywalking"`) && !strings.Contains(evidenceOut, `"provider": "skywalking"`) || !strings.Contains(evidenceOut, traceID) {
		t.Fatalf("%s case evidence via active SQL Store = %s", label, evidenceOut)
	}
}

func TestInterfaceNodeDiscoverRequiresStoreUnlessOfflineTemplatePackage(t *testing.T) {
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	env := []string{"AGENT_TESTBENCH_CONFIG_HOME=" + t.TempDir()}

	missingStore := runCLIFailsWithEnv(t, env, "interface-node", "discover", "--profile", profileDir, "--json")
	if !strings.Contains(missingStore, "--offline-template-package") || !strings.Contains(missingStore, "--store") {
		t.Fatalf("interface-node discover package-only output = %q", missingStore)
	}

	out := runCLIWithEnv(t, env, "interface-node", "discover", "--profile", profileDir, "--offline-template-package", "--filter", "Result Lookup", "--json")
	var report struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode offline interface-node discover json: %v\n%s", err, out)
	}
	if len(report.Items) != 1 || report.Items[0].ID != "node.alpha" {
		t.Fatalf("offline interface-node discover = %#v", report.Items)
	}
}

func TestWorkflowDiscoverRequiresStoreUnlessOfflineTemplatePackage(t *testing.T) {
	profileDir := writeWorkflowBatchReportProfile(t)
	env := []string{"AGENT_TESTBENCH_CONFIG_HOME=" + t.TempDir()}

	missingStore := runCLIFailsWithEnv(t, env, "workflow", "discover", "--profile", profileDir, "--json")
	if !strings.Contains(missingStore, "--offline-template-package") || !strings.Contains(missingStore, "--store") {
		t.Fatalf("workflow discover package-only output = %q", missingStore)
	}

	out := runCLIWithEnv(t, env, "workflow", "discover", "--profile", profileDir, "--offline-template-package", "--filter", "Workflow Alpha", "--json")
	var report struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode offline workflow discover json: %v\n%s", err, out)
	}
	if len(report.Items) != 1 || report.Items[0].ID != "workflow.alpha" {
		t.Fatalf("offline workflow discover = %#v", report.Items)
	}
}
