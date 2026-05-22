package casesuite

import (
	"context"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

func TestSelectCasesFiltersByMaintenanceMetadata(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Alpha Default", NodeID: "node.alpha", Tags: []string{"regression", "smoke"}, Priority: "p0", Owner: "team-a", SortOrder: 2},
			{ID: "case.beta", DisplayName: "Beta Variant", NodeID: "node.alpha", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", SortOrder: 1},
			{ID: "case.gamma", DisplayName: "Gamma Other", NodeID: "node.beta", Tags: []string{"smoke"}, Priority: "p2", Owner: "team-b", Status: "paused", SortOrder: 3},
		},
	}

	cases := SelectCases(bundle, Filter{Tags: []string{"regression"}, Owner: "team-a", Status: "active"})
	if len(cases) != 2 || cases[0].ID != "case.beta" || cases[1].ID != "case.alpha" {
		t.Fatalf("selected cases = %#v", cases)
	}

	filtered := SelectCases(bundle, Filter{Filter: "variant", Tags: []string{"regression"}, Status: "active"})
	if len(filtered) != 1 || filtered[0].ID != "case.beta" {
		t.Fatalf("filtered cases = %#v", filtered)
	}
}

func TestCoverageReportsLatestStatusAndHasPassed(t *testing.T) {
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.passed", DisplayName: "Passed Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 1},
			{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 3},
		},
	}
	records := []store.APICaseRunRecord{
		record("run.failed.old", "case.failed", store.StatusPassed, base),
		record("run.passed.latest", "case.passed", store.StatusPassed, base.Add(time.Minute)),
		record("run.failed.latest", "case.failed", store.StatusFailed, base.Add(2*time.Minute)),
	}
	cases := SelectCases(bundle, Filter{Tags: []string{"regression"}, Status: "active"})

	report, err := Coverage(context.Background(), bundle, recordStore{records: records}, Filter{Tags: []string{"regression"}, Status: "active"}, cases)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if report.OK || report.Counts.Total != 3 || report.Counts.Passed != 1 || report.Counts.Failed != 1 || report.Counts.NotRun != 1 {
		t.Fatalf("coverage report = %#v", report)
	}
	byCase := map[string]Item{}
	for _, item := range report.Items {
		byCase[item.CaseID] = item
	}
	if !byCase["case.failed"].HasPassed || byCase["case.failed"].LatestStatus != store.StatusFailed || byCase["case.failed"].LatestRunID != "run.failed.latest" {
		t.Fatalf("failed case item = %#v", byCase["case.failed"])
	}
	if byCase["case.unrun"].LatestStatus != "not-run" || byCase["case.unrun"].Reason != "no run recorded in Store" {
		t.Fatalf("unrun case item = %#v", byCase["case.unrun"])
	}
}

func TestNormalizeRunStateAliases(t *testing.T) {
	for input, want := range map[string]string{
		"fail":      store.StatusFailed,
		"failed":    store.StatusFailed,
		"PASS":      store.StatusPassed,
		"never-run": "not-run",
		"missing":   "not-run",
	} {
		if got := NormalizeRunState(input); got != want {
			t.Fatalf("NormalizeRunState(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInspectReportsReadinessAndLatestState(t *testing.T) {
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.file", DisplayName: "File Case", NodeID: "node.alpha", CasePath: "cases/file.json", Tags: []string{"regression"}, SortOrder: 1},
			{ID: "case.config", DisplayName: "Config Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 2},
			{ID: "case.missing", DisplayName: "Missing Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 3},
			{ID: "case.paused", DisplayName: "Paused Case", NodeID: "node.alpha", Tags: []string{"regression"}, Status: "paused", SortOrder: 4},
		},
		TemplateConfigs: []profile.TemplateConfig{
			{ID: "config.case.config", ScopeType: "case", ScopeID: "case.config", Status: "active", ConfigJSON: `{"caseId":"case.config","caseExecution":{"method":"POST","path":"/items"}}`},
		},
	}
	records := []store.APICaseRunRecord{
		record("run.file", "case.file", store.StatusPassed, base),
		record("run.config", "case.config", store.StatusFailed, base.Add(time.Minute)),
	}
	cases := SelectCases(bundle, Filter{Tags: []string{"regression"}})

	report, err := Inspect(context.Background(), bundle, recordStore{records: records}, Filter{Tags: []string{"regression"}}, cases)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if report.OK || report.Counts.Total != 4 || report.Counts.Ready != 2 || report.Counts.Blocked != 2 || report.Counts.Failed != 1 || report.Counts.NotRun != 2 {
		t.Fatalf("inspection counts = %#v", report.Counts)
	}
	byCase := map[string]InspectionItem{}
	for _, item := range report.Items {
		byCase[item.CaseID] = item
	}
	if !byCase["case.file"].Ready || !byCase["case.file"].HasRunnableFile || byCase["case.file"].LatestStatus != store.StatusPassed {
		t.Fatalf("file case = %#v", byCase["case.file"])
	}
	if !byCase["case.config"].Ready || !byCase["case.config"].HasExecutionConfig || byCase["case.config"].LatestStatus != store.StatusFailed || byCase["case.config"].SuggestedAction != "rerun" {
		t.Fatalf("config case = %#v", byCase["case.config"])
	}
	if byCase["case.missing"].Ready || len(byCase["case.missing"].Issues) != 1 || byCase["case.missing"].SuggestedAction != "add-runnable-source" {
		t.Fatalf("missing case = %#v", byCase["case.missing"])
	}
	if byCase["case.paused"].Ready || byCase["case.paused"].SuggestedAction != "review-status" {
		t.Fatalf("paused case = %#v", byCase["case.paused"])
	}
}

func TestPlanSelectsReadyCasesAndBuildsBatchRequest(t *testing.T) {
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.passed", DisplayName: "Passed Case", NodeID: "node.alpha", CasePath: "cases/passed.json", Tags: []string{"regression"}, SortOrder: 1},
			{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.alpha", CasePath: "cases/failed.json", Tags: []string{"regression"}, SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", CasePath: "cases/unrun.json", Tags: []string{"regression"}, SortOrder: 3},
			{ID: "case.blocked", DisplayName: "Blocked Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 4},
		},
	}
	records := []store.APICaseRunRecord{
		record("run.passed", "case.passed", store.StatusPassed, base),
		record("run.failed", "case.failed", store.StatusFailed, base.Add(time.Minute)),
	}
	cases := SelectCases(bundle, Filter{Tags: []string{"regression"}, Status: "active"})

	report, err := Plan(context.Background(), bundle, recordStore{records: records}, Filter{Tags: []string{"regression"}, Status: "active"}, cases, PlanOptions{
		RequestID:      "change-001",
		BaseURL:        "http://127.0.0.1:8080",
		EvidenceDir:    ".runtime/evidence",
		TimeoutSeconds: 5,
		Actions:        []string{"run", "rerun"},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !report.OK || report.Counts.Total != 4 || report.Counts.Ready != 3 || report.Counts.Blocked != 1 || report.Counts.Selected != 2 {
		t.Fatalf("plan counts = %#v", report.Counts)
	}
	if got := strings.Join(report.CaseIDs, ","); got != "case.failed,case.unrun" {
		t.Fatalf("case ids = %q", got)
	}
	if report.BatchRequest.RequestID != "change-001" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.failed,case.unrun" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" || report.BatchRequest.TimeoutSeconds != 5 {
		t.Fatalf("batch request = %#v", report.BatchRequest)
	}
	if len(report.Blocked) != 1 || report.Blocked[0].CaseID != "case.blocked" {
		t.Fatalf("blocked = %#v", report.Blocked)
	}
}

func TestImpactPlansCasesFromChangedSignals(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.create", DisplayName: "Create Item", ServiceID: "service.alpha", Operation: "Create", Method: "POST", Path: "/v1/items", Tags: []string{"items"}, SortOrder: 1},
			{ID: "node.search", DisplayName: "Search Item", ServiceID: "service.alpha", Operation: "Search", Method: "GET", Path: "/v1/items/search", Tags: []string{"items"}, SortOrder: 2},
			{ID: "node.other", DisplayName: "Other", ServiceID: "service.beta", Operation: "Other", Method: "GET", Path: "/v1/other", SortOrder: 3},
		},
		Workflows: []profile.Workflow{
			{ID: "workflow.item", DisplayName: "Item Happy Path"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.item", StepID: "create", NodeID: "node.create", CaseID: "case.create", SortOrder: 1},
			{WorkflowID: "workflow.item", StepID: "search", NodeID: "node.search", CaseID: "case.search", SortOrder: 2},
		},
		APICases: []profile.APICase{
			{ID: "case.create", DisplayName: "Create default", NodeID: "node.create", CasePath: "cases/create.json", Tags: []string{"regression"}, Status: "active", SortOrder: 1},
			{ID: "case.search", DisplayName: "Search default", NodeID: "node.search", CasePath: "cases/search.json", Tags: []string{"regression"}, Status: "active", SortOrder: 2},
			{ID: "case.other", DisplayName: "Other default", NodeID: "node.other", CasePath: "cases/other.json", Tags: []string{"regression"}, Status: "active", SortOrder: 3},
		},
	}

	report, err := Impact(context.Background(), bundle, recordStore{}, Filter{Status: "active"}, ImpactOptions{
		Signals: []string{"/v1/items"},
		Plan: PlanOptions{
			RequestID: "change-001",
			Actions:   []string{"run"},
			BaseURL:   "http://127.0.0.1:8080",
		},
	})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if !report.OK || report.Counts.Nodes != 2 || report.Counts.Workflows != 1 || report.Plan.Counts.Selected != 2 {
		t.Fatalf("impact counts = %#v plan=%#v", report.Counts, report.Plan.Counts)
	}
	if got := strings.Join(report.Plan.CaseIDs, ","); got != "case.create,case.search" {
		t.Fatalf("impact case ids = %q", got)
	}
	if got := strings.Join(report.BatchRequest.CaseIDs, ","); got != "case.create,case.search" {
		t.Fatalf("impact batch case ids = %q", got)
	}
	if report.BatchRequest.RequestID != "change-001" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("impact batch request = %#v", report.BatchRequest)
	}
	reasons := map[string][]string{}
	for _, item := range report.Cases {
		reasons[item.CaseID] = item.Reasons
	}
	if len(reasons["case.create"]) == 0 || len(reasons["case.search"]) == 0 || len(reasons["case.other"]) != 0 {
		t.Fatalf("impact reasons = %#v", reasons)
	}
}

func TestStabilityReportsRecentStatusTransitions(t *testing.T) {
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.flaky", DisplayName: "Flaky Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 1},
			{ID: "case.stable", DisplayName: "Stable Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 2},
			{ID: "case.unrun", DisplayName: "Unrun Case", NodeID: "node.alpha", Tags: []string{"regression"}, SortOrder: 3},
		},
	}
	records := []store.APICaseRunRecord{
		record("run.flaky.1", "case.flaky", store.StatusPassed, base),
		record("run.flaky.2", "case.flaky", store.StatusFailed, base.Add(time.Minute)),
		record("run.flaky.3", "case.flaky", store.StatusPassed, base.Add(2*time.Minute)),
		record("run.stable.1", "case.stable", store.StatusFailed, base.Add(3*time.Minute)),
		record("run.stable.2", "case.stable", store.StatusFailed, base.Add(4*time.Minute)),
	}
	cases := SelectCases(bundle, Filter{Tags: []string{"regression"}, Status: "active"})

	report, err := Stability(context.Background(), bundle, recordStore{records: records}, Filter{Tags: []string{"regression"}, Status: "active"}, cases, StabilityOptions{Limit: 3})
	if err != nil {
		t.Fatalf("stability: %v", err)
	}
	if report.OK || report.Counts.Total != 3 || report.Counts.Unstable != 1 || report.Counts.Stable != 1 || report.Counts.NotRun != 1 {
		t.Fatalf("stability counts = %#v", report.Counts)
	}
	byCase := map[string]StabilityItem{}
	for _, item := range report.Items {
		byCase[item.CaseID] = item
	}
	if !byCase["case.flaky"].Unstable || byCase["case.flaky"].Transitions != 2 || byCase["case.flaky"].Passed != 2 || byCase["case.flaky"].Failed != 1 || byCase["case.flaky"].LatestStatus != store.StatusPassed {
		t.Fatalf("flaky item = %#v", byCase["case.flaky"])
	}
	if len(byCase["case.flaky"].Recent) != 3 || byCase["case.flaky"].Recent[0].RunID != "run.flaky.3" || byCase["case.flaky"].Recent[0].DetailURL == "" {
		t.Fatalf("flaky recent = %#v", byCase["case.flaky"].Recent)
	}
	if byCase["case.stable"].Unstable || byCase["case.stable"].Transitions != 0 || byCase["case.stable"].LatestStatus != store.StatusFailed {
		t.Fatalf("stable item = %#v", byCase["case.stable"])
	}
	if byCase["case.unrun"].LatestStatus != "not-run" || byCase["case.unrun"].Reason != "no run recorded in Store" {
		t.Fatalf("unrun item = %#v", byCase["case.unrun"])
	}
}

func TestPriorityRanksImpactedUnstableAndFailedCases(t *testing.T) {
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Create", Path: "/v1/items"},
			{ID: "node.beta", DisplayName: "Node Beta", Operation: "Search", Path: "/v1/items/search"},
		},
		APICases: []profile.APICase{
			{ID: "case.impacted", DisplayName: "Impacted Case", NodeID: "node.alpha", CasePath: "cases/impacted.json", Tags: []string{"regression"}, Priority: "p1", SortOrder: 1},
			{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.beta", CasePath: "cases/failed.json", Tags: []string{"regression"}, Priority: "p0", SortOrder: 2},
			{ID: "case.blocked", DisplayName: "Blocked Case", NodeID: "node.beta", Tags: []string{"regression"}, Priority: "p0", SortOrder: 3},
			{ID: "case.low", DisplayName: "Low Case", NodeID: "node.beta", CasePath: "cases/low.json", Tags: []string{"regression"}, Priority: "p2", SortOrder: 4},
		},
	}
	records := []store.APICaseRunRecord{
		record("run.impacted.1", "case.impacted", store.StatusPassed, base),
		record("run.impacted.2", "case.impacted", store.StatusFailed, base.Add(time.Minute)),
		record("run.impacted.3", "case.impacted", store.StatusPassed, base.Add(2*time.Minute)),
		record("run.failed.1", "case.failed", store.StatusFailed, base.Add(3*time.Minute)),
		record("run.low.1", "case.low", store.StatusPassed, base.Add(4*time.Minute)),
	}
	cases := SelectCases(bundle, Filter{Tags: []string{"regression"}, Status: "active"})

	report, err := Priority(context.Background(), bundle, recordStore{records: records}, Filter{Tags: []string{"regression"}, Status: "active"}, cases, PriorityOptions{
		Signals:        []string{"Create"},
		Limit:          2,
		RequestID:      "change-001",
		BaseURL:        "http://127.0.0.1:8080",
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("priority: %v", err)
	}
	if !report.OK || report.Counts.Total != 4 || report.Counts.Ready != 3 || report.Counts.Blocked != 1 || report.Counts.Selected != 2 {
		t.Fatalf("priority counts = %#v", report.Counts)
	}
	if got := strings.Join(report.CaseIDs, ","); got != "case.impacted,case.failed" {
		t.Fatalf("priority case ids = %q", got)
	}
	if report.BatchRequest.RequestID != "change-001" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.impacted,case.failed" || report.BatchRequest.TimeoutSeconds != 5 {
		t.Fatalf("priority batch = %#v", report.BatchRequest)
	}
	if report.Selected[0].Score <= report.Selected[1].Score || !containsString(report.Selected[0].Reasons, "impacted") || !containsString(report.Selected[0].Reasons, "unstable") {
		t.Fatalf("first selected = %#v", report.Selected[0])
	}
	if !containsString(report.Selected[1].Reasons, "latest failed") || !containsString(report.Selected[1].Reasons, "priority p0") {
		t.Fatalf("second selected = %#v", report.Selected[1])
	}
	if len(report.Blocked) != 1 || report.Blocked[0].CaseID != "case.blocked" {
		t.Fatalf("blocked = %#v", report.Blocked)
	}
}

func TestBriefCombinesCoverageReadinessStabilityAndPriority(t *testing.T) {
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", Operation: "Create", Path: "/v1/items"},
			{ID: "node.beta", DisplayName: "Node Beta", Operation: "Search", Path: "/v1/items/search"},
		},
		APICases: []profile.APICase{
			{ID: "case.impacted", DisplayName: "Impacted Case", NodeID: "node.alpha", CasePath: "cases/impacted.json", Tags: []string{"regression"}, Priority: "p1", SortOrder: 1},
			{ID: "case.failed", DisplayName: "Failed Case", NodeID: "node.beta", CasePath: "cases/failed.json", Tags: []string{"regression"}, Priority: "p0", SortOrder: 2},
			{ID: "case.blocked", DisplayName: "Blocked Case", NodeID: "node.beta", Tags: []string{"regression"}, Priority: "p0", SortOrder: 3},
		},
	}
	records := []store.APICaseRunRecord{
		record("run.impacted.1", "case.impacted", store.StatusPassed, base),
		record("run.impacted.2", "case.impacted", store.StatusFailed, base.Add(time.Minute)),
		record("run.impacted.3", "case.impacted", store.StatusPassed, base.Add(2*time.Minute)),
		record("run.failed.1", "case.failed", store.StatusFailed, base.Add(3*time.Minute)),
	}
	cases := SelectCases(bundle, Filter{Tags: []string{"regression"}, Status: "active"})

	report, err := Brief(context.Background(), bundle, recordStore{records: records}, Filter{Tags: []string{"regression"}, Status: "active"}, cases, BriefOptions{
		Signals:        []string{"Create"},
		Limit:          2,
		RequestID:      "change-012",
		BaseURL:        "http://127.0.0.1:8080",
		TimeoutSeconds: 6,
		StabilityLimit: 3,
	})
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	if !report.OK || report.Counts.Total != 3 || report.Counts.Ready != 2 || report.Counts.Blocked != 1 || report.Counts.Failed != 1 || report.Counts.Unstable != 1 || report.Counts.PrioritySelected != 2 {
		t.Fatalf("brief counts = %#v", report.Counts)
	}
	if got := strings.Join(report.BatchRequest.CaseIDs, ","); got != "case.impacted,case.failed" {
		t.Fatalf("brief batch ids = %q", got)
	}
	if len(report.Recommended) != 2 || report.Recommended[0].CaseID != "case.impacted" || !containsString(report.Recommended[0].Reasons, "impacted") {
		t.Fatalf("brief recommended = %#v", report.Recommended)
	}
	if len(report.Readiness) != 3 || len(report.Coverage) != 3 || len(report.Stability) != 3 {
		t.Fatalf("brief sections readiness=%d coverage=%d stability=%d", len(report.Readiness), len(report.Coverage), len(report.Stability))
	}
	if len(report.Blocked) != 1 || report.Blocked[0].CaseID != "case.blocked" {
		t.Fatalf("brief blocked = %#v", report.Blocked)
	}
}

func TestQualityAuditsMaintainedCaseAuthoringGaps(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
			{ID: "node.beta", DisplayName: "Node Beta"},
			{ID: "node.empty", DisplayName: "Node Without Cases"},
		},
		APICases: []profile.APICase{
			{ID: "case.complete", DisplayName: "Complete Case", Description: "Covers the main path.", NodeID: "node.alpha", CasePath: "cases/complete.json", Tags: []string{"regression"}, Priority: "p0", Owner: "team-a", Status: "active", SortOrder: 1},
			{ID: "case.gaps", DisplayName: "Gap Case", NodeID: "node.beta", Status: "active", SortOrder: 2},
		},
		TemplateConfigs: []profile.TemplateConfig{
			{ID: "cfg.case.complete", ScopeType: "case", ScopeID: "case.complete", Status: "active", ConfigJSON: `{"caseId":"case.complete","caseExecution":{"method":"GET","path":"/items"}}`},
		},
	}
	cases := SelectCases(bundle, Filter{Status: "active"})

	report, err := Quality(context.Background(), bundle, recordStore{}, Filter{Status: "active"}, cases)
	if err != nil {
		t.Fatalf("quality: %v", err)
	}
	if report.OK || report.Counts.Nodes != 3 || report.Counts.NodesWithoutCases != 1 || report.Counts.Cases != 2 || report.Counts.CompleteCases != 1 || report.Counts.IncompleteCases != 1 {
		t.Fatalf("quality counts = %#v", report.Counts)
	}
	if report.Counts.MissingDescription != 1 || report.Counts.MissingTags != 1 || report.Counts.MissingPriority != 1 || report.Counts.MissingOwner != 1 || report.Counts.MissingRunnable != 1 || report.Counts.MissingExecution != 1 {
		t.Fatalf("quality gap counts = %#v", report.Counts)
	}
	byCase := map[string]QualityCase{}
	for _, item := range report.Cases {
		byCase[item.CaseID] = item
	}
	if !byCase["case.complete"].Complete || len(byCase["case.complete"].Issues) != 0 {
		t.Fatalf("complete case = %#v", byCase["case.complete"])
	}
	if byCase["case.gaps"].Complete || !containsString(byCase["case.gaps"].Issues, "missing-owner") || !containsString(byCase["case.gaps"].Issues, "missing-execution-config") {
		t.Fatalf("gap case = %#v", byCase["case.gaps"])
	}
	if len(report.Nodes) != 1 || report.Nodes[0].NodeID != "node.empty" || !containsString(report.Nodes[0].Issues, "no-maintained-cases") {
		t.Fatalf("quality nodes = %#v", report.Nodes)
	}
}

func TestQualityAuditsCaseLifecycleStatus(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.active", DisplayName: "Active Case", Description: "Ready.", NodeID: "node.alpha", CasePath: "cases/active.json", Tags: []string{"regression"}, Priority: "p0", Owner: "team-a", Status: "active", SortOrder: 1},
			{ID: "case.review", DisplayName: "Review Case", Description: "Needs review.", NodeID: "node.alpha", CasePath: "cases/review.json", Tags: []string{"regression"}, Priority: "p1", Owner: "team-a", Status: "review", SortOrder: 2},
			{ID: "case.invalid", DisplayName: "Invalid Case", Description: "Bad status.", NodeID: "node.alpha", CasePath: "cases/invalid.json", Tags: []string{"regression"}, Priority: "p2", Owner: "team-a", Status: "paused", SortOrder: 3},
		},
		TemplateConfigs: []profile.TemplateConfig{
			{ID: "cfg.case.active", ScopeType: "case", ScopeID: "case.active", Status: "active", ConfigJSON: `{"caseId":"case.active","caseExecution":{"method":"GET","path":"/active"}}`},
			{ID: "cfg.case.review", ScopeType: "case", ScopeID: "case.review", Status: "active", ConfigJSON: `{"caseId":"case.review","caseExecution":{"method":"GET","path":"/review"}}`},
			{ID: "cfg.case.invalid", ScopeType: "case", ScopeID: "case.invalid", Status: "active", ConfigJSON: `{"caseId":"case.invalid","caseExecution":{"method":"GET","path":"/invalid"}}`},
		},
	}
	cases := SelectCases(bundle, Filter{})

	report, err := Quality(context.Background(), bundle, recordStore{}, Filter{}, cases)
	if err != nil {
		t.Fatalf("quality: %v", err)
	}
	if report.OK || report.Counts.Cases != 3 || report.Counts.CompleteCases != 1 || report.Counts.IncompleteCases != 2 || report.Counts.NonExecutableLifecycle != 2 || report.Counts.InvalidStatus != 1 {
		t.Fatalf("quality lifecycle counts = %#v", report.Counts)
	}
	byCase := map[string]QualityCase{}
	for _, item := range report.Cases {
		byCase[item.CaseID] = item
	}
	if !byCase["case.active"].Complete {
		t.Fatalf("active case = %#v", byCase["case.active"])
	}
	if byCase["case.review"].Complete || byCase["case.review"].Lifecycle != "review" || !containsString(byCase["case.review"].Issues, "non-executable-lifecycle") {
		t.Fatalf("review case = %#v", byCase["case.review"])
	}
	if byCase["case.invalid"].Complete || byCase["case.invalid"].Lifecycle != "invalid" || !containsString(byCase["case.invalid"].Issues, "invalid-status") {
		t.Fatalf("invalid case = %#v", byCase["case.invalid"])
	}

	plan, err := QualityPlan(context.Background(), bundle, recordStore{}, Filter{}, cases)
	if err != nil {
		t.Fatalf("quality plan: %v", err)
	}
	if plan.Counts.ReviewLifecycle != 2 {
		t.Fatalf("quality plan counts = %#v", plan.Counts)
	}
	lifecycleActions := 0
	for _, action := range plan.Actions {
		if action.Type == "review-case-lifecycle" {
			lifecycleActions++
			if !containsString(action.Issues, "non-executable-lifecycle") && !containsString(action.Issues, "invalid-status") {
				t.Fatalf("lifecycle action missing lifecycle issue = %#v", action)
			}
		}
	}
	if lifecycleActions != 2 {
		t.Fatalf("lifecycle action count = %d actions=%#v", lifecycleActions, plan.Actions)
	}
}

func TestQualityAcceptsExternalExecutorSourceAsRunnable(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
		},
		Executors: []profile.ExecutorDescriptor{
			{ID: "executor.karate", Kind: "karate", SourcePath: "tests/api.feature", Status: "active"},
		},
		APICases: []profile.APICase{
			{
				ID:          "case.karate",
				DisplayName: "Karate Case",
				Description: "Runs through an external Karate feature.",
				NodeID:      "node.alpha",
				Tags:        []string{"regression"},
				Priority:    "p0",
				Owner:       "team-a",
				Status:      "active",
				SourceKind:  "karate",
				SourcePath:  "tests/api.feature",
				ExecutorID:  "executor.karate",
			},
			{
				ID:          "case.missing-executor",
				DisplayName: "Missing Executor Case",
				Description: "References an external source without an executor.",
				NodeID:      "node.alpha",
				Tags:        []string{"regression"},
				Priority:    "p1",
				Owner:       "team-a",
				Status:      "active",
				SourceKind:  "karate",
				SourcePath:  "tests/missing.feature",
				ExecutorID:  "executor.missing",
			},
		},
	}
	cases := SelectCases(bundle, Filter{Status: "active"})

	report, err := Quality(context.Background(), bundle, recordStore{}, Filter{Status: "active"}, cases)
	if err != nil {
		t.Fatalf("quality: %v", err)
	}
	if report.OK || report.Counts.Cases != 2 || report.Counts.CompleteCases != 1 || report.Counts.IncompleteCases != 1 || report.Counts.MissingRunnable != 0 || report.Counts.MissingExecution != 1 {
		t.Fatalf("quality external source counts = %#v", report.Counts)
	}
	byCase := map[string]QualityCase{}
	for _, item := range report.Cases {
		byCase[item.CaseID] = item
	}
	if !byCase["case.karate"].Complete || !byCase["case.karate"].HasRunnableFile || !byCase["case.karate"].HasExecutionConfig {
		t.Fatalf("karate case = %#v", byCase["case.karate"])
	}
	if byCase["case.missing-executor"].Complete || !containsString(byCase["case.missing-executor"].Issues, "missing-executor") {
		t.Fatalf("missing executor case = %#v", byCase["case.missing-executor"])
	}
}

func TestQualityPlanBuildsActionableAuthoringSteps(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha"},
			{ID: "node.empty", DisplayName: "Node Empty"},
		},
		APICases: []profile.APICase{
			{ID: "case.complete", DisplayName: "Complete Case", Description: "Covers the main path.", NodeID: "node.alpha", CasePath: "cases/complete.json", Tags: []string{"regression"}, Priority: "p0", Owner: "team-a", Status: "active", SortOrder: 1},
			{ID: "case.gaps", DisplayName: "Gap Case", NodeID: "node.alpha", Status: "active", SortOrder: 2},
		},
		TemplateConfigs: []profile.TemplateConfig{
			{ID: "cfg.case.complete", ScopeType: "case", ScopeID: "case.complete", Status: "active", ConfigJSON: `{"caseId":"case.complete","caseExecution":{"method":"GET","path":"/items"}}`},
		},
	}
	cases := SelectCases(bundle, Filter{Status: "active"})

	report, err := QualityPlan(context.Background(), bundle, recordStore{}, Filter{Status: "active"}, cases)
	if err != nil {
		t.Fatalf("quality plan: %v", err)
	}
	if !report.OK || report.Counts.Total != 4 || report.Counts.DraftCase != 1 || report.Counts.CompleteMetadata != 1 || report.Counts.AddRunnable != 1 || report.Counts.AddExecution != 1 {
		t.Fatalf("quality plan counts = %#v", report.Counts)
	}
	actionsByType := map[string]QualityPlanAction{}
	for _, action := range report.Actions {
		actionsByType[action.Type] = action
	}
	if actionsByType["draft-case"].NodeID != "node.empty" || actionsByType["draft-case"].SuggestedCaseID != "case.node-empty.default" || !containsString(actionsByType["draft-case"].Command, "draft") {
		t.Fatalf("draft action = %#v", actionsByType["draft-case"])
	}
	if actionsByType["complete-case-metadata"].CaseID != "case.gaps" || !containsString(actionsByType["complete-case-metadata"].Fields, "owner") {
		t.Fatalf("metadata action = %#v", actionsByType["complete-case-metadata"])
	}
	if actionsByType["add-runnable-source"].CaseID != "case.gaps" {
		t.Fatalf("runnable action = %#v", actionsByType["add-runnable-source"])
	}
	if actionsByType["add-execution-config"].CaseID != "case.gaps" {
		t.Fatalf("execution action = %#v", actionsByType["add-execution-config"])
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

type recordStore struct {
	records []store.APICaseRunRecord
}

func (s recordStore) ListAPICaseRunRecordsForCaseIDs(context.Context, []string) ([]store.APICaseRunRecord, error) {
	return s.records, nil
}

func (s recordStore) ListRuns(context.Context) ([]store.Run, error) {
	return nil, nil
}

func (s recordStore) ListAPICaseRuns(context.Context, string) ([]store.APICaseRun, error) {
	return nil, nil
}

func record(runID string, caseID string, status string, at time.Time) store.APICaseRunRecord {
	return store.APICaseRunRecord{
		Run: store.Run{
			ID:        runID,
			ProfileID: "sample",
			Status:    status,
			CreatedAt: at,
			UpdatedAt: at.Add(time.Second),
		},
		CaseRun: store.APICaseRun{
			ID:         runID + ".case",
			RunID:      runID,
			CaseID:     caseID,
			Status:     status,
			StartedAt:  at,
			FinishedAt: at.Add(time.Second),
			CreatedAt:  at,
		},
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}
