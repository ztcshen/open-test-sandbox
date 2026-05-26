package controlplane_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesInterfaceNodeRunHistoryFromStore(t *testing.T) {
	ctx := context.Background()
	s := openInterfaceNodeRunStore(t, ctx)
	defer s.Close()
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	seedInterfaceNodeRunHistory(t, ctx, s, started)
	saveInterfaceNodeAlphaTopology(t, ctx, s, started)

	server := httptest.NewServer(controlplane.NewWithStore(interfaceNodeRunHistoryBundle(), s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	requireInterfaceNodeHistorySummary(t, payload)
	requireInterfaceNodeLatestCaseRun(t, payload, 1, "run.beta", "case.beta", store.StatusFailed, 250)
	requireInterfaceNodeRuns(t, payload, 2, "run.beta")
}

func TestServerScopesInterfaceNodeRunsToWorkflowStepContext(t *testing.T) {
	ctx := context.Background()
	s := openInterfaceNodeRunStore(t, ctx)
	defer s.Close()
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	seedScopedInterfaceNodeRuns(t, ctx, s, started)
	saveInterfaceNodeAlphaTopology(t, ctx, s, started)

	server := httptest.NewServer(controlplane.NewWithStore(interfaceNodeScopedRunBundle(), s))
	defer server.Close()

	global := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	requireGlobalInterfaceNodeRun(t, global)

	scoped := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha&flowId=workflow.alpha&runId=run.alpha&stepId=step.alpha", http.StatusOK)
	requireScopedInterfaceNodeContext(t, scoped)
	requireScopedInterfaceNodeLatestRun(t, scoped)
	requireInterfaceNodeRuns(t, scoped, 1, "run.alpha")
}

func TestServerEvaluatesInterfaceNodeRunTimeoutFromCatalog(t *testing.T) {
	ctx := context.Background()
	s := openInterfaceNodeRunStore(t, ctx)
	defer s.Close()
	started := time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)
	seedInterfaceNodeTimeoutCatalog(t, ctx, s, started)
	recordInterfaceNodeRunPair(t, ctx, s, timeoutInterfaceNodeRun(started))

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	list := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	requireInterfaceNodeTimeoutList(t, list)

	detail := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	requireInterfaceNodeTimeoutDetail(t, detail)
}

func TestServerExposesInterfaceNodeRunsWithoutFullRunScan(t *testing.T) {
	runtime := interfaceNodeCaseRunCatalogStore{
		catalog: interfaceNodeRunCatalog(),
		records: []store.APICaseRunRecord{
			singleInterfaceNodeCaseRunRecord(time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)),
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, runtime))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	history := payload["history"].(map[string]any)
	if history["latestRunId"] != "run.alpha" || history["runCount"] != float64(1) {
		t.Fatalf("interface node history = %#v", history)
	}
	requireInterfaceNodeLatestCaseRun(t, payload, 0, "run.alpha", "", store.StatusPassed, 0)
}

type interfaceNodeRunPair struct {
	run     store.Run
	caseRun store.APICaseRun
}

func openInterfaceNodeRunStore(t *testing.T, ctx context.Context) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	return s
}

func seedInterfaceNodeRunHistory(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()
	for _, pair := range []interfaceNodeRunPair{
		{
			run:     interfaceNodeRun("run.alpha", "workflow.alpha", store.StatusPassed, ".runtime/evidence/run.alpha", "{}", started, time.Time{}),
			caseRun: interfaceNodeCaseRun("run.alpha.case", "run.alpha", "case.alpha", store.StatusPassed, `{"method":"GET","path":"/alpha"}`, `{"status":"passed"}`, started, 150),
		},
		{
			run:     interfaceNodeRun("run.beta", "workflow.alpha", store.StatusFailed, ".runtime/evidence/run.beta", "{}", started.Add(time.Minute), time.Time{}),
			caseRun: interfaceNodeCaseRun("run.beta.case", "run.beta", "case.beta", store.StatusFailed, `{"method":"POST","path":"/beta"}`, `{"status":"failed","errorCount":1}`, started.Add(time.Minute), 250),
		},
	} {
		recordInterfaceNodeRunPair(t, ctx, s, pair)
	}
}

func seedScopedInterfaceNodeRuns(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()
	for _, pair := range []interfaceNodeRunPair{
		{
			run: interfaceNodeRun("run.alpha", "workflow.alpha", store.StatusPassed, "", `{"steps":[
					{"stepId":"step.alpha","caseId":"case.alpha"},
					{"stepId":"step.beta","caseId":"case.beta"}
				]}`, started, time.Time{}),
			caseRun: interfaceNodeCaseRun("run.alpha.case", "run.alpha", "case.alpha", store.StatusPassed, `{"stepId":"step.alpha","method":"POST","path":"/alpha","requestId":"request.alpha"}`, `{"status":"passed"}`, started, 150),
		},
		{
			run:     interfaceNodeRun("run.beta", "case.alpha.standalone", store.StatusFailed, "", `{}`, started.Add(time.Minute), time.Time{}),
			caseRun: interfaceNodeCaseRun("run.beta.case", "run.beta", "case.alpha", store.StatusFailed, `{"method":"POST","path":"/alpha","requestId":"request.beta"}`, `{"status":"failed","errorCount":1}`, started.Add(time.Minute), 250),
		},
	} {
		recordInterfaceNodeRunPair(t, ctx, s, pair)
	}
}

func recordInterfaceNodeRunPair(t *testing.T, ctx context.Context, s store.Store, pair interfaceNodeRunPair) {
	t.Helper()
	if _, err := s.CreateRun(ctx, pair.run); err != nil {
		t.Fatalf("create run %s: %v", pair.run.ID, err)
	}
	if _, err := s.RecordAPICaseRun(ctx, pair.caseRun); err != nil {
		t.Fatalf("record case run %s: %v", pair.caseRun.ID, err)
	}
}

func interfaceNodeRun(id, workflowID, status, evidenceRoot, summary string, createdAt, updatedAt time.Time) store.Run {
	return store.Run{ID: id, ProfileID: "sample", WorkflowID: workflowID, Status: status, EvidenceRoot: evidenceRoot, SummaryJSON: summary, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func interfaceNodeCaseRun(id, runID, caseID, status, requestSummary, assertionSummary string, started time.Time, elapsedMs int) store.APICaseRun {
	return store.APICaseRun{
		ID: id, RunID: runID, CaseID: caseID, Status: status,
		RequestSummaryJSON: requestSummary, AssertionSummaryJSON: assertionSummary,
		StartedAt: started, FinishedAt: started.Add(time.Duration(elapsedMs) * time.Millisecond), CreatedAt: started,
	}
}

func saveInterfaceNodeAlphaTopology(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID: "topology.alpha", WorkflowRunID: "run.alpha", WorkflowID: "workflow.alpha", StepID: "step.alpha",
		CaseID: "case.alpha", RequestID: "request.alpha", TraceID: "trace.alpha", Status: "complete",
		TopologyJSON: `{"provider":"skywalking","status":"complete","requestId":"request.alpha","traceId":"trace.alpha","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology: "service.entry -> service.worker", CreatedAt: started.Add(time.Second),
	}); err != nil {
		t.Fatalf("save trace topology: %v", err)
	}
}

func interfaceNodeRunHistoryBundle() profile.Bundle {
	bundle := interfaceNodeScopedRunBundle()
	bundle.APICases = append(bundle.APICases, profile.APICase{ID: "case.beta", DisplayName: "Case Beta", NodeID: "node.alpha"})
	return bundle
}

func interfaceNodeScopedRunBundle() profile.Bundle {
	return profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
	}
}

func seedInterfaceNodeTimeoutCatalog(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: started,
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Method: "POST", Path: "/alpha", Status: "active", TimeoutMs: 100},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
}

func timeoutInterfaceNodeRun(started time.Time) interfaceNodeRunPair {
	return interfaceNodeRunPair{
		run:     interfaceNodeRun("run.alpha", "workflow.alpha", store.StatusPassed, "", "{}", started, started.Add(150*time.Millisecond)),
		caseRun: interfaceNodeCaseRun("run.alpha.case", "run.alpha", "case.alpha", store.StatusPassed, `{"method":"POST","path":"/alpha"}`, `{"status":"passed"}`, started, 150),
	}
}

func interfaceNodeRunCatalog() store.ProfileCatalog {
	return store.ProfileCatalog{
		ProfileID: "sample",
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Method: "POST", Path: "/alpha", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
		},
	}
}

func interfaceNodeRunCatalogWithDirectoryPresentation() store.ProfileCatalog {
	catalog := interfaceNodeRunCatalog()
	catalog.TemplateConfigs = []store.CatalogTemplateConfig{
		{
			ID:         "cfg.interface-directory.default",
			TemplateID: "TPL-INTERFACE-NODE-DIRECTORY-V1",
			ScopeType:  "interface-node-directory",
			ScopeID:    "_default",
			ConfigJSON: `{"copy":{"directoryTitle":"Configured interface directory","latestElapsedLabel":"Configured latest","totalElapsedLabel":"Configured total"}}`,
			Status:     "active",
		},
	}
	return catalog
}

func singleInterfaceNodeCaseRunRecord(started time.Time) store.APICaseRunRecord {
	return store.APICaseRunRecord{
		Run:     interfaceNodeRun("run.alpha", "workflow.alpha", store.StatusPassed, ".runtime/evidence/run.alpha", "", started, started.Add(time.Second)),
		CaseRun: interfaceNodeCaseRun("run.alpha.case", "run.alpha", "case.alpha", store.StatusPassed, `{"method":"POST","path":"/alpha"}`, `{"status":"passed"}`, started, 150),
	}
}

func requireInterfaceNodeHistorySummary(t *testing.T, payload map[string]any) {
	t.Helper()
	history := payload["history"].(map[string]any)
	if history["latestRunId"] != "run.beta" || history["runCount"] != float64(2) || history["passCount"] != float64(1) || history["failCount"] != float64(1) {
		t.Fatalf("interface node history = %#v", history)
	}
	if history["latestFailureReason"] != "assertion errors: 1" || history["totalElapsedMs"] != float64(400) {
		t.Fatalf("interface node history details = %#v", history)
	}
}

func requireInterfaceNodeLatestCaseRun(t *testing.T, payload map[string]any, index int, runID, caseID, status string, elapsedMs float64) {
	t.Helper()
	cases := payload["cases"].([]any)
	if len(cases) <= index {
		t.Fatalf("interface node cases = %#v", cases)
	}
	latest := cases[index].(map[string]any)["latestRun"].(map[string]any)
	if latest["runId"] != runID || latest["status"] != status {
		t.Fatalf("case latest run = %#v", latest)
	}
	if caseID != "" && latest["caseId"] != caseID {
		t.Fatalf("case latest run case id = %#v", latest)
	}
	if elapsedMs > 0 && latest["elapsedMs"] != elapsedMs {
		t.Fatalf("case latest run elapsed = %#v", latest)
	}
}

func requireInterfaceNodeRuns(t *testing.T, payload map[string]any, count int, firstRunID string) {
	t.Helper()
	runs := payload["runs"].([]any)
	if len(runs) != count || runs[0].(map[string]any)["runId"] != firstRunID {
		t.Fatalf("interface node runs = %#v", runs)
	}
}

func requireGlobalInterfaceNodeRun(t *testing.T, payload map[string]any) {
	t.Helper()
	globalCase := payload["cases"].([]any)[0].(map[string]any)
	if globalCase["latestRun"].(map[string]any)["runId"] != "run.alpha" {
		t.Fatalf("global interface node should prefer latest passing cache: %#v", globalCase)
	}
}

func requireScopedInterfaceNodeContext(t *testing.T, payload map[string]any) {
	t.Helper()
	context := payload["context"].(map[string]any)
	if context["flowId"] != "workflow.alpha" || context["workflowId"] != "workflow.alpha" || context["runId"] != "run.alpha" || context["stepId"] != "step.alpha" {
		t.Fatalf("interface node context = %#v", context)
	}
}

func requireScopedInterfaceNodeLatestRun(t *testing.T, payload map[string]any) {
	t.Helper()
	scopedCase := payload["cases"].([]any)[0].(map[string]any)
	latest := scopedCase["latestRun"].(map[string]any)
	if latest["runId"] != "run.alpha" || latest["caseRunId"] != "run.alpha.case" || latest["elapsedMs"] != float64(150) {
		t.Fatalf("scoped interface node latest run = %#v", latest)
	}
	requireScopedInterfaceNodeTopology(t, latest)
	requireScopedInterfaceNodeRequestSummary(t, latest)
}

func requireScopedInterfaceNodeTopology(t *testing.T, latest map[string]any) {
	t.Helper()
	topology := latest["topology"].(map[string]any)
	if topology["traceId"] != "trace.alpha" || topology["requestId"] != "request.alpha" || topology["status"] != "complete" || topology["provider"] != "skywalking" {
		t.Fatalf("scoped interface node topology = %#v", topology)
	}
}

func requireScopedInterfaceNodeRequestSummary(t *testing.T, latest map[string]any) {
	t.Helper()
	request := latest["requestSummary"].(map[string]any)
	if request["requestId"] != "request.alpha" || request["stepId"] != "step.alpha" {
		t.Fatalf("scoped request summary = %#v", request)
	}
}

func requireInterfaceNodeTimeoutList(t *testing.T, list map[string]any) {
	t.Helper()
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("interface node list = %#v", list)
	}
	node := items[0].(map[string]any)
	if node["admissionStatus"] != store.StatusFailed || node["latestElapsedMs"] != float64(150) || node["timeoutMs"] != float64(100) {
		t.Fatalf("interface node timeout state = %#v", node)
	}
}

func requireInterfaceNodeTimeoutDetail(t *testing.T, detail map[string]any) {
	t.Helper()
	cases := detail["cases"].([]any)
	latest := cases[0].(map[string]any)["latestRun"].(map[string]any)
	if latest["status"] != store.StatusFailed || latest["timeoutExceeded"] != true || latest["timeoutMs"] != float64(100) || latest["failureKind"] != "timeout" {
		t.Fatalf("interface node latest run timeout = %#v", latest)
	}
	if !strings.Contains(latest["failureReason"].(string), "exceeded timeout") {
		t.Fatalf("interface node timeout reason = %#v", latest)
	}
	admission := detail["admission"].(map[string]any)
	if admission["status"] != store.StatusFailed || admission["passedCaseCount"] != float64(0) {
		t.Fatalf("interface node admission timeout = %#v", admission)
	}
}
