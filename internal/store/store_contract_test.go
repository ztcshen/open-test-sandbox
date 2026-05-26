package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestSQLiteStoreContract(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")

	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	exerciseStoreContract(t, ctx, s)
}

func TestSQLiteStoreUsesDefaultPathWhenURLIsEmpty(t *testing.T) {
	cfg := sqlite.Config{BaseDir: t.TempDir()}

	resolved := cfg.Resolve()

	if resolved.Path != filepath.Join(cfg.BaseDir, "runtime", "store.sqlite") {
		t.Fatalf("default sqlite path = %q", resolved.Path)
	}
}

func TestSQLiteStoreCanBeDisabledForPostgresOnlyValidation(t *testing.T) {
	t.Setenv("AGENT_TESTBENCH_DISABLE_SQLITE_STORE", "1")

	_, err := sqlite.Open(context.Background(), sqlite.Config{Path: filepath.Join(t.TempDir(), "store.sqlite")})
	if err == nil {
		t.Fatal("expected sqlite store open to fail when disabled")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "SQLite Store is disabled") {
		t.Fatalf("sqlite disabled error = %q", got)
	}
}

const (
	contractProfileID  = "empty"
	contractRunID      = "run-001"
	contractWorkflowID = "workflow.smoke"
	contractCaseRunID  = "case-run-001"
	contractCaseID     = "case.health"
	contractStepID     = "step.health"
)

func exerciseStoreContract(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()

	started := time.Date(2026, 5, 14, 9, 30, 0, 0, time.UTC)
	requireRunContract(t, ctx, s, started)
	requireAPICaseRunContract(t, ctx, s, started)
	requireEvidenceContract(t, ctx, s)
	requirePostProcessTaskContract(t, ctx, s)
	requireTraceTopologyContract(t, ctx, s, started)
	env := requireEnvironmentContract(t, ctx, s, started)
	requireEnvironmentComponentGraphContract(t, ctx, s, env.ID)
	requireBaselineGateContract(t, ctx, s, started)
	requireProfileIndexContract(t, ctx, s, started)
	activeVersion := requireConfigVersionContract(t, ctx, s, started)
	requireReadModelContract(t, ctx, s, activeVersion, started)
	requireProfileCatalogContract(t, ctx, s, started)
	requireMissingRunError(t, ctx, s)
}

func requireRunContract(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()

	run, err := s.CreateRun(ctx, store.Run{
		ID:           contractRunID,
		ProfileID:    contractProfileID,
		WorkflowID:   contractWorkflowID,
		Status:       store.StatusRunning,
		EvidenceRoot: "evidence/run-001",
		SummaryJSON:  `{"stepCount":1}`,
		StartedAt:    started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.CreatedAt.IsZero() {
		t.Fatalf("created run should have CreatedAt: %#v", run)
	}

	loadedRun, err := s.GetRun(ctx, contractRunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if loadedRun.ProfileID != contractProfileID || loadedRun.WorkflowID != contractWorkflowID || loadedRun.Status != store.StatusRunning {
		t.Fatalf("loaded run = %#v", loadedRun)
	}
	if loadedRun.SummaryJSON != `{"stepCount":1}` {
		t.Fatalf("loaded run summary = %q", loadedRun.SummaryJSON)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != contractRunID {
		t.Fatalf("runs = %#v", runs)
	}
	if runs[0].SummaryJSON != `{"stepCount":1}` {
		t.Fatalf("listed run summary = %q", runs[0].SummaryJSON)
	}
}

func requireAPICaseRunContract(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()

	caseRun, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   contractCaseRunID,
		RunID:                contractRunID,
		CaseID:               contractCaseID,
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"GET"}`,
		AssertionSummaryJSON: `{"passed":1}`,
		StartedAt:            started,
		FinishedAt:           started.Add(250 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	if caseRun.CreatedAt.IsZero() {
		t.Fatalf("case run should have CreatedAt: %#v", caseRun)
	}

	caseRuns, err := s.ListAPICaseRuns(ctx, contractRunID)
	if err != nil {
		t.Fatalf("list case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != contractCaseID || caseRuns[0].Status != store.StatusPassed {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	if caseRuns[0].RequestSummaryJSON != `{"method":"GET"}` || caseRuns[0].AssertionSummaryJSON != `{"passed":1}` {
		t.Fatalf("case run summaries = %#v", caseRuns[0])
	}
	latestCaseStore, ok := s.(interface {
		ListLatestAPICaseRuns(context.Context) ([]store.APICaseRun, error)
	})
	if ok {
		latestCaseRuns, err := latestCaseStore.ListLatestAPICaseRuns(ctx)
		if err != nil {
			t.Fatalf("list latest api case runs: %v", err)
		}
		if len(latestCaseRuns) != 1 || latestCaseRuns[0].ID != contractCaseRunID || latestCaseRuns[0].CaseID != contractCaseID {
			t.Fatalf("latest case runs = %#v", latestCaseRuns)
		}
	}
}

func requireEvidenceContract(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()

	evidence, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         "evidence-001",
		RunID:      contractRunID,
		CaseRunID:  contractCaseRunID,
		StepID:     contractStepID,
		Kind:       "http-response",
		URI:        "evidence/run-001/response.json",
		MediaType:  "application/json",
		SHA256:     "abc123",
		SizeBytes:  42,
		Summary:    "response body",
		Category:   "runtime-attachment",
		Visibility: "public",
		LabelsJSON: `{"owner":"qa","severity":"critical"}`,
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}
	if evidence.CreatedAt.IsZero() {
		t.Fatalf("evidence should have CreatedAt: %#v", evidence)
	}

	evidenceRecords, err := s.ListEvidence(ctx, contractRunID)
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidenceRecords) != 1 || evidenceRecords[0].URI != "evidence/run-001/response.json" {
		t.Fatalf("evidence records = %#v", evidenceRecords)
	}
	if evidenceRecords[0].Kind != "http-response" || evidenceRecords[0].MediaType != "application/json" || evidenceRecords[0].SHA256 != "abc123" || evidenceRecords[0].SizeBytes != 42 || evidenceRecords[0].Summary != "response body" {
		t.Fatalf("evidence metadata = %#v", evidenceRecords[0])
	}
	if evidenceRecords[0].Category != "runtime-attachment" || evidenceRecords[0].Visibility != "public" || evidenceRecords[0].LabelsJSON != `{"owner":"qa","severity":"critical"}` {
		t.Fatalf("evidence attachment metadata = %#v", evidenceRecords[0])
	}
	if evidenceRecords[0].StepID != contractStepID {
		t.Fatalf("evidence step relation = %#v", evidenceRecords[0])
	}
}

func requirePostProcessTaskContract(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()

	taskStarted := time.Now().UTC().Add(-150 * time.Millisecond)
	taskFinished := taskStarted.Add(125 * time.Millisecond)
	task, err := s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:          "task-001",
		RunID:       contractRunID,
		WorkflowID:  "workflow.health",
		StepID:      contractStepID,
		CaseID:      contractCaseID,
		Kind:        "runtime_log_collect",
		Status:      store.StatusPassed,
		StartedAt:   taskStarted,
		FinishedAt:  taskFinished,
		SummaryJSON: `{"systems":2}`,
	})
	if err != nil {
		t.Fatalf("record post process task: %v", err)
	}
	if task.DurationMs != 125 {
		t.Fatalf("task duration should be derived from timestamps: %#v", task)
	}
	tasks, err := s.ListPostProcessTasks(ctx, contractRunID)
	if err != nil {
		t.Fatalf("list post process tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Kind != "runtime_log_collect" || tasks[0].DurationMs != 125 {
		t.Fatalf("post process tasks = %#v", tasks)
	}
}

func requireTraceTopologyContract(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()

	topology, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology-001",
		WorkflowRunID: contractRunID,
		WorkflowID:    contractWorkflowID,
		StepID:        contractStepID,
		CaseID:        contractCaseID,
		RequestID:     "request-001",
		TraceID:       "trace-001",
		Status:        "complete",
		TopologyJSON:  `{"confirmedEdges":[{"source":"service.alpha","target":"service.beta"}],"observedNodes":["service.alpha","service.beta"]}`,
		TextTopology:  "service.alpha -> service.beta",
		CreatedAt:     started.Add(500 * time.Millisecond),
	})
	if err != nil {
		t.Fatalf("save trace topology: %v", err)
	}
	if topology.CreatedAt.IsZero() {
		t.Fatalf("trace topology should have CreatedAt: %#v", topology)
	}
	topologies, err := s.ListTraceTopologies(ctx, contractRunID)
	if err != nil {
		t.Fatalf("list trace topologies: %v", err)
	}
	if len(topologies) != 1 || topologies[0].TraceID != "trace-001" || topologies[0].Status != "complete" {
		t.Fatalf("trace topologies = %#v", topologies)
	}
	if topologies[0].TopologyJSON != topology.TopologyJSON || topologies[0].TextTopology != "service.alpha -> service.beta" {
		t.Fatalf("trace topology payload = %#v", topologies[0])
	}
}

func requireEnvironmentContract(t *testing.T, ctx context.Context, s store.Store, started time.Time) store.Environment {
	t.Helper()

	env, err := s.UpsertEnvironment(ctx, store.Environment{
		ID:                     "env.team.accepted",
		DisplayName:            "Team Accepted Environment",
		Description:            "Shared environment accepted by verification workflow",
		Status:                 "draft",
		ServicesJSON:           `[{"id":"service.alpha","repo":"../service-alpha"}]`,
		ReposJSON:              `{"service.alpha":{"url":"../service-alpha","branch":"main"}}`,
		ComposeJSON:            `{"composeFile":"docker-compose.yml","startCommand":"docker compose up -d"}`,
		HealthChecksJSON:       `[{"id":"alpha-health","url":"http://127.0.0.1:18080/health"}]`,
		VerificationWorkflowID: "workflow.smoke",
		SummaryJSON:            `{"owner":"team"}`,
	})
	if err != nil {
		t.Fatalf("upsert environment: %v", err)
	}
	if env.CreatedAt.IsZero() || env.UpdatedAt.IsZero() {
		t.Fatalf("environment timestamps should be set: %#v", env)
	}
	env.LastVerificationRunID = contractRunID
	env.LastVerificationStatus = store.StatusPassed
	env.EvidenceComplete = true
	env.TopologyComplete = true
	env.Verified = true
	env.Status = "verified"
	env.LastVerifiedAt = started.Add(time.Minute)
	env, err = s.UpsertEnvironment(ctx, env)
	if err != nil {
		t.Fatalf("update environment verification: %v", err)
	}
	loadedEnv, err := s.GetEnvironment(ctx, "env.team.accepted")
	if err != nil {
		t.Fatalf("get environment: %v", err)
	}
	if !loadedEnv.Verified || loadedEnv.LastVerificationStatus != store.StatusPassed || !loadedEnv.EvidenceComplete || !loadedEnv.TopologyComplete {
		t.Fatalf("loaded environment verification = %#v", loadedEnv)
	}
	if !jsonEqual(loadedEnv.ReposJSON, env.ReposJSON) || loadedEnv.VerificationWorkflowID != "workflow.smoke" {
		t.Fatalf("loaded environment catalog fields = %#v", loadedEnv)
	}
	environments, err := s.ListEnvironments(ctx)
	if err != nil {
		t.Fatalf("list environments: %v", err)
	}
	if len(environments) != 1 || environments[0].ID != "env.team.accepted" || !environments[0].Verified {
		t.Fatalf("environments = %#v", environments)
	}
	return env
}

func requireEnvironmentComponentGraphContract(t *testing.T, ctx context.Context, s store.Store, envID string) {
	t.Helper()

	graph := contractEnvironmentComponentGraph()
	if err := s.ReplaceEnvironmentComponentGraph(ctx, envID, graph); err != nil {
		t.Fatalf("replace environment component graph: %v", err)
	}
	loadedGraph, err := s.GetEnvironmentComponentGraph(ctx, envID)
	if err != nil {
		t.Fatalf("get environment component graph: %v", err)
	}
	if len(loadedGraph.Components) != 2 || len(loadedGraph.Dependencies) != 1 || len(loadedGraph.Assets) != 1 {
		t.Fatalf("loaded component graph = %#v", loadedGraph)
	}
	if loadedGraph.Dependencies[0].ConsumerComponentID != "service.alpha" || loadedGraph.Dependencies[0].ProviderComponentID != "mysql" || loadedGraph.Dependencies[0].Phase != "startup" {
		t.Fatalf("loaded component dependency = %#v", loadedGraph.Dependencies[0])
	}
	if loadedGraph.Assets[0].OwnerComponentID != "service.alpha" || loadedGraph.Assets[0].TargetComponentID != "mysql" || !jsonEqual(loadedGraph.Assets[0].SummaryJSON, graph.Assets[0].SummaryJSON) {
		t.Fatalf("loaded component asset = %#v", loadedGraph.Assets[0])
	}
}

func contractEnvironmentComponentGraph() store.EnvironmentComponentGraph {
	return store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{
				ComponentID:     "mysql",
				DisplayName:     "MySQL",
				Kind:            "middleware",
				Role:            "database",
				ComposeService:  "mysql",
				Image:           "mysql:8",
				Required:        true,
				RuntimeJSON:     `{"ports":[3306]}`,
				HealthCheckJSON: `{"type":"tcp","address":"127.0.0.1:3306"}`,
				SummaryJSON:     `{}`,
			},
			{
				ComponentID:     "service.alpha",
				DisplayName:     "Service Alpha",
				Kind:            "app",
				Role:            "business-service",
				ComposeService:  "service-alpha",
				Required:        true,
				RuntimeJSON:     `{}`,
				HealthCheckJSON: `{"type":"url","url":"http://127.0.0.1:18080/health"}`,
				SummaryJSON:     `{}`,
			},
		},
		Dependencies: []store.ComponentDependency{
			{
				ConsumerComponentID: "service.alpha",
				ProviderComponentID: "mysql",
				Phase:               "startup",
				Capability:          "sql",
				Required:            true,
				ProfileJSON:         `{"database":"alpha"}`,
			},
		},
		Assets: []store.ComponentConfigAsset{
			{
				OwnerComponentID:  "service.alpha",
				AssetID:           "alpha.mysql.ddl",
				AssetKind:         "mysql-ddl",
				TargetComponentID: "mysql",
				TargetPath:        "compose/mysql/init/alpha.sql",
				ContentInline:     "create table alpha_smoke (id bigint primary key);",
				SizeBytes:         int64(len("create table alpha_smoke (id bigint primary key);")),
				ApplyOrder:        10,
				SummaryJSON:       `{"ownedBy":"service.alpha"}`,
			},
		},
	}
}

func requireBaselineGateContract(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()

	gate, err := s.UpsertBaselineGate(ctx, store.BaselineGate{
		ProfileID:   contractProfileID,
		SubjectID:   contractWorkflowID,
		Status:      store.StatusPassed,
		Required:    false,
		SummaryJSON: `{"reason":"first green run"}`,
		CheckedAt:   started.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("upsert baseline gate: %v", err)
	}
	if gate.UpdatedAt.IsZero() {
		t.Fatalf("baseline gate should have UpdatedAt: %#v", gate)
	}

	loadedGate, err := s.GetBaselineGate(ctx, contractProfileID, contractWorkflowID)
	if err != nil {
		t.Fatalf("get baseline gate: %v", err)
	}
	if loadedGate.Status != store.StatusPassed || loadedGate.Required {
		t.Fatalf("loaded baseline gate = %#v", loadedGate)
	}
}

func requireProfileIndexContract(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()

	profile, err := s.UpsertProfileIndex(ctx, store.ProfileIndex{
		ProfileID:    contractProfileID,
		BundlePath:   "/tmp/external-profile-bundles/empty",
		BundleDigest: "sha256:bundle",
		SummaryJSON:  `{"workflows":0}`,
		ImportedAt:   started.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("upsert profile index: %v", err)
	}
	if profile.UpdatedAt.IsZero() {
		t.Fatalf("profile index should have UpdatedAt: %#v", profile)
	}

	loadedProfile, err := s.GetProfileIndex(ctx, contractProfileID)
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if loadedProfile.BundlePath != "/tmp/external-profile-bundles/empty" || loadedProfile.BundleDigest != "sha256:bundle" {
		t.Fatalf("loaded profile index = %#v", loadedProfile)
	}
}

func requireConfigVersionContract(t *testing.T, ctx context.Context, s store.Store, started time.Time) store.ConfigVersion {
	t.Helper()

	version, err := s.UpsertConfigVersion(ctx, store.ConfigVersion{
		ID:           "config.empty.001",
		ProfileID:    contractProfileID,
		SourcePath:   "/tmp/external-profile-bundles/empty",
		BundleDigest: "sha256:bundle",
		SummaryJSON:  `{"services":1}`,
		Active:       true,
		PublishedAt:  started.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("upsert config version: %v", err)
	}
	if version.CreatedAt.IsZero() {
		t.Fatalf("config version should have CreatedAt: %#v", version)
	}
	activeVersion, err := s.GetActiveConfigVersion(ctx)
	if err != nil {
		t.Fatalf("get active config version: %v", err)
	}
	if activeVersion.ID != "config.empty.001" || activeVersion.ProfileID != "empty" || activeVersion.BundleDigest != "sha256:bundle" || !activeVersion.Active {
		t.Fatalf("active config version = %#v", activeVersion)
	}
	return activeVersion
}

func requireReadModelContract(t *testing.T, ctx context.Context, s store.Store, activeVersion store.ConfigVersion, started time.Time) {
	t.Helper()

	readModel, err := s.UpsertReadModel(ctx, store.ReadModel{
		ProfileID:       contractProfileID,
		Key:             "interface-nodes",
		ConfigVersionID: activeVersion.ID,
		PayloadJSON:     `{"ok":true,"items":[]}`,
		GeneratedAt:     started.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("upsert read model: %v", err)
	}
	if readModel.UpdatedAt.IsZero() {
		t.Fatalf("read model should have UpdatedAt: %#v", readModel)
	}
	loadedReadModel, err := s.GetReadModel(ctx, contractProfileID, "interface-nodes")
	if err != nil {
		t.Fatalf("get read model: %v", err)
	}
	if loadedReadModel.ConfigVersionID != activeVersion.ID || !jsonEqual(loadedReadModel.PayloadJSON, `{"ok":true,"items":[]}`) {
		t.Fatalf("loaded read model = %#v", loadedReadModel)
	}
}

func requireProfileCatalogContract(t *testing.T, ctx context.Context, s store.Store, started time.Time) {
	t.Helper()

	if err := s.ReplaceProfileCatalog(ctx, contractProfileCatalog(started)); err != nil {
		t.Fatalf("replace profile catalog index: %v", err)
	}
	catalogIndex, err := s.GetProfileCatalogIndex(ctx)
	if err != nil {
		t.Fatalf("get profile catalog index: %v", err)
	}
	if catalogIndex.ProfileID != contractProfileID || catalogIndex.IndexedAt.IsZero() {
		t.Fatalf("profile catalog index identity = %#v", catalogIndex)
	}
	if catalogIndex.Counts.Services != 1 || catalogIndex.Counts.Workflows != 1 || catalogIndex.Counts.APICases != 1 || catalogIndex.Counts.Templates != 2 {
		t.Fatalf("profile catalog index counts = %#v", catalogIndex.Counts)
	}
	catalog, err := s.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get profile catalog: %v", err)
	}
	if len(catalog.Services) != 1 || catalog.Services[0].SourcePath != "/tmp/source/service.alpha" {
		t.Fatalf("profile catalog services = %#v", catalog.Services)
	}
	if len(catalog.APICases) != 1 || catalog.APICases[0].CasePath != "cases/case.alpha.json" || catalog.APICases[0].SourceKind != "karate" || catalog.APICases[0].SourcePath != "tests/api.feature" || catalog.APICases[0].ExecutorID != "executor.karate" || catalog.APICases[0].BaseURL != "http://127.0.0.1:18080" || catalog.APICases[0].EvidenceDir != ".runtime/cases" || catalog.APICases[0].TimeoutSeconds != 12 || catalog.APICases[0].DefaultOverridesJSON != `{"itemId":"item-001"}` {
		t.Fatalf("profile catalog api case run config = %#v", catalog.APICases)
	}
}

func requireMissingRunError(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()

	_, err := s.GetRun(ctx, "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing run error = %v, want ErrNotFound", err)
	}
}

func jsonEqual(left string, right string) bool {
	var leftValue any
	var rightValue any
	if err := json.Unmarshal([]byte(left), &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(right), &rightValue); err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}
