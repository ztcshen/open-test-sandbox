package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

const readModelInterfaceNodeDetailPrefix = "interface-node:"

type interfaceNodeRunContext struct {
	RunID      string
	WorkflowID string
	StepID     string
}

func interfaceNodeRunContextFromQuery(query url.Values) interfaceNodeRunContext {
	return interfaceNodeRunContext{
		RunID:      firstNonEmpty(query.Get("runId"), query.Get("workflowRunId")),
		WorkflowID: firstNonEmpty(query.Get("flowId"), query.Get("workflowId"), query.Get("workflow")),
		StepID:     firstNonEmpty(query.Get("stepId"), query.Get("step")),
	}
}

func (c interfaceNodeRunContext) Active() bool {
	return c.RunID != "" || c.WorkflowID != "" || c.StepID != ""
}

func (c interfaceNodeRunContext) Payload() map[string]string {
	if !c.Active() {
		return nil
	}
	out := map[string]string{}
	if c.RunID != "" {
		out["runId"] = c.RunID
	}
	if c.WorkflowID != "" {
		out["flowId"] = c.WorkflowID
		out["workflowId"] = c.WorkflowID
	}
	if c.StepID != "" {
		out["stepId"] = c.StepID
	}
	return out
}

func (c interfaceNodeRunContext) MatchesRun(run store.Run) bool {
	if c.RunID != "" && run.ID != c.RunID {
		return false
	}
	if c.WorkflowID != "" && run.WorkflowID != c.WorkflowID {
		return false
	}
	return true
}

func (c interfaceNodeRunContext) MatchesCaseRun(run store.Run, caseRun store.APICaseRun) bool {
	if c.StepID == "" {
		return true
	}
	request := jsonObject(caseRun.RequestSummaryJSON)
	if valueString(request["stepId"]) == c.StepID {
		return true
	}
	step, ok := workflowRunStepMust(run.SummaryJSON, c.StepID)
	if !ok {
		return false
	}
	return valueString(step["caseId"]) == caseRun.CaseID
}

func InterfaceNodeDetailReadModelKey(nodeID string) string {
	return readModelInterfaceNodeDetailPrefix + nodeID
}

func InterfaceNodeDetailReadModels(catalog store.ProfileCatalog, configVersionID string, generatedAt time.Time) ([]store.ReadModel, error) {
	models := make([]store.ReadModel, 0, len(catalog.InterfaceNodes))
	for _, node := range catalog.InterfaceNodes {
		payload, ok := interfaceNodeDetailPayloadFromCatalog(catalog, node.ID)
		if !ok {
			continue
		}
		payload.Source = map[string]string{"kind": "read-model", "id": catalog.ProfileID}
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		models = append(models, store.ReadModel{
			ProfileID:       catalog.ProfileID,
			Key:             InterfaceNodeDetailReadModelKey(node.ID),
			ConfigVersionID: configVersionID,
			PayloadJSON:     string(raw),
			GeneratedAt:     generatedAt,
			UpdatedAt:       generatedAt,
		})
	}
	return models, nil
}

func workflowRunStepMust(raw string, stepID string) (map[string]any, bool) {
	summary, err := workflowRunSummary(raw)
	if err != nil {
		return nil, false
	}
	return workflowRunStep(summary, stepID)
}

func interfaceNodeDetailPayloadFromBundleWithStore(ctx context.Context, bundle profile.Bundle, id string, runtime store.Store, runContext interfaceNodeRunContext) (interfaceNodeDetailPayload, bool, error) {
	if runtime != nil {
		catalog, err := runtime.GetProfileCatalog(ctx)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return interfaceNodeDetailPayload{}, true, err
		}
		if err == nil {
			payload, ok, err := interfaceNodeDetailPayloadFromReadModel(ctx, runtime, catalog.ProfileID, id)
			if err != nil {
				return interfaceNodeDetailPayload{}, true, err
			}
			if !ok {
				payload, ok = interfaceNodeDetailPayloadFromCatalog(catalog, id)
			}
			if ok {
				if err := hydrateInterfaceNodeRuns(ctx, runtime, &payload, runContext); err != nil {
					return interfaceNodeDetailPayload{}, true, err
				}
				return payload, true, nil
			}
		}
	}
	payload, ok := interfaceNodeDetailPayloadFromBundle(bundle, id)
	if !ok || runtime == nil {
		return payload, ok, nil
	}
	if err := hydrateInterfaceNodeRuns(ctx, runtime, &payload, runContext); err != nil {
		return interfaceNodeDetailPayload{}, true, err
	}
	return payload, true, nil
}

func interfaceNodeDetailPayloadFromReadModel(ctx context.Context, runtime store.Store, profileID string, nodeID string) (interfaceNodeDetailPayload, bool, error) {
	model, err := runtime.GetReadModel(ctx, profileID, InterfaceNodeDetailReadModelKey(nodeID))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return interfaceNodeDetailPayload{}, false, nil
		}
		return interfaceNodeDetailPayload{}, false, err
	}
	var payload interfaceNodeDetailPayload
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		return interfaceNodeDetailPayload{}, false, err
	}
	payload.Source = map[string]string{"kind": "read-model", "id": profileID}
	return payload, true, nil
}

func hydrateInterfaceNodeRuns(ctx context.Context, runtime store.Store, payload *interfaceNodeDetailPayload, runContext interfaceNodeRunContext) error {
	payload.Context = runContext.Payload()
	timeoutByCase := map[string]int{}
	caseIDs := make(map[string]bool, len(payload.Cases))
	stats := make(map[string]*interfaceNodeCaseStats, len(payload.Cases))
	for _, item := range payload.Cases {
		caseIDs[item.ID] = true
		timeoutByCase[item.ID] = payload.Node.TimeoutMs
		stats[item.ID] = &interfaceNodeCaseStats{CaseID: item.ID}
	}
	if len(caseIDs) == 0 {
		return nil
	}
	if fast, ok := runtime.(interfaceNodeCaseRunRecordStore); ok {
		return hydrateInterfaceNodeRunRecords(ctx, fast, runtime, payload, runContext, caseIDList(caseIDs), stats, timeoutByCase)
	}

	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return err
	}
	allRuns := []map[string]any{}
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if !runContext.MatchesRun(run) {
			continue
		}
		topologies, err := runtime.ListTraceTopologies(ctx, run.ID)
		if err != nil {
			return err
		}
		caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
		if err != nil {
			return err
		}
		for j := len(caseRuns) - 1; j >= 0; j-- {
			caseRun := caseRuns[j]
			if !caseIDs[caseRun.CaseID] {
				continue
			}
			if !runContext.MatchesCaseRun(run, caseRun) {
				continue
			}
			item := interfaceNodeRunItem(run, caseRun, topologies, timeoutByCase[caseRun.CaseID])
			allRuns = append(allRuns, item)
			stats[caseRun.CaseID].Add(item)
		}
	}
	hydrateInterfaceNodeHistory(payload, stats, allRuns)
	return nil
}

type interfaceNodeCaseRunRecordStore interface {
	ListAPICaseRunRecordsForCaseIDs(context.Context, []string) ([]store.APICaseRunRecord, error)
}

func hydrateInterfaceNodeRunRecords(ctx context.Context, fast interfaceNodeCaseRunRecordStore, runtime store.Store, payload *interfaceNodeDetailPayload, runContext interfaceNodeRunContext, caseIDs []string, stats map[string]*interfaceNodeCaseStats, timeoutByCase map[string]int) error {
	records, err := fast.ListAPICaseRunRecordsForCaseIDs(ctx, caseIDs)
	if err != nil {
		return err
	}
	allRuns := []map[string]any{}
	topologyByRun := map[string][]store.TraceTopology{}
	for _, record := range records {
		run := record.Run
		caseRun := record.CaseRun
		if !runContext.MatchesRun(run) || !runContext.MatchesCaseRun(run, caseRun) {
			continue
		}
		topologies, ok := topologyByRun[run.ID]
		if !ok {
			topologies, err = runtime.ListTraceTopologies(ctx, run.ID)
			if err != nil {
				return err
			}
			topologyByRun[run.ID] = topologies
		}
		item := interfaceNodeRunItem(run, caseRun, topologies, timeoutByCase[caseRun.CaseID])
		allRuns = append(allRuns, item)
		if stat := stats[caseRun.CaseID]; stat != nil {
			stat.Add(item)
		}
	}
	hydrateInterfaceNodeHistory(payload, stats, allRuns)
	return nil
}

func caseIDList(caseIDs map[string]bool) []string {
	out := make([]string, 0, len(caseIDs))
	for id := range caseIDs {
		out = append(out, id)
	}
	return out
}

func hydrateInterfaceNodeHistory(payload *interfaceNodeDetailPayload, stats map[string]*interfaceNodeCaseStats, allRuns []map[string]any) {
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
	requiredPassed, blockers := interfaceNodeAdmissionState(payload.Cases)
	payload.Admission.PassedCaseCount = requiredPassed
	payload.Admission.LatestRunID = firstNonEmpty(admissionLatestRunID(payload.Cases), valueString(payload.History["latestRunId"]))
	payload.Admission.Blockers = blockers
	if len(blockers) > 0 {
		payload.Admission.Status = store.StatusFailed
		payload.Attention = interfaceNodeAttentionPayload(payload.Admission)
	} else if payload.Admission.RequiredCaseCount > 0 && requiredPassed == payload.Admission.RequiredCaseCount {
		payload.Admission.Status = store.StatusPassed
	}
}

func admissionLatestRunID(cases []interfaceCase) string {
	for _, item := range cases {
		if !item.RequiredForAdmission || !activeCatalogStatus(item.Status) {
			continue
		}
		if runID := valueString(item.LatestRun["runId"]); runID != "" {
			return runID
		}
	}
	return ""
}

func interfaceNodeAdmissionState(cases []interfaceCase) (int, []map[string]any) {
	passed := 0
	blockers := []map[string]any{}
	for _, item := range cases {
		if !item.RequiredForAdmission || !activeCatalogStatus(item.Status) {
			continue
		}
		run := item.LatestRun
		if len(run) == 0 || valueString(run["runId"]) == "" {
			blockers = append(blockers, map[string]any{
				"caseId":        item.ID,
				"title":         firstNonEmpty(item.Title, item.ID),
				"status":        "missing_run",
				"failureReason": "required case has no run",
			})
			continue
		}
		status := valueString(run["status"])
		if status == store.StatusPassed {
			passed++
			continue
		}
		blockerStatus := "pending"
		reason := "latest run is " + firstNonEmpty(status, "unknown")
		if status == store.StatusFailed {
			blockerStatus = store.StatusFailed
			reason = firstNonEmpty(valueString(run["failureReason"]), "required case latest run failed")
		}
		runID := valueString(run["runId"])
		blocker := map[string]any{
			"caseId":        item.ID,
			"title":         firstNonEmpty(item.Title, item.ID),
			"status":        blockerStatus,
			"runId":         runID,
			"elapsedMs":     run["elapsedMs"],
			"failureKind":   valueString(run["failureKind"]),
			"failureReason": reason,
		}
		if runID != "" {
			blocker["evidenceHref"] = "/evidence-viewer.html?caseRun=" + url.QueryEscape(runID) + "&caseId=" + url.QueryEscape(item.ID)
		}
		blockers = append(blockers, blocker)
	}
	return passed, blockers
}

func interfaceNodeAttentionPayload(admission interfaceNodeAdmission) map[string]any {
	return map[string]any{
		"status":            admission.Status,
		"requiredCaseCount": admission.RequiredCaseCount,
		"passedCaseCount":   admission.PassedCaseCount,
		"latestRunId":       admission.LatestRunID,
		"blockerCount":      len(admission.Blockers),
		"blockers":          admission.Blockers,
	}
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
	if s.LatestRun == nil || (status == store.StatusPassed && valueString(s.LatestRun["status"]) != store.StatusPassed) {
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

func interfaceNodeRunItem(run store.Run, item store.APICaseRun, topologies []store.TraceTopology, timeoutMs int) map[string]any {
	assertion := jsonObject(item.AssertionSummaryJSON)
	elapsedMs := elapsedMilliseconds(item.StartedAt, item.FinishedAt)
	payload := map[string]any{
		"runId":          item.RunID,
		"caseRunId":      item.ID,
		"caseId":         item.CaseID,
		"workflowId":     run.WorkflowID,
		"status":         item.Status,
		"evidencePath":   run.EvidenceRoot,
		"elapsedMs":      elapsedMs,
		"failureKind":    firstNonEmpty(valueString(assertion["failureKind"]), valueString(assertion["failure_kind"])),
		"failureReason":  caseRunFailureReason(assertion),
		"requestSummary": jsonObject(item.RequestSummaryJSON),
		"startedAt":      item.StartedAt,
		"finishedAt":     item.FinishedAt,
		"updatedAt":      latestTime(item.CreatedAt, run.UpdatedAt, run.CreatedAt),
	}
	if topology := storedTraceTopologyEvidence(item.CaseID, topologies); len(topology) > 0 {
		payload[topologyPayloadField] = topology
	}
	evaluation := evaluateRuntimeTimeout(elapsedMs, timeoutMs)
	applyTimeoutFailure(payload, evaluation)
	return payload
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
