#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

PATTERN='scf|融资|放款|还款|scf-chain-sandbox|retail-gateway|scf-loan|scf-gateway|account-channel|sandboxctl'

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
  "profiles"
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

if rg -n -i "$PATTERN" "${existing[@]}"; then
  echo "core contains source-domain terms; move them behind a profile or migration doc" >&2
  exit 1
fi

echo "core scan passed"
