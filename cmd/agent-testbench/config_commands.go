package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type configShowReport struct {
	OK     bool                    `json:"ok"`
	Path   string                  `json:"path"`
	Active string                  `json:"active,omitempty"`
	Stores []configShowStoreReport `json:"stores"`
}

type configShowStoreReport struct {
	Name    string `json:"name"`
	Backend string `json:"backend"`
	URL     string `json:"url"`
	Active  bool   `json:"active"`
}

func runConfigPath(args []string) error {
	flags := flag.NewFlagSet("config path", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected config path arguments: %s", strings.Join(flags.Args(), " "))
	}
	path, err := storeConfigPath()
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

func runConfigShow(args []string) error {
	flags := flag.NewFlagSet("config show", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable config report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected config show arguments: %s", strings.Join(flags.Args(), " "))
	}
	report, err := buildConfigShowReport()
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printConfigShowReport(report)
	return nil
}

func buildConfigShowReport() (configShowReport, error) {
	path, err := storeConfigPath()
	if err != nil {
		return configShowReport{}, err
	}
	cfg, err := loadStoreConfig()
	if err != nil {
		return configShowReport{}, err
	}
	names := make([]string, 0, len(cfg.Stores))
	for name := range cfg.Stores {
		names = append(names, name)
	}
	sort.Strings(names)
	stores := make([]configShowStoreReport, 0, len(names))
	for _, name := range names {
		entry := cfg.Stores[name]
		stores = append(stores, configShowStoreReport{
			Name:    entry.Name,
			Backend: entry.Backend,
			URL:     maskStoreURL(entry.URL),
			Active:  name == cfg.Active,
		})
	}
	return configShowReport{OK: true, Path: path, Active: cfg.Active, Stores: stores}, nil
}

func printConfigShowReport(report configShowReport) {
	fmt.Println("AgentTestBench Config")
	fmt.Printf("Path: %s\n", report.Path)
	fmt.Printf("Active Store: %s\n", stringDefault(report.Active, "(none)"))
	for _, store := range report.Stores {
		marker := " "
		if store.Active {
			marker = "*"
		}
		fmt.Printf("%s %s\t%s\t%s\n", marker, store.Name, store.Backend, store.URL)
	}
}

func runConfigEdit(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("config edit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected config edit arguments: %s", strings.Join(flags.Args(), " "))
	}
	path, err := storeConfigPath()
	if err != nil {
		return err
	}
	cfg, err := loadStoreConfig()
	if err != nil {
		return err
	}
	if err := saveStoreConfig(cfg); err != nil {
		return err
	}
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		return fmt.Errorf("EDITOR is required to run config edit; use config path to open %s manually", path)
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return fmt.Errorf("EDITOR is required to run config edit")
	}
	cmd := exec.CommandContext(ctx, parts[0], append(parts[1:], path)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
