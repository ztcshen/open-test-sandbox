package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/store"
)

func TestCaseDiagnoseReportsExpiredLocalEvidenceNextAction(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "diagnosis-expired-evidence.sqlite")
	runtime, err := openStore(ctx, "sqlite://"+storePath)
	if err != nil {
		t.Fatalf("open Store: %v", err)
	}
	defer runtime.Close()
	if _, err := runtime.CreateRun(ctx, store.Run{ID: "run.expired", ProfileID: "sample", Status: store.StatusPassed, EvidenceRoot: filepath.Join(t.TempDir(), "expired"), SummaryJSON: "{}"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := runtime.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.expired.case",
		RunID:                "run.expired",
		CaseID:               "case.expired",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"GET","path":"/expired"}`,
		AssertionSummaryJSON: `{"status":"passed","errorCount":0}`,
	}); err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	missingDir := filepath.Join(t.TempDir(), "gone")
	for _, item := range []struct {
		kind    string
		summary string
	}{
		{kind: "request", summary: `{"method":"GET","path":"/expired"}`},
		{kind: "response", summary: `{"statusCode":200}`},
	} {
		if _, err := runtime.RecordEvidence(ctx, store.EvidenceRecord{
			ID:        "run.expired." + item.kind,
			RunID:     "run.expired",
			CaseRunID: "run.expired.case",
			Kind:      item.kind,
			URI:       filepath.Join(missingDir, item.kind+".json"),
			MediaType: "application/json",
			Summary:   item.summary,
		}); err != nil {
			t.Fatalf("record %s evidence: %v", item.kind, err)
		}
	}

	out := runCLI(t, "case", "diagnose", "--case-run", "run.expired.case", "--store", "sqlite://"+storePath, "--json")
	var report struct {
		OK          bool     `json:"ok"`
		Warnings    []string `json:"warnings"`
		NextActions []string `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode diagnosis json: %v\n%s", err, out)
	}
	joinedWarnings := strings.Join(report.Warnings, "\n")
	if !report.OK || !strings.Contains(joinedWarnings, "local request evidence file is missing") || !strings.Contains(joinedWarnings, "local response evidence file is missing") {
		t.Fatalf("diagnosis warnings = %#v", report)
	}
	if !strings.Contains(strings.Join(report.NextActions, "\n"), "--evidence-dir") {
		t.Fatalf("diagnosis next actions = %#v", report.NextActions)
	}
}

func TestCaseDiagnoseReadsFileURIEvidence(t *testing.T) {
	storePath := seedReadableCaseDiagnosisEvidence(t, func(path string) string { return "file://" + path })

	out := runCLI(t, "case", "diagnose", "--case-run", "run.readable.case", "--store", "sqlite://"+storePath, "--json")
	requireReadableCaseDiagnosisEvidence(t, out, "file URI")
}

func TestCaseDiagnoseResolvesRelativeEvidenceAgainstRunRoot(t *testing.T) {
	storePath := seedReadableCaseDiagnosisEvidence(t, func(path string) string { return filepath.Base(path) })

	out := runCLI(t, "case", "diagnose", "--case-run", "run.readable.case", "--store", "sqlite://"+storePath, "--json")
	requireReadableCaseDiagnosisEvidence(t, out, "relative URI")
}

func seedReadableCaseDiagnosisEvidence(t *testing.T, evidenceURI func(string) string) string {
	t.Helper()
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "diagnosis-readable-evidence.sqlite")
	runtime, err := openStore(ctx, "sqlite://"+storePath)
	if err != nil {
		t.Fatalf("open Store: %v", err)
	}
	defer runtime.Close()
	evidenceDir := t.TempDir()
	responsePath := filepath.Join(evidenceDir, "response.json")
	assertionsPath := filepath.Join(evidenceDir, "assertions.json")
	if err := os.WriteFile(responsePath, []byte(`{"statusCode":409,"body":"{\"ok\":false}"}`), 0o644); err != nil {
		t.Fatalf("write response evidence: %v", err)
	}
	if err := os.WriteFile(assertionsPath, []byte(`{"status":"failed","errors":["expected business success"]}`), 0o644); err != nil {
		t.Fatalf("write assertions evidence: %v", err)
	}
	if _, err := runtime.CreateRun(ctx, store.Run{ID: "run.readable", ProfileID: "sample", Status: store.StatusFailed, EvidenceRoot: evidenceDir, SummaryJSON: "{}"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := runtime.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.readable.case",
		RunID:                "run.readable",
		CaseID:               "case.readable",
		Status:               store.StatusFailed,
		RequestSummaryJSON:   `{"method":"GET","path":"/readable"}`,
		AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
	}); err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	for _, item := range []struct {
		kind string
		path string
	}{
		{kind: "response", path: responsePath},
		{kind: "assertions", path: assertionsPath},
	} {
		if _, err := runtime.RecordEvidence(ctx, store.EvidenceRecord{
			ID:        "run.readable." + item.kind,
			RunID:     "run.readable",
			CaseRunID: "run.readable.case",
			Kind:      item.kind,
			URI:       evidenceURI(item.path),
			MediaType: "application/json",
			Summary:   "{}",
		}); err != nil {
			t.Fatalf("record %s evidence: %v", item.kind, err)
		}
	}
	return storePath
}

func requireReadableCaseDiagnosisEvidence(t *testing.T, out string, label string) {
	t.Helper()
	var report struct {
		Category        string   `json:"category"`
		AssertionErrors []string `json:"assertionErrors"`
		Warnings        []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode diagnosis json: %v\n%s", err, out)
	}
	if report.Category != "assertion-mismatch" || strings.Join(report.AssertionErrors, "\n") != "expected business success" {
		t.Fatalf("%s diagnosis report = %#v", label, report)
	}
	warnings := strings.Join(report.Warnings, "\n")
	if strings.Contains(warnings, "could not read") || strings.Contains(warnings, "evidence file is missing") {
		t.Fatalf("%s evidence should be readable without warnings: %#v", label, report.Warnings)
	}
}
