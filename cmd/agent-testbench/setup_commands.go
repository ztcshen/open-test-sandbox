package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type setupCommandReport struct {
	OK      bool                `json:"ok"`
	Repo    string              `json:"repo"`
	Store   setupStoreReport    `json:"store"`
	Runtime setupRuntimeReport  `json:"runtime"`
	Steps   []updateCommandStep `json:"steps,omitempty"`
	Next    []string            `json:"next"`
}

type setupStoreReport struct {
	Name    string `json:"name"`
	Backend string `json:"backend"`
	URL     string `json:"url"`
	Active  bool   `json:"active"`
}

type setupRuntimeReport struct {
	Path  string `json:"path"`
	Built bool   `json:"built"`
}

func runSetup(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	repoFlag := flags.String("repo", "", "AgentTestBench git checkout to configure")
	storeName := flags.String("store", "local", "Local Store config name to create or update")
	storeURL := flags.String("url", "", "PostgreSQL, MySQL, or SQLite Store DSN")
	sqlitePath := flags.String("sqlite", "", "SQLite Store path; defaults to REPO/.runtime/agent-testbench-local.sqlite")
	buildRuntime := flags.Bool("build-runtime", false, "Build the local runtime binary into REPO/.runtime/bin")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable setup report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected setup arguments: %s", strings.Join(flags.Args(), " "))
	}
	report, err := setupLocalRuntime(ctx, setupOptions{
		Repo:         *repoFlag,
		StoreName:    *storeName,
		StoreURL:     *storeURL,
		SQLitePath:   *sqlitePath,
		BuildRuntime: *buildRuntime,
	})
	if *jsonOutput {
		if writeErr := writeIndentedJSON(report); writeErr != nil {
			return writeErr
		}
	}
	if err != nil {
		return err
	}
	if !*jsonOutput {
		printSetupReport(report)
	}
	return nil
}

type setupOptions struct {
	Repo         string
	StoreName    string
	StoreURL     string
	SQLitePath   string
	BuildRuntime bool
}

func setupLocalRuntime(ctx context.Context, opts setupOptions) (setupCommandReport, error) {
	repo, err := resolveUpdateRepo(opts.Repo)
	if err != nil {
		return setupCommandReport{OK: false}, err
	}
	storeURL, err := setupStoreURL(repo, opts.StoreURL, opts.SQLitePath)
	if err != nil {
		return setupCommandReport{OK: false, Repo: repo}, err
	}
	entry, err := newStoreConfigEntry(strings.TrimSpace(opts.StoreName), storeURL)
	if err != nil {
		return setupCommandReport{OK: false, Repo: repo}, err
	}
	cfg, err := loadStoreConfig()
	if err != nil {
		return setupCommandReport{OK: false, Repo: repo}, err
	}
	if cfg.Stores == nil {
		cfg.Stores = map[string]storeConfigEntry{}
	}
	cfg.Stores[entry.Name] = entry
	cfg.Active = entry.Name
	if err := saveStoreConfig(cfg); err != nil {
		return setupCommandReport{OK: false, Repo: repo}, err
	}
	runtimePath, err := resolveUpdateOutputPath(repo, filepath.Join(".runtime", "bin", "agent-testbench"))
	if err != nil {
		return setupCommandReport{OK: false, Repo: repo}, err
	}
	report := setupCommandReport{
		OK:   true,
		Repo: repo,
		Store: setupStoreReport{
			Name:    entry.Name,
			Backend: entry.Backend,
			URL:     maskStoreURL(entry.URL),
			Active:  true,
		},
		Runtime: setupRuntimeReport{Path: runtimePath},
		Next: []string{
			"agent-testbench status",
			"agent-testbench doctor",
			"agent-testbench store status --store " + entry.Name,
		},
	}
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o755); err != nil {
		report.OK = false
		return report, err
	}
	if opts.BuildRuntime {
		step := runUpdateCommandStep(ctx, repo, "build-runtime", "go", "build", "-o", runtimePath, "./cmd/agent-testbench")
		report.Steps = append(report.Steps, step)
		report.Runtime.Built = step.OK
		if !step.OK {
			report.OK = false
			return report, updateStepError(step)
		}
	}
	return report, nil
}

func setupStoreURL(repo string, explicitURL string, sqlitePath string) (string, error) {
	explicitURL = strings.TrimSpace(explicitURL)
	sqlitePath = strings.TrimSpace(sqlitePath)
	if explicitURL != "" && sqlitePath != "" {
		return "", fmt.Errorf("--url and --sqlite cannot be combined")
	}
	if explicitURL != "" {
		return explicitURL, nil
	}
	if sqlitePath == "" {
		sqlitePath = filepath.Join(repo, ".runtime", "agent-testbench-local.sqlite")
	} else if !filepath.IsAbs(sqlitePath) {
		sqlitePath = filepath.Join(repo, sqlitePath)
	}
	return "sqlite://" + filepath.Clean(sqlitePath), nil
}

func printSetupReport(report setupCommandReport) {
	fmt.Println("AgentTestBench Setup")
	fmt.Printf("Repo: %s\n", report.Repo)
	fmt.Printf("Store: %s (%s)\n", report.Store.Name, report.Store.Backend)
	fmt.Printf("Runtime: %s\n", report.Runtime.Path)
	if report.Runtime.Built {
		fmt.Println("Runtime Built: true")
	}
	printNextActions(report.Next)
}
