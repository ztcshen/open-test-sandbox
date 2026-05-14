#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

DENYLIST="tools/guardrails/source-domain-terms.txt"

if [[ ! -f "$DENYLIST" ]]; then
  echo "missing source-domain denylist: $DENYLIST" >&2
  exit 1
fi

paths=(
  "README.md"
  "AGENTS.md"
  "CONTEXT.md"
  "package.json"
  "go.mod"
  "bin"
  "cmd"
  "internal"
  "control-plane"
  "profiles/empty"
  "examples"
  "compose"
)

existing=()
for path in "${paths[@]}"; do
  if [[ -e "$path" ]]; then
    existing+=("$path")
  fi
done

if [[ ${#existing[@]} -eq 0 ]]; then
  echo "no core paths to scan"
  exit 0
fi

if rg -n -i -f "$DENYLIST" "${existing[@]}"; then
  echo "core contains source-domain terms; move them behind a profile or config bundle" >&2
  exit 1
fi

echo "core scan passed"
