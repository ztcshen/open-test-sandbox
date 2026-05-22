package controlplane

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"open-test-sandbox/internal/store"
)

var ErrEvidenceListRunNotFound = errors.New("run not found")

type EvidenceListReport struct {
	Runs []EvidenceRunReport `json:"runs"`
}

type EvidenceRunReport struct {
	ID              string                 `json:"id"`
	ProfileID       string                 `json:"profileId"`
	WorkflowID      string                 `json:"workflowId"`
	Status          string                 `json:"status"`
	EvidenceRoot    string                 `json:"evidenceRoot"`
	APICaseRunCount int                    `json:"apiCaseRunCount"`
	EvidenceCount   int                    `json:"evidenceCount"`
	APICaseRuns     []store.APICaseRun     `json:"apiCaseRuns"`
	EvidenceRecords []EvidenceRecordReport `json:"evidenceRecords"`
}

type EvidenceRecordReport struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	CaseRunID  string         `json:"caseRunId"`
	StepID     string         `json:"stepId,omitempty"`
	Kind       string         `json:"kind"`
	URI        string         `json:"uri"`
	MediaType  string         `json:"mediaType"`
	SHA256     string         `json:"sha256"`
	SizeBytes  int64          `json:"sizeBytes"`
	Summary    string         `json:"summary"`
	Category   string         `json:"category,omitempty"`
	Visibility string         `json:"visibility,omitempty"`
	Labels     map[string]any `json:"labels,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

func EvidenceList(ctx context.Context, runtime store.Store, runID string) (EvidenceListReport, error) {
	if runtime == nil {
		return EvidenceListReport{Runs: []EvidenceRunReport{}}, nil
	}
	runs, err := evidenceListRuns(ctx, runtime, runID)
	if err != nil {
		return EvidenceListReport{}, err
	}
	report := EvidenceListReport{Runs: make([]EvidenceRunReport, 0, len(runs))}
	for _, run := range runs {
		caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
		if err != nil {
			return EvidenceListReport{}, err
		}
		records, err := runtime.ListEvidence(ctx, run.ID)
		if err != nil {
			return EvidenceListReport{}, err
		}
		report.Runs = append(report.Runs, EvidenceRunReport{
			ID:              run.ID,
			ProfileID:       run.ProfileID,
			WorkflowID:      run.WorkflowID,
			Status:          run.Status,
			EvidenceRoot:    run.EvidenceRoot,
			APICaseRunCount: len(caseRuns),
			EvidenceCount:   len(records),
			APICaseRuns:     caseRuns,
			EvidenceRecords: evidenceRecordReports(records),
		})
	}
	return report, nil
}

func evidenceRecordReports(records []store.EvidenceRecord) []EvidenceRecordReport {
	out := make([]EvidenceRecordReport, 0, len(records))
	for _, record := range records {
		out = append(out, EvidenceRecordReport{
			ID:         record.ID,
			RunID:      record.RunID,
			CaseRunID:  record.CaseRunID,
			StepID:     evidenceRecordStepID(record),
			Kind:       record.Kind,
			URI:        record.URI,
			MediaType:  record.MediaType,
			SHA256:     record.SHA256,
			SizeBytes:  record.SizeBytes,
			Summary:    record.Summary,
			Category:   record.Category,
			Visibility: record.Visibility,
			Labels:     jsonObject(record.LabelsJSON),
			CreatedAt:  record.CreatedAt,
		})
	}
	return out
}

func evidenceRecordStepID(record store.EvidenceRecord) string {
	if stepID := strings.TrimSpace(record.StepID); stepID != "" {
		return stepID
	}
	if stepID := strings.TrimSpace(valueString(jsonObject(record.LabelsJSON)["stepId"])); stepID != "" {
		return stepID
	}
	return strings.TrimSpace(valueString(jsonObject(record.Summary)["stepId"]))
}

func evidenceListRuns(ctx context.Context, runtime store.Store, runID string) ([]store.Run, error) {
	if strings.TrimSpace(runID) == "" {
		return runtime.ListRuns(ctx)
	}
	run, err := runtime.GetRun(ctx, strings.TrimSpace(runID))
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrEvidenceListRunNotFound, strings.TrimSpace(runID))
	}
	if err != nil {
		return nil, err
	}
	return []store.Run{run}, nil
}

func handleEvidenceList(w http.ResponseWriter, r *http.Request, runtime store.Store) {
	runID := strings.TrimSpace(r.URL.Query().Get("run"))
	if runID == "" {
		runID = strings.TrimSpace(r.URL.Query().Get("runId"))
	}
	report, err := EvidenceList(r.Context(), runtime, runID)
	if errors.Is(err, ErrEvidenceListRunNotFound) {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":   true,
		"runs": report.Runs,
	})
}
