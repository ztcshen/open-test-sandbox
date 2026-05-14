package profile

type Bundle struct {
	ID             string          `json:"id"`
	DisplayName    string          `json:"displayName"`
	Description    string          `json:"description,omitempty"`
	Services       []Service       `json:"services"`
	Workflows      []Workflow      `json:"workflows"`
	InterfaceNodes []InterfaceNode `json:"interfaceNodes"`
	APICases       []APICase       `json:"apiCases"`
	Fixtures       []Fixture       `json:"fixtures"`
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

type Fixture struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

type Counts struct {
	Services       int
	Workflows      int
	InterfaceNodes int
	APICases       int
	Fixtures       int
}

func (b Bundle) Counts() Counts {
	return Counts{
		Services:       len(b.Services),
		Workflows:      len(b.Workflows),
		InterfaceNodes: len(b.InterfaceNodes),
		APICases:       len(b.APICases),
		Fixtures:       len(b.Fixtures),
	}
}
