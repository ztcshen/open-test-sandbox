package httpcapture

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/runner/apicase"
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
	Name     string    `json:"name"`
	Captures []capture `json:"captures"`
}

type capture struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Tags     []string `json:"tags"`
	Request  request  `json:"request"`
	Response response `json:"response"`
}

type request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
}

type response struct {
	Status int `json:"status"`
	Body   any `json:"body"`
}

func Plan(raw []byte, options Options) (PlanResult, error) {
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return PlanResult{}, fmt.Errorf("decode http capture document: %w", err)
	}
	if len(doc.Captures) == 0 {
		return PlanResult{}, errors.New("http capture document requires captures")
	}
	title := strings.TrimSpace(doc.Name)
	if title == "" {
		title = "HTTP Capture"
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
	for index, item := range doc.Captures {
		method := strings.ToUpper(strings.TrimSpace(item.Request.Method))
		if method == "" {
			method = "GET"
		}
		path := strings.TrimSpace(item.Request.Path)
		if path == "" {
			path = "/"
		}
		opSlug := captureSlug(index, method, path, item)
		nodeID := "node." + serviceID + "." + opSlug
		caseID := "case." + serviceID + "." + opSlug
		templateID := "template." + serviceID + "." + opSlug
		displayName := firstNonEmpty(item.Name, item.ID, method+" "+path)
		statusCode := item.Response.Status
		if statusCode == 0 {
			statusCode = 200
		}
		tags := compactTags(append([]string{"recorded", "replay"}, item.Tags...))

		result.InterfaceNodes = append(result.InterfaceNodes, profile.InterfaceNode{
			ID:          nodeID,
			DisplayName: displayName,
			ServiceID:   serviceID,
			Operation:   firstNonEmpty(item.ID, displayName),
			Method:      method,
			Path:        path,
			Status:      "draft",
			Tags:        tags,
			Description: "Draft interface generated from recorded HTTP capture.",
			SortOrder:   len(result.InterfaceNodes) + 1,
		})
		result.RequestTemplates = append(result.RequestTemplates, profile.RequestTemplate{
			ID:           templateID,
			DisplayName:  displayName,
			NodeID:       nodeID,
			Method:       method,
			Path:         path,
			TemplateJSON: compactJSON(map[string]any{"method": method, "path": path, "body": item.Request.Body}),
		})
		casePath := "api-cases/" + caseID + ".json"
		result.APICases = append(result.APICases, profile.APICase{
			ID:                caseID,
			DisplayName:       displayName,
			Description:       "Draft replay case generated from recorded HTTP capture.",
			NodeID:            nodeID,
			RequestTemplateID: templateID,
			Tags:              tags,
			Status:            "draft",
			CasePath:          casePath,
			EvidenceDir:       strings.TrimSpace(options.EvidenceDir),
			SortOrder:         len(result.APICases) + 1,
		})
		result.CaseFiles = append(result.CaseFiles, GeneratedCaseFile{
			Path: casePath,
			Body: runnableCaseBody(caseID, displayName, method, path, item.Request.Headers, item.Request.Body, statusCode, item.Response.Body),
		})
	}
	return result, nil
}

func captureSlug(index int, method string, path string, item capture) string {
	if strings.TrimSpace(item.ID) != "" {
		return slug(item.ID)
	}
	if strings.TrimSpace(item.Name) != "" {
		return slug(item.Name)
	}
	return slug(strings.ToLower(method) + "-" + path + "-" + strconv.Itoa(index+1))
}

func runnableCaseBody(caseID string, title string, method string, path string, headers map[string]string, body any, statusCode int, responseBody any) json.RawMessage {
	item := apicase.Case{
		ID:    caseID,
		Title: title,
		Request: apicase.Request{
			Method:  method,
			Path:    path,
			Headers: headers,
			Body:    bodyMap(body),
		},
		Assertions: apicase.Assertions{
			ExpectedStatusCodes: []int{statusCode},
		},
	}
	if responseText := compactJSON(responseBody); responseText != "{}" && responseText != "null" {
		item.Assertions.ResponseContains = []string{responseText}
	}
	raw, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(append(raw, '\n'))
}

func bodyMap(value any) map[string]any {
	if body, ok := value.(map[string]any); ok {
		return body
	}
	return nil
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

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
