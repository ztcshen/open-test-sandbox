package casesuite

import (
	"context"
	"sort"
	"strings"
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

type PriorityOptions struct {
	Signals        []string `json:"signals,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	RequestID      string   `json:"requestId,omitempty"`
	BaseURL        string   `json:"baseUrl,omitempty"`
	EvidenceDir    string   `json:"evidenceDir,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

type PriorityCounts struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	Blocked  int `json:"blocked"`
	Selected int `json:"selected"`
	Skipped  int `json:"skipped"`
}

type PriorityItem struct {
	InspectionItem
	Score   int      `json:"score"`
	Reasons []string `json:"reasons,omitempty"`
}

type PriorityReport struct {
	OK           bool            `json:"ok"`
	ProfileID    string          `json:"profileId"`
	GeneratedAt  string          `json:"generatedAt"`
	Filters      Filter          `json:"filters"`
	Options      PriorityOptions `json:"options"`
	Counts       PriorityCounts  `json:"counts"`
	CaseIDs      []string        `json:"caseIds"`
	Selected     []PriorityItem  `json:"selected"`
	Skipped      []PriorityItem  `json:"skipped"`
	Blocked      []PriorityItem  `json:"blocked"`
	BatchRequest BatchRequest    `json:"batchRequest"`
	Warnings     []string        `json:"warnings,omitempty"`
}

func Priority(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, cases []profile.APICase, options PriorityOptions) (PriorityReport, error) {
	filter = NormalizeFilter(filter)
	options.Signals = NormalizeStringList(options.Signals)
	inspection, err := Inspect(ctx, bundle, runtime, filter, cases)
	if err != nil {
		return PriorityReport{}, err
	}
	stability, err := Stability(ctx, bundle, runtime, filter, cases, StabilityOptions{Limit: 10})
	if err != nil {
		return PriorityReport{}, err
	}
	impact := collectImpact(bundle, options.Signals)
	stabilityByCase := map[string]StabilityItem{}
	for _, item := range stability.Items {
		stabilityByCase[item.CaseID] = item
	}
	scored := make([]PriorityItem, 0, len(inspection.Items))
	report := PriorityReport{
		OK:          true,
		ProfileID:   bundle.ID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Filters:     filter,
		Options:     options,
		Warnings:    append([]string(nil), inspection.Warnings...),
	}
	for _, item := range inspection.Items {
		row := PriorityItem{InspectionItem: item}
		row.Score, row.Reasons = priorityScore(item, stabilityByCase[item.CaseID], impact.caseReasons[item.CaseID])
		if !item.Ready {
			report.Blocked = append(report.Blocked, row)
			continue
		}
		scored = append(scored, row)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].Priority != scored[j].Priority {
			return priorityWeight(scored[i].Priority) > priorityWeight(scored[j].Priority)
		}
		if scored[i].NodeID != scored[j].NodeID {
			return scored[i].NodeID < scored[j].NodeID
		}
		return scored[i].CaseID < scored[j].CaseID
	})
	limit := options.Limit
	if limit <= 0 || limit > len(scored) {
		limit = len(scored)
	}
	report.Selected = append(report.Selected, scored[:limit]...)
	report.Skipped = append(report.Skipped, scored[limit:]...)
	for _, item := range report.Selected {
		report.CaseIDs = append(report.CaseIDs, item.CaseID)
	}
	report.Counts = PriorityCounts{
		Total:    len(inspection.Items),
		Ready:    len(scored),
		Blocked:  len(report.Blocked),
		Selected: len(report.Selected),
		Skipped:  len(report.Skipped),
	}
	report.BatchRequest = BatchRequest{
		RequestID:      strings.TrimSpace(options.RequestID),
		CaseIDs:        append([]string(nil), report.CaseIDs...),
		BaseURL:        strings.TrimSpace(options.BaseURL),
		EvidenceDir:    strings.TrimSpace(options.EvidenceDir),
		TimeoutSeconds: options.TimeoutSeconds,
	}
	if len(report.CaseIDs) == 0 {
		report.OK = false
		report.Warnings = append(report.Warnings, "no ready cases selected for prioritized execution")
	}
	return report, nil
}

func priorityScore(item InspectionItem, stability StabilityItem, impactReasons []string) (int, []string) {
	score := 0
	reasons := []string{}
	if len(impactReasons) > 0 {
		score += 100
		reasons = append(reasons, "impacted")
	}
	switch NormalizeRunState(item.LatestStatus) {
	case store.StatusFailed:
		score += 60
		reasons = append(reasons, "latest failed")
	case "not-run":
		score += 30
		reasons = append(reasons, "not run")
	case store.StatusPassed:
		score += 5
		reasons = append(reasons, "latest passed")
	}
	if stability.Unstable {
		score += 40
		reasons = append(reasons, "unstable")
	}
	if weight := priorityWeight(item.Priority); weight > 0 {
		score += weight
		reasons = append(reasons, "priority "+strings.ToLower(strings.TrimSpace(item.Priority)))
	}
	if !item.Ready {
		score -= 1000
		reasons = append(reasons, "blocked")
	}
	return score, reasons
}

func priorityWeight(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "p0", "0", "critical":
		return 30
	case "p1", "1", "high":
		return 20
	case "p2", "2", "medium":
		return 10
	default:
		return 0
	}
}
