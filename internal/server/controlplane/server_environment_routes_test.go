package controlplane_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerRegistersServiceIntoSandboxStoreWithoutProfileImport(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sandbox.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/sandbox/services", `{
		"id":"service.gateway",
		"displayName":"Gateway",
		"kind":"http",
		"servicePort":18181,
		"healthUrl":"http://127.0.0.1:18181/health",
		"status":"active"
	}`, http.StatusOK)

	if payload["ok"] != true {
		t.Fatalf("service registration payload = %#v", payload)
	}
	service, ok := payload["service"].(map[string]any)
	if !ok || service["id"] != "service.gateway" || service["servicePort"] != float64(18181) {
		t.Fatalf("registered service payload = %#v", payload["service"])
	}
	if payload["storeId"] != "current" {
		t.Fatalf("store identity should be current, got %#v", payload["storeId"])
	}

	catalog, err := s.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get store catalog: %v", err)
	}
	if catalog.ProfileID != "current" || len(catalog.Services) != 1 {
		t.Fatalf("catalog after service registration = %#v", catalog)
	}
	if catalog.Services[0].ID != "service.gateway" || catalog.Services[0].HealthURL != "http://127.0.0.1:18181/health" {
		t.Fatalf("registered catalog service = %#v", catalog.Services[0])
	}

	catalogPayload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	services, ok := catalogPayload["services"].([]any)
	if !ok || len(services) != 1 {
		t.Fatalf("catalog payload services = %#v", catalogPayload["services"])
	}
	first, ok := services[0].(map[string]any)
	if !ok || first["id"] != "service.gateway" {
		t.Fatalf("catalog payload first service = %#v", services[0])
	}
}

func TestServerRegistersInterfaceIntoSandboxStoreWithoutProfileImport(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sandbox.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		Services: []store.CatalogService{
			{ID: "service.gateway", DisplayName: "Gateway", Kind: "http", ServicePort: 18181, Status: "active"},
		},
	}); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/sandbox/interfaces", `{
		"id":"interface.create",
		"displayName":"Create API",
		"serviceId":"service.gateway",
		"operation":"item.create",
		"method":"POST",
		"path":"/v1/items",
		"timeoutMs":1200,
		"requestTemplate":{"id":"template.create","templateJson":{"body":{"name":"{{override:name|demo}}"}}},
		"case":{
			"id":"case.create.default",
			"displayName":"Create default",
			"caseType":"success",
			"requiredForAdmission":true,
			"expectedJson":{"ok":true},
			"timeoutSeconds":5
		},
		"caseExecution":{
			"method":"POST",
			"path":"/v1/items",
			"body":{"name":"{{override:name|demo}}"},
			"expectedHttpCodes":[200]
		}
	}`, http.StatusOK)

	if payload["ok"] != true || payload["storeId"] != "current" {
		t.Fatalf("interface registration payload = %#v", payload)
	}
	view := payload["interface"].(map[string]any)
	if view["id"] != "interface.create" || view["serviceId"] != "service.gateway" || view["caseId"] != "case.create.default" {
		t.Fatalf("interface registration view = %#v", view)
	}

	catalog, err := s.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get catalog: %v", err)
	}
	if len(catalog.Services) != 1 || catalog.Services[0].ID != "service.gateway" {
		t.Fatalf("service registration should be preserved: %#v", catalog.Services)
	}
	if len(catalog.InterfaceNodes) != 1 || catalog.InterfaceNodes[0].ID != "interface.create" || catalog.InterfaceNodes[0].ServiceID != "service.gateway" {
		t.Fatalf("registered interface node = %#v", catalog.InterfaceNodes)
	}
	if len(catalog.RequestTemplates) != 1 || catalog.RequestTemplates[0].ID != "template.create" || catalog.RequestTemplates[0].NodeID != "interface.create" {
		t.Fatalf("registered request templates = %#v", catalog.RequestTemplates)
	}
	if len(catalog.APICases) != 1 || catalog.APICases[0].ID != "case.create.default" || catalog.APICases[0].NodeID != "interface.create" || !catalog.APICases[0].RequiredForAdmission {
		t.Fatalf("registered api cases = %#v", catalog.APICases)
	}
	executionConfigFound := false
	for _, config := range catalog.TemplateConfigs {
		if config.WorkflowID == "" && config.ScopeType == "case" && config.ScopeID == "case.create.default" && strings.Contains(config.ConfigJSON, `"caseExecution"`) {
			executionConfigFound = true
		}
	}
	if !executionConfigFound {
		t.Fatalf("registered execution config = %#v", catalog.TemplateConfigs)
	}

	catalogPayload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	cases, ok := catalogPayload["apiCases"].([]any)
	if !ok || len(cases) != 1 || cases[0].(map[string]any)["id"] != "case.create.default" {
		t.Fatalf("catalog payload cases = %#v", catalogPayload["apiCases"])
	}
	detail := decodeJSONResponse(t, server.URL+"/api/interface-node?id=interface.create", http.StatusOK)
	if detail["ok"] != true || detail["requested"] != "interface.create" {
		t.Fatalf("interface detail payload = %#v", detail)
	}
}

func TestServerManagesVerifiedEnvironmentCatalogFromStore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sandbox.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	missingWorkflow := postJSONResponse(t, server.URL+"/api/environments", `{"id":"env.no-workflow"}`, http.StatusBadRequest)
	if !strings.Contains(fmt.Sprint(missingWorkflow["error"]), "verificationWorkflowId") {
		t.Fatalf("register without verification workflow should be denied: %#v", missingWorkflow)
	}

	registered := postJSONResponse(t, server.URL+"/api/environments", `{
  "id": "env.team.api",
  "displayName": "Team API Environment",
  "description": "Accepted local Docker environment",
  "services": [{"id":"entry-gateway","repo":"../entry-gateway"}],
  "repos": {"entry-gateway":{"url":"../entry-gateway","branch":"main","checkout":"/tmp/entry-gateway"}},
  "compose": {"composeFile":"docker-compose.yml","startCommand":"docker compose up -d"},
  "healthChecks": [{"id":"retail-health","url":"http://127.0.0.1:18080/health"}],
  "verificationWorkflowId": "workflow.core-10"
}`, http.StatusOK)
	env := registered["environment"].(map[string]any)
	if env["id"] != "env.team.api" || env["status"] != "draft" || env["verified"] != false || env["verificationWorkflowId"] != "workflow.core-10" {
		t.Fatalf("registered environment = %#v", env)
	}

	discover := decodeJSONResponse(t, server.URL+"/api/environments", http.StatusOK)
	if discover["count"] != float64(0) {
		t.Fatalf("unverified environment should stay out of default API discovery: %#v", discover)
	}
	discoverAll := decodeJSONResponse(t, server.URL+"/api/environments?all=true", http.StatusOK)
	if discoverAll["count"] != float64(1) {
		t.Fatalf("all API discovery = %#v", discoverAll)
	}

	denied := postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(denied["error"]), "not publishable") {
		t.Fatalf("publish denied payload = %#v", denied)
	}

	verified := postJSONResponse(t, server.URL+"/api/environments/env.team.api/verify", `{
  "runId": "run.core-10",
  "status": "passed",
  "evidenceComplete": true,
  "topologyComplete": true
}`, http.StatusOK)
	verifiedEnv := verified["environment"].(map[string]any)
	if verifiedEnv["status"] != "verified-ready" || verifiedEnv["lastVerificationRunId"] != "run.core-10" || verifiedEnv["evidenceComplete"] != true || verifiedEnv["topologyComplete"] != true {
		t.Fatalf("verified environment = %#v", verifiedEnv)
	}

	now := time.Now().UTC()
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.core-10",
		ProfileID:  "sample",
		WorkflowID: "workflow.core-10",
		Status:     store.StatusPassed,
		StartedAt:  now.Add(-time.Second),
		FinishedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed verification run: %v", err)
	}
	stillDenied := postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(stillDenied["error"]), "no indexed Evidence") {
		t.Fatalf("publish without verification artifacts should be denied: %#v", stillDenied)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         "run.core-10.summary",
		RunID:      "run.core-10",
		Kind:       "summary",
		URI:        "store://verification/run.core-10/summary.json",
		MediaType:  "application/json",
		SHA256:     "verification-summary-sha256",
		SizeBytes:  2,
		Summary:    `{"status":"passed"}`,
		Category:   "verification",
		Visibility: "internal",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("seed verification Evidence: %v", err)
	}
	noTopology := postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(noTopology["error"]), "no complete SkyWalking topology") {
		t.Fatalf("publish without topology should be denied: %#v", noTopology)
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "run.core-10.topology.skywalking",
		WorkflowRunID: "run.core-10",
		WorkflowID:    "workflow.core-10",
		StepID:        "step.core-10",
		CaseID:        "case.core-10",
		RequestID:     "request.core-10",
		TraceID:       "trace.core-10",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","traceId":"trace.core-10","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("seed verification topology: %v", err)
	}
	stillDenied = postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(stillDenied["error"]), "acceptance report") {
		t.Fatalf("publish without workflow acceptance report should be denied: %#v", stillDenied)
	}
	acceptedRunID := "run.core-10.accepted"
	verified = postJSONResponse(t, server.URL+"/api/environments/env.team.api/verify", `{
  "runId": "run.core-10.accepted",
  "status": "passed",
  "evidenceComplete": true,
  "topologyComplete": true
}`, http.StatusOK)
	verifiedEnv = verified["environment"].(map[string]any)
	if verifiedEnv["lastVerificationRunId"] != acceptedRunID {
		t.Fatalf("accepted verification environment = %#v", verifiedEnv)
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         acceptedRunID,
		ProfileID:  "sample",
		WorkflowID: "workflow.core-10",
		Status:     store.StatusPassed,
		SummaryJSON: `{"acceptance":{"templateId":"environment.workflow.skywalking.v1","ok":true,"workflowId":"workflow.core-10",
"expectedSteps":1,"completedSteps":1,"passedSteps":1,"failedSteps":0,"topologyProvider":"skywalking",
"steps":[{"stepId":"step.core-10","caseId":"case.core-10","status":"passed","elapsedMs":12,"evidenceComplete":true,"topologyComplete":true}]}}`,
		StartedAt:  now.Add(-time.Second),
		FinishedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed accepted verification run summary: %v", err)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         acceptedRunID + ".summary",
		RunID:      acceptedRunID,
		Kind:       "summary",
		URI:        "store://verification/" + acceptedRunID + "/summary.json",
		MediaType:  "application/json",
		SHA256:     "verification-summary-sha256",
		SizeBytes:  2,
		Summary:    `{"status":"passed"}`,
		Category:   "verification",
		Visibility: "internal",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("seed accepted verification Evidence: %v", err)
	}
	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            acceptedRunID + ".topology.skywalking",
		WorkflowRunID: acceptedRunID,
		WorkflowID:    "workflow.core-10",
		StepID:        "step.core-10",
		CaseID:        "case.core-10",
		RequestID:     "request.core-10",
		TraceID:       "trace.core-10",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","traceId":"trace.core-10","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("seed accepted verification topology: %v", err)
	}

	published := postJSONResponse(t, server.URL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusOK)
	publishedEnv := published["environment"].(map[string]any)
	if publishedEnv["status"] != "verified" || publishedEnv["verified"] != true {
		t.Fatalf("published environment = %#v", publishedEnv)
	}
	discoverVerified := decodeJSONResponse(t, server.URL+"/api/environments", http.StatusOK)
	if discoverVerified["count"] != float64(1) {
		t.Fatalf("verified API discovery = %#v", discoverVerified)
	}

	bootstrap := decodeJSONResponse(t, server.URL+"/api/environments/env.team.api/bootstrap", http.StatusOK)
	plan := bootstrap["plan"].(map[string]any)
	if plan["verificationWorkflow"] != "workflow.core-10" || len(plan["healthChecks"].([]any)) != 1 {
		t.Fatalf("bootstrap plan = %#v", plan)
	}
	restorePlan := plan["restore"].(map[string]any)
	dockerPlan := restorePlan["docker"].(map[string]any)
	if restorePlan["pauseBeforeHeavyValidation"] != true || dockerPlan["action"] != "docker-compose" || len(dockerPlan["commands"].([]any)) != 3 {
		t.Fatalf("bootstrap restore plan = %#v", restorePlan)
	}
	steps := plan["steps"].([]any)
	if len(steps) != 4 || steps[0].(map[string]any)["kind"] != "repository" || steps[1].(map[string]any)["kind"] != "docker" || steps[3].(map[string]any)["workflowId"] != "workflow.core-10" {
		t.Fatalf("bootstrap executable steps = %#v", steps)
	}

	registeredOptions := postJSONResponse(t, server.URL+"/api/environments", `{
  "id": "env.compose.options.api",
  "compose": {"composeFile":"compose.yml","projectName":"demo","envFiles":[".env.local"],"profiles":["api"],"services":["web"],"skipPull":true,"skipBuild":true},
  "verificationWorkflowId": "workflow.core-10"
}`, http.StatusOK)
	if registeredOptions["ok"] != true {
		t.Fatalf("register compose options environment = %#v", registeredOptions)
	}
	optionsBootstrap := decodeJSONResponse(t, server.URL+"/api/environments/env.compose.options.api/bootstrap", http.StatusOK)
	optionsDocker := optionsBootstrap["plan"].(map[string]any)["restore"].(map[string]any)["docker"].(map[string]any)
	if optionsDocker["projectName"] != "demo" || optionsDocker["skipPull"] != true || optionsDocker["skipBuild"] != true || len(optionsDocker["commands"].([]any)) != 1 {
		t.Fatalf("compose options bootstrap docker plan = %#v", optionsDocker)
	}
}

func TestServerEnvironmentAPIReportsComponentGraphReadiness(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sandbox.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	registered := postJSONResponse(t, server.URL+"/api/environments", `{
  "id": "env.component.api",
  "compose": {"startCommand":"true"},
  "verificationWorkflowId": "workflow.core-10"
}`, http.StatusOK)
	if registered["ok"] != true {
		t.Fatalf("register environment = %#v", registered)
	}
	if err := s.ReplaceEnvironmentComponentGraph(ctx, "env.component.api", store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "db", Kind: "middleware", Role: "database", ComposeService: "db", Required: true, HealthCheckJSON: `{"type":"compose-service"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
			{ComponentID: "app", Kind: "app", Role: "business-service", ComposeService: "app", Required: true, HealthCheckJSON: `{"type":"url","url":"http://127.0.0.1:18080/health"}`, RuntimeJSON: `{}`, SummaryJSON: `{}`},
		},
		Dependencies: []store.ComponentDependency{
			{ConsumerComponentID: "app", ProviderComponentID: "db", Phase: "startup", Capability: "sql", Required: true, ProfileJSON: `{}`},
		},
		Assets: []store.ComponentConfigAsset{
			{OwnerComponentID: "app", AssetID: "app.schema", AssetKind: "mysql-ddl", TargetComponentID: "db", TargetPath: "compose/mysql/init/app.sql", ContentInline: "create database app;\n", ApplyOrder: 10, SummaryJSON: `{}`},
		},
	}); err != nil {
		t.Fatalf("replace component graph: %v", err)
	}

	inspect := decodeJSONResponse(t, server.URL+"/api/environments/env.component.api", http.StatusOK)
	componentGraph := inspect["componentGraph"].(map[string]any)
	if componentGraph["ok"] != true || componentGraph["components"] != float64(2) || componentGraph["blockingDependencies"] != float64(1) || strings.Join(jsonStringSlice(componentGraph["blockingOrder"]), ",") != "db,app" {
		t.Fatalf("inspect component graph readiness = %#v", componentGraph)
	}

	bootstrap := decodeJSONResponse(t, server.URL+"/api/environments/env.component.api/bootstrap", http.StatusOK)
	plan := bootstrap["plan"].(map[string]any)
	planGraph := plan["componentGraph"].(map[string]any)
	restoreGraph := plan["restore"].(map[string]any)["componentGraph"].(map[string]any)
	startupPlan := plan["componentStartupPlan"].(map[string]any)
	restoreStartupPlan := plan["restore"].(map[string]any)["componentStartupPlan"].(map[string]any)
	if planGraph["ok"] != true || strings.Join(jsonStringSlice(planGraph["blockingOrder"]), ",") != "db,app" {
		t.Fatalf("bootstrap component graph readiness = %#v", planGraph)
	}
	if restoreGraph["ok"] != true || strings.Join(jsonStringSlice(restoreGraph["blockingOrder"]), ",") != "db,app" {
		t.Fatalf("bootstrap restore component graph readiness = %#v", restoreGraph)
	}
	batches := startupPlan["batches"].([]any)
	firstBatch := batches[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	secondBatch := batches[1].(map[string]any)["components"].([]any)[0].(map[string]any)
	if startupPlan["ok"] != true || len(batches) != 2 || firstBatch["componentId"] != "db" || secondBatch["componentId"] != "app" || len(startupPlan["healthGates"].([]any)) != 2 {
		t.Fatalf("bootstrap component startup plan = %#v", startupPlan)
	}
	if restoreStartupPlan["ok"] != true {
		t.Fatalf("bootstrap restore component startup plan = %#v", restoreStartupPlan)
	}
}
