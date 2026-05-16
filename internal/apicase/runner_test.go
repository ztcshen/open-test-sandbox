package apicase_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"open-test-sandbox/internal/apicase"
)

func TestRunWritesEvidenceBundle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	casePath := filepath.Join(t.TempDir(), "case.json")
	writeCaseFile(t, casePath)
	evidenceDir := filepath.Join(t.TempDir(), "evidence")

	result, err := apicase.Run(context.Background(), apicase.RunOptions{
		CasePath:    casePath,
		EvidenceDir: evidenceDir,
		RunID:       "run-001",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("run api case: %v", err)
	}
	if result.Status != "passed" || result.RunID != "run-001" || result.EvidencePath == "" {
		t.Fatalf("result = %#v", result)
	}
	if result.StartedAt == "" || result.FinishedAt == "" || result.ElapsedMs < 0 {
		t.Fatalf("result timing was not recorded: %#v", result)
	}

	for _, name := range []string{"case.json", "request.json", "response.json", "assertions.json", "summary.json"} {
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

func TestRunExecutesHTTPCaseAndWritesResponseEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/items" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	casePath := filepath.Join(t.TempDir(), "case.json")
	writeCaseFile(t, casePath)
	evidenceDir := filepath.Join(t.TempDir(), "evidence")

	result, err := apicase.Run(context.Background(), apicase.RunOptions{
		CasePath:    casePath,
		EvidenceDir: evidenceDir,
		RunID:       "run-002",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("run api case: %v", err)
	}
	if result.Status != "passed" {
		t.Fatalf("result = %#v", result)
	}
	for _, name := range []string{"response.json", "assertions.json"} {
		if _, err := os.Stat(filepath.Join(result.EvidencePath, name)); err != nil {
			t.Fatalf("expected evidence file %s: %v", name, err)
		}
	}
	var assertions struct {
		Status string `json:"status"`
	}
	readJSONFile(t, filepath.Join(result.EvidencePath, "assertions.json"), &assertions)
	if assertions.Status != "passed" {
		t.Fatalf("assertions = %#v", assertions)
	}
}

func TestRunAppliesRequestBodyOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	casePath := filepath.Join(t.TempDir(), "case.json")
	writeCaseFile(t, casePath)
	evidenceDir := filepath.Join(t.TempDir(), "evidence")

	result, err := apicase.Run(context.Background(), apicase.RunOptions{
		CasePath:    casePath,
		EvidenceDir: evidenceDir,
		RunID:       "run-override",
		BaseURL:     server.URL,
		Overrides: map[string]any{
			"id":       "item-override",
			"priority": "high",
		},
	})
	if err != nil {
		t.Fatalf("run api case with overrides: %v", err)
	}

	var request struct {
		Body map[string]any `json:"body"`
	}
	readJSONFile(t, filepath.Join(result.EvidencePath, "request.json"), &request)
	if request.Body["id"] != "item-override" || request.Body["priority"] != "high" {
		t.Fatalf("request body overrides = %#v", request.Body)
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
