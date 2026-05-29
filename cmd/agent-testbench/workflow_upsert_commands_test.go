package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowRegisterAndBindingUpsertStoreCatalog(t *testing.T) {
	profileDir := writeWorkflowBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "workflow-upsert.sqlite")
	storeRef := "sqlite://" + storePath
	runCLI(t, "config", "publish", "--from", profileDir, "--store", storeRef)

	requireWorkflowRegisterReport(t, runWorkflowRegisterJSON(t, storeRef))
	requireWorkflowBindingRegisterReport(t, runWorkflowBindingRegisterJSON(t, storeRef))
	requireWorkflowUpsertInPlace(t, storeRef)
	requireWorkflowDiscoverAfterUpsert(t, storeRef)
	requireWorkflowPlanAfterBinding(t, storeRef)
}

func runWorkflowRegisterJSON(t *testing.T, storeRef string) workflowRegisterTestReport {
	t.Helper()
	out := runCLI(t, "workflow", "register",
		"--store", storeRef,
		"--id", "workflow.smoke",
		"--display-name", "Smoke Workflow",
		"--description", "Ad hoc smoke workflow",
		"--base-step-timeout-ms", "1200",
		"--timeout-offset-ms", "200",
		"--audit",
		"--json",
	)
	var report workflowRegisterTestReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode workflow register json: %v\n%s", err, out)
	}
	return report
}

type workflowRegisterTestReport struct {
	OK        bool   `json:"ok"`
	Created   bool   `json:"created"`
	Updated   bool   `json:"updated"`
	ProfileID string `json:"profileId"`
	Workflow  struct {
		ID                string `json:"id"`
		DisplayName       string `json:"displayName"`
		BaseStepTimeoutMs int    `json:"baseStepTimeoutMs"`
		TimeoutOffsetMs   int    `json:"timeoutOffsetMs"`
	} `json:"workflow"`
	Counts workflowCountsTestReport `json:"counts"`
	Audit  struct {
		OK         bool `json:"ok"`
		IssueCount int  `json:"issueCount"`
	} `json:"audit"`
}

type workflowCountsTestReport struct {
	Before struct {
		Workflows int `json:"workflows"`
	} `json:"before"`
	After struct {
		Workflows int `json:"workflows"`
	} `json:"after"`
}

func requireWorkflowRegisterReport(t *testing.T, report workflowRegisterTestReport) {
	t.Helper()
	if !report.OK || !report.Created || report.Updated || report.ProfileID != "sample" {
		t.Fatalf("workflow register summary = %#v", report)
	}
	if report.Workflow.ID != "workflow.smoke" || report.Workflow.DisplayName != "Smoke Workflow" || report.Workflow.BaseStepTimeoutMs != 1200 || report.Workflow.TimeoutOffsetMs != 200 {
		t.Fatalf("workflow register item = %#v", report.Workflow)
	}
	if report.Counts.After.Workflows != report.Counts.Before.Workflows+1 {
		t.Fatalf("workflow register counts = %#v", report.Counts)
	}
	if !report.Audit.OK || report.Audit.IssueCount != 0 {
		t.Fatalf("workflow register audit = %#v", report.Audit)
	}
}

func runWorkflowBindingRegisterJSON(t *testing.T, storeRef string) workflowBindingRegisterTestReport {
	t.Helper()
	out := runCLI(t, "workflow", "binding", "register",
		"--store", storeRef,
		"--workflow", "workflow.smoke",
		"--step", "smoke",
		"--node", "node.first",
		"--case", "case.first",
		"--required",
		"--sort-order", "5",
		"--audit",
		"--json",
	)
	var report workflowBindingRegisterTestReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode workflow binding register json: %v\n%s", err, out)
	}
	return report
}

type workflowBindingRegisterTestReport struct {
	OK      bool `json:"ok"`
	Created bool `json:"created"`
	Updated bool `json:"updated"`
	Binding struct {
		WorkflowID string `json:"workflowId"`
		StepID     string `json:"stepId"`
		NodeID     string `json:"nodeId"`
		CaseID     string `json:"caseId"`
		Required   bool   `json:"required"`
		SortOrder  int    `json:"sortOrder"`
	} `json:"binding"`
	Audit struct {
		OK           bool `json:"ok"`
		BindingCount int  `json:"bindingCount"`
		IssueCount   int  `json:"issueCount"`
	} `json:"audit"`
}

func requireWorkflowBindingRegisterReport(t *testing.T, report workflowBindingRegisterTestReport) {
	t.Helper()
	if !report.OK || !report.Created || report.Updated {
		t.Fatalf("workflow binding register summary = %#v", report)
	}
	if report.Binding.WorkflowID != "workflow.smoke" || report.Binding.StepID != "smoke" || report.Binding.NodeID != "node.first" || report.Binding.CaseID != "case.first" || !report.Binding.Required || report.Binding.SortOrder != 5 {
		t.Fatalf("workflow binding register item = %#v", report.Binding)
	}
	if !report.Audit.OK || report.Audit.BindingCount != 1 || report.Audit.IssueCount != 0 {
		t.Fatalf("workflow binding audit = %#v", report.Audit)
	}
}

func requireWorkflowUpsertInPlace(t *testing.T, storeRef string) {
	t.Helper()
	out := runCLI(t, "workflow", "upsert", "--store", storeRef, "--id", "workflow.smoke", "--display-name", "Smoke Workflow Updated", "--json")
	var report struct {
		Created bool                     `json:"created"`
		Updated bool                     `json:"updated"`
		Counts  workflowCountsTestReport `json:"counts"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode workflow upsert json: %v\n%s", err, out)
	}
	if report.Created || !report.Updated || report.Counts.After.Workflows != report.Counts.Before.Workflows {
		t.Fatalf("workflow upsert should update in place: %#v", report)
	}
}

func requireWorkflowDiscoverAfterUpsert(t *testing.T, storeRef string) {
	t.Helper()
	discoverOut := runCLI(t, "workflow", "discover", "--store", storeRef, "--filter", "Smoke Workflow Updated", "--json")
	var discoverReport struct {
		Items []struct {
			ID        string `json:"id"`
			StepCount int    `json:"stepCount"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(discoverOut), &discoverReport); err != nil {
		t.Fatalf("decode workflow discover json: %v\n%s", err, discoverOut)
	}
	if len(discoverReport.Items) != 1 || discoverReport.Items[0].ID != "workflow.smoke" || discoverReport.Items[0].StepCount != 1 {
		t.Fatalf("workflow discover after upsert = %#v", discoverReport.Items)
	}
}

func requireWorkflowPlanAfterBinding(t *testing.T, storeRef string) {
	t.Helper()
	planOut := runCLI(t, "workflow", "plan", "--store", storeRef, "--workflow", "workflow.smoke", "--json")
	if !strings.Contains(planOut, `"stepId": "smoke"`) || !strings.Contains(planOut, `"caseId": "case.first"`) {
		t.Fatalf("workflow plan after binding upsert = %s", planOut)
	}
}

func TestWorkflowBindingAuditReportsMissingReferences(t *testing.T) {
	profileDir := writeWorkflowBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "workflow-binding-audit.sqlite")
	storeRef := "sqlite://" + storePath
	runCLI(t, "config", "publish", "--from", profileDir, "--store", storeRef)
	runCLI(t, "workflow", "register", "--store", storeRef, "--id", "workflow.audit", "--json")

	out := runCLI(t,
		"workflow", "binding", "register",
		"--store", storeRef,
		"--workflow", "workflow.audit",
		"--step", "missing",
		"--node", "node.missing",
		"--case", "case.missing",
		"--audit",
		"--json",
	)
	var report struct {
		OK    bool `json:"ok"`
		Audit struct {
			OK     bool `json:"ok"`
			Issues []struct {
				Code string `json:"code"`
			} `json:"issues"`
		} `json:"audit"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode workflow binding audit json: %v\n%s", err, out)
	}
	if !report.OK || report.Audit.OK {
		t.Fatalf("workflow binding audit should report reference issues: %#v", report)
	}
	codes := map[string]bool{}
	for _, issue := range report.Audit.Issues {
		codes[issue.Code] = true
	}
	if !codes["workflow-binding-node-missing"] || !codes["workflow-binding-case-missing"] {
		t.Fatalf("workflow binding audit issue codes = %#v", codes)
	}
}

func TestWorkflowBindingRegisterRejectsUnexpectedPositionalArgs(t *testing.T) {
	profileDir := writeWorkflowBatchReportProfile(t)
	storePath := filepath.Join(t.TempDir(), "workflow-binding-positional.sqlite")
	storeRef := "sqlite://" + storePath
	runCLI(t, "config", "publish", "--from", profileDir, "--store", storeRef)
	runCLI(t, "workflow", "register", "--store", storeRef, "--id", "workflow.positional", "--json")

	out := runCLIFails(t,
		"workflow", "binding", "register",
		"--store", storeRef,
		"--workflow", "workflow.positional",
		"--step", "smoke",
		"--node", "node.first",
		"--required", "false",
		"--json",
	)
	if !strings.Contains(out, "unexpected workflow binding arguments: false") {
		t.Fatalf("workflow binding positional bool output = %q", out)
	}
	discoverOut := runCLI(t, "workflow", "discover", "--store", storeRef, "--filter", "workflow.positional", "--json")
	if strings.Contains(discoverOut, `"stepCount": 1`) {
		t.Fatalf("positional bool failure should not mutate workflow bindings: %s", discoverOut)
	}
}
