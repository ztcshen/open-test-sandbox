package casesuite

import (
	"context"
	"time"

	"agent-testbench/internal/domain/profile"
)

type BriefOptions struct {
	Signals        []string `json:"signals,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	StabilityLimit int      `json:"stabilityLimit,omitempty"`
	RequestID      string   `json:"requestId,omitempty"`
	BaseURL        string   `json:"baseUrl,omitempty"`
	EvidenceDir    string   `json:"evidenceDir,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

type BriefCounts struct {
	Total            int `json:"total"`
	Ready            int `json:"ready"`
	Blocked          int `json:"blocked"`
	Passed           int `json:"passed"`
	Failed           int `json:"failed"`
	NotRun           int `json:"notRun"`
	Unstable         int `json:"unstable"`
	PrioritySelected int `json:"prioritySelected"`
	PrioritySkipped  int `json:"prioritySkipped"`
}

type BriefReport struct {
	OK           bool             `json:"ok"`
	ProfileID    string           `json:"profileId"`
	GeneratedAt  string           `json:"generatedAt"`
	Filters      Filter           `json:"filters"`
	Options      BriefOptions     `json:"options"`
	Counts       BriefCounts      `json:"counts"`
	Recommended  []PriorityItem   `json:"recommended"`
	Blocked      []PriorityItem   `json:"blocked"`
	Readiness    []InspectionItem `json:"readiness"`
	Coverage     []Item           `json:"coverage"`
	Stability    []StabilityItem  `json:"stability"`
	BatchRequest BatchRequest     `json:"batchRequest"`
	Warnings     []string         `json:"warnings,omitempty"`
}

func Brief(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, cases []profile.APICase, options BriefOptions) (BriefReport, error) {
	filter = NormalizeFilter(filter)
	options.Signals = NormalizeStringList(options.Signals)
	if options.StabilityLimit <= 0 {
		options.StabilityLimit = 10
	}
	coverage, err := Coverage(ctx, bundle, runtime, filter, cases)
	if err != nil {
		return BriefReport{}, err
	}
	inspection, err := Inspect(ctx, bundle, runtime, filter, cases)
	if err != nil {
		return BriefReport{}, err
	}
	stability, err := Stability(ctx, bundle, runtime, filter, cases, StabilityOptions{Limit: options.StabilityLimit})
	if err != nil {
		return BriefReport{}, err
	}
	priority := priorityFromParts(bundle, filter, inspection, stability, PriorityOptions{
		Signals:        options.Signals,
		Limit:          options.Limit,
		RequestID:      options.RequestID,
		BaseURL:        options.BaseURL,
		EvidenceDir:    options.EvidenceDir,
		TimeoutSeconds: options.TimeoutSeconds,
	})
	report := BriefReport{
		OK:           len(cases) > 0,
		ProfileID:    bundle.ID,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Filters:      filter,
		Options:      options,
		Recommended:  append([]PriorityItem(nil), priority.Selected...),
		Blocked:      append([]PriorityItem(nil), priority.Blocked...),
		Readiness:    append([]InspectionItem(nil), inspection.Items...),
		Coverage:     append([]Item(nil), coverage.Items...),
		Stability:    append([]StabilityItem(nil), stability.Items...),
		BatchRequest: priority.BatchRequest,
	}
	report.Counts = BriefCounts{
		Total:            inspection.Counts.Total,
		Ready:            inspection.Counts.Ready,
		Blocked:          inspection.Counts.Blocked,
		Passed:           coverage.Counts.Passed,
		Failed:           coverage.Counts.Failed,
		NotRun:           coverage.Counts.NotRun,
		Unstable:         stability.Counts.Unstable,
		PrioritySelected: priority.Counts.Selected,
		PrioritySkipped:  priority.Counts.Skipped,
	}
	report.Warnings = appendUniqueStrings(report.Warnings, coverage.Warnings...)
	report.Warnings = appendUniqueStrings(report.Warnings, inspection.Warnings...)
	report.Warnings = appendUniqueStrings(report.Warnings, stability.Warnings...)
	report.Warnings = appendUniqueStrings(report.Warnings, priority.Warnings...)
	if len(cases) == 0 {
		report.Warnings = appendUniqueStrings(report.Warnings, "no cases matched selector")
	}
	return report, nil
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range additions {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	return values
}
