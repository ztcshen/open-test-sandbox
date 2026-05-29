package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"agent-testbench/internal/runner/apicase"
	"agent-testbench/internal/store"
)

type caseDiagnosisReport struct {
	OK              bool                  `json:"ok"`
	CaseRunID       string                `json:"caseRunId"`
	RunID           string                `json:"runId"`
	CaseID          string                `json:"caseId"`
	Status          string                `json:"status"`
	Operation       string                `json:"operation,omitempty"`
	Category        string                `json:"category"`
	PrimaryFinding  string                `json:"primaryFinding"`
	EvidencePath    string                `json:"evidencePath,omitempty"`
	AssertionErrors []string              `json:"assertionErrors"`
	Signals         []caseDiagnosisSignal `json:"signals"`
	NextActions     []string              `json:"nextActions"`
	Warnings        []string              `json:"warnings"`
}

type caseDiagnosisSignal struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type caseDiagnosisArtifacts struct {
	AssertionErrors      []string
	HTTPStatus           int
	Warnings             []string
	MissingLocalEvidence []string
}

const caseDiagnosisEvidenceKindAssertions = "assertions"

func runCaseDiagnose(ctx context.Context, args []string) error {
	return runCaseEvidenceReport(ctx, args, "case diagnose", diagnoseCaseEvidence, printCaseDiagnosis)
}

func diagnoseCaseEvidence(ctx context.Context, runtime store.Store, caseRunID string, runID string, caseID string, stepID string) (caseDiagnosisReport, error) {
	payload, err := readCaseEvidence(ctx, runtime, caseRunID, runID, caseID, stepID)
	if err != nil {
		return caseDiagnosisReport{}, err
	}
	evidence := mapFromReportAny(payload["evidence"])
	summary := mapFromReportAny(evidence["summary"])
	assertions := mapFromReportAny(evidence["assertions"])
	response := mapFromReportAny(evidence["response"])
	request := mapFromReportAny(evidence["request"])

	report := caseDiagnosisReport{
		CaseRunID:       valueString(summary["case_run_id"]),
		RunID:           valueString(summary["run_id"]),
		CaseID:          valueString(summary["case_id"]),
		Status:          valueString(summary["status"]),
		Operation:       firstNonEmpty(valueString(summary["operation"]), caseRunOperationFromRequest(request, valueString(summary["case_id"]))),
		EvidencePath:    valueString(summary["evidence_path"]),
		AssertionErrors: []string{},
		Signals:         []caseDiagnosisSignal{},
		NextActions:     []string{},
		Warnings:        []string{},
	}
	report.OK = strings.EqualFold(report.Status, store.StatusPassed)

	artifacts, err := readCaseDiagnosisArtifacts(ctx, runtime, report.RunID, report.CaseRunID)
	if err != nil {
		return caseDiagnosisReport{}, err
	}
	report.AssertionErrors = artifacts.AssertionErrors
	report.Warnings = append(report.Warnings, artifacts.Warnings...)
	httpStatus := firstPositiveInt(artifacts.HTTPStatus, intFromReportAny(response["http_code"]), intFromReportAny(summary["actual_http_code"]))
	assertionStatus := valueString(assertions["status"])
	errorCount := firstPositiveInt(len(report.AssertionErrors), intFromReportAny(assertions["errorCount"]))

	report.Category = caseDiagnosisCategory(report.Status, assertionStatus, errorCount, httpStatus)
	report.PrimaryFinding = caseDiagnosisPrimaryFinding(report.Category, report.AssertionErrors, httpStatus, report.Status)
	report.Signals = caseDiagnosisSignals(report, assertionStatus, errorCount, httpStatus)
	report.NextActions = caseDiagnosisNextActions(report, httpStatus, errorCount, artifacts.MissingLocalEvidence)
	return report, nil
}

func readCaseDiagnosisArtifacts(ctx context.Context, runtime store.Store, runID string, caseRunID string) (caseDiagnosisArtifacts, error) {
	out := caseDiagnosisArtifacts{AssertionErrors: []string{}, Warnings: []string{}, MissingLocalEvidence: []string{}}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(caseRunID) == "" {
		out.Warnings = append(out.Warnings, "case run evidence identity is incomplete")
		return out, nil
	}
	records, err := runtime.ListEvidence(ctx, runID)
	if err != nil {
		return caseDiagnosisArtifacts{}, err
	}
	evidenceRoot := ""
	if run, err := runtime.GetRun(ctx, runID); err == nil {
		evidenceRoot = run.EvidenceRoot
	}
	for _, record := range records {
		if record.CaseRunID != caseRunID {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(record.Kind))
		readPath := record.URI
		switch kind {
		case "request", "response", caseDiagnosisEvidenceKindAssertions:
			if path, missing, unreadable := caseDiagnosisLocalEvidenceState(record.URI, evidenceRoot); missing {
				out.Warnings = append(out.Warnings, "local "+kind+" evidence file is missing: "+path)
				out.MissingLocalEvidence = append(out.MissingLocalEvidence, path)
				continue
			} else if unreadable != "" {
				out.Warnings = append(out.Warnings, "local "+kind+" evidence file is unreadable: "+unreadable)
				continue
			} else if path != "" {
				readPath = path
			}
		default:
			continue
		}
		switch kind {
		case caseDiagnosisEvidenceKindAssertions:
			var assertions apicase.AssertionEvidence
			if err := readJSONFile(readPath, &assertions); err != nil {
				out.Warnings = append(out.Warnings, "could not read assertions evidence: "+err.Error())
				continue
			}
			out.AssertionErrors = append(out.AssertionErrors, assertions.Errors...)
		case "response":
			var response apicase.ResponseEvidence
			if err := readJSONFile(readPath, &response); err != nil {
				out.Warnings = append(out.Warnings, "could not read response evidence: "+err.Error())
				continue
			}
			out.HTTPStatus = response.StatusCode
		}
	}
	return out, nil
}

func caseDiagnosisLocalEvidenceState(uri string, evidenceRoot string) (path string, missing bool, unreadable string) {
	uri = strings.TrimSpace(uri)
	if uri == "" || !caseDiagnosisURIIsLocalFile(uri) {
		return "", false, ""
	}
	path = caseDiagnosisLocalEvidencePath(uri, evidenceRoot)
	if _, err := os.Stat(path); err == nil {
		return path, false, ""
	} else if errors.Is(err, os.ErrNotExist) {
		return path, true, ""
	} else {
		return path, false, err.Error()
	}
}

func caseDiagnosisURIIsLocalFile(uri string) bool {
	return strings.HasPrefix(uri, "file://") || !strings.Contains(uri, "://")
}

func caseDiagnosisLocalEvidencePath(uri string, evidenceRoot string) string {
	path := filepath.Clean(strings.TrimPrefix(strings.TrimSpace(uri), "file://"))
	if filepath.IsAbs(path) {
		return path
	}
	root := strings.TrimSpace(evidenceRoot)
	if root == "" {
		return path
	}
	root = filepath.Clean(strings.TrimPrefix(root, "file://"))
	if root == "." || root == "" {
		return path
	}
	if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return path
	}
	return filepath.Join(root, path)
}

func caseDiagnosisCategory(status string, assertionStatus string, errorCount int, httpStatus int) string {
	if strings.EqualFold(status, store.StatusPassed) {
		return "passed"
	}
	if strings.EqualFold(assertionStatus, store.StatusFailed) || errorCount > 0 {
		return "assertion-mismatch"
	}
	if httpStatus >= 500 {
		return "server-error"
	}
	if httpStatus >= 400 {
		return "client-error"
	}
	if httpStatus == 0 {
		return "missing-response-evidence"
	}
	return "case-failure"
}

func caseDiagnosisPrimaryFinding(category string, assertionErrors []string, httpStatus int, status string) string {
	if len(assertionErrors) > 0 {
		return "Assertion mismatch: " + assertionErrors[0]
	}
	switch category {
	case "passed":
		return "Case run passed"
	case "server-error":
		return fmt.Sprintf("Target returned HTTP %d", httpStatus)
	case "client-error":
		return fmt.Sprintf("Target rejected the request with HTTP %d", httpStatus)
	case "missing-response-evidence":
		return "Response evidence is missing"
	default:
		return "Case run finished with status " + firstNonEmpty(status, "unknown")
	}
}

func caseDiagnosisSignals(report caseDiagnosisReport, assertionStatus string, errorCount int, httpStatus int) []caseDiagnosisSignal {
	signals := []caseDiagnosisSignal{
		{Name: "case.status", Value: report.Status},
	}
	if report.Operation != "" {
		signals = append(signals, caseDiagnosisSignal{Name: "operation", Value: report.Operation})
	}
	if httpStatus > 0 {
		signals = append(signals, caseDiagnosisSignal{Name: "http.status", Value: strconv.Itoa(httpStatus)})
	}
	if assertionStatus != "" {
		signals = append(signals, caseDiagnosisSignal{Name: "assertion.status", Value: assertionStatus})
	}
	if errorCount > 0 {
		signals = append(signals, caseDiagnosisSignal{Name: "assertion.error_count", Value: strconv.Itoa(errorCount)})
	}
	return signals
}

func caseDiagnosisNextActions(report caseDiagnosisReport, httpStatus int, errorCount int, missingLocalEvidence []string) []string {
	actions := []string{}
	if report.CaseRunID != "" {
		actions = append(actions, "agent-testbench case evidence --case-run "+report.CaseRunID+" --json")
	}
	if len(missingLocalEvidence) > 0 {
		actions = append(actions, "Rerun the case with --evidence-dir pointing to a durable directory, or copy/export local Evidence before temporary files are cleaned up; missing: "+strings.Join(missingLocalEvidence, ", "))
	}
	if errorCount > 0 {
		actions = append(actions, "Inspect request.json, response.json, and assertions.json under "+firstNonEmpty(report.EvidencePath, "the Evidence directory"))
	}
	if httpStatus >= 400 {
		actions = append(actions, "Compare the planned request with the target service contract and expected status codes")
	}
	if len(actions) == 0 {
		actions = append(actions, "No failure action needed")
	}
	return actions
}

func printCaseDiagnosis(report caseDiagnosisReport) {
	fmt.Println("Case Diagnosis")
	fmt.Printf("Case Run: %s\n", report.CaseRunID)
	fmt.Printf("Case: %s\n", report.CaseID)
	fmt.Printf("Status: %s\n", report.Status)
	fmt.Printf("Category: %s\n", report.Category)
	fmt.Printf("Finding: %s\n", report.PrimaryFinding)
	for _, signal := range report.Signals {
		fmt.Printf("Signal: %s=%s\n", signal.Name, signal.Value)
	}
	for _, action := range report.NextActions {
		fmt.Printf("Next: %s\n", action)
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}
