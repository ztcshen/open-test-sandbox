package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"open-test-sandbox/internal/profile"
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
