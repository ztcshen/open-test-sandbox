package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agent-testbench/internal/domain/casesuite"
	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

type caseListReport struct {
	OK        bool           `json:"ok"`
	ProfileID string         `json:"profileId"`
	Count     int            `json:"count"`
	Filters   caseListFilter `json:"filters"`
	Items     []caseListItem `json:"items"`
}

type caseListFilter struct {
	Filter   string   `json:"filter,omitempty"`
	NodeID   string   `json:"nodeId,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Status   string   `json:"status,omitempty"`
	Owner    string   `json:"owner,omitempty"`
	Priority string   `json:"priority,omitempty"`
}

type caseListItem struct {
	ID                   string   `json:"id"`
	DisplayName          string   `json:"displayName,omitempty"`
	Description          string   `json:"description,omitempty"`
	NodeID               string   `json:"nodeId,omitempty"`
	CaseType             string   `json:"caseType,omitempty"`
	Scenario             string   `json:"scenario,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	Priority             string   `json:"priority,omitempty"`
	Owner                string   `json:"owner,omitempty"`
	Status               string   `json:"status,omitempty"`
	RequiredForAdmission bool     `json:"requiredForAdmission"`
	SortOrder            int      `json:"sortOrder,omitempty"`
	HasRunnableFile      bool     `json:"hasRunnableFile"`
	HasExecutionConfig   bool     `json:"hasExecutionConfig"`
}

func runCaseDiscover(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case discover", "")
	offlineTemplatePackage := selection.flags.Bool("offline-template-package", false, "Read the template package directly for offline review")
	jsonOutput := selection.flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := selection.parse(args); err != nil {
		return err
	}
	discoveryProfileRef, resolvedStoreURL, err := resolveDiscoveryInputs(*selection.profilePath, *selection.storeRef, *selection.storeURL, *offlineTemplatePackage)
	if err != nil {
		return err
	}
	bundle, sourceStore, cleanup, err := loadInterfaceNodeReportBundle(ctx, discoveryProfileRef, *selection.profileHome, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report := caseList(ctx, bundle, sourceStore, selection.caseListFilter())
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	for _, item := range report.Items {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.DisplayName, item.NodeID, item.Status, item.Priority, strings.Join(item.Tags, ","))
	}
	return nil
}

func caseList(ctx context.Context, bundle profile.Bundle, runtime store.Store, filters caseListFilter) caseListReport {
	cases := append([]profile.APICase(nil), bundle.APICases...)
	sort.SliceStable(cases, func(i, j int) bool {
		if cases[i].NodeID != cases[j].NodeID {
			return cases[i].NodeID < cases[j].NodeID
		}
		if cases[i].SortOrder != cases[j].SortOrder {
			return cases[i].SortOrder < cases[j].SortOrder
		}
		return cases[i].ID < cases[j].ID
	})
	executionConfigs := casesuite.ExecutionConfigSet(ctx, bundle, runtime)
	report := caseListReport{OK: true, ProfileID: bundle.ID, Filters: normalizeCaseListFilter(filters)}
	for _, item := range cases {
		if !matchesCaseFilters(item, filters) {
			continue
		}
		report.Items = append(report.Items, caseListItem{
			ID:                   item.ID,
			DisplayName:          item.DisplayName,
			Description:          item.Description,
			NodeID:               item.NodeID,
			CaseType:             item.CaseType,
			Scenario:             item.Scenario,
			Tags:                 append([]string(nil), item.Tags...),
			Priority:             item.Priority,
			Owner:                item.Owner,
			Status:               effectiveCaseStatus(item),
			RequiredForAdmission: item.RequiredForAdmission,
			SortOrder:            item.SortOrder,
			HasRunnableFile:      strings.TrimSpace(item.CasePath) != "",
			HasExecutionConfig:   executionConfigs[item.ID],
		})
	}
	report.Count = len(report.Items)
	return report
}

func normalizeCaseListFilter(filters caseListFilter) caseListFilter {
	filters.Filter = strings.TrimSpace(filters.Filter)
	filters.NodeID = strings.TrimSpace(filters.NodeID)
	filters.Status = strings.TrimSpace(filters.Status)
	filters.Owner = strings.TrimSpace(filters.Owner)
	filters.Priority = strings.TrimSpace(filters.Priority)
	filters.Tags = normalizeStringList(filters.Tags)
	return filters
}

func matchesCaseFilters(item profile.APICase, filters caseListFilter) bool {
	filters = normalizeCaseListFilter(filters)
	if filters.NodeID != "" && item.NodeID != filters.NodeID {
		return false
	}
	if filters.Status != "" && !strings.EqualFold(effectiveCaseStatus(item), filters.Status) {
		return false
	}
	if filters.Owner != "" && !strings.EqualFold(strings.TrimSpace(item.Owner), filters.Owner) {
		return false
	}
	if filters.Priority != "" && !strings.EqualFold(strings.TrimSpace(item.Priority), filters.Priority) {
		return false
	}
	if len(filters.Tags) > 0 && !caseHasAllTags(item.Tags, filters.Tags) {
		return false
	}
	return matchesDiscoveryFilter(filters.Filter, item.ID, item.DisplayName, item.Scenario, item.Description, item.Owner, item.Priority, strings.Join(item.Tags, " "))
}

func effectiveCaseStatus(item profile.APICase) string {
	status := strings.TrimSpace(item.Status)
	if status == "" {
		return "active"
	}
	return status
}

func caseHasAllTags(actual []string, required []string) bool {
	actualSet := map[string]bool{}
	for _, tag := range actual {
		normalized := normalizedDiscoveryText(tag)
		if normalized != "" {
			actualSet[normalized] = true
		}
	}
	for _, tag := range required {
		normalized := normalizedDiscoveryText(tag)
		if normalized != "" && !actualSet[normalized] {
			return false
		}
	}
	return true
}
