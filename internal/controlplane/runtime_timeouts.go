package controlplane

import (
	"context"
	"errors"
	"fmt"

	"open-test-sandbox/internal/store"
)

type runtimeTimeoutEvaluation struct {
	TimeoutMs    int
	ElapsedMs    int64
	Exceeded     bool
	ExceededByMs int64
	Reason       string
}

func evaluateRuntimeTimeout(elapsedMs int64, timeoutMs int) runtimeTimeoutEvaluation {
	out := runtimeTimeoutEvaluation{TimeoutMs: timeoutMs, ElapsedMs: elapsedMs}
	if timeoutMs <= 0 || elapsedMs <= int64(timeoutMs) {
		return out
	}
	out.Exceeded = true
	out.ExceededByMs = elapsedMs - int64(timeoutMs)
	out.Reason = fmt.Sprintf("elapsed %d ms exceeded timeout %d ms", elapsedMs, timeoutMs)
	return out
}

func workflowTimeoutConfigFromStore(ctx context.Context, runtime store.Store, workflowID string) (map[string]int, int, error) {
	if runtime == nil {
		return map[string]int{}, 0, nil
	}
	catalog, err := runtime.GetProfileCatalog(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return map[string]int{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	return workflowTimeoutConfigFromCatalog(catalog, workflowID), workflowOverallTimeoutMs(catalog, workflowID), nil
}

func workflowTimeoutConfigFromCatalog(catalog store.ProfileCatalog, workflowID string) map[string]int {
	nodeTimeouts := interfaceNodeTimeoutsByID(catalog)
	baseTimeoutMs := 0
	for _, workflow := range catalog.Workflows {
		if workflow.ID == workflowID {
			baseTimeoutMs = workflow.BaseStepTimeoutMs
			break
		}
	}
	out := map[string]int{}
	for _, binding := range catalog.WorkflowBindings {
		if binding.WorkflowID != workflowID || binding.StepID == "" {
			continue
		}
		timeoutMs := nodeTimeouts[binding.NodeID]
		if timeoutMs <= 0 {
			timeoutMs = baseTimeoutMs
		}
		if timeoutMs > 0 {
			out[binding.StepID] = timeoutMs
		}
	}
	return out
}

func workflowOverallTimeoutMs(catalog store.ProfileCatalog, workflowID string) int {
	workflowBaseMs := 0
	timeoutOffsetMs := 0
	for _, workflow := range catalog.Workflows {
		if workflow.ID == workflowID {
			workflowBaseMs = workflow.BaseStepTimeoutMs
			timeoutOffsetMs = workflow.TimeoutOffsetMs
			break
		}
	}
	if workflowBaseMs <= 0 && timeoutOffsetMs <= 0 && len(catalog.WorkflowBindings) == 0 {
		return 0
	}
	nodeTimeouts := interfaceNodeTimeoutsByID(catalog)
	total := timeoutOffsetMs
	for _, binding := range catalog.WorkflowBindings {
		if binding.WorkflowID != workflowID {
			continue
		}
		timeoutMs := nodeTimeouts[binding.NodeID]
		if timeoutMs <= 0 {
			timeoutMs = workflowBaseMs
		}
		total += timeoutMs
	}
	return total
}

func interfaceNodeTimeoutsByID(catalog store.ProfileCatalog) map[string]int {
	out := map[string]int{}
	for _, node := range catalog.InterfaceNodes {
		if node.ID != "" && node.TimeoutMs > 0 {
			out[node.ID] = node.TimeoutMs
		}
	}
	return out
}

func interfaceCaseTimeoutsByID(catalog store.ProfileCatalog) map[string]int {
	nodeTimeouts := interfaceNodeTimeoutsByID(catalog)
	out := map[string]int{}
	for _, item := range catalog.APICases {
		if item.ID == "" {
			continue
		}
		if timeoutMs := nodeTimeouts[item.NodeID]; timeoutMs > 0 {
			out[item.ID] = timeoutMs
		}
	}
	return out
}

func applyTimeoutFailure(payload map[string]any, evaluation runtimeTimeoutEvaluation) {
	if !evaluation.Exceeded {
		if evaluation.TimeoutMs > 0 {
			payload["timeoutMs"] = evaluation.TimeoutMs
		}
		return
	}
	payload["status"] = store.StatusFailed
	payload["ok"] = false
	payload["stepOk"] = false
	payload["timeoutMs"] = evaluation.TimeoutMs
	payload["timeoutExceeded"] = true
	payload["timeoutExceededByMs"] = evaluation.ExceededByMs
	payload["failureKind"] = "timeout"
	payload["failureReason"] = evaluation.Reason
	summary := mapFromAny(payload["summary"])
	if summary == nil {
		summary = map[string]any{}
	}
	summary["failureKind"] = "timeout"
	summary["failureReason"] = evaluation.Reason
	payload["summary"] = summary
	payload["bodyHealth"] = map[string]any{
		"ok":      false,
		"level":   store.StatusFailed,
		"message": evaluation.Reason,
	}
}
