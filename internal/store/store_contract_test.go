package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
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

func exerciseStoreContract(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()

	started := time.Date(2026, 5, 14, 9, 30, 0, 0, time.UTC)
	run, err := s.CreateRun(ctx, store.Run{
		ID:           "run-001",
		ProfileID:    "empty",
		WorkflowID:   "workflow.smoke",
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

	loadedRun, err := s.GetRun(ctx, "run-001")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if loadedRun.ProfileID != "empty" || loadedRun.WorkflowID != "workflow.smoke" || loadedRun.Status != store.StatusRunning {
		t.Fatalf("loaded run = %#v", loadedRun)
	}
	if loadedRun.SummaryJSON != `{"stepCount":1}` {
		t.Fatalf("loaded run summary = %q", loadedRun.SummaryJSON)
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-001" {
		t.Fatalf("runs = %#v", runs)
	}
	if runs[0].SummaryJSON != `{"stepCount":1}` {
		t.Fatalf("listed run summary = %q", runs[0].SummaryJSON)
	}

	caseRun, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "case-run-001",
		RunID:                "run-001",
		CaseID:               "case.health",
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

	caseRuns, err := s.ListAPICaseRuns(ctx, "run-001")
	if err != nil {
		t.Fatalf("list case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != "case.health" || caseRuns[0].Status != store.StatusPassed {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	if caseRuns[0].RequestSummaryJSON != `{"method":"GET"}` || caseRuns[0].AssertionSummaryJSON != `{"passed":1}` {
		t.Fatalf("case run summaries = %#v", caseRuns[0])
	}

	evidence, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        "evidence-001",
		RunID:     "run-001",
		CaseRunID: "case-run-001",
		Kind:      "http-response",
		URI:       "evidence/run-001/response.json",
		MediaType: "application/json",
		SHA256:    "abc123",
		SizeBytes: 42,
		Summary:   "response body",
	})
	if err != nil {
		t.Fatalf("record evidence: %v", err)
	}
	if evidence.CreatedAt.IsZero() {
		t.Fatalf("evidence should have CreatedAt: %#v", evidence)
	}

	evidenceRecords, err := s.ListEvidence(ctx, "run-001")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(evidenceRecords) != 1 || evidenceRecords[0].URI != "evidence/run-001/response.json" {
		t.Fatalf("evidence records = %#v", evidenceRecords)
	}
	if evidenceRecords[0].Kind != "http-response" || evidenceRecords[0].MediaType != "application/json" || evidenceRecords[0].SHA256 != "abc123" || evidenceRecords[0].SizeBytes != 42 || evidenceRecords[0].Summary != "response body" {
		t.Fatalf("evidence metadata = %#v", evidenceRecords[0])
	}

	gate, err := s.UpsertBaselineGate(ctx, store.BaselineGate{
		ProfileID:   "empty",
		SubjectID:   "workflow.smoke",
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

	loadedGate, err := s.GetBaselineGate(ctx, "empty", "workflow.smoke")
	if err != nil {
		t.Fatalf("get baseline gate: %v", err)
	}
	if loadedGate.Status != store.StatusPassed || loadedGate.Required {
		t.Fatalf("loaded baseline gate = %#v", loadedGate)
	}

	profile, err := s.UpsertProfileIndex(ctx, store.ProfileIndex{
		ProfileID:    "empty",
		BundlePath:   "profiles/empty",
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

	loadedProfile, err := s.GetProfileIndex(ctx, "empty")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if loadedProfile.BundlePath != "profiles/empty" || loadedProfile.BundleDigest != "sha256:bundle" {
		t.Fatalf("loaded profile index = %#v", loadedProfile)
	}

	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "empty",
		IndexedAt: started.Add(3 * time.Minute),
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		Workflows: []store.CatalogWorkflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
		RequestTemplates: []store.CatalogRequestTemplate{
			{ID: "template.alpha", DisplayName: "Template Alpha", NodeID: "node.alpha", TemplateJSON: `{"method":"GET"}`},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.alpha", StepID: "step.alpha", NodeID: "node.alpha", CaseID: "case.alpha", Required: true},
		},
		CaseDependencies: []store.CatalogCaseDependency{
			{ID: "dependency.alpha", CaseID: "case.alpha", FixtureID: "fixture.alpha", MappingsJSON: `[]`},
		},
		Fixtures: []store.CatalogFixture{
			{ID: "fixture.alpha", DisplayName: "Fixture Alpha", Kind: "json", DataJSON: `{}`},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog index: %v", err)
	}
	catalogIndex, err := s.GetProfileCatalogIndex(ctx)
	if err != nil {
		t.Fatalf("get profile catalog index: %v", err)
	}
	if catalogIndex.ProfileID != "empty" || catalogIndex.IndexedAt.IsZero() {
		t.Fatalf("profile catalog index identity = %#v", catalogIndex)
	}
	if catalogIndex.Counts.Services != 1 || catalogIndex.Counts.Workflows != 1 || catalogIndex.Counts.APICases != 1 || catalogIndex.Counts.Templates != 2 {
		t.Fatalf("profile catalog index counts = %#v", catalogIndex.Counts)
	}

	_, err = s.GetRun(ctx, "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing run error = %v, want ErrNotFound", err)
	}
}
