package requesttemplate_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/runner/requesttemplate"
)

func TestRenderUsesFixtureData(t *testing.T) {
	bundle := profile.Bundle{
		RequestTemplates: []profile.RequestTemplate{
			{
				ID:           "template.create",
				Method:       "POST",
				Path:         "/v1/items/{{.itemId}}",
				TemplateJSON: `{"id":"{{.itemId}}","quantity":{{.quantity}}}`,
			},
		},
		Fixtures: []profile.Fixture{
			{
				ID:       "fixture.item",
				DataJSON: `{"itemId":"item-001","quantity":3}`,
			},
		},
	}

	rendered, err := requesttemplate.Render(bundle, requesttemplate.Options{
		TemplateID: "template.create",
		FixtureID:  "fixture.item",
	})
	if err != nil {
		t.Fatalf("render request template: %v", err)
	}
	if rendered.Method != "POST" || rendered.Path != "/v1/items/item-001" {
		t.Fatalf("rendered request identity = %#v", rendered)
	}
	var body map[string]any
	if err := json.Unmarshal(rendered.Body, &body); err != nil {
		t.Fatalf("decode rendered body: %v", err)
	}
	if body["id"] != "item-001" || body["quantity"].(float64) != 3 {
		t.Fatalf("rendered body = %#v", body)
	}
}

func TestRenderRejectsMissingTemplate(t *testing.T) {
	_, err := requesttemplate.Render(profile.Bundle{}, requesttemplate.Options{TemplateID: "template.missing"})
	if !errors.Is(err, requesttemplate.ErrTemplateNotFound) || !strings.Contains(err.Error(), "template.missing") {
		t.Fatalf("missing template error = %v", err)
	}
}
