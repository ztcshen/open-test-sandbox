package casesuite

import (
	"context"
	"sort"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

type StabilityOptions struct {
	Limit int `json:"limit,omitempty"`
}

type StabilityCounts struct {
	Total    int `json:"total"`
	Stable   int `json:"stable"`
	Unstable int `json:"unstable"`
	NotRun   int `json:"notRun"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
}

type StabilityRecentRun struct {
	RunID     string `json:"runId"`
	CaseRunID string `json:"caseRunId"`
	Status    string `json:"status"`
	DetailURL string `json:"detailUrl,omitempty"`
	ElapsedMs int64  `json:"elapsedMs,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type StabilityItem struct {
	CaseID       string               `json:"caseId"`
	Title        string               `json:"title"`
	Description  string               `json:"description,omitempty"`
	NodeID       string               `json:"nodeId,omitempty"`
	NodeName     string               `json:"nodeName,omitempty"`
	Tags         []string             `json:"tags,omitempty"`
	Priority     string               `json:"priority,omitempty"`
	Owner        string               `json:"owner,omitempty"`
	LatestStatus string               `json:"latestStatus"`
	Passed       int                  `json:"passed"`
	Failed       int                  `json:"failed"`
	Transitions  int                  `json:"transitions"`
	Unstable     bool                 `json:"unstable"`
	Reason       string               `json:"reason,omitempty"`
	Recent       []StabilityRecentRun `json:"recent,omitempty"`
}

type StabilityReport struct {
	OK          bool             `json:"ok"`
	ProfileID   string           `json:"profileId"`
	GeneratedAt string           `json:"generatedAt"`
	Filters     Filter           `json:"filters"`
	Options     StabilityOptions `json:"options"`
	Counts      StabilityCounts  `json:"counts"`
	Items       []StabilityItem  `json:"items"`
	Warnings    []string         `json:"warnings,omitempty"`
}

func Stability(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, cases []profile.APICase, options StabilityOptions) (StabilityReport, error) {
	filter = NormalizeFilter(filter)
	if options.Limit <= 0 {
		options.Limit = 10
	}
	report := StabilityReport{
		OK:          true,
		ProfileID:   bundle.ID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Filters:     filter,
		Options:     options,
		Counts:      StabilityCounts{Total: len(cases)},
		Items:       []StabilityItem{},
	}
	if runtime == nil {
		report.OK = len(cases) == 0
		report.Counts.NotRun = len(cases)
		report.Warnings = append(report.Warnings, "runtime store is not configured")
	}
	records, err := RecordsForCaseIDs(ctx, runtime, CaseIDs(cases))
	if err != nil {
		return StabilityReport{}, err
	}
	recordsByCase := recordsGroupedByCase(records)
	nodesByID := map[string]profile.InterfaceNode{}
	for _, node := range bundle.InterfaceNodes {
		nodesByID[node.ID] = node
	}
	for _, item := range cases {
		node := nodesByID[item.NodeID]
		row := StabilityItem{
			CaseID:       item.ID,
			Title:        firstNonEmpty(item.DisplayName, item.ID),
			Description:  item.Description,
			NodeID:       item.NodeID,
			NodeName:     firstNonEmpty(node.DisplayName, item.NodeID),
			Tags:         append([]string(nil), item.Tags...),
			Priority:     item.Priority,
			Owner:        item.Owner,
			LatestStatus: "not-run",
		}
		caseRecords := recordsByCase[item.ID]
		if len(caseRecords) == 0 {
			row.Reason = "no run recorded in Store"
			report.Counts.NotRun++
			report.OK = false
			report.Items = append(report.Items, row)
			continue
		}
		if len(caseRecords) > options.Limit {
			caseRecords = caseRecords[:options.Limit]
		}
		row.Recent = stabilityRecentRuns(caseRecords)
		row.LatestStatus = NormalizeRunState(caseRecords[0].CaseRun.Status)
		for index, record := range caseRecords {
			status := NormalizeRunState(record.CaseRun.Status)
			switch status {
			case store.StatusPassed:
				row.Passed++
			case store.StatusFailed:
				row.Failed++
			}
			if index > 0 && status != NormalizeRunState(caseRecords[index-1].CaseRun.Status) {
				row.Transitions++
			}
		}
		row.Unstable = row.Passed > 0 && row.Failed > 0 && row.Transitions > 0
		if row.Unstable {
			row.Reason = "recent runs include both passed and failed results"
			report.Counts.Unstable++
			report.OK = false
		} else {
			report.Counts.Stable++
		}
		if row.LatestStatus == store.StatusPassed {
			report.Counts.Passed++
		}
		if row.LatestStatus == store.StatusFailed {
			report.Counts.Failed++
		}
		report.Items = append(report.Items, row)
	}
	return report, nil
}

func recordsGroupedByCase(records []store.APICaseRunRecord) map[string][]store.APICaseRunRecord {
	out := map[string][]store.APICaseRunRecord{}
	for _, record := range records {
		caseID := record.CaseRun.CaseID
		out[caseID] = append(out[caseID], record)
	}
	for caseID := range out {
		sort.SliceStable(out[caseID], func(i, j int) bool {
			return RecordNewer(out[caseID][i], out[caseID][j])
		})
	}
	return out
}

func stabilityRecentRuns(records []store.APICaseRunRecord) []StabilityRecentRun {
	out := make([]StabilityRecentRun, 0, len(records))
	for _, record := range records {
		out = append(out, StabilityRecentRun{
			RunID:     record.Run.ID,
			CaseRunID: record.CaseRun.ID,
			Status:    NormalizeRunState(record.CaseRun.Status),
			DetailURL: DetailURL(record.CaseRun.ID),
			ElapsedMs: ElapsedMs(record.CaseRun.StartedAt, record.CaseRun.FinishedAt),
			CreatedAt: record.CaseRun.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	return out
}
