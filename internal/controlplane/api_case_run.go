package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"open-test-sandbox/internal/apicase"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func handleAPICaseRun(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	casePath := strings.TrimSpace(valueString(payload["casePath"]))
	if casePath == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "casePath is required"})
		return
	}

	ctx := r.Context()
	if seconds := intValue(payload["timeoutSeconds"]); seconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		defer cancel()
	}
	result, err := apicase.Run(ctx, apicase.RunOptions{
		CasePath:    casePath,
		BaseURL:     strings.TrimSpace(valueString(payload["baseUrl"])),
		EvidenceDir: firstNonEmpty(valueString(payload["evidenceDir"]), filepath.Join(".runtime", "cases")),
		RunID:       strings.TrimSpace(valueString(payload["runId"])),
		DryRun:      boolValue(payload["dryRun"]),
	})
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if runtime != nil {
		if err := recordAPICaseRun(r.Context(), runtime, bundle.ID, result); err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	report := apiCaseRunReport(result)
	writeJSON(w, map[string]any{
		"ok":        result.Status == store.StatusPassed,
		"dryRun":    result.DryRun,
		"report":    report,
		"summary":   report,
		"viewerUrl": apiCaseViewerURL(result),
	})
}

func recordAPICaseRun(ctx context.Context, runtime store.Store, profileID string, result apicase.RunResult) error {
	now := time.Now().UTC()
	startedAt := apiCaseResultTime(result.StartedAt, now)
	finishedAt := apiCaseResultTime(result.FinishedAt, now)
	if finishedAt.Before(startedAt) {
		finishedAt = startedAt
	}
	requestSummary, assertionSummary, err := apiCaseEvidenceSummaries(result)
	if err != nil {
		return err
	}
	if _, err := runtime.CreateRun(ctx, store.Run{
		ID:           result.RunID,
		ProfileID:    profileID,
		Status:       result.Status,
		EvidenceRoot: result.EvidencePath,
		SummaryJSON:  compactJSON(apiCaseRunReport(result)),
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		CreatedAt:    startedAt,
		UpdatedAt:    finishedAt,
	}); err != nil {
		return err
	}
	caseRunID := result.RunID + ".case"
	if _, err := runtime.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   caseRunID,
		RunID:                result.RunID,
		CaseID:               result.CaseID,
		Status:               result.Status,
		RequestSummaryJSON:   requestSummary,
		AssertionSummaryJSON: assertionSummary,
		StartedAt:            startedAt,
		FinishedAt:           finishedAt,
		CreatedAt:            startedAt,
	}); err != nil {
		return err
	}
	for _, name := range []string{"case.json", "request.json", "response.json", "assertions.json", "summary.json"} {
		path := filepath.Join(result.EvidencePath, name)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		kind := strings.TrimSuffix(name, ".json")
		summary, err := apiCaseEvidenceSummary(path, kind, info.Size())
		if err != nil {
			return err
		}
		if _, err := runtime.RecordEvidence(ctx, store.EvidenceRecord{
			ID:        result.RunID + "." + name,
			RunID:     result.RunID,
			CaseRunID: caseRunID,
			Kind:      kind,
			URI:       path,
			MediaType: "application/json",
			SizeBytes: info.Size(),
			Summary:   summary,
			CreatedAt: finishedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func apiCaseEvidenceSummaries(result apicase.RunResult) (string, string, error) {
	requestSummary, err := apiCaseEvidenceSummary(filepath.Join(result.EvidencePath, "request.json"), "request", 0)
	if err != nil {
		return "", "", err
	}
	assertionPath := filepath.Join(result.EvidencePath, "assertions.json")
	assertionSummary, err := apiCaseEvidenceSummary(assertionPath, "assertions", 0)
	if errors.Is(err, os.ErrNotExist) {
		assertionSummary = compactJSON(map[string]any{"status": result.Status, "errorCount": 0})
		err = nil
	}
	return requestSummary, assertionSummary, err
}

func apiCaseEvidenceSummary(path string, kind string, fallbackSize int64) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return "", err
	}
	switch kind {
	case "request":
		headers, _ := item["headers"].(map[string]any)
		_, hasBody := item["body"]
		return compactJSON(map[string]any{
			"method":      valueString(item["method"]),
			"path":        valueString(item["path"]),
			"headerCount": len(headers),
			"hasBody":     hasBody,
		}), nil
	case "response":
		headers, _ := item["headers"].(map[string]any)
		return compactJSON(map[string]any{
			"statusCode":  intValue(item["statusCode"]),
			"headerCount": len(headers),
			"bodyBytes":   len(valueString(item["body"])),
		}), nil
	case "assertions":
		errorsValue, _ := item["errors"].([]any)
		return compactJSON(map[string]any{
			"status":     valueString(item["status"]),
			"errorCount": len(errorsValue),
		}), nil
	default:
		if fallbackSize == 0 {
			fallbackSize = int64(len(raw))
		}
		return compactJSON(map[string]any{"kind": kind, "sizeBytes": fallbackSize}), nil
	}
}

func apiCaseRunReport(result apicase.RunResult) map[string]any {
	return map[string]any{
		"run_id":        result.RunID,
		"case_id":       result.CaseID,
		"status":        result.Status,
		"dry_run":       result.DryRun,
		"evidence_path": result.EvidencePath,
		"started_at":    result.StartedAt,
		"finished_at":   result.FinishedAt,
		"elapsed_ms":    result.ElapsedMs,
	}
}

func apiCaseViewerURL(result apicase.RunResult) string {
	if result.RunID == "" {
		return ""
	}
	return "/evidence-viewer.html?caseRun=" + url.QueryEscape(result.RunID)
}

func apiCaseResultTime(value string, fallback time.Time) time.Time {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fallback
	}
	return parsed.UTC()
}

func compactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		out, _ := typed.Int64()
		return int(out)
	default:
		return 0
	}
}
