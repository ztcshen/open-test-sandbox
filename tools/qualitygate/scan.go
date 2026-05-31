package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type scanState struct {
	cfg        Config
	modulePath string
	inScope    func(string) bool
	packages   map[string]*PackageMetric
}

func Analyze(options Options) (Report, error) {
	cfg := DefaultConfig()
	root := options.Root
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Report{}, err
	}
	modulePath, err := readModulePath(absRoot)
	if err != nil {
		return Report{}, err
	}

	report := Report{
		GeneratedAt: nowUTC(),
		Mode:        modeName(options.Strict),
		Root:        absRoot,
		ScopePaths:  normalizedScope(options.ScopePaths),
		Inputs: ReportInputs{
			JSCPDReport: options.JSCPDPath,
		},
	}
	state := scanState{
		cfg:        cfg,
		modulePath: modulePath,
		inScope:    scopeMatcher(options.ScopePaths),
		packages:   map[string]*PackageMetric{},
	}
	if err := filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		rel = normalizePath(rel)
		if entry.IsDir() {
			if rel != "" && shouldSkipPath(rel, cfg) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || shouldSkipPath(rel, cfg) {
			return nil
		}
		return state.scanGoFile(absRoot, rel, &report)
	}); err != nil {
		return Report{}, err
	}

	state.addPackageIssues(&report)
	if options.JSCPDPath != "" {
		if err := addJSCPDIssues(options.JSCPDPath, &report, cfg); err != nil {
			return Report{}, err
		}
	}
	addCombinationIssues(&report, cfg, state.inScope)
	if diff, ok := gitDiff(absRoot); ok {
		report.Inputs.GitDiff = true
		addGitSafetyIssues(diff, &report, cfg)
	}
	finalizeReport(&report)
	return report, nil
}

func (s *scanState) scanGoFile(root string, rel string, report *Report) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	generated := isGeneratedPath(rel, s.cfg) || hasGeneratedComment(raw, s.cfg)
	if generated {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, raw, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse imports %s: %w", rel, err)
	}
	fullFile, err := parser.ParseFile(fset, path, raw, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse %s: %w", rel, err)
	}

	pkgDir := packageDirForFile(rel)
	effectiveLines := effectiveLineCount(raw)
	fileMetric := FileMetric{
		Path:           rel,
		Package:        file.Name.Name,
		PackageDir:     pkgDir,
		EffectiveLines: effectiveLines,
		TestFile:       strings.HasSuffix(rel, "_test.go"),
		Generated:      generated,
	}
	pkg := s.packageMetric(pkgDir, file.Name.Name)
	pkg.FileCount++
	pkg.EffectiveLines += effectiveLines

	for _, decl := range fullFile.Decls {
		switch node := decl.(type) {
		case *ast.FuncDecl:
			fileMetric.FunctionCount++
			start := fset.Position(node.Pos()).Line
			end := fset.Position(node.End()).Line
			fn := FunctionMetric{
				Name:       functionName(node),
				File:       rel,
				PackageDir: pkgDir,
				StartLine:  start,
				EndLine:    end,
				Lines:      end - start + 1,
				Statements: countFunctionStatements(node),
			}
			report.Metrics.Functions = append(report.Metrics.Functions, fn)
			s.addFunctionSizeIssue(report, fn)
		case *ast.GenDecl:
			s.scanTypes(node, fset, rel, pkgDir, report)
		}
	}

	report.Metrics.Files = append(report.Metrics.Files, fileMetric)
	s.addThresholdIssue(report, thresholdIssueInput{
		ID:             "file-lines",
		Category:       CategorySize,
		Message:        "Go file is larger than the local AI-safe file budget",
		File:           rel,
		Package:        pkgDir,
		Value:          effectiveLines,
		Warn:           s.fileLineWarningThreshold(rel),
		Block:          s.cfg.FileEffectiveLinesBlock,
		Recommendation: "Do not split mechanically; first identify cohesive responsibilities and keep helpers package-local.",
		ConfigOnly:     isConfigOrEnumFile(rel),
	})
	s.addThresholdIssue(report, thresholdIssueInput{
		ID:             "file-function-count",
		Category:       CategorySize,
		Message:        "Go file contains too many functions for reliable AI edits",
		File:           rel,
		Package:        pkgDir,
		Value:          fileMetric.FunctionCount,
		Warn:           s.fileFunctionCountWarningThreshold(rel),
		Block:          s.cfg.FileFunctionCountBlock,
		Recommendation: "Group functions by behavior or command surface; avoid dumping shared code into helper packages.",
		ConfigOnly:     strings.HasSuffix(rel, "_test.go"),
	})
	s.addImportBoundaryIssues(file, fset, rel, pkgDir, report)
	s.addNoSemanticPackageIssue(rel, report)
	return nil
}

func (s *scanState) scanTypes(decl *ast.GenDecl, fset *token.FileSet, rel string, pkgDir string, report *Report) {
	for _, spec := range decl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		switch value := typeSpec.Type.(type) {
		case *ast.StructType:
			count := fieldCount(value.Fields)
			s.addThresholdIssue(report, thresholdIssueInput{
				ID:             "struct-fields",
				Category:       CategorySize,
				Message:        "struct has more fields than the local AI-safe data-shape budget",
				File:           rel,
				Line:           fset.Position(typeSpec.Pos()).Line,
				Package:        pkgDir,
				Value:          count,
				Warn:           s.cfg.StructFieldsWarn,
				Block:          s.cfg.StructFieldsBlock,
				Recommendation: "Keep natural DTOs when appropriate; extract domain value objects only when fields form a real concept.",
				ConfigOnly:     isConfigOrEnumFile(rel),
			})
		case *ast.InterfaceType:
			count := fieldCount(value.Methods)
			s.addThresholdIssue(report, thresholdIssueInput{
				ID:             "interface-methods",
				Category:       CategorySize,
				Message:        "interface has more methods than the local AI-safe boundary budget",
				File:           rel,
				Line:           fset.Position(typeSpec.Pos()).Line,
				Package:        pkgDir,
				Value:          count,
				Warn:           s.cfg.InterfaceMethodsWarn,
				Block:          s.cfg.InterfaceMethodsBlock,
				Recommendation: "Prefer small behavior interfaces only when multiple implementations, mocks, or a real boundary need them.",
			})
		}
	}
}

func (s *scanState) addPackageIssues(report *Report) {
	for _, metric := range s.packages {
		report.Metrics.Packages = append(report.Metrics.Packages, *metric)
		warnLines, blockLines, warnFiles, blockFiles := s.packageThreshold(metric.Dir)
		s.addThresholdIssue(report, thresholdIssueInput{
			ID:             "package-lines",
			Category:       "package",
			Message:        "package has grown beyond the local AI-safe line budget",
			Package:        metric.Dir,
			File:           metric.Dir,
			Value:          metric.EffectiveLines,
			Warn:           warnLines,
			Block:          blockLines,
			Recommendation: "Split packages only around stable semantics, not to appease a line counter.",
		})
		s.addThresholdIssue(report, thresholdIssueInput{
			ID:             "package-file-count",
			Category:       "package",
			Message:        "package has too many Go files for reliable AI navigation",
			Package:        metric.Dir,
			File:           metric.Dir,
			Value:          metric.FileCount,
			Warn:           warnFiles,
			Block:          blockFiles,
			Recommendation: "Check whether the package has multiple concepts before introducing a new package boundary.",
		})
	}
	sort.Slice(report.Metrics.Packages, func(i int, j int) bool {
		return report.Metrics.Packages[i].Dir < report.Metrics.Packages[j].Dir
	})
}

func (s *scanState) addFunctionSizeIssue(report *Report, fn FunctionMetric) {
	if !s.inScope(fn.File) {
		return
	}
	warnLines, warnStatements := s.functionWarningThreshold(fn.File)
	severity := ""
	if fn.Lines > s.cfg.FunctionLinesBlock {
		severity = SeverityBlock
	} else if fn.Lines > warnLines {
		severity = SeverityWarning
	}
	if severity == "" {
		return
	}
	report.addIssue(Issue{
		ID:             "function-lines",
		Severity:       scopedSeverity(severity, fn.File, s.inScope),
		Category:       CategorySize,
		Message:        "function is longer than the AI-safe budget",
		File:           fn.File,
		Line:           fn.StartLine,
		EndLine:        fn.EndLine,
		Package:        fn.PackageDir,
		Function:       fn.Name,
		Value:          fmt.Sprintf("%d lines, %d statements", fn.Lines, fn.Statements),
		Threshold:      fmt.Sprintf("warning>%d lines, density note>%d statements, blocking>%d lines", warnLines, warnStatements, s.cfg.FunctionLinesBlock),
		Recommendation: "Split by business step or policy boundary only when the extracted name has domain meaning.",
	})
}

func (s *scanState) functionWarningThreshold(file string) (int, int) {
	switch {
	case strings.HasSuffix(file, "_test.go"):
		return 99, 80
	case isRouteRegistrationFile(file):
		return 99, 80
	case isCLISurfaceFile(file),
		isControlPlaneSurfaceFile(file),
		isStoreSchemaSurfaceFile(file),
		isProfileArtifactSurfaceFile(file),
		isRunnerEvidenceSurfaceFile(file),
		isStoreContractSurfaceFile(file),
		isQualityGateToolFile(file):
		return 99, 80
	default:
		return s.cfg.FunctionLinesWarn, s.cfg.FunctionStatementsWarn
	}
}

func (s *scanState) fileLineWarningThreshold(file string) int {
	if strings.HasSuffix(file, "_test.go") ||
		isCLISurfaceFile(file) ||
		isControlPlaneSurfaceFile(file) ||
		isStoreSchemaSurfaceFile(file) ||
		isProfileArtifactSurfaceFile(file) ||
		isRunnerEvidenceSurfaceFile(file) ||
		isStoreContractSurfaceFile(file) ||
		isQualityGateToolFile(file) {
		return s.cfg.FileEffectiveLinesBlock - 1
	}
	return s.cfg.FileEffectiveLinesWarn
}

func (s *scanState) fileFunctionCountWarningThreshold(file string) int {
	if isStoreSchemaSurfaceFile(file) {
		return s.cfg.FileFunctionCountBlock - 1
	}
	return s.cfg.FileFunctionCountWarn
}

func (s *scanState) packageThreshold(dir string) (int, int, int, int) {
	switch dir {
	case "cmd/agent-testbench":
		return 30000, 36000, 140, 160
	case "internal/server/controlplane":
		return 30000, 36000, 150, 170
	case "internal/store/sqlstore":
		return 4500, 6000, 30, 40
	case "internal/domain/casesuite":
		return 3000, 4000, 20, 30
	case "internal/store/sqlite":
		return 2500, 3500, 15, 25
	case "tools/qualitygate":
		return 2500, 3500, 12, 20
	default:
		return s.cfg.PackageLinesWarn, s.cfg.PackageLinesBlock, s.cfg.PackageFileCountWarn, s.cfg.PackageFileCountBlock
	}
}

func (s *scanState) packageMetric(dir string, name string) *PackageMetric {
	if metric, ok := s.packages[dir]; ok {
		return metric
	}
	metric := &PackageMetric{Dir: dir, Name: name}
	s.packages[dir] = metric
	return metric
}

type thresholdIssueInput struct {
	ID             string
	Category       string
	Message        string
	File           string
	Line           int
	EndLine        int
	Package        string
	Function       string
	Value          int
	Warn           int
	Block          int
	Recommendation string
	ConfigOnly     bool
}

func (s *scanState) addThresholdIssue(report *Report, input thresholdIssueInput) {
	if !s.inScope(input.File) {
		return
	}
	severity := ""
	if input.Block > 0 && input.Value > input.Block {
		severity = SeverityBlock
	} else if input.Value > input.Warn {
		severity = SeverityWarning
	}
	if severity == "" {
		return
	}
	if input.ConfigOnly && severity == SeverityBlock {
		severity = SeverityWarning
	}
	report.addIssue(Issue{
		ID:             input.ID,
		Severity:       scopedSeverity(severity, input.File, s.inScope),
		Category:       input.Category,
		Message:        input.Message,
		File:           input.File,
		Line:           input.Line,
		EndLine:        input.EndLine,
		Package:        input.Package,
		Function:       input.Function,
		Value:          strconv.Itoa(input.Value),
		Threshold:      fmt.Sprintf("warning>%d blocking>%d", input.Warn, input.Block),
		Recommendation: input.Recommendation,
	})
}

func (s *scanState) addImportBoundaryIssues(file *ast.File, fset *token.FileSet, rel string, pkgDir string, report *Report) {
	if !s.inScope(rel) {
		return
	}
	for _, item := range file.Imports {
		importPath, err := strconv.Unquote(item.Path.Value)
		if err != nil {
			continue
		}
		message, ok := architectureViolation(pkgDir, importPath, s.modulePath)
		if !ok {
			continue
		}
		report.addIssue(Issue{
			ID:             "architecture-boundary",
			Severity:       scopedSeverity(SeverityBlock, rel, s.inScope),
			Category:       "architecture",
			Message:        message,
			File:           rel,
			Line:           fset.Position(item.Pos()).Line,
			Package:        pkgDir,
			Value:          importPath,
			Recommendation: "Route dependencies through the owning domain/usecase/repository/client boundary; do not bypass layers from handler or domain code.",
		})
	}
}

func (s *scanState) addNoSemanticPackageIssue(rel string, report *Report) {
	if !s.inScope(rel) {
		return
	}
	pkgDir := packageDirForFile(rel)
	base := filepath.Base(pkgDir)
	if !s.cfg.NoSemanticPackageNames[base] || !isCorePath(pkgDir) {
		return
	}
	report.addIssue(Issue{
		ID:             "no-semantic-package-name",
		Severity:       scopedSeverity(SeverityBlock, rel, s.inScope),
		Category:       "ai-abstraction-safety",
		Message:        "package name is too generic for AI-driven duplicate remediation",
		File:           rel,
		Package:        pkgDir,
		Value:          base,
		Recommendation: "Use a domain name such as policy, validator, calculator, repository, or client wrapper when that concept truly exists.",
	})
}

func architectureViolation(currentDir string, importPath string, modulePath string) (string, bool) {
	if modulePath == "" || !strings.HasPrefix(importPath, modulePath+"/") {
		return "", false
	}
	target := strings.TrimPrefix(importPath, modulePath+"/")
	for _, rule := range []func(string, string) (string, bool){
		handlerImportViolation,
		domainImportViolation,
		serviceImportViolation,
		runnerImportViolation,
		storeImportViolation,
		serverImportViolation,
		publicPackageImportViolation,
	} {
		if message, ok := rule(currentDir, target); ok {
			return message, true
		}
	}
	if strings.HasPrefix(currentDir, "internal/") && strings.HasPrefix(target, "cmd/") {
		return "internal package depends on CLI package", true
	}
	return "", false
}

func handlerImportViolation(currentDir string, target string) (string, bool) {
	if !hasPathSegment(currentDir, "handler", "controller") {
		return "", false
	}
	if hasPathSegment(target, "dao", "mapper", "infra", "infrastructure") || strings.HasPrefix(target, "internal/store") {
		return "handler/controller package depends directly on persistence or infrastructure", true
	}
	return "", false
}

func domainImportViolation(currentDir string, target string) (string, bool) {
	if !strings.HasPrefix(currentDir, "internal/domain") {
		return "", false
	}
	if strings.HasPrefix(target, "internal/store") ||
		strings.HasPrefix(target, "internal/server") ||
		strings.HasPrefix(target, "internal/runner") ||
		strings.HasPrefix(target, "cmd/") ||
		hasPathSegment(target, "dao", "infra", "infrastructure", "repository", "client") {
		return "domain package depends on infrastructure, Store, runner, server, repository, or client implementation", true
	}
	return "", false
}

func serviceImportViolation(currentDir string, target string) (string, bool) {
	if !hasPathSegment(currentDir, "service", "usecase", "biz") {
		return "", false
	}
	if hasPathSegment(target, "handler", "controller") || strings.HasPrefix(target, "internal/server/controlplane") {
		return "service/usecase/biz package depends upward on handler/controller code", true
	}
	return "", false
}

func runnerImportViolation(currentDir string, target string) (string, bool) {
	if strings.HasPrefix(currentDir, "internal/runner") &&
		(strings.HasPrefix(target, "internal/server") || strings.HasPrefix(target, "cmd/")) {
		return "runner package depends upward on server or CLI packages", true
	}
	return "", false
}

func storeImportViolation(currentDir string, target string) (string, bool) {
	if strings.HasPrefix(currentDir, "internal/store") &&
		(strings.HasPrefix(target, "internal/server") ||
			strings.HasPrefix(target, "internal/runner") ||
			strings.HasPrefix(target, "cmd/")) {
		return "store package depends upward on runner, server, or CLI packages", true
	}
	return "", false
}

func serverImportViolation(currentDir string, target string) (string, bool) {
	if strings.HasPrefix(currentDir, "internal/server") && strings.HasPrefix(target, "cmd/") {
		return "server package depends on CLI package", true
	}
	return "", false
}

func publicPackageImportViolation(currentDir string, target string) (string, bool) {
	if strings.HasPrefix(currentDir, "pkg/") && strings.HasPrefix(target, "internal/") {
		return "public pkg package depends on internal business code", true
	}
	return "", false
}

func readModulePath(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", scanner.Err()
}

func effectiveLineCount(raw []byte) int {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	count := 0
	inBlock := false
	for scanner.Scan() {
		line := scanner.Text()
		code := stripComments(line, &inBlock)
		if strings.TrimSpace(code) != "" {
			count++
		}
	}
	return count
}

func countFunctionStatements(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}
	count := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node.(type) {
		case nil:
			return true
		case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
			return true
		case ast.Stmt:
			count++
		}
		return true
	})
	return count
}

func stripComments(line string, inBlock *bool) string {
	var out strings.Builder
	for i := 0; i < len(line); {
		if *inBlock {
			end := strings.Index(line[i:], "*/")
			if end < 0 {
				return out.String()
			}
			i += end + 2
			*inBlock = false
			continue
		}
		if strings.HasPrefix(line[i:], "/*") {
			*inBlock = true
			i += 2
			continue
		}
		if strings.HasPrefix(line[i:], "//") {
			break
		}
		out.WriteByte(line[i])
		i++
	}
	return out.String()
}

func fieldCount(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			count++
			continue
		}
		count += len(field.Names)
	}
	return count
}

func functionName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return fmt.Sprintf("(%s).%s", receiverName(fn.Recv.List[0].Type), fn.Name.Name)
}

func receiverName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverName(value.X)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func hasGeneratedComment(raw []byte, cfg Config) bool {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for i := 0; scanner.Scan() && i < 8; i++ {
		line := strings.TrimSpace(scanner.Text())
		for _, prefix := range cfg.GeneratedCommentPrefixes {
			if strings.HasPrefix(line, prefix) {
				return true
			}
		}
	}
	return false
}

func scopedSeverity(severity string, path string, inScope func(string) bool) string {
	if severity == SeverityBlock && !inScope(path) {
		return SeverityWarning
	}
	return severity
}

func normalizedScope(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = normalizePath(path)
		if path != "" {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}
