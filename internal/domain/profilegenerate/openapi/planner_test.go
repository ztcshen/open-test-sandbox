package openapi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"open-test-sandbox/internal/domain/profilegenerate/openapi"
)

func TestPlanGeneratesDraftNegativeCasesFromRequiredSchemaFields(t *testing.T) {
	spec := []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Catalog API"},
  "paths": {
    "/items": {
      "post": {
        "operationId": "createItem",
        "summary": "Create item",
        "tags": ["catalog"],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["id", "name"],
                "properties": {
                  "id": {"type": "string", "example": "item-001"},
                  "name": {"type": "string", "example": "Example Item"}
                }
              }
            }
          }
        },
        "responses": {
          "201": {"description": "Created"},
          "400": {"description": "Bad request"}
        }
      }
    }
  }
}`)

	plan, err := openapi.Plan(spec, openapi.Options{ServiceID: "service.catalog", EvidenceDir: ".runtime/generated"})
	if err != nil {
		t.Fatalf("plan schema generation: %v", err)
	}
	if !plan.OK || plan.Service.ID != "service.catalog" || plan.Service.Status != "draft" {
		t.Fatalf("plan summary = %#v", plan)
	}
	if len(plan.Candidates) != 2 || len(plan.APICases) != 2 || len(plan.CaseFiles) != 2 {
		t.Fatalf("plan counts candidates:%d cases:%d files:%d", len(plan.Candidates), len(plan.APICases), len(plan.CaseFiles))
	}
	first := plan.Candidates[0]
	if first.ID != "candidate.service.catalog.create-item.missing-id" || first.Kind != "missing-required-field" || first.Field != "id" || first.Reason == "" {
		t.Fatalf("first candidate = %#v", first)
	}
	apiCase := plan.APICases[0]
	if apiCase.ID != "case.service.catalog.create-item.missing-id" || apiCase.Status != "draft" || apiCase.CasePath != "api-cases/case.service.catalog.create-item.missing-id.json" || apiCase.EvidenceDir != ".runtime/generated" {
		t.Fatalf("api case = %#v", apiCase)
	}
	if strings.Join(apiCase.Tags, ",") != "generated,schema,negative,catalog" {
		t.Fatalf("api case tags = %#v", apiCase.Tags)
	}
	var runnable struct {
		Request struct {
			Method string         `json:"method"`
			Path   string         `json:"path"`
			Body   map[string]any `json:"body"`
		} `json:"request"`
		Assertions struct {
			ExpectedStatusCodes []int `json:"expectedStatusCodes"`
		} `json:"assertions"`
	}
	if err := json.Unmarshal(plan.CaseFiles[0].Body, &runnable); err != nil {
		t.Fatalf("decode generated case file: %v", err)
	}
	if runnable.Request.Method != "POST" || runnable.Request.Path != "/items" {
		t.Fatalf("runnable request = %#v", runnable.Request)
	}
	if _, ok := runnable.Request.Body["id"]; ok || runnable.Request.Body["name"] != "Example Item" {
		t.Fatalf("runnable body should omit id only: %#v", runnable.Request.Body)
	}
	if len(runnable.Assertions.ExpectedStatusCodes) != 1 || runnable.Assertions.ExpectedStatusCodes[0] != 400 {
		t.Fatalf("expected negative status assertion = %#v", runnable.Assertions)
	}
	if len(plan.Warnings) == 0 || !strings.Contains(plan.Warnings[0], "draft") {
		t.Fatalf("warnings = %#v", plan.Warnings)
	}
}

func TestPlanWarnsWhenNoGeneratableSchemasExist(t *testing.T) {
	spec := []byte(`{"openapi":"3.0.3","info":{"title":"Empty"},"paths":{"/ping":{"get":{"responses":{"200":{"description":"OK"}}}}}}`)

	plan, err := openapi.Plan(spec, openapi.Options{})
	if err != nil {
		t.Fatalf("plan empty schema: %v", err)
	}
	if plan.OK || len(plan.Candidates) != 0 || len(plan.Warnings) == 0 {
		t.Fatalf("empty schema plan = %#v", plan)
	}
}
