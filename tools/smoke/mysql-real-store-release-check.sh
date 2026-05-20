#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

raw_dsn="${OTSANDBOX_REAL_MYSQL_STORE_DSN:-${OTSANDBOX_SMOKE_STORE_DSN:-${OTSANDBOX_SMOKE_STORE:-}}}"
if [[ -z "$raw_dsn" ]]; then
  echo "Set OTSANDBOX_REAL_MYSQL_STORE_DSN, OTSANDBOX_SMOKE_STORE_DSN, or OTSANDBOX_SMOKE_STORE to a dedicated mysql:// Open Test Sandbox smoke Store DSN." >&2
  echo "Also set OTSANDBOX_REQUIRE_REAL_SKYWALKING=1, OTS_TRACE_GRAPHQL_URL, and OTS_SMOKE_TRACE_IDS for all 10 workflow steps." >&2
  echo "Example: OTSANDBOX_REAL_MYSQL_STORE_DSN='mysql://user:pass@host:3306/otsandbox_smoke?tls=false' npm run release-check:mysql-real" >&2
  exit 1
fi

dsn_info=$(node tools/smoke/mysql-store-dsn-guard.mjs "$raw_dsn")

scheme=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.scheme || '')" "$dsn_info")
database=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.database || '')" "$dsn_info")
safe_name=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.safeName))" "$dsn_info")
masked=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.masked || '')" "$dsn_info")
parse_ok=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.parseOK))" "$dsn_info")

if [[ "$parse_ok" != "true" || "$scheme" != "mysql" || -z "$database" ]]; then
  echo "OTSANDBOX_REAL_MYSQL_STORE_DSN must be a mysql:// DSN with a database path." >&2
  exit 1
fi

if [[ "$safe_name" != "true" ]]; then
  echo "Refusing to run release-check against database '$database'." >&2
  echo "Use a dedicated sandbox/smoke/test/ci database name, not a business schema." >&2
  exit 1
fi

if [[ "${OTSANDBOX_REQUIRE_REAL_SKYWALKING:-}" != "1" ]]; then
  echo "npm run release-check:mysql-real requires OTSANDBOX_REQUIRE_REAL_SKYWALKING=1." >&2
  echo "Provide OTS_TRACE_GRAPHQL_URL and OTS_SMOKE_TRACE_IDS for all 10 workflow steps." >&2
  exit 1
fi

if [[ -z "${OTS_TRACE_GRAPHQL_URL:-}" ]]; then
  echo "npm run release-check:mysql-real requires OTS_TRACE_GRAPHQL_URL." >&2
  exit 1
fi

trace_graphql_url_ok=$(OTS_TRACE_GRAPHQL_URL="$OTS_TRACE_GRAPHQL_URL" node <<'NODE'
const raw = String(process.env.OTS_TRACE_GRAPHQL_URL || "").trim();
try {
  const parsed = new URL(raw);
  process.stdout.write(parsed.protocol === "http:" || parsed.protocol === "https:" ? "true" : "false");
} catch {
  process.stdout.write("false");
}
NODE
)
if [[ "$trace_graphql_url_ok" != "true" ]]; then
  echo "npm run release-check:mysql-real requires OTS_TRACE_GRAPHQL_URL to be an http/https URL." >&2
  exit 1
fi

if [[ -z "${OTS_SMOKE_TRACE_IDS:-}" ]]; then
  echo "npm run release-check:mysql-real requires OTS_SMOKE_TRACE_IDS for the 10-step workflow." >&2
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
  console.error(`npm run release-check:mysql-real requires OTS_SMOKE_TRACE_IDS for all 10 workflow steps; missing: ${missing.join(" ")}.`);
  process.exit(1);
}
NODE

echo "Running MySQL release-check against dedicated Store: $masked" >&2
echo "Real SkyWalking release mode: required" >&2
export OTSANDBOX_SMOKE_STORE_DSN="$raw_dsn"
export OTSANDBOX_MYSQL_TEST_DSN="${OTSANDBOX_MYSQL_TEST_DSN:-$raw_dsn}"
export OTSANDBOX_MYSQL_TEST_DSN_MODE="${OTSANDBOX_MYSQL_TEST_DSN_MODE:-existing}"
echo "MySQL Store contract mode: $OTSANDBOX_MYSQL_TEST_DSN_MODE" >&2
if [[ "${OTSANDBOX_REAL_MYSQL_RELEASE_DRY_RUN:-}" == "1" ]]; then
  echo "Validated MySQL release-check Store: $masked" >&2
  echo "Would run: npm run release-check" >&2
  exit 0
fi
exec npm run release-check
