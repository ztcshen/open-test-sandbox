package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

type caseExecutionResult struct {
	ok            bool
	httpCode      int
	baseURL       string
	failureReason string
	result        map[string]any
}

func executeTestKitCase(ctx context.Context, bundle profile.Bundle, runtime store.Store, item runnableAPICase, payload map[string]any) caseExecutionResult {
	if item.Execution == nil {
		return caseExecutionResult{
			ok:            false,
			failureReason: "api case execution adapter is not configured",
			result: map[string]any{
				"request":  map[string]any{"caseId": item.Case.ID},
				"response": map[string]any{"body": "{}"},
			},
		}
	}
	request, err := buildCaseHTTPRequest(ctx, bundle, runtime, *item.Execution, item.CaseBaseURL, payload)
	if err != nil {
		return failedCaseExecution(item.Case.ID, err.Error())
	}
	if err := applyAPICaseRequestModel(&request, item.Case); err != nil {
		return failedCaseExecution(item.Case.ID, err.Error())
	}
	if request.requiresBody() && request.body == nil {
		return failedCaseExecution(item.Case.ID, fmt.Sprintf("%s caseExecution.body is required for %s; add caseExecution.body or a request template that renders a body", request.method, item.Case.ID))
	}
	httpRequest, err := http.NewRequestWithContext(ctx, request.method, request.fullURL, request.bodyReader())
	if err != nil {
		return failedCaseExecution(item.Case.ID, err.Error())
	}
	for key, value := range request.headers {
		httpRequest.Header.Set(key, value)
	}
	if _, ok := request.headers["Content-Type"]; !ok && request.body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	started := time.Now()
	client := http.Client{Timeout: testKitTimeout(payload)}
	response, err := client.Do(httpRequest)
	if err != nil {
		return failedCaseExecution(item.Case.ID, err.Error())
	}
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	closeErr := response.Body.Close()
	if readErr != nil {
		return failedCaseExecution(item.Case.ID, readErr.Error())
	}
	if closeErr != nil {
		return failedCaseExecution(item.Case.ID, closeErr.Error())
	}
	responseSummary := map[string]any{
		"statusCode": response.StatusCode,
		"headers":    responseHeaders(response.Header),
		"body":       string(responseBody),
		"elapsedMs":  time.Since(started).Milliseconds(),
	}
	passed := expectedHTTPCode(response.StatusCode, request.expectedHTTPCodes)
	failureReason := ""
	if !passed {
		failureReason = fmt.Sprintf("unexpected http status %d", response.StatusCode)
	}
	if passed {
		for _, expected := range request.expectedResponse {
			expected = strings.TrimSpace(expected)
			if expected == "" {
				continue
			}
			if !strings.Contains(string(responseBody), expected) {
				passed = false
				failureReason = fmt.Sprintf("response body missing %q", expected)
				break
			}
		}
	}
	return caseExecutionResult{
		ok:            passed,
		httpCode:      response.StatusCode,
		baseURL:       request.baseURL,
		failureReason: failureReason,
		result: map[string]any{
			"request":  request.summary(),
			"response": responseSummary,
		},
	}
}

type caseHTTPRequest struct {
	method            string
	baseURL           string
	fullURL           string
	path              string
	headers           map[string]string
	auth              map[string]string
	body              any
	expectedHTTPCodes []int
	expectedResponse  []string
	nodeID            string
	signed            bool
}

func (request caseHTTPRequest) requiresBody() bool {
	switch strings.ToUpper(strings.TrimSpace(request.method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

func buildCaseHTTPRequest(ctx context.Context, bundle profile.Bundle, runtime store.Store, execution caseExecutionConfig, caseBaseURL string, payload map[string]any) (caseHTTPRequest, error) {
	baseURL := strings.TrimRight(valueString(payload["baseUrl"]), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(caseBaseURL, "/")
	}
	if baseURL == "" {
		baseURL = catalogServiceBaseURL(ctx, runtime, execution.NodeID)
	}
	if baseURL == "" {
		baseURL = serviceBaseURL(ctx, bundle.Services, execution.NodeID)
	}
	if baseURL == "" {
		return caseHTTPRequest{}, fmt.Errorf("service runtime is not available for %s", execution.NodeID)
	}
	rendered := renderCaseExecution(execution, mapFromAny(payload["overrides"]))
	path := strings.TrimSpace(rendered.Path)
	if path == "" {
		path = "/"
	}
	fullURL, err := joinCaseURL(baseURL, path, rendered.Query)
	if err != nil {
		return caseHTTPRequest{}, err
	}
	request := caseHTTPRequest{
		method:            strings.ToUpper(firstNonEmpty(rendered.Method, "GET")),
		baseURL:           baseURL,
		fullURL:           fullURL,
		path:              path,
		headers:           headerStrings(rendered.Headers),
		auth:              headerStrings(rendered.Auth),
		body:              rendered.Body,
		expectedHTTPCodes: rendered.ExpectedHTTPCodes,
		expectedResponse:  rendered.ExpectedResponse,
		nodeID:            rendered.NodeID,
		signed:            rendered.Signed,
	}
	request.ensureForwardingHeaders()
	if request.signed {
		if err := request.applySigning(); err != nil {
			return caseHTTPRequest{}, err
		}
	}
	return request, nil
}

func serviceBaseURL(ctx context.Context, services []profile.Service, serviceID string) string {
	runtime := dockerRuntimeByService(ctx, services)[serviceID]
	if runtime.Port == 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", runtime.Port)
}

func catalogServiceBaseURL(ctx context.Context, runtime store.Store, serviceID string) string {
	if runtime == nil || strings.TrimSpace(serviceID) == "" {
		return ""
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return ""
	}
	resolvedServiceID := strings.TrimSpace(serviceID)
	for _, node := range catalog.InterfaceNodes {
		if node.ID == serviceID && strings.TrimSpace(node.ServiceID) != "" {
			resolvedServiceID = strings.TrimSpace(node.ServiceID)
			break
		}
	}
	for _, service := range catalog.Services {
		if service.ID != resolvedServiceID {
			continue
		}
		port := service.ServicePort
		if port <= 0 {
			return ""
		}
		return fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	return ""
}

func (request caseHTTPRequest) bodyReader() io.Reader {
	if request.body == nil {
		return nil
	}
	raw, err := json.Marshal(request.body)
	if err != nil {
		raw = []byte("{}")
	}
	return bytes.NewReader(raw)
}

func (request caseHTTPRequest) summary() map[string]any {
	return map[string]any{
		"method":            request.method,
		"baseUrl":           request.baseURL,
		"fullUrl":           request.fullURL,
		"path":              request.path,
		"nodeId":            request.nodeID,
		"headers":           request.headers,
		"body":              request.body,
		"expectedHttpCodes": request.expectedHTTPCodes,
		"signed":            request.signed,
	}
}

func (request *caseHTTPRequest) ensureForwardingHeaders() {
	if request.headers == nil {
		request.headers = map[string]string{}
	}
	if _, ok := request.headers["X-Forwarded-For"]; !ok {
		request.headers["X-Forwarded-For"] = "192.168.1.100"
	}
	if _, ok := request.headers["X-Real-IP"]; !ok {
		request.headers["X-Real-IP"] = "192.168.1.100"
	}
}

func (request *caseHTTPRequest) applySigning() error {
	uri, err := request.requestURI()
	if err != nil {
		return err
	}
	body := ""
	if request.body != nil {
		raw, err := json.Marshal(request.body)
		if err != nil {
			return err
		}
		body = string(raw)
	}
	auth, err := requestSigningAuthorization(request.method, uri, body, request.auth)
	if err != nil {
		return err
	}
	request.headers["Authorization"] = auth
	return nil
}

func (request caseHTTPRequest) requestURI() (string, error) {
	parsed, err := url.Parse(request.fullURL)
	if err != nil {
		return "", err
	}
	if parsed.RequestURI() == "" {
		return "/", nil
	}
	return parsed.RequestURI(), nil
}
