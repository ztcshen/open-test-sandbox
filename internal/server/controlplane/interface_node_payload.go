package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/domain/profilecatalog"
	"agent-testbench/internal/store"
)

type interfaceNodesPayload struct {
	OK           bool                      `json:"ok"`
	TemplateID   string                    `json:"templateId"`
	Filters      map[string]string         `json:"filters"`
	Source       map[string]string         `json:"source"`
	Items        []interfaceNodeItem       `json:"items"`
	Presentation interfaceNodePresentation `json:"presentation,omitempty"`
}

type interfaceNodeItem struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"displayName,omitempty"`
	ServiceID            string `json:"serviceId,omitempty"`
	Operation            string `json:"operation,omitempty"`
	Method               string `json:"method,omitempty"`
	Path                 string `json:"path,omitempty"`
	Href                 string `json:"href"`
	Status               string `json:"status"`
	AdmissionStatus      string `json:"admissionStatus"`
	ValidationStatus     string `json:"validationStatus"`
	ValidationIssueCount int    `json:"validationIssueCount"`
	RequiredCaseCount    int    `json:"requiredCaseCount"`
	PassedCaseCount      int    `json:"passedCaseCount"`
	TimeoutMs            int    `json:"timeoutMs,omitempty"`
	LatestRunID          string `json:"latestRunId,omitempty"`
	LatestElapsedMs      int64  `json:"latestElapsedMs,omitempty"`
	TotalElapsedMs       int64  `json:"totalElapsedMs,omitempty"`
}

type interfaceNodeDetailPayload struct {
	OK               bool                       `json:"ok"`
	TemplateID       string                     `json:"templateId"`
	Source           map[string]string          `json:"source"`
	Context          map[string]string          `json:"context,omitempty"`
	Error            string                     `json:"error,omitempty"`
	Requested        string                     `json:"requested,omitempty"`
	Available        []interfaceNodeItem        `json:"available,omitempty"`
	Attention        map[string]any             `json:"attention,omitempty"`
	Node             interfaceNodeDetail        `json:"node,omitempty"`
	Admission        interfaceNodeAdmission     `json:"admission,omitempty"`
	RequestTemplates []interfaceRequestTemplate `json:"requestTemplates"`
	Cases            []interfaceCase            `json:"cases"`
	Fields           interfaceNodeFields        `json:"fields"`
	History          map[string]any             `json:"history"`
	Runs             []map[string]any           `json:"runs"`
	Presentation     interfaceNodePresentation  `json:"presentation,omitempty"`
}

type interfaceNodePresentation struct {
	Copy map[string]string `json:"copy,omitempty"`
}

type interfaceNodeDetail struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName,omitempty"`
	ServiceID   string   `json:"serviceId,omitempty"`
	Operation   string   `json:"operation,omitempty"`
	Method      string   `json:"method,omitempty"`
	Path        string   `json:"path,omitempty"`
	TimeoutMs   int      `json:"timeoutMs,omitempty"`
	TemplateID  string   `json:"templateId,omitempty"`
	Version     string   `json:"version,omitempty"`
	Status      string   `json:"status,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Description string   `json:"description,omitempty"`
	SortOrder   int      `json:"sortOrder,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
}

type interfaceNodeAdmission struct {
	Status            string           `json:"status"`
	RequiredCaseCount int              `json:"requiredCaseCount"`
	PassedCaseCount   int              `json:"passedCaseCount"`
	LatestRunID       string           `json:"latestRunId,omitempty"`
	Blockers          []map[string]any `json:"blockers"`
}

type interfaceRequestTemplate struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	Version      string `json:"version,omitempty"`
	Status       string `json:"status,omitempty"`
	Method       string `json:"method,omitempty"`
	Path         string `json:"path,omitempty"`
	TemplateJSON string `json:"templateJson,omitempty"`
}

type interfaceCase struct {
	ID                   string           `json:"id"`
	Title                string           `json:"title,omitempty"`
	CaseType             string           `json:"caseType"`
	Blocked              bool             `json:"blocked"`
	BlockedReason        string           `json:"blockedReason"`
	Scenario             string           `json:"scenario"`
	PayloadTemplateJSON  string           `json:"payloadTemplateJson,omitempty"`
	RequestTemplateID    string           `json:"requestTemplateId"`
	PatchJSON            string           `json:"patchJson,omitempty"`
	RenderMode           string           `json:"renderMode,omitempty"`
	ExpectedJSON         string           `json:"expectedJson,omitempty"`
	Status               string           `json:"status,omitempty"`
	SortOrder            int              `json:"sortOrder,omitempty"`
	RequiredForAdmission bool             `json:"requiredForAdmission"`
	Dependencies         []map[string]any `json:"dependencies"`
	LatestRun            map[string]any   `json:"latestRun,omitempty"`
}

type interfaceNodeFields struct {
	Request  []map[string]any `json:"request"`
	Response []map[string]any `json:"response"`
}

func interfaceNodesPayloadFromBundle(bundle profile.Bundle, serviceID string, operation string) interfaceNodesPayload {
	items := make([]interfaceNodeItem, 0, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		if serviceID != "" && node.ServiceID != serviceID {
			continue
		}
		nodeOperation := firstNonEmpty(node.Operation, node.DisplayName, node.ID)
		if operation != "" && nodeOperation != operation {
			continue
		}
		items = append(items, interfaceNodeItem{
			ID:               node.ID,
			DisplayName:      node.DisplayName,
			ServiceID:        node.ServiceID,
			Operation:        nodeOperation,
			Href:             "/interface-node.html?id=" + node.ID,
			Status:           "pending",
			AdmissionStatus:  "pending",
			ValidationStatus: "valid",
			TimeoutMs:        node.TimeoutMs,
		})
	}
	return interfaceNodesPayload{
		OK:         true,
		TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Filters:    map[string]string{"serviceId": serviceID, "operation": operation},
		Source:     map[string]string{"kind": "profile", "id": bundle.ID},
		Items:      items,
	}
}

func interfaceNodesPayloadFromStore(ctx context.Context, catalog store.ProfileCatalog, runtime store.Store, serviceID string, operation string) (interfaceNodesPayload, error) {
	payload, ok, err := interfaceNodesPayloadFromReadModel(ctx, runtime, catalog.ProfileID, serviceID, operation)
	if err != nil {
		return interfaceNodesPayload{}, err
	}
	if !ok {
		payload = interfaceNodesBasePayloadFromCatalog(catalog, serviceID, operation)
	}
	if err := hydrateInterfaceNodesPayload(ctx, catalog, runtime, &payload); err != nil {
		return interfaceNodesPayload{}, err
	}
	if len(payload.Presentation.Copy) == 0 {
		payload.Presentation = interfaceNodeDirectoryPresentationForCatalog(catalog.TemplateConfigs)
	}
	return payload, nil
}

func interfaceNodesPayloadFromReadModel(ctx context.Context, runtime store.Store, profileID string, serviceID string, operation string) (interfaceNodesPayload, bool, error) {
	model, err := runtime.GetReadModel(ctx, profileID, profilecatalog.ReadModelInterfaceNodes)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return interfaceNodesPayload{}, false, nil
		}
		return interfaceNodesPayload{}, false, err
	}
	var payload interfaceNodesPayload
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		return interfaceNodesPayload{}, false, err
	}
	payload.Filters = map[string]string{"serviceId": serviceID, "operation": operation}
	payload.Source = map[string]string{"kind": "read-model", "id": profileID}
	if serviceID == "" && operation == "" {
		return payload, true, nil
	}
	filtered := payload.Items[:0]
	for _, item := range payload.Items {
		if serviceID != "" && item.ServiceID != serviceID {
			continue
		}
		if operation != "" && item.Operation != operation {
			continue
		}
		filtered = append(filtered, item)
	}
	payload.Items = filtered
	return payload, true, nil
}

func applyInterfaceNodeSearchFilter(payload *interfaceNodesPayload, filter string) {
	filter = strings.TrimSpace(filter)
	if payload.Filters == nil {
		payload.Filters = map[string]string{}
	}
	payload.Filters["filter"] = filter
	if filter == "" {
		return
	}
	filtered := payload.Items[:0]
	for _, item := range payload.Items {
		if matchesControlplaneDiscoveryFilter(filter, item.ID, item.DisplayName, item.Operation, item.Method, item.Path, item.ServiceID) {
			filtered = append(filtered, item)
		}
	}
	payload.Items = filtered
}

func interfaceNodesBasePayloadFromCatalog(catalog store.ProfileCatalog, serviceID string, operation string) interfaceNodesPayload {
	items := make([]interfaceNodeItem, 0, len(catalog.InterfaceNodes))
	for _, node := range catalog.InterfaceNodes {
		if serviceID != "" && node.ServiceID != serviceID {
			continue
		}
		nodeOperation := firstNonEmpty(node.Operation, node.DisplayName, node.ID)
		if operation != "" && nodeOperation != operation {
			continue
		}
		cases := catalogCasesForNode(catalog.APICases, node.ID)
		required := 0
		for _, item := range cases {
			if item.RequiredForAdmission {
				required++
			}
		}
		items = append(items, interfaceNodeItem{
			ID:                   node.ID,
			DisplayName:          node.DisplayName,
			ServiceID:            node.ServiceID,
			Operation:            nodeOperation,
			Method:               node.Method,
			Path:                 node.Path,
			Href:                 "/interface-node.html?id=" + node.ID,
			Status:               firstNonEmpty(node.Status, "draft"),
			AdmissionStatus:      "pending",
			ValidationStatus:     "valid",
			ValidationIssueCount: 0,
			RequiredCaseCount:    required,
			PassedCaseCount:      0,
			TimeoutMs:            node.TimeoutMs,
		})
	}
	return interfaceNodesPayload{
		OK:           true,
		TemplateID:   "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Filters:      map[string]string{"serviceId": serviceID, "operation": operation},
		Source:       map[string]string{"kind": "store", "id": catalog.ProfileID},
		Items:        items,
		Presentation: interfaceNodeDirectoryPresentationForCatalog(catalog.TemplateConfigs),
	}
}

func hydrateInterfaceNodesPayload(ctx context.Context, catalog store.ProfileCatalog, runtime store.Store, payload *interfaceNodesPayload) error {
	latest, err := preferredCaseStates(ctx, catalog, runtime)
	if err != nil {
		return err
	}
	for index := range payload.Items {
		cases := catalogCasesForNode(catalog.APICases, payload.Items[index].ID)
		passed, failed, missing := 0, 0, 0
		required := 0
		latestRunID := ""
		latestElapsedMs := int64(0)
		latestObservedAt := time.Time{}
		latestRequiredRunID := ""
		latestRequiredElapsedMs := int64(0)
		latestRequiredObservedAt := time.Time{}
		totalElapsedMs := int64(0)
		for _, item := range cases {
			state := latest[item.ID]
			if state.RunID != "" && (latestRunID == "" || state.ObservedAt.After(latestObservedAt)) {
				latestRunID = state.RunID
				latestElapsedMs = state.ElapsedMs
				latestObservedAt = state.ObservedAt
			}
			if !item.RequiredForAdmission {
				continue
			}
			required++
			if state.RunID != "" && (latestRequiredRunID == "" || state.ObservedAt.After(latestRequiredObservedAt)) {
				latestRequiredRunID = state.RunID
				latestRequiredElapsedMs = state.ElapsedMs
				latestRequiredObservedAt = state.ObservedAt
			}
			totalElapsedMs += state.ElapsedMs
			switch state.Status {
			case store.StatusPassed:
				passed++
			case store.StatusFailed:
				failed++
			default:
				missing++
			}
		}
		admission := "pending"
		if required > 0 && passed == required {
			admission = store.StatusPassed
		} else if failed > 0 {
			admission = store.StatusFailed
		} else if missing == 0 && required == 0 {
			admission = "pending"
		}
		payload.Items[index].AdmissionStatus = admission
		payload.Items[index].RequiredCaseCount = required
		payload.Items[index].PassedCaseCount = passed
		payload.Items[index].LatestRunID = firstNonEmpty(latestRequiredRunID, latestRunID)
		payload.Items[index].LatestElapsedMs = firstPositiveInt64(latestRequiredElapsedMs, latestElapsedMs)
		payload.Items[index].TotalElapsedMs = firstPositiveInt64(totalElapsedMs, payload.Items[index].LatestElapsedMs)
	}
	return nil
}

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

func latestCaseStatuses(ctx context.Context, runtime store.Store) (map[string]string, error) {
	states, err := latestCaseStates(ctx, runtime)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for caseID, state := range states {
		out[caseID] = state.Status
	}
	return out, nil
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

func interfaceNodeDetailPayloadFromBundle(bundle profile.Bundle, id string) (interfaceNodeDetailPayload, bool) {
	for _, node := range bundle.InterfaceNodes {
		if node.ID == id {
			return interfaceNodeDetailPayloadForNode(bundle, node), true
		}
	}
	return interfaceNodeDetailPayload{
		OK:         false,
		TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Source:     map[string]string{"kind": "profile", "id": bundle.ID},
		Error:      "interface node not found",
		Requested:  id,
		Available:  interfaceNodesPayloadFromBundle(bundle, "", "").Items,
		Cases:      []interfaceCase{},
		Fields:     emptyInterfaceNodeFields(),
		History:    emptyInterfaceNodeHistory(),
		Runs:       []map[string]any{},
	}, false
}

func interfaceNodeDetailPayloadForNode(bundle profile.Bundle, node profile.InterfaceNode) interfaceNodeDetailPayload {
	templates := requestTemplatesForNode(bundle.RequestTemplates, node.ID)
	cases := casesForNode(bundle.APICases, bundle.CaseDependencies, node.ID)
	method, path := "", ""
	if len(templates) > 0 {
		method = templates[0].Method
		path = templates[0].Path
	}
	return interfaceNodeDetailPayload{
		OK:         true,
		TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Source:     map[string]string{"kind": "profile", "id": bundle.ID},
		Node: interfaceNodeDetail{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
			Operation:   firstNonEmpty(node.DisplayName, node.ID),
			Method:      method,
			Path:        path,
			TimeoutMs:   node.TimeoutMs,
		},
		Admission: interfaceNodeAdmission{
			Status:            "pending",
			RequiredCaseCount: 0,
			PassedCaseCount:   0,
			Blockers:          []map[string]any{},
		},
		RequestTemplates: templates,
		Cases:            cases,
		Fields:           emptyInterfaceNodeFields(),
		History:          emptyInterfaceNodeHistory(),
		Runs:             []map[string]any{},
	}
}

func interfaceNodeDetailPayloadFromCatalog(catalog store.ProfileCatalog, id string) (interfaceNodeDetailPayload, bool) {
	var node store.CatalogInterfaceNode
	found := false
	for _, item := range catalog.InterfaceNodes {
		if item.ID == id {
			node = item
			found = true
			break
		}
	}
	if !found {
		return interfaceNodeDetailPayload{}, false
	}
	cases := casesForCatalogNode(catalog, id)
	return interfaceNodeDetailPayload{
		OK:         true,
		TemplateID: "TPL-INTERFACE-NODE-CASE-LIST-V1",
		Source:     map[string]string{"kind": "store", "id": catalog.ProfileID},
		Requested:  id,
		Node: interfaceNodeDetail{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			ServiceID:   node.ServiceID,
			Operation:   firstNonEmpty(node.Operation, node.DisplayName, node.ID),
			Method:      node.Method,
			Path:        node.Path,
			TimeoutMs:   node.TimeoutMs,
			TemplateID:  node.TemplateID,
			Version:     node.Version,
			Status:      node.Status,
			Tags:        node.Tags,
			Description: node.Description,
			SortOrder:   node.SortOrder,
			CreatedAt:   node.CreatedAt,
			UpdatedAt:   node.UpdatedAt,
		},
		Admission: interfaceNodeAdmission{
			Status:            "pending",
			RequiredCaseCount: requiredInterfaceCaseCount(cases),
			PassedCaseCount:   0,
			Blockers:          []map[string]any{},
		},
		RequestTemplates: requestTemplatesForCatalogNode(catalog.RequestTemplates, id),
		Cases:            cases,
		Fields:           fieldsForCatalogNode(catalog.InterfaceFields, id),
		History:          emptyInterfaceNodeHistory(),
		Runs:             []map[string]any{},
		Presentation:     interfaceNodePresentationForCatalog(catalog.TemplateConfigs, node),
	}, true
}

func interfaceNodePresentationForCatalog(configs []store.CatalogTemplateConfig, node store.CatalogInterfaceNode) interfaceNodePresentation {
	copy := map[string]string{}
	for _, config := range configs {
		if !visibleTemplateConfigStatus(config.Status) || config.ScopeType != "interface-node" {
			continue
		}
		configCopy := stringMapFromAny(jsonObject(config.ConfigJSON)["copy"])
		if len(configCopy) == 0 {
			continue
		}
		switch {
		case config.NodeID == "" && (config.ScopeID == "" || config.ScopeID == "_default"):
			mergeStringMap(copy, configCopy)
		case config.NodeID == node.ID || config.ScopeID == node.ID:
			mergeStringMap(copy, configCopy)
		}
	}
	if len(copy) == 0 {
		return interfaceNodePresentation{}
	}
	return interfaceNodePresentation{Copy: copy}
}

func interfaceNodeDirectoryPresentationForCatalog(configs []store.CatalogTemplateConfig) interfaceNodePresentation {
	copy := map[string]string{}
	for _, config := range configs {
		if !visibleTemplateConfigStatus(config.Status) || config.ScopeType != "interface-node-directory" {
			continue
		}
		if config.ScopeID != "" && config.ScopeID != "_default" {
			continue
		}
		configCopy := stringMapFromAny(jsonObject(config.ConfigJSON)["copy"])
		if len(configCopy) == 0 {
			continue
		}
		mergeStringMap(copy, configCopy)
	}
	if len(copy) == 0 {
		return interfaceNodePresentation{}
	}
	return interfaceNodePresentation{Copy: copy}
}

func mergeStringMap(target map[string]string, source map[string]string) {
	for key, value := range source {
		if key != "" && value != "" {
			target[key] = value
		}
	}
}

func requestTemplatesForCatalogNode(items []store.CatalogRequestTemplate, nodeID string) []interfaceRequestTemplate {
	templates := make([]interfaceRequestTemplate, 0)
	for _, item := range items {
		if item.NodeID != nodeID || !activeCatalogStatus(item.Status) {
			continue
		}
		templates = append(templates, interfaceRequestTemplate{
			ID:           item.ID,
			Name:         item.DisplayName,
			Version:      item.Version,
			Status:       firstNonEmpty(item.Status, "active"),
			Method:       item.Method,
			Path:         item.Path,
			TemplateJSON: item.TemplateJSON,
		})
	}
	sort.SliceStable(templates, func(i int, j int) bool { return templates[i].ID < templates[j].ID })
	return templates
}

func casesForCatalogNode(catalog store.ProfileCatalog, nodeID string) []interfaceCase {
	dependenciesByCase := make(map[string][]map[string]any)
	fixtureByID := make(map[string]store.CatalogFixture)
	for _, fixture := range catalog.Fixtures {
		fixtureByID[fixture.ID] = fixture
	}
	for _, dependency := range catalog.CaseDependencies {
		if !activeCatalogStatus(dependency.Status) {
			continue
		}
		fixture := fixtureByID[dependency.FixtureID]
		dependenciesByCase[dependency.CaseID] = append(dependenciesByCase[dependency.CaseID], map[string]any{
			"id":               dependency.ID,
			"fixtureProfileId": dependency.FixtureID,
			"profile": map[string]any{
				"id":   fixture.ID,
				"name": fixture.DisplayName,
				"kind": fixture.Kind,
			},
			"required":     dependency.Required,
			"mappingsJson": dependency.MappingsJSON,
		})
	}
	cases := make([]interfaceCase, 0)
	for _, item := range catalog.APICases {
		if item.NodeID != nodeID || !activeCatalogStatus(item.Status) {
			continue
		}
		cases = append(cases, interfaceCase{
			ID:                   item.ID,
			Title:                item.DisplayName,
			CaseType:             firstNonEmpty(item.CaseType, "api"),
			Scenario:             item.Scenario,
			PayloadTemplateJSON:  item.PayloadTemplateJSON,
			RequestTemplateID:    item.RequestTemplateID,
			PatchJSON:            item.PatchJSON,
			RenderMode:           item.RenderMode,
			ExpectedJSON:         item.ExpectedJSON,
			Status:               item.Status,
			SortOrder:            item.SortOrder,
			RequiredForAdmission: item.RequiredForAdmission,
			Dependencies:         nonNil(dependenciesByCase[item.ID]),
		})
	}
	return cases
}

func fieldsForCatalogNode(items []store.CatalogInterfaceNodeField, nodeID string) interfaceNodeFields {
	fields := emptyInterfaceNodeFields()
	for _, item := range items {
		if item.NodeID != nodeID || !activeCatalogStatus(item.Status) {
			continue
		}
		row := map[string]any{
			"id":          item.ID,
			"fieldPath":   item.FieldPath,
			"displayName": item.DisplayName,
			"dataType":    item.DataType,
			"required":    item.Required,
			"bindable":    item.Bindable,
			"portType":    item.PortType,
		}
		switch item.Direction {
		case "response":
			fields.Response = append(fields.Response, row)
		default:
			fields.Request = append(fields.Request, row)
		}
	}
	return fields
}

func requiredInterfaceCaseCount(items []interfaceCase) int {
	count := 0
	for _, item := range items {
		if item.RequiredForAdmission {
			count++
		}
	}
	return count
}

func activeCatalogStatus(status string) bool {
	status = strings.TrimSpace(strings.ToLower(status))
	return status == "" || status == "active"
}

func requestTemplatesForNode(items []profile.RequestTemplate, nodeID string) []interfaceRequestTemplate {
	templates := make([]interfaceRequestTemplate, 0)
	for _, item := range items {
		if item.NodeID != nodeID {
			continue
		}
		templates = append(templates, interfaceRequestTemplate{
			ID:           item.ID,
			Name:         item.DisplayName,
			Status:       "active",
			Method:       item.Method,
			Path:         item.Path,
			TemplateJSON: item.TemplateJSON,
		})
	}
	return templates
}

func casesForNode(items []profile.APICase, dependencies []profile.CaseDependency, nodeID string) []interfaceCase {
	dependenciesByCase := make(map[string][]map[string]any)
	for _, dependency := range dependencies {
		dependenciesByCase[dependency.CaseID] = append(dependenciesByCase[dependency.CaseID], map[string]any{
			"id":               dependency.ID,
			"fixtureProfileId": dependency.FixtureID,
			"mappingsJson":     dependency.MappingsJSON,
		})
	}
	cases := make([]interfaceCase, 0)
	for _, item := range items {
		if item.NodeID != nodeID {
			continue
		}
		cases = append(cases, interfaceCase{
			ID:                   item.ID,
			Title:                item.DisplayName,
			CaseType:             "success",
			RequiredForAdmission: false,
			Dependencies:         nonNil(dependenciesByCase[item.ID]),
		})
	}
	return cases
}

func emptyInterfaceNodeFields() interfaceNodeFields {
	return interfaceNodeFields{
		Request:  []map[string]any{},
		Response: []map[string]any{},
	}
}

func emptyInterfaceNodeHistory() map[string]any {
	return map[string]any{
		"latestRunId":         "",
		"passCount":           0,
		"failCount":           0,
		"runCount":            0,
		"latestFailureReason": "",
		"totalElapsedMs":      0,
		"perCase":             []map[string]any{},
	}
}
