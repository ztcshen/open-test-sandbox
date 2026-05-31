#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

DENYLIST="tools/guardrails/source-domain-terms.txt"

if [[ -e team-configs ]]; then
  echo "core repo must not contain bundled team configuration; keep team data outside this repository" >&2
  exit 1
fi

if [[ ! -f "$DENYLIST" ]]; then
  echo "missing source-domain denylist: $DENYLIST" >&2
  exit 1
fi

existing=()
scan_args=("$@")
if [[ ${#scan_args[@]} -gt 0 ]]; then
  git_files_cmd=(git ls-files --cached --others --exclude-standard -z -- "${scan_args[@]}")
else
  git_files_cmd=(git ls-files --cached --others --exclude-standard -z)
fi

while IFS= read -r -d '' path; do
  case "$path" in
    .git/*|.idea/*|.runtime/*|.scratch/*|.understand-anything/*|node_modules/*)
      continue
      ;;
    docs/progress/*|docs/plans/*)
      continue
      ;;
    package-lock.json|tools/guardrails/source-domain-terms.txt)
      continue
      ;;
  esac
  if [[ -f "$path" ]]; then
    existing+=("$path")
  fi
done < <("${git_files_cmd[@]}")

if [[ ${#existing[@]} -eq 0 ]]; then
  echo "no core paths to scan"
  exit 0
fi

if rg -n -i -f "$DENYLIST" "${existing[@]}"; then
  echo "core contains source-domain terms; move them into private validation data" >&2
  exit 1
fi

echo "core scan passed"
