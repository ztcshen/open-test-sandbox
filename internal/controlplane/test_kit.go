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
	"strings"
	"time"

	"open-test-sandbox/internal/profile"
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
	RequireRequestID      bool           `json:"requireRequestId"`
	Signed                bool           `json:"signed"`
	TraceCorrelatorFields []string       `json:"traceCorrelatorFields"`
}

type caseExecutionTemplateConfig struct {
	CaseID        string              `json:"caseId"`
	CaseExecution caseExecutionConfig `json:"caseExecution"`
}

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
	rendered.Body = renderCaseExecutionValue(rendered.Body, overrides)
	return rendered
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
	for {
		start := strings.Index(rendered, "{{")
		end := strings.Index(rendered, "}}")
		if start < 0 || end < start {
			break
		}
		token := rendered[start+2 : end]
		replacement := renderCaseToken(token, overrides)
		rendered = rendered[:start] + replacement + rendered[end+2:]
	}
	return rendered
}

func renderCaseToken(token string, overrides map[string]any) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "override:") {
		body := strings.TrimPrefix(token, "override:")
		key, defaultValue, _ := strings.Cut(body, "|")
		if value := strings.TrimSpace(valueString(overrides[strings.TrimSpace(key)])); value != "" {
			return value
		}
		return renderDefaultValue(defaultValue)
	}
	if strings.HasPrefix(token, "serial:") {
		return serialValue(strings.TrimPrefix(token, "serial:"))
	}
	if token == "now:datetime" {
		return time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	return "{{" + token + "}}"
}

func renderDefaultValue(value string) string {
	if strings.HasPrefix(value, "serial:") {
		return serialValue(strings.TrimPrefix(value, "serial:"))
	}
	return value
}

func serialValue(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "GEN"
	}
	return prefix + time.Now().UTC().Format("20060102150405")
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
		ID:           runID,
		ProfileID:    bundle.ID,
		WorkflowID:   workflowID,
		Status:       status,
		EvidenceRoot: evidenceRoot,
		SummaryJSON:  string(raw),
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		CreatedAt:    startedAt,
		UpdatedAt:    finishedAt,
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
	for attempt := 0; attempt < 4; attempt++ {
		row, topology, err := collectTraceTopology(ctx, runtime, collector, payload)
		if err == nil {
			return row, topology, nil
		}
		lastErr = err
		if !retryableTraceCollectError(err) || attempt == 3 {
			break
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 500 * time.Millisecond)
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
		return runnableAPICase{Case: item, Execution: findCaseExecutionConfig(ctx, runtime, id, payload)}, true
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
			return runnableAPICase{Case: profile.APICase{
				ID:          item.ID,
				DisplayName: item.DisplayName,
				NodeID:      item.NodeID,
				CasePath:    resolveCatalogAPICasePath(ctx, runtime, catalog.ProfileID, item.CasePath),
			}, Execution: findCaseExecutionConfigFromCatalog(catalog, id, payload), CaseBaseURL: item.BaseURL}, true
		}
	}
	return runnableAPICase{}, false
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
	if runtime == nil {
		return nil
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return nil
	}
	return findCaseExecutionConfigFromCatalog(catalog, caseID, payload)
}

func findCaseExecutionConfigFromCatalog(catalog store.ProfileCatalog, caseID string, payload map[string]any) *caseExecutionConfig {
	workflowID := valueString(payload["workflowId"])
	stepID := valueString(payload["stepId"])
	var defaultValue *caseExecutionConfig
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
			return &next
		}
		if parsed.CaseID != caseID {
			continue
		}
		if defaultValue == nil {
			defaultValue = &next
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
