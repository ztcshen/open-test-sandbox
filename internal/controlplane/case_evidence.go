package controlplane

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"

	"open-test-sandbox/internal/store"
)

func handleCaseEvidence(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	if runtime == nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	runID := strings.TrimSpace(r.URL.Query().Get("runId"))
	if runID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "runId is required"})
		return
	}
	run, err := runtime.GetRun(r.Context(), runID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	caseRuns, err := runtime.ListAPICaseRuns(r.Context(), run.ID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(caseRuns) == 0 {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	selected, ok := selectCaseEvidenceRun(caseRuns, r.URL.Query().Get("caseId"), r.URL.Query().Get("stepId"), run.SummaryJSON)
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "case evidence not found"})
		return
	}
	records, err := runtime.ListEvidence(r.Context(), run.ID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	catalog, catalogErr := runtime.GetProfileCatalog(r.Context())
	if catalogErr != nil {
		catalog = store.ProfileCatalog{}
	}
	topologies, err := runtime.ListTraceTopologies(r.Context(), run.ID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, caseEvidencePayload(run, selected, caseRuns, records, catalog, topologies))
}

func selectCaseEvidenceRun(caseRuns []store.APICaseRun, caseID string, stepID string, summaryJSON string) (store.APICaseRun, bool) {
	caseID = strings.TrimSpace(caseID)
	stepID = strings.TrimSpace(stepID)
	if caseID == "" && stepID != "" {
		caseID = caseIDForWorkflowStep(summaryJSON, stepID)
	}
	if caseID == "" {
		return caseRuns[0], true
	}
	for _, item := range caseRuns {
		if item.CaseID == caseID {
			return item, true
		}
	}
	return store.APICaseRun{}, false
}

func caseIDForWorkflowStep(summaryJSON string, stepID string) string {
	summary := jsonObject(summaryJSON)
	for _, raw := range workflowRunSteps(summary) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(valueString(step["stepId"])) == stepID {
			return strings.TrimSpace(valueString(step["caseId"]))
		}
	}
	return ""
}

func caseEvidencePayload(run store.Run, item store.APICaseRun, caseRuns []store.APICaseRun, records []store.EvidenceRecord, catalog store.ProfileCatalog, topologies []store.TraceTopology) map[string]any {
	request := caseEvidenceRequest(records, item.ID, jsonObject(item.RequestSummaryJSON))
	assertions := jsonObject(item.AssertionSummaryJSON)
	response := caseEvidenceResponse(records, item.ID)
	saved := jsonObject(run.SummaryJSON)
	trace := mapFromAny(saved["trace"])
	fixture := caseEvidenceSeedData(item.CaseID, catalog, run, caseRuns)
	topology := topologyEvidenceViewForCase(topologyEvidenceViewInput{
		RunID:        run.ID,
		CaseID:       item.CaseID,
		SavedSummary: saved,
		Rows:         topologies,
	})
	operation := caseRunOperation(request, item.CaseID)
	summary := map[string]any{
		"case_id":       item.CaseID,
		"run_id":        run.ID,
		"operation":     operation,
		"evidence_path": run.EvidenceRoot,
		"status":        item.Status,
	}
	if code, ok := response["http_code"]; ok {
		summary["actual_http_code"] = code
	}
	if reason := caseRunFailureReason(assertions); reason != "" {
		summary["failure_reason"] = reason
	}
	assertions["passed"] = strings.EqualFold(valueString(assertions["status"]), store.StatusPassed)
	return map[string]any{
		"ok": true,
		"evidence": map[string]any{
			"summary":    summary,
			"trace":      trace,
			"request":    request,
			"response":   response,
			"assertions": assertions,
			"services":   []map[string]any{},
			"logs":       []map[string]any{},
			"mysql":      map[string]any{"ok": true, "queries": []map[string]any{}},
			"fixture":    fixture,
			"topology":   topology,
		},
	}
}

func caseEvidenceSeedData(caseID string, catalog store.ProfileCatalog, run store.Run, caseRuns []store.APICaseRun) map[string]any {
	dependencies := caseDependencyPayloads(caseID, catalog)
	if len(dependencies) == 0 {
		return emptyFixtureEvidencePayload()
	}
	upstreamSteps := workflowStepPayloads(caseWorkflowPrefix(caseID, catalog))
	if len(upstreamSteps) > 0 {
		upstreamSteps = upstreamSteps[:len(upstreamSteps)-1]
	}
	applyRuns := preconditionApplyRuns(run, upstreamSteps, caseRuns)
	return map[string]any{
		"status":        "configured",
		"applyRuns":     applyRuns,
		"dependencies":  dependencies,
		"upstreamSteps": upstreamSteps,
		"summary": map[string]any{
			"applyCount":      len(applyRuns),
			"restoreCount":    0,
			"failedCount":     failedPreconditionApplyRuns(applyRuns),
			"dependencyCount": len(dependencies),
			"upstreamCount":   len(upstreamSteps),
		},
	}
}

func preconditionApplyRuns(run store.Run, upstreamSteps []map[string]any, caseRuns []store.APICaseRun) []map[string]any {
	caseRunsByCase := make(map[string]store.APICaseRun, len(caseRuns))
	for _, item := range caseRuns {
		if item.CaseID != "" {
			caseRunsByCase[item.CaseID] = item
		}
	}
	caseIDByStep := workflowRunCaseIDsByStep(run.SummaryJSON)
	out := []map[string]any{}
	for _, step := range upstreamSteps {
		caseID := strings.TrimSpace(valueString(step["caseId"]))
		if caseID == "" {
			continue
		}
		item, ok := caseRunsByCase[caseID]
		if !ok {
			if runtimeCaseID := caseIDByStep[strings.TrimSpace(valueString(step["stepId"]))]; runtimeCaseID != "" {
				item, ok = caseRunsByCase[runtimeCaseID]
			}
		}
		if !ok {
			continue
		}
		status := "applied"
		if !strings.EqualFold(item.Status, store.StatusPassed) {
			status = "failed"
		}
		out = append(out, map[string]any{
			"id":                item.ID,
			"runId":             run.ID,
			"workflowId":        firstNonEmpty(valueString(step["workflowId"]), run.WorkflowID),
			"stepId":            valueString(step["stepId"]),
			"caseId":            item.CaseID,
			"status":            status,
			"caseStatus":        item.Status,
			"request":           jsonObject(item.RequestSummaryJSON),
			"assertions":        jsonObject(item.AssertionSummaryJSON),
			"startedAt":         item.StartedAt,
			"finishedAt":        item.FinishedAt,
			"fixtureInstanceId": run.ID + ":" + item.CaseID,
		})
	}
	return out
}

func workflowRunCaseIDsByStep(summaryJSON string) map[string]string {
	summary := jsonObject(summaryJSON)
	out := map[string]string{}
	for _, raw := range workflowRunSteps(summary) {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		stepID := strings.TrimSpace(valueString(step["stepId"]))
		caseID := strings.TrimSpace(valueString(step["caseId"]))
		if stepID != "" && caseID != "" {
			out[stepID] = caseID
		}
	}
	return out
}

func failedPreconditionApplyRuns(items []map[string]any) int {
	count := 0
	for _, item := range items {
		if !strings.EqualFold(valueString(item["status"]), "applied") {
			count++
		}
	}
	return count
}

func caseDependencyPayloads(caseID string, catalog store.ProfileCatalog) []map[string]any {
	fixtures := make(map[string]store.CatalogFixture, len(catalog.Fixtures))
	for _, fixture := range catalog.Fixtures {
		fixtures[fixture.ID] = fixture
	}
	out := []map[string]any{}
	for _, dependency := range catalog.CaseDependencies {
		if dependency.CaseID != caseID || !activeCatalogStatus(dependency.Status) {
			continue
		}
		fixture := fixtures[dependency.FixtureID]
		item := map[string]any{
			"id":               dependency.ID,
			"caseId":           dependency.CaseID,
			"fixtureProfileId": dependency.FixtureID,
			"required":         dependency.Required,
			"mappings":         jsonArray(dependency.MappingsJSON),
			"mappingsJson":     dependency.MappingsJSON,
			"status":           dependency.Status,
			"sortOrder":        dependency.SortOrder,
		}
		if fixture.ID != "" {
			item["profile"] = map[string]any{
				"id":               fixture.ID,
				"name":             fixture.DisplayName,
				"sourceType":       fixture.Kind,
				"sourceWorkflowId": fixture.SourceWorkflowID,
				"sourceUntilStep":  fixture.SourceUntilStep,
				"ttlSeconds":       fixture.TTLSeconds,
				"status":           fixture.Status,
				"sortOrder":        fixture.SortOrder,
				"sourceSteps":      workflowStepPayloads(fixtureSourceSteps(fixture, catalog)),
			}
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return valueString(out[i]["sortOrder"]) < valueString(out[j]["sortOrder"])
	})
	return out
}

func caseWorkflowPrefix(caseID string, catalog store.ProfileCatalog) []store.CatalogWorkflowBinding {
	var out []store.CatalogWorkflowBinding
	for _, candidate := range catalog.WorkflowBindings {
		if candidate.CaseID != caseID {
			continue
		}
		workflow := sortedWorkflowBindings(candidate.WorkflowID, catalog)
		for _, step := range workflow {
			out = append(out, step)
			if step.StepID == candidate.StepID {
				break
			}
		}
	}
	return out
}

func fixtureSourceSteps(fixture store.CatalogFixture, catalog store.ProfileCatalog) []store.CatalogWorkflowBinding {
	workflow := sortedWorkflowBindings(fixture.SourceWorkflowID, catalog)
	if len(workflow) == 0 || strings.TrimSpace(fixture.SourceUntilStep) == "" {
		return workflow
	}
	out := []store.CatalogWorkflowBinding{}
	for _, step := range workflow {
		out = append(out, step)
		if step.StepID == fixture.SourceUntilStep {
			break
		}
	}
	return out
}

func sortedWorkflowBindings(workflowID string, catalog store.ProfileCatalog) []store.CatalogWorkflowBinding {
	if strings.TrimSpace(workflowID) == "" {
		return nil
	}
	out := []store.CatalogWorkflowBinding{}
	for _, step := range catalog.WorkflowBindings {
		if step.WorkflowID == workflowID {
			out = append(out, step)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder == out[j].SortOrder {
			return out[i].StepID < out[j].StepID
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out
}

func workflowStepPayloads(steps []store.CatalogWorkflowBinding) []map[string]any {
	out := make([]map[string]any, 0, len(steps))
	for index, step := range steps {
		out = append(out, map[string]any{
			"workflowId": step.WorkflowID,
			"stepId":     step.StepID,
			"nodeId":     step.NodeID,
			"caseId":     step.CaseID,
			"required":   step.Required,
			"sortOrder":  step.SortOrder,
			"index":      index + 1,
		})
	}
	return out
}

func mapFromAny(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case nil:
		return map[string]any{}
	default:
		return map[string]any{}
	}
}

func jsonArray(raw string) []any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []any{}
	}
	var out []any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []any{}
	}
	return out
}

func caseEvidenceRequest(records []store.EvidenceRecord, caseRunID string, fallback map[string]any) map[string]any {
	request := copyMap(fallback)
	for _, record := range records {
		if record.CaseRunID != caseRunID || record.Kind != "request" {
			continue
		}
		if body, ok := evidenceRecordObject(record); ok {
			for key, value := range body {
				request[key] = value
			}
		}
		request["evidence_uri"] = record.URI
		break
	}
	return request
}

func caseEvidenceResponse(records []store.EvidenceRecord, caseRunID string) map[string]any {
	response := map[string]any{}
	for _, record := range records {
		if record.CaseRunID != caseRunID || record.Kind != "response" {
			continue
		}
		summary := jsonObject(record.Summary)
		if code, ok := summary["statusCode"]; ok {
			response["http_code"] = code
		}
		if bytes, ok := summary["bodyBytes"]; ok {
			response["body_bytes"] = bytes
		}
		if body, ok := evidenceRecordObject(record); ok {
			if code, ok := body["statusCode"]; ok {
				response["http_code"] = code
			}
			if headers, ok := body["headers"]; ok {
				response["headers"] = headers
			}
			if responseBody, ok := body["body"]; ok {
				response["body"] = responseBody
			}
		}
		response["evidence_uri"] = record.URI
		break
	}
	return response
}

func evidenceRecordObject(record store.EvidenceRecord) (map[string]any, bool) {
	raw, err := os.ReadFile(record.URI)
	if err != nil {
		return nil, false
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, false
	}
	return body, true
}

func copyMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func emptyFixtureEvidencePayload() map[string]any {
	return map[string]any{
		"status":    "empty",
		"applyRuns": []map[string]any{},
		"summary": map[string]any{
			"applyCount":   0,
			"restoreCount": 0,
			"failedCount":  0,
		},
	}
}
