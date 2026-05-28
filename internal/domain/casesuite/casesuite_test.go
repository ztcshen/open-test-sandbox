package casesuite

import (
	"context"
	"strings"
	"testing"
	"time"

	domaincatalog "agent-testbench/internal/domain/catalog"
	"agent-testbench/internal/domain/execution"
	"agent-testbench/internal/domain/profile"
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
	records := []execution.APICaseRunRecord{
		record("run.failed.old", "case.failed", execution.StatusPassed, base),
		record("run.passed.latest", "case.passed", execution.StatusPassed, base.Add(time.Minute)),
		record("run.failed.latest", "case.failed", execution.StatusFailed, base.Add(2*time.Minute)),
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
	if !byCase["case.failed"].HasPassed || byCase["case.failed"].LatestStatus != execution.StatusFailed || byCase["case.failed"].LatestRunID != "run.failed.latest" {
		t.Fatalf("failed case item = %#v", byCase["case.failed"])
	}
	if byCase["case.unrun"].LatestStatus != "not-run" || byCase["case.unrun"].Reason != "no run recorded in Store" {
		t.Fatalf("unrun case item = %#v", byCase["case.unrun"])
	}
}

func TestNormalizeRunStateAliases(t *testing.T) {
	for input, want := range map[string]string{
		"fail":      execution.StatusFailed,
		"failed":    execution.StatusFailed,
		"PASS":      execution.StatusPassed,
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
			{ID: "config.case.config", ScopeType: "case", ScopeID: "case.config", Status: "active", ConfigJSON: `{"caseId":"case.config","caseExecution":{"method":"POST","path":"/items","body":{"id":"item-001"}}}`},
		},
	}
	records := []execution.APICaseRunRecord{
		record("run.file", "case.file", execution.StatusPassed, base),
		record("run.config", "case.config", execution.StatusFailed, base.Add(time.Minute)),
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
	if !byCase["case.file"].Ready || !byCase["case.file"].HasRunnableFile || byCase["case.file"].LatestStatus != execution.StatusPassed {
		t.Fatalf("file case = %#v", byCase["case.file"])
	}
	if !byCase["case.config"].Ready || !byCase["case.config"].HasExecutionConfig || byCase["case.config"].LatestStatus != execution.StatusFailed || byCase["case.config"].SuggestedAction != "rerun" {
		t.Fatalf("config case = %#v", byCase["case.config"])
	}
	if byCase["case.missing"].Ready || len(byCase["case.missing"].Issues) != 1 || byCase["case.missing"].SuggestedAction != "add-runnable-source" {
		t.Fatalf("missing case = %#v", byCase["case.missing"])
	}
	if byCase["case.paused"].Ready || byCase["case.paused"].SuggestedAction != "review-status" {
		t.Fatalf("paused case = %#v", byCase["case.paused"])
	}
}

func TestExecutionConfigSetDoesNotMarkBodylessWriteConfigRunnable(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		APICases: []profile.APICase{
			{ID: "case.post.payload", RenderMode: "template_patch", PayloadTemplateJSON: `{"id":"item-001"}`, PatchJSON: `[{"op":"add","path":"$.mode","value":"ok"}]`},
			{ID: "case.post.template", RequestTemplateID: "tpl.post", RenderMode: "template_patch", PatchJSON: `[{"op":"add","path":"$.mode","value":"ok"}]`},
			{ID: "case.post.no-patch", PayloadTemplateJSON: `{"id":"item-003"}`},
		},
		RequestTemplates: []profile.RequestTemplate{
			{ID: "tpl.post", Method: "POST", Path: "/items", TemplateJSON: `{"id":"item-002"}`},
		},
		TemplateConfigs: []profile.TemplateConfig{
			{ID: "cfg.case.post.empty", ScopeType: "case", ScopeID: "case.post.empty", Status: "active", ConfigJSON: `{"caseId":"case.post.empty","caseExecution":{"method":"POST","path":"/items"}}`},
			{ID: "cfg.case.post.body", ScopeType: "case", ScopeID: "case.post.body", Status: "active", ConfigJSON: `{"caseId":"case.post.body","caseExecution":{"method":"POST","path":"/items","body":{"id":"item-001"}}}`},
			{ID: "cfg.case.post.payload", ScopeType: "case", ScopeID: "case.post.payload", Status: "active", ConfigJSON: `{"caseId":"case.post.payload","caseExecution":{"method":"POST","path":"/items"}}`},
			{ID: "cfg.case.post.template", ScopeType: "case", ScopeID: "case.post.template", Status: "active", ConfigJSON: `{"caseId":"case.post.template","caseExecution":{"method":"POST","path":"/items"}}`},
			{ID: "cfg.case.post.no-patch", ScopeType: "case", ScopeID: "case.post.no-patch", Status: "active", ConfigJSON: `{"caseId":"case.post.no-patch","caseExecution":{"method":"POST","path":"/items"}}`},
			{ID: "cfg.case.scoped.get", ScopeType: "case", ScopeID: "case.scoped.get", Status: "active", ConfigJSON: `{"caseExecution":{"method":"GET","path":"/items"}}`},
			{ID: "cfg.case.get.empty", ScopeType: "case", ScopeID: "case.get.empty", Status: "active", ConfigJSON: `{"caseId":"case.get.empty","caseExecution":{"method":"GET","path":"/items"}}`},
		},
	}

	configs := ExecutionConfigSet(context.Background(), bundle, recordStore{})
	for _, caseID := range []string{"case.post.empty", "case.post.no-patch"} {
		if configs[caseID] {
			t.Fatalf("bodyless POST config %s should not be treated as runnable: %#v", caseID, configs)
		}
	}
	for _, caseID := range []string{"case.post.body", "case.post.payload", "case.post.template", "case.scoped.get", "case.get.empty"} {
		if !configs[caseID] {
			t.Fatalf("usable execution config %s missing: %#v", caseID, configs)
		}
	}
}

func TestExecutionConfigSetUsesCatalogCaseScopeAndBodySources(t *testing.T) {
	runtime := recordStore{
		catalog: &domaincatalog.ProfileCatalog{
			APICases: []domaincatalog.APICase{
				{ID: "case.catalog.payload", PayloadTemplateJSON: `{"id":"item-001"}`, RenderMode: "template_patch", PatchJSON: `[{"op":"add","path":"$.mode","value":"ok"}]`},
			},
			TemplateConfigs: []domaincatalog.TemplateConfig{
				{ID: "cfg.catalog.scoped.get", ScopeType: "case", ScopeID: "case.catalog.scoped", Status: "active", ConfigJSON: `{"caseExecution":{"method":"GET","path":"/items"}}`},
				{ID: "cfg.catalog.payload", ScopeType: "case", ScopeID: "case.catalog.payload", Status: "active", ConfigJSON: `{"caseId":"case.catalog.payload","caseExecution":{"method":"POST","path":"/items"}}`},
				{ID: "cfg.catalog.empty", ScopeType: "case", ScopeID: "case.catalog.empty", Status: "active", ConfigJSON: `{"caseId":"case.catalog.empty","caseExecution":{"method":"POST","path":"/items"}}`},
			},
		},
	}

	configs := ExecutionConfigSet(context.Background(), profile.Bundle{ID: "sample"}, runtime)
	if !configs["case.catalog.scoped"] || !configs["case.catalog.payload"] {
		t.Fatalf("usable execution configs missing: %#v", configs)
	}
	if configs["case.catalog.empty"] {
		t.Fatalf("bodyless catalog POST config should not be treated as runnable: %#v", configs)
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
	records := []execution.APICaseRunRecord{
		record("run.passed", "case.passed", execution.StatusPassed, base),
		record("run.failed", "case.failed", execution.StatusFailed, base.Add(time.Minute)),
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
	records := []execution.APICaseRunRecord{
		record("run.flaky.1", "case.flaky", execution.StatusPassed, base),
		record("run.flaky.2", "case.flaky", execution.StatusFailed, base.Add(time.Minute)),
		record("run.flaky.3", "case.flaky", execution.StatusPassed, base.Add(2*time.Minute)),
		record("run.stable.1", "case.stable", execution.StatusFailed, base.Add(3*time.Minute)),
		record("run.stable.2", "case.stable", execution.StatusFailed, base.Add(4*time.Minute)),
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
	if !byCase["case.flaky"].Unstable || byCase["case.flaky"].Transitions != 2 || byCase["case.flaky"].Passed != 2 || byCase["case.flaky"].Failed != 1 || byCase["case.flaky"].LatestStatus != execution.StatusPassed {
		t.Fatalf("flaky item = %#v", byCase["case.flaky"])
	}
	if len(byCase["case.flaky"].Recent) != 3 || byCase["case.flaky"].Recent[0].RunID != "run.flaky.3" || byCase["case.flaky"].Recent[0].DetailURL == "" {
		t.Fatalf("flaky recent = %#v", byCase["case.flaky"].Recent)
	}
	if byCase["case.stable"].Unstable || byCase["case.stable"].Transitions != 0 || byCase["case.stable"].LatestStatus != execution.StatusFailed {
		t.Fatalf("stable item = %#v", byCase["case.stable"])
	}
	if byCase["case.unrun"].LatestStatus != "not-run" || byCase["case.unrun"].Reason != "no run recorded in Store" {
		t.Fatalf("unrun item = %#v", byCase["case.unrun"])
	}
}

func TestPriorityRanksImpactedUnstableAndFailedCases(t *testing.T) {
	fixture := writePriorityScenario(t, true)
	cases := SelectCases(fixture.bundle, Filter{Tags: []string{"regression"}, Status: "active"})

	report, err := Priority(context.Background(), fixture.bundle, recordStore{records: fixture.records}, Filter{Tags: []string{"regression"}, Status: "active"}, cases, PriorityOptions{
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
	fixture := writePriorityScenario(t, false)
	cases := SelectCases(fixture.bundle, Filter{Tags: []string{"regression"}, Status: "active"})

	report, err := Brief(context.Background(), fixture.bundle, recordStore{records: fixture.records}, Filter{Tags: []string{"regression"}, Status: "active"}, cases, BriefOptions{
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
