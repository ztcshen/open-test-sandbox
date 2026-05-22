package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
)

func TestSourceSnapshotRevision(t *testing.T) {
	branch, commit := sourceSnapshotRevision("/tmp/runtime/service/main-4e8d26674209")
	if branch != "main" || commit != "4e8d26674209" {
		t.Fatalf("snapshot revision = %q %q", branch, commit)
	}
}

func TestSourceSnapshotRevisionAllowsBranchHyphens(t *testing.T) {
	branch, commit := sourceSnapshotRevision("/tmp/runtime/service/release-candidate-4e8d26674209")
	if branch != "release-candidate" || commit != "4e8d26674209" {
		t.Fatalf("snapshot revision = %q %q", branch, commit)
	}
}

func TestSourceSnapshotRevisionIgnoresNonRevisionPaths(t *testing.T) {
	branch, commit := sourceSnapshotRevision("/tmp/runtime/service/current")
	if branch != "" || commit != "" {
		t.Fatalf("non revision path = %q %q", branch, commit)
	}
}

func TestConfiguredRuntimeUsesProfileEnvSnapshot(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "runtime.env")
	if err := os.WriteFile(envPath, []byte("DOCKER_SERVICE_ONE_REPO='"+filepath.Join(dir, "snapshots", "service-one", "main-4e8d26674209")+"'\n"), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	bundle := profile.Bundle{
		ID:              "sample",
		BaseDir:         dir,
		RuntimeEnvFiles: []string{"runtime.env"},
		Services: []profile.Service{{
			ID:            "service-one",
			Kind:          "app",
			RepoEnv:       "SERVICE_ONE_REPO",
			ContainerName: "sandbox-service-one",
			ServicePort:   18080,
		}},
	}
	runtime := configuredRuntimeByService(context.Background(), bundle)["service-one"]
	if runtime.BranchName != "main" || runtime.CommitID != "4e8d26674209" || runtime.SourcePath == "" || runtime.Port != 18080 {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func TestFindRunnableAPICaseResolvesStoreCatalogPathFromProfileIndex(t *testing.T) {
	ctx := context.Background()
	runtime, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer runtime.Close()

	profileDir := t.TempDir()
	casePath := filepath.Join(profileDir, "cases", "case.alpha.json")
	if err := os.MkdirAll(filepath.Dir(casePath), 0o755); err != nil {
		t.Fatalf("mkdir cases: %v", err)
	}
	if err := os.WriteFile(casePath, []byte(`{"id":"case.alpha","request":{"method":"GET","path":"/health"}}`), 0o644); err != nil {
		t.Fatalf("write case: %v", err)
	}
	now := time.Now().UTC()
	if _, err := runtime.UpsertProfileIndex(ctx, store.ProfileIndex{ProfileID: "sample", BundlePath: profileDir, ImportedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert profile index: %v", err)
	}
	if err := runtime.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		APICases:  []store.CatalogAPICase{{ID: "case.alpha", CasePath: "cases/case.alpha.json"}},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	runnable, ok := findRunnableAPICase(ctx, profile.Bundle{}, runtime, "case.alpha", map[string]any{})
	if !ok || runnable.Case.CasePath != casePath {
		t.Fatalf("runnable case = %#v ok=%v", runnable.Case, ok)
	}
}

func TestFindRunnableAPICaseResolvesBundlePathFromProfileIndex(t *testing.T) {
	ctx := context.Background()
	runtime, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer runtime.Close()

	profileDir := t.TempDir()
	casePath := filepath.Join(profileDir, "cases", "case.bundle.json")
	if err := os.MkdirAll(filepath.Dir(casePath), 0o755); err != nil {
		t.Fatalf("mkdir cases: %v", err)
	}
	if err := os.WriteFile(casePath, []byte(`{"id":"case.bundle","request":{"method":"GET","path":"/health"}}`), 0o644); err != nil {
		t.Fatalf("write case: %v", err)
	}
	now := time.Now().UTC()
	if _, err := runtime.UpsertProfileIndex(ctx, store.ProfileIndex{ProfileID: "sample", BundlePath: profileDir, ImportedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert profile index: %v", err)
	}

	runnable, ok := findRunnableAPICase(ctx, profile.Bundle{
		ID:       "sample",
		APICases: []profile.APICase{{ID: "case.bundle", CasePath: "cases/case.bundle.json"}},
	}, runtime, "case.bundle", map[string]any{})
	if !ok || runnable.Case.CasePath != casePath {
		t.Fatalf("runnable case = %#v ok=%v", runnable.Case, ok)
	}
}

func TestAPICaseBatchWorkflowPlansResolveBundlePathFromProfileIndex(t *testing.T) {
	ctx := context.Background()
	runtime, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer runtime.Close()

	profileDir := t.TempDir()
	casePath := filepath.Join(profileDir, "cases", "case.workflow.json")
	if err := os.MkdirAll(filepath.Dir(casePath), 0o755); err != nil {
		t.Fatalf("mkdir cases: %v", err)
	}
	if err := os.WriteFile(casePath, []byte(`{"id":"case.workflow","request":{"method":"GET","path":"/health"}}`), 0o644); err != nil {
		t.Fatalf("write case: %v", err)
	}
	now := time.Now().UTC()
	if _, err := runtime.UpsertProfileIndex(ctx, store.ProfileIndex{ProfileID: "sample", BundlePath: profileDir, ImportedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert profile index: %v", err)
	}

	plans := apiCaseBatchWorkflowPlans(ctx, profile.Bundle{
		ID:             "sample",
		APICases:       []profile.APICase{{ID: "case.workflow", NodeID: "node.workflow", CasePath: "cases/case.workflow.json"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.workflow"}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.sample",
			StepID:     "step.workflow",
			NodeID:     "node.workflow",
			CaseID:     "case.workflow",
		}},
	}, runtime, apiCaseBatchRunRequest{WorkflowID: "workflow.sample"})
	if len(plans) != 1 || plans[0].CasePath != casePath {
		t.Fatalf("plans = %#v", plans)
	}
}
