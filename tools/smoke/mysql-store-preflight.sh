#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

STORE_NAME=""
STORE_URL=""
OUTPUT_PREFIX=".runtime/mysql-store-preflight"
AGENT_TESTBENCH_BIN="${AGENT_TESTBENCH_BIN:-}"
HANDSHAKE_PROBE="${AGENT_TESTBENCH_MYSQL_HANDSHAKE_PROBE:-python3 tools/smoke/mysql-handshake-probe.py}"

usage() {
  cat <<'EOF'
Usage:
  mysql-store-preflight.sh --store NAME [--output-prefix PREFIX]
  mysql-store-preflight.sh --url MYSQL_DSN [--output-prefix PREFIX]

Checks route/proxy/aTrust evidence and requires a real MySQL handshake before
shared Store promotion. This script never runs store provision, schema upgrade,
store copy, read-back, or environment restore.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --store)
      STORE_NAME="${2:-}"
      shift 2
      ;;
    --url)
      STORE_URL="${2:-}"
      shift 2
      ;;
    --output-prefix)
      OUTPUT_PREFIX="${2:-}"
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

if [[ -z "$STORE_NAME" && -z "$STORE_URL" ]]; then
  echo "set --store NAME or --url MYSQL_DSN" >&2
  exit 2
fi
if [[ -n "$STORE_NAME" && -n "$STORE_URL" ]]; then
  echo "set only one of --store or --url" >&2
  exit 2
fi

run_agent-testbench() {
  if [[ -n "$AGENT_TESTBENCH_BIN" ]]; then
    "$AGENT_TESTBENCH_BIN" "$@"
  elif [[ -x ".runtime/agent-testbench-dev" ]]; then
    .runtime/agent-testbench-dev "$@"
  else
    go run ./cmd/agent-testbench "$@"
  fi
}

if [[ -n "$STORE_NAME" ]]; then
  STORE_URL="$(
    run_agent-testbench store config list --json |
      jq -r --arg name "$STORE_NAME" '.stores[] | select(.name == $name) | .url'
  )"
fi
if [[ -z "$STORE_URL" ]]; then
  echo "Store config not found: ${STORE_NAME:-<url>}" >&2
  exit 2
fi

mkdir -p "$(dirname "$OUTPUT_PREFIX")"

read -r MASKED_URL STORE_HOST STORE_PORT < <(
  STORE_URL="$STORE_URL" python3 - <<'PY'
import os
from urllib.parse import urlparse, urlunparse

url = os.environ["STORE_URL"]
parsed = urlparse(url)
host = parsed.hostname or ""
port = parsed.port or 3306
netloc = parsed.netloc
if parsed.password:
    user = parsed.username or ""
    hostport = host
    if parsed.port:
        hostport = f"{host}:{parsed.port}"
    netloc = f"{user}:xxxxx@{hostport}" if user else hostport
masked = urlunparse((parsed.scheme.lower(), netloc, parsed.path, parsed.params, parsed.query, parsed.fragment))
print(masked, host, port)
PY
)

ROUTE_FILE="${OUTPUT_PREFIX}-mysql-route.txt"
ROUTES_FILE="${OUTPUT_PREFIX}-routes.txt"
PROXY_FILE="${OUTPUT_PREFIX}-system-proxy.txt"
ATRUST_STATUS_FILE="${OUTPUT_PREFIX}-atrust-status-tail.txt"
ATRUST_SSLPROXY_FILE="${OUTPUT_PREFIX}-atrust-sslproxy-tail.txt"
HANDSHAKE_FILE="${OUTPUT_PREFIX}-mysql-handshake.json"
SUMMARY_FILE="${OUTPUT_PREFIX}-mysql-network-summary.txt"
BLOCKED_FILE="${OUTPUT_PREFIX}-mysql-preflight-blocked.md"
OK_FILE="${OUTPUT_PREFIX}-mysql-preflight-ok.md"

if [[ -n "$STORE_HOST" ]]; then
  route -n get "$STORE_HOST" > "$ROUTE_FILE" 2>&1 || true
fi
netstat -rn -f inet > "$ROUTES_FILE" 2>&1 || true
scutil --proxy > "$PROXY_FILE" 2>&1 || true

ATRUST_LOG_DIR="$HOME/Library/Application Support/aTrust/logs"
if [[ -d "$ATRUST_LOG_DIR" ]]; then
  tail -n 260 "$ATRUST_LOG_DIR/aTrustHttpServer.log" 2>/dev/null |
    rg -n 'service/status|tunnelStatus|status|logout|login|clientIp|aUem/status|virtualNetStatus|CONNECT 10\.0\.20\.108:3306|SPA|spa seed' \
    > "$ATRUST_STATUS_FILE" || true
  tail -n 260 "$ATRUST_LOG_DIR/aTrustSSLProxy.log" 2>/dev/null |
    rg -n '10\.0\.20\.108:3306|CONNECT|spa seed|mitm proxy|error|fail' \
    > "$ATRUST_SSLPROXY_FILE" || true
else
  : > "$ATRUST_STATUS_FILE"
  : > "$ATRUST_SSLPROXY_FILE"
fi

{
  echo "team_store=${STORE_NAME:-<direct-url>}"
  echo "store_url=$MASKED_URL"
  echo "store_host=$STORE_HOST"
  echo "store_port=$STORE_PORT"
  echo "route_file=$ROUTE_FILE"
  echo "routes_file=$ROUTES_FILE"
  echo "system_proxy_file=$PROXY_FILE"
  echo "atrust_status_file=$ATRUST_STATUS_FILE"
  echo "atrust_sslproxy_file=$ATRUST_SSLPROXY_FILE"
  echo "handshake_file=$HANDSHAKE_FILE"
} > "$SUMMARY_FILE"

if ! $HANDSHAKE_PROBE --url "$STORE_URL" --json > "$HANDSHAKE_FILE"; then
  ROUTE_INTERFACE="$(awk '/interface:/ {print $2; exit}' "$ROUTE_FILE" 2>/dev/null || true)"
  ROUTE_GATEWAY="$(awk '/gateway:/ {print $2; exit}' "$ROUTE_FILE" 2>/dev/null || true)"
  HANDSHAKE_ERROR="$(jq -r '.error // ""' "$HANDSHAKE_FILE" 2>/dev/null || true)"
  {
    echo "# Baofoo MySQL Store preflight blocked"
    echo
    echo "- team_store: \`${STORE_NAME:-<direct-url>}\`"
    echo "- store_url: \`$MASKED_URL\`"
    echo "- store_host: \`$STORE_HOST\`"
    echo "- store_port: \`$STORE_PORT\`"
    echo "- route_interface: \`${ROUTE_INTERFACE:-unknown}\`"
    echo "- route_gateway: \`${ROUTE_GATEWAY:-unknown}\`"
    echo "- mysql_handshake: failed"
    echo "- mysql_error: \`${HANDSHAKE_ERROR:-unknown}\`"
    echo
    echo "The script stopped before store provision, schema upgrade, store copy, read-back, or restore."
    echo "A real MySQL protocol handshake must succeed before this shared Store can be created or updated."
    echo
    echo "Operator checks:"
    echo "- In aTrust/EasyConnect, make sure the session is logged in and the MySQL resource for \`$STORE_HOST:$STORE_PORT\` is authorized."
    echo "- If the Baofoo resource is route-based, \`route -n get $STORE_HOST\` should not point at the Wi-Fi gateway."
    echo "- If the Baofoo resource is proxy-based, configure the Store to the concrete local proxy endpoint that emits a MySQL handshake."
    if [[ "$STORE_HOST" == 10.* && "$ROUTE_INTERFACE" != utun* ]]; then
      echo "- Current route looks wrong for a private VPN target. If approved by the operator, a temporary host route to the VPN gateway may be required, for example:"
      echo "  \`sudo route -n add -host $STORE_HOST 10.251.1.1\`"
      echo "  Remove it after testing with:"
      echo "  \`sudo route -n delete -host $STORE_HOST\`"
    fi
    echo
    echo "Evidence files:"
    echo "- $ROUTE_FILE"
    echo "- $ROUTES_FILE"
    echo "- $PROXY_FILE"
    echo "- $ATRUST_STATUS_FILE"
    echo "- $ATRUST_SSLPROXY_FILE"
    echo "- $HANDSHAKE_FILE"
  } > "$BLOCKED_FILE"
  cat "$BLOCKED_FILE" >&2
  exit 2
fi

{
  echo "# Baofoo MySQL Store preflight ok"
  echo
  echo "- team_store: \`${STORE_NAME:-<direct-url>}\`"
  echo "- store_url: \`$MASKED_URL\`"
  echo "- store_host: \`$STORE_HOST\`"
  echo "- store_port: \`$STORE_PORT\`"
  echo "- mysql_handshake: ok"
  echo
  echo "The shared MySQL Store is reachable. It is now safe to run the Store promotion script."
} > "$OK_FILE"
cat "$OK_FILE"
