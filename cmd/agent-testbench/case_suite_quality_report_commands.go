package main

import (
	"context"
	"encoding/json"
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
	jsonPath := filepath.Join(outputDir, "report.json")
	htmlPath := filepath.Join(outputDir, "report.html")
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(htmlPath, []byte(renderCaseSuiteQualityReportHTML(*report)), 0o644)
}

func renderCaseSuiteQualityReportHTML(report caseSuiteQualityReport) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Case Suite Quality Report</title><style>`)
	b.WriteString(`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#111827;background:#f8fafc}main{max-width:1280px;margin:auto}h1{font-size:24px;margin:0 0 4px}.meta{color:#4b5563;margin-bottom:16px}.summary{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.pill{border:1px solid #d1d5db;background:white;border-radius:6px;padding:6px 10px;font-size:13px}table{width:100%;border-collapse:collapse;background:white;border:1px solid #d1d5db}th,td{border-bottom:1px solid #e5e7eb;text-align:left;vertical-align:top;padding:7px 8px;font-size:13px}th{background:#f3f4f6;color:#374151}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.wrap{word-break:break-all}.small{font-size:12px;color:#6b7280}.ok{color:#047857}.bad{color:#b91c1c}`)
	b.WriteString(`</style></head><body><main>`)
	b.WriteString(`<h1>Case Suite Quality Report</h1>`)
	b.WriteString(`<div class="meta">` + html.EscapeString(report.ProfileID) + `</div><div class="summary">`)
	b.WriteString(reportPill("status", statusText(report.QualityPlan.Quality.OK)))
	b.WriteString(reportPill("actions", strconv.Itoa(report.Counts.Total)))
	b.WriteString(reportPill("draft", strconv.Itoa(report.Counts.DraftCase)))
	b.WriteString(reportPill("metadata", strconv.Itoa(report.Counts.CompleteMetadata)))
	b.WriteString(reportPill("runnable", strconv.Itoa(report.Counts.AddRunnable)))
	b.WriteString(reportPill("execution", strconv.Itoa(report.Counts.AddExecution)))
	b.WriteString(reportPill("elapsed", fmt.Sprintf("%d ms", report.ElapsedMs)))
	if len(report.Filters.Tags) > 0 {
		b.WriteString(reportPill("tags", strings.Join(report.Filters.Tags, ",")))
	}
	if report.Filters.Owner != "" {
		b.WriteString(reportPill("owner", report.Filters.Owner))
	}
	if report.Filters.Priority != "" {
		b.WriteString(reportPill("priority", report.Filters.Priority))
	}
	b.WriteString(`</div><table><thead><tr><th>#</th><th>Action</th><th>Target</th><th>Fields</th><th>Issues</th><th>Reason</th><th>Command</th></tr></thead><tbody>`)
	for index, item := range report.QualityPlan.Actions {
		target := firstNonEmpty(item.CaseID, item.SuggestedCaseID, item.NodeID)
		b.WriteString(`<tr><td class="mono">` + strconv.Itoa(index+1) + `</td>`)
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
	b.WriteString(`</tbody></table></main></body></html>`)
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
