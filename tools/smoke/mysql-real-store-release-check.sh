#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

raw_dsn="${OTSANDBOX_REAL_MYSQL_STORE_DSN:-${OTSANDBOX_SMOKE_STORE_DSN:-${OTSANDBOX_SMOKE_STORE:-}}}"
if [[ -z "$raw_dsn" ]]; then
  echo "Set OTSANDBOX_REAL_MYSQL_STORE_DSN, OTSANDBOX_SMOKE_STORE_DSN, or OTSANDBOX_SMOKE_STORE to a dedicated mysql:// Open Test Sandbox smoke Store DSN." >&2
  echo "Example: OTSANDBOX_REAL_MYSQL_STORE_DSN='mysql://user:pass@host:3306/otsandbox_smoke?tls=false' npm run release-check:mysql-real" >&2
  exit 1
fi

dsn_info=$(RAW_DSN="$raw_dsn" node <<'NODE'
const raw = String(process.env.RAW_DSN || "").trim();
try {
  const parsed = new URL(raw);
  const database = decodeURIComponent(parsed.pathname.replace(/^\/+/, ""));
  const masked = new URL(raw);
  if (masked.password) masked.password = "xxxxx";
  const safeName = /(^|[_-])otsandbox([_-]|$)|(^|[_-])(smoke|test|ci)([_-]|$)/i.test(database);
  console.log(JSON.stringify({
    ok: true,
    scheme: parsed.protocol.replace(/:$/, "").toLowerCase(),
    database,
    safeName,
    masked: masked.toString(),
  }));
} catch (error) {
  console.log(JSON.stringify({ ok: false, error: error.message }));
}
NODE
)

scheme=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.scheme || '')" "$dsn_info")
database=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.database || '')" "$dsn_info")
safe_name=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.safeName))" "$dsn_info")
masked=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.masked || '')" "$dsn_info")
parse_ok=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.ok))" "$dsn_info")

if [[ "$parse_ok" != "true" || "$scheme" != "mysql" || -z "$database" ]]; then
  echo "OTSANDBOX_REAL_MYSQL_STORE_DSN must be a mysql:// DSN with a database path." >&2
  exit 1
fi

if [[ "$safe_name" != "true" ]]; then
  echo "Refusing to run release-check against database '$database'." >&2
  echo "Use a dedicated sandbox/smoke/test/ci database name, not a business schema." >&2
  exit 1
fi

echo "Running MySQL release-check against dedicated Store: $masked" >&2
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
