package controlplane

import (
	"sort"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
)

type catalogPayload struct {
	SchemaVersion string               `json:"schemaVersion"`
	OK            bool                 `json:"ok"`
	GeneratedAt   time.Time            `json:"generatedAt"`
	Navigation    map[string]any       `json:"navigation"`
	Warnings      []string             `json:"warnings"`
	Source        map[string]string    `json:"source"`
	Presentation  *catalogPresentation `json:"presentation,omitempty"`
	Services      []catalogService     `json:"services"`
	Workflows     []catalogWorkflow    `json:"workflows"`
	APICases      []catalogAPICase     `json:"apiCases"`
	Topology      catalogTopology      `json:"topology"`
}

type catalogPresentation struct {
	WorkflowFinder *catalogWorkflowFinderConfig `json:"workflowFinder,omitempty"`
}

type catalogWorkflowFinderConfig struct {
	TargetStepCount      int    `json:"targetStepCount,omitempty"`
	TargetInterfaceCount int    `json:"targetInterfaceCount,omitempty"`
	TargetLabel          string `json:"targetLabel,omitempty"`
}

type catalogService struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"displayName,omitempty"`
	Role         string   `json:"role,omitempty"`
	Port         int      `json:"port,omitempty"`
	Dependencies []string `json:"dependencies"`
}

type catalogWorkflow struct {
	ID                string                       `json:"id"`
	DisplayName       string                       `json:"displayName,omitempty"`
	Description       string                       `json:"description,omitempty"`
	Entrypoint        string                       `json:"entrypoint"`
	BaseStepTimeoutMs int                          `json:"baseStepTimeoutMs"`
	TimeoutOffsetMs   int                          `json:"timeoutOffsetMs"`
	TimeoutMs         int                          `json:"timeoutMs"`
	Graph             catalogTopology              `json:"graph,omitempty"`
	Observability     catalogWorkflowObservability `json:"observability,omitempty"`
	Steps             []catalogWorkflowStep        `json:"steps"`
	StepCount         int                          `json:"stepCount,omitempty"`
	CaseCount         int                          `json:"caseCount,omitempty"`
	ServiceCount      int                          `json:"serviceCount,omitempty"`
	Presentation      catalogWorkflowPresentation  `json:"presentation"`
	RunCount          int                          `json:"runCount"`
	LatestRun         map[string]any               `json:"latestRun,omitempty"`
}

type catalogWorkflowStep struct {
	ID                 string                  `json:"id"`
	DisplayName        string                  `json:"displayName,omitempty"`
	ServiceID          string                  `json:"serviceId,omitempty"`
	CaseID             string                  `json:"caseId,omitempty"`
	Action             string                  `json:"action,omitempty"`
	Required           bool                    `json:"required,omitempty"`
	Executable         bool                    `json:"executable"`
	EvidenceKinds      []string                `json:"evidenceKinds,omitempty"`
	RelatedMockTargets []string                `json:"relatedMockTargets,omitempty"`
	Inputs             []map[string]any        `json:"inputs,omitempty"`
	Exports            []map[string]any        `json:"exports,omitempty"`
	TimeoutMs          int                     `json:"timeoutMs,omitempty"`
	Presentation       catalogStepPresentation `json:"presentation,omitempty"`
}

type catalogStepPresentation struct {
	Copy map[string]string `json:"copy,omitempty"`
}

type catalogWorkflowPresentation struct {
	Kind     string                 `json:"kind,omitempty"`
	Template string                 `json:"template,omitempty"`
	Title    string                 `json:"title,omitempty"`
	Copy     map[string]string      `json:"copy,omitempty"`
	Stages   []catalogWorkflowStage `json:"stages,omitempty"`
}

type catalogWorkflowObservability struct {
	Panels []catalogWorkflowPanel `json:"panels,omitempty"`
}

type catalogWorkflowPanel struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
	Scope string `json:"scope,omitempty"`
}

type catalogWorkflowStage struct {
	ID      string                     `json:"id"`
	Title   string                     `json:"title,omitempty"`
	Summary string                     `json:"summary,omitempty"`
	Steps   []catalogWorkflowStageStep `json:"steps,omitempty"`
}

type catalogWorkflowStageStep struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	CaseID string `json:"caseId,omitempty"`
}

type catalogAPICase struct {
	ID               string         `json:"id"`
	DisplayName      string         `json:"displayName,omitempty"`
	NodeID           string         `json:"nodeId,omitempty"`
	CasePath         string         `json:"casePath,omitempty"`
	SourceKind       string         `json:"sourceKind,omitempty"`
	SourcePath       string         `json:"sourcePath,omitempty"`
	ExecutorID       string         `json:"executorId,omitempty"`
	BaseURL          string         `json:"baseUrl,omitempty"`
	EvidenceDir      string         `json:"evidenceDir,omitempty"`
	TimeoutSeconds   int            `json:"timeoutSeconds,omitempty"`
	DefaultOverrides map[string]any `json:"defaultOverrides,omitempty"`
}

type catalogTopology struct {
	Nodes []string      `json:"nodes"`
	Edges []catalogEdge `json:"edges"`
}

type catalogEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func catalogPayloadFromBundle(bundle profile.Bundle) catalogPayload {
	services := make([]catalogService, 0, len(bundle.Services))
	nodes := make([]string, 0, len(bundle.Services))
	for _, service := range bundle.Services {
		nodes = append(nodes, service.ID)
		services = append(services, catalogService{
			ID:           service.ID,
			DisplayName:  service.DisplayName,
			Role:         firstNonEmpty(service.Kind, "service"),
			Dependencies: []string{},
		})
	}

	apiCases := make([]catalogAPICase, 0, len(bundle.APICases))
	for _, item := range bundle.APICases {
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
			DefaultOverrides: item.DefaultOverrides,
		})
	}

	return catalogPayload{
		SchemaVersion: "1",
		OK:            true,
		GeneratedAt:   time.Now().UTC(),
		Navigation:    map[string]any{},
		Warnings:      []string{},
		Source: map[string]string{
			"kind":        "profile",
			"id":          bundle.ID,
			"displayName": bundle.DisplayName,
		},
		Services:     services,
		Presentation: catalogPresentationFromProfileConfigs(bundle.TemplateConfigs),
		Workflows:    catalogWorkflows(bundle),
		APICases:     apiCases,
		Topology: catalogTopology{
			Nodes: nodes,
			Edges: []catalogEdge{},
		},
	}
}

func catalogPresentationFromProfileConfigs(configs []profile.TemplateConfig) *catalogPresentation {
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

func mergeCatalogWorkflowFinder(presentation *catalogPresentation, config map[string]any) {
	rawFinder := config["workflowFinder"]
	if rawFinder == nil {
		rawFinder = config["targetWorkflow"]
	}
	finderConfig, ok := rawFinder.(map[string]any)
	if !ok {
		return
	}
	if presentation.WorkflowFinder == nil {
		presentation.WorkflowFinder = &catalogWorkflowFinderConfig{}
	}
	if value := intValue(finderConfig["targetStepCount"]); value > 0 {
		presentation.WorkflowFinder.TargetStepCount = value
	}
	if value := intValue(finderConfig["targetInterfaceCount"]); value > 0 {
		presentation.WorkflowFinder.TargetInterfaceCount = value
	}
	if value := strings.TrimSpace(valueString(finderConfig["targetLabel"])); value != "" {
		presentation.WorkflowFinder.TargetLabel = value
	}
	if presentation.WorkflowFinder.TargetStepCount == 0 && presentation.WorkflowFinder.TargetInterfaceCount == 0 && presentation.WorkflowFinder.TargetLabel == "" {
		presentation.WorkflowFinder = nil
	}
}

func catalogWorkflows(bundle profile.Bundle) []catalogWorkflow {
	bindingsByWorkflow := make(map[string][]profile.WorkflowBinding)
	for _, binding := range bundle.WorkflowBindings {
		bindingsByWorkflow[binding.WorkflowID] = append(bindingsByWorkflow[binding.WorkflowID], binding)
	}
	for workflowID := range bindingsByWorkflow {
		sort.SliceStable(bindingsByWorkflow[workflowID], func(i int, j int) bool {
			return bindingsByWorkflow[workflowID][i].StepID < bindingsByWorkflow[workflowID][j].StepID
		})
	}

	nodeByID := make(map[string]profile.InterfaceNode, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		nodeByID[node.ID] = node
	}
	caseByID := make(map[string]profile.APICase, len(bundle.APICases))
	for _, item := range bundle.APICases {
		caseByID[item.ID] = item
	}

	workflows := make([]catalogWorkflow, 0, len(bundle.Workflows))
	for _, workflow := range bundle.Workflows {
		steps := make([]catalogWorkflowStep, 0, len(bindingsByWorkflow[workflow.ID]))
		for _, binding := range bindingsByWorkflow[workflow.ID] {
			node := nodeByID[binding.NodeID]
			item := caseByID[binding.CaseID]
			steps = append(steps, catalogWorkflowStep{
				ID:          firstNonEmpty(binding.StepID, binding.NodeID, binding.CaseID),
				DisplayName: firstNonEmpty(item.DisplayName, node.DisplayName, binding.StepID),
				ServiceID:   node.ServiceID,
				CaseID:      binding.CaseID,
				Action:      item.DisplayName,
				Required:    binding.Required,
				TimeoutMs:   node.TimeoutMs,
			})
		}
		workflows = append(workflows, catalogWorkflow{
			ID:                workflow.ID,
			DisplayName:       workflow.DisplayName,
			Description:       workflow.Description,
			Entrypoint:        "/workflow-studio.html",
			BaseStepTimeoutMs: workflow.BaseStepTimeoutMs,
			TimeoutOffsetMs:   workflow.TimeoutOffsetMs,
			TimeoutMs:         workflowBudgetMs(workflow.BaseStepTimeoutMs, workflow.TimeoutOffsetMs, steps),
			Steps:             steps,
			Presentation:      catalogWorkflowPresentation{Kind: workflowPresentationKind(steps)},
		})
	}
	return workflows
}

func workflowPresentationKind(steps []catalogWorkflowStep) string {
	if len(steps) == 0 {
		return "controlPlaneTool"
	}
	return "businessFlow"
}
