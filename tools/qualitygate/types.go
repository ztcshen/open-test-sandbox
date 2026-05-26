package main

import "time"

const (
	SeverityWarning = "warning"
	SeverityBlock   = "block"

	CategoryAISafety = "ai-safety"
	CategoryCombined = "combined"
	CategorySize     = "size"
)

type Options struct {
	Root       string
	ReportDir  string
	JSCPDPath  string
	Strict     bool
	ScopePaths []string
}

type Config struct {
	FileEffectiveLinesWarn   int
	FileEffectiveLinesBlock  int
	FunctionLinesWarn        int
	FunctionLinesBlock       int
	FunctionDuplicateLines   int
	StructFieldsWarn         int
	StructFieldsBlock        int
	InterfaceMethodsWarn     int
	InterfaceMethodsBlock    int
	PackageLinesWarn         int
	PackageLinesBlock        int
	PackageFileCountWarn     int
	PackageFileCountBlock    int
	FileFunctionCountWarn    int
	FileFunctionCountBlock   int
	DuplicatePercentWarn     float64
	DuplicatePercentBlock    float64
	DuplicateBlockLines      int
	PackageDuplicateBlocks   int
	NoSemanticPackageNames   map[string]bool
	ExcludedDirs             map[string]bool
	ExcludedFileSuffixes     []string
	GeneratedFileSuffixes    []string
	GeneratedPathFragments   []string
	GeneratedCommentPrefixes []string
}

type Report struct {
	GeneratedAt time.Time    `json:"generatedAt"`
	Mode        string       `json:"mode"`
	Root        string       `json:"root"`
	ScopePaths  []string     `json:"scopePaths,omitempty"`
	Summary     Summary      `json:"summary"`
	Issues      []Issue      `json:"issues"`
	Metrics     Metrics      `json:"metrics"`
	Inputs      ReportInputs `json:"inputs"`
	ToolNotes   []string     `json:"toolNotes,omitempty"`
}

type Summary struct {
	Warnings int  `json:"warnings"`
	Blocks   int  `json:"blocks"`
	Passed   bool `json:"passed"`
}

type ReportInputs struct {
	JSCPDReport string `json:"jscpdReport,omitempty"`
	GitDiff     bool   `json:"gitDiff"`
}

type Metrics struct {
	Files     []FileMetric     `json:"files,omitempty"`
	Functions []FunctionMetric `json:"functions,omitempty"`
	Packages  []PackageMetric  `json:"packages,omitempty"`
	Duplicate DuplicateMetric  `json:"duplicate"`
	GitSafety GitSafetyMetric  `json:"gitSafety"`
}

type FileMetric struct {
	Path           string `json:"path"`
	Package        string `json:"package"`
	PackageDir     string `json:"packageDir"`
	EffectiveLines int    `json:"effectiveLines"`
	FunctionCount  int    `json:"functionCount"`
	TestFile       bool   `json:"testFile"`
	Generated      bool   `json:"generated"`
}

type FunctionMetric struct {
	Name       string `json:"name"`
	File       string `json:"file"`
	PackageDir string `json:"packageDir"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
	Lines      int    `json:"lines"`
}

type PackageMetric struct {
	Dir            string `json:"dir"`
	Name           string `json:"name"`
	FileCount      int    `json:"fileCount"`
	EffectiveLines int    `json:"effectiveLines"`
}

type DuplicateMetric struct {
	Percentage      float64          `json:"percentage"`
	DuplicatedLines int              `json:"duplicatedLines"`
	CloneCount      int              `json:"cloneCount"`
	Blocks          []DuplicateBlock `json:"blocks,omitempty"`
}

type DuplicateBlock struct {
	FirstFile      string `json:"firstFile"`
	FirstStart     int    `json:"firstStart"`
	FirstEnd       int    `json:"firstEnd"`
	SecondFile     string `json:"secondFile"`
	SecondStart    int    `json:"secondStart"`
	SecondEnd      int    `json:"secondEnd"`
	Lines          int    `json:"lines"`
	Kind           string `json:"kind"`
	Recommendation string `json:"recommendation"`
}

type GitSafetyMetric struct {
	AddedLines           int      `json:"addedLines"`
	DeletedLines         int      `json:"deletedLines"`
	ChangedFiles         int      `json:"changedFiles"`
	DeletedTests         []string `json:"deletedTests,omitempty"`
	PublicAPITouchpoints []string `json:"publicApiTouchpoints,omitempty"`
	SensitiveFiles       []string `json:"sensitiveFiles,omitempty"`
	ErrorHandlingDeletes []string `json:"errorHandlingDeletes,omitempty"`
}

type Issue struct {
	ID             string   `json:"id"`
	Severity       string   `json:"severity"`
	Category       string   `json:"category"`
	Message        string   `json:"message"`
	File           string   `json:"file,omitempty"`
	Line           int      `json:"line,omitempty"`
	EndLine        int      `json:"endLine,omitempty"`
	Package        string   `json:"package,omitempty"`
	Function       string   `json:"function,omitempty"`
	Value          string   `json:"value,omitempty"`
	Threshold      string   `json:"threshold,omitempty"`
	DuplicateKind  string   `json:"duplicateKind,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
	Evidence       []string `json:"evidence,omitempty"`
	Historical     bool     `json:"historical,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		FileEffectiveLinesWarn:  400,
		FileEffectiveLinesBlock: 600,
		FunctionLinesWarn:       60,
		FunctionLinesBlock:      100,
		FunctionDuplicateLines:  80,
		StructFieldsWarn:        25,
		StructFieldsBlock:       40,
		InterfaceMethodsWarn:    10,
		InterfaceMethodsBlock:   20,
		PackageLinesWarn:        1500,
		PackageLinesBlock:       0,
		PackageFileCountWarn:    20,
		PackageFileCountBlock:   0,
		FileFunctionCountWarn:   25,
		FileFunctionCountBlock:  45,
		DuplicatePercentWarn:    5,
		DuplicatePercentBlock:   8,
		DuplicateBlockLines:     40,
		PackageDuplicateBlocks:  3,
		NoSemanticPackageNames: map[string]bool{
			"common":  true,
			"commons": true,
			"helper":  true,
			"helpers": true,
			"util":    true,
			"utils":   true,
		},
		ExcludedDirs: map[string]bool{
			".git":         true,
			".idea":        true,
			".runtime":     true,
			".scratch":     true,
			"node_modules": true,
			"vendor":       true,
			"third_party":  true,
			"generated":    true,
			"gen":          true,
			"mocks":        true,
			"mock":         true,
			"testdata":     true,
			"docs":         true,
			"migrations":   true,
			"scripts":      true,
		},
		ExcludedFileSuffixes: []string{
			".pb.go",
			".pb.gw.go",
			".gen.go",
			"_mock.go",
			"wire_gen.go",
		},
		GeneratedFileSuffixes: []string{
			".pb.go",
			".pb.gw.go",
			".gen.go",
			"_mock.go",
			"wire_gen.go",
		},
		GeneratedPathFragments: []string{
			"/swagger/",
			"/openapi/",
			"/control-plane/static/assets/react/",
		},
		GeneratedCommentPrefixes: []string{
			"// Code generated",
			"// This file was generated",
			"// DO NOT EDIT",
		},
	}
}
