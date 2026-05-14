package apicase_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"open-test-sandbox/internal/apicase"
)

func TestDryRunWritesEvidenceBundle(t *testing.T) {
	casePath := filepath.Join(t.TempDir(), "case.json")
	writeCaseFile(t, casePath)
	evidenceDir := filepath.Join(t.TempDir(), "evidence")

	result, err := apicase.Run(context.Background(), apicase.RunOptions{
		CasePath:    casePath,
		EvidenceDir: evidenceDir,
		DryRun:      true,
		RunID:       "run-001",
	})
	if err != nil {
		t.Fatalf("run api case dry-run: %v", err)
	}
	if result.Status != "passed" || result.RunID != "run-001" || result.EvidencePath == "" {
		t.Fatalf("result = %#v", result)
	}

	for _, name := range []string{"case.json", "request.json", "summary.json"} {
		if _, err := os.Stat(filepath.Join(result.EvidencePath, name)); err != nil {
			t.Fatalf("expected evidence file %s: %v", name, err)
		}
	}

	var request struct {
		Method string         `json:"method"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	}
	readJSONFile(t, filepath.Join(result.EvidencePath, "request.json"), &request)
	if request.Method != "POST" || request.Path != "/v1/items" || request.Body["id"] != "item-001" {
		t.Fatalf("request = %#v", request)
	}
}

func writeCaseFile(t *testing.T, path string) {
	t.Helper()
	raw := []byte(`{
  "id": "case.alpha",
  "title": "Create Item",
  "request": {
    "method": "POST",
    "path": "/v1/items",
    "headers": {"Content-Type": "application/json"},
    "body": {"id": "item-001"}
  },
  "assertions": {
    "expectedStatusCodes": [200, 201],
    "responseContains": ["created"]
  }
}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write case file: %v", err)
	}
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}
