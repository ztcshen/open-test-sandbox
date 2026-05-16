package controlplane

import (
	"context"
	"errors"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

func apiCaseCapabilitiesFromBundleWithStore(ctx context.Context, bundle profile.Bundle, runtime store.Store) (apiCaseCapabilitiesPayload, error) {
	payload := apiCaseCapabilitiesFromBundle(bundle)
	if runtime == nil {
		return payload, nil
	}
	if catalog, err := runtime.GetProfileCatalog(ctx); err == nil {
		payload = apiCaseCapabilitiesFromCatalog(catalog)
	} else if !errors.Is(err, store.ErrNotFound) {
		return apiCaseCapabilitiesPayload{}, err
	}
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return apiCaseCapabilitiesPayload{}, err
	}
	byCase, err := apiCaseCapabilityRuns(ctx, runtime, runs)
	if err != nil {
		return apiCaseCapabilitiesPayload{}, err
	}
	for i := range payload.Cases {
		state := byCase[payload.Cases[i].ID]
		payload.Cases[i].RunCount = state.Count
		payload.Cases[i].LatestRun = state.Latest
	}
	return payload, nil
}

type apiCaseCapabilityRunState struct {
	Count  int
	Latest map[string]any
}

func apiCaseCapabilityRuns(ctx context.Context, runtime store.Store, runs []store.Run) (map[string]apiCaseCapabilityRunState, error) {
	byCase := map[string]apiCaseCapabilityRunState{}
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		for j := len(caseRuns) - 1; j >= 0; j-- {
			caseRun := caseRuns[j]
			state := byCase[caseRun.CaseID]
			state.Count++
			if state.Latest == nil {
				state.Latest = interfaceNodeRunItem(run, caseRun, nil, 0)
			}
			byCase[caseRun.CaseID] = state
		}
	}
	return byCase, nil
}
