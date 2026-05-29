package controlplane_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
)

func TestServerMarksMissingLocalEvidenceLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openCaseEvidenceSQLiteStore(t, ctx)
	defer s.Close()
	createCaseEvidenceRun(t, ctx, s)
	recordMissingLocalCaseEvidence(t, ctx, s)

	payload, ok, err := controlplane.CaseEvidencePayloadForCaseRunID(ctx, s, "run.alpha.case")
	if err != nil || !ok {
		t.Fatalf("case evidence payload ok=%t err=%v", ok, err)
	}
	evidence := payload["evidence"].(map[string]any)
	requireMissingLocalEvidenceLifecycle(t, evidence, "request")
	requireMissingLocalEvidenceLifecycle(t, evidence, "response")
}

func TestServerResolvesRelativeLocalEvidenceAgainstRunRoot(t *testing.T) {
	ctx := context.Background()
	s := openCaseEvidenceSQLiteStore(t, ctx)
	defer s.Close()
	evidenceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(evidenceRoot, "request.json"), []byte(`{"method":"POST","path":"/relative","body":{"ok":true}}`), 0o644); err != nil {
		t.Fatalf("write relative request evidence: %v", err)
	}
	if err := os.WriteFile(filepath.Join(evidenceRoot, "response.json"), []byte(`{"statusCode":201,"body":"{\"ok\":true}"}`), 0o644); err != nil {
		t.Fatalf("write relative response evidence: %v", err)
	}
	if _, err := s.CreateRun(ctx, store.Run{ID: "run.relative", ProfileID: "sample", Status: store.StatusPassed, EvidenceRoot: evidenceRoot, SummaryJSON: "{}"}); err != nil {
		t.Fatalf("create relative run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.relative.case",
		RunID:                "run.relative",
		CaseID:               "case.relative",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"POST","path":"/relative"}`,
		AssertionSummaryJSON: `{"status":"passed","errorCount":0}`,
	}); err != nil {
		t.Fatalf("record relative api case run: %v", err)
	}
	recordRelativeLocalEvidence(t, ctx, s, "request", `{"method":"POST","path":"/relative"}`)
	recordRelativeLocalEvidence(t, ctx, s, "response", `{"statusCode":201}`)

	payload, ok, err := controlplane.CaseEvidencePayloadForCaseRunID(ctx, s, "run.relative.case")
	if err != nil || !ok {
		t.Fatalf("case evidence payload ok=%t err=%v", ok, err)
	}
	evidence := payload["evidence"].(map[string]any)
	requireAvailableLocalEvidenceLifecycle(t, evidence, "request", filepath.Join(evidenceRoot, "request.json"))
	requireAvailableLocalEvidenceLifecycle(t, evidence, "response", filepath.Join(evidenceRoot, "response.json"))
}

func recordRelativeLocalEvidence(t *testing.T, ctx context.Context, s store.Store, kind string, summary string) {
	t.Helper()
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         "run.relative." + kind,
		RunID:      "run.relative",
		CaseRunID:  "run.relative.case",
		Kind:       kind,
		URI:        kind + ".json",
		MediaType:  "application/json",
		Summary:    summary,
		Category:   "http-exchange",
		Visibility: "public",
	}); err != nil {
		t.Fatalf("record relative %s evidence: %v", kind, err)
	}
}

func recordMissingLocalCaseEvidence(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()
	missingDir := filepath.Join(t.TempDir(), "expired")
	for _, item := range []struct {
		kind    string
		summary string
	}{
		{kind: "request", summary: `{"method":"POST","path":"/alpha"}`},
		{kind: "response", summary: `{"statusCode":200}`},
	} {
		if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
			ID:         "run.alpha." + item.kind,
			RunID:      "run.alpha",
			CaseRunID:  "run.alpha.case",
			Kind:       item.kind,
			URI:        filepath.Join(missingDir, item.kind+".json"),
			MediaType:  "application/json",
			Summary:    item.summary,
			Category:   "http-exchange",
			Visibility: "public",
		}); err != nil {
			t.Fatalf("record %s evidence: %v", item.kind, err)
		}
	}
}

func requireAvailableLocalEvidenceLifecycle(t *testing.T, evidence map[string]any, key string, path string) {
	t.Helper()
	attachment := evidence[key].(map[string]any)["attachment"].(map[string]any)
	lifecycle := attachment["lifecycle"].(map[string]any)
	if lifecycle["kind"] != "local-file" || lifecycle["available"] != true || lifecycle["state"] != "available" || lifecycle["path"] != path {
		t.Fatalf("%s lifecycle = %#v, want path %s", key, lifecycle, path)
	}
}

func requireMissingLocalEvidenceLifecycle(t *testing.T, evidence map[string]any, key string) {
	t.Helper()
	attachment := evidence[key].(map[string]any)["attachment"].(map[string]any)
	lifecycle := attachment["lifecycle"].(map[string]any)
	if lifecycle["kind"] != "local-file" || lifecycle["available"] != false || lifecycle["state"] != "missing" {
		t.Fatalf("%s lifecycle = %#v", key, lifecycle)
	}
	if !strings.Contains(fmt.Sprint(lifecycle["nextAction"]), "--evidence-dir") {
		t.Fatalf("%s lifecycle next action = %#v", key, lifecycle)
	}
}
