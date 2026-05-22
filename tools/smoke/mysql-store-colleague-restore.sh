#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

STORE_NAME=""
STORE_URL=""
ENVIRONMENT_ID=""
WORKSPACE=""
SERVER_URL=""
BASE_URL=""
OUTPUT_PREFIX=".runtime/mysql-store-colleague-restore-$(date +%Y%m%dT%H%M%S)"
MIN_COMPONENTS=1
MIN_DEPENDENCIES=0
MIN_ASSETS=1
MIN_INLINE_ASSET_BYTES=1
MIN_ACCEPTANCE_STEPS=1
USE_EXISTING_CONTAINERS=0
PULL_IMAGES=0
CLEAN_DOCKER_STATE=0
CLEAN_DOCKER_IMAGES=0
ALLOW_DESTRUCTIVE_DOCKER_CLEANUP=0
VERIFY_CONTROL_PLANE_URL=""
ACCEPTANCE_TIMEOUT_SECONDS=240
HEALTH_TIMEOUT_SECONDS=120
OTSANDBOX_BIN="${OTSANDBOX_BIN:-}"

usage() {
  cat <<'EOF'
Usage:
  mysql-store-colleague-restore.sh \
    --store TEAM_MYSQL_STORE \
    [--store-url MYSQL_DSN] \
    --environment ENV_ID \
    --workspace PATH \
    --server-url URL \
    [--base-url URL] \
    [--verify-control-plane-url URL] \
    [--output-prefix PREFIX] \
    [--min-components N] [--min-dependencies N] [--min-assets N] \
    [--min-inline-asset-bytes N] [--min-acceptance-steps N] \
    [--use-existing-containers] [--pull] \
    [--clean-docker-state] [--clean-docker-images] \
    [--allow-destructive-docker-cleanup]

This is the colleague-side restore entrypoint. It does not copy local Store
data into the team Store. It verifies that the named MySQL Store is reachable,
schema-ready, active locally, visible to the running control plane, and contains
the verified environment before running environment restore and its acceptance
workflow.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --store)
      STORE_NAME="${2:-}"
      shift 2
      ;;
    --store-url)
      STORE_URL="${2:-}"
      shift 2
      ;;
    --environment)
      ENVIRONMENT_ID="${2:-}"
      shift 2
      ;;
    --workspace)
      WORKSPACE="${2:-}"
      shift 2
      ;;
    --server-url)
      SERVER_URL="${2:-}"
      shift 2
      ;;
    --base-url)
      BASE_URL="${2:-}"
      shift 2
      ;;
    --verify-control-plane-url)
      VERIFY_CONTROL_PLANE_URL="${2:-}"
      shift 2
      ;;
    --output-prefix)
      OUTPUT_PREFIX="${2:-}"
      shift 2
      ;;
    --min-components)
      MIN_COMPONENTS="${2:-}"
      shift 2
      ;;
    --min-dependencies)
      MIN_DEPENDENCIES="${2:-}"
      shift 2
      ;;
    --min-assets)
      MIN_ASSETS="${2:-}"
      shift 2
      ;;
    --min-inline-asset-bytes)
      MIN_INLINE_ASSET_BYTES="${2:-}"
      shift 2
      ;;
    --min-acceptance-steps)
      MIN_ACCEPTANCE_STEPS="${2:-}"
      shift 2
      ;;
    --use-existing-containers)
      USE_EXISTING_CONTAINERS=1
      shift
      ;;
    --pull)
      PULL_IMAGES=1
      shift
      ;;
    --clean-docker-state)
      CLEAN_DOCKER_STATE=1
      shift
      ;;
    --clean-docker-images)
      CLEAN_DOCKER_IMAGES=1
      shift
      ;;
    --allow-destructive-docker-cleanup)
      ALLOW_DESTRUCTIVE_DOCKER_CLEANUP=1
      shift
      ;;
    --acceptance-timeout-seconds)
      ACCEPTANCE_TIMEOUT_SECONDS="${2:-}"
      shift 2
      ;;
    --health-timeout-seconds)
      HEALTH_TIMEOUT_SECONDS="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

require_value() {
  local name="$1"
  local value="$2"
  if [[ -z "$value" ]]; then
    echo "missing required argument: $name" >&2
    usage >&2
    exit 2
  fi
}

require_non_negative_int() {
  local name="$1"
  local value="$2"
  if ! [[ "$value" =~ ^[0-9]+$ ]]; then
    echo "$name must be a non-negative integer" >&2
    exit 2
  fi
}

require_value "--store" "$STORE_NAME"
require_value "--environment" "$ENVIRONMENT_ID"
require_value "--workspace" "$WORKSPACE"
require_value "--server-url" "$SERVER_URL"
require_non_negative_int "--min-components" "$MIN_COMPONENTS"
require_non_negative_int "--min-dependencies" "$MIN_DEPENDENCIES"
require_non_negative_int "--min-assets" "$MIN_ASSETS"
require_non_negative_int "--min-inline-asset-bytes" "$MIN_INLINE_ASSET_BYTES"
require_non_negative_int "--min-acceptance-steps" "$MIN_ACCEPTANCE_STEPS"
require_non_negative_int "--acceptance-timeout-seconds" "$ACCEPTANCE_TIMEOUT_SECONDS"
require_non_negative_int "--health-timeout-seconds" "$HEALTH_TIMEOUT_SECONDS"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required for colleague restore assertions" >&2
  exit 2
fi

run_otsandbox() {
  if [[ -n "$OTSANDBOX_BIN" ]]; then
    "$OTSANDBOX_BIN" "$@"
  elif [[ -x ".runtime/otsandbox-dev" ]]; then
    .runtime/otsandbox-dev "$@"
  elif [[ -x "./bin/otsandbox.sh" ]]; then
    ./bin/otsandbox.sh "$@"
  else
    go run ./cmd/otsandbox "$@"
  fi
}

mkdir -p "$(dirname "$OUTPUT_PREFIX")"

STATUS_REPORT="${OUTPUT_PREFIX}-store-status.json"
STATUS_ASSERTION="${OUTPUT_PREFIX}-store-status-assertion.json"
ACTIVE_STORE_REPORT="${OUTPUT_PREFIX}-store-use.txt"
ACTIVE_STORE_CURRENT_REPORT="${OUTPUT_PREFIX}-store-current.json"
ACTIVE_STORE_ASSERTION="${OUTPUT_PREFIX}-store-current-assertion.json"
CONTROL_PLANE_CURRENT_REPORT="${OUTPUT_PREFIX}-control-plane-store-current.json"
CONTROL_PLANE_ASSERTION="${OUTPUT_PREFIX}-control-plane-store-current-assertion.json"
INSPECT_REPORT="${OUTPUT_PREFIX}-env-inspect.json"
INSPECT_ASSERTION="${OUTPUT_PREFIX}-env-inspect-assertion.json"
RESTORE_REPORT="${OUTPUT_PREFIX}-restore.json"
RESTORE_ASSERTION="${OUTPUT_PREFIX}-restore-assertion.json"
PUBLISH_REPORT="${OUTPUT_PREFIX}-publish-verified.json"
PUBLISH_ASSERTION="${OUTPUT_PREFIX}-publish-verified-assertion.json"

if [[ -n "$STORE_URL" ]]; then
  run_otsandbox store config set "$STORE_NAME" --url "$STORE_URL" \
    > "${OUTPUT_PREFIX}-store-config-set.txt"
fi

tools/smoke/mysql-store-preflight.sh \
  --store "$STORE_NAME" \
  --output-prefix "$OUTPUT_PREFIX"

run_otsandbox store status --store "$STORE_NAME" --json > "$STATUS_REPORT"
jq -e '
  .ok == true
  and .backend == "mysql"
  and ((.pending // 0) == 0)
' "$STATUS_REPORT" > "$STATUS_ASSERTION"

run_otsandbox store use "$STORE_NAME" > "$ACTIVE_STORE_REPORT"
run_otsandbox store current --json > "$ACTIVE_STORE_CURRENT_REPORT"
jq -e '
  .ok == true
  and .name == $storeName
  and .backend == "mysql"
' --arg storeName "$STORE_NAME" \
  "$ACTIVE_STORE_CURRENT_REPORT" > "$ACTIVE_STORE_ASSERTION"

if [[ -z "$VERIFY_CONTROL_PLANE_URL" ]]; then
  VERIFY_CONTROL_PLANE_URL="$SERVER_URL"
fi
VERIFY_CONTROL_PLANE_URL="${VERIFY_CONTROL_PLANE_URL%/}"
CONTROL_PLANE_CURRENT_URL="${VERIFY_CONTROL_PLANE_URL}/api/store/current"
CONTROL_PLANE_CURRENT_URL="$CONTROL_PLANE_CURRENT_URL" python3 - <<'PY' > "$CONTROL_PLANE_CURRENT_REPORT"
import os
import sys
import urllib.request

url = os.environ["CONTROL_PLANE_CURRENT_URL"]
try:
    with urllib.request.urlopen(url, timeout=10) as response:
        sys.stdout.buffer.write(response.read())
except Exception as exc:
    raise SystemExit(f"control-plane Store current check failed for {url}: {exc}")
PY
jq -e '
  .ok == true
  and .configured == true
  and .name == $storeName
  and .backend == "mysql"
' --arg storeName "$STORE_NAME" \
  "$CONTROL_PLANE_CURRENT_REPORT" > "$CONTROL_PLANE_ASSERTION"

run_otsandbox environment inspect "$ENVIRONMENT_ID" \
  --store "$STORE_NAME" \
  --json > "$INSPECT_REPORT"
jq -e '
  .ok == true
  and .environment.id == $environmentID
  and .environment.status == "verified"
  and .environment.verified == true
  and .environment.evidenceComplete == true
  and .environment.topologyComplete == true
  and .componentGraph.configured == true
  and .componentGraph.ok == true
  and .componentGraph.components >= ($minComponents | tonumber)
  and .componentGraph.dependencies >= ($minDependencies | tonumber)
  and .componentGraph.assets >= ($minAssets | tonumber)
  and .componentGraph.inlineAssetBytes >= ($minInlineAssetBytes | tonumber)
' \
  --arg environmentID "$ENVIRONMENT_ID" \
  --arg minComponents "$MIN_COMPONENTS" \
  --arg minDependencies "$MIN_DEPENDENCIES" \
  --arg minAssets "$MIN_ASSETS" \
  --arg minInlineAssetBytes "$MIN_INLINE_ASSET_BYTES" \
  "$INSPECT_REPORT" > "$INSPECT_ASSERTION"

restore_args=(
  environment restore "$ENVIRONMENT_ID"
  --store "$STORE_NAME"
  --workspace "$WORKSPACE"
  --execute
  --run-workflow
  --server-url "$SERVER_URL"
  --acceptance-timeout-seconds "$ACCEPTANCE_TIMEOUT_SECONDS"
  --health-timeout-seconds "$HEALTH_TIMEOUT_SECONDS"
  --json
)
if [[ "$USE_EXISTING_CONTAINERS" == "1" ]]; then
  restore_args+=(--use-existing-containers)
fi
if [[ "$PULL_IMAGES" == "1" ]]; then
  restore_args+=(--pull)
fi
if [[ "$CLEAN_DOCKER_STATE" == "1" ]]; then
  restore_args+=(--clean-docker-state)
fi
if [[ "$CLEAN_DOCKER_IMAGES" == "1" ]]; then
  restore_args+=(--clean-docker-images)
fi
if [[ "$ALLOW_DESTRUCTIVE_DOCKER_CLEANUP" == "1" ]]; then
  restore_args+=(--allow-destructive-docker-cleanup)
fi
if [[ -n "$BASE_URL" ]]; then
  restore_args+=(--base-url "$BASE_URL")
fi
run_otsandbox "${restore_args[@]}" > "$RESTORE_REPORT"

jq -e '
  .ok == true
  and .environment.summary.lastRestore.ok == true
  and .workflow.ok == true
  and .workflow.action == "run-acceptance-workflow"
  and .workflow.acceptance.ok == true
  and (.workflow.acceptance.expectedSteps >= ($minAcceptanceSteps | tonumber))
  and (.workflow.acceptance.completedSteps == .workflow.acceptance.expectedSteps)
  and (.workflow.acceptance.passedSteps == .workflow.acceptance.expectedSteps)
  and ((.workflow.acceptance.failedSteps // .workflow.counts.failed // 0) == 0)
  and ((.workflow.acceptance.topologyProvider // "") == "skywalking")
' --arg minAcceptanceSteps "$MIN_ACCEPTANCE_STEPS" \
  "$RESTORE_REPORT" > "$RESTORE_ASSERTION"

run_otsandbox environment publish-verified "$ENVIRONMENT_ID" \
  --store "$STORE_NAME" \
  --json > "$PUBLISH_REPORT"
jq -e '
  .ok == true
  and .environment.id == $environmentID
  and .environment.status == "verified"
  and .environment.verified == true
  and .environment.evidenceComplete == true
  and .environment.topologyComplete == true
' --arg environmentID "$ENVIRONMENT_ID" \
  "$PUBLISH_REPORT" > "$PUBLISH_ASSERTION"

{
  echo "# MySQL Store colleague restore complete"
  echo
  echo "- store: \`$STORE_NAME\`"
  echo "- environment: \`$ENVIRONMENT_ID\`"
  echo "- workspace: \`$WORKSPACE\`"
  echo "- server_url: \`$SERVER_URL\`"
  echo "- acceptance_min_steps: $MIN_ACCEPTANCE_STEPS"
  echo
  echo "Evidence files:"
  if [[ -n "$STORE_URL" ]]; then
    echo "- ${OUTPUT_PREFIX}-store-config-set.txt"
  fi
  echo "- $STATUS_REPORT"
  echo "- $STATUS_ASSERTION"
  echo "- $ACTIVE_STORE_CURRENT_REPORT"
  echo "- $ACTIVE_STORE_ASSERTION"
  echo "- $CONTROL_PLANE_CURRENT_REPORT"
  echo "- $CONTROL_PLANE_ASSERTION"
  echo "- $INSPECT_REPORT"
  echo "- $INSPECT_ASSERTION"
  echo "- $RESTORE_REPORT"
  echo "- $RESTORE_ASSERTION"
  echo "- $PUBLISH_REPORT"
  echo "- $PUBLISH_ASSERTION"
} | tee "${OUTPUT_PREFIX}-restore-complete.md"
