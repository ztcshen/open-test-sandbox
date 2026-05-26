package controlplane

import (
	"net/http"

	"agent-testbench/internal/domain/profile"
)

type profilePayload struct {
	TemplatePackageID string         `json:"templatePackageId"`
	ID                string         `json:"id"`
	DisplayName       string         `json:"displayName"`
	Counts            profile.Counts `json:"counts"`
}

type profileAssetsPayload struct {
	TemplatePackageID string                  `json:"templatePackageId"`
	Services          []profile.Service       `json:"services"`
	Workflows         []profile.Workflow      `json:"workflows"`
	InterfaceNodes    []profile.InterfaceNode `json:"interfaceNodes"`
	APICases          []profile.APICase       `json:"apiCases"`
}

func writeProfileSummary(w http.ResponseWriter, bundle profile.Bundle) {
	writeJSON(w, profilePayload{
		TemplatePackageID: bundle.ID,
		ID:                bundle.ID,
		DisplayName:       bundle.DisplayName,
		Counts:            bundle.Counts(),
	})
}

func writeProfileAssets(w http.ResponseWriter, bundle profile.Bundle) {
	writeJSON(w, profileAssetsPayload{
		TemplatePackageID: bundle.ID,
		Services:          nonNil(bundle.Services),
		Workflows:         nonNil(bundle.Workflows),
		InterfaceNodes:    nonNil(bundle.InterfaceNodes),
		APICases:          nonNil(bundle.APICases),
	})
}
