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

func handleWorkflowPlan(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	workflowID := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("workflowId"), r.URL.Query().Get("workflow")))
	if workflowID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "workflowId is required"})
		return
	}
	payload, ok, err := WorkflowPlanPayload(r.Context(), bundle, workflowID, runtime)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "workflow not found"})
		return
	}
	writeJSON(w, payload)
}

func WorkflowPlanPayload(ctx context.Context, bundle profile.Bundle, workflowID string, runtime store.Store) (map[string]any, bool, error) {
	workflowID = strings.TrimSpace(workflowID)
	if runtime != nil {
		catalog, err := runtime.GetProfileCatalog(ctx)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, false, err
		}
		if err == nil && len(catalog.Workflows) > 0 {
			return workflowPlanPayloadFromCatalog(catalog, workflowID)
		}
	}
	return workflowPlanPayloadFromBundle(bundle, workflowID)
}

func workflowPlanPayloadFromBundle(bundle profile.Bundle, workflowID string) (map[string]any, bool, error) {
	var workflow profile.Workflow
	found := false
	for _, item := range bundle.Workflows {
		if item.ID == workflowID {
			workflow = item
			found = true
			break
		}
	}
	if !found {
		return nil, false, nil
	}
	nodes := make(map[string]profile.InterfaceNode, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		nodes[node.ID] = node
	}
	cases := make(map[string]profile.APICase, len(bundle.APICases))
	for _, item := range bundle.APICases {
		cases[item.ID] = item
	}
	steps := make([]map[string]any, 0)
	for _, binding := range bundle.WorkflowBindings {
		if binding.WorkflowID != workflowID {
			continue
		}
		step := workflowPlanStep(binding.WorkflowID, binding.StepID, binding.NodeID, binding.CaseID, binding.Required, binding.SortOrder)
		if node, ok := nodes[binding.NodeID]; ok {
			step["node"] = map[string]any{"id": node.ID, "displayName": node.DisplayName, "serviceId": node.ServiceID, "method": node.Method, "path": node.Path}
		}
		if item, ok := cases[binding.CaseID]; ok {
			step["case"] = map[string]any{"id": item.ID, "displayName": item.DisplayName, "nodeId": item.NodeID, "status": item.Status}
		}
		steps = append(steps, step)
	}
	sortWorkflowPlanSteps(steps)
	return workflowPlanPayload(bundle.ID, workflowID, map[string]any{"id": workflow.ID, "displayName": workflow.DisplayName}, steps, "profile"), true, nil
}

func workflowPlanPayloadFromCatalog(catalog store.ProfileCatalog, workflowID string) (map[string]any, bool, error) {
	var workflow store.CatalogWorkflow
	found := false
	for _, item := range catalog.Workflows {
		if item.ID == workflowID {
			workflow = item
			found = true
			break
		}
	}
	if !found {
		return nil, false, nil
	}
	nodes := make(map[string]store.CatalogInterfaceNode, len(catalog.InterfaceNodes))
	for _, node := range catalog.InterfaceNodes {
		nodes[node.ID] = node
	}
	cases := make(map[string]store.CatalogAPICase, len(catalog.APICases))
	for _, item := range catalog.APICases {
		cases[item.ID] = item
	}
	steps := make([]map[string]any, 0)
	for _, binding := range catalog.WorkflowBindings {
		if binding.WorkflowID != workflowID {
			continue
		}
		step := workflowPlanStep(binding.WorkflowID, binding.StepID, binding.NodeID, binding.CaseID, binding.Required, binding.SortOrder)
		if node, ok := nodes[binding.NodeID]; ok {
			step["node"] = map[string]any{"id": node.ID, "displayName": node.DisplayName, "serviceId": node.ServiceID, "method": node.Method, "path": node.Path, "status": node.Status}
		}
		if item, ok := cases[binding.CaseID]; ok {
			step["case"] = map[string]any{"id": item.ID, "displayName": item.DisplayName, "nodeId": item.NodeID, "status": item.Status}
		}
		steps = append(steps, step)
	}
	sortWorkflowPlanSteps(steps)
	workflowValue := map[string]any{"id": workflow.ID, "displayName": workflow.DisplayName}
	return workflowPlanPayload(catalog.ProfileID, workflowID, workflowValue, steps, "store"), true, nil
}

func workflowPlanStep(workflowID string, stepID string, nodeID string, caseID string, required bool, sortOrder int) map[string]any {
	return map[string]any{
		"workflowId": workflowID,
		"stepId":     stepID,
		"nodeId":     nodeID,
		"caseId":     caseID,
		"required":   required,
		"sortOrder":  sortOrder,
	}
}

func workflowPlanPayload(profileID string, workflowID string, workflow map[string]any, steps []map[string]any, sourceKind string) map[string]any {
	required := 0
	for _, step := range steps {
		if value, _ := step["required"].(bool); value {
			required++
		}
	}
	return map[string]any{
		"ok":         true,
		"profileId":  profileID,
		"workflowId": workflowID,
		"workflow":   workflow,
		"steps":      steps,
		"counts": map[string]any{
			"steps":         len(steps),
			"requiredSteps": required,
		},
		"source": map[string]any{"kind": sourceKind},
	}
}

func sortWorkflowPlanSteps(steps []map[string]any) {
	sort.SliceStable(steps, func(i int, j int) bool {
		leftOrder := intValue(steps[i]["sortOrder"])
		rightOrder := intValue(steps[j]["sortOrder"])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return valueString(steps[i]["stepId"]) < valueString(steps[j]["stepId"])
	})
}
