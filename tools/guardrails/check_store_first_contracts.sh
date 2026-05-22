#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

paths=(
  AGENTS.md
  CONTRIBUTING.md
  README.md
  README.zh-CN.md
  SECURITY.md
  cmd/agent-testbench/main.go
  control-plane/frontend/src
  docs
  package.json
  tools/examples
  tools/release-check.sh
  tools/smoke
)

violations=0

check_pattern() {
  local pattern=$1
  local message=$2
  local matches
  matches=$(rg -n -i "$pattern" "${paths[@]}" || true)
  if [[ -n "$matches" ]]; then
    echo "$message" >&2
    echo "$matches" >&2
    violations=1
  fi
}

check_current_docs_pattern() {
  local pattern=$1
  local message=$2
  local matches
  matches=$(rg -n -i "$pattern" "${paths[@]}" --glob '!docs/progress/**' --glob '!docs/plans/**' || true)
  if [[ -n "$matches" ]]; then
    echo "$message" >&2
    echo "$matches" >&2
    violations=1
  fi
}

check_pattern 'default sqlite|sqlite by default|默认 SQLite|SQLite is the default|保持 SQLite 默认' \
  "Store-first docs must not describe SQLite as the default active Store."

check_pattern 'store-url[[:space:]][^`"$]*\.runtime/store\.sqlite|--store-url[[:space:]]+\.runtime/store\.sqlite' \
  "Daily workflow examples must use --store NAME_OR_DSN instead of --store-url .runtime/store.sqlite."

check_pattern '^[[:space:]]*npm run release-check[[:space:]]*$' \
  "Release-check examples must show AGENT_TESTBENCH_SMOKE_STORE_DSN so the SQL Store gate runs."

check_pattern 'release gate:[[:space:]]*`npm run release-check`|发布门禁：`npm run release-check`' \
  "Release gate shorthand must mention AGENT_TESTBENCH_SMOKE_STORE_DSN."

check_current_docs_pattern 'PostgreSQL Store is the active source of truth|sandbox'\''s own PostgreSQL Store/control-plane database|PostgreSQL is the default product Store|PostgreSQL remains the default|PostgreSQL remains the default upstream|default product Store backend|PostgreSQL by default|PostgreSQL is default|PostgreSQL is the default Store backend|MySQL is supported for teams|MySQL Store can be used for the same smoke shape|PostgreSQL 是默认|PostgreSQL 仍是默认后端|默认产品 Store|默认 Store 后端|MySQL 支持团队测试环境|也支持 MySQL Store|也支持以[[:space:]]*MySQL' \
  "SQL Store docs must not describe PostgreSQL as the only active source or default product Store."

check_current_docs_pattern '65536|131072|16384|16 KB|64 KB|128 KB' \
  "Current Store docs must not mention superseded small Store payload limits; use the 1 MB-only Store boundary."

check_pattern '^[[:space:]]*npm run demo:api-case[[:space:]]*$' \
  "API case demo examples must show AGENT_TESTBENCH_DEMO_STORE or active Store setup."

check_pattern 'AGENT_TESTBENCH_CLEAN_DEMO_OUTPUT=1 npm run demo:api-case' \
  "Release-check must pass AGENT_TESTBENCH_DEMO_STORE and disable SQLite Store for the demo."

check_pattern "topology:[[:space:]]*\\{[[:space:]]*status:[[:space:]]*['\"](partial|complete|unavailable)|\"topology\":[[:space:]]*\\{[[:space:]]*\"status\"[[:space:]]*:[[:space:]]*\"(partial|complete|unavailable)" \
  "SkyWalking topology fixtures must set provider/source before status."

if ! rg -q -i 'not release evidence|not proof of a live SkyWalking deployment' README.md docs/store-backends.md docs/release-checklist.md; then
  echo "Docs must state that synthetic SkyWalking smoke is not live release proof." >&2
  violations=1
fi

if ! rg -q -i 'unavailable, failed, or skipped' README.md README.zh-CN.md docs/cli-api-contracts.md docs/roadmap.md; then
  echo "Docs must state that missing SkyWalking topology reports unavailable, failed, or skipped status." >&2
  violations=1
fi

if ! rg -q -i 'synthetic smoke is not live topology proof' tools/release-check.sh; then
  echo "release-check must distinguish synthetic smoke from live SkyWalking proof." >&2
  violations=1
fi

if ! rg -q 'AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING' tools/release-check.sh docs/release-checklist.md; then
  echo "release-check must keep the explicit real SkyWalking enforcement mode documented and implemented." >&2
  violations=1
fi

if ! rg -q 'AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1' tools/release-check.sh || ! rg -q 'requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL' tools/smoke/skywalking-release-guard.mjs; then
  echo "release-check real SkyWalking mode must require AGENT_TESTBENCH_TRACE_GRAPHQL_URL before expensive gates run." >&2
  violations=1
fi

if ! rg -q 'AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1' tools/release-check.sh || ! rg -q 'requires AGENT_TESTBENCH_SMOKE_TRACE_IDS' tools/smoke/skywalking-release-guard.mjs; then
  echo "release-check real SkyWalking mode must require AGENT_TESTBENCH_SMOKE_TRACE_IDS for the configured workflow." >&2
  violations=1
fi

if ! rg -q 'every configured workflow step' tools/smoke/skywalking-release-guard.mjs docs/release-checklist.md; then
  echo "release-check real SkyWalking mode must require trace ids for every configured workflow step." >&2
  violations=1
fi

if ! rg -q 'AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1 requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL' tools/smoke/control-plane-smoke.mjs; then
  echo "control-plane smoke harness must reject required real SkyWalking mode without AGENT_TESTBENCH_TRACE_GRAPHQL_URL." >&2
  violations=1
fi

if ! rg -q 'every configured workflow step' tools/smoke/control-plane-smoke.mjs tools/smoke/control-plane-smoke.test.mjs; then
  echo "control-plane smoke harness must require trace ids for every configured workflow step in real SkyWalking mode." >&2
  violations=1
fi

generic_resolver_count=$(rg -n 'resolveStoreReference\(' cmd/agent-testbench/main.go | wc -l | tr -d ' ')
if [[ "$generic_resolver_count" != "4" ]]; then
  echo "Daily command code must not add generic Store resolver calls; use resolveRequiredDailyStoreReference unless the path is Store maintenance, offline review, or migration." >&2
  rg -n 'resolveStoreReference\(' cmd/agent-testbench/main.go >&2 || true
  violations=1
fi

compat_required_resolver_count=$(rg -n 'resolveRequiredStoreReference\(' cmd/agent-testbench/main.go | wc -l | tr -d ' ')
if [[ "$compat_required_resolver_count" != "1" ]]; then
  echo "Only explicit migration/compatibility commands may use resolveRequiredStoreReference in CLI handlers." >&2
  rg -n 'resolveRequiredStoreReference\(' cmd/agent-testbench/main.go >&2 || true
  violations=1
fi

blocked_a="fall"
blocked_b="back"
blocked_word="${blocked_a}${blocked_b}"

repo_files=()
while IFS= read -r -d '' path; do
  case "$path" in
    .git/*|.idea/*|.runtime/*|.scratch/*|node_modules/*)
      continue
      ;;
    control-plane/static/assets/react/*)
      continue
      ;;
  esac
  if [[ -f "$path" ]]; then
    repo_files+=("$path")
  fi
done < <(git ls-files --cached --others --exclude-standard -z)

if [[ ${#repo_files[@]} -gt 0 ]]; then
  blocked_matches=$(rg -n -i "$blocked_word" "${repo_files[@]}" || true)
  if [[ -n "$blocked_matches" ]]; then
    echo "Store-first repo scan found a blocked legacy term." >&2
    echo "$blocked_matches" >&2
    violations=1
  fi
fi

if [[ "$violations" -ne 0 ]]; then
  exit 1
fi

echo "Store-first contract scan passed"
