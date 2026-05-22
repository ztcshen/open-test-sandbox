package controlplane

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/runner/requesttemplate"
)

type TemplateRenderRequest struct {
	TemplateID string `json:"templateId"`
	FixtureID  string `json:"fixtureId"`
}

type TemplateRenderPayload struct {
	OK                bool                    `json:"ok"`
	TemplatePackageID string                  `json:"templatePackageId"`
	Request           requesttemplate.Request `json:"request"`
}

func TemplateRenderPayloadFor(bundle profile.Bundle, req TemplateRenderRequest) (TemplateRenderPayload, error) {
	rendered, err := requesttemplate.Render(bundle, requesttemplate.Options{
		TemplateID: strings.TrimSpace(req.TemplateID),
		FixtureID:  strings.TrimSpace(req.FixtureID),
	})
	if err != nil {
		return TemplateRenderPayload{}, err
	}
	return TemplateRenderPayload{
		OK:                true,
		TemplatePackageID: bundle.ID,
		Request:           rendered,
	}, nil
}

func handleTemplateRender(w http.ResponseWriter, r *http.Request, bundle profile.Bundle) {
	var req TemplateRenderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	payload, err := TemplateRenderPayloadFor(bundle, req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, requesttemplate.ErrTemplateNotFound) || errors.Is(err, requesttemplate.ErrFixtureNotFound) {
			status = http.StatusNotFound
		}
		writeJSONStatus(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, payload)
}
