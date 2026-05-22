package executor_test

import (
	"context"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/runner/executor"
	"agent-testbench/internal/store"
)

func TestPlanValidatesExternalToolDescriptors(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		Executors: []profile.ExecutorDescriptor{
			{ID: "executor.command", DisplayName: "No-op command", Kind: "custom-command", Command: "true", Status: "active", ArtifactPaths: []string{"reports/command.json"}},
			{ID: "executor.karate", DisplayName: "Karate API suite", Kind: "karate", SourcePath: "tests/api.feature", Status: "active"},
			{ID: "executor.pytest", DisplayName: "Pytest suite", Kind: "pytest", Status: "active"},
			{ID: "executor.unknown", DisplayName: "Unknown suite", Kind: "unknown", SourcePath: "tests/unknown", Status: "active"},
		},
	}

	report := executor.Plan(context.Background(), bundle)

	if report.OK || report.ProfileID != "sample" || report.Counts.Total != 4 || report.Counts.Ready != 2 || report.Counts.Blocked != 2 {
		t.Fatalf("plan summary = %#v", report)
	}
	byID := map[string]executor.PlanItem{}
	for _, item := range report.Items {
		byID[item.ID] = item
	}
	if !byID["executor.command"].Ready || byID["executor.command"].RunMode != "dry-run" || byID["executor.command"].Command != "true" {
		t.Fatalf("command executor = %#v", byID["executor.command"])
	}
	if !byID["executor.karate"].Ready || byID["executor.karate"].SourcePath != "tests/api.feature" {
		t.Fatalf("karate executor = %#v", byID["executor.karate"])
	}
	if byID["executor.pytest"].Ready || !containsIssue(byID["executor.pytest"].Issues, "missing-source-path") {
		t.Fatalf("pytest executor = %#v", byID["executor.pytest"])
	}
	if byID["executor.unknown"].Ready || !containsIssue(byID["executor.unknown"].Issues, "unsupported-kind") {
		t.Fatalf("unknown executor = %#v", byID["executor.unknown"])
	}
}

func TestPlanFromCatalogDerivesExecutorItemsFromAPICases(t *testing.T) {
	catalog := store.ProfileCatalog{
		ProfileID: "current",
		APICases: []store.CatalogAPICase{
			{ID: "case.karate", DisplayName: "Karate case", SourceKind: "karate", SourcePath: "tests/api.feature", ExecutorID: "executor.karate", Status: "active", TimeoutSeconds: 20},
			{ID: "case.pytest", DisplayName: "Pytest case", SourceKind: "pytest", ExecutorID: "executor.pytest", Status: "active"},
			{ID: "case.http", DisplayName: "HTTP case", CasePath: "cases/http.json", Status: "active"},
			{ID: "case.inactive", DisplayName: "Inactive case", SourceKind: "pytest", SourcePath: "tests/inactive.py", ExecutorID: "executor.inactive", Status: "draft"},
		},
	}

	report := executor.PlanFromCatalog(context.Background(), catalog)

	if report.OK || report.ProfileID != "current" || report.Counts.Total != 4 || report.Counts.Ready != 2 || report.Counts.Blocked != 2 {
		t.Fatalf("catalog plan summary = %#v", report)
	}
	byID := map[string]executor.PlanItem{}
	for _, item := range report.Items {
		byID[item.ID] = item
	}
	if !byID["executor.karate"].Ready || byID["executor.karate"].Kind != "karate" || byID["executor.karate"].SourcePath != "tests/api.feature" || byID["executor.karate"].TimeoutSeconds != 20 {
		t.Fatalf("karate catalog executor = %#v", byID["executor.karate"])
	}
	if byID["executor.pytest"].Ready || !containsIssue(byID["executor.pytest"].Issues, "missing-source-path") {
		t.Fatalf("pytest catalog executor = %#v", byID["executor.pytest"])
	}
	if !byID["case:case.http"].Ready || byID["case:case.http"].Kind != "http-case" || byID["case:case.http"].SourcePath != "cases/http.json" {
		t.Fatalf("http catalog executor = %#v", byID["case:case.http"])
	}
	if byID["executor.inactive"].Ready || !containsIssue(byID["executor.inactive"].Issues, "inactive") {
		t.Fatalf("inactive catalog executor = %#v", byID["executor.inactive"])
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("catalog plan warnings = %#v", report.Warnings)
	}
}

func containsIssue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
