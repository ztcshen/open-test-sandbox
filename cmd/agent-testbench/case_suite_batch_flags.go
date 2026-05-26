package main

import (
	"errors"

	"agent-testbench/internal/domain/casesuite"
)

type caseSuiteBatchRequestFlags struct {
	requestID      *string
	baseURL        *string
	evidenceDir    *string
	timeoutSeconds *int
	jsonOutput     *bool
	signals        stringListFlag
	changes        stringListFlag
}

func addCaseSuiteBatchRequestFlags(selection *caseSelectionCLIFlags) caseSuiteBatchRequestFlags {
	out := caseSuiteBatchRequestFlags{
		requestID:      selection.flags.String("request-id", "", "Request id for the generated batch request"),
		baseURL:        selection.flags.String("base-url", "", "Base URL for the generated batch request"),
		evidenceDir:    selection.flags.String("evidence-dir", "", "Evidence directory for the generated batch request"),
		timeoutSeconds: selection.flags.Int("timeout-seconds", 0, "Timeout seconds for the generated batch request"),
		jsonOutput:     selection.flags.Bool("json", false, "Emit a machine-readable JSON report"),
	}
	selection.flags.Var(&out.signals, "signal", "Changed path, interface text, workflow text, tag, or case text; repeat for multiple signals")
	selection.flags.Var(&out.changes, "change", "Alias for --signal; repeat for multiple changes")
	return out
}

func (f caseSuiteBatchRequestFlags) validateTimeoutNonNegative() error {
	if *f.timeoutSeconds < 0 {
		return errors.New("--timeout-seconds cannot be negative")
	}
	return nil
}

func (f caseSuiteBatchRequestFlags) signalValues() []string {
	return append(f.signals.Values(), f.changes.Values()...)
}

func (f caseSuiteBatchRequestFlags) priorityOptions(limit int) casesuite.PriorityOptions {
	return casesuite.PriorityOptions{
		Signals:        f.signalValues(),
		Limit:          limit,
		RequestID:      *f.requestID,
		BaseURL:        *f.baseURL,
		EvidenceDir:    *f.evidenceDir,
		TimeoutSeconds: *f.timeoutSeconds,
	}
}

func (f caseSuiteBatchRequestFlags) briefOptions(limit int, stabilityLimit int) casesuite.BriefOptions {
	return casesuite.BriefOptions{
		Signals:        f.signalValues(),
		Limit:          limit,
		StabilityLimit: stabilityLimit,
		RequestID:      *f.requestID,
		BaseURL:        *f.baseURL,
		EvidenceDir:    *f.evidenceDir,
		TimeoutSeconds: *f.timeoutSeconds,
	}
}
