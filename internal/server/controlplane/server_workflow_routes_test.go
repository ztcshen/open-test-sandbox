package controlplane_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesEmptyRunListsForReactShell(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/runs")
	if err != nil {
		t.Fatalf("get runs api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("runs status = %d", resp.StatusCode)
	}

	var payload struct {
		OK           bool             `json:"ok"`
		WorkflowRuns []map[string]any `json:"workflowRuns"`
		ReplayRuns   []map[string]any `json:"replayRuns"`
		ProbeRuns    []map[string]any `json:"probeRuns"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode runs api: %v", err)
	}
	if !payload.OK {
		t.Fatalf("runs should expose ok envelope: %#v", payload)
	}
	if payload.WorkflowRuns == nil || payload.ReplayRuns == nil || payload.ProbeRuns == nil {
		t.Fatalf("runs should encode empty arrays: %#v", payload)
	}
}

func TestServerExposesWorkflowRunContracts(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  `{"summary":{"expectedStepCount":2,"stepCount":2},"steps":[{"stepId":"step.alpha","ok":true},{"stepId":"step.beta","ok":false,"summary":{"httpCode":200},"result":{"response":{"statusCode":200}}}]}`,
		CreatedAt:    started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:          "run.beta",
		ProfileID:   "sample",
		WorkflowID:  "workflow.alpha",
		Status:      store.StatusPassed,
		SummaryJSON: `{"kind":"apiCase","summary":{"httpCode":200},"steps":[{"stepId":"step.beta","caseId":"case.beta","ok":true,"summary":{"httpCode":200},"result":{"response":{"statusCode":200,"body":"{}"}}}]}`,
		CreatedAt:   started.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create incomplete run: %v", err)
	}
	_, err = s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.alpha",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.beta",
		CaseID:        "case.beta",
		RequestID:     "request.beta",
		TraceID:       "trace.beta",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"service.alpha","target":"service.beta"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.alpha","service.beta"]}`,
		TextTopology:  "service.alpha -> service.beta",
	})
	if err != nil {
		t.Fatalf("save topology: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "runtime-logs.json")
	if err := os.WriteFile(logPath, []byte(`{"systems":[{"name":"worker","found":true,"coreLogs":["request.beta handled"]}]}`), 0o644); err != nil {
		t.Fatalf("write runtime logs: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "runtime.logs.step.beta",
		RunID:     "run.alpha",
		CaseRunID: "step.beta",
		Kind:      "runtime_logs",
		URI:       logPath,
		MediaType: "application/json",
		Summary:   `{"stepId":"step.beta"}`,
		CreatedAt: started,
	})
	if err != nil {
		t.Fatalf("record runtime logs: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	list := decodeJSONResponse(t, server.URL+"/api/runs", http.StatusOK)
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

	detail := decodeJSONResponse(t, server.URL+"/api/workflow-runs/run.alpha", http.StatusOK)
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

	step := decodeJSONResponse(t, server.URL+"/api/workflow-runs/step?runId=run.alpha&stepId=step.beta", http.StatusOK)
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

	latest := decodeJSONResponse(t, server.URL+"/api/workflow-runs/latest-step?workflowId=workflow.alpha&stepId=step.beta", http.StatusOK)
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

func TestServerCopiesStepTraceTopologyIntoSavedWorkflowRun(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 18, 7, 0, 0, 0, time.UTC)
	_, err = s.CreateRun(ctx, store.Run{
		ID:          "run.single.apply",
		ProfileID:   "sample",
		WorkflowID:  "workflow.alpha",
		Status:      store.StatusPassed,
		SummaryJSON: `{"kind":"apiCase","caseId":"case.apply","stepId":"apply","ok":true}`,
		CreatedAt:   started,
		UpdatedAt:   started,
	})
	if err != nil {
		t.Fatalf("create source run: %v", err)
	}
	_, err = s.SaveTraceTopology(ctx, store.TraceTopology{
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
		CreatedAt:     started,
	})
	if err != nil {
		t.Fatalf("save source topology: %v", err)
	}
	_, err = s.RecordPostProcessTask(ctx, store.PostProcessTask{
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
	})
	if err != nil {
		t.Fatalf("record source post-process task: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/workflow-runs", "application/json", strings.NewReader(`{
		"workflowId":"workflow.alpha",
		"ok":true,
		"steps":[
			{"stepId":"apply","caseId":"case.apply","runId":"run.single.apply","caseRunId":"run.single.apply.case","ok":true,"status":"passed"}
		]
	}`))
	if err != nil {
		t.Fatalf("post workflow run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("workflow run status = %d body=%s", resp.StatusCode, raw)
	}

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
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 18, 7, 0, 0, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:          "run.single.apply",
		ProfileID:   "sample",
		WorkflowID:  "workflow.alpha",
		Status:      store.StatusPassed,
		SummaryJSON: `{"kind":"apiCase","caseId":"case.apply","stepId":"apply","ok":true}`,
		CreatedAt:   started,
		UpdatedAt:   started,
	}); err != nil {
		t.Fatalf("create source run: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/workflow-runs", "application/json", strings.NewReader(`{
		"workflowId":"workflow.alpha",
		"ok":true,
		"steps":[
			{"stepId":"apply","caseId":"case.apply","runId":"run.single.apply","caseRunId":"run.single.apply.case","ok":true,"status":"passed"}
		]
	}`))
	if err != nil {
		t.Fatalf("post workflow run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("workflow run status = %d body=%s", resp.StatusCode, raw)
	}

	_, err = s.SaveTraceTopology(ctx, store.TraceTopology{
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
		CreatedAt:     started.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("save late source topology: %v", err)
	}

	latest := decodeJSONResponse(t, server.URL+"/api/workflow-runs/latest-step?workflowId=workflow.alpha&stepId=apply", http.StatusOK)
	topologies := latest["traceTopologies"].([]any)
	if len(topologies) != 1 || topologies[0].(map[string]any)["traceId"] != "trace.apply" {
		t.Fatalf("latest saved workflow step should backfill late topology: %#v", latest)
	}
}

func TestServerEvaluatesWorkflowStepTimeoutFromCatalog(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
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
	_, err = s.CreateRun(ctx, store.Run{
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
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

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
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/workflow-runs", "application/json", strings.NewReader(`{
		"workflowId":"workflow.alpha",
		"status":"passed",
		"ok":true,
		"steps":[{"stepId":"step.alpha","caseId":"case.alpha","ok":true,"summary":{"requestId":"request.alpha","httpCode":200}}],
		"summary":{"expectedStepCount":1,"stepCount":1}
	}`))
	if err != nil {
		t.Fatalf("post workflow run: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("save workflow run status = %d body=%s", resp.StatusCode, raw)
	}
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

func TestServerExposesWorkflowAuditWithoutStore(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.alpha", http.StatusOK)
	if payload["ok"] != true || payload["profileId"] != "sample" || payload["workflowId"] != "workflow.alpha" {
		t.Fatalf("workflow audit identity = %#v", payload)
	}
	if payload["bindingCount"] != float64(1) || payload["issueCount"] != float64(0) {
		t.Fatalf("workflow audit counts = %#v", payload)
	}
	if _, ok := payload["store"]; ok {
		t.Fatalf("workflow audit without store should not include store report: %#v", payload)
	}
	bindings := payload["bindings"].([]any)
	if len(bindings) != 1 || bindings[0].(map[string]any)["caseId"] != "case.alpha" {
		t.Fatalf("workflow audit bindings = %#v", payload)
	}
}

func TestServerExposesWorkflowPlanAPI(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.one", NodeID: "node.alpha", CaseID: "case.alpha", Required: true, SortOrder: 1},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-plan?workflowId=workflow.alpha", http.StatusOK)
	if payload["ok"] != true || payload["workflowId"] != "workflow.alpha" {
		t.Fatalf("workflow plan summary = %#v", payload)
	}
	workflow := payload["workflow"].(map[string]any)
	if workflow["id"] != "workflow.alpha" || workflow["displayName"] != "Workflow Alpha" {
		t.Fatalf("workflow plan workflow = %#v", workflow)
	}
	counts := payload["counts"].(map[string]any)
	if counts["steps"] != float64(1) || counts["requiredSteps"] != float64(1) {
		t.Fatalf("workflow plan counts = %#v", counts)
	}
	steps := payload["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("workflow plan steps = %#v", payload)
	}
	step := steps[0].(map[string]any)
	if step["stepId"] != "step.one" || step["nodeId"] != "node.alpha" || step["caseId"] != "case.alpha" || step["required"] != true {
		t.Fatalf("workflow plan step = %#v", step)
	}
	if node := step["node"].(map[string]any); node["displayName"] != "Node Alpha" {
		t.Fatalf("workflow plan step node = %#v", node)
	}
	if item := step["case"].(map[string]any); item["displayName"] != "Case Alpha" {
		t.Fatalf("workflow plan step case = %#v", item)
	}
}

func TestServerExposesWorkflowDiscoveryAPI(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha", Description: "Primary smoke path"},
			{ID: "workflow.beta", DisplayName: "Workflow Beta"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.one", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
			{WorkflowID: "workflow.alpha", StepID: "step.two", NodeID: "node.beta", CaseID: "case.beta", Required: true},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflows?filter=smoke", http.StatusOK)
	if payload["ok"] != true || payload["profileId"] != "sample" || payload["count"] != float64(1) {
		t.Fatalf("workflow discovery summary = %#v", payload)
	}
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("workflow discovery items = %#v", payload)
	}
	item := items[0].(map[string]any)
	if item["id"] != "workflow.alpha" || item["displayName"] != "Workflow Alpha" || item["stepCount"] != float64(2) {
		t.Fatalf("workflow discovery item = %#v", item)
	}
}

func TestServerExposesWorkflowDiscoveryAPIFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "store-profile",
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.store", DisplayName: "Store Workflow", Description: "Store smoke path"},
			{ID: "workflow.other", DisplayName: "Other Workflow"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.store", StepID: "step.one", NodeID: "node.store", CaseID: "case.store", Required: true},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "bundle-profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflows?filter=store", http.StatusOK)
	if payload["ok"] != true || payload["profileId"] != "store-profile" || payload["count"] != float64(1) {
		t.Fatalf("workflow discovery store summary = %#v", payload)
	}
	source := payload["source"].(map[string]any)
	if source["kind"] != "store" {
		t.Fatalf("workflow discovery source = %#v", source)
	}
	item := payload["items"].([]any)[0].(map[string]any)
	if item["id"] != "workflow.store" || item["stepCount"] != float64(1) {
		t.Fatalf("workflow discovery store item = %#v", item)
	}
}

func TestServerExposesWorkflowAuditStoreState(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	older := time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	for _, item := range []struct {
		id        string
		status    string
		createdAt time.Time
		caseRuns  []store.APICaseRun
	}{
		{
			id:        "run.alpha",
			status:    store.StatusPassed,
			createdAt: older,
			caseRuns: []store.APICaseRun{
				{ID: "run.alpha.case.alpha", CaseID: "case.alpha", Status: store.StatusPassed, CreatedAt: older},
			},
		},
		{
			id:        "run.beta",
			status:    store.StatusFailed,
			createdAt: newer,
			caseRuns: []store.APICaseRun{
				{ID: "run.beta.case.alpha", CaseID: "case.alpha", Status: store.StatusFailed, CreatedAt: newer},
				{ID: "run.beta.case.beta", CaseID: "case.beta", Status: store.StatusPassed, CreatedAt: newer},
			},
		},
	} {
		_, err = s.CreateRun(ctx, store.Run{
			ID:          item.id,
			ProfileID:   "sample",
			WorkflowID:  "workflow.alpha",
			Status:      item.status,
			SummaryJSON: "{}",
			CreatedAt:   item.createdAt,
			UpdatedAt:   item.createdAt,
		})
		if err != nil {
			t.Fatalf("create run %s: %v", item.id, err)
		}
		for _, caseRun := range item.caseRuns {
			caseRun.RunID = item.id
			_, err = s.RecordAPICaseRun(ctx, caseRun)
			if err != nil {
				t.Fatalf("record api case run %s: %v", caseRun.ID, err)
			}
		}
	}

	bundle := profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
			{ID: "node.beta", DisplayName: "Node Beta"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", NodeID: "node.alpha"},
			{ID: "case.beta", NodeID: "node.beta"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
			{WorkflowID: "workflow.alpha", StepID: "step.beta", NodeID: "node.beta", CaseID: "case.beta", Required: false},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.alpha", http.StatusOK)
	storeReport := payload["store"].(map[string]any)
	latestRun := storeReport["latestRun"].(map[string]any)
	if latestRun["id"] != "run.beta" || latestRun["status"] != store.StatusFailed {
		t.Fatalf("workflow audit latest run = %#v", storeReport)
	}
	bindingCases := storeReport["bindingCases"].([]any)
	if len(bindingCases) != 2 {
		t.Fatalf("workflow audit binding cases = %#v", storeReport)
	}
	alpha := bindingCases[0].(map[string]any)
	if alpha["caseId"] != "case.alpha" || alpha["latestStatus"] != store.StatusFailed || alpha["latestRunId"] != "run.beta" || alpha["hasPassed"] != true {
		t.Fatalf("workflow audit alpha case state = %#v", alpha)
	}
	beta := bindingCases[1].(map[string]any)
	if beta["caseId"] != "case.beta" || beta["latestStatus"] != store.StatusPassed || beta["latestRunId"] != "run.beta" || beta["required"] != false {
		t.Fatalf("workflow audit beta case state = %#v", beta)
	}
}

func TestServerRejectsWorkflowAuditWithoutWorkflowID(t *testing.T) {
	server := httptest.NewServer(controlplane.New(profile.Bundle{ID: "sample"}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit", http.StatusBadRequest)
	if payload["ok"] != false || !strings.Contains(payload["error"].(string), "workflowId") {
		t.Fatalf("workflow audit missing id payload = %#v", payload)
	}
}

func TestServerReturnsNotFoundForUnknownWorkflowAudit(t *testing.T) {
	server := httptest.NewServer(controlplane.New(profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
	}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.missing", http.StatusNotFound)
	if payload["ok"] != false || !strings.Contains(payload["error"].(string), "workflow not found") {
		t.Fatalf("workflow audit missing workflow payload = %#v", payload)
	}
}

func TestServerReturnsInternalErrorForWorkflowAuditStoreFailure(t *testing.T) {
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{
		ID: "sample",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
	}, failingListRunsStore{}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/workflow-audit?workflowId=workflow.alpha", http.StatusInternalServerError)
	if payload["ok"] != false || !strings.Contains(payload["error"].(string), "list runs failed") {
		t.Fatalf("workflow audit store failure payload = %#v", payload)
	}
}
