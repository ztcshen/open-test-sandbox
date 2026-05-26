package sqlstore_test

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlstore"
)

func TestStoreRecordsRuntimeEvidenceTopologyAndPostProcessThroughDatabaseSQL(t *testing.T) {
	for _, tt := range observabilityDialectExpectations() {
		t.Run(tt.name, func(t *testing.T) {
			exerciseStoreObservabilityDatabaseSQL(t, tt)
		})
	}
}

type observabilityDialectExpectation struct {
	name                       string
	dialect                    sqlstore.Dialect
	topologyUpsertFragments    []string
	postProcessUpsertFragments []string
}

const (
	observabilityRunID         = "run-001"
	observabilityCaseRunID     = "case-run-001"
	observabilityWorkflowID    = "workflow.alpha"
	observabilityStepID        = "step-http"
	observabilityCaseID        = "case.alpha"
	observabilityEvidenceID    = "evidence-001"
	observabilityRequestID     = "request-001"
	observabilityTraceID       = "trace-001"
	observabilityTopologyID    = "topology-001"
	observabilityPostProcessID = "task-001"
)

func observabilityDialectExpectations() []observabilityDialectExpectation {
	return []observabilityDialectExpectation{
		{
			name:                    "postgres",
			dialect:                 sqlstore.PostgresDialect{},
			topologyUpsertFragments: []string{"on conflict(id) do update", "topology_json = excluded.topology_json"},
			postProcessUpsertFragments: []string{
				"on conflict(id) do update",
				"summary_json = excluded.summary_json",
			},
		},
		{
			name:                    "mysql",
			dialect:                 sqlstore.MySQLDialect{},
			topologyUpsertFragments: []string{"on duplicate key update", "topology_json = values(topology_json)"},
			postProcessUpsertFragments: []string{
				"on duplicate key update",
				"summary_json = values(summary_json)",
			},
		},
	}
}

func exerciseStoreObservabilityDatabaseSQL(t *testing.T, tt observabilityDialectExpectation) {
	t.Helper()

	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, tt.dialect)
	createdAt := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)

	assertEvidenceRecordAndLookup(t, ctx, s, state, createdAt, tt)
	assertTraceTopologyUpsertAndLookup(t, ctx, s, state, tt)
	assertPostProcessTaskUpsertAndLookup(t, ctx, s, state, createdAt, tt)
}

func assertEvidenceRecordAndLookup(
	t *testing.T,
	ctx context.Context,
	s *sqlstore.Store,
	state *fakeSQLState,
	createdAt time.Time,
	tt observabilityDialectExpectation,
) {
	t.Helper()

	evidence, err := s.RecordEvidence(ctx, evidenceRecordFixture(createdAt))
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}
	exec := state.lastExec(t)
	assertSQLContains(t, exec.query, tt.name+" evidence query", "insert into evidence_records", sqlValuesClause(tt.dialect, 14))
	if evidence.CreatedAt.IsZero() || exec.args[0] != observabilityEvidenceID || exec.args[12] != `{"phase":"request"}` {
		t.Fatalf("%s evidence record/args = %#v %#v", tt.name, evidence, exec.args)
	}

	emptyLabels, err := s.RecordEvidence(ctx, evidenceRecordWithoutLabelsFixture(createdAt))
	if err != nil {
		t.Fatalf("record evidence with empty labels: %v", err)
	}
	exec = state.lastExec(t)
	if emptyLabels.LabelsJSON != "{}" || exec.args[12] != "{}" {
		t.Fatalf("%s empty evidence labels should be normalized for SQL JSON columns: %#v %#v", tt.name, emptyLabels, exec.args)
	}

	queueEvidenceRows(state, createdAt)
	evidenceRows, err := s.ListEvidence(ctx, observabilityRunID)
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidenceRows) != 1 || evidenceRows[0].ID != observabilityEvidenceID || evidenceRows[0].SizeBytes != 512 {
		t.Fatalf("%s evidence rows = %#v", tt.name, evidenceRows)
	}
	query := state.lastQuery(t)
	assertSQLContains(t, query.query, tt.name+" evidence list query", "from evidence_records where run_id = "+tt.dialect.BindVar(1))
	if query.args[0] != observabilityRunID {
		t.Fatalf("%s evidence list query = %#v", tt.name, query)
	}
}

func assertTraceTopologyUpsertAndLookup(
	t *testing.T,
	ctx context.Context,
	s *sqlstore.Store,
	state *fakeSQLState,
	tt observabilityDialectExpectation,
) {
	t.Helper()

	topology, err := s.SaveTraceTopology(ctx, traceTopologyFixture())
	if err != nil {
		t.Fatalf("save trace topology: %v", err)
	}
	exec := state.lastExec(t)
	assertSQLContains(t, exec.query, tt.name+" topology query", "insert into trace_topologies", sqlValuesClause(tt.dialect, 11))
	assertSQLContains(t, exec.query, tt.name+" topology query", tt.topologyUpsertFragments...)
	if topology.CreatedAt.IsZero() || exec.args[1] != observabilityRunID || exec.args[8] != `{"provider":"skywalking"}` {
		t.Fatalf("%s topology record/args = %#v %#v", tt.name, topology, exec.args)
	}

	queueTraceTopologyRows(state, topology)
	topologies, err := s.ListTraceTopologies(ctx, observabilityRunID)
	if err != nil {
		t.Fatalf("list trace topologies: %v", err)
	}
	if len(topologies) != 1 || topologies[0].TraceID != observabilityTraceID || topologies[0].TopologyJSON != `{"provider":"skywalking"}` {
		t.Fatalf("%s topologies = %#v", tt.name, topologies)
	}
	query := state.lastQuery(t)
	assertSQLContains(t, query.query, tt.name+" topology list query", "from trace_topologies where workflow_run_id = "+tt.dialect.BindVar(1))
	if query.args[0] != observabilityRunID {
		t.Fatalf("%s topology list query = %#v", tt.name, query)
	}
}

func assertPostProcessTaskUpsertAndLookup(
	t *testing.T,
	ctx context.Context,
	s *sqlstore.Store,
	state *fakeSQLState,
	createdAt time.Time,
	tt observabilityDialectExpectation,
) {
	t.Helper()

	task, err := s.RecordPostProcessTask(ctx, postProcessTaskFixture(createdAt))
	if err != nil {
		t.Fatalf("record post-process task: %v", err)
	}
	if task.DurationMs != 3000 {
		t.Fatalf("post-process duration = %d, want 3000", task.DurationMs)
	}
	exec := state.lastExec(t)
	assertSQLContains(t, exec.query, tt.name+" post-process query", "insert into post_process_tasks", sqlValuesClause(tt.dialect, 13))
	assertSQLContains(t, exec.query, tt.name+" post-process query", tt.postProcessUpsertFragments...)
	if task.CreatedAt.IsZero() || exec.args[5] != "skywalking-topology" || exec.args[11] != `{"collected":true}` {
		t.Fatalf("%s post-process task/args = %#v %#v", tt.name, task, exec.args)
	}

	queuePostProcessTaskRows(state, task)
	tasks, err := s.ListPostProcessTasks(ctx, observabilityRunID)
	if err != nil {
		t.Fatalf("list post-process tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Kind != "skywalking-topology" || tasks[0].DurationMs != 3000 {
		t.Fatalf("%s post-process tasks = %#v", tt.name, tasks)
	}
	query := state.lastQuery(t)
	assertSQLContains(t, query.query, tt.name+" post-process list query", "from post_process_tasks where run_id = "+tt.dialect.BindVar(1))
	if query.args[0] != observabilityRunID {
		t.Fatalf("%s post-process list query = %#v", tt.name, query)
	}
}

func evidenceRecordFixture(createdAt time.Time) store.EvidenceRecord {
	return store.EvidenceRecord{
		ID:         observabilityEvidenceID,
		RunID:      observabilityRunID,
		CaseRunID:  observabilityCaseRunID,
		StepID:     observabilityStepID,
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
	}
}

func evidenceRecordWithoutLabelsFixture(createdAt time.Time) store.EvidenceRecord {
	return store.EvidenceRecord{
		ID:        "evidence-empty-labels",
		RunID:     observabilityRunID,
		CaseRunID: observabilityCaseRunID,
		Kind:      "http",
		CreatedAt: createdAt,
	}
}

func traceTopologyFixture() store.TraceTopology {
	return store.TraceTopology{
		ID:            observabilityTopologyID,
		WorkflowRunID: observabilityRunID,
		WorkflowID:    observabilityWorkflowID,
		StepID:        observabilityStepID,
		CaseID:        observabilityCaseID,
		RequestID:     observabilityRequestID,
		TraceID:       observabilityTraceID,
		Status:        store.StatusPassed,
		TopologyJSON:  `{"provider":"skywalking"}`,
		TextTopology:  "client -> service",
	}
}

func postProcessTaskFixture(createdAt time.Time) store.PostProcessTask {
	started := createdAt.Add(1 * time.Minute)
	return store.PostProcessTask{
		ID:          observabilityPostProcessID,
		RunID:       observabilityRunID,
		WorkflowID:  observabilityWorkflowID,
		StepID:      observabilityStepID,
		CaseID:      observabilityCaseID,
		Kind:        "skywalking-topology",
		Status:      store.StatusPassed,
		StartedAt:   started,
		FinishedAt:  started.Add(3 * time.Second),
		SummaryJSON: `{"collected":true}`,
	}
}

func queueEvidenceRows(state *fakeSQLState, createdAt time.Time) {
	state.queueRows(fakeRows{
		columns: []string{"id", "run_id", "case_run_id", "step_id", "kind", "uri", "media_type", "sha256", "size_bytes", "summary", "category", "visibility", "labels_json", "created_at"},
		values: [][]driver.Value{{
			observabilityEvidenceID, observabilityRunID, observabilityCaseRunID, observabilityStepID, "http", ".runtime/evidence/request.json",
			"application/json", "abc123", int64(512), "request evidence", "request", "public", `{"phase":"request"}`, createdAt.Format(time.RFC3339Nano),
		}},
	})
}

func queueTraceTopologyRows(state *fakeSQLState, topology store.TraceTopology) {
	state.queueRows(fakeRows{
		columns: []string{"id", "workflow_run_id", "workflow_id", "step_id", "case_id", "request_id", "trace_id", "status", "topology_json", "text_topology", "created_at"},
		values: [][]driver.Value{{
			observabilityTopologyID, observabilityRunID, observabilityWorkflowID, observabilityStepID, observabilityCaseID, observabilityRequestID, observabilityTraceID,
			store.StatusPassed, `{"provider":"skywalking"}`, "client -> service", topology.CreatedAt.Format(time.RFC3339Nano),
		}},
	})
}

func queuePostProcessTaskRows(state *fakeSQLState, task store.PostProcessTask) {
	state.queueRows(fakeRows{
		columns: []string{"id", "run_id", "workflow_id", "step_id", "case_id", "kind", "status", "started_at", "finished_at", "duration_ms", "error", "summary_json", "created_at"},
		values: [][]driver.Value{{
			observabilityPostProcessID, observabilityRunID, observabilityWorkflowID, observabilityStepID, observabilityCaseID, "skywalking-topology", store.StatusPassed,
			task.StartedAt.Format(time.RFC3339Nano), task.FinishedAt.Format(time.RFC3339Nano), int64(3000), "", `{"collected":true}`, task.CreatedAt.Format(time.RFC3339Nano),
		}},
	})
}
