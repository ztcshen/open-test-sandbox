package controlplane

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"open-test-sandbox/internal/domain/casesuite"
	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/runner/apicase"
	"open-test-sandbox/internal/runner/junit"
	"open-test-sandbox/internal/store"
)

type apiCaseBatchRunRequest struct {
	RequestID      string                    `json:"requestId"`
	EnvironmentID  string                    `json:"environmentId,omitempty"`
	CaseIDs        []string                  `json:"caseIds"`
	NodeIDs        []string                  `json:"nodeIds"`
	WorkflowID     string                    `json:"workflowId"`
	Suite          apiCaseBatchSuiteSelector `json:"suite,omitempty"`
	BaseURL        string                    `json:"baseUrl"`
	EvidenceDir    string                    `json:"evidenceDir"`
	TimeoutSeconds int                       `json:"timeoutSeconds"`
	Overrides      map[string]any            `json:"overrides"`
}

type apiCaseBatchSuiteSelector struct {
	Filter    string   `json:"filter,omitempty"`
	NodeID    string   `json:"nodeId,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Status    string   `json:"status,omitempty"`
	Owner     string   `json:"owner,omitempty"`
	Priority  string   `json:"priority,omitempty"`
	RunStates []string `json:"runStates,omitempty"`
}

type apiCaseBatchCasePlan struct {
	ID              string
	DisplayName     string
	Scenario        string
	NodeID          string
	NodeDisplayName string
	Operation       string
	Method          string
	Path            string
	StepID          string
	CasePath        string
	BaseURL         string
	EvidenceDir     string
	TimeoutSeconds  int
	Overrides       map[string]any
	Execution       *caseExecutionConfig
	Exports         []map[string]any
	Case            profile.APICase
}

type apiCaseBatchCaseReport struct {
	CaseID          string `json:"caseId"`
	DisplayName     string `json:"displayName,omitempty"`
	Scenario        string `json:"scenario,omitempty"`
	NodeID          string `json:"nodeId"`
	NodeDisplayName string `json:"nodeDisplayName,omitempty"`
	Operation       string `json:"operation,omitempty"`
	Method          string `json:"method,omitempty"`
	Path            string `json:"path,omitempty"`
	StepID          string `json:"stepId,omitempty"`
	RunID           string `json:"runId,omitempty"`
	CaseRunID       string `json:"caseRunId,omitempty"`
	Status          string `json:"status"`
	ViewerURL       string `json:"viewerUrl,omitempty"`
	DetailURL       string `json:"detailUrl,omitempty"`
	EvidencePath    string `json:"evidencePath,omitempty"`
	ElapsedMs       int64  `json:"elapsedMs"`
	Error           string `json:"error,omitempty"`
	FailureCategory string `json:"failureCategory,omitempty"`
	StartedAt       string `json:"startedAt,omitempty"`
	FinishedAt      string `json:"finishedAt,omitempty"`
}

type apiCaseBatchNodeReport struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Operation   string `json:"operation,omitempty"`
	Method      string `json:"method,omitempty"`
	Path        string `json:"path,omitempty"`
}

type apiCaseBatchRunReport struct {
	OK                   bool                       `json:"ok"`
	BatchRunID           string                     `json:"batchRunId"`
	RequestID            string                     `json:"requestId"`
	EnvironmentID        string                     `json:"environmentId,omitempty"`
	ProfileID            string                     `json:"profileId"`
	CaseIDs              []string                   `json:"caseIds,omitempty"`
	NodeIDs              []string                   `json:"nodeIds"`
	WorkflowID           string                     `json:"workflowId,omitempty"`
	Suite                *apiCaseBatchSuiteSelector `json:"suite,omitempty"`
	Status               string                     `json:"status"`
	Total                int                        `json:"total"`
	Completed            int                        `json:"completed"`
	Passed               int                        `json:"passed"`
	Failed               int                        `json:"failed"`
	Skipped              int                        `json:"skipped"`
	ReportURL            string                     `json:"reportUrl,omitempty"`
	StartedAt            string                     `json:"startedAt"`
	FinishedAt           string                     `json:"finishedAt,omitempty"`
	Nodes                []apiCaseBatchNodeReport   `json:"nodes,omitempty"`
	Cases                []apiCaseBatchCaseReport   `json:"cases"`
	Acceptance           workflowAcceptanceReport   `json:"acceptance,omitempty"`
	Error                string                     `json:"error,omitempty"`
	HTMLReportPath       string                     `json:"htmlReportPath,omitempty"`
	HTMLReportURL        string                     `json:"htmlReportUrl,omitempty"`
	JUnitReportPath      string                     `json:"junitReportPath,omitempty"`
	JUnitReportURL       string                     `json:"junitReportUrl,omitempty"`
	ArtifactManifestPath string                     `json:"artifactManifestPath,omitempty"`
	ArtifactManifestURL  string                     `json:"artifactManifestUrl,omitempty"`
	FailureSummaryPath   string                     `json:"failureSummaryPath,omitempty"`
	FailureSummaryURL    string                     `json:"failureSummaryUrl,omitempty"`
}

type apiCaseBatchArtifactManifest struct {
	OK          bool                   `json:"ok"`
	BatchRunID  string                 `json:"batchRunId"`
	RequestID   string                 `json:"requestId"`
	ProfileID   string                 `json:"profileId"`
	Status      string                 `json:"status"`
	GeneratedAt string                 `json:"generatedAt"`
	Artifacts   []apiCaseBatchArtifact `json:"artifacts"`
}

type apiCaseBatchArtifact struct {
	Kind      string `json:"kind"`
	CaseID    string `json:"caseId,omitempty"`
	CaseRunID string `json:"caseRunId,omitempty"`
	URL       string `json:"url,omitempty"`
	Path      string `json:"path,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
}

type apiCaseBatchFailureSummary struct {
	OK          bool                     `json:"ok"`
	BatchRunID  string                   `json:"batchRunId"`
	RequestID   string                   `json:"requestId"`
	ProfileID   string                   `json:"profileId"`
	Status      string                   `json:"status"`
	Failed      int                      `json:"failed"`
	GeneratedAt string                   `json:"generatedAt"`
	Failures    []apiCaseBatchCaseReport `json:"failures"`
}

//go:embed templates/api_case_batch_report.html
var apiCaseBatchReportTemplateSource string

var apiCaseBatchReportTemplate = template.Must(template.New("api-case-batch-report").Parse(apiCaseBatchReportTemplateSource))

type apiCaseBatchRunner struct {
	mu   sync.RWMutex
	runs map[string]apiCaseBatchRunReport
}

func newAPICaseBatchRunner() *apiCaseBatchRunner {
	return &apiCaseBatchRunner{runs: map[string]apiCaseBatchRunReport{}}
}

func handleAPICaseBatchRunStart(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store, runner *apiCaseBatchRunner, collector traceCollector) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	request := apiCaseBatchRunRequest{
		RequestID:      strings.TrimSpace(valueString(payload["requestId"])),
		EnvironmentID:  strings.TrimSpace(valueString(payload["environmentId"])),
		CaseIDs:        stringListValue(payload["caseIds"]),
		NodeIDs:        stringListValue(payload["nodeIds"]),
		WorkflowID:     strings.TrimSpace(valueString(payload["workflowId"])),
		Suite:          apiCaseBatchSuiteSelectorValue(payload["suite"]),
		BaseURL:        strings.TrimSpace(valueString(payload["baseUrl"])),
		EvidenceDir:    strings.TrimSpace(valueString(payload["evidenceDir"])),
		TimeoutSeconds: intValue(payload["timeoutSeconds"]),
		Overrides:      mapValue(payload["overrides"]),
	}
	report, status, err := startAPICaseBatchRun(r.Context(), bundle, runtime, runner, request, collector)
	if err != nil {
		writeJSONStatus(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSONStatus(w, http.StatusAccepted, report)
}

func startAPICaseBatchRun(ctx context.Context, bundle profile.Bundle, runtime store.Store, runner *apiCaseBatchRunner, request apiCaseBatchRunRequest, collector traceCollector) (apiCaseBatchRunReport, int, error) {
	if request.RequestID == "" {
		return apiCaseBatchRunReport{}, http.StatusBadRequest, errors.New("requestId is required")
	}
	request.CaseIDs = compactUniqueStringListPreserveOrder(request.CaseIDs)
	request.NodeIDs = compactUniqueStringList(request.NodeIDs)
	request.Suite = normalizeAPICaseBatchSuiteSelector(request.Suite)
	if len(request.CaseIDs) == 0 && len(request.NodeIDs) == 0 && request.WorkflowID == "" && !request.Suite.configured() {
		return apiCaseBatchRunReport{}, http.StatusBadRequest, errors.New("caseIds, nodeIds, workflowId, or suite is required")
	}
	plans, err := apiCaseBatchPlans(ctx, bundle, runtime, request)
	if err != nil {
		return apiCaseBatchRunReport{}, http.StatusInternalServerError, err
	}
	if len(plans) == 0 {
		return apiCaseBatchRunReport{}, http.StatusBadRequest, errors.New("no api cases matched selector")
	}

	batchRunID := newAPICaseBatchRunID(request.RequestID)
	now := time.Now().UTC()
	report := apiCaseBatchRunReport{
		OK:                   true,
		BatchRunID:           batchRunID,
		RequestID:            request.RequestID,
		EnvironmentID:        request.EnvironmentID,
		ProfileID:            bundle.ID,
		CaseIDs:              request.CaseIDs,
		NodeIDs:              request.NodeIDs,
		WorkflowID:           request.WorkflowID,
		Status:               store.StatusRunning,
		Total:                len(plans),
		ReportURL:            "/api/cases/batch-runs/" + url.PathEscape(batchRunID),
		StartedAt:            now.Format(time.RFC3339Nano),
		Nodes:                apiCaseBatchNodesFromPlans(plans),
		Cases:                make([]apiCaseBatchCaseReport, 0, len(plans)),
		HTMLReportPath:       filepath.Join(apiCaseBatchReportDir(request, plans), batchRunID, "report.html"),
		HTMLReportURL:        "/api/cases/batch-runs/" + url.PathEscape(batchRunID) + "/report.html",
		JUnitReportPath:      filepath.Join(apiCaseBatchReportDir(request, plans), batchRunID, "report.junit.xml"),
		JUnitReportURL:       "/api/cases/batch-runs/" + url.PathEscape(batchRunID) + "/report.junit.xml",
		ArtifactManifestPath: filepath.Join(apiCaseBatchReportDir(request, plans), batchRunID, "artifacts.json"),
		ArtifactManifestURL:  "/api/cases/batch-runs/" + url.PathEscape(batchRunID) + "/artifacts.json",
		FailureSummaryPath:   filepath.Join(apiCaseBatchReportDir(request, plans), batchRunID, "failures.json"),
		FailureSummaryURL:    "/api/cases/batch-runs/" + url.PathEscape(batchRunID) + "/failures.json",
	}
	if request.Suite.configured() {
		suite := request.Suite
		report.Suite = &suite
	}
	for _, plan := range plans {
		report.Cases = append(report.Cases, apiCaseBatchCaseReport{
			CaseID:          plan.ID,
			DisplayName:     plan.DisplayName,
			Scenario:        plan.Scenario,
			NodeID:          plan.NodeID,
			NodeDisplayName: plan.NodeDisplayName,
			Operation:       plan.Operation,
			Method:          plan.Method,
			Path:            plan.Path,
			StepID:          plan.StepID,
			Status:          store.StatusRunning,
		})
	}
	if err := writeAPICaseBatchHTMLReport(report); err != nil {
		return apiCaseBatchRunReport{}, http.StatusInternalServerError, err
	}
	if err := writeAPICaseBatchJUnitReport(report); err != nil {
		return apiCaseBatchRunReport{}, http.StatusInternalServerError, err
	}
	if err := writeAPICaseBatchArtifactManifest(report); err != nil {
		return apiCaseBatchRunReport{}, http.StatusInternalServerError, err
	}
	if err := writeAPICaseBatchFailureSummary(report); err != nil {
		return apiCaseBatchRunReport{}, http.StatusInternalServerError, err
	}
	runner.save(report)

	go runner.run(context.Background(), batchRunID, bundle, request.EnvironmentID, request.WorkflowID, plans, runtime, bundle.FailureCategories, collector)
	return report, http.StatusAccepted, nil
}

func handleAPICaseBatchRunReport(w http.ResponseWriter, r *http.Request, runner *apiCaseBatchRunner) {
	idValue := strings.TrimPrefix(r.URL.Path, "/api/cases/batch-runs/")
	wantsHTML := strings.HasSuffix(idValue, "/report.html")
	wantsJUnit := strings.HasSuffix(idValue, "/report.junit.xml")
	wantsArtifacts := strings.HasSuffix(idValue, "/artifacts.json")
	wantsFailures := strings.HasSuffix(idValue, "/failures.json")
	if wantsHTML {
		idValue = strings.TrimSuffix(idValue, "/report.html")
	}
	if wantsJUnit {
		idValue = strings.TrimSuffix(idValue, "/report.junit.xml")
	}
	if wantsArtifacts {
		idValue = strings.TrimSuffix(idValue, "/artifacts.json")
	}
	if wantsFailures {
		idValue = strings.TrimSuffix(idValue, "/failures.json")
	}
	id, err := url.PathUnescape(idValue)
	if err != nil || strings.TrimSpace(id) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "batchRunId is required"})
		return
	}
	report, ok := runner.get(id)
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"ok": false, "error": "batch run not found"})
		return
	}
	if wantsHTML {
		http.ServeFile(w, r, report.HTMLReportPath)
		return
	}
	if wantsJUnit {
		http.ServeFile(w, r, report.JUnitReportPath)
		return
	}
	if wantsArtifacts {
		http.ServeFile(w, r, report.ArtifactManifestPath)
		return
	}
	if wantsFailures {
		http.ServeFile(w, r, report.FailureSummaryPath)
		return
	}
	writeJSON(w, report)
}

func (r *apiCaseBatchRunner) run(ctx context.Context, batchRunID string, bundle profile.Bundle, environmentID string, workflowID string, plans []apiCaseBatchCasePlan, runtime store.Store, rules []profile.FailureCategoryRule, collector traceCollector) {
	workflowOverrides := map[string]any{}
	for index, plan := range plans {
		caseCtx := ctx
		var cancel context.CancelFunc
		if plan.TimeoutSeconds > 0 {
			caseCtx, cancel = context.WithTimeout(ctx, time.Duration(plan.TimeoutSeconds)*time.Second)
		}
		casePath := plan.CasePath
		baseURL := plan.BaseURL
		overrides := mergeStringAnyMaps(workflowOverrides, plan.Overrides)
		if plan.Execution != nil {
			plan.Overrides = overrides
			materializedPath, materializedBaseURL, err := materializeAPICaseBatchExecution(caseCtx, bundle, runtime, batchRunID, workflowID, plan)
			if err != nil {
				if cancel != nil {
					cancel()
				}
				item := apiCaseBatchCaseReport{
					CaseID:          plan.ID,
					DisplayName:     plan.DisplayName,
					Scenario:        plan.Scenario,
					NodeID:          plan.NodeID,
					NodeDisplayName: plan.NodeDisplayName,
					Operation:       plan.Operation,
					Method:          plan.Method,
					Path:            plan.Path,
					StepID:          plan.StepID,
					Status:          store.StatusFailed,
					Error:           err.Error(),
				}
				item.FailureCategory = apiCaseBatchApplyFailureCategoryRules(rules, item.Status, apiCaseBatchFailureCategoryFromError(err), item.Error)
				r.updateCase(batchRunID, index, item)
				continue
			}
			casePath = materializedPath
			baseURL = materializedBaseURL
			overrides = nil
		}
		result, err := apicase.Run(caseCtx, apicase.RunOptions{
			CasePath:    casePath,
			BaseURL:     baseURL,
			EvidenceDir: plan.EvidenceDir,
			RunID:       apiCaseBatchCaseRunID(batchRunID, plan.StepID, plan.ID),
			Overrides:   overrides,
		})
		if cancel != nil {
			cancel()
		}
		item := apiCaseBatchCaseReport{
			CaseID:          plan.ID,
			DisplayName:     plan.DisplayName,
			Scenario:        plan.Scenario,
			NodeID:          plan.NodeID,
			NodeDisplayName: plan.NodeDisplayName,
			Operation:       plan.Operation,
			Method:          plan.Method,
			Path:            plan.Path,
			StepID:          plan.StepID,
			Status:          store.StatusFailed,
		}
		if err != nil {
			item.Error = err.Error()
			item.FailureCategory = apiCaseBatchApplyFailureCategoryRules(rules, item.Status, apiCaseBatchFailureCategoryFromError(err), item.Error)
		} else {
			item.RunID = result.RunID
			item.CaseRunID = apiCaseRunRecordID(result.RunID)
			item.Status = result.Status
			item.ViewerURL = apiCaseViewerURL(result)
			item.DetailURL = apiCaseEvidenceDetailURL(item.CaseRunID)
			item.EvidencePath = result.EvidencePath
			item.ElapsedMs = result.ElapsedMs
			item.StartedAt = result.StartedAt
			item.FinishedAt = result.FinishedAt
			item.Error = apiCaseBatchFailureMessage(result)
			item.FailureCategory = apiCaseBatchApplyFailureCategoryRules(rules, item.Status, apiCaseBatchFailureCategory(result), item.Error)
			if runtime != nil {
				if err := recordAPICaseRunWithContext(ctx, runtime, recordAPICaseRunContext{
					ProfileID:     bundle.ID,
					EnvironmentID: environmentID,
					WorkflowID:    workflowID,
					StepID:        plan.StepID,
				}, result); err != nil {
					item.Status = store.StatusFailed
					item.Error = err.Error()
					item.FailureCategory = apiCaseBatchApplyFailureCategoryRules(rules, item.Status, apiCaseBatchFailureCategoryFromError(err), item.Error)
				}
				if item.Status == store.StatusPassed && strings.TrimSpace(workflowID) != "" && strings.TrimSpace(plan.StepID) != "" {
					collectAPICaseBatchTraceTopology(ctx, runtime, collector, workflowID, plan, result)
				}
			}
			if item.Status == store.StatusPassed {
				workflowOverrides = mergeStringAnyMaps(workflowOverrides, apiCaseBatchEvidenceOverridesForPlan(plan, result.EvidencePath))
			}
		}
		r.updateCase(batchRunID, index, item)
	}
	r.finish(ctx, batchRunID, bundle.ID, workflowID, runtime)
}

func materializeAPICaseBatchExecution(ctx context.Context, bundle profile.Bundle, runtime store.Store, batchRunID string, workflowID string, plan apiCaseBatchCasePlan) (string, string, error) {
	payload := map[string]any{
		"caseId":     plan.ID,
		"stepId":     plan.StepID,
		"workflowId": workflowID,
		"baseUrl":    plan.BaseURL,
		"overrides":  plan.Overrides,
	}
	request, err := buildCaseHTTPRequest(ctx, bundle, runtime, *plan.Execution, plan.BaseURL, payload)
	if err != nil {
		return "", "", err
	}
	if err := applyAPICaseRequestModel(&request, plan.Case); err != nil {
		return "", "", err
	}
	body := mapFromAny(request.body)
	apiCase := apicase.Case{
		ID:    plan.ID,
		Title: firstNonEmpty(plan.DisplayName, plan.ID),
		Request: apicase.Request{
			Method:  request.method,
			Path:    apiCaseBatchRequestPath(request),
			Headers: request.headers,
			Body:    body,
		},
		Assertions: apicase.Assertions{
			ExpectedStatusCodes: request.expectedHTTPCodes,
			ResponseContains:    request.expectedResponse,
		},
	}
	dir := filepath.Join(".runtime", "case-batches", batchRunID, "materialized-cases")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	path := filepath.Join(dir, safeRuntimeLogPathSegment(firstNonEmpty(plan.StepID, plan.ID))+".json")
	raw, err := json.MarshalIndent(apiCase, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return "", "", err
	}
	return path, request.baseURL, nil
}

func apiCaseBatchRequestPath(request caseHTTPRequest) string {
	baseURL := strings.TrimRight(strings.TrimSpace(request.baseURL), "/")
	fullURL := strings.TrimSpace(request.fullURL)
	if baseURL != "" && strings.HasPrefix(fullURL, baseURL) {
		path := strings.TrimSpace(strings.TrimPrefix(fullURL, baseURL))
		if path != "" {
			return path
		}
	}
	if strings.TrimSpace(request.path) != "" {
		return request.path
	}
	return "/"
}

func apiCaseBatchEvidenceOverridesForPlan(plan apiCaseBatchCasePlan, evidencePath string) map[string]any {
	out := apiCaseBatchEvidenceOverrides(evidencePath)
	request, _ := jsonFileObject(filepath.Join(evidencePath, "request.json"))
	response, _ := jsonFileObject(filepath.Join(evidencePath, "response.json"))
	requestBody := apiCaseBatchJSONBody(request)
	responseBody := apiCaseBatchJSONBody(response)
	for _, export := range plan.Exports {
		name := apiCaseBatchOverrideKey(valueString(export["name"]))
		if name == "" {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(valueString(export["from"])))
		path := strings.TrimSpace(valueString(export["path"]))
		var root any
		switch source {
		case "requestbody", "request.body":
			root = requestBody
		case "responsebody", "response.body":
			root = responseBody
		case "request":
			root = request
		case "response":
			root = responseBody
			if root == nil {
				root = response
			}
		default:
			root = responseBody
		}
		value, ok := apiCaseBatchPathValue(root, path)
		if !ok {
			continue
		}
		text := strings.TrimSpace(apiCaseBatchOverrideValueString(value))
		if text != "" {
			out[name] = text
		}
	}
	return out
}

func apiCaseBatchJSONBody(payload map[string]any) any {
	body := strings.TrimSpace(valueString(payload["body"]))
	if body == "" {
		return nil
	}
	var parsed any
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&parsed) != nil {
		return nil
	}
	return parsed
}

func apiCaseBatchPathValue(root any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return root, root != nil
	}
	current := root
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]any:
			value, ok := typed[part]
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func apiCaseBatchEvidenceOverrides(evidencePath string) map[string]any {
	out := map[string]any{}
	for _, name := range []string{"request.json", "response.json"} {
		payload, _ := jsonFileObject(filepath.Join(evidencePath, name))
		collectAPICaseBatchOverrideFields(out, payload)
		if body := strings.TrimSpace(valueString(payload["body"])); body != "" {
			var parsed any
			decoder := json.NewDecoder(strings.NewReader(body))
			decoder.UseNumber()
			if decoder.Decode(&parsed) == nil {
				collectAPICaseBatchOverrideFields(out, parsed)
			}
		}
	}
	return out
}

func collectAPICaseBatchOverrideFields(out map[string]any, value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			recordAPICaseBatchOverrideField(out, key, item)
			collectAPICaseBatchOverrideFields(out, item)
		}
	case []any:
		for _, item := range typed {
			collectAPICaseBatchOverrideFields(out, item)
		}
	case map[string]string:
		for key, item := range typed {
			recordAPICaseBatchOverrideField(out, key, item)
		}
	}
}

func recordAPICaseBatchOverrideField(out map[string]any, key string, value any) {
	normalized := apiCaseBatchOverrideKey(key)
	if normalized == "" {
		return
	}
	text := strings.TrimSpace(apiCaseBatchOverrideValueString(value))
	if text == "" {
		return
	}
	out[normalized] = text
}

func apiCaseBatchOverrideValueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		asInt := int64(typed)
		if typed == float64(asInt) {
			return strconv.FormatInt(asInt, 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		asInt := int64(typed)
		if typed == float32(asInt) {
			return strconv.FormatInt(asInt, 10)
		}
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	default:
		return valueString(value)
	}
}

func apiCaseBatchOverrideKey(key string) string {
	return normalizeAPICaseBatchOverrideKey(strings.TrimSpace(key))
}

func normalizeAPICaseBatchOverrideKey(key string) string {
	if key == "" {
		return ""
	}
	runes := []rune(key)
	var out strings.Builder
	var previousUnderscore bool
	for index, char := range runes {
		switch {
		case isAPICaseBatchLower(char):
			out.WriteRune(char)
			previousUnderscore = false
		case isAPICaseBatchUpper(char):
			previous := rune(0)
			if index > 0 {
				previous = runes[index-1]
			}
			next := rune(0)
			if index+1 < len(runes) {
				next = runes[index+1]
			}
			if index > 0 && !previousUnderscore && (isAPICaseBatchLower(previous) || isAPICaseBatchDigit(previous) || isAPICaseBatchLower(next)) {
				out.WriteByte('_')
			}
			out.WriteRune(char + ('a' - 'A'))
			previousUnderscore = false
		case isAPICaseBatchDigit(char):
			out.WriteRune(char)
			previousUnderscore = false
		case char == '_' || char == '-' || char == ' ':
			if out.Len() > 0 && !previousUnderscore {
				out.WriteByte('_')
				previousUnderscore = true
			}
		default:
			return ""
		}
	}
	normalized := strings.Trim(out.String(), "_")
	if normalized == "" {
		return ""
	}
	return normalized
}

func isAPICaseBatchLower(char rune) bool {
	return char >= 'a' && char <= 'z'
}

func isAPICaseBatchUpper(char rune) bool {
	return char >= 'A' && char <= 'Z'
}

func isAPICaseBatchDigit(char rune) bool {
	return char >= '0' && char <= '9'
}

func collectAPICaseBatchTraceTopology(ctx context.Context, runtime store.Store, collector traceCollector, workflowID string, plan apiCaseBatchCasePlan, result apicase.RunResult) {
	if runtime == nil || result.Status != store.StatusPassed {
		return
	}
	request, _ := jsonFileObject(filepath.Join(result.EvidencePath, "request.json"))
	response, _ := jsonFileObject(filepath.Join(result.EvidencePath, "response.json"))
	payload := map[string]any{
		"workflowId": workflowID,
		"stepId":     plan.StepID,
	}
	if plan.Execution != nil && strings.TrimSpace(plan.Execution.TraceEndpoint) != "" {
		payload["traceEndpoint"] = plan.Execution.TraceEndpoint
	}
	resultPayload := map[string]any{
		"ok":         true,
		"caseId":     result.CaseID,
		"stepId":     plan.StepID,
		"startedAt":  result.StartedAt,
		"finishedAt": result.FinishedAt,
		"result": map[string]any{
			"request":  request,
			"response": response,
		},
	}
	collectAndRecordTestKitTraceTopology(ctx, runtime, collector, result.RunID, payload, resultPayload)
}

func copyAPICaseBatchTraceTopologies(ctx context.Context, runtime store.Store, report apiCaseBatchRunReport) {
	if runtime == nil || strings.TrimSpace(report.BatchRunID) == "" {
		return
	}
	for _, item := range report.Cases {
		sourceRunID := strings.TrimSpace(item.RunID)
		if sourceRunID == "" {
			continue
		}
		rows, err := runtime.ListTraceTopologies(ctx, sourceRunID)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if !isSkyWalkingTraceTopology(row) {
				continue
			}
			copied := row
			copied.ID = report.BatchRunID + "." + safeRuntimeLogPathSegment(firstNonEmpty(item.StepID, row.StepID, item.CaseID, row.CaseID)) + ".topology.skywalking"
			copied.WorkflowRunID = report.BatchRunID
			copied.WorkflowID = firstNonEmpty(report.WorkflowID, row.WorkflowID)
			copied.StepID = firstNonEmpty(item.StepID, row.StepID)
			copied.CaseID = firstNonEmpty(item.CaseID, row.CaseID)
			copied.CreatedAt = time.Now().UTC()
			_, _ = runtime.SaveTraceTopology(ctx, copied)
		}
	}
}

func (r *apiCaseBatchRunner) save(report apiCaseBatchRunReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[report.BatchRunID] = cloneAPICaseBatchReport(report)
}

func (r *apiCaseBatchRunner) get(id string) (apiCaseBatchRunReport, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	report, ok := r.runs[id]
	return cloneAPICaseBatchReport(report), ok
}

func (r *apiCaseBatchRunner) updateCase(batchRunID string, index int, item apiCaseBatchCaseReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	report := r.runs[batchRunID]
	if index >= 0 && index < len(report.Cases) {
		report.Cases[index] = item
	}
	refreshAPICaseBatchCounts(&report)
	_ = writeAPICaseBatchHTMLReport(report)
	_ = writeAPICaseBatchJUnitReport(report)
	_ = writeAPICaseBatchArtifactManifest(report)
	_ = writeAPICaseBatchFailureSummary(report)
	r.runs[batchRunID] = report
}

func (r *apiCaseBatchRunner) finish(ctx context.Context, batchRunID string, profileID string, workflowID string, runtime store.Store) {
	r.mu.Lock()
	report := r.runs[batchRunID]
	refreshAPICaseBatchCounts(&report)
	if report.Failed > 0 {
		report.Status = store.StatusFailed
		report.OK = false
	} else {
		report.Status = store.StatusPassed
		report.OK = true
	}
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if strings.TrimSpace(report.WorkflowID) != "" {
		report.Acceptance = buildWorkflowAcceptanceReport(ctx, runtime, report)
	}
	_ = writeAPICaseBatchHTMLReport(report)
	_ = writeAPICaseBatchJUnitReport(report)
	_ = writeAPICaseBatchArtifactManifest(report)
	_ = writeAPICaseBatchFailureSummary(report)
	recordAPICaseBatchReportArtifacts(ctx, runtime, profileID, workflowID, report)
	if strings.TrimSpace(report.WorkflowID) != "" {
		copyAPICaseBatchTraceTopologies(ctx, runtime, report)
	}
	finalizeEnvironmentAcceptanceRun(ctx, runtime, report)
	r.runs[batchRunID] = report
	r.mu.Unlock()
}

func recordAPICaseBatchReportArtifacts(ctx context.Context, runtime store.Store, profileID string, workflowID string, report apiCaseBatchRunReport) {
	if runtime == nil || strings.TrimSpace(report.BatchRunID) == "" {
		return
	}
	startedAt := parseAPICaseBatchReportTime(report.StartedAt, time.Now().UTC())
	finishedAt := parseAPICaseBatchReportTime(report.FinishedAt, time.Now().UTC())
	if finishedAt.Before(startedAt) {
		finishedAt = startedAt
	}
	evidenceRoot := strings.TrimSpace(filepath.Dir(report.HTMLReportPath))
	if evidenceRoot == "." {
		evidenceRoot = strings.TrimSpace(filepath.Dir(report.ArtifactManifestPath))
	}
	if evidenceRoot == "." {
		evidenceRoot = ""
	}
	if _, err := runtime.CreateRun(ctx, store.Run{
		ID:            report.BatchRunID,
		ProfileID:     strings.TrimSpace(profileID),
		EnvironmentID: strings.TrimSpace(report.EnvironmentID),
		WorkflowID:    strings.TrimSpace(workflowID),
		Status:        report.Status,
		EvidenceRoot:  evidenceRoot,
		SummaryJSON:   compactJSON(apiCaseBatchRunStoreSummary(report)),
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		CreatedAt:     startedAt,
		UpdatedAt:     finishedAt,
	}); err != nil {
		return
	}
	for _, artifact := range apiCaseBatchReportEvidenceArtifacts(report) {
		info, err := os.Stat(artifact.Path)
		if err != nil {
			continue
		}
		_, _ = runtime.RecordEvidence(ctx, store.EvidenceRecord{
			ID:         report.BatchRunID + ".report." + artifact.Kind,
			RunID:      report.BatchRunID,
			Kind:       artifact.Kind,
			URI:        artifact.Path,
			MediaType:  artifact.MediaType,
			SizeBytes:  info.Size(),
			Summary:    artifact.Summary,
			Category:   "report",
			Visibility: "public",
			LabelsJSON: compactJSON(map[string]any{
				"batchRunId": report.BatchRunID,
				"requestId":  report.RequestID,
				"kind":       artifact.Kind,
			}),
			CreatedAt: finishedAt,
		})
	}
}

func apiCaseBatchRunStoreSummary(report apiCaseBatchRunReport) map[string]any {
	out := jsonObject(compactJSON(report))
	steps := make([]map[string]any, 0, len(report.Cases))
	for _, item := range report.Cases {
		step := map[string]any{
			"stepId":    item.StepID,
			"caseId":    item.CaseID,
			"nodeId":    item.NodeID,
			"status":    item.Status,
			"elapsedMs": item.ElapsedMs,
		}
		if item.RunID != "" {
			step["runId"] = item.RunID
		}
		if item.CaseRunID != "" {
			step["caseRunId"] = item.CaseRunID
		}
		if item.Error != "" {
			step["error"] = item.Error
		}
		if item.FailureCategory != "" {
			step["failureCategory"] = item.FailureCategory
		}
		steps = append(steps, step)
	}
	out["steps"] = steps
	out["summary"] = map[string]any{
		"expectedStepCount": report.Total,
		"stepCount":         report.Completed,
		"passed":            report.Passed,
		"failed":            report.Failed,
		"skipped":           report.Skipped,
	}
	return out
}

type apiCaseBatchReportEvidenceArtifact struct {
	Kind      string
	Path      string
	MediaType string
	Summary   string
}

func apiCaseBatchReportEvidenceArtifacts(report apiCaseBatchRunReport) []apiCaseBatchReportEvidenceArtifact {
	return []apiCaseBatchReportEvidenceArtifact{
		{Kind: "html", Path: report.HTMLReportPath, MediaType: "text/html", Summary: "API case batch HTML report"},
		{Kind: "junit", Path: report.JUnitReportPath, MediaType: "application/xml", Summary: "API case batch JUnit report"},
		{Kind: "artifact-manifest", Path: report.ArtifactManifestPath, MediaType: "application/json", Summary: "API case batch artifact manifest"},
		{Kind: "failure-summary", Path: report.FailureSummaryPath, MediaType: "application/json", Summary: "API case batch failure summary"},
	}
}

func parseAPICaseBatchReportTime(value string, defaultValue time.Time) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return defaultValue
	}
	return parsed.UTC()
}

func writeAPICaseBatchHTMLReport(report apiCaseBatchRunReport) error {
	if strings.TrimSpace(report.HTMLReportPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(report.HTMLReportPath), 0o755); err != nil {
		return err
	}
	var rendered bytes.Buffer
	if err := apiCaseBatchReportTemplate.Execute(&rendered, report); err != nil {
		return err
	}
	return os.WriteFile(report.HTMLReportPath, rendered.Bytes(), 0o644)
}

func writeAPICaseBatchJUnitReport(report apiCaseBatchRunReport) error {
	if strings.TrimSpace(report.JUnitReportPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(report.JUnitReportPath), 0o755); err != nil {
		return err
	}
	raw, err := renderAPICaseBatchJUnit(report)
	if err != nil {
		return err
	}
	return os.WriteFile(report.JUnitReportPath, raw, 0o644)
}

func renderAPICaseBatchJUnit(report apiCaseBatchRunReport) ([]byte, error) {
	cases := make([]junit.Case, 0, len(report.Cases))
	for _, item := range report.Cases {
		cases = append(cases, junit.Case{
			Name:           item.CaseID,
			ClassName:      item.NodeID,
			Status:         item.Status,
			TimeSeconds:    float64(item.ElapsedMs) / 1000,
			FailureMessage: item.Error,
			Output:         item.Error,
		})
	}
	return junit.Render(junit.Suite{Name: "API Case Batch " + report.RequestID, Cases: cases})
}

func writeAPICaseBatchArtifactManifest(report apiCaseBatchRunReport) error {
	if strings.TrimSpace(report.ArtifactManifestPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(report.ArtifactManifestPath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(apiCaseBatchArtifacts(report), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(report.ArtifactManifestPath, append(raw, '\n'), 0o644)
}

func apiCaseBatchArtifacts(report apiCaseBatchRunReport) apiCaseBatchArtifactManifest {
	manifest := apiCaseBatchArtifactManifest{
		OK:          report.OK,
		BatchRunID:  report.BatchRunID,
		RequestID:   report.RequestID,
		ProfileID:   report.ProfileID,
		Status:      report.Status,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Artifacts:   []apiCaseBatchArtifact{},
	}
	manifest.Artifacts = append(manifest.Artifacts,
		apiCaseBatchArtifact{Kind: "json", URL: report.ReportURL, MediaType: "application/json"},
		apiCaseBatchArtifact{Kind: "html", URL: report.HTMLReportURL, Path: report.HTMLReportPath, MediaType: "text/html"},
		apiCaseBatchArtifact{Kind: "junit", URL: report.JUnitReportURL, Path: report.JUnitReportPath, MediaType: "application/xml"},
		apiCaseBatchArtifact{Kind: "artifact-manifest", URL: report.ArtifactManifestURL, Path: report.ArtifactManifestPath, MediaType: "application/json"},
		apiCaseBatchArtifact{Kind: "failure-summary", URL: report.FailureSummaryURL, Path: report.FailureSummaryPath, MediaType: "application/json"},
	)
	for _, item := range report.Cases {
		if strings.TrimSpace(item.DetailURL) != "" {
			manifest.Artifacts = append(manifest.Artifacts, apiCaseBatchArtifact{
				Kind:      "case-detail",
				CaseID:    item.CaseID,
				CaseRunID: item.CaseRunID,
				URL:       item.DetailURL,
				MediaType: "application/json",
			})
		}
		if strings.TrimSpace(item.EvidencePath) != "" {
			manifest.Artifacts = append(manifest.Artifacts, apiCaseBatchArtifact{
				Kind:      "case-evidence",
				CaseID:    item.CaseID,
				CaseRunID: item.CaseRunID,
				Path:      item.EvidencePath,
			})
		}
	}
	return manifest
}

func writeAPICaseBatchFailureSummary(report apiCaseBatchRunReport) error {
	if strings.TrimSpace(report.FailureSummaryPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(report.FailureSummaryPath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(apiCaseBatchFailures(report), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(report.FailureSummaryPath, append(raw, '\n'), 0o644)
}

func apiCaseBatchFailures(report apiCaseBatchRunReport) apiCaseBatchFailureSummary {
	summary := apiCaseBatchFailureSummary{
		OK:          report.Failed == 0,
		BatchRunID:  report.BatchRunID,
		RequestID:   report.RequestID,
		ProfileID:   report.ProfileID,
		Status:      report.Status,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Failures:    []apiCaseBatchCaseReport{},
	}
	for _, item := range report.Cases {
		if item.Status == store.StatusFailed {
			summary.Failures = append(summary.Failures, item)
		}
	}
	summary.Failed = len(summary.Failures)
	summary.OK = summary.Failed == 0
	return summary
}

func apiCaseBatchFailureMessage(result apicase.RunResult) string {
	if result.Status != store.StatusFailed {
		return ""
	}
	if strings.TrimSpace(result.EvidencePath) == "" {
		return "case run failed"
	}
	raw, err := os.ReadFile(filepath.Join(result.EvidencePath, "assertions.json"))
	if err != nil {
		return "case run failed"
	}
	var assertions apicase.AssertionEvidence
	if err := json.Unmarshal(raw, &assertions); err != nil {
		return "case run failed"
	}
	if len(assertions.Errors) == 0 {
		return "case run failed"
	}
	return strings.Join(assertions.Errors, "; ")
}

func apiCaseBatchFailureCategory(result apicase.RunResult) string {
	if result.Status != store.StatusFailed {
		return ""
	}
	if strings.TrimSpace(result.EvidencePath) == "" {
		return "case-failure"
	}
	raw, err := os.ReadFile(filepath.Join(result.EvidencePath, "assertions.json"))
	if err != nil {
		return "case-failure"
	}
	var assertions apicase.AssertionEvidence
	if err := json.Unmarshal(raw, &assertions); err != nil {
		return "case-failure"
	}
	if len(assertions.Errors) == 0 {
		return "case-failure"
	}
	return "assertion-mismatch"
}

func apiCaseBatchFailureCategoryFromError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "context deadline exceeded"), strings.Contains(message, "timeout"):
		return "timeout"
	case strings.Contains(message, "base url"), strings.Contains(message, "send request"), strings.Contains(message, "create request"), strings.Contains(message, "parse"):
		return "transport-error"
	default:
		return "case-failure"
	}
}

func apiCaseBatchApplyFailureCategoryRules(rules []profile.FailureCategoryRule, status string, defaultCategory string, message string) string {
	if strings.TrimSpace(defaultCategory) == "" {
		return ""
	}
	for _, rule := range rules {
		if apiCaseBatchFailureCategoryRuleMatches(rule, status, defaultCategory, message) {
			return firstNonEmpty(rule.Category, rule.Name, defaultCategory)
		}
	}
	return defaultCategory
}

func apiCaseBatchFailureCategoryRuleMatches(rule profile.FailureCategoryRule, status string, defaultCategory string, message string) bool {
	matcher := rule.Matchers
	if len(matcher.Statuses) > 0 && !containsFold(matcher.Statuses, status) {
		return false
	}
	if len(matcher.FailureCategories) > 0 && !containsFold(matcher.FailureCategories, defaultCategory) {
		return false
	}
	if len(matcher.MessageContains) > 0 && !containsMessageFragment(matcher.MessageContains, message) {
		return false
	}
	return len(matcher.Statuses) > 0 || len(matcher.FailureCategories) > 0 || len(matcher.MessageContains) > 0
}

func containsFold(values []string, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(strings.ToLower(value)) == want {
			return true
		}
	}
	return false
}

func containsMessageFragment(fragments []string, message string) bool {
	message = strings.ToLower(message)
	for _, fragment := range fragments {
		fragment = strings.TrimSpace(strings.ToLower(fragment))
		if fragment != "" && strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func apiCaseBatchPlans(ctx context.Context, bundle profile.Bundle, runtime store.Store, request apiCaseBatchRunRequest) ([]apiCaseBatchCasePlan, error) {
	if len(request.CaseIDs) > 0 {
		return apiCaseBatchExactCasePlans(ctx, bundle, runtime, request), nil
	}
	if strings.TrimSpace(request.WorkflowID) != "" {
		return apiCaseBatchWorkflowPlans(ctx, bundle, runtime, request), nil
	}
	if request.Suite.configured() {
		return apiCaseBatchSuitePlans(ctx, bundle, runtime, request)
	}
	return apiCaseBatchNodePlans(ctx, bundle, runtime, request), nil
}

func apiCaseBatchExactCasePlans(ctx context.Context, bundle profile.Bundle, runtime store.Store, request apiCaseBatchRunRequest) []apiCaseBatchCasePlan {
	casesByID := make(map[string]profile.APICase, len(bundle.APICases))
	for _, item := range bundle.APICases {
		casesByID[item.ID] = item
	}
	cases := make([]profile.APICase, 0, len(request.CaseIDs))
	for _, id := range request.CaseIDs {
		if item, ok := casesByID[id]; ok {
			cases = append(cases, item)
		}
	}
	return apiCaseBatchPlansFromCases(ctx, bundle, runtime, request, cases)
}

func apiCaseBatchNodePlans(ctx context.Context, bundle profile.Bundle, runtime store.Store, request apiCaseBatchRunRequest) []apiCaseBatchCasePlan {
	nodesByID := apiCaseBatchNodesByID(bundle)
	nodeSet := map[string]bool{}
	for _, id := range request.NodeIDs {
		nodeSet[id] = true
	}
	out := make([]apiCaseBatchCasePlan, 0, len(bundle.APICases))
	for _, item := range bundle.APICases {
		if !nodeSet[strings.TrimSpace(item.NodeID)] {
			continue
		}
		casePath := strings.TrimSpace(item.CasePath)
		payload := map[string]any{"caseId": item.ID}
		template := findCaseExecutionTemplateConfig(ctx, runtime, item.ID, payload)
		var execution *caseExecutionConfig
		var exports []map[string]any
		if template != nil {
			execution = &template.CaseExecution
			exports = template.Exports
		}
		if casePath == "" && execution == nil {
			continue
		}
		node := nodesByID[item.NodeID]
		out = append(out, apiCaseBatchCasePlan{
			ID:              item.ID,
			DisplayName:     item.DisplayName,
			Scenario:        item.Scenario,
			NodeID:          item.NodeID,
			NodeDisplayName: node.DisplayName,
			Operation:       node.Operation,
			Method:          apiCaseBatchPlanMethod(node, execution),
			Path:            apiCaseBatchPlanPath(node, execution),
			CasePath:        resolveBatchAPICasePath(ctx, runtime, bundle, casePath),
			BaseURL:         firstNonEmpty(request.BaseURL, item.BaseURL),
			EvidenceDir:     firstNonEmpty(request.EvidenceDir, item.EvidenceDir, filepath.Join(".runtime", "case-batches")),
			TimeoutSeconds:  firstPositive(request.TimeoutSeconds, item.TimeoutSeconds),
			Overrides:       mergeStringAnyMaps(item.DefaultOverrides, request.Overrides),
			Execution:       execution,
			Exports:         exports,
			Case:            item,
		})
	}
	return out
}

func apiCaseBatchSuitePlans(ctx context.Context, bundle profile.Bundle, runtime store.Store, request apiCaseBatchRunRequest) ([]apiCaseBatchCasePlan, error) {
	filter := casesuite.Filter{
		Filter:   request.Suite.Filter,
		NodeID:   request.Suite.NodeID,
		Tags:     request.Suite.Tags,
		Status:   request.Suite.Status,
		Owner:    request.Suite.Owner,
		Priority: request.Suite.Priority,
	}
	cases := casesuite.SelectCases(bundle, filter)
	if len(request.Suite.RunStates) > 0 {
		report, err := casesuite.Coverage(ctx, bundle, runtime, filter, cases)
		if err != nil {
			return nil, err
		}
		stateSet := casesuite.RunStateSet(request.Suite.RunStates)
		filtered := make([]profile.APICase, 0, len(cases))
		for _, item := range report.Items {
			if !stateSet[casesuite.NormalizeRunState(item.LatestStatus)] {
				continue
			}
			if apiCase, ok := findAPICase(bundle.APICases, item.CaseID); ok {
				filtered = append(filtered, apiCase)
			}
		}
		cases = filtered
	}
	return apiCaseBatchPlansFromCases(ctx, bundle, runtime, request, cases), nil
}

func apiCaseBatchPlansFromCases(ctx context.Context, bundle profile.Bundle, runtime store.Store, request apiCaseBatchRunRequest, cases []profile.APICase) []apiCaseBatchCasePlan {
	nodesByID := apiCaseBatchNodesByID(bundle)
	out := make([]apiCaseBatchCasePlan, 0, len(cases))
	for _, item := range cases {
		casePath := strings.TrimSpace(item.CasePath)
		payload := map[string]any{"caseId": item.ID}
		template := findCaseExecutionTemplateConfig(ctx, runtime, item.ID, payload)
		var execution *caseExecutionConfig
		var exports []map[string]any
		if template != nil {
			execution = &template.CaseExecution
			exports = template.Exports
		}
		if casePath == "" && execution == nil {
			continue
		}
		node := nodesByID[item.NodeID]
		out = append(out, apiCaseBatchCasePlan{
			ID:              item.ID,
			DisplayName:     item.DisplayName,
			Scenario:        item.Scenario,
			NodeID:          item.NodeID,
			NodeDisplayName: node.DisplayName,
			Operation:       node.Operation,
			Method:          apiCaseBatchPlanMethod(node, execution),
			Path:            apiCaseBatchPlanPath(node, execution),
			CasePath:        resolveBatchAPICasePath(ctx, runtime, bundle, casePath),
			BaseURL:         firstNonEmpty(request.BaseURL, item.BaseURL),
			EvidenceDir:     firstNonEmpty(request.EvidenceDir, item.EvidenceDir, filepath.Join(".runtime", "case-batches")),
			TimeoutSeconds:  firstPositive(request.TimeoutSeconds, item.TimeoutSeconds),
			Overrides:       mergeStringAnyMaps(item.DefaultOverrides, request.Overrides),
			Execution:       execution,
			Exports:         exports,
			Case:            item,
		})
	}
	return out
}

func apiCaseBatchWorkflowPlans(ctx context.Context, bundle profile.Bundle, runtime store.Store, request apiCaseBatchRunRequest) []apiCaseBatchCasePlan {
	nodesByID := apiCaseBatchNodesByID(bundle)
	casesByID := make(map[string]profile.APICase, len(bundle.APICases))
	for _, item := range bundle.APICases {
		casesByID[item.ID] = item
	}
	bindings := make([]profile.WorkflowBinding, 0, len(bundle.WorkflowBindings))
	for _, binding := range bundle.WorkflowBindings {
		if binding.WorkflowID == request.WorkflowID {
			bindings = append(bindings, binding)
		}
	}
	sort.SliceStable(bindings, func(i, j int) bool {
		if bindings[i].SortOrder != bindings[j].SortOrder {
			return bindings[i].SortOrder < bindings[j].SortOrder
		}
		return bindings[i].StepID < bindings[j].StepID
	})
	out := make([]apiCaseBatchCasePlan, 0, len(bindings))
	for _, binding := range bindings {
		item, ok := casesByID[binding.CaseID]
		if !ok {
			continue
		}
		casePath := strings.TrimSpace(item.CasePath)
		nodeID := firstNonEmpty(binding.NodeID, item.NodeID)
		node := nodesByID[nodeID]
		payload := map[string]any{"caseId": item.ID, "workflowId": request.WorkflowID, "stepId": binding.StepID}
		template := findCaseExecutionTemplateConfig(ctx, runtime, item.ID, payload)
		var execution *caseExecutionConfig
		var exports []map[string]any
		if template != nil {
			execution = &template.CaseExecution
			exports = template.Exports
		}
		if casePath == "" && execution == nil {
			continue
		}
		out = append(out, apiCaseBatchCasePlan{
			ID:              item.ID,
			DisplayName:     item.DisplayName,
			Scenario:        item.Scenario,
			NodeID:          nodeID,
			NodeDisplayName: node.DisplayName,
			Operation:       node.Operation,
			Method:          apiCaseBatchPlanMethod(node, execution),
			Path:            apiCaseBatchPlanPath(node, execution),
			StepID:          binding.StepID,
			CasePath:        resolveBatchAPICasePath(ctx, runtime, bundle, casePath),
			BaseURL:         firstNonEmpty(request.BaseURL, item.BaseURL),
			EvidenceDir:     firstNonEmpty(request.EvidenceDir, item.EvidenceDir, filepath.Join(".runtime", "case-batches")),
			TimeoutSeconds:  firstPositive(request.TimeoutSeconds, item.TimeoutSeconds),
			Overrides:       mergeStringAnyMaps(item.DefaultOverrides, request.Overrides),
			Execution:       execution,
			Exports:         exports,
			Case:            item,
		})
	}
	return out
}

func apiCaseBatchPlanMethod(node profile.InterfaceNode, execution *caseExecutionConfig) string {
	if strings.TrimSpace(node.Method) != "" {
		return node.Method
	}
	if execution != nil {
		return execution.Method
	}
	return ""
}

func apiCaseBatchPlanPath(node profile.InterfaceNode, execution *caseExecutionConfig) string {
	if strings.TrimSpace(node.Path) != "" {
		return node.Path
	}
	if execution != nil {
		return execution.Path
	}
	return ""
}

func resolveBatchAPICasePath(ctx context.Context, runtime store.Store, bundle profile.Bundle, casePath string) string {
	return resolveBundleAPICasePath(ctx, runtime, bundle, casePath)
}

func apiCaseBatchNodesByID(bundle profile.Bundle) map[string]profile.InterfaceNode {
	out := make(map[string]profile.InterfaceNode, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		out[node.ID] = node
	}
	return out
}

func apiCaseBatchNodesFromPlans(plans []apiCaseBatchCasePlan) []apiCaseBatchNodeReport {
	seen := map[string]bool{}
	out := make([]apiCaseBatchNodeReport, 0, len(plans))
	for _, plan := range plans {
		if strings.TrimSpace(plan.NodeID) == "" || seen[plan.NodeID] {
			continue
		}
		seen[plan.NodeID] = true
		out = append(out, apiCaseBatchNodeReport{
			ID:          plan.NodeID,
			DisplayName: plan.NodeDisplayName,
			Operation:   plan.Operation,
			Method:      plan.Method,
			Path:        plan.Path,
		})
	}
	return out
}

func apiCaseBatchReportDir(request apiCaseBatchRunRequest, plans []apiCaseBatchCasePlan) string {
	if strings.TrimSpace(request.EvidenceDir) != "" {
		return request.EvidenceDir
	}
	for _, plan := range plans {
		if strings.TrimSpace(plan.EvidenceDir) != "" {
			return plan.EvidenceDir
		}
	}
	return filepath.Join(".runtime", "case-batches")
}

func refreshAPICaseBatchCounts(report *apiCaseBatchRunReport) {
	report.Completed = 0
	report.Passed = 0
	report.Failed = 0
	report.Skipped = 0
	for _, item := range report.Cases {
		switch item.Status {
		case store.StatusPassed:
			report.Completed++
			report.Passed++
		case store.StatusFailed:
			report.Completed++
			report.Failed++
		case store.StatusSkipped:
			report.Completed++
			report.Skipped++
		}
	}
}

func cloneAPICaseBatchReport(report apiCaseBatchRunReport) apiCaseBatchRunReport {
	report.NodeIDs = append([]string(nil), report.NodeIDs...)
	if report.Suite != nil {
		suite := *report.Suite
		suite.Tags = append([]string(nil), report.Suite.Tags...)
		suite.RunStates = append([]string(nil), report.Suite.RunStates...)
		report.Suite = &suite
	}
	report.Nodes = append([]apiCaseBatchNodeReport(nil), report.Nodes...)
	report.Cases = append([]apiCaseBatchCaseReport(nil), report.Cases...)
	report.Acceptance.Steps = append([]workflowAcceptanceStep(nil), report.Acceptance.Steps...)
	report.Acceptance.Requirements = append([]workflowAcceptanceRequirement(nil), report.Acceptance.Requirements...)
	return report
}

func stringListValue(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if raw := strings.TrimSpace(valueString(value)); raw != "" {
			return casesuite.NormalizeStringList([]string{raw})
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(valueString(item)); value != "" {
			out = append(out, value)
		}
	}
	return casesuite.NormalizeStringList(out)
}

func apiCaseBatchSuiteSelectorValue(value any) apiCaseBatchSuiteSelector {
	raw := mapValue(value)
	if len(raw) == 0 {
		return apiCaseBatchSuiteSelector{}
	}
	return normalizeAPICaseBatchSuiteSelector(apiCaseBatchSuiteSelector{
		Filter:    strings.TrimSpace(valueString(raw["filter"])),
		NodeID:    firstNonEmpty(valueString(raw["nodeId"]), valueString(raw["node"])),
		Tags:      firstNonNilStringList(raw["tags"], raw["tag"]),
		Status:    strings.TrimSpace(valueString(raw["status"])),
		Owner:     strings.TrimSpace(valueString(raw["owner"])),
		Priority:  strings.TrimSpace(valueString(raw["priority"])),
		RunStates: firstNonNilStringList(raw["runStates"], raw["runState"]),
	})
}

func normalizeAPICaseBatchSuiteSelector(selector apiCaseBatchSuiteSelector) apiCaseBatchSuiteSelector {
	selector.Filter = strings.TrimSpace(selector.Filter)
	selector.NodeID = strings.TrimSpace(selector.NodeID)
	selector.Tags = casesuite.NormalizeStringList(selector.Tags)
	selector.Status = strings.TrimSpace(selector.Status)
	if selector.Status == "" && selector.configuredWithoutStatus() {
		selector.Status = "active"
	}
	selector.Owner = strings.TrimSpace(selector.Owner)
	selector.Priority = strings.TrimSpace(selector.Priority)
	selector.RunStates = casesuite.NormalizeStringList(selector.RunStates)
	for index, value := range selector.RunStates {
		selector.RunStates[index] = casesuite.NormalizeRunState(value)
	}
	return selector
}

func (s apiCaseBatchSuiteSelector) configured() bool {
	return s.configuredWithoutStatus() || strings.TrimSpace(s.Status) != ""
}

func (s apiCaseBatchSuiteSelector) configuredWithoutStatus() bool {
	return strings.TrimSpace(s.Filter) != "" || strings.TrimSpace(s.NodeID) != "" || len(s.Tags) > 0 || strings.TrimSpace(s.Owner) != "" || strings.TrimSpace(s.Priority) != "" || len(s.RunStates) > 0
}

func firstNonNilStringList(values ...any) []string {
	for _, value := range values {
		if out := stringListValue(value); len(out) > 0 {
			return out
		}
	}
	return nil
}

func compactUniqueStringList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func compactUniqueStringListPreserveOrder(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
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

func resolveBundleFilePath(baseDir string, value string) string {
	if filepath.IsAbs(value) || strings.TrimSpace(baseDir) == "" {
		return value
	}
	return filepath.Join(baseDir, value)
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func mergeStringAnyMaps(base map[string]any, overlay map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overlay {
		out[key] = value
	}
	return out
}

func newAPICaseBatchRunID(requestID string) string {
	return "batch." + safeRunIDPart(requestID) + "." + time.Now().UTC().Format("20060102T150405.000000000Z")
}

func apiCaseBatchCaseRunID(batchRunID string, stepID string, caseID string) string {
	if strings.TrimSpace(stepID) != "" {
		return batchRunID + "." + safeRunIDPart(stepID) + "." + safeRunIDPart(caseID)
	}
	return batchRunID + "." + safeRunIDPart(caseID)
}

func apiCaseEvidenceDetailURL(caseRunID string) string {
	if strings.TrimSpace(caseRunID) == "" {
		return ""
	}
	return "/api/case-run/evidence?caseRunId=" + url.QueryEscape(caseRunID)
}

func safeRunIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "item"
	}
	var builder strings.Builder
	builder.Grow(len(value))
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			builder.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			builder.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '.', ch == '-', ch == '_':
			builder.WriteRune(ch)
		default:
			builder.WriteByte('-')
		}
	}
	out := strings.Trim(builder.String(), "-._")
	if out == "" {
		return "item"
	}
	return out
}
