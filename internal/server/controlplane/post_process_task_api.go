package controlplane

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"open-test-sandbox/internal/store"
)

type postProcessTaskFilter struct {
	RunID  string
	StepID string
	CaseID string
	Kind   string
	Status string
}

type postProcessTaskCounts struct {
	Total      int   `json:"total"`
	Passed     int   `json:"passed"`
	Failed     int   `json:"failed"`
	Running    int   `json:"running"`
	Skipped    int   `json:"skipped"`
	DurationMs int64 `json:"durationMs"`
}

type PostProcessTaskStatusSummary struct {
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason"`
	DisplayStatus string `json:"displayStatus"`
}

func PostProcessTaskReadableStatus(row store.PostProcessTask) PostProcessTaskStatusSummary {
	reason := strings.TrimSpace(row.Error)
	if reason == "" {
		summary := map[string]any{}
		_ = json.Unmarshal([]byte(row.SummaryJSON), &summary)
		for _, key := range []string{"reason", "skipReason", "failureReason", "message"} {
			if value := strings.TrimSpace(valueString(summary[key])); value != "" {
				reason = value
				break
			}
		}
	}
	outcome := strings.TrimSpace(row.Status)
	switch row.Status {
	case store.StatusPassed:
		outcome = "success"
		if reason == "" {
			reason = "completed"
		}
	case store.StatusFailed:
		outcome = "failed"
		if reason == "" {
			reason = "failed"
		}
	case store.StatusSkipped:
		outcome = "skipped"
		if reason == "" {
			reason = "skipped"
		}
	case store.StatusRunning:
		outcome = "running"
		if reason == "" {
			reason = "in progress"
		}
	default:
		if outcome == "" {
			outcome = "unknown"
		}
		if reason == "" {
			reason = outcome
		}
	}
	return PostProcessTaskStatusSummary{
		Outcome:       outcome,
		Reason:        reason,
		DisplayStatus: strings.TrimSpace(row.Status + ": " + reason),
	}
}

func handlePostProcessTasks(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	filter := postProcessTaskFilter{
		RunID:  strings.TrimSpace(r.URL.Query().Get("runId")),
		StepID: strings.TrimSpace(r.URL.Query().Get("stepId")),
		CaseID: strings.TrimSpace(r.URL.Query().Get("caseId")),
		Kind:   strings.TrimSpace(r.URL.Query().Get("kind")),
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
	}
	if filter.RunID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "runId is required"})
		return
	}
	if runtime == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "runtime Store is not configured"})
		return
	}
	if _, err := runtime.GetRun(r.Context(), filter.RunID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSONStatus(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	rows, err := runtime.ListPostProcessTasks(r.Context(), filter.RunID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	counts := postProcessTaskCounts{}
	tasks := []map[string]any{}
	for _, row := range rows {
		if !controlPlanePostProcessTaskMatches(row, filter) {
			continue
		}
		counts.Total++
		counts.DurationMs += row.DurationMs
		switch row.Status {
		case store.StatusPassed:
			counts.Passed++
		case store.StatusFailed:
			counts.Failed++
		case store.StatusRunning:
			counts.Running++
		case store.StatusSkipped:
			counts.Skipped++
		}
		readable := PostProcessTaskReadableStatus(row)
		tasks = append(tasks, map[string]any{
			"id":            row.ID,
			"runId":         row.RunID,
			"workflowId":    row.WorkflowID,
			"stepId":        row.StepID,
			"caseId":        row.CaseID,
			"kind":          row.Kind,
			"status":        row.Status,
			"startedAt":     row.StartedAt,
			"finishedAt":    row.FinishedAt,
			"durationMs":    row.DurationMs,
			"outcome":       readable.Outcome,
			"reason":        readable.Reason,
			"displayStatus": readable.DisplayStatus,
			"error":         row.Error,
			"summaryJson":   row.SummaryJSON,
			"createdAt":     row.CreatedAt,
		})
	}
	writeJSON(w, map[string]any{
		"ok":     true,
		"runId":  filter.RunID,
		"stepId": filter.StepID,
		"caseId": filter.CaseID,
		"kind":   filter.Kind,
		"status": filter.Status,
		"counts": counts,
		"tasks":  tasks,
	})
}

func controlPlanePostProcessTaskMatches(row store.PostProcessTask, filter postProcessTaskFilter) bool {
	if filter.StepID != "" && row.StepID != filter.StepID {
		return false
	}
	if filter.CaseID != "" && row.CaseID != filter.CaseID {
		return false
	}
	if filter.Kind != "" && row.Kind != filter.Kind {
		return false
	}
	if filter.Status != "" && row.Status != filter.Status {
		return false
	}
	return true
}
