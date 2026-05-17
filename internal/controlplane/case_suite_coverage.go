package controlplane

import (
	"net/http"
	"strconv"

	"open-test-sandbox/internal/casesuite"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func handleCaseSuiteCoverage(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	filter := caseSuiteCoverageFilterFromRequest(r)
	items := casesuite.SelectCases(bundle, filter)
	report, err := casesuite.Coverage(r.Context(), bundle, runtime, filter, items)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func handleCaseSuiteInspection(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	filter := caseSuiteCoverageFilterFromRequest(r)
	items := casesuite.SelectCases(bundle, filter)
	report, err := casesuite.Inspect(r.Context(), bundle, runtime, filter, items)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func handleCaseSuitePlan(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	filter := caseSuiteCoverageFilterFromRequest(r)
	items := casesuite.SelectCases(bundle, filter)
	report, err := casesuite.Plan(r.Context(), bundle, runtime, filter, items, caseSuitePlanOptionsFromRequest(r))
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func handleCaseSuiteStability(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	filter := caseSuiteCoverageFilterFromRequest(r)
	items := casesuite.SelectCases(bundle, filter)
	report, err := casesuite.Stability(r.Context(), bundle, runtime, filter, items, caseSuiteStabilityOptionsFromRequest(r))
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func handleCaseSuitePriority(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	filter := caseSuiteCoverageFilterFromRequest(r)
	items := casesuite.SelectCases(bundle, filter)
	report, err := casesuite.Priority(r.Context(), bundle, runtime, filter, items, caseSuitePriorityOptionsFromRequest(r))
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func handleCaseSuiteBrief(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	filter := caseSuiteCoverageFilterFromRequest(r)
	items := casesuite.SelectCases(bundle, filter)
	report, err := casesuite.Brief(r.Context(), bundle, runtime, filter, items, caseSuiteBriefOptionsFromRequest(r))
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func handleCaseSuiteQuality(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	filter := caseSuiteCoverageFilterFromRequest(r)
	items := casesuite.SelectCases(bundle, filter)
	report, err := casesuite.Quality(r.Context(), bundle, runtime, filter, items)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

func handleCaseSuiteImpact(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	filter := caseSuiteCoverageFilterFromRequest(r)
	report, err := casesuite.Impact(r.Context(), bundle, runtime, filter, caseSuiteImpactOptionsFromRequest(r))
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, report)
}

type caseSuiteImpactRunResponse struct {
	OK         bool                   `json:"ok"`
	Impact     casesuite.ImpactReport `json:"impact"`
	BatchRun   apiCaseBatchRunReport  `json:"batchRun"`
	BatchRunID string                 `json:"batchRunId"`
	ReportURL  string                 `json:"reportUrl"`
	Status     string                 `json:"status"`
}

func handleCaseSuiteImpactRun(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store, runner *apiCaseBatchRunner) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	filter := caseSuiteCoverageFilterFromPayload(payload)
	options := caseSuiteImpactOptionsFromPayload(payload)
	impact, err := casesuite.Impact(r.Context(), bundle, runtime, filter, options)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(impact.BatchRequest.CaseIDs) == 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "no ready impacted cases selected for execution", "impact": impact})
		return
	}
	batchRequest := apiCaseBatchRunRequest{
		RequestID:      impact.BatchRequest.RequestID,
		CaseIDs:        impact.BatchRequest.CaseIDs,
		BaseURL:        impact.BatchRequest.BaseURL,
		EvidenceDir:    impact.BatchRequest.EvidenceDir,
		TimeoutSeconds: impact.BatchRequest.TimeoutSeconds,
		Overrides:      mapValue(payload["overrides"]),
	}
	report, status, err := startAPICaseBatchRun(r.Context(), bundle, runtime, runner, batchRequest)
	if err != nil {
		writeJSONStatus(w, status, map[string]any{"ok": false, "error": err.Error(), "impact": impact})
		return
	}
	writeJSONStatus(w, http.StatusAccepted, caseSuiteImpactRunResponse{
		OK:         true,
		Impact:     impact,
		BatchRun:   report,
		BatchRunID: report.BatchRunID,
		ReportURL:  report.ReportURL,
		Status:     report.Status,
	})
}

func caseSuiteCoverageFilterFromRequest(r *http.Request) casesuite.Filter {
	query := r.URL.Query()
	return casesuite.NormalizeFilter(casesuite.Filter{
		Filter:   query.Get("filter"),
		NodeID:   firstNonEmpty(query.Get("node"), query.Get("nodeId")),
		Tags:     queryStringList(query["tag"], query["tags"]),
		Status:   firstNonEmpty(query.Get("status"), "active"),
		Owner:    query.Get("owner"),
		Priority: query.Get("priority"),
	})
}

func caseSuiteCoverageFilterFromPayload(payload map[string]any) casesuite.Filter {
	return casesuite.NormalizeFilter(casesuite.Filter{
		Filter:   valueString(payload["filter"]),
		NodeID:   firstNonEmpty(valueString(payload["node"]), valueString(payload["nodeId"])),
		Tags:     stringListValue(firstNonNil(payload["tag"], payload["tags"])),
		Status:   firstNonEmpty(valueString(payload["status"]), "active"),
		Owner:    valueString(payload["owner"]),
		Priority: valueString(payload["priority"]),
	})
}

func caseSuitePlanOptionsFromRequest(r *http.Request) casesuite.PlanOptions {
	query := r.URL.Query()
	return casesuite.PlanOptions{
		RequestID:      query.Get("requestId"),
		Actions:        queryStringList(query["action"], query["actions"]),
		BaseURL:        query.Get("baseUrl"),
		EvidenceDir:    query.Get("evidenceDir"),
		TimeoutSeconds: queryIntValue(query.Get("timeoutSeconds")),
	}
}

func caseSuiteStabilityOptionsFromRequest(r *http.Request) casesuite.StabilityOptions {
	return casesuite.StabilityOptions{Limit: queryIntValue(r.URL.Query().Get("limit"))}
}

func caseSuitePriorityOptionsFromRequest(r *http.Request) casesuite.PriorityOptions {
	query := r.URL.Query()
	return casesuite.PriorityOptions{
		Signals:        queryStringList(query["signal"], query["signals"], query["change"], query["changes"], query["changedPath"], query["changedPaths"]),
		Limit:          queryIntValue(query.Get("limit")),
		RequestID:      query.Get("requestId"),
		BaseURL:        query.Get("baseUrl"),
		EvidenceDir:    query.Get("evidenceDir"),
		TimeoutSeconds: queryIntValue(query.Get("timeoutSeconds")),
	}
}

func caseSuiteBriefOptionsFromRequest(r *http.Request) casesuite.BriefOptions {
	query := r.URL.Query()
	return casesuite.BriefOptions{
		Signals:        queryStringList(query["signal"], query["signals"], query["change"], query["changes"], query["changedPath"], query["changedPaths"]),
		Limit:          queryIntValue(query.Get("limit")),
		StabilityLimit: queryIntValue(query.Get("stabilityLimit")),
		RequestID:      query.Get("requestId"),
		BaseURL:        query.Get("baseUrl"),
		EvidenceDir:    query.Get("evidenceDir"),
		TimeoutSeconds: queryIntValue(query.Get("timeoutSeconds")),
	}
}

func caseSuitePlanOptionsFromPayload(payload map[string]any) casesuite.PlanOptions {
	return casesuite.PlanOptions{
		RequestID:      valueString(payload["requestId"]),
		Actions:        stringListValue(firstNonNil(payload["action"], payload["actions"])),
		BaseURL:        valueString(payload["baseUrl"]),
		EvidenceDir:    valueString(payload["evidenceDir"]),
		TimeoutSeconds: intValue(payload["timeoutSeconds"]),
	}
}

func caseSuiteImpactOptionsFromRequest(r *http.Request) casesuite.ImpactOptions {
	query := r.URL.Query()
	return casesuite.ImpactOptions{
		Signals: queryStringList(query["signal"], query["signals"], query["change"], query["changes"], query["changedPath"], query["changedPaths"]),
		Plan:    caseSuitePlanOptionsFromRequest(r),
	}
}

func caseSuiteImpactOptionsFromPayload(payload map[string]any) casesuite.ImpactOptions {
	return casesuite.ImpactOptions{
		Signals: stringListValue(firstNonNil(payload["signal"], payload["signals"], payload["change"], payload["changes"], payload["changedPath"], payload["changedPaths"])),
		Plan:    caseSuitePlanOptionsFromPayload(payload),
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func queryIntValue(value string) int {
	out, _ := strconv.Atoi(value)
	return out
}

func queryStringList(groups ...[]string) []string {
	out := []string{}
	for _, group := range groups {
		out = append(out, group...)
	}
	return casesuite.NormalizeStringList(out)
}
