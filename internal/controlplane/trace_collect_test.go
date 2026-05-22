package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
)

func TestTraceTopologyCollectPersistsProviderSpanRefs(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	startedAt := time.Date(2026, 5, 15, 8, 30, 0, 0, time.UTC)
	_, err = s.CreateRun(ctx, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  startedAt,
		FinishedAt: startedAt.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["POST:/alpha"],"duration":120,"start":"2026-05-15 0830","isError":false,"traceIds":["trace.alpha"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.alpha","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/alpha","type":"Entry","component":"Tomcat"},{"traceId":"trace.alpha","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.alpha","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"POST:/alpha","type":"Entry","component":"Server"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	server := httptest.NewServer(NewWithOptions(profile.Bundle{ID: "sample"}, Options{
		Runtime:         s,
		TraceGraphQLURL: provider.URL,
	}))
	defer server.Close()

	body := map[string]any{
		"runId":     "run.alpha",
		"stepId":    "step.alpha",
		"caseId":    "case.alpha",
		"requestId": "request.alpha",
		"endpoint":  "/alpha",
		"startedAt": startedAt.Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal collect request: %v", err)
	}
	resp, err := http.Post(server.URL+"/api/trace-topology/collect", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("collect topology: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("collect status = %d", resp.StatusCode)
	}

	var payload struct {
		TraceTopology struct {
			WorkflowRunID string `json:"workflowRunId"`
			TraceID       string `json:"traceId"`
			Status        string `json:"status"`
		} `json:"traceTopology"`
		Topology struct {
			SpanCount      int `json:"spanCount"`
			ConfirmedEdges []struct {
				Source string `json:"source"`
				Target string `json:"target"`
			} `json:"confirmedEdges"`
		} `json:"topology"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode collect response: %v", err)
	}
	if payload.TraceTopology.WorkflowRunID != "run.alpha" || payload.TraceTopology.TraceID != "trace.alpha" || payload.TraceTopology.Status != "complete" {
		t.Fatalf("trace topology row = %#v", payload.TraceTopology)
	}
	if payload.Topology.SpanCount != 2 || len(payload.Topology.ConfirmedEdges) != 1 {
		t.Fatalf("topology summary = %#v", payload.Topology)
	}
	edge := payload.Topology.ConfirmedEdges[0]
	if edge.Source != "service.entry" || edge.Target != "service.worker" {
		t.Fatalf("confirmed edge = %#v", edge)
	}

	rows, err := s.ListTraceTopologies(ctx, "run.alpha")
	if err != nil {
		t.Fatalf("list trace topologies: %v", err)
	}
	if len(rows) != 1 || rows[0].WorkflowID != "workflow.alpha" || rows[0].CaseID != "case.alpha" {
		t.Fatalf("stored topologies = %#v", rows)
	}
	if strings.TrimSpace(rows[0].ID) == "" {
		t.Fatalf("stored topology id should be generated when payload omits id: %#v", rows[0])
	}
	tasks, err := s.ListPostProcessTasks(ctx, "run.alpha")
	if err != nil {
		t.Fatalf("list post-process tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Kind != postProcessKindTraceTopology || tasks[0].Status != store.StatusPassed {
		t.Fatalf("post-process tasks = %#v", tasks)
	}
	if tasks[0].WorkflowID != "workflow.alpha" || tasks[0].StepID != "step.alpha" || tasks[0].CaseID != "case.alpha" {
		t.Fatalf("post-process task context = %#v", tasks[0])
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(tasks[0].SummaryJSON), &summary); err != nil {
		t.Fatalf("decode post-process task summary: %v", err)
	}
	if summary["traceId"] != "trace.alpha" || summary["requestId"] != "request.alpha" || summary["topologyStatus"] != "complete" || summary["spanCount"] != float64(2) {
		t.Fatalf("post-process task summary = %#v", summary)
	}
}

func TestTraceTopologyCollectRecordsFailedPostProcessTaskOnProviderError(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:         "run.provider-error",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusFailed,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[]}}}`))
	}))
	defer provider.Close()

	server := httptest.NewServer(NewWithOptions(profile.Bundle{ID: "sample"}, Options{
		Runtime:         s,
		TraceGraphQLURL: provider.URL,
	}))
	defer server.Close()

	raw, err := json.Marshal(map[string]any{
		"runId":     "run.provider-error",
		"stepId":    "step.alpha",
		"caseId":    "case.alpha",
		"requestId": "request.alpha",
		"traceId":   "trace.missing",
	})
	if err != nil {
		t.Fatalf("marshal collect request: %v", err)
	}
	resp, err := http.Post(server.URL+"/api/trace-topology/collect", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("collect topology: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("collect status = %d", resp.StatusCode)
	}
	tasks, err := s.ListPostProcessTasks(ctx, "run.provider-error")
	if err != nil {
		t.Fatalf("list post-process tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Kind != postProcessKindTraceTopology || tasks[0].Status != store.StatusFailed {
		t.Fatalf("post-process tasks = %#v", tasks)
	}
	if tasks[0].WorkflowID != "workflow.alpha" || tasks[0].StepID != "step.alpha" || tasks[0].CaseID != "case.alpha" || tasks[0].Error == "" {
		t.Fatalf("failed post-process task = %#v", tasks[0])
	}
}

func TestTraceTopologyCollectUsesExplicitTraceID(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:         "run.direct",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		if strings.Contains(payload.Query, "queryBasicTraces") {
			t.Fatalf("explicit trace id should not query candidates")
		}
		if payload.Variables["traceId"] != "trace.direct" {
			t.Fatalf("trace id variable = %#v", payload.Variables)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.direct","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/direct","type":"Entry","component":"Tomcat"},{"traceId":"trace.direct","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.direct","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"POST:/direct","type":"Entry","component":"Server"}]}}}`))
	}))
	defer provider.Close()

	server := httptest.NewServer(NewWithOptions(profile.Bundle{ID: "sample"}, Options{
		Runtime:         s,
		TraceGraphQLURL: provider.URL,
	}))
	defer server.Close()

	raw, err := json.Marshal(map[string]any{
		"runId":     "run.direct",
		"stepId":    "step.direct",
		"caseId":    "case.direct",
		"requestId": "request.direct",
		"traceId":   "trace.direct",
	})
	if err != nil {
		t.Fatalf("marshal collect request: %v", err)
	}
	resp, err := http.Post(server.URL+"/api/trace-topology/collect", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("collect topology: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("collect status = %d", resp.StatusCode)
	}

	rows, err := s.ListTraceTopologies(ctx, "run.direct")
	if err != nil {
		t.Fatalf("list trace topologies: %v", err)
	}
	if len(rows) != 1 || rows[0].TraceID != "trace.direct" || rows[0].RequestID != "request.direct" || rows[0].Status != "complete" {
		t.Fatalf("stored direct topology = %#v", rows)
	}
}

func TestTraceCandidatesPreferRunTimeWindow(t *testing.T) {
	startedAt := time.Date(2026, 5, 18, 11, 1, 34, 793739000, time.UTC)
	finishedAt := time.Date(2026, 5, 18, 11, 1, 35, 223739000, time.UTC)
	candidates := []traceCandidate{
		{TraceID: "trace.too-early", Start: "1779102093735"},
		{TraceID: "trace.inside", Start: "1779102095116"},
	}

	sortTraceCandidatesByRunWindow(candidates, startedAt, finishedAt)

	if candidates[0].TraceID != "trace.inside" {
		t.Fatalf("first candidate = %#v", candidates[0])
	}
}

func TestTestKitTraceTopologyCollectPayloadUsesSandboxCallbackPath(t *testing.T) {
	payload, ok := testKitTraceTopologyCollectPayload("run.callback", map[string]any{
		"stepId": "callback",
	}, map[string]any{
		"ok":     true,
		"caseId": "case.callback",
		"result": map[string]any{
			"request": map[string]any{
				"path": "/__sandbox/llt/callback",
				"headers": map[string]any{
					"X-Sandbox-Callback-Path": "/account-app/v1/llt/notice",
				},
			},
			"response": map[string]any{
				"headers": map[string]any{},
			},
		},
	})
	if !ok {
		t.Fatalf("collect payload was not built")
	}
	if payload["endpoint"] != "/account-app/v1/llt/notice" {
		t.Fatalf("endpoint = %#v", payload["endpoint"])
	}
}

func TestTestKitTraceTopologyCollectPayloadUsesConfiguredTraceEndpoint(t *testing.T) {
	payload, ok := testKitTraceTopologyCollectPayload("run.gateway", map[string]any{
		"stepId":        "trial",
		"traceEndpoint": "POST:/api/v1/acc/scf/financing/trial",
	}, map[string]any{
		"ok":     true,
		"caseId": "case.trial",
		"result": map[string]any{
			"request": map[string]any{
				"path": "/v1/supplychain/financing/trial",
			},
			"response": map[string]any{},
		},
	})
	if !ok {
		t.Fatalf("collect payload was not built")
	}
	if payload["endpoint"] != "POST:/api/v1/acc/scf/financing/trial" {
		t.Fatalf("endpoint = %#v", payload["endpoint"])
	}
}

func TestReadJSONPayloadPreservesLargeNumericOverrides(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/test-kit/run", strings.NewReader(`{
		"overrides": {
			"payout_id": 9161030727085880
		}
	}`))

	payload, err := readJSONPayload(request)
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	overrides := mapFromAny(payload["overrides"])
	rendered := renderCaseString("{{override:payout_id}}", overrides)

	if rendered != "9161030727085880" {
		t.Fatalf("rendered payout_id = %q", rendered)
	}
}
