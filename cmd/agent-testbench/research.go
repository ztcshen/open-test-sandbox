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
	"time"
)

const featureRadarIndexEnv = "AGENT_TESTBENCH_FEATURE_RADAR_INDEX"

type featureRadarIndex struct {
	GeneratedAt       string                         `json:"generatedAt"`
	SourceGeneratedAt string                         `json:"sourceGeneratedAt"`
	Policy            featureRadarPolicy             `json:"policy"`
	TokenIndex        map[string][]string            `json:"tokenIndex"`
	Features          map[string]featureRadarFeature `json:"features"`
	ProjectIndex      map[string]featureRadarProject `json:"projectIndex"`
	RefreshSummary    featureRadarRefreshSummary     `json:"refreshSummary"`
}

type featureRadarPolicy struct {
	MinStars    int    `json:"minStars"`
	Months      int    `json:"months"`
	PushedAfter string `json:"pushedAfter"`
}

type featureRadarFeature struct {
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Intent     string              `json:"intent"`
	Aliases    []string            `json:"aliases"`
	TopMatches []featureRadarMatch `json:"topMatches"`
}

type featureRadarMatch struct {
	FullName     string   `json:"fullName"`
	URL          string   `json:"url"`
	Stars        int      `json:"stars"`
	PushedAt     string   `json:"pushedAt"`
	FeatureScore int      `json:"featureScore"`
	Reasons      []string `json:"reasons"`
}

type featureRadarProject struct {
	FullName        string   `json:"fullName"`
	URL             string   `json:"url"`
	Stars           int      `json:"stars"`
	PushedAt        string   `json:"pushedAt"`
	Language        string   `json:"language"`
	Topics          []string `json:"topics"`
	MatchedFeatures []string `json:"matchedFeatures"`
	Features        []string `json:"features"`
}

type featureRadarRefreshSummary struct {
	ResultCount       int      `json:"resultCount"`
	SeedResultCount   int      `json:"seedResultCount"`
	CacheResultCount  int      `json:"cacheResultCount"`
	CacheFeatureCount int      `json:"cacheFeatureCount"`
	CacheFeatures     []string `json:"cacheFeatures"`
}

type featureResearchReport struct {
	Feature           featureRadarFeature  `json:"feature"`
	Policy            featureRadarPolicy   `json:"policy"`
	SourceGeneratedAt string               `json:"sourceGeneratedAt"`
	Matches           []featureRadarMatch  `json:"matches"`
	NextCommands      []featureNextCommand `json:"nextCommands"`
}

type featureResearchPlanReport struct {
	OK                   bool                 `json:"ok"`
	Feature              featureRadarFeature  `json:"feature"`
	Policy               featureRadarPolicy   `json:"policy"`
	SourceGeneratedAt    string               `json:"sourceGeneratedAt"`
	ReferenceGate        featureReferenceGate `json:"referenceGate"`
	References           []featureRadarMatch  `json:"references"`
	NextCommands         []featureNextCommand `json:"nextCommands"`
	VerificationCommands []string             `json:"verificationCommands"`
}

type featureCoverageReport struct {
	OK                bool                  `json:"ok"`
	Policy            featureRadarPolicy    `json:"policy"`
	SourceGeneratedAt string                `json:"sourceGeneratedAt"`
	ReferenceGate     featureCoverageGate   `json:"referenceGate"`
	Features          []featureCoverageItem `json:"features"`
}

type featureCoverageGate struct {
	Required int `json:"required"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
}

type featureCoverageItem struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	Intent        string              `json:"intent,omitempty"`
	References    int                 `json:"references"`
	Gate          string              `json:"gate"`
	TopReferences []featureRadarMatch `json:"topReferences"`
}

type featureAuditReport struct {
	OK                bool                    `json:"ok"`
	Policy            featureRadarPolicy      `json:"policy"`
	SourceGeneratedAt string                  `json:"sourceGeneratedAt"`
	Counts            featureAuditCounts      `json:"counts"`
	Violations        []featureAuditViolation `json:"violations"`
}

type featureAuditCounts struct {
	Features   int `json:"features"`
	References int `json:"references"`
	Projects   int `json:"projects"`
	Violations int `json:"violations"`
}

type featureAuditViolation struct {
	FeatureID string `json:"featureId"`
	FullName  string `json:"fullName,omitempty"`
	Field     string `json:"field"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
	Message   string `json:"message"`
}

type featureStatusReport struct {
	OK                  bool                `json:"ok"`
	Fresh               bool                `json:"fresh"`
	GeneratedAt         string              `json:"generatedAt"`
	SourceGeneratedAt   string              `json:"sourceGeneratedAt"`
	CheckedAt           string              `json:"checkedAt"`
	MaxAgeHours         int                 `json:"maxAgeHours"`
	AgeHours            int                 `json:"ageHours"`
	StaleReason         string              `json:"staleReason,omitempty"`
	Policy              featureRadarPolicy  `json:"policy"`
	Counts              featureStatusCounts `json:"counts"`
	NextRefreshCommands []string            `json:"nextRefreshCommands"`
}

type featureStatusCounts struct {
	Features       int `json:"features"`
	References     int `json:"references"`
	Projects       int `json:"projects"`
	CachedResults  int `json:"cachedResults"`
	CachedFeatures int `json:"cachedFeatures"`
}

type featureMatrixReport struct {
	OK                bool                `json:"ok"`
	Filter            string              `json:"filter,omitempty"`
	Count             int                 `json:"count"`
	Policy            featureRadarPolicy  `json:"policy"`
	SourceGeneratedAt string              `json:"sourceGeneratedAt"`
	Features          []featureMatrixItem `json:"features"`
}

type featureMatrixItem struct {
	ID            string                   `json:"id"`
	Title         string                   `json:"title"`
	Intent        string                   `json:"intent,omitempty"`
	References    int                      `json:"references"`
	TopReferences []featureMatrixReference `json:"topReferences"`
}

type featureMatrixReference struct {
	FullName        string   `json:"fullName"`
	URL             string   `json:"url"`
	Stars           int      `json:"stars"`
	PushedAt        string   `json:"pushedAt"`
	FeatureScore    int      `json:"featureScore"`
	Reasons         []string `json:"reasons,omitempty"`
	Language        string   `json:"language,omitempty"`
	Topics          []string `json:"topics,omitempty"`
	MatchedFeatures []string `json:"matchedFeatures,omitempty"`
}

type featureRefreshPlanReport struct {
	OK                bool                  `json:"ok"`
	NeedsRefresh      bool                  `json:"needsRefresh"`
	Reasons           []string              `json:"reasons,omitempty"`
	SourceGeneratedAt string                `json:"sourceGeneratedAt"`
	Checks            featureRefreshChecks  `json:"checks"`
	Counts            featureRefreshCounts  `json:"counts"`
	FocusFeatures     []featureRefreshFocus `json:"focusFeatures"`
	NextCommands      []string              `json:"nextCommands"`
}

type featureRefreshChecks struct {
	Fresh      bool `json:"fresh"`
	AuditOK    bool `json:"auditOk"`
	CoverageOK bool `json:"coverageOk"`
}

type featureRefreshCounts struct {
	Features          int `json:"features"`
	References        int `json:"references"`
	Projects          int `json:"projects"`
	AuditViolations   int `json:"auditViolations"`
	CoverageFailures  int `json:"coverageFailures"`
	ProjectViolations int `json:"projectViolations"`
}

type featureRefreshFocus struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	References      int      `json:"references"`
	Gate            string   `json:"gate"`
	Reasons         []string `json:"reasons"`
	MatrixCommand   string   `json:"matrixCommand"`
	PlanCommand     string   `json:"planCommand"`
	RefreshCommand  string   `json:"refreshCommand"`
	TopProjectNames []string `json:"topProjectNames,omitempty"`
}

type featureRoadmapReport struct {
	OK                bool                 `json:"ok"`
	Policy            featureRadarPolicy   `json:"policy"`
	SourceGeneratedAt string               `json:"sourceGeneratedAt"`
	ReferenceGate     featureCoverageGate  `json:"referenceGate"`
	Count             int                  `json:"count"`
	Items             []featureRoadmapItem `json:"items"`
}

type featureRoadmapItem struct {
	ID                     string               `json:"id"`
	Title                  string               `json:"title"`
	Intent                 string               `json:"intent,omitempty"`
	References             int                  `json:"references"`
	Gate                   string               `json:"gate"`
	ReadinessScore         int                  `json:"readinessScore"`
	AvailableCommands      int                  `json:"availableCommands"`
	ImplementationCommands int                  `json:"implementationCommands"`
	TotalStars             int                  `json:"totalStars"`
	PlanCommand            string               `json:"planCommand"`
	TopReferences          []featureRadarMatch  `json:"topReferences"`
	NextCommands           []featureNextCommand `json:"nextCommands"`
}

type featureBacklogReport struct {
	OK                bool                 `json:"ok"`
	Policy            featureRadarPolicy   `json:"policy"`
	SourceGeneratedAt string               `json:"sourceGeneratedAt"`
	ReferenceGate     featureCoverageGate  `json:"referenceGate"`
	Count             int                  `json:"count"`
	Items             []featureBacklogItem `json:"items"`
}

type featureBacklogItem struct {
	TaskID                 string               `json:"taskId"`
	Priority               int                  `json:"priority"`
	FeatureID              string               `json:"featureId"`
	Title                  string               `json:"title"`
	Intent                 string               `json:"intent,omitempty"`
	Status                 string               `json:"status"`
	ReadinessScore         int                  `json:"readinessScore"`
	References             []featureRadarMatch  `json:"references"`
	PlanCommand            string               `json:"planCommand"`
	ImplementationCommands []featureNextCommand `json:"implementationCommands"`
	VerificationCommands   []string             `json:"verificationCommands"`
	AcceptanceCriteria     []string             `json:"acceptanceCriteria"`
}

type featureGateReport struct {
	OK                   bool                 `json:"ok"`
	Feature              featureRadarFeature  `json:"feature"`
	Policy               featureRadarPolicy   `json:"policy"`
	SourceGeneratedAt    string               `json:"sourceGeneratedAt"`
	Checks               featureGateChecks    `json:"checks"`
	ReferenceGate        featureReferenceGate `json:"referenceGate"`
	CommandGate          featureCommandGate   `json:"commandGate"`
	References           []featureRadarMatch  `json:"references"`
	NextCommands         []featureNextCommand `json:"nextCommands"`
	VerificationCommands []string             `json:"verificationCommands"`
	Reasons              []string             `json:"reasons,omitempty"`
}

type featureGateChecks struct {
	Fresh           bool `json:"fresh"`
	AuditOK         bool `json:"auditOk"`
	ReferenceGateOK bool `json:"referenceGateOk"`
	CommandGateOK   bool `json:"commandGateOk"`
}

type featureCommandGate struct {
	Required          string               `json:"required"`
	OK                bool                 `json:"ok"`
	Matched           featureNextCommand   `json:"matched,omitempty"`
	AvailableCommands []featureNextCommand `json:"availableCommands"`
}

type featureReferenceGate struct {
	Required int  `json:"required"`
	Found    int  `json:"found"`
	OK       bool `json:"ok"`
}

type featureNextCommand struct {
	Command        string   `json:"command"`
	CommandPath    []string `json:"commandPath,omitempty"`
	CatalogCommand string   `json:"catalogCommand,omitempty"`
	Available      bool     `json:"available"`
	Purpose        string   `json:"purpose"`
}

type featureRadarCatalogReport struct {
	OK                bool                      `json:"ok"`
	Filter            string                    `json:"filter,omitempty"`
	Count             int                       `json:"count"`
	Policy            featureRadarPolicy        `json:"policy"`
	SourceGeneratedAt string                    `json:"sourceGeneratedAt"`
	Features          []featureRadarCatalogItem `json:"features"`
}

type featureRadarCatalogItem struct {
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Intent     string              `json:"intent,omitempty"`
	Aliases    []string            `json:"aliases,omitempty"`
	MatchCount int                 `json:"matchCount"`
	TopMatches []featureRadarMatch `json:"topMatches"`
}

type featureSearchReport struct {
	OK                bool                     `json:"ok"`
	Query             string                   `json:"query"`
	NormalizedQuery   string                   `json:"normalizedQuery"`
	Count             int                      `json:"count"`
	Policy            featureRadarPolicy       `json:"policy"`
	SourceGeneratedAt string                   `json:"sourceGeneratedAt"`
	Candidates        []featureSearchCandidate `json:"candidates"`
}

type featureSearchCandidate struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	Intent        string              `json:"intent,omitempty"`
	Score         int                 `json:"score"`
	MatchedTokens []string            `json:"matchedTokens"`
	References    int                 `json:"references"`
	Gate          string              `json:"gate"`
	PlanCommand   string              `json:"planCommand"`
	TopReferences []featureRadarMatch `json:"topReferences"`
}

func runResearch(args []string) error {
	if len(args) == 0 {
		return errors.New("missing research command")
	}
	switch args[0] {
	case "feature":
		return runResearchFeature(args[1:])
	case "features":
		return runResearchFeatures(args[1:])
	case "search":
		return runResearchSearch(args[1:])
	case "plan":
		return runResearchPlan(args[1:])
	case "coverage":
		return runResearchCoverage(args[1:])
	case "audit":
		return runResearchAudit(args[1:])
	case "status":
		return runResearchStatus(args[1:])
	case "matrix":
		return runResearchMatrix(args[1:])
	case "refresh-plan":
		return runResearchRefreshPlan(args[1:])
	case "roadmap":
		return runResearchRoadmap(args[1:])
	case "backlog":
		return runResearchBacklog(args[1:])
	case "gate":
		return runResearchGate(args[1:])
	default:
		return fmt.Errorf("unknown research command: %s", args[0])
	}
}

func runResearchFeature(args []string) error {
	flags := flag.NewFlagSet("research feature", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	featureQuery := flags.String("feature", "", "Feature text, id, or alias to search")
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	limit := flags.Int("limit", 5, "Maximum number of reference projects to show")
	requireMinMatches := flags.Int("require-min-matches", 0, "Fail when fewer than this many reference projects are available")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*featureQuery) == "" {
		return errors.New("research feature requires --feature TEXT")
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research feature requires --radar-index PATH or %s", featureRadarIndexEnv)
	}

	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	feature, err := findFeatureRadarFeature(index, *featureQuery)
	if err != nil {
		return err
	}
	if *requireMinMatches > 0 && len(feature.TopMatches) < *requireMinMatches {
		return fmt.Errorf("feature %q requires at least %d reference projects, found %d", feature.ID, *requireMinMatches, len(feature.TopMatches))
	}
	report := featureResearchReport{
		Feature:           feature,
		Policy:            index.Policy,
		SourceGeneratedAt: index.SourceGeneratedAt,
		Matches:           limitFeatureRadarMatches(feature.TopMatches, *limit),
		NextCommands:      featureNextCommands(feature.ID),
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	printFeatureResearchReport(report)
	return nil
}

func runResearchFeatures(args []string) error {
	flags := flag.NewFlagSet("research features", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	filter := flags.String("filter", "", "Filter features by id, title, intent, alias, or reference")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research features requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	report := buildFeatureRadarCatalogReport(index, *filter)
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	printFeatureRadarCatalogReport(report)
	return nil
}

func runResearchSearch(args []string) error {
	flags := flag.NewFlagSet("research search", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	query := flags.String("query", "", "Feature text to search")
	featureQuery := flags.String("feature", "", "Alias for --query")
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	limit := flags.Int("limit", 5, "Maximum candidate features to show")
	referenceLimit := flags.Int("reference-limit", 2, "Maximum top references per candidate to include")
	minReferences := flags.Int("min-references", 3, "Reference gate shown for each candidate")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedQuery := researchFirstNonEmpty(*query, *featureQuery)
	if strings.TrimSpace(resolvedQuery) == "" {
		return errors.New("research search requires --query TEXT")
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research search requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	report := buildFeatureSearchReport(index, resolvedIndexPath, resolvedQuery, *limit, *referenceLimit, *minReferences)
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	printFeatureSearchReport(report)
	return nil
}

func runResearchCoverage(args []string) error {
	flags := flag.NewFlagSet("research coverage", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	minReferences := flags.Int("min-references", 3, "Fail unless every feature has at least this many reference projects")
	limit := flags.Int("limit", 3, "Maximum top references per feature to include")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research coverage requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	report := buildFeatureCoverageReport(index, *minReferences, *limit)
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
	} else {
		printFeatureCoverageReport(report)
	}
	if !report.OK {
		return errors.New("feature coverage gate failed")
	}
	return nil
}

func runResearchAudit(args []string) error {
	flags := flag.NewFlagSet("research audit", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	minReferences := flags.Int("min-references", 1, "Fail when any feature has fewer than this many reference projects")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research audit requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	report := buildFeatureAuditReport(index, *minReferences)
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
	} else {
		printFeatureAuditReport(report)
	}
	if !report.OK {
		return errors.New("feature radar audit failed")
	}
	return nil
}

func runResearchStatus(args []string) error {
	flags := flag.NewFlagSet("research status", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	maxAgeHours := flags.Int("max-age-hours", 72, "Fail when the feature index is older than this many hours")
	nowValue := flags.String("now", "", "Override current time for deterministic checks (RFC3339)")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research status requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	checkedAt, err := parseResearchStatusNow(*nowValue)
	if err != nil {
		return err
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	report := buildFeatureStatusReport(index, resolvedIndexPath, *maxAgeHours, checkedAt)
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
	} else {
		printFeatureStatusReport(report)
	}
	if !report.OK {
		return errors.New("feature radar status failed")
	}
	return nil
}

func runResearchMatrix(args []string) error {
	flags := flag.NewFlagSet("research matrix", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	filter := flags.String("filter", "", "Filter features by id, title, intent, alias, or reference")
	limit := flags.Int("limit", 3, "Maximum top references per feature to include")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research matrix requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	report := buildFeatureMatrixReport(index, *filter, *limit)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printFeatureMatrixReport(report)
	return nil
}

func runResearchRefreshPlan(args []string) error {
	flags := flag.NewFlagSet("research refresh-plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	minReferences := flags.Int("min-references", 3, "Require this many reference projects before a feature is healthy")
	maxAgeHours := flags.Int("max-age-hours", 72, "Mark the feature index stale after this many hours")
	nowValue := flags.String("now", "", "Override current time for deterministic checks (RFC3339)")
	limit := flags.Int("limit", 5, "Maximum focus features to include")
	requireReady := flags.Bool("require-ready", false, "Exit non-zero when the radar needs refresh")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research refresh-plan requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	checkedAt, err := parseResearchStatusNow(*nowValue)
	if err != nil {
		return err
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	report := buildFeatureRefreshPlanReport(index, resolvedIndexPath, *minReferences, *maxAgeHours, checkedAt, *limit)
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
	} else {
		printFeatureRefreshPlanReport(report)
	}
	if *requireReady && report.NeedsRefresh {
		return errors.New("feature radar refresh plan is not ready")
	}
	return nil
}

func runResearchRoadmap(args []string) error {
	flags := flag.NewFlagSet("research roadmap", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	minReferences := flags.Int("min-references", 3, "Require this many reference projects for a feature to be roadmap-ready")
	limit := flags.Int("limit", 5, "Maximum roadmap candidates to show")
	referenceLimit := flags.Int("reference-limit", 2, "Maximum top references per roadmap candidate to include")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research roadmap requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	report := buildFeatureRoadmapReport(index, resolvedIndexPath, *minReferences, *limit, *referenceLimit)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printFeatureRoadmapReport(report)
	return nil
}

func runResearchBacklog(args []string) error {
	flags := flag.NewFlagSet("research backlog", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	minReferences := flags.Int("min-references", 3, "Require this many reference projects for a feature task to be ready")
	limit := flags.Int("limit", 5, "Maximum backlog tasks to show")
	referenceLimit := flags.Int("reference-limit", 2, "Maximum top references per backlog task to include")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research backlog requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	report := buildFeatureBacklogReport(index, resolvedIndexPath, *minReferences, *limit, *referenceLimit)
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	printFeatureBacklogReport(report)
	return nil
}

func runResearchPlan(args []string) error {
	flags := flag.NewFlagSet("research plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	featureQuery := flags.String("feature", "", "Feature text, id, or alias to plan")
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	limit := flags.Int("limit", 5, "Maximum number of reference projects to include")
	requireMinMatches := flags.Int("require-min-matches", 0, "Fail when fewer than this many reference projects are available")
	outputFormat := flags.String("format", "text", "Output format: text, json, or markdown")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON plan")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*featureQuery) == "" {
		return errors.New("research plan requires --feature TEXT")
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research plan requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	feature, err := findFeatureRadarFeature(index, *featureQuery)
	if err != nil {
		return err
	}
	report := buildFeatureResearchPlan(index, resolvedIndexPath, feature, *featureQuery, *limit, *requireMinMatches)
	if *requireMinMatches > 0 && !report.ReferenceGate.OK {
		return fmt.Errorf("feature %q requires at least %d reference projects, found %d", feature.ID, *requireMinMatches, len(feature.TopMatches))
	}
	format := strings.ToLower(strings.TrimSpace(*outputFormat))
	if *jsonOutput {
		format = "json"
	}
	switch format {
	case "", "text":
		printFeatureResearchPlan(report)
	case "json":
		return json.NewEncoder(os.Stdout).Encode(report)
	case "markdown", "md":
		printFeatureResearchPlanMarkdown(report)
	default:
		return fmt.Errorf("unsupported research plan format %q", *outputFormat)
	}
	return nil
}

func runResearchGate(args []string) error {
	flags := flag.NewFlagSet("research gate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	featureQuery := flags.String("feature", "", "Feature text, id, or alias to gate")
	indexPath := flags.String("radar-index", "", "Path to github-feature-radar data/feature-index.json")
	requireMinMatches := flags.Int("require-min-matches", 3, "Fail when fewer than this many reference projects are available")
	requireCommand := flags.String("require-command", "", "Require a matching AgentTestBench command path, for example 'workflow report'")
	maxAgeHours := flags.Int("max-age-hours", 72, "Fail when the feature index is older than this many hours")
	nowValue := flags.String("now", "", "Override current time for deterministic checks (RFC3339)")
	limit := flags.Int("limit", 5, "Maximum number of reference projects to include")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON gate report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*featureQuery) == "" {
		return errors.New("research gate requires --feature TEXT")
	}
	resolvedIndexPath := strings.TrimSpace(*indexPath)
	if resolvedIndexPath == "" {
		resolvedIndexPath = strings.TrimSpace(os.Getenv(featureRadarIndexEnv))
	}
	if resolvedIndexPath == "" {
		return fmt.Errorf("research gate requires --radar-index PATH or %s", featureRadarIndexEnv)
	}
	checkedAt, err := parseResearchStatusNow(*nowValue)
	if err != nil {
		return err
	}
	index, err := readFeatureRadarIndex(resolvedIndexPath)
	if err != nil {
		return err
	}
	feature, err := findFeatureRadarFeature(index, *featureQuery)
	if err != nil {
		return err
	}
	report := buildFeatureGateReport(index, resolvedIndexPath, feature, *featureQuery, *requireMinMatches, *requireCommand, *maxAgeHours, checkedAt, *limit)
	if *jsonOutput {
		if err := writeIndentedJSON(report); err != nil {
			return err
		}
	} else {
		printFeatureGateReport(report)
	}
	if !report.OK {
		return errors.New("feature research gate failed")
	}
	return nil
}

func buildFeatureCoverageReport(index featureRadarIndex, minReferences int, limit int) featureCoverageReport {
	if minReferences <= 0 {
		minReferences = 1
	}
	ids := make([]string, 0, len(index.Features))
	for id := range index.Features {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	report := featureCoverageReport{
		OK:                true,
		Policy:            index.Policy,
		SourceGeneratedAt: index.SourceGeneratedAt,
		ReferenceGate: featureCoverageGate{
			Required: minReferences,
		},
		Features: []featureCoverageItem{},
	}
	for _, id := range ids {
		feature := index.Features[id]
		references := len(feature.TopMatches)
		gate := "passed"
		if references < minReferences {
			gate = "failed"
			report.OK = false
			report.ReferenceGate.Failed++
		} else {
			report.ReferenceGate.Passed++
		}
		report.Features = append(report.Features, featureCoverageItem{
			ID:            feature.ID,
			Title:         feature.Title,
			Intent:        feature.Intent,
			References:    references,
			Gate:          gate,
			TopReferences: limitFeatureRadarMatches(feature.TopMatches, limit),
		})
	}
	return report
}

func buildFeatureAuditReport(index featureRadarIndex, minReferences int) featureAuditReport {
	if minReferences <= 0 {
		minReferences = 1
	}
	report := featureAuditReport{
		OK:                true,
		Policy:            index.Policy,
		SourceGeneratedAt: index.SourceGeneratedAt,
		Counts: featureAuditCounts{
			Features: len(index.Features),
			Projects: len(index.ProjectIndex),
		},
		Violations: []featureAuditViolation{},
	}
	ids := make([]string, 0, len(index.Features))
	for id := range index.Features {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		feature := index.Features[id]
		if len(feature.TopMatches) < minReferences {
			report.Violations = append(report.Violations, featureAuditViolation{
				FeatureID: feature.ID,
				Field:     "references",
				Expected:  fmt.Sprintf(">= %d", minReferences),
				Actual:    fmt.Sprintf("%d", len(feature.TopMatches)),
				Message:   "feature does not have enough qualifying reference projects",
			})
		}
		for _, match := range feature.TopMatches {
			report.Counts.References++
			report.Violations = append(report.Violations, featureMatchAuditViolations(index.Policy, feature.ID, match)...)
		}
	}
	projectNames := make([]string, 0, len(index.ProjectIndex))
	for fullName := range index.ProjectIndex {
		projectNames = append(projectNames, fullName)
	}
	sort.Strings(projectNames)
	for _, fullName := range projectNames {
		report.Violations = append(report.Violations, featureProjectAuditViolations(index.Policy, fullName, index.ProjectIndex[fullName])...)
	}
	report.Counts.Violations = len(report.Violations)
	report.OK = report.Counts.Violations == 0
	return report
}

func featureProjectAuditViolations(policy featureRadarPolicy, key string, project featureRadarProject) []featureAuditViolation {
	fullName := researchFirstNonEmpty(project.FullName, key)
	match := featureRadarMatch{
		FullName: fullName,
		URL:      project.URL,
		Stars:    project.Stars,
		PushedAt: project.PushedAt,
	}
	violations := featureMatchAuditViolations(policy, "project-index", match)
	if len(projectMatchedFeatures(project)) == 0 {
		violations = append(violations, featureAuditViolation{
			FeatureID: "project-index",
			FullName:  fullName,
			Field:     "matchedFeatures",
			Expected:  "non-empty",
			Actual:    "0",
			Message:   "project index entry is not attached to any feature",
		})
	}
	return violations
}

func featureMatchAuditViolations(policy featureRadarPolicy, featureID string, match featureRadarMatch) []featureAuditViolation {
	var violations []featureAuditViolation
	fullName := strings.TrimSpace(match.FullName)
	if fullName == "" {
		violations = append(violations, featureAuditViolation{
			FeatureID: featureID,
			Field:     "fullName",
			Expected:  "non-empty",
			Actual:    "",
			Message:   "reference project is missing a fullName",
		})
	}
	if strings.TrimSpace(match.URL) == "" {
		violations = append(violations, featureAuditViolation{
			FeatureID: featureID,
			FullName:  fullName,
			Field:     "url",
			Expected:  "non-empty",
			Actual:    "",
			Message:   "reference project is missing a GitHub URL",
		})
	}
	if policy.MinStars > 0 && match.Stars < policy.MinStars {
		violations = append(violations, featureAuditViolation{
			FeatureID: featureID,
			FullName:  fullName,
			Field:     "stars",
			Expected:  fmt.Sprintf(">= %d", policy.MinStars),
			Actual:    fmt.Sprintf("%d", match.Stars),
			Message:   "reference project is below the configured star floor",
		})
	}
	pushedAt := strings.TrimSpace(match.PushedAt)
	if pushedAt == "" {
		violations = append(violations, featureAuditViolation{
			FeatureID: featureID,
			FullName:  fullName,
			Field:     "pushedAt",
			Expected:  "non-empty",
			Actual:    "",
			Message:   "reference project is missing a pushedAt timestamp",
		})
	} else if policy.PushedAfter != "" && pushedAt < policy.PushedAfter {
		violations = append(violations, featureAuditViolation{
			FeatureID: featureID,
			FullName:  fullName,
			Field:     "pushedAt",
			Expected:  ">= " + policy.PushedAfter,
			Actual:    pushedAt,
			Message:   "reference project is older than the configured recency window",
		})
	}
	return violations
}

func buildFeatureStatusReport(index featureRadarIndex, indexPath string, maxAgeHours int, checkedAt time.Time) featureStatusReport {
	if maxAgeHours <= 0 {
		maxAgeHours = 72
	}
	timestamp := researchFirstNonEmpty(index.SourceGeneratedAt, index.GeneratedAt)
	parsedAt, err := parseFeatureRadarTimestamp(timestamp)
	ageHours := 0
	staleReason := ""
	if err != nil {
		staleReason = "feature index is missing a parseable generated timestamp"
	} else {
		age := checkedAt.Sub(parsedAt)
		if age < 0 {
			age = 0
		}
		ageHours = int(age.Hours())
		if age > time.Duration(maxAgeHours)*time.Hour {
			staleReason = fmt.Sprintf("feature index is older than %dh", maxAgeHours)
		}
	}
	counts := featureStatusCounts{
		Features:       len(index.Features),
		Projects:       len(index.ProjectIndex),
		CachedResults:  index.RefreshSummary.CacheResultCount,
		CachedFeatures: index.RefreshSummary.CacheFeatureCount,
	}
	for _, feature := range index.Features {
		counts.References += len(feature.TopMatches)
	}
	return featureStatusReport{
		OK:                  staleReason == "",
		Fresh:               staleReason == "",
		GeneratedAt:         index.GeneratedAt,
		SourceGeneratedAt:   index.SourceGeneratedAt,
		CheckedAt:           checkedAt.UTC().Format(time.RFC3339),
		MaxAgeHours:         maxAgeHours,
		AgeHours:            ageHours,
		StaleReason:         staleReason,
		Policy:              index.Policy,
		Counts:              counts,
		NextRefreshCommands: featureRadarRefreshCommands(indexPath, maxAgeHours, 3),
	}
}

func buildFeatureMatrixReport(index featureRadarIndex, filter string, limit int) featureMatrixReport {
	if limit < 0 {
		limit = 0
	}
	filter = strings.TrimSpace(filter)
	report := featureMatrixReport{
		OK:                true,
		Filter:            filter,
		Policy:            index.Policy,
		SourceGeneratedAt: index.SourceGeneratedAt,
		Features:          []featureMatrixItem{},
	}
	ids := make([]string, 0, len(index.Features))
	for id := range index.Features {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		feature := index.Features[id]
		if !featureRadarCatalogMatches(feature, filter) {
			continue
		}
		matches := limitFeatureRadarMatches(feature.TopMatches, limit)
		item := featureMatrixItem{
			ID:            feature.ID,
			Title:         feature.Title,
			Intent:        feature.Intent,
			References:    len(feature.TopMatches),
			TopReferences: make([]featureMatrixReference, 0, len(matches)),
		}
		for _, match := range matches {
			item.TopReferences = append(item.TopReferences, featureMatrixReferenceFromMatch(match, index.ProjectIndex[match.FullName]))
		}
		report.Features = append(report.Features, item)
	}
	report.Count = len(report.Features)
	return report
}

func featureMatrixReferenceFromMatch(match featureRadarMatch, project featureRadarProject) featureMatrixReference {
	return featureMatrixReference{
		FullName:        match.FullName,
		URL:             researchFirstNonEmpty(match.URL, project.URL),
		Stars:           match.Stars,
		PushedAt:        match.PushedAt,
		FeatureScore:    match.FeatureScore,
		Reasons:         match.Reasons,
		Language:        project.Language,
		Topics:          project.Topics,
		MatchedFeatures: projectMatchedFeatures(project),
	}
}

func projectMatchedFeatures(project featureRadarProject) []string {
	if len(project.MatchedFeatures) > 0 {
		return project.MatchedFeatures
	}
	return project.Features
}

func buildFeatureRefreshPlanReport(index featureRadarIndex, indexPath string, minReferences int, maxAgeHours int, checkedAt time.Time, limit int) featureRefreshPlanReport {
	if minReferences <= 0 {
		minReferences = 3
	}
	if limit <= 0 {
		limit = 5
	}
	status := buildFeatureStatusReport(index, indexPath, maxAgeHours, checkedAt)
	audit := buildFeatureAuditReport(index, minReferences)
	coverage := buildFeatureCoverageReport(index, minReferences, 3)
	nextCommands := featureRadarRefreshCommands(indexPath, maxAgeHours, minReferences)
	report := featureRefreshPlanReport{
		OK:                true,
		SourceGeneratedAt: index.SourceGeneratedAt,
		Checks: featureRefreshChecks{
			Fresh:      status.Fresh,
			AuditOK:    audit.OK,
			CoverageOK: coverage.OK,
		},
		Counts: featureRefreshCounts{
			Features:          audit.Counts.Features,
			References:        audit.Counts.References,
			Projects:          audit.Counts.Projects,
			AuditViolations:   audit.Counts.Violations,
			CoverageFailures:  coverage.ReferenceGate.Failed,
			ProjectViolations: countProjectAuditViolations(audit.Violations),
		},
		NextCommands: nextCommands,
	}
	if !status.Fresh {
		report.Reasons = append(report.Reasons, status.StaleReason)
	}
	if !audit.OK {
		report.Reasons = append(report.Reasons, fmt.Sprintf("audit has %d violation(s)", audit.Counts.Violations))
	}
	if !coverage.OK {
		report.Reasons = append(report.Reasons, fmt.Sprintf("coverage has %d feature(s) below %d references", coverage.ReferenceGate.Failed, minReferences))
	}
	report.NeedsRefresh = len(report.Reasons) > 0
	report.FocusFeatures = buildFeatureRefreshFocus(index, indexPath, coverage, audit, minReferences, limit, firstFeatureRefreshCommand(nextCommands))
	return report
}

func firstFeatureRefreshCommand(commands []string) string {
	if len(commands) == 0 {
		return "npm run refresh -- --limit 20"
	}
	return commands[0]
}

func countProjectAuditViolations(violations []featureAuditViolation) int {
	count := 0
	for _, violation := range violations {
		if violation.FeatureID == "project-index" {
			count++
		}
	}
	return count
}

func buildFeatureRefreshFocus(index featureRadarIndex, indexPath string, coverage featureCoverageReport, audit featureAuditReport, minReferences int, limit int, refreshCommand string) []featureRefreshFocus {
	violationsByFeature := map[string]int{}
	for _, violation := range audit.Violations {
		if violation.FeatureID == "" || violation.FeatureID == "project-index" {
			continue
		}
		violationsByFeature[violation.FeatureID]++
	}
	items := []featureRefreshFocus{}
	for _, feature := range coverage.Features {
		reasons := []string{}
		if feature.Gate != "passed" {
			reasons = append(reasons, fmt.Sprintf("reference coverage below %d", minReferences))
		}
		if violationsByFeature[feature.ID] > 0 {
			reasons = append(reasons, fmt.Sprintf("feature audit has %d violation(s)", violationsByFeature[feature.ID]))
		}
		if len(reasons) == 0 {
			continue
		}
		items = append(items, featureRefreshFocus{
			ID:              feature.ID,
			Title:           feature.Title,
			References:      feature.References,
			Gate:            feature.Gate,
			Reasons:         reasons,
			MatrixCommand:   "agent-testbench research matrix --filter " + quoteCommandValue(feature.ID) + featureRadarIndexFlag(indexPath) + " --limit 3 --json",
			PlanCommand:     featurePlanCommand(feature.ID, minReferences, indexPath),
			RefreshCommand:  refreshCommand,
			TopProjectNames: featureRefreshProjectNames(index.Features[feature.ID].TopMatches),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Gate != items[j].Gate {
			return items[i].Gate == "failed"
		}
		if items[i].References != items[j].References {
			return items[i].References < items[j].References
		}
		return items[i].ID < items[j].ID
	})
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func featureRefreshProjectNames(matches []featureRadarMatch) []string {
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if strings.TrimSpace(match.FullName) != "" {
			out = append(out, match.FullName)
		}
	}
	return out
}

func parseResearchStatusNow(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse --now: %w", err)
	}
	return parsed.UTC(), nil
}

func parseFeatureRadarTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("empty timestamp")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed.UTC(), nil
	}
	return time.Parse("2006-01-02T15:04:05Z", value)
}

func researchFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func featureRadarRefreshCommands(indexPath string, maxAgeHours int, minReferences int) []string {
	if maxAgeHours <= 0 {
		maxAgeHours = 72
	}
	if minReferences <= 0 {
		minReferences = 3
	}
	root := filepath.Dir(filepath.Dir(indexPath))
	return []string{
		fmt.Sprintf("cd %s && npm run refresh -- --limit 20", quoteShellPath(root)),
		fmt.Sprintf("cd %s && npm run status -- --max-age-hours %d --min-references %d", quoteShellPath(root), maxAgeHours, minReferences),
		fmt.Sprintf("cd %s && npm run audit", quoteShellPath(root)),
		fmt.Sprintf("cd %s && npm run coverage -- --min-references %d", quoteShellPath(root), minReferences),
		fmt.Sprintf("cd %s && npm run index", quoteShellPath(root)),
	}
}

func quoteShellPath(path string) string {
	path = expandUserHomePath(strings.TrimSpace(path))
	if path == "" {
		return "."
	}
	if shellPathNeedsQuoting(path) {
		return quoteShellValue(path)
	}
	return path
}

func expandUserHomePath(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}

func shellPathNeedsQuoting(path string) bool {
	for _, item := range path {
		if item >= 'a' && item <= 'z' {
			continue
		}
		if item >= 'A' && item <= 'Z' {
			continue
		}
		if item >= '0' && item <= '9' {
			continue
		}
		switch item {
		case '/', '.', '_', '-':
			continue
		default:
			return true
		}
	}
	return false
}

func buildFeatureRoadmapReport(index featureRadarIndex, indexPath string, minReferences int, limit int, referenceLimit int) featureRoadmapReport {
	coverage := buildFeatureCoverageReport(index, minReferences, referenceLimit)
	items := make([]featureRoadmapItem, 0, len(coverage.Features))
	for _, feature := range coverage.Features {
		nextCommands := featureNextCommands(feature.ID)
		availableCommands, implementationCommands := countRoadmapCommands(nextCommands)
		totalStars := totalFeatureStars(index.Features[feature.ID].TopMatches)
		item := featureRoadmapItem{
			ID:                     feature.ID,
			Title:                  feature.Title,
			Intent:                 feature.Intent,
			References:             feature.References,
			Gate:                   feature.Gate,
			ReadinessScore:         featureReadinessScore(feature.References, availableCommands, implementationCommands, totalStars),
			AvailableCommands:      availableCommands,
			ImplementationCommands: implementationCommands,
			TotalStars:             totalStars,
			PlanCommand:            featurePlanCommand(feature.ID, coverage.ReferenceGate.Required, indexPath),
			TopReferences:          feature.TopReferences,
			NextCommands:           nextCommands,
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Gate != items[j].Gate {
			return items[i].Gate == "passed"
		}
		if items[i].ReadinessScore != items[j].ReadinessScore {
			return items[i].ReadinessScore > items[j].ReadinessScore
		}
		if items[i].References != items[j].References {
			return items[i].References > items[j].References
		}
		if items[i].TotalStars != items[j].TotalStars {
			return items[i].TotalStars > items[j].TotalStars
		}
		return items[i].ID < items[j].ID
	})
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return featureRoadmapReport{
		OK:                coverage.OK,
		Policy:            index.Policy,
		SourceGeneratedAt: index.SourceGeneratedAt,
		ReferenceGate:     coverage.ReferenceGate,
		Count:             len(items),
		Items:             items,
	}
}

func buildFeatureBacklogReport(index featureRadarIndex, indexPath string, minReferences int, limit int, referenceLimit int) featureBacklogReport {
	roadmap := buildFeatureRoadmapReport(index, indexPath, minReferences, limit, referenceLimit)
	items := make([]featureBacklogItem, 0, len(roadmap.Items))
	for index, item := range roadmap.Items {
		priority := index + 1
		items = append(items, featureBacklogItem{
			TaskID:                 fmt.Sprintf("research-backlog-%03d-%s", priority, item.ID),
			Priority:               priority,
			FeatureID:              item.ID,
			Title:                  item.Title,
			Intent:                 item.Intent,
			Status:                 featureBacklogStatus(item),
			ReadinessScore:         item.ReadinessScore,
			References:             item.TopReferences,
			PlanCommand:            item.PlanCommand,
			ImplementationCommands: featureImplementationCommands(item.NextCommands),
			VerificationCommands:   featureBacklogVerificationCommands(item, roadmap.ReferenceGate.Required, indexPath),
			AcceptanceCriteria:     featureBacklogAcceptanceCriteria(item, roadmap.ReferenceGate.Required),
		})
	}
	return featureBacklogReport{
		OK:                roadmap.OK,
		Policy:            roadmap.Policy,
		SourceGeneratedAt: roadmap.SourceGeneratedAt,
		ReferenceGate:     roadmap.ReferenceGate,
		Count:             len(items),
		Items:             items,
	}
}

func featureBacklogStatus(item featureRoadmapItem) string {
	if item.Gate == "passed" && item.ImplementationCommands > 0 {
		return "ready"
	}
	if item.Gate == "passed" {
		return "needs-command"
	}
	return "needs-references"
}

func featureImplementationCommands(commands []featureNextCommand) []featureNextCommand {
	out := []featureNextCommand{}
	for _, command := range commands {
		if command.Available && len(command.CommandPath) > 0 && command.CommandPath[0] != "research" {
			out = append(out, command)
		}
	}
	return out
}

func featureBacklogVerificationCommands(item featureRoadmapItem, minReferences int, indexPath string) []string {
	commands := []string{
		fmt.Sprintf("agent-testbench research coverage%s --min-references %d --json", featureRadarIndexFlag(indexPath), minReferences),
		item.PlanCommand,
	}
	for _, command := range featureImplementationCommands(item.NextCommands) {
		commands = append(commands, command.Command)
	}
	return commands
}

func featureBacklogAcceptanceCriteria(item featureRoadmapItem, minReferences int) []string {
	return []string{
		fmt.Sprintf("feature reference gate passes with at least %d recent 3K+ star projects", minReferences),
		"research plan captures the reference-backed design and verification commands",
		fmt.Sprintf("at least one implementation command is available in the current CLI catalog (%d found)", item.ImplementationCommands),
	}
}

func countRoadmapCommands(commands []featureNextCommand) (int, int) {
	available := 0
	implementation := 0
	for _, command := range commands {
		if !command.Available {
			continue
		}
		available++
		if len(command.CommandPath) > 0 && command.CommandPath[0] != "research" {
			implementation++
		}
	}
	return available, implementation
}

func totalFeatureStars(matches []featureRadarMatch) int {
	total := 0
	for _, match := range matches {
		total += match.Stars
	}
	return total
}

func featureReadinessScore(references int, availableCommands int, implementationCommands int, totalStars int) int {
	starWeight := totalStars / 10000
	if starWeight > 20 {
		starWeight = 20
	}
	return implementationCommands*100 + availableCommands*10 + references*20 + starWeight
}

func featurePlanCommand(featureID string, minReferences int, indexPath string) string {
	return "agent-testbench research plan --feature " + quoteCommandValue(featureID) + featureRadarIndexFlag(indexPath) + featureRequireMinFlag(minReferences) + " --format markdown"
}

func buildFeatureResearchPlan(index featureRadarIndex, indexPath string, feature featureRadarFeature, featureQuery string, limit int, requireMinMatches int) featureResearchPlanReport {
	nextCommands := featureNextCommands(feature.ID)
	gate := featureReferenceGate{
		Required: requireMinMatches,
		Found:    len(feature.TopMatches),
		OK:       requireMinMatches == 0 || len(feature.TopMatches) >= requireMinMatches,
	}
	return featureResearchPlanReport{
		OK:                   gate.OK,
		Feature:              feature,
		Policy:               index.Policy,
		SourceGeneratedAt:    index.SourceGeneratedAt,
		ReferenceGate:        gate,
		References:           limitFeatureRadarMatches(feature.TopMatches, limit),
		NextCommands:         nextCommands,
		VerificationCommands: featureVerificationCommands(featureQuery, requireMinMatches, indexPath, nextCommands),
	}
}

func buildFeatureGateReport(index featureRadarIndex, indexPath string, feature featureRadarFeature, featureQuery string, requireMinMatches int, requireCommand string, maxAgeHours int, checkedAt time.Time, limit int) featureGateReport {
	if requireMinMatches <= 0 {
		requireMinMatches = 1
	}
	status := buildFeatureStatusReport(index, indexPath, maxAgeHours, checkedAt)
	audit := buildFeatureAuditReport(index, requireMinMatches)
	nextCommands := featureNextCommands(feature.ID)
	referenceGate := featureReferenceGate{
		Required: requireMinMatches,
		Found:    len(feature.TopMatches),
		OK:       len(feature.TopMatches) >= requireMinMatches,
	}
	commandGate := buildFeatureCommandGate(nextCommands, requireCommand)
	report := featureGateReport{
		Feature:           feature,
		Policy:            index.Policy,
		SourceGeneratedAt: index.SourceGeneratedAt,
		Checks: featureGateChecks{
			Fresh:           status.Fresh,
			AuditOK:         audit.OK,
			ReferenceGateOK: referenceGate.OK,
			CommandGateOK:   commandGate.OK,
		},
		ReferenceGate:        referenceGate,
		CommandGate:          commandGate,
		References:           limitFeatureRadarMatches(feature.TopMatches, limit),
		NextCommands:         nextCommands,
		VerificationCommands: featureGateVerificationCommands(featureQuery, requireMinMatches, requireCommand, maxAgeHours, indexPath, nextCommands),
	}
	if !status.Fresh {
		report.Reasons = append(report.Reasons, status.StaleReason)
	}
	if !audit.OK {
		report.Reasons = append(report.Reasons, fmt.Sprintf("audit has %d violation(s)", audit.Counts.Violations))
	}
	if !referenceGate.OK {
		report.Reasons = append(report.Reasons, fmt.Sprintf("feature %q requires at least %d reference projects, found %d", feature.ID, referenceGate.Required, referenceGate.Found))
	}
	if !commandGate.OK {
		if strings.TrimSpace(requireCommand) == "" {
			report.Reasons = append(report.Reasons, "no available AgentTestBench command is attached to feature")
		} else {
			report.Reasons = append(report.Reasons, fmt.Sprintf("required command %s is not available", strings.TrimSpace(requireCommand)))
		}
	}
	report.OK = len(report.Reasons) == 0
	return report
}

func buildFeatureCommandGate(commands []featureNextCommand, requireCommand string) featureCommandGate {
	required := strings.TrimSpace(requireCommand)
	if required == "" {
		required = "any available next command"
	}
	gate := featureCommandGate{
		Required:          required,
		AvailableCommands: availableFeatureCommands(commands),
	}
	for _, command := range commands {
		if !command.Available {
			continue
		}
		if strings.TrimSpace(requireCommand) == "" || featureCommandMatches(command, requireCommand) {
			gate.OK = true
			gate.Matched = command
			return gate
		}
	}
	return gate
}

func availableFeatureCommands(commands []featureNextCommand) []featureNextCommand {
	out := []featureNextCommand{}
	for _, command := range commands {
		if command.Available {
			out = append(out, command)
		}
	}
	return out
}

func featureCommandMatches(command featureNextCommand, requireCommand string) bool {
	path := featureNextCommandPath(requireCommand)
	needle := normalizeFeatureRadarText(strings.Join(path, " "))
	if needle == "" {
		needle = normalizeFeatureRadarText(requireCommand)
	}
	return normalizeFeatureRadarText(command.CatalogCommand) == needle ||
		normalizeFeatureRadarText(strings.Join(command.CommandPath, " ")) == needle
}

func featureGateVerificationCommands(featureQuery string, requireMinMatches int, requireCommand string, maxAgeHours int, indexPath string, nextCommands []featureNextCommand) []string {
	commands := []string{
		fmt.Sprintf("agent-testbench research refresh-plan%s --require-ready --min-references %d --max-age-hours %d --json", featureRadarIndexFlag(indexPath), requireMinMatches, maxAgeHours),
		"agent-testbench research gate --feature " + quoteCommandValue(featureQuery) + featureRadarIndexFlag(indexPath) + featureRequireMinFlag(requireMinMatches) + featureRequireCommandFlag(requireCommand) + " --json",
	}
	for _, item := range nextCommands {
		if item.Available {
			commands = append(commands, item.Command)
		}
	}
	return commands
}

func featureRequireCommandFlag(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return " --require-command " + quoteCommandValue(value)
}

func buildFeatureRadarCatalogReport(index featureRadarIndex, filter string) featureRadarCatalogReport {
	filter = strings.TrimSpace(filter)
	report := featureRadarCatalogReport{
		OK:                true,
		Filter:            filter,
		Policy:            index.Policy,
		SourceGeneratedAt: index.SourceGeneratedAt,
		Features:          []featureRadarCatalogItem{},
	}
	ids := make([]string, 0, len(index.Features))
	for id := range index.Features {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		feature := index.Features[id]
		if !featureRadarCatalogMatches(feature, filter) {
			continue
		}
		report.Features = append(report.Features, featureRadarCatalogItem{
			ID:         feature.ID,
			Title:      feature.Title,
			Intent:     feature.Intent,
			Aliases:    feature.Aliases,
			MatchCount: len(feature.TopMatches),
			TopMatches: feature.TopMatches,
		})
	}
	report.Count = len(report.Features)
	return report
}

func buildFeatureSearchReport(index featureRadarIndex, indexPath string, query string, limit int, referenceLimit int, minReferences int) featureSearchReport {
	if limit <= 0 {
		limit = 5
	}
	if referenceLimit < 0 {
		referenceLimit = 0
	}
	if minReferences <= 0 {
		minReferences = 3
	}
	normalizedQuery := normalizeFeatureRadarText(query)
	queryTerms := strings.Fields(normalizedQuery)
	scores := map[string]int{}
	matchedTokens := map[string][]string{}
	for token, ids := range index.TokenIndex {
		score := featureSearchTokenScore(token, normalizedQuery, queryTerms)
		if score <= 0 {
			continue
		}
		for _, id := range ids {
			if _, ok := index.Features[id]; !ok {
				continue
			}
			scores[id] += score
			if featureSearchDisplayToken(token) {
				matchedTokens[id] = append(matchedTokens[id], token)
			}
		}
	}

	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left := index.Features[ids[i]]
		right := index.Features[ids[j]]
		if scores[ids[i]] != scores[ids[j]] {
			return scores[ids[i]] > scores[ids[j]]
		}
		if len(left.TopMatches) != len(right.TopMatches) {
			return len(left.TopMatches) > len(right.TopMatches)
		}
		return ids[i] < ids[j]
	})
	if len(ids) > limit {
		ids = ids[:limit]
	}

	candidates := make([]featureSearchCandidate, 0, len(ids))
	for _, id := range ids {
		feature := index.Features[id]
		references := len(feature.TopMatches)
		tokens := uniqueSortedStrings(matchedTokens[id])
		candidates = append(candidates, featureSearchCandidate{
			ID:            feature.ID,
			Title:         feature.Title,
			Intent:        feature.Intent,
			Score:         scores[id],
			MatchedTokens: tokens,
			References:    references,
			Gate:          featureGateStatus(references, minReferences),
			PlanCommand:   featurePlanCommand(feature.ID, minReferences, indexPath),
			TopReferences: limitFeatureRadarMatches(feature.TopMatches, referenceLimit),
		})
	}

	return featureSearchReport{
		OK:                len(candidates) > 0,
		Query:             strings.TrimSpace(query),
		NormalizedQuery:   normalizedQuery,
		Count:             len(candidates),
		Policy:            index.Policy,
		SourceGeneratedAt: index.SourceGeneratedAt,
		Candidates:        candidates,
	}
}

func featureSearchTokenScore(token string, normalizedQuery string, queryTerms []string) int {
	token = normalizeFeatureRadarText(token)
	if token == "" || normalizedQuery == "" {
		return 0
	}
	if token == normalizedQuery {
		return 8
	}
	if strings.Contains(token, normalizedQuery) || strings.Contains(normalizedQuery, token) {
		return 4
	}
	score := 0
	for _, term := range queryTerms {
		if term != "" && strings.Contains(token, term) {
			score++
		}
	}
	return score
}

func featureSearchDisplayToken(token string) bool {
	return len(strings.TrimSpace(token)) <= 48
}

func featureGateStatus(references int, minReferences int) string {
	if references >= minReferences {
		return "passed"
	}
	return "failed"
}

func uniqueSortedStrings(values []string) []string {
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
	sort.Strings(out)
	return out
}

func readFeatureRadarIndex(path string) (featureRadarIndex, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return featureRadarIndex{}, fmt.Errorf("read feature radar index: %w", err)
	}
	var index featureRadarIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return featureRadarIndex{}, fmt.Errorf("decode feature radar index: %w", err)
	}
	if len(index.Features) == 0 {
		return featureRadarIndex{}, errors.New("feature radar index has no features")
	}
	return index, nil
}

func findFeatureRadarFeature(index featureRadarIndex, query string) (featureRadarFeature, error) {
	normalizedQuery := normalizeFeatureRadarText(query)
	if normalizedQuery == "" {
		return featureRadarFeature{}, errors.New("feature query is empty")
	}
	if ids := index.TokenIndex[normalizedQuery]; len(ids) > 0 {
		return featureRadarFeatureByID(index, ids[0])
	}

	queryTerms := strings.Fields(normalizedQuery)
	type scoredFeature struct {
		ID    string
		Score int
	}
	var scored []scoredFeature
	for token, ids := range index.TokenIndex {
		score := 0
		if strings.Contains(token, normalizedQuery) || strings.Contains(normalizedQuery, token) {
			score += 4
		}
		for _, term := range queryTerms {
			if strings.Contains(token, term) {
				score++
			}
		}
		if score == 0 {
			continue
		}
		for _, id := range ids {
			scored = append(scored, scoredFeature{ID: id, Score: score})
		}
	}
	if len(scored) == 0 {
		return featureRadarFeature{}, fmt.Errorf("no feature radar match for %q", query)
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].ID < scored[j].ID
	})
	return featureRadarFeatureByID(index, scored[0].ID)
}

func featureRadarFeatureByID(index featureRadarIndex, id string) (featureRadarFeature, error) {
	feature, ok := index.Features[id]
	if !ok {
		return featureRadarFeature{}, fmt.Errorf("feature radar index points to missing feature %q", id)
	}
	return feature, nil
}

func normalizeFeatureRadarText(value string) string {
	var builder strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			builder.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(builder.String())
}

func limitFeatureRadarMatches(matches []featureRadarMatch, limit int) []featureRadarMatch {
	if limit <= 0 || limit >= len(matches) {
		return matches
	}
	return matches[:limit]
}

func featureNextCommands(featureID string) []featureNextCommand {
	var commands []featureNextCommand
	switch featureID {
	case "api-test-runner":
		commands = []featureNextCommand{
			{Command: "agent-testbench case run --case PATH --base-url URL --dry-run --json", Purpose: "Preview a file-backed API case without HTTP, Evidence, or Store side effects."},
			{Command: "agent-testbench case run --case-id ID --store NAME_OR_DSN --base-url URL --json", Purpose: "Run a Store catalog case and index run facts plus Evidence."},
		}
	case "cli-command-ux":
		commands = []featureNextCommand{
			{Command: "agent-testbench commands --filter TEXT --json", Purpose: "Discover matching command paths, Store awareness, and tags before planning execution."},
			{Command: "agent-testbench help", Purpose: "Read the human CLI surface that backs the machine-readable command catalog."},
		}
	case "workflow-orchestration":
		commands = []featureNextCommand{
			{Command: "agent-testbench workflow gate --run RUN_ID --require-passed --require-steps --require-evidence --json", Purpose: "Gate a persisted workflow run by status, steps, and Evidence completeness."},
			{Command: "agent-testbench workflow report --workflow ID --store NAME_OR_DSN --json", Purpose: "Produce a workflow execution report from Store-backed catalog facts."},
		}
	case "evidence-diagnosis":
		commands = []featureNextCommand{
			{Command: "agent-testbench case diagnose --case-run CASE_RUN_ID --store NAME_OR_DSN --json", Purpose: "Classify a failed case run and emit compact Evidence signals plus follow-up actions."},
			{Command: "agent-testbench evidence tasks --run RUN_ID --store NAME_OR_DSN --json", Purpose: "Inspect post-process Evidence tasks for a run."},
		}
	case "quality-gates":
		commands = []featureNextCommand{
			{Command: "agent-testbench case gate --run RUN_ID --store NAME_OR_DSN --require-no-failures --require-evidence --json", Purpose: "Fail CI when case runs fail or indexed Evidence is incomplete."},
			{Command: "agent-testbench workflow gate --run RUN_ID --store NAME_OR_DSN --require-passed --require-steps --require-evidence --json", Purpose: "Fail CI when workflow status, step status, or Evidence completeness is not acceptable."},
		}
	case "github-radar-generation":
		commands = []featureNextCommand{
			{Command: "agent-testbench research search --query TEXT --json", Purpose: "Rank candidate feature records from the external GitHub Feature Radar token index."},
			{Command: "agent-testbench research features --filter TEXT --json", Purpose: "Search feature records from the external GitHub Feature Radar index."},
			{Command: "agent-testbench research feature --feature TEXT --require-min-matches 3 --json", Purpose: "Gate a CLI design slice on enough qualifying open-source references."},
		}
	default:
		commands = []featureNextCommand{
			{Command: "agent-testbench research search --query TEXT --json", Purpose: "Rank candidate feature records before choosing the next CLI command."},
			{Command: "agent-testbench research features --filter TEXT --json", Purpose: "Find the closest maintained feature record before choosing the next CLI command."},
		}
	}
	return annotateFeatureNextCommands(commands)
}

func annotateFeatureNextCommands(commands []featureNextCommand) []featureNextCommand {
	catalog := map[string]bool{}
	for _, item := range commandCatalog("").Commands {
		catalog[item.Command] = true
	}
	for index := range commands {
		path := featureNextCommandPath(commands[index].Command)
		if len(path) == 0 {
			continue
		}
		catalogCommand := strings.Join(path, " ")
		commands[index].CommandPath = path
		commands[index].CatalogCommand = catalogCommand
		commands[index].Available = catalog[catalogCommand]
	}
	return commands
}

func featureNextCommandPath(command string) []string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil
	}
	if fields[0] == "agent-testbench" {
		fields = fields[1:]
	}
	path := []string{}
	for _, field := range fields {
		if commandUsagePathStops(field) {
			break
		}
		path = append(path, strings.Trim(field, ","))
	}
	return path
}

func featureVerificationCommands(featureQuery string, requireMinMatches int, indexPath string, nextCommands []featureNextCommand) []string {
	out := []string{"agent-testbench research feature --feature " + quoteCommandValue(featureQuery) + featureRadarIndexFlag(indexPath) + featureRequireMinFlag(requireMinMatches) + " --json"}
	for _, item := range nextCommands {
		if item.Available {
			out = append(out, item.Command)
		}
	}
	return out
}

func featureRequireMinFlag(value int) string {
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf(" --require-min-matches %d", value)
}

func quoteCommandValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `''`
	}
	return quoteShellValue(value)
}

func quoteCommandPathValue(value string) string {
	value = expandUserHomePath(strings.TrimSpace(value))
	if value == "" {
		return `''`
	}
	return quoteShellValue(value)
}

func quoteShellValue(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func featureRadarIndexFlag(indexPath string) string {
	if strings.TrimSpace(indexPath) == "" {
		return ""
	}
	return " --radar-index " + quoteCommandPathValue(indexPath)
}

func featureRadarCatalogMatches(feature featureRadarFeature, filter string) bool {
	if strings.TrimSpace(filter) == "" {
		return true
	}
	needle := normalizeFeatureRadarText(filter)
	haystack := normalizeFeatureRadarText(strings.Join([]string{
		feature.ID,
		feature.Title,
		feature.Intent,
		strings.Join(feature.Aliases, " "),
		featureRadarMatchText(feature.TopMatches),
	}, " "))
	return strings.Contains(haystack, needle)
}

func featureRadarMatchText(matches []featureRadarMatch) string {
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		parts = append(parts, match.FullName)
	}
	return strings.Join(parts, " ")
}

func printFeatureResearchReport(report featureResearchReport) {
	fmt.Printf("Feature: %s (%s)\n", report.Feature.Title, report.Feature.ID)
	if report.Feature.Intent != "" {
		fmt.Printf("Intent: %s\n", report.Feature.Intent)
	}
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	if report.SourceGeneratedAt != "" {
		fmt.Printf("Radar index: %s\n", report.SourceGeneratedAt)
	}
	fmt.Println("References:")
	for _, match := range report.Matches {
		fmt.Printf("- %s (%d stars, pushed %s, score %d)\n", match.FullName, match.Stars, shortDate(match.PushedAt), match.FeatureScore)
		if match.URL != "" {
			fmt.Printf("  %s\n", match.URL)
		}
		if len(match.Reasons) > 0 {
			fmt.Printf("  %s\n", strings.Join(match.Reasons, "; "))
		}
	}
	if len(report.NextCommands) > 0 {
		fmt.Println("Next commands:")
		for _, item := range report.NextCommands {
			fmt.Printf("- %s\n", item.Command)
			if item.Purpose != "" {
				fmt.Printf("  %s\n", item.Purpose)
			}
		}
	}
}

func printFeatureRadarCatalogReport(report featureRadarCatalogReport) {
	fmt.Println("Feature Radar")
	fmt.Printf("Features: %d\n", report.Count)
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	if report.SourceGeneratedAt != "" {
		fmt.Printf("Radar index: %s\n", report.SourceGeneratedAt)
	}
	if report.Filter != "" {
		fmt.Printf("Filter: %s\n", report.Filter)
	}
	for _, feature := range report.Features {
		fmt.Printf("- %s (%s, %d references)\n", feature.Title, feature.ID, feature.MatchCount)
		if feature.Intent != "" {
			fmt.Printf("  %s\n", feature.Intent)
		}
		if len(feature.TopMatches) > 0 {
			fmt.Printf("  Top reference: %s\n", feature.TopMatches[0].FullName)
		}
	}
}

func printFeatureSearchReport(report featureSearchReport) {
	fmt.Println("Feature Search")
	fmt.Printf("Query: %s\n", report.Query)
	fmt.Printf("Candidates: %d\n", report.Count)
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	if report.SourceGeneratedAt != "" {
		fmt.Printf("Radar index: %s\n", report.SourceGeneratedAt)
	}
	for _, candidate := range report.Candidates {
		fmt.Printf("- %s (%s): score=%d references=%d gate=%s\n", candidate.Title, candidate.ID, candidate.Score, candidate.References, candidate.Gate)
		if len(candidate.MatchedTokens) > 0 {
			fmt.Printf("  matched: %s\n", strings.Join(candidate.MatchedTokens, ", "))
		}
		if candidate.PlanCommand != "" {
			fmt.Printf("  Plan: %s\n", candidate.PlanCommand)
		}
		if len(candidate.TopReferences) > 0 {
			fmt.Printf("  Top reference: %s\n", candidate.TopReferences[0].FullName)
		}
	}
}

func printFeatureCoverageReport(report featureCoverageReport) {
	fmt.Println("Feature Coverage")
	fmt.Printf("Reference gate: %s required=%d passed=%d failed=%d\n", statusWord(report.OK), report.ReferenceGate.Required, report.ReferenceGate.Passed, report.ReferenceGate.Failed)
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	for _, feature := range report.Features {
		fmt.Printf("- %s (%s): %s references=%d\n", feature.Title, feature.ID, feature.Gate, feature.References)
		for _, match := range feature.TopReferences {
			fmt.Printf("  - %s (%d stars, pushed %s)\n", match.FullName, match.Stars, shortDate(match.PushedAt))
		}
	}
}

func printFeatureAuditReport(report featureAuditReport) {
	fmt.Println("Research Audit")
	fmt.Printf("Result: %s\n", statusWord(report.OK))
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	fmt.Printf("Counts: features=%d references=%d projects=%d violations=%d\n", report.Counts.Features, report.Counts.References, report.Counts.Projects, report.Counts.Violations)
	for _, violation := range report.Violations {
		target := violation.FeatureID
		if violation.FullName != "" {
			target += " " + violation.FullName
		}
		fmt.Printf("- %s %s expected %s actual %s\n", target, violation.Field, violation.Expected, violation.Actual)
	}
}

func printFeatureStatusReport(report featureStatusReport) {
	fmt.Println("Research Status")
	fmt.Printf("Fresh: %s\n", statusWord(report.Fresh))
	fmt.Printf("Generated: %s\n", researchFirstNonEmpty(report.SourceGeneratedAt, report.GeneratedAt))
	fmt.Printf("Age: %dh max=%dh\n", report.AgeHours, report.MaxAgeHours)
	if report.StaleReason != "" {
		fmt.Printf("Stale reason: %s\n", report.StaleReason)
	}
	fmt.Printf("Counts: features=%d references=%d projects=%d cachedResults=%d cachedFeatures=%d\n", report.Counts.Features, report.Counts.References, report.Counts.Projects, report.Counts.CachedResults, report.Counts.CachedFeatures)
	fmt.Println("Refresh commands:")
	for _, command := range report.NextRefreshCommands {
		fmt.Printf("- %s\n", command)
	}
}

func printFeatureMatrixReport(report featureMatrixReport) {
	fmt.Println("Feature Matrix")
	fmt.Printf("Features: %d\n", report.Count)
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	if report.SourceGeneratedAt != "" {
		fmt.Printf("Radar index: %s\n", report.SourceGeneratedAt)
	}
	if report.Filter != "" {
		fmt.Printf("Filter: %s\n", report.Filter)
	}
	for _, feature := range report.Features {
		fmt.Printf("- %s (%s, %d references)\n", feature.Title, feature.ID, feature.References)
		if feature.Intent != "" {
			fmt.Printf("  %s\n", feature.Intent)
		}
		for _, ref := range feature.TopReferences {
			fmt.Printf("  - %s (%d stars, pushed %s, score %d)\n", ref.FullName, ref.Stars, shortDate(ref.PushedAt), ref.FeatureScore)
			if ref.Language != "" {
				fmt.Printf("    language: %s\n", ref.Language)
			}
			if len(ref.MatchedFeatures) > 0 {
				fmt.Printf("    features: %s\n", strings.Join(ref.MatchedFeatures, ", "))
			}
			if len(ref.Reasons) > 0 {
				fmt.Printf("    reasons: %s\n", strings.Join(ref.Reasons, "; "))
			}
		}
	}
}

func printFeatureRefreshPlanReport(report featureRefreshPlanReport) {
	fmt.Println("Research Refresh Plan")
	if report.NeedsRefresh {
		fmt.Println("Needs refresh: yes")
	} else {
		fmt.Println("Needs refresh: no")
	}
	if report.SourceGeneratedAt != "" {
		fmt.Printf("Radar index: %s\n", report.SourceGeneratedAt)
	}
	fmt.Printf("Checks: fresh=%s audit=%s coverage=%s\n", statusWord(report.Checks.Fresh), statusWord(report.Checks.AuditOK), statusWord(report.Checks.CoverageOK))
	fmt.Printf("Counts: features=%d references=%d projects=%d auditViolations=%d coverageFailures=%d projectViolations=%d\n", report.Counts.Features, report.Counts.References, report.Counts.Projects, report.Counts.AuditViolations, report.Counts.CoverageFailures, report.Counts.ProjectViolations)
	if len(report.Reasons) > 0 {
		fmt.Println("Reasons:")
		for _, reason := range report.Reasons {
			fmt.Printf("- %s\n", reason)
		}
	}
	if len(report.FocusFeatures) > 0 {
		fmt.Println("Focus features:")
		for _, feature := range report.FocusFeatures {
			fmt.Printf("- %s (%s): %s references=%d\n", feature.Title, feature.ID, feature.Gate, feature.References)
			if len(feature.Reasons) > 0 {
				fmt.Printf("  reasons: %s\n", strings.Join(feature.Reasons, "; "))
			}
			fmt.Printf("  matrix: %s\n", feature.MatrixCommand)
			fmt.Printf("  plan: %s\n", feature.PlanCommand)
			fmt.Printf("  refresh: %s\n", feature.RefreshCommand)
		}
	}
	fmt.Println("Next commands:")
	for _, command := range report.NextCommands {
		fmt.Printf("- %s\n", command)
	}
}

func printFeatureRoadmapReport(report featureRoadmapReport) {
	fmt.Println("Research Roadmap")
	fmt.Printf("Reference gate: %s required=%d passed=%d failed=%d\n", statusWord(report.OK), report.ReferenceGate.Required, report.ReferenceGate.Passed, report.ReferenceGate.Failed)
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	for _, item := range report.Items {
		fmt.Printf("- %s (%s): %s score=%d references=%d commands=%d\n", item.Title, item.ID, item.Gate, item.ReadinessScore, item.References, item.AvailableCommands)
		if len(item.TopReferences) > 0 {
			fmt.Printf("  Top reference: %s\n", item.TopReferences[0].FullName)
		}
		fmt.Printf("  Plan: %s\n", item.PlanCommand)
	}
}

func printFeatureBacklogReport(report featureBacklogReport) {
	fmt.Println("Research Backlog")
	fmt.Printf("Reference gate: %s required=%d passed=%d failed=%d\n", statusWord(report.OK), report.ReferenceGate.Required, report.ReferenceGate.Passed, report.ReferenceGate.Failed)
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	for _, item := range report.Items {
		fmt.Printf("- P%d %s (%s): %s score=%d\n", item.Priority, item.Title, item.FeatureID, item.Status, item.ReadinessScore)
		if len(item.References) > 0 {
			fmt.Printf("  Reference: %s\n", item.References[0].FullName)
		}
		fmt.Printf("  Plan: %s\n", item.PlanCommand)
		if len(item.VerificationCommands) > 0 {
			fmt.Printf("  Verify: %s\n", item.VerificationCommands[0])
		}
		fmt.Println("  Acceptance:")
		for _, criterion := range item.AcceptanceCriteria {
			fmt.Printf("  - %s\n", criterion)
		}
	}
}

func printFeatureGateReport(report featureGateReport) {
	fmt.Println("Research Gate")
	fmt.Printf("Feature: %s (%s)\n", report.Feature.Title, report.Feature.ID)
	fmt.Printf("Checks: fresh=%s audit=%s references=%s command=%s\n", statusWord(report.Checks.Fresh), statusWord(report.Checks.AuditOK), statusWord(report.Checks.ReferenceGateOK), statusWord(report.Checks.CommandGateOK))
	fmt.Printf("Reference gate: %s required=%d found=%d\n", statusWord(report.ReferenceGate.OK), report.ReferenceGate.Required, report.ReferenceGate.Found)
	matched := report.CommandGate.Matched.CatalogCommand
	if matched == "" {
		matched = "-"
	}
	fmt.Printf("Command gate: %s required=%s matched=%s\n", statusWord(report.CommandGate.OK), report.CommandGate.Required, matched)
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	if report.SourceGeneratedAt != "" {
		fmt.Printf("Radar index: %s\n", report.SourceGeneratedAt)
	}
	if len(report.Reasons) > 0 {
		fmt.Println("Reasons:")
		for _, reason := range report.Reasons {
			fmt.Printf("- %s\n", reason)
		}
	}
	fmt.Println("References:")
	for _, match := range report.References {
		fmt.Printf("- %s (%d stars, pushed %s)\n", match.FullName, match.Stars, shortDate(match.PushedAt))
	}
	fmt.Println("Verification commands:")
	for _, command := range report.VerificationCommands {
		fmt.Printf("- %s\n", command)
	}
}

func printFeatureResearchPlan(report featureResearchPlanReport) {
	fmt.Println("Research Plan")
	fmt.Printf("Feature: %s (%s)\n", report.Feature.Title, report.Feature.ID)
	fmt.Printf("Reference gate: %s required=%d found=%d\n", statusWord(report.ReferenceGate.OK), report.ReferenceGate.Required, report.ReferenceGate.Found)
	fmt.Printf("Policy: stars >= %d, pushed >= %s\n", report.Policy.MinStars, report.Policy.PushedAfter)
	fmt.Println("References:")
	for _, match := range report.References {
		fmt.Printf("- %s (%d stars, pushed %s)\n", match.FullName, match.Stars, shortDate(match.PushedAt))
	}
	fmt.Println("Next commands:")
	for _, command := range report.NextCommands {
		fmt.Printf("- %s available=%t\n", command.Command, command.Available)
	}
	fmt.Println("Verification commands:")
	for _, command := range report.VerificationCommands {
		fmt.Printf("- %s\n", command)
	}
}

func printFeatureResearchPlanMarkdown(report featureResearchPlanReport) {
	fmt.Printf("# Research Plan: %s\n\n", report.Feature.Title)
	if report.Feature.Intent != "" {
		fmt.Printf("%s\n\n", report.Feature.Intent)
	}
	fmt.Println("## Reference Gate")
	fmt.Println()
	fmt.Println("| Required | Found | Status |")
	fmt.Println("| --- | --- | --- |")
	fmt.Printf("| %d | %d | %s |\n\n", report.ReferenceGate.Required, report.ReferenceGate.Found, statusWord(report.ReferenceGate.OK))
	fmt.Println("## References")
	fmt.Println()
	fmt.Println("| Project | Stars | Pushed | Score |")
	fmt.Println("| --- | ---: | --- | ---: |")
	for _, match := range report.References {
		project := match.FullName
		if match.URL != "" {
			project = "[" + match.FullName + "](" + match.URL + ")"
		}
		fmt.Printf("| %s | %d | %s | %d |\n", project, match.Stars, shortDate(match.PushedAt), match.FeatureScore)
	}
	fmt.Println()
	fmt.Println("## Next Commands")
	fmt.Println()
	for _, command := range report.NextCommands {
		fmt.Printf("- `%s`", command.Command)
		if command.CatalogCommand != "" {
			fmt.Printf(" (%s, available=%t)", command.CatalogCommand, command.Available)
		}
		fmt.Println()
		if command.Purpose != "" {
			fmt.Printf("  - %s\n", command.Purpose)
		}
	}
	fmt.Println()
	fmt.Println("## Verification Commands")
	fmt.Println()
	for _, command := range report.VerificationCommands {
		fmt.Printf("- `%s`\n", command)
	}
}

func statusWord(ok bool) string {
	if ok {
		return "ok"
	}
	return "failed"
}

func shortDate(value string) string {
	if len(value) >= len("2006-01-02") {
		return value[:len("2006-01-02")]
	}
	return value
}
