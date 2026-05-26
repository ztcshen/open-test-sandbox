package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"agent-testbench/internal/store"
)

type reportHTMLPill struct {
	Label string
	Value string
}

const reportTotalPillLabel = "total"

type reportCaseExecutionCells struct {
	Status      string
	HTTPCode    int
	ElapsedMs   int64
	DetailURL   string
	CaseRunID   string
	Method      string
	FullURL     string
	BodyPreview string
	Error       string
}

func newReportCaseExecutionCells(status string, httpCode int, elapsedMs int64, detailURL string, caseRunID string, method string, fullURL string, bodyPreview string, errText string) reportCaseExecutionCells {
	return reportCaseExecutionCells{
		Status:      status,
		HTTPCode:    httpCode,
		ElapsedMs:   elapsedMs,
		DetailURL:   detailURL,
		CaseRunID:   caseRunID,
		Method:      method,
		FullURL:     fullURL,
		BodyPreview: bodyPreview,
		Error:       errText,
	}
}

func reportArtifactPaths(outputDir string) (string, string) {
	return filepath.Join(outputDir, "report.json"), filepath.Join(outputDir, "report.html")
}

func writeJSONAndHTMLReportArtifacts(jsonPath string, htmlPath string, report any, htmlReport string) error {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(htmlPath, []byte(htmlReport), 0o644)
}

func writeReportHTMLStart(b *strings.Builder, title string, maxWidth int) {
	if maxWidth <= 0 {
		maxWidth = 1280
	}
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>`)
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</title><style>`)
	b.WriteString(`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#111827;background:#f8fafc}main{max-width:`)
	b.WriteString(strconv.Itoa(maxWidth))
	b.WriteString(`px;margin:auto}h1{font-size:24px;margin:0 0 4px}.meta{color:#4b5563;margin-bottom:16px}.summary{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.pill{border:1px solid #d1d5db;background:white;border-radius:6px;padding:6px 10px;font-size:13px}.ok{color:#047857}.bad{color:#b91c1c}table{width:100%;border-collapse:collapse;background:white;border:1px solid #d1d5db}th,td{border-bottom:1px solid #e5e7eb;text-align:left;vertical-align:top;padding:7px 8px;font-size:13px}th{background:#f3f4f6;color:#374151}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.wrap{word-break:break-all}.small{font-size:12px;color:#6b7280}`)
	b.WriteString(`</style></head><body><main>`)
}

func writeReportHeading(b *strings.Builder, heading string, meta string, extraMeta ...string) {
	b.WriteString(`<h1>`)
	b.WriteString(html.EscapeString(heading))
	b.WriteString(`</h1><div class="meta">`)
	b.WriteString(html.EscapeString(meta))
	for _, extra := range extraMeta {
		if extra == "" {
			continue
		}
		b.WriteString(` · `)
		b.WriteString(html.EscapeString(extra))
	}
	b.WriteString(`</div>`)
}

func writeReportSummary(b *strings.Builder, pills ...reportHTMLPill) {
	b.WriteString(`<div class="summary">`)
	for _, pill := range pills {
		b.WriteString(reportPill(pill.Label, pill.Value))
	}
	b.WriteString(`</div>`)
}

func caseExecutionReportPills(ok bool, counts interfaceNodeCaseReportCounts, elapsedMs int64) []reportHTMLPill {
	return []reportHTMLPill{
		{"status", statusText(ok)},
		{reportTotalPillLabel, strconv.Itoa(counts.Total)},
		{"passed", strconv.Itoa(counts.Passed)},
		{"failed", strconv.Itoa(counts.Failed)},
		{"derived configs", strconv.Itoa(counts.DerivedConfigs)},
		{"elapsed", reportElapsedText(elapsedMs)},
	}
}

func appendCaseListFilterReportPills(pills []reportHTMLPill, filters caseListFilter) []reportHTMLPill {
	if len(filters.Tags) > 0 {
		pills = append(pills, reportHTMLPill{"tags", strings.Join(filters.Tags, ",")})
	}
	if filters.Owner != "" {
		pills = append(pills, reportHTMLPill{"owner", filters.Owner})
	}
	if filters.Priority != "" {
		pills = append(pills, reportHTMLPill{"priority", filters.Priority})
	}
	return pills
}

func reportElapsedText(elapsedMs int64) string {
	return fmt.Sprintf("%d ms", elapsedMs)
}

func reportStatusClass(status string) string {
	if status == store.StatusPassed {
		return "ok"
	}
	return "bad"
}

func writeReportIndexCell(b *strings.Builder, index int) {
	b.WriteString(`<tr><td class="mono">`)
	b.WriteString(strconv.Itoa(index + 1))
	b.WriteString(`</td>`)
}

func writeReportCaseTitleCell(b *strings.Builder, title string, caseID string, description string) {
	b.WriteString(`<td><div>`)
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</div><div class="mono small wrap">`)
	b.WriteString(html.EscapeString(caseID))
	b.WriteString(`</div>`)
	if description != "" {
		b.WriteString(`<div class="small">`)
		b.WriteString(html.EscapeString(description))
		b.WriteString(`</div>`)
	}
	b.WriteString(`</td>`)
}

func writeReportCaseExecutionCells(b *strings.Builder, cells reportCaseExecutionCells) {
	writeReportStatusCell(b, cells.Status)
	writeReportIntCell(b, cells.HTTPCode)
	writeReportElapsedCell(b, cells.ElapsedMs)
	writeReportEvidenceCell(b, cells.DetailURL, cells.CaseRunID)
	writeReportRequestCell(b, cells.Method, cells.FullURL)
	writeReportTextCell(b, "mono wrap", cells.BodyPreview)
	writeReportTextCell(b, "wrap", cells.Error)
}

func writeReportStatusCell(b *strings.Builder, status string) {
	b.WriteString(`<td class="`)
	b.WriteString(reportStatusClass(status))
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(status))
	b.WriteString(`</td>`)
}

func writeReportIntCell(b *strings.Builder, value int) {
	b.WriteString(`<td class="mono">`)
	b.WriteString(strconv.Itoa(value))
	b.WriteString(`</td>`)
}

func writeReportElapsedCell(b *strings.Builder, elapsedMs int64) {
	b.WriteString(`<td class="mono">`)
	b.WriteString(reportElapsedText(elapsedMs))
	b.WriteString(`</td>`)
}

func writeReportEvidenceCell(b *strings.Builder, detailURL string, caseRunID string) {
	b.WriteString(`<td class="mono wrap">`)
	if detailURL != "" {
		b.WriteString(`<a href="`)
		b.WriteString(html.EscapeString(detailURL))
		b.WriteString(`">caseRunId</a><br>`)
	}
	b.WriteString(html.EscapeString(caseRunID))
	b.WriteString(`</td>`)
}

func writeReportRequestCell(b *strings.Builder, method string, fullURL string) {
	b.WriteString(`<td class="mono wrap">`)
	b.WriteString(html.EscapeString(strings.TrimSpace(method + " " + fullURL)))
	b.WriteString(`</td>`)
}

func writeReportTextCell(b *strings.Builder, className string, text string) {
	b.WriteString(`<td class="`)
	b.WriteString(className)
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(text))
	b.WriteString(`</td>`)
}

func finishReportHTMLTable(b *strings.Builder) {
	b.WriteString(`</tbody></table></main></body></html>`)
}
