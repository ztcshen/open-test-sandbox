#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

raw_dsn="${AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN:-${AGENT_TESTBENCH_SMOKE_STORE_DSN:-${AGENT_TESTBENCH_SMOKE_STORE:-}}}"
if [[ -z "$raw_dsn" ]]; then
  echo "Set AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN, AGENT_TESTBENCH_SMOKE_STORE_DSN, or AGENT_TESTBENCH_SMOKE_STORE to a dedicated mysql:// AgentTestBench smoke Store DSN." >&2
  echo "Also set AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1, AGENT_TESTBENCH_TRACE_GRAPHQL_URL, AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS, and AGENT_TESTBENCH_SMOKE_TRACE_IDS for every configured workflow step." >&2
  echo "Example: AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run release-check:mysql-real" >&2
  exit 1
fi

dsn_info=$(node tools/smoke/mysql-store-dsn-guard.mjs "$raw_dsn")

scheme=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.scheme || '')" "$dsn_info")
database=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.database || '')" "$dsn_info")
safe_name=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.safeName))" "$dsn_info")
masked=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.masked || '')" "$dsn_info")
parse_ok=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.parseOK))" "$dsn_info")

if [[ "$parse_ok" != "true" || "$scheme" != "mysql" || -z "$database" ]]; then
  echo "AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN must be a mysql:// DSN with a database path." >&2
  exit 1
fi

if [[ "$safe_name" != "true" ]]; then
  echo "Refusing to run release-check against database '$database'." >&2
  echo "Use a dedicated sandbox/smoke/test/ci database name, not a business schema." >&2
  exit 1
fi

if [[ "${AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING:-}" != "1" ]]; then
  echo "npm run release-check:mysql-real requires AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1." >&2
  echo "Provide AGENT_TESTBENCH_TRACE_GRAPHQL_URL, AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS, and AGENT_TESTBENCH_SMOKE_TRACE_IDS for every configured workflow step." >&2
  exit 1
fi

node tools/smoke/skywalking-release-guard.mjs "npm run release-check:mysql-real"

requested_contract_mode="${AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE:-existing}"
if [[ "$requested_contract_mode" != "existing" ]]; then
  echo "npm run release-check:mysql-real requires AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE=existing." >&2
  echo "Use generic release-check with an explicitly isolated local admin database for create-drop contract tests." >&2
  exit 1
fi

echo "Running MySQL release-check against dedicated Store: $masked" >&2
echo "Real SkyWalking release mode: required" >&2
export AGENT_TESTBENCH_SMOKE_STORE_DSN="$raw_dsn"
export AGENT_TESTBENCH_MYSQL_TEST_DSN="${AGENT_TESTBENCH_MYSQL_TEST_DSN:-$raw_dsn}"
export AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE="existing"
echo "MySQL Store contract mode: existing" >&2
if [[ "${AGENT_TESTBENCH_REAL_MYSQL_RELEASE_DRY_RUN:-}" == "1" ]]; then
  echo "Validated MySQL release-check Store: $masked" >&2
  echo "Would run: npm run release-check" >&2
  exit 0
fi
exec npm run release-check
