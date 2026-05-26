package main

import "agent-testbench/internal/domain/casesuite"

type caseSuiteImpactFlags struct {
	requestID      *string
	baseURL        *string
	timeoutSeconds *int
	jsonOutput     *bool
	actions        stringListFlag
	signals        stringListFlag
	changes        stringListFlag
}

func addCaseSuiteImpactFlags(selection caseSelectionCLIFlags, baseURLHelp string, timeoutDefault int, timeoutHelp string) caseSuiteImpactFlags {
	out := caseSuiteImpactFlags{
		requestID:      selection.flags.String("request-id", "", "Request id for the generated batch request"),
		baseURL:        selection.flags.String("base-url", "", baseURLHelp),
		timeoutSeconds: selection.flags.Int("timeout-seconds", timeoutDefault, timeoutHelp),
		jsonOutput:     selection.flags.Bool("json", false, "Emit a machine-readable JSON report"),
	}
	selection.flags.Var(&out.actions, "action", "Only select ready cases with this suggested action; repeat for multiple actions")
	selection.flags.Var(&out.signals, "signal", "Changed path, interface text, workflow text, tag, or case text; repeat for multiple signals")
	selection.flags.Var(&out.changes, "change", "Alias for --signal; repeat for multiple changes")
	return out
}

func (f caseSuiteImpactFlags) signalValues() []string {
	return append(f.signals.Values(), f.changes.Values()...)
}

func (f caseSuiteImpactFlags) planOptions(evidenceDir string) casesuite.PlanOptions {
	return casesuite.PlanOptions{
		RequestID:      *f.requestID,
		Actions:        f.actions.Values(),
		BaseURL:        *f.baseURL,
		EvidenceDir:    evidenceDir,
		TimeoutSeconds: *f.timeoutSeconds,
	}
}
