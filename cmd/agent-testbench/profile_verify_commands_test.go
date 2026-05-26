package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

type profileVerifyCheckResult struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
}

type profileVerifyRuntimeReport struct {
	OK      bool                        `json:"ok"`
	Summary profileVerifyRuntimeSummary `json:"summary"`
	Checks  []profileVerifyCheckResult  `json:"checks"`
}

type profileVerifyRuntimeSummary struct {
	TotalChecks          int  `json:"totalChecks"`
	PassedChecks         int  `json:"passedChecks"`
	FailedChecks         int  `json:"failedChecks"`
	RequiredCaseRuns     bool `json:"requiredCaseRuns"`
	RequiredWorkflowRuns bool `json:"requiredWorkflowRuns"`
}

type profileImportCommandReport struct {
	ProfileID  string   `json:"profileId"`
	BundlePath string   `json:"bundlePath"`
	ReadModels []string `json:"readModels"`
}

type namedProfileVerifyReport struct {
	OK      bool `json:"ok"`
	Publish struct {
		ProfileID  string   `json:"profileId"`
		ReadModels []string `json:"readModels"`
	} `json:"publish"`
	Summary profileVerifyRuntimeSummary `json:"summary"`
	Checks  []profileVerifyCheckResult  `json:"checks"`
}

func TestProfileVerifyCommandAuditsPublishesAndChecksReadModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)

	out := runCLI(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--json")

	var report struct {
		OK    bool `json:"ok"`
		Audit struct {
			OK         bool `json:"ok"`
			IssueCount int  `json:"issueCount"`
		} `json:"audit"`
		Publish struct {
			ProfileID  string   `json:"profileId"`
			ReadModels []string `json:"readModels"`
		} `json:"publish"`
		Checks []struct {
			Name   string `json:"name"`
			OK     bool   `json:"ok"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile verify report: %v\n%s", err, out)
	}
	if !report.OK || !report.Audit.OK || report.Audit.IssueCount != 0 || report.Publish.ProfileID != "empty" {
		t.Fatalf("profile verify report = %#v", report)
	}
	if strings.Join(report.Publish.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("profile verify read models = %#v", report.Publish.ReadModels)
	}
	if len(report.Checks) < 5 {
		t.Fatalf("profile verify checks = %#v", report.Checks)
	}
	for _, check := range report.Checks {
		if !check.OK || check.Detail == "" {
			t.Fatalf("profile verify check = %#v", check)
		}
	}
	if got := sqliteScalar(t, dbPath, "select value from kv where key = 'active_profile_id';"); got != "empty" {
		t.Fatalf("active profile id after verify = %q", got)
	}
}

func TestProfileVerifyCommandAcceptsPackedArchive(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)
	archivePath := filepath.Join(t.TempDir(), "empty-profile.tgz")
	runCLI(t, "profile", "pack", "--profile", profileDir, "--output", archivePath)
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "verify", "--profile", archivePath, "--profile-home", profileHome, "--store", "sqlite://"+dbPath, "--json")

	var report struct {
		OK      bool `json:"ok"`
		Publish struct {
			ProfileID  string `json:"profileId"`
			BundlePath string `json:"bundlePath"`
		} `json:"publish"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile verify archive report: %v\n%s", err, out)
	}
	targetPath := filepath.Join(profileHome, "empty")
	if !report.OK || report.Publish.ProfileID != "empty" || report.Publish.BundlePath != targetPath {
		t.Fatalf("profile verify archive report = %#v", report)
	}
	if got := sqliteScalar(t, dbPath, "select bundle_path from profile_indexes where profile_id = 'empty';"); got != targetPath {
		t.Fatalf("archive verify profile index path = %q, want %q", got, targetPath)
	}
}

func TestProfileVerifyCommandStopsBeforePublishWhenAuditFails(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.missing"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+storePath)
	if !strings.Contains(out, "profile audit failed") || !strings.Contains(out, "api-case-node-missing") {
		t.Fatalf("profile verify failure output = %q", out)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if _, err := s.GetProfileIndex(context.Background(), "sample"); err == nil {
		t.Fatalf("profile verify wrote profile index after audit failure")
	} else if err != store.ErrNotFound {
		t.Fatalf("get profile index after verify failure: %v", err)
	}
}

func TestProfileVerifyCommandCanRequirePassedAPICaseRuns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeInterfaceNodeCaseProfile(t)
	seedProfileVerifyPassedCaseRun(t, dbPath, "run-alpha", "case-run-alpha", "case.alpha", "2026-05-14T01:00:00Z")
	requireProfileVerifyMissingCaseRun(t, profileDir, dbPath)
	seedProfileVerifyPassedCaseRun(t, dbPath, "run-beta", "case-run-beta", "case.beta", "2026-05-14T01:01:00Z")

	report := runProfileVerifyCaseRunsJSON(t, profileDir, dbPath)
	requireProfileVerifyCaseRunReport(t, report)
}

func seedProfileVerifyPassedCaseRun(t *testing.T, dbPath string, runID string, caseRunID string, caseID string, startAt string) {
	t.Helper()

	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	startedAt := mustParseTime(t, startAt)
	finishedAt := startedAt.Add(time.Second)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         runID,
		ProfileID:  "sample",
		WorkflowID: caseID,
		Status:     store.StatusPassed,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		CreatedAt:  finishedAt,
		UpdatedAt:  finishedAt,
	}); err != nil {
		t.Fatalf("create %s run: %v", caseID, err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         caseRunID,
		RunID:      runID,
		CaseID:     caseID,
		Status:     store.StatusPassed,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		CreatedAt:  finishedAt,
	}); err != nil {
		t.Fatalf("record %s case run: %v", caseID, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

func requireProfileVerifyMissingCaseRun(t *testing.T, profileDir string, dbPath string) {
	t.Helper()

	missing := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-case-runs")
	if !strings.Contains(missing, "api-case-run:case.beta") || !strings.Contains(missing, "no passed run") {
		t.Fatalf("missing case run verify output = %q", missing)
	}
	missingJSON := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-case-runs", "--json")
	for _, want := range []string{`"ok": false`, `"firstFailed": "api-case-run:case.beta"`, `"name": "api-case-run:case.beta"`} {
		if !strings.Contains(missingJSON, want) {
			t.Fatalf("missing case run json output does not contain %q:\n%s", want, missingJSON)
		}
	}
}

func runProfileVerifyCaseRunsJSON(t *testing.T, profileDir string, dbPath string) profileVerifyRuntimeReport {
	t.Helper()

	out := runCLI(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-case-runs", "--json")
	var report profileVerifyRuntimeReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile verify runtime report: %v\n%s", err, out)
	}
	return report
}

func requireProfileVerifyCaseRunReport(t *testing.T, report profileVerifyRuntimeReport) {
	t.Helper()

	if !report.OK || !hasProfileVerifyCheck(report.Checks, "api-case-run:case.alpha") || !hasProfileVerifyCheck(report.Checks, "api-case-run:case.beta") {
		t.Fatalf("profile verify runtime report = %#v", report)
	}
}

func TestProfileVerifyCommandCanRequirePassedWorkflowRuns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := filepath.Join(t.TempDir(), "profile")
	writeWorkflowProfile(t, profileDir)

	missing := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-workflow-runs")
	if !strings.Contains(missing, "workflow-run:workflow.alpha") || !strings.Contains(missing, "no passed run") {
		t.Fatalf("missing workflow run verify output = %q", missing)
	}

	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.workflow.alpha",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T02:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T02:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T02:00:01Z"),
		UpdatedAt:  mustParseTime(t, "2026-05-14T02:00:01Z"),
	}); err != nil {
		t.Fatalf("create workflow run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t, "profile", "verify", "--profile", profileDir, "--store", "sqlite://"+dbPath, "--require-workflow-runs", "--json")
	var report profileVerifyRuntimeReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile verify workflow report: %v\n%s", err, out)
	}
	if !report.OK || !hasProfileVerifyCheck(report.Checks, "workflow-run:workflow.alpha") {
		t.Fatalf("profile verify workflow report = %#v", report)
	}
	if report.Summary.TotalChecks != len(report.Checks) || report.Summary.PassedChecks != len(report.Checks) || report.Summary.FailedChecks != 0 {
		t.Fatalf("profile verify summary counts = %#v checks=%d", report.Summary, len(report.Checks))
	}
	if !report.Summary.RequiredWorkflowRuns || report.Summary.RequiredCaseRuns {
		t.Fatalf("profile verify summary gates = %#v", report.Summary)
	}
}

func TestProfileImportAndVerifyUseNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-profile-pg")
	runProfileImportAndVerifyUseNamedActiveStore(t, storeRef, "pg", "PostgreSQL")
}

func TestProfileImportAndVerifyUseNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-profile-mysql")
	runProfileImportAndVerifyUseNamedActiveStore(t, storeRef, "mysql", "MySQL")
}

func runProfileImportAndVerifyUseNamedActiveStore(t *testing.T, storeRef string, runLabel string, label string) {
	t.Helper()
	importDir := importProfileAndRequireNamedStoreIndex(t, storeRef, label)
	verifyDir := seedNamedProfileVerifyCaseRuns(t, storeRef, runLabel, label)
	verifyReport := runNamedProfileVerifyJSON(t, verifyDir, label)
	requireNamedProfileVerifyReport(t, label, verifyReport)
	requireNamedProfileVerifyStoreState(t, storeRef, label, importDir, verifyDir)
}

func importProfileAndRequireNamedStoreIndex(t *testing.T, storeRef string, label string) string {
	t.Helper()

	importDir := writeEmptyProfileBundle(t)
	importOut := runCLI(t, "profile", "import", "--from", importDir, "--json")
	var importReport profileImportCommandReport
	if err := json.Unmarshal([]byte(importOut), &importReport); err != nil {
		t.Fatalf("decode %s profile import json: %v\n%s", label, err, importOut)
	}
	if importReport.ProfileID != "empty" || importReport.BundlePath != importDir || strings.Join(importReport.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("%s profile import report = %#v", label, importReport)
	}

	ctx := context.Background()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open %s profile Store: %v", label, err)
	}
	index, err := runtime.GetProfileIndex(ctx, "empty")
	if err != nil {
		t.Fatalf("get %s profile index: %v", label, err)
	}
	if index.BundlePath != importDir || !strings.HasPrefix(index.BundleDigest, "sha256:") {
		t.Fatalf("%s profile index = %#v", label, index)
	}
	catalogIndex, err := runtime.GetProfileCatalogIndex(ctx)
	if err != nil {
		t.Fatalf("get %s profile catalog index: %v", label, err)
	}
	if catalogIndex.ProfileID != "empty" {
		t.Fatalf("%s profile catalog index = %#v", label, catalogIndex)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("close imported %s profile Store: %v", label, err)
	}
	return importDir
}

func seedNamedProfileVerifyCaseRuns(t *testing.T, storeRef string, runLabel string, label string) string {
	t.Helper()

	verifyDir := writeInterfaceNodeCaseProfile(t)
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	ctx := context.Background()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open %s profile Store for case runs: %v", label, err)
	}
	base := mustParseTime(t, "2026-05-18T03:00:00Z")
	recordCaseRunForCoverage(t, ctx, runtime, "run."+runLabel+".alpha."+suffix, "case.alpha", store.StatusPassed, base)
	recordCaseRunForCoverage(t, ctx, runtime, "run."+runLabel+".beta."+suffix, "case.beta", store.StatusPassed, base.Add(time.Minute))
	if err := runtime.Close(); err != nil {
		t.Fatalf("close %s profile Store: %v", label, err)
	}
	return verifyDir
}

func runNamedProfileVerifyJSON(t *testing.T, verifyDir string, label string) namedProfileVerifyReport {
	t.Helper()

	verifyOut := runCLI(t, "profile", "verify", "--profile", verifyDir, "--require-case-runs", "--json")
	var verifyReport namedProfileVerifyReport
	if err := json.Unmarshal([]byte(verifyOut), &verifyReport); err != nil {
		t.Fatalf("decode %s profile verify json: %v\n%s", label, err, verifyOut)
	}
	return verifyReport
}

func requireNamedProfileVerifyReport(t *testing.T, label string, verifyReport namedProfileVerifyReport) {
	t.Helper()

	if !verifyReport.OK || verifyReport.Publish.ProfileID != "sample" || !hasReadModels(verifyReport.Publish.ReadModels, "interface-nodes", "catalog", "dashboard") {
		t.Fatalf("%s profile verify report = %#v", label, verifyReport)
	}
	if !verifyReport.Summary.RequiredCaseRuns || verifyReport.Summary.FailedChecks != 0 {
		t.Fatalf("%s profile verify summary = %#v", label, verifyReport.Summary)
	}
	if !hasProfileVerifyCheck(verifyReport.Checks, "api-case-run:case.alpha") || !hasProfileVerifyCheck(verifyReport.Checks, "api-case-run:case.beta") {
		t.Fatalf("%s profile verify checks = %#v", label, verifyReport.Checks)
	}
}

func requireNamedProfileVerifyStoreState(t *testing.T, storeRef string, label string, importDir string, verifyDir string) {
	t.Helper()

	ctx := context.Background()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("reopen %s profile Store: %v", label, err)
	}
	defer runtime.Close()
	verifiedIndex, err := runtime.GetProfileIndex(ctx, "sample")
	if err != nil {
		t.Fatalf("get verified %s profile index: %v", label, err)
	}
	if verifiedIndex.BundlePath != verifyDir || !strings.HasPrefix(verifiedIndex.BundleDigest, "sha256:") {
		t.Fatalf("verified %s profile index = %#v", label, verifiedIndex)
	}
	verifiedCatalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get verified %s profile catalog: %v", label, err)
	}
	if verifiedCatalog.ProfileID != "sample" || len(verifiedCatalog.APICases) != 2 {
		t.Fatalf("verified %s profile catalog = %#v", label, verifiedCatalog)
	}
}
