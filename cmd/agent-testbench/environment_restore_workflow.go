package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"agent-testbench/internal/store"
)

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
	payload := mapFromReportAny(value)
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
