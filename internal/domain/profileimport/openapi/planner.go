package openapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/runner/apicase"
)

type Options struct {
	ServiceID   string
	EvidenceDir string
}

type PlanResult struct {
	Service          profile.Service           `json:"service"`
	InterfaceNodes   []profile.InterfaceNode   `json:"interfaceNodes"`
	RequestTemplates []profile.RequestTemplate `json:"requestTemplates"`
	APICases         []profile.APICase         `json:"apiCases"`
	CaseFiles        []GeneratedCaseFile       `json:"caseFiles"`
}

type GeneratedCaseFile struct {
	Path string          `json:"path"`
	Body json.RawMessage `json:"body"`
}

type document struct {
	OpenAPI string                    `json:"openapi"`
	Info    info                      `json:"info"`
	Paths   map[string]pathOperations `json:"paths"`
}

type info struct {
	Title string `json:"title"`
}

type pathOperations map[string]operation

type operation struct {
	OperationID string              `json:"operationId"`
	Summary     string              `json:"summary"`
	Description string              `json:"description"`
	Tags        []string            `json:"tags"`
	RequestBody requestBody         `json:"requestBody"`
	Responses   map[string]response `json:"responses"`
}

type requestBody struct {
	Content map[string]mediaType `json:"content"`
}

type mediaType struct {
	Example any `json:"example"`
}

type response struct {
	Description string `json:"description"`
}

func Plan(raw []byte, options Options) (PlanResult, error) {
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return PlanResult{}, fmt.Errorf("decode openapi document: %w", err)
	}
	if strings.TrimSpace(doc.OpenAPI) == "" {
		return PlanResult{}, errors.New("openapi version is required")
	}
	if len(doc.Paths) == 0 {
		return PlanResult{}, errors.New("openapi paths are required")
	}
	title := strings.TrimSpace(doc.Info.Title)
	if title == "" {
		title = "OpenAPI Service"
	}
	serviceID := strings.TrimSpace(options.ServiceID)
	if serviceID == "" {
		serviceID = "service." + slug(title)
	}
	result := PlanResult{
		Service: profile.Service{
			ID:          serviceID,
			DisplayName: title,
			Kind:        "http",
			Status:      "draft",
		},
	}
	for _, path := range sortedKeys(doc.Paths) {
		ops := doc.Paths[path]
		for _, method := range sortedHTTPMethods(ops) {
			op := ops[method]
			opSlug := operationSlug(method, path, op)
			nodeID := "node." + serviceID + "." + opSlug
			caseID := "case." + serviceID + "." + opSlug
			templateID := "template." + serviceID + "." + opSlug
			displayName := firstNonEmpty(op.Summary, op.OperationID, strings.ToUpper(method)+" "+path)
			statusCode := firstSuccessStatus(op.Responses)
			if statusCode == 0 {
				statusCode = 200
			}
			body := jsonExampleBody(op)

			result.InterfaceNodes = append(result.InterfaceNodes, profile.InterfaceNode{
				ID:          nodeID,
				DisplayName: displayName,
				ServiceID:   serviceID,
				Operation:   firstNonEmpty(op.OperationID, displayName),
				Method:      strings.ToUpper(method),
				Path:        path,
				Status:      "draft",
				Tags:        compactTags(op.Tags),
				Description: op.Description,
				SortOrder:   len(result.InterfaceNodes) + 1,
			})
			result.RequestTemplates = append(result.RequestTemplates, profile.RequestTemplate{
				ID:           templateID,
				DisplayName:  displayName,
				NodeID:       nodeID,
				Method:       strings.ToUpper(method),
				Path:         path,
				TemplateJSON: compactJSON(map[string]any{"method": strings.ToUpper(method), "path": path, "body": body}),
			})
			casePath := "api-cases/" + caseID + ".json"
			result.APICases = append(result.APICases, profile.APICase{
				ID:                caseID,
				DisplayName:       displayName,
				Description:       "Draft case generated from OpenAPI import plan.",
				NodeID:            nodeID,
				RequestTemplateID: templateID,
				Tags:              compactTags(append([]string{"openapi"}, op.Tags...)),
				Status:            "draft",
				CasePath:          casePath,
				EvidenceDir:       strings.TrimSpace(options.EvidenceDir),
				SortOrder:         len(result.APICases) + 1,
			})
			result.CaseFiles = append(result.CaseFiles, GeneratedCaseFile{
				Path: casePath,
				Body: runnableCaseBody(caseID, displayName, method, path, body, statusCode),
			})
		}
	}
	return result, nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedHTTPMethods(values pathOperations) []string {
	allowed := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true, "head": true, "options": true}
	methods := []string{}
	for method := range values {
		method = strings.ToLower(strings.TrimSpace(method))
		if allowed[method] {
			methods = append(methods, method)
		}
	}
	sort.SliceStable(methods, func(i, j int) bool {
		return methodRank(methods[i]) < methodRank(methods[j])
	})
	return methods
}

func methodRank(method string) int {
	order := map[string]int{"get": 10, "post": 20, "put": 30, "patch": 40, "delete": 50, "head": 60, "options": 70}
	if rank, ok := order[method]; ok {
		return rank
	}
	return 100
}

func operationSlug(method string, path string, op operation) string {
	if strings.TrimSpace(op.OperationID) != "" {
		return slug(op.OperationID)
	}
	return slug(strings.ToLower(method) + "-" + path)
}

func firstSuccessStatus(responses map[string]response) int {
	codes := []int{}
	for code := range responses {
		parsed, err := strconv.Atoi(code)
		if err == nil && parsed >= 200 && parsed < 300 {
			codes = append(codes, parsed)
		}
	}
	sort.Ints(codes)
	if len(codes) == 0 {
		return 0
	}
	return codes[0]
}

func jsonExampleBody(op operation) map[string]any {
	if op.RequestBody.Content == nil {
		return nil
	}
	for _, contentType := range []string{"application/json"} {
		if media, ok := op.RequestBody.Content[contentType]; ok {
			if body, ok := media.Example.(map[string]any); ok {
				return body
			}
		}
	}
	return nil
}

func runnableCaseBody(caseID string, title string, method string, path string, body map[string]any, statusCode int) json.RawMessage {
	item := apicase.Case{
		ID:    caseID,
		Title: title,
		Request: apicase.Request{
			Method: strings.ToUpper(method),
			Path:   path,
		},
		Assertions: apicase.Assertions{
			ExpectedStatusCodes: []int{statusCode},
		},
	}
	if len(body) > 0 {
		item.Request.Headers = map[string]string{"Content-Type": "application/json"}
		item.Request.Body = body
	}
	raw, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(append(raw, '\n'))
}

func compactTags(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func compactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func slug(value string) string {
	value = splitCamelCase(value)
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "item"
	}
	return value
}

func splitCamelCase(value string) string {
	var builder strings.Builder
	var previous rune
	for index, ch := range value {
		if index > 0 && previous >= 'a' && previous <= 'z' && ch >= 'A' && ch <= 'Z' {
			builder.WriteByte('-')
		}
		builder.WriteRune(ch)
		previous = ch
	}
	return builder.String()
}
