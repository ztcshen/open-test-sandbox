package controlplane

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

func handleWorkflowDiscovery(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	payload, err := WorkflowDiscoveryPayload(r.Context(), bundle, r.URL.Query().Get("filter"), runtime)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, payload)
}

func WorkflowDiscoveryPayload(ctx context.Context, bundle profile.Bundle, filter string, runtime store.Store) (map[string]any, error) {
	if runtime != nil {
		catalog, err := runtime.GetProfileCatalog(ctx)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if err == nil && len(catalog.Workflows) > 0 {
			return workflowDiscoveryPayloadFromCatalog(catalog, filter), nil
		}
	}
	return workflowDiscoveryPayloadFromBundle(bundle, filter), nil
}

func workflowDiscoveryPayloadFromBundle(bundle profile.Bundle, filter string) map[string]any {
	stepCounts := make(map[string]int, len(bundle.WorkflowBindings))
	for _, binding := range bundle.WorkflowBindings {
		if strings.TrimSpace(binding.WorkflowID) != "" {
			stepCounts[binding.WorkflowID]++
		}
	}
	workflows := append([]profile.Workflow(nil), bundle.Workflows...)
	sort.SliceStable(workflows, func(i, j int) bool {
		return workflows[i].ID < workflows[j].ID
	})
	items := make([]map[string]any, 0, len(workflows))
	for _, workflow := range workflows {
		if !matchesControlplaneDiscoveryFilter(filter, workflow.ID, workflow.DisplayName, workflow.Description) {
			continue
		}
		items = append(items, map[string]any{
			"id":          workflow.ID,
			"displayName": workflow.DisplayName,
			"description": workflow.Description,
			"stepCount":   stepCounts[workflow.ID],
		})
	}
	return workflowDiscoveryPayload(bundle.ID, items, "profile")
}

func workflowDiscoveryPayloadFromCatalog(catalog store.ProfileCatalog, filter string) map[string]any {
	stepCounts := make(map[string]int, len(catalog.WorkflowBindings))
	for _, binding := range catalog.WorkflowBindings {
		if strings.TrimSpace(binding.WorkflowID) != "" {
			stepCounts[binding.WorkflowID]++
		}
	}
	workflows := append([]store.CatalogWorkflow(nil), catalog.Workflows...)
	sort.SliceStable(workflows, func(i, j int) bool {
		return workflows[i].ID < workflows[j].ID
	})
	items := make([]map[string]any, 0, len(workflows))
	for _, workflow := range workflows {
		if !matchesControlplaneDiscoveryFilter(filter, workflow.ID, workflow.DisplayName, workflow.Description) {
			continue
		}
		items = append(items, map[string]any{
			"id":          workflow.ID,
			"displayName": workflow.DisplayName,
			"description": workflow.Description,
			"stepCount":   stepCounts[workflow.ID],
		})
	}
	return workflowDiscoveryPayload(catalog.ProfileID, items, "store")
}

func workflowDiscoveryPayload(profileID string, items []map[string]any, sourceKind string) map[string]any {
	return map[string]any{
		"ok":        true,
		"profileId": profileID,
		"count":     len(items),
		"items":     items,
		"source":    map[string]any{"kind": sourceKind},
	}
}

func matchesControlplaneDiscoveryFilter(filter string, values ...string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), filter) {
			return true
		}
	}
	return false
}
