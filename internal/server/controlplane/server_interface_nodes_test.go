package controlplane_test

import (
	"context"
	"encoding/json"
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

func TestServerExposesInterfaceNodesForService(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
			{ID: "node.beta", DisplayName: "Node Beta", ServiceID: "service.beta"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/interface-nodes?serviceId=service.alpha")
	if err != nil {
		t.Fatalf("get interface nodes api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("interface nodes status = %d", resp.StatusCode)
	}

	var payload struct {
		Source struct {
			Kind string `json:"kind"`
		} `json:"source"`
		Items []struct {
			ID                string `json:"id"`
			DisplayName       string `json:"displayName"`
			ServiceID         string `json:"serviceId"`
			Href              string `json:"href"`
			AdmissionStatus   string `json:"admissionStatus"`
			ValidationStatus  string `json:"validationStatus"`
			RequiredCaseCount int    `json:"requiredCaseCount"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode interface nodes api: %v", err)
	}
	if payload.Source.Kind != "profile" {
		t.Fatalf("interface node source = %#v", payload.Source)
	}
	if len(payload.Items) != 1 || payload.Items[0].ID != "node.alpha" || payload.Items[0].ServiceID != "service.alpha" {
		t.Fatalf("interface node items = %#v", payload.Items)
	}
	if payload.Items[0].Href == "" || payload.Items[0].AdmissionStatus != "pending" || payload.Items[0].ValidationStatus != "valid" || payload.Items[0].RequiredCaseCount != 0 {
		t.Fatalf("interface node link/status = %#v", payload.Items[0])
	}
}

func TestServerFiltersInterfaceNodesBySearchText(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Create item"},
			{ID: "node.beta", DisplayName: "Node Beta", ServiceID: "service.beta", Operation: "Delete item"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-nodes?filter=delete", http.StatusOK)
	filters := payload["filters"].(map[string]any)
	if filters["filter"] != "delete" {
		t.Fatalf("interface node filters = %#v", filters)
	}
	items := payload["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != "node.beta" {
		t.Fatalf("filtered interface nodes = %#v", payload)
	}
}

func TestServerExposesInterfaceNodesFromLatestCaseRunsWithoutFullRunScan(t *testing.T) {
	runtime := latestCaseRunCatalogStore{
		catalog: store.ProfileCatalog{
			ProfileID: "sample",
			InterfaceNodes: []store.CatalogInterfaceNode{
				{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Method: "POST", Path: "/alpha", Status: "active"},
			},
			APICases: []store.CatalogAPICase{
				{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
			},
			TemplateConfigs: []store.CatalogTemplateConfig{
				{
					ID:         "cfg.interface-directory.default",
					TemplateID: "TPL-INTERFACE-NODE-DIRECTORY-V1",
					ScopeType:  "interface-node-directory",
					ScopeID:    "_default",
					ConfigJSON: `{"copy":{"directoryTitle":"Configured interface directory","latestElapsedLabel":"Configured latest","totalElapsedLabel":"Configured total"}}`,
					Status:     "active",
				},
			},
		},
		latest: []store.APICaseRun{
			{
				ID:         "run.alpha.case",
				RunID:      "run.alpha",
				CaseID:     "case.alpha",
				Status:     store.StatusPassed,
				StartedAt:  time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
				FinishedAt: time.Date(2026, 5, 15, 10, 0, 0, 240*int(time.Millisecond), time.UTC),
			},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, runtime))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("interface node items = %#v", items)
	}
	item := items[0].(map[string]any)
	if item["admissionStatus"] != store.StatusPassed || item["passedCaseCount"] != float64(1) || item["latestRunId"] != "run.alpha" {
		t.Fatalf("interface node latest state = %#v", item)
	}
	if item["latestElapsedMs"] != float64(240) || item["totalElapsedMs"] != float64(240) {
		t.Fatalf("interface node elapsed state = %#v", item)
	}
	presentation := payload["presentation"].(map[string]any)
	copy := presentation["copy"].(map[string]any)
	if copy["directoryTitle"] != "Configured interface directory" || copy["totalElapsedLabel"] != "Configured total" {
		t.Fatalf("interface node directory presentation = %#v", presentation)
	}
}

func TestServerHydratesInterfaceNodeCoverageFromLatestCaseRunsWithoutFullRunScan(t *testing.T) {
	catalog := store.ProfileCatalog{
		ProfileID: "sample",
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
		},
	}
	models, err := controlplane.InterfaceNodeCoverageReadModels(catalog, "config.sample.001", time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build coverage read models: %v", err)
	}
	readModels := map[string]store.ReadModel{}
	for _, model := range models {
		readModels[model.Key] = model
	}
	runtime := latestCaseRunCatalogStore{
		catalog:    catalog,
		readModels: readModels,
		latest: []store.APICaseRun{
			{ID: "run.alpha.case", RunID: "run.alpha", CaseID: "case.alpha", Status: store.StatusPassed},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, runtime))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node/coverage?workflow=workflow.alpha", http.StatusOK)
	source := payload["source"].(map[string]any)
	if source["kind"] != "read-model" {
		t.Fatalf("coverage source = %#v", source)
	}
	rows := payload["rows"].([]any)
	row := rows[0].(map[string]any)
	if row["admissionStatus"] != store.StatusPassed || row["passedCaseCount"] != float64(1) || row["latestRunId"] != "run.alpha" {
		t.Fatalf("coverage row latest state = %#v", row)
	}
	summary := payload["summary"].(map[string]any)
	if summary["passedNodes"] != float64(1) || summary["pendingNodes"] != float64(0) || summary["failedNodes"] != float64(0) {
		t.Fatalf("coverage summary latest state = %#v", summary)
	}
}

func TestServerExposesInterfaceNodeDetail(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
		RequestTemplates: []profile.RequestTemplate{
			{ID: "template.alpha", DisplayName: "Template Alpha", NodeID: "node.alpha", Method: "POST", Path: "/alpha", TemplateJSON: "{}"},
		},
		CaseDependencies: []profile.CaseDependency{
			{ID: "dependency.alpha", CaseID: "case.alpha", FixtureID: "fixture.alpha", MappingsJSON: "[]"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/interface-node?id=node.alpha")
	if err != nil {
		t.Fatalf("get interface node api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("interface node status = %d", resp.StatusCode)
	}

	var payload struct {
		Node struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			ServiceID   string `json:"serviceId"`
			Method      string `json:"method"`
			Path        string `json:"path"`
		} `json:"node"`
		Admission struct {
			Status            string `json:"status"`
			RequiredCaseCount int    `json:"requiredCaseCount"`
			PassedCaseCount   int    `json:"passedCaseCount"`
		} `json:"admission"`
		RequestTemplates []map[string]any `json:"requestTemplates"`
		Cases            []map[string]any `json:"cases"`
		Fields           struct {
			Request  []map[string]any `json:"request"`
			Response []map[string]any `json:"response"`
		} `json:"fields"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode interface node api: %v", err)
	}
	if payload.Node.ID != "node.alpha" || payload.Node.ServiceID != "service.alpha" {
		t.Fatalf("interface node detail = %#v", payload.Node)
	}
	if payload.Node.Method != "POST" || payload.Node.Path != "/alpha" {
		t.Fatalf("interface node operation = %#v", payload.Node)
	}
	if payload.Admission.Status != "pending" || payload.Admission.RequiredCaseCount != 0 || payload.Admission.PassedCaseCount != 0 {
		t.Fatalf("interface node admission = %#v", payload.Admission)
	}
	if len(payload.RequestTemplates) != 1 || payload.RequestTemplates[0]["id"] != "template.alpha" {
		t.Fatalf("interface node templates = %#v", payload.RequestTemplates)
	}
	if len(payload.Cases) != 1 || payload.Cases[0]["id"] != "case.alpha" {
		t.Fatalf("interface node cases = %#v", payload.Cases)
	}
	if payload.Cases == nil || payload.Fields.Request == nil || payload.Fields.Response == nil {
		t.Fatalf("interface node empty arrays = %#v", payload)
	}
}

func TestServerExposesInterfaceNodeRunHistoryFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		run     store.Run
		caseRun store.APICaseRun
	}{
		{
			run: store.Run{
				ID:           "run.alpha",
				ProfileID:    "sample",
				WorkflowID:   "workflow.alpha",
				Status:       store.StatusPassed,
				EvidenceRoot: ".runtime/evidence/run.alpha",
				CreatedAt:    started,
			},
			caseRun: store.APICaseRun{
				ID:                   "run.alpha.case",
				RunID:                "run.alpha",
				CaseID:               "case.alpha",
				Status:               store.StatusPassed,
				RequestSummaryJSON:   `{"method":"GET","path":"/alpha"}`,
				AssertionSummaryJSON: `{"status":"passed"}`,
				StartedAt:            started,
				FinishedAt:           started.Add(150 * time.Millisecond),
				CreatedAt:            started,
			},
		},
		{
			run: store.Run{
				ID:           "run.beta",
				ProfileID:    "sample",
				WorkflowID:   "workflow.alpha",
				Status:       store.StatusFailed,
				EvidenceRoot: ".runtime/evidence/run.beta",
				CreatedAt:    started.Add(time.Minute),
			},
			caseRun: store.APICaseRun{
				ID:                   "run.beta.case",
				RunID:                "run.beta",
				CaseID:               "case.beta",
				Status:               store.StatusFailed,
				RequestSummaryJSON:   `{"method":"POST","path":"/beta"}`,
				AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
				StartedAt:            started.Add(time.Minute),
				FinishedAt:           started.Add(time.Minute + 250*time.Millisecond),
				CreatedAt:            started.Add(time.Minute),
			},
		},
	} {
		if _, err := s.CreateRun(ctx, item.run); err != nil {
			t.Fatalf("create run %s: %v", item.run.ID, err)
		}
		if _, err := s.RecordAPICaseRun(ctx, item.caseRun); err != nil {
			t.Fatalf("record case run %s: %v", item.caseRun.ID, err)
		}
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.alpha",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.alpha",
		CaseID:        "case.alpha",
		RequestID:     "request.alpha",
		TraceID:       "trace.alpha",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","requestId":"request.alpha","traceId":"trace.alpha","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     started.Add(time.Second),
	}); err != nil {
		t.Fatalf("save trace topology: %v", err)
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
			{ID: "case.beta", DisplayName: "Case Beta", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	history := payload["history"].(map[string]any)
	if history["latestRunId"] != "run.beta" || history["runCount"] != float64(2) || history["passCount"] != float64(1) || history["failCount"] != float64(1) {
		t.Fatalf("interface node history = %#v", history)
	}
	if history["latestFailureReason"] != "assertion errors: 1" || history["totalElapsedMs"] != float64(400) {
		t.Fatalf("interface node history details = %#v", history)
	}
	cases := payload["cases"].([]any)
	if len(cases) != 2 {
		t.Fatalf("interface node cases = %#v", cases)
	}
	latest := cases[1].(map[string]any)["latestRun"].(map[string]any)
	if latest["runId"] != "run.beta" || latest["caseId"] != "case.beta" || latest["status"] != store.StatusFailed || latest["elapsedMs"] != float64(250) {
		t.Fatalf("case latest run = %#v", latest)
	}
	runs := payload["runs"].([]any)
	if len(runs) != 2 || runs[0].(map[string]any)["runId"] != "run.beta" {
		t.Fatalf("interface node runs = %#v", runs)
	}
}

func TestServerScopesInterfaceNodeRunsToWorkflowStepContext(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		run     store.Run
		caseRun store.APICaseRun
	}{
		{
			run: store.Run{
				ID:         "run.alpha",
				ProfileID:  "sample",
				WorkflowID: "workflow.alpha",
				Status:     store.StatusPassed,
				SummaryJSON: `{"steps":[
					{"stepId":"step.alpha","caseId":"case.alpha"},
					{"stepId":"step.beta","caseId":"case.beta"}
				]}`,
				CreatedAt: started,
			},
			caseRun: store.APICaseRun{
				ID:                   "run.alpha.case",
				RunID:                "run.alpha",
				CaseID:               "case.alpha",
				Status:               store.StatusPassed,
				RequestSummaryJSON:   `{"stepId":"step.alpha","method":"POST","path":"/alpha","requestId":"request.alpha"}`,
				AssertionSummaryJSON: `{"status":"passed"}`,
				StartedAt:            started,
				FinishedAt:           started.Add(150 * time.Millisecond),
				CreatedAt:            started,
			},
		},
		{
			run: store.Run{
				ID:          "run.beta",
				ProfileID:   "sample",
				WorkflowID:  "case.alpha.standalone",
				Status:      store.StatusFailed,
				SummaryJSON: `{}`,
				CreatedAt:   started.Add(time.Minute),
			},
			caseRun: store.APICaseRun{
				ID:                   "run.beta.case",
				RunID:                "run.beta",
				CaseID:               "case.alpha",
				Status:               store.StatusFailed,
				RequestSummaryJSON:   `{"method":"POST","path":"/alpha","requestId":"request.beta"}`,
				AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
				StartedAt:            started.Add(time.Minute),
				FinishedAt:           started.Add(time.Minute + 250*time.Millisecond),
				CreatedAt:            started.Add(time.Minute),
			},
		},
	} {
		if _, err := s.CreateRun(ctx, item.run); err != nil {
			t.Fatalf("create run %s: %v", item.run.ID, err)
		}
		if _, err := s.RecordAPICaseRun(ctx, item.caseRun); err != nil {
			t.Fatalf("record case run %s: %v", item.caseRun.ID, err)
		}
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.alpha",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.alpha",
		CaseID:        "case.alpha",
		RequestID:     "request.alpha",
		TraceID:       "trace.alpha",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","requestId":"request.alpha","traceId":"trace.alpha","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     started.Add(time.Second),
	}); err != nil {
		t.Fatalf("save trace topology: %v", err)
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	global := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	globalCase := global["cases"].([]any)[0].(map[string]any)
	if globalCase["latestRun"].(map[string]any)["runId"] != "run.alpha" {
		t.Fatalf("global interface node should prefer latest passing cache: %#v", globalCase)
	}

	scoped := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha&flowId=workflow.alpha&runId=run.alpha&stepId=step.alpha", http.StatusOK)
	context := scoped["context"].(map[string]any)
	if context["flowId"] != "workflow.alpha" || context["workflowId"] != "workflow.alpha" || context["runId"] != "run.alpha" || context["stepId"] != "step.alpha" {
		t.Fatalf("interface node context = %#v", context)
	}
	scopedCase := scoped["cases"].([]any)[0].(map[string]any)
	latest := scopedCase["latestRun"].(map[string]any)
	if latest["runId"] != "run.alpha" || latest["caseRunId"] != "run.alpha.case" || latest["elapsedMs"] != float64(150) {
		t.Fatalf("scoped interface node latest run = %#v", latest)
	}
	topology := latest["topology"].(map[string]any)
	if topology["traceId"] != "trace.alpha" || topology["requestId"] != "request.alpha" || topology["status"] != "complete" || topology["provider"] != "skywalking" {
		t.Fatalf("scoped interface node topology = %#v", topology)
	}
	request := latest["requestSummary"].(map[string]any)
	if request["requestId"] != "request.alpha" || request["stepId"] != "step.alpha" {
		t.Fatalf("scoped request summary = %#v", request)
	}
	runs := scoped["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["runId"] != "run.alpha" {
		t.Fatalf("scoped interface node runs = %#v", runs)
	}

}

func TestServerEvaluatesInterfaceNodeRunTimeoutFromCatalog(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	started := time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)
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
	_, err = s.CreateRun(ctx, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		CreatedAt:  started,
		UpdatedAt:  started.Add(150 * time.Millisecond),
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
		AssertionSummaryJSON: `{"status":"passed"}`,
		StartedAt:            started,
		FinishedAt:           started.Add(150 * time.Millisecond),
		CreatedAt:            started,
	})
	if err != nil {
		t.Fatalf("record case run: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	list := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("interface node list = %#v", list)
	}
	node := items[0].(map[string]any)
	if node["admissionStatus"] != store.StatusFailed || node["latestElapsedMs"] != float64(150) || node["timeoutMs"] != float64(100) {
		t.Fatalf("interface node timeout state = %#v", node)
	}

	detail := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
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

func TestServerExposesInterfaceNodeRunsWithoutFullRunScan(t *testing.T) {
	runtime := interfaceNodeCaseRunCatalogStore{
		catalog: store.ProfileCatalog{
			ProfileID: "sample",
			InterfaceNodes: []store.CatalogInterfaceNode{
				{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Method: "POST", Path: "/alpha", Status: "active"},
			},
			APICases: []store.CatalogAPICase{
				{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
			},
		},
		records: []store.APICaseRunRecord{{
			Run: store.Run{
				ID:           "run.alpha",
				ProfileID:    "sample",
				WorkflowID:   "workflow.alpha",
				Status:       store.StatusPassed,
				EvidenceRoot: ".runtime/evidence/run.alpha",
				CreatedAt:    time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
				UpdatedAt:    time.Date(2026, 5, 15, 9, 0, 1, 0, time.UTC),
			},
			CaseRun: store.APICaseRun{
				ID:                   "run.alpha.case",
				RunID:                "run.alpha",
				CaseID:               "case.alpha",
				Status:               store.StatusPassed,
				RequestSummaryJSON:   `{"method":"POST","path":"/alpha"}`,
				AssertionSummaryJSON: `{"status":"passed"}`,
				StartedAt:            time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
				FinishedAt:           time.Date(2026, 5, 15, 9, 0, 0, 150*int(time.Millisecond), time.UTC),
				CreatedAt:            time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
			},
		}},
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, runtime))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node?id=node.alpha", http.StatusOK)
	history := payload["history"].(map[string]any)
	if history["latestRunId"] != "run.alpha" || history["runCount"] != float64(1) {
		t.Fatalf("interface node history = %#v", history)
	}
	cases := payload["cases"].([]any)
	latest := cases[0].(map[string]any)["latestRun"].(map[string]any)
	if latest["runId"] != "run.alpha" || latest["caseRunId"] != "run.alpha.case" || latest["status"] != store.StatusPassed {
		t.Fatalf("interface node latest run = %#v", latest)
	}
}
