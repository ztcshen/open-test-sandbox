package profile

// EmptyBundle returns the generic no-profile state used before an external
// profile bundle has been published into the local store.
func EmptyBundle() Bundle {
	return Bundle{
		ID:                "empty",
		DisplayName:       "Empty Profile",
		Services:          []Service{},
		Workflows:         []Workflow{},
		InterfaceNodes:    []InterfaceNode{},
		APICases:          []APICase{},
		Executors:         []ExecutorDescriptor{},
		RequestTemplates:  []RequestTemplate{},
		CaseDependencies:  []CaseDependency{},
		WorkflowBindings:  []WorkflowBinding{},
		Fixtures:          []Fixture{},
		TemplateConfigs:   []TemplateConfig{},
		AgentTestProfiles: []AgentTestProfile{},
	}
}
