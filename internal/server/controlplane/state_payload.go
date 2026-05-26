package controlplane

import (
	"agent-testbench/internal/domain/profile"
)

type statePayload struct {
	Services []stateService `json:"services"`
}

type stateService struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Status string `json:"status"`
	Exists bool   `json:"exists"`
}

func statePayloadFromBundle(bundle profile.Bundle) statePayload {
	services := make([]stateService, 0, len(bundle.Services))
	for _, service := range bundle.Services {
		services = append(services, stateService{
			ID:     service.ID,
			Name:   firstNonEmpty(service.DisplayName, service.ID),
			Kind:   service.Kind,
			Status: "missing",
			Exists: false,
		})
	}
	return statePayload{Services: services}
}
