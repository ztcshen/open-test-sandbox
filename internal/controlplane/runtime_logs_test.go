package controlplane

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
)

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
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
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
