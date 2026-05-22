package controlplane

import (
	"context"
	"fmt"
	"time"

	"open-test-sandbox/internal/domain/profilecatalog"
	"open-test-sandbox/internal/store"
)

func UpsertProfileReadModels(ctx context.Context, runtime store.Store, catalog store.ProfileCatalog, configVersionID string, generatedAt time.Time) ([]string, error) {
	models := []store.ReadModel{}

	interfaceNodes, err := profilecatalog.InterfaceNodesReadModel(catalog, configVersionID, generatedAt)
	if err != nil {
		return nil, fmt.Errorf("build interface nodes read model %q: %w", catalog.ProfileID, err)
	}
	models = append(models, interfaceNodes)

	catalogModel, err := CatalogReadModel(catalog, configVersionID, generatedAt)
	if err != nil {
		return nil, fmt.Errorf("build catalog read model %q: %w", catalog.ProfileID, err)
	}
	models = append(models, catalogModel)

	dashboard, err := DashboardReadModel(catalog, configVersionID, generatedAt)
	if err != nil {
		return nil, fmt.Errorf("build dashboard read model %q: %w", catalog.ProfileID, err)
	}
	models = append(models, dashboard)

	details, err := InterfaceNodeDetailReadModels(catalog, configVersionID, generatedAt)
	if err != nil {
		return nil, fmt.Errorf("build interface node detail read models %q: %w", catalog.ProfileID, err)
	}
	models = append(models, details...)

	coverage, err := InterfaceNodeCoverageReadModels(catalog, configVersionID, generatedAt)
	if err != nil {
		return nil, fmt.Errorf("build interface node coverage read models %q: %w", catalog.ProfileID, err)
	}
	models = append(models, coverage...)

	keys := make([]string, 0, len(models))
	for _, model := range models {
		if _, err := runtime.UpsertReadModel(ctx, model); err != nil {
			return nil, fmt.Errorf("store read model %q/%q: %w", catalog.ProfileID, model.Key, err)
		}
		keys = append(keys, model.Key)
	}
	return keys, nil
}
