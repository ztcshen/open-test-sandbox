package controlplane

import (
	"sync"

	"agent-testbench/internal/domain/profile"
)

type profileState struct {
	mu     sync.RWMutex
	bundle profile.Bundle
}

func newProfileState(bundle profile.Bundle) *profileState {
	return &profileState{bundle: bundle}
}

func (s *profileState) Current() profile.Bundle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bundle
}

func (s *profileState) Replace(bundle profile.Bundle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bundle = bundle
}
