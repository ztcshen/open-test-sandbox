package controlplane

type dashboardPayload struct {
	OK             bool                  `json:"ok"`
	Source         map[string]string     `json:"source,omitempty"`
	Summary        dashboardSummary      `json:"summary"`
	Groups         []dashboardGroup      `json:"groups"`
	ServiceRuntime []serviceRuntime      `json:"serviceRuntime"`
	Presentation   dashboardPresentation `json:"presentation,omitempty"`
}

type dashboardSummary struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Missing   int `json:"missing"`
	Unhealthy int `json:"unhealthy"`
}

type dashboardGroup struct {
	ID          string          `json:"id"`
	Label       string          `json:"label"`
	DisplayName string          `json:"displayName"`
	Items       []dashboardItem `json:"items"`
}

type dashboardItem struct {
	ID             string                `json:"id"`
	Name           string                `json:"name,omitempty"`
	DisplayName    string                `json:"displayName,omitempty"`
	State          string                `json:"state"`
	Health         string                `json:"health"`
	Kind           string                `json:"kind,omitempty"`
	OK             bool                  `json:"ok"`
	Branch         string                `json:"branch,omitempty"`
	Profile        string                `json:"profile,omitempty"`
	Container      string                `json:"container,omitempty"`
	Image          string                `json:"image,omitempty"`
	Port           int                   `json:"port,omitempty"`
	ManagementPort int                   `json:"managementPort,omitempty"`
	Message        string                `json:"message,omitempty"`
	Presentation   dashboardPresentation `json:"presentation,omitempty"`
}

type dashboardPresentation struct {
	Copy map[string]string `json:"copy,omitempty"`
}
