package controlplane

type runsPayload struct {
	OK           bool             `json:"ok"`
	WorkflowRuns []map[string]any `json:"workflowRuns"`
	ReplayRuns   []map[string]any `json:"replayRuns"`
	ProbeRuns    []map[string]any `json:"probeRuns"`
}
