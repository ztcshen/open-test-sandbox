package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"agent-testbench/internal/domain/profilecatalog"
	"agent-testbench/internal/domain/workflowaudit"
	"agent-testbench/internal/store"
)

type workflowCatalogUpsertReport struct {
	OK          bool                        `json:"ok"`
	Operation   string                      `json:"operation"`
	ProfileID   string                      `json:"profileId"`
	Created     bool                        `json:"created"`
	Updated     bool                        `json:"updated"`
	Workflow    *workflowUpsertWorkflow     `json:"workflow,omitempty"`
	Binding     *workflowUpsertBinding      `json:"binding,omitempty"`
	Counts      workflowCatalogUpsertCounts `json:"counts"`
	Audit       *workflowaudit.Report       `json:"audit,omitempty"`
	NextActions []string                    `json:"nextActions,omitempty"`
}

type workflowCatalogUpsertCounts struct {
	Before profileImportCounts `json:"before"`
	After  profileImportCounts `json:"after"`
}

type workflowUpsertWorkflow struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName,omitempty"`
	Description       string `json:"description,omitempty"`
	BaseStepTimeoutMs int    `json:"baseStepTimeoutMs,omitempty"`
	TimeoutOffsetMs   int    `json:"timeoutOffsetMs,omitempty"`
}

type workflowUpsertBinding struct {
	WorkflowID string `json:"workflowId"`
	StepID     string `json:"stepId"`
	NodeID     string `json:"nodeId"`
	CaseID     string `json:"caseId,omitempty"`
	Required   bool   `json:"required"`
	SortOrder  int    `json:"sortOrder,omitempty"`
}

type workflowRegisterOptions struct {
	ProfileID         string
	WorkflowID        string
	DisplayName       string
	Description       string
	BaseStepTimeoutMs int
	TimeoutOffsetMs   int
	Audit             bool
	PassedFlags       map[string]bool
}

type workflowBindingRegisterOptions struct {
	ProfileID   string
	WorkflowID  string
	StepID      string
	NodeID      string
	CaseID      string
	Required    bool
	SortOrder   int
	Audit       bool
	PassedFlags map[string]bool
}

func runWorkflowRegister(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow register", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	profileID := flags.String("profile", "", "Profile id to use when the Store has no profile catalog yet")
	workflowID := flags.String("id", "", "Workflow id")
	displayName := flags.String("display-name", "", "Workflow display name")
	description := flags.String("description", "", "Workflow description")
	baseStepTimeoutMs := flags.Int("base-step-timeout-ms", 0, "Base per-step timeout in milliseconds")
	timeoutOffsetMs := flags.Int("timeout-offset-ms", 0, "Additional per-step timeout offset in milliseconds")
	auditOutput := flags.Bool("audit", false, "Run workflow audit after upsert")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected workflow arguments: %s", strings.Join(flags.Args(), " "))
	}
	passedFlags := parsedFlagNames(flags)
	if strings.TrimSpace(*workflowID) == "" {
		return errors.New("--id is required")
	}
	if *baseStepTimeoutMs < 0 || *timeoutOffsetMs < 0 {
		return errors.New("--base-step-timeout-ms and --timeout-offset-ms must be non-negative")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report, err := upsertWorkflowCatalogWorkflow(ctx, runtime, workflowRegisterOptions{
		ProfileID:         *profileID,
		WorkflowID:        *workflowID,
		DisplayName:       *displayName,
		Description:       *description,
		BaseStepTimeoutMs: *baseStepTimeoutMs,
		TimeoutOffsetMs:   *timeoutOffsetMs,
		Audit:             *auditOutput,
		PassedFlags:       passedFlags,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printWorkflowCatalogUpsertReport(report)
	return nil
}

func runWorkflowBinding(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing workflow binding command")
	}
	switch args[0] {
	case "register", "upsert":
		return runWorkflowBindingRegister(ctx, args[1:])
	default:
		return fmt.Errorf("unknown workflow binding command: %s", args[0])
	}
}

func runWorkflowBindingRegister(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow binding register", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	profileID := flags.String("profile", "", "Profile id to use when the Store has no profile catalog yet")
	workflowID := flags.String("workflow", "", "Workflow id")
	stepID := flags.String("step", "", "Workflow step id")
	nodeID := flags.String("node", "", "Interface node id")
	caseID := flags.String("case", "", "API Case id")
	required := flags.Bool("required", false, "Mark this workflow step as required; use --required=false to clear")
	sortOrder := flags.Int("sort-order", 0, "Workflow binding sort order")
	auditOutput := flags.Bool("audit", false, "Run workflow audit after upsert")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected workflow binding arguments: %s", strings.Join(flags.Args(), " "))
	}
	passedFlags := parsedFlagNames(flags)
	if strings.TrimSpace(*workflowID) == "" || strings.TrimSpace(*stepID) == "" || strings.TrimSpace(*nodeID) == "" {
		return errors.New("--workflow, --step, and --node are required")
	}
	if *sortOrder < 0 {
		return errors.New("--sort-order must be non-negative")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report, err := upsertWorkflowCatalogBinding(ctx, runtime, workflowBindingRegisterOptions{
		ProfileID:   *profileID,
		WorkflowID:  *workflowID,
		StepID:      *stepID,
		NodeID:      *nodeID,
		CaseID:      *caseID,
		Required:    *required,
		SortOrder:   *sortOrder,
		Audit:       *auditOutput,
		PassedFlags: passedFlags,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printWorkflowCatalogUpsertReport(report)
	return nil
}

func upsertWorkflowCatalogWorkflow(ctx context.Context, runtime store.Store, options workflowRegisterOptions) (workflowCatalogUpsertReport, error) {
	catalog, err := loadMutableProfileCatalog(ctx, runtime, options.ProfileID)
	if err != nil {
		return workflowCatalogUpsertReport{}, err
	}
	beforeCounts := profileImportCountsFromCatalog(catalog)
	workflow, exists := findCatalogWorkflow(catalog.Workflows, options.WorkflowID)
	workflow.ID = strings.TrimSpace(options.WorkflowID)
	if options.PassedFlags["display-name"] || !exists {
		workflow.DisplayName = firstNonEmpty(strings.TrimSpace(options.DisplayName), workflow.ID)
	}
	if options.PassedFlags["description"] {
		workflow.Description = strings.TrimSpace(options.Description)
	}
	if options.PassedFlags["base-step-timeout-ms"] {
		workflow.BaseStepTimeoutMs = options.BaseStepTimeoutMs
	}
	if options.PassedFlags["timeout-offset-ms"] {
		workflow.TimeoutOffsetMs = options.TimeoutOffsetMs
	}
	catalog.Workflows = upsertCatalogWorkflow(catalog.Workflows, workflow)
	catalog.IndexedAt = time.Now().UTC()
	if err := runtime.ReplaceProfileCatalog(ctx, catalog); err != nil {
		return workflowCatalogUpsertReport{}, err
	}
	report := workflowCatalogUpsertReport{
		OK:        true,
		Operation: "workflow upsert",
		ProfileID: catalog.ProfileID,
		Created:   !exists,
		Updated:   exists,
		Workflow:  workflowUpsertWorkflowFromCatalog(workflow),
		Counts: workflowCatalogUpsertCounts{
			Before: beforeCounts,
			After:  profileImportCountsFromCatalog(catalog),
		},
		NextActions: []string{
			"agent-testbench workflow binding register --workflow " + quoteCommandValue(workflow.ID) + " --step STEP --node NODE --case CASE --audit --json",
			"agent-testbench workflow discover --filter " + quoteCommandValue(workflow.ID) + " --json",
		},
	}
	if options.Audit {
		audit, err := auditWorkflowCatalog(ctx, runtime, catalog, workflow.ID)
		if err != nil {
			return workflowCatalogUpsertReport{}, err
		}
		report.Audit = &audit
	}
	return report, nil
}

func upsertWorkflowCatalogBinding(ctx context.Context, runtime store.Store, options workflowBindingRegisterOptions) (workflowCatalogUpsertReport, error) {
	catalog, err := loadMutableProfileCatalog(ctx, runtime, options.ProfileID)
	if err != nil {
		return workflowCatalogUpsertReport{}, err
	}
	if _, ok := findCatalogWorkflow(catalog.Workflows, options.WorkflowID); !ok {
		return workflowCatalogUpsertReport{}, fmt.Errorf("workflow not found: %s", strings.TrimSpace(options.WorkflowID))
	}
	beforeCounts := profileImportCountsFromCatalog(catalog)
	binding, exists := findCatalogWorkflowBinding(catalog.WorkflowBindings, options.WorkflowID, options.StepID)
	binding.WorkflowID = strings.TrimSpace(options.WorkflowID)
	binding.StepID = strings.TrimSpace(options.StepID)
	binding.NodeID = strings.TrimSpace(options.NodeID)
	if options.PassedFlags["case"] || !exists {
		binding.CaseID = strings.TrimSpace(options.CaseID)
	}
	if options.PassedFlags["required"] {
		binding.Required = options.Required
	} else if !exists {
		binding.Required = true
	}
	if options.PassedFlags["sort-order"] {
		binding.SortOrder = options.SortOrder
	} else if !exists {
		binding.SortOrder = nextWorkflowBindingSortOrder(catalog.WorkflowBindings, binding.WorkflowID)
	}
	catalog.WorkflowBindings = upsertCatalogWorkflowBinding(catalog.WorkflowBindings, binding)
	catalog.IndexedAt = time.Now().UTC()
	if err := runtime.ReplaceProfileCatalog(ctx, catalog); err != nil {
		return workflowCatalogUpsertReport{}, err
	}
	report := workflowCatalogUpsertReport{
		OK:        true,
		Operation: "workflow binding upsert",
		ProfileID: catalog.ProfileID,
		Created:   !exists,
		Updated:   exists,
		Binding:   workflowUpsertBindingFromCatalog(binding),
		Counts: workflowCatalogUpsertCounts{
			Before: beforeCounts,
			After:  profileImportCountsFromCatalog(catalog),
		},
		NextActions: []string{
			"agent-testbench workflow plan --workflow " + quoteCommandValue(binding.WorkflowID) + " --json",
			"agent-testbench workflow audit --workflow " + quoteCommandValue(binding.WorkflowID) + " --json",
		},
	}
	if options.Audit {
		audit, err := auditWorkflowCatalog(ctx, runtime, catalog, binding.WorkflowID)
		if err != nil {
			return workflowCatalogUpsertReport{}, err
		}
		report.Audit = &audit
	}
	return report, nil
}

func loadMutableProfileCatalog(ctx context.Context, runtime store.Store, requestedProfileID string) (store.ProfileCatalog, error) {
	requestedProfileID = strings.TrimSpace(requestedProfileID)
	catalog, err := runtime.GetProfileCatalog(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return store.ProfileCatalog{ProfileID: firstNonEmpty(requestedProfileID, "default"), IndexedAt: time.Now().UTC()}, nil
	}
	if err != nil {
		return store.ProfileCatalog{}, err
	}
	if strings.TrimSpace(catalog.ProfileID) == "" {
		catalog.ProfileID = firstNonEmpty(requestedProfileID, "default")
	}
	if requestedProfileID != "" && catalog.ProfileID != requestedProfileID {
		return store.ProfileCatalog{}, fmt.Errorf("store profile catalog is %q, not %q", catalog.ProfileID, requestedProfileID)
	}
	return catalog, nil
}

func auditWorkflowCatalog(ctx context.Context, runtime store.Store, catalog store.ProfileCatalog, workflowID string) (workflowaudit.Report, error) {
	return workflowaudit.Audit(ctx, workflowaudit.Options{
		Bundle:     profilecatalog.ToBundle(catalog),
		WorkflowID: workflowID,
		Store:      runtime,
	})
}

func parsedFlagNames(flags *flag.FlagSet) map[string]bool {
	out := map[string]bool{}
	flags.Visit(func(item *flag.Flag) {
		out[item.Name] = true
	})
	return out
}

func findCatalogWorkflow(items []store.CatalogWorkflow, id string) (store.CatalogWorkflow, bool) {
	id = strings.TrimSpace(id)
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return store.CatalogWorkflow{}, false
}

func upsertCatalogWorkflow(items []store.CatalogWorkflow, update store.CatalogWorkflow) []store.CatalogWorkflow {
	out := append([]store.CatalogWorkflow(nil), items...)
	for index, item := range out {
		if item.ID == update.ID {
			out[index] = update
			sortCatalogWorkflows(out)
			return out
		}
	}
	out = append(out, update)
	sortCatalogWorkflows(out)
	return out
}

func sortCatalogWorkflows(items []store.CatalogWorkflow) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
}

func findCatalogWorkflowBinding(items []store.CatalogWorkflowBinding, workflowID string, stepID string) (store.CatalogWorkflowBinding, bool) {
	key := catalogWorkflowBindingKey(workflowID, stepID)
	for _, item := range items {
		if catalogWorkflowBindingKey(item.WorkflowID, item.StepID) == key {
			return item, true
		}
	}
	return store.CatalogWorkflowBinding{}, false
}

func upsertCatalogWorkflowBinding(items []store.CatalogWorkflowBinding, update store.CatalogWorkflowBinding) []store.CatalogWorkflowBinding {
	out := append([]store.CatalogWorkflowBinding(nil), items...)
	updateKey := catalogWorkflowBindingKey(update.WorkflowID, update.StepID)
	for index, item := range out {
		if catalogWorkflowBindingKey(item.WorkflowID, item.StepID) == updateKey {
			out[index] = update
			sortCatalogWorkflowBindings(out)
			return out
		}
	}
	out = append(out, update)
	sortCatalogWorkflowBindings(out)
	return out
}

func sortCatalogWorkflowBindings(items []store.CatalogWorkflowBinding) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.SortOrder != right.SortOrder {
			return left.SortOrder < right.SortOrder
		}
		if left.WorkflowID != right.WorkflowID {
			return left.WorkflowID < right.WorkflowID
		}
		return left.StepID < right.StepID
	})
}

func catalogWorkflowBindingKey(workflowID string, stepID string) string {
	return strings.TrimSpace(workflowID) + "\x00" + strings.TrimSpace(stepID)
}

func nextWorkflowBindingSortOrder(items []store.CatalogWorkflowBinding, workflowID string) int {
	maxOrder := 0
	for _, item := range items {
		if item.WorkflowID == workflowID && item.SortOrder > maxOrder {
			maxOrder = item.SortOrder
		}
	}
	return maxOrder + 1
}

func workflowUpsertWorkflowFromCatalog(item store.CatalogWorkflow) *workflowUpsertWorkflow {
	return &workflowUpsertWorkflow{
		ID:                item.ID,
		DisplayName:       item.DisplayName,
		Description:       item.Description,
		BaseStepTimeoutMs: item.BaseStepTimeoutMs,
		TimeoutOffsetMs:   item.TimeoutOffsetMs,
	}
}

func workflowUpsertBindingFromCatalog(item store.CatalogWorkflowBinding) *workflowUpsertBinding {
	return &workflowUpsertBinding{
		WorkflowID: item.WorkflowID,
		StepID:     item.StepID,
		NodeID:     item.NodeID,
		CaseID:     item.CaseID,
		Required:   item.Required,
		SortOrder:  item.SortOrder,
	}
}

func printWorkflowCatalogUpsertReport(report workflowCatalogUpsertReport) {
	if report.Workflow != nil {
		fmt.Printf("Workflow Upsert: %s\n", report.Workflow.ID)
		if report.Workflow.DisplayName != "" {
			fmt.Printf("Display Name: %s\n", report.Workflow.DisplayName)
		}
	}
	if report.Binding != nil {
		fmt.Printf("Workflow Binding Upsert: %s %s\n", report.Binding.WorkflowID, report.Binding.StepID)
		fmt.Printf("Node: %s\n", report.Binding.NodeID)
		if report.Binding.CaseID != "" {
			fmt.Printf("Case: %s\n", report.Binding.CaseID)
		}
		fmt.Printf("Required: %t\n", report.Binding.Required)
	}
	fmt.Printf("Profile: %s\n", report.ProfileID)
	fmt.Printf("Created: %t\n", report.Created)
	fmt.Printf("Updated: %t\n", report.Updated)
	fmt.Printf("Workflows: %d -> %d\n", report.Counts.Before.Workflows, report.Counts.After.Workflows)
	fmt.Printf("Workflow Bindings: %d -> %d\n", report.Counts.Before.WorkflowBindings, report.Counts.After.WorkflowBindings)
	if report.Audit != nil {
		fmt.Printf("Audit OK: %t\n", report.Audit.OK)
		fmt.Printf("Audit Issues: %d\n", report.Audit.IssueCount)
		for _, issue := range report.Audit.Issues {
			fmt.Printf("- [%s] %s %s %s: %s\n", issue.Severity, issue.Code, issue.SubjectType, issue.SubjectID, issue.Message)
		}
	}
	for _, action := range report.NextActions {
		fmt.Printf("Next: %s\n", action)
	}
}
