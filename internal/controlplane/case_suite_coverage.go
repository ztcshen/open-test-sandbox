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
