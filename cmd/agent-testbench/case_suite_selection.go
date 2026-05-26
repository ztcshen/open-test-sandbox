package main

import (
	"context"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

type selectedCaseSuite struct {
	Bundle         profile.Bundle
	Store          store.Store
	SourceStoreURL string
	Filters        caseListFilter
	Cases          []profile.APICase
	cleanup        func()
}

func loadSelectedCaseSuite(ctx context.Context, selection *caseSelectionCLIFlags) (selectedCaseSuite, error) {
	bundle, sourceStore, sourceStoreURL, cleanup, err := selection.loadRequiredBundle(ctx)
	if err != nil {
		return selectedCaseSuite{}, err
	}
	filters := selection.caseListFilter()
	return selectedCaseSuite{
		Bundle:         bundle,
		Store:          sourceStore,
		SourceStoreURL: sourceStoreURL,
		Filters:        filters,
		Cases:          selectedCaseSuiteCases(bundle, filters),
		cleanup:        cleanup,
	}, nil
}

func (s selectedCaseSuite) Close() {
	if s.cleanup != nil {
		s.cleanup()
	}
}
