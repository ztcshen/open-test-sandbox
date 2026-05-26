package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestServerExposesCaseRunsFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  "{}",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.alpha.case",
		RunID:                "run.alpha",
		CaseID:               "case.alpha",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"passed","errorCount":0}`,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "run.alpha.response",
		RunID:     "run.alpha",
		CaseRunID: "run.alpha.case",
		Kind:      "response",
		URI:       ".runtime/evidence/run.alpha/response.json",
		MediaType: "application/json",
		Summary:   `{"statusCode":200}`,
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/runs", http.StatusOK)
	caseRuns := payload["caseRuns"].([]any)
	if len(caseRuns) != 1 {
		t.Fatalf("case runs = %#v", payload)
	}
	item := caseRuns[0].(map[string]any)
	if item["runId"] != "run.alpha" || item["caseId"] != "case.alpha" || item["status"] != "passed" {
		t.Fatalf("case run item = %#v", item)
	}
	if item["operation"] != "POST /alpha" || item["evidencePath"] != ".runtime/evidence/run.alpha" || item["evidenceCount"] != float64(1) {
		t.Fatalf("case run details = %#v", item)
	}
}

func TestServerExposesCaseRunFailureCategoriesFromProfile(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.failed",
		ProfileID:    "sample",
		Status:       store.StatusFailed,
		EvidenceRoot: ".runtime/evidence/run.failed",
		SummaryJSON:  "{}",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.failed.case",
		RunID:                "run.failed",
		CaseID:               "case.failed",
		Status:               store.StatusFailed,
		RequestSummaryJSON:   `{"method":"GET","path":"/failed"}`,
		AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		FailureCategories: []profile.FailureCategoryRule{
			{
				Name: "Product errors",
				Matchers: profile.FailureCategoryMatchers{
					Statuses:          []string{store.StatusFailed},
					FailureCategories: []string{"assertion-mismatch"},
					MessageContains:   []string{"assertion errors"},
				},
			},
			{
				Name: "Later matching rule",
				Matchers: profile.FailureCategoryMatchers{
					Statuses: []string{store.StatusFailed},
				},
			},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/runs", http.StatusOK)
	caseRuns := payload["caseRuns"].([]any)
	if len(caseRuns) != 1 {
		t.Fatalf("case runs = %#v", payload)
	}
	item := caseRuns[0].(map[string]any)
	if item["failureCategory"] != "Product errors" || item["defaultFailureCategory"] != "assertion-mismatch" {
		t.Fatalf("case run failure category = %#v", item)
	}
	categories := payload["failureCategories"].([]any)
	if len(categories) != 2 || categories[0].(map[string]any)["name"] != "Product errors" {
		t.Fatalf("failure category rules = %#v", payload["failureCategories"])
	}
}

func TestServerExposesCaseEvidenceFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  "{}",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.alpha.case",
		RunID:                "run.alpha",
		CaseID:               "case.alpha",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha","hasBody":true}`,
		AssertionSummaryJSON: `{"status":"passed","errorCount":0}`,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	evidenceDir := t.TempDir()
	requestPath := filepath.Join(evidenceDir, "request.json")
	if err := os.WriteFile(requestPath, []byte(`{"method":"POST","path":"/alpha?token=query-secret","headers":{"Content-Type":"application/json","Authorization":"Bearer request-secret"},"body":{"id":"item-001","token":"request-token"}}`), 0o644); err != nil {
		t.Fatalf("write request evidence: %v", err)
	}
	responsePath := filepath.Join(evidenceDir, "response.json")
	if err := os.WriteFile(responsePath, []byte(`{"statusCode":200,"headers":{"Content-Type":"application/json","Set-Cookie":"session=response-cookie"},"body":"{\"ok\":true,\"password\":\"response-secret\"}"}`), 0o644); err != nil {
		t.Fatalf("write response evidence: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         "run.alpha.request",
		RunID:      "run.alpha",
		CaseRunID:  "run.alpha.case",
		StepID:     "step.alpha",
		Kind:       "request",
		URI:        requestPath,
		MediaType:  "application/json",
		SHA256:     "sha256-request",
		SizeBytes:  128,
		Summary:    `{"method":"POST","path":"/alpha","hasBody":true}`,
		Category:   "http-exchange",
		Visibility: "public",
		LabelsJSON: `{"kind":"request","owner":"qa"}`,
	})
	if err != nil {
		t.Fatalf("record request evidence: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "run.alpha.response",
		RunID:     "run.alpha",
		CaseRunID: "run.alpha.case",
		Kind:      "response",
		URI:       responsePath,
		MediaType: "application/json",
		Summary:   `{"statusCode":200,"bodyBytes":19}`,
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId=run.alpha&caseId=case.alpha", http.StatusOK)
	evidence := payload["evidence"].(map[string]any)
	summary := evidence["summary"].(map[string]any)
	request := evidence["request"].(map[string]any)
	response := evidence["response"].(map[string]any)
	assertions := evidence["assertions"].(map[string]any)
	if summary["case_id"] != "case.alpha" || request["method"] != "POST" || request["path"] != "/alpha?token=%5BREDACTED%5D" {
		t.Fatalf("case evidence request = %#v", payload)
	}
	requestBody := request["body"].(map[string]any)
	if requestBody["id"] != "item-001" {
		t.Fatalf("case evidence request body = %#v", request)
	}
	redactedRequest, _ := json.Marshal(request)
	for _, leaked := range []string{"query-secret", "request-secret", "request-token"} {
		if strings.Contains(string(redactedRequest), leaked) {
			t.Fatalf("case evidence request leaked %q: %s", leaked, redactedRequest)
		}
	}
	if !strings.Contains(string(redactedRequest), "[REDACTED]") {
		t.Fatalf("case evidence request was not redacted: %s", redactedRequest)
	}
	if response["http_code"] != float64(200) || assertions["status"] != "passed" {
		t.Fatalf("case evidence response/assertions = %#v", payload)
	}
	redactedResponse, _ := json.Marshal(response)
	for _, leaked := range []string{"response-secret", "response-cookie"} {
		if strings.Contains(string(redactedResponse), leaked) {
			t.Fatalf("case evidence response leaked %q: %s", leaked, redactedResponse)
		}
	}
	if !strings.Contains(fmt.Sprint(response["body"]), "[REDACTED]") {
		t.Fatalf("case evidence response body = %#v", response)
	}
	attachment := request["attachment"].(map[string]any)
	labels := attachment["labels"].(map[string]any)
	if attachment["category"] != "http-exchange" || attachment["visibility"] != "public" || labels["owner"] != "qa" {
		t.Fatalf("case evidence attachment metadata = %#v", request)
	}
	if attachment["id"] != "run.alpha.request" || attachment["runId"] != "run.alpha" || attachment["caseRunId"] != "run.alpha.case" || attachment["stepId"] != "step.alpha" || attachment["kind"] != "request" {
		t.Fatalf("case evidence attachment identity = %#v", attachment)
	}
	if attachment["uri"] != requestPath || attachment["mediaType"] != "application/json" || attachment["sha256"] != "sha256-request" || attachment["sizeBytes"] != float64(128) {
		t.Fatalf("case evidence attachment file metadata = %#v", attachment)
	}
}

func TestServerExposesFailedCaseRunEvidenceByCaseRunID(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC)
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusFailed,
		EvidenceRoot: filepath.Join(t.TempDir(), "evidence"),
		SummaryJSON:  `{"steps":[{"stepId":"step.alpha","caseId":"case.alpha"}]}`,
		CreatedAt:    started,
		UpdatedAt:    started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "case-run.alpha",
		RunID:                "run.alpha",
		CaseID:               "case.alpha",
		Status:               store.StatusFailed,
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha","stepId":"step.alpha"}`,
		AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
		StartedAt:            started,
		FinishedAt:           started.Add(300 * time.Millisecond),
		CreatedAt:            started,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	evidenceDir := t.TempDir()
	logPath := filepath.Join(evidenceDir, "runtime-logs.json")
	if err := os.WriteFile(logPath, []byte(`{"systems":[{"id":"service.alpha","name":"Service Alpha","found":true,"coreLogs":["request.alpha failed in worker"]}]}`), 0o644); err != nil {
		t.Fatalf("write runtime logs: %v", err)
	}
	_, err = s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "run.alpha.logs",
		RunID:     "run.alpha",
		CaseRunID: "case-run.alpha",
		Kind:      "runtime_logs",
		URI:       logPath,
		MediaType: "application/json",
		Summary:   `{"caseId":"case.alpha","stepId":"step.alpha","systems":1}`,
		CreatedAt: started,
	})
	if err != nil {
		t.Fatalf("record runtime logs: %v", err)
	}
	_, err = s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.alpha",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.alpha",
		CaseID:        "case.alpha",
		RequestID:     "request.alpha",
		TraceID:       "trace.alpha",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
	})
	if err != nil {
		t.Fatalf("save topology: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case-run/evidence?caseRunId=case-run.alpha", http.StatusOK)
	evidence := payload["evidence"].(map[string]any)
	summary := evidence["summary"].(map[string]any)
	if summary["case_run_id"] != "case-run.alpha" || summary["run_id"] != "run.alpha" || summary["status"] != store.StatusFailed {
		t.Fatalf("failed case evidence summary = %#v", summary)
	}
	topology := evidence["topology"].(map[string]any)
	if topology["traceId"] != "trace.alpha" || len(topology["confirmedEdges"].([]any)) != 1 {
		t.Fatalf("failed case evidence topology = %#v", topology)
	}
	logs := evidence["logs"].([]any)
	if len(logs) != 1 {
		t.Fatalf("failed case evidence logs = %#v", evidence)
	}
	log := logs[0].(map[string]any)
	systems := log["systems"].([]any)
	if log["kind"] != "runtime_logs" || len(systems) != 1 || systems[0].(map[string]any)["found"] != true {
		t.Fatalf("failed case evidence log details = %#v", logs)
	}
}

func TestServerExposesCaseEvidenceDependenciesWithoutInventingTopology(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.alpha", DisplayName: "Alpha workflow"},
		},
		Services: []store.CatalogService{
			{ID: "service.one", DisplayName: "One", Kind: "app"},
			{ID: "service.two", DisplayName: "Two", Kind: "app"},
			{ID: "service.three", DisplayName: "Three", Kind: "app"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.one", DisplayName: "Node One", ServiceID: "service.one", Status: "active", SortOrder: 1},
			{ID: "node.two", DisplayName: "Node Two", ServiceID: "service.two", Status: "active", SortOrder: 2},
			{ID: "node.three", DisplayName: "Node Three", ServiceID: "service.three", Status: "active", SortOrder: 3},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.step.one", DisplayName: "Step One", NodeID: "node.one", RequiredForAdmission: true, Status: "active", SortOrder: 1},
			{ID: "case.step.two.config", DisplayName: "Step Two", NodeID: "node.two", RequiredForAdmission: true, Status: "active", SortOrder: 2},
			{ID: "case.step.three", DisplayName: "Step Three", NodeID: "node.three", RequiredForAdmission: true, Status: "active", SortOrder: 3},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.one", NodeID: "node.one", CaseID: "case.step.one", Required: true, SortOrder: 1},
			{WorkflowID: "workflow.alpha", StepID: "step.two", NodeID: "node.two", CaseID: "case.step.two.config", Required: true, SortOrder: 2},
			{WorkflowID: "workflow.alpha", StepID: "step.three", NodeID: "node.three", CaseID: "case.step.three", Required: true, SortOrder: 3},
		},
		Fixtures: []store.CatalogFixture{
			{ID: "fixture.after.second", DisplayName: "After second", Kind: "workflow_prefix", SourceWorkflowID: "workflow.alpha", SourceUntilStep: "step.two", Status: "active", SortOrder: 1},
		},
		CaseDependencies: []store.CatalogCaseDependency{
			{ID: "dependency.step.three", CaseID: "case.step.three", FixtureID: "fixture.after.second", Required: true, MappingsJSON: `[{"from":"$.exports.item_id","to":"$.request.item_id"}]`, Status: "active", SortOrder: 1},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:        "run.alpha",
		ProfileID: "sample",
		Status:    store.StatusPassed,
		SummaryJSON: `{"steps":[
			{"stepId":"step.one","caseId":"case.step.one","ok":true},
			{"stepId":"step.two","caseId":"case.step.two.runtime","ok":true},
			{"stepId":"step.three","caseId":"case.step.three","ok":true}
		]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	for _, item := range []store.APICaseRun{
		{ID: "run.alpha.case.one", RunID: "run.alpha", CaseID: "case.step.one", Status: store.StatusPassed, RequestSummaryJSON: `{"method":"POST","path":"/one"}`, AssertionSummaryJSON: `{"status":"passed"}`},
		{ID: "run.alpha.case.two", RunID: "run.alpha", CaseID: "case.step.two.runtime", Status: store.StatusPassed, RequestSummaryJSON: `{"method":"POST","path":"/two"}`, AssertionSummaryJSON: `{"status":"passed"}`},
		{ID: "run.alpha.case.three", RunID: "run.alpha", CaseID: "case.step.three", Status: store.StatusPassed, RequestSummaryJSON: `{"method":"POST","path":"/three"}`, AssertionSummaryJSON: `{"status":"passed"}`},
	} {
		if _, err := s.RecordAPICaseRun(ctx, item); err != nil {
			t.Fatalf("record api case run: %v", err)
		}
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId=run.alpha&caseId=case.step.three", http.StatusOK)
	evidence := payload["evidence"].(map[string]any)
	fixture := evidence["fixture"].(map[string]any)
	if fixture["status"] != "configured" {
		t.Fatalf("fixture status = %#v", fixture)
	}
	dependencies := fixture["dependencies"].([]any)
	if len(dependencies) != 1 {
		t.Fatalf("dependencies = %#v", fixture)
	}
	dependency := dependencies[0].(map[string]any)
	if dependency["fixtureProfileId"] != "fixture.after.second" {
		t.Fatalf("dependency = %#v", dependency)
	}
	upstreamSteps := fixture["upstreamSteps"].([]any)
	if len(upstreamSteps) != 2 {
		t.Fatalf("upstream steps = %#v", fixture)
	}
	applyRuns := fixture["applyRuns"].([]any)
	if len(applyRuns) != 2 {
		t.Fatalf("apply runs = %#v", fixture)
	}
	firstApply := applyRuns[0].(map[string]any)
	if firstApply["caseId"] != "case.step.one" || firstApply["runId"] != "run.alpha" || firstApply["status"] != "applied" {
		t.Fatalf("first apply run = %#v", firstApply)
	}
	secondApply := applyRuns[1].(map[string]any)
	if secondApply["caseId"] != "case.step.two.runtime" || secondApply["stepId"] != "step.two" {
		t.Fatalf("second apply run = %#v", secondApply)
	}
	fixtureSummary := fixture["summary"].(map[string]any)
	if fixtureSummary["applyCount"] != float64(2) || fixtureSummary["failedCount"] != float64(0) {
		t.Fatalf("fixture summary = %#v", fixtureSummary)
	}
	topology := evidence["topology"].(map[string]any)
	if topology["status"] != "unavailable" {
		t.Fatalf("topology = %#v", topology)
	}
	edges := topology["confirmedEdges"].([]any)
	if len(edges) != 0 {
		t.Fatalf("workflow order must not be exposed as confirmed topology edges: %#v", topology)
	}
}

func TestServerSelectsCaseEvidenceWithinWorkflowRun(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	_, err = s.CreateRun(ctx, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		SummaryJSON: `{"steps":[
			{"stepId":"step.one","caseId":"case.one"},
			{"stepId":"step.two","caseId":"case.two"}
		]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	for _, item := range []store.APICaseRun{
		{ID: "run.alpha.case.01", RunID: "run.alpha", CaseID: "case.one", Status: store.StatusPassed, AssertionSummaryJSON: `{"status":"passed"}`},
		{ID: "run.alpha.case.02", RunID: "run.alpha", CaseID: "case.two", Status: store.StatusPassed, AssertionSummaryJSON: `{"status":"passed"}`},
	} {
		if _, err := s.RecordAPICaseRun(ctx, item); err != nil {
			t.Fatalf("record case run: %v", err)
		}
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.two",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.two",
		CaseID:        "case.two",
		RequestID:     "request.two",
		TraceID:       "trace.two",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"service.one","target":"service.two"}],"externalExits":[],"unresolvedExits":[]}`,
	}); err != nil {
		t.Fatalf("save trace topology: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId=run.alpha&caseId=case.two", http.StatusOK)
	evidence := payload["evidence"].(map[string]any)
	summary := evidence["summary"].(map[string]any)
	if summary["case_id"] != "case.two" {
		t.Fatalf("selected evidence summary = %#v", payload)
	}
	topology := evidence["topology"].(map[string]any)
	if topology["traceId"] != "trace.two" || len(topology["confirmedEdges"].([]any)) != 1 {
		t.Fatalf("selected evidence topology = %#v", topology)
	}

	byStep := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId=run.alpha&stepId=step.two", http.StatusOK)
	byStepEvidence := byStep["evidence"].(map[string]any)
	byStepSummary := byStepEvidence["summary"].(map[string]any)
	if byStepSummary["case_id"] != "case.two" {
		t.Fatalf("step-selected evidence summary = %#v", byStep)
	}
}

func TestServerExposesCaseTimingFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		runID    string
		caseID   string
		duration time.Duration
	}{
		{runID: "run.fast", caseID: "case.fast", duration: 150 * time.Millisecond},
		{runID: "run.slow", caseID: "case.slow", duration: 1250 * time.Millisecond},
	} {
		_, err = s.CreateRun(ctx, store.Run{
			ID:           item.runID,
			ProfileID:    "sample",
			Status:       store.StatusPassed,
			EvidenceRoot: ".runtime/evidence/" + item.runID,
			SummaryJSON:  "{}",
			StartedAt:    started,
			FinishedAt:   started.Add(item.duration),
			CreatedAt:    started,
			UpdatedAt:    started.Add(item.duration),
		})
		if err != nil {
			t.Fatalf("create run %s: %v", item.runID, err)
		}
		_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
			ID:                   item.runID + ".case",
			RunID:                item.runID,
			CaseID:               item.caseID,
			Status:               store.StatusPassed,
			RequestSummaryJSON:   `{"method":"GET","path":"/timing"}`,
			AssertionSummaryJSON: `{"status":"passed","errorCount":0}`,
			StartedAt:            started,
			FinishedAt:           started.Add(item.duration),
			CreatedAt:            started,
		})
		if err != nil {
			t.Fatalf("record case run %s: %v", item.runID, err)
		}
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/case/timing?kind=case", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["caseRunCount"] != float64(2) || summary["durationMeasuredCount"] != float64(2) || summary["maxDurationMs"] != float64(1250) {
		t.Fatalf("case timing summary = %#v", summary)
	}
	slowest := summary["slowestRows"].(map[string]any)["caseRun"].(map[string]any)
	if slowest["id"] != "run.slow.case" || slowest["caseId"] != "case.slow" || slowest["durationMs"] != float64(1250) {
		t.Fatalf("slowest timing row = %#v", slowest)
	}
}
