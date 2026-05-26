#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

target=${1:-internal/server/controlplane}
threshold=${AGENT_TESTBENCH_DUPLICATION_THRESHOLD:-1.9}
min_lines=${AGENT_TESTBENCH_DUPLICATION_MIN_LINES:-8}
min_tokens=${AGENT_TESTBENCH_DUPLICATION_MIN_TOKENS:-80}
max_lines=${AGENT_TESTBENCH_DUPLICATION_MAX_LINES:-3000}

exec npx --yes jscpd@4.2.4 "$target" \
  --format go \
  --pattern "**/*.go" \
  --ignore "**/*_test.go" \
  --min-lines "$min_lines" \
  --min-tokens "$min_tokens" \
  --max-lines "$max_lines" \
  --threshold "$threshold" \
  --reporters console \
  --noTips
