package sqlstore_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlstore"
)

func TestStoreRecordsRuntimeEvidenceTopologyAndPostProcessThroughDatabaseSQL(t *testing.T) {
	tests := []struct {
		name              string
		dialect           sqlstore.Dialect
		evidenceValues    string
		evidenceWhere     string
		topologyValues    string
		topologyUpsert    string
		topologyUpdate    string
		topologyWhere     string
		postProcessValues string
		postProcessUpsert string
		postProcessUpdate string
		postProcessWhere  string
	}{
		{
			name:              "postgres",
			dialect:           sqlstore.PostgresDialect{},
			evidenceValues:    "values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)",
			evidenceWhere:     "from evidence_records where run_id = $1",
			topologyValues:    "values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)",
			topologyUpsert:    "on conflict(id) do update",
			topologyUpdate:    "topology_json = excluded.topology_json",
			topologyWhere:     "from trace_topologies where workflow_run_id = $1",
			postProcessValues: "values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)",
			postProcessUpsert: "on conflict(id) do update",
			postProcessUpdate: "summary_json = excluded.summary_json",
			postProcessWhere:  "from post_process_tasks where run_id = $1",
		},
		{
			name:              "mysql",
			dialect:           sqlstore.MySQLDialect{},
			evidenceValues:    "values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			evidenceWhere:     "from evidence_records where run_id = ?",
			topologyValues:    "values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			topologyUpsert:    "on duplicate key update",
			topologyUpdate:    "topology_json = values(topology_json)",
			topologyWhere:     "from trace_topologies where workflow_run_id = ?",
			postProcessValues: "values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			postProcessUpsert: "on duplicate key update",
			postProcessUpdate: "summary_json = values(summary_json)",
			postProcessWhere:  "from post_process_tasks where run_id = ?",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, state := openFakeSQLDB(t)
			defer db.Close()
			s := sqlstore.New(db, tt.dialect)
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
			if !strings.Contains(exec.query, "insert into evidence_records") || !strings.Contains(exec.query, tt.evidenceValues) {
				t.Fatalf("%s evidence query did not use expected bind vars:\n%s", tt.name, exec.query)
			}
			if evidence.CreatedAt.IsZero() || exec.args[0] != "evidence-001" || exec.args[12] != `{"phase":"request"}` {
				t.Fatalf("%s evidence record/args = %#v %#v", tt.name, evidence, exec.args)
			}
			emptyLabels, err := s.RecordEvidence(ctx, store.EvidenceRecord{
				ID:        "evidence-empty-labels",
				RunID:     "run-001",
				CaseRunID: "case-run-001",
				Kind:      "http",
				CreatedAt: createdAt,
			})
			if err != nil {
				t.Fatalf("record evidence with empty labels: %v", err)
			}
			exec = state.lastExec(t)
			if emptyLabels.LabelsJSON != "{}" || exec.args[12] != "{}" {
				t.Fatalf("%s empty evidence labels should be normalized for SQL JSON columns: %#v %#v", tt.name, emptyLabels, exec.args)
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
				t.Fatalf("%s evidence rows = %#v", tt.name, evidenceRows)
			}
			query := state.lastQuery(t)
			if !strings.Contains(query.query, tt.evidenceWhere) || query.args[0] != "run-001" {
				t.Fatalf("%s evidence list query = %#v", tt.name, query)
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
			if !strings.Contains(exec.query, "insert into trace_topologies") || !strings.Contains(exec.query, tt.topologyValues) {
				t.Fatalf("%s topology query did not use expected bind vars:\n%s", tt.name, exec.query)
			}
			if !strings.Contains(exec.query, tt.topologyUpsert) || !strings.Contains(exec.query, tt.topologyUpdate) {
				t.Fatalf("%s topology query did not use expected upsert:\n%s", tt.name, exec.query)
			}
			if topology.CreatedAt.IsZero() || exec.args[1] != "run-001" || exec.args[8] != `{"provider":"skywalking"}` {
				t.Fatalf("%s topology record/args = %#v %#v", tt.name, topology, exec.args)
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
				t.Fatalf("%s topologies = %#v", tt.name, topologies)
			}
			query = state.lastQuery(t)
			if !strings.Contains(query.query, tt.topologyWhere) || query.args[0] != "run-001" {
				t.Fatalf("%s topology list query = %#v", tt.name, query)
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
				SummaryJSON: `{"collected":true}`,
			})
			if err != nil {
				t.Fatalf("record post-process task: %v", err)
			}
			if task.DurationMs != 3000 {
				t.Fatalf("post-process duration = %d, want 3000", task.DurationMs)
			}
			exec = state.lastExec(t)
			if !strings.Contains(exec.query, "insert into post_process_tasks") || !strings.Contains(exec.query, tt.postProcessValues) {
				t.Fatalf("%s post-process query did not use expected bind vars:\n%s", tt.name, exec.query)
			}
			if !strings.Contains(exec.query, tt.postProcessUpsert) || !strings.Contains(exec.query, tt.postProcessUpdate) {
				t.Fatalf("%s post-process query did not use expected upsert:\n%s", tt.name, exec.query)
			}
			if task.CreatedAt.IsZero() || exec.args[5] != "skywalking-topology" || exec.args[11] != `{"collected":true}` {
				t.Fatalf("%s post-process task/args = %#v %#v", tt.name, task, exec.args)
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
				t.Fatalf("%s post-process tasks = %#v", tt.name, tasks)
			}
			query = state.lastQuery(t)
			if !strings.Contains(query.query, tt.postProcessWhere) || query.args[0] != "run-001" {
				t.Fatalf("%s post-process list query = %#v", tt.name, query)
			}
		})
	}
}
