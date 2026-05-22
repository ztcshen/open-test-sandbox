#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec go run "$ROOT_DIR/cmd/agent-testbench" "$@"
