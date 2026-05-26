package controlplane_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesEmptyRunListsForReactShell(t *testing.T) {
	server := newWorkflowRoutesProfileServer(t, loadEmptyProfile(t))

	var payload struct {
		OK           bool             `json:"ok"`
		WorkflowRuns []map[string]any `json:"workflowRuns"`
		ReplayRuns   []map[string]any `json:"replayRuns"`
		ProbeRuns    []map[string]any `json:"probeRuns"`
	}
	getJSONInto(t, server.URL+"/api/runs", http.StatusOK, &payload)
	if !payload.OK {
		t.Fatalf("runs should expose ok envelope: %#v", payload)
	}
	if payload.WorkflowRuns == nil || payload.ReplayRuns == nil || payload.ProbeRuns == nil {
		t.Fatalf("runs should encode empty arrays: %#v", payload)
	}
}

func openWorkflowRoutesSQLiteStore(t *testing.T, ctx context.Context) *sqlite.Store {
	t.Helper()

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newWorkflowRoutesProfileServer(t *testing.T, bundle profile.Bundle) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(controlplane.New(bundle))
	t.Cleanup(server.Close)
	return server
}

func newWorkflowRoutesStoreServer(t *testing.T, bundle profile.Bundle, runtime store.Store) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(controlplane.NewWithStore(bundle, runtime))
	t.Cleanup(server.Close)
	return server
}

func sampleWorkflowRoutesProfile() profile.Bundle {
	return profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}
}

func recordWorkflowRouteRun(t *testing.T, ctx context.Context, s *sqlite.Store, run store.Run) {
	t.Helper()

	if _, err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run %s: %v", run.ID, err)
	}
}

func saveWorkflowRouteTraceTopology(t *testing.T, ctx context.Context, s *sqlite.Store) {
	t.Helper()

	if _, err := s.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            "topology.alpha",
		WorkflowRunID: "run.alpha",
		WorkflowID:    "workflow.alpha",
		StepID:        "step.beta",
		CaseID:        "case.beta",
		RequestID:     "request.beta",
		TraceID:       "trace.beta",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","confirmedEdges":[{"source":"service.alpha","target":"service.beta"}],"externalExits":[],"unresolvedExits":[],"observedNodes":["service.alpha","service.beta"]}`,
		TextTopology:  "service.alpha -> service.beta",
	}); err != nil {
		t.Fatalf("save topology: %v", err)
	}
}

func recordWorkflowRouteRuntimeLogs(t *testing.T, ctx context.Context, s *sqlite.Store, started time.Time) {
	t.Helper()

	logPath := filepath.Join(t.TempDir(), "runtime-logs.json")
	if err := os.WriteFile(logPath, []byte(`{"systems":[{"name":"worker","found":true,"coreLogs":["request.beta handled"]}]}`), 0o644); err != nil {
		t.Fatalf("write runtime logs: %v", err)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "runtime.logs.step.beta",
		RunID:     "run.alpha",
		CaseRunID: "step.beta",
		Kind:      "runtime_logs",
		URI:       logPath,
		MediaType: "application/json",
		Summary:   `{"stepId":"step.beta"}`,
		CreatedAt: started,
	}); err != nil {
		t.Fatalf("record runtime logs: %v", err)
	}
}
