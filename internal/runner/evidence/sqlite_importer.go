package evidence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"open-test-sandbox/internal/store/sqlite"
)

type SQLiteImportOptions struct {
	SourcePath string
	TargetPath string
	ProfileID  string
}

func ImportLegacyRuntimeSQLite(ctx context.Context, options SQLiteImportOptions) (ImportResult, error) {
	if strings.TrimSpace(options.SourcePath) == "" {
		return ImportResult{}, errors.New("source path is required")
	}
	if strings.TrimSpace(options.TargetPath) == "" {
		return ImportResult{}, errors.New("target path is required")
	}
	if strings.TrimSpace(options.ProfileID) == "" {
		return ImportResult{}, errors.New("profile id is required")
	}
	if _, err := os.Stat(options.SourcePath); err != nil {
		return ImportResult{}, fmt.Errorf("stat source runtime store: %w", err)
	}
	target, err := sqlite.Open(ctx, sqlite.Config{Path: options.TargetPath})
	if err != nil {
		return ImportResult{}, err
	}
	_ = target.Close()

	result, err := countLegacyRuntimeRows(ctx, options.SourcePath)
	if err != nil {
		return ImportResult{}, err
	}

	script := fmt.Sprintf(`
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
ATTACH DATABASE %s AS source_runtime;
BEGIN;

INSERT OR IGNORE INTO runs (id, profile_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at)
SELECT
  'legacy-workflow-' || id,
  %s,
  workflow_id,
  CASE lower(status)
    WHEN 'passed' THEN 'passed'
    WHEN 'failed' THEN 'failed'
    WHEN 'skipped' THEN 'skipped'
    ELSE 'running'
  END,
  '',
  substr(summary_json, 1, 8192),
  created_at,
  '',
  created_at,
  created_at
FROM source_runtime.workflow_runs;

INSERT OR IGNORE INTO runs (id, profile_id, workflow_id, status, evidence_root, summary_json, started_at, finished_at, created_at, updated_at)
SELECT
  run_id,
  %s,
  '',
  CASE lower(status)
    WHEN 'passed' THEN 'passed'
    WHEN 'failed' THEN 'failed'
    WHEN 'skipped' THEN 'skipped'
    ELSE 'running'
  END,
  evidence_path,
  substr(summary_json, 1, 8192),
  created_at,
  created_at,
  created_at,
  created_at
FROM source_runtime.interface_node_case_run
GROUP BY run_id;

INSERT OR IGNORE INTO api_case_runs (id, run_id, case_id, status, request_summary_json, assertion_summary_json, started_at, finished_at, created_at)
SELECT
  'legacy-case-run-' || id,
  run_id,
  case_id,
  CASE lower(status)
    WHEN 'passed' THEN 'passed'
    WHEN 'failed' THEN 'failed'
    WHEN 'skipped' THEN 'skipped'
    ELSE 'running'
  END,
  '{}',
  json_object(
    'nodeId', node_id,
    'failureKind', failure_kind,
    'failureReason', failure_reason,
    'elapsedMs', elapsed_ms,
    'summary', substr(summary_json, 1, 8192)
  ),
  created_at,
  created_at,
  created_at
FROM source_runtime.interface_node_case_run;

INSERT OR IGNORE INTO evidence_records (id, run_id, case_run_id, kind, uri, media_type, sha256, size_bytes, summary, created_at)
SELECT
  'legacy-evidence-' || id,
  run_id,
  'legacy-case-run-' || id,
  'case-run',
  evidence_path,
  'application/json',
  '',
  0,
  failure_reason,
  created_at
FROM source_runtime.interface_node_case_run
WHERE trim(evidence_path) != '';

COMMIT;
DETACH DATABASE source_runtime;
`, sqlLiteral(options.SourcePath), sqlLiteral(options.ProfileID), sqlLiteral(options.ProfileID))

	cmd := exec.CommandContext(ctx, "sqlite3", options.TargetPath, script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return ImportResult{}, fmt.Errorf("import legacy runtime rows: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return result, nil
}

func sqlLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func countLegacyRuntimeRows(ctx context.Context, sourcePath string) (ImportResult, error) {
	statement := `
select
  (select count(*) from workflow_runs) + (select count(distinct run_id) from interface_node_case_run) as run_count,
  (select count(*) from interface_node_case_run) as api_case_run_count,
  (select count(*) from interface_node_case_run where trim(evidence_path) != '') as evidence_count;
`
	var rows []struct {
		RunCount        int `json:"run_count"`
		APICaseRunCount int `json:"api_case_run_count"`
		EvidenceCount   int `json:"evidence_count"`
	}
	if err := legacyQuery(ctx, sourcePath, statement, &rows); err != nil {
		return ImportResult{}, err
	}
	if len(rows) == 0 {
		return ImportResult{}, nil
	}
	return ImportResult{
		RunCount:        rows[0].RunCount,
		APICaseRunCount: rows[0].APICaseRunCount,
		EvidenceCount:   rows[0].EvidenceCount,
	}, nil
}
