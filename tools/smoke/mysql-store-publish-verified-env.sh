#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

SOURCE_STORE=""
TARGET_STORE=""
ENVIRONMENT_ID=""
WORKFLOW_ID=""
OUTPUT_PREFIX=".runtime/mysql-store-publish-$(date +%Y%m%dT%H%M%S)"
MIN_COMPONENTS=1
MIN_DEPENDENCIES=0
MIN_ASSETS=1
MIN_INLINE_ASSET_BYTES=1
RESTORE=0
WORKSPACE=""
SERVER_URL=""
BASE_URL=""
VERIFY_CONTROL_PLANE_URL=""
USE_EXISTING_CONTAINERS=0
ACCEPTANCE_TIMEOUT_SECONDS=240
HEALTH_TIMEOUT_SECONDS=120
STARTUP_FILES_TEXT=""
OTSANDBOX_BIN="${OTSANDBOX_BIN:-}"

usage() {
  cat <<'EOF'
Usage:
  mysql-store-publish-verified-env.sh \
    --from SOURCE_STORE \
    --to TARGET_MYSQL_STORE \
    --environment ENV_ID \
    --workflow WORKFLOW_ID \
    [--output-prefix PREFIX] \
    [--min-components N] [--min-dependencies N] [--min-assets N] \
    [--min-inline-asset-bytes N] \
    [--startup-file TARGET=SOURCE_FILE]... \
    [--restore --workspace PATH --server-url URL [--base-url URL]] \
    [--verify-control-plane-url URL] \
    [--use-existing-containers]

This is the one-command promotion path for a verified environment. It:
  1. proves the source Store already contains the verified environment and
     component graph;
  2. requires a real MySQL handshake through mysql-store-preflight.sh;
  3. provisions and upgrades the target Store through otsandbox CLI;
  4. copies verified restore metadata from the source Store;
  5. reads the copied environment back from the target Store;
  6. switches the local active Store to the target Store;
  7. optionally verifies a running control plane reads the target Store;
  8. optionally runs environment restore and the acceptance workflow.

The script stops before any remote Store mutation if the source Store is not
ready or the MySQL handshake fails.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --from)
      SOURCE_STORE="${2:-}"
      shift 2
      ;;
    --to)
      TARGET_STORE="${2:-}"
      shift 2
      ;;
    --environment)
      ENVIRONMENT_ID="${2:-}"
      shift 2
      ;;
    --workflow)
      WORKFLOW_ID="${2:-}"
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
    --startup-file)
      STARTUP_FILES_TEXT+="${2:-}"$'\n'
      shift 2
      ;;
    --restore)
      RESTORE=1
      shift
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
    --use-existing-containers)
      USE_EXISTING_CONTAINERS=1
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

require_value "--from" "$SOURCE_STORE"
require_value "--to" "$TARGET_STORE"
require_value "--environment" "$ENVIRONMENT_ID"
require_value "--workflow" "$WORKFLOW_ID"
require_non_negative_int "--min-components" "$MIN_COMPONENTS"
require_non_negative_int "--min-dependencies" "$MIN_DEPENDENCIES"
require_non_negative_int "--min-assets" "$MIN_ASSETS"
require_non_negative_int "--min-inline-asset-bytes" "$MIN_INLINE_ASSET_BYTES"
require_non_negative_int "--acceptance-timeout-seconds" "$ACCEPTANCE_TIMEOUT_SECONDS"
require_non_negative_int "--health-timeout-seconds" "$HEALTH_TIMEOUT_SECONDS"

if [[ "$RESTORE" == "1" ]]; then
  require_value "--workspace" "$WORKSPACE"
  require_value "--server-url" "$SERVER_URL"
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required for Store promotion assertions" >&2
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

COPY_REPORT="${OUTPUT_PREFIX}-store-copy.json"
COPY_ASSERTION="${OUTPUT_PREFIX}-store-copy-assertion.json"
SOURCE_INSPECT_REPORT="${OUTPUT_PREFIX}-source-env-inspect.json"
SOURCE_INSPECT_ASSERTION="${OUTPUT_PREFIX}-source-env-inspect-assertion.json"
INSPECT_REPORT="${OUTPUT_PREFIX}-env-inspect.json"
INSPECT_ASSERTION="${OUTPUT_PREFIX}-env-inspect-assertion.json"
ACTIVE_STORE_REPORT="${OUTPUT_PREFIX}-store-use.txt"
ACTIVE_STORE_CURRENT_REPORT="${OUTPUT_PREFIX}-store-current.json"
ACTIVE_STORE_ASSERTION="${OUTPUT_PREFIX}-store-current-assertion.json"
CONTROL_PLANE_CURRENT_REPORT="${OUTPUT_PREFIX}-control-plane-store-current.json"
CONTROL_PLANE_ASSERTION="${OUTPUT_PREFIX}-control-plane-store-current-assertion.json"
STATUS_AFTER_UPGRADE_REPORT="${OUTPUT_PREFIX}-store-status-after-upgrade.json"
STATUS_AFTER_UPGRADE_ASSERTION="${OUTPUT_PREFIX}-store-status-after-upgrade-assertion.json"
RESTORE_REPORT="${OUTPUT_PREFIX}-restore.json"

run_otsandbox environment inspect "$ENVIRONMENT_ID" \
  --store "$SOURCE_STORE" \
  --json > "$SOURCE_INSPECT_REPORT"

jq -e '
  .ok == true
  and .environment.id == $environmentID
  and .environment.verificationWorkflowId == $workflowID
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
  --arg workflowID "$WORKFLOW_ID" \
  --arg minComponents "$MIN_COMPONENTS" \
  --arg minDependencies "$MIN_DEPENDENCIES" \
  --arg minAssets "$MIN_ASSETS" \
  --arg minInlineAssetBytes "$MIN_INLINE_ASSET_BYTES" \
  "$SOURCE_INSPECT_REPORT" > "$SOURCE_INSPECT_ASSERTION"

tools/smoke/mysql-store-preflight.sh \
  --store "$TARGET_STORE" \
  --output-prefix "$OUTPUT_PREFIX"

run_otsandbox store provision --store "$TARGET_STORE" \
  --json > "${OUTPUT_PREFIX}-store-provision.json"
run_otsandbox store status --store "$TARGET_STORE" \
  --json > "${OUTPUT_PREFIX}-store-status-before-copy.json"
run_otsandbox store upgrade --store "$TARGET_STORE"
run_otsandbox store status --store "$TARGET_STORE" \
  --json > "$STATUS_AFTER_UPGRADE_REPORT"

jq -e '
  .ok == true
  and .backend == "mysql"
  and ((.pending // 0) == 0)
' "$STATUS_AFTER_UPGRADE_REPORT" > "$STATUS_AFTER_UPGRADE_ASSERTION"

copy_args=(
  store copy
  --from "$SOURCE_STORE"
  --to "$TARGET_STORE"
  --require-environment "$ENVIRONMENT_ID"
  --require-verification-workflow "$WORKFLOW_ID"
  --require-verified-environment
  --json
)
if [[ "$MIN_COMPONENTS" -gt 0 ]]; then
  copy_args+=(--require-min-components "$MIN_COMPONENTS")
fi
if [[ "$MIN_DEPENDENCIES" -gt 0 ]]; then
  copy_args+=(--require-min-dependencies "$MIN_DEPENDENCIES")
fi
if [[ "$MIN_ASSETS" -gt 0 ]]; then
  copy_args+=(--require-min-assets "$MIN_ASSETS")
fi
if [[ "$MIN_INLINE_ASSET_BYTES" -gt 0 ]]; then
  copy_args+=(--require-inline-asset-bytes "$MIN_INLINE_ASSET_BYTES")
fi
run_otsandbox "${copy_args[@]}" > "$COPY_REPORT"

jq -e '
  .ok == true
  and .profileCatalogs >= 1
  and .profileIndexes >= 1
  and .configVersions >= 1
  and ((.readModels // []) | length) >= 1
  and (.environmentIds | index($environmentID) != null)
  and any(.environmentRefs[]?; .id == $environmentID
    and .verificationWorkflowId == $workflowID
    and .status == "verified"
    and .verified == true
    and .evidenceComplete == true
    and .topologyComplete == true)
  and any(.componentRefs[]?; .environmentId == $environmentID
    and .components >= ($minComponents | tonumber)
    and .dependencies >= ($minDependencies | tonumber)
    and .assets >= ($minAssets | tonumber)
    and .inlineAssetBytes >= ($minInlineAssetBytes | tonumber))
' \
  --arg environmentID "$ENVIRONMENT_ID" \
  --arg workflowID "$WORKFLOW_ID" \
  --arg minComponents "$MIN_COMPONENTS" \
  --arg minDependencies "$MIN_DEPENDENCIES" \
  --arg minAssets "$MIN_ASSETS" \
  --arg minInlineAssetBytes "$MIN_INLINE_ASSET_BYTES" \
  "$COPY_REPORT" > "$COPY_ASSERTION"

run_otsandbox environment inspect "$ENVIRONMENT_ID" \
  --store "$TARGET_STORE" \
  --json > "$INSPECT_REPORT"

jq -e '
  .ok == true
  and .environment.id == $environmentID
  and .environment.verificationWorkflowId == $workflowID
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
  --arg workflowID "$WORKFLOW_ID" \
  --arg minComponents "$MIN_COMPONENTS" \
  --arg minDependencies "$MIN_DEPENDENCIES" \
  --arg minAssets "$MIN_ASSETS" \
  --arg minInlineAssetBytes "$MIN_INLINE_ASSET_BYTES" \
  "$INSPECT_REPORT" > "$INSPECT_ASSERTION"

index=0
while IFS= read -r startup_file; do
  if [[ -z "$startup_file" ]]; then
    continue
  fi
  if [[ "$startup_file" != *=* ]]; then
    echo "--startup-file must be TARGET=SOURCE_FILE: $startup_file" >&2
    exit 2
  fi
  index=$((index + 1))
  run_otsandbox environment startup-file put "$ENVIRONMENT_ID" \
    --store "$TARGET_STORE" \
    --file "$startup_file" \
    --json > "${OUTPUT_PREFIX}-startup-file-${index}.json"
done <<< "$STARTUP_FILES_TEXT"

run_otsandbox store use "$TARGET_STORE" > "$ACTIVE_STORE_REPORT"
run_otsandbox store current --json > "$ACTIVE_STORE_CURRENT_REPORT"

jq -e '
  .ok == true
  and .name == $targetStore
  and .backend == "mysql"
' --arg targetStore "$TARGET_STORE" \
  "$ACTIVE_STORE_CURRENT_REPORT" > "$ACTIVE_STORE_ASSERTION"

if [[ -n "$VERIFY_CONTROL_PLANE_URL" ]]; then
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
    and .name == $targetStore
    and .backend == "mysql"
  ' --arg targetStore "$TARGET_STORE" \
    "$CONTROL_PLANE_CURRENT_REPORT" > "$CONTROL_PLANE_ASSERTION"
fi

if [[ "$RESTORE" == "1" ]]; then
  restore_args=(
    environment restore "$ENVIRONMENT_ID"
    --store "$TARGET_STORE"
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
  if [[ -n "$BASE_URL" ]]; then
    restore_args+=(--base-url "$BASE_URL")
  fi
  run_otsandbox "${restore_args[@]}" > "$RESTORE_REPORT"
  jq -e '
    .ok == true
    and .environment.summary.lastRestore.ok == true
    and .environment.summary.lastRestore.workflow.acceptance.ok == true
  ' "$RESTORE_REPORT" > "${OUTPUT_PREFIX}-restore-assertion.json"
fi

{
  echo "# MySQL Store verified environment promotion complete"
  echo
  echo "- source_store: \`$SOURCE_STORE\`"
  echo "- target_store: \`$TARGET_STORE\`"
  echo "- environment: \`$ENVIRONMENT_ID\`"
  echo "- workflow: \`$WORKFLOW_ID\`"
  echo "- active_store_switched: true"
  echo "- restore_executed: $([[ "$RESTORE" == "1" ]] && echo true || echo false)"
  echo
  echo "Evidence files:"
  echo "- $SOURCE_INSPECT_REPORT"
  echo "- $SOURCE_INSPECT_ASSERTION"
  echo "- ${OUTPUT_PREFIX}-store-provision.json"
  echo "- ${OUTPUT_PREFIX}-store-status-before-copy.json"
  echo "- $STATUS_AFTER_UPGRADE_REPORT"
  echo "- $STATUS_AFTER_UPGRADE_ASSERTION"
  echo "- $COPY_REPORT"
  echo "- $COPY_ASSERTION"
  echo "- $INSPECT_REPORT"
  echo "- $INSPECT_ASSERTION"
  echo "- $ACTIVE_STORE_REPORT"
  echo "- $ACTIVE_STORE_CURRENT_REPORT"
  echo "- $ACTIVE_STORE_ASSERTION"
  if [[ -n "$VERIFY_CONTROL_PLANE_URL" ]]; then
    echo "- $CONTROL_PLANE_CURRENT_REPORT"
    echo "- $CONTROL_PLANE_ASSERTION"
  fi
  if [[ "$RESTORE" == "1" ]]; then
    echo "- $RESTORE_REPORT"
    echo "- ${OUTPUT_PREFIX}-restore-assertion.json"
  fi
} | tee "${OUTPUT_PREFIX}-publish-complete.md"
