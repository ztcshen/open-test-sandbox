package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"open-test-sandbox/internal/controlplane"
	"open-test-sandbox/internal/store"
)

type storeCopyStateReport struct {
	OK              bool     `json:"ok"`
	Source          string   `json:"source,omitempty"`
	Target          string   `json:"target,omitempty"`
	ProfileCatalogs int      `json:"profileCatalogs"`
	ProfileIndexes  int      `json:"profileIndexes"`
	ConfigVersions  int      `json:"configVersions"`
	ReadModels      []string `json:"readModels,omitempty"`
	Environments    int      `json:"environments"`
	ComponentGraphs int      `json:"componentGraphs"`
	RunsSkipped     bool     `json:"runsSkipped"`
	Notes           []string `json:"notes,omitempty"`
}

func runStoreCopy(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("store copy", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	sourceRef := flags.String("from", "", "Source Store name or DSN")
	targetRef := flags.String("to", "", "Target Store name or DSN")
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
	if *jsonOutput {
		return writeIndentedJSON(report)
	}
	fmt.Printf("Copied Store current state\n")
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
		graph, err := source.GetEnvironmentComponentGraph(ctx, env.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return report, fmt.Errorf("read source component graph %q: %w", env.ID, err)
		}
		if err == nil && (len(graph.Components) > 0 || len(graph.Dependencies) > 0 || len(graph.Assets) > 0) {
			if err := target.ReplaceEnvironmentComponentGraph(ctx, env.ID, graph); err != nil {
				return report, fmt.Errorf("write target component graph %q: %w", env.ID, err)
			}
			report.ComponentGraphs++
		}
	}
	return report, nil
}
