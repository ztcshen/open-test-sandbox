#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

echo "==> verifying Go module cache"
go mod verify

echo "==> checking Go packages with readonly module metadata"
go list -mod=readonly ./... >/dev/null

if [[ -f package-lock.json ]]; then
  echo "==> validating npm lockfile"
  npm ci --dry-run --ignore-scripts >/dev/null
fi

echo "dependency baseline passed"
