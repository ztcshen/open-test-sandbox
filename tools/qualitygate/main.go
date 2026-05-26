package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	var options Options
	var scopeFile string
	flag.StringVar(&options.Root, "root", ".", "repository root")
	flag.StringVar(&options.ReportDir, "report-dir", filepath.Join("build", "reports", "quality-gate"), "quality gate report directory")
	flag.StringVar(&options.JSCPDPath, "jscpd-json", "", "optional jscpd JSON report path")
	flag.BoolVar(&options.Strict, "strict", strings.EqualFold(os.Getenv("QUALITY_GATE_STRICT"), "true"), "exit non-zero on blocking issues")
	flag.StringVar(&scopeFile, "scope-file", "", "optional newline-delimited changed-path scope file")
	flag.Parse()

	options.ScopePaths = append(options.ScopePaths, flag.Args()...)
	if envScope := os.Getenv("QUALITY_GATE_SCOPE"); envScope != "" {
		options.ScopePaths = append(options.ScopePaths, strings.Fields(envScope)...)
	}
	if scopeFile != "" {
		paths, err := readScopeFile(scopeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "qualitygate: read scope file: %v\n", err)
			os.Exit(2)
		}
		options.ScopePaths = append(options.ScopePaths, paths...)
	}

	report, err := Analyze(options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "qualitygate: %v\n", err)
		os.Exit(2)
	}
	if err := writeReports(report, options.ReportDir); err != nil {
		fmt.Fprintf(os.Stderr, "qualitygate: write reports: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("quality gate report: %s\n", filepath.Join(options.ReportDir, "quality-gate.md"))
	fmt.Printf("quality gate json: %s\n", filepath.Join(options.ReportDir, "quality-gate.json"))
	fmt.Printf("quality gate summary: %d blocking, %d warning\n", report.Summary.Blocks, report.Summary.Warnings)
	if options.Strict && report.Summary.Blocks > 0 {
		os.Exit(1)
	}
}

func readScopeFile(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		paths = append(paths, line)
	}
	return paths, nil
}
