package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) string {
	return runCLIWithExpectation(t, nil, false, args...)
}

func runCLIWithEnv(t *testing.T, env []string, args ...string) string {
	return runCLIWithExpectation(t, env, false, args...)
}

func runCLIFailsWithEnv(t *testing.T, env []string, args ...string) string {
	return runCLIWithExpectation(t, env, true, args...)
}

func runCLIFails(t *testing.T, args ...string) string {
	return runCLIWithExpectation(t, nil, true, args...)
}

func runCLIWithExpectation(t *testing.T, env []string, wantErr bool, args ...string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(append(os.Environ(), env...), "AGENT_TESTBENCH_TEST_CLI=1")
	out, err := cmd.CombinedOutput()
	if wantErr && err == nil {
		t.Fatalf("agent-testbench %s unexpectedly succeeded:\n%s", strings.Join(args, " "), out)
	}
	if !wantErr && err != nil {
		t.Fatalf("agent-testbench %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func runStoreCommand(t *testing.T, args ...string) string {
	return runStoreCommandWithExpectation(t, false, args...)
}

func runStoreCommandFails(t *testing.T, args ...string) string {
	return runStoreCommandWithExpectation(t, true, args...)
}

func runStoreCommandWithExpectation(t *testing.T, wantErr bool, args ...string) string {
	t.Helper()
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = writePipe
	runErr := runStore(context.Background(), args)
	if closeErr := writePipe.Close(); closeErr != nil {
		t.Fatalf("close stdout pipe: %v", closeErr)
	}
	os.Stdout = originalStdout
	out, readErr := io.ReadAll(readPipe)
	if readErr != nil {
		t.Fatalf("read stdout pipe: %v", readErr)
	}
	if wantErr && runErr == nil {
		t.Fatalf("store %s unexpectedly succeeded:\n%s", strings.Join(args, " "), out)
	}
	if !wantErr && runErr != nil {
		t.Fatalf("store %s failed: %v\n%s", strings.Join(args, " "), runErr, out)
	}
	return string(out)
}
