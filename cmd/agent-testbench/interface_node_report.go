package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/domain/profilecatalog"
	"agent-testbench/internal/domain/redaction"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
)

type interfaceNodeCaseReport struct {
	OK             bool                          `json:"ok"`
	ProfileID      string                        `json:"profileId"`
	NodeID         string                        `json:"nodeId"`
	NodeName       string                        `json:"nodeName"`
	Operation      string                        `json:"operation,omitempty"`
	Method         string                        `json:"method,omitempty"`
	Path           string                        `json:"path,omitempty"`
	ReportURL      string                        `json:"reportUrl"`
	JSONReportURL  string                        `json:"jsonReportUrl"`
	ElapsedMs      int64                         `json:"elapsedMs"`
	GeneratedAt    time.Time                     `json:"generatedAt"`
	Counts         interfaceNodeCaseReportCounts `json:"counts"`
	Results        []interfaceNodeCaseReportItem `json:"results"`
	Warnings       []string                      `json:"warnings,omitempty"`
	SourceStoreURL string                        `json:"sourceStoreUrl,omitempty"`
}

type interfaceNodeCaseReportCounts struct {
	Total          int `json:"total"`
	Passed         int `json:"passed"`
	Failed         int `json:"failed"`
	DerivedConfigs int `json:"derivedConfigs"`
}

type interfaceNodeCaseReportItem struct {
	CaseID      string `json:"caseId"`
	Title       string `json:"title"`
	RunID       string `json:"runId,omitempty"`
	CaseRunID   string `json:"caseRunId,omitempty"`
	ViewerURL   string `json:"viewerUrl,omitempty"`
	DetailURL   string `json:"detailUrl,omitempty"`
	Status      string `json:"status"`
	HTTPCode    int    `json:"httpCode,omitempty"`
	ElapsedMs   int64  `json:"elapsedMs"`
	Method      string `json:"method,omitempty"`
	Path        string `json:"path,omitempty"`
	FullURL     string `json:"fullUrl,omitempty"`
	BaseURL     string `json:"baseUrl,omitempty"`
	Error       string `json:"error,omitempty"`
	BodyPreview string `json:"bodyPreview,omitempty"`
}

func runInterfaceNodeCaseReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("interface-node case report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	nodeID := flags.String("node", "", "Interface node id")
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	outputDir := flags.String("output-dir", "", "Report output directory")
	timeoutSeconds := flags.Int("timeout-seconds", 3, "Timeout per API Case")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*nodeID) == "" {
		return errors.New("--node is required")
	}
	if *timeoutSeconds <= 0 {
		return errors.New("--timeout-seconds must be greater than zero")
	}

	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	bundle, sourceStore, cleanup, err := loadInterfaceNodeReportBundle(ctx, *profilePath, *profileHome, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer cleanup()
	node, err := findInterfaceNodeByID(bundle.InterfaceNodes, *nodeID)
	if err != nil {
		return err
	}
	cases := interfaceNodeReportCases(bundle.APICases, node.ID)
	if len(cases) == 0 {
		return fmt.Errorf("no API cases found for interface node %s", node.ID)
	}
	derived := deriveInterfaceNodeCaseConfigs(bundle, node, cases)
	bundle.TemplateConfigs = mergeTemplateConfigs(bundle.TemplateConfigs, derived)
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "node."+safeReportID(node.ID)+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeInterfaceNodeCaseReport(ctx, bundle, node, cases, derived, sourceStore, resolvedStoreURL, *baseURL, absOutputDir, *timeoutSeconds)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printInterfaceNodeCaseReport(report)
	return nil
}

func loadInterfaceNodeReportBundle(ctx context.Context, profileRef string, profileHomeRef string, storeURL string) (profile.Bundle, store.Store, func(), error) {
	cleanup := func() {}
	var sourceStore store.Store
	if strings.TrimSpace(storeURL) != "" {
		opened, err := openStore(ctx, storeURL)
		if err != nil {
			return profile.Bundle{}, nil, cleanup, err
		}
		sourceStore = opened
		cleanup = cleanupCLIStore(opened)
	}
	if strings.TrimSpace(profileRef) != "" {
		resolvedProfilePath, err := resolveProfileReference(profileRef, profileHomeRef)
		if err != nil {
			cleanup()
			return profile.Bundle{}, nil, func() {}, err
		}
		bundle, err := profile.Load(resolvedProfilePath)
		if err != nil {
			cleanup()
			return profile.Bundle{}, nil, func() {}, err
		}
		return bundle, sourceStore, cleanup, nil
	}
	if sourceStore == nil {
		return profile.Bundle{}, nil, cleanup, errors.New("--profile, --store, --store-url, or an active Store is required")
	}
	bundle, err := serveBundle(ctx, sourceStore)
	if err != nil {
		cleanup()
		return profile.Bundle{}, nil, func() {}, err
	}
	return bundle, sourceStore, cleanup, nil
}

func loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx context.Context, profileRef string, profileHomeRef string, storeRef string, legacyStoreURL string) (profile.Bundle, store.Store, string, func(), error) {
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(storeRef, legacyStoreURL)
	if err != nil {
		return profile.Bundle{}, nil, "", func() {}, err
	}
	bundle, runtime, cleanup, err := loadInterfaceNodeReportBundle(ctx, profileRef, profileHomeRef, resolvedStoreURL)
	if err != nil {
		return profile.Bundle{}, nil, resolvedStoreURL, cleanup, err
	}
	return bundle, runtime, resolvedStoreURL, cleanup, nil
}

func resolveDiscoveryInputs(profileRef string, storeRef string, legacyStoreURL string, offlineTemplatePackage bool) (string, string, error) {
	profileRef = strings.TrimSpace(profileRef)
	storeRef = strings.TrimSpace(storeRef)
	legacyStoreURL = strings.TrimSpace(legacyStoreURL)
	if offlineTemplatePackage {
		if profileRef == "" {
			return "", "", errors.New("--offline-template-package requires --profile")
		}
		if storeRef != "" || legacyStoreURL != "" {
			return "", "", errors.New("--offline-template-package cannot be combined with --store or --store-url")
		}
		return profileRef, "", nil
	}
	if profileRef != "" {
		return "", "", errors.New("--profile is for offline template package review; add --offline-template-package or use --store NAME_OR_DSN")
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(storeRef, legacyStoreURL)
	if err != nil {
		return "", "", err
	}
	return "", resolvedStoreURL, nil
}

func findInterfaceNodeByID(nodes []profile.InterfaceNode, id string) (profile.InterfaceNode, error) {
	id = strings.TrimSpace(id)
	for _, node := range nodes {
		if node.ID == id {
			return node, nil
		}
	}
	return profile.InterfaceNode{}, fmt.Errorf("interface node not found: %s", id)
}

func normalizedDiscoveryText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, "interface")
	value = strings.TrimSuffix(value, "api")
	value = strings.TrimSuffix(value, "接口")
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "", "/", "")
	return replacer.Replace(strings.TrimSpace(value))
}

func matchesDiscoveryFilter(filter string, values ...string) bool {
	needle := normalizedDiscoveryText(filter)
	if needle == "" {
		return true
	}
	for _, value := range values {
		haystack := normalizedDiscoveryText(value)
		if haystack != "" && (strings.Contains(haystack, needle) || strings.Contains(needle, haystack)) {
			return true
		}
	}
	return false
}

func interfaceNodeReportCases(cases []profile.APICase, nodeID string) []profile.APICase {
	out := make([]profile.APICase, 0)
	for _, item := range cases {
		if item.NodeID == nodeID {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func deriveInterfaceNodeCaseConfigs(bundle profile.Bundle, node profile.InterfaceNode, cases []profile.APICase) []profile.TemplateConfig {
	caseSet := map[string]profile.APICase{}
	for _, item := range cases {
		caseSet[item.ID] = item
	}
	configured := caseExecutionConfigIDs(bundle.TemplateConfigs)
	base := map[string]any{}
	for _, config := range bundle.TemplateConfigs {
		caseID, ok := caseExecutionConfigCaseID(config.ConfigJSON)
		if !ok {
			continue
		}
		if _, belongs := caseSet[caseID]; !belongs {
			continue
		}
		if err := json.Unmarshal([]byte(config.ConfigJSON), &base); err == nil && len(mapFromReportAny(base["caseExecution"])) > 0 {
			break
		}
	}
	if len(base) == 0 {
		return nil
	}
	out := make([]profile.TemplateConfig, 0)
	for _, item := range cases {
		if configured[item.ID] != "" {
			continue
		}
		configJSON, ok := derivedCaseExecutionConfigJSON(base, node, item)
		if !ok {
			continue
		}
		out = append(out, profile.TemplateConfig{
			ID:         "cfg.generated." + safeReportID(item.ID),
			TemplateID: "case-execution",
			NodeID:     node.ID,
			ScopeType:  "case",
			ScopeID:    item.ID,
			Title:      firstNonEmpty(item.DisplayName, item.ID) + " execution",
			ConfigJSON: configJSON,
			Status:     "active",
			SortOrder:  item.SortOrder,
		})
	}
	return out
}

func derivedCaseExecutionConfigJSON(base map[string]any, node profile.InterfaceNode, item profile.APICase) (string, bool) {
	next := cloneMap(base)
	next["caseId"] = item.ID
	execution := mapFromReportAny(next["caseExecution"])
	if len(execution) == 0 {
		return "", false
	}
	mergePayloadTemplateIntoExecution(execution, item.PayloadTemplateJSON)
	mergeExpectedConfigIntoExecution(execution, item.ExpectedJSON)
	next["caseExecution"] = execution
	if caseBlock := mapFromReportAny(next["case"]); len(caseBlock) > 0 {
		caseBlock["id"] = item.ID
		caseBlock["title"] = firstNonEmpty(item.DisplayName, item.ID)
		if item.PayloadTemplateJSON != "" {
			caseBlock["payload"] = rawJSONObject(item.PayloadTemplateJSON)
		}
		next["case"] = caseBlock
	}
	if strings.TrimSpace(valueString(next["action"])) == "" {
		next["action"] = firstNonEmpty(node.Operation, node.ID)
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func mergePayloadTemplateIntoExecution(execution map[string]any, payloadJSON string) {
	payload := rawJSONObject(payloadJSON)
	if len(payload) == 0 {
		return
	}
	if query := mapFromReportAny(payload["query"]); len(query) > 0 {
		mergeReportMap(execution, "query", query)
	}
	if headers := mapFromReportAny(payload["headers"]); len(headers) > 0 {
		mergeReportMap(execution, "headers", headers)
	}
	if body, ok := payload["body"]; ok {
		if bodyMap := mapFromReportAny(body); len(bodyMap) > 0 {
			mergeReportMap(execution, "body", bodyMap)
		} else {
			execution["body"] = body
		}
		return
	}
	if _, hasStructuredKeys := payload["query"]; hasStructuredKeys {
		return
	}
	if strings.EqualFold(valueString(execution["method"]), "GET") {
		mergeReportMap(execution, "query", payload)
		return
	}
	mergeReportMap(execution, "body", payload)
}

func mergeExpectedConfigIntoExecution(execution map[string]any, expectedJSON string) {
	expected := rawJSONObject(expectedJSON)
	if len(expected) == 0 {
		return
	}
	if codes := intSliceFromReportAny(firstReportValue(expected, "expectedHttpCodes", "expected_http_codes")); len(codes) > 0 {
		values := make([]any, 0, len(codes))
		for _, code := range codes {
			values = append(values, code)
		}
		execution["expectedHttpCodes"] = values
	}
	for _, key := range []string{"requireRequestId", "require_request_id"} {
		if value, ok := expected[key].(bool); ok {
			execution["requireRequestId"] = value
			break
		}
	}
}

func executeInterfaceNodeCaseReport(ctx context.Context, bundle profile.Bundle, node profile.InterfaceNode, cases []profile.APICase, derived []profile.TemplateConfig, sourceStore store.Store, sourceStoreURL string, baseURL string, outputDir string, timeoutSeconds int) (interfaceNodeCaseReport, error) {
	started := time.Now()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return interfaceNodeCaseReport{}, err
	}
	runtime, err := requiredReportStore(sourceStore)
	if err != nil {
		return interfaceNodeCaseReport{}, err
	}
	if err := runtime.ReplaceProfileCatalog(ctx, profilecatalog.FromBundle(bundle, time.Now().UTC())); err != nil {
		return interfaceNodeCaseReport{}, err
	}
	handler := controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: runtime})
	server := httptest.NewServer(handler)
	defer server.Close()
	rawBatch, err := postTestKitRunBatch(server.URL, cases, baseURL, timeoutSeconds, "case batch")
	if err != nil {
		return interfaceNodeCaseReport{}, err
	}
	report := interfaceNodeCaseReport{
		OK:             boolFromReportAny(rawBatch["ok"]),
		ProfileID:      bundle.ID,
		NodeID:         node.ID,
		NodeName:       firstNonEmpty(node.DisplayName, node.ID),
		Operation:      node.Operation,
		Method:         node.Method,
		Path:           node.Path,
		ElapsedMs:      time.Since(started).Milliseconds(),
		GeneratedAt:    time.Now().UTC(),
		SourceStoreURL: sourceStoreURL,
		Counts: interfaceNodeCaseReportCounts{
			Total:          len(cases),
			DerivedConfigs: len(derived),
		},
	}
	report.Results = interfaceNodeCaseReportItems(rawBatch["results"])
	for _, item := range report.Results {
		if item.Status == store.StatusPassed {
			report.Counts.Passed++
		} else {
			report.Counts.Failed++
		}
	}
	report.OK = report.Counts.Total > 0 && report.Counts.Failed == 0
	if err := writeInterfaceNodeCaseReportFiles(outputDir, &report); err != nil {
		return interfaceNodeCaseReport{}, err
	}
	return report, nil
}

func requiredReportStore(sourceStore store.Store) (store.Store, error) {
	if sourceStore == nil {
		return nil, errors.New("daily report execution requires an active Store or --store NAME_OR_DSN")
	}
	return sourceStore, nil
}

func interfaceNodeCaseReportItems(value any) []interfaceNodeCaseReportItem {
	values := listFromReportAny(value)
	out := make([]interfaceNodeCaseReportItem, 0, len(values))
	for _, raw := range values {
		item := mapFromReportAny(raw)
		result := mapFromReportAny(item["result"])
		request := mapFromReportAny(result["request"])
		response := mapFromReportAny(result["response"])
		summary := mapFromReportAny(item["summary"])
		status := valueString(item["status"])
		if status == "" {
			status = store.StatusFailed
			if boolFromReportAny(item["ok"]) {
				status = store.StatusPassed
			}
		}
		out = append(out, interfaceNodeCaseReportItem{
			CaseID:      valueString(item["caseId"]),
			Title:       firstNonEmpty(valueString(item["title"]), valueString(item["caseId"])),
			RunID:       valueString(item["runId"]),
			CaseRunID:   valueString(item["caseRunId"]),
			ViewerURL:   valueString(item["viewerUrl"]),
			DetailURL:   valueString(item["detailUrl"]),
			Status:      status,
			HTTPCode:    firstPositiveInt(intFromReportAny(summary["httpCode"]), intFromReportAny(response["statusCode"])),
			ElapsedMs:   int64(intFromReportAny(item["elapsedMs"])),
			Method:      valueString(request["method"]),
			Path:        valueString(request["path"]),
			FullURL:     redaction.URL(valueString(request["fullUrl"])),
			BaseURL:     firstNonEmpty(valueString(summary["targetBaseUrl"]), valueString(request["baseUrl"])),
			Error:       firstNonEmpty(valueString(item["error"]), valueString(summary["failureReason"])),
			BodyPreview: truncateReportText(redaction.Text(valueString(response["body"])), 160),
		})
	}
	return out
}

func writeInterfaceNodeCaseReportFiles(outputDir string, report *interfaceNodeCaseReport) error {
	jsonPath, htmlPath := reportArtifactPaths(outputDir)
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	return writeJSONAndHTMLReportArtifacts(jsonPath, htmlPath, report, renderInterfaceNodeCaseReportHTML(*report))
}

func renderInterfaceNodeCaseReportHTML(report interfaceNodeCaseReport) string {
	var b strings.Builder
	writeReportHTMLStart(&b, "API Case Report", 1280)
	writeReportHeading(&b, report.NodeName, report.NodeID, report.Operation)
	writeReportSummary(&b, caseExecutionReportPills(report.OK, report.Counts, report.ElapsedMs)...)
	b.WriteString(`<table><thead><tr><th>#</th><th>Case</th><th>Status</th><th>HTTP</th><th>Elapsed</th><th>Evidence</th><th>Request</th><th>Response</th><th>Error</th></tr></thead><tbody>`)
	for index, item := range report.Results {
		writeReportIndexCell(&b, index)
		writeReportCaseTitleCell(&b, item.Title, item.CaseID, "")
		writeReportCaseExecutionCells(&b, newReportCaseExecutionCells(item.Status, item.HTTPCode, item.ElapsedMs, item.DetailURL, item.CaseRunID, item.Method, item.FullURL, item.BodyPreview, item.Error))
		b.WriteString(`</tr>`)
	}
	finishReportHTMLTable(&b)
	return b.String()
}

func printInterfaceNodeCaseReport(report interfaceNodeCaseReport) {
	fmt.Printf("API Case Report: %s\n", report.NodeID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Passed: %d Failed: %d\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed)
	fmt.Printf("Derived Configs: %d\n", report.Counts.DerivedConfigs)
	fmt.Printf("Elapsed: %d ms\n", report.ElapsedMs)
	fmt.Printf("Report: %s\n", report.ReportURL)
}
