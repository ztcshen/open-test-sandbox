#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

SOURCE_STORE=""
TARGET_STORE=""
ENVIRONMENT_ID=""
WORKFLOW_ID=""
OUTPUT_PREFIX=".runtime/mysql-store-goal-audit-$(date +%Y%m%dT%H%M%S)"
MIN_COMPONENTS=1
MIN_DEPENDENCIES=0
MIN_ASSETS=1
MIN_INLINE_ASSET_BYTES=1
CONTROL_PLANE_URL=""
OTSANDBOX_BIN="${OTSANDBOX_BIN:-}"

usage() {
  cat <<'EOF'
Usage:
  mysql-store-goal-audit.sh \
    --from SOURCE_STORE \
    --to TARGET_MYSQL_STORE \
    --environment ENV_ID \
    --workflow WORKFLOW_ID \
    [--output-prefix PREFIX] \
    [--control-plane-url URL] \
    [--min-components N] [--min-dependencies N] [--min-assets N] \
    [--min-inline-asset-bytes N]

Read-only audit for the shared MySQL Store migration goal. It does not
provision, upgrade, copy, switch Stores, restore Docker, or run workflows.
It proves which gate is currently blocking the end-to-end objective.
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
    --control-plane-url)
      CONTROL_PLANE_URL="${2:-}"
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

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required for goal audit assertions" >&2
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

json_bool() {
  if [[ "$1" == "true" ]]; then
    echo true
  else
    echo false
  fi
}

mkdir -p "$(dirname "$OUTPUT_PREFIX")"

SOURCE_INSPECT="${OUTPUT_PREFIX}-source-env-inspect.json"
TARGET_STATUS="${OUTPUT_PREFIX}-target-store-status.json"
TARGET_INSPECT="${OUTPUT_PREFIX}-target-env-inspect.json"
ACTIVE_CURRENT="${OUTPUT_PREFIX}-active-store-current.json"
CONTROL_PLANE_CURRENT="${OUTPUT_PREFIX}-control-plane-store-current.json"
HANDSHAKE_REPORT="${OUTPUT_PREFIX}-mysql-handshake.json"
ROUTE_REPORT="${OUTPUT_PREFIX}-mysql-route.txt"
SUMMARY_JSON="${OUTPUT_PREFIX}-summary.json"
SUMMARY_MD="${OUTPUT_PREFIX}-summary.md"

SOURCE_READY=false
TARGET_REACHABLE=false
TARGET_SCHEMA_READY=false
TARGET_ENV_READY=false
ACTIVE_TARGET=false
CONTROL_PLANE_READY=false

if run_otsandbox environment inspect "$ENVIRONMENT_ID" --store "$SOURCE_STORE" --json > "$SOURCE_INSPECT"; then
  if jq -e '
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
    "$SOURCE_INSPECT" >/dev/null; then
    SOURCE_READY=true
  fi
fi

set +e
tools/smoke/mysql-store-preflight.sh --store "$TARGET_STORE" --output-prefix "$OUTPUT_PREFIX" >/dev/null 2>"${OUTPUT_PREFIX}-preflight.stderr"
PREFLIGHT_STATUS=$?
set -e
if [[ "$PREFLIGHT_STATUS" -eq 0 ]]; then
  TARGET_REACHABLE=true
fi
ROUTE_INTERFACE="$(awk '/interface:/ {print $2; exit}' "$ROUTE_REPORT" 2>/dev/null || true)"
ROUTE_GATEWAY="$(awk '/gateway:/ {print $2; exit}' "$ROUTE_REPORT" 2>/dev/null || true)"
MYSQL_ERROR="$(jq -r '.error // ""' "$HANDSHAKE_REPORT" 2>/dev/null || true)"
MYSQL_HOST="$(jq -r '.host // ""' "$HANDSHAKE_REPORT" 2>/dev/null || true)"
MYSQL_PORT="$(jq -r '(.port // "") | tostring' "$HANDSHAKE_REPORT" 2>/dev/null || true)"

if [[ "$TARGET_REACHABLE" == "true" ]]; then
  if run_otsandbox store status --store "$TARGET_STORE" --json > "$TARGET_STATUS"; then
    if jq -e '.ok == true and .backend == "mysql" and ((.pending // 0) == 0)' "$TARGET_STATUS" >/dev/null; then
      TARGET_SCHEMA_READY=true
    fi
  fi
  if run_otsandbox environment inspect "$ENVIRONMENT_ID" --store "$TARGET_STORE" --json > "$TARGET_INSPECT"; then
    if jq -e '
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
      "$TARGET_INSPECT" >/dev/null; then
      TARGET_ENV_READY=true
    fi
  fi
fi

if run_otsandbox store current --json > "$ACTIVE_CURRENT"; then
  if jq -e '.ok == true and .name == $targetStore and .backend == "mysql"' --arg targetStore "$TARGET_STORE" "$ACTIVE_CURRENT" >/dev/null; then
    ACTIVE_TARGET=true
  fi
fi

if [[ -n "$CONTROL_PLANE_URL" ]]; then
  CONTROL_PLANE_URL="${CONTROL_PLANE_URL%/}"
  CONTROL_PLANE_CURRENT_URL="${CONTROL_PLANE_URL}/api/store/current"
  if CONTROL_PLANE_CURRENT_URL="$CONTROL_PLANE_CURRENT_URL" python3 - <<'PY' > "$CONTROL_PLANE_CURRENT"; then
import os
import sys
import urllib.request

url = os.environ["CONTROL_PLANE_CURRENT_URL"]
with urllib.request.urlopen(url, timeout=10) as response:
    sys.stdout.buffer.write(response.read())
PY
    if jq -e '.ok == true and .configured == true and .name == $targetStore and .backend == "mysql"' --arg targetStore "$TARGET_STORE" "$CONTROL_PLANE_CURRENT" >/dev/null; then
      CONTROL_PLANE_READY=true
    fi
  fi
else
  CONTROL_PLANE_READY=true
fi

COMPLETE=false
if [[ "$SOURCE_READY" == "true" && "$TARGET_REACHABLE" == "true" && "$TARGET_SCHEMA_READY" == "true" && "$TARGET_ENV_READY" == "true" && "$ACTIVE_TARGET" == "true" && "$CONTROL_PLANE_READY" == "true" ]]; then
  COMPLETE=true
fi

BLOCKER="none"
NEXT_ACTION="none"
NEXT_COMMAND=()
if [[ "$SOURCE_READY" != "true" ]]; then
  BLOCKER="source-store-not-ready"
  NEXT_ACTION="fix the source Store verified environment or component graph, then rerun this audit"
  NEXT_COMMAND=(".runtime/team-mysql-goal-audit-commands.sh")
elif [[ "$TARGET_REACHABLE" != "true" ]]; then
  BLOCKER="target-mysql-handshake"
  NEXT_ACTION="fix VPN/route/proxy until the MySQL handshake succeeds, then run .runtime/team-mysql-pending-publish-commands.sh"
  NEXT_COMMAND=(".runtime/team-mysql-pending-publish-commands.sh")
elif [[ "$TARGET_SCHEMA_READY" != "true" ]]; then
  BLOCKER="target-schema-not-ready"
  NEXT_ACTION="run .runtime/team-mysql-pending-publish-commands.sh to provision, upgrade, and copy the Store"
  NEXT_COMMAND=(".runtime/team-mysql-pending-publish-commands.sh")
elif [[ "$TARGET_ENV_READY" != "true" ]]; then
  BLOCKER="target-environment-not-copied"
  NEXT_ACTION="run .runtime/team-mysql-pending-publish-commands.sh to copy the verified environment into the shared Store"
  NEXT_COMMAND=(".runtime/team-mysql-pending-publish-commands.sh")
elif [[ "$ACTIVE_TARGET" != "true" ]]; then
  BLOCKER="active-store-not-target"
  NEXT_ACTION="run otsandbox store use for the target MySQL Store, then rerun this audit"
  NEXT_COMMAND=("otsandbox" "store" "use" "$TARGET_STORE")
elif [[ "$CONTROL_PLANE_READY" != "true" ]]; then
  BLOCKER="control-plane-not-target"
  NEXT_ACTION="restart otsandbox serve with the target MySQL Store, then rerun this audit"
  NEXT_COMMAND=("otsandbox" "serve" "--store" "$TARGET_STORE")
elif [[ "$COMPLETE" == "true" ]]; then
  NEXT_ACTION="run .runtime/team-mysql-colleague-restore-commands.sh for a colleague/new-machine restore proof"
  NEXT_COMMAND=(".runtime/team-mysql-colleague-restore-commands.sh")
fi

NEXT_JSON="$(NEXT_ACTION="$NEXT_ACTION" python3 - "${NEXT_COMMAND[@]}" <<'PY'
import json
import os
import shlex
import sys

payload = {
    "action": os.environ["NEXT_ACTION"],
    "command": sys.argv[1:],
    "shell": shlex.join(sys.argv[1:]),
}
print(json.dumps(payload))
PY
)"

{
  echo "{"
  echo "  \"ok\": $(json_bool "$COMPLETE"),"
  echo "  \"blocker\": \"$BLOCKER\","
  echo "  \"nextAction\": $(echo "$NEXT_JSON" | jq '.action'),"
  echo "  \"nextCommand\": $(echo "$NEXT_JSON" | jq '.command'),"
  echo "  \"nextCommandShell\": $(echo "$NEXT_JSON" | jq '.shell'),"
  echo "  \"sourceStore\": \"$SOURCE_STORE\","
  echo "  \"targetStore\": \"$TARGET_STORE\","
  echo "  \"environmentId\": \"$ENVIRONMENT_ID\","
  echo "  \"workflowId\": \"$WORKFLOW_ID\","
  echo "  \"targetDiagnostics\": {"
  echo "    \"mysqlHost\": $(jq -Rn --arg value "$MYSQL_HOST" '$value'),"
  echo "    \"mysqlPort\": $(jq -Rn --arg value "$MYSQL_PORT" '$value'),"
  echo "    \"mysqlError\": $(jq -Rn --arg value "$MYSQL_ERROR" '$value'),"
  echo "    \"routeInterface\": $(jq -Rn --arg value "$ROUTE_INTERFACE" '$value'),"
  echo "    \"routeGateway\": $(jq -Rn --arg value "$ROUTE_GATEWAY" '$value')"
  echo "  },"
  echo "  \"checks\": {"
  echo "    \"sourceReady\": $(json_bool "$SOURCE_READY"),"
  echo "    \"targetReachable\": $(json_bool "$TARGET_REACHABLE"),"
  echo "    \"targetSchemaReady\": $(json_bool "$TARGET_SCHEMA_READY"),"
  echo "    \"targetEnvironmentReady\": $(json_bool "$TARGET_ENV_READY"),"
  echo "    \"activeStoreIsTarget\": $(json_bool "$ACTIVE_TARGET"),"
  echo "    \"controlPlaneIsTarget\": $(json_bool "$CONTROL_PLANE_READY")"
  echo "  }"
  echo "}"
} > "$SUMMARY_JSON"

{
  echo "# MySQL Store migration goal audit"
  echo
  echo "- ok: \`$COMPLETE\`"
  echo "- blocker: \`$BLOCKER\`"
  echo "- next_action: $NEXT_ACTION"
  echo "- next_command: \`$(echo "$NEXT_JSON" | jq -r '.shell')\`"
  echo "- source_store: \`$SOURCE_STORE\`"
  echo "- target_store: \`$TARGET_STORE\`"
  echo "- environment: \`$ENVIRONMENT_ID\`"
  echo "- workflow: \`$WORKFLOW_ID\`"
  echo "- mysql_error: \`${MYSQL_ERROR:-none}\`"
  echo "- route_interface: \`${ROUTE_INTERFACE:-unknown}\`"
  echo "- route_gateway: \`${ROUTE_GATEWAY:-unknown}\`"
  echo
  echo "Checks:"
  echo "- sourceReady: \`$SOURCE_READY\`"
  echo "- targetReachable: \`$TARGET_REACHABLE\`"
  echo "- targetSchemaReady: \`$TARGET_SCHEMA_READY\`"
  echo "- targetEnvironmentReady: \`$TARGET_ENV_READY\`"
  echo "- activeStoreIsTarget: \`$ACTIVE_TARGET\`"
  echo "- controlPlaneIsTarget: \`$CONTROL_PLANE_READY\`"
  echo
  echo "Evidence files:"
  echo "- $SOURCE_INSPECT"
  echo "- ${OUTPUT_PREFIX}-mysql-handshake.json"
  echo "- $TARGET_STATUS"
  echo "- $TARGET_INSPECT"
  echo "- $ACTIVE_CURRENT"
  if [[ -n "$CONTROL_PLANE_URL" ]]; then
    echo "- $CONTROL_PLANE_CURRENT"
  fi
  echo "- $SUMMARY_JSON"
} > "$SUMMARY_MD"

cat "$SUMMARY_MD"
if [[ "$COMPLETE" == "true" ]]; then
  exit 0
fi
exit 2
