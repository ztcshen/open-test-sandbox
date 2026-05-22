package openapi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profileimport/openapi"
)

func TestPlanFromOpenAPIJSONCreatesReviewableProfileAssets(t *testing.T) {
	spec := []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Catalog API"},
  "paths": {
    "/items": {
      "get": {
        "operationId": "listItems",
        "summary": "List items",
        "tags": ["catalog"],
        "responses": {"200": {"description": "OK"}}
      },
      "post": {
        "operationId": "createItem",
        "summary": "Create item",
        "tags": ["catalog", "write"],
        "requestBody": {
          "content": {
            "application/json": {
              "example": {"id": "item-001", "name": "Example Item"}
            }
          }
        },
        "responses": {"201": {"description": "Created"}}
      }
    }
  }
}`)

	plan, err := openapi.Plan(spec, openapi.Options{ServiceID: "service.catalog", EvidenceDir: ".runtime/openapi"})
	if err != nil {
		t.Fatalf("plan openapi: %v", err)
	}

	if plan.Service.ID != "service.catalog" || plan.Service.DisplayName != "Catalog API" || plan.Service.Kind != "http" {
		t.Fatalf("service = %#v", plan.Service)
	}
	if len(plan.InterfaceNodes) != 2 || len(plan.RequestTemplates) != 2 || len(plan.APICases) != 2 || len(plan.CaseFiles) != 2 {
		t.Fatalf("plan counts = nodes:%d templates:%d cases:%d files:%d", len(plan.InterfaceNodes), len(plan.RequestTemplates), len(plan.APICases), len(plan.CaseFiles))
	}

	firstNode := plan.InterfaceNodes[0]
	if firstNode.ID != "node.service.catalog.list-items" || firstNode.ServiceID != "service.catalog" || firstNode.Method != "GET" || firstNode.Path != "/items" || firstNode.Status != "draft" {
		t.Fatalf("first node = %#v", firstNode)
	}
	secondCase := plan.APICases[1]
	if secondCase.ID != "case.service.catalog.create-item" || secondCase.NodeID != "node.service.catalog.create-item" || secondCase.Status != "draft" || secondCase.CasePath != "api-cases/case.service.catalog.create-item.json" || secondCase.EvidenceDir != ".runtime/openapi" {
		t.Fatalf("second case = %#v", secondCase)
	}
	if strings.Join(secondCase.Tags, ",") != "openapi,catalog,write" {
		t.Fatalf("case tags = %#v", secondCase.Tags)
	}

	var runnable struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Request struct {
			Method string         `json:"method"`
			Path   string         `json:"path"`
			Body   map[string]any `json:"body"`
		} `json:"request"`
		Assertions struct {
			ExpectedStatusCodes []int `json:"expectedStatusCodes"`
		} `json:"assertions"`
	}
	if err := json.Unmarshal(plan.CaseFiles[1].Body, &runnable); err != nil {
		t.Fatalf("decode generated case file: %v", err)
	}
	if runnable.ID != secondCase.ID || runnable.Request.Method != "POST" || runnable.Request.Path != "/items" || runnable.Request.Body["id"] != "item-001" {
		t.Fatalf("generated runnable case = %#v", runnable)
	}
	if len(runnable.Assertions.ExpectedStatusCodes) != 1 || runnable.Assertions.ExpectedStatusCodes[0] != 201 {
		t.Fatalf("generated assertions = %#v", runnable.Assertions)
	}
}

func TestPlanRejectsNonOpenAPIDocument(t *testing.T) {
	_, err := openapi.Plan([]byte(`{"info":{"title":"Missing version"}}`), openapi.Options{})
	if err == nil || !strings.Contains(err.Error(), "openapi") {
		t.Fatalf("expected openapi validation error, got %v", err)
	}
}
