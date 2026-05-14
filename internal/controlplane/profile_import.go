package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/profileaudit"
	"open-test-sandbox/internal/profilecatalog"
	"open-test-sandbox/internal/store"
)

type profileImportRequest struct {
	Path  string `json:"path"`
	Audit bool   `json:"audit"`
}

type profileImportResponse struct {
	ProfileID    string               `json:"profileId"`
	BundlePath   string               `json:"bundlePath"`
	BundleDigest string               `json:"bundleDigest"`
	ImportedAt   time.Time            `json:"importedAt"`
	Counts       profileImportCounts  `json:"counts"`
	Store        profileImportStore   `json:"store"`
	Audit        *profileaudit.Report `json:"audit,omitempty"`
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
	ProfileID    string    `json:"profileId"`
	BundlePath   string    `json:"bundlePath"`
	BundleDigest string    `json:"bundleDigest"`
	ImportedAt   time.Time `json:"importedAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func handleProfileImport(w http.ResponseWriter, r *http.Request, runtime store.Store, activate func(profile.Bundle)) {
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
	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "path is required"})
		return
	}
	bundle, report, err := importProfileBundle(r.Context(), runtime, req)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.HasPrefix(err.Error(), "load profile") || strings.HasPrefix(err.Error(), "digest profile") || strings.HasPrefix(err.Error(), "audit profile") {
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

func importProfileBundle(ctx context.Context, runtime store.Store, req profileImportRequest) (profile.Bundle, profileImportResponse, error) {
	bundle, err := profile.Load(req.Path)
	if err != nil {
		return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("load profile %q: %w", req.Path, err)
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
	if err := runtime.ReplaceProfileCatalog(ctx, profilecatalog.FromBundle(bundle, importedAt)); err != nil {
		return profile.Bundle{}, profileImportResponse{}, fmt.Errorf("store profile catalog %q: %w", bundle.ID, err)
	}
	response := profileImportResponse{
		ProfileID:    bundle.ID,
		BundlePath:   req.Path,
		BundleDigest: digest,
		ImportedAt:   importedAt,
		Counts:       profileImportCountsFrom(counts),
		Store: profileImportStore{
			ProfileID:    index.ProfileID,
			BundlePath:   index.BundlePath,
			BundleDigest: index.BundleDigest,
			ImportedAt:   index.ImportedAt,
			UpdatedAt:    index.UpdatedAt,
		},
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
