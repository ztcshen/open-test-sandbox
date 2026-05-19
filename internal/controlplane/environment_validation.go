package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"open-test-sandbox/internal/store"
)

// ValidateEnvironmentPublishable verifies that the recorded environment gate
// matches persisted run Evidence and a real SkyWalking topology row.
func ValidateEnvironmentPublishable(ctx context.Context, runtime store.Store, env store.Environment) error {
	if runtime == nil {
		return fmt.Errorf("environment %s is not publishable: runtime Store is not configured", env.ID)
	}
	if env.LastVerificationStatus != store.StatusPassed || strings.TrimSpace(env.LastVerificationRunID) == "" || !env.EvidenceComplete || !env.TopologyComplete {
		return fmt.Errorf("environment %s is not publishable: verification must pass with complete Evidence and SkyWalking topology", env.ID)
	}
	run, err := runtime.GetRun(ctx, env.LastVerificationRunID)
	if err != nil {
		return fmt.Errorf("environment %s is not publishable: verification run %s was not found in Store", env.ID, env.LastVerificationRunID)
	}
	if run.Status != store.StatusPassed {
		return fmt.Errorf("environment %s is not publishable: verification run %s status is %s", env.ID, run.ID, run.Status)
	}
	records, err := runtime.ListEvidence(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("environment %s is not publishable: verification Evidence could not be read: %w", env.ID, err)
	}
	if len(records) == 0 {
		return fmt.Errorf("environment %s is not publishable: verification run %s has no indexed Evidence", env.ID, run.ID)
	}
	rows, err := runtime.ListTraceTopologies(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("environment %s is not publishable: SkyWalking topology could not be read: %w", env.ID, err)
	}
	for _, row := range rows {
		if completeSkyWalkingTopologyRow(row) {
			return nil
		}
	}
	return fmt.Errorf("environment %s is not publishable: verification run %s has no complete SkyWalking topology", env.ID, run.ID)
}

func completeSkyWalkingTopologyRow(row store.TraceTopology) bool {
	if row.Status != "complete" || strings.TrimSpace(row.TraceID) == "" {
		return false
	}
	var topology struct {
		Provider       string `json:"provider"`
		Source         string `json:"source"`
		Status         string `json:"status"`
		TraceID        string `json:"traceId"`
		ConfirmedEdges []struct {
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"confirmedEdges"`
		ObservedNodes []string `json:"observedNodes"`
	}
	if err := json.Unmarshal([]byte(row.TopologyJSON), &topology); err != nil {
		return false
	}
	if !strings.EqualFold(topology.Provider, "skywalking") && !strings.EqualFold(topology.Source, "skywalking") {
		return false
	}
	if topology.Status != "complete" || strings.TrimSpace(topology.TraceID) == "" {
		return false
	}
	if len(topology.ConfirmedEdges) == 0 || len(topology.ObservedNodes) == 0 {
		return false
	}
	for _, edge := range topology.ConfirmedEdges {
		if strings.TrimSpace(edge.Source) != "" && strings.TrimSpace(edge.Target) != "" {
			return true
		}
	}
	return false
}
