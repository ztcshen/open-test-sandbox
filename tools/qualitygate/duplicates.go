package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type jscpdReport struct {
	Statistics struct {
		Total struct {
			Percentage      float64 `json:"percentage"`
			DuplicatedLines float64 `json:"duplicatedLines"`
			Clones          float64 `json:"clones"`
		} `json:"total"`
	} `json:"statistics"`
	Duplicates []jscpdDuplicate `json:"duplicates"`
}

type jscpdDuplicate struct {
	Lines      int       `json:"lines"`
	Fragment   string    `json:"fragment"`
	FirstFile  jscpdFile `json:"firstFile"`
	SecondFile jscpdFile `json:"secondFile"`
}

type jscpdFile struct {
	Name  string `json:"name"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

func addJSCPDIssues(path string, report *Report, cfg Config) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			report.ToolNotes = append(report.ToolNotes, "jscpd did not produce a JSON report; this usually means the selected scope had no Go files to scan")
			return nil
		}
		return err
	}
	var parsed jscpdReport
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("decode jscpd report: %w", err)
	}

	report.Metrics.Duplicate.Percentage = parsed.Statistics.Total.Percentage
	report.Metrics.Duplicate.DuplicatedLines = int(parsed.Statistics.Total.DuplicatedLines)
	report.Metrics.Duplicate.CloneCount = int(parsed.Statistics.Total.Clones)

	if parsed.Statistics.Total.Percentage > cfg.DuplicatePercentBlock {
		report.addIssue(Issue{
			ID:             "duplicate-total",
			Severity:       SeverityBlock,
			Category:       "duplicate",
			Message:        "changed/new code duplication exceeds the blocking threshold",
			Value:          fmt.Sprintf("%.2f%%", parsed.Statistics.Total.Percentage),
			Threshold:      fmt.Sprintf("warning>%.1f%% blocking>%.1f%%", cfg.DuplicatePercentWarn, cfg.DuplicatePercentBlock),
			Recommendation: "Inspect duplicate categories before extracting; keep DTO/test fixture duplication when it is clearer.",
		})
	} else if parsed.Statistics.Total.Percentage > cfg.DuplicatePercentWarn {
		report.addIssue(Issue{
			ID:             "duplicate-total",
			Severity:       SeverityWarning,
			Category:       "duplicate",
			Message:        "changed/new code duplication exceeds the warning threshold",
			Value:          fmt.Sprintf("%.2f%%", parsed.Statistics.Total.Percentage),
			Threshold:      fmt.Sprintf("warning>%.1f%% blocking>%.1f%%", cfg.DuplicatePercentWarn, cfg.DuplicatePercentBlock),
			Recommendation: "Review whether the duplicate is a real business rule or only benign assembly/test fixture shape.",
		})
	}

	for _, item := range parsed.Duplicates {
		block := DuplicateBlock{
			FirstFile:      normalizePath(item.FirstFile.Name),
			FirstStart:     item.FirstFile.Start,
			FirstEnd:       item.FirstFile.End,
			SecondFile:     normalizePath(item.SecondFile.Name),
			SecondStart:    item.SecondFile.Start,
			SecondEnd:      item.SecondFile.End,
			Lines:          item.Lines,
			Kind:           classifyDuplicate(item.Fragment),
			Recommendation: duplicateRecommendation(item.Fragment),
		}
		report.Metrics.Duplicate.Blocks = append(report.Metrics.Duplicate.Blocks, block)
		severity := SeverityWarning
		if item.Lines >= cfg.DuplicateBlockLines && (isCorePath(block.FirstFile) || isCorePath(block.SecondFile)) {
			severity = SeverityBlock
		}
		report.addIssue(Issue{
			ID:             "duplicate-block",
			Severity:       severity,
			Category:       "duplicate",
			Message:        "duplicate code block detected by jscpd",
			File:           block.FirstFile,
			Line:           block.FirstStart,
			EndLine:        block.FirstEnd,
			Value:          fmt.Sprintf("%d lines", block.Lines),
			Threshold:      fmt.Sprintf("blocking core duplicate block >= %d lines", cfg.DuplicateBlockLines),
			DuplicateKind:  block.Kind,
			Recommendation: block.Recommendation,
			Evidence: []string{
				fmt.Sprintf("%s:%d-%d", block.FirstFile, block.FirstStart, block.FirstEnd),
				fmt.Sprintf("%s:%d-%d", block.SecondFile, block.SecondStart, block.SecondEnd),
			},
		})
	}
	sort.Slice(report.Metrics.Duplicate.Blocks, func(i int, j int) bool {
		left := report.Metrics.Duplicate.Blocks[i]
		right := report.Metrics.Duplicate.Blocks[j]
		if left.FirstFile == right.FirstFile {
			return left.FirstStart < right.FirstStart
		}
		return left.FirstFile < right.FirstFile
	})
	return nil
}

func addCombinationIssues(report *Report, cfg Config, inScope func(string) bool) {
	largeFiles := map[string]bool{}
	for _, issue := range report.Issues {
		if issue.ID == "file-lines" && issue.Severity == SeverityBlock {
			largeFiles[issue.File] = true
		}
	}

	duplicatesByFile := map[string][]DuplicateBlock{}
	duplicatesByPackage := map[string][]DuplicateBlock{}
	for _, block := range report.Metrics.Duplicate.Blocks {
		duplicatesByFile[block.FirstFile] = append(duplicatesByFile[block.FirstFile], block)
		duplicatesByFile[block.SecondFile] = append(duplicatesByFile[block.SecondFile], block)
		duplicatesByPackage[packageDirForFile(block.FirstFile)] = append(duplicatesByPackage[packageDirForFile(block.FirstFile)], block)
		duplicatesByPackage[packageDirForFile(block.SecondFile)] = append(duplicatesByPackage[packageDirForFile(block.SecondFile)], block)
	}

	addLargeDuplicateFileIssues(report, largeFiles, inScope)
	addLongDuplicateFunctionIssues(report, duplicatesByFile, cfg, inScope)
	addPackageDuplicateIssues(report, duplicatesByPackage, cfg, inScope)
}

func addLargeDuplicateFileIssues(report *Report, largeFiles map[string]bool, inScope func(string) bool) {
	for _, block := range report.Metrics.Duplicate.Blocks {
		for _, file := range []string{block.FirstFile, block.SecondFile} {
			if !inScope(file) || !largeFiles[file] {
				continue
			}
			report.addIssue(Issue{
				ID:             "combined-large-duplicate-file",
				Severity:       SeverityBlock,
				Category:       CategoryCombined,
				Message:        "same Go file is both oversized and duplicated",
				File:           file,
				Package:        packageDirForFile(file),
				DuplicateKind:  block.Kind,
				Recommendation: "Fix by naming the repeated responsibility first; do not create catch-all utils/common/helper packages.",
			})
		}
	}
}

func addLongDuplicateFunctionIssues(report *Report, blocksByFile map[string][]DuplicateBlock, cfg Config, inScope func(string) bool) {
	for _, fn := range report.Metrics.Functions {
		if fn.Lines <= cfg.FunctionDuplicateLines || !inScope(fn.File) {
			continue
		}
		if block, ok := overlappingDuplicate(fn, blocksByFile[fn.File]); ok {
			report.addIssue(Issue{
				ID:             "combined-long-duplicate-function",
				Severity:       SeverityBlock,
				Category:       CategoryCombined,
				Message:        "same function is both long and part of a duplicate block",
				File:           fn.File,
				Line:           fn.StartLine,
				EndLine:        fn.EndLine,
				Package:        fn.PackageDir,
				Function:       fn.Name,
				DuplicateKind:  block.Kind,
				Recommendation: "Extract a domain/policy/validator/repository/client concept only when the duplicate meaning is real.",
			})
		}
	}
}

func overlappingDuplicate(fn FunctionMetric, blocks []DuplicateBlock) (DuplicateBlock, bool) {
	for _, block := range blocks {
		if rangesOverlap(fn.StartLine, fn.EndLine, block.FirstStart, block.FirstEnd) ||
			rangesOverlap(fn.StartLine, fn.EndLine, block.SecondStart, block.SecondEnd) {
			return block, true
		}
	}
	return DuplicateBlock{}, false
}

func addPackageDuplicateIssues(report *Report, duplicatesByPackage map[string][]DuplicateBlock, cfg Config, inScope func(string) bool) {
	for _, metric := range report.Metrics.Packages {
		if !inScope(metric.Dir) {
			continue
		}
		blocks := uniqueDuplicateBlocks(duplicatesByPackage[metric.Dir])
		if metric.EffectiveLines > cfg.PackageLinesWarn && len(blocks) >= 2 {
			report.addIssue(Issue{
				ID:             "combined-large-duplicate-package",
				Severity:       SeverityBlock,
				Category:       CategoryCombined,
				Message:        "package is large and contains multiple duplicate blocks",
				Package:        metric.Dir,
				Value:          fmt.Sprintf("%d duplicate blocks", len(blocks)),
				Recommendation: "Review whether this package owns multiple semantic areas before splitting or extracting.",
			})
		}
		highRisk := 0
		for _, block := range blocks {
			if isHighRiskDuplicateKind(block.Kind) {
				highRisk++
			}
		}
		if highRisk >= cfg.PackageDuplicateBlocks {
			report.addIssue(Issue{
				ID:             "combined-similar-workflows",
				Severity:       SeverityBlock,
				Category:       CategoryCombined,
				Message:        "same package contains three or more similar workflow-like duplicate blocks",
				Package:        metric.Dir,
				Value:          fmt.Sprintf("%d high-risk duplicates", highRisk),
				Recommendation: "Prefer usecase helpers, validators, policies, repository methods, or client wrappers with clear names.",
			})
		}
	}
}

func classifyDuplicate(fragment string) string {
	lower := strings.ToLower(fragment)
	signals := duplicateSignals{
		Validation:   containsAny(lower, "valid", "required", "missing"),
		BuildRequest: strings.Contains(lower, "build") && strings.Contains(lower, "request"),
		Call:         containsAny(lower, "client", ".do(", ".post(", ".get("),
		Error:        containsAny(lower, "if err != nil", "return err", "handle error"),
		Response:     containsAny(lower, "response", "result"),
		TestFixture:  containsAny(lower, "test", "fixture"),
	}
	if signals.WorkflowLike() || strings.Contains(lower, "convert response") {
		return "workflow skeleton / error handling / validation / remote call wrapper"
	}
	if signals.Validation {
		return "parameter validation"
	}
	if signals.Error {
		return "error handling"
	}
	if signals.BuildRequest || signals.Call {
		return "remote call wrapper"
	}
	if signals.Response {
		return "DTO / request / response assembly"
	}
	if signals.TestFixture {
		return "test fixture"
	}
	return "structural duplicate"
}

type duplicateSignals struct {
	Validation   bool
	BuildRequest bool
	Call         bool
	Error        bool
	Response     bool
	TestFixture  bool
}

func (s duplicateSignals) WorkflowLike() bool {
	return (s.Validation && s.Error) || (s.BuildRequest && s.Call && s.Error)
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func duplicateRecommendation(fragment string) string {
	kind := classifyDuplicate(fragment)
	switch kind {
	case "parameter validation":
		return "Consider a validator or domain policy; keep local checks when rules intentionally differ."
	case "error handling":
		return "Consider package-local error mapping only when behavior is identical; do not hide distinct failures."
	case "remote call wrapper":
		return "Consider a named client wrapper or repository method that owns the protocol detail."
	case "DTO / request / response assembly":
		return "Usually tolerate DTO assembly duplication unless it repeats a business rule."
	case "test fixture":
		return "Prefer test builders for noisy setup; fixture duplication is warning-only unless it hides behavior."
	default:
		if isHighRiskDuplicateKind(kind) {
			return "Consider a domain method, usecase helper, repository method, client wrapper, validator, or policy with business meaning."
		}
		return "Review intent before abstracting; never create generic utils/common/helper just to silence duplication."
	}
}

func isHighRiskDuplicateKind(kind string) bool {
	return strings.Contains(kind, "workflow") ||
		strings.Contains(kind, "validation") ||
		strings.Contains(kind, "error handling") ||
		strings.Contains(kind, "remote call")
}

func rangesOverlap(aStart int, aEnd int, bStart int, bEnd int) bool {
	return aStart <= bEnd && bStart <= aEnd
}

func uniqueDuplicateBlocks(blocks []DuplicateBlock) []DuplicateBlock {
	seen := map[string]bool{}
	out := []DuplicateBlock{}
	for _, block := range blocks {
		key := strings.Join([]string{
			block.FirstFile,
			fmt.Sprint(block.FirstStart),
			block.SecondFile,
			fmt.Sprint(block.SecondStart),
		}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, block)
	}
	return out
}
