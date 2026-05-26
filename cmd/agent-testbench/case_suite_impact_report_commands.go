package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"agent-testbench/internal/domain/casesuite"
	"agent-testbench/internal/domain/profile"
)

type caseSuiteImpactExecutionReport struct {
	OK        bool                   `json:"ok"`
	Impact    casesuite.ImpactReport `json:"impact"`
	Report    caseSuiteReport        `json:"report"`
	ElapsedMs int64                  `json:"elapsedMs"`
}

func runCaseSuiteImpactReport(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlagsWithFilterHelp("case suite impact-report", "active", "Additional case selector filter")
	impactFlags := addCaseSuiteImpactFlags(selection, "Base URL for live request execution", 3, "Timeout per API Case")
	outputDir := selection.flags.String("output-dir", "", "Report output directory")
	if err := selection.parse(args); err != nil {
		return err
	}
	if *impactFlags.timeoutSeconds <= 0 {
		return errors.New("--timeout-seconds must be greater than zero")
	}
	started := time.Now()
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	impact, err := casesuite.Impact(ctx, selected.Bundle, selected.Store, caseSuiteFilter(selected.Filters), casesuite.ImpactOptions{
		Signals: impactFlags.signalValues(),
		Plan:    impactFlags.planOptions(""),
	})
	if err != nil {
		return err
	}
	cases := apiCasesByIDs(selected.Bundle.APICases, impact.BatchRequest.CaseIDs)
	if len(cases) == 0 {
		return errors.New("no ready impacted API cases selected for execution")
	}
	derived := deriveCaseSuiteConfigs(selected.Bundle, cases)
	selected.Bundle.TemplateConfigs = mergeTemplateConfigs(selected.Bundle.TemplateConfigs, derived)
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "case-suite-impact."+safeReportID(strings.Join(impact.Signals, "-"))+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeCaseSuiteReport(ctx, selected.Bundle, cases, derived, selected.Store, selected.SourceStoreURL, selected.Filters, *impactFlags.baseURL, absOutputDir, *impactFlags.timeoutSeconds)
	if err != nil {
		return err
	}
	out := caseSuiteImpactExecutionReport{
		OK:        impact.OK && report.OK,
		Impact:    impact,
		Report:    report,
		ElapsedMs: time.Since(started).Milliseconds(),
	}
	if *impactFlags.jsonOutput {
		return writeIndentedJSON(out)
	}
	printCaseSuiteImpactExecutionReport(out)
	return nil
}

func apiCasesByIDs(cases []profile.APICase, ids []string) []profile.APICase {
	byID := map[string]profile.APICase{}
	for _, item := range cases {
		byID[item.ID] = item
	}
	out := make([]profile.APICase, 0, len(ids))
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			out = append(out, item)
		}
	}
	return out
}

func printCaseSuiteImpactExecutionReport(report caseSuiteImpactExecutionReport) {
	fmt.Println("Case Suite Impact Report")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Selected: %d Passed: %d Failed: %d\n", report.Impact.Counts.Selected, report.Report.Counts.Passed, report.Report.Counts.Failed)
	for _, item := range report.Report.Results {
		fmt.Printf("- %s [%s]", item.CaseID, item.Status)
		if item.CaseRunID != "" {
			fmt.Printf(" %s", item.CaseRunID)
		}
		fmt.Println()
	}
}
