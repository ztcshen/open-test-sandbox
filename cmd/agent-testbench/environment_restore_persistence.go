package main

import (
	"context"
	"strings"
	"time"

	"agent-testbench/internal/store"
)

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
	defer closeCLIStore(runtime)
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
