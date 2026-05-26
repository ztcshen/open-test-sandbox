package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/domain/profilecatalog"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
)

type workflowCaseReport struct {
	OK            bool                     `json:"ok"`
	ProfileID     string                   `json:"profileId"`
	WorkflowID    string                   `json:"workflowId"`
	WorkflowName  string                   `json:"workflowName"`
	RunID         string                   `json:"runId,omitempty"`
	ReportURL     string                   `json:"reportUrl"`
	JSONReportURL string                   `json:"jsonReportUrl"`
	ElapsedMs     int64                    `json:"elapsedMs"`
	GeneratedAt   time.Time                `json:"generatedAt"`
	Counts        workflowCaseReportCounts `json:"counts"`
	Steps         []workflowCaseReportStep `json:"steps"`
}

type workflowCaseReportCounts struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

type workflowCaseReportStep struct {
	StepID    string `json:"stepId"`
	Title     string `json:"title"`
	CaseID    string `json:"caseId"`
	RunID     string `json:"runId,omitempty"`
	CaseRunID string `json:"caseRunId,omitempty"`
	ViewerURL string `json:"viewerUrl,omitempty"`
	DetailURL string `json:"detailUrl,omitempty"`
	Status    string `json:"status"`
	HTTPCode  int    `json:"httpCode,omitempty"`
	ElapsedMs int64  `json:"elapsedMs"`
	Method    string `json:"method,omitempty"`
	FullURL   string `json:"fullUrl,omitempty"`
	Error     string `json:"error,omitempty"`
}

type workflowCaseReportExecution struct {
	RawSteps    []any
	StepResults []map[string]any
	StepReports []workflowCaseReportStep
}

func runWorkflowReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	workflowID := flags.String("workflow", "", "Workflow id")
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	outputDir := flags.String("output-dir", "", "Report output directory")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workflowID) == "" {
		return errors.New("--workflow is required")
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	bundle, sourceStore, cleanup, err := loadInterfaceNodeReportBundle(ctx, *profilePath, *profileHome, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer cleanup()
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "workflow."+safeReportID(*workflowID)+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeWorkflowCaseReport(ctx, bundle, sourceStore, *workflowID, absOutputDir, *baseURL)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Workflow Report: %s\n", report.WorkflowID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Passed: %d Failed: %d\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed)
	fmt.Printf("Elapsed: %d ms\n", report.ElapsedMs)
	fmt.Printf("Report: %s\n", report.ReportURL)
	return nil
}

func executeWorkflowCaseReport(ctx context.Context, bundle profile.Bundle, sourceStore store.Store, workflowID string, outputDir string, baseURL string) (workflowCaseReport, error) {
	started := time.Now()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return workflowCaseReport{}, err
	}
	runtime, err := requiredReportStore(sourceStore)
	if err != nil {
		return workflowCaseReport{}, err
	}
	if err := runtime.ReplaceProfileCatalog(ctx, profilecatalog.FromBundle(bundle, time.Now().UTC())); err != nil {
		return workflowCaseReport{}, err
	}
	handler := controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: runtime})
	server := httptest.NewServer(handler)
	defer server.Close()
	catalog, err := fetchReportMap(server.URL + "/api/catalog")
	if err != nil {
		return workflowCaseReport{}, err
	}
	workflow, err := findWorkflowByIDFromCatalog(catalog, workflowID)
	if err != nil {
		return workflowCaseReport{}, err
	}
	execution, err := runWorkflowCaseReportSteps(server.URL, bundle, workflowID, workflow, baseURL)
	if err != nil {
		return workflowCaseReport{}, err
	}
	report := workflowCaseReportFromExecution(bundle, workflowID, workflow, started, execution)
	if runID := saveWorkflowCaseReportRun(server.URL, workflowID, report, execution); runID != "" {
		report.RunID = runID
	}
	if err := writeWorkflowCaseReportFiles(outputDir, &report); err != nil {
		return workflowCaseReport{}, err
	}
	return report, nil
}

func runWorkflowCaseReportSteps(serverURL string, bundle profile.Bundle, workflowID string, workflow map[string]any, baseURL string) (workflowCaseReportExecution, error) {
	bindingCaseIDs := workflowBindingCaseIDs(bundle.WorkflowBindings, workflowID)
	contextValues := map[string]any{}
	rawSteps := listFromReportAny(workflow["steps"])
	execution := workflowCaseReportExecution{
		RawSteps:    rawSteps,
		StepResults: make([]map[string]any, 0, len(rawSteps)),
		StepReports: make([]workflowCaseReportStep, 0, len(rawSteps)),
	}
	for _, rawStep := range execution.RawSteps {
		step := mapFromReportAny(rawStep)
		caseID := runnableWorkflowCaseID(bundle.APICases, valueString(step["caseId"]), bindingCaseIDs[valueString(step["id"])])
		if caseID == "" {
			continue
		}
		result, err := postReportMap(serverURL+"/api/test-kit/run", workflowStepRunPayload(workflow, workflowID, step, caseID, contextValues, baseURL))
		if err != nil {
			return workflowCaseReportExecution{}, err
		}
		result["stepId"] = valueString(step["id"])
		result["title"] = firstNonEmpty(valueString(step["displayName"]), valueString(step["id"]))
		result["stepOk"] = boolFromReportAny(result["ok"])
		execution.StepResults = append(execution.StepResults, result)
		execution.StepReports = append(execution.StepReports, workflowReportStepItem(step, result))
		for key, value := range workflowExportedValues(step, result) {
			contextValues[key] = value
		}
		if !boolFromReportAny(result["ok"]) {
			break
		}
	}
	return execution, nil
}

func workflowStepRunPayload(workflow map[string]any, workflowID string, step map[string]any, caseID string, contextValues map[string]any, baseURL string) map[string]any {
	return map[string]any{
		"caseId":         caseID,
		"workflowId":     workflowID,
		"stepId":         valueString(step["id"]),
		"overrides":      contextValues,
		"timeoutSeconds": workflowStepTimeoutSeconds(workflow, step),
		"baseUrl":        baseURL,
	}
}

func workflowCaseReportFromExecution(bundle profile.Bundle, workflowID string, workflow map[string]any, started time.Time, execution workflowCaseReportExecution) workflowCaseReport {
	report := workflowCaseReport{
		OK:           len(execution.StepReports) == len(execution.RawSteps),
		ProfileID:    bundle.ID,
		WorkflowID:   workflowID,
		WorkflowName: firstNonEmpty(valueString(workflow["displayName"]), workflowID),
		ElapsedMs:    time.Since(started).Milliseconds(),
		GeneratedAt:  time.Now().UTC(),
		Steps:        execution.StepReports,
		Counts:       workflowCaseReportCounts{Total: len(execution.RawSteps)},
	}
	for _, item := range execution.StepReports {
		if item.Status == store.StatusPassed {
			report.Counts.Passed++
			continue
		}
		report.Counts.Failed++
		report.OK = false
	}
	if missing := len(execution.RawSteps) - len(execution.StepReports); missing > 0 {
		report.Counts.Failed += missing
		report.OK = false
	}
	return report
}

func saveWorkflowCaseReportRun(serverURL string, workflowID string, report workflowCaseReport, execution workflowCaseReportExecution) string {
	if len(execution.StepResults) == 0 {
		return ""
	}
	snapshot := map[string]any{
		"workflowId": workflowID,
		"status":     statusText(report.OK),
		"ok":         report.OK,
		"elapsedMs":  report.ElapsedMs,
		"summary": map[string]any{
			"expectedStepCount": len(execution.RawSteps),
			"stepCount":         len(execution.StepReports),
			"passed":            report.Counts.Passed,
			"elapsedMs":         report.ElapsedMs,
		},
		"steps": execution.StepResults,
	}
	if saved, err := postReportMap(serverURL+"/api/workflow-runs", snapshot); err == nil {
		return valueString(saved["workflowRunId"])
	}
	return ""
}

func workflowBindingCaseIDs(bindings []profile.WorkflowBinding, workflowID string) map[string]string {
	out := map[string]string{}
	for _, item := range bindings {
		if item.WorkflowID == workflowID && strings.TrimSpace(item.StepID) != "" && strings.TrimSpace(item.CaseID) != "" {
			out[item.StepID] = item.CaseID
		}
	}
	return out
}

func runnableWorkflowCaseID(cases []profile.APICase, candidates ...string) string {
	known := map[string]bool{}
	for _, item := range cases {
		known[item.ID] = true
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && known[candidate] {
			return candidate
		}
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}

func fetchReportMap(endpoint string) (map[string]any, error) {
	response, err := http.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: close report response body: %v\n", closeErr)
		}
	}()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s failed with http status %d", endpoint, response.StatusCode)
	}
	return payload, nil
}

func postReportMap(endpoint string, payload map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	response, err := http.Post(endpoint, "application/json", strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: close report response body: %v\n", closeErr)
		}
	}()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}
	result["httpStatus"] = response.StatusCode
	return result, nil
}

func findWorkflowByIDFromCatalog(catalog map[string]any, id string) (map[string]any, error) {
	id = strings.TrimSpace(id)
	for _, raw := range listFromReportAny(catalog["workflows"]) {
		workflow := mapFromReportAny(raw)
		if valueString(workflow["id"]) == id {
			return workflow, nil
		}
	}
	return nil, fmt.Errorf("workflow not found: %s", id)
}

func workflowStepTimeoutSeconds(workflow map[string]any, step map[string]any) int {
	timeoutMs := firstPositiveInt(intFromReportAny(step["timeoutMs"]), intFromReportAny(workflow["baseStepTimeoutMs"]), 3000)
	seconds := timeoutMs / 1000
	if timeoutMs%1000 != 0 {
		seconds++
	}
	if seconds <= 0 {
		return 3
	}
	return seconds
}

func workflowReportStepItem(step map[string]any, result map[string]any) workflowCaseReportStep {
	item := interfaceNodeCaseReportItems([]any{result})
	status := store.StatusFailed
	httpCode := 0
	elapsedMs := int64(intFromReportAny(result["elapsedMs"]))
	method := ""
	fullURL := ""
	errText := ""
	runID := valueString(result["runId"])
	caseRunID := valueString(result["caseRunId"])
	viewerURL := valueString(result["viewerUrl"])
	detailURL := valueString(result["detailUrl"])
	if len(item) > 0 {
		status = item[0].Status
		httpCode = item[0].HTTPCode
		elapsedMs = item[0].ElapsedMs
		method = item[0].Method
		fullURL = item[0].FullURL
		errText = item[0].Error
		runID = item[0].RunID
		caseRunID = item[0].CaseRunID
		viewerURL = item[0].ViewerURL
		detailURL = item[0].DetailURL
	}
	return workflowCaseReportStep{
		StepID:    valueString(step["id"]),
		Title:     firstNonEmpty(valueString(step["displayName"]), valueString(step["id"])),
		CaseID:    valueString(result["caseId"]),
		RunID:     runID,
		CaseRunID: caseRunID,
		ViewerURL: viewerURL,
		DetailURL: detailURL,
		Status:    status,
		HTTPCode:  httpCode,
		ElapsedMs: elapsedMs,
		Method:    method,
		FullURL:   fullURL,
		Error:     errText,
	}
}

func workflowExportedValues(step map[string]any, result map[string]any) map[string]any {
	out := map[string]any{}
	for _, rawExport := range listFromReportAny(step["exports"]) {
		item := mapFromReportAny(rawExport)
		name := valueString(item["name"])
		if name == "" {
			continue
		}
		value := workflowValueAtPath(workflowExportRoot(result, valueString(item["from"])), valueString(item["path"]))
		if value != nil && valueString(value) != "" {
			out[name] = value
		}
	}
	return out
}

func workflowExportRoot(result map[string]any, source string) any {
	resultBlock := mapFromReportAny(result["result"])
	request := mapFromReportAny(resultBlock["request"])
	response := mapFromReportAny(resultBlock["response"])
	responseBody := rawJSONObject(valueString(response["body"]))
	switch source {
	case "request", "requestBody":
		return firstReportValue(request, "body")
	case "requestQuery":
		return firstReportValue(request, "query")
	case "responseHeaders":
		return firstReportValue(response, "headers")
	case "response", "responseBody", "":
		return responseBody
	default:
		return responseBody
	}
}

func workflowValueAtPath(root any, path string) any {
	if strings.TrimSpace(path) == "" {
		return root
	}
	current := root
	for _, part := range strings.Split(path, ".") {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
		if current == nil {
			return nil
		}
	}
	return current
}

func writeWorkflowCaseReportFiles(outputDir string, report *workflowCaseReport) error {
	jsonPath, htmlPath := reportArtifactPaths(outputDir)
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	return writeJSONAndHTMLReportArtifacts(jsonPath, htmlPath, report, renderWorkflowCaseReportHTML(*report))
}

func renderWorkflowCaseReportHTML(report workflowCaseReport) string {
	var b strings.Builder
	writeReportHTMLStart(&b, "Workflow Report", 1280)
	writeReportHeading(&b, report.WorkflowName, report.WorkflowID, report.RunID)
	writeReportSummary(&b,
		reportHTMLPill{"status", statusText(report.OK)},
		reportHTMLPill{"steps", strconv.Itoa(report.Counts.Total)},
		reportHTMLPill{"passed", strconv.Itoa(report.Counts.Passed)},
		reportHTMLPill{"failed", strconv.Itoa(report.Counts.Failed)},
		reportHTMLPill{"elapsed", reportElapsedText(report.ElapsedMs)},
	)
	b.WriteString(`<table><thead><tr><th>#</th><th>Step</th><th>Case</th><th>Status</th><th>HTTP</th><th>Elapsed</th><th>Evidence</th><th>Request</th><th>Error</th></tr></thead><tbody>`)
	for index, item := range report.Steps {
		writeReportIndexCell(&b, index)
		b.WriteString(`<td><div>` + html.EscapeString(item.Title) + `</div><div class="mono small wrap">` + html.EscapeString(item.StepID) + `</div></td>`)
		writeReportTextCell(&b, "mono wrap", item.CaseID)
		writeReportStatusCell(&b, item.Status)
		writeReportIntCell(&b, item.HTTPCode)
		writeReportElapsedCell(&b, item.ElapsedMs)
		writeReportEvidenceCell(&b, item.DetailURL, item.CaseRunID)
		writeReportRequestCell(&b, item.Method, item.FullURL)
		writeReportTextCell(&b, "wrap", item.Error)
		b.WriteString(`</tr>`)
	}
	finishReportHTMLTable(&b)
	return b.String()
}
