package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

func DashboardReadModel(catalog store.ProfileCatalog, configVersionID string, generatedAt time.Time) (store.ReadModel, error) {
	payload := dashboardPayloadFromCatalog(catalog)
	payload.Source = map[string]string{"kind": "read-model", "id": catalog.ProfileID}
	raw, err := json.Marshal(payload)
	if err != nil {
		return store.ReadModel{}, err
	}
	return store.ReadModel{
		ProfileID:       catalog.ProfileID,
		Key:             ReadModelDashboard,
		ConfigVersionID: configVersionID,
		PayloadJSON:     string(raw),
		GeneratedAt:     generatedAt,
		UpdatedAt:       generatedAt,
	}, nil
}

func dashboardPayloadFromBundleWithStore(ctx context.Context, bundle profile.Bundle, runtime store.Store) (dashboardPayload, error) {
	if runtime == nil {
		return dashboardPayloadFromBundle(ctx, bundle), nil
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return dashboardPayload{}, err
	}
	if err == nil && len(catalog.Services) > 0 {
		payload, ok, err := dashboardPayloadFromReadModel(ctx, runtime, catalog.ProfileID)
		if err != nil {
			return dashboardPayload{}, err
		}
		if !ok {
			payload = dashboardPayloadFromCatalog(catalog)
		}
		hydrateDashboardRuntime(ctx, runtime, &payload, catalog)
		return payload, nil
	}
	return dashboardPayloadFromBundle(ctx, bundle), nil
}

func dashboardPayloadFromReadModel(ctx context.Context, runtime store.Store, profileID string) (dashboardPayload, bool, error) {
	model, err := runtime.GetReadModel(ctx, profileID, ReadModelDashboard)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return dashboardPayload{}, false, nil
		}
		return dashboardPayload{}, false, err
	}
	var payload dashboardPayload
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		return dashboardPayload{}, false, err
	}
	payload.Source = map[string]string{"kind": "read-model", "id": profileID}
	return payload, true, nil
}

func dashboardPayloadFromCatalog(catalog store.ProfileCatalog) dashboardPayload {
	services := activeCatalogServices(catalog.Services)
	items := make([]dashboardItem, 0, len(services))
	serviceRuntimes := make([]serviceRuntime, 0, len(services))
	for _, service := range services {
		state := "missing"
		health := "unknown"
		healthy := false
		if strings.EqualFold(service.Kind, "external") {
			state = "external"
			health = "external"
			healthy = true
		}
		runtime := serviceRuntimeFromCatalogService(service, state, health, healthy)
		if runtime.ServiceID != "" {
			serviceRuntimes = append(serviceRuntimes, runtime)
		}
		items = append(items, dashboardItemFromCatalogService(catalog, service, runtime, state, health, healthy))
	}
	return dashboardPayload{
		OK:             true,
		Source:         map[string]string{"kind": "store", "id": catalog.ProfileID},
		Summary:        dashboardSummaryForItems(items),
		Groups:         []dashboardGroup{{ID: "business", Label: "Services", DisplayName: "Services", Items: items}},
		ServiceRuntime: serviceRuntimes,
		Presentation:   dashboardPresentationForCatalog(catalog.TemplateConfigs, ""),
	}
}

func hydrateDashboardRuntime(ctx context.Context, runtime store.Store, payload *dashboardPayload, catalog store.ProfileCatalog) {
	services := activeCatalogServices(catalog.Services)
	filterDashboardPayloadServices(payload, services)
	dockerRuntimes := dockerRuntimeByCatalogService(ctx, services)
	componentHealthURLByService := dashboardComponentHealthURLByService(ctx, runtime, services)
	runtimeByService := map[string]serviceRuntime{}
	for _, runtime := range payload.ServiceRuntime {
		runtimeByService[runtime.ServiceID] = runtime
	}
	for _, service := range services {
		runtime := runtimeByService[service.ID]
		if observed, ok := dockerRuntimes[service.ID]; ok {
			runtime = mergeRuntime(runtime, observed)
		}
		runtime = applyHTTPServiceHealth(ctx, runtime, firstNonEmpty(componentHealthURLByService[service.ID], service.HealthURL))
		runtimeByService[service.ID] = runtime
	}
	payload.ServiceRuntime = make([]serviceRuntime, 0, len(services))
	for groupIndex := range payload.Groups {
		for itemIndex := range payload.Groups[groupIndex].Items {
			item := &payload.Groups[groupIndex].Items[itemIndex]
			service := catalogServiceByID(services, item.ID)
			runtime := runtimeByService[item.ID]
			if runtime.ServiceID == "" {
				continue
			}
			state := runtime.State
			health := runtime.Health
			healthy := runtime.OK
			*item = dashboardItemFromCatalogService(catalog, service, runtime, state, health, healthy)
		}
	}
	for _, service := range services {
		if runtime := runtimeByService[service.ID]; runtime.ServiceID != "" {
			payload.ServiceRuntime = append(payload.ServiceRuntime, runtime)
		}
	}
	allItems := []dashboardItem{}
	for _, group := range payload.Groups {
		allItems = append(allItems, group.Items...)
	}
	payload.Summary = dashboardSummaryForItems(allItems)
}

func dashboardComponentHealthURLByService(ctx context.Context, runtime store.Store, services []store.CatalogService) map[string]string {
	if runtime == nil || len(services) == 0 {
		return nil
	}
	serviceIDs := make(map[string]bool, len(services))
	for _, service := range services {
		serviceIDs[strings.TrimSpace(service.ID)] = true
	}
	envs, err := runtime.ListEnvironments(ctx)
	if err != nil {
		return nil
	}
	bestScore := 0
	best := map[string]string{}
	for _, env := range envs {
		graph, err := runtime.GetEnvironmentComponentGraph(ctx, env.ID)
		if err != nil {
			continue
		}
		score := 0
		urls := map[string]string{}
		for _, component := range graph.Components {
			id := strings.TrimSpace(component.ComponentID)
			if !serviceIDs[id] {
				continue
			}
			score++
			check, errText := normalizeEnvironmentComponentHealthCheck(component)
			if errText != "" {
				continue
			}
			if strings.TrimSpace(valueString(check["kind"])) == "url" {
				urls[id] = strings.TrimSpace(valueString(check["url"]))
			}
		}
		if score > bestScore {
			bestScore = score
			best = urls
		}
	}
	if bestScore == 0 {
		return nil
	}
	return best
}

func activeCatalogServices(services []store.CatalogService) []store.CatalogService {
	out := make([]store.CatalogService, 0, len(services))
	for _, service := range services {
		if catalogServiceActive(service) {
			out = append(out, service)
		}
	}
	return out
}

func catalogServiceActive(service store.CatalogService) bool {
	status := strings.TrimSpace(service.Status)
	return status == "" || strings.EqualFold(status, "active")
}

func filterDashboardPayloadServices(payload *dashboardPayload, services []store.CatalogService) {
	activeByID := make(map[string]bool, len(services))
	for _, service := range services {
		activeByID[service.ID] = true
	}
	for groupIndex := range payload.Groups {
		items := payload.Groups[groupIndex].Items[:0]
		for _, item := range payload.Groups[groupIndex].Items {
			if activeByID[item.ID] {
				items = append(items, item)
			}
		}
		payload.Groups[groupIndex].Items = items
	}
	runtimes := payload.ServiceRuntime[:0]
	for _, runtime := range payload.ServiceRuntime {
		if activeByID[runtime.ServiceID] {
			runtimes = append(runtimes, runtime)
		}
	}
	payload.ServiceRuntime = runtimes
}

func dashboardItemFromCatalogService(catalog store.ProfileCatalog, service store.CatalogService, runtime serviceRuntime, state string, health string, healthy bool) dashboardItem {
	return dashboardItem{
		ID:             service.ID,
		Name:           firstNonEmpty(service.DisplayName, service.ID),
		DisplayName:    service.DisplayName,
		State:          firstNonEmpty(state, "missing"),
		Health:         firstNonEmpty(health, "unknown"),
		Kind:           service.Kind,
		OK:             healthy,
		Branch:         catalog.ProfileID,
		Profile:        catalog.ProfileID,
		Container:      firstNonEmpty(runtime.Container, service.ContainerName),
		Image:          firstNonEmpty(runtime.Image, service.Image),
		Port:           firstPositiveInt(runtime.Port, service.ServicePort),
		ManagementPort: firstPositiveInt(runtime.ManagementPort, service.ManagementPort),
		Message:        runtime.Message,
		Presentation:   dashboardPresentationForCatalog(catalog.TemplateConfigs, service.ID),
	}
}

func serviceRuntimeFromCatalogService(service store.CatalogService, state string, health string, ok bool) serviceRuntime {
	branchName, commitID := sourceSnapshotRevision(service.SourcePath)
	return serviceRuntime{
		ServiceID:      service.ID,
		NodeRole:       service.Kind,
		Container:      service.ContainerName,
		Image:          service.Image,
		SourcePath:     service.SourcePath,
		BranchName:     firstNonEmpty(service.GitBranch, branchName),
		CommitID:       commitID,
		State:          firstNonEmpty(state, "missing"),
		Health:         firstNonEmpty(health, "unknown"),
		OK:             ok,
		Port:           service.ServicePort,
		ManagementPort: service.ManagementPort,
	}
}

func dashboardSummaryForItems(items []dashboardItem) dashboardSummary {
	healthy, missing, unhealthy := 0, 0, 0
	for _, item := range items {
		if item.OK {
			healthy++
			continue
		}
		if item.State == "missing" {
			missing++
			continue
		}
		unhealthy++
	}
	return dashboardSummary{Total: len(items), Healthy: healthy, Missing: missing, Unhealthy: unhealthy}
}

func dashboardPresentationForCatalog(configs []store.CatalogTemplateConfig, serviceID string) dashboardPresentation {
	copy := map[string]string{}
	for _, config := range configs {
		if !visibleTemplateConfigStatus(config.Status) {
			continue
		}
		configCopy := stringMapFromAny(jsonObject(config.ConfigJSON)["copy"])
		if len(configCopy) == 0 {
			continue
		}
		switch {
		case config.ScopeType == "environment":
			mergeStringMap(copy, configCopy)
		case config.ScopeType == "environment-node" && config.NodeID == "" && (config.ScopeID == "" || config.ScopeID == "_default"):
			mergeStringMap(copy, configCopy)
		case config.ScopeType == "environment-node" && serviceID != "" && (config.NodeID == serviceID || config.ScopeID == serviceID):
			mergeStringMap(copy, configCopy)
		}
	}
	if len(copy) == 0 {
		return dashboardPresentation{}
	}
	return dashboardPresentation{Copy: copy}
}

func catalogServiceByID(services []store.CatalogService, id string) store.CatalogService {
	for _, service := range services {
		if service.ID == id {
			return service
		}
	}
	return store.CatalogService{ID: id}
}

func dashboardPayloadFromBundle(ctx context.Context, bundle profile.Bundle) dashboardPayload {
	dockerRuntimes := dockerRuntimeByService(ctx, bundle.Services)
	configuredRuntimes := configuredRuntimeByService(ctx, bundle)
	items := make([]dashboardItem, 0, len(bundle.Services))
	serviceRuntimes := make([]serviceRuntime, 0, len(bundle.Services))
	for _, service := range bundle.Services {
		runtime := configuredRuntimes[service.ID]
		dockerRuntime, ok := dockerRuntimes[service.ID]
		state := "missing"
		health := "unknown"
		healthy := false
		if ok {
			runtime = mergeRuntime(runtime, dockerRuntime)
			state = dockerRuntime.State
			health = dockerRuntime.Health
			healthy = dockerRuntime.OK
		} else if strings.EqualFold(service.Kind, "external") {
			runtime = serviceRuntime{
				ServiceID: service.ID,
				NodeRole:  service.Kind,
				State:     "external",
				Health:    "external",
				OK:        true,
			}
			runtime = applyHTTPServiceHealth(ctx, runtime, service.HealthURL)
			state = "external"
			health = runtime.Health
			healthy = runtime.OK
		}
		if runtime.ServiceID != "" {
			serviceRuntimes = append(serviceRuntimes, runtime)
		}
		items = append(items, dashboardItem{
			ID:             service.ID,
			Name:           firstNonEmpty(service.DisplayName, service.ID),
			DisplayName:    service.DisplayName,
			State:          state,
			Health:         health,
			Kind:           service.Kind,
			OK:             healthy,
			Branch:         bundle.ID,
			Profile:        bundle.ID,
			Container:      firstNonEmpty(runtime.Container, service.ContainerName),
			Image:          firstNonEmpty(runtime.Image, service.Image),
			Port:           firstPositiveInt(runtime.Port, service.ServicePort),
			ManagementPort: firstPositiveInt(runtime.ManagementPort, service.ManagementPort),
			Message:        runtime.Message,
		})
	}
	healthy, missing, unhealthy := 0, 0, 0
	for _, item := range items {
		if item.OK {
			healthy++
			continue
		}
		if item.State == "missing" {
			missing++
			continue
		}
		unhealthy++
	}
	return dashboardPayload{
		OK:     true,
		Source: map[string]string{"kind": "profile", "id": bundle.ID},
		Summary: dashboardSummary{
			Total:     len(items),
			Healthy:   healthy,
			Missing:   missing,
			Unhealthy: unhealthy,
		},
		Groups: []dashboardGroup{{
			ID:          "business",
			Label:       "Services",
			DisplayName: "Services",
			Items:       items,
		}},
		ServiceRuntime: serviceRuntimes,
	}
}
