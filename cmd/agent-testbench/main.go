package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-testbench/internal/domain/casesuite"
	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/domain/profileaudit"
	"agent-testbench/internal/domain/profilecatalog"
	profilegenerateopenapi "agent-testbench/internal/domain/profilegenerate/openapi"
	"agent-testbench/internal/domain/profilehome"
	profileimporthttpcapture "agent-testbench/internal/domain/profileimport/httpcapture"
	profileimportopenapi "agent-testbench/internal/domain/profileimport/openapi"
	"agent-testbench/internal/domain/redaction"
	"agent-testbench/internal/domain/workflowaudit"
	"agent-testbench/internal/runner/apicase"
	"agent-testbench/internal/runner/evidence"
	"agent-testbench/internal/runner/executor"
	"agent-testbench/internal/runner/junit"
	"agent-testbench/internal/runner/requesttemplate"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/mysql"
	storeopen "agent-testbench/internal/store/open"
	"agent-testbench/internal/store/postgres"
	"agent-testbench/internal/store/sqlite"
	"agent-testbench/internal/store/sqlstore"
)

const version = "0.1.0"
const environmentRestoreAttemptLimit = 20

var postgresSchemaStatus = postgres.SchemaStatus
var postgresUpgradeSchema = postgres.UpgradeSchema
var mysqlSchemaStatus = mysql.SchemaStatus
var mysqlUpgradeSchema = mysql.UpgradeSchema
var mysqlProvisionDatabase = mysql.ProvisionDatabase

type profileImportReport struct {
	ProfileID     string               `json:"profileId"`
	BundlePath    string               `json:"bundlePath"`
	BundleDigest  string               `json:"bundleDigest"`
	Counts        profileImportCounts  `json:"counts"`
	Diff          profileImportDiff    `json:"diff"`
	StorePath     string               `json:"storePath"`
	CatalogIndex  profileCatalogIndex  `json:"catalogIndex"`
	ConfigVersion profileConfigVersion `json:"configVersion"`
	ReadModels    []string             `json:"readModels"`
	ImportedAt    time.Time            `json:"importedAt"`
	Audit         *profileaudit.Report `json:"audit,omitempty"`
}

type profileImportCounts struct {
	Services         int `json:"services"`
	Workflows        int `json:"workflows"`
	InterfaceNodes   int `json:"interfaceNodes"`
	APICases         int `json:"apiCases"`
	RequestTemplates int `json:"requestTemplates"`
	CaseDependencies int `json:"caseDependencies"`
	WorkflowBindings int `json:"workflowBindings"`
	Fixtures         int `json:"fixtures"`
}

type profileImportDiff struct {
	HasPreviousCatalog bool                         `json:"hasPreviousCatalog"`
	Before             profileImportCounts          `json:"before"`
	After              profileImportCounts          `json:"after"`
	APICases           profileImportCaseDiff        `json:"apiCases"`
	NodeCaseDeltas     []profileImportNodeCaseDelta `json:"nodeCaseDeltas,omitempty"`
}

type profileImportCaseDiff struct {
	Before  int      `json:"before"`
	After   int      `json:"after"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

type profileImportNodeCaseDelta struct {
	NodeID string `json:"nodeId"`
	Before int    `json:"before"`
	After  int    `json:"after"`
	Delta  int    `json:"delta"`
}

type profileCatalogIndex struct {
	ProfileID string                    `json:"profileId"`
	IndexedAt time.Time                 `json:"indexedAt"`
	Counts    profileCatalogIndexCounts `json:"counts"`
}

type profileCatalogIndexReport struct {
	ProfileID     string                `json:"profileId"`
	IndexedAt     time.Time             `json:"indexedAt"`
	Counts        profileImportCounts   `json:"counts"`
	ConfigVersion *profileConfigVersion `json:"configVersion,omitempty"`
}

type profileCatalogIndexCounts struct {
	Services         int `json:"services"`
	Workflows        int `json:"workflows"`
	InterfaceNodes   int `json:"interfaceNodes"`
	APICases         int `json:"apiCases"`
	RequestTemplates int `json:"requestTemplates"`
	CaseDependencies int `json:"caseDependencies"`
	WorkflowBindings int `json:"workflowBindings"`
	Fixtures         int `json:"fixtures"`
	Templates        int `json:"templates"`
	TemplateConfigs  int `json:"templateConfigs"`
}

type profileConfigVersion struct {
	ID           string    `json:"id"`
	ProfileID    string    `json:"profileId"`
	SourcePath   string    `json:"sourcePath"`
	BundleDigest string    `json:"bundleDigest"`
	Active       bool      `json:"active"`
	PublishedAt  time.Time `json:"publishedAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

type profileVerifyReport struct {
	OK        bool                 `json:"ok"`
	Error     string               `json:"error,omitempty"`
	ProfileID string               `json:"profileId"`
	Audit     profileaudit.Report  `json:"audit"`
	Publish   profileImportReport  `json:"publish"`
	Summary   profileVerifySummary `json:"summary"`
	Checks    []profileVerifyCheck `json:"checks"`
}

type profileVerifySummary struct {
	TotalChecks          int    `json:"totalChecks"`
	PassedChecks         int    `json:"passedChecks"`
	FailedChecks         int    `json:"failedChecks"`
	RequiredCaseRuns     bool   `json:"requiredCaseRuns"`
	RequiredWorkflowRuns bool   `json:"requiredWorkflowRuns"`
	FirstFailed          string `json:"firstFailed,omitempty"`
}

type profileVerifyCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type profileVerifyOptions struct {
	RequireCaseRuns     bool
	RequireWorkflowRuns bool
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Printf("AgentTestBench %s\n", version)
	case "help", "--help", "-h":
		printHelp()
	case "commands":
		if err := runCommands(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "store":
		if err := runStore(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "sandbox":
		if err := runSandbox(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "environment":
		if err := runEnvironment(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "profile":
		if err := runProfile(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "template-package", "template-packages":
		if err := runTemplatePackage(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "config":
		if err := runConfig(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "evidence":
		if err := runEvidence(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "trace":
		if err := runTrace(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "replay":
		if err := runReplay(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "executor":
		if err := runExecutor(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "workflow":
		if err := runWorkflow(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "baseline":
		if err := runBaseline(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "template":
		if err := runTemplate(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "case":
		if err := runCase(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "interface-node":
		if err := runInterfaceNode(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "research":
		if err := runResearch(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printHelp()
		os.Exit(2)
	}
}

func parseInterspersedFlags(flags *flag.FlagSet, args []string) error {
	return flags.Parse(interspersedFlagArgs(flags, args))
}

func interspersedFlagArgs(flags *flag.FlagSet, args []string) []string {
	flagArgs := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !looksLikeFlagToken(arg) {
			positional = append(positional, arg)
			continue
		}
		flagArgs = append(flagArgs, arg)
		name, inlineValue := flagTokenName(arg)
		defined := flags.Lookup(name)
		if defined == nil || inlineValue || isBoolFlagValue(defined.Value) || i+1 >= len(args) {
			continue
		}
		i++
		flagArgs = append(flagArgs, args[i])
	}
	return append(flagArgs, positional...)
}

func looksLikeFlagToken(arg string) bool {
	return strings.HasPrefix(arg, "-") && arg != "-"
}

func flagTokenName(arg string) (string, bool) {
	name := strings.TrimLeft(arg, "-")
	if before, _, ok := strings.Cut(name, "="); ok {
		return before, true
	}
	return name, false
}

func isBoolFlagValue(value flag.Value) bool {
	boolValue, ok := value.(interface {
		IsBoolFlag() bool
	})
	return ok && boolValue.IsBoolFlag()
}

func printHelp() {
	fmt.Println(helpText())
}

func helpText() string {
	return `AgentTestBench

Usage:
  agent-testbench version
  agent-testbench commands [--filter TEXT] [--json]
  agent-testbench store config set NAME --url postgres://...
  agent-testbench store config set NAME --url mysql://...
  agent-testbench store config set NAME --url sqlite://PATH
  agent-testbench store config list [--json]
  agent-testbench store use NAME
  agent-testbench store current [--json]
  agent-testbench store status [--store NAME_OR_DSN] [--json]
  agent-testbench store provision [--store NAME_OR_DSN] [--json]
  agent-testbench store upgrade [--store NAME_OR_DSN]
  agent-testbench store ddl [--backend postgres|mysql] [--store NAME_OR_DSN]
  agent-testbench store copy --from NAME_OR_DSN --to NAME_OR_DSN [--require-environment ENV_ID] [--require-verification-workflow ID] [--require-verified-environment] [--require-min-components N] [--require-min-dependencies N] [--require-min-assets N] [--require-inline-asset-bytes N] [--json]
  agent-testbench environment register --id ID [--store NAME_OR_DSN] [--display-name NAME] [--service ID] [--repo SERVICE=PATH] [--branch SERVICE=BRANCH] [--checkout SERVICE=PATH] [--package-repo URL] [--package-branch BRANCH] [--package-ref REF] [--compose-file PATH]... [--compose-generated-file TARGET=SOURCE_FILE]... [--compose-env KEY=VALUE]... [--start-command TEXT] [--health-url URL] [--health-tcp HOST:PORT] [--health-command CMD] [--health-compose-service SERVICE] [--verification-workflow ID] [--json]
  agent-testbench environment discover [--store NAME_OR_DSN] [--all] [--json]
  agent-testbench environment inspect ENV_ID [--store NAME_OR_DSN] [--json]
  agent-testbench environment bootstrap ENV_ID [--store NAME_OR_DSN] [--json]
  agent-testbench environment repo set ENV_ID [--repo SERVICE=URL] [--branch SERVICE=BRANCH] [--repo-ref SERVICE=REF] [--checkout SERVICE=PATH] [--store NAME_OR_DSN] [--json]
  agent-testbench environment startup-file put ENV_ID --file TARGET=SOURCE_FILE [--store NAME_OR_DSN] [--json]
  agent-testbench environment components inspect ENV_ID [--store NAME_OR_DSN] [--json]
  agent-testbench environment components replace ENV_ID --file COMPONENT_GRAPH_JSON [--store NAME_OR_DSN] [--json]
  agent-testbench environment restore ENV_ID --workspace PATH [--store NAME_OR_DSN] [--execute] [--pull] [--prepare-repos-only] [--assume-clean-docker] [--use-existing-containers] [--clean-docker-state] [--clean-docker-images] [--allow-destructive-docker-cleanup] [--run-workflow --server-url URL] [--base-url URL] [--workflow-output-dir PATH] [--health-timeout-seconds N] [--json]
  agent-testbench environment acceptance start ENV_ID --server-url URL --request-id ID [--base-url URL] [--evidence-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench environment acceptance report ENV_ID --server-url URL --run ID [--json]
  agent-testbench environment verify ENV_ID --run ID --status STATUS [--evidence-complete] [--topology-complete] [--store NAME_OR_DSN] [--json]
  agent-testbench environment publish-verified ENV_ID [--store NAME_OR_DSN] [--json]
  agent-testbench sandbox start [--store NAME_OR_DSN] [--service ID] [--kind KIND] [--timeout-seconds N] [--json]
  agent-testbench sandbox service register --id ID [--store NAME_OR_DSN] [--display-name NAME] [--kind KIND] [--service-port N] [--health-url URL] [--json]
  agent-testbench sandbox interface register --id ID --service-id ID --path PATH [--store NAME_OR_DSN] [--method METHOD] [--case-id ID] [--case-title TEXT] [--required-for-admission] [--json]
  agent-testbench template-package install --from PATH [--profile-home PATH] [--force]
  agent-testbench template-package inspect --template-package PATH_OR_ID [--profile-home PATH]
  agent-testbench template-package catalog-index [--store NAME_OR_DSN] [--json]
  agent-testbench template-package verify --template-package PATH_OR_ID [--profile-home PATH] [--store NAME_OR_DSN] [--require-case-runs] [--require-workflow-runs] [--json] [--force]
  agent-testbench template-package import --from PATH_OR_ID [--profile-home PATH] [--store NAME_OR_DSN] [--json] [--audit] [--require-audit-ok] [--force]
  agent-testbench profile init --output PATH [--id ID] [--display-name NAME] [--force]
  agent-testbench profile install --from PATH [--profile-home PATH] [--force]
  agent-testbench profile pack --profile PATH_OR_ID --output PATH [--profile-home PATH] [--force]
  agent-testbench profile list [--profile-home PATH] [--json]
  agent-testbench profile inspect --profile PATH_OR_ID [--profile-home PATH]
  agent-testbench profile export --store NAME_OR_DSN --output PATH [--force] [--json]
  agent-testbench profile audit --profile PATH_OR_ID --offline-template-package [--profile-home PATH] [--store NAME_OR_DSN] [--json] [--force]
  agent-testbench profile audit-plan --profile PATH_OR_ID --offline-template-package [--profile-home PATH] [--store NAME_OR_DSN] [--json] [--force]
  agent-testbench profile doctor --profile PATH_OR_ID --case-id ID [--profile-home PATH] [--json]
  agent-testbench profile repair --from-manifest PATH [--profile PATH_OR_ID] [--profile-home PATH] [--apply] [--json]
  agent-testbench profile generation-plan openapi --from PATH [--service-id ID] [--evidence-dir PATH] [--output-dir PATH] [--json]
  agent-testbench profile import-plan openapi --from PATH [--service-id ID] [--evidence-dir PATH] [--output-dir PATH] [--json]
  agent-testbench profile import-plan http-capture --from PATH [--service-id ID] [--evidence-dir PATH] [--output-dir PATH] [--json]
  agent-testbench profile verify --profile PATH_OR_ID [--profile-home PATH] [--store NAME_OR_DSN] [--require-case-runs] [--require-workflow-runs] [--json] [--force]
  agent-testbench profile import --from PATH_OR_ID [--profile-home PATH] [--store NAME_OR_DSN] [--json] [--audit] [--require-audit-ok] [--force]
  agent-testbench config publish --from PATH_OR_ID [--profile-home PATH] [--store NAME_OR_DSN] [--json] [--audit] [--require-audit-ok] [--force]
  agent-testbench executor plan [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--json]
  agent-testbench evidence import --from PATH --profile ID [--store NAME_OR_DSN]
  agent-testbench evidence list [--store NAME_OR_DSN] [--run ID] [--json]
  agent-testbench evidence tasks [--store NAME_OR_DSN] --run ID [--step ID] [--case ID] [--kind KIND] [--status STATUS] [--json]
  agent-testbench trace topology collect --run ID [--store NAME_OR_DSN] --trace-graphql-url URL [--step ID] [--case ID] [--request ID] [--endpoint TEXT] [--trace-id ID] [--json]
  agent-testbench replay evidence --trace-id ID [--json]
  agent-testbench workflow discover [--store NAME_OR_DSN] [--filter TEXT] [--json]
  agent-testbench workflow discover --profile PATH_OR_ID --offline-template-package [--profile-home PATH] [--filter TEXT] [--json]
  agent-testbench workflow plan [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] --workflow ID [--json]
  agent-testbench workflow audit --workflow ID [--store NAME_OR_DSN] [--json]
  agent-testbench workflow audit --profile PATH --offline-template-package --workflow ID [--store NAME_OR_DSN] [--json]
  agent-testbench workflow runs [--store NAME_OR_DSN] [--json]
  agent-testbench workflow run --run ID [--store NAME_OR_DSN] [--json]
  agent-testbench workflow step --run ID --step ID [--store NAME_OR_DSN] [--json]
  agent-testbench workflow latest-step --workflow ID --step ID [--store NAME_OR_DSN] [--json]
  agent-testbench workflow gate --run ID [--store NAME_OR_DSN] [--require-passed] [--require-steps] [--require-evidence] [--json]
  agent-testbench workflow report --workflow ID [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--base-url URL] [--output-dir PATH] [--json]
  agent-testbench workflow acceptance start --server-url URL --workflow ID --request-id ID [--base-url URL] [--evidence-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench workflow acceptance report --server-url URL --run ID [--json]
  agent-testbench baseline get --profile ID --subject ID [--store NAME_OR_DSN]
  agent-testbench baseline set --profile ID --subject ID --status STATUS [--required] [--store NAME_OR_DSN]
  agent-testbench template render [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] --template ID [--fixture ID]
  agent-testbench interface-node discover [--store NAME_OR_DSN] [--filter TEXT] [--json]
  agent-testbench interface-node discover --profile PATH_OR_ID --offline-template-package [--profile-home PATH] [--filter TEXT] [--json]
  agent-testbench interface-node coverage [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--workflow ID] [--json]
  agent-testbench interface-node coverage-gaps [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--workflow ID] [--json]
  agent-testbench interface-node case audit --profile PATH --node ID [--json]
  agent-testbench interface-node case draft --profile PATH --node ID --case-id ID [--title TEXT] [--case-path PATH] [--method METHOD] [--path PATH] [--tag TAG] [--priority PRIORITY] [--owner OWNER] [--output PATH] [--json]
  agent-testbench interface-node case apply --profile PATH --file PATH [--json]
  agent-testbench interface-node case report --node ID [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--base-url URL] [--output-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench case discover [--store NAME_OR_DSN] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--json]
  agent-testbench case discover --profile PATH_OR_ID --offline-template-package [--profile-home PATH] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--json]
  agent-testbench case suite report [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--base-url URL] [--output-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench case suite coverage [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--json]
  agent-testbench case suite stability [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--limit N] [--json]
  agent-testbench case suite priority [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--signal TEXT] [--change TEXT] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--limit N] [--request-id ID] [--base-url URL] [--evidence-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench case suite brief [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--signal TEXT] [--change TEXT] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--limit N] [--stability-limit N] [--request-id ID] [--base-url URL] [--evidence-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench case suite quality [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--json]
  agent-testbench case suite quality-plan [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--json]
  agent-testbench case suite quality-report [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--output-dir PATH] [--json]
  agent-testbench case suite inspect [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--json]
  agent-testbench case suite plan [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--action ACTION] [--request-id ID] [--base-url URL] [--evidence-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench case suite impact [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--signal TEXT] [--change TEXT] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--action ACTION] [--request-id ID] [--base-url URL] [--evidence-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench case suite impact-report [--profile PATH_OR_ID] [--profile-home PATH] [--store NAME_OR_DSN] [--signal TEXT] [--change TEXT] [--filter TEXT] [--node ID] [--tag TAG] [--status STATUS] [--owner OWNER] [--priority PRIORITY] [--action ACTION] [--request-id ID] [--base-url URL] [--output-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench case runs [--store NAME_OR_DSN] [--run ID] [--json]
  agent-testbench case evidence [--store NAME_OR_DSN] [--case-run ID | --run ID [--case-id ID] [--step-id ID]] [--json]
  agent-testbench case timing [--store NAME_OR_DSN] [--kind KIND] [--max-age-minutes N] [--json]
  agent-testbench case batch start --server-url URL [--case ID]... [--node ID]... [--workflow ID] [--suite NAME] [--request-id ID] [--base-url URL] [--evidence-dir PATH] [--timeout-seconds N] [--json]
  agent-testbench case batch report --server-url URL --run ID [--json]
  agent-testbench case run --case PATH [--base-url URL] [--override KEY=VALUE] [--evidence-dir PATH] [--run-id ID] [--dry-run] [--json]
  agent-testbench case run --case-id ID [--base-url URL] [--override KEY=VALUE] [--evidence-dir PATH] [--store NAME_OR_DSN] [--run-id ID] [--json]
  agent-testbench case incomplete-batches [--profile PATH_OR_ID] [--store NAME_OR_DSN] [--json]
  agent-testbench research feature --feature TEXT --radar-index PATH [--limit N] [--require-min-matches N] [--json]
  agent-testbench research features --radar-index PATH [--filter TEXT] [--json]
  agent-testbench research search --query TEXT --radar-index PATH [--limit N] [--reference-limit N] [--min-references N] [--json]
  agent-testbench research references --feature TEXT --radar-index PATH [--limit N] [--json]
  agent-testbench research live-check --radar-index PATH [--feature TEXT] [--limit N] [--max-star-drift N] [--max-pushed-drift-hours N] [--github-api-url URL] [--json]
  agent-testbench research brief --query TEXT --radar-index PATH [--min-references N] [--require-command COMMAND] [--max-age-hours N] [--reference-limit N] [--live-check] [--max-star-drift N] [--max-pushed-drift-hours N] [--format text|json|markdown] [--json]
  agent-testbench research sync --radar-root PATH [--radar-index PATH] [--refresh-limit N] [--max-age-hours N] [--min-references N] [--execute] [--json]
  agent-testbench research coverage --radar-index PATH [--min-references N] [--limit N] [--json]
  agent-testbench research audit --radar-index PATH [--min-references N] [--json]
  agent-testbench research status --radar-index PATH [--max-age-hours N] [--json]
  agent-testbench research matrix --radar-index PATH [--filter TEXT] [--limit N] [--json]
  agent-testbench research refresh-plan --radar-index PATH [--min-references N] [--max-age-hours N] [--limit N] [--require-ready] [--json]
  agent-testbench research roadmap --radar-index PATH [--min-references N] [--limit N] [--reference-limit N] [--live-check] [--max-star-drift N] [--max-pushed-drift-hours N] [--github-api-url URL] [--json]
  agent-testbench research backlog --radar-index PATH [--min-references N] [--limit N] [--reference-limit N] [--json]
  agent-testbench research gate --feature TEXT --radar-index PATH [--require-min-matches N] [--require-command COMMAND] [--max-age-hours N] [--live-check] [--max-star-drift N] [--max-pushed-drift-hours N] [--json]
  agent-testbench research plan --feature TEXT --radar-index PATH [--limit N] [--require-min-matches N] [--live-check] [--max-star-drift N] [--max-pushed-drift-hours N] [--github-api-url URL] [--format text|json|markdown] [--json]
  agent-testbench case diagnose [--store NAME_OR_DSN] [--case-run ID | --run ID [--case-id ID] [--step-id ID]] [--json]
  agent-testbench case gate [--store NAME_OR_DSN] [--run ID] [--require-no-failures] [--require-evidence] [--min-passed N] [--json]
  agent-testbench serve [--profile PATH_OR_ID] [--profile-home PATH] [--host HOST] [--port PORT] [--store NAME_OR_DSN]
  agent-testbench help

Serve reads profile catalog data from the local Store. When --profile is set,
the external bundle is first published into the Store/read-model, then served
from that indexed view.`
}

type commandCatalogReport struct {
	OK       bool                 `json:"ok"`
	Filter   string               `json:"filter,omitempty"`
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
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable command catalog")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report := commandCatalog(*filter)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCommandCatalog(report)
	return nil
}

func commandCatalog(filter string) commandCatalogReport {
	filter = strings.TrimSpace(filter)
	report := commandCatalogReport{OK: true, Filter: filter, Commands: []commandCatalogItem{}}
	for _, usage := range commandUsageLines() {
		item := commandCatalogItemFromUsage(usage)
		if len(item.Path) == 0 {
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
	for _, line := range lines {
		usage := strings.TrimSpace(line)
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
	for _, item := range report.Commands {
		fmt.Printf("- %s [%s]\n", item.Command, item.Area)
		fmt.Printf("  %s\n", item.Usage)
	}
}

func runStore(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing store command")
	}
	switch args[0] {
	case "config":
		return runStoreConfig(args[1:])
	case "use":
		return runStoreUse(args[1:])
	case "current":
		return runStoreCurrent(args[1:])
	case "ddl":
		return runStoreDDL(args[1:])
	case "copy":
		return runStoreCopy(ctx, args[1:])
	}

	flags := flag.NewFlagSet("store "+args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resolvedStoreURL) == "" {
		return activeStoreRequiredError()
	}
	backend, _ := storeBackendFromURL(resolvedStoreURL)
	if backend == "postgres" {
		cfg, err := postgres.ParseConfigFromURL(resolvedStoreURL)
		if err != nil {
			return err
		}
		switch args[0] {
		case "status":
			status, err := postgresSchemaStatus(ctx, cfg)
			if err != nil {
				if *jsonOutput {
					if jsonErr := writeIndentedJSON(postgresStoreStatusErrorReport(cfg.URL, err)); jsonErr != nil {
						return jsonErr
					}
				}
				return err
			}
			if *jsonOutput {
				return writeIndentedJSON(postgresStoreStatusReport(status))
			}
			printPostgresStoreStatus(status)
		case "upgrade":
			status, err := postgresUpgradeSchema(ctx, cfg)
			if err != nil {
				return err
			}
			fmt.Printf("Upgraded store schema to version %d\n", status.CurrentVersion)
			fmt.Printf("Applied: %d\n", status.AppliedCount)
			fmt.Printf("URL: %s\n", maskStoreURL(status.URL))
		default:
			return fmt.Errorf("unknown store command: %s", args[0])
		}
		return nil
	}
	if backend == "mysql" {
		cfg, err := mysql.ParseConfigFromURL(resolvedStoreURL)
		if err != nil {
			return err
		}
		switch args[0] {
		case "status":
			status, err := mysqlSchemaStatus(ctx, cfg)
			if err != nil {
				if *jsonOutput {
					if jsonErr := writeIndentedJSON(mysqlStoreStatusErrorReport(cfg.URL, err)); jsonErr != nil {
						return jsonErr
					}
				}
				return err
			}
			if *jsonOutput {
				return writeIndentedJSON(mysqlStoreStatusReport(status))
			}
			printMySQLStoreStatus(status)
		case "provision":
			result, err := mysqlProvisionDatabase(ctx, cfg)
			if err != nil {
				if *jsonOutput {
					if jsonErr := writeIndentedJSON(map[string]any{
						"ok":      false,
						"backend": "mysql",
						"url":     maskStoreURL(cfg.URL),
						"error":   err.Error(),
					}); jsonErr != nil {
						return jsonErr
					}
				}
				return err
			}
			if *jsonOutput {
				return writeIndentedJSON(map[string]any{
					"ok":       true,
					"backend":  "mysql",
					"url":      maskStoreURL(result.URL),
					"database": result.Database,
					"created":  result.Created,
				})
			}
			if result.Created {
				fmt.Printf("Created MySQL store database %s\n", result.Database)
			} else {
				fmt.Printf("MySQL store database already exists: %s\n", result.Database)
			}
			fmt.Printf("URL: %s\n", maskStoreURL(result.URL))
		case "upgrade":
			status, err := mysqlUpgradeSchema(ctx, cfg)
			if err != nil {
				return err
			}
			fmt.Printf("Upgraded store schema to version %d\n", status.CurrentVersion)
			fmt.Printf("Applied: %d\n", status.AppliedCount)
			fmt.Printf("URL: %s\n", maskStoreURL(status.URL))
		default:
			return fmt.Errorf("unknown store command: %s", args[0])
		}
		return nil
	}
	cfg, err := sqlite.ParseConfigFromURL(resolvedStoreURL)
	if err != nil {
		return err
	}

	switch args[0] {
	case "status":
		status, err := sqlite.SchemaStatus(ctx, cfg)
		if err != nil {
			if *jsonOutput {
				if jsonErr := writeIndentedJSON(sqliteStoreStatusErrorReport(cfg, err)); jsonErr != nil {
					return jsonErr
				}
			}
			return err
		}
		if *jsonOutput {
			return writeIndentedJSON(sqliteStoreStatusReport(status))
		}
		printStoreStatus(status)
	case "upgrade":
		status, err := sqlite.UpgradeSchema(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Upgraded store schema to version %d\n", status.CurrentVersion)
		fmt.Printf("Applied: %d\n", status.AppliedCount)
		fmt.Printf("Path: %s\n", status.Path)
	default:
		return fmt.Errorf("unknown store command: %s", args[0])
	}
	return nil
}

func runStoreDDL(args []string) error {
	flags := flag.NewFlagSet("store ddl", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	backend := flags.String("backend", "", "Store backend")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	selectedBackend := strings.ToLower(strings.TrimSpace(*backend))
	if selectedBackend == "" {
		inferredBackend, err := inferStoreDDLBackend(*storeRef, *storeURL)
		if err != nil {
			return err
		}
		selectedBackend = inferredBackend
	}
	if selectedBackend == "" {
		selectedBackend = "postgres"
	}
	switch selectedBackend {
	case "postgres", "postgresql":
		fmt.Println(strings.Join(sqlstore.CoreSchemaSQL(sqlstore.PostgresDialect{}), "\n\n"))
		return nil
	case "mysql":
		fmt.Println(strings.Join(sqlstore.CoreSchemaSQL(sqlstore.MySQLDialect{}), "\n\n"))
		return nil
	default:
		return fmt.Errorf("unsupported DDL backend %q; supported backends: postgres, mysql", *backend)
	}
}

func inferStoreDDLBackend(storeRef string, legacyStoreURL string) (string, error) {
	storeRef = strings.TrimSpace(storeRef)
	legacyStoreURL = strings.TrimSpace(legacyStoreURL)
	if legacyStoreURL != "" {
		normalized, err := normalizeLegacyStoreURL(legacyStoreURL)
		if err != nil {
			return "", err
		}
		backend, err := storeBackendFromURL(normalized)
		if err != nil {
			return "", err
		}
		return backend, nil
	}
	if storeRef != "" {
		if backend, err := storeBackendFromURL(storeRef); err == nil && backend != "" {
			return backend, nil
		}
		cfg, err := loadStoreConfig()
		if err != nil {
			return "", err
		}
		entry, ok := cfg.Stores[storeRef]
		if !ok {
			return "", fmt.Errorf("store config %q not found", storeRef)
		}
		if strings.TrimSpace(entry.Backend) != "" {
			return strings.ToLower(strings.TrimSpace(entry.Backend)), nil
		}
		return storeBackendFromURL(entry.URL)
	}
	entry, err := activeStoreConfig()
	if err != nil {
		if errors.Is(err, errNoActiveStoreConfigured) {
			return "", nil
		}
		return "", err
	}
	if strings.TrimSpace(entry.Backend) != "" {
		return strings.ToLower(strings.TrimSpace(entry.Backend)), nil
	}
	return storeBackendFromURL(entry.URL)
}

func runEnvironment(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing environment command")
	}
	switch args[0] {
	case "register":
		return runEnvironmentRegister(ctx, args[1:])
	case "discover":
		return runEnvironmentDiscover(ctx, args[1:])
	case "inspect":
		return runEnvironmentInspect(ctx, args[1:])
	case "bootstrap":
		return runEnvironmentBootstrap(ctx, args[1:])
	case "repo":
		return runEnvironmentRepo(ctx, args[1:])
	case "startup-file":
		return runEnvironmentStartupFile(ctx, args[1:])
	case "components":
		return runEnvironmentComponents(ctx, args[1:])
	case "restore":
		return runEnvironmentRestore(ctx, args[1:])
	case "acceptance":
		return runEnvironmentAcceptance(ctx, args[1:])
	case "verify":
		return runEnvironmentVerify(ctx, args[1:])
	case "publish-verified":
		return runEnvironmentPublishVerified(ctx, args[1:])
	default:
		return fmt.Errorf("unknown environment command: %s", args[0])
	}
}

func runEnvironmentRegister(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment register", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	id := flags.String("id", "", "Environment id")
	displayName := flags.String("display-name", "", "Environment display name")
	description := flags.String("description", "", "Environment description")
	status := flags.String("status", "draft", "Environment status")
	verificationWorkflowID := flags.String("verification-workflow", "", "Verification workflow id")
	composeProjectName := flags.String("compose-project-name", "", "Docker Compose project name")
	composeSkipPull := flags.Bool("compose-skip-pull", false, "Skip Docker Compose image pull during restore")
	composeSkipBuild := flags.Bool("compose-skip-build", false, "Skip Docker Compose build during restore")
	packageRepo := flags.String("package-repo", "", "Environment package Git URL containing compose files and local validation assets")
	packageBranch := flags.String("package-branch", "", "Environment package Git branch")
	packageRef := flags.String("package-ref", "", "Environment package Git ref to checkout detached")
	startCommand := flags.String("start-command", "", "Local startup command")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var services, repos, branches, repoRefs, checkouts, healthURLs, healthTCPs, healthCommands, healthComposeServices, composeFiles, composeGeneratedFiles, composeEnvFiles, composeEnvs, composeProfiles, composeServices stringListFlag
	flags.Var(&services, "service", "Service id; repeat for multiple services")
	flags.Var(&repos, "repo", "Service repo as SERVICE=PATH_OR_URL; repeat for multiple services")
	flags.Var(&branches, "branch", "Service branch as SERVICE=BRANCH; repeat for multiple services")
	flags.Var(&repoRefs, "repo-ref", "Service Git ref as SERVICE=REF; repeat for multiple services")
	flags.Var(&checkouts, "checkout", "Service checkout path as SERVICE=PATH; repeat for multiple services")
	flags.Var(&composeFiles, "compose-file", "Local compose file path; repeat for multiple compose files")
	flags.Var(&composeGeneratedFiles, "compose-generated-file", "Store-backed generated file as TARGET=SOURCE_FILE; repeat for compose/env startup files")
	flags.Var(&composeEnvFiles, "compose-env-file", "Docker Compose env file path; repeat for multiple files")
	flags.Var(&composeEnvs, "compose-env", "Generated Docker Compose env entry as KEY=VALUE; repeat for multiple entries")
	flags.Var(&composeProfiles, "compose-profile", "Docker Compose profile; repeat for multiple profiles")
	flags.Var(&composeServices, "compose-service", "Docker Compose service to start; repeat for multiple services")
	flags.Var(&healthURLs, "health-url", "Health check URL; repeat for multiple checks")
	flags.Var(&healthTCPs, "health-tcp", "TCP health check address as HOST:PORT; repeat for multiple checks")
	flags.Var(&healthCommands, "health-command", "Shell command health check; repeat for multiple checks")
	flags.Var(&healthComposeServices, "health-compose-service", "Docker Compose service health check; repeat for multiple services")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	if strings.TrimSpace(*id) == "" {
		return errors.New("--id is required")
	}
	if strings.TrimSpace(*verificationWorkflowID) == "" {
		return errors.New("--verification-workflow is required for environment acceptance")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	composeConfig, err := environmentComposeConfig(composeFiles, composeGeneratedFiles, *startCommand, *composeProjectName, composeEnvFiles, composeEnvs, composeProfiles, composeServices, *composeSkipPull, *composeSkipBuild, *packageRepo, *packageBranch, *packageRef)
	if err != nil {
		return err
	}
	env := store.Environment{
		ID:                     strings.TrimSpace(*id),
		DisplayName:            strings.TrimSpace(*displayName),
		Description:            strings.TrimSpace(*description),
		Status:                 stringDefault(strings.TrimSpace(*status), "draft"),
		ServicesJSON:           mustCompactJSON(environmentServices(services, repos, branches, repoRefs, checkouts)),
		ReposJSON:              mustCompactJSON(environmentRepoMap(repos, branches, repoRefs, checkouts)),
		ComposeJSON:            mustCompactJSON(composeConfig),
		HealthChecksJSON:       mustCompactJSON(environmentHealthChecks(healthURLs, healthTCPs, healthCommands, healthComposeServices)),
		VerificationWorkflowID: strings.TrimSpace(*verificationWorkflowID),
		SummaryJSON:            mustCompactJSON(map[string]any{"source": "cli"}),
	}
	env, err = runtime.UpsertEnvironment(ctx, env)
	if err != nil {
		return err
	}
	return printEnvironmentCommandResult(env, *jsonOutput)
}

func runEnvironmentDiscover(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment discover", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	includeAll := flags.Bool("all", false, "Include environments that are not verified")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	items, err := runtime.ListEnvironments(ctx)
	if err != nil {
		return err
	}
	filtered := make([]store.Environment, 0, len(items))
	for _, item := range items {
		if *includeAll || item.Verified {
			filtered = append(filtered, item)
		}
	}
	payload := map[string]any{"ok": true, "count": len(filtered), "items": environmentPayloads(filtered)}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	fmt.Printf("Environments: %d\n", len(filtered))
	for _, item := range filtered {
		fmt.Printf("- %s [%s] verified=%t workflow=%s\n", item.ID, item.Status, item.Verified, item.VerificationWorkflowID)
	}
	return nil
}

func runEnvironmentInspect(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment inspect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	id := strings.TrimSpace(flags.Arg(0))
	if id == "" {
		return errors.New("environment id is required")
	}
	env, componentGraph, err := loadEnvironmentAndComponentGraphForCLI(ctx, *storeRef, *storeURL, id)
	if err != nil {
		return err
	}
	return printEnvironmentCommandResult(env, *jsonOutput, componentGraph)
}

func runEnvironmentBootstrap(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment bootstrap", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	id := strings.TrimSpace(flags.Arg(0))
	if id == "" {
		return errors.New("environment id is required")
	}
	env, componentGraph, err := loadEnvironmentAndComponentGraphForCLI(ctx, *storeRef, *storeURL, id)
	if err != nil {
		return err
	}
	bootstrapPlan := controlplane.EnvironmentBootstrapPlan(env)
	componentReadiness := environmentRestoreComponentGraphReport(env.ID, componentGraph)
	componentStartupPlan := controlplane.EnvironmentComponentStartupPlanReport(env.ID, componentGraph)
	bootstrapPlan["componentGraph"] = componentReadiness
	bootstrapPlan["componentStartupPlan"] = componentStartupPlan
	if restorePlan, ok := bootstrapPlan["restore"].(map[string]any); ok {
		restorePlan["componentGraph"] = componentReadiness
		restorePlan["componentStartupPlan"] = componentStartupPlan
	}
	payload := map[string]any{
		"ok":          true,
		"environment": environmentPayload(env),
		"plan":        bootstrapPlan,
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	fmt.Printf("Environment Bootstrap Plan: %s\n", env.ID)
	fmt.Printf("Verification Workflow: %s\n", env.VerificationWorkflowID)
	fmt.Printf("Component Restore-ready: %t\n", componentReadiness.OK)
	if len(componentReadiness.BlockingOrder) > 0 {
		fmt.Printf("Component Blocking Order: %s\n", strings.Join(componentReadiness.BlockingOrder, " -> "))
	}
	fmt.Printf("Repos: %s\n", env.ReposJSON)
	fmt.Printf("Compose: %s\n", env.ComposeJSON)
	return nil
}

func runEnvironmentRepo(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing environment repo command")
	}
	switch args[0] {
	case "set":
		return runEnvironmentRepoSet(ctx, args[1:])
	default:
		return fmt.Errorf("unknown environment repo command: %s", args[0])
	}
}

func runEnvironmentRepoSet(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment repo set", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var repos, branches, repoRefs, checkouts stringListFlag
	flags.Var(&repos, "repo", "Service repo as SERVICE=PATH_OR_URL; repeat for multiple services")
	flags.Var(&branches, "branch", "Service branch as SERVICE=BRANCH; repeat for multiple services")
	flags.Var(&repoRefs, "repo-ref", "Service Git ref as SERVICE=REF; repeat for multiple services")
	flags.Var(&checkouts, "checkout", "Service checkout path as SERVICE=PATH; repeat for multiple services")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	id := strings.TrimSpace(flags.Arg(0))
	if id == "" {
		return errors.New("environment id is required")
	}
	updates := environmentRepoUpdateMap(repos, branches, repoRefs, checkouts)
	if len(updates) == 0 {
		return errors.New("at least one --repo, --branch, --repo-ref, or --checkout update is required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	env, err := runtime.GetEnvironment(ctx, id)
	if err != nil {
		return err
	}
	repoMap := jsonObjectString(env.ReposJSON)
	for serviceID, update := range updates {
		current := jsonObjectFromAny(repoMap[serviceID])
		for key, value := range update {
			if strings.TrimSpace(value) == "" {
				delete(current, key)
				continue
			}
			current[key] = value
		}
		repoMap[serviceID] = current
	}
	env.ReposJSON = mustCompactJSON(repoMap)
	env.ServicesJSON = mustCompactJSON(environmentServicesWithRepoUpdates(jsonArrayString(env.ServicesJSON), updates))
	env.UpdatedAt = time.Now().UTC()
	env, err = runtime.UpsertEnvironment(ctx, env)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":           true,
		"environment":  environmentPayload(env),
		"updatedRepos": updates,
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	fmt.Printf("Updated Environment Repositories: %s\n", env.ID)
	for _, serviceID := range sortedMapKeys(updates) {
		fmt.Printf("- %s\n", serviceID)
	}
	return nil
}

func runEnvironmentStartupFile(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing environment startup-file command")
	}
	switch args[0] {
	case "put":
		return runEnvironmentStartupFilePut(ctx, args[1:])
	default:
		return fmt.Errorf("unknown environment startup-file command: %s", args[0])
	}
}

func runEnvironmentStartupFilePut(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment startup-file put", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var files stringListFlag
	flags.Var(&files, "file", "Generated startup file as TARGET=SOURCE_FILE; repeat for multiple files")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	id := strings.TrimSpace(flags.Arg(0))
	if id == "" {
		return errors.New("environment id is required")
	}
	if len(files.Values()) == 0 {
		return errors.New("--file TARGET=SOURCE_FILE is required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	env, err := runtime.GetEnvironment(ctx, id)
	if err != nil {
		return err
	}
	generated, err := generatedFileContentMapFromFlags(files)
	if err != nil {
		return err
	}
	compose := jsonObjectString(env.ComposeJSON)
	current := stringMapFromAny(compose["generatedFiles"])
	for path, content := range generated {
		current[path] = content
	}
	compose["generatedFiles"] = current
	env.ComposeJSON = mustCompactJSON(compose)
	env.SummaryJSON = environmentStartupFileSummaryJSON(env.SummaryJSON, generated)
	env, err = runtime.UpsertEnvironment(ctx, env)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"environment":    environmentPayload(env),
		"generatedFiles": environmentStartupFilePayload(generated),
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	fmt.Printf("Updated Environment Startup Files: %s\n", env.ID)
	for _, item := range environmentStartupFilePayload(generated) {
		fmt.Printf("- %s (%d bytes)\n", item["path"], item["bytes"])
	}
	return nil
}

func runEnvironmentComponents(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing environment components command")
	}
	switch args[0] {
	case "inspect":
		return runEnvironmentComponentsInspect(ctx, args[1:])
	case "replace":
		return runEnvironmentComponentsReplace(ctx, args[1:])
	default:
		return fmt.Errorf("unknown environment components command: %s", args[0])
	}
}

func runEnvironmentComponentsInspect(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment components inspect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	id := strings.TrimSpace(flags.Arg(0))
	if id == "" {
		return errors.New("environment id is required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := runtime.GetEnvironment(ctx, id); err != nil {
		return err
	}
	graph, err := runtime.GetEnvironmentComponentGraph(ctx, id)
	if err != nil {
		return err
	}
	return printEnvironmentComponentGraph(id, graph, *jsonOutput)
}

func runEnvironmentComponentsReplace(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment components replace", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	file := flags.String("file", "", "Component graph JSON file")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	id := strings.TrimSpace(flags.Arg(0))
	if id == "" {
		return errors.New("environment id is required")
	}
	if strings.TrimSpace(*file) == "" {
		return errors.New("--file COMPONENT_GRAPH_JSON is required")
	}
	raw, err := os.ReadFile(strings.TrimSpace(*file))
	if err != nil {
		return err
	}
	var graph store.EnvironmentComponentGraph
	if err := json.Unmarshal(raw, &graph); err != nil {
		return fmt.Errorf("decode component graph JSON: %w", err)
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := runtime.GetEnvironment(ctx, id); err != nil {
		return err
	}
	readiness := environmentRestoreComponentGraphReport(id, graph)
	if readiness.Configured && !readiness.OK {
		return fmt.Errorf("component graph restore readiness failed: %s", readiness.Error)
	}
	if err := runtime.ReplaceEnvironmentComponentGraph(ctx, id, graph); err != nil {
		return err
	}
	graph, err = runtime.GetEnvironmentComponentGraph(ctx, id)
	if err != nil {
		return err
	}
	return printEnvironmentComponentGraph(id, graph, *jsonOutput)
}

func printEnvironmentComponentGraph(envID string, graph store.EnvironmentComponentGraph, jsonOutput bool) error {
	readiness := environmentRestoreComponentGraphReport(envID, graph)
	payload := map[string]any{
		"ok":            true,
		"environmentId": envID,
		"componentGraph": map[string]any{
			"components":       graph.Components,
			"dependencies":     graph.Dependencies,
			"assets":           graph.Assets,
			"restoreReadiness": readiness,
			"counts": map[string]int{
				"components":   len(graph.Components),
				"dependencies": len(graph.Dependencies),
				"assets":       len(graph.Assets),
			},
		},
	}
	if jsonOutput {
		return writeIndentedJSON(payload)
	}
	fmt.Printf("Environment Component Graph: %s\n", envID)
	fmt.Printf("Components: %d\n", len(graph.Components))
	fmt.Printf("Dependencies: %d\n", len(graph.Dependencies))
	fmt.Printf("Assets: %d\n", len(graph.Assets))
	fmt.Printf("Restore-ready: %t\n", readiness.OK)
	if len(readiness.BlockingOrder) > 0 {
		fmt.Printf("Blocking order: %s\n", strings.Join(readiness.BlockingOrder, " -> "))
	}
	if strings.TrimSpace(readiness.Error) != "" {
		fmt.Printf("Readiness error: %s\n", readiness.Error)
	}
	for _, component := range graph.Components {
		label := strings.TrimSpace(component.DisplayName)
		if label == "" {
			label = component.ComponentID
		}
		fmt.Printf("- %s [%s/%s] compose=%s required=%t\n", component.ComponentID, component.Kind, component.Role, component.ComposeService, component.Required)
		if label != component.ComponentID {
			fmt.Printf("  name: %s\n", label)
		}
	}
	return nil
}

func environmentStartupFilePayload(files map[string]string) []map[string]any {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		out = append(out, map[string]any{
			"path":  path,
			"bytes": len(files[path]),
		})
	}
	return out
}

func environmentStartupFileSummaryJSON(existing string, files map[string]string) string {
	summary := jsonObjectString(existing)
	summary["startupFiles"] = map[string]any{
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"files":     environmentStartupFilePayload(files),
	}
	return mustCompactJSON(summary)
}

type environmentRestoreReport struct {
	OK                   bool                                         `json:"ok"`
	RestoreID            string                                       `json:"restoreId"`
	Executed             bool                                         `json:"executed"`
	EnvironmentID        string                                       `json:"environmentId"`
	VerificationWorkflow string                                       `json:"verificationWorkflow"`
	Workspace            string                                       `json:"workspace"`
	Environment          map[string]any                               `json:"environment,omitempty"`
	Error                string                                       `json:"error,omitempty"`
	Package              environmentRestorePackageReport              `json:"package,omitempty"`
	Repos                []environmentRestoreRepoReport               `json:"repos"`
	SourcePolicy         environmentRestoreSourcePolicy               `json:"sourcePolicy,omitempty"`
	ComponentGraph       environmentRestoreComponentGraph             `json:"componentGraph,omitempty"`
	ComponentStartupPlan controlplane.EnvironmentComponentStartupPlan `json:"componentStartupPlan,omitempty"`
	ComponentAssets      []environmentRestoreComponentAsset           `json:"componentAssets,omitempty"`
	Compose              map[string]any                               `json:"compose"`
	HealthChecks         []any                                        `json:"healthChecks"`
	Preflight            environmentRestorePreflight                  `json:"preflight"`
	Readiness            environmentRestoreReadiness                  `json:"readiness"`
	Docker               environmentRestoreDockerReport               `json:"docker"`
	Workflow             environmentRestoreWorkflowRun                `json:"workflow"`
	CleanMachine         environmentRestoreCleanMachinePlan           `json:"cleanMachine,omitempty"`
	NextActions          []string                                     `json:"nextActions"`
}

type environmentRestoreCleanMachinePlan struct {
	Ready          bool                                         `json:"ready"`
	Summary        environmentRestoreCleanMachineSummary        `json:"summary,omitempty"`
	PrepareCommand []string                                     `json:"prepareCommand,omitempty"`
	ExecuteCommand []string                                     `json:"executeCommand,omitempty"`
	Prerequisites  []environmentRestoreCleanMachinePrerequisite `json:"prerequisites,omitempty"`
	Notes          []string                                     `json:"notes,omitempty"`
}

type environmentRestoreCleanMachineSummary struct {
	EnvironmentID           string `json:"environmentId,omitempty"`
	VerificationWorkflow    string `json:"verificationWorkflow,omitempty"`
	Components              int    `json:"components"`
	StartupBatches          int    `json:"startupBatches"`
	HealthGates             int    `json:"healthGates"`
	ServiceRepositories     int    `json:"serviceRepositories"`
	StartupAssets           int    `json:"startupAssets"`
	RemoteComponentAssets   int    `json:"remoteComponentAssets"`
	InlineAssetBytes        int64  `json:"inlineAssetBytes"`
	RemoteAssetBytes        int64  `json:"remoteAssetBytes"`
	GraphMetadataLimitBytes int    `json:"graphMetadataLimitBytes"`
	InlineAssetLimitBytes   int    `json:"inlineAssetLimitBytes"`
	DockerImagesStored      bool   `json:"dockerImagesStored"`
	LargeBinariesStored     bool   `json:"largeBinariesStored"`
}

type environmentRestoreCleanMachinePrerequisite struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail,omitempty"`
}

type environmentRestoreSourcePolicy struct {
	RemoteOnly bool     `json:"remoteOnly"`
	OK         bool     `json:"ok"`
	Violations []string `json:"violations,omitempty"`
}

type environmentRestoreComponentGraph = controlplane.EnvironmentComponentGraphReadiness

type environmentRestoreComponentAsset struct {
	AssetID          string   `json:"assetId"`
	OwnerComponentID string   `json:"ownerComponentId,omitempty"`
	SourceURL        string   `json:"sourceUrl,omitempty"`
	SourcePath       string   `json:"sourcePath,omitempty"`
	Checkout         string   `json:"checkout,omitempty"`
	TargetPath       string   `json:"targetPath"`
	Bytes            int64    `json:"bytes,omitempty"`
	ApplyOrder       int      `json:"applyOrder,omitempty"`
	Action           string   `json:"action"`
	RepoAction       string   `json:"repoAction,omitempty"`
	Command          []string `json:"command,omitempty"`
	OK               bool     `json:"ok"`
	Error            string   `json:"error,omitempty"`
}

type environmentRestorePackageReport struct {
	Configured bool     `json:"configured"`
	URL        string   `json:"url,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	Ref        string   `json:"ref,omitempty"`
	Checkout   string   `json:"checkout,omitempty"`
	Exists     bool     `json:"exists"`
	Action     string   `json:"action"`
	Command    []string `json:"command,omitempty"`
	OK         bool     `json:"ok"`
	Output     string   `json:"output,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type environmentRestorePackageSpec struct {
	URL      string
	Branch   string
	Ref      string
	Checkout string
}

type environmentRestoreRepoReport struct {
	ServiceID string   `json:"serviceId"`
	URL       string   `json:"url,omitempty"`
	Branch    string   `json:"branch,omitempty"`
	Ref       string   `json:"ref,omitempty"`
	Checkout  string   `json:"checkout"`
	Exists    bool     `json:"exists"`
	Action    string   `json:"action"`
	Command   []string `json:"command,omitempty"`
	OK        bool     `json:"ok"`
	Output    string   `json:"output,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type environmentRestoreRepoSpec struct {
	ServiceID string
	URL       string
	Branch    string
	Ref       string
	Checkout  string
}

type environmentRestorePreflight struct {
	OK                 bool                              `json:"ok"`
	AssumeCleanDocker  bool                              `json:"assumeCleanDocker,omitempty"`
	Tools              []environmentRestorePreflightTool `json:"tools"`
	HeavySteps         []string                          `json:"heavySteps,omitempty"`
	ContainerConflicts []string                          `json:"containerConflicts,omitempty"`
	StartupAssets      []environmentRestoreStartupAsset  `json:"startupAssets,omitempty"`
	Notes              []string                          `json:"notes,omitempty"`
}

type environmentRestoreStartupAsset struct {
	Path        string `json:"path"`
	Source      string `json:"source,omitempty"`
	ComposeFile string `json:"composeFile,omitempty"`
	Kind        string `json:"kind"`
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
}

type environmentRestoreStartupAssetCandidate struct {
	path        string
	source      string
	composeFile string
	kind        string
}

type environmentRestorePreflightTool struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	OK       bool   `json:"ok"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
}

type environmentRestoreReadiness struct {
	OK                         bool                              `json:"ok"`
	Action                     string                            `json:"action"`
	PauseBeforeHeavyValidation bool                              `json:"pauseBeforeHeavyValidation"`
	NextStep                   string                            `json:"nextStep"`
	Items                      []environmentRestoreReadinessItem `json:"items"`
}

type environmentRestoreReadinessItem struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail,omitempty"`
}

type environmentRestoreDockerReport struct {
	OK            bool                                  `json:"ok"`
	Action        string                                `json:"action"`
	ComposeFile   string                                `json:"composeFile,omitempty"`
	Workdir       string                                `json:"workdir,omitempty"`
	Generated     []environmentRestoreGeneratedFile     `json:"generatedFiles,omitempty"`
	AppliedAssets []environmentRestoreAppliedAsset      `json:"appliedAssets,omitempty"`
	Cleanup       environmentRestoreDockerCleanupReport `json:"cleanup,omitempty"`
	Commands      [][]string                            `json:"commands,omitempty"`
	Output        []string                              `json:"output,omitempty"`
	Error         string                                `json:"error,omitempty"`
	HealthChecks  []environmentRestoreHealthCheckReport `json:"healthChecks,omitempty"`
}

type environmentRestoreGeneratedFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	Action string `json:"action"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

type environmentRestoreAppliedAsset struct {
	AssetID              string   `json:"assetId"`
	OwnerComponentID     string   `json:"ownerComponentId,omitempty"`
	TargetComponentID    string   `json:"targetComponentId,omitempty"`
	TargetComposeService string   `json:"targetComposeService,omitempty"`
	DependencyConsumer   string   `json:"dependencyConsumer,omitempty"`
	DependencyProvider   string   `json:"dependencyProvider,omitempty"`
	TargetPath           string   `json:"targetPath,omitempty"`
	Bytes                int      `json:"bytes,omitempty"`
	ApplyOrder           int      `json:"applyOrder,omitempty"`
	Action               string   `json:"action"`
	Command              []string `json:"command,omitempty"`
	Attempts             int      `json:"attempts,omitempty"`
	OK                   bool     `json:"ok"`
	Error                string   `json:"error,omitempty"`
}

type environmentRestoreDockerCleanupReport struct {
	Requested      bool       `json:"requested,omitempty"`
	Allowed        bool       `json:"allowed,omitempty"`
	IncludeImages  bool       `json:"includeImages,omitempty"`
	Action         string     `json:"action,omitempty"`
	BackupCommands [][]string `json:"backupCommands,omitempty"`
	Commands       [][]string `json:"commands,omitempty"`
	Output         []string   `json:"output,omitempty"`
	Error          string     `json:"error,omitempty"`
	Warning        string     `json:"warning,omitempty"`
}

type environmentRestoreHealthCheckReport struct {
	ID         string `json:"id,omitempty"`
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	Address    string `json:"address,omitempty"`
	Command    string `json:"command,omitempty"`
	Service    string `json:"service,omitempty"`
	Container  string `json:"container,omitempty"`
	OK         bool   `json:"ok"`
	StatusCode int    `json:"statusCode,omitempty"`
	State      string `json:"state,omitempty"`
	Health     string `json:"health,omitempty"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
}

type environmentRestoreWorkflowRun struct {
	OK         bool                                 `json:"ok"`
	Action     string                               `json:"action"`
	WorkflowID string                               `json:"workflowId"`
	RunID      string                               `json:"runId,omitempty"`
	OutputDir  string                               `json:"outputDir,omitempty"`
	ReportURL  string                               `json:"reportUrl,omitempty"`
	Counts     workflowCaseReportCounts             `json:"counts,omitempty"`
	Acceptance environmentRestoreWorkflowAcceptance `json:"acceptance,omitempty"`
	Error      string                               `json:"error,omitempty"`
}

type environmentRestoreWorkflowAcceptance struct {
	OK               bool   `json:"ok"`
	TemplateID       string `json:"templateId,omitempty"`
	WorkflowID       string `json:"workflowId,omitempty"`
	ExpectedSteps    int    `json:"expectedSteps,omitempty"`
	CompletedSteps   int    `json:"completedSteps,omitempty"`
	PassedSteps      int    `json:"passedSteps,omitempty"`
	FailedSteps      int    `json:"failedSteps,omitempty"`
	TopologyProvider string `json:"topologyProvider,omitempty"`
}

type environmentRestoreWorkflowOptions struct {
	Run            bool
	EnvironmentID  string
	StoreRef       string
	StoreURL       string
	ServerURL      string
	BaseURL        string
	OutputDir      string
	TimeoutSeconds int
}

type environmentRestoreDockerCleanupOptions struct {
	Requested             bool
	IncludeImages         bool
	Allowed               bool
	UseExistingContainers bool
	AssumeCleanDocker     bool
}

func runEnvironmentRestore(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment restore", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	workspace := flags.String("workspace", "", "Local workspace for cloned or existing service checkouts")
	execute := flags.Bool("execute", false, "Clone or update component repositories, run Docker Compose, and wait for health checks")
	pull := flags.Bool("pull", false, "Run git pull --ff-only for existing checkouts when --execute is set")
	prepareReposOnly := flags.Bool("prepare-repos-only", false, "When --execute is set, clone or validate repositories and stop before Docker startup")
	runWorkflow := flags.Bool("run-workflow", false, "Run the environment verification workflow after Docker health checks pass")
	serverURL := flags.String("server-url", "", "Running control plane base URL for async environment acceptance")
	baseURL := flags.String("base-url", "", "Base URL for verification workflow execution")
	workflowOutputDir := flags.String("workflow-output-dir", "", "Verification workflow report output directory")
	acceptanceTimeoutSeconds := flags.Int("acceptance-timeout-seconds", 120, "Seconds to wait for async environment acceptance report")
	healthTimeoutSeconds := flags.Int("health-timeout-seconds", 60, "Seconds to wait for recorded Docker service health checks")
	useExistingContainers := flags.Bool("use-existing-containers", false, "Adopt already-running fixed-name Docker containers instead of running Docker Compose up")
	assumeCleanDocker := flags.Bool("assume-clean-docker", false, "Dry-run as a colleague/new machine with no existing target Docker containers")
	cleanDockerState := flags.Bool("clean-docker-state", false, "Plan or run Docker Compose cleanup before startup")
	cleanDockerImages := flags.Bool("clean-docker-images", false, "Include Docker Compose image removal in cleanup plan")
	allowDestructiveDockerCleanup := flags.Bool("allow-destructive-docker-cleanup", false, "Allow --execute to run requested Docker cleanup commands")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	id := strings.TrimSpace(flags.Arg(0))
	if id == "" {
		return errors.New("environment id is required")
	}
	if strings.TrimSpace(*workspace) == "" {
		return errors.New("--workspace is required")
	}
	if *healthTimeoutSeconds <= 0 {
		return errors.New("--health-timeout-seconds must be positive")
	}
	if *runWorkflow && !*execute {
		return errors.New("--run-workflow requires --execute")
	}
	if *prepareReposOnly && !*execute {
		return errors.New("--prepare-repos-only requires --execute")
	}
	if *prepareReposOnly && *runWorkflow {
		return errors.New("--prepare-repos-only cannot be combined with --run-workflow")
	}
	if *useExistingContainers && (*cleanDockerState || *cleanDockerImages) {
		return errors.New("--use-existing-containers cannot be combined with Docker cleanup flags")
	}
	if *assumeCleanDocker && *execute {
		return errors.New("--assume-clean-docker is a dry-run planning mode and cannot be combined with --execute")
	}
	if *assumeCleanDocker && (*useExistingContainers || *cleanDockerState || *cleanDockerImages) {
		return errors.New("--assume-clean-docker cannot be combined with Docker adoption or cleanup flags")
	}
	if *runWorkflow && strings.TrimSpace(*serverURL) == "" {
		return errors.New("--run-workflow requires --server-url for async environment acceptance")
	}
	if *acceptanceTimeoutSeconds <= 0 {
		return errors.New("--acceptance-timeout-seconds must be positive")
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer func() { _ = runtime.Close() }()
	env, err := runtime.GetEnvironment(ctx, id)
	if err != nil {
		return err
	}
	componentGraph, err := runtime.GetEnvironmentComponentGraph(ctx, env.ID)
	if err != nil {
		return err
	}
	report, err := buildEnvironmentRestoreReport(ctx, env, *workspace, *execute, *pull, *prepareReposOnly, time.Duration(*healthTimeoutSeconds)*time.Second, environmentRestoreWorkflowOptions{
		Run:            *runWorkflow,
		EnvironmentID:  env.ID,
		StoreRef:       *storeRef,
		StoreURL:       resolvedStoreURL,
		ServerURL:      *serverURL,
		BaseURL:        *baseURL,
		OutputDir:      *workflowOutputDir,
		TimeoutSeconds: *acceptanceTimeoutSeconds,
	}, environmentRestoreDockerCleanupOptions{
		Requested:             *cleanDockerState || *cleanDockerImages,
		IncludeImages:         *cleanDockerImages,
		Allowed:               *allowDestructiveDockerCleanup,
		UseExistingContainers: *useExistingContainers,
		AssumeCleanDocker:     *assumeCleanDocker,
	}, componentGraph)
	if err != nil {
		return err
	}
	if *jsonOutput {
		if encodeErr := writeIndentedJSON(report); encodeErr != nil {
			return encodeErr
		}
	} else {
		printEnvironmentRestoreReport(report)
	}
	if !report.OK {
		return errors.New("environment restore did not complete")
	}
	return nil
}

func runEnvironmentAcceptance(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing environment acceptance command")
	}
	switch args[0] {
	case "start":
		return runEnvironmentAcceptanceStart(ctx, args[1:])
	case "report":
		return runEnvironmentAcceptanceReport(ctx, args[1:])
	default:
		return fmt.Errorf("unknown environment acceptance command: %s", args[0])
	}
}

func runEnvironmentAcceptanceStart(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment acceptance start", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	serverURL := flags.String("server-url", "", "Running control plane base URL")
	requestID := flags.String("request-id", "", "Acceptance request id")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	evidenceDir := flags.String("evidence-dir", "", "Evidence output directory")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Per-step timeout in seconds")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	envID := strings.TrimSpace(flags.Arg(0))
	if envID == "" || strings.TrimSpace(*serverURL) == "" || strings.TrimSpace(*requestID) == "" {
		return errors.New("environment id, --server-url, and --request-id are required")
	}
	payload := map[string]any{"requestId": strings.TrimSpace(*requestID)}
	if strings.TrimSpace(*baseURL) != "" {
		payload["baseUrl"] = strings.TrimSpace(*baseURL)
	}
	if strings.TrimSpace(*evidenceDir) != "" {
		payload["evidenceDir"] = strings.TrimSpace(*evidenceDir)
	}
	if *timeoutSeconds > 0 {
		payload["timeoutSeconds"] = *timeoutSeconds
	}
	result, err := postWorkflowAcceptanceJSON(ctx, environmentAcceptanceRunURL(*serverURL, envID, ""), payload)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(result)
	}
	printEnvironmentAcceptanceStart(result)
	return nil
}

func runEnvironmentAcceptanceReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment acceptance report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	serverURL := flags.String("server-url", "", "Running control plane base URL")
	runID := flags.String("run", "", "Acceptance batch run id")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	envID := strings.TrimSpace(flags.Arg(0))
	if envID == "" || strings.TrimSpace(*serverURL) == "" || strings.TrimSpace(*runID) == "" {
		return errors.New("environment id, --server-url, and --run are required")
	}
	result, err := fetchWorkflowAcceptanceJSON(ctx, environmentAcceptanceRunURL(*serverURL, envID, *runID))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(result)
	}
	printEnvironmentAcceptanceReport(result)
	return nil
}

func environmentAcceptanceRunURL(serverURL string, envID string, runID string) string {
	base := strings.TrimRight(strings.TrimSpace(serverURL), "/") + "/api/environments/" + url.PathEscape(strings.TrimSpace(envID)) + "/acceptance-runs"
	if strings.TrimSpace(runID) != "" {
		base += "/" + url.PathEscape(strings.TrimSpace(runID))
	}
	return base
}

func buildEnvironmentRestoreReport(ctx context.Context, env store.Environment, workspace string, execute bool, pull bool, prepareReposOnly bool, healthTimeout time.Duration, workflowOptions environmentRestoreWorkflowOptions, cleanupOptions environmentRestoreDockerCleanupOptions, componentGraphs ...store.EnvironmentComponentGraph) (environmentRestoreReport, error) {
	workflowID := strings.TrimSpace(env.VerificationWorkflowID)
	if workflowID == "" {
		return environmentRestoreReport{}, fmt.Errorf("environment %s has no verification workflow; restore must be anchored to a verified workflow", env.ID)
	}
	workspace, err := filepath.Abs(strings.TrimSpace(workspace))
	if err != nil {
		return environmentRestoreReport{}, err
	}
	specs := environmentRestoreRepoSpecs(env, workspace)
	compose := jsonObjectString(env.ComposeJSON)
	componentGraph := store.EnvironmentComponentGraph{}
	if len(componentGraphs) > 0 {
		componentGraph = componentGraphs[0]
	}
	compose = environmentRestoreComposeWithComponentAssets(env.ID, compose, componentGraph)
	packageSpec := environmentRestorePackageSpecFromCompose(compose, workspace)
	healthChecks := environmentRestoreEffectiveHealthChecks(jsonArrayString(env.HealthChecksJSON), compose, componentGraph)
	componentGraphReport := environmentRestoreComponentGraphReport(env.ID, componentGraph)
	componentStartupPlan := controlplane.EnvironmentComponentStartupPlanReport(env.ID, componentGraph)
	attemptedAt := time.Now().UTC()
	remoteOnly := environmentRestoreRequiresRemoteSources(workflowOptions.StoreURL)
	report := environmentRestoreReport{
		OK:                   true,
		RestoreID:            "restore." + safeReportID(env.ID) + "." + attemptedAt.Format("20060102T150405.000000000Z"),
		Executed:             execute,
		EnvironmentID:        env.ID,
		VerificationWorkflow: workflowID,
		Workspace:            workspace,
		Compose:              compose,
		HealthChecks:         healthChecks,
		ComponentGraph:       componentGraphReport,
		ComponentStartupPlan: componentStartupPlan,
		Preflight:            environmentRestorePreflightReport(packageSpec, specs, compose, workspace, cleanupOptions, prepareReposOnly),
		SourcePolicy:         environmentRestoreSourcePolicyReport(packageSpec, specs, remoteOnly),
		Workflow: environmentRestoreWorkflowRun{
			OK:         !workflowOptions.Run,
			Action:     "not-requested",
			WorkflowID: workflowID,
		},
		NextActions: []string{
			"run verification workflow " + workflowID,
		},
	}
	if !report.Preflight.OK {
		report.OK = false
	}
	if !report.SourcePolicy.OK {
		report.OK = false
	}
	if report.ComponentGraph.Configured && !report.ComponentGraph.OK {
		report.OK = false
	}
	report.Package = environmentRestorePackage(ctx, packageSpec, execute, pull, remoteOnly)
	if !report.Package.OK {
		report.OK = false
	}
	for _, spec := range specs {
		item := environmentRestoreRepo(ctx, spec, execute, pull)
		if !item.OK {
			report.OK = false
		}
		report.Repos = append(report.Repos, item)
	}
	report.ComponentAssets = environmentRestoreRemoteComponentAssets(ctx, env.ID, componentGraph, workspace, execute, pull)
	for _, item := range report.ComponentAssets {
		if !item.OK {
			report.OK = false
		}
	}
	if report.OK && prepareReposOnly {
		report.Docker = environmentRestoreDockerReport{
			OK:        true,
			Action:    "skipped-after-repository-preparation",
			Workdir:   workspace,
			Generated: prepareEnvironmentRestoreGeneratedFiles(compose, workspace, execute),
		}
		for _, item := range report.Docker.Generated {
			if !item.OK {
				report.OK = false
				report.Docker.OK = false
				report.Docker.Action = "prepare-generated-files"
				report.Docker.Error = item.Error
				break
			}
		}
	} else if report.OK && cleanupOptions.UseExistingContainers {
		report.Docker = environmentRestoreUseExistingContainers(ctx, env.ID, componentGraph, compose, healthChecks, workspace, execute, healthTimeout)
		if !report.Docker.OK {
			report.OK = false
		}
	} else if report.OK {
		report.Docker = environmentRestoreDocker(ctx, env.ID, componentGraph, compose, healthChecks, workspace, execute, healthTimeout, cleanupOptions)
		if !report.Docker.OK {
			report.OK = false
		}
	} else if !report.Preflight.OK {
		report.Docker = environmentRestoreDockerReport{
			OK:      false,
			Action:  "skipped-due-to-preflight",
			Workdir: workspace,
			Error:   "restore preflight did not pass",
		}
	} else if !report.SourcePolicy.OK {
		report.Docker = environmentRestoreDockerReport{
			OK:      false,
			Action:  "skipped-due-to-source-policy",
			Workdir: workspace,
			Error:   "remote Git source policy did not pass",
		}
	} else {
		report.Docker = environmentRestoreDockerReport{
			OK:      false,
			Action:  "skipped-due-to-repository-error",
			Workdir: workspace,
			Error:   "repository preparation did not complete",
		}
	}
	if report.OK && workflowOptions.Run {
		report.Workflow = environmentRestoreRunWorkflow(ctx, workflowID, workspace, workflowOptions)
		if !report.Workflow.OK {
			report.OK = false
		}
		if report.Workflow.RunID != "" {
			env.LastVerificationRunID = report.Workflow.RunID
			env.LastVerificationStatus = statusText(report.Workflow.OK)
			env.EvidenceComplete = report.Workflow.OK && report.Workflow.Acceptance.OK
			env.TopologyComplete = report.Workflow.OK && report.Workflow.Acceptance.OK
			env.Verified = false
			env.Status = "verification-recorded"
		}
	}
	if !execute {
		nextAction := "review the Docker Compose plan, then rerun with --execute"
		if cleanupOptions.AssumeCleanDocker {
			nextAction = "run this environment on the colleague machine with --execute after reviewing the clean-machine Docker plan"
		}
		report.NextActions = append([]string{nextAction}, report.NextActions...)
	}
	report.Readiness = environmentRestoreReadinessReport(report, packageSpec, specs, cleanupOptions)
	if !report.Readiness.OK {
		report.OK = false
		if strings.TrimSpace(report.Error) == "" {
			report.Error = "restore readiness did not pass"
		}
	}
	report.CleanMachine = environmentRestoreCleanMachinePlanForReport(report, workflowOptions, cleanupOptions)
	if strings.TrimSpace(workflowOptions.StoreURL) != "" {
		persisted, err := environmentRestorePersistEnvironment(ctx, workflowOptions.StoreURL, env, report, attemptedAt)
		if err != nil {
			report.OK = false
			report.Error = err.Error()
			if report.Workflow.Action == "run-verification-workflow" {
				report.Workflow.OK = false
				report.Workflow.Error = err.Error()
			}
			report.Readiness = environmentRestoreReadinessReport(report, packageSpec, specs, cleanupOptions)
		} else {
			report.Environment = environmentPayload(persisted)
		}
	}
	return report, nil
}

func environmentRestoreCleanMachinePlanForReport(report environmentRestoreReport, workflowOptions environmentRestoreWorkflowOptions, cleanupOptions environmentRestoreDockerCleanupOptions) environmentRestoreCleanMachinePlan {
	if !cleanupOptions.AssumeCleanDocker {
		return environmentRestoreCleanMachinePlan{}
	}
	storeRef := strings.TrimSpace(workflowOptions.StoreRef)
	if storeRef == "" {
		storeRef = "STORE_NAME_OR_SQL_DSN"
	}
	plan := environmentRestoreCleanMachinePlan{
		Ready: report.OK,
		Summary: environmentRestoreCleanMachineSummary{
			EnvironmentID:           report.EnvironmentID,
			VerificationWorkflow:    report.VerificationWorkflow,
			Components:              report.ComponentGraph.Components,
			StartupBatches:          len(report.ComponentStartupPlan.Batches),
			HealthGates:             len(report.ComponentStartupPlan.HealthGates),
			ServiceRepositories:     len(report.Repos),
			StartupAssets:           len(report.Preflight.StartupAssets),
			RemoteComponentAssets:   report.ComponentGraph.RemoteAssets,
			InlineAssetBytes:        report.ComponentGraph.InlineAssetBytes,
			RemoteAssetBytes:        report.ComponentGraph.RemoteAssetBytes,
			GraphMetadataLimitBytes: store.ComponentGraphMaxBytes,
			InlineAssetLimitBytes:   store.ComponentAssetInlineMaxBytes,
			DockerImagesStored:      false,
			LargeBinariesStored:     false,
		},
		PrepareCommand: []string{
			"agent-testbench",
			"environment",
			"restore",
			report.EnvironmentID,
			"--store",
			storeRef,
			"--workspace",
			report.Workspace,
			"--execute",
			"--prepare-repos-only",
			"--json",
		},
		ExecuteCommand: []string{
			"agent-testbench",
			"environment",
			"restore",
			report.EnvironmentID,
			"--store",
			storeRef,
			"--workspace",
			report.Workspace,
			"--execute",
			"--json",
		},
		Prerequisites: environmentRestoreCleanMachinePrerequisites(report, workflowOptions),
		Notes: []string{
			"Run prepareCommand on the colleague/new machine first to clone or validate repositories and write Store-generated startup files without starting Docker.",
			"Run executeCommand after prepareCommand passes to start Docker and wait for health gates.",
			"The dry-run assumption is not included in the execute command; Docker will be checked on the target machine before startup.",
			"Add --run-workflow --server-url URL after Docker health passes when the control plane is running for acceptance verification.",
		},
	}
	if !report.Readiness.OK {
		plan.Ready = false
	}
	return plan
}

func environmentRestoreCleanMachinePrerequisites(report environmentRestoreReport, workflowOptions environmentRestoreWorkflowOptions) []environmentRestoreCleanMachinePrerequisite {
	out := []environmentRestoreCleanMachinePrerequisite{
		{
			Name:     "sql-store",
			Required: true,
			OK:       environmentRestoreRequiresRemoteSources(workflowOptions.StoreURL),
			Detail:   "configure the named SQL Store before running restore; the Store must stay outside the target Docker environment",
		},
	}
	for _, tool := range report.Preflight.Tools {
		detail := "required on the colleague machine"
		if tool.Path != "" {
			detail += "; current dry-run found " + tool.Path
		}
		if tool.Error != "" {
			detail = tool.Error
		}
		out = append(out, environmentRestoreCleanMachinePrerequisite{
			Name:     "tool:" + tool.Name,
			Required: tool.Required,
			OK:       tool.OK,
			Detail:   detail,
		})
	}
	for _, name := range []string{
		"component-graph",
		"component-startup-plan",
		"remote-git-sources",
		"store-startup-files",
		"startup-assets",
		"service-repositories",
		"docker-start-plan",
		"health-probes",
	} {
		if item, ok := environmentRestoreReadinessItemByName(report.Readiness.Items, name); ok {
			out = append(out, environmentRestoreCleanMachinePrerequisite{
				Name:     name,
				Required: item.Required,
				OK:       item.OK,
				Detail:   item.Detail,
			})
		}
	}
	return out
}

func environmentRestoreReadinessItemByName(items []environmentRestoreReadinessItem, name string) (environmentRestoreReadinessItem, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return environmentRestoreReadinessItem{}, false
}

func environmentRestorePersistEnvironment(ctx context.Context, storeURL string, env store.Environment, report environmentRestoreReport, attemptedAt time.Time) (store.Environment, error) {
	env.SummaryJSON = environmentRestoreSummaryJSON(env.SummaryJSON, report, attemptedAt)
	env.UpdatedAt = time.Now().UTC()
	runtime, err := openStore(ctx, storeURL)
	if err != nil {
		return env, err
	}
	defer func() { _ = runtime.Close() }()
	return runtime.UpsertEnvironment(ctx, env)
}

func environmentRestoreSummaryJSON(existing string, report environmentRestoreReport, attemptedAt time.Time) string {
	summary := jsonObjectString(existing)
	finishedAt := time.Now().UTC()
	lastRestore := map[string]any{
		"id":                   report.RestoreID,
		"attemptedAt":          attemptedAt.Format(time.RFC3339Nano),
		"finishedAt":           finishedAt.Format(time.RFC3339Nano),
		"durationMs":           maxInt64(0, finishedAt.Sub(attemptedAt).Milliseconds()),
		"ok":                   report.OK,
		"executed":             report.Executed,
		"phase":                environmentRestorePhase(report),
		"environmentId":        report.EnvironmentID,
		"verificationWorkflow": report.VerificationWorkflow,
		"workspace":            report.Workspace,
		"preflight": map[string]any{
			"ok":                 report.Preflight.OK,
			"tools":              environmentRestoreSummaryTools(report.Preflight.Tools),
			"heavySteps":         report.Preflight.HeavySteps,
			"containerConflicts": report.Preflight.ContainerConflicts,
			"startupAssets":      environmentRestoreSummaryStartupAssets(report.Preflight.StartupAssets),
		},
		"package":      environmentRestoreSummaryPackage(report.Package),
		"sourcePolicy": report.SourcePolicy,
		"repositories": environmentRestoreSummaryRepos(report.Repos),
		"readiness":    environmentRestoreSummaryReadiness(report.Readiness),
		"docker":       environmentRestoreSummaryDocker(report.Docker),
		"workflow": map[string]any{
			"action":     report.Workflow.Action,
			"ok":         report.Workflow.OK,
			"workflowId": report.Workflow.WorkflowID,
			"runId":      report.Workflow.RunID,
			"outputDir":  report.Workflow.OutputDir,
			"reportUrl":  report.Workflow.ReportURL,
			"counts":     report.Workflow.Counts,
			"acceptance": report.Workflow.Acceptance,
			"error":      report.Workflow.Error,
		},
		"environmentMutation": map[string]any{
			"lastVerificationRunId":  report.Workflow.RunID,
			"lastVerificationStatus": statusText(report.Workflow.OK),
			"evidenceComplete":       report.Workflow.Action == "run-acceptance-workflow" && report.Workflow.OK && report.Workflow.Acceptance.OK,
			"topologyComplete":       report.Workflow.Action == "run-acceptance-workflow" && report.Workflow.OK && report.Workflow.Acceptance.OK,
			"verified":               false,
		},
		"nextActions": report.NextActions,
	}
	if strings.TrimSpace(report.Error) != "" {
		lastRestore["error"] = report.Error
	}
	summary["lastRestore"] = lastRestore
	attempts := appendRestoreAttemptSummary(summary["restoreAttempts"], lastRestore)
	summary["restoreAttempts"] = attempts
	raw := mustCompactJSON(summary)
	for len(raw) > store.EnvironmentSummaryMaxBytes && len(attempts) > 1 {
		attempts = attempts[1:]
		summary["restoreAttempts"] = attempts
		raw = mustCompactJSON(summary)
	}
	if len(raw) > store.EnvironmentSummaryMaxBytes {
		summary["restoreAttempts"] = []any{}
		raw = mustCompactJSON(summary)
	}
	return raw
}

func appendRestoreAttemptSummary(existing any, attempt map[string]any) []any {
	out := []any{}
	if values, ok := existing.([]any); ok {
		for _, value := range values {
			out = append(out, compactRestoreAttemptSummary(mapFromReportAny(value)))
		}
	}
	out = append(out, compactRestoreAttemptSummary(attempt))
	if len(out) > environmentRestoreAttemptLimit {
		out = out[len(out)-environmentRestoreAttemptLimit:]
	}
	return out
}

func compactRestoreAttemptSummary(attempt map[string]any) map[string]any {
	preflight := mapFromReportAny(attempt["preflight"])
	sourcePolicy := mapFromReportAny(attempt["sourcePolicy"])
	readiness := mapFromReportAny(attempt["readiness"])
	docker := mapFromReportAny(attempt["docker"])
	workflow := mapFromReportAny(attempt["workflow"])
	out := map[string]any{
		"id":          valueString(attempt["id"]),
		"attemptedAt": valueString(attempt["attemptedAt"]),
		"finishedAt":  valueString(attempt["finishedAt"]),
		"durationMs":  intFromReportAny(attempt["durationMs"]),
		"ok":          boolFromReportAny(attempt["ok"]),
		"executed":    boolFromReportAny(attempt["executed"]),
		"phase":       valueString(attempt["phase"]),
		"preflight": map[string]any{
			"ok": boolFromReportAny(preflight["ok"]),
		},
		"sourcePolicy": map[string]any{
			"ok":         boolFromReportAny(sourcePolicy["ok"]),
			"remoteOnly": boolFromReportAny(sourcePolicy["remoteOnly"]),
		},
		"readiness": map[string]any{
			"ok":          boolFromReportAny(readiness["ok"]),
			"action":      valueString(readiness["action"]),
			"failedItems": listFromReportAny(readiness["failedItems"]),
		},
		"docker": map[string]any{
			"ok":           boolFromReportAny(docker["ok"]),
			"action":       valueString(docker["action"]),
			"commandCount": intFromReportAny(docker["commandCount"]),
		},
		"workflow": map[string]any{
			"ok":     boolFromReportAny(workflow["ok"]),
			"action": valueString(workflow["action"]),
			"runId":  valueString(workflow["runId"]),
		},
	}
	if environmentID := valueString(attempt["environmentId"]); environmentID != "" {
		out["environmentId"] = environmentID
	}
	if errText := valueString(attempt["error"]); errText != "" {
		out["error"] = truncateReportText(errText, 500)
	}
	return out
}

func environmentRestorePhase(report environmentRestoreReport) string {
	if report.OK {
		return "completed"
	}
	if !report.Preflight.OK {
		return "preflight"
	}
	if report.Package.Configured && !report.Package.OK {
		return "package"
	}
	for _, item := range report.Repos {
		if !item.OK {
			return "repository"
		}
	}
	if !report.Docker.OK {
		for _, item := range report.Docker.HealthChecks {
			if !item.OK {
				return "health-check"
			}
		}
		return "docker"
	}
	if !report.Readiness.OK {
		return "readiness"
	}
	if report.Workflow.Action == "run-verification-workflow" && !report.Workflow.OK {
		return "workflow"
	}
	if strings.TrimSpace(report.Error) != "" {
		return "persist"
	}
	return "completed"
}

func environmentRestoreSummaryTools(tools []environmentRestorePreflightTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, item := range tools {
		out = append(out, map[string]any{
			"name":     item.Name,
			"required": item.Required,
			"ok":       item.OK,
			"error":    item.Error,
		})
	}
	return out
}

func environmentRestoreSummaryStartupAssets(assets []environmentRestoreStartupAsset) []map[string]any {
	out := make([]map[string]any, 0, len(assets))
	for _, item := range assets {
		out = append(out, map[string]any{
			"path":        item.Path,
			"source":      item.Source,
			"composeFile": item.ComposeFile,
			"kind":        item.Kind,
			"ok":          item.OK,
			"error":       item.Error,
		})
	}
	return out
}

func environmentRestoreSummaryPackage(report environmentRestorePackageReport) map[string]any {
	return map[string]any{
		"configured": report.Configured,
		"action":     report.Action,
		"ok":         report.OK,
		"url":        report.URL,
		"branch":     report.Branch,
		"ref":        report.Ref,
		"checkout":   report.Checkout,
		"exists":     report.Exists,
		"error":      report.Error,
	}
}

func environmentRestoreSummaryRepos(repos []environmentRestoreRepoReport) []map[string]any {
	out := make([]map[string]any, 0, len(repos))
	for _, item := range repos {
		out = append(out, map[string]any{
			"serviceId": item.ServiceID,
			"action":    item.Action,
			"ok":        item.OK,
			"exists":    item.Exists,
			"branch":    item.Branch,
			"ref":       item.Ref,
			"checkout":  item.Checkout,
			"error":     item.Error,
		})
	}
	return out
}

func environmentRestoreSummaryReadiness(readiness environmentRestoreReadiness) map[string]any {
	failed := []map[string]any{}
	for _, item := range readiness.Items {
		if item.OK {
			continue
		}
		failed = append(failed, map[string]any{
			"name":     item.Name,
			"required": item.Required,
			"detail":   item.Detail,
		})
	}
	return map[string]any{
		"ok":                         readiness.OK,
		"action":                     readiness.Action,
		"pauseBeforeHeavyValidation": readiness.PauseBeforeHeavyValidation,
		"nextStep":                   readiness.NextStep,
		"failedItems":                failed,
	}
}

func environmentRestoreSummaryDocker(report environmentRestoreDockerReport) map[string]any {
	passedHealth := 0
	for _, item := range report.HealthChecks {
		if item.OK {
			passedHealth++
		}
	}
	out := map[string]any{
		"action":         report.Action,
		"ok":             report.OK,
		"composeFile":    report.ComposeFile,
		"commandCount":   len(report.Commands),
		"healthChecks":   len(report.HealthChecks),
		"healthPassed":   passedHealth,
		"healthFailed":   environmentRestoreSummaryFailedHealth(report.HealthChecks),
		"cleanup":        environmentRestoreSummaryCleanup(report.Cleanup),
		"error":          report.Error,
		"capturedOutput": len(report.Output),
	}
	return out
}

func environmentRestoreSummaryFailedHealth(checks []environmentRestoreHealthCheckReport) []map[string]any {
	out := []map[string]any{}
	for _, item := range checks {
		if item.OK {
			continue
		}
		out = append(out, map[string]any{
			"id":         item.ID,
			"kind":       item.Kind,
			"url":        redaction.URL(item.URL),
			"address":    item.Address,
			"service":    item.Service,
			"container":  item.Container,
			"statusCode": item.StatusCode,
			"state":      item.State,
			"health":     item.Health,
			"error":      item.Error,
		})
	}
	return out
}

func environmentRestoreSummaryCleanup(report environmentRestoreDockerCleanupReport) map[string]any {
	return map[string]any{
		"requested":          report.Requested,
		"allowed":            report.Allowed,
		"includeImages":      report.IncludeImages,
		"action":             report.Action,
		"reviewCommandCount": len(report.BackupCommands),
		"commandCount":       len(report.Commands),
		"error":              report.Error,
	}
}

func environmentRestoreRepoSpecs(env store.Environment, workspace string) []environmentRestoreRepoSpec {
	repoMap := jsonObjectString(env.ReposJSON)
	services := jsonArrayString(env.ServicesJSON)
	specByID := map[string]environmentRestoreRepoSpec{}
	for id, raw := range repoMap {
		spec := environmentRestoreRepoSpec{ServiceID: strings.TrimSpace(id)}
		if item, ok := raw.(map[string]any); ok {
			spec.URL = strings.TrimSpace(valueString(item["url"]))
			spec.Branch = strings.TrimSpace(valueString(item["branch"]))
			spec.Ref = strings.TrimSpace(valueString(item["ref"]))
			spec.Checkout = strings.TrimSpace(valueString(item["checkout"]))
		}
		if spec.ServiceID != "" {
			specByID[spec.ServiceID] = spec
		}
	}
	for _, raw := range services {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(valueString(item["id"]))
		if id == "" {
			continue
		}
		spec := specByID[id]
		spec.ServiceID = id
		if value := strings.TrimSpace(valueString(item["repo"])); value != "" {
			spec.URL = value
		}
		if value := strings.TrimSpace(valueString(item["branch"])); value != "" {
			spec.Branch = value
		}
		if value := strings.TrimSpace(valueString(item["ref"])); value != "" {
			spec.Ref = value
		}
		if value := strings.TrimSpace(valueString(item["checkout"])); value != "" {
			spec.Checkout = value
		}
		specByID[id] = spec
	}
	ids := make([]string, 0, len(specByID))
	for id := range specByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]environmentRestoreRepoSpec, 0, len(ids))
	for _, id := range ids {
		spec := specByID[id]
		if spec.Checkout == "" {
			spec.Checkout = filepath.Join(workspace, safeCheckoutDirName(id))
		} else if !filepath.IsAbs(spec.Checkout) {
			spec.Checkout = filepath.Join(workspace, spec.Checkout)
		}
		out = append(out, spec)
	}
	return out
}

func environmentRestorePackageSpecFromCompose(compose map[string]any, workspace string) environmentRestorePackageSpec {
	pkg := mapFromReportAny(compose["package"])
	spec := environmentRestorePackageSpec{
		URL:    strings.TrimSpace(valueString(pkg["url"])),
		Branch: strings.TrimSpace(valueString(pkg["branch"])),
		Ref:    strings.TrimSpace(valueString(pkg["ref"])),
	}
	checkout := strings.TrimSpace(valueString(pkg["checkout"]))
	if checkout == "" {
		checkout = "."
	}
	if filepath.IsAbs(checkout) {
		spec.Checkout = checkout
	} else {
		spec.Checkout = filepath.Join(workspace, checkout)
	}
	return spec
}

func environmentRestorePackage(ctx context.Context, spec environmentRestorePackageSpec, execute bool, pull bool, storeGeneratedRestore bool) environmentRestorePackageReport {
	report := environmentRestorePackageReport{
		Configured: strings.TrimSpace(spec.URL) != "" || strings.TrimSpace(spec.Ref) != "",
		URL:        spec.URL,
		Branch:     spec.Branch,
		Ref:        spec.Ref,
		Checkout:   spec.Checkout,
		OK:         true,
	}
	if !report.Configured {
		report.Action = "not-configured"
		return report
	}
	if storeGeneratedRestore {
		report.Action = "ignored-for-sql-store-restore"
		return report
	}
	repoReport := environmentRestoreRepo(ctx, environmentRestoreRepoSpec{
		ServiceID: "environment-package",
		URL:       spec.URL,
		Branch:    spec.Branch,
		Ref:       spec.Ref,
		Checkout:  spec.Checkout,
	}, execute, pull)
	report.Exists = repoReport.Exists
	report.Action = repoReport.Action
	report.Command = repoReport.Command
	report.OK = repoReport.OK
	report.Output = repoReport.Output
	report.Error = repoReport.Error
	return report
}

func environmentRestoreEffectiveHealthChecks(checks []any, compose map[string]any, graph store.EnvironmentComponentGraph) []any {
	out := []any{}
	covered := map[string]bool{}
	seen := map[string]bool{}
	startedServices := map[string]bool{}
	for _, service := range stringSliceFromAny(compose["services"]) {
		service = strings.TrimSpace(service)
		if service != "" {
			startedServices[service] = true
		}
	}
	hasServiceAllowList := len(startedServices) > 0
	addCheck := func(raw any) {
		item, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			return
		}
		if signature := environmentRestoreHealthCheckSignature(item); signature != "" {
			if seen[signature] {
				return
			}
			seen[signature] = true
		}
		if strings.TrimSpace(valueString(item["kind"])) == "compose-service" || strings.TrimSpace(valueString(item["type"])) == "compose-service" {
			if service := strings.TrimSpace(valueString(item["service"])); service != "" {
				covered[service] = true
			}
		}
		if strings.TrimSpace(valueString(item["kind"])) == "url" || strings.TrimSpace(valueString(item["type"])) == "url" {
			if service := strings.TrimSpace(valueString(item["service"])); service != "" {
				covered[service] = true
			}
		}
		out = append(out, raw)
	}
	for _, raw := range checks {
		addCheck(raw)
	}
	for _, component := range graph.Components {
		if hasServiceAllowList && strings.TrimSpace(component.ComposeService) != "" && !startedServices[strings.TrimSpace(component.ComposeService)] {
			continue
		}
		item, errText := environmentRestoreNormalizeComponentHealthCheck(component)
		if errText != "" {
			continue
		}
		addCheck(item)
	}
	for _, service := range stringSliceFromAny(compose["services"]) {
		if covered[service] {
			continue
		}
		out = append(out, map[string]any{
			"id":      "compose-service-" + safeReportID(service),
			"kind":    "compose-service",
			"service": service,
		})
		covered[service] = true
	}
	return out
}

func environmentRestoreNormalizeComponentHealthCheck(component store.EnvironmentComponent) (map[string]any, string) {
	raw := strings.TrimSpace(component.HealthCheckJSON)
	if raw == "" || raw == "{}" {
		return nil, "missing health check"
	}
	var item map[string]any
	if err := json.Unmarshal([]byte(raw), &item); err != nil || len(item) == 0 {
		if err != nil {
			return nil, "invalid health check JSON: " + err.Error()
		}
		return nil, "missing health check"
	}
	normalized := map[string]any{}
	for key, value := range item {
		normalized[key] = value
	}
	componentID := strings.TrimSpace(component.ComponentID)
	if strings.TrimSpace(valueString(normalized["id"])) == "" && componentID != "" {
		normalized["id"] = "component-" + safeReportID(componentID)
	}
	if componentID != "" {
		normalized["componentId"] = componentID
	}
	if strings.TrimSpace(valueString(normalized["service"])) == "" && strings.TrimSpace(component.ComposeService) != "" {
		normalized["service"] = strings.TrimSpace(component.ComposeService)
	}
	kind := strings.TrimSpace(valueString(normalized["kind"]))
	if kind == "" {
		kind = strings.TrimSpace(valueString(normalized["type"]))
	}
	if kind == "" && strings.TrimSpace(valueString(normalized["url"])) != "" {
		kind = "url"
	}
	normalized["kind"] = kind
	if environmentRestoreComponentRequiresURLHealth(component) && kind != "url" {
		return nil, strings.TrimSpace(component.Role) + " health check requires url"
	}
	switch kind {
	case "url":
		if strings.TrimSpace(valueString(normalized["url"])) == "" {
			return nil, "url health check requires url"
		}
	case "tcp":
		if strings.TrimSpace(valueString(normalized["address"])) == "" {
			return nil, "tcp health check requires address"
		}
	case "command":
		if strings.TrimSpace(valueString(normalized["command"])) == "" {
			return nil, "command health check requires command"
		}
	case "compose-service":
		if strings.TrimSpace(valueString(normalized["service"])) == "" {
			normalized["service"] = strings.TrimSpace(component.ComposeService)
		}
		if strings.TrimSpace(valueString(normalized["service"])) == "" {
			return nil, "compose-service health check requires service"
		}
	case "container":
		if strings.TrimSpace(valueString(normalized["container"])) == "" {
			return nil, "container health check requires container"
		}
	default:
		if kind == "" {
			return nil, "health check requires kind"
		}
		return nil, "unsupported health check kind: " + kind
	}
	return normalized, ""
}

func environmentRestoreComponentRequiresURLHealth(component store.EnvironmentComponent) bool {
	role := strings.TrimSpace(strings.ToLower(component.Role))
	kind := strings.TrimSpace(strings.ToLower(component.Kind))
	return role == "business-service" || kind == "app"
}

func environmentRestoreHealthCheckSignature(item map[string]any) string {
	kind := strings.TrimSpace(valueString(item["kind"]))
	if kind == "" {
		kind = strings.TrimSpace(valueString(item["type"]))
	}
	switch kind {
	case "url":
		return "url:" + strings.TrimSpace(valueString(item["url"]))
	case "tcp":
		return "tcp:" + strings.TrimSpace(valueString(item["address"]))
	case "command":
		return "command:" + strings.TrimSpace(valueString(item["command"]))
	case "compose-service":
		return "compose-service:" + strings.TrimSpace(valueString(item["service"]))
	case "container":
		return "container:" + strings.TrimSpace(valueString(item["container"]))
	default:
		return ""
	}
}

func environmentRestoreRequiresRemoteSources(storeURL string) bool {
	backend, err := storeBackendFromURL(strings.TrimSpace(storeURL))
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "postgres", "mysql":
		return true
	default:
		return false
	}
}

func environmentRestoreSourcePolicyReport(_ environmentRestorePackageSpec, specs []environmentRestoreRepoSpec, remoteOnly bool) environmentRestoreSourcePolicy {
	report := environmentRestoreSourcePolicy{
		RemoteOnly: remoteOnly,
		OK:         true,
	}
	if !remoteOnly {
		return report
	}
	addViolation := func(label string, rawURL string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" || environmentRestoreIsRemoteGitURL(rawURL) {
			return
		}
		report.OK = false
		report.Violations = append(report.Violations, label+" must use a remote Git URL, got local path/source: "+rawURL)
	}
	for _, spec := range specs {
		addViolation("component "+spec.ServiceID, spec.URL)
	}
	return report
}

func environmentRestoreComponentGraphReport(envID string, graph store.EnvironmentComponentGraph) environmentRestoreComponentGraph {
	return controlplane.EnvironmentComponentGraphReadinessReport(envID, graph)
}

func environmentRestoreOrderedComponentAssets(envID string, g store.EnvironmentComponentGraph) []store.ComponentConfigAsset {
	out := append([]store.ComponentConfigAsset{}, g.Assets...)
	if len(out) == 0 {
		return out
	}
	componentOrder := controlplane.EnvironmentComponentGraphReadinessReport(envID, g).BlockingOrder
	ownerIndex := map[string]int{}
	for i, id := range componentOrder {
		ownerIndex[id] = i
	}
	defaultRank := len(componentOrder) + len(g.Components) + 1
	sort.SliceStable(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		leftOwner := strings.TrimSpace(left.OwnerComponentID)
		rightOwner := strings.TrimSpace(right.OwnerComponentID)
		leftRank, leftOK := ownerIndex[leftOwner]
		if !leftOK {
			leftRank = defaultRank
		}
		rightRank, rightOK := ownerIndex[rightOwner]
		if !rightOK {
			rightRank = defaultRank
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if leftOwner != rightOwner {
			return leftOwner < rightOwner
		}
		if left.ApplyOrder != right.ApplyOrder {
			return left.ApplyOrder < right.ApplyOrder
		}
		if left.AssetID != right.AssetID {
			return left.AssetID < right.AssetID
		}
		return left.TargetPath < right.TargetPath
	})
	return out
}

func environmentRestoreComponentAssetRemoteRefOK(asset store.ComponentConfigAsset) bool {
	ref := jsonObjectString(asset.RemoteRefJSON)
	path := strings.TrimSpace(valueString(ref["path"]))
	if path == "" {
		path = strings.TrimSpace(asset.TargetPath)
	}
	cleanPath := filepath.Clean(path)
	if path == "" || filepath.IsAbs(path) || cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) {
		return false
	}
	rawURL := strings.TrimSpace(valueString(ref["url"]))
	return environmentRestoreIsRemoteGitURL(rawURL)
}

func environmentRestoreComposeWithComponentAssets(envID string, compose map[string]any, graph store.EnvironmentComponentGraph) map[string]any {
	if len(graph.Assets) == 0 {
		return compose
	}
	out := map[string]any{}
	for key, value := range compose {
		out[key] = value
	}
	generated := stringMapFromAny(out["generatedFiles"])
	generatedOrder := stringSliceFromAny(out["generatedFileOrder"])
	if len(generatedOrder) == 0 && len(generated) > 0 {
		for target := range generated {
			generatedOrder = append(generatedOrder, target)
		}
		sort.Strings(generatedOrder)
	}
	for _, asset := range environmentRestoreOrderedComponentAssets(envID, graph) {
		target := filepath.Clean(strings.TrimSpace(asset.TargetPath))
		if target == "." || target == "" || strings.HasPrefix(target, ".."+string(os.PathSeparator)) || filepath.IsAbs(target) {
			continue
		}
		if strings.TrimSpace(asset.ContentInline) == "" {
			continue
		}
		if _, exists := generated[target]; exists {
			continue
		}
		generated[target] = asset.ContentInline
		generatedOrder = append(generatedOrder, target)
	}
	if len(generated) > 0 {
		out["generatedFiles"] = generated
	}
	if len(generatedOrder) > 0 {
		out["generatedFileOrder"] = dedupeStrings(generatedOrder)
	}
	return out
}

func environmentRestoreRemoteComponentAssets(ctx context.Context, envID string, graph store.EnvironmentComponentGraph, workspace string, execute bool, pull bool) []environmentRestoreComponentAsset {
	out := []environmentRestoreComponentAsset{}
	for _, asset := range environmentRestoreOrderedComponentAssets(envID, graph) {
		if strings.TrimSpace(asset.ContentInline) != "" || strings.TrimSpace(asset.RemoteRefJSON) == "" {
			continue
		}
		ref := jsonObjectString(asset.RemoteRefJSON)
		sourceURL := strings.TrimSpace(valueString(ref["url"]))
		sourcePath := strings.TrimSpace(valueString(ref["path"]))
		if sourcePath == "" {
			sourcePath = strings.TrimSpace(asset.TargetPath)
		}
		checkout := strings.TrimSpace(valueString(ref["checkout"]))
		if checkout == "" {
			checkout = filepath.Join(workspace, ".agent-testbench", "component-assets", safeReportID(sourceURL))
		} else if !filepath.IsAbs(checkout) {
			checkout = filepath.Join(workspace, checkout)
		}
		report := environmentRestoreComponentAsset{
			AssetID:          asset.AssetID,
			OwnerComponentID: asset.OwnerComponentID,
			SourceURL:        sourceURL,
			SourcePath:       sourcePath,
			Checkout:         checkout,
			TargetPath:       restoreWorkspacePath(workspace, asset.TargetPath),
			Bytes:            asset.SizeBytes,
			ApplyOrder:       asset.ApplyOrder,
			Action:           "plan-materialize",
			OK:               true,
		}
		if !environmentRestoreComponentAssetRemoteRefOK(asset) {
			report.OK = false
			report.Error = "remote component asset requires remote Git URL plus relative source path"
			out = append(out, report)
			continue
		}
		if ok, errText := environmentRestoreGeneratedFileTargetOK(asset.TargetPath, workspace); !ok {
			report.OK = false
			report.Error = errText
			out = append(out, report)
			continue
		}
		spec := environmentRestoreRepoSpec{
			ServiceID: "component-asset-" + safeReportID(asset.AssetID),
			URL:       sourceURL,
			Branch:    strings.TrimSpace(valueString(ref["branch"])),
			Ref:       strings.TrimSpace(valueString(ref["ref"])),
			Checkout:  checkout,
		}
		repo := environmentRestoreRepo(ctx, spec, execute, pull)
		report.RepoAction = repo.Action
		report.Command = repo.Command
		if !repo.OK {
			report.OK = false
			report.Error = repo.Error
			out = append(out, report)
			continue
		}
		if !execute {
			out = append(out, report)
			continue
		}
		report.Action = "materialize"
		sourceFile := filepath.Join(checkout, filepath.Clean(sourcePath))
		raw, err := os.ReadFile(sourceFile)
		if err != nil {
			report.OK = false
			report.Error = err.Error()
			out = append(out, report)
			continue
		}
		report.Bytes = int64(len(raw))
		if err := os.MkdirAll(filepath.Dir(report.TargetPath), 0o755); err != nil {
			report.OK = false
			report.Error = err.Error()
			out = append(out, report)
			continue
		}
		if err := os.WriteFile(report.TargetPath, raw, 0o644); err != nil {
			report.OK = false
			report.Error = err.Error()
		}
		out = append(out, report)
	}
	return out
}

func environmentRestoreApplyEdgeAssets(ctx context.Context, envID string, graph store.EnvironmentComponentGraph, compose map[string]any, workspace string, execute bool, composeBaseArgs []string) []environmentRestoreAppliedAsset {
	if len(graph.Dependencies) == 0 || len(graph.Assets) == 0 {
		return nil
	}
	assetsByID := map[string]store.ComponentConfigAsset{}
	for _, asset := range graph.Assets {
		if id := strings.TrimSpace(asset.AssetID); id != "" {
			assetsByID[id] = asset
		}
	}
	componentByID := map[string]store.EnvironmentComponent{}
	for _, component := range graph.Components {
		if id := strings.TrimSpace(component.ComponentID); id != "" {
			componentByID[id] = component
		}
	}
	generated := stringMapFromAny(compose["generatedFiles"])
	out := []environmentRestoreAppliedAsset{}
	appliedAssetTargets := map[string]bool{}
	for _, dep := range graph.Dependencies {
		for _, assetID := range environmentRestoreDependencyAssetIDs(dep) {
			asset, ok := assetsByID[assetID]
			if !ok {
				out = append(out, environmentRestoreAppliedAsset{
					AssetID:            assetID,
					DependencyConsumer: dep.ConsumerComponentID,
					DependencyProvider: dep.ProviderComponentID,
					Action:             "missing-edge-asset",
					OK:                 false,
					Error:              "component dependency references missing config asset: " + assetID,
				})
				continue
			}
			targetComponentID := firstNonEmpty(strings.TrimSpace(asset.TargetComponentID), strings.TrimSpace(dep.ProviderComponentID))
			dedupeKey := assetID + "\x00" + targetComponentID
			if appliedAssetTargets[dedupeKey] {
				continue
			}
			appliedAssetTargets[dedupeKey] = true
			item := environmentRestoreApplyEdgeAsset(ctx, dep, asset, componentByID, generated, workspace, execute, composeBaseArgs)
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DependencyProvider != out[j].DependencyProvider {
			return out[i].DependencyProvider < out[j].DependencyProvider
		}
		if out[i].DependencyConsumer != out[j].DependencyConsumer {
			return out[i].DependencyConsumer < out[j].DependencyConsumer
		}
		if out[i].ApplyOrder != out[j].ApplyOrder {
			return out[i].ApplyOrder < out[j].ApplyOrder
		}
		return out[i].AssetID < out[j].AssetID
	})
	return out
}

func environmentRestoreApplyEdgeAsset(ctx context.Context, dep store.ComponentDependency, asset store.ComponentConfigAsset, components map[string]store.EnvironmentComponent, generated map[string]string, workspace string, execute bool, composeBaseArgs []string) environmentRestoreAppliedAsset {
	targetComponentID := firstNonEmpty(strings.TrimSpace(asset.TargetComponentID), strings.TrimSpace(dep.ProviderComponentID))
	targetService := environmentRestoreComponentComposeService(components[targetComponentID], targetComponentID)
	content, contentErr := environmentRestoreEdgeAssetContent(asset, workspace)
	item := environmentRestoreAppliedAsset{
		AssetID:              strings.TrimSpace(asset.AssetID),
		OwnerComponentID:     strings.TrimSpace(asset.OwnerComponentID),
		TargetComponentID:    targetComponentID,
		TargetComposeService: targetService,
		DependencyConsumer:   strings.TrimSpace(dep.ConsumerComponentID),
		DependencyProvider:   strings.TrimSpace(dep.ProviderComponentID),
		TargetPath:           strings.TrimSpace(asset.TargetPath),
		Bytes:                len(content),
		ApplyOrder:           asset.ApplyOrder,
		Action:               "plan-apply-edge-asset",
		OK:                   true,
	}
	if targetComponentID == "" {
		item.OK = false
		item.Error = "edge asset target component is required"
		return item
	}
	if environmentRestoreIsMySQLSQLAsset(asset, dep) {
		item.Action = "plan-apply-mysql-sql"
		item.Command = environmentRestoreMySQLApplyCommand(composeBaseArgs, targetService)
		if len(composeBaseArgs) == 0 || targetService == "" {
			item.OK = false
			item.Error = "mysql edge asset requires a Docker Compose target service"
			return item
		}
		if contentErr != nil {
			item.OK = false
			item.Error = contentErr.Error()
			return item
		}
		if strings.TrimSpace(content) == "" {
			item.OK = false
			item.Error = "mysql edge asset requires SQL content"
			return item
		}
		if execute {
			item.Action = "apply-mysql-sql"
			_, attempts, errText := runRestoreMySQLCommandWithInputRetry(ctx, workspace, item.Command, content)
			item.Attempts = attempts
			if errText != "" {
				item.OK = false
				item.Error = errText
			}
		}
		return item
	}
	targetPath := filepath.Clean(strings.TrimSpace(asset.TargetPath))
	if targetPath == "." || targetPath == "" {
		item.OK = false
		item.Error = "edge asset target path is required"
		return item
	}
	if _, ok := generated[targetPath]; ok {
		item.Action = "project-generated-file"
		if execute {
			item.Action = "verify-generated-file"
			if _, err := os.Stat(restoreWorkspacePath(workspace, targetPath)); err != nil {
				item.OK = false
				item.Error = err.Error()
			}
		}
		return item
	}
	item.OK = false
	item.Error = "edge asset must be generated from Store before target startup: " + targetPath
	return item
}

func environmentRestoreEdgeAssetContent(asset store.ComponentConfigAsset, workspace string) (string, error) {
	if strings.TrimSpace(asset.ContentInline) != "" {
		return asset.ContentInline, nil
	}
	targetPath := filepath.Clean(strings.TrimSpace(asset.TargetPath))
	if targetPath == "." || targetPath == "" || targetPath == ".." || filepath.IsAbs(targetPath) || strings.HasPrefix(targetPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("edge asset target path is required")
	}
	raw, err := os.ReadFile(restoreWorkspacePath(workspace, targetPath))
	if err != nil {
		return "", fmt.Errorf("read edge asset content from %s: %w", targetPath, err)
	}
	return string(raw), nil
}

func environmentRestoreDependencyAssetIDs(dep store.ComponentDependency) []string {
	profile := jsonObjectString(dep.ProfileJSON)
	ids := []string{}
	for _, key := range []string{"assetIds", "configAssetIds", "startupAssetIds", "applyAssetIds"} {
		ids = append(ids, stringSliceFromAny(profile[key])...)
	}
	if value := strings.TrimSpace(valueString(profile["assetId"])); value != "" {
		ids = append(ids, value)
	}
	return dedupeStrings(ids)
}

func environmentRestoreIsMySQLSQLAsset(asset store.ComponentConfigAsset, dep store.ComponentDependency) bool {
	kind := strings.ToLower(strings.TrimSpace(asset.AssetKind))
	capability := strings.ToLower(strings.TrimSpace(dep.Capability))
	if kind == "" {
		return false
	}
	tokens := strings.FieldsFunc(kind, func(r rune) bool {
		return r < 'a' || r > 'z'
	})
	hasSQLToken := false
	hasMySQLToken := false
	for _, token := range tokens {
		switch token {
		case "ddl", "schema", "seed", "sql":
			hasSQLToken = true
		case "mysql":
			hasMySQLToken = true
		}
	}
	if !hasSQLToken {
		return false
	}
	if hasMySQLToken {
		return true
	}
	return capability == "sql" && (environmentRestoreHasMySQLComponentSignal(asset.TargetComponentID) || environmentRestoreHasMySQLComponentSignal(dep.ProviderComponentID))
}

func environmentRestoreHasMySQLComponentSignal(componentID string) bool {
	tokens := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(componentID)), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
	for _, token := range tokens {
		if token == "mysql" {
			return true
		}
	}
	return false
}

func environmentRestoreComponentComposeService(component store.EnvironmentComponent, defaultID string) string {
	if service := strings.TrimSpace(component.ComposeService); service != "" {
		return service
	}
	return strings.TrimSpace(defaultID)
}

func environmentRestoreMySQLApplyCommand(composeBaseArgs []string, service string) []string {
	command := append([]string{"docker", "compose"}, composeBaseArgs...)
	command = append(command, "exec", "-T", service, "sh", "-lc", environmentRestoreMySQLClientScript())
	return command
}

func environmentRestoreMySQLClientScript() string {
	return `user="${MYSQL_USER:-root}"
password="${MYSQL_PASSWORD:-${MYSQL_ROOT_PASSWORD:-}}"
database="${AGENT_TESTBENCH_MYSQL_APPLY_DATABASE:-}"
set --
if [ -n "$user" ]; then
  set -- "$@" "-u${user}"
fi
if [ -n "$password" ]; then
  set -- "$@" "-p${password}"
fi
if [ -n "$database" ]; then
  set -- "$@" "${database}"
fi
exec mysql "$@"`
}

func runRestoreMySQLCommandWithInputRetry(ctx context.Context, workdir string, command []string, input string) (string, int, string) {
	const maxAttempts = 60
	const delay = time.Second
	var lastOutput string
	var lastErr string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		output, errText := runRestoreCommandWithInput(ctx, workdir, command, input)
		if errText == "" {
			return output, attempt, ""
		}
		lastOutput = output
		lastErr = errText
		if !environmentRestoreMySQLApplyErrCanRetry(errText) {
			return output, attempt, errText
		}
		if attempt == maxAttempts {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return lastOutput, attempt, ctx.Err().Error()
		case <-timer.C:
		}
	}
	return lastOutput, maxAttempts, lastErr
}

func environmentRestoreMySQLApplyErrCanRetry(errText string) bool {
	lower := strings.ToLower(errText)
	retryable := []string{
		"access denied for user 'root'@'localhost'",
		"can't connect to local mysql server",
		"can't connect to mysql server",
		"lost connection to mysql server",
		"server has gone away",
		"error 1045",
		"error 2002",
		"error 2003",
		"error 2013",
	}
	for _, item := range retryable {
		if strings.Contains(lower, item) {
			return true
		}
	}
	return false
}

func environmentRestoreIsRemoteGitURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	lower := strings.ToLower(rawURL)
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	at := strings.Index(rawURL, "@")
	colon := strings.Index(rawURL, ":")
	return at > 0 && colon > at+1
}

func environmentRestoreContainerNameConflicts(compose map[string]any, workspace string) []string {
	wanted := environmentRestoreContainerNames(compose, workspace)
	if len(wanted) == 0 {
		return nil
	}
	path, err := exec.LookPath("docker")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "ps", "-a", "--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		return nil
	}
	existing := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			existing[name] = true
		}
	}
	conflicts := []string{}
	for _, name := range wanted {
		if existing[name] {
			conflicts = append(conflicts, name)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

func environmentRestoreContainerNames(compose map[string]any, workspace string) []string {
	byService := environmentRestoreContainerNameByService(compose, workspace)
	names := make([]string, 0, len(byService))
	for _, name := range byService {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func environmentRestoreContainerNameByService(compose map[string]any, workspace string) map[string]string {
	out := map[string]string{}
	addContent := func(content string) {
		for service, container := range parseComposeContainerNames(content) {
			out[service] = container
		}
	}
	for _, content := range stringMapFromAny(compose["generatedFiles"]) {
		addContent(content)
	}
	for _, file := range environmentRestoreComposeFiles(compose) {
		path := restoreWorkspacePath(workspace, file)
		raw, err := os.ReadFile(path)
		if err == nil {
			addContent(string(raw))
		}
	}
	return out
}

func parseComposeContainerNames(content string) map[string]string {
	out := map[string]string{}
	inServices := false
	currentService := ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inServices = trimmed == "services:"
			currentService = ""
			continue
		}
		if !inServices {
			continue
		}
		if indent == 2 && strings.HasSuffix(trimmed, ":") {
			currentService = strings.TrimSuffix(trimmed, ":")
			continue
		}
		if currentService == "" || !strings.HasPrefix(trimmed, "container_name:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "container_name:"))
		name = strings.Trim(name, `"'`)
		if name != "" {
			out[currentService] = name
		}
	}
	return out
}

func environmentRestorePreflightReport(packageSpec environmentRestorePackageSpec, specs []environmentRestoreRepoSpec, compose map[string]any, workspace string, cleanupOptions environmentRestoreDockerCleanupOptions, prepareReposOnly bool) environmentRestorePreflight {
	report := environmentRestorePreflight{
		OK:                true,
		AssumeCleanDocker: cleanupOptions.AssumeCleanDocker,
		Notes: []string{
			"Sandbox control-plane Store must already be reachable outside restored Docker target services.",
			"Heavy Docker image and container validation should be reviewed before deleting or rebuilding existing local Docker state.",
		},
	}
	requiresGit := false
	if strings.TrimSpace(packageSpec.URL) != "" || strings.TrimSpace(packageSpec.Ref) != "" {
		requiresGit = true
	}
	for _, spec := range specs {
		if strings.TrimSpace(spec.URL) != "" || strings.TrimSpace(spec.Ref) != "" {
			requiresGit = true
			break
		}
	}
	if requiresGit {
		report.Tools = append(report.Tools, environmentRestoreTool("git", true))
	}
	composeFile := strings.TrimSpace(valueString(compose["composeFile"]))
	composeFiles := environmentRestoreComposeFiles(compose)
	startCommand := strings.TrimSpace(valueString(compose["startCommand"]))
	if composeFile != "" {
		report.Tools = append(report.Tools, environmentRestoreTool("docker", true))
		report.Tools = append(report.Tools, environmentRestoreCommandTool("docker compose", true, "docker", "compose", "version"))
		if !boolFromReportAny(compose["skipPull"]) {
			report.HeavySteps = append(report.HeavySteps, "docker compose pull may download images")
		}
		if !boolFromReportAny(compose["skipBuild"]) {
			report.HeavySteps = append(report.HeavySteps, "docker compose build may build images from local checkouts")
		}
		report.HeavySteps = append(report.HeavySteps, "docker compose up -d may create or replace containers")
		if cleanupOptions.Requested {
			report.HeavySteps = append(report.HeavySteps, "docker compose down may remove existing containers and orphan containers")
			if cleanupOptions.IncludeImages {
				report.HeavySteps = append(report.HeavySteps, "docker compose down --rmi all may remove local images")
			}
		} else if cleanupOptions.AssumeCleanDocker {
			report.Notes = append(report.Notes, "Clean-machine dry-run assumes target Docker containers do not exist on the colleague machine; current local container names are not treated as blockers.")
		} else if !prepareReposOnly && !cleanupOptions.UseExistingContainers {
			conflicts := environmentRestoreContainerNameConflicts(compose, workspace)
			if len(conflicts) > 0 {
				report.ContainerConflicts = conflicts
				report.OK = false
			}
		}
		if !prepareReposOnly && !cleanupOptions.UseExistingContainers {
			report.StartupAssets = environmentRestoreStartupAssets(compose, specs, workspace)
			for _, asset := range report.StartupAssets {
				if !asset.OK {
					report.OK = false
				}
			}
		}
		for _, file := range composeFiles {
			if resolved := restoreWorkspacePath(workspace, file); strings.TrimSpace(resolved) != "" {
				report.Notes = append(report.Notes, "compose file must exist before Docker execution: "+resolved)
			}
		}
	} else if startCommand != "" {
		report.HeavySteps = append(report.HeavySteps, "start command may create local runtime processes or containers")
	}
	for _, tool := range report.Tools {
		if tool.Required && !tool.OK {
			report.OK = false
		}
	}
	return report
}

func environmentRestoreReadinessReport(report environmentRestoreReport, packageSpec environmentRestorePackageSpec, specs []environmentRestoreRepoSpec, cleanupOptions environmentRestoreDockerCleanupOptions) environmentRestoreReadiness {
	readiness := environmentRestoreReadiness{
		OK:                         true,
		Action:                     "ready-for-operator-review",
		PauseBeforeHeavyValidation: true,
	}
	addItem := func(name string, required bool, ok bool, detail string) {
		readiness.Items = append(readiness.Items, environmentRestoreReadinessItem{
			Name:     name,
			Required: required,
			OK:       ok,
			Detail:   detail,
		})
		if required && !ok {
			readiness.OK = false
		}
	}

	addItem("store-boundary", true, true, "sandbox SQL Store must stay outside the restored Docker target environment")
	addItem("verification-workflow", true, strings.TrimSpace(report.VerificationWorkflow) != "", "restore is anchored to workflow "+strings.TrimSpace(report.VerificationWorkflow))
	if report.ComponentGraph.Configured {
		detail := fmt.Sprintf("%d component(s), %d blocking dependency edge(s), %d runtime edge(s), %d asset(s), %d inline asset bytes, %d remote asset(s)",
			report.ComponentGraph.Components, report.ComponentGraph.BlockingDependencies, report.ComponentGraph.RuntimeDependencies,
			report.ComponentGraph.Assets, report.ComponentGraph.InlineAssetBytes, report.ComponentGraph.RemoteAssets)
		if strings.TrimSpace(report.ComponentGraph.Error) != "" {
			detail = report.ComponentGraph.Error
		}
		addItem("component-graph", true, report.ComponentGraph.OK, detail)
		startupDetail := fmt.Sprintf("%d startup batch(es), %d health gate(s)", len(report.ComponentStartupPlan.Batches), len(report.ComponentStartupPlan.HealthGates))
		if strings.TrimSpace(report.ComponentStartupPlan.Error) != "" {
			startupDetail = report.ComponentStartupPlan.Error
		}
		addItem("component-startup-plan", true, report.ComponentStartupPlan.OK, startupDetail)
	} else if report.SourcePolicy.RemoteOnly {
		addItem("component-graph", true, false, "SQL Store one-click Docker restore requires a Store component graph for services, middleware, mocks, observability, dependencies, assets, and health gates")
	} else {
		addItem("component-graph", false, true, "no Store component graph recorded yet; restore will use legacy service and compose metadata")
	}
	if len(report.Preflight.ContainerConflicts) > 0 {
		addItem("docker-container-conflicts", true, false, "existing Docker containers would be reused or replaced by fixed container_name values: "+strings.Join(report.Preflight.ContainerConflicts, ", "))
	} else if cleanupOptions.AssumeCleanDocker {
		addItem("docker-container-conflicts", true, true, "clean-machine dry-run assumes target Docker containers are absent; no local Docker deletion was performed")
	} else if cleanupOptions.UseExistingContainers {
		addItem("docker-container-conflicts", true, true, "existing fixed-name Docker containers are explicitly adopted; Docker Compose up will not run")
	} else if strings.TrimSpace(valueString(report.Compose["composeFile"])) != "" {
		addItem("docker-container-conflicts", true, true, "no existing Docker container_name conflicts detected for non-destructive restore")
	}
	if report.SourcePolicy.RemoteOnly {
		detail := "all component source repositories must be remote Git URLs for SQL Store-backed one-click environments; environment startup files come from compact Store metadata"
		if len(report.SourcePolicy.Violations) > 0 {
			detail = strings.Join(report.SourcePolicy.Violations, "; ")
		}
		addItem("remote-git-sources", true, report.SourcePolicy.OK, detail)
	}
	if strings.TrimSpace(packageSpec.URL) != "" {
		detail := "environment package will be cloned or validated before Docker startup"
		if report.Package.Action != "" {
			detail = "environment package " + report.Package.Action + " at " + report.Package.Checkout
		}
		addItem("environment-package", true, report.Package.OK, detail)
	}
	if report.SourcePolicy.RemoteOnly {
		ok, detail := environmentRestoreStoreStartupFilesReady(report.Compose)
		addItem("store-startup-files", true, ok, detail)
	}
	startupAssetsOK, startupAssetsDetail := environmentRestoreStartupAssetsReadiness(report.Preflight.StartupAssets)
	addItem("startup-assets", true, startupAssetsOK, startupAssetsDetail)

	repoOK := true
	for _, item := range report.Repos {
		if !item.OK {
			repoOK = false
			break
		}
	}
	switch {
	case len(specs) == 0:
		addItem("component-repositories", true, true, "no component repositories recorded; Docker uses the recorded compose/start plan and existing local context")
	case report.Executed:
		addItem("component-repositories", true, repoOK, fmt.Sprintf("%d component repository checkout(s) prepared before Docker startup", len(specs)))
	default:
		addItem("component-repositories", true, repoOK, fmt.Sprintf("%d component repository checkout(s) will be cloned or validated before Docker startup", len(specs)))
	}

	dockerPlanOK := report.Docker.OK && (report.Docker.Action == "plan-docker-compose" || report.Docker.Action == "run-docker-compose" || report.Docker.Action == "plan-start-command" || report.Docker.Action == "run-start-command" || report.Docker.Action == "plan-use-existing-containers" || report.Docker.Action == "use-existing-containers" || report.Docker.Action == "skipped-after-repository-preparation")
	addItem("docker-start-plan", true, dockerPlanOK, environmentRestoreReadinessDockerDetail(report))

	composeServices := stringSliceFromAny(report.Compose["services"])
	if strings.TrimSpace(valueString(report.Compose["composeFile"])) != "" {
		detail := "Docker Compose will start all services in the recorded file, including middleware images such as Apollo or MySQL when present"
		if len(composeServices) > 0 {
			detail = "Docker Compose service allow-list: " + strings.Join(composeServices, ", ")
		}
		addItem("compose-services-and-middleware", true, true, detail)
	}

	healthProbeCount := len(report.HealthChecks)
	addItem("health-probes", true, healthProbeCount > 0, fmt.Sprintf("%d Store-backed health probe(s) recorded for post-start readiness", healthProbeCount))

	cleanupOK := true
	cleanupDetail := "Docker cleanup not requested"
	if cleanupOptions.Requested || report.Docker.Cleanup.Requested {
		cleanupOK = report.Docker.Cleanup.Requested && len(report.Docker.Cleanup.BackupCommands) > 0 && len(report.Docker.Cleanup.Commands) > 0
		if report.Executed && !report.Docker.Cleanup.Allowed {
			cleanupOK = false
		}
		cleanupDetail = "Compose-scoped cleanup must be reviewed before simulating a clean colleague machine"
	}
	addItem("docker-cleanup-review", true, cleanupOK, cleanupDetail)

	workflowReady := strings.TrimSpace(report.VerificationWorkflow) != ""
	workflowDetail := "rerun with --execute --run-workflow --server-url URL after Docker health passes"
	if report.Workflow.Action == "run-acceptance-workflow" {
		workflowReady = report.Workflow.OK
		workflowDetail = "async acceptance report status: " + statusText(report.Workflow.OK)
	}
	addItem("workflow-run-gate", true, workflowReady, workflowDetail)
	addItem("operator-pause", true, true, "pause before deleting containers/images or running long image downloads for clean-machine validation")

	if !readiness.OK {
		readiness.Action = "fix-readiness-items-before-docker"
		readiness.NextStep = "fix failed readiness items before real clean-machine validation"
		return readiness
	}
	if report.Executed && report.Workflow.Action == "run-acceptance-workflow" && report.Workflow.OK {
		readiness.Action = "restore-executed-and-workflow-verified"
		readiness.NextStep = "publish only after the async acceptance report and verified discovery gates pass"
		return readiness
	}
	if report.Executed {
		readiness.Action = "ready-for-workflow-verification"
		readiness.NextStep = "run the anchored async environment acceptance workflow and collect Evidence/topology"
		return readiness
	}
	if cleanupOptions.AssumeCleanDocker {
		readiness.Action = "ready-for-clean-machine-execute"
		readiness.NextStep = "run the same restore on the colleague machine with --execute; this dry-run did not delete or reuse local Docker containers"
		return readiness
	}
	readiness.NextStep = "review the plan, then ask for operator approval before destructive Docker cleanup or image removal"
	return readiness
}

func environmentRestoreReadinessDockerDetail(report environmentRestoreReport) string {
	switch report.Docker.Action {
	case "plan-docker-compose", "run-docker-compose":
		if report.Docker.ComposeFile != "" {
			return "Docker Compose plan uses " + report.Docker.ComposeFile
		}
		return "Docker Compose plan is recorded"
	case "plan-start-command", "run-start-command":
		return "recorded start command will run from workspace"
	case "plan-use-existing-containers", "use-existing-containers":
		return "existing Docker containers are adopted; Docker Compose startup is skipped"
	case "skipped-due-to-repository-error":
		return "Docker startup is blocked until repository preparation succeeds"
	case "skipped-due-to-preflight":
		return "Docker startup is blocked until restore preflight succeeds"
	case "skipped-after-repository-preparation":
		return "repository preparation completed; Docker startup intentionally skipped"
	case "skipped-due-to-source-policy":
		return "Docker startup is blocked until package and component sources use remote Git URLs"
	case "missing-docker-plan":
		return "composeFile or startCommand is required"
	default:
		if strings.TrimSpace(report.Docker.Error) != "" {
			return report.Docker.Error
		}
		return "Docker startup plan is not ready"
	}
}

func environmentRestoreStoreStartupFilesReady(compose map[string]any) (bool, string) {
	composeFiles := environmentRestoreComposeFiles(compose)
	if len(composeFiles) == 0 {
		if strings.TrimSpace(valueString(compose["startCommand"])) != "" {
			return true, "restore uses a recorded start command; no compose startup file is required"
		}
		return false, "composeFile or startCommand is required"
	}
	generated := stringMapFromAny(compose["generatedFiles"])
	missing := []string{}
	for _, file := range composeFiles {
		clean := filepath.Clean(strings.TrimSpace(file))
		if _, ok := generated[clean]; !ok {
			missing = append(missing, file)
		}
	}
	if len(missing) > 0 {
		return false, "SQL Store restore must write compose startup files from compact Store metadata; missing generatedFiles for: " + strings.Join(missing, ", ")
	}
	return true, fmt.Sprintf("%d compose startup file(s) will be generated from Store metadata", len(composeFiles))
}

func environmentRestoreStartupAssetsReadiness(assets []environmentRestoreStartupAsset) (bool, string) {
	if len(assets) == 0 {
		return true, "no additional Compose startup assets are required for this restore path"
	}
	missing := []string{}
	for _, asset := range assets {
		if asset.OK {
			continue
		}
		missing = append(missing, asset.Path)
	}
	if len(missing) > 0 {
		return false, "missing Compose startup assets before Docker startup: " + strings.Join(missing, ", ")
	}
	return true, fmt.Sprintf("%d Compose startup asset(s) are available before Docker startup", len(assets))
}

func environmentRestoreStartupAssets(compose map[string]any, specs []environmentRestoreRepoSpec, workspace string) []environmentRestoreStartupAsset {
	generated := stringMapFromAny(compose["generatedFiles"])
	generatedPaths := map[string]bool{}
	for path := range generated {
		generatedPaths[filepath.Clean(path)] = true
	}
	repoCheckouts := map[string]bool{}
	for _, spec := range specs {
		if spec.Checkout == "" {
			continue
		}
		repoCheckouts[filepath.Clean(spec.Checkout)] = true
	}
	candidates := []environmentRestoreStartupAssetCandidate{}
	for _, composeFile := range environmentRestoreComposeFiles(compose) {
		cleanCompose := filepath.Clean(composeFile)
		content := generated[cleanCompose]
		if content == "" {
			if raw, err := os.ReadFile(restoreWorkspacePath(workspace, composeFile)); err == nil {
				content = string(raw)
			}
		}
		if content == "" {
			continue
		}
		composeDir := filepath.Dir(cleanCompose)
		for _, item := range environmentRestoreStartupAssetCandidates(content, cleanCompose, composeDir, compose, workspace) {
			candidates = append(candidates, item)
		}
	}
	seen := map[string]bool{}
	out := []environmentRestoreStartupAsset{}
	for _, item := range candidates {
		clean := filepath.Clean(item.path)
		if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			continue
		}
		if environmentRestoreStartupAssetCoveredByRepo(clean, repoCheckouts) {
			continue
		}
		key := clean + "\x00" + item.source + "\x00" + item.composeFile
		if seen[key] {
			continue
		}
		seen[key] = true
		asset := environmentRestoreStartupAsset{
			Path:        clean,
			Source:      item.source,
			ComposeFile: item.composeFile,
			Kind:        item.kind,
			OK:          true,
		}
		if !environmentRestoreStartupAssetAvailable(clean, workspace, generatedPaths) {
			asset.OK = false
			asset.Error = "startup asset must exist in the restore workspace or be provided through Store generatedFiles"
		}
		out = append(out, asset)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Source < out[j].Source
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func environmentRestoreStartupAssetCandidates(content string, composeFile string, composeDir string, compose map[string]any, workspace string) []environmentRestoreStartupAssetCandidate {
	out := []environmentRestoreStartupAssetCandidate{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "/sandbox/compose/") {
			for _, path := range extractSandboxComposePaths(trimmed) {
				out = append(out, environmentRestoreStartupAssetCandidate{path: path, source: trimmed, composeFile: composeFile, kind: "container-command"})
			}
		}
		volume := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if volume == trimmed {
			continue
		}
		source, target, ok := parseComposeShortVolume(volume)
		if !ok || !strings.HasPrefix(target, "/") {
			continue
		}
		assetPath, assetOK := environmentRestoreStartupAssetPath(source, composeDir, compose, workspace)
		if !assetOK {
			continue
		}
		out = append(out, environmentRestoreStartupAssetCandidate{path: assetPath, source: source, composeFile: composeFile, kind: "bind-source"})
	}
	for _, envFile := range stringSliceFromAny(compose["envFiles"]) {
		if envFile == "" {
			continue
		}
		out = append(out, environmentRestoreStartupAssetCandidate{path: filepath.Clean(envFile), source: envFile, composeFile: composeFile, kind: "compose-env-file"})
	}
	return out
}

func parseComposeShortVolume(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	if strings.HasPrefix(value, "[") || strings.Contains(value, "source:") || strings.Contains(value, "target:") {
		return "", "", false
	}
	parts := strings.Split(value, ":")
	if len(parts) < 2 {
		return "", "", false
	}
	source := strings.Trim(parts[0], `"' `)
	target := strings.Trim(parts[1], `"' `)
	if source == "" || target == "" {
		return "", "", false
	}
	if !composeHostSourceLooksLikePath(source) {
		return "", "", false
	}
	return source, target, true
}

func composeHostSourceLooksLikePath(source string) bool {
	return strings.HasPrefix(source, ".") ||
		strings.HasPrefix(source, "/") ||
		strings.HasPrefix(source, "~") ||
		strings.HasPrefix(source, "$") ||
		strings.HasPrefix(source, "${")
}

func environmentRestoreStartupAssetPath(source string, composeDir string, compose map[string]any, workspace string) (string, bool) {
	expanded := expandEnvironmentRestoreComposeSource(source, compose, workspace)
	if expanded == "" {
		return "", false
	}
	if strings.HasPrefix(expanded, "../.runtime") || strings.Contains(expanded, string(os.PathSeparator)+".runtime"+string(os.PathSeparator)) {
		return "", false
	}
	if strings.HasPrefix(expanded, "~") || strings.HasPrefix(expanded, "$HOME") || strings.HasPrefix(expanded, "${HOME}") {
		return "", false
	}
	if filepath.IsAbs(expanded) {
		if rel, err := filepath.Rel(workspace, expanded); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." {
			return filepath.Clean(rel), true
		}
		return "", false
	}
	if strings.HasPrefix(expanded, "./") || strings.HasPrefix(expanded, "../") {
		return filepath.Clean(filepath.Join(composeDir, expanded)), true
	}
	return "", false
}

func expandEnvironmentRestoreComposeSource(source string, compose map[string]any, workspace string) string {
	values := stringMapFromAny(compose["env"])
	expanded := strings.TrimSpace(source)
	for key, value := range values {
		value = strings.ReplaceAll(value, "$AGENT_TESTBENCH_WORKSPACE", workspace)
		expanded = strings.ReplaceAll(expanded, "${"+key+"}", value)
		expanded = strings.ReplaceAll(expanded, "$"+key, value)
		for {
			start := strings.Index(expanded, "${"+key+":-")
			if start < 0 {
				break
			}
			end := strings.Index(expanded[start:], "}")
			if end < 0 {
				break
			}
			end += start
			expanded = expanded[:start] + value + expanded[end+1:]
		}
	}
	expanded = strings.ReplaceAll(expanded, "$AGENT_TESTBENCH_WORKSPACE", workspace)
	expanded = strings.ReplaceAll(expanded, "${AGENT_TESTBENCH_WORKSPACE}", workspace)
	return expanded
}

func extractSandboxComposePaths(value string) []string {
	out := []string{}
	for _, field := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '"' || r == '\'' || r == ',' || r == '[' || r == ']' || r == ' ' || r == '\t'
	}) {
		field = strings.TrimSpace(field)
		if !strings.HasPrefix(field, "/sandbox/compose/") {
			continue
		}
		out = append(out, filepath.Clean(strings.TrimPrefix(field, "/sandbox/")))
	}
	return out
}

func environmentRestoreStartupAssetCoveredByRepo(path string, repoCheckouts map[string]bool) bool {
	for checkout := range repoCheckouts {
		if path == checkout || strings.HasPrefix(path, checkout+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func environmentRestoreStartupAssetAvailable(path string, workspace string, generatedPaths map[string]bool) bool {
	if generatedPaths[filepath.Clean(path)] {
		return true
	}
	prefix := filepath.Clean(path) + string(os.PathSeparator)
	for generated := range generatedPaths {
		if strings.HasPrefix(generated, prefix) {
			return true
		}
	}
	if _, err := os.Stat(restoreWorkspacePath(workspace, path)); err == nil {
		return true
	}
	return false
}

func environmentRestoreTool(name string, required bool) environmentRestorePreflightTool {
	tool := environmentRestorePreflightTool{Name: name, Required: required}
	path, err := exec.LookPath(name)
	if err != nil {
		tool.OK = false
		tool.Error = err.Error()
		return tool
	}
	tool.OK = true
	tool.Path = path
	return tool
}

func environmentRestoreCommandTool(name string, required bool, command string, args ...string) environmentRestorePreflightTool {
	tool := environmentRestorePreflightTool{Name: name, Required: required}
	path, err := exec.LookPath(command)
	if err != nil {
		tool.OK = false
		tool.Error = err.Error()
		return tool
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tool.OK = false
		tool.Path = path
		tool.Error = strings.TrimSpace(string(out))
		if tool.Error == "" {
			tool.Error = err.Error()
		}
		return tool
	}
	tool.OK = true
	tool.Path = path
	return tool
}

func environmentRestoreRepo(ctx context.Context, spec environmentRestoreRepoSpec, execute bool, pull bool) environmentRestoreRepoReport {
	report := environmentRestoreRepoReport{
		ServiceID: spec.ServiceID,
		URL:       spec.URL,
		Branch:    spec.Branch,
		Ref:       spec.Ref,
		Checkout:  spec.Checkout,
		OK:        true,
	}
	if stat, err := os.Stat(spec.Checkout); err == nil && stat.IsDir() {
		report.Exists = true
		if strings.TrimSpace(spec.URL) != "" && environmentRestoreDirIsEmpty(spec.Checkout) {
			if !execute {
				report.Exists = false
				report.Action = "clone"
				args := restoreGitCloneArgs(spec)
				report.Command = append([]string{"git"}, args...)
				return report
			}
			return environmentRestoreCloneIntoCheckout(ctx, spec, report)
		}
		if strings.TrimSpace(spec.URL) != "" || strings.TrimSpace(spec.Ref) != "" {
			if ok, errText := environmentRestoreValidateCheckout(ctx, spec); !ok {
				report.OK = false
				report.Action = "invalid-existing-checkout"
				report.Error = errText
				return report
			}
		}
		if strings.TrimSpace(spec.Ref) != "" {
			if atRef, _ := environmentRestoreCheckoutDetachedAtRef(ctx, spec); atRef {
				report.Action = "use-existing-checkout"
				return report
			}
			checkoutCommands := environmentRestoreExistingRefCommands(spec)
			report.Action = "checkout-existing-ref"
			report.Command = flattenRestoreCommands(checkoutCommands)
			if !execute {
				return report
			}
			outputs := make([]string, 0, len(checkoutCommands))
			for _, command := range checkoutCommands {
				if len(command) == 0 {
					continue
				}
				output, errText := runRestoreGitCommand(ctx, command[1:]...)
				if strings.TrimSpace(output) != "" {
					outputs = append(outputs, output)
				}
				if errText != "" {
					report.OK = false
					report.Output = strings.Join(outputs, "\n")
					report.Error = errText
					return report
				}
			}
			report.Output = strings.Join(outputs, "\n")
			report.OK = true
			return report
		}
		if strings.TrimSpace(spec.URL) == "" || !execute || !pull {
			report.Action = "use-existing-checkout"
			return report
		}
		args := []string{"-C", spec.Checkout, "pull", "--ff-only"}
		report.Action = "pull-existing-checkout"
		report.Command = append([]string{"git"}, args...)
		report.Output, report.Error = runRestoreGitCommand(ctx, args...)
		report.OK = report.Error == ""
		return report
	}
	if strings.TrimSpace(spec.URL) == "" {
		report.OK = false
		report.Action = "missing-repo-url"
		report.Error = "repository url is required when checkout is missing"
		return report
	}
	if !execute {
		report.Action = "clone"
		args := restoreGitCloneArgs(spec)
		report.Command = append([]string{"git"}, args...)
		return report
	}
	if err := os.MkdirAll(filepath.Dir(spec.Checkout), 0o755); err != nil {
		report.OK = false
		report.Action = "prepare-checkout-parent"
		report.Error = err.Error()
		return report
	}
	return environmentRestoreCloneIntoCheckout(ctx, spec, report)
}

func environmentRestoreCloneIntoCheckout(ctx context.Context, spec environmentRestoreRepoSpec, report environmentRestoreRepoReport) environmentRestoreRepoReport {
	args := restoreGitCloneArgs(spec)
	report.Action = "clone"
	report.Command = append([]string{"git"}, args...)
	report.Output, report.Error = runRestoreGitCommand(ctx, args...)
	report.OK = report.Error == ""
	if report.OK && strings.TrimSpace(spec.Ref) != "" {
		checkoutArgs := []string{"-C", spec.Checkout, "checkout", "--detach", strings.TrimSpace(spec.Ref)}
		report.Command = append(report.Command, append([]string{"&&", "git"}, checkoutArgs...)...)
		output, errText := runRestoreGitCommand(ctx, checkoutArgs...)
		if strings.TrimSpace(output) != "" {
			report.Output = strings.TrimSpace(report.Output + "\n" + output)
		}
		report.Error = errText
		report.OK = report.Error == ""
	}
	return report
}

func environmentRestoreDirIsEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

func environmentRestoreExistingRefCommands(spec environmentRestoreRepoSpec) [][]string {
	out := [][]string{}
	if strings.TrimSpace(spec.URL) != "" {
		out = append(out, []string{"git", "-C", spec.Checkout, "fetch", "--tags", "origin"})
	}
	out = append(out, []string{"git", "-C", spec.Checkout, "checkout", "--detach", strings.TrimSpace(spec.Ref)})
	return out
}

func flattenRestoreCommands(commands [][]string) []string {
	out := []string{}
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "&&")
		}
		out = append(out, command...)
	}
	return out
}

func environmentRestoreValidateCheckout(ctx context.Context, spec environmentRestoreRepoSpec) (bool, string) {
	if _, errText := runRestoreGitCommand(ctx, "-C", spec.Checkout, "rev-parse", "--is-inside-work-tree"); errText != "" {
		return false, "existing checkout is not a Git repository: " + spec.Checkout
	}
	if strings.TrimSpace(spec.URL) != "" {
		remote, errText := runRestoreGitCommand(ctx, "-C", spec.Checkout, "remote", "get-url", "origin")
		if errText != "" {
			return false, errText
		}
		if strings.TrimSpace(remote) != strings.TrimSpace(spec.URL) {
			return false, fmt.Sprintf("existing checkout origin mismatch: got %s want %s", strings.TrimSpace(remote), strings.TrimSpace(spec.URL))
		}
	}
	if dirty, errText := runRestoreGitCommand(ctx, "-C", spec.Checkout, "status", "--porcelain"); errText != "" {
		return false, errText
	} else if strings.TrimSpace(dirty) != "" {
		return false, "existing checkout has uncommitted changes"
	}
	return true, ""
}

func environmentRestoreCheckoutDetachedAtRef(ctx context.Context, spec environmentRestoreRepoSpec) (bool, string) {
	head, errText := runRestoreGitCommand(ctx, "-C", spec.Checkout, "rev-parse", "HEAD")
	if errText != "" {
		return false, errText
	}
	target, errText := runRestoreGitCommand(ctx, "-C", spec.Checkout, "rev-parse", strings.TrimSpace(spec.Ref)+"^{commit}")
	if errText != "" {
		return false, errText
	}
	branch, errText := runRestoreGitCommand(ctx, "-C", spec.Checkout, "rev-parse", "--abbrev-ref", "HEAD")
	if errText != "" {
		return false, errText
	}
	return strings.TrimSpace(head) == strings.TrimSpace(target) && strings.TrimSpace(branch) == "HEAD", ""
}

func environmentRestoreUseExistingContainers(ctx context.Context, envID string, graph store.EnvironmentComponentGraph, compose map[string]any, healthChecks []any, workspace string, execute bool, healthTimeout time.Duration) environmentRestoreDockerReport {
	report := environmentRestoreDockerReport{
		OK:          true,
		Action:      "plan-use-existing-containers",
		Workdir:     workspace,
		ComposeFile: strings.Join(environmentRestoreResolvedComposeFiles(workspace, environmentRestoreComposeFiles(compose)), ","),
		Generated:   prepareEnvironmentRestoreGeneratedFiles(compose, workspace, execute),
	}
	composeBaseArgs := []string{}
	if report.ComposeFile != "" {
		composeBaseArgs = environmentRestoreComposeBaseArgs(compose, workspace, environmentRestoreResolvedComposeFiles(workspace, environmentRestoreComposeFiles(compose)))
	}
	for _, item := range report.Generated {
		if !item.OK {
			report.OK = false
			report.Action = "prepare-generated-files"
			report.Error = item.Error
			return report
		}
	}
	if !execute {
		return report
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		report.OK = false
		report.Action = "prepare-workspace"
		report.Error = err.Error()
		return report
	}
	if envFile, err := writeEnvironmentRestoreGeneratedEnvFile(workspace, compose); err != nil {
		report.OK = false
		report.Action = "prepare-compose-env"
		report.Error = err.Error()
		return report
	} else if envFile != "" {
		report.Output = append(report.Output, "generated compose env file: "+envFile)
	}
	report.Action = "use-existing-containers"
	report.AppliedAssets = environmentRestoreApplyEdgeAssets(ctx, envID, graph, compose, workspace, execute, composeBaseArgs)
	for _, asset := range report.AppliedAssets {
		if !asset.OK {
			report.OK = false
			report.Error = asset.Error
			return report
		}
	}
	report.HealthChecks = waitEnvironmentRestoreHealthChecks(ctx, environmentRestoreAdoptedContainerHealthChecks(healthChecks, compose, workspace), healthTimeout, workspace, nil)
	for _, check := range report.HealthChecks {
		if !check.OK {
			report.OK = false
		}
	}
	return report
}

func environmentRestoreAdoptedContainerHealthChecks(checks []any, compose map[string]any, workspace string) []any {
	containers := environmentRestoreContainerNameByService(compose, workspace)
	out := make([]any, 0, len(checks))
	for _, raw := range checks {
		item, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(valueString(item["kind"])) != "compose-service" {
			out = append(out, raw)
			continue
		}
		service := strings.TrimSpace(valueString(item["service"]))
		container := strings.TrimSpace(containers[service])
		if service == "" || container == "" {
			out = append(out, raw)
			continue
		}
		converted := map[string]any{}
		for key, value := range item {
			converted[key] = value
		}
		converted["kind"] = "container"
		converted["container"] = container
		out = append(out, converted)
	}
	return out
}

func environmentRestoreDocker(ctx context.Context, envID string, graph store.EnvironmentComponentGraph, compose map[string]any, healthChecks []any, workspace string, execute bool, healthTimeout time.Duration, cleanupOptions environmentRestoreDockerCleanupOptions) environmentRestoreDockerReport {
	report := environmentRestoreDockerReport{
		OK:      true,
		Workdir: workspace,
	}
	composeFile := strings.TrimSpace(valueString(compose["composeFile"]))
	composeFiles := environmentRestoreComposeFiles(compose)
	startCommand := strings.TrimSpace(valueString(compose["startCommand"]))
	composeBaseArgs := []string{}
	switch {
	case composeFile != "":
		report.Action = "plan-docker-compose"
		resolvedComposeFiles := environmentRestoreResolvedComposeFiles(workspace, composeFiles)
		report.ComposeFile = strings.Join(resolvedComposeFiles, ",")
		baseArgs := environmentRestoreComposeBaseArgs(compose, workspace, resolvedComposeFiles)
		composeBaseArgs = baseArgs
		services := stringSliceFromAny(compose["services"])
		report.Cleanup = environmentRestoreDockerCleanupPlan(baseArgs, cleanupOptions)
		imageServices, buildServices := environmentRestoreComposeCommandServices(compose, workspace, composeFiles, services)
		if !boolFromReportAny(compose["skipPull"]) {
			if len(imageServices) > 0 {
				report.Commands = append(report.Commands, append(append([]string{"docker", "compose"}, baseArgs...), append([]string{"pull"}, imageServices...)...))
			}
		}
		if !boolFromReportAny(compose["skipBuild"]) {
			if len(buildServices) > 0 {
				report.Commands = append(report.Commands, append(append([]string{"docker", "compose"}, baseArgs...), append([]string{"build"}, buildServices...)...))
			}
		}
		report.Commands = append(report.Commands, append(append([]string{"docker", "compose"}, baseArgs...), append([]string{"up", "-d"}, services...)...))
	case startCommand != "":
		report.Action = "plan-start-command"
		report.Commands = [][]string{{"/bin/sh", "-c", startCommand}}
		if cleanupOptions.Requested {
			report.OK = false
			report.Cleanup = environmentRestoreDockerCleanupReport{
				Requested:     true,
				Allowed:       cleanupOptions.Allowed,
				IncludeImages: cleanupOptions.IncludeImages,
				Action:        "unsupported-cleanup",
				Error:         "Docker cleanup requires a recorded composeFile",
			}
			report.Error = report.Cleanup.Error
			return report
		}
	default:
		report.OK = false
		report.Action = "missing-docker-plan"
		report.Error = "composeFile or startCommand is required to restore Docker services"
		return report
	}
	report.Generated = prepareEnvironmentRestoreGeneratedFiles(compose, workspace, false)
	for _, item := range report.Generated {
		if !item.OK {
			report.OK = false
			report.Action = "prepare-generated-files"
			report.Error = item.Error
			return report
		}
	}
	if !execute {
		return report
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		report.OK = false
		report.Action = "prepare-workspace"
		report.Error = err.Error()
		return report
	}
	report.Generated = prepareEnvironmentRestoreGeneratedFiles(compose, workspace, true)
	for _, item := range report.Generated {
		if !item.OK {
			report.OK = false
			report.Action = "prepare-generated-files"
			report.Error = item.Error
			return report
		}
	}
	if envFile, err := writeEnvironmentRestoreGeneratedEnvFile(workspace, compose); err != nil {
		report.OK = false
		report.Action = "prepare-compose-env"
		report.Error = err.Error()
		return report
	} else if envFile != "" {
		report.Output = append(report.Output, "generated compose env file: "+envFile)
	}
	if report.ComposeFile != "" {
		for _, composeFile := range strings.Split(report.ComposeFile, ",") {
			composeFile = strings.TrimSpace(composeFile)
			if composeFile == "" {
				continue
			}
			if stat, err := os.Stat(composeFile); err != nil {
				report.OK = false
				report.Action = "missing-compose-file"
				report.Error = fmt.Sprintf("compose file is required before Docker execution: %s", composeFile)
				return report
			} else if stat.IsDir() {
				report.OK = false
				report.Action = "invalid-compose-file"
				report.Error = fmt.Sprintf("compose file path is a directory: %s", composeFile)
				return report
			}
		}
		if report.Cleanup.Requested {
			if !report.Cleanup.Allowed {
				report.OK = false
				report.Cleanup.Action = "cleanup-blocked"
				report.Cleanup.Error = "Docker cleanup requested during --execute; rerun with --allow-destructive-docker-cleanup after reviewing cleanup commands"
				report.Error = report.Cleanup.Error
				return report
			}
			report.Cleanup.Action = "run-cleanup"
			for _, command := range append(report.Cleanup.BackupCommands, report.Cleanup.Commands...) {
				output, errText := runRestoreCommand(ctx, workspace, command)
				if strings.TrimSpace(output) != "" {
					report.Cleanup.Output = append(report.Cleanup.Output, output)
				}
				if errText != "" {
					report.OK = false
					report.Cleanup.Error = errText
					report.Error = errText
					return report
				}
			}
		}
	}
	if report.Action == "plan-docker-compose" {
		report.Action = "run-docker-compose"
	} else {
		report.Action = "run-start-command"
	}
	for _, command := range report.Commands {
		output, errText := runRestoreCommand(ctx, workspace, command)
		if strings.TrimSpace(output) != "" {
			report.Output = append(report.Output, output)
		}
		if errText != "" {
			report.OK = false
			report.Error = errText
			return report
		}
	}
	report.AppliedAssets = environmentRestoreApplyEdgeAssets(ctx, envID, graph, compose, workspace, execute, composeBaseArgs)
	for _, asset := range report.AppliedAssets {
		if !asset.OK {
			report.OK = false
			report.Error = asset.Error
			return report
		}
	}
	report.HealthChecks = waitEnvironmentRestoreHealthChecks(ctx, healthChecks, healthTimeout, workspace, composeBaseArgs)
	for _, check := range report.HealthChecks {
		if !check.OK {
			report.OK = false
		}
	}
	return report
}

func environmentRestoreDockerCleanupPlan(baseArgs []string, options environmentRestoreDockerCleanupOptions) environmentRestoreDockerCleanupReport {
	if !options.Requested {
		return environmentRestoreDockerCleanupReport{}
	}
	cleanup := environmentRestoreDockerCleanupReport{
		Requested:     true,
		Allowed:       options.Allowed,
		IncludeImages: options.IncludeImages,
		Action:        "plan-cleanup",
		Warning:       "Review Docker cleanup commands before simulating a clean colleague machine; the sandbox SQL Store must remain outside these Docker target services.",
	}
	cleanup.BackupCommands = [][]string{
		append(append([]string{"docker", "compose"}, baseArgs...), "ps"),
		append(append([]string{"docker", "compose"}, baseArgs...), "images"),
		append(append([]string{"docker", "compose"}, baseArgs...), "config"),
	}
	down := append(append([]string{"docker", "compose"}, baseArgs...), "down", "--remove-orphans")
	if options.IncludeImages {
		down = append(down, "--rmi", "all")
	}
	cleanup.Commands = [][]string{down}
	return cleanup
}

func environmentRestoreRunWorkflow(ctx context.Context, workflowID string, workspace string, options environmentRestoreWorkflowOptions) environmentRestoreWorkflowRun {
	report := environmentRestoreWorkflowRun{
		WorkflowID: workflowID,
		Action:     "run-acceptance-workflow",
	}
	if strings.TrimSpace(options.ServerURL) == "" {
		report.Error = "--server-url is required for async environment acceptance"
		return report
	}
	if strings.TrimSpace(options.EnvironmentID) == "" {
		report.Error = "environment id is required for async environment acceptance"
		return report
	}
	outputDir := strings.TrimSpace(options.OutputDir)
	if outputDir == "" {
		outputDir = filepath.Join(workspace, ".agent-testbench", "reports", "acceptance."+safeReportID(workflowID)+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.OutputDir = absOutputDir
	requestID := "restore." + safeReportID(options.EnvironmentID) + "." + time.Now().UTC().Format("20060102T150405.000000000Z")
	payload := map[string]any{
		"requestId":   requestID,
		"evidenceDir": absOutputDir,
	}
	if strings.TrimSpace(options.BaseURL) != "" {
		payload["baseUrl"] = strings.TrimSpace(options.BaseURL)
	}
	if options.TimeoutSeconds > 0 {
		payload["timeoutSeconds"] = options.TimeoutSeconds
	}
	started, err := postWorkflowAcceptanceJSON(ctx, environmentAcceptanceRunURL(options.ServerURL, options.EnvironmentID, ""), payload)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.RunID = strings.TrimSpace(valueString(started["batchRunId"]))
	report.ReportURL = strings.TrimSpace(valueString(started["reportUrl"]))
	if report.RunID == "" {
		report.Error = "environment acceptance start did not return batchRunId"
		return report
	}
	finalPayload, err := waitEnvironmentAcceptanceReport(ctx, options.ServerURL, options.EnvironmentID, report.RunID, options.TimeoutSeconds)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.Acceptance = environmentRestoreAcceptanceFromPayload(finalPayload["acceptance"])
	report.WorkflowID = firstNonEmpty(report.Acceptance.WorkflowID, workflowID)
	report.Counts = workflowCaseReportCounts{
		Total:  report.Acceptance.ExpectedSteps,
		Passed: report.Acceptance.PassedSteps,
		Failed: report.Acceptance.FailedSteps,
	}
	report.OK = report.Acceptance.OK &&
		report.Acceptance.TemplateID == "environment.workflow.skywalking.v1" &&
		report.Acceptance.WorkflowID == workflowID &&
		report.Acceptance.ExpectedSteps > 0 &&
		report.Acceptance.CompletedSteps == report.Acceptance.ExpectedSteps &&
		report.Acceptance.PassedSteps == report.Acceptance.ExpectedSteps &&
		report.Acceptance.FailedSteps == 0 &&
		report.Acceptance.TopologyProvider == "skywalking"
	if !report.OK {
		report.Error = "async acceptance report did not pass"
	}
	return report
}

func waitEnvironmentAcceptanceReport(ctx context.Context, serverURL string, environmentID string, runID string, timeoutSeconds int) (map[string]any, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	var last map[string]any
	for {
		payload, err := fetchWorkflowAcceptanceJSON(ctx, environmentAcceptanceRunURL(serverURL, environmentID, runID))
		if err != nil {
			return nil, err
		}
		last = payload
		status := strings.TrimSpace(valueString(payload["status"]))
		if status != "" && status != store.StatusRunning {
			return payload, nil
		}
		if time.Now().After(deadline) {
			return last, fmt.Errorf("timed out waiting for async environment acceptance report: %s", runID)
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, ctx.Err()
		case <-timer.C:
		}
	}
}

func environmentRestoreAcceptanceFromPayload(value any) environmentRestoreWorkflowAcceptance {
	payload, _ := value.(map[string]any)
	return environmentRestoreWorkflowAcceptance{
		OK:               boolFromReportAny(payload["ok"]),
		TemplateID:       strings.TrimSpace(valueString(payload["templateId"])),
		WorkflowID:       strings.TrimSpace(valueString(payload["workflowId"])),
		ExpectedSteps:    intFromReportAny(payload["expectedSteps"]),
		CompletedSteps:   intFromReportAny(payload["completedSteps"]),
		PassedSteps:      intFromReportAny(payload["passedSteps"]),
		FailedSteps:      intFromReportAny(payload["failedSteps"]),
		TopologyProvider: strings.TrimSpace(valueString(payload["topologyProvider"])),
	}
}

func restoreGitCloneArgs(spec environmentRestoreRepoSpec) []string {
	args := []string{"clone"}
	if strings.TrimSpace(spec.Branch) != "" {
		args = append(args, "--branch", strings.TrimSpace(spec.Branch))
	}
	args = append(args, strings.TrimSpace(spec.URL), strings.TrimSpace(spec.Checkout))
	return args
}

func restoreWorkspacePath(workspace string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(workspace, value)
}

func environmentRestoreComposeFiles(compose map[string]any) []string {
	files := stringSliceFromAny(compose["composeFiles"])
	if len(files) == 0 {
		if file := strings.TrimSpace(valueString(compose["composeFile"])); file != "" {
			files = []string{file}
		}
	}
	return files
}

func environmentRestoreResolvedComposeFiles(workspace string, files []string) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		if resolved := restoreWorkspacePath(workspace, file); strings.TrimSpace(resolved) != "" {
			out = append(out, resolved)
		}
	}
	return out
}

func prepareEnvironmentRestoreGeneratedFiles(compose map[string]any, workspace string, execute bool) []environmentRestoreGeneratedFile {
	files := stringMapFromAny(compose["generatedFiles"])
	if len(files) == 0 {
		return nil
	}
	paths := environmentRestoreGeneratedFilePaths(compose, files)
	out := make([]environmentRestoreGeneratedFile, 0, len(paths))
	for _, path := range paths {
		content := files[path]
		report := environmentRestoreGeneratedFile{
			Path:   restoreWorkspacePath(workspace, path),
			Bytes:  len(content),
			Action: "plan-write",
			OK:     true,
		}
		if ok, errText := environmentRestoreGeneratedFileTargetOK(path, workspace); !ok {
			report.OK = false
			report.Error = errText
			out = append(out, report)
			continue
		}
		if execute {
			report.Action = "write"
			if err := os.MkdirAll(filepath.Dir(report.Path), 0o755); err != nil {
				report.OK = false
				report.Error = err.Error()
			} else if err := os.WriteFile(report.Path, []byte(content), 0o644); err != nil {
				report.OK = false
				report.Error = err.Error()
			}
		}
		out = append(out, report)
	}
	return out
}

func environmentRestoreGeneratedFilePaths(compose map[string]any, files map[string]string) []string {
	paths := make([]string, 0, len(files))
	seen := map[string]bool{}
	for _, path := range stringSliceFromAny(compose["generatedFileOrder"]) {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "." || clean == "" || seen[clean] {
			continue
		}
		if _, exists := files[clean]; !exists {
			continue
		}
		paths = append(paths, clean)
		seen[clean] = true
	}
	remaining := make([]string, 0, len(files)-len(paths))
	for path := range files {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "." || clean == "" || seen[clean] {
			continue
		}
		remaining = append(remaining, clean)
	}
	sort.Strings(remaining)
	paths = append(paths, remaining...)
	return paths
}

func dedupeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
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

func environmentRestoreGeneratedFileTargetOK(path string, workspace string) (bool, string) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return false, "generated file path is empty"
	}
	if filepath.IsAbs(raw) {
		return false, "generated file path must be relative to the restore workspace: " + raw
	}
	clean := filepath.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return false, "generated file path must stay inside the restore workspace: " + raw
	}
	target := restoreWorkspacePath(workspace, clean)
	rel, err := filepath.Rel(workspace, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, "generated file path must stay inside the restore workspace: " + raw
	}
	return true, ""
}

func environmentRestoreComposeBaseArgs(compose map[string]any, workspace string, composeFiles []string) []string {
	args := []string{}
	for _, composeFile := range composeFiles {
		args = append(args, "-f", composeFile)
	}
	if len(stringMapFromAny(compose["env"])) > 0 {
		args = append(args, "--env-file", environmentRestoreGeneratedEnvFilePath(workspace))
	}
	if projectName := strings.TrimSpace(valueString(compose["projectName"])); projectName != "" {
		args = append(args, "-p", projectName)
	}
	for _, envFile := range stringSliceFromAny(compose["envFiles"]) {
		args = append(args, "--env-file", restoreWorkspacePath(workspace, envFile))
	}
	for _, profile := range stringSliceFromAny(compose["profiles"]) {
		args = append(args, "--profile", profile)
	}
	return args
}

func environmentRestoreGeneratedEnvFilePath(workspace string) string {
	return filepath.Join(workspace, ".agent-testbench", "restore.env")
}

func environmentRestoreComposeCommandServices(compose map[string]any, workspace string, composeFiles []string, selected []string) ([]string, []string) {
	knownServices, buildServices := environmentRestoreComposeBuildServiceSet(compose, workspace, composeFiles)
	services := append([]string{}, selected...)
	if len(services) == 0 && len(knownServices) > 0 {
		services = make([]string, 0, len(knownServices))
		for service := range knownServices {
			services = append(services, service)
		}
		sort.Strings(services)
	}
	imageOut := []string{}
	buildOut := []string{}
	for _, service := range services {
		service = strings.TrimSpace(service)
		if service == "" {
			continue
		}
		if buildServices[service] {
			buildOut = append(buildOut, service)
			continue
		}
		imageOut = append(imageOut, service)
	}
	return imageOut, buildOut
}

func environmentRestoreComposeBuildServiceSet(compose map[string]any, workspace string, composeFiles []string) (map[string]bool, map[string]bool) {
	known := map[string]bool{}
	builds := map[string]bool{}
	generated := stringMapFromAny(compose["generatedFiles"])
	for _, file := range composeFiles {
		content := generated[filepath.Clean(file)]
		if content == "" {
			content = generated[file]
		}
		if content == "" {
			if raw, err := os.ReadFile(restoreWorkspacePath(workspace, file)); err == nil {
				content = string(raw)
			}
		}
		if content == "" {
			continue
		}
		fileKnown, fileBuilds := environmentRestoreComposeBuildServicesFromText(content)
		for service := range fileKnown {
			known[service] = true
		}
		for service := range fileBuilds {
			known[service] = true
			builds[service] = true
		}
	}
	return known, builds
}

func environmentRestoreComposeBuildServicesFromText(content string) (map[string]bool, map[string]bool) {
	known := map[string]bool{}
	builds := map[string]bool{}
	inServices := false
	servicesIndent := -1
	serviceIndent := -1
	currentService := ""
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := leadingSpaceCount(line)
		trimmed := strings.TrimSpace(line)
		if !inServices {
			if trimmed == "services:" {
				inServices = true
				servicesIndent = indent
			}
			continue
		}
		if indent <= servicesIndent {
			break
		}
		if strings.HasPrefix(trimmed, "-") {
			continue
		}
		if strings.HasSuffix(trimmed, ":") {
			key := strings.TrimSuffix(trimmed, ":")
			if serviceIndent < 0 || indent == serviceIndent {
				serviceIndent = indent
				currentService = strings.TrimSpace(key)
				if currentService != "" {
					known[currentService] = true
				}
				continue
			}
		}
		if currentService != "" && indent > serviceIndent && (trimmed == "build:" || strings.HasPrefix(trimmed, "build: ")) {
			builds[currentService] = true
		}
	}
	return known, builds
}

func leadingSpaceCount(value string) int {
	count := 0
	for _, r := range value {
		if r != ' ' {
			break
		}
		count++
	}
	return count
}

func writeEnvironmentRestoreGeneratedEnvFile(workspace string, compose map[string]any) (string, error) {
	values := stringMapFromAny(compose["env"])
	if len(values) == 0 {
		return "", nil
	}
	path := environmentRestoreGeneratedEnvFilePath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		value := strings.ReplaceAll(values[key], "$AGENT_TESTBENCH_WORKSPACE", workspace)
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(value)
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func stringMapFromAny(value any) map[string]string {
	out := map[string]string{}
	switch typed := value.(type) {
	case map[string]string:
		for key, value := range typed {
			if strings.TrimSpace(key) != "" {
				out[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	case map[string]any:
		for key, value := range typed {
			if strings.TrimSpace(key) != "" {
				out[strings.TrimSpace(key)] = strings.TrimSpace(valueString(value))
			}
		}
	}
	return out
}

func stringSliceFromAny(value any) []string {
	values, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if strings.TrimSpace(item) != "" {
					out = append(out, strings.TrimSpace(item))
				}
			}
			return out
		}
		return nil
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if value := strings.TrimSpace(valueString(item)); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func runRestoreGitCommand(ctx context.Context, args ...string) (string, string) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, err.Error()
	}
	return output, ""
}

func runRestoreCommand(ctx context.Context, workdir string, command []string) (string, string) {
	if len(command) == 0 {
		return "", "empty restore command"
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if output != "" {
			return output, err.Error() + ": " + output
		}
		return output, err.Error()
	}
	return output, ""
}

func runRestoreCommandWithInput(ctx context.Context, workdir string, command []string, input string) (string, string) {
	if len(command) == 0 {
		return "", "empty restore command"
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workdir
	cmd.Stdin = bytes.NewBufferString(input)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if output != "" {
			return output, err.Error() + ": " + output
		}
		return output, err.Error()
	}
	return output, ""
}

func waitEnvironmentRestoreHealthChecks(ctx context.Context, checks []any, timeout time.Duration, workspace string, composeBaseArgs []string) []environmentRestoreHealthCheckReport {
	out := make([]environmentRestoreHealthCheckReport, 0, len(checks))
	for _, raw := range checks {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.TrimSpace(valueString(item["kind"]))
		if kind == "" && strings.TrimSpace(valueString(item["url"])) != "" {
			kind = "url"
		}
		check := environmentRestoreHealthCheckReport{
			ID:        strings.TrimSpace(valueString(item["id"])),
			Kind:      kind,
			URL:       strings.TrimSpace(valueString(item["url"])),
			Address:   strings.TrimSpace(valueString(item["address"])),
			Command:   strings.TrimSpace(valueString(item["command"])),
			Service:   strings.TrimSpace(valueString(item["service"])),
			Container: strings.TrimSpace(valueString(item["container"])),
		}
		switch check.Kind {
		case "url", "":
			if check.URL == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreURLHealthCheck(ctx, check, timeout))
		case "tcp":
			if check.Address == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreTCPHealthCheck(ctx, check, timeout))
		case "command":
			if check.Command == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreCommandHealthCheck(ctx, check, timeout, workspace))
		case "compose-service":
			if check.Service == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreComposeServiceHealthCheck(ctx, check, timeout, workspace, composeBaseArgs))
		case "container":
			if check.Container == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreContainerHealthCheck(ctx, check, timeout))
		default:
			check.Error = "unsupported health check kind: " + check.Kind
			out = append(out, check)
		}
	}
	return out
}

func waitEnvironmentRestoreURLHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration) environmentRestoreHealthCheckReport {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastErr string
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, check.URL, nil)
		if err != nil {
			check.Error = err.Error()
			return check
		}
		resp, err := client.Do(req)
		if err == nil {
			check.StatusCode = resp.StatusCode
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				check.OK = true
				check.Error = ""
				return check
			}
			lastErr = fmt.Sprintf("health check returned HTTP %d", resp.StatusCode)
		} else {
			lastErr = err.Error()
		}
		if time.Now().After(deadline) {
			check.Error = lastErr
			return check
		}
		select {
		case <-ctx.Done():
			check.Error = ctx.Err().Error()
			return check
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func waitEnvironmentRestoreTCPHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration) environmentRestoreHealthCheckReport {
	deadline := time.Now().Add(timeout)
	var lastErr string
	for {
		dialer := net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", check.Address)
		if err == nil {
			_ = conn.Close()
			check.OK = true
			check.Error = ""
			return check
		}
		lastErr = err.Error()
		if time.Now().After(deadline) {
			check.Error = lastErr
			return check
		}
		select {
		case <-ctx.Done():
			check.Error = ctx.Err().Error()
			return check
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func waitEnvironmentRestoreCommandHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration, workspace string) environmentRestoreHealthCheckReport {
	return waitEnvironmentRestoreCommand(ctx, check, timeout, workspace, []string{"/bin/sh", "-c", check.Command}, func(check *environmentRestoreHealthCheckReport, output string) bool {
		check.Output = truncateReportText(output, 200)
		return true
	})
}

func waitEnvironmentRestoreComposeServiceHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration, workspace string, composeBaseArgs []string) environmentRestoreHealthCheckReport {
	if len(composeBaseArgs) == 0 {
		check.Error = "compose service health check requires composeFile"
		return check
	}
	command := append(append([]string{"docker", "compose"}, composeBaseArgs...), "ps", "--format", "json", check.Service)
	return waitEnvironmentRestoreCommand(ctx, check, timeout, workspace, command, func(check *environmentRestoreHealthCheckReport, output string) bool {
		check.Output = truncateReportText(output, 200)
		state, health := parseComposeServiceHealth(output)
		check.State = state
		check.Health = health
		return state == "running" && (health == "" || health == "healthy")
	})
}

func waitEnvironmentRestoreContainerHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration) environmentRestoreHealthCheckReport {
	command := []string{"docker", "inspect", "--format", "{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}", check.Container}
	return waitEnvironmentRestoreCommand(ctx, check, timeout, "", command, func(check *environmentRestoreHealthCheckReport, output string) bool {
		check.Output = truncateReportText(output, 200)
		fields := strings.Fields(output)
		if len(fields) > 0 {
			check.State = strings.TrimSpace(fields[0])
		}
		if len(fields) > 1 {
			check.Health = strings.TrimSpace(fields[1])
		}
		return check.State == "running" && (check.Health == "" || check.Health == "healthy")
	})
}

func waitEnvironmentRestoreCommand(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration, workspace string, command []string, ok func(*environmentRestoreHealthCheckReport, string) bool) environmentRestoreHealthCheckReport {
	deadline := time.Now().Add(timeout)
	var lastErr string
	for {
		output, errText := runRestoreCommand(ctx, workspace, command)
		if errText == "" && ok(&check, output) {
			check.OK = true
			check.Error = ""
			if check.Output == "" {
				check.Output = truncateReportText(output, 200)
			}
			return check
		}
		if errText != "" {
			lastErr = errText
		} else {
			lastErr = "health command did not report ready"
		}
		if time.Now().After(deadline) {
			check.Error = lastErr
			if check.Output == "" {
				check.Output = truncateReportText(output, 200)
			}
			return check
		}
		select {
		case <-ctx.Done():
			check.Error = ctx.Err().Error()
			return check
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func parseComposeServiceHealth(output string) (string, string) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", ""
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(output), &object); err == nil && object != nil {
		return strings.ToLower(valueString(firstNonNil(object["State"], object["state"]))), strings.ToLower(valueString(firstNonNil(object["Health"], object["health"])))
	}
	var array []map[string]any
	if err := json.Unmarshal([]byte(output), &array); err == nil && len(array) > 0 {
		return strings.ToLower(valueString(firstNonNil(array[0]["State"], array[0]["state"]))), strings.ToLower(valueString(firstNonNil(array[0]["Health"], array[0]["health"])))
	}
	lower := strings.ToLower(output)
	state := ""
	health := ""
	if strings.Contains(lower, "running") {
		state = "running"
	}
	if strings.Contains(lower, "unhealthy") {
		health = "unhealthy"
	} else if strings.Contains(lower, "healthy") {
		health = "healthy"
	}
	return state, health
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func safeCheckoutDirName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "service"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-")
	return replacer.Replace(value)
}

func printEnvironmentRestoreReport(report environmentRestoreReport) {
	fmt.Printf("Environment Restore: %s\n", report.EnvironmentID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Executed: %t\n", report.Executed)
	fmt.Printf("Workspace: %s\n", report.Workspace)
	fmt.Printf("Verification Workflow: %s\n", report.VerificationWorkflow)
	if report.RestoreID != "" {
		fmt.Printf("Restore ID: %s\n", report.RestoreID)
	}
	if report.Error != "" {
		fmt.Printf("Error: %s\n", report.Error)
	}
	if report.Readiness.Action != "" {
		fmt.Printf("Readiness: %s (ok=%t)\n", report.Readiness.Action, report.Readiness.OK)
		for _, item := range report.Readiness.Items {
			state := "ok"
			if !item.OK {
				state = "failed"
			}
			fmt.Printf("  %s: %s\n", item.Name, state)
			if item.Detail != "" {
				fmt.Printf("    %s\n", item.Detail)
			}
		}
		if report.Readiness.NextStep != "" {
			fmt.Printf("  next: %s\n", report.Readiness.NextStep)
		}
	}
	for _, repo := range report.Repos {
		state := repo.Action
		if !repo.OK {
			state = "failed"
		}
		fmt.Printf("- %s [%s]\n", repo.ServiceID, state)
		fmt.Printf("  checkout: %s\n", repo.Checkout)
		if repo.URL != "" {
			fmt.Printf("  repo: %s\n", repo.URL)
		}
		if repo.Branch != "" {
			fmt.Printf("  branch: %s\n", repo.Branch)
		}
		if repo.Error != "" {
			fmt.Printf("  error: %s\n", repo.Error)
		}
	}
	dockerState := report.Docker.Action
	if !report.Docker.OK {
		dockerState = "failed"
	}
	fmt.Printf("Docker: %s\n", dockerState)
	if report.Docker.ComposeFile != "" {
		fmt.Printf("  compose: %s\n", report.Docker.ComposeFile)
	}
	for _, command := range report.Docker.Commands {
		fmt.Printf("  command: %s\n", strings.Join(command, " "))
	}
	if report.Docker.Cleanup.Requested {
		fmt.Printf("  cleanup: %s\n", report.Docker.Cleanup.Action)
		if report.Docker.Cleanup.Warning != "" {
			fmt.Printf("    warning: %s\n", report.Docker.Cleanup.Warning)
		}
		for _, command := range report.Docker.Cleanup.BackupCommands {
			fmt.Printf("    backup: %s\n", strings.Join(command, " "))
		}
		for _, command := range report.Docker.Cleanup.Commands {
			fmt.Printf("    cleanup-command: %s\n", strings.Join(command, " "))
		}
		if report.Docker.Cleanup.Error != "" {
			fmt.Printf("    error: %s\n", report.Docker.Cleanup.Error)
		}
	}
	for _, check := range report.Docker.HealthChecks {
		state := "failed"
		if check.OK {
			state = "ok"
		}
		fmt.Printf("  health: %s [%s]\n", check.URL, state)
		if check.Error != "" {
			fmt.Printf("    error: %s\n", check.Error)
		}
	}
	if report.Docker.Error != "" {
		fmt.Printf("  error: %s\n", report.Docker.Error)
	}
	fmt.Printf("Workflow: %s [%s]\n", report.Workflow.WorkflowID, report.Workflow.Action)
	if report.Workflow.RunID != "" {
		fmt.Printf("  run: %s\n", report.Workflow.RunID)
	}
	if report.Workflow.OutputDir != "" {
		fmt.Printf("  report: %s\n", report.Workflow.OutputDir)
	}
	if report.Workflow.Error != "" {
		fmt.Printf("  error: %s\n", report.Workflow.Error)
	}
	for _, action := range report.NextActions {
		fmt.Printf("Next: %s\n", action)
	}
}

func runEnvironmentVerify(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runID := flags.String("run", "", "Verification run id")
	status := flags.String("status", "", "Verification status")
	evidenceComplete := flags.Bool("evidence-complete", false, "Evidence is complete for the verification run")
	topologyComplete := flags.Bool("topology-complete", false, "SkyWalking topology is complete for the verification run")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	id := strings.TrimSpace(flags.Arg(0))
	if id == "" {
		return errors.New("environment id is required")
	}
	if strings.TrimSpace(*runID) == "" || strings.TrimSpace(*status) == "" {
		return errors.New("--run and --status are required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	env, err := runtime.GetEnvironment(ctx, id)
	if err != nil {
		return err
	}
	env.LastVerificationRunID = strings.TrimSpace(*runID)
	env.LastVerificationStatus = strings.TrimSpace(*status)
	env.EvidenceComplete = *evidenceComplete
	env.TopologyComplete = *topologyComplete
	env.Verified = false
	env.Status = "verification-recorded"
	if env.LastVerificationStatus == store.StatusPassed && env.EvidenceComplete && env.TopologyComplete {
		env.Status = "verified-ready"
		env.LastVerifiedAt = time.Now().UTC()
	}
	env, err = runtime.UpsertEnvironment(ctx, env)
	if err != nil {
		return err
	}
	return printEnvironmentCommandResult(env, *jsonOutput)
}

func runEnvironmentPublishVerified(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("environment publish-verified", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := parseInterspersedFlags(flags, args); err != nil {
		return err
	}
	id := strings.TrimSpace(flags.Arg(0))
	if id == "" {
		return errors.New("environment id is required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	env, err := runtime.GetEnvironment(ctx, id)
	if err != nil {
		return err
	}
	if err := controlplane.ValidateEnvironmentPublishable(ctx, runtime, env); err != nil {
		return err
	}
	env.Verified = true
	env.Status = "verified"
	if env.LastVerifiedAt.IsZero() {
		env.LastVerifiedAt = time.Now().UTC()
	}
	env, err = runtime.UpsertEnvironment(ctx, env)
	if err != nil {
		return err
	}
	return printEnvironmentCommandResult(env, *jsonOutput)
}

func openRequiredCLIStore(ctx context.Context, storeRef string, legacyStoreURL string) (store.Store, func(), error) {
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(storeRef, legacyStoreURL)
	if err != nil {
		return nil, func() {}, err
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return nil, func() {}, err
	}
	return runtime, func() { _ = runtime.Close() }, nil
}

func loadEnvironmentForCLI(ctx context.Context, storeRef string, legacyStoreURL string, id string) (store.Environment, error) {
	runtime, cleanup, err := openRequiredCLIStore(ctx, storeRef, legacyStoreURL)
	if err != nil {
		return store.Environment{}, err
	}
	defer cleanup()
	return runtime.GetEnvironment(ctx, id)
}

func loadEnvironmentAndComponentGraphForCLI(ctx context.Context, storeRef string, legacyStoreURL string, id string) (store.Environment, store.EnvironmentComponentGraph, error) {
	runtime, cleanup, err := openRequiredCLIStore(ctx, storeRef, legacyStoreURL)
	if err != nil {
		return store.Environment{}, store.EnvironmentComponentGraph{}, err
	}
	defer cleanup()
	env, err := runtime.GetEnvironment(ctx, id)
	if err != nil {
		return store.Environment{}, store.EnvironmentComponentGraph{}, err
	}
	graph, err := runtime.GetEnvironmentComponentGraph(ctx, id)
	if err != nil {
		return store.Environment{}, store.EnvironmentComponentGraph{}, err
	}
	return env, graph, nil
}

func printEnvironmentCommandResult(env store.Environment, jsonOutput bool, componentGraphs ...store.EnvironmentComponentGraph) error {
	payload := map[string]any{"ok": true, "environment": environmentPayload(env)}
	if len(componentGraphs) > 0 {
		payload["componentGraph"] = environmentRestoreComponentGraphReport(env.ID, componentGraphs[0])
	}
	if jsonOutput {
		return writeIndentedJSON(payload)
	}
	fmt.Printf("Environment: %s\n", env.ID)
	fmt.Printf("Status: %s\n", env.Status)
	fmt.Printf("Verified: %t\n", env.Verified)
	if env.VerificationWorkflowID != "" {
		fmt.Printf("Verification Workflow: %s\n", env.VerificationWorkflowID)
	}
	if env.LastVerificationRunID != "" {
		fmt.Printf("Last Verification Run: %s [%s]\n", env.LastVerificationRunID, env.LastVerificationStatus)
	}
	fmt.Printf("Evidence Complete: %t\n", env.EvidenceComplete)
	fmt.Printf("SkyWalking Topology Complete: %t\n", env.TopologyComplete)
	if len(componentGraphs) > 0 {
		readiness := environmentRestoreComponentGraphReport(env.ID, componentGraphs[0])
		fmt.Printf("Component Restore-ready: %t\n", readiness.OK)
		if len(readiness.BlockingOrder) > 0 {
			fmt.Printf("Component Blocking Order: %s\n", strings.Join(readiness.BlockingOrder, " -> "))
		}
		if strings.TrimSpace(readiness.Error) != "" {
			fmt.Printf("Component Readiness Error: %s\n", readiness.Error)
		}
	}
	return nil
}

func environmentPayloads(items []store.Environment) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, environmentPayload(item))
	}
	return out
}

func environmentPayload(env store.Environment) map[string]any {
	payload := map[string]any{
		"id":                     env.ID,
		"displayName":            env.DisplayName,
		"description":            env.Description,
		"status":                 env.Status,
		"verified":               env.Verified,
		"services":               jsonArrayString(env.ServicesJSON),
		"repos":                  jsonObjectString(env.ReposJSON),
		"compose":                jsonObjectString(env.ComposeJSON),
		"healthChecks":           jsonArrayString(env.HealthChecksJSON),
		"verificationWorkflowId": env.VerificationWorkflowID,
		"lastVerificationRunId":  env.LastVerificationRunID,
		"lastVerificationStatus": env.LastVerificationStatus,
		"evidenceComplete":       env.EvidenceComplete,
		"topologyComplete":       env.TopologyComplete,
		"summary":                jsonObjectString(env.SummaryJSON),
		"createdAt":              env.CreatedAt,
		"updatedAt":              env.UpdatedAt,
	}
	if !env.LastVerifiedAt.IsZero() {
		payload["lastVerifiedAt"] = env.LastVerifiedAt
	}
	return payload
}

func environmentServices(services stringListFlag, repos stringListFlag, branches stringListFlag, repoRefs stringListFlag, checkouts stringListFlag) []map[string]any {
	repoByService := environmentKeyValueMap(repos)
	branchByService := environmentKeyValueMap(branches)
	refByService := environmentKeyValueMap(repoRefs)
	checkoutByService := environmentKeyValueMap(checkouts)
	ids := map[string]bool{}
	for _, id := range services.Values() {
		ids[id] = true
	}
	for id := range repoByService {
		ids[id] = true
	}
	for id := range branchByService {
		ids[id] = true
	}
	for id := range refByService {
		ids[id] = true
	}
	for id := range checkoutByService {
		ids[id] = true
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	out := make([]map[string]any, 0, len(ordered))
	for _, id := range ordered {
		item := map[string]any{"id": id}
		if repo := repoByService[id]; repo != "" {
			item["repo"] = repo
		}
		if branch := branchByService[id]; branch != "" {
			item["branch"] = branch
		}
		if ref := refByService[id]; ref != "" {
			item["ref"] = ref
		}
		if checkout := checkoutByService[id]; checkout != "" {
			item["checkout"] = checkout
		}
		out = append(out, item)
	}
	return out
}

func environmentRepoMap(repos stringListFlag, branches stringListFlag, repoRefs stringListFlag, checkouts stringListFlag) map[string]any {
	repoByService := environmentKeyValueMap(repos)
	branchByService := environmentKeyValueMap(branches)
	refByService := environmentKeyValueMap(repoRefs)
	checkoutByService := environmentKeyValueMap(checkouts)
	ids := map[string]bool{}
	for id := range repoByService {
		ids[id] = true
	}
	for id := range branchByService {
		ids[id] = true
	}
	for id := range refByService {
		ids[id] = true
	}
	for id := range checkoutByService {
		ids[id] = true
	}
	out := map[string]any{}
	for id := range ids {
		item := map[string]any{}
		if repo := repoByService[id]; repo != "" {
			item["url"] = repo
		}
		if branch := branchByService[id]; branch != "" {
			item["branch"] = branch
		}
		if ref := refByService[id]; ref != "" {
			item["ref"] = ref
		}
		if checkout := checkoutByService[id]; checkout != "" {
			item["checkout"] = checkout
		}
		out[id] = item
	}
	return out
}

func environmentComposeConfig(composeFiles stringListFlag, generatedFiles stringListFlag, startCommand string, projectName string, envFiles stringListFlag, envs stringListFlag, profiles stringListFlag, services stringListFlag, skipPull bool, skipBuild bool, packageRepo string, packageBranch string, packageRef string) (map[string]any, error) {
	files := composeFiles.Values()
	composeFile := ""
	if len(files) > 0 {
		composeFile = strings.TrimSpace(files[0])
	}
	out := map[string]any{
		"composeFile":  composeFile,
		"startCommand": strings.TrimSpace(startCommand),
	}
	if len(files) > 0 {
		out["composeFiles"] = files
	}
	generated, err := generatedFileContentMapFromFlags(generatedFiles)
	if err != nil {
		return nil, err
	}
	if len(generated) > 0 {
		out["generatedFiles"] = generated
	}
	if strings.TrimSpace(projectName) != "" {
		out["projectName"] = strings.TrimSpace(projectName)
	}
	if len(envFiles.Values()) > 0 {
		out["envFiles"] = envFiles.Values()
	}
	if values := keyValueMapFromFlags(envs); len(values) > 0 {
		out["env"] = values
	}
	if len(profiles.Values()) > 0 {
		out["profiles"] = profiles.Values()
	}
	if len(services.Values()) > 0 {
		out["services"] = services.Values()
	}
	if skipPull {
		out["skipPull"] = true
	}
	if skipBuild {
		out["skipBuild"] = true
	}
	packageConfig := map[string]string{}
	if strings.TrimSpace(packageRepo) != "" {
		packageConfig["url"] = strings.TrimSpace(packageRepo)
	}
	if strings.TrimSpace(packageBranch) != "" {
		packageConfig["branch"] = strings.TrimSpace(packageBranch)
	}
	if strings.TrimSpace(packageRef) != "" {
		packageConfig["ref"] = strings.TrimSpace(packageRef)
	}
	if len(packageConfig) > 0 {
		packageConfig["checkout"] = "."
		out["package"] = packageConfig
	}
	return out, nil
}

func generatedFileContentMapFromFlags(values stringListFlag) (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range values.Values() {
		target, source, ok := strings.Cut(raw, "=")
		target = strings.TrimSpace(target)
		source = strings.TrimSpace(source)
		if !ok || target == "" || source == "" {
			return nil, fmt.Errorf("generated compose file must be TARGET=SOURCE_FILE, got %q", raw)
		}
		if filepath.IsAbs(target) || target == "." || target == ".." || strings.HasPrefix(filepath.Clean(target), ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("generated compose file target must be relative to the restore workspace: %s", target)
		}
		content, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("read generated compose source %s: %w", source, err)
		}
		out[filepath.Clean(target)] = string(content)
	}
	return out, nil
}

func keyValueMapFromFlags(values stringListFlag) map[string]string {
	out := map[string]string{}
	for _, raw := range values.Values() {
		key, value, ok := strings.Cut(raw, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}

func environmentHealthChecks(urls stringListFlag, tcpAddresses stringListFlag, commands stringListFlag, composeServices stringListFlag) []map[string]any {
	out := make([]map[string]any, 0, len(urls.Values())+len(tcpAddresses.Values())+len(commands.Values())+len(composeServices.Values()))
	index := 1
	for _, url := range urls.Values() {
		out = append(out, map[string]any{"id": fmt.Sprintf("health-%02d", index), "kind": "url", "url": url})
		index++
	}
	for _, address := range tcpAddresses.Values() {
		out = append(out, map[string]any{"id": fmt.Sprintf("health-%02d", index), "kind": "tcp", "address": address})
		index++
	}
	for _, command := range commands.Values() {
		out = append(out, map[string]any{"id": fmt.Sprintf("health-%02d", index), "kind": "command", "command": command})
		index++
	}
	for _, service := range composeServices.Values() {
		out = append(out, map[string]any{"id": fmt.Sprintf("health-%02d", index), "kind": "compose-service", "service": service})
		index++
	}
	return out
}

func environmentRepoUpdateMap(repos stringListFlag, branches stringListFlag, repoRefs stringListFlag, checkouts stringListFlag) map[string]map[string]string {
	repoByService := environmentKeyValueMap(repos)
	branchByService := environmentKeyValueMap(branches)
	refByService := environmentKeyValueMap(repoRefs)
	checkoutByService := environmentKeyValueMap(checkouts)
	updates := map[string]map[string]string{}
	add := func(serviceID, key, value string) {
		serviceID = strings.TrimSpace(serviceID)
		if serviceID == "" {
			return
		}
		if _, ok := updates[serviceID]; !ok {
			updates[serviceID] = map[string]string{}
		}
		updates[serviceID][key] = value
	}
	for serviceID, value := range repoByService {
		add(serviceID, "url", value)
	}
	for serviceID, value := range branchByService {
		add(serviceID, "branch", value)
	}
	for serviceID, value := range refByService {
		add(serviceID, "ref", value)
	}
	for serviceID, value := range checkoutByService {
		add(serviceID, "checkout", value)
	}
	return updates
}

func environmentServicesWithRepoUpdates(existing []any, updates map[string]map[string]string) []any {
	out := make([]any, 0, len(existing)+len(updates))
	seen := map[string]bool{}
	for _, raw := range existing {
		item := jsonObjectFromAny(raw)
		serviceID := strings.TrimSpace(valueString(item["id"]))
		if serviceID == "" {
			continue
		}
		if update, ok := updates[serviceID]; ok {
			applyEnvironmentServiceRepoUpdate(item, update)
		}
		seen[serviceID] = true
		out = append(out, item)
	}
	for _, serviceID := range sortedMapKeys(updates) {
		if seen[serviceID] {
			continue
		}
		item := map[string]any{"id": serviceID}
		applyEnvironmentServiceRepoUpdate(item, updates[serviceID])
		out = append(out, item)
	}
	return out
}

func applyEnvironmentServiceRepoUpdate(item map[string]any, update map[string]string) {
	keyMap := map[string]string{
		"url":      "repo",
		"branch":   "branch",
		"ref":      "ref",
		"checkout": "checkout",
	}
	for repoKey, serviceKey := range keyMap {
		value, ok := update[repoKey]
		if !ok {
			continue
		}
		if strings.TrimSpace(value) == "" {
			delete(item, serviceKey)
			continue
		}
		item[serviceKey] = value
	}
}

func jsonObjectFromAny(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func sortedMapKeys[V any](items map[string]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func environmentKeyValueMap(values stringListFlag) map[string]string {
	out := map[string]string{}
	for _, value := range values.Values() {
		key, raw, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		raw = strings.TrimSpace(raw)
		if !ok || key == "" || raw == "" {
			continue
		}
		out[key] = raw
	}
	return out
}

func mustCompactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func jsonObjectString(raw string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(stringDefault(raw, "{}")), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func jsonArrayString(raw string) []any {
	var out []any
	if err := json.Unmarshal([]byte(stringDefault(raw, "[]")), &out); err != nil || out == nil {
		return []any{}
	}
	return out
}

func stringDefault(value string, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func printStoreStatus(status sqlite.SchemaStatusResult) {
	pending := status.TargetVersion - status.CurrentVersion
	if pending < 0 {
		pending = 0
	}
	fmt.Println("Store: sqlite")
	fmt.Printf("Path: %s\n", status.Path)
	fmt.Printf("Version: %d\n", status.CurrentVersion)
	fmt.Printf("Target: %d\n", status.TargetVersion)
	fmt.Printf("Pending: %d\n", pending)
}

type storeStatusReport struct {
	OK             bool   `json:"ok"`
	Backend        string `json:"backend"`
	URL            string `json:"url,omitempty"`
	Path           string `json:"path,omitempty"`
	CurrentVersion int    `json:"currentVersion"`
	TargetVersion  int    `json:"targetVersion"`
	Pending        int    `json:"pending"`
	Error          string `json:"error,omitempty"`
}

func sqliteStoreStatusReport(status sqlite.SchemaStatusResult) storeStatusReport {
	return storeStatusReport{
		OK:             true,
		Backend:        "sqlite",
		Path:           status.Path,
		CurrentVersion: status.CurrentVersion,
		TargetVersion:  status.TargetVersion,
		Pending:        pendingStoreSchemaVersions(status.CurrentVersion, status.TargetVersion),
	}
}

func sqliteStoreStatusErrorReport(cfg sqlite.Config, statusErr error) storeStatusReport {
	return storeStatusReport{
		OK:      false,
		Backend: "sqlite",
		Path:    cfg.Resolve().Path,
		Error:   statusErr.Error(),
	}
}

func postgresStoreStatusReport(status postgres.SchemaStatusResult) storeStatusReport {
	return storeStatusReport{
		OK:             true,
		Backend:        "postgres",
		URL:            maskStoreURL(status.URL),
		CurrentVersion: status.CurrentVersion,
		TargetVersion:  status.TargetVersion,
		Pending:        pendingStoreSchemaVersions(status.CurrentVersion, status.TargetVersion),
	}
}

func postgresStoreStatusErrorReport(storeURL string, statusErr error) storeStatusReport {
	return storeStatusReport{
		OK:            false,
		Backend:       "postgres",
		URL:           maskStoreURL(storeURL),
		TargetVersion: sqlstore.CurrentSchemaVersion,
		Pending:       sqlstore.CurrentSchemaVersion,
		Error:         statusErr.Error(),
	}
}

func mysqlStoreStatusReport(status mysql.SchemaStatusResult) storeStatusReport {
	return storeStatusReport{
		OK:             true,
		Backend:        "mysql",
		URL:            maskStoreURL(status.URL),
		CurrentVersion: status.CurrentVersion,
		TargetVersion:  status.TargetVersion,
		Pending:        pendingStoreSchemaVersions(status.CurrentVersion, status.TargetVersion),
	}
}

func mysqlStoreStatusErrorReport(storeURL string, statusErr error) storeStatusReport {
	return storeStatusReport{
		OK:            false,
		Backend:       "mysql",
		URL:           maskStoreURL(storeURL),
		TargetVersion: sqlstore.CurrentSchemaVersion,
		Pending:       sqlstore.CurrentSchemaVersion,
		Error:         statusErr.Error(),
	}
}

func pendingStoreSchemaVersions(current int, target int) int {
	pending := target - current
	if pending < 0 {
		return 0
	}
	return pending
}

func printPostgresStoreStatus(status postgres.SchemaStatusResult) {
	pending := status.TargetVersion - status.CurrentVersion
	if pending < 0 {
		pending = 0
	}
	fmt.Println("Store: postgres")
	fmt.Printf("URL: %s\n", maskStoreURL(status.URL))
	fmt.Printf("Version: %d\n", status.CurrentVersion)
	fmt.Printf("Target: %d\n", status.TargetVersion)
	fmt.Printf("Pending: %d\n", pending)
}

func printMySQLStoreStatus(status mysql.SchemaStatusResult) {
	pending := status.TargetVersion - status.CurrentVersion
	if pending < 0 {
		pending = 0
	}
	fmt.Println("Store: mysql")
	fmt.Printf("URL: %s\n", maskStoreURL(status.URL))
	fmt.Printf("Version: %d\n", status.CurrentVersion)
	fmt.Printf("Target: %d\n", status.TargetVersion)
	fmt.Printf("Pending: %d\n", pending)
}

type sandboxStartReport struct {
	OK        bool                        `json:"ok"`
	StorePath string                      `json:"storePath"`
	Services  []sandboxStartServiceResult `json:"services"`
	Counts    sandboxStartReportCounts    `json:"counts"`
}

type sandboxStartReportCounts struct {
	Total   int `json:"total"`
	Started int `json:"started"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

type sandboxStartServiceResult struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	Kind           string `json:"kind"`
	ContainerName  string `json:"containerName,omitempty"`
	ServicePort    int    `json:"servicePort,omitempty"`
	ManagementPort int    `json:"managementPort,omitempty"`
	Command        string `json:"command,omitempty"`
	Skipped        bool   `json:"skipped"`
	SkipReason     string `json:"skipReason,omitempty"`
	ExitCode       int    `json:"exitCode"`
	Output         string `json:"output,omitempty"`
	Error          string `json:"error,omitempty"`
}

func runSandbox(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing sandbox command")
	}
	switch args[0] {
	case "start":
		return runSandboxStart(ctx, args[1:])
	case "service":
		return runSandboxService(ctx, args[1:])
	case "interface":
		return runSandboxInterface(ctx, args[1:])
	default:
		return fmt.Errorf("unknown sandbox command: %s", args[0])
	}
}

func runSandboxService(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing sandbox service command")
	}
	switch args[0] {
	case "register":
		return runSandboxServiceRegister(ctx, args[1:])
	default:
		return fmt.Errorf("unknown sandbox service command: %s", args[0])
	}
}

func runSandboxInterface(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing sandbox interface command")
	}
	switch args[0] {
	case "register":
		return runSandboxInterfaceRegister(ctx, args[1:])
	default:
		return fmt.Errorf("unknown sandbox interface command: %s", args[0])
	}
}

func runSandboxServiceRegister(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("sandbox service register", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	id := flags.String("id", "", "Service id")
	displayName := flags.String("display-name", "", "Service display name")
	kind := flags.String("kind", "", "Service kind")
	servicePort := flags.Int("service-port", 0, "Service port")
	managementPort := flags.Int("management-port", 0, "Management port")
	startupCommand := flags.String("startup-command", "", "Startup command")
	healthURL := flags.String("health-url", "", "Health URL")
	status := flags.String("status", "", "Service status")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer runtime.Close()
	response, err := controlplane.RegisterSandboxService(ctx, runtime, controlplane.SandboxServiceRegistrationRequest{
		ID:             *id,
		DisplayName:    *displayName,
		Kind:           *kind,
		ServicePort:    *servicePort,
		ManagementPort: *managementPort,
		StartupCommand: *startupCommand,
		HealthURL:      *healthURL,
		Status:         *status,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(response)
	}
	fmt.Printf("Registered service: %s\n", response.Service.ID)
	fmt.Printf("Store: %s\n", response.StoreID)
	fmt.Printf("Kind: %s\n", response.Service.Kind)
	if response.Service.ServicePort > 0 {
		fmt.Printf("Port: %d\n", response.Service.ServicePort)
	}
	return nil
}

func runSandboxInterfaceRegister(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("sandbox interface register", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	id := flags.String("id", "", "Interface id")
	displayName := flags.String("display-name", "", "Interface display name")
	serviceID := flags.String("service-id", "", "Entry service id")
	operation := flags.String("operation", "", "Operation name")
	method := flags.String("method", "", "HTTP method")
	path := flags.String("path", "", "HTTP path")
	templateID := flags.String("template-id", "", "Request template id")
	caseID := flags.String("case-id", "", "API case id")
	caseTitle := flags.String("case-title", "", "API case title")
	requiredForAdmission := flags.Bool("required-for-admission", false, "Require this case for interface admission")
	timeoutMs := flags.Int("timeout-ms", 0, "Interface timeout in milliseconds")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Case timeout in seconds")
	status := flags.String("status", "", "Interface status")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer runtime.Close()
	response, err := controlplane.RegisterSandboxInterface(ctx, runtime, controlplane.SandboxInterfaceRegistrationRequest{
		ID:          *id,
		DisplayName: *displayName,
		ServiceID:   *serviceID,
		Operation:   *operation,
		Method:      *method,
		Path:        *path,
		TemplateID:  *templateID,
		TimeoutMs:   *timeoutMs,
		Status:      *status,
		Case: controlplane.SandboxInterfaceCase{
			ID:                   *caseID,
			DisplayName:          *caseTitle,
			RequiredForAdmission: *requiredForAdmission,
			TimeoutSeconds:       *timeoutSeconds,
		},
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(response)
	}
	fmt.Printf("Registered interface: %s\n", response.Interface.ID)
	fmt.Printf("Store: %s\n", response.StoreID)
	fmt.Printf("Service: %s\n", response.Interface.ServiceID)
	fmt.Printf("Case: %s\n", response.Interface.CaseID)
	return nil
}

func runSandboxStart(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("sandbox start", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	serviceID := flags.String("service", "", "Only start one registered service")
	serviceKind := flags.String("kind", "", "Only start services of this kind; default includes all kinds")
	timeoutSeconds := flags.Int("timeout-seconds", 300, "Per-service startup command timeout")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *timeoutSeconds <= 0 {
		return errors.New("--timeout-seconds must be greater than 0")
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer runtime.Close()
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return err
	}
	report := sandboxStartReport{
		OK:        true,
		StorePath: maskStoreURL(resolvedStoreURL),
	}
	kindFilter := strings.TrimSpace(*serviceKind)
	for _, service := range catalog.Services {
		if strings.TrimSpace(*serviceID) != "" && service.ID != strings.TrimSpace(*serviceID) {
			continue
		}
		if kindFilter != "" && strings.TrimSpace(service.Kind) != kindFilter {
			continue
		}
		result := runSandboxServiceStartup(ctx, service, time.Duration(*timeoutSeconds)*time.Second)
		report.Services = append(report.Services, result)
		report.Counts.Total++
		switch {
		case result.Skipped:
			report.Counts.Skipped++
		case result.ExitCode == 0:
			report.Counts.Started++
		default:
			report.Counts.Failed++
			report.OK = false
		}
	}
	if strings.TrimSpace(*serviceID) != "" && report.Counts.Total == 0 {
		return fmt.Errorf("registered service not found: %s", strings.TrimSpace(*serviceID))
	}
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
	} else {
		printSandboxStartReport(report)
	}
	if !report.OK {
		return errors.New("one or more sandbox services failed to start")
	}
	return nil
}

func runSandboxServiceStartup(ctx context.Context, service store.CatalogService, timeout time.Duration) sandboxStartServiceResult {
	command := strings.TrimSpace(service.StartupCommand)
	result := sandboxStartServiceResult{
		ID:             service.ID,
		DisplayName:    service.DisplayName,
		Kind:           service.Kind,
		ContainerName:  service.ContainerName,
		ServicePort:    service.ServicePort,
		ManagementPort: service.ManagementPort,
		Command:        command,
		ExitCode:       0,
	}
	if strings.TrimSpace(service.Status) != "" && strings.TrimSpace(service.Status) != "active" {
		result.Skipped = true
		result.SkipReason = "service is not active"
		return result
	}
	if command == "" {
		result.Skipped = true
		result.SkipReason = "startup command is empty"
		return result
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "/bin/sh", "-c", command)
	output, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(output))
	if commandCtx.Err() == context.DeadlineExceeded {
		result.ExitCode = 124
		result.Error = "startup command timed out"
		return result
	}
	if err != nil {
		result.ExitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		}
		result.Error = err.Error()
	}
	return result
}

func printSandboxStartReport(report sandboxStartReport) {
	fmt.Println("Sandbox Start")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Store: %s\n", report.StorePath)
	fmt.Printf("Total: %d Started: %d Skipped: %d Failed: %d\n", report.Counts.Total, report.Counts.Started, report.Counts.Skipped, report.Counts.Failed)
	for _, service := range report.Services {
		state := "started"
		if service.Skipped {
			state = "skipped"
		}
		if service.ExitCode != 0 {
			state = "failed"
		}
		fmt.Printf("- %s [%s]\n", service.ID, state)
		if service.Command != "" {
			fmt.Printf("  command: %s\n", service.Command)
		}
		if service.SkipReason != "" {
			fmt.Printf("  reason: %s\n", service.SkipReason)
		}
		if service.Error != "" {
			fmt.Printf("  error: %s\n", service.Error)
		}
		if service.Output != "" {
			fmt.Printf("  output: %s\n", service.Output)
		}
	}
}

func runProfile(args []string) error {
	if len(args) == 0 {
		return errors.New("missing profile command")
	}

	switch args[0] {
	case "init":
		return runProfileInit(args[1:])
	case "install":
		return runProfileInstall(args[1:])
	case "pack":
		return runProfilePack(args[1:])
	case "list":
		return runProfileList(args[1:])
	case "inspect":
		return runProfileInspect(args[1:])
	case "export":
		return runProfileExport(context.Background(), args[1:])
	case "audit":
		return runProfileAudit(context.Background(), args[1:])
	case "audit-plan":
		return runProfileAuditPlan(context.Background(), args[1:])
	case "doctor":
		return runProfileDoctor(args[1:])
	case "repair":
		return runProfileRepair(args[1:])
	case "generation-plan":
		return runProfileGenerationPlan(args[1:])
	case "import-plan":
		return runProfileImportPlan(args[1:])
	case "catalog-index":
		return runProfileCatalogIndex(context.Background(), args[1:])
	case "import":
		return runProfileImport(context.Background(), args[1:])
	case "verify":
		return runProfileVerify(context.Background(), args[1:])
	default:
		return fmt.Errorf("unknown profile command: %s", args[0])
	}
}

func runTemplatePackage(args []string) error {
	if len(args) == 0 {
		return errors.New("missing template-package command")
	}
	if err := runProfile(args); err != nil {
		if strings.HasPrefix(err.Error(), "unknown profile command:") {
			return fmt.Errorf("unknown template-package command: %s", args[0])
		}
		return err
	}
	return nil
}

func runConfig(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing config command")
	}
	switch args[0] {
	case "publish", "apply":
		return runConfigPublish(ctx, args[1:])
	default:
		return fmt.Errorf("unknown config command: %s", args[0])
	}
}

func runExecutor(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing executor command")
	}
	switch args[0] {
	case "plan":
		return runExecutorPlan(ctx, args[1:])
	default:
		return fmt.Errorf("unknown executor command: %s", args[0])
	}
}

func runExecutorPlan(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("executor plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := executorPlanReport(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printExecutorPlan(report)
	return nil
}

func executorPlanReport(ctx context.Context, profileRef string, profileHomeRef string, storeRef string, legacyStoreURL string) (executor.PlanReport, error) {
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(storeRef, legacyStoreURL)
	if err != nil {
		return executor.PlanReport{}, err
	}
	if strings.TrimSpace(profileRef) != "" {
		resolvedProfilePath, err := materializeProfileReference(profileRef, profileHomeRef, false)
		if err != nil {
			return executor.PlanReport{}, err
		}
		bundle, err := profile.Load(resolvedProfilePath)
		if err != nil {
			return executor.PlanReport{}, err
		}
		return executor.Plan(ctx, bundle), nil
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return executor.PlanReport{}, err
	}
	defer runtime.Close()
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return executor.PlanReport{}, err
	}
	return executor.PlanFromCatalog(ctx, catalog), nil
}

func printExecutorPlan(report executor.PlanReport) {
	fmt.Println("Executor Plan")
	fmt.Printf("Profile: %s\n", report.ProfileID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked)
	for _, item := range report.Items {
		state := "blocked"
		if item.Ready {
			state = "ready"
		}
		fmt.Printf("- %s [%s] %s\n", item.ID, item.Kind, state)
		if item.SourcePath != "" {
			fmt.Printf("  source: %s\n", item.SourcePath)
		}
		if item.Command != "" {
			fmt.Printf("  command: %s\n", item.Command)
		}
		if len(item.Issues) > 0 {
			fmt.Printf("  issues: %s\n", strings.Join(item.Issues, ","))
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runTrace(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing trace command")
	}
	switch args[0] {
	case "topology":
		return runTraceTopology(ctx, args[1:])
	default:
		return fmt.Errorf("unknown trace command: %s", args[0])
	}
}

func runTraceTopology(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing trace topology command")
	}
	switch args[0] {
	case "collect":
		return runTraceTopologyCollect(ctx, args[1:])
	default:
		return fmt.Errorf("unknown trace topology command: %s", args[0])
	}
}

func runTraceTopologyCollect(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("trace topology collect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	graphQLURL := flags.String("trace-graphql-url", os.Getenv("AGENT_TESTBENCH_TRACE_GRAPHQL_URL"), "Trace provider GraphQL URL")
	runID := flags.String("run", "", "Workflow run id")
	stepID := flags.String("step", "", "Workflow step id")
	caseID := flags.String("case", "", "API case id")
	requestID := flags.String("request", "", "Request id")
	endpoint := flags.String("endpoint", "", "Trace endpoint")
	traceID := flags.String("trace-id", "", "Trace id")
	startedAt := flags.String("started-at", "", "Run started timestamp")
	finishedAt := flags.String("finished-at", "", "Run finished timestamp")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runID) == "" {
		return errors.New("--run is required")
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer runtime.Close()
	payload := map[string]any{
		"runId":      *runID,
		"stepId":     *stepID,
		"caseId":     *caseID,
		"requestId":  *requestID,
		"endpoint":   *endpoint,
		"traceId":    *traceID,
		"startedAt":  *startedAt,
		"finishedAt": *finishedAt,
	}
	response, err := controlplane.CollectTraceTopologyPayload(ctx, runtime, controlplane.TraceCollector{GraphQLURL: *graphQLURL}, payload)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(response)
	}
	printTraceTopologyCollect(response)
	return nil
}

func printTraceTopologyCollect(response map[string]any) {
	row := mapFromReportAny(response["traceTopology"])
	topology := mapFromReportAny(response["topology"])
	fmt.Println("Trace Topology Collect")
	fmt.Printf("Run: %s\n", valueString(row["workflowRunId"]))
	fmt.Printf("Trace: %s\n", valueString(row["traceId"]))
	fmt.Printf("Status: %s\n", valueString(row["status"]))
	fmt.Printf("Spans: %s\n", valueString(topology["spanCount"]))
	if edges, ok := topology["confirmedEdges"].([]any); ok {
		fmt.Printf("Confirmed Edges: %d\n", len(edges))
	}
}

func runReplay(args []string) error {
	if len(args) == 0 {
		return errors.New("missing replay command")
	}
	switch args[0] {
	case "evidence":
		return runReplayEvidence(args[1:])
	default:
		return fmt.Errorf("unknown replay command: %s", args[0])
	}
}

func runReplayEvidence(args []string) error {
	flags := flag.NewFlagSet("replay evidence", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	traceID := flags.String("trace-id", "", "Trace id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	payload, err := controlplane.ReplayEvidencePayload(*traceID)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	run := mapFromReportAny(payload["run"])
	evidence := mapFromReportAny(payload["evidence"])
	fmt.Println("Replay Evidence")
	fmt.Printf("Trace: %s\n", valueString(run["traceId"]))
	if systems, ok := evidence["systems"].([]map[string]any); ok {
		fmt.Printf("Systems: %d\n", len(systems))
		return nil
	}
	if systems, ok := evidence["systems"].([]any); ok {
		fmt.Printf("Systems: %d\n", len(systems))
		return nil
	}
	fmt.Println("Systems: 0")
	return nil
}

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
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create generation plan output directory: %w", err)
	}
	written := []string{"generation-plan.json"}
	for _, item := range []profile.Service{report.Plan.Service} {
		relative := filepath.Join("services", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range report.Plan.InterfaceNodes {
		relative := filepath.Join("interface-nodes", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range report.Plan.APICases {
		relative := filepath.Join("cases", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range report.Plan.CaseFiles {
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
	flags := flag.NewFlagSet("profile import-plan openapi", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	sourcePath := flags.String("from", "", "OpenAPI JSON document path")
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
		return fmt.Errorf("read openapi document: %w", err)
	}
	plan, err := profileimportopenapi.Plan(raw, profileimportopenapi.Options{
		ServiceID:   *serviceID,
		EvidenceDir: *evidenceDir,
	})
	if err != nil {
		return err
	}
	report := profileImportPlanReport{
		Kind:       "openapi",
		SourcePath: *sourcePath,
		Plan:       importPlanAssetsFromOpenAPI(plan),
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
	printProfileImportPlan("OpenAPI Import Plan", report)
	return nil
}

func runProfileHTTPCaptureImportPlan(args []string) error {
	flags := flag.NewFlagSet("profile import-plan http-capture", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	sourcePath := flags.String("from", "", "HTTP capture JSON document path")
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
		return fmt.Errorf("read http capture document: %w", err)
	}
	plan, err := profileimporthttpcapture.Plan(raw, profileimporthttpcapture.Options{
		ServiceID:   *serviceID,
		EvidenceDir: *evidenceDir,
	})
	if err != nil {
		return err
	}
	report := profileImportPlanReport{
		Kind:       "http-capture",
		SourcePath: *sourcePath,
		Plan:       importPlanAssetsFromHTTPCapture(plan),
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
	printProfileImportPlan("HTTP Capture Import Plan", report)
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
	files := make([]profileImportPlanCaseFile, 0, len(plan.CaseFiles))
	for _, item := range plan.CaseFiles {
		files = append(files, profileImportPlanCaseFile{Path: item.Path, Body: item.Body})
	}
	return profileImportPlanAssets{
		Service:          plan.Service,
		InterfaceNodes:   plan.InterfaceNodes,
		RequestTemplates: plan.RequestTemplates,
		APICases:         plan.APICases,
		CaseFiles:        files,
	}
}

func importPlanAssetsFromHTTPCapture(plan profileimporthttpcapture.PlanResult) profileImportPlanAssets {
	files := make([]profileImportPlanCaseFile, 0, len(plan.CaseFiles))
	for _, item := range plan.CaseFiles {
		files = append(files, profileImportPlanCaseFile{Path: item.Path, Body: item.Body})
	}
	return profileImportPlanAssets{
		Service:          plan.Service,
		InterfaceNodes:   plan.InterfaceNodes,
		RequestTemplates: plan.RequestTemplates,
		APICases:         plan.APICases,
		CaseFiles:        files,
	}
}

func writeProfileImportPlanOutput(outputDir string, report profileImportPlanReport) ([]string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create import plan output directory: %w", err)
	}
	written := []string{"import-plan.json"}
	for _, item := range []profile.Service{report.Plan.Service} {
		relative := filepath.Join("services", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range report.Plan.InterfaceNodes {
		relative := filepath.Join("interface-nodes", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range report.Plan.RequestTemplates {
		relative := filepath.Join("request-templates", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range report.Plan.APICases {
		relative := filepath.Join("cases", safeImportPlanFileName(item.ID)+".json")
		if err := writeImportPlanJSON(outputDir, relative, item); err != nil {
			return nil, err
		}
		written = append(written, filepath.ToSlash(relative))
	}
	for _, item := range report.Plan.CaseFiles {
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
	value = strings.TrimSpace(value)
	if value == "" {
		return "asset"
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
		return "asset"
	}
	return builder.String()
}

func runProfileInit(args []string) error {
	flags := flag.NewFlagSet("profile init", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	outputPath := flags.String("output", "", "External profile bundle output path")
	profileID := flags.String("id", "local", "Profile id")
	displayName := flags.String("display-name", "Local Profile", "Profile display name")
	force := flags.Bool("force", false, "Overwrite generated files when they already exist")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := initProfileBundle(*outputPath, *profileID, *displayName, *force)
	if err != nil {
		return err
	}
	fmt.Printf("Initialized external profile bundle: %s\n", report.ID)
	fmt.Printf("Path: %s\n", report.Path)
	fmt.Printf("Manifest: %s\n", filepath.Join(report.Path, "profile.json"))
	return nil
}

type profileInitReport struct {
	ID   string
	Path string
}

type profileExportReport struct {
	OK        bool                `json:"ok"`
	ProfileID string              `json:"profileId"`
	Output    string              `json:"output"`
	Counts    profileExportCounts `json:"counts"`
}

type profileExportCounts struct {
	Services         int `json:"services"`
	Workflows        int `json:"workflows"`
	InterfaceNodes   int `json:"interfaceNodes"`
	APICases         int `json:"apiCases"`
	RequestTemplates int `json:"requestTemplates"`
	CaseDependencies int `json:"caseDependencies"`
	WorkflowBindings int `json:"workflowBindings"`
	Fixtures         int `json:"fixtures"`
	TemplateConfigs  int `json:"templateConfigs"`
}

type profileInstallReport = profilehome.InstallReport

type profilePackReport = profilehome.PackReport

type profileListReport = profilehome.ListReport

type profileListItem = profilehome.ListItem

func initProfileBundle(outputPath string, profileID string, displayName string, force bool) (profileInitReport, error) {
	outputPath = strings.TrimSpace(outputPath)
	profileID = strings.TrimSpace(profileID)
	displayName = strings.TrimSpace(displayName)
	if outputPath == "" {
		return profileInitReport{}, errors.New("--output is required")
	}
	if profileID == "" {
		return profileInitReport{}, errors.New("--id must not be empty")
	}
	if displayName == "" {
		return profileInitReport{}, errors.New("--display-name must not be empty")
	}
	if isCoreProfilesPath(outputPath) {
		return profileInitReport{}, errors.New("profile bundles must be initialized outside this core repository")
	}
	if err := os.MkdirAll(outputPath, 0o755); err != nil {
		return profileInitReport{}, err
	}
	for _, dir := range []string{
		"services",
		"workflows",
		"interface-nodes",
		"cases",
		"executors",
		"request-templates",
		"case-dependencies",
		"workflow-bindings",
		"fixtures",
	} {
		if err := os.MkdirAll(filepath.Join(outputPath, dir), 0o755); err != nil {
			return profileInitReport{}, err
		}
	}
	manifestPath := filepath.Join(outputPath, "profile.json")
	if _, err := os.Stat(manifestPath); err == nil && !force {
		return profileInitReport{}, fmt.Errorf("%s already exists; pass --force to overwrite generated files", manifestPath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return profileInitReport{}, err
	}
	manifest := profile.Bundle{
		ID:               profileID,
		DisplayName:      displayName,
		Description:      "External profile bundle generated by AgentTestBench.",
		Services:         []profile.Service{},
		Workflows:        []profile.Workflow{},
		InterfaceNodes:   []profile.InterfaceNode{},
		APICases:         []profile.APICase{},
		RequestTemplates: []profile.RequestTemplate{},
		CaseDependencies: []profile.CaseDependency{},
		WorkflowBindings: []profile.WorkflowBinding{},
		Fixtures:         []profile.Fixture{},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return profileInitReport{}, err
	}
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o644); err != nil {
		return profileInitReport{}, err
	}
	readmePath := filepath.Join(outputPath, "README.md")
	if _, err := os.Stat(readmePath); errors.Is(err, os.ErrNotExist) || force {
		body := "# External Profile Bundle\n\nPublish this bundle into the selected SQL Store before serving it through AgentTestBench:\n\n```sh\nagent-testbench store use local-personal\nagent-testbench config publish --from . --store local-personal\nagent-testbench serve --profile . --store local-personal\n```\n"
		if err := os.WriteFile(readmePath, []byte(body), 0o644); err != nil {
			return profileInitReport{}, err
		}
	} else if err != nil {
		return profileInitReport{}, err
	}
	ignorePath := filepath.Join(outputPath, ".gitignore")
	if _, err := os.Stat(ignorePath); errors.Is(err, os.ErrNotExist) || force {
		body := ".runtime/\n*.sqlite\n*.sqlite-*\n*.db\n*.db-*\n*.log\n"
		if err := os.WriteFile(ignorePath, []byte(body), 0o644); err != nil {
			return profileInitReport{}, err
		}
	} else if err != nil {
		return profileInitReport{}, err
	}
	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return profileInitReport{}, err
	}
	return profileInitReport{ID: profileID, Path: absPath}, nil
}

func runProfileExport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile export", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	outputPath := flags.String("output", "", "Profile bundle output path")
	force := flags.Bool("force", false, "Overwrite generated profile manifest when it already exists")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	s, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer s.Close()
	report, err := exportProfileCatalogFromStore(ctx, s, *outputPath, *force)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Exported profile: %s\n", report.ProfileID)
	fmt.Printf("Path: %s\n", report.Output)
	fmt.Printf("Services: %d\n", report.Counts.Services)
	fmt.Printf("API Cases: %d\n", report.Counts.APICases)
	fmt.Printf("Template Configs: %d\n", report.Counts.TemplateConfigs)
	return nil
}

func exportProfileCatalogFromStore(ctx context.Context, s store.Store, outputPath string, force bool) (profileExportReport, error) {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return profileExportReport{}, errors.New("--output is required")
	}
	if isCoreProfilesPath(outputPath) {
		return profileExportReport{}, errors.New("profile bundles must be exported outside this core repository")
	}
	catalog, err := s.GetProfileCatalog(ctx)
	if err != nil {
		return profileExportReport{}, err
	}
	bundle := profilecatalog.ToBundle(catalog)
	if strings.TrimSpace(bundle.DisplayName) == "" {
		bundle.DisplayName = bundle.ID
	}
	if err := os.MkdirAll(outputPath, 0o755); err != nil {
		return profileExportReport{}, err
	}
	manifestPath := filepath.Join(outputPath, "profile.json")
	if _, err := os.Stat(manifestPath); err == nil && !force {
		return profileExportReport{}, fmt.Errorf("%s already exists; pass --force to overwrite generated files", manifestPath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return profileExportReport{}, err
	}
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return profileExportReport{}, err
	}
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o644); err != nil {
		return profileExportReport{}, fmt.Errorf("write profile manifest %s: %w", manifestPath, err)
	}
	if _, err := profile.Load(outputPath); err != nil {
		return profileExportReport{}, fmt.Errorf("exported profile is invalid: %w", err)
	}
	return profileExportReport{
		OK:        true,
		ProfileID: bundle.ID,
		Output:    outputPath,
		Counts:    profileExportAssetCounts(bundle),
	}, nil
}

func profileExportAssetCounts(bundle profile.Bundle) profileExportCounts {
	return profileExportCounts{
		Services:         len(bundle.Services),
		Workflows:        len(bundle.Workflows),
		InterfaceNodes:   len(bundle.InterfaceNodes),
		APICases:         len(bundle.APICases),
		RequestTemplates: len(bundle.RequestTemplates),
		CaseDependencies: len(bundle.CaseDependencies),
		WorkflowBindings: len(bundle.WorkflowBindings),
		Fixtures:         len(bundle.Fixtures),
		TemplateConfigs:  len(bundle.TemplateConfigs),
	}
}

func runProfileInstall(args []string) error {
	flags := flag.NewFlagSet("profile install", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	from := flags.String("from", "", "External profile bundle path")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	force := flags.Bool("force", false, "Replace an already installed profile")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := installProfileBundle(*from, *profileHome, *force)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Installed profile: %s\n", report.ID)
	fmt.Printf("Path: %s\n", report.TargetPath)
	fmt.Printf("Digest: %s\n", report.BundleDigest)
	return nil
}

func runProfilePack(args []string) error {
	flags := flag.NewFlagSet("profile pack", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profileRef := flags.String("profile", "", "Profile bundle path or installed profile id")
	templatePackageRef := flags.String("template-package", "", "Template package path or installed template package id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	outputPath := flags.String("output", "", "Archive output path")
	force := flags.Bool("force", false, "Replace an existing archive")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := packProfileBundle(templatePackageReference(*templatePackageRef, *profileRef), *profileHome, *outputPath, *force)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Packed profile: %s\n", report.ID)
	fmt.Printf("Archive: %s\n", report.OutputPath)
	fmt.Printf("Files: %d\n", report.FileCount)
	fmt.Printf("Digest: %s\n", report.BundleDigest)
	return nil
}

func runProfileList(args []string) error {
	flags := flag.NewFlagSet("profile list", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := listInstalledProfiles(*profileHome)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Profile Home: %s\n", report.ProfileHome)
	if len(report.Profiles) == 0 {
		fmt.Println("Profiles: 0")
		return nil
	}
	for _, item := range report.Profiles {
		if !item.Valid {
			fmt.Printf("- %s (invalid) %s: %s\n", item.ID, item.Path, item.Error)
			continue
		}
		fmt.Printf("- %s (%s) %s\n", item.ID, item.DisplayName, item.Path)
	}
	return nil
}

func installProfileBundle(from string, profileHome string, force bool) (profileInstallReport, error) {
	return profilehome.Install(from, profileHome, force)
}

func packProfileBundle(profileRef string, profileHome string, outputPath string, force bool) (profilePackReport, error) {
	return profilehome.Pack(profileRef, profileHome, outputPath, force)
}

func listInstalledProfiles(profileHome string) (profileListReport, error) {
	return profilehome.List(profileHome)
}

func resolveProfileHome(value string) (string, error) {
	return profilehome.ResolveHome(value)
}

func resolveProfileReference(value string, profileHome string) (string, error) {
	return profilehome.ResolveReference(value, profileHome)
}

func templatePackageReference(storeFirst string, legacy string) string {
	if value := strings.TrimSpace(storeFirst); value != "" {
		return value
	}
	return legacy
}

func materializeProfileReference(value string, profileHome string, force bool) (string, error) {
	resolved, err := resolveProfileReference(value, profileHome)
	if err != nil {
		return "", err
	}
	if !profilehome.IsArchivePath(resolved) {
		return resolved, nil
	}
	report, err := installProfileBundle(resolved, profileHome, force)
	if err != nil {
		return "", err
	}
	return report.TargetPath, nil
}

func isCoreProfilesPath(path string) bool {
	return profilehome.IsCoreProfilesPath(path)
}

func runProfileInspect(args []string) error {
	flags := flag.NewFlagSet("profile inspect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	templatePackagePath := flags.String("template-package", "", "Template package path or installed template package id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedProfilePath, err := resolveProfileReference(templatePackageReference(*templatePackagePath, *profilePath), *profileHome)
	if err != nil {
		return err
	}
	bundle, err := profile.Load(resolvedProfilePath)
	if err != nil {
		return err
	}
	printProfile(bundle)
	return nil
}

type profileDoctorReport struct {
	OK          bool                 `json:"ok"`
	ProfileID   string               `json:"profileId,omitempty"`
	ProfilePath string               `json:"profilePath"`
	CaseID      string               `json:"caseId"`
	Checks      []profileDoctorCheck `json:"checks"`
}

type profileDoctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func runProfileDoctor(args []string) error {
	flags := flag.NewFlagSet("profile doctor", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	templatePackagePath := flags.String("template-package", "", "Template package path or installed template package id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	caseID := flags.String("case-id", "", "API case id to inspect")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedProfilePath, err := resolveProfileReference(templatePackageReference(*templatePackagePath, *profilePath), *profileHome)
	if err != nil {
		return err
	}
	report := profileDoctor(resolvedProfilePath, *caseID)
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
		if !report.OK {
			return errors.New("profile doctor found issues")
		}
		return nil
	}
	printProfileDoctor(report)
	if !report.OK {
		return errors.New("profile doctor found issues")
	}
	return nil
}

func profileDoctor(profilePath string, caseID string) profileDoctorReport {
	report := profileDoctorReport{
		ProfilePath: profilePath,
		CaseID:      strings.TrimSpace(caseID),
		OK:          true,
	}
	if report.CaseID == "" {
		return appendProfileDoctorCheck(report, "case-id", false, "--case-id is required")
	}
	bundle, err := profile.Load(profilePath)
	if err != nil {
		return appendProfileDoctorCheck(report, "profile-load", false, err.Error())
	}
	report.ProfileID = bundle.ID
	report = appendProfileDoctorCheck(report, "profile-load", true, "profile loaded")
	apiCase, foundCase := findProfileAPICase(bundle.APICases, report.CaseID)
	report = appendProfileDoctorCheck(report, "case-catalog", foundCase, "case is present in loaded profile catalog")
	rawCatalogIDs := loadRawCatalogCaseIDs(bundle.BaseDir)
	if len(rawCatalogIDs) > 0 {
		report = appendProfileDoctorCheck(report, "catalog-json-entry", rawCatalogIDs[report.CaseID], "case is present in catalog.json interfaceNodeCases")
	}
	caseFile := profileCaseFilePath(bundle.BaseDir, apiCase)
	if report.CaseID != "" {
		_, err := os.Stat(caseFile)
		report = appendProfileDoctorCheck(report, "case-file", err == nil, caseFile)
	}
	if !foundCase {
		return report
	}
	if strings.TrimSpace(apiCase.NodeID) != "" {
		_, foundNode := findProfileInterfaceNode(bundle.InterfaceNodes, apiCase.NodeID)
		report = appendProfileDoctorCheck(report, "interface-node", foundNode, "node "+apiCase.NodeID+" exists")
	}
	if strings.TrimSpace(apiCase.RequestTemplateID) != "" {
		_, foundTemplate := findProfileRequestTemplate(bundle.RequestTemplates, apiCase.RequestTemplateID)
		report = appendProfileDoctorCheck(report, "request-template", foundTemplate, "template "+apiCase.RequestTemplateID+" exists")
	}
	for _, item := range bundle.CaseDependencies {
		if item.CaseID != apiCase.ID {
			continue
		}
		_, foundFixture := findProfileFixture(bundle.Fixtures, item.FixtureID)
		report = appendProfileDoctorCheck(report, "fixture:"+item.FixtureID, foundFixture, "dependency "+item.ID+" fixture exists")
	}
	if strings.TrimSpace(apiCase.PatchJSON) != "" {
		report = appendProfileDoctorCheck(report, "patch-json", validJSONObjectOrArray(apiCase.PatchJSON), "patchJson parses as JSON")
	}
	if strings.TrimSpace(apiCase.ExpectedJSON) != "" {
		report = appendProfileDoctorCheck(report, "expected-json", validJSONObjectOrArray(apiCase.ExpectedJSON), "expectedJson parses as JSON")
	}
	return report
}

func appendProfileDoctorCheck(report profileDoctorReport, name string, ok bool, detail string) profileDoctorReport {
	if !ok {
		report.OK = false
	}
	report.Checks = append(report.Checks, profileDoctorCheck{Name: name, OK: ok, Detail: detail})
	return report
}

func printProfileDoctor(report profileDoctorReport) {
	fmt.Println("Profile Doctor")
	fmt.Printf("Profile: %s\n", firstNonEmpty(report.ProfileID, report.ProfilePath))
	fmt.Printf("Case: %s\n", report.CaseID)
	fmt.Printf("OK: %t\n", report.OK)
	for _, check := range report.Checks {
		status := "ok"
		if !check.OK {
			status = "issue"
		}
		fmt.Printf("- %s [%s] %s\n", check.Name, status, check.Detail)
	}
}

type profileRepairReport struct {
	OK           bool                 `json:"ok"`
	Applied      bool                 `json:"applied"`
	ProfilePath  string               `json:"profilePath"`
	ManifestPath string               `json:"manifestPath"`
	Summary      profileRepairSummary `json:"summary"`
	Items        []profileRepairItem  `json:"items"`
	Warnings     []string             `json:"warnings,omitempty"`
}

type profileRepairSummary struct {
	CatalogCasesRestored int `json:"catalogCasesRestored"`
	CaseFilesRestored    int `json:"caseFilesRestored"`
	AlreadyPresent       int `json:"alreadyPresent"`
	ChangedFiles         int `json:"changedFiles"`
}

type profileRepairItem struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	Path   string `json:"path,omitempty"`
	Action string `json:"action"`
}

type profileRepairManifest struct {
	ProfilePath  string                     `json:"profilePath"`
	CatalogPath  string                     `json:"catalogPath"`
	CaseIDs      []string                   `json:"caseIds"`
	CatalogCases []json.RawMessage          `json:"catalogCases"`
	CaseFiles    map[string]string          `json:"caseFiles"`
	CaseFileJSON map[string]json.RawMessage `json:"caseFileJson"`
}

func runProfileRepair(args []string) error {
	flags := flag.NewFlagSet("profile repair", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	manifestPath := flags.String("from-manifest", "", "Repair manifest path")
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	apply := flags.Bool("apply", false, "Write repaired profile files")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := profileRepair(*manifestPath, *profilePath, *profileHome, *apply)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printProfileRepair(report)
	return nil
}

func profileRepair(manifestPath string, profileRef string, profileHome string, apply bool) (profileRepairReport, error) {
	manifestPath = strings.TrimSpace(manifestPath)
	if manifestPath == "" {
		return profileRepairReport{}, errors.New("--from-manifest is required")
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return profileRepairReport{}, fmt.Errorf("read repair manifest: %w", err)
	}
	var manifest profileRepairManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return profileRepairReport{}, fmt.Errorf("decode repair manifest: %w", err)
	}
	resolvedProfilePath := strings.TrimSpace(profileRef)
	if resolvedProfilePath != "" {
		resolvedProfilePath, err = resolveProfileReference(resolvedProfilePath, profileHome)
		if err != nil {
			return profileRepairReport{}, err
		}
	} else {
		resolvedProfilePath = strings.TrimSpace(manifest.ProfilePath)
	}
	if resolvedProfilePath == "" {
		return profileRepairReport{}, errors.New("profile repair needs --profile or manifest profilePath")
	}
	catalogPath := profileRepairCatalogPath(resolvedProfilePath, manifest)
	report := profileRepairReport{OK: true, Applied: apply, ProfilePath: resolvedProfilePath, ManifestPath: manifestPath}
	catalogRaw, err := os.ReadFile(catalogPath)
	if err != nil {
		return profileRepairReport{}, fmt.Errorf("read profile catalog: %w", err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(catalogRaw, &catalog); err != nil {
		return profileRepairReport{}, fmt.Errorf("decode profile catalog: %w", err)
	}
	cases := rawJSONListFromAny(catalog["interfaceNodeCases"])
	byID := map[string]json.RawMessage{}
	for _, rawCase := range cases {
		id := jsonID(rawCase)
		if id != "" {
			byID[id] = rawCase
		}
	}
	for _, rawCase := range manifest.CatalogCases {
		id := jsonID(rawCase)
		if id == "" {
			report.Warnings = append(report.Warnings, "skipped catalog case without id")
			continue
		}
		action := "already-present"
		if _, ok := byID[id]; !ok {
			cases = append(cases, rawCase)
			byID[id] = rawCase
			action = "restore"
			report.Summary.CatalogCasesRestored++
		} else {
			report.Summary.AlreadyPresent++
		}
		report.Items = append(report.Items, profileRepairItem{Kind: "catalog-case", ID: id, Action: action})
	}
	fileContents := profileRepairCaseFiles(manifest)
	for sourcePath, content := range fileContents {
		targetPath := profileRepairCaseFilePath(resolvedProfilePath, manifest.ProfilePath, sourcePath)
		action := "already-present"
		current, err := os.ReadFile(targetPath)
		if err != nil || string(current) != content {
			action = "restore"
			report.Summary.CaseFilesRestored++
			if apply {
				if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
					return profileRepairReport{}, err
				}
				if err := os.WriteFile(targetPath, []byte(ensureTrailingNewline(content)), 0o644); err != nil {
					return profileRepairReport{}, err
				}
			}
		} else {
			report.Summary.AlreadyPresent++
		}
		report.Items = append(report.Items, profileRepairItem{Kind: "case-file", Path: targetPath, Action: action})
	}
	if apply && report.Summary.CatalogCasesRestored > 0 {
		nextCases := make([]any, 0, len(cases))
		for _, rawCase := range cases {
			var value any
			if err := json.Unmarshal(rawCase, &value); err != nil {
				return profileRepairReport{}, err
			}
			nextCases = append(nextCases, value)
		}
		catalog["interfaceNodeCases"] = nextCases
		out, err := json.MarshalIndent(catalog, "", "  ")
		if err != nil {
			return profileRepairReport{}, err
		}
		if err := os.WriteFile(catalogPath, append(out, '\n'), 0o644); err != nil {
			return profileRepairReport{}, err
		}
	}
	if report.Summary.CatalogCasesRestored > 0 && apply {
		report.Summary.ChangedFiles++
	}
	if report.Summary.CaseFilesRestored > 0 && apply {
		report.Summary.ChangedFiles += report.Summary.CaseFilesRestored
	}
	return report, nil
}

func printProfileRepair(report profileRepairReport) {
	fmt.Println("Profile Repair")
	fmt.Printf("Profile: %s\n", report.ProfilePath)
	fmt.Printf("Applied: %t\n", report.Applied)
	fmt.Printf("Catalog Cases Restored: %d\n", report.Summary.CatalogCasesRestored)
	fmt.Printf("Case Files Restored: %d\n", report.Summary.CaseFilesRestored)
	for _, item := range report.Items {
		target := firstNonEmpty(item.ID, item.Path)
		fmt.Printf("- %s %s: %s\n", item.Kind, target, item.Action)
	}
}

func findProfileAPICase(items []profile.APICase, id string) (profile.APICase, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return profile.APICase{}, false
}

func findProfileInterfaceNode(items []profile.InterfaceNode, id string) (profile.InterfaceNode, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return profile.InterfaceNode{}, false
}

func findProfileRequestTemplate(items []profile.RequestTemplate, id string) (profile.RequestTemplate, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return profile.RequestTemplate{}, false
}

func findProfileFixture(items []profile.Fixture, id string) (profile.Fixture, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return profile.Fixture{}, false
}

func profileCaseFilePath(baseDir string, apiCase profile.APICase) string {
	if strings.TrimSpace(apiCase.CasePath) != "" {
		if filepath.IsAbs(apiCase.CasePath) {
			return apiCase.CasePath
		}
		return filepath.Join(baseDir, apiCase.CasePath)
	}
	return filepath.Join(baseDir, "cases", apiCase.ID+".json")
}

func loadRawCatalogCaseIDs(baseDir string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(baseDir, "catalog.json"))
	if err != nil {
		return out
	}
	var payload struct {
		InterfaceNodeCases []json.RawMessage `json:"interfaceNodeCases"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return out
	}
	for _, item := range payload.InterfaceNodeCases {
		if id := jsonID(item); id != "" {
			out[id] = true
		}
	}
	return out
}

func validJSONObjectOrArray(value string) bool {
	var parsed any
	if json.Unmarshal([]byte(value), &parsed) != nil {
		return false
	}
	switch parsed.(type) {
	case map[string]any, []any:
		return true
	default:
		return false
	}
}

func profileRepairCatalogPath(profilePath string, manifest profileRepairManifest) string {
	if strings.TrimSpace(manifest.CatalogPath) != "" {
		if filepath.IsAbs(manifest.CatalogPath) {
			return manifest.CatalogPath
		}
		if strings.TrimSpace(manifest.ProfilePath) != "" {
			if rel, err := filepath.Rel(manifest.ProfilePath, manifest.CatalogPath); err == nil && !strings.HasPrefix(rel, "..") {
				return filepath.Join(profilePath, rel)
			}
		}
		return manifest.CatalogPath
	}
	return filepath.Join(profilePath, "catalog.json")
}

func rawJSONListFromAny(value any) []json.RawMessage {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]json.RawMessage, 0, len(values))
	for _, item := range values {
		raw, err := json.Marshal(item)
		if err == nil {
			out = append(out, raw)
		}
	}
	return out
}

func jsonID(raw json.RawMessage) string {
	var payload struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.ID)
}

func profileRepairCaseFiles(manifest profileRepairManifest) map[string]string {
	out := map[string]string{}
	for path, content := range manifest.CaseFiles {
		out[path] = content
	}
	for path, raw := range manifest.CaseFileJSON {
		out[path] = string(raw)
	}
	return out
}

func profileRepairCaseFilePath(profilePath string, manifestProfilePath string, sourcePath string) string {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return filepath.Join(profilePath, "cases", "case.json")
	}
	if strings.TrimSpace(manifestProfilePath) != "" {
		if rel, err := filepath.Rel(manifestProfilePath, sourcePath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.Join(profilePath, rel)
		}
	}
	if filepath.IsAbs(sourcePath) {
		return sourcePath
	}
	return filepath.Join(profilePath, sourcePath)
}

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}

func runProfileCatalogIndex(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile catalog-index", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	report, err := readProfileCatalogIndex(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printProfileCatalogIndex(report)
	return nil
}

func runProfileVerify(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	templatePackagePath := flags.String("template-package", "", "Template package path or installed template package id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	requireCaseRuns := flags.Bool("require-case-runs", false, "Require every API Case in the profile to have a latest passed Store run")
	requireWorkflowRuns := flags.Bool("require-workflow-runs", false, "Require every Workflow in the profile to have a latest passed Store run")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	force := flags.Bool("force", false, "Replace an installed profile when --profile points to a packed archive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	s, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer s.Close()
	resolvedProfilePath, err := materializeProfileReference(templatePackageReference(*templatePackagePath, *profilePath), *profileHome, *force)
	if err != nil {
		return err
	}
	report, err := verifyProfileBundle(ctx, s, resolvedProfilePath, maskStoreURL(resolvedStoreURL), profileVerifyOptions{
		RequireCaseRuns:     *requireCaseRuns,
		RequireWorkflowRuns: *requireWorkflowRuns,
	})
	if err != nil {
		if *jsonOutput && report.ProfileID != "" {
			if report.Error == "" {
				report.Error = err.Error()
			}
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			if encodeErr := encoder.Encode(report); encodeErr != nil {
				return encodeErr
			}
		}
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printProfileVerify(report)
	return nil
}

func runProfileImport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile import", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return runConfigPublishWithFlags(ctx, flags, args, "Imported profile")
}

func runConfigPublish(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("config publish", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return runConfigPublishWithFlags(ctx, flags, args, "Published config")
}

func runConfigPublishWithFlags(ctx context.Context, flags *flag.FlagSet, args []string, textPrefix string) error {
	from := flags.String("from", "", "Profile bundle path")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	auditOutput := flags.Bool("audit", false, "Run profile audit after import")
	requireAuditOK := flags.Bool("require-audit-ok", false, "Fail before writing the Store unless profile audit has no issues")
	force := flags.Bool("force", false, "Replace an installed profile when --from points to a packed archive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	s, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer s.Close()

	resolvedFrom, err := materializeProfileReference(*from, *profileHome, *force)
	if err != nil {
		return err
	}
	report, err := publishProfileBundleToStore(ctx, s, resolvedFrom, maskStoreURL(resolvedStoreURL), *auditOutput, *requireAuditOK)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Printf("%s: %s\n", textPrefix, report.ProfileID)
	fmt.Printf("Digest: %s\n", report.BundleDigest)
	printProfileImportDiff(report.Diff)
	if report.Audit != nil {
		printProfileImportAudit(*report.Audit)
	}
	return nil
}

func publishProfileBundleToStore(ctx context.Context, s store.Store, from string, storePath string, auditOutput bool, requireAuditOK bool) (profileImportReport, error) {
	bundle, err := profile.Load(from)
	if err != nil {
		return profileImportReport{}, err
	}
	if requireAuditOK {
		auditReport, err := profileaudit.Audit(ctx, profileaudit.Options{
			Bundle:     bundle,
			BundlePath: from,
		})
		if err != nil {
			return profileImportReport{}, err
		}
		if !auditReport.OK {
			return profileImportReport{}, fmt.Errorf("profile audit failed for profile %q: %s", bundle.ID, profileaudit.FailureSummary(auditReport))
		}
	}
	digest, err := profile.BundleDigest(from)
	if err != nil {
		return profileImportReport{}, err
	}
	summary, err := json.Marshal(bundle.Counts())
	if err != nil {
		return profileImportReport{}, err
	}
	importedAt := time.Now().UTC()
	if _, err := s.UpsertProfileIndex(ctx, store.ProfileIndex{
		ProfileID:    bundle.ID,
		BundlePath:   from,
		BundleDigest: digest,
		SummaryJSON:  string(summary),
		ImportedAt:   importedAt,
	}); err != nil {
		return profileImportReport{}, err
	}
	catalog := profilecatalog.FromBundle(bundle, importedAt)
	previousCatalog, hasPreviousCatalog, err := readCurrentProfileCatalog(ctx, s)
	if err != nil {
		return profileImportReport{}, err
	}
	if err := s.ReplaceProfileCatalog(ctx, catalog); err != nil {
		return profileImportReport{}, err
	}
	configVersion, err := s.UpsertConfigVersion(ctx, store.ConfigVersion{
		ID:           configVersionID(bundle.ID, importedAt),
		ProfileID:    bundle.ID,
		SourcePath:   from,
		BundleDigest: digest,
		SummaryJSON:  string(summary),
		Active:       true,
		PublishedAt:  importedAt,
		CreatedAt:    importedAt,
	})
	if err != nil {
		return profileImportReport{}, err
	}
	readModelKeys, err := controlplane.UpsertProfileReadModels(ctx, s, catalog, configVersion.ID, importedAt)
	if err != nil {
		return profileImportReport{}, err
	}
	catalogIndex, err := s.GetProfileCatalogIndex(ctx)
	if err != nil {
		return profileImportReport{}, err
	}
	report := profileImportReport{
		ProfileID:     bundle.ID,
		BundlePath:    from,
		BundleDigest:  digest,
		Counts:        profileImportAssetCounts(bundle.Counts()),
		Diff:          profileImportDiffFromCatalogs(previousCatalog, catalog, hasPreviousCatalog),
		StorePath:     storePath,
		CatalogIndex:  profileCatalogIndexFromStore(catalogIndex),
		ConfigVersion: profileConfigVersionFromStore(configVersion),
		ReadModels:    readModelKeys,
		ImportedAt:    importedAt,
	}
	if auditOutput {
		auditReport, err := profileaudit.Audit(ctx, profileaudit.Options{
			Bundle:     bundle,
			BundlePath: from,
			Store:      s,
		})
		if err != nil {
			return profileImportReport{}, err
		}
		report.Audit = &auditReport
	}
	return report, nil
}

func verifyProfileBundle(ctx context.Context, s store.Store, profilePath string, storePath string, options profileVerifyOptions) (profileVerifyReport, error) {
	bundle, err := profile.Load(profilePath)
	if err != nil {
		return profileVerifyReport{}, err
	}
	auditReport, err := profileaudit.Audit(ctx, profileaudit.Options{
		Bundle:     bundle,
		BundlePath: profilePath,
	})
	if err != nil {
		return profileVerifyReport{}, err
	}
	if !auditReport.OK {
		return profileVerifyReport{}, fmt.Errorf("profile audit failed for profile %q: %s", bundle.ID, profileaudit.FailureSummary(auditReport))
	}
	publishReport, err := publishProfileBundleToStore(ctx, s, profilePath, storePath, true, true)
	if err != nil {
		return profileVerifyReport{}, err
	}
	checks, err := verifyPublishedProfile(ctx, s, bundle, publishReport, options)
	if err != nil {
		return profileVerifyReport{}, err
	}
	report := profileVerifyReport{
		OK:        profileChecksOK(checks),
		ProfileID: bundle.ID,
		Audit:     *publishReport.Audit,
		Publish:   publishReport,
		Summary:   summarizeProfileVerification(checks, options),
		Checks:    checks,
	}
	if !report.OK {
		report.Error = fmt.Sprintf("profile verification failed for profile %q: %s", bundle.ID, firstFailedProfileCheck(checks))
		return report, fmt.Errorf("profile verification failed for profile %q: %s", bundle.ID, firstFailedProfileCheck(checks))
	}
	return report, nil
}

func summarizeProfileVerification(checks []profileVerifyCheck, options profileVerifyOptions) profileVerifySummary {
	summary := profileVerifySummary{
		TotalChecks:          len(checks),
		RequiredCaseRuns:     options.RequireCaseRuns,
		RequiredWorkflowRuns: options.RequireWorkflowRuns,
	}
	for _, check := range checks {
		if check.OK {
			summary.PassedChecks++
			continue
		}
		summary.FailedChecks++
		if summary.FirstFailed == "" {
			summary.FirstFailed = check.Name
		}
	}
	return summary
}

func verifyPublishedProfile(ctx context.Context, s store.Store, bundle profile.Bundle, report profileImportReport, options profileVerifyOptions) ([]profileVerifyCheck, error) {
	checks := make([]profileVerifyCheck, 0, 6)
	index, err := s.GetProfileIndex(ctx, report.ProfileID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			checks = appendProfileCheck(checks, "profile-index", false, "profile index was not written")
			return checks, nil
		}
		return nil, err
	}
	checks = appendProfileCheck(checks, "profile-index", index.BundleDigest == report.BundleDigest, "profile index digest matches published bundle")

	catalogIndex, err := s.GetProfileCatalogIndex(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			checks = appendProfileCheck(checks, "catalog-index", false, "catalog index was not written")
		} else {
			return nil, err
		}
	} else {
		checks = appendProfileCheck(checks, "catalog-index", catalogIndex.ProfileID == report.ProfileID, "catalog index points to active profile")
	}

	activeConfig, err := s.GetActiveConfigVersion(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			checks = appendProfileCheck(checks, "active-config", false, "active config version was not written")
		} else {
			return nil, err
		}
	} else {
		ok := activeConfig.ID == report.ConfigVersion.ID && activeConfig.ProfileID == report.ProfileID && activeConfig.BundleDigest == report.BundleDigest
		checks = appendProfileCheck(checks, "active-config", ok, "active config version matches published bundle")
	}

	for _, key := range []string{profilecatalog.ReadModelInterfaceNodes, controlplane.ReadModelCatalog, controlplane.ReadModelDashboard} {
		model, err := s.GetReadModel(ctx, report.ProfileID, key)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				checks = appendProfileCheck(checks, "read-model:"+key, false, "read model was not written")
				continue
			}
			return nil, err
		}
		ok := model.ConfigVersionID == report.ConfigVersion.ID && strings.TrimSpace(model.PayloadJSON) != ""
		checks = appendProfileCheck(checks, "read-model:"+key, ok, "read model exists for published config version")
	}
	if options.RequireCaseRuns {
		caseRunChecks, err := verifyProfileAPICaseRuns(ctx, s, bundle)
		if err != nil {
			return nil, err
		}
		checks = append(checks, caseRunChecks...)
	}
	if options.RequireWorkflowRuns {
		workflowChecks, err := verifyProfileWorkflowRuns(ctx, s, bundle)
		if err != nil {
			return nil, err
		}
		checks = append(checks, workflowChecks...)
	}
	return checks, nil
}

func verifyProfileWorkflowRuns(ctx context.Context, s store.Store, bundle profile.Bundle) ([]profileVerifyCheck, error) {
	if len(bundle.Workflows) == 0 {
		return []profileVerifyCheck{{Name: "workflow-runs", OK: true, Detail: "profile declares no workflows"}}, nil
	}
	runs, err := s.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	latestByWorkflow := map[string]store.Run{}
	for _, item := range runs {
		if item.WorkflowID == "" {
			continue
		}
		current, ok := latestByWorkflow[item.WorkflowID]
		if !ok || item.CreatedAt.After(current.CreatedAt) || (item.CreatedAt.Equal(current.CreatedAt) && item.ID > current.ID) {
			latestByWorkflow[item.WorkflowID] = item
		}
	}
	checks := make([]profileVerifyCheck, 0, len(bundle.Workflows))
	for _, item := range bundle.Workflows {
		run, ok := latestByWorkflow[item.ID]
		if !ok || !isPassedStatus(run.Status) {
			checks = appendProfileCheck(checks, "workflow-run:"+item.ID, false, "no passed run recorded in Store")
			continue
		}
		checks = appendProfileCheck(checks, "workflow-run:"+item.ID, true, "latest Workflow run passed")
	}
	return checks, nil
}

func verifyProfileAPICaseRuns(ctx context.Context, s store.Store, bundle profile.Bundle) ([]profileVerifyCheck, error) {
	if len(bundle.APICases) == 0 {
		return []profileVerifyCheck{{Name: "api-case-runs", OK: true, Detail: "profile declares no API cases"}}, nil
	}
	latestStore, ok := s.(interface {
		ListLatestAPICaseRuns(context.Context) ([]store.APICaseRun, error)
	})
	if !ok {
		return nil, errors.New("runtime store does not support latest API case run lookup")
	}
	latestRuns, err := latestStore.ListLatestAPICaseRuns(ctx)
	if err != nil {
		return nil, err
	}
	latestByCase := map[string]store.APICaseRun{}
	for _, item := range latestRuns {
		latestByCase[item.CaseID] = item
	}
	checks := make([]profileVerifyCheck, 0, len(bundle.APICases))
	for _, item := range bundle.APICases {
		run, ok := latestByCase[item.ID]
		if !ok || !isPassedStatus(run.Status) {
			checks = appendProfileCheck(checks, "api-case-run:"+item.ID, false, "no passed run recorded in Store")
			continue
		}
		checks = appendProfileCheck(checks, "api-case-run:"+item.ID, true, "latest API case run passed")
	}
	return checks, nil
}

func isPassedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass", "passed", "success", "ok":
		return true
	default:
		return false
	}
}

func appendProfileCheck(checks []profileVerifyCheck, name string, ok bool, detail string) []profileVerifyCheck {
	return append(checks, profileVerifyCheck{Name: name, OK: ok, Detail: detail})
}

func profileChecksOK(checks []profileVerifyCheck) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}

func firstFailedProfileCheck(checks []profileVerifyCheck) string {
	for _, check := range checks {
		if !check.OK {
			return check.Name + ": " + check.Detail
		}
	}
	return "no checks passed"
}

func profileImportAssetCounts(counts profile.Counts) profileImportCounts {
	return profileImportCounts{
		Services:         counts.Services,
		Workflows:        counts.Workflows,
		InterfaceNodes:   counts.InterfaceNodes,
		APICases:         counts.APICases,
		RequestTemplates: counts.RequestTemplates,
		CaseDependencies: counts.CaseDependencies,
		WorkflowBindings: counts.WorkflowBindings,
		Fixtures:         counts.Fixtures,
	}
}

func readCurrentProfileCatalog(ctx context.Context, s store.Store) (store.ProfileCatalog, bool, error) {
	catalog, err := s.GetProfileCatalog(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.ProfileCatalog{}, false, nil
		}
		return store.ProfileCatalog{}, false, err
	}
	return catalog, true, nil
}

func profileImportDiffFromCatalogs(before store.ProfileCatalog, after store.ProfileCatalog, hasBefore bool) profileImportDiff {
	diff := profileImportDiff{
		HasPreviousCatalog: hasBefore,
		Before:             profileImportCountsFromCatalog(before),
		After:              profileImportCountsFromCatalog(after),
		APICases: profileImportCaseDiff{
			Before: len(before.APICases),
			After:  len(after.APICases),
		},
	}
	if !hasBefore {
		diff.APICases.Before = 0
		diff.Before = profileImportCounts{}
	}
	beforeIDs := map[string]bool{}
	for _, item := range before.APICases {
		beforeIDs[item.ID] = true
	}
	afterIDs := map[string]bool{}
	for _, item := range after.APICases {
		afterIDs[item.ID] = true
		if hasBefore && !beforeIDs[item.ID] {
			diff.APICases.Added = append(diff.APICases.Added, item.ID)
		}
	}
	if hasBefore {
		for _, item := range before.APICases {
			if !afterIDs[item.ID] {
				diff.APICases.Removed = append(diff.APICases.Removed, item.ID)
			}
		}
	}
	sort.Strings(diff.APICases.Added)
	sort.Strings(diff.APICases.Removed)
	diff.NodeCaseDeltas = profileImportNodeCaseDeltas(before.APICases, after.APICases, hasBefore)
	return diff
}

func profileImportCountsFromCatalog(catalog store.ProfileCatalog) profileImportCounts {
	return profileImportCounts{
		Services:         len(catalog.Services),
		Workflows:        len(catalog.Workflows),
		InterfaceNodes:   len(catalog.InterfaceNodes),
		APICases:         len(catalog.APICases),
		RequestTemplates: len(catalog.RequestTemplates),
		CaseDependencies: len(catalog.CaseDependencies),
		WorkflowBindings: len(catalog.WorkflowBindings),
		Fixtures:         len(catalog.Fixtures),
	}
}

func profileImportNodeCaseDeltas(before []store.CatalogAPICase, after []store.CatalogAPICase, hasBefore bool) []profileImportNodeCaseDelta {
	beforeCounts := map[string]int{}
	if hasBefore {
		for _, item := range before {
			beforeCounts[firstNonEmpty(item.NodeID, "(none)")]++
		}
	}
	afterCounts := map[string]int{}
	for _, item := range after {
		afterCounts[firstNonEmpty(item.NodeID, "(none)")]++
	}
	nodeIDs := map[string]bool{}
	for nodeID := range beforeCounts {
		nodeIDs[nodeID] = true
	}
	for nodeID := range afterCounts {
		nodeIDs[nodeID] = true
	}
	out := make([]profileImportNodeCaseDelta, 0, len(nodeIDs))
	for _, nodeID := range sortedBoolMapKeys(nodeIDs) {
		beforeCount := beforeCounts[nodeID]
		afterCount := afterCounts[nodeID]
		if hasBefore && beforeCount == afterCount {
			continue
		}
		out = append(out, profileImportNodeCaseDelta{
			NodeID: nodeID,
			Before: beforeCount,
			After:  afterCount,
			Delta:  afterCount - beforeCount,
		})
	}
	return out
}

func sortedBoolMapKeys(items map[string]bool) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func printProfileImportDiff(diff profileImportDiff) {
	if !diff.HasPreviousCatalog {
		fmt.Printf("API Cases: %d\n", diff.APICases.After)
		return
	}
	fmt.Printf("API Cases: %d -> %d\n", diff.APICases.Before, diff.APICases.After)
	for _, item := range diff.NodeCaseDeltas {
		if item.Delta == 0 {
			continue
		}
		fmt.Printf("- %s: %d -> %d (%+d)\n", item.NodeID, item.Before, item.After, item.Delta)
	}
	if len(diff.APICases.Added) > 0 {
		fmt.Printf("Added Cases: %d\n", len(diff.APICases.Added))
	}
	if len(diff.APICases.Removed) > 0 {
		fmt.Printf("Removed Cases: %d\n", len(diff.APICases.Removed))
	}
}

func profileCatalogIndexFromStore(index store.ProfileCatalogIndex) profileCatalogIndex {
	return profileCatalogIndex{
		ProfileID: index.ProfileID,
		IndexedAt: index.IndexedAt,
		Counts: profileCatalogIndexCounts{
			Services:         index.Counts.Services,
			Workflows:        index.Counts.Workflows,
			InterfaceNodes:   index.Counts.InterfaceNodes,
			APICases:         index.Counts.APICases,
			RequestTemplates: index.Counts.RequestTemplates,
			CaseDependencies: index.Counts.CaseDependencies,
			WorkflowBindings: index.Counts.WorkflowBindings,
			Fixtures:         index.Counts.Fixtures,
			Templates:        index.Counts.Templates,
			TemplateConfigs:  index.Counts.TemplateConfigs,
		},
	}
}

func profileConfigVersionFromStore(item store.ConfigVersion) profileConfigVersion {
	return profileConfigVersion{
		ID:           item.ID,
		ProfileID:    item.ProfileID,
		SourcePath:   item.SourcePath,
		BundleDigest: item.BundleDigest,
		Active:       item.Active,
		PublishedAt:  item.PublishedAt,
		CreatedAt:    item.CreatedAt,
	}
}

func configVersionID(profileID string, publishedAt time.Time) string {
	safeProfileID := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(strings.TrimSpace(profileID))
	if safeProfileID == "" {
		safeProfileID = "profile"
	}
	return "config." + safeProfileID + "." + publishedAt.UTC().Format("20060102T150405.000000000Z")
}

func printProfileImportAudit(report profileaudit.Report) {
	fmt.Printf("Audit OK: %t\n", report.OK)
	fmt.Printf("Audit Issues: %d\n", report.IssueCount)
	for _, item := range report.Issues {
		fmt.Printf("- [%s] %s %s %s: %s\n", item.Severity, item.Code, item.SubjectType, item.SubjectID, item.Message)
	}
}

func readProfileCatalogIndex(ctx context.Context, storeURL string) (profileCatalogIndexReport, error) {
	runtime, err := openStore(ctx, storeURL)
	if err != nil {
		return profileCatalogIndexReport{}, err
	}
	defer runtime.Close()
	index, err := runtime.GetProfileCatalogIndex(ctx)
	if err != nil {
		return profileCatalogIndexReport{}, err
	}
	report := profileCatalogIndexReport{
		ProfileID: index.ProfileID,
		IndexedAt: index.IndexedAt,
		Counts: profileImportCounts{
			Services:         index.Counts.Services,
			Workflows:        index.Counts.Workflows,
			InterfaceNodes:   index.Counts.InterfaceNodes,
			APICases:         index.Counts.APICases,
			RequestTemplates: index.Counts.RequestTemplates,
			CaseDependencies: index.Counts.CaseDependencies,
			WorkflowBindings: index.Counts.WorkflowBindings,
			Fixtures:         index.Counts.Fixtures,
		},
	}
	if version, err := runtime.GetActiveConfigVersion(ctx); err == nil {
		value := profileConfigVersionFromStore(version)
		report.ConfigVersion = &value
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return profileCatalogIndexReport{}, err
	}
	return report, nil
}

func printProfileCatalogIndex(report profileCatalogIndexReport) {
	fmt.Printf("Template Package Catalog Index: %s\n", report.ProfileID)
	fmt.Printf("Indexed At: %s\n", report.IndexedAt.Format(time.RFC3339))
	fmt.Printf("Services: %d\n", report.Counts.Services)
	fmt.Printf("Workflows: %d\n", report.Counts.Workflows)
	fmt.Printf("Interface Nodes: %d\n", report.Counts.InterfaceNodes)
	fmt.Printf("API Cases: %d\n", report.Counts.APICases)
	fmt.Printf("Request Templates: %d\n", report.Counts.RequestTemplates)
	if report.ConfigVersion != nil {
		fmt.Printf("Config Version: %s\n", report.ConfigVersion.ID)
	}
}

func printProfileVerify(report profileVerifyReport) {
	fmt.Printf("Profile Verification: %s\n", report.ProfileID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Audit OK: %t\n", report.Audit.OK)
	fmt.Printf("Published Config: %s\n", report.Publish.ConfigVersion.ID)
	fmt.Printf("Read Models: %s\n", strings.Join(report.Publish.ReadModels, ", "))
	fmt.Printf("Checks: %d passed / %d total", report.Summary.PassedChecks, report.Summary.TotalChecks)
	if report.Summary.FailedChecks > 0 {
		fmt.Printf(" (%d failed", report.Summary.FailedChecks)
		if report.Summary.FirstFailed != "" {
			fmt.Printf(", first failed: %s", report.Summary.FirstFailed)
		}
		fmt.Print(")")
	}
	fmt.Println()
	fmt.Printf("Runtime Gates: api-cases=%t workflows=%t\n", report.Summary.RequiredCaseRuns, report.Summary.RequiredWorkflowRuns)
	fmt.Println("Checks:")
	for _, check := range report.Checks {
		fmt.Printf("- %s: %t (%s)\n", check.Name, check.OK, check.Detail)
	}
}

func printProfile(bundle profile.Bundle) {
	counts := bundle.Counts()
	fmt.Printf("Profile: %s\n", bundle.ID)
	fmt.Printf("Display Name: %s\n", bundle.DisplayName)
	fmt.Printf("Services: %d\n", counts.Services)
	fmt.Printf("Workflows: %d\n", counts.Workflows)
	fmt.Printf("Interface Nodes: %d\n", counts.InterfaceNodes)
	fmt.Printf("API Cases: %d\n", counts.APICases)
	fmt.Printf("Request Templates: %d\n", counts.RequestTemplates)
	fmt.Printf("Case Dependencies: %d\n", counts.CaseDependencies)
	fmt.Printf("Workflow Bindings: %d\n", counts.WorkflowBindings)
	fmt.Printf("Fixtures: %d\n", counts.Fixtures)
}

func runProfileAudit(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile audit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	templatePackagePath := flags.String("template-package", "", "Template package path or installed template package id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	offlineTemplatePackage := flags.Bool("offline-template-package", false, "Read the template package directly for offline review")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	force := flags.Bool("force", false, "Replace an installed profile when --profile points to a packed archive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*offlineTemplatePackage {
		return errors.New("--profile audit reads template packages only for offline review; add --offline-template-package")
	}
	resolvedProfilePath, err := materializeProfileReference(templatePackageReference(*templatePackagePath, *profilePath), *profileHome, *force)
	if err != nil {
		return err
	}
	bundle, err := profile.Load(resolvedProfilePath)
	if err != nil {
		return err
	}

	options := profileaudit.Options{
		Bundle:     bundle,
		BundlePath: resolvedProfilePath,
	}
	resolvedStoreURL, err := resolveStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resolvedStoreURL) != "" {
		s, err := openStore(ctx, resolvedStoreURL)
		if err != nil {
			return err
		}
		defer s.Close()
		options.Store = s
	}

	report, err := profileaudit.Audit(ctx, options)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printProfileAudit(report)
	return nil
}

func runProfileAuditPlan(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("profile audit-plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	templatePackagePath := flags.String("template-package", "", "Template package path or installed template package id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	offlineTemplatePackage := flags.Bool("offline-template-package", false, "Read the template package directly for offline review")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	force := flags.Bool("force", false, "Replace an installed profile when --profile points to a packed archive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*offlineTemplatePackage {
		return errors.New("--profile audit-plan reads template packages only for offline review; add --offline-template-package")
	}
	resolvedStoreURL, err := resolveStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	report, err := profileAuditRepairPlan(ctx, templatePackageReference(*templatePackagePath, *profilePath), *profileHome, resolvedStoreURL, *force)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printProfileAuditRepairPlan(report)
	return nil
}

func profileAuditRepairPlan(ctx context.Context, profilePath string, profileHome string, storeURL string, force bool) (profileaudit.RepairPlanReport, error) {
	resolvedProfilePath, err := materializeProfileReference(profilePath, profileHome, force)
	if err != nil {
		return profileaudit.RepairPlanReport{}, err
	}
	bundle, err := profile.Load(resolvedProfilePath)
	if err != nil {
		return profileaudit.RepairPlanReport{}, err
	}
	options := profileaudit.Options{
		Bundle:     bundle,
		BundlePath: resolvedProfilePath,
	}
	if strings.TrimSpace(storeURL) != "" {
		s, err := openStore(ctx, storeURL)
		if err != nil {
			return profileaudit.RepairPlanReport{}, err
		}
		defer s.Close()
		options.Store = s
	}
	audit, err := profileaudit.Audit(ctx, options)
	if err != nil {
		return profileaudit.RepairPlanReport{}, err
	}
	return profileaudit.RepairPlan(audit), nil
}

func printProfileAudit(report profileaudit.Report) {
	fmt.Printf("Profile Audit: %s\n", report.ProfileID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Issues: %d\n", report.IssueCount)
	for _, item := range report.Issues {
		fmt.Printf("- [%s] %s %s %s: %s\n", item.Severity, item.Code, item.SubjectType, item.SubjectID, item.Message)
	}
	if report.Store == nil {
		return
	}
	fmt.Printf("Store Profile Indexed: %t\n", report.Store.ProfileIndexed)
	if report.Store.BundleDigest != "" || report.Store.IndexedDigest != "" {
		fmt.Printf("Store Digest Matches: %t\n", report.Store.DigestMatches)
	}
	for _, item := range report.Store.APICases {
		status := item.LatestStatus
		if status == "" {
			status = "not-run"
		}
		fmt.Printf("API Case: %s Status: %s Passed: %t\n", item.CaseID, status, item.HasPassed)
	}
}

func printProfileAuditRepairPlan(report profileaudit.RepairPlanReport) {
	fmt.Printf("Profile Audit Repair Plan: %s\n", report.ProfileID)
	fmt.Printf("Issues: %d\n", report.IssueCount)
	fmt.Printf("Actions: %d\n", report.ActionCount)
	for _, item := range report.Actions {
		fmt.Printf("- %s %s %s %s: %s\n", item.Type, item.IssueCode, item.SubjectType, item.SubjectID, item.SuggestedChange)
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

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
	flags := flag.NewFlagSet("interface-node discover", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	offlineTemplatePackage := flags.Bool("offline-template-package", false, "Read the template package directly for offline review")
	filter := flags.String("filter", "", "Filter by id, display name, or operation")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	discoveryProfileRef, resolvedStoreURL, err := resolveDiscoveryInputs(*profilePath, *storeRef, *storeURL, *offlineTemplatePackage)
	if err != nil {
		return err
	}
	bundle, _, cleanup, err := loadInterfaceNodeReportBundle(ctx, discoveryProfileRef, *profileHome, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report := interfaceNodeList(bundle, *filter)
	if *jsonOutput {
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
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	workflowID := flags.String("workflow", "", "Workflow id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, runtime, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()

	var payload map[string]any
	if gapsOnly {
		payload, err = controlplane.InterfaceNodeCoverageGapsPayload(ctx, bundle, *workflowID, runtime)
	} else {
		payload, err = controlplane.InterfaceNodeCoveragePayload(ctx, bundle, *workflowID, runtime)
	}
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printInterfaceNodeCoverage(payload, gapsOnly)
	return nil
}

func printInterfaceNodeCoverage(payload map[string]any, gapsOnly bool) {
	if gapsOnly {
		fmt.Printf("Interface Node Coverage Gaps: %s\n", valueString(payload["workflowId"]))
		summary := mapFromReportAny(payload["summary"])
		fmt.Printf("Total Steps: %d\n", intFromReportAny(summary["totalSteps"]))
		fmt.Printf("Gaps: %d\n", intFromReportAny(summary["gapCount"]))
		for _, item := range listFromReportAny(payload["gaps"]) {
			row := mapFromReportAny(item)
			fmt.Printf("Gap: %s Node: %s Case: %s\n", valueString(row["stepId"]), valueString(row["nodeId"]), valueString(row["caseId"]))
		}
		return
	}
	fmt.Printf("Interface Node Coverage: %s\n", valueString(payload["workflowId"]))
	summary := mapFromReportAny(payload["summary"])
	fmt.Printf("Total Steps: %d\n", intFromReportAny(summary["totalSteps"]))
	fmt.Printf("Mapped Steps: %d\n", intFromReportAny(summary["mappedSteps"]))
	fmt.Printf("Unmapped Steps: %d\n", intFromReportAny(summary["unmappedSteps"]))
	for _, item := range listFromReportAny(payload["rows"]) {
		row := mapFromReportAny(item)
		fmt.Printf("Step: %s Node: %s Mapped: %t Admission: %s\n", valueString(row["stepId"]), valueString(row["nodeId"]), boolFromReportAny(row["mapped"]), valueString(row["admissionStatus"]))
	}
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

func auditInterfaceNodeCaseExecutionConfigs(bundle profile.Bundle, nodeID string) interfaceNodeCaseAuditReport {
	configs := caseExecutionConfigIDs(bundle.TemplateConfigs)
	report := interfaceNodeCaseAuditReport{ProfileID: bundle.ID, NodeID: nodeID}
	for _, item := range bundle.APICases {
		if item.NodeID != nodeID {
			continue
		}
		report.Counts.Cases++
		if configID := configs[item.ID]; configID != "" {
			report.Counts.Configured++
			report.Configured = append(report.Configured, interfaceNodeCaseConfigured{CaseID: item.ID, ConfigID: configID})
			continue
		}
		report.Counts.Missing++
		report.Missing = append(report.Missing, interfaceNodeCaseMissing{CaseID: item.ID, Title: firstNonEmpty(item.DisplayName, item.ID)})
	}
	report.OK = report.Counts.Cases > 0 && report.Counts.Missing == 0
	return report
}

func caseExecutionConfigIDs(configs []profile.TemplateConfig) map[string]string {
	out := map[string]string{}
	for _, config := range configs {
		if config.Status != "" && config.Status != "active" {
			continue
		}
		caseID, ok := caseExecutionConfigCaseID(config.ConfigJSON)
		if ok {
			out[caseID] = config.ID
		}
	}
	return out
}

func caseExecutionConfigCaseID(configJSON string) (string, bool) {
	var parsed struct {
		CaseID        string `json:"caseId"`
		CaseExecution struct {
			Method string `json:"method"`
			NodeID string `json:"nodeId"`
			Path   string `json:"path"`
		} `json:"caseExecution"`
	}
	if err := json.Unmarshal([]byte(configJSON), &parsed); err != nil {
		return "", false
	}
	if strings.TrimSpace(parsed.CaseID) == "" {
		return "", false
	}
	if parsed.CaseExecution.Method == "" && parsed.CaseExecution.NodeID == "" && parsed.CaseExecution.Path == "" {
		return "", false
	}
	return parsed.CaseID, true
}

func printInterfaceNodeCaseAudit(report interfaceNodeCaseAuditReport) {
	fmt.Printf("Profile: %s\n", report.ProfileID)
	fmt.Printf("Interface Node: %s\n", report.NodeID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Cases: %d\n", report.Counts.Cases)
	fmt.Printf("Configured: %d\n", report.Counts.Configured)
	fmt.Printf("Missing: %d\n", report.Counts.Missing)
	for _, item := range report.Missing {
		fmt.Printf("- missing case execution: %s\n", item.CaseID)
	}
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

func draftInterfaceNodeCase(bundle profile.Bundle, nodeID string, caseID string, title string, casePath string, method string, requestPath string, tags []string, priority string, owner string) (interfaceNodeCaseDraftReport, error) {
	node, ok := findInterfaceNode(bundle.InterfaceNodes, nodeID)
	if !ok {
		return interfaceNodeCaseDraftReport{}, fmt.Errorf("interface node %q not found", nodeID)
	}
	caseID = strings.TrimSpace(caseID)
	if caseExists(bundle.APICases, caseID) {
		return interfaceNodeCaseDraftReport{}, fmt.Errorf("api case %q already exists", caseID)
	}
	method = strings.ToUpper(strings.TrimSpace(firstNonEmpty(method, node.Method, "GET")))
	requestPath = strings.TrimSpace(firstNonEmpty(requestPath, node.Path, "/"))
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	title = strings.TrimSpace(firstNonEmpty(title, node.DisplayName, caseID))
	if strings.TrimSpace(casePath) == "" {
		casePath = filepath.ToSlash(filepath.Join("api-cases", safeCaseFileName(caseID)+".json"))
	}
	apiCase := profile.APICase{
		ID:          caseID,
		DisplayName: title,
		Description: "Generated draft for " + firstNonEmpty(node.DisplayName, node.ID) + ".",
		NodeID:      node.ID,
		Tags:        casesuite.NormalizeStringList(tags),
		Priority:    strings.TrimSpace(priority),
		Owner:       strings.TrimSpace(owner),
		Status:      "active",
		SortOrder:   nextCaseSortOrder(bundle.APICases),
		CasePath:    filepath.ToSlash(casePath),
	}
	caseFile := caseFileInput{
		Path: apiCase.CasePath,
		Case: apicase.Case{
			ID:    caseID,
			Title: title,
			Request: apicase.Request{
				Method:  method,
				Path:    requestPath,
				Headers: draftCaseHeaders(method),
				Body:    draftCaseBody(method),
			},
			Assertions: apicase.Assertions{ExpectedStatusCodes: []int{http.StatusOK}},
		},
	}
	configJSON, err := compactJSONValue(map[string]any{
		"caseId": caseID,
		"caseExecution": map[string]any{
			"method":            method,
			"nodeId":            node.ID,
			"path":              requestPath,
			"expectedHttpCodes": []int{http.StatusOK},
		},
	})
	if err != nil {
		return interfaceNodeCaseDraftReport{}, err
	}
	config := profile.TemplateConfig{
		ID:          "cfg." + caseID,
		TemplateID:  "case-execution",
		NodeID:      node.ID,
		ScopeType:   "case",
		ScopeID:     caseID,
		Title:       title + " execution",
		Description: "Generated draft execution config.",
		ConfigJSON:  configJSON,
		Status:      "active",
		SortOrder:   apiCase.SortOrder,
	}
	applyBundle := interfaceNodeCaseApplyRequest{
		APICases:        []profile.APICase{apiCase},
		TemplateConfigs: []templateConfigInput{{TemplateConfig: config}},
		CaseFiles:       []caseFileInput{caseFile},
	}
	return interfaceNodeCaseDraftReport{
		OK:             true,
		ProfileID:      bundle.ID,
		NodeID:         node.ID,
		CaseID:         caseID,
		CasePath:       apiCase.CasePath,
		APICase:        apiCase,
		TemplateConfig: config,
		CaseFile:       caseFile,
		ApplyBundle:    applyBundle,
	}, nil
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
	caseID = strings.TrimSpace(caseID)
	if caseID == "" {
		return "case"
	}
	var builder strings.Builder
	for _, r := range caseID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('-')
	}
	if builder.Len() == 0 {
		return "case"
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

func applyInterfaceNodeCaseConfigs(profilePath string, requestPath string) (interfaceNodeCaseApplyResult, error) {
	raw, err := os.ReadFile(requestPath)
	if err != nil {
		return interfaceNodeCaseApplyResult{}, fmt.Errorf("read case config bundle %s: %w", requestPath, err)
	}
	var request interfaceNodeCaseApplyRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return interfaceNodeCaseApplyResult{}, fmt.Errorf("decode case config bundle %s: %w", requestPath, err)
	}
	request.APICases = append(request.APICases, request.InterfaceNodeCases...)
	if len(request.TemplateConfigs) == 0 && len(request.APICases) == 0 && len(request.CaseFiles) == 0 {
		return interfaceNodeCaseApplyResult{}, errors.New("case config bundle must include apiCases, templateConfigs, or caseFiles")
	}
	configs := make([]profile.TemplateConfig, 0, len(request.TemplateConfigs))
	for _, item := range request.TemplateConfigs {
		config, err := normalizeTemplateConfigInput(item)
		if err != nil {
			return interfaceNodeCaseApplyResult{}, err
		}
		configs = append(configs, config)
	}
	apiCases := make([]profile.APICase, 0, len(request.APICases))
	for _, item := range request.APICases {
		apiCase, err := normalizeAPICaseInput(item)
		if err != nil {
			return interfaceNodeCaseApplyResult{}, err
		}
		apiCases = append(apiCases, apiCase)
	}
	if err := writeCaseFiles(profilePath, request.CaseFiles); err != nil {
		return interfaceNodeCaseApplyResult{}, err
	}
	catalogPath := filepath.Join(profilePath, "catalog.json")
	payload, existingConfigs, existingCases, err := readCatalogCaseAssets(catalogPath)
	if err != nil {
		return interfaceNodeCaseApplyResult{}, err
	}
	if len(configs) > 0 {
		merged := mergeTemplateConfigs(existingConfigs, configs)
		configRaw, err := json.Marshal(merged)
		if err != nil {
			return interfaceNodeCaseApplyResult{}, err
		}
		payload["templateConfigs"] = configRaw
	}
	if len(apiCases) > 0 {
		merged := mergeProfileAPICases(existingCases, apiCases)
		casesRaw, err := json.Marshal(merged)
		if err != nil {
			return interfaceNodeCaseApplyResult{}, err
		}
		payload["interfaceNodeCases"] = casesRaw
		delete(payload, "apiCases")
	}
	if _, ok := payload["schemaVersion"]; !ok {
		payload["schemaVersion"] = json.RawMessage(`"1"`)
	}
	next, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return interfaceNodeCaseApplyResult{}, err
	}
	next = append(next, '\n')
	if err := os.WriteFile(catalogPath, next, 0o644); err != nil {
		return interfaceNodeCaseApplyResult{}, fmt.Errorf("write profile catalog %s: %w", catalogPath, err)
	}
	if _, err := profile.Load(profilePath); err != nil {
		return interfaceNodeCaseApplyResult{}, fmt.Errorf("profile catalog is invalid after apply: %w", err)
	}
	return interfaceNodeCaseApplyResult{Applied: len(configs), Cases: len(apiCases), Files: len(request.CaseFiles)}, nil
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

func writeIndentedJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func compactRawJSON(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	compact, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(compact), nil
}

func compactJSONValue(value any) (string, error) {
	compact, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(compact), nil
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
	positions := map[string]int{}
	out := make([]profile.TemplateConfig, 0, len(existing)+len(updates))
	for _, item := range existing {
		positions[item.ID] = len(out)
		out = append(out, item)
	}
	for _, item := range updates {
		if index, ok := positions[item.ID]; ok {
			out[index] = item
			continue
		}
		positions[item.ID] = len(out)
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.SortOrder != right.SortOrder {
			return left.SortOrder < right.SortOrder
		}
		return left.ID < right.ID
	})
	return out
}

func mergeProfileAPICases(existing []profile.APICase, updates []profile.APICase) []profile.APICase {
	positions := map[string]int{}
	out := make([]profile.APICase, 0, len(existing)+len(updates))
	for _, item := range existing {
		positions[item.ID] = len(out)
		out = append(out, item)
	}
	for _, item := range updates {
		if index, ok := positions[item.ID]; ok {
			out[index] = item
			continue
		}
		positions[item.ID] = len(out)
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.SortOrder != right.SortOrder {
			return left.SortOrder < right.SortOrder
		}
		return left.ID < right.ID
	})
	return out
}

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
		cleanup = func() { _ = opened.Close() }
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

func loadInterfaceNodeReportBundleFromStoreFlags(ctx context.Context, profileRef string, profileHomeRef string, storeRef string, legacyStoreURL string) (profile.Bundle, store.Store, string, func(), error) {
	resolvedStoreURL, err := resolveOptionalBundleStoreReference(profileRef, storeRef, legacyStoreURL)
	if err != nil {
		return profile.Bundle{}, nil, "", func() {}, err
	}
	bundle, runtime, cleanup, err := loadInterfaceNodeReportBundle(ctx, profileRef, profileHomeRef, resolvedStoreURL)
	if err != nil {
		return profile.Bundle{}, nil, resolvedStoreURL, cleanup, err
	}
	return bundle, runtime, resolvedStoreURL, cleanup, nil
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
	caseIDs := make([]string, 0, len(cases))
	for _, item := range cases {
		caseIDs = append(caseIDs, item.ID)
	}
	requestPayload := map[string]any{"caseIds": caseIDs, "baseUrl": baseURL, "timeoutSeconds": timeoutSeconds}
	rawRequest, _ := json.Marshal(requestPayload)
	response, err := http.Post(server.URL+"/api/test-kit/run-batch", "application/json", strings.NewReader(string(rawRequest)))
	if err != nil {
		return interfaceNodeCaseReport{}, err
	}
	defer response.Body.Close()
	var rawBatch map[string]any
	if err := json.NewDecoder(response.Body).Decode(&rawBatch); err != nil {
		return interfaceNodeCaseReport{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return interfaceNodeCaseReport{}, fmt.Errorf("case batch failed with http status %d", response.StatusCode)
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
	values, _ := value.([]any)
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
	jsonPath := filepath.Join(outputDir, "report.json")
	htmlPath := filepath.Join(outputDir, "report.html")
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(htmlPath, []byte(renderInterfaceNodeCaseReportHTML(*report)), 0o644)
}

func renderInterfaceNodeCaseReportHTML(report interfaceNodeCaseReport) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>API Case Report</title><style>`)
	b.WriteString(`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#111827;background:#f8fafc}main{max-width:1280px;margin:auto}h1{font-size:24px;margin:0 0 4px}.meta{color:#4b5563;margin-bottom:16px}.summary{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.pill{border:1px solid #d1d5db;background:white;border-radius:6px;padding:6px 10px;font-size:13px}.ok{color:#047857}.bad{color:#b91c1c}table{width:100%;border-collapse:collapse;background:white;border:1px solid #d1d5db}th,td{border-bottom:1px solid #e5e7eb;text-align:left;vertical-align:top;padding:7px 8px;font-size:13px}th{background:#f3f4f6;color:#374151}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.wrap{word-break:break-all}.small{font-size:12px;color:#6b7280}`)
	b.WriteString(`</style></head><body><main>`)
	b.WriteString(`<h1>` + html.EscapeString(report.NodeName) + `</h1>`)
	b.WriteString(`<div class="meta">` + html.EscapeString(report.NodeID))
	if report.Operation != "" {
		b.WriteString(` · ` + html.EscapeString(report.Operation))
	}
	b.WriteString(`</div><div class="summary">`)
	b.WriteString(reportPill("status", statusText(report.OK)))
	b.WriteString(reportPill("total", strconv.Itoa(report.Counts.Total)))
	b.WriteString(reportPill("passed", strconv.Itoa(report.Counts.Passed)))
	b.WriteString(reportPill("failed", strconv.Itoa(report.Counts.Failed)))
	b.WriteString(reportPill("derived configs", strconv.Itoa(report.Counts.DerivedConfigs)))
	b.WriteString(reportPill("elapsed", fmt.Sprintf("%d ms", report.ElapsedMs)))
	b.WriteString(`</div><table><thead><tr><th>#</th><th>Case</th><th>Status</th><th>HTTP</th><th>Elapsed</th><th>Evidence</th><th>Request</th><th>Response</th><th>Error</th></tr></thead><tbody>`)
	for index, item := range report.Results {
		statusClass := "bad"
		if item.Status == store.StatusPassed {
			statusClass = "ok"
		}
		b.WriteString(`<tr><td class="mono">` + strconv.Itoa(index+1) + `</td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.Title) + `</div><div class="mono small wrap">` + html.EscapeString(item.CaseID) + `</div></td>`)
		b.WriteString(`<td class="` + statusClass + `">` + html.EscapeString(item.Status) + `</td>`)
		b.WriteString(`<td class="mono">` + strconv.Itoa(item.HTTPCode) + `</td>`)
		b.WriteString(`<td class="mono">` + fmt.Sprintf("%d ms", item.ElapsedMs) + `</td>`)
		b.WriteString(`<td class="mono wrap">`)
		if item.DetailURL != "" {
			b.WriteString(`<a href="` + html.EscapeString(item.DetailURL) + `">caseRunId</a><br>`)
		}
		b.WriteString(html.EscapeString(item.CaseRunID))
		b.WriteString(`</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(strings.TrimSpace(item.Method+" "+item.FullURL)) + `</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(item.BodyPreview) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(item.Error) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></main></body></html>`)
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

func reportPill(label string, value string) string {
	return `<span class="pill"><span class="small">` + html.EscapeString(label) + `</span> ` + html.EscapeString(value) + `</span>`
}

func statusText(ok bool) string {
	if ok {
		return store.StatusPassed
	}
	return store.StatusFailed
}

func mapFromReportAny(value any) map[string]any {
	typed, _ := value.(map[string]any)
	if typed == nil {
		return map[string]any{}
	}
	return typed
}

func listFromReportAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		if typed == nil {
			return []any{}
		}
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return []any{}
	}
}

func rawJSONObject(value string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(value) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(value), &out)
	return out
}

func cloneMap(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func mergeReportMap(target map[string]any, key string, values map[string]any) {
	next := mapFromReportAny(target[key])
	if len(next) == 0 {
		next = map[string]any{}
	}
	for itemKey, itemValue := range values {
		next[itemKey] = itemValue
	}
	target[key] = next
}

func firstReportValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func intSliceFromReportAny(value any) []int {
	switch typed := value.(type) {
	case []any:
		out := make([]int, 0, len(typed))
		for _, item := range typed {
			if number := intFromReportAny(item); number > 0 {
				out = append(out, number)
			}
		}
		return out
	case []int:
		return typed
	default:
		return nil
	}
}

func intFromReportAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		out, _ := typed.Int64()
		return int(out)
	default:
		return 0
	}
}

func valueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}

func boolFromReportAny(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func truncateReportText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	if limit <= 1 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func maxInt64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func safeReportID(value string) string {
	var b strings.Builder
	for _, item := range value {
		if item >= 'a' && item <= 'z' || item >= 'A' && item <= 'Z' || item >= '0' && item <= '9' || item == '.' || item == '_' || item == '-' {
			b.WriteRune(item)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "item"
	}
	return b.String()
}

func runWorkflow(args []string) error {
	if len(args) == 0 {
		return errors.New("missing workflow command")
	}
	switch args[0] {
	case "discover":
		return runWorkflowDiscover(context.Background(), args[1:])
	case "plan":
		return runWorkflowPlan(args[1:])
	case "audit":
		return runWorkflowAudit(context.Background(), args[1:])
	case "runs":
		return runWorkflowRuns(context.Background(), args[1:])
	case "run":
		return runWorkflowRun(context.Background(), args[1:])
	case "step":
		return runWorkflowStep(context.Background(), args[1:])
	case "latest-step":
		return runWorkflowLatestStep(context.Background(), args[1:])
	case "gate":
		return runWorkflowGate(context.Background(), args[1:])
	case "report":
		return runWorkflowReport(context.Background(), args[1:])
	case "acceptance":
		return runWorkflowAcceptance(context.Background(), args[1:])
	default:
		return fmt.Errorf("unknown workflow command: %s", args[0])
	}
}

func runWorkflowAcceptance(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing workflow acceptance command")
	}
	switch args[0] {
	case "start":
		return runWorkflowAcceptanceStart(ctx, args[1:])
	case "report":
		return runWorkflowAcceptanceReport(ctx, args[1:])
	default:
		return fmt.Errorf("unknown workflow acceptance command: %s", args[0])
	}
}

func runWorkflowAcceptanceStart(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow acceptance start", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	serverURL := flags.String("server-url", "", "Running control plane base URL")
	workflowID := flags.String("workflow", "", "Workflow id")
	requestID := flags.String("request-id", "", "Acceptance request id")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	evidenceDir := flags.String("evidence-dir", "", "Evidence output directory")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Per-step timeout in seconds")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" || strings.TrimSpace(*workflowID) == "" || strings.TrimSpace(*requestID) == "" {
		return errors.New("--server-url, --workflow, and --request-id are required")
	}
	payload := map[string]any{
		"requestId":  strings.TrimSpace(*requestID),
		"workflowId": strings.TrimSpace(*workflowID),
	}
	if strings.TrimSpace(*baseURL) != "" {
		payload["baseUrl"] = strings.TrimSpace(*baseURL)
	}
	if strings.TrimSpace(*evidenceDir) != "" {
		payload["evidenceDir"] = strings.TrimSpace(*evidenceDir)
	}
	if *timeoutSeconds > 0 {
		payload["timeoutSeconds"] = *timeoutSeconds
	}
	result, err := postWorkflowAcceptanceJSON(ctx, workflowAcceptanceURL(*serverURL, "/api/cases/batch-runs"), payload)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(result)
	}
	printWorkflowAcceptanceStart(result)
	return nil
}

func runWorkflowAcceptanceReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow acceptance report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	serverURL := flags.String("server-url", "", "Running control plane base URL")
	runID := flags.String("run", "", "Acceptance batch run id")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" || strings.TrimSpace(*runID) == "" {
		return errors.New("--server-url and --run are required")
	}
	result, err := fetchWorkflowAcceptanceJSON(ctx, workflowAcceptanceURL(*serverURL, "/api/cases/batch-runs/"+url.PathEscape(strings.TrimSpace(*runID))))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(result)
	}
	printWorkflowAcceptanceReport(result)
	return nil
}

func workflowAcceptanceURL(serverURL string, apiPath string) string {
	return strings.TrimRight(strings.TrimSpace(serverURL), "/") + apiPath
}

func postWorkflowAcceptanceJSON(ctx context.Context, endpoint string, payload map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return doWorkflowAcceptanceJSON(req)
}

func fetchWorkflowAcceptanceJSON(ctx context.Context, endpoint string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return doWorkflowAcceptanceJSON(req)
}

func doWorkflowAcceptanceJSON(req *http.Request) (map[string]any, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return payload, fmt.Errorf("%s %s failed with http status %d: %s", req.Method, req.URL.String(), resp.StatusCode, valueString(payload["error"]))
	}
	return payload, nil
}

func printWorkflowAcceptanceStart(payload map[string]any) {
	fmt.Printf("Workflow Acceptance Run: %s\n", valueString(payload["batchRunId"]))
	fmt.Printf("Workflow: %s\n", valueString(payload["workflowId"]))
	fmt.Printf("Status: %s\n", valueString(payload["status"]))
	fmt.Printf("Report: %s\n", valueString(payload["reportUrl"]))
}

func printWorkflowAcceptanceReport(payload map[string]any) {
	acceptance := mapFromReportAny(payload["acceptance"])
	fmt.Printf("Workflow Acceptance Report: %s\n", valueString(payload["batchRunId"]))
	fmt.Printf("Workflow: %s\n", firstNonEmpty(valueString(acceptance["workflowId"]), valueString(payload["workflowId"])))
	fmt.Printf("Status: %s\n", valueString(payload["status"]))
	fmt.Printf("Accepted: %t\n", boolFromReportAny(acceptance["ok"]))
	fmt.Printf("Template: %s\n", valueString(acceptance["templateId"]))
}

func printEnvironmentAcceptanceStart(payload map[string]any) {
	fmt.Printf("Environment Acceptance Run: %s\n", valueString(payload["batchRunId"]))
	fmt.Printf("Environment: %s\n", valueString(payload["environmentId"]))
	fmt.Printf("Workflow: %s\n", valueString(payload["workflowId"]))
	fmt.Printf("Status: %s\n", valueString(payload["status"]))
	fmt.Printf("Report: %s\n", valueString(payload["reportUrl"]))
}

func printEnvironmentAcceptanceReport(payload map[string]any) {
	acceptance := mapFromReportAny(payload["acceptance"])
	health := mapFromReportAny(acceptance["healthSummary"])
	fmt.Printf("Environment Acceptance Report: %s\n", valueString(payload["batchRunId"]))
	fmt.Printf("Environment: %s\n", valueString(payload["environmentId"]))
	fmt.Printf("Workflow: %s\n", firstNonEmpty(valueString(acceptance["workflowId"]), valueString(payload["workflowId"])))
	fmt.Printf("Status: %s\n", valueString(payload["status"]))
	fmt.Printf("Accepted: %t\n", boolFromReportAny(acceptance["ok"]))
	fmt.Printf("Health: %d/%d\n", intFromReportAny(health["passed"]), intFromReportAny(health["total"]))
}

func runWorkflowStep(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow step", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runID := flags.String("run", "", "Workflow run id")
	stepID := flags.String("step", "", "Workflow step id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runID) == "" || strings.TrimSpace(*stepID) == "" {
		return errors.New("--run and --step are required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	payload, ok, err := controlplane.WorkflowStepRunPayload(ctx, runtime, *runID, *stepID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("workflow run step not found: %s %s", strings.TrimSpace(*runID), strings.TrimSpace(*stepID))
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printWorkflowStep(payload)
	return nil
}

func runWorkflowLatestStep(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow latest-step", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	workflowID := flags.String("workflow", "", "Workflow id")
	stepID := flags.String("step", "", "Workflow step id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workflowID) == "" || strings.TrimSpace(*stepID) == "" {
		return errors.New("--workflow and --step are required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	payload, ok, err := controlplane.LatestWorkflowStepRunPayload(ctx, runtime, *workflowID, *stepID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("workflow run step not found: %s %s", strings.TrimSpace(*workflowID), strings.TrimSpace(*stepID))
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printWorkflowStep(payload)
	return nil
}

func printWorkflowStep(payload map[string]any) {
	run := mapFromReportAny(payload["run"])
	summary := mapFromReportAny(payload["summary"])
	fmt.Println("Workflow Step")
	fmt.Printf("Run: %s\n", valueString(run["id"]))
	fmt.Printf("Workflow: %s\n", valueString(run["workflowId"]))
	steps, _ := summary["steps"].([]any)
	if len(steps) > 0 {
		step := mapFromReportAny(steps[0])
		fmt.Printf("Step: %s\n", valueString(step["stepId"]))
		fmt.Printf("Case: %s\n", valueString(step["caseId"]))
		fmt.Printf("Status: %s\n", valueString(step["status"]))
	}
}

func runWorkflowRuns(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow runs", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	payload, err := controlplane.WorkflowRunsPayload(ctx, runtime)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printWorkflowRuns(payload)
	return nil
}

func runWorkflowRun(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runID := flags.String("run", "", "Workflow run id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runID) == "" {
		return errors.New("--run is required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	payload, ok, err := controlplane.WorkflowRunPayload(ctx, runtime, *runID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("workflow run not found: %s", strings.TrimSpace(*runID))
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printWorkflowRun(payload)
	return nil
}

func printWorkflowRuns(payload map[string]any) {
	items, _ := payload["workflowRuns"].([]map[string]any)
	if items == nil {
		if rawItems, ok := payload["workflowRuns"].([]any); ok {
			for _, raw := range rawItems {
				if item := mapFromReportAny(raw); len(item) > 0 {
					items = append(items, item)
				}
			}
		}
	}
	fmt.Println("Workflow Runs")
	fmt.Printf("Total: %d\n", len(items))
	for _, item := range items {
		fmt.Printf("- %s [%s] %s steps=%s\n", valueString(item["id"]), valueString(item["status"]), valueString(item["workflowId"]), valueString(item["stepCount"]))
	}
}

func printWorkflowRun(payload map[string]any) {
	run := mapFromReportAny(payload["run"])
	summary := mapFromReportAny(payload["summary"])
	fmt.Println("Workflow Run")
	fmt.Printf("Run: %s\n", valueString(run["id"]))
	fmt.Printf("Workflow: %s\n", valueString(run["workflowId"]))
	fmt.Printf("Status: %s\n", valueString(run["status"]))
	if count := valueString(run["stepCount"]); count != "" {
		fmt.Printf("Steps: %s\n", count)
	} else if steps, ok := summary["steps"].([]any); ok {
		fmt.Printf("Steps: %d\n", len(steps))
	}
}

type workflowGateReport struct {
	OK              bool               `json:"ok"`
	RunID           string             `json:"runId"`
	WorkflowID      string             `json:"workflowId,omitempty"`
	Status          string             `json:"status"`
	Counts          workflowGateCounts `json:"counts"`
	Gates           workflowGateGates  `json:"gates"`
	FailedSteps     []workflowGateStep `json:"failedSteps"`
	MissingEvidence []workflowGateStep `json:"missingEvidence"`
	NextActions     []string           `json:"nextActions"`
	Warnings        []string           `json:"warnings"`
}

type workflowGateCounts struct {
	Steps            int `json:"steps"`
	PassedSteps      int `json:"passedSteps"`
	FailedSteps      int `json:"failedSteps"`
	OtherSteps       int `json:"otherSteps"`
	CaseRuns         int `json:"caseRuns"`
	EvidenceComplete int `json:"evidenceComplete"`
}

type workflowGateGates struct {
	RunPassed        bool `json:"runPassed"`
	StepsPresent     bool `json:"stepsPresent"`
	StepsPassed      bool `json:"stepsPassed"`
	EvidenceComplete bool `json:"evidenceComplete"`
}

type workflowGateStep struct {
	StepID        string `json:"stepId,omitempty"`
	CaseID        string `json:"caseId,omitempty"`
	CaseRunID     string `json:"caseRunId,omitempty"`
	Status        string `json:"status,omitempty"`
	EvidenceCount int    `json:"evidenceCount"`
}

type workflowGateOptions struct {
	RunID           string
	RequirePassed   bool
	RequireSteps    bool
	RequireEvidence bool
}

func runWorkflowGate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow gate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runID := flags.String("run", "", "Workflow run id")
	requirePassed := flags.Bool("require-passed", false, "Fail unless the workflow run status is passed")
	requireSteps := flags.Bool("require-steps", false, "Fail unless workflow steps exist and every step passed")
	requireEvidence := flags.Bool("require-evidence", false, "Fail unless every step case run has indexed Evidence")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runID) == "" {
		return errors.New("--run is required")
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report, err := buildWorkflowGateReport(ctx, runtime, workflowGateOptions{
		RunID:           *runID,
		RequirePassed:   *requirePassed,
		RequireSteps:    *requireSteps,
		RequireEvidence: *requireEvidence,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
	} else {
		printWorkflowGate(report)
	}
	if !report.OK {
		return errors.New("workflow gate failed")
	}
	return nil
}

func buildWorkflowGateReport(ctx context.Context, runtime store.Store, options workflowGateOptions) (workflowGateReport, error) {
	run, err := runtime.GetRun(ctx, strings.TrimSpace(options.RunID))
	if err != nil {
		return workflowGateReport{}, err
	}
	caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
	if err != nil {
		return workflowGateReport{}, err
	}
	evidence, err := runtime.ListEvidence(ctx, run.ID)
	if err != nil {
		return workflowGateReport{}, err
	}
	caseRunByID := map[string]store.APICaseRun{}
	caseRunsByCase := map[string][]store.APICaseRun{}
	caseRunsByStep := map[string][]store.APICaseRun{}
	for _, item := range caseRuns {
		caseRunByID[item.ID] = item
		caseRunsByCase[item.CaseID] = append(caseRunsByCase[item.CaseID], item)
		if stepID := apiCaseRunStepID(item); stepID != "" {
			caseRunsByStep[stepID] = append(caseRunsByStep[stepID], item)
		}
	}
	evidenceCountByCaseRun := map[string]int{}
	for _, record := range evidence {
		if strings.TrimSpace(record.CaseRunID) != "" {
			evidenceCountByCaseRun[record.CaseRunID]++
		}
	}

	report := workflowGateReport{
		RunID:           run.ID,
		WorkflowID:      run.WorkflowID,
		Status:          run.Status,
		FailedSteps:     []workflowGateStep{},
		MissingEvidence: []workflowGateStep{},
		NextActions:     []string{},
		Warnings:        []string{},
	}
	steps := workflowGateSteps(run.SummaryJSON)
	report.Counts.Steps = len(steps)
	report.Counts.CaseRuns = len(caseRuns)
	for _, rawStep := range steps {
		step := workflowGateStepFrom(rawStep, caseRunByID, caseRunsByStep, caseRunsByCase, evidenceCountByCaseRun)
		switch {
		case strings.EqualFold(step.Status, store.StatusPassed):
			report.Counts.PassedSteps++
		case strings.EqualFold(step.Status, store.StatusFailed):
			report.Counts.FailedSteps++
			report.FailedSteps = append(report.FailedSteps, step)
		default:
			report.Counts.OtherSteps++
			report.FailedSteps = append(report.FailedSteps, step)
		}
		if step.EvidenceCount > 0 {
			report.Counts.EvidenceComplete++
		} else {
			report.MissingEvidence = append(report.MissingEvidence, step)
		}
	}
	report.Gates = workflowGateGates{
		RunPassed:        strings.EqualFold(run.Status, store.StatusPassed),
		StepsPresent:     report.Counts.Steps > 0,
		StepsPassed:      report.Counts.Steps > 0 && report.Counts.FailedSteps == 0 && report.Counts.OtherSteps == 0,
		EvidenceComplete: report.Counts.Steps > 0 && len(report.MissingEvidence) == 0,
	}
	report.OK = (!options.RequirePassed || report.Gates.RunPassed) &&
		(!options.RequireSteps || (report.Gates.StepsPresent && report.Gates.StepsPassed)) &&
		(!options.RequireEvidence || report.Gates.EvidenceComplete)
	report.NextActions = workflowGateNextActions(report, options)
	return report, nil
}

func workflowGateSteps(summaryJSON string) []map[string]any {
	summary := rawJSONObject(summaryJSON)
	steps := listFromReportAny(summary["steps"])
	out := make([]map[string]any, 0, len(steps))
	for _, raw := range steps {
		step := mapFromReportAny(raw)
		if len(step) > 0 {
			out = append(out, step)
		}
	}
	return out
}

func workflowGateStepFrom(step map[string]any, caseRunByID map[string]store.APICaseRun, caseRunsByStep map[string][]store.APICaseRun, caseRunsByCase map[string][]store.APICaseRun, evidenceCountByCaseRun map[string]int) workflowGateStep {
	out := workflowGateStep{
		StepID:    firstNonEmpty(valueString(step["stepId"]), valueString(step["id"])),
		CaseID:    valueString(step["caseId"]),
		CaseRunID: valueString(step["caseRunId"]),
		Status:    valueString(step["status"]),
	}
	if out.CaseRunID != "" {
		if item, ok := caseRunByID[out.CaseRunID]; ok {
			out.CaseID = firstNonEmpty(out.CaseID, item.CaseID)
			out.Status = firstNonEmpty(out.Status, item.Status)
		}
	}
	if out.CaseRunID == "" && out.StepID != "" {
		if items := caseRunsByStep[out.StepID]; len(items) == 1 {
			item := items[0]
			out.CaseID = firstNonEmpty(out.CaseID, item.CaseID)
			out.CaseRunID = item.ID
			out.Status = firstNonEmpty(out.Status, item.Status)
		}
	}
	if out.CaseRunID == "" && out.CaseID != "" {
		if items := caseRunsByCase[out.CaseID]; len(items) == 1 {
			item := items[0]
			out.CaseRunID = item.ID
			out.Status = firstNonEmpty(out.Status, item.Status)
		}
	}
	if out.Status == "" {
		out.Status = "unknown"
	}
	out.EvidenceCount = evidenceCountByCaseRun[out.CaseRunID]
	return out
}

func apiCaseRunStepID(item store.APICaseRun) string {
	return strings.TrimSpace(valueString(jsonObjectString(item.RequestSummaryJSON)["stepId"]))
}

func workflowGateNextActions(report workflowGateReport, options workflowGateOptions) []string {
	actions := []string{}
	if !report.Gates.StepsPresent {
		return []string{"agent-testbench workflow run --run " + quoteCommandValue(report.RunID) + " --json"}
	}
	for index, item := range report.FailedSteps {
		if index >= 3 {
			break
		}
		if item.StepID != "" {
			actions = append(actions, "agent-testbench workflow step --run "+quoteCommandValue(report.RunID)+" --step "+quoteCommandValue(item.StepID)+" --json")
		}
		if item.CaseRunID != "" {
			actions = append(actions, "agent-testbench case diagnose --case-run "+quoteCommandValue(item.CaseRunID)+" --json")
		}
	}
	if options.RequireEvidence {
		for index, item := range report.MissingEvidence {
			if index >= 3 {
				break
			}
			if item.CaseRunID != "" {
				actions = append(actions, "agent-testbench case evidence --case-run "+quoteCommandValue(item.CaseRunID)+" --json")
			}
		}
	}
	if len(actions) == 0 {
		actions = append(actions, "Workflow gate passed; no action needed")
	}
	return actions
}

func printWorkflowGate(report workflowGateReport) {
	fmt.Println("Workflow Gate")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Run: %s\n", report.RunID)
	fmt.Printf("Workflow: %s\n", report.WorkflowID)
	fmt.Printf("Status: %s\n", report.Status)
	fmt.Printf("Steps: %d Passed: %d Failed: %d Other: %d CaseRuns: %d EvidenceComplete: %d\n", report.Counts.Steps, report.Counts.PassedSteps, report.Counts.FailedSteps, report.Counts.OtherSteps, report.Counts.CaseRuns, report.Counts.EvidenceComplete)
	fmt.Printf("Gates: runPassed=%t stepsPresent=%t stepsPassed=%t evidenceComplete=%t\n", report.Gates.RunPassed, report.Gates.StepsPresent, report.Gates.StepsPassed, report.Gates.EvidenceComplete)
	for _, item := range report.FailedSteps {
		fmt.Printf("Failed Step: %s %s %s %s\n", item.StepID, item.CaseID, item.CaseRunID, item.Status)
	}
	for _, item := range report.MissingEvidence {
		fmt.Printf("Missing Evidence: %s %s %s\n", item.StepID, item.CaseID, item.CaseRunID)
	}
	for _, action := range report.NextActions {
		fmt.Printf("Next: %s\n", action)
	}
}

type workflowListReport struct {
	OK        bool               `json:"ok"`
	ProfileID string             `json:"profileId"`
	Count     int                `json:"count"`
	Items     []workflowListItem `json:"items"`
}

type workflowListItem struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	StepCount   int    `json:"stepCount"`
}

func runWorkflowDiscover(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow discover", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, or description")
	offlineTemplatePackage := flags.Bool("offline-template-package", false, "Read the template package directly for offline review")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	discoveryProfileRef, resolvedStoreURL, err := resolveDiscoveryInputs(*profilePath, *storeRef, *storeURL, *offlineTemplatePackage)
	if err != nil {
		return err
	}
	bundle, _, cleanup, err := loadInterfaceNodeReportBundle(ctx, discoveryProfileRef, *profileHome, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report := workflowList(bundle, *filter)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	for _, item := range report.Items {
		fmt.Printf("%s\t%s\t%d\n", item.ID, item.DisplayName, item.StepCount)
	}
	return nil
}

func workflowList(bundle profile.Bundle, filter string) workflowListReport {
	stepCounts := map[string]int{}
	for _, item := range bundle.WorkflowBindings {
		if strings.TrimSpace(item.WorkflowID) != "" {
			stepCounts[item.WorkflowID]++
		}
	}
	workflows := append([]profile.Workflow(nil), bundle.Workflows...)
	sort.SliceStable(workflows, func(i, j int) bool {
		return workflows[i].ID < workflows[j].ID
	})
	report := workflowListReport{OK: true, ProfileID: bundle.ID}
	for _, workflow := range workflows {
		if !matchesDiscoveryFilter(filter, workflow.ID, workflow.DisplayName, workflow.Description) {
			continue
		}
		report.Items = append(report.Items, workflowListItem{
			ID:          workflow.ID,
			DisplayName: workflow.DisplayName,
			Description: workflow.Description,
			StepCount:   stepCounts[workflow.ID],
		})
	}
	report.Count = len(report.Items)
	return report
}

type workflowCaseReport struct {
	OK            bool                     `json:"ok"`
	ProfileID     string                   `json:"profileId"`
	WorkflowID    string                   `json:"workflowId"`
	WorkflowName  string                   `json:"workflowName"`
	RunID         string                   `json:"runId,omitempty"`
	ReportURL     string                   `json:"reportUrl"`
	JSONReportURL string                   `json:"jsonReportUrl"`
	ElapsedMs     int64                    `json:"elapsedMs"`
	GeneratedAt   time.Time                `json:"generatedAt"`
	Counts        workflowCaseReportCounts `json:"counts"`
	Steps         []workflowCaseReportStep `json:"steps"`
}

type workflowCaseReportCounts struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

type workflowCaseReportStep struct {
	StepID    string `json:"stepId"`
	Title     string `json:"title"`
	CaseID    string `json:"caseId"`
	RunID     string `json:"runId,omitempty"`
	CaseRunID string `json:"caseRunId,omitempty"`
	ViewerURL string `json:"viewerUrl,omitempty"`
	DetailURL string `json:"detailUrl,omitempty"`
	Status    string `json:"status"`
	HTTPCode  int    `json:"httpCode,omitempty"`
	ElapsedMs int64  `json:"elapsedMs"`
	Method    string `json:"method,omitempty"`
	FullURL   string `json:"fullUrl,omitempty"`
	Error     string `json:"error,omitempty"`
}

func runWorkflowReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	workflowID := flags.String("workflow", "", "Workflow id")
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	outputDir := flags.String("output-dir", "", "Report output directory")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workflowID) == "" {
		return errors.New("--workflow is required")
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
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "workflow."+safeReportID(*workflowID)+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeWorkflowCaseReport(ctx, bundle, sourceStore, *workflowID, absOutputDir, *baseURL)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Workflow Report: %s\n", report.WorkflowID)
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Passed: %d Failed: %d\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed)
	fmt.Printf("Elapsed: %d ms\n", report.ElapsedMs)
	fmt.Printf("Report: %s\n", report.ReportURL)
	return nil
}

func executeWorkflowCaseReport(ctx context.Context, bundle profile.Bundle, sourceStore store.Store, workflowID string, outputDir string, baseURL string) (workflowCaseReport, error) {
	started := time.Now()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return workflowCaseReport{}, err
	}
	runtime, err := requiredReportStore(sourceStore)
	if err != nil {
		return workflowCaseReport{}, err
	}
	if err := runtime.ReplaceProfileCatalog(ctx, profilecatalog.FromBundle(bundle, time.Now().UTC())); err != nil {
		return workflowCaseReport{}, err
	}
	handler := controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: runtime})
	server := httptest.NewServer(handler)
	defer server.Close()
	catalog, err := fetchReportMap(server.URL + "/api/catalog")
	if err != nil {
		return workflowCaseReport{}, err
	}
	workflow, err := findWorkflowByIDFromCatalog(catalog, workflowID)
	if err != nil {
		return workflowCaseReport{}, err
	}
	bindingCaseIDs := workflowBindingCaseIDs(bundle.WorkflowBindings, workflowID)
	contextValues := map[string]any{}
	rawSteps, _ := workflow["steps"].([]any)
	steps := make([]map[string]any, 0, len(rawSteps))
	stepReports := make([]workflowCaseReportStep, 0, len(rawSteps))
	for _, rawStep := range rawSteps {
		step := mapFromReportAny(rawStep)
		caseID := runnableWorkflowCaseID(bundle.APICases, valueString(step["caseId"]), bindingCaseIDs[valueString(step["id"])])
		if caseID == "" {
			continue
		}
		timeoutSeconds := workflowStepTimeoutSeconds(workflow, step)
		payload := map[string]any{
			"caseId":         caseID,
			"workflowId":     workflowID,
			"stepId":         valueString(step["id"]),
			"overrides":      contextValues,
			"timeoutSeconds": timeoutSeconds,
			"baseUrl":        baseURL,
		}
		result, err := postReportMap(server.URL+"/api/test-kit/run", payload)
		if err != nil {
			return workflowCaseReport{}, err
		}
		result["stepId"] = valueString(step["id"])
		result["title"] = firstNonEmpty(valueString(step["displayName"]), valueString(step["id"]))
		result["stepOk"] = boolFromReportAny(result["ok"])
		steps = append(steps, result)
		stepReports = append(stepReports, workflowReportStepItem(step, result))
		for key, value := range workflowExportedValues(step, result) {
			contextValues[key] = value
		}
		if !boolFromReportAny(result["ok"]) {
			break
		}
	}
	report := workflowCaseReport{
		OK:           len(stepReports) == len(rawSteps),
		ProfileID:    bundle.ID,
		WorkflowID:   workflowID,
		WorkflowName: firstNonEmpty(valueString(workflow["displayName"]), workflowID),
		ElapsedMs:    time.Since(started).Milliseconds(),
		GeneratedAt:  time.Now().UTC(),
		Steps:        stepReports,
		Counts:       workflowCaseReportCounts{Total: len(rawSteps)},
	}
	for _, item := range stepReports {
		if item.Status == store.StatusPassed {
			report.Counts.Passed++
		} else {
			report.Counts.Failed++
			report.OK = false
		}
	}
	if len(stepReports) != len(rawSteps) {
		report.Counts.Failed += len(rawSteps) - len(stepReports)
		report.OK = false
	}
	snapshot := map[string]any{
		"workflowId": workflowID,
		"status":     statusText(report.OK),
		"ok":         report.OK,
		"elapsedMs":  report.ElapsedMs,
		"summary": map[string]any{
			"expectedStepCount": len(rawSteps),
			"stepCount":         len(stepReports),
			"passed":            report.Counts.Passed,
			"elapsedMs":         report.ElapsedMs,
		},
		"steps": steps,
	}
	if len(steps) > 0 {
		if saved, err := postReportMap(server.URL+"/api/workflow-runs", snapshot); err == nil {
			report.RunID = valueString(saved["workflowRunId"])
		}
	}
	if err := writeWorkflowCaseReportFiles(outputDir, &report); err != nil {
		return workflowCaseReport{}, err
	}
	return report, nil
}

func workflowBindingCaseIDs(bindings []profile.WorkflowBinding, workflowID string) map[string]string {
	out := map[string]string{}
	for _, item := range bindings {
		if item.WorkflowID == workflowID && strings.TrimSpace(item.StepID) != "" && strings.TrimSpace(item.CaseID) != "" {
			out[item.StepID] = item.CaseID
		}
	}
	return out
}

func runnableWorkflowCaseID(cases []profile.APICase, candidates ...string) string {
	known := map[string]bool{}
	for _, item := range cases {
		known[item.ID] = true
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && known[candidate] {
			return candidate
		}
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}

func fetchReportMap(endpoint string) (map[string]any, error) {
	response, err := http.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s failed with http status %d", endpoint, response.StatusCode)
	}
	return payload, nil
}

func postReportMap(endpoint string, payload map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	response, err := http.Post(endpoint, "application/json", strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}
	result["httpStatus"] = response.StatusCode
	return result, nil
}

func findWorkflowByIDFromCatalog(catalog map[string]any, id string) (map[string]any, error) {
	rawWorkflows, _ := catalog["workflows"].([]any)
	id = strings.TrimSpace(id)
	for _, raw := range rawWorkflows {
		workflow := mapFromReportAny(raw)
		if valueString(workflow["id"]) == id {
			return workflow, nil
		}
	}
	return nil, fmt.Errorf("workflow not found: %s", id)
}

func workflowStepTimeoutSeconds(workflow map[string]any, step map[string]any) int {
	timeoutMs := firstPositiveInt(intFromReportAny(step["timeoutMs"]), intFromReportAny(workflow["baseStepTimeoutMs"]), 3000)
	seconds := timeoutMs / 1000
	if timeoutMs%1000 != 0 {
		seconds++
	}
	if seconds <= 0 {
		return 3
	}
	return seconds
}

func workflowReportStepItem(step map[string]any, result map[string]any) workflowCaseReportStep {
	item := interfaceNodeCaseReportItems([]any{result})
	status := store.StatusFailed
	httpCode := 0
	elapsedMs := int64(intFromReportAny(result["elapsedMs"]))
	method := ""
	fullURL := ""
	errText := ""
	runID := valueString(result["runId"])
	caseRunID := valueString(result["caseRunId"])
	viewerURL := valueString(result["viewerUrl"])
	detailURL := valueString(result["detailUrl"])
	if len(item) > 0 {
		status = item[0].Status
		httpCode = item[0].HTTPCode
		elapsedMs = item[0].ElapsedMs
		method = item[0].Method
		fullURL = item[0].FullURL
		errText = item[0].Error
		runID = item[0].RunID
		caseRunID = item[0].CaseRunID
		viewerURL = item[0].ViewerURL
		detailURL = item[0].DetailURL
	}
	return workflowCaseReportStep{
		StepID:    valueString(step["id"]),
		Title:     firstNonEmpty(valueString(step["displayName"]), valueString(step["id"])),
		CaseID:    valueString(result["caseId"]),
		RunID:     runID,
		CaseRunID: caseRunID,
		ViewerURL: viewerURL,
		DetailURL: detailURL,
		Status:    status,
		HTTPCode:  httpCode,
		ElapsedMs: elapsedMs,
		Method:    method,
		FullURL:   fullURL,
		Error:     errText,
	}
}

func workflowExportedValues(step map[string]any, result map[string]any) map[string]any {
	out := map[string]any{}
	rawExports, _ := step["exports"].([]any)
	for _, rawExport := range rawExports {
		item := mapFromReportAny(rawExport)
		name := valueString(item["name"])
		if name == "" {
			continue
		}
		value := workflowValueAtPath(workflowExportRoot(result, valueString(item["from"])), valueString(item["path"]))
		if value != nil && valueString(value) != "" {
			out[name] = value
		}
	}
	return out
}

func workflowExportRoot(result map[string]any, source string) any {
	resultBlock := mapFromReportAny(result["result"])
	request := mapFromReportAny(resultBlock["request"])
	response := mapFromReportAny(resultBlock["response"])
	responseBody := rawJSONObject(valueString(response["body"]))
	switch source {
	case "request", "requestBody":
		return firstReportValue(request, "body")
	case "requestQuery":
		return firstReportValue(request, "query")
	case "responseHeaders":
		return firstReportValue(response, "headers")
	case "response", "responseBody", "":
		return responseBody
	default:
		return responseBody
	}
}

func workflowValueAtPath(root any, path string) any {
	if strings.TrimSpace(path) == "" {
		return root
	}
	current := root
	for _, part := range strings.Split(path, ".") {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
		if current == nil {
			return nil
		}
	}
	return current
}

func writeWorkflowCaseReportFiles(outputDir string, report *workflowCaseReport) error {
	jsonPath := filepath.Join(outputDir, "report.json")
	htmlPath := filepath.Join(outputDir, "report.html")
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(htmlPath, []byte(renderWorkflowCaseReportHTML(*report)), 0o644)
}

func renderWorkflowCaseReportHTML(report workflowCaseReport) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Workflow Report</title><style>`)
	b.WriteString(`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#111827;background:#f8fafc}main{max-width:1280px;margin:auto}h1{font-size:24px;margin:0 0 4px}.meta{color:#4b5563;margin-bottom:16px}.summary{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.pill{border:1px solid #d1d5db;background:white;border-radius:6px;padding:6px 10px;font-size:13px}.ok{color:#047857}.bad{color:#b91c1c}table{width:100%;border-collapse:collapse;background:white;border:1px solid #d1d5db}th,td{border-bottom:1px solid #e5e7eb;text-align:left;vertical-align:top;padding:7px 8px;font-size:13px}th{background:#f3f4f6;color:#374151}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.wrap{word-break:break-all}.small{font-size:12px;color:#6b7280}`)
	b.WriteString(`</style></head><body><main>`)
	b.WriteString(`<h1>` + html.EscapeString(report.WorkflowName) + `</h1>`)
	b.WriteString(`<div class="meta">` + html.EscapeString(report.WorkflowID))
	if report.RunID != "" {
		b.WriteString(` · ` + html.EscapeString(report.RunID))
	}
	b.WriteString(`</div><div class="summary">`)
	b.WriteString(reportPill("status", statusText(report.OK)))
	b.WriteString(reportPill("steps", strconv.Itoa(report.Counts.Total)))
	b.WriteString(reportPill("passed", strconv.Itoa(report.Counts.Passed)))
	b.WriteString(reportPill("failed", strconv.Itoa(report.Counts.Failed)))
	b.WriteString(reportPill("elapsed", fmt.Sprintf("%d ms", report.ElapsedMs)))
	b.WriteString(`</div><table><thead><tr><th>#</th><th>Step</th><th>Case</th><th>Status</th><th>HTTP</th><th>Elapsed</th><th>Evidence</th><th>Request</th><th>Error</th></tr></thead><tbody>`)
	for index, item := range report.Steps {
		statusClass := "bad"
		if item.Status == store.StatusPassed {
			statusClass = "ok"
		}
		b.WriteString(`<tr><td class="mono">` + strconv.Itoa(index+1) + `</td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.Title) + `</div><div class="mono small wrap">` + html.EscapeString(item.StepID) + `</div></td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(item.CaseID) + `</td>`)
		b.WriteString(`<td class="` + statusClass + `">` + html.EscapeString(item.Status) + `</td>`)
		b.WriteString(`<td class="mono">` + strconv.Itoa(item.HTTPCode) + `</td>`)
		b.WriteString(`<td class="mono">` + fmt.Sprintf("%d ms", item.ElapsedMs) + `</td>`)
		b.WriteString(`<td class="mono wrap">`)
		if item.DetailURL != "" {
			b.WriteString(`<a href="` + html.EscapeString(item.DetailURL) + `">caseRunId</a><br>`)
		}
		b.WriteString(html.EscapeString(item.CaseRunID))
		b.WriteString(`</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(strings.TrimSpace(item.Method+" "+item.FullURL)) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(item.Error) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></main></body></html>`)
	return b.String()
}

func runWorkflowAudit(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("workflow audit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	workflowID := flags.String("workflow", "", "Workflow id")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	offlineTemplatePackage := flags.Bool("offline-template-package", false, "Read the template package directly for offline review")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workflowID) == "" {
		return errors.New("--workflow is required")
	}

	var (
		bundle  profile.Bundle
		runtime store.Store
		cleanup = func() {}
		err     error
	)
	if *offlineTemplatePackage {
		if strings.TrimSpace(*profilePath) == "" {
			return errors.New("--offline-template-package requires --profile")
		}
		bundle, err = profile.Load(*profilePath)
		if err != nil {
			return err
		}
		resolvedStoreURL, err := resolveStoreReference(*storeRef, *storeURL)
		if err != nil {
			return err
		}
		if strings.TrimSpace(resolvedStoreURL) != "" {
			runtime, err = openStore(ctx, resolvedStoreURL)
			if err != nil {
				return err
			}
			cleanup = func() { _ = runtime.Close() }
		}
	} else {
		if strings.TrimSpace(*profilePath) != "" {
			return errors.New("--profile is for offline template package review; add --offline-template-package or use --store NAME_OR_DSN")
		}
		resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
		if err != nil {
			return err
		}
		runtime, err = openStore(ctx, resolvedStoreURL)
		if err != nil {
			return err
		}
		cleanup = func() { _ = runtime.Close() }
		bundle, err = serveBundle(ctx, runtime)
		if err != nil {
			cleanup()
			return err
		}
	}
	defer cleanup()
	if _, ok := findWorkflow(bundle, *workflowID); !ok {
		return fmt.Errorf("workflow not found: %s", *workflowID)
	}

	options := workflowaudit.Options{
		Bundle:     bundle,
		WorkflowID: *workflowID,
		Store:      runtime,
	}
	report, err := workflowaudit.Audit(ctx, options)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printWorkflowAudit(report)
	return nil
}

func printWorkflowAudit(report workflowaudit.Report) {
	fmt.Printf("Workflow Audit: %s\n", report.WorkflowID)
	fmt.Printf("Profile: %s\n", report.ProfileID)
	if report.DisplayName != "" {
		fmt.Printf("Display Name: %s\n", report.DisplayName)
	}
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Issues: %d\n", report.IssueCount)
	fmt.Printf("Bindings: %d\n", report.BindingCount)
	for _, item := range report.Bindings {
		fmt.Printf("Binding: %s Node: %s", item.StepID, item.NodeID)
		if item.CaseID != "" {
			fmt.Printf(" Case: %s", item.CaseID)
		}
		fmt.Printf(" Required: %t\n", item.Required)
	}
	for _, item := range report.Issues {
		fmt.Printf("- [%s] %s %s %s: %s\n", item.Severity, item.Code, item.SubjectType, item.SubjectID, item.Message)
	}
	if report.Store == nil {
		return
	}
	if report.Store.LatestRun == nil {
		fmt.Println("Latest Run: not-run")
	} else {
		fmt.Printf("Latest Run: %s [%s]\n", report.Store.LatestRun.ID, report.Store.LatestRun.Status)
	}
	for _, item := range report.Store.BindingCases {
		status := item.LatestStatus
		if status == "" {
			status = "not-run"
		}
		fmt.Printf("Binding Case: %s %s Status: %s Passed: %t\n", item.StepID, item.CaseID, status, item.HasPassed)
	}
}

func runWorkflowPlan(args []string) error {
	flags := flag.NewFlagSet("workflow plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	workflowID := flags.String("workflow", "", "Workflow id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workflowID) == "" {
		return errors.New("--workflow is required")
	}
	bundle, runtime, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(context.Background(), *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	var planStore store.Store
	if runtime != nil {
		planStore = runtime
	}
	payload, ok, err := controlplane.WorkflowPlanPayload(context.Background(), bundle, *workflowID, planStore)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("workflow not found: %s", *workflowID)
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}

	fmt.Printf("Workflow: %s\n", *workflowID)
	for _, raw := range listFromReportAny(payload["steps"]) {
		step := mapFromReportAny(raw)
		fmt.Printf("Step: %s\n", valueString(step["stepId"]))
		fmt.Printf("Node: %s\n", valueString(step["nodeId"]))
		if caseID := valueString(step["caseId"]); caseID != "" {
			fmt.Printf("Case: %s\n", caseID)
		}
		fmt.Printf("Required: %t\n", boolFromReportAny(step["required"]))
	}
	return nil
}

func findWorkflow(bundle profile.Bundle, id string) (profile.Workflow, bool) {
	for _, workflow := range bundle.Workflows {
		if workflow.ID == id {
			return workflow, true
		}
	}
	return profile.Workflow{}, false
}

func runBaseline(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing baseline command")
	}
	switch args[0] {
	case "get":
		return runBaselineGet(ctx, args[1:])
	case "set":
		return runBaselineSet(ctx, args[1:])
	default:
		return fmt.Errorf("unknown baseline command: %s", args[0])
	}
}

func runBaselineGet(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("baseline get", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	profileID := flags.String("profile", "", "Profile id")
	subjectID := flags.String("subject", "", "Subject id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	s, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer s.Close()

	gate, err := s.GetBaselineGate(ctx, *profileID, *subjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("baseline gate not found: %s %s", *profileID, *subjectID)
		}
		return err
	}
	printBaselineGate(gate)
	return nil
}

func runBaselineSet(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("baseline set", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	profileID := flags.String("profile", "", "Profile id")
	subjectID := flags.String("subject", "", "Subject id")
	status := flags.String("status", "", "Gate status")
	required := flags.Bool("required", false, "Mark the gate as required")
	summaryJSON := flags.String("summary-json", "{}", "Gate summary JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	s, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer s.Close()

	now := time.Now().UTC()
	gate, err := s.UpsertBaselineGate(ctx, store.BaselineGate{
		ProfileID:   *profileID,
		SubjectID:   *subjectID,
		Status:      *status,
		Required:    *required,
		SummaryJSON: *summaryJSON,
		CheckedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return err
	}
	printBaselineGate(gate)
	return nil
}

func openStore(ctx context.Context, storeURL string) (store.Store, error) {
	return storeopen.Open(ctx, storeURL)
}

func printBaselineGate(gate store.BaselineGate) {
	fmt.Printf("Baseline Gate: %s %s\n", gate.ProfileID, gate.SubjectID)
	fmt.Printf("Status: %s\n", gate.Status)
	fmt.Printf("Required: %t\n", gate.Required)
}

func runTemplate(args []string) error {
	if len(args) == 0 {
		return errors.New("missing template command")
	}
	switch args[0] {
	case "render":
		return runTemplateRender(args[1:])
	default:
		return fmt.Errorf("unknown template command: %s", args[0])
	}
}

func runTemplateRender(args []string) error {
	flags := flag.NewFlagSet("template render", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	templateID := flags.String("template", "", "Request template id")
	fixtureID := flags.String("fixture", "", "Fixture id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, cleanup, err := loadTemplateRenderBundle(context.Background(), *profilePath, *profileHome, *storeRef, *storeURL, *templateID)
	if err != nil {
		return err
	}
	defer cleanup()
	rendered, err := requesttemplate.Render(bundle, requesttemplate.Options{
		TemplateID: *templateID,
		FixtureID:  *fixtureID,
	})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(rendered)
}

func loadTemplateRenderBundle(ctx context.Context, profileRef string, profileHomeRef string, storeRef string, legacyStoreURL string, templateID string) (profile.Bundle, func(), error) {
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(storeRef, legacyStoreURL)
	if err != nil {
		return profile.Bundle{}, func() {}, err
	}
	if strings.TrimSpace(profileRef) != "" {
		resolvedProfile, err := resolveProfileReference(profileRef, profileHomeRef)
		if err != nil {
			return profile.Bundle{}, func() {}, err
		}
		bundle, err := profile.Load(resolvedProfile)
		return bundle, func() {}, err
	}
	runtime, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return profile.Bundle{}, func() {}, err
	}
	bundle, err := serveBundle(ctx, runtime)
	if err != nil {
		_ = runtime.Close()
		return profile.Bundle{}, func() {}, err
	}
	if templateNeedsPublishedProfile(bundle, templateID) {
		if catalogIndex, err := runtime.GetProfileCatalogIndex(ctx); err == nil && strings.TrimSpace(catalogIndex.ProfileID) != "" {
			if profileIndex, err := runtime.GetProfileIndex(ctx, catalogIndex.ProfileID); err == nil && strings.TrimSpace(profileIndex.BundlePath) != "" {
				if pathBundle, err := profile.Load(profileIndex.BundlePath); err == nil {
					bundle = pathBundle
				}
			}
		}
	}
	return bundle, func() { _ = runtime.Close() }, nil
}

func templateNeedsPublishedProfile(bundle profile.Bundle, templateID string) bool {
	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		return false
	}
	for _, item := range bundle.RequestTemplates {
		if item.ID != templateID {
			continue
		}
		return strings.TrimSpace(item.Method) == "" || strings.TrimSpace(item.Path) == ""
	}
	return false
}

func runEvidence(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing evidence command")
	}
	switch args[0] {
	case "import":
		return runEvidenceImport(ctx, args[1:])
	case "list":
		return runEvidenceList(ctx, args[1:])
	case "tasks":
		return runEvidenceTasks(ctx, args[1:])
	default:
		return fmt.Errorf("unknown evidence command: %s", args[0])
	}
}

func runEvidenceList(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("evidence list", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runID := flags.String("run", "", "Run id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	s, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()

	report, err := controlplane.EvidenceList(ctx, s, *runID)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printEvidenceList(report)
	return nil
}

func printEvidenceList(report controlplane.EvidenceListReport) {
	for _, run := range report.Runs {
		fmt.Printf("Run: %s\n", run.ID)
		fmt.Printf("Profile: %s\n", run.ProfileID)
		fmt.Printf("Status: %s\n", run.Status)
		for _, caseRun := range run.APICaseRuns {
			fmt.Printf("Case Run: %s\n", caseRun.ID)
			fmt.Printf("Case: %s\n", caseRun.CaseID)
			fmt.Printf("Case Status: %s\n", caseRun.Status)
		}
		for _, record := range run.EvidenceRecords {
			fmt.Printf("Evidence: %s %s\n", record.Kind, record.URI)
			if record.StepID != "" {
				fmt.Printf("  Step: %s\n", record.StepID)
			}
		}
	}
}

type evidenceTaskReport struct {
	OK     bool               `json:"ok"`
	RunID  string             `json:"runId"`
	StepID string             `json:"stepId,omitempty"`
	CaseID string             `json:"caseId,omitempty"`
	Kind   string             `json:"kind,omitempty"`
	Status string             `json:"status,omitempty"`
	Counts evidenceTaskCounts `json:"counts"`
	Tasks  []evidenceTaskItem `json:"tasks"`
}

type evidenceTaskCounts struct {
	Total      int   `json:"total"`
	Passed     int   `json:"passed"`
	Failed     int   `json:"failed"`
	Running    int   `json:"running"`
	Skipped    int   `json:"skipped"`
	DurationMs int64 `json:"durationMs"`
}

type evidenceTaskItem struct {
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	WorkflowID    string    `json:"workflowId,omitempty"`
	StepID        string    `json:"stepId,omitempty"`
	CaseID        string    `json:"caseId,omitempty"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	Outcome       string    `json:"outcome"`
	Reason        string    `json:"reason"`
	DisplayStatus string    `json:"displayStatus"`
	StartedAt     time.Time `json:"startedAt"`
	FinishedAt    time.Time `json:"finishedAt"`
	DurationMs    int64     `json:"durationMs"`
	Error         string    `json:"error,omitempty"`
	SummaryJSON   string    `json:"summaryJson,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type evidenceTaskFilter struct {
	RunID  string
	StepID string
	CaseID string
	Kind   string
	Status string
}

func runEvidenceTasks(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("evidence tasks", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runID := flags.String("run", "", "Run id")
	stepID := flags.String("step", "", "Workflow step id")
	caseID := flags.String("case", "", "API case id")
	kind := flags.String("kind", "", "Post-process task kind")
	status := flags.String("status", "", "Post-process task status")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runID) == "" {
		return errors.New("--run is required")
	}
	s, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report, err := evidenceTasks(ctx, s, evidenceTaskFilter{
		RunID:  *runID,
		StepID: *stepID,
		CaseID: *caseID,
		Kind:   *kind,
		Status: *status,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printEvidenceTasks(report)
	return nil
}

func evidenceTasks(ctx context.Context, s store.Store, filter evidenceTaskFilter) (evidenceTaskReport, error) {
	filter.RunID = strings.TrimSpace(filter.RunID)
	if filter.RunID == "" {
		return evidenceTaskReport{}, errors.New("run id is required")
	}
	if _, err := s.GetRun(ctx, filter.RunID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return evidenceTaskReport{}, fmt.Errorf("run not found: %s", filter.RunID)
		}
		return evidenceTaskReport{}, err
	}
	rows, err := s.ListPostProcessTasks(ctx, filter.RunID)
	if err != nil {
		return evidenceTaskReport{}, err
	}
	report := evidenceTaskReport{
		OK:     true,
		RunID:  filter.RunID,
		StepID: strings.TrimSpace(filter.StepID),
		CaseID: strings.TrimSpace(filter.CaseID),
		Kind:   strings.TrimSpace(filter.Kind),
		Status: strings.TrimSpace(filter.Status),
		Tasks:  []evidenceTaskItem{},
	}
	for _, row := range rows {
		if !postProcessTaskMatches(row, filter) {
			continue
		}
		readable := controlplane.PostProcessTaskReadableStatus(row)
		report.Tasks = append(report.Tasks, evidenceTaskItem{
			ID:            row.ID,
			RunID:         row.RunID,
			WorkflowID:    row.WorkflowID,
			StepID:        row.StepID,
			CaseID:        row.CaseID,
			Kind:          row.Kind,
			Status:        row.Status,
			Outcome:       readable.Outcome,
			Reason:        readable.Reason,
			DisplayStatus: readable.DisplayStatus,
			StartedAt:     row.StartedAt,
			FinishedAt:    row.FinishedAt,
			DurationMs:    row.DurationMs,
			Error:         row.Error,
			SummaryJSON:   row.SummaryJSON,
			CreatedAt:     row.CreatedAt,
		})
		report.Counts.Total++
		report.Counts.DurationMs += row.DurationMs
		switch row.Status {
		case store.StatusPassed:
			report.Counts.Passed++
		case store.StatusFailed:
			report.Counts.Failed++
		case store.StatusRunning:
			report.Counts.Running++
		case store.StatusSkipped:
			report.Counts.Skipped++
		}
	}
	return report, nil
}

func postProcessTaskMatches(row store.PostProcessTask, filter evidenceTaskFilter) bool {
	if filter.StepID != "" && row.StepID != filter.StepID {
		return false
	}
	if filter.CaseID != "" && row.CaseID != filter.CaseID {
		return false
	}
	if filter.Kind != "" && row.Kind != filter.Kind {
		return false
	}
	if filter.Status != "" && row.Status != filter.Status {
		return false
	}
	return true
}

func printEvidenceTasks(report evidenceTaskReport) {
	fmt.Printf("Post Process Tasks: %s\n", report.RunID)
	fmt.Printf("Total: %d Passed: %d Failed: %d Running: %d Skipped: %d Duration: %d ms\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed, report.Counts.Running, report.Counts.Skipped, report.Counts.DurationMs)
	for _, task := range report.Tasks {
		fmt.Printf("- %s %s [%s] %d ms\n", task.ID, task.Kind, task.DisplayStatus, task.DurationMs)
		if task.StepID != "" {
			fmt.Printf("  Step: %s\n", task.StepID)
		}
		if task.CaseID != "" {
			fmt.Printf("  Case: %s\n", task.CaseID)
		}
		if task.Reason != "" {
			fmt.Printf("  Reason: %s\n", task.Reason)
		}
		if task.Error != "" {
			fmt.Printf("  Error: %s\n", task.Error)
		}
	}
}

func runEvidenceImport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("evidence import", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	from := flags.String("from", "", "Source runtime SQLite path")
	profileID := flags.String("profile", "", "Profile id")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	s, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer s.Close()
	result, err := evidence.ImportLegacyRuntime(ctx, evidence.ImportOptions{
		SourcePath: *from,
		ProfileID:  *profileID,
		Store:      s,
	})
	if err != nil {
		return err
	}
	report := evidenceImportReport{
		SourcePath:      *from,
		StorePath:       resolvedStoreURL,
		ProfileID:       *profileID,
		RunCount:        result.RunCount,
		APICaseRunCount: result.APICaseRunCount,
		EvidenceCount:   result.EvidenceCount,
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Println("Imported evidence index")
	fmt.Printf("Runs: %d\n", result.RunCount)
	fmt.Printf("API Case Runs: %d\n", result.APICaseRunCount)
	fmt.Printf("Evidence Records: %d\n", result.EvidenceCount)
	return nil
}

type evidenceImportReport struct {
	SourcePath      string `json:"sourcePath"`
	StorePath       string `json:"storePath"`
	ProfileID       string `json:"profileId"`
	RunCount        int    `json:"runCount"`
	APICaseRunCount int    `json:"apiCaseRunCount"`
	EvidenceCount   int    `json:"evidenceCount"`
}

func runCase(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing case command")
	}
	switch args[0] {
	case "discover":
		return runCaseDiscover(ctx, args[1:])
	case "suite":
		return runCaseSuite(ctx, args[1:])
	case "run":
		return runCaseRun(ctx, args[1:])
	case "runs":
		return runCaseRuns(ctx, args[1:])
	case "evidence":
		return runCaseEvidence(ctx, args[1:])
	case "diagnose":
		return runCaseDiagnose(ctx, args[1:])
	case "gate":
		return runCaseGate(ctx, args[1:])
	case "timing":
		return runCaseTiming(ctx, args[1:])
	case "batch":
		return runCaseBatch(ctx, args[1:])
	case "incomplete-batches":
		return runCaseIncompleteBatches(ctx, args[1:])
	default:
		return fmt.Errorf("unknown case command: %s", args[0])
	}
}

func runCaseSuite(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing case suite command")
	}
	switch args[0] {
	case "report":
		return runCaseSuiteReport(ctx, args[1:])
	case "coverage":
		return runCaseSuiteCoverage(ctx, args[1:])
	case "stability":
		return runCaseSuiteStability(ctx, args[1:])
	case "priority":
		return runCaseSuitePriority(ctx, args[1:])
	case "brief":
		return runCaseSuiteBrief(ctx, args[1:])
	case "quality":
		return runCaseSuiteQuality(ctx, args[1:])
	case "quality-plan":
		return runCaseSuiteQualityPlan(ctx, args[1:])
	case "quality-report":
		return runCaseSuiteQualityReport(ctx, args[1:])
	case "inspect":
		return runCaseSuiteInspect(ctx, args[1:])
	case "plan":
		return runCaseSuitePlan(ctx, args[1:])
	case "impact":
		return runCaseSuiteImpact(ctx, args[1:])
	case "impact-report":
		return runCaseSuiteImpactReport(ctx, args[1:])
	default:
		return fmt.Errorf("unknown case suite command: %s", args[0])
	}
}

func runCaseBatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing case batch command")
	}
	switch args[0] {
	case "start":
		return runCaseBatchStart(ctx, args[1:])
	case "report":
		return runCaseBatchReport(ctx, args[1:])
	default:
		return fmt.Errorf("unknown case batch command: %s", args[0])
	}
}

func runCaseBatchStart(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case batch start", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	serverURL := flags.String("server-url", "", "Running control plane base URL")
	workflowID := flags.String("workflow", "", "Workflow id")
	suite := flags.String("suite", "", "Suite selector")
	requestID := flags.String("request-id", "", "Batch request id")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	evidenceDir := flags.String("evidence-dir", "", "Evidence output directory")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Per-case timeout in seconds")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	var caseIDs, nodeIDs stringListFlag
	flags.Var(&caseIDs, "case", "Case id; repeat for multiple cases")
	flags.Var(&nodeIDs, "node", "Interface node id; repeat for multiple nodes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" {
		return errors.New("--server-url is required")
	}
	payload := map[string]any{}
	if values := caseIDs.Values(); len(values) > 0 {
		payload["caseIds"] = values
	}
	if values := nodeIDs.Values(); len(values) > 0 {
		payload["nodeIds"] = values
	}
	if strings.TrimSpace(*workflowID) != "" {
		payload["workflowId"] = strings.TrimSpace(*workflowID)
	}
	if strings.TrimSpace(*suite) != "" {
		payload["suite"] = strings.TrimSpace(*suite)
	}
	if len(payload) == 0 {
		return errors.New("at least one of --case, --node, --workflow, or --suite is required")
	}
	if strings.TrimSpace(*requestID) != "" {
		payload["requestId"] = strings.TrimSpace(*requestID)
	}
	if strings.TrimSpace(*baseURL) != "" {
		payload["baseUrl"] = strings.TrimSpace(*baseURL)
	}
	if strings.TrimSpace(*evidenceDir) != "" {
		payload["evidenceDir"] = strings.TrimSpace(*evidenceDir)
	}
	if *timeoutSeconds > 0 {
		payload["timeoutSeconds"] = *timeoutSeconds
	}
	result, err := postWorkflowAcceptanceJSON(ctx, workflowAcceptanceURL(*serverURL, "/api/cases/batch-runs"), payload)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(result)
	}
	printCaseBatchStart(result)
	return nil
}

func runCaseBatchReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case batch report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	serverURL := flags.String("server-url", "", "Running control plane base URL")
	runID := flags.String("run", "", "Case batch run id")
	jsonOutput := flags.Bool("json", false, "Emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" || strings.TrimSpace(*runID) == "" {
		return errors.New("--server-url and --run are required")
	}
	result, err := fetchWorkflowAcceptanceJSON(ctx, workflowAcceptanceURL(*serverURL, "/api/cases/batch-runs/"+url.PathEscape(strings.TrimSpace(*runID))))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(result)
	}
	printCaseBatchReport(result)
	return nil
}

func printCaseBatchStart(payload map[string]any) {
	fmt.Printf("Case Batch Run: %s\n", valueString(payload["batchRunId"]))
	fmt.Printf("Status: %s\n", valueString(payload["status"]))
	if workflowID := valueString(payload["workflowId"]); workflowID != "" {
		fmt.Printf("Workflow: %s\n", workflowID)
	}
	if total := intFromReportAny(payload["total"]); total > 0 {
		fmt.Printf("Total: %d\n", total)
	}
	fmt.Printf("Report: %s\n", valueString(payload["reportUrl"]))
}

func printCaseBatchReport(payload map[string]any) {
	fmt.Printf("Case Batch Report: %s\n", valueString(payload["batchRunId"]))
	fmt.Printf("Status: %s\n", valueString(payload["status"]))
	fmt.Printf("OK: %t\n", boolFromReportAny(payload["ok"]))
	if total := intFromReportAny(payload["total"]); total > 0 {
		fmt.Printf("Total: %d\n", total)
	}
	if passed := intFromReportAny(payload["passed"]); passed > 0 {
		fmt.Printf("Passed: %d\n", passed)
	}
	if failed := intFromReportAny(payload["failed"]); failed > 0 {
		fmt.Printf("Failed: %d\n", failed)
	}
}

type caseListReport struct {
	OK        bool           `json:"ok"`
	ProfileID string         `json:"profileId"`
	Count     int            `json:"count"`
	Filters   caseListFilter `json:"filters"`
	Items     []caseListItem `json:"items"`
}

type caseListFilter struct {
	Filter   string   `json:"filter,omitempty"`
	NodeID   string   `json:"nodeId,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Status   string   `json:"status,omitempty"`
	Owner    string   `json:"owner,omitempty"`
	Priority string   `json:"priority,omitempty"`
}

type caseListItem struct {
	ID                   string   `json:"id"`
	DisplayName          string   `json:"displayName,omitempty"`
	Description          string   `json:"description,omitempty"`
	NodeID               string   `json:"nodeId,omitempty"`
	CaseType             string   `json:"caseType,omitempty"`
	Scenario             string   `json:"scenario,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	Priority             string   `json:"priority,omitempty"`
	Owner                string   `json:"owner,omitempty"`
	Status               string   `json:"status,omitempty"`
	RequiredForAdmission bool     `json:"requiredForAdmission"`
	SortOrder            int      `json:"sortOrder,omitempty"`
	HasRunnableFile      bool     `json:"hasRunnableFile"`
	HasExecutionConfig   bool     `json:"hasExecutionConfig"`
}

func runCaseTiming(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case timing", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	kind := flags.String("kind", "", "Timing kind")
	maxAgeMinutes := flags.String("max-age-minutes", "", "Only include case runs created within this many minutes")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	payload, err := controlplane.CaseTimingPayload(ctx, runtime, *kind, *maxAgeMinutes)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printCaseTiming(payload)
	return nil
}

func printCaseTiming(payload map[string]any) {
	summary := mapFromReportAny(payload["summary"])
	fmt.Println("Case Timing")
	fmt.Printf("Case Runs: %s\n", valueString(summary["caseRunCount"]))
	fmt.Printf("Measured: %s\n", valueString(summary["durationMeasuredCount"]))
	fmt.Printf("Max Duration: %s ms\n", valueString(summary["maxDurationMs"]))
	if slowest := mapFromReportAny(summary["slowestRows"]); len(slowest) > 0 {
		if row := mapFromReportAny(slowest["caseRun"]); len(row) > 0 {
			fmt.Printf("Slowest: %s %s ms\n", valueString(row["id"]), valueString(row["durationMs"]))
		}
	}
}

func runCaseEvidence(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case evidence", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	caseRunID := flags.String("case-run", "", "Case run id")
	runID := flags.String("run", "", "Run id")
	caseID := flags.String("case-id", "", "Case id within the run")
	stepID := flags.String("step-id", "", "Workflow step id within the run")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	payload, err := readCaseEvidence(ctx, runtime, *caseRunID, *runID, *caseID, *stepID)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(payload)
	}
	printCaseEvidence(payload)
	return nil
}

func readCaseEvidence(ctx context.Context, runtime store.Store, caseRunID string, runID string, caseID string, stepID string) (map[string]any, error) {
	var payload map[string]any
	var ok bool
	var err error
	if strings.TrimSpace(caseRunID) != "" {
		payload, ok, err = controlplane.CaseEvidencePayloadForCaseRunID(ctx, runtime, caseRunID)
	} else if strings.TrimSpace(runID) != "" {
		payload, ok, err = controlplane.CaseEvidencePayloadForRunID(ctx, runtime, runID, caseID, stepID)
	} else {
		return nil, errors.New("--case-run or --run is required")
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("case evidence not found")
	}
	return payload, nil
}

func printCaseEvidence(payload map[string]any) {
	evidence := mapFromReportAny(payload["evidence"])
	summary := mapFromReportAny(evidence["summary"])
	fmt.Println("Case Evidence")
	fmt.Printf("Case Run: %s\n", valueString(summary["case_run_id"]))
	fmt.Printf("Case: %s\n", valueString(summary["case_id"]))
	fmt.Printf("Run: %s\n", valueString(summary["run_id"]))
	fmt.Printf("Status: %s\n", valueString(summary["status"]))
	fmt.Printf("Operation: %s\n", valueString(summary["operation"]))
	if evidencePath := valueString(summary["evidence_path"]); evidencePath != "" {
		fmt.Printf("Evidence: %s\n", evidencePath)
	}
}

type caseDiagnosisReport struct {
	OK              bool                  `json:"ok"`
	CaseRunID       string                `json:"caseRunId"`
	RunID           string                `json:"runId"`
	CaseID          string                `json:"caseId"`
	Status          string                `json:"status"`
	Operation       string                `json:"operation,omitempty"`
	Category        string                `json:"category"`
	PrimaryFinding  string                `json:"primaryFinding"`
	EvidencePath    string                `json:"evidencePath,omitempty"`
	AssertionErrors []string              `json:"assertionErrors"`
	Signals         []caseDiagnosisSignal `json:"signals"`
	NextActions     []string              `json:"nextActions"`
	Warnings        []string              `json:"warnings"`
}

type caseDiagnosisSignal struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type caseDiagnosisArtifacts struct {
	AssertionErrors []string
	HTTPStatus      int
	Warnings        []string
}

func runCaseDiagnose(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case diagnose", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	caseRunID := flags.String("case-run", "", "Case run id")
	runID := flags.String("run", "", "Run id")
	caseID := flags.String("case-id", "", "Case id within the run")
	stepID := flags.String("step-id", "", "Workflow step id within the run")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report, err := diagnoseCaseEvidence(ctx, runtime, *caseRunID, *runID, *caseID, *stepID)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseDiagnosis(report)
	return nil
}

func diagnoseCaseEvidence(ctx context.Context, runtime store.Store, caseRunID string, runID string, caseID string, stepID string) (caseDiagnosisReport, error) {
	payload, err := readCaseEvidence(ctx, runtime, caseRunID, runID, caseID, stepID)
	if err != nil {
		return caseDiagnosisReport{}, err
	}
	evidence := mapFromReportAny(payload["evidence"])
	summary := mapFromReportAny(evidence["summary"])
	assertions := mapFromReportAny(evidence["assertions"])
	response := mapFromReportAny(evidence["response"])
	request := mapFromReportAny(evidence["request"])

	report := caseDiagnosisReport{
		CaseRunID:       valueString(summary["case_run_id"]),
		RunID:           valueString(summary["run_id"]),
		CaseID:          valueString(summary["case_id"]),
		Status:          valueString(summary["status"]),
		Operation:       firstNonEmpty(valueString(summary["operation"]), caseRunOperationFromRequest(request, valueString(summary["case_id"]))),
		EvidencePath:    valueString(summary["evidence_path"]),
		AssertionErrors: []string{},
		Signals:         []caseDiagnosisSignal{},
		NextActions:     []string{},
		Warnings:        []string{},
	}
	report.OK = strings.EqualFold(report.Status, store.StatusPassed)

	artifacts, err := readCaseDiagnosisArtifacts(ctx, runtime, report.RunID, report.CaseRunID)
	if err != nil {
		return caseDiagnosisReport{}, err
	}
	report.AssertionErrors = artifacts.AssertionErrors
	report.Warnings = append(report.Warnings, artifacts.Warnings...)
	httpStatus := firstPositiveInt(artifacts.HTTPStatus, intFromReportAny(response["http_code"]), intFromReportAny(summary["actual_http_code"]))
	assertionStatus := valueString(assertions["status"])
	errorCount := firstPositiveInt(len(report.AssertionErrors), intFromReportAny(assertions["errorCount"]))

	report.Category = caseDiagnosisCategory(report.Status, assertionStatus, errorCount, httpStatus)
	report.PrimaryFinding = caseDiagnosisPrimaryFinding(report.Category, report.AssertionErrors, httpStatus, report.Status)
	report.Signals = caseDiagnosisSignals(report, assertionStatus, errorCount, httpStatus)
	report.NextActions = caseDiagnosisNextActions(report, httpStatus, errorCount)
	return report, nil
}

func readCaseDiagnosisArtifacts(ctx context.Context, runtime store.Store, runID string, caseRunID string) (caseDiagnosisArtifacts, error) {
	out := caseDiagnosisArtifacts{AssertionErrors: []string{}, Warnings: []string{}}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(caseRunID) == "" {
		out.Warnings = append(out.Warnings, "case run evidence identity is incomplete")
		return out, nil
	}
	records, err := runtime.ListEvidence(ctx, runID)
	if err != nil {
		return caseDiagnosisArtifacts{}, err
	}
	for _, record := range records {
		if record.CaseRunID != caseRunID {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(record.Kind)) {
		case "assertions":
			var assertions apicase.AssertionEvidence
			if err := readJSONFile(record.URI, &assertions); err != nil {
				out.Warnings = append(out.Warnings, "could not read assertions evidence: "+err.Error())
				continue
			}
			out.AssertionErrors = append(out.AssertionErrors, assertions.Errors...)
		case "response":
			var response apicase.ResponseEvidence
			if err := readJSONFile(record.URI, &response); err != nil {
				out.Warnings = append(out.Warnings, "could not read response evidence: "+err.Error())
				continue
			}
			out.HTTPStatus = response.StatusCode
		}
	}
	return out, nil
}

func caseDiagnosisCategory(status string, assertionStatus string, errorCount int, httpStatus int) string {
	if strings.EqualFold(status, store.StatusPassed) {
		return "passed"
	}
	if strings.EqualFold(assertionStatus, store.StatusFailed) || errorCount > 0 {
		return "assertion-mismatch"
	}
	if httpStatus >= 500 {
		return "server-error"
	}
	if httpStatus >= 400 {
		return "client-error"
	}
	if httpStatus == 0 {
		return "missing-response-evidence"
	}
	return "case-failure"
}

func caseDiagnosisPrimaryFinding(category string, assertionErrors []string, httpStatus int, status string) string {
	if len(assertionErrors) > 0 {
		return "Assertion mismatch: " + assertionErrors[0]
	}
	switch category {
	case "passed":
		return "Case run passed"
	case "server-error":
		return fmt.Sprintf("Target returned HTTP %d", httpStatus)
	case "client-error":
		return fmt.Sprintf("Target rejected the request with HTTP %d", httpStatus)
	case "missing-response-evidence":
		return "Response evidence is missing"
	default:
		return "Case run finished with status " + firstNonEmpty(status, "unknown")
	}
}

func caseDiagnosisSignals(report caseDiagnosisReport, assertionStatus string, errorCount int, httpStatus int) []caseDiagnosisSignal {
	signals := []caseDiagnosisSignal{
		{Name: "case.status", Value: report.Status},
	}
	if report.Operation != "" {
		signals = append(signals, caseDiagnosisSignal{Name: "operation", Value: report.Operation})
	}
	if httpStatus > 0 {
		signals = append(signals, caseDiagnosisSignal{Name: "http.status", Value: strconv.Itoa(httpStatus)})
	}
	if assertionStatus != "" {
		signals = append(signals, caseDiagnosisSignal{Name: "assertion.status", Value: assertionStatus})
	}
	if errorCount > 0 {
		signals = append(signals, caseDiagnosisSignal{Name: "assertion.error_count", Value: strconv.Itoa(errorCount)})
	}
	return signals
}

func caseDiagnosisNextActions(report caseDiagnosisReport, httpStatus int, errorCount int) []string {
	actions := []string{}
	if report.CaseRunID != "" {
		actions = append(actions, "agent-testbench case evidence --case-run "+report.CaseRunID+" --json")
	}
	if errorCount > 0 {
		actions = append(actions, "Inspect request.json, response.json, and assertions.json under "+firstNonEmpty(report.EvidencePath, "the Evidence directory"))
	}
	if httpStatus >= 400 {
		actions = append(actions, "Compare the planned request with the target service contract and expected status codes")
	}
	if len(actions) == 0 {
		actions = append(actions, "No failure action needed")
	}
	return actions
}

func printCaseDiagnosis(report caseDiagnosisReport) {
	fmt.Println("Case Diagnosis")
	fmt.Printf("Case Run: %s\n", report.CaseRunID)
	fmt.Printf("Case: %s\n", report.CaseID)
	fmt.Printf("Status: %s\n", report.Status)
	fmt.Printf("Category: %s\n", report.Category)
	fmt.Printf("Finding: %s\n", report.PrimaryFinding)
	for _, signal := range report.Signals {
		fmt.Printf("Signal: %s=%s\n", signal.Name, signal.Value)
	}
	for _, action := range report.NextActions {
		fmt.Printf("Next: %s\n", action)
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

type caseRunsCLIReport struct {
	OK       bool              `json:"ok"`
	CaseRuns []caseRunsCLIItem `json:"caseRuns"`
	Warnings []string          `json:"warnings"`
}

type caseRunsCLIItem struct {
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	CaseID        string    `json:"caseId"`
	Status        string    `json:"status"`
	Operation     string    `json:"operation"`
	EvidencePath  string    `json:"evidencePath"`
	EvidenceCount int       `json:"evidenceCount"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type caseGateReport struct {
	OK              bool              `json:"ok"`
	RunID           string            `json:"runId,omitempty"`
	Counts          caseGateCounts    `json:"counts"`
	Gates           caseGateGates     `json:"gates"`
	FailedCaseRuns  []caseRunsCLIItem `json:"failedCaseRuns"`
	MissingEvidence []caseRunsCLIItem `json:"missingEvidence"`
	NextActions     []string          `json:"nextActions"`
	Warnings        []string          `json:"warnings"`
}

type caseGateCounts struct {
	Total            int `json:"total"`
	Passed           int `json:"passed"`
	Failed           int `json:"failed"`
	Other            int `json:"other"`
	EvidenceComplete int `json:"evidenceComplete"`
}

type caseGateGates struct {
	HasCaseRuns      bool `json:"hasCaseRuns"`
	NoFailures       bool `json:"noFailures"`
	MinPassed        bool `json:"minPassed"`
	EvidenceComplete bool `json:"evidenceComplete"`
}

type caseGateOptions struct {
	RunID             string
	RequireNoFailures bool
	RequireEvidence   bool
	MinPassed         int
}

func runCaseGate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case gate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runFilter := flags.String("run", "", "Only gate case runs for one run id")
	requireNoFailures := flags.Bool("require-no-failures", false, "Fail when any selected case run is not passed")
	requireEvidence := flags.Bool("require-evidence", false, "Fail when any selected case run has no indexed Evidence")
	minPassed := flags.Int("min-passed", 0, "Fail unless at least this many selected case runs passed")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report, err := buildCaseGateReport(ctx, runtime, caseGateOptions{
		RunID:             *runFilter,
		RequireNoFailures: *requireNoFailures,
		RequireEvidence:   *requireEvidence,
		MinPassed:         *minPassed,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
	} else {
		printCaseGate(report)
	}
	if !report.OK {
		return errors.New("case gate failed")
	}
	return nil
}

func buildCaseGateReport(ctx context.Context, runtime store.Store, options caseGateOptions) (caseGateReport, error) {
	items, err := listCaseRunsFromStore(ctx, runtime, options.RunID)
	if err != nil {
		return caseGateReport{}, err
	}
	report := caseGateReport{
		RunID:           strings.TrimSpace(options.RunID),
		FailedCaseRuns:  []caseRunsCLIItem{},
		MissingEvidence: []caseRunsCLIItem{},
		NextActions:     []string{},
		Warnings:        []string{},
	}
	for _, item := range items.CaseRuns {
		report.Counts.Total++
		if strings.EqualFold(item.Status, store.StatusPassed) {
			report.Counts.Passed++
		} else if strings.EqualFold(item.Status, store.StatusFailed) {
			report.Counts.Failed++
			report.FailedCaseRuns = append(report.FailedCaseRuns, item)
		} else {
			report.Counts.Other++
			report.FailedCaseRuns = append(report.FailedCaseRuns, item)
		}
		if item.EvidenceCount > 0 {
			report.Counts.EvidenceComplete++
		} else {
			report.MissingEvidence = append(report.MissingEvidence, item)
		}
	}
	report.Gates = caseGateGates{
		HasCaseRuns:      report.Counts.Total > 0,
		NoFailures:       report.Counts.Failed == 0 && report.Counts.Other == 0,
		MinPassed:        report.Counts.Passed >= options.MinPassed,
		EvidenceComplete: len(report.MissingEvidence) == 0,
	}
	report.OK = report.Gates.HasCaseRuns &&
		(!options.RequireNoFailures || report.Gates.NoFailures) &&
		(!options.RequireEvidence || report.Gates.EvidenceComplete) &&
		report.Gates.MinPassed
	report.NextActions = caseGateNextActions(report, options)
	return report, nil
}

func caseGateNextActions(report caseGateReport, options caseGateOptions) []string {
	actions := []string{}
	if !report.Gates.HasCaseRuns {
		base := "agent-testbench case runs --json"
		if report.RunID != "" {
			base = "agent-testbench case runs --run " + report.RunID + " --json"
		}
		return []string{base}
	}
	for index, item := range report.FailedCaseRuns {
		if index >= 3 {
			break
		}
		actions = append(actions, "agent-testbench case diagnose --case-run "+item.ID+" --json")
	}
	if options.RequireEvidence {
		for index, item := range report.MissingEvidence {
			if index >= 3 {
				break
			}
			actions = append(actions, "agent-testbench case evidence --case-run "+item.ID+" --json")
		}
	}
	if options.MinPassed > 0 && !report.Gates.MinPassed {
		actions = append(actions, fmt.Sprintf("Run or repair enough cases to reach min-passed=%d", options.MinPassed))
	}
	if len(actions) == 0 {
		actions = append(actions, "Case gate passed; no action needed")
	}
	return actions
}

func printCaseGate(report caseGateReport) {
	fmt.Println("Case Gate")
	fmt.Printf("OK: %t\n", report.OK)
	if report.RunID != "" {
		fmt.Printf("Run: %s\n", report.RunID)
	}
	fmt.Printf("Total: %d Passed: %d Failed: %d Other: %d EvidenceComplete: %d\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed, report.Counts.Other, report.Counts.EvidenceComplete)
	fmt.Printf("Gates: hasCaseRuns=%t noFailures=%t minPassed=%t evidenceComplete=%t\n", report.Gates.HasCaseRuns, report.Gates.NoFailures, report.Gates.MinPassed, report.Gates.EvidenceComplete)
	for _, item := range report.FailedCaseRuns {
		fmt.Printf("Failed: %s %s %s\n", item.ID, item.CaseID, item.Status)
	}
	for _, item := range report.MissingEvidence {
		fmt.Printf("Missing Evidence: %s %s\n", item.ID, item.CaseID)
	}
	for _, action := range report.NextActions {
		fmt.Printf("Next: %s\n", action)
	}
}

func runCaseRuns(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case runs", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	runFilter := flags.String("run", "", "Only list case runs for one run id")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	runtime, cleanup, err := openRequiredCLIStore(ctx, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report, err := listCaseRunsFromStore(ctx, runtime, *runFilter)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseRuns(report)
	return nil
}

func listCaseRunsFromStore(ctx context.Context, runtime store.Store, runFilter string) (caseRunsCLIReport, error) {
	runs, err := runtime.ListRuns(ctx)
	if err != nil {
		return caseRunsCLIReport{}, err
	}
	filter := strings.TrimSpace(runFilter)
	report := caseRunsCLIReport{OK: true, Warnings: []string{}}
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if filter != "" && run.ID != filter {
			continue
		}
		caseRuns, err := runtime.ListAPICaseRuns(ctx, run.ID)
		if err != nil {
			return caseRunsCLIReport{}, err
		}
		evidence, err := runtime.ListEvidence(ctx, run.ID)
		if err != nil {
			return caseRunsCLIReport{}, err
		}
		for j := len(caseRuns) - 1; j >= 0; j-- {
			report.CaseRuns = append(report.CaseRuns, caseRunsCLIItemFrom(run, caseRuns[j], evidence))
		}
	}
	return report, nil
}

func caseRunsCLIItemFrom(run store.Run, item store.APICaseRun, evidence []store.EvidenceRecord) caseRunsCLIItem {
	evidenceCount := 0
	for _, record := range evidence {
		if record.CaseRunID == item.ID {
			evidenceCount++
		}
	}
	request := rawJSONObject(item.RequestSummaryJSON)
	return caseRunsCLIItem{
		ID:            item.ID,
		RunID:         item.RunID,
		CaseID:        item.CaseID,
		Status:        item.Status,
		Operation:     caseRunOperationFromRequest(request, item.CaseID),
		EvidencePath:  run.EvidenceRoot,
		EvidenceCount: evidenceCount,
		UpdatedAt:     firstNonZeroTime(item.CreatedAt, run.UpdatedAt, run.CreatedAt),
	}
}

func caseRunOperationFromRequest(request map[string]any, defaultValue string) string {
	method := strings.ToUpper(strings.TrimSpace(valueString(request["method"])))
	path := strings.TrimSpace(valueString(request["path"]))
	if method != "" && path != "" {
		return method + " " + path
	}
	if method != "" {
		return method
	}
	if path != "" {
		return path
	}
	return defaultValue
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func printCaseRuns(report caseRunsCLIReport) {
	fmt.Println("Case Runs")
	fmt.Printf("Total: %d\n", len(report.CaseRuns))
	for _, item := range report.CaseRuns {
		fmt.Printf("- %s [%s] %s %s evidence=%d\n", item.ID, item.Status, item.CaseID, item.Operation, item.EvidenceCount)
	}
}

func runCaseDiscover(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case discover", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	offlineTemplatePackage := flags.Bool("offline-template-package", false, "Read the template package directly for offline review")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	discoveryProfileRef, resolvedStoreURL, err := resolveDiscoveryInputs(*profilePath, *storeRef, *storeURL, *offlineTemplatePackage)
	if err != nil {
		return err
	}
	bundle, sourceStore, cleanup, err := loadInterfaceNodeReportBundle(ctx, discoveryProfileRef, *profileHome, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer cleanup()
	report := caseList(ctx, bundle, sourceStore, caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	})
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	for _, item := range report.Items {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.DisplayName, item.NodeID, item.Status, item.Priority, strings.Join(item.Tags, ","))
	}
	return nil
}

type caseSuiteCoverageReport struct {
	OK             bool             `json:"ok"`
	ProfileID      string           `json:"profileId"`
	GeneratedAt    string           `json:"generatedAt"`
	Filters        casesuite.Filter `json:"filters"`
	Counts         casesuite.Counts `json:"counts"`
	Items          []casesuite.Item `json:"items"`
	Warnings       []string         `json:"warnings,omitempty"`
	SourceStoreURL string           `json:"sourceStoreUrl,omitempty"`
}

func runCaseSuiteCoverage(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite coverage", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, sourceStore, resolvedStoreURL, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filters := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filters)
	report, err := caseSuiteCoverage(ctx, bundle, sourceStore, resolvedStoreURL, filters, cases)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteCoverage(report)
	return nil
}

func caseSuiteCoverage(ctx context.Context, bundle profile.Bundle, runtime store.Store, sourceStoreURL string, filters caseListFilter, cases []profile.APICase) (caseSuiteCoverageReport, error) {
	report, err := casesuite.Coverage(ctx, bundle, runtime, caseSuiteFilter(filters), cases)
	if err != nil {
		return caseSuiteCoverageReport{}, err
	}
	return caseSuiteCoverageReport{
		OK:             report.OK,
		ProfileID:      report.ProfileID,
		GeneratedAt:    report.GeneratedAt,
		Filters:        report.Filters,
		Counts:         report.Counts,
		Items:          report.Items,
		Warnings:       report.Warnings,
		SourceStoreURL: sourceStoreURL,
	}, nil
}

func caseSuiteFilter(filters caseListFilter) casesuite.Filter {
	filters = normalizeCaseListFilter(filters)
	return casesuite.Filter{
		Filter:   filters.Filter,
		NodeID:   filters.NodeID,
		Tags:     append([]string(nil), filters.Tags...),
		Status:   filters.Status,
		Owner:    filters.Owner,
		Priority: filters.Priority,
	}
}

func printCaseSuiteCoverage(report caseSuiteCoverageReport) {
	fmt.Println("Case Suite Coverage")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Passed: %d Failed: %d Not Run: %d\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed, report.Counts.NotRun)
	for _, item := range report.Items {
		fmt.Printf("- %s [%s]", item.CaseID, item.LatestStatus)
		if item.CaseRunID != "" {
			fmt.Printf(" %s", item.CaseRunID)
		}
		if item.Reason != "" {
			fmt.Printf(" %s", item.Reason)
		}
		fmt.Println()
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteStability(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite stability", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	limit := flags.Int("limit", 10, "Recent runs per case to analyze")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}
	bundle, sourceStore, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filterValue := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filterValue)
	report, err := casesuite.Stability(ctx, bundle, sourceStore, caseSuiteFilter(filterValue), cases, casesuite.StabilityOptions{Limit: *limit})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteStability(report)
	return nil
}

func printCaseSuiteStability(report casesuite.StabilityReport) {
	fmt.Println("Case Suite Stability")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Stable: %d Unstable: %d Not Run: %d\n", report.Counts.Total, report.Counts.Stable, report.Counts.Unstable, report.Counts.NotRun)
	for _, item := range report.Items {
		fmt.Printf("- %s latest=%s transitions=%d unstable=%t\n", item.CaseID, item.LatestStatus, item.Transitions, item.Unstable)
		if item.Reason != "" {
			fmt.Printf("  reason: %s\n", item.Reason)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuitePriority(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite priority", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	limit := flags.Int("limit", 0, "Maximum ready cases to select; 0 selects all ready cases")
	requestID := flags.String("request-id", "", "Request id for the generated batch request")
	baseURL := flags.String("base-url", "", "Base URL for the generated batch request")
	evidenceDir := flags.String("evidence-dir", "", "Evidence directory for the generated batch request")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Timeout seconds for the generated batch request")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	var signals stringListFlag
	var changes stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	flags.Var(&signals, "signal", "Changed path, interface text, workflow text, tag, or case text; repeat for multiple signals")
	flags.Var(&changes, "change", "Alias for --signal; repeat for multiple changes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *limit < 0 {
		return errors.New("--limit cannot be negative")
	}
	if *timeoutSeconds < 0 {
		return errors.New("--timeout-seconds cannot be negative")
	}
	bundle, sourceStore, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filterValue := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filterValue)
	prioritySignals := append(signals.Values(), changes.Values()...)
	report, err := casesuite.Priority(ctx, bundle, sourceStore, caseSuiteFilter(filterValue), cases, casesuite.PriorityOptions{
		Signals:        prioritySignals,
		Limit:          *limit,
		RequestID:      *requestID,
		BaseURL:        *baseURL,
		EvidenceDir:    *evidenceDir,
		TimeoutSeconds: *timeoutSeconds,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuitePriority(report)
	return nil
}

func printCaseSuitePriority(report casesuite.PriorityReport) {
	fmt.Println("Case Suite Priority")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d Selected: %d Skipped: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked, report.Counts.Selected, report.Counts.Skipped)
	for _, item := range report.Selected {
		fmt.Printf("- %s score=%d latest=%s\n", item.CaseID, item.Score, item.LatestStatus)
		for _, reason := range item.Reasons {
			fmt.Printf("  reason: %s\n", reason)
		}
	}
	for _, item := range report.Blocked {
		fmt.Printf("- blocked %s score=%d\n", item.CaseID, item.Score)
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteBrief(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite brief", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	limit := flags.Int("limit", 0, "Maximum ready cases to recommend; 0 recommends all ready cases")
	stabilityLimit := flags.Int("stability-limit", 10, "Recent runs per case to analyze")
	requestID := flags.String("request-id", "", "Request id for the generated batch request")
	baseURL := flags.String("base-url", "", "Base URL for the generated batch request")
	evidenceDir := flags.String("evidence-dir", "", "Evidence directory for the generated batch request")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Timeout seconds for the generated batch request")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	var signals stringListFlag
	var changes stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	flags.Var(&signals, "signal", "Changed path, interface text, workflow text, tag, or case text; repeat for multiple signals")
	flags.Var(&changes, "change", "Alias for --signal; repeat for multiple changes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *limit < 0 {
		return errors.New("--limit cannot be negative")
	}
	if *stabilityLimit <= 0 {
		return errors.New("--stability-limit must be greater than zero")
	}
	if *timeoutSeconds < 0 {
		return errors.New("--timeout-seconds cannot be negative")
	}
	bundle, sourceStore, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filterValue := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filterValue)
	briefSignals := append(signals.Values(), changes.Values()...)
	report, err := casesuite.Brief(ctx, bundle, sourceStore, caseSuiteFilter(filterValue), cases, casesuite.BriefOptions{
		Signals:        briefSignals,
		Limit:          *limit,
		StabilityLimit: *stabilityLimit,
		RequestID:      *requestID,
		BaseURL:        *baseURL,
		EvidenceDir:    *evidenceDir,
		TimeoutSeconds: *timeoutSeconds,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteBrief(report)
	return nil
}

func printCaseSuiteBrief(report casesuite.BriefReport) {
	fmt.Println("Case Suite Brief")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d Passed: %d Failed: %d Not Run: %d Unstable: %d Recommended: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked, report.Counts.Passed, report.Counts.Failed, report.Counts.NotRun, report.Counts.Unstable, report.Counts.PrioritySelected)
	for _, item := range report.Recommended {
		fmt.Printf("- %s score=%d latest=%s\n", item.CaseID, item.Score, item.LatestStatus)
		for _, reason := range item.Reasons {
			fmt.Printf("  reason: %s\n", reason)
		}
	}
	for _, item := range report.Blocked {
		fmt.Printf("- blocked %s\n", item.CaseID)
		for _, issue := range item.Issues {
			fmt.Printf("  issue: %s\n", issue)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteQuality(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite quality", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, sourceStore, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filterValue := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filterValue)
	report, err := casesuite.Quality(ctx, bundle, sourceStore, caseSuiteFilter(filterValue), cases)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteQuality(report)
	return nil
}

func printCaseSuiteQuality(report casesuite.QualityReport) {
	fmt.Println("Case Suite Quality")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Nodes: %d Without Cases: %d Cases: %d Complete: %d Incomplete: %d\n", report.Counts.Nodes, report.Counts.NodesWithoutCases, report.Counts.Cases, report.Counts.CompleteCases, report.Counts.IncompleteCases)
	if report.Counts.InvalidStatus > 0 || report.Counts.NonExecutableLifecycle > 0 {
		fmt.Printf("Lifecycle: non-executable=%d invalid=%d\n", report.Counts.NonExecutableLifecycle, report.Counts.InvalidStatus)
	}
	for _, item := range report.Nodes {
		fmt.Printf("- node %s\n", item.NodeID)
		for _, issue := range item.Issues {
			fmt.Printf("  issue: %s\n", issue)
		}
	}
	for _, item := range report.Cases {
		if item.Complete {
			continue
		}
		fmt.Printf("- case %s\n", item.CaseID)
		for _, issue := range item.Issues {
			fmt.Printf("  issue: %s\n", issue)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteQualityPlan(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite quality-plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, sourceStore, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filterValue := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filterValue)
	report, err := casesuite.QualityPlan(ctx, bundle, sourceStore, caseSuiteFilter(filterValue), cases)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteQualityPlan(report)
	return nil
}

func printCaseSuiteQualityPlan(report casesuite.QualityPlanReport) {
	fmt.Println("Case Suite Quality Plan")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Draft Case: %d Complete Metadata: %d Review Lifecycle: %d Add Runnable: %d Add Execution: %d\n", report.Counts.Total, report.Counts.DraftCase, report.Counts.CompleteMetadata, report.Counts.ReviewLifecycle, report.Counts.AddRunnable, report.Counts.AddExecution)
	for _, item := range report.Actions {
		switch item.Type {
		case "draft-case":
			fmt.Printf("- draft %s for node %s\n", item.SuggestedCaseID, item.NodeID)
		case "review-case-lifecycle":
			fmt.Printf("- review lifecycle %s\n", item.CaseID)
		default:
			fmt.Printf("- %s %s\n", item.Type, item.CaseID)
		}
		if len(item.Fields) > 0 {
			fmt.Printf("  fields: %s\n", strings.Join(item.Fields, ","))
		}
		if item.Reason != "" {
			fmt.Printf("  reason: %s\n", item.Reason)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

type caseSuiteQualityReport struct {
	OK             bool                        `json:"ok"`
	ProfileID      string                      `json:"profileId"`
	Title          string                      `json:"title"`
	ReportURL      string                      `json:"reportUrl"`
	JSONReportURL  string                      `json:"jsonReportUrl"`
	ElapsedMs      int64                       `json:"elapsedMs"`
	GeneratedAt    time.Time                   `json:"generatedAt"`
	Filters        caseListFilter              `json:"filters"`
	Counts         casesuite.QualityPlanCounts `json:"counts"`
	QualityPlan    casesuite.QualityPlanReport `json:"qualityPlan"`
	Warnings       []string                    `json:"warnings,omitempty"`
	SourceStoreURL string                      `json:"sourceStoreUrl,omitempty"`
}

func runCaseSuiteQualityReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite quality-report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	outputDir := flags.String("output-dir", "", "Report output directory")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, sourceStore, resolvedStoreURL, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filterValue := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filterValue)
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "case-suite-quality."+safeReportID(caseSuiteFilterSlug(filterValue))+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeCaseSuiteQualityReport(ctx, bundle, sourceStore, resolvedStoreURL, filterValue, cases, absOutputDir)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteQualityReport(report)
	return nil
}

func executeCaseSuiteQualityReport(ctx context.Context, bundle profile.Bundle, sourceStore store.Store, sourceStoreURL string, filters caseListFilter, cases []profile.APICase, outputDir string) (caseSuiteQualityReport, error) {
	started := time.Now()
	plan, err := casesuite.QualityPlan(ctx, bundle, sourceStore, caseSuiteFilter(filters), cases)
	if err != nil {
		return caseSuiteQualityReport{}, err
	}
	report := caseSuiteQualityReport{
		OK:             true,
		ProfileID:      bundle.ID,
		Title:          "Case Suite Quality Report",
		ElapsedMs:      time.Since(started).Milliseconds(),
		GeneratedAt:    time.Now().UTC(),
		Filters:        normalizeCaseListFilter(filters),
		Counts:         plan.Counts,
		QualityPlan:    plan,
		Warnings:       append([]string(nil), plan.Warnings...),
		SourceStoreURL: sourceStoreURL,
	}
	if sourceStore == nil {
		report.Warnings = append(report.Warnings, "source Store was not available; report used profile bundle only")
	}
	if err := writeCaseSuiteQualityReportFiles(outputDir, &report); err != nil {
		return caseSuiteQualityReport{}, err
	}
	return report, nil
}

func writeCaseSuiteQualityReportFiles(outputDir string, report *caseSuiteQualityReport) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	jsonPath := filepath.Join(outputDir, "report.json")
	htmlPath := filepath.Join(outputDir, "report.html")
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(htmlPath, []byte(renderCaseSuiteQualityReportHTML(*report)), 0o644)
}

func renderCaseSuiteQualityReportHTML(report caseSuiteQualityReport) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Case Suite Quality Report</title><style>`)
	b.WriteString(`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#111827;background:#f8fafc}main{max-width:1280px;margin:auto}h1{font-size:24px;margin:0 0 4px}.meta{color:#4b5563;margin-bottom:16px}.summary{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.pill{border:1px solid #d1d5db;background:white;border-radius:6px;padding:6px 10px;font-size:13px}table{width:100%;border-collapse:collapse;background:white;border:1px solid #d1d5db}th,td{border-bottom:1px solid #e5e7eb;text-align:left;vertical-align:top;padding:7px 8px;font-size:13px}th{background:#f3f4f6;color:#374151}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.wrap{word-break:break-all}.small{font-size:12px;color:#6b7280}.ok{color:#047857}.bad{color:#b91c1c}`)
	b.WriteString(`</style></head><body><main>`)
	b.WriteString(`<h1>Case Suite Quality Report</h1>`)
	b.WriteString(`<div class="meta">` + html.EscapeString(report.ProfileID) + `</div><div class="summary">`)
	b.WriteString(reportPill("status", statusText(report.QualityPlan.Quality.OK)))
	b.WriteString(reportPill("actions", strconv.Itoa(report.Counts.Total)))
	b.WriteString(reportPill("draft", strconv.Itoa(report.Counts.DraftCase)))
	b.WriteString(reportPill("metadata", strconv.Itoa(report.Counts.CompleteMetadata)))
	b.WriteString(reportPill("runnable", strconv.Itoa(report.Counts.AddRunnable)))
	b.WriteString(reportPill("execution", strconv.Itoa(report.Counts.AddExecution)))
	b.WriteString(reportPill("elapsed", fmt.Sprintf("%d ms", report.ElapsedMs)))
	if len(report.Filters.Tags) > 0 {
		b.WriteString(reportPill("tags", strings.Join(report.Filters.Tags, ",")))
	}
	if report.Filters.Owner != "" {
		b.WriteString(reportPill("owner", report.Filters.Owner))
	}
	if report.Filters.Priority != "" {
		b.WriteString(reportPill("priority", report.Filters.Priority))
	}
	b.WriteString(`</div><table><thead><tr><th>#</th><th>Action</th><th>Target</th><th>Fields</th><th>Issues</th><th>Reason</th><th>Command</th></tr></thead><tbody>`)
	for index, item := range report.QualityPlan.Actions {
		target := firstNonEmpty(item.CaseID, item.SuggestedCaseID, item.NodeID)
		b.WriteString(`<tr><td class="mono">` + strconv.Itoa(index+1) + `</td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.Type) + `</div></td>`)
		b.WriteString(`<td><div class="mono wrap">` + html.EscapeString(target) + `</div>`)
		if item.NodeID != "" {
			b.WriteString(`<div class="small">node: ` + html.EscapeString(item.NodeID) + `</div>`)
		}
		if item.NodeName != "" {
			b.WriteString(`<div class="small">` + html.EscapeString(item.NodeName) + `</div>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(strings.Join(item.Fields, ", ")) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(strings.Join(item.Issues, ", ")) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(item.Reason) + `</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(strings.Join(item.Command, " ")) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></main></body></html>`)
	return b.String()
}

func printCaseSuiteQualityReport(report caseSuiteQualityReport) {
	fmt.Println("Case Suite Quality Report")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total Actions: %d Draft Case: %d Complete Metadata: %d Add Runnable: %d Add Execution: %d\n", report.Counts.Total, report.Counts.DraftCase, report.Counts.CompleteMetadata, report.Counts.AddRunnable, report.Counts.AddExecution)
	fmt.Printf("Elapsed: %d ms\n", report.ElapsedMs)
	fmt.Printf("Report: %s\n", report.ReportURL)
	fmt.Printf("JSON: %s\n", report.JSONReportURL)
}

func runCaseSuiteInspect(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite inspect", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
	}
	bundle, sourceStore, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filterValue := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filterValue)
	report, err := casesuite.Inspect(ctx, bundle, sourceStore, caseSuiteFilter(filterValue), cases)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteInspection(report)
	return nil
}

func printCaseSuiteInspection(report casesuite.InspectionReport) {
	fmt.Println("Case Suite Inspection")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d Passed: %d Failed: %d Not Run: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked, report.Counts.Passed, report.Counts.Failed, report.Counts.NotRun)
	for _, item := range report.Items {
		fmt.Printf("- %s ready=%t latest=%s action=%s\n", item.CaseID, item.Ready, item.LatestStatus, item.SuggestedAction)
		for _, issue := range item.Issues {
			fmt.Printf("  issue: %s\n", issue)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuitePlan(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	requestID := flags.String("request-id", "", "Request id for the generated batch request")
	baseURL := flags.String("base-url", "", "Base URL for the generated batch request")
	evidenceDir := flags.String("evidence-dir", "", "Evidence directory for the generated batch request")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Timeout seconds for the generated batch request")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	var actions stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	flags.Var(&actions, "action", "Only select ready cases with this suggested action; repeat for multiple actions")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *timeoutSeconds < 0 {
		return errors.New("--timeout-seconds cannot be negative")
	}
	bundle, sourceStore, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filterValue := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filterValue)
	report, err := casesuite.Plan(ctx, bundle, sourceStore, caseSuiteFilter(filterValue), cases, casesuite.PlanOptions{
		RequestID:      *requestID,
		Actions:        actions.Values(),
		BaseURL:        *baseURL,
		EvidenceDir:    *evidenceDir,
		TimeoutSeconds: *timeoutSeconds,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuitePlan(report)
	return nil
}

func printCaseSuitePlan(report casesuite.PlanReport) {
	fmt.Println("Case Suite Plan")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Ready: %d Blocked: %d Selected: %d Skipped: %d\n", report.Counts.Total, report.Counts.Ready, report.Counts.Blocked, report.Counts.Selected, report.Counts.Skipped)
	for _, item := range report.Selected {
		fmt.Printf("- %s action=%s latest=%s\n", item.CaseID, item.SuggestedAction, item.LatestStatus)
	}
	for _, item := range report.Blocked {
		fmt.Printf("- blocked %s action=%s\n", item.CaseID, item.SuggestedAction)
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func runCaseSuiteImpact(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite impact", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Additional case selector filter")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	requestID := flags.String("request-id", "", "Request id for the generated batch request")
	baseURL := flags.String("base-url", "", "Base URL for the generated batch request")
	evidenceDir := flags.String("evidence-dir", "", "Evidence directory for the generated batch request")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Timeout seconds for the generated batch request")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	var actions stringListFlag
	var signals stringListFlag
	var changes stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	flags.Var(&actions, "action", "Only select ready cases with this suggested action; repeat for multiple actions")
	flags.Var(&signals, "signal", "Changed path, interface text, workflow text, tag, or case text; repeat for multiple signals")
	flags.Var(&changes, "change", "Alias for --signal; repeat for multiple changes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *timeoutSeconds < 0 {
		return errors.New("--timeout-seconds cannot be negative")
	}
	bundle, sourceStore, _, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filterValue := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	impactSignals := append(signals.Values(), changes.Values()...)
	report, err := casesuite.Impact(ctx, bundle, sourceStore, caseSuiteFilter(filterValue), casesuite.ImpactOptions{
		Signals: impactSignals,
		Plan: casesuite.PlanOptions{
			RequestID:      *requestID,
			Actions:        actions.Values(),
			BaseURL:        *baseURL,
			EvidenceDir:    *evidenceDir,
			TimeoutSeconds: *timeoutSeconds,
		},
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteImpact(report)
	return nil
}

func printCaseSuiteImpact(report casesuite.ImpactReport) {
	fmt.Println("Case Suite Impact")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Signals: %d Nodes: %d Workflows: %d Cases: %d Selected: %d Blocked: %d\n", report.Counts.Signals, report.Counts.Nodes, report.Counts.Workflows, report.Counts.Cases, report.Counts.Selected, report.Counts.Blocked)
	for _, item := range report.Cases {
		fmt.Printf("- %s action=%s latest=%s\n", item.CaseID, item.SuggestedAction, item.LatestStatus)
		for _, reason := range item.Reasons {
			fmt.Printf("  reason: %s\n", reason)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

type caseSuiteImpactExecutionReport struct {
	OK        bool                   `json:"ok"`
	Impact    casesuite.ImpactReport `json:"impact"`
	Report    caseSuiteReport        `json:"report"`
	ElapsedMs int64                  `json:"elapsedMs"`
}

func runCaseSuiteImpactReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite impact-report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Additional case selector filter")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	requestID := flags.String("request-id", "", "Request id for the generated batch request")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	outputDir := flags.String("output-dir", "", "Report output directory")
	timeoutSeconds := flags.Int("timeout-seconds", 3, "Timeout per API Case")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	var actions stringListFlag
	var signals stringListFlag
	var changes stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	flags.Var(&actions, "action", "Only select ready cases with this suggested action; repeat for multiple actions")
	flags.Var(&signals, "signal", "Changed path, interface text, workflow text, tag, or case text; repeat for multiple signals")
	flags.Var(&changes, "change", "Alias for --signal; repeat for multiple changes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *timeoutSeconds <= 0 {
		return errors.New("--timeout-seconds must be greater than zero")
	}
	started := time.Now()
	bundle, sourceStore, resolvedStoreURL, cleanup, err := loadRequiredInterfaceNodeReportBundleFromStoreFlags(ctx, *profilePath, *profileHome, *storeRef, *storeURL)
	if err != nil {
		return err
	}
	defer cleanup()
	filters := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	impactSignals := append(signals.Values(), changes.Values()...)
	impact, err := casesuite.Impact(ctx, bundle, sourceStore, caseSuiteFilter(filters), casesuite.ImpactOptions{
		Signals: impactSignals,
		Plan: casesuite.PlanOptions{
			RequestID:      *requestID,
			Actions:        actions.Values(),
			BaseURL:        *baseURL,
			TimeoutSeconds: *timeoutSeconds,
		},
	})
	if err != nil {
		return err
	}
	cases := apiCasesByIDs(bundle.APICases, impact.BatchRequest.CaseIDs)
	if len(cases) == 0 {
		return errors.New("no ready impacted API cases selected for execution")
	}
	derived := deriveCaseSuiteConfigs(bundle, cases)
	bundle.TemplateConfigs = mergeTemplateConfigs(bundle.TemplateConfigs, derived)
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "case-suite-impact."+safeReportID(strings.Join(impact.Signals, "-"))+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeCaseSuiteReport(ctx, bundle, cases, derived, sourceStore, resolvedStoreURL, filters, *baseURL, absOutputDir, *timeoutSeconds)
	if err != nil {
		return err
	}
	out := caseSuiteImpactExecutionReport{
		OK:        impact.OK && report.OK,
		Impact:    impact,
		Report:    report,
		ElapsedMs: time.Since(started).Milliseconds(),
	}
	if *jsonOutput {
		return writeIndentedJSON(out)
	}
	printCaseSuiteImpactExecutionReport(out)
	return nil
}

func apiCasesByIDs(cases []profile.APICase, ids []string) []profile.APICase {
	byID := map[string]profile.APICase{}
	for _, item := range cases {
		byID[item.ID] = item
	}
	out := make([]profile.APICase, 0, len(ids))
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			out = append(out, item)
		}
	}
	return out
}

func printCaseSuiteImpactExecutionReport(report caseSuiteImpactExecutionReport) {
	fmt.Println("Case Suite Impact Report")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Selected: %d Passed: %d Failed: %d\n", report.Impact.Counts.Selected, report.Report.Counts.Passed, report.Report.Counts.Failed)
	for _, item := range report.Report.Results {
		fmt.Printf("- %s [%s]", item.CaseID, item.Status)
		if item.CaseRunID != "" {
			fmt.Printf(" %s", item.CaseRunID)
		}
		fmt.Println()
	}
}

type caseSuiteReport struct {
	OK             bool                          `json:"ok"`
	ProfileID      string                        `json:"profileId"`
	Title          string                        `json:"title"`
	ReportURL      string                        `json:"reportUrl"`
	JSONReportURL  string                        `json:"jsonReportUrl"`
	JUnitReportURL string                        `json:"junitReportUrl,omitempty"`
	ElapsedMs      int64                         `json:"elapsedMs"`
	GeneratedAt    time.Time                     `json:"generatedAt"`
	Filters        caseListFilter                `json:"filters"`
	Counts         interfaceNodeCaseReportCounts `json:"counts"`
	Results        []caseSuiteReportItem         `json:"results"`
	Warnings       []string                      `json:"warnings,omitempty"`
	SourceStoreURL string                        `json:"sourceStoreUrl,omitempty"`
}

type caseSuiteReportItem struct {
	CaseID      string   `json:"caseId"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	NodeID      string   `json:"nodeId,omitempty"`
	NodeName    string   `json:"nodeName,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	RunID       string   `json:"runId,omitempty"`
	CaseRunID   string   `json:"caseRunId,omitempty"`
	ViewerURL   string   `json:"viewerUrl,omitempty"`
	DetailURL   string   `json:"detailUrl,omitempty"`
	Status      string   `json:"status"`
	HTTPCode    int      `json:"httpCode,omitempty"`
	ElapsedMs   int64    `json:"elapsedMs"`
	Method      string   `json:"method,omitempty"`
	Path        string   `json:"path,omitempty"`
	FullURL     string   `json:"fullUrl,omitempty"`
	BaseURL     string   `json:"baseUrl,omitempty"`
	Error       string   `json:"error,omitempty"`
	BodyPreview string   `json:"bodyPreview,omitempty"`
}

func runCaseSuiteReport(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case suite report", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	filter := flags.String("filter", "", "Filter by id, display name, scenario, description, tag, owner, or priority")
	nodeID := flags.String("node", "", "Only include cases attached to this interface node id")
	status := flags.String("status", "active", "Only include cases with this status")
	owner := flags.String("owner", "", "Only include cases owned by this value")
	priority := flags.String("priority", "", "Only include cases with this priority")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	outputDir := flags.String("output-dir", "", "Report output directory")
	timeoutSeconds := flags.Int("timeout-seconds", 3, "Timeout per API Case")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	var tags stringListFlag
	flags.Var(&tags, "tag", "Only include cases with this tag; repeat for multiple tags")
	if err := flags.Parse(args); err != nil {
		return err
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
	filters := caseListFilter{
		Filter:   *filter,
		NodeID:   *nodeID,
		Tags:     tags.Values(),
		Status:   *status,
		Owner:    *owner,
		Priority: *priority,
	}
	cases := selectedCaseSuiteCases(bundle, filters)
	if len(cases) == 0 {
		return errors.New("no API cases matched selector")
	}
	derived := deriveCaseSuiteConfigs(bundle, cases)
	bundle.TemplateConfigs = mergeTemplateConfigs(bundle.TemplateConfigs, derived)
	if strings.TrimSpace(*outputDir) == "" {
		*outputDir = filepath.Join(".runtime", "reports", "case-suite."+safeReportID(caseSuiteFilterSlug(filters))+"."+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	report, err := executeCaseSuiteReport(ctx, bundle, cases, derived, sourceStore, resolvedStoreURL, filters, *baseURL, absOutputDir, *timeoutSeconds)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printCaseSuiteReport(report)
	return nil
}

func selectedCaseSuiteCases(bundle profile.Bundle, filters caseListFilter) []profile.APICase {
	return casesuite.SelectCases(bundle, caseSuiteFilter(filters))
}

func deriveCaseSuiteConfigs(bundle profile.Bundle, cases []profile.APICase) []profile.TemplateConfig {
	nodesByID := make(map[string]profile.InterfaceNode, len(bundle.InterfaceNodes))
	for _, node := range bundle.InterfaceNodes {
		nodesByID[node.ID] = node
	}
	casesByNode := map[string][]profile.APICase{}
	for _, item := range cases {
		casesByNode[item.NodeID] = append(casesByNode[item.NodeID], item)
	}
	nodeIDs := make([]string, 0, len(casesByNode))
	for nodeID := range casesByNode {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	out := make([]profile.TemplateConfig, 0)
	selected := map[string]bool{}
	for _, item := range cases {
		selected[item.ID] = true
	}
	for _, nodeID := range nodeIDs {
		node, ok := nodesByID[nodeID]
		if !ok {
			continue
		}
		for _, config := range deriveInterfaceNodeCaseConfigs(bundle, node, interfaceNodeReportCases(bundle.APICases, nodeID)) {
			if selected[config.ScopeID] {
				out = append(out, config)
			}
		}
	}
	return out
}

func executeCaseSuiteReport(ctx context.Context, bundle profile.Bundle, cases []profile.APICase, derived []profile.TemplateConfig, sourceStore store.Store, sourceStoreURL string, filters caseListFilter, baseURL string, outputDir string, timeoutSeconds int) (caseSuiteReport, error) {
	started := time.Now()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return caseSuiteReport{}, err
	}
	runtime, err := requiredReportStore(sourceStore)
	if err != nil {
		return caseSuiteReport{}, err
	}
	if err := runtime.ReplaceProfileCatalog(ctx, profilecatalog.FromBundle(bundle, time.Now().UTC())); err != nil {
		return caseSuiteReport{}, err
	}
	handler := controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: runtime})
	server := httptest.NewServer(handler)
	defer server.Close()
	caseIDs := make([]string, 0, len(cases))
	for _, item := range cases {
		caseIDs = append(caseIDs, item.ID)
	}
	requestPayload := map[string]any{"caseIds": caseIDs, "baseUrl": baseURL, "timeoutSeconds": timeoutSeconds}
	rawRequest, _ := json.Marshal(requestPayload)
	response, err := http.Post(server.URL+"/api/test-kit/run-batch", "application/json", strings.NewReader(string(rawRequest)))
	if err != nil {
		return caseSuiteReport{}, err
	}
	defer response.Body.Close()
	var rawBatch map[string]any
	if err := json.NewDecoder(response.Body).Decode(&rawBatch); err != nil {
		return caseSuiteReport{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return caseSuiteReport{}, fmt.Errorf("case suite batch failed with http status %d", response.StatusCode)
	}
	report := caseSuiteReport{
		OK:             boolFromReportAny(rawBatch["ok"]),
		ProfileID:      bundle.ID,
		Title:          "Case Suite Report",
		ElapsedMs:      time.Since(started).Milliseconds(),
		GeneratedAt:    time.Now().UTC(),
		Filters:        normalizeCaseListFilter(filters),
		SourceStoreURL: sourceStoreURL,
		Counts: interfaceNodeCaseReportCounts{
			Total:          len(cases),
			DerivedConfigs: len(derived),
		},
	}
	report.Results = caseSuiteReportItems(interfaceNodeCaseReportItems(rawBatch["results"]), cases, bundle.InterfaceNodes)
	for _, item := range report.Results {
		if item.Status == store.StatusPassed {
			report.Counts.Passed++
		} else {
			report.Counts.Failed++
		}
	}
	report.OK = report.Counts.Total > 0 && report.Counts.Failed == 0
	if err := writeCaseSuiteReportFiles(outputDir, &report); err != nil {
		return caseSuiteReport{}, err
	}
	return report, nil
}

func caseSuiteReportItems(results []interfaceNodeCaseReportItem, cases []profile.APICase, nodes []profile.InterfaceNode) []caseSuiteReportItem {
	casesByID := make(map[string]profile.APICase, len(cases))
	for _, item := range cases {
		casesByID[item.ID] = item
	}
	nodesByID := make(map[string]profile.InterfaceNode, len(nodes))
	for _, node := range nodes {
		nodesByID[node.ID] = node
	}
	out := make([]caseSuiteReportItem, 0, len(results))
	for _, result := range results {
		apiCase := casesByID[result.CaseID]
		node := nodesByID[apiCase.NodeID]
		out = append(out, caseSuiteReportItem{
			CaseID:      result.CaseID,
			Title:       result.Title,
			Description: apiCase.Description,
			NodeID:      apiCase.NodeID,
			NodeName:    firstNonEmpty(node.DisplayName, apiCase.NodeID),
			Tags:        append([]string(nil), apiCase.Tags...),
			Priority:    apiCase.Priority,
			Owner:       apiCase.Owner,
			RunID:       result.RunID,
			CaseRunID:   result.CaseRunID,
			ViewerURL:   result.ViewerURL,
			DetailURL:   result.DetailURL,
			Status:      result.Status,
			HTTPCode:    result.HTTPCode,
			ElapsedMs:   result.ElapsedMs,
			Method:      result.Method,
			Path:        result.Path,
			FullURL:     result.FullURL,
			BaseURL:     result.BaseURL,
			Error:       result.Error,
			BodyPreview: result.BodyPreview,
		})
	}
	return out
}

func writeCaseSuiteReportFiles(outputDir string, report *caseSuiteReport) error {
	jsonPath := filepath.Join(outputDir, "report.json")
	htmlPath := filepath.Join(outputDir, "report.html")
	junitPath := filepath.Join(outputDir, "report.junit.xml")
	report.JSONReportURL = jsonPath
	report.ReportURL = htmlPath
	report.JUnitReportURL = junitPath
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(htmlPath, []byte(renderCaseSuiteReportHTML(*report)), 0o644); err != nil {
		return err
	}
	junitRaw, err := renderCaseSuiteJUnit(*report)
	if err != nil {
		return err
	}
	return os.WriteFile(junitPath, junitRaw, 0o644)
}

func renderCaseSuiteJUnit(report caseSuiteReport) ([]byte, error) {
	cases := make([]junit.Case, 0, len(report.Results))
	for _, item := range report.Results {
		cases = append(cases, junit.Case{
			Name:           firstNonEmpty(item.CaseID, item.Title),
			ClassName:      firstNonEmpty(item.NodeID, item.NodeName),
			Status:         item.Status,
			TimeSeconds:    float64(item.ElapsedMs) / 1000,
			FailureMessage: item.Error,
			Output:         firstNonEmpty(item.Error, item.BodyPreview),
		})
	}
	return junit.Render(junit.Suite{Name: firstNonEmpty(report.Title, "Case Suite Report"), Cases: cases})
}

func renderCaseSuiteReportHTML(report caseSuiteReport) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>Case Suite Report</title><style>`)
	b.WriteString(`body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#111827;background:#f8fafc}main{max-width:1320px;margin:auto}h1{font-size:24px;margin:0 0 4px}.meta{color:#4b5563;margin-bottom:16px}.summary{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.pill{border:1px solid #d1d5db;background:white;border-radius:6px;padding:6px 10px;font-size:13px}.ok{color:#047857}.bad{color:#b91c1c}table{width:100%;border-collapse:collapse;background:white;border:1px solid #d1d5db}th,td{border-bottom:1px solid #e5e7eb;text-align:left;vertical-align:top;padding:7px 8px;font-size:13px}th{background:#f3f4f6;color:#374151}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.wrap{word-break:break-all}.small{font-size:12px;color:#6b7280}`)
	b.WriteString(`</style></head><body><main>`)
	b.WriteString(`<h1>Case Suite Report</h1>`)
	b.WriteString(`<div class="meta">` + html.EscapeString(report.ProfileID) + `</div><div class="summary">`)
	b.WriteString(reportPill("status", statusText(report.OK)))
	b.WriteString(reportPill("total", strconv.Itoa(report.Counts.Total)))
	b.WriteString(reportPill("passed", strconv.Itoa(report.Counts.Passed)))
	b.WriteString(reportPill("failed", strconv.Itoa(report.Counts.Failed)))
	b.WriteString(reportPill("derived configs", strconv.Itoa(report.Counts.DerivedConfigs)))
	b.WriteString(reportPill("elapsed", fmt.Sprintf("%d ms", report.ElapsedMs)))
	if len(report.Filters.Tags) > 0 {
		b.WriteString(reportPill("tags", strings.Join(report.Filters.Tags, ",")))
	}
	if report.Filters.Owner != "" {
		b.WriteString(reportPill("owner", report.Filters.Owner))
	}
	if report.Filters.Priority != "" {
		b.WriteString(reportPill("priority", report.Filters.Priority))
	}
	b.WriteString(`</div><table><thead><tr><th>#</th><th>Case</th><th>Node</th><th>Maintainer</th><th>Status</th><th>HTTP</th><th>Elapsed</th><th>Evidence</th><th>Request</th><th>Response</th><th>Error</th></tr></thead><tbody>`)
	for index, item := range report.Results {
		statusClass := "bad"
		if item.Status == store.StatusPassed {
			statusClass = "ok"
		}
		b.WriteString(`<tr><td class="mono">` + strconv.Itoa(index+1) + `</td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.Title) + `</div><div class="mono small wrap">` + html.EscapeString(item.CaseID) + `</div>`)
		if item.Description != "" {
			b.WriteString(`<div class="small">` + html.EscapeString(item.Description) + `</div>`)
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.NodeName) + `</div><div class="mono small wrap">` + html.EscapeString(item.NodeID) + `</div></td>`)
		b.WriteString(`<td><div>` + html.EscapeString(item.Owner) + `</div><div class="small">` + html.EscapeString(item.Priority) + `</div><div class="small">` + html.EscapeString(strings.Join(item.Tags, ", ")) + `</div></td>`)
		b.WriteString(`<td class="` + statusClass + `">` + html.EscapeString(item.Status) + `</td>`)
		b.WriteString(`<td class="mono">` + strconv.Itoa(item.HTTPCode) + `</td>`)
		b.WriteString(`<td class="mono">` + fmt.Sprintf("%d ms", item.ElapsedMs) + `</td>`)
		b.WriteString(`<td class="mono wrap">`)
		if item.DetailURL != "" {
			b.WriteString(`<a href="` + html.EscapeString(item.DetailURL) + `">caseRunId</a><br>`)
		}
		b.WriteString(html.EscapeString(item.CaseRunID))
		b.WriteString(`</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(strings.TrimSpace(item.Method+" "+item.FullURL)) + `</td>`)
		b.WriteString(`<td class="mono wrap">` + html.EscapeString(item.BodyPreview) + `</td>`)
		b.WriteString(`<td class="wrap">` + html.EscapeString(item.Error) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></main></body></html>`)
	return b.String()
}

func printCaseSuiteReport(report caseSuiteReport) {
	fmt.Println("Case Suite Report")
	fmt.Printf("OK: %t\n", report.OK)
	fmt.Printf("Total: %d Passed: %d Failed: %d\n", report.Counts.Total, report.Counts.Passed, report.Counts.Failed)
	fmt.Printf("Derived Configs: %d\n", report.Counts.DerivedConfigs)
	fmt.Printf("Elapsed: %d ms\n", report.ElapsedMs)
	fmt.Printf("Report: %s\n", report.ReportURL)
}

func caseSuiteFilterSlug(filters caseListFilter) string {
	filters = normalizeCaseListFilter(filters)
	parts := []string{filters.Filter, filters.NodeID, filters.Status, filters.Owner, filters.Priority}
	parts = append(parts, filters.Tags...)
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			return part
		}
	}
	return "all"
}

func caseList(ctx context.Context, bundle profile.Bundle, runtime store.Store, filters caseListFilter) caseListReport {
	cases := append([]profile.APICase(nil), bundle.APICases...)
	sort.SliceStable(cases, func(i, j int) bool {
		if cases[i].NodeID != cases[j].NodeID {
			return cases[i].NodeID < cases[j].NodeID
		}
		if cases[i].SortOrder != cases[j].SortOrder {
			return cases[i].SortOrder < cases[j].SortOrder
		}
		return cases[i].ID < cases[j].ID
	})
	executionConfigs := caseExecutionConfigSet(ctx, runtime)
	report := caseListReport{OK: true, ProfileID: bundle.ID, Filters: normalizeCaseListFilter(filters)}
	for _, item := range cases {
		if !matchesCaseFilters(item, filters) {
			continue
		}
		report.Items = append(report.Items, caseListItem{
			ID:                   item.ID,
			DisplayName:          item.DisplayName,
			Description:          item.Description,
			NodeID:               item.NodeID,
			CaseType:             item.CaseType,
			Scenario:             item.Scenario,
			Tags:                 append([]string(nil), item.Tags...),
			Priority:             item.Priority,
			Owner:                item.Owner,
			Status:               effectiveCaseStatus(item),
			RequiredForAdmission: item.RequiredForAdmission,
			SortOrder:            item.SortOrder,
			HasRunnableFile:      strings.TrimSpace(item.CasePath) != "",
			HasExecutionConfig:   executionConfigs[item.ID],
		})
	}
	report.Count = len(report.Items)
	return report
}

func normalizeCaseListFilter(filters caseListFilter) caseListFilter {
	filters.Filter = strings.TrimSpace(filters.Filter)
	filters.NodeID = strings.TrimSpace(filters.NodeID)
	filters.Status = strings.TrimSpace(filters.Status)
	filters.Owner = strings.TrimSpace(filters.Owner)
	filters.Priority = strings.TrimSpace(filters.Priority)
	filters.Tags = normalizeStringList(filters.Tags)
	return filters
}

func matchesCaseFilters(item profile.APICase, filters caseListFilter) bool {
	filters = normalizeCaseListFilter(filters)
	if filters.NodeID != "" && item.NodeID != filters.NodeID {
		return false
	}
	if filters.Status != "" && !strings.EqualFold(effectiveCaseStatus(item), filters.Status) {
		return false
	}
	if filters.Owner != "" && !strings.EqualFold(strings.TrimSpace(item.Owner), filters.Owner) {
		return false
	}
	if filters.Priority != "" && !strings.EqualFold(strings.TrimSpace(item.Priority), filters.Priority) {
		return false
	}
	if len(filters.Tags) > 0 && !caseHasAllTags(item.Tags, filters.Tags) {
		return false
	}
	return matchesDiscoveryFilter(filters.Filter, item.ID, item.DisplayName, item.Scenario, item.Description, item.Owner, item.Priority, strings.Join(item.Tags, " "))
}

func effectiveCaseStatus(item profile.APICase) string {
	status := strings.TrimSpace(item.Status)
	if status == "" {
		return "active"
	}
	return status
}

func caseHasAllTags(actual []string, required []string) bool {
	actualSet := map[string]bool{}
	for _, tag := range actual {
		normalized := normalizedDiscoveryText(tag)
		if normalized != "" {
			actualSet[normalized] = true
		}
	}
	for _, tag := range required {
		normalized := normalizedDiscoveryText(tag)
		if normalized != "" && !actualSet[normalized] {
			return false
		}
	}
	return true
}

func caseExecutionConfigSet(ctx context.Context, runtime store.Store) map[string]bool {
	out := map[string]bool{}
	if runtime == nil {
		return out
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return out
	}
	for _, config := range catalog.TemplateConfigs {
		if config.ScopeType == "case" && strings.TrimSpace(config.ScopeID) != "" {
			out[strings.TrimSpace(config.ScopeID)] = true
			continue
		}
		var payload struct {
			CaseID string `json:"caseId"`
		}
		if json.Unmarshal([]byte(config.ConfigJSON), &payload) == nil && strings.TrimSpace(payload.CaseID) != "" {
			out[strings.TrimSpace(payload.CaseID)] = true
		}
	}
	return out
}

func runCaseIncompleteBatches(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case incomplete-batches", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	jsonOutput := flags.Bool("json", false, "Print JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	s, err := openStore(ctx, resolvedStoreURL)
	if err != nil {
		return err
	}
	defer s.Close()

	bundle, err := incompleteCaseBundle(ctx, strings.TrimSpace(*profilePath), s)
	if err != nil {
		return err
	}
	report, err := incompleteCaseReportForStore(ctx, bundle, s)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	printIncompleteCaseReport(report)
	return nil
}

func incompleteCaseBundle(ctx context.Context, profilePath string, runtime store.Store) (profile.Bundle, error) {
	if profilePath != "" {
		return profile.Load(profilePath)
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		return profile.Bundle{}, err
	}
	return profilecatalog.ToBundle(catalog), nil
}

type incompleteCaseReport struct {
	OK       bool                 `json:"ok"`
	Count    int                  `json:"count"`
	Items    []incompleteCaseItem `json:"items"`
	Warnings []string             `json:"warnings"`
}

type incompleteCaseItem struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Reason           string `json:"reason"`
	Source           string `json:"source"`
	Message          string `json:"message"`
	SuggestedCommand string `json:"suggestedCommand"`
}

func incompleteCaseReportForStore(ctx context.Context, bundle profile.Bundle, s store.Store) (incompleteCaseReport, error) {
	passed, latest, err := apiCaseRunStatusByCase(ctx, s)
	if err != nil {
		return incompleteCaseReport{}, err
	}
	items := make([]incompleteCaseItem, 0)
	for _, item := range bundle.APICases {
		if strings.TrimSpace(item.ID) == "" || passed[item.ID] {
			continue
		}
		reason := "not-run"
		if status := latest[item.ID]; status != "" {
			reason = "latest-" + status
		}
		items = append(items, incompleteCaseItem{
			ID:               item.ID,
			Title:            firstNonEmpty(item.DisplayName, item.ID),
			Reason:           reason,
			Source:           "profile:" + bundle.ID,
			Message:          "no passed Store run found for this API Case",
			SuggestedCommand: apiCaseSuggestedCommand(item),
		})
	}
	return incompleteCaseReport{
		OK:       true,
		Count:    len(items),
		Items:    items,
		Warnings: []string{},
	}, nil
}

func apiCaseRunStatusByCase(ctx context.Context, s store.Store) (map[string]bool, map[string]string, error) {
	runs, err := s.ListRuns(ctx)
	if err != nil {
		return nil, nil, err
	}
	passed := map[string]bool{}
	latest := map[string]string{}
	for i := len(runs) - 1; i >= 0; i-- {
		caseRuns, err := s.ListAPICaseRuns(ctx, runs[i].ID)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range caseRuns {
			if latest[item.CaseID] == "" {
				latest[item.CaseID] = item.Status
			}
			if strings.EqualFold(item.Status, store.StatusPassed) {
				passed[item.CaseID] = true
			}
		}
	}
	return passed, latest, nil
}

func apiCaseSuggestedCommand(item profile.APICase) string {
	casePath := strings.TrimSpace(item.CasePath)
	if casePath == "" {
		return ""
	}
	parts := []string{"agent-testbench case run --case " + strconv.Quote(casePath)}
	if strings.TrimSpace(item.BaseURL) != "" {
		parts = append(parts, "--base-url "+strconv.Quote(item.BaseURL))
	}
	if strings.TrimSpace(item.EvidenceDir) != "" {
		parts = append(parts, "--evidence-dir "+strconv.Quote(item.EvidenceDir))
	}
	return strings.Join(parts, " ")
}

func printIncompleteCaseReport(report incompleteCaseReport) {
	fmt.Printf("Incomplete API Cases: %d\n", report.Count)
	for _, item := range report.Items {
		fmt.Printf("- %s [%s]\n", item.ID, item.Reason)
		if item.SuggestedCommand != "" {
			fmt.Printf("  %s\n", item.SuggestedCommand)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func runCaseRun(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("case run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	overrides := mapFlag{}
	casePath := flags.String("case", "", "API case file path")
	caseID := flags.String("case-id", "", "API case id from the active Store catalog")
	baseURL := flags.String("base-url", "", "Base URL for live request execution")
	evidenceDir := flags.String("evidence-dir", filepath.Join(".runtime", "cases"), "Evidence output directory")
	runID := flags.String("run-id", "", "Run id")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	profileID := flags.String("profile", "default", "Profile id for store records")
	timeoutSeconds := flags.Int("timeout-seconds", 0, "Request timeout in seconds for Store catalog case execution")
	dryRun := flags.Bool("dry-run", false, "Preview the file-backed case run without sending HTTP, writing Evidence, or indexing Store records")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	flags.Var(&overrides, "override", "Request body override as key=value; repeat for multiple values")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dryRun {
		if strings.TrimSpace(*caseID) != "" {
			return errors.New("case run --dry-run currently supports --case PATH")
		}
		if strings.TrimSpace(*casePath) == "" {
			return errors.New("case run --dry-run requires --case PATH")
		}
		plan, err := apicase.Plan(apicase.RunOptions{
			CasePath:    *casePath,
			BaseURL:     *baseURL,
			EvidenceDir: *evidenceDir,
			RunID:       *runID,
			Overrides:   overrides.Values(),
		})
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeIndentedJSON(plan)
		}
		printCaseRunDryRun(plan)
		return nil
	}
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(*storeRef, *storeURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*caseID) != "" {
		result, err := runStoreCatalogCase(ctx, resolvedStoreURL, *profileID, *caseID, *baseURL, *evidenceDir, *runID, *timeoutSeconds, overrides.Values())
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeIndentedJSON(result)
		}
		printStoreCatalogCaseRun(result)
		return nil
	}
	if strings.TrimSpace(*casePath) == "" {
		return errors.New("case run requires --case PATH or --case-id ID")
	}
	result, err := apicase.Run(ctx, apicase.RunOptions{
		CasePath:    *casePath,
		BaseURL:     *baseURL,
		EvidenceDir: *evidenceDir,
		RunID:       *runID,
		Overrides:   overrides.Values(),
	})
	if err != nil {
		return err
	}
	if err := indexCaseRun(ctx, resolvedStoreURL, *profileID, result); err != nil {
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(result)
	}
	fmt.Printf("Case Run: %s\n", result.RunID)
	fmt.Printf("Case: %s\n", result.CaseID)
	fmt.Printf("Status: %s\n", result.Status)
	fmt.Printf("Evidence: %s\n", result.EvidencePath)
	return nil
}

func printCaseRunDryRun(plan apicase.DryRunPlan) {
	fmt.Printf("Case Run Dry Run: %s\n", plan.RunID)
	fmt.Printf("Case: %s\n", plan.CaseID)
	fmt.Printf("Request: %s %s\n", plan.Request.Method, plan.Request.Path)
	if plan.Request.URL != "" {
		fmt.Printf("URL: %s\n", plan.Request.URL)
	}
	fmt.Printf("Headers: %d\n", len(plan.Request.HeaderKeys))
	fmt.Printf("Body: %t", plan.Request.HasBody)
	if len(plan.Request.BodyKeys) > 0 {
		fmt.Printf(" keys=%s", strings.Join(plan.Request.BodyKeys, ","))
	}
	fmt.Println()
	if len(plan.Assertions.ExpectedStatusCodes) > 0 {
		fmt.Printf("Expected Status: %s\n", intListString(plan.Assertions.ExpectedStatusCodes))
	}
	if plan.Assertions.ResponseContainsCount > 0 {
		fmt.Printf("Response Contains Checks: %d\n", plan.Assertions.ResponseContainsCount)
	}
	fmt.Printf("Will Send HTTP: %t\n", plan.Effects.HTTPRequest)
	fmt.Printf("Will Write Evidence: %t\n", plan.Effects.WritesEvidence)
	fmt.Printf("Will Write Store: %t\n", plan.Effects.WritesStore)
	fmt.Printf("Planned Evidence: %s\n", plan.Effects.PlannedEvidencePath)
	for _, warning := range plan.Warnings {
		fmt.Printf("Warning: %s\n", warning)
	}
}

func intListString(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ",")
}

func runStoreCatalogCase(ctx context.Context, storeURL string, profileID string, caseID string, baseURL string, evidenceDir string, runID string, timeoutSeconds int, overrides map[string]any) (map[string]any, error) {
	if strings.TrimSpace(storeURL) == "" {
		return nil, errNoActiveStoreConfigured
	}
	runtime, err := openStore(ctx, storeURL)
	if err != nil {
		return nil, err
	}
	defer runtime.Close()
	handler := controlplane.NewWithStore(profile.Bundle{ID: strings.TrimSpace(profileID)}, runtime)
	server := httptest.NewServer(handler)
	defer server.Close()
	payload := map[string]any{
		"caseId":      strings.TrimSpace(caseID),
		"baseUrl":     strings.TrimSpace(baseURL),
		"evidenceDir": strings.TrimSpace(evidenceDir),
		"runId":       strings.TrimSpace(runID),
	}
	if timeoutSeconds > 0 {
		payload["timeoutSeconds"] = timeoutSeconds
	}
	if len(overrides) > 0 {
		payload["overrides"] = overrides
	}
	result, err := postReportMap(server.URL+"/api/test-kit/run", payload)
	if err != nil {
		return nil, err
	}
	status := intFromReportAny(result["httpStatus"])
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("case run failed with http status %d: %s", status, valueString(result["error"]))
	}
	return result, nil
}

func printStoreCatalogCaseRun(result map[string]any) {
	fmt.Printf("Case Run: %s\n", valueString(result["runId"]))
	fmt.Printf("Case: %s\n", valueString(result["caseId"]))
	fmt.Printf("Status: %s\n", valueString(result["status"]))
	if summary := mapFromReportAny(result["summary"]); len(summary) > 0 {
		if target := valueString(summary["targetBaseUrl"]); target != "" {
			fmt.Printf("Target: %s\n", target)
		}
	}
	fmt.Printf("Evidence: %s\n", valueString(result["viewerUrl"]))
}

type mapFlag map[string]any

func (m *mapFlag) String() string {
	if m == nil || len(*m) == 0 {
		return ""
	}
	raw, _ := json.Marshal(*m)
	return string(raw)
}

func (m *mapFlag) Set(value string) error {
	key, parsed, ok := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return fmt.Errorf("override must use key=value")
	}
	if *m == nil {
		*m = map[string]any{}
	}
	(*m)[key] = parsed
	return nil
}

func (m mapFlag) Values() map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for key, value := range m {
		out[key] = value
	}
	return out
}

type stringListFlag []string

func (s *stringListFlag) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	*s = append(*s, value)
	return nil
}

func (s stringListFlag) Values() []string {
	return normalizeStringList([]string(s))
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func indexCaseRun(ctx context.Context, storeURL string, profileID string, result apicase.RunResult) error {
	s, err := openStore(ctx, storeURL)
	if err != nil {
		return err
	}
	defer s.Close()

	now := time.Now().UTC()
	startedAt := runResultTime(result.StartedAt, now)
	finishedAt := runResultTime(result.FinishedAt, now)
	if finishedAt.Before(startedAt) {
		finishedAt = startedAt
	}
	requestSummary, assertionSummary, err := apiCaseRunSummaries(result.EvidencePath)
	if err != nil {
		return err
	}
	if _, err := s.CreateRun(ctx, store.Run{
		ID:           result.RunID,
		ProfileID:    profileID,
		WorkflowID:   "",
		Status:       result.Status,
		EvidenceRoot: result.EvidencePath,
		SummaryJSON:  caseRunSummaryJSON(result),
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		CreatedAt:    startedAt,
		UpdatedAt:    finishedAt,
	}); err != nil {
		return err
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   result.RunID + ".case",
		RunID:                result.RunID,
		CaseID:               result.CaseID,
		Status:               result.Status,
		RequestSummaryJSON:   requestSummary,
		AssertionSummaryJSON: assertionSummary,
		StartedAt:            startedAt,
		FinishedAt:           finishedAt,
		CreatedAt:            startedAt,
	}); err != nil {
		return err
	}
	for _, name := range []string{"case.json", "request.json", "response.json", "assertions.json", "summary.json"} {
		path := filepath.Join(result.EvidencePath, name)
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		summary, err := evidenceSummary(path, strings.TrimSuffix(name, ".json"))
		if err != nil {
			return err
		}
		if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
			ID:        result.RunID + "." + name,
			RunID:     result.RunID,
			CaseRunID: result.RunID + ".case",
			Kind:      strings.TrimSuffix(name, ".json"),
			URI:       path,
			MediaType: "application/json",
			Summary:   summary,
			CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func caseRunSummaryJSON(result apicase.RunResult) string {
	path := filepath.Join(result.EvidencePath, "summary.json")
	if raw, err := os.ReadFile(path); err == nil && json.Valid(raw) {
		return strings.TrimSpace(string(raw))
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func runResultTime(value string, defaultValue time.Time) time.Time {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return defaultValue
	}
	return parsed.UTC()
}

type requestSummary struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	HeaderCount int    `json:"headerCount"`
	HasBody     bool   `json:"hasBody"`
}

type assertionSummary struct {
	Status     string `json:"status"`
	ErrorCount int    `json:"errorCount"`
}

type responseSummary struct {
	StatusCode  int `json:"statusCode"`
	HeaderCount int `json:"headerCount"`
	BodyBytes   int `json:"bodyBytes"`
}

func apiCaseRunSummaries(evidencePath string) (string, string, error) {
	request, err := requestSummaryJSON(filepath.Join(evidencePath, "request.json"))
	if err != nil {
		return "", "", err
	}
	assertions, err := assertionSummaryJSON(filepath.Join(evidencePath, "assertions.json"))
	if err != nil {
		return "", "", err
	}
	return request, assertions, nil
}

func evidenceSummary(path string, kind string) (string, error) {
	switch kind {
	case "request":
		return requestSummaryJSON(path)
	case "response":
		return responseSummaryJSON(path)
	case "assertions":
		return assertionSummaryJSON(path)
	default:
		return "", nil
	}
}

func requestSummaryJSON(path string) (string, error) {
	var request apicase.Request
	if err := readJSONFile(path, &request); err != nil {
		return "", err
	}
	return compactJSON(requestSummary{
		Method:      strings.ToUpper(request.Method),
		Path:        request.Path,
		HeaderCount: len(request.Headers),
		HasBody:     request.Body != nil,
	})
}

func responseSummaryJSON(path string) (string, error) {
	var response apicase.ResponseEvidence
	if err := readJSONFile(path, &response); err != nil {
		return "", err
	}
	return compactJSON(responseSummary{
		StatusCode:  response.StatusCode,
		HeaderCount: len(response.Headers),
		BodyBytes:   len([]byte(response.Body)),
	})
}

func assertionSummaryJSON(path string) (string, error) {
	var assertions apicase.AssertionEvidence
	if err := readJSONFile(path, &assertions); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return compactJSON(assertionSummary{Status: "not-run"})
		}
		return "", err
	}
	return compactJSON(assertionSummary{
		Status:     assertions.Status,
		ErrorCount: len(assertions.Errors),
	})
}

func readJSONFile(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func compactJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func runServe(args []string) error {
	cfg, err := serveConfigFromArgs(args)
	if err != nil {
		return err
	}
	handler, cleanup, err := serveHandler(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	addr := cfg.host + ":" + strconv.Itoa(cfg.port)
	fmt.Printf("AgentTestBench listening on http://%s\n", addr)
	return http.ListenAndServe(addr, handler)
}

type serveConfig struct {
	profilePath     string
	profileHome     string
	host            string
	port            int
	storeRef        string
	storeURL        string
	traceGraphQLURL string
}

func serveHandlerFromArgs(args []string) (http.Handler, func() error, error) {
	cfg, err := serveConfigFromArgs(args)
	if err != nil {
		return nil, nil, err
	}
	return serveHandler(cfg)
}

func serveConfigFromArgs(args []string) (serveConfig, error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path or installed profile id")
	profileHome := flags.String("profile-home", "", "Installed profile bundle home")
	host := flags.String("host", "127.0.0.1", "HTTP host")
	port := flags.Int("port", 18191, "HTTP port")
	storeRef := flags.String("store", "", "Named Store config or Store DSN")
	storeURL := flags.String("store-url", "", legacyStoreURLFlagHelp)
	traceGraphQLURL := flags.String("trace-graphql-url", os.Getenv("AGENT_TESTBENCH_TRACE_GRAPHQL_URL"), "Trace provider GraphQL URL")
	if err := flags.Parse(args); err != nil {
		return serveConfig{}, err
	}
	return serveConfig{profilePath: *profilePath, profileHome: *profileHome, host: *host, port: *port, storeRef: *storeRef, storeURL: *storeURL, traceGraphQLURL: *traceGraphQLURL}, nil
}

func serveHandler(cfg serveConfig) (http.Handler, func() error, error) {
	resolvedStoreURL, err := resolveRequiredDailyStoreReference(cfg.storeRef, cfg.storeURL)
	if err != nil {
		return nil, nil, err
	}
	storeLabel := resolvedStoreURL
	storeInfo := serveStoreInfo(cfg, resolvedStoreURL)
	runtime, err := storeopen.Open(context.Background(), resolvedStoreURL)
	if err != nil {
		return nil, nil, err
	}
	ctx := context.Background()
	if strings.TrimSpace(cfg.profilePath) != "" {
		profilePath, err := resolveProfileReference(cfg.profilePath, cfg.profileHome)
		if err != nil {
			_ = runtime.Close()
			return nil, nil, err
		}
		if _, err := publishProfileBundleToStore(ctx, runtime, profilePath, storeLabel, false, false); err != nil {
			_ = runtime.Close()
			return nil, nil, err
		}
	}
	bundle, err := serveBundle(ctx, runtime)
	if err != nil {
		_ = runtime.Close()
		return nil, nil, err
	}
	return controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: runtime, TraceGraphQLURL: cfg.traceGraphQLURL, ProfileHome: cfg.profileHome, StoreInfo: storeInfo}), runtime.Close, nil
}

func serveBundle(ctx context.Context, runtime store.Store) (profile.Bundle, error) {
	if runtime != nil {
		catalog, err := runtime.GetProfileCatalog(ctx)
		if err == nil && catalog.ProfileID != "" {
			return profilecatalog.ToBundle(catalog), nil
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return profile.Bundle{}, err
		}
		if catalogIndex, err := runtime.GetProfileCatalogIndex(ctx); err == nil && strings.TrimSpace(catalogIndex.ProfileID) != "" {
			if profileIndex, err := runtime.GetProfileIndex(ctx, catalogIndex.ProfileID); err == nil && strings.TrimSpace(profileIndex.BundlePath) != "" {
				if bundle, err := profile.Load(profileIndex.BundlePath); err == nil {
					return bundle, nil
				}
			}
		}
	}
	return profile.EmptyBundle(), nil
}

func serveStoreInfo(cfg serveConfig, resolvedStoreURL string) controlplane.StoreInfo {
	backend, _ := storeBackendFromURL(resolvedStoreURL)
	info := controlplane.StoreInfo{
		Configured: true,
		Backend:    backend,
		URL:        maskStoreURL(resolvedStoreURL),
		Source:     "active-config",
	}
	if strings.TrimSpace(cfg.storeURL) != "" {
		info.Source = "store-url"
		return info
	}
	storeRef := strings.TrimSpace(cfg.storeRef)
	if storeRef == "" {
		if entry, err := activeStoreConfig(); err == nil {
			info.Name = entry.Name
		}
		return info
	}
	if directBackend, err := storeBackendFromURL(storeRef); err == nil && directBackend != "" {
		info.Source = "store-flag"
		return info
	}
	info.Source = "store-config"
	info.Name = storeRef
	return info
}
