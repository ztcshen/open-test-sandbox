package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
)

type slowEvidenceStore struct {
	store.Store
	delay time.Duration
}

func (s slowEvidenceStore) ListEvidence(ctx context.Context, runID string) ([]store.EvidenceRecord, error) {
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return s.Store.ListEvidence(ctx, runID)
	}
}

func TestWorkflowStepLogsUseCachedEvidenceWhenLookupIsSlow(t *testing.T) {
	ctx := context.Background()
	sqliteStore, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer sqliteStore.Close()

	run := store.Run{
		ID:           "run.cached",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: filepath.Join(t.TempDir(), "evidence"),
		SummaryJSON:  `{"steps":[{"stepId":"step.alpha"}]}`,
		CreatedAt:    time.Now().UTC(),
	}
	if _, err := sqliteStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "runtime-logs.json")
	if err := os.WriteFile(logPath, []byte(`{"systems":[{"name":"worker","found":true,"coreLogs":["request.alpha handled"]}]}`), 0o644); err != nil {
		t.Fatalf("write cached runtime logs: %v", err)
	}
	if _, err := sqliteStore.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "runtime.logs.step.alpha",
		RunID:     run.ID,
		CaseRunID: "step.alpha",
		Kind:      workflowStepRuntimeLogsKind,
		URI:       logPath,
		MediaType: "application/json",
		Summary:   `{"stepId":"step.alpha"}`,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("record cached runtime logs: %v", err)
	}

	step := map[string]any{"stepId": "step.alpha"}
	runtime := slowEvidenceStore{Store: sqliteStore, delay: 150 * time.Millisecond}
	enrichWorkflowStepLogs(ctx, runtime, run, step, nil)

	systems := listFromAny(mapFromAny(step["trace"])["systems"])
	if len(systems) != 1 || systems[0].(map[string]any)["name"] != "worker" {
		t.Fatalf("workflow step should use cached runtime logs after slow lookup: %#v", step)
	}
}

func TestWorkflowStepLogsReturnPendingAndPersistInBackground(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	run := store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: filepath.Join(t.TempDir(), "evidence"),
		SummaryJSON:  `{"steps":[{"stepId":"step.alpha"}]}`,
		CreatedAt:    time.Now().UTC(),
	}
	if _, err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	originalCommand := runRuntimeCommand
	defer func() { runRuntimeCommand = originalCommand }()
	runRuntimeCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case len(args) >= 2 && args[0] == "ps":
			return []byte("sandbox-worker\n"), nil
		case len(args) >= 1 && args[0] == "logs":
			time.Sleep(120 * time.Millisecond)
			return []byte("[INFO] request.alpha handled by worker\n"), nil
		default:
			return []byte{}, nil
		}
	}

	step := map[string]any{
		"stepId": "step.alpha",
		"caseId": "case.alpha",
		"summary": map[string]any{
			"requestId": "request.alpha",
		},
	}
	topologies := []map[string]any{{
		"requestId": "request.alpha",
		"topologyJson": map[string]any{
			"observedNodes": []any{"worker"},
		},
	}}

	started := time.Now()
	enrichWorkflowStepLogs(ctx, s, run, step, topologies)
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("enrichWorkflowStepLogs blocked on runtime collection for %s", elapsed)
	}
	pending := listFromAny(mapFromAny(step["trace"])["systems"])
	if len(pending) != 1 || pending[0].(map[string]any)["pending"] != true {
		t.Fatalf("pending log systems = %#v", pending)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		records, err := s.ListEvidence(ctx, run.ID)
		if err != nil {
			t.Fatalf("list evidence: %v", err)
		}
		tasks, err := s.ListPostProcessTasks(ctx, run.ID)
		if err != nil {
			t.Fatalf("list post process tasks: %v", err)
		}
		if len(records) == 1 && records[0].Kind == workflowStepRuntimeLogsKind && len(tasks) == 1 {
			if tasks[0].Kind != postProcessKindRuntimeLogs || tasks[0].DurationMs <= 0 || tasks[0].Status != store.StatusPassed {
				t.Fatalf("post process task = %#v", tasks[0])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("runtime log evidence was not persisted in the background")
}
