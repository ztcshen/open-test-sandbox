package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
)

type storeCopyStateReport struct {
	OK              bool                         `json:"ok"`
	Source          string                       `json:"source,omitempty"`
	Target          string                       `json:"target,omitempty"`
	Error           string                       `json:"error,omitempty"`
	ProfileCatalogs int                          `json:"profileCatalogs"`
	ProfileIndexes  int                          `json:"profileIndexes"`
	ConfigVersions  int                          `json:"configVersions"`
	ReadModels      []string                     `json:"readModels,omitempty"`
	Environments    int                          `json:"environments"`
	EnvironmentIDs  []string                     `json:"environmentIds,omitempty"`
	EnvironmentRefs []storeCopyEnvironmentReport `json:"environmentRefs,omitempty"`
	ComponentGraphs int                          `json:"componentGraphs"`
	ComponentRefs   []storeCopyComponentReport   `json:"componentRefs,omitempty"`
	RunsSkipped     bool                         `json:"runsSkipped"`
	Notes           []string                     `json:"notes,omitempty"`
}

type storeCopyEnvironmentReport struct {
	ID                     string `json:"id"`
	Status                 string `json:"status,omitempty"`
	Verified               bool   `json:"verified"`
	VerificationWorkflowID string `json:"verificationWorkflowId,omitempty"`
	LastVerificationStatus string `json:"lastVerificationStatus,omitempty"`
	EvidenceComplete       bool   `json:"evidenceComplete"`
	TopologyComplete       bool   `json:"topologyComplete"`
}

type storeCopyComponentReport struct {
	EnvironmentID           string `json:"environmentId"`
	Components              int    `json:"components"`
	Dependencies            int    `json:"dependencies"`
	Assets                  int    `json:"assets"`
	InlineAssetBytes        int    `json:"inlineAssetBytes"`
	LargestInlineAssetID    string `json:"largestInlineAssetId,omitempty"`
	LargestInlineAssetBytes int    `json:"largestInlineAssetBytes,omitempty"`
}

func runStoreCopy(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("store copy", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	sourceRef := flags.String("from", "", "Source Store name or DSN")
	targetRef := flags.String("to", "", "Target Store name or DSN")
	requireEnvironment := flags.String("require-environment", "", "Require an environment id to be present after copy")
	requireWorkflow := flags.String("require-verification-workflow", "", "Require the copied environment to use this verification workflow id")
	requireVerifiedEnvironment := flags.Bool("require-verified-environment", false, "Require the required environment to be verified with Evidence and topology flags")
	requireMinComponents := flags.Int("require-min-components", 0, "Require at least this many copied components for --require-environment")
	requireMinDependencies := flags.Int("require-min-dependencies", 0, "Require at least this many copied dependencies for --require-environment")
	requireMinAssets := flags.Int("require-min-assets", 0, "Require at least this many copied assets for --require-environment")
	requireInlineAssetBytes := flags.Int("require-inline-asset-bytes", 0, "Require at least this many inline asset bytes for --require-environment")
	jsonOutput := flags.Bool("json", false, "Emit a machine-readable JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	sourceURL, err := resolveRequiredStoreReference(*sourceRef, "")
	if err != nil {
		return fmt.Errorf("resolve source Store: %w", err)
	}
	targetURL, err := resolveRequiredStoreReference(*targetRef, "")
	if err != nil {
		return fmt.Errorf("resolve target Store: %w", err)
	}
	source, err := openStore(ctx, sourceURL)
	if err != nil {
		return fmt.Errorf("open source Store: %w", err)
	}
	defer source.Close()
	target, err := openStore(ctx, targetURL)
	if err != nil {
		return fmt.Errorf("open target Store: %w", err)
	}
	defer target.Close()
	report, err := copyStoreCurrentState(ctx, source, target)
	if err != nil {
		return err
	}
	report.OK = true
	report.Source = maskStoreURL(sourceURL)
	report.Target = maskStoreURL(targetURL)
	requirements := storeCopyRequirements{
		EnvironmentID:          *requireEnvironment,
		VerificationWorkflowID: *requireWorkflow,
		VerifiedEnvironment:    *requireVerifiedEnvironment,
		MinComponents:          *requireMinComponents,
		MinDependencies:        *requireMinDependencies,
		MinAssets:              *requireMinAssets,
		MinInlineAssetBytes:    *requireInlineAssetBytes,
	}
	if err := validateStoreCopyRequirements(report, requirements); err != nil {
		report.OK = false
		report.Error = err.Error()
		if *jsonOutput {
			if jsonErr := writeIndentedJSON(report); jsonErr != nil {
				return jsonErr
			}
		}
		return err
	}
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Copied current Store data\n")
	fmt.Printf("Source: %s\n", report.Source)
	fmt.Printf("Target: %s\n", report.Target)
	fmt.Printf("Profile catalogs: %d\n", report.ProfileCatalogs)
	fmt.Printf("Profile indexes: %d\n", report.ProfileIndexes)
	fmt.Printf("Config versions: %d\n", report.ConfigVersions)
	fmt.Printf("Read models: %d\n", len(report.ReadModels))
	fmt.Printf("Environments: %d\n", report.Environments)
	fmt.Printf("Component graphs: %d\n", report.ComponentGraphs)
	return nil
}

func copyStoreCurrentState(ctx context.Context, source store.Store, target store.Store) (storeCopyStateReport, error) {
	report := storeCopyStateReport{
		RunsSkipped: true,
		Notes: []string{
			"Copied restore-critical current Store metadata only.",
			"Historical runs, Evidence indexes, and topology rows are intentionally not copied; rerun acceptance on the target Store.",
		},
	}
	catalog, err := source.GetProfileCatalog(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return report, fmt.Errorf("read source profile catalog: %w", err)
	}
	if err == nil {
		if err := target.ReplaceProfileCatalog(ctx, catalog); err != nil {
			return report, fmt.Errorf("write target profile catalog %q: %w", catalog.ProfileID, err)
		}
		report.ProfileCatalogs = 1
		if catalog.ProfileID != "" {
			index, err := source.GetProfileIndex(ctx, catalog.ProfileID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return report, fmt.Errorf("read source profile index %q: %w", catalog.ProfileID, err)
			}
			if err == nil {
				if _, err := target.UpsertProfileIndex(ctx, index); err != nil {
					return report, fmt.Errorf("write target profile index %q: %w", index.ProfileID, err)
				}
				report.ProfileIndexes = 1
			}
			configVersion, configErr := source.GetActiveConfigVersion(ctx)
			generatedAt := catalog.IndexedAt
			configVersionID := "store-copy." + catalog.ProfileID
			if configErr != nil && !errors.Is(configErr, store.ErrNotFound) {
				return report, fmt.Errorf("read source active config version: %w", configErr)
			}
			if configErr == nil {
				configVersion.Active = true
				written, err := target.UpsertConfigVersion(ctx, configVersion)
				if err != nil {
					return report, fmt.Errorf("write target active config version %q: %w", configVersion.ID, err)
				}
				report.ConfigVersions = 1
				configVersionID = written.ID
				if !written.PublishedAt.IsZero() {
					generatedAt = written.PublishedAt
				}
			} else {
				report.Notes = append(report.Notes, "Source Store has no active config version; target read models use a synthetic store-copy version id.")
			}
			if generatedAt.IsZero() {
				generatedAt = time.Now().UTC()
			}
			keys, err := controlplane.UpsertProfileReadModels(ctx, target, catalog, configVersionID, generatedAt)
			if err != nil {
				return report, fmt.Errorf("write target read models for profile %q: %w", catalog.ProfileID, err)
			}
			report.ReadModels = keys
		}
	}
	environments, err := source.ListEnvironments(ctx)
	if err != nil {
		return report, fmt.Errorf("read source environments: %w", err)
	}
	for _, env := range environments {
		if _, err := target.UpsertEnvironment(ctx, env); err != nil {
			return report, fmt.Errorf("write target environment %q: %w", env.ID, err)
		}
		report.Environments++
		report.EnvironmentIDs = append(report.EnvironmentIDs, env.ID)
		report.EnvironmentRefs = append(report.EnvironmentRefs, storeCopyEnvironmentReport{
			ID:                     env.ID,
			Status:                 env.Status,
			Verified:               env.Verified,
			VerificationWorkflowID: env.VerificationWorkflowID,
			LastVerificationStatus: env.LastVerificationStatus,
			EvidenceComplete:       env.EvidenceComplete,
			TopologyComplete:       env.TopologyComplete,
		})
		graph, err := source.GetEnvironmentComponentGraph(ctx, env.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return report, fmt.Errorf("read source component graph %q: %w", env.ID, err)
		}
		if err == nil && (len(graph.Components) > 0 || len(graph.Dependencies) > 0 || len(graph.Assets) > 0) {
			if err := target.ReplaceEnvironmentComponentGraph(ctx, env.ID, graph); err != nil {
				return report, fmt.Errorf("write target component graph %q: %w", env.ID, err)
			}
			report.ComponentGraphs++
			report.ComponentRefs = append(report.ComponentRefs, storeCopyComponentGraphReport(env.ID, graph))
		}
	}
	return report, nil
}

func storeCopyComponentGraphReport(envID string, graph store.EnvironmentComponentGraph) storeCopyComponentReport {
	out := storeCopyComponentReport{
		EnvironmentID: envID,
		Components:    len(graph.Components),
		Dependencies:  len(graph.Dependencies),
		Assets:        len(graph.Assets),
	}
	for _, asset := range graph.Assets {
		size := len(asset.ContentInline)
		out.InlineAssetBytes += size
		if size > out.LargestInlineAssetBytes {
			out.LargestInlineAssetID = asset.AssetID
			out.LargestInlineAssetBytes = size
		}
	}
	return out
}

type storeCopyRequirements struct {
	EnvironmentID          string
	VerificationWorkflowID string
	VerifiedEnvironment    bool
	MinComponents          int
	MinDependencies        int
	MinAssets              int
	MinInlineAssetBytes    int
}

func validateStoreCopyRequirements(report storeCopyStateReport, requirements storeCopyRequirements) error {
	requiredEnvironmentID := strings.TrimSpace(requirements.EnvironmentID)
	if requiredEnvironmentID == "" {
		if strings.TrimSpace(requirements.VerificationWorkflowID) != "" || requirements.VerifiedEnvironment || requirements.MinComponents > 0 || requirements.MinDependencies > 0 || requirements.MinAssets > 0 || requirements.MinInlineAssetBytes > 0 {
			return errors.New("store copy requirement flags require --require-environment ENV_ID")
		}
		return nil
	}
	env, ok := storeCopyFindEnvironment(report, requiredEnvironmentID)
	if !ok {
		return fmt.Errorf("required environment %q was not copied", requiredEnvironmentID)
	}
	requiredWorkflowID := strings.TrimSpace(requirements.VerificationWorkflowID)
	if requiredWorkflowID != "" && env.VerificationWorkflowID != requiredWorkflowID {
		return fmt.Errorf("required environment %q verification workflow is %q, want %q", requiredEnvironmentID, env.VerificationWorkflowID, requiredWorkflowID)
	}
	if requirements.VerifiedEnvironment && (!env.Verified || env.Status != "verified" || !env.EvidenceComplete || !env.TopologyComplete) {
		return fmt.Errorf("required environment %q is not verified with complete Evidence and topology flags", requiredEnvironmentID)
	}
	requiresGraph := requirements.MinComponents > 0 || requirements.MinDependencies > 0 || requirements.MinAssets > 0 || requirements.MinInlineAssetBytes > 0
	graph := storeCopyComponentReport{}
	if requiresGraph {
		var ok bool
		graph, ok = storeCopyFindComponentGraph(report, requiredEnvironmentID)
		if !ok || graph.Components == 0 {
			return fmt.Errorf("required environment %q has no copied component graph", requiredEnvironmentID)
		}
	}
	if requirements.MinComponents > 0 && graph.Components < requirements.MinComponents {
		return fmt.Errorf("required environment %q copied component count is %d, below required minimum %d", requiredEnvironmentID, graph.Components, requirements.MinComponents)
	}
	if requirements.MinDependencies > 0 && graph.Dependencies < requirements.MinDependencies {
		return fmt.Errorf("required environment %q copied dependency count is %d, below required minimum %d", requiredEnvironmentID, graph.Dependencies, requirements.MinDependencies)
	}
	if requirements.MinAssets > 0 && graph.Assets < requirements.MinAssets {
		return fmt.Errorf("required environment %q copied asset count is %d, below required minimum %d", requiredEnvironmentID, graph.Assets, requirements.MinAssets)
	}
	if requirements.MinInlineAssetBytes > 0 && graph.InlineAssetBytes < requirements.MinInlineAssetBytes {
		return fmt.Errorf("required environment %q copied inline asset bytes is %d, below required minimum %d", requiredEnvironmentID, graph.InlineAssetBytes, requirements.MinInlineAssetBytes)
	}
	return nil
}

func storeCopyFindEnvironment(report storeCopyStateReport, environmentID string) (storeCopyEnvironmentReport, bool) {
	for _, env := range report.EnvironmentRefs {
		if env.ID == environmentID {
			return env, true
		}
	}
	return storeCopyEnvironmentReport{}, false
}

func storeCopyFindComponentGraph(report storeCopyStateReport, environmentID string) (storeCopyComponentReport, bool) {
	for _, graph := range report.ComponentRefs {
		if graph.EnvironmentID == environmentID {
			return graph, true
		}
	}
	return storeCopyComponentReport{}, false
}
