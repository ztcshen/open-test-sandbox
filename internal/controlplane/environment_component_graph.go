package controlplane

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"open-test-sandbox/internal/store"

	gonumgraph "gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

type EnvironmentComponentGraphReadiness struct {
	Configured              bool       `json:"configured"`
	OK                      bool       `json:"ok"`
	Components              int        `json:"components"`
	Dependencies            int        `json:"dependencies"`
	BlockingDependencies    int        `json:"blockingDependencies"`
	RuntimeDependencies     int        `json:"runtimeDependencies"`
	Assets                  int        `json:"assets"`
	InlineAssetBytes        int64      `json:"inlineAssetBytes"`
	RemoteAssets            int        `json:"remoteAssets"`
	RemoteAssetBytes        int64      `json:"remoteAssetBytes"`
	MissingRemoteAssetRefs  int        `json:"missingRemoteAssetRefs"`
	RequiredHealthChecks    int        `json:"requiredHealthChecks"`
	MissingHealthChecks     int        `json:"missingHealthChecks"`
	BlockingOrder           []string   `json:"blockingOrder,omitempty"`
	BlockingCycles          [][]string `json:"blockingCycles,omitempty"`
	LargestInlineAssetID    string     `json:"largestInlineAssetId,omitempty"`
	LargestInlineAssetBytes int64      `json:"largestInlineAssetBytes,omitempty"`
	Error                   string     `json:"error,omitempty"`
}

func EnvironmentComponentGraphReadinessReport(envID string, graph store.EnvironmentComponentGraph) EnvironmentComponentGraphReadiness {
	report := EnvironmentComponentGraphReadiness{
		Configured:   len(graph.Components) > 0 || len(graph.Dependencies) > 0 || len(graph.Assets) > 0,
		OK:           true,
		Components:   len(graph.Components),
		Dependencies: len(graph.Dependencies),
		Assets:       len(graph.Assets),
	}
	if !report.Configured {
		return report
	}
	if err := store.ValidateEnvironmentComponentGraph(envID, graph); err != nil {
		report.OK = false
		report.Error = err.Error()
		return report
	}
	graphErrors := []string{}
	componentHealthErrors := []string{}
	for _, component := range graph.Components {
		if component.Required {
			report.RequiredHealthChecks++
			if _, errText := normalizeEnvironmentComponentHealthCheck(component); errText != "" {
				report.MissingHealthChecks++
				componentHealthErrors = append(componentHealthErrors, strings.TrimSpace(component.ComponentID)+": "+errText)
			}
		}
	}
	for _, dep := range graph.Dependencies {
		if strings.EqualFold(strings.TrimSpace(dep.Phase), "runtime") {
			report.RuntimeDependencies++
		} else {
			report.BlockingDependencies++
		}
	}
	blockingOrder, blockingCycles, blockingErr := environmentComponentBlockingDependencyOrder(graph)
	report.BlockingOrder = blockingOrder
	report.BlockingCycles = blockingCycles
	if blockingErr != "" {
		graphErrors = append(graphErrors, blockingErr)
	}
	for _, asset := range graph.Assets {
		size := int64(len(asset.ContentInline))
		report.InlineAssetBytes += size
		if size > report.LargestInlineAssetBytes {
			report.LargestInlineAssetBytes = size
			report.LargestInlineAssetID = asset.AssetID
		}
		if strings.TrimSpace(asset.ContentInline) == "" && strings.TrimSpace(asset.RemoteRefJSON) != "" {
			report.RemoteAssets++
			report.RemoteAssetBytes += asset.SizeBytes
			if !environmentComponentAssetRemoteRefOK(asset) {
				report.MissingRemoteAssetRefs++
			}
		}
	}
	if report.MissingHealthChecks > 0 {
		detail := fmt.Sprintf("%d required component(s) are missing valid Store-backed health checks", report.MissingHealthChecks)
		if len(componentHealthErrors) > 0 {
			detail += ": " + strings.Join(componentHealthErrors, "; ")
		}
		graphErrors = append(graphErrors, detail)
	}
	if report.MissingRemoteAssetRefs > 0 {
		graphErrors = append(graphErrors, fmt.Sprintf("%d remote component asset(s) are missing remote Git URL/path metadata", report.MissingRemoteAssetRefs))
	}
	if len(graphErrors) > 0 {
		report.OK = false
		report.Error = strings.Join(graphErrors, "; ")
	}
	return report
}

func normalizeEnvironmentComponentHealthCheck(component store.EnvironmentComponent) (map[string]any, string) {
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
	kind := strings.TrimSpace(valueString(normalized["kind"]))
	if kind == "" {
		kind = strings.TrimSpace(valueString(normalized["type"]))
	}
	if kind == "" && strings.TrimSpace(valueString(normalized["url"])) != "" {
		kind = "url"
	}
	normalized["kind"] = kind
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

func environmentComponentBlockingDependencyOrder(g store.EnvironmentComponentGraph) ([]string, [][]string, string) {
	if len(g.Components) == 0 {
		return nil, nil, ""
	}
	directed := simple.NewDirectedGraph()
	idByNode := map[int64]string{}
	nodeByID := map[string]gonumgraph.Node{}
	for i, component := range g.Components {
		id := strings.TrimSpace(component.ComponentID)
		if id == "" {
			continue
		}
		node := simple.Node(i + 1)
		directed.AddNode(node)
		idByNode[node.ID()] = id
		nodeByID[id] = node
	}
	orderNodes := func(nodes []gonumgraph.Node) {
		sort.Slice(nodes, func(i, j int) bool {
			return idByNode[nodes[i].ID()] < idByNode[nodes[j].ID()]
		})
	}
	selfCycles := [][]string{}
	for _, dep := range g.Dependencies {
		if strings.EqualFold(strings.TrimSpace(dep.Phase), "runtime") {
			continue
		}
		consumer := strings.TrimSpace(dep.ConsumerComponentID)
		provider := strings.TrimSpace(dep.ProviderComponentID)
		from := nodeByID[provider]
		to := nodeByID[consumer]
		if from == nil || to == nil {
			continue
		}
		if from.ID() == to.ID() {
			selfCycles = append(selfCycles, []string{provider, consumer})
			continue
		}
		directed.SetEdge(directed.NewEdge(from, to))
	}
	sorted, err := topo.SortStabilized(directed, orderNodes)
	order := make([]string, 0, len(sorted))
	for _, node := range sorted {
		if node == nil {
			continue
		}
		if id := idByNode[node.ID()]; id != "" {
			order = append(order, id)
		}
	}
	cycles := selfCycles
	if err != nil {
		for _, cycle := range topo.DirectedCyclesIn(directed) {
			cycles = append(cycles, environmentComponentGonumNodeIDs(cycle, idByNode))
		}
		var unorderable topo.Unorderable
		if len(cycles) == 0 && errors.As(err, &unorderable) {
			for _, component := range unorderable {
				cycles = append(cycles, environmentComponentGonumNodeIDs(component, idByNode))
			}
		}
	}
	if len(cycles) > 0 {
		return order, cycles, "blocking component dependencies contain cycle(s): " + environmentComponentCycleText(cycles)
	}
	if err != nil {
		return order, cycles, err.Error()
	}
	return order, nil, ""
}

func environmentComponentGonumNodeIDs(nodes []gonumgraph.Node, idByNode map[int64]string) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if id := idByNode[node.ID()]; id != "" {
			out = append(out, id)
		}
	}
	if len(out) > 1 && out[0] != out[len(out)-1] {
		out = append(out, out[0])
	}
	return out
}

func environmentComponentCycleText(cycles [][]string) string {
	parts := make([]string, 0, len(cycles))
	for _, cycle := range cycles {
		if len(cycle) == 0 {
			continue
		}
		parts = append(parts, strings.Join(cycle, " -> "))
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

func environmentComponentAssetRemoteRefOK(asset store.ComponentConfigAsset) bool {
	ref := jsonObject(asset.RemoteRefJSON)
	path := strings.TrimSpace(valueString(ref["path"]))
	if path == "" {
		path = strings.TrimSpace(asset.TargetPath)
	}
	cleanPath := filepath.Clean(path)
	if path == "" || filepath.IsAbs(path) || cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) {
		return false
	}
	return environmentComponentIsRemoteGitURL(strings.TrimSpace(valueString(ref["url"])))
}

func environmentComponentIsRemoteGitURL(rawURL string) bool {
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
