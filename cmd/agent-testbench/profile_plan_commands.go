package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agent-testbench/internal/domain/profile"
	profilegenerateopenapi "agent-testbench/internal/domain/profilegenerate/openapi"
	profileimporthttpcapture "agent-testbench/internal/domain/profileimport/httpcapture"
	profileimportopenapi "agent-testbench/internal/domain/profileimport/openapi"
)

type profileGenerationPlanReport struct {
	Kind         string                            `json:"kind"`
	SourcePath   string                            `json:"sourcePath"`
	OutputDir    string                            `json:"outputDir,omitempty"`
	WrittenFiles []string                          `json:"writtenFiles,omitempty"`
	Plan         profilegenerateopenapi.PlanResult `json:"plan"`
}

func runProfileGenerationPlan(args []string) error {
	if len(args) == 0 {
		return errors.New("missing profile generation-plan kind")
	}
	switch args[0] {
	case "openapi":
		return runProfileOpenAPIGenerationPlan(args[1:])
	default:
		return fmt.Errorf("unknown profile generation-plan kind: %s", args[0])
	}
}

func runProfileOpenAPIGenerationPlan(args []string) error {
	flags := flag.NewFlagSet("profile generation-plan openapi", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	sourcePath := flags.String("from", "", "OpenAPI JSON document path")
	serviceID := flags.String("service-id", "", "Service ID for generated draft assets")
	evidenceDir := flags.String("evidence-dir", "", "Evidence directory for generated draft API cases")
	outputDir := flags.String("output-dir", "", "Write a reviewable generation plan file tree")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*sourcePath) == "" {
		return errors.New("missing --from")
	}
	raw, err := os.ReadFile(*sourcePath)
	if err != nil {
		return fmt.Errorf("read openapi document: %w", err)
	}
	plan, err := profilegenerateopenapi.Plan(raw, profilegenerateopenapi.Options{
		ServiceID:   *serviceID,
		EvidenceDir: *evidenceDir,
	})
	if err != nil {
		return err
	}
	report := profileGenerationPlanReport{
		Kind:       "openapi",
		SourcePath: *sourcePath,
		Plan:       plan,
	}
	if strings.TrimSpace(*outputDir) != "" {
		report.OutputDir = *outputDir
		writtenFiles, err := writeProfileGenerationPlanOutput(*outputDir, report)
		if err != nil {
			return err
		}
		report.WrittenFiles = writtenFiles
		if err := writeProfileGenerationPlanManifest(*outputDir, report); err != nil {
			return err
		}
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printProfileGenerationPlan("OpenAPI Generation Plan", report)
	return nil
}

func printProfileGenerationPlan(title string, report profileGenerationPlanReport) {
	fmt.Println(title)
	fmt.Printf("Source: %s\n", report.SourcePath)
	fmt.Printf("Service: %s\n", report.Plan.Service.ID)
	fmt.Printf("OK: %t\n", report.Plan.OK)
	fmt.Printf("Candidates: %d\n", len(report.Plan.Candidates))
	fmt.Printf("API Cases: %d\n", len(report.Plan.APICases))
	fmt.Printf("Case Files: %d\n", len(report.Plan.CaseFiles))
	if strings.TrimSpace(report.OutputDir) != "" {
		fmt.Printf("Output Dir: %s\n", report.OutputDir)
		fmt.Printf("Written Files: %d\n", len(report.WrittenFiles))
	}
	for _, warning := range report.Plan.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func writeProfileGenerationPlanOutput(outputDir string, report profileGenerationPlanReport) ([]string, error) {
	return writeProfilePlanAssetTree(outputDir, "generation-plan.json", generationPlanAssets(report.Plan))
}

func writeProfileGenerationPlanManifest(outputDir string, report profileGenerationPlanReport) error {
	return writeImportPlanJSON(outputDir, "generation-plan.json", report)
}

type profileImportPlanReport struct {
	Kind         string                  `json:"kind"`
	SourcePath   string                  `json:"sourcePath"`
	OutputDir    string                  `json:"outputDir,omitempty"`
	WrittenFiles []string                `json:"writtenFiles,omitempty"`
	Plan         profileImportPlanAssets `json:"plan"`
}

type profileImportPlanAssets struct {
	Service          profile.Service             `json:"service"`
	InterfaceNodes   []profile.InterfaceNode     `json:"interfaceNodes"`
	RequestTemplates []profile.RequestTemplate   `json:"requestTemplates"`
	APICases         []profile.APICase           `json:"apiCases"`
	CaseFiles        []profileImportPlanCaseFile `json:"caseFiles"`
}

type profileImportPlanCaseFile struct {
	Path string          `json:"path"`
	Body json.RawMessage `json:"body"`
}

func runProfileImportPlan(args []string) error {
	if len(args) == 0 {
		return errors.New("missing profile import-plan kind")
	}
	switch args[0] {
	case "openapi":
		return runProfileOpenAPIImportPlan(args[1:])
	case "http-capture":
		return runProfileHTTPCaptureImportPlan(args[1:])
	default:
		return fmt.Errorf("unknown profile import-plan kind: %s", args[0])
	}
}

func runProfileOpenAPIImportPlan(args []string) error {
	return runProfileImportPlanKind(args, profileImportPlanKind{
		Command:    "profile import-plan openapi",
		Kind:       "openapi",
		SourceHelp: "OpenAPI JSON document path",
		ReadLabel:  "openapi document",
		Title:      "OpenAPI Import Plan",
		Build: func(raw []byte, serviceID string, evidenceDir string) (profileImportPlanAssets, error) {
			plan, err := profileimportopenapi.Plan(raw, profileimportopenapi.Options{
				ServiceID:   serviceID,
				EvidenceDir: evidenceDir,
			})
			if err != nil {
				return profileImportPlanAssets{}, err
			}
			return importPlanAssetsFromOpenAPI(plan), nil
		},
	})
}

func runProfileHTTPCaptureImportPlan(args []string) error {
	return runProfileImportPlanKind(args, profileImportPlanKind{
		Command:    "profile import-plan http-capture",
		Kind:       "http-capture",
		SourceHelp: "HTTP capture JSON document path",
		ReadLabel:  "http capture document",
		Title:      "HTTP Capture Import Plan",
		Build: func(raw []byte, serviceID string, evidenceDir string) (profileImportPlanAssets, error) {
			plan, err := profileimporthttpcapture.Plan(raw, profileimporthttpcapture.Options{
				ServiceID:   serviceID,
				EvidenceDir: evidenceDir,
			})
			if err != nil {
				return profileImportPlanAssets{}, err
			}
			return importPlanAssetsFromHTTPCapture(plan), nil
		},
	})
}

type profileImportPlanKind struct {
	Command    string
	Kind       string
	SourceHelp string
	ReadLabel  string
	Title      string
	Build      func(raw []byte, serviceID string, evidenceDir string) (profileImportPlanAssets, error)
}

func runProfileImportPlanKind(args []string, kind profileImportPlanKind) error {
	flags := flag.NewFlagSet(kind.Command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	sourcePath := flags.String("from", "", kind.SourceHelp)
	serviceID := flags.String("service-id", "", "Service ID for generated draft assets")
	evidenceDir := flags.String("evidence-dir", "", "Evidence directory for generated draft API cases")
	outputDir := flags.String("output-dir", "", "Write a reviewable import plan file tree")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*sourcePath) == "" {
		return errors.New("missing --from")
	}
	raw, err := os.ReadFile(*sourcePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", kind.ReadLabel, err)
	}
	plan, err := kind.Build(raw, *serviceID, *evidenceDir)
	if err != nil {
		return err
	}
	report := profileImportPlanReport{
		Kind:       kind.Kind,
		SourcePath: *sourcePath,
		Plan:       plan,
	}
	if strings.TrimSpace(*outputDir) != "" {
		report.OutputDir = *outputDir
		writtenFiles, err := writeProfileImportPlanOutput(*outputDir, report)
		if err != nil {
			return err
		}
		report.WrittenFiles = writtenFiles
		if err := writeProfileImportPlanManifest(*outputDir, report); err != nil {
			return err
		}
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printProfileImportPlan(kind.Title, report)
	return nil
}

func printProfileImportPlan(title string, report profileImportPlanReport) {
	fmt.Println(title)
	fmt.Printf("Source: %s\n", report.SourcePath)
	fmt.Printf("Service: %s\n", report.Plan.Service.ID)
	fmt.Printf("Interface Nodes: %d\n", len(report.Plan.InterfaceNodes))
	fmt.Printf("Request Templates: %d\n", len(report.Plan.RequestTemplates))
	fmt.Printf("API Cases: %d\n", len(report.Plan.APICases))
	fmt.Printf("Case Files: %d\n", len(report.Plan.CaseFiles))
	if strings.TrimSpace(report.OutputDir) != "" {
		fmt.Printf("Output Dir: %s\n", report.OutputDir)
		fmt.Printf("Written Files: %d\n", len(report.WrittenFiles))
	}
}

func importPlanAssetsFromOpenAPI(plan profileimportopenapi.PlanResult) profileImportPlanAssets {
	return newProfileImportPlanAssets(plan.Service, plan.InterfaceNodes, plan.RequestTemplates, plan.APICases, len(plan.CaseFiles), func(index int) (string, json.RawMessage) {
		return plan.CaseFiles[index].Path, plan.CaseFiles[index].Body
	})
}

func importPlanAssetsFromHTTPCapture(plan profileimporthttpcapture.PlanResult) profileImportPlanAssets {
	return newProfileImportPlanAssets(plan.Service, plan.InterfaceNodes, plan.RequestTemplates, plan.APICases, len(plan.CaseFiles), func(index int) (string, json.RawMessage) {
		return plan.CaseFiles[index].Path, plan.CaseFiles[index].Body
	})
}

func newProfileImportPlanAssets(service profile.Service, nodes []profile.InterfaceNode, templates []profile.RequestTemplate, cases []profile.APICase, caseFileCount int, caseFileAt func(int) (string, json.RawMessage)) profileImportPlanAssets {
	files := make([]profileImportPlanCaseFile, 0, caseFileCount)
	for index := 0; index < caseFileCount; index++ {
		path, body := caseFileAt(index)
		files = append(files, profileImportPlanCaseFile{Path: path, Body: body})
	}
	return profileImportPlanAssets{
		Service:          service,
		InterfaceNodes:   nodes,
		RequestTemplates: templates,
		APICases:         cases,
		CaseFiles:        files,
	}
}

func writeProfileImportPlanOutput(outputDir string, report profileImportPlanReport) ([]string, error) {
	return writeProfilePlanAssetTree(outputDir, "import-plan.json", report.Plan)
}

func generationPlanAssets(plan profilegenerateopenapi.PlanResult) profileImportPlanAssets {
	return newProfileImportPlanAssets(plan.Service, plan.InterfaceNodes, nil, plan.APICases, len(plan.CaseFiles), func(index int) (string, json.RawMessage) {
		return plan.CaseFiles[index].Path, plan.CaseFiles[index].Body
	})
}

func writeProfilePlanAssetTree(outputDir string, manifestName string, assets profileImportPlanAssets) ([]string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create import plan output directory: %w", err)
	}
	written := []string{manifestName}
	for _, item := range []profile.Service{assets.Service} {
		relative := filepath.Join("services", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range assets.InterfaceNodes {
		relative := filepath.Join("interface-nodes", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range assets.RequestTemplates {
		relative := filepath.Join("request-templates", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range assets.APICases {
		relative := filepath.Join("cases", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range assets.CaseFiles {
		relative, err := safeBundleRelativePath(item.Path)
		if err != nil {
			return nil, err
		}
		if err := writeImportPlanRawJSON(outputDir, relative, item.Body); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	sort.Strings(written)
	return written, nil
}

func writeProfileImportPlanManifest(outputDir string, report profileImportPlanReport) error {
	return writeImportPlanJSON(outputDir, "import-plan.json", report)
}

func writeImportPlanJSON(outputDir string, relative string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeImportPlanRawJSON(outputDir, relative, append(raw, '\n'))
}

func writeImportPlanRawJSON(outputDir string, relative string, raw []byte) error {
	relative, err := safeBundleRelativePath(relative)
	if err != nil {
		return err
	}
	target := filepath.Join(outputDir, relative)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create import plan output directory %s: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		return fmt.Errorf("write import plan output %s: %w", target, err)
	}
	return nil
}

func safeImportPlanFileName(value string) string {
	return safeProfileAssetFileName(value, "asset")
}
