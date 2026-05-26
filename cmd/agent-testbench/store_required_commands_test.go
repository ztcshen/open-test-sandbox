package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestDailyReportExecutionsUseSelectedStoreWithoutSQLiteDefault(t *testing.T) {
	fixture := newSelectedStoreReportFixture(t)
	assertInterfaceNodeReportUsesSelectedStore(t, fixture)
	assertWorkflowReportUsesSelectedStore(t, fixture)
	assertCaseSuiteReportUsesSelectedStore(t, fixture)
}

type selectedStoreReportFixture struct {
	ctx             context.Context
	serverURL       string
	sourceStore     store.Store
	interfaceBundle profile.Bundle
	node            profile.InterfaceNode
	cases           []profile.APICase
	derived         []profile.TemplateConfig
}

func newSelectedStoreReportFixture(t *testing.T) selectedStoreReportFixture {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lookup":
			if r.URL.Query().Get("mode") == "bad" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, `{"status":"rejected"}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
		case "/first":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"item_id":"item-001"}`)
		case "/second":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	ctx := context.Background()
	sourceStore, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "store.sqlite")})
	if err != nil {
		t.Fatalf("open selected store before disabling sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sourceStore.Close() })
	t.Setenv("AGENT_TESTBENCH_DISABLE_SQLITE_STORE", "1")

	interfaceBundle, err := profile.Load(writeInterfaceNodeBatchReportProfile(t))
	if err != nil {
		t.Fatalf("load interface bundle: %v", err)
	}
	node, err := findInterfaceNodeByID(interfaceBundle.InterfaceNodes, "node.alpha")
	if err != nil {
		t.Fatalf("find node: %v", err)
	}
	cases := interfaceNodeReportCases(interfaceBundle.APICases, node.ID)
	derived := deriveInterfaceNodeCaseConfigs(interfaceBundle, node, cases)
	interfaceBundle.TemplateConfigs = mergeTemplateConfigs(interfaceBundle.TemplateConfigs, derived)

	return selectedStoreReportFixture{
		ctx:             ctx,
		serverURL:       server.URL,
		sourceStore:     sourceStore,
		interfaceBundle: interfaceBundle,
		node:            node,
		cases:           cases,
		derived:         derived,
	}
}

func assertInterfaceNodeReportUsesSelectedStore(t *testing.T, fixture selectedStoreReportFixture) {
	t.Helper()

	interfaceDir := filepath.Join(t.TempDir(), "interface-report")
	interfaceReport, err := executeInterfaceNodeCaseReport(fixture.ctx, fixture.interfaceBundle, fixture.node, fixture.cases, fixture.derived, fixture.sourceStore, "selected-store", fixture.serverURL, interfaceDir, 1)
	if err != nil {
		t.Fatalf("execute interface report with selected store: %v", err)
	}
	if !interfaceReport.OK || interfaceReport.Counts.Total != 2 || interfaceReport.Counts.Passed != 2 {
		t.Fatalf("interface report = %#v", interfaceReport)
	}
	if _, err := os.Stat(filepath.Join(interfaceDir, "runtime.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interface report created runtime.sqlite, stat err=%v", err)
	}
}

func assertWorkflowReportUsesSelectedStore(t *testing.T, fixture selectedStoreReportFixture) {
	t.Helper()

	workflowBundle, err := profile.Load(writeWorkflowBatchReportProfile(t))
	if err != nil {
		t.Fatalf("load workflow bundle: %v", err)
	}
	workflowDir := filepath.Join(t.TempDir(), "workflow-report")
	workflowReport, err := executeWorkflowCaseReport(fixture.ctx, workflowBundle, fixture.sourceStore, "workflow.alpha", workflowDir, fixture.serverURL)
	if err != nil {
		t.Fatalf("execute workflow report with selected store: %v", err)
	}
	if !workflowReport.OK || workflowReport.Counts.Total != 2 || workflowReport.Counts.Passed != 2 || workflowReport.RunID == "" {
		t.Fatalf("workflow report = %#v", workflowReport)
	}
	if _, err := os.Stat(filepath.Join(workflowDir, "runtime.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workflow report created runtime.sqlite, stat err=%v", err)
	}
}

func assertCaseSuiteReportUsesSelectedStore(t *testing.T, fixture selectedStoreReportFixture) {
	t.Helper()

	suiteDir := filepath.Join(t.TempDir(), "suite-report")
	suiteReport, err := executeCaseSuiteReport(fixture.ctx, fixture.interfaceBundle, fixture.cases, fixture.derived, fixture.sourceStore, "selected-store", caseListFilter{}, fixture.serverURL, suiteDir, 1)
	if err != nil {
		t.Fatalf("execute suite report with selected store: %v", err)
	}
	if !suiteReport.OK || suiteReport.Counts.Total != 2 || suiteReport.Counts.Passed != 2 {
		t.Fatalf("suite report = %#v", suiteReport)
	}
	if _, err := os.Stat(filepath.Join(suiteDir, "runtime.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("suite report created runtime.sqlite, stat err=%v", err)
	}
}

func TestInterfaceNodeCaseReportRequiresStoreBeforeProfileLoad(t *testing.T) {
	env := []string{"AGENT_TESTBENCH_CONFIG_HOME=" + t.TempDir()}
	out := runCLIFailsWithEnv(t, env,
		"interface-node", "case", "report",
		"--node", "node.alpha",
		"--profile", filepath.Join(t.TempDir(), "missing-profile"),
		"--json",
	)
	if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
		t.Fatalf("interface-node case report output = %q", out)
	}
	if strings.Contains(out, "missing-profile") {
		t.Fatalf("interface-node case report loaded profile before Store binding: %q", out)
	}
}

func TestWorkflowReportRequiresStoreBeforeProfileLoad(t *testing.T) {
	env := []string{"AGENT_TESTBENCH_CONFIG_HOME=" + t.TempDir()}
	out := runCLIFailsWithEnv(t, env,
		"workflow", "report",
		"--workflow", "workflow.alpha",
		"--profile", filepath.Join(t.TempDir(), "missing-profile"),
		"--json",
	)
	if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
		t.Fatalf("workflow report output = %q", out)
	}
	if strings.Contains(out, "missing-profile") {
		t.Fatalf("workflow report loaded profile before Store binding: %q", out)
	}
}

func TestCaseSuiteReportRequiresStoreBeforeProfileLoad(t *testing.T) {
	env := []string{"AGENT_TESTBENCH_CONFIG_HOME=" + t.TempDir()}
	out := runCLIFailsWithEnv(t, env,
		"case", "suite", "report",
		"--profile", filepath.Join(t.TempDir(), "missing-profile"),
		"--json",
	)
	if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
		t.Fatalf("case suite report output = %q", out)
	}
	if strings.Contains(out, "missing-profile") {
		t.Fatalf("case suite report loaded profile before Store binding: %q", out)
	}
}

func TestDailyPlanningCommandsRequireStoreBeforeProfileLoad(t *testing.T) {
	missingProfile := filepath.Join(t.TempDir(), "missing-profile")

	for _, tt := range dailyPlanningStoreRequiredCases(missingProfile) {
		t.Run(tt.name, func(t *testing.T) {
			assertCommandRequiresStoreBeforeProfileLoad(t, tt.name, tt.args)
		})
	}
}

type storeRequiredCommandCase struct {
	name string
	args []string
}

func dailyPlanningStoreRequiredCases(missingProfile string) []storeRequiredCommandCase {
	return []storeRequiredCommandCase{
		{name: "interface-node coverage", args: []string{"interface-node", "coverage", "--profile", missingProfile, "--json"}},
		{name: "interface-node coverage-gaps", args: []string{"interface-node", "coverage-gaps", "--profile", missingProfile, "--json"}},
		{name: "workflow plan", args: []string{"workflow", "plan", "--workflow", "workflow.alpha", "--profile", missingProfile, "--json"}},
		{name: "case suite stability", args: []string{"case", "suite", "stability", "--profile", missingProfile, "--json"}},
		{name: "case suite coverage", args: []string{"case", "suite", "coverage", "--profile", missingProfile, "--json"}},
		{name: "case suite priority", args: []string{"case", "suite", "priority", "--profile", missingProfile, "--json"}},
		{name: "case suite brief", args: []string{"case", "suite", "brief", "--profile", missingProfile, "--json"}},
		{name: "case suite quality", args: []string{"case", "suite", "quality", "--profile", missingProfile, "--json"}},
		{name: "case suite quality-plan", args: []string{"case", "suite", "quality-plan", "--profile", missingProfile, "--json"}},
		{name: "case suite quality-report", args: []string{"case", "suite", "quality-report", "--profile", missingProfile, "--json"}},
		{name: "case suite inspect", args: []string{"case", "suite", "inspect", "--profile", missingProfile, "--json"}},
		{name: "case suite plan", args: []string{"case", "suite", "plan", "--profile", missingProfile, "--json"}},
		{name: "case suite impact", args: []string{"case", "suite", "impact", "--profile", missingProfile, "--json"}},
		{name: "case suite impact-report", args: []string{"case", "suite", "impact-report", "--profile", missingProfile, "--json"}},
	}
}

func assertCommandRequiresStoreBeforeProfileLoad(t *testing.T, name string, args []string) {
	t.Helper()

	env := []string{"AGENT_TESTBENCH_CONFIG_HOME=" + t.TempDir()}
	out := runCLIFailsWithEnv(t, env, args...)
	if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
		t.Fatalf("%s output = %q", name, out)
	}
	if strings.Contains(out, "missing-profile") {
		t.Fatalf("%s loaded profile before Store binding: %q", name, out)
	}
}

func TestExecutorAndTemplateCommandsRequireStoreBeforeProfileLoad(t *testing.T) {
	missingProfile := filepath.Join(t.TempDir(), "missing-profile")
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "executor plan",
			args: []string{"executor", "plan", "--profile", missingProfile, "--json"},
		},
		{
			name: "template render",
			args: []string{"template", "render", "--profile", missingProfile, "--template", "template.create"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := []string{"AGENT_TESTBENCH_CONFIG_HOME=" + t.TempDir()}
			out := runCLIFailsWithEnv(t, env, tt.args...)
			if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
				t.Fatalf("%s output = %q", tt.name, out)
			}
			if strings.Contains(out, "missing-profile") {
				t.Fatalf("%s loaded profile before Store binding: %q", tt.name, out)
			}
		})
	}
}

func TestAuditCommandsRequireExplicitStoreOrOfflineReviewBeforeProfileLoad(t *testing.T) {
	missingProfile := filepath.Join(t.TempDir(), "missing-profile")
	tests := []struct {
		name       string
		args       []string
		wantPieces []string
	}{
		{
			name:       "workflow audit",
			args:       []string{"workflow", "audit", "--profile", missingProfile, "--workflow", "workflow.alpha", "--json"},
			wantPieces: []string{"--offline-template-package", "--store"},
		},
		{
			name:       "profile audit",
			args:       []string{"profile", "audit", "--profile", missingProfile, "--json"},
			wantPieces: []string{"--offline-template-package"},
		},
		{
			name:       "profile audit-plan",
			args:       []string{"profile", "audit-plan", "--profile", missingProfile, "--json"},
			wantPieces: []string{"--offline-template-package"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := []string{"AGENT_TESTBENCH_CONFIG_HOME=" + t.TempDir()}
			out := runCLIFailsWithEnv(t, env, tt.args...)
			for _, want := range tt.wantPieces {
				if !strings.Contains(out, want) {
					t.Fatalf("%s output missing %q: %q", tt.name, want, out)
				}
			}
			if strings.Contains(out, "missing-profile") {
				t.Fatalf("%s loaded profile before Store binding: %q", tt.name, out)
			}
		})
	}
}
