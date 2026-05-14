package profile

type Bundle struct {
	ID               string            `json:"id"`
	DisplayName      string            `json:"displayName"`
	Description      string            `json:"description,omitempty"`
	Services         []Service         `json:"services"`
	Workflows        []Workflow        `json:"workflows"`
	InterfaceNodes   []InterfaceNode   `json:"interfaceNodes"`
	APICases         []APICase         `json:"apiCases"`
	RequestTemplates []RequestTemplate `json:"requestTemplates"`
	CaseDependencies []CaseDependency  `json:"caseDependencies"`
	WorkflowBindings []WorkflowBinding `json:"workflowBindings"`
	Fixtures         []Fixture         `json:"fixtures"`
}

type Service struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

type Workflow struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
}

type InterfaceNode struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	ServiceID   string `json:"serviceId,omitempty"`
}

type APICase struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	NodeID      string `json:"nodeId,omitempty"`
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
}

type Fixture struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

type Counts struct {
	Services         int
	Workflows        int
	InterfaceNodes   int
	APICases         int
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
		RequestTemplates: len(b.RequestTemplates),
		CaseDependencies: len(b.CaseDependencies),
		WorkflowBindings: len(b.WorkflowBindings),
		Fixtures:         len(b.Fixtures),
	}
}
