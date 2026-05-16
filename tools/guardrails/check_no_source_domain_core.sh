#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

DENYLIST="tools/guardrails/source-domain-terms.txt"

if [[ -e profiles ]]; then
  echo "core repo must not contain a bundled profile directory; keep profile bundles outside this repository" >&2
  exit 1
fi

if [[ ! -f "$DENYLIST" ]]; then
  echo "missing source-domain denylist: $DENYLIST" >&2
  exit 1
fi

existing=()
while IFS= read -r -d '' path; do
  case "$path" in
    .git/*|.idea/*|.runtime/*|node_modules/*)
      continue
      ;;
    package-lock.json|tools/guardrails/source-domain-terms.txt)
      continue
      ;;
  esac
  if [[ -f "$path" ]]; then
    existing+=("$path")
  fi
done < <(git ls-files --cached --others --exclude-standard -z)

if [[ ${#existing[@]} -eq 0 ]]; then
  echo "no core paths to scan"
  exit 0
fi

if rg -n -i -f "$DENYLIST" "${existing[@]}"; then
  echo "core contains source-domain terms; move them behind a profile or config bundle" >&2
  exit 1
fi

echo "core scan passed"
