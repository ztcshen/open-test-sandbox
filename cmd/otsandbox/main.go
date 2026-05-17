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
	"sort"
	"strconv"
	"strings"
	"time"

	"open-test-sandbox/internal/apicase"
	"open-test-sandbox/internal/controlplane"
	"open-test-sandbox/internal/evidence"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/profileaudit"
	"open-test-sandbox/internal/profilecatalog"
	"open-test-sandbox/internal/profilehome"
	"open-test-sandbox/internal/requesttemplate"
	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
	"open-test-sandbox/internal/workflowaudit"
)

const version = "0.1.0"

type profileImportReport struct {
	ProfileID     string               `json:"profileId"`
	BundlePath    string               `json:"bundlePath"`
	BundleDigest  string               `json:"bundleDigest"`
	Counts        profileImportCounts  `json:"counts"`
	StorePath     string               `json:"storePath"`
	CatalogIndex  profileCatalogIndex  `json:"catalogIndex"`
	ConfigVersion profileConfigVersion `json:"configVersion"`
	ReadModels    []string             `json:"readModels"`
	ImportedAt    time.Time            `json:"importedAt"`
	Audit         *profileaudit.Report `json:"audit,omitempty"`
}

type profileImportCounts struct {
	Services         int `json:"services"`
	Workflows        int `json:"workflows"`
	InterfaceNodes   int `json:"interfaceNodes"`
	APICases         int `json:"apiCases"`
	RequestTemplates int `json:"requestTemplates"`
	CaseDependencies int `json:"caseDependencies"`
	WorkflowBindings int `json:"workflowBindings"`
	Fixtures         int `json:"fixtures"`
}

type profileCatalogIndex struct {
	ProfileID string                    `json:"profileId"`
	IndexedAt time.Time                 `json:"indexedAt"`
	Counts    profileCatalogIndexCounts `json:"counts"`
}

type profileCatalogIndexCounts struct {
	Services         int `json:"services"`
	Workflows        int `json:"workflows"`
	InterfaceNodes   int `json:"interfaceNodes"`
	APICases         int `json:"apiCases"`
	RequestTemplates int `json:"requestTemplates"`
	CaseDependencies int `json:"caseDependencies"`
	WorkflowBindings int `json:"workflowBindings"`
	Fixtures         int `json:"fixtures"`
	Templates        int `json:"templates"`
	TemplateConfigs  int `json:"templateConfigs"`
}

type profileConfigVersion struct {
	ID           string    `json:"id"`
	ProfileID    string    `json:"profileId"`
	SourcePath   string    `json:"sourcePath"`
	BundleDigest string    `json:"bundleDigest"`
	Active       bool      `json:"active"`
	PublishedAt  time.Time `json:"publishedAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

type profileVerifyReport struct {
	OK        bool                 `json:"ok"`
	Error     string               `json:"error,omitempty"`
	ProfileID string               `json:"profileId"`
	Audit     profileaudit.Report  `json:"audit"`
	Publish   profileImportReport  `json:"publish"`
	Summary   profileVerifySummary `json:"summary"`
	Checks    []profileVerifyCheck `json:"checks"`
}

type profileVerifySummary struct {
	TotalChecks          int    `json:"totalChecks"`
	PassedChecks         int    `json:"passedChecks"`
	FailedChecks         int    `json:"failedChecks"`
	RequiredCaseRuns     bool   `json:"requiredCaseRuns"`
	RequiredWorkflowRuns bool   `json:"requiredWorkflowRuns"`
	FirstFailed          string `json:"firstFailed,omitempty"`
}

type profileVerifyCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type profileVerifyOptions struct {
	RequireCaseRuns     bool
	RequireWorkflowRuns bool
}

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
	case "config":
		if err := runConfig(context.Background(), os.Args[2:]); err != nil {
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
	case "interface-node":
		if err := runInterfaceNode(os.Args[2:]); err != nil {
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
  otsandbox store upgrade [--store-url PATH]
  otsandbox profile init --output PATH [--id ID] [--display-name NAME] [--force]
  otsandbox profile install --from PATH [--profile-home PATH] [--force]
  otsandbox profile pack --profile PATH_OR_ID --output PATH [--profile-home PATH] [--force]
  otsandbox profile list [--profile-home PATH] [--json]
  otsandbox profile inspect --profile PATH_OR_ID [--profile-home PATH]
  otsandbox profile audit --profile PATH_OR_ID [--profile-home PATH] [--store-url PATH] [--json] [--force]
  otsandbox profile verify --profile PATH_OR_ID [--profile-home PATH] [--store-url PATH] [--require-case-runs] [--require-workflow-runs] [--json] [--force]
  otsandbox profile import --from PATH_OR_ID [--profile-home PATH] [--store-url PATH] [--json] [--audit] [--require-audit-ok] [--force]
  otsandbox config publish --from PATH_OR_ID [--profile-home PATH] [--store-url PATH] [--json] [--audit] [--require-audit-ok] [--force]
  otsandbox evidence import --from PATH --profile ID [--store-url PATH]
  otsandbox evidence list [--store-url PATH] [--run ID] [--json]
  otsandbox workflow discover [--profile PATH_OR_ID] [--profile-home PATH] [--store-url PATH] [--filter TEXT] [--json]
  otsandbox workflow plan --profile PATH --workflow ID
  otsandbox workflow audit --profile PATH --workflow ID [--store-url PATH] [--json]
  otsandbox workflow report --workflow ID [--profile PATH_OR_ID] [--profile-home PATH] [--store-url PATH] [--base-url URL] [--output-dir PATH] [--json]
  otsandbox baseline get --profile ID --subject ID [--store-url PATH]
  otsandbox baseline set --profile ID --subject ID --status STATUS [--required] [--store-url PATH]
  otsandbox template render --profile PATH --template ID [--fixture ID]
  otsandbox interface-node discover [--profile PATH_OR_ID] [--profile-home PATH] [--store-url PATH] [--filter TEXT] [--json]
  otsandbox interface-node case audit --profile PATH --node ID [--json]
  otsandbox interface-node case apply --profile PATH --file PATH [--json]
  otsandbox interface-node case report --node ID [--profile PATH_OR_ID] [--profile-home PATH] [--store-url PATH] [--base-url URL] [--output-dir PATH] [--timeout-seconds N] [--json]
  otsandbox case discover [--profile PATH_OR_ID] [--profile-home PATH] [--store-url PATH] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--json]
  otsandbox case suite report [--profile PATH_OR_ID] [--profile-home PATH] [--store-url PATH] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--base-url URL] [--output-dir PATH] [--timeout-seconds N] [--json]
  otsandbox case run --case PATH --base-url URL [--override KEY=VALUE] [--evidence-dir PATH]
  otsandbox case incomplete-batches --profile PATH [--store-url PATH] [--json]
  otsandbox serve [--profile PATH_OR_ID] [--profile-home PATH] [--host HOST] [--port PORT] [--store-url PATH]
  otsandbox help

Serve reads profile catalog data from the local Store. When --profile is set,
the external bundle is first published into the Store/read-model, then served
from that indexed view.`)
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
		status, err := sqlite.SchemaStatus(ctx, cfg)
		if err != nil {
			return err
		}
		printStoreStatus(status)
	case "upgrade":
		status, err := sqlite.UpgradeSchema(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Upgraded store schema to version %d\n", status.CurrentVersion)
		fmt.Printf("Applied: %d\n", status.AppliedCount)
		fmt.Printf("Path: %s\n", status.Path)
	default:
		return fmt.Errorf("unknown store command: %s", args[0])
	}
	return nil
}

func printStoreStatus(status sqlite.SchemaStatusResult) {
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
	case "init":
		return runProfileInit(args[1:])
	case "install":
		return runProfileInstall(args[1:])
	case "pack":
		return runProfilePack(args[1:])
	case "list":
		return runProfileList(args[1:])
	case "inspect":
		return runProfileInspect(args[1:])
	case "audit":
		return runProfileAudit(context.Background(), args[1:])
	case "import":
		return runProfileImport(context.Background(), args[1:])
	case "verify":
		return runProfileVerify(context.Background(), args[1:])
	default:
		return fmt.Errorf("unknown profile command: %s", args[0])
	}
}

func runConfig(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing config command")
	}
	switch args[0] {
	case "publish", "apply":
		return runConfigPublish(ctx, args[1:])
	default:
		return fmt.Errorf("unknown config command: %s", args[0])
	}
}

func runProfileInit(args []string) error {
	flags := flag.NewFlagSet("profile init", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	outputPath := flags.String("output", "", "External profile bundle output path")
	profileID := flags.String("id", "local", "Profile id")
	displayName := flags.String("display-name", "Local Profile", "Profile display name")
	force := flags.Bool("force", false, "Overwrite generated files when they already exist")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := initProfileBundle(*outputPath, *profileID, *displayName, *force)
	if err != nil {
		return err
	}
	fmt.Printf("Initialized external profile bundle: %s\n", report.ID)
	fmt.Printf("Path: %s\n", report.Path)
	fmt.Printf("Manifest: %s\n", filepath.Join(report.Path, "profile.json"))
	return nil
}

type profileInitReport struct {
	ID   string
	Path string
}

type profileInstallReport = profilehome.InstallReport

type profilePackReport = profilehome.PackReport

type profileListReport = profilehome.ListReport

type profileListItem = profilehome.ListItem

func initProfileBundle(outputPath string, profileID string, displayName string, force bool) (profileInitReport, error) {
	outputPath = strings.TrimSpace(outputPath)
	profileID = strings.TrimSpace(profileID)
	displayName = strings.TrimSpace(displayName)
	if outputPath == "" {
		return profileInitReport{}, errors.New("--output is required")
	}
	if profileID == "" {
		return profileInitReport{}, errors.New("--id must not be empty")
	}
	if displayName == "" {
		return profileInitReport{}, errors.New("--display-name must not be empty")
	}
	if isCoreProfilesPath(outputPath) {
		return profileInitReport{}, errors.New("profile bundles must be initialized outside this core repository")
	}
	if err := os.MkdirAll(outputPath, 0o755); err != nil {
		return profileInitReport{}, err
	}
	for _, dir := range []string{
		"services",
		"workflows",
		"interface-nodes",
		"cases",
		"request-templates",
		"case-dependencies",
		"workflow-bindings",
		"fixtures",
	} {
		if err := os.MkdirAll(filepath.Join(outputPath, dir), 0o755); err != nil {
			return profileInitReport{}, err
		}
	}
	manifestPath := filepath.Join(outputPath, "profile.json")
	if _, err := os.Stat(manifestPath); err == nil && !force {
		return profileInitReport{}, fmt.Errorf("%s already exists; pass --force to overwrite generated files", manifestPath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return profileInitReport{}, err
	}
	manifest := profile.Bundle{
		ID:               profileID,
		DisplayName:      displayName,
		Description:      "External profile bundle generated by Open Test Sandbox.",
		Services:         []profile.Service{},
		Workflows:        []profile.Workflow{},
		InterfaceNodes:   []profile.InterfaceNode{},
		APICases:         []profile.APICase{},
		RequestTemplates: []profile.RequestTemplate{},
		CaseDependencies: []profile.CaseDependency{},
		WorkflowBindings: []profile.WorkflowBinding{},
		Fixtures:         []profile.Fixture{},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return profileInitReport{}, err
	}
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o644); err != nil {
		return profileInitReport{}, err
	}
	readmePath := filepath.Join(outputPath, "README.md")
	if _, err := os.Stat(readmePath); errors.Is(err, os.ErrNotExist) || force {
		body := "# External Profile Bundle\n\nPublish this bundle into a local Store before serving it through Open Test Sandbox:\n\n```sh\notsandbox config publish --from . --store-url .runtime/store.sqlite\notsandbox serve --store-url .runtime/store.sqlite\n```\n"
		if err := os.WriteFile(readmePath, []byte(body), 0o644); err != nil {
			return profileInitReport{}, err
		}
	} else if err != nil {
		return profileInitReport{}, err
	}
	ignorePath := filepath.Join(outputPath, ".gitignore")
	if _, err := os.Stat(ignorePath); errors.Is(err, os.ErrNotExist) || force {
		body := ".runtime/\n*.sqlite\n*.sqlite-*\n*.db\n*.db-*\n*.log\n"
		if err := os.WriteFile(ignorePath, []byte(body), 0o644); err != nil {
			return profileInitReport{}, err
		}
	} else if err != nil {
		return profileInitReport{}, err
	}
	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return profileInitReport{}, err
	}
	return profileInitReport{ID: profileID, Path: absPath}, nil
}

func runProfileInstall(args []string) error {
	flags := flag.NewFlagSet("profile install", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	from := flags.String("from", "", "External profile bundle path")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	force := flags.Bool("force", false, "Replace an already installed profile")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := installProfileBundle(*from, *profileHome, *force)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Installed profile: %s\n", report.ID)
	fmt.Printf("Path: %s\n", report.TargetPath)
	fmt.Printf("Digest: %s\n", report.BundleDigest)
	return nil
}

func runProfilePack(args []string) error {
	flags := flag.NewFlagSet("profile pack", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profileRef := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	outputPath := flags.String("output", "", "Archive output path")
	force := flags.Bool("force", false, "Replace an existing archive")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := packProfileBundle(*profileRef, *profileHome, *outputPath, *force)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Packed profile: %s\n", report.ID)
	fmt.Printf("Archive: %s\n", report.OutputPath)
	fmt.Printf("Files: %d\n", report.FileCount)
	fmt.Printf("Digest: %s\n", report.BundleDigest)
	return nil
}

func runProfileList(args []string) error {
	flags := flag.NewFlagSet("profile list", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := listInstalledProfiles(*profileHome)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Profile Home: %s\n", report.ProfileHome)
	if len(report.Profiles) == 0 {
		fmt.Println("Profiles: 0")
		return nil
	}
	for _, item := range report.Profiles {
		if !item.Valid {
			fmt.Printf("- %s (invalid) %s: %s\n", item.ID, item.Path, item.Error)
			continue
		}
		fmt.Printf("- %s (%s) %s\n", item.ID, item.DisplayName, item.Path)
	}
	return nil
}

func installProfileBundle(from string, profileHome string, force bool) (profileInstallReport, error) {
	return profilehome.Install(from, profileHome, force)
}

func packProfileBundle(profileRef string, profileHome string, outputPath string, force bool) (profilePackReport, error) {
	return profilehome.Pack(profileRef, profileHome, outputPath, force)
}

func listInstalledProfiles(profileHome string) (profileListReport, error) {
	return profilehome.List(profileHome)
}

func resolveProfileHome(value string) (string, error) {
	return profilehome.ResolveHome(value)
}

func resolveProfileReference(value string, profileHome string) (string, error) {
	return profilehome.ResolveReference(value, profileHome)
}

func materializeProfileReference(value string, profileHome string, force bool) (string, error) {
	resolved, err := resolveProfileReference(value, profileHome)
	if err != nil {
		return "", err
	}
	if !profilehome.IsArchivePath(resolved) {
		return resolved, nil
	}
	report, err := installProfileBundle(resolved, profileHome, force)
	if err != nil {
		return "", err
	}
	return report.TargetPath, nil
}

func isCoreProfilesPath(path string) bool {
	return profilehome.IsCoreProfilesPath(path)
}

func runProfileInspect(args []string) error {
	flags := flag.NewFlagSet("profile inspect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedProfilePath, err := resolveProfileReference(*profilePath, *profileHome)
	if err != nil {
		return err
	}
	bundle, err := profile.Load(resolvedProfilePath)
	if err != nil {
		return err
	}
	printProfile(bundle)
	return nil
}

func runProfileVerify(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	requireCaseRuns := flags.Bool("require-case-runs", false, "Require every API Case in the profile to have a latest passed Store run")
	requireWorkflowRuns := flags.Bool("require-workflow-runs", false, "Require every Workflow in the profile to have a latest passed Store run")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	force := flags.Bool("force", false, "Replace an installed profile when --profile points to a packed archive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := sqlite.ParseConfigFromURL(*storeURL)
	if err != nil {
		return err
	}
	cfg = cfg.Resolve()
	s, err := sqlite.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.Close()
	resolvedProfilePath, err := materializeProfileReference(*profilePath, *profileHome, *force)
	if err != nil {
		return err
	}
	report, err := verifyProfileBundle(ctx, s, resolvedProfilePath, cfg.Path, profileVerifyOptions{
		RequireCaseRuns:     *requireCaseRuns,
		RequireWorkflowRuns: *requireWorkflowRuns,
	})
	if err != nil {
		if *jsonOutput && report.ProfileID != "" {
			if report.Error == "" {
				report.Error = err.Error()
			}
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			if encodeErr := encoder.Encode(report); encodeErr != nil {
				return encodeErr
			}
		}
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printProfileVerify(report)
	return nil
}

func runProfileImport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile import", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return runConfigPublishWithFlags(ctx, flags, args, "Imported profile")
}

func runConfigPublish(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("config publish", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return runConfigPublishWithFlags(ctx, flags, args, "Published config")
}

func runConfigPublishWithFlags(ctx context.Context, flags *flag.FlagSet, args []string, textPrefix string) error {
	from := flags.String("from", "", "Profile bundle path")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	auditOutput := flags.Bool("audit", false, "Run profile audit after import")
	requireAuditOK := flags.Bool("require-audit-ok", false, "Fail before writing the Store unless profile audit has no issues")
	force := flags.Bool("force", false, "Replace an installed profile when --from points to a packed archive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := sqlite.ParseConfigFromURL(*storeURL)
	if err != nil {
		return err
	}
	cfg = cfg.Resolve()
	s, err := sqlite.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	resolvedFrom, err := materializeProfileReference(*from, *profileHome, *force)
	if err != nil {
		return err
	}
	report, err := publishProfileBundleToStore(ctx, s, resolvedFrom, cfg.Path, *auditOutput, *requireAuditOK)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Printf("%s: %s\n", textPrefix, report.ProfileID)
	fmt.Printf("Digest: %s\n", report.BundleDigest)
	if report.Audit != nil {
		printProfileImportAudit(*report.Audit)
	}
	return nil
}

func publishProfileBundleToStore(ctx context.Context, s store.Store, from string, storePath string, auditOutput bool, requireAuditOK bool) (profileImportReport, error) {
	bundle, err := profile.Load(from)
	if err != nil {
		return profileImportReport{}, err
	}
	if requireAuditOK {
		auditReport, err := profileaudit.Audit(ctx, profileaudit.Options{
			Bundle:     bundle,
			BundlePath: from,
		})
		if err != nil {
			return profileImportReport{}, err
		}
		if !auditReport.OK {
			return profileImportReport{}, fmt.Errorf("profile audit failed for profile %q: %s", bundle.ID, profileaudit.FailureSummary(auditReport))
		}
	}
	digest, err := profile.BundleDigest(from)
	if err != nil {
		return profileImportReport{}, err
	}
	summary, err := json.Marshal(bundle.Counts())
	if err != nil {
		return profileImportReport{}, err
	}
	importedAt := time.Now().UTC()
	if _, err := s.UpsertProfileIndex(ctx, store.ProfileIndex{
		ProfileID:    bundle.ID,
		BundlePath:   from,
		BundleDigest: digest,
		SummaryJSON:  string(summary),
		ImportedAt:   importedAt,
	}); err != nil {
		return profileImportReport{}, err
	}
	catalog := profilecatalog.FromBundle(bundle, importedAt)
	if err := s.ReplaceProfileCatalog(ctx, catalog); err != nil {
		return profileImportReport{}, err
	}
	configVersion, err := s.UpsertConfigVersion(ctx, store.ConfigVersion{
		ID:           configVersionID(bundle.ID, importedAt),
		ProfileID:    bundle.ID,
		SourcePath:   from,
		BundleDigest: digest,
		SummaryJSON:  string(summary),
		Active:       true,
		PublishedAt:  importedAt,
		CreatedAt:    importedAt,
	})
	if err != nil {
		return profileImportReport{}, err
	}
	readModelKeys, err := controlplane.UpsertProfileReadModels(ctx, s, catalog, configVersion.ID, importedAt)
	if err != nil {
		return profileImportReport{}, err
	}
	catalogIndex, err := s.GetProfileCatalogIndex(ctx)
	if err != nil {
		return profileImportReport{}, err
	}
	report := profileImportReport{
		ProfileID:     bundle.ID,
		BundlePath:    from,
		BundleDigest:  digest,
		Counts:        profileImportAssetCounts(bundle.Counts()),
		StorePath:     storePath,
		CatalogIndex:  profileCatalogIndexFromStore(catalogIndex),
		ConfigVersion: profileConfigVersionFromStore(configVersion),
		ReadModels:    readModelKeys,
		ImportedAt:    importedAt,
	}
	if auditOutput {
		auditReport, err := profileaudit.Audit(ctx, profileaudit.Options{
			Bundle:     bundle,
			BundlePath: from,
			Store:      s,
		})
		if err != nil {
			return profileImportReport{}, err
		}
		report.Audit = &auditReport
	}
	return report, nil
}

func verifyProfileBundle(ctx context.Context, s store.Store, profilePath string, storePath string, options profileVerifyOptions) (profileVerifyReport, error) {
	bundle, err := profile.Load(profilePath)
	if err != nil {
		return profileVerifyReport{}, err
	}
	auditReport, err := profileaudit.Audit(ctx, profileaudit.Options{
		Bundle:     bundle,
		BundlePath: profilePath,
	})
	if err != nil {
		return profileVerifyReport{}, err
	}
	if !auditReport.OK {
		return profileVerifyReport{}, fmt.Errorf("profile audit failed for profile %q: %s", bundle.ID, profileaudit.FailureSummary(auditReport))
	}
	publishReport, err := publishProfileBundleToStore(ctx, s, profilePath, storePath, true, true)
	if err != nil {
		return profileVerifyReport{}, err
	}
	checks, err := verifyPublishedProfile(ctx, s, bundle, publishReport, options)
	if err != nil {
		return profileVerifyReport{}, err
	}
	report := profileVerifyReport{
		OK:        profileChecksOK(checks),
		ProfileID: bundle.ID,
		Audit:     *publishReport.Audit,
		Publish:   publishReport,
		Summary:   summarizeProfileVerification(checks, options),
		Checks:    checks,
	}
	if !report.OK {
		report.Error = fmt.Sprintf("profile verification failed for profile %q: %s", bundle.ID, firstFailedProfileCheck(checks))
		return report, fmt.Errorf("profile verification failed for profile %q: %s", bundle.ID, firstFailedProfileCheck(checks))
	}
	return report, nil
}

func summarizeProfileVerification(checks []profileVerifyCheck, options profileVerifyOptions) profileVerifySummary {
	summary := profileVerifySummary{
		TotalChecks:          len(checks),
		RequiredCaseRuns:     options.RequireCaseRuns,
		RequiredWorkflowRuns: options.RequireWorkflowRuns,
	}
	for _, check := range checks {
		if check.OK {
			summary.PassedChecks++
			continue
		}
		summary.FailedChecks++
		if summary.FirstFailed == "" {
			summary.FirstFailed = check.Name
		}
	}
	return summary
}

func verifyPublishedProfile(ctx context.Context, s store.Store, bundle profile.Bundle, report profileImportReport, options profileVerifyOptions) ([]profileVerifyCheck, error) {
	checks := make([]profileVerifyCheck, 0, 6)
	index, err := s.GetProfileIndex(ctx, report.ProfileID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			checks = appendProfileCheck(checks, "profile-index", false, "profile index was not written")
			return checks, nil
		}
		return nil, err
	}
	checks = appendProfileCheck(checks, "profile-index", index.BundleDigest == report.BundleDigest, "profile index digest matches published bundle")

	catalogIndex, err := s.GetProfileCatalogIndex(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			checks = appendProfileCheck(checks, "catalog-index", false, "catalog index was not written")
		} else {
			return nil, err
		}
	} else {
		checks = appendProfileCheck(checks, "catalog-index", catalogIndex.ProfileID == report.ProfileID, "catalog index points to active profile")
	}

	activeConfig, err := s.GetActiveConfigVersion(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			checks = appendProfileCheck(checks, "active-config", false, "active config version was not written")
		} else {
			return nil, err
		}
	} else {
		ok := activeConfig.ID == report.ConfigVersion.ID && activeConfig.ProfileID == report.ProfileID && activeConfig.BundleDigest == report.BundleDigest
		checks = appendProfileCheck(checks, "active-config", ok, "active config version matches published bundle")
	}

	for _, key := range []string{profilecatalog.ReadModelInterfaceNodes, controlplane.ReadModelCatalog, controlplane.ReadModelDashboard} {
		model, err := s.GetReadModel(ctx, report.ProfileID, key)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				checks = appendProfileCheck(checks, "read-model:"+key, false, "read model was not written")
				continue
			}
			return nil, err
		}
		ok := model.ConfigVersionID == report.ConfigVersion.ID && strings.TrimSpace(model.PayloadJSON) != ""
		checks = appendProfileCheck(checks, "read-model:"+key, ok, "read model exists for published config version")
	}
	if options.RequireCaseRuns {
		caseRunChecks, err := verifyProfileAPICaseRuns(ctx, s, bundle)
		if err != nil {
			return nil, err
		}
		checks = append(checks, caseRunChecks...)
	}
	if options.RequireWorkflowRuns {
		workflowChecks, err := verifyProfileWorkflowRuns(ctx, s, bundle)
		if err != nil {
			return nil, err
		}
		checks = append(checks, workflowChecks...)
	}
	return checks, nil
}

func verifyProfileWorkflowRuns(ctx context.Context, s store.Store, bundle profile.Bundle) ([]profileVerifyCheck, error) {
	if len(bundle.Workflows) == 0 {
		return []profileVerifyCheck{{Name: "workflow-runs", OK: true, Detail: "profile declares no workflows"}}, nil
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	latestByWorkflow := map[string]store.Run{}
	for _, item := range runs {
		if item.WorkflowID == "" {
			continue
		}
		current, ok := latestByWorkflow[item.WorkflowID]
		if !ok || item.CreatedAt.After(current.CreatedAt) || (item.CreatedAt.Equal(current.CreatedAt) && item.ID > current.ID) {
			latestByWorkflow[item.WorkflowID] = item
		}
	}
	checks := make([]profileVerifyCheck, 0, len(bundle.Workflows))
	for _, item := range bundle.Workflows {
		run, ok := latestByWorkflow[item.ID]
		if !ok || !isPassedStatus(run.Status) {
			checks = appendProfileCheck(checks, "workflow-run:"+item.ID, false, "no passed run recorded in Store")
			continue
		}
		checks = appendProfileCheck(checks, "workflow-run:"+item.ID, true, "latest Workflow run passed")
	}
	return checks, nil
}

func verifyProfileAPICaseRuns(ctx context.Context, s store.Store, bundle profile.Bundle) ([]profileVerifyCheck, error) {
	if len(bundle.APICases) == 0 {
		return []profileVerifyCheck{{Name: "api-case-runs", OK: true, Detail: "profile declares no API cases"}}, nil
	}
	latestStore, ok := s.(interface {
		ListLatestAPICaseRuns(context.Context) ([]store.APICaseRun, error)
	})
	if !ok {
		return nil, errors.New("runtime store does not support latest API case run lookup")
	}
	latestRuns, err := latestStore.ListLatestAPICaseRuns(ctx)
	if err != nil {
		return nil, err
	}
	latestByCase := map[string]store.APICaseRun{}
	for _, item := range latestRuns {
		latestByCase[item.CaseID] = item
	}
	checks := make([]profileVerifyCheck, 0, len(bundle.APICases))
	for _, item := range bundle.APICases {
		run, ok := latestByCase[item.ID]
		if !ok || !isPassedStatus(run.Status) {
			checks = appendProfileCheck(checks, "api-case-run:"+item.ID, false, "no passed run recorded in Store")
			continue
		}
		checks = appendProfileCheck(checks, "api-case-run:"+item.ID, true, "latest API case run passed")
	}
	return checks, nil
}

func isPassedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass", "passed", "success", "ok":
		return true
	default:
		return false
	}
}

func appendProfileCheck(checks []profileVerifyCheck, name string, ok bool, detail string) []profileVerifyCheck {
	return append(checks, profileVerifyCheck{Name: name, OK: ok, Detail: detail})
}

func profileChecksOK(checks []profileVerifyCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}

func firstFailedProfileCheck(checks []profileVerifyCheck) string {
	for _, check := range checks {
		if !check.OK {
			return check.Name + ": " + check.Detail
		}
	}
	return "no checks passed"
}

func profileImportAssetCounts(counts profile.Counts) profileImportCounts {
	return profileImportCounts{
		Services:         counts.Services,
		Workflows:        counts.Workflows,
		InterfaceNodes:   counts.InterfaceNodes,
		APICases:         counts.APICases,
		RequestTemplates: counts.RequestTemplates,
		CaseDependencies: counts.CaseDependencies,
		WorkflowBindings: counts.WorkflowBindings,
		Fixtures:         counts.Fixtures,
	}
}

func profileCatalogIndexFromStore(index store.ProfileCatalogIndex) profileCatalogIndex {
	return profileCatalogIndex{
		ProfileID: index.ProfileID,
		IndexedAt: index.IndexedAt,
		Counts: profileCatalogIndexCounts{
			Services:         index.Counts.Services,
			Workflows:        index.Counts.Workflows,
			InterfaceNodes:   index.Counts.InterfaceNodes,
			APICases:         index.Counts.APICases,
			RequestTemplates: index.Counts.RequestTemplates,
			CaseDependencies: index.Counts.CaseDependencies,
			WorkflowBindings: index.Counts.WorkflowBindings,
			Fixtures:         index.Counts.Fixtures,
			Templates:        index.Counts.Templates,
			TemplateConfigs:  index.Counts.TemplateConfigs,
		},
	}
}

func profileConfigVersionFromStore(item store.ConfigVersion) profileConfigVersion {
	return profileConfigVersion{
		ID:           item.ID,
		ProfileID:    item.ProfileID,
		SourcePath:   item.SourcePath,
		BundleDigest: item.BundleDigest,
		Active:       item.Active,
		PublishedAt:  item.PublishedAt,
		CreatedAt:    item.CreatedAt,
	}
}

func configVersionID(profileID string, publishedAt time.Time) string {
	safeProfileID := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(strings.TrimSpace(profileID))
	if safeProfileID == "" {
		safeProfileID = "profile"
	}
	return "config." + safeProfileID + "." + publishedAt.UTC().Format("20060102T150405.000000000Z")
}

func printProfileImportAudit(report profileaudit.Report) {
	fmt.Printf("Audit OK: %t\n", report.OK)
	fmt.Printf("Audit Issues: %d\n", report.IssueCount)
	for _, item := range report.Issues {
		fmt.Printf("- [%s] %s %s %s: %s\n", item.Severity, item.Code, item.SubjectType, item.SubjectID, item.Message)
	}
}

func printProfileVerify(report profileVerifyReport) {
	fmt.Printf("Profile Verification: %s\n", report.ProfileID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Audit OK: %t\n", report.Audit.OK)
	fmt.Printf("Published Config: %s\n", report.Publish.ConfigVersion.ID)
	fmt.Printf("Read Models: %s\n", strings.Join(report.Publish.ReadModels, ", "))
	fmt.Printf("Checks: %d passed / %d total", report.Summary.PassedChecks, report.Summary.TotalChecks)
	if report.Summary.FailedChecks > 0 {
		fmt.Printf(" (%d failed", report.Summary.FailedChecks)
		if report.Summary.FirstFailed != "" {
			fmt.Printf(", first failed: %s", report.Summary.FirstFailed)
		}
		fmt.Print(")")
	}
	fmt.Println()
	fmt.Printf("Runtime Gates: api-cases=%t workflows=%t\n", report.Summary.RequiredCaseRuns, report.Summary.RequiredWorkflowRuns)
	fmt.Println("Checks:")
	for _, check := range report.Checks {
		fmt.Printf("- %s: %t (%s)\n", check.Name, check.OK, check.Detail)
	}
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

func runProfileAudit(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile audit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	force := flags.Bool("force", false, "Replace an installed profile when --profile points to a packed archive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedProfilePath, err := materializeProfileReference(*profilePath, *profileHome, *force)
	if err != nil {
		return err
	}
	bundle, err := profile.Load(resolvedProfilePath)
	if err != nil {
		return err
	}

	options := profileaudit.Options{
		Bundle:     bundle,
		BundlePath: resolvedProfilePath,
	}
	if strings.TrimSpace(*storeURL) != "" {
		s, err := openStore(ctx, *storeURL)
		if err != nil {
			return err
		}
		defer s.Close()
		options.Store = s
	}

	report, err := profileaudit.Audit(ctx, options)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printProfileAudit(report)
	return nil
}

func printProfileAudit(report profileaudit.Report) {
	fmt.Printf("Profile Audit: %s\n", report.ProfileID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Issues: %d\n", report.IssueCount)
	for _, item := range report.Issues {
		fmt.Printf("- [%s] %s %s %s: %s\n", item.Severity, item.Code, item.SubjectType, item.SubjectID, item.Message)
	}
	if report.Store == nil {
		return
	}
	fmt.Printf("Store Profile Indexed: %t\n", report.Store.ProfileIndexed)
	if report.Store.BundleDigest != "" || report.Store.IndexedDigest != "" {
		fmt.Printf("Store Digest Matches: %t\n", report.Store.DigestMatches)
	}
	for _, item := range report.Store.APICases {
		status := item.LatestStatus
		if status == "" {
			status = "not-run"
		}
		fmt.Printf("API Case: %s Status: %s Passed: %t\n", item.CaseID, status, item.HasPassed)
	}
}

type interfaceNodeCaseAuditReport struct {
	OK         bool                          `json:"ok"`
	ProfileID  string                        `json:"profileId"`
	NodeID     string                        `json:"nodeId"`
	Counts     interfaceNodeCaseAuditCounts  `json:"counts"`
	Configured []interfaceNodeCaseConfigured `json:"configured"`
	Missing    []interfaceNodeCaseMissing    `json:"missing"`
}

type interfaceNodeCaseAuditCounts struct {
	Cases      int `json:"cases"`
	Configured int `json:"configured"`
	Missing    int `json:"missing"`
}

type interfaceNodeCaseConfigured struct {
	CaseID   string `json:"caseId"`
	ConfigID string `json:"configId"`
}

type interfaceNodeCaseMissing struct {
	CaseID string `json:"caseId"`
	Title  string `json:"title,omitempty"`
}

type interfaceNodeCaseApplyRequest struct {
	TemplateConfigs []templateConfigInput `json:"templateConfigs"`
}

type templateConfigInput struct {
	profile.TemplateConfig
	Config json.RawMessage `json:"config,omitempty"`
}

type interfaceNodeListReport struct {
	OK        bool                    `json:"ok"`
	ProfileID string                  `json:"profileId"`
	Count     int                     `json:"count"`
	Items     []interfaceNodeListItem `json:"items"`
}

type interfaceNodeListItem struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Operation   string `json:"operation,omitempty"`
	Method      string `json:"method,omitempty"`
	Path        string `json:"path,omitempty"`
	ServiceID   string `json:"serviceId,omitempty"`
	CaseCount   int    `json:"caseCount"`
}

func runInterfaceNode(args []string) error {
	if len(args) == 0 {
		return errors.New("missing interface-node command")
	}
	if args[0] == "discover" {
		return runInterfaceNodeDiscover(context.Background(), args[1:])
	}
	if args[0] != "case" {
		return fmt.Errorf("unknown interface-node command: %s", args[0])
	}
	if len(args) < 2 {
		return errors.New("missing interface-node case command")
	}
	switch args[1] {
	case "audit":
		return runInterfaceNodeCaseAudit(args[2:])
	case "apply":
		return runInterfaceNodeCaseApply(args[2:])
	case "report":
		return runInterfaceNodeCaseReport(context.Background(), args[2:])
	default:
		return fmt.Errorf("unknown interface-node case command: %s", args[1])
	}
}

func runInterfaceNodeDiscover(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("interface-node discover", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeURL := flags.String("store-url", filepath.Join(".runtime", "acceptance.sqlite"), "SQLite store URL or path")
	filter := flags.String("filter", "", "Filter by id, display name, or operation")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, _, cleanup, err := loadInterfaceNodeReportBundle(ctx, *profilePath, *profileHome, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report := interfaceNodeList(bundle, *filter)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	for _, item := range report.Items {
		fmt.Printf("%s\t%s\t%d\n", item.ID, item.DisplayName, item.CaseCount)
	}
	return nil
}

func interfaceNodeList(bundle profile.Bundle, filter string) interfaceNodeListReport {
	caseCounts := map[string]int{}
	for _, item := range bundle.APICases {
		if strings.TrimSpace(item.NodeID) != "" {
			caseCounts[item.NodeID]++
		}
	}
	nodes := append([]profile.InterfaceNode(nil), bundle.InterfaceNodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].SortOrder != nodes[j].SortOrder {
			return nodes[i].SortOrder < nodes[j].SortOrder
		}
		return nodes[i].ID < nodes[j].ID
	})
	report := interfaceNodeListReport{OK: true, ProfileID: bundle.ID}
	for _, node := range nodes {
		if !matchesDiscoveryFilter(filter, node.ID, node.DisplayName, node.Operation) {
			continue
		}
		report.Items = append(report.Items, interfaceNodeListItem{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			Operation:   node.Operation,
			Method:      node.Method,
			Path:        node.Path,
			ServiceID:   node.ServiceID,
			CaseCount:   caseCounts[node.ID],
		})
	}
	report.Count = len(report.Items)
	return report
}

func runInterfaceNodeCaseAudit(args []string) error {
	flags := flag.NewFlagSet("interface-node case audit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	nodeID := flags.String("node", "", "Interface node id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*profilePath) == "" {
		return errors.New("--profile is required")
	}
	if strings.TrimSpace(*nodeID) == "" {
		return errors.New("--node is required")
	}
	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}
	report := auditInterfaceNodeCaseExecutionConfigs(bundle, *nodeID)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printInterfaceNodeCaseAudit(report)
	return nil
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

func caseExecutionConfigIDs(configs []profile.TemplateConfig) map[string]string {
	out := map[string]string{}
	for _, config := range configs {
		if config.Status != "" && config.Status != "active" {
			continue
		}
		caseID, ok := caseExecutionConfigCaseID(config.ConfigJSON)
		if ok {
			out[caseID] = config.ID
		}
	}
	return out
}

func caseExecutionConfigCaseID(configJSON string) (string, bool) {
	var parsed struct {
		CaseID        string `json:"caseId"`
		CaseExecution struct {
			Method string `json:"method"`
			NodeID string `json:"nodeId"`
			Path   string `json:"path"`
		} `json:"caseExecution"`
	}
	if err := json.Unmarshal([]byte(configJSON), &parsed); err != nil {
		return "", false
	}
	if strings.TrimSpace(parsed.CaseID) == "" {
		return "", false
	}
	if parsed.CaseExecution.Method == "" && parsed.CaseExecution.NodeID == "" && parsed.CaseExecution.Path == "" {
		return "", false
	}
	return parsed.CaseID, true
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

func runInterfaceNodeCaseApply(args []string) error {
	flags := flag.NewFlagSet("interface-node case apply", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	requestPath := flags.String("file", "", "Case execution config bundle")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*profilePath) == "" {
		return errors.New("--profile is required")
	}
	if strings.TrimSpace(*requestPath) == "" {
		return errors.New("--file is required")
	}
	applied, err := applyInterfaceNodeCaseConfigs(*profilePath, *requestPath)
	if err != nil {
		return err
	}
	result := map[string]any{"profile": *profilePath, "file": *requestPath, "applied": applied}
	if *jsonOutput {
		return writeIndentedJSON(result)
	}
	fmt.Printf("Applied interface node case configs: %d\n", applied)
	fmt.Printf("Profile: %s\n", *profilePath)
	return nil
}

func applyInterfaceNodeCaseConfigs(profilePath string, requestPath string) (int, error) {
	raw, err := os.ReadFile(requestPath)
	if err != nil {
		return 0, fmt.Errorf("read case config bundle %s: %w", requestPath, err)
	}
	var request interfaceNodeCaseApplyRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return 0, fmt.Errorf("decode case config bundle %s: %w", requestPath, err)
	}
	if len(request.TemplateConfigs) == 0 {
		return 0, errors.New("case config bundle must include templateConfigs")
	}
	configs := make([]profile.TemplateConfig, 0, len(request.TemplateConfigs))
	for _, item := range request.TemplateConfigs {
		config, err := normalizeTemplateConfigInput(item)
		if err != nil {
			return 0, err
		}
		configs = append(configs, config)
	}
	catalogPath := filepath.Join(profilePath, "catalog.json")
	payload, existing, err := readCatalogTemplateConfigs(catalogPath)
	if err != nil {
		return 0, err
	}
	merged := mergeTemplateConfigs(existing, configs)
	configRaw, err := json.Marshal(merged)
	if err != nil {
		return 0, err
	}
	payload["templateConfigs"] = configRaw
	if _, ok := payload["schemaVersion"]; !ok {
		payload["schemaVersion"] = json.RawMessage(`"1"`)
	}
	next, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return 0, err
	}
	next = append(next, '\n')
	if err := os.WriteFile(catalogPath, next, 0o644); err != nil {
		return 0, fmt.Errorf("write profile catalog %s: %w", catalogPath, err)
	}
	if _, err := profile.Load(profilePath); err != nil {
		return 0, fmt.Errorf("profile catalog is invalid after apply: %w", err)
	}
	return len(configs), nil
}

func normalizeTemplateConfigInput(input templateConfigInput) (profile.TemplateConfig, error) {
	config := input.TemplateConfig
	if len(input.Config) > 0 {
		compact, err := compactRawJSON(input.Config)
		if err != nil {
			return profile.TemplateConfig{}, fmt.Errorf("template config %q config is invalid: %w", config.ID, err)
		}
		config.ConfigJSON = compact
	}
	if strings.TrimSpace(config.ID) == "" {
		return profile.TemplateConfig{}, errors.New("template config id is required")
	}
	if strings.TrimSpace(config.ConfigJSON) == "" {
		return profile.TemplateConfig{}, fmt.Errorf("template config %q configJson is required", config.ID)
	}
	if caseID, ok := caseExecutionConfigCaseID(config.ConfigJSON); !ok {
		return profile.TemplateConfig{}, fmt.Errorf("template config %q must contain caseId and caseExecution", config.ID)
	} else if strings.TrimSpace(config.ScopeID) == "" {
		config.ScopeID = caseID
	}
	if strings.TrimSpace(config.ScopeType) == "" {
		config.ScopeType = "case"
	}
	if strings.TrimSpace(config.Status) == "" {
		config.Status = "active"
	}
	return config, nil
}

func writeIndentedJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func compactRawJSON(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	compact, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(compact), nil
}

func readCatalogTemplateConfigs(path string) (map[string]json.RawMessage, []profile.TemplateConfig, error) {
	payload := map[string]json.RawMessage{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return payload, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read profile catalog %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, fmt.Errorf("decode profile catalog %s: %w", path, err)
	}
	var configs []profile.TemplateConfig
	if rawConfigs, ok := payload["templateConfigs"]; ok {
		if err := json.Unmarshal(rawConfigs, &configs); err != nil {
			return nil, nil, fmt.Errorf("decode profile catalog templateConfigs %s: %w", path, err)
		}
	}
	return payload, configs, nil
}

func mergeTemplateConfigs(existing []profile.TemplateConfig, updates []profile.TemplateConfig) []profile.TemplateConfig {
	positions := map[string]int{}
	out := make([]profile.TemplateConfig, 0, len(existing)+len(updates))
	for _, item := range existing {
		positions[item.ID] = len(out)
		out = append(out, item)
	}
	for _, item := range updates {
		if index, ok := positions[item.ID]; ok {
			out[index] = item
			continue
		}
		positions[item.ID] = len(out)
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.SortOrder != right.SortOrder {
			return left.SortOrder < right.SortOrder
		}
		return left.ID < right.ID
	})
	return out
}

type interfaceNodeCaseReport struct {
	OK             bool                          `json:"ok"`
	ProfileID      string                        `json:"profileId"`
	NodeID         string                        `json:"nodeId"`
	NodeName       string                        `json:"nodeName"`
	Operation      string                        `json:"operation,omitempty"`
	Method         string                        `json:"method,omitempty"`
	Path           string                        `json:"path,omitempty"`
	ReportURL      string                        `json:"reportUrl"`
	JSONReportURL  string                        `json:"jsonReportUrl"`
	ElapsedMs      int64                         `json:"elapsedMs"`
	GeneratedAt    time.Time                     `json:"generatedAt"`
	Counts         interfaceNodeCaseReportCounts `json:"counts"`
	Results        []interfaceNodeCaseReportItem `json:"results"`
	Warnings       []string                      `json:"warnings,omitempty"`
	SourceStoreURL string                        `json:"sourceStoreUrl,omitempty"`
}

type interfaceNodeCaseReportCounts struct {
	Total          int `json:"total"`
	Passed         int `json:"passed"`
	Failed         int `json:"failed"`
	DerivedConfigs int `json:"derivedConfigs"`
}

type interfaceNodeCaseReportItem struct {
	CaseID      string `json:"caseId"`
	Title       string `json:"title"`
	RunID       string `json:"runId,omitempty"`
	CaseRunID   string `json:"caseRunId,omitempty"`
	ViewerURL   string `json:"viewerUrl,omitempty"`
	DetailURL   string `json:"detailUrl,omitempty"`
	Status      string `json:"status"`
	HTTPCode    int    `json:"httpCode,omitempty"`
	ElapsedMs   int64  `json:"elapsedMs"`
	Method      string `json:"method,omitempty"`
	Path        string `json:"path,omitempty"`
	FullURL     string `json:"fullUrl,omitempty"`
	BaseURL     string `json:"baseUrl,omitempty"`
	Error       string `json:"error,omitempty"`
	BodyPreview string `json:"bodyPreview,omitempty"`
}

func runInterfaceNodeCaseReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("interface-node case report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	nodeID := flags.String("node", "", "Interface node id")
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeURL := flags.String("store-url", filepath.Join(".runtime", "acceptance.sqlite"), "SQLite store URL or path")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	outputDir := flags.String("output-dir", "", "Report output directory")
	timeoutSeconds := flags.Int("timeout-seconds", 3, "Timeout per API Case")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*nodeID) == "" {
		return errors.New("--node is required")
	}
	if *timeoutSeconds <= 0 {
		return errors.New("--timeout-seconds must be greater than zero")
	}

	bundle, sourceStore, cleanup, err := loadInterfaceNodeReportBundle(ctx, *profilePath, *profileHome, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	node, err := findInterfaceNodeByID(bundle.InterfaceNodes, *nodeID)
	if err != nil {
		return err
	}
	cases := interfaceNodeReportCases(bundle.APICases, node.ID)
	if len(cases) == 0 {
		return fmt.Errorf("no API cases found for interface node %s", node.ID)
	}
	derived := deriveInterfaceNodeCaseConfigs(bundle, node, cases)
	bundle.TemplateConfigs = mergeTemplateConfigs(bundle.TemplateConfigs, derived)
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "node."+safeReportID(node.ID)+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeInterfaceNodeCaseReport(ctx, bundle, node, cases, derived, sourceStore, *storeURL, *baseURL, absOutputDir, *timeoutSeconds)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printInterfaceNodeCaseReport(report)
	return nil
}

func loadInterfaceNodeReportBundle(ctx context.Context, profileRef string, profileHomeRef string, storeURL string) (profile.Bundle, *sqlite.Store, func(), error) {
	cleanup := func() {}
	var sourceStore *sqlite.Store
	if strings.TrimSpace(storeURL) != "" {
		opened, err := openStore(ctx, storeURL)
		if err != nil {
			return profile.Bundle{}, nil, cleanup, err
		}
		sourceStore = opened
		cleanup = func() { _ = opened.Close() }
	}
	if strings.TrimSpace(profileRef) != "" {
		resolvedProfilePath, err := resolveProfileReference(profileRef, profileHomeRef)
		if err != nil {
			cleanup()
			return profile.Bundle{}, nil, func() {}, err
		}
		bundle, err := profile.Load(resolvedProfilePath)
		if err != nil {
			cleanup()
			return profile.Bundle{}, nil, func() {}, err
		}
		return bundle, sourceStore, cleanup, nil
	}
	if sourceStore == nil {
		return profile.Bundle{}, nil, cleanup, errors.New("--profile or --store-url is required")
	}
	bundle, err := serveBundle(ctx, sourceStore)
	if err != nil {
		cleanup()
		return profile.Bundle{}, nil, func() {}, err
	}
	return bundle, sourceStore, cleanup, nil
}

func findInterfaceNodeByID(nodes []profile.InterfaceNode, id string) (profile.InterfaceNode, error) {
	id = strings.TrimSpace(id)
	for _, node := range nodes {
		if node.ID == id {
			return node, nil
		}
	}
	return profile.InterfaceNode{}, fmt.Errorf("interface node not found: %s", id)
}

func normalizedDiscoveryText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, "interface")
	value = strings.TrimSuffix(value, "api")
	value = strings.TrimSuffix(value, "接口")
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "", "/", "")
	return replacer.Replace(strings.TrimSpace(value))
}

func matchesDiscoveryFilter(filter string, values ...string) bool {
	needle := normalizedDiscoveryText(filter)
	if needle == "" {
		return true
	}
	for _, value := range values {
		haystack := normalizedDiscoveryText(value)
		if haystack != "" && (strings.Contains(haystack, needle) || strings.Contains(needle, haystack)) {
			return true
		}
	}
	return false
}

func interfaceNodeReportCases(cases []profile.APICase, nodeID string) []profile.APICase {
	out := make([]profile.APICase, 0)
	for _, item := range cases {
		if item.NodeID == nodeID {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func deriveInterfaceNodeCaseConfigs(bundle profile.Bundle, node profile.InterfaceNode, cases []profile.APICase) []profile.TemplateConfig {
	caseSet := map[string]profile.APICase{}
	for _, item := range cases {
		caseSet[item.ID] = item
	}
	configured := caseExecutionConfigIDs(bundle.TemplateConfigs)
	base := map[string]any{}
	for _, config := range bundle.TemplateConfigs {
		caseID, ok := caseExecutionConfigCaseID(config.ConfigJSON)
		if !ok {
			continue
		}
		if _, belongs := caseSet[caseID]; !belongs {
			continue
		}
		if err := json.Unmarshal([]byte(config.ConfigJSON), &base); err == nil && len(mapFromReportAny(base["caseExecution"])) > 0 {
			break
		}
	}
	if len(base) == 0 {
		return nil
	}
	out := make([]profile.TemplateConfig, 0)
	for _, item := range cases {
		if configured[item.ID] != "" {
			continue
		}
		configJSON, ok := derivedCaseExecutionConfigJSON(base, node, item)
		if !ok {
			continue
		}
		out = append(out, profile.TemplateConfig{
			ID:         "cfg.generated." + safeReportID(item.ID),
			TemplateID: "case-execution",
			NodeID:     node.ID,
			ScopeType:  "case",
			ScopeID:    item.ID,
			Title:      firstNonEmpty(item.DisplayName, item.ID) + " execution",
			ConfigJSON: configJSON,
			Status:     "active",
			SortOrder:  item.SortOrder,
		})
	}
	return out
}

func derivedCaseExecutionConfigJSON(base map[string]any, node profile.InterfaceNode, item profile.APICase) (string, bool) {
	next := cloneMap(base)
	next["caseId"] = item.ID
	execution := mapFromReportAny(next["caseExecution"])
	if len(execution) == 0 {
		return "", false
	}
	mergePayloadTemplateIntoExecution(execution, item.PayloadTemplateJSON)
	mergeExpectedConfigIntoExecution(execution, item.ExpectedJSON)
	next["caseExecution"] = execution
	if caseBlock := mapFromReportAny(next["case"]); len(caseBlock) > 0 {
		caseBlock["id"] = item.ID
		caseBlock["title"] = firstNonEmpty(item.DisplayName, item.ID)
		if item.PayloadTemplateJSON != "" {
			caseBlock["payload"] = rawJSONObject(item.PayloadTemplateJSON)
		}
		next["case"] = caseBlock
	}
	if strings.TrimSpace(valueString(next["action"])) == "" {
		next["action"] = firstNonEmpty(node.Operation, node.ID)
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func mergePayloadTemplateIntoExecution(execution map[string]any, payloadJSON string) {
	payload := rawJSONObject(payloadJSON)
	if len(payload) == 0 {
		return
	}
	if query := mapFromReportAny(payload["query"]); len(query) > 0 {
		mergeReportMap(execution, "query", query)
	}
	if headers := mapFromReportAny(payload["headers"]); len(headers) > 0 {
		mergeReportMap(execution, "headers", headers)
	}
	if body, ok := payload["body"]; ok {
		if bodyMap := mapFromReportAny(body); len(bodyMap) > 0 {
			mergeReportMap(execution, "body", bodyMap)
		} else {
			execution["body"] = body
		}
		return
	}
	if _, hasStructuredKeys := payload["query"]; hasStructuredKeys {
		return
	}
	if strings.EqualFold(valueString(execution["method"]), "GET") {
		mergeReportMap(execution, "query", payload)
		return
	}
	mergeReportMap(execution, "body", payload)
}

func mergeExpectedConfigIntoExecution(execution map[string]any, expectedJSON string) {
	expected := rawJSONObject(expectedJSON)
	if len(expected) == 0 {
		return
	}
	if codes := intSliceFromReportAny(firstReportValue(expected, "expectedHttpCodes", "expected_http_codes")); len(codes) > 0 {
		values := make([]any, 0, len(codes))
		for _, code := range codes {
			values = append(values, code)
		}
		execution["expectedHttpCodes"] = values
	}
	for _, key := range []string{"requireRequestId", "require_request_id"} {
		if value, ok := expected[key].(bool); ok {
			execution["requireRequestId"] = value
			break
		}
	}
}

func executeInterfaceNodeCaseReport(ctx context.Context, bundle profile.Bundle, node profile.InterfaceNode, cases []profile.APICase, derived []profile.TemplateConfig, sourceStore *sqlite.Store, sourceStoreURL string, baseURL string, outputDir string, timeoutSeconds int) (interfaceNodeCaseReport, error) {
	started := time.Now()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return interfaceNodeCaseReport{}, err
	}
	runtime, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(outputDir, "runtime.sqlite")})
	if err != nil {
		return interfaceNodeCaseReport{}, err
	}
	defer runtime.Close()
	if err := runtime.ReplaceProfileCatalog(ctx, profilecatalog.FromBundle(bundle, time.Now().UTC())); err != nil {
		return interfaceNodeCaseReport{}, err
	}
	handler := controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: runtime})
	server := httptest.NewServer(handler)
	defer server.Close()
	caseIDs := make([]string, 0, len(cases))
	for _, item := range cases {
		caseIDs = append(caseIDs, item.ID)
	}
	requestPayload := map[string]any{"caseIds": caseIDs, "baseUrl": baseURL, "timeoutSeconds": timeoutSeconds}
	rawRequest, _ := json.Marshal(requestPayload)
	response, err := http.Post(server.URL+"/api/test-kit/run-batch", "application/json", strings.NewReader(string(rawRequest)))
	if err != nil {
		return interfaceNodeCaseReport{}, err
	}
	defer response.Body.Close()
	var rawBatch map[string]any
	if err := json.NewDecoder(response.Body).Decode(&rawBatch); err != nil {
		return interfaceNodeCaseReport{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return interfaceNodeCaseReport{}, fmt.Errorf("case batch failed with http status %d", response.StatusCode)
	}
	report := interfaceNodeCaseReport{
		OK:             boolFromReportAny(rawBatch["ok"]),
		ProfileID:      bundle.ID,
		NodeID:         node.ID,
		NodeName:       firstNonEmpty(node.DisplayName, node.ID),
		Operation:      node.Operation,
		Method:         node.Method,
		Path:           node.Path,
		ElapsedMs:      time.Since(started).Milliseconds(),
		GeneratedAt:    time.Now().UTC(),
		SourceStoreURL: sourceStoreURL,
		Counts: interfaceNodeCaseReportCounts{
			Total:          len(cases),
			DerivedConfigs: len(derived),
		},
	}
	report.Results = interfaceNodeCaseReportItems(rawBatch["results"])
	for _, item := range report.Results {
		if item.Status == store.StatusPassed {
			report.Counts.Passed++
		} else {
			report.Counts.Failed++
		}
	}
	report.OK = report.Counts.Total > 0 && report.Counts.Failed == 0
	if sourceStore == nil {
		report.Warnings = append(report.Warnings, "source Store was not available; report used profile bundle only")
	}
	if err := writeInterfaceNodeCaseReportFiles(outputDir, &report); err != nil {
		return interfaceNodeCaseReport{}, err
	}
	return report, nil
}

func interfaceNodeCaseReportItems(value any) []interfaceNodeCaseReportItem {
	values, _ := value.([]any)
	out := make([]interfaceNodeCaseReportItem, 0, len(values))
	for _, raw := range values {
		item := mapFromReportAny(raw)
		result := mapFromReportAny(item["result"])
		request := mapFromReportAny(result["request"])
		response := mapFromReportAny(result["response"])
		summary := mapFromReportAny(item["summary"])
		status := valueString(item["status"])
		if status == "" {
			status = store.StatusFailed
			if boolFromReportAny(item["ok"]) {
				status = store.StatusPassed
			}
		}
		out = append(out, interfaceNodeCaseReportItem{
			CaseID:      valueString(item["caseId"]),
			Title:       firstNonEmpty(valueString(item["title"]), valueString(item["caseId"])),
			RunID:       valueString(item["runId"]),
			CaseRunID:   valueString(item["caseRunId"]),
			ViewerURL:   valueString(item["viewerUrl"]),
			DetailURL:   valueString(item["detailUrl"]),
			Status:      status,
			HTTPCode:    firstPositiveInt(intFromReportAny(summary["httpCode"]), intFromReportAny(response["statusCode"])),
			ElapsedMs:   int64(intFromReportAny(item["elapsedMs"])),
			Method:      valueString(request["method"]),
			Path:        valueString(request["path"]),
			FullURL:     valueString(request["fullUrl"]),
			BaseURL:     firstNonEmpty(valueString(summary["targetBaseUrl"]), valueString(request["baseUrl"])),
			Error:       firstNonEmpty(valueString(item["error"]), valueString(summary["failureReason"])),
			BodyPreview: truncateReportText(valueString(response["body"]), 160),
		})
	}
	return out
}

func writeInterfaceNodeCaseReportFiles(outputDir string, report *interfaceNodeCaseReport) error {
	jsonPath := filepath.Join(outputDir, "report.json")
	htmlPath := filepath.Join(outputDir, "report.html")
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(htmlPath, []byte(renderInterfaceNodeCaseReportHTML(*report)), 0o644)
}

func renderInterfaceNodeCaseReportHTML(report interfaceNodeCaseReport) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>API Case Report</title><style>`)
	b.WriteString(`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#111827;background:#f8fafc}main{max-width:1280px;margin:auto}h1{font-size:24px;margin:0 0 4px}.meta{color:#4b5563;margin-bottom:16px}.summary{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.pill{border:1px solid #d1d5db;background:white;border-radius:6px;padding:6px 10px;font-size:13px}.ok{color:#047857}.bad{color:#b91c1c}table{width:100%;border-collapse:collapse;background:white;border:1px solid #d1d5db}th,td{border-bottom:1px solid #e5e7eb;text-align:left;vertical-align:top;padding:7px 8px;font-size:13px}th{background:#f3f4f6;color:#374151}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.wrap{word-break:break-all}.small{font-size:12px;color:#6b7280}`)
	b.WriteString(`</style></head><body><main>`)
	b.WriteString(`<h1>` + html.EscapeString(report.NodeName) + `</h1>`)
	b.WriteString(`<div class="meta">` + html.EscapeString(report.NodeID))
	if report.Operation != "" {
		b.WriteString(` · ` + html.EscapeString(report.Operation))
	}
	b.WriteString(`</div><div class="summary">`)
	b.WriteString(reportPill("status", statusText(report.OK)))
	b.WriteString(reportPill("total", strconv.Itoa(report.Counts.Total)))
	b.WriteString(reportPill("passed", strconv.Itoa(report.Counts.Passed)))
	b.WriteString(reportPill("failed", strconv.Itoa(report.Counts.Failed)))
	b.WriteString(reportPill("derived configs", strconv.Itoa(report.Counts.DerivedConfigs)))
	b.WriteString(reportPill("elapsed", fmt.Sprintf("%d ms", report.ElapsedMs)))
	b.WriteString(`</div><table><thead><tr><th>#</th><th>Case</th><th>Status</th><th>HTTP</th><th>Elapsed</th><th>Evidence</th><th>Request</th><th>Response</th><th>Error</th></tr></thead><tbody>`)
	for index, item := range report.Results {
		statusClass := "bad"
		if item.Status == store.StatusPassed {
			statusClass = "ok"
		}
		b.WriteString(`<tr><td class="mono">` + strconv.Itoa(index+1) + `</td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.Title) + `</div><div class="mono small wrap">` + html.EscapeString(item.CaseID) + `</div></td>`)
		b.WriteString(`<td class="` + statusClass + `">` + html.EscapeString(item.Status) + `</td>`)
		b.WriteString(`<td class="mono">` + strconv.Itoa(item.HTTPCode) + `</td>`)
		b.WriteString(`<td class="mono">` + fmt.Sprintf("%d ms", item.ElapsedMs) + `</td>`)
		b.WriteString(`<td class="mono wrap">`)
		if item.DetailURL != "" {
			b.WriteString(`<a href="` + html.EscapeString(item.DetailURL) + `">caseRunId</a><br>`)
		}
		b.WriteString(html.EscapeString(item.CaseRunID))
		b.WriteString(`</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(strings.TrimSpace(item.Method+" "+item.FullURL)) + `</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(item.BodyPreview) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(item.Error) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></main></body></html>`)
	return b.String()
}

func printInterfaceNodeCaseReport(report interfaceNodeCaseReport) {
	fmt.Printf("API Case Report: %s\n", report.NodeID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Passed: %d Failed: %d\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed)
	fmt.Printf("Derived Configs: %d\n", report.Counts.DerivedConfigs)
	fmt.Printf("Elapsed: %d ms\n", report.ElapsedMs)
	fmt.Printf("Report: %s\n", report.ReportURL)
}

func reportPill(label string, value string) string {
	return `<span class="pill"><span class="small">` + html.EscapeString(label) + `</span> ` + html.EscapeString(value) + `</span>`
}

func statusText(ok bool) string {
	if ok {
		return store.StatusPassed
	}
	return store.StatusFailed
}

func mapFromReportAny(value any) map[string]any {
	typed, _ := value.(map[string]any)
	if typed == nil {
		return map[string]any{}
	}
	return typed
}

func rawJSONObject(value string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(value) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(value), &out)
	return out
}

func cloneMap(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func mergeReportMap(target map[string]any, key string, values map[string]any) {
	next := mapFromReportAny(target[key])
	if len(next) == 0 {
		next = map[string]any{}
	}
	for itemKey, itemValue := range values {
		next[itemKey] = itemValue
	}
	target[key] = next
}

func firstReportValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func intSliceFromReportAny(value any) []int {
	switch typed := value.(type) {
	case []any:
		out := make([]int, 0, len(typed))
		for _, item := range typed {
			if number := intFromReportAny(item); number > 0 {
				out = append(out, number)
			}
		}
		return out
	case []int:
		return typed
	default:
		return nil
	}
}

func intFromReportAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		out, _ := typed.Int64()
		return int(out)
	default:
		return 0
	}
}

func valueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}

func boolFromReportAny(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func truncateReportText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	if limit <= 1 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func safeReportID(value string) string {
	var b strings.Builder
	for _, item := range value {
		if item >= 'a' && item <= 'z' || item >= 'A' && item <= 'Z' || item >= '0' && item <= '9' || item == '.' || item == '_' || item == '-' {
			b.WriteRune(item)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "item"
	}
	return b.String()
}

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
	case "report":
		return runWorkflowReport(context.Background(), args[1:])
	default:
		return fmt.Errorf("unknown workflow command: %s", args[0])
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
	flags := flag.NewFlagSet("workflow discover", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeURL := flags.String("store-url", filepath.Join(".runtime", "acceptance.sqlite"), "SQLite store URL or path")
	filter := flags.String("filter", "", "Filter by id, display name, or description")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, _, cleanup, err := loadInterfaceNodeReportBundle(ctx, *profilePath, *profileHome, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report := workflowList(bundle, *filter)
	if *jsonOutput {
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

func runWorkflowReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	workflowID := flags.String("workflow", "", "Workflow id")
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeURL := flags.String("store-url", filepath.Join(".runtime", "acceptance.sqlite"), "SQLite store URL or path")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	outputDir := flags.String("output-dir", "", "Report output directory")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workflowID) == "" {
		return errors.New("--workflow is required")
	}
	bundle, sourceStore, cleanup, err := loadInterfaceNodeReportBundle(ctx, *profilePath, *profileHome, *storeURL)
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

func executeWorkflowCaseReport(ctx context.Context, bundle profile.Bundle, sourceStore *sqlite.Store, workflowID string, outputDir string, baseURL string) (workflowCaseReport, error) {
	started := time.Now()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return workflowCaseReport{}, err
	}
	runtime, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(outputDir, "runtime.sqlite")})
	if err != nil {
		return workflowCaseReport{}, err
	}
	defer runtime.Close()
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
	bindingCaseIDs := workflowBindingCaseIDs(bundle.WorkflowBindings, workflowID)
	contextValues := map[string]any{}
	rawSteps, _ := workflow["steps"].([]any)
	steps := make([]map[string]any, 0, len(rawSteps))
	stepReports := make([]workflowCaseReportStep, 0, len(rawSteps))
	for _, rawStep := range rawSteps {
		step := mapFromReportAny(rawStep)
		caseID := runnableWorkflowCaseID(bundle.APICases, valueString(step["caseId"]), bindingCaseIDs[valueString(step["id"])])
		if caseID == "" {
			continue
		}
		timeoutSeconds := workflowStepTimeoutSeconds(workflow, step)
		payload := map[string]any{
			"caseId":         caseID,
			"workflowId":     workflowID,
			"stepId":         valueString(step["id"]),
			"overrides":      contextValues,
			"timeoutSeconds": timeoutSeconds,
			"baseUrl":        baseURL,
		}
		result, err := postReportMap(server.URL+"/api/test-kit/run", payload)
		if err != nil {
			return workflowCaseReport{}, err
		}
		result["stepId"] = valueString(step["id"])
		result["title"] = firstNonEmpty(valueString(step["displayName"]), valueString(step["id"]))
		result["stepOk"] = boolFromReportAny(result["ok"])
		steps = append(steps, result)
		stepReports = append(stepReports, workflowReportStepItem(step, result))
		for key, value := range workflowExportedValues(step, result) {
			contextValues[key] = value
		}
		if !boolFromReportAny(result["ok"]) {
			break
		}
	}
	report := workflowCaseReport{
		OK:           len(stepReports) == len(rawSteps),
		ProfileID:    bundle.ID,
		WorkflowID:   workflowID,
		WorkflowName: firstNonEmpty(valueString(workflow["displayName"]), workflowID),
		ElapsedMs:    time.Since(started).Milliseconds(),
		GeneratedAt:  time.Now().UTC(),
		Steps:        stepReports,
		Counts:       workflowCaseReportCounts{Total: len(rawSteps)},
	}
	for _, item := range stepReports {
		if item.Status == store.StatusPassed {
			report.Counts.Passed++
		} else {
			report.Counts.Failed++
			report.OK = false
		}
	}
	if len(stepReports) != len(rawSteps) {
		report.Counts.Failed += len(rawSteps) - len(stepReports)
		report.OK = false
	}
	snapshot := map[string]any{
		"workflowId": workflowID,
		"status":     statusText(report.OK),
		"ok":         report.OK,
		"elapsedMs":  report.ElapsedMs,
		"summary": map[string]any{
			"expectedStepCount": len(rawSteps),
			"stepCount":         len(stepReports),
			"passed":            report.Counts.Passed,
			"elapsedMs":         report.ElapsedMs,
		},
		"steps": steps,
	}
	if len(steps) > 0 {
		if saved, err := postReportMap(server.URL+"/api/workflow-runs", snapshot); err == nil {
			report.RunID = valueString(saved["workflowRunId"])
			if sourceStore != nil && report.RunID != "" {
				if run, runErr := runtime.GetRun(ctx, report.RunID); runErr == nil {
					_, _ = sourceStore.CreateRun(ctx, run)
				}
				if caseRuns, caseErr := runtime.ListAPICaseRuns(ctx, report.RunID); caseErr == nil {
					for _, item := range caseRuns {
						_, _ = sourceStore.RecordAPICaseRun(ctx, item)
					}
				}
			}
		}
	}
	if err := writeWorkflowCaseReportFiles(outputDir, &report); err != nil {
		return workflowCaseReport{}, err
	}
	return report, nil
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
	defer response.Body.Close()
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
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}
	result["httpStatus"] = response.StatusCode
	return result, nil
}

func findWorkflowByIDFromCatalog(catalog map[string]any, id string) (map[string]any, error) {
	rawWorkflows, _ := catalog["workflows"].([]any)
	id = strings.TrimSpace(id)
	for _, raw := range rawWorkflows {
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
	rawExports, _ := step["exports"].([]any)
	for _, rawExport := range rawExports {
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
	jsonPath := filepath.Join(outputDir, "report.json")
	htmlPath := filepath.Join(outputDir, "report.html")
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(htmlPath, []byte(renderWorkflowCaseReportHTML(*report)), 0o644)
}

func renderWorkflowCaseReportHTML(report workflowCaseReport) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Workflow Report</title><style>`)
	b.WriteString(`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#111827;background:#f8fafc}main{max-width:1280px;margin:auto}h1{font-size:24px;margin:0 0 4px}.meta{color:#4b5563;margin-bottom:16px}.summary{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.pill{border:1px solid #d1d5db;background:white;border-radius:6px;padding:6px 10px;font-size:13px}.ok{color:#047857}.bad{color:#b91c1c}table{width:100%;border-collapse:collapse;background:white;border:1px solid #d1d5db}th,td{border-bottom:1px solid #e5e7eb;text-align:left;vertical-align:top;padding:7px 8px;font-size:13px}th{background:#f3f4f6;color:#374151}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.wrap{word-break:break-all}.small{font-size:12px;color:#6b7280}`)
	b.WriteString(`</style></head><body><main>`)
	b.WriteString(`<h1>` + html.EscapeString(report.WorkflowName) + `</h1>`)
	b.WriteString(`<div class="meta">` + html.EscapeString(report.WorkflowID))
	if report.RunID != "" {
		b.WriteString(` · ` + html.EscapeString(report.RunID))
	}
	b.WriteString(`</div><div class="summary">`)
	b.WriteString(reportPill("status", statusText(report.OK)))
	b.WriteString(reportPill("steps", strconv.Itoa(report.Counts.Total)))
	b.WriteString(reportPill("passed", strconv.Itoa(report.Counts.Passed)))
	b.WriteString(reportPill("failed", strconv.Itoa(report.Counts.Failed)))
	b.WriteString(reportPill("elapsed", fmt.Sprintf("%d ms", report.ElapsedMs)))
	b.WriteString(`</div><table><thead><tr><th>#</th><th>Step</th><th>Case</th><th>Status</th><th>HTTP</th><th>Elapsed</th><th>Evidence</th><th>Request</th><th>Error</th></tr></thead><tbody>`)
	for index, item := range report.Steps {
		statusClass := "bad"
		if item.Status == store.StatusPassed {
			statusClass = "ok"
		}
		b.WriteString(`<tr><td class="mono">` + strconv.Itoa(index+1) + `</td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.Title) + `</div><div class="mono small wrap">` + html.EscapeString(item.StepID) + `</div></td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(item.CaseID) + `</td>`)
		b.WriteString(`<td class="` + statusClass + `">` + html.EscapeString(item.Status) + `</td>`)
		b.WriteString(`<td class="mono">` + strconv.Itoa(item.HTTPCode) + `</td>`)
		b.WriteString(`<td class="mono">` + fmt.Sprintf("%d ms", item.ElapsedMs) + `</td>`)
		b.WriteString(`<td class="mono wrap">`)
		if item.DetailURL != "" {
			b.WriteString(`<a href="` + html.EscapeString(item.DetailURL) + `">caseRunId</a><br>`)
		}
		b.WriteString(html.EscapeString(item.CaseRunID))
		b.WriteString(`</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(strings.TrimSpace(item.Method+" "+item.FullURL)) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(item.Error) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></main></body></html>`)
	return b.String()
}

func runWorkflowAudit(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow audit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	workflowID := flags.String("workflow", "", "Workflow id")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}

	options := workflowaudit.Options{
		Bundle:     bundle,
		WorkflowID: *workflowID,
	}
	if strings.TrimSpace(*storeURL) != "" {
		s, err := openStore(ctx, *storeURL)
		if err != nil {
			return err
		}
		defer s.Close()
		options.Store = s
	}

	report, err := workflowaudit.Audit(ctx, options)
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
	case "discover":
		return runCaseDiscover(ctx, args[1:])
	case "suite":
		return runCaseSuite(ctx, args[1:])
	case "run":
		return runCaseRun(ctx, args[1:])
	case "incomplete-batches":
		return runCaseIncompleteBatches(ctx, args[1:])
	default:
		return fmt.Errorf("unknown case command: %s", args[0])
	}
}

func runCaseSuite(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing case suite command")
	}
	switch args[0] {
	case "report":
		return runCaseSuiteReport(ctx, args[1:])
	default:
		return fmt.Errorf("unknown case suite command: %s", args[0])
	}
}

type caseListReport struct {
	OK        bool           `json:"ok"`
	ProfileID string         `json:"profileId"`
	Count     int            `json:"count"`
	Filters   caseListFilter `json:"filters"`
	Items     []caseListItem `json:"items"`
}

type caseListFilter struct {
	Filter   string   `json:"filter,omitempty"`
	NodeID   string   `json:"nodeId,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Status   string   `json:"status,omitempty"`
	Owner    string   `json:"owner,omitempty"`
	Priority string   `json:"priority,omitempty"`
}

type caseListItem struct {
	ID                   string   `json:"id"`
	DisplayName          string   `json:"displayName,omitempty"`
	Description          string   `json:"description,omitempty"`
	NodeID               string   `json:"nodeId,omitempty"`
	CaseType             string   `json:"caseType,omitempty"`
	Scenario             string   `json:"scenario,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	Priority             string   `json:"priority,omitempty"`
	Owner                string   `json:"owner,omitempty"`
	Status               string   `json:"status,omitempty"`
	RequiredForAdmission bool     `json:"requiredForAdmission"`
	SortOrder            int      `json:"sortOrder,omitempty"`
	HasRunnableFile      bool     `json:"hasRunnableFile"`
	HasExecutionConfig   bool     `json:"hasExecutionConfig"`
}

func runCaseDiscover(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case discover", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeURL := flags.String("store-url", filepath.Join(".runtime", "acceptance.sqlite"), "SQLite store URL or path")
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, sourceStore, cleanup, err := loadInterfaceNodeReportBundle(ctx, *profilePath, *profileHome, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report := caseList(ctx, bundle, sourceStore, caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	})
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	for _, item := range report.Items {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.DisplayName, item.NodeID, item.Status, item.Priority, strings.Join(item.Tags, ","))
	}
	return nil
}

type caseSuiteReport struct {
	OK             bool                          `json:"ok"`
	ProfileID      string                        `json:"profileId"`
	Title          string                        `json:"title"`
	ReportURL      string                        `json:"reportUrl"`
	JSONReportURL  string                        `json:"jsonReportUrl"`
	ElapsedMs      int64                         `json:"elapsedMs"`
	GeneratedAt    time.Time                     `json:"generatedAt"`
	Filters        caseListFilter                `json:"filters"`
	Counts         interfaceNodeCaseReportCounts `json:"counts"`
	Results        []caseSuiteReportItem         `json:"results"`
	Warnings       []string                      `json:"warnings,omitempty"`
	SourceStoreURL string                        `json:"sourceStoreUrl,omitempty"`
}

type caseSuiteReportItem struct {
	CaseID      string   `json:"caseId"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	NodeID      string   `json:"nodeId,omitempty"`
	NodeName    string   `json:"nodeName,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	RunID       string   `json:"runId,omitempty"`
	CaseRunID   string   `json:"caseRunId,omitempty"`
	ViewerURL   string   `json:"viewerUrl,omitempty"`
	DetailURL   string   `json:"detailUrl,omitempty"`
	Status      string   `json:"status"`
	HTTPCode    int      `json:"httpCode,omitempty"`
	ElapsedMs   int64    `json:"elapsedMs"`
	Method      string   `json:"method,omitempty"`
	Path        string   `json:"path,omitempty"`
	FullURL     string   `json:"fullUrl,omitempty"`
	BaseURL     string   `json:"baseUrl,omitempty"`
	Error       string   `json:"error,omitempty"`
	BodyPreview string   `json:"bodyPreview,omitempty"`
}

func runCaseSuiteReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeURL := flags.String("store-url", filepath.Join(".runtime", "acceptance.sqlite"), "SQLite store URL or path")
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	outputDir := flags.String("output-dir", "", "Report output directory")
	timeoutSeconds := flags.Int("timeout-seconds", 3, "Timeout per API Case")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *timeoutSeconds <= 0 {
		return errors.New("--timeout-seconds must be greater than zero")
	}
	bundle, sourceStore, cleanup, err := loadInterfaceNodeReportBundle(ctx, *profilePath, *profileHome, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filters := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filters)
	if len(cases) == 0 {
		return errors.New("no API cases matched selector")
	}
	derived := deriveCaseSuiteConfigs(bundle, cases)
	bundle.TemplateConfigs = mergeTemplateConfigs(bundle.TemplateConfigs, derived)
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "case-suite."+safeReportID(caseSuiteFilterSlug(filters))+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeCaseSuiteReport(ctx, bundle, cases, derived, sourceStore, *storeURL, filters, *baseURL, absOutputDir, *timeoutSeconds)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteReport(report)
	return nil
}

func selectedCaseSuiteCases(bundle profile.Bundle, filters caseListFilter) []profile.APICase {
	out := make([]profile.APICase, 0)
	for _, item := range bundle.APICases {
		if matchesCaseFilters(item, filters) {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].NodeID != out[j].NodeID {
			return out[i].NodeID < out[j].NodeID
		}
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func deriveCaseSuiteConfigs(bundle profile.Bundle, cases []profile.APICase) []profile.TemplateConfig {
	nodesByID := make(map[string]profile.InterfaceNode, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		nodesByID[node.ID] = node
	}
	casesByNode := map[string][]profile.APICase{}
	for _, item := range cases {
		casesByNode[item.NodeID] = append(casesByNode[item.NodeID], item)
	}
	nodeIDs := make([]string, 0, len(casesByNode))
	for nodeID := range casesByNode {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	out := make([]profile.TemplateConfig, 0)
	selected := map[string]bool{}
	for _, item := range cases {
		selected[item.ID] = true
	}
	for _, nodeID := range nodeIDs {
		node, ok := nodesByID[nodeID]
		if !ok {
			continue
		}
		for _, config := range deriveInterfaceNodeCaseConfigs(bundle, node, interfaceNodeReportCases(bundle.APICases, nodeID)) {
			if selected[config.ScopeID] {
				out = append(out, config)
			}
		}
	}
	return out
}

func executeCaseSuiteReport(ctx context.Context, bundle profile.Bundle, cases []profile.APICase, derived []profile.TemplateConfig, sourceStore *sqlite.Store, sourceStoreURL string, filters caseListFilter, baseURL string, outputDir string, timeoutSeconds int) (caseSuiteReport, error) {
	started := time.Now()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return caseSuiteReport{}, err
	}
	runtime, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(outputDir, "runtime.sqlite")})
	if err != nil {
		return caseSuiteReport{}, err
	}
	defer runtime.Close()
	if err := runtime.ReplaceProfileCatalog(ctx, profilecatalog.FromBundle(bundle, time.Now().UTC())); err != nil {
		return caseSuiteReport{}, err
	}
	handler := controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: runtime})
	server := httptest.NewServer(handler)
	defer server.Close()
	caseIDs := make([]string, 0, len(cases))
	for _, item := range cases {
		caseIDs = append(caseIDs, item.ID)
	}
	requestPayload := map[string]any{"caseIds": caseIDs, "baseUrl": baseURL, "timeoutSeconds": timeoutSeconds}
	rawRequest, _ := json.Marshal(requestPayload)
	response, err := http.Post(server.URL+"/api/test-kit/run-batch", "application/json", strings.NewReader(string(rawRequest)))
	if err != nil {
		return caseSuiteReport{}, err
	}
	defer response.Body.Close()
	var rawBatch map[string]any
	if err := json.NewDecoder(response.Body).Decode(&rawBatch); err != nil {
		return caseSuiteReport{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return caseSuiteReport{}, fmt.Errorf("case suite batch failed with http status %d", response.StatusCode)
	}
	report := caseSuiteReport{
		OK:             boolFromReportAny(rawBatch["ok"]),
		ProfileID:      bundle.ID,
		Title:          "Case Suite Report",
		ElapsedMs:      time.Since(started).Milliseconds(),
		GeneratedAt:    time.Now().UTC(),
		Filters:        normalizeCaseListFilter(filters),
		SourceStoreURL: sourceStoreURL,
		Counts: interfaceNodeCaseReportCounts{
			Total:          len(cases),
			DerivedConfigs: len(derived),
		},
	}
	report.Results = caseSuiteReportItems(interfaceNodeCaseReportItems(rawBatch["results"]), cases, bundle.InterfaceNodes)
	for _, item := range report.Results {
		if item.Status == store.StatusPassed {
			report.Counts.Passed++
		} else {
			report.Counts.Failed++
		}
	}
	report.OK = report.Counts.Total > 0 && report.Counts.Failed == 0
	if sourceStore == nil {
		report.Warnings = append(report.Warnings, "source Store was not available; report used profile bundle only")
	}
	if err := writeCaseSuiteReportFiles(outputDir, &report); err != nil {
		return caseSuiteReport{}, err
	}
	return report, nil
}

func caseSuiteReportItems(results []interfaceNodeCaseReportItem, cases []profile.APICase, nodes []profile.InterfaceNode) []caseSuiteReportItem {
	casesByID := make(map[string]profile.APICase, len(cases))
	for _, item := range cases {
		casesByID[item.ID] = item
	}
	nodesByID := make(map[string]profile.InterfaceNode, len(nodes))
	for _, node := range nodes {
		nodesByID[node.ID] = node
	}
	out := make([]caseSuiteReportItem, 0, len(results))
	for _, result := range results {
		apiCase := casesByID[result.CaseID]
		node := nodesByID[apiCase.NodeID]
		out = append(out, caseSuiteReportItem{
			CaseID:      result.CaseID,
			Title:       result.Title,
			Description: apiCase.Description,
			NodeID:      apiCase.NodeID,
			NodeName:    firstNonEmpty(node.DisplayName, apiCase.NodeID),
			Tags:        append([]string(nil), apiCase.Tags...),
			Priority:    apiCase.Priority,
			Owner:       apiCase.Owner,
			RunID:       result.RunID,
			CaseRunID:   result.CaseRunID,
			ViewerURL:   result.ViewerURL,
			DetailURL:   result.DetailURL,
			Status:      result.Status,
			HTTPCode:    result.HTTPCode,
			ElapsedMs:   result.ElapsedMs,
			Method:      result.Method,
			Path:        result.Path,
			FullURL:     result.FullURL,
			BaseURL:     result.BaseURL,
			Error:       result.Error,
			BodyPreview: result.BodyPreview,
		})
	}
	return out
}

func writeCaseSuiteReportFiles(outputDir string, report *caseSuiteReport) error {
	jsonPath := filepath.Join(outputDir, "report.json")
	htmlPath := filepath.Join(outputDir, "report.html")
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(htmlPath, []byte(renderCaseSuiteReportHTML(*report)), 0o644)
}

func renderCaseSuiteReportHTML(report caseSuiteReport) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Case Suite Report</title><style>`)
	b.WriteString(`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#111827;background:#f8fafc}main{max-width:1320px;margin:auto}h1{font-size:24px;margin:0 0 4px}.meta{color:#4b5563;margin-bottom:16px}.summary{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.pill{border:1px solid #d1d5db;background:white;border-radius:6px;padding:6px 10px;font-size:13px}.ok{color:#047857}.bad{color:#b91c1c}table{width:100%;border-collapse:collapse;background:white;border:1px solid #d1d5db}th,td{border-bottom:1px solid #e5e7eb;text-align:left;vertical-align:top;padding:7px 8px;font-size:13px}th{background:#f3f4f6;color:#374151}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.wrap{word-break:break-all}.small{font-size:12px;color:#6b7280}`)
	b.WriteString(`</style></head><body><main>`)
	b.WriteString(`<h1>Case Suite Report</h1>`)
	b.WriteString(`<div class="meta">` + html.EscapeString(report.ProfileID) + `</div><div class="summary">`)
	b.WriteString(reportPill("status", statusText(report.OK)))
	b.WriteString(reportPill("total", strconv.Itoa(report.Counts.Total)))
	b.WriteString(reportPill("passed", strconv.Itoa(report.Counts.Passed)))
	b.WriteString(reportPill("failed", strconv.Itoa(report.Counts.Failed)))
	b.WriteString(reportPill("derived configs", strconv.Itoa(report.Counts.DerivedConfigs)))
	b.WriteString(reportPill("elapsed", fmt.Sprintf("%d ms", report.ElapsedMs)))
	if len(report.Filters.Tags) > 0 {
		b.WriteString(reportPill("tags", strings.Join(report.Filters.Tags, ",")))
	}
	if report.Filters.Owner != "" {
		b.WriteString(reportPill("owner", report.Filters.Owner))
	}
	if report.Filters.Priority != "" {
		b.WriteString(reportPill("priority", report.Filters.Priority))
	}
	b.WriteString(`</div><table><thead><tr><th>#</th><th>Case</th><th>Node</th><th>Maintainer</th><th>Status</th><th>HTTP</th><th>Elapsed</th><th>Evidence</th><th>Request</th><th>Response</th><th>Error</th></tr></thead><tbody>`)
	for index, item := range report.Results {
		statusClass := "bad"
		if item.Status == store.StatusPassed {
			statusClass = "ok"
		}
		b.WriteString(`<tr><td class="mono">` + strconv.Itoa(index+1) + `</td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.Title) + `</div><div class="mono small wrap">` + html.EscapeString(item.CaseID) + `</div>`)
		if item.Description != "" {
			b.WriteString(`<div class="small">` + html.EscapeString(item.Description) + `</div>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.NodeName) + `</div><div class="mono small wrap">` + html.EscapeString(item.NodeID) + `</div></td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.Owner) + `</div><div class="small">` + html.EscapeString(item.Priority) + `</div><div class="small">` + html.EscapeString(strings.Join(item.Tags, ", ")) + `</div></td>`)
		b.WriteString(`<td class="` + statusClass + `">` + html.EscapeString(item.Status) + `</td>`)
		b.WriteString(`<td class="mono">` + strconv.Itoa(item.HTTPCode) + `</td>`)
		b.WriteString(`<td class="mono">` + fmt.Sprintf("%d ms", item.ElapsedMs) + `</td>`)
		b.WriteString(`<td class="mono wrap">`)
		if item.DetailURL != "" {
			b.WriteString(`<a href="` + html.EscapeString(item.DetailURL) + `">caseRunId</a><br>`)
		}
		b.WriteString(html.EscapeString(item.CaseRunID))
		b.WriteString(`</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(strings.TrimSpace(item.Method+" "+item.FullURL)) + `</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(item.BodyPreview) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(item.Error) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></main></body></html>`)
	return b.String()
}

func printCaseSuiteReport(report caseSuiteReport) {
	fmt.Println("Case Suite Report")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Passed: %d Failed: %d\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed)
	fmt.Printf("Derived Configs: %d\n", report.Counts.DerivedConfigs)
	fmt.Printf("Elapsed: %d ms\n", report.ElapsedMs)
	fmt.Printf("Report: %s\n", report.ReportURL)
}

func caseSuiteFilterSlug(filters caseListFilter) string {
	filters = normalizeCaseListFilter(filters)
	parts := []string{filters.Filter, filters.NodeID, filters.Status, filters.Owner, filters.Priority}
	parts = append(parts, filters.Tags...)
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			return part
		}
	}
	return "all"
}

func caseList(ctx context.Context, bundle profile.Bundle, runtime store.Store, filters caseListFilter) caseListReport {
	cases := append([]profile.APICase(nil), bundle.APICases...)
	sort.SliceStable(cases, func(i, j int) bool {
		if cases[i].NodeID != cases[j].NodeID {
			return cases[i].NodeID < cases[j].NodeID
		}
		if cases[i].SortOrder != cases[j].SortOrder {
			return cases[i].SortOrder < cases[j].SortOrder
		}
		return cases[i].ID < cases[j].ID
	})
	executionConfigs := caseExecutionConfigSet(ctx, runtime)
	report := caseListReport{OK: true, ProfileID: bundle.ID, Filters: normalizeCaseListFilter(filters)}
	for _, item := range cases {
		if !matchesCaseFilters(item, filters) {
			continue
		}
		report.Items = append(report.Items, caseListItem{
			ID:                   item.ID,
			DisplayName:          item.DisplayName,
			Description:          item.Description,
			NodeID:               item.NodeID,
			CaseType:             item.CaseType,
			Scenario:             item.Scenario,
			Tags:                 append([]string(nil), item.Tags...),
			Priority:             item.Priority,
			Owner:                item.Owner,
			Status:               effectiveCaseStatus(item),
			RequiredForAdmission: item.RequiredForAdmission,
			SortOrder:            item.SortOrder,
			HasRunnableFile:      strings.TrimSpace(item.CasePath) != "",
			HasExecutionConfig:   executionConfigs[item.ID],
		})
	}
	report.Count = len(report.Items)
	return report
}

func normalizeCaseListFilter(filters caseListFilter) caseListFilter {
	filters.Filter = strings.TrimSpace(filters.Filter)
	filters.NodeID = strings.TrimSpace(filters.NodeID)
	filters.Status = strings.TrimSpace(filters.Status)
	filters.Owner = strings.TrimSpace(filters.Owner)
	filters.Priority = strings.TrimSpace(filters.Priority)
	filters.Tags = normalizeStringList(filters.Tags)
	return filters
}

func matchesCaseFilters(item profile.APICase, filters caseListFilter) bool {
	filters = normalizeCaseListFilter(filters)
	if filters.NodeID != "" && item.NodeID != filters.NodeID {
		return false
	}
	if filters.Status != "" && !strings.EqualFold(effectiveCaseStatus(item), filters.Status) {
		return false
	}
	if filters.Owner != "" && !strings.EqualFold(strings.TrimSpace(item.Owner), filters.Owner) {
		return false
	}
	if filters.Priority != "" && !strings.EqualFold(strings.TrimSpace(item.Priority), filters.Priority) {
		return false
	}
	if len(filters.Tags) > 0 && !caseHasAllTags(item.Tags, filters.Tags) {
		return false
	}
	return matchesDiscoveryFilter(filters.Filter, item.ID, item.DisplayName, item.Scenario, item.Description, item.Owner, item.Priority, strings.Join(item.Tags, " "))
}

func effectiveCaseStatus(item profile.APICase) string {
	status := strings.TrimSpace(item.Status)
	if status == "" {
		return "active"
	}
	return status
}

func caseHasAllTags(actual []string, required []string) bool {
	actualSet := map[string]bool{}
	for _, tag := range actual {
		normalized := normalizedDiscoveryText(tag)
		if normalized != "" {
			actualSet[normalized] = true
		}
	}
	for _, tag := range required {
		normalized := normalizedDiscoveryText(tag)
		if normalized != "" && !actualSet[normalized] {
			return false
		}
	}
	return true
}

func caseExecutionConfigSet(ctx context.Context, runtime store.Store) map[string]bool {
	out := map[string]bool{}
	if runtime == nil {
		return out
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return out
	}
	for _, config := range catalog.TemplateConfigs {
		if config.ScopeType == "case" && strings.TrimSpace(config.ScopeID) != "" {
			out[strings.TrimSpace(config.ScopeID)] = true
			continue
		}
		var payload struct {
			CaseID string `json:"caseId"`
		}
		if json.Unmarshal([]byte(config.ConfigJSON), &payload) == nil && strings.TrimSpace(payload.CaseID) != "" {
			out[strings.TrimSpace(payload.CaseID)] = true
		}
	}
	return out
}

func runCaseIncompleteBatches(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case incomplete-batches", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	jsonOutput := flags.Bool("json", false, "Print JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}
	s, err := openStore(ctx, *storeURL)
	if err != nil {
		return err
	}
	defer s.Close()

	report, err := incompleteCaseReportForStore(ctx, bundle, s)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printIncompleteCaseReport(report)
	return nil
}

type incompleteCaseReport struct {
	OK       bool                 `json:"ok"`
	Count    int                  `json:"count"`
	Items    []incompleteCaseItem `json:"items"`
	Warnings []string             `json:"warnings"`
}

type incompleteCaseItem struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Reason           string `json:"reason"`
	Source           string `json:"source"`
	Message          string `json:"message"`
	SuggestedCommand string `json:"suggestedCommand"`
}

func incompleteCaseReportForStore(ctx context.Context, bundle profile.Bundle, s store.Store) (incompleteCaseReport, error) {
	passed, latest, err := apiCaseRunStatusByCase(ctx, s)
	if err != nil {
		return incompleteCaseReport{}, err
	}
	items := make([]incompleteCaseItem, 0)
	for _, item := range bundle.APICases {
		if strings.TrimSpace(item.ID) == "" || passed[item.ID] {
			continue
		}
		reason := "not-run"
		if status := latest[item.ID]; status != "" {
			reason = "latest-" + status
		}
		items = append(items, incompleteCaseItem{
			ID:               item.ID,
			Title:            firstNonEmpty(item.DisplayName, item.ID),
			Reason:           reason,
			Source:           "profile:" + bundle.ID,
			Message:          "no passed Store run found for this API Case",
			SuggestedCommand: apiCaseSuggestedCommand(item),
		})
	}
	return incompleteCaseReport{
		OK:       true,
		Count:    len(items),
		Items:    items,
		Warnings: []string{},
	}, nil
}

func apiCaseRunStatusByCase(ctx context.Context, s store.Store) (map[string]bool, map[string]string, error) {
	runs, err := s.ListRuns(ctx)
	if err != nil {
		return nil, nil, err
	}
	passed := map[string]bool{}
	latest := map[string]string{}
	for i := len(runs) - 1; i >= 0; i-- {
		caseRuns, err := s.ListAPICaseRuns(ctx, runs[i].ID)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range caseRuns {
			if latest[item.CaseID] == "" {
				latest[item.CaseID] = item.Status
			}
			if strings.EqualFold(item.Status, store.StatusPassed) {
				passed[item.CaseID] = true
			}
		}
	}
	return passed, latest, nil
}

func apiCaseSuggestedCommand(item profile.APICase) string {
	casePath := strings.TrimSpace(item.CasePath)
	if casePath == "" {
		return ""
	}
	parts := []string{"otsandbox case run --case " + strconv.Quote(casePath)}
	if strings.TrimSpace(item.BaseURL) != "" {
		parts = append(parts, "--base-url "+strconv.Quote(item.BaseURL))
	}
	if strings.TrimSpace(item.EvidenceDir) != "" {
		parts = append(parts, "--evidence-dir "+strconv.Quote(item.EvidenceDir))
	}
	return strings.Join(parts, " ")
}

func printIncompleteCaseReport(report incompleteCaseReport) {
	fmt.Printf("Incomplete API Cases: %d\n", report.Count)
	for _, item := range report.Items {
		fmt.Printf("- %s [%s]\n", item.ID, item.Reason)
		if item.SuggestedCommand != "" {
			fmt.Printf("  %s\n", item.SuggestedCommand)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func runCaseRun(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	overrides := mapFlag{}
	casePath := flags.String("case", "", "API case file path")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	evidenceDir := flags.String("evidence-dir", filepath.Join(".runtime", "cases"), "Evidence output directory")
	runID := flags.String("run-id", "", "Run id")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	profileID := flags.String("profile", "default", "Profile id for store records")
	flags.Var(&overrides, "override", "Request body override as key=value; repeat for multiple values")
	if err := flags.Parse(args); err != nil {
		return err
	}
	result, err := apicase.Run(ctx, apicase.RunOptions{
		CasePath:    *casePath,
		BaseURL:     *baseURL,
		EvidenceDir: *evidenceDir,
		RunID:       *runID,
		Overrides:   overrides.Values(),
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

type mapFlag map[string]any

func (m *mapFlag) String() string {
	if m == nil || len(*m) == 0 {
		return ""
	}
	raw, _ := json.Marshal(*m)
	return string(raw)
}

func (m *mapFlag) Set(value string) error {
	key, parsed, ok := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return fmt.Errorf("override must use key=value")
	}
	if *m == nil {
		*m = map[string]any{}
	}
	(*m)[key] = parsed
	return nil
}

func (m mapFlag) Values() map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for key, value := range m {
		out[key] = value
	}
	return out
}

type stringListFlag []string

func (s *stringListFlag) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	*s = append(*s, value)
	return nil
}

func (s stringListFlag) Values() []string {
	return normalizeStringList([]string(s))
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
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
	startedAt := runResultTime(result.StartedAt, now)
	finishedAt := runResultTime(result.FinishedAt, now)
	if finishedAt.Before(startedAt) {
		finishedAt = startedAt
	}
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
		SummaryJSON:  caseRunSummaryJSON(result),
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		CreatedAt:    startedAt,
		UpdatedAt:    finishedAt,
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
		StartedAt:            startedAt,
		FinishedAt:           finishedAt,
		CreatedAt:            startedAt,
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

func caseRunSummaryJSON(result apicase.RunResult) string {
	path := filepath.Join(result.EvidencePath, "summary.json")
	if raw, err := os.ReadFile(path); err == nil && json.Valid(raw) {
		return strings.TrimSpace(string(raw))
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func runResultTime(value string, fallback time.Time) time.Time {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fallback
	}
	return parsed.UTC()
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
	profilePath     string
	profileHome     string
	host            string
	port            int
	storeURL        string
	traceGraphQLURL string
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
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	host := flags.String("host", "127.0.0.1", "HTTP host")
	port := flags.Int("port", 18191, "HTTP port")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	traceGraphQLURL := flags.String("trace-graphql-url", os.Getenv("OTS_TRACE_GRAPHQL_URL"), "Trace provider GraphQL URL")
	if err := flags.Parse(args); err != nil {
		return serveConfig{}, err
	}
	return serveConfig{profilePath: *profilePath, profileHome: *profileHome, host: *host, port: *port, storeURL: *storeURL, traceGraphQLURL: *traceGraphQLURL}, nil
}

func serveHandler(cfg serveConfig) (http.Handler, func() error, error) {
	storeCfg, err := sqlite.ParseConfigFromURL(cfg.storeURL)
	if err != nil {
		return nil, nil, err
	}
	storeCfg = storeCfg.Resolve()
	runtime, err := sqlite.Open(context.Background(), storeCfg)
	if err != nil {
		return nil, nil, err
	}
	ctx := context.Background()
	if strings.TrimSpace(cfg.profilePath) != "" {
		profilePath, err := resolveProfileReference(cfg.profilePath, cfg.profileHome)
		if err != nil {
			_ = runtime.Close()
			return nil, nil, err
		}
		if _, err := publishProfileBundleToStore(ctx, runtime, profilePath, storeCfg.Path, false, false); err != nil {
			_ = runtime.Close()
			return nil, nil, err
		}
	}
	bundle, err := serveBundle(ctx, runtime)
	if err != nil {
		_ = runtime.Close()
		return nil, nil, err
	}
	return controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: runtime, TraceGraphQLURL: cfg.traceGraphQLURL, ProfileHome: cfg.profileHome}), runtime.Close, nil
}

func serveBundle(ctx context.Context, runtime store.Store) (profile.Bundle, error) {
	if runtime != nil {
		if catalogIndex, err := runtime.GetProfileCatalogIndex(ctx); err == nil && strings.TrimSpace(catalogIndex.ProfileID) != "" {
			if profileIndex, err := runtime.GetProfileIndex(ctx, catalogIndex.ProfileID); err == nil && strings.TrimSpace(profileIndex.BundlePath) != "" {
				if bundle, err := profile.Load(profileIndex.BundlePath); err == nil {
					return bundle, nil
				}
			}
		}
		catalog, err := runtime.GetProfileCatalog(ctx)
		if err == nil && catalog.ProfileID != "" {
			return profilecatalog.ToBundle(catalog), nil
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return profile.Bundle{}, err
		}
	}
	return profile.EmptyBundle(), nil
}
