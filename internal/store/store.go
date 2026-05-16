package store

import (
	"context"
	"errors"
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
}

type Run struct {
	ID           string
	ProfileID    string
	WorkflowID   string
	Status       string
	EvidenceRoot string
	SummaryJSON  string
	StartedAt    time.Time
	FinishedAt   time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
	ID        string
	RunID     string
	CaseRunID string
	Kind      string
	URI       string
	MediaType string
	SHA256    string
	SizeBytes int64
	Summary   string
	CreatedAt time.Time
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
	NodeID               string
	CaseType             string
	Scenario             string
	PayloadTemplateJSON  string
	RequestTemplateID    string
	PatchJSON            string
	RenderMode           string
	ExpectedJSON         string
	RequiredForAdmission bool
	Status               string
	SortOrder            int
	CasePath             string
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
