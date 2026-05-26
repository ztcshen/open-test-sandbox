package controlplane

import (
	"context"
	"encoding/json"
	"errors"
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
