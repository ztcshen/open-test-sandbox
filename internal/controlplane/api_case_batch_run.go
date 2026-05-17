package controlplane

import (
	"bytes"
	"context"
	_ "embed"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"open-test-sandbox/internal/apicase"
	"open-test-sandbox/internal/casesuite"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store"
)

type apiCaseBatchRunRequest struct {
	RequestID      string                    `json:"requestId"`
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
	OK             bool                       `json:"ok"`
	BatchRunID     string                     `json:"batchRunId"`
	RequestID      string                     `json:"requestId"`
	ProfileID      string                     `json:"profileId"`
	NodeIDs        []string                   `json:"nodeIds"`
	WorkflowID     string                     `json:"workflowId,omitempty"`
	Suite          *apiCaseBatchSuiteSelector `json:"suite,omitempty"`
	Status         string                     `json:"status"`
	Total          int                        `json:"total"`
	Completed      int                        `json:"completed"`
	Passed         int                        `json:"passed"`
	Failed         int                        `json:"failed"`
	Skipped        int                        `json:"skipped"`
	ReportURL      string                     `json:"reportUrl,omitempty"`
	StartedAt      string                     `json:"startedAt"`
	FinishedAt     string                     `json:"finishedAt,omitempty"`
	Nodes          []apiCaseBatchNodeReport   `json:"nodes,omitempty"`
	Cases          []apiCaseBatchCaseReport   `json:"cases"`
	Error          string                     `json:"error,omitempty"`
	HTMLReportPath string                     `json:"htmlReportPath,omitempty"`
	HTMLReportURL  string                     `json:"htmlReportUrl,omitempty"`
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

func handleAPICaseBatchRunStart(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store, runner *apiCaseBatchRunner) {
	payload, err := readJSONPayload(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	request := apiCaseBatchRunRequest{
		RequestID:      strings.TrimSpace(valueString(payload["requestId"])),
		NodeIDs:        stringListValue(payload["nodeIds"]),
		WorkflowID:     strings.TrimSpace(valueString(payload["workflowId"])),
		Suite:          apiCaseBatchSuiteSelectorValue(payload["suite"]),
		BaseURL:        strings.TrimSpace(valueString(payload["baseUrl"])),
		EvidenceDir:    strings.TrimSpace(valueString(payload["evidenceDir"])),
		TimeoutSeconds: intValue(payload["timeoutSeconds"]),
		Overrides:      mapValue(payload["overrides"]),
	}
	if request.RequestID == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "requestId is required"})
		return
	}
	request.NodeIDs = compactUniqueStringList(request.NodeIDs)
	request.Suite = normalizeAPICaseBatchSuiteSelector(request.Suite)
	if len(request.NodeIDs) == 0 && request.WorkflowID == "" && !request.Suite.configured() {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "nodeIds, workflowId, or suite is required"})
		return
	}
	plans, err := apiCaseBatchPlans(r.Context(), bundle, runtime, request)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(plans) == 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "no api cases matched selector"})
		return
	}

	batchRunID := newAPICaseBatchRunID(request.RequestID)
	now := time.Now().UTC()
	report := apiCaseBatchRunReport{
		OK:             true,
		BatchRunID:     batchRunID,
		RequestID:      request.RequestID,
		ProfileID:      bundle.ID,
		NodeIDs:        request.NodeIDs,
		WorkflowID:     request.WorkflowID,
		Status:         store.StatusRunning,
		Total:          len(plans),
		ReportURL:      "/api/cases/batch-runs/" + url.PathEscape(batchRunID),
		StartedAt:      now.Format(time.RFC3339Nano),
		Nodes:          apiCaseBatchNodesFromPlans(plans),
		Cases:          make([]apiCaseBatchCaseReport, 0, len(plans)),
		HTMLReportPath: filepath.Join(apiCaseBatchReportDir(request, plans), batchRunID, "report.html"),
		HTMLReportURL:  "/api/cases/batch-runs/" + url.PathEscape(batchRunID) + "/report.html",
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
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	runner.save(report)

	go runner.run(context.Background(), batchRunID, bundle.ID, plans, runtime)
	writeJSONStatus(w, http.StatusAccepted, report)
}

func handleAPICaseBatchRunReport(w http.ResponseWriter, r *http.Request, runner *apiCaseBatchRunner) {
	idValue := strings.TrimPrefix(r.URL.Path, "/api/cases/batch-runs/")
	wantsHTML := strings.HasSuffix(idValue, "/report.html")
	if wantsHTML {
		idValue = strings.TrimSuffix(idValue, "/report.html")
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
	writeJSON(w, report)
}

func (r *apiCaseBatchRunner) run(ctx context.Context, batchRunID string, profileID string, plans []apiCaseBatchCasePlan, runtime store.Store) {
	for index, plan := range plans {
		caseCtx := ctx
		var cancel context.CancelFunc
		if plan.TimeoutSeconds > 0 {
			caseCtx, cancel = context.WithTimeout(ctx, time.Duration(plan.TimeoutSeconds)*time.Second)
		}
		result, err := apicase.Run(caseCtx, apicase.RunOptions{
			CasePath:    plan.CasePath,
			BaseURL:     plan.BaseURL,
			EvidenceDir: plan.EvidenceDir,
			RunID:       apiCaseBatchCaseRunID(batchRunID, plan.StepID, plan.ID),
			Overrides:   plan.Overrides,
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
			if runtime != nil {
				if err := recordAPICaseRun(ctx, runtime, profileID, result); err != nil {
					item.Status = store.StatusFailed
					item.Error = err.Error()
				}
			}
		}
		r.updateCase(batchRunID, index, item)
	}
	r.finish(batchRunID)
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
	r.runs[batchRunID] = report
}

func (r *apiCaseBatchRunner) finish(batchRunID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	_ = writeAPICaseBatchHTMLReport(report)
	r.runs[batchRunID] = report
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

func apiCaseBatchPlans(ctx context.Context, bundle profile.Bundle, runtime store.Store, request apiCaseBatchRunRequest) ([]apiCaseBatchCasePlan, error) {
	if strings.TrimSpace(request.WorkflowID) != "" {
		return apiCaseBatchWorkflowPlans(bundle, request), nil
	}
	if request.Suite.configured() {
		return apiCaseBatchSuitePlans(ctx, bundle, runtime, request)
	}
	return apiCaseBatchNodePlans(bundle, request), nil
}

func apiCaseBatchNodePlans(bundle profile.Bundle, request apiCaseBatchRunRequest) []apiCaseBatchCasePlan {
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
		if casePath == "" {
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
			Method:          node.Method,
			Path:            node.Path,
			CasePath:        resolveBundleFilePath(bundle.BaseDir, casePath),
			BaseURL:         firstNonEmpty(request.BaseURL, item.BaseURL),
			EvidenceDir:     firstNonEmpty(request.EvidenceDir, item.EvidenceDir, filepath.Join(".runtime", "case-batches")),
			TimeoutSeconds:  firstPositive(request.TimeoutSeconds, item.TimeoutSeconds),
			Overrides:       mergeStringAnyMaps(item.DefaultOverrides, request.Overrides),
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
	return apiCaseBatchPlansFromCases(bundle, request, cases), nil
}

func apiCaseBatchPlansFromCases(bundle profile.Bundle, request apiCaseBatchRunRequest, cases []profile.APICase) []apiCaseBatchCasePlan {
	nodesByID := apiCaseBatchNodesByID(bundle)
	out := make([]apiCaseBatchCasePlan, 0, len(cases))
	for _, item := range cases {
		casePath := strings.TrimSpace(item.CasePath)
		if casePath == "" {
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
			Method:          node.Method,
			Path:            node.Path,
			CasePath:        resolveBundleFilePath(bundle.BaseDir, casePath),
			BaseURL:         firstNonEmpty(request.BaseURL, item.BaseURL),
			EvidenceDir:     firstNonEmpty(request.EvidenceDir, item.EvidenceDir, filepath.Join(".runtime", "case-batches")),
			TimeoutSeconds:  firstPositive(request.TimeoutSeconds, item.TimeoutSeconds),
			Overrides:       mergeStringAnyMaps(item.DefaultOverrides, request.Overrides),
		})
	}
	return out
}

func apiCaseBatchWorkflowPlans(bundle profile.Bundle, request apiCaseBatchRunRequest) []apiCaseBatchCasePlan {
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
		if casePath == "" {
			continue
		}
		nodeID := firstNonEmpty(binding.NodeID, item.NodeID)
		node := nodesByID[nodeID]
		out = append(out, apiCaseBatchCasePlan{
			ID:              item.ID,
			DisplayName:     item.DisplayName,
			Scenario:        item.Scenario,
			NodeID:          nodeID,
			NodeDisplayName: node.DisplayName,
			Operation:       node.Operation,
			Method:          node.Method,
			Path:            node.Path,
			StepID:          binding.StepID,
			CasePath:        resolveBundleFilePath(bundle.BaseDir, casePath),
			BaseURL:         firstNonEmpty(request.BaseURL, item.BaseURL),
			EvidenceDir:     firstNonEmpty(request.EvidenceDir, item.EvidenceDir, filepath.Join(".runtime", "case-batches")),
			TimeoutSeconds:  firstPositive(request.TimeoutSeconds, item.TimeoutSeconds),
			Overrides:       mergeStringAnyMaps(item.DefaultOverrides, request.Overrides),
		})
	}
	return out
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
