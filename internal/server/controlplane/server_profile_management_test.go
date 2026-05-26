package controlplane_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profilehome"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerListsInstalledProfilesFromProfileHome(t *testing.T) {
	profileHome := t.TempDir()
	installedDir := filepath.Join(profileHome, "sample")
	writeWorkbenchSampleProfileAt(t, installedDir)
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{ProfileHome: profileHome}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/profile/installed", http.StatusOK)

	if payload["profileHome"] != profileHome {
		t.Fatalf("installed profile home = %#v", payload)
	}
	items, ok := payload["profiles"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("installed profiles = %#v", payload["profiles"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("installed profile item = %#v", items[0])
	}
	counts, ok := item["counts"].(map[string]any)
	if item["id"] != "sample" || item["displayName"] != "Sample Profile" || item["path"] != installedDir || !ok || counts["workflows"] != float64(1) || counts["apiCases"] != float64(1) {
		t.Fatalf("installed profile item = %#v", item)
	}
	if digest, ok := item["bundleDigest"].(string); !ok || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("installed profile digest = %#v", item["bundleDigest"])
	}
}

func TestServerListsInvalidInstalledProfileWithoutFailing(t *testing.T) {
	profileHome := t.TempDir()
	brokenDir := filepath.Join(profileHome, "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("create broken profile dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "profile.json"), []byte(`{"id":`), 0o644); err != nil {
		t.Fatalf("write broken profile: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{ProfileHome: profileHome}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/profile/installed", http.StatusOK)

	items, ok := payload["profiles"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("installed profiles = %#v", payload["profiles"])
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["id"] != "broken" || item["path"] != brokenDir || item["valid"] != false || item["error"] == "" {
		t.Fatalf("invalid installed profile item = %#v", items[0])
	}
}

func TestServerInstallsProfileBundleIntoProfileHome(t *testing.T) {
	sourceDir := writeWorkbenchSampleProfile(t)
	profileHome := t.TempDir()
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{ProfileHome: profileHome}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/install", `{"path":`+mustJSON(t, sourceDir)+`}`, http.StatusOK)

	targetPath := filepath.Join(profileHome, "sample")
	if payload["id"] != "sample" || payload["targetPath"] != targetPath {
		t.Fatalf("install payload = %#v", payload)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "profile.json")); err != nil {
		t.Fatalf("installed manifest missing: %v", err)
	}
	list := decodeJSONResponse(t, server.URL+"/api/profile/installed", http.StatusOK)
	items, ok := list["profiles"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("installed profiles after install = %#v", list)
	}
}

func TestServerImportsPackedProfileArchiveIntoRuntimeStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	sourceDir := writeWorkbenchSampleProfile(t)
	archivePath := filepath.Join(t.TempDir(), "sample-profile.tgz")
	if _, err := profilehome.Pack(sourceDir, "", archivePath, false); err != nil {
		t.Fatalf("pack sample profile: %v", err)
	}
	profileHome := t.TempDir()
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{Runtime: s, ProfileHome: profileHome}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`+mustJSON(t, archivePath)+`}`, http.StatusOK)

	targetPath := filepath.Join(profileHome, "sample")
	if payload["profileId"] != "sample" || payload["bundlePath"] != targetPath {
		t.Fatalf("archive import payload = %#v", payload)
	}
	if _, err := os.Stat(filepath.Join(targetPath, "profile.json")); err != nil {
		t.Fatalf("installed archive profile missing: %v", err)
	}
	index, err := s.GetProfileIndex(ctx, "sample")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if index.BundlePath != targetPath || !strings.HasPrefix(index.BundleDigest, "sha256:") {
		t.Fatalf("archive profile index = %#v", index)
	}
	active := decodeJSONResponse(t, server.URL+"/api/profile", http.StatusOK)
	if active["id"] != "sample" {
		t.Fatalf("active profile after archive import = %#v", active)
	}
}

func TestServerVerifiesPackedProfileArchiveIntoRuntimeStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	sourceDir := writeWorkbenchSampleProfile(t)
	archivePath := filepath.Join(t.TempDir(), "sample-profile.tgz")
	if _, err := profilehome.Pack(sourceDir, "", archivePath, false); err != nil {
		t.Fatalf("pack sample profile: %v", err)
	}
	profileHome := t.TempDir()
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{Runtime: s, ProfileHome: profileHome}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, archivePath)+`}`, http.StatusOK)

	targetPath := filepath.Join(profileHome, "sample")
	if payload["ok"] != true || payload["profileId"] != "sample" {
		t.Fatalf("archive verify payload = %#v", payload)
	}
	publish, ok := payload["publish"].(map[string]any)
	if !ok || publish["bundlePath"] != targetPath {
		t.Fatalf("archive verify publish = %#v", payload["publish"])
	}
	index, err := s.GetProfileIndex(ctx, "sample")
	if err != nil {
		t.Fatalf("get verified archive profile index: %v", err)
	}
	if index.BundlePath != targetPath || !strings.HasPrefix(index.BundleDigest, "sha256:") {
		t.Fatalf("verified archive profile index = %#v", index)
	}
}

func TestServerCanVerifyInstalledProfileByID(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileHome := t.TempDir()
	installedDir := filepath.Join(profileHome, "sample")
	writeWorkbenchSampleProfileAt(t, installedDir)
	server := httptest.NewServer(controlplane.NewWithOptions(loadEmptyProfile(t), controlplane.Options{Runtime: s, ProfileHome: profileHome}))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":"sample"}`, http.StatusOK)

	if payload["ok"] != true || payload["profileId"] != "sample" {
		t.Fatalf("verify installed profile payload = %#v", payload)
	}
	publish, ok := payload["publish"].(map[string]any)
	if !ok || publish["bundlePath"] != installedDir {
		t.Fatalf("verify installed profile publish = %#v", payload["publish"])
	}
	active := decodeJSONResponse(t, server.URL+"/api/profile", http.StatusOK)
	if active["id"] != "sample" {
		t.Fatalf("active profile after installed verify = %#v", active)
	}
}

func TestServerImportsProfileBundleWithAudit(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeAuditSampleProfile(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`+mustJSON(t, profileDir)+`,"audit":true}`, http.StatusOK)

	audit, ok := payload["audit"].(map[string]any)
	if !ok {
		t.Fatalf("missing audit in import payload = %#v", payload)
	}
	if audit["ok"] != false || audit["issueCount"] != float64(2) {
		t.Fatalf("audit summary = %#v", audit)
	}
	auditStore, ok := audit["store"].(map[string]any)
	if !ok || auditStore["profileIndexed"] != true || auditStore["digestMatches"] != true {
		t.Fatalf("audit store = %#v", audit["store"])
	}
}

func TestServerCanRequireCleanProfileAuditBeforeImport(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeAuditSampleProfile(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`+mustJSON(t, profileDir)+`,"requireAuditOk":true}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "profile audit failed") {
		t.Fatalf("strict import payload = %#v", payload)
	}
	if _, err := s.GetProfileIndex(ctx, "sample"); err == nil {
		t.Fatalf("strict import wrote profile index")
	} else if err != store.ErrNotFound {
		t.Fatalf("get profile index after strict import: %v", err)
	}
}

func TestServerExposesProfileAuditRepairPlan(t *testing.T) {
	profileDir := writeAuditSampleProfile(t)
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/audit-plan", `{"path":`+mustJSON(t, profileDir)+`}`, http.StatusOK)

	if payload["ok"] != true || payload["profileId"] != "sample" || payload["actionCount"] != float64(2) {
		t.Fatalf("profile audit plan payload = %#v", payload)
	}
	counts, ok := payload["counts"].(map[string]any)
	if !ok || counts["updateReferenceOrAddAsset"] != float64(2) {
		t.Fatalf("profile audit plan counts = %#v", payload["counts"])
	}
	actions, ok := payload["actions"].([]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("profile audit plan actions = %#v", payload["actions"])
	}
	first := actions[0].(map[string]any)
	if first["type"] != "update-reference-or-add-asset" || first["issueCode"] != "api-case-node-missing" || first["subjectId"] != "case.alpha" {
		t.Fatalf("profile audit plan first action = %#v", first)
	}

	missing := postJSONResponse(t, server.URL+"/api/profile/audit-plan", `{}`, http.StatusBadRequest)
	if missing["ok"] != false || !strings.Contains(fmt.Sprint(missing["error"]), "path is required") {
		t.Fatalf("profile audit plan missing path = %#v", missing)
	}
}

func TestServerVerifiesProfileBundleBeforeActivation(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeEmptyProfileBundle(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`}`, http.StatusOK)

	if payload["ok"] != true || payload["profileId"] != "empty" {
		t.Fatalf("profile verify payload = %#v", payload)
	}
	audit, ok := payload["audit"].(map[string]any)
	if !ok || audit["ok"] != true || audit["issueCount"] != float64(0) {
		t.Fatalf("profile verify audit = %#v", payload["audit"])
	}
	publish, ok := payload["publish"].(map[string]any)
	if !ok || publish["profileId"] != "empty" || publish["configVersion"] == nil {
		t.Fatalf("profile verify publish = %#v", payload["publish"])
	}
	checks, ok := payload["checks"].([]any)
	if !ok || len(checks) < 6 {
		t.Fatalf("profile verify checks = %#v", payload["checks"])
	}
	for _, raw := range checks {
		check, ok := raw.(map[string]any)
		if !ok || check["ok"] != true || check["detail"] == "" {
			t.Fatalf("profile verify check = %#v", raw)
		}
	}
	index, err := s.GetProfileIndex(ctx, "empty")
	if err != nil {
		t.Fatalf("get verified profile index: %v", err)
	}
	if index.BundleDigest == "" {
		t.Fatalf("verified profile index = %#v", index)
	}
	model, err := s.GetReadModel(ctx, "empty", controlplane.ReadModelDashboard)
	if err != nil {
		t.Fatalf("get verified dashboard read model: %v", err)
	}
	if model.ConfigVersionID == "" {
		t.Fatalf("verified dashboard read model = %#v", model)
	}
}

func TestServerProfileVerifyCanRequirePassedAPICaseRuns(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeWorkbenchSampleProfile(t)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.alpha",
		ProfileID:  "sample",
		WorkflowID: "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
		UpdatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
	}); err != nil {
		t.Fatalf("create api case run parent: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "case-run.alpha",
		RunID:      "run.alpha",
		CaseID:     "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  mustParseTime(t, "2026-05-14T01:00:00Z"),
		FinishedAt: mustParseTime(t, "2026-05-14T01:00:01Z"),
		CreatedAt:  mustParseTime(t, "2026-05-14T01:00:01Z"),
	}); err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`,"requireCaseRuns":true}`, http.StatusOK)

	if payload["ok"] != true {
		t.Fatalf("profile verify runtime payload = %#v", payload)
	}
	checks, ok := payload["checks"].([]any)
	if !ok || !hasJSONCheck(checks, "api-case-run:case.alpha") {
		t.Fatalf("profile verify runtime checks = %#v", payload["checks"])
	}
}

func TestServerProfileVerifyFailureIncludesDiagnosticReport(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeWorkbenchSampleProfile(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`,"requireCaseRuns":true}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "profile verification failed") {
		t.Fatalf("profile verify failure payload = %#v", payload)
	}
	summary, ok := payload["summary"].(map[string]any)
	if !ok || summary["firstFailed"] != "api-case-run:case.alpha" || summary["failedChecks"] != float64(1) {
		t.Fatalf("profile verify failure summary = %#v", payload["summary"])
	}
	checks, ok := payload["checks"].([]any)
	if !ok || !hasJSONFailedCheck(checks, "api-case-run:case.alpha") {
		t.Fatalf("profile verify failure checks = %#v", payload["checks"])
	}
}

func TestServerProfileVerifyCanRequirePassedWorkflowRuns(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeWorkbenchSampleProfile(t)
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
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`,"requireWorkflowRuns":true}`, http.StatusOK)

	if payload["ok"] != true {
		t.Fatalf("profile verify workflow payload = %#v", payload)
	}
	checks, ok := payload["checks"].([]any)
	if !ok || !hasJSONCheck(checks, "workflow-run:workflow.alpha") {
		t.Fatalf("profile verify workflow checks = %#v", payload["checks"])
	}
	summary, ok := payload["summary"].(map[string]any)
	if !ok {
		t.Fatalf("profile verify workflow summary missing: %#v", payload)
	}
	if summary["totalChecks"] != float64(len(checks)) || summary["passedChecks"] != float64(len(checks)) || summary["failedChecks"] != float64(0) {
		t.Fatalf("profile verify workflow summary counts = %#v checks=%d", summary, len(checks))
	}
	if summary["requiredWorkflowRuns"] != true || summary["requiredCaseRuns"] != false {
		t.Fatalf("profile verify workflow summary gates = %#v", summary)
	}
}

func TestServerProfileVerifyStopsBeforePublishWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	profileDir := writeAuditSampleProfile(t)
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/verify", `{"path":`+mustJSON(t, profileDir)+`}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "profile audit failed") {
		t.Fatalf("profile verify failure payload = %#v", payload)
	}
	if _, err := s.GetProfileIndex(ctx, "sample"); err == nil {
		t.Fatalf("profile verify wrote profile index after audit failure")
	} else if err != store.ErrNotFound {
		t.Fatalf("get profile index after verify failure: %v", err)
	}
}

func TestServerRejectsProfileImportWithoutRuntimeStore(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":"/tmp/external-profile"}`, http.StatusNotImplemented)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "runtime store") {
		t.Fatalf("missing store payload = %#v", payload)
	}
}

func TestServerRejectsCatalogIndexWithoutRuntimeStore(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/profile/catalog-index", http.StatusNotImplemented)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "runtime store") {
		t.Fatalf("missing store catalog index payload = %#v", payload)
	}
}

func TestServerRejectsProfileImportNonPost(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/profile/import")
	if err != nil {
		t.Fatalf("get profile import: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("profile import get status = %d", resp.StatusCode)
	}
}

func TestServerRejectsProfileImportInvalidJSON(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "invalid json") {
		t.Fatalf("invalid json payload = %#v", payload)
	}
}

func TestServerRejectsProfileImportWithoutPath(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"audit":true}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "path is required") {
		t.Fatalf("missing path payload = %#v", payload)
	}
}

func TestServerRejectsProfileImportForInvalidProfile(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), []byte(`{"id":"","displayName":"Broken Profile"}`), 0o644); err != nil {
		t.Fatalf("write invalid profile: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(loadEmptyProfile(t), s))
	defer server.Close()

	payload := postJSONResponse(t, server.URL+"/api/profile/import", `{"path":`+mustJSON(t, dir)+`}`, http.StatusBadRequest)

	if payload["ok"] != false || !strings.Contains(fmt.Sprint(payload["error"]), "load profile") {
		t.Fatalf("invalid profile payload = %#v", payload)
	}
}
