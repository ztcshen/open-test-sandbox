#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

step() {
  printf '\n==> %s\n' "$*"
}

step "checking whitespace"
git diff --check

step "checking generated state is not tracked"
if [[ -d team-configs ]]; then
  echo "root team-configs directory is not allowed in the core repository" >&2
  exit 1
fi

tracked_generated=$(git ls-files \
  '.runtime' \
  'cmd/otsandbox/.runtime' \
  'internal/controlplane/.runtime' \
  'node_modules' \
  'team-configs' \
  'test-results' \
  'coverage' \
  '*.db' \
  '*.sqlite' \
  '*.sqlite3')

if [[ -n "$tracked_generated" ]]; then
  echo "generated or local-only paths are tracked:" >&2
  echo "$tracked_generated" >&2
  exit 1
fi

step "checking source-domain guardrail"
tools/guardrails/check_no_source_domain_core.sh

step "running Go tests"
go test ./... -count=1

step "running generic API case demo"
OTSANDBOX_CLEAN_DEMO_OUTPUT=1 npm run demo:api-case

step "building React workbench"
npm run build:frontend

step "running frontend model tests"
npm run test:frontend

step "running browser smoke tests"
npm run smoke:frontend

step "release check passed"
