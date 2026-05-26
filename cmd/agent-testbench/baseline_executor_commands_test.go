package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agent-testbench/internal/store"
)

type executorPlanCommandReport struct {
	OK        bool               `json:"ok"`
	ProfileID string             `json:"profileId"`
	Counts    executorPlanCounts `json:"counts"`
	Items     []executorPlanItem `json:"items"`
}

type executorPlanCounts struct {
	Total   int `json:"total"`
	Ready   int `json:"ready"`
	Blocked int `json:"blocked"`
}

type executorPlanItem struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	SourcePath     string   `json:"sourcePath"`
	Ready          bool     `json:"ready"`
	RunMode        string   `json:"runMode"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	Issues         []string `json:"issues"`
}

func TestExecutorPlanCommandReportsProfileDescriptors(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-executor-plan-pg")
	runExecutorPlanCommandReportsProfileDescriptors(t, storeRef, "PostgreSQL")
}

func TestExecutorPlanCommandUsesNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-executor-plan-mysql")
	runExecutorPlanCommandReportsProfileDescriptors(t, storeRef, "MySQL")
}

func runExecutorPlanCommandReportsProfileDescriptors(t *testing.T, storeRef string, label string) {
	t.Helper()
	seedExecutorPlanProfileCatalog(t, storeRef, label)

	report := runExecutorPlanJSON(t, label)
	requireExecutorPlanSummary(t, label, report)
	requireExecutorPlanItems(t, label, report)
	requireExecutorPlanText(t, label)
}

func seedExecutorPlanProfileCatalog(t *testing.T, storeRef string, label string) {
	t.Helper()

	s, err := openStore(context.Background(), storeRef)
	if err != nil {
		t.Fatalf("open %s executor store: %v", label, err)
	}
	if err := s.ReplaceProfileCatalog(context.Background(), store.ProfileCatalog{
		ProfileID: "current",
		APICases: []store.CatalogAPICase{
			{ID: "case.catalog", DisplayName: "Catalog Case", SourceKind: "pytest", SourcePath: "tests/catalog_test.py", ExecutorID: "executor.catalog", Status: "active", TimeoutSeconds: 11},
			{ID: "case.blocked", DisplayName: "Blocked Case", SourceKind: "pytest", ExecutorID: "executor.blocked", Status: "active"},
		},
	}); err != nil {
		t.Fatalf("seed %s executor store: %v", label, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close %s executor store: %v", label, err)
	}
}

func runExecutorPlanJSON(t *testing.T, label string) executorPlanCommandReport {
	t.Helper()

	out := runCLI(t, "executor", "plan", "--json")
	var report executorPlanCommandReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s executor plan json: %v\n%s", label, err, out)
	}
	return report
}

func requireExecutorPlanSummary(t *testing.T, label string, report executorPlanCommandReport) {
	t.Helper()

	if report.OK || report.ProfileID != "current" || report.Counts.Total != 2 || report.Counts.Ready != 1 || report.Counts.Blocked != 1 {
		t.Fatalf("%s executor plan summary = %#v", label, report)
	}
}

func requireExecutorPlanItems(t *testing.T, label string, report executorPlanCommandReport) {
	t.Helper()

	itemsByID := map[string]executorPlanItem{}
	for _, item := range report.Items {
		itemsByID[item.ID] = item
	}
	blocked := itemsByID["executor.blocked"]
	if blocked.ID == "" || blocked.Ready || !containsString(blocked.Issues, "missing-source-path") {
		t.Fatalf("%s blocked executor item = %#v", label, blocked)
	}
	ready := itemsByID["executor.catalog"]
	if ready.ID == "" || ready.Kind != "pytest" || ready.SourcePath != "tests/catalog_test.py" || !ready.Ready || ready.RunMode != "dry-run" || ready.TimeoutSeconds != 11 {
		t.Fatalf("%s ready executor item = %#v", label, ready)
	}
}

func requireExecutorPlanText(t *testing.T, label string) {
	t.Helper()

	textOut := runCLI(t, "executor", "plan")
	for _, want := range []string{"Executor Plan", "Profile: current", "Ready: 1", "Blocked: 1", "missing-source-path"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("%s executor plan text missing %q:\n%s", label, want, textOut)
		}
	}
}

func TestBaselineGateCommandsSetAndGetState(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-baseline-pg")
	runBaselineGateCommandsSetAndGetState(t, "PostgreSQL")
}

func TestBaselineGateCommandsUseNamedMySQLActiveStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "daily-baseline-mysql")
	runBaselineGateCommandsSetAndGetState(t, "MySQL")
}

func runBaselineGateCommandsSetAndGetState(t *testing.T, label string) {
	t.Helper()
	subjectID := uniqueTestID(t, "workflow.alpha")

	out := runCLI(t, "baseline", "set", "--profile", "sample", "--subject", subjectID, "--status", "passed", "--required")
	if !strings.Contains(out, "Baseline Gate: sample "+subjectID) || !strings.Contains(out, "Status: passed") {
		t.Fatalf("%s baseline set output = %q", label, out)
	}

	out = runCLI(t, "baseline", "get", "--profile", "sample", "--subject", subjectID)
	for _, want := range []string{"Baseline Gate: sample " + subjectID, "Status: passed", "Required: true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s baseline get output missing %q: %q", label, want, out)
		}
	}
}

func TestBaselineGetCommandRejectsMissingGate(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-baseline-missing-pg")
	runBaselineGetCommandRejectsMissingGate(t, "PostgreSQL")
}

func TestBaselineGetCommandRejectsMissingGateWithMySQLStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "daily-baseline-missing-mysql")
	runBaselineGetCommandRejectsMissingGate(t, "MySQL")
}

func runBaselineGetCommandRejectsMissingGate(t *testing.T, label string) {
	t.Helper()
	subjectID := uniqueTestID(t, "workflow.missing")

	out := runCLIFails(t, "baseline", "get", "--profile", "sample", "--subject", subjectID)
	if !strings.Contains(out, "baseline gate not found") || !strings.Contains(out, "sample "+subjectID) {
		t.Fatalf("%s missing baseline gate output = %q", label, out)
	}
}
