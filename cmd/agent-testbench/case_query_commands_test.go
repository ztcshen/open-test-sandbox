package main

import (
	"strings"
	"testing"
)

func TestCaseRunsCommandListsStoredCaseRuns(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-runs-pg")
	runCaseRunsCommandListsStoredCaseRuns(t, storeRef, "PostgreSQL")
}

func TestCaseRunsCommandUsesNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-case-runs-mysql")
	runCaseRunsCommandListsStoredCaseRuns(t, storeRef, "MySQL")
}

func runCaseRunsCommandListsStoredCaseRuns(t *testing.T, storeRef string, label string) {
	t.Helper()
	fixture := seedCaseRunsCommandFixture(t, storeRef, label)
	out := runCLI(t, "case", "runs", "--run", fixture.runID, "--json")
	assertCaseRunsReport(t, label, out, fixture)
}

func TestCaseEvidenceCommandReadsCaseRunEvidence(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-evidence-pg")
	runCaseEvidenceCommandReadsCaseRunEvidence(t, storeRef, "PostgreSQL")
}

func TestCaseEvidenceCommandUsesNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-case-evidence-mysql")
	runCaseEvidenceCommandReadsCaseRunEvidence(t, storeRef, "MySQL")
}

func runCaseEvidenceCommandReadsCaseRunEvidence(t *testing.T, storeRef string, label string) {
	t.Helper()
	fixture := seedCaseEvidenceCommandFixture(t, storeRef, label)
	out := runCLI(t, "case", "evidence", "--case-run", fixture.caseRunID, "--json")
	assertCaseEvidencePayload(t, label, out, fixture)
}

func TestCaseTimingCommandSummarizesStoredCaseRuns(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-case-timing-pg")
	runCaseTimingCommandSummarizesStoredCaseRuns(t, storeRef, "PostgreSQL")
}

func TestCaseTimingCommandUsesNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-case-timing-mysql")
	runCaseTimingCommandSummarizesStoredCaseRuns(t, storeRef, "MySQL")
}

func runCaseTimingCommandSummarizesStoredCaseRuns(t *testing.T, storeRef string, label string) {
	t.Helper()
	fixture := seedCaseTimingCommandFixture(t, storeRef, label)
	out := runCLI(t, "case", "timing", "--kind", "case", "--max-age-minutes", "1", "--json")
	assertCaseTimingPayload(t, label, out, fixture)
}

func TestCaseQueryCommandsAcceptStoreFlag(t *testing.T) {
	storeRef := createCaseQueryStoreFlagStore(t)
	assertCaseQueryStoreFlagCommands(t, storeRef)
}

func TestCaseReadCommandsUseNamedSQLiteActiveStore(t *testing.T) {
	configureNamedSQLiteActiveStore(t, "daily-case-read-sqlite")
	runID := uniqueTestID(t, "case-run-sqlite")
	createStoredCaseRun(t, runID, "SQLite")

	if out := runCLI(t, "case", "runs", "--json"); !strings.Contains(out, runID) {
		t.Fatalf("SQLite case runs output = %q", out)
	}
	if out := runCLI(t, "case", "evidence", "--case-run", runID+".case", "--json"); !strings.Contains(out, runID) || !strings.Contains(out, "/v1/items") {
		t.Fatalf("SQLite case evidence output = %q", out)
	}
	if out := runCLI(t, "case", "timing", "--kind", "case", "--json"); !strings.Contains(out, `"caseRunCount": 1`) {
		t.Fatalf("SQLite case timing output = %q", out)
	}
}
