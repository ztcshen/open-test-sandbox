package controlplane

import (
	"net/http"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

type routeDeps struct {
	profiles        *profileState
	runtime         store.Store
	collector       traceCollector
	caseBatchRunner *apiCaseBatchRunner
	profileHome     string
	storeInfo       StoreInfo
	staticDir       string
}

func NewWithOptions(bundle profile.Bundle, options Options) http.Handler {
	mux := http.NewServeMux()
	deps := routeDeps{
		profiles:        newProfileState(bundle),
		runtime:         options.Runtime,
		collector:       traceCollector{GraphQLURL: options.TraceGraphQLURL},
		caseBatchRunner: newAPICaseBatchRunner(),
		profileHome:     options.ProfileHome,
		storeInfo:       options.StoreInfo,
		staticDir:       findStaticDir(),
	}
	registerTemplatePackageRoutes(mux, deps)
	registerWorkbenchRoutes(mux, deps)
	registerWorkflowRoutes(mux, deps)
	registerCaseRoutes(mux, deps)
	registerInterfaceNodeRoutes(mux, deps)
	registerStaticRoutes(mux, deps)
	return mux
}
