package sqlstore_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlstore"
)

func TestStoreRecordsAndReadsRunsThroughDatabaseSQL(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, sqlstore.PostgresDialect{})
	started := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)

	created, err := s.CreateRun(ctx, store.Run{
		ID:           "run-001",
		ProfileID:    "profile.alpha",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusRunning,
		EvidenceRoot: ".runtime/evidence/run-001",
		SummaryJSON:  `{"stepCount":1}`,
		StartedAt:    started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("created run timestamps = %#v", created)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "insert into runs") || !strings.Contains(exec.query, "values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)") {
		t.Fatalf("create run query did not use postgres bind vars:\n%s", exec.query)
	}
	if exec.args[0] != "run-001" || exec.args[5] != `{"stepCount":1}` {
		t.Fatalf("create run args = %#v", exec.args)
	}

	state.queueRows(fakeRows{
		columns: []string{"id", "profile_id", "workflow_id", "status", "evidence_root", "summary_json", "started_at", "finished_at", "created_at", "updated_at"},
		values: [][]driver.Value{{
			"run-001", "profile.alpha", "workflow.alpha", store.StatusPassed, ".runtime/evidence/run-001", `{"stepCount":1}`,
			started.Format(time.RFC3339Nano), "", created.CreatedAt.Format(time.RFC3339Nano), created.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
	loaded, err := s.GetRun(ctx, "run-001")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if loaded.ID != "run-001" || loaded.Status != store.StatusPassed || loaded.SummaryJSON != `{"stepCount":1}` || !loaded.StartedAt.Equal(started) {
		t.Fatalf("loaded run = %#v", loaded)
	}
	query := state.lastQuery(t)
	if !strings.Contains(query.query, "from runs where id = $1") || query.args[0] != "run-001" {
		t.Fatalf("get run query = %#v", query)
	}

	state.queueRows(fakeRows{
		columns: []string{"id", "profile_id", "workflow_id", "status", "evidence_root", "summary_json", "started_at", "finished_at", "created_at", "updated_at"},
		values: [][]driver.Value{{
			"run-001", "profile.alpha", "workflow.alpha", store.StatusPassed, ".runtime/evidence/run-001", `{"stepCount":1}`,
			started.Format(time.RFC3339Nano), "", created.CreatedAt.Format(time.RFC3339Nano), created.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-001" {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestStoreRecordsAndReadsAPICaseRunsThroughDatabaseSQL(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, sqlstore.MySQLDialect{})
	started := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)
	finished := started.Add(250 * time.Millisecond)

	created, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "case-run-001",
		RunID:                "run-001",
		CaseID:               "case.alpha",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"GET"}`,
		AssertionSummaryJSON: `{"passed":1}`,
		StartedAt:            started,
		FinishedAt:           finished,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Fatalf("created case run timestamp = %#v", created)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "insert into api_case_runs") || strings.Contains(exec.query, "$1") || !strings.Contains(exec.query, "values (?, ?, ?, ?, ?, ?, ?, ?, ?)") {
		t.Fatalf("case run query did not use mysql bind vars:\n%s", exec.query)
	}
	if exec.args[2] != "case.alpha" || exec.args[4] != `{"method":"GET"}` {
		t.Fatalf("case run args = %#v", exec.args)
	}

	state.queueRows(fakeRows{
		columns: []string{"id", "run_id", "case_id", "status", "request_summary_json", "assertion_summary_json", "started_at", "finished_at", "created_at"},
		values: [][]driver.Value{{
			"case-run-001", "run-001", "case.alpha", store.StatusPassed, `{"method":"GET"}`, `{"passed":1}`,
			started.Format(time.RFC3339Nano), finished.Format(time.RFC3339Nano), created.CreatedAt.Format(time.RFC3339Nano),
		}},
	})
	caseRuns, err := s.ListAPICaseRuns(ctx, "run-001")
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].ID != "case-run-001" || caseRuns[0].CaseID != "case.alpha" {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	query := state.lastQuery(t)
	if !strings.Contains(query.query, "from api_case_runs where run_id = ?") || query.args[0] != "run-001" {
		t.Fatalf("case run list query = %#v", query)
	}
}

func TestStoreRecordsRuntimeEvidenceTopologyAndPostProcessThroughDatabaseSQL(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, sqlstore.PostgresDialect{})
	createdAt := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)

	evidence, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         "evidence-001",
		RunID:      "run-001",
		CaseRunID:  "case-run-001",
		StepID:     "step-http",
		Kind:       "http",
		URI:        ".runtime/evidence/request.json",
		MediaType:  "application/json",
		SHA256:     "abc123",
		SizeBytes:  512,
		Summary:    "request evidence",
		Category:   "request",
		Visibility: "public",
		LabelsJSON: `{"phase":"request"}`,
		CreatedAt:  createdAt,
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "insert into evidence_records") || !strings.Contains(exec.query, "values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)") {
		t.Fatalf("evidence query did not use postgres bind vars:\n%s", exec.query)
	}
	if evidence.CreatedAt.IsZero() || exec.args[0] != "evidence-001" || exec.args[12] != `{"phase":"request"}` {
		t.Fatalf("evidence record/args = %#v %#v", evidence, exec.args)
	}

	state.queueRows(fakeRows{
		columns: []string{"id", "run_id", "case_run_id", "step_id", "kind", "uri", "media_type", "sha256", "size_bytes", "summary", "category", "visibility", "labels_json", "created_at"},
		values: [][]driver.Value{{
			"evidence-001", "run-001", "case-run-001", "step-http", "http", ".runtime/evidence/request.json",
			"application/json", "abc123", int64(512), "request evidence", "request", "public", `{"phase":"request"}`, createdAt.Format(time.RFC3339Nano),
		}},
	})
	evidenceRows, err := s.ListEvidence(ctx, "run-001")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidenceRows) != 1 || evidenceRows[0].ID != "evidence-001" || evidenceRows[0].SizeBytes != 512 {
		t.Fatalf("evidence rows = %#v", evidenceRows)
	}
	query := state.lastQuery(t)
	if !strings.Contains(query.query, "from evidence_records where run_id = $1") || query.args[0] != "run-001" {
		t.Fatalf("evidence list query = %#v", query)
	}

	topology, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology-001",
		WorkflowRunID: "run-001",
		WorkflowID:    "workflow.alpha",
		StepID:        "step-http",
		CaseID:        "case.alpha",
		RequestID:     "request-001",
		TraceID:       "trace-001",
		Status:        store.StatusPassed,
		TopologyJSON:  `{"provider":"skywalking"}`,
		TextTopology:  "client -> service",
	})
	if err != nil {
		t.Fatalf("save trace topology: %v", err)
	}
	exec = state.lastExec(t)
	if !strings.Contains(exec.query, "insert into trace_topologies") || !strings.Contains(exec.query, "values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)") {
		t.Fatalf("topology query did not use postgres bind vars:\n%s", exec.query)
	}
	if topology.CreatedAt.IsZero() || exec.args[1] != "run-001" || exec.args[8] != `{"provider":"skywalking"}` {
		t.Fatalf("topology record/args = %#v %#v", topology, exec.args)
	}

	state.queueRows(fakeRows{
		columns: []string{"id", "workflow_run_id", "workflow_id", "step_id", "case_id", "request_id", "trace_id", "status", "topology_json", "text_topology", "created_at"},
		values: [][]driver.Value{{
			"topology-001", "run-001", "workflow.alpha", "step-http", "case.alpha", "request-001", "trace-001",
			store.StatusPassed, `{"provider":"skywalking"}`, "client -> service", topology.CreatedAt.Format(time.RFC3339Nano),
		}},
	})
	topologies, err := s.ListTraceTopologies(ctx, "run-001")
	if err != nil {
		t.Fatalf("list trace topologies: %v", err)
	}
	if len(topologies) != 1 || topologies[0].TraceID != "trace-001" || topologies[0].TopologyJSON != `{"provider":"skywalking"}` {
		t.Fatalf("topologies = %#v", topologies)
	}
	query = state.lastQuery(t)
	if !strings.Contains(query.query, "from trace_topologies where workflow_run_id = $1") || query.args[0] != "run-001" {
		t.Fatalf("topology list query = %#v", query)
	}

	started := createdAt.Add(1 * time.Minute)
	finished := started.Add(3 * time.Second)
	task, err := s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:          "task-001",
		RunID:       "run-001",
		WorkflowID:  "workflow.alpha",
		StepID:      "step-http",
		CaseID:      "case.alpha",
		Kind:        "skywalking-topology",
		Status:      store.StatusPassed,
		StartedAt:   started,
		FinishedAt:  finished,
		DurationMs:  3000,
		SummaryJSON: `{"collected":true}`,
	})
	if err != nil {
		t.Fatalf("record post-process task: %v", err)
	}
	exec = state.lastExec(t)
	if !strings.Contains(exec.query, "insert into post_process_tasks") || !strings.Contains(exec.query, "values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)") {
		t.Fatalf("post-process query did not use postgres bind vars:\n%s", exec.query)
	}
	if task.CreatedAt.IsZero() || exec.args[5] != "skywalking-topology" || exec.args[11] != `{"collected":true}` {
		t.Fatalf("post-process task/args = %#v %#v", task, exec.args)
	}

	state.queueRows(fakeRows{
		columns: []string{"id", "run_id", "workflow_id", "step_id", "case_id", "kind", "status", "started_at", "finished_at", "duration_ms", "error", "summary_json", "created_at"},
		values: [][]driver.Value{{
			"task-001", "run-001", "workflow.alpha", "step-http", "case.alpha", "skywalking-topology", store.StatusPassed,
			started.Format(time.RFC3339Nano), finished.Format(time.RFC3339Nano), int64(3000), "", `{"collected":true}`, task.CreatedAt.Format(time.RFC3339Nano),
		}},
	})
	tasks, err := s.ListPostProcessTasks(ctx, "run-001")
	if err != nil {
		t.Fatalf("list post-process tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Kind != "skywalking-topology" || tasks[0].DurationMs != 3000 {
		t.Fatalf("post-process tasks = %#v", tasks)
	}
	query = state.lastQuery(t)
	if !strings.Contains(query.query, "from post_process_tasks where run_id = $1") || query.args[0] != "run-001" {
		t.Fatalf("post-process list query = %#v", query)
	}
}

func TestStoreUpsertsAndReadsBaselineGateThroughDatabaseSQL(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, sqlstore.MySQLDialect{})
	checkedAt := time.Date(2026, 5, 19, 11, 0, 0, 0, time.UTC)

	gate, err := s.UpsertBaselineGate(ctx, store.BaselineGate{
		ProfileID:   "profile.alpha",
		SubjectID:   "workflow.alpha",
		Status:      store.StatusPassed,
		Required:    true,
		SummaryJSON: `{"required":true}`,
		CheckedAt:   checkedAt,
	})
	if err != nil {
		t.Fatalf("upsert baseline gate: %v", err)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "insert into baseline_gates") || strings.Contains(exec.query, "$1") || !strings.Contains(exec.query, "values (?, ?, ?, ?, ?, ?, ?)") {
		t.Fatalf("baseline gate query did not use mysql bind vars:\n%s", exec.query)
	}
	if !strings.Contains(exec.query, "on duplicate key update") || !strings.Contains(exec.query, "status = values(status)") {
		t.Fatalf("baseline gate query did not use mysql upsert:\n%s", exec.query)
	}
	if gate.UpdatedAt.IsZero() || exec.args[0] != "profile.alpha" || exec.args[3] != true {
		t.Fatalf("baseline gate/args = %#v %#v", gate, exec.args)
	}

	state.queueRows(fakeRows{
		columns: []string{"profile_id", "subject_id", "status", "required", "summary_json", "checked_at", "updated_at"},
		values: [][]driver.Value{{
			"profile.alpha", "workflow.alpha", store.StatusPassed, true, `{"required":true}`,
			checkedAt.Format(time.RFC3339Nano), gate.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
	loaded, err := s.GetBaselineGate(ctx, "profile.alpha", "workflow.alpha")
	if err != nil {
		t.Fatalf("get baseline gate: %v", err)
	}
	if loaded.ProfileID != "profile.alpha" || !loaded.Required || !loaded.CheckedAt.Equal(checkedAt) {
		t.Fatalf("loaded baseline gate = %#v", loaded)
	}
	query := state.lastQuery(t)
	if !strings.Contains(query.query, "from baseline_gates where profile_id = ? and subject_id = ?") || query.args[0] != "profile.alpha" || query.args[1] != "workflow.alpha" {
		t.Fatalf("baseline gate get query = %#v", query)
	}
}

func TestStoreUpsertsConfigIndexAndReadModelsThroughDatabaseSQL(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, sqlstore.PostgresDialect{})
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	profileIndex, err := s.UpsertProfileIndex(ctx, store.ProfileIndex{
		ProfileID:    "profile.alpha",
		BundlePath:   "stores/profile.alpha",
		BundleDigest: "sha256:profile",
		SummaryJSON:  `{"services":2}`,
		ImportedAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert profile index: %v", err)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "insert into profile_indexes") || !strings.Contains(exec.query, "values ($1, $2, $3, $4, $5, $6)") {
		t.Fatalf("profile index query did not use postgres bind vars:\n%s", exec.query)
	}
	if !strings.Contains(exec.query, "on conflict(profile_id) do update") || exec.args[0] != "profile.alpha" || exec.args[3] != `{"services":2}` {
		t.Fatalf("profile index upsert = %#v args=%#v query=%s", profileIndex, exec.args, exec.query)
	}
	if profileIndex.UpdatedAt.IsZero() {
		t.Fatalf("profile index updated timestamp = %#v", profileIndex)
	}

	state.queueRows(fakeRows{
		columns: []string{"profile_id", "bundle_path", "bundle_digest", "summary_json", "imported_at", "updated_at"},
		values: [][]driver.Value{{
			"profile.alpha", "stores/profile.alpha", "sha256:profile", `{"services":2}`,
			now.Format(time.RFC3339Nano), profileIndex.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
	loadedIndex, err := s.GetProfileIndex(ctx, "profile.alpha")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if loadedIndex.ProfileID != "profile.alpha" || loadedIndex.BundleDigest != "sha256:profile" || !loadedIndex.ImportedAt.Equal(now) {
		t.Fatalf("loaded profile index = %#v", loadedIndex)
	}
	query := state.lastQuery(t)
	if !strings.Contains(query.query, "from profile_indexes where profile_id = $1") || query.args[0] != "profile.alpha" {
		t.Fatalf("profile index get query = %#v", query)
	}

	configVersion, err := s.UpsertConfigVersion(ctx, store.ConfigVersion{
		ID:           "config-001",
		ProfileID:    "profile.alpha",
		SourcePath:   "stores/profile.alpha/catalog.json",
		BundleDigest: "sha256:config",
		SummaryJSON:  `{"cases":5}`,
		Active:       true,
		PublishedAt:  now.Add(1 * time.Minute),
	})
	if err != nil {
		t.Fatalf("upsert config version: %v", err)
	}
	if configVersion.CreatedAt.IsZero() {
		t.Fatalf("config version created timestamp = %#v", configVersion)
	}
	execs := state.lastExecs(t, 2)
	if !strings.Contains(execs[0].query, "update config_versions set active = $1") || execs[0].args[0] != false {
		t.Fatalf("active config reset query = %#v", execs[0])
	}
	if !strings.Contains(execs[1].query, "insert into config_versions") || !strings.Contains(execs[1].query, "values ($1, $2, $3, $4, $5, $6, $7, $8)") {
		t.Fatalf("config version insert query did not use postgres bind vars:\n%s", execs[1].query)
	}
	if !strings.Contains(execs[1].query, "on conflict(id) do update") || execs[1].args[0] != "config-001" || execs[1].args[5] != true {
		t.Fatalf("config version upsert args/query = %#v %s", execs[1].args, execs[1].query)
	}

	state.queueRows(fakeRows{
		columns: []string{"id", "profile_id", "source_path", "bundle_digest", "summary_json", "active", "published_at", "created_at"},
		values: [][]driver.Value{{
			"config-001", "profile.alpha", "stores/profile.alpha/catalog.json", "sha256:config", `{"cases":5}`,
			true, configVersion.PublishedAt.Format(time.RFC3339Nano), configVersion.CreatedAt.Format(time.RFC3339Nano),
		}},
	})
	active, err := s.GetActiveConfigVersion(ctx)
	if err != nil {
		t.Fatalf("get active config version: %v", err)
	}
	if active.ID != "config-001" || !active.Active || active.BundleDigest != "sha256:config" {
		t.Fatalf("active config version = %#v", active)
	}
	query = state.lastQuery(t)
	if !strings.Contains(query.query, "from config_versions") || !strings.Contains(query.query, "where active = $1") || query.args[0] != true {
		t.Fatalf("active config query = %#v", query)
	}

	readModel, err := s.UpsertReadModel(ctx, store.ReadModel{
		ProfileID:       "profile.alpha",
		Key:             "workflow-discovery",
		ConfigVersionID: "config-001",
		PayloadJSON:     `{"workflows":[{"id":"workflow.alpha"}]}`,
		GeneratedAt:     now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("upsert read model: %v", err)
	}
	exec = state.lastExec(t)
	if !strings.Contains(exec.query, "insert into config_read_model") || !strings.Contains(exec.query, "values ($1, $2, $3, $4, $5, $6)") {
		t.Fatalf("read model query did not use postgres bind vars:\n%s", exec.query)
	}
	if !strings.Contains(exec.query, "on conflict(profile_id, model_key) do update") || exec.args[1] != "workflow-discovery" {
		t.Fatalf("read model upsert args/query = %#v %s", exec.args, exec.query)
	}
	if readModel.UpdatedAt.IsZero() {
		t.Fatalf("read model updated timestamp = %#v", readModel)
	}

	state.queueRows(fakeRows{
		columns: []string{"profile_id", "model_key", "config_version_id", "payload_json", "generated_at", "updated_at"},
		values: [][]driver.Value{{
			"profile.alpha", "workflow-discovery", "config-001", `{"workflows":[{"id":"workflow.alpha"}]}`,
			readModel.GeneratedAt.Format(time.RFC3339Nano), readModel.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
	loadedReadModel, err := s.GetReadModel(ctx, "profile.alpha", "workflow-discovery")
	if err != nil {
		t.Fatalf("get read model: %v", err)
	}
	if loadedReadModel.ProfileID != "profile.alpha" || loadedReadModel.Key != "workflow-discovery" || loadedReadModel.ConfigVersionID != "config-001" {
		t.Fatalf("loaded read model = %#v", loadedReadModel)
	}
	query = state.lastQuery(t)
	if !strings.Contains(query.query, "from config_read_model") || !strings.Contains(query.query, "where profile_id = $1 and model_key = $2") {
		t.Fatalf("read model get query = %#v", query)
	}
}

const fakeDriverName = "otsandbox_sqlstore_fake"

var registerFakeDriverOnce sync.Once

func openFakeSQLDB(t *testing.T) (*sql.DB, *fakeSQLState) {
	t.Helper()
	registerFakeDriverOnce.Do(func() {
		sql.Register(fakeDriverName, fakeSQLDriver{})
	})
	state := &fakeSQLState{}
	name := fakeSQLStateRegistry.put(state)
	db, err := sql.Open(fakeDriverName, name)
	if err != nil {
		t.Fatalf("open fake sql db: %v", err)
	}
	return db, state
}

type fakeSQLCall struct {
	query string
	args  []any
}

type fakeRows struct {
	columns []string
	values  [][]driver.Value
}

type fakeSQLState struct {
	mu      sync.Mutex
	execs   []fakeSQLCall
	queries []fakeSQLCall
	rows    []fakeRows
}

func (s *fakeSQLState) queueRows(rows fakeRows) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, rows)
}

func (s *fakeSQLState) lastExec(t *testing.T) fakeSQLCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.execs) == 0 {
		t.Fatal("no exec calls recorded")
	}
	return s.execs[len(s.execs)-1]
}

func (s *fakeSQLState) lastExecs(t *testing.T, count int) []fakeSQLCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.execs) < count {
		t.Fatalf("recorded exec calls = %d, want at least %d", len(s.execs), count)
	}
	return append([]fakeSQLCall(nil), s.execs[len(s.execs)-count:]...)
}

func (s *fakeSQLState) lastQuery(t *testing.T) fakeSQLCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) == 0 {
		t.Fatal("no query calls recorded")
	}
	return s.queries[len(s.queries)-1]
}

var fakeSQLStateRegistry = &fakeRegistry{states: map[string]*fakeSQLState{}}

type fakeRegistry struct {
	mu     sync.Mutex
	next   int
	states map[string]*fakeSQLState
}

func (r *fakeRegistry) put(state *fakeSQLState) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	name := "fake-db"
	for i := 0; i < r.next; i++ {
		name += "x"
	}
	r.states[name] = state
	return name
}

func (r *fakeRegistry) get(name string) *fakeSQLState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.states[name]
}

type fakeSQLDriver struct{}

func (fakeSQLDriver) Open(name string) (driver.Conn, error) {
	state := fakeSQLStateRegistry.get(name)
	if state == nil {
		return nil, errors.New("unknown fake database")
	}
	return fakeSQLConn{state: state}, nil
}

type fakeSQLConn struct {
	state *fakeSQLState
}

func (c fakeSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (c fakeSQLConn) Close() error              { return nil }
func (c fakeSQLConn) Begin() (driver.Tx, error) { return nil, errors.New("tx not supported") }

func (c fakeSQLConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.execs = append(c.state.execs, fakeSQLCall{query: query, args: namedValues(args)})
	return driver.RowsAffected(1), nil
}

func (c fakeSQLConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries = append(c.state.queries, fakeSQLCall{query: query, args: namedValues(args)})
	if len(c.state.rows) == 0 {
		return &fakeSQLRows{}, nil
	}
	rows := c.state.rows[0]
	c.state.rows = c.state.rows[1:]
	return &fakeSQLRows{columns: rows.columns, values: rows.values}, nil
}

func namedValues(values []driver.NamedValue) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value.Value)
	}
	return out
}

type fakeSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r fakeSQLRows) Columns() []string {
	return r.columns
}

func (r fakeSQLRows) Close() error {
	return nil
}

func (r *fakeSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
