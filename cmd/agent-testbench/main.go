package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-testbench/internal/domain/casesuite"
	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/runner/apicase"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/mysql"
	"agent-testbench/internal/store/postgres"
)

const version = "0.1.0"
const interfaceNodeCommand = "interface-node"

type rootCommand func([]string) error

type unknownRootCommandError string

func (e unknownRootCommandError) Error() string {
	return "unknown command: " + string(e)
}

var rootCommands = map[string]rootCommand{
	"commands":           runCommands,
	"update":             func(args []string) error { return runUpdate(context.Background(), args) },
	"store":              func(args []string) error { return runStore(context.Background(), args) },
	"sandbox":            func(args []string) error { return runSandbox(context.Background(), args) },
	"environment":        func(args []string) error { return runEnvironment(context.Background(), args) },
	"runtime":            func(args []string) error { return runRuntime(context.Background(), args) },
	"profile":            runProfile,
	"template-package":   runTemplatePackage,
	"template-packages":  runTemplatePackage,
	"config":             func(args []string) error { return runConfig(context.Background(), args) },
	"evidence":           func(args []string) error { return runEvidence(context.Background(), args) },
	"trace":              func(args []string) error { return runTrace(context.Background(), args) },
	"replay":             runReplay,
	"executor":           func(args []string) error { return runExecutor(context.Background(), args) },
	"workflow":           runWorkflow,
	"baseline":           func(args []string) error { return runBaseline(context.Background(), args) },
	"template":           runTemplate,
	"case":               func(args []string) error { return runCase(context.Background(), args) },
	interfaceNodeCommand: runInterfaceNode,
	"serve":              runServe,
}

func main() {
	if err := runRootCommand(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var unknown unknownRootCommandError
		if errors.As(err, &unknown) {
			printHelp()
		}
		os.Exit(2)
	}
}

func runRootCommand(args []string) error {
	if len(args) < 1 {
		printHelp()
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("AgentTestBench %s\n", version)
		return nil
	case "help", "--help", "-h":
		printHelp()
		return nil
	}
	command, ok := rootCommands[args[0]]
	if !ok {
		return unknownRootCommandError(args[0])
	}
	return command(args[1:])
}

func printHelp() {
	fmt.Println(helpText())
}

func helpText() string {
	return helpTextContent
}

func applyEnvironmentServiceRepoUpdate(item map[string]any, update map[string]string) {
	keyMap := map[string]string{
		"url":      "repo",
		"branch":   "branch",
		"ref":      "ref",
		"checkout": "checkout",
	}
	for repoKey, serviceKey := range keyMap {
		value, ok := update[repoKey]
		if !ok {
			continue
		}
		if strings.TrimSpace(value) == "" {
			delete(item, serviceKey)
			continue
		}
		item[serviceKey] = value
	}
}

func printPostgresStoreStatus(status postgres.SchemaStatusResult) {
	pending := status.TargetVersion - status.CurrentVersion
	if pending < 0 {
		pending = 0
	}
	fmt.Println("Store: postgres")
	fmt.Printf("URL: %s\n", maskStoreURL(status.URL))
	fmt.Printf("Version: %d\n", status.CurrentVersion)
	fmt.Printf("Target: %d\n", status.TargetVersion)
	fmt.Printf("Pending: %d\n", pending)
}

func printMySQLStoreStatus(status mysql.SchemaStatusResult) {
	pending := status.TargetVersion - status.CurrentVersion
	if pending < 0 {
		pending = 0
	}
	fmt.Println("Store: mysql")
	fmt.Printf("URL: %s\n", maskStoreURL(status.URL))
	fmt.Printf("Version: %d\n", status.CurrentVersion)
	fmt.Printf("Target: %d\n", status.TargetVersion)
	fmt.Printf("Pending: %d\n", pending)
}

func printInterfaceNodeCoverage(payload map[string]any, gapsOnly bool) {
	if gapsOnly {
		fmt.Printf("Interface Node Coverage Gaps: %s\n", valueString(payload["workflowId"]))
		summary := mapFromReportAny(payload["summary"])
		fmt.Printf("Total Steps: %d\n", intFromReportAny(summary["totalSteps"]))
		fmt.Printf("Gaps: %d\n", intFromReportAny(summary["gapCount"]))
		for _, item := range listFromReportAny(payload["gaps"]) {
			row := mapFromReportAny(item)
			fmt.Printf("Gap: %s Node: %s Case: %s\n", valueString(row["stepId"]), valueString(row["nodeId"]), valueString(row["caseId"]))
		}
		return
	}
	fmt.Printf("Interface Node Coverage: %s\n", valueString(payload["workflowId"]))
	summary := mapFromReportAny(payload["summary"])
	fmt.Printf("Total Steps: %d\n", intFromReportAny(summary["totalSteps"]))
	fmt.Printf("Mapped Steps: %d\n", intFromReportAny(summary["mappedSteps"]))
	fmt.Printf("Unmapped Steps: %d\n", intFromReportAny(summary["unmappedSteps"]))
	for _, item := range listFromReportAny(payload["rows"]) {
		row := mapFromReportAny(item)
		fmt.Printf("Step: %s Node: %s Mapped: %t Admission: %s\n", valueString(row["stepId"]), valueString(row["nodeId"]), boolFromReportAny(row["mapped"]), valueString(row["admissionStatus"]))
	}
}

func auditInterfaceNodeCaseExecutionConfigs(bundle profile.Bundle, nodeID string) interfaceNodeCaseAuditReport {
	configs := caseExecutionConfigIDs(bundle.TemplateConfigs)
	report := interfaceNodeCaseAuditReport{ProfileID: bundle.ID, NodeID: nodeID}
	for _, item := range bundle.APICases {
		if item.NodeID != nodeID {
			continue
		}
		report.Counts.Cases++
		if configID := configs[item.ID]; configID != "" {
			report.Counts.Configured++
			report.Configured = append(report.Configured, interfaceNodeCaseConfigured{CaseID: item.ID, ConfigID: configID})
			continue
		}
		report.Counts.Missing++
		report.Missing = append(report.Missing, interfaceNodeCaseMissing{CaseID: item.ID, Title: firstNonEmpty(item.DisplayName, item.ID)})
	}
	report.OK = report.Counts.Cases > 0 && report.Counts.Missing == 0
	return report
}

func printInterfaceNodeCaseAudit(report interfaceNodeCaseAuditReport) {
	fmt.Printf("Profile: %s\n", report.ProfileID)
	fmt.Printf("Interface Node: %s\n", report.NodeID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Cases: %d\n", report.Counts.Cases)
	fmt.Printf("Configured: %d\n", report.Counts.Configured)
	fmt.Printf("Missing: %d\n", report.Counts.Missing)
	for _, item := range report.Missing {
		fmt.Printf("- missing case execution: %s\n", item.CaseID)
	}
}

func draftInterfaceNodeCase(bundle profile.Bundle, nodeID string, caseID string, title string, casePath string, method string, requestPath string, tags []string, priority string, owner string) (interfaceNodeCaseDraftReport, error) {
	node, ok := findInterfaceNode(bundle.InterfaceNodes, nodeID)
	if !ok {
		return interfaceNodeCaseDraftReport{}, fmt.Errorf("interface node %q not found", nodeID)
	}
	caseID = strings.TrimSpace(caseID)
	if caseExists(bundle.APICases, caseID) {
		return interfaceNodeCaseDraftReport{}, fmt.Errorf("api case %q already exists", caseID)
	}
	method = strings.ToUpper(strings.TrimSpace(firstNonEmpty(method, node.Method, "GET")))
	requestPath = strings.TrimSpace(firstNonEmpty(requestPath, node.Path, "/"))
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	title = strings.TrimSpace(firstNonEmpty(title, node.DisplayName, caseID))
	if strings.TrimSpace(casePath) == "" {
		casePath = filepath.ToSlash(filepath.Join("api-cases", safeCaseFileName(caseID)+".json"))
	}
	apiCase := profile.APICase{
		ID:          caseID,
		DisplayName: title,
		Description: "Generated draft for " + firstNonEmpty(node.DisplayName, node.ID) + ".",
		NodeID:      node.ID,
		Tags:        casesuite.NormalizeStringList(tags),
		Priority:    strings.TrimSpace(priority),
		Owner:       strings.TrimSpace(owner),
		Status:      "active",
		SortOrder:   nextCaseSortOrder(bundle.APICases),
		CasePath:    filepath.ToSlash(casePath),
	}
	caseFile := caseFileInput{
		Path: apiCase.CasePath,
		Case: apicase.Case{
			ID:    caseID,
			Title: title,
			Request: apicase.Request{
				Method:  method,
				Path:    requestPath,
				Headers: draftCaseHeaders(method),
				Body:    draftCaseBody(method),
			},
			Assertions: apicase.Assertions{ExpectedStatusCodes: []int{http.StatusOK}},
		},
	}
	configJSON, err := compactJSONValue(map[string]any{
		"caseId": caseID,
		"caseExecution": map[string]any{
			"method":            method,
			"nodeId":            node.ID,
			"path":              requestPath,
			"expectedHttpCodes": []int{http.StatusOK},
		},
	})
	if err != nil {
		return interfaceNodeCaseDraftReport{}, err
	}
	config := profile.TemplateConfig{
		ID:          "cfg." + caseID,
		TemplateID:  "case-execution",
		NodeID:      node.ID,
		ScopeType:   "case",
		ScopeID:     caseID,
		Title:       title + " execution",
		Description: "Generated draft execution config.",
		ConfigJSON:  configJSON,
		Status:      "active",
		SortOrder:   apiCase.SortOrder,
	}
	applyBundle := interfaceNodeCaseApplyRequest{
		APICases:        []profile.APICase{apiCase},
		TemplateConfigs: []templateConfigInput{{TemplateConfig: config}},
		CaseFiles:       []caseFileInput{caseFile},
	}
	return interfaceNodeCaseDraftReport{
		OK:             true,
		ProfileID:      bundle.ID,
		NodeID:         node.ID,
		CaseID:         caseID,
		CasePath:       apiCase.CasePath,
		APICase:        apiCase,
		TemplateConfig: config,
		CaseFile:       caseFile,
		ApplyBundle:    applyBundle,
	}, nil
}

func applyInterfaceNodeCaseConfigs(profilePath string, requestPath string) (interfaceNodeCaseApplyResult, error) {
	raw, err := os.ReadFile(requestPath)
	if err != nil {
		return interfaceNodeCaseApplyResult{}, fmt.Errorf("read case config bundle %s: %w", requestPath, err)
	}
	var request interfaceNodeCaseApplyRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return interfaceNodeCaseApplyResult{}, fmt.Errorf("decode case config bundle %s: %w", requestPath, err)
	}
	request.APICases = append(request.APICases, request.InterfaceNodeCases...)
	if len(request.TemplateConfigs) == 0 && len(request.APICases) == 0 && len(request.CaseFiles) == 0 {
		return interfaceNodeCaseApplyResult{}, errors.New("case config bundle must include apiCases, templateConfigs, or caseFiles")
	}
	configs := make([]profile.TemplateConfig, 0, len(request.TemplateConfigs))
	for _, item := range request.TemplateConfigs {
		config, err := normalizeTemplateConfigInput(item)
		if err != nil {
			return interfaceNodeCaseApplyResult{}, err
		}
		configs = append(configs, config)
	}
	apiCases := make([]profile.APICase, 0, len(request.APICases))
	for _, item := range request.APICases {
		apiCase, err := normalizeAPICaseInput(item)
		if err != nil {
			return interfaceNodeCaseApplyResult{}, err
		}
		apiCases = append(apiCases, apiCase)
	}
	if err := writeCaseFiles(profilePath, request.CaseFiles); err != nil {
		return interfaceNodeCaseApplyResult{}, err
	}
	catalogPath := filepath.Join(profilePath, "catalog.json")
	payload, existingConfigs, existingCases, err := readCatalogCaseAssets(catalogPath)
	if err != nil {
		return interfaceNodeCaseApplyResult{}, err
	}
	if len(configs) > 0 {
		merged := mergeTemplateConfigs(existingConfigs, configs)
		configRaw, err := json.Marshal(merged)
		if err != nil {
			return interfaceNodeCaseApplyResult{}, err
		}
		payload["templateConfigs"] = configRaw
	}
	if len(apiCases) > 0 {
		merged := mergeProfileAPICases(existingCases, apiCases)
		casesRaw, err := json.Marshal(merged)
		if err != nil {
			return interfaceNodeCaseApplyResult{}, err
		}
		payload["interfaceNodeCases"] = casesRaw
		delete(payload, "apiCases")
	}
	if _, ok := payload["schemaVersion"]; !ok {
		payload["schemaVersion"] = json.RawMessage(`"1"`)
	}
	next, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return interfaceNodeCaseApplyResult{}, err
	}
	next = append(next, '\n')
	if err := os.WriteFile(catalogPath, next, 0o644); err != nil {
		return interfaceNodeCaseApplyResult{}, fmt.Errorf("write profile catalog %s: %w", catalogPath, err)
	}
	if _, err := profile.Load(profilePath); err != nil {
		return interfaceNodeCaseApplyResult{}, fmt.Errorf("profile catalog is invalid after apply: %w", err)
	}
	return interfaceNodeCaseApplyResult{Applied: len(configs), Cases: len(apiCases), Files: len(request.CaseFiles)}, nil
}

func buildWorkflowGateReport(ctx context.Context, runtime store.Store, options workflowGateOptions) (workflowGateReport, error) {
	run, err := runtime.GetRun(ctx, strings.TrimSpace(options.RunID))
	if err != nil {
		return workflowGateReport{}, err
	}
	caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
	if err != nil {
		return workflowGateReport{}, err
	}
	evidence, err := runtime.ListEvidence(ctx, run.ID)
	if err != nil {
		return workflowGateReport{}, err
	}
	caseRunIndex := indexWorkflowGateCaseRuns(caseRuns)
	evidenceCountByCaseRun := indexWorkflowGateEvidence(evidence)

	report := workflowGateReport{
		RunID:           run.ID,
		WorkflowID:      run.WorkflowID,
		Status:          run.Status,
		FailedSteps:     []workflowGateStep{},
		MissingEvidence: []workflowGateStep{},
		NextActions:     []string{},
		Warnings:        []string{},
	}
	steps := workflowGateSteps(run.SummaryJSON)
	report.Counts.Steps = len(steps)
	report.Counts.CaseRuns = len(caseRuns)
	for _, rawStep := range steps {
		step := workflowGateStepFrom(rawStep, caseRunIndex.byID, caseRunIndex.byStep, caseRunIndex.byCase, evidenceCountByCaseRun)
		addWorkflowGateStep(&report, step)
	}
	report.Gates = workflowGateGates{
		RunPassed:        strings.EqualFold(run.Status, store.StatusPassed),
		StepsPresent:     report.Counts.Steps > 0,
		StepsPassed:      report.Counts.Steps > 0 && report.Counts.FailedSteps == 0 && report.Counts.OtherSteps == 0,
		EvidenceComplete: report.Counts.Steps > 0 && len(report.MissingEvidence) == 0,
	}
	report.OK = (!options.RequirePassed || report.Gates.RunPassed) &&
		(!options.RequireSteps || (report.Gates.StepsPresent && report.Gates.StepsPassed)) &&
		(!options.RequireEvidence || report.Gates.EvidenceComplete)
	report.NextActions = workflowGateNextActions(report, options)
	return report, nil
}

type workflowGateCaseRunIndex struct {
	byID   map[string]store.APICaseRun
	byCase map[string][]store.APICaseRun
	byStep map[string][]store.APICaseRun
}

func indexWorkflowGateCaseRuns(caseRuns []store.APICaseRun) workflowGateCaseRunIndex {
	index := workflowGateCaseRunIndex{
		byID:   map[string]store.APICaseRun{},
		byCase: map[string][]store.APICaseRun{},
		byStep: map[string][]store.APICaseRun{},
	}
	for _, item := range caseRuns {
		index.byID[item.ID] = item
		index.byCase[item.CaseID] = append(index.byCase[item.CaseID], item)
		if stepID := apiCaseRunStepID(item); stepID != "" {
			index.byStep[stepID] = append(index.byStep[stepID], item)
		}
	}
	return index
}

func indexWorkflowGateEvidence(evidence []store.EvidenceRecord) map[string]int {
	out := map[string]int{}
	for _, record := range evidence {
		if strings.TrimSpace(record.CaseRunID) != "" {
			out[record.CaseRunID]++
		}
	}
	return out
}

func addWorkflowGateStep(report *workflowGateReport, step workflowGateStep) {
	switch {
	case strings.EqualFold(step.Status, store.StatusPassed):
		report.Counts.PassedSteps++
	case strings.EqualFold(step.Status, store.StatusFailed):
		report.Counts.FailedSteps++
		report.FailedSteps = append(report.FailedSteps, step)
	default:
		report.Counts.OtherSteps++
		report.FailedSteps = append(report.FailedSteps, step)
	}
	if step.EvidenceCount > 0 {
		report.Counts.EvidenceComplete++
		return
	}
	report.MissingEvidence = append(report.MissingEvidence, step)
}

func postProcessTaskMatches(row store.PostProcessTask, filter evidenceTaskFilter) bool {
	if filter.StepID != "" && row.StepID != filter.StepID {
		return false
	}
	if filter.CaseID != "" && row.CaseID != filter.CaseID {
		return false
	}
	if filter.Kind != "" && row.Kind != filter.Kind {
		return false
	}
	if filter.Status != "" && row.Status != filter.Status {
		return false
	}
	return true
}

func executeCaseSuiteQualityReport(ctx context.Context, bundle profile.Bundle, sourceStore store.Store, sourceStoreURL string, filters caseListFilter, cases []profile.APICase, outputDir string) (caseSuiteQualityReport, error) {
	started := time.Now()
	plan, err := casesuite.QualityPlan(ctx, bundle, sourceStore, caseSuiteFilter(filters), cases)
	if err != nil {
		return caseSuiteQualityReport{}, err
	}
	report := caseSuiteQualityReport{
		OK:             true,
		ProfileID:      bundle.ID,
		Title:          "Case Suite Quality Report",
		ElapsedMs:      time.Since(started).Milliseconds(),
		GeneratedAt:    time.Now().UTC(),
		Filters:        normalizeCaseListFilter(filters),
		Counts:         plan.Counts,
		QualityPlan:    plan,
		Warnings:       append([]string(nil), plan.Warnings...),
		SourceStoreURL: sourceStoreURL,
	}
	if sourceStore == nil {
		report.Warnings = append(report.Warnings, "source Store was not available; report used profile bundle only")
	}
	if err := writeCaseSuiteQualityReportFiles(outputDir, &report); err != nil {
		return caseSuiteQualityReport{}, err
	}
	return report, nil
}
