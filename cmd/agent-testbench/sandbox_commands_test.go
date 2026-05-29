package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

type sandboxStartCommandReport struct {
	OK       bool                         `json:"ok"`
	DryRun   bool                         `json:"dryRun"`
	Services []sandboxStartCommandService `json:"services"`
	Counts   struct {
		Planned int `json:"planned"`
	} `json:"counts"`
}

type sandboxStartCommandService struct {
	ID       string `json:"id"`
	ExitCode int    `json:"exitCode"`
	Skipped  bool   `json:"skipped"`
	Planned  bool   `json:"planned"`
}

type sandboxStartFixture struct {
	storePath           string
	startedPath         string
	platformStartedPath string
}

func TestSandboxStartCommandRunsStartupCommandsFromStore(t *testing.T) {
	fixture := writeSandboxStartStoreFixture(t)
	report := runSandboxStartJSON(t, "sqlite://"+fixture.storePath, "sandbox start")
	requireSandboxStartServices(t, report)
	requireSandboxStartupSideEffects(t, fixture)
}

func TestSandboxStartMissingServiceExplainsRegistryBoundary(t *testing.T) {
	fixture := writeSandboxStartStoreFixture(t)

	out := runCLIFails(t, "sandbox", "start", "--store", "sqlite://"+fixture.storePath, "--service", "mysql")
	if !strings.Contains(out, "profile service registry") || !strings.Contains(out, "environment restore") {
		t.Fatalf("missing service error should explain registry boundary, got %q", out)
	}
}

func TestSandboxServiceListReportsRegisteredServicesReadOnly(t *testing.T) {
	fixture := writeSandboxStartStoreFixture(t)

	out := runCLI(t, "sandbox", "service", "list", "--store", "sqlite://"+fixture.storePath, "--json")
	var report struct {
		OK       bool `json:"ok"`
		Count    int  `json:"count"`
		Services []struct {
			ID             string `json:"id"`
			Kind           string `json:"kind"`
			StartupCommand string `json:"startupCommand"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode sandbox service list json: %v\n%s", err, out)
	}
	if !report.OK || report.Count != 3 || len(report.Services) != 3 {
		t.Fatalf("sandbox service list report = %#v", report)
	}
	if report.Services[0].ID != "entry-service" || report.Services[0].Kind != "app" || report.Services[0].StartupCommand == "" {
		t.Fatalf("sandbox service list first service = %#v", report.Services[0])
	}
	requireSandboxNoStartupSideEffects(t, fixture)
}

func TestSandboxServiceListCanIncludeEnvironmentComponentGraph(t *testing.T) {
	fixture := writeSandboxStartStoreFixture(t)
	ctx := context.Background()
	s, err := openStore(ctx, "sqlite://"+fixture.storePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if _, err := s.UpsertEnvironment(ctx, store.Environment{ID: "env.sandbox.components", DisplayName: "Sandbox Components", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert environment: %v", err)
	}
	if err := s.ReplaceEnvironmentComponentGraph(ctx, "env.sandbox.components", store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "entry-service", DisplayName: "Entry Component", Kind: "app", Role: "consumer", ComposeService: "entry", Required: true},
			{ComponentID: "mysql", DisplayName: "MySQL", Kind: "database", Role: "provider", ComposeService: "mysql", Required: true},
		},
	}); err != nil {
		t.Fatalf("replace component graph: %v", err)
	}

	out := runCLI(t, "sandbox", "service", "list", "--store", "sqlite://"+fixture.storePath, "--environment", "env.sandbox.components", "--include-components", "--json")
	var report struct {
		OK       bool `json:"ok"`
		Count    int  `json:"count"`
		Services []struct {
			ID                string   `json:"id"`
			Sources           []string `json:"sources"`
			InProfileRegistry bool     `json:"inProfileRegistry"`
			InComponentGraph  bool     `json:"inComponentGraph"`
			ComposeService    string   `json:"composeService"`
			Required          bool     `json:"required"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode sandbox service/component list json: %v\n%s", err, out)
	}
	if !report.OK || report.Count != 4 {
		t.Fatalf("sandbox service/component list report = %#v", report)
	}
	byID := map[string]struct {
		sources           []string
		inProfileRegistry bool
		inComponentGraph  bool
		composeService    string
		required          bool
	}{}
	for _, item := range report.Services {
		byID[item.ID] = struct {
			sources           []string
			inProfileRegistry bool
			inComponentGraph  bool
			composeService    string
			required          bool
		}{item.Sources, item.InProfileRegistry, item.InComponentGraph, item.ComposeService, item.Required}
	}
	entry := byID["entry-service"]
	if !entry.inProfileRegistry || !entry.inComponentGraph || entry.composeService != "entry" {
		t.Fatalf("merged entry-service item = %#v", entry)
	}
	mysql := byID["mysql"]
	if mysql.inProfileRegistry || !mysql.inComponentGraph || mysql.composeService != "mysql" || !mysql.required || strings.Join(mysql.sources, ",") != "environment-component-graph" {
		t.Fatalf("component-only mysql item = %#v", mysql)
	}

	filtered := runCLI(t, "sandbox", "service", "list", "--store", "sqlite://"+fixture.storePath, "--environment", "env.sandbox.components", "--include-components", "--service", "mysql", "--json")
	var filteredReport struct {
		Count    int `json:"count"`
		Services []struct {
			ID string `json:"id"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(filtered), &filteredReport); err != nil {
		t.Fatalf("decode filtered sandbox service/component list json: %v\n%s", err, filtered)
	}
	if filteredReport.Count != 1 || filteredReport.Services[0].ID != "mysql" {
		t.Fatalf("filtered component-only service = %#v", filteredReport)
	}
}

func TestSandboxServiceListCanReadComponentOnlyEnvironment(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "component-only.sqlite")
	ctx := context.Background()
	s, err := openStore(ctx, "sqlite://"+storePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if _, err := s.UpsertEnvironment(ctx, store.Environment{ID: "env.component.only", DisplayName: "Component Only", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert environment: %v", err)
	}
	if err := s.ReplaceEnvironmentComponentGraph(ctx, "env.component.only", store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{ComponentID: "mysql", DisplayName: "MySQL", Kind: "database", Role: "provider", ComposeService: "mysql", Required: true},
		},
	}); err != nil {
		t.Fatalf("replace component graph: %v", err)
	}

	out := runCLI(t, "sandbox", "service", "list", "--store", "sqlite://"+storePath, "--environment", "env.component.only", "--include-components", "--json")
	var report struct {
		OK       bool `json:"ok"`
		Count    int  `json:"count"`
		Services []struct {
			ID                string `json:"id"`
			InProfileRegistry bool   `json:"inProfileRegistry"`
			InComponentGraph  bool   `json:"inComponentGraph"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode component-only sandbox service list json: %v\n%s", err, out)
	}
	if !report.OK || report.Count != 1 || report.Services[0].ID != "mysql" || report.Services[0].InProfileRegistry || !report.Services[0].InComponentGraph {
		t.Fatalf("component-only sandbox service list = %#v", report)
	}
}

func TestSandboxStartDryRunDoesNotRunStartupCommands(t *testing.T) {
	fixture := writeSandboxStartStoreFixture(t)

	report := runSandboxStartJSON(t, "sqlite://"+fixture.storePath, "sandbox start dry-run", "--dry-run")
	if !report.OK || !report.DryRun || report.Counts.Planned != 2 {
		t.Fatalf("sandbox start dry-run report = %#v", report)
	}
	planned := map[string]bool{}
	skipped := map[string]bool{}
	for _, service := range report.Services {
		planned[service.ID] = service.Planned
		skipped[service.ID] = service.Skipped
	}
	if !planned["entry-service"] || !planned["platform-service"] || !skipped["documented-service"] {
		t.Fatalf("sandbox start dry-run services = %#v", report.Services)
	}
	requireSandboxNoStartupSideEffects(t, fixture)
}

func writeSandboxStartStoreFixture(t *testing.T) sandboxStartFixture {
	t.Helper()

	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	startedPath := filepath.Join(dir, "started.txt")
	platformStartedPath := filepath.Join(dir, "platform-started.txt")
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sandbox",
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{
				ID:             "entry-service",
				DisplayName:    "Entry Service",
				Kind:           "app",
				StartupCommand: fmt.Sprintf("printf entry-service > %q", startedPath),
				Status:         "active",
			},
			{
				ID:             "platform-service",
				DisplayName:    "Platform Service",
				Kind:           "platform",
				StartupCommand: fmt.Sprintf("printf platform-service > %q", platformStartedPath),
				Status:         "active",
			},
			{
				ID:          "documented-service",
				DisplayName: "Documented Service",
				Kind:        "external",
				Status:      "active",
			},
		},
	}); err != nil {
		t.Fatalf("replace catalog: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	return sandboxStartFixture{
		storePath:           storePath,
		startedPath:         startedPath,
		platformStartedPath: platformStartedPath,
	}
}

func runSandboxStartJSON(t *testing.T, storeRef string, label string, args ...string) sandboxStartCommandReport {
	t.Helper()

	cliArgs := append([]string{"sandbox", "start", "--json"}, args...)
	if storeRef != "" {
		cliArgs = append([]string{"sandbox", "start", "--store", storeRef, "--json"}, args...)
	}
	out := runCLI(t, cliArgs...)
	var report sandboxStartCommandReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s sandbox start report: %v\n%s", label, err, out)
	}
	return report
}

func requireSandboxStartServices(t *testing.T, report sandboxStartCommandReport) {
	t.Helper()

	if !report.OK || len(report.Services) != 3 {
		t.Fatalf("sandbox start report = %#v", report)
	}
	byID := map[string]int{}
	skippedByID := map[string]bool{}
	for _, service := range report.Services {
		byID[service.ID] = service.ExitCode
		skippedByID[service.ID] = service.Skipped
	}
	if byID["entry-service"] != 0 || skippedByID["entry-service"] {
		t.Fatalf("entry-service result exit=%d skipped=%t", byID["entry-service"], skippedByID["entry-service"])
	}
	if byID["platform-service"] != 0 || skippedByID["platform-service"] {
		t.Fatalf("platform-service result exit=%d skipped=%t", byID["platform-service"], skippedByID["platform-service"])
	}
	if !skippedByID["documented-service"] {
		t.Fatalf("documented-service should be skipped without a startup command")
	}
}

func requireSandboxStartupSideEffects(t *testing.T, fixture sandboxStartFixture) {
	t.Helper()

	started, err := os.ReadFile(fixture.startedPath)
	if err != nil {
		t.Fatalf("read startup side effect: %v", err)
	}
	if string(started) != "entry-service" {
		t.Fatalf("startup command wrote %q", started)
	}
	platformStarted, err := os.ReadFile(fixture.platformStartedPath)
	if err != nil {
		t.Fatalf("read platform startup side effect: %v", err)
	}
	if string(platformStarted) != "platform-service" {
		t.Fatalf("platform startup command wrote %q", platformStarted)
	}
}

func requireSandboxNoStartupSideEffects(t *testing.T, fixture sandboxStartFixture) {
	t.Helper()
	for _, path := range []string{fixture.startedPath, fixture.platformStartedPath} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("startup side effect should not exist: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat startup side effect %s: %v", path, err)
		}
	}
}

func TestSandboxStartUsesNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-sandbox-start-pg")
	runSandboxStartUsesNamedActiveStore(t, storeRef, "pg", "PostgreSQL")
}

func TestSandboxStartUsesNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-sandbox-start-mysql")
	runSandboxStartUsesNamedActiveStore(t, storeRef, "mysql", "MySQL")
}

func runSandboxStartUsesNamedActiveStore(t *testing.T, storeRef string, suffixLabel string, label string) {
	t.Helper()
	startedPath, serviceID := seedNamedSandboxStartCatalog(t, storeRef, suffixLabel, label)
	report := runSandboxStartJSON(t, "", label, "--service", serviceID)
	requireNamedSandboxStartReport(t, label, report, serviceID)
	requireNamedSandboxStartupSideEffect(t, label, startedPath, serviceID)
}

func seedNamedSandboxStartCatalog(t *testing.T, storeRef string, suffixLabel string, label string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	startedPath := filepath.Join(dir, "started-"+suffixLabel+".txt")
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	serviceID := "entry-service-" + suffixLabel + "-" + suffix

	ctx := context.Background()
	s, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open %s active SQL Store: %v", label, err)
	}
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sandbox-" + suffixLabel + "-" + suffix,
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{
				ID:             serviceID,
				DisplayName:    "Entry Service " + label,
				Kind:           "app",
				StartupCommand: fmt.Sprintf("printf %s > %q", serviceID, startedPath),
				Status:         "active",
			},
		},
	}); err != nil {
		_ = s.Close()
		t.Fatalf("replace %s catalog: %v", label, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close %s SQL Store: %v", label, err)
	}
	return startedPath, serviceID
}

func requireNamedSandboxStartReport(t *testing.T, label string, report sandboxStartCommandReport, serviceID string) {
	t.Helper()

	if !report.OK || len(report.Services) != 1 || report.Services[0].ID != serviceID || report.Services[0].ExitCode != 0 || report.Services[0].Skipped {
		t.Fatalf("%s sandbox start report = %#v", label, report)
	}
}

func requireNamedSandboxStartupSideEffect(t *testing.T, label string, startedPath string, serviceID string) {
	t.Helper()

	started, err := os.ReadFile(startedPath)
	if err != nil {
		t.Fatalf("read %s startup side effect: %v", label, err)
	}
	if string(started) != serviceID {
		t.Fatalf("%s startup command wrote %q want %q", label, started, serviceID)
	}
}

func TestSandboxRegisterCommandsWriteStoreCatalog(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")

	serviceOut := runCLI(t, "sandbox", "service", "register",
		"--store", "sqlite://"+storePath,
		"--id", "service.gateway",
		"--display-name", "Gateway",
		"--kind", "http",
		"--service-port", "18080",
		"--health-url", newHealthyTestURL(t),
	)
	if !strings.Contains(serviceOut, "Registered service: service.gateway") {
		t.Fatalf("service register output = %q", serviceOut)
	}

	interfaceOut := runCLI(t, "sandbox", "interface", "register",
		"--store", "sqlite://"+storePath,
		"--id", "node.create-order",
		"--service-id", "service.gateway",
		"--method", "POST",
		"--path", "/orders",
		"--case-id", "case.create-order",
		"--case-title", "Create order",
		"--required-for-admission",
	)
	if !strings.Contains(interfaceOut, "Registered interface: node.create-order") || !strings.Contains(interfaceOut, "Case: case.create-order") {
		t.Fatalf("interface register output = %q", interfaceOut)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	catalog, err := s.GetProfileCatalog(context.Background())
	if err != nil {
		t.Fatalf("get catalog: %v", err)
	}
	if catalog.ProfileID != "current" || len(catalog.Services) != 1 || catalog.Services[0].ID != "service.gateway" {
		t.Fatalf("catalog services = %#v", catalog)
	}
	if len(catalog.InterfaceNodes) != 1 || catalog.InterfaceNodes[0].ID != "node.create-order" || catalog.InterfaceNodes[0].ServiceID != "service.gateway" {
		t.Fatalf("catalog interface nodes = %#v", catalog.InterfaceNodes)
	}
	if len(catalog.RequestTemplates) != 1 || catalog.RequestTemplates[0].NodeID != "node.create-order" {
		t.Fatalf("catalog request templates = %#v", catalog.RequestTemplates)
	}
	if len(catalog.APICases) != 1 || catalog.APICases[0].ID != "case.create-order" || !catalog.APICases[0].RequiredForAdmission {
		t.Fatalf("catalog api cases = %#v", catalog.APICases)
	}
}

func TestSandboxRegisterCommandsUseNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-sandbox-register-pg")
	runSandboxRegisterCommandsUseNamedActiveStore(t, storeRef, "pg", "PostgreSQL")
}

func TestSandboxRegisterCommandsUseNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-sandbox-register-mysql")
	runSandboxRegisterCommandsUseNamedActiveStore(t, storeRef, "mysql", "MySQL")
}

func runSandboxRegisterCommandsUseNamedActiveStore(t *testing.T, storeRef string, suffixLabel string, label string) {
	t.Helper()
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	serviceID := "service.gateway." + suffixLabel + "." + suffix
	nodeID := "node.create-order." + suffixLabel + "." + suffix
	caseID := "case.create-order." + suffixLabel + "." + suffix

	registerNamedSandboxService(t, label, serviceID)
	registerNamedSandboxInterface(t, label, serviceID, nodeID, caseID)
	requireNamedSandboxCatalog(t, storeRef, label, serviceID, nodeID, caseID)
}

func registerNamedSandboxService(t *testing.T, label string, serviceID string) {
	t.Helper()

	out := runCLI(t, "sandbox", "service", "register",
		"--id", serviceID,
		"--display-name", "Gateway "+label,
		"--kind", "http",
		"--service-port", "18080",
		"--health-url", newHealthyTestURL(t),
	)
	if !strings.Contains(out, "Registered service: "+serviceID) {
		t.Fatalf("%s service register output = %q", label, out)
	}
}

func registerNamedSandboxInterface(t *testing.T, label string, serviceID string, nodeID string, caseID string) {
	t.Helper()

	out := runCLI(t, "sandbox", "interface", "register",
		"--id", nodeID,
		"--service-id", serviceID,
		"--method", "POST",
		"--path", "/orders",
		"--case-id", caseID,
		"--case-title", "Create order",
		"--required-for-admission",
	)
	if !strings.Contains(out, "Registered interface: "+nodeID) || !strings.Contains(out, "Case: "+caseID) {
		t.Fatalf("%s interface register output = %q", label, out)
	}
}

func requireNamedSandboxCatalog(t *testing.T, storeRef string, label string, serviceID string, nodeID string, caseID string) {
	t.Helper()

	s, err := openStore(context.Background(), storeRef)
	if err != nil {
		t.Fatalf("open SQL Store: %v", err)
	}
	defer s.Close()
	catalog, err := s.GetProfileCatalog(context.Background())
	if err != nil {
		t.Fatalf("get %s catalog: %v", label, err)
	}
	serviceFound := false
	for _, service := range catalog.Services {
		if service.ID == serviceID {
			serviceFound = true
			break
		}
	}
	if !serviceFound {
		t.Fatalf("%s catalog services = %#v", label, catalog.Services)
	}
	nodeFound := false
	for _, node := range catalog.InterfaceNodes {
		if node.ID == nodeID && node.ServiceID == serviceID {
			nodeFound = true
			break
		}
	}
	if !nodeFound {
		t.Fatalf("%s catalog interface nodes = %#v", label, catalog.InterfaceNodes)
	}
	templateFound := false
	for _, template := range catalog.RequestTemplates {
		if template.NodeID == nodeID {
			templateFound = true
			break
		}
	}
	if !templateFound {
		t.Fatalf("%s catalog request templates = %#v", label, catalog.RequestTemplates)
	}
	caseFound := false
	for _, apiCase := range catalog.APICases {
		if apiCase.ID == caseID && apiCase.RequiredForAdmission {
			caseFound = true
			break
		}
	}
	if !caseFound {
		t.Fatalf("%s catalog api cases = %#v", label, catalog.APICases)
	}
}
