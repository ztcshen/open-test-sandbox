package main

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agent-testbench/internal/domain/casesuite"
)

type caseSuiteQualityReport struct {
	OK             bool                        `json:"ok"`
	ProfileID      string                      `json:"profileId"`
	Title          string                      `json:"title"`
	ReportURL      string                      `json:"reportUrl"`
	JSONReportURL  string                      `json:"jsonReportUrl"`
	ElapsedMs      int64                       `json:"elapsedMs"`
	GeneratedAt    time.Time                   `json:"generatedAt"`
	Filters        caseListFilter              `json:"filters"`
	Counts         casesuite.QualityPlanCounts `json:"counts"`
	QualityPlan    casesuite.QualityPlanReport `json:"qualityPlan"`
	Warnings       []string                    `json:"warnings,omitempty"`
	SourceStoreURL string                      `json:"sourceStoreUrl,omitempty"`
}

func runCaseSuiteQualityReport(ctx context.Context, args []string) error {
	selection := newCaseSelectionCLIFlags("case suite quality-report", "active")
	outputDir := selection.flags.String("output-dir", "", "Report output directory")
	jsonOutput := selection.flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := selection.parse(args); err != nil {
		return err
	}
	selected, err := loadSelectedCaseSuite(ctx, selection)
	if err != nil {
		return err
	}
	defer selected.Close()
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "case-suite-quality."+safeReportID(caseSuiteFilterSlug(selected.Filters))+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeCaseSuiteQualityReport(ctx, selected.Bundle, selected.Store, selected.SourceStoreURL, selected.Filters, selected.Cases, absOutputDir)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteQualityReport(report)
	return nil
}

func writeCaseSuiteQualityReportFiles(outputDir string, report *caseSuiteQualityReport) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	jsonPath, htmlPath := reportArtifactPaths(outputDir)
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	return writeJSONAndHTMLReportArtifacts(jsonPath, htmlPath, report, renderCaseSuiteQualityReportHTML(*report))
}

func renderCaseSuiteQualityReportHTML(report caseSuiteQualityReport) string {
	var b strings.Builder
	writeReportHTMLStart(&b, "Case Suite Quality Report", 1280)
	writeReportHeading(&b, "Case Suite Quality Report", report.ProfileID)
	pills := []reportHTMLPill{
		{"status", statusText(report.QualityPlan.Quality.OK)},
		{"actions", strconv.Itoa(report.Counts.Total)},
		{"draft", strconv.Itoa(report.Counts.DraftCase)},
		{"metadata", strconv.Itoa(report.Counts.CompleteMetadata)},
		{"runnable", strconv.Itoa(report.Counts.AddRunnable)},
		{"execution", strconv.Itoa(report.Counts.AddExecution)},
		{"elapsed", reportElapsedText(report.ElapsedMs)},
	}
	writeReportSummary(&b, appendCaseListFilterReportPills(pills, report.Filters)...)
	b.WriteString(`<table><thead><tr><th>#</th><th>Action</th><th>Target</th><th>Fields</th><th>Issues</th><th>Reason</th><th>Command</th></tr></thead><tbody>`)
	for index, item := range report.QualityPlan.Actions {
		target := firstNonEmpty(item.CaseID, item.SuggestedCaseID, item.NodeID)
		writeReportIndexCell(&b, index)
		b.WriteString(`<td><div>` + html.EscapeString(item.Type) + `</div></td>`)
		b.WriteString(`<td><div class="mono wrap">` + html.EscapeString(target) + `</div>`)
		if item.NodeID != "" {
			b.WriteString(`<div class="small">node: ` + html.EscapeString(item.NodeID) + `</div>`)
		}
		if item.NodeName != "" {
			b.WriteString(`<div class="small">` + html.EscapeString(item.NodeName) + `</div>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(strings.Join(item.Fields, ", ")) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(strings.Join(item.Issues, ", ")) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(item.Reason) + `</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(strings.Join(item.Command, " ")) + `</td></tr>`)
	}
	finishReportHTMLTable(&b)
	return b.String()
}

func printCaseSuiteQualityReport(report caseSuiteQualityReport) {
	fmt.Println("Case Suite Quality Report")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total Actions: %d Draft Case: %d Complete Metadata: %d Add Runnable: %d Add Execution: %d\n", report.Counts.Total, report.Counts.DraftCase, report.Counts.CompleteMetadata, report.Counts.AddRunnable, report.Counts.AddExecution)
	fmt.Printf("Elapsed: %d ms\n", report.ElapsedMs)
	fmt.Printf("Report: %s\n", report.ReportURL)
	fmt.Printf("JSON: %s\n", report.JSONReportURL)
}
