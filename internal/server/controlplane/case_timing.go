package controlplane

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"open-test-sandbox/internal/store"
)

func handleCaseTiming(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	payload, err := CaseTimingPayload(r.Context(), runtime, r.URL.Query().Get("kind"), r.URL.Query().Get("maxAgeMinutes"))
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, payload)
}

func CaseTimingPayload(ctx context.Context, runtime store.Store, kindValue string, maxAgeMinutes string) (map[string]any, error) {
	if runtime == nil {
		return emptyCaseTimingPayload(), nil
	}
	kind := strings.ToLower(strings.TrimSpace(kindValue))
	if kind == "" {
		kind = "all"
	}
	if kind == "candidate" {
		return emptyCaseTimingPayload(), nil
	}
	rows, err := caseTimingRows(ctx, runtime, maxAgeDuration(maxAgeMinutes))
	if err != nil {
		return nil, err
	}
	return caseTimingPayload(rows), nil
}

func caseTimingRows(ctx context.Context, runtime store.Store, maxAge time.Duration) ([]map[string]any, error) {
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().UTC().Add(-maxAge)
	}
	rows := make([]map[string]any, 0)
	for _, run := range runs {
		caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		for _, item := range caseRuns {
			if !cutoff.IsZero() && item.CreatedAt.Before(cutoff) {
				continue
			}
			duration := measuredDuration(item.StartedAt, item.FinishedAt, run.StartedAt, run.FinishedAt)
			row := map[string]any{
				"kind":       "caseRun",
				"id":         item.ID,
				"runId":      item.RunID,
				"caseId":     item.CaseID,
				"status":     item.Status,
				"durationMs": duration,
				"source":     "store",
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func caseTimingPayload(rows []map[string]any) map[string]any {
	measured := 0
	maxDuration := int64(0)
	var slowest map[string]any
	for _, row := range rows {
		duration, _ := row["durationMs"].(int64)
		if duration <= 0 {
			continue
		}
		measured++
		if duration > maxDuration {
			maxDuration = duration
			slowest = row
		}
	}
	slowestRows := map[string]any{}
	if slowest != nil {
		slowestRows["caseRun"] = slowest
		slowestRows["overall"] = slowest
	}
	return map[string]any{
		"ok": true,
		"summary": map[string]any{
			"caseRunCount":          len(rows),
			"candidateBatchCount":   0,
			"durationMeasuredCount": measured,
			"maxDurationMs":         maxDuration,
			"speedup":               map[string]any{"available": false},
			"slowestRows":           slowestRows,
		},
		"warningDetails": []map[string]any{},
		"warnings":       []string{},
	}
}

func emptyCaseTimingPayload() map[string]any {
	return caseTimingPayload(nil)
}

func measuredDuration(values ...time.Time) int64 {
	for i := 0; i+1 < len(values); i += 2 {
		started := values[i]
		finished := values[i+1]
		if started.IsZero() || finished.IsZero() || finished.Before(started) {
			continue
		}
		return finished.Sub(started).Milliseconds()
	}
	return 0
}

func maxAgeDuration(value string) time.Duration {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	minutes, err := strconv.Atoi(value)
	if err != nil || minutes <= 0 {
		return 0
	}
	return time.Duration(minutes) * time.Minute
}
