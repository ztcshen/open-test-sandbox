package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var publicAPIPattern = regexp.MustCompile(`^\s*(func|type)\s+[A-Z][A-Za-z0-9_]*`)

func gitDiff(root string) (string, bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", false
	}
	parts := []string{}
	for _, args := range [][]string{
		{"diff", "--cached", "--no-ext-diff"},
		{"diff", "--no-ext-diff"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		raw, err := cmd.Output()
		if err == nil && len(bytes.TrimSpace(raw)) > 0 {
			parts = append(parts, string(raw))
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

func addGitSafetyIssues(diff string, report *Report, _ Config) {
	metric := collectGitSafety(diff)
	report.Metrics.GitSafety = metric
	addGitDeletionIssues(metric, report)
	addGitTouchpointIssues(metric, report)
}

func collectGitSafety(diff string) GitSafetyMetric {
	currentFile := ""
	changed := map[string]bool{}
	deletedTests := map[string]bool{}
	publicTouches := map[string]bool{}
	sensitive := map[string]bool{}
	errorAdds := map[string]int{}
	errorDeletes := map[string]int{}
	added := 0
	deleted := 0

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			currentFile = parseDiffFile(line)
			if currentFile != "" {
				changed[currentFile] = true
				if isSensitiveGeneratedContract(currentFile) {
					sensitive[currentFile] = true
				}
			}
		case strings.HasPrefix(line, "deleted file mode"):
			if strings.HasSuffix(currentFile, "_test.go") {
				deletedTests[currentFile] = true
			}
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added++
			content := strings.TrimPrefix(line, "+")
			if currentFile != "" && !strings.HasSuffix(currentFile, "_test.go") && publicAPIPattern.MatchString(content) {
				publicTouches[currentFile+":"+strings.TrimSpace(content)] = true
			}
			if currentFile != "" && deletesErrorHandling(content) {
				errorAdds[currentFile+":"+strings.TrimSpace(content)]++
			}
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			deleted++
			content := strings.TrimPrefix(line, "-")
			if currentFile != "" && !strings.HasSuffix(currentFile, "_test.go") && publicAPIPattern.MatchString(content) {
				publicTouches[currentFile+":"+strings.TrimSpace(content)] = true
			}
			if currentFile != "" && deletesErrorHandling(content) {
				errorDeletes[currentFile+":"+strings.TrimSpace(content)]++
			}
		}
	}
	for key, addedCount := range errorAdds {
		if deletedCount := errorDeletes[key]; deletedCount > 0 {
			remaining := deletedCount - addedCount
			if remaining > 0 {
				errorDeletes[key] = remaining
			} else {
				delete(errorDeletes, key)
			}
		}
	}

	return GitSafetyMetric{
		AddedLines:           added,
		DeletedLines:         deleted,
		ChangedFiles:         len(changed),
		DeletedTests:         sortedKeys(deletedTests),
		PublicAPITouchpoints: sortedKeys(publicTouches),
		SensitiveFiles:       sortedKeys(sensitive),
		ErrorHandlingDeletes: sortedCountKeys(errorDeletes),
	}
}

func addGitDeletionIssues(metric GitSafetyMetric, report *Report) {
	deleted := metric.DeletedLines
	added := metric.AddedLines
	if deleted > 100 {
		severity := ""
		if isDuplicateTaskKind() {
			severity = SeverityBlock
		} else if deleted > added {
			severity = SeverityWarning
		}
		if severity != "" {
			report.addIssue(Issue{
				ID:             "ai-safety-large-deletion",
				Severity:       severity,
				Category:       CategoryAISafety,
				Message:        "large deletion detected; duplicate-fix tasks must not delete broad behavior to pass the gate",
				Value:          intString(deleted),
				Threshold:      "warning when deletions > 100 and exceed additions; blocking when QUALITY_GATE_TASK_KIND contains duplicate and deletions > 100",
				Recommendation: "List the removed behavior and prove tests still cover it before proceeding.",
			})
		}
	}
	if deleted > 0 && deleted > added*2 {
		report.addIssue(Issue{
			ID:             "ai-safety-deletion-ratio",
			Severity:       SeverityWarning,
			Category:       CategoryAISafety,
			Message:        "deleted lines are more than twice added lines",
			Value:          intString(deleted) + " deleted / " + intString(added) + " added",
			Threshold:      "warning when deleted lines > added lines * 2",
			Recommendation: "Make sure the change is not removing business logic just to reduce duplication.",
		})
	}
	if metric.ChangedFiles > 8 {
		report.addIssue(Issue{
			ID:             "ai-safety-many-files",
			Severity:       SeverityWarning,
			Category:       CategoryAISafety,
			Message:        "change touches more than eight files",
			Value:          intString(metric.ChangedFiles),
			Threshold:      "warning > 8 changed files",
			Recommendation: "Write or update a refactor plan before continuing with broad changes.",
		})
	}
}

func isDuplicateTaskKind() bool {
	return strings.Contains(strings.ToLower(getenv("QUALITY_GATE_TASK_KIND")), "duplicate")
}

func addGitTouchpointIssues(metric GitSafetyMetric, report *Report) {
	if len(metric.DeletedTests) > 0 {
		report.addIssue(Issue{
			ID:             "ai-safety-deleted-tests",
			Severity:       SeverityBlock,
			Category:       CategoryAISafety,
			Message:        "test file deletion detected",
			Value:          intString(len(metric.DeletedTests)),
			Recommendation: "Do not delete tests to pass a gate; replace or move them with equivalent coverage.",
			Evidence:       metric.DeletedTests,
		})
	}
	if len(metric.PublicAPITouchpoints) > 0 {
		report.addIssue(Issue{
			ID:             "ai-safety-public-api",
			Severity:       SeverityWarning,
			Category:       CategoryAISafety,
			Message:        "public function/type/interface/struct touchpoints changed",
			Value:          intString(len(metric.PublicAPITouchpoints)),
			Recommendation: "List downstream impact in the report before merging.",
			Evidence:       metric.PublicAPITouchpoints,
		})
	}
	if len(metric.SensitiveFiles) > 0 {
		report.addIssue(Issue{
			ID:             "ai-safety-sensitive-contract",
			Severity:       SeverityWarning,
			Category:       CategoryAISafety,
			Message:        "migration, proto, OpenAPI, Swagger, or Wire contract file changed",
			Value:          intString(len(metric.SensitiveFiles)),
			Recommendation: "Call out contract compatibility and regeneration/verification separately.",
			Evidence:       metric.SensitiveFiles,
		})
	}
	if len(metric.ErrorHandlingDeletes) > 0 {
		report.addIssue(Issue{
			ID:             "ai-safety-error-handling-delete",
			Severity:       SeverityWarning,
			Category:       CategoryAISafety,
			Message:        "deleted lines appear to remove error handling",
			Value:          intString(len(metric.ErrorHandlingDeletes)),
			Recommendation: "Explain why the error branch is still covered or replace it with equivalent handling.",
			Evidence:       metric.ErrorHandlingDeletes,
		})
	}
}

func parseDiffFile(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return ""
	}
	path := strings.TrimPrefix(fields[3], "b/")
	return normalizePath(path)
}

func isSensitiveGeneratedContract(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	rel := strings.ToLower(normalizePath(path))
	return strings.HasSuffix(base, ".proto") ||
		strings.Contains(rel, "/migrations/") ||
		strings.Contains(rel, "/migration/") ||
		strings.Contains(base, "openapi") ||
		strings.Contains(base, "swagger") ||
		base == "wire.go" ||
		base == "wire_gen.go"
}

func deletesErrorHandling(line string) bool {
	line = strings.ToLower(strings.TrimSpace(line))
	return strings.Contains(line, "if err != nil") ||
		strings.Contains(line, "return err") ||
		strings.Contains(line, "errors.") ||
		strings.Contains(line, "fmt.Errorf")
}
