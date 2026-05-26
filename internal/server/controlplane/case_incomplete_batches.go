package controlplane

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
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
	passed, latest, err := apiCaseRunStatusByCase(r.Context(), runtime)
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

func apiCaseRunStatusByCase(ctx context.Context, runtime store.Store) (map[string]bool, map[string]string, error) {
	passed := map[string]bool{}
	latest := map[string]string{}
	err := visitLatestAPICaseRuns(ctx, runtime, func(item store.APICaseRun) {
		if latest[item.CaseID] == "" {
			latest[item.CaseID] = item.Status
		}
		if strings.EqualFold(item.Status, store.StatusPassed) {
			passed[item.CaseID] = true
		}
	})
	if err != nil {
		return nil, nil, err
	}
	return passed, latest, nil
}

func visitLatestAPICaseRuns(ctx context.Context, runtime store.Store, visit func(store.APICaseRun)) error {
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return err
	}
	for i := len(runs) - 1; i >= 0; i-- {
		caseRuns, err := runtime.ListAPICaseRuns(ctx, runs[i].ID)
		if err != nil {
			return err
		}
		for _, item := range caseRuns {
			visit(item)
		}
	}
	return nil
}

func apiCaseSuggestedCommand(item profile.APICase) string {
	casePath := strings.TrimSpace(item.CasePath)
	if casePath == "" {
		return ""
	}
	parts := []string{"agent-testbench case run --case " + strconv.Quote(casePath)}
	parts = appendShellFlag(parts, "--base-url", item.BaseURL)
	parts = appendShellFlag(parts, "--evidence-dir", item.EvidenceDir)
	return strings.Join(parts, " ")
}

func appendShellFlag(parts []string, flag string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return parts
	}
	return append(parts, flag+" "+strconv.Quote(value))
}
