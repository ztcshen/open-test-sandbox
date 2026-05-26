package controlplane_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesWorkflowRunContracts(t *testing.T) {
	ctx := context.Background()
	s := openWorkflowRoutesSQLiteStore(t, ctx)
	started := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	seedWorkflowRunContracts(t, ctx, s, started)

	server := newWorkflowRoutesStoreServer(t, sampleWorkflowRoutesProfile(), s)

	requireWorkflowRunListContract(t, server.URL)
	requireWorkflowRunDetailContract(t, server.URL)
	requireWorkflowRunStepContract(t, server.URL)
	requireLatestWorkflowStepContract(t, server.URL)
}

func TestServerCopiesStepTraceTopologyIntoSavedWorkflowRun(t *testing.T) {
	ctx := context.Background()
	s := openWorkflowRoutesSQLiteStore(t, ctx)
	started := time.Date(2026, 5, 18, 7, 0, 0, 0, time.UTC)
	recordSingleApplySourceRun(t, ctx, s, started)
	saveSingleApplyTraceTopology(t, ctx, s, started, "save source topology")
	recordSingleApplyTopologyTask(t, ctx, s, started)

	server := newWorkflowRoutesStoreServer(t, profile.Bundle{ID: "sample"}, s)
	postSingleApplyWorkflowRun(t, server.URL)

	latest := decodeJSONResponse(t, server.URL+"/api/workflow-runs/latest-step?workflowId=workflow.alpha&stepId=apply", http.StatusOK)
	topologies := latest["traceTopologies"].([]any)
	if len(topologies) != 1 {
		t.Fatalf("latest saved workflow step should include copied topology: %#v", latest)
	}
	topology := topologies[0].(map[string]any)
	if topology["runId"] == "run.single.apply" || topology["stepId"] != "apply" || topology["traceId"] != "trace.apply" {
		t.Fatalf("copied topology should belong to saved workflow run and keep step evidence: %#v", topology)
	}
	runID := topology["workflowRunId"].(string)
	tasks := decodeJSONResponse(t, server.URL+"/api/post-process-tasks?runId="+url.QueryEscape(runID)+"&stepId=apply&kind=trace_topology_collect", http.StatusOK)
	taskRows := tasks["tasks"].([]any)
	counts := tasks["counts"].(map[string]any)
	if len(taskRows) != 1 || counts["passed"] != float64(1) || taskRows[0].(map[string]any)["runId"] != runID {
		t.Fatalf("latest saved workflow run should include copied topology task: %#v", tasks)
	}
}

func TestServerBackfillsSavedWorkflowStepTopologyAfterAsyncCollection(t *testing.T) {
	ctx := context.Background()
	s := openWorkflowRoutesSQLiteStore(t, ctx)
	started := time.Date(2026, 5, 18, 7, 0, 0, 0, time.UTC)
	recordSingleApplySourceRun(t, ctx, s, started)

	server := newWorkflowRoutesStoreServer(t, profile.Bundle{ID: "sample"}, s)
	postSingleApplyWorkflowRun(t, server.URL)
	saveSingleApplyTraceTopology(t, ctx, s, started.Add(time.Second), "save late source topology")

	latest := decodeJSONResponse(t, server.URL+"/api/workflow-runs/latest-step?workflowId=workflow.alpha&stepId=apply", http.StatusOK)
	topologies := latest["traceTopologies"].([]any)
	if len(topologies) != 1 || topologies[0].(map[string]any)["traceId"] != "trace.apply" {
		t.Fatalf("latest saved workflow step should backfill late topology: %#v", latest)
	}
}

func TestServerEvaluatesWorkflowStepTimeoutFromCatalog(t *testing.T) {
	ctx := context.Background()
	s := openWorkflowRoutesSQLiteStore(t, ctx)
	started := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	replaceWorkflowTimeoutCatalog(t, ctx, s, started)
	recordTimedWorkflowRouteRun(t, ctx, s, started)

	server := newWorkflowRoutesStoreServer(t, sampleWorkflowRoutesProfile(), s)

	detail := decodeJSONResponse(t, server.URL+"/api/workflow-runs/run.alpha", http.StatusOK)
	summary := detail["summary"].(map[string]any)
	steps := summary["steps"].([]any)
	step := steps[0].(map[string]any)
	if step["status"] != store.StatusFailed || step["stepOk"] != false || step["timeoutExceeded"] != true || step["timeoutMs"] != float64(100) {
		t.Fatalf("workflow run detail step timeout = %#v", step)
	}
	if summary["status"] != store.StatusFailed || summary["ok"] != false {
		t.Fatalf("workflow run summary timeout status = %#v", summary)
	}

	stepPayload := decodeJSONResponse(t, server.URL+"/api/workflow-runs/step?runId=run.alpha&stepId=step.alpha", http.StatusOK)
	stepSummary := stepPayload["summary"].(map[string]any)
	scopedStep := stepSummary["steps"].([]any)[0].(map[string]any)
	if scopedStep["status"] != store.StatusFailed || scopedStep["timeoutExceeded"] != true || !strings.Contains(scopedStep["failureReason"].(string), "exceeded timeout") {
		t.Fatalf("workflow run step timeout = %#v", scopedStep)
	}
}

func TestServerSavesWorkflowRunToStore(t *testing.T) {
	ctx := context.Background()
	s := openWorkflowRoutesSQLiteStore(t, ctx)
	server := newWorkflowRoutesStoreServer(t, sampleWorkflowRoutesProfile(), s)

	resp := postWorkflowRouteRunOK(t, server.URL, `{
		"workflowId":"workflow.alpha",
		"status":"passed",
		"ok":true,
		"steps":[{"stepId":"step.alpha","caseId":"case.alpha","ok":true,"summary":{"requestId":"request.alpha","httpCode":200}}],
		"summary":{"expectedStepCount":1,"stepCount":1}
	}`, "save workflow run status")
	defer resp.Body.Close()

	var saved struct {
		OK            bool   `json:"ok"`
		WorkflowRunID string `json:"workflowRunId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		t.Fatalf("decode saved workflow run: %v", err)
	}
	if !saved.OK || saved.WorkflowRunID == "" {
		t.Fatalf("saved workflow run = %#v", saved)
	}

	loaded := decodeJSONResponse(t, server.URL+"/api/workflow-runs/"+saved.WorkflowRunID, http.StatusOK)
	run := loaded["run"].(map[string]any)
	if run["workflowId"] != "workflow.alpha" || run["status"] != "passed" {
		t.Fatalf("loaded saved workflow run = %#v", loaded)
	}
	if run["evidenceRoot"] != "" {
		t.Fatalf("empty evidence root should stay empty: %#v", run)
	}

	caseRuns, err := s.ListAPICaseRuns(ctx, saved.WorkflowRunID)
	if err != nil {
		t.Fatalf("list case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != "case.alpha" || caseRuns[0].Status != store.StatusPassed {
		t.Fatalf("saved workflow case runs = %#v", caseRuns)
	}
	evidence := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId="+saved.WorkflowRunID, http.StatusOK)
	evidenceBody := evidence["evidence"].(map[string]any)
	summary := evidenceBody["summary"].(map[string]any)
	if summary["case_id"] != "case.alpha" || summary["status"] != store.StatusPassed {
		t.Fatalf("saved workflow evidence = %#v", evidence)
	}
}

func seedWorkflowRunContracts(t *testing.T, ctx context.Context, s *sqlite.Store, started time.Time) {
	t.Helper()

	recordWorkflowRouteRun(t, ctx, s, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  `{"summary":{"expectedStepCount":2,"stepCount":2},"steps":[{"stepId":"step.alpha","ok":true},{"stepId":"step.beta","ok":false,"summary":{"httpCode":200},"result":{"response":{"statusCode":200}}}]}`,
		CreatedAt:    started,
	})
	recordWorkflowRouteRun(t, ctx, s, store.Run{
		ID:          "run.beta",
		ProfileID:   "sample",
		WorkflowID:  "workflow.alpha",
		Status:      store.StatusPassed,
		SummaryJSON: `{"kind":"apiCase","summary":{"httpCode":200},"steps":[{"stepId":"step.beta","caseId":"case.beta","ok":true,"summary":{"httpCode":200},"result":{"response":{"statusCode":200,"body":"{}"}}}]}`,
		CreatedAt:   started.Add(time.Minute),
	})
	saveWorkflowRouteTraceTopology(t, ctx, s)
	recordWorkflowRouteRuntimeLogs(t, ctx, s, started)
}

func requireWorkflowRunListContract(t *testing.T, serverURL string) {
	t.Helper()

	list := decodeJSONResponse(t, serverURL+"/api/runs", http.StatusOK)
	workflowRuns := list["workflowRuns"].([]any)
	if len(workflowRuns) != 2 || workflowRuns[0].(map[string]any)["id"] != "run.beta" || workflowRuns[1].(map[string]any)["id"] != "run.alpha" {
		t.Fatalf("workflow run list = %#v", list)
	}
	if list["ok"] != true {
		t.Fatalf("workflow run list should expose ok envelope: %#v", list)
	}
	firstRun := workflowRuns[1].(map[string]any)
	if firstRun["summaryJson"] == "" || firstRun["stepCount"] != float64(2) {
		t.Fatalf("workflow run list summary fields = %#v", firstRun)
	}
}

func requireWorkflowRunDetailContract(t *testing.T, serverURL string) {
	t.Helper()

	detail := decodeJSONResponse(t, serverURL+"/api/workflow-runs/run.alpha", http.StatusOK)
	if detail["ok"] != true {
		t.Fatalf("workflow run detail failed: %#v", detail)
	}
	traceTopologies := detail["traceTopologies"].([]any)
	if len(traceTopologies) != 1 {
		t.Fatalf("workflow run detail should include topology array: %#v", detail)
	}
	if traceTopologies[0].(map[string]any)["traceId"] != "trace.beta" {
		t.Fatalf("workflow run topology row = %#v", traceTopologies[0])
	}
	summary := detail["summary"].(map[string]any)
	if len(summary["steps"].([]any)) != 2 {
		t.Fatalf("workflow run detail summary = %#v", summary)
	}
}

func requireWorkflowRunStepContract(t *testing.T, serverURL string) {
	t.Helper()

	step := decodeJSONResponse(t, serverURL+"/api/workflow-runs/step?runId=run.alpha&stepId=step.beta", http.StatusOK)
	stepSummary := step["summary"].(map[string]any)
	steps := stepSummary["steps"].([]any)
	if len(steps) != 1 || steps[0].(map[string]any)["stepId"] != "step.beta" {
		t.Fatalf("workflow run step payload = %#v", step)
	}
	if strings.Contains(mustJSON(t, step), "step.alpha") {
		t.Fatalf("workflow run step leaked other steps: %#v", step)
	}
	stepTopologies := step["traceTopologies"].([]any)
	if len(stepTopologies) != 1 || stepTopologies[0].(map[string]any)["stepId"] != "step.beta" {
		t.Fatalf("workflow run step topology payload = %#v", step)
	}
}

func requireLatestWorkflowStepContract(t *testing.T, serverURL string) {
	t.Helper()

	latest := decodeJSONResponse(t, serverURL+"/api/workflow-runs/latest-step?workflowId=workflow.alpha&stepId=step.beta", http.StatusOK)
	latestRun := latest["run"].(map[string]any)
	if latestRun["id"] != "run.alpha" {
		t.Fatalf("latest workflow step should prefer full workflow cache over newer single-step runs: %#v", latest)
	}
	latestStep := latest["summary"].(map[string]any)["steps"].([]any)[0].(map[string]any)
	trace := latestStep["trace"].(map[string]any)
	systems := trace["systems"].([]any)
	if len(systems) != 1 || systems[0].(map[string]any)["name"] != "worker" {
		t.Fatalf("latest workflow step should use cached runtime logs: %#v", latest)
	}
}

func recordSingleApplySourceRun(t *testing.T, ctx context.Context, s *sqlite.Store, started time.Time) {
	t.Helper()

	recordWorkflowRouteRun(t, ctx, s, store.Run{
		ID:          "run.single.apply",
		ProfileID:   "sample",
		WorkflowID:  "workflow.alpha",
		Status:      store.StatusPassed,
		SummaryJSON: `{"kind":"apiCase","caseId":"case.apply","stepId":"apply","ok":true}`,
		CreatedAt:   started,
		UpdatedAt:   started,
	})
}

func saveSingleApplyTraceTopology(t *testing.T, ctx context.Context, s *sqlite.Store, createdAt time.Time, failureLabel string) {
	t.Helper()

	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.single.apply",
		WorkflowRunID: "run.single.apply",
		WorkflowID:    "workflow.alpha",
		StepID:        "apply",
		CaseID:        "case.apply",
		RequestID:     "request.apply",
		TraceID:       "trace.apply",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"entry-service","target":"worker-service"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["entry-service","worker-service"]}`,
		TextTopology:  "entry-service -> worker-service",
		CreatedAt:     createdAt,
	}); err != nil {
		t.Fatalf("%s: %v", failureLabel, err)
	}
}

func recordSingleApplyTopologyTask(t *testing.T, ctx context.Context, s *sqlite.Store, started time.Time) {
	t.Helper()

	if _, err := s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:          "task.single.apply.topology",
		RunID:       "run.single.apply",
		WorkflowID:  "workflow.alpha",
		StepID:      "apply",
		CaseID:      "case.apply",
		Kind:        "trace_topology_collect",
		Status:      store.StatusPassed,
		StartedAt:   started,
		FinishedAt:  started.Add(100 * time.Millisecond),
		DurationMs:  100,
		SummaryJSON: `{"traceId":"trace.apply","topologyStatus":"complete"}`,
		CreatedAt:   started,
	}); err != nil {
		t.Fatalf("record source post-process task: %v", err)
	}
}

func postSingleApplyWorkflowRun(t *testing.T, serverURL string) {
	t.Helper()

	resp := postWorkflowRouteRunOK(t, serverURL, singleApplyWorkflowRunRequest, "workflow run status")
	resp.Body.Close()
}

func postWorkflowRouteRunOK(t *testing.T, serverURL string, body string, statusLabel string) *http.Response {
	t.Helper()

	resp, err := http.Post(serverURL+"/api/workflow-runs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post workflow run: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("%s = %d body=%s", statusLabel, resp.StatusCode, raw)
	}
	return resp
}

const singleApplyWorkflowRunRequest = `{
	"workflowId":"workflow.alpha",
	"ok":true,
	"steps":[
		{"stepId":"apply","caseId":"case.apply","runId":"run.single.apply","caseRunId":"run.single.apply.case","ok":true,"status":"passed"}
	]
}`

func replaceWorkflowTimeoutCatalog(t *testing.T, ctx context.Context, s *sqlite.Store, started time.Time) {
	t.Helper()

	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: started,
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha", BaseStepTimeoutMs: 500, TimeoutOffsetMs: 0},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Method: "POST", Path: "/alpha", Status: "active", TimeoutMs: 100},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true, SortOrder: 1},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
}

func recordTimedWorkflowRouteRun(t *testing.T, ctx context.Context, s *sqlite.Store, started time.Time) {
	t.Helper()

	recordWorkflowRouteRun(t, ctx, s, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		SummaryJSON: `{
			"status":"passed",
			"summary":{"expectedStepCount":1,"stepCount":1,"passed":1,"elapsedMs":150},
			"steps":[{"stepId":"step.alpha","caseId":"case.alpha","ok":true,"stepOk":true,"status":"passed","elapsedMs":150}]
		}`,
		CreatedAt: started,
		UpdatedAt: started.Add(150 * time.Millisecond),
	})
}
