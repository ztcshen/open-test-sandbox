package controlplane

import "agent-testbench/internal/store"

type Options struct {
	Runtime         store.Store
	TraceGraphQLURL string
	ProfileHome     string
	StoreInfo       StoreInfo
}

type StoreInfo struct {
	Configured bool   `json:"configured"`
	Name       string `json:"name,omitempty"`
	Backend    string `json:"backend,omitempty"`
	URL        string `json:"url,omitempty"`
	Source     string `json:"source,omitempty"`
}

type storeCurrentPayload struct {
	OK bool `json:"ok"`
	StoreInfo
}
