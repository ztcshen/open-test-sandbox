package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
)

func runWorkflow(args []string) error {
	if len(args) == 0 {
		return errors.New("missing workflow command")
	}
	switch args[0] {
	case "discover":
		return runWorkflowDiscover(context.Background(), args[1:])
	case "plan":
		return runWorkflowPlan(args[1:])
	case "audit":
		return runWorkflowAudit(context.Background(), args[1:])
	case "runs":
		return runWorkflowRuns(context.Background(), args[1:])
	case "run":
		return runWorkflowRun(context.Background(), args[1:])
	case "step":
		return runWorkflowStep(context.Background(), args[1:])
	case "latest-step":
		return runWorkflowLatestStep(context.Background(), args[1:])
	case "gate":
		return runWorkflowGate(context.Background(), args[1:])
	case "report":
		return runWorkflowReport(context.Background(), args[1:])
	case "acceptance":
		return runWorkflowAcceptance(context.Background(), args[1:])
	default:
		return fmt.Errorf("unknown workflow command: %s", args[0])
	}
}

func runWorkflowAcceptance(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing workflow acceptance command")
	}
	switch args[0] {
	case "start":
		return runWorkflowAcceptanceStart(ctx, args[1:])
	case "report":
		return runWorkflowAcceptanceReport(ctx, args[1:])
	default:
		return fmt.Errorf("unknown workflow acceptance command: %s", args[0])
	}
}

func runWorkflowAcceptanceStart(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow acceptance start", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	serverURL := flags.String("server-url", "", "Running control plane base URL")
	workflowID := flags.String("workflow", "", "Workflow id")
	requestID := flags.String("request-id", "", "Acceptance request id")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	evidenceDir := flags.String("evidence-dir", "", "Evidence output directory")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Per-step timeout in seconds")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" || strings.TrimSpace(*workflowID) == "" || strings.TrimSpace(*requestID) == "" {
		return errors.New("--server-url, --workflow, and --request-id are required")
	}
	payload := map[string]any{
		"requestId":  strings.TrimSpace(*requestID),
		"workflowId": strings.TrimSpace(*workflowID),
	}
	addWorkflowAcceptanceOptionalPayloadFields(payload, *baseURL, *evidenceDir, *timeoutSeconds)
	return postWorkflowAcceptanceRunResult(ctx, *serverURL, payload, *jsonOutput, printWorkflowAcceptanceStart)
}

func runWorkflowAcceptanceReport(ctx context.Context, args []string) error {
	return runWorkflowAcceptanceReportCommand(ctx, args, "workflow acceptance report", "Acceptance batch run id", printWorkflowAcceptanceReport)
}

func workflowAcceptanceURL(serverURL string, apiPath string) string {
	return strings.TrimRight(strings.TrimSpace(serverURL), "/") + apiPath
}

func postWorkflowAcceptanceJSON(ctx context.Context, endpoint string, payload map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return doWorkflowAcceptanceJSON(req)
}

func fetchWorkflowAcceptanceJSON(ctx context.Context, endpoint string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return doWorkflowAcceptanceJSON(req)
}

func doWorkflowAcceptanceJSON(req *http.Request) (map[string]any, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: close workflow acceptance response body: %v\n", closeErr)
		}
	}()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return payload, fmt.Errorf("%s %s failed with http status %d: %s", req.Method, req.URL.String(), resp.StatusCode, valueString(payload["error"]))
	}
	return payload, nil
}

func printWorkflowAcceptanceStart(payload map[string]any) {
	fmt.Printf("Workflow Acceptance Run: %s\n", valueString(payload["batchRunId"]))
	fmt.Printf("Workflow: %s\n", valueString(payload["workflowId"]))
	fmt.Printf("Status: %s\n", valueString(payload["status"]))
	fmt.Printf("Report: %s\n", valueString(payload["reportUrl"]))
}

func printWorkflowAcceptanceReport(payload map[string]any) {
	acceptance := mapFromReportAny(payload["acceptance"])
	fmt.Printf("Workflow Acceptance Report: %s\n", valueString(payload["batchRunId"]))
	fmt.Printf("Workflow: %s\n", firstNonEmpty(valueString(acceptance["workflowId"]), valueString(payload["workflowId"])))
	fmt.Printf("Status: %s\n", valueString(payload["status"]))
	fmt.Printf("Accepted: %t\n", boolFromReportAny(acceptance["ok"]))
	fmt.Printf("Template: %s\n", valueString(acceptance["templateId"]))
}

func runWorkflowStep(ctx context.Context, args []string) error {
	return runWorkflowStepLookup(ctx, args, workflowStepLookupOptions{
		Command:       "workflow step",
		ScopeFlag:     "run",
		ScopeHelp:     "Workflow run id",
		RequiredError: "--run and --step are required",
		Lookup:        controlplane.WorkflowStepRunPayload,
	})
}

func runWorkflowLatestStep(ctx context.Context, args []string) error {
	return runWorkflowStepLookup(ctx, args, workflowStepLookupOptions{
		Command:       "workflow latest-step",
		ScopeFlag:     "workflow",
		ScopeHelp:     "Workflow id",
		RequiredError: "--workflow and --step are required",
		Lookup:        controlplane.LatestWorkflowStepRunPayload,
	})
}

type workflowStepLookupOptions struct {
	Command       string
	ScopeFlag     string
	ScopeHelp     string
	RequiredError string
	Lookup        func(context.Context, store.Store, string, string) (map[string]any, bool, error)
}

func runWorkflowStepLookup(ctx context.Context, args []string, options workflowStepLookupOptions) error {
	flags := flag.NewFlagSet(options.Command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	scopeID := flags.String(options.ScopeFlag, "", options.ScopeHelp)
	stepID := flags.String("step", "", "Workflow step id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*scopeID) == "" || strings.TrimSpace(*stepID) == "" {
		return errors.New(options.RequiredError)
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	payload, ok, err := options.Lookup(ctx, runtime, *scopeID, *stepID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("workflow run step not found: %s %s", strings.TrimSpace(*scopeID), strings.TrimSpace(*stepID))
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printWorkflowStep(payload)
	return nil
}

func printWorkflowStep(payload map[string]any) {
	run := mapFromReportAny(payload["run"])
	summary := mapFromReportAny(payload["summary"])
	fmt.Println("Workflow Step")
	fmt.Printf("Run: %s\n", valueString(run["id"]))
	fmt.Printf("Workflow: %s\n", valueString(run["workflowId"]))
	steps := listFromReportAny(summary["steps"])
	if len(steps) > 0 {
		step := mapFromReportAny(steps[0])
		fmt.Printf("Step: %s\n", valueString(step["stepId"]))
		fmt.Printf("Case: %s\n", valueString(step["caseId"]))
		fmt.Printf("Status: %s\n", valueString(step["status"]))
	}
}

func runWorkflowRuns(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow runs", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	payload, err := controlplane.WorkflowRunsPayload(ctx, runtime)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printWorkflowRuns(payload)
	return nil
}

func runWorkflowRun(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runID := flags.String("run", "", "Workflow run id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runID) == "" {
		return errors.New("--run is required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	payload, ok, err := controlplane.WorkflowRunPayload(ctx, runtime, *runID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("workflow run not found: %s", strings.TrimSpace(*runID))
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printWorkflowRun(payload)
	return nil
}

func printWorkflowRuns(payload map[string]any) {
	rawItems := listFromReportAny(payload["workflowRuns"])
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if item := mapFromReportAny(raw); len(item) > 0 {
			items = append(items, item)
		}
	}
	fmt.Println("Workflow Runs")
	fmt.Printf("Total: %d\n", len(items))
	for _, item := range items {
		fmt.Printf("- %s [%s] %s steps=%s\n", valueString(item["id"]), valueString(item["status"]), valueString(item["workflowId"]), valueString(item["stepCount"]))
	}
}

func printWorkflowRun(payload map[string]any) {
	run := mapFromReportAny(payload["run"])
	summary := mapFromReportAny(payload["summary"])
	fmt.Println("Workflow Run")
	fmt.Printf("Run: %s\n", valueString(run["id"]))
	fmt.Printf("Workflow: %s\n", valueString(run["workflowId"]))
	fmt.Printf("Status: %s\n", valueString(run["status"]))
	if count := valueString(run["stepCount"]); count != "" {
		fmt.Printf("Steps: %s\n", count)
	} else if steps := listFromReportAny(summary["steps"]); len(steps) > 0 {
		fmt.Printf("Steps: %d\n", len(steps))
	}
}

type workflowGateReport struct {
	OK              bool               `json:"ok"`
	RunID           string             `json:"runId"`
	WorkflowID      string             `json:"workflowId,omitempty"`
	Status          string             `json:"status"`
	Counts          workflowGateCounts `json:"counts"`
	Gates           workflowGateGates  `json:"gates"`
	FailedSteps     []workflowGateStep `json:"failedSteps"`
	MissingEvidence []workflowGateStep `json:"missingEvidence"`
	NextActions     []string           `json:"nextActions"`
	Warnings        []string           `json:"warnings"`
}

type workflowGateCounts struct {
	Steps            int `json:"steps"`
	PassedSteps      int `json:"passedSteps"`
	FailedSteps      int `json:"failedSteps"`
	OtherSteps       int `json:"otherSteps"`
	CaseRuns         int `json:"caseRuns"`
	EvidenceComplete int `json:"evidenceComplete"`
}

type workflowGateGates struct {
	RunPassed        bool `json:"runPassed"`
	StepsPresent     bool `json:"stepsPresent"`
	StepsPassed      bool `json:"stepsPassed"`
	EvidenceComplete bool `json:"evidenceComplete"`
}

type workflowGateStep struct {
	StepID        string `json:"stepId,omitempty"`
	CaseID        string `json:"caseId,omitempty"`
	CaseRunID     string `json:"caseRunId,omitempty"`
	Status        string `json:"status,omitempty"`
	EvidenceCount int    `json:"evidenceCount"`
}

type workflowGateOptions struct {
	RunID           string
	RequirePassed   bool
	RequireSteps    bool
	RequireEvidence bool
}

func runWorkflowGate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow gate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runID := flags.String("run", "", "Workflow run id")
	requirePassed := flags.Bool("require-passed", false, "Fail unless the workflow run status is passed")
	requireSteps := flags.Bool("require-steps", false, "Fail unless workflow steps exist and every step passed")
	requireEvidence := flags.Bool("require-evidence", false, "Fail unless every step case run has indexed Evidence")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runID) == "" {
		return errors.New("--run is required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report, err := buildWorkflowGateReport(ctx, runtime, workflowGateOptions{
		RunID:           *runID,
		RequirePassed:   *requirePassed,
		RequireSteps:    *requireSteps,
		RequireEvidence: *requireEvidence,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
	} else {
		printWorkflowGate(report)
	}
	if !report.OK {
		return errors.New("workflow gate failed")
	}
	return nil
}

func workflowGateSteps(summaryJSON string) []map[string]any {
	summary := rawJSONObject(summaryJSON)
	steps := listFromReportAny(summary["steps"])
	out := make([]map[string]any, 0, len(steps))
	for _, raw := range steps {
		step := mapFromReportAny(raw)
		if len(step) > 0 {
			out = append(out, step)
		}
	}
	return out
}

func workflowGateStepFrom(step map[string]any, caseRunByID map[string]store.APICaseRun, caseRunsByStep map[string][]store.APICaseRun, caseRunsByCase map[string][]store.APICaseRun, evidenceCountByCaseRun map[string]int) workflowGateStep {
	out := workflowGateStep{
		StepID:    firstNonEmpty(valueString(step["stepId"]), valueString(step["id"])),
		CaseID:    valueString(step["caseId"]),
		CaseRunID: valueString(step["caseRunId"]),
		Status:    valueString(step["status"]),
	}
	if out.CaseRunID != "" {
		if item, ok := caseRunByID[out.CaseRunID]; ok {
			out.CaseID = firstNonEmpty(out.CaseID, item.CaseID)
			out.Status = firstNonEmpty(out.Status, item.Status)
		}
	}
	if out.CaseRunID == "" && out.StepID != "" {
		if items := caseRunsByStep[out.StepID]; len(items) == 1 {
			item := items[0]
			out.CaseID = firstNonEmpty(out.CaseID, item.CaseID)
			out.CaseRunID = item.ID
			out.Status = firstNonEmpty(out.Status, item.Status)
		}
	}
	if out.CaseRunID == "" && out.CaseID != "" {
		if items := caseRunsByCase[out.CaseID]; len(items) == 1 {
			item := items[0]
			out.CaseRunID = item.ID
			out.Status = firstNonEmpty(out.Status, item.Status)
		}
	}
	if out.Status == "" {
		out.Status = "unknown"
	}
	out.EvidenceCount = evidenceCountByCaseRun[out.CaseRunID]
	return out
}

func apiCaseRunStepID(item store.APICaseRun) string {
	return strings.TrimSpace(valueString(jsonObjectString(item.RequestSummaryJSON)["stepId"]))
}

func workflowGateNextActions(report workflowGateReport, options workflowGateOptions) []string {
	actions := []string{}
	if !report.Gates.StepsPresent {
		return []string{"agent-testbench workflow run --run " + quoteCommandValue(report.RunID) + " --json"}
	}
	for index, item := range report.FailedSteps {
		if index >= 3 {
			break
		}
		if item.StepID != "" {
			actions = append(actions, "agent-testbench workflow step --run "+quoteCommandValue(report.RunID)+" --step "+quoteCommandValue(item.StepID)+" --json")
		}
		if item.CaseRunID != "" {
			actions = append(actions, "agent-testbench case diagnose --case-run "+quoteCommandValue(item.CaseRunID)+" --json")
		}
	}
	if options.RequireEvidence {
		for index, item := range report.MissingEvidence {
			if index >= 3 {
				break
			}
			if item.CaseRunID != "" {
				actions = append(actions, "agent-testbench case evidence --case-run "+quoteCommandValue(item.CaseRunID)+" --json")
			}
		}
	}
	if len(actions) == 0 {
		actions = append(actions, "Workflow gate passed; no action needed")
	}
	return actions
}

func printWorkflowGate(report workflowGateReport) {
	fmt.Println("Workflow Gate")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Run: %s\n", report.RunID)
	fmt.Printf("Workflow: %s\n", report.WorkflowID)
	fmt.Printf("Status: %s\n", report.Status)
	fmt.Printf("Steps: %d Passed: %d Failed: %d Other: %d CaseRuns: %d EvidenceComplete: %d\n", report.Counts.Steps, report.Counts.PassedSteps, report.Counts.FailedSteps, report.Counts.OtherSteps, report.Counts.CaseRuns, report.Counts.EvidenceComplete)
	fmt.Printf("Gates: runPassed=%t stepsPresent=%t stepsPassed=%t evidenceComplete=%t\n", report.Gates.RunPassed, report.Gates.StepsPresent, report.Gates.StepsPassed, report.Gates.EvidenceComplete)
	for _, item := range report.FailedSteps {
		fmt.Printf("Failed Step: %s %s %s %s\n", item.StepID, item.CaseID, item.CaseRunID, item.Status)
	}
	for _, item := range report.MissingEvidence {
		fmt.Printf("Missing Evidence: %s %s %s\n", item.StepID, item.CaseID, item.CaseRunID)
	}
	for _, action := range report.NextActions {
		fmt.Printf("Next: %s\n", action)
	}
}

type workflowListReport struct {
	OK        bool               `json:"ok"`
	ProfileID string             `json:"profileId"`
	Count     int                `json:"count"`
	Items     []workflowListItem `json:"items"`
}

type workflowListItem struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	StepCount   int    `json:"stepCount"`
}

func runWorkflowDiscover(ctx context.Context, args []string) error {
	options, err := parseProfileDiscoveryCommandOptions("workflow discover", "Filter by id, display name, or description", args)
	if err != nil {
		return err
	}
	bundle, cleanup, err := options.loadDiscoveryBundle(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	report := workflowList(bundle, options.Filter)
	if options.JSONOutput {
		return writeIndentedJSON(report)
	}
	for _, item := range report.Items {
		fmt.Printf("%s\t%s\t%d\n", item.ID, item.DisplayName, item.StepCount)
	}
	return nil
}

func workflowList(bundle profile.Bundle, filter string) workflowListReport {
	stepCounts := map[string]int{}
	for _, item := range bundle.WorkflowBindings {
		if strings.TrimSpace(item.WorkflowID) != "" {
			stepCounts[item.WorkflowID]++
		}
	}
	workflows := append([]profile.Workflow(nil), bundle.Workflows...)
	sort.SliceStable(workflows, func(i, j int) bool {
		return workflows[i].ID < workflows[j].ID
	})
	report := workflowListReport{OK: true, ProfileID: bundle.ID}
	for _, workflow := range workflows {
		if !matchesDiscoveryFilter(filter, workflow.ID, workflow.DisplayName, workflow.Description) {
			continue
		}
		report.Items = append(report.Items, workflowListItem{
			ID:          workflow.ID,
			DisplayName: workflow.DisplayName,
			Description: workflow.Description,
			StepCount:   stepCounts[workflow.ID],
		})
	}
	report.Count = len(report.Items)
	return report
}
