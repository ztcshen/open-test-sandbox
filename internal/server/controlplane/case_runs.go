package controlplane

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

func handleCaseRuns(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	if runtime == nil {
		writeJSON(w, emptyCaseRunsPayload(bundle))
		return
	}
	runs, err := runtime.ListRuns(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	items := make([]map[string]any, 0)
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		caseRuns, err := runtime.ListAPICaseRuns(r.Context(), run.ID)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		evidence, err := runtime.ListEvidence(r.Context(), run.ID)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		for j := len(caseRuns) - 1; j >= 0; j-- {
			items = append(items, caseRunItem(run, caseRuns[j], evidence, bundle.FailureCategories))
		}
	}
	writeJSON(w, map[string]any{
		"ok":                true,
		"caseRuns":          items,
		"failureCategories": caseRunFailureCategoryRulesPayload(bundle.FailureCategories),
		"warnings":          []string{},
	})
}

func emptyCaseRunsPayload(bundle profile.Bundle) map[string]any {
	return map[string]any{
		"ok":                true,
		"caseRuns":          []map[string]any{},
		"failureCategories": caseRunFailureCategoryRulesPayload(bundle.FailureCategories),
		"warnings":          []string{},
	}
}

func caseRunItem(run store.Run, item store.APICaseRun, evidence []store.EvidenceRecord, rules []profile.FailureCategoryRule) map[string]any {
	request := jsonObject(item.RequestSummaryJSON)
	assertion := jsonObject(item.AssertionSummaryJSON)
	operation := caseRunOperation(request, item.CaseID)
	failureReason := caseRunFailureReason(assertion)
	defaultFailureCategory := caseRunDefaultFailureCategory(item.Status, assertion)
	failureCategory := apiCaseBatchApplyFailureCategoryRules(rules, item.Status, defaultFailureCategory, failureReason)
	evidenceCount := 0
	for _, record := range evidence {
		if record.CaseRunID == item.ID {
			evidenceCount++
		}
	}
	out := map[string]any{
		"id":            item.ID,
		"runId":         item.RunID,
		"caseId":        item.CaseID,
		"status":        item.Status,
		"operation":     operation,
		"evidencePath":  run.EvidenceRoot,
		"evidenceCount": evidenceCount,
		"updatedAt":     latestTime(item.CreatedAt, run.UpdatedAt, run.CreatedAt),
	}
	if failureReason != "" {
		out["failureReason"] = failureReason
	}
	if defaultFailureCategory != "" {
		out["defaultFailureCategory"] = defaultFailureCategory
	}
	if failureCategory != "" {
		out["failureCategory"] = failureCategory
	}
	return out
}

func jsonObject(raw string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func caseRunOperation(summary map[string]any, defaultValue string) string {
	method := strings.ToUpper(valueString(summary["method"]))
	path := valueString(summary["path"])
	if method != "" && path != "" {
		return method + " " + path
	}
	if method != "" {
		return method
	}
	if path != "" {
		return path
	}
	return defaultValue
}

func caseRunFailureReason(assertion map[string]any) string {
	status := strings.ToLower(valueString(assertion["status"]))
	if status == "" || status == store.StatusPassed {
		return ""
	}
	if count := valueString(assertion["errorCount"]); count != "" && count != "0" {
		return "assertion errors: " + count
	}
	return "assertion status: " + status
}

func caseRunDefaultFailureCategory(status string, assertion map[string]any) string {
	if strings.EqualFold(status, store.StatusPassed) {
		return ""
	}
	if category := strings.TrimSpace(valueString(assertion["failureCategory"])); category != "" {
		return category
	}
	assertionStatus := strings.ToLower(valueString(assertion["status"]))
	errorCount := valueString(assertion["errorCount"])
	if assertionStatus == store.StatusFailed || (errorCount != "" && errorCount != "0") {
		return "assertion-mismatch"
	}
	if strings.EqualFold(status, store.StatusFailed) {
		return "case-failure"
	}
	return ""
}

func caseRunFailureCategoryRulesPayload(rules []profile.FailureCategoryRule) []map[string]any {
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		item := map[string]any{
			"name":     rule.Name,
			"category": rule.Category,
			"matchers": map[string]any{
				"statuses":          append([]string(nil), rule.Matchers.Statuses...),
				"failureCategories": append([]string(nil), rule.Matchers.FailureCategories...),
				"messageContains":   append([]string(nil), rule.Matchers.MessageContains...),
			},
		}
		out = append(out, item)
	}
	return out
}

func latestTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
