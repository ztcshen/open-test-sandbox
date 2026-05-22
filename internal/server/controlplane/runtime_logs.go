package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-testbench/internal/store"
)

const runtimeLogLineLimit = 12
const workflowStepRuntimeLogsKind = "runtime_logs"
const workflowStepRuntimeLogCacheTimeout = time.Second

func enrichWorkflowStepLogs(ctx context.Context, runtime store.Store, run store.Run, step map[string]any, topologies []map[string]any) {
	trace := mapFromAny(step["trace"])
	if systems, ok := trace["systems"].([]any); ok && len(systems) > 0 {
		return
	}
	stepID := strings.TrimSpace(valueString(step["stepId"]))
	cacheCtx, cancel := context.WithTimeout(ctx, workflowStepRuntimeLogCacheTimeout)
	defer cancel()
	if cached, ok := cachedWorkflowStepRuntimeLogs(cacheCtx, runtime, run.ID, stepID); ok {
		trace["systems"] = cached
		step["trace"] = trace
		return
	}
	trace["systems"] = pendingWorkflowStepLogSystems(topologies)
	step["trace"] = trace
	scheduleWorkflowStepRuntimeLogCollection(runtime, run, step, topologies)
}

func scheduleWorkflowStepRuntimeLogCollection(runtime store.Store, run store.Run, step map[string]any, topologies []map[string]any) {
	if runtime == nil || run.ID == "" {
		return
	}
	stepCopy := copyMap(step)
	topologyCopy := make([]map[string]any, 0, len(topologies))
	for _, row := range topologies {
		topologyCopy = append(topologyCopy, copyMap(row))
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		collectAndPersistWorkflowStepRuntimeLogs(ctx, runtime, run, stepCopy, topologyCopy)
	}()
}

func collectAndPersistWorkflowStepRuntimeLogs(ctx context.Context, runtime store.Store, run store.Run, step map[string]any, topologies []map[string]any) {
	started := time.Now().UTC()
	stepID := strings.TrimSpace(valueString(step["stepId"]))
	caseID := valueString(step["caseId"])
	status := store.StatusSkipped
	errText := ""
	summary := map[string]any{}
	defer func() {
		finished := time.Now().UTC()
		recordPostProcessTask(ctx, runtime, store.PostProcessTask{
			ID:          run.ID + "." + safeRuntimeLogPathSegment(stepID) + "." + postProcessKindRuntimeLogs,
			RunID:       run.ID,
			WorkflowID:  run.WorkflowID,
			StepID:      stepID,
			CaseID:      caseID,
			Kind:        postProcessKindRuntimeLogs,
			Status:      status,
			StartedAt:   started,
			FinishedAt:  finished,
			DurationMs:  finished.Sub(started).Milliseconds(),
			Error:       errText,
			SummaryJSON: compactJSON(summary),
			CreatedAt:   finished,
		})
	}()
	if stepID == "" {
		errText = "stepId is required"
		status = store.StatusFailed
		return
	}
	if _, ok := cachedWorkflowStepRuntimeLogs(ctx, runtime, run.ID, stepID); ok {
		summary["cacheHit"] = true
		return
	}
	nodes := workflowStepLogNodes(topologies)
	correlators := workflowStepLogCorrelators(step, topologies)
	summary["nodes"] = len(nodes)
	summary["correlators"] = len(correlators)
	if len(nodes) == 0 || len(correlators) == 0 {
		return
	}
	systems := collectRuntimeLogSystems(ctx, nodes, correlators)
	summary["systems"] = len(systems)
	summary["matchedSystems"] = matchedRuntimeLogSystemCount(systems)
	if len(systems) == 0 {
		return
	}
	status = store.StatusPassed
	persistWorkflowStepRuntimeLogs(ctx, runtime, run, step, systems)
}

func matchedRuntimeLogSystemCount(systems []map[string]any) int {
	count := 0
	for _, system := range systems {
		if found, _ := system["found"].(bool); found {
			count++
		}
	}
	return count
}

func pendingWorkflowStepLogSystems(topologies []map[string]any) []map[string]any {
	nodes := workflowStepLogNodes(topologies)
	out := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, map[string]any{
			"id":        node,
			"name":      node,
			"container": runtimeLogNodeName(node),
			"found":     false,
			"pending":   true,
			"coreLogs":  []string{},
			"note":      "Runtime logs are being collected in the background.",
		})
	}
	return out
}

func cachedWorkflowStepRuntimeLogs(ctx context.Context, runtime store.Store, runID string, stepID string) (any, bool) {
	if runtime == nil || runID == "" || stepID == "" {
		return nil, false
	}
	records, err := runtime.ListEvidence(ctx, runID)
	if err != nil {
		return nil, false
	}
	for _, record := range records {
		if record.Kind != workflowStepRuntimeLogsKind || valueString(jsonObject(record.Summary)["stepId"]) != stepID {
			continue
		}
		body, ok := evidenceRecordObject(record)
		if !ok {
			continue
		}
		systems := listFromAny(body["systems"])
		if len(systems) == 0 {
			continue
		}
		return systems, true
	}
	return nil, false
}

func persistWorkflowStepRuntimeLogs(ctx context.Context, runtime store.Store, run store.Run, step map[string]any, systems []map[string]any) {
	if runtime == nil || run.ID == "" || len(systems) == 0 {
		return
	}
	stepID := strings.TrimSpace(valueString(step["stepId"]))
	if stepID == "" {
		return
	}
	payload := map[string]any{
		"runId":    run.ID,
		"stepId":   stepID,
		"caseId":   valueString(step["caseId"]),
		"cachedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"systems":  systems,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	dir := run.EvidenceRoot
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Join(".runtime", "evidence", run.ID)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	uri := filepath.Join(dir, fmt.Sprintf("workflow-step-%s-runtime-logs.json", safeRuntimeLogPathSegment(stepID)))
	if err := os.WriteFile(uri, raw, 0o644); err != nil {
		return
	}
	sum := sha256.Sum256(raw)
	_, _ = runtime.RecordEvidence(ctx, store.EvidenceRecord{
		ID:        fmt.Sprintf("%s.%s.%s", run.ID, safeRuntimeLogPathSegment(stepID), workflowStepRuntimeLogsKind),
		RunID:     run.ID,
		CaseRunID: stepID,
		Kind:      workflowStepRuntimeLogsKind,
		URI:       uri,
		MediaType: "application/json",
		SHA256:    hex.EncodeToString(sum[:]),
		SizeBytes: int64(len(raw)),
		Summary:   compactJSON(map[string]any{"stepId": stepID, "caseId": valueString(step["caseId"]), "systems": len(systems)}),
		CreatedAt: time.Now().UTC(),
	})
}

func safeRuntimeLogPathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "step"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return builder.String()
}

func workflowStepLogCorrelators(step map[string]any, topologies []map[string]any) []string {
	values := []string{}
	for _, row := range topologies {
		values = append(values, valueString(row["requestId"]), valueString(row["traceId"]))
		topology := workflowStepTopologyMap(row)
		values = append(values, valueString(topology["requestId"]), valueString(topology["traceId"]))
	}
	summary := mapFromAny(step["summary"])
	values = append(values, valueString(summary["requestId"]))
	result := mapFromAny(step["result"])
	response := mapFromAny(result["response"])
	headers := mapFromAny(response["headers"])
	values = append(values, valueString(headers["Request-Id"]), valueString(headers["Request-ID"]), valueString(headers["request-id"]))
	return compactStrings(values)
}

func workflowStepLogNodes(topologies []map[string]any) []string {
	values := []string{}
	for _, row := range topologies {
		topology := workflowStepTopologyMap(row)
		values = append(values, stringListFromAny(topology["observedNodes"])...)
		for _, raw := range []any{topology["confirmedEdges"], topology["externalExits"], topology["unresolvedExits"]} {
			for _, item := range listFromAny(raw) {
				edge := mapFromAny(item)
				values = append(values, valueString(edge["source"]), valueString(edge["target"]))
			}
		}
	}
	return compactStrings(values)
}

func workflowStepTopologyMap(row map[string]any) map[string]any {
	raw, ok := row["topologyJson"]
	if !ok {
		return map[string]any{}
	}
	if topology := mapFromAny(raw); len(topology) > 0 {
		return topology
	}
	text := valueString(raw)
	if strings.TrimSpace(text) == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func collectRuntimeLogSystems(ctx context.Context, nodes []string, correlators []string) []map[string]any {
	names := runtimeContainerNames(ctx)
	if len(names) == 0 {
		return nil
	}
	out := []map[string]any{}
	seen := map[string]bool{}
	for _, node := range nodes {
		container := runtimeContainerForNode(names, node)
		if container == "" || seen[node] {
			continue
		}
		seen[node] = true
		lines, matched := runtimeContainerLogLines(ctx, container, correlators)
		out = append(out, map[string]any{
			"id":              node,
			"name":            node,
			"container":       container,
			"found":           len(lines) > 0,
			"matchedKeywords": matched,
			"coreLogs":        lines,
			"note":            "No matching local runtime logs for this step.",
		})
	}
	return out
}

func runtimeContainerNames(ctx context.Context) []string {
	out, err := runtimeCommand(ctx, 2*time.Second, "docker", "ps", "--format", "{{.Names}}")
	if err != nil {
		return nil
	}
	return compactStrings(strings.Split(string(out), "\n"))
}

func runtimeContainerForNode(names []string, node string) string {
	candidate := runtimeLogNodeName(node)
	if candidate == "" {
		return ""
	}
	for _, name := range names {
		if name == candidate {
			return name
		}
	}
	for _, name := range names {
		if strings.HasSuffix(name, "-"+candidate) {
			return name
		}
	}
	for _, name := range names {
		if strings.Contains(name, candidate) {
			return name
		}
	}
	return ""
}

func runtimeLogNodeName(node string) string {
	candidate := strings.TrimSpace(node)
	if candidate == "" {
		return ""
	}
	if index := strings.Index(candidate, ":"); index >= 0 {
		candidate = candidate[:index]
	}
	return strings.TrimSpace(candidate)
}

func runtimeContainerLogLines(ctx context.Context, container string, correlators []string) ([]string, []string) {
	out, err := runtimeCommand(ctx, 3*time.Second, "docker", "logs", "--since", "2h", "--tail", "3000", container)
	if err != nil {
		return nil, nil
	}
	lines := []string{}
	matchedSet := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if !runtimeLineMatches(line, correlators, matchedSet) {
			continue
		}
		lines = append(lines, line)
		if len(lines) >= runtimeLogLineLimit {
			break
		}
	}
	matched := make([]string, 0, len(matchedSet))
	for value := range matchedSet {
		matched = append(matched, value)
	}
	sort.Strings(matched)
	return lines, matched
}

func runtimeLineMatches(line string, correlators []string, matched map[string]bool) bool {
	ok := false
	for _, value := range correlators {
		if value == "" || !strings.Contains(line, value) {
			continue
		}
		matched[value] = true
		ok = true
	}
	return ok
}

func runtimeCommand(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return runRuntimeCommand(commandCtx, name, args...)
}

var runRuntimeCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func compactStrings(values []string) []string {
	out := []string{}
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

func listFromAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}
