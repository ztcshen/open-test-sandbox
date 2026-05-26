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

func openEnvironmentRouteStore(t *testing.T, ctx context.Context) *sqlite.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sandbox.sqlite")
	cfg := sqlite.Config{Path: path}
	s, err := sqlite.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open environment route sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func startEnvironmentRouteServer(t *testing.T, s *sqlite.Store) string {
	t.Helper()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	t.Cleanup(server.Close)
	return server.URL
}

func TestServerRegistersServiceIntoSandboxStoreWithoutProfileImport(t *testing.T) {
	ctx := context.Background()
	s := openEnvironmentRouteStore(t, ctx)
	serverURL := startEnvironmentRouteServer(t, s)

	payload := postJSONResponse(t, serverURL+"/api/sandbox/services", `{
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

	catalogPayload := decodeJSONResponse(t, serverURL+"/api/catalog", http.StatusOK)
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
	s := openEnvironmentRouteStore(t, ctx)
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		Services: []store.CatalogService{
			{ID: "service.gateway", DisplayName: "Gateway", Kind: "http", ServicePort: 18181, Status: "active"},
		},
	}); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	serverURL := startEnvironmentRouteServer(t, s)

	payload := postJSONResponse(t, serverURL+"/api/sandbox/interfaces", `{
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

	catalogPayload := decodeJSONResponse(t, serverURL+"/api/catalog", http.StatusOK)
	cases, ok := catalogPayload["apiCases"].([]any)
	if !ok || len(cases) != 1 || cases[0].(map[string]any)["id"] != "case.create.default" {
		t.Fatalf("catalog payload cases = %#v", catalogPayload["apiCases"])
	}
	detail := decodeJSONResponse(t, serverURL+"/api/interface-node?id=interface.create", http.StatusOK)
	if detail["ok"] != true || detail["requested"] != "interface.create" {
		t.Fatalf("interface detail payload = %#v", detail)
	}
}

func TestServerManagesVerifiedEnvironmentCatalogFromStore(t *testing.T) {
	fixture := newVerifiedEnvironmentCatalogFixture(t)

	requireVerificationWorkflowForEnvironmentRegistration(t, fixture.serverURL)
	registerDraftTeamEnvironment(t, fixture.serverURL)
	requireEnvironmentDiscoveryCount(t, fixture.serverURL, "", 0, "unverified environment should stay out of default API discovery")
	requireEnvironmentDiscoveryCount(t, fixture.serverURL, "?all=true", 1, "all API discovery")
	requirePublishVerifiedDenied(t, fixture.serverURL, "not publishable", "publish denied payload")

	requireEnvironmentVerificationReady(t, fixture.serverURL, "run.core-10")
	fixture.seedVerificationRun(t, "run.core-10", "")
	requirePublishVerifiedDenied(t, fixture.serverURL, "no indexed Evidence", "publish without verification artifacts should be denied")
	fixture.seedVerificationEvidence(t, "run.core-10")
	requirePublishVerifiedDenied(t, fixture.serverURL, "no complete SkyWalking topology", "publish without topology should be denied")
	fixture.seedVerificationTopology(t, "run.core-10")
	requirePublishVerifiedDenied(t, fixture.serverURL, "acceptance report", "publish without workflow acceptance report should be denied")

	acceptedRunID := "run.core-10.accepted"
	requireEnvironmentVerificationRun(t, fixture.serverURL, acceptedRunID)
	fixture.seedAcceptedVerificationRun(t, acceptedRunID)
	fixture.seedVerificationEvidence(t, acceptedRunID)
	fixture.seedVerificationTopology(t, acceptedRunID)

	requirePublishedVerifiedEnvironment(t, fixture.serverURL)
	requireTeamEnvironmentBootstrapPlan(t, fixture.serverURL)
	registerComposeOptionsEnvironment(t, fixture.serverURL)
	requireComposeOptionsBootstrapPlan(t, fixture.serverURL)
}

type verifiedEnvironmentCatalogFixture struct {
	ctx       context.Context
	store     *sqlite.Store
	serverURL string
	now       time.Time
}

func newVerifiedEnvironmentCatalogFixture(t *testing.T) verifiedEnvironmentCatalogFixture {
	t.Helper()
	ctx := context.Background()
	s := openEnvironmentRouteStore(t, ctx)
	return verifiedEnvironmentCatalogFixture{
		ctx:       ctx,
		store:     s,
		serverURL: startEnvironmentRouteServer(t, s),
		now:       time.Now().UTC(),
	}
}

func requireVerificationWorkflowForEnvironmentRegistration(t *testing.T, serverURL string) {
	t.Helper()
	missingWorkflow := postJSONResponse(t, serverURL+"/api/environments", `{"id":"env.no-workflow"}`, http.StatusBadRequest)
	if !strings.Contains(fmt.Sprint(missingWorkflow["error"]), "verificationWorkflowId") {
		t.Fatalf("register without verification workflow should be denied: %#v", missingWorkflow)
	}
}

func registerDraftTeamEnvironment(t *testing.T, serverURL string) {
	t.Helper()
	registered := postJSONResponse(t, serverURL+"/api/environments", `{
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
}

func requireEnvironmentDiscoveryCount(t *testing.T, serverURL string, query string, want float64, message string) {
	t.Helper()
	discover := decodeJSONResponse(t, serverURL+"/api/environments"+query, http.StatusOK)
	if discover["count"] != want {
		t.Fatalf("%s: %#v", message, discover)
	}
}

func requirePublishVerifiedDenied(t *testing.T, serverURL string, errorFragment string, message string) {
	t.Helper()
	denied := postJSONResponse(t, serverURL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusConflict)
	if !strings.Contains(fmt.Sprint(denied["error"]), errorFragment) {
		t.Fatalf("%s: %#v", message, denied)
	}
}

func requireEnvironmentVerificationReady(t *testing.T, serverURL string, runID string) {
	t.Helper()
	verifiedEnv := postPassedEnvironmentVerification(t, serverURL, runID)
	if verifiedEnv["status"] != "verified-ready" || verifiedEnv["lastVerificationRunId"] != runID || verifiedEnv["evidenceComplete"] != true || verifiedEnv["topologyComplete"] != true {
		t.Fatalf("verified environment = %#v", verifiedEnv)
	}
}

func requireEnvironmentVerificationRun(t *testing.T, serverURL string, runID string) map[string]any {
	t.Helper()
	verifiedEnv := postPassedEnvironmentVerification(t, serverURL, runID)
	if verifiedEnv["lastVerificationRunId"] != runID {
		t.Fatalf("accepted verification environment = %#v", verifiedEnv)
	}
	return verifiedEnv
}

func postPassedEnvironmentVerification(t *testing.T, serverURL string, runID string) map[string]any {
	t.Helper()
	verified := postJSONResponse(t, serverURL+"/api/environments/env.team.api/verify", fmt.Sprintf(`{
  "runId": %q,
  "status": "passed",
  "evidenceComplete": true,
  "topologyComplete": true
}`, runID), http.StatusOK)
	return verified["environment"].(map[string]any)
}

func (fixture verifiedEnvironmentCatalogFixture) seedVerificationRun(t *testing.T, runID string, summaryJSON string) {
	t.Helper()
	if _, err := fixture.store.CreateRun(fixture.ctx, store.Run{
		ID:          runID,
		ProfileID:   "sample",
		WorkflowID:  "workflow.core-10",
		Status:      store.StatusPassed,
		SummaryJSON: summaryJSON,
		StartedAt:   fixture.now.Add(-time.Second),
		FinishedAt:  fixture.now,
		CreatedAt:   fixture.now,
		UpdatedAt:   fixture.now,
	}); err != nil {
		t.Fatalf("seed verification run: %v", err)
	}
}

func (fixture verifiedEnvironmentCatalogFixture) seedVerificationEvidence(t *testing.T, runID string) {
	t.Helper()
	if _, err := fixture.store.RecordEvidence(fixture.ctx, store.EvidenceRecord{
		ID:         runID + ".summary",
		RunID:      runID,
		Kind:       "summary",
		URI:        "store://verification/" + runID + "/summary.json",
		MediaType:  "application/json",
		SHA256:     "verification-summary-sha256",
		SizeBytes:  2,
		Summary:    `{"status":"passed"}`,
		Category:   "verification",
		Visibility: "internal",
		CreatedAt:  fixture.now,
	}); err != nil {
		t.Fatalf("seed verification Evidence: %v", err)
	}
}

func (fixture verifiedEnvironmentCatalogFixture) seedVerificationTopology(t *testing.T, runID string) {
	t.Helper()
	if _, err := fixture.store.SaveTraceTopology(fixture.ctx, store.TraceTopology{
		ID:            runID + ".topology.skywalking",
		WorkflowRunID: runID,
		WorkflowID:    "workflow.core-10",
		StepID:        "step.core-10",
		CaseID:        "case.core-10",
		RequestID:     "request.core-10",
		TraceID:       "trace.core-10",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","traceId":"trace.core-10","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     fixture.now,
	}); err != nil {
		t.Fatalf("seed verification topology: %v", err)
	}
}

func (fixture verifiedEnvironmentCatalogFixture) seedAcceptedVerificationRun(t *testing.T, runID string) {
	t.Helper()
	fixture.seedVerificationRun(t, runID, `{"acceptance":{"templateId":"environment.workflow.skywalking.v1","ok":true,"workflowId":"workflow.core-10",
"expectedSteps":1,"completedSteps":1,"passedSteps":1,"failedSteps":0,"topologyProvider":"skywalking",
"steps":[{"stepId":"step.core-10","caseId":"case.core-10","status":"passed","elapsedMs":12,"evidenceComplete":true,"topologyComplete":true}]}}`)
}

func requirePublishedVerifiedEnvironment(t *testing.T, serverURL string) {
	t.Helper()
	published := postJSONResponse(t, serverURL+"/api/environments/env.team.api/publish-verified", `{}`, http.StatusOK)
	publishedEnv := published["environment"].(map[string]any)
	if publishedEnv["status"] != "verified" || publishedEnv["verified"] != true {
		t.Fatalf("published environment = %#v", publishedEnv)
	}
	discoverVerified := decodeJSONResponse(t, serverURL+"/api/environments", http.StatusOK)
	if discoverVerified["count"] != float64(1) {
		t.Fatalf("verified API discovery = %#v", discoverVerified)
	}
}

func requireTeamEnvironmentBootstrapPlan(t *testing.T, serverURL string) {
	t.Helper()
	bootstrap := decodeJSONResponse(t, serverURL+"/api/environments/env.team.api/bootstrap", http.StatusOK)
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
}

func registerComposeOptionsEnvironment(t *testing.T, serverURL string) {
	t.Helper()
	registeredOptions := postJSONResponse(t, serverURL+"/api/environments", `{
  "id": "env.compose.options.api",
  "compose": {"composeFile":"compose.yml","projectName":"demo","envFiles":[".env.local"],"profiles":["api"],"services":["web"],"skipPull":true,"skipBuild":true},
  "verificationWorkflowId": "workflow.core-10"
}`, http.StatusOK)
	if registeredOptions["ok"] != true {
		t.Fatalf("register compose options environment = %#v", registeredOptions)
	}
}

func requireComposeOptionsBootstrapPlan(t *testing.T, serverURL string) {
	t.Helper()
	optionsBootstrap := decodeJSONResponse(t, serverURL+"/api/environments/env.compose.options.api/bootstrap", http.StatusOK)
	optionsDocker := optionsBootstrap["plan"].(map[string]any)["restore"].(map[string]any)["docker"].(map[string]any)
	if optionsDocker["projectName"] != "demo" || optionsDocker["skipPull"] != true || optionsDocker["skipBuild"] != true || len(optionsDocker["commands"].([]any)) != 1 {
		t.Fatalf("compose options bootstrap docker plan = %#v", optionsDocker)
	}
}

func TestServerEnvironmentAPIReportsComponentGraphReadiness(t *testing.T) {
	ctx := context.Background()
	s := openEnvironmentRouteStore(t, ctx)
	serverURL := startEnvironmentRouteServer(t, s)

	registered := postJSONResponse(t, serverURL+"/api/environments", `{
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

	inspect := decodeJSONResponse(t, serverURL+"/api/environments/env.component.api", http.StatusOK)
	componentGraph := inspect["componentGraph"].(map[string]any)
	if componentGraph["ok"] != true || componentGraph["components"] != float64(2) || componentGraph["blockingDependencies"] != float64(1) || strings.Join(jsonStringSlice(componentGraph["blockingOrder"]), ",") != "db,app" {
		t.Fatalf("inspect component graph readiness = %#v", componentGraph)
	}

	bootstrap := decodeJSONResponse(t, serverURL+"/api/environments/env.component.api/bootstrap", http.StatusOK)
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
