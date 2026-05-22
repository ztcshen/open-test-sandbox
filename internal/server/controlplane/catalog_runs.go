package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

const ReadModelCatalog = "catalog"

func CatalogReadModel(catalog store.ProfileCatalog, configVersionID string, generatedAt time.Time) (store.ReadModel, error) {
	payload := catalogPayloadFromStoreCatalog(catalog, map[string]catalogWorkflowRunState{})
	payload.Source = map[string]string{"kind": "read-model", "id": catalog.ProfileID}
	raw, err := json.Marshal(payload)
	if err != nil {
		return store.ReadModel{}, err
	}
	return store.ReadModel{
		ProfileID:       catalog.ProfileID,
		Key:             ReadModelCatalog,
		ConfigVersionID: configVersionID,
		PayloadJSON:     string(raw),
		GeneratedAt:     generatedAt,
		UpdatedAt:       generatedAt,
	}, nil
}

func catalogPayloadFromBundleWithStore(ctx context.Context, bundle profile.Bundle, runtime store.Store) (catalogPayload, error) {
	if runtime == nil {
		return catalogPayloadFromBundle(bundle), nil
	}
	runs, err := catalogRunHeaders(ctx, runtime)
	if err != nil {
		return catalogPayload{}, err
	}
	byWorkflow := catalogWorkflowRuns(runs)

	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return catalogPayload{}, err
	}
	if err == nil && catalogHasAnyState(catalog) {
		return catalogPayloadFromStoreCatalog(catalog, byWorkflow), nil
	}

	payload := catalogPayloadFromBundle(bundle)
	for i := range payload.Workflows {
		state := byWorkflow[payload.Workflows[i].ID]
		payload.Workflows[i].RunCount = state.Count
		payload.Workflows[i].LatestRun = state.Latest
	}
	return payload, nil
}

func catalogPayloadFromReadModel(ctx context.Context, runtime store.Store, profileID string) (catalogPayload, bool, error) {
	model, err := runtime.GetReadModel(ctx, profileID, ReadModelCatalog)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return catalogPayload{}, false, nil
		}
		return catalogPayload{}, false, err
	}
	var payload catalogPayload
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		return catalogPayload{}, false, err
	}
	payload.Source = map[string]string{"kind": "read-model", "id": profileID}
	return payload, true, nil
}

func hydrateCatalogPayloadRuns(payload *catalogPayload, byWorkflow map[string]catalogWorkflowRunState) {
	for index := range payload.Workflows {
		state := byWorkflow[payload.Workflows[index].ID]
		payload.Workflows[index].RunCount = state.Count
		payload.Workflows[index].LatestRun = state.Latest
	}
}

func catalogHasWorkflowDirectory(catalog store.ProfileCatalog) bool {
	return len(catalog.WorkflowBindings) > 0 || len(catalog.Workflows) > 0 || len(catalog.InterfaceNodes) > 0
}

func catalogHasAnyState(catalog store.ProfileCatalog) bool {
	return catalogHasWorkflowDirectory(catalog) ||
		len(catalog.Services) > 0 ||
		len(catalog.APICases) > 0 ||
		len(catalog.RequestTemplates) > 0 ||
		len(catalog.InterfaceFields) > 0 ||
		len(catalog.CaseDependencies) > 0 ||
		len(catalog.Fixtures) > 0 ||
		len(catalog.TemplateConfigs) > 0
}

func catalogPayloadFromStoreCatalog(catalog store.ProfileCatalog, byWorkflow map[string]catalogWorkflowRunState) catalogPayload {
	services, serviceIDs := catalogServicesFromStore(catalog)
	workflows := catalogWorkflowsFromStore(catalog, byWorkflow)
	apiCases := catalogAPICasesFromStore(catalog)
	topology := catalogTopologyFromWorkflows(serviceIDs, workflows)

	return catalogPayload{
		SchemaVersion: "1",
		OK:            true,
		GeneratedAt:   time.Now().UTC(),
		Navigation:    map[string]any{},
		Warnings:      []string{},
		Source: map[string]string{
			"kind": "store",
			"id":   catalog.ProfileID,
		},
		Presentation: catalogPresentationFromStoreConfigs(catalog.TemplateConfigs),
		Services:     services,
		Workflows:    workflows,
		APICases:     apiCases,
		Topology:     topology,
	}
}

func catalogPresentationFromStoreConfigs(configs []store.CatalogTemplateConfig) *catalogPresentation {
	var presentation catalogPresentation
	for _, config := range configs {
		if !visibleTemplateConfigStatus(config.Status) || config.ScopeType != "workflow-directory" {
			continue
		}
		if config.ScopeID != "" && config.ScopeID != "_default" {
			continue
		}
		mergeCatalogWorkflowFinder(&presentation, jsonObject(config.ConfigJSON))
	}
	if presentation.WorkflowFinder == nil {
		return nil
	}
	return &presentation
}

func catalogServicesFromStore(catalog store.ProfileCatalog) ([]catalogService, []string) {
	services := make([]catalogService, 0, len(catalog.Services))
	seen := map[string]bool{}
	for _, service := range catalog.Services {
		if service.ID == "" || seen[service.ID] {
			continue
		}
		seen[service.ID] = true
		services = append(services, catalogService{
			ID:           service.ID,
			DisplayName:  service.DisplayName,
			Role:         firstNonEmpty(service.Kind, "service"),
			Dependencies: []string{},
		})
	}
	for _, node := range catalog.InterfaceNodes {
		if node.ServiceID == "" || seen[node.ServiceID] {
			continue
		}
		seen[node.ServiceID] = true
		services = append(services, catalogService{
			ID:           node.ServiceID,
			DisplayName:  node.ServiceID,
			Role:         "service",
			Dependencies: []string{},
		})
	}
	serviceIDs := make([]string, 0, len(services))
	for _, service := range services {
		serviceIDs = append(serviceIDs, service.ID)
	}
	return services, serviceIDs
}

func catalogWorkflowsFromStore(catalog store.ProfileCatalog, byWorkflow map[string]catalogWorkflowRunState) []catalogWorkflow {
	bindingsByWorkflow := map[string][]store.CatalogWorkflowBinding{}
	for _, binding := range catalog.WorkflowBindings {
		if binding.WorkflowID == "" {
			continue
		}
		bindingsByWorkflow[binding.WorkflowID] = append(bindingsByWorkflow[binding.WorkflowID], binding)
	}
	for workflowID := range bindingsByWorkflow {
		sort.SliceStable(bindingsByWorkflow[workflowID], func(i int, j int) bool {
			left := bindingsByWorkflow[workflowID][i]
			right := bindingsByWorkflow[workflowID][j]
			if left.SortOrder != right.SortOrder {
				return left.SortOrder < right.SortOrder
			}
			return left.StepID < right.StepID
		})
	}

	workflowIDs := make([]string, 0, len(bindingsByWorkflow))
	if len(bindingsByWorkflow) > 0 {
		for workflowID := range bindingsByWorkflow {
			workflowIDs = append(workflowIDs, workflowID)
		}
	} else {
		for _, workflow := range catalog.Workflows {
			if workflow.ID != "" {
				workflowIDs = append(workflowIDs, workflow.ID)
			}
		}
	}
	sort.Strings(workflowIDs)

	workflowByID := map[string]store.CatalogWorkflow{}
	for _, workflow := range catalog.Workflows {
		workflowByID[workflow.ID] = workflow
	}
	nodeByID := map[string]store.CatalogInterfaceNode{}
	for _, node := range catalog.InterfaceNodes {
		nodeByID[node.ID] = node
	}
	caseByID := map[string]store.CatalogAPICase{}
	for _, item := range catalog.APICases {
		caseByID[item.ID] = item
	}
	workflowConfigByID := workflowTemplateConfigs(catalog.TemplateConfigs)
	stepConfigByWorkflow := stepTemplateConfigs(catalog.TemplateConfigs)

	workflows := make([]catalogWorkflow, 0, len(workflowIDs))
	for _, workflowID := range workflowIDs {
		workflow := workflowByID[workflowID]
		workflowConfig := workflowConfigByID[workflowID]
		stepConfigByID := stepConfigByWorkflow[workflowID]
		steps := make([]catalogWorkflowStep, 0, len(bindingsByWorkflow[workflowID]))
		services := map[string]bool{}
		for _, binding := range bindingsByWorkflow[workflowID] {
			node := nodeByID[binding.NodeID]
			item := caseByID[binding.CaseID]
			stepConfig := stepConfigByID[binding.StepID]
			stepConfigJSON := jsonObject(stepConfig.ConfigJSON)
			stepID := firstNonEmpty(binding.StepID, binding.NodeID, binding.CaseID)
			serviceID := firstNonEmpty(valueString(stepConfigJSON["serviceId"]), stepConfig.NodeID, node.ServiceID)
			if serviceID != "" {
				services[serviceID] = true
			}
			steps = append(steps, catalogWorkflowStep{
				ID:                 stepID,
				DisplayName:        firstNonEmpty(stepConfig.Title, item.DisplayName, node.DisplayName, binding.StepID),
				ServiceID:          serviceID,
				CaseID:             firstNonEmpty(binding.CaseID, valueString(stepConfigJSON["caseId"])),
				Action:             firstNonEmpty(valueString(stepConfigJSON["action"]), item.CaseType, node.Operation, item.DisplayName),
				Required:           binding.Required,
				Executable:         true,
				EvidenceKinds:      stringListFromAny(stepConfigJSON["evidenceKinds"]),
				RelatedMockTargets: stringListFromAny(stepConfigJSON["relatedMockTargets"]),
				Inputs:             mapListFromAny(stepConfigJSON["inputs"]),
				Exports:            mapListFromAny(stepConfigJSON["exports"]),
				TimeoutMs:          node.TimeoutMs,
				Presentation:       catalogStepPresentationForStore(stepConfigByID, stepID),
			})
		}
		workflowConfigJSON := jsonObject(workflowConfig.ConfigJSON)
		baseStepTimeoutMs := firstPositiveInt(intValue(workflowConfigJSON["baseStepTimeoutMs"]), workflow.BaseStepTimeoutMs)
		timeoutOffsetMs := firstPositiveInt(intValue(workflowConfigJSON["timeoutOffsetMs"]), workflow.TimeoutOffsetMs)
		state := byWorkflow[workflowID]
		workflows = append(workflows, catalogWorkflow{
			ID:                workflowID,
			DisplayName:       firstNonEmpty(workflowConfig.Title, workflow.DisplayName, workflowID),
			Description:       firstNonEmpty(workflowConfig.Description, workflow.Description),
			Entrypoint:        "/workflow-detail.html?id=" + url.QueryEscape(workflowID),
			BaseStepTimeoutMs: baseStepTimeoutMs,
			TimeoutOffsetMs:   timeoutOffsetMs,
			TimeoutMs:         workflowBudgetMs(baseStepTimeoutMs, timeoutOffsetMs, steps),
			Steps:             steps,
			StepCount:         len(steps),
			CaseCount:         workflowCaseCount(steps),
			ServiceCount:      len(services),
			Graph:             catalogGraphFromTemplateConfigs(catalog.TemplateConfigs),
			Observability: catalogWorkflowObservability{
				Panels: defaultWorkflowObservabilityPanels(),
			},
			Presentation: catalogWorkflowPresentationForStore(firstNonEmpty(workflowConfig.Title, workflow.DisplayName, workflowID), steps, stringMapFromAny(workflowConfigJSON["copy"])),
			RunCount:     state.Count,
			LatestRun:    catalogWorkflowLatestRun(state, len(steps)),
		})
	}
	return workflows
}

func workflowBudgetMs(baseStepTimeoutMs int, timeoutOffsetMs int, steps []catalogWorkflowStep) int {
	if baseStepTimeoutMs <= 0 {
		baseStepTimeoutMs = 3000
	}
	total := timeoutOffsetMs
	for _, step := range steps {
		if step.TimeoutMs > 0 {
			total += step.TimeoutMs
		} else {
			total += baseStepTimeoutMs
		}
	}
	if total < 0 {
		return 0
	}
	return total
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func workflowTemplateConfigs(items []store.CatalogTemplateConfig) map[string]store.CatalogTemplateConfig {
	out := map[string]store.CatalogTemplateConfig{}
	for _, item := range items {
		if !visibleTemplateConfigStatus(item.Status) || item.ScopeType != "workflow" || item.ScopeID == "" {
			continue
		}
		out[item.ScopeID] = item
	}
	return out
}

func stepTemplateConfigs(items []store.CatalogTemplateConfig) map[string]map[string]store.CatalogTemplateConfig {
	out := map[string]map[string]store.CatalogTemplateConfig{}
	for _, item := range items {
		if !visibleTemplateConfigStatus(item.Status) || item.ScopeType != "step" || item.WorkflowID == "" || item.ScopeID == "" {
			continue
		}
		if out[item.WorkflowID] == nil {
			out[item.WorkflowID] = map[string]store.CatalogTemplateConfig{}
		}
		out[item.WorkflowID][item.ScopeID] = item
	}
	return out
}

func catalogGraphFromTemplateConfigs(items []store.CatalogTemplateConfig) catalogTopology {
	edges := []catalogEdge{}
	nodes := map[string]bool{}
	seen := map[string]bool{}
	for _, item := range items {
		if !visibleTemplateConfigStatus(item.Status) || item.ScopeType != "topology-edge" {
			continue
		}
		raw := jsonObject(item.ConfigJSON)
		from := strings.TrimSpace(valueString(raw["from"]))
		to := strings.TrimSpace(valueString(raw["to"]))
		if from == "" || to == "" {
			continue
		}
		key := from + "\x00" + to
		if seen[key] {
			continue
		}
		seen[key] = true
		nodes[from] = true
		nodes[to] = true
		edges = append(edges, catalogEdge{From: from, To: to})
	}
	nodeList := make([]string, 0, len(nodes))
	for node := range nodes {
		nodeList = append(nodeList, node)
	}
	sort.Strings(nodeList)
	return catalogTopology{Nodes: nodeList, Edges: edges}
}

func workflowCaseCount(steps []catalogWorkflowStep) int {
	count := 0
	for _, step := range steps {
		if step.CaseID != "" {
			count++
		}
	}
	return count
}

func defaultWorkflowObservabilityPanels() []catalogWorkflowPanel {
	return []catalogWorkflowPanel{
		{ID: "workflowGraph", Title: "Workflow Graph", Type: "workflowGraph", Scope: "workflow"},
		{ID: "stepSequence", Title: "Step Sequence", Type: "stepSequence", Scope: "workflow"},
		{ID: "serviceEvidence", Title: "Service Evidence", Type: "serviceEvidence", Scope: "workflow"},
		{ID: "evidenceKinds", Title: "Evidence Kinds", Type: "evidenceKinds", Scope: "workflow"},
		{ID: "runHistory", Title: "Run History", Type: "runHistory", Scope: "workflow"},
	}
}

func catalogWorkflowPresentationForStore(title string, steps []catalogWorkflowStep, copy map[string]string) catalogWorkflowPresentation {
	stageSteps := make([]catalogWorkflowStageStep, 0, len(steps))
	for _, step := range steps {
		stageSteps = append(stageSteps, catalogWorkflowStageStep{
			ID:     step.ID,
			Title:  firstNonEmpty(step.DisplayName, step.ID),
			CaseID: step.CaseID,
		})
	}
	return catalogWorkflowPresentation{
		Kind:     workflowPresentationKind(steps),
		Template: "workflowStudio",
		Title:    title,
		Copy:     copy,
		Stages: []catalogWorkflowStage{{
			ID:      "steps",
			Title:   "Workflow Steps",
			Summary: "Generated from runtime catalog steps.",
			Steps:   stageSteps,
		}},
	}
}

func catalogStepPresentationForStore(configByStep map[string]store.CatalogTemplateConfig, stepID string) catalogStepPresentation {
	copy := map[string]string{}
	for _, config := range []store.CatalogTemplateConfig{configByStep["_default"], configByStep[stepID]} {
		if !visibleTemplateConfigStatus(config.Status) || config.ScopeType != "step" {
			continue
		}
		mergeStringMap(copy, stringMapFromAny(jsonObject(config.ConfigJSON)["copy"]))
	}
	if len(copy) == 0 {
		return catalogStepPresentation{}
	}
	return catalogStepPresentation{Copy: copy}
}

func stringMapFromAny(value any) map[string]string {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for key, item := range raw {
		text := valueString(item)
		if key != "" && text != "" {
			out[key] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringListFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(valueString(item)); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func mapListFromAny(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if value := mapFromAny(item); len(value) > 0 {
			out = append(out, value)
		}
	}
	return out
}

func visibleTemplateConfigStatus(status string) bool {
	status = strings.TrimSpace(strings.ToLower(status))
	return status == "" || (status != "inactive" && status != "deleted" && status != "disabled")
}

func catalogAPICasesFromStore(catalog store.ProfileCatalog) []catalogAPICase {
	apiCases := make([]catalogAPICase, 0, len(catalog.APICases))
	for _, item := range catalog.APICases {
		apiCases = append(apiCases, catalogAPICase{
			ID:               item.ID,
			DisplayName:      item.DisplayName,
			NodeID:           item.NodeID,
			CasePath:         item.CasePath,
			SourceKind:       item.SourceKind,
			SourcePath:       item.SourcePath,
			ExecutorID:       item.ExecutorID,
			BaseURL:          item.BaseURL,
			EvidenceDir:      item.EvidenceDir,
			TimeoutSeconds:   item.TimeoutSeconds,
			DefaultOverrides: jsonObject(item.DefaultOverridesJSON),
		})
	}
	return apiCases
}

func catalogTopologyFromWorkflows(serviceIDs []string, workflows []catalogWorkflow) catalogTopology {
	edgeSeen := map[string]bool{}
	edges := []catalogEdge{}
	for _, workflow := range workflows {
		lastServiceID := ""
		for _, step := range workflow.Steps {
			if step.ServiceID == "" {
				continue
			}
			if lastServiceID != "" && lastServiceID != step.ServiceID {
				key := lastServiceID + "\x00" + step.ServiceID
				if !edgeSeen[key] {
					edgeSeen[key] = true
					edges = append(edges, catalogEdge{From: lastServiceID, To: step.ServiceID})
				}
			}
			lastServiceID = step.ServiceID
		}
	}
	return catalogTopology{
		Nodes: serviceIDs,
		Edges: edges,
	}
}

type catalogWorkflowRunState struct {
	Count  int
	Latest map[string]any
	Runs   []store.Run
}

func catalogWorkflowRuns(runs []store.Run) map[string]catalogWorkflowRunState {
	byWorkflow := map[string]catalogWorkflowRunState{}
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if isAPICaseRunHeader(run) {
			continue
		}
		state := byWorkflow[run.WorkflowID]
		state.Count++
		if state.Latest == nil {
			state.Latest = workflowRunCatalogItem(run)
		}
		state.Runs = append(state.Runs, run)
		byWorkflow[run.WorkflowID] = state
	}
	return byWorkflow
}

func catalogWorkflowLatestRun(state catalogWorkflowRunState, expectedStepCount int) map[string]any {
	if expectedStepCount <= 0 {
		return state.Latest
	}
	for _, run := range state.Runs {
		if workflowRunHeaderStepCount(run) >= expectedStepCount {
			return workflowRunCatalogItem(run)
		}
	}
	return state.Latest
}

func workflowRunHeaderStepCount(run store.Run) int {
	summary, err := workflowRunSummary(run.SummaryJSON)
	if err != nil {
		return 0
	}
	if steps := mapListFromAny(summary["steps"]); len(steps) > 0 {
		return len(steps)
	}
	nested := mapFromAny(summary["summary"])
	return intFromAny(nested["stepCount"])
}

func isAPICaseRunHeader(run store.Run) bool {
	summary, err := workflowRunSummary(run.SummaryJSON)
	if err != nil {
		return false
	}
	if valueString(summary["kind"]) == "apiCase" {
		return true
	}
	nested := mapFromAny(summary["summary"])
	return valueString(nested["caseId"]) != "" && intFromAny(nested["expectedStepCount"]) == 0
}

type runHeaderStore interface {
	ListRunHeaders(context.Context) ([]store.Run, error)
}

func catalogRunHeaders(ctx context.Context, runtime store.Store) ([]store.Run, error) {
	if fast, ok := runtime.(runHeaderStore); ok {
		return fast.ListRunHeaders(ctx)
	}
	return runtime.ListRuns(ctx)
}
