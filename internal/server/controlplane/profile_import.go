package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/domain/profileaudit"
	"open-test-sandbox/internal/domain/profilecatalog"
	"open-test-sandbox/internal/domain/profilehome"
	"open-test-sandbox/internal/store"
)

type profileImportRequest struct {
	TemplatePackagePath string `json:"templatePackagePath"`
	Path                string `json:"path"`
	Audit               bool   `json:"audit"`
	RequireAuditOK      bool   `json:"requireAuditOk"`
	RequireCaseRuns     bool   `json:"requireCaseRuns"`
	RequireWorkflowRuns bool   `json:"requireWorkflowRuns"`
	Force               bool   `json:"force"`
}

type profileInstallRequest struct {
	TemplatePackagePath string `json:"templatePackagePath"`
	Path                string `json:"path"`
	Force               bool   `json:"force"`
}

type profileAuditPlanRequest struct {
	TemplatePackagePath string `json:"templatePackagePath"`
	Path                string `json:"path"`
	Force               bool   `json:"force"`
}

type profileImportResponse struct {
	TemplatePackageID     string                     `json:"templatePackageId"`
	TemplatePackagePath   string                     `json:"templatePackagePath"`
	TemplatePackageDigest string                     `json:"templatePackageDigest"`
	ProfileID             string                     `json:"profileId"`
	BundlePath            string                     `json:"bundlePath"`
	BundleDigest          string                     `json:"bundleDigest"`
	ImportedAt            time.Time                  `json:"importedAt"`
	Counts                profileImportCounts        `json:"counts"`
	Store                 profileImportStore         `json:"store"`
	ConfigVersion         profileImportConfigVersion `json:"configVersion"`
	ReadModels            []string                   `json:"readModels"`
	Audit                 *profileaudit.Report       `json:"audit,omitempty"`
}

type profileImportCounts struct {
	Services         int `json:"services"`
	Workflows        int `json:"workflows"`
	InterfaceNodes   int `json:"interfaceNodes"`
	APICases         int `json:"apiCases"`
	RequestTemplates int `json:"requestTemplates"`
	CaseDependencies int `json:"caseDependencies"`
	WorkflowBindings int `json:"workflowBindings"`
	Fixtures         int `json:"fixtures"`
}

type profileImportStore struct {
	TemplatePackageID     string    `json:"templatePackageId"`
	TemplatePackagePath   string    `json:"templatePackagePath"`
	TemplatePackageDigest string    `json:"templatePackageDigest"`
	ProfileID             string    `json:"profileId"`
	BundlePath            string    `json:"bundlePath"`
	BundleDigest          string    `json:"bundleDigest"`
	ImportedAt            time.Time `json:"importedAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type profileImportConfigVersion struct {
	ID                    string    `json:"id"`
	TemplatePackageID     string    `json:"templatePackageId"`
	TemplatePackagePath   string    `json:"templatePackagePath"`
	TemplatePackageDigest string    `json:"templatePackageDigest"`
	ProfileID             string    `json:"profileId"`
	SourcePath            string    `json:"sourcePath"`
	BundleDigest          string    `json:"bundleDigest"`
	Active                bool      `json:"active"`
	PublishedAt           time.Time `json:"publishedAt"`
	CreatedAt             time.Time `json:"createdAt"`
}

type profileVerifyResponse struct {
	OK                bool                  `json:"ok"`
	Error             string                `json:"error,omitempty"`
	TemplatePackageID string                `json:"templatePackageId"`
	ProfileID         string                `json:"profileId"`
	Audit             profileaudit.Report   `json:"audit"`
	Publish           profileImportResponse `json:"publish"`
	Summary           profileVerifySummary  `json:"summary"`
	Checks            []profileVerifyCheck  `json:"checks"`
}

type profileVerifySummary struct {
	TotalChecks          int    `json:"totalChecks"`
	PassedChecks         int    `json:"passedChecks"`
	FailedChecks         int    `json:"failedChecks"`
	RequiredCaseRuns     bool   `json:"requiredCaseRuns"`
	RequiredWorkflowRuns bool   `json:"requiredWorkflowRuns"`
	FirstFailed          string `json:"firstFailed,omitempty"`
}

type profileVerifyCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type profileVerifyOptions struct {
	RequireCaseRuns     bool
	RequireWorkflowRuns bool
}

func templatePackageRequestPath(templatePackagePath string, legacyPath string) string {
	if value := strings.TrimSpace(templatePackagePath); value != "" {
		return value
	}
	return strings.TrimSpace(legacyPath)
}

func handleProfileImport(w http.ResponseWriter, r *http.Request, runtime store.Store, activate func(profile.Bundle), profileHome string) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "runtime store is not configured"})
		return
	}
	var req profileImportRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	req.Path = templatePackageRequestPath(req.TemplatePackagePath, req.Path)
	if req.Path == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "path is required"})
		return
	}
	resolvedPath, err := profilehome.ResolveReference(req.Path, profileHome)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	req.Path, err = materializeImportProfilePath(resolvedPath, profileHome, req.Force)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	bundle, report, err := importProfileBundle(r.Context(), runtime, req)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.HasPrefix(err.Error(), "load profile") || strings.HasPrefix(err.Error(), "digest profile") || strings.HasPrefix(err.Error(), "audit profile") || strings.HasPrefix(err.Error(), "profile audit failed") {
			status = http.StatusBadRequest
		}
		writeJSONStatus(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if activate != nil {
		activate(bundle)
	}
	writeJSON(w, report)
}

func handleProfileVerify(w http.ResponseWriter, r *http.Request, runtime store.Store, activate func(profile.Bundle), profileHome string) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "runtime store is not configured"})
		return
	}
	var req profileImportRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	req.Path = templatePackageRequestPath(req.TemplatePackagePath, req.Path)
	if req.Path == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "path is required"})
		return
	}
	resolvedPath, err := profilehome.ResolveReference(req.Path, profileHome)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	req.Path, err = materializeImportProfilePath(resolvedPath, profileHome, req.Force)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	bundle, report, err := verifyProfileBundle(r.Context(), runtime, req.Path, profileVerifyOptions{
		RequireCaseRuns:     req.RequireCaseRuns,
		RequireWorkflowRuns: req.RequireWorkflowRuns,
	})
	if err != nil {
		if report.ProfileID != "" {
			if report.Error == "" {
				report.Error = err.Error()
			}
			writeJSONStatus(w, http.StatusBadRequest, report)
			return
		}
		status := http.StatusInternalServerError
		if isProfileRequestError(err) {
			status = http.StatusBadRequest
		}
		writeJSONStatus(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if activate != nil {
		activate(bundle)
	}
	writeJSON(w, report)
}

func handleProfileAuditPlan(w http.ResponseWriter, r *http.Request, runtime store.Store, profileHome string) {
	var req profileAuditPlanRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	req.Path = templatePackageRequestPath(req.TemplatePackagePath, req.Path)
	if req.Path == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "path is required"})
		return
	}
	resolvedPath, err := profilehome.ResolveReference(req.Path, profileHome)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	resolvedPath, err = materializeImportProfilePath(resolvedPath, profileHome, req.Force)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	bundle, err := profile.Load(resolvedPath)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	audit, err := profileaudit.Audit(r.Context(), profileaudit.Options{
		Bundle:     bundle,
		BundlePath: resolvedPath,
		Store:      runtime,
	})
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, profileaudit.RepairPlan(audit))
}

func materializeImportProfilePath(path string, profileHome string, force bool) (string, error) {
	if !profilehome.IsArchivePath(path) {
		return path, nil
	}
	report, err := profilehome.Install(path, profileHome, force)
	if err != nil {
		return "", err
	}
	return report.TargetPath, nil
}

func handleInstalledProfiles(w http.ResponseWriter, _ *http.Request, profileHome string) {
	report, err := profilehome.List(profileHome)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func handleProfileInstall(w http.ResponseWriter, r *http.Request, profileHome string) {
	var req profileInstallRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	req.Path = templatePackageRequestPath(req.TemplatePackagePath, req.Path)
	if req.Path == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "path is required"})
		return
	}
	report, err := profilehome.Install(req.Path, profileHome, req.Force)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func importProfileBundle(ctx context.Context, runtime store.Store, req profileImportRequest) (profile.Bundle, profileImportResponse, error) {
	bundle, err := profile.Load(req.Path)
	if err != nil {
		return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("load profile %q: %w", req.Path, err)
	}
	if req.RequireAuditOK {
		auditReport, err := profileaudit.Audit(ctx, profileaudit.Options{
			Bundle:     bundle,
			BundlePath: req.Path,
		})
		if err != nil {
			return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("audit profile %q: %w", bundle.ID, err)
		}
		if !auditReport.OK {
			return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("profile audit failed for profile %q: %s", bundle.ID, profileaudit.FailureSummary(auditReport))
		}
	}
	digest, err := profile.BundleDigest(req.Path)
	if err != nil {
		return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("digest profile %q: %w", req.Path, err)
	}
	counts := bundle.Counts()
	summary, err := json.Marshal(counts)
	if err != nil {
		return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("summarize profile %q: %w", bundle.ID, err)
	}
	importedAt := time.Now().UTC()
	index, err := runtime.UpsertProfileIndex(ctx, store.ProfileIndex{
		ProfileID:    bundle.ID,
		BundlePath:   req.Path,
		BundleDigest: digest,
		SummaryJSON:  string(summary),
		ImportedAt:   importedAt,
	})
	if err != nil {
		return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("store profile index %q: %w", bundle.ID, err)
	}
	catalog := profilecatalog.FromBundle(bundle, importedAt)
	if err := runtime.ReplaceProfileCatalog(ctx, catalog); err != nil {
		return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("store profile catalog %q: %w", bundle.ID, err)
	}
	configVersion, err := runtime.UpsertConfigVersion(ctx, store.ConfigVersion{
		ID:           profileImportConfigVersionID(bundle.ID, importedAt),
		ProfileID:    bundle.ID,
		SourcePath:   req.Path,
		BundleDigest: digest,
		SummaryJSON:  string(summary),
		Active:       true,
		PublishedAt:  importedAt,
		CreatedAt:    importedAt,
	})
	if err != nil {
		return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("store config version %q: %w", bundle.ID, err)
	}
	readModelKeys, err := UpsertProfileReadModels(ctx, runtime, catalog, configVersion.ID, importedAt)
	if err != nil {
		return profile.Bundle{}, profileImportResponse{}, err
	}
	response := profileImportResponse{
		TemplatePackageID:     bundle.ID,
		TemplatePackagePath:   req.Path,
		TemplatePackageDigest: digest,
		ProfileID:             bundle.ID,
		BundlePath:            req.Path,
		BundleDigest:          digest,
		ImportedAt:            importedAt,
		Counts:                profileImportCountsFrom(counts),
		Store: profileImportStore{
			TemplatePackageID:     index.ProfileID,
			TemplatePackagePath:   index.BundlePath,
			TemplatePackageDigest: index.BundleDigest,
			ProfileID:             index.ProfileID,
			BundlePath:            index.BundlePath,
			BundleDigest:          index.BundleDigest,
			ImportedAt:            index.ImportedAt,
			UpdatedAt:             index.UpdatedAt,
		},
		ConfigVersion: profileImportConfigVersionFromStore(configVersion),
		ReadModels:    readModelKeys,
	}
	if req.Audit {
		auditReport, err := profileaudit.Audit(ctx, profileaudit.Options{
			Bundle:     bundle,
			BundlePath: req.Path,
			Store:      runtime,
		})
		if err != nil {
			return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("audit profile %q: %w", bundle.ID, err)
		}
		response.Audit = &auditReport
	}
	return bundle, response, nil
}

func verifyProfileBundle(ctx context.Context, runtime store.Store, path string, options profileVerifyOptions) (profile.Bundle, profileVerifyResponse, error) {
	bundle, err := profile.Load(path)
	if err != nil {
		return profile.Bundle{}, profileVerifyResponse{}, fmt.Errorf("load profile %q: %w", path, err)
	}
	auditReport, err := profileaudit.Audit(ctx, profileaudit.Options{
		Bundle:     bundle,
		BundlePath: path,
	})
	if err != nil {
		return profile.Bundle{}, profileVerifyResponse{}, fmt.Errorf("audit profile %q: %w", bundle.ID, err)
	}
	if !auditReport.OK {
		return profile.Bundle{}, profileVerifyResponse{}, fmt.Errorf("profile audit failed for profile %q: %s", bundle.ID, profileaudit.FailureSummary(auditReport))
	}
	bundle, publish, err := importProfileBundle(ctx, runtime, profileImportRequest{
		Path:           path,
		Audit:          true,
		RequireAuditOK: true,
	})
	if err != nil {
		return profile.Bundle{}, profileVerifyResponse{}, err
	}
	checks, err := verifyPublishedProfile(ctx, runtime, bundle, publish, options)
	if err != nil {
		return profile.Bundle{}, profileVerifyResponse{}, err
	}
	report := profileVerifyResponse{
		OK:                profileVerifyChecksOK(checks),
		TemplatePackageID: bundle.ID,
		ProfileID:         bundle.ID,
		Audit:             *publish.Audit,
		Publish:           publish,
		Summary:           summarizeProfileVerification(checks, options),
		Checks:            checks,
	}
	if !report.OK {
		report.Error = fmt.Sprintf("profile verification failed for profile %q: %s", bundle.ID, firstFailedProfileVerifyCheck(checks))
		return profile.Bundle{}, report, errors.New(report.Error)
	}
	return bundle, report, nil
}

func summarizeProfileVerification(checks []profileVerifyCheck, options profileVerifyOptions) profileVerifySummary {
	summary := profileVerifySummary{
		TotalChecks:          len(checks),
		RequiredCaseRuns:     options.RequireCaseRuns,
		RequiredWorkflowRuns: options.RequireWorkflowRuns,
	}
	for _, check := range checks {
		if check.OK {
			summary.PassedChecks++
			continue
		}
		summary.FailedChecks++
		if summary.FirstFailed == "" {
			summary.FirstFailed = check.Name
		}
	}
	return summary
}

func verifyPublishedProfile(ctx context.Context, runtime store.Store, bundle profile.Bundle, report profileImportResponse, options profileVerifyOptions) ([]profileVerifyCheck, error) {
	checks := make([]profileVerifyCheck, 0, 6)
	index, err := runtime.GetProfileIndex(ctx, report.ProfileID)
	if err != nil {
		if err == store.ErrNotFound {
			checks = appendProfileVerifyCheck(checks, "profile-index", false, "profile index was not written")
			return checks, nil
		}
		return nil, err
	}
	checks = appendProfileVerifyCheck(checks, "profile-index", index.BundleDigest == report.BundleDigest, "profile index digest matches published bundle")

	catalogIndex, err := runtime.GetProfileCatalogIndex(ctx)
	if err != nil {
		if err == store.ErrNotFound {
			checks = appendProfileVerifyCheck(checks, "catalog-index", false, "catalog index was not written")
		} else {
			return nil, err
		}
	} else {
		checks = appendProfileVerifyCheck(checks, "catalog-index", catalogIndex.ProfileID == report.ProfileID, "catalog index points to active profile")
	}

	activeConfig, err := runtime.GetActiveConfigVersion(ctx)
	if err != nil {
		if err == store.ErrNotFound {
			checks = appendProfileVerifyCheck(checks, "active-config", false, "active config version was not written")
		} else {
			return nil, err
		}
	} else {
		ok := activeConfig.ID == report.ConfigVersion.ID && activeConfig.ProfileID == report.ProfileID && activeConfig.BundleDigest == report.BundleDigest
		checks = appendProfileVerifyCheck(checks, "active-config", ok, "active config version matches published bundle")
	}

	for _, key := range []string{profilecatalog.ReadModelInterfaceNodes, ReadModelCatalog, ReadModelDashboard} {
		model, err := runtime.GetReadModel(ctx, report.ProfileID, key)
		if err != nil {
			if err == store.ErrNotFound {
				checks = appendProfileVerifyCheck(checks, "read-model:"+key, false, "read model was not written")
				continue
			}
			return nil, err
		}
		ok := model.ConfigVersionID == report.ConfigVersion.ID && strings.TrimSpace(model.PayloadJSON) != ""
		checks = appendProfileVerifyCheck(checks, "read-model:"+key, ok, "read model exists for published config version")
	}
	if options.RequireCaseRuns {
		caseRunChecks, err := verifyProfileAPICaseRuns(ctx, runtime, bundle)
		if err != nil {
			return nil, err
		}
		checks = append(checks, caseRunChecks...)
	}
	if options.RequireWorkflowRuns {
		workflowChecks, err := verifyProfileWorkflowRuns(ctx, runtime, bundle)
		if err != nil {
			return nil, err
		}
		checks = append(checks, workflowChecks...)
	}
	return checks, nil
}

func verifyProfileWorkflowRuns(ctx context.Context, runtime store.Store, bundle profile.Bundle) ([]profileVerifyCheck, error) {
	if len(bundle.Workflows) == 0 {
		return []profileVerifyCheck{{Name: "workflow-runs", OK: true, Detail: "profile declares no workflows"}}, nil
	}
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	latestByWorkflow := map[string]store.Run{}
	for _, item := range runs {
		if item.WorkflowID == "" {
			continue
		}
		current, ok := latestByWorkflow[item.WorkflowID]
		if !ok || item.CreatedAt.After(current.CreatedAt) || (item.CreatedAt.Equal(current.CreatedAt) && item.ID > current.ID) {
			latestByWorkflow[item.WorkflowID] = item
		}
	}
	checks := make([]profileVerifyCheck, 0, len(bundle.Workflows))
	for _, item := range bundle.Workflows {
		run, ok := latestByWorkflow[item.ID]
		if !ok || !isPassedStatus(run.Status) {
			checks = appendProfileVerifyCheck(checks, "workflow-run:"+item.ID, false, "no passed run recorded in Store")
			continue
		}
		checks = appendProfileVerifyCheck(checks, "workflow-run:"+item.ID, true, "latest Workflow run passed")
	}
	return checks, nil
}

func verifyProfileAPICaseRuns(ctx context.Context, runtime store.Store, bundle profile.Bundle) ([]profileVerifyCheck, error) {
	if len(bundle.APICases) == 0 {
		return []profileVerifyCheck{{Name: "api-case-runs", OK: true, Detail: "profile declares no API cases"}}, nil
	}
	latestStore, ok := runtime.(interface {
		ListLatestAPICaseRuns(context.Context) ([]store.APICaseRun, error)
	})
	if !ok {
		return nil, errors.New("runtime store does not support latest API case run lookup")
	}
	latestRuns, err := latestStore.ListLatestAPICaseRuns(ctx)
	if err != nil {
		return nil, err
	}
	latestByCase := map[string]store.APICaseRun{}
	for _, item := range latestRuns {
		latestByCase[item.CaseID] = item
	}
	checks := make([]profileVerifyCheck, 0, len(bundle.APICases))
	for _, item := range bundle.APICases {
		run, ok := latestByCase[item.ID]
		if !ok || !isPassedStatus(run.Status) {
			checks = appendProfileVerifyCheck(checks, "api-case-run:"+item.ID, false, "no passed run recorded in Store")
			continue
		}
		checks = appendProfileVerifyCheck(checks, "api-case-run:"+item.ID, true, "latest API case run passed")
	}
	return checks, nil
}

func isPassedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass", "passed", "success", "ok":
		return true
	default:
		return false
	}
}

func appendProfileVerifyCheck(checks []profileVerifyCheck, name string, ok bool, detail string) []profileVerifyCheck {
	return append(checks, profileVerifyCheck{Name: name, OK: ok, Detail: detail})
}

func profileVerifyChecksOK(checks []profileVerifyCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}

func firstFailedProfileVerifyCheck(checks []profileVerifyCheck) string {
	for _, check := range checks {
		if !check.OK {
			return check.Name + ": " + check.Detail
		}
	}
	return "no checks passed"
}

func isProfileRequestError(err error) bool {
	message := err.Error()
	return strings.HasPrefix(message, "load profile") ||
		strings.HasPrefix(message, "digest profile") ||
		strings.HasPrefix(message, "audit profile") ||
		strings.HasPrefix(message, "profile audit failed")
}

func profileImportCountsFrom(counts profile.Counts) profileImportCounts {
	return profileImportCounts{
		Services:         counts.Services,
		Workflows:        counts.Workflows,
		InterfaceNodes:   counts.InterfaceNodes,
		APICases:         counts.APICases,
		RequestTemplates: counts.RequestTemplates,
		CaseDependencies: counts.CaseDependencies,
		WorkflowBindings: counts.WorkflowBindings,
		Fixtures:         counts.Fixtures,
	}
}

func profileImportConfigVersionID(profileID string, publishedAt time.Time) string {
	safeProfileID := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(strings.TrimSpace(profileID))
	if safeProfileID == "" {
		safeProfileID = "profile"
	}
	return "config." + safeProfileID + "." + publishedAt.UTC().Format("20060102T150405.000000000Z")
}

func profileImportConfigVersionFromStore(item store.ConfigVersion) profileImportConfigVersion {
	return profileImportConfigVersion{
		ID:                    item.ID,
		TemplatePackageID:     item.ProfileID,
		TemplatePackagePath:   item.SourcePath,
		TemplatePackageDigest: item.BundleDigest,
		ProfileID:             item.ProfileID,
		SourcePath:            item.SourcePath,
		BundleDigest:          item.BundleDigest,
		Active:                item.Active,
		PublishedAt:           item.PublishedAt,
		CreatedAt:             item.CreatedAt,
	}
}
