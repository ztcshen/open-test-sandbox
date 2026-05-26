#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

target=${1:-.}
report_dir=${QUALITY_GATE_REPORT_DIR:-build/reports/quality-gate/duplicates}
threshold=${AGENT_TESTBENCH_DUPLICATION_THRESHOLD:-8}
min_lines=${AGENT_TESTBENCH_DUPLICATION_MIN_LINES:-8}
min_tokens=${AGENT_TESTBENCH_DUPLICATION_MIN_TOKENS:-80}
max_lines=${AGENT_TESTBENCH_DUPLICATION_MAX_LINES:-20000}
max_size=${AGENT_TESTBENCH_DUPLICATION_MAX_SIZE:-5mb}
mkdir -p "$report_dir"

ignore_flags=(
  --ignore "**/vendor/**"
  --ignore "**/third_party/**"
  --ignore "**/generated/**"
  --ignore "**/gen/**"
  --ignore "**/mocks/**"
  --ignore "**/mock/**"
  --ignore "**/testdata/**"
  --ignore "**/docs/**"
  --ignore "**/migrations/**"
  --ignore "**/scripts/**"
  --ignore "**/.runtime/**"
  --ignore "**/.scratch/**"
  --ignore "**/node_modules/**"
  --ignore "**/control-plane/static/assets/react/**"
  --ignore "**/*.pb.go"
  --ignore "**/*.pb.gw.go"
  --ignore "**/*.gen.go"
  --ignore "**/*_mock.go"
  --ignore "**/wire_gen.go"
  --ignore "**/swagger/**"
  --ignore "**/openapi/**"
)
if [[ "${AGENT_TESTBENCH_DUPLICATION_INCLUDE_TESTS:-0}" != "1" ]]; then
  ignore_flags+=(--ignore "**/*_test.go")
fi

cmd=(
  npx --yes jscpd@4.2.4 "$target"
  --format go
  --pattern "**/*.go"
  "${ignore_flags[@]}"
)
cmd+=(
  --min-lines "$min_lines"
  --min-tokens "$min_tokens"
  --max-lines "$max_lines"
  --max-size "$max_size"
  --threshold "$threshold"
  --reporters console,json,markdown
  --output "$report_dir"
  --noTips
)

exec "${cmd[@]}"
