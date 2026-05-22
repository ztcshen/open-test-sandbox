package requesttemplate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"agent-testbench/internal/domain/profile"
)

var (
	ErrTemplateNotFound = errors.New("request template not found")
	ErrFixtureNotFound  = errors.New("fixture not found")
)

type Options struct {
	TemplateID string
	FixtureID  string
}

type Request struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

func Render(bundle profile.Bundle, options Options) (Request, error) {
	requestTemplate, ok := findRequestTemplate(bundle, options.TemplateID)
	if !ok {
		return Request{}, fmt.Errorf("%w: %s", ErrTemplateNotFound, options.TemplateID)
	}
	data, err := fixtureData(bundle, options.FixtureID)
	if err != nil {
		return Request{}, err
	}

	path, err := renderText(requestTemplate.Path, data)
	if err != nil {
		return Request{}, fmt.Errorf("render request path: %w", err)
	}
	rendered := Request{
		Method: strings.ToUpper(requestTemplate.Method),
		Path:   path,
	}
	if strings.TrimSpace(requestTemplate.TemplateJSON) == "" {
		return rendered, nil
	}
	body, err := renderText(requestTemplate.TemplateJSON, data)
	if err != nil {
		return Request{}, fmt.Errorf("render request body: %w", err)
	}
	if !json.Valid([]byte(body)) {
		return Request{}, errors.New("rendered request body is not valid JSON")
	}
	rendered.Body = json.RawMessage(body)
	return rendered, nil
}

func findRequestTemplate(bundle profile.Bundle, id string) (profile.RequestTemplate, bool) {
	for _, requestTemplate := range bundle.RequestTemplates {
		if requestTemplate.ID == id {
			return requestTemplate, true
		}
	}
	return profile.RequestTemplate{}, false
}

func fixtureData(bundle profile.Bundle, id string) (map[string]any, error) {
	if strings.TrimSpace(id) == "" {
		return map[string]any{}, nil
	}
	for _, fixture := range bundle.Fixtures {
		if fixture.ID != id {
			continue
		}
		if strings.TrimSpace(fixture.DataJSON) == "" {
			return map[string]any{}, nil
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(fixture.DataJSON), &data); err != nil {
			return nil, fmt.Errorf("decode fixture data %q: %w", id, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrFixtureNotFound, id)
}

func renderText(source string, data map[string]any) (string, error) {
	tmpl, err := template.New("request").Option("missingkey=error").Parse(source)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}
