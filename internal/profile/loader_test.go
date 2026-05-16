package profile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"open-test-sandbox/internal/profile"
)

func TestLoadEmptyProfileBundle(t *testing.T) {
	bundle, err := profile.Load(filepath.Join("..", "..", "profiles", "empty"))
	if err != nil {
		t.Fatalf("load empty profile: %v", err)
	}

	if bundle.ID != "empty" || bundle.DisplayName != "Empty Profile" {
		t.Fatalf("bundle identity = %#v", bundle)
	}
	if bundle.Counts().Workflows != 0 || bundle.Counts().APICases != 0 || bundle.Counts().Fixtures != 0 {
		t.Fatalf("empty bundle counts = %#v", bundle.Counts())
	}
}

func TestLoadProfileReturnsActionableValidationErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), []byte(`{"displayName":"Missing ID"}`), 0o644); err != nil {
		t.Fatalf("write invalid profile: %v", err)
	}

	_, err := profile.Load(dir)
	if err == nil || !strings.Contains(err.Error(), "profile id is required") {
		t.Fatalf("load invalid profile error = %v", err)
	}
}

func TestLoadProfileReadsSplitAssetDirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "split",
  "displayName": "Split Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "services", "service.json"), `{"id":"service.one","displayName":"Service One","kind":"http"}`)
	writeFile(t, filepath.Join(dir, "workflows", "workflow.json"), `{"id":"workflow.one","displayName":"Workflow One"}`)
	writeFile(t, filepath.Join(dir, "interface-nodes", "node.json"), `{"id":"node.one","displayName":"Node One","serviceId":"service.one"}`)
	writeFile(t, filepath.Join(dir, "cases", "case.json"), `{"id":"case.one","displayName":"Case One","nodeId":"node.one","caseType":"success","scenario":"happy path","requestTemplateId":"template.one","patchJson":"[]","renderMode":"template_patch","expectedJson":"{}","requiredForAdmission":true,"status":"active","sortOrder":3}`)
	writeFile(t, filepath.Join(dir, "request-templates", "template.json"), `{"id":"template.one","displayName":"Template One","nodeId":"node.one","method":"GET","path":"/health","templateJson":"{}"}`)
	writeFile(t, filepath.Join(dir, "case-dependencies", "dependency.json"), `{"id":"dependency.one","caseId":"case.one","fixtureId":"fixture.one","mappingsJson":"[]"}`)
	writeFile(t, filepath.Join(dir, "workflow-bindings", "binding.json"), `{"workflowId":"workflow.one","stepId":"step.one","nodeId":"node.one","caseId":"case.one","required":true}`)
	writeFile(t, filepath.Join(dir, "fixtures", "fixture.json"), `{"id":"fixture.one","displayName":"Fixture One","kind":"json"}`)

	bundle, err := profile.Load(dir)
	if err != nil {
		t.Fatalf("load split profile: %v", err)
	}

	counts := bundle.Counts()
	if counts.Services != 1 || counts.Workflows != 1 || counts.InterfaceNodes != 1 || counts.APICases != 1 || counts.RequestTemplates != 1 || counts.CaseDependencies != 1 || counts.WorkflowBindings != 1 || counts.Fixtures != 1 {
		t.Fatalf("split profile counts = %#v", counts)
	}
	if bundle.APICases[0].ID != "case.one" || bundle.RequestTemplates[0].NodeID != "node.one" || !bundle.WorkflowBindings[0].Required {
		t.Fatalf("split profile assets = %#v", bundle)
	}
	if bundle.APICases[0].CaseType != "success" || !bundle.APICases[0].RequiredForAdmission || bundle.APICases[0].SortOrder != 3 {
		t.Fatalf("split profile case metadata = %#v", bundle.APICases[0])
	}
	if bundle.APICases[0].CasePath != filepath.ToSlash(filepath.Join("cases", "case.json")) {
		t.Fatalf("split profile case path = %#v", bundle.APICases[0])
	}
}

func TestLoadProfileMergesCatalogInterfaceCaseMetadata(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{"id":"catalog-cases","displayName":"Catalog Cases"}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "interfaceNodeCases": [
    {"id":"case.one","nodeId":"node.one","title":"Case One From Catalog","caseType":"success","requiredForAdmission":true,"status":"active","sortOrder":9}
  ]
}`)
	writeFile(t, filepath.Join(dir, "cases", "case.one.json"), `{"id":"case.one","nodeId":"node.one","casePath":"cases/case.one.json"}`)

	bundle, err := profile.Load(dir)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if len(bundle.APICases) != 1 {
		t.Fatalf("api cases = %#v", bundle.APICases)
	}
	item := bundle.APICases[0]
	if item.DisplayName != "Case One From Catalog" || item.CaseType != "success" || !item.RequiredForAdmission || item.Status != "active" || item.SortOrder != 9 || item.CasePath == "" {
		t.Fatalf("merged api case = %#v", item)
	}
}

func TestLoadProfileAllowsSplitCaseToDisableCatalogAdmission(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{"id":"catalog-cases","displayName":"Catalog Cases"}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "interfaceNodeCases": [
    {"id":"case.one","nodeId":"node.one","title":"Case One From Catalog","caseType":"failure","requiredForAdmission":true,"status":"active","sortOrder":9}
  ]
}`)
	writeFile(t, filepath.Join(dir, "cases", "case.one.json"), `{"id":"case.one","nodeId":"node.one","requiredForAdmission":false,"casePath":"cases/case.one.json"}`)

	bundle, err := profile.Load(dir)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if len(bundle.APICases) != 1 {
		t.Fatalf("api cases = %#v", bundle.APICases)
	}
	if bundle.APICases[0].RequiredForAdmission {
		t.Fatalf("split case should disable catalog admission: %#v", bundle.APICases[0])
	}
}

func TestLoadProfileReadsCatalogNodeConfigs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "catalog-nodes",
  "displayName": "Catalog Nodes",
  "runtimeEnvFiles": ["runtime.env"]
}`)
	writeFile(t, filepath.Join(dir, "services", "service.json"), `{"id":"service.one","displayName":"Service One","kind":"app"}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "nodeConfigs": [
    {
      "id": "service.one",
      "displayName": "Service One Configured",
      "role": "app",
      "repoEnv": "SERVICE_ONE_REPO",
      "containerName": "sandbox-service-one",
      "dockerService": "service-one",
      "servicePort": 18080,
      "managementPort": 18081,
      "sortOrder": 7
    }
  ]
}`)

	bundle, err := profile.Load(dir)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if bundle.BaseDir != dir || len(bundle.RuntimeEnvFiles) != 1 {
		t.Fatalf("profile paths = %#v", bundle)
	}
	if len(bundle.Services) != 1 {
		t.Fatalf("services = %#v", bundle.Services)
	}
	service := bundle.Services[0]
	if service.DisplayName != "Service One Configured" || service.RepoEnv != "SERVICE_ONE_REPO" || service.ContainerName != "sandbox-service-one" || service.ServicePort != 18080 || service.ManagementPort != 18081 {
		t.Fatalf("service = %#v", service)
	}
}

func TestLoadProfileReadsAgentValidationAndConfigAuthoring(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "agent-ready",
  "displayName": "Agent Ready Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "agent-test-profiles.json"), `{
  "schemaVersion": "1",
  "profiles": [
    {
      "id": "baseline",
      "title": "Baseline chain",
      "steps": [{"type": "workflow", "id": "workflow.baseline"}],
      "probes": [{"name": "row_count", "query": "select count(*) from records"}],
      "evidencePolicy": {"collectTrace": true, "collectLogs": true},
      "configPolicy": {
        "allowedChanges": [{"kind": "env", "key": "SANDBOX_FLAG"}]
      },
      "requiredConfig": [
        {"kind": "setting", "key": "feature.flag", "suggestedValue": "enabled", "reason": "exercise config application"}
      ]
    }
  ]
}`)
	writeFile(t, filepath.Join(dir, "config-authoring.json"), `{
  "schemaVersion": "1",
  "role": "configuration-subagent",
  "summary": "Concrete template configuration is authored by a dedicated subagent.",
  "guidePath": "template-config/SKILL.md",
  "allowedWritePaths": ["template-config/"],
  "allowedReadPaths": ["template-config/SKILL.md"],
  "mainAgentResponsibilities": ["maintain tools", "review friction"],
  "subagentResponsibilities": ["author configuration", "report friction"],
  "handoffRequiredFields": ["changedFiles", "friction"],
  "frictionCategories": ["missing-model-capability", "unclear-document-semantics"]
}`)

	bundle, err := profile.Load(dir)
	if err != nil {
		t.Fatalf("load agent-ready profile: %v", err)
	}

	if len(bundle.AgentTestProfiles) != 1 {
		t.Fatalf("agent test profiles = %#v", bundle.AgentTestProfiles)
	}
	agentProfile := bundle.AgentTestProfiles[0]
	if agentProfile.ID != "baseline" || len(agentProfile.Steps) != 1 || len(agentProfile.ConfigPolicy.AllowedChanges) != 1 || len(agentProfile.RequiredConfig) != 1 {
		t.Fatalf("agent test profile = %#v", agentProfile)
	}
	if bundle.ConfigAuthoring.Role != "configuration-subagent" || len(bundle.ConfigAuthoring.AllowedWritePaths) != 1 || len(bundle.ConfigAuthoring.FrictionCategories) != 2 {
		t.Fatalf("config authoring = %#v", bundle.ConfigAuthoring)
	}
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
