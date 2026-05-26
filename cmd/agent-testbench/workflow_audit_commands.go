package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/domain/workflowaudit"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
)

func runWorkflowAudit(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow audit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	workflowID := flags.String("workflow", "", "Workflow id")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	offlineTemplatePackage := flags.Bool("offline-template-package", false, "Read the template package directly for offline review")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workflowID) == "" {
		return errors.New("--workflow is required")
	}

	bundle, runtime, cleanup, err := loadWorkflowAuditInputs(ctx, *profilePath, *storeRef, *storeURL, *offlineTemplatePackage)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, ok := findWorkflow(bundle, *workflowID); !ok {
		return fmt.Errorf("workflow not found: %s", *workflowID)
	}

	report, err := workflowaudit.Audit(ctx, workflowaudit.Options{
		Bundle:     bundle,
		WorkflowID: *workflowID,
		Store:      runtime,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printWorkflowAudit(report)
	return nil
}

func loadWorkflowAuditInputs(ctx context.Context, profilePath string, storeRef string, storeURL string, offlineTemplatePackage bool) (profile.Bundle, store.Store, func(), error) {
	if offlineTemplatePackage {
		return loadOfflineWorkflowAuditInputs(ctx, profilePath, storeRef, storeURL)
	}
	if strings.TrimSpace(profilePath) != "" {
		return profile.Bundle{}, nil, func() {}, errors.New("--profile is for offline template package review; add --offline-template-package or use --store NAME_OR_DSN")
	}
	return loadStoreWorkflowAuditInputs(ctx, storeRef, storeURL)
}

func loadOfflineWorkflowAuditInputs(ctx context.Context, profilePath string, storeRef string, storeURL string) (profile.Bundle, store.Store, func(), error) {
	if strings.TrimSpace(profilePath) == "" {
		return profile.Bundle{}, nil, func() {}, errors.New("--offline-template-package requires --profile")
	}
	bundle, err := profile.Load(profilePath)
	if err != nil {
		return profile.Bundle{}, nil, func() {}, err
	}
	resolvedStoreURL, err := resolveStoreReference(storeRef, storeURL)
	if err != nil {
		return profile.Bundle{}, nil, func() {}, err
	}
	if strings.TrimSpace(resolvedStoreURL) == "" {
		return bundle, nil, func() {}, nil
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return profile.Bundle{}, nil, func() {}, err
	}
	return bundle, runtime, func() { closeCLIStore(runtime) }, nil
}

func loadStoreWorkflowAuditInputs(ctx context.Context, storeRef string, storeURL string) (profile.Bundle, store.Store, func(), error) {
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(storeRef, storeURL)
	if err != nil {
		return profile.Bundle{}, nil, func() {}, err
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return profile.Bundle{}, nil, func() {}, err
	}
	bundle, err := serveBundle(ctx, runtime)
	if err != nil {
		closeCLIStore(runtime)
		return profile.Bundle{}, nil, func() {}, err
	}
	return bundle, runtime, func() { closeCLIStore(runtime) }, nil
}

func printWorkflowAudit(report workflowaudit.Report) {
	fmt.Printf("Workflow Audit: %s\n", report.WorkflowID)
	fmt.Printf("Profile: %s\n", report.ProfileID)
	if report.DisplayName != "" {
		fmt.Printf("Display Name: %s\n", report.DisplayName)
	}
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Issues: %d\n", report.IssueCount)
	fmt.Printf("Bindings: %d\n", report.BindingCount)
	for _, item := range report.Bindings {
		fmt.Printf("Binding: %s Node: %s", item.StepID, item.NodeID)
		if item.CaseID != "" {
			fmt.Printf(" Case: %s", item.CaseID)
		}
		fmt.Printf(" Required: %t\n", item.Required)
	}
	for _, item := range report.Issues {
		fmt.Printf("- [%s] %s %s %s: %s\n", item.Severity, item.Code, item.SubjectType, item.SubjectID, item.Message)
	}
	if report.Store == nil {
		return
	}
	if report.Store.LatestRun == nil {
		fmt.Println("Latest Run: not-run")
	} else {
		fmt.Printf("Latest Run: %s [%s]\n", report.Store.LatestRun.ID, report.Store.LatestRun.Status)
	}
	for _, item := range report.Store.BindingCases {
		status := item.LatestStatus
		if status == "" {
			status = "not-run"
		}
		fmt.Printf("Binding Case: %s %s Status: %s Passed: %t\n", item.StepID, item.CaseID, status, item.HasPassed)
	}
}

func runWorkflowPlan(args []string) error {
	options, err := parseProfileWorkflowStoreCommandOptions("workflow plan", args, true)
	if err != nil {
		return err
	}
	bundle, runtime, _, cleanup, err := options.loadRequiredBundle(context.Background())
	if err != nil {
		return err
	}
	defer cleanup()
	var planStore store.Store
	if runtime != nil {
		planStore = runtime
	}
	payload, ok, err := controlplane.WorkflowPlanPayload(context.Background(), bundle, options.WorkflowID, planStore)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("workflow not found: %s", options.WorkflowID)
	}
	if options.JSONOutput {
		return writeIndentedJSON(payload)
	}

	fmt.Printf("Workflow: %s\n", options.WorkflowID)
	for _, raw := range listFromReportAny(payload["steps"]) {
		step := mapFromReportAny(raw)
		fmt.Printf("Step: %s\n", valueString(step["stepId"]))
		fmt.Printf("Node: %s\n", valueString(step["nodeId"]))
		if caseID := valueString(step["caseId"]); caseID != "" {
			fmt.Printf("Case: %s\n", caseID)
		}
		fmt.Printf("Required: %t\n", boolFromReportAny(step["required"]))
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
