package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

type commandCatalogReport struct {
	OK       bool                 `json:"ok"`
	Filter   string               `json:"filter,omitempty"`
	Area     string               `json:"area,omitempty"`
	Count    int                  `json:"count"`
	Commands []commandCatalogItem `json:"commands"`
}

type commandCatalogItem struct {
	Command    string   `json:"command"`
	Area       string   `json:"area"`
	Path       []string `json:"path"`
	Usage      string   `json:"usage"`
	StoreAware bool     `json:"storeAware"`
	Tags       []string `json:"tags"`
}

func runCommands(args []string) error {
	flags := flag.NewFlagSet("commands", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	filter := flags.String("filter", "", "Filter command catalog by command, area, usage, or tag")
	area := flags.String("area", "", "Restrict command catalog to one area, such as store, case, workflow, or environment")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable command catalog")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report := commandCatalogForArea(*filter, *area)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCommandCatalog(report)
	return nil
}

func commandCatalog(filter string) commandCatalogReport {
	return commandCatalogForArea(filter, "")
}

func commandCatalogForArea(filter string, area string) commandCatalogReport {
	filter = strings.TrimSpace(filter)
	area = strings.TrimSpace(area)
	report := commandCatalogReport{OK: true, Filter: filter, Area: area, Commands: []commandCatalogItem{}}
	for _, usage := range commandUsageLines() {
		item := commandCatalogItemFromUsage(usage)
		if len(item.Path) == 0 {
			continue
		}
		if area != "" && item.Area != area {
			continue
		}
		if !commandCatalogMatches(item, filter) {
			continue
		}
		report.Commands = append(report.Commands, item)
	}
	report.Count = len(report.Commands)
	return report
}

func commandUsageLines() []string {
	lines := strings.Split(helpText(), "\n")
	out := []string{}
	inUsage := false
	for _, line := range lines {
		usage := strings.TrimSpace(line)
		if usage == "Usage:" {
			inUsage = true
			continue
		}
		if inUsage && usage == "" {
			break
		}
		if !inUsage {
			continue
		}
		if strings.HasPrefix(usage, "agent-testbench ") {
			out = append(out, usage)
		}
	}
	return out
}

func commandCatalogItemFromUsage(usage string) commandCatalogItem {
	rest := strings.TrimSpace(strings.TrimPrefix(usage, "agent-testbench "))
	fields := strings.Fields(rest)
	path := []string{}
	for _, field := range fields {
		if commandUsagePathStops(field) {
			break
		}
		path = append(path, strings.Trim(field, ","))
	}
	area := ""
	if len(path) > 0 {
		area = path[0]
	}
	tags := commandCatalogTags(area, usage)
	return commandCatalogItem{
		Command:    strings.Join(path, " "),
		Area:       area,
		Path:       path,
		Usage:      usage,
		StoreAware: strings.Contains(usage, "--store NAME_OR_DSN"),
		Tags:       tags,
	}
}

func commandUsagePathStops(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || strings.HasPrefix(token, "[") || strings.HasPrefix(token, "(") || strings.HasPrefix(token, "--") || strings.Contains(token, "|") {
		return true
	}
	trimmed := strings.Trim(token, ".,")
	if strings.Contains(trimmed, "=") || strings.Contains(trimmed, ":") || strings.Contains(trimmed, "/") {
		return true
	}
	hasLetter := false
	for _, item := range trimmed {
		if item >= 'a' && item <= 'z' {
			return false
		}
		if item >= 'A' && item <= 'Z' {
			hasLetter = true
		}
	}
	return hasLetter
}

func commandCatalogTags(area string, usage string) []string {
	tags := []string{area}
	if strings.Contains(usage, "--store NAME_OR_DSN") {
		tags = append(tags, "store-first")
	}
	if strings.Contains(usage, "--json") {
		tags = append(tags, "json")
	}
	if strings.Contains(usage, "gate") || strings.Contains(usage, "verify") || strings.Contains(usage, "acceptance") {
		tags = append(tags, "quality-gate")
	}
	if strings.Contains(usage, "diagnose") || strings.Contains(usage, "evidence") || strings.Contains(usage, "trace") {
		tags = append(tags, "evidence")
	}
	if strings.Contains(usage, "workflow") {
		tags = append(tags, "workflow")
	}
	return normalizeStringList(tags)
}

func commandCatalogMatches(item commandCatalogItem, filter string) bool {
	if filter == "" {
		return true
	}
	needle := normalizedDiscoveryText(filter)
	haystack := normalizedDiscoveryText(strings.Join(append([]string{item.Command, item.Area, item.Usage}, item.Tags...), " "))
	return strings.Contains(haystack, needle)
}

func printCommandCatalog(report commandCatalogReport) {
	fmt.Println("Commands")
	fmt.Printf("Total: %d\n", report.Count)
	if report.Filter != "" {
		fmt.Printf("Filter: %s\n", report.Filter)
	}
	if report.Area != "" {
		fmt.Printf("Area: %s\n", report.Area)
	}
	for _, item := range report.Commands {
		fmt.Printf("- %s [%s]\n", item.Command, item.Area)
		fmt.Printf("  %s\n", item.Usage)
	}
}
