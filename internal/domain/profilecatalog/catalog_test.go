package profilecatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

func TestFromBundleResolvesServiceSourcePathFromRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "sources", "service-one", "main-4e8d26674209")
	writeTestFile(t, filepath.Join(dir, "runtime.env"), "DOCKER_SERVICE_ONE_REPO='"+sourcePath+"'\n")
	bundle := profile.Bundle{
		ID:              "sample",
		BaseDir:         dir,
		RuntimeEnvFiles: []string{"runtime.env"},
		Services: []profile.Service{{
			ID:      "service-one",
			Kind:    "app",
			RepoEnv: "SERVICE_ONE_REPO",
		}},
	}

	catalog := FromBundle(bundle, time.Now().UTC())

	if len(catalog.Services) != 1 || catalog.Services[0].SourcePath != sourcePath {
		t.Fatalf("catalog services = %#v", catalog.Services)
	}
	if roundTrip := ToBundle(catalog); roundTrip.Services[0].SourcePath != sourcePath {
		t.Fatalf("round trip service = %#v", roundTrip.Services[0])
	}
}

func TestCatalogRoundTripPreservesExternalAPICaseSource(t *testing.T) {
	bundle := profile.Bundle{
		ID: "sample",
		APICases: []profile.APICase{
			{
				ID:         "case.karate",
				NodeID:     "node.alpha",
				SourceKind: "karate",
				SourcePath: "tests/api.feature",
				ExecutorID: "executor.karate",
			},
		},
	}

	catalog := FromBundle(bundle, time.Now().UTC())
	if len(catalog.APICases) != 1 || catalog.APICases[0].SourceKind != "karate" || catalog.APICases[0].SourcePath != "tests/api.feature" || catalog.APICases[0].ExecutorID != "executor.karate" {
		t.Fatalf("catalog api case source = %#v", catalog.APICases)
	}
	roundTrip := ToBundle(catalog)
	if len(roundTrip.APICases) != 1 || roundTrip.APICases[0].SourceKind != "karate" || roundTrip.APICases[0].SourcePath != "tests/api.feature" || roundTrip.APICases[0].ExecutorID != "executor.karate" {
		t.Fatalf("round trip api case source = %#v", roundTrip.APICases)
	}
}

func TestInterfaceNodesReadModelMaterializesStaticDirectoryPayload(t *testing.T) {
	catalog := store.ProfileCatalog{
		ProfileID: "sample",
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Create", Method: "POST", Path: "/alpha", Status: "active", TimeoutMs: 2000},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", NodeID: "node.alpha", RequiredForAdmission: true, Status: "active"},
			{ID: "case.beta", NodeID: "node.alpha", RequiredForAdmission: false, Status: "active"},
		},
	}

	model, err := InterfaceNodesReadModel(catalog, "config.sample.001", time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build read model: %v", err)
	}

	var payload struct {
		Source map[string]string `json:"source"`
		Items  []struct {
			ID                string `json:"id"`
			RequiredCaseCount int    `json:"requiredCaseCount"`
			AdmissionStatus   string `json:"admissionStatus"`
			Href              string `json:"href"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode read model payload: %v\n%s", err, model.PayloadJSON)
	}
	if model.ProfileID != "sample" || model.Key != ReadModelInterfaceNodes || model.ConfigVersionID != "config.sample.001" {
		t.Fatalf("read model identity = %#v", model)
	}
	if payload.Source["kind"] != "read-model" || len(payload.Items) != 1 {
		t.Fatalf("read model payload = %#v", payload)
	}
	if payload.Items[0].ID != "node.alpha" || payload.Items[0].RequiredCaseCount != 1 || payload.Items[0].AdmissionStatus != "pending" || payload.Items[0].Href == "" {
		t.Fatalf("read model item = %#v", payload.Items[0])
	}
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
