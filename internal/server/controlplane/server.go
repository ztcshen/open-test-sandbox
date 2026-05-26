package controlplane

import (
	"net/http"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

func New(bundle profile.Bundle) http.Handler {
	return NewWithStore(bundle, nil)
}

func NewWithStore(bundle profile.Bundle, runtime store.Store) http.Handler {
	return NewWithOptions(bundle, Options{Runtime: runtime})
}
