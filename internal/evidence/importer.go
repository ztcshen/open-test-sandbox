package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"open-test-sandbox/internal/store"
)

type ImportOptions struct {
	SourcePath string
	ProfileID  string
	Store      store.Store
}

type ImportResult struct {
	RunCount        int
	APICaseRunCount int
	EvidenceCount   int
}

func ImportLegacyRuntime(ctx context.Context, options ImportOptions) (ImportResult, error) {
	if strings.TrimSpace(options.SourcePath) == "" {
		return ImportResult{}, errors.New("source path is required")
	}
	if strings.TrimSpace(options.ProfileID) == "" {
		return ImportResult{}, errors.New("profile id is required")
	}
	if options.Store == nil {
		return ImportResult{}, errors.New("store is required")
	}
	if _, err := os.Stat(options.SourcePath); err != nil {
		return ImportResult{}, fmt.Errorf("stat source runtime store: %w", err)
	}

	var result ImportResult
	workflowRuns, err := loadLegacyWorkflowRuns(ctx, options.SourcePath)
	if err != nil {
		return ImportResult{}, err
	}
	for _, item := range workflowRuns {
		run := store.Run{
			ID:           fmt.Sprintf("legacy-workflow-%d", item.ID),
			ProfileID:    options.ProfileID,
			WorkflowID:   item.WorkflowID,
			Status:       normalizeStatus(item.Status),
			EvidenceRoot: "",
			SummaryJSON:  item.SummaryJSON,
			StartedAt:    parseTime(item.CreatedAt),
			CreatedAt:    parseTime(item.CreatedAt),
			UpdatedAt:    parseTime(item.CreatedAt),
		}
		if _, err := createRunIfMissing(ctx, options.Store, run); err != nil {
			return ImportResult{}, err
		}
		result.RunCount++
	}

	caseRuns, err := loadLegacyCaseRuns(ctx, options.SourcePath)
	if err != nil {
		return ImportResult{}, err
	}
	for _, item := range caseRuns {
		parent := store.Run{
			ID:           item.RunID,
			ProfileID:    options.ProfileID,
			WorkflowID:   "",
			Status:       normalizeStatus(item.Status),
			EvidenceRoot: item.EvidencePath,
			SummaryJSON:  item.SummaryJSON,
			StartedAt:    parseTime(item.CreatedAt),
			CreatedAt:    parseTime(item.CreatedAt),
			UpdatedAt:    parseTime(item.CreatedAt),
		}
		if _, err := createRunIfMissing(ctx, options.Store, parent); err != nil {
			return ImportResult{}, err
		}
		result.RunCount++

		caseRun := store.APICaseRun{
			ID:                   fmt.Sprintf("legacy-case-run-%d", item.ID),
			RunID:                item.RunID,
			CaseID:               item.CaseID,
			Status:               normalizeStatus(item.Status),
			RequestSummaryJSON:   "{}",
			AssertionSummaryJSON: legacyCaseSummary(item),
			StartedAt:            parseTime(item.CreatedAt),
			FinishedAt:           parseTime(item.CreatedAt),
			CreatedAt:            parseTime(item.CreatedAt),
		}
		if _, err := options.Store.RecordAPICaseRun(ctx, caseRun); err != nil {
			return ImportResult{}, err
		}
		result.APICaseRunCount++

		if strings.TrimSpace(item.EvidencePath) != "" {
			record := store.EvidenceRecord{
				ID:        fmt.Sprintf("legacy-evidence-%d", item.ID),
				RunID:     item.RunID,
				CaseRunID: caseRun.ID,
				Kind:      "case-run",
				URI:       item.EvidencePath,
				MediaType: "application/json",
				Summary:   item.FailureReason,
				CreatedAt: parseTime(item.CreatedAt),
			}
			if _, err := options.Store.RecordEvidence(ctx, record); err != nil {
				return ImportResult{}, err
			}
			result.EvidenceCount++
		}
	}
	return result, nil
}

func createRunIfMissing(ctx context.Context, target store.Store, run store.Run) (store.Run, error) {
	loaded, err := target.GetRun(ctx, run.ID)
	if err == nil {
		return loaded, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Run{}, err
	}
	return target.CreateRun(ctx, run)
}

type legacyWorkflowRun struct {
	ID          int64  `json:"id"`
	WorkflowID  string `json:"workflow_id"`
	Status      string `json:"status"`
	SummaryJSON string `json:"summary_json"`
	CreatedAt   string `json:"created_at"`
}

type legacyCaseRun struct {
	ID            int64  `json:"id"`
	NodeID        string `json:"node_id"`
	CaseID        string `json:"case_id"`
	RunID         string `json:"run_id"`
	Status        string `json:"status"`
	FailureKind   string `json:"failure_kind"`
	FailureReason string `json:"failure_reason"`
	EvidencePath  string `json:"evidence_path"`
	ElapsedMs     int64  `json:"elapsed_ms"`
	SummaryJSON   string `json:"summary_json"`
	CreatedAt     string `json:"created_at"`
}

func loadLegacyWorkflowRuns(ctx context.Context, sourcePath string) ([]legacyWorkflowRun, error) {
	var rows []legacyWorkflowRun
	if err := legacyQuery(ctx, sourcePath, `
select id, workflow_id, status, substr(summary_json, 1, 8192) as summary_json, created_at
from workflow_runs order by id;`, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func loadLegacyCaseRuns(ctx context.Context, sourcePath string) ([]legacyCaseRun, error) {
	var rows []legacyCaseRun
	if err := legacyQuery(ctx, sourcePath, `
select id, node_id, case_id, run_id, status, failure_kind, failure_reason, evidence_path, elapsed_ms, substr(summary_json, 1, 8192) as summary_json, created_at
from interface_node_case_run order by id;`, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func legacyQuery(ctx context.Context, sourcePath string, statement string, target any) error {
	cmd := exec.CommandContext(ctx, "sqlite3", "-json", sourcePath, statement)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("query source runtime store: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		out = []byte("[]")
	}
	if err := json.Unmarshal(out, target); err != nil {
		return fmt.Errorf("decode source runtime rows: %w", err)
	}
	return nil
}

func legacyCaseSummary(item legacyCaseRun) string {
	raw, err := json.Marshal(map[string]any{
		"nodeId":        item.NodeID,
		"failureKind":   item.FailureKind,
		"failureReason": item.FailureReason,
		"elapsedMs":     item.ElapsedMs,
		"summary":       item.SummaryJSON,
	})
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func normalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case store.StatusPassed:
		return store.StatusPassed
	case store.StatusFailed:
		return store.StatusFailed
	case store.StatusSkipped:
		return store.StatusSkipped
	default:
		return store.StatusRunning
	}
}

func parseTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Now().UTC()
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}
