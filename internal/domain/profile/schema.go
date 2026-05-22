package profile

import "encoding/json"

type Bundle struct {
	ID                string                `json:"id"`
	DisplayName       string                `json:"displayName"`
	Description       string                `json:"description,omitempty"`
	BaseDir           string                `json:"-"`
	RuntimeEnvFiles   []string              `json:"runtimeEnvFiles,omitempty"`
	Services          []Service             `json:"services"`
	Workflows         []Workflow            `json:"workflows"`
	InterfaceNodes    []InterfaceNode       `json:"interfaceNodes"`
	APICases          []APICase             `json:"apiCases"`
	Executors         []ExecutorDescriptor  `json:"executors,omitempty"`
	FailureCategories []FailureCategoryRule `json:"failureCategories,omitempty"`
	RequestTemplates  []RequestTemplate     `json:"requestTemplates"`
	CaseDependencies  []CaseDependency      `json:"caseDependencies"`
	WorkflowBindings  []WorkflowBinding     `json:"workflowBindings"`
	Fixtures          []Fixture             `json:"fixtures"`
	TemplateConfigs   []TemplateConfig      `json:"templateConfigs,omitempty"`
	AgentTestProfiles []AgentTestProfile    `json:"agentTestProfiles,omitempty"`
	ConfigAuthoring   ConfigAuthoring       `json:"configAuthoring,omitempty"`
}

type Service struct {
	ID                  string   `json:"id"`
	DisplayName         string   `json:"displayName,omitempty"`
	Kind                string   `json:"kind,omitempty"`
	AttachedTemplateIDs []string `json:"attachedTemplateIds,omitempty"`
	GitURL              string   `json:"gitUrl,omitempty"`
	GitBranch           string   `json:"gitBranch,omitempty"`
	RepoEnv             string   `json:"repoEnv,omitempty"`
	SourcePath          string   `json:"sourcePath,omitempty"`
	ContainerName       string   `json:"containerName,omitempty"`
	Image               string   `json:"image,omitempty"`
	DockerService       string   `json:"dockerService,omitempty"`
	ServicePort         int      `json:"servicePort,omitempty"`
	ManagementPort      int      `json:"managementPort,omitempty"`
	MemoryMb            int      `json:"memoryMb,omitempty"`
	CPUMilli            int      `json:"cpuMilli,omitempty"`
	StartupCommand      string   `json:"startupCommand,omitempty"`
	HealthURL           string   `json:"healthUrl,omitempty"`
	LogPath             string   `json:"logPath,omitempty"`
	Status              string   `json:"status,omitempty"`
	SortOrder           int      `json:"sortOrder,omitempty"`
}

type Workflow struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName,omitempty"`
	Description       string `json:"description,omitempty"`
	BaseStepTimeoutMs int    `json:"baseStepTimeoutMs,omitempty"`
	TimeoutOffsetMs   int    `json:"timeoutOffsetMs,omitempty"`
}

type InterfaceNode struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName,omitempty"`
	ServiceID   string   `json:"serviceId,omitempty"`
	Operation   string   `json:"operation,omitempty"`
	Method      string   `json:"method,omitempty"`
	Path        string   `json:"path,omitempty"`
	TemplateID  string   `json:"templateId,omitempty"`
	Version     string   `json:"version,omitempty"`
	Status      string   `json:"status,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Description string   `json:"description,omitempty"`
	TimeoutMs   int      `json:"timeoutMs,omitempty"`
	SortOrder   int      `json:"sortOrder,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
}

type APICase struct {
	ID                   string         `json:"id"`
	DisplayName          string         `json:"displayName,omitempty"`
	Description          string         `json:"description,omitempty"`
	NodeID               string         `json:"nodeId,omitempty"`
	CaseType             string         `json:"caseType,omitempty"`
	Scenario             string         `json:"scenario,omitempty"`
	Tags                 []string       `json:"tags,omitempty"`
	Priority             string         `json:"priority,omitempty"`
	Owner                string         `json:"owner,omitempty"`
	PayloadTemplateJSON  string         `json:"payloadTemplateJson,omitempty"`
	RequestTemplateID    string         `json:"requestTemplateId,omitempty"`
	PatchJSON            string         `json:"patchJson,omitempty"`
	RenderMode           string         `json:"renderMode,omitempty"`
	ExpectedJSON         string         `json:"expectedJson,omitempty"`
	RequiredForAdmission bool           `json:"requiredForAdmission,omitempty"`
	Status               string         `json:"status,omitempty"`
	SortOrder            int            `json:"sortOrder,omitempty"`
	CasePath             string         `json:"casePath,omitempty"`
	SourceKind           string         `json:"sourceKind,omitempty"`
	SourcePath           string         `json:"sourcePath,omitempty"`
	ExecutorID           string         `json:"executorId,omitempty"`
	BaseURL              string         `json:"baseUrl,omitempty"`
	EvidenceDir          string         `json:"evidenceDir,omitempty"`
	TimeoutSeconds       int            `json:"timeoutSeconds,omitempty"`
	DefaultOverrides     map[string]any `json:"defaultOverrides,omitempty"`

	requiredForAdmissionSet bool
}

type ExecutorDescriptor struct {
	ID             string            `json:"id"`
	DisplayName    string            `json:"displayName,omitempty"`
	Description    string            `json:"description,omitempty"`
	Kind           string            `json:"kind"`
	Tool           string            `json:"tool,omitempty"`
	SourcePath     string            `json:"sourcePath,omitempty"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	WorkingDir     string            `json:"workingDir,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	Status         string            `json:"status,omitempty"`
	TimeoutSeconds int               `json:"timeoutSeconds,omitempty"`
	ArtifactPaths  []string          `json:"artifactPaths,omitempty"`
	SortOrder      int               `json:"sortOrder,omitempty"`
}

type FailureCategoryRule struct {
	Name     string                  `json:"name"`
	Category string                  `json:"category,omitempty"`
	Matchers FailureCategoryMatchers `json:"matchers,omitempty"`
}

type FailureCategoryMatchers struct {
	Statuses          []string `json:"statuses,omitempty"`
	FailureCategories []string `json:"failureCategories,omitempty"`
	MessageContains   []string `json:"messageContains,omitempty"`
}

func (c *APICase) UnmarshalJSON(data []byte) error {
	type alias APICase
	aux := struct {
		RequiredForAdmission *bool `json:"requiredForAdmission"`
		*alias
	}{
		alias: (*alias)(c),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.RequiredForAdmission != nil {
		c.RequiredForAdmission = *aux.RequiredForAdmission
		c.requiredForAdmissionSet = true
	}
	return nil
}

type RequestTemplate struct {
	ID           string `json:"id"`
	DisplayName  string `json:"displayName,omitempty"`
	NodeID       string `json:"nodeId,omitempty"`
	Method       string `json:"method,omitempty"`
	Path         string `json:"path,omitempty"`
	TemplateJSON string `json:"templateJson,omitempty"`
}

type CaseDependency struct {
	ID           string `json:"id"`
	CaseID       string `json:"caseId"`
	FixtureID    string `json:"fixtureId"`
	MappingsJSON string `json:"mappingsJson,omitempty"`
}

type WorkflowBinding struct {
	WorkflowID string `json:"workflowId"`
	StepID     string `json:"stepId"`
	NodeID     string `json:"nodeId"`
	CaseID     string `json:"caseId,omitempty"`
	Required   bool   `json:"required,omitempty"`
	SortOrder  int    `json:"sortOrder,omitempty"`
}

type Fixture struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind,omitempty"`
	DataJSON    string `json:"dataJson,omitempty"`
}

type TemplateConfig struct {
	ID          string `json:"id"`
	TemplateID  string `json:"templateId,omitempty"`
	NodeID      string `json:"nodeId,omitempty"`
	WorkflowID  string `json:"workflowId,omitempty"`
	ScopeType   string `json:"scopeType,omitempty"`
	ScopeID     string `json:"scopeId,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	ConfigJSON  string `json:"configJson,omitempty"`
	Status      string `json:"status,omitempty"`
	SortOrder   int    `json:"sortOrder,omitempty"`
}

type AgentTestProfile struct {
	ID             string            `json:"id"`
	Title          string            `json:"title,omitempty"`
	Context        map[string]any    `json:"context,omitempty"`
	Params         map[string]any    `json:"params,omitempty"`
	Steps          []AgentTestStep   `json:"steps,omitempty"`
	Probes         []AgentTestProbe  `json:"probes,omitempty"`
	MySQLProbes    []AgentTestProbe  `json:"mysqlProbes,omitempty"`
	EvidencePolicy map[string]bool   `json:"evidencePolicy,omitempty"`
	ConfigPolicy   AgentConfigPolicy `json:"configPolicy,omitempty"`
	RequiredConfig []RequiredConfig  `json:"requiredConfig,omitempty"`
}

type AgentTestStep struct {
	Type                 string         `json:"type,omitempty"`
	ID                   string         `json:"id,omitempty"`
	Method               string         `json:"method,omitempty"`
	URL                  string         `json:"url,omitempty"`
	Headers              map[string]any `json:"headers,omitempty"`
	Body                 map[string]any `json:"body,omitempty"`
	ExpectedStatus       int            `json:"expectedStatus,omitempty"`
	ExpectedBodyContains []string       `json:"expectedBodyContains,omitempty"`
}

type AgentTestProbe struct {
	Name  string `json:"name,omitempty"`
	Query string `json:"query,omitempty"`
	SQL   string `json:"sql,omitempty"`
}

type AgentConfigPolicy struct {
	AllowedChanges []ConfigChange `json:"allowedChanges,omitempty"`
}

type ConfigChange struct {
	Kind string `json:"kind,omitempty"`
	Key  string `json:"key,omitempty"`
}

type RequiredConfig struct {
	Kind           string `json:"kind,omitempty"`
	Key            string `json:"key,omitempty"`
	SuggestedValue string `json:"suggestedValue,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type ConfigAuthoring struct {
	SchemaVersion               string   `json:"schemaVersion,omitempty"`
	Role                        string   `json:"role,omitempty"`
	Summary                     string   `json:"summary,omitempty"`
	GuidePath                   string   `json:"guidePath,omitempty"`
	AllowedWritePaths           []string `json:"allowedWritePaths,omitempty"`
	AllowedReadPaths            []string `json:"allowedReadPaths,omitempty"`
	MainAgentResponsibilities   []string `json:"mainAgentResponsibilities,omitempty"`
	SubagentResponsibilities    []string `json:"subagentResponsibilities,omitempty"`
	HandoffRequiredFields       []string `json:"handoffRequiredFields,omitempty"`
	FrictionCategories          []string `json:"frictionCategories,omitempty"`
	RequiresDedicatedSubagent   bool     `json:"requiresDedicatedSubagent,omitempty"`
	ProhibitsMainAgentAuthoring bool     `json:"prohibitsMainAgentAuthoring,omitempty"`
}

type Counts struct {
	Services         int
	Workflows        int
	InterfaceNodes   int
	APICases         int
	Executors        int
	RequestTemplates int
	CaseDependencies int
	WorkflowBindings int
	Fixtures         int
}

func (b Bundle) Counts() Counts {
	return Counts{
		Services:         len(b.Services),
		Workflows:        len(b.Workflows),
		InterfaceNodes:   len(b.InterfaceNodes),
		APICases:         len(b.APICases),
		Executors:        len(b.Executors),
		RequestTemplates: len(b.RequestTemplates),
		CaseDependencies: len(b.CaseDependencies),
		WorkflowBindings: len(b.WorkflowBindings),
		Fixtures:         len(b.Fixtures),
	}
}
