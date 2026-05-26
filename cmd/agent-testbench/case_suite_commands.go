package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agent-testbench/internal/domain/casesuite"
	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

func runCaseSuite(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing case suite command")
	}
	switch args[0] {
	case "report":
		return runCaseSuiteReport(ctx, args[1:])
	case "coverage":
		return runCaseSuiteCoverage(ctx, args[1:])
	case "stability":
		return runCaseSuiteStability(ctx, args[1:])
	case "priority":
		return runCaseSuitePriority(ctx, args[1:])
	case "brief":
		return runCaseSuiteBrief(ctx, args[1:])
	case "quality":
		return runCaseSuiteQuality(ctx, args[1:])
	case "quality-plan":
		return runCaseSuiteQualityPlan(ctx, args[1:])
	case "quality-report":
		return runCaseSuiteQualityReport(ctx, args[1:])
	case "inspect":
		return runCaseSuiteInspect(ctx, args[1:])
	case "plan":
		return runCaseSuitePlan(ctx, args[1:])
	case "impact":
		return runCaseSuiteImpact(ctx, args[1:])
	case "impact-report":
		return runCaseSuiteImpactReport(ctx, args[1:])
	default:
		return fmt.Errorf("unknown case suite command: %s", args[0])
	}
}

type caseSuiteCoverageReport struct {
	OK             bool             `json:"ok"`
	ProfileID      string           `json:"profileId"`
	GeneratedAt    string           `json:"generatedAt"`
	Filters        casesuite.Filter `json:"filters"`
	Counts         casesuite.Counts `json:"counts"`
	Items          []casesuite.Item `json:"items"`
	Warnings       []string         `json:"warnings,omitempty"`
	SourceStoreURL string           `json:"sourceStoreUrl,omitempty"`
}

func runCaseSuiteCoverage(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case suite coverage", "active")
	jsonOutput := selection.flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := selection.parse(args); err != nil {
		return err
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	report, err := caseSuiteCoverage(ctx, selected.Bundle, selected.Store, selected.SourceStoreURL, selected.Filters, selected.Cases)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteCoverage(report)
	return nil
}

func caseSuiteCoverage(ctx context.Context, bundle profile.Bundle, runtime store.Store, sourceStoreURL string, filters caseListFilter, cases []profile.APICase) (caseSuiteCoverageReport, error) {
	report, err := casesuite.Coverage(ctx, bundle, runtime, caseSuiteFilter(filters), cases)
	if err != nil {
		return caseSuiteCoverageReport{}, err
	}
	return caseSuiteCoverageReport{
		OK:             report.OK,
		ProfileID:      report.ProfileID,
		GeneratedAt:    report.GeneratedAt,
		Filters:        report.Filters,
		Counts:         report.Counts,
		Items:          report.Items,
		Warnings:       report.Warnings,
		SourceStoreURL: sourceStoreURL,
	}, nil
}

func caseSuiteFilter(filters caseListFilter) casesuite.Filter {
	filters = normalizeCaseListFilter(filters)
	return casesuite.Filter{
		Filter:   filters.Filter,
		NodeID:   filters.NodeID,
		Tags:     append([]string(nil), filters.Tags...),
		Status:   filters.Status,
		Owner:    filters.Owner,
		Priority: filters.Priority,
	}
}

func printCaseSuiteCoverage(report caseSuiteCoverageReport) {
	fmt.Println("Case Suite Coverage")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Passed: %d Failed: %d Not Run: %d\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed, report.Counts.NotRun)
	for _, item := range report.Items {
		fmt.Printf("- %s [%s]", item.CaseID, item.LatestStatus)
		if item.CaseRunID != "" {
			fmt.Printf(" %s", item.CaseRunID)
		}
		if item.Reason != "" {
			fmt.Printf(" %s", item.Reason)
		}
		fmt.Println()
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteStability(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case suite stability", "active")
	limit := selection.flags.Int("limit", 10, "Recent runs per case to analyze")
	jsonOutput := selection.flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := selection.parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	report, err := casesuite.Stability(ctx, selected.Bundle, selected.Store, caseSuiteFilter(selected.Filters), selected.Cases, casesuite.StabilityOptions{Limit: *limit})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteStability(report)
	return nil
}

func printCaseSuiteStability(report casesuite.StabilityReport) {
	fmt.Println("Case Suite Stability")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Stable: %d Unstable: %d Not Run: %d\n", report.Counts.Total, report.Counts.Stable, report.Counts.Unstable, report.Counts.NotRun)
	for _, item := range report.Items {
		fmt.Printf("- %s latest=%s transitions=%d unstable=%t\n", item.CaseID, item.LatestStatus, item.Transitions, item.Unstable)
		if item.Reason != "" {
			fmt.Printf("  reason: %s\n", item.Reason)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuitePriority(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case suite priority", "active")
	limit := selection.flags.Int("limit", 0, "Maximum ready cases to select; 0 selects all ready cases")
	batchFlags := addCaseSuiteBatchRequestFlags(selection)
	if err := selection.parse(args); err != nil {
		return err
	}
	if *limit < 0 {
		return errors.New("--limit cannot be negative")
	}
	if err := batchFlags.validateTimeoutNonNegative(); err != nil {
		return err
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	report, err := casesuite.Priority(ctx, selected.Bundle, selected.Store, caseSuiteFilter(selected.Filters), selected.Cases, batchFlags.priorityOptions(*limit))
	if err != nil {
		return err
	}
	if *batchFlags.jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuitePriority(report)
	return nil
}

func printCaseSuitePriority(report casesuite.PriorityReport) {
	fmt.Println("Case Suite Priority")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d Selected: %d Skipped: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked, report.Counts.Selected, report.Counts.Skipped)
	for _, item := range report.Selected {
		fmt.Printf("- %s score=%d latest=%s\n", item.CaseID, item.Score, item.LatestStatus)
		for _, reason := range item.Reasons {
			fmt.Printf("  reason: %s\n", reason)
		}
	}
	for _, item := range report.Blocked {
		fmt.Printf("- blocked %s score=%d\n", item.CaseID, item.Score)
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteBrief(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case suite brief", "active")
	limit := selection.flags.Int("limit", 0, "Maximum ready cases to recommend; 0 recommends all ready cases")
	stabilityLimit := selection.flags.Int("stability-limit", 10, "Recent runs per case to analyze")
	batchFlags := addCaseSuiteBatchRequestFlags(selection)
	if err := selection.parse(args); err != nil {
		return err
	}
	if *limit < 0 {
		return errors.New("--limit cannot be negative")
	}
	if *stabilityLimit <= 0 {
		return errors.New("--stability-limit must be greater than zero")
	}
	if err := batchFlags.validateTimeoutNonNegative(); err != nil {
		return err
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	report, err := casesuite.Brief(ctx, selected.Bundle, selected.Store, caseSuiteFilter(selected.Filters), selected.Cases, batchFlags.briefOptions(*limit, *stabilityLimit))
	if err != nil {
		return err
	}
	if *batchFlags.jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteBrief(report)
	return nil
}

func printCaseSuiteBrief(report casesuite.BriefReport) {
	fmt.Println("Case Suite Brief")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d Passed: %d Failed: %d Not Run: %d Unstable: %d Recommended: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked, report.Counts.Passed, report.Counts.Failed, report.Counts.NotRun, report.Counts.Unstable, report.Counts.PrioritySelected)
	for _, item := range report.Recommended {
		fmt.Printf("- %s score=%d latest=%s\n", item.CaseID, item.Score, item.LatestStatus)
		for _, reason := range item.Reasons {
			fmt.Printf("  reason: %s\n", reason)
		}
	}
	for _, item := range report.Blocked {
		fmt.Printf("- blocked %s\n", item.CaseID)
		for _, issue := range item.Issues {
			fmt.Printf("  issue: %s\n", issue)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteQuality(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case suite quality", "active")
	jsonOutput := selection.flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := selection.parse(args); err != nil {
		return err
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	report, err := casesuite.Quality(ctx, selected.Bundle, selected.Store, caseSuiteFilter(selected.Filters), selected.Cases)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteQuality(report)
	return nil
}

func printCaseSuiteQuality(report casesuite.QualityReport) {
	fmt.Println("Case Suite Quality")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Nodes: %d Without Cases: %d Cases: %d Complete: %d Incomplete: %d\n", report.Counts.Nodes, report.Counts.NodesWithoutCases, report.Counts.Cases, report.Counts.CompleteCases, report.Counts.IncompleteCases)
	if report.Counts.InvalidStatus > 0 || report.Counts.NonExecutableLifecycle > 0 {
		fmt.Printf("Lifecycle: non-executable=%d invalid=%d\n", report.Counts.NonExecutableLifecycle, report.Counts.InvalidStatus)
	}
	for _, item := range report.Nodes {
		fmt.Printf("- node %s\n", item.NodeID)
		for _, issue := range item.Issues {
			fmt.Printf("  issue: %s\n", issue)
		}
	}
	for _, item := range report.Cases {
		if item.Complete {
			continue
		}
		fmt.Printf("- case %s\n", item.CaseID)
		for _, issue := range item.Issues {
			fmt.Printf("  issue: %s\n", issue)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteQualityPlan(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case suite quality-plan", "active")
	jsonOutput := selection.flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := selection.parse(args); err != nil {
		return err
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	report, err := casesuite.QualityPlan(ctx, selected.Bundle, selected.Store, caseSuiteFilter(selected.Filters), selected.Cases)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteQualityPlan(report)
	return nil
}

func printCaseSuiteQualityPlan(report casesuite.QualityPlanReport) {
	fmt.Println("Case Suite Quality Plan")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Draft Case: %d Complete Metadata: %d Review Lifecycle: %d Add Runnable: %d Add Execution: %d\n", report.Counts.Total, report.Counts.DraftCase, report.Counts.CompleteMetadata, report.Counts.ReviewLifecycle, report.Counts.AddRunnable, report.Counts.AddExecution)
	for _, item := range report.Actions {
		switch item.Type {
		case "draft-case":
			fmt.Printf("- draft %s for node %s\n", item.SuggestedCaseID, item.NodeID)
		case "review-case-lifecycle":
			fmt.Printf("- review lifecycle %s\n", item.CaseID)
		default:
			fmt.Printf("- %s %s\n", item.Type, item.CaseID)
		}
		if len(item.Fields) > 0 {
			fmt.Printf("  fields: %s\n", strings.Join(item.Fields, ","))
		}
		if item.Reason != "" {
			fmt.Printf("  reason: %s\n", item.Reason)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteInspect(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case suite inspect", "active")
	jsonOutput := selection.flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := selection.parse(args); err != nil {
		return err
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	report, err := casesuite.Inspect(ctx, selected.Bundle, selected.Store, caseSuiteFilter(selected.Filters), selected.Cases)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteInspection(report)
	return nil
}

func printCaseSuiteInspection(report casesuite.InspectionReport) {
	fmt.Println("Case Suite Inspection")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d Passed: %d Failed: %d Not Run: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked, report.Counts.Passed, report.Counts.Failed, report.Counts.NotRun)
	for _, item := range report.Items {
		fmt.Printf("- %s ready=%t latest=%s action=%s\n", item.CaseID, item.Ready, item.LatestStatus, item.SuggestedAction)
		for _, issue := range item.Issues {
			fmt.Printf("  issue: %s\n", issue)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuitePlan(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case suite plan", "active")
	requestID := selection.flags.String("request-id", "", "Request id for the generated batch request")
	baseURL := selection.flags.String("base-url", "", "Base URL for the generated batch request")
	evidenceDir := selection.flags.String("evidence-dir", "", "Evidence directory for the generated batch request")
	timeoutSeconds := selection.flags.Int("timeout-seconds", 0, "Timeout seconds for the generated batch request")
	jsonOutput := selection.flags.Bool("json", false, "Emit a machine-readable JSON report")
	var actions stringListFlag
	selection.flags.Var(&actions, "action", "Only select ready cases with this suggested action; repeat for multiple actions")
	if err := selection.parse(args); err != nil {
		return err
	}
	if *timeoutSeconds < 0 {
		return errors.New("--timeout-seconds cannot be negative")
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	report, err := casesuite.Plan(ctx, selected.Bundle, selected.Store, caseSuiteFilter(selected.Filters), selected.Cases, casesuite.PlanOptions{
		RequestID:      *requestID,
		Actions:        actions.Values(),
		BaseURL:        *baseURL,
		EvidenceDir:    *evidenceDir,
		TimeoutSeconds: *timeoutSeconds,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuitePlan(report)
	return nil
}

func printCaseSuitePlan(report casesuite.PlanReport) {
	fmt.Println("Case Suite Plan")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d Selected: %d Skipped: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked, report.Counts.Selected, report.Counts.Skipped)
	for _, item := range report.Selected {
		fmt.Printf("- %s action=%s latest=%s\n", item.CaseID, item.SuggestedAction, item.LatestStatus)
	}
	for _, item := range report.Blocked {
		fmt.Printf("- blocked %s action=%s\n", item.CaseID, item.SuggestedAction)
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteImpact(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlagsWithFilterHelp("case suite impact", "active", "Additional case selector filter")
	impactFlags := addCaseSuiteImpactFlags(selection, "Base URL for the generated batch request", 0, "Timeout seconds for the generated batch request")
	evidenceDir := selection.flags.String("evidence-dir", "", "Evidence directory for the generated batch request")
	if err := selection.parse(args); err != nil {
		return err
	}
	if *impactFlags.timeoutSeconds < 0 {
		return errors.New("--timeout-seconds cannot be negative")
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	report, err := casesuite.Impact(ctx, selected.Bundle, selected.Store, caseSuiteFilter(selected.Filters), casesuite.ImpactOptions{
		Signals: impactFlags.signalValues(),
		Plan:    impactFlags.planOptions(*evidenceDir),
	})
	if err != nil {
		return err
	}
	if *impactFlags.jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteImpact(report)
	return nil
}

func printCaseSuiteImpact(report casesuite.ImpactReport) {
	fmt.Println("Case Suite Impact")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Signals: %d Nodes: %d Workflows: %d Cases: %d Selected: %d Blocked: %d\n", report.Counts.Signals, report.Counts.Nodes, report.Counts.Workflows, report.Counts.Cases, report.Counts.Selected, report.Counts.Blocked)
	for _, item := range report.Cases {
		fmt.Printf("- %s action=%s latest=%s\n", item.CaseID, item.SuggestedAction, item.LatestStatus)
		for _, reason := range item.Reasons {
			fmt.Printf("  reason: %s\n", reason)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}
