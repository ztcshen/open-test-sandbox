package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

const (
	readModelInterfaceNodeCoveragePrefix     = "interface-node-coverage:"
	readModelInterfaceNodeCoverageGapsPrefix = "interface-node-coverage-gaps:"
)

func interfaceNodeCoveragePayloadFromBundle(bundle profile.Bundle, workflowID string) map[string]any {
	rows := interfaceNodeCoverageRows(bundle, workflowID)
	return interfaceNodeCoveragePayload(workflowID, map[string]string{"kind": "profile", "id": bundle.ID}, rows)
}

func interfaceNodeCoverageGapsPayloadFromBundle(bundle profile.Bundle, workflowID string) map[string]any {
	rows := interfaceNodeCoverageRows(bundle, workflowID)
	return interfaceNodeCoverageGapsPayload(workflowID, map[string]string{"kind": "profile", "id": bundle.ID}, rows)
}

func interfaceNodeCoveragePayloadFromBundleWithStore(ctx context.Context, bundle profile.Bundle, workflowID string, runtime store.Store) (map[string]any, error) {
	if runtime == nil {
		return interfaceNodeCoveragePayloadFromBundle(bundle, workflowID), nil
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		payload, ok, err := readInterfaceNodeCoverageModel(ctx, runtime, catalog.ProfileID, InterfaceNodeCoverageReadModelKey(workflowID))
		if err != nil {
			return nil, err
		}
		if ok {
			if err := hydrateInterfaceNodeCoveragePayload(ctx, catalog, runtime, payload); err != nil {
				return nil, err
			}
			return payload, nil
		}
		payload = interfaceNodeCoveragePayloadFromCatalog(catalog, workflowID)
		if err := hydrateInterfaceNodeCoveragePayload(ctx, catalog, runtime, payload); err != nil {
			return nil, err
		}
		return payload, nil
	}
	return interfaceNodeCoveragePayloadFromBundle(bundle, workflowID), nil
}

func InterfaceNodeCoveragePayload(ctx context.Context, bundle profile.Bundle, workflowID string, runtime store.Store) (map[string]any, error) {
	return interfaceNodeCoveragePayloadFromBundleWithStore(ctx, bundle, workflowID, runtime)
}

func interfaceNodeCoverageGapsPayloadFromBundleWithStore(ctx context.Context, bundle profile.Bundle, workflowID string, runtime store.Store) (map[string]any, error) {
	if runtime == nil {
		return interfaceNodeCoverageGapsPayloadFromBundle(bundle, workflowID), nil
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		payload, ok, err := readInterfaceNodeCoverageModel(ctx, runtime, catalog.ProfileID, InterfaceNodeCoverageGapsReadModelKey(workflowID))
		if err != nil {
			return nil, err
		}
		if ok {
			return payload, nil
		}
		return interfaceNodeCoverageGapsPayloadFromCatalog(catalog, workflowID), nil
	}
	return interfaceNodeCoverageGapsPayloadFromBundle(bundle, workflowID), nil
}

func InterfaceNodeCoverageGapsPayload(ctx context.Context, bundle profile.Bundle, workflowID string, runtime store.Store) (map[string]any, error) {
	return interfaceNodeCoverageGapsPayloadFromBundleWithStore(ctx, bundle, workflowID, runtime)
}

func InterfaceNodeCoverageReadModelKey(workflowID string) string {
	return readModelInterfaceNodeCoveragePrefix + workflowID
}

func InterfaceNodeCoverageGapsReadModelKey(workflowID string) string {
	return readModelInterfaceNodeCoverageGapsPrefix + workflowID
}

func InterfaceNodeCoverageReadModels(catalog store.ProfileCatalog, configVersionID string, generatedAt time.Time) ([]store.ReadModel, error) {
	workflowIDs := interfaceNodeCoverageWorkflowIDs(catalog)
	models := make([]store.ReadModel, 0, len(workflowIDs)*2)
	for _, workflowID := range workflowIDs {
		rows := interfaceNodeCoverageRowsFromCatalog(catalog, workflowID)
		payloads := []struct {
			key     string
			payload map[string]any
		}{
			{
				key:     InterfaceNodeCoverageReadModelKey(workflowID),
				payload: interfaceNodeCoveragePayload(workflowID, map[string]string{"kind": "read-model", "id": catalog.ProfileID}, rows),
			},
			{
				key:     InterfaceNodeCoverageGapsReadModelKey(workflowID),
				payload: interfaceNodeCoverageGapsPayload(workflowID, map[string]string{"kind": "read-model", "id": catalog.ProfileID}, rows),
			},
		}
		for _, item := range payloads {
			raw, err := json.Marshal(item.payload)
			if err != nil {
				return nil, err
			}
			models = append(models, store.ReadModel{
				ProfileID:       catalog.ProfileID,
				Key:             item.key,
				ConfigVersionID: configVersionID,
				PayloadJSON:     string(raw),
				GeneratedAt:     generatedAt,
				UpdatedAt:       generatedAt,
			})
		}
	}
	return models, nil
}

func readInterfaceNodeCoverageModel(ctx context.Context, runtime store.Store, profileID string, key string) (map[string]any, bool, error) {
	model, err := runtime.GetReadModel(ctx, profileID, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		return nil, false, err
	}
	payload["source"] = map[string]string{"kind": "read-model", "id": profileID}
	return payload, true, nil
}

func interfaceNodeCoveragePayloadFromCatalog(catalog store.ProfileCatalog, workflowID string) map[string]any {
	rows := interfaceNodeCoverageRowsFromCatalog(catalog, workflowID)
	return interfaceNodeCoveragePayload(workflowID, map[string]string{"kind": "store", "id": catalog.ProfileID}, rows)
}

func interfaceNodeCoverageGapsPayloadFromCatalog(catalog store.ProfileCatalog, workflowID string) map[string]any {
	rows := interfaceNodeCoverageRowsFromCatalog(catalog, workflowID)
	return interfaceNodeCoverageGapsPayload(workflowID, map[string]string{"kind": "store", "id": catalog.ProfileID}, rows)
}

func interfaceNodeCoveragePayload(workflowID string, source map[string]string, rows []map[string]any) map[string]any {
	return map[string]any{
		"ok":         true,
		"templateId": "TPL-INTERFACE-NODE-COVERAGE-V1",
		"workflowId": workflowID,
		"summary":    interfaceNodeCoverageSummary(rows),
		"rows":       rows,
		"source":     source,
	}
}

func hydrateInterfaceNodeCoveragePayload(ctx context.Context, catalog store.ProfileCatalog, runtime store.Store, payload map[string]any) error {
	latest, err := latestCaseStates(ctx, runtime)
	if err != nil {
		return err
	}
	stateByNode := interfaceNodeCoverageAdmissionByNode(catalog, latest)
	rows := interfaceNodeCoveragePayloadRows(payload)
	for _, row := range rows {
		nodeID := valueString(row["nodeId"])
		state, ok := stateByNode[nodeID]
		if !ok {
			continue
		}
		row["admissionStatus"] = state.Status
		row["requiredCaseCount"] = state.Required
		row["passedCaseCount"] = state.Passed
		if state.LatestRunID != "" {
			row["latestRunId"] = state.LatestRunID
		}
	}
	payload["rows"] = rows
	payload["summary"] = interfaceNodeCoverageSummary(rows)
	return nil
}

type interfaceNodeCoverageAdmission struct {
	Status      string
	Required    int
	Passed      int
	LatestRunID string
}

func interfaceNodeCoverageAdmissionByNode(catalog store.ProfileCatalog, latest map[string]latestCaseState) map[string]interfaceNodeCoverageAdmission {
	casesByNode := map[string][]store.CatalogAPICase{}
	for _, item := range catalog.APICases {
		if item.NodeID == "" || !activeCatalogStatus(item.Status) {
			continue
		}
		casesByNode[item.NodeID] = append(casesByNode[item.NodeID], item)
	}
	out := map[string]interfaceNodeCoverageAdmission{}
	for nodeID, cases := range casesByNode {
		required, passed, failed, missing := 0, 0, 0, 0
		latestRunID := ""
		for _, item := range cases {
			state := latest[item.ID]
			if latestRunID == "" {
				latestRunID = state.RunID
			}
			if !item.RequiredForAdmission {
				continue
			}
			required++
			switch state.Status {
			case store.StatusPassed:
				passed++
			case store.StatusFailed:
				failed++
			default:
				missing++
			}
		}
		status := "pending"
		if required > 0 && passed == required {
			status = store.StatusPassed
		} else if failed > 0 {
			status = store.StatusFailed
		} else if missing == 0 && required == 0 {
			status = "pending"
		}
		out[nodeID] = interfaceNodeCoverageAdmission{
			Status:      status,
			Required:    required,
			Passed:      passed,
			LatestRunID: latestRunID,
		}
	}
	return out
}

func interfaceNodeCoveragePayloadRows(payload map[string]any) []map[string]any {
	switch rows := payload["rows"].(type) {
	case []map[string]any:
		return rows
	case []any:
		out := make([]map[string]any, 0, len(rows))
		for _, item := range rows {
			if row, ok := item.(map[string]any); ok {
				out = append(out, row)
			}
		}
		return out
	default:
		return []map[string]any{}
	}
}

func interfaceNodeCoverageGapsPayload(workflowID string, source map[string]string, rows []map[string]any) map[string]any {
	gaps := make([]map[string]any, 0)
	for _, row := range rows {
		if mapped, _ := row["mapped"].(bool); !mapped {
			gaps = append(gaps, row)
		}
	}
	return map[string]any{
		"ok":         true,
		"templateId": "TPL-INTERFACE-NODE-COVERAGE-GAP-V1",
		"workflowId": workflowID,
		"summary": map[string]any{
			"totalSteps": len(rows),
			"gapCount":   len(gaps),
		},
		"gaps":   gaps,
		"source": source,
	}
}

func interfaceNodeCoverageRows(bundle profile.Bundle, workflowID string) []map[string]any {
	nodeByID := make(map[string]profile.InterfaceNode, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		nodeByID[node.ID] = node
	}
	caseByID := make(map[string]profile.APICase, len(bundle.APICases))
	for _, item := range bundle.APICases {
		caseByID[item.ID] = item
	}

	rows := make([]map[string]any, 0)
	for _, binding := range bundle.WorkflowBindings {
		if workflowID != "" && binding.WorkflowID != workflowID {
			continue
		}
		node, mapped := nodeByID[binding.NodeID]
		item := caseByID[binding.CaseID]
		row := map[string]any{
			"workflowId":      binding.WorkflowID,
			"stepId":          binding.StepID,
			"nodeId":          binding.NodeID,
			"caseId":          binding.CaseID,
			"caseDisplayName": item.DisplayName,
			"required":        binding.Required,
			"mapped":          mapped,
			"admissionStatus": "pending",
		}
		if mapped {
			row["nodeDisplayName"] = node.DisplayName
			row["serviceId"] = node.ServiceID
			row["href"] = "/interface-node.html?id=" + node.ID
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i int, j int) bool {
		left := valueString(rows[i]["workflowId"]) + "\x00" + valueString(rows[i]["stepId"])
		right := valueString(rows[j]["workflowId"]) + "\x00" + valueString(rows[j]["stepId"])
		return left < right
	})
	return rows
}

func interfaceNodeCoverageRowsFromCatalog(catalog store.ProfileCatalog, workflowID string) []map[string]any {
	nodeByID := make(map[string]store.CatalogInterfaceNode, len(catalog.InterfaceNodes))
	for _, node := range catalog.InterfaceNodes {
		nodeByID[node.ID] = node
	}
	caseByID := make(map[string]store.CatalogAPICase, len(catalog.APICases))
	for _, item := range catalog.APICases {
		caseByID[item.ID] = item
	}

	rows := make([]map[string]any, 0)
	for _, binding := range catalog.WorkflowBindings {
		if workflowID != "" && binding.WorkflowID != workflowID {
			continue
		}
		node, mapped := nodeByID[binding.NodeID]
		item := caseByID[binding.CaseID]
		row := map[string]any{
			"workflowId":      binding.WorkflowID,
			"stepId":          binding.StepID,
			"nodeId":          binding.NodeID,
			"caseId":          binding.CaseID,
			"caseDisplayName": item.DisplayName,
			"required":        binding.Required,
			"mapped":          mapped,
			"admissionStatus": "pending",
		}
		if mapped {
			row["nodeDisplayName"] = node.DisplayName
			row["serviceId"] = node.ServiceID
			row["href"] = "/interface-node.html?id=" + node.ID
		}
		rows = append(rows, row)
	}
	sortInterfaceNodeCoverageRows(rows)
	return rows
}

func interfaceNodeCoverageWorkflowIDs(catalog store.ProfileCatalog) []string {
	seen := map[string]bool{}
	for _, workflow := range catalog.Workflows {
		if workflow.ID != "" {
			seen[workflow.ID] = true
		}
	}
	for _, binding := range catalog.WorkflowBindings {
		if binding.WorkflowID != "" {
			seen[binding.WorkflowID] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortInterfaceNodeCoverageRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i int, j int) bool {
		left := valueString(rows[i]["workflowId"]) + "\x00" + valueString(rows[i]["stepId"])
		right := valueString(rows[j]["workflowId"]) + "\x00" + valueString(rows[j]["stepId"])
		return left < right
	})
}

func interfaceNodeCoverageSummary(rows []map[string]any) map[string]any {
	mapped := 0
	passed, failed, pending := 0, 0, 0
	for _, row := range rows {
		if ok, _ := row["mapped"].(bool); ok {
			mapped++
		}
		switch valueString(row["admissionStatus"]) {
		case store.StatusPassed:
			passed++
		case store.StatusFailed:
			failed++
		default:
			pending++
		}
	}
	return map[string]any{
		"totalSteps":    len(rows),
		"mappedSteps":   mapped,
		"unmappedSteps": len(rows) - mapped,
		"passedNodes":   passed,
		"failedNodes":   failed,
		"pendingNodes":  pending,
	}
}
