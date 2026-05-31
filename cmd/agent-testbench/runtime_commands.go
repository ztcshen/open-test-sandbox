package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/runner/executor"
	"agent-testbench/internal/server/controlplane"
)

func runConfig(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing config command")
	}
	switch args[0] {
	case "path":
		return runConfigPath(args[1:])
	case "show":
		return runConfigShow(args[1:])
	case "edit":
		return runConfigEdit(ctx, args[1:])
	case "publish", "apply":
		return runConfigPublish(ctx, args[1:])
	default:
		return fmt.Errorf("unknown config command: %s", args[0])
	}
}

func runExecutor(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing executor command")
	}
	switch args[0] {
	case "plan":
		return runExecutorPlan(ctx, args[1:])
	default:
		return fmt.Errorf("unknown executor command: %s", args[0])
	}
}

func runExecutorPlan(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("executor plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := executorPlanReport(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printExecutorPlan(report)
	return nil
}

func executorPlanReport(ctx context.Context, profileRef string, profileHomeRef string, storeRef string, legacyStoreURL string) (executor.PlanReport, error) {
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(storeRef, legacyStoreURL)
	if err != nil {
		return executor.PlanReport{}, err
	}
	if strings.TrimSpace(profileRef) != "" {
		resolvedProfilePath, err := materializeProfileReference(profileRef, profileHomeRef, false)
		if err != nil {
			return executor.PlanReport{}, err
		}
		bundle, err := profile.Load(resolvedProfilePath)
		if err != nil {
			return executor.PlanReport{}, err
		}
		return executor.Plan(ctx, bundle), nil
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return executor.PlanReport{}, err
	}
	defer closeCLIStore(runtime)
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return executor.PlanReport{}, err
	}
	return executor.PlanFromCatalog(ctx, catalog), nil
}

func printExecutorPlan(report executor.PlanReport) {
	fmt.Println("Executor Plan")
	fmt.Printf("Profile: %s\n", report.ProfileID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked)
	for _, item := range report.Items {
		state := "blocked"
		if item.Ready {
			state = "ready"
		}
		fmt.Printf("- %s [%s] %s\n", item.ID, item.Kind, state)
		if item.SourcePath != "" {
			fmt.Printf("  source: %s\n", item.SourcePath)
		}
		if item.Command != "" {
			fmt.Printf("  command: %s\n", item.Command)
		}
		if len(item.Issues) > 0 {
			fmt.Printf("  issues: %s\n", strings.Join(item.Issues, ","))
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runTrace(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing trace command")
	}
	switch args[0] {
	case "topology":
		return runTraceTopology(ctx, args[1:])
	default:
		return fmt.Errorf("unknown trace command: %s", args[0])
	}
}

func runTraceTopology(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing trace topology command")
	}
	switch args[0] {
	case "collect":
		return runTraceTopologyCollect(ctx, args[1:])
	default:
		return fmt.Errorf("unknown trace topology command: %s", args[0])
	}
}

func runTraceTopologyCollect(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("trace topology collect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	graphQLURL := flags.String("trace-graphql-url", os.Getenv("AGENT_TESTBENCH_TRACE_GRAPHQL_URL"), "Trace provider GraphQL URL")
	runID := flags.String("run", "", "Workflow run id")
	stepID := flags.String("step", "", "Workflow step id")
	caseID := flags.String("case", "", "API case id")
	requestID := flags.String("request", "", "Request id")
	endpoint := flags.String("endpoint", "", "Trace endpoint")
	traceID := flags.String("trace-id", "", "Trace id")
	startedAt := flags.String("started-at", "", "Run started timestamp")
	finishedAt := flags.String("finished-at", "", "Run finished timestamp")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runID) == "" {
		return errors.New("--run is required")
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer closeCLIStore(runtime)
	payload := map[string]any{
		"runId":      *runID,
		"stepId":     *stepID,
		"caseId":     *caseID,
		"requestId":  *requestID,
		"endpoint":   *endpoint,
		"traceId":    *traceID,
		"startedAt":  *startedAt,
		"finishedAt": *finishedAt,
	}
	response, err := controlplane.CollectTraceTopologyPayload(ctx, runtime, controlplane.TraceCollector{GraphQLURL: *graphQLURL}, payload)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(response)
	}
	printTraceTopologyCollect(response)
	return nil
}

func printTraceTopologyCollect(response map[string]any) {
	row := mapFromReportAny(response["traceTopology"])
	topology := mapFromReportAny(response["topology"])
	fmt.Println("Trace Topology Collect")
	fmt.Printf("Run: %s\n", valueString(row["workflowRunId"]))
	fmt.Printf("Trace: %s\n", valueString(row["traceId"]))
	fmt.Printf("Status: %s\n", valueString(row["status"]))
	fmt.Printf("Spans: %s\n", valueString(topology["spanCount"]))
	if edges, ok := topology["confirmedEdges"].([]any); ok {
		fmt.Printf("Confirmed Edges: %d\n", len(edges))
	}
}

func runReplay(args []string) error {
	if len(args) == 0 {
		return errors.New("missing replay command")
	}
	switch args[0] {
	case "evidence":
		return runReplayEvidence(args[1:])
	default:
		return fmt.Errorf("unknown replay command: %s", args[0])
	}
}

func runReplayEvidence(args []string) error {
	flags := flag.NewFlagSet("replay evidence", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	traceID := flags.String("trace-id", "", "Trace id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	payload, err := controlplane.ReplayEvidencePayload(*traceID)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	run := mapFromReportAny(payload["run"])
	evidence := mapFromReportAny(payload["evidence"])
	fmt.Println("Replay Evidence")
	fmt.Printf("Trace: %s\n", valueString(run["traceId"]))
	if systems, ok := evidence["systems"].([]map[string]any); ok {
		fmt.Printf("Systems: %d\n", len(systems))
		return nil
	}
	if systems, ok := evidence["systems"].([]any); ok {
		fmt.Printf("Systems: %d\n", len(systems))
		return nil
	}
	fmt.Println("Systems: 0")
	return nil
}
