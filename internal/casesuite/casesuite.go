package casesuite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

type Filter struct {
	Filter   string   `json:"filter,omitempty"`
	NodeID   string   `json:"nodeId,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Status   string   `json:"status,omitempty"`
	Owner    string   `json:"owner,omitempty"`
	Priority string   `json:"priority,omitempty"`
}

type Counts struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	NotRun int `json:"notRun"`
}

type Item struct {
	CaseID       string   `json:"caseId"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	NodeID       string   `json:"nodeId,omitempty"`
	NodeName     string   `json:"nodeName,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Priority     string   `json:"priority,omitempty"`
	Owner        string   `json:"owner,omitempty"`
	LatestStatus string   `json:"latestStatus"`
	LatestRunID  string   `json:"latestRunId,omitempty"`
	CaseRunID    string   `json:"caseRunId,omitempty"`
	DetailURL    string   `json:"detailUrl,omitempty"`
	ElapsedMs    int64    `json:"elapsedMs,omitempty"`
	HasPassed    bool     `json:"hasPassed"`
	Reason       string   `json:"reason,omitempty"`
}

type Report struct {
	OK          bool     `json:"ok"`
	ProfileID   string   `json:"profileId"`
	GeneratedAt string   `json:"generatedAt"`
	Filters     Filter   `json:"filters"`
	Counts      Counts   `json:"counts"`
	Items       []Item   `json:"items"`
	Warnings    []string `json:"warnings,omitempty"`
}

type InspectionCounts struct {
	Total            int `json:"total"`
	Ready            int `json:"ready"`
	Blocked          int `json:"blocked"`
	Passed           int `json:"passed"`
	Failed           int `json:"failed"`
	NotRun           int `json:"notRun"`
	MissingRunnable  int `json:"missingRunnable"`
	MissingExecution int `json:"missingExecution"`
	Inactive         int `json:"inactive"`
}

type InspectionItem struct {
	CaseID             string   `json:"caseId"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	NodeID             string   `json:"nodeId,omitempty"`
	NodeName           string   `json:"nodeName,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	Priority           string   `json:"priority,omitempty"`
	Owner              string   `json:"owner,omitempty"`
	Status             string   `json:"status"`
	Ready              bool     `json:"ready"`
	HasRunnableFile    bool     `json:"hasRunnableFile"`
	HasExecutionConfig bool     `json:"hasExecutionConfig"`
	LatestStatus       string   `json:"latestStatus"`
	LatestRunID        string   `json:"latestRunId,omitempty"`
	CaseRunID          string   `json:"caseRunId,omitempty"`
	DetailURL          string   `json:"detailUrl,omitempty"`
	ElapsedMs          int64    `json:"elapsedMs,omitempty"`
	HasPassed          bool     `json:"hasPassed"`
	Issues             []string `json:"issues,omitempty"`
	SuggestedAction    string   `json:"suggestedAction,omitempty"`
}

type InspectionReport struct {
	OK          bool             `json:"ok"`
	ProfileID   string           `json:"profileId"`
	GeneratedAt string           `json:"generatedAt"`
	Filters     Filter           `json:"filters"`
	Counts      InspectionCounts `json:"counts"`
	Items       []InspectionItem `json:"items"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type PlanOptions struct {
	RequestID      string   `json:"requestId,omitempty"`
	Actions        []string `json:"actions,omitempty"`
	BaseURL        string   `json:"baseUrl,omitempty"`
	EvidenceDir    string   `json:"evidenceDir,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

type PlanCounts struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	Blocked  int `json:"blocked"`
	Selected int `json:"selected"`
	Skipped  int `json:"skipped"`
}

type BatchRequest struct {
	RequestID      string         `json:"requestId,omitempty"`
	CaseIDs        []string       `json:"caseIds"`
	BaseURL        string         `json:"baseUrl,omitempty"`
	EvidenceDir    string         `json:"evidenceDir,omitempty"`
	TimeoutSeconds int            `json:"timeoutSeconds,omitempty"`
	Overrides      map[string]any `json:"overrides,omitempty"`
}

type PlanReport struct {
	OK           bool             `json:"ok"`
	ProfileID    string           `json:"profileId"`
	GeneratedAt  string           `json:"generatedAt"`
	Filters      Filter           `json:"filters"`
	Options      PlanOptions      `json:"options"`
	Counts       PlanCounts       `json:"counts"`
	CaseIDs      []string         `json:"caseIds"`
	Selected     []InspectionItem `json:"selected"`
	Blocked      []InspectionItem `json:"blocked"`
	Skipped      []InspectionItem `json:"skipped"`
	BatchRequest BatchRequest     `json:"batchRequest"`
	Warnings     []string         `json:"warnings,omitempty"`
}

type RecordStore interface {
	ListRuns(context.Context) ([]store.Run, error)
	ListAPICaseRuns(context.Context, string) ([]store.APICaseRun, error)
}

func SelectCases(bundle profile.Bundle, filter Filter) []profile.APICase {
	filter = NormalizeFilter(filter)
	out := make([]profile.APICase, 0)
	for _, item := range bundle.APICases {
		if CaseMatches(item, filter) {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].NodeID != out[j].NodeID {
			return out[i].NodeID < out[j].NodeID
		}
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func Coverage(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, cases []profile.APICase) (Report, error) {
	report := Report{
		OK:          true,
		ProfileID:   bundle.ID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Filters:     NormalizeFilter(filter),
		Counts:      Counts{Total: len(cases)},
		Items:       []Item{},
	}
	if runtime == nil {
		report.OK = len(cases) == 0
		report.Counts.NotRun = len(cases)
		report.Warnings = append(report.Warnings, "runtime store is not configured")
	}
	records, err := RecordsForCaseIDs(ctx, runtime, CaseIDs(cases))
	if err != nil {
		return Report{}, err
	}
	stateByCase := StateByCase(records)
	nodesByID := map[string]profile.InterfaceNode{}
	for _, node := range bundle.InterfaceNodes {
		nodesByID[node.ID] = node
	}
	for _, item := range cases {
		state := stateByCase[item.ID]
		node := nodesByID[item.NodeID]
		row := Item{
			CaseID:      item.ID,
			Title:       firstNonEmpty(item.DisplayName, item.ID),
			Description: item.Description,
			NodeID:      item.NodeID,
			NodeName:    firstNonEmpty(node.DisplayName, item.NodeID),
			Tags:        append([]string(nil), item.Tags...),
			Priority:    item.Priority,
			Owner:       item.Owner,
			HasPassed:   state.HasPassed,
		}
		if state.Latest.CaseRun.ID == "" {
			row.LatestStatus = "not-run"
			row.Reason = "no run recorded in Store"
			report.Counts.NotRun++
			report.OK = false
		} else {
			row.LatestStatus = state.Latest.CaseRun.Status
			row.LatestRunID = state.Latest.Run.ID
			row.CaseRunID = state.Latest.CaseRun.ID
			row.DetailURL = DetailURL(row.CaseRunID)
			row.ElapsedMs = ElapsedMs(state.Latest.CaseRun.StartedAt, state.Latest.CaseRun.FinishedAt)
			if isPassedStatus(state.Latest.CaseRun.Status) {
				report.Counts.Passed++
			} else {
				report.Counts.Failed++
				report.OK = false
				row.Reason = firstNonEmpty(AssertionSummaryReason(state.Latest.CaseRun.AssertionSummaryJSON), "latest run is "+state.Latest.CaseRun.Status)
			}
		}
		report.Items = append(report.Items, row)
	}
	return report, nil
}

func Inspect(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, cases []profile.APICase) (InspectionReport, error) {
	coverage, err := Coverage(ctx, bundle, runtime, filter, cases)
	if err != nil {
		return InspectionReport{}, err
	}
	configs := ExecutionConfigSet(ctx, bundle, runtime)
	coverageByCase := map[string]Item{}
	for _, item := range coverage.Items {
		coverageByCase[item.CaseID] = item
	}
	report := InspectionReport{
		OK:          true,
		ProfileID:   bundle.ID,
		GeneratedAt: coverage.GeneratedAt,
		Filters:     coverage.Filters,
		Counts:      InspectionCounts{Total: len(cases)},
		Items:       []InspectionItem{},
		Warnings:    append([]string(nil), coverage.Warnings...),
	}
	if len(cases) == 0 {
		report.OK = false
		report.Warnings = append(report.Warnings, "no cases matched selector")
	}
	for _, item := range cases {
		coverageItem := coverageByCase[item.ID]
		status := CaseStatus(item)
		row := InspectionItem{
			CaseID:             item.ID,
			Title:              firstNonEmpty(item.DisplayName, item.ID),
			Description:        item.Description,
			NodeID:             item.NodeID,
			NodeName:           coverageItem.NodeName,
			Tags:               append([]string(nil), item.Tags...),
			Priority:           item.Priority,
			Owner:              item.Owner,
			Status:             status,
			HasRunnableFile:    strings.TrimSpace(item.CasePath) != "",
			HasExecutionConfig: configs[item.ID],
			LatestStatus:       coverageItem.LatestStatus,
			LatestRunID:        coverageItem.LatestRunID,
			CaseRunID:          coverageItem.CaseRunID,
			DetailURL:          coverageItem.DetailURL,
			ElapsedMs:          coverageItem.ElapsedMs,
			HasPassed:          coverageItem.HasPassed,
		}
		if row.LatestStatus == "" {
			row.LatestStatus = "not-run"
		}
		if !strings.EqualFold(status, "active") {
			row.Issues = append(row.Issues, "case status is "+status)
			report.Counts.Inactive++
		}
		if !row.HasRunnableFile {
			report.Counts.MissingRunnable++
		}
		if !row.HasExecutionConfig {
			report.Counts.MissingExecution++
		}
		if !row.HasRunnableFile && !row.HasExecutionConfig {
			row.Issues = append(row.Issues, "missing runnable case file or execution config")
		}
		row.Ready = len(row.Issues) == 0
		if row.Ready {
			report.Counts.Ready++
		} else {
			report.OK = false
			report.Counts.Blocked++
		}
		switch NormalizeRunState(row.LatestStatus) {
		case store.StatusPassed:
			report.Counts.Passed++
		case store.StatusFailed:
			report.Counts.Failed++
		default:
			report.Counts.NotRun++
		}
		row.SuggestedAction = SuggestedAction(row)
		report.Items = append(report.Items, row)
	}
	return report, nil
}

func Plan(ctx context.Context, bundle profile.Bundle, runtime RecordStore, filter Filter, cases []profile.APICase, options PlanOptions) (PlanReport, error) {
	inspection, err := Inspect(ctx, bundle, runtime, filter, cases)
	if err != nil {
		return PlanReport{}, err
	}
	options.Actions = NormalizeStringList(options.Actions)
	actionSet := actionSet(options.Actions)
	report := PlanReport{
		OK:          true,
		ProfileID:   bundle.ID,
		GeneratedAt: inspection.GeneratedAt,
		Filters:     inspection.Filters,
		Options:     options,
		Counts: PlanCounts{
			Total:   inspection.Counts.Total,
			Ready:   inspection.Counts.Ready,
			Blocked: inspection.Counts.Blocked,
		},
		Selected: []InspectionItem{},
		Blocked:  []InspectionItem{},
		Skipped:  []InspectionItem{},
		Warnings: append([]string(nil), inspection.Warnings...),
	}
	for _, item := range inspection.Items {
		if !item.Ready {
			report.Blocked = append(report.Blocked, item)
			continue
		}
		if len(actionSet) > 0 && !actionSet[item.SuggestedAction] {
			report.Skipped = append(report.Skipped, item)
			continue
		}
		report.Selected = append(report.Selected, item)
		report.CaseIDs = append(report.CaseIDs, item.CaseID)
	}
	report.Counts.Selected = len(report.Selected)
	report.Counts.Skipped = len(report.Skipped)
	report.BatchRequest = BatchRequest{
		RequestID:      strings.TrimSpace(options.RequestID),
		CaseIDs:        append([]string(nil), report.CaseIDs...),
		BaseURL:        strings.TrimSpace(options.BaseURL),
		EvidenceDir:    strings.TrimSpace(options.EvidenceDir),
		TimeoutSeconds: options.TimeoutSeconds,
	}
	if len(report.CaseIDs) == 0 {
		report.OK = false
		report.Warnings = append(report.Warnings, "no ready cases selected for execution")
	}
	return report, nil
}

type State struct {
	Latest    store.APICaseRunRecord
	HasPassed bool
}

func StateByCase(records []store.APICaseRunRecord) map[string]State {
	out := map[string]State{}
	for _, record := range records {
		caseID := record.CaseRun.CaseID
		state := out[caseID]
		if isPassedStatus(record.CaseRun.Status) {
			state.HasPassed = true
		}
		if state.Latest.CaseRun.ID == "" || RecordNewer(record, state.Latest) {
			state.Latest = record
		}
		out[caseID] = state
	}
	return out
}

func RecordNewer(left store.APICaseRunRecord, right store.APICaseRunRecord) bool {
	if left.CaseRun.CreatedAt.After(right.CaseRun.CreatedAt) {
		return true
	}
	return left.CaseRun.CreatedAt.Equal(right.CaseRun.CreatedAt) && left.CaseRun.ID > right.CaseRun.ID
}

func RecordsForCaseIDs(ctx context.Context, runtime RecordStore, caseIDs []string) ([]store.APICaseRunRecord, error) {
	if runtime == nil || len(caseIDs) == 0 {
		return []store.APICaseRunRecord{}, nil
	}
	if fast, ok := runtime.(interface {
		ListAPICaseRunRecordsForCaseIDs(context.Context, []string) ([]store.APICaseRunRecord, error)
	}); ok {
		return fast.ListAPICaseRunRecordsForCaseIDs(ctx, caseIDs)
	}
	caseSet := map[string]bool{}
	for _, id := range caseIDs {
		caseSet[id] = true
	}
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]store.APICaseRunRecord, 0)
	for _, run := range runs {
		caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		for _, caseRun := range caseRuns {
			if caseSet[caseRun.CaseID] {
				out = append(out, store.APICaseRunRecord{Run: run, CaseRun: caseRun})
			}
		}
	}
	return out, nil
}

func ExecutionConfigSet(ctx context.Context, bundle profile.Bundle, runtime RecordStore) map[string]bool {
	out := map[string]bool{}
	addProfileTemplateConfigs(out, bundle.TemplateConfigs)
	if catalogRuntime, ok := runtime.(interface {
		GetProfileCatalog(context.Context) (store.ProfileCatalog, error)
	}); ok {
		if catalog, err := catalogRuntime.GetProfileCatalog(ctx); err == nil {
			addCatalogTemplateConfigs(out, catalog.TemplateConfigs)
		}
	}
	return out
}

func addProfileTemplateConfigs(out map[string]bool, configs []profile.TemplateConfig) {
	for _, config := range configs {
		if !activeStatus(config.Status) {
			continue
		}
		if config.ScopeType == "case" && strings.TrimSpace(config.ScopeID) != "" {
			out[strings.TrimSpace(config.ScopeID)] = true
			continue
		}
		if caseID := executionConfigCaseID(config.ConfigJSON); caseID != "" {
			out[caseID] = true
		}
	}
}

func addCatalogTemplateConfigs(out map[string]bool, configs []store.CatalogTemplateConfig) {
	for _, config := range configs {
		if !activeStatus(config.Status) {
			continue
		}
		if config.ScopeType == "case" && strings.TrimSpace(config.ScopeID) != "" {
			out[strings.TrimSpace(config.ScopeID)] = true
			continue
		}
		if caseID := executionConfigCaseID(config.ConfigJSON); caseID != "" {
			out[caseID] = true
		}
	}
}

func executionConfigCaseID(configJSON string) string {
	var payload struct {
		CaseID        string         `json:"caseId"`
		CaseExecution map[string]any `json:"caseExecution"`
	}
	if json.Unmarshal([]byte(configJSON), &payload) != nil {
		return ""
	}
	if strings.TrimSpace(payload.CaseID) == "" || len(payload.CaseExecution) == 0 {
		return ""
	}
	return strings.TrimSpace(payload.CaseID)
}

func activeStatus(status string) bool {
	return strings.TrimSpace(status) == "" || strings.EqualFold(strings.TrimSpace(status), "active")
}

func SuggestedAction(item InspectionItem) string {
	if !strings.EqualFold(item.Status, "active") {
		return "review-status"
	}
	if !item.HasRunnableFile && !item.HasExecutionConfig {
		return "add-runnable-source"
	}
	if NormalizeRunState(item.LatestStatus) == store.StatusFailed {
		return "rerun"
	}
	if NormalizeRunState(item.LatestStatus) == "not-run" {
		return "run"
	}
	return "keep"
}

func actionSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func CaseMatches(item profile.APICase, filter Filter) bool {
	filter = NormalizeFilter(filter)
	if filter.NodeID != "" && item.NodeID != filter.NodeID {
		return false
	}
	if filter.Status != "" && !strings.EqualFold(CaseStatus(item), filter.Status) {
		return false
	}
	if filter.Owner != "" && !strings.EqualFold(strings.TrimSpace(item.Owner), filter.Owner) {
		return false
	}
	if filter.Priority != "" && !strings.EqualFold(strings.TrimSpace(item.Priority), filter.Priority) {
		return false
	}
	if len(filter.Tags) > 0 && !HasAllTags(item.Tags, filter.Tags) {
		return false
	}
	return MatchesText(filter.Filter, item.ID, item.DisplayName, item.Description, item.Scenario, item.Owner, item.Priority, strings.Join(item.Tags, " "), item.NodeID)
}

func NormalizeFilter(filter Filter) Filter {
	filter.Filter = strings.TrimSpace(filter.Filter)
	filter.NodeID = strings.TrimSpace(filter.NodeID)
	filter.Tags = NormalizeStringList(filter.Tags)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Owner = strings.TrimSpace(filter.Owner)
	filter.Priority = strings.TrimSpace(filter.Priority)
	return filter
}

func CaseStatus(item profile.APICase) string {
	if strings.TrimSpace(item.Status) == "" {
		return "active"
	}
	return item.Status
}

func HasAllTags(actual []string, required []string) bool {
	actualSet := map[string]bool{}
	for _, tag := range actual {
		if normalized := SearchText(tag); normalized != "" {
			actualSet[normalized] = true
		}
	}
	for _, tag := range required {
		if normalized := SearchText(tag); normalized != "" && !actualSet[normalized] {
			return false
		}
	}
	return true
}

func MatchesText(filter string, values ...string) bool {
	needle := SearchText(filter)
	if needle == "" {
		return true
	}
	for _, value := range values {
		haystack := SearchText(value)
		if haystack != "" && (strings.Contains(haystack, needle) || strings.Contains(needle, haystack)) {
			return true
		}
	}
	return false
}

func SearchText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "", "/", "")
	return replacer.Replace(value)
}

func NormalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key := strings.ToLower(part)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, part)
		}
	}
	return out
}

func CaseIDs(cases []profile.APICase) []string {
	out := make([]string, 0, len(cases))
	for _, item := range cases {
		if strings.TrimSpace(item.ID) != "" {
			out = append(out, item.ID)
		}
	}
	return out
}

func DetailURL(caseRunID string) string {
	if strings.TrimSpace(caseRunID) == "" {
		return ""
	}
	return "/api/case-run/evidence?caseRunId=" + url.QueryEscape(caseRunID)
}

func AssertionSummaryReason(summaryJSON string) string {
	var payload struct {
		FailureReason string `json:"failureReason"`
		ErrorCount    int    `json:"errorCount"`
	}
	if json.Unmarshal([]byte(summaryJSON), &payload) != nil {
		return ""
	}
	if strings.TrimSpace(payload.FailureReason) != "" {
		return payload.FailureReason
	}
	if payload.ErrorCount > 0 {
		return fmt.Sprintf("assertion errors: %d", payload.ErrorCount)
	}
	return ""
}

func ElapsedMs(started time.Time, finished time.Time) int64 {
	if started.IsZero() || finished.IsZero() || finished.Before(started) {
		return 0
	}
	return finished.Sub(started).Milliseconds()
}

func NormalizeRunState(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "notrun", "not-run", "missing", "never-run":
		return "not-run"
	case "pass", "passed", "success", "ok":
		return store.StatusPassed
	case "fail", "failed", "error":
		return store.StatusFailed
	default:
		return value
	}
}

func RunStateSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		if normalized := NormalizeRunState(value); normalized != "" {
			out[normalized] = true
		}
	}
	return out
}

func isPassedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass", "passed", "success", "ok":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
