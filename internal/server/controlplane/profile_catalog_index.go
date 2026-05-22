package controlplane

import (
	"errors"
	"net/http"
	"time"

	"open-test-sandbox/internal/store"
)

type profileCatalogIndexResponse struct {
	ProfileID     string                        `json:"profileId"`
	IndexedAt     time.Time                     `json:"indexedAt"`
	Counts        profileCatalogIndexCounts     `json:"counts"`
	ConfigVersion *profileConfigVersionResponse `json:"configVersion,omitempty"`
}

type profileCatalogIndexCounts struct {
	Services         int `json:"services"`
	Workflows        int `json:"workflows"`
	InterfaceNodes   int `json:"interfaceNodes"`
	APICases         int `json:"apiCases"`
	RequestTemplates int `json:"requestTemplates"`
	WorkflowBindings int `json:"workflowBindings"`
	CaseDependencies int `json:"caseDependencies"`
	Fixtures         int `json:"fixtures"`
	Templates        int `json:"templates"`
	TemplateConfigs  int `json:"templateConfigs"`
}

type profileConfigVersionResponse struct {
	ID           string    `json:"id"`
	ProfileID    string    `json:"profileId"`
	SourcePath   string    `json:"sourcePath"`
	BundleDigest string    `json:"bundleDigest"`
	Active       bool      `json:"active"`
	PublishedAt  time.Time `json:"publishedAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

func handleProfileCatalogIndex(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "runtime store is not configured"})
		return
	}
	index, err := runtime.GetProfileCatalogIndex(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "profile catalog index not found"})
			return
		}
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	response := profileCatalogIndexResponse{
		ProfileID: index.ProfileID,
		IndexedAt: index.IndexedAt,
		Counts: profileCatalogIndexCounts{
			Services:         index.Counts.Services,
			Workflows:        index.Counts.Workflows,
			InterfaceNodes:   index.Counts.InterfaceNodes,
			APICases:         index.Counts.APICases,
			RequestTemplates: index.Counts.RequestTemplates,
			WorkflowBindings: index.Counts.WorkflowBindings,
			CaseDependencies: index.Counts.CaseDependencies,
			Fixtures:         index.Counts.Fixtures,
			Templates:        index.Counts.Templates,
			TemplateConfigs:  index.Counts.TemplateConfigs,
		},
	}
	if version, err := runtime.GetActiveConfigVersion(r.Context()); err == nil {
		response.ConfigVersion = &profileConfigVersionResponse{
			ID:           version.ID,
			ProfileID:    version.ProfileID,
			SourcePath:   version.SourcePath,
			BundleDigest: version.BundleDigest,
			Active:       version.Active,
			PublishedAt:  version.PublishedAt,
			CreatedAt:    version.CreatedAt,
		}
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, response)
}
