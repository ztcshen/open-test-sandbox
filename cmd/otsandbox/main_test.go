package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreMigrateAndStatusCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")

	initial := runCLI(t, "store", "status", "--store-url", dbPath)
	if !strings.Contains(initial, "Version: 0") || !strings.Contains(initial, "Pending: 1") {
		t.Fatalf("initial status output = %q", initial)
	}

	migrated := runCLI(t, "store", "migrate", "--store-url", dbPath)
	if !strings.Contains(migrated, "Migrated store to version 1") {
		t.Fatalf("migrate output = %q", migrated)
	}

	current := runCLI(t, "store", "status", "--store-url", dbPath)
	if !strings.Contains(current, "Version: 1") || !strings.Contains(current, "Pending: 0") {
		t.Fatalf("current status output = %q", current)
	}
}

func TestProfileInspectCommand(t *testing.T) {
	out := runCLI(t, "profile", "inspect", "--profile", "../../profiles/empty")
	for _, want := range []string{"Profile: empty", "Display Name: Empty Profile", "Workflows: 0", "API Cases: 0", "Request Templates: 0", "Case Dependencies: 0", "Workflow Bindings: 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile inspect output missing %q: %q", want, out)
		}
	}
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
