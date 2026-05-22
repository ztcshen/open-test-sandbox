package controlplane

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

type runnableAPICase struct {
	Case        profile.APICase
	Execution   *caseExecutionConfig
	CaseBaseURL string
}

type caseExecutionConfig struct {
	Method                string         `json:"method"`
	NodeID                string         `json:"nodeId"`
	Path                  string         `json:"path"`
	Query                 map[string]any `json:"query"`
	Headers               map[string]any `json:"headers"`
	Auth                  map[string]any `json:"auth"`
	Body                  any            `json:"body"`
	ExpectedHTTPCodes     []int          `json:"expectedHttpCodes"`
	ExpectedResponse      []string       `json:"expectedResponseContains"`
	RequireRequestID      bool           `json:"requireRequestId"`
	Signed                bool           `json:"signed"`
	TraceEndpoint         string         `json:"traceEndpoint"`
	TraceCorrelatorFields []string       `json:"traceCorrelatorFields"`
}

type caseExecutionTemplateConfig struct {
	CaseID        string              `json:"caseId"`
	CaseExecution caseExecutionConfig `json:"caseExecution"`
	Exports       []map[string]any    `json:"exports"`
}

var caseSerialCounter uint64

func handleTestKitRun(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store, collector traceCollector) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	result, status := testKitCaseResult(r.Context(), bundle, runtime, payload)
	if status == http.StatusOK {
		runID, err := recordTestKitRun(r, bundle, runtime, payload, result)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		attachCaseRunEvidenceHandles(result, runID)
		if runID != "" {
			if shouldInlineTestKitTraceTopology(payload) {
				collectAndRecordTestKitTraceTopology(r.Context(), runtime, collector, runID, payload, result)
			} else {
				scheduleTestKitTraceTopology(runtime, collector, runID, payload, result)
			}
		}
	}
	writeJSONStatus(w, status, result)
}

func handleTestKitRunBatch(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	caseIDs := testKitCaseIDs(payload["caseIds"])
	if len(caseIDs) == 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "caseIds are required"})
		return
	}

	results := make([]map[string]any, 0, len(caseIDs))
	passed := 0
	started := time.Now()
	for _, caseID := range caseIDs {
		itemPayload := map[string]any{
			"caseId":         caseID,
			"baseUrl":        payload["baseUrl"],
			"timeoutSeconds": payload["timeoutSeconds"],
		}
		result, _ := testKitCaseResult(r.Context(), bundle, runtime, itemPayload)
		runID, err := recordTestKitRunWithContext(r.Context(), bundle, runtime, itemPayload, result)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		attachCaseRunEvidenceHandles(result, runID)
		if result["ok"] == true {
			passed++
		}
		results = append(results, result)
	}
	writeJSON(w, map[string]any{
		"ok":        passed == len(results),
		"results":   results,
		"elapsedMs": time.Since(started).Milliseconds(),
		"summary": map[string]any{
			"caseCount": len(results),
			"passed":    passed,
			"failed":    len(results) - passed,
		},
	})
}

func attachCaseRunEvidenceHandles(result map[string]any, runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	caseRunID := runID + ".case"
	result["runId"] = runID
	result["caseRunId"] = caseRunID
	result["detailUrl"] = "/api/case-run/evidence?caseRunId=" + url.QueryEscape(caseRunID)
	result["viewerUrl"] = "/evidence-viewer.html?caseRun=" + url.QueryEscape(runID)
}

func testKitCaseResult(ctx context.Context, bundle profile.Bundle, runtime store.Store, payload map[string]any) (map[string]any, int) {
	started := time.Now()
	caseID := valueString(payload["caseId"])
	if caseID == "" {
		return map[string]any{"ok": false, "error": "caseId is required", "code": http.StatusBadRequest}, http.StatusBadRequest
	}
	item, ok := findRunnableAPICase(ctx, bundle, runtime, caseID, payload)
	if !ok {
		return map[string]any{
			"ok":     false,
			"caseId": caseID,
			"status": store.StatusFailed,
			"error":  "api case not found",
			"code":   http.StatusNotFound,
		}, http.StatusNotFound
	}

	executionResult := executeTestKitCase(ctx, bundle, runtime, item, payload)
	runOK := executionResult.ok
	status := store.StatusPassed
	if !runOK {
		status = store.StatusFailed
	}
	stepID := valueString(payload["stepId"])
	result := map[string]any{
		"ok":        runOK,
		"caseId":    item.Case.ID,
		"title":     firstNonEmpty(item.Case.DisplayName, item.Case.ID),
		"stepId":    stepID,
		"status":    status,
		"elapsedMs": time.Since(started).Milliseconds(),
		"summary": map[string]any{
			"caseId":        item.Case.ID,
			"stepId":        stepID,
			"failureReason": executionResult.failureReason,
			"httpCode":      executionResult.httpCode,
			"targetBaseUrl": executionResult.baseURL,
		},
		"result": executionResult.result,
	}
	if executionResult.failureReason != "" {
		result["error"] = executionResult.failureReason
	}
	return result, http.StatusOK
}

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
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
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

func requestSigningAuthorization(method string, uri string, body string, auth map[string]string) (string, error) {
	timestamp := time.Now().Unix()
	nonce := requestNonce()
	payload := fmt.Sprintf("%s\n%s\n%d\n%s\n\n%s\n", method, uri, timestamp, nonce, body)
	signature, err := signRequestPayload(payload, auth)
	if err != nil {
		return "", err
	}
	fields := signingHeaderFields(auth)
	fields["nonce_str"] = nonce
	fields["signature"] = signature
	fields["timestamp"] = fmt.Sprintf("%d", timestamp)
	if len(fields) < 3 {
		return "", errors.New("signed case requires auth fields")
	}
	ordered := []string{"credential_id", "mch_id", "nonce_str", "signature", "timestamp", "serial_no"}
	for key := range fields {
		if !stringInList(ordered, key) {
			ordered = append(ordered, key)
		}
	}
	parts := make([]string, 0, len(fields))
	for _, key := range ordered {
		if value := fields[key]; value != "" {
			parts = append(parts, fmt.Sprintf(`%s="%s"`, key, value))
		}
	}
	return "RSA " + strings.Join(parts, ","), nil
}

func signingHeaderFields(auth map[string]string) map[string]string {
	fields := map[string]string{}
	for key, value := range auth {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" || signingSecretKey(key) {
			continue
		}
		fields[snakeCase(key)] = value
	}
	return fields
}

func signingSecretKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "keypath", "pfxpath", "pfxpassword", "scheme":
		return true
	default:
		return false
	}
}

func signRequestPayload(payload string, auth map[string]string) (string, error) {
	keyPath, err := requestSigningKeyPath(auth)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("openssl", "dgst", "-sha256", "-sign", keyPath)
	cmd.Stdin = strings.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("openssl sign failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.ToUpper(fmt.Sprintf("%x", stdout.Bytes())), nil
}

func requestSigningKeyPath(auth map[string]string) (string, error) {
	if keyPath := firstNonEmpty(auth["keyPath"], os.Getenv("SANDBOX_SIGN_KEY_PATH")); keyPath != "" {
		return keyPath, nil
	}
	keyPath := filepath.Join(runtimeProjectRoot(), ".runtime", "control-plane", "request_signing_key.pem")
	if _, err := os.Stat(keyPath); err == nil {
		return keyPath, nil
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return "", err
	}
	pfxPath := firstNonEmpty(auth["pfxPath"], os.Getenv("SANDBOX_SIGN_PFX_PATH"))
	pfxPassword := firstNonEmpty(auth["pfxPassword"], os.Getenv("SANDBOX_SIGN_PFX_PASSWORD"))
	if pfxPath == "" || pfxPassword == "" {
		return "", errors.New("signed case requires auth.keyPath or auth.pfxPath/auth.pfxPassword")
	}
	if _, err := os.Stat(pfxPath); err != nil {
		return "", fmt.Errorf("signing pfx not found: %s", pfxPath)
	}
	var lastErr error
	for _, legacy := range []bool{true, false} {
		args := []string{"pkcs12"}
		if legacy {
			args = append(args, "-legacy")
		}
		args = append(args, "-in", pfxPath, "-nocerts", "-nodes", "-password", "pass:"+pfxPassword)
		pkcs12 := exec.Command("openssl", args...)
		pkey := exec.Command("openssl", "pkey", "-out", keyPath)
		pipe, err := pkcs12.StdoutPipe()
		if err != nil {
			return "", err
		}
		pkey.Stdin = pipe
		var pkcs12Err bytes.Buffer
		var pkeyErr bytes.Buffer
		pkcs12.Stderr = &pkcs12Err
		pkey.Stderr = &pkeyErr
		if err := pkey.Start(); err != nil {
			return "", err
		}
		if err := pkcs12.Start(); err != nil {
			_ = pkey.Wait()
			return "", err
		}
		pkcs12WaitErr := pkcs12.Wait()
		pkeyWaitErr := pkey.Wait()
		if pkcs12WaitErr == nil && pkeyWaitErr == nil {
			_ = os.Chmod(keyPath, 0o600)
			return keyPath, nil
		}
		lastErr = fmt.Errorf("extract key failed legacy=%v pkcs12=%v pkey=%v pkcs12_err=%s pkey_err=%s", legacy, pkcs12WaitErr, pkeyWaitErr, strings.TrimSpace(pkcs12Err.String()), strings.TrimSpace(pkeyErr.String()))
	}
	_ = os.Remove(keyPath)
	return "", lastErr
}

func runtimeProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

func requestNonce() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return serialValue("")[:16]
	}
	for i, value := range raw {
		raw[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(raw)
}

func snakeCase(value string) string {
	var out strings.Builder
	for index, item := range value {
		if item >= 'A' && item <= 'Z' {
			if index > 0 {
				out.WriteByte('_')
			}
			out.WriteRune(item + ('a' - 'A'))
			continue
		}
		if item == '-' || item == ' ' {
			out.WriteByte('_')
			continue
		}
		out.WriteRune(item)
	}
	return out.String()
}

func stringInList(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func joinCaseURL(baseURL string, path string, query map[string]any) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	pathURL, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	parsed = parsed.ResolveReference(pathURL)
	values := parsed.Query()
	for key, raw := range query {
		if value := strings.TrimSpace(valueString(raw)); value != "" {
			values.Set(key, value)
		}
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func renderCaseExecution(execution caseExecutionConfig, overrides map[string]any) caseExecutionConfig {
	rendered := execution
	rendered.Path = valueString(renderCaseExecutionValue(rendered.Path, overrides))
	rendered.Query = mapFromAny(renderCaseExecutionValue(rendered.Query, overrides))
	rendered.Headers = mapFromAny(renderCaseExecutionValue(rendered.Headers, overrides))
	rendered.Auth = mapFromAny(renderCaseExecutionValue(rendered.Auth, overrides))
	rendered.TraceEndpoint = valueString(renderCaseExecutionValue(rendered.TraceEndpoint, overrides))
	rendered.Body = renderCaseExecutionValue(rendered.Body, overrides)
	return rendered
}

func applyAPICaseRequestModel(request *caseHTTPRequest, item profile.APICase) error {
	if request == nil {
		return nil
	}
	if err := applyAPICaseExpectedJSON(request, item.ExpectedJSON); err != nil {
		return err
	}
	if strings.TrimSpace(item.RenderMode) != "template_patch" || strings.TrimSpace(item.PatchJSON) == "" || strings.TrimSpace(item.PatchJSON) == "[]" {
		return nil
	}
	if apiCasePatchTargetsQuery(request.method) {
		return applyAPICaseQueryPatch(request, item)
	}
	nextBody := request.body
	if apiCaseUsesSandboxCallback(request.fullURL) {
		merged, err := mergeAPICasePayloadTemplateModel(nextBody, item.PayloadTemplateJSON)
		if err != nil {
			return fmt.Errorf("merge api case payload template %s: %w", item.ID, err)
		}
		nextBody = merged
	}
	if nextBody == nil && strings.TrimSpace(item.PayloadTemplateJSON) != "" {
		var parsed any
		if err := json.Unmarshal([]byte(item.PayloadTemplateJSON), &parsed); err != nil {
			return fmt.Errorf("decode api case payload template %s: %w", item.ID, err)
		}
		nextBody = parsed
	}
	if nextBody == nil {
		return nil
	}
	patched, err := applyAPICaseJSONPatch(nextBody, item.PatchJSON)
	if err != nil {
		return fmt.Errorf("apply api case patch %s: %w", item.ID, err)
	}
	if err := applyAPICaseEquivalentBodyPatch(patched, item.PatchJSON); err != nil {
		return fmt.Errorf("apply api case equivalent field patch %s: %w", item.ID, err)
	}
	request.body = patched
	if err := resignAPICaseRequest(request, item.ID); err != nil {
		return err
	}
	return nil
}

func apiCaseUsesSandboxCallback(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return strings.Contains(rawURL, "/__sandbox/llt/callback")
	}
	return parsed.Path == "/__sandbox/llt/callback"
}

func mergeAPICasePayloadTemplateModel(body any, templateJSON string) (any, error) {
	templateJSON = strings.TrimSpace(templateJSON)
	if templateJSON == "" || templateJSON == "{}" {
		return body, nil
	}
	var templateModel any
	if err := json.Unmarshal([]byte(templateJSON), &templateModel); err != nil {
		return nil, err
	}
	templateObject := mapFromAny(renderCaseExecutionValue(templateModel, nil))
	if len(templateObject) == 0 {
		return body, nil
	}
	bodyObject, ok := body.(map[string]any)
	if !ok {
		return templateObject, nil
	}
	merged := make(map[string]any, len(templateObject)+len(bodyObject))
	for key, value := range templateObject {
		merged[key] = value
	}
	for key, value := range bodyObject {
		merged[key] = value
	}
	return merged, nil
}

func apiCasePatchTargetsQuery(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		return false
	}
}

func applyAPICaseQueryPatch(request *caseHTTPRequest, item profile.APICase) error {
	parsed, queryObject, err := apiCaseQueryObject(request.fullURL)
	if err != nil {
		return fmt.Errorf("decode api case query %s: %w", item.ID, err)
	}
	if len(queryObject) == 0 && strings.TrimSpace(item.PayloadTemplateJSON) != "" {
		var template any
		if err := json.Unmarshal([]byte(item.PayloadTemplateJSON), &template); err != nil {
			return fmt.Errorf("decode api case payload template %s: %w", item.ID, err)
		}
		queryObject = mapFromAny(template)
	}
	patched, err := applyAPICaseJSONPatch(queryObject, item.PatchJSON)
	if err != nil {
		return fmt.Errorf("apply api case patch %s: %w", item.ID, err)
	}
	patchedQuery, ok := patched.(map[string]any)
	if !ok {
		return fmt.Errorf("api case patch %s must keep query as an object", item.ID)
	}
	parsed.RawQuery = apiCaseQueryValues(patchedQuery).Encode()
	request.fullURL = parsed.String()
	request.body = nil
	if err := resignAPICaseRequest(request, item.ID); err != nil {
		return err
	}
	return nil
}

func apiCaseQueryObject(rawURL string) (*url.URL, map[string]any, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	values := parsed.Query()
	out := make(map[string]any, len(values))
	for key, items := range values {
		if len(items) == 1 {
			out[key] = items[0]
			continue
		}
		array := make([]any, 0, len(items))
		for _, item := range items {
			array = append(array, item)
		}
		out[key] = array
	}
	return parsed, out, nil
}

func apiCaseQueryValues(query map[string]any) url.Values {
	values := url.Values{}
	for key, raw := range query {
		switch typed := raw.(type) {
		case nil:
			continue
		case []any:
			for _, item := range typed {
				values.Add(key, valueString(item))
			}
		case []string:
			for _, item := range typed {
				values.Add(key, item)
			}
		default:
			values.Set(key, valueString(typed))
		}
	}
	return values
}

func resignAPICaseRequest(request *caseHTTPRequest, caseID string) error {
	if !request.signed {
		return nil
	}
	if request.headers == nil {
		request.headers = map[string]string{}
	}
	delete(request.headers, "Authorization")
	if err := request.applySigning(); err != nil {
		return fmt.Errorf("sign patched api case request %s: %w", caseID, err)
	}
	return nil
}

func applyAPICaseEquivalentBodyPatch(body any, patchJSON string) error {
	var operations []apiCaseJSONPatchOperation
	if err := json.Unmarshal([]byte(patchJSON), &operations); err != nil {
		return fmt.Errorf("decode patchJson: %w", err)
	}
	for _, operation := range operations {
		segments, err := parseAPICaseJSONPath(operation.Path)
		if err != nil {
			return err
		}
		if len(segments) != 1 || segments[0].Index != nil {
			continue
		}
		candidates := equivalentJSONFieldNames(segments[0].Key)
		if len(candidates) == 0 {
			continue
		}
		applyEquivalentJSONFieldPatch(body, candidates, operation)
	}
	return nil
}

func equivalentJSONFieldNames(key string) map[string]bool {
	parts := strings.Split(strings.TrimSpace(key), "_")
	candidates := map[string]bool{}
	for index := 0; index < len(parts); index++ {
		aliasParts := parts[index:]
		if index > 0 && len(aliasParts) < 2 {
			continue
		}
		name := lowerCamelName(aliasParts)
		if name != "" {
			candidates[name] = true
		}
		identifierParts := append([]string(nil), aliasParts...)
		if len(identifierParts) > 0 && identifierParts[len(identifierParts)-1] == "no" {
			identifierParts[len(identifierParts)-1] = "id"
			if name := lowerCamelName(identifierParts); name != "" {
				candidates[name] = true
			}
		}
	}
	delete(candidates, strings.TrimSpace(key))
	return candidates
}

func lowerCamelName(parts []string) string {
	var out strings.Builder
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if index == 0 && out.Len() == 0 {
			out.WriteString(strings.ToLower(part))
			continue
		}
		out.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			out.WriteString(strings.ToLower(part[1:]))
		}
	}
	return out.String()
}

func applyEquivalentJSONFieldPatch(value any, candidates map[string]bool, operation apiCaseJSONPatchOperation) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if candidates[key] {
				switch strings.ToLower(strings.TrimSpace(operation.Op)) {
				case "add", "replace":
					typed[key] = operation.Value
				case "remove":
					delete(typed, key)
				}
				continue
			}
			applyEquivalentJSONFieldPatch(child, candidates, operation)
		}
	case []any:
		for _, child := range typed {
			applyEquivalentJSONFieldPatch(child, candidates, operation)
		}
	}
}

func applyAPICaseExpectedJSON(request *caseHTTPRequest, expectedJSON string) error {
	expectedJSON = strings.TrimSpace(expectedJSON)
	if expectedJSON == "" || expectedJSON == "{}" {
		return nil
	}
	var parsed struct {
		ExpectedHTTPCodes []int    `json:"expectedHttpCodes"`
		ResponseContains  []string `json:"expectedResponseContains"`
	}
	if err := json.Unmarshal([]byte(expectedJSON), &parsed); err != nil {
		return fmt.Errorf("decode api case expectedJson: %w", err)
	}
	if len(parsed.ExpectedHTTPCodes) > 0 {
		request.expectedHTTPCodes = parsed.ExpectedHTTPCodes
	}
	if len(parsed.ResponseContains) > 0 {
		request.expectedResponse = parsed.ResponseContains
	}
	return nil
}

type apiCaseJSONPatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

type apiCaseJSONPathSegment struct {
	Key   string
	Index *int
}

func applyAPICaseJSONPatch(body any, patchJSON string) (any, error) {
	var operations []apiCaseJSONPatchOperation
	if err := json.Unmarshal([]byte(patchJSON), &operations); err != nil {
		return nil, fmt.Errorf("decode patchJson: %w", err)
	}
	next := body
	for _, operation := range operations {
		operation.Value = renderCaseExecutionValue(operation.Value, nil)
		segments, err := parseAPICaseJSONPath(operation.Path)
		if err != nil {
			return nil, err
		}
		if len(segments) == 0 {
			switch strings.ToLower(strings.TrimSpace(operation.Op)) {
			case "add", "replace":
				next = operation.Value
			case "remove":
				next = nil
			default:
				return nil, fmt.Errorf("unsupported patch op %q", operation.Op)
			}
			continue
		}
		var patchErr error
		next, patchErr = applyAPICaseJSONPatchOperation(next, segments, operation)
		if patchErr != nil {
			return nil, patchErr
		}
	}
	return next, nil
}

func parseAPICaseJSONPath(path string) ([]apiCaseJSONPathSegment, error) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return nil, nil
	}
	parts := strings.Split(path, ".")
	segments := make([]apiCaseJSONPathSegment, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		for part != "" {
			bracket := strings.Index(part, "[")
			if bracket < 0 {
				segments = append(segments, apiCaseJSONPathSegment{Key: part})
				break
			}
			if bracket > 0 {
				segments = append(segments, apiCaseJSONPathSegment{Key: part[:bracket]})
			}
			closeBracket := strings.Index(part[bracket:], "]")
			if closeBracket < 0 {
				return nil, fmt.Errorf("invalid patch path %q", path)
			}
			indexText := part[bracket+1 : bracket+closeBracket]
			index, err := strconv.Atoi(strings.TrimSpace(indexText))
			if err != nil || index < 0 {
				return nil, fmt.Errorf("invalid patch array index %q", path)
			}
			segments = append(segments, apiCaseJSONPathSegment{Index: &index})
			part = part[bracket+closeBracket+1:]
			part = strings.TrimPrefix(part, ".")
		}
	}
	return segments, nil
}

func applyAPICaseJSONPatchOperation(root any, segments []apiCaseJSONPathSegment, operation apiCaseJSONPatchOperation) (any, error) {
	parent := root
	for _, segment := range segments[:len(segments)-1] {
		next, ok := apiCaseJSONPathChild(parent, segment)
		if !ok {
			return root, fmt.Errorf("patch path not found: %s", operation.Path)
		}
		parent = next
	}
	last := segments[len(segments)-1]
	op := strings.ToLower(strings.TrimSpace(operation.Op))
	if last.Index != nil {
		array, ok := parent.([]any)
		if !ok {
			return root, fmt.Errorf("patch path is not an array: %s", operation.Path)
		}
		index := *last.Index
		if index < 0 || index >= len(array) {
			return root, fmt.Errorf("patch array index out of range: %s", operation.Path)
		}
		switch op {
		case "add", "replace":
			array[index] = operation.Value
		case "remove":
			copy(array[index:], array[index+1:])
			array[len(array)-1] = nil
			array = array[:len(array)-1]
			if len(segments) == 1 {
				return array, nil
			}
			return root, assignAPICaseJSONPathChild(root, segments[:len(segments)-1], array)
		default:
			return root, fmt.Errorf("unsupported patch op %q", operation.Op)
		}
		return root, nil
	}
	object, ok := parent.(map[string]any)
	if !ok {
		return root, fmt.Errorf("patch path is not an object: %s", operation.Path)
	}
	switch op {
	case "add", "replace":
		object[last.Key] = operation.Value
	case "remove":
		delete(object, last.Key)
	default:
		return root, fmt.Errorf("unsupported patch op %q", operation.Op)
	}
	return root, nil
}

func apiCaseJSONPathChild(parent any, segment apiCaseJSONPathSegment) (any, bool) {
	if segment.Index != nil {
		array, ok := parent.([]any)
		if !ok || *segment.Index < 0 || *segment.Index >= len(array) {
			return nil, false
		}
		return array[*segment.Index], true
	}
	object, ok := parent.(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := object[segment.Key]
	return value, ok
}

func assignAPICaseJSONPathChild(root any, segments []apiCaseJSONPathSegment, value any) error {
	if len(segments) == 0 {
		return nil
	}
	parent := root
	for _, segment := range segments[:len(segments)-1] {
		next, ok := apiCaseJSONPathChild(parent, segment)
		if !ok {
			return nil
		}
		parent = next
	}
	last := segments[len(segments)-1]
	if last.Index != nil {
		array, ok := parent.([]any)
		if !ok || *last.Index < 0 || *last.Index >= len(array) {
			return nil
		}
		array[*last.Index] = value
		return nil
	}
	if object, ok := parent.(map[string]any); ok {
		object[last.Key] = value
	}
	return nil
}

func renderCaseExecutionValue(value any, overrides map[string]any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = renderCaseExecutionValue(item, overrides)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, renderCaseExecutionValue(item, overrides))
		}
		return out
	case string:
		return renderCaseString(typed, overrides)
	default:
		return value
	}
}

func renderCaseString(value string, overrides map[string]any) string {
	rendered := strings.ReplaceAll(value, "${AUTO_SERIAL}", serialValue("GEN"))
	rendered = strings.ReplaceAll(rendered, "${AUTO_RT_ORDER_ID}", serialValue("RT"))
	cursor := 0
	for {
		if cursor >= len(rendered) {
			break
		}
		start := strings.Index(rendered[cursor:], "{{")
		if start >= 0 {
			start += cursor
		}
		if start < 0 {
			break
		}
		end := strings.Index(rendered[start+2:], "}}")
		if end < 0 {
			break
		}
		end += start + 2
		token := rendered[start+2 : end]
		replacement, ok := renderCaseToken(token, overrides)
		if !ok {
			cursor = end + 2
			continue
		}
		rendered = rendered[:start] + replacement + rendered[end+2:]
		cursor = start + len(replacement)
	}
	return rendered
}

func renderCaseToken(token string, overrides map[string]any) (string, bool) {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "override:") {
		body := strings.TrimPrefix(token, "override:")
		key, defaultValue, _ := strings.Cut(body, "|")
		if value := strings.TrimSpace(valueString(overrides[strings.TrimSpace(key)])); value != "" {
			return renderDefaultValue(value), true
		}
		return renderDefaultValue(defaultValue), true
	}
	if strings.HasPrefix(token, "serial:") {
		return serialValue(strings.TrimPrefix(token, "serial:")), true
	}
	if token == "now:datetime" {
		return time.Now().UTC().Format("2006-01-02 15:04:05"), true
	}
	return "", false
}

func renderDefaultValue(value string) string {
	if strings.HasPrefix(value, "serial:") {
		return serialValue(strings.TrimPrefix(value, "serial:"))
	}
	if value == "now:datetime" {
		return time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	return value
}

func serialValue(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "GEN"
	}
	counter := atomic.AddUint64(&caseSerialCounter, 1) % 1000000
	return fmt.Sprintf("%s%s%06d", prefix, time.Now().UTC().Format("20060102150405"), counter)
}

func headerStrings(headers map[string]any) map[string]string {
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		if strings.TrimSpace(key) != "" {
			out[key] = valueString(value)
		}
	}
	return out
}

func responseHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		out[key] = strings.Join(values, ", ")
	}
	return out
}

func expectedHTTPCode(status int, expected []int) bool {
	if len(expected) == 0 {
		return status >= 200 && status < 300
	}
	for _, value := range expected {
		if status == value {
			return true
		}
	}
	return false
}

func testKitTimeout(payload map[string]any) time.Duration {
	seconds := intValue(payload["timeoutSeconds"])
	if seconds <= 0 {
		seconds = 90
	}
	return time.Duration(seconds) * time.Second
}

func failedCaseExecution(caseID string, reason string) caseExecutionResult {
	return caseExecutionResult{
		ok:            false,
		failureReason: reason,
		result: map[string]any{
			"request":  map[string]any{"caseId": caseID},
			"response": map[string]any{"body": "{}"},
		},
	}
}

func recordTestKitRun(r *http.Request, bundle profile.Bundle, runtime store.Store, payload map[string]any, result map[string]any) (string, error) {
	return recordTestKitRunWithContext(r.Context(), bundle, runtime, payload, result)
}

func recordTestKitRunWithContext(ctx context.Context, bundle profile.Bundle, runtime store.Store, payload map[string]any, result map[string]any) (string, error) {
	if runtime == nil {
		return "", nil
	}
	status := store.StatusFailed
	if result["ok"] == true {
		status = store.StatusPassed
	}
	workflowID := firstNonEmpty(valueString(payload["workflowId"]), valueString(result["caseId"]))
	summary := map[string]any{
		"kind":    "apiCase",
		"summary": result["summary"],
		"steps":   []map[string]any{result},
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	startedAt, finishedAt := testKitResultTimes(result, now)
	runID := firstNonEmpty(valueString(payload["runId"]), workflowRunID(now))
	evidenceRoot, err := writeTestKitEvidenceFiles(result, status, valueString(payload["evidenceDir"]), runID)
	if err != nil {
		return "", err
	}
	_, err = runtime.CreateRun(ctx, store.Run{
		ID:            runID,
		ProfileID:     bundle.ID,
		EnvironmentID: valueString(payload["environmentId"]),
		WorkflowID:    workflowID,
		Status:        status,
		EvidenceRoot:  evidenceRoot,
		SummaryJSON:   string(raw),
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		CreatedAt:     startedAt,
		UpdatedAt:     finishedAt,
	})
	if err != nil {
		return "", err
	}
	caseID := valueString(result["caseId"])
	if caseID == "" {
		return runID, nil
	}
	_, err = runtime.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   runID + ".case",
		RunID:                runID,
		CaseID:               caseID,
		Status:               status,
		RequestSummaryJSON:   compactJSON(testKitRequestSummary(result, valueString(payload["stepId"]), caseID)),
		AssertionSummaryJSON: compactJSON(map[string]any{"status": status}),
		StartedAt:            startedAt,
		FinishedAt:           finishedAt,
		CreatedAt:            startedAt,
	})
	if err != nil {
		return "", err
	}
	if err := recordTestKitEvidence(ctx, runtime, runID, runID+".case", valueString(payload["stepId"]), caseID, evidenceRoot, finishedAt); err != nil {
		return "", err
	}
	return runID, nil
}

func writeTestKitEvidenceFiles(result map[string]any, status string, evidenceDir string, runID string) (string, error) {
	root := ""
	var err error
	if strings.TrimSpace(evidenceDir) != "" {
		root = filepath.Join(evidenceDir, runID)
		if err := os.MkdirAll(root, 0o755); err != nil {
			return "", fmt.Errorf("create test-kit evidence directory: %w", err)
		}
	} else {
		root, err = os.MkdirTemp("", "otsandbox-test-kit-evidence-*")
		if err != nil {
			return "", fmt.Errorf("create test-kit evidence dir: %w", err)
		}
	}
	request := mapFromAny(mapFromAny(result["result"])["request"])
	response := mapFromAny(mapFromAny(result["result"])["response"])
	assertions := map[string]any{
		"status": status,
		"passed": status == store.StatusPassed,
	}
	if reason := strings.TrimSpace(valueString(result["failureReason"])); reason != "" {
		assertions["errors"] = []string{reason}
	}
	for name, payload := range map[string]map[string]any{
		"request.json":    request,
		"response.json":   response,
		"assertions.json": assertions,
	} {
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(root, name), append(raw, '\n'), 0o644); err != nil {
			return "", err
		}
	}
	return root, nil
}

func recordTestKitEvidence(ctx context.Context, runtime store.Store, runID string, caseRunID string, stepID string, caseID string, evidenceRoot string, createdAt time.Time) error {
	for _, name := range []string{"request.json", "response.json", "assertions.json"} {
		path := filepath.Join(evidenceRoot, name)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		kind := strings.TrimSuffix(name, ".json")
		summary, err := apiCaseEvidenceSummary(path, kind, info.Size())
		if err != nil {
			return err
		}
		labels := map[string]any{
			"caseId": caseID,
			"kind":   kind,
			"runId":  runID,
		}
		if strings.TrimSpace(stepID) != "" {
			labels["stepId"] = stepID
		}
		if _, err := runtime.RecordEvidence(ctx, store.EvidenceRecord{
			ID:         runID + "." + name,
			RunID:      runID,
			CaseRunID:  caseRunID,
			StepID:     stepID,
			Kind:       kind,
			URI:        path,
			MediaType:  "application/json",
			SizeBytes:  info.Size(),
			Summary:    summary,
			Category:   apiCaseEvidenceCategory(kind),
			Visibility: "public",
			LabelsJSON: compactJSON(labels),
			CreatedAt:  createdAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func testKitResultTimes(result map[string]any, finishedAt time.Time) (time.Time, time.Time) {
	elapsed := intValue(result["elapsedMs"])
	if elapsed <= 0 {
		elapsed = intValue(mapFromAny(mapFromAny(result["result"])["response"])["elapsedMs"])
	}
	if elapsed <= 0 {
		return finishedAt, finishedAt
	}
	return finishedAt.Add(-time.Duration(elapsed) * time.Millisecond), finishedAt
}

func attachTestKitTraceTopology(ctx context.Context, runtime store.Store, collector traceCollector, runID string, payload map[string]any, result map[string]any) {
	collectPayload, ok := testKitTraceTopologyCollectPayload(runID, payload, result)
	if !ok || runtime == nil || strings.TrimSpace(collector.GraphQLURL) == "" {
		return
	}
	row, topology, err := collectTraceTopologyWithRetry(ctx, runtime, collector, collectPayload)
	if err != nil {
		result["traceTopologyError"] = err.Error()
		return
	}
	result["traceTopology"] = topology
	result["traceTopologyRow"] = traceTopologyPayload(row)
}

func shouldInlineTestKitTraceTopology(payload map[string]any) bool {
	return strings.TrimSpace(valueString(payload["workflowId"])) != "" && strings.TrimSpace(valueString(payload["stepId"])) != ""
}

func collectAndRecordTestKitTraceTopology(ctx context.Context, runtime store.Store, collector traceCollector, runID string, payload map[string]any, result map[string]any) {
	collectPayload, ok := testKitTraceTopologyCollectPayload(runID, payload, result)
	if !ok || runtime == nil {
		return
	}
	if strings.TrimSpace(collector.GraphQLURL) == "" {
		recordSkippedTestKitTraceTopologyTask(runtime, runID, payload, collectPayload, "TraceGraphQLURL is not configured; trace topology collection skipped")
		return
	}
	started := time.Now().UTC()
	status := store.StatusPassed
	errText := ""
	summary := map[string]any{}
	defer func() {
		finished := time.Now().UTC()
		recordPostProcessTask(context.Background(), runtime, store.PostProcessTask{
			ID:          runID + "." + safeRuntimeLogPathSegment(valueString(collectPayload["stepId"])) + "." + postProcessKindTraceTopology,
			RunID:       runID,
			WorkflowID:  valueString(payload["workflowId"]),
			StepID:      valueString(collectPayload["stepId"]),
			CaseID:      valueString(collectPayload["caseId"]),
			Kind:        postProcessKindTraceTopology,
			Status:      status,
			StartedAt:   started,
			FinishedAt:  finished,
			DurationMs:  finished.Sub(started).Milliseconds(),
			Error:       errText,
			SummaryJSON: compactJSON(summary),
			CreatedAt:   finished,
		})
	}()
	collectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	row, topology, err := collectTraceTopologyWithRetry(collectCtx, runtime, collector, collectPayload)
	if err != nil {
		status = store.StatusFailed
		errText = err.Error()
		result["traceTopologyError"] = err.Error()
		return
	}
	result["traceTopology"] = topology
	result["traceTopologyRow"] = traceTopologyPayload(row)
	summary["traceId"] = row.TraceID
	summary["requestId"] = row.RequestID
	summary["topologyStatus"] = topology.Status
	summary["spanCount"] = topology.SpanCount
}

func scheduleTestKitTraceTopology(runtime store.Store, collector traceCollector, runID string, payload map[string]any, result map[string]any) {
	collectPayload, ok := testKitTraceTopologyCollectPayload(runID, payload, result)
	if !ok || runtime == nil {
		return
	}
	if strings.TrimSpace(collector.GraphQLURL) == "" {
		recordSkippedTestKitTraceTopologyTask(runtime, runID, payload, collectPayload, "TraceGraphQLURL is not configured; trace topology collection skipped")
		return
	}
	go func() {
		started := time.Now().UTC()
		status := store.StatusPassed
		errText := ""
		summary := map[string]any{}
		defer func() {
			finished := time.Now().UTC()
			recordPostProcessTask(context.Background(), runtime, store.PostProcessTask{
				ID:          runID + "." + safeRuntimeLogPathSegment(valueString(collectPayload["stepId"])) + "." + postProcessKindTraceTopology,
				RunID:       runID,
				WorkflowID:  valueString(payload["workflowId"]),
				StepID:      valueString(collectPayload["stepId"]),
				CaseID:      valueString(collectPayload["caseId"]),
				Kind:        postProcessKindTraceTopology,
				Status:      status,
				StartedAt:   started,
				FinishedAt:  finished,
				DurationMs:  finished.Sub(started).Milliseconds(),
				Error:       errText,
				SummaryJSON: compactJSON(summary),
				CreatedAt:   finished,
			})
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		row, topology, err := collectTraceTopologyWithRetry(ctx, runtime, collector, collectPayload)
		if err != nil {
			status = store.StatusFailed
			errText = err.Error()
			return
		}
		summary["traceId"] = row.TraceID
		summary["requestId"] = row.RequestID
		summary["topologyStatus"] = topology.Status
		summary["spanCount"] = topology.SpanCount
	}()
}

func recordSkippedTestKitTraceTopologyTask(runtime store.Store, runID string, payload map[string]any, collectPayload map[string]any, errText string) {
	now := time.Now().UTC()
	recordPostProcessTask(context.Background(), runtime, store.PostProcessTask{
		ID:          runID + "." + safeRuntimeLogPathSegment(valueString(collectPayload["stepId"])) + "." + postProcessKindTraceTopology,
		RunID:       runID,
		WorkflowID:  valueString(payload["workflowId"]),
		StepID:      valueString(collectPayload["stepId"]),
		CaseID:      valueString(collectPayload["caseId"]),
		Kind:        postProcessKindTraceTopology,
		Status:      store.StatusSkipped,
		StartedAt:   now,
		FinishedAt:  now,
		DurationMs:  0,
		Error:       errText,
		SummaryJSON: compactJSON(map[string]any{"reason": "trace_provider_missing"}),
		CreatedAt:   now,
	})
}

func testKitTraceTopologyCollectPayload(runID string, payload map[string]any, result map[string]any) (map[string]any, bool) {
	if runID == "" || result["ok"] != true {
		return nil, false
	}
	request := mapFromAny(mapFromAny(result["result"])["request"])
	response := mapFromAny(mapFromAny(result["result"])["response"])
	headers := request["headers"]
	endpoint := firstNonEmpty(
		valueString(payload["traceEndpoint"]),
		valueString(headerValue(headers, "X-Sandbox-Trace-Endpoint")),
		valueString(headerValue(headers, "X-Sandbox-Callback-Path")),
		valueString(request["path"]),
		valueString(request["fullUrl"]),
	)
	if endpoint == "" {
		return nil, false
	}
	return map[string]any{
		"runId":     runID,
		"caseId":    result["caseId"],
		"stepId":    firstNonEmpty(valueString(payload["stepId"]), valueString(result["stepId"])),
		"requestId": responseRequestID(response),
		"endpoint":  endpoint,
	}, true
}

func collectTraceTopologyWithRetry(ctx context.Context, runtime store.Store, collector traceCollector, payload map[string]any) (store.TraceTopology, traceTopology, error) {
	var lastErr error
	attempt := 0
	for {
		row, topology, err := collectTraceTopology(ctx, runtime, collector, payload)
		if err == nil {
			return row, topology, nil
		}
		lastErr = err
		if !retryableTraceCollectError(err) {
			break
		}
		attempt++
		if attempt >= 15 {
			break
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return store.TraceTopology{}, traceTopology{}, ctx.Err()
		case <-timer.C:
		}
	}
	return store.TraceTopology{}, traceTopology{}, lastErr
}

func retryableTraceCollectError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not indexed yet") || strings.Contains(message, "no queryable traces")
}

func responseRequestID(response map[string]any) string {
	for _, key := range []string{"Request-Id", "Request-ID", "request-id", "X-Request-Id", "X-Request-ID"} {
		if value := strings.TrimSpace(valueString(headerValue(response["headers"], key))); value != "" {
			return value
		}
	}
	return ""
}

func headerValue(headers any, key string) any {
	switch typed := headers.(type) {
	case map[string]any:
		return typed[key]
	case map[string]string:
		if value, ok := typed[key]; ok {
			return value
		}
		for itemKey, value := range typed {
			if strings.EqualFold(itemKey, key) {
				return value
			}
		}
		return nil
	default:
		return nil
	}
}

func testKitRequestSummary(result map[string]any, stepID string, caseID string) map[string]any {
	request := mapFromAny(mapFromAny(result["result"])["request"])
	if len(request) == 0 {
		request = map[string]any{}
	}
	request["caseId"] = caseID
	request["stepId"] = stepID
	return request
}

func findAPICase(items []profile.APICase, id string) (profile.APICase, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return profile.APICase{}, false
}

func findRunnableAPICase(ctx context.Context, bundle profile.Bundle, runtime store.Store, id string, payload map[string]any) (runnableAPICase, bool) {
	if item, ok := findAPICase(bundle.APICases, id); ok {
		item.CasePath = resolveBundleAPICasePath(ctx, runtime, bundle, item.CasePath)
		execution := findCaseExecutionConfig(ctx, runtime, id, payload)
		caseBaseURL := item.BaseURL
		if runtime != nil {
			if catalogItem, catalog, ok := findCatalogAPICase(ctx, runtime, id); ok {
				catalogCase := profileAPICaseFromCatalog(catalogItem)
				catalogCase.CasePath = resolveCatalogAPICasePath(ctx, runtime, catalog.ProfileID, catalogItem.CasePath)
				catalogCase.PayloadTemplateJSON = apiCasePayloadTemplateJSON(catalogCase.PayloadTemplateJSON, catalogRequestTemplateJSON(catalog, catalogItem.RequestTemplateID))
				item = mergeRunnableAPICaseModel(item, catalogCase)
				caseBaseURL = firstNonEmpty(caseBaseURL, catalogItem.BaseURL)
				if execution == nil {
					execution = deriveCaseExecutionConfigFromCatalog(catalog, catalogItem)
				}
			}
		}
		return runnableAPICase{Case: item, Execution: execution, CaseBaseURL: caseBaseURL}, true
	}
	if runtime == nil {
		return runnableAPICase{}, false
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return runnableAPICase{}, false
	}
	for _, item := range catalog.APICases {
		if item.ID == id {
			apiCase := profileAPICaseFromCatalog(item)
			apiCase.CasePath = resolveCatalogAPICasePath(ctx, runtime, catalog.ProfileID, item.CasePath)
			apiCase.PayloadTemplateJSON = apiCasePayloadTemplateJSON(apiCase.PayloadTemplateJSON, catalogRequestTemplateJSON(catalog, item.RequestTemplateID))
			execution := findCaseExecutionConfigFromCatalog(catalog, id, payload)
			if execution == nil {
				execution = deriveCaseExecutionConfigFromCatalog(catalog, item)
			}
			return runnableAPICase{Case: apiCase, Execution: execution, CaseBaseURL: item.BaseURL}, true
		}
	}
	return runnableAPICase{}, false
}

func findCatalogAPICase(ctx context.Context, runtime store.Store, id string) (store.CatalogAPICase, store.ProfileCatalog, bool) {
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return store.CatalogAPICase{}, store.ProfileCatalog{}, false
	}
	for _, item := range catalog.APICases {
		if item.ID == id {
			return item, catalog, true
		}
	}
	return store.CatalogAPICase{}, store.ProfileCatalog{}, false
}

func catalogRequestTemplateJSON(catalog store.ProfileCatalog, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for _, item := range catalog.RequestTemplates {
		if item.ID == id {
			return item.TemplateJSON
		}
	}
	return ""
}

func apiCasePayloadTemplateJSON(value string, requestTemplateJSON string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "{}" {
		return requestTemplateJSON
	}
	return value
}

func mergeRunnableAPICaseModel(base profile.APICase, enriched profile.APICase) profile.APICase {
	if strings.TrimSpace(base.DisplayName) == "" {
		base.DisplayName = enriched.DisplayName
	}
	if strings.TrimSpace(base.Description) == "" {
		base.Description = enriched.Description
	}
	if strings.TrimSpace(base.NodeID) == "" {
		base.NodeID = enriched.NodeID
	}
	if strings.TrimSpace(base.CaseType) == "" {
		base.CaseType = enriched.CaseType
	}
	if strings.TrimSpace(base.Scenario) == "" {
		base.Scenario = enriched.Scenario
	}
	if len(base.Tags) == 0 {
		base.Tags = enriched.Tags
	}
	if strings.TrimSpace(base.Priority) == "" {
		base.Priority = enriched.Priority
	}
	if strings.TrimSpace(base.Owner) == "" {
		base.Owner = enriched.Owner
	}
	if strings.TrimSpace(base.PayloadTemplateJSON) == "" || strings.TrimSpace(base.PayloadTemplateJSON) == "{}" {
		base.PayloadTemplateJSON = enriched.PayloadTemplateJSON
	}
	if strings.TrimSpace(base.RequestTemplateID) == "" {
		base.RequestTemplateID = enriched.RequestTemplateID
	}
	if strings.TrimSpace(base.PatchJSON) == "" {
		base.PatchJSON = enriched.PatchJSON
	}
	if strings.TrimSpace(base.RenderMode) == "" {
		base.RenderMode = enriched.RenderMode
	}
	if strings.TrimSpace(base.ExpectedJSON) == "" {
		base.ExpectedJSON = enriched.ExpectedJSON
	}
	if strings.TrimSpace(base.CasePath) == "" {
		base.CasePath = enriched.CasePath
	}
	if strings.TrimSpace(base.BaseURL) == "" {
		base.BaseURL = enriched.BaseURL
	}
	if strings.TrimSpace(base.EvidenceDir) == "" {
		base.EvidenceDir = enriched.EvidenceDir
	}
	if base.TimeoutSeconds == 0 {
		base.TimeoutSeconds = enriched.TimeoutSeconds
	}
	if len(base.DefaultOverrides) == 0 {
		base.DefaultOverrides = enriched.DefaultOverrides
	}
	return base
}

func profileAPICaseFromCatalog(item store.CatalogAPICase) profile.APICase {
	return profile.APICase{
		ID:                   item.ID,
		DisplayName:          item.DisplayName,
		Description:          item.Description,
		NodeID:               item.NodeID,
		CaseType:             item.CaseType,
		Scenario:             item.Scenario,
		Tags:                 item.Tags,
		Priority:             item.Priority,
		Owner:                item.Owner,
		PayloadTemplateJSON:  item.PayloadTemplateJSON,
		RequestTemplateID:    item.RequestTemplateID,
		PatchJSON:            item.PatchJSON,
		RenderMode:           item.RenderMode,
		ExpectedJSON:         item.ExpectedJSON,
		RequiredForAdmission: item.RequiredForAdmission,
		Status:               item.Status,
		SortOrder:            item.SortOrder,
		CasePath:             item.CasePath,
		SourceKind:           item.SourceKind,
		SourcePath:           item.SourcePath,
		ExecutorID:           item.ExecutorID,
		BaseURL:              item.BaseURL,
		EvidenceDir:          item.EvidenceDir,
		TimeoutSeconds:       item.TimeoutSeconds,
		DefaultOverrides:     mapFromAny(jsonObject(item.DefaultOverridesJSON)),
	}
}

func deriveCaseExecutionConfigFromCatalog(catalog store.ProfileCatalog, item store.CatalogAPICase) *caseExecutionConfig {
	if execution := deriveCaseExecutionConfigFromSiblingConfig(catalog, item); execution != nil {
		return execution
	}
	templateID := strings.TrimSpace(item.RequestTemplateID)
	if templateID == "" {
		return nil
	}
	var selected store.CatalogRequestTemplate
	for _, template := range catalog.RequestTemplates {
		if template.ID == templateID && activeCatalogStatus(template.Status) {
			selected = template
			break
		}
	}
	if strings.TrimSpace(selected.ID) == "" {
		return nil
	}
	method := strings.ToUpper(firstNonEmpty(selected.Method, "GET"))
	execution := caseExecutionConfig{
		Method: strings.ToUpper(method),
		NodeID: firstNonEmpty(item.NodeID, selected.NodeID),
		Path:   firstNonEmpty(selected.Path, "/"),
	}
	if strings.TrimSpace(selected.TemplateJSON) != "" {
		var body any
		if json.Unmarshal([]byte(selected.TemplateJSON), &body) == nil {
			if method == http.MethodGet || method == http.MethodHead {
				execution.Query = mapFromAny(body)
			} else {
				execution.Body = body
			}
		}
	}
	if expected := expectedConfigFromAPICase(item.ExpectedJSON); expected != nil {
		execution.ExpectedHTTPCodes = expected.ExpectedHTTPCodes
		execution.ExpectedResponse = expected.ExpectedResponse
		execution.RequireRequestID = expected.RequireRequestID
	}
	return &execution
}

func deriveCaseExecutionConfigFromSiblingConfig(catalog store.ProfileCatalog, item store.CatalogAPICase) *caseExecutionConfig {
	caseNodeByID := map[string]string{}
	for _, apiCase := range catalog.APICases {
		caseNodeByID[apiCase.ID] = apiCase.NodeID
	}
	for _, config := range catalog.TemplateConfigs {
		if config.Status != "" && config.Status != "active" {
			continue
		}
		var parsed caseExecutionTemplateConfig
		if json.Unmarshal([]byte(config.ConfigJSON), &parsed) != nil {
			continue
		}
		if strings.TrimSpace(config.NodeID) != "" && config.NodeID != item.NodeID && caseNodeByID[parsed.CaseID] != item.NodeID {
			continue
		}
		next := parsed.CaseExecution
		if next.Method == "" && next.Path == "" && next.NodeID == "" {
			continue
		}
		if strings.TrimSpace(config.NodeID) == "" && caseNodeByID[parsed.CaseID] != item.NodeID {
			continue
		}
		cloned := cloneCaseExecutionConfig(next)
		if expected := expectedConfigFromAPICase(item.ExpectedJSON); expected != nil {
			cloned.ExpectedHTTPCodes = expected.ExpectedHTTPCodes
			cloned.ExpectedResponse = expected.ExpectedResponse
			cloned.RequireRequestID = expected.RequireRequestID
		}
		return &cloned
	}
	return nil
}

func cloneCaseExecutionConfig(input caseExecutionConfig) caseExecutionConfig {
	raw, err := json.Marshal(input)
	if err != nil {
		return input
	}
	var out caseExecutionConfig
	if json.Unmarshal(raw, &out) != nil {
		return input
	}
	return out
}

func expectedConfigFromAPICase(expectedJSON string) *caseExecutionConfig {
	expectedJSON = strings.TrimSpace(expectedJSON)
	if expectedJSON == "" || expectedJSON == "{}" {
		return nil
	}
	var parsed struct {
		ExpectedHTTPCodes []int    `json:"expectedHttpCodes"`
		ResponseContains  []string `json:"expectedResponseContains"`
		RequireRequestID  bool     `json:"requireRequestId"`
	}
	if json.Unmarshal([]byte(expectedJSON), &parsed) != nil {
		return nil
	}
	return &caseExecutionConfig{
		ExpectedHTTPCodes: parsed.ExpectedHTTPCodes,
		ExpectedResponse:  parsed.ResponseContains,
		RequireRequestID:  parsed.RequireRequestID,
	}
}

func resolveBundleAPICasePath(ctx context.Context, runtime store.Store, bundle profile.Bundle, casePath string) string {
	casePath = strings.TrimSpace(casePath)
	if casePath == "" || filepath.IsAbs(casePath) || fileExists(casePath) {
		return casePath
	}
	if strings.TrimSpace(bundle.BaseDir) != "" {
		candidate := resolveProfilePath(bundle.BaseDir, filepath.FromSlash(casePath))
		if fileExists(candidate) {
			return candidate
		}
	}
	return resolveCatalogAPICasePath(ctx, runtime, bundle.ID, casePath)
}

func resolveCatalogAPICasePath(ctx context.Context, runtime store.Store, profileID string, casePath string) string {
	casePath = strings.TrimSpace(casePath)
	if casePath == "" || filepath.IsAbs(casePath) || fileExists(casePath) || runtime == nil {
		return casePath
	}
	index, err := runtime.GetProfileIndex(ctx, strings.TrimSpace(profileID))
	if err != nil || strings.TrimSpace(index.BundlePath) == "" {
		return casePath
	}
	candidate := filepath.Join(index.BundlePath, filepath.FromSlash(casePath))
	if fileExists(candidate) {
		return candidate
	}
	return casePath
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func findCaseExecutionConfig(ctx context.Context, runtime store.Store, caseID string, payload map[string]any) *caseExecutionConfig {
	template := findCaseExecutionTemplateConfig(ctx, runtime, caseID, payload)
	if template == nil {
		return nil
	}
	return &template.CaseExecution
}

func findCaseExecutionTemplateConfig(ctx context.Context, runtime store.Store, caseID string, payload map[string]any) *caseExecutionTemplateConfig {
	if runtime == nil {
		return nil
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return nil
	}
	return findCaseExecutionTemplateConfigFromCatalog(catalog, caseID, payload)
}

func findCaseExecutionConfigFromCatalog(catalog store.ProfileCatalog, caseID string, payload map[string]any) *caseExecutionConfig {
	template := findCaseExecutionTemplateConfigFromCatalog(catalog, caseID, payload)
	if template == nil {
		return nil
	}
	return &template.CaseExecution
}

func findCaseExecutionTemplateConfigFromCatalog(catalog store.ProfileCatalog, caseID string, payload map[string]any) *caseExecutionTemplateConfig {
	workflowID := valueString(payload["workflowId"])
	stepID := valueString(payload["stepId"])
	var defaultValue *caseExecutionTemplateConfig
	for _, config := range catalog.TemplateConfigs {
		if config.Status != "" && config.Status != "active" {
			continue
		}
		var parsed caseExecutionTemplateConfig
		if err := json.Unmarshal([]byte(config.ConfigJSON), &parsed); err != nil {
			continue
		}
		next := parsed.CaseExecution
		if next.Method == "" && next.Path == "" && next.NodeID == "" {
			continue
		}
		if workflowID != "" && stepID != "" && config.WorkflowID == workflowID && config.ScopeID == stepID {
			return &parsed
		}
		if parsed.CaseID != caseID {
			continue
		}
		if defaultValue == nil {
			defaultValue = &parsed
		}
	}
	return defaultValue
}

func testKitCaseIDs(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if id := valueString(item); id != "" {
				out = append(out, id)
			}
		}
		return out
	case []string:
		return typed
	default:
		return nil
	}
}

func boolValue(value any) bool {
	typed, _ := value.(bool)
	return typed
}
