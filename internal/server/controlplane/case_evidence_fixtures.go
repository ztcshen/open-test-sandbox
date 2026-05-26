package controlplane

import (
	"encoding/json"
	"sort"
	"strings"

	"agent-testbench/internal/store"
)

const caseEvidenceFieldSortOrder = "sortOrder"

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
			"id":                          item.ID,
			"runId":                       run.ID,
			"workflowId":                  firstNonEmpty(valueString(step["workflowId"]), run.WorkflowID),
			"stepId":                      valueString(step["stepId"]),
			"caseId":                      item.CaseID,
			"status":                      status,
			"caseStatus":                  item.Status,
			"request":                     jsonObject(item.RequestSummaryJSON),
			apiCaseEvidenceKindAssertions: jsonObject(item.AssertionSummaryJSON),
			"startedAt":                   item.StartedAt,
			"finishedAt":                  item.FinishedAt,
			"fixtureInstanceId":           run.ID + ":" + item.CaseID,
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
			"id":                       dependency.ID,
			"caseId":                   dependency.CaseID,
			"fixtureProfileId":         dependency.FixtureID,
			"required":                 dependency.Required,
			"mappings":                 jsonArray(dependency.MappingsJSON),
			"mappingsJson":             dependency.MappingsJSON,
			"status":                   dependency.Status,
			caseEvidenceFieldSortOrder: dependency.SortOrder,
		}
		if fixture.ID != "" {
			item["profile"] = map[string]any{
				"id":                       fixture.ID,
				"name":                     fixture.DisplayName,
				"sourceType":               fixture.Kind,
				"sourceWorkflowId":         fixture.SourceWorkflowID,
				"sourceUntilStep":          fixture.SourceUntilStep,
				"ttlSeconds":               fixture.TTLSeconds,
				"status":                   fixture.Status,
				caseEvidenceFieldSortOrder: fixture.SortOrder,
				"sourceSteps":              workflowStepPayloads(fixtureSourceSteps(fixture, catalog)),
			}
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return valueString(out[i][caseEvidenceFieldSortOrder]) < valueString(out[j][caseEvidenceFieldSortOrder])
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
			"workflowId":               step.WorkflowID,
			"stepId":                   step.StepID,
			"nodeId":                   step.NodeID,
			"caseId":                   step.CaseID,
			"required":                 step.Required,
			caseEvidenceFieldSortOrder: step.SortOrder,
			"index":                    index + 1,
		})
	}
	return out
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
