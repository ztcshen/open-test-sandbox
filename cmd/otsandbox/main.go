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
  otsandbox case run --case PATH [--base-url URL] [--dry-run] [--evidence-dir PATH]
  otsandbox serve [--profile PATH] [--host HOST] [--port PORT]
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
	cfg := sqlite.ConfigFromURL(*storeURL)

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
	s, err := sqlite.Open(ctx, sqlite.ConfigFromURL(*storeURL))
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

func runEvidence(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing evidence command")
	}
	switch args[0] {
	case "import":
		return runEvidenceImport(ctx, args[1:])
	default:
		return fmt.Errorf("unknown evidence command: %s", args[0])
	}
}

func runEvidenceImport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("evidence import", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	from := flags.String("from", "", "Source runtime SQLite path")
	profileID := flags.String("profile", "", "Profile id")
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg := sqlite.ConfigFromURL(*storeURL)
	result, err := evidence.ImportLegacyRuntimeSQLite(ctx, evidence.SQLiteImportOptions{
		SourcePath: *from,
		ProfileID:  *profileID,
		TargetPath: cfg.Path,
	})
	if err != nil {
		return err
	}
	fmt.Println("Imported evidence index")
	fmt.Printf("Runs: %d\n", result.RunCount)
	fmt.Printf("API Case Runs: %d\n", result.APICaseRunCount)
	fmt.Printf("Evidence Records: %d\n", result.EvidenceCount)
	return nil
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
	s, err := sqlite.Open(ctx, sqlite.ConfigFromURL(storeURL))
	if err != nil {
		return err
	}
	defer s.Close()

	now := time.Now().UTC()
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
		RequestSummaryJSON:   "{}",
		AssertionSummaryJSON: "{}",
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
		if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
			ID:        result.RunID + "." + name,
			RunID:     result.RunID,
			CaseRunID: result.RunID + ".case",
			Kind:      strings.TrimSuffix(name, ".json"),
			URI:       path,
			MediaType: "application/json",
			CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func runServe(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "profiles/empty", "Profile bundle path")
	host := flags.String("host", "127.0.0.1", "HTTP host")
	port := flags.Int("port", 18191, "HTTP port")
	if err := flags.Parse(args); err != nil {
		return err
	}

	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}
	addr := *host + ":" + strconv.Itoa(*port)
	fmt.Printf("Open Test Sandbox listening on http://%s\n", addr)
	return http.ListenAndServe(addr, controlplane.New(bundle))
}
