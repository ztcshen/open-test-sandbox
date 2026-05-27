package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestShouldSkipPathExcludesGeneratedAndRuntimeFiles(t *testing.T) {
	cfg := DefaultConfig()
	cases := []string{
		".runtime/tmp/app.go",
		"vendor/example/app.go",
		"internal/generated/model.go",
		"internal/server/foo.pb.go",
		"internal/server/foo.pb.gw.go",
		"internal/server/foo.gen.go",
		"internal/server/foo_mock.go",
		"internal/server/wire_gen.go",
		"control-plane/static/assets/react/generated.go",
	}
	for _, path := range cases {
		if !shouldSkipPath(filepath.FromSlash(path), cfg) {
			t.Fatalf("expected %s to be skipped", path)
		}
	}

	if shouldSkipPath(filepath.FromSlash("internal/domain/profile/loader.go"), cfg) {
		t.Fatal("first-party Go source should not be skipped")
	}
}

func TestAnalyzeGoFileReportsSizeSignals(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "domain", "sample", "sample.go")
	writeFile(t, path, `package sample

type Oversized struct {
	A00 string
	A01 string
	A02 string
	A03 string
	A04 string
	A05 string
	A06 string
	A07 string
	A08 string
	A09 string
	A10 string
	A11 string
	A12 string
	A13 string
	A14 string
	A15 string
	A16 string
	A17 string
	A18 string
	A19 string
	A20 string
	A21 string
	A22 string
	A23 string
	A24 string
	A25 string
}

type Wide interface {
	M00()
	M01()
	M02()
	M03()
	M04()
	M05()
	M06()
	M07()
	M08()
	M09()
	M10()
}

func longEnough() {
`+strings.Repeat("\tprintln(\"x\")\n", 61)+`}
`)

	report, err := Analyze(Options{Root: root, ReportDir: filepath.Join(root, "reports")})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	assertIssue(t, report, "function-lines")
	assertNoIssue(t, report, "struct-fields")
	assertNoIssue(t, report, "interface-methods")
}

func TestAnalyzeGoFileKeepsLineOnlyFunctionBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "domain", "sample", "sparse.go")
	writeFile(t, path, `package sample

func sparseButBlocked() {
`+strings.Repeat("\n", 101)+`}
`)

	report, err := Analyze(Options{Root: root, ReportDir: filepath.Join(root, "reports")})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	assertIssueSeverity(t, report, "function-lines", SeverityBlock)
}

func TestAnalyzeGoFileKeepsLineOnlyFunctionWarning(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "domain", "sample", "sparse.go")
	writeFile(t, path, `package sample

func sparseButLong() {
`+strings.Repeat("\n", 62)+`}
`)

	report, err := Analyze(Options{Root: root, ReportDir: filepath.Join(root, "reports")})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	assertIssueSeverity(t, report, "function-lines", SeverityWarning)
}

func TestAnalyzeGoFileKeepsPackageBudgetWarnings(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 21; i++ {
		path := filepath.Join(root, "internal", "domain", "sample", "file_"+strconv.Itoa(i)+".go")
		writeFile(t, path, "package sample\n\nconst Value = 1\n")
	}

	report, err := Analyze(Options{Root: root, ReportDir: filepath.Join(root, "reports")})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	assertIssue(t, report, "package-file-count")
}

func TestAnalyzeGoFileReportsVeryWideStructs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "domain", "sample", "wide.go")
	writeFile(t, path, `package sample

type VeryWide struct {
`+strings.Repeat("\tField string\n", 41)+`}
`)

	report, err := Analyze(Options{Root: root, ReportDir: filepath.Join(root, "reports")})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	assertIssue(t, report, "struct-fields")
}

func TestAnalyzeGoFileReportsVeryWideInterfaces(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "store", "sample", "wide.go")
	writeFile(t, path, `package sample

type VeryWide interface {
`+strings.Repeat("\tMethod()\n", 21)+`}
`)

	report, err := Analyze(Options{Root: root, ReportDir: filepath.Join(root, "reports")})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	assertIssue(t, report, "interface-methods")
}

func TestParseJSCPDReportClassifiesCoreDuplicates(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "jscpd-report.json")
	raw := map[string]any{
		"statistics": map[string]any{
			"total": map[string]any{
				"percentage":      9.2,
				"duplicatedLines": float64(55),
				"clones":          float64(1),
			},
		},
		"duplicates": []map[string]any{
			{
				"lines":    float64(45),
				"fragment": "if err != nil {\n return err\n}\nvalidate input\nbuild request\ncall client\nhandle error\nconvert response\nreturn result",
				"firstFile": map[string]any{
					"name":  "internal/server/controlplane/a.go",
					"start": float64(10),
					"end":   float64(55),
				},
				"secondFile": map[string]any{
					"name":  "internal/server/controlplane/b.go",
					"start": float64(20),
					"end":   float64(65),
				},
			},
		},
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, reportPath, string(data))

	report := Report{}
	if err := addJSCPDIssues(reportPath, &report, DefaultConfig()); err != nil {
		t.Fatalf("parse jscpd: %v", err)
	}

	assertIssue(t, report, "duplicate-total")
	assertIssue(t, report, "duplicate-block")
	if report.Issues[1].DuplicateKind != "workflow skeleton / error handling / validation / remote call wrapper" {
		t.Fatalf("unexpected duplicate classification: %q", report.Issues[1].DuplicateKind)
	}
}

func TestMissingJSCPDReportIsANoteNotFailure(t *testing.T) {
	report := Report{}
	err := addJSCPDIssues(filepath.Join(t.TempDir(), "missing.json"), &report, DefaultConfig())
	if err != nil {
		t.Fatalf("missing jscpd report should not fail scoped non-Go gates: %v", err)
	}
	if len(report.ToolNotes) != 1 || !strings.Contains(report.ToolNotes[0], "no Go files") {
		t.Fatalf("unexpected tool notes: %#v", report.ToolNotes)
	}
}

func TestImportBoundaryFlagsDomainToStoreDependency(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module agent-testbench\n\ngo 1.26\n")
	path := filepath.Join(root, "internal", "domain", "sample", "bad.go")
	writeFile(t, path, `package sample

import "agent-testbench/internal/store/sqlstore"

var _ = sqlstore.Config{}
`)

	report, err := Analyze(Options{Root: root, ReportDir: filepath.Join(root, "reports")})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	assertIssue(t, report, "architecture-boundary")
}

func TestGitSafetyWarnsOnRiskyDeletionRatio(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,7 +1,3 @@
 package sample
-func one() {}
-func two() {}
-func three() {}
-func four() {}
+func one() {}
`
	report := Report{}
	addGitSafetyIssues(diff, &report, DefaultConfig())
	assertIssue(t, report, "ai-safety-deletion-ratio")
}

func TestGitSafetyTreatsBalancedLargeDeletionAsRefactor(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package sample
` + strings.Repeat("-oldStep()\n", 101) + strings.Repeat("+newStep()\n", 120)
	report := Report{}
	addGitSafetyIssues(diff, &report, DefaultConfig())
	assertNoIssue(t, report, "ai-safety-large-deletion")
}

func TestGitSafetyDuplicateTaskStillBlocksLargeDeletion(t *testing.T) {
	t.Setenv("QUALITY_GATE_TASK_KIND", "duplicate cleanup")
	diff := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package sample
` + strings.Repeat("-oldStep()\n", 101) + strings.Repeat("+newStep()\n", 120)
	report := Report{}
	addGitSafetyIssues(diff, &report, DefaultConfig())
	assertIssueSeverity(t, report, "ai-safety-large-deletion", SeverityBlock)
}

func TestGitSafetyIgnoresMovedErrorHandling(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,6 +1,6 @@
 package sample
-if err != nil {
-	return err
-}
+if err != nil {
+	return err
+}
`
	report := Report{}
	addGitSafetyIssues(diff, &report, DefaultConfig())
	assertNoIssue(t, report, "ai-safety-error-handling-delete")
}

func TestGitSafetyWarnsOnRemovedErrorHandling(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,6 +1,4 @@
 package sample
-if err != nil {
-	return err
-}
+return nil
`
	report := Report{}
	addGitSafetyIssues(diff, &report, DefaultConfig())
	assertIssue(t, report, "ai-safety-error-handling-delete")
}

func TestGitSafetyCountsDuplicateErrorHandlingRemovals(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,10 +1,7 @@
 package sample
-if err != nil {
-	return err
-}
-if err != nil {
-	return err
-}
+if err != nil {
+	return err
+}
`
	report := Report{}
	addGitSafetyIssues(diff, &report, DefaultConfig())
	assertIssue(t, report, "ai-safety-error-handling-delete")
}

func TestGitSafetyIgnoresPublicSymbolsInsideTests(t *testing.T) {
	diff := `diff --git a/sample_test.go b/sample_test.go
index 1..2 100644
--- a/sample_test.go
+++ b/sample_test.go
@@ -1,3 +1,7 @@
 package sample
+type Fixture struct{}
+func TestFixture(t *testing.T) {}
`
	report := Report{}
	addGitSafetyIssues(diff, &report, DefaultConfig())
	assertNoIssue(t, report, "ai-safety-public-api")
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func assertIssue(t *testing.T, report Report, id string) {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.ID == id {
			return
		}
	}
	t.Fatalf("missing issue %q in %#v", id, report.Issues)
}

func assertIssueSeverity(t *testing.T, report Report, id string, severity string) {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.ID == id {
			if issue.Severity != severity {
				t.Fatalf("issue %q severity = %q, want %q", id, issue.Severity, severity)
			}
			return
		}
	}
	t.Fatalf("missing issue %q in %#v", id, report.Issues)
}

func assertNoIssue(t *testing.T, report Report, id string) {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.ID == id {
			t.Fatalf("unexpected issue %q in %#v", id, report.Issues)
		}
	}
}
