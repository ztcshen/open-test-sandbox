package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (r *Report) addIssue(issue Issue) {
	r.Issues = append(r.Issues, issue)
}

func finalizeReport(report *Report) {
	sort.Slice(report.Metrics.Files, func(i int, j int) bool {
		return report.Metrics.Files[i].Path < report.Metrics.Files[j].Path
	})
	sort.Slice(report.Metrics.Functions, func(i int, j int) bool {
		left := report.Metrics.Functions[i]
		right := report.Metrics.Functions[j]
		if left.File == right.File {
			return left.StartLine < right.StartLine
		}
		return left.File < right.File
	})
	sort.Slice(report.Issues, func(i int, j int) bool {
		left := report.Issues[i]
		right := report.Issues[j]
		if severityRank(left.Severity) == severityRank(right.Severity) {
			if left.File == right.File {
				return left.Line < right.Line
			}
			return left.File < right.File
		}
		return severityRank(left.Severity) > severityRank(right.Severity)
	})
	report.Summary = Summary{Passed: true}
	for _, issue := range report.Issues {
		switch issue.Severity {
		case SeverityBlock:
			report.Summary.Blocks++
			report.Summary.Passed = false
		case SeverityWarning:
			report.Summary.Warnings++
		}
	}
}

func writeReports(report Report, dir string) error {
	if dir == "" {
		dir = filepath.Join("build", "reports", "quality-gate")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "quality-gate.json"), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "quality-gate.md"), []byte(markdownReport(report)), 0o644)
}

func markdownReport(report Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Go Quality Gate Report\n\n")
	fmt.Fprintf(&b, "- Mode: `%s`\n", report.Mode)
	fmt.Fprintf(&b, "- Root: `%s`\n", report.Root)
	fmt.Fprintf(&b, "- Blocks: `%d`\n", report.Summary.Blocks)
	fmt.Fprintf(&b, "- Warnings: `%d`\n", report.Summary.Warnings)
	fmt.Fprintf(&b, "- jscpd: `%s`\n", emptyDash(report.Inputs.JSCPDReport))
	if len(report.ScopePaths) > 0 {
		fmt.Fprintf(&b, "- Scope: `%s`\n", strings.Join(report.ScopePaths, "`, `"))
	}
	fmt.Fprintf(&b, "\n## Guidance For AI Fixes\n\n")
	fmt.Fprintf(&b, "- Do not create `utils`, `common`, or `helper` packages to silence duplication.\n")
	fmt.Fprintf(&b, "- Prefer domain methods, policies, validators, calculators, repository methods, or client wrappers with real names.\n")
	fmt.Fprintf(&b, "- Keep DTO assembly, generated code, and fixture duplication when abstraction would hide intent.\n")
	fmt.Fprintf(&b, "- Do not delete business branches or tests to make the report smaller.\n\n")

	if len(report.Issues) == 0 {
		fmt.Fprintf(&b, "No issues found.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "## Issues\n\n")
	fmt.Fprintf(&b, "| Severity | Category | Location | Message | Recommendation |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | --- | --- |\n")
	for _, issue := range report.Issues {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			issue.Severity,
			issue.Category,
			markdownLocation(issue),
			escapePipes(issue.Message),
			escapePipes(issue.Recommendation),
		)
	}
	return b.String()
}

func markdownLocation(issue Issue) string {
	target := issue.File
	if target == "" {
		target = issue.Package
	}
	if issue.Line > 0 {
		target = fmt.Sprintf("%s:%d", target, issue.Line)
	}
	if issue.Function != "" {
		target += " `" + issue.Function + "`"
	}
	if issue.Value != "" {
		target += " (" + issue.Value + ")"
	}
	if issue.Historical {
		target += " historical"
	}
	if target == "" {
		return "-"
	}
	return "`" + target + "`"
}

func escapePipes(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	if value == "" {
		return "-"
	}
	return value
}

func severityRank(severity string) int {
	switch severity {
	case SeverityBlock:
		return 2
	case SeverityWarning:
		return 1
	default:
		return 0
	}
}

func nowUTC() time.Time {
	return time.Now().UTC().Round(0)
}

func modeName(strict bool) string {
	if strict {
		return "strict"
	}
	return "report-only"
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func getenv(key string) string {
	return os.Getenv(key)
}

func intString(value int) string {
	return strconv.Itoa(value)
}
