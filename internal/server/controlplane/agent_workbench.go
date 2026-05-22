package controlplane

import (
	"net/http"
	"sort"
	"strings"

	"open-test-sandbox/internal/domain/profile"
	"open-test-sandbox/internal/store"
)

func handleAgentTestWorkbench(w http.ResponseWriter, r *http.Request, bundle profile.Bundle, runtime store.Store) {
	payload := agentTestEmptyPayload()
	profiles := agentTestProfiles(bundle)
	capabilities := agentTestCapabilities(bundle)
	payload["capabilities"] = capabilities
	payload["profiles"] = profiles
	payload["configAuthoring"] = agentTestConfigAuthoring(bundle.ConfigAuthoring)
	if runtime == nil {
		payload["summary"] = agentTestSummary(nil, capabilities, profiles, bundle.ConfigAuthoring)
		writeJSON(w, payload)
		return
	}
	runs, err := runtime.ListRuns(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	agentRuns := agentTestRuns(runs)
	payload["capabilities"] = capabilities
	payload["profiles"] = profiles
	payload["agentRuns"] = agentRuns
	payload["summary"] = agentTestSummary(agentRuns, capabilities, profiles, bundle.ConfigAuthoring)
	writeJSON(w, payload)
}

func agentTestEmptyPayload() map[string]any {
	return map[string]any{
		"ok": true,
		"summary": map[string]any{
			"capabilityCount":         0,
			"profileCount":            0,
			"runCount":                0,
			"configEventCount":        0,
			"escalationEventCount":    0,
			"acceptanceReportCount":   0,
			"statusCounts":            map[string]int{},
			"failureKinds":            map[string]int{},
			"latestFailureKind":       "no active failure",
			"latestAcceptanceVerdict": "",
			"latestAcceptanceStatus":  "",
		},
		"capabilities":      []map[string]any{},
		"profiles":          []map[string]any{},
		"agentRuns":         []map[string]any{},
		"configAuthoring":   map[string]any{},
		"configEvents":      []map[string]any{},
		"escalationEvents":  []map[string]any{},
		"acceptanceReports": []map[string]any{},
		"warnings":          []string{},
	}
}

func agentTestCapabilities(bundle profile.Bundle) []map[string]any {
	if len(bundle.AgentTestProfiles) == 0 && strings.TrimSpace(bundle.ConfigAuthoring.Role) == "" && bundle.Counts() == (profile.Counts{}) {
		return []map[string]any{}
	}
	capabilities := []map[string]any{
		{
			"id":          "evidence-index",
			"title":       "Evidence Diagnosis Index",
			"status":      "available",
			"description": "Run summaries expose diagnosis, evidence roots, status counts, and next-step hints.",
			"evidence":    []string{"runs.summary_json", "evidence_records"},
		},
		{
			"id":          "profile-workbench",
			"title":       "Profile Workbench",
			"status":      "available",
			"description": "Active profile metadata is available for local-first run review.",
			"evidence":    []string{"profile.json", "profile_index"},
		},
		{
			"id":          "case-evidence",
			"title":       "API Case Evidence",
			"status":      "available",
			"description": "API case runs and evidence records are linked from Store data.",
			"evidence":    []string{"api_case_runs", "evidence_records"},
		},
	}
	if strings.TrimSpace(bundle.ConfigAuthoring.Role) != "" {
		capabilities = append(capabilities, map[string]any{
			"id":          "config-authoring-contract",
			"title":       "Subagent Config Authoring",
			"status":      "available",
			"description": "Active profile declares who may author concrete template configuration and what evidence the handoff must include.",
			"evidence":    []string{"config-authoring.json", "agent-test-profiles.json"},
		})
	}
	return capabilities
}

func agentTestProfiles(bundle profile.Bundle) []map[string]any {
	if len(bundle.AgentTestProfiles) > 0 {
		items := make([]map[string]any, 0, len(bundle.AgentTestProfiles))
		for _, item := range bundle.AgentTestProfiles {
			probeCount := len(item.Probes) + len(item.MySQLProbes)
			items = append(items, map[string]any{
				"id":              item.ID,
				"title":           firstNonEmpty(item.Title, item.ID),
				"stepCount":       len(item.Steps),
				"workflowCount":   countAgentTestSteps(item.Steps, "workflow"),
				"caseCount":       countAgentTestSteps(item.Steps, "case"),
				"probeCount":      probeCount,
				"mysqlProbeCount": len(item.MySQLProbes),
				"requiredConfig":  configList(item.RequiredConfig),
				"evidenceKinds":   evidencePolicyKinds(item.EvidencePolicy),
				"allowedChanges":  configChangeList(item.ConfigPolicy.AllowedChanges),
			})
		}
		return items
	}
	if strings.TrimSpace(bundle.ID) == "" || bundle.Counts() == (profile.Counts{}) {
		return []map[string]any{}
	}
	return []map[string]any{{
		"id":             bundle.ID,
		"title":          firstNonEmpty(bundle.DisplayName, bundle.ID),
		"stepCount":      len(bundle.Workflows),
		"workflowCount":  len(bundle.Workflows),
		"caseCount":      len(bundle.APICases),
		"requiredConfig": []map[string]any{},
		"evidenceKinds":  []string{"runs", "evidence"},
		"allowedChanges": []map[string]any{},
	}}
}

func agentTestConfigAuthoring(authoring profile.ConfigAuthoring) map[string]any {
	if strings.TrimSpace(authoring.Role) == "" {
		return map[string]any{}
	}
	return map[string]any{
		"schemaVersion":               authoring.SchemaVersion,
		"role":                        authoring.Role,
		"summary":                     authoring.Summary,
		"guidePath":                   authoring.GuidePath,
		"allowedWritePaths":           nonNil(authoring.AllowedWritePaths),
		"allowedReadPaths":            nonNil(authoring.AllowedReadPaths),
		"mainAgentResponsibilities":   nonNil(authoring.MainAgentResponsibilities),
		"subagentResponsibilities":    nonNil(authoring.SubagentResponsibilities),
		"handoffRequiredFields":       nonNil(authoring.HandoffRequiredFields),
		"frictionCategories":          nonNil(authoring.FrictionCategories),
		"requiresDedicatedSubagent":   authoring.RequiresDedicatedSubagent,
		"prohibitsMainAgentAuthoring": authoring.ProhibitsMainAgentAuthoring,
	}
}

func countAgentTestSteps(steps []profile.AgentTestStep, stepType string) int {
	count := 0
	for _, step := range steps {
		if strings.EqualFold(step.Type, stepType) {
			count++
		}
	}
	return count
}

func evidencePolicyKinds(policy map[string]bool) []string {
	kinds := make([]string, 0, len(policy))
	for key, enabled := range policy {
		if enabled {
			kinds = append(kinds, key)
		}
	}
	sort.Strings(kinds)
	return kinds
}

func configList(items []profile.RequiredConfig) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"kind":           item.Kind,
			"key":            item.Key,
			"suggestedValue": item.SuggestedValue,
			"reason":         item.Reason,
		})
	}
	return out
}

func configChangeList(items []profile.ConfigChange) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"kind": item.Kind,
			"key":  item.Key,
		})
	}
	return out
}

func agentTestRuns(runs []store.Run) []map[string]any {
	items := make([]map[string]any, 0, len(runs))
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		diagnosis := agentTestDiagnosis(run.SummaryJSON)
		failureKind := agentTestFailureKind(run, diagnosis)
		items = append(items, map[string]any{
			"id":                run.ID,
			"runId":             run.ID,
			"repoPath":          "",
			"resolvedServiceId": run.WorkflowID,
			"workflowId":        run.WorkflowID,
			"ref":               "",
			"commitId":          "",
			"profileId":         run.ProfileID,
			"status":            run.Status,
			"failureKind":       failureKind,
			"evidenceRoot":      run.EvidenceRoot,
			"diagnosis":         diagnosis,
			"blockedReport":     nil,
			"startedAt":         run.StartedAt,
			"endedAt":           run.FinishedAt,
			"createdAt":         run.CreatedAt,
		})
	}
	return items
}

func agentTestDiagnosis(summaryJSON string) map[string]any {
	summary := jsonObject(summaryJSON)
	if diagnosis, ok := summary["diagnosisIndex"].(map[string]any); ok {
		return diagnosis
	}
	if nested, ok := summary["summary"].(map[string]any); ok {
		if diagnosis, ok := nested["diagnosisIndex"].(map[string]any); ok {
			return diagnosis
		}
	}
	return map[string]any{}
}

func agentTestFailureKind(run store.Run, diagnosis map[string]any) string {
	if kind := valueString(diagnosis["failureKind"]); kind != "" {
		return kind
	}
	summary := jsonObject(run.SummaryJSON)
	if kind := firstNonEmpty(valueString(summary["failureKind"]), valueString(summary["failure_kind"])); kind != "" {
		return kind
	}
	if run.Status == store.StatusFailed {
		return store.StatusFailed
	}
	return ""
}

func agentTestSummary(runs []map[string]any, capabilities []map[string]any, profiles []map[string]any, authoring profile.ConfigAuthoring) map[string]any {
	statusCounts := map[string]int{}
	failureKinds := map[string]int{}
	for _, run := range runs {
		statusCounts[firstNonEmpty(valueString(run["status"]), "unknown")]++
		if kind := valueString(run["failureKind"]); kind != "" {
			failureKinds[kind]++
		}
	}
	latestFailureKind := "no active failure"
	if len(runs) > 0 {
		if kind := valueString(runs[0]["failureKind"]); kind != "" {
			latestFailureKind = kind
		}
	}
	return map[string]any{
		"capabilityCount":         len(capabilities),
		"profileCount":            len(profiles),
		"runCount":                len(runs),
		"authoringContractCount":  boolCount(strings.TrimSpace(authoring.Role) != ""),
		"configEventCount":        0,
		"escalationEventCount":    0,
		"acceptanceReportCount":   0,
		"statusCounts":            statusCounts,
		"failureKinds":            failureKinds,
		"latestFailureKind":       latestFailureKind,
		"latestAcceptanceVerdict": "",
		"latestAcceptanceStatus":  "",
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
