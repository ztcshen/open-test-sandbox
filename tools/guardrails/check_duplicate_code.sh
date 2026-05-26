#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

target=${1:-internal/server/controlplane}
threshold=${AGENT_TESTBENCH_DUPLICATION_THRESHOLD:-1.9}
min_lines=${AGENT_TESTBENCH_DUPLICATION_MIN_LINES:-8}
min_tokens=${AGENT_TESTBENCH_DUPLICATION_MIN_TOKENS:-80}
max_lines=${AGENT_TESTBENCH_DUPLICATION_MAX_LINES:-20000}
max_size=${AGENT_TESTBENCH_DUPLICATION_MAX_SIZE:-5mb}

cmd=(
  npx --yes jscpd@4.2.4 "$target"
  --format go
  --pattern "**/*.go"
)
if [[ "${AGENT_TESTBENCH_DUPLICATION_INCLUDE_TESTS:-0}" != "1" ]]; then
  cmd+=(--ignore "**/*_test.go")
fi
cmd+=(
  --min-lines "$min_lines"
  --min-tokens "$min_tokens"
  --max-lines "$max_lines"
  --max-size "$max_size"
  --threshold "$threshold"
  --reporters console
  --noTips
)

exec "${cmd[@]}"
