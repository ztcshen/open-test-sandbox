package controlplane

import (
	"context"
	"time"

	"agent-testbench/internal/store"
)

func preferredCaseStates(ctx context.Context, catalog store.ProfileCatalog, runtime store.Store) (map[string]latestCaseState, error) {
	timeoutByCase := interfaceCaseTimeoutsByID(catalog)
	if fast, ok := runtime.(interfaceNodeCaseRunRecordStore); ok {
		caseIDs := make([]string, 0, len(catalog.APICases))
		for _, item := range catalog.APICases {
			if item.ID != "" && activeCatalogStatus(item.Status) {
				caseIDs = append(caseIDs, item.ID)
			}
		}
		records, err := fast.ListAPICaseRunRecordsForCaseIDs(ctx, caseIDs)
		if err != nil {
			return nil, err
		}
		out := map[string]latestCaseState{}
		selectedPassed := map[string]bool{}
		for _, record := range records {
			item := record.CaseRun
			if item.CaseID == "" || selectedPassed[item.CaseID] {
				continue
			}
			state := evaluateLatestCaseStateTimeout(latestCaseStateFromRun(item), timeoutByCase[item.CaseID])
			if _, exists := out[item.CaseID]; !exists {
				out[item.CaseID] = state
			}
			if state.Status == store.StatusPassed {
				out[item.CaseID] = state
				selectedPassed[item.CaseID] = true
			}
		}
		return out, nil
	}
	states, err := latestCaseStates(ctx, runtime)
	if err != nil {
		return nil, err
	}
	for caseID, state := range states {
		states[caseID] = evaluateLatestCaseStateTimeout(state, timeoutByCase[caseID])
	}
	return states, nil
}

type latestCaseState struct {
	Status     string
	RunID      string
	ElapsedMs  int64
	ObservedAt time.Time
}

func latestCaseStates(ctx context.Context, runtime store.Store) (map[string]latestCaseState, error) {
	out := map[string]latestCaseState{}
	if runtime == nil {
		return out, nil
	}
	if fast, ok := runtime.(latestAPICaseRunStore); ok {
		caseRuns, err := fast.ListLatestAPICaseRuns(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range caseRuns {
			if item.CaseID == "" {
				continue
			}
			if _, exists := out[item.CaseID]; !exists {
				out[item.CaseID] = latestCaseStateFromRun(item)
			}
		}
		return out, nil
	}
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	for i := len(runs) - 1; i >= 0; i-- {
		caseRuns, err := runtime.ListAPICaseRuns(ctx, runs[i].ID)
		if err != nil {
			return nil, err
		}
		for j := len(caseRuns) - 1; j >= 0; j-- {
			item := caseRuns[j]
			if item.CaseID == "" {
				continue
			}
			if _, exists := out[item.CaseID]; !exists {
				out[item.CaseID] = latestCaseStateFromRun(item)
			}
		}
	}
	return out, nil
}

func latestCaseStateFromRun(item store.APICaseRun) latestCaseState {
	observedAt := item.FinishedAt
	if observedAt.IsZero() {
		observedAt = item.StartedAt
	}
	if observedAt.IsZero() {
		observedAt = item.CreatedAt
	}
	return latestCaseState{
		Status:     item.Status,
		RunID:      item.RunID,
		ElapsedMs:  elapsedMilliseconds(item.StartedAt, item.FinishedAt),
		ObservedAt: observedAt,
	}
}

func evaluateLatestCaseStateTimeout(state latestCaseState, timeoutMs int) latestCaseState {
	if evaluateRuntimeTimeout(state.ElapsedMs, timeoutMs).Exceeded {
		state.Status = store.StatusFailed
	}
	return state
}

type latestAPICaseRunStore interface {
	ListLatestAPICaseRuns(context.Context) ([]store.APICaseRun, error)
}

func catalogCasesForNode(items []store.CatalogAPICase, nodeID string) []store.CatalogAPICase {
	cases := make([]store.CatalogAPICase, 0)
	for _, item := range items {
		if item.NodeID == nodeID && activeCatalogStatus(item.Status) {
			cases = append(cases, item)
		}
	}
	return cases
}
