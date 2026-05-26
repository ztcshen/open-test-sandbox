package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/runner/apicase"
	"agent-testbench/internal/server/controlplane"
)

type interfaceNodeCaseAuditReport struct {
	OK         bool                          `json:"ok"`
	ProfileID  string                        `json:"profileId"`
	NodeID     string                        `json:"nodeId"`
	Counts     interfaceNodeCaseAuditCounts  `json:"counts"`
	Configured []interfaceNodeCaseConfigured `json:"configured"`
	Missing    []interfaceNodeCaseMissing    `json:"missing"`
}

type interfaceNodeCaseAuditCounts struct {
	Cases      int `json:"cases"`
	Configured int `json:"configured"`
	Missing    int `json:"missing"`
}

type interfaceNodeCaseConfigured struct {
	CaseID   string `json:"caseId"`
	ConfigID string `json:"configId"`
}

type interfaceNodeCaseMissing struct {
	CaseID string `json:"caseId"`
	Title  string `json:"title,omitempty"`
}

type interfaceNodeCaseApplyRequest struct {
	APICases           []profile.APICase     `json:"apiCases,omitempty"`
	InterfaceNodeCases []profile.APICase     `json:"interfaceNodeCases,omitempty"`
	TemplateConfigs    []templateConfigInput `json:"templateConfigs,omitempty"`
	CaseFiles          []caseFileInput       `json:"caseFiles,omitempty"`
}

type templateConfigInput struct {
	profile.TemplateConfig
	Config json.RawMessage `json:"config,omitempty"`
}

type caseFileInput struct {
	Path string       `json:"path"`
	Case apicase.Case `json:"case"`
}

type interfaceNodeCaseDraftReport struct {
	OK             bool                          `json:"ok"`
	ProfileID      string                        `json:"profileId"`
	NodeID         string                        `json:"nodeId"`
	CaseID         string                        `json:"caseId"`
	CasePath       string                        `json:"casePath"`
	BundlePath     string                        `json:"bundlePath,omitempty"`
	APICase        profile.APICase               `json:"apiCase"`
	TemplateConfig profile.TemplateConfig        `json:"templateConfig"`
	CaseFile       caseFileInput                 `json:"caseFile"`
	ApplyBundle    interfaceNodeCaseApplyRequest `json:"applyBundle"`
}

type interfaceNodeCaseApplyResult struct {
	Profile string `json:"profile"`
	File    string `json:"file"`
	Applied int    `json:"applied"`
	Cases   int    `json:"cases"`
	Files   int    `json:"files"`
}

type interfaceNodeListReport struct {
	OK        bool                    `json:"ok"`
	ProfileID string                  `json:"profileId"`
	Count     int                     `json:"count"`
	Items     []interfaceNodeListItem `json:"items"`
}

type interfaceNodeListItem struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Operation   string `json:"operation,omitempty"`
	Method      string `json:"method,omitempty"`
	Path        string `json:"path,omitempty"`
	ServiceID   string `json:"serviceId,omitempty"`
	CaseCount   int    `json:"caseCount"`
}

func runInterfaceNode(args []string) error {
	if len(args) == 0 {
		return errors.New("missing interface-node command")
	}
	if args[0] == "discover" {
		return runInterfaceNodeDiscover(context.Background(), args[1:])
	}
	if args[0] == "coverage" {
		return runInterfaceNodeCoverage(context.Background(), args[1:], false)
	}
	if args[0] == "coverage-gaps" {
		return runInterfaceNodeCoverage(context.Background(), args[1:], true)
	}
	if args[0] != "case" {
		return fmt.Errorf("unknown interface-node command: %s", args[0])
	}
	if len(args) < 2 {
		return errors.New("missing interface-node case command")
	}
	switch args[1] {
	case "audit":
		return runInterfaceNodeCaseAudit(args[2:])
	case "draft":
		return runInterfaceNodeCaseDraft(args[2:])
	case "apply":
		return runInterfaceNodeCaseApply(args[2:])
	case "report":
		return runInterfaceNodeCaseReport(context.Background(), args[2:])
	default:
		return fmt.Errorf("unknown interface-node case command: %s", args[1])
	}
}

func runInterfaceNodeDiscover(ctx context.Context, args []string) error {
	options, err := parseProfileDiscoveryCommandOptions("interface-node discover", "Filter by id, display name, or operation", args)
	if err != nil {
		return err
	}
	bundle, cleanup, err := options.loadDiscoveryBundle(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	report := interfaceNodeList(bundle, options.Filter)
	if options.JSONOutput {
		return writeIndentedJSON(report)
	}
	for _, item := range report.Items {
		fmt.Printf("%s\t%s\t%d\n", item.ID, item.DisplayName, item.CaseCount)
	}
	return nil
}

func runInterfaceNodeCoverage(ctx context.Context, args []string, gapsOnly bool) error {
	name := "interface-node coverage"
	if gapsOnly {
		name = "interface-node coverage-gaps"
	}
	options, err := parseProfileWorkflowStoreCommandOptions(name, args, false)
	if err != nil {
		return err
	}
	bundle, runtime, _, cleanup, err := options.loadRequiredBundle(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	var payload map[string]any
	if gapsOnly {
		payload, err = controlplane.InterfaceNodeCoverageGapsPayload(ctx, bundle, options.WorkflowID, runtime)
	} else {
		payload, err = controlplane.InterfaceNodeCoveragePayload(ctx, bundle, options.WorkflowID, runtime)
	}
	if err != nil {
		return err
	}
	if options.JSONOutput {
		return writeIndentedJSON(payload)
	}
	printInterfaceNodeCoverage(payload, gapsOnly)
	return nil
}

func interfaceNodeList(bundle profile.Bundle, filter string) interfaceNodeListReport {
	caseCounts := map[string]int{}
	for _, item := range bundle.APICases {
		if strings.TrimSpace(item.NodeID) != "" {
			caseCounts[item.NodeID]++
		}
	}
	nodes := append([]profile.InterfaceNode(nil), bundle.InterfaceNodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].SortOrder != nodes[j].SortOrder {
			return nodes[i].SortOrder < nodes[j].SortOrder
		}
		return nodes[i].ID < nodes[j].ID
	})
	report := interfaceNodeListReport{OK: true, ProfileID: bundle.ID}
	for _, node := range nodes {
		if !matchesDiscoveryFilter(filter, node.ID, node.DisplayName, node.Operation) {
			continue
		}
		report.Items = append(report.Items, interfaceNodeListItem{
			ID:          node.ID,
			DisplayName: node.DisplayName,
			Operation:   node.Operation,
			Method:      node.Method,
			Path:        node.Path,
			ServiceID:   node.ServiceID,
			CaseCount:   caseCounts[node.ID],
		})
	}
	report.Count = len(report.Items)
	return report
}

func runInterfaceNodeCaseAudit(args []string) error {
	flags := flag.NewFlagSet("interface-node case audit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	nodeID := flags.String("node", "", "Interface node id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*profilePath) == "" {
		return errors.New("--profile is required")
	}
	if strings.TrimSpace(*nodeID) == "" {
		return errors.New("--node is required")
	}
	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}
	report := auditInterfaceNodeCaseExecutionConfigs(bundle, *nodeID)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printInterfaceNodeCaseAudit(report)
	return nil
}

func runInterfaceNodeCaseDraft(args []string) error {
	flags := flag.NewFlagSet("interface-node case draft", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	nodeID := flags.String("node", "", "Interface node id")
	caseID := flags.String("case-id", "", "Case id to create")
	title := flags.String("title", "", "Case title")
	casePath := flags.String("case-path", "", "Runnable case path inside the profile bundle")
	method := flags.String("method", "", "HTTP method; defaults to the interface node method")
	requestPath := flags.String("path", "", "Request path; defaults to the interface node path")
	priority := flags.String("priority", "", "Case priority metadata")
	owner := flags.String("owner", "", "Case owner metadata")
	outputPath := flags.String("output", "", "Write an apply-ready case config bundle to this path")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Case tag metadata; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*profilePath) == "" {
		return errors.New("--profile is required")
	}
	if strings.TrimSpace(*nodeID) == "" {
		return errors.New("--node is required")
	}
	if strings.TrimSpace(*caseID) == "" {
		return errors.New("--case-id is required")
	}
	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}
	report, err := draftInterfaceNodeCase(bundle, *nodeID, *caseID, *title, *casePath, *method, *requestPath, tags.Values(), *priority, *owner)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*outputPath) != "" {
		if err := writeCaseApplyBundle(*outputPath, report.ApplyBundle); err != nil {
			return err
		}
		report.BundlePath = *outputPath
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Case Draft: %s\n", report.CaseID)
	fmt.Printf("Node: %s\n", report.NodeID)
	fmt.Printf("Case Path: %s\n", report.CasePath)
	if report.BundlePath != "" {
		fmt.Printf("Bundle: %s\n", report.BundlePath)
	}
	return nil
}

func writeCaseApplyBundle(path string, bundle interfaceNodeCaseApplyRequest) error {
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create case draft output directory: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write case draft bundle %s: %w", path, err)
	}
	return nil
}

func findInterfaceNode(nodes []profile.InterfaceNode, id string) (profile.InterfaceNode, bool) {
	id = strings.TrimSpace(id)
	for _, node := range nodes {
		if node.ID == id {
			return node, true
		}
	}
	return profile.InterfaceNode{}, false
}

func caseExists(cases []profile.APICase, id string) bool {
	for _, item := range cases {
		if item.ID == id {
			return true
		}
	}
	return false
}

func nextCaseSortOrder(cases []profile.APICase) int {
	maxOrder := 0
	for _, item := range cases {
		if item.SortOrder > maxOrder {
			maxOrder = item.SortOrder
		}
	}
	return maxOrder + 1
}

func safeCaseFileName(caseID string) string {
	return safeProfileAssetFileName(caseID, "case")
}

func safeProfileAssetFileName(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	var builder strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('-')
	}
	if builder.Len() == 0 {
		return fallback
	}
	return builder.String()
}

func draftCaseHeaders(method string) map[string]string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead:
		return nil
	default:
		return map[string]string{"Content-Type": "application/json"}
	}
}

func draftCaseBody(method string) map[string]any {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead:
		return nil
	default:
		return map[string]any{"sample": true}
	}
}

func runInterfaceNodeCaseApply(args []string) error {
	flags := flag.NewFlagSet("interface-node case apply", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	requestPath := flags.String("file", "", "Case execution config bundle")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*profilePath) == "" {
		return errors.New("--profile is required")
	}
	if strings.TrimSpace(*requestPath) == "" {
		return errors.New("--file is required")
	}
	result, err := applyInterfaceNodeCaseConfigs(*profilePath, *requestPath)
	if err != nil {
		return err
	}
	result.Profile = *profilePath
	result.File = *requestPath
	if *jsonOutput {
		return writeIndentedJSON(result)
	}
	fmt.Printf("Applied interface node case configs: %d\n", result.Applied)
	if result.Cases > 0 {
		fmt.Printf("Applied API cases: %d\n", result.Cases)
	}
	if result.Files > 0 {
		fmt.Printf("Applied case files: %d\n", result.Files)
	}
	fmt.Printf("Profile: %s\n", *profilePath)
	return nil
}

func normalizeTemplateConfigInput(input templateConfigInput) (profile.TemplateConfig, error) {
	config := input.TemplateConfig
	if len(input.Config) > 0 {
		compact, err := compactRawJSON(input.Config)
		if err != nil {
			return profile.TemplateConfig{}, fmt.Errorf("template config %q config is invalid: %w", config.ID, err)
		}
		config.ConfigJSON = compact
	}
	if strings.TrimSpace(config.ID) == "" {
		return profile.TemplateConfig{}, errors.New("template config id is required")
	}
	if strings.TrimSpace(config.ConfigJSON) == "" {
		return profile.TemplateConfig{}, fmt.Errorf("template config %q configJson is required", config.ID)
	}
	if caseID, ok := caseExecutionConfigCaseID(config.ConfigJSON); !ok {
		return profile.TemplateConfig{}, fmt.Errorf("template config %q must contain caseId and caseExecution", config.ID)
	} else if strings.TrimSpace(config.ScopeID) == "" {
		config.ScopeID = caseID
	}
	if strings.TrimSpace(config.ScopeType) == "" {
		config.ScopeType = "case"
	}
	if strings.TrimSpace(config.Status) == "" {
		config.Status = "active"
	}
	return config, nil
}

func normalizeAPICaseInput(item profile.APICase) (profile.APICase, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.NodeID = strings.TrimSpace(item.NodeID)
	item.CasePath = filepath.ToSlash(strings.TrimSpace(item.CasePath))
	if item.ID == "" {
		return profile.APICase{}, errors.New("api case id is required")
	}
	if item.NodeID == "" {
		return profile.APICase{}, fmt.Errorf("api case %q nodeId is required", item.ID)
	}
	if item.Status == "" {
		item.Status = "active"
	}
	if item.DisplayName == "" {
		item.DisplayName = item.ID
	}
	return item, nil
}

func writeCaseFiles(profilePath string, files []caseFileInput) error {
	for _, item := range files {
		relative, err := safeBundleRelativePath(item.Path)
		if err != nil {
			return err
		}
		if strings.TrimSpace(item.Case.ID) == "" {
			return fmt.Errorf("case file %q case id is required", item.Path)
		}
		target := filepath.Join(profilePath, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create case file directory %s: %w", filepath.Dir(target), err)
		}
		raw, err := json.MarshalIndent(item.Case, "", "  ")
		if err != nil {
			return fmt.Errorf("encode case file %s: %w", item.Path, err)
		}
		raw = append(raw, '\n')
		if err := os.WriteFile(target, raw, 0o644); err != nil {
			return fmt.Errorf("write case file %s: %w", target, err)
		}
	}
	return nil
}

func safeBundleRelativePath(value string) (string, error) {
	value = filepath.ToSlash(strings.TrimSpace(value))
	if value == "" {
		return "", errors.New("case file path is required")
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") || value == ".." {
		return "", fmt.Errorf("case file path %q must stay inside the profile bundle", value)
	}
	return filepath.FromSlash(value), nil
}

func readCatalogCaseAssets(path string) (map[string]json.RawMessage, []profile.TemplateConfig, []profile.APICase, error) {
	payload := map[string]json.RawMessage{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return payload, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read profile catalog %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, nil, fmt.Errorf("decode profile catalog %s: %w", path, err)
	}
	var configs []profile.TemplateConfig
	if rawConfigs, ok := payload["templateConfigs"]; ok {
		if err := json.Unmarshal(rawConfigs, &configs); err != nil {
			return nil, nil, nil, fmt.Errorf("decode profile catalog templateConfigs %s: %w", path, err)
		}
	}
	var cases []profile.APICase
	for _, key := range []string{"interfaceNodeCases", "apiCases"} {
		rawCases, ok := payload[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(rawCases, &cases); err != nil {
			return nil, nil, nil, fmt.Errorf("decode profile catalog %s %s: %w", key, path, err)
		}
		break
	}
	return payload, configs, cases, nil
}

func mergeTemplateConfigs(existing []profile.TemplateConfig, updates []profile.TemplateConfig) []profile.TemplateConfig {
	return mergeProfileCatalogItems(existing, updates, func(item profile.TemplateConfig) string {
		return item.ID
	}, func(item profile.TemplateConfig) int {
		return item.SortOrder
	})
}

func mergeProfileAPICases(existing []profile.APICase, updates []profile.APICase) []profile.APICase {
	return mergeProfileCatalogItems(existing, updates, func(item profile.APICase) string {
		return item.ID
	}, func(item profile.APICase) int {
		return item.SortOrder
	})
}

func mergeProfileCatalogItems[T any](existing []T, updates []T, itemID func(T) string, itemSortOrder func(T) int) []T {
	positions := map[string]int{}
	out := make([]T, 0, len(existing)+len(updates))
	for _, item := range existing {
		id := itemID(item)
		positions[id] = len(out)
		out = append(out, item)
	}
	for _, item := range updates {
		id := itemID(item)
		if index, ok := positions[id]; ok {
			out[index] = item
			continue
		}
		positions[id] = len(out)
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		leftOrder, rightOrder := itemSortOrder(out[i]), itemSortOrder(out[j])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return itemID(out[i]) < itemID(out[j])
	})
	return out
}
