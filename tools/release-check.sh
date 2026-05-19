#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

step() {
  printf '\n==> %s\n' "$*"
}

step "checking whitespace"
git diff --check

step "checking PostgreSQL smoke Store"
if [[ -z "${OTSANDBOX_SMOKE_STORE_DSN:-${OTSANDBOX_SMOKE_STORE:-}}" ]]; then
  echo "OTSANDBOX_SMOKE_STORE_DSN is required for release-check." >&2
  echo "Example: OTSANDBOX_SMOKE_STORE_DSN='postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable' npm run release-check" >&2
  exit 1
fi

step "checking SkyWalking smoke provider mode"
if [[ "${OTSANDBOX_REQUIRE_REAL_SKYWALKING:-}" == "1" ]]; then
  if [[ -z "${OTS_TRACE_GRAPHQL_URL:-}" ]]; then
    echo "OTSANDBOX_REQUIRE_REAL_SKYWALKING=1 requires OTS_TRACE_GRAPHQL_URL." >&2
    exit 1
  fi
  if [[ -z "${OTS_SMOKE_TRACE_IDS:-}" ]]; then
    echo "OTSANDBOX_REQUIRE_REAL_SKYWALKING=1 requires OTS_SMOKE_TRACE_IDS for the 10-step workflow." >&2
    exit 1
  fi
  node <<'NODE'
const raw = String(process.env.OTS_SMOKE_TRACE_IDS || "").trim();
const parseTraceIDs = (value) => {
  try {
    const parsed = JSON.parse(value);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return Object.fromEntries(Object.entries(parsed).map(([key, traceID]) => [key, String(traceID).trim()]));
    }
  } catch {
    // Accept comma-separated step=trace mappings when JSON is inconvenient in shell.
  }
  return Object.fromEntries(value.split(",").map((item) => item.split("=").map((part) => part.trim())).filter(([key, traceID]) => key && traceID));
};
const traceIDs = parseTraceIDs(raw);
const missing = Array.from({ length: 10 }, (_, index) => `step-${String(index + 1).padStart(2, "0")}`)
  .filter((stepID) => !traceIDs[stepID]);
if (missing.length > 0) {
  console.error(`OTSANDBOX_REQUIRE_REAL_SKYWALKING=1 requires OTS_SMOKE_TRACE_IDS for all 10 workflow steps; missing: ${missing.join(" ")}.`);
  process.exit(1);
}
NODE
  echo "Real SkyWalking validation required; using configured GraphQL endpoint and smoke trace ids." >&2
elif [[ -z "${OTS_TRACE_GRAPHQL_URL:-}" ]]; then
  echo "OTS_TRACE_GRAPHQL_URL is not set; smoke will use the deterministic synthetic SkyWalking GraphQL provider." >&2
  echo "Set OTS_TRACE_GRAPHQL_URL, OTS_SMOKE_TRACE_IDS, and OTSANDBOX_REQUIRE_REAL_SKYWALKING=1 for final live SkyWalking validation; synthetic smoke is not live topology proof." >&2
fi

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

step "checking Store-first contract guardrail"
tools/guardrails/check_store_first_contracts.sh

step "running Go tests"
go test ./... -count=1

step "running generic API case demo"
OTSANDBOX_CLEAN_DEMO_OUTPUT=1 OTSANDBOX_DISABLE_SQLITE_STORE=1 OTSANDBOX_DEMO_STORE="${OTSANDBOX_SMOKE_STORE_DSN:-${OTSANDBOX_SMOKE_STORE:-}}" npm run demo:api-case

step "building React workbench"
npm run build:frontend

step "running frontend model tests"
npm run test:frontend

step "running smoke harness tests"
node --test tools/examples/*.test.mjs tools/smoke/*.test.mjs

step "running PostgreSQL active Store CLI smoke tests"
npm run smoke:cli:pg-active

step "running PostgreSQL-only browser smoke tests"
npm run smoke:frontend:pg-only

step "release check passed"
