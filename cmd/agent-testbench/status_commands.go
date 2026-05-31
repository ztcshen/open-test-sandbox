package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-testbench/internal/store/mysql"
	"agent-testbench/internal/store/postgres"
	"agent-testbench/internal/store/sqlite"
)

type statusCommandReport struct {
	OK      bool                `json:"ok"`
	Version string              `json:"version"`
	Repo    statusRepoReport    `json:"repo"`
	Runtime statusRuntimeReport `json:"runtime"`
	Store   statusStoreReport   `json:"store"`
	Next    []string            `json:"next"`
}

type statusRepoReport struct {
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	Revision string `json:"revision,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Dirty    bool   `json:"dirty"`
	Error    string `json:"error,omitempty"`
}

type statusRuntimeReport struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Executable bool   `json:"executable"`
}

type statusStoreReport struct {
	Configured bool   `json:"configured"`
	Name       string `json:"name,omitempty"`
	Backend    string `json:"backend,omitempty"`
	URL        string `json:"url,omitempty"`
	RawURL     string `json:"-"`
	ConfigPath string `json:"configPath,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Schema     any    `json:"schema,omitempty"`
}

type doctorCommandReport struct {
	OK     bool                `json:"ok"`
	Checks []doctorCheckReport `json:"checks"`
	Next   []string            `json:"next"`
}

type doctorCheckReport struct {
	Name     string `json:"name"`
	Code     string `json:"code"`
	OK       bool   `json:"ok"`
	Optional bool   `json:"optional,omitempty"`
	Fixed    bool   `json:"fixed,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Fix      string `json:"fix,omitempty"`
}

func runStatus(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	deep := flags.Bool("deep", false, "Include slower Store schema checks")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable status report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected status arguments: %s", strings.Join(flags.Args(), " "))
	}
	report := buildStatusReport(ctx, *deep)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printStatusReport(report)
	return nil
}

func runDoctor(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	fix := flags.Bool("fix", false, "Apply low-risk local setup repairs")
	deep := flags.Bool("deep", false, "Run slower diagnostics such as Docker Compose, Store schema, and optional trace endpoint checks")
	traceURL := flags.String("trace-graphql-url", strings.TrimSpace(os.Getenv("AGENT_TESTBENCH_TRACE_GRAPHQL_URL")), "SkyWalking GraphQL URL for deep reachability diagnostics")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable diagnostics report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected doctor arguments: %s", strings.Join(flags.Args(), " "))
	}
	report := buildDoctorReport(ctx, doctorOptions{Fix: *fix, Deep: *deep, TraceGraphQLURL: *traceURL})
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printDoctorReport(report)
	return nil
}

func buildStatusReport(ctx context.Context, deep bool) statusCommandReport {
	repo := statusRepo(ctx)
	runtime := statusRuntime(repo.Path)
	store := statusStore()
	if deep && store.Configured {
		store.Schema = statusStoreSchema(ctx, store)
	}
	next := statusNextActions(runtime, store)
	return statusCommandReport{
		OK:      repo.Error == "",
		Version: version,
		Repo:    repo,
		Runtime: runtime,
		Store:   store,
		Next:    next,
	}
}

func statusRepo(ctx context.Context) statusRepoReport {
	repo, err := resolveUpdateRepo("")
	if err != nil {
		return statusRepoReport{Error: err.Error()}
	}
	if root, rootErr := updateGitOutput(ctx, repo, "rev-parse", "--show-toplevel"); rootErr == nil {
		repo = root
	}
	report := statusRepoReport{Path: repo}
	if branch, branchErr := updateGitOutput(ctx, repo, "branch", "--show-current"); branchErr == nil {
		report.Branch = branch
	}
	if revision, revErr := updateGitOutput(ctx, repo, "rev-parse", "HEAD"); revErr == nil {
		report.Revision = revision
	}
	if upstream, upstreamErr := updateGitOutput(ctx, repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); upstreamErr == nil {
		report.Upstream = upstream
	}
	if dirty, dirtyErr := updateTrackedDirty(ctx, repo); dirtyErr == nil {
		report.Dirty = dirty
	}
	if report.Revision == "" {
		report.Error = "not a git checkout"
	}
	return report
}

func statusRuntime(repo string) statusRuntimeReport {
	if strings.TrimSpace(repo) == "" {
		repo = "."
	}
	path, err := resolveUpdateOutputPath(repo, filepath.Join(".runtime", "bin", "agent-testbench"))
	if err != nil {
		path = filepath.Join(repo, ".runtime", "bin", "agent-testbench")
	}
	report := statusRuntimeReport{Path: path}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return report
	}
	report.Exists = true
	report.Executable = info.Mode()&0o111 != 0
	return report
}

func statusStore() statusStoreReport {
	path, pathErr := storeConfigPath()
	report := statusStoreReport{}
	if pathErr == nil {
		report.ConfigPath = path
	}
	cfg, err := loadStoreConfig()
	if err != nil {
		report.Detail = err.Error()
		return report
	}
	if strings.TrimSpace(cfg.Active) == "" {
		report.Detail = "no active Store configured"
		return report
	}
	entry, ok := cfg.Stores[cfg.Active]
	if !ok {
		report.Detail = fmt.Sprintf("active Store %q is missing from config", cfg.Active)
		return report
	}
	report.Configured = true
	report.Name = entry.Name
	report.Backend = entry.Backend
	report.RawURL = entry.URL
	report.URL = maskStoreURL(entry.URL)
	return report
}

func statusStoreSchema(ctx context.Context, store statusStoreReport) storeStatusReport {
	backend, err := storeBackendFromURL(store.RawURL)
	if err != nil {
		return storeStatusReport{OK: false, Backend: store.Backend, URL: store.URL, Error: err.Error()}
	}
	switch backend {
	case "postgres":
		cfg, cfgErr := postgres.ParseConfigFromURL(store.RawURL)
		if cfgErr != nil {
			return postgresStoreStatusErrorReport(store.RawURL, cfgErr)
		}
		status, statusErr := postgresSchemaStatus(ctx, cfg)
		if statusErr != nil {
			return postgresStoreStatusErrorReport(store.RawURL, statusErr)
		}
		return postgresStoreStatusReport(status)
	case "mysql":
		cfg, cfgErr := mysql.ParseConfigFromURL(store.RawURL)
		if cfgErr != nil {
			return mysqlStoreStatusErrorReport(store.RawURL, cfgErr)
		}
		status, statusErr := mysqlSchemaStatus(ctx, cfg)
		if statusErr != nil {
			return mysqlStoreStatusErrorReport(store.RawURL, statusErr)
		}
		return mysqlStoreStatusReport(status)
	default:
		cfg, cfgErr := sqlite.ParseConfigFromURL(store.RawURL)
		if cfgErr != nil {
			return sqliteStoreStatusErrorReport(cfg, cfgErr)
		}
		status, statusErr := sqlite.SchemaStatus(ctx, cfg)
		if statusErr != nil {
			return sqliteStoreStatusErrorReport(cfg, statusErr)
		}
		return sqliteStoreStatusReport(status)
	}
}

func statusNextActions(runtime statusRuntimeReport, store statusStoreReport) []string {
	next := []string{}
	if !store.Configured {
		next = append(next,
			"agent-testbench store config set NAME --url sqlite://PATH",
			"agent-testbench store use NAME",
		)
	}
	if !runtime.Exists {
		next = append(next, "agent-testbench update")
	}
	next = append(next, "agent-testbench commands --filter \"case gate\"")
	return next
}

type doctorOptions struct {
	Fix             bool
	Deep            bool
	TraceGraphQLURL string
}

const (
	doctorCheckActiveStore  = "active-store"
	doctorCheckTraceGraphQL = "trace-graphql"
	doctorCodeTraceGraphQL  = "trace.graphql"
)

func buildDoctorReport(ctx context.Context, opts doctorOptions) doctorCommandReport {
	fixErr := ""
	if opts.Fix {
		if err := applyDoctorFixes(); err != nil {
			fixErr = err.Error()
		}
	}
	status := buildStatusReport(ctx, opts.Deep)
	checks := []doctorCheckReport{
		doctorToolCheck("git", false),
		doctorToolCheck("go", false),
		doctorToolCheck("npm", false),
		doctorToolCheck("docker", true),
		doctorRepoCheck(status.Repo),
		doctorStoreCheck(status.Store),
		doctorRuntimeDirectoryCheck(status.Runtime),
		doctorRuntimeCheck(status.Runtime),
	}
	if fixErr != "" {
		checks = append(checks, doctorCheckReport{Name: "doctor-fix", Code: "doctor.fix", OK: false, Detail: fixErr, Fix: "check repository and config-home permissions, then rerun agent-testbench doctor --fix"})
	}
	if opts.Deep {
		checks = append(checks, doctorDockerComposeCheck(ctx))
		checks = append(checks, doctorStoreSchemaCheck(status.Store))
		if strings.TrimSpace(opts.TraceGraphQLURL) != "" {
			checks = append(checks, doctorTraceGraphQLCheck(ctx, opts.TraceGraphQLURL))
		}
	}
	ok := true
	for _, check := range checks {
		if !check.OK && !check.Optional {
			ok = false
			break
		}
	}
	return doctorCommandReport{OK: ok, Checks: checks, Next: status.Next}
}

func doctorToolCheck(name string, optional bool) doctorCheckReport {
	path, err := exec.LookPath(name)
	if err != nil {
		fix := fmt.Sprintf("install %s and ensure it is on PATH", name)
		if optional {
			fix = fmt.Sprintf("install %s before Docker-backed restore flows", name)
		}
		return doctorCheckReport{Name: "tool-" + name, Code: "tool." + name, OK: false, Optional: optional, Detail: "not found on PATH", Fix: fix}
	}
	return doctorCheckReport{Name: "tool-" + name, Code: "tool." + name, OK: true, Optional: optional, Detail: path}
}

func doctorRepoCheck(repo statusRepoReport) doctorCheckReport {
	if repo.Error != "" {
		return doctorCheckReport{Name: "git-checkout", Code: "git.checkout", OK: false, Detail: repo.Error, Fix: "run from an AgentTestBench git checkout or pass --repo to update"}
	}
	detail := repo.Path
	if repo.Branch != "" {
		detail = fmt.Sprintf("%s on %s", repo.Path, repo.Branch)
	}
	return doctorCheckReport{Name: "git-checkout", Code: "git.checkout", OK: true, Detail: detail}
}

func doctorStoreCheck(store statusStoreReport) doctorCheckReport {
	if store.Configured {
		return doctorCheckReport{Name: doctorCheckActiveStore, Code: "store.active", OK: true, Fixed: doctorActiveStoreIsFixed(), Detail: fmt.Sprintf("%s (%s)", store.Name, store.Backend)}
	}
	fix := "run agent-testbench store config set NAME --url sqlite://PATH, then agent-testbench store use NAME"
	return doctorCheckReport{
		Name:   doctorCheckActiveStore,
		Code:   "store.active",
		OK:     false,
		Fixed:  doctorActiveStoreIsFixed(),
		Detail: fmt.Sprintf("%s; %s", stringDefault(store.Detail, "no active Store configured"), fix),
		Fix:    fix,
	}
}

func doctorRuntimeDirectoryCheck(runtime statusRuntimeReport) doctorCheckReport {
	dir := filepath.Dir(runtime.Path)
	info, err := os.Stat(dir)
	if err == nil && info.IsDir() {
		return doctorCheckReport{Name: "runtime-directory", Code: "runtime.directory", OK: true, Fixed: doctorRuntimeDirectoryWasFixed(), Detail: dir}
	}
	return doctorCheckReport{Name: "runtime-directory", Code: "runtime.directory", OK: false, Optional: true, Detail: dir + " is missing", Fix: "run agent-testbench doctor --fix"}
}

func doctorRuntimeCheck(runtime statusRuntimeReport) doctorCheckReport {
	if runtime.Exists && runtime.Executable {
		return doctorCheckReport{Name: "runtime-binary", Code: "runtime.binary", OK: true, Optional: true, Detail: runtime.Path}
	}
	detail := "missing"
	if runtime.Exists {
		detail = "exists but is not executable"
	}
	return doctorCheckReport{Name: "runtime-binary", Code: "runtime.binary", OK: false, Optional: true, Detail: detail, Fix: "run agent-testbench update"}
}

func doctorDockerComposeCheck(ctx context.Context) doctorCheckReport {
	step := runUpdateCommandStep(ctx, ".", "docker-compose-version", "docker", "compose", "version")
	if !step.OK {
		return doctorCheckReport{Name: "docker-compose", Code: "docker.compose", OK: false, Optional: true, Detail: strings.TrimSpace(step.Error), Fix: "install Docker Compose before Docker-backed restore flows"}
	}
	return doctorCheckReport{Name: "docker-compose", Code: "docker.compose", OK: true, Optional: true, Detail: strings.TrimSpace(step.Output)}
}

func doctorStoreSchemaCheck(store statusStoreReport) doctorCheckReport {
	if !store.Configured {
		return doctorCheckReport{Name: "store-schema", Code: "store.schema", OK: false, Optional: true, Detail: "no active Store configured", Fix: "configure an active Store before deep Store diagnostics"}
	}
	schema := jsonObjectFromAny(store.Schema)
	if boolFromReportAny(schema["ok"]) {
		return doctorCheckReport{Name: "store-schema", Code: "store.schema", OK: true, Detail: fmt.Sprintf("%s pending=%d", store.Backend, intFromReportAny(schema["pending"]))}
	}
	return doctorCheckReport{Name: "store-schema", Code: "store.schema", OK: false, Detail: valueString(schema["error"]), Fix: "run agent-testbench store status --json and fix the Store connection or schema"}
}

func doctorTraceGraphQLCheck(ctx context.Context, rawURL string) doctorCheckReport {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body := bytes.NewBufferString(`{"query":"{__typename}"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, body)
	if err != nil {
		return doctorCheckReport{Name: doctorCheckTraceGraphQL, Code: doctorCodeTraceGraphQL, OK: false, Optional: true, Detail: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return doctorCheckReport{Name: doctorCheckTraceGraphQL, Code: doctorCodeTraceGraphQL, OK: false, Optional: true, Detail: err.Error(), Fix: "check AGENT_TESTBENCH_TRACE_GRAPHQL_URL or network reachability"}
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: close trace GraphQL response body: %v\n", closeErr)
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return doctorCheckReport{Name: doctorCheckTraceGraphQL, Code: doctorCodeTraceGraphQL, OK: false, Optional: true, Detail: resp.Status, Fix: "check the SkyWalking GraphQL endpoint"}
	}
	return doctorCheckReport{Name: doctorCheckTraceGraphQL, Code: doctorCodeTraceGraphQL, OK: true, Optional: true, Detail: resp.Status}
}

var doctorFixedActiveStore bool
var doctorFixedRuntimeDirectory bool

func applyDoctorFixes() error {
	doctorFixedActiveStore = false
	doctorFixedRuntimeDirectory = false
	repo, err := resolveUpdateRepo("")
	if err != nil {
		return err
	}
	runtimeDir := filepath.Join(repo, ".runtime", "bin")
	if err := os.MkdirAll(runtimeDir, 0o755); err == nil {
		doctorFixedRuntimeDirectory = true
	}
	if _, err := activeStoreConfig(); err != nil {
		if errors.Is(err, errNoActiveStoreConfigured) {
			cfg, loadErr := loadStoreConfig()
			if loadErr != nil {
				return loadErr
			}
			if cfg.Stores == nil {
				cfg.Stores = map[string]storeConfigEntry{}
			}
			entry, entryErr := newStoreConfigEntry("local", "sqlite://"+filepath.Join(repo, ".runtime", "agent-testbench-local.sqlite"))
			if entryErr != nil {
				return entryErr
			}
			cfg.Stores[entry.Name] = entry
			cfg.Active = entry.Name
			if saveErr := saveStoreConfig(cfg); saveErr != nil {
				return saveErr
			}
			doctorFixedActiveStore = true
		}
	}
	return nil
}

func doctorActiveStoreIsFixed() bool {
	return doctorFixedActiveStore
}

func doctorRuntimeDirectoryWasFixed() bool {
	return doctorFixedRuntimeDirectory
}

func printStatusReport(report statusCommandReport) {
	fmt.Println("AgentTestBench Status")
	fmt.Printf("Version: %s\n", report.Version)
	fmt.Println()
	fmt.Println("Repo")
	fmt.Printf("  Path: %s\n", report.Repo.Path)
	fmt.Printf("  Branch: %s\n", stringDefault(report.Repo.Branch, "(unknown)"))
	fmt.Printf("  Revision: %s\n", stringDefault(shortRevision(report.Repo.Revision), "(unknown)"))
	fmt.Printf("  Upstream: %s\n", stringDefault(report.Repo.Upstream, "(none)"))
	fmt.Printf("  Dirty: %t\n", report.Repo.Dirty)
	fmt.Println()
	fmt.Println("Runtime")
	fmt.Printf("  Binary: %s\n", report.Runtime.Path)
	fmt.Printf("  Ready: %t\n", report.Runtime.Exists && report.Runtime.Executable)
	fmt.Println()
	fmt.Println("Store")
	if report.Store.Configured {
		fmt.Printf("  Active: %s (%s)\n", report.Store.Name, report.Store.Backend)
		fmt.Printf("  URL: %s\n", report.Store.URL)
	} else {
		fmt.Printf("  Active: none (%s)\n", stringDefault(report.Store.Detail, "not configured"))
	}
	printNextActions(report.Next)
}

func printDoctorReport(report doctorCommandReport) {
	fmt.Println("AgentTestBench Doctor")
	for _, check := range report.Checks {
		state := "ok"
		if !check.OK && check.Optional {
			state = "warn"
		} else if !check.OK {
			state = "issue"
		}
		fmt.Printf("- %s [%s] %s\n", check.Name, state, check.Detail)
		if check.Fix != "" {
			fmt.Printf("  fix: %s\n", check.Fix)
		}
	}
	printNextActions(report.Next)
}

func printNextActions(next []string) {
	if len(next) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("Next")
	for _, item := range next {
		fmt.Printf("  - %s\n", item)
	}
}

func shortRevision(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) <= 12 {
		return revision
	}
	return revision[:12]
}
