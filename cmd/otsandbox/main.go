package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"open-test-sandbox/internal/apicase"
	"open-test-sandbox/internal/controlplane"
	"open-test-sandbox/internal/evidence"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/requesttemplate"
	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Printf("Open Test Sandbox %s\n", version)
	case "help", "--help", "-h":
		printHelp()
	case "store":
		if err := runStore(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "profile":
		if err := runProfile(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "evidence":
		if err := runEvidence(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "workflow":
		if err := runWorkflow(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "baseline":
		if err := runBaseline(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "template":
		if err := runTemplate(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "case":
		if err := runCase(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printHelp()
		os.Exit(2)
	}
}

func printHelp() {
	fmt.Println(`Open Test Sandbox

Usage:
  otsandbox version
  otsandbox store status [--store-url PATH]
  otsandbox store migrate [--store-url PATH]
  otsandbox profile inspect --profile PATH
  otsandbox profile import --from PATH [--store-url PATH]
  otsandbox evidence import --from PATH --profile ID [--store-url PATH]
  otsandbox evidence list [--store-url PATH] [--run ID] [--json]
  otsandbox workflow plan --profile PATH --workflow ID
  otsandbox baseline get --profile ID --subject ID [--store-url PATH]
  otsandbox baseline set --profile ID --subject ID --status STATUS [--required] [--store-url PATH]
  otsandbox template render --profile PATH --template ID [--fixture ID]
  otsandbox case run --case PATH [--base-url URL] [--dry-run] [--evidence-dir PATH]
  otsandbox serve [--profile PATH] [--host HOST] [--port PORT] [--store-url PATH]
  otsandbox help`)
}

func runStore(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing store command")
	}

	flags := flag.NewFlagSet("store "+args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := sqlite.ParseConfigFromURL(*storeURL)
	if err != nil {
		return err
	}

	switch args[0] {
	case "status":
		status, err := sqlite.MigrationStatus(ctx, cfg)
		if err != nil {
			return err
		}
		printStoreStatus(status)
	case "migrate":
		status, err := sqlite.Migrate(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Migrated store to version %d\n", status.CurrentVersion)
		fmt.Printf("Applied: %d\n", status.AppliedCount)
		fmt.Printf("Path: %s\n", status.Path)
	default:
		return fmt.Errorf("unknown store command: %s", args[0])
	}
	return nil
}

func printStoreStatus(status sqlite.MigrationStatusResult) {
	pending := status.TargetVersion - status.CurrentVersion
	if pending < 0 {
		pending = 0
	}
	fmt.Println("Store: sqlite")
	fmt.Printf("Path: %s\n", status.Path)
	fmt.Printf("Version: %d\n", status.CurrentVersion)
	fmt.Printf("Target: %d\n", status.TargetVersion)
	fmt.Printf("Pending: %d\n", pending)
}

func runProfile(args []string) error {
	if len(args) == 0 {
		return errors.New("missing profile command")
	}

	switch args[0] {
	case "inspect":
		return runProfileInspect(args[1:])
	case "import":
		return runProfileImport(context.Background(), args[1:])
	default:
		return fmt.Errorf("unknown profile command: %s", args[0])
	}
}

func runProfileInspect(args []string) error {
	flags := flag.NewFlagSet("profile inspect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}
	printProfile(bundle)
	return nil
}

func runProfileImport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile import", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	from := flags.String("from", "", "Profile bundle path")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, err := profile.Load(*from)
	if err != nil {
		return err
	}
	digest, err := profile.BundleDigest(*from)
	if err != nil {
		return err
	}
	cfg, err := sqlite.ParseConfigFromURL(*storeURL)
	if err != nil {
		return err
	}
	s, err := sqlite.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	summary, err := json.Marshal(bundle.Counts())
	if err != nil {
		return err
	}
	if _, err := s.UpsertProfileIndex(ctx, store.ProfileIndex{
		ProfileID:    bundle.ID,
		BundlePath:   *from,
		BundleDigest: digest,
		SummaryJSON:  string(summary),
		ImportedAt:   time.Now().UTC(),
	}); err != nil {
		return err
	}
	fmt.Printf("Imported profile: %s\n", bundle.ID)
	fmt.Printf("Digest: %s\n", digest)
	return nil
}

func printProfile(bundle profile.Bundle) {
	counts := bundle.Counts()
	fmt.Printf("Profile: %s\n", bundle.ID)
	fmt.Printf("Display Name: %s\n", bundle.DisplayName)
	fmt.Printf("Services: %d\n", counts.Services)
	fmt.Printf("Workflows: %d\n", counts.Workflows)
	fmt.Printf("Interface Nodes: %d\n", counts.InterfaceNodes)
	fmt.Printf("API Cases: %d\n", counts.APICases)
	fmt.Printf("Request Templates: %d\n", counts.RequestTemplates)
	fmt.Printf("Case Dependencies: %d\n", counts.CaseDependencies)
	fmt.Printf("Workflow Bindings: %d\n", counts.WorkflowBindings)
	fmt.Printf("Fixtures: %d\n", counts.Fixtures)
}

func runWorkflow(args []string) error {
	if len(args) == 0 {
		return errors.New("missing workflow command")
	}
	switch args[0] {
	case "plan":
		return runWorkflowPlan(args[1:])
	default:
		return fmt.Errorf("unknown workflow command: %s", args[0])
	}
}

func runWorkflowPlan(args []string) error {
	flags := flag.NewFlagSet("workflow plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	workflowID := flags.String("workflow", "", "Workflow id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}
	if _, ok := findWorkflow(bundle, *workflowID); !ok {
		return fmt.Errorf("workflow not found: %s", *workflowID)
	}

	fmt.Printf("Workflow: %s\n", *workflowID)
	for _, binding := range bundle.WorkflowBindings {
		if binding.WorkflowID != *workflowID {
			continue
		}
		fmt.Printf("Step: %s\n", binding.StepID)
		fmt.Printf("Node: %s\n", binding.NodeID)
		if binding.CaseID != "" {
			fmt.Printf("Case: %s\n", binding.CaseID)
		}
		fmt.Printf("Required: %t\n", binding.Required)
	}
	return nil
}

func findWorkflow(bundle profile.Bundle, id string) (profile.Workflow, bool) {
	for _, workflow := range bundle.Workflows {
		if workflow.ID == id {
			return workflow, true
		}
	}
	return profile.Workflow{}, false
}

func runBaseline(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing baseline command")
	}
	switch args[0] {
	case "get":
		return runBaselineGet(ctx, args[1:])
	case "set":
		return runBaselineSet(ctx, args[1:])
	default:
		return fmt.Errorf("unknown baseline command: %s", args[0])
	}
}

func runBaselineGet(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("baseline get", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	profileID := flags.String("profile", "", "Profile id")
	subjectID := flags.String("subject", "", "Subject id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	s, err := openStore(ctx, *storeURL)
	if err != nil {
		return err
	}
	defer s.Close()

	gate, err := s.GetBaselineGate(ctx, *profileID, *subjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("baseline gate not found: %s %s", *profileID, *subjectID)
		}
		return err
	}
	printBaselineGate(gate)
	return nil
}

func runBaselineSet(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("baseline set", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	profileID := flags.String("profile", "", "Profile id")
	subjectID := flags.String("subject", "", "Subject id")
	status := flags.String("status", "", "Gate status")
	required := flags.Bool("required", false, "Mark the gate as required")
	summaryJSON := flags.String("summary-json", "{}", "Gate summary JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	s, err := openStore(ctx, *storeURL)
	if err != nil {
		return err
	}
	defer s.Close()

	now := time.Now().UTC()
	gate, err := s.UpsertBaselineGate(ctx, store.BaselineGate{
		ProfileID:   *profileID,
		SubjectID:   *subjectID,
		Status:      *status,
		Required:    *required,
		SummaryJSON: *summaryJSON,
		CheckedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return err
	}
	printBaselineGate(gate)
	return nil
}

func openStore(ctx context.Context, storeURL string) (*sqlite.Store, error) {
	cfg, err := sqlite.ParseConfigFromURL(storeURL)
	if err != nil {
		return nil, err
	}
	return sqlite.Open(ctx, cfg)
}

func printBaselineGate(gate store.BaselineGate) {
	fmt.Printf("Baseline Gate: %s %s\n", gate.ProfileID, gate.SubjectID)
	fmt.Printf("Status: %s\n", gate.Status)
	fmt.Printf("Required: %t\n", gate.Required)
}

func runTemplate(args []string) error {
	if len(args) == 0 {
		return errors.New("missing template command")
	}
	switch args[0] {
	case "render":
		return runTemplateRender(args[1:])
	default:
		return fmt.Errorf("unknown template command: %s", args[0])
	}
}

func runTemplateRender(args []string) error {
	flags := flag.NewFlagSet("template render", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	templateID := flags.String("template", "", "Request template id")
	fixtureID := flags.String("fixture", "", "Fixture id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}
	rendered, err := requesttemplate.Render(bundle, requesttemplate.Options{
		TemplateID: *templateID,
		FixtureID:  *fixtureID,
	})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(rendered)
}

func runEvidence(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing evidence command")
	}
	switch args[0] {
	case "import":
		return runEvidenceImport(ctx, args[1:])
	case "list":
		return runEvidenceList(ctx, args[1:])
	default:
		return fmt.Errorf("unknown evidence command: %s", args[0])
	}
}

func runEvidenceList(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("evidence list", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	runID := flags.String("run", "", "Run id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := sqlite.ParseConfigFromURL(*storeURL)
	if err != nil {
		return err
	}
	s, err := sqlite.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	report, err := evidenceList(ctx, s, *runID)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printEvidenceList(report)
	return nil
}

type evidenceListReport struct {
	Runs []evidenceRunReport `json:"runs"`
}

type evidenceRunReport struct {
	ID              string                 `json:"id"`
	ProfileID       string                 `json:"profileId"`
	WorkflowID      string                 `json:"workflowId"`
	Status          string                 `json:"status"`
	EvidenceRoot    string                 `json:"evidenceRoot"`
	APICaseRunCount int                    `json:"apiCaseRunCount"`
	EvidenceCount   int                    `json:"evidenceCount"`
	APICaseRuns     []store.APICaseRun     `json:"apiCaseRuns"`
	EvidenceRecords []store.EvidenceRecord `json:"evidenceRecords"`
}

func evidenceList(ctx context.Context, s store.Store, runID string) (evidenceListReport, error) {
	runs, err := evidenceListRuns(ctx, s, runID)
	if err != nil {
		return evidenceListReport{}, err
	}
	report := evidenceListReport{Runs: make([]evidenceRunReport, 0, len(runs))}
	for _, run := range runs {
		caseRuns, err := s.ListAPICaseRuns(ctx, run.ID)
		if err != nil {
			return evidenceListReport{}, err
		}
		records, err := s.ListEvidence(ctx, run.ID)
		if err != nil {
			return evidenceListReport{}, err
		}
		report.Runs = append(report.Runs, evidenceRunReport{
			ID:              run.ID,
			ProfileID:       run.ProfileID,
			WorkflowID:      run.WorkflowID,
			Status:          run.Status,
			EvidenceRoot:    run.EvidenceRoot,
			APICaseRunCount: len(caseRuns),
			EvidenceCount:   len(records),
			APICaseRuns:     caseRuns,
			EvidenceRecords: records,
		})
	}
	return report, nil
}

func evidenceListRuns(ctx context.Context, s store.Store, runID string) ([]store.Run, error) {
	if strings.TrimSpace(runID) == "" {
		return s.ListRuns(ctx)
	}
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("run not found: %s", runID)
		}
		return nil, err
	}
	return []store.Run{run}, nil
}

func printEvidenceList(report evidenceListReport) {
	for _, run := range report.Runs {
		fmt.Printf("Run: %s\n", run.ID)
		fmt.Printf("Profile: %s\n", run.ProfileID)
		fmt.Printf("Status: %s\n", run.Status)
		for _, caseRun := range run.APICaseRuns {
			fmt.Printf("Case Run: %s\n", caseRun.ID)
			fmt.Printf("Case: %s\n", caseRun.CaseID)
			fmt.Printf("Case Status: %s\n", caseRun.Status)
		}
		for _, record := range run.EvidenceRecords {
			fmt.Printf("Evidence: %s %s\n", record.Kind, record.URI)
		}
	}
}

func runEvidenceImport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("evidence import", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	from := flags.String("from", "", "Source runtime SQLite path")
	profileID := flags.String("profile", "", "Profile id")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := sqlite.ParseConfigFromURL(*storeURL)
	if err != nil {
		return err
	}
	result, err := evidence.ImportLegacyRuntimeSQLite(ctx, evidence.SQLiteImportOptions{
		SourcePath: *from,
		ProfileID:  *profileID,
		TargetPath: cfg.Path,
	})
	if err != nil {
		return err
	}
	report := evidenceImportReport{
		SourcePath:      *from,
		StorePath:       cfg.Path,
		ProfileID:       *profileID,
		RunCount:        result.RunCount,
		APICaseRunCount: result.APICaseRunCount,
		EvidenceCount:   result.EvidenceCount,
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Println("Imported evidence index")
	fmt.Printf("Runs: %d\n", result.RunCount)
	fmt.Printf("API Case Runs: %d\n", result.APICaseRunCount)
	fmt.Printf("Evidence Records: %d\n", result.EvidenceCount)
	return nil
}

type evidenceImportReport struct {
	SourcePath      string `json:"sourcePath"`
	StorePath       string `json:"storePath"`
	ProfileID       string `json:"profileId"`
	RunCount        int    `json:"runCount"`
	APICaseRunCount int    `json:"apiCaseRunCount"`
	EvidenceCount   int    `json:"evidenceCount"`
}

func runCase(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing case command")
	}
	switch args[0] {
	case "run":
		return runCaseRun(ctx, args[1:])
	default:
		return fmt.Errorf("unknown case command: %s", args[0])
	}
}

func runCaseRun(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	casePath := flags.String("case", "", "API case file path")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	evidenceDir := flags.String("evidence-dir", filepath.Join(".runtime", "cases"), "Evidence output directory")
	runID := flags.String("run-id", "", "Run id")
	dryRun := flags.Bool("dry-run", false, "Render evidence without sending a request")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	profileID := flags.String("profile", "default", "Profile id for store records")
	if err := flags.Parse(args); err != nil {
		return err
	}
	result, err := apicase.Run(ctx, apicase.RunOptions{
		CasePath:    *casePath,
		BaseURL:     *baseURL,
		EvidenceDir: *evidenceDir,
		RunID:       *runID,
		DryRun:      *dryRun,
	})
	if err != nil {
		return err
	}
	if *storeURL != "" {
		if err := indexCaseRun(ctx, *storeURL, *profileID, result); err != nil {
			return err
		}
	}
	fmt.Printf("Case Run: %s\n", result.RunID)
	fmt.Printf("Case: %s\n", result.CaseID)
	fmt.Printf("Status: %s\n", result.Status)
	fmt.Printf("Evidence: %s\n", result.EvidencePath)
	return nil
}

func indexCaseRun(ctx context.Context, storeURL string, profileID string, result apicase.RunResult) error {
	cfg, err := sqlite.ParseConfigFromURL(storeURL)
	if err != nil {
		return err
	}
	s, err := sqlite.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	now := time.Now().UTC()
	requestSummary, assertionSummary, err := apiCaseRunSummaries(result.EvidencePath)
	if err != nil {
		return err
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:           result.RunID,
		ProfileID:    profileID,
		WorkflowID:   "",
		Status:       result.Status,
		EvidenceRoot: result.EvidencePath,
		SummaryJSON:  "{}",
		StartedAt:    now,
		FinishedAt:   now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		return err
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   result.RunID + ".case",
		RunID:                result.RunID,
		CaseID:               result.CaseID,
		Status:               result.Status,
		RequestSummaryJSON:   requestSummary,
		AssertionSummaryJSON: assertionSummary,
		StartedAt:            now,
		FinishedAt:           now,
		CreatedAt:            now,
	}); err != nil {
		return err
	}
	for _, name := range []string{"case.json", "request.json", "response.json", "assertions.json", "summary.json"} {
		path := filepath.Join(result.EvidencePath, name)
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		summary, err := evidenceSummary(path, strings.TrimSuffix(name, ".json"))
		if err != nil {
			return err
		}
		if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
			ID:        result.RunID + "." + name,
			RunID:     result.RunID,
			CaseRunID: result.RunID + ".case",
			Kind:      strings.TrimSuffix(name, ".json"),
			URI:       path,
			MediaType: "application/json",
			Summary:   summary,
			CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

type requestSummary struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	HeaderCount int    `json:"headerCount"`
	HasBody     bool   `json:"hasBody"`
}

type assertionSummary struct {
	Status     string `json:"status"`
	ErrorCount int    `json:"errorCount"`
}

type responseSummary struct {
	StatusCode  int `json:"statusCode"`
	HeaderCount int `json:"headerCount"`
	BodyBytes   int `json:"bodyBytes"`
}

func apiCaseRunSummaries(evidencePath string) (string, string, error) {
	request, err := requestSummaryJSON(filepath.Join(evidencePath, "request.json"))
	if err != nil {
		return "", "", err
	}
	assertions, err := assertionSummaryJSON(filepath.Join(evidencePath, "assertions.json"))
	if err != nil {
		return "", "", err
	}
	return request, assertions, nil
}

func evidenceSummary(path string, kind string) (string, error) {
	switch kind {
	case "request":
		return requestSummaryJSON(path)
	case "response":
		return responseSummaryJSON(path)
	case "assertions":
		return assertionSummaryJSON(path)
	default:
		return "", nil
	}
}

func requestSummaryJSON(path string) (string, error) {
	var request apicase.Request
	if err := readJSONFile(path, &request); err != nil {
		return "", err
	}
	return compactJSON(requestSummary{
		Method:      strings.ToUpper(request.Method),
		Path:        request.Path,
		HeaderCount: len(request.Headers),
		HasBody:     request.Body != nil,
	})
}

func responseSummaryJSON(path string) (string, error) {
	var response apicase.ResponseEvidence
	if err := readJSONFile(path, &response); err != nil {
		return "", err
	}
	return compactJSON(responseSummary{
		StatusCode:  response.StatusCode,
		HeaderCount: len(response.Headers),
		BodyBytes:   len([]byte(response.Body)),
	})
}

func assertionSummaryJSON(path string) (string, error) {
	var assertions apicase.AssertionEvidence
	if err := readJSONFile(path, &assertions); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return compactJSON(assertionSummary{Status: "not-run"})
		}
		return "", err
	}
	return compactJSON(assertionSummary{
		Status:     assertions.Status,
		ErrorCount: len(assertions.Errors),
	})
}

func readJSONFile(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func compactJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func runServe(args []string) error {
	cfg, err := serveConfigFromArgs(args)
	if err != nil {
		return err
	}
	handler, cleanup, err := serveHandler(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	addr := cfg.host + ":" + strconv.Itoa(cfg.port)
	fmt.Printf("Open Test Sandbox listening on http://%s\n", addr)
	return http.ListenAndServe(addr, handler)
}

type serveConfig struct {
	profilePath string
	host        string
	port        int
	storeURL    string
}

func serveHandlerFromArgs(args []string) (http.Handler, func() error, error) {
	cfg, err := serveConfigFromArgs(args)
	if err != nil {
		return nil, nil, err
	}
	return serveHandler(cfg)
}

func serveConfigFromArgs(args []string) (serveConfig, error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "profiles/empty", "Profile bundle path")
	host := flags.String("host", "127.0.0.1", "HTTP host")
	port := flags.Int("port", 18191, "HTTP port")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	if err := flags.Parse(args); err != nil {
		return serveConfig{}, err
	}
	return serveConfig{profilePath: *profilePath, host: *host, port: *port, storeURL: *storeURL}, nil
}

func serveHandler(cfg serveConfig) (http.Handler, func() error, error) {
	bundle, err := profile.Load(cfg.profilePath)
	if err != nil {
		return nil, nil, err
	}
	storeCfg, err := sqlite.ParseConfigFromURL(cfg.storeURL)
	if err != nil {
		return nil, nil, err
	}
	runtime, err := sqlite.Open(context.Background(), storeCfg)
	if err != nil {
		return nil, nil, err
	}
	return controlplane.NewWithStore(bundle, runtime), runtime.Close, nil
}
