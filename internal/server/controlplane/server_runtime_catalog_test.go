package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerUsesRuntimeCatalogForWorkflowDirectory(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	services := []store.CatalogService{
		{ID: "entry-service", DisplayName: "Entry", Kind: "app"},
		{ID: "worker-service", DisplayName: "Worker", Kind: "app"},
	}
	stepServices := []string{"entry-service", "worker-service", "entry-service"}
	nodes := make([]store.CatalogInterfaceNode, 0, len(stepServices))
	cases := make([]store.CatalogAPICase, 0, len(stepServices))
	bindings := make([]store.CatalogWorkflowBinding, 0, len(stepServices))
	for i, serviceID := range stepServices {
		sortOrder := i + 1
		nodeID := fmt.Sprintf("interface.step.%02d", sortOrder)
		caseID := fmt.Sprintf("case.step.%02d", sortOrder)
		nodes = append(nodes, store.CatalogInterfaceNode{
			ID:          nodeID,
			DisplayName: fmt.Sprintf("Step %02d Interface", sortOrder),
			ServiceID:   serviceID,
			Operation:   fmt.Sprintf("step.%02d", sortOrder),
			Method:      "POST",
			Path:        fmt.Sprintf("/steps/%02d", sortOrder),
			Status:      "active",
			TimeoutMs:   sortOrder * 100,
			SortOrder:   sortOrder,
		})
		cases = append(cases, store.CatalogAPICase{
			ID:                   caseID,
			DisplayName:          fmt.Sprintf("Step %02d Case", sortOrder),
			NodeID:               nodeID,
			CaseType:             "happy_path",
			RequiredForAdmission: true,
			Status:               "active",
			SortOrder:            sortOrder,
		})
		bindings = append(bindings, store.CatalogWorkflowBinding{
			WorkflowID: "workflow.primary",
			StepID:     fmt.Sprintf("step-%02d", sortOrder),
			NodeID:     nodeID,
			CaseID:     caseID,
			Required:   true,
			SortOrder:  sortOrder,
		})
	}

	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		Services:  services,
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.primary", DisplayName: "Primary Workflow", Description: "Runs the primary workflow.", BaseStepTimeoutMs: 300, TimeoutOffsetMs: 50},
		},
		InterfaceNodes:   nodes,
		APICases:         cases,
		WorkflowBindings: bindings,
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:          "cfg.workflow.primary",
				TemplateID:  "TPL-WORKFLOW-LONG-CHAIN-V1",
				WorkflowID:  "workflow.primary",
				ScopeType:   "workflow",
				ScopeID:     "workflow.primary",
				Title:       "Primary Workflow",
				Description: "Runs the primary workflow from runtime template configuration.",
				ConfigJSON:  `{"copy":{"runButton":"Run configured flow","coverageTitle":"Configured coverage","coverageEmpty":"No configured mappings."}}`,
				Status:      "needs-business-input",
				SortOrder:   1,
			},
			{
				ID:         "cfg.workflow-step.default",
				TemplateID: "TPL-WORKFLOW-STEP-V1",
				WorkflowID: "workflow.primary",
				ScopeType:  "step",
				ScopeID:    "_default",
				Title:      "Default workflow step presentation",
				ConfigJSON: `{"copy":{"topologyTitle":"Configured topology","requestTitle":"Configured request","responseTitle":"Configured response","logsTitle":"Configured logs","emptyRun":"No configured step run."}}`,
				Status:     "active",
				SortOrder:  0,
			},
			{
				ID:         "cfg.step.one",
				TemplateID: "TPL-WORKFLOW-STEP-V1",
				WorkflowID: "workflow.primary",
				NodeID:     "entry-service",
				ScopeType:  "step",
				ScopeID:    "step-01",
				Title:      "Entry step",
				ConfigJSON: `{"serviceId":"worker-service","evidenceKinds":["request","response","logs"],"relatedMockTargets":["mock-a"],"inputs":[{"name":"order_id","source":"previous","required":false}],"exports":[{"name":"request_id","from":"response","path":"request_id"}],"copy":{"topologyTitle":"Entry topology"}}`,
				Status:     "active",
				SortOrder:  1,
			},
			{
				ID:         "cfg.step.two",
				TemplateID: "TPL-WORKFLOW-STEP-V1",
				WorkflowID: "workflow.primary",
				NodeID:     "entry-service",
				ScopeType:  "step",
				ScopeID:    "step-02",
				Title:      "Worker step",
				Status:     "active",
				SortOrder:  2,
			},
			{
				ID:         "cfg.edge.entry.worker",
				TemplateID: "TPL-ENVIRONMENT-OVERVIEW-V1",
				ScopeType:  "topology-edge",
				ScopeID:    "entry-service->worker-service",
				ConfigJSON: `{"from":"entry-service","to":"worker-service"}`,
				Status:     "active",
				SortOrder:  1,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:          "run.primary",
		ProfileID:   "sample",
		WorkflowID:  "workflow.primary",
		Status:      store.StatusPassed,
		SummaryJSON: `{"status":"passed","steps":[{"stepId":"step-01","elapsedMs":123},{"stepId":"step-02","elapsedMs":456}],"summary":{"stepCount":2,"elapsedMs":579}}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Workflows: []profile.Workflow{
			{ID: "workflow.profile", DisplayName: "Profile Workflow"},
		},
	}, s))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/catalog")
	if err != nil {
		t.Fatalf("get catalog: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("catalog status = %d", resp.StatusCode)
	}

	var payload struct {
		OK          bool              `json:"ok"`
		GeneratedAt string            `json:"generatedAt"`
		Navigation  map[string]any    `json:"navigation"`
		Warnings    []string          `json:"warnings"`
		Source      map[string]string `json:"source"`
		Services    []struct {
			ID string `json:"id"`
		} `json:"services"`
		Workflows []struct {
			ID                string `json:"id"`
			DisplayName       string `json:"displayName"`
			Description       string `json:"description"`
			Entrypoint        string `json:"entrypoint"`
			StepCount         int    `json:"stepCount"`
			CaseCount         int    `json:"caseCount"`
			ServiceCount      int    `json:"serviceCount"`
			BaseStepTimeoutMs int    `json:"baseStepTimeoutMs"`
			TimeoutOffsetMs   int    `json:"timeoutOffsetMs"`
			TimeoutMs         int    `json:"timeoutMs"`
			Graph             struct {
				Nodes []string `json:"nodes"`
				Edges []struct {
					From string `json:"from"`
					To   string `json:"to"`
				} `json:"edges"`
			} `json:"graph"`
			Observability struct {
				Panels []struct {
					ID string `json:"id"`
				} `json:"panels"`
			} `json:"observability"`
			Presentation struct {
				Template string `json:"template"`
				Title    string `json:"title"`
				Copy     struct {
					RunButton     string `json:"runButton"`
					CoverageTitle string `json:"coverageTitle"`
					CoverageEmpty string `json:"coverageEmpty"`
				} `json:"copy"`
				Stages []struct {
					ID    string `json:"id"`
					Steps []struct {
						ID    string `json:"id"`
						Title string `json:"title"`
					} `json:"steps"`
				} `json:"stages"`
			} `json:"presentation"`
			RunCount  int `json:"runCount"`
			LatestRun struct {
				ID      string `json:"id"`
				Summary struct {
					Steps []struct {
						StepID    string `json:"stepId"`
						ElapsedMs int    `json:"elapsedMs"`
					} `json:"steps"`
				} `json:"summary"`
			} `json:"latestRun"`
			Steps []struct {
				ID                 string           `json:"id"`
				DisplayName        string           `json:"displayName"`
				ServiceID          string           `json:"serviceId"`
				CaseID             string           `json:"caseId"`
				Required           bool             `json:"required"`
				Executable         bool             `json:"executable"`
				EvidenceKinds      []string         `json:"evidenceKinds"`
				RelatedMockTargets []string         `json:"relatedMockTargets"`
				Inputs             []map[string]any `json:"inputs"`
				Exports            []map[string]any `json:"exports"`
				TimeoutMs          int              `json:"timeoutMs"`
				Presentation       struct {
					Copy struct {
						TopologyTitle string `json:"topologyTitle"`
						RequestTitle  string `json:"requestTitle"`
						ResponseTitle string `json:"responseTitle"`
						LogsTitle     string `json:"logsTitle"`
						EmptyRun      string `json:"emptyRun"`
					} `json:"copy"`
				} `json:"presentation"`
			} `json:"steps"`
		} `json:"workflows"`
		APICases []struct {
			ID     string `json:"id"`
			NodeID string `json:"nodeId"`
		} `json:"apiCases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if !payload.OK || payload.GeneratedAt == "" || payload.Navigation == nil || payload.Warnings == nil {
		t.Fatalf("catalog envelope = %#v", payload)
	}
	if payload.Source["kind"] != "store" || payload.Source["id"] != "sample" {
		t.Fatalf("catalog source = %#v", payload.Source)
	}
	if len(payload.Services) != len(services) || len(payload.APICases) != len(cases) {
		t.Fatalf("catalog inventory counts services=%d cases=%d", len(payload.Services), len(payload.APICases))
	}
	if len(payload.Workflows) != 1 {
		t.Fatalf("workflow count = %d, payload=%#v", len(payload.Workflows), payload.Workflows)
	}
	workflow := payload.Workflows[0]
	if workflow.ID != "workflow.primary" {
		t.Fatalf("workflow id = %q", workflow.ID)
	}
	if workflow.DisplayName != "Primary Workflow" || workflow.Description == "" {
		t.Fatalf("workflow metadata = %#v", workflow)
	}
	if workflow.StepCount != len(bindings) || workflow.CaseCount != len(bindings) || workflow.ServiceCount != 2 {
		t.Fatalf("workflow summary counts = %#v", workflow)
	}
	if workflow.BaseStepTimeoutMs != 300 || workflow.TimeoutOffsetMs != 50 || workflow.TimeoutMs != 650 {
		t.Fatalf("workflow timeout budget = %#v", workflow)
	}
	if workflow.Entrypoint != "/workflow-detail.html?id=workflow.primary" {
		t.Fatalf("workflow entrypoint = %q", workflow.Entrypoint)
	}
	if len(workflow.Graph.Nodes) != 2 || len(workflow.Graph.Edges) != 1 || workflow.Graph.Edges[0].From != "entry-service" || workflow.Graph.Edges[0].To != "worker-service" {
		t.Fatalf("workflow graph = %#v", workflow.Graph)
	}
	if len(workflow.Observability.Panels) != 5 || workflow.Observability.Panels[0].ID != "workflowGraph" {
		t.Fatalf("workflow observability = %#v", workflow.Observability)
	}
	if workflow.Presentation.Template != "workflowStudio" || workflow.Presentation.Title != "Primary Workflow" || len(workflow.Presentation.Stages) != 1 || workflow.Presentation.Stages[0].Steps[0].Title != "Entry step" {
		t.Fatalf("workflow presentation = %#v", workflow.Presentation)
	}
	if workflow.Presentation.Copy.RunButton != "Run configured flow" || workflow.Presentation.Copy.CoverageTitle != "Configured coverage" || workflow.Presentation.Copy.CoverageEmpty != "No configured mappings." {
		t.Fatalf("workflow presentation copy = %#v", workflow.Presentation.Copy)
	}
	if workflow.RunCount != 1 || workflow.LatestRun.ID != "run.primary" || len(workflow.LatestRun.Summary.Steps) != 0 {
		t.Fatalf("workflow run state = %#v", workflow)
	}
	if len(workflow.Steps) != len(bindings) {
		t.Fatalf("workflow step count = %d", len(workflow.Steps))
	}
	for i, step := range workflow.Steps {
		wantStep := fmt.Sprintf("step-%02d", i+1)
		wantCase := fmt.Sprintf("case.step.%02d", i+1)
		wantService := stepServices[i]
		if i == 0 {
			wantService = "worker-service"
		}
		if i == 1 {
			wantService = "entry-service"
		}
		if step.ID != wantStep || step.CaseID != wantCase || step.ServiceID != wantService || !step.Required {
			t.Fatalf("step %d = %#v", i, step)
		}
		if step.TimeoutMs != (i+1)*100 {
			t.Fatalf("step timeout %d = %#v", i, step)
		}
		if i == 0 && step.DisplayName != "Entry step" {
			t.Fatalf("step template title = %#v", step)
		}
		if i == 0 {
			if !step.Executable || len(step.EvidenceKinds) != 3 || step.EvidenceKinds[0] != "request" || len(step.RelatedMockTargets) != 1 {
				t.Fatalf("step runtime metadata = %#v", step)
			}
			if len(step.Inputs) != 1 || step.Inputs[0]["name"] != "order_id" || len(step.Exports) != 1 || step.Exports[0]["name"] != "request_id" {
				t.Fatalf("step inputs/exports = %#v", step)
			}
			if step.Presentation.Copy.TopologyTitle != "Entry topology" ||
				step.Presentation.Copy.RequestTitle != "Configured request" ||
				step.Presentation.Copy.ResponseTitle != "Configured response" ||
				step.Presentation.Copy.LogsTitle != "Configured logs" ||
				step.Presentation.Copy.EmptyRun != "No configured step run." {
				t.Fatalf("step presentation copy = %#v", step.Presentation.Copy)
			}
		}
	}
}

func TestServerPersistsBatchCaseRunsForInterfaceNodeGreenState(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "interface.alpha", DisplayName: "Alpha", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha.one", DisplayName: "Alpha one", NodeID: "interface.alpha", CaseType: "success", RequiredForAdmission: true, Status: "active"},
			{ID: "case.alpha.two", DisplayName: "Alpha two", NodeID: "interface.alpha", CaseType: "success", RequiredForAdmission: true, Status: "active"},
			{ID: "case.alpha.optional", DisplayName: "Alpha optional", NodeID: "interface.alpha", CaseType: "success", RequiredForAdmission: false, Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{ID: "cfg.one", ScopeType: "step", ScopeID: "one", Status: "active", ConfigJSON: `{"caseId":"case.alpha.one","caseExecution":{"method":"GET","nodeId":"service.alpha","path":"/ok","expectedHttpCodes":[200]}}`},
			{ID: "cfg.two", ScopeType: "step", ScopeID: "two", Status: "active", ConfigJSON: `{"caseId":"case.alpha.two","caseExecution":{"method":"GET","nodeId":"service.alpha","path":"/ok","expectedHttpCodes":[200]}}`},
			{ID: "cfg.optional", ScopeType: "step", ScopeID: "optional", Status: "active", ConfigJSON: `{"caseId":"case.alpha.optional","caseExecution":{"method":"GET","nodeId":"service.alpha","path":"/ok","expectedHttpCodes":[200]}}`},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/test-kit/run-batch", "application/json", strings.NewReader(fmt.Sprintf(`{"caseIds":["case.alpha.one","case.alpha.two","case.alpha.optional"],"baseUrl":%q}`, target.URL)))
	if err != nil {
		t.Fatalf("post batch: %v", err)
	}
	var batchPayload struct {
		Results []struct {
			RunID     string `json:"runId"`
			CaseRunID string `json:"caseRunId"`
			DetailURL string `json:"detailUrl"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batchPayload); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close batch response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch status = %d", resp.StatusCode)
	}
	if len(batchPayload.Results) != 3 {
		t.Fatalf("batch results = %#v", batchPayload.Results)
	}
	for _, item := range batchPayload.Results {
		if item.RunID == "" || item.CaseRunID != item.RunID+".case" || item.DetailURL != "/api/case-run/evidence?caseRunId="+url.QueryEscape(item.CaseRunID) {
			t.Fatalf("batch case evidence handles = %#v", item)
		}
	}
	detail := decodeJSONResponse(t, server.URL+batchPayload.Results[0].DetailURL, http.StatusOK)
	if detail["ok"] != true {
		t.Fatalf("batch case detail lookup = %#v", detail)
	}

	resp, err = http.Get(server.URL + "/api/interface-node?id=interface.alpha")
	if err != nil {
		t.Fatalf("get interface node: %v", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Admission struct {
			Status          string `json:"status"`
			PassedCaseCount int    `json:"passedCaseCount"`
		} `json:"admission"`
		Cases []struct {
			ID        string         `json:"id"`
			LatestRun map[string]any `json:"latestRun"`
		} `json:"cases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode interface node: %v", err)
	}
	if payload.Admission.Status != store.StatusPassed || payload.Admission.PassedCaseCount != 2 {
		t.Fatalf("admission = %#v", payload.Admission)
	}
	for _, item := range payload.Cases {
		if item.LatestRun["status"] != store.StatusPassed {
			t.Fatalf("case %s latest run = %#v", item.ID, item.LatestRun)
		}
	}

	list := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	if list["ok"] != true || list["templateId"] != "TPL-INTERFACE-NODE-CASE-LIST-V1" {
		t.Fatalf("interface node list envelope = %#v", list)
	}
	filters := list["filters"].(map[string]any)
	if filters["serviceId"] != "" || filters["operation"] != "" {
		t.Fatalf("interface node list filters = %#v", filters)
	}
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("interface node list = %#v", list)
	}
	row := items[0].(map[string]any)
	if row["status"] != "active" || row["admissionStatus"] != store.StatusPassed || row["passedCaseCount"] != float64(2) {
		t.Fatalf("interface node list row = %#v", row)
	}
	if row["operation"] != "Alpha" || row["latestRunId"] == "" {
		t.Fatalf("interface node list row = %#v", row)
	}
}

func TestServerUsesLatestPassingCacheForDirectInterfaceAdmission(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "interface.alpha", DisplayName: "Alpha", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Alpha", NodeID: "interface.alpha", CaseType: "success", RequiredForAdmission: true, Status: "active"},
			{ID: "case.alpha.optional", DisplayName: "Alpha optional", NodeID: "interface.alpha", CaseType: "failure", RequiredForAdmission: false, Status: "active"},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	for _, run := range []store.Run{
		{ID: "run.pass", ProfileID: "sample", WorkflowID: "workflow.alpha", Status: store.StatusPassed, StartedAt: now, FinishedAt: now.Add(100 * time.Millisecond), CreatedAt: now, UpdatedAt: now},
		{ID: "run.fail", ProfileID: "sample", WorkflowID: "case.alpha", Status: store.StatusFailed, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute + 200*time.Millisecond), CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)},
	} {
		if _, err := s.CreateRun(ctx, run); err != nil {
			t.Fatalf("create run %s: %v", run.ID, err)
		}
	}
	for _, item := range []store.APICaseRun{
		{ID: "run.pass.case", RunID: "run.pass", CaseID: "case.alpha", Status: store.StatusPassed, AssertionSummaryJSON: `{"status":"passed"}`, StartedAt: now, FinishedAt: now.Add(100 * time.Millisecond), CreatedAt: now},
		{ID: "run.fail.case", RunID: "run.fail", CaseID: "case.alpha", Status: store.StatusFailed, AssertionSummaryJSON: `{"status":"failed"}`, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute + 200*time.Millisecond), CreatedAt: now.Add(time.Minute)},
		{ID: "run.fail.optional", RunID: "run.fail", CaseID: "case.alpha.optional", Status: store.StatusFailed, AssertionSummaryJSON: `{"status":"failed"}`, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute + 300*time.Millisecond), CreatedAt: now.Add(time.Minute)},
	} {
		if _, err := s.RecordAPICaseRun(ctx, item); err != nil {
			t.Fatalf("record case run %s: %v", item.ID, err)
		}
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	list := decodeJSONResponse(t, server.URL+"/api/interface-nodes", http.StatusOK)
	row := list["items"].([]any)[0].(map[string]any)
	if row["admissionStatus"] != store.StatusPassed || row["latestRunId"] != "run.pass" || row["latestElapsedMs"] != float64(100) {
		t.Fatalf("interface list should prefer cached pass: %#v", row)
	}
	detail := decodeJSONResponse(t, server.URL+"/api/interface-node?id=interface.alpha", http.StatusOK)
	admission := detail["admission"].(map[string]any)
	if admission["status"] != store.StatusPassed || admission["latestRunId"] != "run.pass" {
		t.Fatalf("interface detail should prefer cached pass: %#v", admission)
	}
}

func TestServerExplainsInterfaceAdmissionBlockersFromStoreRuns(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "interface.blocked", DisplayName: "Blocked", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.passed", DisplayName: "Passed case", NodeID: "interface.blocked", CaseType: "success", RequiredForAdmission: true, Status: "active", SortOrder: 1},
			{ID: "case.failed", DisplayName: "Failed case", NodeID: "interface.blocked", CaseType: "success", RequiredForAdmission: true, Status: "active", SortOrder: 2},
			{ID: "case.missing", DisplayName: "Missing case", NodeID: "interface.blocked", CaseType: "failure", RequiredForAdmission: true, Status: "active", SortOrder: 3},
			{ID: "case.optional", DisplayName: "Optional case", NodeID: "interface.blocked", CaseType: "failure", RequiredForAdmission: false, Status: "active", SortOrder: 4},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{ID: "run.blocked", ProfileID: "sample", WorkflowID: "workflow.blocked", Status: store.StatusFailed, SummaryJSON: `{}`})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	for _, item := range []store.APICaseRun{
		{ID: "run.blocked.case.passed", RunID: "run.blocked", CaseID: "case.passed", Status: store.StatusPassed, AssertionSummaryJSON: `{"status":"passed"}`},
		{ID: "run.blocked.case.failed", RunID: "run.blocked", CaseID: "case.failed", Status: store.StatusFailed, AssertionSummaryJSON: `{"status":"failed","errorCount":1,"failureKind":"assertion"}`},
		{ID: "run.blocked.case.optional", RunID: "run.blocked", CaseID: "case.optional", Status: store.StatusFailed, AssertionSummaryJSON: `{"status":"failed","errorCount":1}`},
	} {
		if _, err := s.RecordAPICaseRun(ctx, item); err != nil {
			t.Fatalf("record api case run: %v", err)
		}
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/interface-node?id=interface.blocked", http.StatusOK)
	admission := payload["admission"].(map[string]any)
	if admission["status"] != store.StatusFailed || admission["requiredCaseCount"] != float64(3) || admission["passedCaseCount"] != float64(1) {
		t.Fatalf("admission = %#v", admission)
	}
	blockers := admission["blockers"].([]any)
	if len(blockers) != 2 {
		t.Fatalf("blockers = %#v", admission)
	}
	failed := blockers[0].(map[string]any)
	missing := blockers[1].(map[string]any)
	if failed["caseId"] != "case.failed" || failed["status"] != store.StatusFailed || failed["runId"] != "run.blocked" || failed["failureReason"] != "assertion errors: 1" || failed["failureKind"] != "assertion" || failed["evidenceHref"] != "/evidence-viewer.html?caseRun=run.blocked&caseId=case.failed" {
		t.Fatalf("failed blocker = %#v", failed)
	}
	if missing["caseId"] != "case.missing" || missing["status"] != "missing_run" || missing["failureReason"] != "required case has no run" {
		t.Fatalf("missing blocker = %#v", missing)
	}
	attention := payload["attention"].(map[string]any)
	if attention["status"] != store.StatusFailed || attention["blockerCount"] != float64(2) {
		t.Fatalf("attention = %#v", attention)
	}
}

func TestServerDoesNotServeLegacyTopLevelScripts(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	for _, path := range []string{"/dashboard.js", "/workflows.js"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("close %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, resp.StatusCode)
		}
	}
}
