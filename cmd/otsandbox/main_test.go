package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"open-test-sandbox/internal/apicase"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/schema"
	"open-test-sandbox/internal/store/sqlite"
)

func TestStoreUpgradeAndStatusCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")

	initial := runCLI(t, "store", "status", "--store-url", dbPath)
	if !strings.Contains(initial, "Version: 0") || !strings.Contains(initial, fmt.Sprintf("Pending: %d", schema.CurrentVersion)) {
		t.Fatalf("initial status output = %q", initial)
	}

	upgraded := runCLI(t, "store", "upgrade", "--store-url", dbPath)
	if !strings.Contains(upgraded, fmt.Sprintf("Upgraded store schema to version %d", schema.CurrentVersion)) {
		t.Fatalf("upgrade output = %q", upgraded)
	}

	current := runCLI(t, "store", "status", "--store-url", dbPath)
	if !strings.Contains(current, fmt.Sprintf("Version: %d", schema.CurrentVersion)) || !strings.Contains(current, "Pending: 0") {
		t.Fatalf("current status output = %q", current)
	}
}

func TestStoreCommandsRejectUnsupportedBackendURLs(t *testing.T) {
	out := runCLIFails(t, "store", "status", "--store-url", "postgres://localhost/open_test_sandbox")
	if !strings.Contains(out, "unsupported store backend") || !strings.Contains(out, "sqlite://") {
		t.Fatalf("unsupported backend output = %q", out)
	}
}

func TestProfileInitCommandWritesExternalBundle(t *testing.T) {
	profileDir := filepath.Join(t.TempDir(), "external-profile")

	out := runCLI(t, "profile", "init", "--output", profileDir, "--id", "sample", "--display-name", "Sample Profile")
	if !strings.Contains(out, "Initialized external profile bundle: sample") || !strings.Contains(out, profileDir) {
		t.Fatalf("profile init output = %q", out)
	}
	for _, path := range []string{
		"profile.json",
		"README.md",
		".gitignore",
		"services",
		"workflows",
		"interface-nodes",
		"cases",
		"request-templates",
		"case-dependencies",
		"workflow-bindings",
		"fixtures",
	} {
		if _, err := os.Stat(filepath.Join(profileDir, path)); err != nil {
			t.Fatalf("expected generated path %s: %v", path, err)
		}
	}
	ignore, err := os.ReadFile(filepath.Join(profileDir, ".gitignore"))
	if err != nil {
		t.Fatalf("read generated ignore file: %v", err)
	}
	for _, want := range []string{".runtime/", "*.sqlite", "*.log"} {
		if !strings.Contains(string(ignore), want) {
			t.Fatalf("generated ignore file missing %q:\n%s", want, ignore)
		}
	}

	inspect := runCLI(t, "profile", "inspect", "--profile", profileDir)
	if !strings.Contains(inspect, "Profile: sample") || !strings.Contains(inspect, "Display Name: Sample Profile") {
		t.Fatalf("inspect generated profile = %q", inspect)
	}
}

func TestProfileInitCommandRejectsCoreProfilesPath(t *testing.T) {
	out := runCLIFails(t, "profile", "init", "--output", "profiles/sample")
	if !strings.Contains(out, "outside this core repository") {
		t.Fatalf("profile init rejection output = %q", out)
	}
}

func TestProfileInstallCommandCopiesBundleIntoProfileHome(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	for _, path := range []string{
		filepath.Join(".runtime", "store.sqlite"),
		filepath.Join(".runtime", "evidence", "run.json"),
		filepath.Join(".git", "config"),
		"debug.log",
		"local.sqlite",
	} {
		fullPath := filepath.Join(sourceDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create generated state parent %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte("generated"), 0o644); err != nil {
			t.Fatalf("write generated state %s: %v", path, err)
		}
	}
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "install", "--from", sourceDir, "--profile-home", profileHome)
	if !strings.Contains(out, "Installed profile: sample") || !strings.Contains(out, filepath.Join(profileHome, "sample")) {
		t.Fatalf("profile install output = %q", out)
	}
	for _, path := range []string{"profile.json", filepath.Join("workflows", "workflow.json"), filepath.Join("cases", "case.json")} {
		if _, err := os.Stat(filepath.Join(profileHome, "sample", path)); err != nil {
			t.Fatalf("expected installed path %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(".runtime", "store.sqlite"),
		filepath.Join(".runtime", "evidence", "run.json"),
		filepath.Join(".git", "config"),
		"debug.log",
		"local.sqlite",
	} {
		if _, err := os.Stat(filepath.Join(profileHome, "sample", path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("generated state should not be installed at %s: %v", path, err)
		}
	}

	inspect := runCLI(t, "profile", "inspect", "--profile", "sample", "--profile-home", profileHome)
	if !strings.Contains(inspect, "Profile: sample") || !strings.Contains(inspect, "Workflows: 1") {
		t.Fatalf("inspect installed profile = %q", inspect)
	}

	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	verify := runCLI(t, "profile", "verify", "--profile", "sample", "--profile-home", profileHome, "--store-url", dbPath)
	if !strings.Contains(verify, "Profile Verification: sample") || !strings.Contains(verify, "OK: true") {
		t.Fatalf("verify installed profile = %q", verify)
	}
}

func TestProfilePackCommandWritesCleanArchive(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	for _, path := range []string{
		filepath.Join(".runtime", "store.sqlite"),
		"debug.log",
		"local.sqlite",
	} {
		fullPath := filepath.Join(sourceDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create generated state parent %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte("generated"), 0o644); err != nil {
			t.Fatalf("write generated state %s: %v", path, err)
		}
	}
	outputPath := filepath.Join(t.TempDir(), "sample-profile.tar.gz")

	out := runCLI(t, "profile", "pack", "--profile", sourceDir, "--output", outputPath, "--json")

	var report struct {
		ID           string `json:"id"`
		SourcePath   string `json:"sourcePath"`
		OutputPath   string `json:"outputPath"`
		BundleDigest string `json:"bundleDigest"`
		FileCount    int    `json:"fileCount"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile pack report: %v\n%s", err, out)
	}
	if report.ID != "sample" || report.SourcePath != sourceDir || report.OutputPath != outputPath || report.FileCount == 0 || !strings.HasPrefix(report.BundleDigest, "sha256:") {
		t.Fatalf("profile pack report = %#v", report)
	}
	entries := readTarGZEntries(t, outputPath)
	for _, want := range []string{"sample/profile.json", "sample/workflows/workflow.json", "sample/cases/case.json"} {
		if !containsString(entries, want) {
			t.Fatalf("profile archive missing %s: %#v", want, entries)
		}
	}
	for _, unwanted := range []string{"sample/.runtime/store.sqlite", "sample/debug.log", "sample/local.sqlite"} {
		if containsString(entries, unwanted) {
			t.Fatalf("profile archive included generated state %s: %#v", unwanted, entries)
		}
	}
}

func TestProfilePackCommandResolvesInstalledProfileID(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	profileHome := filepath.Join(t.TempDir(), "profile-home")
	runCLI(t, "profile", "install", "--from", sourceDir, "--profile-home", profileHome)
	outputPath := filepath.Join(t.TempDir(), "installed-profile.tar.gz")

	out := runCLI(t, "profile", "pack", "--profile", "sample", "--profile-home", profileHome, "--output", outputPath)

	if !strings.Contains(out, "Packed profile: sample") || !strings.Contains(out, outputPath) {
		t.Fatalf("profile pack installed output = %q", out)
	}
	if !containsString(readTarGZEntries(t, outputPath), "sample/profile.json") {
		t.Fatalf("installed profile archive missing manifest")
	}
}

func TestProfileInstallCommandAcceptsPackedArchive(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	archivePath := filepath.Join(t.TempDir(), "sample-profile.tar.gz")
	runCLI(t, "profile", "pack", "--profile", sourceDir, "--output", archivePath)
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "install", "--from", archivePath, "--profile-home", profileHome, "--json")

	var report struct {
		ID         string `json:"id"`
		SourcePath string `json:"sourcePath"`
		TargetPath string `json:"targetPath"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode archive install report: %v\n%s", err, out)
	}

	if report.ID != "sample" || report.SourcePath != archivePath || report.TargetPath != filepath.Join(profileHome, "sample") {
		t.Fatalf("profile install archive report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(profileHome, "sample", "profile.json")); err != nil {
		t.Fatalf("installed archive manifest missing: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	verify := runCLI(t, "profile", "verify", "--profile", "sample", "--profile-home", profileHome, "--store-url", dbPath)
	if !strings.Contains(verify, "Profile Verification: sample") || !strings.Contains(verify, "OK: true") {
		t.Fatalf("verify installed archive profile = %q", verify)
	}
}

func TestProfileInstallCommandRejectsUnsafeArchivePath(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "unsafe.tar.gz")
	writeTarGZEntries(t, archivePath, map[string]string{
		"sample/profile.json": `{"id":"sample","displayName":"Sample Profile"}`,
		"../escaped.txt":      "nope",
	})
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLIFails(t, "profile", "install", "--from", archivePath, "--profile-home", profileHome)

	if !strings.Contains(out, "escapes profile root") {
		t.Fatalf("unsafe archive output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(profileHome), "escaped.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe archive wrote escaped path: %v", err)
	}
}

func TestProfileListCommandReportsInstalledBundles(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-profile")
	writeWorkflowProfile(t, sourceDir)
	profileHome := filepath.Join(t.TempDir(), "profile-home")
	runCLI(t, "profile", "install", "--from", sourceDir, "--profile-home", profileHome)

	out := runCLI(t, "profile", "list", "--profile-home", profileHome, "--json")
	var report struct {
		ProfileHome string `json:"profileHome"`
		Profiles    []struct {
			ID           string `json:"id"`
			DisplayName  string `json:"displayName"`
			Path         string `json:"path"`
			BundleDigest string `json:"bundleDigest"`
			Counts       struct {
				Workflows int `json:"workflows"`
				APICases  int `json:"apiCases"`
			} `json:"counts"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile list: %v\n%s", err, out)
	}
	if report.ProfileHome != profileHome || len(report.Profiles) != 1 {
		t.Fatalf("profile list identity = %#v", report)
	}
	item := report.Profiles[0]
	if item.ID != "sample" || item.DisplayName != "Sample Profile" || item.Path != filepath.Join(profileHome, "sample") || !strings.HasPrefix(item.BundleDigest, "sha256:") {
		t.Fatalf("profile list item = %#v", item)
	}
	if item.Counts.Workflows != 1 || item.Counts.APICases != 1 {
		t.Fatalf("profile list counts = %#v", item.Counts)
	}
}

func TestProfileListCommandReportsInvalidInstalledBundle(t *testing.T) {
	profileHome := filepath.Join(t.TempDir(), "profile-home")
	brokenDir := filepath.Join(profileHome, "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("create broken profile dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "profile.json"), []byte(`{"id":`), 0o644); err != nil {
		t.Fatalf("write broken profile: %v", err)
	}

	out := runCLI(t, "profile", "list", "--profile-home", profileHome, "--json")
	var report struct {
		Profiles []struct {
			ID    string `json:"id"`
			Path  string `json:"path"`
			Valid bool   `json:"valid"`
			Error string `json:"error"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode invalid profile list report: %v\n%s", err, out)
	}
	if len(report.Profiles) != 1 || report.Profiles[0].ID != "broken" || report.Profiles[0].Path != brokenDir || report.Profiles[0].Valid || report.Profiles[0].Error == "" {
		t.Fatalf("invalid profile list report = %#v", report)
	}
}

func TestProfileInspectCommand(t *testing.T) {
	profileDir := writeEmptyProfileBundle(t)
	out := runCLI(t, "profile", "inspect", "--profile", profileDir)
	for _, want := range []string{"Profile: empty", "Display Name: Empty Profile", "Workflows: 0", "API Cases: 0", "Request Templates: 0", "Case Dependencies: 0", "Workflow Bindings: 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile inspect output missing %q: %q", want, out)
		}
	}
}

func TestProfileAuditCommandAcceptsPackedArchive(t *testing.T) {
	profileDir := writeEmptyProfileBundle(t)
	archivePath := filepath.Join(t.TempDir(), "empty-profile.tgz")
	runCLI(t, "profile", "pack", "--profile", profileDir, "--output", archivePath)
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "audit", "--profile", archivePath, "--profile-home", profileHome, "--json")

	var report struct {
		ProfileID string `json:"profileId"`
		OK        bool   `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit archive report: %v\n%s", err, out)
	}
	targetPath := filepath.Join(profileHome, "empty")
	if report.ProfileID != "empty" || !report.OK {
		t.Fatalf("profile audit archive report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "profile.json")); err != nil {
		t.Fatalf("installed audit archive manifest missing: %v", err)
	}
}

func TestProfileVerifyCommandAuditsPublishesAndChecksReadModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)

	out := runCLI(t, "profile", "verify", "--profile", profileDir, "--store-url", dbPath, "--json")

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

	out := runCLI(t, "profile", "verify", "--profile", archivePath, "--profile-home", profileHome, "--store-url", dbPath, "--json")

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

	out := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store-url", storePath)
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
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run-alpha",
		ProfileID:  "sample",
		WorkflowID: "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
		UpdatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
	}); err != nil {
		t.Fatalf("create alpha run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "case-run-alpha",
		RunID:      "run-alpha",
		CaseID:     "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
	}); err != nil {
		t.Fatalf("record alpha case run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	missing := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store-url", dbPath, "--require-case-runs")
	if !strings.Contains(missing, "api-case-run:case.beta") || !strings.Contains(missing, "no passed run") {
		t.Fatalf("missing case run verify output = %q", missing)
	}
	missingJSON := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store-url", dbPath, "--require-case-runs", "--json")
	for _, want := range []string{`"ok": false`, `"firstFailed": "api-case-run:case.beta"`, `"name": "api-case-run:case.beta"`} {
		if !strings.Contains(missingJSON, want) {
			t.Fatalf("missing case run json output does not contain %q:\n%s", want, missingJSON)
		}
	}

	s, err = sqlite.Open(ctx, sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run-beta",
		ProfileID:  "sample",
		WorkflowID: "case.beta",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:01:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:01:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:01:01Z"),
		UpdatedAt:  mustParseTime(t, "2026-05-14T01:01:01Z"),
	}); err != nil {
		t.Fatalf("create beta run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "case-run-beta",
		RunID:      "run-beta",
		CaseID:     "case.beta",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:01:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:01:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:01:01Z"),
	}); err != nil {
		t.Fatalf("record beta case run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}

	out := runCLI(t, "profile", "verify", "--profile", profileDir, "--store-url", dbPath, "--require-case-runs", "--json")
	var report struct {
		OK      bool `json:"ok"`
		Summary struct {
			TotalChecks          int  `json:"totalChecks"`
			PassedChecks         int  `json:"passedChecks"`
			FailedChecks         int  `json:"failedChecks"`
			RequiredCaseRuns     bool `json:"requiredCaseRuns"`
			RequiredWorkflowRuns bool `json:"requiredWorkflowRuns"`
		} `json:"summary"`
		Checks []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile verify runtime report: %v\n%s", err, out)
	}
	if !report.OK || !hasProfileVerifyCheck(report.Checks, "api-case-run:case.alpha") || !hasProfileVerifyCheck(report.Checks, "api-case-run:case.beta") {
		t.Fatalf("profile verify runtime report = %#v", report)
	}
}

func TestProfileVerifyCommandCanRequirePassedWorkflowRuns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := filepath.Join(t.TempDir(), "profile")
	writeWorkflowProfile(t, profileDir)

	missing := runCLIFails(t, "profile", "verify", "--profile", profileDir, "--store-url", dbPath, "--require-workflow-runs")
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

	out := runCLI(t, "profile", "verify", "--profile", profileDir, "--store-url", dbPath, "--require-workflow-runs", "--json")
	var report struct {
		OK      bool `json:"ok"`
		Summary struct {
			TotalChecks          int  `json:"totalChecks"`
			PassedChecks         int  `json:"passedChecks"`
			FailedChecks         int  `json:"failedChecks"`
			RequiredCaseRuns     bool `json:"requiredCaseRuns"`
			RequiredWorkflowRuns bool `json:"requiredWorkflowRuns"`
		} `json:"summary"`
		Checks []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"checks"`
	}
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

func TestProfileImportCommandIndexesBundleInStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)

	out := runCLI(t, "profile", "import", "--from", profileDir, "--store-url", dbPath)
	if !strings.Contains(out, "Imported profile: empty") {
		t.Fatalf("profile import output = %q", out)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	index, err := s.GetProfileIndex(context.Background(), "empty")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if index.BundlePath == "" || !strings.HasPrefix(index.BundleDigest, "sha256:") {
		t.Fatalf("profile index = %#v", index)
	}
	if got := sqliteScalar(t, dbPath, "select value from kv where key = 'active_profile_id';"); got != "empty" {
		t.Fatalf("active profile catalog index = %q", got)
	}
}

func TestProfileImportCommandAcceptsPackedArchive(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)
	archivePath := filepath.Join(t.TempDir(), "empty-profile.tar.gz")
	runCLI(t, "profile", "pack", "--profile", profileDir, "--output", archivePath)
	profileHome := filepath.Join(t.TempDir(), "profile-home")

	out := runCLI(t, "profile", "import", "--from", archivePath, "--profile-home", profileHome, "--store-url", dbPath, "--json")

	var report struct {
		ProfileID  string `json:"profileId"`
		BundlePath string `json:"bundlePath"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile import archive report: %v\n%s", err, out)
	}
	targetPath := filepath.Join(profileHome, "empty")
	if report.ProfileID != "empty" || report.BundlePath != targetPath {
		t.Fatalf("profile import archive report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "profile.json")); err != nil {
		t.Fatalf("installed archive manifest missing: %v", err)
	}
	if got := sqliteScalar(t, dbPath, "select source_path from config_versions where active = 1;"); got != targetPath {
		t.Fatalf("archive import config source path = %q, want %q", got, targetPath)
	}
}

func TestConfigPublishCommandIndexesBundleInStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)

	out := runCLI(t, "config", "publish", "--from", profileDir, "--store-url", dbPath, "--json")

	var report struct {
		ProfileID     string   `json:"profileId"`
		BundleDigest  string   `json:"bundleDigest"`
		ReadModels    []string `json:"readModels"`
		ConfigVersion struct {
			ID           string `json:"id"`
			ProfileID    string `json:"profileId"`
			BundleDigest string `json:"bundleDigest"`
			Active       bool   `json:"active"`
		} `json:"configVersion"`
		CatalogIndex struct {
			ProfileID string `json:"profileId"`
		} `json:"catalogIndex"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode config publish report: %v\n%s", err, out)
	}
	if report.ProfileID != "empty" || report.CatalogIndex.ProfileID != "empty" || !strings.HasPrefix(report.BundleDigest, "sha256:") {
		t.Fatalf("config publish report = %#v", report)
	}
	if report.ConfigVersion.ID == "" || report.ConfigVersion.ProfileID != "empty" || report.ConfigVersion.BundleDigest != report.BundleDigest || !report.ConfigVersion.Active {
		t.Fatalf("config version = %#v", report.ConfigVersion)
	}
	if strings.Join(report.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("config publish read models = %#v", report.ReadModels)
	}
	if got := sqliteScalar(t, dbPath, "select value from kv where key = 'active_profile_id';"); got != "empty" {
		t.Fatalf("active config profile = %q", got)
	}
	if got := sqliteScalar(t, dbPath, "select bundle_digest from config_versions where active = 1;"); got != report.BundleDigest {
		t.Fatalf("active config digest = %q, want %q", got, report.BundleDigest)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'empty' and model_key = 'interface-nodes';"); got != report.ConfigVersion.ID {
		t.Fatalf("interface nodes read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'empty' and model_key = 'catalog';"); got != report.ConfigVersion.ID {
		t.Fatalf("catalog read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
}

func TestConfigPublishCommandMaterializesInterfaceNodeDetailReadModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeInterfaceNodeCaseProfile(t)

	out := runCLI(t, "config", "publish", "--from", profileDir, "--store-url", dbPath, "--json")

	var report struct {
		ConfigVersion struct {
			ID string `json:"id"`
		} `json:"configVersion"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode config publish report: %v\n%s", err, out)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'sample' and model_key = 'interface-node:node.alpha';"); got != report.ConfigVersion.ID {
		t.Fatalf("interface node detail read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
	if got := sqliteScalar(t, dbPath, "select json_extract(payload_json, '$.source.kind') from config_read_model where profile_id = 'sample' and model_key = 'interface-node:node.alpha';"); got != "read-model" {
		t.Fatalf("interface node detail source kind = %q", got)
	}
}

func TestConfigPublishCommandMaterializesInterfaceNodeCoverageReadModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeInterfaceNodeCoverageProfile(t)

	out := runCLI(t, "config", "publish", "--from", profileDir, "--store-url", dbPath, "--json")

	var report struct {
		ConfigVersion struct {
			ID string `json:"id"`
		} `json:"configVersion"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode config publish report: %v\n%s", err, out)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'sample' and model_key = 'interface-node-coverage:workflow.alpha';"); got != report.ConfigVersion.ID {
		t.Fatalf("interface node coverage read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
	if got := sqliteScalar(t, dbPath, "select json_extract(payload_json, '$.source.kind') from config_read_model where profile_id = 'sample' and model_key = 'interface-node-coverage-gaps:workflow.alpha';"); got != "read-model" {
		t.Fatalf("interface node coverage gaps source kind = %q", got)
	}
}

func TestProfileImportCommandCanEmitJSONReport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeEmptyProfileBundle(t)

	out := runCLI(t, "profile", "import", "--from", profileDir, "--store-url", dbPath, "--json")

	var report struct {
		ProfileID    string `json:"profileId"`
		BundlePath   string `json:"bundlePath"`
		BundleDigest string `json:"bundleDigest"`
		Counts       struct {
			Services         int `json:"services"`
			Workflows        int `json:"workflows"`
			InterfaceNodes   int `json:"interfaceNodes"`
			APICases         int `json:"apiCases"`
			RequestTemplates int `json:"requestTemplates"`
			CaseDependencies int `json:"caseDependencies"`
			WorkflowBindings int `json:"workflowBindings"`
			Fixtures         int `json:"fixtures"`
		} `json:"counts"`
		CatalogIndex struct {
			ProfileID   string `json:"profileId"`
			IndexedAt   string `json:"indexedAt"`
			StoreCounts struct {
				Services        int `json:"services"`
				Workflows       int `json:"workflows"`
				Templates       int `json:"templates"`
				TemplateConfigs int `json:"templateConfigs"`
			} `json:"counts"`
		} `json:"catalogIndex"`
		StorePath  string   `json:"storePath"`
		ImportedAt string   `json:"importedAt"`
		ReadModels []string `json:"readModels"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile import json: %v\n%s", err, out)
	}
	if report.ProfileID != "empty" || report.BundlePath != profileDir {
		t.Fatalf("report profile/path = %#v", report)
	}
	if !strings.HasPrefix(report.BundleDigest, "sha256:") || report.StorePath != dbPath || report.ImportedAt == "" {
		t.Fatalf("report digest/store/import time = %#v", report)
	}
	if report.Counts.Services != 0 || report.Counts.APICases != 0 || report.Counts.WorkflowBindings != 0 {
		t.Fatalf("report counts = %#v", report.Counts)
	}
	if report.CatalogIndex.ProfileID != "empty" || report.CatalogIndex.IndexedAt == "" {
		t.Fatalf("report catalog index identity = %#v", report.CatalogIndex)
	}
	if report.CatalogIndex.StoreCounts.Services != 0 || report.CatalogIndex.StoreCounts.Templates != 0 || report.CatalogIndex.StoreCounts.TemplateConfigs != 0 {
		t.Fatalf("report catalog index counts = %#v", report.CatalogIndex.StoreCounts)
	}
	if strings.Join(report.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("profile import read models = %#v", report.ReadModels)
	}
}

func TestInterfaceNodeCaseAuditReportsMissingExecutionConfigs(t *testing.T) {
	dir := writeInterfaceNodeCaseProfile(t)

	out := runCLI(t, "interface-node", "case", "audit", "--profile", dir, "--node", "node.alpha", "--json")

	var report struct {
		OK     bool   `json:"ok"`
		NodeID string `json:"nodeId"`
		Counts struct {
			Cases      int `json:"cases"`
			Configured int `json:"configured"`
			Missing    int `json:"missing"`
		} `json:"counts"`
		Missing []struct {
			CaseID string `json:"caseId"`
		} `json:"missing"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode interface node case audit json: %v\n%s", err, out)
	}
	if report.OK || report.NodeID != "node.alpha" || report.Counts.Cases != 2 || report.Counts.Configured != 1 || report.Counts.Missing != 1 {
		t.Fatalf("audit report = %#v", report)
	}
	if len(report.Missing) != 1 || report.Missing[0].CaseID != "case.beta" {
		t.Fatalf("missing cases = %#v", report.Missing)
	}
}

func TestInterfaceNodeCaseApplyMergesExecutionConfigsIntoProfileCatalog(t *testing.T) {
	dir := writeInterfaceNodeCaseProfile(t)
	requestPath := filepath.Join(t.TempDir(), "case-config.json")
	writeFile(t, requestPath, `{
  "templateConfigs": [
    {
      "id": "cfg.case.beta",
      "templateId": "case-execution",
      "nodeId": "node.alpha",
      "scopeType": "case",
      "scopeId": "case.beta",
      "title": "Case Beta execution",
      "status": "active",
      "sortOrder": 2,
      "config": {
        "caseId": "case.beta",
        "caseExecution": {
          "method": "GET",
          "nodeId": "service.alpha",
          "path": "/beta",
          "expectedHttpCodes": [200]
        }
      }
    }
  ]
}`)

	out := runCLI(t, "interface-node", "case", "apply", "--profile", dir, "--file", requestPath, "--json")

	var result struct {
		Applied int    `json:"applied"`
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode interface node case apply json: %v\n%s", err, out)
	}
	if result.Applied != 1 || result.Profile != dir {
		t.Fatalf("apply result = %#v", result)
	}
	audit := runCLI(t, "interface-node", "case", "audit", "--profile", dir, "--node", "node.alpha", "--json")
	var auditReport struct {
		OK     bool `json:"ok"`
		Counts struct {
			Missing int `json:"missing"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(audit), &auditReport); err != nil {
		t.Fatalf("decode audit after apply: %v\n%s", err, audit)
	}
	if !auditReport.OK || auditReport.Counts.Missing != 0 {
		t.Fatalf("audit after apply = %s", audit)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var catalog struct {
		TemplateConfigs []struct {
			ConfigJSON string `json:"configJson"`
		} `json:"templateConfigs"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("decode catalog after apply: %v\n%s", err, raw)
	}
	hasBeta := false
	for _, item := range catalog.TemplateConfigs {
		var config struct {
			CaseID string `json:"caseId"`
		}
		if err := json.Unmarshal([]byte(item.ConfigJSON), &config); err != nil {
			t.Fatalf("decode template config after apply: %v\n%s", err, item.ConfigJSON)
		}
		hasBeta = hasBeta || config.CaseID == "case.beta"
	}
	if !hasBeta || strings.Contains(string(raw), "store.sqlite") {
		t.Fatalf("catalog after apply = %s", raw)
	}
}

func TestInterfaceNodeCaseDraftAndApplyCreatesRunnableMaintainedCase(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha","method":"POST","path":"/v1/items","sortOrder":7}],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	bundlePath := filepath.Join(t.TempDir(), "case-draft.json")

	out := runCLI(t,
		"interface-node", "case", "draft",
		"--profile", dir,
		"--node", "node.alpha",
		"--case-id", "case.generated",
		"--title", "Generated Case",
		"--tag", "regression",
		"--tag", "smoke",
		"--priority", "p1",
		"--owner", "team-a",
		"--output", bundlePath,
		"--json",
	)
	var draft struct {
		OK             bool   `json:"ok"`
		CaseID         string `json:"caseId"`
		NodeID         string `json:"nodeId"`
		BundlePath     string `json:"bundlePath"`
		CasePath       string `json:"casePath"`
		TemplateConfig struct {
			ConfigJSON string `json:"configJson"`
		} `json:"templateConfig"`
		CaseFile struct {
			Path string       `json:"path"`
			Case apicase.Case `json:"case"`
		} `json:"caseFile"`
	}
	if err := json.Unmarshal([]byte(out), &draft); err != nil {
		t.Fatalf("decode case draft json: %v\n%s", err, out)
	}
	if !draft.OK || draft.CaseID != "case.generated" || draft.NodeID != "node.alpha" || draft.BundlePath != bundlePath || draft.CasePath != "api-cases/case.generated.json" {
		t.Fatalf("case draft = %#v", draft)
	}
	if draft.CaseFile.Path != draft.CasePath || draft.CaseFile.Case.Request.Method != "POST" || draft.CaseFile.Case.Request.Path != "/v1/items" {
		t.Fatalf("case draft file = %#v", draft.CaseFile)
	}
	if !strings.Contains(draft.TemplateConfig.ConfigJSON, `"caseId":"case.generated"`) || !strings.Contains(draft.TemplateConfig.ConfigJSON, `"expectedHttpCodes":[200]`) {
		t.Fatalf("case draft config json = %s", draft.TemplateConfig.ConfigJSON)
	}
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("draft bundle missing: %v", err)
	}

	applyOut := runCLI(t, "interface-node", "case", "apply", "--profile", dir, "--file", bundlePath, "--json")
	var applied struct {
		Applied int `json:"applied"`
		Cases   int `json:"cases"`
		Files   int `json:"files"`
	}
	if err := json.Unmarshal([]byte(applyOut), &applied); err != nil {
		t.Fatalf("decode apply draft json: %v\n%s", err, applyOut)
	}
	if applied.Applied != 1 || applied.Cases != 1 || applied.Files != 1 {
		t.Fatalf("apply draft result = %#v", applied)
	}
	if _, err := os.Stat(filepath.Join(dir, "api-cases", "case.generated.json")); err != nil {
		t.Fatalf("applied runnable case file missing: %v", err)
	}
	loaded, err := profile.Load(dir)
	if err != nil {
		t.Fatalf("load applied profile: %v", err)
	}
	if len(loaded.APICases) != 1 || loaded.APICases[0].ID != "case.generated" || loaded.APICases[0].CasePath != "api-cases/case.generated.json" || loaded.APICases[0].Owner != "team-a" {
		t.Fatalf("loaded applied cases = %#v", loaded.APICases)
	}
	audit := runCLI(t, "interface-node", "case", "audit", "--profile", dir, "--node", "node.alpha", "--json")
	var auditReport struct {
		OK     bool `json:"ok"`
		Counts struct {
			Cases      int `json:"cases"`
			Configured int `json:"configured"`
			Missing    int `json:"missing"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(audit), &auditReport); err != nil {
		t.Fatalf("decode audit after draft apply: %v\n%s", err, audit)
	}
	if !auditReport.OK || auditReport.Counts.Cases != 1 || auditReport.Counts.Configured != 1 || auditReport.Counts.Missing != 0 {
		t.Fatalf("audit after draft apply = %#v", auditReport)
	}
}

func TestProfileImportCommandCanAuditImportedProfile(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}],
  "requestTemplates": [],
  "caseDependencies": [{"id":"dependency.alpha","caseId":"case.alpha","fixtureId":"fixture.missing"}],
  "workflowBindings": [],
  "fixtures": []
}`)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "profile", "import", "--from", profileDir, "--store-url", storePath, "--json", "--audit")

	var report struct {
		ProfileID string `json:"profileId"`
		Audit     *struct {
			OK         bool `json:"ok"`
			IssueCount int  `json:"issueCount"`
			Issues     []struct {
				Code      string `json:"code"`
				SubjectID string `json:"subjectId"`
			} `json:"issues"`
			Store *struct {
				ProfileIndexed bool `json:"profileIndexed"`
				DigestMatches  bool `json:"digestMatches"`
			} `json:"store,omitempty"`
		} `json:"audit,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode audited profile import json: %v\n%s", err, out)
	}
	if report.ProfileID != "sample" || report.Audit == nil {
		t.Fatalf("report missing audit = %#v", report)
	}
	if report.Audit.OK || report.Audit.IssueCount != 2 || len(report.Audit.Issues) != 2 {
		t.Fatalf("audit summary = %#v", report.Audit)
	}
	if report.Audit.Issues[0].Code != "api-case-node-missing" || report.Audit.Issues[1].Code != "case-dependency-fixture-missing" {
		t.Fatalf("audit issues = %#v", report.Audit.Issues)
	}
	if report.Audit.Store == nil || !report.Audit.Store.ProfileIndexed || !report.Audit.Store.DigestMatches {
		t.Fatalf("audit store = %#v", report.Audit.Store)
	}

	text := runCLI(t, "profile", "import", "--from", profileDir, "--store-url", storePath, "--audit")
	for _, want := range []string{"Imported profile: sample", "Audit OK: false", "Audit Issues: 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("audited text import output missing %q: %q", want, text)
		}
	}
}

func TestProfileImportCommandCanRequireCleanAuditBeforeWritingStore(t *testing.T) {
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

	out := runCLIFails(t, "profile", "import", "--from", profileDir, "--store-url", storePath, "--require-audit-ok")
	if !strings.Contains(out, "profile audit failed") || !strings.Contains(out, "api-case-node-missing") {
		t.Fatalf("strict profile import output = %q", out)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if _, err := s.GetProfileIndex(context.Background(), "sample"); err == nil {
		t.Fatalf("strict profile import wrote profile index")
	} else if err != store.ErrNotFound {
		t.Fatalf("get profile index after strict failure: %v", err)
	}
}

func TestProfileAuditCommandEmitsJSONWithStoreState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	alphaPath := filepath.Join(dir, "case.alpha.json")
	betaPath := filepath.Join(dir, "case.beta.json")
	writeAPICaseFile(t, alphaPath)
	writeFile(t, betaPath, `{
  "id": "case.beta",
  "title": "Read Item",
  "request": {"method": "GET", "path": "/v1/items/item-001"},
  "assertions": {"expectedStatusCodes": [200]}
}`)
	writeFile(t, filepath.Join(profileDir, "profile.json"), fmt.Sprintf(`{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha","casePath":%q},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.alpha","casePath":%q}
  ],
  "requestTemplates": [{"id":"template.alpha","nodeId":"node.alpha","method":"POST","path":"/v1/items"}],
  "caseDependencies": [{"id":"dependency.beta","caseId":"case.beta","fixtureId":"fixture.missing"}],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.beta","required":true}],
  "fixtures": []
}`, alphaPath, betaPath))

	storePath := filepath.Join(dir, "store.sqlite")
	runCLI(t, "profile", "import", "--from", profileDir, "--store-url", storePath)
	runCLI(t, "case", "run", "--case", alphaPath, "--base-url", server.URL, "--run-id", "run-alpha", "--store-url", storePath, "--profile", "sample")

	out := runCLI(t, "profile", "audit", "--profile", profileDir, "--store-url", storePath, "--json")

	var report struct {
		OK         bool `json:"ok"`
		IssueCount int  `json:"issueCount"`
		Issues     []struct {
			Code      string `json:"code"`
			SubjectID string `json:"subjectId"`
		} `json:"issues"`
		Store *struct {
			ProfileIndexed bool `json:"profileIndexed"`
			DigestMatches  bool `json:"digestMatches"`
			APICases       []struct {
				CaseID       string `json:"caseId"`
				HasPassed    bool   `json:"hasPassed"`
				LatestStatus string `json:"latestStatus"`
			} `json:"apiCases"`
		} `json:"store,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit json: %v\n%s", err, out)
	}
	if report.OK || report.IssueCount != 1 || len(report.Issues) != 1 {
		t.Fatalf("audit report issues = %#v", report)
	}
	if report.Issues[0].Code != "case-dependency-fixture-missing" || report.Issues[0].SubjectID != "dependency.beta" {
		t.Fatalf("audit issue = %#v", report.Issues[0])
	}
	if report.Store == nil || !report.Store.ProfileIndexed || !report.Store.DigestMatches {
		t.Fatalf("audit store state = %#v", report.Store)
	}
	caseState := map[string]struct {
		HasPassed    bool
		LatestStatus string
	}{}
	for _, item := range report.Store.APICases {
		caseState[item.CaseID] = struct {
			HasPassed    bool
			LatestStatus string
		}{HasPassed: item.HasPassed, LatestStatus: item.LatestStatus}
	}
	if !caseState["case.alpha"].HasPassed || caseState["case.alpha"].LatestStatus != "passed" {
		t.Fatalf("case.alpha state = %#v", caseState["case.alpha"])
	}
	if caseState["case.beta"].HasPassed || caseState["case.beta"].LatestStatus != "" {
		t.Fatalf("case.beta state = %#v", caseState["case.beta"])
	}
}

func TestProfileAuditPlanCommandSuggestsRepairActions(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.missing"}],
  "requestTemplates": [],
  "caseDependencies": [{"id":"dependency.alpha","caseId":"case.alpha","fixtureId":""}],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"","nodeId":"node.alpha","caseId":"case.alpha","required":true}],
  "fixtures": [{"id":"fixture.bad","kind":"json","dataJson":"{\"broken\":"}]
}`)

	out := runCLI(t, "profile", "audit-plan", "--profile", profileDir, "--json")
	var report struct {
		OK          bool   `json:"ok"`
		ProfileID   string `json:"profileId"`
		IssueCount  int    `json:"issueCount"`
		ActionCount int    `json:"actionCount"`
		Counts      struct {
			UpdateReferenceOrAddAsset int `json:"updateReferenceOrAddAsset"`
			FillRequiredField         int `json:"fillRequiredField"`
			FixInvalidJSON            int `json:"fixInvalidJson"`
		} `json:"counts"`
		Actions []struct {
			Type            string   `json:"type"`
			IssueCode       string   `json:"issueCode"`
			SubjectID       string   `json:"subjectId"`
			Field           string   `json:"field"`
			SuggestedChange string   `json:"suggestedChange"`
			Command         []string `json:"command"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit plan json: %v\n%s", err, out)
	}
	if !report.OK || report.ProfileID != "sample" || report.IssueCount != 4 || report.ActionCount != 4 {
		t.Fatalf("audit plan summary = %#v", report)
	}
	if report.Counts.UpdateReferenceOrAddAsset != 1 || report.Counts.FillRequiredField != 2 || report.Counts.FixInvalidJSON != 1 {
		t.Fatalf("audit plan counts = %#v", report.Counts)
	}
	if len(report.Actions) != 4 || report.Actions[0].Type != "update-reference-or-add-asset" || report.Actions[0].IssueCode != "api-case-node-missing" || report.Actions[0].SubjectID != "case.alpha" || report.Actions[0].Field != "nodeId" {
		t.Fatalf("audit plan actions = %#v", report.Actions)
	}
	if !strings.Contains(report.Actions[0].SuggestedChange, "Create the missing interface node") || strings.Join(report.Actions[0].Command, " ") != "profile audit --json" {
		t.Fatalf("audit plan first action = %#v", report.Actions[0])
	}

	textOut := runCLI(t, "profile", "audit-plan", "--profile", profileDir)
	for _, want := range []string{"Profile Audit Repair Plan: sample", "Actions: 4", "update-reference-or-add-asset", "api-case-node-missing", "fix-invalid-json"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("audit plan text missing %q:\n%s", want, textOut)
		}
	}
}

func TestBaselineGateCommandsSetAndGetState(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")

	out := runCLI(t, "baseline", "set", "--store-url", storePath, "--profile", "sample", "--subject", "workflow.alpha", "--status", "passed", "--required")
	if !strings.Contains(out, "Baseline Gate: sample workflow.alpha") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("baseline set output = %q", out)
	}

	out = runCLI(t, "baseline", "get", "--store-url", storePath, "--profile", "sample", "--subject", "workflow.alpha")
	for _, want := range []string{"Baseline Gate: sample workflow.alpha", "Status: passed", "Required: true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("baseline get output missing %q: %q", want, out)
		}
	}
}

func TestBaselineGetCommandRejectsMissingGate(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")

	out := runCLIFails(t, "baseline", "get", "--store-url", storePath, "--profile", "sample", "--subject", "workflow.missing")
	if !strings.Contains(out, "baseline gate not found") || !strings.Contains(out, "sample workflow.missing") {
		t.Fatalf("missing baseline gate output = %q", out)
	}
}

func TestWorkflowPlanCommandPrintsBoundSteps(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)

	out := runCLI(t, "workflow", "plan", "--profile", dir, "--workflow", "workflow.alpha")

	for _, want := range []string{
		"Workflow: workflow.alpha",
		"Step: step.one",
		"Node: node.alpha",
		"Case: case.alpha",
		"Required: true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workflow plan output missing %q: %q", want, out)
		}
	}
}

func TestWorkflowPlanCommandRejectsMissingWorkflow(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)

	out := runCLIFails(t, "workflow", "plan", "--profile", dir, "--workflow", "workflow.missing")
	if !strings.Contains(out, "workflow not found") || !strings.Contains(out, "workflow.missing") {
		t.Fatalf("missing workflow output = %q", out)
	}
}

func TestWorkflowAuditCommandEmitsJSONWithScopedStoreState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.missing"}
  ],
  "requestTemplates": [{"id":"template.alpha","nodeId":"node.alpha","method":"POST","path":"/v1/items"}],
  "caseDependencies": [{"id":"dependency.beta","caseId":"case.beta","fixtureId":"fixture.missing"}],
  "workflowBindings": [
    {"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.alpha","required":true},
    {"workflowId":"workflow.alpha","stepId":"step.two","nodeId":"node.alpha","caseId":"case.beta","required":true}
  ],
  "fixtures": []
}`)
	storePath := filepath.Join(dir, "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	started := mustParseTime(t, "2026-01-02T03:04:05Z")
	finished := started.Add(2 * time.Second)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.workflow.001",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusFailed,
		StartedAt:  started,
		FinishedAt: finished,
		CreatedAt:  started,
		UpdatedAt:  finished,
	}); err != nil {
		t.Fatalf("create first workflow run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "run.workflow.001.case.alpha",
		RunID:      "run.workflow.001",
		CaseID:     "case.alpha",
		Status:     store.StatusFailed,
		StartedAt:  started,
		FinishedAt: finished,
		CreatedAt:  started,
	}); err != nil {
		t.Fatalf("record first case run: %v", err)
	}
	laterStarted := started.Add(10 * time.Second)
	laterFinished := laterStarted.Add(3 * time.Second)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.workflow.002",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  laterStarted,
		FinishedAt: laterFinished,
		CreatedAt:  laterStarted,
		UpdatedAt:  laterFinished,
	}); err != nil {
		t.Fatalf("create second workflow run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "run.workflow.002.case.alpha",
		RunID:      "run.workflow.002",
		CaseID:     "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  laterStarted,
		FinishedAt: laterFinished,
		CreatedAt:  laterStarted,
	}); err != nil {
		t.Fatalf("record second case run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t, "workflow", "audit", "--profile", profileDir, "--workflow", "workflow.alpha", "--store-url", storePath, "--json")

	var report struct {
		OK         bool   `json:"ok"`
		WorkflowID string `json:"workflowId"`
		IssueCount int    `json:"issueCount"`
		Issues     []struct {
			Code      string `json:"code"`
			SubjectID string `json:"subjectId"`
		} `json:"issues"`
		Store *struct {
			LatestRun *struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"latestRun"`
			BindingCases []struct {
				StepID       string `json:"stepId"`
				CaseID       string `json:"caseId"`
				HasPassed    bool   `json:"hasPassed"`
				LatestStatus string `json:"latestStatus"`
				LatestRunID  string `json:"latestRunId"`
			} `json:"bindingCases"`
		} `json:"store"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode workflow audit json: %v\n%s", err, out)
	}
	if report.OK || report.WorkflowID != "workflow.alpha" || report.IssueCount != 2 {
		t.Fatalf("workflow audit summary = %#v", report)
	}
	if len(report.Issues) != 2 || report.Issues[0].Code != "api-case-node-missing" || report.Issues[1].Code != "case-dependency-fixture-missing" {
		t.Fatalf("workflow audit issues = %#v", report.Issues)
	}
	if report.Store == nil || report.Store.LatestRun == nil || report.Store.LatestRun.ID != "run.workflow.002" || report.Store.LatestRun.Status != store.StatusPassed {
		t.Fatalf("latest workflow run = %#v", report.Store)
	}
	caseState := map[string]struct {
		HasPassed    bool
		LatestStatus string
		LatestRunID  string
	}{}
	for _, item := range report.Store.BindingCases {
		caseState[item.CaseID] = struct {
			HasPassed    bool
			LatestStatus string
			LatestRunID  string
		}{HasPassed: item.HasPassed, LatestStatus: item.LatestStatus, LatestRunID: item.LatestRunID}
	}
	if !caseState["case.alpha"].HasPassed || caseState["case.alpha"].LatestStatus != store.StatusPassed || caseState["case.alpha"].LatestRunID != "run.workflow.002" {
		t.Fatalf("case.alpha workflow state = %#v", caseState["case.alpha"])
	}
	if caseState["case.beta"].HasPassed || caseState["case.beta"].LatestStatus != "" || caseState["case.beta"].LatestRunID != "" {
		t.Fatalf("case.beta workflow state = %#v", caseState["case.beta"])
	}
}

func TestWorkflowAuditCommandPrintsTextSummary(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)

	out := runCLI(t, "workflow", "audit", "--profile", dir, "--workflow", "workflow.alpha")

	for _, want := range []string{
		"Workflow Audit: workflow.alpha",
		"OK: true",
		"Issues: 0",
		"Bindings: 1",
		"Binding: step.one Node: node.alpha Case: case.alpha Required: true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workflow audit output missing %q: %q", want, out)
		}
	}
}

func TestTemplateRenderCommandPrintsRequestPreview(t *testing.T) {
	dir := t.TempDir()
	writeTemplateProfile(t, dir)

	out := runCLI(t, "template", "render", "--profile", dir, "--template", "template.create", "--fixture", "fixture.item")

	var rendered struct {
		Method string         `json:"method"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &rendered); err != nil {
		t.Fatalf("decode template render output: %v\n%s", err, out)
	}
	if rendered.Method != "POST" || rendered.Path != "/v1/items/item-001" {
		t.Fatalf("rendered request identity = %#v", rendered)
	}
	if rendered.Body["id"] != "item-001" || rendered.Body["quantity"].(float64) != 3 {
		t.Fatalf("rendered request body = %#v", rendered.Body)
	}
}

func TestEvidenceImportCommandIndexesLegacyRuntime(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--store-url", storePath)
	if !strings.Contains(out, "Imported evidence index") || !strings.Contains(out, "Runs: 2") || !strings.Contains(out, "API Case Runs: 1") {
		t.Fatalf("evidence import output = %q", out)
	}
}

func TestEvidenceImportCommandCanEmitJSONReport(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--store-url", storePath, "--json")

	var report struct {
		SourcePath      string `json:"sourcePath"`
		StorePath       string `json:"storePath"`
		ProfileID       string `json:"profileId"`
		RunCount        int    `json:"runCount"`
		APICaseRunCount int    `json:"apiCaseRunCount"`
		EvidenceCount   int    `json:"evidenceCount"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode evidence import json report: %v\n%s", err, out)
	}
	if report.SourcePath != sourcePath || report.StorePath != storePath || report.ProfileID != "sample" {
		t.Fatalf("report paths/profile = %#v", report)
	}
	if report.RunCount != 2 || report.APICaseRunCount != 1 || report.EvidenceCount != 1 {
		t.Fatalf("report counts = %#v", report)
	}
}

func TestEvidenceListCommandPrintsStoreRecords(t *testing.T) {
	storePath := createStoredCaseRun(t, "case-run-004")

	out := runCLI(t, "evidence", "list", "--store-url", storePath)

	for _, want := range []string{"Run: case-run-004", "Case Run: case-run-004.case", "Case: case.alpha", "Evidence: response"} {
		if !strings.Contains(out, want) {
			t.Fatalf("evidence list output missing %q: %q", want, out)
		}
	}
}

func TestEvidenceListCommandCanEmitJSON(t *testing.T) {
	storePath := createStoredCaseRun(t, "case-run-005")

	out := runCLI(t, "evidence", "list", "--store-url", storePath, "--json")

	var report struct {
		Runs []struct {
			ID              string `json:"id"`
			APICaseRunCount int    `json:"apiCaseRunCount"`
			EvidenceCount   int    `json:"evidenceCount"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode evidence list json: %v\n%s", err, out)
	}
	if len(report.Runs) != 1 || report.Runs[0].ID != "case-run-005" {
		t.Fatalf("json runs = %#v", report.Runs)
	}
	if report.Runs[0].APICaseRunCount != 1 || report.Runs[0].EvidenceCount != 5 {
		t.Fatalf("json run counts = %#v", report.Runs[0])
	}
}

func TestEvidenceListCommandRejectsMissingRun(t *testing.T) {
	storePath := createStoredCaseRun(t, "case-run-006")

	out := runCLIFails(t, "evidence", "list", "--store-url", storePath, "--run", "case-run-missing")
	if !strings.Contains(out, "run not found") || !strings.Contains(out, "case-run-missing") {
		t.Fatalf("missing run output = %q", out)
	}
}

func TestEvidenceTasksCommandListsPostProcessTasks(t *testing.T) {
	storePath := createPostProcessTaskStore(t)

	out := runCLI(t,
		"evidence", "tasks",
		"--store-url", storePath,
		"--run", "run.tasks",
		"--step", "step-a",
		"--kind", "trace_topology_collect",
		"--json",
	)
	var report struct {
		RunID  string `json:"runId"`
		Counts struct {
			Total      int   `json:"total"`
			Passed     int   `json:"passed"`
			Failed     int   `json:"failed"`
			Running    int   `json:"running"`
			DurationMs int64 `json:"durationMs"`
		} `json:"counts"`
		Tasks []struct {
			ID         string `json:"id"`
			RunID      string `json:"runId"`
			StepID     string `json:"stepId"`
			Kind       string `json:"kind"`
			Status     string `json:"status"`
			DurationMs int64  `json:"durationMs"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode evidence tasks json: %v\n%s", err, out)
	}
	if report.RunID != "run.tasks" || report.Counts.Total != 1 || report.Counts.Passed != 1 || report.Counts.DurationMs != 125 {
		t.Fatalf("evidence tasks report = %#v", report)
	}
	if len(report.Tasks) != 1 || report.Tasks[0].ID != "task.trace" || report.Tasks[0].StepID != "step-a" || report.Tasks[0].Kind != "trace_topology_collect" {
		t.Fatalf("evidence tasks = %#v", report.Tasks)
	}

	textOut := runCLI(t, "evidence", "tasks", "--store-url", storePath, "--run", "run.tasks", "--status", "failed")
	for _, want := range []string{"Post Process Tasks: run.tasks", "task.logs", "runtime_log_collect", "300 ms", "log source missing"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("evidence tasks text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseRunCommandWritesEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	evidenceDir := filepath.Join(dir, "evidence")

	out := runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-001", "--evidence-dir", evidenceDir)
	if !strings.Contains(out, "Case Run: case-run-001") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("case run output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "case-run-001", "summary.json")); err != nil {
		t.Fatalf("summary evidence missing: %v", err)
	}
}

func TestCaseRunCommandExecutesHTTPCase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["id"] != "item-override" {
			t.Fatalf("request overrides = %#v", request)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	evidenceDir := filepath.Join(dir, "evidence")

	out := runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-002", "--evidence-dir", evidenceDir, "--override", "id=item-override")
	if !strings.Contains(out, "Case Run: case-run-002") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("case run output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "case-run-002", "response.json")); err != nil {
		t.Fatalf("response evidence missing: %v", err)
	}
}

func TestCaseRunCommandIndexesStoreRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	storePath := filepath.Join(dir, "store.sqlite")
	evidenceDir := filepath.Join(dir, "evidence")

	runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-003", "--evidence-dir", evidenceDir, "--store-url", storePath, "--profile", "sample")

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	run, err := s.GetRun(context.Background(), "case-run-003")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.ProfileID != "sample" || run.Status != "passed" {
		t.Fatalf("run = %#v", run)
	}
	if !run.FinishedAt.After(run.StartedAt) {
		t.Fatalf("run timing was not indexed: %#v", run)
	}
	var runSummary struct {
		RunID  string `json:"runId"`
		CaseID string `json:"caseId"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(run.SummaryJSON), &runSummary); err != nil {
		t.Fatalf("decode run summary: %v", err)
	}
	if runSummary.RunID != "case-run-003" || runSummary.CaseID != "case.alpha" || runSummary.Status != "passed" {
		t.Fatalf("run summary = %#v", runSummary)
	}
	caseRuns, err := s.ListAPICaseRuns(context.Background(), "case-run-003")
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != "case.alpha" {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	if !caseRuns[0].FinishedAt.After(caseRuns[0].StartedAt) {
		t.Fatalf("case run timing was not indexed: %#v", caseRuns[0])
	}
	var requestSummary struct {
		Method  string `json:"method"`
		Path    string `json:"path"`
		HasBody bool   `json:"hasBody"`
	}
	if err := json.Unmarshal([]byte(caseRuns[0].RequestSummaryJSON), &requestSummary); err != nil {
		t.Fatalf("decode request summary: %v", err)
	}
	if requestSummary.Method != "POST" || requestSummary.Path != "/v1/items" || !requestSummary.HasBody {
		t.Fatalf("request summary = %#v", requestSummary)
	}
	var assertionSummary struct {
		Status     string `json:"status"`
		ErrorCount int    `json:"errorCount"`
	}
	if err := json.Unmarshal([]byte(caseRuns[0].AssertionSummaryJSON), &assertionSummary); err != nil {
		t.Fatalf("decode assertion summary: %v", err)
	}
	if assertionSummary.Status != "passed" || assertionSummary.ErrorCount != 0 {
		t.Fatalf("assertion summary = %#v", assertionSummary)
	}
	records, err := s.ListEvidence(context.Background(), "case-run-003")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("evidence records = %#v", records)
	}
	var responseSummary string
	for _, record := range records {
		if record.Kind == "response" {
			responseSummary = record.Summary
		}
	}
	var response struct {
		StatusCode int `json:"statusCode"`
		BodyBytes  int `json:"bodyBytes"`
	}
	if err := json.Unmarshal([]byte(responseSummary), &response); err != nil {
		t.Fatalf("decode response evidence summary: %v", err)
	}
	if response.StatusCode != http.StatusOK || response.BodyBytes == 0 {
		t.Fatalf("response evidence summary = %#v", response)
	}
}

func TestInterfaceNodeCaseReportRunsAllCasesByTargetName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "bad":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"status":"rejected","password":"variant-secret"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"accepted","token":"report-secret"}`)
		}
	}))
	defer server.Close()
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)
	listOut := runCLI(t, "interface-node", "discover", "--store-url", storePath, "--filter", "Result Lookup", "--json")
	var listReport struct {
		Items []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listOut), &listReport); err != nil {
		t.Fatalf("decode interface-node discover json: %v\n%s", err, listOut)
	}
	if len(listReport.Items) != 1 || listReport.Items[0].ID != "node.alpha" {
		t.Fatalf("interface-node discover = %#v", listReport.Items)
	}

	outputDir := filepath.Join(t.TempDir(), "report")
	out := runCLI(t,
		"interface-node", "case", "report",
		"--node", listReport.Items[0].ID,
		"--store-url", storePath,
		"--base-url", server.URL,
		"--output-dir", outputDir,
		"--timeout-seconds", "1",
		"--json",
	)

	var report struct {
		OK        bool   `json:"ok"`
		NodeID    string `json:"nodeId"`
		ReportURL string `json:"reportUrl"`
		Counts    struct {
			Total          int `json:"total"`
			Passed         int `json:"passed"`
			Failed         int `json:"failed"`
			DerivedConfigs int `json:"derivedConfigs"`
		} `json:"counts"`
		Results []struct {
			RunID       string `json:"runId"`
			CaseRunID   string `json:"caseRunId"`
			DetailURL   string `json:"detailUrl"`
			BodyPreview string `json:"bodyPreview"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode report json: %v\n%s", err, out)
	}
	if !report.OK || report.NodeID != "node.alpha" || report.Counts.Total != 2 || report.Counts.Passed != 2 || report.Counts.Failed != 0 || report.Counts.DerivedConfigs != 1 {
		t.Fatalf("report = %#v", report)
	}
	if len(report.Results) != 2 || report.Results[0].RunID == "" || report.Results[0].CaseRunID != report.Results[0].RunID+".case" || report.Results[0].DetailURL == "" {
		t.Fatalf("report evidence handles = %#v", report.Results)
	}
	for _, item := range report.Results {
		if strings.Contains(item.BodyPreview, "report-secret") || strings.Contains(item.BodyPreview, "variant-secret") {
			t.Fatalf("report body preview leaked sensitive value: %#v", item)
		}
		if !strings.Contains(item.BodyPreview, "[REDACTED]") {
			t.Fatalf("report body preview was not redacted: %#v", item)
		}
	}
	if _, err := os.Stat(filepath.Join(outputDir, "report.json")); err != nil {
		t.Fatalf("json report missing: %v", err)
	}
	htmlPath := filepath.Join(outputDir, "report.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("html report missing: %v", err)
	}
	for _, want := range []string{"Result Lookup", "Case Alpha Default", "Case Alpha Variant", "passed", "caseRunId"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("html report missing %q:\n%s", want, html)
		}
	}
	for _, leaked := range []string{"report-secret", "variant-secret"} {
		if strings.Contains(string(html), leaked) {
			t.Fatalf("html report leaked %q:\n%s", leaked, html)
		}
	}
	if report.ReportURL != htmlPath {
		t.Fatalf("report url = %q want %q", report.ReportURL, htmlPath)
	}
}

func TestCaseDiscoverFiltersByMaintenanceMetadata(t *testing.T) {
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	out := runCLI(t,
		"case", "discover",
		"--store-url", storePath,
		"--tag", "smoke",
		"--status", "active",
		"--owner", "team-a",
		"--json",
	)

	var report struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
		Items []struct {
			ID          string   `json:"id"`
			DisplayName string   `json:"displayName"`
			NodeID      string   `json:"nodeId"`
			Tags        []string `json:"tags"`
			Priority    string   `json:"priority"`
			Owner       string   `json:"owner"`
			Description string   `json:"description"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode case discover json: %v\n%s", err, out)
	}
	if !report.OK || report.Count != 1 || len(report.Items) != 1 {
		t.Fatalf("case discover report = %#v", report)
	}
	item := report.Items[0]
	if item.ID != "case.alpha.default" || item.NodeID != "node.alpha" || item.Priority != "p0" || item.Owner != "team-a" {
		t.Fatalf("case discover item = %#v", item)
	}
	if strings.Join(item.Tags, ",") != "smoke,regression" || item.Description == "" {
		t.Fatalf("case discover metadata = %#v", item)
	}

	filtered := runCLI(t, "case", "discover", "--store-url", storePath, "--filter", "variant", "--json")
	var filteredReport struct {
		Items []struct {
			ID    string   `json:"id"`
			Tags  []string `json:"tags"`
			Owner string   `json:"owner"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(filtered), &filteredReport); err != nil {
		t.Fatalf("decode filtered case discover json: %v\n%s", err, filtered)
	}
	if len(filteredReport.Items) != 1 || filteredReport.Items[0].ID != "case.alpha.variant" || filteredReport.Items[0].Owner != "team-b" {
		t.Fatalf("filtered case discover = %#v", filteredReport.Items)
	}
}

func TestCaseSuiteReportRunsCasesByMaintenanceFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "bad":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"status":"rejected"}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
		}
	}))
	defer server.Close()
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	outputDir := filepath.Join(t.TempDir(), "suite-report")
	out := runCLI(t,
		"case", "suite", "report",
		"--store-url", storePath,
		"--tag", "smoke",
		"--owner", "team-a",
		"--base-url", server.URL,
		"--output-dir", outputDir,
		"--json",
	)

	var report struct {
		OK             bool   `json:"ok"`
		JUnitReportURL string `json:"junitReportUrl"`
		Filters        struct {
			Tags  []string `json:"tags"`
			Owner string   `json:"owner"`
		} `json:"filters"`
		Counts struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"counts"`
		Results []struct {
			CaseID    string   `json:"caseId"`
			Title     string   `json:"title"`
			NodeID    string   `json:"nodeId"`
			Tags      []string `json:"tags"`
			Priority  string   `json:"priority"`
			Owner     string   `json:"owner"`
			Status    string   `json:"status"`
			CaseRunID string   `json:"caseRunId"`
			DetailURL string   `json:"detailUrl"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite report json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Total != 1 || report.Counts.Passed != 1 || report.Counts.Failed != 0 {
		t.Fatalf("suite report = %#v", report)
	}
	if strings.Join(report.Filters.Tags, ",") != "smoke" || report.Filters.Owner != "team-a" {
		t.Fatalf("suite filters = %#v", report.Filters)
	}
	if len(report.Results) != 1 {
		t.Fatalf("suite results = %#v", report.Results)
	}
	item := report.Results[0]
	if item.CaseID != "case.alpha.default" || item.NodeID != "node.alpha" || item.Priority != "p0" || item.Owner != "team-a" || item.CaseRunID == "" || item.DetailURL == "" {
		t.Fatalf("suite result item = %#v", item)
	}
	if strings.Join(item.Tags, ",") != "smoke,regression" {
		t.Fatalf("suite result tags = %#v", item.Tags)
	}
	html, err := os.ReadFile(filepath.Join(outputDir, "report.html"))
	if err != nil {
		t.Fatalf("suite html report missing: %v", err)
	}
	for _, want := range []string{"Case Suite Report", "Case Alpha Default", "team-a", "smoke", "p0", "caseRunId"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("suite html missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(string(html), "Case Alpha Variant") {
		t.Fatalf("suite html should not include unselected case:\n%s", html)
	}
	junitPath := filepath.Join(outputDir, "report.junit.xml")
	junitRaw, err := os.ReadFile(junitPath)
	if err != nil {
		t.Fatalf("suite junit report missing: %v", err)
	}
	if report.JUnitReportURL != junitPath {
		t.Fatalf("junit report url = %q want %q", report.JUnitReportURL, junitPath)
	}
	for _, want := range []string{`<testsuite name="Case Suite Report" tests="1" failures="0"`, `name="case.alpha.default"`, `classname="node.alpha"`} {
		if !strings.Contains(string(junitRaw), want) {
			t.Fatalf("suite junit missing %q:\n%s", want, junitRaw)
		}
	}

	variantOut := runCLI(t,
		"case", "suite", "report",
		"--store-url", storePath,
		"--tag", "negative",
		"--base-url", server.URL,
		"--output-dir", filepath.Join(t.TempDir(), "variant-suite-report"),
		"--json",
	)
	var variantReport struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total          int `json:"total"`
			Passed         int `json:"passed"`
			DerivedConfigs int `json:"derivedConfigs"`
		} `json:"counts"`
		Results []struct {
			CaseID   string `json:"caseId"`
			Priority string `json:"priority"`
			Owner    string `json:"owner"`
			HTTPCode int    `json:"httpCode"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(variantOut), &variantReport); err != nil {
		t.Fatalf("decode variant suite report json: %v\n%s", err, variantOut)
	}
	if !variantReport.OK || variantReport.Counts.Total != 1 || variantReport.Counts.Passed != 1 || variantReport.Counts.DerivedConfigs != 1 {
		t.Fatalf("variant suite report = %#v", variantReport)
	}
	if len(variantReport.Results) != 1 || variantReport.Results[0].CaseID != "case.alpha.variant" || variantReport.Results[0].HTTPCode != http.StatusBadRequest {
		t.Fatalf("variant suite result = %#v", variantReport.Results)
	}
}

func TestCaseSuiteCoverageReportsLatestRunStatusByMaintenanceFilters(t *testing.T) {
	ctx := context.Background()
	profileDir := writeCaseSuiteCoverageProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseRunForCoverage(t, ctx, s, "run.default.old", "case.default", store.StatusFailed, base)
	recordCaseRunForCoverage(t, ctx, s, "run.default.latest", "case.default", store.StatusPassed, base.Add(time.Minute))
	recordCaseRunForCoverage(t, ctx, s, "run.variant.latest", "case.variant", store.StatusFailed, base.Add(2*time.Minute))
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "coverage",
		"--profile", profileDir,
		"--store-url", storePath,
		"--tag", "regression",
		"--status", "active",
		"--json",
	)

	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
			NotRun int `json:"notRun"`
		} `json:"counts"`
		Items []struct {
			CaseID       string `json:"caseId"`
			Title        string `json:"title"`
			NodeID       string `json:"nodeId"`
			LatestStatus string `json:"latestStatus"`
			LatestRunID  string `json:"latestRunId"`
			CaseRunID    string `json:"caseRunId"`
			DetailURL    string `json:"detailUrl"`
			HasPassed    bool   `json:"hasPassed"`
			Reason       string `json:"reason"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite coverage json: %v\n%s", err, out)
	}
	if report.OK || report.Counts.Total != 3 || report.Counts.Passed != 1 || report.Counts.Failed != 1 || report.Counts.NotRun != 1 {
		t.Fatalf("suite coverage report = %#v", report)
	}
	byCase := map[string]struct {
		LatestStatus string
		LatestRunID  string
		CaseRunID    string
		DetailURL    string
		HasPassed    bool
		Reason       string
	}{}
	for _, item := range report.Items {
		byCase[item.CaseID] = struct {
			LatestStatus string
			LatestRunID  string
			CaseRunID    string
			DetailURL    string
			HasPassed    bool
			Reason       string
		}{item.LatestStatus, item.LatestRunID, item.CaseRunID, item.DetailURL, item.HasPassed, item.Reason}
	}
	if byCase["case.default"].LatestStatus != store.StatusPassed || byCase["case.default"].LatestRunID != "run.default.latest" || !byCase["case.default"].HasPassed {
		t.Fatalf("default coverage = %#v", byCase["case.default"])
	}
	if byCase["case.variant"].LatestStatus != store.StatusFailed || byCase["case.variant"].CaseRunID != "run.variant.latest.case" || byCase["case.variant"].DetailURL == "" || byCase["case.variant"].HasPassed {
		t.Fatalf("variant coverage = %#v", byCase["case.variant"])
	}
	if byCase["case.unrun"].LatestStatus != "not-run" || byCase["case.unrun"].Reason != "no run recorded in Store" {
		t.Fatalf("unrun coverage = %#v", byCase["case.unrun"])
	}

	textOut := runCLI(t, "case", "suite", "coverage", "--profile", profileDir, "--store-url", storePath, "--tag", "regression")
	for _, want := range []string{"Case Suite Coverage", "Total: 3 Passed: 1 Failed: 1 Not Run: 1", "case.variant", "run.variant.latest.case"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("coverage text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteInspectReportsReadinessByMaintenanceFilters(t *testing.T) {
	ctx := context.Background()
	profileDir := writeCaseSuiteCoverageProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseRunForCoverage(t, ctx, s, "run.default.latest", "case.default", store.StatusPassed, base)
	recordCaseRunForCoverage(t, ctx, s, "run.variant.latest", "case.variant", store.StatusFailed, base.Add(time.Minute))
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "inspect",
		"--profile", profileDir,
		"--store-url", storePath,
		"--tag", "regression",
		"--status", "active",
		"--json",
	)

	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total   int `json:"total"`
			Ready   int `json:"ready"`
			Blocked int `json:"blocked"`
			Failed  int `json:"failed"`
			NotRun  int `json:"notRun"`
		} `json:"counts"`
		Items []struct {
			CaseID             string   `json:"caseId"`
			Ready              bool     `json:"ready"`
			HasRunnableFile    bool     `json:"hasRunnableFile"`
			HasExecutionConfig bool     `json:"hasExecutionConfig"`
			LatestStatus       string   `json:"latestStatus"`
			Issues             []string `json:"issues"`
			SuggestedAction    string   `json:"suggestedAction"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite inspection json: %v\n%s", err, out)
	}
	if report.OK || report.Counts.Total != 3 || report.Counts.Ready != 2 || report.Counts.Blocked != 1 || report.Counts.Failed != 1 || report.Counts.NotRun != 1 {
		t.Fatalf("suite inspection report = %#v", report)
	}
	byCase := map[string]struct {
		Ready              bool
		HasRunnableFile    bool
		HasExecutionConfig bool
		LatestStatus       string
		Issues             []string
		SuggestedAction    string
	}{}
	for _, item := range report.Items {
		byCase[item.CaseID] = struct {
			Ready              bool
			HasRunnableFile    bool
			HasExecutionConfig bool
			LatestStatus       string
			Issues             []string
			SuggestedAction    string
		}{item.Ready, item.HasRunnableFile, item.HasExecutionConfig, item.LatestStatus, item.Issues, item.SuggestedAction}
	}
	if !byCase["case.default"].Ready || !byCase["case.default"].HasRunnableFile || byCase["case.default"].LatestStatus != store.StatusPassed {
		t.Fatalf("default inspection = %#v", byCase["case.default"])
	}
	if !byCase["case.variant"].Ready || !byCase["case.variant"].HasExecutionConfig || byCase["case.variant"].SuggestedAction != "rerun" {
		t.Fatalf("variant inspection = %#v", byCase["case.variant"])
	}
	if byCase["case.unrun"].Ready || byCase["case.unrun"].SuggestedAction != "add-runnable-source" || len(byCase["case.unrun"].Issues) == 0 {
		t.Fatalf("unrun inspection = %#v", byCase["case.unrun"])
	}

	textOut := runCLI(t, "case", "suite", "inspect", "--profile", profileDir, "--store-url", storePath, "--tag", "regression")
	for _, want := range []string{"Case Suite Inspection", "Total: 3 Ready: 2 Blocked: 1", "case.unrun", "add-runnable-source"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("inspection text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuitePlanBuildsExecutableBatchRequest(t *testing.T) {
	ctx := context.Background()
	profileDir := writeCaseSuiteCoverageProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseRunForCoverage(t, ctx, s, "run.default.latest", "case.default", store.StatusPassed, base)
	recordCaseRunForCoverage(t, ctx, s, "run.variant.latest", "case.variant", store.StatusFailed, base.Add(time.Minute))
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "plan",
		"--profile", profileDir,
		"--store-url", storePath,
		"--tag", "regression",
		"--status", "active",
		"--action", "run",
		"--action", "rerun",
		"--request-id", "change-001",
		"--base-url", "http://127.0.0.1:8080",
		"--evidence-dir", ".runtime/evidence",
		"--timeout-seconds", "7",
		"--json",
	)

	var report struct {
		OK      bool     `json:"ok"`
		CaseIDs []string `json:"caseIds"`
		Counts  struct {
			Total    int `json:"total"`
			Ready    int `json:"ready"`
			Blocked  int `json:"blocked"`
			Selected int `json:"selected"`
			Skipped  int `json:"skipped"`
		} `json:"counts"`
		BatchRequest struct {
			RequestID      string   `json:"requestId"`
			CaseIDs        []string `json:"caseIds"`
			BaseURL        string   `json:"baseUrl"`
			EvidenceDir    string   `json:"evidenceDir"`
			TimeoutSeconds int      `json:"timeoutSeconds"`
		} `json:"batchRequest"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite plan json: %v\n%s", err, out)
	}
	if !report.OK || strings.Join(report.CaseIDs, ",") != "case.variant" || report.Counts.Total != 3 || report.Counts.Ready != 2 || report.Counts.Blocked != 1 || report.Counts.Selected != 1 || report.Counts.Skipped != 1 {
		t.Fatalf("suite plan report = %#v", report)
	}
	if report.BatchRequest.RequestID != "change-001" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.variant" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" || report.BatchRequest.EvidenceDir != ".runtime/evidence" || report.BatchRequest.TimeoutSeconds != 7 {
		t.Fatalf("batch request = %#v", report.BatchRequest)
	}

	textOut := runCLI(t, "case", "suite", "plan", "--profile", profileDir, "--store-url", storePath, "--tag", "regression", "--action", "rerun")
	for _, want := range []string{"Case Suite Plan", "Selected: 1", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("plan text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteStabilityReportsTransitions(t *testing.T) {
	ctx := context.Background()
	profileDir := writeCaseSuiteCoverageProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseRunForCoverage(t, ctx, s, "run.variant.1", "case.variant", store.StatusPassed, base)
	recordCaseRunForCoverage(t, ctx, s, "run.variant.2", "case.variant", store.StatusFailed, base.Add(time.Minute))
	recordCaseRunForCoverage(t, ctx, s, "run.variant.3", "case.variant", store.StatusPassed, base.Add(2*time.Minute))
	recordCaseRunForCoverage(t, ctx, s, "run.default.1", "case.default", store.StatusPassed, base.Add(3*time.Minute))
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "stability",
		"--profile", profileDir,
		"--store-url", storePath,
		"--tag", "regression",
		"--status", "active",
		"--limit", "3",
		"--json",
	)
	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total    int `json:"total"`
			Stable   int `json:"stable"`
			Unstable int `json:"unstable"`
			NotRun   int `json:"notRun"`
		} `json:"counts"`
		Items []struct {
			CaseID       string `json:"caseId"`
			LatestStatus string `json:"latestStatus"`
			Transitions  int    `json:"transitions"`
			Unstable     bool   `json:"unstable"`
			Recent       []struct {
				RunID string `json:"runId"`
			} `json:"recent"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite stability json: %v\n%s", err, out)
	}
	if report.OK || report.Counts.Total != 3 || report.Counts.Unstable != 1 || report.Counts.Stable != 1 || report.Counts.NotRun != 1 {
		t.Fatalf("suite stability report = %#v", report)
	}
	byCase := map[string]struct {
		LatestStatus string
		Transitions  int
		Unstable     bool
		Recent       []struct {
			RunID string `json:"runId"`
		}
	}{}
	for _, item := range report.Items {
		byCase[item.CaseID] = struct {
			LatestStatus string
			Transitions  int
			Unstable     bool
			Recent       []struct {
				RunID string `json:"runId"`
			}
		}{item.LatestStatus, item.Transitions, item.Unstable, item.Recent}
	}
	if !byCase["case.variant"].Unstable || byCase["case.variant"].Transitions != 2 || byCase["case.variant"].LatestStatus != store.StatusPassed || byCase["case.variant"].Recent[0].RunID != "run.variant.3" {
		t.Fatalf("variant stability = %#v", byCase["case.variant"])
	}

	textOut := runCLI(t, "case", "suite", "stability", "--profile", profileDir, "--store-url", storePath, "--tag", "regression", "--limit", "3")
	for _, want := range []string{"Case Suite Stability", "Unstable: 1", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("stability text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuitePriorityBuildsRankedBatchRequest(t *testing.T) {
	ctx := context.Background()
	profileDir := writeCaseSuiteCoverageProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseRunForCoverage(t, ctx, s, "run.default.1", "case.default", store.StatusPassed, base)
	recordCaseRunForCoverage(t, ctx, s, "run.variant.1", "case.variant", store.StatusPassed, base.Add(time.Minute))
	recordCaseRunForCoverage(t, ctx, s, "run.variant.2", "case.variant", store.StatusFailed, base.Add(2*time.Minute))
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "priority",
		"--profile", profileDir,
		"--store-url", storePath,
		"--tag", "regression",
		"--status", "active",
		"--signal", "Variant",
		"--limit", "2",
		"--request-id", "change-011",
		"--base-url", "http://127.0.0.1:8080",
		"--json",
	)
	var report struct {
		OK      bool     `json:"ok"`
		CaseIDs []string `json:"caseIds"`
		Counts  struct {
			Total    int `json:"total"`
			Selected int `json:"selected"`
			Skipped  int `json:"skipped"`
			Blocked  int `json:"blocked"`
		} `json:"counts"`
		Selected []struct {
			CaseID  string   `json:"caseId"`
			Score   int      `json:"score"`
			Reasons []string `json:"reasons"`
		} `json:"selected"`
		BatchRequest struct {
			RequestID string   `json:"requestId"`
			CaseIDs   []string `json:"caseIds"`
			BaseURL   string   `json:"baseUrl"`
		} `json:"batchRequest"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite priority json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Total != 3 || report.Counts.Selected != 2 || report.Counts.Blocked != 1 || strings.Join(report.CaseIDs, ",") != "case.variant,case.default" {
		t.Fatalf("suite priority report = %#v", report)
	}
	if report.Selected[0].CaseID != "case.variant" || report.Selected[0].Score <= report.Selected[1].Score || len(report.Selected[0].Reasons) == 0 {
		t.Fatalf("suite priority selected = %#v", report.Selected)
	}
	if report.BatchRequest.RequestID != "change-011" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.variant,case.default" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("suite priority batch = %#v", report.BatchRequest)
	}

	textOut := runCLI(t, "case", "suite", "priority", "--profile", profileDir, "--store-url", storePath, "--tag", "regression", "--signal", "Variant", "--limit", "1")
	for _, want := range []string{"Case Suite Priority", "Selected: 1", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("priority text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteBriefSummarizesMaintainedSuiteForAgents(t *testing.T) {
	ctx := context.Background()
	profileDir := writeCaseSuiteCoverageProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseRunForCoverage(t, ctx, s, "run.default.1", "case.default", store.StatusPassed, base)
	recordCaseRunForCoverage(t, ctx, s, "run.variant.1", "case.variant", store.StatusPassed, base.Add(time.Minute))
	recordCaseRunForCoverage(t, ctx, s, "run.variant.2", "case.variant", store.StatusFailed, base.Add(2*time.Minute))
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "brief",
		"--profile", profileDir,
		"--store-url", storePath,
		"--tag", "regression",
		"--status", "active",
		"--signal", "Variant",
		"--limit", "2",
		"--request-id", "change-012",
		"--base-url", "http://127.0.0.1:8080",
		"--json",
	)
	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total            int `json:"total"`
			Ready            int `json:"ready"`
			Blocked          int `json:"blocked"`
			Failed           int `json:"failed"`
			PrioritySelected int `json:"prioritySelected"`
		} `json:"counts"`
		Recommended []struct {
			CaseID string `json:"caseId"`
			Score  int    `json:"score"`
		} `json:"recommended"`
		BatchRequest struct {
			RequestID string   `json:"requestId"`
			CaseIDs   []string `json:"caseIds"`
			BaseURL   string   `json:"baseUrl"`
		} `json:"batchRequest"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite brief json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Total != 3 || report.Counts.Ready != 2 || report.Counts.Blocked != 1 || report.Counts.Failed != 1 || report.Counts.PrioritySelected != 2 {
		t.Fatalf("suite brief report = %#v", report)
	}
	if len(report.Recommended) != 2 || report.Recommended[0].CaseID != "case.variant" || report.Recommended[0].Score <= report.Recommended[1].Score {
		t.Fatalf("suite brief recommended = %#v", report.Recommended)
	}
	if report.BatchRequest.RequestID != "change-012" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.variant,case.default" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("suite brief batch = %#v", report.BatchRequest)
	}

	textOut := runCLI(t, "case", "suite", "brief", "--profile", profileDir, "--store-url", storePath, "--tag", "regression", "--signal", "Variant")
	for _, want := range []string{"Case Suite Brief", "Ready: 2", "Recommended: 2", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("brief text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteQualityAuditsMaintainedCaseMetadata(t *testing.T) {
	profileDir := writeCaseSuiteQualityProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	out := runCLI(t,
		"case", "suite", "quality",
		"--profile", profileDir,
		"--store-url", storePath,
		"--status", "active",
		"--json",
	)
	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Nodes             int `json:"nodes"`
			NodesWithoutCases int `json:"nodesWithoutCases"`
			Cases             int `json:"cases"`
			CompleteCases     int `json:"completeCases"`
			IncompleteCases   int `json:"incompleteCases"`
			MissingOwner      int `json:"missingOwner"`
			MissingRunnable   int `json:"missingRunnable"`
			MissingExecution  int `json:"missingExecution"`
		} `json:"counts"`
		Cases []struct {
			CaseID   string   `json:"caseId"`
			Complete bool     `json:"complete"`
			Issues   []string `json:"issues"`
		} `json:"cases"`
		Nodes []struct {
			NodeID string   `json:"nodeId"`
			Issues []string `json:"issues"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite quality json: %v\n%s", err, out)
	}
	if report.OK || report.Counts.Nodes != 2 || report.Counts.NodesWithoutCases != 1 || report.Counts.Cases != 2 || report.Counts.CompleteCases != 1 || report.Counts.IncompleteCases != 1 {
		t.Fatalf("suite quality report = %#v", report)
	}
	if report.Counts.MissingOwner != 1 || report.Counts.MissingRunnable != 1 || report.Counts.MissingExecution != 1 {
		t.Fatalf("suite quality gaps = %#v", report.Counts)
	}
	if len(report.Nodes) != 1 || report.Nodes[0].NodeID != "node.empty" {
		t.Fatalf("suite quality nodes = %#v", report.Nodes)
	}
	textOut := runCLI(t, "case", "suite", "quality", "--profile", profileDir, "--store-url", storePath, "--status", "active")
	for _, want := range []string{"Case Suite Quality", "Incomplete: 1", "node.empty", "case.gaps"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("quality text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteQualityPlanSuggestsAuthoringActions(t *testing.T) {
	profileDir := writeCaseSuiteQualityProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	out := runCLI(t,
		"case", "suite", "quality-plan",
		"--profile", profileDir,
		"--store-url", storePath,
		"--status", "active",
		"--json",
	)
	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Total            int `json:"total"`
			DraftCase        int `json:"draftCase"`
			CompleteMetadata int `json:"completeMetadata"`
			AddRunnable      int `json:"addRunnable"`
			AddExecution     int `json:"addExecution"`
		} `json:"counts"`
		Actions []struct {
			Type            string   `json:"type"`
			NodeID          string   `json:"nodeId"`
			CaseID          string   `json:"caseId"`
			SuggestedCaseID string   `json:"suggestedCaseId"`
			Fields          []string `json:"fields"`
			Command         []string `json:"command"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite quality plan json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Total != 4 || report.Counts.DraftCase != 1 || report.Counts.CompleteMetadata != 1 || report.Counts.AddRunnable != 1 || report.Counts.AddExecution != 1 {
		t.Fatalf("suite quality plan report = %#v", report)
	}
	if len(report.Actions) != 4 || report.Actions[0].Type != "draft-case" || report.Actions[0].NodeID != "node.empty" || report.Actions[0].SuggestedCaseID != "case.node-empty.default" {
		t.Fatalf("suite quality plan actions = %#v", report.Actions)
	}
	textOut := runCLI(t, "case", "suite", "quality-plan", "--profile", profileDir, "--store-url", storePath, "--status", "active")
	for _, want := range []string{"Case Suite Quality Plan", "Draft Case: 1", "case.node-empty.default", "case.gaps"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("quality plan text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteQualityReportWritesJSONAndHTML(t *testing.T) {
	profileDir := writeCaseSuiteQualityProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	outputDir := filepath.Join(t.TempDir(), "quality-report")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	out := runCLI(t,
		"case", "suite", "quality-report",
		"--profile", profileDir,
		"--store-url", storePath,
		"--status", "active",
		"--output-dir", outputDir,
		"--json",
	)
	var report struct {
		OK            bool   `json:"ok"`
		ProfileID     string `json:"profileId"`
		ReportURL     string `json:"reportUrl"`
		JSONReportURL string `json:"jsonReportUrl"`
		QualityPlan   struct {
			Counts struct {
				Total            int `json:"total"`
				DraftCase        int `json:"draftCase"`
				CompleteMetadata int `json:"completeMetadata"`
				AddRunnable      int `json:"addRunnable"`
				AddExecution     int `json:"addExecution"`
			} `json:"counts"`
			Actions []struct {
				Type            string `json:"type"`
				CaseID          string `json:"caseId"`
				SuggestedCaseID string `json:"suggestedCaseId"`
			} `json:"actions"`
		} `json:"qualityPlan"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite quality report json: %v\n%s", err, out)
	}
	if !report.OK || report.ProfileID != "sample" || report.QualityPlan.Counts.Total != 4 || report.QualityPlan.Counts.DraftCase != 1 || report.QualityPlan.Counts.CompleteMetadata != 1 || report.QualityPlan.Counts.AddRunnable != 1 || report.QualityPlan.Counts.AddExecution != 1 {
		t.Fatalf("suite quality report = %#v", report)
	}
	if report.ReportURL != filepath.Join(outputDir, "report.html") || report.JSONReportURL != filepath.Join(outputDir, "report.json") {
		t.Fatalf("suite quality report paths = %#v", report)
	}
	jsonReportRaw, err := os.ReadFile(filepath.Join(outputDir, "report.json"))
	if err != nil {
		t.Fatalf("read quality json report: %v", err)
	}
	htmlReportRaw, err := os.ReadFile(filepath.Join(outputDir, "report.html"))
	if err != nil {
		t.Fatalf("read quality html report: %v", err)
	}
	jsonReport := string(jsonReportRaw)
	htmlReport := string(htmlReportRaw)
	for _, want := range []string{"Case Suite Quality Report", "case.node-empty.default", "case.gaps", "complete-case-metadata", "add-execution-config"} {
		if !strings.Contains(htmlReport, want) {
			t.Fatalf("quality html missing %q:\n%s", want, htmlReport)
		}
	}
	if !strings.Contains(jsonReport, `"qualityPlan"`) || !strings.Contains(jsonReport, `"case.node-empty.default"`) {
		t.Fatalf("quality json report missing expected content:\n%s", jsonReport)
	}

	textOut := runCLI(t, "case", "suite", "quality-report", "--profile", profileDir, "--store-url", storePath, "--status", "active", "--output-dir", filepath.Join(t.TempDir(), "text-quality-report"))
	for _, want := range []string{"Case Suite Quality Report", "Total Actions: 4", "Report:"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("quality report text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteImpactBuildsExecutableBatchRequest(t *testing.T) {
	ctx := context.Background()
	profileDir := writeCaseSuiteCoverageProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	base := mustParseTime(t, "2026-05-16T01:00:00Z")
	recordCaseRunForCoverage(t, ctx, s, "run.default.latest", "case.default", store.StatusPassed, base)
	recordCaseRunForCoverage(t, ctx, s, "run.variant.latest", "case.variant", store.StatusFailed, base.Add(time.Minute))
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t,
		"case", "suite", "impact",
		"--profile", profileDir,
		"--store-url", storePath,
		"--signal", "/alpha",
		"--status", "active",
		"--action", "run",
		"--action", "rerun",
		"--request-id", "change-002",
		"--base-url", "http://127.0.0.1:8080",
		"--json",
	)

	var report struct {
		OK     bool `json:"ok"`
		Counts struct {
			Signals  int `json:"signals"`
			Nodes    int `json:"nodes"`
			Cases    int `json:"cases"`
			Selected int `json:"selected"`
			Blocked  int `json:"blocked"`
		} `json:"counts"`
		BatchRequest struct {
			RequestID string   `json:"requestId"`
			CaseIDs   []string `json:"caseIds"`
			BaseURL   string   `json:"baseUrl"`
		} `json:"batchRequest"`
		Cases []struct {
			CaseID  string   `json:"caseId"`
			Reasons []string `json:"reasons"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode suite impact json: %v\n%s", err, out)
	}
	if !report.OK || report.Counts.Signals != 1 || report.Counts.Nodes != 1 || report.Counts.Cases != 3 || report.Counts.Selected != 1 || report.Counts.Blocked != 1 {
		t.Fatalf("suite impact report = %#v", report)
	}
	if report.BatchRequest.RequestID != "change-002" || strings.Join(report.BatchRequest.CaseIDs, ",") != "case.variant" || report.BatchRequest.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("impact batch request = %#v", report.BatchRequest)
	}
	if len(report.Cases) != 3 || len(report.Cases[0].Reasons) == 0 {
		t.Fatalf("impact cases = %#v", report.Cases)
	}

	textOut := runCLI(t, "case", "suite", "impact", "--profile", profileDir, "--store-url", storePath, "--signal", "/alpha", "--action", "rerun")
	for _, want := range []string{"Case Suite Impact", "Selected: 1", "case.variant"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("impact text missing %q:\n%s", want, textOut)
		}
	}
}

func TestCaseSuiteImpactReportRunsImpactedCases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lookup" || r.URL.Query().Get("mode") != "ok" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
	}))
	defer server.Close()
	profileDir := writeInterfaceNodeBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)

	outputDir := filepath.Join(t.TempDir(), "impact-report")
	out := runCLI(t,
		"case", "suite", "impact-report",
		"--profile", profileDir,
		"--store-url", storePath,
		"--signal", "/lookup",
		"--tag", "smoke",
		"--status", "active",
		"--action", "run",
		"--request-id", "change-003",
		"--base-url", server.URL,
		"--output-dir", outputDir,
		"--json",
	)

	var report struct {
		OK     bool `json:"ok"`
		Impact struct {
			BatchRequest struct {
				RequestID string   `json:"requestId"`
				CaseIDs   []string `json:"caseIds"`
			} `json:"batchRequest"`
		} `json:"impact"`
		Report struct {
			OK        bool   `json:"ok"`
			ReportURL string `json:"reportUrl"`
			Counts    struct {
				Total  int `json:"total"`
				Passed int `json:"passed"`
				Failed int `json:"failed"`
			} `json:"counts"`
			Results []struct {
				CaseID    string `json:"caseId"`
				CaseRunID string `json:"caseRunId"`
				Status    string `json:"status"`
			} `json:"results"`
		} `json:"report"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode impact report json: %v\n%s", err, out)
	}
	if !report.OK || report.Impact.BatchRequest.RequestID != "change-003" || strings.Join(report.Impact.BatchRequest.CaseIDs, ",") != "case.alpha.default" {
		t.Fatalf("impact report selection = %#v", report)
	}
	if !report.Report.OK || report.Report.Counts.Total != 1 || report.Report.Counts.Passed != 1 || report.Report.Counts.Failed != 0 || len(report.Report.Results) != 1 {
		t.Fatalf("impact execution report = %#v", report.Report)
	}
	if report.Report.Results[0].CaseID != "case.alpha.default" || report.Report.Results[0].CaseRunID == "" || report.Report.Results[0].Status != store.StatusPassed {
		t.Fatalf("impact execution item = %#v", report.Report.Results[0])
	}
	if _, err := os.Stat(filepath.Join(outputDir, "report.html")); err != nil {
		t.Fatalf("impact report html missing: %v", err)
	}
}

func TestWorkflowReportWritesReportWhenStepFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/first":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"item_id":"item-001"}`)
		case "/second":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"status":"failed"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	profileDir := writeWorkflowBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	runCLI(t, "config", "publish", "--from", profileDir, "--store-url", storePath)
	listOut := runCLI(t, "workflow", "discover", "--store-url", storePath, "--filter", "Workflow Alpha", "--json")
	var listReport struct {
		Items []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listOut), &listReport); err != nil {
		t.Fatalf("decode workflow discover json: %v\n%s", err, listOut)
	}
	if len(listReport.Items) != 1 || listReport.Items[0].ID != "workflow.alpha" {
		t.Fatalf("workflow discover = %#v", listReport.Items)
	}

	outputDir := filepath.Join(t.TempDir(), "workflow-report")
	out := runCLI(t,
		"workflow", "report",
		"--workflow", listReport.Items[0].ID,
		"--store-url", storePath,
		"--base-url", server.URL,
		"--output-dir", outputDir,
		"--json",
	)

	var report struct {
		OK        bool   `json:"ok"`
		RunID     string `json:"runId"`
		ReportURL string `json:"reportUrl"`
		Counts    struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"counts"`
		Steps []struct {
			RunID     string `json:"runId"`
			CaseRunID string `json:"caseRunId"`
			DetailURL string `json:"detailUrl"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode workflow report json: %v\n%s", err, out)
	}
	if report.OK || report.RunID == "" || report.Counts.Total != 2 || report.Counts.Passed != 1 || report.Counts.Failed != 1 {
		t.Fatalf("workflow report = %#v", report)
	}
	if len(report.Steps) != 2 || report.Steps[1].RunID == "" || report.Steps[1].CaseRunID != report.Steps[1].RunID+".case" || report.Steps[1].DetailURL == "" {
		t.Fatalf("workflow report evidence handles = %#v", report.Steps)
	}
	htmlPath := filepath.Join(outputDir, "report.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("html report missing: %v", err)
	}
	for _, want := range []string{"Workflow Alpha", "First Step", "Second Step", "failed", "caseRunId"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("workflow html missing %q:\n%s", want, html)
		}
	}
	if report.ReportURL != htmlPath {
		t.Fatalf("report url = %q want %q", report.ReportURL, htmlPath)
	}
}

func TestCaseIncompleteBatchesCommandReportsNotRunCases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	alphaPath := filepath.Join(dir, "case.alpha.json")
	betaPath := filepath.Join(dir, "case.beta.json")
	writeAPICaseFile(t, alphaPath)
	writeFile(t, betaPath, `{
  "id": "case.beta",
  "title": "Read Item",
  "request": {"method": "GET", "path": "/v1/items/item-001"},
  "assertions": {"expectedStatusCodes": [200]}
}`)
	writeFile(t, filepath.Join(profileDir, "profile.json"), fmt.Sprintf(`{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","casePath":%q},
    {"id":"case.beta","displayName":"Case Beta","casePath":%q}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`, alphaPath, betaPath))

	storePath := filepath.Join(dir, "store.sqlite")
	runCLI(t, "case", "run", "--case", alphaPath, "--base-url", server.URL, "--run-id", "run-alpha", "--store-url", storePath, "--profile", "sample")

	out := runCLI(t, "case", "incomplete-batches", "--profile", profileDir, "--store-url", storePath)
	for _, want := range []string{"Incomplete API Cases: 1", "case.beta", "not-run", betaPath} {
		if !strings.Contains(out, want) {
			t.Fatalf("incomplete case output missing %q: %q", want, out)
		}
	}

	jsonOut := runCLI(t, "case", "incomplete-batches", "--profile", profileDir, "--store-url", storePath, "--json")
	var report struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
		Items []struct {
			ID      string `json:"id"`
			Reason  string `json:"reason"`
			Command string `json:"suggestedCommand"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("decode incomplete cases report: %v\n%s", err, jsonOut)
	}
	if !report.OK || report.Count != 1 || len(report.Items) != 1 {
		t.Fatalf("incomplete cases report = %#v", report)
	}
	if report.Items[0].ID != "case.beta" || report.Items[0].Reason != "not-run" {
		t.Fatalf("incomplete case item = %#v", report.Items[0])
	}
	if !strings.Contains(report.Items[0].Command, betaPath) {
		t.Fatalf("suggested command = %q", report.Items[0].Command)
	}
}

func TestServeHandlerUsesConfiguredStore(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "empty",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  `{"steps":[{"stepId":"step.alpha","ok":true}]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	handler, cleanup, err := serveHandlerFromArgs([]string{
		"--store-url", storePath,
	})
	if err != nil {
		t.Fatalf("build serve handler: %v", err)
	}
	defer cleanup()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "run.alpha") {
		t.Fatalf("serve handler did not use configured store: %s", rec.Body.String())
	}
}

func TestServeHandlerCanBootFromPublishedStoreCatalogWithoutProfilePath(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	sourcePath := filepath.Join(t.TempDir(), "sources", "service-alpha", "main-4e8d26674209")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "team-alpha",
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http", SourcePath: sourcePath},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "create", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	handler, cleanup, err := serveHandlerFromArgs([]string{"--store-url", storePath})
	if err != nil {
		t.Fatalf("build serve handler from store catalog: %v", err)
	}
	defer cleanup()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/interface-nodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("interface nodes status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Source struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"source"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode interface nodes payload: %v\n%s", err, rec.Body.String())
	}
	if payload.Source.ID != "team-alpha" || payload.Source.Kind != "store" || len(payload.Items) != 1 || payload.Items[0].ID != "node.alpha" {
		t.Fatalf("serve handler did not use published catalog: %#v", payload)
	}

	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d body=%s", dashboard.Code, dashboard.Body.String())
	}
	if !strings.Contains(dashboard.Body.String(), sourcePath) || !strings.Contains(dashboard.Body.String(), "4e8d26674209") {
		t.Fatalf("dashboard did not use published runtime source: %s", dashboard.Body.String())
	}
}

func TestServeBundleLoadsPublishedProfilePathWhenAvailable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	profileDir := filepath.Join(dir, "external-profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha","casePath":"runnable/case-alpha.json"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(profileDir, "runnable", "case-alpha.json"), `{"id":"case.alpha","request":{"method":"GET","path":"/v1/items"},"assertions":{"expectedStatusCodes":[200]}}`)

	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if _, err := publishProfileBundleToStore(ctx, s, profileDir, storePath, false, false); err != nil {
		t.Fatalf("publish profile: %v", err)
	}

	bundle, err := serveBundle(ctx, s)
	if err != nil {
		t.Fatalf("serve bundle: %v", err)
	}
	if bundle.BaseDir != profileDir {
		t.Fatalf("serve bundle base dir = %q, want %q", bundle.BaseDir, profileDir)
	}
	if len(bundle.APICases) != 1 || bundle.APICases[0].CasePath != "runnable/case-alpha.json" {
		t.Fatalf("serve bundle api cases = %#v", bundle.APICases)
	}
}

func TestServeHandlerPublishesProfilePathIntoStoreBeforeServing(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := filepath.Join(t.TempDir(), "external-profile")
	writeWorkflowProfile(t, profileDir)

	handler, cleanup, err := serveHandlerFromArgs([]string{
		"--profile", profileDir,
		"--store-url", storePath,
	})
	if err != nil {
		t.Fatalf("build serve handler with profile path: %v", err)
	}
	defer cleanup()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/interface-nodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("interface nodes status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Source struct {
			ID string `json:"id"`
		} `json:"source"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode interface nodes payload: %v\n%s", err, rec.Body.String())
	}
	if payload.Source.ID != "sample" || len(payload.Items) != 1 || payload.Items[0].ID != "node.alpha" {
		t.Fatalf("interface nodes payload = %#v", payload)
	}
	if got := sqliteScalar(t, storePath, "select value from kv where key = 'active_profile_id';"); got != "sample" {
		t.Fatalf("active profile id = %q", got)
	}
	if got := sqliteScalar(t, storePath, "select count(*) from config_read_model where profile_id = 'sample';"); got == "0" {
		t.Fatalf("expected serve --profile to publish read models")
	}
}

func TestServeHandlerPublishesInstalledProfileIDBeforeServing(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	profileHome := filepath.Join(t.TempDir(), "profile-home")
	sourceDir := filepath.Join(t.TempDir(), "external-profile")
	writeWorkflowProfile(t, sourceDir)
	runCLI(t, "profile", "install", "--from", sourceDir, "--profile-home", profileHome)

	handler, cleanup, err := serveHandlerFromArgs([]string{
		"--profile", "sample",
		"--profile-home", profileHome,
		"--store-url", storePath,
	})
	if err != nil {
		t.Fatalf("build serve handler with installed profile id: %v", err)
	}
	defer cleanup()

	profiles := httptest.NewRecorder()
	handler.ServeHTTP(profiles, httptest.NewRequest(http.MethodGet, "/api/profile/installed", nil))
	if profiles.Code != http.StatusOK || !strings.Contains(profiles.Body.String(), profileHome) {
		t.Fatalf("installed profiles response = %d %s", profiles.Code, profiles.Body.String())
	}
	if got := sqliteScalar(t, storePath, "select value from kv where key = 'active_profile_id';"); got != "sample" {
		t.Fatalf("active profile id = %q", got)
	}
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func sqliteScalar(t *testing.T, dbPath string, statement string) string {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, statement).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite scalar failed: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func hasProfileVerifyCheck(checks []struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
}, name string) bool {
	for _, check := range checks {
		if check.Name == name && check.OK {
			return true
		}
	}
	return false
}

func runCLIFails(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("go run . %s unexpectedly succeeded:\n%s", strings.Join(args, " "), out)
	}
	return string(out)
}

func readTarGZEntries(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive %s: %v", path, err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("open gzip %s: %v", path, err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var entries []string
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read archive %s: %v", path, err)
		}
		entries = append(entries, header.Name)
	}
	return entries
}

func writeTarGZEntries(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive %s: %v", path, err)
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	defer gz.Close()
	writer := tar.NewWriter(gz)
	defer writer.Close()
	for name, body := range entries {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(body)),
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("write archive header %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatalf("write archive entry %s: %v", name, err)
		}
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func createStoredCaseRun(t *testing.T, runID string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	storePath := filepath.Join(dir, "store.sqlite")
	evidenceDir := filepath.Join(dir, "evidence")

	runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", runID, "--evidence-dir", evidenceDir, "--store-url", storePath, "--profile", "sample")
	return storePath
}

func createPostProcessTaskStore(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open post process task store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close post process task store: %v", err)
		}
	})
	base := time.Date(2026, 5, 17, 1, 2, 3, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.tasks",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  base,
		FinishedAt: base.Add(time.Second),
		CreatedAt:  base,
		UpdatedAt:  base.Add(time.Second),
	}); err != nil {
		t.Fatalf("create task run: %v", err)
	}
	records := []store.PostProcessTask{
		{
			ID:         "task.trace",
			RunID:      "run.tasks",
			WorkflowID: "workflow.alpha",
			StepID:     "step-a",
			CaseID:     "case.alpha",
			Kind:       "trace_topology_collect",
			Status:     store.StatusPassed,
			StartedAt:  base.Add(10 * time.Millisecond),
			FinishedAt: base.Add(135 * time.Millisecond),
			CreatedAt:  base.Add(10 * time.Millisecond),
		},
		{
			ID:          "task.logs",
			RunID:       "run.tasks",
			WorkflowID:  "workflow.alpha",
			StepID:      "step-b",
			CaseID:      "case.beta",
			Kind:        "runtime_log_collect",
			Status:      store.StatusFailed,
			StartedAt:   base.Add(200 * time.Millisecond),
			FinishedAt:  base.Add(500 * time.Millisecond),
			Error:       "log source missing",
			SummaryJSON: `{"source":"runtime-log"}`,
			CreatedAt:   base.Add(200 * time.Millisecond),
		},
	}
	for _, record := range records {
		if _, err := s.RecordPostProcessTask(ctx, record); err != nil {
			t.Fatalf("record post process task %s: %v", record.ID, err)
		}
	}
	return storePath
}

func writeAPICaseFile(t *testing.T, path string) {
	t.Helper()
	raw := []byte(`{
  "id": "case.alpha",
  "title": "Create Item",
  "request": {
    "method": "POST",
    "path": "/v1/items",
    "headers": {"Content-Type": "application/json"},
    "body": {"id": "item-001"}
  },
  "assertions": {
    "expectedStatusCodes": [200],
    "responseContains": ["created"]
  }
}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write api case: %v", err)
	}
}

func writeEmptyProfileBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "empty",
  "displayName": "Empty Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	return dir
}

func writeWorkflowProfile(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "workflows", "workflow.json"), `{"id":"workflow.alpha","displayName":"Workflow Alpha"}`)
	writeFile(t, filepath.Join(dir, "interface-nodes", "node.json"), `{"id":"node.alpha","displayName":"Node Alpha"}`)
	writeFile(t, filepath.Join(dir, "cases", "case.json"), `{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}`)
	writeFile(t, filepath.Join(dir, "workflow-bindings", "binding.json"), `{"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.alpha","required":true}`)
}

func writeTemplateProfile(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "request-templates", "template.json"), `{
  "id": "template.create",
  "method": "POST",
  "path": "/v1/items/{{.itemId}}",
  "templateJson": "{\"id\":\"{{.itemId}}\",\"quantity\":{{.quantity}}}"
}`)
	writeFile(t, filepath.Join(dir, "fixtures", "fixture.json"), `{
  "id": "fixture.item",
  "kind": "json",
  "dataJson": "{\"itemId\":\"item-001\",\"quantity\":3}"
}`)
}

func writeInterfaceNodeCaseProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.alpha"}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "schemaVersion": "1",
  "templateConfigs": [
    {
      "id": "cfg.case.alpha",
      "templateId": "case-execution",
      "nodeId": "node.alpha",
      "scopeType": "case",
      "scopeId": "case.alpha",
      "title": "Case Alpha execution",
      "status": "active",
      "sortOrder": 1,
      "configJson": "{\"caseId\":\"case.alpha\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"service.alpha\",\"path\":\"/alpha\",\"expectedHttpCodes\":[200]}}"
    }
  ]
}`)
	return dir
}

func writeInterfaceNodeCoverageProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha","serviceId":"service.alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.alpha","nodeId":"node.alpha","caseId":"case.alpha","required":true}],
  "fixtures": []
}`)
	return dir
}

func writeInterfaceNodeBatchReportProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Result Lookup","serviceId":"service.alpha","operation":"Result Lookup","method":"GET","path":"/lookup"}],
  "apiCases": [
    {"id":"case.alpha.default","displayName":"Case Alpha Default","nodeId":"node.alpha","payloadTemplateJson":"{\"mode\":\"ok\"}","expectedJson":"{\"expectedHttpCodes\":[200]}","sortOrder":1,"tags":["smoke","regression"],"priority":"p0","owner":"team-a","description":"Default maintained smoke case."},
    {"id":"case.alpha.variant","displayName":"Case Alpha Variant","nodeId":"node.alpha","payloadTemplateJson":"{\"mode\":\"bad\"}","expectedJson":"{\"expectedHttpCodes\":[400]}","sortOrder":2,"tags":["negative"],"priority":"p1","owner":"team-b","description":"Negative maintained variant."}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "schemaVersion": "1",
  "templateConfigs": [
    {
      "id": "cfg.case.alpha.default",
      "templateId": "case-execution",
      "nodeId": "node.alpha",
      "scopeType": "case",
      "scopeId": "case.alpha.default",
      "title": "Case Alpha Default execution",
      "status": "active",
      "sortOrder": 1,
      "configJson": "{\"caseId\":\"case.alpha.default\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"service.alpha\",\"path\":\"/lookup\",\"query\":{\"mode\":\"ok\"},\"expectedHttpCodes\":[200]}}"
    }
  ]
}`)
	return dir
}

func writeCaseSuiteCoverageProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha","serviceId":"service.alpha","operation":"Alpha","method":"GET","path":"/alpha"}],
  "apiCases": [
    {"id":"case.default","displayName":"Default Case","nodeId":"node.alpha","sortOrder":1,"tags":["regression","smoke"],"priority":"p0","owner":"team-a","description":"Default maintained case.","casePath":"cases/default.json"},
    {"id":"case.variant","displayName":"Variant Case","nodeId":"node.alpha","sortOrder":2,"tags":["regression"],"priority":"p1","owner":"team-a","description":"Variant maintained case."},
    {"id":"case.unrun","displayName":"Unrun Case","nodeId":"node.alpha","sortOrder":3,"tags":["regression"],"priority":"p2","owner":"team-b","description":"Unrun maintained case."}
  ],
  "requestTemplates": [],
  "templateConfigs": [
    {
      "id": "config.case.variant",
      "scopeType": "case",
      "scopeId": "case.variant",
      "status": "active",
      "configJson": "{\"caseId\":\"case.variant\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"node.alpha\",\"path\":\"/alpha\",\"expectedHttpCodes\":[200]}}"
    }
  ],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	return dir
}

func writeCaseSuiteQualityProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [],
  "interfaceNodes": [
    {"id":"node.alpha","displayName":"Node Alpha","serviceId":"service.alpha","operation":"Alpha","method":"GET","path":"/alpha"},
    {"id":"node.empty","displayName":"Node Empty","serviceId":"service.alpha","operation":"Empty","method":"GET","path":"/empty"}
  ],
  "apiCases": [
    {"id":"case.complete","displayName":"Complete Case","description":"Ready maintained case.","nodeId":"node.alpha","sortOrder":1,"tags":["regression"],"priority":"p0","owner":"team-a","casePath":"cases/complete.json"},
    {"id":"case.gaps","displayName":"Gap Case","nodeId":"node.alpha","sortOrder":2}
  ],
  "requestTemplates": [],
  "templateConfigs": [
    {
      "id": "config.case.complete",
      "scopeType": "case",
      "scopeId": "case.complete",
      "status": "active",
      "configJson": "{\"caseId\":\"case.complete\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"node.alpha\",\"path\":\"/alpha\",\"expectedHttpCodes\":[200]}}"
    }
  ],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	return dir
}

func recordCaseRunForCoverage(t *testing.T, ctx context.Context, s store.Store, runID string, caseID string, status string, at time.Time) {
	t.Helper()
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         runID,
		ProfileID:  "sample",
		WorkflowID: caseID,
		Status:     status,
		StartedAt:  at,
		FinishedAt: at.Add(time.Second),
		CreatedAt:  at,
		UpdatedAt:  at.Add(time.Second),
	}); err != nil {
		t.Fatalf("create coverage run %s: %v", runID, err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         runID + ".case",
		RunID:      runID,
		CaseID:     caseID,
		Status:     status,
		StartedAt:  at,
		FinishedAt: at.Add(time.Second),
		CreatedAt:  at,
	}); err != nil {
		t.Fatalf("record coverage case run %s: %v", runID, err)
	}
}

func writeWorkflowBatchReportProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha","baseStepTimeoutMs":1000}],
  "interfaceNodes": [
    {"id":"node.first","displayName":"First Node","serviceId":"service.alpha","method":"GET","path":"/first"},
    {"id":"node.second","displayName":"Second Node","serviceId":"service.alpha","method":"GET","path":"/second"}
  ],
  "apiCases": [
    {"id":"case.first","displayName":"First Step Case","nodeId":"node.first","sortOrder":1},
    {"id":"case.second","displayName":"Second Step Case","nodeId":"node.second","sortOrder":2}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [
    {"workflowId":"workflow.alpha","stepId":"first","nodeId":"node.first","caseId":"case.first","required":true,"sortOrder":1},
    {"workflowId":"workflow.alpha","stepId":"second","nodeId":"node.second","caseId":"case.second","required":true,"sortOrder":2}
  ],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "schemaVersion": "1",
  "templateConfigs": [
    {
      "id": "cfg.step.first",
      "templateId": "case-execution",
      "workflowId": "workflow.alpha",
      "nodeId": "service.alpha",
      "scopeType": "step",
      "scopeId": "first",
      "title": "First Step",
      "status": "active",
      "sortOrder": 1,
      "configJson": "{\"caseId\":\"case.first\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"service.alpha\",\"path\":\"/first\",\"expectedHttpCodes\":[200]},\"exports\":[{\"name\":\"item_id\",\"from\":\"responseBody\",\"path\":\"item_id\"}]}"
    },
    {
      "id": "cfg.step.second",
      "templateId": "case-execution",
      "workflowId": "workflow.alpha",
      "nodeId": "service.alpha",
      "scopeType": "step",
      "scopeId": "second",
      "title": "Second Step",
      "status": "active",
      "sortOrder": 2,
      "configJson": "{\"caseId\":\"case.second\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"service.alpha\",\"path\":\"/second\",\"expectedHttpCodes\":[200]},\"inputs\":[{\"name\":\"item_id\",\"source\":\"previous\"}]}"
    }
  ]
}`)
	return dir
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
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

func createLegacyRuntimeDB(t *testing.T, path string) {
	t.Helper()
	statement := `
create table workflow_runs (
  id integer primary key,
  workflow_id text not null,
  status text not null,
  summary_json text not null default '',
  created_at text not null
);
create table interface_node_case_run (
  id integer primary key,
  node_id text not null,
  case_id text not null,
  run_id text not null,
  status text not null,
  failure_kind text not null default '',
  failure_reason text not null default '',
  evidence_path text not null default '',
  elapsed_ms integer not null default 0,
  summary_json text not null default '',
  created_at text not null
);
insert into workflow_runs(id, workflow_id, status, summary_json, created_at)
values (7, 'workflow.alpha', 'passed', '{"steps":1}', '2026-05-14T01:02:03Z');
insert into interface_node_case_run(id, node_id, case_id, run_id, status, evidence_path, summary_json, created_at)
values (11, 'node.alpha', 'case.alpha', 'case-run-parent', 'failed', '.runtime/cases/case-run-parent', '{"failure":"expected"}', '2026-05-14T01:03:03Z');
`
	cmd := exec.Command("sqlite3", path, statement)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create legacy db: %v\n%s", err, out)
	}
}
