package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("store record not found")

const (
	StatusRunning = "running"
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

type Store interface {
	Close() error

	CreateRun(context.Context, Run) (Run, error)
	GetRun(context.Context, string) (Run, error)
	ListRuns(context.Context) ([]Run, error)

	RecordAPICaseRun(context.Context, APICaseRun) (APICaseRun, error)
	ListAPICaseRuns(context.Context, string) ([]APICaseRun, error)

	RecordEvidence(context.Context, EvidenceRecord) (EvidenceRecord, error)
	ListEvidence(context.Context, string) ([]EvidenceRecord, error)
	SaveTraceTopology(context.Context, TraceTopology) (TraceTopology, error)
	ListTraceTopologies(context.Context, string) ([]TraceTopology, error)
	RecordPostProcessTask(context.Context, PostProcessTask) (PostProcessTask, error)
	ListPostProcessTasks(context.Context, string) ([]PostProcessTask, error)

	UpsertBaselineGate(context.Context, BaselineGate) (BaselineGate, error)
	GetBaselineGate(context.Context, string, string) (BaselineGate, error)

	UpsertProfileIndex(context.Context, ProfileIndex) (ProfileIndex, error)
	GetProfileIndex(context.Context, string) (ProfileIndex, error)
	UpsertConfigVersion(context.Context, ConfigVersion) (ConfigVersion, error)
	GetActiveConfigVersion(context.Context) (ConfigVersion, error)
	UpsertReadModel(context.Context, ReadModel) (ReadModel, error)
	GetReadModel(context.Context, string, string) (ReadModel, error)
	ReplaceProfileCatalog(context.Context, ProfileCatalog) error
	GetProfileCatalog(context.Context) (ProfileCatalog, error)
	GetProfileCatalogIndex(context.Context) (ProfileCatalogIndex, error)

	UpsertEnvironment(context.Context, Environment) (Environment, error)
	GetEnvironment(context.Context, string) (Environment, error)
	ListEnvironments(context.Context) ([]Environment, error)
	ReplaceEnvironmentComponentGraph(context.Context, string, EnvironmentComponentGraph) error
	GetEnvironmentComponentGraph(context.Context, string) (EnvironmentComponentGraph, error)
}

type Run struct {
	ID            string
	ProfileID     string
	EnvironmentID string
	WorkflowID    string
	Status        string
	EvidenceRoot  string
	SummaryJSON   string
	StartedAt     time.Time
	FinishedAt    time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type APICaseRun struct {
	ID                   string
	RunID                string
	CaseID               string
	Status               string
	RequestSummaryJSON   string
	AssertionSummaryJSON string
	StartedAt            time.Time
	FinishedAt           time.Time
	CreatedAt            time.Time
}

type APICaseRunRecord struct {
	Run     Run
	CaseRun APICaseRun
}

type EvidenceRecord struct {
	ID         string
	RunID      string
	CaseRunID  string
	StepID     string
	Kind       string
	URI        string
	MediaType  string
	SHA256     string
	SizeBytes  int64
	Summary    string
	Category   string
	Visibility string
	LabelsJSON string
	CreatedAt  time.Time
}

type TraceTopology struct {
	ID            string
	WorkflowRunID string
	WorkflowID    string
	StepID        string
	CaseID        string
	RequestID     string
	TraceID       string
	Status        string
	TopologyJSON  string
	TextTopology  string
	CreatedAt     time.Time
}

type PostProcessTask struct {
	ID          string
	RunID       string
	WorkflowID  string
	StepID      string
	CaseID      string
	Kind        string
	Status      string
	StartedAt   time.Time
	FinishedAt  time.Time
	DurationMs  int64
	Error       string
	SummaryJSON string
	CreatedAt   time.Time
}

type BaselineGate struct {
	ProfileID   string
	SubjectID   string
	Status      string
	Required    bool
	SummaryJSON string
	CheckedAt   time.Time
	UpdatedAt   time.Time
}

type ProfileIndex struct {
	ProfileID    string
	BundlePath   string
	BundleDigest string
	SummaryJSON  string
	ImportedAt   time.Time
	UpdatedAt    time.Time
}

type ConfigVersion struct {
	ID           string
	ProfileID    string
	SourcePath   string
	BundleDigest string
	SummaryJSON  string
	Active       bool
	PublishedAt  time.Time
	CreatedAt    time.Time
}

type ReadModel struct {
	ProfileID       string
	Key             string
	ConfigVersionID string
	PayloadJSON     string
	GeneratedAt     time.Time
	UpdatedAt       time.Time
}

type Environment struct {
	ID                     string
	DisplayName            string
	Description            string
	Status                 string
	Verified               bool
	ServicesJSON           string
	ReposJSON              string
	ComposeJSON            string
	HealthChecksJSON       string
	VerificationWorkflowID string
	LastVerificationRunID  string
	LastVerificationStatus string
	EvidenceComplete       bool
	TopologyComplete       bool
	LastVerifiedAt         time.Time
	SummaryJSON            string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

const (
	StoreMetadataMaxBytes         = 1024 * 1024
	EnvironmentDefinitionMaxBytes = StoreMetadataMaxBytes
	EnvironmentSummaryMaxBytes    = StoreMetadataMaxBytes
	ComponentAssetInlineMaxBytes  = StoreMetadataMaxBytes
	ComponentGraphMaxBytes        = StoreMetadataMaxBytes
)

func ValidateEnvironmentDefinitionSize(e Environment) error {
	definitionFields := []namedSize{
		{name: "id", size: len(e.ID)},
		{name: "display_name", size: len(e.DisplayName)},
		{name: "description", size: len(e.Description)},
		{name: "status", size: len(e.Status)},
		{name: "services_json", size: len(e.ServicesJSON)},
		{name: "repos_json", size: len(e.ReposJSON)},
		{name: "compose_json", size: len(e.ComposeJSON)},
		{name: "health_checks_json", size: len(e.HealthChecksJSON)},
		{name: "verification_workflow_id", size: len(e.VerificationWorkflowID)},
	}
	total := 0
	for _, field := range definitionFields {
		total += field.size
	}
	if total > EnvironmentDefinitionMaxBytes {
		largest := largestNamedSize(definitionFields)
		return fmt.Errorf("environment definition metadata is %d bytes; 1 MB safety boundary is %d bytes; write blocked. Reason: largest contributor %s contributes %d bytes in environment %q; below this boundary the Store accepts deterministic restore metadata, startup configuration, DDL, seed SQL, certificates, keys, and launch scripts without per-kind limits", total, EnvironmentDefinitionMaxBytes, largest.name, largest.size, e.ID)
	}
	if len(e.SummaryJSON) > EnvironmentSummaryMaxBytes {
		return fmt.Errorf("environment summary metadata is %d bytes; 1 MB safety boundary is %d bytes; write blocked. Reason: largest contributor summary_json contributes %d bytes in environment %q; below this boundary the Store accepts acceptance summaries, restore status, and indexes without per-kind limits", len(e.SummaryJSON), EnvironmentSummaryMaxBytes, len(e.SummaryJSON), e.ID)
	}
	return nil
}

type namedSize struct {
	name string
	size int
}

func largestNamedSize(items []namedSize) namedSize {
	largest := namedSize{}
	for _, item := range items {
		if item.size > largest.size {
			largest = item
		}
	}
	return largest
}

type EnvironmentComponentGraph struct {
	Components   []EnvironmentComponent `json:"components"`
	Dependencies []ComponentDependency  `json:"dependencies"`
	Assets       []ComponentConfigAsset `json:"assets"`
}

type EnvironmentComponent struct {
	EnvID           string    `json:"envId,omitempty"`
	ComponentID     string    `json:"componentId"`
	DisplayName     string    `json:"displayName,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	Role            string    `json:"role,omitempty"`
	ComposeService  string    `json:"composeService,omitempty"`
	Image           string    `json:"image,omitempty"`
	Required        bool      `json:"required"`
	RuntimeJSON     string    `json:"runtimeJson,omitempty"`
	HealthCheckJSON string    `json:"healthCheckJson,omitempty"`
	SummaryJSON     string    `json:"summaryJson,omitempty"`
	CreatedAt       time.Time `json:"createdAt,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt,omitempty"`
}

type ComponentDependency struct {
	EnvID               string    `json:"envId,omitempty"`
	ConsumerComponentID string    `json:"consumerComponentId"`
	ProviderComponentID string    `json:"providerComponentId"`
	Phase               string    `json:"phase,omitempty"`
	Capability          string    `json:"capability,omitempty"`
	Required            bool      `json:"required"`
	ProfileJSON         string    `json:"profileJson,omitempty"`
	CreatedAt           time.Time `json:"createdAt,omitempty"`
	UpdatedAt           time.Time `json:"updatedAt,omitempty"`
}

type ComponentConfigAsset struct {
	EnvID             string    `json:"envId,omitempty"`
	OwnerComponentID  string    `json:"ownerComponentId"`
	AssetID           string    `json:"assetId"`
	AssetKind         string    `json:"assetKind,omitempty"`
	TargetComponentID string    `json:"targetComponentId,omitempty"`
	TargetPath        string    `json:"targetPath,omitempty"`
	ContentInline     string    `json:"contentInline,omitempty"`
	RemoteRefJSON     string    `json:"remoteRefJson,omitempty"`
	SHA256            string    `json:"sha256,omitempty"`
	SizeBytes         int64     `json:"sizeBytes,omitempty"`
	ApplyOrder        int       `json:"applyOrder,omitempty"`
	Sensitive         bool      `json:"sensitive"`
	SummaryJSON       string    `json:"summaryJson,omitempty"`
	CreatedAt         time.Time `json:"createdAt,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt,omitempty"`
}

func ValidateEnvironmentComponentGraph(envID string, g EnvironmentComponentGraph) error {
	envID = strings.TrimSpace(envID)
	if envID == "" {
		return fmt.Errorf("environment id is required for component graph")
	}
	total := 0
	componentIDs := map[string]bool{}
	contributors := make([]namedSize, 0, len(g.Components)+len(g.Dependencies)+len(g.Assets))
	for _, component := range g.Components {
		id := strings.TrimSpace(component.ComponentID)
		if id == "" {
			return fmt.Errorf("component id is required")
		}
		componentIDs[id] = true
		size := len(id) + len(component.DisplayName) + len(component.Kind) + len(component.Role) +
			len(component.ComposeService) + len(component.Image) + len(component.RuntimeJSON) +
			len(component.HealthCheckJSON) + len(component.SummaryJSON)
		total += size
		contributors = append(contributors, namedSize{name: fmt.Sprintf("component %q", id), size: size})
	}
	for _, dep := range g.Dependencies {
		consumer := strings.TrimSpace(dep.ConsumerComponentID)
		provider := strings.TrimSpace(dep.ProviderComponentID)
		if consumer == "" || provider == "" {
			return fmt.Errorf("component dependency requires consumer and provider component ids")
		}
		if !componentIDs[consumer] {
			return fmt.Errorf("component dependency consumer %q is not registered in environment %s", consumer, envID)
		}
		if !componentIDs[provider] {
			return fmt.Errorf("component dependency provider %q is not registered in environment %s", provider, envID)
		}
		size := len(consumer) + len(provider) + len(dep.Phase) + len(dep.Capability) + len(dep.ProfileJSON)
		total += size
		contributors = append(contributors, namedSize{name: fmt.Sprintf("dependency %q->%q", consumer, provider), size: size})
	}
	for _, asset := range g.Assets {
		owner := strings.TrimSpace(asset.OwnerComponentID)
		if owner == "" || strings.TrimSpace(asset.AssetID) == "" {
			return fmt.Errorf("component config asset requires owner component id and asset id")
		}
		if !componentIDs[owner] {
			return fmt.Errorf("component config asset owner %q is not registered in environment %s", owner, envID)
		}
		target := strings.TrimSpace(asset.TargetComponentID)
		if target != "" && !componentIDs[target] {
			return fmt.Errorf("component config asset target %q is not registered in environment %s", target, envID)
		}
		if len(asset.ContentInline) > ComponentAssetInlineMaxBytes {
			return fmt.Errorf("component config asset %q inline content is %d bytes; 1 MB safety boundary is %d bytes; write blocked. Reason: owner=%q kind=%q target=%q path=%q is the single inline content contributor over the boundary; below this boundary the Store accepts deterministic text configuration, DDL, seed SQL, certificates, keys, and launch scripts without per-kind limits", asset.AssetID, len(asset.ContentInline), ComponentAssetInlineMaxBytes, owner, asset.AssetKind, target, asset.TargetPath)
		}
		size := len(owner) + len(asset.AssetID) + len(asset.AssetKind) + len(target) + len(asset.TargetPath) +
			len(asset.ContentInline) + len(asset.RemoteRefJSON) + len(asset.SHA256) + len(asset.SummaryJSON)
		total += size
		contributors = append(contributors, namedSize{name: fmt.Sprintf("asset %q owner=%q kind=%q target=%q path=%q", asset.AssetID, owner, asset.AssetKind, target, asset.TargetPath), size: size})
	}
	if total > ComponentGraphMaxBytes {
		largest := largestNamedSize(contributors)
		return fmt.Errorf("environment component graph metadata is %d bytes; 1 MB safety boundary is %d bytes; write blocked. Reason: largest contributor %s contributes %d bytes in environment %q, and the combined component graph is over the boundary; below this boundary the Store accepts deterministic restore metadata and startup text without per-kind limits", total, ComponentGraphMaxBytes, largest.name, largest.size, envID)
	}
	return nil
}

type ProfileCatalog struct {
	ProfileID        string
	IndexedAt        time.Time
	Services         []CatalogService
	Workflows        []CatalogWorkflow
	InterfaceNodes   []CatalogInterfaceNode
	InterfaceFields  []CatalogInterfaceNodeField
	APICases         []CatalogAPICase
	RequestTemplates []CatalogRequestTemplate
	WorkflowBindings []CatalogWorkflowBinding
	CaseDependencies []CatalogCaseDependency
	Fixtures         []CatalogFixture
	TemplateConfigs  []CatalogTemplateConfig
}

type ProfileCatalogIndex struct {
	ProfileID string
	IndexedAt time.Time
	Counts    ProfileCatalogCounts
}

type ProfileCatalogCounts struct {
	Services         int
	Workflows        int
	InterfaceNodes   int
	APICases         int
	RequestTemplates int
	WorkflowBindings int
	CaseDependencies int
	Fixtures         int
	Templates        int
	TemplateConfigs  int
}

type CatalogService struct {
	ID                  string
	DisplayName         string
	Kind                string
	AttachedTemplateIDs []string
	GitURL              string
	GitBranch           string
	RepoEnv             string
	SourcePath          string
	ContainerName       string
	Image               string
	DockerService       string
	ServicePort         int
	ManagementPort      int
	MemoryMb            int
	CPUMilli            int
	StartupCommand      string
	HealthURL           string
	LogPath             string
	Status              string
	SortOrder           int
}

type CatalogWorkflow struct {
	ID                string
	DisplayName       string
	Description       string
	BaseStepTimeoutMs int
	TimeoutOffsetMs   int
}

type CatalogInterfaceNode struct {
	ID          string
	DisplayName string
	ServiceID   string
	Operation   string
	Method      string
	Path        string
	TemplateID  string
	Version     string
	Status      string
	Tags        []string
	Description string
	TimeoutMs   int
	SortOrder   int
	CreatedAt   string
	UpdatedAt   string
}

type CatalogInterfaceNodeField struct {
	ID          string
	NodeID      string
	Direction   string
	FieldPath   string
	DisplayName string
	DataType    string
	Required    bool
	Bindable    bool
	PortType    string
	Status      string
	SortOrder   int
}

type CatalogAPICase struct {
	ID                   string
	DisplayName          string
	Description          string
	NodeID               string
	CaseType             string
	Scenario             string
	Tags                 []string
	Priority             string
	Owner                string
	PayloadTemplateJSON  string
	RequestTemplateID    string
	PatchJSON            string
	RenderMode           string
	ExpectedJSON         string
	RequiredForAdmission bool
	Status               string
	SortOrder            int
	CasePath             string
	SourceKind           string
	SourcePath           string
	ExecutorID           string
	BaseURL              string
	EvidenceDir          string
	TimeoutSeconds       int
	DefaultOverridesJSON string
}

type CatalogRequestTemplate struct {
	ID           string
	DisplayName  string
	NodeID       string
	Method       string
	Path         string
	TemplateJSON string
	Version      string
	Status       string
	SortOrder    int
}

type CatalogWorkflowBinding struct {
	WorkflowID string
	StepID     string
	NodeID     string
	CaseID     string
	Required   bool
	SortOrder  int
}

type CatalogCaseDependency struct {
	ID           string
	CaseID       string
	FixtureID    string
	MappingsJSON string
	Required     bool
	Status       string
	SortOrder    int
}

type CatalogFixture struct {
	ID               string
	DisplayName      string
	Kind             string
	DataJSON         string
	SourceWorkflowID string
	SourceUntilStep  string
	TTLSeconds       int
	Status           string
	SortOrder        int
}

type CatalogTemplateConfig struct {
	ID          string
	TemplateID  string
	NodeID      string
	WorkflowID  string
	ScopeType   string
	ScopeID     string
	Title       string
	Description string
	ConfigJSON  string
	Status      string
	SortOrder   int
}
