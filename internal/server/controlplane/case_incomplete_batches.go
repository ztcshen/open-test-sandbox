package controlplane

import (
	"net/http"
	"strconv"
	"strings"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

func handleCaseIncompleteBatches(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	if runtime == nil {
		writeJSON(w, map[string]any{
			"ok":       true,
			"count":    0,
			"items":    []map[string]any{},
			"warnings": []string{"runtime store is not configured"},
		})
		return
	}
	passed, latest, err := apiCaseRunStatusByCase(r, runtime)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	items := make([]map[string]any, 0)
	for _, item := range bundle.APICases {
		if strings.TrimSpace(item.ID) == "" || passed[item.ID] {
			continue
		}
		reason := "not-run"
		if status := latest[item.ID]; status != "" {
			reason = "latest-" + status
		}
		items = append(items, map[string]any{
			"id":               item.ID,
			"title":            firstNonEmpty(item.DisplayName, item.ID),
			"reason":           reason,
			"source":           "profile:" + bundle.ID,
			"message":          "no passed Store run found for this API Case",
			"suggestedCommand": apiCaseSuggestedCommand(item),
		})
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"count":    len(items),
		"items":    items,
		"warnings": []string{},
	})
}

func apiCaseRunStatusByCase(r *http.Request, runtime store.Store) (map[string]bool, map[string]string, error) {
	runs, err := runtime.ListRuns(r.Context())
	if err != nil {
		return nil, nil, err
	}
	passed := map[string]bool{}
	latest := map[string]string{}
	for i := len(runs) - 1; i >= 0; i-- {
		caseRuns, err := runtime.ListAPICaseRuns(r.Context(), runs[i].ID)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range caseRuns {
			if latest[item.CaseID] == "" {
				latest[item.CaseID] = item.Status
			}
			if strings.EqualFold(item.Status, store.StatusPassed) {
				passed[item.CaseID] = true
			}
		}
	}
	return passed, latest, nil
}

func apiCaseSuggestedCommand(item profile.APICase) string {
	casePath := strings.TrimSpace(item.CasePath)
	if casePath == "" {
		return ""
	}
	parts := []string{"otsandbox case run --case " + strconv.Quote(casePath)}
	if strings.TrimSpace(item.BaseURL) != "" {
		parts = append(parts, "--base-url "+strconv.Quote(item.BaseURL))
	}
	if strings.TrimSpace(item.EvidenceDir) != "" {
		parts = append(parts, "--evidence-dir "+strconv.Quote(item.EvidenceDir))
	}
	return strings.Join(parts, " ")
}
