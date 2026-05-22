package httpcapture_test

import (
	"encoding/json"
	"strings"
	"testing"

	"open-test-sandbox/internal/domain/profileimport/httpcapture"
)

func TestPlanFromHTTPCaptureCreatesDraftReplayCases(t *testing.T) {
	capture := []byte(`{
  "name": "Catalog Traffic",
  "captures": [
    {
      "id": "createItem",
      "name": "Create item from traffic",
      "request": {
        "method": "POST",
        "path": "/items",
        "headers": {"Content-Type": "application/json"},
        "body": {"id": "item-001", "name": "Example"}
      },
      "response": {
        "status": 201,
        "body": {"id": "item-001", "name": "Example"}
      }
    }
  ]
}`)

	plan, err := httpcapture.Plan(capture, httpcapture.Options{ServiceID: "service.catalog", EvidenceDir: ".runtime/replay"})
	if err != nil {
		t.Fatalf("plan http capture: %v", err)
	}
	if plan.Service.ID != "service.catalog" || plan.Service.DisplayName != "Catalog Traffic" || plan.Service.Kind != "http" || plan.Service.Status != "draft" {
		t.Fatalf("service = %#v", plan.Service)
	}
	if len(plan.InterfaceNodes) != 1 || len(plan.APICases) != 1 || len(plan.CaseFiles) != 1 {
		t.Fatalf("plan counts = nodes:%d cases:%d files:%d", len(plan.InterfaceNodes), len(plan.APICases), len(plan.CaseFiles))
	}
	node := plan.InterfaceNodes[0]
	if node.ID != "node.service.catalog.create-item" || node.Method != "POST" || node.Path != "/items" || node.Status != "draft" {
		t.Fatalf("node = %#v", node)
	}
	apiCase := plan.APICases[0]
	if apiCase.ID != "case.service.catalog.create-item" || apiCase.NodeID != node.ID || apiCase.Status != "draft" || apiCase.CasePath != "api-cases/case.service.catalog.create-item.json" || apiCase.EvidenceDir != ".runtime/replay" {
		t.Fatalf("api case = %#v", apiCase)
	}
	if strings.Join(apiCase.Tags, ",") != "recorded,replay" {
		t.Fatalf("case tags = %#v", apiCase.Tags)
	}
	var runnable struct {
		Request struct {
			Method  string            `json:"method"`
			Path    string            `json:"path"`
			Headers map[string]string `json:"headers"`
			Body    map[string]any    `json:"body"`
		} `json:"request"`
		Assertions struct {
			ExpectedStatusCodes []int    `json:"expectedStatusCodes"`
			ResponseContains    []string `json:"responseContains"`
		} `json:"assertions"`
	}
	if err := json.Unmarshal(plan.CaseFiles[0].Body, &runnable); err != nil {
		t.Fatalf("decode runnable case: %v", err)
	}
	if runnable.Request.Method != "POST" || runnable.Request.Path != "/items" || runnable.Request.Headers["Content-Type"] != "application/json" || runnable.Request.Body["id"] != "item-001" {
		t.Fatalf("runnable request = %#v", runnable.Request)
	}
	if len(runnable.Assertions.ExpectedStatusCodes) != 1 || runnable.Assertions.ExpectedStatusCodes[0] != 201 || !strings.Contains(strings.Join(runnable.Assertions.ResponseContains, ","), "item-001") {
		t.Fatalf("runnable assertions = %#v", runnable.Assertions)
	}
}

func TestPlanRejectsCaptureWithoutRecords(t *testing.T) {
	_, err := httpcapture.Plan([]byte(`{"name":"empty","captures":[]}`), httpcapture.Options{})
	if err == nil || !strings.Contains(err.Error(), "captures") {
		t.Fatalf("expected capture validation error, got %v", err)
	}
}
