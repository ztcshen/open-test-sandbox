package controlplane_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
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
		catalog: interfaceNodeRunCatalogWithDirectoryPresentation(),
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
