#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

paths=(
  AGENTS.md
  README.md
  README.zh-CN.md
  cmd/otsandbox/main.go
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

check_pattern 'default sqlite|sqlite by default|默认 SQLite|SQLite is the default|保持 SQLite 默认' \
  "Store-first docs must not describe SQLite as the default active Store."

check_pattern 'store-url[[:space:]][^`"$]*\.runtime/store\.sqlite|--store-url[[:space:]]+\.runtime/store\.sqlite' \
  "Daily workflow examples must use --store NAME_OR_DSN instead of --store-url .runtime/store.sqlite."

check_pattern '^[[:space:]]*npm run release-check[[:space:]]*$' \
  "Release-check examples must show OTSANDBOX_SMOKE_STORE_DSN so the PostgreSQL gate runs."

check_pattern '^[[:space:]]*npm run demo:api-case[[:space:]]*$' \
  "API case demo examples must show OTSANDBOX_DEMO_STORE or active Store setup."

check_pattern 'OTSANDBOX_CLEAN_DEMO_OUTPUT=1 npm run demo:api-case' \
  "Release-check must pass OTSANDBOX_DEMO_STORE and disable SQLite Store for the demo."

check_pattern "topology:[[:space:]]*\\{[[:space:]]*status:[[:space:]]*['\"](partial|complete|unavailable)|\"topology\":[[:space:]]*\\{[[:space:]]*\"status\"[[:space:]]*:[[:space:]]*\"(partial|complete|unavailable)" \
  "SkyWalking topology fixtures must set provider/source before status."

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
