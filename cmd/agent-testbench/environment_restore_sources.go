package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agent-testbench/internal/domain/environmentsource"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
)

type environmentRestoreSourcePolicy struct {
	RemoteOnly bool     `json:"remoteOnly"`
	OK         bool     `json:"ok"`
	Violations []string `json:"violations,omitempty"`
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
	return environmentsource.ComponentAssetRemoteRefOK(asset.TargetPath, asset.RemoteRefJSON)
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
		return environmentRestoreExistingRepo(ctx, spec, report, execute, pull)
	}
	return environmentRestoreMissingRepo(ctx, spec, report, execute)
}

func environmentRestoreExistingRepo(ctx context.Context, spec environmentRestoreRepoSpec, report environmentRestoreRepoReport, execute bool, pull bool) environmentRestoreRepoReport {
	report.Exists = true
	if strings.TrimSpace(spec.URL) != "" && environmentRestoreDirIsEmpty(spec.Checkout) {
		return environmentRestoreEmptyCheckout(ctx, spec, report, execute)
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
		return environmentRestoreExistingRef(ctx, spec, report, execute)
	}
	if strings.TrimSpace(spec.URL) == "" || !execute || !pull {
		report.Action = "use-existing-checkout"
		return report
	}
	return environmentRestorePullExisting(ctx, spec, report)
}

func environmentRestoreEmptyCheckout(ctx context.Context, spec environmentRestoreRepoSpec, report environmentRestoreRepoReport, execute bool) environmentRestoreRepoReport {
	if !execute {
		report.Exists = false
		report.Action = "clone"
		args := restoreGitCloneArgs(spec)
		report.Command = append([]string{"git"}, args...)
		return report
	}
	return environmentRestoreCloneIntoCheckout(ctx, spec, report)
}

func environmentRestoreExistingRef(ctx context.Context, spec environmentRestoreRepoSpec, report environmentRestoreRepoReport, execute bool) environmentRestoreRepoReport {
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

func environmentRestorePullExisting(ctx context.Context, spec environmentRestoreRepoSpec, report environmentRestoreRepoReport) environmentRestoreRepoReport {
	args := []string{"-C", spec.Checkout, "pull", "--ff-only"}
	report.Action = "pull-existing-checkout"
	report.Command = append([]string{"git"}, args...)
	report.Output, report.Error = runRestoreGitCommand(ctx, args...)
	report.OK = report.Error == ""
	return report
}

func environmentRestoreMissingRepo(ctx context.Context, spec environmentRestoreRepoSpec, report environmentRestoreRepoReport, execute bool) environmentRestoreRepoReport {
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

func restoreGitCloneArgs(spec environmentRestoreRepoSpec) []string {
	args := []string{"clone"}
	if strings.TrimSpace(spec.Branch) != "" {
		args = append(args, "--branch", strings.TrimSpace(spec.Branch))
	}
	args = append(args, strings.TrimSpace(spec.URL), strings.TrimSpace(spec.Checkout))
	return args
}

func safeCheckoutDirName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "service"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-")
	return replacer.Replace(value)
}
