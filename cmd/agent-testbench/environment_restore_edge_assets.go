package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-testbench/internal/store"
)

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

func environmentRestoreApplyEdgeAssets(ctx context.Context, graph store.EnvironmentComponentGraph, compose map[string]any, workspace string, execute bool, composeBaseArgs []string) []environmentRestoreAppliedAsset {
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
			attempts, errText := runRestoreMySQLCommandWithInputRetry(ctx, workspace, item.Command, content)
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

func runRestoreMySQLCommandWithInputRetry(ctx context.Context, workdir string, command []string, input string) (int, string) {
	const maxAttempts = 60
	const delay = time.Second
	var lastErr string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, errText := runRestoreCommandWithInput(ctx, workdir, command, input)
		if errText == "" {
			return attempt, ""
		}
		lastErr = errText
		if !environmentRestoreMySQLApplyErrCanRetry(errText) {
			return attempt, errText
		}
		if attempt == maxAttempts {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return attempt, ctx.Err().Error()
		case <-timer.C:
		}
	}
	return maxAttempts, lastErr
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
