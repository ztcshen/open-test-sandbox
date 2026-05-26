package controlplane

import (
	"sort"
	"strings"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

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
