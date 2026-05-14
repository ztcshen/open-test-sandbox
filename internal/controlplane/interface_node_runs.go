package controlplane

import (
	"context"
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func interfaceNodeDetailPayloadFromBundleWithStore(ctx context.Context, bundle profile.Bundle, id string, runtime store.Store) (interfaceNodeDetailPayload, bool, error) {
	payload, ok := interfaceNodeDetailPayloadFromBundle(bundle, id)
	if !ok || runtime == nil {
		return payload, ok, nil
	}
	if err := hydrateInterfaceNodeRuns(ctx, runtime, &payload); err != nil {
		return interfaceNodeDetailPayload{}, true, err
	}
	return payload, true, nil
}

func hydrateInterfaceNodeRuns(ctx context.Context, runtime store.Store, payload *interfaceNodeDetailPayload) error {
	caseIDs := make(map[string]bool, len(payload.Cases))
	stats := make(map[string]*interfaceNodeCaseStats, len(payload.Cases))
	for _, item := range payload.Cases {
		caseIDs[item.ID] = true
		stats[item.ID] = &interfaceNodeCaseStats{CaseID: item.ID}
	}
	if len(caseIDs) == 0 {
		return nil
	}

	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return err
	}
	allRuns := []map[string]any{}
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
		if err != nil {
			return err
		}
		for j := len(caseRuns) - 1; j >= 0; j-- {
			caseRun := caseRuns[j]
			if !caseIDs[caseRun.CaseID] {
				continue
			}
			item := interfaceNodeRunItem(run, caseRun)
			allRuns = append(allRuns, item)
			stats[caseRun.CaseID].Add(item)
		}
	}

	passCount, failCount, totalElapsedMs := 0, 0, int64(0)
	perCase := make([]map[string]any, 0, len(payload.Cases))
	for i := range payload.Cases {
		stat := stats[payload.Cases[i].ID]
		if stat == nil || stat.RunCount == 0 {
			continue
		}
		payload.Cases[i].LatestRun = stat.LatestRun
		passCount += stat.PassCount
		failCount += stat.FailCount
		totalElapsedMs += stat.TotalElapsedMs
		perCase = append(perCase, stat.Payload())
	}

	payload.Runs = allRuns
	payload.History = interfaceNodeHistory(allRuns, perCase, passCount, failCount, totalElapsedMs)
	payload.Admission.PassedCaseCount = passCount
	payload.Admission.LatestRunID = valueString(payload.History["latestRunId"])
	if failCount > 0 {
		payload.Admission.Status = store.StatusFailed
	} else if passCount > 0 {
		payload.Admission.Status = store.StatusPassed
	}
	return nil
}

type interfaceNodeCaseStats struct {
	CaseID              string
	PassCount           int
	FailCount           int
	RunCount            int
	TotalElapsedMs      int64
	LatestRun           map[string]any
	LatestStatus        string
	LatestElapsedMs     int64
	LatestFailureReason string
}

func (s *interfaceNodeCaseStats) Add(item map[string]any) {
	status := valueString(item["status"])
	elapsedMs, _ := item["elapsedMs"].(int64)
	failureReason := valueString(item["failureReason"])
	s.RunCount++
	s.TotalElapsedMs += elapsedMs
	switch status {
	case store.StatusPassed:
		s.PassCount++
	case store.StatusFailed:
		s.FailCount++
	}
	if s.LatestRun == nil {
		s.LatestRun = item
		s.LatestStatus = status
		s.LatestElapsedMs = elapsedMs
		s.LatestFailureReason = failureReason
	}
}

func (s interfaceNodeCaseStats) Payload() map[string]any {
	return map[string]any{
		"caseId":              s.CaseID,
		"passCount":           s.PassCount,
		"failCount":           s.FailCount,
		"runCount":            s.RunCount,
		"latestStatus":        s.LatestStatus,
		"latestRunId":         valueString(s.LatestRun["runId"]),
		"latestElapsedMs":     s.LatestElapsedMs,
		"latestFailureReason": s.LatestFailureReason,
		"totalElapsedMs":      s.TotalElapsedMs,
	}
}

func interfaceNodeRunItem(run store.Run, item store.APICaseRun) map[string]any {
	assertion := jsonObject(item.AssertionSummaryJSON)
	elapsedMs := elapsedMilliseconds(item.StartedAt, item.FinishedAt)
	return map[string]any{
		"runId":          item.RunID,
		"caseRunId":      item.ID,
		"caseId":         item.CaseID,
		"workflowId":     run.WorkflowID,
		"status":         item.Status,
		"evidencePath":   run.EvidenceRoot,
		"elapsedMs":      elapsedMs,
		"failureReason":  caseRunFailureReason(assertion),
		"requestSummary": jsonObject(item.RequestSummaryJSON),
		"startedAt":      item.StartedAt,
		"finishedAt":     item.FinishedAt,
		"updatedAt":      latestTime(item.CreatedAt, run.UpdatedAt, run.CreatedAt),
	}
}

func interfaceNodeHistory(runs []map[string]any, perCase []map[string]any, passCount int, failCount int, totalElapsedMs int64) map[string]any {
	latestRunID := ""
	latestFailureReason := ""
	if len(runs) > 0 {
		latestRunID = valueString(runs[0]["runId"])
	}
	for _, item := range runs {
		if reason := valueString(item["failureReason"]); reason != "" {
			latestFailureReason = reason
			break
		}
	}
	return map[string]any{
		"latestRunId":         latestRunID,
		"passCount":           passCount,
		"failCount":           failCount,
		"runCount":            len(runs),
		"latestFailureReason": latestFailureReason,
		"totalElapsedMs":      totalElapsedMs,
		"perCase":             perCase,
	}
}

func elapsedMilliseconds(started time.Time, finished time.Time) int64 {
	if started.IsZero() || finished.IsZero() || !finished.After(started) {
		return 0
	}
	return finished.Sub(started).Milliseconds()
}
