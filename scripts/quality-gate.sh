#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

REPORT_DIR=${QUALITY_GATE_REPORT_DIR:-build/reports/quality-gate}
STRICT=${QUALITY_GATE_STRICT:-false}
SCOPE_FILE=""
targets=()
target_count=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --strict)
      STRICT=true
      shift
      ;;
    --report-only)
      STRICT=false
      shift
      ;;
    --scope-file)
      if [[ $# -lt 2 ]]; then
        echo "--scope-file requires a path" >&2
        exit 2
      fi
      SCOPE_FILE=$2
      shift 2
      ;;
    --report-dir)
      if [[ $# -lt 2 ]]; then
        echo "--report-dir requires a path" >&2
        exit 2
      fi
      REPORT_DIR=$2
      shift 2
      ;;
    --)
      shift
      while [[ $# -gt 0 ]]; do
        targets+=("$1")
        target_count=$((target_count + 1))
        shift
      done
      ;;
    *)
      targets+=("$1")
      target_count=$((target_count + 1))
      shift
      ;;
  esac
done

mkdir -p "$REPORT_DIR/jscpd"

if [[ "$target_count" -eq 0 ]]; then
  jscpd_targets=(.)
else
  jscpd_targets=("${targets[@]}")
fi

jscpd_ignore=(
  "**/vendor/**"
  "**/third_party/**"
  "**/generated/**"
  "**/gen/**"
  "**/mocks/**"
  "**/mock/**"
  "**/testdata/**"
  "**/docs/**"
  "**/migrations/**"
  "**/scripts/**"
  "**/.runtime/**"
  "**/.scratch/**"
  "**/node_modules/**"
  "**/control-plane/static/assets/react/**"
  "**/*.pb.go"
  "**/*.pb.gw.go"
  "**/*.gen.go"
  "**/*_mock.go"
  "**/wire_gen.go"
  "**/swagger/**"
  "**/openapi/**"
)
if [[ "${AGENT_TESTBENCH_DUPLICATION_INCLUDE_TESTS:-0}" != "1" ]]; then
  jscpd_ignore+=("**/*_test.go")
fi

ignore_flags=()
for pattern in "${jscpd_ignore[@]}"; do
  ignore_flags+=(--ignore "$pattern")
done

echo "==> running jscpd duplicate scan"
npx --yes jscpd@4.2.4 "${jscpd_targets[@]}" \
  --format go \
  --pattern "**/*.go" \
  "${ignore_flags[@]}" \
  --min-lines "${AGENT_TESTBENCH_DUPLICATION_MIN_LINES:-8}" \
  --min-tokens "${AGENT_TESTBENCH_DUPLICATION_MIN_TOKENS:-80}" \
  --max-lines "${AGENT_TESTBENCH_DUPLICATION_MAX_LINES:-20000}" \
  --max-size "${AGENT_TESTBENCH_DUPLICATION_MAX_SIZE:-5mb}" \
  --threshold 100 \
  --exitCode 0 \
  --reporters console,json,markdown \
  --output "$REPORT_DIR/jscpd" \
  --noTips

quality_args=(
  --root "$ROOT_DIR"
  --report-dir "$REPORT_DIR"
  --jscpd-json "$REPORT_DIR/jscpd/jscpd-report.json"
)
if [[ "$STRICT" == "true" ]]; then
  quality_args+=(--strict)
fi
if [[ -n "$SCOPE_FILE" ]]; then
  quality_args+=(--scope-file "$SCOPE_FILE")
fi
if [[ "$target_count" -gt 0 ]]; then
  quality_args+=("${targets[@]}")
fi

echo "==> aggregating Go quality gate report"
go run ./tools/qualitygate "${quality_args[@]}"
