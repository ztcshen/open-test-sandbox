#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

step() {
  printf '\n==> %s\n' "$*"
}

is_postgres_store_dsn() {
  [[ "$1" =~ ^[Pp][Oo][Ss][Tt][Gg][Rr][Ee][Ss]([Qq][Ll])?:// ]]
}

is_mysql_store_dsn() {
  [[ "$1" =~ ^[Mm][Yy][Ss][Qq][Ll]:// ]]
}

is_sqlite_store_dsn() {
  [[ "$1" =~ ^([Ss][Qq][Ll][Ii][Tt][Ee]://|[Ff][Ii][Ll][Ee]:) ]]
}

step "checking whitespace"
git diff --check

step "checking release gate tools"
for tool in rg sqlite3; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "$tool is required for release-check. Install it before running the release gate." >&2
    exit 1
  fi
done

step "checking SQL smoke Store"
if [[ -z "${AGENT_TESTBENCH_SMOKE_STORE_DSN:-${AGENT_TESTBENCH_SMOKE_STORE:-}}" ]]; then
  echo "AGENT_TESTBENCH_SMOKE_STORE_DSN or AGENT_TESTBENCH_SMOKE_STORE is required for release-check." >&2
  echo "SQL Store examples:" >&2
  echo "PostgreSQL: AGENT_TESTBENCH_SMOKE_STORE_DSN='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run release-check" >&2
  echo "MySQL: AGENT_TESTBENCH_SMOKE_STORE='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run release-check" >&2
  echo "SQLite: AGENT_TESTBENCH_SMOKE_STORE='sqlite:///tmp/agent-testbench-smoke.sqlite' npm run release-check" >&2
  exit 1
fi
smoke_store_dsn="${AGENT_TESTBENCH_SMOKE_STORE_DSN:-${AGENT_TESTBENCH_SMOKE_STORE:-}}"
if is_postgres_store_dsn "$smoke_store_dsn"; then
  export AGENT_TESTBENCH_TEST_PG_DSN="${AGENT_TESTBENCH_TEST_PG_DSN:-$smoke_store_dsn}"
elif is_mysql_store_dsn "$smoke_store_dsn"; then
  mysql_dsn_info=$(node tools/smoke/mysql-store-dsn-guard.mjs "$smoke_store_dsn")
  mysql_parse_ok=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.parseOK))" "$mysql_dsn_info")
  mysql_scheme=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.scheme || '')" "$mysql_dsn_info")
  mysql_database=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.database || '')" "$mysql_dsn_info")
  mysql_safe_name=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.safeName))" "$mysql_dsn_info")
  if [[ "$mysql_parse_ok" != "true" || "$mysql_scheme" != "mysql" || -z "$mysql_database" ]]; then
    echo "AGENT_TESTBENCH_SMOKE_STORE_DSN or AGENT_TESTBENCH_SMOKE_STORE must be a mysql:// DSN with a database path." >&2
    exit 1
  fi
  if [[ "$mysql_safe_name" != "true" ]]; then
    echo "Refusing to run release-check against MySQL database '$mysql_database'." >&2
    echo "Use a dedicated sandbox/smoke/test/ci database name, not a business schema." >&2
    exit 1
  fi
  export AGENT_TESTBENCH_MYSQL_TEST_DSN="${AGENT_TESTBENCH_MYSQL_TEST_DSN:-$smoke_store_dsn}"
  export AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE="${AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE:-existing}"
elif is_sqlite_store_dsn "$smoke_store_dsn"; then
  if [[ "${AGENT_TESTBENCH_DISABLE_SQLITE_STORE:-}" == "1" ]]; then
    echo "AGENT_TESTBENCH_DISABLE_SQLITE_STORE cannot be combined with a SQLite release-check Store." >&2
    exit 1
  fi
else
  echo "AGENT_TESTBENCH_SMOKE_STORE_DSN or AGENT_TESTBENCH_SMOKE_STORE must be postgres://, postgresql://, mysql://, sqlite://, or file:." >&2
  exit 1
fi

step "checking SkyWalking smoke provider mode"
if [[ "${AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING:-}" == "1" ]]; then
  node tools/smoke/skywalking-release-guard.mjs "AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1"
  echo "Real SkyWalking validation required; using configured GraphQL endpoint and smoke trace ids." >&2
elif [[ -z "${AGENT_TESTBENCH_TRACE_GRAPHQL_URL:-}" ]]; then
  echo "AGENT_TESTBENCH_TRACE_GRAPHQL_URL is not set; smoke will use the deterministic synthetic SkyWalking GraphQL provider." >&2
  echo "Set AGENT_TESTBENCH_TRACE_GRAPHQL_URL, AGENT_TESTBENCH_SMOKE_TRACE_IDS, and AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1 for final live SkyWalking validation; synthetic smoke is not live topology proof." >&2
fi

step "checking generated state is not tracked"
if [[ -d team-configs ]]; then
  echo "root team-configs directory is not allowed in the core repository" >&2
  exit 1
fi

tracked_generated=$(git ls-files \
  '.runtime' \
  'cmd/agent-testbench/.runtime' \
  'internal/server/controlplane/.runtime' \
  'node_modules' \
  'team-configs' \
  'test-private' \
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
if is_mysql_store_dsn "$smoke_store_dsn"; then
  go test -p 1 ./... -count=1
else
  go test ./... -count=1
fi

step "running generic API case demo"
if is_sqlite_store_dsn "$smoke_store_dsn"; then
  AGENT_TESTBENCH_CLEAN_DEMO_OUTPUT=1 AGENT_TESTBENCH_DEMO_STORE="$smoke_store_dsn" npm run demo:api-case
else
  AGENT_TESTBENCH_CLEAN_DEMO_OUTPUT=1 AGENT_TESTBENCH_DISABLE_SQLITE_STORE=1 AGENT_TESTBENCH_DEMO_STORE="$smoke_store_dsn" npm run demo:api-case
fi

step "building React workbench"
npm run build:frontend

step "running frontend model tests"
npm run test:frontend

step "running smoke harness tests"
node --test tools/examples/*.test.mjs tools/smoke/*.test.mjs

step "running active SQL Store CLI smoke tests"
if is_sqlite_store_dsn "$smoke_store_dsn"; then
  node tools/smoke/cli-active-store-smoke.mjs
else
  npm run smoke:cli:sql-active
fi

if is_mysql_store_dsn "$smoke_store_dsn"; then
  step "running MySQL Store API smoke tests"
  AGENT_TESTBENCH_MYSQL_API_SMOKE_DSN="$smoke_store_dsn" npm run smoke:api:mysql-store
fi

step "running active SQL Store browser smoke tests"
if is_sqlite_store_dsn "$smoke_store_dsn"; then
  node tools/smoke/control-plane-smoke.mjs
else
  npm run smoke:frontend:sql-active
fi

step "release check passed"
