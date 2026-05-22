package controlplane

import (
	"context"
	"time"

	"open-test-sandbox/internal/store"
)

const (
	postProcessKindRuntimeLogs   = "runtime_log_collect"
	postProcessKindTraceTopology = "trace_topology_collect"
)

func recordPostProcessTask(ctx context.Context, runtime store.Store, task store.PostProcessTask) {
	if runtime == nil || task.RunID == "" || task.Kind == "" {
		return
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	_, _ = runtime.RecordPostProcessTask(ctx, task)
}
